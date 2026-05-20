package builtins

import (
	"strings"
	"testing"

	"google.golang.org/protobuf/types/known/structpb"
)

// emptyStruct returns a *structpb.Struct with no fields.
func emptyStruct() *structpb.Struct {
	return &structpb.Struct{Fields: map[string]*structpb.Value{}}
}

// readyStruct returns a *structpb.Struct with Ready=True and Synced=True
// conditions, representing a fully ready Crossplane resource.
func readyStruct() *structpb.Struct {
	s, _ := structpb.NewStruct(map[string]any{
		"status": map[string]any{
			"conditions": []any{
				map[string]any{"type": "Ready", "status": "True"},
				map[string]any{"type": "Synced", "status": "True"},
			},
		},
	})
	return s
}

// k8sObjectStruct builds a structpb.Struct representing a Kubernetes Object
// with top-level apiVersion/kind, wrapper conditions, and optional nested
// resource conditions at status.atProvider.manifest.status.conditions.
func k8sObjectStruct(apiVersion string, wrapperReady bool, nestedConditions []map[string]any) *structpb.Struct {
	wrapperStatus := "False"
	if wrapperReady {
		wrapperStatus = "True"
	}

	statusMap := map[string]any{
		"conditions": []any{
			map[string]any{"type": "Ready", "status": wrapperStatus},
			map[string]any{"type": "Synced", "status": "True"},
		},
	}

	if nestedConditions != nil {
		condList := make([]any, len(nestedConditions))
		for i, c := range nestedConditions {
			condList[i] = c
		}
		statusMap["atProvider"] = map[string]any{
			"manifest": map[string]any{
				"apiVersion": "apps/v1",
				"kind":       "Deployment",
				"status": map[string]any{
					"conditions": condList,
				},
			},
		}
	}

	s, _ := structpb.NewStruct(map[string]any{
		"apiVersion": apiVersion,
		"kind":       "Object",
		"status":     statusMap,
	})
	return s
}

