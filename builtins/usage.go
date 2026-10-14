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
// to enable Usage selector matching. The value is derived from the
// composition-resource-name via ResourceNameLabelValue.
const ResourceNameLabel = "function-starlark.crossplane.io/resource-name"

// maxLabelValueBytes is the Kubernetes limit for a label value. Resource names
// may be up to 253 bytes, so the composition-resource-name can legally exceed
// this, but it is also mirrored into ResourceNameLabel, which cannot.
const maxLabelValueBytes = 63

// ResourceNameLabelValue returns a label-safe (<=63 byte) value for
// ResourceNameLabel. The label is only an internal join key: collector.go
// stamps it on each composed resource and buildUsageResource references the
// same value in the Usage selector, so it need not equal the
// composition-resource-name. It only has to be stable and identical on both
// sides. Names within the limit are returned unchanged so already-applied
// resources and their Usages keep matching; longer names get a truncated
// prefix plus a sha256 suffix that stays a valid DNS-1123 label value.
func ResourceNameLabelValue(name string) string {
	if len(name) <= maxLabelValueBytes {
		return name
	}
	h := sha256.Sum256([]byte(name))
	const suffixLen = 16 // first 8 bytes of the digest as hex
	prefix := strings.TrimRight(name[:maxLabelValueBytes-1-suffixLen], "-")
	return prefix + "-" + fmt.Sprintf("%x", h[:8])
}

// resourceTypeInfo holds the apiVersion, kind, and namespace of a composed
// resource. Namespace is taken from the observed body: Crossplane injects the
// composite's namespace into namespaced composed resources at apply time, so
// the observed object carries it even when the desired body omitted it. An
// empty Namespace means the resource is cluster-scoped.
type resourceTypeInfo struct {
	APIVersion string
	Kind       string
	Namespace  string
}

// usageKind returns the Usage kind for one dependency pair. On the v2
// (protection.crossplane.io) API the Usage kind is namespaced and
// ClusterUsage is cluster-scoped: a pair is a Usage when either side lives in
// a namespace, and a ClusterUsage when both sides are cluster-scoped. The v1
// (apiextensions.crossplane.io) Usage is always cluster-scoped.
func usageKind(apiVersion string, namespaced bool) string {
	if apiVersion == UsageAPIVersionV2 && !namespaced {
		return "ClusterUsage"
	}
	return "Usage"
}

// usageNamespace is the namespace a v2 Usage must live in. The usage
// controller always looks up spec.by in the Usage's own namespace (there is
// no namespace override for by), so a namespaced dependent (by) dictates the
// namespace. spec.of may point at another namespace via
// resourceSelector.namespace, so a namespaced dependency (of) only decides
// when the dependent is cluster-scoped.
func usageNamespace(dependentNS, dependencyNS string) string {
	if dependentNS != "" {
		return dependentNS
	}
	return dependencyNS
}

// buildUsageResource constructs a single Usage resource as a protobuf Struct.
// Uses resourceSelector with matchControllerRef to match composed resources by
// label, since actual K8s resource names aren't known at pipeline time.
// namespace is the Usage's own metadata.namespace (empty for cluster-scoped
// kinds). ofNamespace, when non-empty, is written to spec.of.resourceSelector
// .namespace so a namespaced dependency in a different namespace than the
// Usage can still be resolved.
func buildUsageResource(dependent, dependency, apiVersion, kind, namespace, ofNamespace string, typeInfos map[string]resourceTypeInfo) *structpb.Struct {
	selector := func(name, selectorNS string) *structpb.Value {
		sel := map[string]*structpb.Value{
			"matchControllerRef": structpb.NewBoolValue(true),
			"matchLabels": structpb.NewStructValue(&structpb.Struct{
				Fields: map[string]*structpb.Value{
					ResourceNameLabel: structpb.NewStringValue(ResourceNameLabelValue(name)),
				},
			}),
		}
		if selectorNS != "" {
			sel["namespace"] = structpb.NewStringValue(selectorNS)
		}
		fields := map[string]*structpb.Value{
			"resourceSelector": structpb.NewStructValue(&structpb.Struct{Fields: sel}),
		}
		if info, ok := typeInfos[name]; ok {
			fields["apiVersion"] = structpb.NewStringValue(info.APIVersion)
			fields["kind"] = structpb.NewStringValue(info.Kind)
		}
		return structpb.NewStructValue(&structpb.Struct{Fields: fields})
	}

	fields := map[string]*structpb.Value{
		"apiVersion": structpb.NewStringValue(apiVersion),
		"kind":       structpb.NewStringValue(kind),
		"spec": structpb.NewStructValue(&structpb.Struct{
			Fields: map[string]*structpb.Value{
				"replayDeletion": structpb.NewBoolValue(true),
				"of":             selector(dependency, ofNamespace),
				"by":             selector(dependent, ""),
			},
		}),
	}
	if namespace != "" {
		fields["metadata"] = structpb.NewStructValue(&structpb.Struct{
			Fields: map[string]*structpb.Value{
				"namespace": structpb.NewStringValue(namespace),
			},
		})
	}
	return &structpb.Struct{Fields: fields}
}

