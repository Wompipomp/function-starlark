package runtime

import (
	"strings"
	"sync"
	"testing"

	"github.com/crossplane/crossplane-runtime/v2/pkg/logging"
	"github.com/google/go-cmp/cmp"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"go.starlark.net/starlark"

	"github.com/wompipomp/function-starlark/metrics"
)

// testLogger implements logging.Logger for tests.
type testLogger struct {
	mu      sync.Mutex
	entries []logEntry
}

type logEntry struct {
	msg           string
	keysAndValues []interface{}
}

func (l *testLogger) Info(msg string, keysAndValues ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = append(l.entries, logEntry{msg: msg, keysAndValues: keysAndValues})
}

func (l *testLogger) Debug(msg string, keysAndValues ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = append(l.entries, logEntry{msg: msg, keysAndValues: keysAndValues})
}

func (l *testLogger) WithValues(_ ...any) logging.Logger {
	return l
}

func (l *testLogger) getEntries() []logEntry {
	l.mu.Lock()
	defer l.mu.Unlock()
	cp := make([]logEntry, len(l.entries))
	copy(cp, l.entries)
	return cp
}

func TestBasicExecution(t *testing.T) {
	log := &testLogger{}
	rt := NewRuntime(log)

	globals, err := rt.Execute("x = 1 + 2", starlark.StringDict{}, "composition.star", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	x, ok := globals["x"]
	if !ok {
		t.Fatal("expected global 'x' to be set")
	}

	xInt, ok := x.(starlark.Int)
	if !ok {
		t.Fatalf("expected x to be Int, got %T", x)
	}

	got, _ := starlark.AsInt32(xInt)
	if got != 3 {
		t.Errorf("x = %d, want 3", got)
	}
}

func TestCacheHit(t *testing.T) {
	log := &testLogger{}
	rt := NewRuntime(log)

	source := "x = 1 + 2"

	// First call -- cache miss
	_, err := rt.Execute(source, starlark.StringDict{}, "composition.star", nil)
	if err != nil {
		t.Fatalf("first execute error: %v", err)
	}
	if rt.CacheLen() != 1 {
		t.Fatalf("cache len after first call = %d, want 1", rt.CacheLen())
	}

	// Second call with same source -- cache hit
	_, err = rt.Execute(source, starlark.StringDict{}, "composition.star", nil)
	if err != nil {
		t.Fatalf("second execute error: %v", err)
	}
	if rt.CacheLen() != 1 {
		t.Fatalf("cache len after second call = %d, want 1 (should reuse)", rt.CacheLen())
	}
}

func TestCacheMiss(t *testing.T) {
	log := &testLogger{}
	rt := NewRuntime(log)

	_, err := rt.Execute("x = 1", starlark.StringDict{}, "composition.star", nil)
	if err != nil {
		t.Fatalf("first execute error: %v", err)
	}

	_, err = rt.Execute("x = 2", starlark.StringDict{}, "composition.star", nil)
	if err != nil {
		t.Fatalf("second execute error: %v", err)
	}

	if rt.CacheLen() != 2 {
		t.Fatalf("cache len = %d, want 2 (different sources)", rt.CacheLen())
	}
}

func TestCompilationError(t *testing.T) {
	log := &testLogger{}
	rt := NewRuntime(log)

	_, err := rt.Execute("x = (", starlark.StringDict{}, "composition.star", nil)
	if err == nil {
		t.Fatal("expected compilation error, got nil")
	}

	if !strings.Contains(err.Error(), "starlark compilation error:") {
		t.Errorf("error = %q, want it to contain 'starlark compilation error:'", err.Error())
	}
}

func TestRuntimeError(t *testing.T) {
	log := &testLogger{}
	rt := NewRuntime(log)

	// Use an undefined variable in a runtime context (function call).
	_, err := rt.Execute("x = undefined_var", starlark.StringDict{}, "composition.star", nil)
	if err == nil {
		t.Fatal("expected runtime error, got nil")
	}

	// An undefined variable at top level is actually a compilation error in Starlark
	// because the resolver catches it. Let's use something that fails at runtime.
	_, err = rt.Execute(`
def f():
    return 1 / 0
f()
`, starlark.StringDict{}, "composition.star", nil)
	if err == nil {
		t.Fatal("expected runtime error, got nil")
	}

	if !strings.Contains(err.Error(), "starlark execution error:") {
		t.Errorf("error = %q, want it to contain 'starlark execution error:'", err.Error())
	}
}

func TestLoadBlocked(t *testing.T) {
	log := &testLogger{}
	rt := NewRuntime(log)

	_, err := rt.Execute(`load("foo.star", "bar")`, starlark.StringDict{}, "composition.star", nil)
	if err == nil {
		t.Fatal("expected error for load(), got nil")
	}

	if !strings.Contains(err.Error(), "load() is not supported") {
		t.Errorf("error = %q, want it to contain 'load() is not supported'", err.Error())
	}
}

func TestTopLevelControlFlow(t *testing.T) {
	log := &testLogger{}
	rt := NewRuntime(log)

	// Top-level if and for should be allowed.
	source := `
result = []
for i in range(3):
    result.append(i)
if len(result) == 3:
    ok = True
`
	globals, err := rt.Execute(source, starlark.StringDict{}, "composition.star", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ok, found := globals["ok"]
	if !found {
		t.Fatal("expected global 'ok' to be set")
	}
	if ok != starlark.True {
		t.Errorf("ok = %v, want True", ok)
	}
}

func TestStepLimitExceeded(t *testing.T) {
	log := &testLogger{}
	rt := NewRuntime(log)

	_, err := rt.Execute("while True: pass", starlark.StringDict{}, "composition.star", nil)
	if err == nil {
		t.Fatal("expected step limit error, got nil")
	}

	if !strings.Contains(err.Error(), "exceeded execution limit") {
		t.Errorf("error = %q, want it to contain 'exceeded execution limit'", err.Error())
	}

	if !strings.Contains(err.Error(), "1000000 steps") {
		t.Errorf("error = %q, want it to contain '1000000 steps'", err.Error())
	}
}

func TestPrintRouting(t *testing.T) {
	log := &testLogger{}
	rt := NewRuntime(log)

	_, err := rt.Execute(`print("hello world")`, starlark.StringDict{}, "composition.star", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	entries := log.getEntries()
	found := false
	for _, e := range entries {
		if e.msg == "starlark print" {
			for i := 0; i+1 < len(e.keysAndValues); i += 2 {
				if e.keysAndValues[i] == "msg" && e.keysAndValues[i+1] == "hello world" {
					found = true
				}
			}
		}
	}

	if !found {
		t.Error("expected print message to be routed to logger at Debug level with msg='hello world'")
	}
}

func TestSandboxNoOS(t *testing.T) {
	log := &testLogger{}
	rt := NewRuntime(log)

	// os is not predeclared, so using it should fail at compile time.
	_, err := rt.Execute(`x = os.getenv("HOME")`, starlark.StringDict{}, "composition.star", nil)
	if err == nil {
		t.Fatal("expected error for os access, got nil")
	}
}

func TestSandboxNoSys(t *testing.T) {
	log := &testLogger{}
	rt := NewRuntime(log)

	_, err := rt.Execute(`x = sys.exit(1)`, starlark.StringDict{}, "composition.star", nil)
	if err == nil {
		t.Fatal("expected error for sys access, got nil")
	}
}

func TestSandboxNoIO(t *testing.T) {
	log := &testLogger{}
	rt := NewRuntime(log)

	_, err := rt.Execute(`f = open("test.txt")`, starlark.StringDict{}, "composition.star", nil)
	if err == nil {
		t.Fatal("expected error for io access, got nil")
	}
}

func TestSandboxNoTime(t *testing.T) {
	log := &testLogger{}
	rt := NewRuntime(log)

	_, err := rt.Execute(`t = time.now()`, starlark.StringDict{}, "composition.star", nil)
	if err == nil {
		t.Fatal("expected error for time access, got nil")
	}
}

func TestConcurrentExecution(t *testing.T) {
	log := &testLogger{}
	rt := NewRuntime(log)

	const goroutines = 10
	var wg sync.WaitGroup
	errs := make([]error, goroutines)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, err := rt.Execute("x = 1 + 2", starlark.StringDict{}, "composition.star", nil)
			errs[idx] = err
		}(i)
	}

	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d error: %v", i, err)
		}
	}
}

