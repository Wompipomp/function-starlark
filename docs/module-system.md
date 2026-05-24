# Module system

function-starlark supports multiple ways to organize and share Starlark code.
This guide covers all source modes, module loading patterns, and caching
behavior.

## Source modes

There are three ways to provide Starlark source code to function-starlark.

### Inline source

The simplest approach. Embed the script directly in the Composition YAML via
`spec.source`. Best for small compositions (< 100 lines) and quick prototyping.

```yaml
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
spec:
  mode: Pipeline
  pipeline:
    - step: run-starlark
      functionRef:
        name: function-starlark
      input:
        apiVersion: starlark.fn.crossplane.io/v1alpha1
        kind: StarlarkInput
        spec:
          source: |
            region = get(oxr, "spec.region", "us-east-1")
            Resource("bucket", {
                "apiVersion": "s3.aws.upbound.io/v1beta1",
                "kind": "Bucket",
                "spec": {"forProvider": {"region": region}},
            })
```

### ConfigMap reference

Store the script in a Kubernetes ConfigMap and reference it by name and key via
`spec.scriptConfigRef`. Best for sharing scripts across compositions, separating
code from YAML, and GitOps workflows where scripts are managed separately.

```yaml
# ConfigMap holding the script
apiVersion: v1
kind: ConfigMap
metadata:
  name: my-scripts
  namespace: crossplane-system
data:
  main.star: |
    region = get(oxr, "spec.region", "us-east-1")
    Resource("bucket", {
        "apiVersion": "s3.aws.upbound.io/v1beta1",
        "kind": "Bucket",
        "spec": {"forProvider": {"region": region}},
    })
```

```yaml
# StarlarkInput referencing the ConfigMap
apiVersion: starlark.fn.crossplane.io/v1alpha1
kind: StarlarkInput
spec:
  scriptConfigRef:
    name: my-scripts
    key: main.star       # default key if omitted
```

The ConfigMap must be mounted into the function pod via a
DeploymentRuntimeConfig. See the [deployment guide](deployment-guide.md) for
mount configuration.

### OCI modules (short-form)

Starlark modules packaged and distributed as OCI artifacts. Best for shared
libraries across teams and clusters, and versioned module distribution.

