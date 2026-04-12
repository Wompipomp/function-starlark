package builtins

import (
	"fmt"
	"strings"

	fnv1 "github.com/crossplane/function-sdk-go/proto/v1"
	"github.com/crossplane/function-sdk-go/resource"
	starlarkjson "go.starlark.net/lib/json"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"

	"github.com/wompipomp/function-starlark/convert"
	"github.com/wompipomp/function-starlark/schema"
)

// BuildGlobals constructs the predeclared Starlark globals from a
// RunFunctionRequest and all collectors. It returns a StringDict containing:
//   - oxr: frozen StarlarkDict of the observed composite resource
//   - dxr: mutable StarlarkDict of the desired composite resource
//   - observed: frozen StarlarkDict of frozen StarlarkDicts keyed by resource name
//   - context: mutable plain starlark.Dict of pipeline context
//   - environment: frozen StarlarkDict of EnvironmentConfig data
//   - extra_resources: frozen plain starlark.Dict of extra/required resources
//   - Resource: the collector's builtin for producing desired composed resources
//   - get: utility builtin for safe nested dict access
//   - get_label: utility builtin for safe label value lookup (no dot-splitting)
//   - get_annotation: utility builtin for safe annotation value lookup (no dot-splitting)
//   - set_condition: builtin for setting XR conditions
//   - emit_event: builtin for emitting Normal/Warning events
//   - fatal: builtin for halting execution with a fatal error
//   - set_connection_details: builtin for setting XR-level connection details
//   - set_xr_status: builtin for writing values into dxr.status at dot-paths
//   - get_observed: utility builtin for one-call observed resource field lookup
//   - require_extra_resource: builtin for requesting a single extra resource
//   - require_extra_resources: builtin for requesting multiple extra resources
//   - schema: builtin for defining typed constructors
//   - field: builtin for defining field descriptors
func BuildGlobals(
	req *fnv1.RunFunctionRequest,
	collector *Collector,
	condCollector *ConditionCollector,
	connCollector *ConnectionCollector,
	reqCollector *RequirementsCollector,
) (starlark.StringDict, error) {
	// Build oxr (frozen) from observed composite.
	oxr, err := convert.StructToStarlark(req.GetObserved().GetComposite().GetResource(), true)
	if err != nil {
		return nil, fmt.Errorf("building oxr: %w", err)
	}

	// Build dxr (mutable) from desired composite. Nil means first-in-pipeline.
	dxr, err := convert.StructToStarlark(req.GetDesired().GetComposite().GetResource(), false)
	if err != nil {
		return nil, fmt.Errorf("building dxr: %w", err)
	}

	// Build observed composed resources dict (frozen).
	observed, err := buildObservedDict(req)
	if err != nil {
		return nil, fmt.Errorf("building observed: %w", err)
	}

	// Build pipeline context (mutable plain dict).
	ctxDict, err := buildContextDict(req)
	if err != nil {
		return nil, fmt.Errorf("building context: %w", err)
	}

	// Build environment (frozen StarlarkDict from well-known context key).
	envDict, err := buildEnvironmentDict(req)
	if err != nil {
		return nil, fmt.Errorf("building environment: %w", err)
	}

	// Build extra resources (frozen plain dict).
	extraRes, err := buildExtraResourcesDict(req)
	if err != nil {
		return nil, fmt.Errorf("building extra_resources: %w", err)
	}

	return starlark.StringDict{
		"oxr":                    oxr,
		"dxr":                    dxr,
		"observed":               observed,
		"context":                ctxDict,
		"environment":            envDict,
		"extra_resources":        extraRes,
		"Resource":               collector.Builtin(),
		"skip_resource":          collector.SkipResourceBuiltin(),
		"get":                    starlark.NewBuiltin("get", getFnImpl),
		"get_label":              starlark.NewBuiltin("get_label", getLabelImpl),
		"get_annotation":         starlark.NewBuiltin("get_annotation", getAnnotationImpl),
		"set_condition":          condCollector.SetConditionBuiltin(),
		"emit_event":             condCollector.EmitEventBuiltin(),
		"fatal":                  condCollector.FatalBuiltin(),
		"set_connection_details": connCollector.SetConnectionDetailsBuiltin(),
		"set_xr_status": starlark.NewBuiltin("set_xr_status", func(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
			return setXRStatus(b.Name(), dxr, args, kwargs)
		}),
		"get_observed": starlark.NewBuiltin("get_observed", func(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
			return getObservedImpl(b.Name(), observed, args, kwargs)
		}),
		"require_extra_resource":  reqCollector.RequireExtraResourceBuiltin(),
		"require_extra_resources": reqCollector.RequireExtraResourcesBuiltin(),
		"get_extra_resource": starlark.NewBuiltin("get_extra_resource", func(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
			return getExtraResourceImpl(b.Name(), extraRes, args, kwargs)
		}),
		"get_extra_resources": starlark.NewBuiltin("get_extra_resources", func(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
			return getExtraResourcesImpl(b.Name(), extraRes, args, kwargs)
		}),
		"is_observed": starlark.NewBuiltin("is_observed", func(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
			return isObservedImpl(b.Name(), observed, args, kwargs)
		}),
		"observed_body": starlark.NewBuiltin("observed_body", func(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
			return observedBodyImpl(b.Name(), observed, args, kwargs)
		}),
		"get_condition": starlark.NewBuiltin("get_condition", func(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
			return getConditionImpl(b.Name(), observed, args, kwargs)
		}),
		"schema":                  schema.SchemaBuiltin(),
		"field":                   schema.FieldBuiltin(),
		"struct":                  starlark.NewBuiltin("struct", starlarkstruct.Make),
		"json":                    starlarkjson.Module,
		"crypto":                  CryptoModule,
		"encoding":                EncodingModule,
		"dict":                    DictModule,
		"regex":                   RegexModule,
		"yaml":                    YAMLModule,
	}, nil
}

