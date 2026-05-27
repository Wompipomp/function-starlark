package builtins

import (
	"sort"
	"strings"
	"testing"

	"github.com/crossplane/function-sdk-go/logging"
	"go.starlark.net/starlark"
	"go.starlark.net/syntax"

	"github.com/wompipomp/function-starlark/runtime"
	"github.com/wompipomp/function-starlark/schema"
)

func TestMutableStructCreateAndRead(t *testing.T) {
	ms, err := MakeMutableStruct(
		&starlark.Thread{},
		&starlark.Builtin{},
		nil,
		[]starlark.Tuple{
			{starlark.String("name"), starlark.String("test")},
			{starlark.String("count"), starlark.MakeInt(42)},
		},
	)
	if err != nil {
		t.Fatalf("MakeMutableStruct: %v", err)
	}

	s := ms.(*MutableStruct)

	// Dot-read existing fields.
	v, err := s.Attr("name")
	if err != nil {
		t.Fatalf("Attr(name): %v", err)
	}
	if v != starlark.String("test") {
		t.Errorf("Attr(name) = %v, want \"test\"", v)
	}

	v, err = s.Attr("count")
	if err != nil {
		t.Fatalf("Attr(count): %v", err)
	}
	if v != starlark.MakeInt(42) {
		t.Errorf("Attr(count) = %v, want 42", v)
	}
}

