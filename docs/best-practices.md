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

Use plain `if` statements for environment-specific or optional resources:

```python
if env == "prod":
    Resource("monitoring", {
        "apiVersion": "monitoring.example.io/v1",
        "kind": "Dashboard",
        "spec": {"forProvider": {"region": region, "enabled": True}},
    })
```

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

## See also

- [Builtins reference](builtins-reference.md) -- complete function signatures
- [Features guide](features.md) -- detailed coverage of depends_on, labels,
  connection details, and metrics
- [Module system](module-system.md) -- load(), OCI modules, standard library
- [Deployment guide](deployment-guide.md) -- cluster deployment and metrics
  setup
