package builtins

import (
	"fmt"
	"strings"

	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"

	"github.com/wompipomp/function-starlark/convert"
	"github.com/wompipomp/function-starlark/schema"
)

// maxMergeDepth is the maximum recursion depth for dict.deep_merge.
const maxMergeDepth = 32

// DictModule is the predeclared "dict" namespace module.
// It provides dict manipulation functions for safe merging, filtering,
// and nested path traversal of Kubernetes-style dictionaries.
var DictModule = &starlarkstruct.Module{
	Name: "dict",
	Members: starlark.StringDict{
		"merge":      starlark.NewBuiltin("dict.merge", dictMergeImpl),
		"deep_merge": starlark.NewBuiltin("dict.deep_merge", dictDeepMergeImpl),
		"pick":       starlark.NewBuiltin("dict.pick", dictPickImpl),
		"omit":       starlark.NewBuiltin("dict.omit", dictOmitImpl),
		"compact":    starlark.NewBuiltin("dict.compact", dictCompactImpl),
		"dig":        starlark.NewBuiltin("dict.dig", dictDigImpl),
		"has_path":   starlark.NewBuiltin("dict.has_path", dictHasPathImpl),
	},
}

// dictItems extracts key-value tuples from any dict-like Starlark value.
// It supports *starlark.Dict, *convert.StarlarkDict, and *schema.SchemaDict.
func dictItems(fnName string, v starlark.Value) ([]starlark.Tuple, error) {
	switch d := v.(type) {
	case *starlark.Dict:
		return d.Items(), nil
	case *convert.StarlarkDict:
		return d.InternalDict().Items(), nil
	case *schema.SchemaDict:
		return d.InternalDict().Items(), nil
	default:
		return nil, fmt.Errorf("%s: got %s, want dict", fnName, v.Type())
	}
}

// isDict returns true if v is any of the supported dict types.
func isDict(v starlark.Value) bool {
	switch v.(type) {
	case *starlark.Dict, *convert.StarlarkDict, *schema.SchemaDict:
		return true
	default:
		return false
	}
}

// asMapping returns the starlark.Mapping interface for any supported dict type.
func asMapping(v starlark.Value) (starlark.Mapping, bool) {
	switch d := v.(type) {
	case *starlark.Dict:
		return d, true
	case *convert.StarlarkDict:
		return d, true
	case *schema.SchemaDict:
		return d, true
	default:
		return nil, false
	}
}

// dictMergeImpl implements dict.merge(d1, d2, ...) -> new dict with shallow right-wins merge.
// Requires at least 2 positional arguments, no kwargs.
func dictMergeImpl(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if len(kwargs) > 0 {
		return nil, fmt.Errorf("%s: unexpected keyword arguments", b.Name())
	}
	if len(args) < 2 {
		return nil, fmt.Errorf("%s: requires at least 2 arguments, got %d", b.Name(), len(args))
	}

	result := new(starlark.Dict)
	for _, arg := range args {
		items, err := dictItems(b.Name(), arg)
		if err != nil {
			return nil, err
		}
		for _, item := range items {
			if err := result.SetKey(item[0], item[1]); err != nil {
				return nil, err
			}
		}
	}
	return result, nil
}

// dictDeepMergeImpl implements dict.deep_merge(d1, d2, ...) -> new dict with recursive merge.
// Requires at least 2 positional arguments, no kwargs.
// Lists are treated atomically (right-side replaces). None values overwrite.
func dictDeepMergeImpl(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if len(kwargs) > 0 {
		return nil, fmt.Errorf("%s: unexpected keyword arguments", b.Name())
	}
	if len(args) < 2 {
		return nil, fmt.Errorf("%s: requires at least 2 arguments, got %d", b.Name(), len(args))
	}

	// Fold left-to-right.
	accumulated := args[0]
	for i := 1; i < len(args); i++ {
		var err error
		accumulated, err = deepMergeTwo(b.Name(), accumulated, args[i], 0)
		if err != nil {
			return nil, err
		}
	}
	return accumulated, nil
}

// deepMergeTwo recursively merges two dicts, creating new dicts at each level.
// Never mutates either input.
func deepMergeTwo(fnName string, base, override starlark.Value, depth int) (starlark.Value, error) {
	if depth > maxMergeDepth {
		return nil, fmt.Errorf("%s: recursion depth exceeds maximum (%d)", fnName, maxMergeDepth)
	}

	baseItems, err := dictItems(fnName, base)
	if err != nil {
		return nil, err
	}
	overrideItems, err := dictItems(fnName, override)
	if err != nil {
		return nil, err
	}

	// Build a new result dict starting with all base items.
	result := new(starlark.Dict)
	for _, item := range baseItems {
		if err := result.SetKey(item[0], item[1]); err != nil {
			return nil, err
		}
	}

	// Apply override items.
	for _, item := range overrideItems {
		key, val := item[0], item[1]

		existing, found, err := result.Get(key)
		if err != nil {
			return nil, err
		}

		if found && isDict(existing) && isDict(val) {
			// Both are dicts: recurse.
			merged, err := deepMergeTwo(fnName, existing, val, depth+1)
			if err != nil {
				return nil, err
			}
			if err := result.SetKey(key, merged); err != nil {
				return nil, err
			}
		} else {
			// Right-wins: overwrite.
			if err := result.SetKey(key, val); err != nil {
				return nil, err
			}
		}
	}

	return result, nil
}

