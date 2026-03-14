package builtins

import (
	"fmt"
	"sync"

	fnv1 "github.com/crossplane/function-sdk-go/proto/v1"
	"go.starlark.net/starlark"

	"github.com/wompipomp/function-starlark/convert"
)

// CollectedRequirement holds a single resource requirement accumulated by
// require_resource or require_resources.
type CollectedRequirement struct {
	Name, APIVersion, Kind, MatchName string
	MatchLabels                       map[string]string
}

// RequirementsCollector accumulates resource requirements from Starlark scripts.
type RequirementsCollector struct {
	mu           sync.Mutex
	requirements []CollectedRequirement
}

// NewRequirementsCollector creates an empty RequirementsCollector.
func NewRequirementsCollector() *RequirementsCollector {
	return &RequirementsCollector{}
}

// RequireResourceBuiltin returns a *starlark.Builtin for require_resource.
func (rc *RequirementsCollector) RequireResourceBuiltin() *starlark.Builtin {
	return starlark.NewBuiltin("require_resource", rc.requireResourceFn)
}

// RequireResourcesBuiltin returns a *starlark.Builtin for require_resources.
func (rc *RequirementsCollector) RequireResourcesBuiltin() *starlark.Builtin {
	return starlark.NewBuiltin("require_resources", rc.requireResourcesFn)
}

// Requirements returns a copy of all collected requirements.
func (rc *RequirementsCollector) Requirements() []CollectedRequirement {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	out := make([]CollectedRequirement, len(rc.requirements))
	copy(out, rc.requirements)
	return out
}

// requireResourceFn implements require_resource(name, apiVersion, kind, match_name=None, match_labels=None).
func (rc *RequirementsCollector) requireResourceFn(
	_ *starlark.Thread,
	b *starlark.Builtin,
	args starlark.Tuple,
	kwargs []starlark.Tuple,
) (starlark.Value, error) {
	var name, apiVersion, kind string
	var matchName string
	var matchLabelsDict *starlark.Dict

	if err := starlark.UnpackArgs(b.Name(), args, kwargs,
		"name", &name, "apiVersion", &apiVersion, "kind", &kind,
		"match_name?", &matchName, "match_labels?", &matchLabelsDict); err != nil {
		return nil, err
	}

	// At least one of match_name or match_labels is required.
	if matchName == "" && matchLabelsDict == nil {
		return nil, fmt.Errorf("require_resource: must specify at least one of match_name or match_labels")
	}

	// If both provided, match_name takes precedence.
	var matchLabels map[string]string
	if matchName != "" {
		matchLabels = nil
	} else if matchLabelsDict != nil {
		var err error
		matchLabels, err = dictToStringMap(matchLabelsDict)
		if err != nil {
			return nil, fmt.Errorf("require_resource: match_labels: %w", err)
		}
	}

	rc.mu.Lock()
	rc.requirements = append(rc.requirements, CollectedRequirement{
		Name:        name,
		APIVersion:  apiVersion,
		Kind:        kind,
		MatchName:   matchName,
		MatchLabels: matchLabels,
	})
	rc.mu.Unlock()

	return starlark.None, nil
}

// requireResourcesFn implements require_resources(name, apiVersion, kind, match_labels).
func (rc *RequirementsCollector) requireResourcesFn(
	_ *starlark.Thread,
	b *starlark.Builtin,
	args starlark.Tuple,
	kwargs []starlark.Tuple,
) (starlark.Value, error) {
	var name, apiVersion, kind string
	var matchLabelsDict *starlark.Dict

	if err := starlark.UnpackArgs(b.Name(), args, kwargs,
		"name", &name, "apiVersion", &apiVersion, "kind", &kind,
		"match_labels", &matchLabelsDict); err != nil {
		return nil, err
	}

	matchLabels, err := dictToStringMap(matchLabelsDict)
	if err != nil {
		return nil, fmt.Errorf("require_resources: match_labels: %w", err)
	}

	rc.mu.Lock()
	rc.requirements = append(rc.requirements, CollectedRequirement{
		Name:        name,
		APIVersion:  apiVersion,
		Kind:        kind,
		MatchLabels: matchLabels,
	})
	rc.mu.Unlock()

	return starlark.None, nil
}

// dictToStringMap converts a *starlark.Dict with string keys/values to a Go map.
func dictToStringMap(d *starlark.Dict) (map[string]string, error) {
	m := make(map[string]string, d.Len())
	for _, item := range d.Items() {
		k, ok := item[0].(starlark.String)
		if !ok {
			return nil, fmt.Errorf("key %v (%T) is not a string", item[0], item[0])
		}
		v, ok := item[1].(starlark.String)
		if !ok {
			return nil, fmt.Errorf("value for key %q (%T) is not a string", string(k), item[1])
		}
		m[string(k)] = string(v)
	}
	return m, nil
}

// ApplyRequirements sets resource requirements on the response.
// It creates Requirements and Resources map if nil.
func ApplyRequirements(rsp *fnv1.RunFunctionResponse, reqs []CollectedRequirement) {
	if len(reqs) == 0 {
		return
	}

	if rsp.Requirements == nil {
		rsp.Requirements = &fnv1.Requirements{}
	}
	if rsp.Requirements.Resources == nil {
		rsp.Requirements.Resources = make(map[string]*fnv1.ResourceSelector)
	}

	for _, req := range reqs {
		sel := &fnv1.ResourceSelector{
			ApiVersion: req.APIVersion,
			Kind:       req.Kind,
		}
		if req.MatchName != "" {
			sel.Match = &fnv1.ResourceSelector_MatchName{MatchName: req.MatchName}
		} else if len(req.MatchLabels) > 0 {
			sel.Match = &fnv1.ResourceSelector_MatchLabels{
				MatchLabels: &fnv1.MatchLabels{Labels: req.MatchLabels},
			}
		}
		rsp.Requirements.Resources[req.Name] = sel
	}
}

// buildExtraResourcesDict builds a frozen starlark.Dict from request extra
// resources. It prefers RequiredResources (current) and falls back to
// ExtraResources (deprecated). Each resource is converted to a frozen
// StarlarkDict via convert.StructToStarlark.
func buildExtraResourcesDict(req *fnv1.RunFunctionRequest) (*starlark.Dict, error) {
	d := new(starlark.Dict)

	// Prefer RequiredResources (current), fall back to ExtraResources (deprecated).
	rrs := req.GetRequiredResources()
	if rrs == nil {
		rrs = req.GetExtraResources()
	}
	if rrs == nil {
		d.Freeze()
		return d, nil
	}

	for name, resources := range rrs {
		items := resources.GetItems()
		if len(items) == 0 {
			// No resources matched -- set to None.
			if err := d.SetKey(starlark.String(name), starlark.None); err != nil {
				return nil, fmt.Errorf("extra resource %q: %w", name, err)
			}
			continue
		}

		// Build list of frozen resource dicts.
		elems := make([]starlark.Value, 0, len(items))
		for _, item := range items {
			rd, err := convert.StructToStarlark(item.GetResource(), true) // frozen
			if err != nil {
				return nil, fmt.Errorf("extra resource %q: %w", name, err)
			}
			elems = append(elems, rd)
		}
		list := starlark.NewList(elems)
		list.Freeze()
		if err := d.SetKey(starlark.String(name), list); err != nil {
			return nil, fmt.Errorf("extra resource %q: %w", name, err)
		}
	}

	d.Freeze()
	return d, nil
}
