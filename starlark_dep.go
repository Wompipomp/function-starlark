package main

// Ensure go.starlark.net is retained as a dependency.
// This import proves the Starlark interpreter compiles with zero CGo.
// Actual usage begins in Phase 3 (Starlark Runtime).
import _ "go.starlark.net/starlark"
