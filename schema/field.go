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
	typeName string
	required bool
	defVal   starlark.Value
	enum     *starlark.List
	doc      string
	frozen   bool
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
	if f.typeName != "" {
		parts = append(parts, "type="+f.typeName)
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
		typeName string
		required bool
		defVal   starlark.Value = starlark.None
		enumVal  starlark.Value = starlark.None
		doc      string
	)

	if err := starlark.UnpackArgs(b.Name(), args, kw,
		"type?", &typeName, "required?", &required,
		"default?", &defVal, "enum?", &enumVal, "doc?", &doc); err != nil {
		return nil, err
	}

	// Validate type name.
	if !validTypes[typeName] {
		return nil, fmt.Errorf("field: invalid type %q; valid types: bool, dict, float, int, list, string", typeName)
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
		typeName: typeName,
		required: required,
		defVal:   defVal,
		enum:     enum,
		doc:      doc,
	}, nil
}
