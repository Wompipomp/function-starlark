# E2E test: filesystem relative-path loading.
# Validates MOD-01 (sibling load) and MOD-05 (nested chaining + star imports).

# MOD-01: sibling relative load
load("./helper.star", "greet")

# MOD-05: star import from relative module
load("./utils.star", "*")

msg = greet("e2e")
if msg != "hello, e2e":
    fatal("relative load: greet returned %r, want 'hello, e2e'" % msg)

# utils.star exports compute (loaded via star import)
val = compute(6, 7)
if val != 42:
    fatal("star import: compute(6,7) = %d, want 42" % val)

# MOD-05: nested chaining — helper.star internally loads ./utils.star
# and re-exports nested_val which comes from utils.star
load("./helper.star", "nested_val")
if nested_val != 99:
    fatal("nested chain: nested_val = %d, want 99" % nested_val)

Resource("relative-load-result", {
    "apiVersion": "nop.crossplane.io/v1alpha1",
    "kind": "NopResource",
    "spec": {
        "forProvider": {
            "conditionAfter": [
                {"conditionType": "Ready", "conditionStatus": "True", "time": "0s"},
                {"conditionType": "Synced", "conditionStatus": "True", "time": "0s"},
            ],
        },
    },
})

set_xr_status("test.relativeLoadsWorked", True)
set_xr_status("test.greetResult", msg)
set_xr_status("test.computeResult", val)
set_xr_status("test.nestedChainResult", nested_val)
