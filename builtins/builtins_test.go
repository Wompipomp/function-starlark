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
		"set_connection_details", "require_resource", "require_resources",
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
