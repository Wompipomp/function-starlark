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
	mu        sync.Mutex
	resources map[string]CollectedResource
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

// resourceFn implements the Resource(name, body, ready=True, connection_details=None) Starlark builtin.
func (c *Collector) resourceFn(
	_ *starlark.Thread,
	b *starlark.Builtin,
	args starlark.Tuple,
	kwargs []starlark.Tuple,
) (starlark.Value, error) {
	var name string
	var body *starlark.Dict
	var connDetails *starlark.Dict
	ready := true

	if err := starlark.UnpackArgs(b.Name(), args, kwargs,
		"name", &name, "body", &body, "ready?", &ready,
		"connection_details??", &connDetails); err != nil {
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

	c.mu.Lock()
	c.resources[name] = CollectedResource{
		Name:              name,
		Body:              s,
		Ready:             readyFromBool(ready),
		ConnectionDetails: cd,
	}
	c.mu.Unlock()

	return starlark.None, nil
}

// readyFromBool converts a boolean to the resource.Ready type.
func readyFromBool(ready bool) resource.Ready {
	if ready {
		return resource.ReadyTrue
	}
	return resource.ReadyFalse
}
