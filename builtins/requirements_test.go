package builtins

import (
	"strings"
	"testing"

	fnv1 "github.com/crossplane/function-sdk-go/proto/v1"
	"go.starlark.net/starlark"
	"google.golang.org/protobuf/types/known/structpb"
)

// ---------------------------------------------------------------------------
// RequirementsCollector / require_resource tests
// ---------------------------------------------------------------------------

func TestRequirementsCollector_NewEmpty(t *testing.T) {
	rc := NewRequirementsCollector()
	if rc == nil {
		t.Fatal("NewRequirementsCollector returned nil")
	}
	if len(rc.Requirements()) != 0 {
		t.Errorf("Requirements() = %d, want 0", len(rc.Requirements()))
	}
}

func TestRequireResource_MatchName(t *testing.T) {
	rc := NewRequirementsCollector()
	thread := new(starlark.Thread)

	_, err := starlark.Call(thread, rc.RequireResourceBuiltin(), starlark.Tuple{
		starlark.String("my-db"),
		starlark.String("rds.aws.upbound.io/v1beta1"),
		starlark.String("Instance"),
	}, []starlark.Tuple{
		{starlark.String("match_name"), starlark.String("my-database")},
	})
	if err != nil {
		t.Fatalf("require_resource error: %v", err)
	}

	reqs := rc.Requirements()
	if len(reqs) != 1 {
		t.Fatalf("Requirements() = %d, want 1", len(reqs))
	}
	r := reqs[0]
	if r.Name != "my-db" {
		t.Errorf("Name = %q, want 'my-db'", r.Name)
	}
	if r.APIVersion != "rds.aws.upbound.io/v1beta1" {
		t.Errorf("APIVersion = %q", r.APIVersion)
	}
	if r.Kind != "Instance" {
		t.Errorf("Kind = %q, want 'Instance'", r.Kind)
	}
	if r.MatchName != "my-database" {
		t.Errorf("MatchName = %q, want 'my-database'", r.MatchName)
	}
	if len(r.MatchLabels) != 0 {
		t.Errorf("MatchLabels = %v, want empty", r.MatchLabels)
	}
}

func TestRequireResource_MatchLabels(t *testing.T) {
	rc := NewRequirementsCollector()
	thread := new(starlark.Thread)

	labels := new(starlark.Dict)
	_ = labels.SetKey(starlark.String("app"), starlark.String("db"))

	_, err := starlark.Call(thread, rc.RequireResourceBuiltin(), starlark.Tuple{
		starlark.String("my-db"),
		starlark.String("rds.aws.upbound.io/v1beta1"),
		starlark.String("Instance"),
	}, []starlark.Tuple{
		{starlark.String("match_labels"), labels},
	})
	if err != nil {
		t.Fatalf("require_resource error: %v", err)
	}

	r := rc.Requirements()[0]
	if r.MatchName != "" {
		t.Errorf("MatchName = %q, want empty", r.MatchName)
	}
	if r.MatchLabels["app"] != "db" {
		t.Errorf("MatchLabels[app] = %q, want 'db'", r.MatchLabels["app"])
	}
}

func TestRequireResource_BothMatchNameAndLabels_NameWins(t *testing.T) {
	rc := NewRequirementsCollector()
	thread := new(starlark.Thread)

	labels := new(starlark.Dict)
	_ = labels.SetKey(starlark.String("app"), starlark.String("db"))

	_, err := starlark.Call(thread, rc.RequireResourceBuiltin(), starlark.Tuple{
		starlark.String("my-db"),
		starlark.String("rds.aws.upbound.io/v1beta1"),
		starlark.String("Instance"),
	}, []starlark.Tuple{
		{starlark.String("match_name"), starlark.String("my-database")},
		{starlark.String("match_labels"), labels},
	})
	if err != nil {
		t.Fatalf("require_resource error: %v", err)
	}

	r := rc.Requirements()[0]
	if r.MatchName != "my-database" {
		t.Errorf("MatchName = %q, want 'my-database'", r.MatchName)
	}
	if len(r.MatchLabels) != 0 {
		t.Errorf("MatchLabels should be empty when match_name provided, got %v", r.MatchLabels)
	}
}

