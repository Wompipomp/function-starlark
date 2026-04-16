package builtins

import (
	"strings"
	"testing"

	"github.com/crossplane/function-sdk-go/logging"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/wompipomp/function-starlark/runtime"
)

// ---------------------------------------------------------------------------
// Phase 30 Plan 03 — builtins/dict_test.go
//
// Three-layer test coverage for the `dict` module:
//
//   Layer 1 (unit on BuildGlobals output):
//     - TestBuildGlobals_DictModule
//
//   Layer 2 (in-process via Runtime.Execute):
//     - TestDict_Merge
//     - TestDict_DeepMerge
//     - TestDict_Pick
//     - TestDict_Omit
//     - TestDict_Dig
//     - TestDict_HasPath
//     - TestDict_NegativeCases
//     - TestDict_CrossType (oxr interop with frozen *convert.StarlarkDict)
//
// No Layer 3 (protobuf round-trip) needed for dict — outputs are plain
// *starlark.Dict values.
//
// Fixtures are all inline Go string literals (no external fixture files)
// per 29-CONTEXT.md §Fixture placement.
// ---------------------------------------------------------------------------

// runDictScript compiles and runs a Starlark source string against the full
// BuildGlobals predeclared set (which includes `dict`) via Runtime.Execute,
// returning the post-execution globals. Fails the test on any error.
func runDictScript(t *testing.T, src string) starlark.StringDict {
	t.Helper()
	req := makeReq(nil, nil, nil)
	c := NewCollector(NewConditionCollector(), "test.star", nil, nil)
	globals, err := testBuildGlobals(req, c)
	if err != nil {
		t.Fatalf("testBuildGlobals error: %v", err)
	}
	rt := runtime.NewRuntime(logging.NewNopLogger())
	out, err := rt.Execute(src, globals, "test.star", nil)
	if err != nil {
		t.Fatalf("rt.Execute error: %v\nsource:\n%s", err, src)
	}
	return out
}

// runDictScriptExpectError runs a Starlark source string via Runtime.Execute,
// expecting a non-nil error whose message contains wantErrSubstr (case-
// insensitive). Fails the test if the script succeeds or if the error message
// does not contain the substring.
func runDictScriptExpectError(t *testing.T, src string, wantErrSubstr string) {
	t.Helper()
	req := makeReq(nil, nil, nil)
	c := NewCollector(NewConditionCollector(), "test.star", nil, nil)
	globals, err := testBuildGlobals(req, c)
	if err != nil {
		t.Fatalf("testBuildGlobals error: %v", err)
	}
	rt := runtime.NewRuntime(logging.NewNopLogger())
	_, err = rt.Execute(src, globals, "test.star", nil)
	if err == nil {
		t.Fatalf("expected error containing %q, got nil\nsource:\n%s", wantErrSubstr, src)
	}
	if !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(wantErrSubstr)) {
		t.Errorf("error message = %q, want substring %q (case-insensitive)", err.Error(), wantErrSubstr)
	}
}

// ---------------------------------------------------------------------------
// Layer 1 — structural assertion on BuildGlobals output
// ---------------------------------------------------------------------------

func TestBuildGlobals_DictModule(t *testing.T) {
	req := makeReq(nil, nil, nil)
	c := NewCollector(NewConditionCollector(), "test.star", nil, nil)

	globals, err := testBuildGlobals(req, c)
	if err != nil {
		t.Fatalf("testBuildGlobals error: %v", err)
	}

	v, ok := globals["dict"]
	if !ok {
		t.Fatal(`globals["dict"] missing -- dict module not registered in BuildGlobals`)
	}

	mod, ok := v.(*starlarkstruct.Module)
	if !ok {
		t.Fatalf(`globals["dict"] is %T, want *starlarkstruct.Module`, v)
	}

	if mod.Name != "dict" {
		t.Errorf("mod.Name = %q, want %q", mod.Name, "dict")
	}

	wantMembers := []string{"merge", "deep_merge", "pick", "omit", "compact", "dig", "has_path"}
	for _, name := range wantMembers {
		if _, ok := mod.Members[name]; !ok {
			t.Errorf(`dict.Members missing %q`, name)
		}
	}

	// Guard against drift that silently adds or removes a member.
	if got := len(mod.Members); got != len(wantMembers) {
		t.Errorf("len(mod.Members) = %d, want %d (dict module drift?)", got, len(wantMembers))
	}
}

