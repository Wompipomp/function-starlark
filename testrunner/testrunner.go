package testrunner

import (
	"fmt"
	"io"
	"testing"
)

// Run discovers and executes all *_test.star files under dir.
// It writes formatted test output to w and returns an error if any test fails.
func Run(_ *testing.T, _ string, _ io.Writer) error {
	return fmt.Errorf("not implemented")
}
