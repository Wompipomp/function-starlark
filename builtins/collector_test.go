package builtins

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/crossplane/function-sdk-go/resource"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"go.starlark.net/starlark"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/wompipomp/function-starlark/convert"
	"github.com/wompipomp/function-starlark/metrics"
	"github.com/wompipomp/function-starlark/schema"
)

func TestNewCollector(t *testing.T) {
	c := NewCollector(NewConditionCollector(), "test.star", nil, nil)
	if c == nil {
		t.Fatal("NewCollector returned nil")
	}
	res := c.Resources()
	if len(res) != 0 {
		t.Errorf("Resources() = %d, want 0", len(res))
	}
}

func TestCollector_SingleResource(t *testing.T) {
	c := NewCollector(NewConditionCollector(), "test.star", nil, nil)
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
	c := NewCollector(NewConditionCollector(), "test.star", nil, nil)
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
	c := NewCollector(NewConditionCollector(), "test.star", nil, nil)
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
	c := NewCollector(NewConditionCollector(), "test.star", nil, nil)
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
	c := NewCollector(NewConditionCollector(), "test.star", nil, nil)
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
	c := NewCollector(NewConditionCollector(), "test.star", nil, nil)
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
	c := NewCollector(NewConditionCollector(), "test.star", nil, nil)
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
	c := NewCollector(NewConditionCollector(), "test.star", nil, nil)
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
	c := NewCollector(NewConditionCollector(), "test.star", nil, nil)
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
	c := NewCollector(NewConditionCollector(), "test.star", nil, nil)
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
	// Empty body still gets metadata with the resource-name label.
	metadata := cr.Body.GetFields()["metadata"]
	if metadata == nil {
		t.Fatal("metadata is nil; expected resource-name label")
	}
	labels := metadata.GetStructValue().GetFields()["labels"].GetStructValue().GetFields()
	if got := labels[ResourceNameLabel].GetStringValue(); got != "empty-item" {
		t.Errorf("resource-name label = %q, want %q", got, "empty-item")
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
	c := NewCollector(NewConditionCollector(), "test.star", nil, nil)
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
	c := NewCollector(NewConditionCollector(), "test.star", nil, nil)
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
	c := NewCollector(NewConditionCollector(), "test.star", nil, nil)
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
	c := NewCollector(NewConditionCollector(), "test.star", nil, nil)
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
	c := NewCollector(NewConditionCollector(), "test.star", nil, nil)
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
	c := NewCollector(NewConditionCollector(), "test.star", nil, nil)
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
	c := NewCollector(NewConditionCollector(), "test.star", nil, nil)

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
					"",
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
	c := NewCollector(NewConditionCollector(), "test.star", nil, nil)
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

// --- depends_on tuple tests ---

func TestCollector_DependsOn_Tuple(t *testing.T) {
	c := NewCollector(NewConditionCollector(), "test.star", nil, nil)
	thread := new(starlark.Thread)

	// Create db resource to get a ResourceRef.
	dbBody := new(starlark.Dict)
	_ = dbBody.SetKey(starlark.String("kind"), starlark.String("DB"))

	dbVal, err := starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String("db"),
		dbBody,
	}, nil)
	if err != nil {
		t.Fatalf("Resource('db') error: %v", err)
	}

	// Create app with depends_on=[(db_ref, "status.atProvider.id")].
	appBody := new(starlark.Dict)
	_ = appBody.SetKey(starlark.String("kind"), starlark.String("App"))

	tup := starlark.Tuple{dbVal, starlark.String("status.atProvider.id")}
	depsList := starlark.NewList([]starlark.Value{tup})
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
	if deps[0].FieldPath != "status.atProvider.id" {
		t.Errorf("FieldPath = %q, want %q", deps[0].FieldPath, "status.atProvider.id")
	}
}

func TestCollector_DependsOn_TupleString(t *testing.T) {
	c := NewCollector(NewConditionCollector(), "test.star", nil, nil)
	thread := new(starlark.Thread)

	body := new(starlark.Dict)
	_ = body.SetKey(starlark.String("kind"), starlark.String("App"))

	tup := starlark.Tuple{starlark.String("external-db"), starlark.String("status.ready")}
	depsList := starlark.NewList([]starlark.Value{tup})
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
	if deps[0].Dependency != "external-db" {
		t.Errorf("Dependency = %q, want %q", deps[0].Dependency, "external-db")
	}
	if deps[0].IsRef {
		t.Error("IsRef = true, want false")
	}
	if deps[0].FieldPath != "status.ready" {
		t.Errorf("FieldPath = %q, want %q", deps[0].FieldPath, "status.ready")
	}
}

func TestCollector_DependsOn_TupleEmptyFieldPath(t *testing.T) {
	c := NewCollector(NewConditionCollector(), "test.star", nil, nil)
	thread := new(starlark.Thread)

	body := new(starlark.Dict)
	_ = body.SetKey(starlark.String("kind"), starlark.String("App"))

	tup := starlark.Tuple{starlark.String("db"), starlark.String("")}
	depsList := starlark.NewList([]starlark.Value{tup})
	_, err := starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String("app"),
		body,
	}, []starlark.Tuple{
		{starlark.String("depends_on"), depsList},
	})
	if err == nil {
		t.Fatal("expected error for empty field path")
	}
	if !strings.Contains(err.Error(), "depends_on[0]") {
		t.Errorf("error = %q, should mention depends_on[0]", err.Error())
	}
}

func TestCollector_DependsOn_TupleBadFirstElement(t *testing.T) {
	c := NewCollector(NewConditionCollector(), "test.star", nil, nil)
	thread := new(starlark.Thread)

	body := new(starlark.Dict)
	_ = body.SetKey(starlark.String("kind"), starlark.String("App"))

	tup := starlark.Tuple{starlark.MakeInt(42), starlark.String("field.path")}
	depsList := starlark.NewList([]starlark.Value{tup})
	_, err := starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String("app"),
		body,
	}, []starlark.Tuple{
		{starlark.String("depends_on"), depsList},
	})
	if err == nil {
		t.Fatal("expected error for bad first tuple element")
	}
	if !strings.Contains(err.Error(), "depends_on[0]") {
		t.Errorf("error = %q, should mention depends_on[0]", err.Error())
	}
}

func TestCollector_DependsOn_TupleBadSecondElement(t *testing.T) {
	c := NewCollector(NewConditionCollector(), "test.star", nil, nil)
	thread := new(starlark.Thread)

	body := new(starlark.Dict)
	_ = body.SetKey(starlark.String("kind"), starlark.String("App"))

	dbBody := new(starlark.Dict)
	_ = dbBody.SetKey(starlark.String("kind"), starlark.String("DB"))

	dbVal, _ := starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String("db"),
		dbBody,
	}, nil)

	tup := starlark.Tuple{dbVal, starlark.MakeInt(42)}
	depsList := starlark.NewList([]starlark.Value{tup})
	_, err := starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String("app"),
		body,
	}, []starlark.Tuple{
		{starlark.String("depends_on"), depsList},
	})
	if err == nil {
		t.Fatal("expected error for bad second tuple element")
	}
	if !strings.Contains(err.Error(), "depends_on[0]") {
		t.Errorf("error = %q, should mention depends_on[0]", err.Error())
	}
}

func TestCollector_DependsOn_TupleWrongLength(t *testing.T) {
	c := NewCollector(NewConditionCollector(), "test.star", nil, nil)
	thread := new(starlark.Thread)

	body := new(starlark.Dict)
	_ = body.SetKey(starlark.String("kind"), starlark.String("App"))

	// 1-element tuple
	tup1 := starlark.Tuple{starlark.String("db")}
	depsList := starlark.NewList([]starlark.Value{tup1})
	_, err := starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String("app"),
		body,
	}, []starlark.Tuple{
		{starlark.String("depends_on"), depsList},
	})
	if err == nil {
		t.Fatal("expected error for 1-element tuple")
	}
	if !strings.Contains(err.Error(), "exactly 2 elements") {
		t.Errorf("error = %q, should mention 'exactly 2 elements'", err.Error())
	}

	// 3-element tuple
	tup3 := starlark.Tuple{starlark.String("db"), starlark.String("path"), starlark.String("extra")}
	depsList = starlark.NewList([]starlark.Value{tup3})
	_, err = starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String("app2"),
		body,
	}, []starlark.Tuple{
		{starlark.String("depends_on"), depsList},
	})
	if err == nil {
		t.Fatal("expected error for 3-element tuple")
	}
	if !strings.Contains(err.Error(), "exactly 2 elements") {
		t.Errorf("error = %q, should mention 'exactly 2 elements'", err.Error())
	}
}

func TestCollector_DependsOn_MixedTupleAndBare(t *testing.T) {
	c := NewCollector(NewConditionCollector(), "test.star", nil, nil)
	thread := new(starlark.Thread)

	// Create db resource.
	dbBody := new(starlark.Dict)
	_ = dbBody.SetKey(starlark.String("kind"), starlark.String("DB"))

	dbVal, err := starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String("db"),
		dbBody,
	}, nil)
	if err != nil {
		t.Fatalf("Resource('db') error: %v", err)
	}

	// Create app with depends_on=[db_ref, (db_ref, "status.id"), "external"].
	appBody := new(starlark.Dict)
	_ = appBody.SetKey(starlark.String("kind"), starlark.String("App"))

	tup := starlark.Tuple{dbVal, starlark.String("status.id")}
	depsList := starlark.NewList([]starlark.Value{dbVal, tup, starlark.String("external")})
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
	if len(deps) != 3 {
		t.Fatalf("Dependencies() len = %d, want 3", len(deps))
	}

	// First: bare ResourceRef (no FieldPath).
	if deps[0].FieldPath != "" {
		t.Errorf("deps[0].FieldPath = %q, want empty", deps[0].FieldPath)
	}
	if !deps[0].IsRef {
		t.Error("deps[0].IsRef = false, want true")
	}

	// Second: tuple (ResourceRef, field_path).
	if deps[1].FieldPath != "status.id" {
		t.Errorf("deps[1].FieldPath = %q, want %q", deps[1].FieldPath, "status.id")
	}
	if !deps[1].IsRef {
		t.Error("deps[1].IsRef = false, want true")
	}

	// Third: bare string (no FieldPath).
	if deps[2].FieldPath != "" {
		t.Errorf("deps[2].FieldPath = %q, want empty", deps[2].FieldPath)
	}
	if deps[2].IsRef {
		t.Error("deps[2].IsRef = true, want false")
	}
}

// --- external_name kwarg tests ---

func TestCollector_ExternalName_Basic(t *testing.T) {
	cc := NewConditionCollector()
	c := NewCollector(cc, "test.star", nil, nil)
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
	c := NewCollector(cc, "test.star", nil, nil)
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
	c := NewCollector(cc, "test.star", nil, nil)
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

	// metadata should exist (resource-name label) but no annotations
	metadata := cr.Body.GetFields()["metadata"]
	if metadata == nil {
		t.Fatal("metadata is nil; expected resource-name label")
	}
	annotations := metadata.GetStructValue().GetFields()["annotations"]
	if annotations != nil {
		t.Error("annotations should not be present when external_name is omitted")
	}
}

func TestCollector_ExternalName_EmptyString(t *testing.T) {
	cc := NewConditionCollector()
	c := NewCollector(cc, "test.star", nil, nil)
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
	c := NewCollector(cc, "test.star", nil, nil)
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
	c := NewCollector(cc, "test.star", nil, nil)
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
	c := NewCollector(cc, "test.star", nil, nil)
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

// --- skip_resource builtin tests ---

func TestCollector_SkipResource_ReturnsNone(t *testing.T) {
	cc := NewConditionCollector()
	c := NewCollector(cc, "test.star", nil, nil)
	thread := new(starlark.Thread)

	val, err := starlark.Call(thread, c.SkipResourceBuiltin(), starlark.Tuple{
		starlark.String("audit-logs"),
		starlark.String("encryption disabled"),
	}, nil)
	if err != nil {
		t.Fatalf("skip_resource() error: %v", err)
	}
	if val != starlark.None {
		t.Errorf("skip_resource() = %v, want None", val)
	}
}

func TestCollector_SkipResource_NotInResources(t *testing.T) {
	cc := NewConditionCollector()
	c := NewCollector(cc, "test.star", nil, nil)
	thread := new(starlark.Thread)

	_, err := starlark.Call(thread, c.SkipResourceBuiltin(), starlark.Tuple{
		starlark.String("audit-logs"),
		starlark.String("encryption disabled"),
	}, nil)
	if err != nil {
		t.Fatalf("skip_resource() error: %v", err)
	}

	res := c.Resources()
	if _, ok := res["audit-logs"]; ok {
		t.Error("skipped resource should not appear in Resources()")
	}
}

func TestCollector_SkipResource_Warning(t *testing.T) {
	cc := NewConditionCollector()
	c := NewCollector(cc, "test.star", nil, nil)
	thread := new(starlark.Thread)

	_, err := starlark.Call(thread, c.SkipResourceBuiltin(), starlark.Tuple{
		starlark.String("audit-logs"),
		starlark.String("encryption disabled"),
	}, nil)
	if err != nil {
		t.Fatalf("skip_resource() error: %v", err)
	}

	events := cc.Events()
	if len(events) != 1 {
		t.Fatalf("Events() len = %d, want 1", len(events))
	}
	if events[0].Severity != "Warning" {
		t.Errorf("event severity = %q, want %q", events[0].Severity, "Warning")
	}
	wantMsg := `Skipping resource "audit-logs": encryption disabled`
	if events[0].Message != wantMsg {
		t.Errorf("event message = %q, want %q", events[0].Message, wantMsg)
	}
	if events[0].Target != "Composite" {
		t.Errorf("event target = %q, want %q", events[0].Target, "Composite")
	}
}

func TestCollector_SkipResource_AfterEmit(t *testing.T) {
	cc := NewConditionCollector()
	c := NewCollector(cc, "test.star", nil, nil)
	thread := new(starlark.Thread)

	// Emit a resource first via Resource().
	body := new(starlark.Dict)
	_ = body.SetKey(starlark.String("kind"), starlark.String("DB"))

	_, err := starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String("db"),
		body,
	}, nil)
	if err != nil {
		t.Fatalf("Resource() error: %v", err)
	}

	// Now try to skip the same resource -- should error.
	_, err = starlark.Call(thread, c.SkipResourceBuiltin(), starlark.Tuple{
		starlark.String("db"),
		starlark.String("not needed"),
	}, nil)
	if err == nil {
		t.Fatal("skip_resource after Resource() should error")
	}
	if !strings.Contains(err.Error(), "already emitted, cannot skip") {
		t.Errorf("error = %q, should contain 'already emitted, cannot skip'", err.Error())
	}
}