// ---------------------------------------------------------------------------
// Layer 2 — in-process tests via Runtime.Execute
// ---------------------------------------------------------------------------

func TestDict_Merge(t *testing.T) {
	// Two-dict merge.
	out := runDictScript(t, `
result = dict.merge({"a": 1}, {"b": 2})
has_a = result["a"] == 1
has_b = result["b"] == 2
count = len(result)
`)
	assertBool(t, out, "has_a", true)
	assertBool(t, out, "has_b", true)
	assertInt(t, out, "count", 2)

	// Right-wins on duplicate key.
	out = runDictScript(t, `
result = dict.merge({"a": 1}, {"a": 2})
val = result["a"]
`)
	assertInt(t, out, "val", 2)

	// Three-dict merge.
	out = runDictScript(t, `
result = dict.merge({"a": 1}, {"b": 2}, {"c": 3})
count = len(result)
has_a = result["a"] == 1
has_b = result["b"] == 2
has_c = result["c"] == 3
`)
	assertInt(t, out, "count", 3)
	assertBool(t, out, "has_a", true)
	assertBool(t, out, "has_b", true)
	assertBool(t, out, "has_c", true)

	// Empty dict merge.
	out = runDictScript(t, `
result = dict.merge({}, {"a": 1})
val = result["a"]
count = len(result)
`)
	assertInt(t, out, "val", 1)
	assertInt(t, out, "count", 1)
}

func TestDict_DeepMerge(t *testing.T) {
	// Nested dict merge.
	out := runDictScript(t, `
result = dict.deep_merge({"a": {"x": 1}}, {"a": {"y": 2}})
has_x = result["a"]["x"] == 1
has_y = result["a"]["y"] == 2
`)
	assertBool(t, out, "has_x", true)
	assertBool(t, out, "has_y", true)

	// Scalar right-wins.
	out = runDictScript(t, `
result = dict.deep_merge({"a": 1}, {"a": 2})
val = result["a"]
`)
	assertInt(t, out, "val", 2)

	// List atomic replace.
	out = runDictScript(t, `
result = dict.deep_merge({"a": [1, 2]}, {"a": [3]})
val = result["a"]
is_list = type(val) == "list"
length = len(val)
first = val[0]
`)
	assertBool(t, out, "is_list", true)
	assertInt(t, out, "length", 1)
	assertInt(t, out, "first", 3)

	// None overwrites (not deletes).
	out = runDictScript(t, `
result = dict.deep_merge({"a": 1}, {"a": None})
val = result["a"]
is_none = val == None
`)
	assertBool(t, out, "is_none", true)

	// Key only in base preserved.
	out = runDictScript(t, `
result = dict.deep_merge({"a": 1, "b": 2}, {"a": 3})
has_a = result["a"] == 3
has_b = result["b"] == 2
count = len(result)
`)
	assertBool(t, out, "has_a", true)
	assertBool(t, out, "has_b", true)
	assertInt(t, out, "count", 2)

	// Variadic (3 args).
	out = runDictScript(t, `
result = dict.deep_merge({"a": {"x": 1}}, {"a": {"y": 2}}, {"a": {"z": 3}})
has_x = result["a"]["x"] == 1
has_y = result["a"]["y"] == 2
has_z = result["a"]["z"] == 3
`)
	assertBool(t, out, "has_x", true)
	assertBool(t, out, "has_y", true)
	assertBool(t, out, "has_z", true)

	// Input mutation check: base dict unchanged after deep_merge.
	out = runDictScript(t, `
base = {"a": {"x": 1}}
override = {"a": {"y": 2}}
result = dict.deep_merge(base, override)
base_has_y = "y" in base["a"]
base_only_x = len(base["a"]) == 1
result_has_both = len(result["a"]) == 2
`)
	assertBool(t, out, "base_has_y", false)
	assertBool(t, out, "base_only_x", true)
	assertBool(t, out, "result_has_both", true)

	// Deep nesting (3+ levels).
	out = runDictScript(t, `
base = {"a": {"b": {"c": 1, "d": 2}}}
over = {"a": {"b": {"c": 99, "e": 3}}}
result = dict.deep_merge(base, over)
c_val = result["a"]["b"]["c"]
d_val = result["a"]["b"]["d"]
e_val = result["a"]["b"]["e"]
`)
	assertInt(t, out, "c_val", 99)
	assertInt(t, out, "d_val", 2)
	assertInt(t, out, "e_val", 3)
}

