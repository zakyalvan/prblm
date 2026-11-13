# Releasing

The repository holds several Go modules. The root module, `github.com/kartaladev/prblm`, has no
third-party dependencies. Framework adapters and mapper plugins that need a dependency live in
their own modules in subdirectories, and depend on a released version of the root module.

## Tags

- The root module is tagged `vX.Y.Z`, for example `v0.1.0`.
- A module in a subdirectory is tagged `<dir>/vX.Y.Z`, for example `gin/v0.1.0`. Go resolves a
  module's versions from tags with its directory as the prefix.
- Tag the root module first. A module that depends on a new core feature is released only after
  the core release it needs, and its `go.mod` requires that version.

## Before tagging

1. Run the full check from the repository root:

   ```sh
   go test -race ./... && go vet ./... && golangci-lint run ./... \
     && GOWORK=off go test ./... && ./.github/scripts/check-core-deps.sh
   for target in windows/amd64 darwin/arm64 js/wasm wasip1/wasm plan9/amd64; do
     GOOS="${target%/*}" GOARCH="${target#*/}" go vet ./...
   done
   ```

   `GOWORK=off` matters: it proves each module builds against what its own `go.mod` requires,
   not against the workspace.
2. Move the `[Unreleased]` entries in `CHANGELOG.md` under a heading for the new version and date.
3. In a module other than the root, make sure `go.mod` requires a released root version and has
   no `replace` directive pointing into the repository.

## Tagging

```sh
git tag -a v0.1.0 -m "prblm v0.1.0"   # the root module
git push origin v0.1.0
```

For a module in a subdirectory, use the directory prefix: `git tag -a gin/v0.1.0 -m "gin v0.1.0"`.

## Checking the release

After the push, confirm that the Go toolchain resolves the version outside the workspace:

```sh
GOWORK=off go list -m github.com/kartaladev/prblm@v0.1.0
```

To rehearse this before pushing, point the module path at a local clone that carries the tag, and
use a throwaway module cache. Without one, the rehearsal's content would stay cached under the real
version and shadow the published release on this machine.

```sh
export GIT_CONFIG_GLOBAL=$(mktemp) GOMODCACHE=$(mktemp -d)
git config --file "$GIT_CONFIG_GLOBAL" protocol.file.allow always
git config --file "$GIT_CONFIG_GLOBAL" url."file://$PWD".insteadOf https://github.com/kartaladev/prblm
cd "$(mktemp -d)"   # outside the repository and its go.work
GOWORK=off GOPROXY=direct GONOSUMDB=github.com/kartaladev/prblm GOFLAGS='-mod=mod -modcacherw' \
  go list -m github.com/kartaladev/prblm@v0.1.0
```

Tags are immutable once published: the module proxy and checksum database keep the first content
they see. To fix a bad release, publish a new patch version, and retract the bad one in `go.mod`
if it must not be used.
