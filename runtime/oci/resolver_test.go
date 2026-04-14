package oci

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/crossplane/function-sdk-go/logging"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/google/go-containerregistry/pkg/v1/types"
)

// mockFetcher implements Fetcher for testing.
type mockFetcher struct {
	images    map[string]v1.Image
	calls     int
	headCalls int
	err       error
	keychains []authn.Keychain // records keychains passed to each Fetch call
}

func (m *mockFetcher) Fetch(ref name.Reference, kc authn.Keychain) (v1.Image, error) {
	m.calls++
	m.keychains = append(m.keychains, kc)
	if m.err != nil {
		return nil, m.err
	}
	img, ok := m.images[ref.String()]
	if !ok {
		return nil, fmt.Errorf("image not found: %s", ref.String())
	}
	return img, nil
}

func (m *mockFetcher) Head(ref name.Reference, _ authn.Keychain) (*v1.Descriptor, error) {
	m.headCalls++
	if m.err != nil {
		return nil, m.err
	}
	img, ok := m.images[ref.String()]
	if !ok {
		return nil, fmt.Errorf("image not found: %s", ref.String())
	}
	digest, err := img.Digest()
	if err != nil {
		return nil, err
	}
	return &v1.Descriptor{Digest: digest}, nil
}

// buildTar creates a tar archive from a map of filename -> content.
func buildTar(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for name, content := range files {
		hdr := &tar.Header{
			Name:     name,
			Mode:     0o644,
			Size:     int64(len(content)),
			Typeflag: tar.TypeReg,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("writing tar header for %s: %v", name, err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatalf("writing tar content for %s: %v", name, err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("closing tar writer: %v", err)
	}
	return buf.Bytes()
}

// buildTestImage creates an in-memory OCI image with a tar layer containing
// the given files. Uses the specified artifact and layer media types.
func buildTestImage(t *testing.T, files map[string]string, artifactType, layerType string) v1.Image {
	t.Helper()

	tarData := buildTar(t, files)

	layer, err := tarball.LayerFromOpener(func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(tarData)), nil
	}, tarball.WithMediaType(types.MediaType(layerType)))
	if err != nil {
		t.Fatalf("creating layer: %v", err)
	}

	img := mutate.MediaType(empty.Image, types.OCIManifestSchema1)
	img = mutate.ConfigMediaType(img, types.MediaType(artifactType))
	img, err = mutate.AppendLayers(img, layer)
	if err != nil {
		t.Fatalf("appending layer: %v", err)
	}

	return img
}

// artifactTypeImage wraps a v1.Image to inject artifactType into the raw manifest JSON,
// since go-containerregistry v0.21.2 doesn't expose ArtifactType on the Manifest struct.
type artifactTypeImage struct {
	v1.Image
	artifactType string
}

func (a *artifactTypeImage) RawManifest() ([]byte, error) {
	raw, err := a.Image.RawManifest()
	if err != nil {
		return nil, err
	}
	var m map[string]interface{}
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	m["artifactType"] = a.artifactType
	return json.Marshal(m)
}

// buildOrasImage creates an in-memory OCI image mimicking oras push output:
// each file is a separate raw layer (not tar) with the filename in the
// org.opencontainers.image.title annotation. Config is application/vnd.oci.empty.v1+json.
// artifactType is injected at the manifest level via RawManifest override.
func buildOrasImage(t *testing.T, files map[string]string) v1.Image {
	t.Helper()

	img := mutate.MediaType(empty.Image, types.OCIManifestSchema1)
	img = mutate.ConfigMediaType(img, "application/vnd.oci.empty.v1+json")

	for fileName, content := range files {
		fileContent := content // capture for closure
		layer, err := tarball.LayerFromOpener(func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader([]byte(fileContent))), nil
		}, tarball.WithMediaType("application/vnd.oci.image.layer.v1.tar"))
		if err != nil {
			t.Fatalf("creating layer for %s: %v", fileName, err)
		}

		img, err = mutate.Append(img, mutate.Addendum{
			Layer: layer,
			Annotations: map[string]string{
				"org.opencontainers.image.title": fileName,
			},
		})
		if err != nil {
			t.Fatalf("appending layer %s: %v", fileName, err)
		}
	}

	return &artifactTypeImage{Image: img, artifactType: ArtifactMediaType}
}

