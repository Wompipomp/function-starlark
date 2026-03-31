package runtime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

func TestModuleLoadInline(t *testing.T) {
	log := &testLogger{}
	rt := NewRuntime(log)

	inline := map[string]string{
		"helpers.star": `def greet(name): return "hello " + name`,
	}

	loader := NewModuleLoader(inline, nil, starlark.StringDict{}, rt, "")
	thread := &starlark.Thread{Name: "test", Load: loader.LoadFunc()}

	loaded, err := thread.Load(thread, "helpers.star")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	greet, ok := loaded["greet"]
	if !ok {
		t.Fatal("expected 'greet' in loaded globals")
	}

	// Call the loaded function.
	result, err := starlark.Call(thread, greet, starlark.Tuple{starlark.String("world")}, nil)
	if err != nil {
		t.Fatalf("calling greet: %v", err)
	}

	if result.(starlark.String) != "hello world" {
		t.Errorf("greet('world') = %v, want 'hello world'", result)
	}
}

func TestModuleLoadFilesystem(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "helpers.star"), []byte(`def greet(name): return "hi " + name`), 0o600); err != nil {
		t.Fatal(err)
	}

	log := &testLogger{}
	rt := NewRuntime(log)
	loader := NewModuleLoader(nil, []string{dir}, starlark.StringDict{}, rt, "")

	thread := &starlark.Thread{Name: "test", Load: loader.LoadFunc()}
	loaded, err := thread.Load(thread, "helpers.star")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	greet, ok := loaded["greet"]
	if !ok {
		t.Fatal("expected 'greet' in loaded globals")
	}

	result, err := starlark.Call(thread, greet, starlark.Tuple{starlark.String("world")}, nil)
	if err != nil {
		t.Fatalf("calling greet: %v", err)
	}

	if result.(starlark.String) != "hi world" {
		t.Errorf("greet('world') = %v, want 'hi world'", result)
	}
}

func TestModuleInlinePriority(t *testing.T) {
	dir := t.TempDir()
	// Filesystem version returns "fs".
	if err := os.WriteFile(filepath.Join(dir, "helpers.star"), []byte(`source = "fs"`), 0o600); err != nil {
		t.Fatal(err)
	}

	// Inline version returns "inline".
	inline := map[string]string{
		"helpers.star": `source = "inline"`,
	}

	log := &testLogger{}
	rt := NewRuntime(log)
	loader := NewModuleLoader(inline, []string{dir}, starlark.StringDict{}, rt, "")

	thread := &starlark.Thread{Name: "test", Load: loader.LoadFunc()}
	loaded, err := thread.Load(thread, "helpers.star")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	src, ok := loaded["source"]
	if !ok {
		t.Fatal("expected 'source' in loaded globals")
	}

	if src.(starlark.String) != "inline" {
		t.Errorf("source = %v, want 'inline' (inline should take priority over filesystem)", src)
	}
}

func TestModuleCache(t *testing.T) {
	log := &testLogger{}
	rt := NewRuntime(log)

	inline := map[string]string{
		"helpers.star": `x = 42`,
	}

	loader := NewModuleLoader(inline, nil, starlark.StringDict{}, rt, "")
	thread := &starlark.Thread{Name: "test", Load: loader.LoadFunc()}

	g1, err := thread.Load(thread, "helpers.star")
	if err != nil {
		t.Fatalf("first load error: %v", err)
	}

	g2, err := thread.Load(thread, "helpers.star")
	if err != nil {
		t.Fatalf("second load error: %v", err)
	}

	// Both should have same value.
	x1, _ := starlark.AsInt32(g1["x"].(starlark.Int))
	x2, _ := starlark.AsInt32(g2["x"].(starlark.Int))
	if x1 != x2 {
		t.Errorf("cached values differ: %d vs %d", x1, x2)
	}

	// Verify it is the exact same StringDict (pointer equality via map identity).
	g1["__test_marker__"] = starlark.True
	if g2["__test_marker__"] != starlark.True {
		t.Error("second load returned different map -- cache not working")
	}
}

func TestModuleFrozen(t *testing.T) {
	log := &testLogger{}
	rt := NewRuntime(log)

	inline := map[string]string{
		"helpers.star": `data = [1, 2, 3]`,
	}

	loader := NewModuleLoader(inline, nil, starlark.StringDict{}, rt, "")
	thread := &starlark.Thread{Name: "test", Load: loader.LoadFunc()}

	loaded, err := thread.Load(thread, "helpers.star")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data := loaded["data"].(*starlark.List)
	err = data.Append(starlark.MakeInt(4))
	if err == nil {
		t.Error("expected error when mutating frozen list, got nil")
	}
}

func TestModuleCycle(t *testing.T) {
	log := &testLogger{}
	rt := NewRuntime(log)

	inline := map[string]string{
		"a.star": `load("b.star", "y"); x = 1`,
		"b.star": `load("a.star", "x"); y = 2`,
	}

	loader := NewModuleLoader(inline, nil, starlark.StringDict{}, rt, "")
	thread := &starlark.Thread{Name: "test", Load: loader.LoadFunc()}

	_, err := thread.Load(thread, "a.star")
	if err == nil {
		t.Fatal("expected cycle error, got nil")
	}

	if !strings.Contains(err.Error(), "cycle") {
		t.Errorf("error = %q, want it to contain 'cycle'", err.Error())
	}
}

func TestModuleTransitiveLoad(t *testing.T) {
	log := &testLogger{}
	rt := NewRuntime(log)

	inline := map[string]string{
		"a.star": `load("b.star", "b_val"); a_val = b_val + 1`,
		"b.star": `load("c.star", "c_val"); b_val = c_val + 1`,
		"c.star": `c_val = 10`,
	}

	loader := NewModuleLoader(inline, nil, starlark.StringDict{}, rt, "")
	thread := &starlark.Thread{Name: "test", Load: loader.LoadFunc()}

	loaded, err := thread.Load(thread, "a.star")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	aVal, _ := starlark.AsInt32(loaded["a_val"].(starlark.Int))
	if aVal != 12 {
		t.Errorf("a_val = %d, want 12 (10 + 1 + 1)", aVal)
	}
}

