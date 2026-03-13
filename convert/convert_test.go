package convert

import (
	"math"
	"math/big"
	"testing"

	"github.com/google/go-cmp/cmp"
	"go.starlark.net/starlark"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/structpb"
)

// ---------------------------------------------------------------------------
// StructToStarlark tests
// ---------------------------------------------------------------------------

func TestStructToStarlark_NilStruct(t *testing.T) {
	d, err := StructToStarlark(nil, false)
	if err != nil {
		t.Fatalf("StructToStarlark(nil, false) error: %v", err)
	}
	if d == nil {
		t.Fatal("StructToStarlark(nil, false) returned nil")
	}
	if d.Len() != 0 {
		t.Errorf("Len() = %d, want 0", d.Len())
	}
}

func TestStructToStarlark_NilValue(t *testing.T) {
	s := &structpb.Struct{
		Fields: map[string]*structpb.Value{
			"nilfield": nil,
		},
	}
	d, err := StructToStarlark(s, false)
	if err != nil {
		t.Fatalf("StructToStarlark error: %v", err)
	}
	v, err := d.Attr("nilfield")
	if err != nil {
		t.Fatalf("Attr error: %v", err)
	}
	if v != starlark.None {
		t.Errorf("nil Value -> %v (%T), want starlark.None", v, v)
	}
}

func TestStructToStarlark_NumberInteger(t *testing.T) {
	s := &structpb.Struct{
		Fields: map[string]*structpb.Value{
			"replicas": structpb.NewNumberValue(3.0),
		},
	}
	d, err := StructToStarlark(s, false)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	v, err := d.Attr("replicas")
	if err != nil {
		t.Fatalf("Attr error: %v", err)
	}
	si, ok := v.(starlark.Int)
	if !ok {
		t.Fatalf("replicas = %T, want starlark.Int", v)
	}
	got, _ := si.Int64()
	if got != 3 {
		t.Errorf("replicas = %d, want 3", got)
	}
}

func TestStructToStarlark_NumberFloat(t *testing.T) {
	s := &structpb.Struct{
		Fields: map[string]*structpb.Value{
			"ratio": structpb.NewNumberValue(3.14),
		},
	}
	d, err := StructToStarlark(s, false)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	v, err := d.Attr("ratio")
	if err != nil {
		t.Fatalf("Attr error: %v", err)
	}
	sf, ok := v.(starlark.Float)
	if !ok {
		t.Fatalf("ratio = %T, want starlark.Float", v)
	}
	if float64(sf) != 3.14 {
		t.Errorf("ratio = %v, want 3.14", sf)
	}
}

func TestStructToStarlark_String(t *testing.T) {
	s := &structpb.Struct{
		Fields: map[string]*structpb.Value{
			"name": structpb.NewStringValue("web"),
		},
	}
	d, err := StructToStarlark(s, false)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	v, err := d.Attr("name")
	if err != nil {
		t.Fatal(err)
	}
	if v != starlark.String("web") {
		t.Errorf("name = %v, want \"web\"", v)
	}
}

func TestStructToStarlark_Bool(t *testing.T) {
	s := &structpb.Struct{
		Fields: map[string]*structpb.Value{
			"enabled": structpb.NewBoolValue(true),
		},
	}
	d, err := StructToStarlark(s, false)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	v, err := d.Attr("enabled")
	if err != nil {
		t.Fatal(err)
	}
	if v != starlark.True {
		t.Errorf("enabled = %v, want True", v)
	}
}

func TestStructToStarlark_Null(t *testing.T) {
	s := &structpb.Struct{
		Fields: map[string]*structpb.Value{
			"empty": structpb.NewNullValue(),
		},
	}
	d, err := StructToStarlark(s, false)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	v, err := d.Attr("empty")
	if err != nil {
		t.Fatal(err)
	}
	if v != starlark.None {
		t.Errorf("null = %v, want None", v)
	}
}