// typeInfoFromBody extracts apiVersion, kind, and namespace from a resource
// body. A nil body yields a zero value.
func typeInfoFromBody(body *structpb.Struct) resourceTypeInfo {
	if body == nil {
		return resourceTypeInfo{}
	}
	return resourceTypeInfo{
		APIVersion: body.GetFields()["apiVersion"].GetStringValue(),
		Kind:       body.GetFields()["kind"].GetStringValue(),
		Namespace:  body.GetFields()["metadata"].GetStructValue().GetFields()["namespace"].GetStringValue(),
	}
}

// mergeTypeInfo fills an empty apiVersion/kind from fallback (the desired
// body) without touching Namespace, which must come from observed state.
func mergeTypeInfo(primary, fallback resourceTypeInfo) resourceTypeInfo {
	if primary.APIVersion == "" {
		primary.APIVersion = fallback.APIVersion
	}
	if primary.Kind == "" {
		primary.Kind = fallback.Kind
	}
	return primary
}

// usagePairReady is true when both sides of a depends_on pair are observed
// and ready, and any field-path gate on the dependency is truthy.
func usagePairReady(d DependencyPair, observed map[string]*structpb.Struct) bool {
	by := observed[d.Dependent]
	of := observed[d.Dependency]
	if !isObservedReady(by) || !isObservedReady(of) {
		return false
	}
	if d.FieldPath != "" && !isFieldReady(of, d.FieldPath) {
		return false
	}
	return true
}

// BuildUsageResources generates Usage resources for the depends_on pairs
// whose dependent and dependency are both observed and ready. resources is
// the map of collected resources (name -> CollectedResource) used as a
// fallback for apiVersion/kind; observed holds the observed composed resource
// bodies keyed by composition resource name.
//
// Namespace, and therefore Usage vs ClusterUsage on the v2 API, is read from
// the observed bodies only: Crossplane injects a namespaced XR's namespace
// into its composed resources at apply time, so a desired body that simply
// omits metadata.namespace must not be mistaken for a cluster-scoped
// resource. Pairs that are not yet observed and ready are left out and
// picked up on a later reconciliation. On the v2 API a pair with a
// namespaced side becomes a Usage in the dependent's namespace (or the
// dependency's when the dependent is cluster-scoped), with
// spec.of.resourceSelector.namespace set when the dependency lives in yet
// another namespace; a pair with two cluster-scoped sides becomes a
// ClusterUsage. Returns a map keyed by Usage resource name.
func BuildUsageResources(deps []DependencyPair, apiVersion string, resources map[string]CollectedResource, observed map[string]*structpb.Struct) map[string]*structpb.Struct {
	desiredInfo := make(map[string]resourceTypeInfo, len(resources))
	for name, cr := range resources {
		desiredInfo[name] = typeInfoFromBody(cr.Body)
	}

	result := make(map[string]*structpb.Struct)
	for _, d := range deps {
		if !usagePairReady(d, observed) {
			continue
		}
		byInfo := mergeTypeInfo(typeInfoFromBody(observed[d.Dependent]), desiredInfo[d.Dependent])
		ofInfo := mergeTypeInfo(typeInfoFromBody(observed[d.Dependency]), desiredInfo[d.Dependency])
		typeInfos := map[string]resourceTypeInfo{
			d.Dependent:  byInfo,
			d.Dependency: ofInfo,
		}
		byNS, ofNS := byInfo.Namespace, ofInfo.Namespace
		kind := usageKind(apiVersion, byNS != "" || ofNS != "")
		namespace, ofSelectorNS := "", ""
		if apiVersion == UsageAPIVersionV2 && kind == "Usage" {
			namespace = usageNamespace(byNS, ofNS)
			if ofNS != "" && ofNS != namespace {
				ofSelectorNS = ofNS
			}
		}
		name := usageName(d.Dependent, d.Dependency)
		result[name] = buildUsageResource(d.Dependent, d.Dependency, apiVersion, kind, namespace, ofSelectorNS, typeInfos)
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
