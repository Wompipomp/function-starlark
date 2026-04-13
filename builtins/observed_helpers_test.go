package builtins

import (
	"strings"
	"testing"

	fnv1 "github.com/crossplane/function-sdk-go/proto/v1"
	"go.starlark.net/starlark"
	"google.golang.org/protobuf/types/known/structpb"
)

func TestIsObserved_Exists(t *testing.T) {
	observed := map[string]*fnv1.Resource{
		"db": {Resource: &structpb.Struct{Fields: map[string]*structpb.Value{
			"apiVersion": structpb.NewStringValue("v1"),
		}}},
	}
	req := makeReq(
		map[string]*structpb.Value{"apiVersion": structpb.NewStringValue("v1")},
		map[string]*structpb.Value{"apiVersion": structpb.NewStringValue("v1")},
		observed,
	)
	c := NewCollector(NewConditionCollector(), "test.star", nil)
	globals, err := testBuildGlobals(req, c)
	if err != nil {
		t.Fatal(err)
	}

	got, err := callBuiltin(t, globals, "is_observed",
		starlark.Tuple{starlark.String("db")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != starlark.True {
		t.Errorf("got %v, want True", got)
	}
}

func TestIsObserved_Missing(t *testing.T) {
	req := makeReq(
		map[string]*structpb.Value{"apiVersion": structpb.NewStringValue("v1")},
		map[string]*structpb.Value{"apiVersion": structpb.NewStringValue("v1")},
		nil,
	)
	c := NewCollector(NewConditionCollector(), "test.star", nil)
	globals, err := testBuildGlobals(req, c)
	if err != nil {
		t.Fatal(err)
	}

	got, err := callBuiltin(t, globals, "is_observed",
		starlark.Tuple{starlark.String("missing")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != starlark.False {
		t.Errorf("got %v, want False", got)
	}
}

func TestIsObserved_EmptyName_Errors(t *testing.T) {
	req := makeReq(
		map[string]*structpb.Value{"apiVersion": structpb.NewStringValue("v1")},
		map[string]*structpb.Value{"apiVersion": structpb.NewStringValue("v1")},
		nil,
	)
	c := NewCollector(NewConditionCollector(), "test.star", nil)
	globals, err := testBuildGlobals(req, c)
	if err != nil {
		t.Fatal(err)
	}

	_, err = callBuiltin(t, globals, "is_observed",
		starlark.Tuple{starlark.String("")}, nil)
	if err == nil {
		t.Fatal("expected error for empty name")
	}
	if !strings.Contains(err.Error(), "name must not be empty") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestObservedBody_Exists(t *testing.T) {
	observed := map[string]*fnv1.Resource{
		"db": {Resource: &structpb.Struct{Fields: map[string]*structpb.Value{
			"apiVersion": structpb.NewStringValue("v1"),
		}}},
	}
	req := makeReq(
		map[string]*structpb.Value{"apiVersion": structpb.NewStringValue("v1")},
		map[string]*structpb.Value{"apiVersion": structpb.NewStringValue("v1")},
		observed,
	)
	c := NewCollector(NewConditionCollector(), "test.star", nil)
	globals, err := testBuildGlobals(req, c)
	if err != nil {
		t.Fatal(err)
	}

	got, err := callBuiltin(t, globals, "observed_body",
		starlark.Tuple{starlark.String("db")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got == starlark.None {
		t.Error("expected dict, got None")
	}
	if _, ok := got.(starlark.Mapping); !ok {
		t.Errorf("expected Mapping, got %T", got)
	}
}

func TestObservedBody_Missing_ReturnsNone(t *testing.T) {
	req := makeReq(
		map[string]*structpb.Value{"apiVersion": structpb.NewStringValue("v1")},
		map[string]*structpb.Value{"apiVersion": structpb.NewStringValue("v1")},
		nil,
	)
	c := NewCollector(NewConditionCollector(), "test.star", nil)
	globals, err := testBuildGlobals(req, c)
	if err != nil {
		t.Fatal(err)
	}

	got, err := callBuiltin(t, globals, "observed_body",
		starlark.Tuple{starlark.String("missing")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != starlark.None {
		t.Errorf("got %v, want None", got)
	}
}

func TestObservedBody_Missing_ReturnsCustomDefault(t *testing.T) {
	req := makeReq(
		map[string]*structpb.Value{"apiVersion": structpb.NewStringValue("v1")},
		map[string]*structpb.Value{"apiVersion": structpb.NewStringValue("v1")},
		nil,
	)
	c := NewCollector(NewConditionCollector(), "test.star", nil)
	globals, err := testBuildGlobals(req, c)
	if err != nil {
		t.Fatal(err)
	}

	dflt := new(starlark.Dict)
	got, err := callBuiltin(t, globals, "observed_body",
		starlark.Tuple{starlark.String("missing")},
		[]starlark.Tuple{{starlark.String("default"), dflt}})
	if err != nil {
		t.Fatal(err)
	}
	if got != dflt {
		t.Errorf("got %v, want provided default dict", got)
	}
}

// TestObservedBody_FrozenBody verifies that the body returned by observed_body
// is frozen (since observed data is immutable).
func TestObservedBody_FrozenBody(t *testing.T) {
	observed := map[string]*fnv1.Resource{
		"db": {Resource: &structpb.Struct{Fields: map[string]*structpb.Value{
			"apiVersion": structpb.NewStringValue("v1"),
			"kind":       structpb.NewStringValue("Database"),
		}}},
	}
	req := makeReq(
		map[string]*structpb.Value{"apiVersion": structpb.NewStringValue("v1")},
		map[string]*structpb.Value{"apiVersion": structpb.NewStringValue("v1")},
		observed,
	)
	c := NewCollector(NewConditionCollector(), "test.star", nil)
	globals, err := testBuildGlobals(req, c)
	if err != nil {
		t.Fatal(err)
	}

	got, err := callBuiltin(t, globals, "observed_body",
		starlark.Tuple{starlark.String("db")}, nil)
	if err != nil {
		t.Fatal(err)
	}

	// The body should be a *convert.StarlarkDict (frozen).
	if _, ok := got.(starlark.HasSetKey); ok {
		// Try to set a key -- should fail because it's frozen.
		setter := got.(starlark.HasSetKey)
		if err := setter.SetKey(starlark.String("test"), starlark.String("val")); err == nil {
			t.Error("observed body should be frozen, but SetKey succeeded")
		}
	}
}

// TestObservedBody_ContentVerification verifies that observed_body returns
// a dict with the expected fields matching the observed resource.
func TestObservedBody_ContentVerification(t *testing.T) {
	observed := map[string]*fnv1.Resource{
		"db": {Resource: &structpb.Struct{Fields: map[string]*structpb.Value{
			"apiVersion": structpb.NewStringValue("database.example.org/v1"),
			"kind":       structpb.NewStringValue("PostgreSQL"),
			"metadata": structpb.NewStructValue(&structpb.Struct{
				Fields: map[string]*structpb.Value{
					"name": structpb.NewStringValue("my-db"),
				},
			}),
		}}},
	}
	req := makeReq(
		map[string]*structpb.Value{"apiVersion": structpb.NewStringValue("v1")},
		map[string]*structpb.Value{"apiVersion": structpb.NewStringValue("v1")},
		observed,
	)
	c := NewCollector(NewConditionCollector(), "test.star", nil)
	globals, err := testBuildGlobals(req, c)
	if err != nil {
		t.Fatal(err)
	}

	got, err := callBuiltin(t, globals, "observed_body",
		starlark.Tuple{starlark.String("db")}, nil)
	if err != nil {
		t.Fatal(err)
	}

	m, ok := got.(starlark.Mapping)
	if !ok {
		t.Fatalf("expected Mapping, got %T", got)
	}

	// Check apiVersion field.
	apiVal, found, err := m.Get(starlark.String("apiVersion"))
	if err != nil || !found {
		t.Fatal("apiVersion not found in body")
	}
	apiStr, ok := apiVal.(starlark.String)
	if !ok {
		t.Fatalf("apiVersion is %T, want starlark.String", apiVal)
	}
	if string(apiStr) != "database.example.org/v1" {
		t.Errorf("apiVersion = %q, want %q", string(apiStr), "database.example.org/v1")
	}

	// Check kind field.
	kindVal, found, err := m.Get(starlark.String("kind"))
	if err != nil || !found {
		t.Fatal("kind not found in body")
	}
	kindStr, ok := kindVal.(starlark.String)
	if !ok {
		t.Fatalf("kind is %T, want starlark.String", kindVal)
	}
	if string(kindStr) != "PostgreSQL" {
		t.Errorf("kind = %q, want %q", string(kindStr), "PostgreSQL")
	}
}

func TestObservedBody_EmptyName_Errors(t *testing.T) {
	req := makeReq(
		map[string]*structpb.Value{"apiVersion": structpb.NewStringValue("v1")},
		map[string]*structpb.Value{"apiVersion": structpb.NewStringValue("v1")},
		nil,
	)
	c := NewCollector(NewConditionCollector(), "test.star", nil)
	globals, err := testBuildGlobals(req, c)
	if err != nil {
		t.Fatal(err)
	}

	_, err = callBuiltin(t, globals, "observed_body",
		starlark.Tuple{starlark.String("")}, nil)
	if err == nil {
		t.Fatal("expected error for empty name")
	}
	if !strings.Contains(err.Error(), "name must not be empty") {
		t.Errorf("unexpected error: %v", err)
	}
}
