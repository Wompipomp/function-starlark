# E2E Cluster Tests for function-starlark

End-to-end tests that run function-starlark in a real Crossplane cluster.

## What's tested

| Test | Composition | What it validates |
|------|-------------|-------------------|
| **builtins** | `composition-builtins.yaml` | Function builtins: globals, safe access, Resource (incl. `external_name` kwarg + conflict warning), conditions, events, status, connection details, schema, struct, `set_response_ttl`, observed-state helpers (`is_observed`, `observed_body`, `get_condition`), composite readiness |
| **oci-loading** | `composition-oci.yaml` | `load("oci://...")` from a local registry, transitive module resolution |
| **star-imports** | `composition-star-imports.yaml` | Transitive `load("x.star", "*")` inside modules, diamond dependency pattern |
| **depends-on** | `composition-depends-on.yaml` | Creation sequencing (A->B->C chain), deletion ordering via Usage resources |
| **composite-ready** | `composition-composite-ready.yaml` | Composite readiness gating: auto-gate via `When(condition=False)`, opt-out via `When(optional=True)`, explicit `set_composite_ready(False, reason=..., message=...)`, per-resource `Resource(ready=False/True)` overrides |
| **transitive-skip** | `composition-transitive-skip.yaml` | Transitive skip via `SkippedRef` in `depends_on` (always propagates); `depends_on=[None]` is tolerated without error |
| **mutable-struct** | `composition-mutable-struct.yaml` | mutable_struct construction, mutation, merge, schema validation (construction + SetField + None), schema-aware merge with String() identity, pipeline integration (Resource, dict.compact, yaml.encode, json.encode) |
| **context-env** | `composition-context-env.yaml` | Cross-step pipeline context propagation; `environment` global populated from the well-known `apiextensions.crossplane.io/environment` context key (incl. nested access) |
| **extra-resources** | `composition-extra-resources.yaml` | Extra-resources roundtrip: `require_extra_resource` (match_name), `require_extra_resources` (match_labels), `get_extra_resource`/`get_extra_resources` readers, raw `extra_resources` global |
| **fieldpath-dep** | `composition-fieldpath-dep.yaml` | Tuple `depends_on=[(ref, "field.path")]` readiness: consumer deferred (ComposedResourcesReady=False/WaitingForDependencies) until the producer's observed field path is truthy, then unblocked |
| **usage-v2** | `composition-usage-v2.yaml` | `usageAPIVersion: "v2"` (protection.crossplane.io Usage resources; cluster-scoped XRs get `ClusterUsage`). Runs by default (the e2e cluster is Crossplane 2.x); skipped only when overriding to a 1.x cluster |

Known e2e gap (covered by unit tests only): OCI registry auth via
`dockerConfigSecret`/`dockerConfigCredential`. Credentials are deliberately
never sent to insecure (HTTP) registries, so an e2e test would need a
TLS-enabled registry plus CA trust injected into the function pod.

## Prerequisites

- Docker
- [kind](https://kind.sigs.k8s.io/)
- kubectl
- [oras](https://oras.land/) (for OCI module push)

## Quick start

```bash
# Full lifecycle: create cluster, install crossplane, deploy function, run tests, teardown
./run-tests.sh

# Keep cluster alive after tests (for debugging)
./run-tests.sh --no-teardown

# Only setup the cluster (no tests)
./setup.sh

# Only teardown
./teardown.sh

# Test against Crossplane 1.x instead of the default 2.x
CROSSPLANE_VERSION=1.19 ./run-tests.sh
```

The suite defaults to **Crossplane 2.x** and is version-portable: on a 2.x
cluster it exercises the v2 Usage API (`protection.crossplane.io`, emitting
`ClusterUsage` for the cluster-scoped e2e XRs and running the usage-v2 test);
on a 1.x cluster it uses the v1 Usage API and skips the usage-v2 test. Set
`CROSSPLANE_VERSION` to pick the version.

## Test flow

1. `setup.sh` creates a kind cluster with a local registry, installs Crossplane + provider-nop, builds and loads function-starlark
2. `run-tests.sh` applies XRDs, compositions, and XR claims, then validates outcomes
3. `teardown.sh` deletes the kind cluster and local registry

## Debugging

```bash
# Check function pod logs
kubectl logs -n crossplane-system -l pkg.crossplane.io/function=function-starlark

# Check XR status
kubectl get xtest -o yaml

# Check composed resources
kubectl get managed

# Check Usage resources
kubectl get usages

# Check events
kubectl get events --field-selector involvedObject.kind=XTest
```