func TestModuleNameValidationSlash(t *testing.T) {
	log := &testLogger{}
	rt := NewRuntime(log)
	loader := NewModuleLoader(nil, nil, starlark.StringDict{}, rt, "")
	thread := &starlark.Thread{Name: "test", Load: loader.LoadFunc()}

	_, err := thread.Load(thread, "path/helpers.star")
	if err == nil {
		t.Fatal("expected error for slash in module name, got nil")
	}
	if !strings.Contains(err.Error(), "path separator") {
		t.Errorf("error = %q, want it to contain 'path separator'", err.Error())
	}
}

func TestModuleNameValidationBackslash(t *testing.T) {
	log := &testLogger{}
	rt := NewRuntime(log)
	loader := NewModuleLoader(nil, nil, starlark.StringDict{}, rt, "")
	thread := &starlark.Thread{Name: "test", Load: loader.LoadFunc()}

	_, err := thread.Load(thread, "path\\helpers.star")
	if err == nil {
		t.Fatal("expected error for backslash in module name, got nil")
	}
	if !strings.Contains(err.Error(), "path separator") {
		t.Errorf("error = %q, want it to contain 'path separator'", err.Error())
	}
}

func TestModuleNameValidationNoStar(t *testing.T) {
	log := &testLogger{}
	rt := NewRuntime(log)
	loader := NewModuleLoader(nil, nil, starlark.StringDict{}, rt, "")
	thread := &starlark.Thread{Name: "test", Load: loader.LoadFunc()}

	_, err := thread.Load(thread, "helpers")
	if err == nil {
		t.Fatal("expected error for missing .star suffix, got nil")
	}
	if !strings.Contains(err.Error(), ".star") {
		t.Errorf("error = %q, want it to contain '.star'", err.Error())
	}
}

func TestModuleNameValidationOCI(t *testing.T) {
	log := &testLogger{}
	rt := NewRuntime(log)
	// OCI module not in inline map -- should get "not resolved" error (not "not yet supported").
	loader := NewModuleLoader(nil, nil, starlark.StringDict{}, rt, "")
	thread := &starlark.Thread{Name: "test", Load: loader.LoadFunc()}

	_, err := thread.Load(thread, "oci://registry.example.com/repo:v1/module.star")
	if err == nil {
		t.Fatal("expected error for unresolved OCI module, got nil")
	}
	if !strings.Contains(err.Error(), "not resolved") {
		t.Errorf("error = %q, want it to contain 'not resolved'", err.Error())
	}
}

func TestModuleNotFound(t *testing.T) {
	dir := t.TempDir()
	log := &testLogger{}
	rt := NewRuntime(log)
	loader := NewModuleLoader(nil, []string{dir}, starlark.StringDict{}, rt, "")
	thread := &starlark.Thread{Name: "test", Load: loader.LoadFunc()}

	_, err := thread.Load(thread, "missing.star")
	if err == nil {
		t.Fatal("expected not-found error, got nil")
	}

	errStr := err.Error()
	if !strings.Contains(errStr, "not found") {
		t.Errorf("error = %q, want it to contain 'not found'", errStr)
	}
	if !strings.Contains(errStr, "inline modules") {
		t.Errorf("error = %q, want it to contain 'inline modules'", errStr)
	}
	if !strings.Contains(errStr, dir) {
		t.Errorf("error = %q, want it to contain search path %q", errStr, dir)
	}
}

func TestModuleReceivesBuiltins(t *testing.T) {
	log := &testLogger{}
	rt := NewRuntime(log)

	// Create a custom builtin function to simulate a predeclared.
	myBuiltin := starlark.NewBuiltin("my_builtin", func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
		return starlark.String("builtin_called"), nil
	})

	predeclared := starlark.StringDict{
		"my_builtin": myBuiltin,
	}

	inline := map[string]string{
		"helpers.star": `result = my_builtin()`,
	}

	loader := NewModuleLoader(inline, nil, predeclared, rt, "")
	thread := &starlark.Thread{Name: "test", Load: loader.LoadFunc()}

	loaded, err := thread.Load(thread, "helpers.star")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result, ok := loaded["result"]
	if !ok {
		t.Fatal("expected 'result' in loaded globals")
	}

	if result.(starlark.String) != "builtin_called" {
		t.Errorf("result = %v, want 'builtin_called'", result)
	}
}

func TestModuleErrorTrace(t *testing.T) {
	log := &testLogger{}
	rt := NewRuntime(log)

	inline := map[string]string{
		"bad.star": `
def boom():
    return 1 / 0
boom()
`,
	}

	loader := NewModuleLoader(inline, nil, starlark.StringDict{}, rt, "")
	thread := &starlark.Thread{Name: "test", Load: loader.LoadFunc()}

	_, err := thread.Load(thread, "bad.star")
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if !strings.Contains(err.Error(), "bad.star") {
		t.Errorf("error = %q, want it to contain 'bad.star'", err.Error())
	}
}

func TestModuleStepLimit(t *testing.T) {
	log := &testLogger{}
	rt := NewRuntime(log)

	inline := map[string]string{
		"infinite.star": `
while True:
    pass
`,
	}

	loader := NewModuleLoader(inline, nil, starlark.StringDict{}, rt, "")
	thread := &starlark.Thread{Name: "test", Load: loader.LoadFunc()}

	_, err := thread.Load(thread, "infinite.star")
	if err == nil {
		t.Fatal("expected step limit error, got nil")
	}

	// The error should indicate the step limit was exceeded.
	errStr := err.Error()
	if !strings.Contains(errStr, "exceeded execution limit") {
		t.Errorf("error = %q, want it to contain 'exceeded execution limit'", errStr)
	}
}