// buildObservedDict creates a frozen StarlarkDict of frozen StarlarkDicts
// from the observed composed resources in the request.
func buildObservedDict(req *fnv1.RunFunctionRequest) (*convert.StarlarkDict, error) {
	resources := req.GetObserved().GetResources()
	observed := convert.NewStarlarkDict(len(resources))
	for name, r := range resources {
		d, err := convert.StructToStarlark(r.GetResource(), true) // frozen
		if err != nil {
			return nil, fmt.Errorf("observed resource %q: %w", name, err)
		}
		if err := observed.SetKey(starlark.String(name), d); err != nil {
			return nil, fmt.Errorf("observed resource %q: %w", name, err)
		}
	}
	observed.Freeze()
	return observed, nil
}

// getFnImpl implements get(obj, path, default=None) for safe nested dict access.
// path can be a dot-separated string ("spec.parameters.region") or a list
// of keys (["metadata", "annotations", "app.kubernetes.io/name"]).
func getFnImpl(
	_ *starlark.Thread,
	b *starlark.Builtin,
	args starlark.Tuple,
	kwargs []starlark.Tuple,
) (starlark.Value, error) {
	var obj starlark.Value
	var path starlark.Value
	var dflt starlark.Value = starlark.None

	if err := starlark.UnpackArgs(b.Name(), args, kwargs,
		"obj", &obj, "path", &path, "default?", &dflt); err != nil {
		return nil, err
	}

	keys, err := pathToKeys(path)
	if err != nil {
		return nil, err
	}

	current := obj
	for _, key := range keys {
		mapping, ok := current.(starlark.Mapping)
		if !ok {
			return dflt, nil
		}
		v, found, err := mapping.Get(starlark.String(key))
		if err != nil || !found || v == starlark.None {
			return dflt, nil
		}
		current = v
	}
	return current, nil
}

// metadataLookup safely retrieves a value from res.metadata.<mapName>.<key>
// using direct key lookup (no dot-splitting). It returns dflt when any
// intermediate level is missing or not a Mapping.
func metadataLookup(res starlark.Value, key string, dflt starlark.Value, mapName string) (starlark.Value, error) {
	// Level 1: res must be a Mapping.
	resMapping, ok := res.(starlark.Mapping)
	if !ok {
		return dflt, nil
	}

	// Level 2: Get "metadata" from res.
	metaVal, found, err := resMapping.Get(starlark.String("metadata"))
	if err != nil || !found || metaVal == starlark.None {
		return dflt, nil
	}
	metaMapping, ok := metaVal.(starlark.Mapping)
	if !ok {
		return dflt, nil
	}

	// Level 3: Get mapName ("labels" or "annotations") from metadata.
	mapVal, found, err := metaMapping.Get(starlark.String(mapName))
	if err != nil || !found || mapVal == starlark.None {
		return dflt, nil
	}
	targetMapping, ok := mapVal.(starlark.Mapping)
	if !ok {
		return dflt, nil
	}

	// Level 4: Get key from target map (direct lookup, no dot-splitting).
	v, found, err := targetMapping.Get(starlark.String(key))
	if err != nil || !found {
		return dflt, nil
	}
	return v, nil
}