func TestResolveOrasPerFileLayers(t *testing.T) {
	c := NewCache(5 * time.Minute)
	img := buildOrasImage(t, map[string]string{
		"naming.star":     `def resource_name(key): return key`,
		"networking.star": `def cidr(prefix, bits): return prefix`,
		"labels.star":     `def standard_labels(): return {}`,
		"conditions.star": `def ready(): return True`,
	})

	f := &mockFetcher{images: map[string]v1.Image{
		"ghcr.io/wompipomp/starlark-stdlib:v1": img,
	}}
	r := NewResolver(c, authn.DefaultKeychain, f, logging.NewNopLogger(), "", nil)

	target, err := ParseOCILoadTarget("oci://ghcr.io/wompipomp/starlark-stdlib:v1/naming.star")
	if err != nil {
		t.Fatal(err)
	}

	result, err := r.Resolve(context.Background(), []*OCILoadTarget{target})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["oci://ghcr.io/wompipomp/starlark-stdlib:v1/naming.star"] != `def resource_name(key): return key` {
		t.Errorf("got %q, want resource_name function", result["oci://ghcr.io/wompipomp/starlark-stdlib:v1/naming.star"])
	}
}

func TestResolveFromCache(t *testing.T) {
	c := NewCache(5 * time.Minute)
	c.PutContent("sha256:abc", map[string]string{"helpers.star": "x = 1"})
	c.PutTag("ghcr.io/org/lib:v1", "sha256:abc")

	f := &mockFetcher{images: map[string]v1.Image{}}
	r := NewResolver(c, authn.DefaultKeychain, f, logging.NewNopLogger(), "", nil)

	target, err := ParseOCILoadTarget("oci://ghcr.io/org/lib:v1/helpers.star")
	if err != nil {
		t.Fatal(err)
	}

	result, err := r.Resolve(context.Background(), []*OCILoadTarget{target})
	if err != nil {
		t.Fatal(err)
	}
	if result["oci://ghcr.io/org/lib:v1/helpers.star"] != "x = 1" {
		t.Errorf("got %q, want %q", result["oci://ghcr.io/org/lib:v1/helpers.star"], "x = 1")
	}
	if f.calls != 0 {
		t.Errorf("expected 0 fetch calls (cache hit), got %d", f.calls)
	}
}

func TestResolveFetchAndExtract(t *testing.T) {
	c := NewCache(5 * time.Minute)
	img := buildTestImage(t, map[string]string{
		"helpers.star": `helper = "loaded"`,
		"utils.star":   `util = "loaded"`,
	}, ArtifactMediaType, LayerMediaType)

	f := &mockFetcher{images: map[string]v1.Image{
		"ghcr.io/org/lib:v1": img,
	}}
	r := NewResolver(c, authn.DefaultKeychain, f, logging.NewNopLogger(), "", nil)

	target, err := ParseOCILoadTarget("oci://ghcr.io/org/lib:v1/helpers.star")
	if err != nil {
		t.Fatal(err)
	}

	result, err := r.Resolve(context.Background(), []*OCILoadTarget{target})
	if err != nil {
		t.Fatal(err)
	}
	if result["oci://ghcr.io/org/lib:v1/helpers.star"] != `helper = "loaded"` {
		t.Errorf("got %q, want %q", result["oci://ghcr.io/org/lib:v1/helpers.star"], `helper = "loaded"`)
	}
	if f.calls != 1 {
		t.Errorf("expected 1 fetch call, got %d", f.calls)
	}
}

func TestResolveWrongArtifactType(t *testing.T) {
	c := NewCache(5 * time.Minute)
	img := buildTestImage(t, map[string]string{
		"helpers.star": `x = 1`,
	}, "application/vnd.wrong.type", LayerMediaType)

	f := &mockFetcher{images: map[string]v1.Image{
		"ghcr.io/org/lib:v1": img,
	}}
	r := NewResolver(c, authn.DefaultKeychain, f, logging.NewNopLogger(), "", nil)

	target, err := ParseOCILoadTarget("oci://ghcr.io/org/lib:v1/helpers.star")
	if err != nil {
		t.Fatal(err)
	}

	_, err = r.Resolve(context.Background(), []*OCILoadTarget{target})
	if err == nil {
		t.Fatal("expected error for wrong artifact type")
	}
	if !strings.Contains(err.Error(), "artifact type") {
		t.Errorf("error %q should mention artifact type", err.Error())
	}
}

