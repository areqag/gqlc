# Contributing

## Setup after cloning

Run `just init` once after cloning. This configures git to use the project's
`.githooks/` directory (`git config core.hooksPath .githooks`), which activates
a pre-commit hook that blocks accidental direct commits to `master` or `main`.
The same guard is wired into Claude Code as a `PreToolUse` hook so AI agents are
blocked at the conversation level too. The recipe is idempotent — running it
multiple times is safe.

`just test` and `just doctor` **fail** when `core.hooksPath` has drifted away
from `.githooks`. That drift deactivates every hook in `.githooks/` at once and,
because linked worktrees share one `.git/config`, does so in every worktree
simultaneously. CI cannot see local git config, so this is where drift surfaces.
Repair with `just init`, which is idempotent.

Those recipes only run when someone runs them, so `.githooks/claude-pre-bash`
runs a stricter version of the check on every Bash tool call. Drifted here means
a repo that *ships* `.githooks/` and whose `core.hooksPath` does not point live
at it; a repo without that directory is not distinguishable from any unrelated
repo and neither half fires. Given that, the two halves key on different
directories: while the hook's **own** working directory is inside a drifted repo
it warns on any command it does not refuse outright, and it refuses
`git commit`, `git merge`, `git pull` and `git push` whose **effective target**
repo is drifted — including a `git -C <drifted>` issued from a healthy directory
— until the config is repaired. Those four were picked by measuring which
subcommands run one of the four hooks this repo ships (`pre-commit`,
`commit-msg`, `pre-push`, `post-merge`); `revert`, `cherry-pick`, `rebase` and
`am` ran none of them. Some `merge` and `pull` *shapes* run none either — a
rebasing pull of an already-diverged branch, for one — but the subcommand does
not determine the shape: `pull` alone spans invocations running two of those
hooks, one, and none, depending on its options and on remote state at run time.
The gate keys on the subcommand anyway and over-refuses that last group.
Stricter because `just doctor` compares the configured value and nothing else — with
`core.hooksPath` set to `.githooks` but the directory holding only `*.sample`
files, or a hook file left non-executable, `just doctor` prints `ok` and exits
0 where the Bash hook refuses. In a plain terminal `just doctor` is what you
have, and on those two states it reports nothing. The Bash hook has silent
states of its own: a cwd outside any repo; a repo that ships no `.githooks/` at
all, per the definition above, even with `core.hooksPath` unset; and a
*non-gated* command aimed at a drifted repo from a healthy directory
(`git -C <drifted> status`), because the warn half keys on the hook's own
directory — resolved up to that directory's repo root, so a subdirectory warns
too — and never follows the command's target. Each is a pinned row in
`.githooks/tests/claude-pre-bash-test.sh`.

## Development

Everything runs through `just`, locally and in CI — same recipes, same pinned
tool versions (see the `justfile`). There is nothing else to install or keep
up to date: every lint/fmt recipe first verifies the pinned golangci-lint in
the gitignored `.bin/` and re-provisions it on any mismatch (~3s), so a
version bump in the justfile propagates to every machine automatically.

- `just test` — build + full suite (unit, golden snapshots, godog TCK)
- `just lint` / `just lint-new` — full static analysis / only the diff vs master
- `just fmt` / `just fmt-check` — gofumpt + gci, fix vs check
- `just tidy-check` — go.mod/go.sum drift
- `just vuln` — govulncheck, over both modules and the `codegen_live` battery

Write new tests in an **external** test package (`package foo_test`) wherever
the test does not need unexported access. govulncheck does not analyse the
in-package test variant, so a dependency only an in-package test imports is
outside its call graph and a *called* vulnerability there does not fail the
gate (ADR 0026). `just vuln` prints how much of the root module is currently in
that blind spot; bd gqlc-m5rc is shrinking it, and `just test-codegen-fence`
already holds the nested `test/data/codegen` module at zero.

The hooks split the same checks by budget: pre-commit blocks master commits and
gates formatting (sub-second); pre-push runs the suite and diff-scoped lint
(seconds); CI is the authoritative gate (`lint`, `test`, `tidy`, `actionlint`
and `govulncheck` are required to merge — the vulnerability job reports on
every PR but only scans when a go.mod/go.sum changed, in either module).
