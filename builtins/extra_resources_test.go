package builtins

import (
	"strings"
	"testing"

	fnv1 "github.com/crossplane/function-sdk-go/proto/v1"
	"go.starlark.net/starlark"
	"google.golang.org/protobuf/types/known/structpb"
)

// makeReqWithExtras builds a RunFunctionRequest with both minimal
// OXR/DXR fields and RequiredResources for extra-resource testing.
func makeReqWithExtras(extras map[string]*fnv1.Resources) *fnv1.RunFunctionRequest {
	req := makeReq(
		map[string]*structpb.Value{"apiVersion": structpb.NewStringValue("v1")},
		map[string]*structpb.Value{"apiVersion": structpb.NewStringValue("v1")},
		nil,
	)
	req.RequiredResources = extras
	return req
}

// callBuiltin invokes a starlark.Builtin from the globals dict.
func callBuiltin(t *testing.T, globals starlark.StringDict, name string, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	t.Helper()
	b, ok := globals[name]
	if !ok {
		t.Fatalf("builtin %q not found in globals", name)
	}
	fn, ok := b.(*starlark.Builtin)
	if !ok {
		t.Fatalf("%q is %T, want *starlark.Builtin", name, b)
	}
	return starlark.Call(&starlark.Thread{Name: "test"}, fn, args, kwargs)
}