func TestResolveWrongLayerType(t *testing.T) {
	c := NewCache(5 * time.Minute)
	img := buildTestImage(t, map[string]string{
		"helpers.star": `x = 1`,
	}, ArtifactMediaType, "application/vnd.wrong.layer")

	f := &mockFetcher{images: map[string]v1.Image{
		"ghcr.io/org/lib:v1": img,
	}}
	r := NewResolver(c, authn.DefaultKeychain, f, logging.NewNopLogger(), "", nil)

	target, err := ParseOCILoadTarget("oci://ghcr.io/org/lib:v1/helpers.star")
	if err != nil {
		t.Fatal(err)
	}

	// With unknown layer type and no annotations, the file won't be found.
	_, err = r.Resolve(context.Background(), []*OCILoadTarget{target})
	if err == nil {
		t.Fatal("expected error for wrong layer type")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error %q should mention file not found", err.Error())
	}
}

func TestResolveDeduplicatesSameRef(t *testing.T) {
	c := NewCache(5 * time.Minute)
	img := buildTestImage(t, map[string]string{
		"a.star": `a = 1`,
		"b.star": `b = 2`,
	}, ArtifactMediaType, LayerMediaType)

	f := &mockFetcher{images: map[string]v1.Image{
		"ghcr.io/org/lib:v1": img,
	}}
	r := NewResolver(c, authn.DefaultKeychain, f, logging.NewNopLogger(), "", nil)

	t1, _ := ParseOCILoadTarget("oci://ghcr.io/org/lib:v1/a.star")
	t2, _ := ParseOCILoadTarget("oci://ghcr.io/org/lib:v1/b.star")

	result, err := r.Resolve(context.Background(), []*OCILoadTarget{t1, t2})
	if err != nil {
		t.Fatal(err)
	}
	if result["oci://ghcr.io/org/lib:v1/a.star"] != "a = 1" {
		t.Errorf("a.star = %q, want %q", result["oci://ghcr.io/org/lib:v1/a.star"], "a = 1")
	}
	if result["oci://ghcr.io/org/lib:v1/b.star"] != "b = 2" {
		t.Errorf("b.star = %q, want %q", result["oci://ghcr.io/org/lib:v1/b.star"], "b = 2")
	}
	if f.calls != 1 {
		t.Errorf("expected 1 fetch call (deduplication), got %d", f.calls)
	}
}

func TestResolveEmptyLayers(t *testing.T) {
	c := NewCache(5 * time.Minute)

	// Create an image with no layers.
	img := mutate.MediaType(empty.Image, types.OCIManifestSchema1)
	img = mutate.ConfigMediaType(img, types.MediaType(ArtifactMediaType))

	f := &mockFetcher{images: map[string]v1.Image{
		"ghcr.io/org/lib:v1": img,
	}}
	r := NewResolver(c, authn.DefaultKeychain, f, logging.NewNopLogger(), "", nil)

	target, _ := ParseOCILoadTarget("oci://ghcr.io/org/lib:v1/h.star")
	_, err := r.Resolve(context.Background(), []*OCILoadTarget{target})
	if err == nil {
		t.Fatal("expected error for empty layers")
	}
	if !strings.Contains(err.Error(), "no layers") {
		t.Errorf("error %q should mention no layers", err.Error())
	}
}

func TestResolveFileNotInArtifact(t *testing.T) {
	c := NewCache(5 * time.Minute)
	img := buildTestImage(t, map[string]string{
		"other.star": `x = 1`,
	}, ArtifactMediaType, LayerMediaType)

	f := &mockFetcher{images: map[string]v1.Image{
		"ghcr.io/org/lib:v1": img,
	}}
	r := NewResolver(c, authn.DefaultKeychain, f, logging.NewNopLogger(), "", nil)

	target, _ := ParseOCILoadTarget("oci://ghcr.io/org/lib:v1/helpers.star")
	_, err := r.Resolve(context.Background(), []*OCILoadTarget{target})
	if err == nil {
		t.Fatal("expected error for missing file in artifact")
	}
	if !strings.Contains(err.Error(), "helpers.star") {
		t.Errorf("error %q should mention missing file", err.Error())
	}
}

