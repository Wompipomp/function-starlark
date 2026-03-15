package runtime

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"go.starlark.net/starlark"
	"go.starlark.net/syntax"
)

// LoadFunc is the signature for Thread.Load callbacks.
type LoadFunc func(thread *starlark.Thread, module string) (starlark.StringDict, error)

// ModuleLoader resolves and executes Starlark modules with caching and
// cycle detection. It is created per-reconciliation in fn.go so that each
// reconciliation gets a fresh module globals cache (while sharing the
// bytecode compilation cache via Runtime.getOrCompile).
type ModuleLoader struct {
	inlineModules map[string]string   // name -> source from StarlarkInput.modules
	searchPaths   []string            // ordered filesystem directories
	predeclared   starlark.StringDict // same builtins as main script
	cache         map[string]*moduleEntry
	rt            *Runtime // for bytecode caching and logging
}

// moduleEntry stores the cached result of loading a module.
// A nil entry in the cache map (key present, value nil) indicates a
// module that is currently being loaded -- used for cycle detection.
type moduleEntry struct {
	globals starlark.StringDict
	err     error
}

// NewModuleLoader creates a ModuleLoader with the given inline modules,
// filesystem search paths, predeclared builtins, and runtime for caching.
func NewModuleLoader(inlineModules map[string]string, searchPaths []string, predeclared starlark.StringDict, rt *Runtime) *ModuleLoader {
	if inlineModules == nil {
		inlineModules = map[string]string{}
	}
	return &ModuleLoader{
		inlineModules: inlineModules,
		searchPaths:   searchPaths,
		predeclared:   predeclared,
		cache:         make(map[string]*moduleEntry),
		rt:            rt,
	}
}

// LoadFunc returns a function suitable for use as Thread.Load.
func (m *ModuleLoader) LoadFunc() LoadFunc {
	return m.load
}

// validateModuleName checks that a module name is valid:
// no slashes, must end in .star. OCI references (oci://) are handled
// separately in load() before this check is reached.
func validateModuleName(module string) error {
	if strings.Contains(module, "/") || strings.Contains(module, "\\") {
		return fmt.Errorf("module name %q must not contain path separators", module)
	}
	if !strings.HasSuffix(module, ".star") {
		return fmt.Errorf("module name %q must end with .star", module)
	}
	return nil
}

// ociBaseFilename extracts the base filename from an oci:// URL.
// For "oci://ghcr.io/org/lib:v1/helpers.star" it returns "helpers.star".
func ociBaseFilename(module string) string {
	idx := strings.LastIndex(module, "/")
	if idx == -1 {
		return module
	}
	return module[idx+1:]
}

// resolve returns the source for a module by checking inline modules first,
// then searching filesystem paths in order.
func (m *ModuleLoader) resolve(module string) (string, error) {
	var searched []string

	// Inline modules first.
	if src, ok := m.inlineModules[module]; ok {
		return src, nil
	}
	searched = append(searched, "inline modules")

	// Filesystem search paths.
	for _, dir := range m.searchPaths {
		path := filepath.Join(dir, module)
		data, err := os.ReadFile(path) //nolint:gosec // paths from trusted config
		if err == nil {
			return string(data), nil
		}
		if !os.IsNotExist(err) {
			return "", fmt.Errorf("reading module %q from %s: %w", module, dir, err)
		}
		searched = append(searched, dir)
	}

	return "", fmt.Errorf("module %q not found; searched: %s", module, strings.Join(searched, ", "))
}

