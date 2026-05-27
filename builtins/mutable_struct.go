package builtins

import (
	"fmt"
	"sort"
	"strings"

	"go.starlark.net/starlark"
	"go.starlark.net/syntax"
)

// Compile-time interface compliance checks.
var (
	_ starlark.Value       = (*MutableStruct)(nil)
	_ starlark.HasAttrs    = (*MutableStruct)(nil)
	_ starlark.HasSetField = (*MutableStruct)(nil)
	_ starlark.HasBinary   = (*MutableStruct)(nil)
	_ starlark.Comparable  = (*MutableStruct)(nil)
)

// MutableStruct is a struct-like Starlark value that allows field
// reassignment via dot-access while unfrozen. Internally backed by a
// *starlark.Dict for free freeze/mutation semantics.
type MutableStruct struct {
	d *starlark.Dict
}

// MakeMutableStruct is the Starlark built-in constructor for mutable_struct.
// It accepts keyword-only arguments (no positional args) and returns a new
// MutableStruct with those fields set.
func MakeMutableStruct(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if len(args) > 0 {
		return nil, fmt.Errorf("mutable_struct: unexpected positional arguments")
	}
	d := new(starlark.Dict)
	for _, kv := range kwargs {
		if err := d.SetKey(kv[0], kv[1]); err != nil {
			return nil, err
		}
	}
	return &MutableStruct{d: d}, nil
}

// --- starlark.Value ---

// String returns the Starlark string representation with keys sorted
// alphabetically, e.g. mutable_struct(items = [], name = "test").
func (s *MutableStruct) String() string {
	items := s.d.Items()

	// Collect and sort by key name.
	type kv struct {
		key string
		val starlark.Value
	}
	sorted := make([]kv, 0, len(items))
	for _, item := range items {
		if k, ok := item[0].(starlark.String); ok {
			sorted = append(sorted, kv{key: string(k), val: item[1]})
		}
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].key < sorted[j].key
	})

	buf := new(strings.Builder)
	buf.WriteString("mutable_struct(")
	for i, e := range sorted {
		if i > 0 {
			buf.WriteString(", ")
		}
		buf.WriteString(e.key)
		buf.WriteString(" = ")
		buf.WriteString(e.val.String())
	}
	buf.WriteByte(')')
	return buf.String()
}

// Type returns "mutable_struct".
func (s *MutableStruct) Type() string { return "mutable_struct" }

// Freeze delegates to the internal dict, transitively freezing all values.
func (s *MutableStruct) Freeze() { s.d.Freeze() }

// Truth always returns True, matching immutable struct behavior (structs
// are truthy even when empty).
func (s *MutableStruct) Truth() starlark.Bool { return starlark.True }

// Hash returns an error because mutable_structs are unhashable (like dicts).
func (s *MutableStruct) Hash() (uint32, error) {
	return 0, fmt.Errorf("unhashable type: mutable_struct")
}

// --- starlark.HasAttrs ---

// Attr looks up a field by name. Returns NoSuchAttrError for missing
// fields (struct semantics, not None like StarlarkDict).
func (s *MutableStruct) Attr(name string) (starlark.Value, error) {
	v, found, err := s.d.Get(starlark.String(name))
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, starlark.NoSuchAttrError(
			fmt.Sprintf("mutable_struct has no .%s attribute", name))
	}
	return v, nil
}

// AttrNames returns a sorted list of field names.
func (s *MutableStruct) AttrNames() []string {
	items := s.d.Items()
	names := make([]string, 0, len(items))
	for _, item := range items {
		if k, ok := item[0].(starlark.String); ok {
			names = append(names, string(k))
		}
	}
	sort.Strings(names)
	return names
}

// --- starlark.HasSetField ---

// SetField sets (or adds) a field by name. Returns an error if frozen.
func (s *MutableStruct) SetField(name string, val starlark.Value) error {
	return s.d.SetKey(starlark.String(name), val)
}

// --- starlark.HasBinary ---

// Binary supports the + operator to merge two MutableStructs. The right
// operand wins on key conflicts. Returns nil, nil for unhandled operations.
func (s *MutableStruct) Binary(op syntax.Token, y starlark.Value, side starlark.Side) (starlark.Value, error) {
	other, ok := y.(*MutableStruct)
	if !ok || op != syntax.PLUS {
		return nil, nil // unhandled
	}

	left, right := s, other
	if side == starlark.Right {
		left, right = other, s
	}

	merged := new(starlark.Dict)
	for _, item := range left.d.Items() {
		if err := merged.SetKey(item[0], item[1]); err != nil {
			return nil, err
		}
	}
	for _, item := range right.d.Items() {
		if err := merged.SetKey(item[0], item[1]); err != nil {
			return nil, err
		}
	}

	return &MutableStruct{d: merged}, nil
}

// --- starlark.Comparable ---

// CompareSameType supports == and != comparison between two MutableStructs.
func (s *MutableStruct) CompareSameType(op syntax.Token, y_ starlark.Value, depth int) (bool, error) {
	other := y_.(*MutableStruct)
	switch op {
	case syntax.EQL:
		return mutableStructsEqual(s, other, depth)
	case syntax.NEQ:
		eq, err := mutableStructsEqual(s, other, depth)
		return !eq, err
	default:
		return false, fmt.Errorf("%s %s %s not implemented", s.Type(), op, other.Type())
	}
}

// mutableStructsEqual compares two MutableStructs for equality by
// iterating their sorted key-value pairs.
func mutableStructsEqual(x, y *MutableStruct, depth int) (bool, error) {
	xItems := x.d.Items()
	yItems := y.d.Items()
	if len(xItems) != len(yItems) {
		return false, nil
	}

	// Sort both by key for deterministic comparison.
	sortItems := func(items []starlark.Tuple) {
		sort.Slice(items, func(i, j int) bool {
			ki, _ := items[i][0].(starlark.String)
			kj, _ := items[j][0].(starlark.String)
			return ki < kj
		})
	}
	sortItems(xItems)
	sortItems(yItems)

	for i := range xItems {
		xk, _ := xItems[i][0].(starlark.String)
		yk, _ := yItems[i][0].(starlark.String)
		if xk != yk {
			return false, nil
		}
		eq, err := starlark.EqualDepth(xItems[i][1], yItems[i][1], depth-1)
		if err != nil {
			return false, err
		}
		if !eq {
			return false, nil
		}
	}
	return true, nil
}