func TestResolveTransitiveDeps(t *testing.T) {
	c := NewCache(5 * time.Minute)

	// Module A loads module B from another OCI ref.
	imgA := buildTestImage(t, map[string]string{
		"a.star": `load("oci://ghcr.io/org/dep:v1/b.star", "b_fn")
a_fn = lambda: b_fn()`,
	}, ArtifactMediaType, LayerMediaType)

	imgB := buildTestImage(t, map[string]string{
		"b.star": `b_fn = lambda: "hello"`,
	}, ArtifactMediaType, LayerMediaType)

	f := &mockFetcher{images: map[string]v1.Image{
		"ghcr.io/org/lib:v1": imgA,
		"ghcr.io/org/dep:v1": imgB,
	}}
	r := NewResolver(c, authn.DefaultKeychain, f, logging.NewNopLogger(), "", nil)

	target, _ := ParseOCILoadTarget("oci://ghcr.io/org/lib:v1/a.star")
	result, err := r.Resolve(context.Background(), []*OCILoadTarget{target})
	if err != nil {
		t.Fatal(err)
	}

	// Both a.star and b.star should be resolved (keyed by full OCI URL).
	if _, ok := result["oci://ghcr.io/org/lib:v1/a.star"]; !ok {
		t.Error("expected a.star in result")
	}
	if _, ok := result["oci://ghcr.io/org/dep:v1/b.star"]; !ok {
		t.Error("expected b.star in result (transitive dep)")
	}
}

func TestResolveCycleDetection(t *testing.T) {
	c := NewCache(5 * time.Minute)

	// Module A loads B, module B loads A.
	imgA := buildTestImage(t, map[string]string{
		"a.star": `load("oci://ghcr.io/org/b:v1/b.star", "b_fn")`,
	}, ArtifactMediaType, LayerMediaType)

	imgB := buildTestImage(t, map[string]string{
		"b.star": `load("oci://ghcr.io/org/a:v1/a.star", "a_fn")`,
	}, ArtifactMediaType, LayerMediaType)

	f := &mockFetcher{images: map[string]v1.Image{
		"ghcr.io/org/a:v1": imgA,
		"ghcr.io/org/b:v1": imgB,
	}}
	r := NewResolver(c, authn.DefaultKeychain, f, logging.NewNopLogger(), "", nil)

	target, _ := ParseOCILoadTarget("oci://ghcr.io/org/a:v1/a.star")
	_, err := r.Resolve(context.Background(), []*OCILoadTarget{target})
	if err == nil {
		t.Fatal("expected error for dependency cycle")
	}
	if !strings.Contains(err.Error(), "cycle") {
		t.Errorf("error %q should mention cycle", err.Error())
	}
}

func TestResolveStaleServing(t *testing.T) {
	now := time.Now()
	c := NewCache(5 * time.Minute)
	c.nowFn = func() time.Time { return now }

	// Populate cache with content.
	c.PutContent("sha256:abc", map[string]string{"h.star": "x = 1"})
	c.PutTag("ghcr.io/org/lib:v1", "sha256:abc")

	// Expire the tag.
	c.nowFn = func() time.Time { return now.Add(10 * time.Minute) }

	// Registry is unreachable.
	f := &mockFetcher{err: fmt.Errorf("connection refused")}
	r := NewResolver(c, authn.DefaultKeychain, f, logging.NewNopLogger(), "", nil)

	target, _ := ParseOCILoadTarget("oci://ghcr.io/org/lib:v1/h.star")
	result, err := r.Resolve(context.Background(), []*OCILoadTarget{target})
	if err != nil {
		t.Fatalf("expected stale serving, got error: %v", err)
	}
	if result["oci://ghcr.io/org/lib:v1/h.star"] != "x = 1" {
		t.Errorf("got %q, want %q", result["oci://ghcr.io/org/lib:v1/h.star"], "x = 1")
	}
}

func TestResolveColdMissFails(t *testing.T) {
	c := NewCache(5 * time.Minute)

	// Registry is unreachable, cache is empty.
	f := &mockFetcher{err: fmt.Errorf("connection refused")}
	r := NewResolver(c, authn.DefaultKeychain, f, logging.NewNopLogger(), "", nil)

	target, _ := ParseOCILoadTarget("oci://ghcr.io/org/lib:v1/h.star")
	_, err := r.Resolve(context.Background(), []*OCILoadTarget{target})
	if err == nil {
		t.Fatal("expected error for cold cache + unreachable registry")
	}
}

