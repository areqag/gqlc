#!/usr/bin/env bash
# The gate for CLAUDE.md's "## Scratch space" rule — bd gqlc-wm3x / GH #1309.
#
# THE RULE. Every scratch path is ALLOCATED, never spelled: `mktemp -d`, not a
# chosen name. A chosen name is not yours — sixteen seats doing the same kind of
# work pick the same obvious name, and the loser of the race reads the winner's
# bytes.
#
# WHY IT NEEDS A GATE RATHER THAN PROSE. Measured 2026-08-22 (bd gqlc-b8gd): two
# sessions ran the same mutation ritual over kingdom/bin/km — copy aside to a
# fixed path, mutate, copy back. The second write landed on the first and the
# restore carried one session's uncommitted work into the other's worktree. Both
# branches shared a base, so it applied CLEANLY, with no conflict to raise an
# alarm. It also falsified a mutation battery: a row read KILLED because the
# feature under test had been overwritten out of the tree. Nothing in the repo
# went red for any of that, and nothing would today: the rule was written into
# CLAUDE.md and into three seat souls, and a citizen who breaks it still gets a
# green suite. That is the hole this file closes.
#
# WHAT IT SWEEPS. Tracked shell, just and instruction-markdown sources, for a
# /tmp path with a FIXED name — no mktemp template, no shell expansion. The
# classes are enumerated below with a representative file each, because the
# interesting failure mode of a grep-based sweep is not a wrong regex, it is
# GREPPING NOTHING: an aggregate emptiness check over N classes fires only when
# every class is silent, so dropping two of three leaves it green. The positive
# controls below therefore run over a MINIATURE REPOSITORY with one planted
# violation per class, and assert every one of them is reported by path.
#
# WHAT IT DOES NOT SWEEP, deliberately: docs/specs/*.md. Those are historical
# design records containing already-executed procedures, eighteen matches across
# six files, none of them an instruction a citizen is told to follow today. They
# are worth converting, but as prose editing rather than as an allowlist of dead
# lines — bd filed, see the report on gqlc-wm3x.
#
# Run via: just test-hooks
set -u

# shellcheck source=../git-env-sandbox.sh disable=SC1091
source "$(cd "$(dirname "$0")/.." && pwd)/git-env-sandbox.sh"

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

pass=0
fail=0
ok() {
    pass=$((pass + 1))
    printf 'ok   - %s\n' "$1"
}
bad() {
    fail=$((fail + 1))
    printf 'FAIL - %s\n' "$1"
}

# The covered classes, as git pathspecs. git's wildmatch lets `*` cross `/`, so
# these are recursive.
CLASS_PATHSPECS=(
    '*.sh' '.githooks/*' 'kingdom/bin/*' '.github/scripts/*'
    'justfile' '*.just'
    'CLAUDE.md' 'kingdom/*.md' '.claude/*.md'
)

# The token: a /tmp path and everything up to the first character that cannot be
# part of one in shell or markdown. Assembled from two pieces so that this line
# does not itself read as a violation — the sweep runs over its own source, and
# a gate that has to allowlist itself is one exception away from allowlisting
# the thing it was built to catch.
TOKEN_RE="/tmp"'/[^[:space:]"'"'"'`)(,;]*'

# For the same reason, every /tmp path this file MENTIONS — allowlist tokens,
# fixture text — is composed from this rather than written out. The suite is
# tracked, so it is inside its own sweep; the first run after it was committed
# reported six violations in its own allowlist, which is the gate working.
T="/tmp"

# Prints `<path>:<line>:<token>` for every FIXED /tmp path under $1, which must
# be the root of a git working tree. Two shapes are not fixed and are skipped:
# an mktemp template (XXXXXX), and anything carrying a shell expansion.
raw_scan() {
    local root="$1" f hit tok
    while IFS= read -r -d '' f; do
        while IFS= read -r hit; do
            tok="${hit#*:}"
            case "$tok" in
                *XXXXXX* | *'$'*) continue ;;
            esac
            printf '%s:%s\n' "$f" "$hit"
        done < <(grep -noE "$TOKEN_RE" "$root/$f" 2>/dev/null)
    done < <(cd "$root" && git ls-files -z -- "${CLASS_PATHSPECS[@]}")
}

