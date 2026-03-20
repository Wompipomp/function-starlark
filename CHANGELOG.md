# Changelog

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
