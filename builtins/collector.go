// Package builtins provides the predeclared Starlark globals and resource
// collection for Crossplane composition functions.
package builtins

import (
	"fmt"
	"sync"

	"github.com/crossplane/function-sdk-go/resource"
	"go.starlark.net/starlark"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/wompipomp/function-starlark/convert"
	"github.com/wompipomp/function-starlark/metrics"
	"github.com/wompipomp/function-starlark/schema"
)

const externalNameAnnotation = "crossplane.io/external-name"

// Crossplane traceability label keys auto-injected by Resource().
const (
	labelComposite      = "crossplane.io/composite"
	labelClaimName      = "crossplane.io/claim-name"
	labelClaimNamespace = "crossplane.io/claim-namespace"
)

// ResourceRef is a custom Starlark type returned by Resource().
// It carries the resource name and can be passed to depends_on.
type ResourceRef struct {
	name string
}

// Compile-time interface checks.
var (
	_ starlark.Value    = (*ResourceRef)(nil)
	_ starlark.HasAttrs = (*ResourceRef)(nil)
)

func (r *ResourceRef) String() string       { return r.name }
func (r *ResourceRef) Type() string         { return "ResourceRef" }
func (r *ResourceRef) Freeze()              {} // immutable by construction
func (r *ResourceRef) Truth() starlark.Bool { return starlark.True }
func (r *ResourceRef) Hash() (uint32, error) {
	h := uint32(2166136261) // FNV-1a offset basis
	for i := 0; i < len(r.name); i++ {
		h ^= uint32(r.name[i])
		h *= 16777619
	}
	return h, nil
}

func (r *ResourceRef) Attr(name string) (starlark.Value, error) {
	if name == "name" {
		return starlark.String(r.name), nil
	}
	return nil, nil
}

func (r *ResourceRef) AttrNames() []string { return []string{"name"} }

// SkippedRef is the value returned by Resource() when the resource was skipped
// (when=False, body=None without preserve, preserve_observed miss, or
// transitively skipped via depends_on). It exposes .name and .optional like
// ResourceRef but its truth value is False so `if ref:` continues to gate
// callers correctly. Passing a SkippedRef to depends_on triggers transitive
// skip when optional=False, or is silently ignored when optional=True.
type SkippedRef struct {
	name     string
	optional bool
}

var (
	_ starlark.Value    = (*SkippedRef)(nil)
	_ starlark.HasAttrs = (*SkippedRef)(nil)
)

func (r *SkippedRef) String() string       { return r.name }
func (r *SkippedRef) Type() string         { return "SkippedRef" }
func (r *SkippedRef) Freeze()              {}
func (r *SkippedRef) Truth() starlark.Bool { return starlark.False }
func (r *SkippedRef) Hash() (uint32, error) {
	h := uint32(2166136261)
	for i := 0; i < len(r.name); i++ {
		h ^= uint32(r.name[i])
		h *= 16777619
	}
	return h, nil
}

func (r *SkippedRef) Attr(name string) (starlark.Value, error) {
	switch name {
	case "name":
		return starlark.String(r.name), nil
	case "optional":
		return starlark.Bool(r.optional), nil
	}
	return nil, nil
}

func (r *SkippedRef) AttrNames() []string { return []string{"name", "optional"} }

// DependencyPair records a dependency between two resources.
type DependencyPair struct {
	Dependent  string // resource that depends on another
	Dependency string // resource being depended upon
	IsRef      bool   // true if dependency came from ResourceRef (validate), false if string (trust)
	FieldPath  string // optional dot-separated field path for readiness check (empty = existence only)
}

// CollectedResource holds a single resource produced by the Resource() builtin.
type CollectedResource struct {
	Name              string
	Body              *structpb.Struct
	Ready             resource.Ready
	ConnectionDetails map[string][]byte
}

// GatingSkip records a skip that should block the composite resource from
// being marked Ready. Resources skipped with optional=True do NOT appear here.
type GatingSkip struct {
	Name   string
	Reason string
}

