package schema

import (
	"testing"

	"go.starlark.net/starlark"

	"github.com/wompipomp/function-starlark/convert"
)

func TestCheckType(t *testing.T) {
	tests := []struct {
		name     string
		val      starlark.Value
		typeName string
		want     bool
	}{
		{"string match", starlark.String("hello"), "string", true},
		{"int match", starlark.MakeInt(42), "int", true},
		{"float match", starlark.Float(3.14), "float", true},
		{"bool match", starlark.Bool(true), "bool", true},
		{"list match", starlark.NewList(nil), "list", true},
		{"dict match", starlark.NewDict(0), "dict", true},
		{"type mismatch", starlark.String("hello"), "int", false},
		{"empty type accepts any", starlark.MakeInt(42), "", true},
		{"StarlarkDict as dict", convert.NewStarlarkDict(0), "dict", true},
		{"SchemaDict as dict", NewSchemaDict("Test", starlark.NewDict(0)), "dict", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CheckType(tt.val, tt.typeName)
			if got != tt.want {
				t.Errorf("CheckType(%v, %q) = %v, want %v", tt.val, tt.typeName, got, tt.want)
			}
		})
	}
}

func TestCheckEnum(t *testing.T) {
	enum := starlark.NewList([]starlark.Value{starlark.String("a"), starlark.String("b")})

	if !checkEnum(starlark.String("a"), enum) {
		t.Error("checkEnum(a, [a,b]) should return true")
	}
	if checkEnum(starlark.String("c"), enum) {
		t.Error("checkEnum(c, [a,b]) should return false")
	}
}

func TestFormatEnumError(t *testing.T) {
	enum := starlark.NewList([]starlark.Value{starlark.String("Standard_LRS"), starlark.String("Standard_GRS")})
	got := formatEnumError("sku", starlark.String("SuperFast"), enum)
	want := `sku: value "SuperFast" not in enum ["Standard_LRS", "Standard_GRS"]`
	if got != want {
		t.Errorf("formatEnumError:\n got: %s\nwant: %s", got, want)
	}
}

func TestUnknownFieldError(t *testing.T) {
	fields := []string{"location", "resourceGroupName", "sku", "tags"}

	t.Run("with close match", func(t *testing.T) {
		got := unknownFieldError("resourceGroupNme", fields)
		want := `resourceGroupNme: unknown field (did you mean "resourceGroupName"?)`
		if got != want {
			t.Errorf("got:  %s\nwant: %s", got, want)
		}
	})

	t.Run("no close match", func(t *testing.T) {
		got := unknownFieldError("xyzzy", fields)
		want := `xyzzy: unknown field; valid fields: location, resourceGroupName, sku, tags`
		if got != want {
			t.Errorf("got:  %s\nwant: %s", got, want)
		}
	})
}

func TestCheckTypeUnknownTypeName(t *testing.T) {
	got := CheckType(starlark.String("hello"), "unknown_type")
	if got {
		t.Error("CheckType with unknown type name should return false")
	}
}

func TestCheckEnumIntValues(t *testing.T) {
	enum := starlark.NewList([]starlark.Value{starlark.MakeInt(1), starlark.MakeInt(2), starlark.MakeInt(3)})

	if !checkEnum(starlark.MakeInt(2), enum) {
		t.Error("checkEnum(2, [1,2,3]) should return true")
	}
	if checkEnum(starlark.MakeInt(5), enum) {
		t.Error("checkEnum(5, [1,2,3]) should return false")
	}
}

func TestJoinPath(t *testing.T) {
	tests := []struct {
		prefix, field, want string
	}{
		{"", "location", "location"},
		{"template", "spec", "template.spec"},
		{"template.spec", "containers", "template.spec.containers"},
	}
	for _, tt := range tests {
		got := joinPath(tt.prefix, tt.field)
		if got != tt.want {
			t.Errorf("joinPath(%q, %q) = %q, want %q", tt.prefix, tt.field, got, tt.want)
		}
	}
}

func TestUnknownFieldErrorPath(t *testing.T) {
	fields := []string{"name", "image"}

	t.Run("with path prefix and suggestion", func(t *testing.T) {
		got := unknownFieldErrorPath("containers[0].nme", "nme", fields)
		want := `containers[0].nme: unknown field (did you mean "name"?)`
		if got != want {
			t.Errorf("got:  %s\nwant: %s", got, want)
		}
	})

	t.Run("with path prefix no suggestion", func(t *testing.T) {
		got := unknownFieldErrorPath("spec.xyzzy", "xyzzy", fields)
		want := `spec.xyzzy: unknown field; valid fields: name, image`
		if got != want {
			t.Errorf("got:  %s\nwant: %s", got, want)
		}
	})
}

func TestCheckEnumEmptyList(t *testing.T) {
	enum := starlark.NewList(nil)
	if checkEnum(starlark.String("anything"), enum) {
		t.Error("checkEnum with empty list should always return false")
	}
}

func TestUnknownFieldErrorPathEmptyPrefix(t *testing.T) {
	fields := []string{"name", "image"}
	// When path and fieldName are the same (empty prefix scenario),
	// it should behave identically to unknownFieldError.
	got := unknownFieldErrorPath("nme", "nme", fields)
	want := unknownFieldError("nme", fields)
	if got != want {
		t.Errorf("unknownFieldErrorPath with same path/field:\n got: %s\nwant: %s", got, want)
	}
}
