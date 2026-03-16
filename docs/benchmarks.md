# Performance benchmarks

Comparative benchmarks of function-starlark against function-go-templating,
function-pythonic, and function-kcl. All benchmarks call each function's gRPC
`RunFunction` endpoint over Docker with equivalent workloads producing the
same number of Kubernetes resources.

## Results (Apple M4 Pro)

### 10 resources (gRPC over Docker, cached/warm)

| Function | Version | Latency | B/op | vs Starlark |
|---|---|---|---|---|
| **function-starlark** | v1.3 | **724 us** | 44 KB | 1.0x |
| function-go-templating | v0.11.4 | 999 us | 45 KB | 1.4x slower |
| function-pythonic | v0.5.0 | 1,725 us | 47 KB | 2.4x slower |
| function-kcl | v0.12.0 | 3,505 us | 48 KB | 4.8x slower |

### 50 resources (gRPC over Docker, cached/warm)

| Function | Version | Latency | B/op | vs Starlark |
|---|---|---|---|---|
| **function-starlark** | v1.3 | **1,256 us** | 204 KB | 1.0x |
| function-go-templating | v0.11.4 | 2,751 us | 219 KB | 2.2x slower |
| function-pythonic | v0.5.0 | 6,344 us | 206 KB | 5.1x slower |
| function-kcl | v0.12.0 | 9,318 us | 234 KB | 7.4x slower |

### In-process execution (no gRPC/Docker overhead)

| Benchmark | Latency | Allocs |
|---|---|---|
| RunFunction (10 resources, cached) | 126 us | 1,925 |
| CachedExecution (10 dicts, runtime only) | ~50 us | ~800 |

The ~600 us gap between in-process (126 us) and gRPC-over-Docker (724 us) is
gRPC + protobuf serialization + Docker network overhead -- constant regardless
of workload size.

### Container image size

| Function | Image Size |
|---|---|
| **function-starlark** | **82 MB** |
| function-go-templating | 82 MB |
| function-pythonic | 340 MB |
| function-kcl | 588 MB |

### Idle memory

| Function | Idle RSS |
|---|---|
| **function-starlark** | **7.4 MiB** |
| function-go-templating | 7.1 MiB |
| function-pythonic | ~15 MiB |
| function-kcl | 12.7 MiB |

## Why is Starlark fastest?

Each function receives and returns the same protobuf `RunFunctionRequest` /
`RunFunctionResponse`. The difference is what happens in between:

```
starlark:       bytecode VM  -->  protobuf structs (direct)
go-templating:  template     -->  YAML text  -->  YAML parse  -->  protobuf
pythonic:       CPython VM   -->  Python objects  -->  protobuf
kcl:            KCL compile  -->  KCL VM  -->  KRM YAML  -->  parse  -->  protobuf
```

- **function-starlark** compiles Starlark to bytecode once, caches the
  `*Program`, and on subsequent calls runs the bytecode directly. `Resource()`
  writes to protobuf structs in-memory with no serialization roundtrip.

- **function-go-templating** renders Go templates to YAML text, then parses
  that YAML back into protobuf structs. This serialize-then-deserialize cycle
  is the bottleneck -- YAML parsing cost grows linearly with resource count.

- **function-pythonic** runs a real CPython 3.14 interpreter. Every call goes
  through Python's object protocol, the GIL serializes execution, and objects
  must be marshaled across the Python/gRPC boundary.

- **function-kcl** compiles KCL source, runs it through the KCL VM, outputs
  KRM YAML, and parses that back to protobuf. It carries the largest image
  (588 MB) due to the KCL toolchain.

## Scaling behavior

The gap widens as resource count increases because:
- Starlark's per-resource cost is ~10 us (one `Resource()` call + dict-to-struct)
- Go-templating's per-resource cost is ~35 us (template render + YAML parse)
- Pythonic's per-resource cost is ~115 us (CPython object creation + marshal)
- KCL's per-resource cost is ~145 us (KCL eval + KRM serialize + parse)

At 50 resources, Starlark is 2.2x faster than go-templating and 7.4x faster
than KCL.

## Running the benchmarks

### Prerequisites

```bash
# Build function-starlark image
docker build . --tag=runtime

# Pull comparison images
docker pull xpkg.upbound.io/upbound/function-go-templating:v0.11.4
docker pull xpkg.upbound.io/crossplane-contrib/function-pythonic:v0.5.0
docker pull xpkg.upbound.io/crossplane-contrib/function-kcl:v0.12.0
```

### Comparative benchmarks (gRPC over Docker)

```bash
# All functions, 10 + 50 resources
go test -bench=BenchmarkCompare -benchmem -count=3 -run='^$' -timeout=300s ./bench/

# Single function
go test -bench=BenchmarkCompare_Starlark -benchmem -count=3 -run='^$' ./bench/
```

### In-process benchmarks (no Docker)

```bash
# Full RunFunction pipeline
go test -bench=BenchmarkRunFunction -benchmem -count=5 -run='^$' .

# Runtime-only execution + memory
go test -bench=Benchmark -benchmem -count=5 -run='^$' ./runtime/
```

### CI regression detection

The CI pipeline runs benchmarks on every push with a 140% alert threshold:

```bash
go test -bench=. -benchmem -count=5 -run='^$' ./...
```

Results are tracked over time via
[github-action-benchmark](https://github.com/benchmark-action/github-action-benchmark).
