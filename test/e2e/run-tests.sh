#!/usr/bin/env bash
# E2E test runner for function-starlark.
# Applies XRDs, compositions, and XRs, then validates outcomes.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
NO_TEARDOWN=false
SKIP_SETUP=false

for arg in "$@"; do
    case "$arg" in
        --no-teardown) NO_TEARDOWN=true ;;
        --skip-setup) SKIP_SETUP=true ;;
    esac
done

PASS=0
FAIL=0
TESTS=()

# --- Helpers ---
log()  { echo "==> $*"; }
pass() { echo "  PASS: $1"; PASS=$((PASS + 1)); TESTS+=("PASS: $1"); }
fail() { echo "  FAIL: $1"; FAIL=$((FAIL + 1)); TESTS+=("FAIL: $1"); }

wait_for_condition() {
    local resource="$1" condition="$2" timeout="${3:-120}"
    local start=$SECONDS end=$((SECONDS + timeout))
    while [ $SECONDS -lt $end ]; do
        local val
        val=$(kubectl get "$resource" -o jsonpath="{.status.conditions[?(@.type==\"$condition\")].status}" 2>/dev/null || echo "")
        if [ "$val" = "True" ]; then
            return 0
        fi
        # Heartbeat: without realtime compositions (e.g. Crossplane 1.x) XRs
        # can take ~60-90s to notice composed readiness — show we're alive,
        # not hung. On 2.x (realtime, default) this rarely fires.
        local elapsed=$((SECONDS - start))
        if [ "$elapsed" -gt 0 ] && [ $((elapsed % 30)) -lt 3 ]; then
            echo "    ... still waiting for $condition=True on $resource (${elapsed}s/${timeout}s)"
        fi
        sleep 3
    done
    return 1
}

# Waits until the Starlark function has processed the XR at least once (the
# custom ComposedResourcesReady condition has been set OR the XR Synced
# condition is True). Used by composite-readiness tests that expect the XR to
# remain Ready=False, since wait_for_condition(..., Ready) would time out.
wait_for_reconciled() {
    local resource="$1" timeout="${2:-60}"
    local end=$((SECONDS + timeout))
    while [ $SECONDS -lt $end ]; do
        local synced
        synced=$(kubectl get "$resource" -o jsonpath="{.status.conditions[?(@.type==\"Synced\")].status}" 2>/dev/null || echo "")
        if [ "$synced" = "True" ]; then
            return 0
        fi
        sleep 2
    done
    return 1
}

get_condition_field() {
    local resource="$1" type="$2" field="$3"
    kubectl get "$resource" -o jsonpath="{.status.conditions[?(@.type==\"$type\")].$field}" 2>/dev/null || echo ""
}

wait_for_resource() {
    local resource="$1" timeout="${2:-60}"
    local end=$((SECONDS + timeout))
    while [ $SECONDS -lt $end ]; do
        if kubectl get "$resource" &>/dev/null; then
            return 0
        fi
        sleep 2
    done
    return 1
}

wait_for_deletion() {
    local resource="$1" timeout="${2:-120}"
    local end=$((SECONDS + timeout))
    while [ $SECONDS -lt $end ]; do
        if ! kubectl get "$resource" &>/dev/null; then
            return 0
        fi
        sleep 3
    done
    return 1
}

get_status_field() {
    local resource="$1" path="$2"
    kubectl get "$resource" -o jsonpath="{.status.$path}" 2>/dev/null || echo ""
}

# ============================================================
# TEST 0: STARLARK TESTRUNNER (no cluster needed)
# ============================================================
log ""
log "===== TEST 0: STARLARK TESTRUNNER ====="

log "Running testrunner against e2e fixtures..."
RUNNER_OUTPUT=$(cd "$SCRIPT_DIR/../.." && go test -count=1 -v -run TestE2EFixtures ./testrunner/ 2>&1) || true
if echo "$RUNNER_OUTPUT" | grep -q "^ok"; then
    pass "testrunner: e2e fixtures all pass"

    file_count=$(find "$SCRIPT_DIR/fixtures/testrunner" -name "*_test.star" | wc -l | tr -d ' ')
    pass "testrunner: discovered $file_count test files"

    # TestE2EFixtures internally asserts cross-module loading
    # (test_standard_tags_uses_naming_module in runner buffer).
    # If the Go test passed, cross-module loading works.
    pass "testrunner: cross-module loading verified (via TestE2EFixtures assertions)"
else
    fail "testrunner: e2e fixtures failed"
    echo "$RUNNER_OUTPUT"
fi

# --- Setup ---
if [ "$SKIP_SETUP" = false ]; then
    log "Running setup..."
    "$SCRIPT_DIR/setup.sh"
fi

# --- Cluster capability detection (Crossplane 1.x vs 2.x) ---
# On a Crossplane 2.x cluster the suite exercises the v2 Usage API
# (protection.crossplane.io): compositions pinned to usageAPIVersion "v1" are
# rendered to "v2" on the fly, and usage queries target the v2 group.
USAGE_KIND="usages"
RENDER_USAGE_V2=false
# NOTE: single command, no pipeline — `kubectl api-resources | grep -q` breaks
# under `set -o pipefail` (grep -q exits on first match, kubectl dies with
# SIGPIPE, and the check inverts exactly when the API group exists).
if kubectl get crd usages.protection.crossplane.io &>/dev/null; then
    log "Crossplane 2.x detected (protection.crossplane.io served): using v2 Usage API"
    # All e2e XRs are cluster-scoped (legacy), so the function emits the
    # cluster-scoped ClusterUsage kind on the v2 API.
    USAGE_KIND="clusterusages.protection.crossplane.io"
    RENDER_USAGE_V2=true
fi

# Applies a composition file, rendering usageAPIVersion for the cluster.
apply_composition() {
    if [ "$RENDER_USAGE_V2" = true ]; then
        sed 's/usageAPIVersion: "v1"/usageAPIVersion: "v2"/' "$1" | kubectl apply -f -
    else
        kubectl apply -f "$1"
    fi
}

# --- Apply XRD ---
log "Applying XRD"
kubectl apply -f "$SCRIPT_DIR/xrd.yaml"
sleep 5  # Wait for XRD to be established
kubectl wait --for=condition=Established xrd xtests.e2e.fn-starlark.io --timeout=60s

# --- Apply all compositions ---
log "Applying compositions"
apply_composition "$SCRIPT_DIR/composition-builtins.yaml"
# Use rendered OCI composition with in-cluster registry address
if [ -f "$SCRIPT_DIR/composition-oci-rendered.yaml" ]; then
    apply_composition "$SCRIPT_DIR/composition-oci-rendered.yaml"
else
    echo "WARNING: composition-oci-rendered.yaml not found, using original (may fail in-cluster)"
    apply_composition "$SCRIPT_DIR/composition-oci.yaml"
fi
apply_composition "$SCRIPT_DIR/composition-depends-on.yaml"
apply_composition "$SCRIPT_DIR/composition-star-imports.yaml"
apply_composition "$SCRIPT_DIR/composition-composite-ready.yaml"
apply_composition "$SCRIPT_DIR/composition-transitive-skip.yaml"
apply_composition "$SCRIPT_DIR/composition-relative-loads.yaml"
apply_composition "$SCRIPT_DIR/composition-path-modules.yaml"
apply_composition "$SCRIPT_DIR/composition-bundled-modules.yaml"
apply_composition "$SCRIPT_DIR/composition-mutable-struct.yaml"
apply_composition "$SCRIPT_DIR/composition-context-env.yaml"
apply_composition "$SCRIPT_DIR/composition-extra-resources.yaml"
apply_composition "$SCRIPT_DIR/composition-fieldpath-dep.yaml"
if [ -f "$SCRIPT_DIR/composition-schemas-rendered.yaml" ]; then
    apply_composition "$SCRIPT_DIR/composition-schemas-rendered.yaml"
else
    echo "WARNING: composition-schemas-rendered.yaml not found, using original (may fail in-cluster)"
    apply_composition "$SCRIPT_DIR/composition-schemas.yaml"
fi
sleep 2

# ============================================================
# TEST 1: BUILTINS REGRESSION
# ============================================================
log ""
log "===== TEST 1: BUILTINS REGRESSION ====="

log "Creating XR for builtins test"
kubectl apply -f "$SCRIPT_DIR/xr-builtins.yaml"

log "Waiting for builtins XR to become Ready (indicates script ran without errors)..."
if wait_for_condition "xtest/test-builtins" "Ready" 120; then
    pass "builtins: XR reached Ready condition"
else
    fail "builtins: XR did not reach Ready condition"
    kubectl get xtest/test-builtins -o yaml 2>/dev/null || true
fi

# Check composed resources exist. Composed resource K8s names are auto-generated
# (e.g. "test-builtins-5qr7f"), so query by the function-starlark resource-name
# label which holds the logical Resource() name.
for res in resource-a resource-b resource-with-conn; do
    if kubectl get nopresource \
        -l "crossplane.io/composite=test-builtins,function-starlark.crossplane.io/resource-name=$res" \
        -o name 2>/dev/null | grep -q .; then
        pass "builtins: composed resource '$res' exists"
    else
        fail "builtins: composed resource '$res' not found"
    fi
