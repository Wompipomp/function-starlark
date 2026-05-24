package testrunner

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	crlogging "github.com/crossplane/crossplane-runtime/v2/pkg/logging"
	"github.com/mattn/go-isatty"
	starlarkjson "go.starlark.net/lib/json"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
	"go.starlark.net/starlarktest"
	"go.starlark.net/syntax"

	"github.com/wompipomp/function-starlark/builtins"
	"github.com/wompipomp/function-starlark/runtime"
	"github.com/wompipomp/function-starlark/schema"
)

// discardLogger implements crossplane-runtime logging.Logger by discarding all output.
type discardLogger struct{}

func (discardLogger) Info(_ string, _ ...any)                {}
func (discardLogger) Debug(_ string, _ ...any)               {}
func (d discardLogger) WithValues(_ ...any) crlogging.Logger { return d }

// Run discovers and executes all *_test.star files under dir.
// It writes formatted test output to w and returns an error if any test fails.
func Run(_ *testing.T, dir string, w io.Writer) error {
	files, err := discoverTestFiles(dir)
	if err != nil {
		return fmt.Errorf("discovering test files: %w", err)
	}
	sort.Strings(files)
	if len(files) == 0 {
		return nil
	}

	// Shared Runtime for bytecode caching across all files (TEST-09).
	rt := runtime.NewRuntime(discardLogger{})

	predeclared := purePredeclared()
	color := isTTY(w)
	reporter := &testReporter{}

	var totalPass, totalFail int

	for _, testFile := range files {
		absPath, err := filepath.Abs(testFile)
		if err != nil {
			return fmt.Errorf("resolving absolute path for %s: %w", testFile, err)
		}

		fmt.Fprintf(w, "# %s\n", filepath.Base(testFile))

		// Fresh ModuleLoader per file (TEST-09).
		loader := runtime.NewModuleLoader(nil, []string{filepath.Dir(absPath)}, predeclared, rt, "")

		// Build load function that intercepts "assert.star" with starlarktest.
		interceptLoad := func(thread *starlark.Thread, module string) (starlark.StringDict, error) {
			if module == "assert.star" {
				return starlarktest.LoadAssertModule()
			}
			return loader.LoadFunc()(thread, module)
		}

		thread := &starlark.Thread{
			Name: absPath,
			Load: interceptLoad,
		}

		// SetReporter BEFORE execution (Pitfall 2: top-level code may use asserts).
		starlarktest.SetReporter(thread, reporter)

		globals, execErr := starlark.ExecFileOptions(fileOptions(), thread, absPath, nil, predeclared)
		if execErr != nil {
			fmt.Fprintf(w, "FAIL  %s\n    %s\n", filepath.Base(testFile), execErr)
			totalFail++
			continue
		}

		// Collect test_* zero-arg functions, sorted by definition order.
		type testFunc struct {
			name string
			fn   *starlark.Function
			line int32
		}
		var tests []testFunc
		for name, val := range globals {
			fn, ok := val.(*starlark.Function)
			if !ok || !strings.HasPrefix(name, "test_") {
				continue
			}
			if fn.NumParams() > 0 {
				fmt.Fprintf(w, "  SKIP  %s (has parameters)\n", name)
				continue
			}
			tests = append(tests, testFunc{name: name, fn: fn, line: fn.Position().Line})
		}
		sort.Slice(tests, func(i, j int) bool {
			return tests[i].line < tests[j].line
		})

		for _, tf := range tests {
			reporter.reset()
			start := time.Now()
			fmt.Fprintf(w, "=== RUN   %s\n", tf.name)

			_, callErr := starlark.Call(thread, tf.fn, nil, nil)
			elapsed := time.Since(start)

			if callErr != nil {
				// Runtime error (not assert failure).
				var evalErr *starlark.EvalError
				if errors.As(callErr, &evalErr) {
					fmt.Fprintf(w, "    %s\n", evalErr.Backtrace())
				} else {
					fmt.Fprintf(w, "    %s\n", callErr)
				}
				fmt.Fprintf(w, "%s\n", red(fmt.Sprintf("--- FAIL: %s (%.2fs)", tf.name, elapsed.Seconds()), color))
				totalFail++
			} else if reporter.failed {
				for _, e := range reporter.errors {
					fmt.Fprintf(w, "    %s\n", e)
				}
				fmt.Fprintf(w, "%s\n", red(fmt.Sprintf("--- FAIL: %s (%.2fs)", tf.name, elapsed.Seconds()), color))
				totalFail++
			} else {
				fmt.Fprintf(w, "%s\n", green(fmt.Sprintf("--- PASS: %s (%.2fs)", tf.name, elapsed.Seconds()), color))
				totalPass++
			}
		}
	}

	// Summary.
	total := totalPass + totalFail
	if totalFail > 0 {
		fmt.Fprintf(w, "%s\t%d tests: %d passed, %d failed\n", red("FAIL", color), total, totalPass, totalFail)
		return fmt.Errorf("%d of %d tests failed", totalFail, total)
	}
	fmt.Fprintf(w, "%s\t%d tests passed\n", green("ok", color), total)
	return nil
}

// discoverTestFiles walks dir recursively and returns all *_test.star files.
func discoverTestFiles(dir string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(d.Name(), "_test.star") {
			files = append(files, path)
		}
		return nil
	})
	return files, err
}

// purePredeclared returns the predeclared globals available in test files.
// These are all pure builtins (no Crossplane request context required).
func purePredeclared() starlark.StringDict {
	return starlark.StringDict{
		"json":     starlarkjson.Module,
		"crypto":   builtins.CryptoModule,
		"encoding": builtins.EncodingModule,
		"dict":     builtins.DictModule,
		"regex":    builtins.RegexModule,
		"yaml":     builtins.YAMLModule,
		"schema":   schema.SchemaBuiltin(),
		"field":    schema.FieldBuiltin(),
		"struct":   starlark.NewBuiltin("struct", starlarkstruct.Make),
		"get":      builtins.GetBuiltin(),
	}
}

// fileOptions returns the standard FileOptions matching the project's
// runtime.fileOptions() for consistent syntax support.
func fileOptions() *syntax.FileOptions {
	return &syntax.FileOptions{
		TopLevelControl: true,
		Set:             true,
		While:           true,
	}
}

// isTTY reports whether w is connected to a terminal.
func isTTY(w io.Writer) bool {
	if f, ok := w.(*os.File); ok {
		return isatty.IsTerminal(f.Fd()) || isatty.IsCygwinTerminal(f.Fd())
	}
	return false
}

// green wraps s in ANSI green escape codes when color is true.
func green(s string, color bool) string {
	if color {
		return "\033[32m" + s + "\033[0m"
	}
	return s
}

// red wraps s in ANSI red escape codes when color is true.
func red(s string, color bool) string {
	if color {
		return "\033[31m" + s + "\033[0m"
	}
	return s
}
