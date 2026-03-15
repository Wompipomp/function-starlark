package builtins

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/crossplane/function-sdk-go/resource"
	"go.starlark.net/starlark"
)

func TestNewCollector(t *testing.T) {
	c := NewCollector(NewConditionCollector())
	if c == nil {
		t.Fatal("NewCollector returned nil")
	}
	res := c.Resources()
	if len(res) != 0 {
		t.Errorf("Resources() = %d, want 0", len(res))
	}
}

func TestCollector_SingleResource(t *testing.T) {
	c := NewCollector(NewConditionCollector())
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
	c := NewCollector(NewConditionCollector())
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
	if cr.Ready != resource.ReadyUnspecified {
		t.Errorf("Ready = %v, want ReadyUnspecified", cr.Ready)
	}
}

func TestCollector_ReadyTrue(t *testing.T) {
	c := NewCollector(NewConditionCollector())
	thread := new(starlark.Thread)

	body := new(starlark.Dict)
	_ = body.SetKey(starlark.String("apiVersion"), starlark.String("v1"))

	_, err := starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String("item"),
		body,
	}, []starlark.Tuple{
		{starlark.String("ready"), starlark.True},
	})
	if err != nil {
		t.Fatalf("error: %v", err)
	}

	cr := c.Resources()["item"]
	if cr.Ready != resource.ReadyTrue {
		t.Errorf("Ready = %v, want ReadyTrue", cr.Ready)
	}
}

func TestCollector_ReadyFalse(t *testing.T) {
	c := NewCollector(NewConditionCollector())
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
	c := NewCollector(NewConditionCollector())
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
	c := NewCollector(NewConditionCollector())
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
	c := NewCollector(NewConditionCollector())
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
	c := NewCollector(NewConditionCollector())
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
	c := NewCollector(NewConditionCollector())
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
	c := NewCollector(NewConditionCollector())
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

// --- ResourceRef type tests ---

func TestResourceRef_String(t *testing.T) {
	ref := &ResourceRef{name: "my-db"}
	if got := ref.String(); got != "my-db" {
		t.Errorf("String() = %q, want %q", got, "my-db")
	}
}

func TestResourceRef_Type(t *testing.T) {
	ref := &ResourceRef{name: "my-db"}
	if got := ref.Type(); got != "ResourceRef" {
		t.Errorf("Type() = %q, want %q", got, "ResourceRef")
	}
}

func TestResourceRef_Truth(t *testing.T) {
	ref := &ResourceRef{name: "my-db"}
	if got := ref.Truth(); got != starlark.True {
		t.Errorf("Truth() = %v, want True", got)
	}
}

func TestResourceRef_Hash(t *testing.T) {
	ref := &ResourceRef{name: "my-db"}
	h1, err := ref.Hash()
	if err != nil {
		t.Fatalf("Hash() error: %v", err)
	}

	// Same name should produce same hash (deterministic).
	h2, err := ref.Hash()
	if err != nil {
		t.Fatalf("Hash() error on second call: %v", err)
	}
	if h1 != h2 {
		t.Errorf("Hash() not deterministic: %d != %d", h1, h2)
	}

	// Different name should (very likely) produce different hash.
	ref2 := &ResourceRef{name: "other-db"}
	h3, err := ref2.Hash()
	if err != nil {
		t.Fatalf("Hash() error: %v", err)
	}
	if h1 == h3 {
		t.Errorf("Hash() collision for 'my-db' and 'other-db': both %d", h1)
	}
}

func TestResourceRef_Attr_Name(t *testing.T) {
	ref := &ResourceRef{name: "my-db"}
	v, err := ref.Attr("name")
	if err != nil {
		t.Fatalf("Attr('name') error: %v", err)
	}
	s, ok := v.(starlark.String)
	if !ok {
		t.Fatalf("Attr('name') returned %T, want starlark.String", v)
	}
	if string(s) != "my-db" {
		t.Errorf("Attr('name') = %q, want %q", string(s), "my-db")
	}
}

func TestResourceRef_Attr_Unknown(t *testing.T) {
	ref := &ResourceRef{name: "my-db"}
	v, err := ref.Attr("unknown")
	if err != nil {
		t.Fatalf("Attr('unknown') error: %v", err)
	}
	if v != nil {
		t.Errorf("Attr('unknown') = %v, want nil", v)
	}
}

func TestResourceRef_AttrNames(t *testing.T) {
	ref := &ResourceRef{name: "my-db"}
	names := ref.AttrNames()
	if len(names) != 1 || names[0] != "name" {
		t.Errorf("AttrNames() = %v, want [name]", names)
	}
}

func TestResourceRef_Freeze(t *testing.T) {
	ref := &ResourceRef{name: "my-db"}
	// Freeze is a no-op; just verify it doesn't panic.
	ref.Freeze()
}

// --- Resource() returns *ResourceRef ---

func TestCollector_ResourceReturnsRef(t *testing.T) {
	c := NewCollector(NewConditionCollector())
	thread := new(starlark.Thread)

	body := new(starlark.Dict)
	_ = body.SetKey(starlark.String("apiVersion"), starlark.String("v1"))

	val, err := starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String("my-bucket"),
		body,
	}, nil)
	if err != nil {
		t.Fatalf("Resource() error: %v", err)
	}

	ref, ok := val.(*ResourceRef)
	if !ok {
		t.Fatalf("Resource() returned %T, want *ResourceRef", val)
	}
	if ref.name != "my-bucket" {
		t.Errorf("ResourceRef.name = %q, want %q", ref.name, "my-bucket")
	}
}