// GatingDefer records a resource deferred by the creation sequencer (its
// dependencies are not yet ready). Reason is a human description of the unmet
// dependencies (e.g., `waiting for "db" to have Ready=True`).
type GatingDefer struct {
	Name   string
	Reason string
}

// CompositeReadyOverride captures an explicit set_composite_ready() call.
// When Set is false, the override is absent and auto-gating (via GatingSkip)
// decides the composite ready state.
type CompositeReadyOverride struct {
	Set     bool
	Ready   bool
	Reason  string
	Message string
}

// Collector accumulates Resource() calls from Starlark scripts.
// Duplicate names use last-wins semantics.
type Collector struct {
	mu             sync.Mutex
	resources      map[string]CollectedResource
	skipped        map[string]bool
	gatingSkips    []GatingSkip
	gatingDefers   []GatingDefer
	compositeReady CompositeReadyOverride
	dependencies   []DependencyPair
	cc             *ConditionCollector
	scriptName     string
	oxr            *structpb.Struct      // frozen observed XR, for label extraction
	observed       *convert.StarlarkDict // frozen observed composed resources; may be nil
}

// NewCollector creates an empty Collector. The ConditionCollector is used to
// emit Warning events when the external_name or labels kwarg conflicts with
// existing values. The scriptName is recorded for use in metric labels. The
// oxr is the observed composite resource struct used for auto-injecting
// crossplane traceability labels. The observed dict holds the frozen Starlark
// representation of observed composed resources; it may be nil.
func NewCollector(cc *ConditionCollector, scriptName string, oxr *structpb.Struct, observed *convert.StarlarkDict) *Collector {
	return &Collector{
		resources:  make(map[string]CollectedResource),
		skipped:    make(map[string]bool),
		cc:         cc,
		scriptName: scriptName,
		oxr:        oxr,
		observed:   observed,
	}
}

// Builtin returns a *starlark.Builtin named "Resource" that scripts call
// to produce desired composed resources.
func (c *Collector) Builtin() *starlark.Builtin {
	return starlark.NewBuiltin("Resource", c.resourceFn)
}

// SkipResourceBuiltin returns a *starlark.Builtin named "skip_resource" that
// scripts call to intentionally omit a resource, emitting a Warning event for
// observability.
func (c *Collector) SkipResourceBuiltin() *starlark.Builtin {
	return starlark.NewBuiltin("skip_resource", c.skipResourceFn)
}

// SetCompositeReadyBuiltin returns the "set_composite_ready" builtin.
func (c *Collector) SetCompositeReadyBuiltin() *starlark.Builtin {
	return starlark.NewBuiltin("set_composite_ready", c.setCompositeReadyFn)
}

// lookupObservedBody looks up a resource by name in the observed composed
// resources dict and converts it from *convert.StarlarkDict to *structpb.Struct.
// Returns (nil, false, nil) when the resource is not found or c.observed is nil.
// Server-side-apply fields (metadata.managedFields, metadata.resourceVersion)
// and status are stripped so the result can be re-emitted as desired state
// without tripping apiserver validation.
func (c *Collector) lookupObservedBody(name string) (*structpb.Struct, bool, error) {
	if c.observed == nil {
		return nil, false, nil
	}
	val, found, err := c.observed.Get(starlark.String(name))
	if err != nil {
		return nil, false, fmt.Errorf("observed lookup %q: %w", name, err)
	}
	if !found || val == starlark.None {
		return nil, false, nil
	}
	obsDict, ok := val.(*convert.StarlarkDict)
	if !ok {
		return nil, false, fmt.Errorf("observed %q: expected *convert.StarlarkDict, got %T", name, val)
	}
	s, err := convert.StarlarkToStruct(obsDict)
	if err != nil {
		return nil, false, fmt.Errorf("observed %q: %w", name, err)
	}
	stripObservedReadOnlyFields(s)
	return s, true, nil
}

