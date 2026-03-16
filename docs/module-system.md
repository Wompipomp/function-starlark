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

### OCI modules

Starlark modules packaged and distributed as OCI artifacts. Best for shared
libraries across teams and clusters, and versioned module distribution.

```python
load("oci://ghcr.io/my-org/starlark-lib:v1/helpers.star", "create_bucket")
```

OCI modules are resolved before any Starlark code runs, preserving sandbox
hermeticity. For the full guide on registry setup, pushing, versioning, and
authentication, see the [OCI module distribution guide](oci-module-distribution.md).

## The load() statement

The `load()` statement imports names from other modules:

```python
load("source", "name1", "name2")
```

- First argument is the module source (string).
- Remaining arguments are names to import.
- Aliased imports are supported: `load("module.star", renamed = "original")`
- Star imports bring in all public exports: `load("module.star", "*")`

### Module resolution order

When `load()` is called, function-starlark resolves the module source in this
order:

1. **Inline modules** (`spec.modules`) -- keyed by filename
2. **Module paths** (`spec.modulePaths`) -- filesystem directories
3. **OCI modules** (`oci://` prefix) -- remote OCI registries

```python
# 1. Inline module (defined in spec.modules)
load("helpers.star", "my_func")

# 2. OCI module (explicit oci:// prefix)
load("oci://ghcr.io/myorg/starlark-libs/networking:v1.0.0/helpers.star", "subnet_cidr")

# 3. Standard library (published as OCI artifact)
load("oci://ghcr.io/wompipomp/starlark-stdlib:v1/networking.star", "subnet_cidr")
load("oci://ghcr.io/wompipomp/starlark-stdlib:v1/naming.star", "resource_name")
load("oci://ghcr.io/wompipomp/starlark-stdlib:v1/labels.star", "standard_labels")
load("oci://ghcr.io/wompipomp/starlark-stdlib:v1/conditions.star", "degraded")
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

OCI tag-to-digest mappings are cached with a configurable TTL (default 5
minutes). This avoids re-resolving tags on every reconciliation:

```yaml
apiVersion: starlark.fn.crossplane.io/v1alpha1
kind: StarlarkInput
spec:
  ociCacheTTL: "10m"   # default: 5m
  source: |
    load("oci://ghcr.io/my-org/lib:v1/helpers.star", "create_bucket")
```

Digest-pinned references (e.g., `@sha256:abc123...`) bypass the tag cache
entirely and are cached permanently, since a digest is immutable. Use tags for
development (automatic refresh on TTL expiry) and digest-pinned references for
production (maximum determinism).

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

## See also

- [OCI Module Distribution](oci-module-distribution.md) -- full guide for
  publishing, loading, and authenticating OCI modules
- [Library Authoring Guide](library-authoring.md) -- conventions for writing
  shared Starlark libraries
- [Standard Library Reference](stdlib-reference.md) -- complete API
  documentation for the built-in standard library
