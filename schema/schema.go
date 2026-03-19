package schema

import (
	"fmt"
	"strings"

	"go.starlark.net/starlark"
)

// SchemaCallable is a Starlark callable that validates kwargs at construction
// time and returns a plain *starlark.Dict. It is created by the schema() builtin.
type SchemaCallable struct {
	name   string
	doc    string
	fields map[string]*FieldDescriptor
	order  []string // insertion-order field names
	frozen bool
}

// Compile-time interface checks.
var (
	_ starlark.Value    = (*SchemaCallable)(nil)
	_ starlark.Callable = (*SchemaCallable)(nil)
	_ starlark.HasAttrs = (*SchemaCallable)(nil)
)

func (s *SchemaCallable) String() string        { return fmt.Sprintf("<schema %s>", s.name) }
func (s *SchemaCallable) Type() string           { return "schema" }
func (s *SchemaCallable) Name() string           { return s.name }
func (s *SchemaCallable) Truth() starlark.Bool   { return starlark.True }
func (s *SchemaCallable) Hash() (uint32, error)  { return 0, fmt.Errorf("unhashable type: schema") }

func (s *SchemaCallable) Freeze() {
	if s.frozen {
		return
	}
	s.frozen = true
	for _, fd := range s.fields {
		fd.Freeze()
	}
}

func (s *SchemaCallable) Attr(name string) (starlark.Value, error) {
	switch name {
	case "name":
		return starlark.String(s.name), nil
	case "doc":
		return starlark.String(s.doc), nil
	case "fields":
		d := starlark.NewDict(len(s.fields))
		for _, fname := range s.order {
			if err := d.SetKey(starlark.String(fname), s.fields[fname]); err != nil {
				return nil, err
			}
		}
		return d, nil
	}
	return nil, nil
}

func (s *SchemaCallable) AttrNames() []string {
	return []string{"doc", "fields", "name"}
}

// CallInternal validates kwargs against the schema fields and returns a
// *starlark.Dict on success, or an error listing all validation failures.
func (s *SchemaCallable) CallInternal(_ *starlark.Thread, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if len(args) > 0 {
		return nil, fmt.Errorf("%s: unexpected positional arguments", s.name)
	}

	var errs []string
	seen := make(map[string]bool, len(kwargs))
	result := starlark.NewDict(len(s.fields))

	// Phase 1: process provided kwargs.
	for _, kv := range kwargs {
		key := string(kv[0].(starlark.String))
		val := kv[1]

		fd, ok := s.fields[key]
		if !ok {
			errs = append(errs, unknownFieldError(key, s.order))
			continue
		}
		seen[key] = true

		// Handle None as omission.
		if val == starlark.None {
			if fd.required {
				errs = append(errs, fmt.Sprintf("%s: required field missing", key))
			} else if fd.defVal != starlark.None {
				_ = result.SetKey(starlark.String(key), fd.defVal)
			}
			// else: optional with no default, omit from result
			continue
		}

		// Type check.
		if fd.typeName != "" && !CheckType(val, fd.typeName) {
			errs = append(errs, fmt.Sprintf("%s: expected %s, got %s (%s)", key, fd.typeName, val.Type(), val.String()))
			continue
		}

		// Enum check.
		if fd.enum != nil && !checkEnum(val, fd.enum) {
			errs = append(errs, formatEnumError(key, val, fd.enum))
			continue
		}

		_ = result.SetKey(starlark.String(key), val)
	}

	// Phase 2: check for missing required fields and apply defaults.
	for _, name := range s.order {
		if seen[name] {
			continue
		}
		fd := s.fields[name]
		if fd.required {
			errs = append(errs, fmt.Sprintf("%s: required field missing", name))
		} else if fd.defVal != starlark.None {
			_ = result.SetKey(starlark.String(name), fd.defVal)
		}
	}

	if len(errs) > 0 {
		return nil, fmt.Errorf("%s: %d validation error%s:\n- %s",
			s.name, len(errs), pluralS(len(errs)), strings.Join(errs, "\n- "))
	}

	return result, nil
}

func pluralS(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// SchemaBuiltin returns a *starlark.Builtin implementing the schema() function.
func SchemaBuiltin() *starlark.Builtin {
	return starlark.NewBuiltin("schema", schemaImpl)
}

func schemaImpl(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("schema: requires exactly 1 positional argument (name), got %d", len(args))
	}
	nameVal, ok := args[0].(starlark.String)
	if !ok {
		return nil, fmt.Errorf("schema: name must be a string, got %s", args[0].Type())
	}
	name := string(nameVal)

	var doc string
	fields := make(map[string]*FieldDescriptor)
	var order []string

	for _, kv := range kwargs {
		key := string(kv[0].(starlark.String))
		val := kv[1]

		if key == "doc" {
			docStr, ok := val.(starlark.String)
			if !ok {
				return nil, fmt.Errorf("schema: doc must be a string, got %s", val.Type())
			}
			doc = string(docStr)
			continue
		}

		fd, ok := val.(*FieldDescriptor)
		if !ok {
			return nil, fmt.Errorf("schema: field %q value must be a field(), got %s", key, val.Type())
		}
		fields[key] = fd
		order = append(order, key)
	}

	return &SchemaCallable{
		name:   name,
		doc:    doc,
		fields: fields,
		order:  order,
	}, nil
}
