# Testing

function-starlark includes a built-in test framework for writing unit tests in
Starlark. Tests use the standard `assert.star` module and follow naming
conventions familiar to Go developers -- `*_test.star` files containing `test_*`
functions. The test runner discovers and executes tests automatically, producing
CI-compatible output with exit code 1 on any failure.

## Quick start

### 1. Write a test file

Create a file ending in `_test.star`. Load `assert.star` and define functions
prefixed with `test_`:

```python
load("assert.star", "assert")

def test_equality():
    assert.eq(1, 1)
    assert.eq("hello", "hello")

def test_inequality():
    assert.ne(1, 2)

def test_true():
    assert.true(True)
    assert.true(1 > 0)

# Non-test function -- ignored by the runner
def helper():
    return 42
```

### 2. Run the tests

Use Go's test command to run all `*_test.star` files in the `testrunner`
directory:

```bash
go test ./testrunner/ -count=1 -v
```

### 3. See the output

The runner prints each file heading, individual test results, and a summary:

```
# pass_test.star
=== RUN   test_equality
--- PASS: test_equality (0.00s)
=== RUN   test_inequality
--- PASS: test_inequality (0.00s)
=== RUN   test_true
--- PASS: test_true (0.00s)
ok	3 tests passed
```

On failure, the summary line changes to `FAIL` and the process exits with code
1 -- making it directly usable in CI pipelines.

## Conventions

**File naming.** Test files must end with `_test.star`. The runner discovers
them recursively via directory traversal. Files that do not match this suffix
are ignored. `_test.star` files cannot be loaded as production modules -- the
loader rejects them at runtime with a clear error message (see the
[module system docs](module-system.md) for details).

**Function naming.** Test functions must start with `test_` and take zero
arguments. Functions that match the `test_` prefix but have parameters are
reported as `SKIP` rather than failed:

```
  SKIP  test_needs_arg (has parameters)
```

Functions without the `test_` prefix are ignored entirely. This allows defining
helpers alongside tests in the same file.

**Execution order.** Tests run in definition order -- sorted by source line
number within each file. Files are sorted alphabetically.

**Non-halting assertions.** All assertions in a test function run to completion.
An assertion failure does not short-circuit the remaining assertions in the same
function, matching Go's `t.Error` semantics. This means a single test function
can report multiple failures.

## Assert API reference

Load the assert module at the top of every test file:

```python
load("assert.star", "assert")
```

| Function | Description |
|----------|-------------|
| `assert.eq(x, y)` | Assert `x` equals `y` (float-aware) |
| `assert.ne(x, y)` | Assert `x` does not equal `y` |
| `assert.true(cond, msg="assertion failed")` | Assert `cond` is truthy |
| `assert.lt(x, y)` | Assert `x` is less than `y` |
| `assert.contains(x, y)` | Assert `y` is in `x` |
| `assert.fails(f, pattern)` | Assert `f()` fails with error matching regex `pattern` |
| `assert.fail(msg)` | Report an error (does NOT halt execution) |

### assert.eq

```python
def test_basic_equality():
    assert.eq(1, 1)
    assert.eq("hello", "hello")
    assert.eq([1, 2, 3], [1, 2, 3])
```

### assert.ne

```python
def test_not_equal():
    assert.ne(1, 2)
    assert.ne("hello", "world")
```

### assert.true

```python
def test_conditions():
    assert.true(True)
    assert.true(1 > 0)
    assert.true(len("hello") == 5, "expected length 5")
```

### assert.lt

```python
def test_ordering():
    assert.lt(1, 2)
    assert.lt("abc", "def")
```

### assert.contains

```python
def test_membership():
    assert.contains([1, 2, 3], 2)
    assert.contains("hello world", "world")
    assert.contains({"a": 1, "b": 2}, "a")
```

### assert.fails

```python
def test_expected_errors():
    assert.fails(lambda: 1 // 0, "division by zero")
    assert.fails(lambda: int("abc"), "invalid literal")
```

### assert.fail

```python
def test_conditional_failure():
    result = compute_something()
    if result < 0:
        assert.fail("result should not be negative")
```

Note that `assert.fail` reports the error but does not halt the test function.
Remaining assertions still execute.

## Available builtins in test files

Test files have access to these predeclared builtins:

| Builtin | Purpose |
|---------|---------|
| `json` | JSON encoding and decoding |
| `crypto` | SHA-256 and SHA-512 hashing |
| `encoding` | Base64 encoding and decoding |
| `dict` | Dictionary utilities (`merge`, `pick`, `omit`) |
| `regex` | Regular expression matching |
| `yaml` | YAML encoding and decoding |
| `schema` | Schema definition |
| `field` | Schema field definition |
| `struct` | Struct construction |
| `mutable_struct` | Mutable struct construction |
| `get` | Deep key access with dot notation |

