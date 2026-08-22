#!/usr/bin/env bash
# Tests for the three arms of bd gqlc-tfh1 / GH #1042 — a sibling worktree
# whose branch tracks origin/master, so a bare `git push` there resolves to
# master:
#
#   A. .githooks/guard-push-destination decides on the RESOLVED destination.
#   B. .githooks/pre-push runs that guard, and runs it BEFORE the test suite.
#   C. The branch recipes in CLAUDE.md and in the citizen protocol, run
#      verbatim, leave no origin/master upstream behind.
#   D. `just check-worktree-upstream` names a worktree already in that state.
#
# B and C are the ones that stop this from being decoration: A alone passes
# with the guard wired to nothing, and D alone passes with the recipe reverted.
#
# Run via: just test-hooks
set -u

# When run under a git hook (this file runs from pre-push via `just test`),
# GIT_DIR etc. leak in and would redirect every throwaway repo's git commands
# to the parent repo. Isolate completely.
unset "${!GIT_@}"

HOOKS_DIR="$(cd "$(dirname "$0")/.." && pwd)"
ROOT="$(cd "$HOOKS_DIR/.." && pwd)"
GUARD="$HOOKS_DIR/guard-push-destination"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

GIT=(git -c user.email=t@t.invalid -c user.name=t -c commit.gpgsign=false)

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

# The guard's own words, asserted on so that "the push was refused" cannot be
# mistaken for "the guard refused it": with the guard neutered, pre-push still
# refuses, just for an unrelated reason (bd memory: a fake RED is not a kill).
MARKER='refusing a push that would land on the shared master branch'

# --- A. guard-push-destination, over crafted pre-push stdin -----------------
# $1=name $2=expected(reject|accept) $3=stdin
guard_case() {
    local name="$1" expected="$2" stdin="$3" decision
    if printf '%s' "$stdin" | "$GUARD" origin git@example.invalid:x/y.git >/dev/null 2>&1; then
        decision=accept
    else
        decision=reject
    fi
    if [ "$decision" = "$expected" ]; then
        ok "guard: $name"
    else
        bad "guard: $name (expected $expected, got $decision)"
    fi
}

A=1111111111111111111111111111111111111111
B=2222222222222222222222222222222222222222
Z=0000000000000000000000000000000000000000

# must reject
guard_case "feature branch resolving to master (the bare push this bead is about)" \
    reject "refs/heads/fix/x $A refs/heads/master $B
"
guard_case "HEAD:master, the remedy git's own refusal text suggests first" \
    reject "HEAD $A refs/heads/master $B
"
guard_case "deleting master" \
    reject "(delete) $Z refs/heads/master $B
"
guard_case "feature branch resolving to main" \
    reject "refs/heads/fix/x $A refs/heads/main $B
"
guard_case "master line is not the first line (a --all sweep)" \
    reject "refs/heads/fix/x $A refs/heads/fix/x $Z
refs/heads/other $A refs/heads/master $B
"
# Destination deliberately not master. With master on the right, the rule above
# refuses these lines whether or not the shape check exists — which is why the
# two rows that used to sit here decided nothing: replacing the whole check with
# `if false` left the suite at 27/27 (round-1 review, 2026-08-20).
guard_case "three fields, on a line no other rule would have refused" \
    reject "refs/heads/fix/x $A refs/heads/fix/x
"
guard_case "five fields, on a line no other rule would have refused" \
    reject "refs/heads/fix/x $A refs/heads/fix/x $B extra
"

# must accept
guard_case "nothing to push (git still runs the hook, with empty stdin)" accept ""
guard_case "bare push from master reports refs/heads/master on both sides" \
    accept "refs/heads/master $A refs/heads/master $B
"
guard_case "git push -u origin HEAD, the documented first push" \
    accept "HEAD $A refs/heads/fix/x $Z
"
guard_case "a branch pushed to its own name" \
    accept "refs/heads/fix/x $A refs/heads/fix/x $B
"
guard_case "a tag" \
    accept "refs/tags/v0 $A refs/tags/v0 $Z
"
guard_case "a whitespace-only line among real ones" \
    accept "refs/heads/fix/x $A refs/heads/fix/x $Z