func TestStructToStarlark_NestedStruct(t *testing.T) {
	inner := &structpb.Struct{
		Fields: map[string]*structpb.Value{
			"replicas": structpb.NewNumberValue(3),
		},
	}
	s := &structpb.Struct{
		Fields: map[string]*structpb.Value{
			"spec": structpb.NewStructValue(inner),
		},
	}
	d, err := StructToStarlark(s, false)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	specVal, err := d.Attr("spec")
	if err != nil {
		t.Fatal(err)
	}
	specDict, ok := specVal.(*StarlarkDict)
	if !ok {
		t.Fatalf("spec = %T, want *StarlarkDict", specVal)
	}
	replVal, err := specDict.Attr("replicas")
	if err != nil {
		t.Fatal(err)
	}
	ri, ok := replVal.(starlark.Int)
	if !ok {
		t.Fatalf("replicas = %T, want starlark.Int", replVal)
	}
	got, _ := ri.Int64()
	if got != 3 {
		t.Errorf("replicas = %d, want 3", got)
	}
}

func TestStructToStarlark_List(t *testing.T) {
	lv := &structpb.ListValue{
		Values: []*structpb.Value{
			structpb.NewNumberValue(80),
			structpb.NewNumberValue(443),
		},
	}
	s := &structpb.Struct{
		Fields: map[string]*structpb.Value{
			"ports": structpb.NewListValue(lv),
		},
	}
	d, err := StructToStarlark(s, false)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	v, err := d.Attr("ports")
	if err != nil {
		t.Fatal(err)
	}
	list, ok := v.(*starlark.List)
	if !ok {
		t.Fatalf("ports = %T, want *starlark.List", v)
	}
	if list.Len() != 2 {
		t.Errorf("ports len = %d, want 2", list.Len())
	}
	// Both should be Int (whole numbers)
	for i := 0; i < list.Len(); i++ {
		if _, ok := list.Index(i).(starlark.Int); !ok {
			t.Errorf("ports[%d] = %T, want starlark.Int", i, list.Index(i))
		}
	}
}

func TestStructToStarlark_FreezeTrue(t *testing.T) {
	s := &structpb.Struct{
		Fields: map[string]*structpb.Value{
			"x": structpb.NewNumberValue(1),
		},
	}
	d, err := StructToStarlark(s, true)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	// Should be frozen -- mutation must fail.
	if err := d.SetField("y", starlark.MakeInt(2)); err == nil {
		t.Error("SetField on frozen dict should return error")
	}
}

func TestStructToStarlark_FreezeFalse(t *testing.T) {
	s := &structpb.Struct{
		Fields: map[string]*structpb.Value{
			"x": structpb.NewNumberValue(1),
		},
	}
	d, err := StructToStarlark(s, false)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	// Should be mutable.
	if err := d.SetField("y", starlark.MakeInt(2)); err != nil {
		t.Errorf("SetField on mutable dict: %v", err)
	}
}

func TestStructToStarlark_NaN(t *testing.T) {
	s := &structpb.Struct{
		Fields: map[string]*structpb.Value{
			"nan": structpb.NewNumberValue(math.NaN()),
		},
	}
	d, err := StructToStarlark(s, false)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	v, err := d.Attr("nan")
	if err != nil {
		t.Fatal(err)
	}
	f, ok := v.(starlark.Float)
	if !ok {
		t.Fatalf("NaN = %T, want starlark.Float", v)
	}
	if !math.IsNaN(float64(f)) {
		t.Errorf("NaN = %v, want NaN", f)
	}
}

func TestStructToStarlark_Inf(t *testing.T) {
	s := &structpb.Struct{
		Fields: map[string]*structpb.Value{
			"inf": structpb.NewNumberValue(math.Inf(1)),
		},
	}
	d, err := StructToStarlark(s, false)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	v, err := d.Attr("inf")
	if err != nil {
		t.Fatal(err)
	}
	f, ok := v.(starlark.Float)
	if !ok {
		t.Fatalf("+Inf = %T, want starlark.Float", v)
	}
	if !math.IsInf(float64(f), 1) {
		t.Errorf("+Inf = %v, want +Inf", f)
	}
}

func TestStructToStarlark_EmptyStruct(t *testing.T) {
	s := &structpb.Struct{Fields: map[string]*structpb.Value{}}
	d, err := StructToStarlark(s, false)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if d.Len() != 0 {
		t.Errorf("Len() = %d, want 0", d.Len())
	}
}

