package runtime

import (
	"crypto/sha256"
	"encoding/hex"

	"github.com/crossplane/function-sdk-go/logging"
	"go.starlark.net/starlark"
)

// Runtime compiles and executes Starlark scripts with bytecode caching.
type Runtime struct {
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
func (r *Runtime) Execute(_ string, _ starlark.StringDict) (starlark.StringDict, error) {
	// Stub: not yet implemented
	return nil, nil
}

// CacheLen returns the number of programs in the cache.
func (r *Runtime) CacheLen() int {
	return len(r.cache)
}

// contentHash returns the SHA-256 hex digest of the source.
func contentHash(source string) string {
	h := sha256.Sum256([]byte(source))
	return hex.EncodeToString(h[:])
}
