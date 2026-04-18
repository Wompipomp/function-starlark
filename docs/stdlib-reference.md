# Standard Library Reference

API reference for the function-starlark standard library. The stdlib is
published as a single OCI artifact at `ghcr.io/wompipomp/starlark-stdlib`.

## Quick start

Load the modules you need and use them in your composition:

```python
load("oci://ghcr.io/wompipomp/starlark-stdlib:v1/networking.star", "subnet_cidr")
load("oci://ghcr.io/wompipomp/starlark-stdlib:v1/naming.star", "resource_name")
load("oci://ghcr.io/wompipomp/starlark-stdlib:v1/labels.star", "standard_labels", "crossplane_labels", "merge_labels")
load("oci://ghcr.io/wompipomp/starlark-stdlib:v1/conditions.star", "degraded")

name = resource_name("bucket")
labels = merge_labels(
    standard_labels("my-app", component="storage"),
    crossplane_labels(),
    {"team": "platform"},
)
subnet = subnet_cidr("10.0.0.0/16", 8, 1)  # "10.0.1.0/24"

Resource("bucket", {
    "apiVersion": "s3.aws.upbound.io/v1beta1",
    "kind": "Bucket",
    "metadata": {"name": name, "labels": labels},
    "spec": {"forProvider": {"region": "us-east-1"}},
})
```

---

## networking.star

CIDR math and IP address manipulation functions equivalent to Terraform's
`cidrsubnet()`, `cidrhost()`, and related functions.

### ip_to_int

```python
def ip_to_int(ip)
```

Convert a dotted-quad IP address string to an integer.

**Args:**
- `ip` -- IP address string (e.g., `"192.168.1.0"`)

**Returns:** Integer representation of the IP address.

**Example:**
```python
ip_to_int("10.0.0.1")      # 167772161
ip_to_int("192.168.1.0")   # 3232235776
ip_to_int("255.255.255.0") # 4294967040
```

**Errors:** Fails if the IP has fewer or more than 4 octets, or if any octet
is outside the range 0-255.

---

### int_to_ip

```python
def int_to_ip(n)
```

Convert an integer to a dotted-quad IP address string.

**Args:**
- `n` -- Integer IP address

**Returns:** Dotted-quad IP string (e.g., `"192.168.1.0"`).

**Example:**
```python
int_to_ip(167772161)   # "10.0.0.1"
int_to_ip(3232235776)  # "192.168.1.0"
```

---

### network_address

```python
def network_address(cidr)
```

Compute the network address (first address) from a CIDR string.

**Args:**
- `cidr` -- CIDR string (e.g., `"10.0.1.5/24"`)

**Returns:** Network address as a dotted-quad string.

**Example:**
```python
network_address("10.0.1.5/24")    # "10.0.1.0"
network_address("192.168.0.0/16") # "192.168.0.0"
network_address("10.0.0.0/8")     # "10.0.0.0"
```

---

### broadcast_address

```python
def broadcast_address(cidr)
```

Compute the broadcast address (last address) from a CIDR string.

**Args:**
- `cidr` -- CIDR string (e.g., `"10.0.1.0/24"`)

**Returns:** Broadcast address as a dotted-quad string.

**Example:**
```python
broadcast_address("10.0.1.0/24")    # "10.0.1.255"
broadcast_address("192.168.0.0/16") # "192.168.255.255"
broadcast_address("10.0.0.0/8")     # "10.255.255.255"
```

---

### subnet_cidr

```python
def subnet_cidr(base_cidr, new_bits, subnet_num)
```

Calculate a subnet CIDR from a base CIDR. Equivalent to Terraform's
`cidrsubnet()` function. Divides the base network into smaller subnets by
adding `new_bits` to the prefix length.

**Args:**
- `base_cidr` -- Base CIDR string (e.g., `"10.0.0.0/16"`)
- `new_bits` -- Number of additional prefix bits (e.g., 8 for /24 from /16)
- `subnet_num` -- Subnet index number (0-based)

**Returns:** Subnet CIDR string (e.g., `"10.0.0.0/24"`).

**Example:**
```python
subnet_cidr("10.0.0.0/16", 8, 0)   # "10.0.0.0/24"
subnet_cidr("10.0.0.0/16", 8, 1)   # "10.0.1.0/24"
subnet_cidr("10.0.0.0/16", 8, 255) # "10.0.255.0/24"
subnet_cidr("10.0.0.0/8", 8, 1)    # "10.1.0.0/16"
```