func TestModuleFileOptions(t *testing.T) {
	log := &testLogger{}
	rt := NewRuntime(log)

	inline := map[string]string{
		"control.star": `
result = []
for i in range(3):
    result.append(i)
if len(result) == 3:
    ok = True
`,
	}

	loader := NewModuleLoader(inline, nil, starlark.StringDict{}, rt, "")
	thread := &starlark.Thread{Name: "test", Load: loader.LoadFunc()}

	loaded, err := thread.Load(thread, "control.star")
	if err != nil {
		t.Fatalf("unexpected error (top-level control flow should be allowed): %v", err)
	}

	ok, found := loaded["ok"]
	if !found {
		t.Fatal("expected 'ok' in loaded globals")
	}
	if ok != starlark.True {
		t.Errorf("ok = %v, want True", ok)
	}
}

func TestModuleBytecodeCache(t *testing.T) {
	log := &testLogger{}
	rt := NewRuntime(log)

	inline := map[string]string{
		"helpers.star": `x = 42`,
	}

	cacheBefore := rt.CacheLen()

	loader := NewModuleLoader(inline, nil, starlark.StringDict{}, rt, "")
	thread := &starlark.Thread{Name: "test", Load: loader.LoadFunc()}

	_, err := thread.Load(thread, "helpers.star")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cacheAfter := rt.CacheLen()
	if cacheAfter <= cacheBefore {
		t.Errorf("CacheLen before=%d, after=%d; expected bytecode cache to grow", cacheBefore, cacheAfter)
	}
}

// ========================
// Star Import Tests
// ========================

func TestStarImportAllExports(t *testing.T) {
	log := &testLogger{}
	rt := NewRuntime(log)

	inline := map[string]string{
		"m.star": `x = 1
y = 2
_private = 3`,
	}

	loader := NewModuleLoader(inline, nil, starlark.StringDict{}, rt, "")

	// ResolveStarImports should expand "*" to "x", "y" (not _private).
	source := `load("m.star", "*")
result_x = x
result_y = y`
	rewritten, err := loader.ResolveStarImports(source, "test.star")
	if err != nil {
		t.Fatalf("ResolveStarImports error: %v", err)
	}

	// The rewritten source should not contain "*" and should contain x and y.
	if strings.Contains(rewritten, `"*"`) {
		t.Errorf("rewritten source still contains \"*\": %s", rewritten)
	}

	// Execute the rewritten source to verify names are bound.
	// Load bindings aren't in returned globals, so we use them by assigning to new globals.
	thread := &starlark.Thread{Name: "test", Load: loader.LoadFunc()}
	thread.SetMaxExecutionSteps(maxSteps)
	globals, err := starlark.ExecFileOptions(fileOptions(), thread, "test.star", rewritten, starlark.StringDict{})
	if err != nil {
		t.Fatalf("executing rewritten source: %v", err)
	}

	xVal, _ := starlark.AsInt32(globals["result_x"].(starlark.Int))
	if xVal != 1 {
		t.Errorf("x = %d, want 1", xVal)
	}
	yVal, _ := starlark.AsInt32(globals["result_y"].(starlark.Int))
	if yVal != 2 {
		t.Errorf("y = %d, want 2", yVal)
	}
}

func TestStarImportEmptyModule(t *testing.T) {
	log := &testLogger{}
	rt := NewRuntime(log)

	inline := map[string]string{
		"empty.star": `_hidden = 1`, // no public exports
	}

	loader := NewModuleLoader(inline, nil, starlark.StringDict{}, rt, "")

	source := `load("empty.star", "*")`
	rewritten, err := loader.ResolveStarImports(source, "test.star")
	if err != nil {
		t.Fatalf("ResolveStarImports error: %v", err)
	}

	// Should produce a valid source (empty load removed or no bindings).
	thread := &starlark.Thread{Name: "test", Load: loader.LoadFunc()}
	thread.SetMaxExecutionSteps(maxSteps)
	_, err = starlark.ExecFileOptions(fileOptions(), thread, "test.star", rewritten, starlark.StringDict{})
	if err != nil {
		t.Fatalf("executing rewritten source for empty module: %v", err)
	}
}

func TestStarImportMixedNamedAndStar(t *testing.T) {
	log := &testLogger{}
	rt := NewRuntime(log)

	inline := map[string]string{
		"m.star": `a = 1
b = 2
c = 3`,
	}

	loader := NewModuleLoader(inline, nil, starlark.StringDict{}, rt, "")

	// load("m.star", "a", "*") -- named "a" plus star for remaining exports.
	source := `load("m.star", "a", "*")
result = a + b + c`
	rewritten, err := loader.ResolveStarImports(source, "test.star")
	if err != nil {
		t.Fatalf("ResolveStarImports error: %v", err)
	}

	if strings.Contains(rewritten, `"*"`) {
		t.Errorf("rewritten source still contains \"*\": %s", rewritten)
	}

	thread := &starlark.Thread{Name: "test", Load: loader.LoadFunc()}
	thread.SetMaxExecutionSteps(maxSteps)
	globals, err := starlark.ExecFileOptions(fileOptions(), thread, "test.star", rewritten, starlark.StringDict{})
	if err != nil {
		t.Fatalf("executing rewritten source: %v", err)
	}

	// a + b + c = 1 + 2 + 3 = 6
	result, _ := starlark.AsInt32(globals["result"].(starlark.Int))
	if result != 6 {
		t.Errorf("result = %d, want 6 (a+b+c)", result)
	}
}

