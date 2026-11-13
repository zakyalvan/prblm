# Design

## Context

The motivation and scope are in proposal.md (Why, Scope by Gall's Law). This change declares no spec deltas: its acceptance criteria are the existing suites staying green, and published numbers that anyone can reproduce.

What exists today:
- **Request paths:**
  - **net/http:** `Handler.Wrap` → `answer` → `Respond` → `trackingWriter.send`.
  - **gin:** `Middleware` installs a `deferredWriter`, runs `c.Next()`, then `finish` → `respond` → `send`.
  - **fiber:** `ErrorHandler` → `respond` → `send` into fasthttp's response header.
  - **discovery:** pre-rendered pages, served by `serveDiscovery` in all three.
- **Benchmarks:** only `BenchmarkIsSubtype` (`namespace_internal_test.go`).
- **CI** (`.github/workflows/ci.yml`) runs `go test -race ./...` and `GOWORK=off go test ./...`. Shared runners are too noisy to time anything.
- **fiber:** `fiberprblm` already requires `github.com/valyala/fasthttp` indirectly, through fiber. `app.Handler()` returns a `fasthttp.RequestHandler`.
- **Scheduling (proposal, Why):** measurement runs only when the maintainer's machine is idle. Writing code and docs does not wait for that.

## Goals / Non-Goals

**Goals:**
- One set of request-path cases, measured the same way in all three adapters. The numbers measure the library and the framework, not the test harness.
- Every published number can be reproduced from a committed command and a committed raw result file.
- Optimizations are driven by profiles, verified by `benchstat`, behavior-preserving, and each lands as its own commit.

**Non-Goals:**
- Changing public API or defaults for speed.
- Performance gates in CI.
- Network load tests.
- Benchmarking consumer code such as mappers and enrichers beyond one representative mapper and enricher.

## Slices

Each slice has two kinds of task:
- **Code tasks**, which can run at any time: writing benchmarks, refactoring, docs.
- **Measurement tasks** (marked *idle machine* in tasks.md), which run only on an idle machine and produce the raw result files.

A slice is complete when both are done. Its code tasks may land earlier, because a benchmark is useful (and smoke-tested) before it has published numbers.

1. **net/http, one case, end to end.**
   - At its end: `BenchmarkWrap/mapped-json`, with a mapped error through `Handler.Wrap` to a JSON problem, and its `baseline` row run with `-benchmem`. The README has a "Performance → net/http" subsection and BENCHMARKS.md has the command. On an idle run, the raw output is committed and the table filled.
   - Hot paths measured: resolution (handler mapper), finalization and enrichment, JSON encoding, response writing in net/http.
2. **net/http, every case, and the core micro-benchmarks.**
   - At its end, the cases from the proposal: fallback-500, xml, resolved-type, discovery-200, discovery-304. Also `BenchmarkResolve`, `BenchmarkRespond` (json, xml), `BenchmarkNegotiate` and `BenchmarkResolveType`, next to the existing `BenchmarkIsSubtype`.
   - Hot paths measured: Accept negotiation, XML encoding, type resolution, discovery serving.
3. **gin.**
   - At its end: `BenchmarkMiddleware` in `ginprblm` with the same six cases plus a baseline, and the README "Performance → gin" subsection.
   - Hot paths measured: the gin `deferredWriter` and `finish`, and gin's discovery branch.
4. **fiber.**
   - At its end: `BenchmarkErrorHandler` in `fiberprblm` with the same cases plus a baseline, driven through `app.Handler()`. The README "Performance → fiber" subsection.
   - Hot paths measured: fiber's `respond` and `send`, and its discovery branch with the recorder.
5. **Profiles and profile-driven optimizations.**
   - At its end: CPU and memory profiles of each adapter's benchmarks are taken (*idle machine*). Every function at or above the thresholds in D6 is either optimized with a `benchstat`-verified improvement, or recorded as "examined, not changed" with the reason.
   - Hot paths: whichever the profiles name. Their test coverage is re-checked after each optimization.
6. **Publication and CI smoke.**
   - At its end: the README "Performance" section shows the final `benchstat` tables per adapter, BENCHMARKS.md documents the full protocol, and CI runs every benchmark once.

Slice 1 works on its own: one benchmark, one documented command, one table. No slice needs a later one to be useful.

## Decisions

### D1. Benchmarks live in the module they measure

`bench_test.go` goes in the core, in `ginprblm` and in `fiberprblm`, in the external `_test` package. The micro-benchmarks of unexported functions go in the core's internal test files, next to `BenchmarkIsSubtype`.

- *Alternative:* one `internal/benchtest` module that imports all three, like `internal/paritytest`. Rejected:
  - it would measure the adapters through `GOWORK` only, and not under `GOWORK=off` as released
  - it couples three modules' benchmarks into one `go.mod`
- **Failure modes:**
  - **Benchmarks drift between adapters:** each adapter's benchmark builds its handler from one shared case table, defined the same way in each module (same names, errors, options and Accept values). The README compares rows by name.
  - **`GOWORK=off`:** benchmarks use only their own module's dependencies, so they run in both modes.

### D2. Measure the library, not the harness

**Writers.** Each net/http and gin benchmark writes to a minimal reusable `discardWriter`:
- an `http.ResponseWriter` whose header map is cleared each iteration, and which records the status and counts bytes
- it is not `httptest.ResponseRecorder`, whose buffer growth and header cloning would show up as library allocations

**Requests.** Requests are built once, before `b.Loop()`. The context is never canceled, and the body is `http.NoBody`.

**fiber.** Each iteration drives `app.Handler()` with a `fasthttp.RequestCtx` initialized from a request built once and copied into the context (`req.CopyTo`), so no state leaks between iterations. This makes `github.com/valyala/fasthttp` a direct requirement of `fiberprblm`. It is already in that module's graph through fiber, so no new module is added, and the core is untouched. `app.Test` is rejected, because it goes over an in-memory network connection and measures that.

**Baselines.** Each adapter has a `baseline` case: a handler that writes the same status and a fixed JSON error body without prblm. "prblm overhead" is the difference from it.

- **Failure modes:**
  - **A benchmark measures a broken path** (for example a mapper that no longer matches, so every iteration is the 500 fallback). Before `b.Loop()`, each case serves one request and fails with `b.Fatalf` unless the status, Content-Type and a body marker are what the case expects.
  - **The compiler optimizes work away:** each iteration's observable result (status and byte count) is written to a package-level sink.
  - **Header maps grow across iterations:** the `discardWriter` clears its map each iteration. The gin engine and fiber app are built once and reused, which is how they run in production.
  - **fiber context reuse leaks state:** a fresh `RequestCtx` per iteration, initialized from the copied request. It is checked by the pre-loop assertion and by the same case run twice in the smoke test.

### D3. Cases, one table per module

| Case | Request | Expect |
|---|---|---|
| `baseline` | `GET /v1/vessels/7` | 404, fixed JSON body, no prblm |
| `mapped-json` | `GET /v1/vessels/7`, mapped `errVesselNotFound` | 404 `application/problem+json` |
| `fallback-500` | `GET /v1/vessels/7`, unmapped error | 500 `application/problem+json` |
| `xml` | `GET /v1/vessels/7`, `Accept: application/problem+xml` | 404 `application/problem+xml` |
| `resolved-type` | `GET /v1/contracts/7`, `BlacklistedSupplier.New()` with namespace `/v1/api/probs` | 409, resolved `type` |
| `discovery-200` | `GET /v1/api/probs/contracts/blacklisted-supplier`, `Accept: application/json` | 200 `application/json` |
| `discovery-304` | same, with the matching `If-None-Match` | 304 |

Every case runs with one enricher (`trace_id`) and an `OnError` hook that does nothing. Those are the shape of a real configuration, and they make the hook call part of the measured path.

- **Failure mode:** the cases diverge from the parity cases → the cases reuse the parity test's errors, options and paths, so a bug in one shows up in both.

### D4. Measurement protocol (idle machine)

The protocol is documented in BENCHMARKS.md and is the only way numbers are produced:
1. **Prepare the machine:**
   - idle: no builds, browsers or IDE indexing
   - on mains power, after 2 minutes of rest
   - record `go version`, `go env GOOS GOARCH`, the CPU model (`sysctl -n machdep.cpu.brand_string` on macOS, `lscpu` on Linux) and the commit SHA
2. **Run:** `go test -run '^$' -bench . -benchmem -count 10 -benchtime 1s` per module, with `GOWORK=off` for the published modules.
3. **Save the raw output** to `bench/results/<module>-<yyyy-mm-dd>-<short-sha>.txt` in a committed directory.
4. **Summarize** with `benchstat` (`go install golang.org/x/perf/cmd/benchstat@latest`). The README tables are `benchstat` output.

- *Alternatives:*
  - **Run in CI.** Rejected: noisy shared runners.
  - **Commit only the summary.** Rejected: the raw files are what `benchstat` comparisons need later.
- **Failure modes:**
  - **A noisy run:** `benchstat` reports the ± spread. A row above ±5% is re-run. If it stays above, the published table marks the variance.
  - **Thermal throttling** on a laptop: the 2-minute rest, `-count 10` and the variance check. The README states the machine, so readers can judge.
  - **Results from different machines get mixed:** every raw file name carries the date and SHA, every README table names its machine, and comparisons only use files from the same machine.

### D5. Profiles

CPU and memory profiles are taken per adapter (*idle machine*) with `-cpuprofile` and `-memprofile`, using the same `-bench` filter and `-benchtime 5s` for enough samples. They are read with `go tool pprof -top` and `-list`. Profiles are not committed (they are binary and machine-specific). The commands that produce them are documented.

- **Failure mode:** a profile is dominated by the framework (gin routing, fasthttp parsing) → only functions in this repository are candidates (D6). Framework costs are reported as context in the README, not optimized.

### D6. Optimization policy

**A candidate** is a function in this repository with either:
- at least 5% flat CPU in a request-path profile, or
- at least 10% of `alloc_space` in a memory profile.

**Each candidate:**
1. Is optimized in its own commit. A structural refactor that enables it is a separate, earlier commit, because the refactoring rules keep structural and behavioral commits apart.
2. Keeps every suite green: the core, `ginprblm`, `fiberprblm` and `internal/paritytest` with `-race`, and `GOWORK=off` for the published modules. The parity test proves the output bytes are unchanged.
3. Is compared with `benchstat` on the same machine, `-count 10`, old against new. It is kept only for a significant improvement (p < 0.05) in time/op or allocs/op on at least one request-path case, with no significant regression in any other case. Otherwise it is reverted, and recorded as "examined, not changed".
4. Gets its hot path's coverage re-checked with `go tool cover -func`.

The following are excluded:
- changes to public API, to defaults or to output bytes
- `unsafe`
- `sync.Pool` for objects that escape to consumer code (problems handed to hooks or enrichers), because a pooled object reused after a consumer kept it would be a correctness bug

- **Failure modes:**
  - **An "optimization" changes behavior:** caught by the suites and the parity test. Being behavior-preserving is the acceptance criterion.
  - **The improvement is noise:** caught by the `benchstat` significance rule.
  - **A pooled object leaks across requests:** excluded by policy, and a pool of internal-only objects still needs a `-race` test with concurrent requests.

### D7. CI smoke run

One step per module: `go test -run '^$' -bench . -benchtime 1x ./...`. It checks that benchmarks compile, that their pre-loop assertions pass and that they don't panic. It publishes no timing and gates nothing on speed.

- **Failure mode:** CI gets slower → 1x runs each benchmark once, which costs a few seconds per module.

## Defaults

Nothing configurable changes; see proposal.md, Defaults and Customization.

## Hot Paths

- **No new runtime code paths.** Optimizations change the implementation of existing hot paths:
  - resolution, finalization and enrichment
  - JSON and XML encoding
  - Accept negotiation
  - response writing in each adapter
  - type resolution and discovery serving

  Their existing tests and the parity test must stay green, and coverage of each touched function is re-checked (D6, step 4).
- **Benchmark helpers:** the `discardWriter`, the fasthttp request driving and the pre-loop assertions are test code. The CI smoke run exercises them.

## Risks / Trade-offs

- [Numbers depend on the machine and are read as absolute claims] → Each table names its machine, OS, Go version and SHA, and shows the baseline next to prblm. Readers compare overhead, not absolute times.
- [The idle-machine measurement waits on the maintainer] → Code tasks land independently, and the README shows the subsection with "results pending" until the measured run is committed.
- [Profiles point at framework code that can't be changed here] → The README reports it as context (D5), and nothing outside this repository is changed.
- [An optimization makes code harder to read for a small gain] → The D6 thresholds plus significance rule, and the refactoring rule that a speed gain does not excuse unreadable code. A gain below the threshold is reverted.
- [`fasthttp` becomes a direct requirement of `fiberprblm`] → It is already in the module graph through fiber, at the same version. Only the `go.mod` comment changes.

## Migration Plan

Nothing to migrate. Benchmarks are test-only. Optimizations are internal and behavior-preserving, and can be rolled back by reverting their single commit.
