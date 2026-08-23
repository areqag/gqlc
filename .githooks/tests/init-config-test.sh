#!/usr/bin/env bash
# Unit tests for the git-config half of `just init`.
#
# `just init` does two things: it writes core.hooksPath, and it installs
# .githooks/hooks-drift-tripwire into the default hooks directory. The second
# half has rows — .githooks/tests/hooks-drift-tripwire-test.sh covers the
# install path, the absent-install case, the foreign-squatter refusal and the
# atomicity properties (bd gqlc-4thl, PR #1044). The first half had none, so
# CONTRIBUTING.md's idempotence promise and the claim that init is the bootstrap
# for a fresh clone rested on reading the recipe (bd gqlc-b6s5).
#
# These rows run the REAL recipe against throwaway repositories. A row that only
# read the justfile would be asserting the same thing the recipe says about
# itself, and the defect this half can carry — a value written somewhere every
# linked worktree does not see, or a second run appending rather than replacing —
# is not visible in the text.
#
# THE HAZARD, and why every invocation goes through run_init. `just init` WRITES
# core.hooksPath into whatever repository it resolves to. A --working-directory
# that failed to take would point it at the repository this suite is running in,
# whose config is shared by every linked worktree — the blast radius gqlc-4thl is
# about. So the fixture's git resolution is asserted to land inside $TMP before
# each run, the run is fatal if it does not, and the enclosing repository's own
# value is recorded at start and compared at the end.
#
# This file is registered nowhere. `just test-hooks` globs .githooks/tests/, so
# creating it is the whole of adding it (bd gqlc-234l).
#
# Run via: just test-hooks
set -u

# When run under a git hook (pre-push via `just test`), GIT_DIR and friends leak
# in and would redirect the fixture's git commands at the parent repo.
# Through the SHARED line rather than a private copy of it (bd gqlc-o9wz).
# shellcheck source=../git-env-sandbox.sh disable=SC1091
source "$(cd "$(dirname "$0")/.." && pwd)/git-env-sandbox.sh"

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
JUSTFILE="$REPO_ROOT/justfile"
TRIPWIRE="$REPO_ROOT/.githooks/hooks-drift-tripwire"
INSTALLER="$REPO_ROOT/.githooks/install-hooks-drift-tripwire"
PROBE="$REPO_ROOT/.githooks/gqlc-liveness-probe"
VERIFIER="$REPO_ROOT/.githooks/verify-hooks-live"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

# `just init` is the subject, so a missing `just` is fatal rather than skipped:
# a suite that quietly drops every row it has is the shape of failure the
# discovery in `test-hooks` exists to refuse.
if ! command -v just >/dev/null 2>&1; then
    echo "FAIL - just is not on PATH, so no row in this suite can run" >&2
    exit 1
fi

# The enclosing repository's value, read before anything runs. Compared again at
# the end: if a fixture escaped, this is what says so, and it says so even for an
# escape that happened to write the same value the developer already had.
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

# A fixture shaped like a fresh clone of this repository: a .githooks/ holding a
# live hook and the tripwire installer, and NO core.hooksPath — which is the
# state `just init` exists to move a clone out of.
#
# The seed commit is made before any hook is wired, so it needs no --no-verify.
make_repo() {
    local repo="$1"
    git init -q -b master "$repo"
    git -C "$repo" config user.email t@t.invalid
    git -C "$repo" config user.name t

    mkdir -p "$repo/.githooks"
    # Records that it ran, so a row can witness that core.hooksPath took effect
    # rather than inferring it from the string git config reads back. A value
    # written to a config git does not consult reads back perfectly well.
    cat >"$repo/.githooks/pre-commit" <<'EOF'
#!/usr/bin/env bash
echo ran >"$(git rev-parse --git-common-dir)/githooks-precommit-witness"
exit 0
EOF
    chmod +x "$repo/.githooks/pre-commit"

    # `just init` runs .githooks/install-hooks-drift-tripwire by a path relative
    # to its working directory, so the fixture needs its own copies.
    cp "$TRIPWIRE" "$repo/.githooks/hooks-drift-tripwire"
    cp "$INSTALLER" "$repo/.githooks/install-hooks-drift-tripwire"
    chmod +x "$repo/.githooks/hooks-drift-tripwire" "$repo/.githooks/install-hooks-drift-tripwire"

    # `just init` now verifies its own repair by asking git to run a hook, by the
    # same relative paths, so the fixture needs those two as well.
    cp "$PROBE" "$repo/.githooks/gqlc-liveness-probe"
    cp "$VERIFIER" "$repo/.githooks/verify-hooks-live"
    chmod +x "$repo/.githooks/gqlc-liveness-probe" "$repo/.githooks/verify-hooks-live"

    echo seed >"$repo/seed.txt"
    git -C "$repo" add -A
    git -C "$repo" commit -q -m seed
}

# The absolute git common directory for repo $1.
common_dir() {
    git -C "$1" rev-parse --path-format=absolute --git-common-dir
}

