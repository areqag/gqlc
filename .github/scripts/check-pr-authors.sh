#!/usr/bin/env bash
# The server-side half of the identity gate (bd gqlc-gy3q).
#
# Reads the commits a pull request proposes and refuses the PR when any of them
# carries an identity that cannot belong to a person, or a Co-Authored-By
# trailer attributing the work to Claude / Anthropic.
#
# Usage:  check-pr-authors.sh <commits.ndjson>
#
# The argument is one JSON object per line, exactly what
#   gh api --paginate "repos/$R/pulls/$N/commits" --jq '.[]'
# writes: --jq is applied per page, so pagination yields NDJSON rather than a
# nest of arrays. Only .sha, .commit.author.email, .commit.committer.email and
# .commit.message are read.
#
# WHY SERVER-SIDE AT ALL, given .githooks/commit-msg already applies these
# rules: that hook is advisory. `git commit --no-verify` skips it; a clone that
# never ran `just init` has no core.hooksPath and so never had it; and in this
# repo core.hooksPath has been observed rewritten to /dev/null in the SHARED
# config by worktree-isolated agent spawns, with no writer anyone can name and
# no tripwire (bd gqlc-r41) — during such a window every rule in .githooks is a
# no-op and nothing says so. The one gate that cannot be turned off from a
# developer's machine is the one that runs on GitHub's.
#
# WHY THE PR AND NOT master: this repo squash-merges. The squash commit is
# authored by whoever merged and committed by GitHub, so the identities the
# work was actually written under exist server-side only up to the merge. Look
# then or not at all.
#
# The rules are NOT restated here. They come from
# .githooks/implausible-identity.sh, the same file .githooks/commit-msg reads,
# because a second copy goes stale silently in the direction where master
# accepts what commit-msg refuses.
set -euo pipefail

usage() {
    echo "usage: $0 <commits.ndjson>" >&2
    exit 2
}

[ "$#" -eq 1 ] || usage
COMMITS="$1"

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# SC1091: shellcheck runs without -x here, so it cannot follow the source. A
# missing file fails loudly at run time under `set -e`.
# shellcheck source=../../.githooks/implausible-identity.sh disable=SC1091
. "$REPO_ROOT/.githooks/implausible-identity.sh"

if [ ! -f "$COMMITS" ]; then
    echo "error: no commit list at '$COMMITS' — the fetch that should have written it" >&2
    echo "       did not, so this gate has read nothing. Failing rather than passing." >&2
    exit 1
fi

offences=0
seen=0

# `|| [ -n "$line" ]`: a final line with no trailing newline leaves read at a
# non-zero status with the text already in $line. Without it the LAST commit in
# the list is silently skipped — and the last commit is the one a developer just
# wrote, i.e. exactly the one this gate exists to look at.
while IFS= read -r line || [ -n "$line" ]; do
    [ -n "${line//[[:space:]]/}" ] || continue
    if ! printf '%s' "$line" | jq -e 'type == "object"' >/dev/null 2>&1; then
        echo "error: commit list line $((seen + 1)) is not a JSON object; refusing to" >&2
        echo "       report a verdict over input this gate could not parse." >&2
        exit 1
    fi
    seen=$((seen + 1))

    sha="$(printf '%s' "$line" | jq -r '.sha // "(unknown sha)"')"
    short="${sha:0:8}"

    for role in author committer; do
        email="$(printf '%s' "$line" | jq -r --arg r "$role" '.commit[$r].email // ""')"
        if ! reason="$(implausible_email_reason "$email")"; then
            echo "$short: implausible $role identity <$email>" >&2
            echo "        Refused: $reason." >&2
            offences=$((offences + 1))
        fi
    done

    # The trailer half needs the message text, and messages are multi-line, so
    # it is read per commit rather than tabulated with the addresses above.
    msg="$(printf '%s' "$line" | jq -r '.commit.message // ""')"
    if ! trailer="$(printf '%s\n' "$msg" | ai_attribution_trailer)"; then
        echo "$short: Co-Authored-By trailer attributes this commit to Claude/Anthropic" >&2
        echo "        Offending line: $trailer" >&2
        offences=$((offences + 1))
    fi
done <"$COMMITS"

# A PR always proposes at least one commit. Zero here means the API call
# returned nothing, or returned something this loop skipped — either way the
# gate looked at no commits, and a gate that looked at nothing must not report
# a pass. This repo has shipped a detector that exited 0 on the condition it
# was written to catch; that shape is the reason for this arm.
if [ "$seen" -eq 0 ]; then
    echo "error: the commit list is empty, so this gate examined no commits at all." >&2
    echo "       A pull request has at least one commit; treat this as a broken fetch," >&2
    echo "       not as a clean bill of health." >&2
    exit 1
fi

if [ "$offences" -ne 0 ]; then
    cat >&2 <<ERRMSG

$offences implausible identity/attribution finding(s) across $seen commit(s).

These are the identities the PR proposes; GitHub's squash erases them at merge,
so this is the last point at which they can be refused. Rewrite the offending
commits — e.g.

    git rebase -i --exec 'git commit --amend --no-edit --reset-author' <base>

after fixing \`git config user.email\`, and force-push the branch.
ERRMSG
    exit 1
fi

echo "checked $seen commit(s): all author/committer identities plausible, no AI-attribution trailers"