func TestSequencer(t *testing.T) {
	tests := []struct {
		name              string
		deps              []DependencyPair
		resourceNames     map[string]bool
		observedResources map[string]*structpb.Struct
		ttlSeconds        int
		wantDeferred      []string
		wantAnyDeferred   bool
		wantEventCount    int
		// Optional: check specific event message substrings.
		wantEventMsgs []string
	}{
		{
			name:              "NoDeps",
			deps:              nil,
			resourceNames:     map[string]bool{"app": true},
			observedResources: map[string]*structpb.Struct{},
			ttlSeconds:        10,
			wantDeferred:      nil,
			wantAnyDeferred:   false,
			wantEventCount:    0,
		},
		{
			name: "AllDepsObservedAndReady",
			deps: []DependencyPair{
				{Dependent: "app", Dependency: "db", IsRef: true},
			},
			resourceNames:     map[string]bool{"app": true, "db": true},
			observedResources: map[string]*structpb.Struct{"db": readyStruct()},
			ttlSeconds:        10,
			wantDeferred:      nil,
			wantAnyDeferred:   false,
			wantEventCount:    0,
		},
		{
			name: "SingleDepMissing",
			deps: []DependencyPair{
				{Dependent: "app", Dependency: "db", IsRef: true},
			},
			resourceNames:     map[string]bool{"app": true, "db": true},
			observedResources: map[string]*structpb.Struct{},
			ttlSeconds:        10,
			wantDeferred:      []string{"app"},
			wantAnyDeferred:   true,
			wantEventCount:    1, // single consolidated event
			wantEventMsgs: []string{
				`1 resource(s) deferred: app`,
			},
		},
		{
			name: "MultipleMissingDeps",
			deps: []DependencyPair{
				{Dependent: "app", Dependency: "db", IsRef: true},
				{Dependent: "app", Dependency: "cache", IsRef: false},
			},
			resourceNames:     map[string]bool{"app": true, "db": true, "cache": true},
			observedResources: map[string]*structpb.Struct{},
			ttlSeconds:        10,
			wantDeferred:      []string{"app"},
			wantAnyDeferred:   true,
			wantEventCount:    1, // single consolidated event
			wantEventMsgs: []string{
				`1 resource(s) deferred: app`,
			},
		},
		{
			name: "NeverDeferObservedResource",
			deps: []DependencyPair{
				{Dependent: "app", Dependency: "db", IsRef: true},
			},
			resourceNames:     map[string]bool{"app": true, "db": true},
			observedResources: map[string]*structpb.Struct{"app": emptyStruct()}, // app observed, db NOT
			ttlSeconds:        10,
			wantDeferred:      nil,
			wantAnyDeferred:   false,
			wantEventCount:    0,
		},
		{
			name: "BothObservedNeitherDeferred",
			deps: []DependencyPair{
				{Dependent: "app", Dependency: "db", IsRef: true},
			},
			resourceNames:     map[string]bool{"app": true, "db": true},
			observedResources: map[string]*structpb.Struct{"app": emptyStruct(), "db": emptyStruct()},
			ttlSeconds:        10,
			wantDeferred:      nil,
			wantAnyDeferred:   false,
			wantEventCount:    0,
		},
		{
			name: "TransitiveChainNothingObserved",
			// A->B->C: B depends on A, C depends on B
			deps: []DependencyPair{
				{Dependent: "B", Dependency: "A", IsRef: true},
				{Dependent: "C", Dependency: "B", IsRef: true},
			},
			resourceNames:     map[string]bool{"A": true, "B": true, "C": true},
			observedResources: map[string]*structpb.Struct{},
			ttlSeconds:        10,
			wantDeferred:      []string{"B", "C"}, // sorted
			wantAnyDeferred:   true,
			wantEventCount:    1, // single consolidated event
			wantEventMsgs: []string{
				`2 resource(s) deferred: B, C`,
			},
		},
		{
			name: "TransitiveChainPartialObserved",
			// A->B->C: A is observed and ready. B's dep (A) is met, so B not deferred.
			// C's dep (B) is not observed, so C is deferred.
			deps: []DependencyPair{
				{Dependent: "B", Dependency: "A", IsRef: true},
				{Dependent: "C", Dependency: "B", IsRef: true},
			},
			resourceNames:     map[string]bool{"A": true, "B": true, "C": true},
			observedResources: map[string]*structpb.Struct{"A": readyStruct()},
			ttlSeconds:        10,
			wantDeferred:      []string{"C"},
			wantAnyDeferred:   true,
			wantEventCount:    1, // single consolidated event
			wantEventMsgs: []string{
				`1 resource(s) deferred: C`,
			},
		},
		{
			name: "SummaryEventWithTTL",
			deps: []DependencyPair{
				{Dependent: "app", Dependency: "db", IsRef: true},
			},
			resourceNames:     map[string]bool{"app": true, "db": true},
			observedResources: map[string]*structpb.Struct{},
			ttlSeconds:        30,
			wantDeferred:      []string{"app"},
			wantAnyDeferred:   true,
			wantEventCount:    1, // single consolidated event
			wantEventMsgs: []string{
				`1 resource(s) deferred: app`,
			},
		},
		{
			name: "DeterministicOrder",
			deps: []DependencyPair{
				{Dependent: "zeta", Dependency: "alpha", IsRef: true},
				{Dependent: "beta", Dependency: "alpha", IsRef: true},
				{Dependent: "gamma", Dependency: "alpha", IsRef: true},
			},
			resourceNames:     map[string]bool{"alpha": true, "beta": true, "gamma": true, "zeta": true},
			observedResources: map[string]*structpb.Struct{},
			ttlSeconds:        10,
			wantDeferred:      []string{"beta", "gamma", "zeta"}, // sorted
			wantAnyDeferred:   true,
			wantEventCount:    1, // single consolidated event
		},
		{
			name: "ANDSemantics",
			// app depends on both db and cache; db is observed+ready, cache is not.
			// Because not ALL deps are met, app is deferred.
			deps: []DependencyPair{
				{Dependent: "app", Dependency: "db", IsRef: true},
				{Dependent: "app", Dependency: "cache", IsRef: false},
			},
			resourceNames:     map[string]bool{"app": true, "db": true, "cache": true},
			observedResources: map[string]*structpb.Struct{"db": readyStruct()},
			ttlSeconds:        10,
			wantDeferred:      []string{"app"},
			wantAnyDeferred:   true,
			wantEventCount:    1, // single consolidated event
			wantEventMsgs: []string{
				`1 resource(s) deferred: app`,
			},
		},
		{
			name: "SeparateFromSkip",
			// Verify result struct uses Deferred (not Skipped) and events use
			// "Creation sequencing:" prefix, not "Skipping resource".
			deps: []DependencyPair{
				{Dependent: "svc", Dependency: "ns", IsRef: true},
			},
			resourceNames:     map[string]bool{"svc": true, "ns": true},
			observedResources: map[string]*structpb.Struct{},
			ttlSeconds:        10,
			wantDeferred:      []string{"svc"},
			wantAnyDeferred:   true,
			wantEventCount:    1, // single consolidated event
			wantEventMsgs: []string{
				`Creation sequencing:`,
			},
		},
		// --- Condition-based readiness tests (plain ref) ---
		{
			name: "PlainRefObservedButNotReady",
			// dep observed but has no conditions → deferred.
			deps: []DependencyPair{
				{Dependent: "app", Dependency: "db", IsRef: true},
			},
			resourceNames:     map[string]bool{"app": true, "db": true},
			observedResources: map[string]*structpb.Struct{"db": emptyStruct()},
			ttlSeconds:        10,
			wantDeferred:      []string{"app"},
			wantAnyDeferred:   true,
			wantEventCount:    1, // single consolidated event
			wantEventMsgs: []string{
				`1 resource(s) deferred: app`,
			},
		},
		{
			name: "PlainRefReadyButNotSynced",
			deps: []DependencyPair{
				{Dependent: "app", Dependency: "db", IsRef: true},
			},
			resourceNames: map[string]bool{"app": true, "db": true},
			observedResources: func() map[string]*structpb.Struct {
				s, _ := structpb.NewStruct(map[string]any{
					"status": map[string]any{
						"conditions": []any{
							map[string]any{"type": "Ready", "status": "True"},
						},
					},
				})
				return map[string]*structpb.Struct{"db": s}
			}(),
			ttlSeconds:      10,
			wantDeferred:    []string{"app"},
			wantAnyDeferred: true,
			wantEventCount:  1, // single consolidated event
			wantEventMsgs: []string{
				`1 resource(s) deferred: app`,
			},
		},
		{
			name: "PlainRefReadyAndSynced",
			deps: []DependencyPair{
				{Dependent: "app", Dependency: "db", IsRef: true},
			},
			resourceNames:     map[string]bool{"app": true, "db": true},
			observedResources: map[string]*structpb.Struct{"db": readyStruct()},
			ttlSeconds:        10,
			wantDeferred:      nil,
			wantAnyDeferred:   false,
			wantEventCount:    0,
		},
		{
			name: "PlainRefAntiFlapping",
			// dep loses readiness, but dependent is already in observed → NOT deferred (SEQ-03).
			deps: []DependencyPair{
				{Dependent: "app", Dependency: "db", IsRef: true},
			},
			resourceNames:     map[string]bool{"app": true, "db": true},
			observedResources: map[string]*structpb.Struct{"app": emptyStruct(), "db": emptyStruct()},
			ttlSeconds:        10,
			wantDeferred:      nil,
			wantAnyDeferred:   false,
			wantEventCount:    0,
		},
		// --- Field path tests ---
		{
			name: "FieldPathMissing",
			deps: []DependencyPair{
				{Dependent: "app", Dependency: "db", IsRef: true, FieldPath: "status.id"},
			},
			resourceNames: map[string]bool{"app": true, "db": true},
			observedResources: map[string]*structpb.Struct{
				"db": emptyStruct(), // db observed but no status.id field
			},
			ttlSeconds:      10,
			wantDeferred:    []string{"app"},
			wantAnyDeferred: true,
			wantEventCount:  1, // single consolidated event
			wantEventMsgs: []string{
				`1 resource(s) deferred: app`,
			},
		},
		{
			name: "FieldPathEmptyString",
			deps: []DependencyPair{
				{Dependent: "app", Dependency: "db", IsRef: true, FieldPath: "status.id"},
			},
			resourceNames: map[string]bool{"app": true, "db": true},
			observedResources: func() map[string]*structpb.Struct {
				s, _ := structpb.NewStruct(map[string]any{
					"status": map[string]any{"id": ""},
				})
				return map[string]*structpb.Struct{"db": s}
			}(),
			ttlSeconds:      10,
			wantDeferred:    []string{"app"},
			wantAnyDeferred: true,
			wantEventCount:  1, // single consolidated event
		},
		{
			name: "FieldPathNonEmptyString",
			deps: []DependencyPair{
				{Dependent: "app", Dependency: "db", IsRef: true, FieldPath: "status.id"},
			},
			resourceNames: map[string]bool{"app": true, "db": true},
			observedResources: func() map[string]*structpb.Struct {
				s, _ := structpb.NewStruct(map[string]any{
					"status": map[string]any{"id": "abc123"},
				})
				return map[string]*structpb.Struct{"db": s}
			}(),
			ttlSeconds:      10,
			wantDeferred:    nil,
			wantAnyDeferred: false,
			wantEventCount:  0,
		},
		{
			name: "FieldPathZeroNumber",
			deps: []DependencyPair{
				{Dependent: "app", Dependency: "db", IsRef: true, FieldPath: "status.count"},
			},
			resourceNames: map[string]bool{"app": true, "db": true},
			observedResources: func() map[string]*structpb.Struct {
				s, _ := structpb.NewStruct(map[string]any{
					"status": map[string]any{"count": 0},
				})
				return map[string]*structpb.Struct{"db": s}
			}(),
			ttlSeconds:      10,
			wantDeferred:    []string{"app"},
			wantAnyDeferred: true,
			wantEventCount:  1, // single consolidated event
		},
		{
			name: "FieldPathNonZeroNumber",
			deps: []DependencyPair{
				{Dependent: "app", Dependency: "db", IsRef: true, FieldPath: "status.count"},
			},
			resourceNames: map[string]bool{"app": true, "db": true},
			observedResources: func() map[string]*structpb.Struct {
				s, _ := structpb.NewStruct(map[string]any{
					"status": map[string]any{"count": 42},
				})
				return map[string]*structpb.Struct{"db": s}
			}(),
			ttlSeconds:      10,
			wantDeferred:    nil,
			wantAnyDeferred: false,
			wantEventCount:  0,
		},
		{
			name: "FieldPathNull",
			deps: []DependencyPair{
				{Dependent: "app", Dependency: "db", IsRef: true, FieldPath: "status.id"},
			},
			resourceNames: map[string]bool{"app": true, "db": true},
			observedResources: func() map[string]*structpb.Struct {
				s, _ := structpb.NewStruct(map[string]any{
					"status": map[string]any{"id": nil},
				})
				return map[string]*structpb.Struct{"db": s}
			}(),
			ttlSeconds:      10,
			wantDeferred:    []string{"app"},
			wantAnyDeferred: true,
			wantEventCount:  1, // single consolidated event
		},
		{
			name: "FieldPathBoolTrue",
			deps: []DependencyPair{
				{Dependent: "app", Dependency: "db", IsRef: true, FieldPath: "status.ready"},
			},
			resourceNames: map[string]bool{"app": true, "db": true},
			observedResources: func() map[string]*structpb.Struct {
				s, _ := structpb.NewStruct(map[string]any{
					"status": map[string]any{"ready": true},
				})
				return map[string]*structpb.Struct{"db": s}
			}(),
			ttlSeconds:      10,
			wantDeferred:    nil,
			wantAnyDeferred: false,
			wantEventCount:  0,
		},
		{
			name: "FieldPathBoolFalse",
			deps: []DependencyPair{
				{Dependent: "app", Dependency: "db", IsRef: true, FieldPath: "status.ready"},
			},
			resourceNames: map[string]bool{"app": true, "db": true},
			observedResources: func() map[string]*structpb.Struct {
				s, _ := structpb.NewStruct(map[string]any{
					"status": map[string]any{"ready": false},
				})
				return map[string]*structpb.Struct{"db": s}
			}(),
			ttlSeconds:      10,
			wantDeferred:    []string{"app"},
			wantAnyDeferred: true,
			wantEventCount:  1, // single consolidated event
		},
		{
			name: "FieldPathNested",
			deps: []DependencyPair{
				{Dependent: "app", Dependency: "db", IsRef: true, FieldPath: "status.atProvider.id"},
			},
			resourceNames: map[string]bool{"app": true, "db": true},
			observedResources: func() map[string]*structpb.Struct {
				s, _ := structpb.NewStruct(map[string]any{
					"status": map[string]any{
						"atProvider": map[string]any{
							"id": "proj-123",
						},
					},
				})
				return map[string]*structpb.Struct{"db": s}
			}(),
			ttlSeconds:      10,
			wantDeferred:    nil,
			wantAnyDeferred: false,
			wantEventCount:  0,
		},
		{
			name: "FieldPathNoFieldPath",
			// No FieldPath: checks Ready=True and Synced=True conditions.
			deps: []DependencyPair{
				{Dependent: "app", Dependency: "db", IsRef: true},
			},
			resourceNames:     map[string]bool{"app": true, "db": true},
			observedResources: map[string]*structpb.Struct{"db": readyStruct()},
			ttlSeconds:        10,
			wantDeferred:      nil,
			wantAnyDeferred:   false,
			wantEventCount:    0,
		},
		{
			name: "FieldPathMixed",
			// Mix of deps with and without FieldPath.
			deps: []DependencyPair{
				{Dependent: "app", Dependency: "db", IsRef: true},                              // conditions check
				{Dependent: "app", Dependency: "project", IsRef: true, FieldPath: "status.id"}, // field path
			},
			resourceNames: map[string]bool{"app": true, "db": true, "project": true},
			observedResources: func() map[string]*structpb.Struct {
				s, _ := structpb.NewStruct(map[string]any{
					"status": map[string]any{"id": "proj-123"},
				})
				return map[string]*structpb.Struct{
					"db":      readyStruct(),
					"project": s,
				}
			}(),
			ttlSeconds:      10,
			wantDeferred:    nil,
			wantAnyDeferred: false,
			wantEventCount:  0,
		},
		{
			name: "FieldPathMixedOneUnmet",
			// Mix: db observed (existence ok), project observed but field not ready.
			deps: []DependencyPair{
				{Dependent: "app", Dependency: "db", IsRef: true},
				{Dependent: "app", Dependency: "project", IsRef: true, FieldPath: "status.id"},
			},
			resourceNames: map[string]bool{"app": true, "db": true, "project": true},
			observedResources: map[string]*structpb.Struct{
				"db":      emptyStruct(),
				"project": emptyStruct(), // no status.id
			},
			ttlSeconds:      10,
			wantDeferred:    []string{"app"},
			wantAnyDeferred: true,
			wantEventCount:  1, // single consolidated event
			wantEventMsgs: []string{
				`1 resource(s) deferred: app`,
			},
		},
		{
			name: "FieldPathEventMessage",
			deps: []DependencyPair{
				{Dependent: "app", Dependency: "db", IsRef: true, FieldPath: "status.atProvider.objectId"},
			},
			resourceNames: map[string]bool{"app": true, "db": true},
			observedResources: map[string]*structpb.Struct{
				"db": emptyStruct(),
			},
			ttlSeconds:      10,
			wantDeferred:    []string{"app"},
			wantAnyDeferred: true,
			wantEventCount:  1, // single consolidated event
			wantEventMsgs: []string{
				`1 resource(s) deferred: app`,
			},
		},
		// --- Kubernetes Object nested condition tests ---
		{
			name: "KubernetesObjectNestedAllTrue",
			// K8s Object with nested Deployment conditions all True -> dependency met.
			deps: []DependencyPair{
				{Dependent: "app", Dependency: "k8s-deploy", IsRef: true},
			},
			resourceNames: map[string]bool{"app": true, "k8s-deploy": true},
			observedResources: map[string]*structpb.Struct{
				"k8s-deploy": k8sObjectStruct("kubernetes.crossplane.io/v1alpha2", true, []map[string]any{
					{"type": "Available", "status": "True"},
					{"type": "Progressing", "status": "True"},
				}),
			},
			ttlSeconds:      10,
			wantDeferred:    nil,
			wantAnyDeferred: false,
			wantEventCount:  0,
		},
		{
			name: "KubernetesObjectNestedOneFalse",
			// K8s Object with nested Deployment Available=False -> deferred.
			deps: []DependencyPair{
				{Dependent: "app", Dependency: "k8s-deploy", IsRef: true},
			},
			resourceNames: map[string]bool{"app": true, "k8s-deploy": true},
			observedResources: map[string]*structpb.Struct{
				"k8s-deploy": k8sObjectStruct("kubernetes.crossplane.io/v1alpha2", true, []map[string]any{
					{"type": "Available", "status": "False"},
					{"type": "Progressing", "status": "True"},
				}),
			},
			ttlSeconds:      10,
			wantDeferred:    []string{"app"},
			wantAnyDeferred: true,
			wantEventCount:  1,
			wantEventMsgs: []string{
				`1 resource(s) deferred: app`,
			},
		},
		{
			name: "KubernetesObjectNoNestedConditionsFallback",
			// K8s Object with no nested conditions (nestedConditions=nil) ->
			// falls back to wrapper's own Ready=True+Synced=True.
			deps: []DependencyPair{
				{Dependent: "app", Dependency: "k8s-svc", IsRef: true},
			},
			resourceNames: map[string]bool{"app": true, "k8s-svc": true},
			observedResources: map[string]*structpb.Struct{
				"k8s-svc": k8sObjectStruct("kubernetes.crossplane.io/v1alpha2", true, nil),
			},
			ttlSeconds:      10,
			wantDeferred:    nil,
			wantAnyDeferred: false,
			wantEventCount:  0,
		},
		{
			name: "KubernetesObjectMixedConditionsDeferred",
			// K8s Object with nested conditions where one is False and one is True -> deferred (AllTrue semantics).
			deps: []DependencyPair{
				{Dependent: "app", Dependency: "k8s-deploy", IsRef: true},
			},
			resourceNames: map[string]bool{"app": true, "k8s-deploy": true},
			observedResources: map[string]*structpb.Struct{
				"k8s-deploy": k8sObjectStruct("kubernetes.crossplane.io/v1alpha2", true, []map[string]any{
					{"type": "Available", "status": "True"},
					{"type": "Progressing", "status": "False"},
				}),
			},
			ttlSeconds:      10,
			wantDeferred:    []string{"app"},
			wantAnyDeferred: true,
			wantEventCount:  1,
		},
		{
			name: "KubernetesObjectEmptyNestedConditionsFallback",
			// K8s Object with empty nested conditions list -> falls back to wrapper conditions.
			deps: []DependencyPair{
				{Dependent: "app", Dependency: "k8s-obj", IsRef: true},
			},
			resourceNames: map[string]bool{"app": true, "k8s-obj": true},
			observedResources: map[string]*structpb.Struct{
				"k8s-obj": k8sObjectStruct("kubernetes.crossplane.io/v1alpha2", true, []map[string]any{}),
			},
			ttlSeconds:      10,
			wantDeferred:    nil,
			wantAnyDeferred: false,
			wantEventCount:  0,
		},
		{
			name: "KubernetesObjectMissingAtProviderFallback",
			// K8s Object with missing status.atProvider.manifest path -> falls back to wrapper conditions.
			deps: []DependencyPair{
				{Dependent: "app", Dependency: "k8s-obj", IsRef: true},
			},
			resourceNames: map[string]bool{"app": true, "k8s-obj": true},
			observedResources: func() map[string]*structpb.Struct {
				// Build a K8s Object without the atProvider path at all.
				s, _ := structpb.NewStruct(map[string]any{
					"apiVersion": "kubernetes.crossplane.io/v1alpha2",
					"kind":       "Object",
					"status": map[string]any{
						"conditions": []any{
							map[string]any{"type": "Ready", "status": "True"},
							map[string]any{"type": "Synced", "status": "True"},
						},
					},
				})
				return map[string]*structpb.Struct{"k8s-obj": s}
			}(),
			ttlSeconds:      10,
			wantDeferred:    nil,
			wantAnyDeferred: false,
			wantEventCount:  0,
		},
		{
			name: "NonObjectResourceUnchanged",
			// Non-Object resource (regular MR) still uses Ready=True+Synced=True (no regression).
			deps: []DependencyPair{
				{Dependent: "app", Dependency: "rds", IsRef: true},
			},
			resourceNames: map[string]bool{"app": true, "rds": true},
			observedResources: func() map[string]*structpb.Struct {
				s, _ := structpb.NewStruct(map[string]any{
					"apiVersion": "rds.aws.upbound.io/v1beta1",
					"kind":       "Instance",
					"status": map[string]any{
						"conditions": []any{
							map[string]any{"type": "Ready", "status": "True"},
							map[string]any{"type": "Synced", "status": "True"},
						},
					},
				})
				return map[string]*structpb.Struct{"rds": s}
			}(),
			ttlSeconds:      10,
			wantDeferred:    nil,
			wantAnyDeferred: false,
			wantEventCount:  0,
		},
		{
			name: "KubernetesObjectV1alpha1Detected",
			// K8s Object with apiVersion=kubernetes.crossplane.io/v1alpha1 (different version) still detected.
			deps: []DependencyPair{
				{Dependent: "app", Dependency: "k8s-deploy", IsRef: true},
			},
			resourceNames: map[string]bool{"app": true, "k8s-deploy": true},
			observedResources: map[string]*structpb.Struct{
				"k8s-deploy": k8sObjectStruct("kubernetes.crossplane.io/v1alpha1", true, []map[string]any{
					{"type": "Available", "status": "True"},
				}),
			},
			ttlSeconds:      10,
			wantDeferred:    nil,
			wantAnyDeferred: false,
			wantEventCount:  0,
		},
		{
			name: "KubernetesObjectFieldPathPrecedence",
			// K8s Object with FieldPath dependency -> FieldPath takes precedence (no change in behavior).
			deps: []DependencyPair{
				{Dependent: "app", Dependency: "k8s-deploy", IsRef: true, FieldPath: "status.atProvider.manifest.status.conditions"},
			},
			resourceNames: map[string]bool{"app": true, "k8s-deploy": true},
			observedResources: map[string]*structpb.Struct{
				"k8s-deploy": k8sObjectStruct("kubernetes.crossplane.io/v1alpha2", true, []map[string]any{
					{"type": "Available", "status": "True"},
				}),
			},
			ttlSeconds:      10,
			wantDeferred:    nil,
			wantAnyDeferred: false,
			wantEventCount:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			seq := NewSequencer(tt.deps, tt.resourceNames, tt.observedResources, tt.ttlSeconds)
			result := seq.Evaluate()

			// Check AnyDeferred.
			if result.AnyDeferred != tt.wantAnyDeferred {
				t.Errorf("AnyDeferred = %v, want %v", result.AnyDeferred, tt.wantAnyDeferred)
			}

			// Check Deferred list.
			switch {
			case tt.wantDeferred == nil && result.Deferred != nil:
				// Allow empty slice to match nil expectation.
				if len(result.Deferred) != 0 {
					t.Errorf("Deferred = %v, want nil", result.Deferred)
				}
			case len(result.Deferred) != len(tt.wantDeferred):
				t.Errorf("Deferred = %v, want %v", result.Deferred, tt.wantDeferred)
			default:
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
	c := NewCollector(cc, "test.star", nil, nil)

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
	c := NewCollector(cc, "test.star", nil, nil)

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
		name              string
		deps              []DependencyPair
		resourceNames     map[string]bool
		observedResources map[string]*structpb.Struct
		ttlSeconds        int
		wantDeferred      []string
		wantAnyDeferred   bool
		wantEventCount    int
		wantEventMsgs     []string
	}{
		{
			name:              "EmptyDepsSlice",
			deps:              []DependencyPair{},
			resourceNames:     map[string]bool{"app": true},
			observedResources: map[string]*structpb.Struct{},
			ttlSeconds:        10,
			wantDeferred:      nil,
			wantAnyDeferred:   false,
			wantEventCount:    0,
		},
		{
			name: "SelfDependency",
			deps: []DependencyPair{
				{Dependent: "app", Dependency: "app", IsRef: true},
			},
			resourceNames:     map[string]bool{"app": true},
			observedResources: map[string]*structpb.Struct{},
			ttlSeconds:        10,
			wantDeferred:      []string{"app"},
			wantAnyDeferred:   true,
			wantEventCount:    1, // single consolidated event
			wantEventMsgs: []string{
				`1 resource(s) deferred: app`,
			},
		},
		{
			name: "DependencyNotInResourceNames",
			deps: []DependencyPair{
				{Dependent: "app", Dependency: "external-db", IsRef: false},
			},
			resourceNames:     map[string]bool{"app": true},
			observedResources: map[string]*structpb.Struct{},
			ttlSeconds:        10,
			wantDeferred:      []string{"app"},
			wantAnyDeferred:   true,
			wantEventCount:    1, // single consolidated event
			wantEventMsgs: []string{
				`1 resource(s) deferred: app`,
			},
		},
		{
			name: "ZeroTTL",
			deps: []DependencyPair{
				{Dependent: "app", Dependency: "db", IsRef: true},
			},
			resourceNames:     map[string]bool{"app": true, "db": true},
			observedResources: map[string]*structpb.Struct{},
			ttlSeconds:        0,
			wantDeferred:      []string{"app"},
			wantAnyDeferred:   true,
			wantEventCount:    1, // single consolidated event
			wantEventMsgs: []string{
				`1 resource(s) deferred: app`,
			},
		},
		{
			name: "DependentNotInResourceNames",
			deps: []DependencyPair{
				{Dependent: "phantom", Dependency: "db", IsRef: true},
			},
			resourceNames:     map[string]bool{"db": true},
			observedResources: map[string]*structpb.Struct{},
			ttlSeconds:        10,
			wantDeferred:      []string{"phantom"},
			wantAnyDeferred:   true,
			wantEventCount:    1, // single consolidated event
			wantEventMsgs: []string{
				`1 resource(s) deferred: phantom`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			seq := NewSequencer(tt.deps, tt.resourceNames, tt.observedResources, tt.ttlSeconds)
			result := seq.Evaluate()

			if result.AnyDeferred != tt.wantAnyDeferred {
				t.Errorf("AnyDeferred = %v, want %v", result.AnyDeferred, tt.wantAnyDeferred)
			}

			switch {
			case tt.wantDeferred == nil && result.Deferred != nil:
				if len(result.Deferred) != 0 {
					t.Errorf("Deferred = %v, want nil", result.Deferred)
				}
			case len(result.Deferred) != len(tt.wantDeferred):
				t.Errorf("Deferred = %v, want %v", result.Deferred, tt.wantDeferred)
			default:
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
