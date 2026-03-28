# OCI Module Distribution

Share and reuse Starlark modules across compositions by publishing them to any
OCI-compatible container registry (GHCR, ACR, ECR, Docker Hub, Harbor, etc.).

## How it works

function-starlark resolves OCI modules **before** any Starlark code runs:

1. Scan the main script and inline modules for OCI load targets (both
   short-form `package:tag/file.star` and explicit `oci://` URLs)
2. Expand short-form targets using the configured default registry
3. Deduplicate references (same registry/repo:tag = one pull)
4. Fetch artifacts from the registry (or serve from in-memory cache)
5. Scan fetched modules for transitive OCI loads, repeat until resolved
6. Inject all resolved `.star` files into the inline module map
7. Execute the script -- all OCI modules available as if they were inline

This resolve-then-execute architecture preserves Starlark's sandbox hermeticity:
no network access happens during script execution.

## Loading OCI modules

### Short-form (recommended)

When a default OCI registry is configured (see
[Configuring the Default Registry](#configuring-the-default-registry)), use the
concise short-form syntax:

```python
# Load by tag
load("my-org-starlark-lib:v1/helpers.star", "create_bucket", "create_topic")

# Load by digest (deterministic, skips tag resolution)
load("my-org-starlark-lib@sha256:abc123.../helpers.star", "create_bucket")

# Star import -- all public exports
load("my-org-starlark-lib:v1/helpers.star", "*")
```

Short-form references are expanded using the default registry. For example,
with registry `ghcr.io/my-org`, `my-org-starlark-lib:v1/helpers.star` becomes
`oci://ghcr.io/my-org/my-org-starlark-lib:v1/helpers.star`.

### Explicit full URL

Use the `oci://` prefix for full control over the registry path, or when no
default registry is configured:

```python
# Load by tag
load("oci://ghcr.io/my-org/starlark-lib:v1/helpers.star", "create_bucket", "create_topic")

# Load by digest (deterministic, skips tag resolution)
load("oci://ghcr.io/my-org/starlark-lib@sha256:abc123.../helpers.star", "create_bucket")

# Star import -- all public exports
load("oci://ghcr.io/my-org/starlark-lib:v1/helpers.star", "*")
```

### URL format

```
oci://registry/repo[:tag|@sha256:digest]/file.star
```

| Component | Required | Example |
|-----------|----------|---------|
| `oci://` prefix | Yes | `oci://` |
| Registry | Yes | `ghcr.io`, `myregistry.azurecr.io`, `localhost:5000` |
| Repository | Yes | `my-org/starlark-lib`, `modules/networking` |
| Tag or digest | Yes | `:v1`, `@sha256:abcdef...` |
| File path | Yes | `/helpers.star`, `/networking.star` |

Implicit `:latest` is **not supported** — always specify an explicit tag or
digest. This is intentional: compositions should be reproducible.

### Star import

`load("module.star", "*")` imports all non-underscore exports from a module.
This works for all module types — inline, filesystem, and OCI:

```python
# Star import: brings in everything the module exports
load("oci://ghcr.io/my-org/lib:v1/helpers.star", "*")

# Equivalent to listing every export by name:
# load("oci://ghcr.io/my-org/lib:v1/helpers.star", "create_bucket", "create_topic", "tag_resource")

# Mix named and star imports:
load("oci://ghcr.io/my-org/lib:v1/helpers.star", "create_bucket", "*")
```

Names starting with `_` are private and never exported through star import.

### Namespace alias imports

Namespace aliases solve name conflicts when loading from multiple OCI packages
that export the same type names. Use the `alias="*"` syntax to wrap all exports
in a struct:

```python
# Short-form with namespace alias
load("schemas-k8s:v1.35/apps/v1.star", k8s="*")
load("schemas-azure:v2.5.0/storage/v1.star", storage="*")

# Explicit URL with namespace alias
load("oci://ghcr.io/wompipomp/schemas-azure:v2.5.0/cosmosdb/v1.star", cosmosdb="*")

# Access via dot notation
k8s.Deployment(...)
storage.Account(...)
cosmosdb.Account(...)
```

Namespace aliases work identically for short-form and explicit OCI loads. See
the [module system guide](module-system.md#namespace-alias-imports) for full
syntax details and mixed import examples.

## Configuring the Default Registry

The default registry enables short-form load syntax by providing the
`registry/namespace` prefix for expansion. Configure it at the operator level
(all compositions) or per-composition.

### Environment variable (operator-level)

Set `STARLARK_OCI_DEFAULT_REGISTRY` on the function pod via a
DeploymentRuntimeConfig:

```yaml
apiVersion: pkg.crossplane.io/v1beta1
kind: DeploymentRuntimeConfig
metadata:
  name: function-starlark
spec:
  deploymentTemplate:
    spec:
      template:
        spec:
          containers:
            - name: package-runtime
              env:
                - name: STARLARK_OCI_DEFAULT_REGISTRY
                  value: "ghcr.io/my-org"
              # If you also need private registry auth, add volume mounts here
              volumeMounts:
                - name: registry-creds
                  mountPath: /var/run/secrets/docker/my-registry-creds
                  readOnly: true
          volumes:
            - name: registry-creds
              secret:
                secretName: my-registry-creds
                items:
                  - key: .dockerconfigjson
                    path: config.json
```

Reference the runtime config in your Function:

```yaml
apiVersion: pkg.crossplane.io/v1beta1
kind: Function
metadata:
  name: function-starlark
spec:
  package: ghcr.io/wompipomp/function-starlark:latest
  runtimeConfigRef:
    name: function-starlark
```

### Spec field (per-composition override)

Set `spec.ociDefaultRegistry` in the StarlarkInput to override or replace the
environment variable for a specific composition:

```yaml
apiVersion: starlark.fn.crossplane.io/v1alpha1
kind: StarlarkInput
spec:
  ociDefaultRegistry: "ghcr.io/my-org"
  source: |
    load("my-starlark-lib:v1/helpers.star", "create_bucket")
    Resource("bucket", create_bucket("us-east-1"))
```

### Precedence

`spec.ociDefaultRegistry` (non-empty) takes precedence over the
`STARLARK_OCI_DEFAULT_REGISTRY` environment variable. If neither is configured
and a short-form load target is encountered, the function returns a fatal error
with a clear message explaining both configuration options.

### Editor configuration

If you use the [function-starlark VS Code extension](https://github.com/wompipomp/function-starlark-vscode)
for schema IntelliSense, you also need to configure the default registry in
VS Code settings so the editor can resolve short-form load targets:

```json
{
  "functionStarlark.schemas.registry": "ghcr.io/my-org"
}
```

The default is `ghcr.io/wompipomp`. Keep this in sync with your runtime
registry configuration so that editor diagnostics match deployed behavior.

### Registry value format

The registry value is `host/namespace` (e.g., `ghcr.io/my-org`). Do not
include the `oci://` prefix -- it is stripped silently if present. Trailing
slashes are also stripped silently.

## Publishing modules

Modules are published as OCI artifacts using [oras](https://oras.land/). Each
artifact is a flat tar of `.star` files with a custom media type.

### Install oras

```bash
# macOS
brew install oras

# Linux
curl -LO https://github.com/oras-project/oras/releases/download/v1.2.2/oras_1.2.2_linux_amd64.tar.gz
tar xzf oras_1.2.2_linux_amd64.tar.gz
sudo mv oras /usr/local/bin/
```

### Push a module bundle

```bash
# Single file
oras push ghcr.io/my-org/starlark-lib:v1 \
  --artifact-type application/vnd.fn-starlark.modules.v1+tar \
  helpers.star

# Multiple files in one bundle
oras push ghcr.io/my-org/starlark-lib:v1 \
  --artifact-type application/vnd.fn-starlark.modules.v1+tar \
  helpers.star naming.star networking.star

# With digest pinning output
oras push ghcr.io/my-org/starlark-lib:v1 \
  --artifact-type application/vnd.fn-starlark.modules.v1+tar \
  helpers.star 2>&1 | grep Digest
# Digest: sha256:abc123...
```

### Media types

| Type | Value |
|------|-------|
| Artifact (config) | `application/vnd.fn-starlark.modules.v1+tar` |
| Layer | `application/vnd.fn-starlark.layer.v1.tar` |

function-starlark validates both media types on pull and rejects artifacts that
don't match. This prevents accidentally loading non-Starlark OCI artifacts.

### Bundle layout

The tar layer must contain `.star` files at the root — no directories, no
nested paths. Safety limits enforced on extraction:

- Files must end in `.star` (non-star files are silently skipped)
- Maximum 100 files per bundle
- Maximum 1 MB per file
- No path traversal (`..`, absolute paths)

### Versioning strategy

Use semantic version tags for your module bundles:

```bash
# Development
oras push ghcr.io/my-org/starlark-lib:v1.2.0-dev helpers.star

# Release
oras push ghcr.io/my-org/starlark-lib:v1.2.0 helpers.star

# Major version alias (pin compositions to :v1 for compatible updates)
oras tag ghcr.io/my-org/starlark-lib:v1.2.0 v1
```

## Authentication

### Public registries

No configuration needed. Anonymous pulls work for public repositories.

### Private registries (ACR, ECR, GHCR, etc.)

Two steps: tell function-starlark which secret to use, and mount the secret
into the function pod.

**1. Set `dockerConfigSecret` in your Composition:**

```yaml
apiVersion: starlark.fn.crossplane.io/v1alpha1
kind: StarlarkInput
spec:
  dockerConfigSecret: my-registry-creds
  source: |
    load("oci://myregistry.azurecr.io/modules/helpers:v1/helpers.star", "create_bucket")
    Resource("bucket", create_bucket("us-east-1"))
```

**2. Create the Kubernetes Secret:**

```bash
# From existing Docker config
kubectl create secret docker-registry my-registry-creds \
  --docker-server=myregistry.azurecr.io \
  --docker-username=<username> \
  --docker-password=<password> \
  -n crossplane-system

# Or from an existing .dockerconfigjson
kubectl create secret generic my-registry-creds \
  --from-file=.dockerconfigjson=$HOME/.docker/config.json \
  --type=kubernetes.io/dockerconfigjson \
  -n crossplane-system
```

**3. Mount the secret via DeploymentRuntimeConfig:**

```yaml
apiVersion: pkg.crossplane.io/v1beta1
kind: DeploymentRuntimeConfig
metadata:
  name: function-starlark
spec:
  deploymentTemplate:
    spec:
      template:
        spec:
          containers:
            - name: package-runtime
              volumeMounts:
                - name: registry-creds
                  mountPath: /var/run/secrets/docker/my-registry-creds
                  readOnly: true
          volumes:
            - name: registry-creds
              secret:
                secretName: my-registry-creds
                items:
                  - key: .dockerconfigjson
                    path: config.json
```

The `items` mapping renames `.dockerconfigjson` to `config.json` because
go-containerregistry's credential chain expects standard Docker config format.

**4. Reference the runtime config in your Function:**

```yaml
apiVersion: pkg.crossplane.io/v1beta1
kind: Function
metadata:
  name: function-starlark
spec:
  package: ghcr.io/my-org/function-starlark:v0.1.0
  runtimeConfigRef:
    name: function-starlark
```

### Azure Container Registry (ACR)

```bash
# Create secret from ACR admin credentials
kubectl create secret docker-registry acr-creds \
  --docker-server=myregistry.azurecr.io \
  --docker-username=$(az acr credential show -n myregistry --query username -o tsv) \
  --docker-password=$(az acr credential show -n myregistry --query passwords[0].value -o tsv) \
  -n crossplane-system

# Or use a service principal for non-interactive auth
kubectl create secret docker-registry acr-creds \
  --docker-server=myregistry.azurecr.io \
  --docker-username=<sp-app-id> \
  --docker-password=<sp-password> \
  -n crossplane-system
```

### Amazon ECR

```bash
# ECR token (expires every 12 hours — use a CronJob or external-secrets to refresh)
TOKEN=$(aws ecr get-login-password --region us-east-1)
kubectl create secret docker-registry ecr-creds \
  --docker-server=123456789.dkr.ecr.us-east-1.amazonaws.com \
  --docker-username=AWS \
  --docker-password=$TOKEN \
  -n crossplane-system
```

### GitHub Container Registry (GHCR)

```bash
kubectl create secret docker-registry ghcr-creds \
  --docker-server=ghcr.io \
  --docker-username=<github-username> \
  --docker-password=<github-pat> \
  -n crossplane-system
```

## Caching

OCI modules are cached in-memory with a two-layer architecture:

| Layer | Key | TTL | Purpose |
|-------|-----|-----|---------|
| Tag cache | `registry/repo:tag` → digest | 5 min (configurable) | Avoid HEAD requests on every reconciliation |
| Content cache | `sha256:...` → `.star` files | Forever (immutable) | Content-addressed, same digest = same content |

### How cache lookups work

1. **Fresh hit** (tag TTL not expired): serve from cache, zero network calls
2. **Stale** (tag TTL expired, registry reachable): re-resolve tag → if same
   digest, serve cached content; if new digest, pull new artifact
3. **Stale + registry down**: serve last-known-good content with a warning
4. **Cold miss + registry down**: fail fast with error naming the unreachable
   registry

### Configuring cache TTL

The cache TTL is a pod-level setting configured via the `STARLARK_OCI_CACHE_TTL`
environment variable (default: `5m`). Set it on the function pod via
DeploymentRuntimeConfig:

```yaml
# In your DeploymentRuntimeConfig
env:
  - name: STARLARK_OCI_CACHE_TTL
    value: "10m"
```

The cache lives in-memory on the function pod. It does not survive pod
restarts — the first reconciliation after a restart pays the OCI pull cost
(~200-500ms), then all subsequent reconciliations serve from memory.

### Digest-pinned references skip the tag cache

```python
load("oci://ghcr.io/my-org/lib@sha256:abc123.../helpers.star", "create_bucket")
```

Digest references go directly to the content cache layer. If the digest is
cached, it's served immediately. If not, it's pulled once and cached forever.
This is the most deterministic option for production compositions.

## Transitive dependencies

OCI modules can load other OCI modules. function-starlark resolves the full
dependency tree before execution:

```python
# helpers.star (published to ghcr.io/my-org/lib:v1)
load("oci://ghcr.io/my-org/base:v1/naming.star", "resource_name")

def create_bucket(region):
    return {"apiVersion": "s3.aws.upbound.io/v1beta1", "kind": "Bucket",
            "metadata": {"name": resource_name("bucket", region)}}
```

```python
# composition.star (your composition)
load("oci://ghcr.io/my-org/lib:v1/helpers.star", "create_bucket")
Resource("bucket", create_bucket("us-east-1"))
```

function-starlark will:
1. Scan `composition.star` → find `ghcr.io/my-org/lib:v1`
2. Pull and extract `helpers.star`
3. Scan `helpers.star` → find `ghcr.io/my-org/base:v1`
4. Pull and extract `naming.star`
5. Inject both into the inline module map
6. Execute — all transitive deps available

**Circular dependencies are detected and produce a clear error.** If module A
loads module B which loads module A, resolution fails before execution.

## Complete example

### Module library

```python
# helpers.star — published to ghcr.io/acme/platform-lib:v1
def s3_bucket(name, region, tags={}):
    """Create a standard S3 bucket with org defaults."""
    return {
        "apiVersion": "s3.aws.upbound.io/v1beta1",
        "kind": "Bucket",
        "metadata": {"name": name, "labels": {"team": "platform"}},
        "spec": {"forProvider": {"region": region, "tags": tags}},
    }

def rds_instance(name, region, engine="postgres", size="db.t3.micro"):
    """Create a standard RDS instance."""
    return {
        "apiVersion": "rds.aws.upbound.io/v1beta1",
        "kind": "Instance",
        "metadata": {"name": name},
        "spec": {"forProvider": {
            "region": region, "engine": engine, "instanceClass": size,
        }},
    }
```

### Publish

```bash
oras push ghcr.io/acme/platform-lib:v1 \
  --artifact-type application/vnd.fn-starlark.modules.v1+tar \
  helpers.star
```

### Composition

```yaml
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: xdatabases.acme.io
spec:
  compositeTypeRef:
    apiVersion: acme.io/v1
    kind: XDatabase
  mode: Pipeline
  pipeline:
    - step: create-resources
      functionRef:
        name: function-starlark
      input:
        apiVersion: starlark.fn.crossplane.io/v1alpha1
        kind: StarlarkInput
        spec:
          dockerConfigSecret: ghcr-creds
          source: |
            load("oci://ghcr.io/acme/platform-lib:v1/helpers.star", "*")

            region = get(oxr, "spec.region", "us-east-1")
            name = get(oxr, "metadata.name", "db")

            Resource("bucket", s3_bucket(name + "-backups", region))
            Resource("database", rds_instance(name, region,
                engine=get(oxr, "spec.engine", "postgres"),
                size=get(oxr, "spec.size", "db.t3.micro")))
```

## Using the standard library

function-starlark ships a standard library of Crossplane helpers published to
`ghcr.io/wompipomp/starlark-stdlib`. It provides four modules covering the
most common composition patterns:

| Module | Purpose |
|--------|---------|
| `networking.star` | CIDR math, IP address utilities (equivalent to Terraform's `cidrsubnet`) |
| `naming.star` | Kubernetes-safe resource naming with 63-character limit enforcement |
| `labels.star` | Kubernetes recommended labels and Crossplane labels with merge utility |
| `conditions.star` | Operational status signaling (`degraded`) |

### Loading stdlib modules

```python
# Short-form (recommended, requires default registry ghcr.io/wompipomp)
load("starlark-stdlib:v1/networking.star", "subnet_cidr", "cidr_contains")
load("starlark-stdlib:v1/naming.star", "resource_name")
load("starlark-stdlib:v1/labels.star", "standard_labels", "crossplane_labels", "merge_labels")
load("starlark-stdlib:v1/conditions.star", "degraded")

# Or use star import to get everything from a module
load("starlark-stdlib:v1/networking.star", "*")

# Explicit full URL (always works, no default registry needed)
load("oci://ghcr.io/wompipomp/starlark-stdlib:v1/networking.star", "subnet_cidr")
```

### Example composition using stdlib

```python
# With default registry configured to ghcr.io/wompipomp
load("starlark-stdlib:v1/naming.star", "resource_name")
load("starlark-stdlib:v1/labels.star", "standard_labels", "crossplane_labels", "merge_labels")

name = resource_name("bucket")
labels = merge_labels(
    standard_labels("my-app", component="storage"),
    crossplane_labels(),
)

Resource("bucket", {
    "apiVersion": "s3.aws.upbound.io/v1beta1",
    "kind": "Bucket",
    "metadata": {"name": name, "labels": labels},
    "spec": {"forProvider": {"region": get(oxr, "spec.region", "us-east-1")}},
})
```

The stdlib is a public GHCR package -- no authentication is needed to pull it.
For full API documentation, see [Standard Library Reference](stdlib-reference.md).

## StarlarkInput reference

The following fields are relevant to OCI module distribution:

```yaml
spec:
  # Default OCI registry for short-form load syntax (overrides env var)
  # Format: "registry/namespace" (e.g. "ghcr.io/my-org")
  ociDefaultRegistry: "ghcr.io/my-org"

  # Registries to access over plain HTTP (overrides env var)
  ociInsecureRegistries: ["localhost:5050"]

  # Name of the Kubernetes Secret with Docker registry credentials
  # Must be mounted via DeploymentRuntimeConfig (overrides env var)
  dockerConfigSecret: "my-registry-creds"

  # Inline modules (OCI-resolved modules are merged into this map)
  modules:
    local-helpers.star: |
      def local_fn(): return "local"
```

## Troubleshooting

### "tag or digest required"

```
OCI load target "oci://ghcr.io/my-org/lib/helpers.star": tag or digest required
```

Add an explicit tag or digest. Implicit `:latest` is not supported:

```python
# Bad
load("oci://ghcr.io/my-org/lib/helpers.star", "fn")

# Good
load("oci://ghcr.io/my-org/lib:v1/helpers.star", "fn")
```

### "artifact media type mismatch"

```
unexpected artifact media type "application/vnd.oci.image.config.v1+json" for ghcr.io/my-org/lib:v1
```

The artifact was pushed without the correct `--artifact-type`. Re-push with:

```bash
oras push ghcr.io/my-org/lib:v1 \
  --artifact-type application/vnd.fn-starlark.modules.v1+tar \
  helpers.star
```

### "OCI module not resolved"

```
OCI module "helpers.star" not resolved; ensure the OCI reference was resolvable
```

The OCI scanner didn't find the `oci://` load target in the script (parse error
in the source), or the registry was unreachable with a cold cache. Check:

1. The `load()` statement has correct `oci://` syntax
2. The registry is reachable from the function pod
3. Credentials are mounted if the registry is private

### Registry authentication failures

```bash
# Check the function pod logs
kubectl logs -n crossplane-system -l pkg.crossplane.io/function=function-starlark

# Verify the secret exists and is correctly mounted
kubectl get secret my-registry-creds -n crossplane-system -o jsonpath='{.type}'
# Should be: kubernetes.io/dockerconfigjson

# Verify the DeploymentRuntimeConfig mount
kubectl get pods -n crossplane-system -l pkg.crossplane.io/function=function-starlark \
  -o jsonpath='{.items[0].spec.containers[0].volumeMounts}' | jq .
```
