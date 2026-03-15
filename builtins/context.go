package builtins

import (
	"fmt"

	fnv1 "github.com/crossplane/function-sdk-go/proto/v1"
	"go.starlark.net/starlark"

	"github.com/wompipomp/function-starlark/convert"
)

// environmentKey is the well-known context key for EnvironmentConfig data.
const environmentKey = "apiextensions.crossplane.io/environment"

// buildContextDict converts the pipeline context from a RunFunctionRequest
// into a mutable plain *starlark.Dict. Plain starlark.Dict is used because
// context keys contain dots and slashes (e.g., "apiextensions.crossplane.io/environment")
// which would conflict with StarlarkDict dot-access.
func buildContextDict(req *fnv1.RunFunctionRequest) (*starlark.Dict, error) {
	ctx := req.GetContext()
	if ctx == nil {
		return new(starlark.Dict), nil
	}

	d := new(starlark.Dict)
	for k, v := range ctx.GetFields() {
		sv, err := convert.ProtoValueToPlainStarlark(v, false)
		if err != nil {
			return nil, fmt.Errorf("context key %q: %w", k, err)
		}
		if err := d.SetKey(starlark.String(k), sv); err != nil {
			return nil, fmt.Errorf("context key %q: %w", k, err)
		}
	}
	return d, nil
}

// buildEnvironmentDict extracts the EnvironmentConfig data from the pipeline
// context and returns it as a frozen StarlarkDict. If the environment key is
// missing, nil, or not a struct value, an empty frozen StarlarkDict is returned.
func buildEnvironmentDict(req *fnv1.RunFunctionRequest) (*convert.StarlarkDict, error) {
	ctx := req.GetContext()
	if ctx == nil {
		d := convert.NewStarlarkDict(0)
		d.Freeze()
		return d, nil
	}

	envVal, ok := ctx.GetFields()[environmentKey]
	if !ok || envVal == nil || envVal.GetStructValue() == nil {
		d := convert.NewStarlarkDict(0)
		d.Freeze()
		return d, nil
	}

	return convert.StructToStarlark(envVal.GetStructValue(), true) // frozen
}

// ApplyContext converts the mutable context *starlark.Dict back to a protobuf
// Struct and merges it into the existing rsp.Context. Keys present in the
// Starlark dict overwrite existing keys; keys only in rsp.Context are preserved.
// This ensures downstream pipeline functions do not lose context keys that
// this script did not modify.
func ApplyContext(rsp *fnv1.RunFunctionResponse, ctxVal starlark.Value) error {
	d, ok := ctxVal.(*starlark.Dict)
	if !ok {
		return fmt.Errorf("context is %T, want *starlark.Dict", ctxVal)
	}

	s, err := convert.PlainDictToStruct(d)
	if err != nil {
		return fmt.Errorf("converting context: %w", err)
	}

	if rsp.Context == nil {
		rsp.Context = s
		return nil
	}

	// Merge: script keys overwrite existing, existing-only keys preserved.
	for k, v := range s.GetFields() {
		rsp.Context.Fields[k] = v
	}
	return nil
}

