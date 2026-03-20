// Package schema provides Starlark builtins for typed schema validation.
package schema

import (
	"fmt"
	"strings"

	"go.starlark.net/starlark"
)

// FieldDescriptor holds metadata about a single schema field.
// It implements starlark.Value and starlark.HasAttrs for read-only
// introspection from Starlark code.
type FieldDescriptor struct {
	typeName string          // primitive type name ("string", "int", etc.)
	schema   *SchemaCallable // nested schema reference (mutually exclusive with typeName)
	items    *SchemaCallable // list element schema (only valid when typeName == "list")
	required bool
	defVal   starlark.Value
	enum     *starlark.List
	doc      string
	frozen   bool
}

// typeParam implements starlark.Unpacker for the dual-purpose type= kwarg.
// It accepts either a string (primitive type name) or a *SchemaCallable.
type typeParam struct {
	typeName string
	schema   *SchemaCallable
}

func (tp *typeParam) Unpack(v starlark.Value) error {
	switch v := v.(type) {
	case starlark.String:
		s := string(v)
		if s != "" && !validTypes[s] {
			return fmt.Errorf("invalid type %q; valid types: bool, dict, float, int, list, string", s)
		}
		tp.typeName = s
		return nil
	case *SchemaCallable:
		tp.schema = v
		return nil
	default:
		return fmt.Errorf("type= must be a string or schema, got %s", v.Type())
	}
}

// Compile-time interface checks.
var (
	_ starlark.Value    = (*FieldDescriptor)(nil)
	_ starlark.HasAttrs = (*FieldDescriptor)(nil)
)

// validTypes is the set of allowed type name strings for field(type=...).
// The empty string means "accept any type" (gradual typing).
var validTypes = map[string]bool{
	"":       true,
	"string": true,
	"int":    true,
	"float":  true,
	"bool":   true,
	"list":   true,
	"dict":   true,
}

func (f *FieldDescriptor) String() string {
	var parts []string
	if f.schema != nil {
		parts = append(parts, "type="+f.schema.name)
	} else if f.typeName != "" {
		parts = append(parts, "type="+f.typeName)
	}
	if f.items != nil {
		parts = append(parts, "items="+f.items.name)
	}
	if f.required {
		parts = append(parts, "required")
	}
	if f.defVal != nil && f.defVal != starlark.None {
		parts = append(parts, "default="+f.defVal.String())
	}
	if f.enum != nil {
		parts = append(parts, "enum="+f.enum.String())
	}
	if len(parts) == 0 {
		return "<field>"
	}
	return "<field " + strings.Join(parts, " ") + ">"
}

func (f *FieldDescriptor) Type() string         { return "field" }
func (f *FieldDescriptor) Truth() starlark.Bool  { return starlark.True }
func (f *FieldDescriptor) Hash() (uint32, error) { return 0, fmt.Errorf("unhashable type: field") }

func (f *FieldDescriptor) Freeze() {
	if f.frozen {
		return
	}
	f.frozen = true
	if f.defVal != nil && f.defVal != starlark.None {
		f.defVal.Freeze()
	}
	if f.enum != nil {
		f.enum.Freeze()
	}
}

func (f *FieldDescriptor) Attr(name string) (starlark.Value, error) {
	switch name {
	case "type":
		if f.schema != nil {
			return starlark.String(f.schema.name), nil
		}
		return starlark.String(f.typeName), nil
	case "required":
		return starlark.Bool(f.required), nil
	case "default":
		return f.defVal, nil
	case "enum":
		if f.enum != nil {
			return f.enum, nil
		}
		return starlark.None, nil
	case "doc":
		return starlark.String(f.doc), nil
	}
	return nil, nil
}

func (f *FieldDescriptor) AttrNames() []string {
	return []string{"default", "doc", "enum", "required", "type"}
}

// FieldBuiltin returns a *starlark.Builtin implementing the field() function.
func FieldBuiltin() *starlark.Builtin {
	return starlark.NewBuiltin("field", fieldImpl)
}

func fieldImpl(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kw []starlark.Tuple) (starlark.Value, error) {
	var (
		tp       typeParam
		required bool
		defVal   starlark.Value = starlark.None
		enumVal  starlark.Value = starlark.None
		doc      string
		itemsVal starlark.Value = starlark.None
	)

	if err := starlark.UnpackArgs(b.Name(), args, kw,
		"type?", &tp, "required?", &required,
		"default?", &defVal, "enum?", &enumVal,
		"doc?", &doc, "items?", &itemsVal); err != nil {
		return nil, err
	}

	// Validate items= kwarg.
	var items *SchemaCallable
	if itemsVal != starlark.None {
		var ok bool
		items, ok = itemsVal.(*SchemaCallable)
		if !ok {
			return nil, fmt.Errorf("field: items= must be a schema, got %s", itemsVal.Type())
		}
		if tp.typeName != "list" || tp.schema != nil {
			return nil, fmt.Errorf(`field: items= is only valid when type="list"`)
		}
	}

	// Validate enum is a list if provided.
	var enum *starlark.List
	if enumVal != starlark.None {
		var ok bool
		enum, ok = enumVal.(*starlark.List)
		if !ok {
			return nil, fmt.Errorf("field: enum must be a list, got %s", enumVal.Type())
		}
	}

	// Validate required + default mutual exclusion.
	if required && defVal != starlark.None {
		return nil, fmt.Errorf("field: required and default are mutually exclusive")
	}

	return &FieldDescriptor{
		typeName: tp.typeName,
		schema:   tp.schema,
		items:    items,
		required: required,
		defVal:   defVal,
		enum:     enum,
		doc:      doc,
	}, nil
}
