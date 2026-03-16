# Builtins reference

function-starlark provides 14 predeclared names -- 6 globals and 8 functions --
that are automatically available in every Starlark script without import. These
are the core API for interacting with Crossplane's composite resource model.

## Quick reference

| Name | Type | Description |
|------|------|-------------|
| `oxr` | global | Observed composite resource (read-only) |
| `dxr` | global | Desired composite resource (read-write) |
| `observed` | global | Observed composed resources by name (read-only) |
| `context` | global | Pipeline context (read-write) |
| `environment` | global | EnvironmentConfig data (read-only) |
| `extra_resources` | global | Extra resources from require_resource/require_resources (read-only) |
| `Resource()` | function | Register a desired composed resource |
| `skip_resource()` | function | Remove a resource from desired state |
| `get()` | function | Safe nested dict access with dot-path |
| `set_condition()` | function | Set a condition on the composite resource |
| `emit_event()` | function | Emit a Normal or Warning event |
| `fatal()` | function | Halt execution with a fatal error |
| `set_connection_details()` | function | Set XR-level connection details |
| `require_resource()` | function | Request a single extra resource |
| `require_resources()` | function | Request multiple extra resources |

---

## Globals

### oxr

**Type:** frozen StarlarkDict (read-only)

The observed composite resource (XR). This is a read-only snapshot of the
current XR state from the API server. Use `get()` for safe nested access.

```python
region = get(oxr, "spec.region", "us-east-1")
name = get(oxr, "metadata.name", "unknown")
labels = get(oxr, "metadata.labels", {})
```

StarlarkDict supports both bracket access (`oxr["metadata"]`) and dot-path
access via `get()`. Use `get()` for deeply nested paths to avoid KeyError on
missing intermediate keys.

---

### dxr

**Type:** mutable StarlarkDict (read-write)

The desired composite resource. Write to this global to set status fields or
other desired state on the XR itself.

```python
dxr["status"] = {
    "ready": True,
    "endpoint": "https://my-service.example.com",
}
```

Changes to `dxr` are applied to the XR's desired state in the function
response. This is how you update XR status fields from your composition script.

---

### observed

**Type:** frozen StarlarkDict of frozen StarlarkDicts (read-only)

Observed composed resources keyed by composition resource name. Each value is a
frozen StarlarkDict representing the observed state of that composed resource
from the API server.

```python
if "my-bucket" in observed:
    bucket = observed["my-bucket"]
    arn = get(bucket, "status.atProvider.arn", "")
```

On first reconciliation, `observed` is empty because no resources have been
created yet. Check for key existence before accessing observed resources.

---

### context

**Type:** mutable dict (read-write)

Pipeline context for passing data between pipeline steps. This is a plain
Starlark dict (not a StarlarkDict), so it does not support dot-path access via
`get()`. Use standard bracket access.

```python
# Read from a previous pipeline step
existing = context["some-key"]

# Write for downstream pipeline steps
context["my-function/status"] = "complete"
```

Context is shared across all pipeline steps in a composition. Use namespaced
keys (e.g., `"my-function/key"`) to avoid collisions.

---

### environment

**Type:** frozen StarlarkDict (read-only)

EnvironmentConfig data from the Crossplane function runtime. Typically empty
unless the Function is configured with environment configs in the composition.

```python
env_region = get(environment, "region", "us-east-1")
```

---

### extra_resources

**Type:** frozen dict (read-only)

Extra resources requested via `require_resource()` or `require_resources()`.
Keyed by the request name passed to those functions. This is a plain Starlark
dict (not a StarlarkDict).

```python
require_resource("config", "v1", "ConfigMap",
    match_name="my-config")

# After the function re-runs with the extra resource available:
if "config" in extra_resources:
    config = extra_resources["config"]
    value = get(config, "data.my-key", "default")
```

Extra resources follow a two-pass pattern: on the first pass your script
requests them, and on the next reconciliation the resources are available in
`extra_resources`.

---

### StarlarkDict vs dict

Two dict types appear in the globals:

| Type | Used for | Dot-path via get() | Frozen variant |
|------|----------|-------------------|----------------|
| StarlarkDict | Kubernetes resource objects (`oxr`, `dxr`, `observed`) | Yes | Yes |
| dict | Simple key-value mappings (`context`, `extra_resources`) | No | Yes (extra_resources) |

Both types support standard dict operations: `get()`, `items()`, `keys()`,
`values()`, `in`, bracket access. StarlarkDict additionally converts nested
protobuf structures for Kubernetes resource access.

---

## Functions

### Resource

```python
ref = Resource(name, body, ready=None, labels=<auto>, connection_details=None,
               depends_on=None, external_name=None)
```

Register a desired composed resource. This is the primary function for creating
Kubernetes resources in a composition.

**Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `name` | string | required | Composition resource name. Must be unique across all `Resource()` calls in the script. |
| `body` | dict | required | Kubernetes resource manifest with `apiVersion`, `kind`, `metadata`, `spec`. |
| `ready` | None \| True \| False | None | Readiness signal. See below. |
| `labels` | dict \| None \| omitted | auto-inject | Label behavior. See below. |
| `connection_details` | dict \| None | None | Per-resource connection details (string key-value pairs). |
| `depends_on` | list \| None | None | List of ResourceRef, string, or `(ref, "field.path")` tuple for creation sequencing. |
| `external_name` | string \| None | None | Sugar for `crossplane.io/external-name` annotation. |

