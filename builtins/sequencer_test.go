package builtins

import (
	"strings"
	"testing"

	"google.golang.org/protobuf/types/known/structpb"
)

func TestSequencer(t *testing.T) {
	tests := []struct {
		name            string
		deps            []DependencyPair
		resourceNames   map[string]bool
		observedNames   map[string]bool
		ttlSeconds      int
		wantDeferred    []string
		wantAnyDeferred bool
		wantEventCount  int
		// Optional: check specific event message substrings.
		wantEventMsgs []string
	}{
		{
			name:            "NoDeps",
			deps:            nil,
			resourceNames:   map[string]bool{"app": true},
			observedNames:   map[string]bool{},
			ttlSeconds:      10,
			wantDeferred:    nil,
			wantAnyDeferred: false,
			wantEventCount:  0,
		},
		{
			name: "AllDepsObserved",
			deps: []DependencyPair{
				{Dependent: "app", Dependency: "db", IsRef: true},
			},
			resourceNames:   map[string]bool{"app": true, "db": true},
			observedNames:   map[string]bool{"db": true},
			ttlSeconds:      10,
			wantDeferred:    nil,
			wantAnyDeferred: false,
			wantEventCount:  0,
		},
		{
			name: "SingleDepMissing",
			deps: []DependencyPair{
				{Dependent: "app", Dependency: "db", IsRef: true},
			},
			resourceNames:   map[string]bool{"app": true, "db": true},
			observedNames:   map[string]bool{},
			ttlSeconds:      10,
			wantDeferred:    []string{"app"},
			wantAnyDeferred: true,
			wantEventCount:  2, // per-resource + summary
			wantEventMsgs: []string{
				`Creation sequencing: resource "app" deferred, waiting for "db" to be observed`,
				`Creation sequencing: 1 resource(s) deferred; requeuing in 10s`,
			},
		},
		{
			name: "MultipleMissingDeps",
			deps: []DependencyPair{
				{Dependent: "app", Dependency: "db", IsRef: true},
				{Dependent: "app", Dependency: "cache", IsRef: false},
			},
			resourceNames:   map[string]bool{"app": true, "db": true, "cache": true},
			observedNames:   map[string]bool{},
			ttlSeconds:      10,
			wantDeferred:    []string{"app"},
			wantAnyDeferred: true,
			wantEventCount:  2,
			wantEventMsgs: []string{
				// Missing deps listed sorted: "cache", "db"
				`Creation sequencing: resource "app" deferred, waiting for "cache", "db" to be observed`,
				`Creation sequencing: 1 resource(s) deferred; requeuing in 10s`,
			},
		},
		{
			name: "NeverDeferObservedResource",
			deps: []DependencyPair{
				{Dependent: "app", Dependency: "db", IsRef: true},
			},
			resourceNames:   map[string]bool{"app": true, "db": true},
			observedNames:   map[string]bool{"app": true}, // app observed, db NOT
			ttlSeconds:      10,
			wantDeferred:    nil,
			wantAnyDeferred: false,
			wantEventCount:  0,
		},
		{
			name: "BothObservedNeitherDeferred",
			deps: []DependencyPair{
				{Dependent: "app", Dependency: "db", IsRef: true},
			},
			resourceNames:   map[string]bool{"app": true, "db": true},
			observedNames:   map[string]bool{"app": true, "db": true},
			ttlSeconds:      10,
			wantDeferred:    nil,
			wantAnyDeferred: false,
			wantEventCount:  0,
		},
		{
			name: "TransitiveChainNothingObserved",
			// A->B->C: B depends on A, C depends on B
			deps: []DependencyPair{
				{Dependent: "B", Dependency: "A", IsRef: true},
				{Dependent: "C", Dependency: "B", IsRef: true},
			},
			resourceNames:   map[string]bool{"A": true, "B": true, "C": true},
			observedNames:   map[string]bool{},
			ttlSeconds:      10,
			wantDeferred:    []string{"B", "C"}, // sorted
			wantAnyDeferred: true,
			wantEventCount:  3, // 2 per-resource + 1 summary
			wantEventMsgs: []string{
				`Creation sequencing: resource "B" deferred, waiting for "A" to be observed`,
				`Creation sequencing: resource "C" deferred, waiting for "B" to be observed`,
				`Creation sequencing: 2 resource(s) deferred; requeuing in 10s`,
			},
		},
		{
			name: "TransitiveChainPartialObserved",
			// A->B->C: A is observed. B's dep (A) is met, so B not deferred.
			// C's dep (B) is not observed, so C is deferred.
			deps: []DependencyPair{
				{Dependent: "B", Dependency: "A", IsRef: true},
				{Dependent: "C", Dependency: "B", IsRef: true},
			},
			resourceNames:   map[string]bool{"A": true, "B": true, "C": true},
			observedNames:   map[string]bool{"A": true},
			ttlSeconds:      10,
			wantDeferred:    []string{"C"},
			wantAnyDeferred: true,
			wantEventCount:  2, // per-resource + summary
			wantEventMsgs: []string{
				`Creation sequencing: resource "C" deferred, waiting for "B" to be observed`,
				`Creation sequencing: 1 resource(s) deferred; requeuing in 10s`,
			},
		},
		{
			name: "SummaryEventWithTTL",
			deps: []DependencyPair{
				{Dependent: "app", Dependency: "db", IsRef: true},
			},
			resourceNames:   map[string]bool{"app": true, "db": true},
			observedNames:   map[string]bool{},
			ttlSeconds:      30,
			wantDeferred:    []string{"app"},
			wantAnyDeferred: true,
			wantEventCount:  2,
			wantEventMsgs: []string{
				`Creation sequencing: resource "app" deferred, waiting for "db" to be observed`,
				`requeuing in 30s`, // TTL=30s in summary
			},
		},
		{
			name: "DeterministicOrder",
			deps: []DependencyPair{
				{Dependent: "zeta", Dependency: "alpha", IsRef: true},
				{Dependent: "beta", Dependency: "alpha", IsRef: true},
				{Dependent: "gamma", Dependency: "alpha", IsRef: true},
			},
			resourceNames:   map[string]bool{"alpha": true, "beta": true, "gamma": true, "zeta": true},
			observedNames:   map[string]bool{},
			ttlSeconds:      10,
			wantDeferred:    []string{"beta", "gamma", "zeta"}, // sorted
			wantAnyDeferred: true,
			wantEventCount:  4, // 3 per-resource + 1 summary
		},
		{
			name: "ANDSemantics",
			// app depends on both db and cache; db is observed, cache is not.
			// Because not ALL deps are met, app is deferred.
			deps: []DependencyPair{
				{Dependent: "app", Dependency: "db", IsRef: true},
				{Dependent: "app", Dependency: "cache", IsRef: false},
			},
			resourceNames:   map[string]bool{"app": true, "db": true, "cache": true},
			observedNames:   map[string]bool{"db": true},
			ttlSeconds:      10,
			wantDeferred:    []string{"app"},
			wantAnyDeferred: true,
			wantEventCount:  2,
			wantEventMsgs: []string{
				// Only cache is listed as missing (db is observed)
				`Creation sequencing: resource "app" deferred, waiting for "cache" to be observed`,
			},
		},
		{
			name: "SeparateFromSkip",
			// Verify result struct uses Deferred (not Skipped) and events use
			// "Creation sequencing:" prefix, not "Skipping resource".
			deps: []DependencyPair{
				{Dependent: "svc", Dependency: "ns", IsRef: true},
			},
			resourceNames:   map[string]bool{"svc": true, "ns": true},
			observedNames:   map[string]bool{},
			ttlSeconds:      10,
			wantDeferred:    []string{"svc"},
			wantAnyDeferred: true,
			wantEventCount:  2,
			wantEventMsgs: []string{
				`Creation sequencing:`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			seq := NewSequencer(tt.deps, tt.resourceNames, tt.observedNames, tt.ttlSeconds)
			result := seq.Evaluate()

			// Check AnyDeferred.
			if result.AnyDeferred != tt.wantAnyDeferred {
				t.Errorf("AnyDeferred = %v, want %v", result.AnyDeferred, tt.wantAnyDeferred)
			}

			// Check Deferred list.
			if tt.wantDeferred == nil && result.Deferred != nil {
				// Allow empty slice to match nil expectation.
				if len(result.Deferred) != 0 {
					t.Errorf("Deferred = %v, want nil", result.Deferred)
				}
			} else if len(result.Deferred) != len(tt.wantDeferred) {
				t.Errorf("Deferred = %v, want %v", result.Deferred, tt.wantDeferred)
			} else {
				for i, got := range result.Deferred {
					if got != tt.wantDeferred[i] {
						t.Errorf("Deferred[%d] = %q, want %q", i, got, tt.wantDeferred[i])
					}
				}
			}

			// Check event count.
			if len(result.Events) != tt.wantEventCount {
				t.Errorf("Events count = %d, want %d", len(result.Events), tt.wantEventCount)
				for i, e := range result.Events {
					t.Logf("  Events[%d]: %s", i, e.Message)
				}
			}

			// Check event messages contain expected substrings.
			for _, want := range tt.wantEventMsgs {
				found := false
				for _, e := range result.Events {
					if strings.Contains(e.Message, want) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("no event message contains %q", want)
					for i, e := range result.Events {
						t.Logf("  Events[%d]: %s", i, e.Message)
					}
				}
			}

			// SEQ-06: Verify all events use "Creation sequencing:" prefix when deferred.
			if result.AnyDeferred {
				for i, e := range result.Events {
					if !strings.HasPrefix(e.Message, "Creation sequencing:") {
						t.Errorf("Events[%d] message %q does not have 'Creation sequencing:' prefix", i, e.Message)
					}
					if strings.Contains(e.Message, "Skipping resource") {
						t.Errorf("Events[%d] message %q contains 'Skipping resource' (should be separate from skip)", i, e.Message)
					}
				}
			}

			// Verify all events have Warning severity.
			for i, e := range result.Events {
				if e.Severity != "Warning" {
					t.Errorf("Events[%d].Severity = %q, want \"Warning\"", i, e.Severity)
				}
			}
		})
	}
}

