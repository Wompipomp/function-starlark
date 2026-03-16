# Requirements: function-starlark

**Defined:** 2026-03-16
**Core Value:** Platform engineers can write readable, expressive compositions that run with minimal resource overhead

## v1.5 Requirements

Requirements for v1.5 Builtins milestone. Each maps to roadmap phases.

### Metadata Access

- [x] **META-01**: User can call `get_label(res, key, default)` to safely read a single label value without dot-path splitting
- [x] **META-02**: User can call `get_annotation(res, key, default)` to safely read a single annotation value without dot-path splitting
- [x] **META-03**: `get_label` and `get_annotation` return the default value when the key, labels map, or metadata is missing
- [x] **META-04**: `get_label` and `get_annotation` work with both `xr` (observed) and `observed` resource dicts

### Status Mutation

- [x] **STAT-01**: User can call `set_xr_status(path, value)` to write a value at a dot-separated path under `dxr["status"]`
- [x] **STAT-02**: `set_xr_status` auto-creates intermediate dicts when path segments don't exist
- [x] **STAT-03**: `set_xr_status` preserves existing sibling keys when writing to a shared prefix
- [x] **STAT-04**: `set_xr_status` uses `*convert.StarlarkDict` for intermediates to maintain dot-access consistency

### Observed Access

- [x] **OBSV-01**: User can call `get_observed(name, path, default)` to read a field from an observed resource in one call
- [x] **OBSV-02**: `get_observed` returns the default when the resource doesn't exist in observed state
- [x] **OBSV-03**: `get_observed` returns the default when the path doesn't exist within the resource
- [x] **OBSV-04**: `get_observed` reuses existing `pathToKeys` for path traversal consistency with `get()`

### Documentation

- [ ] **DOCS-01**: llms.txt updated with entries for all four new builtins
- [ ] **DOCS-02**: builtins-reference.md updated with full documentation and examples for each builtin
- [ ] **DOCS-03**: features.md, best-practices.md, and migration-from-kcl.md updated to reference new builtins
- [ ] **DOCS-04**: Example composition updated to use new builtins where applicable

## Future Requirements

None deferred — all identified features included in v1.5.

## Out of Scope

| Feature | Reason |
|---------|--------|
| `set_xr_spec()` builtin | Status is the common write target; spec writes are rare and risky |
| Batch label/annotation access (`get_labels(res)`) | Single-key access covers all real composition patterns |
| `get_desired()` convenience builtin | Desired state is written, not read — different access pattern |
| Prometheus metrics for new builtins | Existing reconciliation metrics suffice; no new counters needed |

## Traceability

Which phases cover which requirements. Updated during roadmap creation.

| Requirement | Phase | Status |
|-------------|-------|--------|
| META-01 | Phase 18 | Complete |
| META-02 | Phase 18 | Complete |
| META-03 | Phase 18 | Complete |
| META-04 | Phase 18 | Complete |
| STAT-01 | Phase 19 | Complete |
| STAT-02 | Phase 19 | Complete |
| STAT-03 | Phase 19 | Complete |
| STAT-04 | Phase 19 | Complete |
| OBSV-01 | Phase 20 | Complete |
| OBSV-02 | Phase 20 | Complete |
| OBSV-03 | Phase 20 | Complete |
| OBSV-04 | Phase 20 | Complete |
| DOCS-01 | Phase 21 | Pending |
| DOCS-02 | Phase 21 | Pending |
| DOCS-03 | Phase 21 | Pending |
| DOCS-04 | Phase 21 | Pending |

**Coverage:**
- v1.5 requirements: 16 total
- Mapped to phases: 16
- Unmapped: 0

---
*Requirements defined: 2026-03-16*
*Last updated: 2026-03-16 after roadmap creation*
