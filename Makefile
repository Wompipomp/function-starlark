.PHONY: generate lint test bench build render render-check xpkg clean

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

# Render and compare against expected output (non-zero exit on mismatch)
render-check: build
	crossplane render example/xr.yaml example/composition.yaml example/functions.yaml --include-function-results 2>/dev/null | diff - example/expected-output.yaml

# Build Crossplane package
xpkg: build
	crossplane xpkg build -f package --embed-runtime-image=runtime -o function-starlark.xpkg

# Clean build artifacts
clean:
	rm -rf package/input/ function-starlark.xpkg