# Runs the repo's REAL `just init` against fixture $1, leaving its exit status
# in INIT_STATUS. Its combined output is captured rather than let through, and
# reprinted only on a non-zero status: a green run prints two lines per fixture
# and would bury the rows, while a failing one is unreadable without them.
#
# The containment check is fatal, not a return code. A row expecting a repaired
# value would go green on a bail-out that never entered the recipe — the shape
# the tripwire suite documents around its own 99 — and here the cost of getting
# it wrong is a write to a config fourteen seats share, so the suite stops
# instead of scoring the row.
INIT_STATUS=0
run_init() {
    local repo="$1" common out
    common="$(common_dir "$repo")"
    case "$common" in
        "$TMP"/*) ;;
        *)
            echo "FAIL - fixture escaped \$TMP ($common); refusing to run 'just init'," >&2
            echo "       which would write core.hooksPath into a shared config." >&2
            exit 1 ;;
    esac
    out="$(just --justfile "$JUSTFILE" --working-directory "$repo" init 2>&1)"
    INIT_STATUS=$?
    if [ "$INIT_STATUS" -ne 0 ]; then
        printf 'just init (%s) exited %d:\n%s\n' "$repo" "$INIT_STATUS" "$out" >&2
    fi
}

# The configured value in repo $1, or <unset>.
hooks_path() {
    git -C "$1" config --get core.hooksPath 2>/dev/null || echo '<unset>'
}

# How many values core.hooksPath carries in repo $1. A repair that appended
# rather than replaced leaves two, and `--get` would still read one of them.
hooks_path_count() {
    git -C "$1" config --get-all core.hooksPath 2>/dev/null | wc -l | tr -d ' '
}

# Attempts a commit in $1 and echoes ran|silent, by whether the repo's own
# .githooks/pre-commit left its witness.
precommit_witness() {
    local repo="$1" witness
    witness="$(common_dir "$repo")/githooks-precommit-witness"
    rm -f "$witness"
    echo "$RANDOM.$$" >"$repo/change.txt"
    git -C "$repo" add -A
    git -C "$repo" commit -q -m change >/dev/null 2>&1
    if [ -e "$witness" ]; then echo ran; else echo silent; fi
}

# --- a fresh clone: core.hooksPath unset ------------------------------------

REPO="$TMP/fresh"
make_repo "$REPO"

check "fresh fixture: core.hooksPath is unset before init" '<unset>' "$(hooks_path "$REPO")"
check "fresh fixture: the repo's own pre-commit does NOT run before init" silent \
    "$(precommit_witness "$REPO")"

run_init "$REPO"
check "init: exits 0 on a fresh clone" 0 "$INIT_STATUS"
check "init: core.hooksPath reads .githooks afterwards" .githooks "$(hooks_path "$REPO")"

# The behavioural half. The row above asserts what git config reads back, which
# is what a write to a file git never consults would also produce. This one
# asserts that the repo's own hook actually executes on a commit, which is the
# claim `just init` is made for.
check "init: the repo's own pre-commit runs on a commit afterwards" ran \
    "$(precommit_witness "$REPO")"

# --- the drifted shape -------------------------------------------------------

# The drift bd gqlc-5fm actually saw: an absolute path at the default hooks
# directory, which exists but holds only .sample files, so every hook is dead
# while an existence test passes.
REPO="$TMP/drifted"
make_repo "$REPO"
git -C "$REPO" config core.hooksPath "$(common_dir "$REPO")/hooks"
check "drifted fixture: core.hooksPath points at the default hooks directory" yes \
    "$(case "$(hooks_path "$REPO")" in */hooks) echo yes ;; *) echo no ;; esac)"
check "drifted fixture: the repo's own pre-commit does NOT run while drifted" silent \
    "$(precommit_witness "$REPO")"

run_init "$REPO"
check "init: repairs a drifted core.hooksPath to .githooks" .githooks "$(hooks_path "$REPO")"
# `git config <name> <value>` replaces; `--add` appends. If init ever grew the
# second spelling, the row above would still read `.githooks` — git returns the
# last value — while every consumer reading --get-all saw two, and `git config
# --get` on a multivalued key is an error in some git versions.
check "init: the repair replaces the drifted value rather than adding to it" 1 \
    "$(hooks_path_count "$REPO")"
check "init: the repo's own pre-commit runs after the repair" ran \
    "$(precommit_witness "$REPO")"

# --- idempotence -------------------------------------------------------------

