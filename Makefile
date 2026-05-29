.PHONY: generate lint test bench build render render-check xpkg clean stdlib-push stdlib-push-local

# crossplane CLI to use. Defaults to `crossplane` on PATH; override to point at a
# v2 crank binary without replacing an existing v1 install, e.g.
#   make render-check CROSSPLANE=$HOME/.local/bin/crank-v2
CROSSPLANE ?= crossplane

# Pinned crossplane render-engine version. `composition render` runs this engine
# in Docker; pinning it keeps render output reproducible across hosts (CI/local).
XP_ENGINE_VERSION ?= v2.3.1

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

# Run crossplane render with example fixtures (v2 render engine)
render: build
	$(CROSSPLANE) composition render example/xr.yaml example/composition.yaml example/functions.yaml --crossplane-version=$(XP_ENGINE_VERSION)

# Render and compare against expected output (non-zero exit on mismatch).
# Both sides pass through normalize-render.py to normalize timestamps/uids and
# canonicalize document order (see that script for details).
render-check: build
	@actual=$$(mktemp) expected=$$(mktemp); \
	$(CROSSPLANE) composition render example/xr.yaml example/composition.yaml example/functions.yaml --include-function-results --crossplane-version=$(XP_ENGINE_VERSION) 2>/dev/null | python3 example/normalize-render.py > "$$actual"; \
	python3 example/normalize-render.py < example/expected-output.yaml > "$$expected"; \
	diff "$$actual" "$$expected"; \
	rc=$$?; rm -f "$$actual" "$$expected"; exit $$rc

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
