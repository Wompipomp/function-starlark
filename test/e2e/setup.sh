#!/usr/bin/env bash
# Setup a kind cluster with Crossplane, provider-nop, and function-starlark
# for end-to-end testing.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"
CLUSTER_NAME="${CLUSTER_NAME:-fn-starlark-e2e}"
REGISTRY_NAME="${REGISTRY_NAME:-fn-starlark-registry}"
REGISTRY_PORT="${REGISTRY_PORT:-5050}"
CROSSPLANE_VERSION="${CROSSPLANE_VERSION:-1.19}"
PROVIDER_NOP_VERSION="${PROVIDER_NOP_VERSION:-v0.3.0}"

echo "==> Setting up e2e test environment"
echo "    Cluster:    $CLUSTER_NAME"
echo "    Registry:   localhost:$REGISTRY_PORT"
echo "    Crossplane: $CROSSPLANE_VERSION"

# --- Local OCI registry ---
if ! docker inspect "$REGISTRY_NAME" &>/dev/null; then
    echo "==> Starting local OCI registry on port $REGISTRY_PORT"
    docker run -d --restart=always -p "$REGISTRY_PORT:5000" --name "$REGISTRY_NAME" registry:2
else
    echo "==> Local registry already running"
fi

# --- kind cluster ---
if kind get clusters 2>/dev/null | grep -q "^${CLUSTER_NAME}$"; then
    echo "==> Kind cluster '$CLUSTER_NAME' already exists"
else
    echo "==> Creating kind cluster '$CLUSTER_NAME'"
    cat <<EOF | kind create cluster --name "$CLUSTER_NAME" --config=-
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
containerdConfigPatches:
- |-
  [plugins."io.containerd.grpc.v1.cri".registry.mirrors."localhost:${REGISTRY_PORT}"]
    endpoint = ["http://${REGISTRY_NAME}:5000"]
  [plugins."io.containerd.grpc.v1.cri".registry.mirrors."e2e-registry.default.svc.cluster.local:5000"]
    endpoint = ["http://${REGISTRY_NAME}:5000"]
EOF
    # Connect registry to kind network
    docker network connect "kind" "$REGISTRY_NAME" 2>/dev/null || true
fi

kubectl cluster-info --context "kind-${CLUSTER_NAME}"

# Get registry IP on the kind network (reachable from pods via K8s Service)
REGISTRY_IP="$(docker inspect -f '{{(index .NetworkSettings.Networks "kind").IPAddress}}' "$REGISTRY_NAME")"
echo "    Registry IP (in-cluster): $REGISTRY_IP"

# In-cluster registry address used by Crossplane pods (via K8s Service DNS)
INCLUSTER_REGISTRY="e2e-registry.default.svc.cluster.local:5000"

# --- Crossplane ---
echo "==> Installing Crossplane ${CROSSPLANE_VERSION}"
helm repo add crossplane-stable https://charts.crossplane.io/stable 2>/dev/null || true
helm repo update crossplane-stable
helm upgrade --install crossplane crossplane-stable/crossplane \
    --namespace crossplane-system --create-namespace \
    --version "$CROSSPLANE_VERSION" \
    --wait --timeout 120s

echo "==> Waiting for Crossplane pods"
kubectl wait --for=condition=Ready pods -l app=crossplane -n crossplane-system --timeout=120s

# --- In-cluster Service for local registry (so pods can reach it via K8s DNS) ---
echo "==> Creating K8s Service for local registry"
kubectl apply -f - <<EOF
apiVersion: v1
kind: Service
metadata:
  name: e2e-registry
spec:
  ports:
  - port: 5000
    targetPort: 5000
---
apiVersion: v1
kind: Endpoints
metadata:
  name: e2e-registry
subsets:
- addresses:
  - ip: ${REGISTRY_IP}
  ports:
  - port: 5000
EOF

# --- provider-nop ---
echo "==> Installing provider-nop ${PROVIDER_NOP_VERSION}"
kubectl apply -f - <<EOF
apiVersion: pkg.crossplane.io/v1
kind: Provider
metadata:
  name: provider-nop
spec:
  package: xpkg.upbound.io/crossplane-contrib/provider-nop:${PROVIDER_NOP_VERSION}
EOF

