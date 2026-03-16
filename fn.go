package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/crossplane/function-sdk-go/errors"
	"github.com/crossplane/function-sdk-go/logging"
	fnv1 "github.com/crossplane/function-sdk-go/proto/v1"
	"github.com/crossplane/function-sdk-go/request"
	"github.com/crossplane/function-sdk-go/response"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/protobuf/types/known/durationpb"

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
	log.Info("Running function")

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

	// Parse the StarlarkInput from the Composition.
	in := &v1alpha1.StarlarkInput{}
	if err := request.GetInput(req, in); err != nil {
		response.Fatal(rsp, errors.Wrapf(err, "cannot get Function input"))
		return rsp, nil
	}

	// Validate that a source script is provided.
	if in.Spec.Source == "" && in.Spec.ScriptConfigRef == nil {
		response.Fatal(rsp, errors.New("spec.source or spec.scriptConfigRef is required"))
		return rsp, nil
	}

	log.Info("Parsed StarlarkInput", "source-length", len(in.Spec.Source))

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
			response.Fatal(rsp, errors.Wrapf(err, "loading script from ConfigMap"))
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
		collector := builtins.NewCollector(condCollector, filename)
		connCollector := builtins.NewConnectionCollector()
		reqCollector := builtins.NewRequirementsCollector()

		globals, err := builtins.BuildGlobals(req, collector, condCollector, connCollector, reqCollector)
		if err != nil {
			response.Fatal(rsp, errors.Wrapf(err, "building Starlark globals"))
			return rsp, nil
		}

		// --- OCI Module Resolution Phase (resolve-then-execute) ---
		// Deep copy inline modules to avoid mutating input across reconciliations.
		inlineModules := make(map[string]string, len(in.Spec.Modules))
		for k, v := range in.Spec.Modules {
			inlineModules[k] = v
		}

		// Scan for OCI load targets in main script + inline modules.
		// Parse errors are non-fatal here: if the script has syntax errors,
		// it will fail later during compilation with a more appropriate message.
		ociTargets, scanErr := oci.ScanForOCILoads(source, inlineModules)
		if scanErr != nil {
			log.Debug("OCI scan skipped due to parse error", "error", scanErr)
			ociTargets = nil
		}

		if len(ociTargets) > 0 {
			// Build keychain from Docker config secret if specified.
			keychain := buildKeychain(in.Spec.DockerConfigSecret)

			fetcher := f.ociFetcher
			if fetcher == nil {
				fetcher = oci.RemoteFetcher{}
			}

			resolver := oci.NewResolver(f.ociCache, keychain, fetcher, log)

			ociTimer := prometheus.NewTimer(metrics.OCIResolveDurationSeconds.WithLabelValues(filename))
			resolvedModules, resolveErr := resolver.Resolve(ctx, ociTargets)
			ociTimer.ObserveDuration()
			if resolveErr != nil {
				response.Fatal(rsp, errors.Wrapf(resolveErr, "resolving OCI modules"))
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
		loader := runtime.NewModuleLoader(inlineModules, searchPaths, globals, f.runtime)

		// Expand star imports before execution.
		source, err = loader.ResolveStarImports(source, filename)
		if err != nil {
			response.Fatal(rsp, errors.Wrapf(err, "resolving star imports"))
			return rsp, nil
		}

		execTimer := prometheus.NewTimer(metrics.ExecutionDurationSeconds.WithLabelValues(filename))
		_, err = f.runtime.Execute(source, globals, filename, loader.LoadFunc())
		execTimer.ObserveDuration()
		if err != nil {
			// Check for FatalError from fatal() builtin before generic error handling.
			var fatalErr *builtins.FatalError
			if errors.As(err, &fatalErr) {
				response.Fatal(rsp, errors.New(fatalErr.Message))
				// Still apply conditions/events/requirements collected before fatal().
				// These are useful diagnostics even though execution was halted.
				builtins.ApplyConditions(rsp, condCollector.Conditions())
				builtins.ApplyEvents(rsp, condCollector.Events())
				builtins.ApplyRequirements(rsp, reqCollector.Requirements())
				return rsp, nil
			}
			response.Fatal(rsp, errors.Wrapf(err, "starlark execution failed"))
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
				response.Fatal(rsp, errors.Wrapf(err, "dependency validation failed"))
				return rsp, nil
			}

			// Warn about string refs that don't match any created resource.
			for _, w := range builtins.WarnUnmatchedStringRefs(deps, resourceNames) {
				response.Warning(rsp, errors.New(w))
			}

			// Generate Usage resources and insert into response.
			apiVersion := builtins.DetectUsageAPIVersion(in.Spec.UsageAPIVersion)
			usageResources := builtins.BuildUsageResources(deps, apiVersion)

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

			response.Warning(rsp, errors.Errorf(
				"depends_on: %d Usage resource(s) generated; compositeDeletePolicy=Foreground is required on the claim for deletion ordering to take effect",
				len(usageResources),
			))

			// --- Creation Sequencing ---
			// Build observed resource name set.
			observedNames := make(map[string]bool)
			for name := range req.GetObserved().GetResources() {
				observedNames[name] = true
			}

			// Determine sequencing TTL.
			seqTTLDuration := 10 * time.Second // default 10s
			if in.Spec.SequencingTTL != "" {
				parsed, parseErr := time.ParseDuration(in.Spec.SequencingTTL)
				if parseErr != nil {
					response.Fatal(rsp, errors.Wrapf(parseErr, "invalid spec.sequencingTTL %q", in.Spec.SequencingTTL))
					return rsp, nil
				}
				seqTTLDuration = parsed
			}
			seqTTLSeconds := int(seqTTLDuration.Seconds())

			seq := builtins.NewSequencer(deps, resourceNames, observedNames, seqTTLSeconds)
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
			}

			// Append sequencing events to condCollector for response emission.
			for _, e := range result.Events {
				condCollector.AddEvent(e)
			}
		}

		// Apply collected resources to response (merges with prior desired state).
		if err := builtins.ApplyResources(rsp, collector); err != nil {
			response.Fatal(rsp, errors.Wrapf(err, "applying composed resources"))
			return rsp, nil
		}
		metrics.ResourcesEmittedTotal.WithLabelValues(filename).Add(float64(len(collector.Resources())))

		// Apply dxr status changes to response desired composite.
		if err := builtins.ApplyDXR(rsp, globals["dxr"]); err != nil {
			response.Fatal(rsp, errors.Wrapf(err, "applying dxr status"))
			return rsp, nil
		}

		// Apply pipeline context changes.
		if err := builtins.ApplyContext(rsp, globals["context"]); err != nil {
			response.Fatal(rsp, errors.Wrapf(err, "applying context"))
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

// buildKeychain creates an authn.Keychain that includes Docker config secret
// credentials (if specified) and falls back to the default keychain.
//
// When dockerConfigSecret is set, it sets DOCKER_CONFIG to the expected mount
// path so the default keychain picks up the credentials. The secret should be
// mounted at /var/run/secrets/docker/<secret-name>/ containing a config.json.
func buildKeychain(dockerConfigSecret string) authn.Keychain {
	if dockerConfigSecret != "" {
		// Docker config mounted via DeploymentRuntimeConfig.
		// Standard mount path: /var/run/secrets/docker/<secret-name>/
		// The directory should contain a config.json (or .dockerconfigjson
		// renamed to config.json via the mount spec).
		configDir := filepath.Join("/var/run/secrets/docker", dockerConfigSecret)
		if _, statErr := os.Stat(configDir); statErr == nil {
			// Set DOCKER_CONFIG so authn.DefaultKeychain reads from this path.
			_ = os.Setenv("DOCKER_CONFIG", configDir) //nolint:errcheck
		}
	}
	return authn.DefaultKeychain
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
	data, err := os.ReadFile(path)
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
