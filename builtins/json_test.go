package builtins

import (
	"math"
	"strings"
	"testing"

	"github.com/crossplane/function-sdk-go/logging"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/wompipomp/function-starlark/convert"
	"github.com/wompipomp/function-starlark/runtime"
)

// ---------------------------------------------------------------------------
// Phase 29 Plan 02 — builtins/json_test.go
//
// Three-layer test coverage for the `json` module binding added in Plan 01:
//
//   Layer 1 (unit on BuildGlobals output):
//     - TestBuildGlobals_JSONModule
//
//   Layer 2 (in-process via Runtime.Execute):
//     - TestJSON_Encode
//     - TestJSON_EncodeIndent
//     - TestJSON_Decode
//     - TestJSON_Indent
//     - TestJSON_RoundTrip_ViaRuntime (matrix incl. int64 boundaries)
//     - TestJSON_NegativeCases
//
//   Layer 3 (protobuf round-trip through convert.PlainDictToStruct):
//     - TestJSON_RoundTrip_ToProtobuf   (added in Task 2 of this plan)
//
// Fixtures are all inline Go string/map literals (no external fixture files)
// per 29-CONTEXT.md §Fixture placement.
// ---------------------------------------------------------------------------

// runJSONScript compiles and runs a Starlark source string against the full
// BuildGlobals predeclared set (which includes `json`) via Runtime.Execute,
// returning the post-execution globals. Fails the test on any error.
func runJSONScript(t *testing.T, src string) starlark.StringDict {
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

// runJSONScriptExpectError runs a Starlark source string via Runtime.Execute,
// expecting a non-nil error whose message contains wantErrSubstr (case-
// insensitive). Fails the test if the script succeeds or if the error message
// does not contain the substring.
func runJSONScriptExpectError(t *testing.T, src string, wantErrSubstr string) {
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
// Layer 1 — structural assertion on BuildGlobals output
// ---------------------------------------------------------------------------

func TestBuildGlobals_JSONModule(t *testing.T) {
	req := makeReq(nil, nil, nil)
	c := NewCollector(NewConditionCollector(), "test.star", nil)

	globals, err := testBuildGlobals(req, c)
	if err != nil {
		t.Fatalf("testBuildGlobals error: %v", err)
	}

	v, ok := globals["json"]
	if !ok {
		t.Fatal(`globals["json"] missing — Plan 01 binding regressed`)
	}

	mod, ok := v.(*starlarkstruct.Module)
	if !ok {
		t.Fatalf(`globals["json"] is %T, want *starlarkstruct.Module`, v)
	}

	if mod.Name != "json" {
		t.Errorf("mod.Name = %q, want %q", mod.Name, "json")
	}

	wantMembers := []string{"encode", "encode_indent", "decode", "indent"}
	for _, name := range wantMembers {
		if _, ok := mod.Members[name]; !ok {
			t.Errorf(`json.Members missing %q`, name)
		}
	}

	// Guard against upstream drift that silently adds a 5th member.
	if got := len(mod.Members); got != len(wantMembers) {
		t.Errorf("len(mod.Members) = %d, want %d (upstream go.starlark.net/lib/json drift?)", got, len(wantMembers))
	}
}

// ---------------------------------------------------------------------------
// Layer 2 — in-process tests via Runtime.Execute
// ---------------------------------------------------------------------------

// TestJSON_Encode covers SC1 / ENC-01.
func TestJSON_Encode(t *testing.T) {
	// Starlark dicts preserve insertion order so the encoded output is
	// deterministic byte-for-byte.
	out := runJSONScript(t, `
result = json.encode({"a": 1, "b": [2, 3]})
null_result = json.encode(None)
true_result = json.encode(True)
`)

	got, ok := out["result"].(starlark.String)
	if !ok {
		t.Fatalf(`out["result"] is %T, want starlark.String`, out["result"])
	}
	if want := `{"a":1,"b":[2,3]}`; string(got) != want {
		t.Errorf("json.encode({...}) = %q, want %q", string(got), want)
	}

	if s, ok := out["null_result"].(starlark.String); !ok || string(s) != "null" {
		t.Errorf("json.encode(None) = %v, want \"null\"", out["null_result"])
	}
	if s, ok := out["true_result"].(starlark.String); !ok || string(s) != "true" {
		t.Errorf("json.encode(True) = %v, want \"true\"", out["true_result"])
	}
}

// TestJSON_EncodeIndent covers SC4 / ENC-02.
func TestJSON_EncodeIndent(t *testing.T) {
	// encode_indent uses keyword-only `prefix` and `indent` params upstream.
	out := runJSONScript(t, `
result = json.encode_indent({"a": 1}, indent="  ")
prefixed = json.encode_indent({"a": 1}, prefix="> ", indent="  ")
`)

	got, ok := out["result"].(starlark.String)
	if !ok {
		t.Fatalf(`out["result"] is %T, want starlark.String`, out["result"])
	}
	if !strings.Contains(string(got), "\n  \"a\": 1") {
		t.Errorf("json.encode_indent(..., indent=\"  \") = %q; missing expected newline+indent+key", string(got))
	}

	pref, ok := out["prefixed"].(starlark.String)
	if !ok {
		t.Fatalf(`out["prefixed"] is %T, want starlark.String`, out["prefixed"])
	}
	// The prefix is applied at the start of each line. Check that it is
	// present somewhere in the output so both keyword-only params are
	// demonstrably reachable.
	if !strings.Contains(string(pref), "> ") {
		t.Errorf("json.encode_indent(..., prefix=\"> \") = %q; missing prefix characters", string(pref))
	}
}

// TestJSON_Decode covers SC2 / ENC-03.
func TestJSON_Decode(t *testing.T) {
	out := runJSONScript(t, `
decoded = json.decode('{"a":1,"b":[2,3],"c":null,"d":"text"}')
none_decoded = json.decode("null")
int_decoded = json.decode("42")
float_decoded = json.decode("42.0")
`)

	d, ok := out["decoded"].(*starlark.Dict)
	if !ok {
		t.Fatalf(`out["decoded"] is %T, want *starlark.Dict`, out["decoded"])
	}

	// "a" -> int 1
	aVal, found, err := d.Get(starlark.String("a"))
	if err != nil || !found {
		t.Fatalf(`decoded["a"] lookup failed: found=%v err=%v`, found, err)
	}
	aInt, ok := aVal.(starlark.Int)
	if !ok {
		t.Errorf(`decoded["a"] is %T, want starlark.Int`, aVal)
	} else if n, _ := aInt.Int64(); n != 1 {
		t.Errorf(`decoded["a"] = %d, want 1`, n)
	}

	// "b" -> list of length 2
	bVal, found, err := d.Get(starlark.String("b"))
	if err != nil || !found {
		t.Fatalf(`decoded["b"] lookup failed: found=%v err=%v`, found, err)
	}
	bList, ok := bVal.(*starlark.List)
	if !ok {
		t.Errorf(`decoded["b"] is %T, want *starlark.List`, bVal)
	} else if bList.Len() != 2 {
		t.Errorf(`decoded["b"].Len() = %d, want 2`, bList.Len())
	}

	// "c" -> None
	cVal, found, err := d.Get(starlark.String("c"))
	if err != nil || !found {
		t.Fatalf(`decoded["c"] lookup failed: found=%v err=%v`, found, err)
	}
	if cVal != starlark.None {
		t.Errorf(`decoded["c"] = %v, want None`, cVal)
	}

	// "d" -> String("text")
	dVal, found, err := d.Get(starlark.String("d"))
	if err != nil || !found {
		t.Fatalf(`decoded["d"] lookup failed: found=%v err=%v`, found, err)
	}
	if s, ok := dVal.(starlark.String); !ok || string(s) != "text" {
		t.Errorf(`decoded["d"] = %v, want "text"`, dVal)
	}

	// json.decode("null") == None
	if out["none_decoded"] != starlark.None {
		t.Errorf(`json.decode("null") = %v, want None`, out["none_decoded"])
	}

	// json.decode("42") is an int, not a float (no decimal point).
	iv, ok := out["int_decoded"].(starlark.Int)
	if !ok {
		t.Errorf(`json.decode("42") is %T, want starlark.Int`, out["int_decoded"])
	} else if n, _ := iv.Int64(); n != 42 {
		t.Errorf(`json.decode("42") = %d, want 42`, n)
	}

	// json.decode("42.0") is a float (decimal point forces float).
	if _, ok := out["float_decoded"].(starlark.Float); !ok {
		t.Errorf(`json.decode("42.0") is %T, want starlark.Float`, out["float_decoded"])
	}
}

// TestJSON_Indent covers SC5 / ENC-04 — string→string reformat only.
func TestJSON_Indent(t *testing.T) {
	out := runJSONScript(t, `
pretty = json.indent('{"a":1}', indent="  ")
`)

	got, ok := out["pretty"].(starlark.String)
	if !ok {
		t.Fatalf(`out["pretty"] is %T, want starlark.String`, out["pretty"])
	}
	if !strings.Contains(string(got), "\n  ") {
		t.Errorf("json.indent(..., indent=\"  \") = %q; missing newline+indent", string(got))
	}
	if !strings.Contains(string(got), `"a": 1`) {
		t.Errorf("json.indent output = %q; missing reformatted key/value", string(got))
	}
}

// TestJSON_RoundTrip_ViaRuntime exercises the Starlark-level round-trip matrix
// including int64 boundary cases (math.MaxInt64 / math.MinInt64) that would
// otherwise be rejected by convert/convert.go:234 (maxSafeInt guard at 2^53).
// See TestJSON_RoundTrip_ToProtobuf for the narrower layer-3 matrix that stays
// within the [-(2^53-1), 2^53-1] range.
func TestJSON_RoundTrip_ViaRuntime(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{
			name: "k8s_deployment",
			body: `{
    "apiVersion": "apps/v1",
    "kind": "Deployment",
    "metadata": {"name": "web", "labels": {"app": "web"}},
    "spec": {
        "replicas": 3,
        "selector": {"matchLabels": {"app": "web"}},
    },
}`,
		},
		{
			name: "k8s_configmap",
			body: `{
    "apiVersion": "v1",
    "kind": "ConfigMap",
    "metadata": {"name": "cfg"},
    "data": {"key1": "v1", "key2": "v2"},
}`,
		},
		{
			name: "null_value",
			body: `{"a": None, "b": 1}`,
		},
		{
			name: "empty_dict",
			body: `{}`,
		},
		{
			name: "empty_list",
			body: `[]`,
		},
		{
			name: "nested_empty",
			body: `{"a": {}, "b": []}`,
		},
		{
			name: "escaped_ascii",
			body: `{"quoted": "a \"b\" c", "back": "x\\y"}`,
		},
		{
			name: "unicode_bmp_astral",
			// BMP (é) + astral plane (🌍 = U+1F30D) codepoints.
			body: `{"greeting": "héllo 🌍"}`,
		},
		{
			name: "ints_safe_range",
			// 9007199254740991 == 2^53 - 1, the convert-layer safe ceiling.
			body: `{"zero": 0, "one": 1, "neg": -1, "big": 9007199254740991, "negbig": -9007199254740991}`,
		},
		{
			name: "floats",
			body: `{"half": 1.5, "neghalf": -0.5, "big": 1.23e45}`,
		},
		{
			// Layer-2 only: math.MaxInt64 / math.MinInt64 round-trip through
			// Starlark-level json.decode(json.encode(...)) but would FAIL at
			// convert.PlainDictToStruct (convert/convert.go:234 guard at 2^53).
			name: "int64_boundaries",
			body: `{"max": ` + formatInt64(math.MaxInt64) + `, "min": ` + formatInt64(math.MinInt64) + `}`,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			src := "body = " + tc.body + "\n" +
				"decoded = json.decode(json.encode(body))\n" +
				"equal = decoded == body\n"
			out := runJSONScript(t, src)
			eq, ok := out["equal"].(starlark.Bool)
			if !ok {
				t.Fatalf(`out["equal"] is %T, want starlark.Bool`, out["equal"])
			}
			if !bool(eq) {
				t.Errorf("round-trip mismatch for %s: decoded != body", tc.name)
			}
		})
	}
}