echo "==> Waiting for provider-nop to become healthy"
kubectl wait --for=condition=Healthy provider.pkg provider-nop --timeout=120s

# --- function-auto-ready ---
echo "==> Installing function-auto-ready"
kubectl apply -f - <<EOF
apiVersion: pkg.crossplane.io/v1beta1
kind: Function
metadata:
  name: function-auto-ready
spec:
  package: xpkg.upbound.io/crossplane-contrib/function-auto-ready:v0.3.0
EOF

echo "==> Waiting for function-auto-ready to become healthy"
kubectl wait --for=condition=Healthy function.pkg function-auto-ready --timeout=120s

# --- Build and load function-starlark ---
echo "==> Building function-starlark runtime image"
cd "$ROOT_DIR"
docker build . --platform linux/amd64 --tag runtime --quiet

echo "==> Building Crossplane xpkg"
crossplane xpkg build -f package --embed-runtime-image=runtime -o function-starlark.xpkg

echo "==> Pushing xpkg to local registry"
crossplane xpkg push "localhost:${REGISTRY_PORT}/function-starlark:e2e" -f function-starlark.xpkg

echo "==> Installing function-starlark"
kubectl apply -f - <<EOF
apiVersion: pkg.crossplane.io/v1beta1
kind: Function
metadata:
  name: function-starlark
spec:
  package: "${INCLUSTER_REGISTRY}/function-starlark:e2e"
EOF

echo "==> Waiting for function-starlark to become healthy"
# Function packages can take a moment to reconcile
for i in $(seq 1 30); do
    if kubectl get function.pkg function-starlark -o jsonpath='{.status.conditions[?(@.type=="Healthy")].status}' 2>/dev/null | grep -q "True"; then
        break
    fi
    echo "    Waiting for function-starlark... ($i/30)"
    sleep 4
done
kubectl wait --for=condition=Healthy function.pkg function-starlark --timeout=60s

# --- Push stdlib to local registry (for OCI loading test) ---
echo "==> Pushing stdlib modules to local registry"
cd "$ROOT_DIR/stdlib"
oras push "localhost:${REGISTRY_PORT}/starlark-stdlib:v1" \
    --artifact-type "application/vnd.fn-starlark.modules.v1+tar" \
    networking.star naming.star labels.star conditions.star

# --- Push mock schema packages to local registry (for schema loading test) ---
echo "==> Pushing mock schema packages to local registry"
cd "$SCRIPT_DIR/schemas/k8s"
oras push "localhost:${REGISTRY_PORT}/schemas-k8s:v1.35" \
    --artifact-type "application/vnd.fn-starlark.modules.v1+tar" \
    apps/v1.star

cd "$SCRIPT_DIR/schemas/azure"
oras push "localhost:${REGISTRY_PORT}/schemas-azure:v2.5.0" \
    --artifact-type "application/vnd.fn-starlark.modules.v1+tar" \
    storage/v1.star cosmosdb/v1.star

# --- Push package-local fixture bundle (for ./sibling.star load scheme) ---
echo "==> Pushing package-local fixture bundle to local registry"
cd "$SCRIPT_DIR/fixtures/package-local"
oras push "localhost:${REGISTRY_PORT}/pkg-local-demo:v1" \
    --artifact-type "application/vnd.fn-starlark.modules.v1+tar" \
    main.star helper.star values.star

# --- Patch OCI compositions with in-cluster registry address ---
echo "==> Patching OCI compositions with in-cluster registry address"
sed "s|localhost:${REGISTRY_PORT}|${INCLUSTER_REGISTRY}|g" \
    "$SCRIPT_DIR/composition-oci.yaml" > "$SCRIPT_DIR/composition-oci-rendered.yaml"
sed "s|localhost:${REGISTRY_PORT}|${INCLUSTER_REGISTRY}|g" \
    "$SCRIPT_DIR/composition-schemas.yaml" > "$SCRIPT_DIR/composition-schemas-rendered.yaml"

echo ""
echo "==> Setup complete!"
echo "    Cluster:  kind-${CLUSTER_NAME}"
echo "    Registry: localhost:${REGISTRY_PORT} (host) / ${REGISTRY_IP}:5000 (in-cluster)"
echo "    Run tests: ${SCRIPT_DIR}/run-tests.sh"
