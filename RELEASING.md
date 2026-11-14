# Releasing

The repository holds several Go modules. The root module, `github.com/kartaladev/prblm`, has no
third-party dependencies. Framework adapters and mapper plugins that need a dependency live in
their own modules in subdirectories, and depend on a released version of the root module.

## Tags

- The root module is tagged `vX.Y.Z`, for example `v0.1.0`.
- A module in a subdirectory is tagged `<dir>/vX.Y.Z`. Go resolves a module's versions from tags
  with its directory as the prefix:

  | Module | Directory | Tag |
  |---|---|---|
  | `github.com/kartaladev/prblm` | `.` | `vX.Y.Z` |
  | `github.com/kartaladev/prblm/ginprblm` | `ginprblm/` | `ginprblm/vX.Y.Z` |
  | `github.com/kartaladev/prblm/fiberprblm` | `fiberprblm/` | `fiberprblm/vX.Y.Z` |
  | `github.com/kartaladev/prblm/mappers/gormmap` | `mappers/gormmap/` | `mappers/gormmap/vX.Y.Z` |
  | `github.com/kartaladev/prblm/mappers/pgxmap` | `mappers/pgxmap/` | `mappers/pgxmap/vX.Y.Z` |
  | `github.com/kartaladev/prblm/mappers/validatormap` | `mappers/validatormap/` | `mappers/validatormap/vX.Y.Z` |

  `mappers/sqlmap` is a package of the root module, released with its tag.

- Tag the root module first. A module that depends on a new core feature is released only after
  the core release it needs, and its `go.mod` requires that version.
- Then tag the other modules. Each requires only the core, so they can be tagged together, on the
  same commit.
- `internal/paritytest` and `internal/plugintest` are unpublished test modules. They are never
  tagged, and they keep their `replace` directives so that they always test the adapters and the
  plugins in the repository. Tests that combine plugins (gorm over pgx, pgx over database/sql)
  belong in `internal/plugintest`, so that no published plugin requires another.

## Before tagging

1. Run the full check in every module, from the repository root:

   ```sh
   ./.github/scripts/check-core-deps.sh
   ./.github/scripts/check-no-fiber.sh . ginprblm mappers/gormmap mappers/pgxmap mappers/validatormap
   ./.github/scripts/check-test-only-deps.sh mappers/gormmap gorm.io/driver/sqlite github.com/mattn/go-sqlite3
   ./.github/scripts/check-test-only-deps.sh mappers/validatormap github.com/go-playground/locales/en \
     github.com/go-playground/locales/id
   for module in . ginprblm fiberprblm internal/paritytest internal/plugintest mappers/gormmap \
     mappers/pgxmap mappers/validatormap; do
     (cd "$module" && go test -race ./... && go vet ./... && golangci-lint run ./... \
       && GOWORK=off go test ./...) || break
   done
   for target in windows/amd64 darwin/arm64 js/wasm wasip1/wasm plan9/amd64; do
     GOOS="${target%/*}" GOARCH="${target#*/}" go vet ./...
   done
   ```

   `GOWORK=off` matters: it proves each module builds against what its own `go.mod` requires,
   not against the workspace.
2. Move the `[Unreleased]` entries in `CHANGELOG.md` under a heading for the new version and date.
3. In a module other than the root, make sure `go.mod` requires released versions and has no
   `replace` directive pointing into the repository. Until the first core release, `ginprblm` and
   `fiberprblm` replace the core with `../`, and the modules in `mappers/` with `../..`: remove
   that line and require the tagged core (`go get github.com/kartaladev/prblm@vX.Y.Z` with
   `GOWORK=off`), then run `GOWORK=off go mod tidy` and `GOWORK=off go test -race ./...` against
   the real tag before tagging them. Consumers ignore `replace` directives, so a published module
   that still has one cannot be built. Check with `grep -l '^replace' */go.mod mappers/*/go.mod`,
   which must list nothing; only the unpublished `internal/*/go.mod` keep their replaces.

## Tagging

```sh
git tag -a v0.1.0 -m "prblm v0.1.0"   # the root module
git push origin v0.1.0
```

For a module in a subdirectory, use the directory prefix:

```sh
git tag -a ginprblm/v0.1.0 -m "ginprblm v0.1.0"
git tag -a fiberprblm/v0.1.0 -m "fiberprblm v0.1.0"
git tag -a mappers/gormmap/v0.1.0 -m "gormmap v0.1.0"
git tag -a mappers/pgxmap/v0.1.0 -m "pgxmap v0.1.0"
git tag -a mappers/validatormap/v0.1.0 -m "validatormap v0.1.0"
git push origin ginprblm/v0.1.0 fiberprblm/v0.1.0 mappers/gormmap/v0.1.0 mappers/pgxmap/v0.1.0 \
  mappers/validatormap/v0.1.0
```

## Checking the release

After the push, confirm that the Go toolchain resolves the version outside the workspace:

```sh
GOWORK=off go list -m github.com/kartaladev/prblm@v0.1.0
GOWORK=off go list -m github.com/kartaladev/prblm/ginprblm@v0.1.0
GOWORK=off go list -m github.com/kartaladev/prblm/fiberprblm@v0.1.0
GOWORK=off go list -m github.com/kartaladev/prblm/mappers/gormmap@v0.1.0
GOWORK=off go list -m github.com/kartaladev/prblm/mappers/pgxmap@v0.1.0
GOWORK=off go list -m github.com/kartaladev/prblm/mappers/validatormap@v0.1.0
```

To rehearse this before pushing, point the module path at a local clone that carries the tag, and
use a throwaway module cache. Without one, the rehearsal's content would stay cached under the real
version and shadow the published release on this machine.

```sh
export GIT_CONFIG_GLOBAL=$(mktemp) GOMODCACHE=$(mktemp -d)
git config --file "$GIT_CONFIG_GLOBAL" protocol.file.allow always
git config --file "$GIT_CONFIG_GLOBAL" url."file://$PWD".insteadOf https://github.com/kartaladev/prblm
cd "$(mktemp -d)"   # outside the repository and its go.work
for module in github.com/kartaladev/prblm github.com/kartaladev/prblm/ginprblm \
  github.com/kartaladev/prblm/fiberprblm github.com/kartaladev/prblm/mappers/gormmap \
  github.com/kartaladev/prblm/mappers/pgxmap github.com/kartaladev/prblm/mappers/validatormap; do
  GOWORK=off GOPROXY=direct GONOSUMDB=github.com/kartaladev/prblm GOFLAGS='-mod=mod -modcacherw' \
    go list -m "$module@v0.1.0"
done
```

Tags are immutable once published: the module proxy and checksum database keep the first content
they see. To fix a bad release, publish a new patch version, and retract the bad one in `go.mod`
if it must not be used.