done

# Check skipped resource does NOT exist (query by logical resource-name label).
if kubectl get nopresource \
    -l "crossplane.io/composite=test-builtins,function-starlark.crossplane.io/resource-name=to-be-skipped" \
    -o name 2>/dev/null | grep -q .; then
    fail "builtins: skip_resource() did not remove 'to-be-skipped'"
else
    pass "builtins: skip_resource() correctly removed 'to-be-skipped'"
fi

# Check status fields set by set_xr_status()
# Builtins count: 36 predeclared names (24 functions + 6 globals + 6 namespace modules).
builtins_count=$(get_status_field "xtest/test-builtins" "test.builtinsCount")
if [ "$builtins_count" = "36" ]; then
    pass "builtins: set_xr_status() wrote builtinsCount=36"
else
    fail "builtins: set_xr_status() builtinsCount='$builtins_count' (expected 36)"
fi

schema_worked=$(get_status_field "xtest/test-builtins" "test.schemaWorked")
if [ "$schema_worked" = "true" ]; then
    pass "builtins: schema() + field() validation worked"
else
    fail "builtins: schema() test status='$schema_worked' (expected true)"
fi

# Check events
events=$(kubectl get events --field-selector involvedObject.name=test-builtins -o jsonpath='{.items[*].message}' 2>/dev/null || echo "")
if echo "$events" | grep -q "builtins"; then
    pass "builtins: emit_event() created event"
else
    fail "builtins: no event found from emit_event()"
fi

# Check custom condition
custom_cond=$(kubectl get xtest/test-builtins -o jsonpath='{.status.conditions[?(@.type=="BuiltinsTest")].reason}' 2>/dev/null || echo "")
if [ "$custom_cond" = "Passed" ]; then
    pass "builtins: set_condition() set custom BuiltinsTest condition"
else
    fail "builtins: custom condition reason='$custom_cond' (expected Passed)"
fi

