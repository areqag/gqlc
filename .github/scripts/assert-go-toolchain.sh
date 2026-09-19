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

# stdout ALONE, and the version token compared whole. Folded together, a cold
# module cache's stderr notice — `go: downloading go1.27.1 (linux/amd64)` —
# names the wanted version followed by a space, and a substring match over both
# streams is satisfied by that notice whatever the `go version` line says.
# Measured 2026-09-19 with go1.27.1 over an empty GOMODCACHE: the notice goes to
# stderr and `go version go1.27.1 linux/amd64` is the whole of stdout. The go
# command's stderr is left to pass through to the caller's log.
if ! reported="$("${go_cmd}" version)"; then
    echo "\`${go_cmd} version\` would not run, so there is nothing to hold against go${want}." >&2
    exit 1
fi

read -r cmd verb token _ <<<"${reported}"
if [ "${cmd} ${verb}" != "go version" ] || [ "${token}" != "go${want}" ]; then
    echo "go.mod names go${want}, but the provisioned toolchain reports: ${reported}" >&2
    exit 1
fi
printf '%s\n' "${reported}"
