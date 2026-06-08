package main

// In-process smoke tests for the Starlark scripts embedded in the e2e
// compositions under test/e2e/. These do NOT replace the cluster e2e suite
// (run-tests.sh); they catch script/runtime drift (syntax errors, wrong
// builtin signatures, fatal assertions) without needing a kind cluster.
//
// Only self-contained compositions are covered: anything that needs a real
// OCI registry or ConfigMap filesystem mounts is exercised exclusively by
// the cluster suite.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/crossplane/function-sdk-go/logging"
	fnv1 "github.com/crossplane/function-sdk-go/proto/v1"
	"github.com/crossplane/function-sdk-go/resource"
	"google.golang.org/protobuf/types/known/structpb"
	"sigs.k8s.io/yaml"

	"github.com/wompipomp/function-starlark/runtime"
)

// e2eStarlarkSteps extracts the StarlarkInput of every function-starlark
// pipeline step from an e2e composition YAML, keyed by step name.
func e2eStarlarkSteps(t *testing.T, file string) map[string]*structpb.Struct {
	t.Helper()

	data, err := os.ReadFile(filepath.Join("test", "e2e", file)) //nolint:gosec // fixed in-repo test fixture path
	if err != nil {
		t.Fatalf("reading %s: %v", file, err)
	}

	var comp struct {
		Spec struct {
			Pipeline []struct {
				Step        string `json:"step"`
				FunctionRef struct {
					Name string `json:"name"`
				} `json:"functionRef"`
				Input map[string]any `json:"input"`
			} `json:"pipeline"`
		} `json:"spec"`
	}
	if err := yaml.Unmarshal(data, &comp); err != nil {
		t.Fatalf("parsing %s: %v", file, err)
	}

	steps := make(map[string]*structpb.Struct)
	for _, s := range comp.Spec.Pipeline {
		if s.FunctionRef.Name != "function-starlark" {
			continue
		}
		j, err := json.Marshal(s.Input)
		if err != nil {
			t.Fatalf("marshaling input of step %q in %s: %v", s.Step, file, err)
		}
		steps[s.Step] = resource.MustStructJSON(string(j))
	}
	if len(steps) == 0 {
		t.Fatalf("no function-starlark steps found in %s", file)
	}
	return steps
}

// runE2EStep executes one extracted step and fails the test on any fatal result.
func runE2EStep(t *testing.T, f *Function, req *fnv1.RunFunctionRequest) *fnv1.RunFunctionResponse {
	t.Helper()
	rsp, err := f.RunFunction(context.Background(), req)
	if err != nil {
		t.Fatalf("RunFunction: %v", err)
	}
	for _, r := range rsp.GetResults() {
		if r.GetSeverity() == fnv1.Severity_SEVERITY_FATAL {
			t.Fatalf("fatal result: %s", r.GetMessage())
		}
	}
	return rsp
}

func mustOXR(t *testing.T, spec string) *fnv1.State {
	t.Helper()
	return &fnv1.State{
		Composite: &fnv1.Resource{
			Resource: resource.MustStructJSON(fmt.Sprintf(
				`{"apiVersion":"e2e.fn-starlark.io/v1","kind":"XTest","metadata":{"name":"smoke"},"spec":%s}`, spec)),
		},
	}
}

// statusTestField navigates rsp.Desired.Composite.Resource.status.test.<key>.
func statusTestField(t *testing.T, rsp *fnv1.RunFunctionResponse, key string) any {
	t.Helper()
	m := rsp.GetDesired().GetComposite().GetResource().AsMap()
	status, _ := m["status"].(map[string]any)
	test, _ := status["test"].(map[string]any)
	return test[key]
}

func TestE2EBuiltinsScript(t *testing.T) {
	rt := runtime.NewRuntime(logging.NewNopLogger())
	f := &Function{log: logging.NewNopLogger(), runtime: rt}
	steps := e2eStarlarkSteps(t, "composition-builtins.yaml")

	rsp := runE2EStep(t, f, &fnv1.RunFunctionRequest{
		Input:    steps["test-all-builtins"],
		Observed: mustOXR(t, `{"region":"us-east-1","environment":"prod","externalConfig":"test-value"}`),
	})

	// set_response_ttl("5s") must win over the sequencing TTL.
	if got := rsp.GetMeta().GetTtl().AsDuration(); got != 5*time.Second {
		t.Errorf("response TTL = %v, want 5s (set_response_ttl)", got)
	}

	// external_name kwarg lands as the annotation; the conflict kwarg wins.
	res := rsp.GetDesired().GetResources()
	for name, want := range map[string]string{
		"resource-ext-name":     "e2e-external-id",
		"resource-ext-conflict": "kwarg-wins",
	} {
		body := res[name].GetResource().AsMap()
		meta, _ := body["metadata"].(map[string]any)
		ann, _ := meta["annotations"].(map[string]any)
		if got := ann["crossplane.io/external-name"]; got != want {
			t.Errorf("%s external-name annotation = %v, want %q", name, got, want)
		}
	}

	// The conflict must produce a Warning result mentioning the override.
	foundWarning := false
	for _, r := range rsp.GetResults() {
		if strings.Contains(r.GetMessage(), "overrides annotation") {
			foundWarning = true
		}
	}
	if !foundWarning {
		t.Errorf("no 'overrides annotation' warning result for external_name conflict")
	}

	// First reconcile: nothing observed yet, helpers report their defaults.
	if got := statusTestField(t, rsp, "isObservedA"); got != false {
		t.Errorf("isObservedA on first reconcile = %v, want false", got)
	}
	if got := statusTestField(t, rsp, "condAReadyStatus"); got != "absent" {
		t.Errorf("condAReadyStatus on first reconcile = %v, want absent", got)
	}
}

