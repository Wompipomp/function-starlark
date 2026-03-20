package schema

import (
	"strings"
	"testing"

	"go.starlark.net/starlark"
)

// helper: call FieldBuiltin with given kwargs, return result or error.
func callField(t *testing.T, kwargs []starlark.Tuple) (*FieldDescriptor, error) {
	t.Helper()
	thread := &starlark.Thread{Name: "test"}
	builtin := FieldBuiltin()
	val, err := starlark.Call(thread, builtin, nil, kwargs)
	if err != nil {
		return nil, err
	}
	fd, ok := val.(*FieldDescriptor)
	if !ok {
		t.Fatalf("expected *FieldDescriptor, got %T", val)
	}
	return fd, nil
}

func kwargs(pairs ...any) []starlark.Tuple {
	var out []starlark.Tuple
	for i := 0; i < len(pairs); i += 2 {
		key := starlark.String(pairs[i].(string))
		out = append(out, starlark.Tuple{key, pairs[i+1].(starlark.Value)})
	}
	return out
}

func TestFieldCreationBasic(t *testing.T) {
	fd, err := callField(t, kwargs("type", starlark.String("string")))
	if err != nil {
		t.Fatal(err)
	}
	if fd.typeName != "string" {
		t.Errorf("typeName = %q, want %q", fd.typeName, "string")
	}
	if fd.required {
		t.Error("required should be false by default")
	}
	if fd.defVal != starlark.None {
		t.Errorf("defVal = %v, want None", fd.defVal)
	}
	if fd.enum != nil {
		t.Error("enum should be nil by default")
	}
	if fd.doc != "" {
		t.Errorf("doc = %q, want empty", fd.doc)
	}
}

func TestFieldCreationRequired(t *testing.T) {
	fd, err := callField(t, kwargs("type", starlark.String("int"), "required", starlark.True))
	if err != nil {
		t.Fatal(err)
	}
	if !fd.required {
		t.Error("required should be true")
	}
}

func TestFieldCreationDefault(t *testing.T) {
	fd, err := callField(t, kwargs("type", starlark.String("string"), "default", starlark.String("westeurope")))
	if err != nil {
		t.Fatal(err)
	}
	if fd.defVal != starlark.String("westeurope") {
		t.Errorf("defVal = %v, want westeurope", fd.defVal)
	}
}

func TestFieldCreationEnum(t *testing.T) {
	enumList := starlark.NewList([]starlark.Value{starlark.String("a"), starlark.String("b")})
	fd, err := callField(t, kwargs("type", starlark.String("string"), "enum", enumList))
	if err != nil {
		t.Fatal(err)
	}
	if fd.enum == nil {
		t.Fatal("enum should not be nil")
	}
	if fd.enum.Len() != 2 {
		t.Errorf("enum.Len() = %d, want 2", fd.enum.Len())
	}
}

func TestFieldCreationDoc(t *testing.T) {
	fd, err := callField(t, kwargs("doc", starlark.String("A description")))
	if err != nil {
		t.Fatal(err)
	}
	if fd.doc != "A description" {
		t.Errorf("doc = %q, want %q", fd.doc, "A description")
	}
}

func TestFieldRequiredAndDefaultMutuallyExclusive(t *testing.T) {
	_, err := callField(t, kwargs("required", starlark.True, "default", starlark.String("x")))
	if err == nil {
		t.Fatal("expected error for required + default")
	}
	want := "field: required and default are mutually exclusive"
	if !strings.Contains(err.Error(), want) {
		t.Errorf("error = %q, want containing %q", err.Error(), want)
	}
}

func TestFieldInvalidType(t *testing.T) {
	_, err := callField(t, kwargs("type", starlark.String("invalid")))
	if err == nil {
		t.Fatal("expected error for invalid type")
	}
	if !strings.Contains(err.Error(), "invalid type") {
		t.Errorf("error = %q, want containing 'invalid type'", err.Error())
	}
	// Should list valid types
	for _, vt := range []string{"bool", "dict", "float", "int", "list", "string"} {
		if !strings.Contains(err.Error(), vt) {
			t.Errorf("error should mention valid type %q, got: %s", vt, err.Error())
		}
	}
}

func TestFieldNoArgs(t *testing.T) {
	fd, err := callField(t, nil)
	if err != nil {
		t.Fatal(err)
	}
	if fd.typeName != "" {
		t.Errorf("typeName = %q, want empty", fd.typeName)
	}
}

