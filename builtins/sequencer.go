package builtins

import (
	"fmt"
	"sort"
	"strings"

	"google.golang.org/protobuf/types/known/structpb"
)

// SequencerResult holds the output of creation sequencing evaluation.
type SequencerResult struct {
	Deferred     []string         // resource names to withhold from desired state
	Events       []CollectedEvent // Warning events for each deferred resource + summary
	GatingDefers []GatingDefer    // per-resource unmet-dep description for composite-ready gating
	AnyDeferred  bool             // true if any resources were deferred (caller sets short TTL)
}

// Sequencer evaluates creation ordering based on dependency relationships
// and observed state. Resources whose dependencies are not yet observed
// (or whose field path values are not ready) are deferred (withheld from
// desired state).
type Sequencer struct {
	deps              []DependencyPair
	resourceNames     map[string]bool
	observedResources map[string]*structpb.Struct
	ttlSeconds        int
}

// NewSequencer creates a Sequencer with the given inputs.
func NewSequencer(
	deps []DependencyPair,
	resourceNames map[string]bool,
	observedResources map[string]*structpb.Struct,
	ttlSeconds int,
) *Sequencer {
	return &Sequencer{
		deps:              deps,
		resourceNames:     resourceNames,
		observedResources: observedResources,
		ttlSeconds:        ttlSeconds,
	}
}

// Evaluate runs creation sequencing and returns the result.
// Resources already in observed state are NEVER deferred (SEQ-03).
// A resource is deferred if ANY of its dependencies are not met (AND semantics).
// A dependency is met when:
//   - FieldPath == "": the dependency resource exists AND has Ready=True + Synced=True conditions
//   - FieldPath != "": the dependency resource exists AND the field path has a truthy value
func (s *Sequencer) Evaluate() SequencerResult {
	// Build map: resource -> list of unmet dependency descriptions.
	unmetDeps := make(map[string][]string)
	for _, d := range s.deps {
		res, exists := s.observedResources[d.Dependency]
		if !exists {
			// Resource not observed at all.
			unmetDeps[d.Dependent] = append(unmetDeps[d.Dependent],
				fmt.Sprintf("%q to be observed", d.Dependency))
			continue
		}
		if d.FieldPath != "" {
			// Tuple syntax: check specific field path.
			if !isFieldReady(res, d.FieldPath) {
				unmetDeps[d.Dependent] = append(unmetDeps[d.Dependent],
					fmt.Sprintf("%q field %q to be ready", d.Dependency, d.FieldPath))
			}
		} else {
			// Plain ref: check Ready=True and Synced=True conditions.
			if !hasCondition(res, "Ready", "True") {
				unmetDeps[d.Dependent] = append(unmetDeps[d.Dependent],
					fmt.Sprintf("%q to have Ready=True", d.Dependency))
			}
			if !hasCondition(res, "Synced", "True") {
				unmetDeps[d.Dependent] = append(unmetDeps[d.Dependent],
					fmt.Sprintf("%q to have Synced=True", d.Dependency))
			}
		}
	}

	var deferred []string
	var events []CollectedEvent

	for name := range unmetDeps {
		// SEQ-03: Never defer resources already in observed state.
		if _, observed := s.observedResources[name]; observed {
			continue
		}
		deferred = append(deferred, name)
	}
	sort.Strings(deferred) // deterministic ordering

	gatingDefers := make([]GatingDefer, 0, len(deferred))
	for _, name := range deferred {
		gatingDefers = append(gatingDefers, GatingDefer{
			Name:   name,
			Reason: strings.Join(unmetDeps[name], "; "),
		})
	}

	anyDeferred := len(deferred) > 0
	if anyDeferred {
		// Stable message (only resource names, no changing reasons) so Kubernetes
		// deduplicates repeated events into a single entry with a counter.
		events = append(events, CollectedEvent{
			Severity: "Warning",
			Message: fmt.Sprintf(
				"Creation sequencing: %d resource(s) deferred: %s",
				len(deferred), strings.Join(deferred, ", "),
			),
			Target: "Composite",
		})
	}

	return SequencerResult{
		Deferred:     deferred,
		Events:       events,
		GatingDefers: gatingDefers,
		AnyDeferred:  anyDeferred,
	}
}

// hasCondition checks whether a resource has a condition with the given type
// and status in its status.conditions[] array. Crossplane conditions follow
// the standard structure: {type: "Ready", status: "True", ...}.
func hasCondition(s *structpb.Struct, condType, condStatus string) bool {
	if s == nil {
		return false
	}
	status := s.GetFields()["status"]
	if status == nil {
		return false
	}
	statusStruct := status.GetStructValue()
	if statusStruct == nil {
		return false
	}
	conditions := statusStruct.GetFields()["conditions"]
	if conditions == nil {
		return false
	}
	condList := conditions.GetListValue()
	if condList == nil {
		return false
	}
	for _, item := range condList.GetValues() {
		cond := item.GetStructValue()
		if cond == nil {
			continue
		}
		t := cond.GetFields()["type"]
		s := cond.GetFields()["status"]
		if t != nil && s != nil &&
			t.GetStringValue() == condType &&
			s.GetStringValue() == condStatus {
			return true
		}
	}
	return false
}

// isFieldReady checks whether a dot-separated field path has a truthy value
// in the given struct. Returns false if any intermediate segment is missing
// or the final value is falsy (nil, null, empty string, zero, false).
func isFieldReady(s *structpb.Struct, fieldPath string) bool {
	if s == nil {
		return false
	}

	keys := strings.Split(fieldPath, ".")
	current := s

	for i, key := range keys {
		if current.GetFields() == nil {
			return false
		}
		val, ok := current.Fields[key]
		if !ok || val == nil {
			return false
		}

		// Last key: check truthiness of the value.
		if i == len(keys)-1 {
			return isValueTruthy(val)
		}

		// Intermediate key: must be a struct to continue traversal.
		next := val.GetStructValue()
		if next == nil {
			return false
		}
		current = next
	}
	return false
}

// isValueTruthy returns whether a structpb.Value is considered "ready".
//   - nil or missing -> false
//   - NullValue -> false
//   - StringValue("") -> false
//   - NumberValue(0) -> false
//   - BoolValue(false) -> false
//   - Everything else (non-empty string, non-zero number, true, struct, list) -> true
func isValueTruthy(v *structpb.Value) bool {
	if v == nil {
		return false
	}
	switch k := v.Kind.(type) {
	case *structpb.Value_NullValue:
		return false
	case *structpb.Value_StringValue:
		return k.StringValue != ""
	case *structpb.Value_NumberValue:
		return k.NumberValue != 0
	case *structpb.Value_BoolValue:
		return k.BoolValue
	case *structpb.Value_StructValue:
		return true
	case *structpb.Value_ListValue:
		return true
	default:
		return false
	}
}

// quotedJoin returns quoted, comma-separated names: "db", "cache".
func quotedJoin(names []string) string {
	quoted := make([]string, len(names))
	for i, n := range names {
		quoted[i] = fmt.Sprintf("%q", n)
	}
	return strings.Join(quoted, ", ")
}
