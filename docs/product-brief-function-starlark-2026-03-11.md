---
stepsCompleted: [1, 2, 3, 4, 5, 6]
inputDocuments:
  - 'docs/technical-crossplane-composition-function-scripting-language-research-2026-03-11.md'
  - 'docs/context-handoff-function-starlark-2026-03-11.md'
date: '2026-03-11'
author: 'Wompipomp'
---

# Product Brief: function-starlark

## Executive Summary

function-starlark is an open source Crossplane composition function that lets platform engineers write compositions in Python-like syntax and run them at Go speed. It eliminates the forced tradeoff between expressiveness and efficiency that plagues current composition functions: function-kcl and function-pythonic offer programming power but consume excessive resources at scale, while function-go-templating is lean but lacks the capabilities of a real programming language.

function-starlark delivers feature completeness — matching the capabilities of function-kcl, function-go-templating, and function-pythonic (context access, observed/desired state manipulation, conditions and events, connection details, extra resources) — while maintaining a minimal memory footprint through Starlark's pure Go bytecode VM. Automated dependency management between composed resources is designed into the architecture from day one, solving one of Crossplane's most persistent pain points (Issue #2072).

---

## Core Vision

### Problem Statement

Crossplane composition functions force platform teams into an unacceptable tradeoff: **power or performance, but not both.** Teams building complex compositions — especially those at scale with numerous compositions — hit hard limits with every available option:

- **function-kcl**: Rich language features but memory leaks (5GB+ idle), heavy resource consumption that becomes unsustainable at scale
- **function-pythonic**: Python readability but runtime overhead that compounds with composition count
- **function-go-templating**: Minimal resource usage but insufficient expressiveness for complex composition logic — not a real programming language
- **Custom Go functions**: Maximum power but prohibitively complex for platform engineers who aren't Go developers

### Problem Impact

Platform engineering teams — often composed of former domain administrators (IAM, networking, storage) rather than software developers — are responsible for maintaining compositions. These teams need approachable tooling, not programming language expertise. When forced into function-kcl for its capabilities, they inherit resource costs that strain cluster budgets. When forced into function-go-templating for efficiency, they hit expressiveness walls that make complex compositions brittle, unreadable, and hard to maintain.

Additionally, Crossplane lacks native dependency management between composed resources (Issue #2072, closed as not_planned). Teams must manually implement resource ordering through complex conditions and state tracking — a pattern that is error-prone and opaque to non-technical composition authors.

### Why Existing Solutions Fall Short

| Solution | Strength | Critical Gap |
|---|---|---|
| function-kcl | Expressive language, context access, events/conditions | Memory leaks, 200MB+ baseline, resource hungry at scale |
| function-pythonic | Python readability, package ecosystem, helper functions | Python runtime overhead, scaling cost |
| function-go-templating | Minimal footprint (~20MB), fast, context access | Limited to templating logic, no real programming constructs |
| Custom Go functions | Full power, maximum performance | High development barrier, not accessible to platform engineers |

No existing solution delivers all three requirements simultaneously: **feature completeness, easy-to-read syntax, and minimal resource footprint.**

### Proposed Solution

**function-starlark** — a Crossplane composition function that lets you write compositions in Python-like syntax and run them at Go speed. Powered by Google's Starlark interpreter compiled into a single Go binary, it delivers:

- **Feature completeness** — full parity with existing composition functions: XR context access, observed/desired state manipulation, conditional resource creation, connection details propagation, events and conditions, extra resources
- **Python-like syntax** that platform engineers can read and write without programming background
- **Native Go performance** — pure Go bytecode VM with ~20MB memory footprint, zero CGo overhead, single binary deployment
- **Dependency management by design** — DAG-based resource ordering via `depends_on()` declarations, architected from day one to eliminate manual condition and state management
- **Hermetic sandboxing** — deterministic execution with no I/O or network access unless explicitly provided
- **Shared library support** — reusable Starlark modules are possible for teams that want to share utilities, but not a primary focus

### Key Differentiators

1. **The power-performance breakthrough**: First composition function to deliver full programming language capability and feature completeness at go-templating-level resource efficiency
2. **Non-programmer accessibility**: Python-like syntax means platform engineers (IAM admins, network engineers) can author compositions without learning a new paradigm
3. **Automated dependency management**: Solves Crossplane Issue #2072 through declarative `depends_on()` — architected from the ground up, not bolted on. A feature no other composition function offers
4. **Feature parity, zero compromise**: Context access, conditions, events, connection details, extra resources — everything teams rely on from function-kcl and function-go-templating, without the resource cost or expressiveness limits
5. **Battle-tested foundation**: Starlark is proven at massive scale by Google (Bazel) and Uber, with 990+ Go importers on GitHub

## Target Users

### Primary Users

#### 1. Domain Platform Engineer — "Lisa"
**Role:** IAM / Networking / Storage Platform Engineer
**Background:** Former domain administrator (5+ years), transitioned into platform engineering. Comfortable with shell scripting, maybe some basic Python. Not a programmer by identity — but writes infrastructure logic daily. Limited Crossplane experience, learned on the job.

**Problem Experience:** Lisa maintains 5-10 compositions for her IAM domain. She hit expressiveness walls with function-go-templating, moved to function-kcl but finds the syntax unfamiliar and memory consumption alarming. She spends more time fighting tooling than solving IAM problems.

**Success Vision:** Lisa opens a `.star` file, reads it like Python, and immediately understands what it does. She writes a new composition in an afternoon, not a week. Her pod uses 20MB instead of 500MB. She consumes utility functions that Marcus built, abstracting away complexity she doesn't need to see. *"I finally feel like I'm writing infrastructure logic, not wrestling a language."*

#### 2. Senior Platform Engineer — "Marcus"
**Role:** Platform Engineering Lead / Cloud Native Specialist
**Background:** Deep Crossplane and Kubernetes expertise. Builds complex multi-level compositions like cloud-native landing zones — dozens of managed resources with cross-resource references and multi-layer abstractions.

**Problem Experience:** Marcus builds landing zone compositions with 30+ resources and deep dependency chains. With function-kcl, memory spikes to gigabytes. With function-go-templating, templates become unreadable spaghetti. He manually implements resource ordering through conditions and state tracking — fragile code that breaks when resources change.

**Key Dynamic:** Marcus is Lisa's enabler. He builds the internal utility libraries and abstractions that Lisa consumes. He designs the patterns, Lisa applies them to her domain. The product must serve both through different layers — Marcus needs full language power, Lisa needs approachable abstractions built on top of it.

**Success Vision:** Marcus writes a landing zone composition with `depends_on()` declarations and resources deploy in the correct order automatically. He publishes internal utility functions that his team reuses across domains. *"It's like writing Python but it runs like Go — and I never have to think about resource ordering again."*

#### 3. Platform Architect / Decision Maker — "David"
**Role:** Platform Architecture Lead
**Background:** Evaluates and approves tooling for the platform engineering team. Responsible for cluster stability, security posture, and team productivity. David is the gatekeeper — if he doesn't buy in, Lisa and Marcus never get to use function-starlark.

**Key Concerns:** Operational risk (What if the VM has a bug? What's the blast radius?), scalability under load, ease of adoption for mixed-skill teams, security (hermetic sandbox), and escape hatch (can we migrate away if needed?). He assesses **risk of adoption vs. risk of staying on function-kcl**.

**What convinces David:** Proven VM (Google Bazel, Uber), hermetic sandbox (can't break the cluster), single binary (nothing to patch), compositions are just configuration (migration is feasible). Resource consumption drops, team velocity increases, fewer composition-related incidents.

**Success Vision:** *"It ticks all the boxes — powerful, efficient, and my team picked it up in a day. Lower risk than what we're running today."*

### Secondary Users

#### 4. Community Contributor — "Priya"
**Role:** Open Source Contributor / Crossplane Community Member
**Background:** Experienced Crossplane user who contributes to the ecosystem. Interested in improving the function-starlark project itself — writing built-in functions, improving the runtime, adding Crossplane-centric utilities to the core.

**Success Vision:** Priya submits a PR adding a new built-in function, the contribution process is clear, and her addition helps hundreds of teams. *"The project is well-structured, contribution is straightforward, and my work has real impact."*

*Note: Shared utility libraries (e.g., name normalization, metadata helpers) are more likely written internally by teams like Marcus's, tailored to their internal developer platform, rather than as open source community contributions.*

### User Journey

| Stage | Experience |
|---|---|
| **Discovery** | Platform engineers hit scaling walls with function-kcl or expressiveness limits with function-go-templating. David evaluates alternatives. Word spreads through Crossplane Slack, community calls, GitHub. |
| **Onboarding** | Engineers install function-starlark as a Crossplane package, write their first composition using familiar Python-like syntax. **Aha moment:** *it works like Python, the pod barely uses memory, and I didn't have to learn a new paradigm.* |
| **Core Usage** | Daily composition authoring — creating, modifying, debugging compositions. Accessing XR context, manipulating observed/desired state, writing conditions and events. Marcus builds utility abstractions, Lisa consumes them. |
| **Advanced Usage** | Defining dependency graphs with `depends_on()`. Building internal utility libraries for the platform team. |
| **Long-term** | function-starlark becomes the default composition function. Compositions are readable, maintainable, resource-efficient. Community contributors improve the core project. |

## Success Metrics

### Technical Performance Targets

| Metric | Target | Benchmark |
|---|---|---|
| Memory footprint (idle) | ~20-40MB | Within 2x of function-go-templating (~20MB) |
| Memory scaling curve | Flat/near-flat growth per composition | Must not grow linearly like function-kcl as composition count increases |
| Reconciliation latency | Sub-second for typical compositions | Within 2-3x of function-go-templating for complex compositions |
| Script execution time | Near-native Go speed | Starlark bytecode VM overhead negligible vs. KCL/Python runtimes |

### Feature Parity Checklist (Tiered by User Impact)

**Tier 1 — MVP Blockers** (used in every composition, required to switch):
- [ ] XR (composite resource) context access (spec, status, metadata)
- [ ] Observed/desired state manipulation of composed resources
- [ ] Conditional resource creation
- [ ] Resource patching and field manipulation
- [ ] Connection details propagation

**Tier 2 — Adoption Accelerators** (common features that drive daily usage):
- [ ] Events and conditions writing back to XR
- [ ] Environment configuration access
- [ ] Helper function definitions and reuse

**Tier 3 — Power Features** (advanced capabilities for complex use cases):
- [ ] Extra resources (reading existing cluster resources)
- [ ] Complex data transformations
- [ ] Automated dependency management via `depends_on()`

### Adoption Metrics

| Metric | Indicator |
|---|---|
| **First-use success rate** | New user goes from zero to working composition following docs without external help |
| **GitHub adoption signals** | Stars, forks, issues filed — signs the project is alive and being used |
| **Crossplane ecosystem recognition** | Listed in Crossplane marketplace, mentioned in community calls, potential acceptance as official function |

### Quality Metrics

| Metric | Target |
|---|---|
| **Compatibility test suite** | Real-world function-kcl compositions ported to Starlark syntax, verified to produce identical desired state output |
| **Performance regression tests** | Benchmark tests on every PR — CI catches memory spikes or latency regressions before merge |
| **Test coverage** | Comprehensive unit and integration tests — builds trust for architect personas like David |
| **CI stability** | Green builds, automated release pipeline |

### Business Objectives

As an open source project, business objectives align with ecosystem impact:

- **3-month goal:** MVP with full feature parity (all tiers) including dependency management, published to Crossplane marketplace, first external users
- **12-month goal:** Module/package system shipped, community contributions flowing, recognized alternative to function-kcl in the Crossplane ecosystem

## MVP Scope

### Core Features (MVP)

**Starlark Runtime:**
- Starlark interpreter embedded in single Go binary via `go.starlark.net`
- Script loading from inline Composition YAML input
- Script loading from ConfigMaps (e.g., via volume mounts)
- Bytecode caching for script re-execution performance
- Hermetic sandbox — no I/O, no network, deterministic execution
- `ScriptLoader` interface designed for future pluggable module loading

**Crossplane Feature Parity (Full):**
- XR (composite resource) context access — spec, status, metadata
- Observed/desired state manipulation of composed resources
- Conditional resource creation
- Resource patching and field manipulation
- Connection details propagation
- Events and conditions writing back to XR
- Environment configuration access
- Helper function definitions and reuse
- Extra resources (reading existing cluster resources)
- Complex data transformations

**Dependency Management (User-Facing):**
- DAG engine with dependency graph and wave ordering
- `depends_on()` declarations in Starlark scripts
- Readiness tracking across reconciliation cycles
- Automated wave-based resource deployment — resources emitted in correct order

**Function Host:**
- gRPC server implementing Crossplane function protocol via `function-sdk-go`
- Request/response handling for RunFunctionRequest/RunFunctionResponse
- Proper error handling, logging, and condition reporting

### Out of Scope for MVP

| Feature | Rationale | Target Phase |
|---|---|---|
| Module/package loading (`load()`) | Defer — need to design module resolution and source strategy | Post-MVP |
| OCI artifact distribution | Requires registry integration, download mechanism, versioning | Post-MVP |
| Shared library ecosystem infrastructure | Possible but not a focus — teams build internally for now | Future |

### MVP Success Criteria

- [ ] Full feature parity with function-kcl — ported compositions produce identical desired state output
- [ ] Dependency management working end-to-end with `depends_on()` and wave-based resource ordering
- [ ] Memory footprint within target range (~20-40MB idle)
- [ ] Memory scaling curve flat/near-flat as composition count increases
- [ ] Published as a Crossplane package installable via `crossplane xpkg`
- [ ] At least one real-world composition running successfully in a cluster
- [ ] CI pipeline with benchmark tests catching performance regressions

### Future Vision

**Post-MVP — Module & Package System:**
- `load()` support for importing Starlark modules from external sources
- Module loading from OCI registries, Git repositories, or HTTP endpoints
- Package resolution and versioning strategy
- Enables teams to build, share, and version utility libraries

**Long-term — Ecosystem & Community:**
- Crossplane marketplace listing and ecosystem recognition
- Community-contributed built-in functions and runtime improvements
- Performance optimization and advanced caching strategies
- Potential proposal to Crossplane community as official function