func TestFieldValidTypes(t *testing.T) {
	for _, typeName := range []string{"", "string", "int", "float", "bool", "list", "dict"} {
		var kw []starlark.Tuple
		if typeName != "" {
			kw = kwargs("type", starlark.String(typeName))
		}
		fd, err := callField(t, kw)
		if err != nil {
			t.Errorf("field(type=%q) error: %v", typeName, err)
			continue
		}
		if fd.typeName != typeName {
			t.Errorf("field(type=%q).typeName = %q", typeName, fd.typeName)
		}
	}
}

func TestFieldEnumNotList(t *testing.T) {
	_, err := callField(t, kwargs("enum", starlark.String("not-a-list")))
	if err == nil {
		t.Fatal("expected error for non-list enum")
	}
	if !strings.Contains(err.Error(), "enum must be a list") {
		t.Errorf("error = %q, want containing 'enum must be a list'", err.Error())
	}
}

// --- String/repr tests ---

func TestFieldStringRequired(t *testing.T) {
	fd, err := callField(t, kwargs("type", starlark.String("string"), "required", starlark.True))
	if err != nil {
		t.Fatal(err)
	}
	got := fd.String()
	want := "<field type=string required>"
	if got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

func TestFieldStringDefault(t *testing.T) {
	fd, err := callField(t, kwargs("type", starlark.String("int"), "default", starlark.MakeInt(0)))
	if err != nil {
		t.Fatal(err)
	}
	got := fd.String()
	want := "<field type=int default=0>"
	if got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

func TestFieldStringEmpty(t *testing.T) {
	fd, err := callField(t, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := fd.String()
	want := "<field>"
	if got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

func TestFieldStringEnum(t *testing.T) {
	enumList := starlark.NewList([]starlark.Value{starlark.String("a"), starlark.String("b")})
	fd, err := callField(t, kwargs("type", starlark.String("string"), "enum", enumList))
	if err != nil {
		t.Fatal(err)
	}
	got := fd.String()
	want := `<field type=string enum=["a", "b"]>`
	if got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

// --- Type() test ---

func TestFieldType(t *testing.T) {
	fd, err := callField(t, nil)
	if err != nil {
		t.Fatal(err)
	}
	if fd.Type() != "field" {
		t.Errorf("Type() = %q, want %q", fd.Type(), "field")
	}
}

// --- Attr tests ---

func TestFieldAttrType(t *testing.T) {
	fd, err := callField(t, kwargs("type", starlark.String("string")))
	if err != nil {
		t.Fatal(err)
	}
	val, attrErr := fd.Attr("type")
	if attrErr != nil {
		t.Fatal(attrErr)
	}
	if val != starlark.String("string") {
		t.Errorf("Attr(type) = %v, want string", val)
	}
}

func TestFieldAttrRequired(t *testing.T) {
	fd, err := callField(t, kwargs("required", starlark.True))
	if err != nil {
		t.Fatal(err)
	}
	val, attrErr := fd.Attr("required")
	if attrErr != nil {
		t.Fatal(attrErr)
	}
	if val != starlark.True {
		t.Errorf("Attr(required) = %v, want True", val)
	}
}

func TestFieldAttrDefault(t *testing.T) {
	fd, err := callField(t, kwargs("default", starlark.String("val")))
	if err != nil {
		t.Fatal(err)
	}
	val, attrErr := fd.Attr("default")
	if attrErr != nil {
		t.Fatal(attrErr)
	}
	if val != starlark.String("val") {
		t.Errorf("Attr(default) = %v, want val", val)
	}
}

func TestFieldAttrEnum(t *testing.T) {
	enumList := starlark.NewList([]starlark.Value{starlark.String("x")})
	fd, err := callField(t, kwargs("enum", enumList))
	if err != nil {
		t.Fatal(err)
	}
	val, attrErr := fd.Attr("enum")
	if attrErr != nil {
		t.Fatal(attrErr)
	}
	list, ok := val.(*starlark.List)
	if !ok {
		t.Fatalf("Attr(enum) type = %T, want *starlark.List", val)
	}
	if list.Len() != 1 {
		t.Errorf("Attr(enum).Len() = %d, want 1", list.Len())
	}
}

func TestFieldAttrEnumNone(t *testing.T) {
	fd, err := callField(t, nil)
	if err != nil {
		t.Fatal(err)
	}
	val, attrErr := fd.Attr("enum")
	if attrErr != nil {
		t.Fatal(attrErr)
	}
	if val != starlark.None {
		t.Errorf("Attr(enum) = %v, want None", val)
	}
}

func TestFieldAttrDoc(t *testing.T) {
	fd, err := callField(t, kwargs("doc", starlark.String("help")))
	if err != nil {
		t.Fatal(err)
	}
	val, attrErr := fd.Attr("doc")
	if attrErr != nil {
		t.Fatal(attrErr)
	}
	if val != starlark.String("help") {
		t.Errorf("Attr(doc) = %v, want help", val)
	}
}

func TestFieldAttrUnknown(t *testing.T) {
	fd, err := callField(t, nil)
	if err != nil {
		t.Fatal(err)
	}
	val, attrErr := fd.Attr("nonexistent")
	if attrErr != nil {
		t.Fatal(attrErr)
	}
	if val != nil {
		t.Errorf("Attr(nonexistent) = %v, want nil", val)
	}
}

func TestFieldAttrNames(t *testing.T) {
	fd, err := callField(t, nil)
	if err != nil {
		t.Fatal(err)
	}
	names := fd.AttrNames()
	want := []string{"default", "doc", "enum", "required", "type"}
	if len(names) != len(want) {
		t.Fatalf("AttrNames() len = %d, want %d", len(names), len(want))
	}
	for i, n := range names {
		if n != want[i] {
			t.Errorf("AttrNames()[%d] = %q, want %q", i, n, want[i])
		}
	}
}

// --- Freeze tests ---

func TestFieldFreeze(t *testing.T) {
	defVal := starlark.NewList([]starlark.Value{starlark.MakeInt(1)})
	enumList := starlark.NewList([]starlark.Value{starlark.String("a")})
	fd, err := callField(t, kwargs("default", defVal, "enum", enumList))
	if err != nil {
		t.Fatal(err)
	}
	fd.Freeze()

	// After freezing, default and enum lists should be frozen
	if err := defVal.Append(starlark.MakeInt(2)); err == nil {
		t.Error("expected error appending to frozen default list")
	}
	if err := enumList.Append(starlark.String("b")); err == nil {
		t.Error("expected error appending to frozen enum list")
	}
}

// --- Truth and Hash tests ---

func TestFieldTruth(t *testing.T) {
	fd, err := callField(t, nil)
	if err != nil {
		t.Fatal(err)
	}
	if fd.Truth() != starlark.True {
		t.Error("Truth() should be True")
	}
}

func TestFieldHash(t *testing.T) {
	fd, err := callField(t, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, hashErr := fd.Hash()
	if hashErr == nil {
		t.Error("expected error from Hash()")
	}
	if !strings.Contains(hashErr.Error(), "unhashable") {
		t.Errorf("Hash() error = %q, want containing 'unhashable'", hashErr.Error())
	}
}

func TestFieldFreezeIdempotent(t *testing.T) {
	defVal := starlark.NewList([]starlark.Value{starlark.MakeInt(1)})
	fd, err := callField(t, kwargs("default", defVal))
	if err != nil {
		t.Fatal(err)
	}
	fd.Freeze()
	fd.Freeze() // second freeze should be a no-op

	// Verify default is still frozen and accessible.
	v, attrErr := fd.Attr("default")
	if attrErr != nil {
		t.Fatal(attrErr)
	}
	list, ok := v.(*starlark.List)
	if !ok {
		t.Fatalf("default is %T, want *starlark.List", v)
	}
	if list.Len() != 1 {
		t.Errorf("default.Len() = %d, want 1", list.Len())
	}
}

func TestFieldCreationAllParams(t *testing.T) {
	enumList := starlark.NewList([]starlark.Value{starlark.String("a"), starlark.String("b")})
	fd, err := callField(t, kwargs(
		"type", starlark.String("string"),
		"enum", enumList,
		"doc", starlark.String("A combined field"),
	))
	if err != nil {
		t.Fatal(err)
	}
	if fd.typeName != "string" {
		t.Errorf("typeName = %q, want %q", fd.typeName, "string")
	}
	if fd.enum == nil || fd.enum.Len() != 2 {
		t.Errorf("enum.Len() = %v, want 2", fd.enum)
	}
	if fd.doc != "A combined field" {
		t.Errorf("doc = %q, want %q", fd.doc, "A combined field")
	}
}

// --- typeParam / nested schema tests ---

func testSchema(name string, fieldNames ...string) *SchemaCallable {
	fields := make(map[string]*FieldDescriptor)
	var order []string
	for _, n := range fieldNames {
		fields[n] = &FieldDescriptor{typeName: "string"}
		order = append(order, n)
	}
	return &SchemaCallable{name: name, fields: fields, order: order}
}

func TestFieldTypeSchemaCallable(t *testing.T) {
	sub := testSchema("Account", "location")
	fd, err := callField(t, kwargs("type", sub))
	if err != nil {
		t.Fatal(err)
	}
	if fd.schema != sub {
		t.Error("schema should reference the SubSchema")
	}
	if fd.typeName != "" {
		t.Errorf("typeName should be empty for schema-typed field, got %q", fd.typeName)
	}
}

func TestFieldTypeStringStillWorks(t *testing.T) {
	fd, err := callField(t, kwargs("type", starlark.String("string")))
	if err != nil {
		t.Fatal(err)
	}
	if fd.typeName != "string" {
		t.Errorf("typeName = %q, want %q", fd.typeName, "string")
	}
	if fd.schema != nil {
		t.Error("schema should be nil for string-typed field")
	}
}

func TestFieldTypeInvalidValue(t *testing.T) {
	_, err := callField(t, kwargs("type", starlark.MakeInt(123)))
	if err == nil {
		t.Fatal("expected error for type=123")
	}
	if !strings.Contains(err.Error(), "type= must be a string or schema, got int") {
		t.Errorf("error = %q, want containing 'type= must be a string or schema, got int'", err.Error())
	}
}

func TestFieldTypeListWithItems(t *testing.T) {
	sub := testSchema("Container", "name", "image")
	fd, err := callField(t, kwargs("type", starlark.String("list"), "items", sub))
	if err != nil {
		t.Fatal(err)
	}
	if fd.typeName != "list" {
		t.Errorf("typeName = %q, want %q", fd.typeName, "list")
	}
	if fd.items != sub {
		t.Error("items should reference the SubSchema")
	}
}

func TestFieldItemsWithoutListType(t *testing.T) {
	sub := testSchema("Item", "name")
	_, err := callField(t, kwargs("items", sub))
	if err == nil {
		t.Fatal("expected error for items= without type=list")
	}
	want := `items= is only valid when type="list"`
	if !strings.Contains(err.Error(), want) {
		t.Errorf("error = %q, want containing %q", err.Error(), want)
	}
}

func TestFieldItemsWithStringType(t *testing.T) {
	sub := testSchema("Item", "name")
	_, err := callField(t, kwargs("type", starlark.String("string"), "items", sub))
	if err == nil {
		t.Fatal("expected error for items= with type=string")
	}
	want := `items= is only valid when type="list"`
	if !strings.Contains(err.Error(), want) {
		t.Errorf("error = %q, want containing %q", err.Error(), want)
	}
}

func TestFieldItemsWithSchemaType(t *testing.T) {
	sub := testSchema("SubA", "a")
	sub2 := testSchema("SubB", "b")
	_, err := callField(t, kwargs("type", sub, "items", sub2))
	if err == nil {
		t.Fatal("expected error for items= with type=SubSchema")
	}
	want := `items= is only valid when type="list"`
	if !strings.Contains(err.Error(), want) {
		t.Errorf("error = %q, want containing %q", err.Error(), want)
	}
}

func TestFieldItemsNotSchema(t *testing.T) {
	_, err := callField(t, kwargs("type", starlark.String("list"), "items", starlark.String("not-a-schema")))
	if err == nil {
		t.Fatal("expected error for items=non-schema")
	}
	if !strings.Contains(err.Error(), "items= must be a schema, got string") {
		t.Errorf("error = %q, want containing 'items= must be a schema, got string'", err.Error())
	}
}

func TestFieldStringSchemaType(t *testing.T) {
	sub := testSchema("Account", "location")
	fd, err := callField(t, kwargs("type", sub))
	if err != nil {
		t.Fatal(err)
	}
	got := fd.String()
	want := "<field type=Account>"
	if got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

func TestFieldStringListWithItems(t *testing.T) {
	sub := testSchema("Container", "name")
	fd, err := callField(t, kwargs("type", starlark.String("list"), "items", sub))
	if err != nil {
		t.Fatal(err)
	}
	got := fd.String()
	want := "<field type=list items=Container>"
	if got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

func TestFieldAttrTypeReturnsSchemaName(t *testing.T) {
	sub := testSchema("Account", "location")
	fd, err := callField(t, kwargs("type", sub))
	if err != nil {
		t.Fatal(err)
	}
	val, attrErr := fd.Attr("type")
	if attrErr != nil {
		t.Fatal(attrErr)
	}
	if val != starlark.String("Account") {
		t.Errorf("Attr(type) = %v, want Account", val)
	}
}
