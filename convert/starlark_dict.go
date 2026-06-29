// Package convert provides bidirectional conversion between protobuf structpb
// values and Starlark values, centered around the custom StarlarkDict type.
package convert

import (
	"fmt"
	"sort"
	"sync"

	"go.starlark.net/starlark"
	"go.starlark.net/syntax"
	"google.golang.org/protobuf/types/known/structpb"
)

// builtinMethodNames lists the dict built-in methods that StarlarkDict exposes.
var builtinMethodNames = []string{
	"clear",
	"get",
	"items",
	"keys",
	"pop",
	"setdefault",
	"update",
	"values",
}

// Compile-time interface compliance checks.
var (
	_ starlark.Value       = (*StarlarkDict)(nil)
	_ starlark.HasAttrs    = (*StarlarkDict)(nil)
	_ starlark.HasSetField = (*StarlarkDict)(nil)
	_ starlark.HasSetKey   = (*StarlarkDict)(nil)
	_ starlark.Mapping     = (*StarlarkDict)(nil)
	_ starlark.Iterable    = (*StarlarkDict)(nil)
	_ starlark.Comparable  = (*StarlarkDict)(nil)
)

// StarlarkDict wraps an ordered map of string keys to Starlark values,
// supporting both dot access (d.key) and bracket access (d["key"]).
// Missing key via dot access returns starlark.None (not an error),
// which is lenient behavior for optional Kubernetes fields.
//
// A StarlarkDict may be constructed lazily (see NewLazyStarlarkDict): when
// lazy is non-nil the underlying dict is materialized from the protobuf struct
// on first access. This lets callers hold many resource bodies (e.g. observed
// composed resources) without paying the conversion cost for the ones a script
// never reads. The lazy path is transparent: the type is unchanged, so every
// existing consumer that type-switches on *StarlarkDict still works.
type StarlarkDict struct {
	d *starlark.Dict

	// lazy, when non-nil, holds the deferred-materialization state for a dict
	// built from a protobuf struct (see NewLazyStarlarkDict). It is nil for
	// eagerly-built dicts, keeping the common case two pointers wide instead of
	// embedding the sync.Once/error/flag by value in every StarlarkDict.
	lazy *lazyState
}

// lazyState holds the deferred conversion state for a lazily-built StarlarkDict.
// It is heap-allocated only for lazy dicts, so eager dicts (oxr, dxr, and every
// nested converted body) do not pay for the sync.Once, error, and flag fields.
type lazyState struct {
	src    *structpb.Struct // protobuf struct converted into the dict on first access
	once   sync.Once
	err    error // conversion error from materialization, surfaced on access
	frozen bool  // freeze requested before materialization
}

// NewStarlarkDict creates a new empty StarlarkDict, presizing the underlying
// hashtable for at least size insertions to avoid incremental rehashing.
func NewStarlarkDict(size int) *StarlarkDict {
	return &StarlarkDict{d: starlark.NewDict(size)}
}

// NewLazyStarlarkDict returns a StarlarkDict that defers converting s into
// Starlark values until the dict is first accessed. The materialized dict is
// frozen iff Freeze is called before first access (which is the normal flow for
// read-only request data such as observed and required resources).
func NewLazyStarlarkDict(s *structpb.Struct) *StarlarkDict {
	return &StarlarkDict{lazy: &lazyState{src: s}}
}

// ensure materializes the lazy dict on first call and returns any conversion
// error. For eagerly-built dicts it is a single nil check.
func (sd *StarlarkDict) ensure() error {
	if sd.lazy == nil {
		if sd.d == nil {
			// Defensive: eagerly-built dicts always have d set; guard a
			// zero-value StarlarkDict so accessors never nil-deref.
			sd.d = starlark.NewDict(0)
		}
		return nil
	}
	sd.lazy.once.Do(func() {
		// A nil src (a resource with no body) converts to an empty dict via
		// StructToStarlark's nil handling, so absent bodies never panic.
		tmp, err := StructToStarlark(sd.lazy.src, sd.lazy.frozen)
		if err != nil {
			// Materialization failed (e.g. a numeric value outside int64
			// range). Surface the error on access; degrade to an empty dict
			// so non-error methods stay safe. In practice request data comes
			// pre-validated from Crossplane, so this path is not expected.
			sd.lazy.err = err
			sd.d = starlark.NewDict(0)
			if sd.lazy.frozen {
				sd.d.Freeze()
			}
			return
		}
		sd.d = tmp.d
	})
	return sd.lazy.err
}

// dict materializes (if needed) and returns the underlying *starlark.Dict for
// methods that cannot propagate a conversion error.
func (sd *StarlarkDict) dict() *starlark.Dict {
	_ = sd.ensure()
	return sd.d
}

// materialized is dict() for callers that can return an error: it surfaces a
// lazy-conversion failure instead of silently degrading to an empty dict.
func (sd *StarlarkDict) materialized() (*starlark.Dict, error) {
	if err := sd.ensure(); err != nil {
		return nil, err
	}
	return sd.d, nil
}

