package schema

import (
	"strings"
	"testing"

	"go.starlark.net/starlark"
	"go.starlark.net/syntax"
)

func newTestSchemaDict(name string, entries ...any) *SchemaDict {
	d := starlark.NewDict(len(entries) / 2)
	for i := 0; i < len(entries); i += 2 {
		_ = d.SetKey(starlark.String(entries[i].(string)), entries[i+1].(starlark.Value))
	}
	return NewSchemaDict(name, d)
}

func TestSchemaDictString(t *testing.T) {
	sd := newTestSchemaDict("Account", "location", starlark.String("westeurope"))
	got := sd.String()
	want := `Account({"location": "westeurope"})`
	if got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

func TestSchemaDictStringEmpty(t *testing.T) {
	sd := newTestSchemaDict("Empty")
	got := sd.String()
	want := "Empty({})"
	if got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

func TestSchemaDictType(t *testing.T) {
	sd := newTestSchemaDict("X")
	if sd.Type() != "dict" {
		t.Errorf("Type() = %q, want %q", sd.Type(), "dict")
	}
}

func TestSchemaDictFreeze(t *testing.T) {
	sd := newTestSchemaDict("Frozen", "k", starlark.String("v"))
	sd.Freeze()
	err := sd.SetKey(starlark.String("new"), starlark.String("val"))
	if err == nil {
		t.Error("expected error setting key on frozen SchemaDict")
	}
}

func TestSchemaDictTruthNonEmpty(t *testing.T) {
	sd := newTestSchemaDict("T", "k", starlark.String("v"))
	if sd.Truth() != starlark.True {
		t.Error("Truth() should be True for non-empty")
	}
}

func TestSchemaDictTruthEmpty(t *testing.T) {
	sd := newTestSchemaDict("T")
	if sd.Truth() != starlark.False {
		t.Error("Truth() should be False for empty")
	}
}

func TestSchemaDictHash(t *testing.T) {
	sd := newTestSchemaDict("H")
	_, err := sd.Hash()
	if err == nil {
		t.Error("expected error from Hash()")
	}
	if !strings.Contains(err.Error(), "unhashable type: dict") {
		t.Errorf("Hash() error = %q, want 'unhashable type: dict'", err.Error())
	}
}

func TestSchemaDictInternalDict(t *testing.T) {
	d := starlark.NewDict(0)
	_ = d.SetKey(starlark.String("a"), starlark.MakeInt(1))
	sd := NewSchemaDict("Test", d)
	if sd.InternalDict() != d {
		t.Error("InternalDict() should return the same *starlark.Dict")
	}
}

func TestSchemaDictSchemaName(t *testing.T) {
	sd := newTestSchemaDict("MySchema")
	if sd.SchemaName() != "MySchema" {
		t.Errorf("SchemaName() = %q, want %q", sd.SchemaName(), "MySchema")
	}
}

func TestSchemaDictGet(t *testing.T) {
	sd := newTestSchemaDict("M", "key", starlark.String("val"))
	v, found, err := sd.Get(starlark.String("key"))
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Error("expected key to be found")
	}
	if v != starlark.String("val") {
		t.Errorf("Get(key) = %v, want val", v)
	}
}

func TestSchemaDictGetMissing(t *testing.T) {
	sd := newTestSchemaDict("M")
	_, found, err := sd.Get(starlark.String("missing"))
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Error("expected key not to be found")
	}
}

func TestSchemaDictSetKey(t *testing.T) {
	sd := newTestSchemaDict("S")
	err := sd.SetKey(starlark.String("a"), starlark.MakeInt(1))
	if err != nil {
		t.Fatal(err)
	}
	v, found, _ := sd.Get(starlark.String("a"))
	if !found || v != starlark.MakeInt(1) {
		t.Errorf("after SetKey, Get(a) = %v/%v, want 1/true", v, found)
	}
}