func TestThreadIsolation(t *testing.T) {
	log := &testLogger{}
	rt := NewRuntime(log)

	const goroutines = 10
	var wg sync.WaitGroup
	results := make([]starlark.StringDict, goroutines)
	errs := make([]error, goroutines)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			// Each goroutine sets x to its own index.
			source := "x = " + starlark.MakeInt(idx).String()
			globals, err := rt.Execute(source, starlark.StringDict{}, "composition.star", nil)
			results[idx] = globals
			errs[idx] = err
		}(i)
	}

	wg.Wait()

	for i := 0; i < goroutines; i++ {
		if errs[i] != nil {
			t.Errorf("goroutine %d error: %v", i, errs[i])
			continue
		}

		x, ok := results[i]["x"]
		if !ok {
			t.Errorf("goroutine %d: expected global 'x'", i)
			continue
		}

		got, _ := starlark.AsInt32(x.(starlark.Int))
		if int(got) != i {
			t.Errorf("goroutine %d: x = %d, want %d", i, got, i)
		}
	}
}

func TestContentHash(t *testing.T) {
	// Same source should produce same hash.
	h1 := contentHash("x = 1")
	h2 := contentHash("x = 1")
	if h1 != h2 {
		t.Errorf("same source produced different hashes: %s vs %s", h1, h2)
	}

	// Different source should produce different hash.
	h3 := contentHash("x = 2")
	if diff := cmp.Diff(h1 == h3, false); diff != "" {
		t.Errorf("different source produced same hash")
	}

	// Hash should be hex-encoded SHA-256 (64 chars).
	if len(h1) != 64 {
		t.Errorf("hash length = %d, want 64", len(h1))
	}
}

