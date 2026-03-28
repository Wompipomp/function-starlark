#!/usr/bin/env bash
# Teardown the e2e test environment.
set -euo pipefail

CLUSTER_NAME="${CLUSTER_NAME:-fn-starlark-e2e}"
REGISTRY_NAME="${REGISTRY_NAME:-fn-starlark-registry}"

echo "==> Tearing down e2e test environment"

if kind get clusters 2>/dev/null | grep -q "^${CLUSTER_NAME}$"; then
    echo "==> Deleting kind cluster '$CLUSTER_NAME'"
    kind delete cluster --name "$CLUSTER_NAME"
else
    echo "==> Kind cluster '$CLUSTER_NAME' not found"
fi

if docker inspect "$REGISTRY_NAME" &>/dev/null; then
    echo "==> Removing local registry '$REGISTRY_NAME'"
    docker rm -f "$REGISTRY_NAME"
else
    echo "==> Local registry '$REGISTRY_NAME' not found"
fi

# Clean up rendered files
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
rm -f "$SCRIPT_DIR/composition-oci-rendered.yaml"
rm -f "$SCRIPT_DIR/composition-schemas-rendered.yaml"

echo "==> Teardown complete"