func TestDict_Pick(t *testing.T) {
	// Standard pick.
	out := runDictScript(t, `
result = dict.pick({"a": 1, "b": 2, "c": 3}, ["a", "c"])
count = len(result)
has_a = result["a"] == 1
has_c = result["c"] == 3
`)
	assertInt(t, out, "count", 2)
	assertBool(t, out, "has_a", true)
	assertBool(t, out, "has_c", true)

	// Missing keys silently skipped.
	out = runDictScript(t, `
result = dict.pick({"a": 1}, ["a", "z"])
count = len(result)
has_a = result["a"] == 1
`)
	assertInt(t, out, "count", 1)
	assertBool(t, out, "has_a", true)

	// Empty key list.
	out = runDictScript(t, `
result = dict.pick({"a": 1}, [])
count = len(result)
`)
	assertInt(t, out, "count", 0)
}

func TestDict_Omit(t *testing.T) {
	// Standard omit.
	out := runDictScript(t, `
result = dict.omit({"a": 1, "b": 2, "c": 3}, ["b"])
count = len(result)
has_a = result["a"] == 1
has_c = result["c"] == 3
has_b = "b" in result
`)
	assertInt(t, out, "count", 2)
	assertBool(t, out, "has_a", true)
	assertBool(t, out, "has_c", true)
	assertBool(t, out, "has_b", false)

	// Missing keys silently ignored.
	out = runDictScript(t, `
result = dict.omit({"a": 1}, ["z"])
count = len(result)
has_a = result["a"] == 1
`)
	assertInt(t, out, "count", 1)
	assertBool(t, out, "has_a", true)

	// Empty key list returns same content.
	out = runDictScript(t, `
result = dict.omit({"a": 1}, [])
count = len(result)
has_a = result["a"] == 1
`)
	assertInt(t, out, "count", 1)
	assertBool(t, out, "has_a", true)
}

func TestDict_Compact(t *testing.T) {
	// Drops None-valued entries.
	out := runDictScript(t, `
result = dict.compact({"a": "keep", "b": None, "c": 42, "d": None})
count = len(result)
has_a = result["a"] == "keep"
has_c = result["c"] == 42
has_b = "b" in result
has_d = "d" in result
`)
	assertInt(t, out, "count", 2)
	assertBool(t, out, "has_a", true)
	assertBool(t, out, "has_c", true)
	assertBool(t, out, "has_b", false)
	assertBool(t, out, "has_d", false)

	// Empty string, empty list, empty dict are preserved — K8s-meaningful.
	out = runDictScript(t, `
result = dict.compact({"s": "", "l": [], "d": {}})
count = len(result)
`)
	assertInt(t, out, "count", 3)

	// All-None input yields empty dict.
	out = runDictScript(t, `
result = dict.compact({"a": None, "b": None})
count = len(result)
`)
	assertInt(t, out, "count", 0)

	// Empty input yields empty dict.
	out = runDictScript(t, `
result = dict.compact({})
count = len(result)
`)
	assertInt(t, out, "count", 0)

	// Declarative manifest pattern: ternary against None, one compact() call
	// to drop the absent keys.
	out = runDictScript(t, `
au_id = ""
owners = ["o1"]
result = dict.compact({
    "displayName": "the-group",
    "administrativeUnitIds": [au_id] if au_id else None,
    "owners": owners if owners else None,
})
count = len(result)
has_name = result["displayName"] == "the-group"
has_owners = "owners" in result
has_au = "administrativeUnitIds" in result
`)
	assertInt(t, out, "count", 2)
	assertBool(t, out, "has_name", true)
	assertBool(t, out, "has_owners", true)
	assertBool(t, out, "has_au", false)
}

