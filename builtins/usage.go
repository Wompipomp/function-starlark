package builtins

import (
	"crypto/sha256"
	"fmt"

	"google.golang.org/protobuf/types/known/structpb"
)

// Usage API version constants for Crossplane Usage resources.
const (
	UsageAPIVersionV1 = "apiextensions.crossplane.io/v1alpha1"
	UsageAPIVersionV2 = "protection.crossplane.io/v1beta1"
)

// usageName produces a deterministic name for a Usage resource.
// Format: "usage-" + first 8 hex chars of sha256(dependent + NUL + dependency).
func usageName(dependent, dependency string) string {
	h := sha256.Sum256([]byte(dependent + "\x00" + dependency))
	return "usage-" + fmt.Sprintf("%x", h[:4])
}

// buildUsageResource constructs a single Usage resource as a protobuf Struct.
func buildUsageResource(dependent, dependency, apiVersion string) *structpb.Struct {
	return &structpb.Struct{
		Fields: map[string]*structpb.Value{
			"apiVersion": structpb.NewStringValue(apiVersion),
			"kind":       structpb.NewStringValue("Usage"),
			"metadata": structpb.NewStructValue(&structpb.Struct{
				Fields: map[string]*structpb.Value{
					"name": structpb.NewStringValue(usageName(dependent, dependency)),
				},
			}),
			"spec": structpb.NewStructValue(&structpb.Struct{
				Fields: map[string]*structpb.Value{
					"replayDeletion": structpb.NewBoolValue(true),
					"of": structpb.NewStructValue(&structpb.Struct{
						Fields: map[string]*structpb.Value{
							"resourceRef": structpb.NewStructValue(&structpb.Struct{
								Fields: map[string]*structpb.Value{
									"name": structpb.NewStringValue(dependency),
								},
							}),
						},
					}),
					"by": structpb.NewStructValue(&structpb.Struct{
						Fields: map[string]*structpb.Value{
							"resourceRef": structpb.NewStructValue(&structpb.Struct{
								Fields: map[string]*structpb.Value{
									"name": structpb.NewStringValue(dependent),
								},
							}),
						},
					}),
				},
			}),
		},
	}
}

// BuildUsageResources generates Usage resources for all dependency pairs.
// Returns a map keyed by Usage resource name.
func BuildUsageResources(deps []DependencyPair, apiVersion string) map[string]*structpb.Struct {
	result := make(map[string]*structpb.Struct, len(deps))
	for _, d := range deps {
		name := usageName(d.Dependent, d.Dependency)
		result[name] = buildUsageResource(d.Dependent, d.Dependency, apiVersion)
	}
	return result
}

// DetectUsageAPIVersion returns the Usage API version based on the user override.
// "v1" returns UsageAPIVersionV1, "v2" returns UsageAPIVersionV2.
// Empty or unrecognized values default to UsageAPIVersionV1 for maximum compatibility.
func DetectUsageAPIVersion(override string) string {
	switch override {
	case "v2":
		return UsageAPIVersionV2
	default:
		return UsageAPIVersionV1
	}
}