func TestRemoveResources(t *testing.T) {
	cc := NewConditionCollector()
	c := NewCollector(cc, "test.star")

	// Add some resources manually via internal map (simulate Resource() calls).
	c.mu.Lock()
	c.resources["app"] = CollectedResource{
		Name: "app",
		Body: &structpb.Struct{Fields: map[string]*structpb.Value{}},
	}
	c.resources["db"] = CollectedResource{
		Name: "db",
		Body: &structpb.Struct{Fields: map[string]*structpb.Value{}},
	}
	c.resources["cache"] = CollectedResource{
		Name: "cache",
		Body: &structpb.Struct{Fields: map[string]*structpb.Value{}},
	}
	c.mu.Unlock()

	// Remove "app" and "cache".
	c.RemoveResources([]string{"app", "cache"})

	resources := c.Resources()
	if len(resources) != 1 {
		t.Fatalf("Resources count = %d, want 1", len(resources))
	}
	if _, ok := resources["db"]; !ok {
		t.Error("expected 'db' to remain in resources")
	}
	if _, ok := resources["app"]; ok {
		t.Error("expected 'app' to be removed")
	}
	if _, ok := resources["cache"]; ok {
		t.Error("expected 'cache' to be removed")
	}
}

func TestRemoveResourcesNonExistent(t *testing.T) {
	cc := NewConditionCollector()
	c := NewCollector(cc, "test.star")

	c.mu.Lock()
	c.resources["app"] = CollectedResource{
		Name: "app",
		Body: &structpb.Struct{Fields: map[string]*structpb.Value{}},
	}
	c.mu.Unlock()

	// Removing non-existent resource should not panic.
	c.RemoveResources([]string{"nonexistent"})

	resources := c.Resources()
	if len(resources) != 1 {
		t.Fatalf("Resources count = %d, want 1", len(resources))
	}
}

