#!/usr/bin/env bash
# The ONE derivation of "is the just on PATH the just CI installs" (bd
# gqlc-i2lw). Two callers read it and do opposite things with a mismatch: `just
# check-just-version` refuses with the install remedy, and the `gates` recipe
# skips its justfile-format arm, visibly. A comparison spelled once in each
# could disagree with itself, and the disagreement would be a `gates` that
# skips on a host check-just-version calls pinned, or grades on one it refuses.
#
# $1 is the pin file (.github/actions/setup-just/just-version in this tree).
#
# It REPORTS and never advises — what a mismatch means is the caller's:
#
#   exit 0   match.     stdout: the PATH version, then the pin, one per line
#   exit 3   mismatch.  stdout: the same two lines
#   exit 1   one side could not be read — no pin, or a just that does not state
#            a version; stderr says why, stdout empty
#
# Both lines are printed on a mismatch because both callers name both versions,
# and a caller that re-read either one would be the second derivation this file
# exists to remove. 3 rather than 1 so that "differs" cannot be mistaken for
# "could not tell" by a caller that tests only for non-zero.
#
# `just` is resolved on PATH, not taken from the just that is running the
# calling recipe: PATH's is the one every `gates` arm invokes.
set -euo pipefail

pin_file="${1:?usage: just-version-vs-pin.sh <pin-file>}"
if [ ! -f "$pin_file" ]; then
    echo "error: $pin_file is absent, so there is no pin to check against." >&2
    exit 1
fi
want="$(tr -d '[:space:]' < "$pin_file")"
if ! printf '%s' "$want" | command grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+$'; then
    echo "error: $pin_file does not hold a just version (got '$want')." >&2
    exit 1
fi
# Asked in its own statement and not at the head of a pipeline: under `set -e`
# with pipefail a `just --version` that exits 3 made this script exit 3 with
# nothing printed, which both callers read as a mismatch between two empty
# versions (bd gqlc-07di).
reported=0
said="$(just --version)" || reported=$?
if [ "$reported" -ne 0 ]; then
    echo "error: \`just --version\` exited $reported, so the just on PATH states no version to check against $pin_file." >&2
    exit 1
fi
# Held to a version at the FRONT and compared whole below: `1.55.1 (abc 2026)`
# states a version that is not the pin, which is a mismatch and the caller's to
# answer; an empty or wordless answer states none, and is not.
have="$(sed -n '1s/^just //p' <<<"$said")"
if ! printf '%s' "$have" | command grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+'; then
    echo "error: \`just --version\` does not state a just version (got '$(sed -n 1p <<<"$said")')." >&2
    exit 1
fi
printf '%s\n%s\n' "$have" "$want"
[ "$have" = "$want" ] || exit 3
