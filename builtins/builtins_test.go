package builtins

import (
	"strings"
	"testing"

	fnv1 "github.com/crossplane/function-sdk-go/proto/v1"
	"go.starlark.net/starlark"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/wompipomp/function-starlark/convert"
)

// helpers to build requests/responses

// testBuildGlobals wraps BuildGlobals with default (empty) extended collectors
// so that existing tests don't need to construct them.
func testBuildGlobals(req *fnv1.RunFunctionRequest, c *Collector) (starlark.StringDict, error) {
	return BuildGlobals(req, c, c.cc, NewConnectionCollector(), NewRequirementsCollector())
}

func makeReq(oxrFields, dxrFields map[string]*structpb.Value, observed map[string]*fnv1.Resource) *fnv1.RunFunctionRequest {
	req := &fnv1.RunFunctionRequest{}

	if oxrFields != nil {
		req.Observed = &fnv1.State{
			Composite: &fnv1.Resource{
				Resource: &structpb.Struct{Fields: oxrFields},
			},
		}
	}
	if dxrFields != nil {
		req.Desired = &fnv1.State{
			Composite: &fnv1.Resource{
				Resource: &structpb.Struct{Fields: dxrFields},
			},
		}
	}
	if observed != nil {
		if req.Observed == nil {
			req.Observed = &fnv1.State{}
		}
		req.Observed.Resources = observed
	}
	return req
}

// ---------------------------------------------------------------------------
// BuildGlobals tests
// ---------------------------------------------------------------------------

func TestBuildGlobals_Keys(t *testing.T) {
	req := makeReq(
		map[string]*structpb.Value{"apiVersion": structpb.NewStringValue("v1")},
		map[string]*structpb.Value{"apiVersion": structpb.NewStringValue("v1")},
		nil,
	)
	c := NewCollector(NewConditionCollector(), "test.star", nil)

	globals, err := testBuildGlobals(req, c)
	if err != nil {
		t.Fatalf("BuildGlobals error: %v", err)
	}

	expected := []string{
		"oxr", "dxr", "observed", "Resource", "skip_resource", "get",
		"context", "environment", "extra_resources",
		"set_condition", "emit_event", "fatal",
		"set_connection_details", "require_extra_resource", "require_extra_resources",
		"get_label", "get_annotation", "set_xr_status", "get_observed",
		"schema", "field", "struct", "json", "crypto", "encoding", "dict",
		"regex", "yaml",
		"get_extra_resource", "get_extra_resources",
		"is_observed", "observed_body", "get_condition",
	}
	if len(globals) != len(expected) {
		t.Errorf("len(globals) = %d, want %d", len(globals), len(expected))
	}
	for _, key := range expected {
		if _, ok := globals[key]; !ok {
			t.Errorf("missing key %q", key)
		}
	}
}

func TestBuildGlobals_OXR_Frozen(t *testing.T) {
	req := makeReq(
		map[string]*structpb.Value{"apiVersion": structpb.NewStringValue("v1")},
		map[string]*structpb.Value{},
		nil,
	)
	c := NewCollector(NewConditionCollector(), "test.star", nil)

	globals, err := testBuildGlobals(req, c)
	if err != nil {
		t.Fatal(err)
	}

	oxr, ok := globals["oxr"].(*convert.StarlarkDict)
	if !ok {
		t.Fatalf("oxr = %T, want *StarlarkDict", globals["oxr"])
	}

	// Should be frozen
	if err := oxr.SetField("x", starlark.MakeInt(1)); err == nil {
		t.Error("oxr should be frozen")
	}
}

func TestBuildGlobals_OXR_Fields(t *testing.T) {
	req := makeReq(
		map[string]*structpb.Value{
			"apiVersion": structpb.NewStringValue("test/v1"),
			"kind":       structpb.NewStringValue("XR"),
			"metadata": structpb.NewStructValue(&structpb.Struct{
				Fields: map[string]*structpb.Value{
					"name": structpb.NewStringValue("my-xr"),
				},
			}),
			"spec": structpb.NewStructValue(&structpb.Struct{
				Fields: map[string]*structpb.Value{
					"region": structpb.NewStringValue("us-east-1"),
				},
			}),
			"status": structpb.NewStructValue(&structpb.Struct{
				Fields: map[string]*structpb.Value{
					"ready": structpb.NewBoolValue(true),
				},
			}),
		},
		map[string]*structpb.Value{},
		nil,
	)
	c := NewCollector(NewConditionCollector(), "test.star", nil)

	globals, err := testBuildGlobals(req, c)
	if err != nil {
		t.Fatal(err)
	}

	oxr := globals["oxr"].(*convert.StarlarkDict)

	// Verify accessible fields
	apiVer, err := oxr.Attr("apiVersion")
	if err != nil {
		t.Fatal(err)
	}
	if apiVer != starlark.String("test/v1") {
		t.Errorf("apiVersion = %v, want 'test/v1'", apiVer)
	}

	spec, err := oxr.Attr("spec")
	if err != nil {
		t.Fatal(err)
	}
	specDict, ok := spec.(*convert.StarlarkDict)
	if !ok {
		t.Fatalf("spec = %T, want *StarlarkDict", spec)
	}
	region, err := specDict.Attr("region")
	if err != nil {
		t.Fatal(err)
	}
	if region != starlark.String("us-east-1") {
		t.Errorf("region = %v, want 'us-east-1'", region)
	}
}

func TestBuildGlobals_DXR_Mutable(t *testing.T) {
	req := makeReq(
		map[string]*structpb.Value{},
		map[string]*structpb.Value{
			"apiVersion": structpb.NewStringValue("v1"),
		},
		nil,
	)
	c := NewCollector(NewConditionCollector(), "test.star", nil)

	globals, err := testBuildGlobals(req, c)
	if err != nil {
		t.Fatal(err)
	}

	dxr, ok := globals["dxr"].(*convert.StarlarkDict)
	if !ok {
		t.Fatalf("dxr = %T, want *StarlarkDict", globals["dxr"])
	}

	// Should be mutable
	if err := dxr.SetField("newField", starlark.String("works")); err != nil {
		t.Errorf("dxr should be mutable: %v", err)
	}
}