func TestAddEvent(t *testing.T) {
	cc := NewConditionCollector()

	event := CollectedEvent{
		Severity: "Warning",
		Message:  "Creation sequencing: test event",
		Target:   "Composite",
	}
	cc.AddEvent(event)

	events := cc.Events()
	if len(events) != 1 {
		t.Fatalf("Events count = %d, want 1", len(events))
	}
	if events[0].Severity != "Warning" {
		t.Errorf("Events[0].Severity = %q, want \"Warning\"", events[0].Severity)
	}
	if events[0].Message != "Creation sequencing: test event" {
		t.Errorf("Events[0].Message = %q, want \"Creation sequencing: test event\"", events[0].Message)
	}
	if events[0].Target != "Composite" {
		t.Errorf("Events[0].Target = %q, want \"Composite\"", events[0].Target)
	}
}

func TestAddEventMultiple(t *testing.T) {
	cc := NewConditionCollector()

	cc.AddEvent(CollectedEvent{Severity: "Warning", Message: "msg1", Target: "Composite"})
	cc.AddEvent(CollectedEvent{Severity: "Normal", Message: "msg2", Target: "Composite"})

	events := cc.Events()
	if len(events) != 2 {
		t.Fatalf("Events count = %d, want 2", len(events))
	}
	if events[0].Message != "msg1" {
		t.Errorf("Events[0].Message = %q, want \"msg1\"", events[0].Message)
	}
	if events[1].Message != "msg2" {
		t.Errorf("Events[1].Message = %q, want \"msg2\"", events[1].Message)
	}
}

