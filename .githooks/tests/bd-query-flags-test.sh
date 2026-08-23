#!/usr/bin/env bash
# The gate for docs/bd-ledger-queries.md — bd gqlc-qh9z, follow-up to gqlc-18br.
#
# THE RULE. Every SCRIPTED `bd list` / `bd ready` call site in this repository
# states its row cap explicitly, and every scripted `bd list` states its status
# set explicitly. Neither default may be relied on.
#
# WHY BOTH, AND WHY SEPARATELY. `bd` is external (/usr/bin/bd, source not in
# this repo, unpatchable here) and applies TWO independent defaults: a status
# filter and a row cap. `-n 0` is the flag people reach for when they mean
# "everything"; it lifts the cap and leaves the status filter in place, silently.
# Measured 2026-08-23 against the deployed binary: `bd list -n 0 --json` returned
# 261 beads and `bd list --all -n 0 --json` returned 775, with nothing on stdout
# or stderr about the 514 that were dropped. Absent and closed call for OPPOSITE
# repairs — an absent review bead must be filed, a closed one must be reopened —
# so the omission does not merely hide rows, it flips a diagnosis. It has already
# cost this repo a duplicate bead and a board understated roughly six-fold.
#
# The cap IS disclosed, but on stderr, and every call site here redirects
# `2>/dev/null` because bd writes ordinary chatter there too. The status filter
# has no notice at any verbosity.
#
# THE RULE IS 'STATED', NOT 'ALL'. `bd list --status in_progress -n 0 --json` is
# correct: a single status is a legitimate explicit choice, and at three km call
# sites it is the whole point of the query. What this gate refuses is a call site
# that says nothing and takes what it is given. Whether a stated status is the
# RIGHT one for its purpose is a semantic question no sweep can answer — that is
# what bd gqlc-c7b5 is, a call site that states `--status open` (so it passes
# here, correctly) while meaning "every unfinished bead".
#
# `bd ready` needs a cap but no status: it is open-only by construction, so there
# is no default to state. Asserted below in both directions.
#
# WHAT IT SWEEPS: tracked shell, just and Go sources. NOT markdown — the
# instruction files and docs/bd-ledger-queries.md itself quote the wrong form on
# purpose, as the counterexample they are teaching against, and a sweep that
# reddened its own documentation would be deleted within the week.
#
# THE FAILURE MODE THIS FILE IS BUILT AROUND is not a wrong rule, it is a sweep
# that MATCHES NOTHING: a call-site population that quietly goes to zero passes
# every call site. So section A pins that the live tree's real invocations are
# each found by path and line, and section C drives the whole scanner over a
# miniature repository with a planted violation of each shape.
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

# The swept classes, as git pathspecs. git's wildmatch lets `*` cross `/`, so
# these are recursive.
CLASS_PATHSPECS=(
    '*.sh' '.githooks/*' 'kingdom/bin/*' '.github/scripts/*'
    'justfile' '*.just'
    '*.go'
)

# --- the scanner --------------------------------------------------------------
# Two invocation shapes, because this repo has both.
#
# SHELL: `bd list ...` at a command position — start of line, or after one of
# the characters that opens one. Prose is excluded in two passes before the
# match: trailing `#` comments are cut, then every quoted span is blanked. Both
# are needed and neither subsumes the other. Without comment-cutting, the ~20
# lines of km and bd-gh-sync commentary that name `bd list` while explaining it
# would all read as call sites. Without quote-blanking, kingdom/bin/km's
# BEADS: UNAVAILABLE diagnostic — which spells `(bd list --status in_progress,`
# inside an echo, with a `(` in front of it and no cap flag after it — reads as a
# capless call site. That line is the reason this is a two-pass strip and not a
# regex.
#
# GO: the exec-args shape — two adjacent quoted arguments — on one line. A call
# whose args are split across lines is reported rather than skipped; a gate that
# guesses in the permissive direction is the one that goes quietly empty.
#
# Restricted to *.go, with `//` comments cut, for a reason this file measured on
# itself. The Go arm originally ran over every enumerated file and cut no
# comments, so the first push after this suite became TRACKED went red on two
# lines of the suite itself: the header prose describing the Go shape, and the
# fixture literal it plants to exercise the Go shape. A gate that fails on its
# own account of itself is one exception away from being allowlisted into
# uselessness, so the repair was to narrow the arm, not to exempt the file.
scan() {
    local root="$1"
    (
        cd "$root" || exit 1
        git ls-files -z -- "${CLASS_PATHSPECS[@]}" | while IFS= read -r -d '' f; do
            [ -f "$f" ] || continue
            sed -e 's/[[:space:]]#.*$//' -e 's/^#.*$//' \
                -e 's/"[^"]*"/""/g' -e "s/'[^']*'/''/g" "$f" |
                grep -nE '(^|[|(&;{}!]|\$\()[[:space:]]*bd[[:space:]]+(list|ready)([[:space:]]|$)' |
                sed "s|^|$f:|"
            case "$f" in
                *.go)
                    sed -e 's|//.*$||' "$f" |
                        grep -nE '"bd",[[:space:]]*"(list|ready)"' |
                        sed "s|^|GO:$f:|"
                    ;;
            esac
        done
    )
}

