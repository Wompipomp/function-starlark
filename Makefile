.PHONY: generate lint test bench build render render-check xpkg clean stdlib-push stdlib-push-local

# Run code generation (deepcopy methods + CRD schemas)
generate:
	go generate ./...

# Run linter
lint:
	golangci-lint run

# Run tests
test:
	go test -race -count=1 ./...

# Run benchmarks
bench:
	go test -bench=. -benchmem -count=1 -run='^$$' ./...

# Build container image
build: generate
	docker build . --tag=runtime

# Run crossplane render with example fixtures
render: build
	crossplane render example/xr.yaml example/composition.yaml example/functions.yaml

# Render and compare against expected output (non-zero exit on mismatch).
# Filters version-dependent noise from `crossplane render`:
#   - generateName: present in some CLI versions
#   - "Unready resources: ..." Ready aggregation message: produced by some CLI
#     versions on top of our explicit Composite.Ready=False; redundant with the
#     ComposedResourcesReady condition we emit ourselves.
render-check: build
	crossplane render example/xr.yaml example/composition.yaml example/functions.yaml --include-function-results 2>/dev/null | grep -v -E '^\s*generateName:|^\s*message: .Unready resources:' | diff - example/expected-output.yaml

# Build Crossplane package
xpkg: build
	crossplane xpkg build -f package --embed-runtime-image=runtime -o function-starlark.xpkg

# Clean build artifacts
clean:
	rm -rf package/input/ function-starlark.xpkg

# Stdlib publishing
STDLIB_REGISTRY ?= ghcr.io/wompipomp/starlark-stdlib
STDLIB_VERSION ?= dev

stdlib-push: ## Push stdlib to registry (STDLIB_REGISTRY and STDLIB_VERSION configurable)
	cd stdlib && oras push $(STDLIB_REGISTRY):$(STDLIB_VERSION) \
		--artifact-type application/vnd.fn-starlark.modules.v1+tar \
		networking.star naming.star labels.star conditions.star

stdlib-push-local: ## Push stdlib to localhost:5000 for local testing
	cd stdlib && oras push localhost:5000/starlark-stdlib:dev \
		--artifact-type application/vnd.fn-starlark.modules.v1+tar \
		networking.star naming.star labels.star conditions.star
