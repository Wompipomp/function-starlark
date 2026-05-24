package testrunner

import "fmt"

// testReporter implements starlarktest.Reporter for per-test failure tracking.
// It captures errors without halting execution, allowing all assertions in a
// test function to run (matching Go's t.Error semantics).
type testReporter struct {
	failed bool
	errors []string
}

func (r *testReporter) Error(args ...any) {
	r.failed = true
	r.errors = append(r.errors, fmt.Sprint(args...))
}

func (r *testReporter) reset() {
	r.failed = false
	r.errors = r.errors[:0]
}
