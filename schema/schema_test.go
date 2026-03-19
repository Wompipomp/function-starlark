package schema

import (
	"strings"
	"testing"

	"go.starlark.net/starlark"
)

// helper: create a FieldDescriptor directly for testing.
func testField(typeName string, required bool, defVal starlark.Value, enum *starlark.List) *FieldDescriptor {
	return &FieldDescriptor{
		typeName: typeName,
		required: required,
		defVal:   defVal,
		enum:     enum,
	}
}

// helper: call SchemaCallable with kwargs.
func callSchema(s *SchemaCallable, kwargs ...starlark.Tuple) (starlark.Value, error) {
	return s.CallInternal(nil, nil, kwargs)
}

func kv(key string, val starlark.Value) starlark.Tuple {
	return starlark.Tuple{starlark.String(key), val}
}

// --- SchemaBuiltin creation tests ---

func TestSchemaBuiltinCreatesCallable(t *testing.T) {
	thread := &starlark.Thread{Name: "test"}
	builtin := SchemaBuiltin()

	result, err := starlark.Call(thread, builtin, starlark.Tuple{starlark.String("Account")}, []starlark.Tuple{
		kv("location", testField("string", false, starlark.None, nil)),
	})
	if err != nil {
		t.Fatalf("schema() failed: %v", err)
	}

	sc, ok := result.(*SchemaCallable)
	if !ok {
		t.Fatalf("schema() returned %T, want *SchemaCallable", result)
	}
	if sc.Name() != "Account" {
		t.Errorf("Name() = %q, want %q", sc.Name(), "Account")
	}
}

func TestSchemaBuiltinWithDoc(t *testing.T) {
	thread := &starlark.Thread{Name: "test"}
	builtin := SchemaBuiltin()

	result, err := starlark.Call(thread, builtin, starlark.Tuple{starlark.String("Account")}, []starlark.Tuple{
		kv("doc", starlark.String("An account")),
		kv("location", testField("string", false, starlark.None, nil)),
	})
	if err != nil {
		t.Fatalf("schema() with doc failed: %v", err)
	}

	sc := result.(*SchemaCallable)
	doc, _ := sc.Attr("doc")
	if doc.(starlark.String) != "An account" {
		t.Errorf("doc = %v, want %q", doc, "An account")
	}
}

func TestSchemaBuiltinEmptySchema(t *testing.T) {
	thread := &starlark.Thread{Name: "test"}
	builtin := SchemaBuiltin()

	_, err := starlark.Call(thread, builtin, starlark.Tuple{starlark.String("Empty")}, nil)
	if err != nil {
		t.Fatalf("schema() with no fields failed: %v", err)
	}
}

func TestSchemaBuiltinRejectsNonField(t *testing.T) {
	thread := &starlark.Thread{Name: "test"}
	builtin := SchemaBuiltin()

	_, err := starlark.Call(thread, builtin, starlark.Tuple{starlark.String("Account")}, []starlark.Tuple{
		kv("location", starlark.String("not a field")),
	})
	if err == nil {
		t.Fatal("expected error for non-FieldDescriptor kwarg")
	}
	if !strings.Contains(err.Error(), "must be a field()") {
		t.Errorf("error = %v, want contains 'must be a field()'", err)
	}
}

func TestSchemaBuiltinRequiresName(t *testing.T) {
	thread := &starlark.Thread{Name: "test"}
	builtin := SchemaBuiltin()

	_, err := starlark.Call(thread, builtin, nil, nil)
	if err == nil {
		t.Fatal("expected error for missing name")
	}
}

// --- SchemaCallable Value interface tests ---

func TestSchemaCallableString(t *testing.T) {
	s := &SchemaCallable{name: "Account"}
	if s.String() != "<schema Account>" {
		t.Errorf("String() = %q, want %q", s.String(), "<schema Account>")
	}
}

func TestSchemaCallableType(t *testing.T) {
	s := &SchemaCallable{name: "Account"}
	if s.Type() != "schema" {
		t.Errorf("Type() = %q, want %q", s.Type(), "schema")
	}
}

func TestSchemaCallableName(t *testing.T) {
	s := &SchemaCallable{name: "Account"}
	if s.Name() != "Account" {
		t.Errorf("Name() = %q, want %q", s.Name(), "Account")
	}
}

func TestSchemaCallableTruth(t *testing.T) {
	s := &SchemaCallable{name: "Account"}
	if s.Truth() != starlark.True {
		t.Error("Truth() should be True")
	}
}

func TestSchemaCallableHash(t *testing.T) {
	s := &SchemaCallable{name: "Account"}
	_, err := s.Hash()
	if err == nil {
		t.Error("Hash() should return error")
	}
}

// --- Introspection tests ---

func TestSchemaAttrName(t *testing.T) {
	s := &SchemaCallable{name: "Account"}
	v, err := s.Attr("name")
	if err != nil {
		t.Fatal(err)
	}
	if v.(starlark.String) != "Account" {
		t.Errorf("Attr(name) = %v, want %q", v, "Account")
	}
}

