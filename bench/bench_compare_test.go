// Package bench provides comparative benchmarks for function-starlark vs
// function-go-templating and function-kcl. All three functions are started as
// Docker containers and called via gRPC with equivalent workloads.
//
// Prerequisites:
//
//	docker pull xpkg.upbound.io/upbound/function-go-templating:v0.11.4
//	docker pull xpkg.upbound.io/crossplane-contrib/function-kcl:v0.12.0
//	(cd .. && docker build . --tag=runtime)
//
// Run:
//
//	go test -bench=. -benchmem -count=5 -run='^$' -timeout=300s ./bench/
package bench

import (
	"context"
	"fmt"
	"net"
	"os/exec"
	"strings"
	"testing"
	"time"

	fnv1 "github.com/crossplane/function-sdk-go/proto/v1"
	"github.com/crossplane/function-sdk-go/resource"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// ---------------------------------------------------------------------------
// Equivalent scripts: each creates 10 S3 buckets with labels and tags.
// ---------------------------------------------------------------------------

const starlarkSource = `
region = get(oxr, "spec.region", "us-east-1")
name = get(oxr, "metadata.name", "bench")

for i in range(10):
    Resource("bucket-%d" % i, {
        "apiVersion": "s3.aws.upbound.io/v1beta1",
        "kind": "Bucket",
        "metadata": {
            "name": "%s-bucket-%d" % (name, i),
            "labels": {"env": "prod", "index": str(i)},
        },
        "spec": {
            "forProvider": {
                "region": region,
                "tags": {"ManagedBy": "crossplane", "Index": str(i)},
            },
        },
    }, labels=None)

set_condition(type="Ready", status="True", reason="Available", message="All resources created")
dxr["status"] = {"bucketCount": 10, "region": region}
`

const goTemplateSource = `
{{- $region := .observed.composite.resource.spec.region | default "us-east-1" -}}
{{- $name := .observed.composite.resource.metadata.name | default "bench" -}}
{{- range $i := until 10 }}
---
apiVersion: s3.aws.upbound.io/v1beta1
kind: Bucket
metadata:
  annotations:
    gotemplating.fn.crossplane.io/composition-resource-name: bucket-{{ $i }}
  name: {{ $name }}-bucket-{{ $i }}
  labels:
    env: prod
    index: "{{ $i }}"
spec:
  forProvider:
    region: {{ $region }}
    tags:
      ManagedBy: crossplane
      Index: "{{ $i }}"
{{- end }}
`

const kclSource = `
oxr = option("params").oxr
_region = oxr.spec.get("region", "us-east-1")
_name = oxr.metadata.name or "bench"

_items = [{
    apiVersion: "s3.aws.upbound.io/v1beta1"
    kind: "Bucket"
    metadata: {
        name: "{}-bucket-{}".format(_name, i)
        annotations: {
            "crossplane.io/composition-resource-name": "bucket-{}".format(i)
        }
        labels: {
            env: "prod"
            index: str(i)
        }
    }
    spec.forProvider: {
        region: _region
        tags: {
            ManagedBy: "crossplane"
            Index: str(i)
        }
    }
} for i in range(10)]

items = _items
`

const pythonicSource = `
class BucketComposite(BaseComposite):
    def compose(self):
        for i in range(10):
            bucket = self.resources[f"bucket-{i}"]("Bucket", "s3.aws.upbound.io/v1beta1")
            bucket.metadata.name = f"bench-bucket-{i}"
            bucket.metadata.labels.env = "prod"
            bucket.metadata.labels.index = str(i)
            bucket.spec.forProvider.region = "us-east-1"
            bucket.spec.forProvider.tags.ManagedBy = "crossplane"
            bucket.spec.forProvider.tags.Index = str(i)
`

const pythonicSource50 = `
class BucketComposite(BaseComposite):
    def compose(self):
        for i in range(50):
            bucket = self.resources[f"bucket-{i}"]("Bucket", "s3.aws.upbound.io/v1beta1")
            bucket.metadata.name = f"bench-bucket-{i}"
            bucket.metadata.labels.env = "prod"
            bucket.metadata.labels.index = str(i)
            bucket.metadata.labels.team = "platform"
            bucket.spec.forProvider.region = "us-east-1"
            bucket.spec.forProvider.tags.ManagedBy = "crossplane"
            bucket.spec.forProvider.tags.Index = str(i)
            bucket.spec.forProvider.tags.Env = "prod"
`

// ---------------------------------------------------------------------------
// Container management
// ---------------------------------------------------------------------------

type container struct {
	name string
	port int
	id   string
}

// freePort asks the OS for an available TCP port.
func freePort(t testing.TB) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("finding free port: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	return port
}

// startContainer starts a function Docker container and returns the allocated port.
func startContainer(t testing.TB, image, name string) *container {
	t.Helper()
	port := freePort(t)

	args := []string{
		"run", "-d", "--rm",
		"--name", name,
		"-p", fmt.Sprintf("127.0.0.1:%d:9443", port),
		image,
		"--insecure",
	}

	out, err := exec.Command("docker", args...).CombinedOutput() //nolint:gosec // benchmark helper; args are test-controlled
	if err != nil {
		t.Fatalf("starting container %s: %v\n%s", name, err, out)
	}

	c := &container{name: name, port: port, id: strings.TrimSpace(string(out))}

	t.Cleanup(func() {
		_ = exec.Command("docker", "rm", "-f", c.id).Run() //nolint:gosec // cleanup; container ID from our own docker run
	})

	return c
}

// waitForReady polls the gRPC endpoint until it responds or times out.
func waitForReady(t testing.TB, addr string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			time.Sleep(200 * time.Millisecond)
			continue
		}
		client := fnv1.NewFunctionRunnerServiceClient(conn)
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		// Send a minimal probe request.
		_, err = client.RunFunction(ctx, &fnv1.RunFunctionRequest{
			Input: resource.MustStructJSON(`{
				"apiVersion": "probe.io/v1",
				"kind": "Probe"
			}`),
		})
		cancel()
		_ = conn.Close()
		// Any response (even Fatal) means the server is up.
		if err == nil {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("container at %s not ready within %s", addr, timeout)
}

// ---------------------------------------------------------------------------
// Request builders
// ---------------------------------------------------------------------------

func starlarkRequest() *fnv1.RunFunctionRequest {
	return &fnv1.RunFunctionRequest{
		Meta: &fnv1.RequestMeta{Tag: "bench"},
		Input: resource.MustStructJSON(fmt.Sprintf(`{
			"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
			"kind": "StarlarkInput",
			"spec": {
				"source": %q
			}
		}`, starlarkSource)),
		Observed: benchObserved(),
	}
}

func goTemplateRequest() *fnv1.RunFunctionRequest {
	return &fnv1.RunFunctionRequest{
		Meta: &fnv1.RequestMeta{Tag: "bench"},
		Input: resource.MustStructJSON(fmt.Sprintf(`{
			"apiVersion": "gotemplating.fn.crossplane.io/v1beta1",
			"kind": "GoTemplate",
			"source": "Inline",
			"inline": {
				"template": %q
			}
		}`, goTemplateSource)),
		Observed: benchObserved(),
	}
}

func kclRequest() *fnv1.RunFunctionRequest {
	return &fnv1.RunFunctionRequest{
		Meta: &fnv1.RequestMeta{Tag: "bench"},
		Input: resource.MustStructJSON(fmt.Sprintf(`{
			"apiVersion": "krm.kcl.dev/v1alpha1",
			"kind": "KCLInput",
			"spec": {
				"source": %q
			}
		}`, kclSource)),
		Observed: benchObserved(),
	}
}

func pythonicRequest(source string) *fnv1.RunFunctionRequest {
	return &fnv1.RunFunctionRequest{
		Meta: &fnv1.RequestMeta{Tag: "bench"},
		Input: resource.MustStructJSON(fmt.Sprintf(`{
			"apiVersion": "pythonic.fn.crossplane.io/v1alpha1",
			"kind": "Composite",
			"composite": %q
		}`, source)),
		Observed: benchObserved(),
		Desired: &fnv1.State{
			Composite: &fnv1.Resource{
				Resource: resource.MustStructJSON(`{
					"apiVersion": "example.crossplane.io/v1",
					"kind": "XBucket",
					"metadata": {"name": "bench-xr"},
					"spec": {"region": "us-east-1"}
				}`),
			},
		},
	}
}

func benchObserved() *fnv1.State {
	return &fnv1.State{
		Composite: &fnv1.Resource{
			Resource: resource.MustStructJSON(`{
				"apiVersion": "example.crossplane.io/v1",
				"kind": "XBucket",
				"metadata": {"name": "bench-xr"},
				"spec": {"region": "us-east-1"}
			}`),
		},
	}
}

// ---------------------------------------------------------------------------
// Image names — override with -ldflags or env vars if needed.
// ---------------------------------------------------------------------------

const (
	imageStarlark   = "runtime" // built locally via `docker build . --tag=runtime`
	imageGoTemplate = "xpkg.upbound.io/upbound/function-go-templating:v0.11.4"
	imageKCL        = "xpkg.upbound.io/crossplane-contrib/function-kcl:v0.12.0"
	imagePythonic   = "xpkg.upbound.io/crossplane-contrib/function-pythonic:v0.5.0"
)

// ---------------------------------------------------------------------------
// Benchmarks
// ---------------------------------------------------------------------------

// BenchmarkCompare_Starlark benchmarks function-starlark via gRPC over Docker.
func BenchmarkCompare_Starlark(b *testing.B) {
	c := startContainer(b, imageStarlark, "bench-starlark")
	addr := fmt.Sprintf("127.0.0.1:%d", c.port)
	waitForReady(b, addr, 30*time.Second)

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		b.Fatal(err)
	}
	defer conn.Close() //nolint:errcheck // benchmark cleanup

	client := fnv1.NewFunctionRunnerServiceClient(conn)
	req := starlarkRequest()

	// Warm up.
	if _, err := client.RunFunction(context.Background(), req); err != nil {
		b.Fatalf("warm-up failed: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		rsp, err := client.RunFunction(context.Background(), req)
		if err != nil {
			b.Fatalf("RunFunction: %v", err)
		}
		for _, r := range rsp.GetResults() {
			if r.GetSeverity() == fnv1.Severity_SEVERITY_FATAL {
				b.Fatalf("Fatal: %s", r.GetMessage())
			}
		}
	}
}

// BenchmarkCompare_GoTemplate benchmarks function-go-templating via gRPC over Docker.
func BenchmarkCompare_GoTemplate(b *testing.B) {
	c := startContainer(b, imageGoTemplate, "bench-gotemplate")
	addr := fmt.Sprintf("127.0.0.1:%d", c.port)
	waitForReady(b, addr, 30*time.Second)

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		b.Fatal(err)
	}
	defer conn.Close() //nolint:errcheck // benchmark cleanup

	client := fnv1.NewFunctionRunnerServiceClient(conn)
	req := goTemplateRequest()

	// Warm up.
	if _, err := client.RunFunction(context.Background(), req); err != nil {
		b.Fatalf("warm-up failed: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		rsp, err := client.RunFunction(context.Background(), req)
		if err != nil {
			b.Fatalf("RunFunction: %v", err)
		}
		for _, r := range rsp.GetResults() {
			if r.GetSeverity() == fnv1.Severity_SEVERITY_FATAL {
				b.Fatalf("Fatal: %s", r.GetMessage())
			}
		}
	}
}

// BenchmarkCompare_KCL benchmarks function-kcl via gRPC over Docker.
func BenchmarkCompare_KCL(b *testing.B) {
	c := startContainer(b, imageKCL, "bench-kcl")
	addr := fmt.Sprintf("127.0.0.1:%d", c.port)
	waitForReady(b, addr, 60*time.Second) // KCL is slower to start

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		b.Fatal(err)
	}
	defer conn.Close() //nolint:errcheck // benchmark cleanup

	client := fnv1.NewFunctionRunnerServiceClient(conn)
	req := kclRequest()

	// Warm up (KCL needs compilation on first call).
	if _, err := client.RunFunction(context.Background(), req); err != nil {
		b.Fatalf("warm-up failed: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		rsp, err := client.RunFunction(context.Background(), req)
		if err != nil {
			b.Fatalf("RunFunction: %v", err)
		}
		for _, r := range rsp.GetResults() {
			if r.GetSeverity() == fnv1.Severity_SEVERITY_FATAL {
				b.Fatalf("Fatal: %s", r.GetMessage())
			}
		}
	}
}

// BenchmarkCompare_Starlark_50Resources benchmarks function-starlark with 50 resources.
func BenchmarkCompare_Starlark_50Resources(b *testing.B) {
	c := startContainer(b, imageStarlark, "bench-starlark-50")
	addr := fmt.Sprintf("127.0.0.1:%d", c.port)
	waitForReady(b, addr, 30*time.Second)

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		b.Fatal(err)
	}
	defer conn.Close() //nolint:errcheck // benchmark cleanup

	client := fnv1.NewFunctionRunnerServiceClient(conn)

	script := `
region = get(oxr, "spec.region", "us-east-1")
name = get(oxr, "metadata.name", "bench")

for i in range(50):
    Resource("bucket-%d" % i, {
        "apiVersion": "s3.aws.upbound.io/v1beta1",
        "kind": "Bucket",
        "metadata": {
            "name": "%s-bucket-%d" % (name, i),
            "labels": {"env": "prod", "index": str(i), "team": "platform"},
        },
        "spec": {
            "forProvider": {
                "region": region,
                "tags": {"ManagedBy": "crossplane", "Index": str(i), "Env": "prod"},
            },
        },
    }, labels=None)

set_condition(type="Ready", status="True", reason="Available", message="All resources created")
dxr["status"] = {"bucketCount": 50, "region": region}
`

	req := &fnv1.RunFunctionRequest{
		Meta: &fnv1.RequestMeta{Tag: "bench-50"},
		Input: resource.MustStructJSON(fmt.Sprintf(`{
			"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
			"kind": "StarlarkInput",
			"spec": {"source": %q}
		}`, script)),
		Observed: benchObserved(),
	}

	// Warm up.
	if _, err := client.RunFunction(context.Background(), req); err != nil {
		b.Fatalf("warm-up failed: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		if _, err := client.RunFunction(context.Background(), req); err != nil {
			b.Fatalf("RunFunction: %v", err)
		}
	}
}

