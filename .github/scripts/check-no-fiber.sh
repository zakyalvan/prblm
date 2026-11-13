#!/bin/sh
# Fails when the module graph of any given module directory contains gofiber
# or fasthttp. Only fiberprblm (and the unpublished parity test module) may
# depend on fiber, so gin and net/http users never download it.
# GOWORK=off checks each module on its own: in workspace mode the graph would
# be the union of every workspace module, fiberprblm included.
# Usage: check-no-fiber.sh DIR...
set -eu
status=0
for dir in "$@"; do
	leaked=$(cd "$dir" && GOWORK=off go list -m all | grep -E 'gofiber|fasthttp' || true)
	if [ -n "$leaked" ]; then
		echo "module in $dir depends on fiber:" >&2
		echo "$leaked" >&2
		status=1
	else
		echo "module in $dir has no fiber dependency"
	fi
done
exit "$status"