// --- depends_on kwarg tests ---

func TestCollector_DependsOn_ResourceRef(t *testing.T) {
	c := NewCollector(NewConditionCollector())
	thread := new(starlark.Thread)

	// Create a resource to get a ResourceRef.
	body := new(starlark.Dict)
	_ = body.SetKey(starlark.String("kind"), starlark.String("DB"))

	dbVal, err := starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String("db"),
		body,
	}, nil)
	if err != nil {
		t.Fatalf("Resource('db') error: %v", err)
	}

	// Create app with depends_on=[db_ref].
	appBody := new(starlark.Dict)
	_ = appBody.SetKey(starlark.String("kind"), starlark.String("App"))

	depsList := starlark.NewList([]starlark.Value{dbVal})
	_, err = starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String("app"),
		appBody,
	}, []starlark.Tuple{
		{starlark.String("depends_on"), depsList},
	})
	if err != nil {
		t.Fatalf("Resource('app') error: %v", err)
	}

	deps := c.Dependencies()
	if len(deps) != 1 {
		t.Fatalf("Dependencies() len = %d, want 1", len(deps))
	}
	if deps[0].Dependent != "app" {
		t.Errorf("Dependent = %q, want %q", deps[0].Dependent, "app")
	}
	if deps[0].Dependency != "db" {
		t.Errorf("Dependency = %q, want %q", deps[0].Dependency, "db")
	}
	if !deps[0].IsRef {
		t.Error("IsRef = false, want true")
	}
}

func TestCollector_DependsOn_String(t *testing.T) {
	c := NewCollector(NewConditionCollector())
	thread := new(starlark.Thread)

	body := new(starlark.Dict)
	_ = body.SetKey(starlark.String("kind"), starlark.String("App"))

	depsList := starlark.NewList([]starlark.Value{starlark.String("external-vpc")})
	_, err := starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String("app"),
		body,
	}, []starlark.Tuple{
		{starlark.String("depends_on"), depsList},
	})
	if err != nil {
		t.Fatalf("Resource('app') error: %v", err)
	}

	deps := c.Dependencies()
	if len(deps) != 1 {
		t.Fatalf("Dependencies() len = %d, want 1", len(deps))
	}
	if deps[0].Dependent != "app" {
		t.Errorf("Dependent = %q, want %q", deps[0].Dependent, "app")
	}
	if deps[0].Dependency != "external-vpc" {
		t.Errorf("Dependency = %q, want %q", deps[0].Dependency, "external-vpc")
	}
	if deps[0].IsRef {
		t.Error("IsRef = true, want false")
	}
}

