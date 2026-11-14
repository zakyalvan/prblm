# Design

## Context

Why this change exists is in proposal.md. The release procedure itself is in `RELEASING.md`; this design fixes the order and the checks for the first release.

When this change starts:
- `add-problem-details-core`, `add-framework-adapters`, `add-error-sentinels` and `add-mapper-plugins` are archived, and their code is on `main`.
- The published modules are the root module (core plus `mappers/sqlmap`), `ginprblm`, `fiberprblm`, `mappers/gormmap`, `mappers/pgxmap` and `mappers/validatormap`. Each sub-module has `replace github.com/kartaladev/prblm => ../..`.
- `internal/paritytest` and `internal/plugintest` are test-only modules in `go.work`. They are never tagged and keep their replaces.

## Goals / Non-Goals

**Goals:**
- `go get github.com/kartaladev/prblm@v0.1.0` and `go get github.com/kartaladev/prblm/<module>@v0.1.0` work from a clean module cache.
- Every published module builds and passes its tests with `GOWORK=off` against the real core tag.

**Non-Goals:**
- Any code, API or behavior change.
- Fixing issue #8 inside this change. If the owner decides it blocks `v0.1.0`, it gets its own change first.

## Slices

1. **The repository lives at `kartaladev/prblm`**: the owner transfers it, `origin` points at the new location, pushes work and CI runs there. Nothing is tagged yet.
2. **The core is released**: the CHANGELOG has a `[0.1.0]` section and `v0.1.0` resolves through the Go module proxy. net/http users can depend on it.
3. **The adapters and plugins are released**: the five sub-modules require `v0.1.0` with no replace, pass `GOWORK=off` against it, and are tagged on that commit.

Each slice leaves a working state: after slice 1 the code works exactly as before, after slice 2 the core is usable on its own, and slice 3 adds the remaining modules.

## Decisions

### D1. Release only after the transfer
- Tagging at `zakyalvan/prblm` would publish versions under a path that doesn't match the module path, and the proxy would cache them there for good.
- **Failure modes:**
  - **The transfer is delayed:** nothing is tagged; `main` keeps working with replaces.
  - **GitHub's redirect from the old URL is used by mistake:** `origin` is changed explicitly and checked with `git remote -v` and `gh repo view kartaladev/prblm`.

### D2. The core is tagged first, the sub-modules on the next commit
- A module can only require a core tag that already exists, so the commit that removes the replaces comes after `v0.1.0`, and all five sub-module tags go on that commit. No plugin requires another, so no sub-module tag waits for another.
- **Failure modes:**
  - **A replace left in a published `go.mod`:** consumers would see an unresolvable version. The check `grep -l replace` over the published `go.mod` files must find nothing before tagging.
  - **A tag pushed by mistake or on the wrong commit:** tags are permanent for the module proxy and checksum database. Each tag push waits for the owner's go-ahead. A broken version is retracted with a `retract` directive in the next patch release, never by moving the tag.
  - **The proxy hasn't seen the tag yet:** verify with `GOWORK=off go list -m <module>@v0.1.0`, with `GOPROXY` at its default, after the push.

### D3. Issue #8 is decided before tagging
- #8 changes what a default response can contain (an upstream problem's detail), which rule 1 of CLAUDE.md cares about. The owner decides whether `v0.1.0` waits for it. The decision is recorded in tasks.md.
- **Failure modes:**
  - **It ships unfixed:** it is already documented in the sentinels godoc and README, and a later fix may change behavior in a minor release while the version is below 1.0.

## Defaults

No default changes; the release publishes the archived changes' defaults.

## Hot Paths

No code changes. Every hot path is covered by the modules' existing tests, which must pass with `GOWORK=off` against the real tag (slice 3).

## Risks / Trade-offs

- [The transfer breaks CI secrets, branch protection or the PR links] → Check CI on the first push after the transfer, before any tag.
- [A consumer depends on a pseudo-version from before the transfer] → None can: the module path never resolved before the transfer.
- [The Go module proxy caches a bad version for good] → Owner go-ahead before every tag push; `retract` for recovery.