func TestCollector_SkipResource_Dedup(t *testing.T) {
	cc := NewConditionCollector()
	c := NewCollector(cc, "test.star", nil, nil)
	thread := new(starlark.Thread)

	// Skip "x" twice.
	_, err := starlark.Call(thread, c.SkipResourceBuiltin(), starlark.Tuple{
		starlark.String("x"),
		starlark.String("r1"),
	}, nil)
	if err != nil {
		t.Fatalf("first skip_resource() error: %v", err)
	}

	_, err = starlark.Call(thread, c.SkipResourceBuiltin(), starlark.Tuple{
		starlark.String("x"),
		starlark.String("r2"),
	}, nil)
	if err != nil {
		t.Fatalf("second skip_resource() error: %v", err)
	}

	// Should only have 1 event (dedup).
	events := cc.Events()
	if len(events) != 1 {
		t.Errorf("Events() len = %d, want 1 (dedup)", len(events))
	}
}

func TestCollector_SkipResource_ThenResource(t *testing.T) {
	cc := NewConditionCollector()
	c := NewCollector(cc, "test.star", nil, nil)
	thread := new(starlark.Thread)

	// Skip "x" first.
	_, err := starlark.Call(thread, c.SkipResourceBuiltin(), starlark.Tuple{
		starlark.String("x"),
		starlark.String("not needed yet"),
	}, nil)
	if err != nil {
		t.Fatalf("skip_resource() error: %v", err)
	}

	// Then emit Resource("x", body) -- should succeed.
	body := new(starlark.Dict)
	_ = body.SetKey(starlark.String("kind"), starlark.String("Thing"))

	_, err = starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String("x"),
		body,
	}, nil)
	if err != nil {
		t.Fatalf("Resource() after skip_resource() should succeed: %v", err)
	}

	res := c.Resources()
	if _, ok := res["x"]; !ok {
		t.Error("Resource() after skip should appear in Resources()")
	}
}

func TestCollector_SkipResource_BadArgs(t *testing.T) {
	cc := NewConditionCollector()
	c := NewCollector(cc, "test.star", nil, nil)
	thread := new(starlark.Thread)

	// Call with wrong number of args (only 1 instead of 2).
	_, err := starlark.Call(thread, c.SkipResourceBuiltin(), starlark.Tuple{
		starlark.String("audit-logs"),
	}, nil)
	if err == nil {
		t.Fatal("skip_resource with wrong arg count should error")
	}
}

func TestCollector_ExternalName_SharedBody(t *testing.T) {
	cc := NewConditionCollector()
	c := NewCollector(cc, "test.star", nil, nil)
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

func TestCollector_SkipResource_Metrics(t *testing.T) {
	cc := NewConditionCollector()
	c := NewCollector(cc, "skip-metrics-test.star", nil, nil)
	thread := new(starlark.Thread)

	label := "skip-metrics-test.star"
	baseSkipped := testutil.ToFloat64(metrics.ResourcesSkippedTotal.WithLabelValues(label))

	// First skip_resource("x", "reason") -- should increment by 1.
	_, err := starlark.Call(thread, c.SkipResourceBuiltin(), starlark.Tuple{
		starlark.String("x"),
		starlark.String("reason"),
	}, nil)
	if err != nil {
		t.Fatalf("first skip_resource() error: %v", err)
	}

	delta := testutil.ToFloat64(metrics.ResourcesSkippedTotal.WithLabelValues(label)) - baseSkipped
	if delta != 1 {
		t.Errorf("skip counter delta after first skip = %v, want 1", delta)
	}

	// Duplicate skip_resource("x", "other") -- should NOT increment (dedup).
	baseSkipped = testutil.ToFloat64(metrics.ResourcesSkippedTotal.WithLabelValues(label))
	_, err = starlark.Call(thread, c.SkipResourceBuiltin(), starlark.Tuple{
		starlark.String("x"),
		starlark.String("other"),
	}, nil)
	if err != nil {
		t.Fatalf("second skip_resource() error: %v", err)
	}

	delta = testutil.ToFloat64(metrics.ResourcesSkippedTotal.WithLabelValues(label)) - baseSkipped
	if delta != 0 {
		t.Errorf("skip counter delta after dedup skip = %v, want 0", delta)
	}
}

// --- readyFromStarlark invalid type tests ---

func TestCollector_ReadyInvalidType(t *testing.T) {
	tests := []struct {
		name     string
		readyVal starlark.Value
		wantErr  string
	}{
		{
			name:     "string",
			readyVal: starlark.String("ready"),
			wantErr:  "ready must be True, False, or None, got string",
		},
		{
			name:     "int",
			readyVal: starlark.MakeInt(42),
			wantErr:  "ready must be True, False, or None, got int",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewCollector(NewConditionCollector(), "test.star", nil, nil)
			thread := new(starlark.Thread)

			body := new(starlark.Dict)
			_ = body.SetKey(starlark.String("apiVersion"), starlark.String("v1"))

			_, err := starlark.Call(thread, c.Builtin(), starlark.Tuple{
				starlark.String("item"),
				body,
			}, []starlark.Tuple{
				{starlark.String("ready"), tt.readyVal},
			})
			if err == nil {
				t.Fatalf("Resource() with ready=%s should return error", tt.readyVal.Type())
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, want to contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

// --- getOrCreateNestedStruct standalone tests ---

func TestGetOrCreateNestedStruct_ExistingChild(t *testing.T) {
	parent := &structpb.Struct{Fields: map[string]*structpb.Value{}}
	child := &structpb.Struct{Fields: map[string]*structpb.Value{
		"existing": structpb.NewStringValue("keep-me"),
	}}
	parent.Fields["metadata"] = structpb.NewStructValue(child)

	got := getOrCreateNestedStruct(parent, "metadata")
	if got != child {
		t.Fatal("should return existing struct, not create new one")
	}
	if got.Fields["existing"].GetStringValue() != "keep-me" {
		t.Error("existing field should be preserved")
	}
}

func TestGetOrCreateNestedStruct_OverwriteNonStruct(t *testing.T) {
	parent := &structpb.Struct{Fields: map[string]*structpb.Value{}}
	parent.Fields["metadata"] = structpb.NewStringValue("not-a-struct")

	got := getOrCreateNestedStruct(parent, "metadata")
	if got == nil {
		t.Fatal("should return a new struct")
	}
	if len(got.Fields) != 0 {
		t.Error("new struct should be empty")
	}
	// Parent should now point to the new struct.
	if parent.Fields["metadata"].GetStructValue() != got {
		t.Error("parent should point to newly created struct")
	}
}

// --- concurrent skip_resource test ---

func TestCollector_SkipResource_Concurrent(t *testing.T) {
	cc := NewConditionCollector()
	c := NewCollector(cc, "test.star", nil, nil)

	const goroutines = 10
	const skipsPerGoroutine = 50

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func(id int) {
			defer wg.Done()
			thread := new(starlark.Thread)
			for i := 0; i < skipsPerGoroutine; i++ {
				name := fmt.Sprintf("res-%d-%d", id, i)
				_, _ = starlark.Call(thread, c.SkipResourceBuiltin(), starlark.Tuple{
					starlark.String(name),
					starlark.String("reason"),
				}, nil)
			}
		}(g)
	}
	wg.Wait()

	// Each unique name should produce exactly one event.
	events := cc.Events()
	want := goroutines * skipsPerGoroutine
	if len(events) != want {
		t.Errorf("Events() len = %d, want %d", len(events), want)
	}
}

// --- NewCollector scriptName tests ---

func TestNewCollector_ScriptName(t *testing.T) {
	cc := NewConditionCollector()

	// Empty scriptName should work.
	c1 := NewCollector(cc, "", nil, nil)
	if c1.scriptName != "" {
		t.Errorf("scriptName = %q, want empty string", c1.scriptName)
	}

	// Constructor should set scriptName.
	c2 := NewCollector(cc, "my-script.star", nil, nil)
	if c2.scriptName != "my-script.star" {
		t.Errorf("scriptName = %q, want %q", c2.scriptName, "my-script.star")
	}
}

// --- Label injection tests ---

// makeOXR builds a *structpb.Struct representing an observed XR with the given
// metadata.name and optional claim labels.
func makeOXR(name, claimName, claimNamespace string) *structpb.Struct {
	mdFields := map[string]*structpb.Value{}
	if name != "" {
		mdFields["name"] = structpb.NewStringValue(name)
	}
	if claimName != "" || claimNamespace != "" {
		lblFields := map[string]*structpb.Value{}
		if claimName != "" {
			lblFields["crossplane.io/claim-name"] = structpb.NewStringValue(claimName)
		}
		if claimNamespace != "" {
			lblFields["crossplane.io/claim-namespace"] = structpb.NewStringValue(claimNamespace)
		}
		mdFields["labels"] = structpb.NewStructValue(&structpb.Struct{Fields: lblFields})
	}
	return &structpb.Struct{
		Fields: map[string]*structpb.Value{
			"metadata": structpb.NewStructValue(&structpb.Struct{Fields: mdFields}),
		},
	}
}

func TestCrossplaneLabelsFromOXR(t *testing.T) {
	oxr := makeOXR("xr-abc", "my-claim", "ns")
	labels := crossplaneLabelsFromOXR(oxr)

	if len(labels) != 3 {
		t.Fatalf("labels count = %d, want 3", len(labels))
	}
	if labels["crossplane.io/composite"] != "xr-abc" {
		t.Errorf("composite = %q, want %q", labels["crossplane.io/composite"], "xr-abc")
	}
	if labels["crossplane.io/claim-name"] != "my-claim" {
		t.Errorf("claim-name = %q, want %q", labels["crossplane.io/claim-name"], "my-claim")
	}
	if labels["crossplane.io/claim-namespace"] != "ns" {
		t.Errorf("claim-namespace = %q, want %q", labels["crossplane.io/claim-namespace"], "ns")
	}
}

func TestCrossplaneLabelsFromOXR_NoClaim(t *testing.T) {
	oxr := makeOXR("xr-abc", "", "")
	labels := crossplaneLabelsFromOXR(oxr)

	if len(labels) != 1 {
		t.Fatalf("labels count = %d, want 1", len(labels))
	}
	if labels["crossplane.io/composite"] != "xr-abc" {
		t.Errorf("composite = %q, want %q", labels["crossplane.io/composite"], "xr-abc")
	}
}

func TestCrossplaneLabelsFromOXR_Nil(t *testing.T) {
	labels := crossplaneLabelsFromOXR(nil)
	if len(labels) != 0 {
		t.Errorf("labels count = %d, want 0 for nil OXR", len(labels))
	}
}

func TestCrossplaneLabelsFromOXR_NilMetadata(t *testing.T) {
	oxr := &structpb.Struct{Fields: map[string]*structpb.Value{}}
	labels := crossplaneLabelsFromOXR(oxr)
	if len(labels) != 0 {
		t.Errorf("labels count = %d, want 0 for OXR with no metadata", len(labels))
	}
}

func TestCollector_Labels_Omitted(t *testing.T) {
	cc := NewConditionCollector()
	oxr := makeOXR("xr-abc", "my-claim", "ns")
	c := NewCollector(cc, "test.star", oxr, nil)
	thread := new(starlark.Thread)

	body := new(starlark.Dict)
	_ = body.SetKey(starlark.String("apiVersion"), starlark.String("v1"))

	_, err := starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String("bucket"),
		body,
	}, nil)
	if err != nil {
		t.Fatalf("Resource() error: %v", err)
	}

	res := c.Resources()
	cr := res["bucket"]
	metadata := cr.Body.GetFields()["metadata"].GetStructValue()
	if metadata == nil {
		t.Fatal("metadata is nil")
	}
	labels := metadata.GetFields()["labels"].GetStructValue()
	if labels == nil {
		t.Fatal("labels is nil")
	}

	if labels.GetFields()["crossplane.io/composite"].GetStringValue() != "xr-abc" {
		t.Errorf("composite = %q, want %q", labels.GetFields()["crossplane.io/composite"].GetStringValue(), "xr-abc")
	}
	if labels.GetFields()["crossplane.io/claim-name"].GetStringValue() != "my-claim" {
		t.Errorf("claim-name = %q, want %q", labels.GetFields()["crossplane.io/claim-name"].GetStringValue(), "my-claim")
	}
	if labels.GetFields()["crossplane.io/claim-namespace"].GetStringValue() != "ns" {
		t.Errorf("claim-namespace = %q, want %q", labels.GetFields()["crossplane.io/claim-namespace"].GetStringValue(), "ns")
	}
}

func TestCollector_Labels_BasicDict(t *testing.T) {
	cc := NewConditionCollector()
	oxr := makeOXR("xr-abc", "my-claim", "ns")
	c := NewCollector(cc, "test.star", oxr, nil)
	thread := new(starlark.Thread)

	body := new(starlark.Dict)
	_ = body.SetKey(starlark.String("apiVersion"), starlark.String("v1"))

	lblDict := new(starlark.Dict)
	_ = lblDict.SetKey(starlark.String("team"), starlark.String("platform"))

	_, err := starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String("bucket"),
		body,
	}, []starlark.Tuple{
		{starlark.String("labels"), lblDict},
	})
	if err != nil {
		t.Fatalf("Resource() error: %v", err)
	}

	res := c.Resources()
	cr := res["bucket"]
	labels := cr.Body.GetFields()["metadata"].GetStructValue().GetFields()["labels"].GetStructValue()

	// User label should be present.
	if labels.GetFields()["team"].GetStringValue() != "platform" {
		t.Errorf("team = %q, want %q", labels.GetFields()["team"].GetStringValue(), "platform")
	}
	// Crossplane labels should also be present.
	if labels.GetFields()["crossplane.io/composite"].GetStringValue() != "xr-abc" {
		t.Errorf("composite = %q, want %q", labels.GetFields()["crossplane.io/composite"].GetStringValue(), "xr-abc")
	}
}

