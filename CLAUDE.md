# prblm

Go library (`github.com/kartaladev/prblm`, Go 1.27) that renders Go errors as RFC 9457 problem details for net/http, gin and fiber v3. Planning lives in `openspec/`.

## Project rules

These rules are mandatory for every task in this repository: planning, implementation and review.

### 1. Library: safe, opinionated defaults, optional flexibility

- Zero configuration must produce correct, safe behavior. A consumer who passes no options gets RFC 9457 compliant output that leaks nothing (no error text on 5xx, no stack traces, no database internals).
- Every default is a deliberate choice, documented in godoc with its reason.
- Customization is opt-in (functional options, interfaces with func adapters, plugin modules). It must never be required for correct behavior.
- An option can loosen a safety default (e.g. exposing database detail) only through an explicit, clearly named opt-in.
- Public API is a commitment: prefer adding small, composable options over widening existing signatures. Keep unexported anything that consumers don't need.

### 2. High-quality idiomatic Go

- Follow the Go skills below, starting from `golang-how-to` (see "Go development").
- Standard library first. A new dependency only goes in the module that needs it, never in the core.
- Run `gofmt`/`goimports`, `go vet` and `golangci-lint` clean before considering work done.

### 3. TDD: RED, GREEN, REFACTOR, repeat

For every behavior, in small steps:
1. **RED:** write a failing test first. Run it and confirm it fails for the expected reason (not a compile error elsewhere, not a wrong assertion).
2. **GREEN:** write the minimal code that makes it pass. Run the tests.
3. **REFACTOR:** with tests green, improve the design from the point of view of Martin Fowler's *Refactoring* (name the smell, apply a named refactoring in small behavior-preserving steps, run the tests after each step). Then run the `/simplify` skill on the changed code and apply what holds up.
4. **REPEAT** with the next behavior.

Never write production code without a failing test that demands it. Never refactor on red. Spec scenarios in `openspec/` map to tests.

### 4. Hot paths must be covered

There is no coverage percentage threshold. Every hot path must be covered by tests, and the coverage must be checked, not assumed (`go test -coverprofile=c.out ./... && go tool cover -func=c.out`).

Hot paths in this library:
- error resolution (self-describing, scoped, consumer, built-in, status hint, fallback)
- problem finalization and enrichment
- JSON and XML encoding
- `Accept` negotiation
- writing the response, in every adapter (net/http, gin, fiber v3)
- panic recovery
- every mapper plugin's match and no-match paths

### 5. Defensive programming (Murphy's Law)

Anything that can go wrong will go wrong. When planning, implementing and reviewing, enumerate what can fail and make sure every relevant scenario is handled and tested:
- nil and zero values (nil error, nil problem, nil mapper, nil options, typed-nil interfaces)
- malformed input (bad `Accept` headers, invalid extension names, invalid XML names, wrong-typed JSON)
- partial failures (encoding fails, response already started, client disconnected, context canceled)
- concurrency (shared package-level problems, concurrent requests, `-race` clean)
- panics inside consumer code (mappers, enrichers, hooks) and inside the library's own recovery path
- framework quirks (gin committing headers early, fiber errors, `http.ErrAbortHandler`)

Reviews must ask "what happens when this fails?" for every new branch, and reject untested failure paths.

### 6. Gall's Law: evolve from a simple system that works

A complex system that works evolved from a simple system that worked. Plans and reviews must reflect that:
- Plan in thin, end-to-end working slices. The first slice is the simplest system that works end to end (e.g. one error to a JSON problem through net/http), and each later slice adds one capability to a system that already works.
- Every slice ends in a working, tested, releasable state. No slice depends on code that only becomes useful in a later slice.
- Prefer deleting or postponing a feature to building it ahead of need.
- In review, reject designs that only work once several unfinished parts come together. If a slice can't be made to work, go back to the last working state and simplify.

### 7. Code navigation with gopls

Use the gopls MCP server (configured in `.mcp.json`) for Go code navigation instead of grep whenever the question is semantic:
- `go_workspace` once at session start, then `go_vulncheck` as a baseline
- `go_search` to locate symbols, `go_file_context` after reading a Go file for the first time, `go_package_api` for a dependency's or sibling module's API
- `go_symbol_references` before changing any definition
- `go_diagnostics` on every changed file after each edit
- `go_rename_symbol` for renames

### 8. Prove every defect claim with a failing test

A suspected error or defect is not reported until it has been reproduced. This applies to any claim that code, a dependency, or a framework behaves wrongly, whether it comes up in implementation, review, debugging or planning:
- Write a test that fails because of the defect, and run it. The failure output is the proof.
- Report the claim together with the test (name and location), the command that runs it, and the failing output.
- If the test passes, the suspicion is not confirmed. Drop the claim or say it's unconfirmed; never present it as a finding.
- If it can't be reproduced as an automated test (e.g. it needs external infrastructure), say so explicitly and label the claim unverified, with the reason.
- The reproducing test stays in the suite as the RED step of the fix (rule 3).

## Go development

Before any Go coding, review, debugging, troubleshooting, or setup task, load the `samber/cc-skills-golang@golang-how-to` skill first — it routes to whichever other Go skills the task needs.

## Required Go skills

The following Go skills from `samber/cc-skills-golang` MUST always be applied when working on this project. Load them at the start of every Go-related task, regardless of whether the user explicitly mentions them.

- `samber/cc-skills-golang@golang-code-style`
- `samber/cc-skills-golang@golang-data-structures`
- `samber/cc-skills-golang@golang-design-patterns`
- `samber/cc-skills-golang@golang-documentation`
- `samber/cc-skills-golang@golang-error-handling`
- `samber/cc-skills-golang@golang-modernize`
- `samber/cc-skills-golang@golang-naming`
- `samber/cc-skills-golang@golang-safety`
- `samber/cc-skills-golang@golang-security`
- `samber/cc-skills-golang@golang-testing`
- `samber/cc-skills-golang@golang-troubleshooting`
- `samber/cc-skills-golang@golang-refactoring`
- `samber/cc-skills-golang@golang-gopls`
- `samber/cc-skills-golang@golang-lint`
