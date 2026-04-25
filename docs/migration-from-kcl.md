# Migrating from function-kcl to function-starlark

This guide helps platform engineers migrate Crossplane compositions from
[function-kcl](https://github.com/crossplane-contrib/function-kcl) to
function-starlark. It covers concept mapping, common patterns, side-by-side
examples, and a step-by-step migration process.

## Why migrate?

function-starlark offers several advantages for composition authoring:

- **Familiar Python-like syntax** -- Starlark uses Python syntax that most
  engineers already know. No need to learn KCL's type system or schema language.
- **Lightweight runtime** -- Starlark compiles to bytecode with sub-second
  execution on cached programs. No external toolchain required. In benchmarks,
  function-starlark is 4.8x faster than function-kcl at 10 resources and 7.4x
  faster at 50 resources (see [benchmarks](benchmarks.md)).
- **Simple mental model** -- Imperative scripting with explicit `Resource()`
  calls instead of assembling an `items` list. What you write is what you get.
- **Deterministic execution** -- Starlark is hermetic by design. No file I/O,
  no network access, no non-determinism.

## Concept mapping

| KCL | Starlark | Notes |
|-----|----------|-------|
| `option("params").oxr` | `oxr` | Predeclared global, frozen (read-only) |
| `option("params").dxr` | `dxr` | Predeclared global, mutable |
| `option("params").ocds` | `observed` | Predeclared global, frozen dict of frozen dicts |
| `oxr.spec?.region or "default"` | `get(oxr, "spec.region", "default")` | Safe nested access with default |
| `items = [...]` | `Resource(name, body)` | Each call registers one desired resource |
| KCL schema / type annotations | Plain dicts | Starlark uses untyped dicts for resource bodies |
| `_resources = [...] if cond else []` | `if cond: Resource(...)` | Conditional resource creation |
| `[expr for x in range(n)]` | `for x in range(n): Resource(...)` | Loop-based resource creation |
| `import module` | Not available | Starlark scripts are self-contained |
| `lambda x: expr` | `lambda x: expr` | Both support lambdas |
| `krm.kcl.dev/composition-resource-name` annotation | First arg to `Resource()` | Name is explicit, not an annotation |
| `oxr.metadata?.labels?["app.kubernetes.io/name"] or ""` | `get_label(oxr, "app.kubernetes.io/name", "")` | Safe dotted-key label access |
| `oxr.metadata?.annotations?["key"] or ""` | `get_annotation(oxr, "key", "")` | Safe annotation access |
| `dxr = {**oxr, status.field = val}` | `set_xr_status("field", val)` | Dot-path status writes with auto-created intermediates |
| `ocds["name"]?.spec?.field or ""` | `get_observed("name", "spec.field", "")` | One-call observed access |

## Global variables

function-starlark provides these predeclared globals:

| Global | Type | Mutable | Description |
|--------|------|---------|-------------|
| `oxr` | StarlarkDict | No (frozen) | Observed composite resource |
| `dxr` | StarlarkDict | Yes | Desired composite resource |
| `observed` | StarlarkDict | No (frozen) | Observed composed resources by name |
| `context` | dict | Yes | Pipeline context (read/write) |
| `environment` | StarlarkDict | No (frozen) | EnvironmentConfig data |
| `extra_resources` | dict | No (frozen) | Extra/required resources |

## Builtin functions

| Function | Signature | Description |
|----------|-----------|-------------|
| `Resource` | `Resource(name, body, ready=None, labels=None_or_dict, connection_details=None, depends_on=None, external_name=None, when=True, skip_reason="", preserve_observed=False, optional=False)` | Register a desired composed resource; returns ResourceRef. `depends_on` accepts ResourceRef, string, or `(ref, "field.path")` tuple. `when=False` auto-gates the XR to Ready=False (opt-out with `optional=True`). |
| `skip_resource` | `skip_resource(name, reason)` | Remove a resource from desired state with a reason. Pure observability -- does NOT gate the XR. For new code prefer `Resource(when=False, skip_reason=...)` which also handles composite-readiness gating. |
| `get` | `get(obj, path, default=None)` | Safe nested dict access with dot-path or list-of-keys |
| `set_condition` | `set_condition(type, status, reason, message, target="Composite")` | Set an XR condition |
| `emit_event` | `emit_event(severity, message, target="Composite")` | Emit a Normal or Warning event |
| `fatal` | `fatal(message)` | Halt execution with a fatal error |
| `set_composite_ready` | `set_composite_ready(ready, reason="", message="")` | Explicitly set the XR's Ready state (takes precedence over auto-gating). KCL has no direct equivalent -- gate the composite via observed state. |
| `set_connection_details` | `set_connection_details(dict)` | Set XR-level connection details |
| `require_extra_resource` | `require_extra_resource(name, apiVersion, kind, match_name=None, match_labels=None)` | Request a single extra resource |
| `require_extra_resources` | `require_extra_resources(name, apiVersion, kind, match_labels)` | Request multiple extra resources by label selector |
| `get_label` | `get_label(res, key, default=None)` | Safe label lookup handling dotted keys |
| `get_annotation` | `get_annotation(res, key, default=None)` | Safe annotation lookup handling dotted keys |
| `set_xr_status` | `set_xr_status(path, value)` | Dot-path XR status writes with auto-created intermediates |
| `get_observed` | `get_observed(name, path, default=None)` | One-call observed resource field lookup |

## Common patterns

### Safe nested access

**KCL:**
```python
region = oxr.spec?.region or "us-east-1"
name = oxr.metadata?.name or "unknown"

# Deep access
zone = oxr.spec?.parameters?.networking?.zone or "default"
```

**Starlark:**
```python
region = get(oxr, "spec.region", "us-east-1")
name = get(oxr, "metadata.name", "unknown")

# Deep access
zone = get(oxr, "spec.parameters.networking.zone", "default")

# Keys with dots (e.g., annotation keys) use list-of-keys form
ann = get(oxr, ["metadata", "annotations", "app.kubernetes.io/name"], "")
```

### Resource creation

**KCL:**
```python
items = [
    {
        apiVersion = "s3.aws.upbound.io/v1beta1"
        kind = "Bucket"
        metadata.name = "my-bucket"
        metadata.annotations = {
            "krm.kcl.dev/composition-resource-name" = "bucket"
        }
        spec.forProvider.region = region
    }
]
```

**Starlark:**
```python
Resource("bucket", {
    "apiVersion": "s3.aws.upbound.io/v1beta1",
    "kind": "Bucket",
    "metadata": {"name": "my-bucket"},
    "spec": {
        "forProvider": {
            "region": region,
        },
    },
})
```

Key differences:
- The resource name is the first argument to `Resource()`, not an annotation.
- Resource bodies are standard Python dicts with string keys, not KCL structs.
- Nested fields use nested dicts, not dot-notation (`spec.forProvider.region`).

### Conditional resources

**KCL:**
```python
_monitoring = [
    {
        apiVersion = "monitoring.example.io/v1"
        kind = "Dashboard"
        metadata.annotations = {
            "krm.kcl.dev/composition-resource-name" = "dashboard"
        }
        spec.enabled = True
    }
] if env == "prod" else []

items = _monitoring + _other_resources
```

**Starlark:**
```python
if env == "prod":
    Resource("dashboard", {
        "apiVersion": "monitoring.example.io/v1",
        "kind": "Dashboard",
        "spec": {"enabled": True},
    })
```

Starlark uses standard `if` statements. No need to build conditional lists
and concatenate them.

### Loop-based resource creation

**KCL:**
```python
_buckets = [
    {
        apiVersion = "s3.aws.upbound.io/v1beta1"
        kind = "Bucket"
        metadata.name = "bucket-${i}"
        metadata.annotations = {
            "krm.kcl.dev/composition-resource-name" = "bucket-${i}"
        }
        spec.forProvider.region = region
    }
    for i in range(3)
]

items = _buckets
```

**Starlark:**
```python
for i in range(3):
    Resource("bucket-%d" % i, {
        "apiVersion": "s3.aws.upbound.io/v1beta1",
        "kind": "Bucket",
        "metadata": {"name": "bucket-%d" % i},
        "spec": {
            "forProvider": {"region": region},
        },
    })
```

Key differences:
- Standard `for` loop instead of list comprehension.
- String formatting uses `%` operator (Python 2 style) instead of `${}` interpolation.

### Connection details

**KCL (XR-level):**
```python
# KCL uses oxr/dxr connection details fields or annotation-based approaches.
# The exact pattern depends on the function-kcl version.
```

**Starlark (XR-level):**
```python
set_connection_details({
    "endpoint": "https://my-service.example.com",
    "password": get(oxr, "spec.credentials.password", ""),
})
```

**Starlark (per-resource):**
```python
Resource("database", {
    "apiVersion": "rds.aws.upbound.io/v1beta1",
    "kind": "Instance",
    "metadata": {"name": "my-db"},
    "spec": {"forProvider": {"region": region}},
}, connection_details={
    "host": "my-db.cluster.us-east-1.rds.amazonaws.com",
    "port": "5432",
})
```

### Conditions and events

**KCL:**
```python
# KCL does not have built-in condition/event support.
# Platform engineers typically use annotation-based approaches or
# rely on Crossplane's automatic condition management.
```

**Starlark:**
```python
# Set a condition on the XR
set_condition(
    type="Ready",
    status="True",
    reason="Available",
    message="All resources provisioned",
)

# Emit an event
emit_event(severity="Normal", message="Provisioning complete")

# Halt execution on fatal error
if not valid:
    fatal(message="Validation failed: missing required field")
```

### DXR status updates

**KCL:**
```python
_dxr = {
    **dxr
    status.ready = "True"
    status.endpoint = endpoint
}
items = [_dxr] + _resources
```

**Starlark:**
```python
# Direct assignment (replaces entire status):
dxr["status"] = {
    "ready": "True",
    "endpoint": endpoint,
}

# Preferred: dot-path writes that preserve sibling fields:
set_xr_status("ready", "True")
set_xr_status("endpoint", endpoint)
```

The `dxr` global is mutable. Direct assignment replaces the entire status dict.
Use `set_xr_status()` for incremental writes that auto-create intermediate
dicts and preserve existing sibling keys.

### Pipeline context

**KCL:**
```python
# Context access depends on function-kcl version and configuration.
```

**Starlark:**
```python
# Read from context
existing = context["some-key"]

# Write to context (propagates to downstream pipeline steps)
context["my-function/status"] = "complete"
```

## Gotchas and differences

### 1. No type system

KCL has a schema/type system. Starlark uses plain dicts. You lose compile-time
type checking but gain simplicity. Validate inputs explicitly:

```python
region = get(oxr, "spec.region", "")
if not region:
    fatal(message="spec.region is required")
```

### 2. String formatting

KCL uses `${}` interpolation. Starlark uses Python `%` formatting:

```python
# KCL:    name = "bucket-${i}"
# Starlark:
name = "bucket-%d" % i
name = "%s-%s" % (prefix, suffix)
```

### 3. Dict access syntax

KCL uses dot-access (`oxr.spec.region`). Starlark StarlarkDicts support
dot-access for simple keys, but `get()` is safer for deeply nested access:

```python
# Works but may raise KeyError if path is missing:
region = oxr.spec.region

# Safe -- returns default if any part of the path is missing:
region = get(oxr, "spec.region", "us-east-1")
```

### 4. Boolean values

KCL uses `True`/`False`. Starlark also uses `True`/`False` (same as Python).
No difference here.

### 5. No imports

Starlark scripts are self-contained. You cannot import modules. Extract common
logic into helper functions within the same script:

```python
def make_bucket(name, region, env):
    Resource(name, {
        "apiVersion": "s3.aws.upbound.io/v1beta1",
        "kind": "Bucket",
        "metadata": {"name": name},
        "spec": {
            "forProvider": {
                "region": region,
                "tags": {"Environment": env},
            },
        },
    })

for i in range(5):
    make_bucket("bucket-%d" % i, region, env)
```

### 6. No None coalescing

KCL has `?.` and `or` for optional chaining. Starlark uses `get()`:

```python
# KCL:     value = oxr.spec?.optional?.field or "default"
# Starlark:
value = get(oxr, "spec.optional.field", "default")
```

### 7. Resource naming

KCL uses the `krm.kcl.dev/composition-resource-name` annotation.
Starlark uses the first argument to `Resource()`:

```python
# KCL:     metadata.annotations = {"krm.kcl.dev/composition-resource-name" = "my-resource"}
# Starlark:
Resource("my-resource", {...})
```

## Step-by-step migration process

### 1. Inventory existing KCL compositions

List all compositions using `function-kcl`:

```bash
kubectl get compositions -o json | \
  jq -r '.items[] | select(.spec.pipeline[]?.functionRef.name == "function-kcl") | .metadata.name'
```

### 2. Install function-starlark

Deploy function-starlark alongside function-kcl (they can coexist):

```bash
cat <<EOF | kubectl apply -f -
apiVersion: pkg.crossplane.io/v1beta1
kind: Function
metadata:
  name: function-starlark
spec:
  package: ghcr.io/wompipomp/function-starlark:latest
EOF
```

### 3. Port each composition

For each KCL composition:

1. Read the KCL source and identify resources, conditionals, and loops.
2. Create the Starlark equivalent using the patterns above.
3. Update the composition pipeline step:
   - Change `functionRef.name` from `function-kcl` to `function-starlark`.
   - Change `input.apiVersion` to `starlark.fn.crossplane.io/v1alpha1`.
   - Change `input.kind` to `StarlarkInput`.
   - Replace `input.spec.source` with the Starlark script.

### 4. Validate with crossplane render

Use `crossplane render` to compare outputs:

```bash
# Render the KCL version
crossplane render xr.yaml kcl-composition.yaml functions.yaml > kcl-output.yaml

# Render the Starlark version
crossplane render xr.yaml starlark-composition.yaml functions.yaml > starlark-output.yaml

# Compare (ignore field ordering)
diff <(yq -P 'sort_keys(..)' kcl-output.yaml) \
     <(yq -P 'sort_keys(..)' starlark-output.yaml)
```

### 5. Deploy and verify

Apply the updated composition to a staging cluster:

```bash
kubectl apply -f starlark-composition.yaml

# Create a test claim and verify resources
kubectl apply -f test-claim.yaml
kubectl get managed -l crossplane.io/composite=$(kubectl get xr -o name | head -1 | cut -d/ -f2)
```

### 6. Remove function-kcl

Once all compositions are migrated and verified:

```bash
kubectl delete function function-kcl
```

## Example: complete migration

See the `example/` directory for a complete side-by-side comparison:

- `example/composition.yaml` -- Starlark version (10 resources)
- `example/kcl-composition.yaml` -- KCL equivalent
- `example/expected-output.yaml` -- Expected render output

The Starlark composition exercises all builtins: `get()`, `Resource()`,
`set_condition()`, `emit_event()`, `set_connection_details()`, `dxr` status
updates, conditionals, and loops.