func TestE2ECompositeReadyScript(t *testing.T) {
	rt := runtime.NewRuntime(logging.NewNopLogger())
	f := &Function{log: logging.NewNopLogger(), runtime: rt}
	steps := e2eStarlarkSteps(t, "composition-composite-ready.yaml")

	wantReady := map[string]struct {
		resource string
		ready    fnv1.Ready
	}{
		"gate":        {},
		"optional":    {},
		"explicit":    {},
		"ready-false": {resource: "never-ready", ready: fnv1.Ready_READY_FALSE},
		"ready-true":  {resource: "forced-ready", ready: fnv1.Ready_READY_TRUE},
	}

	for mode, want := range wantReady {
		t.Run(mode, func(t *testing.T) {
			rsp := runE2EStep(t, f, &fnv1.RunFunctionRequest{
				Input:    steps["composite-ready-scenarios"],
				Observed: mustOXR(t, fmt.Sprintf(`{"mode":%q}`, mode)),
			})
			if want.resource == "" {
				return
			}
			res, ok := rsp.GetDesired().GetResources()[want.resource]
			if !ok {
				t.Fatalf("desired resource %q missing", want.resource)
			}
			if res.GetReady() != want.ready {
				t.Errorf("%s ready = %v, want %v", want.resource, res.GetReady(), want.ready)
			}
		})
	}
}

func TestE2EContextEnvScript(t *testing.T) {
	rt := runtime.NewRuntime(logging.NewNopLogger())
	f := &Function{log: logging.NewNopLogger(), runtime: rt}
	steps := e2eStarlarkSteps(t, "composition-context-env.yaml")

	// Step 1 seeds the context (well-known environment key + custom key).
	rsp1 := runE2EStep(t, f, &fnv1.RunFunctionRequest{
		Input:    steps["seed-context"],
		Observed: mustOXR(t, `{}`),
	})

	// Step 2 receives step 1's context (as Crossplane would carry it).
	rsp2 := runE2EStep(t, f, &fnv1.RunFunctionRequest{
		Input:    steps["read-env"],
		Observed: mustOXR(t, `{}`),
		Context:  rsp1.GetContext(),
	})

	for key, want := range map[string]any{
		"envRegion":        "eu-central-1",
		"envTier":          "gold",
		"envZone":          "a",
		"crossStepContext": "from-step-one",
	} {
		if got := statusTestField(t, rsp2, key); got != want {
			t.Errorf("status test.%s = %v, want %v", key, got, want)
		}
	}
}

func TestE2EExtraResourcesScript(t *testing.T) {
	rt := runtime.NewRuntime(logging.NewNopLogger())
	f := &Function{log: logging.NewNopLogger(), runtime: rt}
	steps := e2eStarlarkSteps(t, "composition-extra-resources.yaml")

	// Iteration 1: no extra resources provided yet -> requirements returned.
	rsp1 := runE2EStep(t, f, &fnv1.RunFunctionRequest{
		Input:    steps["test-extra-resources"],
		Observed: mustOXR(t, `{}`),
	})
	reqs := rsp1.GetRequirements().GetResources()
	for _, key := range []string{"xrd", "nops"} {
		if _, ok := reqs[key]; !ok {
			t.Errorf("requirement %q missing from response (got %v)", key, reqs)
		}
	}
	if got := statusTestField(t, rsp1, "extraResourcesReady"); got != false {
		t.Errorf("extraResourcesReady on iteration 1 = %v, want false", got)
	}

	// Iteration 2: Crossplane fulfilled the requirements.
	rsp2 := runE2EStep(t, f, &fnv1.RunFunctionRequest{
		Input:    steps["test-extra-resources"],
		Observed: mustOXR(t, `{}`),
		RequiredResources: map[string]*fnv1.Resources{
			"xrd": {Items: []*fnv1.Resource{{
				Resource: resource.MustStructJSON(`{"apiVersion":"apiextensions.crossplane.io/v1","kind":"CompositeResourceDefinition","metadata":{"name":"xtests.e2e.fn-starlark.io"},"spec":{"group":"e2e.fn-starlark.io"}}`),
			}}},
			"nops": {Items: []*fnv1.Resource{
				{Resource: resource.MustStructJSON(`{"apiVersion":"nop.crossplane.io/v1alpha1","kind":"NopResource","metadata":{"name":"e2e-extra-nop-2","labels":{"e2e-extra":"true"}}}`)},
				{Resource: resource.MustStructJSON(`{"apiVersion":"nop.crossplane.io/v1alpha1","kind":"NopResource","metadata":{"name":"e2e-extra-nop-1","labels":{"e2e-extra":"true"}}}`)},
			}},
		},
	})

	for key, want := range map[string]any{
		"extraXrdGroup":       "e2e.fn-starlark.io",
		"extraXrdKind":        "CompositeResourceDefinition",
		"extraNopCount":       float64(2),
		"extraNopNamesSorted": "e2e-extra-nop-1,e2e-extra-nop-2",
		"extraRawHasXrd":      true,
		"extraResourcesReady": true,
	} {
		if got := statusTestField(t, rsp2, key); got != want {
			t.Errorf("status test.%s = %v (%T), want %v", key, got, got, want)
		}
	}
}