func TestSchemaDictIterate(t *testing.T) {
	sd := newTestSchemaDict("I", "a", starlark.MakeInt(1), "b", starlark.MakeInt(2))
	iter := sd.Iterate()
	defer iter.Done()
	var keys []string
	var k starlark.Value
	for iter.Next(&k) {
		keys = append(keys, string(k.(starlark.String)))
	}
	if len(keys) != 2 {
		t.Errorf("Iterate returned %d keys, want 2", len(keys))
	}
}

func TestSchemaDictSetField(t *testing.T) {
	sd := newTestSchemaDict("SF")
	err := sd.SetField("mykey", starlark.String("myval"))
	if err != nil {
		t.Fatal(err)
	}
	v, found, _ := sd.Get(starlark.String("mykey"))
	if !found || v != starlark.String("myval") {
		t.Errorf("after SetField, Get = %v/%v", v, found)
	}
}

func TestSchemaDictAttrDataKey(t *testing.T) {
	sd := newTestSchemaDict("A", "location", starlark.String("westeurope"))
	val, err := sd.Attr("location")
	if err != nil {
		t.Fatal(err)
	}
	if val != starlark.String("westeurope") {
		t.Errorf("Attr(location) = %v, want westeurope", val)
	}
}

func TestSchemaDictAttrMissingKey(t *testing.T) {
	sd := newTestSchemaDict("A")
	val, err := sd.Attr("missing")
	if err != nil {
		t.Fatal(err)
	}
	if val != starlark.None {
		t.Errorf("Attr(missing) = %v, want None", val)
	}
}

func TestSchemaDictAttrNames(t *testing.T) {
	sd := newTestSchemaDict("AN", "b", starlark.MakeInt(1), "a", starlark.MakeInt(2))
	names := sd.AttrNames()
	// Should include data keys + builtin method names, sorted
	if len(names) < 2 {
		t.Fatalf("AttrNames() returned %d names, want >= 2", len(names))
	}
	// Verify data keys are present
	nameSet := make(map[string]bool)
	for _, n := range names {
		nameSet[n] = true
	}
	if !nameSet["a"] || !nameSet["b"] {
		t.Errorf("AttrNames() missing data keys, got %v", names)
	}
	// Verify builtin methods are present
	for _, method := range []string{"keys", "values", "items", "get"} {
		if !nameSet[method] {
			t.Errorf("AttrNames() missing builtin method %q, got %v", method, names)
		}
	}
}

