package runtime

import (
	"encoding/hex"
	"errors"
	"fmt"
	"sync"

	"github.com/crossplane/function-sdk-go/logging"
	"github.com/zeebo/blake3"
	"go.starlark.net/starlark"
	"go.starlark.net/syntax"

	"github.com/wompipomp/function-starlark/metrics"
)

const maxSteps = 1_000_000

// Runtime compiles and executes Starlark scripts with bytecode caching.
type Runtime struct {
	mu    sync.RWMutex
	cache map[string]*starlark.Program
	log   logging.Logger

	// noStarImports memoizes the result of star-import scanning. A present
	// key (content hash of source+filename) means the source was scanned and
	// found to contain NO star imports, so ResolveStarImports can return it
	// unchanged without re-parsing -- the dominant per-request cost on the
	// cached execution path. Sources that DO contain star imports are
	// intentionally not cached here: their expansion depends on the live
	// module set, so they must be re-expanded on each call.
	starMu        sync.RWMutex
	noStarImports map[string]struct{}
}

// NewRuntime creates a Runtime with an empty program cache.
func NewRuntime(log logging.Logger) *Runtime {
	return &Runtime{
		cache:         make(map[string]*starlark.Program),
		noStarImports: make(map[string]struct{}),
		log:           log,
	}
}

// hasNoStarImports reports whether (source, filename) was previously scanned
// and found to contain no star imports.
func (r *Runtime) hasNoStarImports(key string) bool {
	r.starMu.RLock()
	_, ok := r.noStarImports[key]
	r.starMu.RUnlock()
	return ok
}

// markNoStarImports records that (source, filename) contains no star imports.
func (r *Runtime) markNoStarImports(key string) {
	r.starMu.Lock()
	r.noStarImports[key] = struct{}{}
	r.starMu.Unlock()
}

// starScanKey derives the memoization key for star-import scanning.
func starScanKey(source, filename string) string {
	return contentHash(source + "\x00" + filename)
}

// Execute compiles (or retrieves from cache) and runs the Starlark source.
// predeclared defines the built-in globals available to the script.
// filename is used in error messages and stack traces (e.g., "my-script.star").
// loadFn, if non-nil, is used as the Thread.Load callback for load() statements.
// A nil loadFn falls back to an error stub that rejects all load() calls.
// Returns post-execution globals.
func (r *Runtime) Execute(source string, predeclared starlark.StringDict, filename string, loadFn LoadFunc) (starlark.StringDict, error) {
	prog, err := r.getOrCompile(source, predeclared, filename)
	if err != nil {
		return nil, fmt.Errorf("starlark compilation error: %w", err)
	}

	thread := &starlark.Thread{
		Name: filename,
		Print: func(_ *starlark.Thread, msg string) {
			r.log.Debug("starlark print", "msg", msg)
		},
	}

	if loadFn != nil {
		thread.Load = loadFn
	} else {
		thread.Load = func(_ *starlark.Thread, _ string) (starlark.StringDict, error) {
			return nil, fmt.Errorf("load() is not supported -- define helpers with def in the same script")
		}
	}
	thread.SetMaxExecutionSteps(maxSteps)

	globals, err := prog.Init(thread, predeclared)
	if err != nil {
		// Check step limit first -- must happen before EvalError check
		// because step limit cancellation also produces an EvalError.
		if thread.ExecutionSteps() >= maxSteps {
			return nil, fmt.Errorf(
				"starlark script exceeded execution limit (%d steps): possible infinite loop",
				maxSteps,
			)
		}

		var evalErr *starlark.EvalError
		if errors.As(err, &evalErr) {
			// Wrap with %w to preserve error chain so callers can use errors.As
			// (e.g., to detect FatalError from the fatal() builtin).
			return nil, fmt.Errorf("starlark execution error: %s: %w", evalErr.Backtrace(), err)
		}

		return nil, fmt.Errorf("starlark execution error: %w", err)
	}

	return globals, nil
}

// getOrCompile returns a cached *Program or compiles the source and caches it.
// The cache key includes the filename to avoid serving programs compiled with
// a different filename (which would produce wrong position info in errors).
func (r *Runtime) getOrCompile(source string, predeclared starlark.StringDict, filename string) (*starlark.Program, error) {
	key := contentHash(source + "\x00" + filename)

	// Fast path: read lock.
	r.mu.RLock()
	prog, ok := r.cache[key]
	r.mu.RUnlock()
	if ok {
		r.log.Debug("cache hit", "hash", key[:12])
		metrics.CacheHitsTotal.WithLabelValues(filename).Inc()
		return prog, nil
	}

	// Slow path: compile and write lock.
	opts := fileOptions()
	isPredeclared := func(name string) bool {
		_, exists := predeclared[name]
		return exists
	}

	_, prog, err := starlark.SourceProgramOptions(opts, filename, source, isPredeclared)
	if err != nil {
		return nil, err
	}

	r.mu.Lock()
	r.cache[key] = prog
	r.mu.Unlock()
	r.log.Debug("cache miss -- compiled", "hash", key[:12])
	metrics.CacheMissesTotal.WithLabelValues(filename).Inc()

	return prog, nil
}

// CacheLen returns the number of programs in the cache.
func (r *Runtime) CacheLen() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.cache)
}

// fileOptions returns the standard FileOptions used for both the main script
// and loaded modules. Extracted to ensure consistent syntax options.
func fileOptions() *syntax.FileOptions {
	return &syntax.FileOptions{
		TopLevelControl: true,
		Set:             true,
		While:           true,
		GlobalReassign:  true,
	}
}

// contentHash returns the BLAKE3-256 hex digest of the source. BLAKE3 is used
// over SHA-256 because this digest is computed on every request to key the
// in-memory bytecode and star-scan caches; BLAKE3 is several times faster while
// remaining cryptographically collision-resistant. The digest never leaves the
// process (it is only ever a map key and a debug-log prefix), so the algorithm
// is a free internal implementation detail.
func contentHash(source string) string {
	h := blake3.Sum256([]byte(source))
	return hex.EncodeToString(h[:])
}
