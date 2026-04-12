package builtins

import (
	"fmt"
	"sync"
	"time"

	"go.starlark.net/starlark"
)

// TTLCollector accumulates a user-set response TTL from Starlark scripts.
// It follows the collector pattern: create with NewTTLCollector, register the
// builtin via SetResponseTTLBuiltin, then read the result with TTL().
type TTLCollector struct {
	mu       sync.Mutex
	duration *time.Duration // nil = not set, non-nil = user-set value (including zero)
}

// NewTTLCollector creates an empty TTLCollector.
func NewTTLCollector() *TTLCollector {
	return &TTLCollector{}
}

// SetResponseTTLBuiltin returns a *starlark.Builtin for set_response_ttl.
func (tc *TTLCollector) SetResponseTTLBuiltin() *starlark.Builtin {
	return starlark.NewBuiltin("set_response_ttl", tc.setResponseTTLFn)
}

// setResponseTTLFn implements set_response_ttl(duration).
// duration can be a Go duration string ("30s", "1m30s") or an int (seconds).
func (tc *TTLCollector) setResponseTTLFn(
	_ *starlark.Thread,
	b *starlark.Builtin,
	args starlark.Tuple,
	kwargs []starlark.Tuple,
) (starlark.Value, error) {
	var duration starlark.Value

	if err := starlark.UnpackArgs(b.Name(), args, kwargs,
		"duration", &duration); err != nil {
		return nil, err
	}

	var d time.Duration

	switch v := duration.(type) {
	case starlark.String:
		parsed, err := time.ParseDuration(string(v))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", b.Name(), err)
		}
		d = parsed
	case starlark.Int:
		secs, ok := v.Int64()
		if !ok {
			return nil, fmt.Errorf("%s: integer too large", b.Name())
		}
		d = time.Duration(secs) * time.Second
	default:
		return nil, fmt.Errorf("%s: got %s, want string or int", b.Name(), duration.Type())
	}

	if d < 0 {
		return nil, fmt.Errorf("%s: duration must be non-negative", b.Name())
	}

	tc.mu.Lock()
	tc.duration = &d
	tc.mu.Unlock()

	return starlark.None, nil
}

// TTL returns the user-set TTL duration, or nil if set_response_ttl was never called.
func (tc *TTLCollector) TTL() *time.Duration {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	if tc.duration == nil {
		return nil
	}
	d := *tc.duration
	return &d
}
