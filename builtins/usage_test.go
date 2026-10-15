package builtins

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"

	"google.golang.org/protobuf/types/known/structpb"
)

func TestUsageName(t *testing.T) {
	t.Run("deterministic hash", func(t *testing.T) {
		name1 := usageName("app", "db")
		name2 := usageName("app", "db")
		if name1 != name2 {
			t.Errorf("usageName not deterministic: got %q and %q", name1, name2)
		}
	})

	t.Run("correct format", func(t *testing.T) {
		name := usageName("app", "db")
		// Compute expected: sha256("app\x00db")[:4] as 8 hex chars
		h := sha256.Sum256([]byte("app\x00db"))
		want := "usage-" + fmt.Sprintf("%x", h[:4])
		if name != want {
			t.Errorf("usageName(%q, %q) = %q, want %q", "app", "db", name, want)
		}
	})

	t.Run("NUL separator prevents collisions", func(t *testing.T) {
		// "a" + NUL + "bc" != "ab" + NUL + "c"
		name1 := usageName("a", "bc")
		name2 := usageName("ab", "c")
		if name1 == name2 {
			t.Errorf("usageName(%q,%q) == usageName(%q,%q) = %q; NUL separator should prevent collision",
				"a", "bc", "ab", "c", name1)
		}
	})
}

func TestResourceNameLabelValue(t *testing.T) {
	t.Run("short name unchanged", func(t *testing.T) {
		name := "env-app-core" // <=63 bytes
		if got := ResourceNameLabelValue(name); got != name {
			t.Errorf("ResourceNameLabelValue(%q) = %q, want unchanged", name, got)
		}
	})

	t.Run("exactly 63 bytes unchanged", func(t *testing.T) {
		name := strings.Repeat("a", 63)
		if got := ResourceNameLabelValue(name); got != name {
			t.Errorf("63-byte name should be unchanged, got %q (len %d)", got, len(got))
		}
	})

	t.Run("over-limit name is hashed and label-safe", func(t *testing.T) {
		// A composition-resource-name of 68 bytes: legal as a name, too long
		// as a label value.
		name := "env-a1b2c3d4-sample-database-services-admin-2026-03-19-08-x7k2q9-core"
		if len(name) <= maxLabelValueBytes {
			t.Fatalf("test precondition: name should exceed %d bytes", maxLabelValueBytes)
		}
		got := ResourceNameLabelValue(name)
		if len(got) > maxLabelValueBytes {
			t.Errorf("hashed value %q is %d bytes, must be <= %d", got, len(got), maxLabelValueBytes)
		}
		if strings.HasPrefix(got, "-") || strings.HasSuffix(got, "-") {
			t.Errorf("hashed value %q must be a valid label (no leading/trailing dash)", got)
		}
		// Deterministic: same input -> same output (resource label and Usage
		// selector must agree).
		if again := ResourceNameLabelValue(name); again != got {
			t.Errorf("not deterministic: %q vs %q", got, again)
		}
		// Distinct over-limit names hash to distinct values.
		other := "env-a1b2c3d4-sample-database-services-users-2026-03-19-08-x7k2q9-core"
		if ResourceNameLabelValue(other) == got {
			t.Errorf("distinct names collided on label value %q", got)
		}
	})

	t.Run("usage selector uses the label-safe value", func(t *testing.T) {
		long := strings.Repeat("x", 70)
		res := buildUsageResource(long, "db", UsageAPIVersionV2, "ClusterUsage", "", "", nil)
		got := usageSelector(res, "by").GetFields()["matchLabels"].GetStructValue().
			GetFields()[ResourceNameLabel].GetStringValue()
		if got != ResourceNameLabelValue(long) {
			t.Errorf("by selector label = %q, want %q", got, ResourceNameLabelValue(long))
		}
	})
}

