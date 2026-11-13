# Proposal

## Why

Teams that adopt prblm put it on every error response of their services, so they need to know what it costs on the request path. The cost should be stated per framework (net/http, gin, fiber v3), because each adapter adds its own work. Today the repository has a single micro-benchmark (`BenchmarkIsSubtype`), no profiles and no published numbers. Any optimization would be a guess.

This change adds reproducible benchmarks for each adapter's request path and for the core hot paths. It profiles them, optimizes only what the profiles show to be hot, and publishes the results in README.md, one section per adapter. It is sequenced after `add-problem-types` (PR #3), so it measures the final adapter API, including namespace resolution and type discovery.

**Scheduling constraint:** the measurements (benchmark runs, profiles, before/after comparisons) are run when the maintainer's machine is otherwise idle, because a loaded machine gives noisy, unrepeatable numbers. Writing the benchmarks and the README scaffolding does not need an idle machine. The tasks keep those two kinds of work apart.

## What Changes

- **Benchmarks, as `_test.go` files only.** They use `b.Loop()`, sub-benchmarks per case and `b.ReportAllocs()`, and live in the module whose code they measure:
  - **core (`prblm`):**
    - net/http request-path benchmarks through `Handler.Wrap`
    - micro-benchmarks for `Resolve`, `Respond` (JSON and XML encoding), Accept negotiation, `isSubtype` and `resolveType`
    - the discovery handler (200 and 304)
  - **`ginprblm`:** request-path benchmarks through `Middleware` (and `Recovery`) on a `gin.Engine`.
  - **`fiberprblm`:** request-path benchmarks through `ErrorHandler`, driving the app's fasthttp handler directly so that `app.Test`'s network round trip is not measured.
- **The same request-path cases in every adapter**, so the numbers can be compared:
  - a mapped error to a JSON problem
  - an unmapped error to the 500 fallback
  - XML negotiation
  - a type resolved under a namespace
  - type documentation (200)
  - documentation revalidation (304)
- **A baseline row:** for each adapter, an equivalent handler that writes a fixed JSON error body without prblm. The overhead of the library is then visible next to the framework's own cost.
- **Profiling workflow:**
  - CPU and memory profiles (`-cpuprofile`, `-memprofile`) of each adapter's request-path benchmarks
  - commands documented in a `BENCHMARKS.md` file next to the README
- **Profile-driven optimizations.** Only for functions the profiles show as hot. Each optimization:
  - is behavior-preserving, with the full test suites green
  - is shown by `benchstat` to be an improvement (old against new, `-count=10`)
  - is dropped when `benchstat` shows no significant change
- **README.md results:** a "Performance" section with one subsection each for net/http, gin and fiber. It holds the `benchstat` tables (time/op, B/op, allocs/op) and names the machine (CPU model), OS, Go version, module versions and the exact commands to reproduce.
- **CI smoke run:** every benchmark runs once (`-run '^$' -bench . -benchtime 1x`), so benchmarks keep compiling and don't panic. CI publishes no timings: shared runners are too noisy for that.

No public API changes. No default changes. No new module dependencies.

## Defaults and Customization

Nothing a consumer configures changes. With zero configuration, `prblm.NewHandler()`, `ginprblm.Middleware()` and `fiberprblm.ErrorHandler()` behave exactly as after `add-problem-types`:
- an unmapped error is a 500 problem with title "Internal Server Error" and no detail
- no error text on any 5xx, no stack traces, no database internals
- `instance` without the query string

Optimizations must keep every one of these guarantees. The existing suites, including the parity test, are the proof. No opt-in is added, and no safety default is loosened.

## Capabilities

### New Capabilities

None. Benchmarks, profiles and documentation are not system behavior.

### Modified Capabilities

None. Optimizations are behavior-preserving by definition. A candidate optimization that would change observable output (status, headers, body bytes, hook calls) is out of scope and is dropped. This change therefore sets `skip_specs: true`.

## Scope by Gall's Law

**The simplest version that works end to end:** one request-path benchmark, a mapped error to a JSON problem through net/http, run with `-benchmem`, and its result in a README table with the machine and command. Each later slice adds one thing to that working baseline:

1. the remaining net/http cases and the core micro-benchmarks
2. gin
3. fiber
4. profiles and profile-driven optimizations, each one verified with `benchstat`
5. the final README results and the CI smoke run

**Deliberately left for later:**
- **Continuous performance tracking** (benchmark history across commits, regression gates in CI). It needs stable dedicated runners.
- **Load tests over a real network** (wrk, vegeta). They measure the server and the network more than the library.
- **Benchmarks for the mapper plugins.** They land with `add-mapper-plugins`.
- **Changing public API or defaults for speed.** That would be a separate change, with its own compatibility decision.

## Impact

- **Code:** new `bench_test.go` files in the core, `ginprblm` and `fiberprblm`. Possibly small internal optimizations in hot functions that the profiles identify. The exact functions are unknown until the profiles are taken.
- **Documentation:** a "Performance" section in README.md and a new BENCHMARKS.md with the reproduction and profiling commands.
- **CI:** one smoke step in `.github/workflows/ci.yml`.
- **Dependencies:** none in any module. `benchstat` (`golang.org/x/perf/cmd/benchstat`) and `go tool pprof` are developer tools, installed with `go install` or already in the toolchain, and are not added to any `go.mod`.
- **Sequencing:** after `add-problem-types` (PR #3) is merged. It doesn't conflict with `add-mapper-plugins`, but if both are in flight, the release in that change's slice 7 should come after this one, so the published numbers match the released code.
