// Package convert provides bidirectional conversion between protobuf structpb
// values and Starlark values, centered around the custom StarlarkDict type.
package convert

import (
	"fmt"
	"sort"

	"go.starlark.net/starlark"
	"go.starlark.net/syntax"
)

// builtinMethodNames lists the dict built-in methods that StarlarkDict exposes.
var builtinMethodNames = []string{
	"clear",
	"get",
	"items",
	"keys",
	"pop",
	"setdefault",
	"update",
	"values",
}

// Compile-time interface compliance checks.
var (
	_ starlark.Value       = (*StarlarkDict)(nil)
	_ starlark.HasAttrs    = (*StarlarkDict)(nil)
	_ starlark.HasSetField = (*StarlarkDict)(nil)
	_ starlark.HasSetKey   = (*StarlarkDict)(nil)
	_ starlark.Mapping     = (*StarlarkDict)(nil)
	_ starlark.Iterable    = (*StarlarkDict)(nil)
	_ starlark.Comparable  = (*StarlarkDict)(nil)
)

// StarlarkDict wraps an ordered map of string keys to Starlark values,
// supporting both dot access (d.key) and bracket access (d["key"]).
// Missing key via dot access returns starlark.None (not an error),
// which is lenient behavior for optional Kubernetes fields.
type StarlarkDict struct {
	d *starlark.Dict
}

// NewStarlarkDict creates a new empty StarlarkDict with an initial capacity hint.
func NewStarlarkDict(size int) *StarlarkDict {
	_ = size // capacity hint; starlark.Dict does not accept a size hint
	return &StarlarkDict{d: new(starlark.Dict)}
}

// InternalDict returns the underlying starlark.Dict for direct access
// by other convert package functions (e.g., conversion).
func (sd *StarlarkDict) InternalDict() *starlark.Dict {
	return sd.d
}

// --- starlark.Value ---

// String returns the Starlark string representation, delegating to the internal Dict.
func (sd *StarlarkDict) String() string {
	return sd.d.String()
}

// Type returns "dict" to match Starlark dict conventions.
func (sd *StarlarkDict) Type() string {
	return "dict"
}

// Freeze makes the dict and all nested values immutable.
func (sd *StarlarkDict) Freeze() {
	sd.d.Freeze()
}

// Truth returns True for non-empty dicts, False for empty.
func (sd *StarlarkDict) Truth() starlark.Bool {
	return sd.d.Truth()
}

// Hash returns an error because dicts are unhashable.
func (sd *StarlarkDict) Hash() (uint32, error) {
	return 0, fmt.Errorf("unhashable type: dict")
}

// --- starlark.HasAttrs ---

// Attr looks up a key by name. If the key exists, it returns the value.
// If the key names a built-in method, it returns a bound builtin.
// If the key is not found, it returns (starlark.None, nil) for lenient
// Kubernetes optional field access.
func (sd *StarlarkDict) Attr(name string) (starlark.Value, error) {
	// Check built-in methods first.
	if b := sd.builtinMethod(name); b != nil {
		return b, nil
	}

	v, found, err := sd.d.Get(starlark.String(name))
	if err != nil {
		return nil, err
	}
	if !found {
		return starlark.None, nil
	}
	return v, nil
}

// AttrNames returns a sorted list of all keys plus built-in method names.
func (sd *StarlarkDict) AttrNames() []string {
	// Collect data keys.
	keys := make([]string, 0, sd.d.Len()+len(builtinMethodNames))
	for _, item := range sd.d.Items() {
		if s, ok := item[0].(starlark.String); ok {
			keys = append(keys, string(s))
		}
	}
	// Append built-in method names.
	keys = append(keys, builtinMethodNames...)
	sort.Strings(keys)
	return keys
}

// --- starlark.HasSetField ---

// SetField sets a key by name. Returns an error if the dict is frozen.
func (sd *StarlarkDict) SetField(name string, val starlark.Value) error {
	return sd.d.SetKey(starlark.String(name), val)
}

// --- starlark.HasSetKey ---

// SetKey sets a key-value pair. Returns an error if the dict is frozen.
func (sd *StarlarkDict) SetKey(k, v starlark.Value) error {
	return sd.d.SetKey(k, v)
}

// --- starlark.Mapping ---

// Get looks up a key and returns its value.
func (sd *StarlarkDict) Get(key starlark.Value) (v starlark.Value, found bool, err error) {
	return sd.d.Get(key)
}

// --- starlark.Iterable ---

// Iterate returns an iterator over the dict keys in insertion order.
func (sd *StarlarkDict) Iterate() starlark.Iterator {
	return sd.d.Iterate()
}

// --- starlark.Comparable ---

// CompareSameType compares two StarlarkDicts.
func (sd *StarlarkDict) CompareSameType(op syntax.Token, y_ starlark.Value, depth int) (bool, error) {
	other, ok := y_.(*StarlarkDict)
	if !ok {
		return false, fmt.Errorf("cannot compare dict with %s", y_.Type())
	}
	// Delegate to the internal dicts' comparison.
	return sd.d.CompareSameType(op, other.d, depth)
}

// --- Len ---

