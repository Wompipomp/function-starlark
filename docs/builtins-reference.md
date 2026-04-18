# Builtins reference

function-starlark provides 34 predeclared names -- 6 globals, 22 functions, and
6 namespace modules -- that are automatically available in every Starlark script
without import. These are the core API for interacting with Crossplane's
composite resource model.

## Quick reference

### Globals

| Name | Type | Description |
|------|------|-------------|
| `oxr` | global | Observed composite resource (read-only) |
| `dxr` | global | Desired composite resource (read-write) |
| `observed` | global | Observed composed resources by name (read-only) |
| `context` | global | Pipeline context (read-write) |
| `environment` | global | EnvironmentConfig data (read-only) |
| `extra_resources` | global | Extra resources from require_extra_resource/require_extra_resources (read-only) |

### Functions

| Name | Type | Description |
|------|------|-------------|
| `Resource()` | function | Register a desired composed resource |
| `skip_resource()` | function | Remove a resource from desired state |
| `get()` | function | Safe nested dict access with dot-path |
| `get_label()` | function | Safe label lookup by exact key (handles dotted keys) |
| `get_annotation()` | function | Safe annotation lookup by exact key (handles dotted keys) |
| `set_condition()` | function | Set a condition on the composite resource |
| `emit_event()` | function | Emit a Normal or Warning event |
| `fatal()` | function | Halt execution with a fatal error |
| `set_connection_details()` | function | Set XR-level connection details |
| `set_xr_status()` | function | Set XR status field at dot-path with auto-created intermediates |
| `get_observed()` | function | One-call observed resource field lookup with default |
| `require_extra_resource()` | function | Request a single extra resource |
| `require_extra_resources()` | function | Request multiple extra resources |
| `schema()` | function | Define a typed constructor with field validation |
| `field()` | function | Define a field descriptor for schema constructors |
| `struct()` | function | Create an immutable struct with named fields (dot-access) |
| `get_extra_resource()` | function | One-call extra-resource field lookup with default |
| `get_extra_resources()` | function | Get all extra resources for a name as list |
| `is_observed()` | function | Check if a composed resource exists in observed state |
| `observed_body()` | function | Get observed resource body with default |
| `get_condition()` | function | Get condition dict from observed resource |
| `set_response_ttl()` | function | Set response TTL for requeue interval |

### Namespace Modules

| Module | Members | Description |
|--------|---------|-------------|
| `json` | encode, decode, encode_indent, indent | JSON encoding/decoding (from go.starlark.net/lib/json) |
| `crypto` | sha256, sha512, sha1, md5, hmac_sha256, blake3, stable_id | Hashing, HMAC, and deterministic IDs |
| `encoding` | b64enc, b64dec, b64url_enc, b64url_dec, b32enc, b32dec, hex_enc, hex_dec | Base64, base32, and hex encoding |
| `dict` | merge, deep_merge, pick, omit, compact, dig, has_path | Dict merge, subset, compact, and path operations |
| `regex` | match, find, find_all, find_groups, replace, replace_all, split | RE2 regular expressions |
| `yaml` | encode, decode, decode_stream | K8s-compatible YAML (via sigs.k8s.io/yaml) |

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

Extra resources requested via `require_extra_resource()` or `require_extra_resources()`.
Keyed by the request name passed to those functions. This is a plain Starlark
dict (not a StarlarkDict).

```python
require_extra_resource("config", "v1", "ConfigMap",
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
               depends_on=None, external_name=None,
               when=True, skip_reason="", preserve_observed=False)
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
| `when` | bool | `True` | Gate resource emission. `False` skips the resource (requires `skip_reason`). Only accepts `True` or `False` -- non-bool values raise a type error. |
| `skip_reason` | string | `""` | Human-readable reason for skipping. Required when `when=False` (without `preserve_observed`). Appears in a Warning event on skip paths. Always legal to set (unused on non-skip paths); useful when `when` is a runtime expression that may flip between `True` and `False` across reconciliations. |
| `preserve_observed` | bool | `False` | When `True` and body is `None` (or `when=False`), emit the observed body verbatim if the resource exists in observed state. Used for cliff-guard patterns to prevent resource deletion when config is temporarily unavailable. |

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

**when / skip_reason / preserve_observed -- resource gating:**

These three kwargs control conditional resource emission and cliff-guard
patterns. The `when` gate is evaluated first (before body type-checking), so when
`when=False` the body kwarg is ignored.

| `when` | `body` | `preserve_observed` | Behavior |
|--------|--------|---------------------|----------|
| True/omitted | dict | False/omitted | **Normal:** emit body as desired resource |
| True/omitted | dict | True | **Normal:** emit body (preserve_observed is a no-op when body is a dict) |
| True/omitted | None | False/omitted | **Warn + skip:** body is None; warns and skips, message suggests `preserve_observed=True` |
| True/omitted | None | True | **Preserve:** emit observed body verbatim if found, skip with Warning if not |
| False | dict/None | False/omitted | **Skip:** `skip_reason` required; resource not emitted; Warning event with skip_reason |
| False | dict/None | True, found | **Cliff guard (found):** emit observed body verbatim; body kwarg ignored |
| False | dict/None | True, not found | **Cliff guard (miss):** skip with Warning; body kwarg ignored |

**Events emitted:**

- **Skip paths** (when=False without preserve, or body=None without preserve)
  emit a Warning event on the composite resource (XR) with the skip reason.
- **Preserve paths** (preserve_observed=True with observed body found) emit a
  Normal event noting that the observed body was emitted verbatim.
- **Cliff guard miss** (preserve_observed=True but resource not found in observed
  state) emits a Warning event indicating the resource was not found.
- All events are associated with the XR as the involvedObject.

**Errors:**

- `when=False` without `skip_reason` (and without `preserve_observed=True`)
  raises an error. A reason must be provided when skipping a resource.
- Non-bool value for `when` raises a type error. Only `True` or `False` are
  accepted (e.g., `when=1` or `when="yes"` will fail).
- `body=None` without `preserve_observed=True` is not a fatal error but logs a
  warning suggesting `preserve_observed=True` and skips the resource.

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

# Conditional emission with gating
Resource("optional-feature", feature_body,
    when=feature_enabled, skip_reason="feature disabled by spec")

# Cliff guard: preserve observed resource when config is unavailable
config = get(oxr, "spec.externalConfig", None)
body = build_resource(config) if config else None
Resource("external-dep", body, preserve_observed=True)
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

### require_extra_resource

```python
require_extra_resource(name, apiVersion, kind, match_name=None, match_labels=None)
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
require_extra_resource("config", "v1", "ConfigMap", match_name="my-config")