func TestBuildGlobals_DXR_NilDesired(t *testing.T) {
	// When desired composite is nil (first-in-pipeline), dxr should be empty mutable dict
	req := makeReq(
		map[string]*structpb.Value{"apiVersion": structpb.NewStringValue("v1")},
		nil,
		nil,
	)
	c := NewCollector(NewConditionCollector(), "test.star", nil)

	globals, err := testBuildGlobals(req, c)
	if err != nil {
		t.Fatal(err)
	}

	dxr, ok := globals["dxr"].(*convert.StarlarkDict)
	if !ok {
		t.Fatalf("dxr = %T, want *StarlarkDict", globals["dxr"])
	}

	// Should be empty
	if dxr.Len() != 0 {
		t.Errorf("dxr.Len() = %d, want 0", dxr.Len())
	}

	// Should be mutable
	if err := dxr.SetField("x", starlark.MakeInt(1)); err != nil {
		t.Errorf("dxr should be mutable: %v", err)
	}
}

func TestBuildGlobals_Observed(t *testing.T) {
	req := makeReq(
		map[string]*structpb.Value{},
		map[string]*structpb.Value{},
		map[string]*fnv1.Resource{
			"bucket": {
				Resource: &structpb.Struct{
					Fields: map[string]*structpb.Value{
						"apiVersion": structpb.NewStringValue("s3.aws.upbound.io/v1"),
					},
				},
			},
			"queue": {
				Resource: &structpb.Struct{
					Fields: map[string]*structpb.Value{
						"apiVersion": structpb.NewStringValue("sqs.aws.upbound.io/v1"),
					},
				},
			},
		},
	)
	c := NewCollector(NewConditionCollector(), "test.star", nil)

	globals, err := testBuildGlobals(req, c)
	if err != nil {
		t.Fatal(err)
	}

	observed, ok := globals["observed"].(*convert.StarlarkDict)
	if !ok {
		t.Fatalf("observed = %T, want *StarlarkDict", globals["observed"])
	}

	// Should have 2 entries
	if observed.Len() != 2 {
		t.Errorf("observed.Len() = %d, want 2", observed.Len())
	}

	// Should be frozen
	if err := observed.SetField("x", starlark.MakeInt(1)); err == nil {
		t.Error("observed should be frozen")
	}

	// Each inner dict should be frozen
	bucketVal, err := observed.Attr("bucket")
	if err != nil {
		t.Fatal(err)
	}
	bucketDict, ok := bucketVal.(*convert.StarlarkDict)
	if !ok {
		t.Fatalf("bucket = %T, want *StarlarkDict", bucketVal)
	}
	if err := bucketDict.SetField("x", starlark.MakeInt(1)); err == nil {
		t.Error("observed resource dict should be frozen")
	}
}

func TestBuildGlobals_Observed_Empty(t *testing.T) {
	req := makeReq(
		map[string]*structpb.Value{},
		map[string]*structpb.Value{},
		nil,
	)
	c := NewCollector(NewConditionCollector(), "test.star", nil)

	globals, err := testBuildGlobals(req, c)
	if err != nil {
		t.Fatal(err)
	}

	observed := globals["observed"].(*convert.StarlarkDict)
	if observed.Len() != 0 {
		t.Errorf("observed.Len() = %d, want 0", observed.Len())
	}

	// Missing resource should return None via Attr
	missing, err := observed.Attr("nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	if missing != starlark.None {
		t.Errorf("observed.nonexistent = %v, want None", missing)
	}
}

// ---------------------------------------------------------------------------
// get() builtin tests
// ---------------------------------------------------------------------------

func TestGetBuiltin_DotPath(t *testing.T) {
	req := makeReq(
		map[string]*structpb.Value{
			"spec": structpb.NewStructValue(&structpb.Struct{
				Fields: map[string]*structpb.Value{
					"parameters": structpb.NewStructValue(&structpb.Struct{
						Fields: map[string]*structpb.Value{
							"region": structpb.NewStringValue("us-east-1"),
						},
					}),
				},
			}),
		},
		map[string]*structpb.Value{},
		nil,
	)
	c := NewCollector(NewConditionCollector(), "test.star", nil)
	globals, err := testBuildGlobals(req, c)
	if err != nil {
		t.Fatal(err)
	}

	thread := new(starlark.Thread)
	result, err := starlark.Call(thread, globals["get"], starlark.Tuple{
		globals["oxr"],
		starlark.String("spec.parameters.region"),
	}, nil)
	if err != nil {
		t.Fatalf("get() error: %v", err)
	}
	if result != starlark.String("us-east-1") {
		t.Errorf("get(oxr, 'spec.parameters.region') = %v, want 'us-east-1'", result)
	}
}

func TestGetBuiltin_MissingReturnsDefault(t *testing.T) {
	req := makeReq(
		map[string]*structpb.Value{
			"spec": structpb.NewStructValue(&structpb.Struct{
				Fields: map[string]*structpb.Value{},
			}),
		},
		map[string]*structpb.Value{},
		nil,
	)
	c := NewCollector(NewConditionCollector(), "test.star", nil)
	globals, err := testBuildGlobals(req, c)
	if err != nil {
		t.Fatal(err)
	}

	thread := new(starlark.Thread)
	result, err := starlark.Call(thread, globals["get"], starlark.Tuple{
		globals["oxr"],
		starlark.String("spec.missing.field"),
	}, nil)
	if err != nil {
		t.Fatalf("get() error: %v", err)
	}
	if result != starlark.None {
		t.Errorf("get(oxr, 'spec.missing.field') = %v, want None", result)
	}
}

func TestGetBuiltin_CustomDefault(t *testing.T) {
	req := makeReq(
		map[string]*structpb.Value{},
		map[string]*structpb.Value{},
		nil,
	)
	c := NewCollector(NewConditionCollector(), "test.star", nil)
	globals, err := testBuildGlobals(req, c)
	if err != nil {
		t.Fatal(err)
	}

	thread := new(starlark.Thread)
	result, err := starlark.Call(thread, globals["get"], starlark.Tuple{
		globals["oxr"],
		starlark.String("spec.missing"),
	}, []starlark.Tuple{
		{starlark.String("default"), starlark.String("fallback")},
	})
	if err != nil {
		t.Fatalf("get() error: %v", err)
	}
	if result != starlark.String("fallback") {
		t.Errorf("get(oxr, 'spec.missing', default='fallback') = %v, want 'fallback'", result)
	}
}

