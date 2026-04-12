package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/crossplane/function-sdk-go/errors"
	"github.com/crossplane/function-sdk-go/logging"
	fnv1 "github.com/crossplane/function-sdk-go/proto/v1"
	"github.com/crossplane/function-sdk-go/request"
	"github.com/crossplane/function-sdk-go/response"
	"github.com/docker/cli/cli/config"
	"github.com/docker/cli/cli/config/types"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/wompipomp/function-starlark/builtins"
	"github.com/wompipomp/function-starlark/input/v1alpha1"
	"github.com/wompipomp/function-starlark/metrics"
	"github.com/wompipomp/function-starlark/runtime"
	"github.com/wompipomp/function-starlark/runtime/oci"
)

// Function implements the Crossplane composition function.
type Function struct {
	fnv1.UnimplementedFunctionRunnerServiceServer
	log        logging.Logger
	runtime    *runtime.Runtime
	scriptDir  string      // base directory for ConfigMap-mounted scripts (default "/scripts")
	ociCache   *oci.Cache  // shared OCI module cache across reconciliations
	ociFetcher oci.Fetcher // OCI image fetcher (nil = default RemoteFetcher); injectable for tests
}

// RunFunction processes a RunFunctionRequest.
func (f *Function) RunFunction(ctx context.Context, req *fnv1.RunFunctionRequest) (*fnv1.RunFunctionResponse, error) {
	log := f.log.WithValues("tag", req.GetMeta().GetTag())
	log.Debug("Running function")

	// Metrics: track reconciliation duration and count with final filename label.
	// Guard: skip recording when filename was never resolved (early failures).
	filename := "unknown"
	reconcileTimer := prometheus.NewTimer(prometheus.ObserverFunc(func(v float64) {
		if filename != "unknown" {
			metrics.ReconciliationDurationSeconds.WithLabelValues(filename).Observe(v)
		}
	}))
	defer reconcileTimer.ObserveDuration()
	defer func() {
		if filename != "unknown" {
			metrics.ReconciliationsTotal.WithLabelValues(filename).Inc()
		}
	}()

	// CRITICAL: response.To copies desired state from the request,
	// preserving resources set by previous functions in the pipeline.
	rsp := response.To(req, response.DefaultTTL)

	// fatal logs at error level and sets the response to Fatal.
	fatal := func(err error) {
		log.Info("Fatal error", "error", err.Error())
		response.Fatal(rsp, err)
	}

	// Parse the StarlarkInput from the Composition.
	in := &v1alpha1.StarlarkInput{}
	if err := request.GetInput(req, in); err != nil {
		fatal(errors.Wrapf(err, "cannot get Function input"))
		return rsp, nil
	}

	// Validate that a source script is provided.
	if in.Spec.Source == "" && in.Spec.ScriptConfigRef == nil {
		fatal(errors.New("spec.source or spec.scriptConfigRef is required"))
		return rsp, nil
	}

	log.Debug("Parsed StarlarkInput", "source-length", len(in.Spec.Source))

	// Resolve script source and filename.
	// Inline scripts use "composition.star"; ConfigMap scripts use the real key.
	source := in.Spec.Source
	filename = "composition.star"
	if source == "" && in.Spec.ScriptConfigRef != nil {
		key := in.Spec.ScriptConfigRef.Key
		if key == "" {
			key = "main.star"
		}
		filename = key

		var err error
		source, err = f.loadScript(in.Spec.ScriptConfigRef)
		if err != nil {
			fatal(errors.Wrapf(err, "loading script from ConfigMap"))
			return rsp, nil
		}
	}

	if source != "" {
		// Capability detection logging (transparent -- does not prevent execution).
		if !request.AdvertisesCapabilities(req) {
			log.Debug("Crossplane does not advertise capabilities")
		} else {
			if !request.HasCapability(req, fnv1.Capability_CAPABILITY_CONDITIONS) {
				log.Debug("Crossplane does not support conditions")
			}
			if !request.HasCapability(req, fnv1.Capability_CAPABILITY_REQUIRED_RESOURCES) {
				log.Debug("Crossplane does not support required resources")
			}
		}

		// Create all collectors for this execution.
		condCollector := builtins.NewConditionCollector()
		collector := builtins.NewCollector(condCollector, filename,
			req.GetObserved().GetComposite().GetResource())
		connCollector := builtins.NewConnectionCollector()
		reqCollector := builtins.NewRequirementsCollector()
		ttlCollector := builtins.NewTTLCollector()

		globals, err := builtins.BuildGlobals(req, collector, condCollector, connCollector, reqCollector, ttlCollector)
		if err != nil {
			fatal(errors.Wrapf(err, "building Starlark globals"))
			return rsp, nil
		}

		// --- OCI Module Resolution Phase (resolve-then-execute) ---
		// Deep copy inline modules to avoid mutating input across reconciliations.
		inlineModules := make(map[string]string, len(in.Spec.Modules))
		for k, v := range in.Spec.Modules {
			inlineModules[k] = v
		}

		// Resolve effective default OCI registry (spec > env var).
		defaultRegistry := ""
		if in.Spec.OCIDefaultRegistry != "" {
			defaultRegistry = oci.NormalizeRegistry(in.Spec.OCIDefaultRegistry)
		} else if envReg := os.Getenv("STARLARK_OCI_DEFAULT_REGISTRY"); envReg != "" {
			defaultRegistry = oci.NormalizeRegistry(envReg)
		}

		// Validate the registry value early so malformed config is caught at the
		// input boundary rather than deep inside expansion/resolution.
		if defaultRegistry != "" {
			if err := oci.ValidateRegistry(defaultRegistry); err != nil {
				fatal(errors.Wrap(err, "validating default OCI registry"))
				return rsp, nil
			}
		}

		// Scan for OCI load targets in main script + inline modules.
		// Parse errors are non-fatal here: if the script has syntax errors,
		// it will fail later during compilation with a more appropriate message.
		// However, default-registry config errors are fatal (user must fix config).
		ociTargets, scanErr := oci.ScanForOCILoads(source, inlineModules, defaultRegistry)
		if scanErr != nil {
			if strings.Contains(scanErr.Error(), "requires a default OCI registry") {
				fatal(errors.Wrapf(scanErr, "scanning for OCI load targets"))
				return rsp, nil
			}
			log.Debug("OCI scan skipped due to parse error", "error", scanErr)
			ociTargets = nil
		}

		// Resolve effective docker config secret (spec > env var).
		dockerConfigSecret := in.Spec.DockerConfigSecret
		if dockerConfigSecret == "" {
			dockerConfigSecret = os.Getenv("STARLARK_DOCKER_CONFIG_SECRET")
		}

		// Resolve gRPC credential name for OCI registry auth.
		dockerConfigCredential := in.Spec.DockerConfigCredential

		// Resolve effective insecure registries (spec > env var, comma-separated).
		insecureRegistries := in.Spec.OCIInsecureRegistries
		if len(insecureRegistries) == 0 {
			if envInsecure := os.Getenv("STARLARK_OCI_INSECURE_REGISTRIES"); envInsecure != "" {
				for _, r := range strings.Split(envInsecure, ",") {
					if trimmed := strings.TrimSpace(r); trimmed != "" {
						insecureRegistries = append(insecureRegistries, trimmed)
					}
				}
			}
		}

		if len(ociTargets) > 0 {
			// Build keychain chain: gRPC credential > filesystem secret > default.
			var keychains []authn.Keychain

			// 1. gRPC credential (crossplane render --function-credentials / Composition credentials block).
			if dockerConfigCredential != "" {
				cred, err := request.GetCredentials(req, dockerConfigCredential)
				if err == nil {
					if kc := buildKeychainFromCredential(cred.Data); kc != nil {
						keychains = append(keychains, kc)
					}
				}
			}

			// 2. Filesystem secret (DeploymentRuntimeConfig volume mount).
			if fsKC := buildKeychain(dockerConfigSecret); fsKC != authn.DefaultKeychain {
				keychains = append(keychains, fsKC)
			}

			// 3. Default keychain (host Docker config).
			keychains = append(keychains, authn.DefaultKeychain)

			keychain := authn.NewMultiKeychain(keychains...)

			fetcher := f.ociFetcher
			if fetcher == nil {
				fetcher = oci.RemoteFetcher{}
			}

			resolver := oci.NewResolver(f.ociCache, keychain, fetcher, log, defaultRegistry, insecureRegistries)

			ociTimer := prometheus.NewTimer(metrics.OCIResolveDurationSeconds.WithLabelValues(filename))
			resolvedModules, resolveErr := resolver.Resolve(ctx, ociTargets)
			ociTimer.ObserveDuration()
			if resolveErr != nil {
				fatal(errors.Wrapf(resolveErr, "resolving OCI modules"))
				return rsp, nil
			}

			// Inject OCI modules into inline map (OCI overrides local per context decision).
			for name, src := range resolvedModules {
				inlineModules[name] = src
			}
		}

		// Determine script directory for filesystem module resolution.
		var scriptSearchDir string
		if in.Spec.ScriptConfigRef != nil {
			dir := f.scriptDir
			if dir == "" {
				dir = "/scripts"
			}
			scriptSearchDir = filepath.Join(dir, in.Spec.ScriptConfigRef.Name)
		}

		// Build search paths: script's own dir first (if ConfigMap), then configured modulePaths.
		var searchPaths []string
		if scriptSearchDir != "" {
			searchPaths = append(searchPaths, scriptSearchDir)
		}
		searchPaths = append(searchPaths, in.Spec.ModulePaths...)

		// Create module loader with merged inline+OCI modules, search paths, and same builtins.
		loader := runtime.NewModuleLoader(inlineModules, searchPaths, globals, f.runtime, defaultRegistry)

		// Expand star imports before execution.
		source, err = loader.ResolveStarImports(source, filename)
		if err != nil {
			fatal(errors.Wrapf(err, "resolving star imports"))
			return rsp, nil
		}

		execTimer := prometheus.NewTimer(metrics.ExecutionDurationSeconds.WithLabelValues(filename))
		_, err = f.runtime.Execute(source, globals, filename, loader.LoadFunc())
		execTimer.ObserveDuration()
		if err != nil {
			// Check for FatalError from fatal() builtin before generic error handling.
			var fatalErr *builtins.FatalError
			if errors.As(err, &fatalErr) {
				fatal(errors.New(fatalErr.Message))
				// Still apply conditions/events/requirements collected before fatal().
				// These are useful diagnostics even though execution was halted.
				builtins.ApplyConditions(rsp, condCollector.Conditions())
				builtins.ApplyEvents(rsp, condCollector.Events())
				builtins.ApplyRequirements(rsp, reqCollector.Requirements())
				return rsp, nil
			}
			fatal(errors.Wrapf(err, "starlark execution failed"))
			return rsp, nil
		}

		// Validate and generate dependency Usage resources.
		deps := collector.Dependencies()
		if len(deps) > 0 {
			// Build resource name set for validation.
			resourceNames := make(map[string]bool, len(collector.Resources()))
			for name := range collector.Resources() {
				resourceNames[name] = true
			}

			if err := builtins.ValidateDependencies(deps, resourceNames); err != nil {
				fatal(errors.Wrapf(err, "dependency validation failed"))
				return rsp, nil
			}

			// Warn about string refs that don't match any created resource.
			for _, w := range builtins.WarnUnmatchedStringRefs(deps, resourceNames) {
				response.Warning(rsp, errors.New(w))
			}

			// Resolve effective usage API version (spec > env var > default).
			usageAPIOverride := in.Spec.UsageAPIVersion
			if usageAPIOverride == "" {
				usageAPIOverride = os.Getenv("STARLARK_USAGE_API_VERSION")
			}
			apiVersion := builtins.ResolveUsageAPIVersion(usageAPIOverride)
			usageResources := builtins.BuildUsageResources(deps, apiVersion, collector.Resources())

			// Ensure Desired and Resources maps exist.
			if rsp.Desired == nil {
				rsp.Desired = &fnv1.State{}
			}
			if rsp.Desired.Resources == nil {
				rsp.Desired.Resources = make(map[string]*fnv1.Resource)
			}

			for name, body := range usageResources {
				rsp.Desired.Resources[name] = &fnv1.Resource{
					Resource: body,
					Ready:    fnv1.Ready_READY_TRUE,
				}
			}

			// --- Creation Sequencing ---
			// Build observed resource map for field path evaluation.
			observedResources := make(map[string]*structpb.Struct)
			for name, r := range req.GetObserved().GetResources() {
				observedResources[name] = r.GetResource()
			}

			// Determine sequencing TTL.
			seqTTLDuration := 10 * time.Second // default 10s
			if in.Spec.SequencingTTL != "" {
				parsed, parseErr := time.ParseDuration(in.Spec.SequencingTTL)
				if parseErr != nil {
					fatal(errors.Wrapf(parseErr, "invalid spec.sequencingTTL %q", in.Spec.SequencingTTL))
					return rsp, nil
				}
				seqTTLDuration = parsed
			}
			seqTTLSeconds := int(seqTTLDuration.Seconds())

			seq := builtins.NewSequencer(deps, resourceNames, observedResources, seqTTLSeconds)
			result := seq.Evaluate()

			if result.AnyDeferred {
				// Remove deferred resources from collector before ApplyResources.
				collector.RemoveResources(result.Deferred)

				// Record metric.
				metrics.ResourcesDeferredTotal.WithLabelValues(filename).Add(float64(len(result.Deferred)))

				// Override response TTL for faster requeue (SEQ-05).
				rsp.Meta.Ttl = durationpb.New(seqTTLDuration)

				// Set Synced=False to prevent premature Ready (per CONTEXT.md decision).
				response.ConditionFalse(rsp, "Synced", "CreationSequencing").
					WithMessage(fmt.Sprintf("%d resource(s) waiting for dependencies", len(result.Deferred)))
			} else {
				// Converged: clear TTL so Crossplane uses its own poll interval.
				rsp.Meta.Ttl = nil
			}

			// Append sequencing events to condCollector for response emission.
			for _, e := range result.Events {
				condCollector.AddEvent(e)
			}
		}

		// Apply collected resources to response (merges with prior desired state).
		if err := builtins.ApplyResources(rsp, collector); err != nil {
			fatal(errors.Wrapf(err, "applying composed resources"))
			return rsp, nil
		}
		metrics.ResourcesEmittedTotal.WithLabelValues(filename).Add(float64(len(collector.Resources())))

		// Apply dxr status changes to response desired composite.
		if err := builtins.ApplyDXR(rsp, globals["dxr"]); err != nil {
			fatal(errors.Wrapf(err, "applying dxr status"))
			return rsp, nil
		}

		// Apply pipeline context changes.
		if err := builtins.ApplyContext(rsp, globals["context"]); err != nil {
			fatal(errors.Wrapf(err, "applying context"))
			return rsp, nil
		}

		// Apply XR-level connection details.
		builtins.ApplyConnectionDetails(rsp, connCollector.ConnectionDetails())

		// Apply conditions.
		builtins.ApplyConditions(rsp, condCollector.Conditions())

		// Apply events.
		builtins.ApplyEvents(rsp, condCollector.Events())

		// Apply requirements.
		builtins.ApplyRequirements(rsp, reqCollector.Requirements())

		// Emit warnings collected during execution.
		for _, w := range reqCollector.Warnings() {
			response.Warning(rsp, errors.New(w))
		}

		response.Normal(rsp, "function-starlark: executed successfully")
	} else {
		response.Normal(rsp, "function-starlark: input parsed successfully (passthrough mode)")
	}

	return rsp, nil
}