// dictPickImpl implements dict.pick(d, keys) -> new dict with only the specified keys.
func dictPickImpl(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var d starlark.Value
	var keys *starlark.List
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "d", &d, "keys", &keys); err != nil {
		return nil, err
	}

	mapping, ok := asMapping(d)
	if !ok {
		return nil, fmt.Errorf("%s: got %s, want dict", b.Name(), d.Type())
	}

	result := new(starlark.Dict)
	iter := keys.Iterate()
	defer iter.Done()
	var k starlark.Value
	for iter.Next(&k) {
		s, ok := k.(starlark.String)
		if !ok {
			return nil, fmt.Errorf("%s: key list element is %s, want string", b.Name(), k.Type())
		}
		v, found, err := mapping.Get(s)
		if err != nil {
			return nil, err
		}
		if found {
			if err := result.SetKey(s, v); err != nil {
				return nil, err
			}
		}
	}
	return result, nil
}

// dictOmitImpl implements dict.omit(d, keys) -> new dict without the specified keys.
func dictOmitImpl(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var d starlark.Value
	var keys *starlark.List
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "d", &d, "keys", &keys); err != nil {
		return nil, err
	}

	// Build exclude set.
	exclude := make(map[string]bool)
	iter := keys.Iterate()
	defer iter.Done()
	var k starlark.Value
	for iter.Next(&k) {
		s, ok := k.(starlark.String)
		if !ok {
			return nil, fmt.Errorf("%s: key list element is %s, want string", b.Name(), k.Type())
		}
		exclude[string(s)] = true
	}

	items, err := dictItems(b.Name(), d)
	if err != nil {
		return nil, err
	}

	result := new(starlark.Dict)
	for _, item := range items {
		keyStr, ok := item[0].(starlark.String)
		if !ok {
			continue
		}
		if !exclude[string(keyStr)] {
			if err := result.SetKey(item[0], item[1]); err != nil {
				return nil, err
			}
		}
	}
	return result, nil
}

// compactValue recursively compacts a Starlark value:
//   - Dicts: prunes None-valued entries and recurses into remaining values.
//   - Lists: recurses into elements (never removes elements).
//   - Everything else (tuples, scalars, None): returned unchanged.
//
// Returns (compacted value, whether anything changed, error).
//
// Aliasing: on the no-change path, nested containers in the returned value
// may share pointers with the input. Callers must not mutate the returned
// value and expect the input to remain untouched.
func compactValue(fnName string, v starlark.Value, depth int) (starlark.Value, bool, error) {
	if depth > maxMergeDepth {
		return nil, false, fmt.Errorf("%s: recursion depth exceeds maximum (%d)", fnName, maxMergeDepth)
	}

	switch {
	case isDict(v):
		return compactDict(fnName, v, depth)
	default:
		if l, ok := v.(*starlark.List); ok {
			return compactList(fnName, l, depth)
		}
		return v, false, nil
	}
}

// compactDict prunes None-valued entries from a dict (any supported dict type)
// and recurses into remaining values. Allocation is deferred until the first
// change is observed: if nothing changed and input is a *starlark.Dict, the
// original is returned and no copy is built. Otherwise a fresh *starlark.Dict
// is returned.
func compactDict(fnName string, v starlark.Value, depth int) (starlark.Value, bool, error) {
	items, err := dictItems(fnName, v)
	if err != nil {
		return nil, false, err
	}

	var result *starlark.Dict
	// materialize lazily allocates result and backfills entries [0, upTo).
	materialize := func(upTo int) error {
		if result != nil {
			return nil
		}
		result = new(starlark.Dict)
		for _, prev := range items[:upTo] {
			if err := result.SetKey(prev[0], prev[1]); err != nil {
				return err
			}
		}
		return nil
	}
	changed := false
	for i, item := range items {
		if item[1] == starlark.None {
			if err := materialize(i); err != nil {
				return nil, false, err
			}
			changed = true
			continue
		}
		val, childChanged, err := compactValue(fnName, item[1], depth+1)
		if err != nil {
			return nil, false, err
		}
		if childChanged {
			if err := materialize(i); err != nil {
				return nil, false, err
			}
			changed = true
		}
		if result != nil {
			if err := result.SetKey(item[0], val); err != nil {
				return nil, false, err
			}
		}
	}
	if !changed {
		if d, ok := v.(*starlark.Dict); ok {
			return d, false, nil
		}
		// Input is a non-*starlark.Dict wrapper (StarlarkDict or SchemaDict).
		// Callers type-assert to *starlark.Dict, so return a plain copy.
		if err := materialize(len(items)); err != nil {
			return nil, false, err
		}
	}
	return result, changed, nil
}