// stripObservedReadOnlyFields removes fields from an observed body that would
// be rejected when re-emitted as desired state via server-side apply.
func stripObservedReadOnlyFields(s *structpb.Struct) {
	if s == nil {
		return
	}
	if md := s.GetFields()["metadata"]; md != nil {
		if mdStruct := md.GetStructValue(); mdStruct != nil {
			delete(mdStruct.Fields, "managedFields")
			delete(mdStruct.Fields, "resourceVersion")
			delete(mdStruct.Fields, "uid")
			delete(mdStruct.Fields, "generation")
			delete(mdStruct.Fields, "creationTimestamp")
		}
	}
	delete(s.Fields, "status")
}

// recordPreserve stores the observed body directly and emits a Warning event.
// The caller must NOT hold c.mu (lock ordering: c.mu before cc.mu).
func (c *Collector) recordPreserve(name string, body *structpb.Struct, message string) {
	c.mu.Lock()
	c.resources[name] = CollectedResource{
		Name:  name,
		Body:  body,
		Ready: resource.ReadyUnspecified,
	}
	c.mu.Unlock()

	c.cc.AddEvent(CollectedEvent{
		Severity: "Warning",
		Message:  fmt.Sprintf("Preserving resource %q: %s", name, message),
		Target:   "Composite",
	})
}

// recordSkip handles skip registration, Warning event emission, and metric
// increment. When gate is true and the skip is new, the resource is also
// recorded as gating composite resource readiness (XR Ready=False).
// It is a no-op if name was already skipped. The caller must NOT hold c.mu
// when calling recordSkip (lock ordering: c.mu before cc.mu).
func (c *Collector) recordSkip(name, reason string, gate bool) {
	c.mu.Lock()
	if c.skipped[name] {
		c.mu.Unlock()
		return
	}
	c.skipped[name] = true
	if gate {
		c.gatingSkips = append(c.gatingSkips, GatingSkip{Name: name, Reason: reason})
	}
	c.mu.Unlock()

	metrics.ResourcesSkippedTotal.WithLabelValues(c.scriptName).Inc()

	c.cc.AddEvent(CollectedEvent{
		Severity: "Warning",
		Message:  fmt.Sprintf("Skipping resource %q: %s", name, reason),
		Target:   "Composite",
	})
}

// setCompositeReadyFn implements set_composite_ready(ready, reason="", message="").
// It records an explicit override of the composite resource's Ready state.
// When called more than once, the last call wins.
func (c *Collector) setCompositeReadyFn(
	_ *starlark.Thread,
	b *starlark.Builtin,
	args starlark.Tuple,
	kwargs []starlark.Tuple,
) (starlark.Value, error) {
	var ready bool
	var reason, message string

	if err := starlark.UnpackArgs(b.Name(), args, kwargs,
		"ready", &ready, "reason?", &reason, "message?", &message); err != nil {
		return nil, err
	}

	c.mu.Lock()
	c.compositeReady = CompositeReadyOverride{
		Set:     true,
		Ready:   ready,
		Reason:  reason,
		Message: message,
	}
	c.mu.Unlock()

	return starlark.None, nil
}

// GatingSkips returns a copy of skipped resources that gate composite readiness.
func (c *Collector) GatingSkips() []GatingSkip {
	c.mu.Lock()
	defer c.mu.Unlock()
	return copyGatingSkips(c.gatingSkips)
}

// GatingDefers returns a copy of sequencer-deferred resources that gate
// composite readiness.
func (c *Collector) GatingDefers() []GatingDefer {
	c.mu.Lock()
	defer c.mu.Unlock()
	return copyGatingDefers(c.gatingDefers)
}

// AddGatingDefers appends sequencer-deferred resources as composite-ready
// gating items. Called by fn.go after Sequencer.Evaluate returns.
func (c *Collector) AddGatingDefers(items []GatingDefer) {
	if len(items) == 0 {
		return
	}
	c.mu.Lock()
	c.gatingDefers = append(c.gatingDefers, items...)
	c.mu.Unlock()
}

// CompositeReadyOverride returns the explicit composite-ready override, if any.
// The returned value's Set field is true iff set_composite_ready() was called.
func (c *Collector) CompositeReadyOverride() CompositeReadyOverride {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.compositeReady
}