func TestStarImportNoStarUnchanged(t *testing.T) {
	log := &testLogger{}
	rt := NewRuntime(log)

	inline := map[string]string{
		"m.star": `x = 1`,
	}

	loader := NewModuleLoader(inline, nil, starlark.StringDict{}, rt, "")

	source := `load("m.star", "x")`
	rewritten, err := loader.ResolveStarImports(source, "test.star")
	if err != nil {
		t.Fatalf("ResolveStarImports error: %v", err)
	}

	if rewritten != source {
		t.Errorf("expected source unchanged when no star import, got %q", rewritten)
	}
}

func TestStarImportFilesystem(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "fs.star"), []byte(`a = 10
b = 20`), 0o600); err != nil {
		t.Fatal(err)
	}

	log := &testLogger{}
	rt := NewRuntime(log)
	loader := NewModuleLoader(nil, []string{dir}, starlark.StringDict{}, rt, "")

	source := `load("fs.star", "*")
result = a + b`
	rewritten, err := loader.ResolveStarImports(source, "test.star")
	if err != nil {
		t.Fatalf("ResolveStarImports error: %v", err)
	}

	thread := &starlark.Thread{Name: "test", Load: loader.LoadFunc()}
	thread.SetMaxExecutionSteps(maxSteps)
	globals, err := starlark.ExecFileOptions(fileOptions(), thread, "test.star", rewritten, starlark.StringDict{})
	if err != nil {
		t.Fatalf("executing rewritten source: %v", err)
	}

	result, _ := starlark.AsInt32(globals["result"].(starlark.Int))
	if result != 30 {
		t.Errorf("result = %d, want 30 (a+b)", result)
	}
}

// ========================
// OCI Module Routing Tests
// ========================

func TestOCIModuleRouting(t *testing.T) {
	log := &testLogger{}
	rt := NewRuntime(log)

	// Pre-populate inline modules with the full OCI URL key (as fn.go
	// injects after OCI resolution with namespaced keys).
	inline := map[string]string{
		"oci://ghcr.io/org/lib:v1/helpers.star": `def greet(name): return "oci-hello " + name`,
	}

	loader := NewModuleLoader(inline, nil, starlark.StringDict{}, rt, "")
	thread := &starlark.Thread{Name: "test", Load: loader.LoadFunc()}

	// Load using oci:// URL -- should resolve to inline module by base filename.
	loaded, err := thread.Load(thread, "oci://ghcr.io/org/lib:v1/helpers.star")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	greet, ok := loaded["greet"]
	if !ok {
		t.Fatal("expected 'greet' in loaded globals")
	}

	result, err := starlark.Call(thread, greet, starlark.Tuple{starlark.String("world")}, nil)
	if err != nil {
		t.Fatalf("calling greet: %v", err)
	}

	if result.(starlark.String) != "oci-hello world" {
		t.Errorf("greet('world') = %v, want 'oci-hello world'", result)
	}
}

func TestOCIModuleNotResolved(t *testing.T) {
	log := &testLogger{}
	rt := NewRuntime(log)

	// No inline modules -- OCI module not pre-resolved.
	loader := NewModuleLoader(nil, nil, starlark.StringDict{}, rt, "")
	thread := &starlark.Thread{Name: "test", Load: loader.LoadFunc()}

	_, err := thread.Load(thread, "oci://ghcr.io/org/lib:v1/missing.star")
	if err == nil {
		t.Fatal("expected error for unresolved OCI module, got nil")
	}

	if !strings.Contains(err.Error(), "not resolved") {
		t.Errorf("error = %q, want it to contain 'not resolved'", err.Error())
	}
	if !strings.Contains(err.Error(), "missing.star") {
		t.Errorf("error = %q, want it to contain 'missing.star'", err.Error())
	}
}

func TestOCIModuleDigestRouting(t *testing.T) {
	log := &testLogger{}
	rt := NewRuntime(log)

	inline := map[string]string{
		"oci://ghcr.io/org/lib@sha256:abc123def456abc123def456abc123def456abc123def456abc123def456abc1/utils.star": `val = "pinned"`,
	}

	loader := NewModuleLoader(inline, nil, starlark.StringDict{}, rt, "")
	thread := &starlark.Thread{Name: "test", Load: loader.LoadFunc()}

	loaded, err := thread.Load(thread, "oci://ghcr.io/org/lib@sha256:abc123def456abc123def456abc123def456abc123def456abc123def456abc1/utils.star")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	v, ok := loaded["val"]
	if !ok {
		t.Fatal("expected 'val' in loaded globals")
	}
	if v.(starlark.String) != "pinned" {
		t.Errorf("val = %v, want 'pinned'", v)
	}
}

func TestModulePrivateNames(t *testing.T) {
	log := &testLogger{}
	rt := NewRuntime(log)

	inline := map[string]string{
		"m.star": `
_private = "secret"
public = "visible"
`,
	}

	loader := NewModuleLoader(inline, nil, starlark.StringDict{}, rt, "")
	thread := &starlark.Thread{Name: "test", Load: loader.LoadFunc()}

	loaded, err := thread.Load(thread, "m.star")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Public should be exported.
	if _, ok := loaded["public"]; !ok {
		t.Error("expected 'public' in loaded globals")
	}

	// Private (underscore-prefixed) should NOT be exported.
	if _, ok := loaded["_private"]; ok {
		t.Error("underscore-prefixed '_private' should NOT be exported by load()")
	}
}

// ========================
// Coverage Gap Tests
// ========================