func TestCacheKeyIsSourceOnly(t *testing.T) {
	log := &testLogger{}
	rt := NewRuntime(log)

	source := "x = 1"

	// Execute with one set of predeclared.
	_, err := rt.Execute(source, starlark.StringDict{
		"a": starlark.String("alpha"),
	}, "composition.star", nil)
	if err != nil {
		t.Fatalf("first execute error: %v", err)
	}

	// Execute same source with different predeclared -- should cache hit.
	_, err = rt.Execute(source, starlark.StringDict{
		"b": starlark.String("beta"),
	}, "composition.star", nil)
	if err != nil {
		t.Fatalf("second execute error: %v", err)
	}

	if rt.CacheLen() != 1 {
		t.Fatalf("cache len = %d, want 1 (cache key should be source only)", rt.CacheLen())
	}
}

func TestDefFunction(t *testing.T) {
	log := &testLogger{}
	rt := NewRuntime(log)

	source := `
def greet(name):
    return "hello " + name
result = greet("world")
`
	globals, err := rt.Execute(source, starlark.StringDict{}, "composition.star", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result, ok := globals["result"]
	if !ok {
		t.Fatal("expected global 'result' to be set")
	}
	if result.(starlark.String) != "hello world" {
		t.Errorf("result = %v, want 'hello world'", result)
	}
}

func TestSetLiteral(t *testing.T) {
	log := &testLogger{}
	rt := NewRuntime(log)

	globals, err := rt.Execute("s = set([1, 2, 3, 2])", starlark.StringDict{}, "composition.star", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	s, ok := globals["s"]
	if !ok {
		t.Fatal("expected global 's' to be set")
	}

	set, ok := s.(*starlark.Set)
	if !ok {
		t.Fatalf("expected s to be *starlark.Set, got %T", s)
	}

	if set.Len() != 3 {
		t.Errorf("set len = %d, want 3 (duplicates removed)", set.Len())
	}
}

func TestEmptySource(t *testing.T) {
	log := &testLogger{}
	rt := NewRuntime(log)

	globals, err := rt.Execute("", starlark.StringDict{}, "composition.star", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(globals) != 0 {
		t.Errorf("globals len = %d, want 0 for empty source", len(globals))
	}
}

func TestConcurrentSameSource(t *testing.T) {
	log := &testLogger{}
	rt := NewRuntime(log)

	const goroutines = 10
	source := "x = 42"
	var wg sync.WaitGroup
	errs := make([]error, goroutines)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, err := rt.Execute(source, starlark.StringDict{}, "composition.star", nil)
			errs[idx] = err
		}(i)
	}

	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d error: %v", i, err)
		}
	}

	if rt.CacheLen() != 1 {
		t.Errorf("cache len = %d, want 1 (all goroutines used same source)", rt.CacheLen())
	}
}