// getLabelImpl implements get_label(res, key, default=None) for safe label lookup.
func getLabelImpl(
	_ *starlark.Thread,
	b *starlark.Builtin,
	args starlark.Tuple,
	kwargs []starlark.Tuple,
) (starlark.Value, error) {
	var res starlark.Value
	var key string
	var dflt starlark.Value = starlark.None

	if err := starlark.UnpackArgs(b.Name(), args, kwargs,
		"res", &res, "key", &key, "default?", &dflt); err != nil {
		return nil, err
	}

	if key == "" {
		return nil, fmt.Errorf("%s: key must not be empty", b.Name())
	}

	return metadataLookup(res, key, dflt, "labels")
}

// getAnnotationImpl implements get_annotation(res, key, default=None) for safe annotation lookup.
func getAnnotationImpl(
	_ *starlark.Thread,
	b *starlark.Builtin,
	args starlark.Tuple,
	kwargs []starlark.Tuple,
) (starlark.Value, error) {
	var res starlark.Value
	var key string
	var dflt starlark.Value = starlark.None

	if err := starlark.UnpackArgs(b.Name(), args, kwargs,
		"res", &res, "key", &key, "default?", &dflt); err != nil {
		return nil, err
	}

	if key == "" {
		return nil, fmt.Errorf("%s: key must not be empty", b.Name())
	}

	return metadataLookup(res, key, dflt, "annotations")
}

// setXRStatus writes a value into dxr["status"] at the given dot-path,
// auto-creating intermediate *convert.StarlarkDict entries as needed.
// It uses mkdir -p semantics: non-dict values at intermediate path segments
// are silently overwritten with new StarlarkDicts.
func setXRStatus(fnName string, dxr *convert.StarlarkDict, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var path string
	var value starlark.Value

	if err := starlark.UnpackArgs(fnName, args, kwargs,
		"path", &path, "value", &value); err != nil {
		return nil, err
	}

	// Validate path.
	if path == "" {
		return nil, fmt.Errorf("%s: path must not be empty", fnName)
	}
	if strings.HasPrefix(path, ".") || strings.HasSuffix(path, ".") || strings.Contains(path, "..") {
		return nil, fmt.Errorf("%s: malformed path %q", fnName, path)
	}

	// Build full segment list: ["status", ...user segments...].
	segments := strings.Split(path, ".")
	allSegments := make([]string, 0, len(segments)+1)
	allSegments = append(allSegments, "status")
	allSegments = append(allSegments, segments...)

	// Walk from dxr through intermediate segments, creating dicts as needed.
	var current starlark.Value = dxr
	for _, seg := range allSegments[:len(allSegments)-1] {
		parent, ok := current.(starlark.HasSetKey)
		if !ok {
			return nil, fmt.Errorf("%s: cannot set key on %s", fnName, current.Type())
		}

		mapping, isMapping := current.(starlark.Mapping)
		var next starlark.Value
		if isMapping {
			v, found, err := mapping.Get(starlark.String(seg))
			if err != nil {
				return nil, err
			}
			if found {
				if _, isM := v.(starlark.Mapping); isM {
					next = v
				}
			}
		}

		if next == nil {
			// Auto-create intermediate StarlarkDict.
			newDict := convert.NewStarlarkDict(0)
			if err := parent.SetKey(starlark.String(seg), newDict); err != nil {
				return nil, err
			}
			next = newDict
		}
		current = next
	}

	// Write the leaf value.
	leaf, ok := current.(starlark.HasSetKey)
	if !ok {
		return nil, fmt.Errorf("%s: cannot set key on %s", fnName, current.Type())
	}
	if err := leaf.SetKey(starlark.String(allSegments[len(allSegments)-1]), value); err != nil {
		return nil, err
	}

	return starlark.None, nil
}

