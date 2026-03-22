# Library Authoring Guide

Create and publish reusable Starlark modules for function-starlark compositions.
This guide covers naming, exports, documentation, versioning, and publishing
conventions. The [standard library](stdlib-reference.md) follows all of these
conventions and serves as a reference implementation.

## Module naming

Use domain-based flat files with the `.star` extension. Each file covers one
domain of functionality. Names should be short, lowercase, and descriptive.

```
networking.star   # CIDR math and IP utilities
naming.star       # Kubernetes-safe resource naming
labels.star       # Label generation helpers
conditions.star   # Status condition wrappers
```

Rules:

- One `.star` file per domain -- no directory nesting inside OCI bundles.
- Name after the problem domain, not the implementation (`networking.star` not
  `cidr_math.star`).
- Avoid prefixes like `lib_` or `stdlib_` -- the OCI registry path provides
  namespace context.

## Export conventions

All public API surface is functions defined with `def`. Constants use
`ALL_CAPS`. Private helpers start with `_`.

```python
# Public constant -- exported
MAX_NAME_LENGTH = 63

# Private helper -- NOT exported (starts with _)
def _validate_input(value):
    if not value:
        fail("value must not be empty")

# Public function -- exported
def resource_name(suffix, xr_name=None):
    """Generate a Kubernetes-safe resource name."""
    _validate_input(suffix)
    # ...
```

Rules:

- Functions are the primary export mechanism. Do not export raw dicts or lists
  that consumers might try to mutate (Starlark freezes module globals).
- Constants via `ALL_CAPS` are fine since strings and numbers are immutable.
- All names starting with `_` are private and excluded from star imports
  (`load("module.star", "*")`).
- No module-level side effects. Never call `Resource()`, `set_condition()`, or
  `emit_event()` at the top level of a library module. These must only appear
  inside function bodies.

When your module exports names that are common across provider packages (e.g.,
`Account`, `Network`), consumers can use namespace alias imports to avoid
conflicts: `load("my-module.star", ns="*")`. This wraps all exports in a struct
bound to `ns`. Design your module's public API knowing that consumers may access
it via dot notation (`ns.Account`) as well as flat imports.

## Docstring format

Every exported function requires a docstring. Use this format:

```python
def subnet_cidr(base_cidr, new_bits, subnet_num):
    """Calculate a subnet CIDR from a base CIDR.

    Equivalent to Terraform's cidrsubnet() function. Divides the base
    network into smaller subnets by adding new_bits to the prefix length.

    Args:
      base_cidr: Base CIDR string (e.g., "10.0.0.0/16")
      new_bits: Number of additional prefix bits (e.g., 8 for /24 from /16)
      subnet_num: Subnet index number (0-based)

    Returns:
      Subnet CIDR string (e.g., "10.0.0.0/24")
    """
```

Structure:

1. **One-line summary** -- first line of the docstring, imperative mood.
2. **Extended description** (optional) -- additional context after a blank line.
3. **Args** -- one line per parameter with name, colon, description.
4. **Returns** -- description of the return value.

Private helpers (`_` prefix) do not require docstrings but they are recommended.

## Predeclared builtins

Library modules loaded via `load()` receive the same predeclared builtins as
the main script. These are available inside function bodies:

| Builtin | Purpose | Module-level safe? |
|---------|---------|-------------------|
| `Resource(name, body, ...)` | Emit a desired composed resource | No -- call inside functions only |
| `get(obj, path, default)` | Safe nested dict access | Yes (pure function) |
| `oxr` | Observed composite resource dict | No -- value changes per reconciliation |
| `dxr` | Desired composite resource dict | No -- value changes per reconciliation |
| `observed` | All observed composed resources | No -- value changes per reconciliation |
| `desired` | All desired composed resources | No -- value changes per reconciliation |
| `set_condition(...)` | Set XR status condition | No -- side effect |
| `emit_event(...)` | Emit Kubernetes event | No -- side effect |

**Critical rule:** Never access `oxr`, `dxr`, `observed`, or `desired` at
module top level. Module globals are frozen after first load and cached. If you
read `oxr` at module level, you get the value from the first reconciliation and
it never updates. Always read these inside function bodies.

```python
# BAD -- oxr read at module level (stale after first load)
_region = get(oxr, "spec.region", "us-east-1")

# GOOD -- oxr read inside function body (fresh every call)
def get_region():
    return get(oxr, "spec.region", "us-east-1")
```

## Versioning

Use semantic version OCI tags with major-version aliases:

```
:v1.0.0    # Specific release
:v1.0.1    # Patch fix
:v1.1.0    # New feature, backward compatible
:v2.0.0    # Breaking change
:v1        # Major version alias -- always points to latest v1.x.x
```

Version rules:

- **Patch** (v1.0.x): Bug fixes, documentation improvements.
- **Minor** (v1.x.0): New functions added, new optional parameters.
- **Major** (vX.0.0): Removed functions, changed return types, renamed
  parameters, changed default behavior.
- **Alias** (:vN): The major version alias lets consumers write
  `load("oci://registry/repo:v1/module.star", ...)` and get compatible updates
  without changing their composition.

## Publishing workflow