# verdict <line-of-code> -> prints `ok`, or a reason.
# Kept separate from the enumeration so both can be exercised directly.
verdict() {
    local code="$1"
    local has_cap=no has_status=no is_ready=no
    # `bd ready` in either shape.
    if printf '%s' "$code" | grep -qE '(bd[[:space:]]+ready([[:space:]]|$)|"bd",[[:space:]]*"ready")'; then
        is_ready=yes
    fi
    # Row cap: -n N, -nN, --limit N, --limit=N, in shell or Go-arg spelling.
    if printf '%s' "$code" | grep -qE '(^|[[:space:]"])(-n([[:space:]]+|=)?[0-9]+|-n["][[:space:]]*,[[:space:]]*["][0-9]+|--limit([[:space:]]+|=)?[0-9]*|"--limit")'; then
        has_cap=yes
    fi
    # Status set: --all, --status <s>, --status=<s>, or the Go-arg spellings.
    if printf '%s' "$code" | grep -qE '(^|[[:space:]"])(--all|--status([[:space:]]+|=)?[a-z_]*|"--all"|"--status")'; then
        has_status=yes
    fi
    if [ "$has_cap" = no ]; then
        printf 'no explicit row cap (-n / --limit)'
        return
    fi
    if [ "$is_ready" = no ] && [ "$has_status" = no ]; then
        printf 'no explicit status set (--all / --status)'
        return
    fi
    printf 'ok'
}

# Every offending site under $1, as `<path>:<line>: <reason>`.
offenders() {
    local root="$1" hit path lineno code v
    while IFS= read -r hit; do
        [ -n "$hit" ] || continue
        case "$hit" in
            GO:*) hit="${hit#GO:}" ;;
        esac
        path="${hit%%:*}"
        rest="${hit#*:}"
        lineno="${rest%%:*}"
        code="${rest#*:}"
        v="$(verdict "$code")"
        [ "$v" = ok ] && continue
        printf '%s:%s: %s\n' "$path" "$lineno" "$v"
    done < <(scan "$root")
}

# --- A. the scanner finds the call sites that are actually there --------------
# Before any verdict about the tree, establish that the sweep is LOOKING, and at
# what. A clean verdict from a scanner that enumerated nothing is the failure
# this whole file is designed around, and it is the failure mode the bead names:
# "a sweep that matches nothing passes everything".

mapfile -t SITES < <(scan "$ROOT")
if [ "${#SITES[@]}" -ge 8 ]; then
    ok "enumeration: the scanner finds ${#SITES[@]} scripted bd list/ready call sites"
else
    bad "enumeration: the scanner found only ${#SITES[@]} call sites — the audit at docs/bd-ledger-queries.md counted nine on 2026-08-23. Either they moved, or the scanner stopped matching and is now passing the whole repo."
fi

site_in() {
    local want="$1" s
    for s in "${SITES[@]}"; do
        case "$s" in *"$want"*) return 0 ;; esac
    done
    return 1
}

# One named representative per shape and per class. A count alone cannot see a
# pathspec that silently stopped matching once the tree is big.
for rep in \
    ".githooks/bd-gh-sync" \
    "kingdom/bin/km" \
    "internal/tools/ghorphan/main.go"; do
    if site_in "$rep"; then
        ok "enumeration: $rep's call sites are swept"
    else
        bad "enumeration: no call site found in $rep — its class or its invocation shape is unguarded"
    fi
done

# --- B. the tree itself is clean ----------------------------------------------

