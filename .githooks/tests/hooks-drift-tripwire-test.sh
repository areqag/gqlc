#!/usr/bin/env bash
# Unit tests for .githooks/hooks-drift-tripwire.
#
# The tripwire's claim is behavioural — "a commit or a push is REFUSED while
# core.hooksPath is drifted, and nothing changes while it is healthy" — so every
# row here runs a real git operation against a throwaway repo and asserts on
# whether it landed, not on what the script prints. A row that only asserted the
# message would stay green if the script exited 0.
#
# These tests install the tripwire with `cp`, which is what `just init` does.
# They deliberately do NOT invoke `just init`: that recipe writes
# core.hooksPath, and a recipe invocation that resolved its working directory to
# this repo instead of the fixture would rewrite the config shared by every
# linked worktree — the exact damage under test.
#
# Run via: just test-hooks
set -u

# When run under a git hook (pre-push via `just test`), GIT_DIR and friends leak
# in and would redirect the fixture's git commands at the parent repo. Through
# the SHARED line rather than a private copy of it (bd gqlc-07bf).
# shellcheck source=../git-env-sandbox.sh disable=SC1091
source "$(cd "$(dirname "$0")/.." && pwd)/git-env-sandbox.sh"

TRIPWIRE="$(cd "$(dirname "$0")/.." && pwd)/hooks-drift-tripwire"
INSTALLER="$(cd "$(dirname "$0")/.." && pwd)/install-hooks-drift-tripwire"
JUSTFILE="$(cd "$(dirname "$0")/../.." && pwd)/justfile"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

# `just check-hooks` is under test below, so a missing `just` is fatal rather
# than skipped: a suite that quietly drops the rows covering the health check is
# the shape of failure this whole file exists to refuse.
if ! command -v just >/dev/null 2>&1; then
    echo "FAIL - just is not on PATH, so the check-hooks rows cannot run" >&2
    exit 1
fi

pass=0
fail=0

check() {
    local name="$1" want="$2" got="$3"
    if [ "$want" = "$got" ]; then
        pass=$((pass + 1)); printf 'ok   - %s\n' "$name"
    else
        fail=$((fail + 1)); printf 'FAIL - %s (expected %s, got %s)\n' "$name" "$want" "$got"
    fi
}

# A fixture repo shaped like this one: a .githooks/ holding a live hook, plus
# the tripwire installed in the default hooks directory. $1 names the repo.
make_repo() {
    local repo="$1"
    git init -q -b master "$repo"
    git -C "$repo" config user.email t@t.invalid
    git -C "$repo" config user.name t

    mkdir -p "$repo/.githooks"
    # Stands in for the real pre-commit. Records that it ran, so a row can tell
    # "the repo's own hook ran" from "the tripwire ran" rather than inferring it
    # from the exit status, which both hooks can produce.
    cat >"$repo/.githooks/pre-commit" <<'EOF'
#!/usr/bin/env bash
echo ran >"$(git rev-parse --git-common-dir)/githooks-precommit-witness"
exit 0
EOF
    chmod +x "$repo/.githooks/pre-commit"

    # `just check-hooks` runs `.githooks/install-hooks-drift-tripwire` by a path
    # relative to its working directory, so the fixture needs its own copy for
    # the rows that drive the real recipe with --working-directory.
    cp "$TRIPWIRE" "$repo/.githooks/hooks-drift-tripwire"
    cp "$INSTALLER" "$repo/.githooks/install-hooks-drift-tripwire"
    chmod +x "$repo/.githooks/hooks-drift-tripwire" "$repo/.githooks/install-hooks-drift-tripwire"

    local dir
    dir="$(hooks_dir "$repo")"
    mkdir -p "$dir"
    local name
    for name in pre-commit commit-msg pre-push post-checkout post-merge; do
        cp "$TRIPWIRE" "$dir/$name"
        chmod +x "$dir/$name"
    done

    echo seed >"$repo/seed.txt"
    git -C "$repo" add -A
    git -C "$repo" -c core.hooksPath=.githooks commit -q -m seed
    git -C "$repo" config core.hooksPath .githooks
}

# The directory the drift redirects execution to, for repo $1.
hooks_dir() {
    echo "$(git -C "$1" rev-parse --path-format=absolute --git-common-dir)/hooks"
}

# Installs file $2 under all five hook names in repo $1.
install_as_all() {
    local repo="$1" file="$2" dir name
    dir="$(hooks_dir "$repo")"
    mkdir -p "$dir"
    for name in pre-commit commit-msg pre-push post-checkout post-merge; do
        cp "$file" "$dir/$name"
        chmod +x "$dir/$name"
    done
}

