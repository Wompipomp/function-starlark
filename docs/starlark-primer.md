# Starlark primer

Starlark is a Python-like configuration language designed for deterministic,
hermetic execution. If you know Python, you already know 90% of Starlark. This
guide covers the 10% that differs.

## What is Starlark?

Starlark was created by Google for the [Bazel](https://bazel.build/) build
system. It uses Python syntax but restricts features that could cause
non-determinism or unbounded computation. function-starlark uses the Go
implementation ([google/starlark-go](https://github.com/google/starlark-go)).

Key design goals:
- **Deterministic** -- the same input always produces the same output.
- **Hermetic** -- no file I/O, no network access, no system calls.
- **Finite** -- no unbounded loops, no recursion.

These constraints make Starlark ideal for configuration languages where you
want safety guarantees without sacrificing readability.

## Key differences from Python

### Missing constructs

**No try/except** -- all errors are fatal. There is no way to catch exceptions.

```python
# Python
try:
    value = data["key"]
except KeyError:
    value = "default"

# Starlark -- use get() for safe access
value = get(oxr, "spec.key", "default")
```

**No while loops** -- only `for` loops with finite iterables.

```python
# Python
while not ready:
    check()

# Starlark -- use for with range()
for i in range(100):
    if is_ready(i):
        break
```

**No recursion** -- functions cannot call themselves.

```python
# Python
def flatten(lst):
    return [x for sub in lst for x in (flatten(sub) if isinstance(sub, list) else [sub])]

# Starlark -- use iterative approaches instead
```

**No classes** -- use dicts and functions. Starlark is not object-oriented.

```python
# Python
class Bucket:
    def __init__(self, name, region):
        self.name = name
        self.region = region

# Starlark -- use dicts
bucket = {"name": "my-bucket", "region": "us-east-1"}
```

**No import** -- use `load()` instead.

```python
# Python
from helpers import my_function

# Starlark
load("helpers.star", "my_function")

# Starlark -- namespace import (all exports in a struct)
load("helpers.star", h="*")
h.my_function()
```

Namespace alias imports wrap all exports in a struct, useful when multiple
modules export the same names.

**No with statement** -- no context managers.

**No generators/yield** -- use list comprehensions.

```python
# Python
def evens(n):
    for i in range(n):
        if i % 2 == 0:
            yield i

# Starlark
evens = [i for i in range(n) if i % 2 == 0]
```

**No \*\*kwargs spread** -- no `{**a, **b}` dict merging.

```python
# Python
merged = {**defaults, **overrides}

# Starlark -- merge manually
merged = {}
for k, v in defaults.items():
    merged[k] = v
for k, v in overrides.items():
    merged[k] = v
```

**No dict.update()** -- merge dicts manually with a loop (see above).

### Behavioral differences

**Global variables are immutable after top-level assignment.** You cannot
reassign a global variable inside a function.

```python
count = 0

def increment():
    count = count + 1  # ERROR: local variable referenced before assignment

# Instead, use a mutable container:
state = {"count": 0}

def increment():
    state["count"] = state["count"] + 1
```

**No is operator** -- use `==` for comparison.

```python
# Python
if x is None:

# Starlark
if x == None:
```

**No chained comparisons** -- `1 < x < 5` is invalid.

```python
# Python
if 1 < x < 5:

# Starlark
if 1 < x and x < 5:
```

**Booleans are not integers** -- `True + 1` is an error. You cannot use
booleans in arithmetic.

**Deterministic dict iteration** -- insertion order is guaranteed (unlike
Python < 3.7). Dicts always iterate in the order keys were inserted.

**No mutation during iteration** -- you cannot modify a dict or list while
iterating over it. Copy first.

```python
# ERROR: cannot modify dict during iteration
for k, v in d.items():
    if v == "remove":
        d.pop(k)

# Correct: copy the items first
to_remove = [k for k, v in d.items() if v == "remove"]
for k in to_remove:
    d.pop(k)
```

### String formatting

Starlark supports **only** the `%` operator for string formatting:

```python
# Works
name = "hello %s" % user
msg = "%s has %d items" % (user, count)

# Does NOT work -- f-strings are invalid
name = f"hello {user}"

# Does NOT work -- .format() does not exist
name = "hello {}".format(user)
```

## Available types

Starlark supports these built-in types:

| Type | Example | Notes |
|------|---------|-------|
| bool | `True`, `False` | Not integers -- cannot use in arithmetic |
| int | `42`, `0xFF` | Arbitrary precision |
| float | `3.14`, `1e10` | IEEE 754 double |
| string | `"hello"`, `'world'` | Immutable, `%` formatting only |
| list | `[1, 2, 3]` | Mutable, ordered |
| tuple | `(1, 2, 3)` | Immutable, ordered |
| dict | `{"a": 1}` | Mutable, insertion-ordered |
| set | `set([1, 2, 3])` | Mutable, no literal syntax |
| None | `None` | Singleton null value |
| function | `def f(): pass` | First-class, no recursion |

## Available Starlark builtins

These are standard Starlark builtins (from the language specification), not
function-starlark-specific:

| Function | Description |
|----------|-------------|
| `len(x)` | Length of a string, list, tuple, dict, or set |
| `range(n)` / `range(start, stop, step)` | Integer sequence |
| `str(x)` | Convert to string |
| `int(x)` | Convert to integer |
| `float(x)` | Convert to float |
| `bool(x)` | Convert to boolean |
| `list(x)` | Convert iterable to list |
| `tuple(x)` | Convert iterable to tuple |
| `dict(pairs)` | Create dict from key-value pairs |
| `type(x)` | Return type name as string |
| `hash(x)` | Hash a string |
| `sorted(x)` | Return sorted list |
| `reversed(x)` | Return reversed iterator |
| `enumerate(x)` | Yield (index, value) pairs |
| `zip(a, b)` | Pair elements from iterables |
| `any(x)` | True if any element is truthy |
| `all(x)` | True if all elements are truthy |
| `min(a, b, ...)` | Minimum value |
| `max(a, b, ...)` | Maximum value |
| `hasattr(x, name)` | Check if attribute exists |
| `getattr(x, name)` | Get attribute value |
| `dir(x)` | List attribute names |
| `repr(x)` | String representation |
| `print(...)` | Print to function logs (stderr), not to resource output |
| `fail(msg)` | Halt with error (standard Starlark -- prefer `fatal()` in function-starlark) |

## function-starlark builtins

On top of standard Starlark, function-starlark adds 22 predeclared names:
6 globals (`oxr`, `dxr`, `observed`, `context`, `environment`,
`extra_resources`) and 16 functions (`Resource`, `skip_resource`, `get`,
`get_label`, `get_annotation`, `set_condition`, `emit_event`, `fatal`,
`set_connection_details`, `set_xr_status`, `get_observed`,
`require_extra_resource`, `require_extra_resources`, `schema`, `field`,
`struct`).

See the [builtins reference](builtins-reference.md) for complete signatures,
parameter types, defaults, and examples.

## Common patterns

### Safe nested access

```python
region = get(oxr, "spec.region", "us-east-1")
name = get(oxr, "metadata.name", "unknown")
```

### Conditional resources

```python
env = get(oxr, "spec.environment", "dev")
if env == "prod":
    Resource("monitoring", {
        "apiVersion": "monitoring.example.io/v1",
        "kind": "Dashboard",
        "spec": {"enabled": True},
    })
```

### Loop-based resource creation

```python
count = get(oxr, "spec.replicas", 3)
for i in range(count):
    Resource("worker-%d" % i, {
        "apiVersion": "apps.example.io/v1",
        "kind": "Worker",
        "metadata": {"name": "worker-%d" % i},
        "spec": {"index": i},
    })
```

### Dict merging

```python
base = {"tier": "standard", "env": "dev"}
override = {"env": "prod", "team": "platform"}

merged = {}
for k, v in base.items():
    merged[k] = v
for k, v in override.items():
    merged[k] = v
# merged: {"tier": "standard", "env": "prod", "team": "platform"}
```

### String building

```python
name = "%s-%s-%d" % (prefix, env, index)
endpoint = "https://%s.%s.svc.cluster.local" % (service, namespace)
```

## Gotchas

The top 5 mistakes Python developers make when writing Starlark:

### 1. Trying to use f-strings

```python
# WRONG -- f-strings do not exist in Starlark
name = f"bucket-{region}"

# CORRECT
name = "bucket-%s" % region
```

### 2. Trying to catch exceptions

```python
# WRONG -- try/except does not exist
try:
    value = data["missing"]
except KeyError:
    value = "default"

# CORRECT -- check conditions or use get()
value = get(data, "missing", "default")
```

### 3. Mutating during iteration

```python
# WRONG -- cannot modify list during iteration
for item in items:
    if item == "remove":
        items.remove(item)

# CORRECT -- build a new list
items = [item for item in items if item != "remove"]
```

### 4. Using booleans as integers

```python
# WRONG -- True is not 1 in Starlark
total = count + True

# CORRECT
total = count + (1 if flag else 0)
```

### 5. Reassigning globals inside functions

```python
# WRONG -- cannot reassign globals
counter = 0
def bump():
    counter = counter + 1  # ERROR

# CORRECT -- use a mutable container
state = {"counter": 0}
def bump():
    state["counter"] = state["counter"] + 1
```

## Further reading

- [Builtins reference](builtins-reference.md) -- Complete API reference for all
  function-starlark globals and functions
- [Starlark language specification](https://github.com/bazelbuild/starlark/blob/master/spec.md) --
  The official Starlark language spec