# THE ALLOWLIST. `<path>|<token>|<reason>`, exact path and exact token — never a
# glob, never a directory, so that a NEW fixed literal in an allowlisted file is
# still caught. Every entry is checked below for staleness: if the match it
# names is gone, the row goes red and the line must be deleted. An allowlist
# nobody prunes is a pattern carve-out with extra steps.
ALLOWED=(
    ".githooks/gate-pushed-commits|$T/m|Printed ADVICE, not a write by this repo: a remedy line telling a citizen how to re-screen one commit by hand. It is still the wrong thing to teach, and bd is filed to reword it; the file belongs to another lane, so it is allowlisted rather than edited here."
    ".githooks/tests/claude-pre-bash-test.sh|$T/x.patch|A command STRING fed to the hook under test as input. Nothing opens it; the row is about how claude-pre-bash classifies 'git am <file>'."
    ".githooks/tests/claude-pre-bash-test.sh|$T/r|Same shape: the text of a bd close command handed to the hook as a fixture, never executed."
    "CLAUDE.md|$T/km.orig|The rule's own prose, NAMING the anti-pattern and the incident that produced it. The three tokens in this section are the examples a citizen is told not to write."
    "CLAUDE.md|$T/probe.jsonl|As above — an example of a chosen name, inside the sentence forbidding chosen names."
    "CLAUDE.md|$T/verdict.md|As above — the third of the three example names in the sentence forbidding chosen names."
)

allowed() {
    local path="$1" tok="$2" entry
    for entry in "${ALLOWED[@]}"; do
        [ "$path" = "${entry%%|*}" ] || continue
        [ "$tok" = "$(printf '%s' "${entry#*|}" | cut -d'|' -f1)" ] && return 0
    done
    return 1
}

# --- A. the enumeration is not empty, per class ------------------------------
# Before any verdict about the tree, establish that the sweep is LOOKING. A
# clean result from a sweep that enumerated nothing is the failure this whole
# file is designed around.

mapfile -t COVERED < <(cd "$ROOT" && git ls-files -- "${CLASS_PATHSPECS[@]}")
if [ "${#COVERED[@]}" -gt 40 ]; then
    ok "coverage: the sweep enumerates ${#COVERED[@]} tracked files"
else
    bad "coverage: the sweep enumerated only ${#COVERED[@]} files — it is reporting on almost nothing"
fi

covers() {
    local want="$1" f
    for f in "${COVERED[@]}"; do [ "$f" = "$want" ] && return 0; done
    return 1
}

# One named representative per class. A pathspec that silently stops matching —
# a renamed directory, a `*` that no longer crosses `/` — is invisible to a
# count alone once the tree is big.
for rep in \
    .githooks/pre-push \
    .githooks/tests/km-test.sh \
    kingdom/bin/km \
    .github/scripts/check-pr-authors.sh \
    justfile \
    CLAUDE.md; do
    if covers "$rep"; then
        ok "coverage: $rep is swept"
    else
        bad "coverage: $rep is NOT swept — its class is unguarded"
    fi
done

# kingdom/*.md is the class the rule most needs, because the seat souls carry
# the rule in their own voice. Counted rather than named: souls come and go.
kmd=$(cd "$ROOT" && git ls-files -- 'kingdom/*.md' | wc -l)
if [ "$kmd" -gt 5 ]; then
    ok "coverage: $kmd kingdom markdown files are swept"
else
    bad "coverage: only $kmd kingdom markdown files matched — the souls are unguarded"
fi

# --- B. the tree itself is clean ---------------------------------------------

violations=""
while IFS= read -r hit; do
    [ -n "$hit" ] || continue
    hpath="${hit%%:*}"
    htok="${hit##*:}"
    allowed "$hpath" "$htok" && continue
    violations+="$hit"$'\n'
done < <(raw_scan "$ROOT")

if [ -z "$violations" ]; then
    ok "the tracked tree contains no fixed /tmp scratch path"
else
    bad "fixed /tmp scratch paths in tracked sources — allocate them instead:
$(printf '%s' "$violations" | sed 's/^/       /')
       Use  scratch=\$(mktemp -d)  or  f=\$(mktemp), and
       trap 'rm -rf \"\$scratch\"' EXIT. A chosen name is not yours: two
       sessions picked the same one and one overwrote the other's uncommitted
       work with no conflict to warn either (bd gqlc-b8gd).
       If the match is genuinely not a scratch path, add it to ALLOWED in
       $(basename "$0") with a reason."
fi

# --- C. POSITIVE CONTROLS, one per class, over a miniature repository --------
# The sweep is driven end to end — git ls-files and all — over a tree shaped
# like this one, with a planted violation at a path of each covered class. This
# is what makes the clean verdict above worth anything: it establishes that a
# violation in each class WOULD have been reported, individually, so a class
# that silently stopped being enumerated cannot hide behind the others.
#
# The literals are assembled at runtime rather than written out, so that this
# file stays clean under its own sweep without an allowlist entry.
MINI="$TMP/mini"
mkdir -p "$MINI/.githooks/tests" "$MINI/kingdom/bin" "$MINI/kingdom/brain" \
    "$MINI/.github/scripts" "$MINI/.claude/skills/x"
git init -q -b master "$MINI"

PLANTED=(
    ".githooks/pre-commit"
    ".githooks/tests/thing-test.sh"
    "kingdom/bin/km"
    ".github/scripts/check-thing.sh"
    "lib/helper.sh"
    "justfile"
    "CLAUDE.md"
    "kingdom/brain/soul.md"
    ".claude/skills/x/SKILL.md"
)
for p in "${PLANTED[@]}"; do
    mkdir -p "$MINI/$(dirname "$p")"
    printf 'cp thing %s/collision-fixture.orig\n' "$T" >"$MINI/$p"
done

# Negative controls, in the same tree so a sweep that reported everything would
# also fail: an allocated path, an expansion, and a bare /tmp.
# The single quotes are the point: this is fixture TEXT to be swept, not code to
# be run, so the expansions must reach the file unexpanded.
# shellcheck disable=SC2016
printf 'd=$(mktemp -d %s/gqlc-XXXXXX)\nrm -rf "$d/inner"\ndf -h %s\n' "$T" "$T" \
    >"$MINI/.githooks/allocates.sh"
git -C "$MINI" add -A
git -C "$MINI" -c user.email=t@t.invalid -c user.name=t -c commit.gpgsign=false \
    commit -q -m fixture

mapfile -t MINI_HITS < <(raw_scan "$MINI")
mini_paths=" $(printf '%s\n' "${MINI_HITS[@]}" | cut -d: -f1 | tr '\n' ' ')"

for p in "${PLANTED[@]}"; do
    case "$mini_paths" in
        *" $p "*) ok "positive control: a fixed literal in $p is reported" ;;
        *) bad "positive control: a fixed literal in $p was NOT reported — that class is swept by nothing" ;;
    esac
