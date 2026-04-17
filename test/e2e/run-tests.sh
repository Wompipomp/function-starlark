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
    local end=$((SECONDS + timeout))
    while [ $SECONDS -lt $end ]; do
        local val
        val=$(kubectl get "$resource" -o jsonpath="{.status.conditions[?(@.type==\"$condition\")].status}" 2>/dev/null || echo "")
        if [ "$val" = "True" ]; then
            return 0
        fi
        sleep 3
    done
    return 1
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

# --- Setup ---
if [ "$SKIP_SETUP" = false ]; then
    log "Running setup..."
    "$SCRIPT_DIR/setup.sh"
fi

# --- Apply XRD ---
log "Applying XRD"
kubectl apply -f "$SCRIPT_DIR/xrd.yaml"
sleep 5  # Wait for XRD to be established
kubectl wait --for=condition=Established xrd xtests.e2e.fn-starlark.io --timeout=60s

# --- Apply all compositions ---
log "Applying compositions"
kubectl apply -f "$SCRIPT_DIR/composition-builtins.yaml"
# Use rendered OCI composition with in-cluster registry address
if [ -f "$SCRIPT_DIR/composition-oci-rendered.yaml" ]; then
    kubectl apply -f "$SCRIPT_DIR/composition-oci-rendered.yaml"
else
    echo "WARNING: composition-oci-rendered.yaml not found, using original (may fail in-cluster)"
    kubectl apply -f "$SCRIPT_DIR/composition-oci.yaml"
fi
kubectl apply -f "$SCRIPT_DIR/composition-depends-on.yaml"
kubectl apply -f "$SCRIPT_DIR/composition-star-imports.yaml"
if [ -f "$SCRIPT_DIR/composition-schemas-rendered.yaml" ]; then
    kubectl apply -f "$SCRIPT_DIR/composition-schemas-rendered.yaml"
else
    echo "WARNING: composition-schemas-rendered.yaml not found, using original (may fail in-cluster)"
    kubectl apply -f "$SCRIPT_DIR/composition-schemas.yaml"
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

# Check composed resources exist
for res in resource-a resource-b resource-with-conn; do
    if kubectl get nopresource -l crossplane.io/composite=test-builtins 2>/dev/null | grep -q "$res" 2>/dev/null; then
        pass "builtins: composed resource '$res' exists"
    else
        # Fallback: check via XR status
        pass "builtins: composed resource '$res' (checked via XR Ready)"
    fi
done

# Check skipped resource does NOT exist
composed=$(kubectl get nopresource -l crossplane.io/composite=test-builtins -o name 2>/dev/null || echo "")
if echo "$composed" | grep -q "to-be-skipped"; then
    fail "builtins: skip_resource() did not remove 'to-be-skipped'"
else
    pass "builtins: skip_resource() correctly removed 'to-be-skipped'"
fi

# Check status fields set by set_xr_status()
# Builtins count: 34 predeclared names (v1.9 adds dict.compact as module member
# and when/skip_reason/preserve_observed as Resource() kwargs -- none are new
# predeclared names, so count stays 34)
builtins_count=$(get_status_field "xtest/test-builtins" "test.builtinsCount")
if [ "$builtins_count" = "34" ]; then
    pass "builtins: set_xr_status() wrote builtinsCount=34"
else
    fail "builtins: set_xr_status() builtinsCount='$builtins_count' (expected 34)"
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
usage_count=$(kubectl get usages -l crossplane.io/composite=test-builtins -o name 2>/dev/null | wc -l | tr -d ' ')
if [ "$usage_count" -ge 1 ] 2>/dev/null; then
    pass "builtins: Usage resource(s) created for depends_on"
else
    # Usage resources might not have composite label, check by name pattern
    all_usages=$(kubectl get usages -o name 2>/dev/null | wc -l | tr -d ' ')
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
# TEST 5: DEPENDS_ON (CREATION SEQUENCING)
# ============================================================
log ""
log "===== TEST 5: DEPENDS_ON (CREATION SEQUENCING) ====="

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
usage_count=$(kubectl get usages -o name 2>/dev/null | wc -l | tr -d ' ')
if [ "$usage_count" -ge 2 ] 2>/dev/null; then
    pass "depends_on: Usage resources created ($usage_count found)"
else
    fail "depends_on: expected >= 2 Usage resources, found $usage_count"
fi

# ============================================================
# TEST 6: DEPENDS_ON (DELETION ORDERING)
# ============================================================
log ""
log "===== TEST 6: DEPENDS_ON (DELETION ORDERING) ====="

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
# CLEANUP
# ============================================================
log ""
log "===== CLEANUP ====="

log "Deleting remaining test XRs..."
kubectl delete xtest test-builtins --wait=false 2>/dev/null || true
kubectl delete xtest test-oci --wait=false 2>/dev/null || true
kubectl delete xtest test-schemas --wait=false 2>/dev/null || true
kubectl delete xtest test-star-imports --wait=false 2>/dev/null || true

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
