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

	"github.com/wompipomp/function-starlark/runtime/oci"
)

// LoadFunc is the signature for Thread.Load callbacks.
type LoadFunc func(thread *starlark.Thread, module string) (starlark.StringDict, error)

// starImportScanSuffix is appended to thread names inside getModuleExports so
// the scan thread is identifiable. load() strips it before using the name as
// an OCI parent reference.
const starImportScanSuffix = " (star-import-scan)"

// ModuleLoader resolves and executes Starlark modules with caching and
// cycle detection. It is created per-reconciliation in fn.go so that each
// reconciliation gets a fresh module globals cache (while sharing the
// bytecode compilation cache via Runtime.getOrCompile).
type ModuleLoader struct {
	inlineModules   map[string]string   // name -> source from StarlarkInput.modules
	searchPaths     []string            // ordered filesystem directories
	predeclared     starlark.StringDict // same builtins as main script
	cache           map[string]*moduleEntry
	expanding       map[string]bool // modules currently being star-expanded (cycle guard)
	rt              *Runtime        // for bytecode caching and logging
	defaultRegistry string          // default OCI registry for short-form load targets
}

// moduleEntry stores the cached result of loading a module.
// A nil entry in the cache map (key present, value nil) indicates a
// module that is currently being loaded -- used for cycle detection.
type moduleEntry struct {
	globals starlark.StringDict
	err     error
}

