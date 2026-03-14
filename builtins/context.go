package builtins

import (
	"fmt"
	"math"

	fnv1 "github.com/crossplane/function-sdk-go/proto/v1"
	"go.starlark.net/starlark"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/wompipomp/function-starlark/convert"
)

// environmentKey is the well-known context key for EnvironmentConfig data.
const environmentKey = "apiextensions.crossplane.io/environment"

// buildContextDict converts the pipeline context from a RunFunctionRequest
// into a mutable plain *starlark.Dict. Plain starlark.Dict is used because
// context keys contain dots and slashes (e.g., "apiextensions.crossplane.io/environment")
// which would conflict with StarlarkDict dot-access.
func buildContextDict(req *fnv1.RunFunctionRequest) (*starlark.Dict, error) {
	ctx := req.GetContext()
	if ctx == nil {
		return new(starlark.Dict), nil
	}

	d := new(starlark.Dict)
	for k, v := range ctx.GetFields() {
		sv, err := protoValueToStarlarkValue(v, false)
		if err != nil {
			return nil, fmt.Errorf("context key %q: %w", k, err)
		}
		if err := d.SetKey(starlark.String(k), sv); err != nil {
			return nil, fmt.Errorf("context key %q: %w", k, err)
		}
	}
	return d, nil
}

// buildEnvironmentDict extracts the EnvironmentConfig data from the pipeline
// context and returns it as a frozen StarlarkDict. If the environment key is
// missing, nil, or not a struct value, an empty frozen StarlarkDict is returned.
func buildEnvironmentDict(req *fnv1.RunFunctionRequest) (*convert.StarlarkDict, error) {
	ctx := req.GetContext()
	if ctx == nil {
		d := convert.NewStarlarkDict(0)
		d.Freeze()
		return d, nil
	}

	envVal, ok := ctx.GetFields()[environmentKey]
	if !ok || envVal == nil || envVal.GetStructValue() == nil {
		d := convert.NewStarlarkDict(0)
		d.Freeze()
		return d, nil
	}

	return convert.StructToStarlark(envVal.GetStructValue(), true) // frozen
}

// ApplyContext converts the mutable context *starlark.Dict back to a protobuf
// Struct and merges it into the existing rsp.Context. Keys present in the
// Starlark dict overwrite existing keys; keys only in rsp.Context are preserved.
// This ensures downstream pipeline functions do not lose context keys that
// this script did not modify.
func ApplyContext(rsp *fnv1.RunFunctionResponse, ctxVal starlark.Value) error {
	d, ok := ctxVal.(*starlark.Dict)
	if !ok {
		return fmt.Errorf("context is %T, want *starlark.Dict", ctxVal)
	}

	s, err := convert.PlainDictToStruct(d)
	if err != nil {
		return fmt.Errorf("converting context: %w", err)
	}

	if rsp.Context == nil {
		rsp.Context = s
		return nil
	}

	// Merge: script keys overwrite existing, existing-only keys preserved.
	for k, v := range s.GetFields() {
		rsp.Context.Fields[k] = v
	}
	return nil
}

// protoValueToStarlarkValue converts a single protobuf Value to its Starlark
// equivalent, producing plain *starlark.Dict for struct values (not StarlarkDict)
// so that the resulting tree is consistent for context usage.
func protoValueToStarlarkValue(v *structpb.Value, freeze bool) (starlark.Value, error) {
	if v == nil {
		return starlark.None, nil
	}

	switch kind := v.GetKind().(type) {
	case *structpb.Value_NullValue:
		return starlark.None, nil
	case *structpb.Value_NumberValue:
		f := kind.NumberValue
		if !math.IsInf(f, 0) && !math.IsNaN(f) && f == math.Trunc(f) {
			return starlark.MakeInt64(int64(f)), nil
		}
		return starlark.Float(f), nil
	case *structpb.Value_StringValue:
		return starlark.String(kind.StringValue), nil
	case *structpb.Value_BoolValue:
		return starlark.Bool(kind.BoolValue), nil
	case *structpb.Value_StructValue:
		d := new(starlark.Dict)
		if kind.StructValue != nil {
			for k, fv := range kind.StructValue.GetFields() {
				sv, err := protoValueToStarlarkValue(fv, freeze)
				if err != nil {
					return nil, fmt.Errorf("field %q: %w", k, err)
				}
				if err := d.SetKey(starlark.String(k), sv); err != nil {
					return nil, fmt.Errorf("field %q: %w", k, err)
				}
			}
		}
		if freeze {
			d.Freeze()
		}
		return d, nil
	case *structpb.Value_ListValue:
		if kind.ListValue == nil {
			l := starlark.NewList(nil)
			if freeze {
				l.Freeze()
			}
			return l, nil
		}
		vals := kind.ListValue.GetValues()
		elems := make([]starlark.Value, len(vals))
		for i, lv := range vals {
			sv, err := protoValueToStarlarkValue(lv, freeze)
			if err != nil {
				return nil, fmt.Errorf("list[%d]: %w", i, err)
			}
			elems[i] = sv
		}
		l := starlark.NewList(elems)
		if freeze {
			l.Freeze()
		}
		return l, nil
	case nil:
		return starlark.None, nil
	default:
		return nil, fmt.Errorf("unsupported protobuf value kind: %T", kind)
	}
}
