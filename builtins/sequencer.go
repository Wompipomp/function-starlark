package builtins

import (
	"fmt"
	"sort"
	"strings"
)

// SequencerResult holds the output of creation sequencing evaluation.
type SequencerResult struct {
	Deferred    []string         // resource names to withhold from desired state
	Events      []CollectedEvent // Warning events for each deferred resource + summary
	AnyDeferred bool             // true if any resources were deferred (caller sets short TTL)
}

// Sequencer evaluates creation ordering based on dependency relationships
// and observed state. Resources whose dependencies are not yet observed
// are deferred (withheld from desired state).
type Sequencer struct {
	deps          []DependencyPair
	resourceNames map[string]bool
	observedNames map[string]bool
	ttlSeconds    int
}

// NewSequencer creates a Sequencer with the given inputs.
func NewSequencer(
	deps []DependencyPair,
	resourceNames map[string]bool,
	observedNames map[string]bool,
	ttlSeconds int,
) *Sequencer {
	return &Sequencer{
		deps:          deps,
		resourceNames: resourceNames,
		observedNames: observedNames,
		ttlSeconds:    ttlSeconds,
	}
}

// Evaluate runs creation sequencing and returns the result.
// Resources already in observed state are NEVER deferred (SEQ-03).
// A resource is deferred if ANY of its dependencies are not in observed (AND semantics).
func (s *Sequencer) Evaluate() SequencerResult {
	// Build map: resource -> list of unmet dependencies.
	unmetDeps := make(map[string][]string)
	for _, d := range s.deps {
		if !s.observedNames[d.Dependency] {
			unmetDeps[d.Dependent] = append(unmetDeps[d.Dependent], d.Dependency)
		}
	}

	var deferred []string
	var events []CollectedEvent

	for name := range unmetDeps {
		// SEQ-03: Never defer resources already in observed state.
		if s.observedNames[name] {
			continue
		}
		deferred = append(deferred, name)
	}
	sort.Strings(deferred) // deterministic ordering

	for _, name := range deferred {
		missing := unmetDeps[name]
		sort.Strings(missing)
		msg := fmt.Sprintf(
			"Creation sequencing: resource %q deferred, waiting for %s to be observed",
			name, quotedJoin(missing),
		)
		events = append(events, CollectedEvent{
			Severity: "Warning",
			Message:  msg,
			Target:   "Composite",
		})
	}

	anyDeferred := len(deferred) > 0
	if anyDeferred {
		events = append(events, CollectedEvent{
			Severity: "Warning",
			Message: fmt.Sprintf(
				"Creation sequencing: %d resource(s) deferred; requeuing in %ds",
				len(deferred), s.ttlSeconds,
			),
			Target: "Composite",
		})
	}

	return SequencerResult{
		Deferred:    deferred,
		Events:      events,
		AnyDeferred: anyDeferred,
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