// InternalDict returns the underlying starlark.Dict for direct access
// by other convert package functions (e.g., conversion). It materializes a
// lazy dict on first call.
func (sd *StarlarkDict) InternalDict() *starlark.Dict {
	return sd.dict()
}

// --- starlark.Value ---

// String returns the Starlark string representation, delegating to the internal Dict.
func (sd *StarlarkDict) String() string {
	return sd.dict().String()
}

// Type returns "dict" to match Starlark dict conventions.
func (sd *StarlarkDict) Type() string {
	return "dict"
}

// Freeze makes the dict and all nested values immutable. For a not-yet
// materialized lazy dict, freezing is deferred to materialization so that
// freezing does not force conversion of resource bodies a script never reads.
func (sd *StarlarkDict) Freeze() {
	if sd.lazy != nil && sd.d == nil {
		// Not yet materialized: defer the freeze so we don't force conversion
		// of a body the script may never read.
		sd.lazy.frozen = true
		return
	}
	if sd.d != nil {
		sd.d.Freeze()
	}
}

// Truth returns True for non-empty dicts, False for empty.
func (sd *StarlarkDict) Truth() starlark.Bool {
	return sd.dict().Truth()
}

// Hash returns an error because dicts are unhashable.
func (sd *StarlarkDict) Hash() (uint32, error) {
	return 0, fmt.Errorf("unhashable type: dict")
}

// --- starlark.HasAttrs ---

// Attr looks up a key by name. If the key exists, it returns the value.
// If the key names a built-in method, it returns a bound builtin.
// If the key is not found, it returns (starlark.None, nil) for lenient
// Kubernetes optional field access.
func (sd *StarlarkDict) Attr(name string) (starlark.Value, error) {
	// Check built-in methods first.
	if b := sd.builtinMethod(name); b != nil {
		return b, nil
	}

	// Materialize and surface any lazy-conversion error, consistent with Get;
	// routing through dict() would discard it and degrade to None (data loss).
	if err := sd.ensure(); err != nil {
		return nil, err
	}
	v, found, err := sd.d.Get(starlark.String(name))
	if err != nil {
		return nil, err
	}
	if !found {
		return starlark.None, nil
	}
	return v, nil
}

// AttrNames returns a sorted list of all keys plus built-in method names.
func (sd *StarlarkDict) AttrNames() []string {
	d := sd.dict()
	// Collect data keys.
	keys := make([]string, 0, d.Len()+len(builtinMethodNames))
	for _, item := range d.Items() {
		if s, ok := item[0].(starlark.String); ok {
			keys = append(keys, string(s))
		}
	}
	// Append built-in method names.
	keys = append(keys, builtinMethodNames...)
	sort.Strings(keys)
	return keys
}

// --- starlark.HasSetField ---

// SetField sets a key by name. Returns an error if the dict is frozen.
func (sd *StarlarkDict) SetField(name string, val starlark.Value) error {
	return sd.dict().SetKey(starlark.String(name), val)
}

// --- starlark.HasSetKey ---

// SetKey sets a key-value pair. Returns an error if the dict is frozen.
func (sd *StarlarkDict) SetKey(k, v starlark.Value) error {
	return sd.dict().SetKey(k, v)
}

// --- starlark.Mapping ---

// Get looks up a key and returns its value.
func (sd *StarlarkDict) Get(key starlark.Value) (v starlark.Value, found bool, err error) {
	if err := sd.ensure(); err != nil {
		return nil, false, err
	}
	return sd.d.Get(key)
}

// --- starlark.Iterable ---

// Iterate returns an iterator over the dict keys in insertion order.
func (sd *StarlarkDict) Iterate() starlark.Iterator {
	return sd.dict().Iterate()
}

// --- starlark.Comparable ---

// CompareSameType compares two StarlarkDicts.
func (sd *StarlarkDict) CompareSameType(op syntax.Token, y_ starlark.Value, depth int) (bool, error) {
	other, ok := y_.(*StarlarkDict)
	if !ok {
		return false, fmt.Errorf("cannot compare dict with %s", y_.Type())
	}
	// Delegate to the internal dicts' comparison.
	return sd.dict().CompareSameType(op, other.dict(), depth)
}

// --- Len ---

// Len returns the number of entries in the dict.
func (sd *StarlarkDict) Len() int {
	return sd.dict().Len()
}

// --- Built-in methods ---

