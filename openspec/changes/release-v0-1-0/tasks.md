# Tasks

No task here changes code or behavior, so there is no RED/GREEN/REFACTOR cycle; each task ends with the check that proves it. Every tag push waits for the repository owner's go-ahead.

## 1. Slice: the repository lives at `kartaladev/prblm`

- [ ] 1.1 Confirm the starting point: `add-mapper-plugins` and `add-error-sentinels` are archived and merged to `main`; on `main`, run the full verification in every module of `go.work` (`go vet`, `golangci-lint run ./...`, `go test -race ./...`) and `GOWORK=off go test ./...` in the six published modules; verify everything passes
- [ ] 1.2 Decide with the owner whether issue #8 (a `*Problem` wrapped anywhere in the chain answers with its own status and detail) blocks `v0.1.0`, and record the decision here; if it blocks, stop this change until a separate change fixes it
- [ ] 1.3 Transfer `zakyalvan/prblm` to `kartaladev` (done by the repository owner) and point `origin` at `git@github.com:kartaladev/prblm.git`; verify `gh repo view kartaladev/prblm` succeeds, `git remote -v` shows the new URL, `git push` works, and CI passes on the first push in the new repository

## 2. Slice: the core is released

- [ ] 2.1 Move the CHANGELOG `[Unreleased]` entries to `[0.1.0]` (core including the sentinel errors and `MergeInto`, and `mappers/sqlmap`), commit, and with the owner's go-ahead tag the core `v0.1.0` and push the tag; verify `GOWORK=off go list -m github.com/kartaladev/prblm@v0.1.0` resolves with an empty module cache

## 3. Slice: the adapters and plugins are released

- [ ] 3.1 In `ginprblm`, `fiberprblm`, `mappers/gormmap`, `mappers/pgxmap` and `mappers/validatormap`, remove the core replace, require `v0.1.0` and run `go mod tidy`; verify `GOWORK=off go test -race ./...` passes in each against the real tag, `grep -l replace` finds no published `go.mod`, and CI is green
- [ ] 3.2 With the owner's go-ahead, tag `ginprblm/v0.1.0`, `fiberprblm/v0.1.0`, `mappers/gormmap/v0.1.0`, `mappers/pgxmap/v0.1.0` and `mappers/validatormap/v0.1.0` on that commit and push them; verify `GOWORK=off go list -m <module>@v0.1.0` resolves for each
- [ ] 3.3 Verify the release from a consumer's view: in an empty module outside the repository, `go get` each published module at `v0.1.0` and build a program that registers a sentinel, a gin and a fiber handler and each plugin's mapper; verify it builds and `go mod graph` shows no replace

## Workflow follow-up

- Archive this change once group 3 is done.