// load implements the Thread.Load callback. It uses the sequential loader
// pattern from starlark-go's example_test.go with nil-sentinel cycle detection.
func (m *ModuleLoader) load(_ *starlark.Thread, module string) (starlark.StringDict, error) {
	// Handle OCI module references: route to inline modules by base filename.
	if strings.HasPrefix(module, "oci://") {
		baseName := ociBaseFilename(module)
		if _, ok := m.inlineModules[baseName]; !ok {
			return nil, fmt.Errorf(
				"OCI module %q not resolved; ensure the OCI reference was resolvable before execution",
				baseName,
			)
		}
		// Use the base filename as the cache key and module name.
		module = baseName
	}

	if err := validateModuleName(module); err != nil {
		return nil, err
	}

	e, ok := m.cache[module]
	if e != nil {
		// Already loaded (success or error cached).
		return e.globals, e.err
	}
	if ok {
		// Key exists but value is nil: loading in progress -> cycle.
		return nil, fmt.Errorf("cycle in load graph: %s", module)
	}

	// Mark as loading in progress (nil sentinel).
	m.cache[module] = nil

	// Resolve module source.
	source, err := m.resolve(module)
	if err != nil {
		e = &moduleEntry{nil, err}
		m.cache[module] = e
		return nil, err
	}

	// Compile via Runtime's bytecode cache (shared across reconciliations).
	prog, err := m.rt.getOrCompile(source, m.predeclared, module)
	if err != nil {
		e = &moduleEntry{nil, fmt.Errorf("compiling module %s: %w", module, err)}
		m.cache[module] = e
		return nil, e.err
	}

	// Create a new thread for this module with its own step counter.
	thread := &starlark.Thread{
		Name: module,
		Load: m.load, // recursive: modules can load other modules
		Print: func(_ *starlark.Thread, msg string) {
			m.rt.log.Debug("starlark print", "module", module, "msg", msg)
		},
	}
	thread.SetMaxExecutionSteps(maxSteps)

	// Execute the module.
	globals, err := prog.Init(thread, m.predeclared)
	if err != nil {
		// Wrap EvalError to include backtrace with module filename,
		// mirroring Runtime.Execute error handling.
		var wrappedErr error
		if thread.ExecutionSteps() >= maxSteps {
			wrappedErr = fmt.Errorf(
				"module %s exceeded execution limit (%d steps): possible infinite loop",
				module, maxSteps,
			)
		} else {
			var evalErr *starlark.EvalError
			if errors.As(err, &evalErr) {
				wrappedErr = fmt.Errorf("error in module %s: %s: %w", module, evalErr.Backtrace(), err)
			} else {
				wrappedErr = fmt.Errorf("error in module %s: %w", module, err)
			}
		}
		e = &moduleEntry{nil, wrappedErr}
		m.cache[module] = e
		return nil, wrappedErr
	}

	// Freeze globals manually since we use getOrCompile + prog.Init
	// instead of ExecFileOptions (which auto-freezes).
	globals.Freeze()

	// Filter out underscore-prefixed private names.
	exported := make(starlark.StringDict, len(globals))
	for name, val := range globals {
		if !strings.HasPrefix(name, "_") {
			exported[name] = val
		}
	}

	e = &moduleEntry{exported, nil}
	m.cache[module] = e
	return e.globals, nil
}

