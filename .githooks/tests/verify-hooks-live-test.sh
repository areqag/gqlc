#!/usr/bin/env bash
# Unit tests for .githooks/verify-hooks-live — the arm that asks git to RUN a
# hook instead of asking the config what it says (bd gqlc-o13d).
#
# The rows that give this file its reason to exist are the "env shape" ones:
# a repository whose .git/config says '.githooks', with
# GIT_CONFIG_PARAMETERS overriding it, in which a real `git commit` runs NO hook
# and lands. Every value-reading check in this repository that consults the file
# is green in that state, and `just init` writes the file, reads it back and
# reports success while nothing is gated. So each of the dead fixtures below is
# paired with a REAL COMMIT, and the row asserts what the commit did — a suite
# that only asserted the verifier's exit code would be asserting agreement
# between two readings of the same string.
#
# Every fixture is a throwaway repo under $TMP. Nothing here writes
# core.hooksPath, or anything else, into the repository this suite runs in; the
# enclosing value is recorded at start and compared at the end, which is what
# says so.
#
# This file is registered nowhere. `just test-hooks` globs .githooks/tests/, so
# creating it is the whole of adding it.
#
# Run via: just test-hooks
set -u

# Under a git hook (pre-push via `just test`) GIT_DIR and friends leak in and
# would redirect every fixture command at the parent repo.
# shellcheck source=../git-env-sandbox.sh disable=SC1091
source "$(cd "$(dirname "$0")/.." && pwd)/git-env-sandbox.sh"

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
VERIFIER="$REPO_ROOT/.githooks/verify-hooks-live"
PROBE_SRC="$REPO_ROOT/.githooks/gqlc-liveness-probe"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

OUTER_BEFORE="$(git -C "$REPO_ROOT" config --get core.hooksPath 2>/dev/null || echo '<unset>')"

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

for f in "$VERIFIER" "$PROBE_SRC"; do
    if [ ! -x "$f" ]; then
        echo "FAIL - $f is missing or not executable, so no row in this suite can run" >&2
        exit 1
    fi
done

# A repository shaped like this one: the REAL probe and the REAL verifier, plus
# a pre-commit that leaves a witness so a row can ask what a commit actually
# did. Seeded before any hook is wired, so the seed needs no --no-verify.
make_repo() {
    local repo="$1"
    git init -q -b master "$repo"
    git -C "$repo" config user.email t@t.invalid
    git -C "$repo" config user.name t
    mkdir -p "$repo/.githooks"
    cp "$PROBE_SRC" "$repo/.githooks/gqlc-liveness-probe"
    cp "$VERIFIER" "$repo/.githooks/verify-hooks-live"
    chmod +x "$repo/.githooks/gqlc-liveness-probe" "$repo/.githooks/verify-hooks-live"
    cat >"$repo/.githooks/pre-commit" <<'EOF'
#!/usr/bin/env bash
echo ran >"$(git rev-parse --path-format=absolute --git-common-dir)/precommit-witness"
exit 0
EOF
    chmod +x "$repo/.githooks/pre-commit"
    echo seed >"$repo/seed.txt"
    git -C "$repo" add -A
    git -C "$repo" commit -q -m seed
}

common_dir() { git -C "$1" rev-parse --path-format=absolute --git-common-dir; }

# Runs the fixture's own copy of the verifier from inside the fixture, under any
# environment the caller exported, leaving its output in VERIFY_OUT and its
# verdict in VERIFY_RC. verdict() renders the latter.
#
# Split in two, and not a function that echoes live|dead, because a function
# called inside $( ) runs in a subshell: the message rows read VERIFY_OUT and
# would have read the parent's stale copy of it forever. That is what the first
# run of this suite did — four message rows red against a verifier that was
# printing the right thing.
#
# Run from INSIDE the fixture, not with -C: core.hooksPath is relative, git
# resolves it against the working tree the hook would run in, and verifying from
# somewhere else would measure a different lookup than the one a commit uses.
VERIFY_OUT=''
VERIFY_RC=0
verify() {
    local repo="$1"
    VERIFY_OUT="$(cd "$repo" && ./.githooks/verify-hooks-live 2>&1)"
    VERIFY_RC=$?
}
verdict() { if [ "$VERIFY_RC" -eq 0 ]; then echo live; else echo dead; fi; }

# Attempts a real commit in $1 and echoes ran|silent by whether the repo's own
# pre-commit left its witness. This is the ground truth every verify() row is
# scored against.
commit_witness() {
    local repo="$1" witness
    witness="$(common_dir "$repo")/precommit-witness"
    rm -f "$witness"
    echo "$RANDOM.$$" >"$repo/change.txt"
    git -C "$repo" add -A >/dev/null 2>&1
    (cd "$repo" && git commit -q -m change >/dev/null 2>&1)
    if [ -e "$witness" ]; then echo ran; else echo silent; fi
}

# --- healthy -----------------------------------------------------------------

REPO="$TMP/healthy"
make_repo "$REPO"
git -C "$REPO" config core.hooksPath .githooks
check "healthy: a real commit runs the repo's pre-commit" ran "$(commit_witness "$REPO")"
verify "$REPO"
check "healthy: the verifier reports live" live "$(verdict)"
check "healthy: the verifier says nothing while it is live" '' "$VERIFY_OUT"