# Check namespace builtins via status fields
crypto_stable=$(get_status_field "xtest/test-builtins" "test.cryptoStableId")
if [ -n "$crypto_stable" ] && [ ${#crypto_stable} -eq 8 ]; then
    pass "builtins: crypto.stable_id() returned 8-char hex"
else
    fail "builtins: crypto.stable_id() result='$crypto_stable' (expected 8-char hex)"
fi

crypto_sha_len=$(get_status_field "xtest/test-builtins" "test.cryptoSha256Len")
if [ "$crypto_sha_len" = "64" ]; then
    pass "builtins: crypto.sha256() returned 64-char hex"
else
    fail "builtins: crypto.sha256() length='$crypto_sha_len' (expected 64)"
fi

regex_match=$(get_status_field "xtest/test-builtins" "test.regexMatchWorked")
if [ "$regex_match" = "true" ]; then
    pass "builtins: regex.match() pattern matching works"
else
    fail "builtins: regex.match() result='$regex_match' (expected true)"
fi

regex_replace=$(get_status_field "xtest/test-builtins" "test.regexReplaceResult")
if [ "$regex_replace" = "hello-world-test" ]; then
    pass "builtins: regex.replace_all() normalized string correctly"
else
    fail "builtins: regex.replace_all() result='$regex_replace' (expected hello-world-test)"
fi

dict_merge_b=$(get_status_field "xtest/test-builtins" "test.dictMergeB")
if [ "$dict_merge_b" = "3" ]; then
    pass "builtins: dict.merge() right-wins behavior correct"
else
    fail "builtins: dict.merge() b='$dict_merge_b' (expected 3)"
fi

deep_merge_b=$(get_status_field "xtest/test-builtins" "test.deepMergeTopB")
if [ "$deep_merge_b" = "3" ]; then
    pass "builtins: dict.deep_merge() nested right-wins behavior correct"
else
    fail "builtins: dict.deep_merge() top.b='$deep_merge_b' (expected 3)"
fi

# Check Usage resource exists (resource-b depends_on resource-a)
usage_count=$(kubectl get "$USAGE_KIND" -l crossplane.io/composite=test-builtins -o name 2>/dev/null | wc -l | tr -d ' ')
if [ "$usage_count" -ge 1 ] 2>/dev/null; then
    pass "builtins: Usage resource(s) created for depends_on"
else
    # Usage resources might not have composite label, check by name pattern
    all_usages=$(kubectl get "$USAGE_KIND" -o name 2>/dev/null | wc -l | tr -d ' ')
    if [ "$all_usages" -ge 1 ] 2>/dev/null; then
        pass "builtins: Usage resource(s) exist in cluster"
    else
        fail "builtins: no Usage resources found"
    fi
fi

# dict.compact recursive tests
compact_pruned=$(get_status_field "xtest/test-builtins" "test.compactNestedPruned")
if [ "$compact_pruned" = "true" ]; then
    pass "compact: nested None pruned"
else
    fail "compact: nested None not pruned (got '$compact_pruned')"
fi

compact_kept=$(get_status_field "xtest/test-builtins" "test.compactNestedKept")
if [ "$compact_kept" = "1" ]; then
    pass "compact: nested non-None kept"
else
    fail "compact: nested non-None not kept (got '$compact_kept')"
fi

compact_list=$(get_status_field "xtest/test-builtins" "test.compactListDictPruned")
if [ "$compact_list" = "true" ]; then
    pass "compact: None in list-nested dict pruned"
else
    fail "compact: None in list-nested dict not pruned (got '$compact_list')"
fi

compact_empty=$(get_status_field "xtest/test-builtins" "test.compactEmptyPreserved")
if [ "$compact_empty" = "true" ]; then
    pass "compact: K8s empties preserved (empty string/list/dict)"
else
    fail "compact: K8s empties not preserved (got '$compact_empty')"
fi

# when=False gating tests
# Check gated resource is absent
composed=$(kubectl get nopresource -l crossplane.io/composite=test-builtins -o name 2>/dev/null || echo "")
if echo "$composed" | grep -q "gated-resource"; then
    fail "gating: gated-resource should have been skipped"
else
    pass "gating: gated-resource correctly skipped"
fi

# Check Warning event containing skip reason
events=$(kubectl get events --field-selector involvedObject.name=test-builtins -o jsonpath='{.items[*].message}' 2>/dev/null || echo "")
if echo "$events" | grep -q "Skipping"; then
    pass "gating: Warning event emitted for skipped resource"
else
    fail "gating: no Warning event with 'Skipping' found"
fi

gated_status=$(get_status_field "xtest/test-builtins" "test.gatedSkipped")
if [ "$gated_status" = "true" ]; then
    pass "gating: set_xr_status confirmed gating executed"
else
    fail "gating: gated test status='$gated_status' (expected true)"
fi

# --- preserve_observed two-phase reconciliation test ---
log "Phase 2: Testing preserve_observed with config removal..."

# Phase 1 verification: preservable-resource should exist (created normally with body=dict).
# Match by logical resource-name label since the K8s object name is auto-generated.
if kubectl get nopresource \
    -l "crossplane.io/composite=test-builtins,function-starlark.crossplane.io/resource-name=preservable-resource" \
    -o name 2>/dev/null | grep -q .; then
    pass "preserve: phase 1 - resource created normally"
else
    fail "preserve: phase 1 - preservable-resource not found"
fi

# Verify config was present on first reconciliation
preserve_config=$(get_status_field "xtest/test-builtins" "test.preserveConfigPresent")
if [ "$preserve_config" = "true" ]; then
    pass "preserve: phase 1 - externalConfig was present"
else
    fail "preserve: phase 1 - externalConfig status='$preserve_config' (expected true)"
fi

# Phase 2: Patch XR to remove externalConfig (triggers re-reconciliation with body=None)
log "Patching XR to remove externalConfig..."
kubectl patch xtest/test-builtins --type=json \
    -p '[{"op":"remove","path":"/spec/externalConfig"}]' 2>/dev/null

# Wait for re-reconciliation -- the XR status field should update
# Use a polling loop: wait until preserveConfigPresent becomes false (or None)
log "Waiting for re-reconciliation..."
for i in $(seq 1 30); do
    preserve_status=$(get_status_field "xtest/test-builtins" "test.preserveConfigPresent" 2>/dev/null || echo "")
    if [ "$preserve_status" = "false" ]; then
        break
    fi
    sleep 2
done

if [ "$preserve_status" = "false" ]; then
    pass "preserve: phase 2 - re-reconciliation detected (config absent)"
else
    fail "preserve: phase 2 - re-reconciliation not detected (status='$preserve_status', expected false)"
fi

# Phase 2 verification: preservable-resource should STILL exist (observed body re-emitted).
if kubectl get nopresource \
    -l "crossplane.io/composite=test-builtins,function-starlark.crossplane.io/resource-name=preservable-resource" \
    -o name 2>/dev/null | grep -q .; then
    pass "preserve: phase 2 - resource preserved after config removal (observed body re-emitted)"
else
    fail "preserve: phase 2 - preservable-resource was deleted (preserve_observed did not work)"
fi

# --- external_name kwarg tests ---
ext_ann=$(kubectl get nopresource \
    -l "crossplane.io/composite=test-builtins,function-starlark.crossplane.io/resource-name=resource-ext-name" \
    -o jsonpath='{.items[0].metadata.annotations.crossplane\.io/external-name}' 2>/dev/null || echo "")
if [ "$ext_ann" = "e2e-external-id" ]; then
    pass "external-name: kwarg set crossplane.io/external-name annotation"
else
    fail "external-name: annotation='$ext_ann' (expected e2e-external-id)"
fi

conflict_ann=$(kubectl get nopresource \
    -l "crossplane.io/composite=test-builtins,function-starlark.crossplane.io/resource-name=resource-ext-conflict" \
    -o jsonpath='{.items[0].metadata.annotations.crossplane\.io/external-name}' 2>/dev/null || echo "")
if [ "$conflict_ann" = "kwarg-wins" ]; then
    pass "external-name: kwarg wins over body annotation on conflict"
else
    fail "external-name: conflict annotation='$conflict_ann' (expected kwarg-wins)"
fi

events=$(kubectl get events --field-selector involvedObject.name=test-builtins -o jsonpath='{.items[*].message}' 2>/dev/null || echo "")
if echo "$events" | grep -q "overrides annotation"; then
    pass "external-name: Warning event emitted for annotation conflict"
else
    fail "external-name: no 'overrides annotation' Warning event found"
fi

# --- set_response_ttl smoke test ---
ttl_set=$(get_status_field "xtest/test-builtins" "test.responseTtlSet")
if [ "$ttl_set" = "true" ]; then
    pass "response-ttl: set_response_ttl() accepted end-to-end"
else
    fail "response-ttl: responseTtlSet='$ttl_set' (expected true)"
fi

# --- is_observed / observed_body / get_condition tests ---
# These flip once a later reconcile observes resource-a (the preserve phase
# above already forced one). Poll on the slowest field (the Ready condition
# must also have appeared on the observed MR).
log "Waiting for observed-state helpers to see resource-a..."
for i in $(seq 1 30); do
    cond_a=$(get_status_field "xtest/test-builtins" "test.condAReadyStatus" 2>/dev/null || echo "")
    if [ "$cond_a" = "True" ]; then
        break
    fi
    sleep 2
done

is_observed_a=$(get_status_field "xtest/test-builtins" "test.isObservedA")
if [ "$is_observed_a" = "true" ]; then
    pass "observed-helpers: is_observed() flipped to true after re-reconcile"
else
    fail "observed-helpers: isObservedA='$is_observed_a' (expected true)"
fi

ob_kind=$(get_status_field "xtest/test-builtins" "test.observedBodyKind")
if [ "$ob_kind" = "NopResource" ]; then
    pass "observed-helpers: observed_body() returned full resource body"
else
    fail "observed-helpers: observedBodyKind='$ob_kind' (expected NopResource)"
fi

if [ "$cond_a" = "True" ]; then
    pass "observed-helpers: get_condition() read Ready condition from observed resource"
else
    fail "observed-helpers: condAReadyStatus='$cond_a' (expected True)"
fi

cond_missing=$(get_status_field "xtest/test-builtins" "test.condMissingIsNone")
if [ "$cond_missing" = "true" ]; then
    pass "observed-helpers: get_condition() returns None for missing condition type"
else
    fail "observed-helpers: condMissingIsNone='$cond_missing' (expected true)"
fi

# ============================================================
# TEST 2: OCI MODULE LOADING
# ============================================================
log ""
log "===== TEST 2: OCI MODULE LOADING ====="

log "Creating XR for OCI test"
kubectl apply -f "$SCRIPT_DIR/xr-oci.yaml"

log "Waiting for OCI XR to become Ready..."
if wait_for_condition "xtest/test-oci" "Ready" 120; then
    pass "oci: XR reached Ready (OCI modules loaded and executed)"
else
    fail "oci: XR did not reach Ready (module loading may have failed)"
    # Show function logs for debugging
    kubectl logs -n crossplane-system -l pkg.crossplane.io/function=function-starlark --tail=50 2>/dev/null || true
fi

oci_loaded=$(get_status_field "xtest/test-oci" "test.ociLoaded")
if [ "$oci_loaded" = "true" ]; then
    pass "oci: set_xr_status confirmed OCI modules loaded"
else
    fail "oci: ociLoaded status='$oci_loaded' (expected true)"
fi

subnet_val=$(get_status_field "xtest/test-oci" "test.subnet")
if [ -n "$subnet_val" ]; then
    pass "oci: networking.star subnet_cidr() returned '$subnet_val'"
else
    fail "oci: subnet_cidr() produced empty result"
fi

pkglocal_msg=$(get_status_field "xtest/test-oci" "test.packageLocalMessage")
if [ "$pkglocal_msg" = "hello, package-local" ]; then
    pass "oci: package-local ./sibling.star loads resolved inside same artifact ('$pkglocal_msg')"
else
    fail "oci: package-local message='$pkglocal_msg' (expected 'hello, package-local')"
fi

# ============================================================
# TEST 3: SCHEMA PACKAGE LOADING
# ============================================================
log ""
log "===== TEST 3: SCHEMA PACKAGE LOADING ====="

log "Creating XR for schemas test"
kubectl apply -f "$SCRIPT_DIR/xr-schemas.yaml"

log "Waiting for schemas XR to become Ready..."
if wait_for_condition "xtest/test-schemas" "Ready" 120; then
    pass "schemas: XR reached Ready (schema packages loaded and validated)"
else
    fail "schemas: XR did not reach Ready (schema loading may have failed)"
    kubectl logs -n crossplane-system -l pkg.crossplane.io/function=function-starlark --tail=50 2>/dev/null || true
fi

schemas_loaded=$(get_status_field "xtest/test-schemas" "test.schemasLoaded")
if [ "$schemas_loaded" = "true" ]; then
    pass "schemas: set_xr_status confirmed schema packages loaded"
else
    fail "schemas: schemasLoaded status='$schemas_loaded' (expected true)"
fi

k8s_replicas=$(get_status_field "xtest/test-schemas" "test.k8sDeploymentReplicas")
if [ "$k8s_replicas" = "3" ]; then
    pass "schemas: k8s.Deployment schema validated (replicas=3)"
else
    fail "schemas: k8s.Deployment replicas='$k8s_replicas' (expected 3)"
fi

storage_loc=$(get_status_field "xtest/test-schemas" "test.storageLocation")
if [ "$storage_loc" = "eastus" ]; then
    pass "schemas: storage.Account schema validated (location=eastus)"
else
    fail "schemas: storage.Account location='$storage_loc' (expected eastus)"
fi

cosmos_loc=$(get_status_field "xtest/test-schemas" "test.cosmosLocation")
if [ "$cosmos_loc" = "westeurope" ]; then
    pass "schemas: cosmosdb.Account schema validated (location=westeurope)"
else
    fail "schemas: cosmosdb.Account location='$cosmos_loc' (expected westeurope)"
fi

# --- Schema defaults populate apiVersion + kind ---
dep_api=$(get_status_field "xtest/test-schemas" "test.k8sDeploymentApiVersion")
if [ "$dep_api" = "apps/v1" ]; then
    pass "schemas: Deployment apiVersion defaulted from schema (apps/v1)"
else
    fail "schemas: Deployment apiVersion='$dep_api' (expected apps/v1)"
fi
dep_kind=$(get_status_field "xtest/test-schemas" "test.k8sDeploymentKind")
if [ "$dep_kind" = "Deployment" ]; then
    pass "schemas: Deployment kind defaulted from schema"
else
    fail "schemas: Deployment kind='$dep_kind' (expected Deployment)"
fi
ss_api=$(get_status_field "xtest/test-schemas" "test.k8sStatefulSetApiVersion")
if [ "$ss_api" = "apps/v1" ]; then
    pass "schemas: StatefulSet apiVersion defaulted from schema (apps/v1)"
else
    fail "schemas: StatefulSet apiVersion='$ss_api' (expected apps/v1)"
fi
ss_kind=$(get_status_field "xtest/test-schemas" "test.k8sStatefulSetKind")
if [ "$ss_kind" = "StatefulSet" ]; then
    pass "schemas: StatefulSet kind defaulted from schema"
else
    fail "schemas: StatefulSet kind='$ss_kind' (expected StatefulSet)"
fi

# --- Resource() reads apiVersion+kind from a SchemaDict's defaults ---
# The composition builds NopRes(spec=...) with no apiVersion/kind; the
# schema defaults must propagate through Resource() into the desired state
# so Crossplane emits a real NopResource in the cluster.
if kubectl get nopresource \
    -l "crossplane.io/composite=test-schemas,function-starlark.crossplane.io/resource-name=schema-defaulted-gvk" \
    -o name 2>/dev/null | grep -q .; then
    pass "schemas: Resource(schema instance) emitted composed resource using schema-defaulted apiVersion/kind"
else
    fail "schemas: no NopResource found for 'schema-defaulted-gvk' (Resource() did not pick up GVK from schema defaults)"
fi

# ============================================================
# TEST 4: TRANSITIVE STAR IMPORTS IN MODULES
# ============================================================
log ""
log "===== TEST 4: TRANSITIVE STAR IMPORTS IN MODULES ====="

log "Creating XR for star-imports test"
kubectl apply -f "$SCRIPT_DIR/xr-star-imports.yaml"

log "Waiting for star-imports XR to become Ready..."
if wait_for_condition "xtest/test-star-imports" "Ready" 120; then
    pass "star-imports: XR reached Ready (transitive star imports work in modules)"
else
    fail "star-imports: XR did not reach Ready (star imports in modules may have failed)"
    kubectl logs -n crossplane-system -l pkg.crossplane.io/function=function-starlark --tail=50 2>/dev/null || true
fi

star_worked=$(get_status_field "xtest/test-star-imports" "test.starImportsWorked")
if [ "$star_worked" = "true" ]; then
    pass "star-imports: all transitive star import assertions passed"
else
    fail "star-imports: starImportsWorked='$star_worked' (expected true)"
fi

platform_name=$(get_status_field "xtest/test-star-imports" "test.platformName")
if [ "$platform_name" = "acme-platform-prod" ]; then
    pass "star-imports: platform.star resolved naming.star exports via load(*, \"*\")"
else
    fail "star-imports: platformName='$platform_name' (expected acme-platform-prod)"
fi

network_name=$(get_status_field "xtest/test-star-imports" "test.networkName")
if [ "$network_name" = "acme-network-prod" ]; then
    pass "star-imports: diamond pattern — network.star also resolved naming.star exports"
else
    fail "star-imports: networkName='$network_name' (expected acme-network-prod)"
fi

# ============================================================
# TEST 5: FILESYSTEM RELATIVE-PATH LOADING (ConfigMap scripts)
# ============================================================
log ""
log "===== TEST 5: FILESYSTEM RELATIVE-PATH LOADING ====="

log "Creating XR for relative-loads test"
kubectl apply -f "$SCRIPT_DIR/xr-relative-loads.yaml"

log "Waiting for relative-loads XR to become Ready..."
if wait_for_condition "xtest/test-relative-loads" "Ready" 120; then
    pass "relative-loads: XR reached Ready (filesystem relative loads worked)"
else
    fail "relative-loads: XR did not reach Ready (relative load may have failed)"
    kubectl logs -n crossplane-system -l pkg.crossplane.io/function=function-starlark --tail=50 2>/dev/null || true
fi

rel_worked=$(get_status_field "xtest/test-relative-loads" "test.relativeLoadsWorked")
if [ "$rel_worked" = "true" ]; then
    pass "relative-loads: all relative load assertions passed"
else
    fail "relative-loads: relativeLoadsWorked='$rel_worked' (expected true)"
fi

greet_result=$(get_status_field "xtest/test-relative-loads" "test.greetResult")
if [ "$greet_result" = "hello, e2e" ]; then
    pass "relative-loads: load(\"./helper.star\", \"greet\") resolved sibling module"
else
    fail "relative-loads: greetResult='$greet_result' (expected 'hello, e2e')"
fi

compute_result=$(get_status_field "xtest/test-relative-loads" "test.computeResult")
if [ "$compute_result" = "42" ]; then
    pass "relative-loads: load(\"./utils.star\", \"*\") star import resolved"
else
    fail "relative-loads: computeResult='$compute_result' (expected 42)"
fi

nested_result=$(get_status_field "xtest/test-relative-loads" "test.nestedChainResult")
if [ "$nested_result" = "99" ]; then
    pass "relative-loads: nested chain (main->helper->utils) resolved correctly"
else
    fail "relative-loads: nestedChainResult='$nested_result' (expected 99)"
fi

# ============================================================
# TEST 6: PATH-BASED INLINE MODULE KEYS
# ============================================================
log ""
log "===== TEST 6: PATH-BASED INLINE MODULE KEYS ====="

log "Creating XR for path-modules test"
kubectl apply -f "$SCRIPT_DIR/xr-path-modules.yaml"

log "Waiting for path-modules XR to become Ready..."
if wait_for_condition "xtest/test-path-modules" "Ready" 120; then
    pass "path-modules: XR reached Ready (path-based module keys resolved)"
else
    fail "path-modules: XR did not reach Ready (path-based module loading may have failed)"
    kubectl logs -n crossplane-system -l pkg.crossplane.io/function=function-starlark --tail=50 2>/dev/null || true
fi

path_worked=$(get_status_field "xtest/test-path-modules" "test.pathModulesWorked")
if [ "$path_worked" = "true" ]; then
    pass "path-modules: all path-based module assertions passed"
else
    fail "path-modules: pathModulesWorked='$path_worked' (expected true)"
fi

path_name=$(get_status_field "xtest/test-path-modules" "test.resourceName")
if [ "$path_name" = "acme-database-prod" ]; then
    pass "path-modules: load(\"lib/naming.star\") resolved path-based key"
else
    fail "path-modules: resourceName='$path_name' (expected 'acme-database-prod')"
fi

path_tags=$(get_status_field "xtest/test-path-modules" "test.tagCount")
if [ "$path_tags" = "3" ]; then
    pass "path-modules: load(\"lib/tags.star\") resolved path-based key"
else
    fail "path-modules: tagCount='$path_tags' (expected 3)"
fi

path_validation=$(get_status_field "xtest/test-path-modules" "test.validationWorked")
if [ "$path_validation" = "true" ]; then
    pass "path-modules: load(\"lib/utils/validation.star\") resolved nested path key"
else
    fail "path-modules: validationWorked='$path_validation' (expected true)"
fi

path_shared=$(get_status_field "xtest/test-path-modules" "test.sharedVersion")
if [ "$path_shared" = "1.0" ]; then
    pass "path-modules: flat key coexists with path-based keys"
else
    fail "path-modules: sharedVersion='$path_shared' (expected '1.0')"
fi

# ============================================================
# TEST 11: BUNDLED MODULES (INTER-MODULE LOADING)
# ============================================================
log ""
log "===== TEST 11: BUNDLED MODULES (INTER-MODULE LOADING) ====="

log "Creating XR for bundled-modules test"
kubectl apply -f "$SCRIPT_DIR/xr-bundled-modules.yaml"

log "Waiting for bundled-modules XR to become Ready..."
if wait_for_condition "xtest/test-bundled-modules" "Ready" 120; then
    pass "bundled-modules: XR reached Ready (inter-module loading worked)"
else
    fail "bundled-modules: XR did not reach Ready (inter-module loading may have failed)"
    kubectl logs -n crossplane-system -l pkg.crossplane.io/function=function-starlark --tail=50 2>/dev/null || true
fi

bundled_worked=$(get_status_field "xtest/test-bundled-modules" "test.bundledWorked")
if [ "$bundled_worked" = "true" ]; then
    pass "bundled-modules: all inter-module loading assertions passed"
else
    fail "bundled-modules: bundledWorked='$bundled_worked' (expected true)"
fi

bundled_name=$(get_status_field "xtest/test-bundled-modules" "test.resourceName")
if [ "$bundled_name" = "acme-database-production" ]; then
    pass "bundled-modules: lib/naming.star loaded lib/config.star (diamond dep)"
else
    fail "bundled-modules: resourceName='$bundled_name' (expected 'acme-database-production')"
fi

bundled_cidr=$(get_status_field "xtest/test-bundled-modules" "test.vpcCidr")
if [ "$bundled_cidr" = "10.0.0.0/16" ]; then
    pass "bundled-modules: lib/networking.star loaded lib/config.star"
else
    fail "bundled-modules: vpcCidr='$bundled_cidr' (expected '10.0.0.0/16')"
fi

bundled_tag_name=$(get_status_field "xtest/test-bundled-modules" "test.tagName")
if [ "$bundled_tag_name" = "acme-api-production" ]; then
    pass "bundled-modules: lib/tags.star loaded both lib/config.star and lib/naming.star (transitive chain)"
else
    fail "bundled-modules: tagName='$bundled_tag_name' (expected 'acme-api-production')"
fi

bundled_tag_count=$(get_status_field "xtest/test-bundled-modules" "test.tagCount")
if [ "$bundled_tag_count" = "4" ]; then
    pass "bundled-modules: standard_tags() returned all 4 tags"
else
    fail "bundled-modules: tagCount='$bundled_tag_count' (expected 4)"
fi

# ============================================================
# TEST 12: MUTABLE_STRUCT (PHASES 45-47)
# ============================================================
log ""
log "===== TEST 12: MUTABLE_STRUCT (PHASES 45-47) ====="

log "Creating XR for mutable_struct test"
kubectl apply -f "$SCRIPT_DIR/xr-mutable-struct.yaml"

log "Waiting for mutable_struct XR to become Ready..."
if wait_for_condition "xtest/test-mutable-struct" "Ready" 120; then
    pass "mutable_struct: XR reached Ready (all assertions passed)"
else
    fail "mutable_struct: XR did not reach Ready"
    kubectl logs -n crossplane-system -l pkg.crossplane.io/function=function-starlark --tail=50 2>/dev/null || true
fi

ms_basic=$(get_status_field "xtest/test-mutable-struct" "test.basicPassed")
if [ "$ms_basic" = "true" ]; then
    pass "mutable_struct: phase 45 — construction, mutation, merge, pipeline integration"
else
    fail "mutable_struct: phase 45 basicPassed='$ms_basic' (expected true)"
fi

ms_schema=$(get_status_field "xtest/test-mutable-struct" "test.schemaPassed")
if [ "$ms_schema" = "true" ]; then
    pass "mutable_struct: phase 46 — schema construction, SetField, None semantics"
else
    fail "mutable_struct: phase 46 schemaPassed='$ms_schema' (expected true)"
fi

ms_nested=$(get_status_field "xtest/test-mutable-struct" "test.nestedPassed")
if [ "$ms_nested" = "true" ]; then
    pass "mutable_struct: phase 46 — nested schema validation"
else
    fail "mutable_struct: phase 46 nestedPassed='$ms_nested' (expected true)"
fi

ms_merge=$(get_status_field "xtest/test-mutable-struct" "test.mergePassed")
if [ "$ms_merge" = "true" ]; then
    pass "mutable_struct: phase 47 — schema merge, String(), None in merge"
else
    fail "mutable_struct: phase 47 mergePassed='$ms_merge' (expected true)"
fi

ms_all=$(get_status_field "xtest/test-mutable-struct" "test.allPassed")
if [ "$ms_all" = "true" ]; then
    pass "mutable_struct: all phases passed end-to-end"
else
    fail "mutable_struct: allPassed='$ms_all' (expected true)"
fi

# Verify composed resources were created from mutable_struct bodies
for res in ms-basic ms-compact ms-schema-merged; do
    if kubectl get nopresource \
        -l "crossplane.io/composite=test-mutable-struct,function-starlark.crossplane.io/resource-name=$res" \
        -o name 2>/dev/null | grep -q .; then
        pass "mutable_struct: composed resource '$res' exists"
    else
        fail "mutable_struct: composed resource '$res' not found"
    fi
done

# ============================================================
# TEST 7: DEPENDS_ON (CREATION SEQUENCING)
# ============================================================
log ""
log "===== TEST 7: DEPENDS_ON (CREATION SEQUENCING) ====="

log "Creating XR for depends_on test"
kubectl apply -f "$SCRIPT_DIR/xr-depends-on.yaml"

# Track creation order by polling for each resource's first appearance.
# database should appear first (no deps), schema after database is Ready,
# app after schema is Ready. standalone has no deps so it can appear anytime.
log "Monitoring creation order (expecting: database -> schema -> app)..."
CREATED_ORDER=()
db_seen=false schema_seen=false app_seen=false standalone_seen=false
POLL_END=$((SECONDS + 180))

resource_exists() {
    kubectl get nopresource -l crossplane.io/composite=test-depends-on \
        -o jsonpath='{range .items[*]}{.metadata.annotations.crossplane\.io/composition-resource-name}{"\n"}{end}' 2>/dev/null | grep -q "^$1$"
}

while [ $SECONDS -lt $POLL_END ]; do
    if [ "$db_seen" = false ] && resource_exists "database"; then
        db_seen=true
        CREATED_ORDER+=("database")
        log "  Created: database (${#CREATED_ORDER[@]})"
    fi
    if [ "$schema_seen" = false ] && resource_exists "schema"; then
        schema_seen=true
        CREATED_ORDER+=("schema")
        log "  Created: schema (${#CREATED_ORDER[@]})"
    fi
    if [ "$app_seen" = false ] && resource_exists "app"; then
        app_seen=true
        CREATED_ORDER+=("app")
        log "  Created: app (${#CREATED_ORDER[@]})"
    fi
    if [ "$standalone_seen" = false ] && resource_exists "standalone"; then
        standalone_seen=true
        log "  Created: standalone (no deps)"
    fi

    # All chain resources created?
    if [ "$db_seen" = true ] && [ "$schema_seen" = true ] && [ "$app_seen" = true ]; then
        break
    fi
    sleep 1
done

# Validate creation order
if [ "${#CREATED_ORDER[@]}" -ge 3 ]; then
    log "  Creation order: ${CREATED_ORDER[*]}"

    db_idx=-1 schema_idx=-1 app_idx=-1
    for i in "${!CREATED_ORDER[@]}"; do
        case "${CREATED_ORDER[$i]}" in
            database) db_idx=$i ;;
            schema) schema_idx=$i ;;
            app) app_idx=$i ;;
        esac
    done

    if [ "$db_idx" -lt "$schema_idx" ] && [ "$schema_idx" -lt "$app_idx" ]; then
        pass "depends_on: creation order correct (database -> schema -> app)"
    else
        fail "depends_on: creation order wrong (expected database < schema < app, got: ${CREATED_ORDER[*]})"
    fi
