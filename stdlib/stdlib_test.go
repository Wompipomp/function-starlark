package stdlib

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/crossplane/crossplane-runtime/v2/pkg/logging"
	"go.starlark.net/starlark"

	"github.com/wompipomp/function-starlark/runtime"
)

// ---------------------------------------------------------------------------
// Test infrastructure
// ---------------------------------------------------------------------------

// testLogger implements logging.Logger for tests.
type testLogger struct{}

func (l *testLogger) Info(_ string, _ ...any)            {}
func (l *testLogger) Debug(_ string, _ ...any)           {}
func (l *testLogger) WithValues(_ ...any) logging.Logger { return l }

// callRecord stores the arguments from a mock builtin call.
type callRecord struct {
	name   string
	kwargs map[string]string
}

// callRecorder is a thread-safe recorder for mock builtin calls.
type callRecorder struct {
	mu    sync.Mutex
	calls []callRecord
}

func (r *callRecorder) record(name string, kwargs []starlark.Tuple) {
	r.mu.Lock()
	defer r.mu.Unlock()
	kw := make(map[string]string)
	for _, pair := range kwargs {
		kw[string(pair[0].(starlark.String))] = pair[1].String()
	}
	r.calls = append(r.calls, callRecord{name: name, kwargs: kw})
}

func (r *callRecorder) getCalls() []callRecord {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]callRecord, len(r.calls))
	copy(out, r.calls)
	return out
}

func (r *callRecorder) reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = nil
}

// loadModule loads a .star file from disk and returns its exports.
func loadModule(t *testing.T, filename string, predeclared starlark.StringDict) starlark.StringDict {
	t.Helper()
	src, err := os.ReadFile(filename) //nolint:gosec // test helper reads test fixtures by name
	if err != nil {
		t.Fatalf("reading %s: %v", filename, err)
	}
	inline := map[string]string{filename: string(src)}
	rt := runtime.NewRuntime(&testLogger{})
	loader := runtime.NewModuleLoader(inline, nil, predeclared, rt, "")
	thread := &starlark.Thread{Name: "test", Load: loader.LoadFunc()}
	loaded, err := thread.Load(thread, filename)
	if err != nil {
		t.Fatalf("loading %s: %v", filename, err)
	}
	return loaded
}

// callStarlark calls a Starlark function with the given args and kwargs.
func callStarlark(t *testing.T, fn starlark.Value, args starlark.Tuple, kwargs []starlark.Tuple) starlark.Value {
	t.Helper()
	thread := &starlark.Thread{Name: "test-call"}
	result, err := starlark.Call(thread, fn, args, kwargs)
	if err != nil {
		t.Fatalf("calling %s: %v", fn.(*starlark.Function).Name(), err)
	}
	return result
}

