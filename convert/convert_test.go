package convert

import (
	"math"
	"math/big"
	"strings"
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

// ---------------------------------------------------------------------------
// Additional tests from add-tests command
// ---------------------------------------------------------------------------

func TestStructToStarlark_NegativeInteger(t *testing.T) {
	s := &structpb.Struct{
		Fields: map[string]*structpb.Value{
			"offset": structpb.NewNumberValue(-5),
		},
	}
	d, err := StructToStarlark(s, false)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	v, err := d.Attr("offset")
	if err != nil {
		t.Fatal(err)
	}
	si, ok := v.(starlark.Int)
	if !ok {
		t.Fatalf("offset = %T, want starlark.Int", v)
	}
	got, _ := si.Int64()
	if got != -5 {
		t.Errorf("offset = %d, want -5", got)
	}
}

func TestStructToStarlark_Zero(t *testing.T) {
	s := &structpb.Struct{
		Fields: map[string]*structpb.Value{
			"count": structpb.NewNumberValue(0),
		},
	}
	d, err := StructToStarlark(s, false)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	v, err := d.Attr("count")
	if err != nil {
		t.Fatal(err)
	}
	si, ok := v.(starlark.Int)
	if !ok {
		t.Fatalf("0.0 = %T, want starlark.Int", v)
	}
	got, _ := si.Int64()
	if got != 0 {
		t.Errorf("count = %d, want 0", got)
	}
}

func TestStructToStarlark_BoolFalse(t *testing.T) {
	s := &structpb.Struct{
		Fields: map[string]*structpb.Value{
			"disabled": structpb.NewBoolValue(false),
		},
	}
	d, err := StructToStarlark(s, false)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	v, err := d.Attr("disabled")
	if err != nil {
		t.Fatal(err)
	}
	if v != starlark.False {
		t.Errorf("disabled = %v, want False", v)
	}
}

func TestStructToStarlark_EmptyString(t *testing.T) {
	original := &structpb.Struct{
		Fields: map[string]*structpb.Value{
			"tag": structpb.NewStringValue(""),
		},
	}
	d, err := StructToStarlark(original, false)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	v, err := d.Attr("tag")
	if err != nil {
		t.Fatal(err)
	}
	if v != starlark.String("") {
		t.Errorf("tag = %v, want empty string", v)
	}

	// Round-trip
	result, err := StarlarkToStruct(d)
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(original, result, protocmp.Transform()); diff != "" {
		t.Errorf("empty string round-trip mismatch:\n%s", diff)
	}
}

func TestStarlarkToStruct_NonStringKey(t *testing.T) {
	d := NewStarlarkDict(0)
	// Insert an integer key via SetKey (bypasses string-only SetField).
	_ = d.SetKey(starlark.MakeInt(42), starlark.String("val"))

	_, err := StarlarkToStruct(d)
	if err == nil {
		t.Fatal("StarlarkToStruct with non-string key should return error")
	}
	if !strings.Contains(err.Error(), "not a string") {
		t.Errorf("error %q should contain 'not a string'", err.Error())
	}
}

func TestRoundTrip_NaNValue(t *testing.T) {
	d := NewStarlarkDict(0)
	_ = d.SetField("nan", starlark.Float(math.NaN()))

	s, err := StarlarkToStruct(d)
	if err != nil {
		t.Fatalf("StarlarkToStruct: %v", err)
	}
	nv := s.GetFields()["nan"].GetNumberValue()
	if !math.IsNaN(nv) {
		t.Errorf("NaN reverse path = %v, want NaN", nv)
	}
}

func TestRoundTrip_InfValues(t *testing.T) {
	d := NewStarlarkDict(0)
	_ = d.SetField("pos_inf", starlark.Float(math.Inf(1)))
	_ = d.SetField("neg_inf", starlark.Float(math.Inf(-1)))

	s, err := StarlarkToStruct(d)
	if err != nil {
		t.Fatalf("StarlarkToStruct: %v", err)
	}

	posInf := s.GetFields()["pos_inf"].GetNumberValue()
	if !math.IsInf(posInf, 1) {
		t.Errorf("+Inf reverse = %v, want +Inf", posInf)
	}

	negInf := s.GetFields()["neg_inf"].GetNumberValue()
	if !math.IsInf(negInf, -1) {
		t.Errorf("-Inf reverse = %v, want -Inf", negInf)
	}
}

// ---------------------------------------------------------------------------
// PlainDictToStruct tests
// ---------------------------------------------------------------------------

func TestPlainDictToStruct_Empty(t *testing.T) {
	d := new(starlark.Dict)
	s, err := PlainDictToStruct(d)
	if err != nil {
		t.Fatalf("PlainDictToStruct(empty) error: %v", err)
	}
	if len(s.GetFields()) != 0 {
		t.Errorf("fields = %d, want 0", len(s.GetFields()))
	}
}

func TestPlainDictToStruct_FlatDict(t *testing.T) {
	d := new(starlark.Dict)
	_ = d.SetKey(starlark.String("name"), starlark.String("web"))
	_ = d.SetKey(starlark.String("replicas"), starlark.MakeInt(3))
	_ = d.SetKey(starlark.String("enabled"), starlark.True)
	_ = d.SetKey(starlark.String("empty"), starlark.None)

	s, err := PlainDictToStruct(d)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	fields := s.GetFields()
	if len(fields) != 4 {
		t.Errorf("fields = %d, want 4", len(fields))
	}
	// Check string
	if sv, ok := fields["name"].GetKind().(*structpb.Value_StringValue); !ok || sv.StringValue != "web" {
		t.Errorf("name = %v, want 'web'", fields["name"])
	}
	// Check int (stored as number)
	if nv, ok := fields["replicas"].GetKind().(*structpb.Value_NumberValue); !ok || nv.NumberValue != 3.0 {
		t.Errorf("replicas = %v, want 3.0", fields["replicas"])
	}
	// Check bool
	if bv, ok := fields["enabled"].GetKind().(*structpb.Value_BoolValue); !ok || !bv.BoolValue {
		t.Errorf("enabled = %v, want true", fields["enabled"])
	}
	// Check None -> null
	if _, ok := fields["empty"].GetKind().(*structpb.Value_NullValue); !ok {
		t.Errorf("empty = %v, want null", fields["empty"])
	}
}

func TestPlainDictToStruct_NestedDict(t *testing.T) {
	// 3 levels: {"metadata": {"labels": {"app": "web"}}}
	inner := new(starlark.Dict)
	_ = inner.SetKey(starlark.String("app"), starlark.String("web"))

	mid := new(starlark.Dict)
	_ = mid.SetKey(starlark.String("labels"), inner)

	outer := new(starlark.Dict)
	_ = outer.SetKey(starlark.String("metadata"), mid)

	s, err := PlainDictToStruct(outer)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	// Navigate to inner
	metaVal, ok := s.GetFields()["metadata"].GetKind().(*structpb.Value_StructValue)
	if !ok {
		t.Fatalf("metadata kind = %T, want StructValue", s.GetFields()["metadata"].GetKind())
	}
	labelsVal, ok := metaVal.StructValue.GetFields()["labels"].GetKind().(*structpb.Value_StructValue)
	if !ok {
		t.Fatalf("labels kind = %T, want StructValue", metaVal.StructValue.GetFields()["labels"].GetKind())
	}
	appVal, ok := labelsVal.StructValue.GetFields()["app"].GetKind().(*structpb.Value_StringValue)
	if !ok {
		t.Fatalf("app kind = %T, want StringValue", labelsVal.StructValue.GetFields()["app"].GetKind())
	}
	if appVal.StringValue != "web" {
		t.Errorf("app = %q, want 'web'", appVal.StringValue)
	}
}

func TestPlainDictToStruct_NonStringKey(t *testing.T) {
	d := new(starlark.Dict)
	_ = d.SetKey(starlark.MakeInt(42), starlark.String("val"))

	_, err := PlainDictToStruct(d)
	if err == nil {
		t.Fatal("PlainDictToStruct with non-string key should return error")
	}
	if !strings.Contains(err.Error(), "not a string") {
		t.Errorf("error %q should contain 'not a string'", err.Error())
	}
}

func TestPlainDictToStruct_WithList(t *testing.T) {
	list := starlark.NewList([]starlark.Value{
		starlark.MakeInt(1),
		starlark.MakeInt(2),
		starlark.MakeInt(3),
	})
	d := new(starlark.Dict)
	_ = d.SetKey(starlark.String("items"), list)

	s, err := PlainDictToStruct(d)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	lv, ok := s.GetFields()["items"].GetKind().(*structpb.Value_ListValue)
	if !ok {
		t.Fatalf("items kind = %T, want ListValue", s.GetFields()["items"].GetKind())
	}
	if len(lv.ListValue.GetValues()) != 3 {
		t.Errorf("items len = %d, want 3", len(lv.ListValue.GetValues()))
	}
}

func TestStarlarkToProtoValue_PlainDict(t *testing.T) {
	d := new(starlark.Dict)
	_ = d.SetKey(starlark.String("key"), starlark.String("value"))

	pv, err := starlarkToProtoValue(d)
	if err != nil {
		t.Fatalf("starlarkToProtoValue(*starlark.Dict) error: %v", err)
	}
	sv, ok := pv.GetKind().(*structpb.Value_StructValue)
	if !ok {
		t.Fatalf("kind = %T, want StructValue", pv.GetKind())
	}
	if sv.StructValue.GetFields()["key"].GetStringValue() != "value" {
		t.Errorf("key = %q, want 'value'", sv.StructValue.GetFields()["key"].GetStringValue())
	}
}

func TestPlainDictToStruct_RoundTrip(t *testing.T) {
	// plain dict -> PlainDictToStruct -> StructToStarlark produces equivalent data
	d := new(starlark.Dict)
	_ = d.SetKey(starlark.String("apiVersion"), starlark.String("v1"))
	_ = d.SetKey(starlark.String("replicas"), starlark.MakeInt(3))
	_ = d.SetKey(starlark.String("enabled"), starlark.True)

	s, err := PlainDictToStruct(d)
	if err != nil {
		t.Fatalf("PlainDictToStruct error: %v", err)
	}

	sd, err := StructToStarlark(s, false)
	if err != nil {
		t.Fatalf("StructToStarlark error: %v", err)
	}

	// Verify values are equivalent
	apiVal, err := sd.Attr("apiVersion")
	if err != nil {
		t.Fatal(err)
	}
	if apiVal != starlark.String("v1") {
		t.Errorf("apiVersion = %v, want 'v1'", apiVal)
	}

	replVal, err := sd.Attr("replicas")
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

	enabledVal, err := sd.Attr("enabled")
	if err != nil {
		t.Fatal(err)
	}
	if enabledVal != starlark.True {
		t.Errorf("enabled = %v, want True", enabledVal)
	}
}

func TestStructToStarlark_FrozenNilStruct(t *testing.T) {
	d, err := StructToStarlark(nil, true)
	if err != nil {
		t.Fatalf("StructToStarlark(nil, true): %v", err)
	}
	if d == nil {
		t.Fatal("returned nil")
	}
	if d.Len() != 0 {
		t.Errorf("Len() = %d, want 0", d.Len())
	}
	// Should be frozen.
	if err := d.SetField("x", starlark.MakeInt(1)); err == nil {
		t.Error("SetField on frozen nil-struct dict should return error")
	}
}

func TestPlainDictToStruct_UnsupportedNestedType(t *testing.T) {
	d := new(starlark.Dict)
	_ = d.SetKey(starlark.String("bad"), starlark.Tuple{starlark.MakeInt(1), starlark.MakeInt(2)})

	_, err := PlainDictToStruct(d)
	if err == nil {
		t.Fatal("PlainDictToStruct with Tuple value should return error")
	}
	if !strings.Contains(err.Error(), "unsupported starlark type") {
		t.Errorf("error %q should contain 'unsupported starlark type'", err.Error())
	}
}

// ---------------------------------------------------------------------------
// ProtoValueToPlainStarlark tests
// ---------------------------------------------------------------------------

func TestProtoValueToPlainStarlark_WholeNumber(t *testing.T) {
	v, err := ProtoValueToPlainStarlark(structpb.NewNumberValue(42.0), false)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	si, ok := v.(starlark.Int)
	if !ok {
		t.Fatalf("got %T, want starlark.Int", v)
	}
	got, _ := si.Int64()
	if got != 42 {
		t.Errorf("got %d, want 42", got)
	}
}

func TestProtoValueToPlainStarlark_Float(t *testing.T) {
	v, err := ProtoValueToPlainStarlark(structpb.NewNumberValue(3.14), false)
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

func TestProtoValueToPlainStarlark_BeyondMaxSafeInt(t *testing.T) {
	// 2^54 = 18014398509481984 -- beyond 2^53 safe integer range.
	// This is a whole number in float64, but converting to int64 and back
	// would lose precision for nearby odd values, so we keep it as Float.
	bigVal := float64(1 << 54)
	v, err := ProtoValueToPlainStarlark(structpb.NewNumberValue(bigVal), false)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	f, ok := v.(starlark.Float)
	if !ok {
		t.Fatalf("2^54 = %T, want starlark.Float (beyond maxSafeInt guard)", v)
	}
	if float64(f) != bigVal {
		t.Errorf("got %v, want %v", f, bigVal)
	}

	// Also test negative beyond maxSafeInt.
	negBigVal := -float64(1 << 54)
	v2, err := ProtoValueToPlainStarlark(structpb.NewNumberValue(negBigVal), false)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	f2, ok := v2.(starlark.Float)
	if !ok {
		t.Fatalf("-2^54 = %T, want starlark.Float", v2)
	}
	if float64(f2) != negBigVal {
		t.Errorf("got %v, want %v", f2, negBigVal)
	}
}

func TestProtoValueToPlainStarlark_StructProducesPlainDict(t *testing.T) {
	sv := structpb.NewStructValue(&structpb.Struct{
		Fields: map[string]*structpb.Value{
			"key": structpb.NewStringValue("val"),
		},
	})
	v, err := ProtoValueToPlainStarlark(sv, false)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	// Must be plain *starlark.Dict, NOT *StarlarkDict
	d, ok := v.(*starlark.Dict)
	if !ok {
		t.Fatalf("got %T, want *starlark.Dict (plain)", v)
	}
	val, found, err := d.Get(starlark.String("key"))
	if err != nil || !found {
		t.Fatalf("key not found: found=%v, err=%v", found, err)
	}
	if val != starlark.String("val") {
		t.Errorf("key = %v, want 'val'", val)
	}
}

func TestProtoValueToPlainStarlark_NestedStructWithList(t *testing.T) {
	inner := &structpb.Struct{
		Fields: map[string]*structpb.Value{
			"items": structpb.NewListValue(&structpb.ListValue{
				Values: []*structpb.Value{
					structpb.NewNumberValue(1),
					structpb.NewStringValue("two"),
				},
			}),
		},
	}
	outer := structpb.NewStructValue(&structpb.Struct{
		Fields: map[string]*structpb.Value{
			"nested": structpb.NewStructValue(inner),
		},
	})

	v, err := ProtoValueToPlainStarlark(outer, false)
	if err != nil {
		t.Fatalf("error: %v", err)
	}

	outerDict, ok := v.(*starlark.Dict)
	if !ok {
		t.Fatalf("outer = %T, want *starlark.Dict", v)
	}

	nestedVal, found, _ := outerDict.Get(starlark.String("nested"))
	if !found {
		t.Fatal("nested key not found")
	}
	nestedDict, ok := nestedVal.(*starlark.Dict)
	if !ok {
		t.Fatalf("nested = %T, want *starlark.Dict", nestedVal)
	}

	itemsVal, found, _ := nestedDict.Get(starlark.String("items"))
	if !found {
		t.Fatal("items key not found")
	}
	list, ok := itemsVal.(*starlark.List)
	if !ok {
		t.Fatalf("items = %T, want *starlark.List", itemsVal)
	}
	if list.Len() != 2 {
		t.Errorf("items len = %d, want 2", list.Len())
	}
	if _, ok := list.Index(0).(starlark.Int); !ok {
		t.Errorf("items[0] = %T, want starlark.Int", list.Index(0))
	}
	if _, ok := list.Index(1).(starlark.String); !ok {
		t.Errorf("items[1] = %T, want starlark.String", list.Index(1))
	}
}

func TestProtoValueToPlainStarlark_FrozenDict(t *testing.T) {
	sv := structpb.NewStructValue(&structpb.Struct{
		Fields: map[string]*structpb.Value{
			"x": structpb.NewNumberValue(1),
		},
	})
	v, err := ProtoValueToPlainStarlark(sv, true)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	d, ok := v.(*starlark.Dict)
	if !ok {
		t.Fatalf("got %T, want *starlark.Dict", v)
	}
	// Should be frozen -- mutation must fail.
	if err := d.SetKey(starlark.String("y"), starlark.MakeInt(2)); err == nil {
		t.Error("SetKey on frozen dict should return error")
	}
}

func TestPlainDictToStruct_MixedNestedTypes(t *testing.T) {
	// Plain dict with all supported types: string, int, bool, None, nested dict, list.
	inner := new(starlark.Dict)
	_ = inner.SetKey(starlark.String("nested"), starlark.String("value"))

	list := starlark.NewList([]starlark.Value{
		starlark.MakeInt(1),
		starlark.String("two"),
	})

	d := new(starlark.Dict)
	_ = d.SetKey(starlark.String("str"), starlark.String("hello"))
	_ = d.SetKey(starlark.String("num"), starlark.MakeInt(42))
	_ = d.SetKey(starlark.String("flag"), starlark.True)
	_ = d.SetKey(starlark.String("nil"), starlark.None)
	_ = d.SetKey(starlark.String("obj"), inner)
	_ = d.SetKey(starlark.String("arr"), list)

	s, err := PlainDictToStruct(d)
	if err != nil {
		t.Fatalf("PlainDictToStruct error: %v", err)
	}
	fields := s.GetFields()
	if len(fields) != 6 {
		t.Errorf("fields = %d, want 6", len(fields))
	}

	// Spot-check each type
	if fields["str"].GetStringValue() != "hello" {
		t.Errorf("str = %v, want 'hello'", fields["str"])
	}
	if fields["num"].GetNumberValue() != 42 {
		t.Errorf("num = %v, want 42", fields["num"])
	}
	if !fields["flag"].GetBoolValue() {
		t.Errorf("flag = %v, want true", fields["flag"])
	}
	if _, ok := fields["nil"].GetKind().(*structpb.Value_NullValue); !ok {
		t.Errorf("nil kind = %T, want NullValue", fields["nil"].GetKind())
	}
	if _, ok := fields["obj"].GetKind().(*structpb.Value_StructValue); !ok {
		t.Errorf("obj kind = %T, want StructValue", fields["obj"].GetKind())
	}
	if lv, ok := fields["arr"].GetKind().(*structpb.Value_ListValue); !ok {
		t.Errorf("arr kind = %T, want ListValue", fields["arr"].GetKind())
	} else if len(lv.ListValue.GetValues()) != 2 {
		t.Errorf("arr len = %d, want 2", len(lv.ListValue.GetValues()))
	}
}

// ---------------------------------------------------------------------------
// ProtoValueToPlainStarlark edge case tests (Phase 7 add-tests)
// ---------------------------------------------------------------------------

func TestProtoValueToPlainStarlark_NilInput(t *testing.T) {
	v, err := ProtoValueToPlainStarlark(nil, false)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if v != starlark.None {
		t.Errorf("nil input = %v (%T), want starlark.None", v, v)
	}
}

func TestProtoValueToPlainStarlark_NullValue(t *testing.T) {
	v, err := ProtoValueToPlainStarlark(structpb.NewNullValue(), false)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if v != starlark.None {
		t.Errorf("NullValue = %v (%T), want starlark.None", v, v)
	}
}

func TestProtoValueToPlainStarlark_Bool(t *testing.T) {
	v, err := ProtoValueToPlainStarlark(structpb.NewBoolValue(true), false)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if v != starlark.True {
		t.Errorf("true = %v, want starlark.True", v)
	}

	v2, err := ProtoValueToPlainStarlark(structpb.NewBoolValue(false), false)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if v2 != starlark.False {
		t.Errorf("false = %v, want starlark.False", v2)
	}
}

func TestProtoValueToPlainStarlark_String(t *testing.T) {
	v, err := ProtoValueToPlainStarlark(structpb.NewStringValue("hello"), false)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if v != starlark.String("hello") {
		t.Errorf("string = %v, want 'hello'", v)
	}
}

func TestProtoValueToPlainStarlark_ExactMaxSafeInt(t *testing.T) {
	// 2^53 = 9007199254740992 is the largest safe integer.
	// The guard uses strict > so exactly 2^53 should remain Int.
	val := float64(1 << 53)
	v, err := ProtoValueToPlainStarlark(structpb.NewNumberValue(val), false)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	si, ok := v.(starlark.Int)
	if !ok {
		t.Fatalf("exact 2^53 = %T, want starlark.Int (at boundary, not beyond)", v)
	}
	got, _ := si.Int64()
	if got != 9007199254740992 {
		t.Errorf("got %d, want 9007199254740992", got)
	}

	// Same for negative boundary.
	negVal := -float64(1 << 53)
	v2, err := ProtoValueToPlainStarlark(structpb.NewNumberValue(negVal), false)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	si2, ok := v2.(starlark.Int)
	if !ok {
		t.Fatalf("exact -2^53 = %T, want starlark.Int", v2)
	}
	got2, _ := si2.Int64()
	if got2 != -9007199254740992 {
		t.Errorf("got %d, want -9007199254740992", got2)
	}
}

func TestProtoValueToPlainStarlark_NilStructValue(t *testing.T) {
	// StructValue wrapper with nil inner Struct should produce empty plain dict.
	v, err := ProtoValueToPlainStarlark(
		&structpb.Value{Kind: &structpb.Value_StructValue{StructValue: nil}}, false)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	d, ok := v.(*starlark.Dict)
	if !ok {
		t.Fatalf("nil StructValue = %T, want *starlark.Dict", v)
	}
	if d.Len() != 0 {
		t.Errorf("Len = %d, want 0", d.Len())
	}
}

func TestProtoValueToPlainStarlark_NilListValue(t *testing.T) {
	// ListValue wrapper with nil inner ListValue should produce empty list.
	v, err := ProtoValueToPlainStarlark(
		&structpb.Value{Kind: &structpb.Value_ListValue{ListValue: nil}}, false)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	list, ok := v.(*starlark.List)
	if !ok {
		t.Fatalf("nil ListValue = %T, want *starlark.List", v)
	}
	if list.Len() != 0 {
		t.Errorf("Len = %d, want 0", list.Len())
	}
}

func TestConvertNumber_ExceedsInt64Range(t *testing.T) {
	tests := []struct {
		name    string
		input   float64
		wantErr string        // non-empty means expect error containing this
		wantVal starlark.Value // expected value when no error
	}{
		{
			name:    "exceeds +int64 range",
			input:   1e19,
			wantErr: "exceeds int64 range",
		},
		{
			name:    "exceeds -int64 range",
			input:   -1e19,
			wantErr: "exceeds int64 range",
		},
		{
			name:    "imprecise zone returns Float",
			input:   float64(1<<53 + 1),
			wantVal: starlark.Float(float64(1<<53 + 1)),
		},
		{
			name:    "normal integer",
			input:   3.0,
			wantVal: starlark.MakeInt64(3),
		},
		{
			name:    "normal float",
			input:   3.14,
			wantVal: starlark.Float(3.14),
		},
		{
			name:    "+Inf returns Float",
			input:   math.Inf(1),
			wantVal: starlark.Float(math.Inf(1)),
		},
		{
			name:    "NaN returns Float",
			input:   math.NaN(),
			wantVal: starlark.Float(math.NaN()),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := convertNumber(tt.input)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("convertNumber(%g) = %v, want error containing %q", tt.input, got, tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("convertNumber(%g) error = %q, want error containing %q", tt.input, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("convertNumber(%g) unexpected error: %v", tt.input, err)
			}
			// Special handling for NaN comparison (NaN != NaN).
			if math.IsNaN(tt.input) {
				f, ok := got.(starlark.Float)
				if !ok {
					t.Fatalf("convertNumber(NaN) = %T, want starlark.Float", got)
				}
				if !math.IsNaN(float64(f)) {
					t.Fatalf("convertNumber(NaN) = %v, want NaN", got)
				}
				return
			}
			if got != tt.wantVal {
				t.Errorf("convertNumber(%g) = %v (%T), want %v (%T)", tt.input, got, got, tt.wantVal, tt.wantVal)
			}
		})
	}
}

func TestStructToStarlark_OverflowError(t *testing.T) {
	s := &structpb.Struct{
		Fields: map[string]*structpb.Value{
			"big": structpb.NewNumberValue(1e19),
		},
	}
	_, err := StructToStarlark(s, false)
	if err == nil {
		t.Fatal("StructToStarlark with 1e19 should return error")
	}
	if !strings.Contains(err.Error(), "exceeds int64 range") {
		t.Errorf("error = %q, want error containing 'exceeds int64 range'", err)
	}
}

func TestProtoValueToPlainStarlark_OverflowError(t *testing.T) {
	_, err := ProtoValueToPlainStarlark(structpb.NewNumberValue(1e19), false)
	if err == nil {
		t.Fatal("ProtoValueToPlainStarlark with 1e19 should return error")
	}
	if !strings.Contains(err.Error(), "exceeds int64 range") {
		t.Errorf("error = %q, want error containing 'exceeds int64 range'", err)
	}
}

func TestProtoValueToPlainStarlark_FrozenNestedList(t *testing.T) {
	// A struct containing a list, both frozen.
	sv := structpb.NewStructValue(&structpb.Struct{
		Fields: map[string]*structpb.Value{
			"items": structpb.NewListValue(&structpb.ListValue{
				Values: []*structpb.Value{structpb.NewNumberValue(1)},
			}),
		},
	})
	v, err := ProtoValueToPlainStarlark(sv, true)
	if err != nil {
		t.Fatalf("error: %v", err)
	}

	d, ok := v.(*starlark.Dict)
	if !ok {
		t.Fatalf("got %T, want *starlark.Dict", v)
	}
	// Dict should be frozen.
	if err := d.SetKey(starlark.String("y"), starlark.MakeInt(2)); err == nil {
		t.Error("SetKey on frozen dict should fail")
	}

	// List inside should also be frozen.
	itemsVal, _, _ := d.Get(starlark.String("items"))
	list, ok := itemsVal.(*starlark.List)
	if !ok {
		t.Fatalf("items = %T, want *starlark.List", itemsVal)
	}
	if err := list.Append(starlark.MakeInt(2)); err == nil {
		t.Error("Append on frozen list should fail")
	}
}