func TestResolveTarSafety(t *testing.T) {
	c := NewCache(5 * time.Minute)

	// Build tar with path traversal attempt.
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	hdr := &tar.Header{
		Name:     "../../../etc/passwd.star",
		Mode:     0o644,
		Size:     5,
		Typeflag: tar.TypeReg,
	}
	_ = tw.WriteHeader(hdr)
	_, _ = tw.Write([]byte("x = 1"))
	_ = tw.Close()

	tarBytes := buf.Bytes()
	layer, err := tarball.LayerFromOpener(func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(tarBytes)), nil
	}, tarball.WithMediaType(types.MediaType(LayerMediaType)))
	if err != nil {
		t.Fatal(err)
	}

	img := mutate.MediaType(empty.Image, types.OCIManifestSchema1)
	img = mutate.ConfigMediaType(img, types.MediaType(ArtifactMediaType))
	img, err = mutate.AppendLayers(img, layer)
	if err != nil {
		t.Fatal(err)
	}

	f := &mockFetcher{images: map[string]v1.Image{
		"ghcr.io/org/lib:v1": img,
	}}
	r := NewResolver(c, authn.DefaultKeychain, f, logging.NewNopLogger(), "", nil)

	target, _ := ParseOCILoadTarget("oci://ghcr.io/org/lib:v1/passwd.star")
	_, err = r.Resolve(context.Background(), []*OCILoadTarget{target})
	if err == nil {
		t.Fatal("expected error for path traversal in tar")
	}
}

func TestResolveSkipsNonStarAndNonRegular(t *testing.T) {
	c := NewCache(5 * time.Minute)

	// Build tar with a non-.star file and a symlink.
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

	// Regular .star file.
	_ = tw.WriteHeader(&tar.Header{Name: "good.star", Mode: 0o644, Size: 5, Typeflag: tar.TypeReg})
	_, _ = tw.Write([]byte("x = 1"))

	// Non-.star file should be skipped.
	_ = tw.WriteHeader(&tar.Header{Name: "readme.md", Mode: 0o644, Size: 6, Typeflag: tar.TypeReg})
	_, _ = tw.Write([]byte("hello!"))

	// Symlink should be skipped.
	_ = tw.WriteHeader(&tar.Header{Name: "link.star", Mode: 0o644, Typeflag: tar.TypeSymlink, Linkname: "good.star"})

	_ = tw.Close()

	symTarBytes := buf.Bytes()
	layer, err := tarball.LayerFromOpener(func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(symTarBytes)), nil
	}, tarball.WithMediaType(types.MediaType(LayerMediaType)))
	if err != nil {
		t.Fatal(err)
	}

	img := mutate.MediaType(empty.Image, types.OCIManifestSchema1)
	img = mutate.ConfigMediaType(img, types.MediaType(ArtifactMediaType))
	img, err = mutate.AppendLayers(img, layer)
	if err != nil {
		t.Fatal(err)
	}

	f := &mockFetcher{images: map[string]v1.Image{
		"ghcr.io/org/lib:v1": img,
	}}
	r := NewResolver(c, authn.DefaultKeychain, f, logging.NewNopLogger(), "", nil)

	target, _ := ParseOCILoadTarget("oci://ghcr.io/org/lib:v1/good.star")
	result, err := r.Resolve(context.Background(), []*OCILoadTarget{target})
	if err != nil {
		t.Fatal(err)
	}
	if result["oci://ghcr.io/org/lib:v1/good.star"] != "x = 1" {
		t.Errorf("good.star = %q, want %q", result["oci://ghcr.io/org/lib:v1/good.star"], "x = 1")
	}
}

func TestResolveUsesKeychain(t *testing.T) {
	c := NewCache(5 * time.Minute)
	img := buildTestImage(t, map[string]string{
		"h.star": `x = 1`,
	}, ArtifactMediaType, LayerMediaType)

	f := &mockFetcher{images: map[string]v1.Image{
		"ghcr.io/org/lib:v1": img,
	}}
	customKC := authn.NewMultiKeychain(authn.DefaultKeychain)
	r := NewResolver(c, customKC, f, logging.NewNopLogger(), "", nil)

	target, _ := ParseOCILoadTarget("oci://ghcr.io/org/lib:v1/h.star")
	result, err := r.Resolve(context.Background(), []*OCILoadTarget{target})
	if err != nil {
		t.Fatal(err)
	}
	if result["oci://ghcr.io/org/lib:v1/h.star"] != "x = 1" {
		t.Errorf("got %q, want %q", result["oci://ghcr.io/org/lib:v1/h.star"], "x = 1")
	}
}