func TestCollector_DependsOn_Mixed(t *testing.T) {
	c := NewCollector(NewConditionCollector())
	thread := new(starlark.Thread)

	// Create db resource first.
	dbBody := new(starlark.Dict)
	_ = dbBody.SetKey(starlark.String("kind"), starlark.String("DB"))

	dbVal, err := starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String("db"),
		dbBody,
	}, nil)
	if err != nil {
		t.Fatalf("Resource('db') error: %v", err)
	}

	// Create app with depends_on=[db_ref, "external-vpc"].
	appBody := new(starlark.Dict)
	_ = appBody.SetKey(starlark.String("kind"), starlark.String("App"))

	depsList := starlark.NewList([]starlark.Value{dbVal, starlark.String("external-vpc")})
	_, err = starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String("app"),
		appBody,
	}, []starlark.Tuple{
		{starlark.String("depends_on"), depsList},
	})
	if err != nil {
		t.Fatalf("Resource('app') error: %v", err)
	}

	deps := c.Dependencies()
	if len(deps) != 2 {
		t.Fatalf("Dependencies() len = %d, want 2", len(deps))
	}

	// First: ResourceRef to db.
	if deps[0].Dependent != "app" || deps[0].Dependency != "db" || !deps[0].IsRef {
		t.Errorf("deps[0] = %+v, want {app, db, true}", deps[0])
	}

	// Second: string ref to external-vpc.
	if deps[1].Dependent != "app" || deps[1].Dependency != "external-vpc" || deps[1].IsRef {
		t.Errorf("deps[1] = %+v, want {app, external-vpc, false}", deps[1])
	}
}

func TestCollector_DependsOn_InvalidType(t *testing.T) {
	c := NewCollector(NewConditionCollector())
	thread := new(starlark.Thread)

	body := new(starlark.Dict)
	_ = body.SetKey(starlark.String("kind"), starlark.String("App"))

	// Pass an integer in depends_on -- should error.
	depsList := starlark.NewList([]starlark.Value{starlark.MakeInt(42)})
	_, err := starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String("app"),
		body,
	}, []starlark.Tuple{
		{starlark.String("depends_on"), depsList},
	})
	if err == nil {
		t.Fatal("Resource() with int in depends_on should error")
	}
	if !strings.Contains(err.Error(), "depends_on[0]") {
		t.Errorf("error = %q, should mention depends_on[0]", err.Error())
	}
}

func TestCollector_NoDependsOn(t *testing.T) {
	c := NewCollector(NewConditionCollector())
	thread := new(starlark.Thread)

	body := new(starlark.Dict)
	_ = body.SetKey(starlark.String("kind"), starlark.String("Item"))

	_, err := starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String("item"),
		body,
	}, nil)
	if err != nil {
		t.Fatalf("Resource() error: %v", err)
	}

	deps := c.Dependencies()
	if len(deps) != 0 {
		t.Errorf("Dependencies() len = %d, want 0 (no depends_on)", len(deps))
	}
}

func TestCollector_AddDependency_Concurrent(t *testing.T) {
	c := NewCollector(NewConditionCollector())

	const goroutines = 10
	const depsPerGoroutine = 100

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func(id int) {
			defer wg.Done()
			for i := 0; i < depsPerGoroutine; i++ {
				c.addDependency(
					fmt.Sprintf("dependent-%d-%d", id, i),
					fmt.Sprintf("dependency-%d-%d", id, i),
					true,
				)
			}
		}(g)
	}
	wg.Wait()

	deps := c.Dependencies()
	want := goroutines * depsPerGoroutine
	if len(deps) != want {
		t.Errorf("Dependencies() len = %d, want %d", len(deps), want)
	}
}

func TestCollector_DependenciesCopy(t *testing.T) {
	c := NewCollector(NewConditionCollector())
	thread := new(starlark.Thread)

	// Create db, then app depending on db.
	dbBody := new(starlark.Dict)
	_ = dbBody.SetKey(starlark.String("kind"), starlark.String("DB"))
	dbVal, _ := starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String("db"),
		dbBody,
	}, nil)

	appBody := new(starlark.Dict)
	_ = appBody.SetKey(starlark.String("kind"), starlark.String("App"))
	depsList := starlark.NewList([]starlark.Value{dbVal})
	_, _ = starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String("app"),
		appBody,
	}, []starlark.Tuple{
		{starlark.String("depends_on"), depsList},
	})

	deps1 := c.Dependencies()
	deps2 := c.Dependencies()

	// Mutating the returned slice should not affect the collector.
	deps1[0].Dependent = "mutated"
	if deps2[0].Dependent == "mutated" {
		t.Error("Dependencies() should return a copy, not a reference")
	}
}

// --- external_name kwarg tests ---

