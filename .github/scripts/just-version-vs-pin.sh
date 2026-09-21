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
#   exit 1   there is no pin to compare against; stderr says why, stdout empty
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
have="$(just --version | sed -n 's/^just //p')"
printf '%s\n%s\n' "$have" "$want"
[ "$have" = "$want" ] || exit 3