// compositeReadyState reads the override, gating skips, and gating defers in a
// single lock acquisition for the ApplyCompositeReady hot path.
func (c *Collector) compositeReadyState() (CompositeReadyOverride, []GatingSkip, []GatingDefer) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.compositeReady, copyGatingSkips(c.gatingSkips), copyGatingDefers(c.gatingDefers)
}

func copyGatingSkips(src []GatingSkip) []GatingSkip {
	if len(src) == 0 {
		return nil
	}
	out := make([]GatingSkip, len(src))
	copy(out, src)
	return out
}

func copyGatingDefers(src []GatingDefer) []GatingDefer {
	if len(src) == 0 {
		return nil
	}
	out := make([]GatingDefer, len(src))
	copy(out, src)
	return out
}

// skipResourceFn implements skip_resource(name, reason). Records the skip and
// emits a Warning event (deduped per name); errors if the resource was already
// emitted via Resource(). Does not gate composite readiness.
func (c *Collector) skipResourceFn(
	_ *starlark.Thread,
	b *starlark.Builtin,
	args starlark.Tuple,
	kwargs []starlark.Tuple,
) (starlark.Value, error) {
	var name, reason string
	if err := starlark.UnpackArgs(b.Name(), args, kwargs,
		"name", &name, "reason", &reason); err != nil {
		return nil, err
	}

	// Conflict check: error if already emitted via Resource().
	c.mu.Lock()
	if _, exists := c.resources[name]; exists {
		c.mu.Unlock()
		return nil, fmt.Errorf("resource %q already emitted, cannot skip", name)
	}
	c.mu.Unlock()

	// Delegate to shared skip path (handles dedup, metric, Warning event).
	c.recordSkip(name, reason, false)
	return starlark.None, nil
}

// Resources returns a copy of all collected resources.
func (c *Collector) Resources() map[string]CollectedResource {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[string]CollectedResource, len(c.resources))
	for k, v := range c.resources {
		out[k] = v
	}
	return out
}

// Dependencies returns a copy of all tracked dependency pairs.
func (c *Collector) Dependencies() []DependencyPair {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]DependencyPair, len(c.dependencies))
	copy(out, c.dependencies)
	return out
}

// RemoveResources removes the named resources from the collector.
// Used by creation sequencing to withhold deferred resources from desired state.
func (c *Collector) RemoveResources(names []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, name := range names {
		delete(c.resources, name)
	}
}

// addDependency records a dependency between two resources.
func (c *Collector) addDependency(dependent, dependency string, isRef bool, fieldPath string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.dependencies = append(c.dependencies, DependencyPair{
		Dependent:  dependent,
		Dependency: dependency,
		IsRef:      isRef,
		FieldPath:  fieldPath,
	})
}

// validateBoolKwarg validates that a starlark.Value is either nil (omitted) or
// a starlark.Bool. Returns (isFalse, wasProvided, error). When val is nil
// (kwarg omitted), returns (false, false, nil).
func validateBoolKwarg(val starlark.Value, paramName, resourceName string) (bool, bool, error) {
	if val == nil {
		return false, false, nil
	}
	b, ok := val.(starlark.Bool)
	if !ok {
		return false, true, fmt.Errorf("Resource(%q): %s must be bool, got %s",
			resourceName, paramName, val.Type())
	}
	return !bool(b), true, nil
}

