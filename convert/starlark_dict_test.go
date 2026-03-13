package convert

import (
	"sort"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"go.starlark.net/starlark"
)

func TestStarlarkDictNewEmpty(t *testing.T) {
	d := NewStarlarkDict(0)
	if d == nil {
		t.Fatal("NewStarlarkDict(0) returned nil")
	}
	if got := d.Type(); got != "dict" {
		t.Errorf("Type() = %q, want %q", got, "dict")
	}
	if got := d.String(); got != "{}" {
		t.Errorf("String() = %q, want %q", got, "{}")
	}
	if got := d.Truth(); got != starlark.False {
		t.Errorf("Truth() = %v, want False", got)
	}
	if got := d.Len(); got != 0 {
		t.Errorf("Len() = %d, want 0", got)
	}
}

func TestStarlarkDictNonEmpty(t *testing.T) {
	d := NewStarlarkDict(2)
	if err := d.SetField("a", starlark.MakeInt(1)); err != nil {
		t.Fatal(err)
	}
	if err := d.SetField("b", starlark.MakeInt(2)); err != nil {
		t.Fatal(err)
	}

	if got := d.Truth(); got != starlark.True {
		t.Errorf("Truth() = %v, want True", got)
	}
	if got := d.Len(); got != 2 {
		t.Errorf("Len() = %d, want 2", got)
	}
	// String should match built-in dict format with keys in insertion order.
	s := d.String()
	if !strings.Contains(s, `"a"`) || !strings.Contains(s, `"b"`) {
		t.Errorf("String() = %q, missing expected keys", s)
	}
}

func TestStarlarkDictAttrExisting(t *testing.T) {
	d := NewStarlarkDict(0)
	if err := d.SetField("key", starlark.String("value")); err != nil {
		t.Fatal(err)
	}

	got, err := d.Attr("key")
	if err != nil {
		t.Fatalf("Attr(%q) error: %v", "key", err)
	}
	if got != starlark.String("value") {
		t.Errorf("Attr(%q) = %v, want %q", "key", got, "value")
	}
}

func TestStarlarkDictAttrMissing(t *testing.T) {
	d := NewStarlarkDict(0)

	got, err := d.Attr("missing")
	if err != nil {
		t.Fatalf("Attr(%q) error: %v", "missing", err)
	}
	if got != starlark.None {
		t.Errorf("Attr(%q) = %v (%T), want starlark.None", "missing", got, got)
	}
}

func TestStarlarkDictAttrNames(t *testing.T) {
	d := NewStarlarkDict(0)
	_ = d.SetField("zebra", starlark.True)
	_ = d.SetField("alpha", starlark.True)
	_ = d.SetField("middle", starlark.True)

	names := d.AttrNames()

	// Should contain all keys and all built-in method names, sorted.
	if !sort.StringsAreSorted(names) {
		t.Errorf("AttrNames() not sorted: %v", names)
	}

	// Check that data keys are present.
	for _, key := range []string{"zebra", "alpha", "middle"} {
		found := false
		for _, n := range names {
			if n == key {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("AttrNames() missing key %q", key)
		}
	}

	// Check that built-in method names are present.
	for _, method := range []string{"keys", "values", "items", "get", "pop", "update", "clear", "setdefault"} {
		found := false
		for _, n := range names {
			if n == method {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("AttrNames() missing method %q", method)
		}
	}
}

func TestStarlarkDictSetFieldAndAttr(t *testing.T) {
	d := NewStarlarkDict(0)
	val := starlark.MakeInt(42)
	if err := d.SetField("answer", val); err != nil {
		t.Fatal(err)
	}

	got, err := d.Attr("answer")
	if err != nil {
		t.Fatal(err)
	}
	if got != val {
		t.Errorf("Attr(%q) = %v, want %v", "answer", got, val)
	}
}

func TestStarlarkDictSetFieldFrozen(t *testing.T) {
	d := NewStarlarkDict(0)
	d.Freeze()

	err := d.SetField("key", starlark.True)
	if err == nil {
		t.Fatal("SetField on frozen dict should return error")
	}
	if !strings.Contains(err.Error(), "frozen") {
		t.Errorf("error %q should contain 'frozen'", err.Error())
	}
}

func TestStarlarkDictSetKeyAndGet(t *testing.T) {
	d := NewStarlarkDict(0)
	key := starlark.String("key")
	val := starlark.String("val")
	if err := d.SetKey(key, val); err != nil {
		t.Fatal(err)
	}

	got, found, err := d.Get(key)
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("Get() found = false, want true")
	}
	if got != val {
		t.Errorf("Get() = %v, want %v", got, val)
	}
}

func TestStarlarkDictSetKeyFrozen(t *testing.T) {
	d := NewStarlarkDict(0)
	d.Freeze()

	err := d.SetKey(starlark.String("key"), starlark.True)
	if err == nil {
		t.Fatal("SetKey on frozen dict should return error")
	}
	if !strings.Contains(err.Error(), "frozen") {
		t.Errorf("error %q should contain 'frozen'", err.Error())
	}
}

func TestStarlarkDictGetExisting(t *testing.T) {
	d := NewStarlarkDict(0)
	_ = d.SetField("x", starlark.MakeInt(7))

	val, found, err := d.Get(starlark.String("x"))
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("Get() found = false, want true")
	}
	if val != starlark.MakeInt(7) {
		t.Errorf("Get() = %v, want 7", val)
	}
}

