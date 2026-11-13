#!/bin/sh
# Fails when the root module requires any module besides itself.
# The core must stay standard-library only (see CLAUDE.md, rule 2).
set -eu
module=$(GOWORK=off go list -m)
all=$(GOWORK=off go list -m all)
if [ "$all" != "$module" ]; then
	echo "core module has third-party requirements:" >&2
	echo "$all" | grep -v -x "$module" >&2
	exit 1
fi
echo "core module has no third-party requirements"