// resourceFn implements the Resource(name, body, ready=None, labels=None, connection_details=None, depends_on=None, external_name=None, when=None, skip_reason="", preserve_observed=None, optional=False) Starlark builtin.
func (c *Collector) resourceFn(
	_ *starlark.Thread,
	b *starlark.Builtin,
	args starlark.Tuple,
	kwargs []starlark.Tuple,
) (starlark.Value, error) {
	var name string
	var bodyVal starlark.Value
	var connDetails *starlark.Dict
	var dependsOn *starlark.List
	var readyVal starlark.Value = starlark.None
	var labelsVal starlark.Value
	var externalNameVal starlark.Value
	var whenVal starlark.Value
	var skipReason string
	var preserveVal starlark.Value
	var optional bool

	if err := starlark.UnpackArgs(b.Name(), args, kwargs,
		"name", &name, "body", &bodyVal, "ready?", &readyVal,
		"labels?", &labelsVal,
		"connection_details??", &connDetails,
		"depends_on??", &dependsOn,
		"external_name??", &externalNameVal,
		"when?", &whenVal,
		"skip_reason?", &skipReason,
		"preserve_observed?", &preserveVal,
		"optional?", &optional); err != nil {
		return nil, err
	}

	// --- Gate logic: evaluate when, preserve_observed, skip_reason BEFORE body type-switch ---

	whenFalse, _, err := validateBoolKwarg(whenVal, "when", name)
	if err != nil {
		return nil, err
	}

	_, preserveProvided, err := validateBoolKwarg(preserveVal, "preserve_observed", name)
	if err != nil {
		return nil, err
	}
	preserveActive := preserveProvided && preserveVal == starlark.True

	gate := !optional

	// Validate skip_reason is provided when gating off.
	// skip_reason is always legal (stable across reconciliations where `when`
	// may flip between True and False); it is only consulted on skip paths.
	if whenFalse && !preserveActive && skipReason == "" {
		return nil, fmt.Errorf("Resource(%q): skip_reason is required when when=False", name)
	}

	// GATE-01: when=False without preserve -> skip.
	if whenFalse && !preserveActive {
		c.recordSkip(name, skipReason, gate)
		return &SkippedRef{name: name, optional: optional}, nil
	}

	// when=False + preserve_observed=True: cliff guard — emit observed body if found.
	if whenFalse && preserveActive {
		s, found, err := c.lookupObservedBody(name)
		if err != nil {
			return nil, fmt.Errorf("Resource(%q): %w", name, err)
		}
		if found {
			c.recordPreserve(name, s, "observed body emitted, gated by when=False")
			return &ResourceRef{name: name}, nil
		}
		// Not found in observed — skip.
		reason := "gated by when=False, not found in observed state (preserve_observed=True)"
		if skipReason != "" {
			reason = skipReason
		}
		c.recordSkip(name, reason, gate)
		return &SkippedRef{name: name, optional: optional}, nil
	}

	// GATE-05: body=None without preserve -> warn and skip.
	if bodyVal == starlark.None && !preserveActive {
		c.recordSkip(name, "body is None. If this resource exists, it will be removed from desired state. Set preserve_observed=True to re-emit the observed body when body is None.", gate)
		return &SkippedRef{name: name, optional: optional}, nil
	}

	// body=None + preserve_observed=True: emit observed body if found, skip otherwise.
	if bodyVal == starlark.None && preserveActive {
		s, found, err := c.lookupObservedBody(name)
		if err != nil {
			return nil, fmt.Errorf("Resource(%q): %w", name, err)
		}
		if found {
			c.recordPreserve(name, s, "body=None, emitting observed body")
			return &ResourceRef{name: name}, nil
		}
		c.recordSkip(name, "not found in observed state", gate)
		return &SkippedRef{name: name, optional: optional}, nil
	}

	// Pre-scan depends_on for transitive-skip triggers BEFORE body conversion
	// so we don't waste work when an upstream skip will short-circuit us.
	// A non-optional SkippedRef anywhere in depends_on (including as the first
	// element of a tuple) propagates the skip to this resource.
	if dependsOn != nil {
		for i := 0; i < dependsOn.Len(); i++ {
			var sr *SkippedRef
			switch v := dependsOn.Index(i).(type) {
			case *SkippedRef:
				sr = v
			case starlark.Tuple:
				if v.Len() == 2 {
					if first, ok := v.Index(0).(*SkippedRef); ok {
						sr = first
					}
				}
			}
			if sr != nil && !sr.optional {
				c.recordSkip(name, fmt.Sprintf("depends on skipped %q", sr.name), gate)
				return &SkippedRef{name: name, optional: optional}, nil
			}
		}
	}

	// --- Normal body processing path (when=True or when omitted, body is dict) ---

	var body *starlark.Dict
	switch v := bodyVal.(type) {
	case *schema.SchemaDict:
		body = v.InternalDict()
	case *starlark.Dict:
		body = v
	default:
		return nil, fmt.Errorf("Resource(%q): body must be dict, got %s", name, bodyVal.Type())
	}

	// Auto-compact: strip None-valued entries from body recursively.
	// This lets users write optional fields as `"key": value if cond else None`
	// without needing to manually wrap with dict.compact().
	compacted, _, err := compactValue("Resource", body, 0)
	if err != nil {
		return nil, fmt.Errorf("Resource(%q): compact: %w", name, err)
	}
	body = compacted.(*starlark.Dict)

	s, err := convert.PlainDictToStruct(body)
	if err != nil {
		return nil, fmt.Errorf("Resource(%q): %w", name, err)
	}

	// Process external_name kwarg if provided.
	if externalNameVal != nil {
		en, ok := externalNameVal.(starlark.String)
		if !ok {
			return nil, fmt.Errorf("Resource(%q): external_name must be string, got %s", name, externalNameVal.Type())
		}
		if string(en) == "" {
			return nil, fmt.Errorf("Resource(%q): external_name must not be empty", name)
		}
		injectExternalName(s, string(en), name, c.cc)
	}

	// Process labels kwarg: auto-inject crossplane traceability labels.
	if err := injectLabels(s, labelsVal, c.oxr, name, c.cc); err != nil {
		return nil, err
	}

	// Inject resource-name label for Usage selector matching.
	metadata := getOrCreateNestedStruct(s, "metadata")
	labels := getOrCreateNestedStruct(metadata, "labels")
	labels.Fields[ResourceNameLabel] = structpb.NewStringValue(name)

	// Convert connection_details dict to map[string][]byte if provided.
	var cd map[string][]byte
	if connDetails != nil {
		cd = make(map[string][]byte, connDetails.Len())
		for _, item := range connDetails.Items() {
			k, ok := item[0].(starlark.String)
			if !ok {
				return nil, fmt.Errorf("Resource(%q): connection_details key must be string, got %s", name, item[0].Type())
			}
			v, ok := item[1].(starlark.String)
			if !ok {
				return nil, fmt.Errorf("Resource(%q): connection_details value must be string, got %s", name, item[1].Type())
			}
			cd[string(k)] = []byte(string(v))
		}
	}

	// Process depends_on list if provided. By this point the pre-scan has
	// already applied transitive skip for any non-optional *SkippedRef items,
	// so any *SkippedRef that survives here is optional and is silently
	// dropped. starlark.None is also tolerated for back-compat with patterns
	// like `depends_on=[ref if ref else None]`.
	if dependsOn != nil {
		for i := 0; i < dependsOn.Len(); i++ {
			item := dependsOn.Index(i)
			if item == starlark.None {
				continue
			}
			switch v := item.(type) {
			case *ResourceRef:
				c.addDependency(name, v.name, true, "")
			case *SkippedRef:
				// Optional skip (non-optional was caught in the pre-scan).
				continue
			case starlark.String:
				c.addDependency(name, string(v), false, "")
			case starlark.Tuple:
				if v.Len() != 2 {
					return nil, fmt.Errorf("Resource(%q): depends_on[%d] tuple must have exactly 2 elements, got %d",
						name, i, v.Len())
				}
				// Extract first element: must be ResourceRef, SkippedRef, or string.
				var depName string
				var isRef bool
				dropTuple := false
				switch first := v.Index(0).(type) {
				case *ResourceRef:
					depName = first.name
					isRef = true
				case *SkippedRef:
					// Optional skip in tuple form: drop the whole entry.
					dropTuple = true
				case starlark.String:
					depName = string(first)
					isRef = false
				default:
					return nil, fmt.Errorf("Resource(%q): depends_on[%d] tuple first element must be ResourceRef, SkippedRef, or string, got %s",
						name, i, v.Index(0).Type())
				}
				if dropTuple {
					continue
				}
				// Extract second element: must be non-empty string.
				fieldPathVal, ok := v.Index(1).(starlark.String)
				if !ok {
					return nil, fmt.Errorf("Resource(%q): depends_on[%d] tuple second element must be string, got %s",
						name, i, v.Index(1).Type())
				}
				fieldPath := string(fieldPathVal)
				if fieldPath == "" {
					return nil, fmt.Errorf("Resource(%q): depends_on[%d] tuple field path must not be empty",
						name, i)
				}
				c.addDependency(name, depName, isRef, fieldPath)
			default:
				return nil, fmt.Errorf("Resource(%q): depends_on[%d] must be ResourceRef, SkippedRef, string, tuple, or None, got %s",
					name, i, item.Type())
			}
		}
	}

	ready, err := readyFromStarlark(readyVal)
	if err != nil {
		return nil, fmt.Errorf("Resource(%q): %w", name, err)
	}

	c.mu.Lock()
	c.resources[name] = CollectedResource{
		Name:              name,
		Body:              s,
		Ready:             ready,
		ConnectionDetails: cd,
	}
	c.mu.Unlock()

	return &ResourceRef{name: name}, nil
}