func TestCollector_Labels_StarlarkDict(t *testing.T) {
	cc := NewConditionCollector()
	oxr := makeOXR("xr-abc", "", "")
	c := NewCollector(cc, "test.star", oxr, nil)
	thread := new(starlark.Thread)

	body := new(starlark.Dict)
	_ = body.SetKey(starlark.String("apiVersion"), starlark.String("v1"))

	// Use *convert.StarlarkDict instead of *starlark.Dict.
	sd := convert.NewStarlarkDict(1)
	_ = sd.SetField("env", starlark.String("prod"))

	_, err := starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String("bucket"),
		body,
	}, []starlark.Tuple{
		{starlark.String("labels"), sd},
	})
	if err != nil {
		t.Fatalf("Resource() error: %v", err)
	}

	res := c.Resources()
	labels := res["bucket"].Body.GetFields()["metadata"].GetStructValue().GetFields()["labels"].GetStructValue()

	if labels.GetFields()["env"].GetStringValue() != "prod" {
		t.Errorf("env = %q, want %q", labels.GetFields()["env"].GetStringValue(), "prod")
	}
	if labels.GetFields()["crossplane.io/composite"].GetStringValue() != "xr-abc" {
		t.Errorf("composite = %q, want %q", labels.GetFields()["crossplane.io/composite"].GetStringValue(), "xr-abc")
	}
}

func TestCollector_Labels_MergePriority(t *testing.T) {
	cc := NewConditionCollector()
	oxr := makeOXR("xr-abc", "", "")
	c := NewCollector(cc, "test.star", oxr, nil)
	thread := new(starlark.Thread)

	// Body has crossplane.io/composite="old"
	bodyLabels := new(starlark.Dict)
	_ = bodyLabels.SetKey(starlark.String("crossplane.io/composite"), starlark.String("old"))
	metadata := new(starlark.Dict)
	_ = metadata.SetKey(starlark.String("labels"), bodyLabels)
	body := new(starlark.Dict)
	_ = body.SetKey(starlark.String("metadata"), metadata)

	// Kwarg overrides crossplane.io/composite="custom"
	lblDict := new(starlark.Dict)
	_ = lblDict.SetKey(starlark.String("crossplane.io/composite"), starlark.String("custom"))

	_, err := starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String("bucket"),
		body,
	}, []starlark.Tuple{
		{starlark.String("labels"), lblDict},
	})
	if err != nil {
		t.Fatalf("Resource() error: %v", err)
	}

	res := c.Resources()
	labels := res["bucket"].Body.GetFields()["metadata"].GetStructValue().GetFields()["labels"].GetStructValue()

	// Kwarg should win over both body and auto-injected.
	got := labels.GetFields()["crossplane.io/composite"].GetStringValue()
	if got != "custom" {
		t.Errorf("composite = %q, want %q (kwarg should win)", got, "custom")
	}

	// Should have both body-vs-auto and kwarg-vs-auto warnings.
	events := cc.Events()
	if len(events) != 2 {
		t.Fatalf("Events() len = %d, want 2", len(events))
	}
}

func TestCollector_Labels_None(t *testing.T) {
	cc := NewConditionCollector()
	oxr := makeOXR("xr-abc", "my-claim", "ns")
	c := NewCollector(cc, "test.star", oxr, nil)
	thread := new(starlark.Thread)

	// Body has existing labels.
	bodyLabels := new(starlark.Dict)
	_ = bodyLabels.SetKey(starlark.String("existing"), starlark.String("keep-me"))
	metadata := new(starlark.Dict)
	_ = metadata.SetKey(starlark.String("labels"), bodyLabels)
	body := new(starlark.Dict)
	_ = body.SetKey(starlark.String("metadata"), metadata)

	_, err := starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String("bucket"),
		body,
	}, []starlark.Tuple{
		{starlark.String("labels"), starlark.None},
	})
	if err != nil {
		t.Fatalf("Resource() error: %v", err)
	}

	res := c.Resources()
	labels := res["bucket"].Body.GetFields()["metadata"].GetStructValue().GetFields()["labels"].GetStructValue()

	// Body label should be preserved.
	if labels.GetFields()["existing"].GetStringValue() != "keep-me" {
		t.Errorf("existing = %q, want %q", labels.GetFields()["existing"].GetStringValue(), "keep-me")
	}
	// No crossplane labels should be injected.
	if labels.GetFields()["crossplane.io/composite"] != nil {
		t.Error("crossplane.io/composite should not be present with labels=None")
	}
}

func TestCollector_Labels_EmptyDict(t *testing.T) {
	cc := NewConditionCollector()
	oxr := makeOXR("xr-abc", "", "")
	c := NewCollector(cc, "test.star", oxr, nil)
	thread := new(starlark.Thread)

	body := new(starlark.Dict)
	_ = body.SetKey(starlark.String("apiVersion"), starlark.String("v1"))

	emptyDict := new(starlark.Dict) // empty

	_, err := starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String("bucket"),
		body,
	}, []starlark.Tuple{
		{starlark.String("labels"), emptyDict},
	})
	if err != nil {
		t.Fatalf("Resource() error: %v", err)
	}

	res := c.Resources()
	labels := res["bucket"].Body.GetFields()["metadata"].GetStructValue().GetFields()["labels"].GetStructValue()

	// Auto-injection should still run with empty dict.
	if labels.GetFields()["crossplane.io/composite"].GetStringValue() != "xr-abc" {
		t.Errorf("composite = %q, want %q", labels.GetFields()["crossplane.io/composite"].GetStringValue(), "xr-abc")
	}
}

func TestCollector_Labels_NonStringKey(t *testing.T) {
	cc := NewConditionCollector()
	c := NewCollector(cc, "test.star", nil, nil)
	thread := new(starlark.Thread)

	body := new(starlark.Dict)

	lblDict := new(starlark.Dict)
	_ = lblDict.SetKey(starlark.MakeInt(42), starlark.String("val"))

	_, err := starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String("bucket"),
		body,
	}, []starlark.Tuple{
		{starlark.String("labels"), lblDict},
	})
	if err == nil {
		t.Fatal("Resource() with non-string label key should return error")
	}
	if !strings.Contains(err.Error(), "labels key must be string, got int") {
		t.Errorf("error = %q, should contain 'labels key must be string, got int'", err.Error())
	}
}

func TestCollector_Labels_NonStringValue(t *testing.T) {
	cc := NewConditionCollector()
	c := NewCollector(cc, "test.star", nil, nil)
	thread := new(starlark.Thread)

	body := new(starlark.Dict)

	lblDict := new(starlark.Dict)
	_ = lblDict.SetKey(starlark.String("k"), starlark.True)

	_, err := starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String("bucket"),
		body,
	}, []starlark.Tuple{
		{starlark.String("labels"), lblDict},
	})
	if err == nil {
		t.Fatal("Resource() with non-string label value should return error")
	}
	if !strings.Contains(err.Error(), "labels value must be string, got bool") {
		t.Errorf("error = %q, should contain 'labels value must be string, got bool'", err.Error())
	}
}

func TestCollector_Labels_BodyConflictWarning(t *testing.T) {
	cc := NewConditionCollector()
	oxr := makeOXR("xr-abc", "", "")
	c := NewCollector(cc, "test.star", oxr, nil)
	thread := new(starlark.Thread)

	// Body has crossplane.io/composite="old"
	bodyLabels := new(starlark.Dict)
	_ = bodyLabels.SetKey(starlark.String("crossplane.io/composite"), starlark.String("old"))
	metadata := new(starlark.Dict)
	_ = metadata.SetKey(starlark.String("labels"), bodyLabels)
	body := new(starlark.Dict)
	_ = body.SetKey(starlark.String("metadata"), metadata)

	_, err := starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String("bucket"),
		body,
	}, nil)
	if err != nil {
		t.Fatalf("Resource() error: %v", err)
	}

	events := cc.Events()
	if len(events) != 1 {
		t.Fatalf("Events() len = %d, want 1", len(events))
	}
	if events[0].Severity != "Warning" {
		t.Errorf("severity = %q, want %q", events[0].Severity, "Warning")
	}
	wantMsg := `Resource "bucket": body label "crossplane.io/composite"="old" overridden by auto-injected "xr-abc"`
	if events[0].Message != wantMsg {
		t.Errorf("message = %q, want %q", events[0].Message, wantMsg)
	}
}

func TestCollector_Labels_KwargConflictWarning(t *testing.T) {
	cc := NewConditionCollector()
	oxr := makeOXR("xr-abc", "", "")
	c := NewCollector(cc, "test.star", oxr, nil)
	thread := new(starlark.Thread)

	body := new(starlark.Dict)

	lblDict := new(starlark.Dict)
	_ = lblDict.SetKey(starlark.String("crossplane.io/composite"), starlark.String("custom"))

	_, err := starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String("bucket"),
		body,
	}, []starlark.Tuple{
		{starlark.String("labels"), lblDict},
	})
	if err != nil {
		t.Fatalf("Resource() error: %v", err)
	}

	events := cc.Events()
	if len(events) != 1 {
		t.Fatalf("Events() len = %d, want 1", len(events))
	}
	wantMsg := `Resource "bucket": labels= kwarg "crossplane.io/composite"="custom" overrides auto-injected "xr-abc"`
	if events[0].Message != wantMsg {
		t.Errorf("message = %q, want %q", events[0].Message, wantMsg)
	}
}

func TestCollector_Labels_KwargVsBodySilent(t *testing.T) {
	cc := NewConditionCollector()
	c := NewCollector(cc, "test.star", nil, nil)
	thread := new(starlark.Thread)

	// Body has a non-crossplane label.
	bodyLabels := new(starlark.Dict)
	_ = bodyLabels.SetKey(starlark.String("team"), starlark.String("old-team"))
	metadata := new(starlark.Dict)
	_ = metadata.SetKey(starlark.String("labels"), bodyLabels)
	body := new(starlark.Dict)
	_ = body.SetKey(starlark.String("metadata"), metadata)

	// Kwarg overrides the same non-crossplane label.
	lblDict := new(starlark.Dict)
	_ = lblDict.SetKey(starlark.String("team"), starlark.String("new-team"))

	_, err := starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String("bucket"),
		body,
	}, []starlark.Tuple{
		{starlark.String("labels"), lblDict},
	})
	if err != nil {
		t.Fatalf("Resource() error: %v", err)
	}

	// No warning should be emitted for kwarg-vs-body (no auto-injected involved).
	events := cc.Events()
	if len(events) != 0 {
		t.Errorf("Events() len = %d, want 0 (kwarg-vs-body is silent)", len(events))
	}

	// Kwarg value should win.
	res := c.Resources()
	labels := res["bucket"].Body.GetFields()["metadata"].GetStructValue().GetFields()["labels"].GetStructValue()
	if labels.GetFields()["team"].GetStringValue() != "new-team" {
		t.Errorf("team = %q, want %q", labels.GetFields()["team"].GetStringValue(), "new-team")
	}
}

func TestCollector_Labels_SharedBody(t *testing.T) {
	cc := NewConditionCollector()
	oxr := makeOXR("xr-abc", "", "")
	c := NewCollector(cc, "test.star", oxr, nil)
	thread := new(starlark.Thread)

	// Use the same body dict for two Resource() calls.
	body := new(starlark.Dict)
	_ = body.SetKey(starlark.String("apiVersion"), starlark.String("v1"))

	lblA := new(starlark.Dict)
	_ = lblA.SetKey(starlark.String("team"), starlark.String("alpha"))

	_, err := starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String("bucket-a"),
		body,
	}, []starlark.Tuple{
		{starlark.String("labels"), lblA},
	})
	if err != nil {
		t.Fatalf("Resource('bucket-a') error: %v", err)
	}

	lblB := new(starlark.Dict)
	_ = lblB.SetKey(starlark.String("team"), starlark.String("beta"))

	_, err = starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String("bucket-b"),
		body,
	}, []starlark.Tuple{
		{starlark.String("labels"), lblB},
	})
	if err != nil {
		t.Fatalf("Resource('bucket-b') error: %v", err)
	}

	res := c.Resources()

	labelsA := res["bucket-a"].Body.GetFields()["metadata"].GetStructValue().GetFields()["labels"].GetStructValue()
	labelsB := res["bucket-b"].Body.GetFields()["metadata"].GetStructValue().GetFields()["labels"].GetStructValue()

	if labelsA.GetFields()["team"].GetStringValue() != "alpha" {
		t.Errorf("bucket-a team = %q, want %q", labelsA.GetFields()["team"].GetStringValue(), "alpha")
	}
	if labelsB.GetFields()["team"].GetStringValue() != "beta" {
		t.Errorf("bucket-b team = %q, want %q", labelsB.GetFields()["team"].GetStringValue(), "beta")
	}
}

func TestCollector_Labels_InvalidType(t *testing.T) {
	cc := NewConditionCollector()
	c := NewCollector(cc, "test.star", nil, nil)
	thread := new(starlark.Thread)

	body := new(starlark.Dict)

	_, err := starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String("bucket"),
		body,
	}, []starlark.Tuple{
		{starlark.String("labels"), starlark.MakeInt(42)},
	})
	if err == nil {
		t.Fatal("Resource() with labels=42 should return error")
	}
	if !strings.Contains(err.Error(), "labels must be dict or None, got int") {
		t.Errorf("error = %q, should contain 'labels must be dict or None, got int'", err.Error())
	}
}

// --- recordSkip tests ---

func TestCollector_RecordSkip_Basic(t *testing.T) {
	cc := NewConditionCollector()
	c := NewCollector(cc, "test.star", nil, nil)

	label := "test.star"
	base := testutil.ToFloat64(metrics.ResourcesSkippedTotal.WithLabelValues(label))

	c.recordSkip("my-resource", "not needed", true)

	events := cc.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Severity != "Warning" {
		t.Errorf("event severity = %q, want %q", events[0].Severity, "Warning")
	}
	wantMsg := `Skipping resource "my-resource": not needed`
	if events[0].Message != wantMsg {
		t.Errorf("event message = %q, want %q", events[0].Message, wantMsg)
	}
	if events[0].Target != "Composite" {
		t.Errorf("event target = %q, want %q", events[0].Target, "Composite")
	}

	delta := testutil.ToFloat64(metrics.ResourcesSkippedTotal.WithLabelValues(label)) - base
	if delta != 1 {
		t.Errorf("metric delta = %v, want 1", delta)
	}
}