func TestStructToStarlark_EmptyList(t *testing.T) {
	s := &structpb.Struct{
		Fields: map[string]*structpb.Value{
			"entries": structpb.NewListValue(&structpb.ListValue{}),
		},
	}
	d, err := StructToStarlark(s, false)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	v, err := d.Attr("entries")
	if err != nil {
		t.Fatal(err)
	}
	list, ok := v.(*starlark.List)
	if !ok {
		t.Fatalf("entries = %T, want *starlark.List", v)
	}
	if list.Len() != 0 {
		t.Errorf("list len = %d, want 0", list.Len())
	}
}

// ---------------------------------------------------------------------------
// StarlarkToStruct tests
// ---------------------------------------------------------------------------

func TestStarlarkToStruct_BasicDict(t *testing.T) {
	d := NewStarlarkDict(0)
	_ = d.SetField("name", starlark.String("web"))
	_ = d.SetField("replicas", starlark.MakeInt(3))
	_ = d.SetField("enabled", starlark.True)
	_ = d.SetField("empty", starlark.None)

	s, err := StarlarkToStruct(d)
	if err != nil {
		t.Fatalf("StarlarkToStruct error: %v", err)
	}
	if s == nil {
		t.Fatal("StarlarkToStruct returned nil")
	}
	fields := s.GetFields()
	if len(fields) != 4 {
		t.Errorf("fields count = %d, want 4", len(fields))
	}
}

func TestStarlarkToStruct_Int(t *testing.T) {
	d := NewStarlarkDict(0)
	_ = d.SetField("n", starlark.MakeInt(3))

	s, err := StarlarkToStruct(d)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	v := s.GetFields()["n"]
	nv, ok := v.GetKind().(*structpb.Value_NumberValue)
	if !ok {
		t.Fatalf("n kind = %T, want NumberValue", v.GetKind())
	}
	if nv.NumberValue != 3.0 {
		t.Errorf("n = %v, want 3.0", nv.NumberValue)
	}
}

func TestStarlarkToStruct_Float(t *testing.T) {
	d := NewStarlarkDict(0)
	_ = d.SetField("f", starlark.Float(3.14))

	s, err := StarlarkToStruct(d)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	v := s.GetFields()["f"]
	nv, ok := v.GetKind().(*structpb.Value_NumberValue)
	if !ok {
		t.Fatalf("f kind = %T, want NumberValue", v.GetKind())
	}
	if nv.NumberValue != 3.14 {
		t.Errorf("f = %v, want 3.14", nv.NumberValue)
	}
}

func TestStarlarkToStruct_String(t *testing.T) {
	d := NewStarlarkDict(0)
	_ = d.SetField("s", starlark.String("hello"))

	s, err := StarlarkToStruct(d)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	v := s.GetFields()["s"]
	sv, ok := v.GetKind().(*structpb.Value_StringValue)
	if !ok {
		t.Fatalf("s kind = %T, want StringValue", v.GetKind())
	}
	if sv.StringValue != "hello" {
		t.Errorf("s = %q, want %q", sv.StringValue, "hello")
	}
}

func TestStarlarkToStruct_Bool(t *testing.T) {
	d := NewStarlarkDict(0)
	_ = d.SetField("b", starlark.True)

	s, err := StarlarkToStruct(d)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	v := s.GetFields()["b"]
	bv, ok := v.GetKind().(*structpb.Value_BoolValue)
	if !ok {
		t.Fatalf("b kind = %T, want BoolValue", v.GetKind())
	}
	if !bv.BoolValue {
		t.Error("b = false, want true")
	}
}

func TestStarlarkToStruct_None(t *testing.T) {
	d := NewStarlarkDict(0)
	_ = d.SetField("n", starlark.None)

	s, err := StarlarkToStruct(d)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	v := s.GetFields()["n"]
	_, ok := v.GetKind().(*structpb.Value_NullValue)
	if !ok {
		t.Fatalf("n kind = %T, want NullValue", v.GetKind())
	}
}

func TestStarlarkToStruct_NestedDict(t *testing.T) {
	inner := NewStarlarkDict(0)
	_ = inner.SetField("x", starlark.MakeInt(1))

	outer := NewStarlarkDict(0)
	_ = outer.SetField("inner", inner)

	s, err := StarlarkToStruct(outer)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	sv, ok := s.GetFields()["inner"].GetKind().(*structpb.Value_StructValue)
	if !ok {
		t.Fatalf("inner kind = %T, want StructValue", s.GetFields()["inner"].GetKind())
	}
	innerFields := sv.StructValue.GetFields()
	if len(innerFields) != 1 {
		t.Errorf("inner fields = %d, want 1", len(innerFields))
	}
}