func TestSchemaAttrDoc(t *testing.T) {
	s := &SchemaCallable{name: "Account", doc: "My doc"}
	v, err := s.Attr("doc")
	if err != nil {
		t.Fatal(err)
	}
	if v.(starlark.String) != "My doc" {
		t.Errorf("Attr(doc) = %v, want %q", v, "My doc")
	}
}

func TestSchemaAttrFields(t *testing.T) {
	s := &SchemaCallable{
		name: "Account",
		fields: map[string]*FieldDescriptor{
			"location": testField("string", false, starlark.None, nil),
		},
		order: []string{"location"},
	}
	v, err := s.Attr("fields")
	if err != nil {
		t.Fatal(err)
	}
	d, ok := v.(*starlark.Dict)
	if !ok {
		t.Fatalf("Attr(fields) returned %T, want *starlark.Dict", v)
	}
	if d.Len() != 1 {
		t.Errorf("fields dict len = %d, want 1", d.Len())
	}
}

func TestSchemaAttrNames(t *testing.T) {
	s := &SchemaCallable{name: "Account"}
	names := s.AttrNames()
	want := []string{"doc", "fields", "name"}
	if len(names) != len(want) {
		t.Fatalf("AttrNames() = %v, want %v", names, want)
	}
	for i, n := range names {
		if n != want[i] {
			t.Errorf("AttrNames()[%d] = %q, want %q", i, n, want[i])
		}
	}
}

// --- Constructor (CallInternal) tests ---

func TestConstructorHappyPath(t *testing.T) {
	s := &SchemaCallable{
		name: "Account",
		fields: map[string]*FieldDescriptor{
			"location": testField("string", false, starlark.None, nil),
		},
		order: []string{"location"},
	}

	result, err := callSchema(s, kv("location", starlark.String("westeurope")))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	d, ok := result.(*starlark.Dict)
	if !ok {
		t.Fatalf("result is %T, want *starlark.Dict", result)
	}

	v, found, _ := d.Get(starlark.String("location"))
	if !found {
		t.Fatal("location key not found")
	}
	if v.(starlark.String) != "westeurope" {
		t.Errorf("location = %v, want westeurope", v)
	}
}

func TestConstructorTypeMismatch(t *testing.T) {
	s := &SchemaCallable{
		name: "Account",
		fields: map[string]*FieldDescriptor{
			"location": testField("string", true, starlark.None, nil),
		},
		order: []string{"location"},
	}

	_, err := callSchema(s, kv("location", starlark.MakeInt(123)))
	if err == nil {
		t.Fatal("expected error for type mismatch")
	}
	errStr := err.Error()
	if !strings.Contains(errStr, "Account: 1 validation error") {
		t.Errorf("error = %v, want contains 'Account: 1 validation error'", errStr)
	}
	if !strings.Contains(errStr, "location: expected string, got int (123)") {
		t.Errorf("error = %v, want contains 'location: expected string, got int (123)'", errStr)
	}
}

func TestConstructorRequiredMissing(t *testing.T) {
	s := &SchemaCallable{
		name: "Account",
		fields: map[string]*FieldDescriptor{
			"location": testField("string", true, starlark.None, nil),
		},
		order: []string{"location"},
	}

	_, err := callSchema(s)
	if err == nil {
		t.Fatal("expected error for required missing")
	}
	if !strings.Contains(err.Error(), "location: required field missing") {
		t.Errorf("error = %v, want contains 'location: required field missing'", err)
	}
}

func TestConstructorUnknownFieldWithSuggestion(t *testing.T) {
	s := &SchemaCallable{
		name: "Account",
		fields: map[string]*FieldDescriptor{
			"location": testField("string", false, starlark.None, nil),
		},
		order: []string{"location"},
	}

	_, err := callSchema(s, kv("loction", starlark.String("x")))
	if err == nil {
		t.Fatal("expected error for unknown field")
	}
	if !strings.Contains(err.Error(), `did you mean "location"`) {
		t.Errorf("error = %v, want contains 'did you mean \"location\"'", err)
	}
}

func TestConstructorUnknownFieldNoSuggestion(t *testing.T) {
	s := &SchemaCallable{
		name: "Account",
		fields: map[string]*FieldDescriptor{
			"location": testField("string", false, starlark.None, nil),
			"sku":      testField("string", false, starlark.None, nil),
		},
		order: []string{"location", "sku"},
	}

	_, err := callSchema(s, kv("xyzzy", starlark.String("x")))
	if err == nil {
		t.Fatal("expected error for unknown field")
	}
	if !strings.Contains(err.Error(), "valid fields: location, sku") {
		t.Errorf("error = %v, want contains 'valid fields: location, sku'", err)
	}
}