func TestRequireResource_NeitherMatchNameNorLabels_Error(t *testing.T) {
	rc := NewRequirementsCollector()
	thread := new(starlark.Thread)

	_, err := starlark.Call(thread, rc.RequireResourceBuiltin(), starlark.Tuple{
		starlark.String("my-db"),
		starlark.String("rds.aws.upbound.io/v1beta1"),
		starlark.String("Instance"),
	}, nil)
	if err == nil {
		t.Fatal("require_resource without match_name or match_labels should error")
	}
	if !strings.Contains(err.Error(), "match_name") || !strings.Contains(err.Error(), "match_labels") {
		t.Errorf("error %q should mention match_name and match_labels", err.Error())
	}
}

// ---------------------------------------------------------------------------
// require_resources tests
// ---------------------------------------------------------------------------

func TestRequireResources_MatchLabels(t *testing.T) {
	rc := NewRequirementsCollector()
	thread := new(starlark.Thread)

	labels := new(starlark.Dict)
	_ = labels.SetKey(starlark.String("team"), starlark.String("platform"))

	_, err := starlark.Call(thread, rc.RequireResourcesBuiltin(), starlark.Tuple{
		starlark.String("all-dbs"),
		starlark.String("rds.aws.upbound.io/v1beta1"),
		starlark.String("Instance"),
		labels,
	}, nil)
	if err != nil {
		t.Fatalf("require_resources error: %v", err)
	}

	reqs := rc.Requirements()
	if len(reqs) != 1 {
		t.Fatalf("Requirements() = %d, want 1", len(reqs))
	}
	r := reqs[0]
	if r.Name != "all-dbs" {
		t.Errorf("Name = %q, want 'all-dbs'", r.Name)
	}
	if r.MatchLabels["team"] != "platform" {
		t.Errorf("MatchLabels[team] = %q, want 'platform'", r.MatchLabels["team"])
	}
}

func TestRequireResources_MatchLabelsRequired(t *testing.T) {
	rc := NewRequirementsCollector()
	thread := new(starlark.Thread)

	// Missing match_labels (required positional)
	_, err := starlark.Call(thread, rc.RequireResourcesBuiltin(), starlark.Tuple{
		starlark.String("all-dbs"),
		starlark.String("rds.aws.upbound.io/v1beta1"),
		starlark.String("Instance"),
	}, nil)
	if err == nil {
		t.Fatal("require_resources without match_labels should error")
	}
}

// ---------------------------------------------------------------------------
// Requirements copy-out test
// ---------------------------------------------------------------------------

func TestRequirements_ReturnsCopy(t *testing.T) {
	rc := NewRequirementsCollector()
	thread := new(starlark.Thread)

	labels := new(starlark.Dict)
	_ = labels.SetKey(starlark.String("app"), starlark.String("db"))

	_, _ = starlark.Call(thread, rc.RequireResourceBuiltin(), starlark.Tuple{
		starlark.String("my-db"),
		starlark.String("v1"),
		starlark.String("Instance"),
	}, []starlark.Tuple{
		{starlark.String("match_name"), starlark.String("db")},
	})

	r1 := rc.Requirements()
	r2 := rc.Requirements()
	r1[0].Name = "modified"
	if r2[0].Name == "modified" {
		t.Error("Requirements() should return a copy")
	}
}

// ---------------------------------------------------------------------------
// ApplyRequirements tests
// ---------------------------------------------------------------------------

func TestApplyRequirements_Empty(t *testing.T) {
	rsp := &fnv1.RunFunctionResponse{}
	ApplyRequirements(rsp, nil)
	if rsp.Requirements != nil {
		t.Error("ApplyRequirements with empty reqs should not set Requirements")
	}
}

func TestApplyRequirements_MatchName(t *testing.T) {
	rsp := &fnv1.RunFunctionResponse{}
	ApplyRequirements(rsp, []CollectedRequirement{
		{
			Name:       "my-db",
			APIVersion: "rds.aws.upbound.io/v1beta1",
			Kind:       "Instance",
			MatchName:  "my-database",
		},
	})

	if rsp.Requirements == nil {
		t.Fatal("Requirements should be set")
	}
	sel, ok := rsp.Requirements.Resources["my-db"]
	if !ok {
		t.Fatal("missing resource selector 'my-db'")
	}
	if sel.ApiVersion != "rds.aws.upbound.io/v1beta1" {
		t.Errorf("ApiVersion = %q", sel.ApiVersion)
	}
	if sel.Kind != "Instance" {
		t.Errorf("Kind = %q", sel.Kind)
	}
	if sel.GetMatchName() != "my-database" {
		t.Errorf("MatchName = %q, want 'my-database'", sel.GetMatchName())
	}
}

