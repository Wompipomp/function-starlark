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
func (s *SchemaCallable) Type() string          { return "schema" }
func (s *SchemaCallable) Name() string          { return s.name }
func (s *SchemaCallable) Truth() starlark.Bool  { return starlark.True }
func (s *SchemaCallable) Hash() (uint32, error) { return 0, fmt.Errorf("unhashable type: schema") }

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
// *SchemaDict on success, or an error listing all validation failures.
func (s *SchemaCallable) CallInternal(_ *starlark.Thread, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if len(args) > 0 {
		return nil, fmt.Errorf("%s: unexpected positional arguments", s.name)
	}

	result, errs := s.validateFields(kwargs, "")
	if len(errs) > 0 {
		return nil, fmt.Errorf("%s: %d validation error%s:\n- %s",
			s.name, len(errs), pluralS(len(errs)), strings.Join(errs, "\n- "))
	}

	return NewSchemaDict(s, result), nil
}

// validateFields performs recursive validation returning a raw dict and error
// strings with full path prefixes. prefix is the current field path (empty for
// top-level).
func (s *SchemaCallable) validateFields(kwargs []starlark.Tuple, prefix string) (*starlark.Dict, []string) {
	var errs []string
	seen := make(map[string]bool, len(kwargs))
	result := starlark.NewDict(len(s.fields))

	// Phase 1: process provided kwargs.
	for _, kv := range kwargs {
		key := string(kv[0].(starlark.String))
		val := kv[1]
		fieldPath := joinPath(prefix, key)

		fd, ok := s.fields[key]
		if !ok {
			errs = append(errs, unknownFieldErrorPath(fieldPath, key, s.order))
			continue
		}
		seen[key] = true

		// Handle None as omission.
		if val == starlark.None {
			if fd.required {
				errs = append(errs, fmt.Sprintf("%s: required field missing", fieldPath))
			} else if fd.defVal != starlark.None {
				_ = result.SetKey(starlark.String(key), fd.defVal)
			}
			// else: optional with no default, omit from result
			continue
		}

		// Nested schema validation.
		if fd.schema != nil {
			switch v := val.(type) {
			case *SchemaDict:
				// Already validated, skip re-validation.
				_ = result.SetKey(starlark.String(key), v)
			case *starlark.Dict:
				// Validate plain dict against sub-schema.
				subResult, subErrs := fd.schema.validateFields(v.Items(), fieldPath)
				errs = append(errs, subErrs...)
				if len(subErrs) == 0 {
					_ = result.SetKey(starlark.String(key), NewSchemaDict(fd.schema, subResult))
				}
			default:
				errs = append(errs, fmt.Sprintf("%s: expected %s or dict, got %s",
					fieldPath, fd.schema.name, val.Type()))
			}
			continue
		}

		// List items validation.
		if fd.items != nil && fd.typeName == "list" {
			list, ok := val.(*starlark.List)
			if !ok {
				errs = append(errs, fmt.Sprintf("%s: expected list, got %s", fieldPath, val.Type()))
				continue
			}
			validatedList := make([]starlark.Value, 0, list.Len())
			listHasErrors := false
			for i := 0; i < list.Len(); i++ {
				elem := list.Index(i)
				elemPath := fmt.Sprintf("%s[%d]", fieldPath, i)
				switch v := elem.(type) {
				case *SchemaDict:
					validatedList = append(validatedList, v)
				case *starlark.Dict:
					subResult, subErrs := fd.items.validateFields(v.Items(), elemPath)
					errs = append(errs, subErrs...)
					if len(subErrs) == 0 {
						validatedList = append(validatedList, NewSchemaDict(fd.items, subResult))
					} else {
						listHasErrors = true
					}
				default:
					errs = append(errs, fmt.Sprintf("%s: expected %s or dict, got %s",
						elemPath, fd.items.name, elem.Type()))
					listHasErrors = true
				}
			}
			if !listHasErrors {
				_ = result.SetKey(starlark.String(key), starlark.NewList(validatedList))
			}
			continue
		}

		// Primitive type check.
		if fd.typeName != "" && !CheckType(val, fd.typeName) {
			errs = append(errs, fmt.Sprintf("%s: expected %s, got %s (%s)", fieldPath, fd.typeName, val.Type(), val.String()))
			continue
		}

		// Enum check.
		if fd.enum != nil && !checkEnum(val, fd.enum) {
			errs = append(errs, formatEnumError(fieldPath, val, fd.enum))
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
		fieldPath := joinPath(prefix, name)
		if fd.required {
			errs = append(errs, fmt.Sprintf("%s: required field missing", fieldPath))
		} else if fd.defVal != starlark.None {
			_ = result.SetKey(starlark.String(name), fd.defVal)
		}
	}

	return result, errs
}

// ValidateFields is the exported form of validateFields for cross-package use.
// It performs recursive validation returning a raw dict and error strings.
func (s *SchemaCallable) ValidateFields(kwargs []starlark.Tuple, prefix string) (*starlark.Dict, []string) {
	return s.validateFields(kwargs, prefix)
}

// ValidateMutation validates a single field mutation and returns the processed
// value to store. The return semantics are:
//   - (processedVal, nil): store processedVal in the dict
//   - (nil, nil): delete the key from the dict (None on optional without default)
//   - (nil, error): validation failed
func (s *SchemaCallable) ValidateMutation(name string, val starlark.Value) (starlark.Value, error) {
	fd, ok := s.fields[name]
	if !ok {
		return nil, fmt.Errorf("%s: %s", s.name, unknownFieldError(name, s.order))
	}

	// None handling.
	if val == starlark.None {
		if fd.required {
			return nil, fmt.Errorf("%s.%s: required field cannot be set to None", s.name, name)
		}
		if fd.defVal != starlark.None {
			return fd.defVal, nil
		}
		return nil, nil // signal "delete key"
	}

	// Nested schema validation.
	if fd.schema != nil {
		switch v := val.(type) {
		case *SchemaDict:
			return v, nil // already validated
		case *starlark.Dict:
			subResult, subErrs := fd.schema.validateFields(v.Items(), name)
			if len(subErrs) > 0 {
				return nil, fmt.Errorf("%s.%s", s.name, subErrs[0])
			}
			return NewSchemaDict(fd.schema, subResult), nil
		case interface{ InternalDict() *starlark.Dict }:
			subResult, subErrs := fd.schema.validateFields(v.InternalDict().Items(), name)
			if len(subErrs) > 0 {
				return nil, fmt.Errorf("%s.%s", s.name, subErrs[0])
			}
			return NewSchemaDict(fd.schema, subResult), nil
		default:
			return nil, fmt.Errorf("%s.%s: expected %s or dict, got %s",
				s.name, name, fd.schema.name, val.Type())
		}
	}

	// List items validation.
	if fd.items != nil && fd.typeName == "list" {
		list, ok := val.(*starlark.List)
		if !ok {
			return nil, fmt.Errorf("%s.%s: expected list, got %s", s.name, name, val.Type())
		}
		validatedList := make([]starlark.Value, 0, list.Len())
		for i := 0; i < list.Len(); i++ {
			elem := list.Index(i)
			elemPath := fmt.Sprintf("%s[%d]", name, i)
			switch v := elem.(type) {
			case *SchemaDict:
				validatedList = append(validatedList, v)
			case *starlark.Dict:
				subResult, subErrs := fd.items.validateFields(v.Items(), elemPath)
				if len(subErrs) > 0 {
					return nil, fmt.Errorf("%s.%s", s.name, subErrs[0])
				}
				validatedList = append(validatedList, NewSchemaDict(fd.items, subResult))
			default:
				return nil, fmt.Errorf("%s.%s: expected %s or dict, got %s",
					s.name, elemPath, fd.items.name, elem.Type())
			}
		}
		return starlark.NewList(validatedList), nil
	}

	// Primitive type check.
	if fd.typeName != "" && !CheckType(val, fd.typeName) {
		return nil, fmt.Errorf("%s.%s: expected %s, got %s (%s)",
			s.name, name, fd.typeName, val.Type(), val.String())
	}

	// Enum check.
	if fd.enum != nil && !checkEnum(val, fd.enum) {
		return nil, fmt.Errorf("%s.%s", s.name, formatEnumError(name, val, fd.enum))
	}

	return val, nil
}

// FieldNames returns the schema's field names in insertion order.
func (s *SchemaCallable) FieldNames() []string {
	return s.order
}

// NewSchemaCallable creates a SchemaCallable for cross-package construction
// (primarily used in tests).
func NewSchemaCallable(name, doc string, fields map[string]*FieldDescriptor, order []string) *SchemaCallable {
	return &SchemaCallable{
		name:   name,
		doc:    doc,
		fields: fields,
		order:  order,
	}
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
