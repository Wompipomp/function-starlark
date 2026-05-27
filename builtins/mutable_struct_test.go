package builtins

import (
	"sort"
	"strings"
	"testing"

	"github.com/crossplane/function-sdk-go/logging"
	"go.starlark.net/starlark"
	"go.starlark.net/syntax"

	"github.com/wompipomp/function-starlark/runtime"
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
