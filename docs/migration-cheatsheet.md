# Migration cheatsheet

Quick-reference mapping from Sprig (function-go-templating) and KCL
(function-kcl) helpers to function-starlark equivalents.

For a narrative migration guide with worked examples, see
[Migration from KCL](migration-from-kcl.md). For full function signatures,
see the [builtins reference](builtins-reference.md).

## Helper mapping

| Category | Sprig / Go Template | KCL | function-starlark | Notes |
|----------|---------------------|-----|-------------------|-------|
| Crypto | `sha256sum` | -- | `crypto.sha256(data)` | Hex digest |
| | `sha512sum` | -- | `crypto.sha512(data)` | |
| | `sha1sum` | -- | `crypto.sha1(data)` | |
| | `md5sum` | -- | `crypto.md5(data)` | Non-cryptographic only |
| | -- | -- | `crypto.hmac_sha256(key, msg)` | New -- HMAC digest |
| | -- | -- | `crypto.blake3(data)` | New -- BLAKE3 digest |
| | `randAlphaNum` / `randAlpha` | -- | `crypto.stable_id(seed, length)` | Deterministic replacement |
| | `adler32sum` | -- | No equivalent | |
| | `bcrypt` | -- | No equivalent | Belongs in provider |
| | `genPrivateKey` / `genCA` | -- | No equivalent | Belongs in provider |
| Encoding | `b64enc` | `base64.encode` | `encoding.b64enc(data)` | Standard base64 |
| | `b64dec` | `base64.decode` | `encoding.b64dec(data)` | |
| | `b32enc` | -- | `encoding.b32enc(data)` | No-padding |
| | `b32dec` | -- | `encoding.b32dec(data)` | |
| | -- | -- | `encoding.b64url_enc(data)` | URL-safe, no padding |
| | -- | -- | `encoding.b64url_dec(data)` | |
| | -- | -- | `encoding.hex_enc(data)` | New |
| | -- | -- | `encoding.hex_dec(data)` | |
| JSON | `toJson` / `mustToJson` | `json.encode` | `json.encode(x)` | |
| | `fromJson` / `mustFromJson` | `json.decode` | `json.decode(x)` | |
| | `toPrettyJson` | -- | `json.encode_indent(x)` | |
| | `toRawJson` | -- | `json.encode(x)` | Same as toJson in Starlark |
| YAML | `toYaml` | `yaml.encode` | `yaml.encode(value)` | K8s-compatible |
| | `fromYaml` | `yaml.decode` | `yaml.decode(s)` | |
| | -- | -- | `yaml.decode_stream(s)` | Multi-document |
| Dict | `merge` / `mergeOverwrite` | -- | `dict.merge(*dicts)` | Shallow right-wins |
| | `deepCopy` | -- | `dict.merge(d, {})` | Creates new dict |
| | -- | -- | `dict.deep_merge(*dicts)` | Recursive right-wins |
| | `pick` | -- | `dict.pick(d, keys)` | |
| | `omit` | -- | `dict.omit(d, keys)` | |
| | `unset` | -- | `dict.omit(d, [key])` | |
| | `dig` | `option("params")?.oxr?.spec?.f` | `dict.dig(d, path)` or `get(oxr, path)` | Dotted path |
| | `hasKey` | -- | `dict.has_path(d, path)` or `key in d` | |
| | `get` | -- | `get(obj, path)` or `dict.dig(d, path)` | |
| | `set` | -- | `d[key] = value` | Direct assignment |
| | `dict` | `{}` literal | `{}` literal | |
| | `keys` | -- | `d.keys()` | Built-in method |
| | `values` | -- | `d.values()` | Built-in method |
| | `pluck` | -- | `[get(d, key) for d in dicts]` | List comprehension |
| Regex | `regexMatch` | `regex.match` | `regex.match(pattern, s)` | Bool |
| | `regexFind` | `regex.findall` | `regex.find(pattern, s)` | First match or None |
| | `regexFindAll` | -- | `regex.find_all(pattern, s)` | All matches |
| | -- | -- | `regex.find_groups(pattern, s)` | Capture groups |
| | -- | `regex.replace` | `regex.replace(pattern, s, repl)` | First match |
| | `regexReplaceAll` | `regex.replace` | `regex.replace_all(pattern, s, repl)` | All matches |
| | `regexSplit` | `regex.split` | `regex.split(pattern, s)` | |

