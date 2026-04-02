# E2E Cluster Tests for function-starlark

End-to-end tests that run function-starlark in a real Crossplane cluster.

## What's tested

| Test | Composition | What it validates |
|------|-------------|-------------------|
| **builtins** | `composition-builtins.yaml` | All 22 builtins: globals, safe access, Resource, conditions, events, status, connection details, schema, struct |
| **oci-loading** | `composition-oci.yaml` | `load("oci://...")` from a local registry, transitive module resolution |
| **star-imports** | `composition-star-imports.yaml` | Transitive `load("x.star", "*")` inside modules, diamond dependency pattern |
| **depends-on** | `composition-depends-on.yaml` | Creation sequencing (A->B->C chain), deletion ordering via Usage resources |

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
```

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