**Errors:** Fails if `base_prefix + new_bits > 32` or if `subnet_num` is out
of range for the number of possible subnets.

---

### cidr_contains

```python
def cidr_contains(cidr, ip)
```

Check if an IP address is within a CIDR range.

**Args:**
- `cidr` -- CIDR string (e.g., `"10.0.0.0/16"`)
- `ip` -- IP address string to check (e.g., `"10.0.1.5"`)

**Returns:** `True` if the IP is within the CIDR range, `False` otherwise.

**Example:**
```python
cidr_contains("10.0.0.0/16", "10.0.1.5")    # True
cidr_contains("10.0.0.0/16", "10.1.0.1")    # False
cidr_contains("192.168.0.0/24", "192.168.0.100") # True
```

---

## naming.star

Smart resource naming with Kubernetes 63-character limit enforcement. Uses
hash-based truncation when the combined XR name and suffix would exceed the
DNS-1123 label limit.

### resource_name

```python
def resource_name(suffix, xr_name=None)
```

Generate a Kubernetes-safe resource name. Combines the XR name with a suffix,
automatically truncating and appending a hash suffix if the result would exceed
63 characters.

**Args:**
- `suffix` -- Resource type suffix (e.g., `"bucket"`, `"rds"`, `"subnet-public"`)
- `xr_name` -- XR name override. If `None`, reads from `oxr` metadata.

**Returns:** String name that fits within 63 characters.

**Example:**
```python
# Short names pass through unchanged
resource_name("bucket", xr_name="my-app")
# "my-app-bucket"

# Long names are truncated with a hash suffix for uniqueness
resource_name("primary-database-instance", xr_name="my-very-long-application-name-that-exceeds-limits")
# "my-very-long-application-name-that-exceeds-limits-primary-d3k7f"

# Without xr_name, reads from oxr.metadata.name
resource_name("bucket")
# "{xr-name}-bucket"
```

**Behavior:**
- If `xr_name + "-" + suffix` is 63 characters or fewer, returns it as-is.
- If it exceeds 63 characters, truncates to `63 - 5 - 1 = 57` characters,
  strips trailing hyphens, and appends a 5-character deterministic hash.
- The hash is derived from the full untruncated name, ensuring different long
  names produce different suffixes.

---

## labels.star

