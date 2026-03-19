package schema

import (
	"fmt"
	"strings"

	"go.starlark.net/starlark"

	"github.com/wompipomp/function-starlark/convert"
)

// CheckType reports whether val matches the given Starlark type name.
// An empty typeName matches any value (gradual typing).
func CheckType(val starlark.Value, typeName string) bool {
	if typeName == "" {
		return true
	}
	switch typeName {
	case "string":
		_, ok := val.(starlark.String)
		return ok
	case "int":
		_, ok := val.(starlark.Int)
		return ok
	case "float":
		_, ok := val.(starlark.Float)
		return ok
	case "bool":
		_, ok := val.(starlark.Bool)
		return ok
	case "list":
		_, ok := val.(*starlark.List)
		return ok
	case "dict":
		if _, ok := val.(*starlark.Dict); ok {
			return true
		}
		if _, ok := val.(*convert.StarlarkDict); ok {
			return true
		}
		return false
	default:
		return false
	}
}

// checkEnum reports whether val is contained in the enum list.
func checkEnum(val starlark.Value, enum *starlark.List) bool {
	for i := 0; i < enum.Len(); i++ {
		if ok, _ := starlark.Equal(val, enum.Index(i)); ok {
			return true
		}
	}
	return false
}

// formatEnumError produces a formatted enum violation message.
func formatEnumError(fieldName string, val starlark.Value, enum *starlark.List) string {
	return fmt.Sprintf("%s: value %s not in enum %s", fieldName, val.String(), enum.String())
}

// unknownFieldError produces a formatted unknown-field message, optionally
// suggesting the closest match using Levenshtein distance.
func unknownFieldError(fieldName string, validFields []string) string {
	if suggestion := Suggest(fieldName, validFields); suggestion != "" {
		return fmt.Sprintf("%s: unknown field (did you mean %q?)", fieldName, suggestion)
	}
	return fmt.Sprintf("%s: unknown field; valid fields: %s", fieldName, strings.Join(validFields, ", "))
}