func TestExtractTarNestedPaths(t *testing.T) {
	c := NewCache(5 * time.Minute)
	img := buildTestImage(t, map[string]string{
		"apps/v1.star": `apps_v1 = "apps"`,
		"core/v1.star": `core_v1 = "core"`,
		"meta/v1.star": `meta_v1 = "meta"`,
	}, ArtifactMediaType, LayerMediaType)

	f := &mockFetcher{images: map[string]v1.Image{
		"ghcr.io/org/provider:v1": img,
	}}
	r := NewResolver(c, authn.DefaultKeychain, f, logging.NewNopLogger(), "", nil)

	target, err := ParseOCILoadTarget("oci://ghcr.io/org/provider:v1/apps/v1.star")
	if err != nil {
		t.Fatal(err)
	}

	result, err := r.Resolve(context.Background(), []*OCILoadTarget{target})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["oci://ghcr.io/org/provider:v1/apps/v1.star"] != `apps_v1 = "apps"` {
		t.Errorf("apps/v1.star = %q, want %q", result["oci://ghcr.io/org/provider:v1/apps/v1.star"], `apps_v1 = "apps"`)
	}
}

func TestExtractOrasNestedPaths(t *testing.T) {
	c := NewCache(5 * time.Minute)
	img := buildOrasImage(t, map[string]string{
		"apps/v1.star": `apps_v1 = "apps"`,
		"core/v1.star": `core_v1 = "core"`,
	})

	f := &mockFetcher{images: map[string]v1.Image{
		"ghcr.io/org/provider:v1": img,
	}}
	r := NewResolver(c, authn.DefaultKeychain, f, logging.NewNopLogger(), "", nil)

	target, err := ParseOCILoadTarget("oci://ghcr.io/org/provider:v1/apps/v1.star")
	if err != nil {
		t.Fatal(err)
	}

	result, err := r.Resolve(context.Background(), []*OCILoadTarget{target})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["oci://ghcr.io/org/provider:v1/apps/v1.star"] != `apps_v1 = "apps"` {
		t.Errorf("apps/v1.star = %q, want %q", result["oci://ghcr.io/org/provider:v1/apps/v1.star"], `apps_v1 = "apps"`)
	}
}

func TestExtractHighFileCount(t *testing.T) {
	c := NewCache(5 * time.Minute)

	// Build a tar with 150 .star files — was blocked by maxFileCount=100.
	files := make(map[string]string, 150)
	for i := 0; i < 150; i++ {
		files[fmt.Sprintf("pkg%d/mod.star", i)] = fmt.Sprintf("val = %d", i)
	}
	img := buildTestImage(t, files, ArtifactMediaType, LayerMediaType)

	f := &mockFetcher{images: map[string]v1.Image{
		"ghcr.io/org/provider-aws:v1": img,
	}}
	r := NewResolver(c, authn.DefaultKeychain, f, logging.NewNopLogger(), "", nil)

	target, err := ParseOCILoadTarget("oci://ghcr.io/org/provider-aws:v1/pkg0/mod.star")
	if err != nil {
		t.Fatal(err)
	}

	result, err := r.Resolve(context.Background(), []*OCILoadTarget{target})
	if err != nil {
		t.Fatalf("unexpected error (should not hit maxFileCount): %v", err)
	}
	if result["oci://ghcr.io/org/provider-aws:v1/pkg0/mod.star"] != "val = 0" {
		t.Errorf("pkg0/mod.star = %q, want %q", result["oci://ghcr.io/org/provider-aws:v1/pkg0/mod.star"], "val = 0")
	}
}

func TestExtractNestedPathTraversal(t *testing.T) {
	c := NewCache(5 * time.Minute)
	img := buildTestImage(t, map[string]string{
		"apps/../../etc/passwd.star": `x = 1`,
	}, ArtifactMediaType, LayerMediaType)

	f := &mockFetcher{images: map[string]v1.Image{
		"ghcr.io/org/lib:v1": img,
	}}
	r := NewResolver(c, authn.DefaultKeychain, f, logging.NewNopLogger(), "", nil)

	target, _ := ParseOCILoadTarget("oci://ghcr.io/org/lib:v1/etc/passwd.star")
	_, err := r.Resolve(context.Background(), []*OCILoadTarget{target})
	if err == nil {
		t.Fatal("expected error for path traversal in nested tar path")
	}
	if !strings.Contains(err.Error(), "path traversal") {
		t.Errorf("error %q should mention path traversal", err.Error())
	}
}