func TestConstructorMultipleErrors(t *testing.T) {
	s := &SchemaCallable{
		name: "Account",
		fields: map[string]*FieldDescriptor{
			"location": testField("string", true, starlark.None, nil),
			"sku":      testField("string", true, starlark.None, nil),
			"tags":     testField("dict", true, starlark.None, nil),
		},
		order: []string{"location", "sku", "tags"},
	}

	// All three required fields missing.
	_, err := callSchema(s)
	if err == nil {
		t.Fatal("expected error for multiple missing fields")
	}
	if !strings.Contains(err.Error(), "Account: 3 validation errors") {
		t.Errorf("error = %v, want contains 'Account: 3 validation errors'", err)
	}
}

func TestConstructorNoneRequiredField(t *testing.T) {
	s := &SchemaCallable{
		name: "Account",
		fields: map[string]*FieldDescriptor{
			"location": testField("string", true, starlark.None, nil),
		},
		order: []string{"location"},
	}

	_, err := callSchema(s, kv("location", starlark.None))
	if err == nil {
		t.Fatal("expected error for None on required field")
	}
	if !strings.Contains(err.Error(), "location: required field missing") {
		t.Errorf("error = %v, want contains 'location: required field missing'", err)
	}
}

func TestConstructorNoneOptionalWithDefault(t *testing.T) {
	s := &SchemaCallable{
		name: "Account",
		fields: map[string]*FieldDescriptor{
			"region": testField("string", false, starlark.String("us"), nil),
		},
		order: []string{"region"},
	}

	result, err := callSchema(s, kv("region", starlark.None))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	d := result.(*starlark.Dict)
	v, found, _ := d.Get(starlark.String("region"))
	if !found {
		t.Fatal("region key not found")
	}
	if v.(starlark.String) != "us" {
		t.Errorf("region = %v, want 'us'", v)
	}
}

func TestConstructorNoneOptionalNoDefault(t *testing.T) {
	s := &SchemaCallable{
		name: "Account",
		fields: map[string]*FieldDescriptor{
			"region": testField("string", false, starlark.None, nil),
		},
		order: []string{"region"},
	}

	result, err := callSchema(s, kv("region", starlark.None))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	d := result.(*starlark.Dict)
	_, found, _ := d.Get(starlark.String("region"))
	if found {
		t.Error("region key should not be present when None + no default")
	}
}

func TestConstructorEnumViolation(t *testing.T) {
	enum := starlark.NewList([]starlark.Value{starlark.String("Standard_LRS")})
	s := &SchemaCallable{
		name: "Account",
		fields: map[string]*FieldDescriptor{
			"sku": testField("string", false, starlark.None, enum),
		},
		order: []string{"sku"},
	}

	_, err := callSchema(s, kv("sku", starlark.String("SuperFast")))
	if err == nil {
		t.Fatal("expected error for enum violation")
	}
	if !strings.Contains(err.Error(), `sku: value "SuperFast" not in enum`) {
		t.Errorf("error = %v, want contains enum error", err)
	}
}

func TestConstructorDefaultsApplied(t *testing.T) {
	s := &SchemaCallable{
		name: "Account",
		fields: map[string]*FieldDescriptor{
			"location": testField("string", false, starlark.None, nil),
			"region":   testField("string", false, starlark.String("eastus"), nil),
		},
		order: []string{"location", "region"},
	}

	result, err := callSchema(s, kv("location", starlark.String("west")))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	d := result.(*starlark.Dict)
	v, found, _ := d.Get(starlark.String("region"))
	if !found {
		t.Fatal("region default not applied")
	}
	if v.(starlark.String) != "eastus" {
		t.Errorf("region = %v, want 'eastus'", v)
	}
}

func TestConstructorRejectsPositionalArgs(t *testing.T) {
	s := &SchemaCallable{
		name:   "Account",
		fields: map[string]*FieldDescriptor{},
		order:  nil,
	}

	_, err := s.CallInternal(nil, starlark.Tuple{starlark.String("positional")}, nil)
	if err == nil {
		t.Fatal("expected error for positional args")
	}
	if !strings.Contains(err.Error(), "unexpected positional arguments") {
		t.Errorf("error = %v, want contains 'unexpected positional arguments'", err)
	}
}

func TestConstructorFreezeAndCallStillWorks(t *testing.T) {
	s := &SchemaCallable{
		name: "Account",
		fields: map[string]*FieldDescriptor{
			"location": testField("string", false, starlark.None, nil),
		},
		order: []string{"location"},
	}

	s.Freeze()

	// Calling after freeze should still work (returns new mutable dict).
	result, err := callSchema(s, kv("location", starlark.String("west")))
	if err != nil {
		t.Fatalf("call after freeze failed: %v", err)
	}

	d := result.(*starlark.Dict)
	// Verify returned dict is mutable.
	if err := d.SetKey(starlark.String("extra"), starlark.String("val")); err != nil {
		t.Errorf("returned dict should be mutable: %v", err)
	}
}