// buildKeychain returns an authn.Keychain for OCI registry authentication.
// When dockerConfigSecret is set, it constructs a per-request keychain from the
// mounted Docker config directory. The secret should be mounted at
// /var/run/secrets/docker/<secret-name>/ containing a config.json.
//
// This avoids the previous os.Setenv("DOCKER_CONFIG") approach which was
// process-global and raced under concurrent gRPC requests.
func buildKeychain(dockerConfigSecret string) authn.Keychain {
	if dockerConfigSecret == "" {
		return authn.DefaultKeychain
	}
	if !filepath.IsLocal(dockerConfigSecret) {
		return authn.DefaultKeychain
	}
	configDir := filepath.Join("/var/run/secrets/docker", dockerConfigSecret)
	if _, err := os.Stat(configDir); err != nil { //nolint:gosec // path validated by filepath.IsLocal above
		return authn.DefaultKeychain
	}
	return authn.NewMultiKeychain(
		&configDirKeychain{dir: configDir},
		authn.DefaultKeychain,
	)
}

// configDirKeychain implements authn.Keychain by loading credentials from a
// specific Docker config directory. Thread-safe — each request gets its own
// keychain instance without mutating process-global state.
type configDirKeychain struct {
	dir string
}

func (k *configDirKeychain) Resolve(target authn.Resource) (authn.Authenticator, error) {
	cf, err := config.Load(k.dir)
	if err != nil {
		return authn.Anonymous, nil
	}
	var cfg, empty types.AuthConfig
	for _, key := range []string{target.String(), target.RegistryStr()} {
		cfg, err = cf.GetAuthConfig(key)
		if err != nil {
			return authn.Anonymous, nil
		}
		cfg.ServerAddress = ""
		if cfg != empty {
			break
		}
	}
	if cfg == empty {
		return authn.Anonymous, nil
	}
	return authn.FromConfig(authn.AuthConfig{
		Username:      cfg.Username,
		Password:      cfg.Password,
		Auth:          cfg.Auth,
		IdentityToken: cfg.IdentityToken,
		RegistryToken: cfg.RegistryToken,
	}), nil
}