"

# --- B. pre-push actually runs the guard, and runs it first -----------------
# A stub `just` both bounds the blast radius (a neutered guard would otherwise
# drop the real suite into a throwaway repo) and turns "did the guard run
# first?" into an observation: on a refused push its log must be empty.
STUB_BIN="$TMP/stub-bin"
JUST_LOG="$TMP/just.log"
mkdir -p "$STUB_BIN"
cat >"$STUB_BIN/just" <<'STUB'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"$JUST_LOG"
exit 1
STUB
chmod +x "$STUB_BIN/just"

ORIGIN="$TMP/origin.git"
"${GIT[@]}" init -q --bare -b master "$ORIGIN"
SEED="$TMP/seed"
"${GIT[@]}" init -q -b master "$SEED"
"${GIT[@]}" -C "$SEED" commit -q --allow-empty -m init
"${GIT[@]}" -C "$SEED" remote add origin "$ORIGIN"
"${GIT[@]}" -C "$SEED" push -q origin master

# A clone in exactly the state the old recipe produced: a feature branch whose
# upstream is origin/master.
E2E="$TMP/e2e"
"${GIT[@]}" clone -q "$ORIGIN" "$E2E"
"${GIT[@]}" -C "$E2E" checkout -q -b fix/x origin/master
"${GIT[@]}" -C "$E2E" commit -q --allow-empty -m "feature work"

# push.default=upstream is what removes the accident that saved ../gqlc-agt0:
# with it, a bare push here goes to master and git raises no objection.
: >"$JUST_LOG"
e2e_err="$TMP/e2e-reject.err"
if env PATH="$STUB_BIN:$PATH" JUST_LOG="$JUST_LOG" \
    "${GIT[@]}" -C "$E2E" -c core.hooksPath="$HOOKS_DIR" -c push.default=upstream \
    push >/dev/null 2>"$e2e_err"; then
    bad "pre-push: bare push from a branch tracking origin/master was ALLOWED"
else
    ok "pre-push: bare push from a branch tracking origin/master is refused"
fi
if grep -qF -- "$MARKER" "$e2e_err"; then
    ok "pre-push: the refusal is the guard's, not a bystander's"
else
    bad "pre-push: refused, but without the guard's message (guard not wired in?)"
fi
if [ -s "$JUST_LOG" ]; then
    bad "pre-push: the test suite ran before the guard decided ($(tr '\n' ' ' <"$JUST_LOG"))"
else
    ok "pre-push: the guard decided before the test suite ran"
fi

# The other direction: a push the guard has no business blocking must reach the
# suite. Without this, a guard hardwired to exit 1 passes every case above.
: >"$JUST_LOG"
e2e_ok_err="$TMP/e2e-accept.err"
env PATH="$STUB_BIN:$PATH" JUST_LOG="$JUST_LOG" \
    "${GIT[@]}" -C "$E2E" -c core.hooksPath="$HOOKS_DIR" \
    push -u origin HEAD >/dev/null 2>"$e2e_ok_err" || true
if grep -qxF 'test' "$JUST_LOG"; then
    ok "pre-push: 'git push -u origin HEAD' passes the guard and reaches 'just test'"
else
    bad "pre-push: 'git push -u origin HEAD' never reached 'just test'"
fi
if grep -qF -- "$MARKER" "$e2e_ok_err"; then
    bad "pre-push: the guard blocked a branch pushed to its own name"
else
    ok "pre-push: the guard let a branch pushed to its own name through"
fi

# A forced identity push to master, over real git, with master rewritten so the
# `+` is doing work. It is allowed, and these two rows are here to make that a
# recorded decision rather than an oversight: a `+` refspec reaches neither this
# hook's argv nor its stdin, so there is nothing here to tell it from the plain
# form. See the Limits paragraph in guard-push-destination.
FORCE_WT="$TMP/state-force-master"
"${GIT[@]}" clone -q "$ORIGIN" "$FORCE_WT"
"${GIT[@]}" -C "$FORCE_WT" commit -q --amend --allow-empty -m "master, rewritten"
: >"$JUST_LOG"
force_err="$TMP/e2e-force.err"
env PATH="$STUB_BIN:$PATH" JUST_LOG="$JUST_LOG" \
    "${GIT[@]}" -C "$FORCE_WT" -c core.hooksPath="$HOOKS_DIR" \
    push origin '+refs/heads/master:refs/heads/master' >/dev/null 2>"$force_err" || true
