package testrunner_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wompipomp/function-starlark/testrunner"
)

func copyFixture(t *testing.T, dst, name string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dst, filepath.Base(name)), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestPassingTests(t *testing.T) {
	dir := t.TempDir()
	copyFixture(t, dir, "pass_test.star")

	var buf bytes.Buffer
	err := testrunner.Run(t, dir, &buf)
	if err != nil {
		t.Fatalf("Run() returned error for passing tests: %v", err)
	}
	output := buf.String()

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

	if strings.Contains(output, "helper") {
		t.Errorf("output should not mention non-test function 'helper', got:\n%s", output)
	}

	if !strings.Contains(output, "ok") {
		t.Errorf("output missing 'ok' summary, got:\n%s", output)
	}
}

func TestCanaryAssertFails(t *testing.T) {
	dir := t.TempDir()
	copyFixture(t, dir, "fail_test.star")

	var buf bytes.Buffer
	err := testrunner.Run(t, dir, &buf)
	if err == nil {
		t.Fatal("Run() should return error when assert.eq(1, 2) fails")
	}
	output := buf.String()

	if !strings.Contains(output, "--- FAIL: test_canary_fail") {
		t.Errorf("output missing FAIL for test_canary_fail, got:\n%s", output)
	}
	if !strings.Contains(output, "--- PASS: test_pass_before_fail") {
		t.Errorf("passing test should still pass, got:\n%s", output)
	}
	if !strings.Contains(output, "FAIL") {
		t.Errorf("summary should contain FAIL, got:\n%s", output)
	}
}

func TestDiscoveryRecursive(t *testing.T) {
	dir := t.TempDir()
	copyFixture(t, dir, "pass_test.star")
	subdir := filepath.Join(dir, "subdir")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join("testdata", "subdir", "nested_test.star"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subdir, "nested_test.star"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := testrunner.Run(t, dir, &buf); err != nil {
		t.Fatalf("Run() returned error: %v", err)
	}
	output := buf.String()

	if !strings.Contains(output, "pass_test.star") {
		t.Errorf("should discover root-level test file, got:\n%s", output)
	}
	if !strings.Contains(output, "nested_test.star") {
		t.Errorf("should discover subdirectory test file, got:\n%s", output)
	}
}

func TestOutputFormat(t *testing.T) {
	dir := t.TempDir()
	copyFixture(t, dir, "pass_test.star")

	var buf bytes.Buffer
	testrunner.Run(t, dir, &buf)
	output := buf.String()

	if !strings.Contains(output, "# pass_test.star") {
		t.Errorf("missing file header, got:\n%s", output)
	}
	if !strings.Contains(output, "=== RUN   ") {
		t.Errorf("missing RUN line, got:\n%s", output)
	}
	if !strings.Contains(output, "--- PASS: ") {
		t.Errorf("missing PASS line, got:\n%s", output)
	}
	if !strings.Contains(output, "tests passed") {
		t.Errorf("missing summary, got:\n%s", output)
	}
}

func TestExitCodeOnFailure(t *testing.T) {
	dir := t.TempDir()
	copyFixture(t, dir, "fail_test.star")

	var buf bytes.Buffer
	if err := testrunner.Run(t, dir, &buf); err == nil {
		t.Fatal("Run() should return error when tests fail")
	}
}

func TestPureBuiltins(t *testing.T) {
	dir := t.TempDir()
	copyFixture(t, dir, "builtins_test.star")

	var buf bytes.Buffer
	if err := testrunner.Run(t, dir, &buf); err != nil {
		t.Fatalf("Run() returned error for builtins tests: %v\n%s", err, buf.String())
	}
}

func TestParameterizedSkip(t *testing.T) {
	dir := t.TempDir()
	copyFixture(t, dir, "parameterized_test.star")

	var buf bytes.Buffer
	if err := testrunner.Run(t, dir, &buf); err != nil {
		t.Fatalf("Run() returned error: %v\n%s", err, buf.String())
	}
	output := buf.String()

	if !strings.Contains(output, "SKIP") || !strings.Contains(output, "test_with_param") {
		t.Errorf("should skip parameterized test with SKIP marker, got:\n%s", output)
	}
	if !strings.Contains(output, "has parameters") {
		t.Errorf("skip message should explain reason, got:\n%s", output)
	}
	if !strings.Contains(output, "--- PASS: test_normal") {
		t.Errorf("test_normal should still pass, got:\n%s", output)
	}
}

func TestLoadError(t *testing.T) {
	dir := t.TempDir()
	copyFixture(t, dir, "load_error_test.star")
	copyFixture(t, dir, "pass_test.star")

	var buf bytes.Buffer
	err := testrunner.Run(t, dir, &buf)
	if err == nil {
		t.Fatal("Run() should return error for load errors")
	}
	output := buf.String()

	if !strings.Contains(output, "load_error_test.star") {
		t.Errorf("should report load error file, got:\n%s", output)
	}
	if !strings.Contains(output, "pass_test.star") {
		t.Errorf("should continue to other files after load error, got:\n%s", output)
	}
}

func TestRelativeLoad(t *testing.T) {
	dir := t.TempDir()
	copyFixture(t, dir, "relative_load_test.star")
	copyFixture(t, dir, "helpers.star")

	var buf bytes.Buffer
	if err := testrunner.Run(t, dir, &buf); err != nil {
		t.Fatalf("Run() returned error for relative load test: %v\n%s", err, buf.String())
	}
	output := buf.String()

	if !strings.Contains(output, "--- PASS: test_greet") {
		t.Errorf("test_greet should pass, got:\n%s", output)
	}
	if !strings.Contains(output, "--- PASS: test_add") {
		t.Errorf("test_add should pass, got:\n%s", output)
	}
}

func TestNoTestFiles(t *testing.T) {
	dir := t.TempDir()

	var buf bytes.Buffer
	if err := testrunner.Run(t, dir, &buf); err != nil {
		t.Fatalf("Run() should not error on empty dir: %v", err)
	}
	if buf.Len() > 0 {
		t.Errorf("should produce no output for empty dir, got:\n%s", buf.String())
	}
}