// compactList recurses into list elements to compact any dicts found inside.
// Never removes elements — None elements in lists pass through unchanged.
// Allocation is deferred until the first changed element: if nothing changed,
// the original list is returned and no copy is built.
func compactList(fnName string, l *starlark.List, depth int) (starlark.Value, bool, error) {
	n := l.Len()
	var elems []starlark.Value
	changed := false
	for i := range n {
		elem := l.Index(i)
		compacted, elemChanged, err := compactValue(fnName, elem, depth+1)
		if err != nil {
			return nil, false, err
		}
		if elemChanged && elems == nil {
			elems = make([]starlark.Value, i, n)
			for j := range i {
				elems[j] = l.Index(j)
			}
			changed = true
		}
		if elems != nil {
			elems = append(elems, compacted)
		}
	}
	if !changed {
		return l, false, nil
	}
	return starlark.NewList(elems), true, nil
}

// dictCompactImpl implements dict.compact(d) -> new dict with None-valued
// entries removed recursively at any nesting depth. Recurses into nested
// dicts and lists. Tuples pass through untouched (immutable). Empty strings,
// empty lists, and empty dicts are preserved because they can be semantically
// meaningful in K8s-style manifests (e.g. `spec: {}` is not the same as
// omitting spec).
//
// The top-level returned dict is always freshly allocated. Nested containers
// may alias the input when the compactor did not modify them; callers should
// not mutate nested values of the result and expect the input to stay intact.
//
// Raises an error if recursion depth exceeds maxMergeDepth (32).
//
// Intended for declaratively building manifests with optional fields:
//
//	body = dict.compact({
//	    "displayName": name,
//	    "administrativeUnitIds": [au_id] if au_id else None,
//	    "owners": owners if owners else None,
//	})
func dictCompactImpl(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var d starlark.Value
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "d", &d); err != nil {
		return nil, err
	}

	items, err := dictItems(b.Name(), d)
	if err != nil {
		return nil, err
	}

	result := new(starlark.Dict)
	for _, item := range items {
		if item[1] == starlark.None {
			continue
		}
		val, _, err := compactValue(b.Name(), item[1], 0)
		if err != nil {
			return nil, err
		}
		if err := result.SetKey(item[0], val); err != nil {
			return nil, err
		}
	}
	return result, nil
}

// validateDictPath validates a dot-path string, rejecting empty paths,
// leading/trailing dots, and consecutive dots.
func validateDictPath(fnName, path string) error {
	if path == "" {
		return fmt.Errorf("%s: path must not be empty", fnName)
	}
	if strings.HasPrefix(path, ".") || strings.HasSuffix(path, ".") || strings.Contains(path, "..") {
		return fmt.Errorf("%s: malformed path %q", fnName, path)
	}
	return nil
}

// dictDigImpl implements dict.dig(d, path, default=None) for safe dot-path lookup.
func dictDigImpl(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var d starlark.Value
	var path string
	var dflt starlark.Value = starlark.None
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "d", &d, "path", &path, "default?", &dflt); err != nil {
		return nil, err
	}

	if err := validateDictPath(b.Name(), path); err != nil {
		return nil, err
	}

	segments := strings.Split(path, ".")
	current := d
	for _, seg := range segments {
		mapping, ok := asMapping(current)
		if !ok {
			return dflt, nil
		}
		v, found, err := mapping.Get(starlark.String(seg))
		if err != nil || !found {
			return dflt, nil
		}
		current = v
	}
	return current, nil
}

// dictHasPathImpl implements dict.has_path(d, path) -> bool for path existence check.
func dictHasPathImpl(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var d starlark.Value
	var path string
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "d", &d, "path", &path); err != nil {
		return nil, err
	}

	if err := validateDictPath(b.Name(), path); err != nil {
		return nil, err
	}

	segments := strings.Split(path, ".")
	current := d
	for _, seg := range segments {
		mapping, ok := asMapping(current)
		if !ok {
			return starlark.False, nil
		}
		v, found, err := mapping.Get(starlark.String(seg))
		if err != nil || !found {
			return starlark.False, nil
		}
		current = v
	}
	return starlark.True, nil
}
