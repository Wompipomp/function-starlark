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

func TestBuildUsageResource(t *testing.T) {
	t.Run("v1alpha1 API version", func(t *testing.T) {
		res := buildUsageResource("app", "db", UsageAPIVersionV1)
		assertUsageResource(t, res, "app", "db", UsageAPIVersionV1)
	})

	t.Run("v1beta1 API version", func(t *testing.T) {
		res := buildUsageResource("app", "db", UsageAPIVersionV2)
		assertUsageResource(t, res, "app", "db", UsageAPIVersionV2)
	})
}

func TestBuildUsageResources(t *testing.T) {
	t.Run("two dependency pairs", func(t *testing.T) {
		deps := []DependencyPair{
			{Dependent: "app", Dependency: "db", IsRef: true},
			{Dependent: "app", Dependency: "cache", IsRef: true},
		}
		result := BuildUsageResources(deps, UsageAPIVersionV1)
		if len(result) != 2 {
			t.Fatalf("BuildUsageResources returned %d resources, want 2", len(result))
		}
		// Check both usage names exist as keys
		name1 := usageName("app", "db")
		name2 := usageName("app", "cache")
		if _, ok := result[name1]; !ok {
			t.Errorf("missing key %q", name1)
		}
		if _, ok := result[name2]; !ok {
			t.Errorf("missing key %q", name2)
		}
	})

	t.Run("chain A->B->C produces 2 usages", func(t *testing.T) {
		deps := []DependencyPair{
			{Dependent: "A", Dependency: "B", IsRef: true},
			{Dependent: "B", Dependency: "C", IsRef: true},
		}
		result := BuildUsageResources(deps, UsageAPIVersionV1)
		if len(result) != 2 {
			t.Fatalf("BuildUsageResources returned %d resources, want 2", len(result))
		}
	})

	t.Run("empty dependency list", func(t *testing.T) {
		result := BuildUsageResources(nil, UsageAPIVersionV1)
		if len(result) != 0 {
			t.Fatalf("BuildUsageResources returned %d resources, want 0", len(result))
		}
	})

	t.Run("duplicate dep pair produces one resource", func(t *testing.T) {
		deps := []DependencyPair{
			{Dependent: "app", Dependency: "db", IsRef: true},
			{Dependent: "app", Dependency: "db", IsRef: true},
		}
		result := BuildUsageResources(deps, UsageAPIVersionV1)
		// Same dependent+dependency hashes to the same name, so map deduplicates.
		if len(result) != 1 {
			t.Fatalf("BuildUsageResources returned %d resources, want 1 (deduplicated)", len(result))
		}
	})
}

func TestDetectUsageAPIVersion(t *testing.T) {
	tests := []struct {
		name     string
		override string
		want     string
	}{
		{"v1 override", "v1", UsageAPIVersionV1},
		{"v2 override", "v2", UsageAPIVersionV2},
		{"empty default", "", UsageAPIVersionV1},
		{"unknown string defaults to v1", "garbage", UsageAPIVersionV1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetectUsageAPIVersion(tt.override)
			if got != tt.want {
				t.Errorf("DetectUsageAPIVersion(%q) = %q, want %q", tt.override, got, tt.want)
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
			{Dependent: "app", Dependency: "db", IsRef: true},          // ResourceRef -- skip
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
func assertUsageResource(t *testing.T, res *structpb.Struct, dependent, dependency, apiVersion string) {
	t.Helper()

	fields := res.GetFields()

	// apiVersion
	if got := fields["apiVersion"].GetStringValue(); got != apiVersion {
		t.Errorf("apiVersion = %q, want %q", got, apiVersion)
	}

	// kind
	if got := fields["kind"].GetStringValue(); got != "Usage" {
		t.Errorf("kind = %q, want %q", got, "Usage")
	}

	// metadata.name
	meta := fields["metadata"].GetStructValue().GetFields()
	wantName := usageName(dependent, dependency)
	if got := meta["name"].GetStringValue(); got != wantName {
		t.Errorf("metadata.name = %q, want %q", got, wantName)
	}

	// spec
	spec := fields["spec"].GetStructValue().GetFields()

	// spec.replayDeletion
	if got := spec["replayDeletion"].GetBoolValue(); !got {
		t.Errorf("spec.replayDeletion = %v, want true", got)
	}

	// spec.of.resourceRef.name
	ofRef := spec["of"].GetStructValue().GetFields()["resourceRef"].GetStructValue().GetFields()
	if got := ofRef["name"].GetStringValue(); got != dependency {
		t.Errorf("spec.of.resourceRef.name = %q, want %q", got, dependency)
	}

	// spec.by.resourceRef.name
	byRef := spec["by"].GetStructValue().GetFields()["resourceRef"].GetStructValue().GetFields()
	if got := byRef["name"].GetStringValue(); got != dependent {
		t.Errorf("spec.by.resourceRef.name = %q, want %q", got, dependent)
	}
}