# On next reconciliation, access the result
if "config" in extra_resources:
    db_host = get(extra_resources["config"], "data.DB_HOST", "localhost")
```

---

### require_extra_resources

```python
require_extra_resources(name, apiVersion, kind, match_labels)
```

Request multiple extra resources matching a label selector. Unlike
`require_extra_resource`, `match_labels` is required (not optional). Access results
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
require_extra_resources("certs", "v1", "Secret",
    match_labels={"app": "my-app", "type": "tls"})

# On next reconciliation, iterate the results
if "certs" in extra_resources:
    for cert in extra_resources["certs"]:
        name = get(cert, "metadata.name", "unknown")
```

---

### get_label

```python
get_label(res, key, default=None)
```

Safe label lookup by exact key. Handles dotted keys like
`"app.kubernetes.io/name"` correctly -- unlike `get()` which splits on dots,
causing `get(oxr, "metadata.labels.app.kubernetes.io/name", "")` to traverse
through `"app"` then `"kubernetes"` then `"io/name"` instead of looking up the
full key. `get_label()` goes directly to `res["metadata"]["labels"][key]` and
returns the default if any part of the chain is missing.

**Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `res` | dict or StarlarkDict | required | Resource to read from (e.g., `oxr`, an observed resource). |
| `key` | string | required | Exact label key (e.g., `"app.kubernetes.io/name"`). Must not be empty. |
| `default` | any | None | Value to return if the label is not found. |

**Returns:** The label value, or `default` if the key, labels map, or metadata
is missing.

**Example:**

```python
# Before: get() splits on dots, so dotted label keys require verbose list form
name = get(oxr, ["metadata", "labels", "app.kubernetes.io/name"], "")

# After: one-call pattern handles dotted keys correctly
name = get_label(oxr, "app.kubernetes.io/name", "")

# Works on any resource with metadata.labels
team = get_label(observed["my-bucket"], "team", "default")
```

---

### get_annotation

```python
get_annotation(res, key, default=None)
```

Safe annotation lookup by exact key. Like `get_label()`, this handles dotted
keys correctly by going directly to `res["metadata"]["annotations"][key]`.
Returns the default if any part of the chain is missing.

**Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `res` | dict or StarlarkDict | required | Resource to read from (e.g., `oxr`, an observed resource). |
| `key` | string | required | Exact annotation key (e.g., `"crossplane.io/external-name"`). Must not be empty. |
| `default` | any | None | Value to return if the annotation is not found. |

**Returns:** The annotation value, or `default` if the key, annotations map, or
metadata is missing.

**Example:**

```python
# Before: verbose list-form access required for dotted annotation keys
ext_name = get(oxr, ["metadata", "annotations", "crossplane.io/external-name"], "")

# After: one-call pattern
ext_name = get_annotation(oxr, "crossplane.io/external-name", "")

# Read an annotation from an observed resource
zone = get_annotation(observed["my-db"], "topology.kubernetes.io/zone", "")
```

---

### set_xr_status

```python
set_xr_status(path, value)
```

Set a status field on the XR at a dot-separated path. Writes to
`dxr["status"][path segments...]`, auto-creating intermediate StarlarkDicts as
needed. Uses mkdir-p semantics: if a non-dict value exists at an intermediate
path segment, it is silently overwritten with a new dict. Consecutive writes to
the same prefix preserve sibling keys.

**Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `path` | string | required | Dot-separated path (e.g., `"atProvider.region"`). Rejects empty paths, leading/trailing dots, and consecutive dots. |
| `value` | any | required | Value to write at the path. |

**Returns:** None.

**Errors:** Fails if path is empty, has leading/trailing dots, or contains
consecutive dots.

**Example:**

```python
# Before: manual dict construction clobbers sibling keys on each write
dxr["status"] = {
    "atProvider": {
        "projectId": project_id,
        "arn": arn,
    },
    "region": region,
}

# After: dot-path writes with auto-created intermediates
set_xr_status("atProvider.projectId", project_id)
set_xr_status("atProvider.arn", arn)
set_xr_status("region", region)
# Sibling preservation: writing atProvider.arn does not clobber atProvider.projectId
```

---

### get_observed

```python
get_observed(name, path, default=None)
```

One-call observed resource field lookup. Equivalent to
`get(observed.get(name, {}), path, default)` but in a single call. Returns the
default when the named resource is missing, the path is missing, or the value at
the path is None.

**Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `name` | string | required | Composition resource name. Must not be empty. |
| `path` | string or list | required | Dot-separated path or list of string keys (same format as `get()`). Must not be empty. |
| `default` | any | None | Value to return if the resource or path is not found. |

**Returns:** Value at the path in the named observed resource, or `default` if
the resource is missing, the path is missing, or the value is None.

**Example:**

