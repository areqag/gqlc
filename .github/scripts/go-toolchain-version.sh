#!/usr/bin/env bash
# Print the Go version go.mod names, without the `go` prefix (e.g. `1.26.6`).
#
# ONE derivation, read by two callers that must not disagree:
# .github/actions/setup-go exports GOTOOLCHAIN=go<version> into the CI job env,
# and `just vuln` exports the same locally. When only CI pinned, `just vuln` was
# red on a clean master for every seat on a box whose default Go is a
# distribution build — govulncheck cannot place a stdlib version on one, so it
# looks every stdlib advisory up under an empty version and the gate goes green
# over the largest attack surface in the binary (bd gqlc-u91z, bd gqlc-irvs).
#
# A second copy of these four lines would be worse than the asymmetry it fixed:
# go.mod gaining a `toolchain` directive would move CI's pin and leave a
# `go`-only reader behind, and the two would then scan under different
# toolchains with nothing red to say so.
#
# `toolchain` wins over `go` when both are present, which is the go command's
# own precedence. Read from the file rather than restated as a literal by either
# caller: a literal is a second place to bump, and a stale one provisions a
# toolchain the build then switches away from, behind a green step.
set -euo pipefail

gomod="${1:-go.mod}"

version="$(awk '$1 == "toolchain" { sub(/^go/, "", $2); print $2; exit }' "${gomod}")"
if [ -z "${version}" ]; then
    version="$(awk '$1 == "go" { print $2; exit }' "${gomod}")"
fi
if [ -z "${version}" ]; then
    echo "error: ${gomod} carries neither a 'toolchain' nor a 'go' directive, so there is" >&2
    echo "       no version to pin. Callers of this script pin GOTOOLCHAIN from it, and a" >&2
    echo "       silent empty answer pins nothing at all (bd gqlc-irvs)." >&2
    exit 1
fi

printf '%s\n' "${version}"