mapfile -t OFFENDERS < <(offenders "$ROOT")
if [ "${#OFFENDERS[@]}" -eq 0 ]; then
    ok "every scripted bd list/ready call site states its status set and row cap"
else
    bad "scripted bd list/ready call sites relying on a default:
$(printf '       %s\n' "${OFFENDERS[@]}")
       bd applies TWO independent defaults, a status filter and a row cap, and
       -n 0 disables only the second — silently, with nothing on stderr the call
       site does not already redirect away. Write the cap as -n 0 / --limit 0
       and the status as --all or --status <s>. See docs/bd-ledger-queries.md.
       \`bd ready\` needs the cap only; it is open-only by construction."
fi

# --- C. POSITIVE AND NEGATIVE CONTROLS, over a miniature repository -----------
# The scanner is driven end to end — git ls-files and all — over a tree shaped
# like this one. This is what makes the clean verdict above worth anything: it
# establishes that each violating shape WOULD have been reported, individually,
# and that each correct shape would not.

MINI="$TMP/mini"
mkdir -p "$MINI"
git init -q -b master "$MINI"

plant() {
    mkdir -p "$MINI/$(dirname "$1")"
    printf '%s\n' "$2" >"$MINI/$1"
}

# VIOLATIONS — one per shape the rule refuses.
plant ".githooks/no-flags-at-all" 'bd list --json | jq length'
# shellcheck disable=SC2016  # fixture TEXT to be swept, not code to be run
plant "kingdom/bin/cap-only" 'ids=$(bd list -n 0 --json 2>/dev/null)'
# shellcheck disable=SC2016  # fixture TEXT to be swept, not code to be run
plant ".github/scripts/status-only.sh" 'x=$(bd list --status all --json)'
# shellcheck disable=SC2016  # fixture TEXT to be swept, not code to be run
plant "lib/ready-uncapped.sh" 'r=$(bd ready --json 2>/dev/null)'
plant "internal/tools/x/main.go" '	out, err := run3(ctx, "bd", "list", "--status", "all", "--json")'
VIOLATING=(
    ".githooks/no-flags-at-all"
    "kingdom/bin/cap-only"
    ".github/scripts/status-only.sh"
    "lib/ready-uncapped.sh"
    "internal/tools/x/main.go"
)

# CORRECT SITES — one per accepted shape. Every one of these is a real spelling
# taken from the audit table, plus the equals-sign and short-flag variants.
# shellcheck disable=SC2016  # fixture TEXT to be swept, not code to be run
plant ".githooks/ok-all-limit" 'bd list --status all --limit 0 --json >"$f" 2>/dev/null'
plant "kingdom/bin/ok-single-status" 'bd list --status in_progress -n 0 --json 2>/dev/null | jq .'
# shellcheck disable=SC2016  # fixture TEXT to be swept, not code to be run
plant "kingdom/bin/ok-ready-capped" 'ready=$(bd ready -n 0 --json 2>/dev/null)'
plant "lib/ok-equals.sh" 'bd list --status=all --limit=0 --json'
plant "lib/ok-dash-all.sh" 'bd list --all -n 0 --json'
plant "internal/tools/y/main.go" '	out, err := run3(ctx, "bd", "list", "--status", "all", "--limit", "0", "--json")'
CORRECT=(
    ".githooks/ok-all-limit"
    "kingdom/bin/ok-single-status"
    "kingdom/bin/ok-ready-capped"
    "lib/ok-equals.sh"
    "lib/ok-dash-all.sh"
    "internal/tools/y/main.go"
)

# NOT CALL SITES — prose that names the command. Both strip passes are exercised
# here, on the two shapes measured in the live tree.
# The parenthesis is the point, and it is why this fixture is not the obvious
# `# \`bd list\` returns a snapshot`: a backticked mention is already refused by
# the command-position test, so it would pass with the comment strip deleted and
# certify nothing. This shape reaches `bd list` through an opening paren, so the
# comment strip is the only thing standing between it and a reported violation.
plant ".githooks/prose-comment" '# The count is read off a snapshot (bd list --json), so it can be stale.'
plant "kingdom/bin/prose-echo" 'echo "BEADS: UNAVAILABLE — the query (bd list --status in_progress, or the jq over it) did not answer"'
# The Go arm's own prose control, and it is here because the arm did not have
# one and was wrong. It matched raw bytes in every enumerated file, so a `//`
# comment in a Go source — and the same text in a shell file — read as a call
# site. That is not hypothetical: it reddened this suite's first push, on this
# suite's own header. Two fixtures, because the arm needed narrowing in two
# independent ways: the comment cut, and the *.go path restriction.
# shellcheck disable=SC2016  # fixture TEXT to be swept, not code to be run
plant "internal/tools/z/main.go" '// The set is read off `bd list` — run3(ctx, "bd", "list", "--json") — once per run.'
# Double-quoted here on purpose, so the inner quotes reach the fixture file
# UNESCAPED. Written with backslashes it would carry `\"bd\",` and match the Go
# pattern nowhere, and the row below would then pass with the *.go restriction
# deleted — a control that certifies nothing.
plant "lib/go-shape-in-shell.sh" "# the Go call site spells it run3(ctx, \"bd\", \"list\", \"--json\")"
plant "lib/prose-quoted.sh" "echo \"bd-gh-sync: 'bd list' exited \${rc}\" >&2"
NOT_SITES=(
    ".githooks/prose-comment"
    "kingdom/bin/prose-echo"
    "lib/prose-quoted.sh"
    "internal/tools/z/main.go"
    "lib/go-shape-in-shell.sh"
)

