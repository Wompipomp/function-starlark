package main

import (
	"context"
	"testing"

	"github.com/crossplane/function-sdk-go/logging"
	fnv1 "github.com/crossplane/function-sdk-go/proto/v1"
	"github.com/crossplane/function-sdk-go/resource"

	"github.com/wompipomp/function-starlark/runtime"
)

// benchScript is a realistic 10-resource Starlark composition that exercises
// the full builtins pipeline: get(), oxr access, conditionals, loops,
// Resource() constructor, set_condition, emit_event, and dxr status update.
const benchScript = `
region = get(oxr, "spec.region", "us-east-1")
env = get(oxr, "spec.environment", "dev")

# Conditional resource: monitoring dashboard in prod only.
if env == "prod":
    Resource("monitoring", {
        "apiVersion": "monitoring.example.io/v1",
        "kind": "Dashboard",
        "metadata": {"name": "prod-dashboard"},
        "spec": {"enabled": True},
    })

# Loop-based resource creation (8 buckets) with conditional replica counts.
for i in range(8):
    name = "bucket-%d" % i
    Resource(name, {
        "apiVersion": "s3.aws.upbound.io/v1beta1",
        "kind": "Bucket",
        "metadata": {"name": name, "labels": {"env": env, "index": str(i)}},
        "spec": {
            "forProvider": {
                "region": region,
                "tags": {"managed-by": "starlark", "env": env},
            },
        },
    })

# Final resource using get() with fallback.
tier = get(oxr, "spec.tier", "standard")
Resource("cache", {
    "apiVersion": "cache.example.io/v1",
    "kind": "Redis",
    "metadata": {"name": "redis-" + tier},
    "spec": {"tier": tier, "region": region},
})

# Conditions and events.
set_condition(type = "Ready", status = "True", reason = "Available", message = "All resources created")
emit_event(severity = "Normal", message = "Created 10 resources in " + region)

# DXR status update.
dxr["status"] = {"bucketCount": 8, "region": region, "tier": tier}
`

// BenchmarkRunFunction exercises the full RunFunction pipeline including input
// parsing, globals construction, cached Starlark execution, resource collection,
// condition/event application, and dxr status update. The cache is pre-warmed
// so this measures steady-state (cached) latency.
func BenchmarkRunFunction(b *testing.B) {
	f := &Function{
		log:     logging.NewNopLogger(),
		runtime: runtime.NewRuntime(logging.NewNopLogger()),
	}

	req := &fnv1.RunFunctionRequest{
		Input: resource.MustStructJSON(`{
			"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
			"kind": "StarlarkInput",
			"spec": {
				"source": "` + escapeJSON(benchScript) + `"
			}
		}`),
		Observed: &fnv1.State{
			Composite: &fnv1.Resource{
				Resource: resource.MustStructJSON(`{
					"apiVersion": "example.crossplane.io/v1",
					"kind": "XBucket",
					"spec": {
						"region": "eu-west-1",
						"environment": "prod",
						"tier": "premium"
					},
					"status": {}
				}`),
			},
		},
	}

	ctx := context.Background()

	// Pre-warm: populate cache and verify sanity.
	rsp, err := f.RunFunction(ctx, req)
	if err != nil {
		b.Fatalf("pre-warm failed: %v", err)
	}
	if rsp == nil {
		b.Fatal("pre-warm returned nil response")
	}

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		if _, err := f.RunFunction(ctx, req); err != nil {
			b.Fatalf("RunFunction failed: %v", err)
		}
	}
}

// escapeJSON escapes a string for embedding in a JSON string literal.
// Handles newlines, tabs, quotes, and backslashes.
func escapeJSON(s string) string {
	var out []byte
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '"':
			out = append(out, '\\', '"')
		case '\\':
			out = append(out, '\\', '\\')
		case '\n':
			out = append(out, '\\', 'n')
		case '\r':
			out = append(out, '\\', 'r')
		case '\t':
			out = append(out, '\\', 't')
		default:
			out = append(out, s[i])
		}
	}
	return string(out)
}
