package testrunner_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/wompipomp/function-starlark/testrunner"
)

func TestPassingTests(t *testing.T) {
	var buf bytes.Buffer
	err := testrunner.Run(t, "testdata", &buf)
	if err != nil {
		t.Fatalf("Run() returned error for passing tests: %v", err)
	}
	output := buf.String()

	// Verify discovery found the file
	if !strings.Contains(output, "pass_test.star") {
		t.Errorf("output missing file header, got:\n%s", output)
	}

	// Verify Go test output format
	for _, want := range []string{
		"=== RUN   test_equality",
		"--- PASS: test_equality",
		"=== RUN   test_inequality",
		"--- PASS: test_inequality",
		"=== RUN   test_true",
		"--- PASS: test_true",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("output missing %q, got:\n%s", want, output)
		}
	}

	// Verify non-test function is ignored
	if strings.Contains(output, "helper") {
		t.Errorf("output should not mention non-test function 'helper', got:\n%s", output)
	}

	// Verify summary
	if !strings.Contains(output, "ok") {
		t.Errorf("output missing 'ok' summary, got:\n%s", output)
	}
}