func TestCollector_RecordSkip_Dedup(t *testing.T) {
	cc := NewConditionCollector()
	c := NewCollector(cc, "recordskip-dedup.star", nil, nil)

	label := "recordskip-dedup.star"
	base := testutil.ToFloat64(metrics.ResourcesSkippedTotal.WithLabelValues(label))

	c.recordSkip("x", "r1", true)
	c.recordSkip("x", "r2", true)

	events := cc.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event after dedup, got %d", len(events))
	}

	delta := testutil.ToFloat64(metrics.ResourcesSkippedTotal.WithLabelValues(label)) - base
	if delta != 1 {
		t.Errorf("metric delta = %v, want 1 (dedup)", delta)
	}
}

func TestCollector_RecordSkip_DifferentNames(t *testing.T) {
	cc := NewConditionCollector()
	c := NewCollector(cc, "recordskip-diff.star", nil, nil)

	label := "recordskip-diff.star"
	base := testutil.ToFloat64(metrics.ResourcesSkippedTotal.WithLabelValues(label))

	c.recordSkip("a", "r1", true)
	c.recordSkip("b", "r2", true)

	events := cc.Events()
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}

	delta := testutil.ToFloat64(metrics.ResourcesSkippedTotal.WithLabelValues(label)) - base
	if delta != 2 {
		t.Errorf("metric delta = %v, want 2", delta)
	}
}

func TestCollector_RecordSkip_EventParity(t *testing.T) {
	cc := NewConditionCollector()
	// Collector A: test recordSkip directly.
	cA := NewCollector(cc, "parity.star", nil, nil)
	cA.recordSkip("x", "some reason", true)

	// Collector B: test via skip_resource builtin.
	cB := NewCollector(cc, "parity.star", nil, nil)
	thread := new(starlark.Thread)
	_, err := starlark.Call(thread, cB.SkipResourceBuiltin(), starlark.Tuple{
		starlark.String("y"),
		starlark.String("some reason"),
	}, nil)
	if err != nil {
		t.Fatalf("skip_resource error: %v", err)
	}

	events := cc.Events()
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}

	// Both events must have same structure (only name differs).
	for i, e := range events {
		if e.Severity != "Warning" {
			t.Errorf("event[%d] severity = %q, want %q", i, e.Severity, "Warning")
		}
		if e.Target != "Composite" {
			t.Errorf("event[%d] target = %q, want %q", i, e.Target, "Composite")
		}
	}
	wantA := `Skipping resource "x": some reason`
	if events[0].Message != wantA {
		t.Errorf("event[0] message = %q, want %q", events[0].Message, wantA)
	}
	wantB := `Skipping resource "y": some reason`
	if events[1].Message != wantB {
		t.Errorf("event[1] message = %q, want %q", events[1].Message, wantB)
	}
}

func TestCollector_RecordSkip_Concurrent(t *testing.T) {
	cc := NewConditionCollector()
	c := NewCollector(cc, "recordskip-concurrent.star", nil, nil)

	label := "recordskip-concurrent.star"
	base := testutil.ToFloat64(metrics.ResourcesSkippedTotal.WithLabelValues(label))

	var wg sync.WaitGroup
	for range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.recordSkip("shared", "reason", true)
		}()
	}
	wg.Wait()

	events := cc.Events()
	if len(events) != 1 {
		t.Errorf("expected exactly 1 event after concurrent dedup, got %d", len(events))
	}

	delta := testutil.ToFloat64(metrics.ResourcesSkippedTotal.WithLabelValues(label)) - base
	if delta != 1 {
		t.Errorf("metric delta = %v, want 1", delta)
	}
}

// whenVal constructs a *WhenValue for use in test kwargs.
func whenVal(condition bool, reason string, keepIfExists bool) *WhenValue {
	return &WhenValue{condition: condition, reason: reason, keepIfExists: keepIfExists, optional: false}
}

// whenValOptional constructs an optional *WhenValue for use in test kwargs.
func whenValOptional(condition bool, reason string, keepIfExists bool) *WhenValue {
	return &WhenValue{condition: condition, reason: reason, keepIfExists: keepIfExists, optional: true}
}

// ---------------------------------------------------------------------------
// GATE-01: Resource(when=When(False, "reason", keep_if_exists=False)) skips
// ---------------------------------------------------------------------------

func TestCollector_WhenFalse_SkipsResource(t *testing.T) {
	cc := NewConditionCollector()
	c := NewCollector(cc, "gate01.star", nil, nil)
	thread := new(starlark.Thread)

	body := new(starlark.Dict)
	_ = body.SetKey(starlark.String("apiVersion"), starlark.String("v1"))

	val, err := starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String("db"),
		body,
	}, []starlark.Tuple{
		{starlark.String("when"), whenVal(false, "not needed", false)},
	})
	if err != nil {
		t.Fatalf("Resource() error: %v", err)
	}

	// Return value is a *SkippedRef carrying the resource name (falsy in Starlark).
	if sr, ok := val.(*SkippedRef); !ok || sr.name != "db" {
		t.Errorf("Resource() = %v (%s), want *SkippedRef{name:\"db\"}", val, val.Type())
	}

	// Resource must NOT appear in collected resources.
	res := c.Resources()
	if _, ok := res["db"]; ok {
		t.Error("skipped resource should not appear in Resources()")
	}

	// recordSkip must have been called — check Warning event.
	events := cc.Events()
	if len(events) != 1 {
		t.Fatalf("Events() len = %d, want 1", len(events))
	}
	wantMsg := `Skipping resource "db": not needed`
	if events[0].Message != wantMsg {
		t.Errorf("event message = %q, want %q", events[0].Message, wantMsg)
	}
	if events[0].Severity != "Warning" {
		t.Errorf("event severity = %q, want %q", events[0].Severity, "Warning")
	}
}

func TestCollector_WhenFalse_SkipsResource_Metrics(t *testing.T) {
	cc := NewConditionCollector()
	c := NewCollector(cc, "gate01-metrics.star", nil, nil)
	thread := new(starlark.Thread)

	label := "gate01-metrics.star"
	base := testutil.ToFloat64(metrics.ResourcesSkippedTotal.WithLabelValues(label))

	body := new(starlark.Dict)
	_ = body.SetKey(starlark.String("kind"), starlark.String("Bucket"))

	_, err := starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String("bucket"),
		body,
	}, []starlark.Tuple{
		{starlark.String("when"), whenVal(false, "disabled", false)},
	})
	if err != nil {
		t.Fatalf("Resource() error: %v", err)
	}

	delta := testutil.ToFloat64(metrics.ResourcesSkippedTotal.WithLabelValues(label)) - base
	if delta != 1 {
		t.Errorf("metric delta = %v, want 1", delta)
	}
}

// ---------------------------------------------------------------------------
// GATE-02: Resource(when=<bare bool>) -> type error (must use When())
// ---------------------------------------------------------------------------

func TestCollector_WhenBareBool_False_Errors(t *testing.T) {
	cc := NewConditionCollector()
	c := NewCollector(cc, "test.star", nil, nil)
	thread := new(starlark.Thread)

	body := new(starlark.Dict)
	_ = body.SetKey(starlark.String("kind"), starlark.String("Bucket"))

	_, err := starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String("bucket"),
		body,
	}, []starlark.Tuple{
		{starlark.String("when"), starlark.False},
	})
	if err == nil {
		t.Fatal("expected error for when=False (bare bool)")
	}
	if !strings.Contains(err.Error(), "when must be a When() value") {
		t.Errorf("error = %q, want to contain %q", err.Error(), "when must be a When() value")
	}
}

func TestCollector_WhenBareBool_True_Errors(t *testing.T) {
	cc := NewConditionCollector()
	c := NewCollector(cc, "test.star", nil, nil)
	thread := new(starlark.Thread)

	body := new(starlark.Dict)
	_ = body.SetKey(starlark.String("kind"), starlark.String("Bucket"))

	_, err := starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String("bucket"),
		body,
	}, []starlark.Tuple{
		{starlark.String("when"), starlark.True},
	})
	if err == nil {
		t.Fatal("expected error for when=True (bare bool)")
	}
	if !strings.Contains(err.Error(), "when must be a When() value") {
		t.Errorf("error = %q, want to contain %q", err.Error(), "when must be a When() value")
	}
}

func TestCollector_WhenFalse_KeepIfExistsFalse(t *testing.T) {
	cc := NewConditionCollector()
	c := NewCollector(cc, "optout.star", nil, nil)
	thread := new(starlark.Thread)

	body := new(starlark.Dict)
	_ = body.SetKey(starlark.String("kind"), starlark.String("Bucket"))

	val, err := starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String("db"),
		body,
	}, []starlark.Tuple{
		{starlark.String("when"), whenVal(false, "intentional removal", false)},
	})
	if err != nil {
		t.Fatalf("Resource() error: %v", err)
	}

	// Return value is a *SkippedRef (intentional deletion).
	if sr, ok := val.(*SkippedRef); !ok || sr.name != "db" {
		t.Errorf("Resource() = %v (%s), want *SkippedRef{name:\"db\"}", val, val.Type())
	}

	// Resource must NOT appear (intentional deletion, not preserved).
	res := c.Resources()
	if _, ok := res["db"]; ok {
		t.Error("opted-out resource should not appear in Resources()")
	}

	// Warning event with user skip reason.
	events := cc.Events()
	if len(events) != 1 {
		t.Fatalf("Events() len = %d, want 1", len(events))
	}
	wantMsg := `Skipping resource "db": intentional removal`
	if events[0].Message != wantMsg {
		t.Errorf("event message = %q, want %q", events[0].Message, wantMsg)
	}
}

func TestCollector_WhenTrue_NormalEmission(t *testing.T) {
	cc := NewConditionCollector()
	c := NewCollector(cc, "test.star", nil, nil)
	thread := new(starlark.Thread)

	body := new(starlark.Dict)
	_ = body.SetKey(starlark.String("kind"), starlark.String("Bucket"))

	val, err := starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String("bucket"),
		body,
	}, []starlark.Tuple{
		{starlark.String("when"), whenVal(true, "some reason", false)},
	})
	if err != nil {
		t.Fatalf("Resource() error: %v", err)
	}

	// Return value must be a ResourceRef (resource emitted as desired).
	if _, ok := val.(*ResourceRef); !ok {
		t.Errorf("Resource() = %v (%s), want *ResourceRef", val, val.Type())
	}

	// Resource MUST appear in collected resources (not skipped).
	res := c.Resources()
	if _, ok := res["bucket"]; !ok {
		t.Error("expected resource \"bucket\" in Resources(), not found")
	}

	// No skip event must have been recorded.
	events := cc.Events()
	if len(events) != 0 {
		t.Errorf("Events() len = %d, want 0 (no skip event should be emitted)", len(events))
	}
}

// ---------------------------------------------------------------------------
// GATE-05: Resource(body=None) without When -> Warning + skip
// ---------------------------------------------------------------------------

func TestCollector_BodyNone_NoPreserve_WarnsAndSkips(t *testing.T) {
	cc := NewConditionCollector()
	c := NewCollector(cc, "test.star", nil, nil)
	thread := new(starlark.Thread)

	val, err := starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String("db"),
		starlark.None,
	}, nil)
	if err != nil {
		t.Fatalf("Resource() error: %v", err)
	}

	// Return value is a *SkippedRef (falsy in Starlark).
	if sr, ok := val.(*SkippedRef); !ok || sr.name != "db" {
		t.Errorf("Resource() = %v (%s), want *SkippedRef{name:\"db\"}", val, val.Type())
	}

	// Resource must NOT appear in collected resources.
	res := c.Resources()
	if _, ok := res["db"]; ok {
		t.Error("body=None resource should not appear in Resources()")
	}

	// Warning event must mention body=None risk.
	events := cc.Events()
	if len(events) != 1 {
		t.Fatalf("Events() len = %d, want 1", len(events))
	}
	wantMsg := `Skipping resource "db": body is None. If this resource exists, it will be removed from desired state. Use When() with keep_if_exists=True to re-emit the observed body.`
	if events[0].Message != wantMsg {
		t.Errorf("event message = %q, want %q", events[0].Message, wantMsg)
	}
	if events[0].Severity != "Warning" {
		t.Errorf("event severity = %q, want %q", events[0].Severity, "Warning")
	}
}

// ---------------------------------------------------------------------------
// GATE-07: when kwarg rejects non-bool values
// ---------------------------------------------------------------------------

func TestCollector_WhenKwarg_StrictType_Int(t *testing.T) {
	cc := NewConditionCollector()
	c := NewCollector(cc, "test.star", nil, nil)
	thread := new(starlark.Thread)

	body := new(starlark.Dict)
	_ = body.SetKey(starlark.String("kind"), starlark.String("Bucket"))

	_, err := starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String("bucket"),
		body,
	}, []starlark.Tuple{
		{starlark.String("when"), starlark.MakeInt(1)},
	})
	if err == nil {
		t.Fatal("expected error for when=1 (non-When)")
	}
	if !strings.Contains(err.Error(), "when must be a When() value") || !strings.Contains(err.Error(), "got int") {
		t.Errorf("error = %q, want to contain 'when must be a When() value' and 'got int'", err.Error())
	}
}

func TestCollector_WhenKwarg_StrictType_String(t *testing.T) {
	cc := NewConditionCollector()
	c := NewCollector(cc, "test.star", nil, nil)
	thread := new(starlark.Thread)

	body := new(starlark.Dict)
	_ = body.SetKey(starlark.String("kind"), starlark.String("Bucket"))

	_, err := starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String("bucket"),
		body,
	}, []starlark.Tuple{
		{starlark.String("when"), starlark.String("true")},
	})
	if err == nil {
		t.Fatal("expected error for when=\"true\" (non-When)")
	}
	if !strings.Contains(err.Error(), "when must be a When() value") || !strings.Contains(err.Error(), "got string") {
		t.Errorf("error = %q, want to contain 'when must be a When() value' and 'got string'", err.Error())
	}
}

// ---------------------------------------------------------------------------
// Normal paths: when=True and when omitted must work as before
// ---------------------------------------------------------------------------

func TestCollector_WhenTrue_NormalPath(t *testing.T) {
	cc := NewConditionCollector()
	c := NewCollector(cc, "test.star", nil, nil)
	thread := new(starlark.Thread)

	body := new(starlark.Dict)
	_ = body.SetKey(starlark.String("apiVersion"), starlark.String("v1"))
	_ = body.SetKey(starlark.String("kind"), starlark.String("Bucket"))

	val, err := starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String("bucket"),
		body,
	}, []starlark.Tuple{
		{starlark.String("when"), whenVal(true, "not needed in prod", false)},
	})
	if err != nil {
		t.Fatalf("Resource() error: %v", err)
	}

	// Return value must be ResourceRef.
	ref, ok := val.(*ResourceRef)
	if !ok {
		t.Fatalf("Resource() = %v (%s), want ResourceRef", val, val.Type())
	}
	if ref.name != "bucket" {
		t.Errorf("ResourceRef.name = %q, want %q", ref.name, "bucket")
	}

	// Resource must appear in collected resources.
	res := c.Resources()
	if _, ok := res["bucket"]; !ok {
		t.Error("when=True resource should appear in Resources()")
	}
}

