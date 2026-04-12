package builtins

import (
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
// Phase 31 Plan 02 — builtins/yaml_test.go
//
// Three-layer test coverage for the `yaml` module:
//
//   Layer 1 (unit on BuildGlobals output):
//     - TestBuildGlobals_YAMLModule
//
//   Layer 2 (in-process via Runtime.Execute):
//     - TestYAML_Encode
//     - TestYAML_Encode_AllDictTypes
//     - TestYAML_Decode
//     - TestYAML_DecodeStream
//     - TestYAML_NegativeCases
//
//   Layer 3 (protobuf round-trip through convert.PlainDictToStruct):
//     - TestYAML_RoundTrip_Protobuf
//
// Fixtures are all inline Go string literals (no external fixture files).
// ---------------------------------------------------------------------------

// runYAMLScript compiles and runs a Starlark source string against the full
// BuildGlobals predeclared set (which includes `yaml`) via Runtime.Execute,
// returning the post-execution globals. Fails the test on any error.
func runYAMLScript(t *testing.T, src string) starlark.StringDict {
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

// runYAMLScriptExpectError runs a Starlark source string via Runtime.Execute,
// expecting a non-nil error whose message contains wantErrSubstr (case-
// insensitive). Fails the test if the script succeeds or if the error message
// does not contain the substring.
func runYAMLScriptExpectError(t *testing.T, src string, wantErrSubstr string) {
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

func TestBuildGlobals_YAMLModule(t *testing.T) {
	req := makeReq(nil, nil, nil)
	c := NewCollector(NewConditionCollector(), "test.star", nil)

	globals, err := testBuildGlobals(req, c)
	if err != nil {
		t.Fatalf("testBuildGlobals error: %v", err)
	}

	v, ok := globals["yaml"]
	if !ok {
		t.Fatal(`globals["yaml"] missing -- yaml module not registered in BuildGlobals`)
	}

	mod, ok := v.(*starlarkstruct.Module)
	if !ok {
		t.Fatalf(`globals["yaml"] is %T, want *starlarkstruct.Module`, v)
	}

	if mod.Name != "yaml" {
		t.Errorf("mod.Name = %q, want %q", mod.Name, "yaml")
	}

	wantMembers := []string{"encode", "decode", "decode_stream"}
	for _, name := range wantMembers {
		if _, ok := mod.Members[name]; !ok {
			t.Errorf(`yaml.Members missing %q`, name)
		}
	}

	// Guard against drift that silently adds or removes a member.
	if got := len(mod.Members); got != len(wantMembers) {
		t.Errorf("len(mod.Members) = %d, want %d (yaml module drift?)", got, len(wantMembers))
	}
}

// ---------------------------------------------------------------------------
// Layer 2 — in-process tests via Runtime.Execute
// ---------------------------------------------------------------------------

// TestYAML_Encode covers ENC-05.
func TestYAML_Encode(t *testing.T) {
	// Dict with sorted keys (sigs.k8s.io/yaml sorts alphabetically).
	out := runYAMLScript(t, `
result = yaml.encode({"kind": "ConfigMap", "apiVersion": "v1"})
`)
	got := string(out["result"].(starlark.String))
	// apiVersion should come before kind (alphabetical sort).
	apiIdx := strings.Index(got, "apiVersion")
	kindIdx := strings.Index(got, "kind")
	if apiIdx < 0 || kindIdx < 0 || apiIdx >= kindIdx {
		t.Errorf("yaml.encode dict keys not sorted alphabetically:\n%s", got)
	}

	// Nested dicts produce block style.
	out = runYAMLScript(t, `
result = yaml.encode({"metadata": {"name": "test", "labels": {"app": "web"}}})
`)
	nested := string(out["result"].(starlark.String))
	if !strings.Contains(nested, "metadata:") {
		t.Errorf("yaml.encode nested dict missing block style:\n%s", nested)
	}
	if !strings.Contains(nested, "  name: test") {
		t.Errorf("yaml.encode nested dict missing indented key:\n%s", nested)
	}

	// None -> null
	out = runYAMLScript(t, `result = yaml.encode(None)`)
	if string(out["result"].(starlark.String)) != "null" {
		t.Errorf("yaml.encode(None) = %q, want %q", out["result"], "null")
	}

	// Bool -> true/false
	out = runYAMLScript(t, `result = yaml.encode(True)`)
	if string(out["result"].(starlark.String)) != "true" {
		t.Errorf("yaml.encode(True) = %q, want %q", out["result"], "true")
	}

	out = runYAMLScript(t, `result = yaml.encode(False)`)
	if string(out["result"].(starlark.String)) != "false" {
		t.Errorf("yaml.encode(False) = %q, want %q", out["result"], "false")
	}

	// Int
	out = runYAMLScript(t, `result = yaml.encode(42)`)
	if string(out["result"].(starlark.String)) != "42" {
		t.Errorf("yaml.encode(42) = %q, want %q", out["result"], "42")
	}

	// Float
	out = runYAMLScript(t, `result = yaml.encode(3.14)`)
	if string(out["result"].(starlark.String)) != "3.14" {
		t.Errorf("yaml.encode(3.14) = %q, want %q", out["result"], "3.14")
	}

	// List
	out = runYAMLScript(t, `result = yaml.encode([1, 2, 3])`)
	listYAML := string(out["result"].(starlark.String))
	if !strings.Contains(listYAML, "- 1") {
		t.Errorf("yaml.encode([1,2,3]) missing sequence items:\n%s", listYAML)
	}

	// Tuple -> sequence (YAML has no tuple concept)
	out = runYAMLScript(t, `result = yaml.encode((1, 2, 3))`)
	tupleYAML := string(out["result"].(starlark.String))
	if !strings.Contains(tupleYAML, "- 1") {
		t.Errorf("yaml.encode((1,2,3)) missing sequence items:\n%s", tupleYAML)
	}

	// Empty dict
	out = runYAMLScript(t, `result = yaml.encode({})`)
	if string(out["result"].(starlark.String)) != "{}" {
		t.Errorf("yaml.encode({}) = %q, want %q", out["result"], "{}")
	}
}

// TestYAML_Encode_AllDictTypes verifies all three dict types encode identically.
func TestYAML_Encode_AllDictTypes(t *testing.T) {
	// Build a protobuf struct, convert to StarlarkDict, encode, compare with
	// plain dict encoding.
	str := structpb.NewStringValue
	num := structpb.NewNumberValue
	original := &structpb.Struct{Fields: map[string]*structpb.Value{
		"apiVersion": str("v1"),
		"kind":       str("ConfigMap"),
		"replicas":   num(3),
	}}

	// Convert to StarlarkDict (frozen).
	starlarkDict, err := convert.StructToStarlark(original, true)
	if err != nil {
		t.Fatalf("StructToStarlark: %v", err)
	}

	req := makeReq(nil, nil, nil)
	c := NewCollector(NewConditionCollector(), "test.star", nil)
	globals, err := testBuildGlobals(req, c)
	if err != nil {
		t.Fatalf("testBuildGlobals: %v", err)
	}

	// Inject the StarlarkDict as a global so we can yaml.encode it.
	globals["starlark_dict"] = starlarkDict

	rt := runtime.NewRuntime(logging.NewNopLogger())
	out, err := rt.Execute(`
# Encode the StarlarkDict
sd_yaml = yaml.encode(starlark_dict)

# Encode a plain dict with identical content
plain_yaml = yaml.encode({"apiVersion": "v1", "kind": "ConfigMap", "replicas": 3})

# They should produce the same YAML
match = (sd_yaml == plain_yaml)
`, globals, "test.star", nil)
	if err != nil {
		t.Fatalf("rt.Execute error: %v", err)
	}

	match, ok := out["match"].(starlark.Bool)
	if !ok || !bool(match) {
		sdYAML := string(out["sd_yaml"].(starlark.String))
		plainYAML := string(out["plain_yaml"].(starlark.String))
		t.Errorf("StarlarkDict and plain dict yaml.encode mismatch:\nStarlarkDict:\n%s\nPlain:\n%s", sdYAML, plainYAML)
	}
}

// TestYAML_Decode covers ENC-06.
func TestYAML_Decode(t *testing.T) {
	// Single-doc YAML with string, int, float, bool, null.
	out := runYAMLScript(t, `
decoded = yaml.decode("apiVersion: v1\nkind: ConfigMap\nreplicas: 3\nenabled: true\nratio: 1.5\nmissing: null\n")
api = decoded["apiVersion"]
kind = decoded["kind"]
replicas = decoded["replicas"]
enabled = decoded["enabled"]
ratio = decoded["ratio"]
missing = decoded["missing"]
`)
	// Strings
	assertString(t, out, "api", "v1")
	assertString(t, out, "kind", "ConfigMap")

	// Integer (not float!) — same as json.decode
	if _, ok := out["replicas"].(starlark.Int); !ok {
		t.Errorf("yaml.decode integer is %T, want starlark.Int", out["replicas"])
	}
	assertInt(t, out, "replicas", 3)

	// Bool
	assertBool(t, out, "enabled", true)

	// Float
	if _, ok := out["ratio"].(starlark.Float); !ok {
		t.Errorf("yaml.decode float is %T, want starlark.Float", out["ratio"])
	}

	// Null -> None
	if out["missing"] != starlark.None {
		t.Errorf("yaml.decode null = %v, want None", out["missing"])
	}

	// Nested structure
	out = runYAMLScript(t, `
decoded = yaml.decode("metadata:\n  name: test\n  labels:\n    app: web\n")
name = decoded["metadata"]["name"]
app = decoded["metadata"]["labels"]["app"]
`)
	assertString(t, out, "name", "test")
	assertString(t, out, "app", "web")
}

// TestYAML_DecodeStream covers ENC-07.
func TestYAML_DecodeStream(t *testing.T) {
	// Multi-doc with --- separators.
	out := runYAMLScript(t, `
docs = yaml.decode_stream("apiVersion: v1\nkind: ConfigMap\n---\napiVersion: v1\nkind: Service\n")
count = len(docs)
first_kind = docs[0]["kind"]
second_kind = docs[1]["kind"]
`)
	assertInt(t, out, "count", 2)
	assertString(t, out, "first_kind", "ConfigMap")
	assertString(t, out, "second_kind", "Service")

	// Trailing --- produces no extra entry.
	out = runYAMLScript(t, `
docs = yaml.decode_stream("a: 1\n---\nb: 2\n---\n")
count = len(docs)
`)
	assertInt(t, out, "count", 2)

	// Leading ---.
	out = runYAMLScript(t, `
docs = yaml.decode_stream("---\na: 1\n---\nb: 2\n")
count = len(docs)
`)
	assertInt(t, out, "count", 2)

	// Empty input returns empty list.
	out = runYAMLScript(t, `
docs = yaml.decode_stream("")
count = len(docs)
`)
	assertInt(t, out, "count", 0)

	// Whitespace-only docs are skipped.
	out = runYAMLScript(t, `
docs = yaml.decode_stream("---\n\n---\na: 1\n---\n  \n---\n")
count = len(docs)
`)
	assertInt(t, out, "count", 1)
}

// TestYAML_NegativeCases asserts that bad inputs fail with expected errors.
func TestYAML_NegativeCases(t *testing.T) {
	// Invalid YAML decode
	runYAMLScriptExpectError(t,
		`x = yaml.decode("---\n- :\n  - :\n    - {a: {b: [c: d]}}")`,
		"yaml.decode",
	)

	// Unsupported encode type (e.g., a set)
	runYAMLScriptExpectError(t,
		`x = yaml.encode(set([1, 2, 3]))`,
		"unsupported type",
	)

	// Wrong argument type to decode (int instead of string)
	runYAMLScriptExpectError(t,
		`x = yaml.decode(42)`,
		"got int",
	)

	// Wrong argument type to decode_stream (int instead of string)
	runYAMLScriptExpectError(t,
		`x = yaml.decode_stream(42)`,
		"got int",
	)
}

// ---------------------------------------------------------------------------
// Layer 3 — full protobuf round-trip through convert.PlainDictToStruct
// ---------------------------------------------------------------------------

func TestYAML_RoundTrip_Protobuf(t *testing.T) {
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
			name: "nested_dicts",
			original: &structpb.Struct{Fields: map[string]*structpb.Value{
				"spec": stct(map[string]*structpb.Value{
					"forProvider": stct(map[string]*structpb.Value{
						"region": str("us-east-1"),
						"tags": stct(map[string]*structpb.Value{
							"env": str("prod"),
						}),
					}),
				}),
			}},
		},
		{
			name: "lists_of_dicts",
			original: &structpb.Struct{Fields: map[string]*structpb.Value{
				"spec": stct(map[string]*structpb.Value{
					"containers": lst(
						structpb.NewStructValue(&structpb.Struct{Fields: map[string]*structpb.Value{
							"name":  str("app"),
							"image": str("nginx:1.21"),
						}}),
						structpb.NewStructValue(&structpb.Struct{Fields: map[string]*structpb.Value{
							"name":  str("sidecar"),
							"image": str("envoy:1.20"),
						}}),
					),
				}),
			}},
		},
		{
			name: "string_values",
			original: &structpb.Struct{Fields: map[string]*structpb.Value{
				"simple":  str("hello"),
				"empty":   str(""),
				"unicode": str("héllo 🌍"),
			}},
		},
		{
			name: "integer_values",
			original: &structpb.Struct{Fields: map[string]*structpb.Value{
				"zero": num(0),
				"one":  num(1),
				"neg":  num(-1),
				"max":  num(9007199254740991),  // 2^53 - 1
				"min":  num(-9007199254740991), // -(2^53 - 1)
			}},
		},
		{
			name: "float_values",
			original: &structpb.Struct{Fields: map[string]*structpb.Value{
				"half":     num(1.5),
				"neghalf":  num(-0.5),
				"pi":       num(3.14159),
				"smallsci": num(1.23e-5),
			}},
		},
		{
			name: "boolean_values",
			original: &structpb.Struct{Fields: map[string]*structpb.Value{
				"enabled":  structpb.NewBoolValue(true),
				"disabled": structpb.NewBoolValue(false),
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
	}

	rt := runtime.NewRuntime(logging.NewNopLogger())

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			// Seed body via protojson -> json.decode to get a plain *starlark.Dict,
			// then yaml.encode -> yaml.decode round-trip.
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
decoded = yaml.decode(yaml.encode(body))`,
				globals,
				"test.star",
				nil,
			)
			if err != nil {
				t.Fatalf("rt.Execute: %v", err)
			}

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
