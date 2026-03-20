package schema

import (
	"fmt"
	"sort"

	"go.starlark.net/starlark"
	"go.starlark.net/syntax"
)

// builtinMethodNames lists the dict built-in methods that SchemaDict exposes.
var schemaDictBuiltinMethodNames = []string{
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
	_ starlark.Value       = (*SchemaDict)(nil)
	_ starlark.HasAttrs    = (*SchemaDict)(nil)
	_ starlark.HasSetField = (*SchemaDict)(nil)
	_ starlark.HasSetKey   = (*SchemaDict)(nil)
	_ starlark.Mapping     = (*SchemaDict)(nil)
	_ starlark.Iterable    = (*SchemaDict)(nil)
	_ starlark.Comparable  = (*SchemaDict)(nil)
)

// SchemaDict wraps a *starlark.Dict and carries the schema name for
// tagged printing and skip-revalidation detection. It implements all
// the same interfaces as convert.StarlarkDict via delegation.
type SchemaDict struct {
	dict       *starlark.Dict
	schemaName string
}

// NewSchemaDict creates a new SchemaDict wrapping the given dict with a schema name.
func NewSchemaDict(name string, dict *starlark.Dict) *SchemaDict {
	return &SchemaDict{dict: dict, schemaName: name}
}

// SchemaName returns the schema name for introspection and skip-revalidation.
func (sd *SchemaDict) SchemaName() string { return sd.schemaName }

// InternalDict returns the underlying *starlark.Dict for Phase 27 compatibility.
func (sd *SchemaDict) InternalDict() *starlark.Dict { return sd.dict }

// --- starlark.Value ---

// String returns the tagged format: Name({...}).
func (sd *SchemaDict) String() string {
	return fmt.Sprintf("%s(%s)", sd.schemaName, sd.dict.String())
}

// Type returns "dict" for compatibility with CheckType.
func (sd *SchemaDict) Type() string { return "dict" }

// Freeze delegates to the internal dict.
func (sd *SchemaDict) Freeze() { sd.dict.Freeze() }

// Truth returns True for non-empty, False for empty.
func (sd *SchemaDict) Truth() starlark.Bool { return sd.dict.Truth() }

// Hash returns an error because dicts are unhashable.
func (sd *SchemaDict) Hash() (uint32, error) {
	return 0, fmt.Errorf("unhashable type: dict")
}

// --- starlark.HasAttrs ---

// Attr looks up a key by name. Checks builtin methods first, then data keys.
// Missing keys return starlark.None (lenient access).
func (sd *SchemaDict) Attr(name string) (starlark.Value, error) {
	// Check built-in methods first.
	if b := sd.builtinMethod(name); b != nil {
		return b, nil
	}

	v, found, err := sd.dict.Get(starlark.String(name))
	if err != nil {
		return nil, err
	}
	if !found {
		return starlark.None, nil
	}
	return v, nil
}

// AttrNames returns a sorted list of all keys plus built-in method names.
func (sd *SchemaDict) AttrNames() []string {
	keys := make([]string, 0, sd.dict.Len()+len(schemaDictBuiltinMethodNames))
	for _, item := range sd.dict.Items() {
		if s, ok := item[0].(starlark.String); ok {
			keys = append(keys, string(s))
		}
	}
	keys = append(keys, schemaDictBuiltinMethodNames...)
	sort.Strings(keys)
	return keys
}

// --- starlark.HasSetField ---

// SetField sets a key by name. Returns an error if frozen.
func (sd *SchemaDict) SetField(name string, val starlark.Value) error {
	return sd.dict.SetKey(starlark.String(name), val)
}

// --- starlark.HasSetKey ---

// SetKey sets a key-value pair. Returns an error if frozen.
func (sd *SchemaDict) SetKey(k, v starlark.Value) error {
	return sd.dict.SetKey(k, v)
}

// --- starlark.Mapping ---

// Get looks up a key and returns its value.
func (sd *SchemaDict) Get(key starlark.Value) (v starlark.Value, found bool, err error) {
	return sd.dict.Get(key)
}

// --- starlark.Iterable ---

// Iterate returns an iterator over the dict keys in insertion order.
func (sd *SchemaDict) Iterate() starlark.Iterator {
	return sd.dict.Iterate()
}