**Returns:** ResourceRef with a `.name` attribute (the composition resource
name). Use in `depends_on` for other resources.

**ready -- three-state readiness:**

- `None` (default): Auto-detect via `function-auto-ready`. Recommended for most
  resources. The resource's readiness is determined by its `status.conditions`.
- `True`: Explicitly mark the resource as ready regardless of its actual status.
- `False`: Explicitly mark the resource as not ready.

**labels -- three-state behavior:**

- **Omitted** (default): Crossplane traceability labels are auto-injected:
  `crossplane.io/composite`, `crossplane.io/claim-name`,
  `crossplane.io/claim-namespace`. Claim labels are only added when a claim
  exists.
- **Dict provided**: User labels are merged at highest priority. Merge order:
  `body.metadata.labels` (lowest) < auto-injected crossplane labels <
  `labels=` kwarg (highest). User values win on conflict. A warning event is
  emitted when a `labels=` kwarg key conflicts with an auto-injected crossplane
  label key.
- **None**: Opt out of all auto-injection. Only `body.metadata.labels` are
  preserved.

**depends_on -- creation sequencing:**

Each element can be:

- **ResourceRef** (returned by a previous `Resource()` call) or **string** --
  defers the dependent until the dependency exists in observed state.
- **Tuple `(ref, "field.path")`** -- defers the dependent until the dependency
  exists in observed state AND the dot-separated field path has a truthy value
  (non-empty string, non-zero number, true, struct, or list). This is useful for
  Object-wrapped resources where the outer Object is observed before the inner
  resource's status fields are populated.

Creates Crossplane Usage resources to ensure the depended-on resources are
created before this one. See [features.md](features.md) for detailed sequencing
behavior.

**external_name:**

Convenience for setting the `crossplane.io/external-name` annotation. Equivalent
to setting the annotation in `body.metadata.annotations`.

**Example:**

```python
# Simple resource
Resource("bucket", {
    "apiVersion": "s3.aws.upbound.io/v1beta1",
    "kind": "Bucket",
    "metadata": {"name": "my-bucket"},
    "spec": {
        "forProvider": {"region": region},
    },
})

# Full example with all kwargs
db_ref = Resource("database", {
    "apiVersion": "rds.aws.upbound.io/v1beta1",
    "kind": "Instance",
    "metadata": {"name": "my-db"},
    "spec": {
        "forProvider": {
            "region": region,
            "instanceClass": "db.t3.micro",
        },
    },
},
    ready=None,
    labels={"team": "platform", "tier": "data"},
    connection_details={"host": "my-db.rds.amazonaws.com", "port": "5432"},
    external_name="my-external-db",
)

# Use the returned ref for creation sequencing
Resource("app", {
    "apiVersion": "apps.example.io/v1",
    "kind": "App",
    "metadata": {"name": "my-app"},
    "spec": {"dbRef": db_ref.name},
}, depends_on=[db_ref])

# Wait for a specific field before creating dependent resource
project = Resource("project", {
    "apiVersion": "project.gcp.upbound.io/v1beta1",
    "kind": "Project",
    "spec": {"forProvider": {"name": "my-project"}},
})
Resource("iam-binding", {
    "apiVersion": "cloudplatform.gcp.upbound.io/v1beta1",
    "kind": "ProjectIAMMember",
    "spec": {"forProvider": {"project": get(observed, "project.status.atProvider.projectId", "")}},
}, depends_on=[(project, "status.atProvider.projectId")])
```

---

### skip_resource

```python
skip_resource(name, reason)
```

Remove a resource from the desired state. Use this to conditionally remove a
resource that was added by a previous pipeline step. A Warning event is emitted
on the first call for a given resource name.

**Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `name` | string | required | Resource name to remove from desired state. |
| `reason` | string | required | Human-readable reason for skipping. |

**Returns:** None.

**Errors:** Fails if the resource was already emitted via `Resource()` in the
current script.

**Example:**

```python
env = get(oxr, "spec.environment", "dev")
if env != "prod":
    skip_resource("monitoring-dashboard", "monitoring only in prod")
```

---

### get

```python
get(obj, path, default=None)
```

Safe nested dict access. Traverses a dict (or StarlarkDict) along a path and
returns the value, or a default if any part of the path is missing.

**Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `obj` | dict or StarlarkDict | required | Object to traverse. |
| `path` | string or list | required | Dot-separated path or list of string keys. |
| `default` | any | None | Value to return if path is not found. |

**Returns:** Value at the path, or `default` if any intermediate key is missing
or the value is None.

**Path formats:**

- **Dot-separated string**: `"spec.region"` -- splits on `.` and traverses each
  segment. Suitable for most Kubernetes field paths.
- **List of strings**: `["metadata", "annotations", "app.kubernetes.io/name"]`
  -- each element is a key. Use this when keys contain dots (e.g., annotation
  keys).