// callStarlarkErr calls a Starlark function expecting it to fail.
func callStarlarkErr(t *testing.T, fn starlark.Value, args starlark.Tuple, kwargs []starlark.Tuple) error {
	t.Helper()
	thread := &starlark.Thread{Name: "test-call"}
	_, err := starlark.Call(thread, fn, args, kwargs)
	if err == nil {
		t.Fatalf("expected error calling %s, got nil", fn.(*starlark.Function).Name())
	}
	return err
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
		parts := strings.Split(string(s), ".")
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

// buildMockOxr creates a mock oxr dict for tests.
func buildMockOxr(name, claimName, claimNamespace string) *starlark.Dict {
	labels := starlark.NewDict(2)
	if claimName != "" {
		_ = labels.SetKey(starlark.String("crossplane.io/claim-name"), starlark.String(claimName))
	}
	if claimNamespace != "" {
		_ = labels.SetKey(starlark.String("crossplane.io/claim-namespace"), starlark.String(claimNamespace))
	}

	metadata := starlark.NewDict(2)
	_ = metadata.SetKey(starlark.String("name"), starlark.String(name))
	_ = metadata.SetKey(starlark.String("labels"), labels)

	oxr := starlark.NewDict(1)
	_ = oxr.SetKey(starlark.String("metadata"), metadata)
	oxr.Freeze()
	return oxr
}

// ---------------------------------------------------------------------------
// TestStdlibNetworking
// ---------------------------------------------------------------------------

func TestStdlibNetworking(t *testing.T) {
	exports := loadModule(t, "networking.star", starlark.StringDict{})

	ipToInt := exports["ip_to_int"]
	intToIP := exports["int_to_ip"]
	networkAddr := exports["network_address"]
	broadcastAddr := exports["broadcast_address"]
	subnetCIDR := exports["subnet_cidr"]
	cidrContains := exports["cidr_contains"]

	t.Run("ip_to_int", func(t *testing.T) {
		result := callStarlark(t, ipToInt, starlark.Tuple{starlark.String("192.168.1.0")}, nil)
		// 192*256^3 + 168*256^2 + 1*256 + 0 = 3232235776
		want := starlark.MakeUint(3232235776)
		if result.String() != want.String() {
			t.Errorf("ip_to_int('192.168.1.0') = %v, want %v", result, want)
		}
	})

	t.Run("int_to_ip", func(t *testing.T) {
		result := callStarlark(t, intToIP, starlark.Tuple{starlark.MakeUint(3232235776)}, nil)
		got := string(result.(starlark.String))
		if got != "192.168.1.0" {
			t.Errorf("int_to_ip(3232235776) = %q, want %q", got, "192.168.1.0")
		}
	})

	t.Run("roundtrip", func(t *testing.T) {
		ips := []string{"0.0.0.0", "255.255.255.255", "10.0.1.5", "172.16.0.1"}
		for _, ip := range ips {
			intResult := callStarlark(t, ipToInt, starlark.Tuple{starlark.String(ip)}, nil)
			strResult := callStarlark(t, intToIP, starlark.Tuple{intResult}, nil)
			got := string(strResult.(starlark.String))
			if got != ip {
				t.Errorf("roundtrip(%q) = %q", ip, got)
			}
		}
	})

	t.Run("network_address", func(t *testing.T) {
		result := callStarlark(t, networkAddr, starlark.Tuple{starlark.String("10.0.1.5/24")}, nil)
		got := string(result.(starlark.String))
		if got != "10.0.1.0" {
			t.Errorf("network_address('10.0.1.5/24') = %q, want %q", got, "10.0.1.0")
		}
	})

	t.Run("broadcast_address", func(t *testing.T) {
		result := callStarlark(t, broadcastAddr, starlark.Tuple{starlark.String("10.0.1.0/24")}, nil)
		got := string(result.(starlark.String))
		if got != "10.0.1.255" {
			t.Errorf("broadcast_address('10.0.1.0/24') = %q, want %q", got, "10.0.1.255")
		}
	})

	t.Run("subnet_cidr", func(t *testing.T) {
		tests := []struct {
			base string
			bits int
			num  int
			want string
		}{
			{"10.0.0.0/16", 8, 0, "10.0.0.0/24"},
			{"10.0.0.0/16", 8, 1, "10.0.1.0/24"},
			{"10.0.0.0/16", 8, 255, "10.0.255.0/24"},
			{"192.168.0.0/24", 4, 0, "192.168.0.0/28"},
			{"192.168.0.0/24", 4, 1, "192.168.0.16/28"},
		}
		for _, tc := range tests {
			t.Run(fmt.Sprintf("%s_%d_%d", tc.base, tc.bits, tc.num), func(t *testing.T) {
				result := callStarlark(t, subnetCIDR,
					starlark.Tuple{
						starlark.String(tc.base),
						starlark.MakeInt(tc.bits),
						starlark.MakeInt(tc.num),
					}, nil)
				got := string(result.(starlark.String))
				if got != tc.want {
					t.Errorf("subnet_cidr(%q, %d, %d) = %q, want %q", tc.base, tc.bits, tc.num, got, tc.want)
				}
			})
		}
	})

	t.Run("cidr_contains_true", func(t *testing.T) {
		result := callStarlark(t, cidrContains,
			starlark.Tuple{starlark.String("10.0.0.0/16"), starlark.String("10.0.1.5")}, nil)
		if result != starlark.True {
			t.Errorf("cidr_contains('10.0.0.0/16', '10.0.1.5') = %v, want True", result)
		}
	})

	t.Run("cidr_contains_false", func(t *testing.T) {
		result := callStarlark(t, cidrContains,
			starlark.Tuple{starlark.String("10.0.0.0/16"), starlark.String("192.168.1.1")}, nil)
		if result != starlark.False {
			t.Errorf("cidr_contains('10.0.0.0/16', '192.168.1.1') = %v, want False", result)
		}
	})

	t.Run("invalid_ip_fails", func(t *testing.T) {
		err := callStarlarkErr(t, ipToInt, starlark.Tuple{starlark.String("1.2.3")}, nil)
		if !strings.Contains(err.Error(), "invalid IP") {
			t.Errorf("error = %q, want it to contain 'invalid IP'", err.Error())
		}
	})

	t.Run("invalid_prefix_fails", func(t *testing.T) {
		err := callStarlarkErr(t, subnetCIDR,
			starlark.Tuple{starlark.String("10.0.0.0/30"), starlark.MakeInt(5), starlark.MakeInt(0)}, nil)
		if !strings.Contains(err.Error(), "exceeds 32") {
			t.Errorf("error = %q, want it to contain 'exceeds 32'", err.Error())
		}
	})

	t.Run("subnet_num_out_of_range", func(t *testing.T) {
		err := callStarlarkErr(t, subnetCIDR,
			starlark.Tuple{starlark.String("10.0.0.0/16"), starlark.MakeInt(8), starlark.MakeInt(256)}, nil)
		if !strings.Contains(err.Error(), "out of range") {
			t.Errorf("error = %q, want it to contain 'out of range'", err.Error())
		}
	})
}

// ---------------------------------------------------------------------------
// TestStdlibNaming
// ---------------------------------------------------------------------------

func TestStdlibNaming(t *testing.T) {
	mockOxr := buildMockOxr("my-xr", "", "")
	getFn := starlark.NewBuiltin("get", mockGetImpl)
	pre := starlark.StringDict{"oxr": mockOxr, "get": getFn}
	exports := loadModule(t, "naming.star", pre)

	resourceName := exports["resource_name"]

	t.Run("short_name_no_truncation", func(t *testing.T) {
		result := callStarlark(t, resourceName,
			starlark.Tuple{starlark.String("bucket")},
			[]starlark.Tuple{{starlark.String("xr_name"), starlark.String("my-xr")}})
		got := string(result.(starlark.String))
		if got != "my-xr-bucket" {
			t.Errorf("resource_name('bucket', xr_name='my-xr') = %q, want %q", got, "my-xr-bucket")
		}
	})

	t.Run("long_name_truncated_to_63", func(t *testing.T) {
		longXR := strings.Repeat("a", 50)
		result := callStarlark(t, resourceName,
			starlark.Tuple{starlark.String("very-long-suffix")},
			[]starlark.Tuple{{starlark.String("xr_name"), starlark.String(longXR)}})
		got := string(result.(starlark.String))
		if len(got) > 63 {
			t.Errorf("resource_name result length = %d, want <= 63", len(got))
		}
		if len(got) == 0 {
			t.Fatal("resource_name returned empty string")
		}
	})

	t.Run("suffix_preserved_after_truncation", func(t *testing.T) {
		longXR := strings.Repeat("a", 60)
		result := callStarlark(t, resourceName,
			starlark.Tuple{starlark.String("bucket")},
			[]starlark.Tuple{{starlark.String("xr_name"), starlark.String(longXR)}})
		got := string(result.(starlark.String))
		if !strings.Contains(got, "-bucket-") {
			t.Errorf("suffix not preserved in truncated name: %q", got)
		}
		if len(got) > 63 {
			t.Errorf("result length = %d, want <= 63", len(got))
		}
	})

	t.Run("truncated_no_trailing_hyphen", func(t *testing.T) {
		// XR name has hyphens near the truncation boundary so rstrip fires.
		xrName := strings.Repeat("a", 38) + "--" + strings.Repeat("b", 20)
		result := callStarlark(t, resourceName,
			starlark.Tuple{starlark.String("very-long-suffix")},
			[]starlark.Tuple{{starlark.String("xr_name"), starlark.String(xrName)}})
		got := string(result.(starlark.String))
		if len(got) > 63 {
			t.Errorf("result length = %d, want <= 63", len(got))
		}
		if strings.Contains(got, "--") {
			t.Errorf("truncated name has double hyphen (trailing hyphen not stripped): %q", got)
		}
	})

	t.Run("reads_from_oxr_when_no_xr_name", func(t *testing.T) {
		result := callStarlark(t, resourceName,
			starlark.Tuple{starlark.String("bucket")}, nil)
		got := string(result.(starlark.String))
		if got != "my-xr-bucket" {
			t.Errorf("resource_name('bucket') = %q, want %q (should read xr_name from oxr)", got, "my-xr-bucket")
		}
	})

	t.Run("sanitize_uppercase", func(t *testing.T) {
		result := callStarlark(t, resourceName,
			starlark.Tuple{starlark.String("Bucket")},
			[]starlark.Tuple{{starlark.String("xr_name"), starlark.String("My-XR")}})
		got := string(result.(starlark.String))
		if got != "my-xr-bucket" {
			t.Errorf("resource_name('Bucket', xr_name='My-XR') = %q, want %q", got, "my-xr-bucket")
		}
	})

	t.Run("sanitize_underscores_and_spaces", func(t *testing.T) {
		result := callStarlark(t, resourceName,
			starlark.Tuple{starlark.String("s3_bucket")},
			[]starlark.Tuple{{starlark.String("xr_name"), starlark.String("my xr name")}})
		got := string(result.(starlark.String))
		if got != "my-xr-name-s3-bucket" {
			t.Errorf("got %q, want %q", got, "my-xr-name-s3-bucket")
		}
	})

	t.Run("sanitize_dots_and_special_chars", func(t *testing.T) {
		result := callStarlark(t, resourceName,
			starlark.Tuple{starlark.String("rds")},
			[]starlark.Tuple{{starlark.String("xr_name"), starlark.String("my.xr@v2")}})
		got := string(result.(starlark.String))
		if got != "my-xr-v2-rds" {
			t.Errorf("got %q, want %q", got, "my-xr-v2-rds")
		}
	})

	t.Run("sanitize_consecutive_invalid_chars", func(t *testing.T) {
		result := callStarlark(t, resourceName,
			starlark.Tuple{starlark.String("bucket")},
			[]starlark.Tuple{{starlark.String("xr_name"), starlark.String("my___xr")}})
		got := string(result.(starlark.String))
		if got != "my-xr-bucket" {
			t.Errorf("got %q, want %q", got, "my-xr-bucket")
		}
	})
}

// ---------------------------------------------------------------------------
// TestStdlibLabels
// ---------------------------------------------------------------------------

func TestStdlibLabels(t *testing.T) {
	mockOxr := buildMockOxr("test-composite", "my-claim", "default")
	getFn := starlark.NewBuiltin("get", mockGetImpl)
	pre := starlark.StringDict{"oxr": mockOxr, "get": getFn}
	exports := loadModule(t, "labels.star", pre)

	standardLabels := exports["standard_labels"]
	crossplaneLabels := exports["crossplane_labels"]
	mergeLabels := exports["merge_labels"]

	t.Run("standard_labels_defaults", func(t *testing.T) {
		result := callStarlark(t, standardLabels,
			starlark.Tuple{starlark.String("my-app")}, nil)
		d := result.(*starlark.Dict)
		assertDictEntry(t, d, "app.kubernetes.io/name", "my-app")
		assertDictEntry(t, d, "app.kubernetes.io/managed-by", "crossplane")
		// version should not be present by default.
		if _, found, _ := d.Get(starlark.String("app.kubernetes.io/version")); found {
			t.Error("standard_labels should not include version by default")
		}
	})

	t.Run("standard_labels_all_options", func(t *testing.T) {
		result := callStarlark(t, standardLabels,
			starlark.Tuple{starlark.String("my-app")},
			[]starlark.Tuple{
				{starlark.String("version"), starlark.String("v1")},
				{starlark.String("component"), starlark.String("api")},
				{starlark.String("part_of"), starlark.String("platform")},
				{starlark.String("instance"), starlark.String("prod-1")},
			})
		d := result.(*starlark.Dict)
		assertDictEntry(t, d, "app.kubernetes.io/name", "my-app")
		assertDictEntry(t, d, "app.kubernetes.io/version", "v1")
		assertDictEntry(t, d, "app.kubernetes.io/component", "api")
		assertDictEntry(t, d, "app.kubernetes.io/part-of", "platform")
		assertDictEntry(t, d, "app.kubernetes.io/instance", "prod-1")
		assertDictEntry(t, d, "app.kubernetes.io/managed-by", "crossplane")
	})

	t.Run("crossplane_labels_from_oxr", func(t *testing.T) {
		result := callStarlark(t, crossplaneLabels, nil, nil)
		d := result.(*starlark.Dict)
		assertDictEntry(t, d, "crossplane.io/composite", "test-composite")
		assertDictEntry(t, d, "crossplane.io/claim-name", "my-claim")
		assertDictEntry(t, d, "crossplane.io/claim-namespace", "default")
	})

	t.Run("merge_labels_override", func(t *testing.T) {
		d1 := starlark.NewDict(2)
		_ = d1.SetKey(starlark.String("a"), starlark.String("1"))
		_ = d1.SetKey(starlark.String("b"), starlark.String("2"))
		d2 := starlark.NewDict(1)
		_ = d2.SetKey(starlark.String("a"), starlark.String("3"))

		result := callStarlark(t, mergeLabels, starlark.Tuple{d1, d2}, nil)
		d := result.(*starlark.Dict)
		assertDictEntry(t, d, "a", "3") // d2 overrides d1
		assertDictEntry(t, d, "b", "2") // b preserved from d1
	})

	t.Run("merge_labels_three_dicts", func(t *testing.T) {
		d1 := starlark.NewDict(1)
		_ = d1.SetKey(starlark.String("x"), starlark.String("1"))
		d2 := starlark.NewDict(1)
		_ = d2.SetKey(starlark.String("y"), starlark.String("2"))
		d3 := starlark.NewDict(1)
		_ = d3.SetKey(starlark.String("x"), starlark.String("3"))

		result := callStarlark(t, mergeLabels, starlark.Tuple{d1, d2, d3}, nil)
		d := result.(*starlark.Dict)
		assertDictEntry(t, d, "x", "3") // d3 overrides d1
		assertDictEntry(t, d, "y", "2") // y from d2
	})
}

// ---------------------------------------------------------------------------
// TestStdlibConditions
// ---------------------------------------------------------------------------

func TestStdlibConditions(t *testing.T) {
	recorder := &callRecorder{}

	setConditionFn := starlark.NewBuiltin("set_condition",
		func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
			recorder.record("set_condition", kwargs)
			return starlark.None, nil
		})

	emitEventFn := starlark.NewBuiltin("emit_event",
		func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
			recorder.record("emit_event", kwargs)
			return starlark.None, nil
		})

	pre := starlark.StringDict{
		"set_condition": setConditionFn,
		"emit_event":    emitEventFn,
	}
	exports := loadModule(t, "conditions.star", pre)

	degradedFn := exports["degraded"]

	t.Run("degraded_calls_both", func(t *testing.T) {
		recorder.reset()
		callStarlark(t, degradedFn,
			starlark.Tuple{starlark.String("DBFailing")},
			[]starlark.Tuple{{starlark.String("message"), starlark.String("connection timeout")}})

		calls := recorder.getCalls()
		if len(calls) != 2 {
			t.Fatalf("expected 2 calls (set_condition + emit_event), got %d", len(calls))
		}
		// First call: set_condition.
		if calls[0].name != "set_condition" {
			t.Errorf("first call = %q, want set_condition", calls[0].name)
		}
		assertCallKwarg(t, calls[0], "type", `"Degraded"`)
		assertCallKwarg(t, calls[0], "status", `"True"`)
		assertCallKwarg(t, calls[0], "reason", `"DBFailing"`)

		// Second call: emit_event.
		if calls[1].name != "emit_event" {
			t.Errorf("second call = %q, want emit_event", calls[1].name)
		}
		assertCallKwarg(t, calls[1], "severity", `"Warning"`)
		if !strings.Contains(calls[1].kwargs["message"], "Degraded") {
			t.Errorf("emit_event message = %q, want it to contain 'Degraded'", calls[1].kwargs["message"])
		}
	})
}