func TestDict_Dig(t *testing.T) {
	// Simple path.
	out := runDictScript(t, `
result = dict.dig({"a": {"b": {"c": 42}}}, "a.b.c")
`)
	assertInt(t, out, "result", 42)

	// Missing segment returns default.
	out = runDictScript(t, `
result = dict.dig({"a": 1}, "a.b.c", default="nope")
`)
	assertString(t, out, "result", "nope")

	// Default is None when not specified.
	out = runDictScript(t, `
result = dict.dig({}, "x")
is_none = result == None
`)
	assertBool(t, out, "is_none", true)

	// Single segment.
	out = runDictScript(t, `
result = dict.dig({"a": 1}, "a")
`)
	assertInt(t, out, "result", 1)
}

func TestDict_HasPath(t *testing.T) {
	out := runDictScript(t, `
yes = dict.has_path({"a": {"b": 1}}, "a.b")
no = dict.has_path({"a": {"b": 1}}, "a.c")
empty = dict.has_path({}, "a")
single = dict.has_path({"a": 1}, "a")
`)
	assertBool(t, out, "yes", true)
	assertBool(t, out, "no", false)
	assertBool(t, out, "empty", false)
	assertBool(t, out, "single", true)
}

// TestDict_MergeWithStarlarkDict verifies that dict.merge works with the
// *convert.StarlarkDict type returned by protobuf conversion (e.g., oxr).
func TestDict_MergeWithStarlarkDict(t *testing.T) {
	req := makeReq(
		map[string]*structpb.Value{
			"apiVersion": structpb.NewStringValue("v1"),
			"kind":       structpb.NewStringValue("XR"),
		},
		map[string]*structpb.Value{"apiVersion": structpb.NewStringValue("v1")},
		nil,
	)
	c := NewCollector(NewConditionCollector(), "test.star", nil, nil)
	globals, err := testBuildGlobals(req, c)
	if err != nil {
		t.Fatalf("testBuildGlobals error: %v", err)
	}

	rt := runtime.NewRuntime(logging.NewNopLogger())

	// dict.merge with oxr (frozen *convert.StarlarkDict) + plain dict.
	out, err := rt.Execute(`
merged = dict.merge(oxr, {"extra": "value"})
has_api = merged["apiVersion"] == "v1"
has_kind = merged["kind"] == "XR"
has_extra = merged["extra"] == "value"
count = len(merged)
`, globals, "test.star", nil)
	if err != nil {
		t.Fatalf("rt.Execute error: %v", err)
	}
	assertBool(t, out, "has_api", true)
	assertBool(t, out, "has_kind", true)
	assertBool(t, out, "has_extra", true)
	assertInt(t, out, "count", 3)
}

// TestDict_DigNonDictIntermediate verifies that dig returns default when a
// non-dict value is encountered at an intermediate path segment.
func TestDict_DigNonDictIntermediate(t *testing.T) {
	out := runDictScript(t, `
result = dict.dig({"a": "string_value"}, "a.b", default="nope")
`)
	assertString(t, out, "result", "nope")

	out = runDictScript(t, `
result = dict.dig({"a": 42}, "a.b.c")
is_none = result == None
`)
	assertBool(t, out, "is_none", true)
}

// TestDict_DeepMergeEmptyDicts verifies that deep_merge of two empty dicts
// produces an empty dict.
func TestDict_DeepMergeEmptyDicts(t *testing.T) {
	out := runDictScript(t, `
result = dict.deep_merge({}, {})
count = len(result)
is_dict = type(result) == "dict"
`)
	assertInt(t, out, "count", 0)
	assertBool(t, out, "is_dict", true)
}

