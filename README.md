# function-starlark

> **In Development** -- This project is under active development and not yet
> published. APIs may change without notice. Do not use in production.

Write Crossplane compositions in Starlark -- a hermetic, Python-like language
with zero external toolchain.

## Why function-starlark

- **Familiar syntax** -- Starlark is a subset of Python. If you know Python, you
  already know Starlark. No new DSL to learn.
- **Lightweight runtime** -- ~20MB memory footprint with bytecode caching and
  sub-second execution. No external compiler or toolchain required.
- **Hermetic sandbox** -- No file I/O, no network access, no non-determinism.
  Scripts are safe by construction.
- **Built-in dependency ordering** -- `depends_on` creates Crossplane Usage
  resources automatically, ensuring resources are created and deleted in the
  right order.
- **Auto-injected labels** -- `crossplane.io/*` labels are applied to every
  resource automatically, with opt-out and merge controls via the `labels=`
  kwarg.
- **Opt-in schema validation** -- `schema()` and `field()` create typed constructors
  that catch typos, wrong types, and missing fields at construction time. Mix
  schema-validated and plain dict resources freely.
  [Builtins reference](docs/builtins-reference.md)
- **Namespace alias imports** -- `load("module.star", ns="*")` wraps all exports
  in a struct, solving name conflicts when loading multiple provider schema
  packages. [Module system](docs/module-system.md)

## Comparison

