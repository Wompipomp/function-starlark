# Features guide

Beyond basic `Resource()` calls, function-starlark provides dependency
ordering, automatic labels, connection details, resource skipping, extra
resources, and observability metrics.

## Dependency ordering (depends_on)

The `depends_on` kwarg on `Resource()` creates Crossplane
[Usage](https://docs.crossplane.io/latest/concepts/usages/) resources that
enforce creation and deletion ordering between composed resources.

### How to use

Pass `ResourceRef` objects (returned by `Resource()`) or resource name strings
to the `depends_on=` kwarg:

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
order. The `spec.usageAPIVersion` field controls which Usage API version is
used (`"v1"` or `"v2"`, default `"v1"`).

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

### require_resource

Request a single resource by name or labels:

```python
require_resource("vpc", "ec2.aws.upbound.io/v1beta1", "VPC", match_name="my-vpc")
```

### require_resources

Request multiple resources by label selector:

```python
require_resources("subnets", "ec2.aws.upbound.io/v1beta1", "Subnet",
    match_labels={"network": "main"})
```

### Accessing extra resources

Required resources are available via the `extra_resources` global dict:

```python
require_resource("vpc", "ec2.aws.upbound.io/v1beta1", "VPC", match_name="my-vpc")

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

## See also

- [Builtins reference](builtins-reference.md) -- complete function signatures
  for all builtins
- [Best practices](best-practices.md) -- composition patterns, label strategy,
  and testing guidance
- [Deployment guide](deployment-guide.md) -- cluster deployment and metrics
  collection
