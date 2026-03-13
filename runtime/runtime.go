package runtime

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"

	"github.com/crossplane/function-sdk-go/logging"
	"go.starlark.net/starlark"
	"go.starlark.net/syntax"
)

const maxSteps = 1_000_000

// Runtime compiles and executes Starlark scripts with bytecode caching.
type Runtime struct {
	mu    sync.RWMutex
	cache map[string]*starlark.Program
	log   logging.Logger
}

// NewRuntime creates a Runtime with an empty program cache.
func NewRuntime(log logging.Logger) *Runtime {
	return &Runtime{
		cache: make(map[string]*starlark.Program),
		log:   log,
	}
}

// Execute compiles (or retrieves from cache) and runs the Starlark source.
// predeclared defines the built-in globals available to the script.
// Returns post-execution globals.
func (r *Runtime) Execute(source string, predeclared starlark.StringDict) (starlark.StringDict, error) {
	prog, err := r.getOrCompile(source, predeclared)
	if err != nil {
		return nil, fmt.Errorf("Starlark compilation error: %w", err)
	}

	thread := &starlark.Thread{
		Name: "composition.star",
		Print: func(_ *starlark.Thread, msg string) {
			r.log.Debug("starlark print", "msg", msg)
		},
		Load: func(_ *starlark.Thread, _ string) (starlark.StringDict, error) {
			return nil, fmt.Errorf("load() is not supported -- define helpers with def in the same script")
		},
	}
	thread.SetMaxExecutionSteps(maxSteps)

	globals, err := prog.Init(thread, predeclared)
	if err != nil {
		// Check step limit first -- must happen before EvalError check
		// because step limit cancellation also produces an EvalError.
		if thread.ExecutionSteps() >= maxSteps {
			return nil, fmt.Errorf(
				"Starlark script exceeded execution limit (%d steps): possible infinite loop",
				maxSteps,
			)
		}

		var evalErr *starlark.EvalError
		if errors.As(err, &evalErr) {
			return nil, fmt.Errorf("Starlark execution error: %s", evalErr.Backtrace())
		}

		return nil, fmt.Errorf("Starlark execution error: %w", err)
	}

	return globals, nil
}

// getOrCompile returns a cached *Program or compiles the source and caches it.
func (r *Runtime) getOrCompile(source string, predeclared starlark.StringDict) (*starlark.Program, error) {
	key := contentHash(source)

	// Fast path: read lock.
	r.mu.RLock()
	prog, ok := r.cache[key]
	r.mu.RUnlock()
	if ok {
		r.log.Debug("cache hit", "hash", key[:12])
		return prog, nil
	}

	// Slow path: compile and write lock.
	opts := &syntax.FileOptions{
		TopLevelControl: true,
		Set:             true,
		While:           true,
	}
	isPredeclared := func(name string) bool {
		_, exists := predeclared[name]
		return exists
	}

	_, prog, err := starlark.SourceProgramOptions(opts, "composition.star", source, isPredeclared)
	if err != nil {
		return nil, err
	}

	r.mu.Lock()
	r.cache[key] = prog
	r.mu.Unlock()
	r.log.Debug("cache miss -- compiled", "hash", key[:12])

	return prog, nil
}

// CacheLen returns the number of programs in the cache.
func (r *Runtime) CacheLen() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.cache)
}

// contentHash returns the SHA-256 hex digest of the source.
func contentHash(source string) string {
	h := sha256.Sum256([]byte(source))
	return hex.EncodeToString(h[:])
}