When a [default OCI registry](#configuring-the-default-oci-registry) is
configured, use the concise short-form syntax:

```python
load("function-starlark-stdlib:v1/naming.star", "resource_name")
```

The short-form `package:tag/file.star` is expanded using the configured default
registry. For example, with registry `ghcr.io/wompipomp`, the load above
becomes `oci://ghcr.io/wompipomp/function-starlark-stdlib:v1/naming.star`.

### OCI modules (explicit full URL)

For cases where you need to specify the full registry path, or when no default
registry is configured, use the explicit `oci://` form:

```python
load("oci://ghcr.io/my-org/starlark-lib:v1/helpers.star", "create_bucket")
```

OCI modules are resolved before any Starlark code runs, preserving sandbox
hermeticity. For the full guide on registry setup, pushing, versioning, and
authentication, see the [OCI module distribution guide](oci-module-distribution.md).

## Configuring the Default OCI Registry

The default OCI registry tells function-starlark where to find short-form
module references. There are two configuration methods, and the spec field
takes precedence over the environment variable.

### Configuration methods

**1. Environment variable (operator-level)**

Set `STARLARK_OCI_DEFAULT_REGISTRY` on the function pod via a
DeploymentRuntimeConfig. This applies to all compositions using this function
instance:

```yaml
apiVersion: pkg.crossplane.io/v1beta1
kind: DeploymentRuntimeConfig
metadata:
  name: function-starlark
spec:
  deploymentTemplate:
    spec:
      template:
        spec:
          containers:
            - name: package-runtime
              env:
                - name: STARLARK_OCI_DEFAULT_REGISTRY
                  value: "ghcr.io/my-org"
```

**2. Spec field (per-composition override)**

Set `spec.ociDefaultRegistry` in the StarlarkInput to override the
environment variable for a specific composition:

```yaml
apiVersion: starlark.fn.crossplane.io/v1alpha1
kind: StarlarkInput
spec:
  ociDefaultRegistry: "ghcr.io/my-org"
  source: |
    load("my-starlark-lib:v1/helpers.star", "create_bucket")
    Resource("bucket", create_bucket("us-east-1"))
```

### Precedence

| Priority | Source | Scope |
|----------|--------|-------|
| 1 (highest) | `spec.ociDefaultRegistry` | Per-composition |
| 2 | `STARLARK_OCI_DEFAULT_REGISTRY` env var | All compositions on this function pod |

If `spec.ociDefaultRegistry` is set (non-empty), it wins. Otherwise the
environment variable is used. If neither is configured and a short-form load
target is encountered, the function returns a fatal error:

```
load target "function-starlark-stdlib:v1/naming.star" requires a default OCI registry;
set STARLARK_OCI_DEFAULT_REGISTRY env var on the function pod or spec.ociDefaultRegistry in function input
```

### Editor configuration

If you use the [function-starlark VS Code extension](https://github.com/wompipomp/function-starlark-vscode),
short-form load targets also need a default registry for schema IntelliSense
(autocomplete, type checking, diagnostics). Set it in VS Code settings:

```json
{
  "functionStarlark.schemas.registry": "ghcr.io/my-org"
}
```

The default is `ghcr.io/wompipomp`. If your runtime uses a different registry,
make sure the VS Code setting matches so that editor IntelliSense resolves the
same schemas as your deployed compositions.

### Detection rules

function-starlark determines the load type using these rules, applied in order:

1. **Starts with `../`** -- rejected with a path-traversal error
   (`path must not escape the caller's directory`)
2. **Starts with `./`** -- relative load, dispatched by caller type:
   - **Filesystem caller:** resolves relative to the caller's directory
     (subdirectories supported, e.g., `./lib/utils.star`)
   - **OCI caller:** package-local, resolves within the same OCI artifact
     (flat paths only -- no subdirectories)
   - **Inline caller:** error (`relative load requires a filesystem-sourced
     module; inline modules have no directory context`)
3. **Starts with `oci://`** -- explicit full OCI URL (no expansion, used as-is)
4. **Contains `:` or `@sha256:`** -- short-form OCI reference, expanded via
   the default registry
5. **Otherwise** -- local module (inline map lookup by key, then filesystem
   search paths)

### Short-form patterns

```python
# Tag reference
load("my-lib:v1/helpers.star", "create_bucket")

# Nested file path
load("my-lib:v1/subdir/utils.star", "validate")

# Digest pinning (deterministic, skips tag resolution)
load("my-lib@sha256:abc123.../helpers.star", "create_bucket")
```

### How expansion works

Given a default registry of `ghcr.io/my-org`:

| Short-form | Expands to |
|-----------|------------|
| `my-lib:v1/helpers.star` | `oci://ghcr.io/my-org/my-lib:v1/helpers.star` |
| `my-lib:v1/sub/utils.star` | `oci://ghcr.io/my-org/my-lib:v1/sub/utils.star` |
| `my-lib@sha256:abc.../h.star` | `oci://ghcr.io/my-org/my-lib@sha256:abc.../h.star` |

The registry value format is `host/namespace` (e.g., `ghcr.io/my-org`). Do not
include `oci://` in the registry value -- it is stripped silently if present.

## The load() statement

The `load()` statement imports names from other modules:

```python
load("source", "name1", "name2")
```

- First argument is the module source (string).
- Remaining arguments are names to import.
- Aliased imports are supported: `load("module.star", renamed = "original")`
- Star imports bring in all public exports: `load("module.star", "*")`

### Relative loads

Modules can load siblings and subdirectory modules using the `./` prefix:

```python
load("./helpers.star", "my_func")           # sibling file
load("./lib/utils.star", "validate")        # subdirectory file
```

The `./` prefix triggers relative resolution from the caller's directory.
Subdirectories are fully supported for filesystem callers. OCI callers are
restricted to flat sibling paths (no subdirectories).

**Path traversal is rejected.** Loading with `../` produces a clear error:

```
relative load "../secrets.star" rejected: path must not escape the caller's directory
```

**Inline modules cannot use relative loads** because they have no directory
context. Attempting a relative load from an inline module produces:

```
relative load "./helpers.star" requires a filesystem-sourced module;
inline modules have no directory context. Use a flat module name or move to a ConfigMap.
```

**Test files cannot be loaded.** Any `_test.star` file -- whether loaded by
flat name or relative path -- is rejected at runtime. This prevents test code
from leaking into production modules. See the
[testing guide](testing.md) for how `_test.star` files are used.

### Namespace alias imports

Namespace alias imports wrap all public exports in a `struct` bound to a name
you choose. Use the syntax `load("module.star", alias="*")` where `alias` is
any valid Starlark identifier:

```python
load("helpers.star", h="*")
h.my_function()
h.MY_CONSTANT
```

Access all exports via dot notation on the alias struct.

**Solving name conflicts across provider packages.** When two modules export the
same name (e.g., Azure `storage` and `cosmosdb` both export `Account`), flat
star imports clash. Namespace aliases solve this by keeping each module's exports
in a separate struct:

```python
load("oci://ghcr.io/wompipomp/schemas-k8s:v1.35/apps/v1.star", k8s="*")
load("oci://ghcr.io/wompipomp/schemas-azure:v2.5.0/storage/v1.star", storage="*")
load("oci://ghcr.io/wompipomp/schemas-azure:v2.5.0/cosmosdb/v1.star", cosmosdb="*")

k8s.Deployment(...)
storage.Account(...)    # no conflict with cosmosdb.Account
cosmosdb.Account(...)
```

**Backwards compatibility.** Plain star imports still work exactly as before:

```python
load("module.star", "*")  # flat import -- all exports available at top level
```

**Mixed syntax.** You can combine named imports with a namespace alias in the
same `load()` call. Named imports are available flat, while the namespace struct
contains everything:

```python
load("module.star", "SpecificFunc", ns="*")
SpecificFunc()   # available flat
ns.OtherFunc()   # available via namespace
```

### Module resolution order

When `load()` is called, function-starlark resolves the module source in this
order:

1. **Relative `./` path** -- resolves relative to the caller's directory (filesystem or OCI)
2. **Inline modules** (`spec.modules`) -- keyed by filename (supports path-based keys)
3. **Module paths** (`spec.modulePaths`) -- filesystem directories
4. **Short-form OCI modules** (`package:tag/file.star`) -- expanded via default registry
5. **Explicit OCI modules** (`oci://` prefix) -- full OCI URL

```python
# 1. Relative load (resolves from caller's directory)
load("./helpers.star", "my_func")

# 2. Inline module (defined in spec.modules)
load("helpers.star", "my_func")

# 3. Short-form OCI module (requires default registry configured)
load("function-starlark-stdlib:v1/naming.star", "resource_name")

# 4. Explicit OCI module (full URL, no default registry needed)
load("oci://ghcr.io/myorg/starlark-libs/networking:v1.0.0/helpers.star", "subnet_cidr")

# 5. Standard library (using short-form with default registry)
load("function-starlark-stdlib:v1/networking.star", "subnet_cidr")
load("function-starlark-stdlib:v1/naming.star", "resource_name")
load("function-starlark-stdlib:v1/labels.star", "standard_labels")
load("function-starlark-stdlib:v1/conditions.star", "degraded")
```

## Inline modules

Define reusable modules directly in the StarlarkInput via `spec.modules`. Each
entry maps a filename to a Starlark script:

```yaml
apiVersion: starlark.fn.crossplane.io/v1alpha1
kind: StarlarkInput
spec:
  modules:
    helpers.star: |
      def make_bucket(name, region):
          return {
              "apiVersion": "s3.aws.upbound.io/v1beta1",
              "kind": "Bucket",
              "metadata": {"name": name},
              "spec": {"forProvider": {"region": region}},
          }
  source: |
    load("helpers.star", "make_bucket")

    region = get(oxr, "spec.region", "us-east-1")
    Resource("bucket", make_bucket("my-bucket", region))
```

Inline modules can load other inline modules. Circular load dependencies are
detected and produce a clear error.

**Path-based keys.** When modules are organized in subdirectories and bundled
into `spec.modules` (e.g., by a CLI tool), the module map uses path-based keys:

```yaml
spec:
  modules:
    helpers.star: |
      def make_tags(env): ...
    lib/utils.star: |
      def validate(x): ...
```

```python
load("helpers.star", "make_tags")
load("lib/utils.star", "validate")
```

Path-based keys bypass the usual name validation, so keys like `lib/utils.star`
work as-is. The loader matches the key directly against the inline module map.

## Standard library

function-starlark ships a standard library published as an OCI artifact at
`ghcr.io/wompipomp/starlark-stdlib`. It provides four modules covering common
composition patterns:

| Module | Functions | Purpose |
|--------|-----------|---------|
| `networking.star` | `ip_to_int`, `int_to_ip`, `network_address`, `broadcast_address`, `subnet_cidr`, `cidr_contains` | CIDR math and IP utilities |
| `naming.star` | `resource_name` | Kubernetes-safe naming with 63-char limit enforcement |
| `labels.star` | `standard_labels`, `crossplane_labels`, `merge_labels` | Kubernetes and Crossplane label generation |
| `conditions.star` | `degraded` | Operational status signaling |

```python
# Short-form (recommended, requires default registry configured)
load("starlark-stdlib:v1/networking.star", "subnet_cidr")

# Explicit full URL (always works, no default registry needed)
load("oci://ghcr.io/wompipomp/starlark-stdlib:v1/networking.star", "subnet_cidr")

subnet = subnet_cidr("10.0.0.0/16", 8, 1)  # "10.0.1.0/24"
```

For full function signatures and documentation, see the
[standard library reference](stdlib-reference.md).

## Caching

function-starlark uses two caching layers to minimize overhead across
reconciliation cycles.

### Bytecode caching

Compiled Starlark bytecode is cached in-memory. When the same script is
executed again (e.g., on the next reconciliation), the cached bytecode is
reused, skipping the parse and compile steps. Cache performance is tracked via
Prometheus metrics:

- `function_starlark_cache_hits_total` -- bytecode cache hits
- `function_starlark_cache_misses_total` -- bytecode cache misses

### OCI tag resolution caching

OCI tag-to-digest mappings are cached in-memory on the function pod. The
resolver honors a Kubernetes-style pull policy set per pod via
`STARLARK_OCI_PULL_POLICY`:

- **`IfNotPresent` (default)** — pull on first reference, then reuse the
  cached copy for the pod's lifetime. No revalidation, zero steady-state
  registry traffic. Treat tags as immutable; restart the pod (or pin a new
  tag/digest) to pick up a retag.
- **`Always`** — revalidate with a manifest `HEAD` on cache miss or after
  `STARLARK_OCI_CACHE_TTL` elapses (default `0` means every reconciliation).
  Unchanged digest reuses cached content; a changed digest triggers a new
  pull. Use this when you intentionally push updates to moving tags and want
  in-place refresh without a pod restart.

Digest-pinned references (e.g., `@sha256:abc123...`) bypass the tag cache
entirely and are cached permanently regardless of policy, since a digest is
immutable. Recommended for production compositions.

OCI resolution time is tracked via the
`function_starlark_oci_resolve_duration_seconds` histogram.

## Private registries

For private OCI registries, set `spec.dockerConfigSecret` to the name of a
Kubernetes Secret containing Docker registry credentials:

```yaml
apiVersion: starlark.fn.crossplane.io/v1alpha1
kind: StarlarkInput
spec:
  dockerConfigSecret: my-registry-creds
  source: |
    load("oci://myregistry.azurecr.io/modules/helpers:v1/helpers.star", "create_bucket")
```

The secret must be mounted into the function pod via a
DeploymentRuntimeConfig. For complete authentication setup (ACR, ECR, GHCR),
see the [OCI module distribution guide](oci-module-distribution.md#authentication).

For local development with `crossplane render`, use `spec.dockerConfigCredential`
instead — it passes Docker credentials via gRPC since `crossplane render` cannot
mount volumes into function containers. Both fields can coexist in the same
Composition (gRPC credential is tried first, filesystem secret second). See the
[OCI module distribution guide](oci-module-distribution.md#local-development-with-crossplane-render)
for setup instructions including Azure ACR token generation.

## See also

- [OCI Module Distribution](oci-module-distribution.md) -- full guide for
  publishing, loading, and authenticating OCI modules
- [Library Authoring Guide](library-authoring.md) -- conventions for writing
  shared Starlark libraries
- [Standard Library Reference](stdlib-reference.md) -- complete API
  documentation for the built-in standard library