# --- the environment shape, which is the whole point -------------------------
#
# .git/config is CORRECT and untouched. The override rides in on
# GIT_CONFIG_PARAMETERS, which is how `git -c core.hooksPath=...` reaches its
# descendants, and git honours it over the file.

REPO="$TMP/env"
make_repo "$REPO"
git -C "$REPO" config core.hooksPath .githooks
export GIT_CONFIG_PARAMETERS="'core.hooksPath=/dev/null'"
check "env shape: .git/config still reads .githooks" .githooks \
    "$(git -C "$REPO" config --file "$REPO/.git/config" --get core.hooksPath)"
check "env shape: a real commit runs NO hook and lands anyway" silent "$(commit_witness "$REPO")"
verify "$REPO"
check "env shape: the verifier reports dead" dead "$(verdict)"
check "env shape: it names the ENVIRONMENT rather than advising just init" yes \
    "$(case "$VERIFY_OUT" in *"came from the ENVIRONMENT"*) echo yes ;; *) echo no ;; esac)"
check "env shape: it says plainly that just init cannot repair it" yes \
    "$(case "$VERIFY_OUT" in *"'just init' CANNOT repair this"*) echo yes ;; *) echo no ;; esac)"
unset GIT_CONFIG_PARAMETERS

# --- /dev/null in the config file, the signature this bead was filed on -------

REPO="$TMP/devnull"
make_repo "$REPO"
git -C "$REPO" config core.hooksPath /dev/null
check "/dev/null: a real commit runs no hook" silent "$(commit_witness "$REPO")"
verify "$REPO"
check "/dev/null: the verifier reports dead" dead "$(verdict)"
check "/dev/null: it names just init as the repair" yes \
    "$(case "$VERIFY_OUT" in *"Repair: just init"*) echo yes ;; *) echo no ;; esac)"

# --- drift to the default hooks directory ------------------------------------

REPO="$TMP/default-dir"
make_repo "$REPO"
git -C "$REPO" config core.hooksPath "$(common_dir "$REPO")/hooks"
check "default-dir drift: a real commit runs no hook" silent "$(commit_witness "$REPO")"
verify "$REPO"
check "default-dir drift: the verifier reports dead" dead "$(verdict)"

# --- core.hooksPath correct, hook left non-executable ------------------------
#
# The state `just check-hooks` documents as one it exits 0 on: the value check
# compares a string and there is nothing wrong with the string.

REPO="$TMP/noexec"
make_repo "$REPO"
git -C "$REPO" config core.hooksPath .githooks
chmod -x "$REPO/.githooks/gqlc-liveness-probe" "$REPO/.githooks/pre-commit"
check "noexec: a real commit runs no hook" silent "$(commit_witness "$REPO")"
verify "$REPO"
check "noexec: the verifier reports dead" dead "$(verdict)"
check "noexec: it reports the value as correct and the probe as the problem" yes \
    "$(case "$VERIFY_OUT" in *"MISSING or not executable"*) echo yes ;; *) echo no ;; esac)"

# --- run from a LINKED worktree ----------------------------------------------
#
# core.hooksPath is relative and git resolves it against the working tree the
# hook runs in, so a linked worktree has to prove itself from its own tree. This
# is also the shape every seat is in: `just test` runs there, under pre-push,
# with GIT_DIR exported.

REPO="$TMP/wt-main"
LINKED="$TMP/wt-linked"
make_repo "$REPO"
git -C "$REPO" config core.hooksPath .githooks
git -C "$REPO" worktree add -q --detach "$LINKED" >/dev/null 2>&1
if [ ! -d "$LINKED" ]; then
    fail=$((fail + 1)); printf 'FAIL - %s\n' "linked-worktree fixture: could not create the worktree"
else
    cp -R "$REPO/.githooks" "$LINKED/.githooks"
    verify "$LINKED"
    check "linked worktree: the verifier reports live from inside it" live "$(verdict)"
    # GIT_DIR is what git exports into a hook's environment, and it is the
    # mechanism behind two incidents on this bead. A verifier that answered
    # about the wrong repository under it would be worse than no verifier.
    LINKED_GITDIR="$(git -C "$LINKED" rev-parse --path-format=absolute --git-dir)"
    export GIT_DIR="$LINKED_GITDIR" GIT_INDEX_FILE="$LINKED_GITDIR/index"
    verify "$LINKED"
    unset GIT_DIR GIT_INDEX_FILE
    check "linked worktree: still live with GIT_DIR exported, as under pre-push" live "$(verdict)"
    git -C "$REPO" config core.hooksPath /dev/null
    verify "$LINKED"
    check "linked worktree: a sibling's clobber of the SHARED config reaches it" dead "$(verdict)"
fi

# --- containment -------------------------------------------------------------

check "containment: this repository's own core.hooksPath is untouched" "$OUTER_BEFORE" \
    "$(git -C "$REPO_ROOT" config --get core.hooksPath 2>/dev/null || echo '<unset>')"

printf '\n%d passed, %d failed\n' "$pass" "$fail"
[ "$fail" -eq 0 ]