// TestDict_DeepMergeDepthLimit verifies the recursion depth limit (32).
func TestDict_DeepMergeDepthLimit(t *testing.T) {
	// Build a deeply nested dict that exceeds 32 levels.
	// Each level nests one deeper: {"a": {"a": {"a": ...}}}
	runDictScriptExpectError(t, `
def make_nested(depth):
    d = {"leaf": True}
    for _ in range(depth):
        d = {"a": d}
    return d

# depth 33 should exceed the limit when merging
base = make_nested(33)
over = make_nested(33)
result = dict.deep_merge(base, over)
`, "recursion depth exceeds maximum")

	// depth 32 should succeed
	out := runDictScript(t, `
def make_nested(depth):
    d = {"leaf": True}
    for _ in range(depth):
        d = {"a": d}
    return d

base = make_nested(32)
over = make_nested(32)
result = dict.deep_merge(base, over)
ok = True
`)
	assertBool(t, out, "ok", true)
}

func TestDict_NegativeCases(t *testing.T) {
	// Malformed paths for dig.
	runDictScriptExpectError(t,
		`x = dict.dig({"a": 1}, "")`,
		"path must not be empty",
	)
	runDictScriptExpectError(t,
		`x = dict.dig({"a": 1}, ".a")`,
		"malformed path",
	)
	runDictScriptExpectError(t,
		`x = dict.dig({"a": 1}, "a.")`,
		"malformed path",
	)
	runDictScriptExpectError(t,
		`x = dict.dig({"a": 1}, "a..b")`,
		"malformed path",
	)

	// Malformed paths for has_path (same validation).
	runDictScriptExpectError(t,
		`x = dict.has_path({"a": 1}, "")`,
		"path must not be empty",
	)

	// Non-dict args.
	runDictScriptExpectError(t,
		`x = dict.merge(42, {})`,
		"got int, want dict",
	)

	// merge with < 2 args.
	runDictScriptExpectError(t,
		`x = dict.merge({})`,
		"requires at least 2 arguments",
	)

	// deep_merge with < 2 args.
	runDictScriptExpectError(t,
		`x = dict.deep_merge({})`,
		"requires at least 2 arguments",
	)

	// pick with non-string keys.
	runDictScriptExpectError(t,
		`x = dict.pick({"a": 1}, [42])`,
		"key list element is int, want string",
	)
}

// TestDict_CrossType verifies that dict functions work on frozen
// *convert.StarlarkDict values (the type used for oxr and observed resources).
func TestDict_CrossType(t *testing.T) {
	// Build a request with real oxr fields so oxr is a frozen *convert.StarlarkDict.
	req := makeReq(
		map[string]*structpb.Value{
			"apiVersion": structpb.NewStringValue("v1"),
			"kind":       structpb.NewStringValue("Composite"),
			"metadata": structpb.NewStructValue(&structpb.Struct{Fields: map[string]*structpb.Value{
				"name": structpb.NewStringValue("my-resource"),
			}}),
		},
		map[string]*structpb.Value{"apiVersion": structpb.NewStringValue("v1")},
		nil,
	)
	c := NewCollector(NewConditionCollector(), "test.star", nil, nil)
	globals, err := testBuildGlobals(req, c)
	if err != nil {
		t.Fatalf("testBuildGlobals error: %v", err)
	}

	rt := runtime.NewRuntime(logging.NewNopLogger())

	// dict.dig on frozen *convert.StarlarkDict.
	out, err := rt.Execute(`
dig_result = dict.dig(oxr, "apiVersion")
dig_nested = dict.dig(oxr, "metadata.name")
`, globals, "test.star", nil)
	if err != nil {
		t.Fatalf("rt.Execute (dig): %v", err)
	}
	assertString(t, out, "dig_result", "v1")
	assertString(t, out, "dig_nested", "my-resource")

	// dict.has_path on frozen *convert.StarlarkDict.
	globals, _ = testBuildGlobals(req, c)
	out, err = rt.Execute(`
has = dict.has_path(oxr, "apiVersion")
not_has = dict.has_path(oxr, "nonexistent")
`, globals, "test.star", nil)
	if err != nil {
		t.Fatalf("rt.Execute (has_path): %v", err)
	}
	assertBool(t, out, "has", true)
	assertBool(t, out, "not_has", false)

	// dict.pick on frozen *convert.StarlarkDict.
	globals, _ = testBuildGlobals(req, c)
	out, err = rt.Execute(`
picked = dict.pick(oxr, ["apiVersion"])
count = len(picked)
val = picked["apiVersion"]
`, globals, "test.star", nil)
	if err != nil {
		t.Fatalf("rt.Execute (pick): %v", err)
	}
	assertInt(t, out, "count", 1)
	assertString(t, out, "val", "v1")

	// dict.omit on frozen *convert.StarlarkDict.
	globals, _ = testBuildGlobals(req, c)
	out, err = rt.Execute(`
omitted = dict.omit(oxr, ["apiVersion"])
has_api = "apiVersion" in omitted
has_kind = "kind" in omitted
`, globals, "test.star", nil)
	if err != nil {
		t.Fatalf("rt.Execute (omit): %v", err)
	}
	assertBool(t, out, "has_api", false)
	assertBool(t, out, "has_kind", true)
}

