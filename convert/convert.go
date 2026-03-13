package convert

import (
	"fmt"
	"math"
	"math/big"

	"go.starlark.net/starlark"
	"google.golang.org/protobuf/types/known/structpb"
)

// maxSafeInt is the largest integer that can be exactly represented as float64.
// Beyond this, float64 loses precision for integer values.
var maxSafeInt = new(big.Int).SetInt64(1 << 53)  // 2^53
var minSafeInt = new(big.Int).Neg(maxSafeInt)     // -2^53

// StructToStarlark converts a protobuf Struct to a StarlarkDict.
// If freeze is true, the resulting dict tree is frozen (immutable).
func StructToStarlark(s *structpb.Struct, freeze bool) (*StarlarkDict, error) {
	if s == nil {
		d := NewStarlarkDict(0)
		if freeze {
			d.Freeze()
		}
		return d, nil
	}

	fields := s.GetFields()
	d := NewStarlarkDict(len(fields))

	for k, v := range fields {
		sv, err := protoValueToStarlark(v, freeze)
		if err != nil {
			return nil, fmt.Errorf("field %q: %w", k, err)
		}
		if err := d.SetKey(starlark.String(k), sv); err != nil {
			return nil, fmt.Errorf("field %q: %w", k, err)
		}
	}

	if freeze {
		d.Freeze()
	}
	return d, nil
}

// protoValueToStarlark converts a single protobuf Value to its Starlark equivalent.
func protoValueToStarlark(v *structpb.Value, freeze bool) (starlark.Value, error) {
	if v == nil {
		return starlark.None, nil
	}

	switch kind := v.GetKind().(type) {
	case *structpb.Value_NullValue:
		return starlark.None, nil
	case *structpb.Value_NumberValue:
		f := kind.NumberValue
		if isWholeNumber(f) {
			return starlark.MakeInt64(int64(f)), nil
		}
		return starlark.Float(f), nil
	case *structpb.Value_StringValue:
		return starlark.String(kind.StringValue), nil
	case *structpb.Value_BoolValue:
		return starlark.Bool(kind.BoolValue), nil
	case *structpb.Value_StructValue:
		return StructToStarlark(kind.StructValue, freeze)
	case *structpb.Value_ListValue:
		return listValueToStarlarkList(kind.ListValue, freeze)
	case nil:
		return starlark.None, nil
	default:
		return nil, fmt.Errorf("unsupported protobuf value kind: %T", kind)
	}
}

// isWholeNumber reports whether f is a finite integer value.
// IsInf and IsNaN are checked first to avoid treating Inf as a whole number
// (math.Trunc(Inf) == Inf is true).
func isWholeNumber(f float64) bool {
	return !math.IsInf(f, 0) && !math.IsNaN(f) && f == math.Trunc(f)
}

// listValueToStarlarkList converts a protobuf ListValue to a Starlark list.
func listValueToStarlarkList(lv *structpb.ListValue, freeze bool) (*starlark.List, error) {
	if lv == nil {
		return starlark.NewList(nil), nil
	}

	vals := lv.GetValues()
	elems := make([]starlark.Value, len(vals))
	for i, v := range vals {
		sv, err := protoValueToStarlark(v, freeze)
		if err != nil {
			return nil, fmt.Errorf("list[%d]: %w", i, err)
		}
		elems[i] = sv
	}

	list := starlark.NewList(elems)
	if freeze {
		list.Freeze()
	}
	return list, nil
}

// StarlarkToStruct converts a StarlarkDict back to a protobuf Struct.
func StarlarkToStruct(d *StarlarkDict) (*structpb.Struct, error) {
	fields := make(map[string]*structpb.Value, d.Len())

	iter := d.Iterate()
	defer iter.Done()

	var key starlark.Value
	for iter.Next(&key) {
		k, ok := key.(starlark.String)
		if !ok {
			return nil, fmt.Errorf("dict key %v (%T) is not a string", key, key)
		}

		val, _, err := d.Get(key)
		if err != nil {
			return nil, fmt.Errorf("field %q: %w", string(k), err)
		}

		pv, err := starlarkToProtoValue(val)
		if err != nil {
			return nil, fmt.Errorf("field %q: %w", string(k), err)
		}
		fields[string(k)] = pv
	}

	return &structpb.Struct{Fields: fields}, nil
}

// starlarkToProtoValue converts a single Starlark value to its protobuf equivalent.
func starlarkToProtoValue(v starlark.Value) (*structpb.Value, error) {
	switch v := v.(type) {
	case starlark.NoneType:
		return structpb.NewNullValue(), nil
	case starlark.Bool:
		return structpb.NewBoolValue(bool(v)), nil
	case starlark.Int:
		bi := v.BigInt()
		if bi.CmpAbs(maxSafeInt) > 0 {
			return nil, fmt.Errorf("integer too large for protobuf number: %s", v.String())
		}
		f, _ := starlark.AsFloat(v)
		return structpb.NewNumberValue(f), nil
	case starlark.Float:
		return structpb.NewNumberValue(float64(v)), nil
	case starlark.String:
		return structpb.NewStringValue(string(v)), nil
	case *StarlarkDict:
		s, err := StarlarkToStruct(v)
		if err != nil {
			return nil, err
		}
		return structpb.NewStructValue(s), nil
	case *starlark.List:
		lv, err := starlarkListToListValue(v)
		if err != nil {
			return nil, err
		}
		return structpb.NewListValue(lv), nil
	default:
		return nil, fmt.Errorf("unsupported starlark type for protobuf conversion: %s", v.Type())
	}
}

// starlarkListToListValue converts a Starlark list to a protobuf ListValue.
func starlarkListToListValue(l *starlark.List) (*structpb.ListValue, error) {
	vals := make([]*structpb.Value, l.Len())
	for i := 0; i < l.Len(); i++ {
		pv, err := starlarkToProtoValue(l.Index(i))
		if err != nil {
			return nil, fmt.Errorf("list[%d]: %w", i, err)
		}
		vals[i] = pv
	}
	return &structpb.ListValue{Values: vals}, nil
}
