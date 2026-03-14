package builtins

import (
	"testing"

	"github.com/crossplane/function-sdk-go/resource"
	"go.starlark.net/starlark"
)

func TestNewCollector(t *testing.T) {
	c := NewCollector()
	if c == nil {
		t.Fatal("NewCollector returned nil")
	}
	res := c.Resources()
	if len(res) != 0 {
		t.Errorf("Resources() = %d, want 0", len(res))
	}
}

func TestCollector_SingleResource(t *testing.T) {
	c := NewCollector()
	thread := new(starlark.Thread)

	body := new(starlark.Dict)
	_ = body.SetKey(starlark.String("apiVersion"), starlark.String("v1"))
	_ = body.SetKey(starlark.String("kind"), starlark.String("Bucket"))

	_, err := starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String("bucket"),
		body,
	}, nil)
	if err != nil {
		t.Fatalf("Resource() error: %v", err)
	}

	res := c.Resources()
	if len(res) != 1 {
		t.Fatalf("Resources() = %d, want 1", len(res))
	}
	cr, ok := res["bucket"]
	if !ok {
		t.Fatal("missing resource 'bucket'")
	}
	if cr.Body == nil {
		t.Fatal("body is nil")
	}
	if cr.Body.GetFields()["apiVersion"].GetStringValue() != "v1" {
		t.Errorf("apiVersion = %q, want 'v1'", cr.Body.GetFields()["apiVersion"].GetStringValue())
	}
}

func TestCollector_ReadyDefault(t *testing.T) {
	c := NewCollector()
	thread := new(starlark.Thread)

	body := new(starlark.Dict)
	_ = body.SetKey(starlark.String("apiVersion"), starlark.String("v1"))

	_, err := starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String("item"),
		body,
	}, nil)
	if err != nil {
		t.Fatalf("error: %v", err)
	}

	cr := c.Resources()["item"]
	if cr.Ready != resource.ReadyTrue {
		t.Errorf("Ready = %v, want ReadyTrue", cr.Ready)
	}
}

func TestCollector_ReadyFalse(t *testing.T) {
	c := NewCollector()
	thread := new(starlark.Thread)

	body := new(starlark.Dict)
	_ = body.SetKey(starlark.String("apiVersion"), starlark.String("v1"))

	_, err := starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String("item"),
		body,
	}, []starlark.Tuple{
		{starlark.String("ready"), starlark.False},
	})
	if err != nil {
		t.Fatalf("error: %v", err)
	}

	cr := c.Resources()["item"]
	if cr.Ready != resource.ReadyFalse {
		t.Errorf("Ready = %v, want ReadyFalse", cr.Ready)
	}
}

func TestCollector_LastWins(t *testing.T) {
	c := NewCollector()
	thread := new(starlark.Thread)

	body1 := new(starlark.Dict)
	_ = body1.SetKey(starlark.String("kind"), starlark.String("First"))

	body2 := new(starlark.Dict)
	_ = body2.SetKey(starlark.String("kind"), starlark.String("Second"))

	_, _ = starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String("item"),
		body1,
	}, nil)

	_, _ = starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String("item"),
		body2,
	}, nil)

	res := c.Resources()
	if len(res) != 1 {
		t.Fatalf("Resources() = %d, want 1 (last-wins)", len(res))
	}
	if res["item"].Body.GetFields()["kind"].GetStringValue() != "Second" {
		t.Errorf("kind = %q, want 'Second'", res["item"].Body.GetFields()["kind"].GetStringValue())
	}
}

func TestCollector_NonStringName(t *testing.T) {
	c := NewCollector()
	thread := new(starlark.Thread)

	body := new(starlark.Dict)

	// Pass an integer as name instead of string
	_, err := starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.MakeInt(42),
		body,
	}, nil)
	if err == nil {
		t.Fatal("Resource() with non-string name should return error")
	}
}

func TestCollector_NonDictBody(t *testing.T) {
	c := NewCollector()
	thread := new(starlark.Thread)

	// Pass a string as body instead of dict
	_, err := starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String("item"),
		starlark.String("not a dict"),
	}, nil)
	if err == nil {
		t.Fatal("Resource() with non-dict body should return error")
	}
}

func TestCollector_ResourcesCopy(t *testing.T) {
	c := NewCollector()
	thread := new(starlark.Thread)

	body := new(starlark.Dict)
	_ = body.SetKey(starlark.String("apiVersion"), starlark.String("v1"))

	_, _ = starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String("item"),
		body,
	}, nil)

	res1 := c.Resources()
	res2 := c.Resources()

	// Modifying returned map should not affect collector
	delete(res1, "item")
	if len(res2) != 1 {
		t.Error("Resources() should return a copy, not a reference")
	}
}

func TestCollector_MultipleDistinct(t *testing.T) {
	c := NewCollector()
	thread := new(starlark.Thread)

	names := []string{"bucket", "queue", "topic"}
	kinds := []string{"Bucket", "Queue", "Topic"}

	for i, name := range names {
		body := new(starlark.Dict)
		_ = body.SetKey(starlark.String("kind"), starlark.String(kinds[i]))

		_, err := starlark.Call(thread, c.Builtin(), starlark.Tuple{
			starlark.String(name),
			body,
		}, nil)
		if err != nil {
			t.Fatalf("Resource(%q) error: %v", name, err)
		}
	}

	res := c.Resources()
	if len(res) != 3 {
		t.Fatalf("Resources() = %d, want 3", len(res))
	}
	for i, name := range names {
		cr, ok := res[name]
		if !ok {
			t.Errorf("missing resource %q", name)
			continue
		}
		got := cr.Body.GetFields()["kind"].GetStringValue()
		if got != kinds[i] {
			t.Errorf("%s kind = %q, want %q", name, got, kinds[i])
		}
	}
}

func TestCollector_EmptyBody(t *testing.T) {
	c := NewCollector()
	thread := new(starlark.Thread)

	body := new(starlark.Dict) // empty

	_, err := starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String("empty-item"),
		body,
	}, nil)
	if err != nil {
		t.Fatalf("Resource() error: %v", err)
	}

	res := c.Resources()
	cr, ok := res["empty-item"]
	if !ok {
		t.Fatal("missing resource 'empty-item'")
	}
	if cr.Body == nil {
		t.Fatal("body is nil")
	}
	if len(cr.Body.GetFields()) != 0 {
		t.Errorf("fields = %d, want 0", len(cr.Body.GetFields()))
	}
}
