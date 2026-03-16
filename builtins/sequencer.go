package builtins

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
// TODO: implement in GREEN phase.
func (s *Sequencer) Evaluate() SequencerResult {
	return SequencerResult{}
}