```python
# Before: two-step lookup with manual existence check
bucket_arn = ""
if "my-bucket" in observed:
    bucket_arn = get(observed["my-bucket"], "status.atProvider.arn", "")

# After: one-call with default
bucket_arn = get_observed("my-bucket", "status.atProvider.arn", "")

# Safe on first reconciliation when no observed resources exist
db_host = get_observed("my-db", "status.atProvider.address", "pending")
```

---

### schema

```python
schema(name, doc=None, **fields)
```

Define a typed constructor that validates keyword arguments at construction
time. Each kwarg (except `doc`) must be a `field()` descriptor. Calling the
returned constructor with kwargs validates types, required fields, enum values,
and unknown fields, reporting **all** errors at once rather than failing on the
first one.

The constructor returns a **SchemaDict** -- a dict-compatible wrapper that
prints with the schema name tag (e.g., `StorageAccount({"location": "eastus"})`)
but has `Type() == "dict"` and works transparently with `Resource()`. You can
read and write keys on a SchemaDict just like a regular dict. The tagged print
format makes debugging easier because you can see which schema produced a value.

**Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `name` | string | required | Schema name, used in error messages and SchemaDict print output. |
| `doc` | string | None | Documentation string for the schema (available via `.doc` attribute). |
| `**fields` | field() descriptors | required | Each kwarg defines a field. The kwarg name is the field name and the value must be a `field()` call. |

**Returns:** SchemaCallable -- a callable constructor. Calling it with kwargs
validates inputs and returns a SchemaDict (dict-compatible).

**Attributes:**

| Attribute | Type | Description |
|-----------|------|-------------|
| `.name` | string | The schema name. |
| `.doc` | string | The documentation string (empty string if not set). |
| `.fields` | dict | Dict mapping field name to FieldDescriptor. |

**Example -- flat schema:**

```python
Account = schema("Account",
    location=field(type="string", required=True),
    sku=field(type="string", default="Standard_LRS",
              enum=["Standard_LRS", "Standard_GRS", "Premium_LRS"]),
    tags=field(type="dict", required=True),
    enable_https=field(type="bool", default=True),
)

acct = Account(location="eastus", tags={"env": "prod"})
# acct is a dict: {"location": "eastus", "sku": "Standard_LRS", "tags": {"env": "prod"}, "enable_https": True}
# prints as: Account({"location": "eastus", "sku": "Standard_LRS", ...})

Resource("storage", {
    "apiVersion": "storage.azure.upbound.io/v1beta1",
    "kind": "Account",
    "spec": {"forProvider": acct},  # works transparently with Resource()
})
```

**Example -- nested schema:**

```python
IpRule = schema("IpRule",
    action=field(type="string", default="Allow"),
    ip_address=field(type="string", required=True),
)

NetworkRules = schema("NetworkRules",
    default_action=field(type="string", enum=["Allow", "Deny"]),
    ip_rules=field(type="list", items=IpRule),
)

rules = NetworkRules(
    default_action="Deny",
    ip_rules=[{"action": "Allow", "ip_address": "10.0.0.1"}],
)
# Nested dicts are validated against IpRule automatically
```

**Error output:**

When validation fails, all errors are reported at once with the schema name:

```
Account: 3 validation errors:
- location: expected string, got int (123)
- sku: value "SuperFast" not in enum ["Standard_LRS", "Standard_GRS", "Premium_LRS"]
- tags: required field missing
```

For nested schemas, error paths include the full field path:

```
NetworkRules: 1 validation error:
- ip_rules[0].ip_address: required field missing
```

---

### field

```python
field(type="", required=False, default=None, enum=None, doc="", items=None)
```

Define a field descriptor for use in `schema()` constructors. A FieldDescriptor
specifies the constraints for a single schema field: its type, whether it is
required, a default value, allowed enum values, documentation, and (for list
fields) the schema for list elements.

The `type` parameter accepts either a string for primitive types or a schema
reference for nested validation. An empty type string (`""`) means the field
accepts any value -- this supports gradual typing where you can add type
constraints incrementally.

**Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `type` | string or schema | `""` | Primitive type name (`"string"`, `"int"`, `"float"`, `"bool"`, `"list"`, `"dict"`) or a schema reference for nested validation. Empty string accepts any value. |
| `required` | bool | `False` | Whether the field must be provided. Mutually exclusive with `default`. |
| `default` | any | `None` | Default value applied when the field is omitted. Mutually exclusive with `required`. |
| `enum` | list or None | `None` | List of allowed values. Validated after type checking. |
| `doc` | string | `""` | Documentation string for the field. |
| `items` | schema or None | `None` | Schema for list elements. Only valid when `type="list"`. Must be a schema reference (not a string). |

**Constraints:**

- `required` and `default` are mutually exclusive -- a field cannot be both
  required and have a default value.
- `items` is only valid when `type="list"`. Using `items` with any other type
  raises an error.
- `items` must be a schema reference (a value returned by `schema()`), not a
  string.

**Returns:** FieldDescriptor (used as a kwarg value in `schema()` calls).

**Attributes:**

| Attribute | Type | Description |
|-----------|------|-------------|
| `.type` | string | The type name (primitive name or schema name). |
| `.required` | bool | Whether the field is required. |
| `.default` | value | The default value (`None` if not set). |
| `.enum` | list or None | The enum constraint list, or `None`. |
| `.doc` | string | The documentation string. |

**Example:**

```python
# Primitive types
field(type="string", required=True)
field(type="int", default=3)
field(type="bool", default=False)
field(type="dict")

# Enum constraint
field(type="string", default="Standard", enum=["Standard", "Premium"])

# Nested schema reference
SubSchema = schema("SubSchema", name=field(type="string", required=True))
field(type=SubSchema)  # validates nested dict against SubSchema

# List of schema
field(type="list", items=SubSchema)  # each list element validated against SubSchema

# Gradual typing (empty type accepts any value)
field()  # any type, optional, no default
field(required=True)  # any type, required

# Documentation
field(type="string", doc="The Azure region for this resource")
```

