package convert

import (
	"testing"

	"go.starlark.net/starlark"
	"google.golang.org/protobuf/types/known/structpb"
)

// mustStruct builds a *structpb.Struct from a Go map or fails the test.
func mustStruct(t *testing.T, m map[string]any) *structpb.Struct {
	t.Helper()
	s, err := structpb.NewStruct(m)
	if err != nil {
		t.Fatalf("structpb.NewStruct: %v", err)
	}
	return s
}

// TestLazyStarlarkDictEquivalentToEager verifies a lazily-materialized dict
// exposes the same keys, values, and nested structure as an eagerly-built one.
func TestLazyStarlarkDictEquivalentToEager(t *testing.T) {
	s := mustStruct(t, map[string]any{
		"apiVersion": "example.io/v1",
		"kind":       "Widget",
		"metadata":   map[string]any{"name": "w1", "labels": map[string]any{"env": "prod"}},
		"spec":       map[string]any{"replicas": 3, "enabled": true},
	})

	eager, err := StructToStarlark(s, true)
	if err != nil {
		t.Fatalf("StructToStarlark: %v", err)
	}
	lazy := NewLazyStarlarkDict(s)
	lazy.Freeze()

	if lazy.Len() != eager.Len() {
		t.Fatalf("Len = %d, want %d", lazy.Len(), eager.Len())
	}

	// Top-level scalar.
	got, _, err := lazy.Get(starlark.String("kind"))
	if err != nil {
		t.Fatalf("Get(kind): %v", err)
	}
	if got != starlark.String("Widget") {
		t.Errorf("kind = %v, want Widget", got)
	}

	// Nested dot access through Attr.
	meta, err := lazy.Attr("metadata")
	if err != nil {
		t.Fatalf("Attr(metadata): %v", err)
	}
	metaMap, ok := meta.(starlark.Mapping)
	if !ok {
		t.Fatalf("metadata is %s, want mapping", meta.Type())
	}
	name, found, err := metaMap.Get(starlark.String("name"))
	if err != nil || !found {
		t.Fatalf("metadata.name lookup: found=%v err=%v", found, err)
	}
	if name != starlark.String("w1") {
		t.Errorf("metadata.name = %v, want w1", name)
	}
}

// TestLazyStarlarkDictFrozenAfterFreeze verifies that freezing before first
// access produces a frozen materialized dict (writes are rejected).
func TestLazyStarlarkDictFrozenAfterFreeze(t *testing.T) {
	lazy := NewLazyStarlarkDict(mustStruct(t, map[string]any{"a": 1}))
	lazy.Freeze()

	// Materialize via a read, then attempt a write -- must error (frozen).
	if _, _, err := lazy.Get(starlark.String("a")); err != nil {
		t.Fatalf("Get(a): %v", err)
	}
	if err := lazy.SetKey(starlark.String("b"), starlark.MakeInt(2)); err == nil {
		t.Fatal("SetKey on frozen lazy dict succeeded, want error")
	}
}

// TestLazyStarlarkDictMutableWhenNotFrozen verifies a lazy dict that is never
// frozen remains writable after materialization.
func TestLazyStarlarkDictMutableWhenNotFrozen(t *testing.T) {
	lazy := NewLazyStarlarkDict(mustStruct(t, map[string]any{"a": 1}))
	// No Freeze().
	if err := lazy.SetKey(starlark.String("b"), starlark.MakeInt(2)); err != nil {
		t.Fatalf("SetKey on unfrozen lazy dict: %v", err)
	}
	v, found, err := lazy.Get(starlark.String("b"))
	if err != nil || !found {
		t.Fatalf("Get(b): found=%v err=%v", found, err)
	}
	if v != starlark.MakeInt(2) {
		t.Errorf("b = %v, want 2", v)
	}
}

// TestLazyStarlarkDictDefersConversion is the core laziness guarantee: a body
// that would fail conversion (a number outside int64 range) produces NO error
// at construction or freeze time -- the error only surfaces when the body is
// actually read. This proves unread resources are never converted.
func TestLazyStarlarkDictDefersConversion(t *testing.T) {
	bad := mustStruct(t, map[string]any{"big": 1e19}) // exceeds int64 range

	lazy := NewLazyStarlarkDict(bad)
	lazy.Freeze() // must not materialize -> must not error

	// Eager conversion of the same struct DOES error, confirming the data is bad.
	if _, err := StructToStarlark(bad, true); err == nil {
		t.Fatal("expected eager StructToStarlark to reject out-of-range number")
	}

	// First read materializes and surfaces the deferred error.
	if _, _, err := lazy.Get(starlark.String("big")); err == nil {
		t.Fatal("expected deferred conversion error on access, got nil")
	}
}

// TestLazyStarlarkDictTruthAndLenWithoutError verifies the cheap metadata
// methods work on a well-formed lazy dict.
func TestLazyStarlarkDictTruthAndLen(t *testing.T) {
	empty := NewLazyStarlarkDict(mustStruct(t, map[string]any{}))
	if empty.Truth() {
		t.Error("empty lazy dict Truth() = true, want false")
	}
	nonEmpty := NewLazyStarlarkDict(mustStruct(t, map[string]any{"a": 1, "b": 2}))
	if !nonEmpty.Truth() {
		t.Error("non-empty lazy dict Truth() = false, want true")
	}
	if nonEmpty.Len() != 2 {
		t.Errorf("Len = %d, want 2", nonEmpty.Len())
	}
}

// TestLazyStarlarkDictRoundTripsBackToStruct verifies a lazy dict converts back
// to protobuf via StarlarkToStruct (the path used to re-emit observed bodies).
func TestLazyStarlarkDictRoundTripsBackToStruct(t *testing.T) {
	s := mustStruct(t, map[string]any{
		"kind": "Widget",
		"spec": map[string]any{"replicas": 3},
	})
	lazy := NewLazyStarlarkDict(s)
	lazy.Freeze()

	out, err := StarlarkToStruct(lazy)
	if err != nil {
		t.Fatalf("StarlarkToStruct: %v", err)
	}
	if got := out.GetFields()["kind"].GetStringValue(); got != "Widget" {
		t.Errorf("round-tripped kind = %q, want Widget", got)
	}
}
