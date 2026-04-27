# Changelog

## [0.11.0](https://github.com/Wompipomp/function-starlark/compare/v0.10.0...v0.11.0) (2026-04-27)


### Features

* **11:** add package-local OCI detector and scanner wiring ([e35672d](https://github.com/Wompipomp/function-starlark/commit/e35672dfb51e53fc7980357ddf1042f61e82656a))
* **11:** document package-local load scheme and add e2e fixture ([19e86e4](https://github.com/Wompipomp/function-starlark/commit/19e86e42be363d19c512b5cca898c4026c37e54c))
* **11:** wire package-local loads into loader and resolver ([728e390](https://github.com/Wompipomp/function-starlark/commit/728e39002d6da156ff5afde3325c5b8231496537))
* **36-01:** implement recursive dict.compact with depth guard ([b9a3411](https://github.com/Wompipomp/function-starlark/commit/b9a3411d277907a4f22c61d48ce8376e87903977))
* **37-01:** add observed dict to Collector and update all call sites ([03911c4](https://github.com/Wompipomp/function-starlark/commit/03911c4842c122be399aa95110ee546301626141))
* **37-01:** extract recordSkip method and refactor skipResourceFn ([99d24f4](https://github.com/Wompipomp/function-starlark/commit/99d24f435136dfe82f22e7331ac37ab584cbc613))
* **38-01:** implement when/skip_reason/preserve_observed gate logic ([82b8b5e](https://github.com/Wompipomp/function-starlark/commit/82b8b5e749de50c0352fd46f739199ca0c81c5bc))
* **38-02:** implement preserve_observed path with observed body lookup ([a4f3b2e](https://github.com/Wompipomp/function-starlark/commit/a4f3b2e4926132876b718c6df5692d4218301120))
* **39-01:** add dict.compact reference documentation ([9c2df82](https://github.com/Wompipomp/function-starlark/commit/9c2df82276fa64f6e71ed49d36816f9cc05a83d9))
* **39-01:** add Resource() gating kwargs documentation with state table ([2dbd09b](https://github.com/Wompipomp/function-starlark/commit/2dbd09bf478dee1d8c2482194aff7ec51c6c130c))
* **39-03:** add dict.compact and when=False E2E test scenarios ([c2cd5e9](https://github.com/Wompipomp/function-starlark/commit/c2cd5e9576b2028e141b0e6e3c10612409695172))
* **39-03:** add preserve_observed two-phase reconciliation E2E test ([4a39859](https://github.com/Wompipomp/function-starlark/commit/4a39859ea9ed94cc1949932c53e0a29dfa297937))
* add dict.compact builtin to remove None-valued entries from dictionaries ([386adfb](https://github.com/Wompipomp/function-starlark/commit/386adfbb40bda537952db51ee0949a7f8f8d0c6f))
* add OCI pull policy support to control tag revalidation behavior ([a14e741](https://github.com/Wompipomp/function-starlark/commit/a14e741055aa38015652a265fc53f37d80d4c867))
* implement composite readiness gating with optional resource support and set_composite_ready builtin ([0d205dd](https://github.com/Wompipomp/function-starlark/commit/0d205dd4cd0ba0266c42b41f78161f6bb39b9f2f))
* implement transitive skipping and deferred dependency gating for conditional resources ([52889eb](https://github.com/Wompipomp/function-starlark/commit/52889eb7a3c8bc732fa08c6f69fce4ff8680bad8))
* **quick-13:** auto-compact Resource() body to strip None-valued entries ([6cdfae4](https://github.com/Wompipomp/function-starlark/commit/6cdfae4d78e239ded5dd109d62b09e5ee50cdeb2))
* **quick-14:** allow skip_reason with when=True in Resource() ([886e9d7](https://github.com/Wompipomp/function-starlark/commit/886e9d7552bbb20d46573a277adda736251e0274))


### Bug Fixes

* **11:** replace if-else chain with switch in ExpandPackageLocal ([f04257f](https://github.com/Wompipomp/function-starlark/commit/f04257fff143d22543d310a3b3f0ca7e2de56b38))
* filter out version-dependent unready resource messages in render-check and update expected output accordingly ([8812579](https://github.com/Wompipomp/function-starlark/commit/8812579e556a7f3d191862d8b84387a26c70b007))
* implement callable module wrapper for dict to support both namespace access and constructor calls ([2550aa8](https://github.com/Wompipomp/function-starlark/commit/2550aa8d9e82c1005ea1d911c7389dd20ece2dae))
* strip read-only metadata fields for preserve_observed and sanitize module scan thread names for package-local imports ([4aa7309](https://github.com/Wompipomp/function-starlark/commit/4aa73092b24c133351ef61a72d182d49808ec60b))


### Performance Improvements

* implement lazy allocation in compactDict and compactList to avoid unnecessary copying ([f97aaf1](https://github.com/Wompipomp/function-starlark/commit/f97aaf12392794beb65f1f40b0f0e134b3aaaa06))

## [0.10.0](https://github.com/Wompipomp/function-starlark/compare/v0.9.0...v0.10.0) (2026-04-13)


### Features

* **29-01:** register json module in BuildGlobals predeclared globals ([428c024](https://github.com/Wompipomp/function-starlark/commit/428c024c9b814570b94e32819c02de6c04c1fe53))
* **30-01:** implement crypto module with 7 functions + register in BuildGlobals ([a7b3289](https://github.com/Wompipomp/function-starlark/commit/a7b328955d4dec693056878e5b0281c06b19cf62))
* **30-02:** implement encoding module with 8 functions + register in BuildGlobals ([d390c21](https://github.com/Wompipomp/function-starlark/commit/d390c21dd2d9f84f6d08b27312e957c82c102193))
* **30-03:** implement dict module with 6 functions and register in BuildGlobals ([b5aa369](https://github.com/Wompipomp/function-starlark/commit/b5aa3697ae16a58cce6ffec2d9fe8bf5ff480577))
* **31-01:** implement regex module with 7 functions and LRU cache ([032e730](https://github.com/Wompipomp/function-starlark/commit/032e73077e7e47e2c6b216a1c80d2b2214349584))
* **31-02:** implement yaml module with 3 functions and register in BuildGlobals ([fa8abc0](https://github.com/Wompipomp/function-starlark/commit/fa8abc0e70b8904fba5e2bcc1a56e65a770b3ff3))
* **32-01:** implement get_extra_resource and get_extra_resources builtins ([186b280](https://github.com/Wompipomp/function-starlark/commit/186b2803e1a43a765252864f1a877ff462b292e2))
* **32-01:** implement is_observed, observed_body, and get_condition builtins ([aac1ebb](https://github.com/Wompipomp/function-starlark/commit/aac1ebb7b740a994361c18c8589bf248954a78a5))
* **32-02:** implement TTLCollector and set_response_ttl builtin ([9adb884](https://github.com/Wompipomp/function-starlark/commit/9adb884e21d15aa4a96cb318e3c1e10e7ca77f85))
* **32-02:** integrate user TTL override in fn.go ([b2a1ddf](https://github.com/Wompipomp/function-starlark/commit/b2a1ddf80e28c3c8912fbb0572966a483156a7a0))
* **32-03:** implement all_ready and any_degraded in conditions.star ([fdeff1d](https://github.com/Wompipomp/function-starlark/commit/fdeff1d8bce1f0f3dd47e0359ea1fa68727b5381))
* **33-01:** update builtins-reference.md with all v1.8 additions ([5b1299f](https://github.com/Wompipomp/function-starlark/commit/5b1299ff97d0edfb9d19e0e3b5b3b4cb8162eb27))
* **33-01:** update starlark-primer.md and llms.txt with v1.8 predeclared names ([83b07e1](https://github.com/Wompipomp/function-starlark/commit/83b07e16fcac7950de937816d83c30ac1fc8b47c))
* **33-02:** create migration-cheatsheet.md with Sprig+KCL mapping ([1802542](https://github.com/Wompipomp/function-starlark/commit/180254244a5a0a96f1a43d2a9e34a95854c074cb))
* **33-02:** update features.md and best-practices.md with v1.8 content ([ea65c10](https://github.com/Wompipomp/function-starlark/commit/ea65c108bc958365797cbc0f540c384691a45470))
* **33-03:** add 7 example compositions for v1.8 namespaces ([1ef1c2e](https://github.com/Wompipomp/function-starlark/commit/1ef1c2ef474f49a03a494229e2dfe527b39b34a9))
* **33-03:** refresh README with comparison table, alpha disclaimer, and 34-name count ([7553608](https://github.com/Wompipomp/function-starlark/commit/75536089fef2667f4bf23bca1e1ecca9471d65bb))
* **34-01:** add crypto and regex modules to naming test predeclared dicts ([ac7681d](https://github.com/Wompipomp/function-starlark/commit/ac7681d6d9fd48d9337bdfb90efbdda48001174e))
* **34-01:** replace hand-rolled hash and sanitize with crypto/regex builtins ([1bf2f8a](https://github.com/Wompipomp/function-starlark/commit/1bf2f8a8ca902b593acf328b7fca0b8150785a2b))
* **34-02:** harden labels.star and networking.star with v1.8 builtins ([8215146](https://github.com/Wompipomp/function-starlark/commit/82151465a21648aee08b6a77140bfca63773601a))
* **34-03:** add v1-to-v2 stdlib migration section to cheatsheet ([5e2c787](https://github.com/Wompipomp/function-starlark/commit/5e2c787adb7c867712528bb19a03164ca6b71567))
* **34-03:** use version-neutral artifact type in stdlib CI workflow ([19f3cef](https://github.com/Wompipomp/function-starlark/commit/19f3cef837686940080295f5a7a4e1fed35bde9c))
* **34-04:** add namespace builtin assertions to e2e test runner ([4ba1547](https://github.com/Wompipomp/function-starlark/commit/4ba15476ed38fda0ace2012c6a6b750289c70ef7))
* **34-04:** add namespace builtin tests to e2e composition ([c174739](https://github.com/Wompipomp/function-starlark/commit/c174739f6151dadb9fa47363d0171179dfe95e9a))
* **35-01:** fix regex empty-match, YAML stream limit, deep_merge depth, CI artifact type ([c513b2a](https://github.com/Wompipomp/function-starlark/commit/c513b2a7eb497aa008482d7f8b05dec186ed4a8c))
* **35-02:** fix API consistency in extra_resources, ttl, and encoding ([ce605be](https://github.com/Wompipomp/function-starlark/commit/ce605be3483659bc29ddde90613e712af807c74f))
* **quick-10-01:** add gRPC credential support for OCI registry authentication ([60d1330](https://github.com/Wompipomp/function-starlark/commit/60d133057151a278426ff9e54c4a9a02fa986a72))


### Bug Fixes

* **35-01:** update e2e counts to 34, fix test numbering, add dict.pick key validation ([09b0e9d](https://github.com/Wompipomp/function-starlark/commit/09b0e9d7cc90f028622a666ba0d1487d5dc2c2b9))
* **35-02:** safe type assertions in tests and blake3 reference digest ([cc99bfa](https://github.com/Wompipomp/function-starlark/commit/cc99bfa58875ca9966f4ab9c7c0dd13b276b0bc3))
* add gosec lint suppressions to crypto builtins, simplify hex validation logic, and reformat builtin map alignment ([c720e54](https://github.com/Wompipomp/function-starlark/commit/c720e540104693ff2efd8e444fa9cefa8a76a616))

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
