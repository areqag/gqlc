#!/usr/bin/env bash
# The git environment a test suite must not inherit. Source it, do not run it:
#
#     source "$(cd "$(dirname "$0")/.." && pwd)/git-env-sandbox.sh"
#
# git exports GIT_DIR, GIT_WORK_TREE and GIT_INDEX_FILE to every hook it runs,
# and this repo runs its own suites from `.githooks/pre-push` via `just test`.
# Those variables beat both the cwd and `git -C <dir>`, so under a hook a
# suite's throwaway repo is not the repo its git commands reach. Measured:
# with GIT_DIR exported, `git -C fixture config user.name fixture` writes the
# SHARED repo's config and leaves the fixture's own config empty, and
# `git -C fixture commit` lands the commit in the shared repo.
#
# That is how `fixture <fixture@example.invalid>` came to author two citizens'
# commits on two different branches (gqlc-7iea): one suite's fixture identity,
# written into the config every linked worktree in the town reads. A suite is
# the last place anyone looks for a writer to the repo it is gating.
#
# Sourcing this IS a convention to remember, and it did not used to be:
# .githooks/tests/git-env-sandbox-test.sh ran every suite in .githooks/tests/
# under a poisoned environment and failed the one that inherited it, so a
# forgotten source was caught rather than remembered. PR #1595 deleted that
# suite and the suites it policed. The one caller left is the justfile's
# `check-beads-export`, which protects itself by refusing when this file is
# missing — but that catches a missing FILE, not a caller who never sources it.
# A new caller that forgets simply reads the wrong repository, quietly, which is
# the failure this file was written for (bd gqlc-u2nim).
unset "${!GIT_@}"