func TestBuildUsageResource(t *testing.T) {
	typeInfos := map[string]resourceTypeInfo{
		"app": {APIVersion: "nop.crossplane.io/v1alpha1", Kind: "NopResource"},
		"db":  {APIVersion: "nop.crossplane.io/v1alpha1", Kind: "NopResource"},
	}
	t.Run("v1alpha1 API version", func(t *testing.T) {
		res := buildUsageResource("app", "db", UsageAPIVersionV1, "Usage", "", "", typeInfos)
		assertUsageResource(t, res, "app", "db", UsageAPIVersionV1, "Usage")
		if _, ok := res.GetFields()["metadata"]; ok {
			t.Error("cluster-scoped Usage must not carry metadata.namespace")
		}
	})

	t.Run("v1beta1 API version", func(t *testing.T) {
		res := buildUsageResource("app", "db", UsageAPIVersionV2, "ClusterUsage", "", "", typeInfos)
		assertUsageResource(t, res, "app", "db", UsageAPIVersionV2, "ClusterUsage")
	})

	t.Run("namespaced usage carries namespace", func(t *testing.T) {
		res := buildUsageResource("app", "db", UsageAPIVersionV2, "Usage", "team-a", "", typeInfos)
		assertUsageResource(t, res, "app", "db", UsageAPIVersionV2, "Usage")
		got := res.GetFields()["metadata"].GetStructValue().GetFields()["namespace"].GetStringValue()
		if got != "team-a" {
			t.Errorf("metadata.namespace = %q, want team-a", got)
		}
		ofSel := usageSelector(res, "of")
		if _, ok := ofSel.GetFields()["namespace"]; ok {
			t.Error("of.resourceSelector.namespace must be omitted when the dependency is in the Usage namespace")
		}
	})

	t.Run("cross-namespace dependency sets of selector namespace", func(t *testing.T) {
		res := buildUsageResource("app", "db", UsageAPIVersionV2, "Usage", "team-a", "team-b", typeInfos)
		got := usageSelector(res, "of").GetFields()["namespace"].GetStringValue()
		if got != "team-b" {
			t.Errorf("of.resourceSelector.namespace = %q, want team-b", got)
		}
		if _, ok := usageSelector(res, "by").GetFields()["namespace"]; ok {
			t.Error("by.resourceSelector must never carry a namespace")
		}
	})
}

// usageSelector returns spec.<side>.resourceSelector of a Usage struct.
func usageSelector(res *structpb.Struct, side string) *structpb.Struct {
	return res.GetFields()["spec"].GetStructValue().
		GetFields()[side].GetStructValue().
		GetFields()["resourceSelector"].GetStructValue()
}

func TestUsageKind(t *testing.T) {
	tests := []struct {
		name       string
		apiVersion string
		namespaced bool
		want       string
	}{
		// The v2 Usage kind is namespaced; a pair of cluster-scoped
		// resources must use the cluster-scoped ClusterUsage.
		{"v2 both cluster-scoped", UsageAPIVersionV2, false, "ClusterUsage"},
		{"v2 a namespaced side", UsageAPIVersionV2, true, "Usage"},
		// The v1 Usage is always cluster-scoped.
		{"v1 both cluster-scoped", UsageAPIVersionV1, false, "Usage"},
		{"v1 a namespaced side", UsageAPIVersionV1, true, "Usage"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := usageKind(tt.apiVersion, tt.namespaced); got != tt.want {
				t.Errorf("usageKind(%q, %v) = %q, want %q", tt.apiVersion, tt.namespaced, got, tt.want)
			}
		})
	}
}

func TestUsageNamespace(t *testing.T) {
	tests := []struct {
		name, dependentNS, dependencyNS, want string
	}{
		{"dependent wins", "env-by", "env-of", "env-by"},
		{"dependency when dependent is cluster-scoped", "", "env-of", "env-of"},
		{"dependent when dependency is cluster-scoped", "env-by", "", "env-by"},
		{"both cluster-scoped", "", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := usageNamespace(tt.dependentNS, tt.dependencyNS); got != tt.want {
				t.Errorf("usageNamespace(%q, %q) = %q, want %q", tt.dependentNS, tt.dependencyNS, got, tt.want)
			}
		})
	}
}

