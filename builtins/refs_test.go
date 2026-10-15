package builtins

import (
	"strings"
	"testing"

	fnv1 "github.com/crossplane/function-sdk-go/proto/v1"
	"go.starlark.net/starlark"
	"go.starlark.net/syntax"
	"google.golang.org/protobuf/types/known/structpb"
)

// execRefsScript builds real globals (observed + extra resources) and runs src.
// It returns the script globals and the collectors for assertions.
func execRefsScript(t *testing.T, src string) (starlark.StringDict, *Collector, *RequirementsCollector) {
	t.Helper()
	req := makeReq(
		map[string]*structpb.Value{"apiVersion": structpb.NewStringValue("v1")},
		map[string]*structpb.Value{"apiVersion": structpb.NewStringValue("v1")},
		map[string]*fnv1.Resource{
			"db": {Resource: &structpb.Struct{Fields: map[string]*structpb.Value{
				"kind": structpb.NewStringValue("Database"),
				"status": structpb.NewStructValue(&structpb.Struct{Fields: map[string]*structpb.Value{
					"atProvider": structpb.NewStructValue(&structpb.Struct{Fields: map[string]*structpb.Value{
						"address": structpb.NewStringValue("db.example.internal"),
					}}),
					"conditions": structpb.NewListValue(&structpb.ListValue{Values: []*structpb.Value{
						structpb.NewStructValue(&structpb.Struct{Fields: map[string]*structpb.Value{
							"type":   structpb.NewStringValue("Ready"),
							"status": structpb.NewStringValue("True"),
						}}),
					}}),
				}}),
			}}},
		},
	)
	req.RequiredResources = map[string]*fnv1.Resources{
		"ConfigMap/ns1/my-config": {Items: []*fnv1.Resource{{Resource: &structpb.Struct{Fields: map[string]*structpb.Value{
			"data": structpb.NewStructValue(&structpb.Struct{Fields: map[string]*structpb.Value{
				"host": structpb.NewStringValue("db.example"),
			}}),
		}}}}},
		"Secret//app=x,tier=db": {Items: []*fnv1.Resource{
			{Resource: &structpb.Struct{Fields: map[string]*structpb.Value{"kind": structpb.NewStringValue("Secret")}}},
			{Resource: &structpb.Struct{Fields: map[string]*structpb.Value{"kind": structpb.NewStringValue("Secret")}}},
		}},
	}

	observed, err := BuildObservedDict(req)
	if err != nil {
		t.Fatalf("BuildObservedDict: %v", err)
	}
	c := NewCollector(NewConditionCollector(), "refs.star", nil, observed)
	rc := NewRequirementsCollector()
	globals, err := BuildGlobals(req, c, c.cc, NewConnectionCollector(), rc, NewTTLCollector(), observed)
	if err != nil {
		t.Fatalf("BuildGlobals: %v", err)
	}
	out, err := starlark.ExecFileOptions(&syntax.FileOptions{}, &starlark.Thread{Name: "refs"}, "refs.star", src, globals)
	if err != nil {
		t.Fatalf("script error: %v", err)
	}
	return out, c, rc
}

func wantStr(t *testing.T, g starlark.StringDict, key, want string) {
	t.Helper()
	v, ok := g[key]
	if !ok {
		t.Fatalf("%s not set", key)
	}
	s, ok := v.(starlark.String)
	if !ok {
		t.Fatalf("%s = %v (%s), want string", key, v, v.Type())
	}
	if string(s) != want {
		t.Errorf("%s = %q, want %q", key, string(s), want)
	}
}

func TestRefHandles_ObservedAccessors(t *testing.T) {
	g, _, _ := execRefsScript(t, `
db = Resource("db", {"apiVersion": "v1", "kind": "Database", "metadata": {"name": "db-1"}})

by_ref   = get_observed(db, "status.atProvider.address", "")
by_name  = get_observed("db", "status.atProvider.address", "")
by_kw    = get_observed(name="db", path="status.atProvider.address")
whole    = get_observed(db)
whole_id = whole.status.atProvider.address
missing  = get_observed(db, "status.atProvider.nope", "dflt")
absent   = get_observed("ghost")
absent_d = get_observed("ghost", default="none")
seen     = is_observed(db)
cond     = get_condition(db, "Ready")["status"]
legacy   = observed_body(db, {}).status.atProvider.address
`)
	wantStr(t, g, "by_ref", "db.example.internal")
	wantStr(t, g, "by_name", "db.example.internal")
	wantStr(t, g, "by_kw", "db.example.internal")
	wantStr(t, g, "whole_id", "db.example.internal")
	wantStr(t, g, "missing", "dflt")
	if g["absent"] != starlark.None {
		t.Errorf("absent = %v, want None", g["absent"])
	}
	wantStr(t, g, "absent_d", "none")
	if g["seen"] != starlark.True {
		t.Errorf("is_observed(ref) = %v, want True", g["seen"])
	}
	wantStr(t, g, "cond", "True")
	wantStr(t, g, "legacy", "db.example.internal")
}

func TestRefHandles_ObservedBodyDeprecationWarnsOnce(t *testing.T) {
	_, c, _ := execRefsScript(t, `
observed_body("db")
observed_body("db")
`)
	var n int
	for _, e := range c.cc.Events() {
		if e.Severity == "Warning" && strings.Contains(e.Message, "observed_body() is deprecated") {
			n++
		}
	}
	if n != 1 {
		t.Errorf("deprecation warnings = %d, want 1", n)
	}
}