func TestQuotedJoin(t *testing.T) {
	tests := []struct {
		name  string
		input []string
		want  string
	}{
		{name: "Empty", input: nil, want: ""},
		{name: "Single", input: []string{"db"}, want: `"db"`},
		{name: "Multiple", input: []string{"cache", "db"}, want: `"cache", "db"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := quotedJoin(tt.input)
			if got != tt.want {
				t.Errorf("quotedJoin(%v) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestSequencerEdgeCases(t *testing.T) {
	tests := []struct {
		name            string
		deps            []DependencyPair
		resourceNames   map[string]bool
		observedNames   map[string]bool
		ttlSeconds      int
		wantDeferred    []string
		wantAnyDeferred bool
		wantEventCount  int
		wantEventMsgs   []string
	}{
		{
			name:            "EmptyDepsSlice",
			deps:            []DependencyPair{},
			resourceNames:   map[string]bool{"app": true},
			observedNames:   map[string]bool{},
			ttlSeconds:      10,
			wantDeferred:    nil,
			wantAnyDeferred: false,
			wantEventCount:  0,
		},
		{
			name: "SelfDependency",
			deps: []DependencyPair{
				{Dependent: "app", Dependency: "app", IsRef: true},
			},
			resourceNames:   map[string]bool{"app": true},
			observedNames:   map[string]bool{},
			ttlSeconds:      10,
			wantDeferred:    []string{"app"},
			wantAnyDeferred: true,
			wantEventCount:  2,
			wantEventMsgs: []string{
				`Creation sequencing: resource "app" deferred, waiting for "app" to be observed`,
			},
		},
		{
			name: "DependencyNotInResourceNames",
			deps: []DependencyPair{
				{Dependent: "app", Dependency: "external-db", IsRef: false},
			},
			resourceNames:   map[string]bool{"app": true},
			observedNames:   map[string]bool{},
			ttlSeconds:      10,
			wantDeferred:    []string{"app"},
			wantAnyDeferred: true,
			wantEventCount:  2,
			wantEventMsgs: []string{
				`Creation sequencing: resource "app" deferred, waiting for "external-db" to be observed`,
			},
		},
		{
			name: "ZeroTTL",
			deps: []DependencyPair{
				{Dependent: "app", Dependency: "db", IsRef: true},
			},
			resourceNames:   map[string]bool{"app": true, "db": true},
			observedNames:   map[string]bool{},
			ttlSeconds:      0,
			wantDeferred:    []string{"app"},
			wantAnyDeferred: true,
			wantEventCount:  2,
			wantEventMsgs: []string{
				`requeuing in 0s`,
			},
		},
		{
			name: "DependentNotInResourceNames",
			deps: []DependencyPair{
				{Dependent: "phantom", Dependency: "db", IsRef: true},
			},
			resourceNames:   map[string]bool{"db": true},
			observedNames:   map[string]bool{},
			ttlSeconds:      10,
			wantDeferred:    []string{"phantom"},
			wantAnyDeferred: true,
			wantEventCount:  2,
			wantEventMsgs: []string{
				`Creation sequencing: resource "phantom" deferred, waiting for "db" to be observed`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			seq := NewSequencer(tt.deps, tt.resourceNames, tt.observedNames, tt.ttlSeconds)
			result := seq.Evaluate()

			if result.AnyDeferred != tt.wantAnyDeferred {
				t.Errorf("AnyDeferred = %v, want %v", result.AnyDeferred, tt.wantAnyDeferred)
			}

			if tt.wantDeferred == nil && result.Deferred != nil {
				if len(result.Deferred) != 0 {
					t.Errorf("Deferred = %v, want nil", result.Deferred)
				}
			} else if len(result.Deferred) != len(tt.wantDeferred) {
				t.Errorf("Deferred = %v, want %v", result.Deferred, tt.wantDeferred)
			} else {
				for i, got := range result.Deferred {
					if got != tt.wantDeferred[i] {
						t.Errorf("Deferred[%d] = %q, want %q", i, got, tt.wantDeferred[i])
					}
				}
			}

			if len(result.Events) != tt.wantEventCount {
				t.Errorf("Events count = %d, want %d", len(result.Events), tt.wantEventCount)
				for i, e := range result.Events {
					t.Logf("  Events[%d]: %s", i, e.Message)
				}
			}

			for _, want := range tt.wantEventMsgs {
				found := false
				for _, e := range result.Events {
					if strings.Contains(e.Message, want) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("no event message contains %q", want)
				}
			}
		})
	}
}
