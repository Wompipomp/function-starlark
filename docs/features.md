# Features guide

Beyond basic `Resource()` calls, function-starlark provides dependency
ordering, automatic labels, connection details, resource skipping, extra
resources, and observability metrics.

## Dependency ordering (depends_on)

The `depends_on` kwarg on `Resource()` creates Crossplane
[Usage](https://docs.crossplane.io/latest/concepts/usages/) resources that
enforce creation and deletion ordering between composed resources.

### How to use

Pass `ResourceRef` objects (returned by `Resource()`), resource name strings,
or `(ref, "field.path")` tuples to the `depends_on=` kwarg:

```python
db_ref = Resource("database", {
    "apiVersion": "rds.aws.upbound.io/v1beta1",
    "kind": "Instance",
    "spec": {"forProvider": {"region": region, "engine": "postgres"}},
})

schema_ref = Resource("schema", {
    "apiVersion": "postgresql.sql.crossplane.io/v1alpha1",
    "kind": "Database",
    "spec": {"forProvider": {"database": "mydb"}},
}, depends_on=[db_ref])

Resource("app", {
    "apiVersion": "kubernetes.crossplane.io/v1alpha1",
    "kind": "Object",
    "spec": {"forProvider": {"manifest": {"kind": "Deployment"}}},
}, depends_on=[schema_ref])
```

### Creation sequencing

When `depends_on` targets a resource that does not yet exist in observed state,
function-starlark **defers** the dependent resource. It returns a response with
a configurable TTL (default 10s via `spec.sequencingTTL`) so Crossplane
retries. On the next reconciliation, if the dependency exists in observed state,
the dependent resource is emitted.

This means resources are created in dependency order across reconciliation
cycles. A Warning event is emitted for each deferred resource (e.g., "waiting
for database").

### Field path readiness

By default, `depends_on` checks whether the dependency exists in observed state.
For resources wrapped in a `kubernetes.crossplane.io Object`, the outer Object
may appear in observed state before the inner resource's status fields are
populated. Use tuple syntax to wait for a specific field:

```python
# The Object is observed, but the inner WizEntraIDGroup may not have
# its objectId yet. Tuple syntax defers until the field is truthy.
group = Resource("group", object_wrapping_entra_group)
Resource("mapping", saml_mapping_body, depends_on=[
    (group, "status.atProvider.manifest.status.atProvider.objectId"),
])
```

A field path is a dot-separated string evaluated on the observed resource's full
struct. The dependent is deferred when:

- The dependency resource does not exist in observed state, OR
- The field path resolves to a missing key, null, empty string, zero, or false

The dependent is allowed when the field resolves to a non-empty string, non-zero
number, true, struct, or list.

```yaml
apiVersion: starlark.fn.crossplane.io/v1alpha1
kind: StarlarkInput
spec:
  sequencingTTL: "30s"   # default: 10s -- increase for slow-provisioning resources
  source: |
    # ...
```

### Usage resources

For each `depends_on` relationship, a Usage resource is automatically created
with `replayDeletion: true`, ensuring deletion happens in reverse dependency
order. The `spec.usageAPIVersion` field (or `STARLARK_USAGE_API_VERSION` env var)
controls which Usage API version is used. Default is `"v2"`
(`protection.crossplane.io/v1beta1`, Crossplane 2.x). Set to `"v1"` for
Crossplane 1.x (`apiextensions.crossplane.io/v1beta1`).

A single summary Warning event is emitted when Usage resources are generated,
reminding that `compositeDeletePolicy: Foreground` is needed on the XRD for
proper deletion ordering.

## Labels

### Auto-injection

By default, every `Resource()` call auto-injects Crossplane traceability labels:

- `crossplane.io/composite` -- the XR name
- `crossplane.io/claim-name` -- the claim name (if a Claim exists)
- `crossplane.io/claim-namespace` -- the claim namespace (if a Claim exists)

Claim labels are only injected when claim metadata exists in the XR, so direct
XR usage without a Claim works without errors.

```python
# Auto-injection happens automatically -- no code needed
Resource("bucket", {
    "apiVersion": "s3.aws.upbound.io/v1beta1",
    "kind": "Bucket",
    "spec": {"forProvider": {"region": "us-east-1"}},
})
# Result labels include: crossplane.io/composite, crossplane.io/claim-name, etc.
```

### Three-way merge

When the `labels=` kwarg is provided, labels are merged with this priority
(lowest to highest):

1. `body` `metadata.labels` (from the resource dict)
2. Auto-injected Crossplane labels
3. `labels=` kwarg (user labels always win)

```python
Resource("bucket", {
    "apiVersion": "s3.aws.upbound.io/v1beta1",
    "kind": "Bucket",
    "metadata": {"labels": {"from-body": "true"}},
    "spec": {"forProvider": {"region": "us-east-1"}},
}, labels={"team": "platform", "env": "prod"})
# Result: body labels + crossplane auto-labels + {"team": "platform", "env": "prod"}
```

### Opt-out

Pass `labels=None` to skip all auto-injection. Only labels present in the body
dict are preserved:

```python
Resource("bucket", {
    "apiVersion": "s3.aws.upbound.io/v1beta1",
    "kind": "Bucket",
    "metadata": {"labels": {"keep-only-this": "true"}},
    "spec": {"forProvider": {"region": "us-east-1"}},
}, labels=None)
# Result: only {"keep-only-this": "true"} -- no crossplane labels injected
```

### Conflict warnings

A Warning event is emitted when a `labels=` kwarg key conflicts with an
auto-injected Crossplane label key (e.g., if you set
`crossplane.io/composite` in the `labels=` kwarg). This is usually a mistake
-- let auto-injection handle Crossplane labels.

## Connection details

### Per-resource

Pass `connection_details={"key": "value"}` to `Resource()` for connection
details associated with a specific composed resource:

```python
Resource("database", {
    "apiVersion": "rds.aws.upbound.io/v1beta1",
    "kind": "Instance",
    "spec": {"forProvider": {"region": region}},
}, connection_details={
    "host": "db.example.com",
    "port": "5432",
    "username": "admin",
})
```

### XR-level

Call `set_connection_details()` for connection details associated with the
composite resource itself:

```python
set_connection_details({
    "region": region,
    "endpoint": "https://api.example.com",
})
```

Both patterns can be used together. Per-resource connection details are tied to
specific composed resources; XR-level connection details are tied to the
composite.

## Resource skipping (skip_resource)

The `skip_resource(name, reason)` builtin removes a resource from the desired
state with a reason. This is useful in multi-step pipelines where a prior step
added a resource that this step wants to conditionally remove.

```python
skip_resource("old-bucket", "Migrated to new storage backend")
```

A Warning event is emitted for each skipped resource, and the
`function_starlark_resources_skipped_total` counter is incremented.

## Conditions and events

### set_condition

Sets a condition on the XR or a composed resource:

```python
set_condition("Ready", "True", "Available", "All 10 resources created in us-east-1")
```

Parameters: `type`, `status`, `reason`, `message`, and optionally
`target="Composite"` (default) or `target="resource-name"`.

### emit_event

Emits a Kubernetes event visible in `kubectl describe` output:

```python
emit_event("Normal", "Composition reconciled successfully")
emit_event("Warning", "Database replica lagging by 30 seconds")
```

The `severity` must be `"Normal"` or `"Warning"`.

### fatal

Halts execution with a fatal error condition set on the XR:

```python
fatal("Missing required spec.region field")
```

### Typical end-of-script pattern

```python
count = 10
set_condition("Ready", "True", "Available", "Created %d resources" % count)
emit_event("Normal", "Composition reconciled: %d resources in %s" % (count, region))
```

## Extra resources

Read existing cluster resources during composition using the extra resources
API.

### require_extra_resource

Request a single resource by name or labels:

```python
require_extra_resource("vpc", "ec2.aws.upbound.io/v1beta1", "VPC", match_name="my-vpc")
```

### require_extra_resources

Request multiple resources by label selector:

```python
require_extra_resources("subnets", "ec2.aws.upbound.io/v1beta1", "Subnet",
    match_labels={"network": "main"})
```

### Accessing extra resources

Required resources are available via the `extra_resources` global dict:

```python
require_extra_resource("vpc", "ec2.aws.upbound.io/v1beta1", "VPC", match_name="my-vpc")

# Access the result (available after Crossplane fulfills the requirement)
vpc = extra_resources.get("vpc")
if vpc:
    vpc_cidr = get(vpc[0], "spec.forProvider.cidrBlock", "10.0.0.0/16")
    Resource("subnet", {
        "apiVersion": "ec2.aws.upbound.io/v1beta1",
        "kind": "Subnet",
        "spec": {"forProvider": {"vpcId": get(vpc[0], "metadata.name"), "cidrBlock": vpc_cidr}},
    })
```

Use case: Reading existing resources to derive configuration (e.g., reading a
VPC to get its CIDR, reading a cluster to get its endpoint).

## Metadata and observed access

function-starlark provides convenience builtins that replace common multi-step
patterns for reading metadata and observed resource state.

### Label and annotation access

The `get_label()` and `get_annotation()` builtins safely read individual label
and annotation values from any resource dict. Unlike `get()`, they handle
dotted keys correctly -- `get_label(oxr, "app.kubernetes.io/name")` looks up
the literal key `app.kubernetes.io/name` in the labels map, instead of
splitting on dots and traversing nested dicts.

```python
# Safe dotted-key access -- returns "unknown" if the label is missing
managed_by = get_label(oxr, "app.kubernetes.io/managed-by", "unknown")

# Annotation access works the same way
ext_name = get_annotation(oxr, "crossplane.io/external-name", "")
```

Both builtins return the default value when the key, the labels/annotations
map, or metadata itself is missing. They work identically on `oxr` and
`observed` resource dicts.

### XR status writes

The `set_xr_status()` builtin writes values into `dxr["status"]` at arbitrary
dot-paths without manually creating intermediate dicts:

```python
# Write related fields at a prefix
set_xr_status("atProvider", {"bucketCount": 8, "environment": env})

# Write an individual field at a separate path
set_xr_status("region", region)
```

Intermediate dicts are auto-created when path segments do not exist. Sibling
fields are preserved -- writing `set_xr_status("atProvider.arn", arn)` does not
clobber an existing `atProvider.projectId`.

### Observed resource access

The `get_observed()` builtin reads fields from observed resources in a single
call, replacing the two-step existence-check-then-get pattern:

```python
# One-call pattern with default for missing resources
bucket_arn = get_observed("bucket-0", "status.atProvider.arn", "pending")
```

Returns the default when the resource does not exist in observed state (common
on initial reconciliation) or when the path does not exist within the resource.

For full signatures and parameters, see the
[builtins reference](builtins-reference.md).

## Schema validation

function-starlark provides opt-in schema validation via `schema()` and `field()`
builtins. Define typed constructors that validate field types, required fields,
enum values, and unknown field names at construction time -- catching errors
before `Resource()` is called.

### Opt-in adoption

Schemas are fully opt-in. You can mix schema-validated resources and plain dict
resources in the same composition. Adopt gradually by adding schemas to the
resources where you want type safety -- there is no all-or-nothing requirement.

### Validation features

Schema constructors catch mistakes at construction time:

- **Wrong types** -- passing an int where a string is expected
- **Missing required fields** -- omitting a field marked `required=True`
- **Unknown fields** -- typos like `locaton` instead of `location`, with
  did-you-mean suggestions
- **Invalid enum values** -- passing `"LRSS"` when only `["LRS", "GRS", "ZRS",
  "GZRS"]` are allowed

All errors are reported at once (not fail-on-first), so you can fix everything
in a single iteration.

### Nested schemas

Schemas can reference other schemas for nested validation. Use `field(type=SubSchema)`
for a nested object and `field(type="list", items=SubSchema)` for a list of
typed objects. Validation errors include the full field path (e.g.,
`network_rules.default_action`).

```python
NetworkRules = schema("NetworkRules",
    default_action=field(type="string", enum=["Allow", "Deny"]),
    bypass=field(type="list"),
)

StorageAccountSpec = schema("StorageAccountSpec",
    location=field(type="string", required=True),
    network_rules=field(type=NetworkRules),
)

sa = StorageAccountSpec(
    location="eastus",
    network_rules=NetworkRules(default_action="Deny", bypass=["AzureServices"]),
)
Resource("storage-account", {
    "apiVersion": "storage.azure.upbound.io/v1beta2",
    "kind": "Account",
    "spec": {"forProvider": sa},
})
```

For full signatures and parameter tables, see the
[builtins reference](builtins-reference.md).

### Provider schema packages with namespace aliases

When loading generated schema packages that export many types (e.g., Azure
provider schemas with different `Account` types across API groups), use
namespace alias imports to avoid name conflicts:

```python
load("oci://ghcr.io/wompipomp/schemas-azure:v2.5.0/storage/v1.star", storage="*")
load("oci://ghcr.io/wompipomp/schemas-azure:v2.5.0/cosmosdb/v1.star", cosmosdb="*")

sa = storage.Account(location="eastus", account_replication_type="LRS")
db = cosmosdb.Account(location="eastus", kind="GlobalDocumentDB")
```

Each namespace struct keeps its provider's types separate. See the
[module system guide](module-system.md#namespace-alias-imports) for full syntax.

## Observability (metrics)

function-starlark exposes 9 Prometheus metrics on the standard `/metrics`
endpoint:

| Metric | Type | Description |
|--------|------|-------------|
| `function_starlark_execution_duration_seconds` | Histogram | Starlark script execution time |
| `function_starlark_reconciliation_duration_seconds` | Histogram | Full RunFunction handler duration |
| `function_starlark_oci_resolve_duration_seconds` | Histogram | OCI module tag-to-digest resolution time |
| `function_starlark_cache_hits_total` | Counter | Bytecode cache hits |
| `function_starlark_cache_misses_total` | Counter | Bytecode cache misses |
| `function_starlark_resources_emitted_total` | Counter | Composed resources emitted |
| `function_starlark_resources_skipped_total` | Counter | Resources skipped via skip_resource() |
| `function_starlark_reconciliations_total` | Counter | RunFunction invocations |
| `function_starlark_resources_deferred_total` | Counter | Resources deferred by creation sequencing |

### Scraping metrics

Metrics are served by the function-sdk-go metrics server. To scrape them, use a
Prometheus ServiceMonitor or scrape config targeting the function pod:

```yaml
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: function-starlark
spec:
  selector:
    matchLabels:
      pkg.crossplane.io/function: function-starlark
  endpoints:
    - port: metrics
```

For deployment and metrics collection setup, see the
[deployment guide](deployment-guide.md).

## Namespace modules

function-starlark provides six namespace modules as predeclared globals --
`json`, `crypto`, `encoding`, `dict`, `regex`, and `yaml`. They are available
in every script without any `load()` statement.

### JSON (json.*)

The `json` module comes from
[go.starlark.net/lib/json](https://pkg.go.dev/go.starlark.net/lib/json) and is
predeclared -- no `load()` required. It encodes and decodes JSON from Starlark
values.

```python
# Round-trip: dict -> JSON string -> dict
data = {"apiVersion": "v1", "kind": "ConfigMap"}
encoded = json.encode(data)       # '{"apiVersion":"v1","kind":"ConfigMap"}'
decoded = json.decode(encoded)    # {"apiVersion": "v1", "kind": "ConfigMap"}

# Pretty-printed JSON for ConfigMap data fields
config = {"logging": {"level": "info", "format": "json"}, "replicas": 3}
Resource("config", {
    "apiVersion": "v1",
    "kind": "ConfigMap",
    "metadata": {"name": "app-config"},
    "data": {"config.json": json.encode_indent(config)},
})
```

The `json` module follows upstream go.starlark.net semantics exactly.
`json.indent(s)` reformats an already-encoded JSON string without
re-encoding.

### Crypto (crypto.*)

The `crypto` module provides hashing functions and deterministic ID generation.

```python
# Hash data for integrity checks or annotation values
digest = crypto.sha256("sensitive-data")   # hex digest string

# Deterministic short ID from composite inputs -- same every reconciliation
xr_name = get(oxr, "metadata.name", "unknown")
suffix = crypto.stable_id("bucket-" + xr_name, length=8)
Resource("bucket", {
    "apiVersion": "s3.aws.upbound.io/v1beta1",
    "kind": "Bucket",
    "metadata": {"name": "data-%s" % suffix},
    "spec": {"forProvider": {"region": "us-east-1"}},
})
```

`md5` is provided for non-cryptographic use only (checksums, cache keys).
`stable_id` generates a deterministic lowercase alphanumeric string from a
seed; the `length` parameter accepts values from 1 to 64 (default 8).
`hmac_sha256(key, msg)` produces an HMAC-SHA256 hex digest. `blake3(data)`
produces a BLAKE3 hex digest.

### Encoding (encoding.*)

The `encoding` module provides base64, base32, and hex encode/decode functions.

```python
# Base64-encode a value for Kubernetes Secret data
secret_value = encoding.b64enc("my-database-password")
Resource("db-secret", {
    "apiVersion": "v1",
    "kind": "Secret",
    "metadata": {"name": "db-credentials"},
    "data": {"password": secret_value},
})

# URL-safe base64 for tokens or identifiers
token = encoding.b64url_enc("user:session:12345")
```

`b64url_enc` / `b64url_dec` use the URL-safe alphabet without padding.
`b32enc` / `b32dec` use standard RFC 4648 encoding without padding.
`hex_enc` / `hex_dec` convert between raw strings and hexadecimal.

### Dict operations (dict.*)

The `dict` module provides merge, deep-merge, pick, omit, and path-based
lookup operations.

```python
# Deep-merge platform defaults with user overrides
defaults = {
    "region": "us-east-1",
    "tags": {"managed-by": "crossplane", "env": "dev"},
    "networking": {"vpcCidr": "10.0.0.0/16", "subnetBits": 8},
}
user = get(oxr, "spec.parameters", {})
merged = dict.deep_merge(defaults, user)
# user's tags merge INTO defaults.tags -- both dicts are unchanged

# Safe nested access into Kubernetes objects
containers = dict.dig(body, "spec.template.spec.containers", default=[])
```

`deep_merge` is recursive with right-wins semantics and returns a new dict
without mutating either input. `merge` is shallow (top-level keys only) with
right-wins semantics. Both require 2 or more arguments. `pick(d, keys)` and
`omit(d, keys)` return new dicts with only the selected or excluded keys.
`has_path(d, path)` checks whether a dotted path exists.

### Regex (regex.*)

The `regex` module provides RE2 regular expression operations using Go RE2
syntax.

```python
# Extract account ID and role name from an AWS ARN
arn = get_observed("role", "status.atProvider.arn", "")
groups = regex.find_groups(r"^arn:aws:iam::(\d+):role/(.+)$", arn)
if groups:
    account_id = groups[0]
    role_name = groups[1]

# Replace version numbers in a string
sanitized = regex.replace_all(r"\d+", version_string, "X")
```

Compiled patterns are cached with LRU eviction for performance.
`replace` and `replace_all` support `$1`-style backreferences in the
replacement string. `find_groups` returns capture group strings from the
first match, or `None` if no match. `match` returns a boolean. `split`
splits a string around pattern matches.

### YAML (yaml.*)

The `yaml` module provides Kubernetes-compatible YAML encode/decode via
sigs.k8s.io/yaml.

```python
# Generate a YAML string for embedding in a ConfigMap
config_dict = {"logging": {"level": "info"}, "replicas": 3}
yaml_str = yaml.encode(config_dict)

# Parse a multi-document YAML string into a list of dicts
multi_doc = """
apiVersion: v1
kind: ConfigMap
---
apiVersion: v1
kind: Secret
"""
docs = yaml.decode_stream(multi_doc)
# docs is a list of two dicts
```

Type mapping matches `json.decode` for consistency (numbers become int or
float, booleans become True/False). Keys are sorted in output. `yaml.encode`
produces no trailing newline.

### Observed and extra-resource helpers

v1.8 adds `is_observed()`, `observed_body()`, `get_extra_resource()`,
`get_extra_resources()`, and `get_condition()` as flat builtins that replace
common multi-step patterns for accessing observed state and extra resources.

```python
# First-reconcile safety: branch on resource existence
if is_observed("database"):
    db_host = get_observed("database", "status.atProvider.address", "")
else:
    db_host = "pending"

# Get the full observed body as a dict
db = observed_body("database", default={})

# Read extra resource fields in one call
cluster_region = get_extra_resource("cluster", "spec.region", "us-west-2")

# Check a condition on an observed resource
cond = get_condition("database", "Ready")
if cond and cond["status"] == "True":
    # database is ready
    pass
```

`get_condition` returns a new unfrozen dict with 4 keys (`status`, `reason`,
`message`, `lastTransitionTime`). Missing fields default to empty string.
Returns `None` when the resource or condition type is not found.

For full signatures and parameters, see the
[builtins reference](builtins-reference.md).

### Response TTL (set_response_ttl)

The `set_response_ttl()` builtin provides user-controlled requeue intervals,
overriding the default `sequencingTTL` from the StarlarkInput spec.

```python
# Adjust polling frequency based on resource state
if is_observed("database"):
    cond = get_condition("database", "Ready")
    if cond and cond["status"] == "True":
        set_response_ttl("5m")  # slow poll when ready
    else:
        set_response_ttl("10s")  # fast poll while provisioning
else:
    set_response_ttl("10s")  # fast poll on first reconcile
```

Accepts Go duration strings (`"30s"`, `"5m"`, `"1h"`) or integer seconds.
The last call wins if `set_response_ttl` is called multiple times. Use this
to reduce API load for stable resources or speed up convergence during
provisioning.

## See also

- [Builtins reference](builtins-reference.md) -- complete function signatures
  for all builtins and namespace modules
- [Best practices](best-practices.md) -- composition patterns, label strategy,
  and testing guidance
- [Migration cheatsheet](migration-cheatsheet.md) -- Sprig/KCL to
  function-starlark helper mapping
- [Deployment guide](deployment-guide.md) -- cluster deployment and metrics
  collection