func TestApplyRequirements_MatchLabels(t *testing.T) {
	rsp := &fnv1.RunFunctionResponse{}
	ApplyRequirements(rsp, []CollectedRequirement{
		{
			Name:        "all-dbs",
			APIVersion:  "rds.aws.upbound.io/v1beta1",
			Kind:        "Instance",
			MatchLabels: map[string]string{"team": "platform"},
		},
	})

	sel := rsp.Requirements.Resources["all-dbs"]
	ml := sel.GetMatchLabels()
	if ml == nil {
		t.Fatal("MatchLabels should be set")
	}
	if ml.Labels["team"] != "platform" {
		t.Errorf("Labels[team] = %q, want 'platform'", ml.Labels["team"])
	}
}

func TestApplyRequirements_CreatesRequirementsIfNil(t *testing.T) {
	rsp := &fnv1.RunFunctionResponse{}
	ApplyRequirements(rsp, []CollectedRequirement{
		{Name: "x", APIVersion: "v1", Kind: "K", MatchName: "n"},
	})

	if rsp.Requirements == nil {
		t.Fatal("Requirements should be created")
	}
	if rsp.Requirements.Resources == nil {
		t.Fatal("Resources map should be created")
	}
}

// ---------------------------------------------------------------------------
// buildExtraResourcesDict tests
// ---------------------------------------------------------------------------

func TestBuildExtraResourcesDict_NilRequest(t *testing.T) {
	req := &fnv1.RunFunctionRequest{}
	d, err := buildExtraResourcesDict(req)
	if err != nil {
		t.Fatalf("error: %v", err)
	}

	// Should be frozen empty dict.
	if d.Len() != 0 {
		t.Errorf("Len = %d, want 0", d.Len())
	}

	// Should be frozen.
	if err := d.SetKey(starlark.String("x"), starlark.None); err == nil {
		t.Error("dict should be frozen")
	}
}

func TestBuildExtraResourcesDict_EmptyItems(t *testing.T) {
	req := &fnv1.RunFunctionRequest{
		RequiredResources: map[string]*fnv1.Resources{
			"my-db": {Items: nil},
		},
	}

	d, err := buildExtraResourcesDict(req)
	if err != nil {
		t.Fatalf("error: %v", err)
	}

	v, found, err := d.Get(starlark.String("my-db"))
	if err != nil {
		t.Fatalf("Get error: %v", err)
	}
	if !found {
		t.Fatal("key 'my-db' not found")
	}
	if v != starlark.None {
		t.Errorf("value = %v, want None for empty items", v)
	}
}

func TestBuildExtraResourcesDict_Populated(t *testing.T) {
	req := &fnv1.RunFunctionRequest{
		RequiredResources: map[string]*fnv1.Resources{
			"my-db": {
				Items: []*fnv1.Resource{
					{
						Resource: &structpb.Struct{
							Fields: map[string]*structpb.Value{
								"apiVersion": structpb.NewStringValue("rds.aws.upbound.io/v1beta1"),
								"kind":       structpb.NewStringValue("Instance"),
							},
						},
					},
				},
			},
		},
	}

	d, err := buildExtraResourcesDict(req)
	if err != nil {
		t.Fatalf("error: %v", err)
	}

	v, found, err := d.Get(starlark.String("my-db"))
	if err != nil {
		t.Fatalf("Get error: %v", err)
	}
	if !found {
		t.Fatal("key 'my-db' not found")
	}

	list, ok := v.(*starlark.List)
	if !ok {
		t.Fatalf("value = %T, want *starlark.List", v)
	}
	if list.Len() != 1 {
		t.Fatalf("list.Len() = %d, want 1", list.Len())
	}

	// Should be frozen.
	if err := d.SetKey(starlark.String("x"), starlark.None); err == nil {
		t.Error("outer dict should be frozen")
	}
}

