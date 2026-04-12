package builtins

import (
	"fmt"
	"strings"

	starlarkjson "go.starlark.net/lib/json"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
	sigsk8syaml "sigs.k8s.io/yaml"

	"github.com/wompipomp/function-starlark/convert"
	"github.com/wompipomp/function-starlark/schema"
)

// YAMLModule is the predeclared "yaml" namespace module.
// It provides YAML encoding/decoding functions using sigs.k8s.io/yaml
// for K8s-compatible output with block style and sorted keys.
var YAMLModule = &starlarkstruct.Module{
	Name: "yaml",
	Members: starlark.StringDict{
		"encode":        starlark.NewBuiltin("yaml.encode", yamlEncode),
		"decode":        starlark.NewBuiltin("yaml.decode", yamlDecode),
		"decode_stream": starlark.NewBuiltin("yaml.decode_stream", yamlDecodeStream),
	},
}

// starlarkToGo recursively converts a Starlark value to a Go interface{}
// suitable for YAML marshaling. It handles all three dict types transparently.
func starlarkToGo(v starlark.Value) (interface{}, error) {
	switch v := v.(type) {
	case starlark.NoneType:
		return nil, nil

	case starlark.Bool:
		return bool(v), nil

	case starlark.Int:
		n, ok := v.Int64()
		if !ok {
			return nil, fmt.Errorf("yaml.encode: integer overflow: %s", v.String())
		}
		return n, nil

	case starlark.Float:
		return float64(v), nil

	case starlark.String:
		return string(v), nil

	case *starlark.List:
		result := make([]interface{}, v.Len())
		for i := 0; i < v.Len(); i++ {
			elem, err := starlarkToGo(v.Index(i))
			if err != nil {
				return nil, err
			}
			result[i] = elem
		}
		return result, nil

	case starlark.Tuple:
		result := make([]interface{}, len(v))
		for i, elem := range v {
			goElem, err := starlarkToGo(elem)
			if err != nil {
				return nil, err
			}
			result[i] = goElem
		}
		return result, nil

	case *starlark.Dict:
		return dictToGoMap(v.Items())

	case *convert.StarlarkDict:
		return dictToGoMap(v.InternalDict().Items())

	case *schema.SchemaDict:
		return dictToGoMap(v.InternalDict().Items())

	default:
		return nil, fmt.Errorf("yaml.encode: unsupported type %s", v.Type())
	}
}

// dictToGoMap converts Starlark dict items ([]starlark.Tuple) to a
// map[string]interface{} suitable for YAML marshaling.
func dictToGoMap(items []starlark.Tuple) (map[string]interface{}, error) {
	result := make(map[string]interface{}, len(items))
	for _, item := range items {
		key, ok := item[0].(starlark.String)
		if !ok {
			return nil, fmt.Errorf("yaml.encode: dict key is %s, want string", item[0].Type())
		}
		val, err := starlarkToGo(item[1])
		if err != nil {
			return nil, err
		}
		result[string(key)] = val
	}
	return result, nil
}

// yamlEncode implements yaml.encode(value) -> YAML string.
func yamlEncode(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var value starlark.Value
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "value", &value); err != nil {
		return nil, err
	}

	goVal, err := starlarkToGo(value)
	if err != nil {
		return nil, err
	}

	yamlBytes, err := sigsk8syaml.Marshal(goVal)
	if err != nil {
		return nil, fmt.Errorf("yaml.encode: %w", err)
	}

	trimmed := strings.TrimRight(string(yamlBytes), "\n")
	return starlark.String(trimmed), nil
}

// yamlDecode implements yaml.decode(s) -> Starlark value.
// It uses the YAML->JSON->starlark.json.decode pipeline to guarantee
// identical type mapping with json.decode.
func yamlDecode(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var s string
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "s", &s); err != nil {
		return nil, err
	}

	return yamlDecodeString(thread, s)
}

// yamlDecodeString converts a single YAML document string to a Starlark value
// via the YAML->JSON->json.decode pipeline.
func yamlDecodeString(thread *starlark.Thread, s string) (starlark.Value, error) {
	jsonBytes, err := sigsk8syaml.YAMLToJSON([]byte(s))
	if err != nil {
		return nil, fmt.Errorf("yaml.decode: %w", err)
	}

	decodeFn := starlarkjson.Module.Members["decode"]
	result, err := starlark.Call(thread, decodeFn, starlark.Tuple{starlark.String(string(jsonBytes))}, nil)
	if err != nil {
		return nil, fmt.Errorf("yaml.decode: %w", err)
	}

	return result, nil
}

// yamlDecodeStream implements yaml.decode_stream(s) -> list of Starlark values.
// It splits input on "---" document separators and decodes each non-empty document.
func yamlDecodeStream(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var s string
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "s", &s); err != nil {
		return nil, err
	}

	if strings.TrimSpace(s) == "" {
		return starlark.NewList(nil), nil
	}

	// Split input into documents by scanning lines for "---" boundaries.
	docs := splitYAMLDocuments(s)

	var results []starlark.Value
	for _, doc := range docs {
		trimmed := strings.TrimSpace(doc)
		if trimmed == "" {
			continue
		}

		val, err := yamlDecodeString(thread, doc)
		if err != nil {
			return nil, err
		}
		results = append(results, val)
	}

	return starlark.NewList(results), nil
}

// splitYAMLDocuments splits a multi-document YAML string into individual documents.
// Documents are separated by lines consisting of "---" (optionally followed by whitespace).
func splitYAMLDocuments(s string) []string {
	var docs []string
	var current strings.Builder

	lines := strings.Split(s, "\n")
	for _, line := range lines {
		trimmed := strings.TrimRight(line, " \t")
		if trimmed == "---" {
			doc := current.String()
			if doc != "" {
				docs = append(docs, doc)
			}
			current.Reset()
			continue
		}
		if current.Len() > 0 {
			current.WriteByte('\n')
		}
		current.WriteString(line)
	}

	// Don't forget trailing content after the last --- (or if no --- at all).
	if current.Len() > 0 {
		docs = append(docs, current.String())
	}

	return docs
}
