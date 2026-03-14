# Staging Cluster Deployment Guide

This guide walks through deploying function-starlark to a staging Kubernetes
cluster for end-to-end validation (QUAL-04). It covers building, installing,
and verifying the function with the example XBucket composition.

## Prerequisites

- Kubernetes cluster (v1.28+) with kubectl access
- [Crossplane](https://crossplane.io/) installed (v1.17+)
- Docker for building the function image
- A container registry accessible from the cluster (e.g., GHCR, ECR, Docker Hub)

Verify Crossplane is running:

```bash
kubectl get pods -n crossplane-system
# NAME                                       READY   STATUS    RESTARTS   AGE
# crossplane-xxxxxxxxxx-xxxxx                1/1     Running   0          5m
# crossplane-rbac-manager-xxxxxxxxxx-xxxxx   1/1     Running   0          5m
```

## Step 1: Build and push the function image

```bash
# Build the image
make build

# Tag for your registry
docker tag runtime ghcr.io/YOUR_ORG/function-starlark:v0.1.0

# Push
docker push ghcr.io/YOUR_ORG/function-starlark:v0.1.0
```

Alternatively, build a Crossplane package:

```bash
make xpkg
# Produces function-starlark.xpkg
```

## Step 2: Install the function

**Option A: Direct image reference**

```yaml
apiVersion: pkg.crossplane.io/v1beta1
kind: Function
metadata:
  name: function-starlark
spec:
  package: ghcr.io/YOUR_ORG/function-starlark:v0.1.0
```

```bash
kubectl apply -f - <<EOF
apiVersion: pkg.crossplane.io/v1beta1
kind: Function
metadata:
  name: function-starlark
spec:
  package: ghcr.io/YOUR_ORG/function-starlark:v0.1.0
EOF
```

**Option B: Crossplane package**

```bash
# Push the package to a registry
crossplane xpkg push ghcr.io/YOUR_ORG/function-starlark:v0.1.0 \
  -f function-starlark.xpkg

# Install via Function resource
kubectl apply -f - <<EOF
apiVersion: pkg.crossplane.io/v1beta1
kind: Function
metadata:
  name: function-starlark
spec:
  package: ghcr.io/YOUR_ORG/function-starlark:v0.1.0
EOF
```

Verify the function is installed and healthy:

```bash
kubectl get functions
# NAME                 INSTALLED   HEALTHY   PACKAGE                                          AGE
# function-starlark    True        True      ghcr.io/YOUR_ORG/function-starlark:v0.1.0        30s
```

## Step 3: Create DeploymentRuntimeConfig (optional)

If using ConfigMap-mounted scripts instead of inline source, create a
DeploymentRuntimeConfig to mount the ConfigMap:

```yaml
apiVersion: pkg.crossplane.io/v1beta1
kind: DeploymentRuntimeConfig
metadata:
  name: function-starlark-scripts
spec:
  deploymentTemplate:
    spec:
      template:
        spec:
          containers:
            - name: package-runtime
              volumeMounts:
                - name: scripts
                  mountPath: /scripts/my-scripts
                  readOnly: true
          volumes:
            - name: scripts
              configMap:
                name: my-scripts
```

Then reference it in the Function:

```yaml
apiVersion: pkg.crossplane.io/v1beta1
kind: Function
metadata:
  name: function-starlark
spec:
  package: ghcr.io/YOUR_ORG/function-starlark:v0.1.0
  runtimeConfigRef:
    name: function-starlark-scripts
```

For inline scripts (the common case), skip this step.

## Step 4: Apply the XRD and Composition

Apply the example XBucket XRD:

```bash
kubectl apply -f - <<EOF
apiVersion: apiextensions.crossplane.io/v1
kind: CompositeResourceDefinition
metadata:
  name: xbuckets.example.crossplane.io
spec:
  group: example.crossplane.io
  names:
    kind: XBucket
    plural: xbuckets
  claimNames:
    kind: Bucket
    plural: buckets
  versions:
    - name: v1
      served: true
      referenceable: true
      schema:
        openAPIV3Schema:
          type: object
          properties:
            spec:
              type: object
              properties:
                region:
                  type: string
                  description: AWS region for the buckets
                environment:
                  type: string
                  description: Deployment environment (dev, staging, prod)
                  enum: [dev, staging, prod]
              required: [region, environment]
            status:
              type: object
              properties:
                bucketCount:
                  type: integer
                region:
                  type: string
                environment:
                  type: string
EOF
```

Apply the example Composition:

```bash
kubectl apply -f example/composition.yaml
```

Verify:

```bash
kubectl get compositions
# NAME                                READY   COMPOSITION-SCHEMA-AWARE   AGE
# xbuckets.example.crossplane.io     True    True                       10s

kubectl get xrds
# NAME                                ESTABLISHED   OFFERED   AGE
# xbuckets.example.crossplane.io     True          True      10s
```

## Step 5: Create a test XBucket and verify

```bash
kubectl apply -f - <<EOF
apiVersion: example.crossplane.io/v1
kind: XBucket
metadata:
  name: test-xbucket
spec:
  region: us-east-1
  environment: prod
EOF
```

Check that the XBucket is created and resources are composed:

```bash
# Check XR status
kubectl get xbucket test-xbucket -o yaml

# Check composed resources
kubectl get managed -l crossplane.io/composite=test-xbucket
```

Expected resources (10 total):
- 8 S3 Bucket resources (bucket-0 through bucket-7)
- 1 monitoring Dashboard (prod-only)
- 1 SNS Topic

Check conditions:

```bash
kubectl get xbucket test-xbucket -o jsonpath='{.status.conditions}' | jq .
```

Expected condition:
```json
[
  {
    "type": "Ready",
    "status": "True",
    "reason": "Available",
    "message": "All 10 resources created in us-east-1"
  }
]
```

Check events:

```bash
kubectl get events --field-selector involvedObject.name=test-xbucket
```

## Step 6: Verify DXR status

```bash
kubectl get xbucket test-xbucket -o jsonpath='{.status}' | jq .
```

Expected:
```json
{
  "bucketCount": 8,
  "region": "us-east-1",
  "environment": "prod"
}
```

## Step 7: Clean up

```bash
kubectl delete xbucket test-xbucket
kubectl delete composition xbuckets.example.crossplane.io
kubectl delete xrd xbuckets.example.crossplane.io
kubectl delete function function-starlark
```

## Troubleshooting

### Function not healthy

```bash
kubectl describe function function-starlark
kubectl logs -n crossplane-system -l pkg.crossplane.io/function=function-starlark
```

Common causes:
- Image not accessible from the cluster (check registry credentials)
- Image architecture mismatch (build for linux/amd64 if cluster runs on amd64)

### Composition not producing resources

```bash
# Check function logs
kubectl logs -n crossplane-system -l pkg.crossplane.io/function=function-starlark

# Check events on the XR
kubectl describe xbucket test-xbucket
```

Common causes:
- Starlark script syntax error (check function logs for stack trace)
- Missing spec fields in the XR (the script uses `get()` with defaults)

### ConfigMap script not loading

```bash
# Verify ConfigMap exists
kubectl get configmap my-scripts -o yaml

# Verify DeploymentRuntimeConfig is applied
kubectl get deploymentruntimeconfig function-starlark-scripts -o yaml

# Check pod volume mounts
kubectl get pods -n crossplane-system -l pkg.crossplane.io/function=function-starlark \
  -o jsonpath='{.items[0].spec.containers[0].volumeMounts}' | jq .
```

Common causes:
- ConfigMap not in the crossplane-system namespace
- Volume mount path does not match expected `/scripts/{configmap-name}/{key}`
- DeploymentRuntimeConfig not referenced in Function spec

### Local validation with crossplane render

Before deploying to a cluster, validate locally:

```bash
make render
# Or for regression checking:
make render-check
```

This builds the Docker image and runs `crossplane render` against the example
fixtures without requiring a Kubernetes cluster.