---

### struct

```python
struct(**kwargs)
```

Create an immutable struct with named fields accessible via dot notation. This
is the standard Starlark struct constructor from `starlarkstruct.Make`. It is
used internally by namespace alias imports (`load("mod.star", ns="*")`) to wrap
module exports in a dot-accessible namespace. It is also available directly for
creating lightweight record types.

**Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `**kwargs` | any | required | Named fields. Each kwarg becomes a field on the struct. |

**Returns:** An immutable struct value. Fields are accessed via dot notation.
Structs support `==` comparison, `str()`, and `dir()`.

**Example:**

```python
# Create a struct directly
point = struct(x=1, y=2)
point.x  # 1
point.y  # 2

# Namespace alias imports use struct internally
load("helpers.star", h="*")
h.my_function()  # h is a struct wrapping all exports from helpers.star
```

---

### get_extra_resource

```python
get_extra_resource(name, path=None, default=None)
```

One-call extra-resource field lookup. Returns the value at `path` within the
first matching extra resource for `name`, or `default` when the name is missing,
the match list is empty, or the path is not found within the resource. When
`path` is None, returns the full resource body dict.

This function takes the first item (`[0]`) from the match list for the given
name. Use `get_extra_resources()` (plural) to retrieve all matching resources.

The request/get pattern is: `require_extra_resource()` requests the resource
(first reconciliation), then `get_extra_resource()` reads the result (subsequent
reconciliations).

**Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `name` | string | required | Request name (key in `extra_resources`). Must not be empty. |
| `path` | string \| None | None | Dot-separated path within the resource body. None returns the full body. |
| `default` | any | None | Value to return when the resource, match list, or path is missing. |

**Returns:** Value at the path in the first matching extra resource, or `default`.

**Errors:** Fails if `name` is empty.

**Example:**

```python
# Request an extra resource (first reconciliation)
require_extra_resource("cluster", "v1", "ConfigMap", match_name="cluster-info")

# Read a field from the extra resource (subsequent reconciliation)
region = get_extra_resource("cluster", "spec.region", "us-west-2")

# Get the full resource body (path=None)
cluster_body = get_extra_resource("cluster")
```

---

### get_extra_resources

```python
get_extra_resources(name, path=None, default=[])
```

Get all extra resources for a given name as a list. Unlike
`get_extra_resource()` (singular) which returns only the first match, this
returns ALL matching resources. When `path` is provided, traverses the dot-path
on each resource and returns only values where the path exists. Returns `default`
(empty list) when the name is not found or the match list is empty.

**Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `name` | string | required | Request name (key in `extra_resources`). Must not be empty. |
| `path` | string \| None | None | Dot-separated path within each resource body. None returns the full body dicts. |
| `default` | any | `[]` | Value to return when the resource or match list is missing. |

**Returns:** List of values (one per matching resource), or `default` if the name
is not found.

**Errors:** Fails if `name` is empty.

**Example:**

```python
# Request multiple extra resources by label
require_extra_resources("certs", "v1", "Secret", match_labels={"type": "tls"})

# Get all matching resources as a list
certs = get_extra_resources("certs")
for cert in certs:
    name = get(cert, "metadata.name", "unknown")

# Extract a specific field from each resource
cert_names = get_extra_resources("certs", "metadata.name")
```

---

### is_observed

```python
is_observed(name)
```

Returns True if the named composed resource exists in the observed state, False
otherwise. Use this for branching without field access. Equivalent to
`name in observed` but more readable and explicit about intent.

**Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `name` | string | required | Composition resource name. Must not be empty. |

**Returns:** `True` if the resource exists in observed state, `False` otherwise.

**Errors:** Fails if `name` is empty.

**Example:**

```python
if is_observed("database"):
    db_host = get_observed("database", "status.atProvider.address", "")
else:
    db_host = "pending"
```

---

### observed_body

```python
observed_body(name, default=None)
```

Returns the full observed resource body dict, or `default` if the named resource
is not found. Combines existence check and body retrieval in one call. Equivalent
to `observed.get(name, {}).get("resource", default)` but as a single call.

**Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `name` | string | required | Composition resource name. Must not be empty. |
| `default` | any | None | Value to return if the resource is not found. |

**Returns:** The full observed resource body dict, or `default`.

**Errors:** Fails if `name` is empty.

**Example:**

```python
db = observed_body("database", default={})
if db:
    db_host = get(db, "status.atProvider.address", "pending")
```

---

### get_condition

```python
get_condition(name, type)
```

Returns a condition dict for the named condition type on the observed resource,
or None if the resource or condition is not found. The returned dict is always
a **new unfrozen dict** (not a reference to frozen observed data), so you can
safely modify it. The dict always has exactly 4 keys; missing fields default to
empty string.

The first argument is the **observed resource name** (not the resource dict
itself).

**Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `name` | string | required | Composition resource name. Must not be empty. |
| `type` | string | required | Condition type to look up (e.g., `"Ready"`, `"Synced"`). Must not be empty. |

**Returns:** Dict with keys `status`, `reason`, `message`, `lastTransitionTime`,
or `None` if the resource or condition type is not found.

**Errors:** Fails if `name` or `type` is empty.

**Example:**

```python
cond = get_condition("database", "Ready")
if cond and cond["status"] == "True":
    # database is ready
    emit_event("Normal", "Database is ready")
else:
    set_response_ttl("30s")
```

---

### set_response_ttl

```python
set_response_ttl(duration)
```

Set the response TTL on the RunFunctionResponse. This controls how long
Crossplane waits before re-running the pipeline (requeue interval). Accepts a
Go duration string (`"30s"`, `"5m"`, `"1m30s"`) or an integer (seconds).
Overrides the default `sequencingTTL` from StarlarkInput. Can be called multiple
times; the last call wins.

**Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `duration` | string \| int | required | Duration as Go duration string (e.g., `"30s"`) or integer seconds. Must be non-negative. |

**Returns:** None.

**Errors:** Fails if `duration` is not a string or int, if the string cannot be
parsed as a Go duration, if the integer is too large, or if the duration is
negative.

**Example:**

```python
# Using a duration string
set_response_ttl("30s")

# Using integer seconds (equivalent to "30s")
set_response_ttl(30)

# Conditional requeue: poll faster when waiting for a resource
if not is_observed("database"):
    set_response_ttl("10s")
else:
    set_response_ttl("5m")
```

---

## Namespace Modules

Six predeclared namespace modules are available in every script without import.
Each module groups related functions under a dot-accessible name.

### json

The `json` module provides JSON encoding and decoding. This is the upstream
module from [go.starlark.net/lib/json](https://pkg.go.dev/go.starlark.net/lib/json),
not a custom implementation.

#### json.encode

```python
json.encode(x)
```

Encode a Starlark value as a JSON string. Dicts, lists, strings, ints, floats,
bools, and None are supported. Dict keys must be strings.

**Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `x` | any | required | Starlark value to encode. |

**Returns:** JSON string.

**Example:**

```python
json.encode({"key": "value", "count": 42})
# '{"key":"value","count":42}'
```

---

#### json.decode

```python
json.decode(x)
```

Decode a JSON string into a Starlark value. JSON objects become dicts, arrays
become lists, strings become strings, numbers become ints or floats, booleans
become bools, and null becomes None.

**Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `x` | string | required | JSON string to decode. |

**Returns:** Starlark value.

**Errors:** Fails if the string is not valid JSON.

**Example:**

```python
data = json.decode('{"name": "test", "count": 5}')
data["name"]   # "test"
data["count"]  # 5
```

---

#### json.encode_indent

```python
json.encode_indent(x, prefix="", indent="\t")
```

Encode a Starlark value as a pretty-printed JSON string with indentation.

**Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `x` | any | required | Starlark value to encode. |
| `prefix` | string | `""` | Prefix prepended to each line. |
| `indent` | string | `"\t"` | Indentation string for each nesting level. |

**Returns:** Pretty-printed JSON string.

**Example:**

```python
json.encode_indent({"key": "value"}, indent="  ")
# '{\n  "key": "value"\n}'
```

---

#### json.indent

```python
json.indent(s, prefix="", indent="\t")
```

Reformat an existing JSON string with new indentation. The input must already
be valid JSON.

**Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `s` | string | required | Valid JSON string to reformat. |
| `prefix` | string | `""` | Prefix prepended to each line. |
| `indent` | string | `"\t"` | Indentation string for each nesting level. |

**Returns:** Reformatted JSON string.

**Errors:** Fails if `s` is not valid JSON.

**Example:**

```python
compact = '{"key":"value","count":42}'
json.indent(compact, indent="  ")
# '{\n  "key": "value",\n  "count": 42\n}'
```

---

### crypto

The `crypto` module provides deterministic hashing, HMAC, and ID generation
functions. All hash functions accept string or bytes input and return lowercase
hex digest strings.

#### crypto.sha256

```python
crypto.sha256(data)
```

Compute the SHA-256 hash of the input data.

**Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `data` | string or bytes | required | Data to hash. |

**Returns:** Lowercase hex-encoded SHA-256 digest string (64 characters).

**Example:**

```python
crypto.sha256("hello world")
# "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9"
```

---

#### crypto.sha512

```python
crypto.sha512(data)
```

Compute the SHA-512 hash of the input data.

**Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `data` | string or bytes | required | Data to hash. |

**Returns:** Lowercase hex-encoded SHA-512 digest string (128 characters).

**Example:**

```python
crypto.sha512("hello world")
# "309ecc489c12d6eb4cc40f50c902f2b4d0ed77ee511a7c7a9bcd3ca86d4cd86f..."
```

---

#### crypto.sha1

```python
crypto.sha1(data)
```

Compute the SHA-1 hash of the input data.

**Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `data` | string or bytes | required | Data to hash. |

**Returns:** Lowercase hex-encoded SHA-1 digest string (40 characters).

**Example:**

```python
crypto.sha1("hello world")
# "2aae6c35c94fcfb415dbe95f408b9ce91ee846ed"
```

---

#### crypto.md5

```python
crypto.md5(data)
```

Compute the MD5 hash of the input data. **Non-cryptographic use only** -- MD5 is
cryptographically broken. Use for checksums, cache keys, or compatibility with
legacy systems that require MD5 hashes.

**Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `data` | string or bytes | required | Data to hash. |

**Returns:** Lowercase hex-encoded MD5 digest string (32 characters).

**Example:**

```python
crypto.md5("hello world")
# "5eb63bbbe01eeed093cb22bb8f5acdc3"
```

---

#### crypto.hmac_sha256

```python
crypto.hmac_sha256(key, message)
```

Compute an HMAC-SHA256 message authentication code.

**Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `key` | string or bytes | required | HMAC secret key. |
| `message` | string or bytes | required | Message to authenticate. |

**Returns:** Lowercase hex-encoded HMAC-SHA256 digest string (64 characters).

**Example:**

```python
crypto.hmac_sha256("secret", "hello world")
# hex HMAC digest
```

---

#### crypto.blake3

```python
crypto.blake3(data)
```

Compute the BLAKE3 hash of the input data. BLAKE3 is faster than SHA-256 on
modern hardware while providing equivalent security.

**Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `data` | string or bytes | required | Data to hash. |

**Returns:** Lowercase hex-encoded BLAKE3 digest string (64 characters).

**Example:**

```python
crypto.blake3("hello world")
# "d74981efa70a0c880b8d8c1985d075dbcbf679b99a5f9914e5aaf96b831a9e24"
```

---

#### crypto.stable_id

```python
crypto.stable_id(seed, length=8)
```

Generate a deterministic hex ID from a seed string. Hashes the seed with SHA-256
and returns the first `length` hex characters. Useful for generating stable,
reproducible short identifiers from longer strings (e.g., resource names from
XR metadata).

**Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `seed` | string or bytes | required | Seed value to hash. |
| `length` | int | `8` | Number of hex characters to return. Must be between 1 and 64 (inclusive). |

**Returns:** Deterministic hex ID string of the specified length.

**Errors:** Fails if `length` is not between 1 and 64.

**Example:**

```python
crypto.stable_id("my-xr-name")
# "a1b2c3d4" (8 hex chars by default)

crypto.stable_id("my-xr-name", length=16)
# "a1b2c3d4e5f6g7h8" (16 hex chars)
```

---

### encoding

The `encoding` module provides base64, base32, and hex encoding/decoding
functions. URL-safe base64 and base32 use no-padding convention.

#### encoding.b64enc

```python
encoding.b64enc(data)
```

Encode data as a standard base64 string (with padding).

**Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `data` | string or bytes | required | Data to encode. |

**Returns:** Standard base64-encoded string.

**Example:**

```python
encoding.b64enc("hello world")
# "aGVsbG8gd29ybGQ="
```

---

#### encoding.b64dec

```python
encoding.b64dec(data)
```

Decode a standard base64 string.

**Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `data` | string | required | Base64-encoded string to decode. |

**Returns:** Decoded string.

**Errors:** Fails if the input is not valid base64.

**Example:**

```python
encoding.b64dec("aGVsbG8gd29ybGQ=")
# "hello world"
```

---

#### encoding.b64url_enc

```python
encoding.b64url_enc(data)
```

Encode data as a URL-safe base64 string without padding. Uses `-` and `_`
instead of `+` and `/`.

**Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `data` | string or bytes | required | Data to encode. |

**Returns:** URL-safe base64-encoded string (no padding).

**Example:**

```python
encoding.b64url_enc("hello+world/test")
# URL-safe base64 without padding characters
```

---

#### encoding.b64url_dec

```python
encoding.b64url_dec(data)
```

Decode a URL-safe base64 string (without padding).

**Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `data` | string | required | URL-safe base64-encoded string to decode. |

**Returns:** Decoded string.

**Errors:** Fails if the input is not valid URL-safe base64.

**Example:**

```python
encoded = encoding.b64url_enc("hello world")
encoding.b64url_dec(encoded)
# "hello world"
```

---

#### encoding.b32enc

```python
encoding.b32enc(data)
```

Encode data as a standard base32 string without padding.

**Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `data` | string or bytes | required | Data to encode. |

**Returns:** Base32-encoded string (no padding).

**Example:**

```python
encoding.b32enc("hello")
# "NBSWY3DP"
```

---

#### encoding.b32dec

```python
encoding.b32dec(data)
```

Decode a base32 string (without padding).

**Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `data` | string | required | Base32-encoded string to decode. |

**Returns:** Decoded string.

**Errors:** Fails if the input is not valid base32.

**Example:**

```python
encoding.b32dec("NBSWY3DP")
# "hello"
```

---

#### encoding.hex_enc

```python
encoding.hex_enc(data)
```

Encode data as a lowercase hex string.

**Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `data` | string or bytes | required | Data to encode. |

**Returns:** Lowercase hex-encoded string.

**Example:**

```python
encoding.hex_enc("hello")
# "68656c6c6f"
```

---

#### encoding.hex_dec

```python
encoding.hex_dec(data)
```

Decode a hex string.

**Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `data` | string | required | Hex-encoded string to decode. |

**Returns:** Decoded string.

**Errors:** Fails if the input is not valid hex.

**Example:**

```python
encoding.hex_dec("68656c6c6f")
# "hello"
```

---

### dict

The `dict` module provides dict manipulation functions for safe merging,
filtering, and nested path traversal of Kubernetes-style dictionaries. All
functions return **new dicts** without mutating their inputs.

#### dict.merge

```python
dict.merge(d1, d2, ...)
```

Shallow right-wins merge of two or more dicts. Returns a new dict where later
arguments override earlier ones for matching keys. Does not mutate any input.

**Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `d1, d2, ...` | dict | required | Two or more dicts to merge. At least 2 arguments required. |

**Returns:** New dict with all keys merged (shallow right-wins).

**Errors:** Fails if fewer than 2 arguments are provided. Fails if any argument
is not a dict.

**Example:**

```python
base = {"tier": "standard", "env": "dev"}
override = {"env": "prod", "team": "platform"}
dict.merge(base, override)
# {"tier": "standard", "env": "prod", "team": "platform"}

# Three-way merge
dict.merge(defaults, team_config, env_overrides)
```

---

#### dict.deep_merge

```python
dict.deep_merge(d1, d2, ...)
```

Recursive right-wins merge of two or more dicts. When both sides have a dict
value for the same key, the merge recurses into those nested dicts. Non-dict
values and lists are treated atomically (right side replaces). Does not mutate
any input -- creates new dicts at every recursion level.

**Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `d1, d2, ...` | dict | required | Two or more dicts to merge. At least 2 arguments required. |

**Returns:** New dict with all keys deeply merged (recursive right-wins).

**Errors:** Fails if fewer than 2 arguments are provided. Fails if any argument
is not a dict.

**Example:**

```python
defaults = {"spec": {"replicas": 3, "image": "nginx:latest"}}
overrides = {"spec": {"replicas": 5, "resources": {"cpu": "100m"}}}
dict.deep_merge(defaults, overrides)
# {"spec": {"replicas": 5, "image": "nginx:latest", "resources": {"cpu": "100m"}}}
```

---

#### dict.pick

```python
dict.pick(d, keys)
```

Return a new dict containing only the specified keys that exist in the input.
Keys that do not exist in `d` are silently ignored.

**Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `d` | dict | required | Source dict. |
| `keys` | list | required | List of keys to include. |

**Returns:** New dict with only the matching keys.

**Example:**

```python
resource = {"apiVersion": "v1", "kind": "ConfigMap", "metadata": {}, "data": {}}
dict.pick(resource, ["apiVersion", "kind"])
# {"apiVersion": "v1", "kind": "ConfigMap"}
```

---

#### dict.omit

```python
dict.omit(d, keys)
```

Return a new dict with the specified keys removed. Keys that do not exist in `d`
are silently ignored.

**Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `d` | dict | required | Source dict. |
| `keys` | list | required | List of keys to exclude. |

**Returns:** New dict without the specified keys.

**Example:**

```python
resource = {"apiVersion": "v1", "kind": "ConfigMap", "metadata": {}, "data": {}}
dict.omit(resource, ["metadata"])
# {"apiVersion": "v1", "kind": "ConfigMap", "data": {}}
```

---

#### dict.compact

```python
dict.compact(d)
```

Recursively removes None-valued entries from a dict at any nesting depth.
Recurses into nested dicts and lists. Tuples pass through untouched (immutable).
None elements in lists are NOT removed (index safety). Returns a new dict without
mutating the input.

**K8s safety:** Empty strings, lists, and dicts are preserved at all depths --
these carry intent in Kubernetes manifests (e.g., `resources: {}` means "no
limits", not "omit the field").

**Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `d` | dict | required | Dict to compact. Nested dicts and dicts within lists are recursively compacted. |

**Returns:** New dict with all None values removed at every nesting depth.

**Errors:** Fails if recursion depth exceeds 32 levels.

**Example:**

```python
# Prune optional fields from a Deployment spec
deploy = dict.compact({
    "apiVersion": "apps/v1",
    "kind": "Deployment",
    "metadata": {
        "name": "my-app",
        "annotations": annotations if annotations else None,
    },
    "spec": {
        "replicas": replicas,
        "template": {
            "spec": {
                "initContainers": init_containers if init_containers else None,
                "containers": [{
                    "name": "app",
                    "image": image,
                    "volumeMounts": volume_mounts if volume_mounts else None,
                }],
                "volumes": volumes if volumes else None,
            },
        },
    },
})
# If annotations=None, initContainers=None, volumes=None:
# Those keys are removed. Empty strings/lists/dicts survive.
```

---

#### dict.dig

```python
dict.dig(d, path, default=None)
```

Safe dotted-path lookup in a dict. Traverses nested dicts along a dot-separated
path and returns the value, or `default` if any intermediate key is missing.

**Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `d` | dict | required | Dict to traverse. |
| `path` | string | required | Dot-separated path (e.g., `"spec.forProvider.region"`). Rejects empty paths, leading/trailing dots, and consecutive dots. |
| `default` | any | None | Value to return if the path is not found. |

**Returns:** Value at the path, or `default`.

**Errors:** Fails if `path` is empty or malformed (leading/trailing/consecutive
dots).

**Example:**

```python
resource = {"spec": {"forProvider": {"region": "us-east-1"}}}
dict.dig(resource, "spec.forProvider.region")
# "us-east-1"

dict.dig(resource, "spec.forProvider.zone", "us-east-1a")
# "us-east-1a" (not found, returns default)
```

---

#### dict.has_path

```python
dict.has_path(d, path)
```

Check if a dotted path exists in a dict. Returns True if all intermediate keys
exist and the final key is present.

**Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `d` | dict | required | Dict to check. |
| `path` | string | required | Dot-separated path. Rejects empty paths, leading/trailing dots, and consecutive dots. |

**Returns:** `True` if the path exists, `False` otherwise.

**Errors:** Fails if `path` is empty or malformed.

**Example:**

```python
resource = {"spec": {"forProvider": {"region": "us-east-1"}}}
dict.has_path(resource, "spec.forProvider.region")  # True
dict.has_path(resource, "spec.forProvider.zone")    # False
```

---

### regex

The `regex` module provides RE2 regular expression functions using Go's
[regexp](https://pkg.go.dev/regexp) package (RE2 syntax). Compiled patterns are
cached in a bounded LRU cache (capacity 64) for performance.

#### regex.match

```python
regex.match(pattern, s)
```

Returns True if the pattern matches anywhere in the string `s`.

**Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `pattern` | string | required | RE2 regular expression pattern. |
| `s` | string | required | String to search. |

**Returns:** `True` if the pattern matches, `False` otherwise.

**Errors:** Fails if the pattern is not a valid RE2 expression.

**Example:**

```python
regex.match("[0-9]+", "abc123def")  # True
regex.match("^[0-9]+$", "abc123")  # False
```

---

#### regex.find

```python
regex.find(pattern, s)
```

Returns the first match of the pattern in the string, or None if no match is
found.

**Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `pattern` | string | required | RE2 regular expression pattern. |
| `s` | string | required | String to search. |

**Returns:** First match string, or `None`.

**Errors:** Fails if the pattern is not a valid RE2 expression.

**Example:**

```python
regex.find("[0-9]+", "abc123def456")
# "123"

regex.find("[0-9]+", "abcdef")
# None
```

---

#### regex.find_all

```python
regex.find_all(pattern, s)
```

Returns a list of all non-overlapping matches of the pattern in the string.
Returns an empty list if no matches are found.

**Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `pattern` | string | required | RE2 regular expression pattern. |
| `s` | string | required | String to search. |

**Returns:** List of match strings. Empty list if no matches.

**Errors:** Fails if the pattern is not a valid RE2 expression.

**Example:**

```python
regex.find_all("[0-9]+", "abc123def456ghi789")
# ["123", "456", "789"]
```

---

#### regex.find_groups

```python
regex.find_groups(pattern, s)
```

Returns the capture groups (excluding group 0 / full match) from the first
match, or None if no match is found.

**Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `pattern` | string | required | RE2 regular expression with capture groups. |
| `s` | string | required | String to search. |

**Returns:** List of capture group strings from the first match, or `None`.

**Errors:** Fails if the pattern is not a valid RE2 expression.

**Example:**

```python
regex.find_groups("(\\w+)@(\\w+)", "user@host")
# ["user", "host"]

# Extract account ID and resource type from an AWS ARN
regex.find_groups("arn:aws:[^:]+:[^:]*:(\\d+):(.+)", arn)
# ["123456789012", "role/my-role"]
```

---

#### regex.replace

```python
regex.replace(pattern, s, replacement)
```

Replace the first match of the pattern in the string with the replacement.
Supports `$1` backreferences to capture groups in the replacement string.
Returns the original string unchanged if no match is found.

**Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `pattern` | string | required | RE2 regular expression pattern. |
| `s` | string | required | String to search. |
| `replacement` | string | required | Replacement string. Supports `$1`, `$2`, etc. backreferences. |

**Returns:** String with the first match replaced.

**Errors:** Fails if the pattern is not a valid RE2 expression.

**Example:**

```python
regex.replace("world", "hello world world", "starlark")
# "hello starlark world"

# Backreference: swap first two words
regex.replace("(\\w+) (\\w+)", "hello world", "$2 $1")
# "world hello"
```

---

#### regex.replace_all

```python
regex.replace_all(pattern, s, replacement)
```

Replace all matches of the pattern in the string with the replacement. Supports
`$1` backreferences to capture groups.

**Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `pattern` | string | required | RE2 regular expression pattern. |
| `s` | string | required | String to search. |
| `replacement` | string | required | Replacement string. Supports `$1`, `$2`, etc. backreferences. |

**Returns:** String with all matches replaced.

**Errors:** Fails if the pattern is not a valid RE2 expression.

**Example:**

```python
regex.replace_all("[0-9]+", "a1b2c3", "X")
# "aXbXcX"
```

---

#### regex.split

```python
regex.split(pattern, s)
```

Split the string on all matches of the pattern. Returns a list of the resulting
substrings.

**Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `pattern` | string | required | RE2 regular expression pattern to split on. |
| `s` | string | required | String to split. |

**Returns:** List of strings.

**Errors:** Fails if the pattern is not a valid RE2 expression.

**Example:**

```python
regex.split("[,;]+", "a,b;;c,d")
# ["a", "b", "c", "d"]
```

---

### yaml

The `yaml` module provides K8s-compatible YAML encoding and decoding using
[sigs.k8s.io/yaml](https://pkg.go.dev/sigs.k8s.io/yaml). Encoding produces
sorted keys and block style by default. Decoding uses a YAML-to-JSON-to-Starlark
pipeline to guarantee identical type mapping with `json.decode`.

#### yaml.encode

```python
yaml.encode(value)
```

Encode a Starlark value as a YAML string. Produces K8s-compatible output with
sorted keys and block style. The trailing newline is trimmed for cleaner string
comparisons.

**Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `value` | any | required | Starlark value to encode. Dict keys must be strings. |

**Returns:** YAML string (sorted keys, no trailing newline).

**Errors:** Fails if the value contains unsupported types or non-string dict keys.

**Example:**

```python
yaml.encode({"apiVersion": "v1", "kind": "ConfigMap", "data": {"key": "value"}})
# "apiVersion: v1\ndata:\n  key: value\nkind: ConfigMap"
```

---

#### yaml.decode

```python
yaml.decode(s)
```

Decode a single-document YAML string into a Starlark value. Uses a
YAML-to-JSON-to-`json.decode` pipeline internally, which guarantees identical
type mapping with `json.decode` (JSON numbers become ints or floats, etc.).

**Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `s` | string | required | YAML string to decode. |

**Returns:** Starlark value.

**Errors:** Fails if the string is not valid YAML.

**Example:**

```python
data = yaml.decode("apiVersion: v1\nkind: ConfigMap\ndata:\n  key: value")
data["kind"]         # "ConfigMap"
data["data"]["key"]  # "value"
```

---

#### yaml.decode_stream

```python
yaml.decode_stream(s)
```

Decode a multi-document YAML string (documents separated by `---`) into a list
of Starlark values. Empty documents are skipped.

**Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `s` | string | required | Multi-document YAML string. |

**Returns:** List of Starlark values (one per YAML document).

**Errors:** Fails if any document is not valid YAML.

**Example:**

```python
docs = yaml.decode_stream("name: a\n---\nname: b\n---\nname: c")
len(docs)       # 3
docs[0]["name"]  # "a"
docs[2]["name"]  # "c"
```

---

## See also

- [Features guide](features.md) -- Detailed behavior for depends_on creation
  sequencing, labels auto-injection, connection details, skip_resource,
  observability metrics, metadata & observed access builtins, namespace modules,
  and schema validation
- [Standard library reference](stdlib-reference.md) -- Additional utility
  functions for networking, naming, labels, and conditions (loaded via
  short-form `load("starlark-stdlib:v1/naming.star", ...)` when a default
  registry is configured, or explicit `load("oci://...", ...")`)
- [Migration from KCL](migration-from-kcl.md) -- Concept mapping from KCL to
  function-starlark, including side-by-side examples
- [Migration cheatsheet](migration-cheatsheet.md) -- Sprig/KCL to
  function-starlark helper mapping