func TestSchemaDictBuiltinKeys(t *testing.T) {
	sd := newTestSchemaDict("BK", "x", starlark.MakeInt(1), "y", starlark.MakeInt(2))
	val, err := sd.Attr("keys")
	if err != nil {
		t.Fatal(err)
	}
	b, ok := val.(*starlark.Builtin)
	if !ok {
		t.Fatalf("Attr(keys) type = %T, want *starlark.Builtin", val)
	}
	thread := &starlark.Thread{Name: "test"}
	result, err := starlark.Call(thread, b, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	list, ok := result.(*starlark.List)
	if !ok {
		t.Fatalf("keys() type = %T, want *starlark.List", result)
	}
	if list.Len() != 2 {
		t.Errorf("keys().Len() = %d, want 2", list.Len())
	}
}

func TestSchemaDictBuiltinValues(t *testing.T) {
	sd := newTestSchemaDict("BV", "x", starlark.MakeInt(10))
	val, err := sd.Attr("values")
	if err != nil {
		t.Fatal(err)
	}
	b, ok := val.(*starlark.Builtin)
	if !ok {
		t.Fatalf("Attr(values) type = %T, want *starlark.Builtin", val)
	}
	thread := &starlark.Thread{Name: "test"}
	result, err := starlark.Call(thread, b, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	list, ok := result.(*starlark.List)
	if !ok {
		t.Fatalf("values() type = %T, want *starlark.List", result)
	}
	if list.Len() != 1 {
		t.Errorf("values().Len() = %d, want 1", list.Len())
	}
}

func TestSchemaDictBuiltinItems(t *testing.T) {
	sd := newTestSchemaDict("BI", "k", starlark.String("v"))
	val, err := sd.Attr("items")
	if err != nil {
		t.Fatal(err)
	}
	b, ok := val.(*starlark.Builtin)
	if !ok {
		t.Fatalf("Attr(items) type = %T, want *starlark.Builtin", val)
	}
	thread := &starlark.Thread{Name: "test"}
	result, err := starlark.Call(thread, b, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	list, ok := result.(*starlark.List)
	if !ok {
		t.Fatalf("items() type = %T, want *starlark.List", result)
	}
	if list.Len() != 1 {
		t.Errorf("items().Len() = %d, want 1", list.Len())
	}
}

func TestSchemaDictBuiltinGet(t *testing.T) {
	sd := newTestSchemaDict("BG", "k", starlark.String("v"))
	val, err := sd.Attr("get")
	if err != nil {
		t.Fatal(err)
	}
	b, ok := val.(*starlark.Builtin)
	if !ok {
		t.Fatalf("Attr(get) type = %T, want *starlark.Builtin", val)
	}
	thread := &starlark.Thread{Name: "test"}
	result, err := starlark.Call(thread, b, starlark.Tuple{starlark.String("k")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result != starlark.String("v") {
		t.Errorf("get(k) = %v, want v", result)
	}

	// Test get with default for missing key
	result, err = starlark.Call(thread, b, starlark.Tuple{starlark.String("missing"), starlark.String("default")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result != starlark.String("default") {
		t.Errorf("get(missing, default) = %v, want default", result)
	}
}

func TestSchemaDictCompareSameType(t *testing.T) {
	sd1 := newTestSchemaDict("C", "a", starlark.MakeInt(1))
	sd2 := newTestSchemaDict("C", "a", starlark.MakeInt(1))
	ok, err := sd1.CompareSameType(syntax.EQL, sd2, 10)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Error("CompareSameType(==) should be true for equal dicts")
	}
}

func TestSchemaDictBuiltinPop(t *testing.T) {
	sd := newTestSchemaDict("P", "k", starlark.String("v"))
	val, err := sd.Attr("pop")
	if err != nil {
		t.Fatal(err)
	}
	b := val.(*starlark.Builtin)
	thread := &starlark.Thread{Name: "test"}
	result, err := starlark.Call(thread, b, starlark.Tuple{starlark.String("k")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result != starlark.String("v") {
		t.Errorf("pop(k) = %v, want v", result)
	}
	// Key should be removed.
	_, found, _ := sd.Get(starlark.String("k"))
	if found {
		t.Error("key should be removed after pop")
	}
}

func TestSchemaDictBuiltinPopMissing(t *testing.T) {
	sd := newTestSchemaDict("P")
	val, err := sd.Attr("pop")
	if err != nil {
		t.Fatal(err)
	}
	b := val.(*starlark.Builtin)
	thread := &starlark.Thread{Name: "test"}
	result, err := starlark.Call(thread, b, starlark.Tuple{starlark.String("missing"), starlark.String("fallback")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result != starlark.String("fallback") {
		t.Errorf("pop(missing, fallback) = %v, want fallback", result)
	}
}

func TestSchemaDictBuiltinPopMissingNoDefault(t *testing.T) {
	sd := newTestSchemaDict("P")
	val, err := sd.Attr("pop")
	if err != nil {
		t.Fatal(err)
	}
	b := val.(*starlark.Builtin)
	thread := &starlark.Thread{Name: "test"}
	_, err = starlark.Call(thread, b, starlark.Tuple{starlark.String("missing")}, nil)
	if err == nil {
		t.Fatal("expected error for pop missing key without default")
	}
	if !strings.Contains(err.Error(), "key") {
		t.Errorf("error = %q, want containing 'key'", err.Error())
	}
}

func TestSchemaDictBuiltinUpdate(t *testing.T) {
	sd := newTestSchemaDict("U", "a", starlark.MakeInt(1))
	val, err := sd.Attr("update")
	if err != nil {
		t.Fatal(err)
	}
	b := val.(*starlark.Builtin)
	thread := &starlark.Thread{Name: "test"}

	other := starlark.NewDict(1)
	_ = other.SetKey(starlark.String("b"), starlark.MakeInt(2))

	_, err = starlark.Call(thread, b, starlark.Tuple{other}, nil)
	if err != nil {
		t.Fatal(err)
	}

	v, found, _ := sd.Get(starlark.String("b"))
	if !found || v != starlark.MakeInt(2) {
		t.Errorf("after update, Get(b) = %v/%v, want 2/true", v, found)
	}
	// Original key preserved.
	v, found, _ = sd.Get(starlark.String("a"))
	if !found || v != starlark.MakeInt(1) {
		t.Errorf("after update, Get(a) = %v/%v, want 1/true", v, found)
	}
}

func TestSchemaDictBuiltinUpdateSchemaDict(t *testing.T) {
	sd := newTestSchemaDict("U", "a", starlark.MakeInt(1))
	val, err := sd.Attr("update")
	if err != nil {
		t.Fatal(err)
	}
	b := val.(*starlark.Builtin)
	thread := &starlark.Thread{Name: "test"}

	other := newTestSchemaDict("Source", "c", starlark.MakeInt(3))
	_, err = starlark.Call(thread, b, starlark.Tuple{other}, nil)
	if err != nil {
		t.Fatal(err)
	}

	v, found, _ := sd.Get(starlark.String("c"))
	if !found || v != starlark.MakeInt(3) {
		t.Errorf("after update from SchemaDict, Get(c) = %v/%v, want 3/true", v, found)
	}
}

func TestSchemaDictBuiltinClear(t *testing.T) {
	sd := newTestSchemaDict("C", "a", starlark.MakeInt(1), "b", starlark.MakeInt(2))
	val, err := sd.Attr("clear")
	if err != nil {
		t.Fatal(err)
	}
	b := val.(*starlark.Builtin)
	thread := &starlark.Thread{Name: "test"}
	_, err = starlark.Call(thread, b, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if sd.Len() != 0 {
		t.Errorf("Len() after clear = %d, want 0", sd.Len())
	}
}

func TestSchemaDictBuiltinSetdefault(t *testing.T) {
	sd := newTestSchemaDict("SD", "existing", starlark.String("val"))
	val, err := sd.Attr("setdefault")
	if err != nil {
		t.Fatal(err)
	}
	b := val.(*starlark.Builtin)
	thread := &starlark.Thread{Name: "test"}

	// Existing key returns its value.
	result, err := starlark.Call(thread, b, starlark.Tuple{starlark.String("existing"), starlark.String("other")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result != starlark.String("val") {
		t.Errorf("setdefault(existing, other) = %v, want val", result)
	}

	// Missing key sets and returns the default.
	result, err = starlark.Call(thread, b, starlark.Tuple{starlark.String("new"), starlark.String("default")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result != starlark.String("default") {
		t.Errorf("setdefault(new, default) = %v, want default", result)
	}
	v, found, _ := sd.Get(starlark.String("new"))
	if !found || v != starlark.String("default") {
		t.Errorf("after setdefault, Get(new) = %v/%v, want default/true", v, found)
	}
}

func TestSchemaDictLen(t *testing.T) {
	sd := newTestSchemaDict("L", "a", starlark.MakeInt(1), "b", starlark.MakeInt(2), "c", starlark.MakeInt(3))
	if sd.Len() != 3 {
		t.Errorf("Len() = %d, want 3", sd.Len())
	}
	empty := newTestSchemaDict("E")
	if empty.Len() != 0 {
		t.Errorf("empty Len() = %d, want 0", empty.Len())
	}
}
