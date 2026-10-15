package builtins

import (
	"fmt"
	"sort"
	"strings"

	"go.starlark.net/starlark"

	"github.com/wompipomp/function-starlark/convert"
)

// ExtraRef is the handle returned by require_extra_resource and
// require_extra_resources. It carries the requirement name and can be passed
// to get_extra_resource / get_extra_resources in place of the name string.
type ExtraRef struct {
	name string
}

var (
	_ starlark.Value    = (*ExtraRef)(nil)
	_ starlark.HasAttrs = (*ExtraRef)(nil)
)

func (r *ExtraRef) String() string       { return r.name }
func (r *ExtraRef) Type() string         { return "ExtraRef" }
func (r *ExtraRef) Freeze()              {}
func (r *ExtraRef) Truth() starlark.Bool { return starlark.True }
func (r *ExtraRef) Hash() (uint32, error) {
	h := uint32(2166136261)
	for i := 0; i < len(r.name); i++ {
		h ^= uint32(r.name[i])
		h *= 16777619
	}
	return h, nil
}

func (r *ExtraRef) Attr(name string) (starlark.Value, error) {
	if name == "name" {
		return starlark.String(r.name), nil
	}
	return nil, nil
}

func (r *ExtraRef) AttrNames() []string { return []string{"name"} }

// refName resolves the first argument of name-taking builtins. It accepts a
// plain string or any handle returned by Resource(), require_extra_resource()
// or require_extra_resources(), so scripts can pass the handle directly
// instead of spelling out ref.name.
func refName(fnName string, v starlark.Value) (string, error) {
	var name string
	switch r := v.(type) {
	case starlark.String:
		name = string(r)
	case *ResourceRef:
		name = r.name
	case *SkippedRef:
		name = r.name
	case *ExtraRef:
		name = r.name
	default:
		return "", fmt.Errorf("%s: name must be a string or resource handle, got %s", fnName, v.Type())
	}
	if name == "" {
		return "", fmt.Errorf("%s: name must not be empty", fnName)
	}
	return name, nil
}

// pathKeys validates and converts an optional path argument. nil and None
// mean "no path" (has=false); "" and [] are rejected; other types are
// delegated to pathToKeys, which rejects non-string/list values.
func pathKeys(fnName string, path starlark.Value) (keys []string, has bool, err error) {
	if path == nil || path == starlark.None {
		return nil, false, nil
	}
	switch p := path.(type) {
	case starlark.String:
		if string(p) == "" {
			return nil, false, fmt.Errorf("%s: path must not be empty", fnName)
		}
	case *starlark.List:
		if p.Len() == 0 {
			return nil, false, fmt.Errorf("%s: path must not be empty", fnName)
		}
	}
	keys, err = pathToKeys(path)
	if err != nil {
		return nil, false, err
	}
	return keys, true, nil
}

// walkPath descends from root along keys. A conversion error from lazy
// materialization propagates; a genuine miss or a None leaf yields dflt.
func walkPath(root starlark.Value, keys []string, dflt starlark.Value) (starlark.Value, error) {
	current := root
	for _, key := range keys {
		mapping, ok := current.(starlark.Mapping)
		if !ok {
			return dflt, nil
		}
		v, found, err := mapping.Get(starlark.String(key))
		if err != nil {
			return nil, err
		}
		if !found || v == starlark.None {
			return dflt, nil
		}
		current = v
	}
	return current, nil
}

// lookupKeys returns root when no path was given, otherwise walks keys.
func lookupKeys(root starlark.Value, keys []string, has bool, dflt starlark.Value) (starlark.Value, error) {
	if !has {
		return root, nil
	}
	return walkPath(root, keys, dflt)
}

// derivedRequirementName builds a stable requirement name from the selector
// when the script omits one: "<Kind>/<namespace>/<match_name>" or
// "<Kind>/<namespace>/k=v,k=v" for label selectors.
func derivedRequirementName(kind, namespace, matchName string, matchLabels map[string]string) string {
	var sel string
	if matchName != "" {
		sel = matchName
	} else {
		pairs := make([]string, 0, len(matchLabels))
		for k, v := range matchLabels {
			pairs = append(pairs, k+"="+v)
		}
		sort.Strings(pairs)
		sel = strings.Join(pairs, ",")
	}
	return kind + "/" + namespace + "/" + sel
}

// getManifestImpl implements get_manifest(ref, path=None, default=None).
// It reads the body of a resource previously registered via Resource(), as
// it will be emitted (after None compaction and label/annotation merging).
// The structpb body is converted lazily on first access and cached per name.
func getManifestImpl(
	fnName string,
	c *Collector,
	args starlark.Tuple,
	kwargs []starlark.Tuple,
) (starlark.Value, error) {
	var nameVal, path starlark.Value
	var dflt starlark.Value = starlark.None

	if err := starlark.UnpackArgs(fnName, args, kwargs,
		"name", &nameVal, "path?", &path, "default?", &dflt); err != nil {
		return nil, err
	}
	name, err := refName(fnName, nameVal)
	if err != nil {
		return nil, err
	}
	keys, has, err := pathKeys(fnName, path)
	if err != nil {
		return nil, err
	}

	body := c.manifest(name)
	if body == nil {
		return dflt, nil
	}
	return lookupKeys(body, keys, has, dflt)
}

// manifest returns the frozen Starlark view of a collected resource body, or
// nil when no resource of that name has been emitted.
func (c *Collector) manifest(name string) *convert.StarlarkDict {
	c.mu.Lock()
	defer c.mu.Unlock()
	if d, ok := c.manifests[name]; ok {
		return d
	}
	cr, ok := c.resources[name]
	if !ok || cr.Body == nil {
		return nil
	}
	d := convert.NewLazyStarlarkDict(cr.Body)
	d.Freeze()
	if c.manifests == nil {
		c.manifests = make(map[string]*convert.StarlarkDict)
	}
	c.manifests[name] = d
	return d
}

// GetManifestBuiltin returns the get_manifest builtin bound to this collector.
func (c *Collector) GetManifestBuiltin() *starlark.Builtin {
	return starlark.NewBuiltin("get_manifest", func(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		return getManifestImpl(b.Name(), c, args, kwargs)
	})
}