// ---------------------------------------------------------------------------
// TestStdlibConventions
// ---------------------------------------------------------------------------

func TestStdlibConventions(t *testing.T) {
	// Create a recording set_condition/emit_event to detect module-level calls.
	loadRecorder := &callRecorder{}

	setConditionFn := starlark.NewBuiltin("set_condition",
		func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
			loadRecorder.record("set_condition", kwargs)
			return starlark.None, nil
		})

	emitEventFn := starlark.NewBuiltin("emit_event",
		func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
			loadRecorder.record("emit_event", kwargs)
			return starlark.None, nil
		})

	resourceFn := starlark.NewBuiltin("Resource",
		func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
			loadRecorder.record("Resource", kwargs)
			return starlark.None, nil
		})

	mockOxr := buildMockOxr("test-xr", "", "")
	getFn := starlark.NewBuiltin("get", mockGetImpl)

	// Mock observed dict for conditions.star (all_ready/any_degraded use observed.keys()).
	mockObserved := starlark.NewDict(0)
	mockObserved.Freeze()

	// Mock get_condition for conditions.star (all_ready/any_degraded use get_condition).
	getConditionFn := starlark.NewBuiltin("get_condition",
		func(_ *starlark.Thread, _ *starlark.Builtin, _ starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
			return starlark.None, nil
		})

	fullPre := starlark.StringDict{
		"oxr":           mockOxr,
		"get":           getFn,
		"set_condition": setConditionFn,
		"emit_event":    emitEventFn,
		"Resource":      resourceFn,
		"observed":      mockObserved,
		"get_condition": getConditionFn,
	}

	modules := []string{
		"networking.star",
		"naming.star",
		"labels.star",
		"conditions.star",
	}

	t.Run("no_module_level_side_effects", func(t *testing.T) {
		for _, mod := range modules {
			loadRecorder.reset()
			_ = loadModule(t, mod, fullPre)
			calls := loadRecorder.getCalls()
			if len(calls) > 0 {
				for _, c := range calls {
					t.Errorf("%s: module-level call to %s(%v)", mod, c.name, c.kwargs)
				}
			}
		}
	})

	t.Run("all_exports_callable", func(t *testing.T) {
		for _, mod := range modules {
			exports := loadModule(t, mod, fullPre)
			for name, val := range exports {
				if strings.HasPrefix(name, "_") {
					continue // private should not be exported
				}
				// ALL_CAPS constants are OK as non-callable.
				if name == strings.ToUpper(name) {
					continue
				}
				if _, ok := val.(starlark.Callable); !ok {
					t.Errorf("%s: export %q is %s, not callable", mod, name, val.Type())
				}
			}
		}
	})

	t.Run("no_private_names_exported", func(t *testing.T) {
		for _, mod := range modules {
			exports := loadModule(t, mod, fullPre)
			for name := range exports {
				if strings.HasPrefix(name, "_") {
					t.Errorf("%s: private name %q should not be exported", mod, name)
				}
			}
		}
	})
}