func TestCollector_WhenOmitted_NormalPath(t *testing.T) {
	// This is implicitly tested by all existing tests but we add an explicit
	// one for the gate-logic matrix.
	cc := NewConditionCollector()
	c := NewCollector(cc, "test.star", nil, nil)
	thread := new(starlark.Thread)

	body := new(starlark.Dict)
	_ = body.SetKey(starlark.String("apiVersion"), starlark.String("v1"))

	val, err := starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String("item"),
		body,
	}, nil)
	if err != nil {
		t.Fatalf("Resource() error: %v", err)
	}

	ref, ok := val.(*ResourceRef)
	if !ok {
		t.Fatalf("Resource() = %v (%s), want ResourceRef", val, val.Type())
	}
	if ref.name != "item" {
		t.Errorf("ResourceRef.name = %q, want %q", ref.name, "item")
	}
}

// stripObservedReadOnlyFields removes read-only metadata fields (managedFields,
// resourceVersion, uid, generation, creationTimestamp) and status from a
// struct so that a keep_if_exists re-emission is accepted by server-side
// apply.
func TestStripObservedReadOnlyFields(t *testing.T) {
	s := &structpb.Struct{
		Fields: map[string]*structpb.Value{
			"apiVersion": structpb.NewStringValue("v1"),
			"metadata": structpb.NewStructValue(&structpb.Struct{
				Fields: map[string]*structpb.Value{
					"name":              structpb.NewStringValue("keep"),
					"managedFields":     structpb.NewListValue(&structpb.ListValue{}),
					"resourceVersion":   structpb.NewStringValue("42"),
					"uid":               structpb.NewStringValue("abc"),
					"generation":        structpb.NewNumberValue(3),
					"creationTimestamp": structpb.NewStringValue("2026-01-01T00:00:00Z"),
				},
			}),
			"status": structpb.NewStructValue(&structpb.Struct{
				Fields: map[string]*structpb.Value{"phase": structpb.NewStringValue("Running")},
			}),
		},
	}

	stripObservedReadOnlyFields(s)

	if _, ok := s.Fields["status"]; ok {
		t.Error("status should be stripped")
	}
	md := s.Fields["metadata"].GetStructValue()
	for _, f := range []string{"managedFields", "resourceVersion", "uid", "generation", "creationTimestamp"} {
		if _, ok := md.Fields[f]; ok {
			t.Errorf("metadata.%s should be stripped", f)
		}
	}
	if md.Fields["name"].GetStringValue() != "keep" {
		t.Error("metadata.name should be preserved")
	}

	// nil safety.
	stripObservedReadOnlyFields(nil)
}

// ---------------------------------------------------------------------------
// makeObservedDict builds a frozen *convert.StarlarkDict containing the named
// resources. Each entry maps name -> *convert.StarlarkDict with the given
// key/value pairs. This mirrors how the runtime presents observed composed
// resources.
// ---------------------------------------------------------------------------

func makeObservedDict(t *testing.T, entries map[string]map[string]string) *convert.StarlarkDict {
	t.Helper()
	obs := convert.NewStarlarkDict(len(entries))
	for name, fields := range entries {
		inner := convert.NewStarlarkDict(len(fields))
		for k, v := range fields {
			if err := inner.SetKey(starlark.String(k), starlark.String(v)); err != nil {
				t.Fatalf("inner.SetKey(%q): %v", k, err)
			}
		}
		inner.Freeze()
		if err := obs.SetKey(starlark.String(name), inner); err != nil {
			t.Fatalf("obs.SetKey(%q): %v", name, err)
		}
	}
	obs.Freeze()
	return obs
}

// ---------------------------------------------------------------------------
// GATE-03: Resource(when=When(False, ..., keep_if_exists=True)) with resource
// in observed state -> emits observed body verbatim.
// ---------------------------------------------------------------------------

func TestCollector_WhenFalse_KeepIfExists_Found(t *testing.T) {
	observed := makeObservedDict(t, map[string]map[string]string{
		"db": {"apiVersion": "v1", "kind": "Database"},
	})
	cc := NewConditionCollector()
	c := NewCollector(cc, "gate03.star", nil, observed)
	thread := new(starlark.Thread)

	val, err := starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String("db"),
		starlark.None,
	}, []starlark.Tuple{
		{starlark.String("when"), whenVal(false, "config unavailable", true)},
	})
	if err != nil {
		t.Fatalf("Resource() error: %v", err)
	}

	// Return value must be ResourceRef.
	ref, ok := val.(*ResourceRef)
	if !ok {
		t.Fatalf("Resource() = %v (%s), want ResourceRef", val, val.Type())
	}
	if ref.name != "db" {
		t.Errorf("ResourceRef.name = %q, want %q", ref.name, "db")
	}

	// Resource must appear in collected resources with observed body.
	res := c.Resources()
	cr, ok := res["db"]
	if !ok {
		t.Fatal("preserved resource should appear in Resources()")
	}
	if cr.Body == nil {
		t.Fatal("preserved resource body is nil")
	}
	if got := cr.Body.GetFields()["apiVersion"].GetStringValue(); got != "v1" {
		t.Errorf("body.apiVersion = %q, want %q", got, "v1")
	}
	if got := cr.Body.GetFields()["kind"].GetStringValue(); got != "Database" {
		t.Errorf("body.kind = %q, want %q", got, "Database")
	}

	// Ready must be ReadyUnspecified.
	if cr.Ready != resource.ReadyUnspecified {
		t.Errorf("Ready = %v, want ReadyUnspecified", cr.Ready)
	}

	// Warning event must use exact preserve message.
	events := cc.Events()
	if len(events) != 1 {
		t.Fatalf("Events() len = %d, want 1", len(events))
	}
	wantMsg := `Preserving resource "db": observed body emitted, gated by when=False`
	if events[0].Message != wantMsg {
		t.Errorf("event message = %q, want %q", events[0].Message, wantMsg)
	}
	if events[0].Severity != "Warning" {
		t.Errorf("event severity = %q, want %q", events[0].Severity, "Warning")
	}
}

// ---------------------------------------------------------------------------
// GATE-04: Resource(when=When(False, ..., keep_if_exists=True)) with resource
// NOT in observed state -> skip with Warning.
// ---------------------------------------------------------------------------

func TestCollector_WhenFalse_KeepIfExists_NotFound(t *testing.T) {
	// Observed dict exists but does NOT contain "db".
	observed := makeObservedDict(t, map[string]map[string]string{
		"other": {"kind": "Bucket"},
	})
	cc := NewConditionCollector()
	c := NewCollector(cc, "gate04.star", nil, observed)
	thread := new(starlark.Thread)

	val, err := starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String("db"),
		starlark.None,
	}, []starlark.Tuple{
		{starlark.String("when"), whenVal(false, "config unavailable", true)},
	})
	if err != nil {
		t.Fatalf("Resource() error: %v", err)
	}

	// Return value is a *SkippedRef carrying the resource name (falsy in Starlark).
	if sr, ok := val.(*SkippedRef); !ok || sr.name != "db" {
		t.Errorf("Resource() = %v (%s), want *SkippedRef{name:\"db\"}", val, val.Type())
	}

	// Resource must NOT appear in collected resources.
	res := c.Resources()
	if _, ok := res["db"]; ok {
		t.Error("not-found preserve resource should not appear in Resources()")
	}

	// Warning event must use the When reason.
	events := cc.Events()
	if len(events) != 1 {
		t.Fatalf("Events() len = %d, want 1", len(events))
	}
	wantMsg := `Skipping resource "db": config unavailable`
	if events[0].Message != wantMsg {
		t.Errorf("event message = %q, want %q", events[0].Message, wantMsg)
	}
}

// GATE-04 (nil observed): When(False, ..., keep_if_exists=True) with
// c.observed == nil -> same as not-found.
func TestCollector_WhenFalse_KeepIfExists_NilObserved(t *testing.T) {
	cc := NewConditionCollector()
	c := NewCollector(cc, "gate04-nil.star", nil, nil) // nil observed
	thread := new(starlark.Thread)

	val, err := starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String("db"),
		starlark.None,
	}, []starlark.Tuple{
		{starlark.String("when"), whenVal(false, "config unavailable", true)},
	})
	if err != nil {
		t.Fatalf("Resource() error: %v", err)
	}

	if sr, ok := val.(*SkippedRef); !ok || sr.name != "db" {
		t.Errorf("Resource() = %v (%s), want *SkippedRef{name:\"db\"}", val, val.Type())
	}

	res := c.Resources()
	if _, ok := res["db"]; ok {
		t.Error("nil-observed preserve resource should not appear in Resources()")
	}

	events := cc.Events()
	if len(events) != 1 {
		t.Fatalf("Events() len = %d, want 1", len(events))
	}
	wantMsg := `Skipping resource "db": config unavailable`
	if events[0].Message != wantMsg {
		t.Errorf("event message = %q, want %q", events[0].Message, wantMsg)
	}
}

// ---------------------------------------------------------------------------
// When(True, ..., keep_if_exists=True) with body dict -> normal path
// (keep_if_exists is dormant when condition=True).
// ---------------------------------------------------------------------------

func TestCollector_WhenTrue_KeepIfExists_BodyProvided(t *testing.T) {
	observed := makeObservedDict(t, map[string]map[string]string{
		"db": {"apiVersion": "observed-v1"},
	})
	cc := NewConditionCollector()
	c := NewCollector(cc, "gate06.star", nil, observed)
	thread := new(starlark.Thread)

	body := new(starlark.Dict)
	_ = body.SetKey(starlark.String("apiVersion"), starlark.String("v1"))
	_ = body.SetKey(starlark.String("kind"), starlark.String("Bucket"))

	val, err := starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String("db"),
		body,
	}, []starlark.Tuple{
		{starlark.String("when"), whenVal(true, "dormant test", true)},
	})
	if err != nil {
		t.Fatalf("Resource() error: %v", err)
	}

	// Return value must be ResourceRef (normal path).
	ref, ok := val.(*ResourceRef)
	if !ok {
		t.Fatalf("Resource() = %v (%s), want ResourceRef", val, val.Type())
	}
	if ref.name != "db" {
		t.Errorf("ResourceRef.name = %q, want %q", ref.name, "db")
	}

	// Resource must have the dict body (NOT observed body).
	res := c.Resources()
	cr, ok := res["db"]
	if !ok {
		t.Fatal("dormant preserve resource should appear in Resources()")
	}
	if got := cr.Body.GetFields()["apiVersion"].GetStringValue(); got != "v1" {
		t.Errorf("body.apiVersion = %q, want %q (should be dict body, not observed)", got, "v1")
	}

	// ResourceNameLabel must be injected (normal path).
	md := cr.Body.GetFields()["metadata"]
	if md == nil {
		t.Fatal("metadata should be present on normal path")
	}
	labels := md.GetStructValue().GetFields()["labels"]
	if labels == nil {
		t.Fatal("metadata.labels should be present on normal path")
	}
	if got := labels.GetStructValue().GetFields()[ResourceNameLabel].GetStringValue(); got != "db" {
		t.Errorf("ResourceNameLabel = %q, want %q", got, "db")
	}

	// No preservation Warning events expected (dormant -- condition is true).
	events := cc.Events()
	if len(events) != 0 {
		t.Errorf("Events() len = %d, want 0 (dormant preserve should emit no warnings)", len(events))
	}
}

// ---------------------------------------------------------------------------
// GATE-08: Preserved body has NO ResourceNameLabel, NO crossplane traceability
// labels injected, NO external_name annotation added.
// ---------------------------------------------------------------------------

func TestCollector_WhenFalse_KeepIfExists_NoLabelInjection(t *testing.T) {
	observed := makeObservedDict(t, map[string]map[string]string{
		"db": {"apiVersion": "v1", "kind": "Database"},
	})
	cc := NewConditionCollector()
	// Provide OXR with crossplane labels to verify they are NOT injected.
	oxr := &structpb.Struct{Fields: map[string]*structpb.Value{
		"metadata": structpb.NewStructValue(&structpb.Struct{Fields: map[string]*structpb.Value{
			"name": structpb.NewStringValue("my-composite"),
		}}),
	}}
	c := NewCollector(cc, "gate08.star", oxr, observed)
	thread := new(starlark.Thread)

	val, err := starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String("db"),
		starlark.None,
	}, []starlark.Tuple{
		{starlark.String("when"), whenVal(false, "dormant", true)},
	})
	if err != nil {
		t.Fatalf("Resource() error: %v", err)
	}

	ref, ok := val.(*ResourceRef)
	if !ok {
		t.Fatalf("Resource() = %v (%s), want ResourceRef", val, val.Type())
	}
	if ref.name != "db" {
		t.Errorf("ResourceRef.name = %q, want %q", ref.name, "db")
	}

	res := c.Resources()
	cr := res["db"]

	// Preserved body should only have the original fields from observed.
	// No metadata.labels injection.
	if md := cr.Body.GetFields()["metadata"]; md != nil {
		if mdStruct := md.GetStructValue(); mdStruct != nil {
			if lbls := mdStruct.GetFields()["labels"]; lbls != nil {
				lblStruct := lbls.GetStructValue()
				if lblStruct != nil {
					// Check NO ResourceNameLabel.
					if _, ok := lblStruct.GetFields()[ResourceNameLabel]; ok {
						t.Errorf("preserved body should NOT have %s label", ResourceNameLabel)
					}
					// Check NO crossplane.io/composite label.
					if _, ok := lblStruct.GetFields()[labelComposite]; ok {
						t.Errorf("preserved body should NOT have %s label", labelComposite)
					}
				}
			}
		}
	}

	// Preserved body should have exactly the observed fields and nothing else.
	bodyFields := cr.Body.GetFields()
	if _, ok := bodyFields["apiVersion"]; !ok {
		t.Error("preserved body should have apiVersion from observed")
	}
	if _, ok := bodyFields["kind"]; !ok {
		t.Error("preserved body should have kind from observed")
	}
	// Metadata should NOT exist (it wasn't in observed).
	if _, ok := bodyFields["metadata"]; ok {
		t.Error("preserved body should NOT have metadata (was not in observed)")
	}
}

