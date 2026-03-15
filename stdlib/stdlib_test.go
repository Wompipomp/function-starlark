package stdlib

import (
	"os"
	"testing"

	"github.com/crossplane/crossplane-runtime/v2/pkg/logging"
	"go.starlark.net/starlark"

	"github.com/wompipomp/function-starlark/runtime"
)

// testLogger implements logging.Logger for tests.
type testLogger struct{}

func (l *testLogger) Info(_ string, _ ...any)              {}
func (l *testLogger) Debug(_ string, _ ...any)             {}
func (l *testLogger) WithValues(_ ...any) logging.Logger { return l }

// loadModule loads a .star file from disk and returns its exports.
func loadModule(t *testing.T, filename string, predeclared starlark.StringDict) starlark.StringDict {
	t.Helper()
	src, err := os.ReadFile(filename)
	if err != nil {
		t.Fatalf("reading %s: %v", filename, err)
	}
	inline := map[string]string{filename: string(src)}
	rt := runtime.NewRuntime(&testLogger{})
	loader := runtime.NewModuleLoader(inline, nil, predeclared, rt)
	thread := &starlark.Thread{Name: "test", Load: loader.LoadFunc()}
	loaded, err := thread.Load(thread, filename)
	if err != nil {
		t.Fatalf("loading %s: %v", filename, err)
	}
	return loaded
}

// TestStdlibModulesExist verifies all four modules load and export expected functions.
func TestStdlibModulesExist(t *testing.T) {
	// networking.star exports
	net := loadModule(t, "networking.star", starlark.StringDict{})
	for _, name := range []string{"ip_to_int", "int_to_ip", "network_address", "broadcast_address", "subnet_cidr", "cidr_contains"} {
		if _, ok := net[name]; !ok {
			t.Errorf("networking.star missing export: %s", name)
		}
	}

	// naming.star needs oxr and get
	mockOxr := starlark.NewDict(1)
	_ = mockOxr.SetKey(starlark.String("metadata"), func() starlark.Value {
		d := starlark.NewDict(1)
		_ = d.SetKey(starlark.String("name"), starlark.String("test-xr"))
		return d
	}())
	mockOxr.Freeze()
	getFn := starlark.NewBuiltin("get", mockGetImpl)
	namingPre := starlark.StringDict{"oxr": mockOxr, "get": getFn}
	naming := loadModule(t, "naming.star", namingPre)
	for _, name := range []string{"resource_name"} {
		if _, ok := naming[name]; !ok {
			t.Errorf("naming.star missing export: %s", name)
		}
	}

	// labels.star needs oxr and get
	labels := loadModule(t, "labels.star", namingPre)
	for _, name := range []string{"standard_labels", "crossplane_labels", "merge_labels"} {
		if _, ok := labels[name]; !ok {
			t.Errorf("labels.star missing export: %s", name)
		}
	}

	// conditions.star needs set_condition and emit_event
	condPre := starlark.StringDict{
		"set_condition": starlark.NewBuiltin("set_condition", mockSetCondition),
		"emit_event":    starlark.NewBuiltin("emit_event", mockEmitEvent),
	}
	conds := loadModule(t, "conditions.star", condPre)
	for _, name := range []string{"ready", "not_ready", "degraded", "progress"} {
		if _, ok := conds[name]; !ok {
			t.Errorf("conditions.star missing export: %s", name)
		}
	}
}

// mockGetImpl implements get(obj, path, default=None) for tests.
func mockGetImpl(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var obj starlark.Value
	var path starlark.Value
	var dflt starlark.Value = starlark.None

	if err := starlark.UnpackArgs(b.Name(), args, kwargs,
		"obj", &obj, "path", &path, "default?", &dflt); err != nil {
		return nil, err
	}

	// Handle dot-separated string path.
	if s, ok := path.(starlark.String); ok {
		parts := splitDot(string(s))
		current := obj
		for _, part := range parts {
			m, ok := current.(starlark.Mapping)
			if !ok {
				return dflt, nil
			}
			v, found, err := m.Get(starlark.String(part))
			if err != nil || !found || v == starlark.None {
				return dflt, nil
			}
			current = v
		}
		return current, nil
	}

	// Handle list path.
	if l, ok := path.(*starlark.List); ok {
		current := obj
		for i := 0; i < l.Len(); i++ {
			key, ok := l.Index(i).(starlark.String)
			if !ok {
				return dflt, nil
			}
			m, ok := current.(starlark.Mapping)
			if !ok {
				return dflt, nil
			}
			v, found, err := m.Get(key)
			if err != nil || !found || v == starlark.None {
				return dflt, nil
			}
			current = v
		}
		return current, nil
	}

	return dflt, nil
}

func splitDot(s string) []string {
	var parts []string
	current := ""
	for _, c := range s {
		if c == '.' {
			parts = append(parts, current)
			current = ""
		} else {
			current += string(c)
		}
	}
	parts = append(parts, current)
	return parts
}

// mockSetCondition records calls for verification.
func mockSetCondition(_ *starlark.Thread, _ *starlark.Builtin, _ starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
	return starlark.None, nil
}

// mockEmitEvent records calls for verification.
func mockEmitEvent(_ *starlark.Thread, _ *starlark.Builtin, _ starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
	return starlark.None, nil
}