func TestGetBuiltin_ListPath(t *testing.T) {
	req := makeReq(
		map[string]*structpb.Value{
			"metadata": structpb.NewStructValue(&structpb.Struct{
				Fields: map[string]*structpb.Value{
					"annotations": structpb.NewStructValue(&structpb.Struct{
						Fields: map[string]*structpb.Value{
							"app.kubernetes.io/name": structpb.NewStringValue("myapp"),
						},
					}),
				},
			}),
		},
		map[string]*structpb.Value{},
		nil,
	)
	c := NewCollector(NewConditionCollector(), "test.star", nil)
	globals, err := testBuildGlobals(req, c)
	if err != nil {
		t.Fatal(err)
	}

	thread := new(starlark.Thread)
	pathList := starlark.NewList([]starlark.Value{
		starlark.String("metadata"),
		starlark.String("annotations"),
		starlark.String("app.kubernetes.io/name"),
	})
	result, err := starlark.Call(thread, globals["get"], starlark.Tuple{
		globals["oxr"],
		pathList,
	}, nil)
	if err != nil {
		t.Fatalf("get() error: %v", err)
	}
	if result != starlark.String("myapp") {
		t.Errorf("get with list path = %v, want 'myapp'", result)
	}
}

func TestGetBuiltin_NonMapping(t *testing.T) {
	thread := new(starlark.Thread)
	getFn := starlark.NewBuiltin("get", getFnImpl)

	result, err := starlark.Call(thread, getFn, starlark.Tuple{
		starlark.String("not a dict"),
		starlark.String("path"),
	}, nil)
	if err != nil {
		t.Fatalf("get() error: %v", err)
	}
	if result != starlark.None {
		t.Errorf("get(non_dict, 'path') = %v, want None", result)
	}
}

// ---------------------------------------------------------------------------
// ApplyResources tests
// ---------------------------------------------------------------------------

func TestApplyResources_Empty(t *testing.T) {
	c := NewCollector(NewConditionCollector(), "test.star", nil)
	rsp := &fnv1.RunFunctionResponse{}

	err := ApplyResources(rsp, c)
	if err != nil {
		t.Fatalf("ApplyResources error: %v", err)
	}
	// Should not modify response
	if rsp.Desired != nil {
		t.Error("empty collector should not create Desired")
	}
}

func TestApplyResources_MergeNew(t *testing.T) {
	c := NewCollector(NewConditionCollector(), "test.star", nil)
	thread := new(starlark.Thread)

	body := new(starlark.Dict)
	_ = body.SetKey(starlark.String("apiVersion"), starlark.String("v1"))

	_, _ = starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String("bucket"),
		body,
	}, nil)

	rsp := &fnv1.RunFunctionResponse{
		Desired: &fnv1.State{
			Resources: make(map[string]*fnv1.Resource),
		},
	}

	err := ApplyResources(rsp, c)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if _, ok := rsp.Desired.Resources["bucket"]; !ok {
		t.Error("missing resource 'bucket' in response")
	}
}

func TestApplyResources_PreservesPrior(t *testing.T) {
	c := NewCollector(NewConditionCollector(), "test.star", nil)
	thread := new(starlark.Thread)

	body := new(starlark.Dict)
	_ = body.SetKey(starlark.String("apiVersion"), starlark.String("v1"))

	_, _ = starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String("new-resource"),
		body,
	}, nil)

	rsp := &fnv1.RunFunctionResponse{
		Desired: &fnv1.State{
			Resources: map[string]*fnv1.Resource{
				"prior-resource": {
					Resource: &structpb.Struct{
						Fields: map[string]*structpb.Value{
							"kind": structpb.NewStringValue("Prior"),
						},
					},
				},
			},
		},
	}

	err := ApplyResources(rsp, c)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if _, ok := rsp.Desired.Resources["prior-resource"]; !ok {
		t.Error("prior resource should be preserved")
	}
	if _, ok := rsp.Desired.Resources["new-resource"]; !ok {
		t.Error("new resource should be added")
	}
}

func TestApplyResources_OverwritesSameName(t *testing.T) {
	c := NewCollector(NewConditionCollector(), "test.star", nil)
	thread := new(starlark.Thread)

	body := new(starlark.Dict)
	_ = body.SetKey(starlark.String("kind"), starlark.String("Updated"))

	_, _ = starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String("item"),
		body,
	}, nil)

	rsp := &fnv1.RunFunctionResponse{
		Desired: &fnv1.State{
			Resources: map[string]*fnv1.Resource{
				"item": {
					Resource: &structpb.Struct{
						Fields: map[string]*structpb.Value{
							"kind": structpb.NewStringValue("Original"),
						},
					},
				},
			},
		},
	}

	err := ApplyResources(rsp, c)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if rsp.Desired.Resources["item"].Resource.GetFields()["kind"].GetStringValue() != "Updated" {
		t.Error("same-name resource should be overwritten")
	}
}

func TestApplyResources_ReadyEnum(t *testing.T) {
	c := NewCollector(NewConditionCollector(), "test.star", nil)
	thread := new(starlark.Thread)

	body1 := new(starlark.Dict)
	_ = body1.SetKey(starlark.String("kind"), starlark.String("ReadyRes"))
	_, _ = starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String("ready-item"),
		body1,
	}, []starlark.Tuple{
		{starlark.String("ready"), starlark.True},
	}) // explicit ready=True

	body2 := new(starlark.Dict)
	_ = body2.SetKey(starlark.String("kind"), starlark.String("NotReadyRes"))
	_, _ = starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String("not-ready-item"),
		body2,
	}, []starlark.Tuple{
		{starlark.String("ready"), starlark.False},
	})

	rsp := &fnv1.RunFunctionResponse{}

	err := ApplyResources(rsp, c)
	if err != nil {
		t.Fatalf("error: %v", err)
	}

	readyProto := rsp.Desired.Resources["ready-item"].Ready
	if readyProto != fnv1.Ready_READY_TRUE {
		t.Errorf("ready-item Ready = %v, want READY_TRUE", readyProto)
	}

	notReadyProto := rsp.Desired.Resources["not-ready-item"].Ready
	if notReadyProto != fnv1.Ready_READY_FALSE {
		t.Errorf("not-ready-item Ready = %v, want READY_FALSE", notReadyProto)
	}
}

func TestApplyResources_NilDesired(t *testing.T) {
	c := NewCollector(NewConditionCollector(), "test.star", nil)
	thread := new(starlark.Thread)

	body := new(starlark.Dict)
	_ = body.SetKey(starlark.String("apiVersion"), starlark.String("v1"))

	_, _ = starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String("item"),
		body,
	}, nil)

	rsp := &fnv1.RunFunctionResponse{} // nil Desired

	err := ApplyResources(rsp, c)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if rsp.Desired == nil {
		t.Fatal("Desired should be created")
	}
	if rsp.Desired.Resources == nil {
		t.Fatal("Resources should be created")
	}
	if _, ok := rsp.Desired.Resources["item"]; !ok {
		t.Error("missing resource 'item'")
	}
}

// ---------------------------------------------------------------------------
// ApplyDXR tests
// ---------------------------------------------------------------------------

