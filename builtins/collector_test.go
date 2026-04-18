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

	c.recordSkip("my-resource", "not needed")

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

	c.recordSkip("x", "r1")
	c.recordSkip("x", "r2")

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

	c.recordSkip("a", "r1")
	c.recordSkip("b", "r2")

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
	cA.recordSkip("x", "some reason")

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
			c.recordSkip("shared", "reason")
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

// ---------------------------------------------------------------------------
// GATE-01: Resource(when=False, skip_reason="reason") skips resource
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
		{starlark.String("when"), starlark.False},
		{starlark.String("skip_reason"), starlark.String("not needed")},
	})
	if err != nil {
		t.Fatalf("Resource() error: %v", err)
	}

	// Return value must be None (not ResourceRef).
	if val != starlark.None {
		t.Errorf("Resource() = %v (%s), want None", val, val.Type())
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
		{starlark.String("when"), starlark.False},
		{starlark.String("skip_reason"), starlark.String("disabled")},
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
// GATE-02: Resource(when=False) without skip_reason -> error
// ---------------------------------------------------------------------------

func TestCollector_WhenFalse_NoSkipReason_Errors(t *testing.T) {
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
		t.Fatal("expected error for when=False without skip_reason")
	}
	if !strings.Contains(err.Error(), "skip_reason is required when when=False") {
		t.Errorf("error = %q, want to contain %q", err.Error(), "skip_reason is required when when=False")
	}
}

func TestCollector_SkipReason_WithoutWhenFalse_Allowed(t *testing.T) {
	cc := NewConditionCollector()
	c := NewCollector(cc, "test.star", nil, nil)
	thread := new(starlark.Thread)

	body := new(starlark.Dict)
	_ = body.SetKey(starlark.String("kind"), starlark.String("Bucket"))

	val, err := starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String("bucket"),
		body,
	}, []starlark.Tuple{
		{starlark.String("skip_reason"), starlark.String("some reason")},
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

func TestCollector_SkipReason_WithWhenTrue_Allowed(t *testing.T) {
	cc := NewConditionCollector()
	c := NewCollector(cc, "test.star", nil, nil)
	thread := new(starlark.Thread)

	body := new(starlark.Dict)
	_ = body.SetKey(starlark.String("kind"), starlark.String("Bucket"))

	val, err := starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String("bucket"),
		body,
	}, []starlark.Tuple{
		{starlark.String("when"), starlark.True},
		{starlark.String("skip_reason"), starlark.String("some reason")},
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
// GATE-05: Resource(body=None) without preserve_observed -> Warning + skip
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

	// Return value must be None.
	if val != starlark.None {
		t.Errorf("Resource() = %v (%s), want None", val, val.Type())
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
	wantMsg := `Skipping resource "db": body is None. If this resource exists, it will be removed from desired state. Set preserve_observed=True to re-emit the observed body when body is None.`
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

func TestCollector_WhenKwarg_StrictBool_Int(t *testing.T) {
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
		t.Fatal("expected error for when=1 (non-bool)")
	}
	if !strings.Contains(err.Error(), "when must be bool, got int") {
		t.Errorf("error = %q, want to contain %q", err.Error(), "when must be bool, got int")
	}
}

func TestCollector_WhenKwarg_StrictBool_String(t *testing.T) {
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
		t.Fatal("expected error for when=\"true\" (non-bool)")
	}
	if !strings.Contains(err.Error(), "when must be bool, got string") {
		t.Errorf("error = %q, want to contain %q", err.Error(), "when must be bool, got string")
	}
}

func TestCollector_PreserveObserved_StrictBool_Int(t *testing.T) {
	cc := NewConditionCollector()
	c := NewCollector(cc, "test.star", nil, nil)
	thread := new(starlark.Thread)

	body := new(starlark.Dict)
	_ = body.SetKey(starlark.String("kind"), starlark.String("Bucket"))

	_, err := starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String("bucket"),
		body,
	}, []starlark.Tuple{
		{starlark.String("preserve_observed"), starlark.MakeInt(1)},
	})
	if err == nil {
		t.Fatal("expected error for preserve_observed=1 (non-bool)")
	}
	if !strings.Contains(err.Error(), "preserve_observed must be bool, got int") {
		t.Errorf("error = %q, want to contain %q", err.Error(), "preserve_observed must be bool, got int")
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
		{starlark.String("when"), starlark.True},
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
// GATE-03: Resource(body=None, preserve_observed=True) with resource in
// observed state -> emits observed body verbatim.
// ---------------------------------------------------------------------------

func TestCollector_PreserveObserved_BodyNone_Found(t *testing.T) {
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
		{starlark.String("preserve_observed"), starlark.True},
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
	wantMsg := `Preserving resource "db": body=None, emitting observed body`
	if events[0].Message != wantMsg {
		t.Errorf("event message = %q, want %q", events[0].Message, wantMsg)
	}
	if events[0].Severity != "Warning" {
		t.Errorf("event severity = %q, want %q", events[0].Severity, "Warning")
	}
}

// ---------------------------------------------------------------------------
// GATE-04: Resource(body=None, preserve_observed=True) with resource NOT in
// observed state -> skip with Warning.
// ---------------------------------------------------------------------------

func TestCollector_PreserveObserved_BodyNone_NotFound(t *testing.T) {
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
		{starlark.String("preserve_observed"), starlark.True},
	})
	if err != nil {
		t.Fatalf("Resource() error: %v", err)
	}

	// Return value must be None (not ResourceRef).
	if val != starlark.None {
		t.Errorf("Resource() = %v (%s), want None", val, val.Type())
	}

	// Resource must NOT appear in collected resources.
	res := c.Resources()
	if _, ok := res["db"]; ok {
		t.Error("not-found preserve resource should not appear in Resources()")
	}

	// Warning event must use "not found in observed state" message.
	events := cc.Events()
	if len(events) != 1 {
		t.Fatalf("Events() len = %d, want 1", len(events))
	}
	wantMsg := `Skipping resource "db": not found in observed state`
	if events[0].Message != wantMsg {
		t.Errorf("event message = %q, want %q", events[0].Message, wantMsg)
	}
}

