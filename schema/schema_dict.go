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

// SchemaDict wraps a *starlark.Dict and carries the schema reference for
// tagged printing, skip-revalidation detection, and post-construction
// mutation validation. It implements all the same interfaces as
// convert.StarlarkDict via delegation.
type SchemaDict struct {
	dict   *starlark.Dict
	schema *SchemaCallable // may be nil for legacy/test usage
}

// NewSchemaDict creates a new SchemaDict wrapping the given dict with a schema.
func NewSchemaDict(s *SchemaCallable, dict *starlark.Dict) *SchemaDict {
	return &SchemaDict{dict: dict, schema: s}
}

// SchemaName returns the schema name for introspection and skip-revalidation.
func (sd *SchemaDict) SchemaName() string {
	if sd.schema != nil {
		return sd.schema.name
	}
	return ""
}

// InternalDict returns the underlying *starlark.Dict for conversion pipelines.
func (sd *SchemaDict) InternalDict() *starlark.Dict { return sd.dict }

// Schema returns the SchemaCallable for introspection (may be nil).
func (sd *SchemaDict) Schema() *SchemaCallable { return sd.schema }

// --- starlark.Value ---

// String returns the tagged format: Name({...}).
func (sd *SchemaDict) String() string {
	return fmt.Sprintf("%s(%s)", sd.SchemaName(), sd.dict.String())
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

// SetField sets a key by name, validating the value against the schema's
// field descriptor (type, enum, nested schema). Returns an error if the
// field is unknown or the value fails validation.
func (sd *SchemaDict) SetField(name string, val starlark.Value) error {
	if sd.schema != nil {
		if err := sd.validateMutation(name, val); err != nil {
			return err
		}
	}
	return sd.dict.SetKey(starlark.String(name), val)
}

// --- starlark.HasSetKey ---

// SetKey sets a key-value pair. For string keys, validates against the
// schema's field descriptor. Returns an error if frozen or invalid.
func (sd *SchemaDict) SetKey(k, v starlark.Value) error {
	if sd.schema != nil {
		if s, ok := k.(starlark.String); ok {
			if err := sd.validateMutation(string(s), v); err != nil {
				return err
			}
		}
	}
	return sd.dict.SetKey(k, v)
}

// validateMutation checks a single field assignment against the schema.
// It validates: unknown fields, type mismatches, enum violations, and
// nested schema compatibility. It does NOT check required (only relevant
// at construction time).
func (sd *SchemaDict) validateMutation(name string, val starlark.Value) error {
	fd, ok := sd.schema.fields[name]
	if !ok {
		return fmt.Errorf("%s: %s", sd.schema.name, unknownFieldError(name, sd.schema.order))
	}

	// None is allowed — it removes/clears the field.
	if val == starlark.None {
		return nil
	}

	// Nested schema validation.
	if fd.schema != nil {
		switch v := val.(type) {
		case *SchemaDict:
			_ = v // Already validated at construction.
		case *starlark.Dict:
			_, subErrs := fd.schema.validateFields(v.Items(), name)
			if len(subErrs) > 0 {
				return fmt.Errorf("%s.%s", sd.schema.name, subErrs[0])
			}
		default:
			return fmt.Errorf("%s.%s: expected %s or dict, got %s",
				sd.schema.name, name, fd.schema.name, val.Type())
		}
		return nil
	}

	// List items validation.
	if fd.items != nil && fd.typeName == "list" {
		list, ok := val.(*starlark.List)
		if !ok {
			return fmt.Errorf("%s.%s: expected list, got %s", sd.schema.name, name, val.Type())
		}
		for i := 0; i < list.Len(); i++ {
			elem := list.Index(i)
			elemPath := fmt.Sprintf("%s[%d]", name, i)
			switch v := elem.(type) {
			case *SchemaDict:
				_ = v // Already validated.
			case *starlark.Dict:
				_, subErrs := fd.items.validateFields(v.Items(), elemPath)
				if len(subErrs) > 0 {
					return fmt.Errorf("%s.%s", sd.schema.name, subErrs[0])
				}
			default:
				return fmt.Errorf("%s.%s: expected %s or dict, got %s",
					sd.schema.name, elemPath, fd.items.name, elem.Type())
			}
		}
		return nil
	}

	// Primitive type check.
	if fd.typeName != "" && !CheckType(val, fd.typeName) {
		return fmt.Errorf("%s.%s: expected %s, got %s (%s)",
			sd.schema.name, name, fd.typeName, val.Type(), val.String())
	}

	// Enum check.
	if fd.enum != nil && !checkEnum(val, fd.enum) {
		return fmt.Errorf("%s.%s", sd.schema.name, formatEnumError(name, val, fd.enum))
	}

	return nil
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
			if err := sd.SetKey(item[0], item[1]); err != nil {
				return nil, err
			}
		}
	case *starlark.Dict:
		for _, item := range o.Items() {
			if err := sd.SetKey(item[0], item[1]); err != nil {
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
	if err := sd.SetKey(key, dflt); err != nil {
		return nil, err
	}
	return dflt, nil
}