func TestStarlarkDictGetMissing(t *testing.T) {
	d := NewStarlarkDict(0)

	val, found, err := d.Get(starlark.String("missing"))
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Fatal("Get() found = true, want false")
	}
	// starlark.Dict.Get returns (None, false, nil) for missing keys.
	if val != starlark.None {
		t.Errorf("Get() = %v, want None", val)
	}
}

func TestStarlarkDictIterate(t *testing.T) {
	d := NewStarlarkDict(0)
	_ = d.SetField("first", starlark.MakeInt(1))
	_ = d.SetField("second", starlark.MakeInt(2))
	_ = d.SetField("third", starlark.MakeInt(3))

	iter := d.Iterate()
	defer iter.Done()

	var keys []string
	var k starlark.Value
	for iter.Next(&k) {
		keys = append(keys, string(k.(starlark.String)))
	}

	want := []string{"first", "second", "third"}
	if diff := cmp.Diff(want, keys); diff != "" {
		t.Errorf("Iterate() keys mismatch (-want +got):\n%s", diff)
	}
}

func TestStarlarkDictFreezeNested(t *testing.T) {
	inner := NewStarlarkDict(0)
	_ = inner.SetField("x", starlark.MakeInt(1))

	outer := NewStarlarkDict(0)
	_ = outer.SetField("inner", inner)

	outer.Freeze()

	// Outer should be frozen.
	if err := outer.SetField("new", starlark.True); err == nil {
		t.Error("SetField on frozen outer should return error")
	}

	// Inner should also be frozen (freeze propagates).
	if err := inner.SetField("y", starlark.MakeInt(2)); err == nil {
		t.Error("SetField on inner (nested frozen) should return error")
	}
}

func TestStarlarkDictFreezeNestedList(t *testing.T) {
	list := starlark.NewList([]starlark.Value{starlark.MakeInt(1)})

	d := NewStarlarkDict(0)
	_ = d.SetField("items", list)

	d.Freeze()

	// The nested list should be frozen.
	if err := list.Append(starlark.MakeInt(2)); err == nil {
		t.Error("Append to frozen list should return error")
	}
}

func TestStarlarkDictHash(t *testing.T) {
	d := NewStarlarkDict(0)
	_, err := d.Hash()
	if err == nil {
		t.Fatal("Hash() should return error for dict")
	}
	if !strings.Contains(err.Error(), "unhashable") {
		t.Errorf("error %q should contain 'unhashable'", err.Error())
	}
}

func TestStarlarkDictTruth(t *testing.T) {
	empty := NewStarlarkDict(0)
	if empty.Truth() != starlark.False {
		t.Error("empty dict Truth() should be False")
	}

	nonEmpty := NewStarlarkDict(0)
	_ = nonEmpty.SetField("k", starlark.True)
	if nonEmpty.Truth() != starlark.True {
		t.Error("non-empty dict Truth() should be True")
	}
}

func TestStarlarkDictNestedDotAccess(t *testing.T) {
	inner := NewStarlarkDict(0)
	_ = inner.SetField("replicas", starlark.MakeInt(3))

	outer := NewStarlarkDict(0)
	_ = outer.SetField("spec", inner)

	// d.spec should return inner StarlarkDict.
	specVal, err := outer.Attr("spec")
	if err != nil {
		t.Fatal(err)
	}
	specDict, ok := specVal.(*StarlarkDict)
	if !ok {
		t.Fatalf("Attr(\"spec\") returned %T, want *StarlarkDict", specVal)
	}

	// d.spec.replicas should return 3.
	replicasVal, err := specDict.Attr("replicas")
	if err != nil {
		t.Fatal(err)
	}
	if replicasVal != starlark.MakeInt(3) {
		t.Errorf("Attr(\"replicas\") = %v, want 3", replicasVal)
	}
}

