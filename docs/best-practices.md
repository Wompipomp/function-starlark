# Best practices

Patterns and recommendations for writing maintainable, testable Crossplane
compositions with function-starlark.

## Composition patterns

### Pattern: Extract-Transform-Emit

The standard composition structure. Read inputs, build configurations, emit
resources:

```python
# 1. Extract: Read input from XR
region = get(oxr, "spec.region", "us-east-1")
env = get(oxr, "spec.environment", "dev")

# 2. Transform: Build resource configurations
bucket_config = {
    "apiVersion": "s3.aws.upbound.io/v1beta1",
    "kind": "Bucket",
    "spec": {"forProvider": {"region": region}},
}

# 3. Emit: Register resources
Resource("bucket", bucket_config)
```

### Pattern: Conditional resources

Prefer `Resource(..., when=..., skip_reason=...)` over `if`-wrapped emissions
for conditional resources. `when=` makes the skip observable as a Warning
event and -- by default -- gates the XR's Ready condition to False until the
condition becomes true. That prevents claims from flipping to Ready
prematurely while a dependency is still pending.

```python
# GATES the XR -- XR stays Ready=False until the cluster is ready.
Resource("db-replica", replica_body,
    when=cluster_ready,
    skip_reason="waiting for cluster to provision")
```

For resources that are *expected* to be absent under some configurations
(feature flags, tier-gated add-ons, environment opt-ins), use
`optional=True` so the absence does not block the XR:

```python
Resource("monitoring", {
    "apiVersion": "monitoring.example.io/v1",
    "kind": "Dashboard",
    "spec": {"forProvider": {"region": region, "enabled": True}},
}, when=env == "prod",
   skip_reason="monitoring only in prod",
   optional=True)
```