func TestModuleCachedError(t *testing.T) {
	log := &testLogger{}
	rt := NewRuntime(log)
	loader := NewModuleLoader(nil, nil, starlark.StringDict{}, rt, "")
	thread := &starlark.Thread{Name: "test", Load: loader.LoadFunc()}

	// First load: module not found.
	_, err1 := thread.Load(thread, "missing.star")
	if err1 == nil {
		t.Fatal("expected error on first load, got nil")
	}

	// Second load: should return cached error (same message).
	_, err2 := thread.Load(thread, "missing.star")
	if err2 == nil {
		t.Fatal("expected error on second load, got nil")
	}

	if err1.Error() != err2.Error() {
		t.Errorf("cached error differs:\n  first:  %q\n  second: %q", err1.Error(), err2.Error())
	}
}

func TestModuleCompilationError(t *testing.T) {
	log := &testLogger{}
	rt := NewRuntime(log)

	inline := map[string]string{
		"bad.star": `def f(`,
	}

	loader := NewModuleLoader(inline, nil, starlark.StringDict{}, rt, "")
	thread := &starlark.Thread{Name: "test", Load: loader.LoadFunc()}

	_, err := thread.Load(thread, "bad.star")
	if err == nil {
		t.Fatal("expected compilation error, got nil")
	}

	if !strings.Contains(err.Error(), "compiling module") {
		t.Errorf("error = %q, want it to contain 'compiling module'", err.Error())
	}
}

func TestModuleFilesystemPermissionError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secret.star")
	if err := os.WriteFile(path, []byte(`x = 1`), 0o000); err != nil {
		t.Fatal(err)
	}

	log := &testLogger{}
	rt := NewRuntime(log)
	loader := NewModuleLoader(nil, []string{dir}, starlark.StringDict{}, rt, "")
	thread := &starlark.Thread{Name: "test", Load: loader.LoadFunc()}

	_, err := thread.Load(thread, "secret.star")
	if err == nil {
		// On some systems (e.g., root), permission errors may not occur.
		t.Skip("file was readable despite 0o000 permissions (likely running as root)")
	}

	if !strings.Contains(err.Error(), "reading module") {
		t.Errorf("error = %q, want it to contain 'reading module'", err.Error())
	}
}

func TestModuleMultipleSearchPaths(t *testing.T) {
	dir1 := t.TempDir() // Empty -- module not here.
	dir2 := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir2, "helpers.star"), []byte(`val = "found"`), 0o600); err != nil {
		t.Fatal(err)
	}

	log := &testLogger{}
	rt := NewRuntime(log)
	loader := NewModuleLoader(nil, []string{dir1, dir2}, starlark.StringDict{}, rt, "")
	thread := &starlark.Thread{Name: "test", Load: loader.LoadFunc()}

	loaded, err := thread.Load(thread, "helpers.star")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	v, ok := loaded["val"]
	if !ok {
		t.Fatal("expected 'val' in loaded globals")
	}
	if v.(starlark.String) != "found" {
		t.Errorf("val = %v, want 'found'", v)
	}
}

func TestModuleEmptyName(t *testing.T) {
	log := &testLogger{}
	rt := NewRuntime(log)
	loader := NewModuleLoader(nil, nil, starlark.StringDict{}, rt, "")
	thread := &starlark.Thread{Name: "test", Load: loader.LoadFunc()}

	_, err := thread.Load(thread, "")
	if err == nil {
		t.Fatal("expected error for empty module name, got nil")
	}
	if !strings.Contains(err.Error(), ".star") {
		t.Errorf("error = %q, want it to contain '.star'", err.Error())
	}
}

// ========================
// Default Registry Tests
// ========================

func TestModuleDefaultRegistryExpansion(t *testing.T) {
	log := &testLogger{}
	rt := NewRuntime(log)

	// Pre-populate inline modules with the full OCI URL key (as fn.go
	// injects after OCI resolution with namespaced keys).
	inline := map[string]string{
		"oci://ghcr.io/wompipomp/function-starlark-stdlib:v1/naming.star": `def resource_name(n): return "prefix-" + n`,
	}

	loader := NewModuleLoader(inline, nil, starlark.StringDict{}, rt, "ghcr.io/wompipomp")
	thread := &starlark.Thread{Name: "test", Load: loader.LoadFunc()}

	// Load using short-form -- should expand to oci:// and route to inline module by base filename.
	loaded, err := thread.Load(thread, "function-starlark-stdlib:v1/naming.star")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	fn, ok := loaded["resource_name"]
	if !ok {
		t.Fatal("expected 'resource_name' in loaded globals")
	}

	result, err := starlark.Call(thread, fn, starlark.Tuple{starlark.String("test")}, nil)
	if err != nil {
		t.Fatalf("calling resource_name: %v", err)
	}

	if result.(starlark.String) != "prefix-test" {
		t.Errorf("resource_name('test') = %v, want 'prefix-test'", result)
	}
}

func TestModuleDefaultRegistryNotConfigured(t *testing.T) {
	log := &testLogger{}
	rt := NewRuntime(log)

	loader := NewModuleLoader(nil, nil, starlark.StringDict{}, rt, "")
	thread := &starlark.Thread{Name: "test", Load: loader.LoadFunc()}

	_, err := thread.Load(thread, "function-starlark-stdlib:v1/naming.star")
	if err == nil {
		t.Fatal("expected error for short-form load without default registry, got nil")
	}

	errStr := err.Error()
	if !strings.Contains(errStr, "requires a default OCI registry") {
		t.Errorf("error = %q, want it to contain 'requires a default OCI registry'", errStr)
	}
	if !strings.Contains(errStr, "STARLARK_OCI_DEFAULT_REGISTRY") {
		t.Errorf("error = %q, want it to contain 'STARLARK_OCI_DEFAULT_REGISTRY'", errStr)
	}
	if !strings.Contains(errStr, "spec.ociDefaultRegistry") {
		t.Errorf("error = %q, want it to contain 'spec.ociDefaultRegistry'", errStr)
	}
}

