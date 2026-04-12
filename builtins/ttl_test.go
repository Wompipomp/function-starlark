package builtins

import (
	"testing"
	"time"

	"go.starlark.net/starlark"
)

func TestTTLCollector_NotSet(t *testing.T) {
	tc := NewTTLCollector()
	if got := tc.TTL(); got != nil {
		t.Errorf("TTL() = %v, want nil", got)
	}
}

func TestTTLCollector_StringDuration(t *testing.T) {
	tests := []struct {
		input string
		want  time.Duration
	}{
		{"30s", 30 * time.Second},
		{"1m30s", 90 * time.Second},
		{"5m", 5 * time.Minute},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			tc := NewTTLCollector()
			thread := &starlark.Thread{Name: "test"}
			builtin := tc.SetResponseTTLBuiltin()
			_, err := starlark.Call(thread, builtin, starlark.Tuple{starlark.String(tt.input)}, nil)
			if err != nil {
				t.Fatalf("set_response_ttl(%q) error: %v", tt.input, err)
			}
			got := tc.TTL()
			if got == nil {
				t.Fatal("TTL() = nil, want non-nil")
			}
			if *got != tt.want {
				t.Errorf("TTL() = %v, want %v", *got, tt.want)
			}
		})
	}
}

func TestTTLCollector_IntDuration(t *testing.T) {
	tests := []struct {
		name  string
		input int
		want  time.Duration
	}{
		{"60 seconds", 60, 60 * time.Second},
		{"1 second", 1, 1 * time.Second},
		{"120 seconds", 120, 120 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc := NewTTLCollector()
			thread := &starlark.Thread{Name: "test"}
			builtin := tc.SetResponseTTLBuiltin()
			_, err := starlark.Call(thread, builtin, starlark.Tuple{starlark.MakeInt(tt.input)}, nil)
			if err != nil {
				t.Fatalf("set_response_ttl(%d) error: %v", tt.input, err)
			}
			got := tc.TTL()
			if got == nil {
				t.Fatal("TTL() = nil, want non-nil")
			}
			if *got != tt.want {
				t.Errorf("TTL() = %v, want %v", *got, tt.want)
			}
		})
	}
}

func TestTTLCollector_ZeroClearsTTL(t *testing.T) {
	// Zero int: TTL() returns pointer to 0 (not nil).
	t.Run("int zero", func(t *testing.T) {
		tc := NewTTLCollector()
		thread := &starlark.Thread{Name: "test"}
		builtin := tc.SetResponseTTLBuiltin()
		_, err := starlark.Call(thread, builtin, starlark.Tuple{starlark.MakeInt(0)}, nil)
		if err != nil {
			t.Fatalf("set_response_ttl(0) error: %v", err)
		}
		got := tc.TTL()
		if got == nil {
			t.Fatal("TTL() = nil, want pointer to 0")
		}
		if *got != 0 {
			t.Errorf("TTL() = %v, want 0", *got)
		}
	})

	// Zero string: TTL() returns pointer to 0 (not nil).
	t.Run("string zero", func(t *testing.T) {
		tc := NewTTLCollector()
		thread := &starlark.Thread{Name: "test"}
		builtin := tc.SetResponseTTLBuiltin()
		_, err := starlark.Call(thread, builtin, starlark.Tuple{starlark.String("0s")}, nil)
		if err != nil {
			t.Fatalf("set_response_ttl(\"0s\") error: %v", err)
		}
		got := tc.TTL()
		if got == nil {
			t.Fatal("TTL() = nil, want pointer to 0")
		}
		if *got != 0 {
			t.Errorf("TTL() = %v, want 0", *got)
		}
	})
}

func TestTTLCollector_Negative(t *testing.T) {
	tests := []struct {
		name  string
		input starlark.Value
	}{
		{"negative int", starlark.MakeInt(-1)},
		{"negative string", starlark.String("-5s")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc := NewTTLCollector()
			thread := &starlark.Thread{Name: "test"}
			builtin := tc.SetResponseTTLBuiltin()
			_, err := starlark.Call(thread, builtin, starlark.Tuple{tt.input}, nil)
			if err == nil {
				t.Fatal("expected error for negative duration")
			}
			if got := err.Error(); !contains(got, "duration must be non-negative") {
				t.Errorf("error = %q, want to contain 'duration must be non-negative'", got)
			}
		})
	}
}

func TestTTLCollector_InvalidType(t *testing.T) {
	tests := []struct {
		name  string
		input starlark.Value
		want  string
	}{
		{"bool", starlark.True, "got bool, want string or int"},
		{"list", starlark.NewList(nil), "got list, want string or int"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc := NewTTLCollector()
			thread := &starlark.Thread{Name: "test"}
			builtin := tc.SetResponseTTLBuiltin()
			_, err := starlark.Call(thread, builtin, starlark.Tuple{tt.input}, nil)
			if err == nil {
				t.Fatal("expected error for invalid type")
			}
			if got := err.Error(); !contains(got, tt.want) {
				t.Errorf("error = %q, want to contain %q", got, tt.want)
			}
		})
	}
}

func TestTTLCollector_LastWriteWins(t *testing.T) {
	tc := NewTTLCollector()
	thread := &starlark.Thread{Name: "test"}
	builtin := tc.SetResponseTTLBuiltin()

	// First call: 30s
	_, err := starlark.Call(thread, builtin, starlark.Tuple{starlark.String("30s")}, nil)
	if err != nil {
		t.Fatalf("first set_response_ttl error: %v", err)
	}

	// Second call: 60s (should overwrite)
	_, err = starlark.Call(thread, builtin, starlark.Tuple{starlark.String("60s")}, nil)
	if err != nil {
		t.Fatalf("second set_response_ttl error: %v", err)
	}

	got := tc.TTL()
	if got == nil {
		t.Fatal("TTL() = nil, want 60s")
	}
	if *got != 60*time.Second {
		t.Errorf("TTL() = %v, want %v", *got, 60*time.Second)
	}
}

// contains is a helper for substring matching in error messages.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && containsSubstr(s, substr)
}

func containsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