func TestResolveTransitivePackageLocal(t *testing.T) {
	c := NewCache(5 * time.Minute)

	// Single artifact with two files. a.star loads its sibling via ./b.star.
	img := buildTestImage(t, map[string]string{
		"a.star": `load("./b.star", "x")
value = x`,
		"b.star": `x = 1`,
	}, ArtifactMediaType, LayerMediaType)

	f := &mockFetcher{images: map[string]v1.Image{
		"ghcr.io/org/mod:v1": img,
	}}
	// defaultRegistry deliberately EMPTY — package-local must not require it.
	r := NewResolver(c, authn.DefaultKeychain, f, logging.NewNopLogger(), "", nil)

	target, _ := ParseOCILoadTarget("oci://ghcr.io/org/mod:v1/a.star")
	result, err := r.Resolve(context.Background(), []*OCILoadTarget{target})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Both files should be in the result map, keyed by full OCI URL.
	if _, ok := result["oci://ghcr.io/org/mod:v1/a.star"]; !ok {
		t.Error("expected a.star in result")
	}
	if _, ok := result["oci://ghcr.io/org/mod:v1/b.star"]; !ok {
		t.Error("expected b.star (package-local sibling) in result")
	}

	// CRITICAL: only ONE fetch call — siblings come from the same artifact.
	if f.calls != 1 {
		t.Errorf("expected 1 fetch call (same-artifact dedup), got %d", f.calls)
	}
}

func TestResolveTransitiveShortForm(t *testing.T) {
	c := NewCache(5 * time.Minute)

	// Module A uses explicit oci:// and contains a short-form load of module B.
	imgA := buildTestImage(t, map[string]string{
		"a.star": `load("dep:v1/b.star", "b_fn")
a_fn = lambda: b_fn()`,
	}, ArtifactMediaType, LayerMediaType)

	// Module B is the transitive dependency that should be discovered
	// via short-form expansion with the resolver's default registry.
	imgB := buildTestImage(t, map[string]string{
		"b.star": `b_fn = lambda: "hello"`,
	}, ArtifactMediaType, LayerMediaType)

	f := &mockFetcher{images: map[string]v1.Image{
		"ghcr.io/org/lib:v1": imgA,
		"ghcr.io/org/dep:v1": imgB,
	}}
	// Key: defaultRegistry is set so short-form "dep:v1/b.star" expands
	// to "oci://ghcr.io/org/dep:v1/b.star" during transitive scanning.
	r := NewResolver(c, authn.DefaultKeychain, f, logging.NewNopLogger(), "ghcr.io/org", nil)

	target, _ := ParseOCILoadTarget("oci://ghcr.io/org/lib:v1/a.star")
	result, err := r.Resolve(context.Background(), []*OCILoadTarget{target})
	if err != nil {
		t.Fatal(err)
	}

	// Both a.star (direct) and b.star (transitive via short-form) should be resolved.
	if _, ok := result["oci://ghcr.io/org/lib:v1/a.star"]; !ok {
		t.Error("expected a.star in result")
	}
	if _, ok := result["oci://ghcr.io/org/dep:v1/b.star"]; !ok {
		t.Error("expected b.star in result (transitive dep via short-form expansion)")
	}
}

func TestResolveInsecureRegistry(t *testing.T) {
	c := NewCache(time.Hour)
	img := buildTestImage(t, map[string]string{"h.star": `val = 1`}, ArtifactMediaType, LayerMediaType)
	f := &mockFetcher{images: map[string]v1.Image{
		"localhost:5050/org/lib:v1": img,
	}}
	customKC := authn.NewMultiKeychain(authn.DefaultKeychain)
	r := NewResolver(c, customKC, f, logging.NewNopLogger(), "", []string{"localhost:5050"})

	target, _ := ParseOCILoadTarget("oci://localhost:5050/org/lib:v1/h.star")
	_, err := r.Resolve(context.Background(), []*OCILoadTarget{target})
	if err != nil {
		t.Fatal(err)
	}

	// Insecure registry should use an empty keychain, not the configured one.
	if len(f.keychains) != 1 {
		t.Fatalf("expected 1 fetch call, got %d", len(f.keychains))
	}
	if f.keychains[0] == customKC {
		t.Error("insecure registry should not use the configured keychain")
	}
}

