package builtins

import (
	"math"
	"strings"
	"testing"

	fnv1 "github.com/crossplane/function-sdk-go/proto/v1"
	"go.starlark.net/starlark"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/wompipomp/function-starlark/convert"
)

// ---------------------------------------------------------------------------
// buildContextDict tests
// ---------------------------------------------------------------------------

func TestBuildContextDict_NilContext(t *testing.T) {
	req := &fnv1.RunFunctionRequest{} // no context
	d, err := buildContextDict(req)
	if err != nil {
		t.Fatalf("buildContextDict error: %v", err)
	}
	if d.Len() != 0 {
		t.Errorf("Len() = %d, want 0", d.Len())
	}
}

func TestBuildContextDict_Populated(t *testing.T) {
	req := &fnv1.RunFunctionRequest{
		Context: &structpb.Struct{
			Fields: map[string]*structpb.Value{
				"apiextensions.crossplane.io/environment": structpb.NewStructValue(&structpb.Struct{
					Fields: map[string]*structpb.Value{
						"region": structpb.NewStringValue("us-west-2"),
					},
				}),
				"custom-key": structpb.NewStringValue("custom-value"),
			},
		},
	}

	d, err := buildContextDict(req)
	if err != nil {
		t.Fatalf("buildContextDict error: %v", err)
	}

	if d.Len() != 2 {
		t.Errorf("Len() = %d, want 2", d.Len())
	}

	// Check custom-key
	v, found, err := d.Get(starlark.String("custom-key"))
	if err != nil || !found {
		t.Fatalf("missing 'custom-key': found=%v, err=%v", found, err)
	}
	if v != starlark.String("custom-value") {
		t.Errorf("custom-key = %v, want 'custom-value'", v)
	}

	// Check environment key (should be a dict)
	envVal, found, err := d.Get(starlark.String("apiextensions.crossplane.io/environment"))
	if err != nil || !found {
		t.Fatalf("missing env key: found=%v, err=%v", found, err)
	}
	// The environment value should be a dict-like value (plain starlark.Dict)
	envDict, ok := envVal.(*starlark.Dict)
	if !ok {
		t.Fatalf("env = %T, want *starlark.Dict", envVal)
	}
	regionVal, found, err := envDict.Get(starlark.String("region"))
	if err != nil || !found {
		t.Fatalf("missing region in env: found=%v, err=%v", found, err)
	}
	if regionVal != starlark.String("us-west-2") {
		t.Errorf("region = %v, want 'us-west-2'", regionVal)
	}
}

func TestBuildContextDict_Mutable(t *testing.T) {
	req := &fnv1.RunFunctionRequest{
		Context: &structpb.Struct{
			Fields: map[string]*structpb.Value{
				"existing": structpb.NewStringValue("value"),
			},
		},
	}

	d, err := buildContextDict(req)
	if err != nil {
		t.Fatal(err)
	}

	// Should be mutable -- scripts can add/modify keys
	if err := d.SetKey(starlark.String("new-key"), starlark.String("new-value")); err != nil {
		t.Errorf("context dict should be mutable: %v", err)
	}
}

// ---------------------------------------------------------------------------
// buildEnvironmentDict tests
// ---------------------------------------------------------------------------

func TestBuildEnvironmentDict_NilContext(t *testing.T) {
	req := &fnv1.RunFunctionRequest{} // no context at all
	d, err := buildEnvironmentDict(req)
	if err != nil {
		t.Fatalf("buildEnvironmentDict error: %v", err)
	}
	if d.Len() != 0 {
		t.Errorf("Len() = %d, want 0", d.Len())
	}
	// Should be frozen
	if err := d.SetField("x", starlark.MakeInt(1)); err == nil {
		t.Error("environment dict should be frozen")
	}
}

