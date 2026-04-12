package builtins

import (
	"fmt"

	"go.starlark.net/starlark"

	"github.com/wompipomp/function-starlark/convert"
)

// isObservedImpl implements is_observed(name).
// Returns True if the named resource exists in the observed dict, False otherwise.
func isObservedImpl(
	fnName string,
	observed *convert.StarlarkDict,
	args starlark.Tuple,
	kwargs []starlark.Tuple,
) (starlark.Value, error) {
	var name string

	if err := starlark.UnpackArgs(fnName, args, kwargs,
		"name", &name); err != nil {
		return nil, err
	}

	if name == "" {
		return nil, fmt.Errorf("%s: name must not be empty", fnName)
	}

	_, found, err := observed.Get(starlark.String(name))
	if err != nil {
		return nil, err
	}
	return starlark.Bool(found), nil
}

// observedBodyImpl implements observed_body(name, default=None).
// Returns the full observed resource body dict, or default if not found.
func observedBodyImpl(
	fnName string,
	observed *convert.StarlarkDict,
	args starlark.Tuple,
	kwargs []starlark.Tuple,
) (starlark.Value, error) {
	var name string
	var dflt starlark.Value = starlark.None

	if err := starlark.UnpackArgs(fnName, args, kwargs,
		"name", &name, "default?", &dflt); err != nil {
		return nil, err
	}

	if name == "" {
		return nil, fmt.Errorf("%s: name must not be empty", fnName)
	}

	val, found, err := observed.Get(starlark.String(name))
	if err != nil {
		return nil, err
	}
	if !found || val == starlark.None {
		return dflt, nil
	}
	return val, nil
}