// ---------------------------------------------------------------------------
// GATE-09: Observed body is *convert.StarlarkDict, converted via
// convert.StarlarkToStruct internally.
// ---------------------------------------------------------------------------

func TestCollector_WhenFalse_KeepIfExists_StarlarkDictConversion(t *testing.T) {
	// Build observed with nested dict to verify StarlarkToStruct handles it.
	obs := convert.NewStarlarkDict(1)
	inner := convert.NewStarlarkDict(2)
	_ = inner.SetKey(starlark.String("apiVersion"), starlark.String("v1"))

	// Add nested spec dict to exercise StarlarkToStruct recursion.
	spec := convert.NewStarlarkDict(1)
	_ = spec.SetKey(starlark.String("region"), starlark.String("us-east-1"))
	spec.Freeze()
	_ = inner.SetKey(starlark.String("spec"), spec)
	inner.Freeze()
	_ = obs.SetKey(starlark.String("db"), inner)
	obs.Freeze()

	cc := NewConditionCollector()
	c := NewCollector(cc, "gate09.star", nil, obs)
	thread := new(starlark.Thread)

	val, err := starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String("db"),
		starlark.None,
	}, []starlark.Tuple{
		{starlark.String("when"), whenVal(false, "dormant", true)},
	})
	if err != nil {
		t.Fatalf("Resource() error: %v", err)
	}

	ref, ok := val.(*ResourceRef)
	if !ok {
		t.Fatalf("Resource() = %v (%s), want ResourceRef", val, val.Type())
	}
	if ref.name != "db" {
		t.Errorf("ResourceRef.name = %q, want %q", ref.name, "db")
	}

	// Verify nested struct conversion.
	res := c.Resources()
	cr := res["db"]
	specField := cr.Body.GetFields()["spec"]
	if specField == nil {
		t.Fatal("preserved body should have spec field from observed")
	}
	specStruct := specField.GetStructValue()
	if specStruct == nil {
		t.Fatal("spec should be a struct")
	}
	if got := specStruct.GetFields()["region"].GetStringValue(); got != "us-east-1" {
		t.Errorf("spec.region = %q, want %q", got, "us-east-1")
	}
}

// ---------------------------------------------------------------------------
// Cliff guard: When(False, ..., keep_if_exists=True) with resource in
// observed state -> emit observed body.
// ---------------------------------------------------------------------------

func TestCollector_WhenFalse_KeepIfExists_CliffGuard_Found(t *testing.T) {
	observed := makeObservedDict(t, map[string]map[string]string{
		"db": {"apiVersion": "v1", "kind": "Database"},
	})
	cc := NewConditionCollector()
	c := NewCollector(cc, "cliff.star", nil, observed)
	thread := new(starlark.Thread)

	body := new(starlark.Dict)
	_ = body.SetKey(starlark.String("kind"), starlark.String("Bucket"))

	val, err := starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String("db"),
		body,
	}, []starlark.Tuple{
		{starlark.String("when"), whenVal(false, "optional", true)},
	})
	if err != nil {
		t.Fatalf("Resource() error: %v", err)
	}

	// Return value must be ResourceRef.
	ref, ok := val.(*ResourceRef)
	if !ok {
		t.Fatalf("Resource() = %v (%s), want ResourceRef", val, val.Type())
	}
	if ref.name != "db" {
		t.Errorf("ResourceRef.name = %q, want %q", ref.name, "db")
	}

	// Resource must appear with observed body (NOT the dict body arg).
	res := c.Resources()
	cr, ok := res["db"]
	if !ok {
		t.Fatal("cliff guard resource should appear in Resources()")
	}
	if got := cr.Body.GetFields()["apiVersion"].GetStringValue(); got != "v1" {
		t.Errorf("body.apiVersion = %q, want %q (should be observed body)", got, "v1")
	}
	if got := cr.Body.GetFields()["kind"].GetStringValue(); got != "Database" {
		t.Errorf("body.kind = %q, want %q (should be observed body)", got, "Database")
	}

	// Ready must be ReadyUnspecified.
	if cr.Ready != resource.ReadyUnspecified {
		t.Errorf("Ready = %v, want ReadyUnspecified", cr.Ready)
	}

	// Warning event for cliff guard.
	events := cc.Events()
	if len(events) != 1 {
		t.Fatalf("Events() len = %d, want 1", len(events))
	}
	wantMsg := `Preserving resource "db": observed body emitted, gated by when=False`
	if events[0].Message != wantMsg {
		t.Errorf("event message = %q, want %q", events[0].Message, wantMsg)
	}
}

// Cliff guard: When(False, ..., keep_if_exists=True) with resource NOT
// in observed state -> skip with reason from When.
func TestCollector_WhenFalse_KeepIfExists_CliffGuard_NotFound(t *testing.T) {
	observed := makeObservedDict(t, map[string]map[string]string{
		"other": {"kind": "Bucket"},
	})
	cc := NewConditionCollector()
	c := NewCollector(cc, "cliff-skip.star", nil, observed)
	thread := new(starlark.Thread)

	body := new(starlark.Dict)
	_ = body.SetKey(starlark.String("kind"), starlark.String("Bucket"))

	val, err := starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String("db"),
		body,
	}, []starlark.Tuple{
		{starlark.String("when"), whenVal(false, "feature gated", true)},
	})
	if err != nil {
		t.Fatalf("Resource() error: %v", err)
	}

	// Return value is a *SkippedRef (falsy in Starlark).
	if sr, ok := val.(*SkippedRef); !ok || sr.name != "db" {
		t.Errorf("Resource() = %v (%s), want *SkippedRef{name:\"db\"}", val, val.Type())
	}

	// Resource must NOT appear.
	res := c.Resources()
	if _, ok := res["db"]; ok {
		t.Error("not-found cliff guard resource should not appear in Resources()")
	}

	// Warning event uses the When reason.
	events := cc.Events()
	if len(events) != 1 {
		t.Fatalf("Events() len = %d, want 1", len(events))
	}
	wantMsg := `Skipping resource "db": feature gated`
	if events[0].Message != wantMsg {
		t.Errorf("event message = %q, want %q", events[0].Message, wantMsg)
	}
}

// Cliff guard with nil observed: When(False, ..., keep_if_exists=True) uses
// the When reason when not found.
func TestCollector_WhenFalse_KeepIfExists_CliffGuard_NilObserved(t *testing.T) {
	cc := NewConditionCollector()
	c := NewCollector(cc, "cliff-reason.star", nil, nil) // nil observed
	thread := new(starlark.Thread)

	body := new(starlark.Dict)
	_ = body.SetKey(starlark.String("kind"), starlark.String("Bucket"))

	val, err := starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String("db"),
		body,
	}, []starlark.Tuple{
		{starlark.String("when"), whenVal(false, "feature disabled", true)},
	})
	if err != nil {
		t.Fatalf("Resource() error: %v", err)
	}

	if sr, ok := val.(*SkippedRef); !ok || sr.name != "db" {
		t.Errorf("Resource() = %v (%s), want *SkippedRef{name:\"db\"}", val, val.Type())
	}

	events := cc.Events()
	if len(events) != 1 {
		t.Fatalf("Events() len = %d, want 1", len(events))
	}
	// When reason should be used.
	wantMsg := `Skipping resource "db": feature disabled`
	if events[0].Message != wantMsg {
		t.Errorf("event message = %q, want %q", events[0].Message, wantMsg)
	}
}

// Preservation is NOT a skip — c.skipped should NOT be set.
func TestCollector_KeepIfExists_NotInSkipped(t *testing.T) {
	observed := makeObservedDict(t, map[string]map[string]string{
		"db": {"apiVersion": "v1"},
	})
	cc := NewConditionCollector()
	c := NewCollector(cc, "preserve-not-skip.star", nil, observed)
	thread := new(starlark.Thread)

	// Preserve via cliff guard.
	_, err := starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String("db"),
		starlark.None,
	}, []starlark.Tuple{
		{starlark.String("when"), whenVal(false, "dormant", true)},
	})
	if err != nil {
		t.Fatalf("Resource() error: %v", err)
	}

	// Verify db is NOT in skipped (so a subsequent Resource() call can still succeed).
	c.mu.Lock()
	isSkipped := c.skipped["db"]
	c.mu.Unlock()
	if isSkipped {
		t.Error("preserved resource should NOT be marked as skipped")
	}

	// Verify resource IS in resources.
	res := c.Resources()
	if _, ok := res["db"]; !ok {
		t.Error("preserved resource should appear in Resources()")
	}
}

// Preservation skip metric: preserved resources should NOT increment
// ResourcesSkippedTotal metric.
func TestCollector_KeepIfExists_NoSkipMetric(t *testing.T) {
	observed := makeObservedDict(t, map[string]map[string]string{
		"db": {"apiVersion": "v1"},
	})
	cc := NewConditionCollector()
	label := "preserve-metric.star"
	c := NewCollector(cc, label, nil, observed)
	thread := new(starlark.Thread)

	base := testutil.ToFloat64(metrics.ResourcesSkippedTotal.WithLabelValues(label))

	_, err := starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String("db"),
		starlark.None,
	}, []starlark.Tuple{
		{starlark.String("when"), whenVal(false, "dormant", true)},
	})
	if err != nil {
		t.Fatalf("Resource() error: %v", err)
	}

	delta := testutil.ToFloat64(metrics.ResourcesSkippedTotal.WithLabelValues(label)) - base
	if delta != 0 {
		t.Errorf("metric delta = %v, want 0 (preservation is not a skip)", delta)
	}
}

func TestCollector_BodyAutoCompact_TopLevel(t *testing.T) {
	c := NewCollector(NewConditionCollector(), "test.star", nil, nil)
	thread := new(starlark.Thread)

	body := new(starlark.Dict)
	_ = body.SetKey(starlark.String("apiVersion"), starlark.String("v1"))
	_ = body.SetKey(starlark.String("optional"), starlark.None)

	_, err := starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String("compact-top"),
		body,
	}, nil)
	if err != nil {
		t.Fatalf("Resource() error: %v", err)
	}

	cr := c.Resources()["compact-top"]
	fields := cr.Body.GetFields()
	if fields["apiVersion"].GetStringValue() != "v1" {
		t.Errorf("apiVersion = %q, want 'v1'", fields["apiVersion"].GetStringValue())
	}
	if _, ok := fields["optional"]; ok {
		t.Error("expected 'optional' (None) to be stripped from body, but it is present")
	}
}

func TestCollector_BodyAutoCompact_Nested(t *testing.T) {
	c := NewCollector(NewConditionCollector(), "test.star", nil, nil)
	thread := new(starlark.Thread)

	inner := new(starlark.Dict)
	_ = inner.SetKey(starlark.String("field"), starlark.String("val"))
	_ = inner.SetKey(starlark.String("removed"), starlark.None)

	body := new(starlark.Dict)
	_ = body.SetKey(starlark.String("spec"), inner)

	_, err := starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String("compact-nested"),
		body,
	}, nil)
	if err != nil {
		t.Fatalf("Resource() error: %v", err)
	}

	cr := c.Resources()["compact-nested"]
	spec := cr.Body.GetFields()["spec"].GetStructValue()
	if spec == nil {
		t.Fatal("spec is nil")
	}
	if spec.GetFields()["field"].GetStringValue() != "val" {
		t.Errorf("spec.field = %q, want 'val'", spec.GetFields()["field"].GetStringValue())
	}
	if _, ok := spec.GetFields()["removed"]; ok {
		t.Error("expected 'removed' (None) to be stripped from nested dict, but it is present")
	}
}

func TestCollector_BodyAutoCompact_ListWithNestedDict(t *testing.T) {
	c := NewCollector(NewConditionCollector(), "test.star", nil, nil)
	thread := new(starlark.Thread)

	elem := new(starlark.Dict)
	_ = elem.SetKey(starlark.String("a"), starlark.String("b"))
	_ = elem.SetKey(starlark.String("c"), starlark.None)

	items := starlark.NewList([]starlark.Value{elem})

	body := new(starlark.Dict)
	_ = body.SetKey(starlark.String("items"), items)

	_, err := starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String("compact-list"),
		body,
	}, nil)
	if err != nil {
		t.Fatalf("Resource() error: %v", err)
	}

	cr := c.Resources()["compact-list"]
	itemsList := cr.Body.GetFields()["items"].GetListValue()
	if itemsList == nil {
		t.Fatal("items is nil")
	}
	if len(itemsList.GetValues()) != 1 {
		t.Fatalf("items length = %d, want 1", len(itemsList.GetValues()))
	}
	elemStruct := itemsList.GetValues()[0].GetStructValue()
	if elemStruct == nil {
		t.Fatal("items[0] is nil")
	}
	if elemStruct.GetFields()["a"].GetStringValue() != "b" {
		t.Errorf("items[0].a = %q, want 'b'", elemStruct.GetFields()["a"].GetStringValue())
	}
	if _, ok := elemStruct.GetFields()["c"]; ok {
		t.Error("expected 'c' (None) to be stripped from list element dict, but it is present")
	}
}

func TestCollector_BodyAutoCompact_NoNone(t *testing.T) {
	c := NewCollector(NewConditionCollector(), "test.star", nil, nil)
	thread := new(starlark.Thread)

	body := new(starlark.Dict)
	_ = body.SetKey(starlark.String("apiVersion"), starlark.String("v1"))
	_ = body.SetKey(starlark.String("kind"), starlark.String("Service"))
	_ = body.SetKey(starlark.String("port"), starlark.MakeInt(8080))

	_, err := starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String("compact-noop"),
		body,
	}, nil)
	if err != nil {
		t.Fatalf("Resource() error: %v", err)
	}

	cr := c.Resources()["compact-noop"]
	fields := cr.Body.GetFields()
	if fields["apiVersion"].GetStringValue() != "v1" {
		t.Errorf("apiVersion = %q, want 'v1'", fields["apiVersion"].GetStringValue())
	}
	if fields["kind"].GetStringValue() != "Service" {
		t.Errorf("kind = %q, want 'Service'", fields["kind"].GetStringValue())
	}
	if fields["port"].GetNumberValue() != 8080 {
		t.Errorf("port = %v, want 8080", fields["port"].GetNumberValue())
	}
}