func TestRefHandles_RejectsBadNames(t *testing.T) {
	for _, src := range []string{
		`get_observed(42, "a")`,
		`get_observed("", "a")`,
		`get_manifest(None)`,
		`get_extra_resource([])`,
	} {
		req := makeReq(nil, nil, nil)
		c := NewCollector(NewConditionCollector(), "t.star", nil, nil)
		globals, err := testBuildGlobals(req, c)
		if err != nil {
			t.Fatal(err)
		}
		_, err = starlark.ExecFileOptions(&syntax.FileOptions{}, &starlark.Thread{}, "t.star", src, globals)
		if err == nil || !strings.Contains(err.Error(), "name must") {
			t.Errorf("%s: err = %v, want name validation error", src, err)
		}
	}
}

func TestGetManifest(t *testing.T) {
	g, _, _ := execRefsScript(t, `
res = Resource("res", {
    "apiVersion": "api.group/v1",
    "kind": "Kind",
    "metadata": {"name": "my-resource", "namespace": "my-namespace", "labels": {"a": "b"}},
    "spec": {"foo": "bar", "gone": None},
}, labels={"extra": "yes"})

name      = get_manifest(res, "metadata.name")
ns_dflt   = get_manifest(res, "metadata.missing", "fallback")
whole     = get_manifest(res)
whole_ns  = whole.metadata.namespace
merged    = get_manifest(res, "metadata.labels.extra")
compacted = get_manifest(res, "spec.gone", "stripped")
by_name   = get_manifest("res", "spec.foo")
unknown   = get_manifest("ghost", "metadata.name", "none")
same      = get_manifest(res) == get_manifest(res)

skipped = Resource("skipped", None, when=When(False, "off", False))
skipped_m = get_manifest(skipped, default="no-manifest")

Resource("res", {"apiVersion": "api.group/v1", "kind": "Kind", "metadata": {"name": "renamed"}})
after_rewrite = get_manifest(res, "metadata.name")
`)
	wantStr(t, g, "name", "my-resource")
	wantStr(t, g, "ns_dflt", "fallback")
	wantStr(t, g, "whole_ns", "my-namespace")
	wantStr(t, g, "merged", "yes")
	wantStr(t, g, "compacted", "stripped")
	wantStr(t, g, "by_name", "bar")
	wantStr(t, g, "unknown", "none")
	if g["same"] != starlark.True {
		t.Errorf("repeated get_manifest should return the cached view")
	}
	wantStr(t, g, "skipped_m", "no-manifest")
	wantStr(t, g, "after_rewrite", "renamed")
}

func TestRequireExtraResource_ReturnsHandle(t *testing.T) {
	g, _, rc := execRefsScript(t, `
# legacy 3-positional form keeps working and now returns a handle
legacy = require_extra_resource("cfg", "v1", "ConfigMap", match_name="legacy-cfg")
legacy_name = legacy.name

# name omitted: derived from kind/namespace/selector and resolvable immediately
cfg = require_extra_resource("v1", "ConfigMap", match_name="my-config", namespace="ns1")
cfg_name = cfg.name
host = get_extra_resource(cfg, "data.host", "")
host_whole = get_extra_resource(cfg).data.host

kw = require_extra_resource(apiVersion="v1", kind="ConfigMap", match_name="kw-cfg")
kw_name = kw.name

secrets = require_extra_resources("v1", "Secret", match_labels={"tier": "db", "app": "x"})
secrets_name = secrets.name
n_secrets = len(get_extra_resources(secrets))
`)
	wantStr(t, g, "legacy_name", "cfg")
	wantStr(t, g, "cfg_name", "ConfigMap/ns1/my-config")
	wantStr(t, g, "host", "db.example")
	wantStr(t, g, "host_whole", "db.example")
	wantStr(t, g, "kw_name", "ConfigMap//kw-cfg")
	wantStr(t, g, "secrets_name", "Secret//app=x,tier=db")
	if n, _ := starlark.AsInt32(g["n_secrets"]); n != 2 {
		t.Errorf("n_secrets = %v, want 2", g["n_secrets"])
	}

	reqs := rc.Requirements()
	if len(reqs) != 4 {
		t.Fatalf("requirements = %d, want 4", len(reqs))
	}
	if reqs[1].Name != "ConfigMap/ns1/my-config" || reqs[1].APIVersion != "v1" || reqs[1].Kind != "ConfigMap" || reqs[1].Namespace != "ns1" {
		t.Errorf("derived requirement = %+v", reqs[1])
	}
	if reqs[3].MatchLabels["app"] != "x" || reqs[3].Kind != "Secret" {
		t.Errorf("label requirement = %+v", reqs[3])
	}
}

func TestRequireExtraResource_MissingIdentity(t *testing.T) {
	for _, src := range []string{
		`require_extra_resource("v1", match_name="x")`,
		`require_extra_resource(kind="ConfigMap", match_name="x")`,
		`require_extra_resources("v1", "Secret")`,
	} {
		req := makeReq(nil, nil, nil)
		c := NewCollector(NewConditionCollector(), "t.star", nil, nil)
		globals, err := testBuildGlobals(req, c)
		if err != nil {
			t.Fatal(err)
		}
		_, err = starlark.ExecFileOptions(&syntax.FileOptions{}, &starlark.Thread{}, "t.star", src, globals)
		if err == nil {
			t.Errorf("%s: expected error", src)
		}
	}
}
