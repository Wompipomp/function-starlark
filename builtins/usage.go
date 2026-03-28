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
	// UsageAPIVersionV1 is for Crossplane 1.x (v1.19+).
	UsageAPIVersionV1 = "apiextensions.crossplane.io/v1beta1"
	// UsageAPIVersionV2 is for Crossplane 2.x which moved Usage to a new API group.
	UsageAPIVersionV2 = "protection.crossplane.io/v1beta1"
)

// usageName produces a deterministic name for a Usage resource.
// Format: "usage-" + first 8 hex chars of sha256(dependent + NUL + dependency).
func usageName(dependent, dependency string) string {
	h := sha256.Sum256([]byte(dependent + "\x00" + dependency))
	return "usage-" + fmt.Sprintf("%x", h[:4])
}

// ResourceNameLabel is the label added to each composed resource by Resource()
// to enable Usage selector matching. The value is the composition-resource-name.
const ResourceNameLabel = "function-starlark.crossplane.io/resource-name"

// resourceTypeInfo holds the apiVersion and kind extracted from a composed resource body.
type resourceTypeInfo struct {
	APIVersion string
	Kind       string
}

// buildUsageResource constructs a single Usage resource as a protobuf Struct.
// Uses resourceSelector with matchControllerRef to match composed resources by
// label, since actual K8s resource names aren't known at pipeline time.
func buildUsageResource(dependent, dependency, apiVersion string, typeInfos map[string]resourceTypeInfo) *structpb.Struct {
	selector := func(name string) *structpb.Value {
		fields := map[string]*structpb.Value{
			"resourceSelector": structpb.NewStructValue(&structpb.Struct{
				Fields: map[string]*structpb.Value{
					"matchControllerRef": structpb.NewBoolValue(true),
					"matchLabels": structpb.NewStructValue(&structpb.Struct{
						Fields: map[string]*structpb.Value{
							ResourceNameLabel: structpb.NewStringValue(name),
						},
					}),
				},
			}),
		}
		if info, ok := typeInfos[name]; ok {
			fields["apiVersion"] = structpb.NewStringValue(info.APIVersion)
			fields["kind"] = structpb.NewStringValue(info.Kind)
		}
		return structpb.NewStructValue(&structpb.Struct{Fields: fields})
	}

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
					"of":             selector(dependency),
					"by":             selector(dependent),
				},
			}),
		},
	}
}

// BuildUsageResources generates Usage resources for all dependency pairs.
// resources is the map of collected resources (name -> CollectedResource) used
// to extract apiVersion/kind for Usage selectors.
// Returns a map keyed by Usage resource name.
func BuildUsageResources(deps []DependencyPair, apiVersion string, resources map[string]CollectedResource) map[string]*structpb.Struct {
	// Build type info map from collected resources.
	typeInfos := make(map[string]resourceTypeInfo, len(resources))
	for name, cr := range resources {
		if cr.Body == nil {
			continue
		}
		typeInfos[name] = resourceTypeInfo{
			APIVersion: cr.Body.GetFields()["apiVersion"].GetStringValue(),
			Kind:       cr.Body.GetFields()["kind"].GetStringValue(),
		}
	}

	result := make(map[string]*structpb.Struct, len(deps))
	for _, d := range deps {
		name := usageName(d.Dependent, d.Dependency)
		result[name] = buildUsageResource(d.Dependent, d.Dependency, apiVersion, typeInfos)
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

// WarnUnmatchedStringRefs checks for string refs (IsRef=false) in depends_on
// that do not match any resource created by the script. Returns a warning
// string for each unmatched ref. ResourceRef deps (IsRef=true) are skipped
// because they are already validated by ValidateDependencies.
func WarnUnmatchedStringRefs(deps []DependencyPair, resourceNames map[string]bool) []string {
	var warnings []string
	for _, d := range deps {
		if d.IsRef {
			continue
		}
		if !resourceNames[d.Dependency] {
			available := make([]string, 0, len(resourceNames))
			for name := range resourceNames {
				available = append(available, name)
			}
			sort.Strings(available)
			warnings = append(warnings, fmt.Sprintf(
				"depends_on: %q (string ref in Resource(%q)) does not match any resource created by this script; available resources: [%s]",
				d.Dependency, d.Dependent, strings.Join(available, ", ")))
		}
	}
	return warnings
}

// ResolveUsageAPIVersion returns the Usage API version based on the user override.
// "v1" returns UsageAPIVersionV1 (Crossplane 1.x), "v2" returns UsageAPIVersionV2.
// Empty or unrecognized values default to UsageAPIVersionV2 (Crossplane 2.x).
func ResolveUsageAPIVersion(override string) string {
	switch override {
	case "v1":
		return UsageAPIVersionV1
	default:
		return UsageAPIVersionV2
	}
}
