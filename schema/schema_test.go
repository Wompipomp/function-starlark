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

// helper: create a FieldDescriptor with a nested schema reference.
func testFieldWithSchema(schema *SchemaCallable, required bool) *FieldDescriptor {
	return &FieldDescriptor{
		schema:   schema,
		required: required,
		defVal:   starlark.None,
	}
}

// helper: create a FieldDescriptor with list type and items schema.
func testFieldWithItems(items *SchemaCallable) *FieldDescriptor {
	return &FieldDescriptor{
		typeName: "list",
		items:    items,
		defVal:   starlark.None,
	}
}

// helper: build a *starlark.Dict from key-value pairs.
func makeDict(pairs ...starlark.Tuple) *starlark.Dict {
	d := starlark.NewDict(len(pairs))
	for _, p := range pairs {
		_ = d.SetKey(p[0], p[1])
	}
	return d
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

func TestConstructorEmptySchemaCall(t *testing.T) {
	s := &SchemaCallable{
		name:   "Empty",
		fields: map[string]*FieldDescriptor{},
		order:  nil,
	}

	result, err := callSchema(s)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	d, ok := result.(*starlark.Dict)
	if !ok {
		t.Fatalf("result is %T, want *starlark.Dict", result)
	}
	if d.Len() != 0 {
		t.Errorf("empty schema call returned dict with %d entries, want 0", d.Len())
	}
}

func TestConstructorEnumHappyPath(t *testing.T) {
	enum := starlark.NewList([]starlark.Value{starlark.String("Standard_LRS"), starlark.String("Standard_GRS")})
	s := &SchemaCallable{
		name: "Account",
		fields: map[string]*FieldDescriptor{
			"sku": testField("string", false, starlark.None, enum),
		},
		order: []string{"sku"},
	}

	result, err := callSchema(s, kv("sku", starlark.String("Standard_LRS")))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	d := result.(*starlark.Dict)
	v, found, _ := d.Get(starlark.String("sku"))
	if !found {
		t.Fatal("sku key not found")
	}
	if v.(starlark.String) != "Standard_LRS" {
		t.Errorf("sku = %v, want Standard_LRS", v)
	}
}

func TestConstructorGradualTyping(t *testing.T) {
	s := &SchemaCallable{
		name: "Flexible",
		fields: map[string]*FieldDescriptor{
			"value": testField("", false, starlark.None, nil), // type="" accepts anything
		},
		order: []string{"value"},
	}

	// Should accept string.
	result, err := callSchema(s, kv("value", starlark.String("hello")))
	if err != nil {
		t.Fatalf("string value rejected: %v", err)
	}
	d := result.(*starlark.Dict)
	v, _, _ := d.Get(starlark.String("value"))
	if v.(starlark.String) != "hello" {
		t.Errorf("value = %v, want hello", v)
	}

	// Should accept int.
	result, err = callSchema(s, kv("value", starlark.MakeInt(42)))
	if err != nil {
		t.Fatalf("int value rejected: %v", err)
	}
	d = result.(*starlark.Dict)
	v, _, _ = d.Get(starlark.String("value"))
	if v.(starlark.Int) != starlark.MakeInt(42) {
		t.Errorf("value = %v, want 42", v)
	}

	// Should accept bool.
	result, err = callSchema(s, kv("value", starlark.True))
	if err != nil {
		t.Fatalf("bool value rejected: %v", err)
	}
	d = result.(*starlark.Dict)
	v, _, _ = d.Get(starlark.String("value"))
	if v.(starlark.Bool) != starlark.True {
		t.Errorf("value = %v, want True", v)
	}
}

func TestConstructorMixedErrorTypes(t *testing.T) {
	s := &SchemaCallable{
		name: "Account",
		fields: map[string]*FieldDescriptor{
			"location": testField("string", true, starlark.None, nil),
			"sku":      testField("string", false, starlark.None, nil),
		},
		order: []string{"location", "sku"},
	}

	// type mismatch on sku + required missing on location + unknown field xyzzy
	_, err := callSchema(s,
		kv("sku", starlark.MakeInt(123)),
		kv("xyzzy", starlark.String("x")),
	)
	if err == nil {
		t.Fatal("expected error for mixed error types")
	}
	errStr := err.Error()
	if !strings.Contains(errStr, "Account: 3 validation errors") {
		t.Errorf("error = %v, want 3 validation errors", errStr)
	}
	if !strings.Contains(errStr, "sku: expected string, got int") {
		t.Errorf("error should contain type mismatch for sku: %s", errStr)
	}
	if !strings.Contains(errStr, "xyzzy: unknown field") {
		t.Errorf("error should contain unknown field xyzzy: %s", errStr)
	}
	if !strings.Contains(errStr, "location: required field missing") {
		t.Errorf("error should contain required missing for location: %s", errStr)
	}
}

func TestSchemaAttrUnknown(t *testing.T) {
	s := &SchemaCallable{name: "Account"}
	v, err := s.Attr("nonexistent")
	if err != nil {
		t.Fatalf("Attr(nonexistent) returned error: %v", err)
	}
	if v != nil {
		t.Errorf("Attr(nonexistent) = %v, want nil", v)
	}
}

func TestSchemaFieldsFreshDict(t *testing.T) {
	s := &SchemaCallable{
		name: "Account",
		fields: map[string]*FieldDescriptor{
			"location": testField("string", false, starlark.None, nil),
		},
		order: []string{"location"},
	}

	v1, err := s.Attr("fields")
	if err != nil {
		t.Fatal(err)
	}
	v2, err := s.Attr("fields")
	if err != nil {
		t.Fatal(err)
	}

	d1 := v1.(*starlark.Dict)
	d2 := v2.(*starlark.Dict)

	// Mutating d1 should not affect d2.
	_ = d1.SetKey(starlark.String("extra"), starlark.String("injected"))
	if d1.Len() != 2 {
		t.Errorf("d1.Len() = %d, want 2", d1.Len())
	}
	if d2.Len() != 1 {
		t.Errorf("d2.Len() = %d, want 1 (should be independent)", d2.Len())
	}
}

func TestSchemaBuiltinNonStringName(t *testing.T) {
	thread := &starlark.Thread{Name: "test"}
	builtin := SchemaBuiltin()

	_, err := starlark.Call(thread, builtin, starlark.Tuple{starlark.MakeInt(123)}, nil)
	if err == nil {
		t.Fatal("expected error for non-string name")
	}
	if !strings.Contains(err.Error(), "name must be a string") {
		t.Errorf("error = %v, want contains 'name must be a string'", err)
	}
}

func TestConstructorAllOptionalDefaults(t *testing.T) {
	s := &SchemaCallable{
		name: "Config",
		fields: map[string]*FieldDescriptor{
			"region":   testField("string", false, starlark.String("us-east-1"), nil),
			"replicas": testField("int", false, starlark.MakeInt(3), nil),
			"tags":     testField("dict", false, starlark.None, nil), // optional, no default
		},
		order: []string{"region", "replicas", "tags"},
	}

	// Call with no kwargs — defaults should be applied.
	result, err := callSchema(s)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	d := result.(*starlark.Dict)

	// region should have default.
	v, found, _ := d.Get(starlark.String("region"))
	if !found {
		t.Fatal("region not found")
	}
	if v.(starlark.String) != "us-east-1" {
		t.Errorf("region = %v, want us-east-1", v)
	}

	// replicas should have default.
	v, found, _ = d.Get(starlark.String("replicas"))
	if !found {
		t.Fatal("replicas not found")
	}
	if v.(starlark.Int) != starlark.MakeInt(3) {
		t.Errorf("replicas = %v, want 3", v)
	}

	// tags (optional, no default) should be omitted.
	_, found, _ = d.Get(starlark.String("tags"))
	if found {
		t.Error("tags should not be present (optional with no default)")
	}
}

// --- Nested schema validation tests (Plan 02) ---

// CallInternal returns SchemaDict instead of raw *starlark.Dict.
func TestConstructorReturnsSchemaDict(t *testing.T) {
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

	sd, ok := result.(*SchemaDict)
	if !ok {
		t.Fatalf("result is %T, want *SchemaDict", result)
	}
	if sd.SchemaName() != "Account" {
		t.Errorf("SchemaName() = %q, want %q", sd.SchemaName(), "Account")
	}

	v, found, _ := sd.Get(starlark.String("location"))
	if !found {
		t.Fatal("location key not found")
	}
	if v.(starlark.String) != "westeurope" {
		t.Errorf("location = %v, want westeurope", v)
	}
}

// Nested schema field with plain dict value validates recursively.
func TestConstructorPlainDictNested(t *testing.T) {
	inner := &SchemaCallable{
		name: "Location",
		fields: map[string]*FieldDescriptor{
			"region": testField("string", true, starlark.None, nil),
		},
		order: []string{"region"},
	}

	outer := &SchemaCallable{
		name: "Account",
		fields: map[string]*FieldDescriptor{
			"location": testFieldWithSchema(inner, true),
		},
		order: []string{"location"},
	}

	// Pass a plain dict for the nested schema field.
	plainDict := makeDict(kv("region", starlark.String("westeurope")))
	result, err := callSchema(outer, kv("location", plainDict))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	sd := result.(*SchemaDict)
	locVal, found, _ := sd.Get(starlark.String("location"))
	if !found {
		t.Fatal("location key not found")
	}

	// The nested value should be wrapped in SchemaDict.
	locSD, ok := locVal.(*SchemaDict)
	if !ok {
		t.Fatalf("nested value is %T, want *SchemaDict", locVal)
	}
	if locSD.SchemaName() != "Location" {
		t.Errorf("nested SchemaName() = %q, want %q", locSD.SchemaName(), "Location")
	}

	v, found, _ := locSD.Get(starlark.String("region"))
	if !found {
		t.Fatal("region key not found in nested dict")
	}
	if v.(starlark.String) != "westeurope" {
		t.Errorf("region = %v, want westeurope", v)
	}
}

// SchemaDict value skips re-validation.
func TestConstructorSchemaDictSkipsRevalidation(t *testing.T) {
	inner := &SchemaCallable{
		name: "Location",
		fields: map[string]*FieldDescriptor{
			"region": testField("string", true, starlark.None, nil),
		},
		order: []string{"region"},
	}

	outer := &SchemaCallable{
		name: "Account",
		fields: map[string]*FieldDescriptor{
			"location": testFieldWithSchema(inner, true),
		},
		order: []string{"location"},
	}

	// Pre-validate with inner schema.
	innerResult, err := callSchema(inner, kv("region", starlark.String("eastus")))
	if err != nil {
		t.Fatalf("inner call failed: %v", err)
	}

	// Pass SchemaDict directly — should be stored without re-validation.
	result, err := callSchema(outer, kv("location", innerResult))
	if err != nil {
		t.Fatalf("outer call failed: %v", err)
	}

	sd := result.(*SchemaDict)
	locVal, found, _ := sd.Get(starlark.String("location"))
	if !found {
		t.Fatal("location key not found")
	}

	// Should be the exact same SchemaDict we passed in.
	if locVal != innerResult {
		t.Error("expected SchemaDict to be stored directly without re-validation")
	}
}

// Nested schema field with wrong type (e.g., string) should error.
func TestConstructorNestedSchema(t *testing.T) {
	inner := &SchemaCallable{
		name: "Location",
		fields: map[string]*FieldDescriptor{
			"region": testField("string", true, starlark.None, nil),
		},
		order: []string{"region"},
	}

	outer := &SchemaCallable{
		name: "Account",
		fields: map[string]*FieldDescriptor{
			"location": testFieldWithSchema(inner, true),
		},
		order: []string{"location"},
	}

	_, err := callSchema(outer, kv("location", starlark.String("not-a-dict")))
	if err == nil {
		t.Fatal("expected error for wrong type on schema field")
	}
	errStr := err.Error()
	if !strings.Contains(errStr, "location: expected Location or dict, got string") {
		t.Errorf("error = %v, want contains 'location: expected Location or dict, got string'", errStr)
	}
}

// None on optional schema-typed field applies omission semantics.
func TestConstructorNestedNoneOptional(t *testing.T) {
	inner := &SchemaCallable{
		name: "Location",
		fields: map[string]*FieldDescriptor{
			"region": testField("string", true, starlark.None, nil),
		},
		order: []string{"region"},
	}

	outer := &SchemaCallable{
		name: "Account",
		fields: map[string]*FieldDescriptor{
			"location": testFieldWithSchema(inner, false),
		},
		order: []string{"location"},
	}

	result, err := callSchema(outer, kv("location", starlark.None))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	sd := result.(*SchemaDict)
	_, found, _ := sd.Get(starlark.String("location"))
	if found {
		t.Error("location should be omitted when None on optional schema field")
	}
}

// None on required schema-typed field should error.
func TestConstructorNestedNoneRequired(t *testing.T) {
	inner := &SchemaCallable{
		name: "Location",
		fields: map[string]*FieldDescriptor{
			"region": testField("string", true, starlark.None, nil),
		},
		order: []string{"region"},
	}

	outer := &SchemaCallable{
		name: "Account",
		fields: map[string]*FieldDescriptor{
			"location": testFieldWithSchema(inner, true),
		},
		order: []string{"location"},
	}

	_, err := callSchema(outer, kv("location", starlark.None))
	if err == nil {
		t.Fatal("expected error for None on required schema field")
	}
	if !strings.Contains(err.Error(), "location: required field missing") {
		t.Errorf("error = %v, want contains 'location: required field missing'", err)
	}
}

// List with items schema validates each element.
func TestConstructorListItems(t *testing.T) {
	item := &SchemaCallable{
		name: "Container",
		fields: map[string]*FieldDescriptor{
			"name":  testField("string", true, starlark.None, nil),
			"image": testField("string", true, starlark.None, nil),
		},
		order: []string{"name", "image"},
	}

	outer := &SchemaCallable{
		name: "PodSpec",
		fields: map[string]*FieldDescriptor{
			"containers": testFieldWithItems(item),
		},
		order: []string{"containers"},
	}

	c1 := makeDict(
		kv("name", starlark.String("web")),
		kv("image", starlark.String("nginx")),
	)
	c2 := makeDict(
		kv("name", starlark.String("sidecar")),
		kv("image", starlark.String("envoy")),
	)

	result, err := callSchema(outer, kv("containers", starlark.NewList([]starlark.Value{c1, c2})))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	sd := result.(*SchemaDict)
	listVal, found, _ := sd.Get(starlark.String("containers"))
	if !found {
		t.Fatal("containers not found")
	}

	list, ok := listVal.(*starlark.List)
	if !ok {
		t.Fatalf("containers is %T, want *starlark.List", listVal)
	}
	if list.Len() != 2 {
		t.Fatalf("list len = %d, want 2", list.Len())
	}

	// Each element should be a SchemaDict.
	for i := 0; i < list.Len(); i++ {
		elem, ok := list.Index(i).(*SchemaDict)
		if !ok {
			t.Errorf("element %d is %T, want *SchemaDict", i, list.Index(i))
			continue
		}
		if elem.SchemaName() != "Container" {
			t.Errorf("element %d SchemaName() = %q, want %q", i, elem.SchemaName(), "Container")
		}
	}
}

// Empty list passes validation.
func TestConstructorEmptyListValid(t *testing.T) {
	item := &SchemaCallable{
		name: "Container",
		fields: map[string]*FieldDescriptor{
			"name": testField("string", true, starlark.None, nil),
		},
		order: []string{"name"},
	}

	outer := &SchemaCallable{
		name: "PodSpec",
		fields: map[string]*FieldDescriptor{
			"containers": testFieldWithItems(item),
		},
		order: []string{"containers"},
	}

	result, err := callSchema(outer, kv("containers", starlark.NewList(nil)))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	sd := result.(*SchemaDict)
	listVal, found, _ := sd.Get(starlark.String("containers"))
	if !found {
		t.Fatal("containers not found")
	}

	list := listVal.(*starlark.List)
	if list.Len() != 0 {
		t.Errorf("empty list should have 0 elements, got %d", list.Len())
	}
}

// List with items schema, SchemaDict elements skip re-validation.
func TestConstructorListItemsSchemaDictSkipsRevalidation(t *testing.T) {
	item := &SchemaCallable{
		name: "Container",
		fields: map[string]*FieldDescriptor{
			"name": testField("string", true, starlark.None, nil),
		},
		order: []string{"name"},
	}

	outer := &SchemaCallable{
		name: "PodSpec",
		fields: map[string]*FieldDescriptor{
			"containers": testFieldWithItems(item),
		},
		order: []string{"containers"},
	}

	// Pre-validate element.
	elemResult, err := callSchema(item, kv("name", starlark.String("web")))
	if err != nil {
		t.Fatalf("inner call failed: %v", err)
	}

	result, err := callSchema(outer, kv("containers", starlark.NewList([]starlark.Value{elemResult})))
	if err != nil {
		t.Fatalf("outer call failed: %v", err)
	}

	sd := result.(*SchemaDict)
	listVal, _, _ := sd.Get(starlark.String("containers"))
	list := listVal.(*starlark.List)

	// Should be the same SchemaDict we passed.
	if list.Index(0) != elemResult {
		t.Error("expected SchemaDict element to be stored directly")
	}
}

// Non-list value for list-with-items field should error.
func TestConstructorListItemsNonList(t *testing.T) {
	item := &SchemaCallable{
		name: "Container",
		fields: map[string]*FieldDescriptor{
			"name": testField("string", true, starlark.None, nil),
		},
		order: []string{"name"},
	}

	outer := &SchemaCallable{
		name: "PodSpec",
		fields: map[string]*FieldDescriptor{
			"containers": testFieldWithItems(item),
		},
		order: []string{"containers"},
	}

	_, err := callSchema(outer, kv("containers", starlark.String("not-a-list")))
	if err == nil {
		t.Fatal("expected error for non-list value on list-with-items field")
	}
	if !strings.Contains(err.Error(), "containers: expected list, got string") {
		t.Errorf("error = %v, want contains 'containers: expected list, got string'", err)
	}
}

// List element with wrong type should error with bracket-index path.
func TestListIndexErrorPath(t *testing.T) {
	item := &SchemaCallable{
		name: "Container",
		fields: map[string]*FieldDescriptor{
			"name": testField("string", true, starlark.None, nil),
		},
		order: []string{"name"},
	}

	outer := &SchemaCallable{
		name: "PodSpec",
		fields: map[string]*FieldDescriptor{
			"containers": testFieldWithItems(item),
		},
		order: []string{"containers"},
	}

	// Element is a string, not a dict.
	_, err := callSchema(outer, kv("containers", starlark.NewList([]starlark.Value{starlark.String("not-a-dict")})))
	if err == nil {
		t.Fatal("expected error for wrong type element")
	}
	if !strings.Contains(err.Error(), "containers[0]: expected Container or dict, got string") {
		t.Errorf("error = %v, want contains 'containers[0]: expected Container or dict, got string'", err)
	}
}

// Nested error path: "parent.child" (dot-separated).
func TestNestedErrorPath(t *testing.T) {
	inner := &SchemaCallable{
		name: "Location",
		fields: map[string]*FieldDescriptor{
			"region": testField("string", true, starlark.None, nil),
		},
		order: []string{"region"},
	}

	outer := &SchemaCallable{
		name: "Account",
		fields: map[string]*FieldDescriptor{
			"location": testFieldWithSchema(inner, true),
		},
		order: []string{"location"},
	}

	// Pass a plain dict missing the required field.
	plainDict := makeDict() // empty dict, missing required "region"
	_, err := callSchema(outer, kv("location", plainDict))
	if err == nil {
		t.Fatal("expected error for missing required nested field")
	}
	errStr := err.Error()
	// Error should have full dot-path.
	if !strings.Contains(errStr, "location.region: required field missing") {
		t.Errorf("error = %v, want contains 'location.region: required field missing'", errStr)
	}
	// Summary should name the outermost schema.
	if !strings.Contains(errStr, "Account: 1 validation error") {
		t.Errorf("error = %v, want contains 'Account: 1 validation error'", errStr)
	}
}

// Did-you-mean at nested level.
func TestNestedDidYouMean(t *testing.T) {
	inner := &SchemaCallable{
		name: "Container",
		fields: map[string]*FieldDescriptor{
			"name":  testField("string", true, starlark.None, nil),
			"image": testField("string", true, starlark.None, nil),
		},
		order: []string{"name", "image"},
	}

	outer := &SchemaCallable{
		name: "PodSpec",
		fields: map[string]*FieldDescriptor{
			"containers": testFieldWithItems(inner),
		},
		order: []string{"containers"},
	}

	// Typo "nme" instead of "name" in a list element.
	badDict := makeDict(
		kv("nme", starlark.String("web")),
		kv("image", starlark.String("nginx")),
	)
	_, err := callSchema(outer, kv("containers", starlark.NewList([]starlark.Value{badDict})))
	if err == nil {
		t.Fatal("expected error for typo in nested field")
	}
	errStr := err.Error()
	if !strings.Contains(errStr, `containers[0].nme: unknown field (did you mean "name"?)`) {
		t.Errorf("error = %v, want contains 'containers[0].nme: unknown field (did you mean \"name\"?)'", errStr)
	}
}

// Multiple nested errors roll up to outermost schema.
func TestNestedErrorRollup(t *testing.T) {
	container := &SchemaCallable{
		name: "Container",
		fields: map[string]*FieldDescriptor{
			"name":  testField("string", true, starlark.None, nil),
			"image": testField("string", true, starlark.None, nil),
		},
		order: []string{"name", "image"},
	}

	spec := &SchemaCallable{
		name: "PodSpec",
		fields: map[string]*FieldDescriptor{
			"containers": testFieldWithItems(container),
		},
		order: []string{"containers"},
	}

	template := &SchemaCallable{
		name: "Template",
		fields: map[string]*FieldDescriptor{
			"spec": testFieldWithSchema(spec, true),
		},
		order: []string{"spec"},
	}

	deployment := &SchemaCallable{
		name: "DeploymentSpec",
		fields: map[string]*FieldDescriptor{
			"template": testFieldWithSchema(template, true),
		},
		order: []string{"template"},
	}

	// Container 0: typo "nme" instead of "name".
	// Container 1: missing "image".
	c0 := makeDict(
		kv("nme", starlark.String("web")),
		kv("image", starlark.String("nginx")),
	)
	c1 := makeDict(
		kv("name", starlark.String("sidecar")),
		// missing "image"
	)

	specDict := makeDict(
		kv("containers", starlark.NewList([]starlark.Value{c0, c1})),
	)
	templateDict := makeDict(kv("spec", specDict))

	_, err := callSchema(deployment, kv("template", templateDict))
	if err == nil {
		t.Fatal("expected error for nested validation failures")
	}
	errStr := err.Error()

	// Summary names outermost schema.
	if !strings.Contains(errStr, "DeploymentSpec:") {
		t.Errorf("error should name DeploymentSpec: %s", errStr)
	}

	// Error from container 0's typo with full path.
	if !strings.Contains(errStr, `template.spec.containers[0].nme: unknown field (did you mean "name"?)`) {
		t.Errorf("error should contain full path for container[0] typo: %s", errStr)
	}

	// Error from container 1's missing required field.
	if !strings.Contains(errStr, "template.spec.containers[1].image: required field missing") {
		t.Errorf("error should contain full path for container[1] missing image: %s", errStr)
	}

	// Should have at least 2 errors (could be more if "nme" as unknown causes both unknown + missing "name").
	if !strings.Contains(errStr, "validation error") {
		t.Errorf("error should mention validation errors: %s", errStr)
	}
}

// Deeply nested path format test.
func TestDeeplyNestedErrorPath(t *testing.T) {
	inner := &SchemaCallable{
		name: "Env",
		fields: map[string]*FieldDescriptor{
			"value": testField("string", true, starlark.None, nil),
		},
		order: []string{"value"},
	}

	container := &SchemaCallable{
		name: "Container",
		fields: map[string]*FieldDescriptor{
			"name": testField("string", true, starlark.None, nil),
			"envs": testFieldWithItems(inner),
		},
		order: []string{"name", "envs"},
	}

	spec := &SchemaCallable{
		name: "PodSpec",
		fields: map[string]*FieldDescriptor{
			"containers": testFieldWithItems(container),
		},
		order: []string{"containers"},
	}

	// Container with env missing required value.
	envDict := makeDict() // missing "value"
	c := makeDict(
		kv("name", starlark.String("web")),
		kv("envs", starlark.NewList([]starlark.Value{envDict})),
	)

	_, err := callSchema(spec, kv("containers", starlark.NewList([]starlark.Value{c})))
	if err == nil {
		t.Fatal("expected error for deeply nested validation failure")
	}
	errStr := err.Error()
	// Path should be: containers[0].envs[0].value
	if !strings.Contains(errStr, "containers[0].envs[0].value: required field missing") {
		t.Errorf("error = %v, want contains 'containers[0].envs[0].value: required field missing'", errStr)
	}
}

// Existing flat behavior unchanged (backward compat).
func TestConstructorFlatSchemaUnchanged(t *testing.T) {
	s := &SchemaCallable{
		name: "Account",
		fields: map[string]*FieldDescriptor{
			"location": testField("string", true, starlark.None, nil),
			"sku":      testField("string", false, starlark.String("Standard_LRS"), nil),
		},
		order: []string{"location", "sku"},
	}

	result, err := callSchema(s, kv("location", starlark.String("westeurope")))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should return SchemaDict.
	sd, ok := result.(*SchemaDict)
	if !ok {
		t.Fatalf("result is %T, want *SchemaDict", result)
	}

	// Type() should still be "dict" for backward compat.
	if sd.Type() != "dict" {
		t.Errorf("Type() = %q, want %q", sd.Type(), "dict")
	}

	// Values should be accessible.
	v, found, _ := sd.Get(starlark.String("location"))
	if !found || v.(starlark.String) != "westeurope" {
		t.Errorf("location = %v, want westeurope", v)
	}
	v, found, _ = sd.Get(starlark.String("sku"))
	if !found || v.(starlark.String) != "Standard_LRS" {
		t.Errorf("sku = %v, want Standard_LRS (default)", v)
	}
}