func TestApplyDXR_Basic(t *testing.T) {
	dxr := convert.NewStarlarkDict(0)
	_ = dxr.SetField("apiVersion", starlark.String("v1"))
	_ = dxr.SetField("status", starlark.String("ready"))

	rsp := &fnv1.RunFunctionResponse{
		Desired: &fnv1.State{
			Composite: &fnv1.Resource{},
		},
	}

	err := ApplyDXR(rsp, dxr)
	if err != nil {
		t.Fatalf("ApplyDXR error: %v", err)
	}

	if rsp.Desired.Composite.Resource == nil {
		t.Fatal("Composite.Resource is nil")
	}
	if rsp.Desired.Composite.Resource.GetFields()["apiVersion"].GetStringValue() != "v1" {
		t.Error("apiVersion not set correctly")
	}
}

func TestApplyDXR_NilDesired(t *testing.T) {
	dxr := convert.NewStarlarkDict(0)
	_ = dxr.SetField("status", starlark.String("ready"))

	rsp := &fnv1.RunFunctionResponse{} // nil Desired

	err := ApplyDXR(rsp, dxr)
	if err != nil {
		t.Fatalf("error: %v", err)
	}

	if rsp.Desired == nil {
		t.Fatal("Desired should be created")
	}
	if rsp.Desired.Composite == nil {
		t.Fatal("Composite should be created")
	}
	if rsp.Desired.Composite.Resource == nil {
		t.Fatal("Resource should be set")
	}
}

func TestApplyDXR_EmptyDXR(t *testing.T) {
	dxr := convert.NewStarlarkDict(0)

	rsp := &fnv1.RunFunctionResponse{}

	err := ApplyDXR(rsp, dxr)
	if err != nil {
		t.Fatalf("error: %v", err)
	}

	if rsp.Desired.Composite.Resource == nil {
		t.Fatal("Resource should be set (empty Struct)")
	}
	if len(rsp.Desired.Composite.Resource.GetFields()) != 0 {
		t.Errorf("fields = %d, want 0", len(rsp.Desired.Composite.Resource.GetFields()))
	}
}

func TestApplyDXR_WrongType(t *testing.T) {
	// ApplyDXR should return error when passed a plain *starlark.Dict instead of *StarlarkDict.
	d := new(starlark.Dict)
	_ = d.SetKey(starlark.String("x"), starlark.MakeInt(1))

	rsp := &fnv1.RunFunctionResponse{}

	err := ApplyDXR(rsp, d)
	if err == nil {
		t.Fatal("ApplyDXR with *starlark.Dict should return error")
	}
	if !strings.Contains(err.Error(), "want *convert.StarlarkDict") {
		t.Errorf("error %q should contain 'want *convert.StarlarkDict'", err.Error())
	}
}

// ---------------------------------------------------------------------------
// pathToKeys tests
// ---------------------------------------------------------------------------

func TestPathToKeys_InvalidType(t *testing.T) {
	_, err := pathToKeys(starlark.MakeInt(42))
	if err == nil {
		t.Fatal("pathToKeys with Int should return error")
	}
	if !strings.Contains(err.Error(), "must be string or list") {
		t.Errorf("error %q should contain 'must be string or list'", err.Error())
	}
}

func TestPathToKeys_NonStringListElement(t *testing.T) {
	pathList := starlark.NewList([]starlark.Value{
		starlark.String("spec"),
		starlark.MakeInt(42),
	})
	_, err := pathToKeys(pathList)
	if err == nil {
		t.Fatal("pathToKeys with non-string list element should return error")
	}
	if !strings.Contains(err.Error(), "want string") {
		t.Errorf("error %q should contain 'want string'", err.Error())
	}
}

// ---------------------------------------------------------------------------
// get_label builtin tests
// ---------------------------------------------------------------------------

