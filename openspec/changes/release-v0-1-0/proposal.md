# Proposal

## Why

The core, the gin and fiber v3 adapters and the mapper plugins are implemented, but nothing is published. The module path is `github.com/kartaladev/prblm`, while the repository still lives at `zakyalvan/prblm`, so the path doesn't resolve and no consumer can `go get` any module. Until a release, the adapter and plugin modules reach the core through `replace github.com/kartaladev/prblm => ../..`, which consumers ignore.

The release was the last group of `add-mapper-plugins` (tasks 7.2–7.5). It is split out into this change so the plugin work can merge first, the repository can be transferred to `kartaladev`, and the release can then be done in the new repository.

## What Changes

- The repository moves from `zakyalvan/prblm` to `kartaladev/prblm` (done by the repository owner), and `origin` points at the new location.
- The CHANGELOG `[Unreleased]` entries become `[0.1.0]`: the core (including the sentinel errors and `MergeInto`) and `mappers/sqlmap`, which is a package of the root module.
- The core is tagged `v0.1.0`.
- `ginprblm`, `fiberprblm`, `mappers/gormmap`, `mappers/pgxmap` and `mappers/validatormap` drop their core `replace`, require `github.com/kartaladev/prblm v0.1.0`, and are tagged `<module>/v0.1.0` on that commit.
- No code or behavior changes. `internal/paritytest` and `internal/plugintest` are unpublished and keep their replaces.

## Defaults and Customization

Unchanged: this change publishes what the archived changes specify. Every default they document ships as is, and nothing loosens a safety default.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

None. This change has no spec-level behavior, so its `.openspec.yaml` sets `skip_specs: true`.

## Scope by Gall's Law

The simplest release that works end to end is the core tag alone: once `github.com/kartaladev/prblm@v0.1.0` resolves, net/http users can use it. The adapter and plugin tags follow on the next commit, because a module can only require a core tag that already exists. Nothing else is in scope: no API change, no new module, and no fix for issue #8 (a `*Problem` wrapped anywhere in the chain answers with its own status and detail). Whether #8 must be fixed before `v0.1.0` is decided at the start of this change.

## Impact

- GitHub: the repository location, its CI and branch protection after the transfer.
- `CHANGELOG.md`, and the `go.mod` and `go.sum` of the five published sub-modules.
- Tags: `v0.1.0`, `ginprblm/v0.1.0`, `fiberprblm/v0.1.0`, `mappers/gormmap/v0.1.0`, `mappers/pgxmap/v0.1.0` and `mappers/validatormap/v0.1.0`. A pushed tag is permanent for the Go module proxy and checksum database, so every tag push needs the owner's go-ahead.