// getObservedImpl implements get_observed(name, path, default=None).
// It looks up an observed resource by name, then traverses the path
// using the same pathToKeys + Mapping walk as get().
func getObservedImpl(
	fnName string,
	observed *convert.StarlarkDict,
	args starlark.Tuple,
	kwargs []starlark.Tuple,
) (starlark.Value, error) {
	var name string
	var path starlark.Value
	var dflt starlark.Value = starlark.None

	if err := starlark.UnpackArgs(fnName, args, kwargs,
		"name", &name, "path", &path, "default?", &dflt); err != nil {
		return nil, err
	}

	// Validate name.
	if name == "" {
		return nil, fmt.Errorf("%s: name must not be empty", fnName)
	}

	// Validate path not empty (both "" and [] are rejected).
	switch p := path.(type) {
	case starlark.String:
		if string(p) == "" {
			return nil, fmt.Errorf("%s: path must not be empty", fnName)
		}
	case *starlark.List:
		if p.Len() == 0 {
			return nil, fmt.Errorf("%s: path must not be empty", fnName)
		}
	}

	// Convert path to keys.
	keys, err := pathToKeys(path)
	if err != nil {
		return nil, err
	}

	// Step 1: Look up resource by name in observed dict.
	res, found, err := observed.Get(starlark.String(name))
	if err != nil || !found || res == starlark.None {
		return dflt, nil
	}

	// Step 2: Walk the path (same as getFnImpl loop).
	current := res
	for _, key := range keys {
		mapping, ok := current.(starlark.Mapping)
		if !ok {
			return dflt, nil
		}
		v, found, err := mapping.Get(starlark.String(key))
		if err != nil || !found || v == starlark.None {
			return dflt, nil
		}
		current = v
	}
	return current, nil
}

// pathToKeys converts a path value to a slice of string keys.
func pathToKeys(path starlark.Value) ([]string, error) {
	switch p := path.(type) {
	case starlark.String:
		return strings.Split(string(p), "."), nil
	case *starlark.List:
		keys := make([]string, p.Len())
		for i := 0; i < p.Len(); i++ {
			s, ok := p.Index(i).(starlark.String)
			if !ok {
				return nil, fmt.Errorf("get: path list element %d is %s, want string", i, p.Index(i).Type())
			}
			keys[i] = string(s)
		}
		return keys, nil
	default:
		return nil, fmt.Errorf("get: path must be string or list, got %s", path.Type())
	}
}

// ApplyResources merges collected resources into the response without
// overwriting prior desired resources not touched by the collector.
func ApplyResources(rsp *fnv1.RunFunctionResponse, collector *Collector) error {
	collected := collector.Resources()
	if len(collected) == 0 {
		return nil
	}

	// Ensure Desired and Resources maps exist.
	if rsp.Desired == nil {
		rsp.Desired = &fnv1.State{}
	}
	if rsp.Desired.Resources == nil {
		rsp.Desired.Resources = make(map[string]*fnv1.Resource)
	}

	for name, cr := range collected {
		rsp.Desired.Resources[name] = &fnv1.Resource{
			Resource:          cr.Body,
			Ready:             readyToProto(cr.Ready),
			ConnectionDetails: cr.ConnectionDetails,
		}
	}
	return nil
}

// ApplyDXR converts the mutable dxr StarlarkDict back to protobuf and sets
// it on the response desired composite.
func ApplyDXR(rsp *fnv1.RunFunctionResponse, dxr starlark.Value) error {
	sd, ok := dxr.(*convert.StarlarkDict)
	if !ok {
		return fmt.Errorf("dxr is %T, want *convert.StarlarkDict", dxr)
	}

	s, err := convert.StarlarkToStruct(sd)
	if err != nil {
		return fmt.Errorf("converting dxr: %w", err)
	}

	if rsp.Desired == nil {
		rsp.Desired = &fnv1.State{}
	}
	if rsp.Desired.Composite == nil {
		rsp.Desired.Composite = &fnv1.Resource{}
	}
	rsp.Desired.Composite.Resource = s
	return nil
}

// readyToProto maps the resource.Ready type to the protobuf Ready enum.
func readyToProto(r resource.Ready) fnv1.Ready {
	switch r {
	case resource.ReadyTrue:
		return fnv1.Ready_READY_TRUE
	case resource.ReadyFalse:
		return fnv1.Ready_READY_FALSE
	default:
		return fnv1.Ready_READY_UNSPECIFIED
	}
}