func TestCollector_BodyAutoCompact_SchemaDict(t *testing.T) {
	c := NewCollector(NewConditionCollector(), "test.star", nil, nil)
	thread := new(starlark.Thread)

	inner := new(starlark.Dict)
	_ = inner.SetKey(starlark.String("apiVersion"), starlark.String("v1"))
	_ = inner.SetKey(starlark.String("optional"), starlark.None)

	sd := schema.NewSchemaDict(nil, inner)

	_, err := starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String("compact-schema"),
		sd,
	}, nil)
	if err != nil {
		t.Fatalf("Resource() error: %v", err)
	}

	cr := c.Resources()["compact-schema"]
	fields := cr.Body.GetFields()
	if fields["apiVersion"].GetStringValue() != "v1" {
		t.Errorf("apiVersion = %q, want 'v1'", fields["apiVersion"].GetStringValue())
	}
	if _, ok := fields["optional"]; ok {
		t.Error("expected 'optional' (None) to be stripped from SchemaDict body, but it is present")
	}
}

// ---------------------------------------------------------------------------
// Composite readiness gating: When(optional=) and set_composite_ready()
// ---------------------------------------------------------------------------

// callResourceWhenFalse is a helper to invoke Resource(name, body=None, when=When(False, reason, keepIfExists), <extra kwargs>).
// Returns any error from the call.
func callResourceWhenFalse(c *Collector, name string, w *WhenValue, extra []starlark.Tuple) error {
	thread := new(starlark.Thread)
	kwargs := []starlark.Tuple{
		{starlark.String("when"), w},
	}
	kwargs = append(kwargs, extra...)
	_, err := starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String(name),
		starlark.None,
	}, kwargs)
	return err
}

func TestCollector_Gating_WhenFalseDefaultGates(t *testing.T) {
	c := NewCollector(NewConditionCollector(), "test.star", nil, nil)

	if err := callResourceWhenFalse(c, "r1", whenVal(false, "not yet", false), nil); err != nil {
		t.Fatalf("Resource() error: %v", err)
	}

	skips := c.GatingSkips()
	if len(skips) != 1 {
		t.Fatalf("GatingSkips() len = %d, want 1", len(skips))
	}
	if skips[0].Name != "r1" || skips[0].Reason != "not yet" {
		t.Errorf("GatingSkips()[0] = %+v, want {Name:r1 Reason:\"not yet\"}", skips[0])
	}
}

func TestCollector_Gating_OptionalTrueDoesNotGate(t *testing.T) {
	c := NewCollector(NewConditionCollector(), "test.star", nil, nil)

	err := callResourceWhenFalse(c, "backup", whenValOptional(false, "backups disabled", false), nil)
	if err != nil {
		t.Fatalf("Resource() error: %v", err)
	}

	if got := c.GatingSkips(); len(got) != 0 {
		t.Errorf("GatingSkips() = %+v, want empty (optional=True)", got)
	}
	if _, skipped := c.skipped["backup"]; !skipped {
		t.Error("resource should still be recorded as skipped")
	}
}

func TestCollector_Gating_OptionalFalseExplicitGates(t *testing.T) {
	c := NewCollector(NewConditionCollector(), "test.star", nil, nil)

	err := callResourceWhenFalse(c, "r1", whenVal(false, "explicit", false), nil)
	if err != nil {
		t.Fatalf("Resource() error: %v", err)
	}
	if got := c.GatingSkips(); len(got) != 1 {
		t.Errorf("GatingSkips() len = %d, want 1", len(got))
	}
}

func TestCollector_Gating_Dedup(t *testing.T) {
	c := NewCollector(NewConditionCollector(), "test.star", nil, nil)

	if err := callResourceWhenFalse(c, "r1", whenVal(false, "first", false), nil); err != nil {
		t.Fatalf("first call error: %v", err)
	}
	// Second call for the same name should be a no-op (skipped map dedups).
	if err := callResourceWhenFalse(c, "r1", whenVal(false, "second", false), nil); err != nil {
		t.Fatalf("second call error: %v", err)
	}

	skips := c.GatingSkips()
	if len(skips) != 1 {
		t.Errorf("GatingSkips() len = %d, want 1 (deduped)", len(skips))
	}
}

func TestCollector_Gating_PreserveObservedFoundDoesNotGate(t *testing.T) {
	// Build an observed dict containing "r1" so the cliff-guard finds it.
	observed := convert.NewStarlarkDict(1)
	obsBody := convert.NewStarlarkDict(0)
	_ = obsBody.SetField("apiVersion", starlark.String("v1"))
	_ = obsBody.SetField("kind", starlark.String("Dummy"))
	_ = observed.SetKey(starlark.String("r1"), obsBody)
	observed.Freeze()

	c := NewCollector(NewConditionCollector(), "test.star", nil, observed)

	err := callResourceWhenFalse(c, "r1", whenVal(false, "preserved", true), nil)
	if err != nil {
		t.Fatalf("Resource() error: %v", err)
	}

	// Observed body was emitted; no gating should be recorded.
	if got := c.GatingSkips(); len(got) != 0 {
		t.Errorf("GatingSkips() = %+v, want empty (preserve found)", got)
	}
	if _, emitted := c.Resources()["r1"]; !emitted {
		t.Error("expected r1 to be emitted from observed body")
	}
}

func TestCollector_Gating_PreserveObservedMissGates(t *testing.T) {
	// Empty observed dict: preserve miss should skip + gate (default optional=False).
	observed := convert.NewStarlarkDict(0)
	observed.Freeze()
	c := NewCollector(NewConditionCollector(), "test.star", nil, observed)

	err := callResourceWhenFalse(c, "r1", whenVal(false, "preserved-miss", true), nil)
	if err != nil {
		t.Fatalf("Resource() error: %v", err)
	}
	if got := c.GatingSkips(); len(got) != 1 {
		t.Errorf("GatingSkips() len = %d, want 1 (preserve miss should gate)", len(got))
	}
}

func TestCollector_Gating_PreserveObservedMissOptionalDoesNotGate(t *testing.T) {
	observed := convert.NewStarlarkDict(0)
	observed.Freeze()
	c := NewCollector(NewConditionCollector(), "test.star", nil, observed)

	err := callResourceWhenFalse(c, "r1", whenValOptional(false, "preserved-miss", true), nil)
	if err != nil {
		t.Fatalf("Resource() error: %v", err)
	}
	if got := c.GatingSkips(); len(got) != 0 {
		t.Errorf("GatingSkips() = %+v, want empty (optional=True)", got)
	}
}

func TestCollector_Gating_SkipResourceNeverGates(t *testing.T) {
	// skip_resource is a pure observability primitive -- it emits a Warning
	// event and records a skip but never gates composite readiness. Gating
	// lives on Resource(when=False) where emission is actually decided.
	c := NewCollector(NewConditionCollector(), "test.star", nil, nil)
	thread := new(starlark.Thread)

	_, err := starlark.Call(thread, c.SkipResourceBuiltin(), starlark.Tuple{
		starlark.String("r1"),
		starlark.String("just no"),
	}, nil)
	if err != nil {
		t.Fatalf("skip_resource() error: %v", err)
	}

	if got := c.GatingSkips(); len(got) != 0 {
		t.Errorf("GatingSkips() = %+v, want empty (skip_resource never gates)", got)
	}
}

func TestCollector_SetCompositeReady_False(t *testing.T) {
	c := NewCollector(NewConditionCollector(), "test.star", nil, nil)
	thread := new(starlark.Thread)

	_, err := starlark.Call(thread, c.SetCompositeReadyBuiltin(), starlark.Tuple{
		starlark.False,
	}, []starlark.Tuple{
		{starlark.String("reason"), starlark.String("WaitingForCluster")},
		{starlark.String("message"), starlark.String("cluster is still provisioning")},
	})
	if err != nil {
		t.Fatalf("set_composite_ready() error: %v", err)
	}

	got := c.CompositeReadyOverride()
	if !got.Set {
		t.Fatal("Override.Set = false, want true")
	}
	if got.Ready {
		t.Error("Override.Ready = true, want false")
	}
	if got.Reason != "WaitingForCluster" {
		t.Errorf("Override.Reason = %q, want WaitingForCluster", got.Reason)
	}
	if got.Message != "cluster is still provisioning" {
		t.Errorf("Override.Message = %q, want cluster is still provisioning", got.Message)
	}
}

func TestCollector_SetCompositeReady_True(t *testing.T) {
	c := NewCollector(NewConditionCollector(), "test.star", nil, nil)
	thread := new(starlark.Thread)

	_, err := starlark.Call(thread, c.SetCompositeReadyBuiltin(), starlark.Tuple{
		starlark.True,
	}, nil)
	if err != nil {
		t.Fatalf("set_composite_ready() error: %v", err)
	}

	got := c.CompositeReadyOverride()
	if !got.Set || !got.Ready {
		t.Errorf("Override = %+v, want {Set:true Ready:true}", got)
	}
}

func TestCollector_SetCompositeReady_LastCallWins(t *testing.T) {
	c := NewCollector(NewConditionCollector(), "test.star", nil, nil)
	thread := new(starlark.Thread)

	_, _ = starlark.Call(thread, c.SetCompositeReadyBuiltin(), starlark.Tuple{
		starlark.True,
	}, nil)
	_, err := starlark.Call(thread, c.SetCompositeReadyBuiltin(), starlark.Tuple{
		starlark.False,
	}, []starlark.Tuple{
		{starlark.String("reason"), starlark.String("Flipped")},
	})
	if err != nil {
		t.Fatalf("set_composite_ready() error: %v", err)
	}

	got := c.CompositeReadyOverride()
	if got.Ready || got.Reason != "Flipped" {
		t.Errorf("Override = %+v, want last call (Ready=false, Reason=Flipped)", got)
	}
}

func TestCollector_SetCompositeReady_WrongType(t *testing.T) {
	c := NewCollector(NewConditionCollector(), "test.star", nil, nil)
	thread := new(starlark.Thread)

	_, err := starlark.Call(thread, c.SetCompositeReadyBuiltin(), starlark.Tuple{
		starlark.String("yes"),
	}, nil)
	if err == nil {
		t.Fatal("expected type error for non-bool ready arg, got nil")
	}
}

// ---------------------------------------------------------------------------
// SkippedRef + transitive skip + depends_on tolerance
// ---------------------------------------------------------------------------

// callResource is a small helper that invokes Resource(name, body, <kwargs>).
// Returns (val, err).
func callResource(c *Collector, name string, body starlark.Value, extra []starlark.Tuple) (starlark.Value, error) {
	thread := new(starlark.Thread)
	return starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String(name),
		body,
	}, extra)
}

func TestSkippedRef_Attributes(t *testing.T) {
	sr := &SkippedRef{name: "db"}

	if got := sr.Type(); got != "SkippedRef" {
		t.Errorf("Type() = %q, want SkippedRef", got)
	}
	if sr.Truth() != starlark.False {
		t.Error("SkippedRef.Truth() = True, want False (must be falsy)")
	}
	if sr.String() != "db" {
		t.Errorf("String() = %q, want db", sr.String())
	}
	nameAttr, err := sr.Attr("name")
	if err != nil || nameAttr != starlark.String("db") {
		t.Errorf(".name = %v, %v; want String(db)", nameAttr, err)
	}
}

func TestSkippedRef_ReturnedFromWhenFalse(t *testing.T) {
	c := NewCollector(NewConditionCollector(), "test.star", nil, nil)
	val, err := callResource(c, "db", starlark.None, []starlark.Tuple{
		{starlark.String("when"), whenVal(false, "waiting", false)},
	})
	if err != nil {
		t.Fatalf("Resource() error: %v", err)
	}
	sr, ok := val.(*SkippedRef)
	if !ok {
		t.Fatalf("Resource() = %T, want *SkippedRef", val)
	}
	if sr.name != "db" {
		t.Errorf("SkippedRef.name = %q, want db", sr.name)
	}
}

func TestSkippedRef_ReturnedFromWhenFalseOptional(t *testing.T) {
	c := NewCollector(NewConditionCollector(), "test.star", nil, nil)
	val, err := callResource(c, "backup", starlark.None, []starlark.Tuple{
		{starlark.String("when"), whenValOptional(false, "disabled", false)},
	})
	if err != nil {
		t.Fatalf("Resource() error: %v", err)
	}
	sr, ok := val.(*SkippedRef)
	if !ok {
		t.Fatalf("Resource() = %T, want *SkippedRef", val)
	}
	if sr.name != "backup" {
		t.Errorf("SkippedRef.name = %q, want backup", sr.name)
	}
	if got := c.GatingSkips(); len(got) != 0 {
		t.Errorf("GatingSkips() = %+v, want empty (When optional=True)", got)
	}
}

func TestSkippedRef_ReturnedFromBodyNone(t *testing.T) {
	c := NewCollector(NewConditionCollector(), "test.star", nil, nil)
	val, err := callResource(c, "db", starlark.None, nil)
	if err != nil {
		t.Fatalf("Resource() error: %v", err)
	}
	if _, ok := val.(*SkippedRef); !ok {
		t.Errorf("Resource() = %T, want *SkippedRef", val)
	}
}

func TestSkippedRef_ReturnedFromPreserveMiss(t *testing.T) {
	observed := convert.NewStarlarkDict(0)
	observed.Freeze()
	c := NewCollector(NewConditionCollector(), "test.star", nil, observed)
	val, err := callResource(c, "db", starlark.None, []starlark.Tuple{
		{starlark.String("when"), whenVal(false, "keep if exists miss", true)},
	})
	if err != nil {
		t.Fatalf("Resource() error: %v", err)
	}
	if _, ok := val.(*SkippedRef); !ok {
		t.Errorf("Resource() = %T, want *SkippedRef", val)
	}
}

func TestDependsOn_AcceptsNone(t *testing.T) {
	c := NewCollector(NewConditionCollector(), "test.star", nil, nil)
	body := new(starlark.Dict)
	_ = body.SetKey(starlark.String("kind"), starlark.String("X"))

	depList := starlark.NewList([]starlark.Value{starlark.None})
	_, err := callResource(c, "a", body, []starlark.Tuple{
		{starlark.String("depends_on"), depList},
	})
	if err != nil {
		t.Fatalf("Resource() error (None in depends_on should be tolerated): %v", err)
	}
	if got := c.Dependencies(); len(got) != 0 {
		t.Errorf("Dependencies() = %+v, want empty (None entry should not record a dep)", got)
	}
	if _, ok := c.Resources()["a"]; !ok {
		t.Error("expected 'a' to be emitted normally despite None in depends_on")
	}
}