# CONTRIBUTING.md promises this in as many words: "The recipe is idempotent —
# running it multiple times is safe". Asserted over the whole local config
# rather than over core.hooksPath alone, so a second run that added some other
# key is caught too.
REPO="$TMP/twice"
make_repo "$REPO"
run_init "$REPO"
check "idempotence fixture: the first init exits 0" 0 "$INIT_STATUS"
BEFORE="$(git -C "$REPO" config --local --list | sort)"
run_init "$REPO"
check "init: a second run exits 0" 0 "$INIT_STATUS"
AFTER="$(git -C "$REPO" config --local --list | sort)"
check "init: a second run changes nothing in the local config" same \
    "$(if [ "$BEFORE" = "$AFTER" ]; then echo same; else echo "changed:$(diff <(echo "$BEFORE") <(echo "$AFTER") | tr '\n' ' ')"; fi)"
check "init: core.hooksPath still carries exactly one value after two runs" 1 \
    "$(hooks_path_count "$REPO")"

# --- run from a linked worktree ----------------------------------------------

# The blast-radius half of gqlc-4thl. core.hooksPath lives in the config every
# linked worktree shares, and that is the whole reason one careless session
# disables the hooks for all of them. The property wanted here is the other
# direction: an init run from INSIDE a linked worktree has to land in the shared
# config, so that repairing from one worktree repairs them all. A value written
# to a worktree-local config would read back correctly in the worktree that
# wrote it and leave every other worktree still drifted.
#
# extensions.worktreeConfig is left unset, which is the state of this repository
# and of a fresh clone. With it unset git has nowhere worktree-local to put the
# value, so what this row really pins is that init does not turn the extension
# on in order to find one.
REPO="$TMP/wt-main"
LINKED="$TMP/wt-linked"
make_repo "$REPO"
git -C "$REPO" worktree add -q --detach "$LINKED" >/dev/null 2>&1
if [ ! -d "$LINKED" ]; then
    fail=$((fail + 1)); printf 'FAIL - %s\n' "linked-worktree fixture: could not create the worktree"
else
    cp -R "$REPO/.githooks" "$LINKED/.githooks"
    check "linked-worktree fixture: it shares the main repo's config file" same \
        "$(if [ "$(common_dir "$LINKED")" = "$(common_dir "$REPO")" ]; then echo same; else echo different; fi)"
    check "linked-worktree fixture: core.hooksPath is unset before init" '<unset>' \
        "$(hooks_path "$LINKED")"

    run_init "$LINKED"
    check "init from a linked worktree: exits 0" 0 "$INIT_STATUS"
    check "init from a linked worktree: the value is visible in the MAIN worktree" .githooks \
        "$(hooks_path "$REPO")"
    check "init from a linked worktree: it does not enable extensions.worktreeConfig" '<unset>' \
        "$(git -C "$LINKED" config --get extensions.worktreeConfig 2>/dev/null || echo '<unset>')"
    check "init from a linked worktree: it writes no worktree-local config file" 0 \
        "$(find "$(common_dir "$REPO")/worktrees" -name 'config.worktree' 2>/dev/null | wc -l | tr -d ' ')"
fi

# --- init must not report success it cannot witness --------------------------
#
# bd gqlc-o13d, the P0: `just init` printed "git hooks activated" from the fact
# that the write returned, and the write is not the claim. The state below is the
# one where those come apart with no race and no writer to catch — the value is
# overridden from the ENVIRONMENT, so .git/config is correct before init runs,
# correct after it, reads back '.githooks', and no hook runs.
#
# Scored on the recipe's EXIT STATUS and on a real commit, not on its output
# text: a recipe that printed a warning and exited 0 would leave the reader with
# exactly the belief this bead is about.
REPO="$TMP/env-override"
make_repo "$REPO"
git -C "$REPO" config core.hooksPath .githooks
export GIT_CONFIG_PARAMETERS="'core.hooksPath=/dev/null'"
check "env-override fixture: the config FILE is already correct" .githooks \
    "$(git -C "$REPO" config --file "$REPO/.git/config" --get core.hooksPath)"
check "env-override fixture: no hook runs on a real commit anyway" silent \
    "$(precommit_witness "$REPO")"
run_init "$REPO"
check "init: REFUSES rather than reporting success it cannot witness" 1 "$INIT_STATUS"
unset GIT_CONFIG_PARAMETERS
# The same fixture with the override gone is repaired and says so, which is what
# stops the row above from being satisfied by a recipe that refuses everything.
run_init "$REPO"
check "init: exits 0 again once the override is cleared" 0 "$INIT_STATUS"
check "init: the repo's own pre-commit runs once the override is cleared" ran \
    "$(precommit_witness "$REPO")"

# --- containment -------------------------------------------------------------

# Every `just init` above ran with --working-directory. This is what says so.
# Without it, a --working-directory that silently stopped taking would move all
# the writes into the repository this suite runs in, and every row above would
# still be green — they read the value back through the same misdirection that
# wrote it.
check "containment: this repository's own core.hooksPath is untouched" "$OUTER_BEFORE" \
    "$(git -C "$REPO_ROOT" config --get core.hooksPath 2>/dev/null || echo '<unset>')"

# --- summary -----------------------------------------------------------------

printf '\n%d passed, %d failed\n' "$pass" "$fail"
[ "$fail" -eq 0 ]