else
    fail "depends_on: not all chain resources were created (got ${#CREATED_ORDER[@]}/3)"
fi

if [ "$standalone_seen" = true ]; then
    pass "depends_on: standalone (no deps) created"
else
    fail "depends_on: standalone resource not found"
fi

# Wait for full chain to reach Ready
log "Waiting for dependency chain to reach Ready..."
if wait_for_condition "xtest/test-depends-on" "Ready" 120; then
    pass "depends_on: full chain reached Ready"
else
    fail "depends_on: chain did not reach Ready within timeout"
fi

# Verify Usage resources exist (2 pairs: schema->database, app->schema)
usage_count=$(kubectl get "$USAGE_KIND" -o name 2>/dev/null | wc -l | tr -d ' ')
if [ "$usage_count" -ge 2 ] 2>/dev/null; then
    pass "depends_on: Usage resources created ($usage_count found)"
else
    fail "depends_on: expected >= 2 Usage resources, found $usage_count"
fi

# ============================================================
# TEST 8: DEPENDS_ON (DELETION ORDERING)
# ============================================================
log ""
log "===== TEST 8: DEPENDS_ON (DELETION ORDERING) ====="

log "Starting deletion watcher..."
DELETION_LOG=$(mktemp)

# Watch for DELETED events in real-time — captures the true ordering.
# Run in a process group so we can kill the entire pipeline.
set -m
kubectl get nopresource -l crossplane.io/composite=test-depends-on \
    --watch-only --output-watch-events -o json 2>/dev/null | \
    while IFS= read -r line; do
        type=$(echo "$line" | grep -o '"type":"[^"]*"' | head -1 | cut -d'"' -f4)
        if [ "$type" = "DELETED" ]; then
            name=$(echo "$line" | grep -o '"crossplane.io/composition-resource-name":"[^"]*"' | head -1 | cut -d'"' -f4)
            if [ -n "$name" ]; then
                echo "$name" >> "$DELETION_LOG"
                echo "    Watcher: $name deleted" >&2
            fi
        fi
    done &