func TestCollector_ExternalName_Basic(t *testing.T) {
	cc := NewConditionCollector()
	c := NewCollector(cc)
	thread := new(starlark.Thread)

	body := new(starlark.Dict)
	_ = body.SetKey(starlark.String("apiVersion"), starlark.String("v1"))
	_ = body.SetKey(starlark.String("kind"), starlark.String("Bucket"))

	_, err := starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String("bucket"),
		body,
	}, []starlark.Tuple{
		{starlark.String("external_name"), starlark.String("my-bucket")},
	})
	if err != nil {
		t.Fatalf("Resource() error: %v", err)
	}

	res := c.Resources()
	cr := res["bucket"]
	if cr.Body == nil {
		t.Fatal("body is nil")
	}

	// Check metadata.annotations["crossplane.io/external-name"] = "my-bucket"
	metadata := cr.Body.GetFields()["metadata"].GetStructValue()
	if metadata == nil {
		t.Fatal("metadata is nil")
	}
	annotations := metadata.GetFields()["annotations"].GetStructValue()
	if annotations == nil {
		t.Fatal("annotations is nil")
	}
	got := annotations.GetFields()["crossplane.io/external-name"].GetStringValue()
	if got != "my-bucket" {
		t.Errorf("external-name annotation = %q, want %q", got, "my-bucket")
	}
}

func TestCollector_ExternalName_EmptyBody(t *testing.T) {
	cc := NewConditionCollector()
	c := NewCollector(cc)
	thread := new(starlark.Thread)

	body := new(starlark.Dict) // empty -- no metadata

	_, err := starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String("item"),
		body,
	}, []starlark.Tuple{
		{starlark.String("external_name"), starlark.String("x")},
	})
	if err != nil {
		t.Fatalf("Resource() error: %v", err)
	}

	res := c.Resources()
	cr := res["item"]

	// metadata.annotations path should be auto-created
	metadata := cr.Body.GetFields()["metadata"].GetStructValue()
	if metadata == nil {
		t.Fatal("metadata should be auto-created")
	}
	annotations := metadata.GetFields()["annotations"].GetStructValue()
	if annotations == nil {
		t.Fatal("annotations should be auto-created")
	}
	got := annotations.GetFields()["crossplane.io/external-name"].GetStringValue()
	if got != "x" {
		t.Errorf("external-name annotation = %q, want %q", got, "x")
	}
}

func TestCollector_ExternalName_Omitted(t *testing.T) {
	cc := NewConditionCollector()
	c := NewCollector(cc)
	thread := new(starlark.Thread)

	body := new(starlark.Dict)
	_ = body.SetKey(starlark.String("apiVersion"), starlark.String("v1"))

	_, err := starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String("item"),
		body,
	}, nil)
	if err != nil {
		t.Fatalf("Resource() error: %v", err)
	}

	res := c.Resources()
	cr := res["item"]

	// No metadata.annotations should be injected
	if cr.Body.GetFields()["metadata"] != nil {
		t.Error("metadata should not be present when external_name is omitted")
	}
}

func TestCollector_ExternalName_EmptyString(t *testing.T) {
	cc := NewConditionCollector()
	c := NewCollector(cc)
	thread := new(starlark.Thread)

	body := new(starlark.Dict)
	_ = body.SetKey(starlark.String("apiVersion"), starlark.String("v1"))

	_, err := starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String("item"),
		body,
	}, []starlark.Tuple{
		{starlark.String("external_name"), starlark.String("")},
	})
	if err == nil {
		t.Fatal("Resource() with external_name='' should return error")
	}
	if !strings.Contains(err.Error(), "must not be empty") {
		t.Errorf("error = %q, should contain 'must not be empty'", err.Error())
	}
}

func TestCollector_ExternalName_NonString(t *testing.T) {
	cc := NewConditionCollector()
	c := NewCollector(cc)
	thread := new(starlark.Thread)

	body := new(starlark.Dict)
	_ = body.SetKey(starlark.String("apiVersion"), starlark.String("v1"))

	_, err := starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String("item"),
		body,
	}, []starlark.Tuple{
		{starlark.String("external_name"), starlark.MakeInt(123)},
	})
	if err == nil {
		t.Fatal("Resource() with external_name=123 should return error")
	}
	if !strings.Contains(err.Error(), "must be string") {
		t.Errorf("error = %q, should contain 'must be string'", err.Error())
	}
}

