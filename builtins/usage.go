package builtins

import (
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"

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

// ValidateDependencies checks dependency pairs for missing targets and circular dependencies.
// Missing target validation only applies to ResourceRef dependencies (IsRef=true).
// Cycle detection applies to all dependency pairs.
func ValidateDependencies(deps []DependencyPair, resourceNames map[string]bool) error {
	// Step 1: Missing target check (ResourceRef only).
	for _, d := range deps {
		if !d.IsRef {
			continue
		}
		if !resourceNames[d.Dependency] {
			available := make([]string, 0, len(resourceNames))
			for name := range resourceNames {
				available = append(available, name)
			}
			sort.Strings(available)
			return fmt.Errorf("Resource(%q): depends_on references %q which was not created by this script; available resources: [%s]",
				d.Dependent, d.Dependency, strings.Join(available, ", "))
		}
	}

	// Step 2: Cycle detection using DFS with 3-color marking.
	// Build adjacency list from all dependency pairs.
	adj := make(map[string][]string)
	nodes := make(map[string]bool)
	for _, d := range deps {
		adj[d.Dependent] = append(adj[d.Dependent], d.Dependency)
		nodes[d.Dependent] = true
		nodes[d.Dependency] = true
	}

	const (
		white = 0 // unvisited
		gray  = 1 // in current DFS path
		black = 2 // fully processed
	)
	color := make(map[string]int)
	parent := make(map[string]string)

	var dfs func(node string) error
	dfs = func(node string) error {
		color[node] = gray
		for _, neighbor := range adj[node] {
			if color[neighbor] == gray {
				// Found cycle -- reconstruct path.
				cycle := []string{neighbor, node}
				cur := node
				for cur != neighbor {
					cur = parent[cur]
					cycle = append(cycle, cur)
				}
				// Reverse to get forward order.
				for i, j := 0, len(cycle)-1; i < j; i, j = i+1, j-1 {
					cycle[i], cycle[j] = cycle[j], cycle[i]
				}
				return fmt.Errorf("circular dependency detected: %s", strings.Join(cycle, " -> "))
			}
			if color[neighbor] == white {
				parent[neighbor] = node
				if err := dfs(neighbor); err != nil {
					return err
				}
			}
		}
		color[node] = black
		return nil
	}

	// Sort node names for deterministic traversal order.
	sortedNodes := make([]string, 0, len(nodes))
	for n := range nodes {
		sortedNodes = append(sortedNodes, n)
	}
	sort.Strings(sortedNodes)

	for _, node := range sortedNodes {
		if color[node] == white {
			if err := dfs(node); err != nil {
				return err
			}
		}
	}

	return nil
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