func TestResolveStarImportsWithDefaultRegistry(t *testing.T) {
	log := &testLogger{}
	rt := NewRuntime(log)

	// Inline module keyed by full OCI URL (as fn.go injects after OCI resolution).
	inline := map[string]string{
		"oci://ghcr.io/wompipomp/function-starlark-stdlib:v1/naming.star": `resource_name = "rn"
helper = "h"
_private = "p"`,
	}

	loader := NewModuleLoader(inline, nil, starlark.StringDict{}, rt, "ghcr.io/wompipomp")

	// Short-form OCI target with star import.
	source := `load("function-starlark-stdlib:v1/naming.star", "*")
result_rn = resource_name
result_h = helper`
	rewritten, err := loader.ResolveStarImports(source, "test.star")
	if err != nil {
		t.Fatalf("ResolveStarImports error: %v", err)
	}

	// Star should be expanded -- no "*" in rewritten source.
	if strings.Contains(rewritten, `"*"`) {
		t.Errorf("rewritten source still contains \"*\": %s", rewritten)
	}

	// The module name in the load statement should remain the short-form
	// (not expanded to oci://), because only the resolution path expands.
	if !strings.Contains(rewritten, "function-starlark-stdlib:v1/naming.star") {
		t.Errorf("rewritten source lost module name: %s", rewritten)
	}

	// Execute the rewritten source to verify the exports are bound.
	thread := &starlark.Thread{Name: "test", Load: loader.LoadFunc()}
	thread.SetMaxExecutionSteps(maxSteps)
	globals, err := starlark.ExecFileOptions(fileOptions(), thread, "test.star", rewritten, starlark.StringDict{})
	if err != nil {
		t.Fatalf("executing rewritten source: %v", err)
	}

	if globals["result_rn"].(starlark.String) != "rn" {
		t.Errorf("resource_name = %v, want 'rn'", globals["result_rn"])
	}
	if globals["result_h"].(starlark.String) != "h" {
		t.Errorf("helper = %v, want 'h'", globals["result_h"])
	}
	// _private should NOT be exported.
	if _, ok := globals["_private"]; ok {
		t.Error("_private should not be exported via star import")
	}
}

// ========================
// Namespace Star Import Tests
// ========================

func TestNamespaceStarImportBasic(t *testing.T) {
	log := &testLogger{}
	rt := NewRuntime(log)

	inline := map[string]string{
		"m.star": `x = 1
y = 2
_private = 3`,
	}

	predeclared := starlark.StringDict{
		"struct": starlark.NewBuiltin("struct", starlarkstruct.Make),
	}

	loader := NewModuleLoader(inline, nil, predeclared, rt, "")

	source := `load("m.star", ns="*")
result_x = ns.x
result_y = ns.y`
	rewritten, err := loader.ResolveStarImports(source, "test.star")
	if err != nil {
		t.Fatalf("ResolveStarImports error: %v", err)
	}

	// The rewritten source should not contain "*".
	if strings.Contains(rewritten, `"*"`) {
		t.Errorf("rewritten source still contains \"*\": %s", rewritten)
	}

	// Execute the rewritten source.
	thread := &starlark.Thread{Name: "test", Load: loader.LoadFunc()}
	thread.SetMaxExecutionSteps(maxSteps)
	globals, err := starlark.ExecFileOptions(fileOptions(), thread, "test.star", rewritten, predeclared)
	if err != nil {
		t.Fatalf("executing rewritten source: %v", err)
	}

	xVal, _ := starlark.AsInt32(globals["result_x"].(starlark.Int))
	if xVal != 1 {
		t.Errorf("ns.x = %d, want 1", xVal)
	}
	yVal, _ := starlark.AsInt32(globals["result_y"].(starlark.Int))
	if yVal != 2 {
		t.Errorf("ns.y = %d, want 2", yVal)
	}
}

func TestNamespaceStarImportNoCollision(t *testing.T) {
	log := &testLogger{}
	rt := NewRuntime(log)

	inline := map[string]string{
		"m1.star": `Account = "m1_account"`,
		"m2.star": `Account = "m2_account"`,
	}

	predeclared := starlark.StringDict{
		"struct": starlark.NewBuiltin("struct", starlarkstruct.Make),
	}

	loader := NewModuleLoader(inline, nil, predeclared, rt, "")

	source := `load("m1.star", s1="*")
load("m2.star", s2="*")
result1 = s1.Account
result2 = s2.Account`
	rewritten, err := loader.ResolveStarImports(source, "test.star")
	if err != nil {
		t.Fatalf("ResolveStarImports error: %v", err)
	}

	thread := &starlark.Thread{Name: "test", Load: loader.LoadFunc()}
	thread.SetMaxExecutionSteps(maxSteps)
	globals, err := starlark.ExecFileOptions(fileOptions(), thread, "test.star", rewritten, predeclared)
	if err != nil {
		t.Fatalf("executing rewritten source: %v", err)
	}

	r1 := string(globals["result1"].(starlark.String))
	r2 := string(globals["result2"].(starlark.String))
	if r1 != "m1_account" {
		t.Errorf("s1.Account = %q, want 'm1_account'", r1)
	}
	if r2 != "m2_account" {
		t.Errorf("s2.Account = %q, want 'm2_account'", r2)
	}
	if r1 == r2 {
		t.Error("s1.Account and s2.Account should differ but they are the same")
	}
}

