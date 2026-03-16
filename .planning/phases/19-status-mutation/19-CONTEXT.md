# Phase 19: Status Mutation - Context

**Gathered:** 2026-03-16
**Status:** Ready for planning

<domain>
## Phase Boundary

A `set_xr_status(path, value)` builtin that lets platform engineers write values into `dxr["status"]` at arbitrary dot-paths without destroying sibling fields or manually creating intermediate dicts. Auto-created intermediates use `*convert.StarlarkDict` so dot-access notation works consistently throughout the status tree.

</domain>

<decisions>
## Implementation Decisions

### Path format
- Dot-strings only — no list path support (unlike `get()` which accepts both)
- Status paths are always simple like `"atProvider.projectId"` — no dotted keys to worry about
- Single-segment paths allowed: `set_xr_status("ready", True)` writes `dxr.status.ready`
- Empty path strings rejected with error — same pattern as `get_label` rejecting empty keys
- Malformed paths rejected: leading/trailing dots or consecutive dots (e.g., `".foo"`, `"foo."`, `"foo..bar"`) produce error rather than creating empty-string keys

### DXR access pattern
- Closure over dxr global — builtin captures dxr at construction time in `BuildGlobals`
- Call signature: `set_xr_status(path, value)` — no explicit dxr parameter since status mutation is always on dxr
- Constructed directly in `BuildGlobals` as a closure, not as a method on any collector
- Both `path` and `value` are required positional arguments

### Value handling
- Accepts any Starlark value (strings, ints, dicts, lists, bools, None) — `StarlarkToStruct` handles conversion at the end via `ApplyDXR`
- Writing `None` stores None at the path (converted to null in protobuf) — does not delete the key
- Writing a dict value replaces whatever was at that path (no deep merge) — users use deeper paths for surgical writes
- Two usage patterns: individual `set_xr_status("atProvider.projectId", pid)` or bulk `set_xr_status("atProvider", {"projectId": pid, "region": region})`

### Intermediate creation
- Auto-creates the `"status"` key on dxr if it doesn't exist yet — users never need `dxr["status"] = {}` first
- Auto-creates all intermediate dicts along the path as `*convert.StarlarkDict` (not plain `*starlark.Dict`)
- If a non-dict value sits at an intermediate path segment, overwrite it silently with a new StarlarkDict (mkdir -p semantics)

### Error handling
- Let Starlark frozen-mutation errors propagate naturally — no custom wrapper for frozen dxr
- Returns `None` — pure side-effect, consistent with `set_condition`, `emit_event`, `set_connection_details`

### Claude's Discretion
- Internal Go function organization and file placement
- Test strategy and table-driven test structure
- Whether to extract path validation into a shared helper

</decisions>

<code_context>
## Existing Code Insights

### Reusable Assets
- `pathToKeys` (`builtins/builtins.go:237-254`): Existing path splitting logic — NOT reused since set_xr_status uses dot-strings only with custom validation (reject empty segments)
- `BuildGlobals` (`builtins/builtins.go:32-94`): Registration point — add `set_xr_status` entry with dxr closure
- `convert.NewStarlarkDict` (`convert/starlark_dict.go:45-48`): Used for creating intermediate dicts along the path
- `StarlarkDict.SetField` / `SetKey`: Used for writing values at each path level

### Established Patterns
- Standalone builtin functions registered in `BuildGlobals` return dict
- `starlark.UnpackArgs` for parameter parsing
- Mutation builtins (set_condition, emit_event, set_connection_details) all return `starlark.None`
- `StarlarkDict` implements `HasSetField` and `Mapping` interfaces — used for intermediate navigation and mutation

### Integration Points
- `BuildGlobals` return dict — add `"set_xr_status"` entry (predeclared count increases from 16 to 17)
- dxr variable already in scope in `BuildGlobals` — closure captures it directly
- `ApplyDXR` (`builtins/builtins.go:284-303`) converts the mutated dxr back to protobuf — no changes needed there

</code_context>

<specifics>
## Specific Ideas

- The main value proposition over `dxr["status"] = {...}` is preserving sibling keys from prior pipeline steps — the dict-literal approach clobbers everything under status
- For bulk writes, users can write a dict at a prefix: `set_xr_status("atProvider", {"projectId": pid, "region": region})` — this replaces at that level but preserves other top-level status siblings

</specifics>

<deferred>
## Deferred Ideas

None — discussion stayed within phase scope

</deferred>

---

*Phase: 19-status-mutation*
*Context gathered: 2026-03-16*