WATCH_PID=$!
set +m
sleep 1  # let watcher start

log "Deleting depends_on XR (foreground cascade)..."
kubectl delete xtest test-depends-on --cascade=foreground --wait --timeout=180s 2>/dev/null || true

# Give a moment for final events, then kill the entire pipeline process group.
sleep 2
kill -- -$WATCH_PID 2>/dev/null || kill $WATCH_PID 2>/dev/null || true
wait $WATCH_PID 2>/dev/null || true

# Read deletion order (deduplicate — watch may emit multiple DELETED events per resource).
DELETED_ORDER=()
SEEN_LIST=""
while IFS= read -r name; do
    case ",$SEEN_LIST," in
        *",$name,"*) ;; # already seen
        *)
            SEEN_LIST="${SEEN_LIST:+$SEEN_LIST,}$name"
            DELETED_ORDER+=("$name")
            ;;
    esac
done < "$DELETION_LOG"
rm -f "$DELETION_LOG"

log "Deletion order captured: ${DELETED_ORDER[*]:-none}"

# Validate deletion order
if [ "${#DELETED_ORDER[@]}" -ge 3 ]; then
    log "  Deletion order: ${DELETED_ORDER[*]}"

    # app should be deleted before schema, schema before database
    app_idx=-1 schema_idx=-1 db_idx=-1
    for i in "${!DELETED_ORDER[@]}"; do
        case "${DELETED_ORDER[$i]}" in
            app) app_idx=$i ;;
            schema) schema_idx=$i ;;
            database) db_idx=$i ;;
        esac
    done

    if [ "$app_idx" -lt "$schema_idx" ] && [ "$schema_idx" -lt "$db_idx" ]; then
        pass "depends_on: deletion order correct (app -> schema -> database)"
    else
        fail "depends_on: deletion order wrong (expected app < schema < database, got: ${DELETED_ORDER[*]})"
    fi