func TestStarlarkToStruct_List(t *testing.T) {
	list := starlark.NewList([]starlark.Value{
		starlark.MakeInt(80),
		starlark.MakeInt(443),
	})

	d := NewStarlarkDict(0)
	_ = d.SetField("ports", list)

	s, err := StarlarkToStruct(d)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	lv, ok := s.GetFields()["ports"].GetKind().(*structpb.Value_ListValue)
	if !ok {
		t.Fatalf("ports kind = %T, want ListValue", s.GetFields()["ports"].GetKind())
	}
	if len(lv.ListValue.GetValues()) != 2 {
		t.Errorf("ports len = %d, want 2", len(lv.ListValue.GetValues()))
	}
}

func TestStarlarkToStruct_TupleError(t *testing.T) {
	d := NewStarlarkDict(0)
	_ = d.SetKey(starlark.String("t"), starlark.Tuple{starlark.MakeInt(1)})

	_, err := StarlarkToStruct(d)
	if err == nil {
		t.Fatal("StarlarkToStruct with Tuple should return error")
	}
}

func TestStarlarkToStruct_SetError(t *testing.T) {
	d := NewStarlarkDict(0)
	s := new(starlark.Set)
	_ = s.Insert(starlark.MakeInt(1))
	_ = d.SetKey(starlark.String("s"), s)

	_, err := StarlarkToStruct(d)
	if err == nil {
		t.Fatal("StarlarkToStruct with Set should return error")
	}
}

// ---------------------------------------------------------------------------
// Round-trip tests
// ---------------------------------------------------------------------------

func TestRoundTrip_K8sDeployment(t *testing.T) {
	labels := &structpb.Struct{
		Fields: map[string]*structpb.Value{
			"app": structpb.NewStringValue("web"),
		},
	}
	ports := &structpb.ListValue{
		Values: []*structpb.Value{
			structpb.NewNumberValue(80),
			structpb.NewNumberValue(443),
		},
	}
	spec := &structpb.Struct{
		Fields: map[string]*structpb.Value{
			"replicas": structpb.NewNumberValue(3),
			"ports":    structpb.NewListValue(ports),
		},
	}
	original := &structpb.Struct{
		Fields: map[string]*structpb.Value{
			"apiVersion": structpb.NewStringValue("apps/v1"),
			"kind":       structpb.NewStringValue("Deployment"),
			"labels":     structpb.NewStructValue(labels),
			"spec":       structpb.NewStructValue(spec),
			"enabled":    structpb.NewBoolValue(true),
			"optional":   structpb.NewNullValue(),
		},
	}

	// Forward: protobuf -> starlark
	dict, err := StructToStarlark(original, false)
	if err != nil {
		t.Fatalf("StructToStarlark: %v", err)
	}

	// Reverse: starlark -> protobuf
	result, err := StarlarkToStruct(dict)
	if err != nil {
		t.Fatalf("StarlarkToStruct: %v", err)
	}

	if diff := cmp.Diff(original, result, protocmp.Transform()); diff != "" {
		t.Errorf("round-trip mismatch (-want +got):\n%s", diff)
	}
}

func TestRoundTrip_DeeplyNested(t *testing.T) {
	// 4 levels deep
	level3 := &structpb.Struct{
		Fields: map[string]*structpb.Value{
			"value": structpb.NewNumberValue(42),
		},
	}
	level2 := &structpb.Struct{
		Fields: map[string]*structpb.Value{
			"level3": structpb.NewStructValue(level3),
		},
	}
	level1 := &structpb.Struct{
		Fields: map[string]*structpb.Value{
			"level2": structpb.NewStructValue(level2),
		},
	}
	original := &structpb.Struct{
		Fields: map[string]*structpb.Value{
			"level1": structpb.NewStructValue(level1),
		},
	}

	dict, err := StructToStarlark(original, false)
	if err != nil {
		t.Fatalf("StructToStarlark: %v", err)
	}
	result, err := StarlarkToStruct(dict)
	if err != nil {
		t.Fatalf("StarlarkToStruct: %v", err)
	}
	if diff := cmp.Diff(original, result, protocmp.Transform()); diff != "" {
		t.Errorf("round-trip mismatch (-want +got):\n%s", diff)
	}
}