// GATE-04 (nil observed): Resource(body=None, preserve_observed=True) with
// c.observed == nil -> same as not-found.
func TestCollector_PreserveObserved_BodyNone_NilObserved(t *testing.T) {
	cc := NewConditionCollector()
	c := NewCollector(cc, "gate04-nil.star", nil, nil) // nil observed
	thread := new(starlark.Thread)

	val, err := starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String("db"),
		starlark.None,
	}, []starlark.Tuple{
		{starlark.String("preserve_observed"), starlark.True},
	})
	if err != nil {
		t.Fatalf("Resource() error: %v", err)
	}

	if val != starlark.None {
		t.Errorf("Resource() = %v (%s), want None", val, val.Type())
	}

	res := c.Resources()
	if _, ok := res["db"]; ok {
		t.Error("nil-observed preserve resource should not appear in Resources()")
	}

	events := cc.Events()
	if len(events) != 1 {
		t.Fatalf("Events() len = %d, want 1", len(events))
	}
	wantMsg := `Skipping resource "db": not found in observed state`
	if events[0].Message != wantMsg {
		t.Errorf("event message = %q, want %q", events[0].Message, wantMsg)
	}
}

// ---------------------------------------------------------------------------
// GATE-06: Resource(body=dict, preserve_observed=True) -> normal path
// (preserve_observed is dormant, body processed normally with injection).
// ---------------------------------------------------------------------------

func TestCollector_PreserveObserved_Dormant_BodyProvided(t *testing.T) {
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
		{starlark.String("preserve_observed"), starlark.True},
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

	// No preservation Warning events expected (dormant preserve).
	events := cc.Events()
	if len(events) != 0 {
		t.Errorf("Events() len = %d, want 0 (dormant preserve should emit no warnings)", len(events))
	}
}

// GATE-06 (when=False dormant): Resource(body=dict, when=True, preserve_observed=True) -> normal path.
func TestCollector_PreserveObserved_Dormant_WhenTrue(t *testing.T) {
	cc := NewConditionCollector()
	c := NewCollector(cc, "gate06-when.star", nil, nil)
	thread := new(starlark.Thread)

	body := new(starlark.Dict)
	_ = body.SetKey(starlark.String("apiVersion"), starlark.String("v1"))

	val, err := starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String("item"),
		body,
	}, []starlark.Tuple{
		{starlark.String("when"), starlark.True},
		{starlark.String("preserve_observed"), starlark.True},
	})
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