func TestWhileLoopNormal(t *testing.T) {
	log := &testLogger{}
	rt := NewRuntime(log)

	// Use a list to accumulate results (Starlark forbids top-level
	// variable reassignment inside while loops).
	source := `
acc = []
i = [0]
while i[0] < 5:
    acc.append(i[0])
    i[0] = i[0] + 1
total = len(acc)
`
	globals, err := rt.Execute(source, starlark.StringDict{}, "composition.star", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	total, ok := globals["total"]
	if !ok {
		t.Fatal("expected global 'total' to be set")
	}

	got, _ := starlark.AsInt32(total.(starlark.Int))
	if got != 5 {
		t.Errorf("total = %d, want 5 (items 0..4)", got)
	}
}

func TestPredeclaredAccess(t *testing.T) {
	log := &testLogger{}
	rt := NewRuntime(log)

	// A predeclared variable should be accessible in the script.
	predeclared := starlark.StringDict{
		"greeting": starlark.String("hello"),
	}

	globals, err := rt.Execute("x = greeting", predeclared, "composition.star", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	x, ok := globals["x"]
	if !ok {
		t.Fatal("expected global 'x' to be set")
	}
	if x.(starlark.String) != "hello" {
		t.Errorf("x = %v, want 'hello'", x)
	}
}

func TestFilenameInError(t *testing.T) {
	log := &testLogger{}
	rt := NewRuntime(log)

	// Execute a script that fails at runtime with a custom filename.
	// The error backtrace should contain "my-script.star", not "composition.star".
	_, err := rt.Execute(`
def boom():
    return 1 / 0
boom()
`, starlark.StringDict{}, "my-script.star", nil)
	if err == nil {
		t.Fatal("expected runtime error, got nil")
	}

	if !strings.Contains(err.Error(), "my-script.star") {
		t.Errorf("error = %q, want it to contain 'my-script.star'", err.Error())
	}

	if strings.Contains(err.Error(), "composition.star") {
		t.Errorf("error = %q, should NOT contain 'composition.star'", err.Error())
	}
}

func TestCacheKeyIncludesFilename(t *testing.T) {
	log := &testLogger{}
	rt := NewRuntime(log)

	source := "x = 42"

	// Execute same source with two different filenames.
	_, err := rt.Execute(source, starlark.StringDict{}, "a.star", nil)
	if err != nil {
		t.Fatalf("first execute error: %v", err)
	}

	_, err = rt.Execute(source, starlark.StringDict{}, "b.star", nil)
	if err != nil {
		t.Fatalf("second execute error: %v", err)
	}

	// Different filenames must produce different cache entries.
	if rt.CacheLen() != 2 {
		t.Fatalf("cache len = %d, want 2 (different filenames should produce different cache entries)", rt.CacheLen())
	}
}

func TestCacheMetrics(t *testing.T) {
	log := &testLogger{}
	rt := NewRuntime(log)

	// Use a unique filename to avoid cross-test interference with the global registry.
	filename := "cache-metrics-test.star"

	// Record baselines.
	baseHits := testutil.ToFloat64(metrics.CacheHitsTotal.WithLabelValues(filename))
	baseMisses := testutil.ToFloat64(metrics.CacheMissesTotal.WithLabelValues(filename))

	// First execute -- cache miss.
	_, err := rt.Execute("x = 42", starlark.StringDict{}, filename, nil)
	if err != nil {
		t.Fatalf("first execute error: %v", err)
	}

	missDelta := testutil.ToFloat64(metrics.CacheMissesTotal.WithLabelValues(filename)) - baseMisses
	hitDelta := testutil.ToFloat64(metrics.CacheHitsTotal.WithLabelValues(filename)) - baseHits
	if missDelta != 1 {
		t.Errorf("cache miss delta after first execute = %v, want 1", missDelta)
	}
	if hitDelta != 0 {
		t.Errorf("cache hit delta after first execute = %v, want 0", hitDelta)
	}

	// Second execute with same source -- cache hit.
	baseMisses = testutil.ToFloat64(metrics.CacheMissesTotal.WithLabelValues(filename))
	baseHits = testutil.ToFloat64(metrics.CacheHitsTotal.WithLabelValues(filename))

	_, err = rt.Execute("x = 42", starlark.StringDict{}, filename, nil)
	if err != nil {
		t.Fatalf("second execute error: %v", err)
	}

	hitDelta = testutil.ToFloat64(metrics.CacheHitsTotal.WithLabelValues(filename)) - baseHits
	missDelta = testutil.ToFloat64(metrics.CacheMissesTotal.WithLabelValues(filename)) - baseMisses
	if hitDelta != 1 {
		t.Errorf("cache hit delta after second execute = %v, want 1", hitDelta)
	}
	if missDelta != 0 {
		t.Errorf("cache miss delta after second execute = %v, want 0", missDelta)
	}
}