func TestResolveSecureRegistryUsesKeychain(t *testing.T) {
	c := NewCache(time.Hour)
	img := buildTestImage(t, map[string]string{"h.star": `val = 1`}, ArtifactMediaType, LayerMediaType)
	f := &mockFetcher{images: map[string]v1.Image{
		"ghcr.io/org/lib:v1": img,
	}}
	customKC := authn.NewMultiKeychain(authn.DefaultKeychain)
	// localhost:5050 is insecure, but we're fetching from ghcr.io — should use real keychain.
	r := NewResolver(c, customKC, f, logging.NewNopLogger(), "", []string{"localhost:5050"})

	target, _ := ParseOCILoadTarget("oci://ghcr.io/org/lib:v1/h.star")
	_, err := r.Resolve(context.Background(), []*OCILoadTarget{target})
	if err != nil {
		t.Fatal(err)
	}

	if len(f.keychains) != 1 {
		t.Fatalf("expected 1 fetch call, got %d", len(f.keychains))
	}
	if f.keychains[0] != customKC {
		t.Error("secure registry should use the configured keychain")
	}
}

func TestResolveHeadOptimization(t *testing.T) {
	now := time.Now()
	c := NewCache(5 * time.Minute)
	c.nowFn = func() time.Time { return now }

	// Build image and compute its digest.
	img := buildTestImage(t, map[string]string{
		"h.star": `x = 1`,
	}, ArtifactMediaType, LayerMediaType)
	digest, err := img.Digest()
	if err != nil {
		t.Fatal(err)
	}

	// Pre-populate cache with content at this digest.
	c.PutContent(digest.String(), map[string]string{"h.star": "x = 1"})
	c.PutTag("ghcr.io/org/lib:v1", digest.String())

	// Expire the tag cache.
	c.nowFn = func() time.Time { return now.Add(10 * time.Minute) }

	// Fetcher has the same image (same digest).
	f := &mockFetcher{images: map[string]v1.Image{
		"ghcr.io/org/lib:v1": img,
	}}
	r := NewResolver(c, authn.DefaultKeychain, f, logging.NewNopLogger(), "", nil)

	target, _ := ParseOCILoadTarget("oci://ghcr.io/org/lib:v1/h.star")
	result, err := r.Resolve(context.Background(), []*OCILoadTarget{target})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["oci://ghcr.io/org/lib:v1/h.star"] != "x = 1" {
		t.Errorf("got %q, want %q", result["oci://ghcr.io/org/lib:v1/h.star"], "x = 1")
	}
	// HEAD should be called, but Fetch should NOT (digest unchanged).
	if f.headCalls != 1 {
		t.Errorf("expected 1 HEAD call, got %d", f.headCalls)
	}
	if f.calls != 0 {
		t.Errorf("expected 0 Fetch calls (HEAD optimization), got %d", f.calls)
	}
}

func TestResolveHeadDigestChanged(t *testing.T) {
	now := time.Now()
	c := NewCache(5 * time.Minute)
	c.nowFn = func() time.Time { return now }

	// Pre-populate cache with old content.
	c.PutContent("sha256:old", map[string]string{"h.star": "x = 1"})
	c.PutTag("ghcr.io/org/lib:v1", "sha256:old")

	// Expire the tag cache.
	c.nowFn = func() time.Time { return now.Add(10 * time.Minute) }

	// Registry has a new image (different digest).
	img := buildTestImage(t, map[string]string{
		"h.star": `x = 2`,
	}, ArtifactMediaType, LayerMediaType)

	f := &mockFetcher{images: map[string]v1.Image{
		"ghcr.io/org/lib:v1": img,
	}}
	r := NewResolver(c, authn.DefaultKeychain, f, logging.NewNopLogger(), "", nil)

	target, _ := ParseOCILoadTarget("oci://ghcr.io/org/lib:v1/h.star")
	result, err := r.Resolve(context.Background(), []*OCILoadTarget{target})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should get the NEW content.
	if result["oci://ghcr.io/org/lib:v1/h.star"] != `x = 2` {
		t.Errorf("got %q, want %q", result["oci://ghcr.io/org/lib:v1/h.star"], `x = 2`)
	}
	// Both HEAD and Fetch should be called (digest changed).
	if f.headCalls != 1 {
		t.Errorf("expected 1 HEAD call, got %d", f.headCalls)
	}
	if f.calls != 1 {
		t.Errorf("expected 1 Fetch call (digest changed), got %d", f.calls)
	}
}
