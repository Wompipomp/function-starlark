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

// Collector accumulates Resource() calls from Starlark scripts.
// Duplicate names use last-wins semantics.
type Collector struct {
	mu           sync.Mutex
	resources    map[string]CollectedResource
	skipped      map[string]bool
	dependencies []DependencyPair
	cc           *ConditionCollector
	scriptName   string
	oxr          *structpb.Struct // frozen observed XR, for label extraction
}

// NewCollector creates an empty Collector. The ConditionCollector is used to
// emit Warning events when the external_name or labels kwarg conflicts with
// existing values. The scriptName is recorded for use in metric labels. The
// oxr is the observed composite resource struct used for auto-injecting
// crossplane traceability labels.
func NewCollector(cc *ConditionCollector, scriptName string, oxr *structpb.Struct) *Collector {
	return &Collector{
		resources:  make(map[string]CollectedResource),
		skipped:    make(map[string]bool),
		cc:         cc,
		scriptName: scriptName,
		oxr:        oxr,
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

// skipResourceFn implements skip_resource(name, reason). It records the skip,
// emits a Warning event on first call for a given name, and errors if the
// resource was already emitted via Resource().
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

	c.mu.Lock()
	// Error if already emitted via Resource().
	if _, exists := c.resources[name]; exists {
		c.mu.Unlock()
		return nil, fmt.Errorf("resource %q already emitted, cannot skip", name)
	}
	// Deduplicate: only first skip emits warning.
	if c.skipped[name] {
		c.mu.Unlock()
		return starlark.None, nil
	}
	c.skipped[name] = true
	c.mu.Unlock()

	metrics.ResourcesSkippedTotal.WithLabelValues(c.scriptName).Inc()

	// Emit Warning event. Release c.mu before acquiring cc.mu to avoid
	// lock ordering issues (Pitfall 4 from RESEARCH.md).
	msg := fmt.Sprintf("Skipping resource %q: %s", name, reason)
	c.cc.mu.Lock()
	c.cc.events = append(c.cc.events, CollectedEvent{
		Severity: "Warning", Message: msg, Target: "Composite",
	})
	c.cc.mu.Unlock()

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

// resourceFn implements the Resource(name, body, ready=None, labels=None, connection_details=None, depends_on=None, external_name=None) Starlark builtin.
func (c *Collector) resourceFn(
	_ *starlark.Thread,
	b *starlark.Builtin,
	args starlark.Tuple,
	kwargs []starlark.Tuple,
) (starlark.Value, error) {
	var name string
	var body *starlark.Dict
	var connDetails *starlark.Dict
	var dependsOn *starlark.List
	var readyVal starlark.Value = starlark.None
	var labelsVal starlark.Value
	var externalNameVal starlark.Value

	if err := starlark.UnpackArgs(b.Name(), args, kwargs,
		"name", &name, "body", &body, "ready?", &readyVal,
		"labels?", &labelsVal,
		"connection_details??", &connDetails,
		"depends_on??", &dependsOn,
		"external_name??", &externalNameVal); err != nil {
		return nil, err
	}

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

	// Process depends_on list if provided.
	if dependsOn != nil {
		for i := 0; i < dependsOn.Len(); i++ {
			item := dependsOn.Index(i)
			switch v := item.(type) {
			case *ResourceRef:
				c.addDependency(name, v.name, true, "")
			case starlark.String:
				c.addDependency(name, string(v), false, "")
			case starlark.Tuple:
				if v.Len() != 2 {
					return nil, fmt.Errorf("Resource(%q): depends_on[%d] tuple must have exactly 2 elements, got %d",
						name, i, v.Len())
				}
				// Extract first element: must be ResourceRef or string.
				var depName string
				var isRef bool
				switch first := v.Index(0).(type) {
				case *ResourceRef:
					depName = first.name
					isRef = true
				case starlark.String:
					depName = string(first)
					isRef = false
				default:
					return nil, fmt.Errorf("Resource(%q): depends_on[%d] tuple first element must be ResourceRef or string, got %s",
						name, i, v.Index(0).Type())
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
				return nil, fmt.Errorf("Resource(%q): depends_on[%d] must be ResourceRef, string, or tuple, got %s",
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
