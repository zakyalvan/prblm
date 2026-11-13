#!/bin/sh
# Fails when the non-test packages of a module import a package that must
# only be used by its tests, such as a database driver a plugin is tested
# against. Such a module stays a requirement in go.mod, but consumers must
# never compile it into their programs.
# Usage: check-test-only-deps.sh DIR IMPORT-PATH-PREFIX...
set -eu
dir=$1
shift
deps=$(cd "$dir" && GOWORK=off go list -deps ./...)
status=0
for prefix in "$@"; do
	leaked=$(echo "$deps" | grep -E "^${prefix}(/|$)" || true)
	if [ -n "$leaked" ]; then
		echo "non-test code in $dir imports $prefix:" >&2
		echo "$leaked" >&2
		status=1
	else
		echo "non-test code in $dir does not import $prefix"
	fi
done
exit "$status"