// BenchmarkCompare_GoTemplate_50Resources benchmarks function-go-templating with 50 resources.
func BenchmarkCompare_GoTemplate_50Resources(b *testing.B) {
	c := startContainer(b, imageGoTemplate, "bench-gotemplate-50")
	addr := fmt.Sprintf("127.0.0.1:%d", c.port)
	waitForReady(b, addr, 30*time.Second)

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		b.Fatal(err)
	}
	defer conn.Close() //nolint:errcheck // benchmark cleanup

	client := fnv1.NewFunctionRunnerServiceClient(conn)

	tmpl := `
{{- $region := .observed.composite.resource.spec.region | default "us-east-1" -}}
{{- $name := .observed.composite.resource.metadata.name | default "bench" -}}
{{- range $i := until 50 }}
---
apiVersion: s3.aws.upbound.io/v1beta1
kind: Bucket
metadata:
  annotations:
    gotemplating.fn.crossplane.io/composition-resource-name: bucket-{{ $i }}
  name: {{ $name }}-bucket-{{ $i }}
  labels:
    env: prod
    index: "{{ $i }}"
    team: platform
spec:
  forProvider:
    region: {{ $region }}
    tags:
      ManagedBy: crossplane
      Index: "{{ $i }}"
      Env: prod
{{- end }}
`

	req := &fnv1.RunFunctionRequest{
		Meta: &fnv1.RequestMeta{Tag: "bench-50"},
		Input: resource.MustStructJSON(fmt.Sprintf(`{
			"apiVersion": "gotemplating.fn.crossplane.io/v1beta1",
			"kind": "GoTemplate",
			"source": "Inline",
			"inline": {"template": %q}
		}`, tmpl)),
		Observed: benchObserved(),
	}

	// Warm up.
	if _, err := client.RunFunction(context.Background(), req); err != nil {
		b.Fatalf("warm-up failed: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		if _, err := client.RunFunction(context.Background(), req); err != nil {
			b.Fatalf("RunFunction: %v", err)
		}
	}
}