// credentialKeychain implements authn.Keychain by loading credentials from
// Docker config.json bytes delivered via gRPC credential data.
type credentialKeychain struct {
	data []byte
}

func (k *credentialKeychain) Resolve(target authn.Resource) (authn.Authenticator, error) {
	cf, err := config.LoadFromReader(bytes.NewReader(k.data))
	if err != nil {
		return authn.Anonymous, nil
	}
	var cfg, empty types.AuthConfig
	for _, key := range []string{target.String(), target.RegistryStr()} {
		cfg, err = cf.GetAuthConfig(key)
		if err != nil {
			return authn.Anonymous, nil
		}
		cfg.ServerAddress = ""
		if cfg != empty {
			break
		}
	}
	if cfg == empty {
		return authn.Anonymous, nil
	}
	return authn.FromConfig(authn.AuthConfig{
		Username:      cfg.Username,
		Password:      cfg.Password,
		Auth:          cfg.Auth,
		IdentityToken: cfg.IdentityToken,
		RegistryToken: cfg.RegistryToken,
	}), nil
}

// buildKeychainFromCredential creates an authn.Keychain from gRPC credential
// data containing a Docker config.json. Checks "config.json" first (generic
// secrets), then ".dockerconfigjson" (kubernetes.io/dockerconfigjson secrets).
// Returns nil if neither key is found.
func buildKeychainFromCredential(data map[string][]byte) authn.Keychain {
	configJSON, ok := data["config.json"]
	if !ok {
		configJSON, ok = data[".dockerconfigjson"]
		if !ok {
			return nil
		}
	}
	return &credentialKeychain{data: configJSON}
}

// loadScript reads a Starlark script from a ConfigMap volume mount.
// The ConfigMap is expected to be mounted at {f.scriptDir}/{ref.Name}/{key}.
func (f *Function) loadScript(ref *v1alpha1.ScriptConfigRef) (string, error) {
	key := ref.Key
	if key == "" {
		key = "main.star"
	}

	dir := f.scriptDir
	if dir == "" {
		dir = "/scripts"
	}

	// Validate path components to prevent directory traversal.
	if !filepath.IsLocal(ref.Name) {
		return "", fmt.Errorf("script ConfigMap name %q contains path traversal", ref.Name)
	}
	if !filepath.IsLocal(key) {
		return "", fmt.Errorf("script key %q contains path traversal", key)
	}

	path := filepath.Join(dir, ref.Name, key)
	data, err := os.ReadFile(path) //nolint:gosec // path components validated by filepath.IsLocal above
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf(
				"script file %q not found; ensure the ConfigMap %q is mounted via DeploymentRuntimeConfig",
				path, ref.Name,
			)
		}
		return "", fmt.Errorf("reading script file %q: %w", path, err)
	}
	return string(data), nil
}
