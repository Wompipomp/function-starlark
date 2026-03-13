package convert

import (
	"go.starlark.net/starlark"
	"google.golang.org/protobuf/types/known/structpb"
)

// StructToStarlark converts a protobuf Struct to a StarlarkDict.
// If freeze is true, the resulting dict tree is frozen (immutable).
func StructToStarlark(_ *structpb.Struct, _ bool) (*StarlarkDict, error) {
	return nil, nil
}

// StarlarkToStruct converts a StarlarkDict back to a protobuf Struct.
func StarlarkToStruct(_ *StarlarkDict) (*structpb.Struct, error) {
	return nil, nil
}

// Ensure starlark import is used (will be used in implementation).
var _ starlark.Value