// readyFromStarlark converts a Starlark value to the resource.Ready type.
// None -> ReadyUnspecified (let function-auto-ready detect readiness)
// True -> ReadyTrue (explicitly ready, e.g. ProviderConfig)
// False -> ReadyFalse (explicitly not ready)
// Any other type returns an error naming the invalid type.
func readyFromStarlark(v starlark.Value) (resource.Ready, error) {
	switch v {
	case starlark.None:
		return resource.ReadyUnspecified, nil
	case starlark.True:
		return resource.ReadyTrue, nil
	case starlark.False:
		return resource.ReadyFalse, nil
	default:
		return "", fmt.Errorf("ready must be True, False, or None, got %s", v.Type())
	}
}

// getOrCreateNestedStruct returns the child struct at the given key, creating
// it if it does not exist or if the existing value is not a struct.
func getOrCreateNestedStruct(parent *structpb.Struct, key string) *structpb.Struct {
	if v, ok := parent.Fields[key]; ok {
		if sv := v.GetStructValue(); sv != nil {
			return sv
		}
	}
	child := &structpb.Struct{Fields: make(map[string]*structpb.Value)}
	parent.Fields[key] = structpb.NewStructValue(child)
	return child
}

// injectExternalName sets the crossplane.io/external-name annotation on the
// resource body struct. If the annotation already exists, the kwarg value wins
// and a Warning event is emitted via the ConditionCollector.
func injectExternalName(s *structpb.Struct, externalName, resourceName string, cc *ConditionCollector) {
	metadata := getOrCreateNestedStruct(s, "metadata")
	annotations := getOrCreateNestedStruct(metadata, "annotations")

	if existing, ok := annotations.Fields[externalNameAnnotation]; ok {
		oldVal := existing.GetStringValue()
		msg := fmt.Sprintf("Resource %q: external_name kwarg %q overrides annotation %q",
			resourceName, externalName, oldVal)
		cc.mu.Lock()
		cc.events = append(cc.events, CollectedEvent{
			Severity: "Warning", Message: msg, Target: "Composite",
		})
		cc.mu.Unlock()
	}
	annotations.Fields[externalNameAnnotation] = structpb.NewStringValue(externalName)
}

