package oci

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
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
	images map[string]v1.Image
	calls  int
	err    error
}

func (m *mockFetcher) Fetch(ref name.Reference, _ authn.Keychain) (v1.Image, error) {
	m.calls++
	if m.err != nil {
		return nil, m.err
	}
	img, ok := m.images[ref.String()]
	if !ok {
		return nil, fmt.Errorf("image not found: %s", ref.String())
	}
	return img, nil
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

	layer, err := tarball.LayerFromReader(bytes.NewReader(tarData), tarball.WithMediaType(types.MediaType(layerType)))
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

func TestResolveFromCache(t *testing.T) {
	c := NewCache(5 * time.Minute)
	c.PutContent("sha256:abc", map[string]string{"helpers.star": "x = 1"})
	c.PutTag("ghcr.io/org/lib:v1", "sha256:abc")

	f := &mockFetcher{images: map[string]v1.Image{}}
	r := NewResolver(c, authn.DefaultKeychain, f, logging.NewNopLogger())

	target, err := ParseOCILoadTarget("oci://ghcr.io/org/lib:v1/helpers.star")
	if err != nil {
		t.Fatal(err)
	}

	result, err := r.Resolve(context.Background(), []*OCILoadTarget{target})
	if err != nil {
		t.Fatal(err)
	}
	if result["helpers.star"] != "x = 1" {
		t.Errorf("got %q, want %q", result["helpers.star"], "x = 1")
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
	r := NewResolver(c, authn.DefaultKeychain, f, logging.NewNopLogger())

	target, err := ParseOCILoadTarget("oci://ghcr.io/org/lib:v1/helpers.star")
	if err != nil {
		t.Fatal(err)
	}

	result, err := r.Resolve(context.Background(), []*OCILoadTarget{target})
	if err != nil {
		t.Fatal(err)
	}
	if result["helpers.star"] != `helper = "loaded"` {
		t.Errorf("got %q, want %q", result["helpers.star"], `helper = "loaded"`)
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
	r := NewResolver(c, authn.DefaultKeychain, f, logging.NewNopLogger())

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
	r := NewResolver(c, authn.DefaultKeychain, f, logging.NewNopLogger())

	target, err := ParseOCILoadTarget("oci://ghcr.io/org/lib:v1/helpers.star")
	if err != nil {
		t.Fatal(err)
	}

	_, err = r.Resolve(context.Background(), []*OCILoadTarget{target})
	if err == nil {
		t.Fatal("expected error for wrong layer type")
	}
	if !strings.Contains(err.Error(), "layer media type") {
		t.Errorf("error %q should mention layer media type", err.Error())
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
	r := NewResolver(c, authn.DefaultKeychain, f, logging.NewNopLogger())

	t1, _ := ParseOCILoadTarget("oci://ghcr.io/org/lib:v1/a.star")
	t2, _ := ParseOCILoadTarget("oci://ghcr.io/org/lib:v1/b.star")

	result, err := r.Resolve(context.Background(), []*OCILoadTarget{t1, t2})
	if err != nil {
		t.Fatal(err)
	}
	if result["a.star"] != "a = 1" {
		t.Errorf("a.star = %q, want %q", result["a.star"], "a = 1")
	}
	if result["b.star"] != "b = 2" {
		t.Errorf("b.star = %q, want %q", result["b.star"], "b = 2")
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
	r := NewResolver(c, authn.DefaultKeychain, f, logging.NewNopLogger())

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
	r := NewResolver(c, authn.DefaultKeychain, f, logging.NewNopLogger())

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
	r := NewResolver(c, authn.DefaultKeychain, f, logging.NewNopLogger())

	target, _ := ParseOCILoadTarget("oci://ghcr.io/org/lib:v1/a.star")
	result, err := r.Resolve(context.Background(), []*OCILoadTarget{target})
	if err != nil {
		t.Fatal(err)
	}

	// Both a.star and b.star should be resolved.
	if _, ok := result["a.star"]; !ok {
		t.Error("expected a.star in result")
	}
	if _, ok := result["b.star"]; !ok {
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
	r := NewResolver(c, authn.DefaultKeychain, f, logging.NewNopLogger())

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
	r := NewResolver(c, authn.DefaultKeychain, f, logging.NewNopLogger())

	target, _ := ParseOCILoadTarget("oci://ghcr.io/org/lib:v1/h.star")
	result, err := r.Resolve(context.Background(), []*OCILoadTarget{target})
	if err != nil {
		t.Fatalf("expected stale serving, got error: %v", err)
	}
	if result["h.star"] != "x = 1" {
		t.Errorf("got %q, want %q", result["h.star"], "x = 1")
	}
}

func TestResolveColdMissFails(t *testing.T) {
	c := NewCache(5 * time.Minute)

	// Registry is unreachable, cache is empty.
	f := &mockFetcher{err: fmt.Errorf("connection refused")}
	r := NewResolver(c, authn.DefaultKeychain, f, logging.NewNopLogger())

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

	layer, err := tarball.LayerFromReader(bytes.NewReader(buf.Bytes()), tarball.WithMediaType(types.MediaType(LayerMediaType)))
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
	r := NewResolver(c, authn.DefaultKeychain, f, logging.NewNopLogger())

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

	layer, err := tarball.LayerFromReader(bytes.NewReader(buf.Bytes()), tarball.WithMediaType(types.MediaType(LayerMediaType)))
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
	r := NewResolver(c, authn.DefaultKeychain, f, logging.NewNopLogger())

	target, _ := ParseOCILoadTarget("oci://ghcr.io/org/lib:v1/good.star")
	result, err := r.Resolve(context.Background(), []*OCILoadTarget{target})
	if err != nil {
		t.Fatal(err)
	}
	if result["good.star"] != "x = 1" {
		t.Errorf("good.star = %q, want %q", result["good.star"], "x = 1")
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
	r := NewResolver(c, customKC, f, logging.NewNopLogger())

	target, _ := ParseOCILoadTarget("oci://ghcr.io/org/lib:v1/h.star")
	result, err := r.Resolve(context.Background(), []*OCILoadTarget{target})
	if err != nil {
		t.Fatal(err)
	}
	if result["h.star"] != "x = 1" {
		t.Errorf("got %q, want %q", result["h.star"], "x = 1")
	}
}