func TestCollector_ExternalName_Conflict(t *testing.T) {
	cc := NewConditionCollector()
	c := NewCollector(cc)
	thread := new(starlark.Thread)

	// Build body with existing crossplane.io/external-name annotation
	annotations := new(starlark.Dict)
	_ = annotations.SetKey(starlark.String("crossplane.io/external-name"), starlark.String("old"))

	metadata := new(starlark.Dict)
	_ = metadata.SetKey(starlark.String("annotations"), annotations)

	body := new(starlark.Dict)
	_ = body.SetKey(starlark.String("apiVersion"), starlark.String("v1"))
	_ = body.SetKey(starlark.String("metadata"), metadata)

	_, err := starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String("bucket"),
		body,
	}, []starlark.Tuple{
		{starlark.String("external_name"), starlark.String("new")},
	})
	if err != nil {
		t.Fatalf("Resource() error: %v", err)
	}

	// Kwarg should win
	res := c.Resources()
	cr := res["bucket"]
	md := cr.Body.GetFields()["metadata"].GetStructValue()
	ann := md.GetFields()["annotations"].GetStructValue()
	got := ann.GetFields()["crossplane.io/external-name"].GetStringValue()
	if got != "new" {
		t.Errorf("external-name annotation = %q, want %q (kwarg should win)", got, "new")
	}

	// Warning event should be emitted
	events := cc.Events()
	if len(events) != 1 {
		t.Fatalf("Events() len = %d, want 1", len(events))
	}
	if events[0].Severity != "Warning" {
		t.Errorf("event severity = %q, want %q", events[0].Severity, "Warning")
	}
	wantMsg := `Resource "bucket": external_name kwarg "new" overrides annotation "old"`
	if events[0].Message != wantMsg {
		t.Errorf("event message = %q, want %q", events[0].Message, wantMsg)
	}
}

func TestCollector_ExternalName_NoConflict(t *testing.T) {
	cc := NewConditionCollector()
	c := NewCollector(cc)
	thread := new(starlark.Thread)

	body := new(starlark.Dict)
	_ = body.SetKey(starlark.String("apiVersion"), starlark.String("v1"))

	_, err := starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String("bucket"),
		body,
	}, []starlark.Tuple{
		{starlark.String("external_name"), starlark.String("my-bucket")},
	})
	if err != nil {
		t.Fatalf("Resource() error: %v", err)
	}

	// No warning should be emitted when there's no conflict
	events := cc.Events()
	if len(events) != 0 {
		t.Errorf("Events() len = %d, want 0 (no conflict)", len(events))
	}
}

func TestCollector_ExternalName_SharedBody(t *testing.T) {
	cc := NewConditionCollector()
	c := NewCollector(cc)
	thread := new(starlark.Thread)

	// Use the same body dict for two Resource() calls with different external_name values.
	body := new(starlark.Dict)
	_ = body.SetKey(starlark.String("apiVersion"), starlark.String("v1"))

	_, err := starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String("bucket-a"),
		body,
	}, []starlark.Tuple{
		{starlark.String("external_name"), starlark.String("name-a")},
	})
	if err != nil {
		t.Fatalf("Resource('bucket-a') error: %v", err)
	}

	_, err = starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String("bucket-b"),
		body,
	}, []starlark.Tuple{
		{starlark.String("external_name"), starlark.String("name-b")},
	})
	if err != nil {
		t.Fatalf("Resource('bucket-b') error: %v", err)
	}

	res := c.Resources()

	// Each resource should have its own correct annotation (no cross-contamination).
	aAnn := res["bucket-a"].Body.GetFields()["metadata"].GetStructValue().GetFields()["annotations"].GetStructValue()
	gotA := aAnn.GetFields()["crossplane.io/external-name"].GetStringValue()
	if gotA != "name-a" {
		t.Errorf("bucket-a external-name = %q, want %q", gotA, "name-a")
	}

	bAnn := res["bucket-b"].Body.GetFields()["metadata"].GetStructValue().GetFields()["annotations"].GetStructValue()
	gotB := bAnn.GetFields()["crossplane.io/external-name"].GetStringValue()
	if gotB != "name-b" {
		t.Errorf("bucket-b external-name = %q, want %q", gotB, "name-b")
	}
}