// readyObserved builds an observed body with Ready=True and Synced=True.
// ns == "" leaves metadata.namespace unset (cluster-scoped).
func readyObserved(apiVersion, kind, ns string) *structpb.Struct {
	meta := map[string]any{"name": strings.ToLower(kind)}
	if ns != "" {
		meta["namespace"] = ns
	}
	s, _ := structpb.NewStruct(map[string]any{
		"apiVersion": apiVersion,
		"kind":       kind,
		"metadata":   meta,
		"status": map[string]any{
			"conditions": []any{
				map[string]any{"type": "Ready", "status": "True"},
				map[string]any{"type": "Synced", "status": "True"},
			},
		},
	})
	return s
}

// notReadyObserved builds an observed body with Ready=False.
func notReadyObserved(apiVersion, kind string) *structpb.Struct {
	s, _ := structpb.NewStruct(map[string]any{
		"apiVersion": apiVersion,
		"kind":       kind,
		"status": map[string]any{
			"conditions": []any{
				map[string]any{"type": "Ready", "status": "False"},
				map[string]any{"type": "Synced", "status": "True"},
			},
		},
	})
	return s
}

func TestBuildUsageResources(t *testing.T) {
	ready := map[string]*structpb.Struct{
		"app":   readyObserved("v1", "App", ""),
		"db":    readyObserved("v1", "Database", ""),
		"cache": readyObserved("v1", "Cache", ""),
		"A":     readyObserved("v1", "A", ""),
		"B":     readyObserved("v1", "B", ""),
		"C":     readyObserved("v1", "C", ""),
	}

	t.Run("two dependency pairs", func(t *testing.T) {
		deps := []DependencyPair{
			{Dependent: "app", Dependency: "db", IsRef: true},
			{Dependent: "app", Dependency: "cache", IsRef: true},
		}
		result := BuildUsageResources(deps, UsageAPIVersionV1, nil, ready)
		if len(result) != 2 {
			t.Fatalf("BuildUsageResources returned %d resources, want 2", len(result))
		}
		for name, res := range result {
			if !strings.HasPrefix(name, "usage-") {
				t.Errorf("resource name %q does not start with usage-", name)
			}
			if got := res.GetFields()["apiVersion"].GetStringValue(); got != UsageAPIVersionV1 {
				t.Errorf("apiVersion = %q, want %q", got, UsageAPIVersionV1)
			}
		}
	})

	t.Run("chain A->B->C produces 2 usages", func(t *testing.T) {
		deps := []DependencyPair{
			{Dependent: "A", Dependency: "B", IsRef: true},
			{Dependent: "B", Dependency: "C", IsRef: true},
		}
		result := BuildUsageResources(deps, UsageAPIVersionV1, nil, ready)
		if len(result) != 2 {
			t.Fatalf("BuildUsageResources returned %d resources, want 2", len(result))
		}
	})

	t.Run("empty dependency list", func(t *testing.T) {
		result := BuildUsageResources(nil, UsageAPIVersionV1, nil, nil)
		if len(result) != 0 {
			t.Fatalf("BuildUsageResources returned %d resources, want 0", len(result))
		}
	})

	t.Run("duplicate dep pair produces one resource", func(t *testing.T) {
		deps := []DependencyPair{
			{Dependent: "app", Dependency: "db", IsRef: true},
			{Dependent: "app", Dependency: "db", IsRef: true},
		}
		result := BuildUsageResources(deps, UsageAPIVersionV1, nil, ready)
		// Same dependent+dependency hashes to the same name, so map deduplicates.
		if len(result) != 1 {
			t.Fatalf("BuildUsageResources returned %d resources, want 1 (deduplicated)", len(result))
		}
	})

	t.Run("skips pair until both sides are observed and ready", func(t *testing.T) {
		deps := []DependencyPair{{Dependent: "app", Dependency: "db", IsRef: true}}
		if got := BuildUsageResources(deps, UsageAPIVersionV2, nil, nil); len(got) != 0 {
			t.Fatalf("nothing observed: got %d usages, want 0", len(got))
		}
		onlyDep := map[string]*structpb.Struct{"db": readyObserved("v1", "Database", "team-a")}
		if got := BuildUsageResources(deps, UsageAPIVersionV2, nil, onlyDep); len(got) != 0 {
			t.Fatalf("dependent missing: got %d usages, want 0", len(got))
		}
		depNotReady := map[string]*structpb.Struct{
			"app": readyObserved("v1", "App", "team-a"),
			"db":  notReadyObserved("v1", "Database"),
		}
		if got := BuildUsageResources(deps, UsageAPIVersionV2, nil, depNotReady); len(got) != 0 {
			t.Fatalf("dependency not ready: got %d usages, want 0", len(got))
		}
	})

	t.Run("field path gate must be truthy before the usage is emitted", func(t *testing.T) {
		deps := []DependencyPair{{Dependent: "app", Dependency: "db", IsRef: true, FieldPath: "status.atProvider.id"}}
		observed := map[string]*structpb.Struct{
			"app": readyObserved("v1", "App", ""),
			"db":  readyObserved("v1", "Database", ""),
		}
		if got := BuildUsageResources(deps, UsageAPIVersionV2, nil, observed); len(got) != 0 {
			t.Fatalf("field not set: got %d usages, want 0", len(got))
		}
		observed["db"].GetFields()["status"].GetStructValue().Fields["atProvider"] = structpb.NewStructValue(&structpb.Struct{
			Fields: map[string]*structpb.Value{"id": structpb.NewStringValue("db-123")},
		})
		if got := BuildUsageResources(deps, UsageAPIVersionV2, nil, observed); len(got) != 1 {
			t.Fatalf("field set: got %d usages, want 1", len(got))
		}
	})

	// mkDesired builds a minimal desired body without metadata.namespace,
	// which is what scripts typically emit for namespaced resources.
	mkDesired := func(kind string) CollectedResource {
		return CollectedResource{Body: &structpb.Struct{Fields: map[string]*structpb.Value{
			"apiVersion": structpb.NewStringValue("example.org/v1"),
			"kind":       structpb.NewStringValue(kind),
		}}}
	}
	kindOf := func(res *structpb.Struct) string { return res.GetFields()["kind"].GetStringValue() }
	nsOf := func(res *structpb.Struct) string {
		return res.GetFields()["metadata"].GetStructValue().GetFields()["namespace"].GetStringValue()
	}

	t.Run("v2: kind follows observed namespaces per pair", func(t *testing.T) {
		deps := []DependencyPair{
			{Dependent: "vpc", Dependency: "igw", IsRef: true},
			{Dependent: "vpc", Dependency: "collector", IsRef: true},
			{Dependent: "collector", Dependency: "vpc", IsRef: true},
		}
		resources := map[string]CollectedResource{
			"vpc":       mkDesired("VPC"),
			"igw":       mkDesired("InternetGateway"),
			"collector": mkDesired("CollectorRequest"),
		}
		observed := map[string]*structpb.Struct{
			"vpc":       readyObserved("example.org/v1", "VPC", ""),
			"igw":       readyObserved("example.org/v1", "InternetGateway", ""),
			"collector": readyObserved("example.org/v1", "CollectorRequest", "env-a"),
		}
		result := BuildUsageResources(deps, UsageAPIVersionV2, resources, observed)

		cluster := result[usageName("vpc", "igw")]
		if cluster == nil {
			t.Fatal("missing cluster/cluster pair usage")
		}
		if kindOf(cluster) != "ClusterUsage" {
			t.Errorf("cluster/cluster pair kind = %q, want ClusterUsage", kindOf(cluster))
		}
		if _, ok := cluster.GetFields()["metadata"]; ok {
			t.Error("ClusterUsage must not carry metadata.namespace")
		}

		// Cluster-scoped dependent, namespaced dependency: Usage lives in
		// the dependency's namespace, no selector namespace needed.
		byCluster := result[usageName("vpc", "collector")]
		if byCluster == nil {
			t.Fatal("missing cluster-by/namespaced-of pair usage")
		}
		if kindOf(byCluster) != "Usage" || nsOf(byCluster) != "env-a" {
			t.Errorf("cluster-by/namespaced-of pair = %s in %q, want Usage in env-a", kindOf(byCluster), nsOf(byCluster))
		}
		if _, ok := usageSelector(byCluster, "of").GetFields()["namespace"]; ok {
			t.Error("of selector namespace must be omitted when it equals the Usage namespace")
		}

		// Namespaced dependent, cluster-scoped dependency: Usage lives in
		// the dependent's namespace.
		byNS := result[usageName("collector", "vpc")]
		if byNS == nil {
			t.Fatal("missing namespaced-by/cluster-of pair usage")
		}
		if kindOf(byNS) != "Usage" || nsOf(byNS) != "env-a" {
			t.Errorf("namespaced-by/cluster-of pair = %s in %q, want Usage in env-a", kindOf(byNS), nsOf(byNS))
		}
	})

	t.Run("v2: cross-namespace pair sets of selector namespace", func(t *testing.T) {
		deps := []DependencyPair{{Dependent: "app", Dependency: "db", IsRef: true}}
		observed := map[string]*structpb.Struct{
			"app": readyObserved("example.org/v1", "App", "team-a"),
			"db":  readyObserved("example.org/v1", "Database", "team-b"),
		}
		res := BuildUsageResources(deps, UsageAPIVersionV2, nil, observed)[usageName("app", "db")]
		if res == nil {
			t.Fatal("missing usage")
		}
		if kindOf(res) != "Usage" || nsOf(res) != "team-a" {
			t.Fatalf("got %s in %q, want Usage in the dependent's namespace team-a", kindOf(res), nsOf(res))
		}
		if got := usageSelector(res, "of").GetFields()["namespace"].GetStringValue(); got != "team-b" {
			t.Errorf("of.resourceSelector.namespace = %q, want team-b", got)
		}
	})

	t.Run("v2: namespace comes from observed state, not the desired body", func(t *testing.T) {
		deps := []DependencyPair{{Dependent: "app", Dependency: "db", IsRef: true}}
		// Desired bodies omit metadata.namespace; Crossplane injected the
		// XR namespace into the observed objects.
		resources := map[string]CollectedResource{
			"app": mkDesired("App"),
			"db":  mkDesired("Database"),
		}
		observed := map[string]*structpb.Struct{
			"app": readyObserved("example.org/v1", "App", "default"),
			"db":  readyObserved("example.org/v1", "Database", "default"),
		}
		res := BuildUsageResources(deps, UsageAPIVersionV2, resources, observed)[usageName("app", "db")]
		if res == nil {
			t.Fatal("missing usage")
		}
		// A namespaced XR cannot compose a cluster-scoped ClusterUsage.
		if kindOf(res) != "Usage" || nsOf(res) != "default" {
			t.Errorf("got %s in %q, want Usage in default", kindOf(res), nsOf(res))
		}
	})

	t.Run("apiVersion and kind fall back to the desired body", func(t *testing.T) {
		deps := []DependencyPair{{Dependent: "app", Dependency: "db", IsRef: true}}
		resources := map[string]CollectedResource{
			"app": mkDesired("App"),
			"db":  mkDesired("Database"),
		}
		// Observed bodies that carry only status (no apiVersion/kind).
		bare := func() *structpb.Struct {
			s, _ := structpb.NewStruct(map[string]any{
				"status": map[string]any{"conditions": []any{
					map[string]any{"type": "Ready", "status": "True"},
					map[string]any{"type": "Synced", "status": "True"},
				}},
			})
			return s
		}
		observed := map[string]*structpb.Struct{"app": bare(), "db": bare()}
		res := BuildUsageResources(deps, UsageAPIVersionV2, resources, observed)[usageName("app", "db")]
		if res == nil {
			t.Fatal("missing usage")
		}
		of := res.GetFields()["spec"].GetStructValue().GetFields()["of"].GetStructValue().GetFields()
		if got := of["kind"].GetStringValue(); got != "Database" {
			t.Errorf("of.kind = %q, want Database (from desired body)", got)
		}
		if got := of["apiVersion"].GetStringValue(); got != "example.org/v1" {
			t.Errorf("of.apiVersion = %q, want example.org/v1 (from desired body)", got)
		}
	})

	t.Run("v1 ignores namespaces", func(t *testing.T) {
		deps := []DependencyPair{{Dependent: "app", Dependency: "db", IsRef: true}}
		observed := map[string]*structpb.Struct{
			"app": readyObserved("example.org/v1", "App", "team-a"),
			"db":  readyObserved("example.org/v1", "Database", "team-b"),
		}
		res := BuildUsageResources(deps, UsageAPIVersionV1, nil, observed)[usageName("app", "db")]
		if res == nil {
			t.Fatal("missing usage")
		}
		if kindOf(res) != "Usage" {
			t.Errorf("kind = %q, want Usage", kindOf(res))
		}
		if _, ok := res.GetFields()["metadata"]; ok {
			t.Error("v1 Usage must not carry metadata.namespace")
		}
		if _, ok := usageSelector(res, "of").GetFields()["namespace"]; ok {
			t.Error("v1 Usage selector must not carry a namespace")
		}
	})
}

