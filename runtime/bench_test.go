package runtime

import (
	"fmt"
	goruntime "runtime"
	"testing"

	"go.starlark.net/starlark"
)

// starBenchModule is a small library module with several public exports,
// shared by the explicit-vs-star import benchmarks below.
const starBenchModule = `
def make_name(prefix, i):
    return "%s-%d" % (prefix, i)

def make_tags(env):
    return {"managed-by": "starlark", "env": env}

DEFAULT_REGION = "us-east-1"
DEFAULT_ENV = "prod"
MAX_REPLICAS = 5
`

// explicitImportScript imports specific names. It contains no star import, so
// the pre-execution star-import scan is memoized after the first call and
// skipped on every subsequent reconciliation.
const explicitImportScript = `
load("m.star", "make_name", "make_tags", "DEFAULT_REGION", "DEFAULT_ENV")
names = [make_name("bucket", i) for i in range(10)]
tags = make_tags(DEFAULT_ENV)
region = DEFAULT_REGION
`

// starImportScript imports all exports via "*". Star expansion depends on the
// live module set, so it cannot be memoized -- the script is re-scanned and
// re-expanded on every reconciliation.
const starImportScript = `
load("m.star", "*")
names = [make_name("bucket", i) for i in range(10)]
tags = make_tags(DEFAULT_ENV)
region = DEFAULT_REGION
`

// BenchmarkResolveStarImports measures the per-reconciliation cost of the
// star-import scan that runs before every execution, contrasting the two paths:
//
//   - "explicit": named imports only. The scan finds no star import and
//     memoizes that verdict on the shared Runtime, so later calls skip the parse.
//   - "star": load("m.star", "*"). Expansion depends on the live module set, so
//     the script is re-scanned and re-expanded (including a module export scan)
//     on every call.
//
// The explicit path should be substantially cheaper, which is why named imports
// are recommended on latency-sensitive hot paths. See docs/module-system.md
// ("Star-import scan memoization").
func BenchmarkResolveStarImports(b *testing.B) {
	cases := []struct {
		name   string
		source string
	}{
		{"explicit", explicitImportScript},
		{"star", starImportScript},
	}

	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			// The memoization cache lives on the Runtime, which is shared across
			// reconciliations; the ModuleLoader is per-reconciliation but its
			// fast-path check consults the shared Runtime cache.
			rt := NewRuntime(&testLogger{})
			inline := map[string]string{"m.star": starBenchModule}
			loader := NewModuleLoader(inline, nil, starlark.StringDict{}, rt, "")

			// Warm up: first call performs the scan (and, for the explicit case,
			// caches the "no star imports" verdict).
			if _, err := loader.ResolveStarImports(tc.source, "composition.star"); err != nil {
				b.Fatalf("warm-up failed: %v", err)
			}

			b.ReportAllocs()
			b.ResetTimer()

			for b.Loop() {
				if _, err := loader.ResolveStarImports(tc.source, "composition.star"); err != nil {
					b.Fatalf("ResolveStarImports failed: %v", err)
				}
			}
		})
	}
}

// fixtureScript is a non-trivial Starlark script (~25 lines) that exercises
// the core interpreter: loops, conditionals, string formatting, dict manipulation.
// It creates 10 resource-like dicts to simulate a typical composition workload.
const fixtureScript = `
resources = []
for i in range(10):
    name = "resource-%d" % i
    env = "prod" if i % 2 == 0 else "dev"
    tags = {"index": str(i), "env": env, "managed-by": "starlark"}
    spec = {
        "name": name,
        "replicas": 1 if env == "dev" else 3,
        "region": "us-east-1",
        "tags": tags,
    }
    resource = {
        "apiVersion": "example.io/v1",
        "kind": "Widget",
        "metadata": {"name": name, "labels": {"env": env}},
        "spec": spec,
    }
    resources.append(resource)

total = len(resources)
summary = "created %d resources" % total
`

// BenchmarkCachedExecution measures cached-path latency (ns/op) for a
// 10-resource composition script. The cache is pre-warmed before the
// benchmark loop. This validates PERF-03: sub-second cached reconciliation.
func BenchmarkCachedExecution(b *testing.B) {
	log := &testLogger{}
	rt := NewRuntime(log)

	predeclared := starlark.StringDict{}

	// Pre-warm the cache.
	if _, err := rt.Execute(fixtureScript, predeclared, "bench.star", nil); err != nil {
		b.Fatalf("pre-warm failed: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		if _, err := rt.Execute(fixtureScript, predeclared, "bench.star", nil); err != nil {
			b.Fatalf("execute failed: %v", err)
		}
	}
}

// BenchmarkIdleMemory measures the idle memory footprint of a Runtime after
// executing one composition. This validates PERF-01: under 40MB idle memory.
// This is a measurement benchmark (not throughput), so it runs a single
// iteration and reports the result via b.ReportMetric.
func BenchmarkIdleMemory(b *testing.B) {
	// Baseline measurement.
	goruntime.GC()
	goruntime.GC()
	var before goruntime.MemStats
	goruntime.ReadMemStats(&before)

	// Create runtime and execute one composition.
	log := &testLogger{}
	rt := NewRuntime(log)
	if _, err := rt.Execute(fixtureScript, starlark.StringDict{}, "bench.star", nil); err != nil {
		b.Fatalf("execute failed: %v", err)
	}

	// Measure idle (post-execution, post-GC).
	goruntime.GC()
	goruntime.GC()
	var after goruntime.MemStats
	goruntime.ReadMemStats(&after)

	delta := after.HeapAlloc - before.HeapAlloc
	idleMB := float64(delta) / (1024 * 1024)
	b.ReportMetric(idleMB, "MB/idle")
	b.ReportMetric(0, "ns/op")
}

// BenchmarkMemoryScaling measures memory growth as the number of cached
// compositions increases from 10 to 25 to 50. Flat/near-flat scaling means
// MB/composition is approximately constant. This validates PERF-02.
// Each sub-benchmark is a measurement (not throughput) -- runs once and
// reports metrics.
func BenchmarkMemoryScaling(b *testing.B) {
	for _, count := range []int{10, 25, 50} {
		b.Run(fmt.Sprintf("compositions=%d", count), func(b *testing.B) {
			// Baseline.
			goruntime.GC()
			goruntime.GC()
			var before goruntime.MemStats
			goruntime.ReadMemStats(&before)

			// Cache 'count' unique programs.
			log := &testLogger{}
			rt := NewRuntime(log)
			for i := 0; i < count; i++ {
				src := fmt.Sprintf(`
resources = []
for j in range(10):
    name = "res-%d-%%d" %% j
    resources.append({"name": name, "index": j})
total_%d = len(resources)
`, i, i)
				if _, err := rt.Execute(src, starlark.StringDict{}, "bench.star", nil); err != nil {
					b.Fatalf("execute %d failed: %v", i, err)
				}
			}

			// Measure post-cache memory.
			goruntime.GC()
			goruntime.GC()
			var after goruntime.MemStats
			goruntime.ReadMemStats(&after)

			delta := after.HeapAlloc - before.HeapAlloc
			totalMB := float64(delta) / (1024 * 1024)
			perCompMB := totalMB / float64(count)

			b.ReportMetric(totalMB, "MB/total")
			b.ReportMetric(perCompMB, "MB/composition")
			b.ReportMetric(0, "ns/op")
		})
	}
}