// NewModuleLoader creates a ModuleLoader with the given inline modules,
// filesystem search paths, predeclared builtins, runtime for caching, and
// default registry for short-form OCI load targets.
func NewModuleLoader(inlineModules map[string]string, searchPaths []string, predeclared starlark.StringDict, rt *Runtime, defaultRegistry string) *ModuleLoader {
	if inlineModules == nil {
		inlineModules = map[string]string{}
	}
	return &ModuleLoader{
		inlineModules:   inlineModules,
		searchPaths:     searchPaths,
		predeclared:     predeclared,
		cache:           make(map[string]*moduleEntry),
		expanding:       make(map[string]bool),
		rt:              rt,
		defaultRegistry: defaultRegistry,
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
	if strings.HasSuffix(module, "_test.star") {
		return fmt.Errorf("module name %q is a test file and cannot be loaded as a module", module)
	}
	return nil
}

// isFilesystemRelativeTarget checks if a module name is a filesystem-relative
// load target: starts with "./" and ends with ".star". Unlike
// oci.IsPackageLocalTarget, subdirectories are allowed (e.g., "./sub/x.star").
func isFilesystemRelativeTarget(module string) bool {
	return strings.HasPrefix(module, "./") && strings.HasSuffix(module, ".star")
}

// isFilesystemCaller reports whether the thread's Name indicates a
// filesystem-sourced module (contains "/" but does not start with "oci://").
func isFilesystemCaller(threadName string) bool {
	return strings.Contains(threadName, "/") && !strings.HasPrefix(threadName, "oci://")
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
func (m *ModuleLoader) load(thread *starlark.Thread, module string) (starlark.StringDict, error) {
	// Relative ./ and ../ loads: determine caller context and dispatch.
	// Reject ../ early with a user-friendly path-traversal error.
	if strings.HasPrefix(module, "../") {
		return nil, fmt.Errorf(
			"relative load %q rejected: path must not escape the caller's directory",
			module,
		)
	}
	if strings.HasPrefix(module, "./") {
		parent := ""
		if thread != nil {
			// Strip the " (star-import-scan)" suffix that getModuleExports
			// appends to thread names so OCI URLs remain parseable during
			// export scanning of modules that use package-local loads.
			parent = strings.TrimSuffix(thread.Name, starImportScanSuffix)
		}

		switch {
		case strings.HasPrefix(parent, "oci://"):
			if !oci.IsPackageLocalTarget(module) {
				return nil, fmt.Errorf(
					"OCI package-local load %q must be a flat sibling (no subdirectories)",
					module,
				)
			}
			pt, err := oci.ParseOCILoadTarget(parent)
			if err != nil {
				return nil, fmt.Errorf("parsing OCI parent %q for package-local load %q: %w", parent, module, err)
			}
			expanded, err := oci.ExpandPackageLocal(module, pt.RefStr)
			if err != nil {
				return nil, err
			}
			module = expanded
		case isFilesystemCaller(parent):
			// Filesystem relative resolution.
			if !isFilesystemRelativeTarget(module) {
				return nil, fmt.Errorf("relative load %q must end with .star", module)
			}
			callerDir := filepath.Dir(parent)
			relPath := module[2:] // strip "./"
			if strings.HasSuffix(relPath, "_test.star") {
				return nil, fmt.Errorf(
					"relative load %q rejected: test files (*_test.star) cannot be loaded as modules",
					module,
				)
			}
			if !filepath.IsLocal(relPath) {
				return nil, fmt.Errorf(
					"relative load %q rejected: path must not escape the caller's directory",
					module,
				)
			}
			resolved := filepath.Join(callerDir, relPath)
			data, err := os.ReadFile(resolved) //nolint:gosec // path validated by filepath.IsLocal
			if err != nil {
				if os.IsNotExist(err) {
					return nil, fmt.Errorf(
						"module %q not found; resolved to %s from caller %s",
						module, resolved, filepath.Base(parent),
					)
				}
				return nil, fmt.Errorf("reading module %q from %s: %w", module, resolved, err)
			}
			// Use resolved absolute path as cache key and module identity.
			if e, ok := m.cache[resolved]; ok {
				if e != nil {
					return e.globals, e.err
				}
				return nil, fmt.Errorf("cycle in load graph: %s", resolved)
			}
			m.cache[resolved] = nil // nil sentinel for cycle detection

			source := string(data)
			source, err = m.ResolveStarImports(source, resolved)
			if err != nil {
				e := &moduleEntry{nil, fmt.Errorf("expanding star imports in module %s: %w", resolved, err)}
				m.cache[resolved] = e
				return nil, e.err
			}
			prog, err := m.rt.getOrCompile(source, m.predeclared, resolved)
			if err != nil {
				e := &moduleEntry{nil, fmt.Errorf("compiling module %s: %w", resolved, err)}
				m.cache[resolved] = e
				return nil, e.err
			}
			modThread := &starlark.Thread{
				Name: resolved, // absolute path enables nested relative loads
				Load: m.load,
				Print: func(_ *starlark.Thread, msg string) {
					m.rt.log.Debug("starlark print", "module", resolved, "msg", msg)
				},
			}
			modThread.SetMaxExecutionSteps(maxSteps)
			globals, execErr := prog.Init(modThread, m.predeclared)
			if execErr != nil {
				var wrappedErr error
				if modThread.ExecutionSteps() >= maxSteps {
					wrappedErr = fmt.Errorf("module %s exceeded execution limit (%d steps): possible infinite loop", resolved, maxSteps)
				} else {
					var evalErr *starlark.EvalError
					if errors.As(execErr, &evalErr) {
						wrappedErr = fmt.Errorf("error in module %s: %s: %w", resolved, evalErr.Backtrace(), execErr)
					} else {
						wrappedErr = fmt.Errorf("error in module %s: %w", resolved, execErr)
					}
				}
				e := &moduleEntry{nil, wrappedErr}
				m.cache[resolved] = e
				return nil, wrappedErr
			}
			globals.Freeze()
			exported := make(starlark.StringDict, len(globals))
			for name, val := range globals {
				if !strings.HasPrefix(name, "_") {
					exported[name] = val
				}
			}
			e := &moduleEntry{exported, nil}
			m.cache[resolved] = e
			return e.globals, nil
		default:
			return nil, fmt.Errorf(
				"relative load %q requires a filesystem-sourced module; "+
					"inline modules have no directory context. "+
					"Use a flat module name or move to a ConfigMap",
				module,
			)
		}
	} else if oci.IsDefaultRegistryTarget(module) {
		// Expand short-form OCI targets before existing routing.
		expanded, err := oci.ExpandDefaultRegistry(module, m.defaultRegistry)
		if err != nil {
			return nil, err
		}
		module = expanded
	}

	// Handle OCI module references: use the full oci:// URL as the key.
	// This avoids collisions when different packages export the same filename.
	if strings.HasPrefix(module, "oci://") {
		if _, ok := m.inlineModules[module]; !ok {
			return nil, fmt.Errorf(
				"OCI module %q not resolved; ensure the OCI reference was resolvable before execution",
				module,
			)
		}
		// Skip validateModuleName — OCI URLs contain "/" and ":" which are
		// already validated by the OCI parser.
	} else if _, ok := m.inlineModules[module]; ok {
		// Path-based inline module key found (e.g., "lib/helpers.star" from CLI bundling).
		// Skip validateModuleName — the key is trusted because it came from the inline
		// module map, which is populated by the runtime's caller (CLI or composition YAML).
	} else if err := validateModuleName(module); err != nil {
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

	// Expand star imports in module source before compilation. This mirrors
	// how fn.go expands star imports in the main script. Cycle detection for
	// the ResolveStarImports → getModuleExports → ResolveStarImports chain
	// is handled by the expanding map inside ResolveStarImports itself.
	source, err = m.ResolveStarImports(source, module)
	if err != nil {
		e = &moduleEntry{nil, fmt.Errorf("expanding star imports in module %s: %w", module, err)}
		m.cache[module] = e
		return nil, e.err
	}

	// Compile via Runtime's bytecode cache (shared across reconciliations).
	prog, err := m.rt.getOrCompile(source, m.predeclared, module)
	if err != nil {
		e = &moduleEntry{nil, fmt.Errorf("compiling module %s: %w", module, err)}
		m.cache[module] = e
		return nil, e.err
	}

	// Create a new thread for this module with its own step counter.
	modThread := &starlark.Thread{
		Name: module,
		Load: m.load, // recursive: modules can load other modules
		Print: func(_ *starlark.Thread, msg string) {
			m.rt.log.Debug("starlark print", "module", module, "msg", msg)
		},
	}
	modThread.SetMaxExecutionSteps(maxSteps)

	// Execute the module.
	globals, err := prog.Init(modThread, m.predeclared)
	if err != nil {
		// Wrap EvalError to include backtrace with module filename,
		// mirroring Runtime.Execute error handling.
		var wrappedErr error
		if modThread.ExecutionSteps() >= maxSteps {
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
// Namespace star imports like load("m.star", ns="*") are rewritten to import
// all exports with prefixed temporary names and then assign a struct:
//
//	load("m.star", _ns_ns__x="x", _ns_ns__y="y")
//	ns = struct(x=_ns_ns__x, y=_ns_ns__y)
//
// This is needed because starlark-go does not natively support "*" in load()
// statements -- it would look for a literal key named "*" in the loaded dict.
func (m *ModuleLoader) ResolveStarImports(source, filename string) (string, error) {
	// Cycle guard: if we're already expanding star imports for this module,
	// return source unchanged. The actual load() cycle detection (nil-sentinel)
	// will produce a proper error at runtime.
	if m.expanding[filename] {
		return source, nil
	}

	// Fast path: if this exact (source, filename) was previously scanned and
	// found to have no star imports, skip the full parse entirely. The parse
	// is otherwise repeated on every reconciliation even though the bytecode
	// is already cached, making it the dominant per-request cost.
	scanKey := starScanKey(source, filename)
	if m.rt.hasNoStarImports(scanKey) {
		return source, nil
	}

	m.expanding[filename] = true
	defer delete(m.expanding, filename)

	opts := fileOptions()
	f, err := opts.Parse(filename, source, 0)
	if err != nil {
		// Parse errors are non-fatal: return source unchanged. The actual
		// compilation step will produce a proper error message.
		return source, nil //nolint:nilerr
	}

	// Collect load statements that contain any star import (plain or namespace).
	type starLoad struct {
		stmt *syntax.LoadStmt
	}
	var starLoads []starLoad

	for _, stmt := range f.Stmts {
		load, ok := stmt.(*syntax.LoadStmt)
		if !ok {
			continue
		}
		hasStar := false
		for i, from := range load.From {
			if from.Name == "*" {
				_ = load.To[i] // ensure index valid
				hasStar = true
				break
			}
		}
		if hasStar {
			starLoads = append(starLoads, starLoad{stmt: load})
		}
	}

	if len(starLoads) == 0 {
		// Memoize: this source has no star imports, so future calls can skip
		// the parse above. Only the negative result is cached -- sources that
		// DO use star imports must be re-expanded against the live module set.
		m.rt.markNoStarImports(scanKey)
		return source, nil
	}

	// For each star import, resolve the module and get its export names.
	// We process replacements from end to start to preserve character offsets.
	lines := strings.Split(source, "\n")
	for i := len(starLoads) - 1; i >= 0; i-- {
		sl := starLoads[i]
		mod := sl.stmt.ModuleName()
		// loadMod is the module name used in the rewritten load() statement.
		// For filesystem-relative modules we keep the original "./..." path
		// so the runtime load() callback resolves it; for OCI we use the
		// expanded URL.
		loadMod := mod

		// Relative ./ handling: dispatch based on caller type.
		if strings.HasPrefix(mod, "./") {
			switch {
			case strings.HasPrefix(filename, "oci://"):
				if !oci.IsPackageLocalTarget(mod) {
					return "", fmt.Errorf("OCI package-local load %q must be a flat sibling", mod)
				}
				pt, err := oci.ParseOCILoadTarget(filename)
				if err != nil {
					return "", fmt.Errorf("parsing OCI parent %q for package-local load %q: %w", filename, mod, err)
				}
				expanded, err := oci.ExpandPackageLocal(mod, pt.RefStr)
				if err != nil {
					return "", fmt.Errorf("resolving star import from %q: %w", mod, err)
				}
				mod = expanded
				loadMod = mod
			case isFilesystemCaller(filename):
				callerDir := filepath.Dir(filename)
				relPath := mod[2:]
				if !filepath.IsLocal(relPath) {
					return "", fmt.Errorf(
						"relative load %q rejected: path must not escape the caller's directory",
						mod,
					)
				}
				mod = filepath.Join(callerDir, relPath)
			default:
				return "", fmt.Errorf(
					"relative load %q requires a filesystem-sourced module; "+
						"inline modules have no directory context",
					mod,
				)
			}
		} else if oci.IsDefaultRegistryTarget(mod) {
			// Expand short-form OCI targets before routing.
			expanded, err := oci.ExpandDefaultRegistry(mod, m.defaultRegistry)
			if err != nil {
				return "", fmt.Errorf("resolving star import from %q: %w", mod, err)
			}
			mod = expanded
			loadMod = mod
		}

		// Use resolved module path as key for getModuleExports.
		resolvedMod := mod

		exports, err := m.getModuleExports(resolvedMod)
		if err != nil {
			return "", fmt.Errorf("resolving star import from %q: %w", mod, err)
		}

		// Classify each From/To pair in this load statement.
		// Collect explicit (non-star) names to exclude from star expansion.
		explicitNames := make(map[string]bool)
		var namespaces []string // namespace alias names (from ns="*" entries)
		hasPlainStar := false

		for j, from := range sl.stmt.From {
			to := sl.stmt.To[j]
			switch {
			case from.Name == "*" && to.Name == "*":
				hasPlainStar = true
			case from.Name == "*":
				namespaces = append(namespaces, to.Name)
			default:
				explicitNames[to.Name] = true
			}
		}

		// Compute star-expanded names (exports minus explicitly imported ones).
		var starNames []string
		for _, name := range exports {
			if !explicitNames[name] {
				starNames = append(starNames, name)
			}
		}

		// Build the replacement load arguments and struct lines.
		var allArgs []string
		var structLines []string

		// Preserve explicitly named imports (using From for aliased names).
		for j, from := range sl.stmt.From {
			to := sl.stmt.To[j]
			if from.Name == "*" {
				continue // skip star entries, handled below
			}
			if from.Name == to.Name {
				allArgs = append(allArgs, fmt.Sprintf("%q", to.Name))
			} else {
				allArgs = append(allArgs, fmt.Sprintf("%s=%q", to.Name, from.Name))
			}
		}

		// Add plain star expanded names (direct imports).
		if hasPlainStar {
			for _, name := range starNames {
				allArgs = append(allArgs, fmt.Sprintf("%q", name))
			}
		}

		// Add namespace star expanded names (prefixed temporary imports + struct).
		for _, ns := range namespaces {
			var structArgs []string
			for _, name := range starNames {
				tmpName := fmt.Sprintf("_ns_%s__%s", ns, name)
				allArgs = append(allArgs, fmt.Sprintf("%s=%q", tmpName, name))
				structArgs = append(structArgs, fmt.Sprintf("%s=%s", name, tmpName))
			}
			structLine := fmt.Sprintf("%s = struct(%s)", ns, strings.Join(structArgs, ", "))
			structLines = append(structLines, structLine)
		}

		// If no load args and no struct lines, remove the load statement entirely.
		if len(allArgs) == 0 && len(structLines) == 0 {
			startLine := int(sl.stmt.Load.Line) - 1
			endLine := int(sl.stmt.Rparen.Line) - 1
			for l := startLine; l <= endLine && l < len(lines); l++ {
				lines[l] = ""
			}
			continue
		}

		// Build the replacement load + optional struct lines.
		var replacement string
		if len(allArgs) > 0 {
			replacement = fmt.Sprintf("load(%q, %s)", loadMod, strings.Join(allArgs, ", "))
		}
		if len(structLines) > 0 {
			if replacement != "" {
				replacement = replacement + "\n" + strings.Join(structLines, "\n")
			} else {
				replacement = strings.Join(structLines, "\n")
			}
		}

		// Replace the original load line(s).
		startLine := int(sl.stmt.Load.Line) - 1
		endLine := int(sl.stmt.Rparen.Line) - 1
		lines[startLine] = replacement
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
// It is read-only: it does NOT set a cache entry in m.cache, so it can safely
// be called during ResolveStarImports even for modules currently being loaded
// (the load() nil-sentinel prevents actual re-execution of those modules).
func (m *ModuleLoader) getModuleExports(module string) ([]string, error) {
	// Resolve module source.
	var src string
	var err error
	if filepath.IsAbs(module) {
		// Filesystem-relative module: read directly from absolute path.
		data, readErr := os.ReadFile(module) //nolint:gosec // path validated by filepath.IsLocal in caller
		if readErr != nil {
			return nil, fmt.Errorf("reading module %q: %w", module, readErr)
		}
		src = string(data)
	} else {
		src, err = m.resolve(module)
		if err != nil {
			return nil, err
		}
	}

	// Expand star imports in module source before compilation so that modules
	// that themselves use load("x.star", "*") are compiled correctly during
	// export scanning.
	src, err = m.ResolveStarImports(src, module)
	if err != nil {
		return nil, fmt.Errorf("expanding star imports in module %s for export scan: %w", module, err)
	}

	// Compile and execute the module to get its exports.
	prog, err := m.rt.getOrCompile(src, m.predeclared, module)
	if err != nil {
		return nil, fmt.Errorf("compiling module %s for star import: %w", module, err)
	}

	thread := &starlark.Thread{
		Name: module + starImportScanSuffix,
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
