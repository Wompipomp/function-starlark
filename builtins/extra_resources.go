package builtins

import (
	"fmt"

	"go.starlark.net/starlark"
)

// getExtraResourceImpl implements get_extra_resource(name, path?, default=None).
// It looks up extraRes[name][0], optionally traverses a dot-path, and returns
// the default when the requirement is missing, the match list is empty/None,
// or the path segment is not found.
func getExtraResourceImpl(
	fnName string,
	extraRes *starlark.Dict,
	args starlark.Tuple,
	kwargs []starlark.Tuple,
) (starlark.Value, error) {
	var name string
	var path starlark.Value
	var dflt starlark.Value = starlark.None

	if err := starlark.UnpackArgs(fnName, args, kwargs,
		"name", &name, "path?", &path, "default?", &dflt); err != nil {
		return nil, err
	}

	if name == "" {
		return nil, fmt.Errorf("%s: name must not be empty", fnName)
	}

	// Look up extra resource by name.
	val, found, err := extraRes.Get(starlark.String(name))
	if err != nil {
		return nil, err
	}
	if !found || val == starlark.None {
		return dflt, nil
	}

	list, ok := val.(*starlark.List)
	if !ok || list.Len() == 0 {
		return dflt, nil
	}

	// Take the first matched resource.
	res := list.Index(0)

	// If no path given, return the full resource body.
	if path == nil || path == starlark.None {
		return res, nil
	}

	// Validate path is not empty.
	pathStr, ok := path.(starlark.String)
	if ok && string(pathStr) == "" {
		return nil, fmt.Errorf("%s: path must not be empty", fnName)
	}

	// Traverse the dot-path.
	keys, err := pathToKeys(path)
	if err != nil {
		return nil, err
	}

	current := res
	for _, key := range keys {
		mapping, ok := current.(starlark.Mapping)
		if !ok {
			return dflt, nil
		}
		v, found, err := mapping.Get(starlark.String(key))
		if err != nil || !found || v == starlark.None {
			return dflt, nil
		}
		current = v
	}
	return current, nil
}

// getExtraResourcesImpl implements get_extra_resources(name, path?, default=[]).
// It looks up all items in extraRes[name], optionally traverses a dot-path on
// each, and returns a list of values. Defaults to an empty list when the
// requirement is missing or the match list is empty/None.
func getExtraResourcesImpl(
	fnName string,
	extraRes *starlark.Dict,
	args starlark.Tuple,
	kwargs []starlark.Tuple,
) (starlark.Value, error) {
	var name string
	var path starlark.Value
	var dflt starlark.Value = starlark.NewList(nil)

	if err := starlark.UnpackArgs(fnName, args, kwargs,
		"name", &name, "path?", &path, "default?", &dflt); err != nil {
		return nil, err
	}

	if name == "" {
		return nil, fmt.Errorf("%s: name must not be empty", fnName)
	}

	// Look up extra resource by name.
	val, found, err := extraRes.Get(starlark.String(name))
	if err != nil {
		return nil, err
	}
	if !found || val == starlark.None {
		return dflt, nil
	}

	list, ok := val.(*starlark.List)
	if !ok {
		return dflt, nil
	}

	// If no path given, return a mutable copy of the list (all body dicts).
	if path == nil || path == starlark.None {
		items := make([]starlark.Value, list.Len())
		for i := 0; i < list.Len(); i++ {
			items[i] = list.Index(i)
		}
		return starlark.NewList(items), nil
	}

	// Traverse the dot-path on each item, collecting values where path exists.
	keys, err := pathToKeys(path)
	if err != nil {
		return nil, err
	}

	var collected []starlark.Value
	for i := 0; i < list.Len(); i++ {
		item := list.Index(i)
		current := item
		found := true
		for _, key := range keys {
			mapping, ok := current.(starlark.Mapping)
			if !ok {
				found = false
				break
			}
			v, exists, err := mapping.Get(starlark.String(key))
			if err != nil || !exists || v == starlark.None {
				found = false
				break
			}
			current = v
		}
		if found {
			collected = append(collected, current)
		}
	}
	return starlark.NewList(collected), nil
}