func TestRoundTrip_MixedTypesList(t *testing.T) {
	nested := &structpb.Struct{
		Fields: map[string]*structpb.Value{
			"nested": structpb.NewStringValue("dict"),
		},
	}
	lv := &structpb.ListValue{
		Values: []*structpb.Value{
			structpb.NewNumberValue(1),
			structpb.NewStringValue("two"),
			structpb.NewBoolValue(true),
			structpb.NewNullValue(),
			structpb.NewStructValue(nested),
		},
	}
	original := &structpb.Struct{
		Fields: map[string]*structpb.Value{
			"mixed": structpb.NewListValue(lv),
		},
	}

	dict, err := StructToStarlark(original, false)
	if err != nil {
		t.Fatalf("StructToStarlark: %v", err)
	}
	result, err := StarlarkToStruct(dict)
	if err != nil {
		t.Fatalf("StarlarkToStruct: %v", err)
	}
	if diff := cmp.Diff(original, result, protocmp.Transform()); diff != "" {
		t.Errorf("round-trip mismatch (-want +got):\n%s", diff)
	}
}

// ---------------------------------------------------------------------------
// Integer precision tests
// ---------------------------------------------------------------------------

func TestIntegerPrecision_Port(t *testing.T) {
	original := &structpb.Struct{
		Fields: map[string]*structpb.Value{
			"port": structpb.NewNumberValue(65535),
		},
	}
	dict, err := StructToStarlark(original, false)
	if err != nil {
		t.Fatal(err)
	}

	// Verify it's an Int, not Float
	v, _ := dict.Attr("port")
	if _, ok := v.(starlark.Int); !ok {
		t.Fatalf("port = %T, want starlark.Int", v)
	}

	result, err := StarlarkToStruct(dict)
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(original, result, protocmp.Transform()); diff != "" {
		t.Errorf("integer precision round-trip mismatch:\n%s", diff)
	}
}

func TestIntegerPrecision_2pow53(t *testing.T) {
	// 2^53 = 9007199254740992 is the largest integer exactly representable as float64.
	val := float64(9007199254740992)
	original := &structpb.Struct{
		Fields: map[string]*structpb.Value{
			"big": structpb.NewNumberValue(val),
		},
	}
	dict, err := StructToStarlark(original, false)
	if err != nil {
		t.Fatal(err)
	}

	v, _ := dict.Attr("big")
	si, ok := v.(starlark.Int)
	if !ok {
		t.Fatalf("2^53 = %T, want starlark.Int", v)
	}
	got, _ := si.Int64()
	if got != 9007199254740992 {
		t.Errorf("2^53 = %d, want 9007199254740992", got)
	}

	result, err := StarlarkToStruct(dict)
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(original, result, protocmp.Transform()); diff != "" {
		t.Errorf("2^53 round-trip mismatch:\n%s", diff)
	}
}

func TestStarlarkToStruct_IntTooLargeForFloat64(t *testing.T) {
	// Create an integer much larger than float64 can represent
	huge := new(big.Int).Lsh(big.NewInt(1), 256) // 2^256
	d := NewStarlarkDict(0)
	_ = d.SetKey(starlark.String("huge"), starlark.MakeBigInt(huge))

	_, err := StarlarkToStruct(d)
	if err == nil {
		t.Fatal("StarlarkToStruct with huge Int should return error")
	}
}

// ---------------------------------------------------------------------------
// Edge case tests
// ---------------------------------------------------------------------------

func TestEdgeCases_NilListValue(t *testing.T) {
	// A Value_ListValue where the ListValue pointer itself is nil.
	s := &structpb.Struct{
		Fields: map[string]*structpb.Value{
			"entries": {Kind: &structpb.Value_ListValue{ListValue: nil}},
		},
	}
	d, err := StructToStarlark(s, false)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	v, err := d.Attr("entries")
	if err != nil {
		t.Fatal(err)
	}
	list, ok := v.(*starlark.List)
	if !ok {
		t.Fatalf("entries = %T, want *starlark.List", v)
	}
	if list.Len() != 0 {
		t.Errorf("nil ListValue len = %d, want 0", list.Len())
	}
}