// ---------------------------------------------------------------------------
// GATE-08: Preserved body has NO ResourceNameLabel, NO crossplane traceability
// labels injected, NO external_name annotation added.
// ---------------------------------------------------------------------------

func TestCollector_PreserveObserved_NoLabelInjection(t *testing.T) {
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
		{starlark.String("preserve_observed"), starlark.True},
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

func TestCollector_PreserveObserved_StarlarkDictConversion(t *testing.T) {
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
		{starlark.String("preserve_observed"), starlark.True},
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
// Cliff guard: Resource(when=False, preserve_observed=True) with resource in
// observed state -> emit observed body.
// ---------------------------------------------------------------------------

func TestCollector_WhenFalse_PreserveObserved_Found(t *testing.T) {
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
		{starlark.String("when"), starlark.False},
		{starlark.String("preserve_observed"), starlark.True},
		{starlark.String("skip_reason"), starlark.String("optional")},
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

// Cliff guard: Resource(when=False, preserve_observed=True) with resource NOT
// in observed state -> skip.
func TestCollector_WhenFalse_PreserveObserved_NotFound(t *testing.T) {
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
		{starlark.String("when"), starlark.False},
		{starlark.String("preserve_observed"), starlark.True},
	})
	if err != nil {
		t.Fatalf("Resource() error: %v", err)
	}

	// Return value must be None.
	if val != starlark.None {
		t.Errorf("Resource() = %v (%s), want None", val, val.Type())
	}

	// Resource must NOT appear.
	res := c.Resources()
	if _, ok := res["db"]; ok {
		t.Error("not-found cliff guard resource should not appear in Resources()")
	}

	// Warning event for cliff guard skip.
	events := cc.Events()
	if len(events) != 1 {
		t.Fatalf("Events() len = %d, want 1", len(events))
	}
	wantMsg := `Skipping resource "db": gated by when=False, not found in observed state (preserve_observed=True)`
	if events[0].Message != wantMsg {
		t.Errorf("event message = %q, want %q", events[0].Message, wantMsg)
	}
}

// Cliff guard with user-provided skip_reason: used as message when not found.
func TestCollector_WhenFalse_PreserveObserved_NotFound_CustomReason(t *testing.T) {
	cc := NewConditionCollector()
	c := NewCollector(cc, "cliff-reason.star", nil, nil) // nil observed
	thread := new(starlark.Thread)

	body := new(starlark.Dict)
	_ = body.SetKey(starlark.String("kind"), starlark.String("Bucket"))

	val, err := starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String("db"),
		body,
	}, []starlark.Tuple{
		{starlark.String("when"), starlark.False},
		{starlark.String("preserve_observed"), starlark.True},
		{starlark.String("skip_reason"), starlark.String("feature disabled")},
	})
	if err != nil {
		t.Fatalf("Resource() error: %v", err)
	}

	if val != starlark.None {
		t.Errorf("Resource() = %v (%s), want None", val, val.Type())
	}

	events := cc.Events()
	if len(events) != 1 {
		t.Fatalf("Events() len = %d, want 1", len(events))
	}
	// User-provided skip_reason should be used.
	wantMsg := `Skipping resource "db": feature disabled`
	if events[0].Message != wantMsg {
		t.Errorf("event message = %q, want %q", events[0].Message, wantMsg)
	}
}

// Preservation is NOT a skip — c.skipped should NOT be set.
func TestCollector_PreserveObserved_NotInSkipped(t *testing.T) {
	observed := makeObservedDict(t, map[string]map[string]string{
		"db": {"apiVersion": "v1"},
	})
	cc := NewConditionCollector()
	c := NewCollector(cc, "preserve-not-skip.star", nil, observed)
	thread := new(starlark.Thread)

	// First preserve the resource.
	_, err := starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String("db"),
		starlark.None,
	}, []starlark.Tuple{
		{starlark.String("preserve_observed"), starlark.True},
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
func TestCollector_PreserveObserved_NoSkipMetric(t *testing.T) {
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
		{starlark.String("preserve_observed"), starlark.True},
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
