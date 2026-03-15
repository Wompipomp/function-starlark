package runtime

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go.starlark.net/starlark"
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
// no slashes, must end in .star, no OCI references.
func validateModuleName(module string) error {
	if strings.HasPrefix(module, "oci://") {
		return fmt.Errorf("OCI module loading is not yet supported; use local module paths")
	}
	if strings.Contains(module, "/") || strings.Contains(module, "\\") {
		return fmt.Errorf("module name %q must not contain path separators", module)
	}
	if !strings.HasSuffix(module, ".star") {
		return fmt.Errorf("module name %q must end with .star", module)
	}
	return nil
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