func TestNamespaceStarImportMixedExplicit(t *testing.T) {
	log := &testLogger{}
	rt := NewRuntime(log)

	inline := map[string]string{
		"m.star": `a = 1
b = 2
extra = 3`,
	}

	predeclared := starlark.StringDict{
		"struct": starlark.NewBuiltin("struct", starlarkstruct.Make),
	}

	loader := NewModuleLoader(inline, nil, predeclared, rt, "")

	// Mixed: namespace star + explicit import.
	source := `load("m.star", ns="*", "extra")
result_a = ns.a
result_b = ns.b
result_extra = extra`
	rewritten, err := loader.ResolveStarImports(source, "test.star")
	if err != nil {
		t.Fatalf("ResolveStarImports error: %v", err)
	}

	thread := &starlark.Thread{Name: "test", Load: loader.LoadFunc()}
	thread.SetMaxExecutionSteps(maxSteps)
	globals, err := starlark.ExecFileOptions(fileOptions(), thread, "test.star", rewritten, predeclared)
	if err != nil {
		t.Fatalf("executing rewritten source: %v", err)
	}

	aVal, _ := starlark.AsInt32(globals["result_a"].(starlark.Int))
	if aVal != 1 {
		t.Errorf("ns.a = %d, want 1", aVal)
	}
	bVal, _ := starlark.AsInt32(globals["result_b"].(starlark.Int))
	if bVal != 2 {
		t.Errorf("ns.b = %d, want 2", bVal)
	}
	extraVal, _ := starlark.AsInt32(globals["result_extra"].(starlark.Int))
	if extraVal != 3 {
		t.Errorf("extra = %d, want 3", extraVal)
	}
}

func TestNamespaceStarImportEmpty(t *testing.T) {
	log := &testLogger{}
	rt := NewRuntime(log)

	inline := map[string]string{
		"empty.star": `_private = 1`, // no public exports
	}

	predeclared := starlark.StringDict{
		"struct": starlark.NewBuiltin("struct", starlarkstruct.Make),
	}

	loader := NewModuleLoader(inline, nil, predeclared, rt, "")

	source := `load("empty.star", ns="*")
result = ns`
	rewritten, err := loader.ResolveStarImports(source, "test.star")
	if err != nil {
		t.Fatalf("ResolveStarImports error: %v", err)
	}

	thread := &starlark.Thread{Name: "test", Load: loader.LoadFunc()}
	thread.SetMaxExecutionSteps(maxSteps)
	globals, err := starlark.ExecFileOptions(fileOptions(), thread, "test.star", rewritten, predeclared)
	if err != nil {
		t.Fatalf("executing rewritten source for empty module: %v", err)
	}

	// The namespace should be an empty struct.
	ns := globals["result"]
	if ns == nil {
		t.Fatal("expected 'result' (ns) in globals, got nil")
	}
	if ns.Type() != "struct" {
		t.Errorf("ns.Type() = %q, want 'struct'", ns.Type())
	}
}

func TestNamespaceStarImportOCI(t *testing.T) {
	log := &testLogger{}
	rt := NewRuntime(log)

	inline := map[string]string{
		"oci://ghcr.io/org/lib:v1/helpers.star": `greet = "hello"
farewell = "bye"`,
	}

	predeclared := starlark.StringDict{
		"struct": starlark.NewBuiltin("struct", starlarkstruct.Make),
	}

	loader := NewModuleLoader(inline, nil, predeclared, rt, "")

	source := `load("oci://ghcr.io/org/lib:v1/helpers.star", ns="*")
result = ns.greet`
	rewritten, err := loader.ResolveStarImports(source, "test.star")
	if err != nil {
		t.Fatalf("ResolveStarImports error: %v", err)
	}

	// The OCI URL should be preserved in the rewritten load.
	if !strings.Contains(rewritten, "oci://ghcr.io/org/lib:v1/helpers.star") {
		t.Errorf("rewritten source lost OCI URL: %s", rewritten)
	}

	thread := &starlark.Thread{Name: "test", Load: loader.LoadFunc()}
	thread.SetMaxExecutionSteps(maxSteps)
	globals, err := starlark.ExecFileOptions(fileOptions(), thread, "test.star", rewritten, predeclared)
	if err != nil {
		t.Fatalf("executing rewritten source: %v", err)
	}

	result := string(globals["result"].(starlark.String))
	if result != "hello" {
		t.Errorf("ns.greet = %q, want 'hello'", result)
	}
}

func TestNamespaceAndPlainStarSameLoad(t *testing.T) {
	log := &testLogger{}
	rt := NewRuntime(log)

	inline := map[string]string{
		"m.star": `x = 1
y = 2`,
	}

	predeclared := starlark.StringDict{
		"struct": starlark.NewBuiltin("struct", starlarkstruct.Make),
	}

	loader := NewModuleLoader(inline, nil, predeclared, rt, "")

	// Both plain star and namespace star in same load.
	source := `load("m.star", ns="*", "*")
result_direct_x = x
result_direct_y = y
result_ns_x = ns.x
result_ns_y = ns.y`
	rewritten, err := loader.ResolveStarImports(source, "test.star")
	if err != nil {
		t.Fatalf("ResolveStarImports error: %v", err)
	}

	thread := &starlark.Thread{Name: "test", Load: loader.LoadFunc()}
	thread.SetMaxExecutionSteps(maxSteps)
	globals, err := starlark.ExecFileOptions(fileOptions(), thread, "test.star", rewritten, predeclared)
	if err != nil {
		t.Fatalf("executing rewritten source: %v", err)
	}

	dxVal, _ := starlark.AsInt32(globals["result_direct_x"].(starlark.Int))
	if dxVal != 1 {
		t.Errorf("x (direct) = %d, want 1", dxVal)
	}
	dyVal, _ := starlark.AsInt32(globals["result_direct_y"].(starlark.Int))
	if dyVal != 2 {
		t.Errorf("y (direct) = %d, want 2", dyVal)
	}
	nxVal, _ := starlark.AsInt32(globals["result_ns_x"].(starlark.Int))
	if nxVal != 1 {
		t.Errorf("ns.x = %d, want 1", nxVal)
	}
	nyVal, _ := starlark.AsInt32(globals["result_ns_y"].(starlark.Int))
	if nyVal != 2 {
		t.Errorf("ns.y = %d, want 2", nyVal)
	}
}