else
    fail "depends_on: not all chain resources were deleted (got ${#DELETED_ORDER[@]}/3)"
fi

# Wait for XR itself to be gone
if wait_for_deletion "xtest/test-depends-on" 60; then
    pass "depends_on: XR fully deleted"
else
    fail "depends_on: XR not fully deleted"
fi

# ============================================================
# TEST 9: COMPOSITE READINESS GATING
# ============================================================
log ""
log "===== TEST 9: COMPOSITE READINESS GATING ====="

# --- Scenario A: auto-gate ---
log "Scenario A: auto-gate (Resource(when=False) without optional=True)"
kubectl apply -f "$SCRIPT_DIR/xr-composite-ready-gate.yaml"

if wait_for_reconciled "xtest/test-composite-ready-gate" 60; then
    pass "composite-ready/gate: XR reconciled by function"
else
    fail "composite-ready/gate: XR never Synced within timeout"
    kubectl get xtest/test-composite-ready-gate -o yaml 2>/dev/null || true
fi

# Ready should stay False (or Unknown). Must NOT be True.
gate_ready=$(get_condition_field "xtest/test-composite-ready-gate" "Ready" "status")
if [ "$gate_ready" = "True" ]; then
    fail "composite-ready/gate: XR Ready=$gate_ready (expected False; auto-gating failed)"
else
    pass "composite-ready/gate: XR Ready=$gate_ready (not True, as expected)"
fi

# A ComposedResourcesReady=False condition should be present.
gate_cond_status=$(get_condition_field "xtest/test-composite-ready-gate" "ComposedResourcesReady" "status")
gate_cond_reason=$(get_condition_field "xtest/test-composite-ready-gate" "ComposedResourcesReady" "reason")
gate_cond_msg=$(get_condition_field "xtest/test-composite-ready-gate" "ComposedResourcesReady" "message")
if [ "$gate_cond_status" = "False" ] && [ "$gate_cond_reason" = "PendingConditionalResources" ]; then
    pass "composite-ready/gate: ComposedResourcesReady=False with reason PendingConditionalResources"
else
    fail "composite-ready/gate: condition status=$gate_cond_status reason=$gate_cond_reason (expected False / PendingConditionalResources)"
fi
if echo "$gate_cond_msg" | grep -q "pending-dep"; then
    pass "composite-ready/gate: condition message names the skipped resource"
else
    fail "composite-ready/gate: condition message='$gate_cond_msg' (expected to mention pending-dep)"
fi

# --- Scenario B: optional opt-out ---
log "Scenario B: optional opt-out (Resource(when=False, optional=True))"
kubectl apply -f "$SCRIPT_DIR/xr-composite-ready-optional.yaml"

if wait_for_condition "xtest/test-composite-ready-optional" "Ready" 120; then
    pass "composite-ready/optional: XR reached Ready=True (optional skip did not gate)"
else
    fail "composite-ready/optional: XR did not reach Ready (optional=True should not gate)"
    kubectl get xtest/test-composite-ready-optional -o yaml 2>/dev/null || true
fi

# ComposedResourcesReady condition must NOT be present (optional skips don't emit it).
opt_cond_status=$(get_condition_field "xtest/test-composite-ready-optional" "ComposedResourcesReady" "status")
if [ -z "$opt_cond_status" ]; then
    pass "composite-ready/optional: no ComposedResourcesReady condition (as expected)"
else
    fail "composite-ready/optional: unexpected ComposedResourcesReady=$opt_cond_status (optional skips should not emit it)"
fi

# --- Scenario C: explicit set_composite_ready(False) ---
log "Scenario C: explicit set_composite_ready(False, reason=..., message=...)"
kubectl apply -f "$SCRIPT_DIR/xr-composite-ready-explicit.yaml"

if wait_for_reconciled "xtest/test-composite-ready-explicit" 60; then
    pass "composite-ready/explicit: XR reconciled by function"
else
    fail "composite-ready/explicit: XR never Synced within timeout"
fi

explicit_ready=$(get_condition_field "xtest/test-composite-ready-explicit" "Ready" "status")
if [ "$explicit_ready" = "True" ]; then
    fail "composite-ready/explicit: XR Ready=$explicit_ready (expected False; explicit override failed)"
else
    pass "composite-ready/explicit: XR Ready=$explicit_ready (not True, as expected)"
fi

explicit_cond_reason=$(get_condition_field "xtest/test-composite-ready-explicit" "ComposedResourcesReady" "reason")
explicit_cond_msg=$(get_condition_field "xtest/test-composite-ready-explicit" "ComposedResourcesReady" "message")
if [ "$explicit_cond_reason" = "WaitingForExternalSignal" ]; then
    pass "composite-ready/explicit: condition carries user-supplied reason"
else
    fail "composite-ready/explicit: condition reason='$explicit_cond_reason' (expected WaitingForExternalSignal)"
fi
if echo "$explicit_cond_msg" | grep -q "explicit set_composite_ready"; then
    pass "composite-ready/explicit: condition carries user-supplied message"
else
    fail "composite-ready/explicit: condition message='$explicit_cond_msg' (expected user-supplied message)"
fi

# --- Scenario D: Resource(ready=False) override ---
log "Scenario D: Resource(ready=False) keeps XR not-Ready despite MR Ready=True"
kubectl apply -f "$SCRIPT_DIR/xr-composite-ready-rfalse.yaml"

if wait_for_reconciled "xtest/test-composite-ready-rfalse" 60; then
    pass "composite-ready/ready-false: XR reconciled by function"
else
    fail "composite-ready/ready-false: XR never Synced within timeout"
    kubectl get xtest/test-composite-ready-rfalse -o yaml 2>/dev/null || true
fi

# The composed resource must exist (ready=False is not a skip)...
if kubectl get nopresource \
    -l "crossplane.io/composite=test-composite-ready-rfalse,function-starlark.crossplane.io/resource-name=never-ready" \
    -o name 2>/dev/null | grep -q .; then
    pass "composite-ready/ready-false: composed resource 'never-ready' exists"
else
    fail "composite-ready/ready-false: composed resource 'never-ready' not found"
fi

# ...and the MR itself reaches Ready=True (proving the override, not the MR
# state, is what blocks the XR). Poll: provider-nop sets the condition on its
# first reconcile, which can lag creation by a few seconds.
rfalse_mr_ready=""
for i in $(seq 1 20); do
    rfalse_mr_ready=$(kubectl get nopresource \
        -l "crossplane.io/composite=test-composite-ready-rfalse,function-starlark.crossplane.io/resource-name=never-ready" \
        -o jsonpath='{.items[0].status.conditions[?(@.type=="Ready")].status}' 2>/dev/null || echo "")
    if [ "$rfalse_mr_ready" = "True" ]; then
        break
    fi
    sleep 3
done
if [ "$rfalse_mr_ready" = "True" ]; then
    pass "composite-ready/ready-false: MR's own Ready condition is True"
else
    fail "composite-ready/ready-false: MR Ready='$rfalse_mr_ready' (expected True)"
fi

rfalse_ready=$(get_condition_field "xtest/test-composite-ready-rfalse" "Ready" "status")
if [ "$rfalse_ready" = "True" ]; then
    fail "composite-ready/ready-false: XR Ready=$rfalse_ready (expected not True; ready=False override failed)"
else
    pass "composite-ready/ready-false: XR Ready=$rfalse_ready (not True, as expected)"
fi

# --- Scenario E: Resource(ready=True) override ---
log "Scenario E: Resource(ready=True) makes XR Ready despite MR Ready=False"
kubectl apply -f "$SCRIPT_DIR/xr-composite-ready-rtrue.yaml"

if wait_for_condition "xtest/test-composite-ready-rtrue" "Ready" 120; then
    pass "composite-ready/ready-true: XR reached Ready=True (explicit override honored)"
else
    fail "composite-ready/ready-true: XR did not reach Ready (ready=True override failed)"
    kubectl get xtest/test-composite-ready-rtrue -o yaml 2>/dev/null || true
fi

# The MR's own Ready condition must be False (proving the override, not the MR
# state, is what made the XR Ready). The XR goes Ready immediately via the
# override, so poll: provider-nop may not have written the MR's own
# (intentionally False) Ready condition yet at this point.
rtrue_mr_ready=""
for i in $(seq 1 20); do
    rtrue_mr_ready=$(kubectl get nopresource \
        -l "crossplane.io/composite=test-composite-ready-rtrue,function-starlark.crossplane.io/resource-name=forced-ready" \
        -o jsonpath='{.items[0].status.conditions[?(@.type=="Ready")].status}' 2>/dev/null || echo "")
    if [ "$rtrue_mr_ready" = "False" ]; then
        break
    fi
    sleep 3
