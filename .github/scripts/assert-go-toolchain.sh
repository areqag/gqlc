#!/usr/bin/env bash
# Refuse unless `<go> version` reports exactly the toolchain go.mod names.
#
#   assert-go-toolchain.sh <version> [go-command]
#
# <version> carries no `go` prefix, the shape go-toolchain-version.sh prints.
# The caller sets GOTOOLCHAIN; this script reads whatever toolchain that yields.
# On a match it prints the `go version` line and exits 0. On a refusal it exits
# 1 with the reason as the last line of stderr and nothing on stdout.
#
# It is a script rather than lines inside .github/actions/setup-go because a
# composite action's `run:` block cannot be driven by a test, and the rows in
# assert-go-toolchain.rows are what hold this to its claim (bd gqlc-ma1l).
set -euo pipefail

want="${1:-}"
go_cmd="${2:-go}"
if [ -z "${want}" ]; then
    echo "usage: assert-go-toolchain.sh <version> [go-command]" >&2
    exit 2
fi

if ! resolved="$("${go_cmd}" version 2>&1)"; then
    echo "\`${go_cmd} version\` would not run, so there is nothing to hold against go${want}: ${resolved}" >&2
    exit 1
fi

case "$resolved" in
    *"go${want} "* | *"go${want}") ;;
    *)
        echo "go.mod names go${want}, but the provisioned toolchain reports: ${resolved}" >&2
        exit 1
        ;;
esac
printf '%s\n' "${resolved}"
