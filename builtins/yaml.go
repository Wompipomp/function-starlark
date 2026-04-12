package builtins

import (
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

// YAMLModule is the predeclared "yaml" namespace module.
// It provides YAML encoding/decoding functions using sigs.k8s.io/yaml
// for K8s-compatible output.
var YAMLModule = &starlarkstruct.Module{
	Name: "yaml",
	Members: starlark.StringDict{
		"encode":        starlark.NewBuiltin("yaml.encode", yamlEncode),
		"decode":        starlark.NewBuiltin("yaml.decode", yamlDecode),
		"decode_stream": starlark.NewBuiltin("yaml.decode_stream", yamlDecodeStream),
	},
}

func yamlEncode(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	return nil, nil // stub
}

func yamlDecode(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	return nil, nil // stub
}

func yamlDecodeStream(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	return nil, nil // stub
}
