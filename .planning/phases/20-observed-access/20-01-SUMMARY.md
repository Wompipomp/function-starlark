---
phase: 20-observed-access
plan: 01
subsystem: api
tags: [starlark, builtins, observed, dict-access, pathToKeys]

# Dependency graph
requires:
  - phase: 18-metadata-access
    provides: metadataLookup pattern, pathToKeys reuse precedent
provides:
  - get_observed(name, path, default=None) builtin for one-call observed resource field lookup
affects: []

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Closure-captured dict lookup composed with pathToKeys + Mapping walk"

key-files:
  created: []
  modified:
    - builtins/builtins.go
    - builtins/builtins_test.go

key-decisions:
  - "name param is Go string in UnpackArgs (auto type-checks, no manual validation needed)"
  - "None-as-missing throughout entire lookup chain: observed.Get, non-Mapping, path miss, None at leaf"
  - "No dot-validation on path (follow get() behavior, not set_xr_status strictness)"

patterns-established:
  - "Observed resource accessor: closure captures observed dict, composes name lookup with path walk"

requirements-completed: [OBSV-01, OBSV-02, OBSV-03, OBSV-04]

# Metrics
duration: 3min
completed: 2026-03-16
---

# Phase 20 Plan 01: Observed Access Summary

**get_observed(name, path, default) builtin composing observed dict name lookup with pathToKeys + Mapping walk from get()**

## Performance

- **Duration:** 3 min
- **Started:** 2026-03-16T20:34:21Z
- **Completed:** 2026-03-16T20:37:18Z
- **Tasks:** 1 (TDD: RED + GREEN)
- **Files modified:** 2

## Accomplishments
- Implemented get_observed builtin that replaces the two-step `if name in observed: val = get(observed[name], path)` pattern with a single `val = get_observed(name, path)` call
- Full validation: empty name/path errors, pathToKeys type checking, None-as-missing throughout
- 17 table-driven test cases covering OBSV-01 through OBSV-04 plus validation and edge cases

## Task Commits

Each task was committed atomically:

1. **Task 1 RED: TDD get_observed tests** - `84f13ae` (test)
2. **Task 1 GREEN: implement get_observed** - `7afecaa` (feat)

## Files Created/Modified
- `builtins/builtins.go` - Added getObservedImpl function, closure registration in BuildGlobals, doc comment update
- `builtins/builtins_test.go` - Added TestGetObserved (17 subtests), updated TestBuildGlobals_Keys to 19 entries

## Decisions Made
- name param as Go string in UnpackArgs: Starlark auto-validates string type, no manual check needed
- None-as-missing throughout entire lookup chain: consistent with get() behavior
- No dot-validation on path: follows get() lenient behavior, not set_xr_status strictness
- Empty path (both "" and []) rejected with error: catches likely bugs

## Deviations from Plan

None - plan executed exactly as written.

## Issues Encountered
None

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- get_observed builtin complete and tested
- BuildGlobals now has 19 entries
- Ready for next phase

## Self-Check: PASSED

---
*Phase: 20-observed-access*
*Completed: 2026-03-16*
