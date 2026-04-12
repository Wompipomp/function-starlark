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