if grep -qxF 'test' "$JUST_LOG"; then
    ok "pre-push: a forced master:master reads as identity here and reaches 'just test'"
else
    bad "pre-push: a forced master:master never reached 'just test'"
fi
if grep -qF -- "$MARKER" "$force_err"; then
    bad "pre-push: the guard refused a forced master:master — it cannot see the '+'"
else
    ok "pre-push: the guard treats a forced master:master as it treats a plain one"
fi

# --- C. the recipe in CLAUDE.md, run verbatim -------------------------------
# Extracted rather than restated: a copy here would keep passing after someone
# reverted the document, which is the failure this bead is a report of.
recipe="$(grep -m1 -E '^git worktree add ' "$ROOT/CLAUDE.md" || true)"
if [ -z "$recipe" ]; then
    bad "CLAUDE.md: no 'git worktree add' line found to test"
else
    DOC="$TMP/doc"
    "${GIT[@]}" clone -q "$ORIGIN" "$DOC"
    doc_wt="$TMP/doc-worktree"
    cmd="${recipe//..\/<repo>-<bead-slug>/$doc_wt}"
    cmd="${cmd//<branch-name>/feat/doc-probe}"
    if (cd "$DOC" && eval "$cmd") >/dev/null 2>&1; then
        up="$("${GIT[@]}" -C "$doc_wt" rev-parse --abbrev-ref --symbolic-full-name '@{u}' 2>/dev/null || true)"
        if [ "$up" = "origin/master" ]; then
            bad "CLAUDE.md: the documented recipe leaves the branch tracking origin/master"
        else
            ok "CLAUDE.md: the documented recipe leaves upstream '${up:-<none>}', not origin/master"
        fi
    else
        bad "CLAUDE.md: the documented recipe did not run ($cmd)"
    fi
fi

# --no-track leaves no upstream at all, so the document has to say how the
# first push sets one; otherwise the recipe hands over a worktree that cannot
# push without a flag nobody wrote down.
if grep -qF 'git push -u origin HEAD' "$ROOT/CLAUDE.md"; then
    ok "CLAUDE.md: names the first push that sets the upstream"
else
    bad "CLAUDE.md: no 'git push -u origin HEAD' — the recipe leaves no upstream and no way to set one"
fi

# The same defect in a second spelling. citizen-protocol.md step 1 branches with
# `git checkout -b <type>/<slug> origin/master`, which tracks for the reason the
# worktree recipe did; every citizen runs it at the start of every bead, so it is
# the more travelled of the two. Fixing one spelling and shipping the other would
# close this bead on a false premise (Սեդրակ's ruling, 2026-08-21). Extracted and
# run rather than grepped, for the reason given in section C.
PROTOCOL="$ROOT/kingdom/brain/playbooks/citizen-protocol.md"
proto="$(grep -m1 -oE '`git fetch origin && git checkout [^`]*`' "$PROTOCOL" | tr -d '`' || true)"
if [ -z "$proto" ]; then
    bad "citizen-protocol.md: no 'git fetch && git checkout' branch recipe found to test"
else
    PDOC="$TMP/proto"
    "${GIT[@]}" clone -q "$ORIGIN" "$PDOC"
    pcmd="${proto//<type>\/<slug>/feat/proto-probe}"
    if (cd "$PDOC" && eval "$pcmd") >/dev/null 2>&1; then
        pup="$("${GIT[@]}" -C "$PDOC" rev-parse --abbrev-ref --symbolic-full-name '@{u}' 2>/dev/null || true)"
        if [ "$pup" = "origin/master" ]; then
            bad "citizen-protocol.md: the documented branch recipe leaves the branch tracking origin/master"
        else
            ok "citizen-protocol.md: the documented branch recipe leaves upstream '${pup:-<none>}', not origin/master"
        fi
    else
        bad "citizen-protocol.md: the documented branch recipe did not run ($pcmd)"
    fi