git -C "$MINI" add -A
git -C "$MINI" -c user.email=t@t.invalid -c user.name=t -c commit.gpgsign=false \
    commit -q -m fixture

mapfile -t MINI_OFF < <(offenders "$MINI")
mini_off_paths=" $(printf '%s\n' "${MINI_OFF[@]}" | cut -d: -f1 | tr '\n' ' ')"
mapfile -t MINI_SITES < <(scan "$MINI")
mini_site_paths=" $(printf '%s\n' "${MINI_SITES[@]}" | sed 's/^GO://' | cut -d: -f1 | tr '\n' ' ')"

for p in "${VIOLATING[@]}"; do
    case "$mini_off_paths" in
        *" $p "*) ok "positive control: the violation in $p is reported" ;;
        *) bad "positive control: the violation in $p was NOT reported — that shape passes the gate" ;;
    esac
done

for p in "${CORRECT[@]}"; do
    case "$mini_site_paths" in
        *" $p "*) : ;;
        *) bad "control setup: the correct call site in $p was not even enumerated, so the row below proves nothing" ;;
    esac
    case "$mini_off_paths" in
        *" $p "*) bad "negative control: the CORRECT call site in $p was reported — the gate would redden compliant code" ;;
        *) ok "negative control: the correct call site in $p passes" ;;
    esac
done

for p in "${NOT_SITES[@]}"; do
    case "$mini_site_paths" in
        *" $p "*) bad "prose control: $p merely NAMES bd list in a comment or a string and was enumerated as a call site" ;;
        *) ok "prose control: $p names bd list without being read as a call site" ;;
    esac
done

# --- D. the verdict function itself discriminates -----------------------------
# Section C exercises the scanner end to end but reads only paths. These rows
# read the REASON, so a gate that reports every violation with the wrong
# diagnosis — telling a citizen to add a cap when what is missing is the status —
# is caught. A wrong reason on a real refusal is what sends the next reader to
# the wrong line.

vcheck() {
    local name="$1" code="$2" want="$3" got
    got="$(verdict "$code")"
    case "$got" in
        *"$want"*) ok "$name" ;;
        *) bad "$name (wanted a verdict matching '$want', got '$got')" ;;
    esac
}

vcheck "verdict: a bare 'bd list --json' is missing its cap" \
    'bd list --json' 'no explicit row cap'
vcheck "verdict: 'bd list -n 0 --json' is missing its status set" \
    'bd list -n 0 --json' 'no explicit status set'
vcheck "verdict: 'bd list --all -n 0 --json' is accepted" \
    'bd list --all -n 0 --json' 'ok'
vcheck "verdict: 'bd ready -n 0' needs no status — it is open-only by construction" \
    'bd ready -n 0 --json' 'ok'
vcheck "verdict: 'bd ready --json' still needs its cap" \
    'bd ready --json' 'no explicit row cap'
vcheck "verdict: a single stated status is explicit enough — the rule is 'stated', not 'all'" \
    'bd list --status in_progress -n 0 --json' 'ok'

printf -- '---\n%d passed, %d failed\n' "$pass" "$fail"
[ "$fail" -eq 0 ]
