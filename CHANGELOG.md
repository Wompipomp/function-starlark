# Changelog

## [0.9.0](https://github.com/Wompipomp/function-starlark/compare/v0.8.1...v0.9.0) (2026-04-04)


### Features

* optimize OCI resolution with HEAD requests and add cycle detection for star imports ([9dba680](https://github.com/Wompipomp/function-starlark/commit/9dba68075341454f809ea52366ac4f9ecabb33c6))
* **quick-8-01:** expand star imports in module source before compilation ([2223859](https://github.com/Wompipomp/function-starlark/commit/22238591c155e278ea300dfb05bbc45315675318))


### Bug Fixes

* mirror requirements to ExtraResources for backward compatibility with crossplane render &lt;=v1.20 ([9b16a1f](https://github.com/Wompipomp/function-starlark/commit/9b16a1f01d8e14995490ebcbedd00d43605b346b))

## [0.8.1](https://github.com/Wompipomp/function-starlark/compare/v0.8.0...v0.8.1) (2026-03-29)


### Bug Fixes

* namespace OCI modules by full URL to prevent cross-package collisions ([67ee8d1](https://github.com/Wompipomp/function-starlark/commit/67ee8d10e5123dbeb0a8bfdd1032c7dc5d772f7b))

## [0.8.0](https://github.com/Wompipomp/function-starlark/compare/v0.7.0...v0.8.0) (2026-03-28)


### Features

* add env var fallbacks and promote ociCacheTTL to CLI ([8a5fd4c](https://github.com/Wompipomp/function-starlark/commit/8a5fd4c29987a2cf0fe2558c7c329caa679197c6))
* default Usage API to v2 (Crossplane 2.x) and rename to ResolveUsageAPIVersion ([e96d6c5](https://github.com/Wompipomp/function-starlark/commit/e96d6c5d04c39417fc0c864fe5940194035990e6))
* implement end-to-end testing suite, add OCI insecure registry fallback, and update resource metadata handling in builtins ([eed5dcd](https://github.com/Wompipomp/function-starlark/commit/eed5dcdf65fc6618e600f445e9a1beeceed36330))
* inject resource-name labels, update Usage to use resourceSelectors, and improve OCI resolution and deduplication logic ([ad2d0eb](https://github.com/Wompipomp/function-starlark/commit/ad2d0ebec71d66397e2be8e64389166c0a8de81a))
* **quick-3:** preserve relative paths in OCI extraction and raise maxFileCount to 1000 ([60e2ec2](https://github.com/Wompipomp/function-starlark/commit/60e2ec2587ece3fa6ac60231e3741c77c87a553b))
* **quick-4:** implement namespace alias support for load() star imports ([4817ada](https://github.com/Wompipomp/function-starlark/commit/4817adaa969362ed3df8bc715a6fc8cce624865b))


### Bug Fixes

* **quick-4:** update expected globals count for struct builtin ([2e4be5f](https://github.com/Wompipomp/function-starlark/commit/2e4be5f3ba4bbae44d8aaedcab068418dd16509b))
* rewrite if-else chain to switch in ResolveStarImports per gocritic lint ([1dd65a9](https://github.com/Wompipomp/function-starlark/commit/1dd65a98b41775ab25ad102890cf770407b867d4))
* set usageAPIVersion to v1 in e2e compositions for Crossplane 1.x ([0901eae](https://github.com/Wompipomp/function-starlark/commit/0901eaebe5f3c7045a5f4c0a8449716d7a8b71d1))
* suppress gosec G703 false positive on validated path in buildKeychain ([7ad53ac](https://github.com/Wompipomp/function-starlark/commit/7ad53ac2779c42e30366d2b5a043895d2b38a985))
* update expected render output for v2 Usage API default ([233ac9d](https://github.com/Wompipomp/function-starlark/commit/233ac9dda0bf26ff5091f236348e734fb942d6b7))
* use canonical registry matching for insecure registries and add test coverage ([f7673c9](https://github.com/Wompipomp/function-starlark/commit/f7673c93b8e0b906e6feb81725b016f6c57bd043))

## [0.7.0](https://github.com/Wompipomp/function-starlark/compare/v0.6.0...v0.7.0) (2026-03-20)


### Features

* **25-01:** implement FieldDescriptor type and field() builtin ([7930a31](https://github.com/Wompipomp/function-starlark/commit/7930a31ba148288b7419e59081fe2f642b63f886))
* **25-01:** implement Levenshtein distance and Suggest utility ([401f17e](https://github.com/Wompipomp/function-starlark/commit/401f17ed3d7a65c4c0853974c50f24014fdf37a5))
* **25-02:** implement SchemaCallable type and validation utilities ([7cbe607](https://github.com/Wompipomp/function-starlark/commit/7cbe607cb2d8322a13025cf819adaf8e9cadc6df))
* **25-02:** register schema and field builtins in BuildGlobals ([9536832](https://github.com/Wompipomp/function-starlark/commit/9536832dfe3b65da312b79adb0b87aee123ed8d9))
* **26-01:** extend FieldDescriptor with typeParam Unpacker and items= kwarg ([00fd0fd](https://github.com/Wompipomp/function-starlark/commit/00fd0fd07c1aebe9638d6dff1425492950af3651))
* **26-01:** implement SchemaDict type with starlark interface delegation ([392a1ad](https://github.com/Wompipomp/function-starlark/commit/392a1ad9e5748f71b2905b48430c20ccb62728e4))
* **26-02:** implement recursive validation with path accumulation and SchemaDict return ([e69c541](https://github.com/Wompipomp/function-starlark/commit/e69c5418fca439e330ace93a6de66cf55b65acdf))
* **27-01:** wire SchemaDict into Resource() and starlarkToProtoValue pipeline ([0a3cf9f](https://github.com/Wompipomp/function-starlark/commit/0a3cf9f3b2e13e98ad467e901d82a66e1fd1b32e))


### Bug Fixes

* Prevent out-of-bounds access in test utilities when processing an odd number of elements. ([a13e021](https://github.com/Wompipomp/function-starlark/commit/a13e02140c834eb5aa2c72732c6ca8e0a1b6105d))
* validate default OCI registry at input boundary ([bd2d999](https://github.com/Wompipomp/function-starlark/commit/bd2d9992c3225cf641bcc380435945c527122a80))


### Performance Improvements

* **27-02:** add 20-field schema construction benchmark ([d06bf3a](https://github.com/Wompipomp/function-starlark/commit/d06bf3a377dc92803fbd4be01e5f4981b511dc1e))

## [0.6.0](https://github.com/Wompipomp/function-starlark/compare/v0.5.3...v0.6.0) (2026-03-19)


### Features

* **22-01:** rename require_resource to require_extra_resource in Go source ([a325e5b](https://github.com/Wompipomp/function-starlark/commit/a325e5bec5e917fe6ba87a37e51ba9e002730cd4))
* **23-01:** implement default registry helpers ([2589bc0](https://github.com/Wompipomp/function-starlark/commit/2589bc0795e6be878744c727a21ff36d8ab7bf86))
* **23-01:** wire default registry through scanner, loader, resolver, and fn.go ([267024a](https://github.com/Wompipomp/function-starlark/commit/267024ab93ebbbc64b1fdd3f815251772f7d2cd6))
* **23-02:** document short-form OCI load syntax and default registry configuration ([002a92b](https://github.com/Wompipomp/function-starlark/commit/002a92b929bfa4a252047c97e10df3c6ca9ff082))
* **23-02:** update llms.txt, builtins-reference, and example with short-form syntax ([4c7b8a5](https://github.com/Wompipomp/function-starlark/commit/4c7b8a58377a7bf936dbf710569370b30e27ed5f))
* **24-01:** add release-please GitHub Actions workflow ([3c3c7a5](https://github.com/Wompipomp/function-starlark/commit/3c3c7a577476378df497b7d00d6fc4df52e9ff07))