// BenchmarkCompare_KCL_50Resources benchmarks function-kcl with 50 resources.
func BenchmarkCompare_KCL_50Resources(b *testing.B) {
	c := startContainer(b, imageKCL, "bench-kcl-50")
	addr := fmt.Sprintf("127.0.0.1:%d", c.port)
	waitForReady(b, addr, 60*time.Second)

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		b.Fatal(err)
	}
	defer conn.Close() //nolint:errcheck // benchmark cleanup

	client := fnv1.NewFunctionRunnerServiceClient(conn)

	kcl := `
oxr = option("params").oxr
_region = oxr.spec.get("region", "us-east-1")
_name = oxr.metadata.name or "bench"

_items = [{
    apiVersion: "s3.aws.upbound.io/v1beta1"
    kind: "Bucket"
    metadata: {
        name: "{}-bucket-{}".format(_name, i)
        annotations: {
            "crossplane.io/composition-resource-name": "bucket-{}".format(i)
        }
        labels: {
            env: "prod"
            index: str(i)
            team: "platform"
        }
    }
    spec.forProvider: {
        region: _region
        tags: {
            ManagedBy: "crossplane"
            Index: str(i)
            Env: "prod"
        }
    }
} for i in range(50)]

items = _items
`

	req := &fnv1.RunFunctionRequest{
		Meta: &fnv1.RequestMeta{Tag: "bench-50"},
		Input: resource.MustStructJSON(fmt.Sprintf(`{
			"apiVersion": "krm.kcl.dev/v1alpha1",
			"kind": "KCLInput",
			"spec": {"source": %q}
		}`, kcl)),
		Observed: benchObserved(),
	}

	// Warm up.
	if _, err := client.RunFunction(context.Background(), req); err != nil {
		b.Fatalf("warm-up failed: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		if _, err := client.RunFunction(context.Background(), req); err != nil {
			b.Fatalf("RunFunction: %v", err)
		}
	}
}

