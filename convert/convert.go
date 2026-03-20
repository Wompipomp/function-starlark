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
var maxSafeInt = new(big.Int).SetInt64(1 << 53) // 2^53

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

// dictFactory creates a dict-like Starlark value from protobuf struct fields.
// StructToStarlark passes a factory that produces *StarlarkDict;
// ProtoValueToPlainStarlark passes one that produces *starlark.Dict.
type dictFactory func(fields map[string]*structpb.Value, freeze bool) (starlark.Value, error)

// convertNumber converts a float64 to a Starlark numeric value with an int64
// overflow guard. Values that are whole numbers and within int64 range are
// returned as starlark.Int; values in the imprecise zone (|x| > 2^53) fall
// back to starlark.Float; values exceeding int64 range return an error.
func convertNumber(f float64) (starlark.Value, error) {
	if isWholeNumber(f) {
		// Guard: values at or beyond int64 boundaries must error.
		// float64(math.MaxInt64) rounds up to 2^63 exactly, so >= catches overflow.
		// float64(math.MinInt64) is exactly -2^63, so <= catches underflow.
		// Note: MinInt64 (-2^63) IS a valid int64 value, but we treat both boundaries
		// symmetrically with <= / >= for simplicity. This is moot in practice because
		// both float64(MaxInt64) and float64(MinInt64) fall in the imprecise zone
		// (|x| > 2^53), so the earlier imprecise-zone check returns them as
		// starlark.Float before this boundary check is reached.
		if f >= float64(math.MaxInt64) || f <= float64(math.MinInt64) {
			return nil, fmt.Errorf("value %g exceeds int64 range", f)
		}
		// Values beyond 2^53 lose float64 precision for integers.
		// Keep as Float to avoid silent precision loss.
		if f > float64(1<<53) || f < -float64(1<<53) {
			return starlark.Float(f), nil
		}
		return starlark.MakeInt64(int64(f)), nil
	}
	return starlark.Float(f), nil
}

// convertProtoValue is the shared conversion core for protobuf Value to
// Starlark. The mkDict callback controls whether struct values become
// *StarlarkDict or plain *starlark.Dict.
func convertProtoValue(v *structpb.Value, freeze bool, mkDict dictFactory) (starlark.Value, error) {
	if v == nil {
		return starlark.None, nil
	}

	switch kind := v.GetKind().(type) {
	case *structpb.Value_NullValue:
		return starlark.None, nil
	case *structpb.Value_NumberValue:
		return convertNumber(kind.NumberValue)
	case *structpb.Value_StringValue:
		return starlark.String(kind.StringValue), nil
	case *structpb.Value_BoolValue:
		return starlark.Bool(kind.BoolValue), nil
	case *structpb.Value_StructValue:
		return mkDict(kind.StructValue.GetFields(), freeze)
	case *structpb.Value_ListValue:
		return convertListValue(kind.ListValue, freeze, mkDict)
	case nil:
		return starlark.None, nil
	default:
		return nil, fmt.Errorf("unsupported protobuf value kind: %T", kind)
	}
}

// convertListValue converts a protobuf ListValue to a Starlark list, using
// convertProtoValue with the provided dictFactory for element conversion.
func convertListValue(lv *structpb.ListValue, freeze bool, mkDict dictFactory) (*starlark.List, error) {
	if lv == nil {
		l := starlark.NewList(nil)
		if freeze {
			l.Freeze()
		}
		return l, nil
	}

	vals := lv.GetValues()
	elems := make([]starlark.Value, len(vals))
	for i, v := range vals {
		sv, err := convertProtoValue(v, freeze, mkDict)
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

// isWholeNumber reports whether f is a finite integer value.
// IsInf and IsNaN are checked first to avoid treating Inf as a whole number
// (math.Trunc(Inf) == Inf is true).
func isWholeNumber(f float64) bool {
	return !math.IsInf(f, 0) && !math.IsNaN(f) && f == math.Trunc(f)
}

// protoValueToStarlark converts a single protobuf Value to its Starlark
// equivalent, producing *StarlarkDict for struct values.
func protoValueToStarlark(v *structpb.Value, freeze bool) (starlark.Value, error) {
	return convertProtoValue(v, freeze, func(fields map[string]*structpb.Value, freeze bool) (starlark.Value, error) {
		// Reconstruct a *structpb.Struct to reuse StructToStarlark's nil
		// handling and freeze logic.
		return StructToStarlark(&structpb.Struct{Fields: fields}, freeze)
	})
}

// ProtoValueToPlainStarlark converts a single protobuf Value to its Starlark
// equivalent, producing plain *starlark.Dict for struct values (not
// *StarlarkDict). This is used for context data where keys may contain dots
// and slashes that conflict with StarlarkDict's dot-access.
func ProtoValueToPlainStarlark(v *structpb.Value, freeze bool) (starlark.Value, error) {
	return convertProtoValue(v, freeze, plainDictFactory)
}

// plainDictFactory builds a plain *starlark.Dict from protobuf struct fields.
func plainDictFactory(fields map[string]*structpb.Value, freeze bool) (starlark.Value, error) {
	d := new(starlark.Dict)
	for k, fv := range fields {
		sv, err := convertProtoValue(fv, freeze, plainDictFactory)
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

// PlainDictToStruct converts a plain *starlark.Dict (as produced by dict
// literals in Starlark scripts) to a protobuf Struct. It rejects non-string
// keys and handles nested dicts recursively via starlarkToProtoValue.
func PlainDictToStruct(d *starlark.Dict) (*structpb.Struct, error) {
	fields := make(map[string]*structpb.Value, d.Len())
	for _, item := range d.Items() {
		k, ok := item[0].(starlark.String)
		if !ok {
			return nil, fmt.Errorf("dict key %v (%T) is not a string", item[0], item[0])
		}
		pv, err := starlarkToProtoValue(item[1])
		if err != nil {
			return nil, fmt.Errorf("field %q: %w", string(k), err)
		}
		fields[string(k)] = pv
	}
	return &structpb.Struct{Fields: fields}, nil
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
	case *starlark.Dict:
		s, err := PlainDictToStruct(v)
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
		type dictWrapper interface {
			InternalDict() *starlark.Dict
		}
		if w, ok := v.(dictWrapper); ok {
			s, err := PlainDictToStruct(w.InternalDict())
			if err != nil {
				return nil, err
			}
			return structpb.NewStructValue(s), nil
		}
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