func TestBuildEnvironmentDict_NoEnvKey(t *testing.T) {
	req := &fnv1.RunFunctionRequest{
		Context: &structpb.Struct{
			Fields: map[string]*structpb.Value{
				"other-key": structpb.NewStringValue("value"),
			},
		},
	}

	d, err := buildEnvironmentDict(req)
	if err != nil {
		t.Fatal(err)
	}
	if d.Len() != 0 {
		t.Errorf("Len() = %d, want 0", d.Len())
	}
	// Should be frozen
	if err := d.SetField("x", starlark.MakeInt(1)); err == nil {
		t.Error("environment dict should be frozen")
	}
}

func TestBuildEnvironmentDict_Populated(t *testing.T) {
	req := &fnv1.RunFunctionRequest{
		Context: &structpb.Struct{
			Fields: map[string]*structpb.Value{
				"apiextensions.crossplane.io/environment": structpb.NewStructValue(&structpb.Struct{
					Fields: map[string]*structpb.Value{
						"region":  structpb.NewStringValue("eu-west-1"),
						"cluster": structpb.NewStringValue("prod"),
					},
				}),
			},
		},
	}

	d, err := buildEnvironmentDict(req)
	if err != nil {
		t.Fatal(err)
	}
	if d.Len() != 2 {
		t.Errorf("Len() = %d, want 2", d.Len())
	}

	// Check region
	regionVal, err := d.Attr("region")
	if err != nil {
		t.Fatal(err)
	}
	if regionVal != starlark.String("eu-west-1") {
		t.Errorf("region = %v, want 'eu-west-1'", regionVal)
	}
}

func TestBuildEnvironmentDict_Frozen(t *testing.T) {
	req := &fnv1.RunFunctionRequest{
		Context: &structpb.Struct{
			Fields: map[string]*structpb.Value{
				"apiextensions.crossplane.io/environment": structpb.NewStructValue(&structpb.Struct{
					Fields: map[string]*structpb.Value{
						"region": structpb.NewStringValue("eu-west-1"),
					},
				}),
			},
		},
	}

	d, err := buildEnvironmentDict(req)
	if err != nil {
		t.Fatal(err)
	}

	// Should be frozen (read-only)
	if err := d.SetField("newField", starlark.MakeInt(1)); err == nil {
		t.Error("environment dict should be frozen")
	}
}

func TestBuildEnvironmentDict_NonStructValue(t *testing.T) {
	// If environment key is not a struct value, return empty frozen dict
	req := &fnv1.RunFunctionRequest{
		Context: &structpb.Struct{
			Fields: map[string]*structpb.Value{
				"apiextensions.crossplane.io/environment": structpb.NewStringValue("not-a-struct"),
			},
		},
	}

	d, err := buildEnvironmentDict(req)
	if err != nil {
		t.Fatal(err)
	}
	if d.Len() != 0 {
		t.Errorf("Len() = %d, want 0 for non-struct env value", d.Len())
	}
}

// ---------------------------------------------------------------------------
// ApplyContext tests
// ---------------------------------------------------------------------------

func TestApplyContext_EmptyDict(t *testing.T) {
	d := new(starlark.Dict)
	rsp := &fnv1.RunFunctionResponse{}

	if err := ApplyContext(rsp, d); err != nil {
		t.Fatalf("ApplyContext error: %v", err)
	}

	if rsp.Context == nil {
		t.Fatal("Context should be set (empty struct)")
	}
	if len(rsp.Context.GetFields()) != 0 {
		t.Errorf("fields = %d, want 0", len(rsp.Context.GetFields()))
	}
}

func TestApplyContext_Populated(t *testing.T) {
	d := new(starlark.Dict)
	_ = d.SetKey(starlark.String("key1"), starlark.String("value1"))
	_ = d.SetKey(starlark.String("key2"), starlark.MakeInt(42))

	rsp := &fnv1.RunFunctionResponse{}

	if err := ApplyContext(rsp, d); err != nil {
		t.Fatalf("ApplyContext error: %v", err)
	}

	if rsp.Context == nil {
		t.Fatal("Context is nil")
	}
	fields := rsp.Context.GetFields()
	if fields["key1"].GetStringValue() != "value1" {
		t.Errorf("key1 = %v, want 'value1'", fields["key1"])
	}
	if fields["key2"].GetNumberValue() != 42 {
		t.Errorf("key2 = %v, want 42", fields["key2"])
	}
}

