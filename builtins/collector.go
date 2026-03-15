// Package builtins provides the predeclared Starlark globals and resource
// collection for Crossplane composition functions.
package builtins

import (
	"fmt"
	"sync"

	"github.com/crossplane/function-sdk-go/resource"
	"go.starlark.net/starlark"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/wompipomp/function-starlark/convert"
)

// ResourceRef is a custom Starlark type returned by Resource().
// It carries the resource name and can be passed to depends_on.
type ResourceRef struct {
	name string
}

// Compile-time interface checks.
var (
	_ starlark.Value    = (*ResourceRef)(nil)
	_ starlark.HasAttrs = (*ResourceRef)(nil)
)

func (r *ResourceRef) String() string      { return r.name }
func (r *ResourceRef) Type() string        { return "ResourceRef" }
func (r *ResourceRef) Freeze()             {} // immutable by construction
func (r *ResourceRef) Truth() starlark.Bool { return starlark.True }
func (r *ResourceRef) Hash() (uint32, error) {
	h := uint32(2166136261) // FNV-1a offset basis
	for i := 0; i < len(r.name); i++ {
		h ^= uint32(r.name[i])
		h *= 16777619
	}
	return h, nil
}

func (r *ResourceRef) Attr(name string) (starlark.Value, error) {
	if name == "name" {
		return starlark.String(r.name), nil
	}
	return nil, nil
}

func (r *ResourceRef) AttrNames() []string { return []string{"name"} }

// DependencyPair records a dependency between two resources.
type DependencyPair struct {
	Dependent  string // resource that depends on another
	Dependency string // resource being depended upon
	IsRef      bool   // true if dependency came from ResourceRef (validate), false if string (trust)
}

// CollectedResource holds a single resource produced by the Resource() builtin.
type CollectedResource struct {
	Name              string
	Body              *structpb.Struct
	Ready             resource.Ready
	ConnectionDetails map[string][]byte
}

// Collector accumulates Resource() calls from Starlark scripts.
// Duplicate names use last-wins semantics.
type Collector struct {
	mu           sync.Mutex
	resources    map[string]CollectedResource
	dependencies []DependencyPair
}

// NewCollector creates an empty Collector.
func NewCollector() *Collector {
	return &Collector{resources: make(map[string]CollectedResource)}
}

// Builtin returns a *starlark.Builtin named "Resource" that scripts call
// to produce desired composed resources.
func (c *Collector) Builtin() *starlark.Builtin {
	return starlark.NewBuiltin("Resource", c.resourceFn)
}

// Resources returns a copy of all collected resources.
func (c *Collector) Resources() map[string]CollectedResource {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[string]CollectedResource, len(c.resources))
	for k, v := range c.resources {
		out[k] = v
	}
	return out
}

// Dependencies returns a copy of all tracked dependency pairs.
func (c *Collector) Dependencies() []DependencyPair {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]DependencyPair, len(c.dependencies))
	copy(out, c.dependencies)
	return out
}

// addDependency records a dependency between two resources.
func (c *Collector) addDependency(dependent, dependency string, isRef bool) {
	c.dependencies = append(c.dependencies, DependencyPair{
		Dependent:  dependent,
		Dependency: dependency,
		IsRef:      isRef,
	})
}

// resourceFn implements the Resource(name, body, ready=None, connection_details=None, depends_on=None) Starlark builtin.
func (c *Collector) resourceFn(
	_ *starlark.Thread,
	b *starlark.Builtin,
	args starlark.Tuple,
	kwargs []starlark.Tuple,
) (starlark.Value, error) {
	var name string
	var body *starlark.Dict
	var connDetails *starlark.Dict
	var dependsOn *starlark.List
	var readyVal starlark.Value = starlark.None

	if err := starlark.UnpackArgs(b.Name(), args, kwargs,
		"name", &name, "body", &body, "ready?", &readyVal,
		"connection_details??", &connDetails,
		"depends_on??", &dependsOn); err != nil {
		return nil, err
	}

	s, err := convert.PlainDictToStruct(body)
	if err != nil {
		return nil, fmt.Errorf("Resource(%q): %w", name, err)
	}

	// Convert connection_details dict to map[string][]byte if provided.
	var cd map[string][]byte
	if connDetails != nil {
		cd = make(map[string][]byte, connDetails.Len())
		for _, item := range connDetails.Items() {
			k, ok := item[0].(starlark.String)
			if !ok {
				return nil, fmt.Errorf("Resource(%q): connection_details key must be string, got %s", name, item[0].Type())
			}
			v, ok := item[1].(starlark.String)
			if !ok {
				return nil, fmt.Errorf("Resource(%q): connection_details value must be string, got %s", name, item[1].Type())
			}
			cd[string(k)] = []byte(string(v))
		}
	}

	// Process depends_on list if provided.
	if dependsOn != nil {
		for i := 0; i < dependsOn.Len(); i++ {
			item := dependsOn.Index(i)
			switch v := item.(type) {
			case *ResourceRef:
				c.addDependency(name, v.name, true)
			case starlark.String:
				c.addDependency(name, string(v), false)
			default:
				return nil, fmt.Errorf("Resource(%q): depends_on[%d] must be ResourceRef or string, got %s",
					name, i, item.Type())
			}
		}
	}

	c.mu.Lock()
	c.resources[name] = CollectedResource{
		Name:              name,
		Body:              s,
		Ready:             readyFromStarlark(readyVal),
		ConnectionDetails: cd,
	}
	c.mu.Unlock()

	return &ResourceRef{name: name}, nil
}

// readyFromStarlark converts a Starlark value to the resource.Ready type.
// None -> ReadyUnspecified (let function-auto-ready detect readiness)
// True -> ReadyTrue (explicitly ready, e.g. ProviderConfig)
// False -> ReadyFalse (explicitly not ready)
func readyFromStarlark(v starlark.Value) resource.Ready {
	switch v {
	case starlark.None:
		return resource.ReadyUnspecified
	case starlark.True:
		return resource.ReadyTrue
	default:
		return resource.ReadyFalse
	}
}