// --- starlark.Comparable ---

// CompareSameType compares two SchemaDicts by delegating to internal dicts.
func (sd *SchemaDict) CompareSameType(op syntax.Token, y_ starlark.Value, depth int) (bool, error) {
	other, ok := y_.(*SchemaDict)
	if !ok {
		return false, fmt.Errorf("cannot compare dict with %s", y_.Type())
	}
	return sd.dict.CompareSameType(op, other.dict, depth)
}

// --- Len ---

// Len returns the number of entries in the dict.
func (sd *SchemaDict) Len() int {
	return sd.dict.Len()
}

// --- Built-in methods ---

// builtinMethod returns a bound starlark.Builtin for the named method, or nil.
func (sd *SchemaDict) builtinMethod(name string) *starlark.Builtin {
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

func (sd *SchemaDict) keysMethod(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if err := starlark.UnpackPositionalArgs("keys", args, kwargs, 0); err != nil {
		return nil, err
	}
	items := sd.dict.Items()
	keys := make([]starlark.Value, len(items))
	for i, item := range items {
		keys[i] = item[0]
	}
	return starlark.NewList(keys), nil
}

func (sd *SchemaDict) valuesMethod(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if err := starlark.UnpackPositionalArgs("values", args, kwargs, 0); err != nil {
		return nil, err
	}
	items := sd.dict.Items()
	vals := make([]starlark.Value, len(items))
	for i, item := range items {
		vals[i] = item[1]
	}
	return starlark.NewList(vals), nil
}

func (sd *SchemaDict) itemsMethod(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if err := starlark.UnpackPositionalArgs("items", args, kwargs, 0); err != nil {
		return nil, err
	}
	items := sd.dict.Items()
	tuples := make([]starlark.Value, len(items))
	for i, item := range items {
		tuples[i] = starlark.Tuple{item[0], item[1]}
	}
	return starlark.NewList(tuples), nil
}

func (sd *SchemaDict) getMethod(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var key starlark.Value
	var dflt starlark.Value = starlark.None
	if err := starlark.UnpackPositionalArgs("get", args, kwargs, 1, &key, &dflt); err != nil {
		return nil, err
	}
	v, found, err := sd.dict.Get(key)
	if err != nil {
		return nil, err
	}
	if !found {
		return dflt, nil
	}
	return v, nil
}

func (sd *SchemaDict) popMethod(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var key starlark.Value
	var dflt starlark.Value
	if err := starlark.UnpackPositionalArgs("pop", args, kwargs, 1, &key, &dflt); err != nil {
		return nil, err
	}
	v, found, err := sd.dict.Delete(key)
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

func (sd *SchemaDict) updateMethod(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var other starlark.Value
	if err := starlark.UnpackPositionalArgs("update", args, kwargs, 1, &other); err != nil {
		return nil, err
	}
	switch o := other.(type) {
	case *SchemaDict:
		for _, item := range o.dict.Items() {
			if err := sd.dict.SetKey(item[0], item[1]); err != nil {
				return nil, err
			}
		}
	case *starlark.Dict:
		for _, item := range o.Items() {
			if err := sd.dict.SetKey(item[0], item[1]); err != nil {
				return nil, err
			}
		}
	default:
		return nil, fmt.Errorf("update: unsupported argument type %s", other.Type())
	}
	return starlark.None, nil
}

func (sd *SchemaDict) clearMethod(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if err := starlark.UnpackPositionalArgs("clear", args, kwargs, 0); err != nil {
		return nil, err
	}
	if err := sd.dict.Clear(); err != nil {
		return nil, err
	}
	return starlark.None, nil
}

func (sd *SchemaDict) setdefaultMethod(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var key starlark.Value
	var dflt starlark.Value = starlark.None
	if err := starlark.UnpackPositionalArgs("setdefault", args, kwargs, 1, &key, &dflt); err != nil {
		return nil, err
	}
	v, found, err := sd.dict.Get(key)
	if err != nil {
		return nil, err
	}
	if found {
		return v, nil
	}
	if err := sd.dict.SetKey(key, dflt); err != nil {
		return nil, err
	}
	return dflt, nil
}