// ResolveStarImports preprocesses Starlark source to expand star imports.
// A star import like load("m.star", "*") is rewritten to load("m.star", "x", "y")
// where x, y are the module's non-underscore exports. If no star imports are
// found, the source is returned unchanged.
//
// This is needed because starlark-go does not natively support "*" in load()
// statements -- it would look for a literal key named "*" in the loaded dict.
func (m *ModuleLoader) ResolveStarImports(source, filename string) (string, error) {
	opts := fileOptions()
	f, err := opts.Parse(filename, source, 0)
	if err != nil {
		// Parse errors are non-fatal: return source unchanged. The actual
		// compilation step will produce a proper error message.
		return source, nil //nolint:nilerr
	}

	// Collect star import load statements.
	type starLoad struct {
		stmt *syntax.LoadStmt
	}
	var starLoads []starLoad

	for _, stmt := range f.Stmts {
		load, ok := stmt.(*syntax.LoadStmt)
		if !ok {
			continue
		}
		for _, ident := range load.To {
			if ident.Name == "*" {
				starLoads = append(starLoads, starLoad{stmt: load})
				break
			}
		}
	}

	if len(starLoads) == 0 {
		return source, nil
	}

	// For each star import, resolve the module and get its export names.
	// We process replacements from end to start to preserve character offsets.
	lines := strings.Split(source, "\n")
	for i := len(starLoads) - 1; i >= 0; i-- {
		sl := starLoads[i]
		mod := sl.stmt.ModuleName()

		// Resolve the module name (handle OCI routing).
		resolvedMod := mod
		if strings.HasPrefix(mod, "oci://") {
			resolvedMod = ociBaseFilename(mod)
		}

		exports, err := m.getModuleExports(resolvedMod)
		if err != nil {
			return "", fmt.Errorf("resolving star import from %q: %w", mod, err)
		}

		// Build the new load argument list.
		var names []string
		// Collect non-star names that are already explicitly listed.
		existingNames := make(map[string]bool)
		for _, ident := range sl.stmt.To {
			if ident.Name != "*" {
				existingNames[ident.Name] = true
			}
		}

		// Add all export names not already listed.
		for _, name := range exports {
			if !existingNames[name] {
				names = append(names, name)
			}
		}

		// Build the replacement load statement text.
		var allArgs []string
		// Preserve explicitly named imports (using From for aliased names).
		for j, ident := range sl.stmt.To {
			if ident.Name == "*" {
				continue
			}
			from := sl.stmt.From[j]
			if from.Name == ident.Name {
				allArgs = append(allArgs, fmt.Sprintf("%q", ident.Name))
			} else {
				allArgs = append(allArgs, fmt.Sprintf("%s=%q", ident.Name, from.Name))
			}
		}
		// Add the star-expanded names.
		for _, name := range names {
			allArgs = append(allArgs, fmt.Sprintf("%q", name))
		}

		// If no exports and no explicit names, remove the load statement entirely.
		if len(allArgs) == 0 {
			// Remove the entire line(s) for this load statement.
			startLine := int(sl.stmt.Load.Line) - 1 // 1-indexed to 0-indexed
			// Find end by looking at Rparen position.
			endLine := int(sl.stmt.Rparen.Line) - 1
			for l := startLine; l <= endLine && l < len(lines); l++ {
				lines[l] = ""
			}
			continue
		}

		newLoad := fmt.Sprintf("load(%q, %s)", mod, strings.Join(allArgs, ", "))

		// Replace the original load line(s).
		startLine := int(sl.stmt.Load.Line) - 1
		endLine := int(sl.stmt.Rparen.Line) - 1
		lines[startLine] = newLoad
		// Clear any continuation lines.
		for l := startLine + 1; l <= endLine && l < len(lines); l++ {
			lines[l] = ""
		}
	}

	// Rejoin and clean up empty lines left by removal.
	result := strings.Join(lines, "\n")
	return result, nil
}

// getModuleExports resolves a module and returns its sorted public export names.
func (m *ModuleLoader) getModuleExports(module string) ([]string, error) {
	// Resolve module source.
	src, err := m.resolve(module)
	if err != nil {
		return nil, err
	}

	// Compile and execute the module to get its exports.
	prog, err := m.rt.getOrCompile(src, m.predeclared, module)
	if err != nil {
		return nil, fmt.Errorf("compiling module %s for star import: %w", module, err)
	}

	thread := &starlark.Thread{
		Name: module + " (star-import-scan)",
		Load: m.load,
		Print: func(_ *starlark.Thread, _ string) {
			// Suppress print during export scanning.
		},
	}
	thread.SetMaxExecutionSteps(maxSteps)

	globals, err := prog.Init(thread, m.predeclared)
	if err != nil {
		return nil, fmt.Errorf("executing module %s for star import: %w", module, err)
	}

	var names []string
	for name := range globals {
		if !strings.HasPrefix(name, "_") {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names, nil
}