func TestApplyContext_PreservesKeys(t *testing.T) {
	// When context dict starts from request context, unmodified keys should
	// pass through to the response.
	req := &fnv1.RunFunctionRequest{
		Context: &structpb.Struct{
			Fields: map[string]*structpb.Value{
				"original":  structpb.NewStringValue("preserved"),
				"to-change": structpb.NewStringValue("old"),
			},
		},
	}

	d, err := buildContextDict(req)
	if err != nil {
		t.Fatal(err)
	}

	// Modify one key, leave the other
	_ = d.SetKey(starlark.String("to-change"), starlark.String("new"))

	rsp := &fnv1.RunFunctionResponse{}
	if err := ApplyContext(rsp, d); err != nil {
		t.Fatal(err)
	}

	fields := rsp.Context.GetFields()
	if fields["original"].GetStringValue() != "preserved" {
		t.Error("unmodified key should be preserved")
	}
	if fields["to-change"].GetStringValue() != "new" {
		t.Error("modified key should reflect new value")
	}
}

func TestApplyContext_WrongType(t *testing.T) {
	// ApplyContext should reject non-*starlark.Dict values
	sd := convert.NewStarlarkDict(0)
	rsp := &fnv1.RunFunctionResponse{}

	err := ApplyContext(rsp, sd)
	if err == nil {
		t.Fatal("ApplyContext with *StarlarkDict should return error")
	}
	if !strings.Contains(err.Error(), "want *starlark.Dict") {
		t.Errorf("error %q should contain 'want *starlark.Dict'", err.Error())
	}
}

// ---------------------------------------------------------------------------
// protoValueToStarlarkValue tests
// ---------------------------------------------------------------------------

func TestProtoValue_NilInput(t *testing.T) {
	v, err := protoValueToStarlarkValue(nil, false)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if v != starlark.None {
		t.Errorf("got %v, want None", v)
	}
}

func TestProtoValue_NullValue(t *testing.T) {
	v, err := protoValueToStarlarkValue(structpb.NewNullValue(), false)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if v != starlark.None {
		t.Errorf("got %v, want None", v)
	}
}

func TestProtoValue_Integer(t *testing.T) {
	// A whole-number float (42.0) should produce starlark.Int, not Float.
	v, err := protoValueToStarlarkValue(structpb.NewNumberValue(42.0), false)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	intVal, ok := v.(starlark.Int)
	if !ok {
		t.Fatalf("got %T, want starlark.Int", v)
	}
	got, _ := intVal.Int64()
	if got != 42 {
		t.Errorf("got %d, want 42", got)
	}
}

func TestProtoValue_Float(t *testing.T) {
	v, err := protoValueToStarlarkValue(structpb.NewNumberValue(3.14), false)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	f, ok := v.(starlark.Float)
	if !ok {
		t.Fatalf("got %T, want starlark.Float", v)
	}
	if float64(f) != 3.14 {
		t.Errorf("got %v, want 3.14", f)
	}
}

func TestProtoValue_NaN(t *testing.T) {
	v, err := protoValueToStarlarkValue(structpb.NewNumberValue(math.NaN()), false)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	f, ok := v.(starlark.Float)
	if !ok {
		t.Fatalf("got %T, want starlark.Float", v)
	}
	if !math.IsNaN(float64(f)) {
		t.Errorf("got %v, want NaN", f)
	}
}

func TestProtoValue_Inf(t *testing.T) {
	v, err := protoValueToStarlarkValue(structpb.NewNumberValue(math.Inf(1)), false)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	f, ok := v.(starlark.Float)
	if !ok {
		t.Fatalf("got %T, want starlark.Float", v)
	}
	if !math.IsInf(float64(f), 1) {
		t.Errorf("got %v, want +Inf", f)
	}
}

func TestProtoValue_Bool(t *testing.T) {
	v, err := protoValueToStarlarkValue(structpb.NewBoolValue(true), false)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	b, ok := v.(starlark.Bool)
	if !ok {
		t.Fatalf("got %T, want starlark.Bool", v)
	}
	if b != starlark.True {
		t.Errorf("got %v, want True", b)
	}
}