func TestGetLabel(t *testing.T) {
	// Helper to build xr with labels and annotations.
	xrWithLabelsAndAnnotations := func(labels, annotations map[string]*structpb.Value) map[string]*structpb.Value {
		metadataFields := map[string]*structpb.Value{}
		if labels != nil {
			metadataFields["labels"] = structpb.NewStructValue(&structpb.Struct{Fields: labels})
		}
		if annotations != nil {
			metadataFields["annotations"] = structpb.NewStructValue(&structpb.Struct{Fields: annotations})
		}
		return map[string]*structpb.Value{
			"apiVersion": structpb.NewStringValue("test/v1"),
			"metadata":   structpb.NewStructValue(&structpb.Struct{Fields: metadataFields}),
		}
	}

	tests := []struct {
		name    string
		obj     starlark.Value // if non-nil, use directly instead of building from oxrFields
		oxr     map[string]*structpb.Value
		key     starlark.Value
		dflt    starlark.Value // nil means no default kwarg
		want    starlark.Value
		wantErr string
	}{
		{
			name: "happy path dotted key",
			oxr: xrWithLabelsAndAnnotations(
				map[string]*structpb.Value{
					"app.kubernetes.io/name": structpb.NewStringValue("myapp"),
				},
				nil,
			),
			key:  starlark.String("app.kubernetes.io/name"),
			want: starlark.String("myapp"),
		},
		{
			name: "simple key",
			oxr: xrWithLabelsAndAnnotations(
				map[string]*structpb.Value{
					"env": structpb.NewStringValue("production"),
				},
				nil,
			),
			key:  starlark.String("env"),
			want: starlark.String("production"),
		},
		{
			name: "missing key returns None",
			oxr: xrWithLabelsAndAnnotations(
				map[string]*structpb.Value{
					"env": structpb.NewStringValue("production"),
				},
				nil,
			),
			key:  starlark.String("nonexistent"),
			want: starlark.None,
		},
		{
			name: "missing key returns custom default",
			oxr: xrWithLabelsAndAnnotations(
				map[string]*structpb.Value{
					"env": structpb.NewStringValue("production"),
				},
				nil,
			),
			key:  starlark.String("nonexistent"),
			dflt: starlark.String("fallback"),
			want: starlark.String("fallback"),
		},
		{
			name: "missing labels map returns default",
			oxr:  xrWithLabelsAndAnnotations(nil, nil),
			key:  starlark.String("key"),
			want: starlark.None,
		},
		{
			name: "missing metadata returns default",
			oxr: map[string]*structpb.Value{
				"apiVersion": structpb.NewStringValue("test/v1"),
			},
			key:  starlark.String("key"),
			want: starlark.None,
		},
		{
			name: "non-Mapping res returns default silently",
			obj:  starlark.String("not a dict"),
			key:  starlark.String("key"),
			want: starlark.None,
		},
		{
			name: "None res returns default silently",
			obj:  starlark.None,
			key:  starlark.String("key"),
			want: starlark.None,
		},
		{
			name:    "empty key string raises error",
			oxr:     xrWithLabelsAndAnnotations(nil, nil),
			key:     starlark.String(""),
			wantErr: "must not be empty",
		},
		{
			name: "empty string label value returned as-is",
			oxr: xrWithLabelsAndAnnotations(
				map[string]*structpb.Value{
					"key": structpb.NewStringValue(""),
				},
				nil,
			),
			key:  starlark.String("key"),
			want: starlark.String(""),
		},
		{
			name: "works on observed resource dict (frozen StarlarkDict)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			thread := new(starlark.Thread)
			getLabelFn := starlark.NewBuiltin("get_label", getLabelImpl)

			var obj starlark.Value
			switch {
			case tt.obj != nil:
				obj = tt.obj
			case tt.name == "works on observed resource dict (frozen StarlarkDict)":
				// Build an observed resource with labels.
				req := makeReq(
					map[string]*structpb.Value{},
					map[string]*structpb.Value{},
					map[string]*fnv1.Resource{
						"bucket": {
							Resource: &structpb.Struct{
								Fields: xrWithLabelsAndAnnotations(
									map[string]*structpb.Value{
										"app.kubernetes.io/name": structpb.NewStringValue("myapp"),
									},
									nil,
								),
							},
						},
					},
				)
				c := NewCollector(NewConditionCollector(), "test.star", nil)
				globals, err := testBuildGlobals(req, c)
				if err != nil {
					t.Fatal(err)
				}
				observed := globals["observed"].(*convert.StarlarkDict)
				bucketVal, err := observed.Attr("bucket")
				if err != nil {
					t.Fatal(err)
				}
				obj = bucketVal

				// Call get_label on the observed resource.
				result, err := starlark.Call(thread, getLabelFn, starlark.Tuple{
					obj,
					starlark.String("app.kubernetes.io/name"),
				}, nil)
				if err != nil {
					t.Fatalf("get_label() error: %v", err)
				}
				if result != starlark.String("myapp") {
					t.Errorf("get_label(observed_res, 'app.kubernetes.io/name') = %v, want 'myapp'", result)
				}
				return
			default:
				// Build oxr from fields.
				req := makeReq(tt.oxr, map[string]*structpb.Value{}, nil)
				c := NewCollector(NewConditionCollector(), "test.star", nil)
				globals, err := testBuildGlobals(req, c)
				if err != nil {
					t.Fatal(err)
				}
				obj = globals["oxr"]
			}

			positionalArgs := starlark.Tuple{obj, tt.key}
			var kwargs []starlark.Tuple
			if tt.dflt != nil {
				kwargs = []starlark.Tuple{
					{starlark.String("default"), tt.dflt},
				}
			}

			result, err := starlark.Call(thread, getLabelFn, positionalArgs, kwargs)

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("error %q should contain %q", err.Error(), tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("get_label() error: %v", err)
			}
			if result != tt.want {
				t.Errorf("get_label() = %v, want %v", result, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// get_annotation builtin tests
// ---------------------------------------------------------------------------

func TestGetAnnotation(t *testing.T) {
	tests := []struct {
		name    string
		oxr     map[string]*structpb.Value
		key     starlark.Value
		want    starlark.Value
		wantErr string
	}{
		{
			name: "happy path dotted key",
			oxr: map[string]*structpb.Value{
				"metadata": structpb.NewStructValue(&structpb.Struct{
					Fields: map[string]*structpb.Value{
						"annotations": structpb.NewStructValue(&structpb.Struct{
							Fields: map[string]*structpb.Value{
								"crossplane.io/external-name": structpb.NewStringValue("my-resource"),
							},
						}),
					},
				}),
			},
			key:  starlark.String("crossplane.io/external-name"),
			want: starlark.String("my-resource"),
		},
		{
			name: "missing annotations map returns default",
			oxr: map[string]*structpb.Value{
				"metadata": structpb.NewStructValue(&structpb.Struct{
					Fields: map[string]*structpb.Value{},
				}),
			},
			key:  starlark.String("key"),
			want: starlark.None,
		},
		{
			name: "missing metadata returns default",
			oxr: map[string]*structpb.Value{
				"apiVersion": structpb.NewStringValue("test/v1"),
			},
			key:  starlark.String("key"),
			want: starlark.None,
		},
		{
			name:    "empty key string raises error",
			oxr:     map[string]*structpb.Value{},
			key:     starlark.String(""),
			wantErr: "must not be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			thread := new(starlark.Thread)
			getAnnotationFn := starlark.NewBuiltin("get_annotation", getAnnotationImpl)

			req := makeReq(tt.oxr, map[string]*structpb.Value{}, nil)
			c := NewCollector(NewConditionCollector(), "test.star", nil)
			globals, err := testBuildGlobals(req, c)
			if err != nil {
				t.Fatal(err)
			}
			obj := globals["oxr"]

			result, err := starlark.Call(thread, getAnnotationFn, starlark.Tuple{
				obj,
				tt.key,
			}, nil)

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("error %q should contain %q", err.Error(), tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("get_annotation() error: %v", err)
			}
			if result != tt.want {
				t.Errorf("get_annotation() = %v, want %v", result, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// set_xr_status builtin tests
// ---------------------------------------------------------------------------

func TestSetXRStatus(t *testing.T) {
	// Helper to create a set_xr_status builtin closure over the given dxr.
	makeSetFn := func(dxr *convert.StarlarkDict) *starlark.Builtin {
		return starlark.NewBuiltin("set_xr_status", func(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
			return setXRStatus(b.Name(), dxr, args, kwargs)
		})
	}

	thread := new(starlark.Thread)

	tests := []struct {
		name    string
		setup   func() *convert.StarlarkDict // build dxr before test
		path    starlark.Value
		value   starlark.Value
		verify  func(t *testing.T, dxr *convert.StarlarkDict) // verify dxr after call
		wantErr string
	}{
		{
			name: "nested path atProvider.projectId",
			setup: func() *convert.StarlarkDict {
				return convert.NewStarlarkDict(0)
			},
			path:  starlark.String("atProvider.projectId"),
			value: starlark.String("proj-123"),
			verify: func(t *testing.T, dxr *convert.StarlarkDict) {
				status, err := dxr.Attr("status")
				if err != nil {
					t.Fatalf("dxr.Attr(status): %v", err)
				}
				sd, ok := status.(*convert.StarlarkDict)
				if !ok {
					t.Fatalf("status is %T, want *StarlarkDict", status)
				}
				ap, err := sd.Attr("atProvider")
				if err != nil {
					t.Fatalf("status.Attr(atProvider): %v", err)
				}
				apDict, ok := ap.(*convert.StarlarkDict)
				if !ok {
					t.Fatalf("atProvider is %T, want *StarlarkDict", ap)
				}
				pid, err := apDict.Attr("projectId")
				if err != nil {
					t.Fatalf("atProvider.Attr(projectId): %v", err)
				}
				if pid != starlark.String("proj-123") {
					t.Errorf("projectId = %v, want 'proj-123'", pid)
				}
			},
		},
		{
			name: "single-segment path ready",
			setup: func() *convert.StarlarkDict {
				return convert.NewStarlarkDict(0)
			},
			path:  starlark.String("ready"),
			value: starlark.True,
			verify: func(t *testing.T, dxr *convert.StarlarkDict) {
				status, _ := dxr.Attr("status")
				sd := status.(*convert.StarlarkDict)
				ready, _ := sd.Attr("ready")
				if ready != starlark.True {
					t.Errorf("ready = %v, want True", ready)
				}
			},
		},
		{
			name: "empty path error",
			setup: func() *convert.StarlarkDict {
				return convert.NewStarlarkDict(0)
			},
			path:    starlark.String(""),
			value:   starlark.String("val"),
			wantErr: "path must not be empty",
		},
		{
			name: "malformed path leading dot",
			setup: func() *convert.StarlarkDict {
				return convert.NewStarlarkDict(0)
			},
			path:    starlark.String(".foo"),
			value:   starlark.String("val"),
			wantErr: "malformed path",
		},
		{
			name: "malformed path trailing dot",
			setup: func() *convert.StarlarkDict {
				return convert.NewStarlarkDict(0)
			},
			path:    starlark.String("foo."),
			value:   starlark.String("val"),
			wantErr: "malformed path",
		},
		{
			name: "malformed path consecutive dots",
			setup: func() *convert.StarlarkDict {
				return convert.NewStarlarkDict(0)
			},
			path:    starlark.String("foo..bar"),
			value:   starlark.String("val"),
			wantErr: "malformed path",
		},
		{
			name: "auto-creates status key",
			setup: func() *convert.StarlarkDict {
				return convert.NewStarlarkDict(0)
			},
			path:  starlark.String("ready"),
			value: starlark.True,
			verify: func(t *testing.T, dxr *convert.StarlarkDict) {
				v, found, err := dxr.Get(starlark.String("status"))
				if err != nil {
					t.Fatalf("dxr.Get(status): %v", err)
				}
				if !found {
					t.Fatal("status key not found on dxr")
				}
				if _, ok := v.(*convert.StarlarkDict); !ok {
					t.Errorf("status is %T, want *convert.StarlarkDict", v)
				}
			},
		},
		{
			name: "auto-creates intermediate dicts",
			setup: func() *convert.StarlarkDict {
				return convert.NewStarlarkDict(0)
			},
			path:  starlark.String("a.b.c"),
			value: starlark.String("deep"),
			verify: func(t *testing.T, dxr *convert.StarlarkDict) {
				status, _ := dxr.Attr("status")
				sd := status.(*convert.StarlarkDict)
				a, _ := sd.Attr("a")
				aDict := a.(*convert.StarlarkDict)
				b, _ := aDict.Attr("b")
				bDict := b.(*convert.StarlarkDict)
				c, _ := bDict.Attr("c")
				if c != starlark.String("deep") {
					t.Errorf("a.b.c = %v, want 'deep'", c)
				}
			},
		},
		{
			name: "sibling paths both persist",
			setup: func() *convert.StarlarkDict {
				return convert.NewStarlarkDict(0)
			},
			path:  starlark.String("atProvider.projectId"),
			value: starlark.String("proj-123"),
			verify: func(t *testing.T, dxr *convert.StarlarkDict) {
				// Make a second call for sibling path.
				setFn := makeSetFn(dxr)
				_, err := starlark.Call(thread, setFn, starlark.Tuple{
					starlark.String("atProvider.region"),
					starlark.String("us-east-1"),
				}, nil)
				if err != nil {
					t.Fatalf("second set_xr_status call: %v", err)
				}

				// Verify both present.
				status, _ := dxr.Attr("status")
				sd := status.(*convert.StarlarkDict)
				ap, _ := sd.Attr("atProvider")
				apDict := ap.(*convert.StarlarkDict)

				pid, _ := apDict.Attr("projectId")
				if pid != starlark.String("proj-123") {
					t.Errorf("projectId = %v, want 'proj-123'", pid)
				}
				region, _ := apDict.Attr("region")
				if region != starlark.String("us-east-1") {
					t.Errorf("region = %v, want 'us-east-1'", region)
				}
			},
		},
		{
			name: "preserves existing top-level status siblings",
			setup: func() *convert.StarlarkDict {
				dxr := convert.NewStarlarkDict(0)
				statusDict := convert.NewStarlarkDict(0)
				_ = statusDict.SetKey(starlark.String("ready"), starlark.True)
				_ = dxr.SetKey(starlark.String("status"), statusDict)
				return dxr
			},
			path:  starlark.String("atProvider.projectId"),
			value: starlark.String("proj-123"),
			verify: func(t *testing.T, dxr *convert.StarlarkDict) {
				status, _ := dxr.Attr("status")
				sd := status.(*convert.StarlarkDict)
				ready, _ := sd.Attr("ready")
				if ready != starlark.True {
					t.Errorf("ready = %v, want True (sibling should be preserved)", ready)
				}
				ap, _ := sd.Attr("atProvider")
				apDict := ap.(*convert.StarlarkDict)
				pid, _ := apDict.Attr("projectId")
				if pid != starlark.String("proj-123") {
					t.Errorf("projectId = %v, want 'proj-123'", pid)
				}
			},
		},
		{
			name: "auto-created intermediates are StarlarkDict type",
			setup: func() *convert.StarlarkDict {
				return convert.NewStarlarkDict(0)
			},
			path:  starlark.String("atProvider.projectId"),
			value: starlark.String("proj-123"),
			verify: func(t *testing.T, dxr *convert.StarlarkDict) {
				statusVal, found, _ := dxr.Get(starlark.String("status"))
				if !found {
					t.Fatal("status not found")
				}
				if _, ok := statusVal.(*convert.StarlarkDict); !ok {
					t.Errorf("status is %T, want *convert.StarlarkDict", statusVal)
				}

				statusDict := statusVal.(*convert.StarlarkDict)
				apVal, found, _ := statusDict.Get(starlark.String("atProvider"))
				if !found {
					t.Fatal("atProvider not found")
				}
				if _, ok := apVal.(*convert.StarlarkDict); !ok {
					t.Errorf("atProvider is %T, want *convert.StarlarkDict", apVal)
				}
			},
		},
		{
			name: "dot-access works on auto-created intermediates",
			setup: func() *convert.StarlarkDict {
				return convert.NewStarlarkDict(0)
			},
			path:  starlark.String("atProvider.projectId"),
			value: starlark.String("proj-123"),
			verify: func(t *testing.T, dxr *convert.StarlarkDict) {
				// Use Attr chain to verify dot-access works.
				status, err := dxr.Attr("status")
				if err != nil {
					t.Fatalf("dxr.Attr(status): %v", err)
				}
				statusSD, ok := status.(*convert.StarlarkDict)
				if !ok {
					t.Fatalf("status is %T, want *StarlarkDict", status)
				}
				ap, err := statusSD.Attr("atProvider")
				if err != nil {
					t.Fatalf("status.Attr(atProvider): %v", err)
				}
				apSD, ok := ap.(*convert.StarlarkDict)
				if !ok {
					t.Fatalf("atProvider is %T, want *StarlarkDict", ap)
				}
				pid, err := apSD.Attr("projectId")
				if err != nil {
					t.Fatalf("atProvider.Attr(projectId): %v", err)
				}
				if pid != starlark.String("proj-123") {
					t.Errorf("projectId = %v, want 'proj-123'", pid)
				}
			},
		},
		{
			name: "non-dict intermediate overwritten with StarlarkDict",
			setup: func() *convert.StarlarkDict {
				dxr := convert.NewStarlarkDict(0)
				statusDict := convert.NewStarlarkDict(0)
				// Put a string value at "atProvider" -- should be overwritten.
				_ = statusDict.SetKey(starlark.String("atProvider"), starlark.String("was-a-string"))
				_ = dxr.SetKey(starlark.String("status"), statusDict)
				return dxr
			},
			path:  starlark.String("atProvider.projectId"),
			value: starlark.String("proj-123"),
			verify: func(t *testing.T, dxr *convert.StarlarkDict) {
				status, _ := dxr.Attr("status")
				sd := status.(*convert.StarlarkDict)
				ap, _ := sd.Attr("atProvider")
				apDict, ok := ap.(*convert.StarlarkDict)
				if !ok {
					t.Fatalf("atProvider is %T after overwrite, want *StarlarkDict", ap)
				}
				pid, _ := apDict.Attr("projectId")
				if pid != starlark.String("proj-123") {
					t.Errorf("projectId = %v, want 'proj-123'", pid)
				}
			},
		},
		{
			name: "writing None stores None at path",
			setup: func() *convert.StarlarkDict {
				return convert.NewStarlarkDict(0)
			},
			path:  starlark.String("atProvider.projectId"),
			value: starlark.None,
			verify: func(t *testing.T, dxr *convert.StarlarkDict) {
				status, _ := dxr.Attr("status")
				sd := status.(*convert.StarlarkDict)
				ap, _ := sd.Attr("atProvider")
				apDict := ap.(*convert.StarlarkDict)
				pid, found, _ := apDict.Get(starlark.String("projectId"))
				if !found {
					t.Fatal("projectId not found")
				}
				if pid != starlark.None {
					t.Errorf("projectId = %v, want None", pid)
				}
			},
		},
		{
			name: "writing a dict replaces at path no deep merge",
			setup: func() *convert.StarlarkDict {
				dxr := convert.NewStarlarkDict(0)
				statusDict := convert.NewStarlarkDict(0)
				apDict := convert.NewStarlarkDict(0)
				_ = apDict.SetKey(starlark.String("existing"), starlark.String("val"))
				_ = statusDict.SetKey(starlark.String("atProvider"), apDict)
				_ = dxr.SetKey(starlark.String("status"), statusDict)
				return dxr
			},
			path: starlark.String("atProvider"),
			value: func() starlark.Value {
				d := convert.NewStarlarkDict(0)
				_ = d.SetKey(starlark.String("replacement"), starlark.String("new"))
				return d
			}(),
			verify: func(t *testing.T, dxr *convert.StarlarkDict) {
				status, _ := dxr.Attr("status")
				sd := status.(*convert.StarlarkDict)
				ap, _ := sd.Attr("atProvider")
				apDict, ok := ap.(*convert.StarlarkDict)
				if !ok {
					t.Fatalf("atProvider is %T, want *StarlarkDict", ap)
				}
				// "existing" should be gone (replaced, not merged).
				existing, _ := apDict.Attr("existing")
				if existing != starlark.None {
					t.Errorf("existing = %v, want None (should be replaced)", existing)
				}
				replacement, _ := apDict.Attr("replacement")
				if replacement != starlark.String("new") {
					t.Errorf("replacement = %v, want 'new'", replacement)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dxr := tt.setup()
			setFn := makeSetFn(dxr)

			result, err := starlark.Call(thread, setFn, starlark.Tuple{
				tt.path,
				tt.value,
			}, nil)

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("error %q should contain %q", err.Error(), tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("set_xr_status() error: %v", err)
			}
			if result != starlark.None {
				t.Errorf("set_xr_status() = %v, want None", result)
			}
			if tt.verify != nil {
				tt.verify(t, dxr)
			}
		})
	}
}

func TestGetBuiltin_NoneIntermediate(t *testing.T) {
	// When an intermediate value in the path is starlark.None, get() should return default.
	req := makeReq(
		map[string]*structpb.Value{
			"spec": structpb.NewNullValue(), // spec is null/None
		},
		map[string]*structpb.Value{},
		nil,
	)
	c := NewCollector(NewConditionCollector(), "test.star", nil)
	globals, err := testBuildGlobals(req, c)
	if err != nil {
		t.Fatal(err)
	}

	thread := new(starlark.Thread)
	result, err := starlark.Call(thread, globals["get"], starlark.Tuple{
		globals["oxr"],
		starlark.String("spec.parameters.region"),
	}, []starlark.Tuple{
		{starlark.String("default"), starlark.String("fallback")},
	})
	if err != nil {
		t.Fatalf("get() error: %v", err)
	}
	if result != starlark.String("fallback") {
		t.Errorf("get(oxr, 'spec.parameters.region', default='fallback') with None intermediate = %v, want 'fallback'", result)
	}
}

// ---------------------------------------------------------------------------
// get_observed builtin tests
// ---------------------------------------------------------------------------

func TestGetObserved(t *testing.T) {
	// Common observed resources for most test cases.
	observedResources := map[string]*fnv1.Resource{
		"my-bucket": {Resource: &structpb.Struct{Fields: map[string]*structpb.Value{
			"status": structpb.NewStructValue(&structpb.Struct{Fields: map[string]*structpb.Value{
				"atProvider": structpb.NewStructValue(&structpb.Struct{Fields: map[string]*structpb.Value{
					"arn": structpb.NewStringValue("arn:aws:s3:::my-bucket"),
				}}),
			}}),
			"metadata": structpb.NewStructValue(&structpb.Struct{Fields: map[string]*structpb.Value{
				"annotations": structpb.NewStructValue(&structpb.Struct{Fields: map[string]*structpb.Value{
					"app.kubernetes.io/name": structpb.NewStringValue("myapp"),
				}}),
			}}),
		}}},
		"null-leaf": {Resource: &structpb.Struct{Fields: map[string]*structpb.Value{
			"status": structpb.NewStructValue(&structpb.Struct{Fields: map[string]*structpb.Value{
				"field": structpb.NewNullValue(),
			}}),
		}}},
		"scalar-res": {Resource: &structpb.Struct{Fields: map[string]*structpb.Value{
			"status": structpb.NewStringValue("just-a-string"),
		}}},
	}

	tests := []struct {
		name     string
		observed map[string]*fnv1.Resource // nil uses observedResources
		args     starlark.Tuple
		kwargs   []starlark.Tuple
		want     starlark.Value
		wantErr  string
	}{
		// OBSV-01: Happy path.
		{
			name: "happy path dot-string",
			args: starlark.Tuple{starlark.String("my-bucket"), starlark.String("status.atProvider.arn")},
			want: starlark.String("arn:aws:s3:::my-bucket"),
		},
		{
			name:   "happy path with explicit default (unused)",
			args:   starlark.Tuple{starlark.String("my-bucket"), starlark.String("status.atProvider.arn")},
			kwargs: []starlark.Tuple{{starlark.String("default"), starlark.String("fallback")}},
			want:   starlark.String("arn:aws:s3:::my-bucket"),
		},
		{
			name:   "kwargs name and path",
			args:   starlark.Tuple{},
			kwargs: []starlark.Tuple{{starlark.String("name"), starlark.String("my-bucket")}, {starlark.String("path"), starlark.String("status.atProvider.arn")}},
			want:   starlark.String("arn:aws:s3:::my-bucket"),
		},
		// OBSV-02: Missing resource.
		{
			name: "missing resource returns None",
			args: starlark.Tuple{starlark.String("nonexistent"), starlark.String("status.atProvider.arn")},
			want: starlark.None,
		},
		{
			name:   "missing resource returns explicit default",
			args:   starlark.Tuple{starlark.String("nonexistent"), starlark.String("status.atProvider.arn")},
			kwargs: []starlark.Tuple{{starlark.String("default"), starlark.String("fallback")}},
			want:   starlark.String("fallback"),
		},
		// OBSV-03: Missing path.
		{
			name: "missing path returns None",
			args: starlark.Tuple{starlark.String("my-bucket"), starlark.String("status.noSuchField")},
			want: starlark.None,
		},
		{
			name:   "missing path returns explicit default",
			args:   starlark.Tuple{starlark.String("my-bucket"), starlark.String("status.atProvider.noSuchField")},
			kwargs: []starlark.Tuple{{starlark.String("default"), starlark.String("fallback")}},
			want:   starlark.String("fallback"),
		},
		{
			name: "deeply nested missing path returns None",
			args: starlark.Tuple{starlark.String("my-bucket"), starlark.String("deeply.nested.missing.path")},
			want: starlark.None,
		},
		{
			name: "None at leaf treated as missing",
			args: starlark.Tuple{starlark.String("null-leaf"), starlark.String("status.field")},
			want: starlark.None,
		},
		// OBSV-04: pathToKeys reuse.
		{
			name: "list path returns same as dot-string",
			args: starlark.Tuple{starlark.String("my-bucket"), starlark.NewList([]starlark.Value{
				starlark.String("status"), starlark.String("atProvider"), starlark.String("arn"),
			})},
			want: starlark.String("arn:aws:s3:::my-bucket"),
		},
		{
			name: "list path with dotted key",
			args: starlark.Tuple{starlark.String("my-bucket"), starlark.NewList([]starlark.Value{
				starlark.String("metadata"), starlark.String("annotations"), starlark.String("app.kubernetes.io/name"),
			})},
			want: starlark.String("myapp"),
		},
		// Validation cases.
		{
			name:    "empty name returns error",
			args:    starlark.Tuple{starlark.String(""), starlark.String("status.field")},
			wantErr: "name must not be empty",
		},
		{
			name:    "empty string path returns error",
			args:    starlark.Tuple{starlark.String("res"), starlark.String("")},
			wantErr: "path must not be empty",
		},
		{
			name:    "empty list path returns error",
			args:    starlark.Tuple{starlark.String("res"), starlark.NewList([]starlark.Value{})},
			wantErr: "path must not be empty",
		},
		{
			name:    "non-string non-list path returns error",
			args:    starlark.Tuple{starlark.String("res"), starlark.MakeInt(123)},
			wantErr: "path must be string or list",
		},
		// Edge cases.
		{
			name: "non-Mapping at intermediate path returns default",
			args: starlark.Tuple{starlark.String("scalar-res"), starlark.String("status.nested.field")},
			want: starlark.None,
		},
		{
			name:   "non-Mapping at intermediate path returns explicit default",
			args:   starlark.Tuple{starlark.String("scalar-res"), starlark.String("status.nested.field")},
			kwargs: []starlark.Tuple{{starlark.String("default"), starlark.String("fallback")}},
			want:   starlark.String("fallback"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			obs := tt.observed
			if obs == nil {
				obs = observedResources
			}

			req := makeReq(
				map[string]*structpb.Value{"apiVersion": structpb.NewStringValue("v1")},
				map[string]*structpb.Value{"apiVersion": structpb.NewStringValue("v1")},
				obs,
			)
			c := NewCollector(NewConditionCollector(), "test.star", nil)
			globals, err := testBuildGlobals(req, c)
			if err != nil {
				t.Fatalf("BuildGlobals error: %v", err)
			}

			getObservedFn := globals["get_observed"]
			thread := new(starlark.Thread)
			result, err := starlark.Call(thread, getObservedFn, tt.args, tt.kwargs)

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("error %q should contain %q", err.Error(), tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("get_observed() error: %v", err)
			}
			if result != tt.want {
				t.Errorf("get_observed() = %v, want %v", result, tt.want)
			}
		})
	}
}