Plain `if` guards are still fine for purely local control flow that does not
map to a "resource missing by design" semantic, but they leave no audit trail
and no readiness signal. See
[features.md#composite-readiness-gating](features.md#composite-readiness-gating)
for the full model.

### Pattern: Loop resources with refs

Capture `ResourceRef` objects for dependency chains:

```python
bucket_refs = []
for i in range(count):
    ref = Resource("bucket-%d" % i, {
        "apiVersion": "s3.aws.upbound.io/v1beta1",
        "kind": "Bucket",
        "metadata": {"name": "%s-bucket-%d" % (xr_name, i)},
        "spec": {"forProvider": {"region": region}},
    })
    bucket_refs.append(ref)

# Aggregator depends on all buckets
Resource("aggregator", {
    "apiVersion": "lambda.aws.upbound.io/v1beta1",
    "kind": "Function",
    "spec": {"forProvider": {"region": region}},
}, depends_on=bucket_refs)
```

### Pattern: Helper functions

Extract repeated logic into functions at the top of the script:

```python
def make_bucket(name, region, tags={}):
    return {
        "apiVersion": "s3.aws.upbound.io/v1beta1",
        "kind": "Bucket",
        "metadata": {"name": name},
        "spec": {"forProvider": {"region": region, "tags": tags}},
    }

Resource("data", make_bucket("data-bucket", region))
Resource("logs", make_bucket("logs-bucket", region, tags={"Purpose": "logging"}))
```

### Pattern: Status reporting

Always set a condition and emit a summary event at the end of the script:

```python
count = 10
set_condition("Ready", "True", "Available", "Created %d resources" % count)
emit_event("Normal", "Composition reconciled successfully")
```

This provides visibility into composition health via `kubectl describe` and
XR status conditions.

For writing structured status fields, use `set_xr_status()` instead of direct
`dxr["status"]` assignment. It auto-creates intermediate dicts and preserves
sibling fields:

```python
set_xr_status("atProvider.projectId", project_id)
set_xr_status("atProvider.arn", arn)
set_xr_status("region", region)
```

## Label strategy

### Default: Auto-injection

Let auto-injection handle `crossplane.io/*` labels. Do not manually set them
-- function-starlark injects `crossplane.io/composite`, `crossplane.io/claim-name`,
and `crossplane.io/claim-namespace` automatically on every `Resource()` call.

### Reading labels and annotations

To read labels with dotted keys like `app.kubernetes.io/name`, use
`get_label()` instead of `get()` which splits on dots:

```python
# Correct -- looks up the literal key in the labels map
name = get_label(oxr, "app.kubernetes.io/name", "unknown")

# Also works for annotations
ext_name = get_annotation(oxr, "crossplane.io/external-name", "")
```

Both return the default when the key, labels/annotations map, or metadata is
missing. See the [builtins reference](builtins-reference.md#get_label) for
full details.

### Custom labels

Use the `labels=` kwarg for team, cost-center, or environment labels that
should apply to all resources:

```python
common_labels = {"team": "platform", "cost-center": "eng-123", "env": env}

Resource("bucket", {...}, labels=common_labels)
Resource("topic", {...}, labels=common_labels)
```

### Body labels

Use `body` `metadata.labels` for resource-specific labels that vary per
resource (e.g., index labels in a loop):

```python
for i in range(count):
    Resource("bucket-%d" % i, {
        "apiVersion": "s3.aws.upbound.io/v1beta1",
        "kind": "Bucket",
        "metadata": {"labels": {"index": str(i)}},
        "spec": {"forProvider": {"region": region}},
    }, labels=common_labels)
    # Result: index label from body + common labels from kwarg + crossplane auto-labels
```

### Opt-out

Use `labels=None` only when you need exact control over labels -- for example,
when migrating existing resources that must not gain new labels:

```python
Resource("legacy-bucket", {...}, labels=None)
```

### Collision warning

If your `labels=` kwarg uses a key that collides with `crossplane.io/*`, a
Warning event is emitted. This is usually a mistake -- let auto-injection
handle Crossplane labels.

## Dependency patterns

### Simple chain

A depends on B:

```python
b_ref = Resource("database", {...})
Resource("schema", {...}, depends_on=[b_ref])
```

### Fan-out

Multiple resources depend on one parent:

```python
parent_ref = Resource("vpc", {...})
Resource("subnet-a", {...}, depends_on=[parent_ref])
Resource("subnet-b", {...}, depends_on=[parent_ref])
Resource("subnet-c", {...}, depends_on=[parent_ref])
```

### Fan-in

One resource depends on many:

```python
ref1 = Resource("subnet-a", {...})
ref2 = Resource("subnet-b", {...})
ref3 = Resource("subnet-c", {...})
Resource("route-table", {...}, depends_on=[ref1, ref2, ref3])
```

### Field path readiness (Object wrappers)

When a resource is wrapped in a `kubernetes.crossplane.io Object`, the Object
appears in observed state before the inner resource has its status populated.
Use tuple syntax to wait for a specific field instead of manual observed-state
guards:

```python
# Instead of:
#   group_oid = get(observed, "group.status.atProvider.manifest.status.atProvider.objectId", "")
#   if group_oid:
#       Resource("mapping", {...})

# Use tuple syntax:
group = Resource("group", object_body)
Resource("mapping", {
    "spec": {"forProvider": {"groupId": get(observed, "group.status.atProvider.manifest.status.atProvider.objectId", "")}},
}, depends_on=[(group, "status.atProvider.manifest.status.atProvider.objectId")])
```

This is cleaner and ensures the SAML mapping is deferred until the field is
truthy, while still generating Usage resources for deletion ordering.

### No circular dependencies

function-starlark detects cycles in the dependency graph and reports a fatal
error. Ensure your dependency graph is a DAG.

### Tuning sequencingTTL

The default 10s TTL works for most resources. Increase for slow-provisioning
resources (e.g., RDS instances, EKS clusters):

```yaml
apiVersion: starlark.fn.crossplane.io/v1alpha1
kind: StarlarkInput
spec:
  sequencingTTL: "60s"    # default: 10s
```

## Testing with crossplane render

The `crossplane render` CLI runs compositions locally against a function Docker
image without needing a Kubernetes cluster. This is the primary testing
workflow for function-starlark compositions.

### Setup

1. Build the function Docker image:

```bash
make build
```

2. Create example fixtures. The project's own [example/](../example/) directory
   is a working template:

- `example/xr.yaml` -- sample XR input
- `example/composition.yaml` -- composition to test
- `example/functions.yaml` -- function reference with Docker runtime
- `example/expected-output.yaml` -- expected render output

### Run

```bash
crossplane render example/xr.yaml example/composition.yaml example/functions.yaml
```

Use `--include-function-results` to see events and results in the output:

```bash
crossplane render example/xr.yaml example/composition.yaml example/functions.yaml \
  --include-function-results
```

### Automated regression testing

Use `make render-check` to diff render output against expected output. Add this
to your CI pipeline:

```bash
make render-check
```

This builds the Docker image, runs `crossplane render`, and diffs the output
against `example/expected-output.yaml`. Any unexpected change causes a failure.

### Tips

- When updating compositions, update `expected-output.yaml` to match. The diff
  shows you exactly what changed.
- Keep fixture XRs minimal -- test one pattern per XR, not every feature at
  once.
- Use the existing [example/](../example/) directory as a starting template
  for your own composition tests.

## Module organization

| Composition size | Recommendation |
|------------------|----------------|
| Small (< 100 lines) | Inline source is fine |
| Medium (100-300 lines) | Extract helpers into inline modules (`spec.modules`) |
| Large (300+ lines) or shared | Package as OCI modules -- see [OCI module distribution](oci-module-distribution.md) |

Use standard library modules for common patterns (networking, naming, labels,
conditions) rather than reimplementing. See the
[standard library reference](stdlib-reference.md).

### Namespace aliases for provider schemas

When multiple schema packages export the same type name (common with cloud
providers that define `Account`, `Network`, or `Subnet` across API groups),
use namespace alias imports to avoid name conflicts:

```python
# Problem: both modules export "Account" -- flat star imports clash
# load("schemas-azure:v2.5.0/storage/v1.star", "*")
# load("schemas-azure:v2.5.0/cosmosdb/v1.star", "*")

# Solution: namespace alias imports keep each provider's types separate
load("schemas-azure:v2.5.0/storage/v1.star", storage="*")
load("schemas-azure:v2.5.0/cosmosdb/v1.star", cosmosdb="*")

storage.Account(location="eastus", account_replication_type="LRS")
cosmosdb.Account(location="eastus", kind="GlobalDocumentDB")
```

Use one namespace per API group or provider package. This mirrors how Go and
Python organize types by package path and makes it clear which provider each
type belongs to.

## Error handling

Starlark has no `try/except`. Use defensive coding patterns:

### Safe nested access

Use `get()` with defaults instead of direct dict access:

```python
# Safe -- returns "us-east-1" if path does not exist
region = get(oxr, "spec.region", "us-east-1")

# Unsafe -- raises KeyError if spec or region is missing
region = oxr["spec"]["region"]
```

For observed resources, use `get_observed()` to avoid manual existence checks:

```python
# One call instead of checking "bucket" in observed first
arn = get_observed("bucket", "status.atProvider.arn", "pending")
```

### Check before access

Use `if "key" in dict:` before accessing optional fields:

```python
if "monitoring" in get(oxr, "spec", {}):
    Resource("dashboard", {...})
```

### Fail fast

Use `fatal()` for unrecoverable errors with clear messages:

```python
region = get(oxr, "spec.region")
if not region:
    fatal("spec.region is required but was not provided")
```

### Warn on recoverable issues

Use `emit_event("Warning", ...)` for situations that are not fatal but should
be visible:

```python
if count > 100:
    emit_event("Warning", "Creating %d resources -- consider splitting into smaller compositions" % count)
```

## Schema validation

### When to use schemas

Use schemas when field accuracy matters:

- **Production resources** -- storage accounts, databases, networking rules where
  a typo causes silent misconfiguration
- **Frequently-edited compositions** -- schemas catch regressions when multiple
  people modify the same composition
- **Resources with many similar field names** -- `accountTier` vs `accountKind`
  vs `accountReplicationType` are easy to confuse

Use plain dicts when the overhead is not worth it:

- **Simple resources** with 2-3 obvious fields
- **Prototyping** -- schemas can be added later without changing resource output
- **Well-understood structures** that rarely change

### Opt-in adoption strategy

Start with your most error-prone resource. Add schemas incrementally -- you do
not need to schema-validate every resource in a composition. Schema-validated
and plain dict resources mix freely:

```python
# Schema-validated -- catches typos in storage account fields
sa = StorageAccountSpec(location=location, account_replication_type="LRS")
Resource("storage-account", {
    "spec": {"forProvider": sa},
    # ...
})

# Plain dict -- simple resource, schema not needed
Resource("resource-group", {
    "spec": {"forProvider": {"location": location}},
    # ...
})
```

### Schema composition patterns

Define sub-schemas for nested structures. Keep schema definitions at the top of
the script, before Extract-Transform-Emit:

```python
# 1. Schema definitions (top of script)
NetworkRules = schema("NetworkRules",
    default_action=field(type="string", enum=["Allow", "Deny"]),
)

StorageAccountSpec = schema("StorageAccountSpec",
    location=field(type="string", required=True),
    network_rules=field(type=NetworkRules),
)

# 2. Extract: Read input from XR
location = get(oxr, "spec.location", "eastus")

# 3. Transform + Emit: Build and register resources
sa = StorageAccountSpec(location=location, network_rules=NetworkRules(default_action="Deny"))
Resource("storage-account", {"spec": {"forProvider": sa}})
```

For shared schemas across compositions, schema definitions can be placed in
modules loaded via `load()`. See [module system](module-system.md) for details.

## v1.8 patterns

### Pattern: Deterministic resource naming with crypto

Non-deterministic names (randAlpha, UUID) cause resource churn across
reconciliation cycles. Use `crypto.stable_id()` for deterministic suffixes
derived from composite inputs.

```python
xr_name = get(oxr, "metadata.name", "unknown")
region = get(oxr, "spec.region", "us-east-1")

# Short deterministic ID from composite inputs -- same every reconciliation
suffix = crypto.stable_id(xr_name + "-" + region)
Resource("bucket", {
    "apiVersion": "s3.aws.upbound.io/v1beta1",
    "kind": "Bucket",
    "metadata": {"name": "data-%s" % suffix},
    "spec": {"forProvider": {"region": region}},
})
```

`stable_id` generates a deterministic lowercase alphanumeric ID from a seed.
The same seed always produces the same ID. Use it wherever you need a short
unique suffix derived from XR inputs. The `length` parameter controls output
(1-64 chars, default 8).

### Pattern: Deep-merging default configs

Platform defaults must merge recursively with user overrides without mutating
either dict. Use `dict.deep_merge()` for nested structures.

```python
defaults = {
    "region": "us-east-1",
    "tags": {"managed-by": "crossplane", "env": "dev"},
    "networking": {"vpcCidr": "10.0.0.0/16", "subnetBits": 8},
}
user = get(oxr, "spec.parameters", {})
merged = dict.deep_merge(defaults, user)
# user's tags merge INTO defaults.tags -- both dicts preserved
```

`deep_merge` recursively merges nested dicts with right-wins semantics. Both
inputs are unchanged. Use `dict.merge()` for shallow (top-level keys only)
merge.

### Pattern: Regex field extraction from ARNs and URIs

Extracting account IDs, regions, or resource names from AWS ARNs, Azure
resource IDs, or URIs requires fragile string splitting. Use
`regex.find_groups()` for structured extraction.

```python
arn = get_observed("role", "status.atProvider.arn", "")
groups = regex.find_groups(r"arn:aws:iam::(\d+):role/(.*)", arn)
if groups:
    account_id = groups[0]
    role_name = groups[1]
```

`find_groups` returns capture group strings from the first match, or `None`
if no match. Use `regex.match()` for boolean checks, `regex.replace_all()`
for transformations. Patterns use Go RE2 syntax (not PCRE).

### Pattern: Condition aggregation with stdlib

Checking readiness across multiple composed resources requires iterating
observed state and parsing condition arrays. The conditions stdlib simplifies
this to a single function call.

```python
load("starlark-stdlib:v1/conditions.star", "all_ready", "any_degraded", "degraded")

if any_degraded(["database", "cache"]):
    degraded("SubsystemFailing", "One or more data stores is not healthy")
elif all_ready():
    set_condition("Ready", "True", "Available", "All resources ready")
```

`all_ready()` returns `True` when every listed resource (or all observed, if
`None`) has `Ready=True`. `any_degraded()` returns `True` when any has
`Ready=False` or `Synced=False`. With `None` argument and zero observed
resources, `all_ready` returns `False` (first-reconcile safety).

### Pattern: First-reconcile safety with observed helpers

Accessing observed resources that do not exist yet (first reconciliation)
crashes the script. Use the v1.8 observed helpers for safe access.

```python
# Branch safely on existence
if is_observed("database"):
    db_host = get_observed("database", "status.atProvider.address", "")
    db_ready = get_condition("database", "Ready")
else:
    db_host = "pending"
    db_ready = None

# Or get the full body with a safe default
db = observed_body("database", default={})
```

`is_observed()` checks existence without field access. `observed_body()`
returns the full body dict or a default. `get_condition()` returns `None`
when the resource or condition is missing. All three are safe on first
reconciliation when observed is empty.

### Pattern: Custom requeue intervals with set_response_ttl

The default 10s requeue is too fast for slow-provisioning resources (RDS,
EKS) or too slow for time-sensitive operations. Use `set_response_ttl()` to
tune the interval based on resource state.

```python
# Fast polling while waiting for slow resource
if not is_observed("cluster"):
    set_response_ttl("15s")  # first reconcile -- medium poll
elif get_condition("cluster", "Ready") and get_condition("cluster", "Ready")["status"] != "True":
    set_response_ttl("30s")  # provisioning -- slower poll
else:
    set_response_ttl("5m")   # ready -- slow poll
```

`set_response_ttl()` overrides the default `sequencingTTL`. Accepts Go
duration strings (`"30s"`, `"5m"`) or int seconds. Last call wins if called
multiple times.

## v1.9 patterns

### Pattern: Optional fields with dict.compact

Replace manual None-guarding with recursive `dict.compact`. Set optional fields
to `None` and let compact prune them at any depth.

```python
# Before: manual None-guarding
spec = {
    "replicas": replicas,
}
if annotations:
    spec["metadata"] = {"annotations": annotations}
if volumes:
    spec["template"] = {"spec": {"volumes": volumes}}

# After: recursive dict.compact
spec = dict.compact({
    "replicas": replicas,
    "metadata": {
        "annotations": annotations if annotations else None,
    },
    "template": {
        "spec": {
            "volumes": volumes if volumes else None,
        },
    },
})
```

Empty strings, lists, and dicts are preserved -- these carry intent in
Kubernetes manifests (e.g., `resources: {}` means "no limits", not "omit the
field"). See [builtins reference](builtins-reference.md#dictcompact) for the
full signature and behavior details.

### Pattern: Gated / preservable resources

Three progressive patterns for controlling resource emission declaratively,
from simple conditional skipping to cliff-guard preservation.

**(a) Simple conditional emission with when/skip_reason:**

Skip a resource when a feature is disabled. Replaces wrapping `Resource()` in
`if/else` blocks with `skip_resource()`:

```python
# Skip resource when feature is disabled
feature_enabled = get(oxr, "spec.features.monitoring", False)
Resource("monitoring-stack", monitoring_body,
    when=feature_enabled, skip_reason="monitoring disabled in spec")
```

**(b) Cliff guard with preserve_observed:**

When config comes from an extra resource that may not exist on the first
reconciliation (e.g., Azure connection config), use `preserve_observed` to keep
the resource alive while the config source is temporarily unavailable:

```python
# Extra resource may not exist on first reconciliation
azure_config = get_extra_resource("azure-conn", "data.config", None)
body = {
    "apiVersion": "nop.crossplane.io/v1alpha1",
    "kind": "NopResource",
    "spec": {"forProvider": {"config": azure_config}},
} if azure_config else None

Resource("azure-dep", body, preserve_observed=True)
# First reconcile (no extra resource yet): body=None, emits observed body if it
#   exists, skips if not
# Subsequent reconciles: body=dict, emitted normally (preserve_observed is a no-op)
```

**(c) Combined: when + preserve_observed:**

Gate on an explicit toggle while also preserving the observed body when config
is absent:

```python
# Gate + preserve: skip when explicitly disabled, preserve observed when body absent
enabled = get(oxr, "spec.features.cache", True)
cache_config = get(oxr, "spec.cacheConfig", None)
body = build_cache(cache_config) if cache_config else None

Resource("cache", body,
    when=enabled, skip_reason="cache disabled",
    preserve_observed=True)
```

See [builtins reference](builtins-reference.md#resource) for the full behavior
state table covering all combinations of `when`, `body`, and
`preserve_observed`.

## See also

- [Builtins reference](builtins-reference.md) -- complete function signatures
- [Features guide](features.md) -- detailed coverage of depends_on, labels,
  connection details, namespace modules, and metrics
- [Migration cheatsheet](migration-cheatsheet.md) -- Sprig/KCL to
  function-starlark helper mapping
- [Module system](module-system.md) -- load(), OCI modules, standard library
- [Deployment guide](deployment-guide.md) -- cluster deployment and metrics
  setup
