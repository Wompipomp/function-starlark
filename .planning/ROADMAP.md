# Roadmap: function-starlark

## Milestones

- ✅ **v1.0 Initial Release** — Phases 1-6 (shipped 2026-03-14)
- ✅ **v1.1 Composability & Lifecycle** — Phases 7-10 (shipped 2026-03-15)
- ✅ **v1.2 Ergonomics & Observability** — Phases 11-13 (shipped 2026-03-15)
- ✅ **v1.3 Lifecycle & Hardening** — Phases 14-15 (shipped 2026-03-16)
- ✅ **v1.4 Documentation & Labels** — Phases 16-17 (shipped 2026-03-16)
- 🚧 **v1.5 Builtins** — Phases 18-21 (in progress)

## Phases

<details>
<summary>✅ v1.0 Initial Release (Phases 1-6) — SHIPPED 2026-03-14</summary>

- [x] Phase 1: Foundation (3/3 plans) — completed 2026-03-13
- [x] Phase 2: Type Conversion (2/2 plans) — completed 2026-03-13
- [x] Phase 3: Starlark Runtime (2/2 plans) — completed 2026-03-13
- [x] Phase 4: Core Builtins (2/2 plans) — completed 2026-03-14
- [x] Phase 5: Extended Builtins (3/3 plans) — completed 2026-03-14
- [x] Phase 6: Hardening (4/4 plans) — completed 2026-03-14

Full details: `milestones/v1.0-ROADMAP.md`

</details>

<details>
<summary>✅ v1.1 Composability & Lifecycle (Phases 7-10) — SHIPPED 2026-03-15</summary>

- [x] Phase 7: Dependency Ordering & Tech Debt (5/5 plans) — completed 2026-03-15
- [x] Phase 8: Module Loading (2/2 plans) — completed 2026-03-15
- [x] Phase 9: OCI Distribution (2/2 plans) — completed 2026-03-15
- [x] Phase 10: Shared Library Ecosystem (2/2 plans) — completed 2026-03-15

Full details: `milestones/v1.1-ROADMAP.md`

</details>

<details>
<summary>✅ v1.2 Ergonomics & Observability (Phases 11-13) — SHIPPED 2026-03-15</summary>

- [x] Phase 11: Housekeeping (2/2 plans) — completed 2026-03-15
- [x] Phase 12: Builtin Ergonomics (2/2 plans) — completed 2026-03-15
- [x] Phase 13: Observability (2/2 plans) — completed 2026-03-15

Full details: `milestones/v1.2-ROADMAP.md`

</details>

<details>
<summary>✅ v1.3 Lifecycle & Hardening (Phases 14-15) — SHIPPED 2026-03-16</summary>

- [x] Phase 14: Hardening & Contracts (2/2 plans) — completed 2026-03-15
- [x] Phase 15: Creation Sequencing (2/2 plans) — completed 2026-03-16

Full details: `milestones/v1.3-ROADMAP.md`

</details>

<details>
<summary>✅ v1.4 Documentation & Labels (Phases 16-17) — SHIPPED 2026-03-16</summary>

- [x] Phase 16: Labels Kwarg (2/2 plans) — completed 2026-03-16
- [x] Phase 17: Comprehensive Documentation (3/3 plans) — completed 2026-03-16

Full details: `milestones/v1.4-ROADMAP.md`

</details>

### v1.5 Builtins (In Progress)

**Milestone Goal:** Add ergonomic builtins that eliminate common multi-step patterns and fix the dot-in-key correctness issue for label/annotation access.

- [x] **Phase 18: Metadata Access** — Safe label and annotation access that handles dotted keys correctly (completed 2026-03-16)
- [x] **Phase 19: Status Mutation** — Dot-path XR status writes with auto-created intermediates (completed 2026-03-16)
- [x] **Phase 20: Observed Access** — One-call observed resource field lookup (completed 2026-03-16)
- [ ] **Phase 21: Documentation** — Update all docs to cover new builtins with examples

## Phase Details

### Phase 18: Metadata Access
**Goal**: Users can safely read individual label and annotation values from any resource without dot-splitting corrupting dotted keys
**Depends on**: Nothing (first phase of v1.5)
**Requirements**: META-01, META-02, META-03, META-04
**Success Criteria** (what must be TRUE):
  1. User can call `get_label(xr, "app.kubernetes.io/name")` and receive the correct label value without the key being split on dots
  2. User can call `get_annotation(res, "crossplane.io/external-name")` and receive the correct annotation value without the key being split on dots
  3. Both builtins return the default value when the key is missing, the labels/annotations map is missing, or metadata itself is missing
  4. Both builtins work identically on `xr` (observed XR dict) and `observed` resource dicts
**Plans**: 1 plan

Plans:
- [ ] 18-01-PLAN.md — TDD: get_label and get_annotation builtins with shared metadataLookup helper