// Len returns the number of entries in the dict.
func (sd *StarlarkDict) Len() int {
	return sd.d.Len()
}

// --- Built-in methods ---

// builtinMethod returns a bound starlark.Builtin for the named method, or nil.
func (sd *StarlarkDict) builtinMethod(name string) *starlark.Builtin {
	switch name {
	case "keys":
		return starlark.NewBuiltin("keys", sd.keysMethod)
	case "values":
		return starlark.NewBuiltin("values", sd.valuesMethod)
	case "items":
		return starlark.NewBuiltin("items", sd.itemsMethod)
	case "get":
		return starlark.NewBuiltin("get", sd.getMethod)
	case "pop":
		return starlark.NewBuiltin("pop", sd.popMethod)
	case "update":
		return starlark.NewBuiltin("update", sd.updateMethod)
	case "clear":
		return starlark.NewBuiltin("clear", sd.clearMethod)
	case "setdefault":
		return starlark.NewBuiltin("setdefault", sd.setdefaultMethod)
	default:
		return nil
	}
}

// keysMethod implements dict.keys() -> list of keys.
func (sd *StarlarkDict) keysMethod(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if err := starlark.UnpackPositionalArgs("keys", args, kwargs, 0); err != nil {
		return nil, err
	}
	items := sd.d.Items()
	keys := make([]starlark.Value, len(items))
	for i, item := range items {
		keys[i] = item[0]
	}
	return starlark.NewList(keys), nil
}

// valuesMethod implements dict.values() -> list of values.
func (sd *StarlarkDict) valuesMethod(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if err := starlark.UnpackPositionalArgs("values", args, kwargs, 0); err != nil {
		return nil, err
	}
	items := sd.d.Items()
	vals := make([]starlark.Value, len(items))
	for i, item := range items {
		vals[i] = item[1]
	}
	return starlark.NewList(vals), nil
}

// itemsMethod implements dict.items() -> list of (key, value) tuples.
func (sd *StarlarkDict) itemsMethod(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if err := starlark.UnpackPositionalArgs("items", args, kwargs, 0); err != nil {
		return nil, err
	}
	items := sd.d.Items()
	tuples := make([]starlark.Value, len(items))
	for i, item := range items {
		tuples[i] = starlark.Tuple{item[0], item[1]}
	}
	return starlark.NewList(tuples), nil
}

// getMethod implements dict.get(key, default=None) -> value or default.
func (sd *StarlarkDict) getMethod(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var key starlark.Value
	var dflt starlark.Value = starlark.None
	if err := starlark.UnpackPositionalArgs("get", args, kwargs, 1, &key, &dflt); err != nil {
		return nil, err
	}
	v, found, err := sd.d.Get(key)
	if err != nil {
		return nil, err
	}
	if !found {
		return dflt, nil
	}
	return v, nil
}

// popMethod implements dict.pop(key, default?) -> remove and return value.
func (sd *StarlarkDict) popMethod(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var key starlark.Value
	var dflt starlark.Value
	if err := starlark.UnpackPositionalArgs("pop", args, kwargs, 1, &key, &dflt); err != nil {
		return nil, err
	}
	v, found, err := sd.d.Delete(key)
	if err != nil {
		return nil, err
	}
	if !found {
		if dflt != nil {
			return dflt, nil
		}
		return nil, fmt.Errorf("pop: key %s not found", key.String())
	}
	return v, nil
}

// updateMethod implements dict.update(other) -> merge other dict entries.
func (sd *StarlarkDict) updateMethod(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var other starlark.Value
	if err := starlark.UnpackPositionalArgs("update", args, kwargs, 1, &other); err != nil {
		return nil, err
	}

	// Accept *StarlarkDict, *starlark.Dict, or any iterable of pairs.
	switch o := other.(type) {
	case *StarlarkDict:
		for _, item := range o.d.Items() {
			if err := sd.d.SetKey(item[0], item[1]); err != nil {
				return nil, err
			}
		}
	case *starlark.Dict:
		for _, item := range o.Items() {
			if err := sd.d.SetKey(item[0], item[1]); err != nil {
				return nil, err
			}
		}
	default:
		return nil, fmt.Errorf("update: unsupported argument type %s", other.Type())
	}

	return starlark.None, nil
}

// clearMethod implements dict.clear() -> remove all entries.
func (sd *StarlarkDict) clearMethod(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if err := starlark.UnpackPositionalArgs("clear", args, kwargs, 0); err != nil {
		return nil, err
	}
	if err := sd.d.Clear(); err != nil {
		return nil, err
	}
	return starlark.None, nil
}

// setdefaultMethod implements dict.setdefault(key, default=None) -> return existing or set default.
func (sd *StarlarkDict) setdefaultMethod(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var key starlark.Value
	var dflt starlark.Value = starlark.None
	if err := starlark.UnpackPositionalArgs("setdefault", args, kwargs, 1, &key, &dflt); err != nil {
		return nil, err
	}
	v, found, err := sd.d.Get(key)
	if err != nil {
		return nil, err
	}
	if found {
		return v, nil
	}
	if err := sd.d.SetKey(key, dflt); err != nil {
		return nil, err
	}
	return dflt, nil
}