## Concepts

| Concept | Sprig / Go Template | KCL | function-starlark |
|---------|---------------------|-----|-------------------|
| Composite resource (read) | `.observed.composite.resource` | `option("params")?.oxr` | `oxr` global |
| Desired composite (write) | `.desired.composite.resource` | `option("params")?.dxr` | `dxr` global |
| Observed composed resources | `.observed.resources` | `option("params")?.ocds` | `observed` global |
| Create composed resource | Return in manifest list | `items = [resource]` | `Resource(name, body)` |
| Resource naming | Annotation-based | `krm.kcl.dev/composition-resource-name` | First arg to `Resource()` |
| Safe nested access | Nested `.` with `default` | `x?.field` | `get(obj, "path", default)` |
| Conditional resource | `{{- if }}` blocks | `if condition:` | `if condition:` + `Resource()` |
| Loop resources | `{{- range }}` | `for x in list:` | `for x in list:` + `Resource()` |
| XR status write | Direct `.desired` assignment | `dxr.status.field = val` | `set_xr_status("field", val)` |
| Connection details | Per-resource annotations | Per-resource annotations | `connection_details=` kwarg or `set_connection_details()` |
| Dependency ordering | Not built-in | Not built-in | `depends_on=` kwarg on `Resource()` |
| Labels | Manual | Manual via annotations | Auto-injected + `labels=` kwarg |
| Extra resources | ExtraResources spec field | ExtraResources spec field | `require_extra_resource()` + `get_extra_resource()` |
| Halt on error | Not built-in (`fail`) | `assert cond, msg` | `if not cond: fatal(msg)` |
| Events | Not built-in | Not built-in | `emit_event(severity, message)` |
| Module system | None (partials) | KCL import + OCI/Git | `load()` with OCI/ConfigMap/inline |
| Type validation | Untyped strings | Schema-based types | `schema()` + `field()` builtins |

## stdlib v1 to v2 migration

stdlib v2 replaces hand-rolled logic in the four stdlib modules with v1.8
namespace builtins (`crypto`, `regex`, `dict`, `get_label`). It requires
a function-starlark v1.8+ runtime and introduces one breaking change in
`naming.star`.

### Breaking changes

| Module | Change | v1 Behavior | v2 Behavior | Impact |
|--------|--------|-------------|-------------|--------|
| `naming.star` | `hash_suffix` algorithm | `hash()` + base-36 (0-9a-z) | `crypto.stable_id` SHA-256 hex (0-9a-f) | **Breaking**: Truncated resource names change, causing Crossplane to create new resources and orphan old ones |
| `naming.star` | `_sanitize` implementation | Character-by-character loop | `regex.replace_all` one-liner | Non-breaking: identical output |
| `labels.star` | `merge_labels` implementation | Manual for-loop merge | `dict.merge` builtin | Non-breaking: identical behavior |
| `labels.star` | `crossplane_labels` label lookup | `get(oxr, ["metadata", "labels", ...])` | `get_label(oxr, "key", "")` | Non-breaking: identical result |
| `networking.star` | IP/CIDR validation | Only validates after parsing | `regex.match` pre-validation + existing parsing | Non-breaking: catches malformed input earlier |
| `conditions.star` | No changes | Already uses `get_condition` | Same | No migration needed |

### Upgrade steps

1. Upgrade function-starlark runtime to v1.8+.
2. Change `load()` statements from `stdlib:v1/` to `stdlib:v2/` (or
   `starlark-stdlib:v2/`).
3. If using `hash_suffix` or `resource_name` with truncated names: expect
   resource name changes. Plan for resource recreation or adopt the new names.
4. Run compositions and verify no `"name not defined"` errors (confirms v1.8
   runtime).

### Runtime requirement

v2 stdlib calls `crypto.stable_id`, `regex.match`, `regex.replace_all`,
`dict.merge`, and `get_label` -- all predeclared in v1.8. Running v2 modules
on older runtimes produces a clear `"name 'crypto' not defined"` error at load
time.

## See also

- [Builtins reference](builtins-reference.md) -- complete function signatures
  for all builtins and namespace modules
- [Migration from KCL](migration-from-kcl.md) -- narrative migration guide with
  side-by-side code examples
- [Features guide](features.md) -- detailed coverage of namespace modules,
  dependency ordering, labels, and metrics