### Prerequisites

Install the [oras CLI](https://oras.land/):

```bash
# macOS
brew install oras

# Linux
curl -LO https://github.com/oras-project/oras/releases/download/v1.2.2/oras_1.2.2_linux_amd64.tar.gz
tar xzf oras_1.2.2_linux_amd64.tar.gz
sudo mv oras /usr/local/bin/
```

### Push to a registry

```bash
# Login (GHCR example)
echo "$GITHUB_TOKEN" | oras login ghcr.io -u USERNAME --password-stdin

# Push your module(s)
oras push ghcr.io/my-org/my-starlark-lib:v1.0.0 \
  --artifact-type application/vnd.fn-starlark.modules.v1+tar \
  networking.star helpers.star

# Tag major version alias
oras tag ghcr.io/my-org/my-starlark-lib:v1.0.0 v1
```

The `--artifact-type` flag is required. function-starlark validates this media
type on pull and rejects artifacts that do not match.

### Bundle layout

The OCI artifact must contain `.star` files at the root. No directories, no
nested paths. Safety limits enforced on extraction:

- Files must end in `.star`
- Maximum 100 files per bundle
- Maximum 1 MB per file
- No path traversal (`..`, absolute paths)

### Local development

For testing against a local registry:

```bash
# Start a local OCI registry
docker run -d -p 5000:5000 registry:2

# Push to local registry
oras push localhost:5000/my-lib:dev \
  --artifact-type application/vnd.fn-starlark.modules.v1+tar \
  helpers.star

# Use in compositions
load("oci://localhost:5000/my-lib:dev/helpers.star", "my_func")
```

### CI publishing

See the function-starlark stdlib workflow (`.github/workflows/stdlib-publish.yaml`)
for a complete GitHub Actions example that publishes on git tag push.

## Common pitfalls

### Dict immutability after load

Starlark freezes all module globals after `load()` completes. Any dict or list
defined at module level becomes immutable. Library functions must create and
return new dicts, never modify module-level data.

```python
# BAD -- modifying a module-level dict
_defaults = {"region": "us-east-1"}

def set_region(r):
    _defaults["region"] = r  # FAILS: "cannot insert into frozen dict"

# GOOD -- returning a new dict
def get_defaults(region="us-east-1"):
    return {"region": region}
```

### No dict.update() or spread syntax

Starlark does not have `dict.update()` or `{**a, **b}` spread syntax. Use
explicit loop-based merging:

```python
def merge(base, overrides):
    result = {}
    for k in base:
        result[k] = base[k]
    for k in overrides:
        result[k] = overrides[k]
    return result
```

### Module-level oxr access is stale

Module globals are cached after first load. If you read `oxr` at module level,
the value is from the first reconciliation and never updates. Always read
`oxr`, `dxr`, `observed`, and `desired` inside function bodies.

### Bitwise NOT produces negative numbers

Starlark integers are arbitrary precision. `~0xFF` produces `-256`, not
`0xFFFFFF00`. Always mask with `& 0xFFFFFFFF` for 32-bit unsigned behavior:

```python
mask = ~((1 << (32 - prefix)) - 1) & 0xFFFFFFFF
```

### hash() returns signed integers

The `hash()` built-in can return negative integers. Convert to positive before
using for name generation:

```python
h = hash(name)
if h < 0:
    h = -h
```

## Complete example

A minimal library module following all conventions:

```python
"""Tagging helpers for AWS resources.

Generates consistent tags for AWS resources with org defaults
and Crossplane metadata.
"""

# Public constants
DEFAULT_ENVIRONMENT = "production"

# Private helpers
def _org_tags():
    return {"ManagedBy": "crossplane", "Team": "platform"}

# Public API
def resource_tags(name, environment=DEFAULT_ENVIRONMENT, extra={}):
    """Generate standard AWS resource tags.

    Args:
      name: Resource name for the Name tag
      environment: Environment tag value (default: "production")
      extra: Additional tags to merge (later keys override)

    Returns:
      Dict of tag key-value pairs
    """
    tags = _org_tags()
    tags["Name"] = name
    tags["Environment"] = environment
    for k in extra:
        tags[k] = extra[k]
    return tags
```

Publish it:

```bash
oras push ghcr.io/my-org/aws-tags:v1.0.0 \
  --artifact-type application/vnd.fn-starlark.modules.v1+tar \
  tagging.star
oras tag ghcr.io/my-org/aws-tags:v1.0.0 v1
```

Use it in a composition:

```python
load("oci://ghcr.io/my-org/aws-tags:v1/tagging.star", "resource_tags")

tags = resource_tags("my-bucket", extra={"Project": "data-lake"})

Resource("bucket", {
    "apiVersion": "s3.aws.upbound.io/v1beta1",
    "kind": "Bucket",
    "metadata": {"name": "my-bucket"},
    "spec": {"forProvider": {"region": "us-east-1", "tags": tags}},
})
```

## Reference

- [Standard Library Reference](stdlib-reference.md) -- API docs for the
  built-in standard library (networking, naming, labels, conditions)
- [OCI Module Distribution](oci-module-distribution.md) -- Loading modules from
  OCI registries, authentication, caching