// ---------------------------------------------------------------------------
// Phase 36 Plan 01 — Recursive dict.compact tests
// ---------------------------------------------------------------------------

// TestDict_CompactRecursive tests DICT-01: recursive None pruning at any depth.
func TestDict_CompactRecursive(t *testing.T) {
	// Nested dict with None at depth 2.
	out := runDictScript(t, `
result = dict.compact({"a": {"b": None, "c": 1}})
c_val = result["a"]["c"]
has_b = "b" in result["a"]
`)
	assertInt(t, out, "c_val", 1)
	assertBool(t, out, "has_b", false)

	// Depth-3 pruning.
	out = runDictScript(t, `
result = dict.compact({"a": {"b": {"c": None, "d": 2}}})
d_val = result["a"]["b"]["d"]
has_c = "c" in result["a"]["b"]
`)
	assertInt(t, out, "d_val", 2)
	assertBool(t, out, "has_c", false)

	// Dict inside a list has its None entries pruned.
	out = runDictScript(t, `
result = dict.compact({"a": [{"b": None, "c": 1}]})
c_val = result["a"][0]["c"]
has_b = "b" in result["a"][0]
`)
	assertInt(t, out, "c_val", 1)
	assertBool(t, out, "has_b", false)

	// Dict inside nested lists (list of lists) has its None entries pruned.
	out = runDictScript(t, `
result = dict.compact({"a": [[{"b": None, "c": 1}]]})
c_val = result["a"][0][0]["c"]
has_b = "b" in result["a"][0][0]
`)
	assertInt(t, out, "c_val", 1)
	assertBool(t, out, "has_b", false)

	// None elements inside lists are NOT removed (index safety).
	out = runDictScript(t, `
result = dict.compact({"a": [None, 1, None, 2]})
length = len(result["a"])
first_none = result["a"][0] == None
second_val = result["a"][1]
third_none = result["a"][2] == None
fourth_val = result["a"][3]
`)
	assertInt(t, out, "length", 4)
	assertBool(t, out, "first_none", true)
	assertInt(t, out, "second_val", 1)
	assertBool(t, out, "third_none", true)
	assertInt(t, out, "fourth_val", 2)

	// Tuple containing a dict with None passes through untouched (immutable).
	out = runDictScript(t, `
result = dict.compact({"a": ({"b": None},)})
inner = result["a"][0]
has_b = "b" in inner
`)
	assertBool(t, out, "has_b", true)

	// Input dict is not mutated (copy semantics).
	out = runDictScript(t, `
original = {"a": {"b": None, "c": 1}}
result = dict.compact(original)
orig_has_b = "b" in original["a"]
result_has_b = "b" in result["a"]
`)
	assertBool(t, out, "orig_has_b", true)
	assertBool(t, out, "result_has_b", false)
}