// crossplaneLabelsFromOXR extracts crossplane traceability labels from the
// observed composite resource. Returns only non-empty values, handling nil
// at every level.
func crossplaneLabelsFromOXR(oxr *structpb.Struct) map[string]string {
	labels := make(map[string]string)
	if oxr == nil {
		return labels
	}
	md := oxr.GetFields()["metadata"]
	if md == nil {
		return labels
	}
	mdStruct := md.GetStructValue()
	if mdStruct == nil {
		return labels
	}
	// composite name from metadata.name (always present)
	if nameVal := mdStruct.GetFields()["name"]; nameVal != nil {
		if name := nameVal.GetStringValue(); name != "" {
			labels[labelComposite] = name
		}
	}
	// claim labels from metadata.labels (only when claim exists)
	lblVal := mdStruct.GetFields()["labels"]
	if lblVal == nil {
		return labels
	}
	lblStruct := lblVal.GetStructValue()
	if lblStruct == nil {
		return labels
	}
	if cn := lblStruct.GetFields()[labelClaimName]; cn != nil {
		if v := cn.GetStringValue(); v != "" {
			labels[labelClaimName] = v
		}
	}
	if cns := lblStruct.GetFields()[labelClaimNamespace]; cns != nil {
		if v := cns.GetStringValue(); v != "" {
			labels[labelClaimNamespace] = v
		}
	}
	return labels
}

