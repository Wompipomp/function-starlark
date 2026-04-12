package builtins

import (
	"strings"
	"testing"

	"github.com/crossplane/function-sdk-go/logging"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"

	"github.com/wompipomp/function-starlark/runtime"
)

// ---------------------------------------------------------------------------
// Phase 31 Plan 01 — builtins/regex_test.go
//
// Three-layer test coverage for the `regex` module:
//
//   Layer 1 (unit on BuildGlobals output):
//     - TestBuildGlobals_RegexModule
//
//   Layer 2 (in-process via Runtime.Execute):
//     - TestRegex_Match
//     - TestRegex_Find
//     - TestRegex_FindAll
//     - TestRegex_FindGroups
//     - TestRegex_Replace
//     - TestRegex_ReplaceAll
//     - TestRegex_Split
//     - TestRegex_NegativeCases
//
// No Layer 3 (protobuf round-trip) needed for regex — outputs are plain
// strings/bools/lists that already survive the convert pipeline.
//
// Fixtures are all inline Go string literals (no external fixture files)
// per 29-CONTEXT.md §Fixture placement.
// ---------------------------------------------------------------------------

// runRegexScript compiles and runs a Starlark source string against the full
// BuildGlobals predeclared set (which includes `regex`) via Runtime.Execute,
// returning the post-execution globals. Fails the test on any error.
func runRegexScript(t *testing.T, src string) starlark.StringDict {
	t.Helper()
	req := makeReq(nil, nil, nil)
	c := NewCollector(NewConditionCollector(), "test.star", nil)
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

// runRegexScriptExpectError runs a Starlark source string via Runtime.Execute,
// expecting a non-nil error whose message contains wantErrSubstr (case-
// insensitive). Fails the test if the script succeeds or if the error message
// does not contain the substring.
func runRegexScriptExpectError(t *testing.T, src string, wantErrSubstr string) {
	t.Helper()
	req := makeReq(nil, nil, nil)
	c := NewCollector(NewConditionCollector(), "test.star", nil)
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
// Layer 1 -- structural assertion on BuildGlobals output
// ---------------------------------------------------------------------------

func TestBuildGlobals_RegexModule(t *testing.T) {
	req := makeReq(nil, nil, nil)
	c := NewCollector(NewConditionCollector(), "test.star", nil)

	globals, err := testBuildGlobals(req, c)
	if err != nil {
		t.Fatalf("testBuildGlobals error: %v", err)
	}

	v, ok := globals["regex"]
	if !ok {
		t.Fatal(`globals["regex"] missing -- regex module not registered in BuildGlobals`)
	}

	mod, ok := v.(*starlarkstruct.Module)
	if !ok {
		t.Fatalf(`globals["regex"] is %T, want *starlarkstruct.Module`, v)
	}

	if mod.Name != "regex" {
		t.Errorf("mod.Name = %q, want %q", mod.Name, "regex")
	}

	wantMembers := []string{"match", "find", "find_all", "find_groups", "replace", "replace_all", "split"}
	for _, name := range wantMembers {
		if _, ok := mod.Members[name]; !ok {
			t.Errorf(`regex.Members missing %q`, name)
		}
	}

	// Guard against drift that silently adds or removes a member.
	if got := len(mod.Members); got != len(wantMembers) {
		t.Errorf("len(mod.Members) = %d, want %d (regex module drift?)", got, len(wantMembers))
	}
}

// ---------------------------------------------------------------------------
// Layer 2 -- in-process tests via Runtime.Execute
// ---------------------------------------------------------------------------

// TestRegex_Match covers REG-01.
func TestRegex_Match(t *testing.T) {
	out := runRegexScript(t, `
# Partial match: pattern matches anywhere in string
match_true = regex.match(r"^arn:aws:", "arn:aws:iam::123:role/foo")
match_false = regex.match(r"^arn:gcp:", "arn:aws:iam::123:role/foo")

# Unanchored partial match
partial = regex.match(r"\d+", "abc123def")

# No match at all
nomatch = regex.match(r"\d+", "abcdef")
`)
	assertBool(t, out, "match_true", true)
	assertBool(t, out, "match_false", false)
	assertBool(t, out, "partial", true)
	assertBool(t, out, "nomatch", false)
}

// TestRegex_Find covers REG-02.
func TestRegex_Find(t *testing.T) {
	out := runRegexScript(t, `
first = regex.find(r"\d+", "abc123def456")
none_result = regex.find(r"\d+", "abc")
`)
	assertString(t, out, "first", "123")
	assertNone(t, out, "none_result")
}

// TestRegex_FindAll covers REG-03.
func TestRegex_FindAll(t *testing.T) {
	out := runRegexScript(t, `
matches = regex.find_all(r"\d+", "a1b22c333")
no_matches = regex.find_all(r"\d+", "abc")
`)
	assertStringList(t, out, "matches", []string{"1", "22", "333"})
	assertStringList(t, out, "no_matches", []string{})
}

// TestRegex_FindGroups covers REG-04.
func TestRegex_FindGroups(t *testing.T) {
	out := runRegexScript(t, `
# ARN extraction use case
groups = regex.find_groups(r"^arn:aws:iam::(\d+):role/(.+)$", "arn:aws:iam::123456:role/admin")

# No match returns None
none_result = regex.find_groups(r"^arn:aws:iam::(\d+):role/(.+)$", "not-an-arn")
`)
	assertStringList(t, out, "groups", []string{"123456", "admin"})
	assertNone(t, out, "none_result")
}

// TestRegex_Replace covers REG-05.
func TestRegex_Replace(t *testing.T) {
	out := runRegexScript(t, `
# First match only
first_only = regex.replace(r"\d+", "a1b2c3", "X")

# No match returns original
no_match = regex.replace(r"\d+", "abc", "X")

# $1 backreference support
backref = regex.replace(r"(\w+)@(\w+)", "user@host rest", "$1 at $2")
`)
	assertString(t, out, "first_only", "aXb2c3")
	assertString(t, out, "no_match", "abc")
	assertString(t, out, "backref", "user at host rest")
}

// TestRegex_ReplaceAll covers REG-06.
func TestRegex_ReplaceAll(t *testing.T) {
	out := runRegexScript(t, `
# All matches
all = regex.replace_all(r"\d+", "a1b2c3", "X")

# $1 backreference support
backref = regex.replace_all(r"(\w+)@(\w+)", "a@b c@d", "$1-$2")
`)
	assertString(t, out, "all", "aXbXcX")
	assertString(t, out, "backref", "a-b c-d")
}

// TestRegex_Split covers REG-07.
func TestRegex_Split(t *testing.T) {
	out := runRegexScript(t, `
result = regex.split(r"[,;]\s*", "a, b;c, d")

# Trailing separator produces trailing empty string
trailing = regex.split(r",", "a,b,")
`)
	assertStringList(t, out, "result", []string{"a", "b", "c", "d"})
	assertStringList(t, out, "trailing", []string{"a", "b", ""})
}

// TestRegex_NegativeCases asserts that bad inputs fail with expected errors.
func TestRegex_NegativeCases(t *testing.T) {
	// Invalid pattern
	runRegexScriptExpectError(t,
		`x = regex.match("[unclosed", "test")`,
		"error parsing regexp",
	)

	// Error includes function name
	runRegexScriptExpectError(t,
		`x = regex.find("[unclosed", "test")`,
		"regex.find",
	)

	// Wrong arg type (int instead of string)
	runRegexScriptExpectError(t,
		`x = regex.match(42, "test")`,
		"got int",
	)
}

// ---------------------------------------------------------------------------
// Test assertion helpers (assertBool, assertString, assertInt already in
// dict_test.go; only new helpers defined here)
// ---------------------------------------------------------------------------

func assertNone(t *testing.T, out starlark.StringDict, key string) {
	t.Helper()
	v, ok := out[key]
	if !ok {
		t.Fatalf("out[%q] missing", key)
	}
	if v != starlark.None {
		t.Errorf("out[%q] = %v (%T), want None", key, v, v)
	}
}

func assertStringList(t *testing.T, out starlark.StringDict, key string, want []string) {
	t.Helper()
	v, ok := out[key]
	if !ok {
		t.Fatalf("out[%q] missing", key)
	}
	list, ok := v.(*starlark.List)
	if !ok {
		t.Fatalf("out[%q] is %T, want *starlark.List", key, v)
	}
	if list.Len() != len(want) {
		t.Fatalf("out[%q] len = %d, want %d; got %s", key, list.Len(), len(want), list.String())
	}
	for i, w := range want {
		s, ok := list.Index(i).(starlark.String)
		if !ok {
			t.Errorf("out[%q][%d] is %T, want starlark.String", key, i, list.Index(i))
			continue
		}
		if string(s) != w {
			t.Errorf("out[%q][%d] = %q, want %q", key, i, string(s), w)
		}
	}
}