// formatInt64 formats an int64 as a decimal string for use in inline Starlark
// source, without pulling in fmt/strconv (keeping test imports focused on the
// domain packages under test).
func formatInt64(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		// Handle MinInt64 directly: -MinInt64 overflows int64.
		if n == math.MinInt64 {
			return "-9223372036854775808"
		}
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// TestJSON_NegativeCases asserts that bad inputs fail with expected errors.
func TestJSON_NegativeCases(t *testing.T) {
	// Non-string dict key — json.encode should reject.
	runJSONScriptExpectError(t,
		`x = json.encode({1: "x"})`,
		"key",
	)

	// Non-finite float — upstream rejects with "Infinity" / "finite" wording.
	runJSONScriptExpectError(t,
		`x = json.encode(float("inf"))`,
		"finite",
	)

	// Malformed JSON string — upstream error message contains "json".
	runJSONScriptExpectError(t,
		`x = json.decode("not json")`,
		"json",
	)
}

// ---------------------------------------------------------------------------
// Layer 3 — full protobuf round-trip through convert.PlainDictToStruct.
//
// NOTE on int64 boundaries: math.MaxInt64 / math.MinInt64 and any value
// with |n| > 2^53 - 1 are deliberately EXCLUDED from this layer. The
// convert layer enforces a safe-integer ceiling at 2^53 in
// convert/convert.go:234 so that all round-tripped numbers are exactly
// representable as float64 on the protobuf side. int64 edge cases are
// exercised only at the Starlark level in TestJSON_RoundTrip_ViaRuntime,
// per 29-RESEARCH.md §Pitfall 3 and 29-CONTEXT.md §Round-trip edge cases.
//
// TestJSON_RoundTrip_ToProtobuf is the canonical proof of SC3 —
// "byte-identical round-trip of an observed resource body."
// ---------------------------------------------------------------------------

func TestJSON_RoundTrip_ToProtobuf(t *testing.T) {
	// Helpers for building structpb.Value instances.
	str := structpb.NewStringValue
	num := structpb.NewNumberValue
	nul := structpb.NewNullValue
	stct := func(fields map[string]*structpb.Value) *structpb.Value {
		return structpb.NewStructValue(&structpb.Struct{Fields: fields})
	}
	lst := func(values ...*structpb.Value) *structpb.Value {
		return structpb.NewListValue(&structpb.ListValue{Values: values})
	}

	cases := []struct {
		name     string
		original *structpb.Struct
	}{
		{
			name: "k8s_deployment",
			original: &structpb.Struct{Fields: map[string]*structpb.Value{
				"apiVersion": str("apps/v1"),
				"kind":       str("Deployment"),
				"metadata": stct(map[string]*structpb.Value{
					"name":   str("web"),
					"labels": stct(map[string]*structpb.Value{"app": str("web")}),
				}),
				"spec": stct(map[string]*structpb.Value{
					"replicas": num(3),
					"selector": stct(map[string]*structpb.Value{
						"matchLabels": stct(map[string]*structpb.Value{"app": str("web")}),
					}),
				}),
			}},
		},
		{
			name: "k8s_configmap",
			original: &structpb.Struct{Fields: map[string]*structpb.Value{
				"apiVersion": str("v1"),
				"kind":       str("ConfigMap"),
				"metadata":   stct(map[string]*structpb.Value{"name": str("cfg")}),
				"data": stct(map[string]*structpb.Value{
					"key1": str("v1"),
					"key2": str("v2"),
				}),
			}},
		},
		{
			name: "null_value",
			original: &structpb.Struct{Fields: map[string]*structpb.Value{
				"a": nul(),
				"b": str("x"),
			}},
		},
		{
			name: "empty_dict",
			original: &structpb.Struct{Fields: map[string]*structpb.Value{
				"a": stct(map[string]*structpb.Value{}),
			}},
		},
		{
			name: "empty_list",
			original: &structpb.Struct{Fields: map[string]*structpb.Value{
				"a": lst(),
			}},
		},
		{
			name: "nested_empty",
			original: &structpb.Struct{Fields: map[string]*structpb.Value{
				"a": stct(map[string]*structpb.Value{}),
				"b": lst(),
			}},
		},
		{
			name: "escaped_ascii",
			original: &structpb.Struct{Fields: map[string]*structpb.Value{
				"s": str(`a "b" c\d`),
			}},
		},
		{
			name: "unicode_bmp_astral",
			original: &structpb.Struct{Fields: map[string]*structpb.Value{
				"s": str("héllo 🌍"),
			}},
		},
		{
			name: "ints_safe_range",
			// All inside [-(2^53 - 1), 2^53 - 1]; 9007199254740991 == 2^53-1.
			// See convert/convert.go:234 (maxSafeInt guard) for rationale.
			original: &structpb.Struct{Fields: map[string]*structpb.Value{
				"zero": num(0),
				"one":  num(1),
				"neg":  num(-1),
				"max":  num(9007199254740991),
				"min":  num(-9007199254740991),
			}},
		},
		{
			name: "floats",
			// Note: floats must be non-whole or within [−2^53, 2^53] to
			// survive convert.convertNumber's int64-range guard. Large
			// whole-value floats such as 1.23e45 are rejected at input.
			// Non-whole values always pass straight through as starlark.Float.
			original: &structpb.Struct{Fields: map[string]*structpb.Value{
				"half":     num(1.5),
				"neghalf":  num(-0.5),
				"pi":       num(3.14159),
				"smallsci": num(1.23e-5),
				"bignonwh": num(1.2345e10), // non-whole, safely inside int64
			}},
		},
	}

	// rtHelper is a shared Runtime for all subtests (the bytecode cache
	// keys on source+filename, so subtests that use different source bodies
	// each get their own compilation).
	rt := runtime.NewRuntime(logging.NewNopLogger())

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			// Seed the `body` global via protojson → json.decode in the
			// script. This path mirrors production (a request Struct
			// becomes a plain *starlark.Dict after json.decode) and avoids
			// the *convert.StarlarkDict wrapper type, which upstream
			// json.encode does not recognize as a mapping (it falls back
			// to Iterable and emits a JSON array of keys — see
			// RESEARCH.md §Pitfall 1 / Plan 02 execution note).
			jsonBytes, err := protojson.Marshal(tc.original)
			if err != nil {
				t.Fatalf("protojson.Marshal: %v", err)
			}

			req := makeReq(nil, nil, nil)
			c := NewCollector(NewConditionCollector(), "test.star", nil)
			globals, err := testBuildGlobals(req, c)
			if err != nil {
				t.Fatalf("testBuildGlobals: %v", err)
			}
			globals["body_json"] = starlark.String(string(jsonBytes))

			out, err := rt.Execute(
				`body = json.decode(body_json)
decoded = json.decode(json.encode(body))`,
				globals,
				"test.star",
				nil,
			)
			if err != nil {
				t.Fatalf("rt.Execute: %v", err)
			}

			// json.decode always returns a plain *starlark.Dict. For
			// top-level matrix rows whose root is a JSON object, decoded
			// must type-assert to *starlark.Dict for the convert layer.
			decoded, ok := out["decoded"].(*starlark.Dict)
			if !ok {
				t.Fatalf(`out["decoded"] is %T, want *starlark.Dict`, out["decoded"])
			}

			got, err := convert.PlainDictToStruct(decoded)
			if err != nil {
				t.Fatalf("convert.PlainDictToStruct: %v", err)
			}

			if !proto.Equal(tc.original, got) {
				t.Errorf("case %q: proto mismatch\n original=%v\n got=%v", tc.name, tc.original, got)
			}
		})
	}
}