func TestStarlarkDictBuiltinKeys(t *testing.T) {
	d := NewStarlarkDict(0)
	_ = d.SetField("b", starlark.MakeInt(2))
	_ = d.SetField("a", starlark.MakeInt(1))

	keysAttr, err := d.Attr("keys")
	if err != nil {
		t.Fatal(err)
	}
	fn, ok := keysAttr.(*starlark.Builtin)
	if !ok {
		t.Fatalf("Attr(\"keys\") = %T, want *starlark.Builtin", keysAttr)
	}

	result, err := starlark.Call(&starlark.Thread{}, fn, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	list, ok := result.(*starlark.List)
	if !ok {
		t.Fatalf("keys() returned %T, want *starlark.List", result)
	}
	if list.Len() != 2 {
		t.Errorf("keys() len = %d, want 2", list.Len())
	}
}

func TestStarlarkDictBuiltinValues(t *testing.T) {
	d := NewStarlarkDict(0)
	_ = d.SetField("x", starlark.MakeInt(10))

	valuesAttr, err := d.Attr("values")
	if err != nil {
		t.Fatal(err)
	}
	fn, ok := valuesAttr.(*starlark.Builtin)
	if !ok {
		t.Fatalf("Attr(\"values\") = %T, want *starlark.Builtin", valuesAttr)
	}

	result, err := starlark.Call(&starlark.Thread{}, fn, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	list, ok := result.(*starlark.List)
	if !ok {
		t.Fatalf("values() returned %T, want *starlark.List", result)
	}
	if list.Len() != 1 {
		t.Errorf("values() len = %d, want 1", list.Len())
	}
}

func TestStarlarkDictBuiltinItems(t *testing.T) {
	d := NewStarlarkDict(0)
	_ = d.SetField("k", starlark.String("v"))

	itemsAttr, err := d.Attr("items")
	if err != nil {
		t.Fatal(err)
	}
	fn := itemsAttr.(*starlark.Builtin)

	result, err := starlark.Call(&starlark.Thread{}, fn, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	list, ok := result.(*starlark.List)
	if !ok {
		t.Fatalf("items() returned %T, want *starlark.List", result)
	}
	if list.Len() != 1 {
		t.Errorf("items() len = %d, want 1", list.Len())
	}
	// Each item should be a tuple.
	item := list.Index(0)
	tup, ok := item.(starlark.Tuple)
	if !ok {
		t.Fatalf("items()[0] = %T, want starlark.Tuple", item)
	}
	if len(tup) != 2 {
		t.Fatalf("items()[0] len = %d, want 2", len(tup))
	}
	if tup[0] != starlark.String("k") {
		t.Errorf("items()[0][0] = %v, want \"k\"", tup[0])
	}
	if tup[1] != starlark.String("v") {
		t.Errorf("items()[0][1] = %v, want \"v\"", tup[1])
	}
}

func TestStarlarkDictBuiltinGet(t *testing.T) {
	d := NewStarlarkDict(0)
	_ = d.SetField("x", starlark.MakeInt(5))

	getAttr, err := d.Attr("get")
	if err != nil {
		t.Fatal(err)
	}
	fn := getAttr.(*starlark.Builtin)

	// get(key) for existing key.
	result, err := starlark.Call(&starlark.Thread{}, fn, starlark.Tuple{starlark.String("x")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result != starlark.MakeInt(5) {
		t.Errorf("get(\"x\") = %v, want 5", result)
	}

	// get(key) for missing key returns None.
	result, err = starlark.Call(&starlark.Thread{}, fn, starlark.Tuple{starlark.String("missing")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result != starlark.None {
		t.Errorf("get(\"missing\") = %v, want None", result)
	}

	// get(key, default) for missing key returns default.
	result, err = starlark.Call(&starlark.Thread{}, fn, starlark.Tuple{starlark.String("missing"), starlark.MakeInt(99)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result != starlark.MakeInt(99) {
		t.Errorf("get(\"missing\", 99) = %v, want 99", result)
	}
}

func TestStarlarkDictBuiltinPop(t *testing.T) {
	d := NewStarlarkDict(0)
	_ = d.SetField("x", starlark.MakeInt(5))

	popAttr, err := d.Attr("pop")
	if err != nil {
		t.Fatal(err)
	}
	fn := popAttr.(*starlark.Builtin)

	// pop(key) removes and returns.
	result, err := starlark.Call(&starlark.Thread{}, fn, starlark.Tuple{starlark.String("x")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result != starlark.MakeInt(5) {
		t.Errorf("pop(\"x\") = %v, want 5", result)
	}

	// Key should be removed.
	_, found, _ := d.Get(starlark.String("x"))
	if found {
		t.Error("key \"x\" still present after pop")
	}

	// pop(missing) without default should error.
	_, err = starlark.Call(&starlark.Thread{}, fn, starlark.Tuple{starlark.String("missing")}, nil)
	if err == nil {
		t.Error("pop(missing) without default should error")
	}

	// pop(missing, default) should return default.
	result, err = starlark.Call(&starlark.Thread{}, fn, starlark.Tuple{starlark.String("missing"), starlark.MakeInt(42)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result != starlark.MakeInt(42) {
		t.Errorf("pop(\"missing\", 42) = %v, want 42", result)
	}
}

func TestStarlarkDictBuiltinUpdate(t *testing.T) {
	d := NewStarlarkDict(0)
	_ = d.SetField("a", starlark.MakeInt(1))

	other := NewStarlarkDict(0)
	_ = other.SetField("b", starlark.MakeInt(2))
	_ = other.SetField("a", starlark.MakeInt(10)) // overwrite

	updateAttr, err := d.Attr("update")
	if err != nil {
		t.Fatal(err)
	}
	fn := updateAttr.(*starlark.Builtin)

	_, err = starlark.Call(&starlark.Thread{}, fn, starlark.Tuple{other}, nil)
	if err != nil {
		t.Fatal(err)
	}

	// "a" should be overwritten to 10.
	val, err := d.Attr("a")
	if err != nil {
		t.Fatal(err)
	}
	if val != starlark.MakeInt(10) {
		t.Errorf("after update, a = %v, want 10", val)
	}

	// "b" should be added.
	val, err = d.Attr("b")
	if err != nil {
		t.Fatal(err)
	}
	if val != starlark.MakeInt(2) {
		t.Errorf("after update, b = %v, want 2", val)
	}
}

func TestStarlarkDictBuiltinClear(t *testing.T) {
	d := NewStarlarkDict(0)
	_ = d.SetField("a", starlark.MakeInt(1))
	_ = d.SetField("b", starlark.MakeInt(2))

	clearAttr, err := d.Attr("clear")
	if err != nil {
		t.Fatal(err)
	}
	fn := clearAttr.(*starlark.Builtin)

	_, err = starlark.Call(&starlark.Thread{}, fn, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	if d.Len() != 0 {
		t.Errorf("after clear, Len() = %d, want 0", d.Len())
	}
}

func TestStarlarkDictBuiltinSetdefault(t *testing.T) {
	d := NewStarlarkDict(0)
	_ = d.SetField("existing", starlark.MakeInt(5))

	sdAttr, err := d.Attr("setdefault")
	if err != nil {
		t.Fatal(err)
	}
	fn := sdAttr.(*starlark.Builtin)

	// setdefault on existing key returns existing value.
	result, err := starlark.Call(&starlark.Thread{}, fn, starlark.Tuple{starlark.String("existing"), starlark.MakeInt(99)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result != starlark.MakeInt(5) {
		t.Errorf("setdefault(\"existing\", 99) = %v, want 5", result)
	}

	// setdefault on missing key sets and returns default.
	result, err = starlark.Call(&starlark.Thread{}, fn, starlark.Tuple{starlark.String("new"), starlark.MakeInt(42)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result != starlark.MakeInt(42) {
		t.Errorf("setdefault(\"new\", 42) = %v, want 42", result)
	}

	// Verify it was set.
	val, err := d.Attr("new")
	if err != nil {
		t.Fatal(err)
	}
	if val != starlark.MakeInt(42) {
		t.Errorf("Attr(\"new\") = %v, want 42", val)
	}

	// setdefault without default uses None.
	result, err = starlark.Call(&starlark.Thread{}, fn, starlark.Tuple{starlark.String("nodefault")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result != starlark.None {
		t.Errorf("setdefault(\"nodefault\") = %v, want None", result)
	}
}

func TestStarlarkDictLen(t *testing.T) {
	d := NewStarlarkDict(0)
	if d.Len() != 0 {
		t.Errorf("empty Len() = %d, want 0", d.Len())
	}

	_ = d.SetField("a", starlark.True)
	_ = d.SetField("b", starlark.True)
	_ = d.SetField("c", starlark.True)

	if d.Len() != 3 {
		t.Errorf("Len() = %d, want 3", d.Len())
	}
}