func TestEdgeCases_NilStructValue(t *testing.T) {
	// A Value_StructValue where the Struct pointer itself is nil.
	s := &structpb.Struct{
		Fields: map[string]*structpb.Value{
			"obj": {Kind: &structpb.Value_StructValue{StructValue: nil}},
		},
	}
	d, err := StructToStarlark(s, false)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	v, err := d.Attr("obj")
	if err != nil {
		t.Fatal(err)
	}
	inner, ok := v.(*StarlarkDict)
	if !ok {
		t.Fatalf("obj = %T, want *StarlarkDict", v)
	}
	if inner.Len() != 0 {
		t.Errorf("nil StructValue Len = %d, want 0", inner.Len())
	}
}

func TestEdgeCases_FreezeNestedStructAndList(t *testing.T) {
	inner := &structpb.Struct{
		Fields: map[string]*structpb.Value{
			"x": structpb.NewNumberValue(1),
		},
	}
	lv := &structpb.ListValue{
		Values: []*structpb.Value{structpb.NewNumberValue(2)},
	}
	s := &structpb.Struct{
		Fields: map[string]*structpb.Value{
			"obj":     structpb.NewStructValue(inner),
			"entries": structpb.NewListValue(lv),
		},
	}
	d, err := StructToStarlark(s, true)
	if err != nil {
		t.Fatal(err)
	}

	// Top-level frozen
	if err := d.SetField("y", starlark.MakeInt(9)); err == nil {
		t.Error("SetField on frozen top-level should fail")
	}

	// Nested dict frozen
	obj, _ := d.Attr("obj")
	if err := obj.(*StarlarkDict).SetField("z", starlark.MakeInt(9)); err == nil {
		t.Error("SetField on frozen nested dict should fail")
	}

	// Nested list frozen
	entries, _ := d.Attr("entries")
	if err := entries.(*starlark.List).Append(starlark.MakeInt(9)); err == nil {
		t.Error("Append on frozen list should fail")
	}
}

func TestUnsupportedTypes_Tuple(t *testing.T) {
	d := NewStarlarkDict(0)
	_ = d.SetKey(starlark.String("t"), starlark.Tuple{starlark.MakeInt(1), starlark.MakeInt(2)})

	_, err := StarlarkToStruct(d)
	if err == nil {
		t.Fatal("expected error for Tuple")
	}
}

func TestUnsupportedTypes_Set(t *testing.T) {
	d := NewStarlarkDict(0)
	s := new(starlark.Set)
	_ = s.Insert(starlark.MakeInt(1))
	_ = d.SetKey(starlark.String("s"), s)

	_, err := StarlarkToStruct(d)
	if err == nil {
		t.Fatal("expected error for Set")
	}
}

func TestStructToStarlark_NegativeInf(t *testing.T) {
	s := &structpb.Struct{
		Fields: map[string]*structpb.Value{
			"ninf": structpb.NewNumberValue(math.Inf(-1)),
		},
	}
	d, err := StructToStarlark(s, false)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	v, err := d.Attr("ninf")
	if err != nil {
		t.Fatal(err)
	}
	f, ok := v.(starlark.Float)
	if !ok {
		t.Fatalf("-Inf = %T, want starlark.Float", v)
	}
	if !math.IsInf(float64(f), -1) {
		t.Errorf("-Inf = %v, want -Inf", f)
	}
}

func TestStructToStarlark_NegativeZero(t *testing.T) {
	s := &structpb.Struct{
		Fields: map[string]*structpb.Value{
			"nzero": structpb.NewNumberValue(math.Copysign(0, -1)),
		},
	}
	d, err := StructToStarlark(s, false)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	v, err := d.Attr("nzero")
	if err != nil {
		t.Fatal(err)
	}
	// -0.0 is a whole number (Trunc(-0.0) == -0.0) so should be Int(0)
	_, ok := v.(starlark.Int)
	if !ok {
		t.Fatalf("-0 = %T, want starlark.Int", v)
	}
}