// ---------------------------------------------------------------------------
// TestStdlibConditionsAggregation
// ---------------------------------------------------------------------------

// makeCondDict creates a condition dict with {status, reason, message, lastTransitionTime}.
func makeCondDict(status, reason, message, lastTransitionTime string) *starlark.Dict {
	d := starlark.NewDict(4)
	_ = d.SetKey(starlark.String("status"), starlark.String(status))
	_ = d.SetKey(starlark.String("reason"), starlark.String(reason))
	_ = d.SetKey(starlark.String("message"), starlark.String(message))
	_ = d.SetKey(starlark.String("lastTransitionTime"), starlark.String(lastTransitionTime))
	return d
}

// buildCondAggPredeclared builds predeclared globals for conditions aggregation tests.
// condData maps resource name -> condition type -> condition dict.
// observedKeys lists the keys to put in the observed dict.
func buildCondAggPredeclared(
	condData map[string]map[string]starlark.Value,
	observedKeys []string,
) starlark.StringDict {
	// Build observed dict with keys matching observedKeys (values are None -- only keys() used).
	observed := starlark.NewDict(len(observedKeys))
	for _, k := range observedKeys {
		_ = observed.SetKey(starlark.String(k), starlark.None)
	}
	observed.Freeze()

	// Build mock get_condition.
	getConditionFn := starlark.NewBuiltin("get_condition",
		func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
			var name, condType string
			if err := starlark.UnpackArgs("get_condition", args, kwargs,
				"name", &name, "type", &condType); err != nil {
				return nil, err
			}
			if types, ok := condData[name]; ok {
				if cond, ok := types[condType]; ok {
					return cond, nil
				}
			}
			return starlark.None, nil
		})

	// Mock set_condition and emit_event (required by conditions.star for degraded()).
	setConditionFn := starlark.NewBuiltin("set_condition",
		func(_ *starlark.Thread, _ *starlark.Builtin, _ starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
			return starlark.None, nil
		})
	emitEventFn := starlark.NewBuiltin("emit_event",
		func(_ *starlark.Thread, _ *starlark.Builtin, _ starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
			return starlark.None, nil
		})

	return starlark.StringDict{
		"observed":      observed,
		"get_condition": getConditionFn,
		"set_condition": setConditionFn,
		"emit_event":    emitEventFn,
	}
}

