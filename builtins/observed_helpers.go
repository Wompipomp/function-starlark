package builtins

import (
	"go.starlark.net/starlark"

	"github.com/wompipomp/function-starlark/convert"
)

// isObservedImpl implements is_observed(ref). ref is a name or a Resource() handle.
// Returns True if the named resource exists in the observed dict, False otherwise.
func isObservedImpl(
	fnName string,
	observed *convert.StarlarkDict,
	args starlark.Tuple,
	kwargs []starlark.Tuple,
) (starlark.Value, error) {
	var nameVal starlark.Value

	if err := starlark.UnpackArgs(fnName, args, kwargs,
		"name", &nameVal); err != nil {
		return nil, err
	}
	name, err := refName(fnName, nameVal)
	if err != nil {
		return nil, err
	}

	_, found, err := observed.Get(starlark.String(name))
	if err != nil {
		return nil, err
	}
	return starlark.Bool(found), nil
}

// observedBodyImpl implements observed_body(ref, default=None). Deprecated: use get_observed(ref).
// Returns the full observed resource body dict, or default if not found.
func observedBodyImpl(
	fnName string,
	observed *convert.StarlarkDict,
	args starlark.Tuple,
	kwargs []starlark.Tuple,
) (starlark.Value, error) {
	var nameVal starlark.Value
	var dflt starlark.Value = starlark.None

	if err := starlark.UnpackArgs(fnName, args, kwargs,
		"name", &nameVal, "default?", &dflt); err != nil {
		return nil, err
	}
	name, err := refName(fnName, nameVal)
	if err != nil {
		return nil, err
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