done
if [ "$rtrue_mr_ready" = "False" ]; then
    pass "composite-ready/ready-true: MR's own Ready condition is False (override did the work)"
else
    fail "composite-ready/ready-true: MR Ready='$rtrue_mr_ready' (expected False)"
fi

# ============================================================
# TEST 10: TRANSITIVE SKIP + DEPENDS_ON TOLERANCE
# ============================================================
log ""
log "===== TEST 10: TRANSITIVE SKIP + DEPENDS_ON TOLERANCE ====="

# --- Scenario A: transitive skip ---
log "Scenario A: transitive skip (Resource(when=False) + downstream depends_on=[upstream])"
kubectl apply -f "$SCRIPT_DIR/xr-transitive-skip.yaml"

if wait_for_reconciled "xtest/test-transitive-skip" 60; then
    pass "transitive-skip/cascade: XR reconciled by function (depends_on=[None] tolerated, no fatal)"
else
    fail "transitive-skip/cascade: XR never Synced within timeout"
    kubectl get xtest/test-transitive-skip -o yaml 2>/dev/null || true
fi

trans_ready=$(get_condition_field "xtest/test-transitive-skip" "Ready" "status")
if [ "$trans_ready" = "True" ]; then
    fail "transitive-skip/cascade: XR Ready=$trans_ready (expected False; transitive cascade failed)"
else
    pass "transitive-skip/cascade: XR Ready=$trans_ready (not True, as expected)"
fi

trans_cond_status=$(get_condition_field "xtest/test-transitive-skip" "ComposedResourcesReady" "status")
trans_cond_reason=$(get_condition_field "xtest/test-transitive-skip" "ComposedResourcesReady" "reason")
trans_cond_msg=$(get_condition_field "xtest/test-transitive-skip" "ComposedResourcesReady" "message")
if [ "$trans_cond_status" = "False" ] && [ "$trans_cond_reason" = "PendingConditionalResources" ]; then
    pass "transitive-skip/cascade: ComposedResourcesReady=False/PendingConditionalResources"
else
    fail "transitive-skip/cascade: condition status=$trans_cond_status reason=$trans_cond_reason (expected False / PendingConditionalResources)"
fi
if echo "$trans_cond_msg" | grep -q "upstream" && echo "$trans_cond_msg" | grep -q "downstream"; then
    pass "transitive-skip/cascade: condition message names both 'upstream' and 'downstream'"
else
    fail "transitive-skip/cascade: condition message='$trans_cond_msg' (expected to mention both upstream and downstream)"
fi

# --- Scenario B: optional cascade propagates transitive skip ---
log "Scenario B: optional cascade (When(optional=True) skip propagates to downstream via depends_on)"
kubectl apply -f "$SCRIPT_DIR/xr-transitive-skip-optional.yaml"

if wait_for_reconciled "xtest/test-transitive-skip-optional" 60; then
    pass "transitive-skip/optional-cascade: XR reconciled by function"
else
    fail "transitive-skip/optional-cascade: XR never Synced within timeout"
    kubectl get xtest/test-transitive-skip-optional -o yaml 2>/dev/null || true
fi

opt_ready=$(get_condition_field "xtest/test-transitive-skip-optional" "Ready" "status")
if [ "$opt_ready" != "True" ]; then
    pass "transitive-skip/optional-cascade: XR Ready=$opt_ready (not True, downstream transitively skipped)"
else
    fail "transitive-skip/optional-cascade: XR Ready=$opt_ready (expected not True; transitive skip should gate)"
fi

opt_cascade_cond=$(get_condition_field "xtest/test-transitive-skip-optional" "ComposedResourcesReady" "status")
opt_cascade_reason=$(get_condition_field "xtest/test-transitive-skip-optional" "ComposedResourcesReady" "reason")
if [ "$opt_cascade_cond" = "False" ] && [ "$opt_cascade_reason" = "PendingConditionalResources" ]; then
    pass "transitive-skip/optional-cascade: ComposedResourcesReady=False/PendingConditionalResources"
else
    fail "transitive-skip/optional-cascade: condition status=$opt_cascade_cond reason=$opt_cascade_reason (expected False / PendingConditionalResources)"
fi

# ============================================================
# TEST 13: CROSS-STEP CONTEXT + ENVIRONMENT GLOBAL
# ============================================================
log ""
log "===== TEST 13: CROSS-STEP CONTEXT + ENVIRONMENT GLOBAL ====="

log "Creating XR for context-env test"
kubectl apply -f "$SCRIPT_DIR/xr-context-env.yaml"

log "Waiting for context-env XR to become Ready..."
if wait_for_condition "xtest/test-context-env" "Ready" 120; then
    pass "context-env: XR reached Ready"
else
    fail "context-env: XR did not reach Ready"
    kubectl logs -n crossplane-system -l pkg.crossplane.io/function=function-starlark --tail=50 2>/dev/null || true
fi

env_region=$(get_status_field "xtest/test-context-env" "test.envRegion")
if [ "$env_region" = "eu-central-1" ]; then
    pass "context-env: environment global populated from well-known context key"
else
    fail "context-env: envRegion='$env_region' (expected eu-central-1)"
fi

env_tier=$(get_status_field "xtest/test-context-env" "test.envTier")
if [ "$env_tier" = "gold" ]; then
    pass "context-env: environment top-level field read"
else
    fail "context-env: envTier='$env_tier' (expected gold)"
fi

env_zone=$(get_status_field "xtest/test-context-env" "test.envZone")
if [ "$env_zone" = "a" ]; then
    pass "context-env: nested environment access via get() works"
else
    fail "context-env: envZone='$env_zone' (expected a)"
fi

cross_step=$(get_status_field "xtest/test-context-env" "test.crossStepContext")
if [ "$cross_step" = "from-step-one" ]; then
    pass "context-env: custom context key propagated across pipeline steps"
else
    fail "context-env: crossStepContext='$cross_step' (expected from-step-one)"
fi

# ============================================================
# TEST 14: EXTRA RESOURCES (require -> fulfill -> read)
# ============================================================
log ""
log "===== TEST 14: EXTRA RESOURCES (require -> fulfill -> read) ====="

# Pre-create two standalone cluster-scoped NopResources for the
# require_extra_resources(match_labels=...) lookup.
log "Creating standalone NopResources for extra-resources lookup"
kubectl apply -f - <<EOF
apiVersion: nop.crossplane.io/v1alpha1
kind: NopResource
metadata:
  name: e2e-extra-nop-1
  labels:
    e2e-extra: "true"
spec:
  forProvider:
    conditionAfter:
      - conditionType: Ready
        conditionStatus: "True"
        time: 0s
---
apiVersion: nop.crossplane.io/v1alpha1
kind: NopResource
metadata:
  name: e2e-extra-nop-2
  labels:
    e2e-extra: "true"
spec:
  forProvider:
    conditionAfter:
      - conditionType: Ready
        conditionStatus: "True"
        time: 0s
EOF

log "Creating XR for extra-resources test"
kubectl apply -f "$SCRIPT_DIR/xr-extra-resources.yaml"

log "Waiting for extra-resources XR to become Ready..."
if wait_for_condition "xtest/test-extra-resources" "Ready" 120; then
    pass "extra-resources: XR reached Ready"
else
    fail "extra-resources: XR did not reach Ready"
    kubectl logs -n crossplane-system -l pkg.crossplane.io/function=function-starlark --tail=50 2>/dev/null || true
fi

# Crossplane fulfills requirements within a single reconcile (iterative dance),
# but poll briefly in case the first Ready reconcile predates the nops.
log "Waiting for extra resources to be fulfilled..."
for i in $(seq 1 30); do
    extra_ready=$(get_status_field "xtest/test-extra-resources" "test.extraResourcesReady" 2>/dev/null || echo "")
    if [ "$extra_ready" = "true" ]; then
        break
    fi
    sleep 2
done

if [ "$extra_ready" = "true" ]; then
    pass "extra-resources: require -> fulfill -> read roundtrip completed"
else
    fail "extra-resources: extraResourcesReady='$extra_ready' (expected true)"
fi

extra_group=$(get_status_field "xtest/test-extra-resources" "test.extraXrdGroup")
if [ "$extra_group" = "e2e.fn-starlark.io" ]; then
    pass "extra-resources: get_extra_resource() dot-path read (match_name lookup)"
else
    fail "extra-resources: extraXrdGroup='$extra_group' (expected e2e.fn-starlark.io)"
fi

extra_kind=$(get_status_field "xtest/test-extra-resources" "test.extraXrdKind")
if [ "$extra_kind" = "CompositeResourceDefinition" ]; then
    pass "extra-resources: get_extra_resource() full-body read"
else
    fail "extra-resources: extraXrdKind='$extra_kind' (expected CompositeResourceDefinition)"
fi

extra_count=$(get_status_field "xtest/test-extra-resources" "test.extraNopCount")
if [ "$extra_count" = "2" ]; then
    pass "extra-resources: require_extra_resources(match_labels) matched both NopResources"
else
    fail "extra-resources: extraNopCount='$extra_count' (expected 2)"