func TestResolveUsageAPIVersion(t *testing.T) {
	tests := []struct {
		name     string
		override string
		want     string
	}{
		{"v1 override", "v1", UsageAPIVersionV1},
		{"v2 override", "v2", UsageAPIVersionV2},
		{"empty defaults to v2", "", UsageAPIVersionV2},
		{"unknown string defaults to v2", "garbage", UsageAPIVersionV2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveUsageAPIVersion(tt.override)
			if got != tt.want {
				t.Errorf("ResolveUsageAPIVersion(%q) = %q, want %q", tt.override, got, tt.want)
			}
		})
	}
}

func TestValidateDependencies(t *testing.T) {
	allResources := map[string]bool{
		"app":   true,
		"db":    true,
		"cache": true,
	}

	t.Run("all targets present", func(t *testing.T) {
		deps := []DependencyPair{
			{Dependent: "app", Dependency: "db", IsRef: true},
			{Dependent: "app", Dependency: "cache", IsRef: true},
		}
		if err := ValidateDependencies(deps, allResources); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("missing ResourceRef target", func(t *testing.T) {
		deps := []DependencyPair{
			{Dependent: "app", Dependency: "missing-svc", IsRef: true},
		}
		err := ValidateDependencies(deps, allResources)
		if err == nil {
			t.Fatal("expected error for missing target, got nil")
		}
		msg := err.Error()
		if !strings.Contains(msg, "missing-svc") {
			t.Errorf("error should mention missing target: %v", err)
		}
		if !strings.Contains(msg, "available resources") {
			t.Errorf("error should list available resources: %v", err)
		}
	})

	t.Run("string ref not validated", func(t *testing.T) {
		deps := []DependencyPair{
			{Dependent: "app", Dependency: "external-vpc", IsRef: false},
		}
		if err := ValidateDependencies(deps, allResources); err != nil {
			t.Errorf("string refs should not be validated, got: %v", err)
		}
	})

	t.Run("self-reference cycle", func(t *testing.T) {
		deps := []DependencyPair{
			{Dependent: "A", Dependency: "A", IsRef: true},
		}
		err := ValidateDependencies(deps, map[string]bool{"A": true})
		if err == nil {
			t.Fatal("expected cycle error, got nil")
		}
		if !strings.Contains(err.Error(), "circular dependency") {
			t.Errorf("error should mention circular dependency: %v", err)
		}
	})

	t.Run("2-node cycle", func(t *testing.T) {
		deps := []DependencyPair{
			{Dependent: "A", Dependency: "B", IsRef: true},
			{Dependent: "B", Dependency: "A", IsRef: true},
		}
		err := ValidateDependencies(deps, map[string]bool{"A": true, "B": true})
		if err == nil {
			t.Fatal("expected cycle error, got nil")
		}
		if !strings.Contains(err.Error(), "circular dependency") {
			t.Errorf("error should mention circular dependency: %v", err)
		}
	})

	t.Run("3-node cycle", func(t *testing.T) {
		deps := []DependencyPair{
			{Dependent: "A", Dependency: "B", IsRef: true},
			{Dependent: "B", Dependency: "C", IsRef: true},
			{Dependent: "C", Dependency: "A", IsRef: true},
		}
		err := ValidateDependencies(deps, map[string]bool{"A": true, "B": true, "C": true})
		if err == nil {
			t.Fatal("expected cycle error, got nil")
		}
		msg := err.Error()
		if !strings.Contains(msg, "circular dependency") {
			t.Errorf("error should mention circular dependency: %v", err)
		}
		// Should contain the cycle path with ->
		if !strings.Contains(msg, " -> ") {
			t.Errorf("error should show cycle path with ' -> ': %v", err)
		}
	})

	t.Run("valid chain no cycle", func(t *testing.T) {
		deps := []DependencyPair{
			{Dependent: "A", Dependency: "B", IsRef: true},
			{Dependent: "B", Dependency: "C", IsRef: true},
		}
		err := ValidateDependencies(deps, map[string]bool{"A": true, "B": true, "C": true})
		if err != nil {
			t.Errorf("valid chain should not error: %v", err)
		}
	})

	t.Run("mixed refs and string refs", func(t *testing.T) {
		deps := []DependencyPair{
			{Dependent: "app", Dependency: "db", IsRef: true},
			{Dependent: "app", Dependency: "external-vpc", IsRef: false},
		}
		err := ValidateDependencies(deps, map[string]bool{"app": true, "db": true})
		if err != nil {
			t.Errorf("mixed valid refs should not error: %v", err)
		}
	})

	t.Run("empty deps", func(t *testing.T) {
		if err := ValidateDependencies(nil, map[string]bool{"app": true}); err != nil {
			t.Errorf("empty deps should not error: %v", err)
		}
	})

	t.Run("empty resourceNames with ref", func(t *testing.T) {
		deps := []DependencyPair{
			{Dependent: "app", Dependency: "db", IsRef: true},
		}
		err := ValidateDependencies(deps, map[string]bool{})
		if err == nil {
			t.Fatal("expected error for ResourceRef against empty resourceNames")
		}
		if !strings.Contains(err.Error(), "db") {
			t.Errorf("error should mention missing target 'db': %v", err)
		}
		if !strings.Contains(err.Error(), "available resources: []") {
			t.Errorf("error should show empty available list: %v", err)
		}
	})
}

func TestWarnUnmatchedStringRefs(t *testing.T) {
	t.Run("unmatched string ref returns warning", func(t *testing.T) {
		deps := []DependencyPair{
			{Dependent: "app", Dependency: "external-vpc", IsRef: false},
		}
		resourceNames := map[string]bool{"app": true, "db": true}

		warnings := WarnUnmatchedStringRefs(deps, resourceNames)
		if len(warnings) != 1 {
			t.Fatalf("expected 1 warning, got %d: %v", len(warnings), warnings)
		}
		w := warnings[0]
		if !strings.Contains(w, "external-vpc") {
			t.Errorf("warning should mention unmatched ref: %s", w)
		}
		if !strings.Contains(w, "string ref") {
			t.Errorf("warning should mention 'string ref': %s", w)
		}
		if !strings.Contains(w, "does not match") {
			t.Errorf("warning should contain 'does not match': %s", w)
		}
		// Available resources should be sorted alphabetically.
		if !strings.Contains(w, "[app, db]") {
			t.Errorf("warning should list available resources sorted: %s", w)
		}
	})

	t.Run("matched string ref returns no warning", func(t *testing.T) {
		deps := []DependencyPair{
			{Dependent: "app", Dependency: "db", IsRef: false},
		}
		resourceNames := map[string]bool{"app": true, "db": true}

		warnings := WarnUnmatchedStringRefs(deps, resourceNames)
		if len(warnings) != 0 {
			t.Errorf("expected 0 warnings for matched string ref, got %d: %v", len(warnings), warnings)
		}
	})

	t.Run("mixed deps warns only for unmatched string ref", func(t *testing.T) {
		deps := []DependencyPair{
			{Dependent: "app", Dependency: "db", IsRef: true},            // ResourceRef -- skip
			{Dependent: "app", Dependency: "external-vpc", IsRef: false}, // unmatched string ref
		}
		resourceNames := map[string]bool{"app": true, "db": true}

		warnings := WarnUnmatchedStringRefs(deps, resourceNames)
		if len(warnings) != 1 {
			t.Fatalf("expected 1 warning (string ref only), got %d: %v", len(warnings), warnings)
		}
		if !strings.Contains(warnings[0], "external-vpc") {
			t.Errorf("warning should mention unmatched ref: %s", warnings[0])
		}
	})

	t.Run("all ResourceRef returns no warnings", func(t *testing.T) {
		deps := []DependencyPair{
			{Dependent: "app", Dependency: "db", IsRef: true},
			{Dependent: "app", Dependency: "cache", IsRef: true},
		}
		resourceNames := map[string]bool{"app": true, "db": true, "cache": true}

		warnings := WarnUnmatchedStringRefs(deps, resourceNames)
		if len(warnings) != 0 {
			t.Errorf("expected 0 warnings for all-ResourceRef deps, got %d: %v", len(warnings), warnings)
		}
	})

	t.Run("multiple unmatched string refs", func(t *testing.T) {
		deps := []DependencyPair{
			{Dependent: "app", Dependency: "missing-a", IsRef: false},
			{Dependent: "web", Dependency: "missing-b", IsRef: false},
		}
		resourceNames := map[string]bool{"app": true, "web": true}

		warnings := WarnUnmatchedStringRefs(deps, resourceNames)
		if len(warnings) != 2 {
			t.Fatalf("expected 2 warnings, got %d: %v", len(warnings), warnings)
		}
		foundA, foundB := false, false
		for _, w := range warnings {
			if strings.Contains(w, "missing-a") {
				foundA = true
			}
			if strings.Contains(w, "missing-b") {
				foundB = true
			}
		}
		if !foundA {
			t.Error("expected a warning mentioning 'missing-a'")
		}
		if !foundB {
			t.Error("expected a warning mentioning 'missing-b'")
		}
	})
}

// assertUsageResource verifies a Usage protobuf Struct has the correct structure.
func assertUsageResource(t *testing.T, res *structpb.Struct, dependent, dependency, apiVersion, kind string) {
	t.Helper()

	fields := res.GetFields()

	// apiVersion
	if got := fields["apiVersion"].GetStringValue(); got != apiVersion {
		t.Errorf("apiVersion = %q, want %q", got, apiVersion)
	}

	// kind
	if got := fields["kind"].GetStringValue(); got != kind {
		t.Errorf("kind = %q, want %q", got, kind)
	}

	// metadata.name must never be set (Crossplane generates it). Only a
	// namespaced v2 Usage carries metadata at all, and then only a namespace.
	if meta, ok := fields["metadata"]; ok {
		mf := meta.GetStructValue().GetFields()
		if _, hasName := mf["name"]; hasName {
			t.Errorf("expected no metadata.name on Usage body, got %v", meta)
		}
		if kind != "Usage" || apiVersion != UsageAPIVersionV2 {
			t.Errorf("expected no metadata on cluster-scoped %s body, got %v", kind, meta)
		}
	}

	// spec
	spec := fields["spec"].GetStructValue().GetFields()

	// spec.replayDeletion
	if got := spec["replayDeletion"].GetBoolValue(); !got {
		t.Errorf("spec.replayDeletion = %v, want true", got)
	}

	// spec.of.resourceSelector.matchControllerRef
	ofSel := spec["of"].GetStructValue().GetFields()["resourceSelector"].GetStructValue().GetFields()
	if got := ofSel["matchControllerRef"].GetBoolValue(); !got {
		t.Errorf("spec.of.resourceSelector.matchControllerRef = %v, want true", got)
	}
	ofLabels := ofSel["matchLabels"].GetStructValue().GetFields()
	if got := ofLabels[ResourceNameLabel].GetStringValue(); got != dependency {
		t.Errorf("spec.of matchLabels[%s] = %q, want %q", ResourceNameLabel, got, dependency)
	}

	// spec.by.resourceSelector.matchControllerRef
	bySel := spec["by"].GetStructValue().GetFields()["resourceSelector"].GetStructValue().GetFields()
	if got := bySel["matchControllerRef"].GetBoolValue(); !got {
		t.Errorf("spec.by.resourceSelector.matchControllerRef = %v, want true", got)
	}
	byLabels := bySel["matchLabels"].GetStructValue().GetFields()
	if got := byLabels[ResourceNameLabel].GetStringValue(); got != dependent {
		t.Errorf("spec.by matchLabels[%s] = %q, want %q", ResourceNameLabel, got, dependent)
	}
}
