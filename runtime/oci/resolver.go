package oci

import (
	"archive/tar"
	"context"
	"fmt"
	"io"
	"path/filepath"
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
	maxFileCount = 100

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
	cache    *Cache
	keychain authn.Keychain
	fetcher  Fetcher
	log      logging.Logger
}

// NewResolver creates a Resolver with the given cache, keychain, fetcher, and logger.
func NewResolver(cache *Cache, keychain authn.Keychain, fetcher Fetcher, log logging.Logger) *Resolver {
	return &Resolver{
		cache:    cache,
		keychain: keychain,
		fetcher:  fetcher,
		log:      log,
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
			result[t.File] = src
		}

		// Scan extracted files for transitive OCI loads.
		for _, src := range files {
			transitive, err := ScanForOCILoads(src, nil)
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

	// Parse the OCI reference for go-containerregistry.
	ref, err := name.ParseReference(refStr, name.StrictValidation)
	if err != nil {
		return nil, fmt.Errorf("parsing OCI reference %q: %w", refStr, err)
	}

	// Fetch the image.
	img, err := r.fetcher.Fetch(ref, r.keychain)
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

	if string(manifest.Config.MediaType) != ArtifactMediaType {
		return nil, fmt.Errorf(
			"unexpected artifact type %q for %s; expected %q for Starlark module bundles",
			manifest.Config.MediaType, refStr, ArtifactMediaType,
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

	// Validate layer media type.
	mt, err := layers[0].MediaType()
	if err != nil {
		return nil, fmt.Errorf("getting layer media type for %s: %w", refStr, err)
	}
	if string(mt) != LayerMediaType {
		return nil, fmt.Errorf(
			"unexpected layer media type %q for %s; expected %q",
			mt, refStr, LayerMediaType,
		)
	}

	// Extract tar contents.
	rc, err := layers[0].Uncompressed()
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

		// Skip non-regular files (symlinks, directories, etc.).
		if hdr.Typeflag != tar.TypeReg {
			continue
		}

		// Flatten to base name.
		baseName := filepath.Base(hdr.Name)

		// Skip non-.star files.
		if !strings.HasSuffix(baseName, ".star") {
			continue
		}

		// Validate filename: no path traversal.
		if strings.Contains(hdr.Name, "..") {
			return nil, fmt.Errorf("path traversal detected in tar entry %q from %s", hdr.Name, refStr)
		}

		// Enforce file count limit.
		fileCount++
		if fileCount > maxFileCount {
			return nil, fmt.Errorf("OCI artifact %s exceeds maximum file count (%d)", refStr, maxFileCount)
		}

		// Read file content with size limit.
		data, err := io.ReadAll(io.LimitReader(tr, maxFileSize+1))
		if err != nil {
			return nil, fmt.Errorf("reading %s from tar in %s: %w", baseName, refStr, err)
		}
		if len(data) > maxFileSize {
			return nil, fmt.Errorf("file %s in %s exceeds maximum size (%d bytes)", baseName, refStr, maxFileSize)
		}

		files[baseName] = string(data)
	}

	return files, nil
}