| Feature | function-starlark | function-kcl | function-go-templating |
|---------|-------------------|--------------|------------------------|
| Language | Starlark (Python-like) | KCL (custom DSL) | Go templates (Helm-like) |
| Source modes | Inline, ConfigMap, OCI | Inline, OCI, Git, FileSystem | Inline, FileSystem, Environment |
| Module system | load() with OCI/ConfigMap/inline | KCL import + OCI/Git | None (partials via template) |
| Memory footprint | ~20MB idle | 200MB+ baseline | ~20-40MB idle |
| Connection details | Per-resource + XR-level | Per-resource via annotations | Per-resource + XR-level |
| Dependency ordering | depends_on + creation sequencing | Not built-in | Not built-in |
| Auto labels | crossplane.io/* auto-injected | Manual via annotations | Manual via template |
| Conditions/Events | set_condition(), emit_event() | Via KCL annotations | Via custom conditions (1.17+) |
| Extra resources | require_extra_resource/require_extra_resources | ExtraResources spec | ExtraResources spec |
| Readiness control | ready= kwarg (None/True/False) | Annotation-based | Annotation-based |
| Type system | Opt-in schema validation + namespace alias imports | Schema-based with types | Untyped strings |
| Observability | 9 Prometheus metrics | None built-in | None built-in |
| Metadata builtins | get_label(), get_annotation(), get_observed(), set_xr_status() | Manual via get()/set | Manual via template |
| Sandbox | Hermetic (no I/O, no network) | KCL sandbox | Go template sandbox |
| Standard library | networking, naming, labels, conditions | KCL module ecosystem | Sprig functions |

## Quick start

This example creates an S3 bucket and an SNS topic that depends on it,
demonstrating core patterns: safe nested access with `get()`, conditional
resource creation, dependency ordering, and status conditions.

### Step 1: Write the composition

```yaml
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: xnotifications.example.crossplane.io
spec:
  compositeTypeRef:
    apiVersion: example.crossplane.io/v1
    kind: XNotification
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
            env = get(oxr, "spec.environment", "dev")
            xr_name = get(oxr, "metadata.name", "unknown")

            # Create a bucket
            bucket = Resource("bucket", {
                "apiVersion": "s3.aws.upbound.io/v1beta1",
                "kind": "Bucket",
                "metadata": {"name": "%s-data" % xr_name},
                "spec": {
                    "forProvider": {"region": region},
                },
            })

            # Create a notification topic that depends on the bucket
            Resource("topic", {
                "apiVersion": "sns.aws.upbound.io/v1beta1",
                "kind": "Topic",
                "metadata": {"name": "%s-notifications" % xr_name},
                "spec": {
                    "forProvider": {
                        "region": region,
                        "displayName": "Notifications for %s" % xr_name,
                    },
                },
            }, depends_on=[bucket])

            # Conditionally add monitoring in prod
            if env == "prod":
                Resource("dashboard", {
                    "apiVersion": "monitoring.example.io/v1",
                    "kind": "Dashboard",
                    "metadata": {"name": "%s-dashboard" % xr_name},
                    "spec": {"forProvider": {"region": region}},
                })

            set_condition(
                type="Ready",
                status="True",
                reason="Available",
                message="Resources created in %s" % region,
            )
```

### Step 2: Define the composite resource

```yaml
apiVersion: example.crossplane.io/v1
kind: XNotification
metadata:
  name: my-notifications
spec:
  region: us-east-1
  environment: prod
```

### Step 3: Render locally

```bash
crossplane render xr.yaml composition.yaml functions.yaml
```

The output includes the composed Bucket and Topic resources, a Usage resource
expressing the dependency from topic to bucket, a Dashboard (because
`environment` is `prod`), status conditions, and `crossplane.io/*` labels
auto-injected on every resource.

For a comprehensive 10-resource example exercising all builtins, see
[example/](example/).

## Installation

Install function-starlark into your Crossplane control plane:

```yaml
apiVersion: pkg.crossplane.io/v1beta1
kind: Function
metadata:
  name: function-starlark
spec:
  package: ghcr.io/wompipomp/function-starlark:latest
```

```bash
kubectl apply -f function.yaml
```

For detailed deployment options including Helm, OCI packages, private
registries, and ConfigMap-mounted scripts, see
[docs/deployment-guide.md](docs/deployment-guide.md).

## Configuration reference

All fields under `spec` in a `StarlarkInput` resource:

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `source` | string | -- | Inline Starlark script (required unless `scriptConfigRef` is set) |
| `scriptConfigRef.name` | string | -- | ConfigMap name containing the script |
| `scriptConfigRef.key` | string | `main.star` | Key within the ConfigMap |
| `modules` | map[string]string | -- | Inline modules loadable via `load("name.star", "func")` |
| `modulePaths` | []string | -- | Additional filesystem directories for module resolution |
| `dockerConfigSecret` | string | -- | Kubernetes Secret name for private OCI registry credentials |
| `dockerConfigCredential` | string | -- | gRPC credential name for private OCI registry auth (`crossplane render --function-credentials` or Composition `credentials` block) |
| `ociDefaultRegistry` | string | -- | Default OCI registry for short-form `load()` targets |
| `ociInsecureRegistries` | []string | -- | Registries to access over plain HTTP (development only) |
| `usageAPIVersion` | string | `v2` | Crossplane Usage API version -- `v1` (Crossplane 1.x) or `v2` (Crossplane 2.x) |
| `sequencingTTL` | duration | `10s` | Response TTL when creation sequencing defers resources |

### Crossplane 1.x compatibility

The `depends_on` feature generates Usage resources using the Crossplane 2.x API
(`protection.crossplane.io/v1beta1`) by default. If you are running **Crossplane 1.x**,
set the Usage API version to `v1` — either per-composition or cluster-wide:

```yaml
# Per-composition (in StarlarkInput spec)
usageAPIVersion: "v1"
```

```bash
# Or cluster-wide (env var on function pod)
STARLARK_USAGE_API_VERSION=v1
```

### Environment variables

Settings that apply cluster-wide can be set as environment variables on the function pod.
Per-composition `spec` fields take precedence over env vars.

| Env var | Default | Description |
|---------|---------|-------------|
| `STARLARK_OCI_DEFAULT_REGISTRY` | -- | Default OCI registry for short-form `load()` targets |
| `STARLARK_OCI_INSECURE_REGISTRIES` | -- | Comma-separated list of HTTP-only registries |
| `STARLARK_OCI_CACHE_TTL` | `5m` | TTL for OCI tag-to-digest resolution cache |
| `STARLARK_DOCKER_CONFIG_SECRET` | -- | Kubernetes Secret name for private OCI registry credentials |
| `STARLARK_USAGE_API_VERSION` | -- | Crossplane Usage API version override (`v1` or `v2`) |

Example with all fields:

```yaml
apiVersion: starlark.fn.crossplane.io/v1alpha1
kind: StarlarkInput
spec:
  source: |
    Resource("bucket", {"apiVersion": "s3.aws.upbound.io/v1beta1", "kind": "Bucket"})

  scriptConfigRef:
    name: my-scripts
    key: main.star

  modules:
    helpers.star: |
      def make_tags(env): return {"Environment": env}

  modulePaths:
    - /scripts/shared-lib

  dockerConfigSecret: registry-creds
  dockerConfigCredential: registry-creds
  ociDefaultRegistry: "ghcr.io/my-org"
  ociInsecureRegistries: ["localhost:5050"]
  usageAPIVersion: "v2"
  sequencingTTL: "10s"
```

## Documentation

| Guide | Description |
|-------|-------------|
| [docs/builtins-reference.md](docs/builtins-reference.md) | Complete API reference for all globals and functions |
| [docs/starlark-primer.md](docs/starlark-primer.md) | Starlark for Python developers |
| [docs/module-system.md](docs/module-system.md) | load(), OCI modules, stdlib, caching |
| [docs/features.md](docs/features.md) | depends_on, labels, connection details, skip_resource, metrics |
| [docs/best-practices.md](docs/best-practices.md) | Composition patterns, label strategy, testing |
| [docs/migration-from-kcl.md](docs/migration-from-kcl.md) | Migration guide from function-kcl |
| [docs/stdlib-reference.md](docs/stdlib-reference.md) | Standard library reference |
| [docs/oci-module-distribution.md](docs/oci-module-distribution.md) | OCI module distribution guide |
| [docs/library-authoring.md](docs/library-authoring.md) | Writing shared Starlark libraries |
| [docs/deployment-guide.md](docs/deployment-guide.md) | Deployment and operations guide |
| [llms.txt](llms.txt) | Machine-readable project reference for LLMs |

## Ecosystem

| Project | Description |
|---------|-------------|
| [function-starlark-gen](https://github.com/wompipomp/function-starlark-gen) | Go CLI that generates typed Starlark schema libraries from OpenAPI specs, Kubernetes CRDs, and Crossplane provider CRDs. Three modes: `starlark-gen k8s`, `starlark-gen crd`, `starlark-gen provider`. |
| [function-starlark-schemas](https://github.com/wompipomp/function-starlark-schemas) | Pre-built schema packages generated by function-starlark-gen. OCI-distributed at `ghcr.io/wompipomp/schemas-*`. |
| [function-starlark-vscode](https://github.com/wompipomp/function-starlark-vscode) | VS Code extension with autocomplete, hover docs, signature help, syntax highlighting, format-on-save, and schema IntelliSense with OCI-based auto-download. |

## Contributing

Contributions welcome. Please open an issue to discuss before submitting a PR.

## License

Apache 2.0