func TestProtoValue_StructFrozen(t *testing.T) {
	sv := structpb.NewStructValue(&structpb.Struct{
		Fields: map[string]*structpb.Value{
			"key": structpb.NewStringValue("val"),
		},
	})
	v, err := protoValueToStarlarkValue(sv, true)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	d, ok := v.(*starlark.Dict)
	if !ok {
		t.Fatalf("got %T, want *starlark.Dict", v)
	}
	if err := d.SetKey(starlark.String("new"), starlark.None); err == nil {
		t.Error("frozen struct dict should reject SetKey")
	}
}

func TestProtoValue_ListNilValue(t *testing.T) {
	// ListValue with nil internal value should return empty list.
	v, err := protoValueToStarlarkValue(&structpb.Value{
		Kind: &structpb.Value_ListValue{ListValue: nil},
	}, false)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	l, ok := v.(*starlark.List)
	if !ok {
		t.Fatalf("got %T, want *starlark.List", v)
	}
	if l.Len() != 0 {
		t.Errorf("Len() = %d, want 0", l.Len())
	}
}

func TestProtoValue_ListFrozen(t *testing.T) {
	lv := structpb.NewListValue(&structpb.ListValue{
		Values: []*structpb.Value{structpb.NewStringValue("a")},
	})
	v, err := protoValueToStarlarkValue(lv, true)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	l, ok := v.(*starlark.List)
	if !ok {
		t.Fatalf("got %T, want *starlark.List", v)
	}
	if err := l.Append(starlark.None); err == nil {
		t.Error("frozen list should reject Append")
	}
}

// ---------------------------------------------------------------------------
// Context round-trip tests
// ---------------------------------------------------------------------------

func TestContextRoundTrip(t *testing.T) {
	// Context from request -> starlark.Dict -> ApplyContext -> response
	// should produce equivalent protobuf.
	req := &fnv1.RunFunctionRequest{
		Context: &structpb.Struct{
			Fields: map[string]*structpb.Value{
				"simple":   structpb.NewStringValue("hello"),
				"number":   structpb.NewNumberValue(3.14),
				"flag":     structpb.NewBoolValue(true),
				"nullable": structpb.NewNullValue(),
				"nested": structpb.NewStructValue(&structpb.Struct{
					Fields: map[string]*structpb.Value{
						"inner": structpb.NewStringValue("deep"),
					},
				}),
				"list": structpb.NewListValue(&structpb.ListValue{
					Values: []*structpb.Value{
						structpb.NewStringValue("a"),
						structpb.NewNumberValue(1),
					},
				}),
			},
		},
	}

	d, err := buildContextDict(req)
	if err != nil {
		t.Fatalf("buildContextDict error: %v", err)
	}

	rsp := &fnv1.RunFunctionResponse{}
	if err := ApplyContext(rsp, d); err != nil {
		t.Fatalf("ApplyContext error: %v", err)
	}

	// Verify round-trip
	fields := rsp.Context.GetFields()
	if fields["simple"].GetStringValue() != "hello" {
		t.Error("simple string lost in round-trip")
	}
	if fields["number"].GetNumberValue() != 3.14 {
		t.Error("number lost in round-trip")
	}
	if fields["flag"].GetBoolValue() != true {
		t.Error("bool lost in round-trip")
	}
	if _, ok := fields["nullable"].GetKind().(*structpb.Value_NullValue); !ok {
		t.Error("null value lost in round-trip")
	}
	nestedFields := fields["nested"].GetStructValue().GetFields()
	if nestedFields["inner"].GetStringValue() != "deep" {
		t.Error("nested struct lost in round-trip")
	}
	listVals := fields["list"].GetListValue().GetValues()
	if len(listVals) != 2 {
		t.Errorf("list length = %d, want 2", len(listVals))
	}
	if listVals[0].GetStringValue() != "a" {
		t.Error("list[0] lost in round-trip")
	}
}