// builtinMethod returns a bound starlark.Builtin for the named method, or nil.
func (sd *StarlarkDict) builtinMethod(name string) *starlark.Builtin {
	switch name {
	case "keys":
		return starlark.NewBuiltin("keys", sd.keysMethod)
	case "values":
		return starlark.NewBuiltin("values", sd.valuesMethod)
	case "items":
		return starlark.NewBuiltin("items", sd.itemsMethod)
	case "get":
		return starlark.NewBuiltin("get", sd.getMethod)
	case "pop":
		return starlark.NewBuiltin("pop", sd.popMethod)
	case "update":
		return starlark.NewBuiltin("update", sd.updateMethod)
	case "clear":
		return starlark.NewBuiltin("clear", sd.clearMethod)
	case "setdefault":
		return starlark.NewBuiltin("setdefault", sd.setdefaultMethod)
	default:
		return nil
	}
}

// keysMethod implements dict.keys() -> list of keys.
func (sd *StarlarkDict) keysMethod(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if err := starlark.UnpackPositionalArgs("keys", args, kwargs, 0); err != nil {
		return nil, err
	}
	d, err := sd.materialized()
	if err != nil {
		return nil, err
	}
	items := d.Items()
	keys := make([]starlark.Value, len(items))
	for i, item := range items {
		keys[i] = item[0]
	}
	return starlark.NewList(keys), nil
}

// valuesMethod implements dict.values() -> list of values.
func (sd *StarlarkDict) valuesMethod(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if err := starlark.UnpackPositionalArgs("values", args, kwargs, 0); err != nil {
		return nil, err
	}
	d, err := sd.materialized()
	if err != nil {
		return nil, err
	}
	items := d.Items()
	vals := make([]starlark.Value, len(items))
	for i, item := range items {
		vals[i] = item[1]
	}
	return starlark.NewList(vals), nil
}

// itemsMethod implements dict.items() -> list of (key, value) tuples.
func (sd *StarlarkDict) itemsMethod(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if err := starlark.UnpackPositionalArgs("items", args, kwargs, 0); err != nil {
		return nil, err
	}
	d, err := sd.materialized()
	if err != nil {
		return nil, err
	}
	items := d.Items()
	tuples := make([]starlark.Value, len(items))
	for i, item := range items {
		tuples[i] = starlark.Tuple{item[0], item[1]}
	}
	return starlark.NewList(tuples), nil
}

// getMethod implements dict.get(key, default=None) -> value or default.
func (sd *StarlarkDict) getMethod(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var key starlark.Value
	var dflt starlark.Value = starlark.None
	if err := starlark.UnpackPositionalArgs("get", args, kwargs, 1, &key, &dflt); err != nil {
		return nil, err
	}
	d, err := sd.materialized()
	if err != nil {
		return nil, err
	}
	v, found, err := d.Get(key)
	if err != nil {
		return nil, err
	}
	if !found {
		return dflt, nil
	}
	return v, nil
}

// popMethod implements dict.pop(key, default?) -> remove and return value.
func (sd *StarlarkDict) popMethod(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var key starlark.Value
	var dflt starlark.Value
	if err := starlark.UnpackPositionalArgs("pop", args, kwargs, 1, &key, &dflt); err != nil {
		return nil, err
	}
	v, found, err := sd.dict().Delete(key)
	if err != nil {
		return nil, err
	}
	if !found {
		if dflt != nil {
			return dflt, nil
		}
		return nil, fmt.Errorf("pop: key %s not found", key.String())
	}
	return v, nil
}

// updateMethod implements dict.update(other) -> merge other dict entries.
func (sd *StarlarkDict) updateMethod(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var other starlark.Value
	if err := starlark.UnpackPositionalArgs("update", args, kwargs, 1, &other); err != nil {
		return nil, err
	}

	// Accept *StarlarkDict, *starlark.Dict, or any iterable of pairs.
	switch o := other.(type) {
	case *StarlarkDict:
		for _, item := range o.dict().Items() {
			if err := sd.dict().SetKey(item[0], item[1]); err != nil {
				return nil, err
			}
		}
	case *starlark.Dict:
		for _, item := range o.Items() {
			if err := sd.dict().SetKey(item[0], item[1]); err != nil {
				return nil, err
			}
		}
	default:
		return nil, fmt.Errorf("update: unsupported argument type %s", other.Type())
	}

	return starlark.None, nil
}

// clearMethod implements dict.clear() -> remove all entries.
func (sd *StarlarkDict) clearMethod(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if err := starlark.UnpackPositionalArgs("clear", args, kwargs, 0); err != nil {
		return nil, err
	}
	if err := sd.dict().Clear(); err != nil {
		return nil, err
	}
	return starlark.None, nil
}

// setdefaultMethod implements dict.setdefault(key, default=None) -> return existing or set default.
func (sd *StarlarkDict) setdefaultMethod(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var key starlark.Value
	var dflt starlark.Value = starlark.None
	if err := starlark.UnpackPositionalArgs("setdefault", args, kwargs, 1, &key, &dflt); err != nil {
		return nil, err
	}
	v, found, err := sd.dict().Get(key)
	if err != nil {
		return nil, err
	}
	if found {
		return v, nil
	}
	if err := sd.dict().SetKey(key, dflt); err != nil {
		return nil, err
	}
	return dflt, nil
}