fi

extra_names=$(get_status_field "xtest/test-extra-resources" "test.extraNopNamesSorted")
if [ "$extra_names" = "e2e-extra-nop-1,e2e-extra-nop-2" ]; then
    pass "extra-resources: get_extra_resources() mapped dot-path over all matches"
else
    fail "extra-resources: extraNopNamesSorted='$extra_names' (expected e2e-extra-nop-1,e2e-extra-nop-2)"
fi

extra_raw=$(get_status_field "xtest/test-extra-resources" "test.extraRawHasXrd")
if [ "$extra_raw" = "true" ]; then
    pass "extra-resources: raw extra_resources global contains requirement key"
else
    fail "extra-resources: extraRawHasXrd='$extra_raw' (expected true)"
fi

# ============================================================
# TEST 15: TUPLE DEPENDS_ON (FIELD-PATH READINESS)
# ============================================================
log ""
log "===== TEST 15: TUPLE DEPENDS_ON (FIELD-PATH READINESS) ====="

log "Phase 1: creating XR without signal (consumer must be deferred)"
# Reset any leftover XR from a previous run: phase 2 patches spec.signal onto
# it, and a plain re-apply would not remove that field (it was never part of
# the applied manifest), which would break the phase 1 deferral assertions.
if kubectl get xtest/test-fieldpath-dep &>/dev/null; then
    log "Deleting leftover test-fieldpath-dep XR from a previous run..."
    kubectl delete xtest test-fieldpath-dep --wait --timeout=120s 2>/dev/null || true
fi
kubectl apply -f "$SCRIPT_DIR/xr-fieldpath-dep.yaml"

if wait_for_reconciled "xtest/test-fieldpath-dep" 60; then
    pass "fieldpath-dep: XR reconciled by function"
else
    fail "fieldpath-dep: XR never Synced within timeout"
    kubectl get xtest/test-fieldpath-dep -o yaml 2>/dev/null || true
fi

# Producer must exist (no deps).
if kubectl get nopresource \
    -l "crossplane.io/composite=test-fieldpath-dep,function-starlark.crossplane.io/resource-name=producer" \
    -o name 2>/dev/null | grep -q .; then
    pass "fieldpath-dep: phase 1 - producer created"
else
    fail "fieldpath-dep: phase 1 - producer not found"
fi

# Consumer must be deferred (field path not yet truthy).
if kubectl get nopresource \
    -l "crossplane.io/composite=test-fieldpath-dep,function-starlark.crossplane.io/resource-name=consumer" \
    -o name 2>/dev/null | grep -q .; then
    fail "fieldpath-dep: phase 1 - consumer exists (should be deferred on field path)"
else
    pass "fieldpath-dep: phase 1 - consumer deferred until field path is truthy"
fi

fp_ready=$(get_condition_field "xtest/test-fieldpath-dep" "Ready" "status")
if [ "$fp_ready" = "True" ]; then
    fail "fieldpath-dep: phase 1 - XR Ready=$fp_ready (expected not True while consumer deferred)"
else
    pass "fieldpath-dep: phase 1 - XR Ready=$fp_ready (not True, as expected)"
fi

fp_cond_status=$(get_condition_field "xtest/test-fieldpath-dep" "ComposedResourcesReady" "status")
fp_cond_reason=$(get_condition_field "xtest/test-fieldpath-dep" "ComposedResourcesReady" "reason")
if [ "$fp_cond_status" = "False" ] && [ "$fp_cond_reason" = "WaitingForDependencies" ]; then
    pass "fieldpath-dep: phase 1 - ComposedResourcesReady=False with reason WaitingForDependencies"
else
    fail "fieldpath-dep: phase 1 - condition status=$fp_cond_status reason=$fp_cond_reason (expected False / WaitingForDependencies)"
fi

log "Phase 2: patching XR with signal (producer gains annotation, consumer unblocks)"
kubectl patch xtest/test-fieldpath-dep --type=merge -p '{"spec":{"signal":"go"}}'

# The producer's annotation must be applied and then OBSERVED on a subsequent
# reconcile before the consumer is emitted, so allow a generous window.
log "Waiting for consumer to be created after signal..."
consumer_created=false
FP_END=$((SECONDS + 150))
while [ $SECONDS -lt $FP_END ]; do
    if kubectl get nopresource \
        -l "crossplane.io/composite=test-fieldpath-dep,function-starlark.crossplane.io/resource-name=consumer" \
        -o name 2>/dev/null | grep -q .; then
        consumer_created=true
        break
    fi
    sleep 3
done

if [ "$consumer_created" = true ]; then
    pass "fieldpath-dep: phase 2 - consumer created once field path became truthy"
else
    fail "fieldpath-dep: phase 2 - consumer never created after signal"
fi

if wait_for_condition "xtest/test-fieldpath-dep" "Ready" 120; then
    pass "fieldpath-dep: phase 2 - XR reached Ready after dependency satisfied"
else
    fail "fieldpath-dep: phase 2 - XR did not reach Ready"
fi

# ============================================================
# TEST 16: USAGE API VERSION V2 (conditional, Crossplane 2.x only)
# ============================================================
log ""
log "===== TEST 16: USAGE API VERSION V2 (conditional) ====="

if [ "$RENDER_USAGE_V2" = true ]; then
    log "protection.crossplane.io available - running usageAPIVersion v2 test"
    kubectl apply -f "$SCRIPT_DIR/composition-usage-v2.yaml"
    kubectl apply -f "$SCRIPT_DIR/xr-usage-v2.yaml"

    if wait_for_condition "xtest/test-usage-v2" "Ready" 120; then
        pass "usage-v2: XR reached Ready"
    else
        fail "usage-v2: XR did not reach Ready"
    fi

    # Cluster-scoped XR + v2 API -> ClusterUsage (the v2 Usage kind is namespaced).
    v2_usage_count=$(kubectl get clusterusages.protection.crossplane.io -o name 2>/dev/null | wc -l | tr -d ' ')
    if [ "$v2_usage_count" -ge 1 ] 2>/dev/null; then
        pass "usage-v2: protection.crossplane.io ClusterUsage resource(s) created ($v2_usage_count found)"
    else
        fail "usage-v2: no protection.crossplane.io ClusterUsage resources found"
    fi

    kubectl delete xtest test-usage-v2 --wait=false 2>/dev/null || true
else
    log "SKIP: protection.crossplane.io not served (Crossplane 1.x cluster) - usageAPIVersion v2 not testable here"
fi

# ============================================================
# CLEANUP
# ============================================================
log ""
log "===== CLEANUP ====="

log "Deleting remaining test XRs..."
kubectl delete xtest test-builtins --wait=false 2>/dev/null || true
kubectl delete xtest test-oci --wait=false 2>/dev/null || true
kubectl delete xtest test-schemas --wait=false 2>/dev/null || true
kubectl delete xtest test-star-imports --wait=false 2>/dev/null || true
kubectl delete xtest test-relative-loads --wait=false 2>/dev/null || true
kubectl delete xtest test-path-modules --wait=false 2>/dev/null || true
kubectl delete xtest test-bundled-modules --wait=false 2>/dev/null || true
kubectl delete xtest test-mutable-struct --wait=false 2>/dev/null || true
kubectl delete xtest test-composite-ready-gate --wait=false 2>/dev/null || true
kubectl delete xtest test-composite-ready-optional --wait=false 2>/dev/null || true
kubectl delete xtest test-composite-ready-explicit --wait=false 2>/dev/null || true
kubectl delete xtest test-composite-ready-rfalse --wait=false 2>/dev/null || true
kubectl delete xtest test-composite-ready-rtrue --wait=false 2>/dev/null || true
kubectl delete xtest test-transitive-skip --wait=false 2>/dev/null || true
kubectl delete xtest test-transitive-skip-optional --wait=false 2>/dev/null || true
kubectl delete xtest test-context-env --wait=false 2>/dev/null || true
kubectl delete xtest test-extra-resources --wait=false 2>/dev/null || true
kubectl delete xtest test-fieldpath-dep --wait=false 2>/dev/null || true
kubectl delete nopresource e2e-extra-nop-1 e2e-extra-nop-2 --wait=false 2>/dev/null || true

# Wait for cleanup
sleep 10

# ============================================================
# RESULTS
# ============================================================
echo ""
echo "============================================================"
echo " E2E TEST RESULTS"
echo "============================================================"
echo ""
for t in "${TESTS[@]}"; do
    echo "  $t"
done
echo ""
echo "  Total: $((PASS + FAIL))  Passed: $PASS  Failed: $FAIL"
echo "============================================================"

# Teardown
if [ "$NO_TEARDOWN" = false ]; then
    log "Tearing down..."
    "$SCRIPT_DIR/teardown.sh"
else
    log "Skipping teardown (--no-teardown)"
    log "Cluster: kind-fn-starlark-e2e"
fi

# Exit code
if [ "$FAIL" -gt 0 ]; then
    exit 1
fi