// ========================
// Transitive Star Import Tests
// ========================

// TestTransitiveStarImportInModule verifies that a module which itself contains
// load("x.star", "*") can be loaded without error. Previously, star imports
// inside modules were not expanded before compilation, causing starlark-go to
// look for a literal "*" key which does not exist.
func TestTransitiveStarImportInModule(t *testing.T) {
	log := &testLogger{}
	rt := NewRuntime(log)

	inline := map[string]string{
		// naming.star: simple value export
		"naming.star": `upper = "ACME"`,
		// platform.star: uses star import from naming.star; load is on its own line
		"platform.star": "load(\"naming.star\", \"*\")\nplatform_val = upper + \"_platform\"",
	}

	loader := NewModuleLoader(inline, nil, starlark.StringDict{}, rt, "")
	thread := &starlark.Thread{Name: "test", Load: loader.LoadFunc()}

	loaded, err := thread.Load(thread, "platform.star")
	if err != nil {
		t.Fatalf("loading platform.star (which uses load(\"naming.star\", \"*\")): %v", err)
	}

	v, ok := loaded["platform_val"]
	if !ok {
		t.Fatal("expected 'platform_val' in loaded globals")
	}
	if v.(starlark.String) != "ACME_platform" {
		t.Errorf("platform_val = %v, want 'ACME_platform'", v)
	}
}

// TestDiamondDependencyDedup verifies that when two modules both import a shared
// module (diamond pattern), the shared module executes exactly once (cache hit).
func TestDiamondDependencyDedup(t *testing.T) {
	log := &testLogger{}
	rt := NewRuntime(log)

	inline := map[string]string{
		"naming.star":   `val = 1`,
		"platform.star": `load("naming.star", "val"); p = val + 10`,
		"custom.star":   `load("naming.star", "val"); c = val + 20`,
	}

	loader := NewModuleLoader(inline, nil, starlark.StringDict{}, rt, "")
	thread := &starlark.Thread{Name: "test", Load: loader.LoadFunc()}

	// Load platform.star -- triggers load of naming.star (cached as side effect)
	pGlobals, err := thread.Load(thread, "platform.star")
	if err != nil {
		t.Fatalf("loading platform.star: %v", err)
	}
	pVal, _ := starlark.AsInt32(pGlobals["p"].(starlark.Int))
	if pVal != 11 {
		t.Errorf("p = %d, want 11 (val+10 = 1+10)", pVal)
	}

	// Load custom.star -- naming.star should be a cache hit
	cGlobals, err := thread.Load(thread, "custom.star")
	if err != nil {
		t.Fatalf("loading custom.star: %v", err)
	}
	cVal, _ := starlark.AsInt32(cGlobals["c"].(starlark.Int))
	if cVal != 21 {
		t.Errorf("c = %d, want 21 (val+20 = 1+20)", cVal)
	}

	// Verify naming.star cache entry exists (deduplication working).
	if _, ok := loader.cache["naming.star"]; !ok {
		t.Error("naming.star should be in loader cache after diamond dependency")
	}
}

// TestTransitiveStarImportDiamond verifies the diamond pattern when both
// intermediate modules use load("naming.star", "*") instead of named imports.
func TestTransitiveStarImportDiamond(t *testing.T) {
	log := &testLogger{}
	rt := NewRuntime(log)

	inline := map[string]string{
		"naming.star":   "val = 1",
		"platform.star": "load(\"naming.star\", \"*\")\np = val + 10",
		"custom.star":   "load(\"naming.star\", \"*\")\nc = val + 20",
	}

	loader := NewModuleLoader(inline, nil, starlark.StringDict{}, rt, "")
	thread := &starlark.Thread{Name: "test", Load: loader.LoadFunc()}

	pGlobals, err := thread.Load(thread, "platform.star")
	if err != nil {
		t.Fatalf("loading platform.star (with star import of naming.star): %v", err)
	}
	pVal, _ := starlark.AsInt32(pGlobals["p"].(starlark.Int))
	if pVal != 11 {
		t.Errorf("p = %d, want 11 (val+10 = 1+10)", pVal)
	}

	cGlobals, err := thread.Load(thread, "custom.star")
	if err != nil {
		t.Fatalf("loading custom.star (with star import of naming.star): %v", err)
	}
	cVal, _ := starlark.AsInt32(cGlobals["c"].(starlark.Int))
	if cVal != 21 {
		t.Errorf("c = %d, want 21 (val+20 = 1+20)", cVal)
	}
}

func TestNamespaceStarImportMultiline(t *testing.T) {
	log := &testLogger{}
	rt := NewRuntime(log)

	inline := map[string]string{
		"m.star": `a = 10
b = 20`,
	}

	predeclared := starlark.StringDict{
		"struct": starlark.NewBuiltin("struct", starlarkstruct.Make),
	}

	loader := NewModuleLoader(inline, nil, predeclared, rt, "")

	// Multiline load statement with namespace star.
	source := `load(
    "m.star",
    ns="*",
)
result = ns.a + ns.b`
	rewritten, err := loader.ResolveStarImports(source, "test.star")
	if err != nil {
		t.Fatalf("ResolveStarImports error: %v", err)
	}

	thread := &starlark.Thread{Name: "test", Load: loader.LoadFunc()}
	thread.SetMaxExecutionSteps(maxSteps)
	globals, err := starlark.ExecFileOptions(fileOptions(), thread, "test.star", rewritten, predeclared)
	if err != nil {
		t.Fatalf("executing rewritten source: %v", err)
	}

	result, _ := starlark.AsInt32(globals["result"].(starlark.Int))
	if result != 30 {
		t.Errorf("result = %d, want 30 (a+b)", result)
	}
}