### Phase 19: Status Mutation
**Goal**: Users can write values into XR status at arbitrary dot-paths without destroying sibling fields or manually creating intermediate dicts
**Depends on**: Phase 18
**Requirements**: STAT-01, STAT-02, STAT-03, STAT-04
**Success Criteria** (what must be TRUE):
  1. User can call `set_xr_status("atProvider.projectId", pid)` and the value appears at `dxr.status.atProvider.projectId`
  2. Intermediate dicts are auto-created when path segments do not exist, so the user never manually builds nested dicts
  3. Two consecutive calls writing to sibling paths under the same prefix both persist (no clobbering)
  4. Auto-created intermediate dicts use `StarlarkDict` so dot-access notation (`dxr.status.atProvider.projectId`) works consistently throughout the status tree
**Plans**: 1 plan

Plans:
- [ ] 19-01-PLAN.md — TDD: set_xr_status builtin with dot-path walking and intermediate StarlarkDict creation

### Phase 20: Observed Access
**Goal**: Users can read fields from observed resources in a single call instead of the multi-step existence-check-then-get pattern
**Depends on**: Phase 19
**Requirements**: OBSV-01, OBSV-02, OBSV-03, OBSV-04
**Success Criteria** (what must be TRUE):
  1. User can call `get_observed("my-bucket", "status.atProvider.arn")` and receive the field value in one call
  2. Calling `get_observed` for a resource name that does not exist in observed state returns the default value without error
  3. Calling `get_observed` for a path that does not exist within an observed resource returns the default value without error
  4. Path traversal in `get_observed` uses the same `pathToKeys` logic as `get()`, so path semantics are identical
**Plans**: 1 plan

Plans:
- [ ] 20-01-PLAN.md — TDD: get_observed builtin with closure over observed dict and pathToKeys reuse

### Phase 21: Documentation
**Goal**: All new builtins are documented across every documentation artifact so users can discover and use them
**Depends on**: Phase 20
**Requirements**: DOCS-01, DOCS-02, DOCS-03, DOCS-04
**Success Criteria** (what must be TRUE):
  1. `llms.txt` contains entries for `get_label`, `get_annotation`, `set_xr_status`, and `get_observed` with correct signatures
  2. `docs/builtins-reference.md` has complete sections for each builtin including description, signature, parameters, return value, and usage examples
  3. `docs/features.md`, `docs/best-practices.md`, and `docs/migration-from-kcl.md` reference the new builtins in appropriate context
  4. The example composition demonstrates at least one new builtin in a realistic usage pattern
**Plans**: 2 plans

Plans:
- [x] 21-01-PLAN.md — Core reference docs: builtins-reference.md and llms.txt with full entries for all 4 new builtins
- [ ] 21-02-PLAN.md — Example composition rewrite + cross-doc updates (features, best-practices, migration, README)

## Progress

**Execution Order:**
Phases execute in numeric order: 18 -> 19 -> 20 -> 21

| Phase | Milestone | Plans Complete | Status | Completed |
|-------|-----------|----------------|--------|-----------|
| 1. Foundation | v1.0 | 3/3 | Complete | 2026-03-13 |
| 2. Type Conversion | v1.0 | 2/2 | Complete | 2026-03-13 |
| 3. Starlark Runtime | v1.0 | 2/2 | Complete | 2026-03-13 |
| 4. Core Builtins | v1.0 | 2/2 | Complete | 2026-03-14 |
| 5. Extended Builtins | v1.0 | 3/3 | Complete | 2026-03-14 |
| 6. Hardening | v1.0 | 4/4 | Complete | 2026-03-14 |
| 7. Dependency Ordering & Tech Debt | v1.1 | 5/5 | Complete | 2026-03-15 |
| 8. Module Loading | v1.1 | 2/2 | Complete | 2026-03-15 |
| 9. OCI Distribution | v1.1 | 2/2 | Complete | 2026-03-15 |
| 10. Shared Library Ecosystem | v1.1 | 2/2 | Complete | 2026-03-15 |
| 11. Housekeeping | v1.2 | 2/2 | Complete | 2026-03-15 |
| 12. Builtin Ergonomics | v1.2 | 2/2 | Complete | 2026-03-15 |
| 13. Observability | v1.2 | 2/2 | Complete | 2026-03-15 |
| 14. Hardening & Contracts | v1.3 | 2/2 | Complete | 2026-03-15 |
| 15. Creation Sequencing | v1.3 | 2/2 | Complete | 2026-03-16 |
| 16. Labels Kwarg | v1.4 | 2/2 | Complete | 2026-03-16 |
| 17. Comprehensive Documentation | v1.4 | 3/3 | Complete | 2026-03-16 |
| 18. Metadata Access | 1/1 | Complete    | 2026-03-16 | - |
| 19. Status Mutation | 1/1 | Complete    | 2026-03-16 | - |
| 20. Observed Access | 1/1 | Complete    | 2026-03-16 | - |
| 21. Documentation | v1.5 | 1/2 | In progress | - |