// TestDict_CompactPreservesEmpties tests DICT-02: K8s-meaningful empties preserved.
func TestDict_CompactPreservesEmpties(t *testing.T) {
	// Empty string, empty list, empty dict at depth 2 survive compaction.
	out := runDictScript(t, `
result = dict.compact({"a": {"s": "", "l": [], "d": {}}})
has_s = "s" in result["a"]
has_l = "l" in result["a"]
has_d = "d" in result["a"]
count = len(result["a"])
`)
	assertBool(t, out, "has_s", true)
	assertBool(t, out, "has_l", true)
	assertBool(t, out, "has_d", true)
	assertInt(t, out, "count", 3)

	// Dict that becomes {} after all None values removed is preserved (post-compaction empty).
	out = runDictScript(t, `
result = dict.compact({"a": {"b": None, "c": None}})
has_a = "a" in result
inner_count = len(result["a"])
`)
	assertBool(t, out, "has_a", true)
	assertInt(t, out, "inner_count", 0)

	// List of dicts that become empty after compaction: [{}, {}].
	out = runDictScript(t, `
result = dict.compact({"a": [{"b": None}, {}]})
length = len(result["a"])
first_count = len(result["a"][0])
second_count = len(result["a"][1])
`)
	assertInt(t, out, "length", 2)
	assertInt(t, out, "first_count", 0)
	assertInt(t, out, "second_count", 0)

	// Deep empties at depth 3 survive.
	out = runDictScript(t, `
result = dict.compact({"a": {"b": {"s": "", "l": [], "d": {}}}})
inner = result["a"]["b"]
count = len(inner)
`)
	assertInt(t, out, "count", 3)
}

// TestDict_CompactDepthLimit tests DICT-03: recursion depth guard.
func TestDict_CompactDepthLimit(t *testing.T) {
	// Dict nested 33 levels deep raises "recursion depth exceeds maximum (32)".
	runDictScriptExpectError(t, `
def make_nested(depth):
    d = {"leaf": True}
    for _ in range(depth):
        d = {"a": d}
    return d

result = dict.compact(make_nested(33))
`, "recursion depth exceeds maximum")

	// Dict nested 32 levels deep succeeds.
	out := runDictScript(t, `
def make_nested(depth):
    d = {"leaf": True}
    for _ in range(depth):
        d = {"a": d}
    return d

result = dict.compact(make_nested(32))
ok = True
`)
	assertBool(t, out, "ok", true)

	// Alternating dict/list nesting counts toward depth — 33+ levels raises error.
	runDictScriptExpectError(t, `
def make_alternating(depth):
    d = {"leaf": True}
    for i in range(depth):
        if i % 2 == 0:
            d = [d]
        else:
            d = {"a": d}
    return {"root": d}

result = dict.compact(make_alternating(33))
`, "recursion depth exceeds maximum")
}

// ---------------------------------------------------------------------------
// Test assertion helpers
// ---------------------------------------------------------------------------

func assertBool(t *testing.T, out starlark.StringDict, name string, want bool) {
	t.Helper()
	v, ok := out[name]
	if !ok {
		t.Fatalf("output missing %q", name)
	}
	b, ok := v.(starlark.Bool)
	if !ok {
		t.Fatalf("%s is %T, want starlark.Bool", name, v)
	}
	if bool(b) != want {
		t.Errorf("%s = %v, want %v", name, b, want)
	}
}

func assertInt(t *testing.T, out starlark.StringDict, name string, want int64) {
	t.Helper()
	v, ok := out[name]
	if !ok {
		t.Fatalf("output missing %q", name)
	}
	i, ok := v.(starlark.Int)
	if !ok {
		t.Fatalf("%s is %T, want starlark.Int", name, v)
	}
	n, _ := i.Int64()
	if n != want {
		t.Errorf("%s = %d, want %d", name, n, want)
	}
}

func assertString(t *testing.T, out starlark.StringDict, name string, want string) {
	t.Helper()
	v, ok := out[name]
	if !ok {
		t.Fatalf("output missing %q", name)
	}
	s, ok := v.(starlark.String)
	if !ok {
		t.Fatalf("%s is %T, want starlark.String", name, v)
	}
	if string(s) != want {
		t.Errorf("%s = %q, want %q", name, string(s), want)
	}
}