func TestE2EFieldpathDepScript(t *testing.T) {
	rt := runtime.NewRuntime(logging.NewNopLogger())
	f := &Function{log: logging.NewNopLogger(), runtime: rt}
	steps := e2eStarlarkSteps(t, "composition-fieldpath-dep.yaml")

	// Phase 1: no signal -> producer emitted, consumer deferred on field path.
	rsp1 := runE2EStep(t, f, &fnv1.RunFunctionRequest{
		Input:    steps["test-fieldpath-dep"],
		Observed: mustOXR(t, `{}`),
	})
	res1 := rsp1.GetDesired().GetResources()
	if _, ok := res1["producer"]; !ok {
		t.Errorf("phase 1: producer missing from desired state")
	}
	if _, ok := res1["consumer"]; ok {
		t.Errorf("phase 1: consumer present in desired state, want deferred")
	}

	// Phase 2: signal set AND producer observed with the annotation -> the
	// field-path dependency is met and the consumer is emitted.
	observed := mustOXR(t, `{"signal":"go"}`)
	observed.Resources = map[string]*fnv1.Resource{
		"producer": {Resource: resource.MustStructJSON(`{
			"apiVersion":"nop.crossplane.io/v1alpha1","kind":"NopResource",
			"metadata":{"name":"producer","annotations":{"e2e-signal":"go"}},
			"status":{"conditions":[
				{"type":"Ready","status":"True"},
				{"type":"Synced","status":"True"}]}}`)},
	}
	rsp2 := runE2EStep(t, f, &fnv1.RunFunctionRequest{
		Input:    steps["test-fieldpath-dep"],
		Observed: observed,
	})
	if _, ok := rsp2.GetDesired().GetResources()["consumer"]; !ok {
		t.Errorf("phase 2: consumer missing from desired state, want emitted")
	}
}

func TestE2EUsageV2Script(t *testing.T) {
	rt := runtime.NewRuntime(logging.NewNopLogger())
	f := &Function{log: logging.NewNopLogger(), runtime: rt}
	steps := e2eStarlarkSteps(t, "composition-usage-v2.yaml")

	// Observe the base as ready so the dependent is emitted and a Usage
	// resource is generated for the pair.
	observed := mustOXR(t, `{}`)
	observed.Resources = map[string]*fnv1.Resource{
		"usage-base": {Resource: resource.MustStructJSON(`{
			"apiVersion":"nop.crossplane.io/v1alpha1","kind":"NopResource",
			"metadata":{"name":"usage-base"},
			"status":{"conditions":[
				{"type":"Ready","status":"True"},
				{"type":"Synced","status":"True"}]}}`)},
	}
	rsp := runE2EStep(t, f, &fnv1.RunFunctionRequest{
		Input:    steps["test-usage-v2"],
		Observed: observed,
	})

	// The e2e XR is cluster-scoped (legacy), so the v2 API must produce the
	// cluster-scoped ClusterUsage kind (the v2 Usage kind is namespaced).
	foundV2Usage := false
	for _, r := range rsp.GetDesired().GetResources() {
		m := r.GetResource().AsMap()
		if m["kind"] == "ClusterUsage" && m["apiVersion"] == "protection.crossplane.io/v1beta1" {
			foundV2Usage = true
		}
	}
	if !foundV2Usage {
		t.Errorf("no protection.crossplane.io/v1beta1 ClusterUsage in desired state (usageAPIVersion v2, cluster-scoped composite)")
	}

	// A namespaced composite must produce the namespaced Usage kind instead.
	observedNS := &fnv1.State{
		Composite: &fnv1.Resource{
			Resource: resource.MustStructJSON(`{"apiVersion":"e2e.fn-starlark.io/v1","kind":"XTest","metadata":{"name":"smoke","namespace":"default"},"spec":{}}`),
		},
		Resources: observed.Resources,
	}
	rspNS := runE2EStep(t, f, &fnv1.RunFunctionRequest{
		Input:    steps["test-usage-v2"],
		Observed: observedNS,
	})
	foundNSUsage := false
	for _, r := range rspNS.GetDesired().GetResources() {
		m := r.GetResource().AsMap()
		if m["kind"] == "Usage" && m["apiVersion"] == "protection.crossplane.io/v1beta1" {
			foundNSUsage = true
		}
	}
	if !foundNSUsage {
		t.Errorf("no namespaced Usage in desired state (usageAPIVersion v2, namespaced composite)")
	}
}
