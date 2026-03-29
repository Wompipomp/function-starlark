package oci

import (
	"archive/tar"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/crossplane/function-sdk-go/logging"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

const (
	// ArtifactMediaType is the expected config media type for Starlark module bundles.
	ArtifactMediaType = "application/vnd.fn-starlark.modules.v1+tar"

	// LayerMediaType is the expected media type for Starlark module tar layers.
	LayerMediaType = "application/vnd.fn-starlark.layer.v1.tar"

	// maxFileSize is the maximum size of a single .star file in bytes (1 MB).
	maxFileSize = 1 << 20

	// maxFileCount is the maximum number of .star files in a single artifact.
	maxFileCount = 1000

	// maxTransitiveDepth is the maximum depth for transitive OCI dependency resolution.
	maxTransitiveDepth = 10
)

// Fetcher abstracts OCI image fetching for testability.
type Fetcher interface {
	Fetch(ref name.Reference, keychain authn.Keychain) (v1.Image, error)
}

// RemoteFetcher is the default Fetcher that pulls from remote registries.
type RemoteFetcher struct{}

// Fetch pulls an image from a remote registry using the given keychain.
func (RemoteFetcher) Fetch(ref name.Reference, keychain authn.Keychain) (v1.Image, error) {
	return remote.Image(ref, remote.WithAuthFromKeychain(keychain))
}

// Resolver fetches OCI artifacts, validates media types, extracts .star files,
// and resolves transitive OCI dependencies.
type Resolver struct {
	cache           *Cache
	keychain        authn.Keychain
	fetcher         Fetcher
	log             logging.Logger
	defaultRegistry string
	insecureRegs    map[string]bool
}

// NewResolver creates a Resolver with the given cache, keychain, fetcher, logger,
// default registry, and list of insecure (HTTP-only) registries.
func NewResolver(cache *Cache, keychain authn.Keychain, fetcher Fetcher, log logging.Logger, defaultRegistry string, insecureRegistries []string) *Resolver {
	insecure := make(map[string]bool, len(insecureRegistries))
	for _, r := range insecureRegistries {
		insecure[r] = true
	}
	return &Resolver{
		cache:           cache,
		keychain:        keychain,
		fetcher:         fetcher,
		log:             log,
		defaultRegistry: defaultRegistry,
		insecureRegs:    insecure,
	}
}

// Resolve fetches and extracts .star files for the given OCI load targets.
// It deduplicates targets by RefStr, checks the cache first, and resolves
// transitive OCI dependencies found in fetched modules.
//
// Returns a map of filename -> source for all resolved modules.
func (r *Resolver) Resolve(ctx context.Context, targets []*OCILoadTarget) (map[string]string, error) {
	visited := make(map[string]bool)
	return r.resolveRecursive(ctx, targets, visited, 0)
}

// resolveRecursive handles the recursive resolution of OCI targets with
// cycle detection and depth limiting.
func (r *Resolver) resolveRecursive(ctx context.Context, targets []*OCILoadTarget, visited map[string]bool, depth int) (map[string]string, error) {
	if depth > maxTransitiveDepth {
		return nil, fmt.Errorf("OCI dependency resolution exceeded maximum depth (%d)", maxTransitiveDepth)
	}

	// Deduplicate targets by RefStr.
	uniqueRefs := make(map[string][]*OCILoadTarget)
	var refOrder []string
	for _, t := range targets {
		if _, exists := uniqueRefs[t.RefStr]; !exists {
			refOrder = append(refOrder, t.RefStr)
		}
		uniqueRefs[t.RefStr] = append(uniqueRefs[t.RefStr], t)
	}

	result := make(map[string]string)
	var transitiveTargets []*OCILoadTarget

	for _, refStr := range refOrder {
		refTargets := uniqueRefs[refStr]

		// Check for cycle.
		if visited[refStr] {
			return nil, fmt.Errorf("OCI dependency cycle detected: %s", refStr)
		}
		visited[refStr] = true

		// Try to resolve from cache or fetch.
		files, err := r.resolveRef(ctx, refStr, refTargets[0])
		if err != nil {
			return nil, err
		}

		// Map requested files into result.
		for _, t := range refTargets {
			src, ok := files[t.File]
			if !ok {
				available := make([]string, 0, len(files))
				for k := range files {
					available = append(available, k)
				}
				return nil, fmt.Errorf(
					"file %q not found in OCI artifact %s; available files: %s",
					t.File, refStr, strings.Join(available, ", "),
				)
			}
			result[t.RawURL] = src
		}

		// Scan extracted files for transitive OCI loads.
		for _, src := range files {
			transitive, err := ScanForOCILoads(src, nil, r.defaultRegistry)
			if err != nil {
				// Non-fatal: if scanning fails (e.g., invalid Starlark syntax
				// in a module that won't be loaded), we skip it. The actual
				// execution will catch the error.
				r.log.Debug("skipping transitive scan of OCI module", "error", err)
				continue
			}
			for _, t := range transitive {
				if visited[t.RefStr] {
					return nil, fmt.Errorf("OCI dependency cycle detected: %s -> %s", refStr, t.RefStr)
				}
				transitiveTargets = append(transitiveTargets, t)
			}
		}
	}

	// Resolve transitive dependencies.
	if len(transitiveTargets) > 0 {
		transResult, err := r.resolveRecursive(ctx, transitiveTargets, visited, depth+1)
		if err != nil {
			return nil, err
		}
		for k, v := range transResult {
			result[k] = v
		}
	}

	return result, nil
}

// resolveRef resolves a single OCI reference, checking cache first.
func (r *Resolver) resolveRef(ctx context.Context, refStr string, target *OCILoadTarget) (map[string]string, error) {
	// Check tag cache.
	files, fresh := r.cache.GetByTag(refStr)
	if fresh {
		r.log.Debug("OCI cache hit", "ref", refStr)
		return files, nil
	}

	// Check digest cache for digest-pinned references.
	if target.Digest != "" {
		if df, ok := r.cache.GetByDigest(target.Digest); ok {
			r.log.Debug("OCI digest cache hit", "digest", target.Digest)
			return df, nil
		}
	}

	// Parse the OCI reference first to get the canonical registry name.
	ref, err := name.ParseReference(refStr, name.StrictValidation)
	if err != nil {
		return nil, fmt.Errorf("parsing OCI reference %q: %w", refStr, err)
	}

	// Check if this registry is in the insecure list. Use the canonical
	// registry name from the parsed reference to handle normalization
	// (e.g. docker.io vs index.docker.io).
	insecure := r.insecureRegs[ref.Context().RegistryStr()]
	if insecure {
		// Re-parse with name.Insecure to allow plain HTTP connections.
		ref, err = name.ParseReference(refStr, name.StrictValidation, name.Insecure)
		if err != nil {
			return nil, fmt.Errorf("parsing insecure OCI reference %q: %w", refStr, err)
		}
	}

	// Fetch the image. For insecure registries, use an empty keychain
	// (resolves to anonymous) to avoid sending credentials over plaintext HTTP.
	var img v1.Image
	if insecure {
		img, err = r.fetcher.Fetch(ref, authn.NewMultiKeychain())
	} else {
		img, err = r.fetcher.Fetch(ref, r.keychain)
	}
	if err != nil {
		// If we have stale content, serve it with a warning.
		if files != nil {
			r.log.Info("serving stale OCI cache content", "ref", refStr, "error", err)
			return files, nil
		}
		return nil, fmt.Errorf("fetching OCI artifact %s: %w", refStr, err)
	}

	// Validate and extract.
	extracted, err := r.extractStarFiles(img, refStr)
	if err != nil {
		return nil, err
	}

	// Get digest for cache storage.
	digest, err := img.Digest()
	if err != nil {
		return nil, fmt.Errorf("getting digest for %s: %w", refStr, err)
	}

	// Store in cache.
	digestStr := digest.String()
	r.cache.PutContent(digestStr, extracted)
	r.cache.PutTag(refStr, digestStr)

	return extracted, nil
}

// extractStarFiles validates the OCI artifact and extracts .star files from
// the tar layer.
func (r *Resolver) extractStarFiles(img v1.Image, refStr string) (map[string]string, error) {
	// Validate artifact type via config media type.
	manifest, err := img.Manifest()
	if err != nil {
		return nil, fmt.Errorf("reading manifest for %s: %w", refStr, err)
	}

	// Check artifactType at the manifest level (OCI 1.1+), then fall back to
	// config.mediaType for backwards compatibility with older oras versions.
	artifactType := string(manifest.Config.MediaType)
	rawManifest, err := img.RawManifest()
	if err == nil {
		var raw struct {
			ArtifactType string `json:"artifactType"`
		}
		if json.Unmarshal(rawManifest, &raw) == nil && raw.ArtifactType != "" {
			artifactType = raw.ArtifactType
		}
	}
	if artifactType != ArtifactMediaType {
		return nil, fmt.Errorf(
			"unexpected artifact type %q for %s; expected %q for Starlark module bundles",
			artifactType, refStr, ArtifactMediaType,
		)
	}

	// Get layers.
	layers, err := img.Layers()
	if err != nil {
		return nil, fmt.Errorf("getting layers for %s: %w", refStr, err)
	}
	if len(layers) == 0 {
		return nil, fmt.Errorf("OCI artifact %s has no layers", refStr)
	}

	// Extract .star files from layers. oras pushes each file as a separate
	// layer with the filename in the org.opencontainers.image.title annotation.
	// We also support a single tar layer (custom media type) for backwards compat.
	files := make(map[string]string)

	if len(layers) == 1 && r.isTarLayer(layers[0]) {
		// Single tar layer: extract all .star files from the tar archive.
		extracted, err := r.extractFromTar(layers[0], refStr)
		if err != nil {
			return nil, err
		}
		return extracted, nil
	}

	// Multiple layers or standard OCI layer type: each layer is a raw file.
	for i, layer := range layers {
		if len(files) >= maxFileCount {
			return nil, fmt.Errorf("OCI artifact %s exceeds maximum file count (%d)", refStr, maxFileCount)
		}

		// Get filename from annotation.
		desc, err := r.layerDescriptor(img, i)
		if err != nil {
			return nil, fmt.Errorf("getting layer descriptor for %s: %w", refStr, err)
		}
		title := desc.Annotations["org.opencontainers.image.title"]
		if title == "" {
			continue
		}
		if strings.Contains(title, "..") {
			return nil, fmt.Errorf("path traversal detected in layer title %q from %s", title, refStr)
		}
		cleaned := path.Clean(title)
		if !strings.HasSuffix(path.Base(cleaned), ".star") {
			continue
		}

		// Read raw layer content.
		rc, err := layer.Uncompressed()
		if err != nil {
			return nil, fmt.Errorf("reading layer %d for %s: %w", i, refStr, err)
		}
		data, err := io.ReadAll(io.LimitReader(rc, maxFileSize+1))
		rc.Close() //nolint:errcheck,gosec
		if err != nil {
			return nil, fmt.Errorf("reading %s from %s: %w", cleaned, refStr, err)
		}
		if len(data) > maxFileSize {
			return nil, fmt.Errorf("file %s in %s exceeds maximum size (%d bytes)", cleaned, refStr, maxFileSize)
		}

		files[cleaned] = string(data)
	}

	return files, nil
}

// isTarLayer returns true if the layer has the custom Starlark tar media type.
func (r *Resolver) isTarLayer(layer v1.Layer) bool {
	mt, err := layer.MediaType()
	if err != nil {
		return false
	}
	return string(mt) == LayerMediaType
}

// layerDescriptor returns the descriptor for the layer at the given index
// by reading it from the image manifest.
func (r *Resolver) layerDescriptor(img v1.Image, index int) (v1.Descriptor, error) {
	manifest, err := img.Manifest()
	if err != nil {
		return v1.Descriptor{}, err
	}
	if index >= len(manifest.Layers) {
		return v1.Descriptor{}, fmt.Errorf("layer index %d out of range", index)
	}
	return manifest.Layers[index], nil
}

// extractFromTar extracts .star files from a single tar layer.
func (r *Resolver) extractFromTar(layer v1.Layer, refStr string) (map[string]string, error) {
	rc, err := layer.Uncompressed()
	if err != nil {
		return nil, fmt.Errorf("reading layer for %s: %w", refStr, err)
	}
	defer rc.Close() //nolint:errcheck

	files := make(map[string]string)
	tr := tar.NewReader(rc)
	fileCount := 0

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("reading tar from %s: %w", refStr, err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		if strings.Contains(hdr.Name, "..") {
			return nil, fmt.Errorf("path traversal detected in tar entry %q from %s", hdr.Name, refStr)
		}
		cleaned := strings.TrimPrefix(path.Clean(hdr.Name), "/")
		if !strings.HasSuffix(path.Base(cleaned), ".star") {
			continue
		}
		fileCount++
		if fileCount > maxFileCount {
			return nil, fmt.Errorf("OCI artifact %s exceeds maximum file count (%d)", refStr, maxFileCount)
		}
		data, err := io.ReadAll(io.LimitReader(tr, maxFileSize+1))
		if err != nil {
			return nil, fmt.Errorf("reading %s from tar in %s: %w", cleaned, refStr, err)
		}
		if len(data) > maxFileSize {
			return nil, fmt.Errorf("file %s in %s exceeds maximum size (%d bytes)", cleaned, refStr, maxFileSize)
		}
		files[cleaned] = string(data)
	}

	return files, nil
}