**Example:**

```python
# Simple path
region = get(oxr, "spec.region", "us-east-1")

# Deep nested path
zone = get(oxr, "spec.parameters.networking.zone", "default")

# Keys with dots (annotations) -- use list form
ann = get(oxr, ["metadata", "annotations", "app.kubernetes.io/name"], "")

# Access observed resource fields
arn = get(observed.get("my-bucket", {}), "status.atProvider.arn", "")
```

---

### set_condition

```python
set_condition(type, status, reason, message, target="Composite")
```

Set a condition on the composite resource. Conditions are informational status
signals -- they do **not** control XR readiness (readiness is managed by the
`ready` parameter on `Resource()` or by `function-auto-ready`).

**Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `type` | string | required | Condition type (e.g., `"Ready"`, `"Synced"`, `"Degraded"`). |
| `status` | string | required | `"True"`, `"False"`, or `"Unknown"`. |
| `reason` | string | required | Machine-readable reason (e.g., `"Available"`, `"ReconcileError"`). |
| `message` | string | required | Human-readable message. |
| `target` | string | `"Composite"` | Always `"Composite"` (XR-level condition). |

**Returns:** None.

**Example:**

```python
set_condition(
    type="Ready",
    status="True",
    reason="Available",
    message="All resources provisioned successfully",
)
```

---

### emit_event

```python
emit_event(severity, message, target="Composite")
```

Emit an event on the composite resource.

**Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `severity` | string | required | `"Normal"` or `"Warning"`. |
| `message` | string | required | Event message. |
| `target` | string | `"Composite"` | Always `"Composite"`. |

**Returns:** None.

**Errors:** Fails if severity is not `"Normal"` or `"Warning"`.

**Example:**

```python
emit_event(severity="Normal", message="Provisioning complete")
emit_event(severity="Warning", message="Deprecated API version in use")
```

---

### fatal

```python
fatal(message)
```

Halt script execution immediately with a fatal error. The function returns a
fatal result to Crossplane, which sets a `ReconcileError` condition on the XR.

**Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `message` | string | required | Error message describing why execution was halted. |

**Returns:** Does not return. Raises a fatal error that stops script execution.

**Example:**

```python
region = get(oxr, "spec.region", "")
if not region:
    fatal(message="spec.region is required")
```

---

### set_connection_details

```python
set_connection_details(details)
```

Set XR-level connection details. These are merged with any per-resource
`connection_details` provided via `Resource()`.

**Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `details` | dict | required | Dict of string keys to string values. |

**Returns:** None.

**Example:**

```python
set_connection_details({
    "endpoint": "https://my-service.example.com",
    "password": get(oxr, "spec.credentials.password", ""),
    "port": "5432",
})
```

---

### require_resource

```python
require_resource(name, apiVersion, kind, match_name=None, match_labels=None)
```

Request a single extra resource from the Crossplane API server. At least one of
`match_name` or `match_labels` must be provided. Access the result via
`extra_resources[name]` on the next reconciliation.

**Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `name` | string | required | Request name (used as key in `extra_resources`). |
| `apiVersion` | string | required | API version to match (e.g., `"v1"`). |
| `kind` | string | required | Kind to match (e.g., `"ConfigMap"`). |
| `match_name` | string \| None | None | Match by resource name. |
| `match_labels` | dict \| None | None | Match by labels. |

**Returns:** None.

**Note:** If both `match_name` and `match_labels` are provided, `match_name`
takes precedence and `match_labels` is ignored (a warning is emitted).

**Example:**

```python
# Request a ConfigMap by name
require_resource("config", "v1", "ConfigMap", match_name="my-config")

# On next reconciliation, access the result
if "config" in extra_resources:
    db_host = get(extra_resources["config"], "data.DB_HOST", "localhost")
```

---

### require_resources

```python
require_resources(name, apiVersion, kind, match_labels)
```

Request multiple extra resources matching a label selector. Unlike
`require_resource`, `match_labels` is required (not optional). Access results
via `extra_resources[name]` on the next reconciliation.

**Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `name` | string | required | Request name (used as key in `extra_resources`). |
| `apiVersion` | string | required | API version to match. |
| `kind` | string | required | Kind to match. |
| `match_labels` | dict | required | Label selector (required). |

**Returns:** None.

**Example:**

```python
# Request all Secrets with a specific label
require_resources("certs", "v1", "Secret",
    match_labels={"app": "my-app", "type": "tls"})

# On next reconciliation, iterate the results
if "certs" in extra_resources:
    for cert in extra_resources["certs"]:
        name = get(cert, "metadata.name", "unknown")
```

---

## See also

- [Features guide](features.md) -- Detailed behavior for depends_on creation
  sequencing, labels auto-injection, connection details, skip_resource, and
  observability metrics
- [Standard library reference](stdlib-reference.md) -- Additional utility
  functions for networking, naming, labels, and conditions (loaded via
  `load("oci://...")`)
- [Migration from KCL](migration-from-kcl.md) -- Concept mapping from KCL to
  function-starlark, including side-by-side examples