fi

if grep -qF 'git push -u origin HEAD' "$PROTOCOL"; then
    ok "citizen-protocol.md: names the first push that sets the upstream"
else
    bad "citizen-protocol.md: no 'git push -u origin HEAD' — the recipe leaves no upstream and no way to set one"
fi

# --- D. just check-worktree-upstream ----------------------------------------
# Wiring first. The recipe existing and the recipe running are two facts, and
# every case below establishes only the first. Read out of `just --dump`'s
# parsed recipe graph rather than grepped out of the justfile, so a dependency
# sitting in a comment cannot answer for one that is wired.
if just --dump --dump-format json 2>/dev/null | python3 -c '
import json, sys
d = json.load(sys.stdin)
deps = [x["recipe"] for x in d["recipes"]["test"]["dependencies"]]
sys.exit(0 if "check-worktree-upstream" in deps else 1)
'; then
    ok "check-worktree-upstream: 'just test' depends on it"
else
    bad "check-worktree-upstream: 'just test' does not depend on it — nothing runs it"
fi

# $1=name $2=expected(reject|accept) $3=dir
recipe_case() {
    local name="$1" expected="$2" dir="$3" decision
    if just -f "$ROOT/justfile" check-worktree-upstream "$dir" >/dev/null 2>&1; then
        decision=accept
    else
        decision=reject
    fi
    if [ "$decision" = "$expected" ]; then
        ok "check-worktree-upstream: $name"
    else
        bad "check-worktree-upstream: $name (expected $expected, got $decision)"
    fi
}

BAD_WT="$TMP/state-bad"
"${GIT[@]}" clone -q "$ORIGIN" "$BAD_WT"
"${GIT[@]}" -C "$BAD_WT" checkout -q -b fix/tracks-master origin/master
recipe_case "a branch tracking origin/master is named" reject "$BAD_WT"

"${GIT[@]}" -C "$BAD_WT" branch --unset-upstream
recipe_case "the documented repair clears it" accept "$BAD_WT"

OWN_WT="$TMP/state-own"
"${GIT[@]}" clone -q "$ORIGIN" "$OWN_WT"
"${GIT[@]}" -C "$OWN_WT" checkout -q -b fix/own origin/master
"${GIT[@]}" -C "$OWN_WT" branch --unset-upstream
"${GIT[@]}" -C "$OWN_WT" commit -q --allow-empty -m work
"${GIT[@]}" -C "$OWN_WT" push -q --no-verify -u origin HEAD
recipe_case "a branch tracking its own remote branch" accept "$OWN_WT"

MASTER_WT="$TMP/state-master"
"${GIT[@]}" clone -q "$ORIGIN" "$MASTER_WT"
recipe_case "master tracking origin/master (what CI checks out on a master push)" \
    accept "$MASTER_WT"

DETACHED_WT="$TMP/state-detached"
"${GIT[@]}" clone -q "$ORIGIN" "$DETACHED_WT"
"${GIT[@]}" -C "$DETACHED_WT" checkout -q --detach HEAD
recipe_case "detached HEAD (a reviewer at a SHA, and CI on a pull_request)" \
    accept "$DETACHED_WT"

NEAR_WT="$TMP/state-near-miss"
"${GIT[@]}" clone -q "$ORIGIN" "$NEAR_WT"
"${GIT[@]}" -C "$NEAR_WT" checkout -q -b fix/near origin/master
"${GIT[@]}" -C "$NEAR_WT" branch --unset-upstream
"${GIT[@]}" -C "$NEAR_WT" push -q --no-verify origin "HEAD:refs/heads/topic/master"
"${GIT[@]}" -C "$NEAR_WT" fetch -q origin
"${GIT[@]}" -C "$NEAR_WT" branch --set-upstream-to=origin/topic/master >/dev/null 2>&1
recipe_case "upstream origin/topic/master is not this bug" accept "$NEAR_WT"

printf -- '---\n%d passed, %d failed\n' "$pass" "$fail"
[ "$fail" -eq 0 ]
