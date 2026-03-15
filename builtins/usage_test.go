package builtins

import (
	"crypto/sha256"
	"fmt"
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