# Installs file $3 under the single hook name $2 in repo $1, leaving the other
# four as make_repo left them. This is what lets a row see which names an array
# holds: under install_as_all every name carries the shape, so any one of them
# trips the check and answers for the rest.
install_as_one() {
    local repo="$1" name="$2" file="$3" dir
    dir="$(hooks_dir "$repo")"
    mkdir -p "$dir"
    cp "$file" "$dir/$name"
    chmod +x "$dir/$name"
}

# How many of the five names carry the tripwire marker in repo $1.
count_marked() {
    local repo="$1" dir name n=0
    dir="$(hooks_dir "$repo")"
    for name in pre-commit commit-msg pre-push post-checkout post-merge; do
        if [ -e "$dir/$name" ] && grep -q gqlc-hooks-drift-tripwire "$dir/$name" 2>/dev/null; then
            n=$((n + 1))
        fi
    done
    echo "$n"
}

# Runs the repo's REAL `just check-hooks` against fixture $1, leaving its
# combined output in CH_OUT and returning its exit status.
#
# CI is unset for the invocation: check-hooks short-circuits to 0 under CI, so
# leaving it set would make every row below green without running the body — a
# suite reporting on a recipe it never entered.
#
# The fixture's git resolution is asserted to land inside $TMP first. A
# --working-directory that failed to take would point check-hooks at the repo
# this suite is running in, whose hooks directory is shared by every linked
# worktree, and its self-heal arm WRITES.
CH_OUT=""
run_check_hooks() {
    local repo="$1" common
    common="$(git -C "$repo" rev-parse --path-format=absolute --git-common-dir)"
    case "$common" in
        "$TMP"/*) ;;
        *) CH_OUT="fixture escaped \$TMP: $common"; return 99 ;;
    esac
    CH_OUT="$(env -u CI just --justfile "$JUSTFILE" --working-directory "$repo" check-hooks 2>&1)"
}

# Runs the real installer against fixture $1, with the same containment guard.
run_installer() {
    local repo="$1"
    shift
    local common
    common="$(git -C "$repo" rev-parse --path-format=absolute --git-common-dir)"
    case "$common" in
        "$TMP"/*) ;;
        *) return 99 ;;
    esac
    (cd "$repo" && "$INSTALLER" "$@") 2>&1
}

# passed|refused for an exit status, so a row reads as a verdict rather than a
# number. This protects a row expecting `passed` — the containment bail-out (99)
# reads as `refused` and reddens it — but NOT one expecting `refused`, which goes
# green on a 99 that never entered the recipe. Every such row is therefore paired
# with a CAUSE row asserting what CH_OUT says, and on a 99 CH_OUT holds
# "fixture escaped $TMP" rather than the product message, so the pair reddens.
verdict() {
    if [ "$1" -eq 0 ]; then echo passed; else echo refused; fi
}

# Attempts a commit in $1 and echoes landed|blocked.
try_commit() {
    local repo="$1" msg="${2:-change}"
    local before after
    before="$(git -C "$repo" rev-parse HEAD)"
    date +%s%N >>"$repo/churn.txt"
    git -C "$repo" add -A >/dev/null 2>&1
    git -C "$repo" commit -q -m "$msg" >/dev/null 2>&1
    after="$(git -C "$repo" rev-parse HEAD)"
    # Asserts on whether HEAD MOVED, not on git's exit status: a hook that
    # refused and a hook that failed for its own reasons both exit non-zero,
    # and only one of them leaves the commit unwritten.
    if [ "$before" = "$after" ]; then echo blocked; else echo landed; fi
}

# --- healthy: the tripwire is installed but must never be consulted -----------

REPO="$TMP/healthy"
make_repo "$REPO"
git -C "$REPO" config core.hooksPath .githooks
rm -f "$REPO/.git/githooks-precommit-witness"
check "healthy hooksPath: commit lands" landed "$(try_commit "$REPO")"
check "healthy hooksPath: the repo's own pre-commit is what ran" ran \
    "$(cat "$REPO/.git/githooks-precommit-witness" 2>/dev/null || echo missing)"

# --- drifted: an absolute path at the common dir's hooks directory ------------

REPO="$TMP/drift-abs"
make_repo "$REPO"
git -C "$REPO" config core.hooksPath "$(git -C "$REPO" rev-parse --path-format=absolute --git-common-dir)/hooks"
check "drifted to an absolute common-dir hooks path: commit REFUSED" blocked "$(try_commit "$REPO")"

# --- drifted: unset, which is the same directory by default ------------------

REPO="$TMP/drift-unset"
make_repo "$REPO"
git -C "$REPO" config --unset core.hooksPath
check "core.hooksPath unset: commit REFUSED" blocked "$(try_commit "$REPO")"

# --- the refusal has to say how to repair it ---------------------------------

out="$( (cd "$REPO" || exit 1; "$TRIPWIRE" 2>&1 >/dev/null) || true )"
case "$out" in
    *"just init"*) check "the refusal names the repair" yes yes ;;
    *) check "the refusal names the repair" yes no ;;
esac
case "$out" in
    *commit-msg*) check "the refusal names the dead AI-attribution gate" yes yes ;;
    *) check "the refusal names the dead AI-attribution gate" yes no ;;
esac

# --- the severity word must agree with what happened (bd gqlc-g1o8) -----------
# The message block used to run UNCONDITIONALLY, before the case that decides the
# exit status, so all five installed names printed an identical block headed
# ERROR and then three exited 1 while two exited 0. Measured, three copies run by
# name: pre-commit exit=1 "ERROR: ...", post-checkout exit=0 "ERROR: ...",
# post-merge exit=0 "ERROR: ...".
#
# So after `git checkout` or `git worktree add` under drift, a reader met a block
# saying ERROR over an operation that SUCCEEDED and that the hook deliberately
# did not block, with nothing in the text saying so. That is the worst place for
# it: `git worktree add` is the operation that CAUSES the drift (bd gqlc-7ijy),
# so the first meeting with this message is on a command that worked. Crying wolf
# in an autonomous town is expensive — the block read as noise is the block
# ignored when it is real.
#
# Three claims per arm, and the exit status is asserted alongside the words:
# without that pairing, a copy that printed WARNING and then exited 1 would
# satisfy the word rows, which is the same disagreement in the other direction.

NAMED="$TMP/named"
mkdir -p "$NAMED"

# run_named <hook name>, leaving the stderr block in NAMED_OUT and the exit
# status in NAMED_RC. Both are variables and neither is echoed: an echoed status
# consumed by `$(...)` runs the function in a subshell, so every NAMED_OUT it set
# is discarded and the word rows read a stale value — measured on the first cut
# of this section, where all twelve went red against an empty string, and where
# a differently-ordered section would instead have gone GREEN against the
# previous row's output.
NAMED_OUT=""
NAMED_RC=0
run_named() {
    cp "$TRIPWIRE" "$NAMED/$1"
    chmod +x "$NAMED/$1"
    NAMED_RC=0
    NAMED_OUT="$( (cd "$REPO" || exit 1; "$NAMED/$1" 2>&1 >/dev/null) )" || NAMED_RC=$?
}

says() { # $1=name $2=needle
    case "$NAMED_OUT" in
        *"$2"*) check "$1" yes yes ;;
        *) check "$1" yes no ;;
    esac
}

run_named pre-commit
check "as pre-commit: exits 1" 1 "$NAMED_RC"
says "as pre-commit: the block is headed ERROR" "ERROR: local git hooks are inactive"
says "as pre-commit: it says the operation was REFUSED" "was REFUSED"

for warn in post-checkout post-merge; do
    run_named "$warn"
    check "as $warn: exits 0" 0 "$NAMED_RC"
    says "as $warn: the block is headed WARNING" "WARNING: local git hooks are inactive"
    says "as $warn: it says the operation was ALLOWED" "was ALLOWED"
    # "cannot block it, and did not try", never "is ignored". post-merge's
    # status genuinely IS ignored by git; post-checkout's BECOMES `git
    # checkout`'s and `git switch`'s, so it is observed and the 0 is deliberate.
    # A message saying "ignored" would be false for one of the two names.
    says "as $warn: it does not claim the status is ignored" "cannot block it, and did not try"
    case "$NAMED_OUT" in
        *"is ignored"*) check "as $warn: the word 'ignored' is absent" yes no ;;
        *) check "as $warn: the word 'ignored' is absent" yes yes ;;
    esac
    # The whole block survives on the warn arms: the reader still needs to know
    # which gates are dead and how to repair it, and the repair is the same
    # either way.
    says "as $warn: the repair is still named" "just init"
    says "as $warn: the dead gates are still enumerated" "commit-msg"
done

# --- a push is refused too ---------------------------------------------------

REPO="$TMP/drift-push"
make_repo "$REPO"
git init -q --bare "$TMP/remote.git"
git -C "$REPO" remote add origin "$TMP/remote.git"
git -C "$REPO" config --unset core.hooksPath
if git -C "$REPO" push -q origin master >/dev/null 2>&1; then
    check "drifted: push REFUSED" blocked landed
else
    check "drifted: push REFUSED" blocked blocked
fi

# --- one install arms every linked worktree ----------------------------------
# Linked worktrees share the common dir, so they share both the drifted config
# and the tripwire. This is the row that makes a single `just init` sufficient.

REPO="$TMP/drift-wt"
make_repo "$REPO"
git -C "$REPO" config --unset core.hooksPath
git -C "$REPO" worktree add -q "$TMP/linked" -b linked >/dev/null 2>&1
wt_rc=$?
check "drifted: a commit from a LINKED worktree is REFUSED" blocked "$(try_commit "$TMP/linked")"

# --- git worktree add must still produce a usable worktree -------------------
# post-checkout fires during `git worktree add`. Its exit status is NOT ignored:
# githooks(5) says it "becomes the exit status of these two commands" (git switch
# and git checkout), and measured, `git worktree add` returns 1 AFTER creating the
# worktree while `git checkout -b` and `git switch` return 1 with HEAD moved. The
# tripwire exits 0 on post-checkout for that reason, and these rows hold it to it.
#
# Both halves are asserted because neither alone sees the regression: the
# worktree is created either way, so the file check stays green when
# post-checkout starts failing, and it is the exit status that carries the
# damage — an agent spawn reporting failure over a worktree that exists.

check "drifted: worktree add REPORTS success (post-checkout does not gate)" 0 "$wt_rc"
check "drifted: worktree add produced a populated worktree" yes \
    "$([ -f "$TMP/linked/seed.txt" ] && echo yes || echo no)"

# --- bd gqlc-b1kt: `git rebase -i` reword ------------------------------------
# A reword fires pre-commit and commit-msg, so under drift it used to smuggle a
# commit message past the AI-attribution gate. Measured on this branch: with the
# gate wired and the tripwire absent, a reword landed
# `Co-Authored-By: Claude <noreply@anthropic.com>` on HEAD.

REPO="$TMP/drift-reword"
make_repo "$REPO"
echo more >>"$REPO/seed.txt"
git -C "$REPO" add -A
git -C "$REPO" -c core.hooksPath=.githooks commit -q -m "second"
git -C "$REPO" config --unset core.hooksPath

cat >"$TMP/reword-editor.sh" <<'EOF'
#!/usr/bin/env bash
printf 'reworded\n\nCo-Authored-By: Claude <noreply@anthropic.com>\n' >"$1"
EOF
chmod +x "$TMP/reword-editor.sh"

before="$(git -C "$REPO" rev-parse HEAD)"
GIT_SEQUENCE_EDITOR="sed -i '1s/^pick/reword/'" GIT_EDITOR="$TMP/reword-editor.sh" \
    git -C "$REPO" rebase -i HEAD~1 >/dev/null 2>&1
after="$(git -C "$REPO" rev-parse HEAD)"
git -C "$REPO" rebase --abort >/dev/null 2>&1
if [ "$before" = "$after" ]; then
    check "drifted: rebase -i reword REFUSED (bd gqlc-b1kt)" blocked blocked
else
    check "drifted: rebase -i reword REFUSED (bd gqlc-b1kt)" blocked landed
fi
check "drifted: the reword did not smuggle an AI-attribution trailer onto HEAD" 0 \
    "$(git -C "$REPO" log -1 --format=%B | grep -ci claude || true)"

# --- a merge commit reaches a different name ---------------------------------
# git runs pre-merge-commit for a merge commit, not pre-commit, and
# pre-merge-commit is not one of the five names installed here. Of the five, the
# ones git reaches on this path are commit-msg, before the commit, and post-merge
# after it — and post-merge exits 0 by design. So commit-msg is what refuses a
# merge under drift, on its own rather than as a second vote behind pre-commit.
# Measured: with every name real the merge is refused; with commit-msg alone
# disarmed the merge LANDS carrying a git-parsed
# `Co-Authored-By: Claude <noreply@anthropic.com>` trailer; with pre-commit alone
# disarmed it is still refused.

REPO="$TMP/drift-merge"
make_repo "$REPO"
git -C "$REPO" checkout -q -b side
echo side >"$REPO/side.txt"
git -C "$REPO" add -A
git -C "$REPO" -c core.hooksPath=.githooks commit -q -m side
git -C "$REPO" checkout -q master
echo main >"$REPO/main.txt"
git -C "$REPO" add -A
git -C "$REPO" -c core.hooksPath=.githooks commit -q -m main2
git -C "$REPO" config --unset core.hooksPath

printf 'merge side\n\nCo-Authored-By: Claude <noreply@anthropic.com>\n' >"$TMP/merge-msg"
before="$(git -C "$REPO" rev-parse HEAD)"
git -C "$REPO" merge --no-ff -F "$TMP/merge-msg" side >/dev/null 2>&1
after="$(git -C "$REPO" rev-parse HEAD)"
git -C "$REPO" merge --abort >/dev/null 2>&1
if [ "$before" = "$after" ]; then
    check "drifted: a merge commit is REFUSED (commit-msg is the name git reaches)" blocked blocked
else
    check "drifted: a merge commit is REFUSED (commit-msg is the name git reaches)" blocked landed
fi
check "drifted: the merge did not smuggle an AI-attribution trailer onto HEAD" 0 \
    "$(git -C "$REPO" log -1 --format=%B | grep -ci claude || true)"

# --- `just check-hooks` must certify the guard FIRES, not that a file is there -
# The marker grep certifies PRESENCE. Measured against the first cut of this
# branch: a three-line file carrying the shebang, the marker and `exit 0`,
# installed as all five names, made `just doctor` print "ok" (rc=0) while a
# drifted commit LANDED. So did a 1000-byte `cp` truncation of the real tripwire —
# the marker is on line 2 and everything up to the case statement is comment, so
# a prefix parses and exits 0. Both are pinned here from the outside: the fixture
# drives the repo's REAL recipe rather than a re-implementation of it.

NOOP="$TMP/marker-noop"
printf '#!/usr/bin/env bash\n# gqlc-hooks-drift-tripwire\nexit 0\n' >"$NOOP"
chmod +x "$NOOP"
TRUNC="$TMP/trunc-1000"
head -c 1000 "$TRIPWIRE" >"$TRUNC"
chmod +x "$TRUNC"

# The premise the two rows below rest on: both shapes really do pass the marker
# grep and really do exit 0. Without these, a row could go green because the
# fixture was malformed rather than because the check bit.
"$NOOP" >/dev/null 2>&1
check "the marker-bearing no-op exits 0 (so the marker grep alone passes it)" 0 "$?"
"$TRUNC" >/dev/null 2>&1
check "a 1000-byte truncation parses and exits 0" 0 "$?"
check "a 1000-byte truncation still carries the marker" yes \
    "$(grep -q gqlc-hooks-drift-tripwire "$TRUNC" && echo yes || echo no)"

REPO="$TMP/ch-real"
make_repo "$REPO"
run_check_hooks "$REPO"
check "check-hooks: a real install passes" passed "$(verdict $?)"

REPO="$TMP/ch-noop"
make_repo "$REPO"
install_as_all "$REPO" "$NOOP"
run_check_hooks "$REPO"
check "check-hooks: a marker-bearing 'exit 0' no-op is REFUSED" refused "$(verdict $?)"
case "$CH_OUT" in
    *"exits 0 when run"*) check "check-hooks: the refusal says the hook exits 0" yes yes ;;
    *) check "check-hooks: the refusal says the hook exits 0" yes no ;;
esac

REPO="$TMP/ch-trunc"
make_repo "$REPO"
install_as_all "$REPO" "$TRUNC"
run_check_hooks "$REPO"
check "check-hooks: a truncated copy of the tripwire is REFUSED" refused "$(verdict $?)"
# verdict() reads an exit status and nothing else, so a row expecting `refused`
# goes green on any non-zero exit whatever produced it. Demonstrated: with
# `exit 1` planted at the top of the recipe body, this row and three others
# stayed green. The message is what says WHICH check bit.
case "$CH_OUT" in
    *"exits 0 when run"*) check "check-hooks: the truncation is refused for exiting 0" yes yes ;;
    *) check "check-hooks: the truncation is refused for exiting 0" yes no ;;
esac

# --- WHICH names are checked, not merely that some name is -------------------
# Every row above installs its shape under all five names, which hides what
# `blocking` and `warning` contain: drop one name from either array and the
# remaining four trip the check on the same fixture with the same verdict, so
# every row stays green. Measured for each of the five. Dropping `commit-msg`
# is the one that costs something — the merge rows above show that name refusing
# a drifted merge commit on its own, so a disarmed copy there that check-hooks
# no longer looks at is bd gqlc-b1kt's hole with `just doctor` printing ok.
#
# So one fixture per name, disarmed ALONE with the other four left real. The
# five names are spelled out here rather than read from the justfile, because a
# test that sources its expectation from the artifact under test asserts nothing
# about it.
#
# Each pair asserts the exit status AND that the refusal names that hook's own
# path, so a row cannot go green on a different name's refusal — the failure
# mode this whole section exists to close.

BLOCKER="$TMP/marker-blocker"
printf '#!/usr/bin/env bash\n# gqlc-hooks-drift-tripwire\nexit 1\n' >"$BLOCKER"
chmod +x "$BLOCKER"

# The blocking arms: a marker-bearing no-op at one name, the rest real.
for name in pre-commit commit-msg pre-push; do
    REPO="$TMP/ch-only-$name"
    make_repo "$REPO"
    install_as_one "$REPO" "$name" "$NOOP"
    run_check_hooks "$REPO"
    check "check-hooks: a no-op at $name ALONE is REFUSED" refused "$(verdict $?)"
    case "$CH_OUT" in
        *"/$name carries the drift tripwire marker"*)
            check "check-hooks: the refusal names $name" yes yes ;;
        *) check "check-hooks: the refusal names $name" yes no ;;
    esac
done

# The warn arms, in the other direction: a copy that BLOCKS. post-checkout's
# exit status becomes git checkout's and git switch's, so a blocking copy there
# breaks every branch switch in every drifted worktree; post-merge's status is
# ignored by git, so a non-zero code there is a copy that is not this tripwire.
for name in post-checkout post-merge; do
    REPO="$TMP/ch-blocking-$name"
    make_repo "$REPO"
    install_as_one "$REPO" "$name" "$BLOCKER"
    run_check_hooks "$REPO"
    check "check-hooks: a copy that BLOCKS at $name ALONE is REFUSED" refused "$(verdict $?)"
    case "$CH_OUT" in
        *"/$name exits non-zero when run"*)
            check "check-hooks: the refusal names $name" yes yes ;;
        *) check "check-hooks: the refusal names $name" yes no ;;
    esac
done

# --- check-hooks SELF-HEALS a missing install, and refuses a drifted config ---
# `just test` is what .githooks/pre-push runs, so refusing here over an absent
# hook file would make `just init` a precondition for every push in every
# registered worktree — and `git push --no-verify`, the obvious answer to a push
# refused for a reason unrelated to the commits, skips .githooks/pre-push
# WHOLESALE and takes `just test` and `just lint-new` with it. An absent file is
# unambiguous, so it is repaired. ensure-golangci sets the precedent.

REPO="$TMP/ch-selfheal"
make_repo "$REPO"
rm -f "$(hooks_dir "$REPO")"/{pre-commit,commit-msg,pre-push,post-checkout,post-merge}
check "self-heal fixture starts with no tripwire installed" 0 "$(count_marked "$REPO")"
run_check_hooks "$REPO"
check "check-hooks: a missing install is SELF-HEALED, not refused" passed "$(verdict $?)"
check "check-hooks: the self-heal installs all five names" 5 "$(count_marked "$REPO")"
case "$CH_OUT" in
    *"self-healed"*) check "check-hooks: the self-heal says so" yes yes ;;
    *) check "check-hooks: the self-heal says so" yes no ;;
esac
# The repair has to be a real one, not a file that satisfies the next check.
git -C "$REPO" config --unset core.hooksPath
check "check-hooks: a drifted commit AFTER the self-heal is REFUSED" blocked "$(try_commit "$REPO")"
git -C "$REPO" config core.hooksPath .githooks
run_check_hooks "$REPO"
check "check-hooks: a second run heals nothing and says nothing" "" "$CH_OUT"

# The self-heal installs what is ABSENT and leaves what is present alone. The
# install is shared by every linked worktree while each worktree's copy of the
# source sits at its own parked commit, so a refresh on every push would have two
# worktrees on different branches rewriting each other's copy on every push. An
# older copy that still refuses is a working copy.
REPO="$TMP/ch-parked-copy"
make_repo "$REPO"
OLDER="$TMP/older-tripwire"
cat "$TRIPWIRE" >"$OLDER"
echo '# an older copy of this file, from another worktree parked at another commit' >>"$OLDER"
install_as_all "$REPO" "$OLDER"
rm -f "$(hooks_dir "$REPO")/pre-push"
run_check_hooks "$REPO"
check "check-hooks: an older parked copy plus one gap heals the gap" passed "$(verdict $?)"
check "check-hooks: the older copies were left alone" 4 \
    "$(grep -l 'an older copy of this file' "$(hooks_dir "$REPO")"/{pre-commit,commit-msg,post-checkout,post-merge} | wc -l)"
check "check-hooks: the gap was filled from the current source" no \
    "$(grep -q 'an older copy of this file' "$(hooks_dir "$REPO")/pre-push" && echo yes || echo no)"

# Arm 1 must NOT self-heal: rewriting core.hooksPath would erase the very drift
# the recipe exists to report, and that value is in the config every linked
# worktree shares.
REPO="$TMP/ch-drifted-config"
make_repo "$REPO"
git -C "$REPO" config core.hooksPath "$(hooks_dir "$REPO")"
run_check_hooks "$REPO"
check "check-hooks: a drifted core.hooksPath is REFUSED, not healed" refused "$(verdict $?)"
case "$CH_OUT" in
    *"core.hooksPath is '$(hooks_dir "$REPO")'"*)
        check "check-hooks: the refusal quotes the drifted value" yes yes ;;
    *) check "check-hooks: the refusal quotes the drifted value" yes no ;;
esac
check "check-hooks: it did not rewrite core.hooksPath" "$(hooks_dir "$REPO")" \
    "$(git -C "$REPO" config --get core.hooksPath)"

# --- a foreign hook squatting a name is the one ambiguous case: REFUSE --------

REPO="$TMP/ch-foreign"
make_repo "$REPO"
rm -f "$(hooks_dir "$REPO")"/{pre-commit,commit-msg,pre-push,post-checkout,post-merge}
printf '#!/usr/bin/env bash\n# somebody else wrote this\nexit 0\n' >"$(hooks_dir "$REPO")/post-merge"
chmod +x "$(hooks_dir "$REPO")/post-merge"
run_check_hooks "$REPO"
check "check-hooks: a foreign hook squatting a name is REFUSED" refused "$(verdict $?)"
case "$CH_OUT" in
    *"not the drift tripwire:"*"/post-merge"*)
        check "check-hooks: the refusal names the squatted path" yes yes ;;
    *) check "check-hooks: the refusal names the squatted path" yes no ;;
esac
check "check-hooks: the foreign hook was not clobbered" yes \
    "$(grep -q 'somebody else wrote this' "$(hooks_dir "$REPO")/post-merge" && echo yes || echo no)"
# The refusal used to abort mid-loop, so a foreign post-merge (last in loop
# order) left 4 of 5 installed and a foreign pre-commit (first) left 0. The five
# names go in as a set or not at all, whichever name is squatted.
check "install: a foreign LAST name installs none of the five" 0 "$(count_marked "$REPO")"

REPO="$TMP/ch-foreign-first"
make_repo "$REPO"
rm -f "$(hooks_dir "$REPO")"/{pre-commit,commit-msg,pre-push,post-checkout,post-merge}
printf '#!/usr/bin/env bash\n# somebody else wrote this\nexit 0\n' >"$(hooks_dir "$REPO")/pre-commit"
chmod +x "$(hooks_dir "$REPO")/pre-commit"
run_installer "$REPO" >/dev/null 2>&1
check "install: a foreign FIRST name installs none of the five" 0 "$(count_marked "$REPO")"

# --- the install is a rename, not a write over the live path -----------------
# `cp` truncates before it writes, and a prefix of the tripwire carries the
# marker and exits 0 — the self-certifying fail-open two sections up. Now that
# check-hooks installs from inside a running pre-push, concurrent installs are
# the designed steady state, so the window has to be closed rather than bounded.
# A rename replaces the directory entry: the inode a reader already opened is the
# whole old file, and a reader arriving later gets the whole new one. Measured on
# the inode, because that is what distinguishes the two implementations — a copy
# over the live path keeps it.

REPO="$TMP/install-atomic"
make_repo "$REPO"
run_installer "$REPO" >/dev/null 2>&1
ino_before="$(stat -c %i "$(hooks_dir "$REPO")/pre-commit")"
run_installer "$REPO" >/dev/null 2>&1
ino_after="$(stat -c %i "$(hooks_dir "$REPO")/pre-commit")"
if [ "$ino_before" = "$ino_after" ]; then
    check "install: re-installing replaces the file by rename" renamed "written-in-place"
else
    check "install: re-installing replaces the file by rename" renamed renamed
fi
check "install: no temp file is left behind" 0 \
    "$(find "$(hooks_dir "$REPO")" -name '*.tmp.*' | wc -l)"
check "install: the installed copy still refuses" 1 \
    "$("$(hooks_dir "$REPO")/pre-commit" >/dev/null 2>&1; echo $?)"

# The installed copy has to be executable whatever mode the source carries. `cp`
# gives a newly created file the source's mode, so a 644 source installs a 644
# hook unless the installer sets the bit — and git SKIPS a non-executable hook
# without a word, which is a tripwire that installs, reports success and gates
# nothing. Driven through `check-hooks` so the installer under test is the
# fixture's own copy, whose source this row can chmod.
REPO="$TMP/install-mode"
make_repo "$REPO"
chmod 644 "$REPO/.githooks/hooks-drift-tripwire"
rm -f "$(hooks_dir "$REPO")"/{pre-commit,commit-msg,pre-push,post-checkout,post-merge}
check "install-mode fixture: the source is not executable" no \
    "$([ -x "$REPO/.githooks/hooks-drift-tripwire" ] && echo yes || echo no)"
run_check_hooks "$REPO"
check "install: a non-executable source still installs an executable hook" yes \
    "$([ -x "$(hooks_dir "$REPO")/pre-commit" ] && echo yes || echo no)"

# --- what check-hooks counts as ABSENT: both clauses of the predicate ---------
# `[ ! -x "$target" ] || ! grep -q <marker> "$target"` decides which names reach
# the self-heal. Every fixture above either deletes a file (so both clauses agree)
# or leaves all five real (so neither fires), which is why deleting either clause
# once left the whole suite green. The two fixtures below separate them: each is
# a state where exactly one clause is what bites.

# The -x clause. All five installed and marker-bearing at mode 644. git skips a
# non-executable hook without a word, so this is an install that passes a marker
# grep and gates nothing. Distinct from the `install-mode` rows above, which cover
# the INSTALLER setting the bit on a copy it makes from a 644 source; here the
# already-installed files are 644 and it is check-hooks that has to notice.
REPO="$TMP/ch-installed-644"
make_repo "$REPO"
chmod 644 "$(hooks_dir "$REPO")"/{pre-commit,commit-msg,pre-push,post-checkout,post-merge}
check "644 fixture: all five carry the marker (so the grep clause passes them)" 5 \
    "$(count_marked "$REPO")"
check "644 fixture: none of the five is executable" 0 \
    "$(find "$(hooks_dir "$REPO")" -maxdepth 1 -perm -u+x \
        \( -name pre-commit -o -name commit-msg -o -name pre-push \
           -o -name post-checkout -o -name post-merge \) | wc -l)"
run_check_hooks "$REPO"
check "check-hooks: a marker-bearing 644 install is HEALED, not refused" passed "$(verdict $?)"
check "check-hooks: the healed install is executable at all five names" 5 \
    "$(find "$(hooks_dir "$REPO")" -maxdepth 1 -perm -u+x \
        \( -name pre-commit -o -name commit-msg -o -name pre-push \
           -o -name post-checkout -o -name post-merge \) | wc -l)"
# The consequence, which is what the clause is for: measured with the clause
# deleted, check-hooks left the five at 644 and a drifted commit LANDED.
git -C "$REPO" config --unset core.hooksPath
check "check-hooks: a drifted commit after healing a 644 install is REFUSED" blocked \
    "$(try_commit "$REPO")"

# The marker-grep clause. All five present and executable, one of them a foreign
# script. It exits 1, so the blocking-arm behaviour loop below is satisfied by it
# and the grep is the only thing that can tell it from the tripwire. The
# `ch-foreign` fixture above does not reach this: there the other four are deleted,
# so the -x clause puts them in `absent` and the installer meets the squatter
# anyway.
SQUATTER="$TMP/foreign-blocker"
printf '#!/usr/bin/env bash\n# somebody else wrote this too\nexit 1\n' >"$SQUATTER"
chmod +x "$SQUATTER"
REPO="$TMP/ch-squatter-all-present"
make_repo "$REPO"
cp "$SQUATTER" "$(hooks_dir "$REPO")/pre-commit"
chmod +x "$(hooks_dir "$REPO")/pre-commit"
check "squatter fixture: all five are present and executable" 5 \
    "$(find "$(hooks_dir "$REPO")" -maxdepth 1 -perm -u+x \
        \( -name pre-commit -o -name commit-msg -o -name pre-push \
           -o -name post-checkout -o -name post-merge \) | wc -l)"
check "squatter fixture: the squatter exits non-zero (so the behaviour loop passes it)" 1 \
    "$("$(hooks_dir "$REPO")/pre-commit" >/dev/null 2>&1; echo $?)"
run_check_hooks "$REPO"
check "check-hooks: a foreign squatter among five present hooks is REFUSED" refused \
    "$(verdict $?)"
case "$CH_OUT" in
    *"not the drift tripwire:"*"/pre-commit"*)
        check "check-hooks: the refusal names the squatted pre-commit" yes yes ;;
    *) check "check-hooks: the refusal names the squatted pre-commit" yes no ;;
esac

# --- summary -----------------------------------------------------------------

printf '\n%d passed, %d failed\n' "$pass" "$fail"
[ "$fail" -eq 0 ]