Crossplane-specific builtins -- `Resource`, `oxr`, `dxr`, `observed`, and
others -- are NOT available in test files because they require a Crossplane
reconciliation context that does not exist during testing.

```python
load("assert.star", "assert")

def test_json():
    assert.eq(json.encode({"a": 1}), '{"a":1}')

def test_yaml():
    result = yaml.encode({"key": "value"})
    assert.contains(result, "key:")

def test_crypto():
    h = crypto.sha256("hello")
    assert.eq(len(h), 64)

def test_encoding():
    encoded = encoding.b64enc("hello")
    assert.eq(encoding.b64dec(encoded), "hello")

def test_regex():
    assert.true(regex.match(r"^hello", "hello world"))

def test_dict_merge():
    result = dict.merge({"a": 1}, {"b": 2})
    assert.eq(result["a"], 1)
    assert.eq(result["b"], 2)

def test_struct():
    s = struct(name = "test", value = 42)
    assert.eq(s.name, "test")
    assert.eq(s.value, 42)

def test_get():
    obj = {"a": {"b": {"c": 1}}}
    assert.eq(get(obj, "a.b.c"), 1)
    assert.eq(get(obj, "a.x", "default"), "default")

def test_schema():
    Point = schema("Point",
        x = field(type = "int"),
        y = field(type = "int"),
    )
    p = Point(x = 1, y = 2)
    assert.eq(p.x, 1)
    assert.eq(p.y, 2)
```

## Relative loads in tests

Test files can load helper modules using relative paths with the `./` prefix.
This is useful for importing the modules under test:

```python
load("assert.star", "assert")
load("./helpers.star", "greet", "add")

def test_greet():
    assert.eq(greet("world"), "hello world")

def test_add():
    assert.eq(add(2, 3), 5)
```

Subdirectory paths are supported: `load("./lib/utils.star", "validate")`.
Path traversal with `../` is rejected. See the
[module system docs](module-system.md) for full relative-load rules.

## Complete end-to-end example

A realistic helper module and its test file, demonstrating how to structure
testable Starlark code.

**helpers.star** -- the module under test:

```python
def resource_label(env, team, name):
    """Build a standard label map for a managed resource."""
    return {
        "app.kubernetes.io/name": name,
        "app.kubernetes.io/managed-by": "crossplane",
        "env": env,
        "team": team,
    }

def merge_tags(base, overrides):
    """Merge two tag dictionaries, with overrides taking precedence."""
    result = dict(base)
    result.update(overrides)
    return result
```

**helpers_test.star** -- the test file:

```python
load("assert.star", "assert")
load("./helpers.star", "resource_label", "merge_tags")

def test_resource_label():
    labels = resource_label("prod", "platform", "my-bucket")
    assert.eq(labels["env"], "prod")
    assert.eq(labels["team"], "platform")
    assert.eq(labels["app.kubernetes.io/name"], "my-bucket")
    assert.eq(labels["app.kubernetes.io/managed-by"], "crossplane")

def test_resource_label_keys():
    labels = resource_label("dev", "infra", "db")
    assert.eq(len(labels), 4)
    assert.contains(labels, "env")
    assert.contains(labels, "team")

def test_merge_tags_basic():
    base = {"env": "prod", "team": "platform"}
    extra = {"version": "v2"}
    result = merge_tags(base, extra)
    assert.eq(result["env"], "prod")
    assert.eq(result["version"], "v2")

def test_merge_tags_override():
    base = {"env": "dev"}
    overrides = {"env": "prod"}
    result = merge_tags(base, overrides)
    assert.eq(result["env"], "prod")
```

## Go test integration

Go contributors can run Starlark tests from Go using the `testrunner` package.
The `testrunner.Run` function discovers and executes all `*_test.star` files in
a directory:

```go
package mypackage_test

import (
	"bytes"
	"testing"

	"github.com/wompipomp/function-starlark/testrunner"
)

func TestCompositionHelpers(t *testing.T) {
	var buf bytes.Buffer
	err := testrunner.Run(t, "path/to/tests", &buf)
	if err != nil {
		t.Fatalf("starlark tests failed:\n%s", buf.String())
	}
}
```

The runner uses a shared `Runtime` for bytecode caching across files while
creating a fresh `ModuleLoader` per file. This means compiled bytecode is
reused for performance but module-level state is isolated between test files.