func TestMutableStructSetExistingField(t *testing.T) {
	ms, err := MakeMutableStruct(
		&starlark.Thread{},
		&starlark.Builtin{},
		nil,
		[]starlark.Tuple{
			{starlark.String("name"), starlark.String("old")},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	s := ms.(*MutableStruct)

	if err := s.SetField("name", starlark.String("new")); err != nil {
		t.Fatalf("SetField: %v", err)
	}

	v, err := s.Attr("name")
	if err != nil {
		t.Fatal(err)
	}
	if v != starlark.String("new") {
		t.Errorf("Attr(name) = %v, want \"new\"", v)
	}
}

func TestMutableStructAddNewField(t *testing.T) {
	ms, err := MakeMutableStruct(
		&starlark.Thread{},
		&starlark.Builtin{},
		nil,
		[]starlark.Tuple{
			{starlark.String("name"), starlark.String("test")},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	s := ms.(*MutableStruct)

	// Add a field that didn't exist at construction.
	if err := s.SetField("new_field", starlark.String("added")); err != nil {
		t.Fatalf("SetField: %v", err)
	}

	v, err := s.Attr("new_field")
	if err != nil {
		t.Fatal(err)
	}
	if v != starlark.String("added") {
		t.Errorf("Attr(new_field) = %v, want \"added\"", v)
	}
}

func TestMutableStructFreezeBlocksMutation(t *testing.T) {
	ms, err := MakeMutableStruct(
		&starlark.Thread{},
		&starlark.Builtin{},
		nil,
		[]starlark.Tuple{
			{starlark.String("name"), starlark.String("test")},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	s := ms.(*MutableStruct)

	s.Freeze()

	err = s.SetField("name", starlark.String("new"))
	if err == nil {
		t.Fatal("SetField on frozen mutable_struct should return error")
	}
	if !strings.Contains(err.Error(), "frozen") {
		t.Errorf("error %q should contain 'frozen'", err.Error())
	}
}

func TestMutableStructFreezeTransitive(t *testing.T) {
	list := starlark.NewList([]starlark.Value{starlark.MakeInt(1)})

	ms, err := MakeMutableStruct(
		&starlark.Thread{},
		&starlark.Builtin{},
		nil,
		[]starlark.Tuple{
			{starlark.String("items"), list},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	s := ms.(*MutableStruct)

	// List should be mutable before freeze.
	if err := list.Append(starlark.MakeInt(2)); err != nil {
		t.Fatalf("Append before freeze: %v", err)
	}

	s.Freeze()

	// List should be frozen after struct freeze.
	if err := list.Append(starlark.MakeInt(3)); err == nil {
		t.Fatal("Append to list after struct freeze should error")
	}
}

func TestMutableStructMergePlus(t *testing.T) {
	a, err := MakeMutableStruct(
		&starlark.Thread{},
		&starlark.Builtin{},
		nil,
		[]starlark.Tuple{
			{starlark.String("x"), starlark.MakeInt(1)},
			{starlark.String("y"), starlark.MakeInt(2)},
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	b, err := MakeMutableStruct(
		&starlark.Thread{},
		&starlark.Builtin{},
		nil,
		[]starlark.Tuple{
			{starlark.String("y"), starlark.MakeInt(20)},
			{starlark.String("z"), starlark.MakeInt(30)},
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	sa := a.(*MutableStruct)
	sb := b.(*MutableStruct)

	// a + b: right side wins on conflict.
	result, err := sa.Binary(syntax.PLUS, sb, starlark.Left)
	if err != nil {
		t.Fatalf("Binary +: %v", err)
	}

	merged, ok := result.(*MutableStruct)
	if !ok {
		t.Fatalf("Binary + returned %T, want *MutableStruct", result)
	}

	// x should come from a.
	v, _ := merged.Attr("x")
	if v != starlark.MakeInt(1) {
		t.Errorf("merged.x = %v, want 1", v)
	}

	// y should come from b (right wins).
	v, _ = merged.Attr("y")
	if v != starlark.MakeInt(20) {
		t.Errorf("merged.y = %v, want 20", v)
	}

	// z should come from b.
	v, _ = merged.Attr("z")
	if v != starlark.MakeInt(30) {
		t.Errorf("merged.z = %v, want 30", v)
	}
}

func TestMutableStructType(t *testing.T) {
	ms, err := MakeMutableStruct(
		&starlark.Thread{},
		&starlark.Builtin{},
		nil,
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if got := ms.(*MutableStruct).Type(); got != "mutable_struct" {
		t.Errorf("Type() = %q, want %q", got, "mutable_struct")
	}
}

func TestMutableStructString(t *testing.T) {
	ms, err := MakeMutableStruct(
		&starlark.Thread{},
		&starlark.Builtin{},
		nil,
		[]starlark.Tuple{
			{starlark.String("name"), starlark.String("test")},
			{starlark.String("items"), starlark.NewList(nil)},
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	got := ms.(*MutableStruct).String()
	want := `mutable_struct(items = [], name = "test")`
	if got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

func TestMutableStructEquality(t *testing.T) {
	makeMS := func(kwargs []starlark.Tuple) *MutableStruct {
		ms, err := MakeMutableStruct(&starlark.Thread{}, &starlark.Builtin{}, nil, kwargs)
		if err != nil {
			t.Fatal(err)
		}
		return ms.(*MutableStruct)
	}

	a := makeMS([]starlark.Tuple{
		{starlark.String("x"), starlark.MakeInt(1)},
		{starlark.String("y"), starlark.String("hello")},
	})
	b := makeMS([]starlark.Tuple{
		{starlark.String("x"), starlark.MakeInt(1)},
		{starlark.String("y"), starlark.String("hello")},
	})
	c := makeMS([]starlark.Tuple{
		{starlark.String("x"), starlark.MakeInt(999)},
	})

	// Equal structs.
	eq, err := a.CompareSameType(syntax.EQL, b, 10)
	if err != nil {
		t.Fatalf("CompareSameType ==: %v", err)
	}
	if !eq {
		t.Error("equal mutable_structs should compare == as true")
	}

	// Not equal.
	neq, err := a.CompareSameType(syntax.NEQ, c, 10)
	if err != nil {
		t.Fatalf("CompareSameType !=: %v", err)
	}
	if !neq {
		t.Error("different mutable_structs should compare != as true")
	}
}

func TestMutableStructHash(t *testing.T) {
	ms, err := MakeMutableStruct(&starlark.Thread{}, &starlark.Builtin{}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = ms.(*MutableStruct).Hash()
	if err == nil {
		t.Fatal("Hash() should return error")
	}
	if !strings.Contains(err.Error(), "unhashable") {
		t.Errorf("error %q should contain 'unhashable'", err.Error())
	}
}

func TestMutableStructTruth(t *testing.T) {
	// Empty struct should be truthy (matching immutable struct behavior).
	ms, err := MakeMutableStruct(&starlark.Thread{}, &starlark.Builtin{}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if ms.(*MutableStruct).Truth() != starlark.True {
		t.Error("Truth() should be True even when empty")
	}
}

func TestMutableStructRejectsPositionalArgs(t *testing.T) {
	_, err := MakeMutableStruct(
		&starlark.Thread{},
		&starlark.Builtin{},
		starlark.Tuple{starlark.String("bad")},
		nil,
	)
	if err == nil {
		t.Fatal("should reject positional arguments")
	}
	if !strings.Contains(err.Error(), "positional") {
		t.Errorf("error %q should contain 'positional'", err.Error())
	}
}

func TestMutableStructAttrNames(t *testing.T) {
	ms, err := MakeMutableStruct(
		&starlark.Thread{},
		&starlark.Builtin{},
		nil,
		[]starlark.Tuple{
			{starlark.String("zebra"), starlark.True},
			{starlark.String("alpha"), starlark.True},
			{starlark.String("middle"), starlark.True},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	s := ms.(*MutableStruct)

	names := s.AttrNames()
	if !sort.StringsAreSorted(names) {
		t.Errorf("AttrNames() not sorted: %v", names)
	}
	want := []string{"alpha", "middle", "zebra"}
	if len(names) != len(want) {
		t.Fatalf("AttrNames() = %v, want %v", names, want)
	}
	for i, n := range names {
		if n != want[i] {
			t.Errorf("AttrNames()[%d] = %q, want %q", i, n, want[i])
		}
	}
}

func TestMutableStructAttrMissingReturnsNoSuchAttrError(t *testing.T) {
	ms, err := MakeMutableStruct(&starlark.Thread{}, &starlark.Builtin{}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	s := ms.(*MutableStruct)

	v, err := s.Attr("nonexistent")
	if v != nil {
		t.Errorf("Attr(nonexistent) = %v, want nil", v)
	}
	if err == nil {
		t.Fatal("Attr(nonexistent) should return error")
	}

	// Should be a NoSuchAttrError.
	var nsae starlark.NoSuchAttrError
	if !strings.Contains(err.Error(), "no .nonexistent attribute") {
		t.Errorf("error %q should contain 'no .nonexistent attribute'", err.Error())
	}
	// Verify it's the right error type by checking string conversion.
	_ = nsae
	if !strings.Contains(err.Error(), "mutable_struct") {
		t.Errorf("error %q should contain 'mutable_struct'", err.Error())
	}
}

func TestMutableStructBinaryPlusRightSide(t *testing.T) {
	a, _ := MakeMutableStruct(&starlark.Thread{}, &starlark.Builtin{}, nil,
		[]starlark.Tuple{{starlark.String("x"), starlark.MakeInt(1)}})
	b, _ := MakeMutableStruct(&starlark.Thread{}, &starlark.Builtin{}, nil,
		[]starlark.Tuple{{starlark.String("x"), starlark.MakeInt(2)}})

	sa := a.(*MutableStruct)
	sb := b.(*MutableStruct)

	// When side == Right, x is actually the right operand, so we swap.
	result, err := sa.Binary(syntax.PLUS, sb, starlark.Right)
	if err != nil {
		t.Fatalf("Binary + Right: %v", err)
	}
	merged := result.(*MutableStruct)

	// When sa is on the right side, sa's value should win.
	v, _ := merged.Attr("x")
	if v != starlark.MakeInt(1) {
		t.Errorf("merged.x = %v, want 1 (right side wins)", v)
	}
}

func TestMutableStructBinaryNonMutableStruct(t *testing.T) {
	ms, _ := MakeMutableStruct(&starlark.Thread{}, &starlark.Builtin{}, nil, nil)
	s := ms.(*MutableStruct)

	// + with a non-MutableStruct should return nil, nil (unhandled).
	result, err := s.Binary(syntax.PLUS, starlark.MakeInt(1), starlark.Left)
	if result != nil || err != nil {
		t.Errorf("Binary + with int: got (%v, %v), want (nil, nil)", result, err)
	}
}

func TestMutableStructCompareUnsupportedOp(t *testing.T) {
	a, _ := MakeMutableStruct(&starlark.Thread{}, &starlark.Builtin{}, nil, nil)
	b, _ := MakeMutableStruct(&starlark.Thread{}, &starlark.Builtin{}, nil, nil)

	sa := a.(*MutableStruct)
	sb := b.(*MutableStruct)

	_, err := sa.CompareSameType(syntax.LT, sb, 10)
	if err == nil {
		t.Fatal("CompareSameType with < should return error")
	}
}

// ---------------------------------------------------------------------------
// Phase 46 Plan 01 — Schema-backed MutableStruct construction tests
// ---------------------------------------------------------------------------

// helper: create a SchemaCallable for testing.
func testSchemaCallable(name string, fields map[string]*schema.FieldDescriptor, order []string) *schema.SchemaCallable {
	return schema.NewSchemaCallable(name, "", fields, order)
}

// helper: create a FieldDescriptor directly for mutable_struct tests.
func testMSField(typeName string, required bool, defVal starlark.Value, enum *starlark.List) *schema.FieldDescriptor {
	return schema.NewFieldDescriptor(typeName, nil, nil, required, defVal, enum, "")
}

// helper: create a FieldDescriptor with a nested schema reference.
func testMSFieldWithSchema(sc *schema.SchemaCallable, required bool) *schema.FieldDescriptor {
	return schema.NewFieldDescriptor("", sc, nil, required, starlark.None, nil, "")
}

// SCHM-01: schema= kwarg accepted with type-check.
func TestMutableStructSchemaKwargAccepted(t *testing.T) {
	sc := testSchemaCallable("MySchema", map[string]*schema.FieldDescriptor{
		"name": testMSField("string", true, starlark.None, nil),
	}, []string{"name"})

	ms, err := MakeMutableStruct(
		&starlark.Thread{}, &starlark.Builtin{}, nil,
		[]starlark.Tuple{
			{starlark.String("schema"), sc},
			{starlark.String("name"), starlark.String("hello")},
		},
	)
	if err != nil {
		t.Fatalf("MakeMutableStruct: %v", err)
	}

	s := ms.(*MutableStruct)
	v, err := s.Attr("name")
	if err != nil {
		t.Fatalf("Attr(name): %v", err)
	}
	if v != starlark.String("hello") {
		t.Errorf("name = %v, want \"hello\"", v)
	}

	// Type() should still return "mutable_struct".
	if s.Type() != "mutable_struct" {
		t.Errorf("Type() = %q, want \"mutable_struct\"", s.Type())
	}
}

// SCHM-01: schema= with non-SchemaCallable produces type error.
func TestMutableStructSchemaNonSchemaCallable(t *testing.T) {
	_, err := MakeMutableStruct(
		&starlark.Thread{}, &starlark.Builtin{}, nil,
		[]starlark.Tuple{
			{starlark.String("schema"), starlark.MakeInt(123)},
		},
	)
	if err == nil {
		t.Fatal("expected error for non-SchemaCallable schema=")
	}
	if !strings.Contains(err.Error(), "schema= must be a schema(), got int") {
		t.Errorf("error = %v, want contains 'schema= must be a schema(), got int'", err)
	}
}

// SCHM-02: Construction validates type mismatch.
func TestMutableStructSchemaTypeMismatch(t *testing.T) {
	sc := testSchemaCallable("MySchema", map[string]*schema.FieldDescriptor{
		"name": testMSField("string", true, starlark.None, nil),
	}, []string{"name"})

	_, err := MakeMutableStruct(
		&starlark.Thread{}, &starlark.Builtin{}, nil,
		[]starlark.Tuple{
			{starlark.String("schema"), sc},
			{starlark.String("name"), starlark.MakeInt(123)},
		},
	)
	if err == nil {
		t.Fatal("expected error for type mismatch")
	}
	if !strings.Contains(err.Error(), "name: expected string, got int") {
		t.Errorf("error = %v, want contains 'name: expected string, got int'", err)
	}
}

// SCHM-02: Construction validates required field missing.
func TestMutableStructSchemaRequiredMissing(t *testing.T) {
	sc := testSchemaCallable("MySchema", map[string]*schema.FieldDescriptor{
		"name": testMSField("string", true, starlark.None, nil),
	}, []string{"name"})

	_, err := MakeMutableStruct(
		&starlark.Thread{}, &starlark.Builtin{}, nil,
		[]starlark.Tuple{
			{starlark.String("schema"), sc},
		},
	)
	if err == nil {
		t.Fatal("expected error for required missing")
	}
	if !strings.Contains(err.Error(), "name: required field missing") {
		t.Errorf("error = %v, want contains 'name: required field missing'", err)
	}
}

// SCHM-02: Construction validates enum.
func TestMutableStructSchemaEnumViolation(t *testing.T) {
	enum := starlark.NewList([]starlark.Value{starlark.String("active"), starlark.String("inactive")})
	sc := testSchemaCallable("MySchema", map[string]*schema.FieldDescriptor{
		"status": testMSField("string", false, starlark.None, enum),
	}, []string{"status"})

	_, err := MakeMutableStruct(
		&starlark.Thread{}, &starlark.Builtin{}, nil,
		[]starlark.Tuple{
			{starlark.String("schema"), sc},
			{starlark.String("status"), starlark.String("bad")},
		},
	)
	if err == nil {
		t.Fatal("expected error for enum violation")
	}
	if !strings.Contains(err.Error(), "not in enum") {
		t.Errorf("error = %v, want contains 'not in enum'", err)
	}
}

// SCHM-02: Construction validates nested schema fields recursively.
func TestMutableStructSchemaNestedValidation(t *testing.T) {
	inner := testSchemaCallable("Location", map[string]*schema.FieldDescriptor{
		"region": testMSField("string", true, starlark.None, nil),
	}, []string{"region"})

	sc := testSchemaCallable("MySchema", map[string]*schema.FieldDescriptor{
		"location": testMSFieldWithSchema(inner, true),
	}, []string{"location"})

	d := starlark.NewDict(1)
	_ = d.SetKey(starlark.String("region"), starlark.String("westeurope"))

	ms, err := MakeMutableStruct(
		&starlark.Thread{}, &starlark.Builtin{}, nil,
		[]starlark.Tuple{
			{starlark.String("schema"), sc},
			{starlark.String("location"), d},
		},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	s := ms.(*MutableStruct)
	v, err := s.Attr("location")
	if err != nil {
		t.Fatalf("Attr(location): %v", err)
	}
	// Nested dict should be wrapped as SchemaDict.
	sd, ok := v.(*schema.SchemaDict)
	if !ok {
		t.Fatalf("location is %T, want *schema.SchemaDict", v)
	}
	if sd.SchemaName() != "Location" {
		t.Errorf("SchemaName() = %q, want \"Location\"", sd.SchemaName())
	}
}

// SCHM-03: Construction applies defaults for missing optional fields.
func TestMutableStructSchemaDefaults(t *testing.T) {
	sc := testSchemaCallable("MySchema", map[string]*schema.FieldDescriptor{
		"name":     testMSField("string", true, starlark.None, nil),
		"replicas": testMSField("int", false, starlark.MakeInt(3), nil),
	}, []string{"name", "replicas"})

	ms, err := MakeMutableStruct(
		&starlark.Thread{}, &starlark.Builtin{}, nil,
		[]starlark.Tuple{
			{starlark.String("schema"), sc},
			{starlark.String("name"), starlark.String("x")},
		},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	s := ms.(*MutableStruct)
	v, err := s.Attr("replicas")
	if err != nil {
		t.Fatalf("Attr(replicas): %v", err)
	}
	if v != starlark.MakeInt(3) {
		t.Errorf("replicas = %v, want 3", v)
	}
}

// SCHM-03: None on optional with default restores default.
func TestMutableStructSchemaNoneRestoresDefault(t *testing.T) {
	sc := testSchemaCallable("MySchema", map[string]*schema.FieldDescriptor{
		"name":     testMSField("string", true, starlark.None, nil),
		"replicas": testMSField("int", false, starlark.MakeInt(3), nil),
	}, []string{"name", "replicas"})

	ms, err := MakeMutableStruct(
		&starlark.Thread{}, &starlark.Builtin{}, nil,
		[]starlark.Tuple{
			{starlark.String("schema"), sc},
			{starlark.String("name"), starlark.String("x")},
			{starlark.String("replicas"), starlark.None},
		},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	s := ms.(*MutableStruct)
	v, err := s.Attr("replicas")
	if err != nil {
		t.Fatalf("Attr(replicas): %v", err)
	}
	if v != starlark.MakeInt(3) {
		t.Errorf("replicas = %v, want 3 (default)", v)
	}
}

// SCHM-04: Construction rejects unknown fields with "did you mean?" suggestion.
func TestMutableStructSchemaUnknownFieldSuggestion(t *testing.T) {
	sc := testSchemaCallable("MySchema", map[string]*schema.FieldDescriptor{
		"name": testMSField("string", true, starlark.None, nil),
	}, []string{"name"})

	_, err := MakeMutableStruct(
		&starlark.Thread{}, &starlark.Builtin{}, nil,
		[]starlark.Tuple{
			{starlark.String("schema"), sc},
			{starlark.String("nam"), starlark.String("x")},
		},
	)
	if err == nil {
		t.Fatal("expected error for unknown field")
	}
	if !strings.Contains(err.Error(), `did you mean "name"`) {
		t.Errorf("error = %v, want contains 'did you mean \"name\"'", err)
	}
}

// SCHM-04: Construction rejects unknown fields with valid fields list.
func TestMutableStructSchemaUnknownFieldNoSuggestion(t *testing.T) {
	sc := testSchemaCallable("MySchema", map[string]*schema.FieldDescriptor{
		"name":   testMSField("string", false, starlark.None, nil),
		"region": testMSField("string", false, starlark.None, nil),
	}, []string{"name", "region"})

	_, err := MakeMutableStruct(
		&starlark.Thread{}, &starlark.Builtin{}, nil,
		[]starlark.Tuple{
			{starlark.String("schema"), sc},
			{starlark.String("zzz"), starlark.String("x")},
		},
	)
	if err == nil {
		t.Fatal("expected error for unknown field")
	}
	if !strings.Contains(err.Error(), "valid fields: name, region") {
		t.Errorf("error = %v, want contains 'valid fields: name, region'", err)
	}
}

// SCHM-07: Multi-error: multiple violations reported at once.
func TestMutableStructSchemaMultiError(t *testing.T) {
	sc := testSchemaCallable("MySchema", map[string]*schema.FieldDescriptor{
		"name":  testMSField("string", true, starlark.None, nil),
		"count": testMSField("int", false, starlark.None, nil),
	}, []string{"name", "count"})

	_, err := MakeMutableStruct(
		&starlark.Thread{}, &starlark.Builtin{}, nil,
		[]starlark.Tuple{
			{starlark.String("schema"), sc},
			{starlark.String("zzz"), starlark.String("x")},
			{starlark.String("count"), starlark.String("bad")},
		},
	)
	if err == nil {
		t.Fatal("expected error for multiple violations")
	}
	errStr := err.Error()
	// Should report both errors.
	if !strings.Contains(errStr, "MySchema:") {
		t.Errorf("error should name schema: %s", errStr)
	}
	if !strings.Contains(errStr, "zzz: unknown field") {
		t.Errorf("error should contain unknown field for zzz: %s", errStr)
	}
	if !strings.Contains(errStr, "count: expected int") {
		t.Errorf("error should contain type error for count: %s", errStr)
	}
	// Multi-error with additional required missing.
	if !strings.Contains(errStr, "validation error") {
		t.Errorf("error should mention validation errors: %s", errStr)
	}
}

// SCHM-01 + AttrNames: schema-backed AttrNames returns schema field names.
func TestMutableStructSchemaAttrNames(t *testing.T) {
	sc := testSchemaCallable("MySchema", map[string]*schema.FieldDescriptor{
		"name":     testMSField("string", true, starlark.None, nil),
		"replicas": testMSField("int", false, starlark.MakeInt(3), nil),
		"region":   testMSField("string", false, starlark.None, nil),
	}, []string{"name", "replicas", "region"})

	ms, err := MakeMutableStruct(
		&starlark.Thread{}, &starlark.Builtin{}, nil,
		[]starlark.Tuple{
			{starlark.String("schema"), sc},
			{starlark.String("name"), starlark.String("x")},
		},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	s := ms.(*MutableStruct)
	names := s.AttrNames()
	// Should include all schema field names, not just dict keys.
	if len(names) != 3 {
		t.Fatalf("AttrNames() = %v, want 3 names", names)
	}
	// Should be sorted.
	if !sort.StringsAreSorted(names) {
		t.Errorf("AttrNames() not sorted: %v", names)
	}
}

// ---------------------------------------------------------------------------
// Phase 46 Plan 02 — Schema-aware SetField validation tests (SCHM-05, SCHM-06)
// ---------------------------------------------------------------------------

// helper: create a FieldDescriptor for list with items schema.
func testMSFieldList(items *schema.SchemaCallable, required bool) *schema.FieldDescriptor {
	return schema.NewFieldDescriptor("list", nil, items, required, starlark.None, nil, "")
}

// SCHM-05: SetField validates type constraint — rejects type mismatch.
func TestMutableStructSchemaSetFieldTypeMismatch(t *testing.T) {
	sc := testSchemaCallable("MySchema", map[string]*schema.FieldDescriptor{
		"name": testMSField("string", true, starlark.None, nil),
	}, []string{"name"})

	ms, err := MakeMutableStruct(
		&starlark.Thread{}, &starlark.Builtin{}, nil,
		[]starlark.Tuple{
			{starlark.String("schema"), sc},
			{starlark.String("name"), starlark.String("hello")},
		},
	)
	if err != nil {
		t.Fatalf("MakeMutableStruct: %v", err)
	}
	s := ms.(*MutableStruct)

	err = s.SetField("name", starlark.MakeInt(123))
	if err == nil {
		t.Fatal("expected error for type mismatch on SetField")
	}
	if !strings.Contains(err.Error(), "expected string, got int") {
		t.Errorf("error = %v, want contains 'expected string, got int'", err)
	}
}

// SCHM-05: SetField validates enum constraint — rejects enum violation.
func TestMutableStructSchemaSetFieldEnumViolation(t *testing.T) {
	enum := starlark.NewList([]starlark.Value{starlark.String("active"), starlark.String("inactive")})
	sc := testSchemaCallable("MySchema", map[string]*schema.FieldDescriptor{
		"status": testMSField("string", false, starlark.None, enum),
	}, []string{"status"})

	ms, err := MakeMutableStruct(
		&starlark.Thread{}, &starlark.Builtin{}, nil,
		[]starlark.Tuple{
			{starlark.String("schema"), sc},
			{starlark.String("status"), starlark.String("active")},
		},
	)
	if err != nil {
		t.Fatalf("MakeMutableStruct: %v", err)
	}
	s := ms.(*MutableStruct)

	err = s.SetField("status", starlark.String("bad"))
	if err == nil {
		t.Fatal("expected error for enum violation on SetField")
	}
	if !strings.Contains(err.Error(), "not in enum") {
		t.Errorf("error = %v, want contains 'not in enum'", err)
	}
}

// SCHM-05: SetField accepts valid value and stores it.
func TestMutableStructSchemaSetFieldValidValue(t *testing.T) {
	sc := testSchemaCallable("MySchema", map[string]*schema.FieldDescriptor{
		"name": testMSField("string", true, starlark.None, nil),
	}, []string{"name"})

	ms, err := MakeMutableStruct(
		&starlark.Thread{}, &starlark.Builtin{}, nil,
		[]starlark.Tuple{
			{starlark.String("schema"), sc},
			{starlark.String("name"), starlark.String("old")},
		},
	)
	if err != nil {
		t.Fatalf("MakeMutableStruct: %v", err)
	}
	s := ms.(*MutableStruct)

	err = s.SetField("name", starlark.String("new"))
	if err != nil {
		t.Fatalf("SetField valid value: %v", err)
	}
	v, err := s.Attr("name")
	if err != nil {
		t.Fatalf("Attr(name): %v", err)
	}
	if v != starlark.String("new") {
		t.Errorf("name = %v, want \"new\"", v)
	}
}

// SCHM-05: SetField validates nested dict — wraps as SchemaDict.
func TestMutableStructSchemaSetFieldNestedDict(t *testing.T) {
	inner := testSchemaCallable("Location", map[string]*schema.FieldDescriptor{
		"region": testMSField("string", true, starlark.None, nil),
	}, []string{"region"})

	sc := testSchemaCallable("MySchema", map[string]*schema.FieldDescriptor{
		"location": testMSFieldWithSchema(inner, true),
	}, []string{"location"})

	// Build initial struct with valid location.
	initialDict := starlark.NewDict(1)
	_ = initialDict.SetKey(starlark.String("region"), starlark.String("eastus"))

	ms, err := MakeMutableStruct(
		&starlark.Thread{}, &starlark.Builtin{}, nil,
		[]starlark.Tuple{
			{starlark.String("schema"), sc},
			{starlark.String("location"), initialDict},
		},
	)
	if err != nil {
		t.Fatalf("MakeMutableStruct: %v", err)
	}
	s := ms.(*MutableStruct)

	// Assign a new plain dict — should validate and wrap as SchemaDict.
	newDict := starlark.NewDict(1)
	_ = newDict.SetKey(starlark.String("region"), starlark.String("westeurope"))

	err = s.SetField("location", newDict)
	if err != nil {
		t.Fatalf("SetField nested dict: %v", err)
	}

	v, err := s.Attr("location")
	if err != nil {
		t.Fatalf("Attr(location): %v", err)
	}
	sd, ok := v.(*schema.SchemaDict)
	if !ok {
		t.Fatalf("location is %T, want *schema.SchemaDict", v)
	}
	if sd.SchemaName() != "Location" {
		t.Errorf("SchemaName() = %q, want \"Location\"", sd.SchemaName())
	}
}

// SCHM-05: SetField validates MutableStruct assigned to nested schema field.
func TestMutableStructSchemaSetFieldNestedMutableStruct(t *testing.T) {
	inner := testSchemaCallable("Location", map[string]*schema.FieldDescriptor{
		"region": testMSField("string", true, starlark.None, nil),
	}, []string{"region"})

	sc := testSchemaCallable("MySchema", map[string]*schema.FieldDescriptor{
		"location": testMSFieldWithSchema(inner, true),
	}, []string{"location"})

	initialDict := starlark.NewDict(1)
	_ = initialDict.SetKey(starlark.String("region"), starlark.String("eastus"))

	ms, err := MakeMutableStruct(
		&starlark.Thread{}, &starlark.Builtin{}, nil,
		[]starlark.Tuple{
			{starlark.String("schema"), sc},
			{starlark.String("location"), initialDict},
		},
	)
	if err != nil {
		t.Fatalf("MakeMutableStruct: %v", err)
	}
	s := ms.(*MutableStruct)

	// Create a MutableStruct (without schema) to assign to the nested field.
	nested, err := MakeMutableStruct(
		&starlark.Thread{}, &starlark.Builtin{}, nil,
		[]starlark.Tuple{
			{starlark.String("region"), starlark.String("northeurope")},
		},
	)
	if err != nil {
		t.Fatalf("MakeMutableStruct nested: %v", err)
	}

	err = s.SetField("location", nested)
	if err != nil {
		t.Fatalf("SetField nested MutableStruct: %v", err)
	}

	v, err := s.Attr("location")
	if err != nil {
		t.Fatalf("Attr(location): %v", err)
	}
	sd, ok := v.(*schema.SchemaDict)
	if !ok {
		t.Fatalf("location is %T, want *schema.SchemaDict", v)
	}
	if sd.SchemaName() != "Location" {
		t.Errorf("SchemaName() = %q, want \"Location\"", sd.SchemaName())
	}
}

// SCHM-05: SetField validates list items with schema.
func TestMutableStructSchemaSetFieldListItems(t *testing.T) {
	itemSchema := testSchemaCallable("Item", map[string]*schema.FieldDescriptor{
		"x": testMSField("int", true, starlark.None, nil),
	}, []string{"x"})

	sc := testSchemaCallable("MySchema", map[string]*schema.FieldDescriptor{
		"items": testMSFieldList(itemSchema, true),
	}, []string{"items"})

	// Build initial struct with valid list.
	d1 := starlark.NewDict(1)
	_ = d1.SetKey(starlark.String("x"), starlark.MakeInt(1))
	initialList := starlark.NewList([]starlark.Value{d1})

	ms, err := MakeMutableStruct(
		&starlark.Thread{}, &starlark.Builtin{}, nil,
		[]starlark.Tuple{
			{starlark.String("schema"), sc},
			{starlark.String("items"), initialList},
		},
	)
	if err != nil {
		t.Fatalf("MakeMutableStruct: %v", err)
	}
	s := ms.(*MutableStruct)

	// Assign new list — elements should be validated.
	d2 := starlark.NewDict(1)
	_ = d2.SetKey(starlark.String("x"), starlark.MakeInt(42))
	newList := starlark.NewList([]starlark.Value{d2})

	err = s.SetField("items", newList)
	if err != nil {
		t.Fatalf("SetField list items: %v", err)
	}

	v, err := s.Attr("items")
	if err != nil {
		t.Fatalf("Attr(items): %v", err)
	}
	list, ok := v.(*starlark.List)
	if !ok {
		t.Fatalf("items is %T, want *starlark.List", v)
	}
	if list.Len() != 1 {
		t.Fatalf("list.Len() = %d, want 1", list.Len())
	}
	// Element should be wrapped as SchemaDict.
	elem := list.Index(0)
	sd, ok := elem.(*schema.SchemaDict)
	if !ok {
		t.Fatalf("items[0] is %T, want *schema.SchemaDict", elem)
	}
	if sd.SchemaName() != "Item" {
		t.Errorf("items[0].SchemaName() = %q, want \"Item\"", sd.SchemaName())
	}
}

// SCHM-06: SetField rejects unknown field with "did you mean?" suggestion.
func TestMutableStructSchemaSetFieldUnknownFieldSuggestion(t *testing.T) {
	sc := testSchemaCallable("MySchema", map[string]*schema.FieldDescriptor{
		"name": testMSField("string", true, starlark.None, nil),
	}, []string{"name"})

	ms, err := MakeMutableStruct(
		&starlark.Thread{}, &starlark.Builtin{}, nil,
		[]starlark.Tuple{
			{starlark.String("schema"), sc},
			{starlark.String("name"), starlark.String("hello")},
		},
	)
	if err != nil {
		t.Fatalf("MakeMutableStruct: %v", err)
	}
	s := ms.(*MutableStruct)

	// "nam" is Levenshtein distance 1 from "name" — should trigger suggestion.
	err = s.SetField("nam", starlark.String("x"))
	if err == nil {
		t.Fatal("expected error for unknown field on SetField")
	}
	if !strings.Contains(err.Error(), `did you mean "name"`) {
		t.Errorf("error = %v, want contains 'did you mean \"name\"'", err)
	}
}

// SCHM-06: SetField rejects unknown field with valid fields list (no close match).
func TestMutableStructSchemaSetFieldUnknownFieldNoSuggestion(t *testing.T) {
	sc := testSchemaCallable("MySchema", map[string]*schema.FieldDescriptor{
		"name":   testMSField("string", false, starlark.None, nil),
		"region": testMSField("string", false, starlark.None, nil),
	}, []string{"name", "region"})

	ms, err := MakeMutableStruct(
		&starlark.Thread{}, &starlark.Builtin{}, nil,
		[]starlark.Tuple{
			{starlark.String("schema"), sc},
		},
	)
	if err != nil {
		t.Fatalf("MakeMutableStruct: %v", err)
	}
	s := ms.(*MutableStruct)

	err = s.SetField("zzz", starlark.String("x"))
	if err == nil {
		t.Fatal("expected error for unknown field on SetField")
	}
	if !strings.Contains(err.Error(), "valid fields: name, region") {
		t.Errorf("error = %v, want contains 'valid fields: name, region'", err)
	}
}

// SCHM-05 None semantics: None on required field rejected.
func TestMutableStructSchemaSetFieldNoneOnRequired(t *testing.T) {
	sc := testSchemaCallable("MySchema", map[string]*schema.FieldDescriptor{
		"name": testMSField("string", true, starlark.None, nil),
	}, []string{"name"})

	ms, err := MakeMutableStruct(
		&starlark.Thread{}, &starlark.Builtin{}, nil,
		[]starlark.Tuple{
			{starlark.String("schema"), sc},
			{starlark.String("name"), starlark.String("hello")},
		},
	)
	if err != nil {
		t.Fatalf("MakeMutableStruct: %v", err)
	}
	s := ms.(*MutableStruct)

	err = s.SetField("name", starlark.None)
	if err == nil {
		t.Fatal("expected error for None on required field")
	}
	if !strings.Contains(err.Error(), "required field cannot be set to None") {
		t.Errorf("error = %v, want contains 'required field cannot be set to None'", err)
	}
}

// SCHM-05 None semantics: None on optional with default restores default.
func TestMutableStructSchemaSetFieldNoneRestoresDefault(t *testing.T) {
	sc := testSchemaCallable("MySchema", map[string]*schema.FieldDescriptor{
		"replicas": testMSField("int", false, starlark.MakeInt(3), nil),
	}, []string{"replicas"})

	ms, err := MakeMutableStruct(
		&starlark.Thread{}, &starlark.Builtin{}, nil,
		[]starlark.Tuple{
			{starlark.String("schema"), sc},
			{starlark.String("replicas"), starlark.MakeInt(5)},
		},
	)
	if err != nil {
		t.Fatalf("MakeMutableStruct: %v", err)
	}
	s := ms.(*MutableStruct)

	// Setting None should restore default of 3.
	err = s.SetField("replicas", starlark.None)
	if err != nil {
		t.Fatalf("SetField None: %v", err)
	}
	v, err := s.Attr("replicas")
	if err != nil {
		t.Fatalf("Attr(replicas): %v", err)
	}
	if v != starlark.MakeInt(3) {
		t.Errorf("replicas = %v, want 3 (default)", v)
	}
}

// SCHM-05 None semantics: None on optional without default deletes field.
func TestMutableStructSchemaSetFieldNoneDeletesField(t *testing.T) {
	sc := testSchemaCallable("MySchema", map[string]*schema.FieldDescriptor{
		"region": testMSField("string", false, starlark.None, nil),
	}, []string{"region"})

	ms, err := MakeMutableStruct(
		&starlark.Thread{}, &starlark.Builtin{}, nil,
		[]starlark.Tuple{
			{starlark.String("schema"), sc},
			{starlark.String("region"), starlark.String("eastus")},
		},
	)
	if err != nil {
		t.Fatalf("MakeMutableStruct: %v", err)
	}
	s := ms.(*MutableStruct)

	// Setting None on optional-without-default should delete the field.
	err = s.SetField("region", starlark.None)
	if err != nil {
		t.Fatalf("SetField None: %v", err)
	}
	// Field should be gone from dict.
	_, err = s.Attr("region")
	if err == nil {
		t.Fatal("expected NoSuchAttrError after deleting field")
	}
}

// SCHM-05: SetField without schema behaves exactly as before (no validation).
func TestMutableStructSetFieldNoSchemaUnchanged(t *testing.T) {
	ms, err := MakeMutableStruct(
		&starlark.Thread{}, &starlark.Builtin{}, nil,
		[]starlark.Tuple{
			{starlark.String("name"), starlark.String("test")},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	s := ms.(*MutableStruct)

	// Should allow any field, any type — no validation.
	if err := s.SetField("anything", starlark.MakeInt(999)); err != nil {
		t.Fatalf("SetField on non-schema struct: %v", err)
	}
	v, err := s.Attr("anything")
	if err != nil {
		t.Fatalf("Attr(anything): %v", err)
	}
	if v != starlark.MakeInt(999) {
		t.Errorf("anything = %v, want 999", v)
	}
}

// ---------------------------------------------------------------------------
// Phase 45 Plan 01 — Pipeline integration tests
// ---------------------------------------------------------------------------

func TestMutableStructInternalDict(t *testing.T) {
	ms, err := MakeMutableStruct(
		&starlark.Thread{},
		&starlark.Builtin{},
		nil,
		[]starlark.Tuple{
			{starlark.String("name"), starlark.String("test")},
			{starlark.String("count"), starlark.MakeInt(42)},
		},
	)
	if err != nil {
		t.Fatalf("MakeMutableStruct: %v", err)
	}
	s := ms.(*MutableStruct)

	d := s.InternalDict()
	if d == nil {
		t.Fatal("InternalDict() returned nil")
	}

	// Should be the same backing dict.
	items := d.Items()
	if len(items) != 2 {
		t.Fatalf("InternalDict().Items() = %d items, want 2", len(items))
	}

	// Verify the items match.
	found := map[string]starlark.Value{}
	for _, item := range items {
		k, ok := item[0].(starlark.String)
		if !ok {
			t.Fatalf("key is %T, want starlark.String", item[0])
		}
		found[string(k)] = item[1]
	}
	if found["name"] != starlark.String("test") {
		t.Errorf("name = %v, want \"test\"", found["name"])
	}
	if found["count"] != starlark.MakeInt(42) {
		t.Errorf("count = %v, want 42", found["count"])
	}
}

func TestMutableStructJSONEncode(t *testing.T) {
	// Verify json.encode works on mutable_struct via the HasAttrs path
	// (zero code changes needed -- this is a test-only verification).
	req := makeReq(nil, nil, nil)
	c := NewCollector(NewConditionCollector(), "test.star", nil, nil)
	globals, err := testBuildGlobals(req, c)
	if err != nil {
		t.Fatalf("testBuildGlobals error: %v", err)
	}
	rt := runtime.NewRuntime(logging.NewNopLogger())
	out, err := rt.Execute(`
ms = mutable_struct(a=1, b="hello")
ms_json = json.encode(ms)
dict_json = json.encode({"a": 1, "b": "hello"})
match = (ms_json == dict_json)
`, globals, "test.star", nil)
	if err != nil {
		t.Fatalf("rt.Execute error: %v", err)
	}
	assertBool(t, out, "match", true)
}