// BenchmarkCompare_Pythonic benchmarks function-pythonic via gRPC over Docker.
func BenchmarkCompare_Pythonic(b *testing.B) {
	c := startContainer(b, imagePythonic, "bench-pythonic")
	addr := fmt.Sprintf("127.0.0.1:%d", c.port)
	waitForReady(b, addr, 30*time.Second)

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		b.Fatal(err)
	}
	defer conn.Close() //nolint:errcheck // benchmark cleanup

	client := fnv1.NewFunctionRunnerServiceClient(conn)
	req := pythonicRequest(pythonicSource)

	// Warm up.
	if _, err := client.RunFunction(context.Background(), req); err != nil {
		b.Fatalf("warm-up failed: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		rsp, err := client.RunFunction(context.Background(), req)
		if err != nil {
			b.Fatalf("RunFunction: %v", err)
		}
		for _, r := range rsp.GetResults() {
			if r.GetSeverity() == fnv1.Severity_SEVERITY_FATAL {
				b.Fatalf("Fatal: %s", r.GetMessage())
			}
		}
	}
}

// BenchmarkCompare_Pythonic_50Resources benchmarks function-pythonic with 50 resources.
func BenchmarkCompare_Pythonic_50Resources(b *testing.B) {
	c := startContainer(b, imagePythonic, "bench-pythonic-50")
	addr := fmt.Sprintf("127.0.0.1:%d", c.port)
	waitForReady(b, addr, 30*time.Second)

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		b.Fatal(err)
	}
	defer conn.Close() //nolint:errcheck // benchmark cleanup

	client := fnv1.NewFunctionRunnerServiceClient(conn)
	req := pythonicRequest(pythonicSource50)

	// Warm up.
	if _, err := client.RunFunction(context.Background(), req); err != nil {
		b.Fatalf("warm-up failed: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		rsp, err := client.RunFunction(context.Background(), req)
		if err != nil {
			b.Fatalf("RunFunction: %v", err)
		}
		for _, r := range rsp.GetResults() {
			if r.GetSeverity() == fnv1.Severity_SEVERITY_FATAL {
				b.Fatalf("Fatal: %s", r.GetMessage())
			}
		}
	}
}