Kubernetes recommended labels and Crossplane-specific labels, plus a merge
utility. Follows the [Kubernetes recommended labels](https://kubernetes.io/docs/concepts/overview/working-with-objects/common-labels/)
specification.

### Constants

Label key constants for use in custom label logic:

```python
# Kubernetes recommended label keys
APP_NAME         = "app.kubernetes.io/name"
APP_INSTANCE     = "app.kubernetes.io/instance"
APP_VERSION      = "app.kubernetes.io/version"
APP_COMPONENT    = "app.kubernetes.io/component"
APP_PART_OF      = "app.kubernetes.io/part-of"
APP_MANAGED_BY   = "app.kubernetes.io/managed-by"

# Crossplane label keys
XP_COMPOSITE       = "crossplane.io/composite"
XP_CLAIM_NAME      = "crossplane.io/claim-name"
XP_CLAIM_NAMESPACE = "crossplane.io/claim-namespace"
```

---

### standard_labels

```python
def standard_labels(name, instance="", version="", component="", part_of="", managed_by="crossplane")
```

Generate Kubernetes recommended labels. Only includes labels with non-empty
values, except `name` and `managed_by` which are always present.

**Args:**
- `name` -- Application name (`app.kubernetes.io/name`)
- `instance` -- Unique instance identifier (optional)
- `version` -- Application version (optional)
- `component` -- Component within architecture (optional)
- `part_of` -- Higher-level application name (optional)
- `managed_by` -- Tool managing the resource (default: `"crossplane"`)

**Returns:** Dict of label key-value pairs.

**Example:**
```python
standard_labels("my-app")
# {"app.kubernetes.io/name": "my-app", "app.kubernetes.io/managed-by": "crossplane"}

standard_labels("my-app", version="1.2.0", component="database")
# {"app.kubernetes.io/name": "my-app",
#  "app.kubernetes.io/managed-by": "crossplane",
#  "app.kubernetes.io/version": "1.2.0",
#  "app.kubernetes.io/component": "database"}
```

---

### crossplane_labels

```python
def crossplane_labels(composite_name="", claim_name="", claim_namespace="")
```

Generate Crossplane-specific labels. When parameters are empty, reads values
from the observed composite resource (`oxr`) metadata.

**Args:**
- `composite_name` -- XR name override (reads from `oxr` if empty)
- `claim_name` -- Claim name override (reads from `oxr` labels if empty)
- `claim_namespace` -- Claim namespace override (reads from `oxr` labels if empty)

**Returns:** Dict of Crossplane label key-value pairs.

**Example:**
```python
# Auto-read from oxr
crossplane_labels()
# {"crossplane.io/composite": "my-xr",
#  "crossplane.io/claim-name": "my-claim",
#  "crossplane.io/claim-namespace": "default"}

# Manual override
crossplane_labels(composite_name="custom-xr")
# {"crossplane.io/composite": "custom-xr"}
```

---

### merge_labels

```python
def merge_labels(*label_dicts)
```

Merge multiple label dicts with later dicts overriding earlier ones. Creates a
new dict -- does not modify any input dicts.

**Args:**
- `*label_dicts` -- Variable number of label dicts to merge

**Returns:** New merged dict of labels.

**Example:**
```python
merge_labels(
    standard_labels("my-app"),
    crossplane_labels(),
    {"team": "platform", "env": "prod"},
)
# Combines all labels; if any key appears in multiple dicts,
# the value from the last dict wins.
```

---

## conditions.star

Ergonomic wrapper around `set_condition` and `emit_event` for signaling
operational status. These set **informational** status conditions on the
composite resource -- they do **not** control XR readiness.

> **Note:** XR readiness is determined by the `Ready` field on desired composed
> resources, typically managed by `function-auto-ready` or the `ready` parameter
> on `Resource()`. Do not use `set_condition` to control readiness.

### degraded

```python
def degraded(reason, message="")
```

Signal that the composite resource is degraded and emit a warning event. Calls
both `set_condition` (with `type="Degraded"`, `status="True"`) and `emit_event`
with `severity="Warning"`. Use for recoverable failures where the composition is
impaired but not completely broken.

**Args:**
- `reason` -- Machine-readable reason (e.g., `"DBFailing"`, `"QuotaExceeded"`)
- `message` -- Optional human-readable message

**Returns:** None.

**Example:**
```python
degraded("ReplicaLag", "Read replica lagging by 30 seconds")
degraded("QuotaExceeded")
```

---

## Installation

The standard library is available as an OCI artifact. No installation step is
needed -- just reference it in your `load()` statements:

```python
load("oci://ghcr.io/wompipomp/starlark-stdlib:v1/networking.star", "*")
```

For private registries, configure authentication as described in
[OCI Module Distribution](oci-module-distribution.md#authentication).

## Version policy

The stdlib follows semantic versioning:

| Tag | Meaning |
|-----|---------|
| `:v1.0.0` | Specific release |
| `:v1` | Latest v1.x.x (backward-compatible updates) |
| `:dev` | Development snapshot (unstable) |

Pin to a major version alias (`:v1`) for compositions that should receive
compatible updates, or pin to a specific tag for maximum reproducibility.

## Caching and freshness

Resolved stdlib artifacts are cached in-memory by the function pod. The
revalidation strategy is governed by a Kubernetes-style pull policy:

| `STARLARK_OCI_PULL_POLICY` | Behavior |
|---------------------------|----------|
| `IfNotPresent` (default)  | First reference pulls the artifact; subsequent reconciliations reuse the in-memory copy for the pod's lifetime. No HEAD checks. Pick up a retag by restarting the pod or bumping the tag. |
| `Always`                  | On cache miss or after `STARLARK_OCI_CACHE_TTL` expires, the resolver issues a manifest HEAD. The digest is compared against the digest cache; unchanged digests reuse cached content, changed digests trigger a full pull. |

`STARLARK_OCI_CACHE_TTL` is only consulted under `Always` (default `0` =
revalidate on every reconciliation). Digest-pinned loads — 
`oci://…/stdlib@sha256:…/naming.star` — bypass policy/TTL altogether and
never revalidate.

Under the default `IfNotPresent` policy, steady-state registry traffic is
zero regardless of reconciliation rate. Use `Always` when you intentionally
need in-place updates without a pod restart.

## See also

- [Library Authoring Guide](library-authoring.md) -- How to create your own
  Starlark module library
- [OCI Module Distribution](oci-module-distribution.md) -- Loading, publishing,
  authentication, and caching for OCI modules