func TestBuildExtraResourcesDict_FallbackToExtraResources(t *testing.T) {
	// When RequiredResources is nil, fall back to deprecated ExtraResources.
	req := &fnv1.RunFunctionRequest{
		ExtraResources: map[string]*fnv1.Resources{
			"legacy": {
				Items: []*fnv1.Resource{
					{
						Resource: &structpb.Struct{
							Fields: map[string]*structpb.Value{
								"kind": structpb.NewStringValue("Legacy"),
							},
						},
					},
				},
			},
		},
	}

	d, err := buildExtraResourcesDict(req)
	if err != nil {
		t.Fatalf("error: %v", err)
	}

	v, found, _ := d.Get(starlark.String("legacy"))
	if !found {
		t.Fatal("key 'legacy' not found (deprecated fallback)")
	}
	list := v.(*starlark.List)
	if list.Len() != 1 {
		t.Errorf("list.Len() = %d, want 1", list.Len())
	}
}

func TestBuildExtraResourcesDict_RequiredTakesPrecedence(t *testing.T) {
	// When both RequiredResources and ExtraResources are set,
	// RequiredResources takes precedence.
	req := &fnv1.RunFunctionRequest{
		RequiredResources: map[string]*fnv1.Resources{
			"new-field": {
				Items: []*fnv1.Resource{
					{
						Resource: &structpb.Struct{
							Fields: map[string]*structpb.Value{
								"source": structpb.NewStringValue("required"),
							},
						},
					},
				},
			},
		},
		ExtraResources: map[string]*fnv1.Resources{
			"old-field": {
				Items: []*fnv1.Resource{
					{
						Resource: &structpb.Struct{
							Fields: map[string]*structpb.Value{
								"source": structpb.NewStringValue("extra"),
							},
						},
					},
				},
			},
		},
	}

	d, err := buildExtraResourcesDict(req)
	if err != nil {
		t.Fatalf("error: %v", err)
	}

	// Should have new-field from RequiredResources.
	_, found, _ := d.Get(starlark.String("new-field"))
	if !found {
		t.Error("key 'new-field' not found from RequiredResources")
	}

	// Should NOT have old-field from ExtraResources since RequiredResources is present.
	_, found, _ = d.Get(starlark.String("old-field"))
	if found {
		t.Error("key 'old-field' should not be present when RequiredResources is used")
	}
}

func TestBuildExtraResourcesDict_FrozenResourceDicts(t *testing.T) {
	req := &fnv1.RunFunctionRequest{
		RequiredResources: map[string]*fnv1.Resources{
			"my-db": {
				Items: []*fnv1.Resource{
					{
						Resource: &structpb.Struct{
							Fields: map[string]*structpb.Value{
								"apiVersion": structpb.NewStringValue("v1"),
							},
						},
					},
				},
			},
		},
	}

	d, err := buildExtraResourcesDict(req)
	if err != nil {
		t.Fatalf("error: %v", err)
	}

	v, _, _ := d.Get(starlark.String("my-db"))
	list := v.(*starlark.List)

	// List should be frozen.
	if err := list.Append(starlark.None); err == nil {
		t.Error("resource list should be frozen")
	}
}

func TestBuildExtraResourcesDict_MultipleResources(t *testing.T) {
	req := &fnv1.RunFunctionRequest{
		RequiredResources: map[string]*fnv1.Resources{
			"dbs": {
				Items: []*fnv1.Resource{
					{Resource: &structpb.Struct{Fields: map[string]*structpb.Value{"name": structpb.NewStringValue("db1")}}},
					{Resource: &structpb.Struct{Fields: map[string]*structpb.Value{"name": structpb.NewStringValue("db2")}}},
					{Resource: &structpb.Struct{Fields: map[string]*structpb.Value{"name": structpb.NewStringValue("db3")}}},
				},
			},
		},
	}

	d, err := buildExtraResourcesDict(req)
	if err != nil {
		t.Fatalf("error: %v", err)
	}

	v, _, _ := d.Get(starlark.String("dbs"))
	list := v.(*starlark.List)
	if list.Len() != 3 {
		t.Errorf("list.Len() = %d, want 3", list.Len())
	}
}