func TestStdlibConditionsAggregation(t *testing.T) {
	// --- all_ready tests ---

	t.Run("all_ready_all_true", func(t *testing.T) {
		condData := map[string]map[string]starlark.Value{
			"db":    {"Ready": makeCondDict("True", "Available", "", "")},
			"cache": {"Ready": makeCondDict("True", "Available", "", "")},
		}
		pre := buildCondAggPredeclared(condData, []string{"db", "cache"})
		exports := loadModule(t, "conditions.star", pre)
		result := callStarlark(t, exports["all_ready"], nil, nil)
		if result != starlark.True {
			t.Errorf("all_ready() = %v, want True", result)
		}
	})

	t.Run("all_ready_one_not_ready", func(t *testing.T) {
		condData := map[string]map[string]starlark.Value{
			"db":    {"Ready": makeCondDict("True", "Available", "", "")},
			"cache": {"Ready": makeCondDict("False", "Initializing", "", "")},
		}
		pre := buildCondAggPredeclared(condData, []string{"db", "cache"})
		exports := loadModule(t, "conditions.star", pre)
		result := callStarlark(t, exports["all_ready"], nil, nil)
		if result != starlark.False {
			t.Errorf("all_ready() = %v, want False", result)
		}
	})

	t.Run("all_ready_no_conditions", func(t *testing.T) {
		condData := map[string]map[string]starlark.Value{
			"db": {}, // No conditions at all.
		}
		pre := buildCondAggPredeclared(condData, []string{"db"})
		exports := loadModule(t, "conditions.star", pre)
		result := callStarlark(t, exports["all_ready"], nil, nil)
		if result != starlark.False {
			t.Errorf("all_ready() with no conditions = %v, want False", result)
		}
	})

	t.Run("all_ready_none_zero_observed", func(t *testing.T) {
		condData := map[string]map[string]starlark.Value{}
		pre := buildCondAggPredeclared(condData, []string{}) // No observed resources.
		exports := loadModule(t, "conditions.star", pre)
		// Call with resources=None (default).
		result := callStarlark(t, exports["all_ready"], nil, nil)
		if result != starlark.False {
			t.Errorf("all_ready(None) with zero observed = %v, want False", result)
		}
	})

	t.Run("all_ready_explicit_empty_list", func(t *testing.T) {
		condData := map[string]map[string]starlark.Value{}
		pre := buildCondAggPredeclared(condData, []string{})
		exports := loadModule(t, "conditions.star", pre)
		// Call with resources=[].
		emptyList := starlark.NewList(nil)
		result := callStarlark(t, exports["all_ready"], starlark.Tuple{emptyList}, nil)
		if result != starlark.True {
			t.Errorf("all_ready([]) = %v, want True (vacuous truth)", result)
		}
	})

	t.Run("all_ready_named_resources", func(t *testing.T) {
		condData := map[string]map[string]starlark.Value{
			"db":    {"Ready": makeCondDict("True", "Available", "", "")},
			"cache": {"Ready": makeCondDict("False", "Initializing", "", "")},
		}
		pre := buildCondAggPredeclared(condData, []string{"db", "cache"})
		exports := loadModule(t, "conditions.star", pre)
		// Only check "db" which is ready -- should be True.
		namesList := starlark.NewList([]starlark.Value{starlark.String("db")})
		result := callStarlark(t, exports["all_ready"], starlark.Tuple{namesList}, nil)
		if result != starlark.True {
			t.Errorf("all_ready(['db']) = %v, want True", result)
		}
	})

	// --- any_degraded tests ---

	t.Run("any_degraded_ready_false", func(t *testing.T) {
		condData := map[string]map[string]starlark.Value{
			"db":    {"Ready": makeCondDict("True", "Available", "", ""), "Synced": makeCondDict("True", "", "", "")},
			"cache": {"Ready": makeCondDict("False", "Failing", "", ""), "Synced": makeCondDict("True", "", "", "")},
		}
		pre := buildCondAggPredeclared(condData, []string{"db", "cache"})
		exports := loadModule(t, "conditions.star", pre)
		result := callStarlark(t, exports["any_degraded"], nil, nil)
		if result != starlark.True {
			t.Errorf("any_degraded() with Ready=False = %v, want True", result)
		}
	})

	t.Run("any_degraded_synced_false", func(t *testing.T) {
		condData := map[string]map[string]starlark.Value{
			"db": {"Ready": makeCondDict("True", "Available", "", ""), "Synced": makeCondDict("False", "OutOfSync", "", "")},
		}
		pre := buildCondAggPredeclared(condData, []string{"db"})
		exports := loadModule(t, "conditions.star", pre)
		result := callStarlark(t, exports["any_degraded"], nil, nil)
		if result != starlark.True {
			t.Errorf("any_degraded() with Synced=False = %v, want True", result)
		}
	})

	t.Run("any_degraded_all_healthy", func(t *testing.T) {
		condData := map[string]map[string]starlark.Value{
			"db":    {"Ready": makeCondDict("True", "Available", "", ""), "Synced": makeCondDict("True", "", "", "")},
			"cache": {"Ready": makeCondDict("True", "Available", "", ""), "Synced": makeCondDict("True", "", "", "")},
		}
		pre := buildCondAggPredeclared(condData, []string{"db", "cache"})
		exports := loadModule(t, "conditions.star", pre)
		result := callStarlark(t, exports["any_degraded"], nil, nil)
		if result != starlark.False {
			t.Errorf("any_degraded() with all healthy = %v, want False", result)
		}
	})

	t.Run("any_degraded_no_conditions", func(t *testing.T) {
		condData := map[string]map[string]starlark.Value{
			"db": {}, // No conditions at all.
		}
		pre := buildCondAggPredeclared(condData, []string{"db"})
		exports := loadModule(t, "conditions.star", pre)
		result := callStarlark(t, exports["any_degraded"], nil, nil)
		if result != starlark.False {
			t.Errorf("any_degraded() with no conditions = %v, want False (not degraded)", result)
		}
	})

	t.Run("any_degraded_none_zero_observed", func(t *testing.T) {
		condData := map[string]map[string]starlark.Value{}
		pre := buildCondAggPredeclared(condData, []string{})
		exports := loadModule(t, "conditions.star", pre)
		result := callStarlark(t, exports["any_degraded"], nil, nil)
		if result != starlark.False {
			t.Errorf("any_degraded(None) with zero observed = %v, want False", result)
		}
	})

	t.Run("any_degraded_explicit_empty_list", func(t *testing.T) {
		condData := map[string]map[string]starlark.Value{}
		pre := buildCondAggPredeclared(condData, []string{})
		exports := loadModule(t, "conditions.star", pre)
		emptyList := starlark.NewList(nil)
		result := callStarlark(t, exports["any_degraded"], starlark.Tuple{emptyList}, nil)
		if result != starlark.False {
			t.Errorf("any_degraded([]) = %v, want False (vacuous)", result)
		}
	})
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func assertDictEntry(t *testing.T, d *starlark.Dict, key, want string) {
	t.Helper()
	v, found, _ := d.Get(starlark.String(key))
	if !found {
		t.Errorf("dict missing key %q", key)
		return
	}
	got := string(v.(starlark.String))
	if got != want {
		t.Errorf("dict[%q] = %q, want %q", key, got, want)
	}
}

func assertCallKwarg(t *testing.T, call callRecord, key, want string) {
	t.Helper()
	got, ok := call.kwargs[key]
	if !ok {
		t.Errorf("call %q missing kwarg %q", call.name, key)
		return
	}
	if got != want {
		t.Errorf("call %q kwarg %q = %q, want %q", call.name, key, got, want)
	}
}
