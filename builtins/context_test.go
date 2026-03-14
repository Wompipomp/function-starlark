package builtins

import (
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
