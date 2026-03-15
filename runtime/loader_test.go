package runtime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.starlark.net/starlark"
)

func TestModuleLoadInline(t *testing.T) {
	log := &testLogger{}
	rt := NewRuntime(log)

	inline := map[string]string{
		"helpers.star": `def greet(name): return "hello " + name`,
	}

	loader := NewModuleLoader(inline, nil, starlark.StringDict{}, rt)
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
	if err := os.WriteFile(filepath.Join(dir, "helpers.star"), []byte(`def greet(name): return "hi " + name`), 0o644); err != nil {
		t.Fatal(err)
	}

	log := &testLogger{}
	rt := NewRuntime(log)
	loader := NewModuleLoader(nil, []string{dir}, starlark.StringDict{}, rt)

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
	if err := os.WriteFile(filepath.Join(dir, "helpers.star"), []byte(`source = "fs"`), 0o644); err != nil {
		t.Fatal(err)
	}

	// Inline version returns "inline".
	inline := map[string]string{
		"helpers.star": `source = "inline"`,
	}

	log := &testLogger{}
	rt := NewRuntime(log)
	loader := NewModuleLoader(inline, []string{dir}, starlark.StringDict{}, rt)

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

	loader := NewModuleLoader(inline, nil, starlark.StringDict{}, rt)
	thread := &starlark.Thread{Name: "test", Load: loader.LoadFunc()}

	g1, err := thread.Load(thread, "helpers.star")
	if err != nil {
		t.Fatalf("first load error: %v", err)
	}

	g2, err := thread.Load(thread, "helpers.star")
	if err != nil {
		t.Fatalf("second load error: %v", err)
	}

	// Cached: same map instance.
	if &g1 == nil || &g2 == nil {
		t.Fatal("nil globals")
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

	loader := NewModuleLoader(inline, nil, starlark.StringDict{}, rt)
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

	loader := NewModuleLoader(inline, nil, starlark.StringDict{}, rt)
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

	loader := NewModuleLoader(inline, nil, starlark.StringDict{}, rt)
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
	loader := NewModuleLoader(nil, nil, starlark.StringDict{}, rt)
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
	loader := NewModuleLoader(nil, nil, starlark.StringDict{}, rt)
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
	loader := NewModuleLoader(nil, nil, starlark.StringDict{}, rt)
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
	loader := NewModuleLoader(nil, nil, starlark.StringDict{}, rt)
	thread := &starlark.Thread{Name: "test", Load: loader.LoadFunc()}

	_, err := thread.Load(thread, "oci://registry.example.com/module.star")
	if err == nil {
		t.Fatal("expected error for OCI reference, got nil")
	}
	if !strings.Contains(err.Error(), "OCI module loading is not yet supported") {
		t.Errorf("error = %q, want it to contain OCI hint", err.Error())
	}
}

func TestModuleNotFound(t *testing.T) {
	dir := t.TempDir()
	log := &testLogger{}
	rt := NewRuntime(log)
	loader := NewModuleLoader(nil, []string{dir}, starlark.StringDict{}, rt)
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

	loader := NewModuleLoader(inline, nil, predeclared, rt)
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

	loader := NewModuleLoader(inline, nil, starlark.StringDict{}, rt)
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

	loader := NewModuleLoader(inline, nil, starlark.StringDict{}, rt)
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

	loader := NewModuleLoader(inline, nil, starlark.StringDict{}, rt)
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

	loader := NewModuleLoader(inline, nil, starlark.StringDict{}, rt)
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

func TestModulePrivateNames(t *testing.T) {
	log := &testLogger{}
	rt := NewRuntime(log)

	inline := map[string]string{
		"m.star": `
_private = "secret"
public = "visible"
`,
	}

	loader := NewModuleLoader(inline, nil, starlark.StringDict{}, rt)
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