func TestGetExtraResource_PathLookup(t *testing.T) {
	res := &structpb.Struct{
		Fields: map[string]*structpb.Value{
			"spec": structpb.NewStructValue(&structpb.Struct{
				Fields: map[string]*structpb.Value{
					"region": structpb.NewStringValue("us-west-2"),
				},
			}),
		},
	}
	extras := map[string]*fnv1.Resources{
		"cluster": {Items: []*fnv1.Resource{{Resource: res}}},
	}
	req := makeReqWithExtras(extras)
	c := NewCollector(NewConditionCollector(), "test.star", nil)
	globals, err := testBuildGlobals(req, c)
	if err != nil {
		t.Fatal(err)
	}

	got, err := callBuiltin(t, globals, "get_extra_resource",
		starlark.Tuple{starlark.String("cluster"), starlark.String("spec.region")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.(starlark.String) != "us-west-2" {
		t.Errorf("got %v, want us-west-2", got)
	}
}

func TestGetExtraResource_NoPath_FullBody(t *testing.T) {
	res := &structpb.Struct{
		Fields: map[string]*structpb.Value{
			"spec": structpb.NewStructValue(&structpb.Struct{
				Fields: map[string]*structpb.Value{
					"region": structpb.NewStringValue("eu-central-1"),
				},
			}),
		},
	}
	extras := map[string]*fnv1.Resources{
		"cluster": {Items: []*fnv1.Resource{{Resource: res}}},
	}
	req := makeReqWithExtras(extras)
	c := NewCollector(NewConditionCollector(), "test.star", nil)
	globals, err := testBuildGlobals(req, c)
	if err != nil {
		t.Fatal(err)
	}

	got, err := callBuiltin(t, globals, "get_extra_resource",
		starlark.Tuple{starlark.String("cluster")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Should return a dict (the full body), not None.
	if got == starlark.None {
		t.Error("expected full body dict, got None")
	}
	// Verify it's a Mapping (dict-like).
	if _, ok := got.(starlark.Mapping); !ok {
		t.Errorf("expected Mapping, got %T", got)
	}
}

func TestGetExtraResource_Missing_ReturnsDefault(t *testing.T) {
	req := makeReqWithExtras(nil)
	c := NewCollector(NewConditionCollector(), "test.star", nil)
	globals, err := testBuildGlobals(req, c)
	if err != nil {
		t.Fatal(err)
	}

	got, err := callBuiltin(t, globals, "get_extra_resource",
		starlark.Tuple{starlark.String("missing")},
		[]starlark.Tuple{{starlark.String("default"), starlark.String("fallback")}})
	if err != nil {
		t.Fatal(err)
	}
	if got.(starlark.String) != "fallback" {
		t.Errorf("got %v, want fallback", got)
	}
}

func TestGetExtraResource_EmptyMatch_ReturnsDefault(t *testing.T) {
	extras := map[string]*fnv1.Resources{
		"empty-match": {Items: nil}, // empty items -> None in dict
	}
	req := makeReqWithExtras(extras)
	c := NewCollector(NewConditionCollector(), "test.star", nil)
	globals, err := testBuildGlobals(req, c)
	if err != nil {
		t.Fatal(err)
	}

	got, err := callBuiltin(t, globals, "get_extra_resource",
		starlark.Tuple{starlark.String("empty-match")},
		[]starlark.Tuple{{starlark.String("default"), starlark.String("d")}})
	if err != nil {
		t.Fatal(err)
	}
	if got.(starlark.String) != "d" {
		t.Errorf("got %v, want d", got)
	}
}

func TestGetExtraResource_MissingPathSegment_ReturnsDefault(t *testing.T) {
	res := &structpb.Struct{
		Fields: map[string]*structpb.Value{
			"spec": structpb.NewStructValue(&structpb.Struct{
				Fields: map[string]*structpb.Value{},
			}),
		},
	}
	extras := map[string]*fnv1.Resources{
		"cluster": {Items: []*fnv1.Resource{{Resource: res}}},
	}
	req := makeReqWithExtras(extras)
	c := NewCollector(NewConditionCollector(), "test.star", nil)
	globals, err := testBuildGlobals(req, c)
	if err != nil {
		t.Fatal(err)
	}

	got, err := callBuiltin(t, globals, "get_extra_resource",
		starlark.Tuple{starlark.String("cluster"), starlark.String("spec.nonexistent")},
		[]starlark.Tuple{{starlark.String("default"), starlark.String("x")}})
	if err != nil {
		t.Fatal(err)
	}
	if got.(starlark.String) != "x" {
		t.Errorf("got %v, want x", got)
	}
}

func TestGetExtraResource_EmptyName_Errors(t *testing.T) {
	req := makeReqWithExtras(nil)
	c := NewCollector(NewConditionCollector(), "test.star", nil)
	globals, err := testBuildGlobals(req, c)
	if err != nil {
		t.Fatal(err)
	}

	_, err = callBuiltin(t, globals, "get_extra_resource",
		starlark.Tuple{starlark.String("")}, nil)
	if err == nil {
		t.Fatal("expected error for empty name")
	}
	if !strings.Contains(err.Error(), "name must not be empty") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestGetExtraResources_PathLookup(t *testing.T) {
	mkRes := func(region string) *fnv1.Resource {
		return &fnv1.Resource{
			Resource: &structpb.Struct{
				Fields: map[string]*structpb.Value{
					"spec": structpb.NewStructValue(&structpb.Struct{
						Fields: map[string]*structpb.Value{
							"region": structpb.NewStringValue(region),
						},
					}),
				},
			},
		}
	}
	extras := map[string]*fnv1.Resources{
		"clusters": {Items: []*fnv1.Resource{mkRes("us-west-2"), mkRes("eu-central-1")}},
	}
	req := makeReqWithExtras(extras)
	c := NewCollector(NewConditionCollector(), "test.star", nil)
	globals, err := testBuildGlobals(req, c)
	if err != nil {
		t.Fatal(err)
	}

	got, err := callBuiltin(t, globals, "get_extra_resources",
		starlark.Tuple{starlark.String("clusters"), starlark.String("spec.region")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	list, ok := got.(*starlark.List)
	if !ok {
		t.Fatalf("got %T, want *starlark.List", got)
	}
	if list.Len() != 2 {
		t.Fatalf("len = %d, want 2", list.Len())
	}
	if list.Index(0).(starlark.String) != "us-west-2" {
		t.Errorf("[0] = %v, want us-west-2", list.Index(0))
	}
	if list.Index(1).(starlark.String) != "eu-central-1" {
		t.Errorf("[1] = %v, want eu-central-1", list.Index(1))
	}
}

func TestGetExtraResources_NoPath_FullBodies(t *testing.T) {
	mkRes := func(region string) *fnv1.Resource {
		return &fnv1.Resource{
			Resource: &structpb.Struct{
				Fields: map[string]*structpb.Value{
					"spec": structpb.NewStructValue(&structpb.Struct{
						Fields: map[string]*structpb.Value{
							"region": structpb.NewStringValue(region),
						},
					}),
				},
			},
		}
	}
	extras := map[string]*fnv1.Resources{
		"clusters": {Items: []*fnv1.Resource{mkRes("us-west-2"), mkRes("eu-central-1")}},
	}
	req := makeReqWithExtras(extras)
	c := NewCollector(NewConditionCollector(), "test.star", nil)
	globals, err := testBuildGlobals(req, c)
	if err != nil {
		t.Fatal(err)
	}

	got, err := callBuiltin(t, globals, "get_extra_resources",
		starlark.Tuple{starlark.String("clusters")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	list, ok := got.(*starlark.List)
	if !ok {
		t.Fatalf("got %T, want *starlark.List", got)
	}
	if list.Len() != 2 {
		t.Fatalf("len = %d, want 2", list.Len())
	}
}

func TestGetExtraResources_Missing_ReturnsEmptyList(t *testing.T) {
	req := makeReqWithExtras(nil)
	c := NewCollector(NewConditionCollector(), "test.star", nil)
	globals, err := testBuildGlobals(req, c)
	if err != nil {
		t.Fatal(err)
	}

	got, err := callBuiltin(t, globals, "get_extra_resources",
		starlark.Tuple{starlark.String("missing")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	list, ok := got.(*starlark.List)
	if !ok {
		t.Fatalf("got %T, want *starlark.List", got)
	}
	if list.Len() != 0 {
		t.Errorf("len = %d, want 0", list.Len())
	}
}

func TestGetExtraResources_EmptyMatch_ReturnsDefault(t *testing.T) {
	extras := map[string]*fnv1.Resources{
		"empty-match": {Items: nil},
	}
	req := makeReqWithExtras(extras)
	c := NewCollector(NewConditionCollector(), "test.star", nil)
	globals, err := testBuildGlobals(req, c)
	if err != nil {
		t.Fatal(err)
	}

	got, err := callBuiltin(t, globals, "get_extra_resources",
		starlark.Tuple{starlark.String("empty-match")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	list, ok := got.(*starlark.List)
	if !ok {
		t.Fatalf("got %T, want *starlark.List", got)
	}
	if list.Len() != 0 {
		t.Errorf("len = %d, want 0", list.Len())
	}
}