done

case "$mini_paths" in
    *" .githooks/allocates.sh "*)
        bad "negative control: an mktemp template / \$-expansion / bare /tmp was reported as a violation — the sweep would redden correct code"
        ;;
    *) ok "negative control: mktemp templates, expansions and a bare /tmp are not reported" ;;
esac

# --- D. the allowlist is exact, and is not allowed to rot --------------------
# First that it discriminates at all. An allowlist keyed on the FILE rather than
# on the file-and-token would exempt CLAUDE.md forever, and the next citizen to
# write a real fixed path into it — the most-read instruction file in the repo —
# would be exempted by an entry written about a different line.

if allowed "CLAUDE.md" "$T/km.orig"; then
    ok "allowlist: the token it names is exempt"
else
    bad "allowlist: it does not even exempt the token it names"
fi
if allowed "CLAUDE.md" "$T/some-other-name"; then
    bad "allowlist: a DIFFERENT token in an allowlisted file is exempt too — the entry is exempting the file"
else
    ok "allowlist: a different token in the same file is still a violation"
fi


mapfile -t ROOT_HITS < <(raw_scan "$ROOT")
for entry in "${ALLOWED[@]}"; do
    apath="${entry%%|*}"
    rest="${entry#*|}"
    atok="${rest%%|*}"
    areason="${rest#*|}"

    case "$apath$atok" in
        *'*'* | *'?'*)
            bad "allowlist: '$apath|$atok' contains a glob — entries must be exact, or a new violation in the same file walks through"
            continue
            ;;
    esac
    if [ ${#areason} -lt 40 ]; then
        bad "allowlist: '$apath|$atok' has no real reason attached"
        continue
    fi

    matched=no
    for hit in "${ROOT_HITS[@]}"; do
        [ "${hit%%:*}" = "$apath" ] && [ "${hit##*:}" = "$atok" ] && matched=yes && break
    done
    if [ "$matched" = yes ]; then
        ok "allowlist: '$apath|$atok' still names a real match"
    else
        bad "allowlist: '$apath|$atok' matches nothing any more — DELETE that line from ALLOWED in $(basename "$0"). A stale exception is a hole waiting for a file to be reused."
    fi
done

printf -- '---\n%d passed, %d failed\n' "$pass" "$fail"
[ "$fail" -eq 0 ]
