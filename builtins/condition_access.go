package builtins

import (
	"fmt"

	"go.starlark.net/starlark"

	"github.com/wompipomp/function-starlark/convert"
)

// conditionFields are the keys always present in the returned condition dict.
var conditionFields = []string{"status", "reason", "message", "lastTransitionTime"}

// getConditionImpl implements get_condition(name, type).
// It looks up the named resource in observed, traverses status.conditions,
// and returns a new unfrozen *starlark.Dict with the 4 standard condition
// fields (missing fields default to empty string). Returns None if the
// resource is not found or the condition type is not present.
func getConditionImpl(
	fnName string,
	observed *convert.StarlarkDict,
	args starlark.Tuple,
	kwargs []starlark.Tuple,
) (starlark.Value, error) {
	var name, condType string

	if err := starlark.UnpackArgs(fnName, args, kwargs,
		"name", &name, "type", &condType); err != nil {
		return nil, err
	}

	if name == "" {
		return nil, fmt.Errorf("%s: name must not be empty", fnName)
	}
	if condType == "" {
		return nil, fmt.Errorf("%s: type must not be empty", fnName)
	}

	// Look up resource in observed.
	res, found, err := observed.Get(starlark.String(name))
	if err != nil {
		return nil, err
	}
	if !found || res == starlark.None {
		return starlark.None, nil
	}

	// Traverse: resource -> "status".
	resMapping, ok := res.(starlark.Mapping)
	if !ok {
		return starlark.None, nil
	}
	statusVal, found, err := resMapping.Get(starlark.String("status"))
	if err != nil || !found || statusVal == starlark.None {
		return starlark.None, nil
	}

	// Traverse: status -> "conditions".
	statusMapping, ok := statusVal.(starlark.Mapping)
	if !ok {
		return starlark.None, nil
	}
	conditionsVal, found, err := statusMapping.Get(starlark.String("conditions"))
	if err != nil || !found || conditionsVal == starlark.None {
		return starlark.None, nil
	}

	// Iterate conditions list.
	condList, ok := conditionsVal.(starlark.Indexable)
	if !ok {
		return starlark.None, nil
	}

	for i := 0; i < condList.Len(); i++ {
		item := condList.Index(i)
		itemMapping, ok := item.(starlark.Mapping)
		if !ok {
			continue
		}

		// Check if this condition's "type" matches.
		typeVal, found, err := itemMapping.Get(starlark.String("type"))
		if err != nil || !found {
			continue
		}
		typeStr, ok := typeVal.(starlark.String)
		if !ok || string(typeStr) != condType {
			continue
		}

		// Build a new unfrozen dict with all 4 fields.
		result := new(starlark.Dict)
		for _, field := range conditionFields {
			val, found, err := itemMapping.Get(starlark.String(field))
			if err != nil || !found || val == starlark.None {
				val = starlark.String("")
			}
			if err := result.SetKey(starlark.String(field), val); err != nil {
				return nil, err
			}
		}
		return result, nil
	}

	return starlark.None, nil
}
