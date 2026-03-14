package builtins

import (
	"fmt"
	"sync"

	fnv1 "github.com/crossplane/function-sdk-go/proto/v1"
	"go.starlark.net/starlark"
)

// ConnectionCollector accumulates XR-level connection details from
// set_connection_details() calls. Per-resource connection details are
// handled by the Collector via the Resource() kwarg.
type ConnectionCollector struct {
	mu      sync.Mutex
	details map[string][]byte
}

// NewConnectionCollector creates an empty ConnectionCollector.
func NewConnectionCollector() *ConnectionCollector {
	return &ConnectionCollector{details: make(map[string][]byte)}
}

// SetConnectionDetailsBuiltin returns a *starlark.Builtin named
// "set_connection_details" that scripts call to set XR-level connection
// details. Multiple calls merge additively (second call adds keys,
// does not replace all).
func (c *ConnectionCollector) SetConnectionDetailsBuiltin() *starlark.Builtin {
	return starlark.NewBuiltin("set_connection_details", c.setConnectionDetailsFn)
}

// ConnectionDetails returns a copy of the accumulated connection details.
func (c *ConnectionCollector) ConnectionDetails() map[string][]byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[string][]byte, len(c.details))
	for k, v := range c.details {
		out[k] = v
	}
	return out
}

func (c *ConnectionCollector) setConnectionDetailsFn(
	_ *starlark.Thread,
	b *starlark.Builtin,
	args starlark.Tuple,
	kwargs []starlark.Tuple,
) (starlark.Value, error) {
	var d *starlark.Dict

	if err := starlark.UnpackPositionalArgs(b.Name(), args, kwargs, 1, &d); err != nil {
		return nil, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	for _, item := range d.Items() {
		k, ok := item[0].(starlark.String)
		if !ok {
			return nil, fmt.Errorf("set_connection_details: key must be string, got %s", item[0].Type())
		}
		v, ok := item[1].(starlark.String)
		if !ok {
			return nil, fmt.Errorf("set_connection_details: value must be string, got %s", item[1].Type())
		}
		c.details[string(k)] = []byte(string(v))
	}

	return starlark.None, nil
}

// ApplyConnectionDetails sets XR-level connection details on the response
// desired composite. It creates Desired/Composite/ConnectionDetails if nil
// (first-in-pipeline) and merges additively with existing details.
func ApplyConnectionDetails(rsp *fnv1.RunFunctionResponse, cd map[string][]byte) {
	if len(cd) == 0 {
		return
	}

	if rsp.Desired == nil {
		rsp.Desired = &fnv1.State{}
	}
	if rsp.Desired.Composite == nil {
		rsp.Desired.Composite = &fnv1.Resource{}
	}
	if rsp.Desired.Composite.ConnectionDetails == nil {
		rsp.Desired.Composite.ConnectionDetails = make(map[string][]byte)
	}

	for k, v := range cd {
		rsp.Desired.Composite.ConnectionDetails[k] = v
	}
}