// injectLabels performs three-way label merging on the resource body struct.
// Priority: body metadata.labels (lowest) < auto-injected crossplane labels < labels= kwarg (highest).
// If labelsVal is starlark.None, all injection is skipped (opt-out).
// If labelsVal is nil (omitted) or an empty dict, only auto-injection runs.
// Warning events are emitted for body-vs-auto and kwarg-vs-auto conflicts.
func injectLabels(s *structpb.Struct, labelsVal starlark.Value, oxr *structpb.Struct, resourceName string, cc *ConditionCollector) error {
	// Explicit opt-out: labels=None
	if labelsVal == starlark.None {
		return nil
	}

	// Extract crossplane labels from OXR
	xpLabels := crossplaneLabelsFromOXR(oxr)

	// If no crossplane labels and no user labels, nothing to do.
	if len(xpLabels) == 0 && labelsVal == nil {
		return nil
	}

	metadata := getOrCreateNestedStruct(s, "metadata")
	bodyLabels := getOrCreateNestedStruct(metadata, "labels")

	// Step 1: Set crossplane labels, warn if body already has them.
	for k, v := range xpLabels {
		if existing, ok := bodyLabels.Fields[k]; ok {
			oldVal := existing.GetStringValue()
			msg := fmt.Sprintf("Resource %q: body label %q=%q overridden by auto-injected %q",
				resourceName, k, oldVal, v)
			cc.mu.Lock()
			cc.events = append(cc.events, CollectedEvent{
				Severity: "Warning", Message: msg, Target: "Composite",
			})
			cc.mu.Unlock()
		}
		bodyLabels.Fields[k] = structpb.NewStringValue(v)
	}

	// Step 2: If user provided labels dict, overlay on top (user wins).
	if labelsVal != nil {
		var items []starlark.Tuple
		switch d := labelsVal.(type) {
		case *starlark.Dict:
			items = d.Items()
		case *convert.StarlarkDict:
			items = d.InternalDict().Items()
		default:
			return fmt.Errorf("Resource(%q): labels must be dict or None, got %s",
				resourceName, labelsVal.Type())
		}

		for _, item := range items {
			k, ok := item[0].(starlark.String)
			if !ok {
				return fmt.Errorf("Resource(%q): labels key must be string, got %s",
					resourceName, item[0].Type())
			}
			v, ok := item[1].(starlark.String)
			if !ok {
				return fmt.Errorf("Resource(%q): labels value must be string, got %s",
					resourceName, item[1].Type())
			}
			// Warn if kwarg overrides an auto-injected crossplane label.
			if autoVal, isXP := xpLabels[string(k)]; isXP {
				msg := fmt.Sprintf("Resource %q: labels= kwarg %q=%q overrides auto-injected %q",
					resourceName, string(k), string(v), autoVal)
				cc.mu.Lock()
				cc.events = append(cc.events, CollectedEvent{
					Severity: "Warning", Message: msg, Target: "Composite",
				})
				cc.mu.Unlock()
			}
			bodyLabels.Fields[string(k)] = structpb.NewStringValue(string(v))
		}
	}

	return nil
}