func TestDependsOn_SkippedRefTriggersTransitiveSkip(t *testing.T) {
	c := NewCollector(NewConditionCollector(), "test.star", nil, nil)
	body := new(starlark.Dict)
	_ = body.SetKey(starlark.String("kind"), starlark.String("App"))

	skipped := &SkippedRef{name: "db"}
	depList := starlark.NewList([]starlark.Value{skipped})
	val, err := callResource(c, "app", body, []starlark.Tuple{
		{starlark.String("depends_on"), depList},
	})
	if err != nil {
		t.Fatalf("Resource() error: %v", err)
	}
	sr, ok := val.(*SkippedRef)
	if !ok {
		t.Fatalf("Resource() = %T, want *SkippedRef (transitive skip)", val)
	}
	if sr.name != "app" {
		t.Errorf("SkippedRef.name = %q, want app", sr.name)
	}
	if _, emitted := c.Resources()["app"]; emitted {
		t.Error("transitively skipped 'app' should not be in Resources()")
	}
	skips := c.GatingSkips()
	if len(skips) != 1 || skips[0].Name != "app" {
		t.Errorf("GatingSkips() = %+v, want one entry for 'app'", skips)
	}
	if !strings.Contains(skips[0].Reason, "depends on skipped") ||
		!strings.Contains(skips[0].Reason, `"db"`) {
		t.Errorf("skip reason = %q, want it to mention `depends on skipped \"db\"`", skips[0].Reason)
	}
}

func TestDependsOn_TransitiveSkipAlwaysGates(t *testing.T) {
	c := NewCollector(NewConditionCollector(), "test.star", nil, nil)
	body := new(starlark.Dict)
	_ = body.SetKey(starlark.String("kind"), starlark.String("App"))

	skipped := &SkippedRef{name: "db"}
	depList := starlark.NewList([]starlark.Value{skipped})
	val, err := callResource(c, "app", body, []starlark.Tuple{
		{starlark.String("depends_on"), depList},
	})
	if err != nil {
		t.Fatalf("Resource() error: %v", err)
	}
	if _, ok := val.(*SkippedRef); !ok {
		t.Fatalf("Resource() = %T, want *SkippedRef", val)
	}
	if got := c.GatingSkips(); len(got) != 1 {
		t.Errorf("GatingSkips() len = %d, want 1 (transitive skip always gates)", len(got))
	}
}

func TestDependsOn_MixedSkippedAndReal_SkippedWins(t *testing.T) {
	c := NewCollector(NewConditionCollector(), "test.star", nil, nil)
	body := new(starlark.Dict)
	_ = body.SetKey(starlark.String("kind"), starlark.String("App"))

	real := &ResourceRef{name: "cache"}
	skipped := &SkippedRef{name: "db"}
	depList := starlark.NewList([]starlark.Value{real, skipped})

	val, err := callResource(c, "app", body, []starlark.Tuple{
		{starlark.String("depends_on"), depList},
	})
	if err != nil {
		t.Fatalf("Resource() error: %v", err)
	}
	if _, ok := val.(*SkippedRef); !ok {
		t.Errorf("Resource() = %T, want *SkippedRef (any skip triggers transitive skip)", val)
	}
	if got := c.Dependencies(); len(got) != 0 {
		t.Errorf("Dependencies() = %+v, want empty (transitive skip aborts before recording any deps)", got)
	}
}

func TestDependsOn_TupleWithSkippedRefFirst_TransitiveSkip(t *testing.T) {
	c := NewCollector(NewConditionCollector(), "test.star", nil, nil)
	body := new(starlark.Dict)
	_ = body.SetKey(starlark.String("kind"), starlark.String("App"))

	skipped := &SkippedRef{name: "db"}
	tup := starlark.Tuple{skipped, starlark.String("status.atProvider.host")}
	depList := starlark.NewList([]starlark.Value{tup})

	val, err := callResource(c, "app", body, []starlark.Tuple{
		{starlark.String("depends_on"), depList},
	})
	if err != nil {
		t.Fatalf("Resource() error: %v", err)
	}
	if _, ok := val.(*SkippedRef); !ok {
		t.Errorf("Resource() = %T, want *SkippedRef (tuple with SkippedRef triggers transitive skip)", val)
	}
	if got := c.Dependencies(); len(got) != 0 {
		t.Errorf("Dependencies() = %+v, want empty (transitive skip aborts before recording deps)", got)
	}
}

func TestDependsOn_TupleWithNoneFirst_StillErrors(t *testing.T) {
	c := NewCollector(NewConditionCollector(), "test.star", nil, nil)
	body := new(starlark.Dict)
	_ = body.SetKey(starlark.String("kind"), starlark.String("App"))

	tup := starlark.Tuple{starlark.None, starlark.String("x.y")}
	depList := starlark.NewList([]starlark.Value{tup})

	_, err := callResource(c, "app", body, []starlark.Tuple{
		{starlark.String("depends_on"), depList},
	})
	if err == nil {
		t.Fatal("(None, \"path\") tuple should still error; bare None tolerance is intentional, tuples are too explicit to silently drop")
	}
}

// ---------------------------------------------------------------------------
// WhenValue type tests
// ---------------------------------------------------------------------------

func TestWhenValue_String(t *testing.T) {
	w := &WhenValue{condition: true, reason: "test", keepIfExists: false}
	want := `When(True, "test", keep_if_exists=False, optional=False)`
	if got := w.String(); got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}

	w2 := &WhenValue{condition: false, reason: "reason", keepIfExists: true, optional: true}
	want2 := `When(False, "reason", keep_if_exists=True, optional=True)`
	if got := w2.String(); got != want2 {
		t.Errorf("String() = %q, want %q", got, want2)
	}
}

func TestWhenValue_Attrs(t *testing.T) {
	w := &WhenValue{condition: true, reason: "test reason", keepIfExists: false}

	cond, err := w.Attr("condition")
	if err != nil {
		t.Fatalf("Attr(condition) error: %v", err)
	}
	if cond != starlark.True {
		t.Errorf("Attr(condition) = %v, want True", cond)
	}

	reason, err := w.Attr("reason")
	if err != nil {
		t.Fatalf("Attr(reason) error: %v", err)
	}
	if reason != starlark.String("test reason") {
		t.Errorf("Attr(reason) = %v, want %q", reason, "test reason")
	}

	keep, err := w.Attr("keep_if_exists")
	if err != nil {
		t.Fatalf("Attr(keep_if_exists) error: %v", err)
	}
	if keep != starlark.False {
		t.Errorf("Attr(keep_if_exists) = %v, want False", keep)
	}

	opt, err := w.Attr("optional")
	if err != nil {
		t.Fatalf("Attr(optional) error: %v", err)
	}
	if opt != starlark.False {
		t.Errorf("Attr(optional) = %v, want False", opt)
	}

	// Unknown attr returns nil.
	v, err := w.Attr("unknown")
	if err != nil {
		t.Fatalf("Attr(unknown) error: %v", err)
	}
	if v != nil {
		t.Errorf("Attr(unknown) = %v, want nil", v)
	}

	// AttrNames must be sorted.
	names := w.AttrNames()
	if len(names) != 4 || names[0] != "condition" || names[1] != "keep_if_exists" || names[2] != "optional" || names[3] != "reason" {
		t.Errorf("AttrNames() = %v, want [condition keep_if_exists optional reason]", names)
	}
}

func TestWhenValue_Truth(t *testing.T) {
	if got := (&WhenValue{condition: true}).Truth(); got != starlark.True {
		t.Errorf("Truth() for condition=true = %v, want True", got)
	}
	if got := (&WhenValue{condition: false}).Truth(); got != starlark.False {
		t.Errorf("Truth() for condition=false = %v, want False", got)
	}
}

func TestWhenValue_Hash(t *testing.T) {
	w := &WhenValue{condition: true, reason: "x", keepIfExists: false}
	_, err := w.Hash()
	if err == nil {
		t.Fatal("Hash() should return error (unhashable)")
	}
	if !strings.Contains(err.Error(), "unhashable type: When") {
		t.Errorf("Hash() error = %q, want to contain %q", err.Error(), "unhashable type: When")
	}
}

func TestWhenBuiltin_AllMandatory(t *testing.T) {
	fn := starlark.NewBuiltin("When", whenBuiltin)
	thread := new(starlark.Thread)

	// Missing all args.
	_, err := starlark.Call(thread, fn, nil, nil)
	if err == nil {
		t.Fatal("When() with no args should error")
	}

	// Missing keep_if_exists.
	_, err = starlark.Call(thread, fn, starlark.Tuple{starlark.False, starlark.String("reason")}, nil)
	if err == nil {
		t.Fatal("When(False, 'reason') with missing keep_if_exists should error")
	}
}

func TestWhenBuiltin_StrictBool_Condition(t *testing.T) {
	fn := starlark.NewBuiltin("When", whenBuiltin)
	thread := new(starlark.Thread)

	_, err := starlark.Call(thread, fn, starlark.Tuple{starlark.MakeInt(1), starlark.String("reason"), starlark.False}, nil)
	if err == nil {
		t.Fatal("When(1, ...) should error (condition must be bool)")
	}
	if !strings.Contains(err.Error(), "condition must be bool, got int") {
		t.Errorf("error = %q, want to contain %q", err.Error(), "condition must be bool, got int")
	}
}

func TestWhenBuiltin_StrictBool_KeepIfExists(t *testing.T) {
	fn := starlark.NewBuiltin("When", whenBuiltin)
	thread := new(starlark.Thread)

	_, err := starlark.Call(thread, fn, starlark.Tuple{starlark.False, starlark.String("reason"), starlark.MakeInt(1)}, nil)
	if err == nil {
		t.Fatal("When(False, 'reason', 1) should error (keep_if_exists must be bool)")
	}
	if !strings.Contains(err.Error(), "keep_if_exists must be bool, got int") {
		t.Errorf("error = %q, want to contain %q", err.Error(), "keep_if_exists must be bool, got int")
	}
}

func TestWhenBuiltin_StrictBool_Optional(t *testing.T) {
	fn := starlark.NewBuiltin("When", whenBuiltin)
	thread := new(starlark.Thread)

	_, err := starlark.Call(thread, fn, starlark.Tuple{starlark.False, starlark.String("reason"), starlark.False}, []starlark.Tuple{
		{starlark.String("optional"), starlark.MakeInt(1)},
	})
	if err == nil {
		t.Fatal("When(optional=1) should error (optional must be bool)")
	}
	if !strings.Contains(err.Error(), "optional must be bool, got int") {
		t.Errorf("error = %q, want to contain %q", err.Error(), "optional must be bool, got int")
	}
}

func TestWhenBuiltin_OptionalDefaultsFalse(t *testing.T) {
	fn := starlark.NewBuiltin("When", whenBuiltin)
	thread := new(starlark.Thread)

	val, err := starlark.Call(thread, fn, starlark.Tuple{starlark.False, starlark.String("reason"), starlark.False}, nil)
	if err != nil {
		t.Fatalf("When() error: %v", err)
	}
	w := val.(*WhenValue)
	if w.optional {
		t.Error("optional should default to false")
	}
}

func TestWhenBuiltin_EmptyReason(t *testing.T) {
	fn := starlark.NewBuiltin("When", whenBuiltin)
	thread := new(starlark.Thread)

	_, err := starlark.Call(thread, fn, starlark.Tuple{starlark.False, starlark.String(""), starlark.False}, nil)
	if err == nil {
		t.Fatal("When(False, '', False) should error (reason must not be empty)")
	}
	if !strings.Contains(err.Error(), "reason must not be empty") {
		t.Errorf("error = %q, want to contain %q", err.Error(), "reason must not be empty")
	}
}

// ---------------------------------------------------------------------------
// Phase 45 Plan 01 — MutableStruct as Resource body and labels
// ---------------------------------------------------------------------------

func TestCollectorMutableStructBody(t *testing.T) {
	cc := NewConditionCollector()
	c := NewCollector(cc, "test.star", nil, nil)
	thread := new(starlark.Thread)

	// Build a MutableStruct body with apiVersion, kind, metadata, spec.
	body, err := MakeMutableStruct(thread, &starlark.Builtin{}, nil, []starlark.Tuple{
		{starlark.String("apiVersion"), starlark.String("v1")},
		{starlark.String("kind"), starlark.String("Bucket")},
	})
	if err != nil {
		t.Fatalf("MakeMutableStruct: %v", err)
	}

	_, err = starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String("bucket"),
		body,
	}, nil)
	if err != nil {
		t.Fatalf("Resource() error: %v", err)
	}

	res := c.Resources()
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
	if cr.Body.GetFields()["kind"].GetStringValue() != "Bucket" {
		t.Errorf("kind = %q, want 'Bucket'", cr.Body.GetFields()["kind"].GetStringValue())
	}
}

func TestCollectorMutableStructLabels(t *testing.T) {
	cc := NewConditionCollector()
	oxr := makeOXR("xr-abc", "my-claim", "ns")
	c := NewCollector(cc, "test.star", oxr, nil)
	thread := new(starlark.Thread)

	body := new(starlark.Dict)
	_ = body.SetKey(starlark.String("apiVersion"), starlark.String("v1"))

	// Build a MutableStruct for labels.
	lblMS, err := MakeMutableStruct(thread, &starlark.Builtin{}, nil, []starlark.Tuple{
		{starlark.String("team"), starlark.String("platform")},
	})
	if err != nil {
		t.Fatalf("MakeMutableStruct: %v", err)
	}

	_, err = starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String("bucket"),
		body,
	}, []starlark.Tuple{
		{starlark.String("labels"), lblMS},
	})
	if err != nil {
		t.Fatalf("Resource() error: %v", err)
	}

	res := c.Resources()
	cr := res["bucket"]
	labels := cr.Body.GetFields()["metadata"].GetStructValue().GetFields()["labels"].GetStructValue()

	// User label should be present.
	if labels.GetFields()["team"].GetStringValue() != "platform" {
		t.Errorf("team = %q, want %q", labels.GetFields()["team"].GetStringValue(), "platform")
	}
	// Crossplane labels should also be present.
	if labels.GetFields()["crossplane.io/composite"].GetStringValue() != "xr-abc" {
		t.Errorf("composite = %q, want %q", labels.GetFields()["crossplane.io/composite"].GetStringValue(), "xr-abc")
	}
}
