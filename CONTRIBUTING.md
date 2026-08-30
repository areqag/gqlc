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

`just init` also installs `.githooks/hooks-drift-tripwire` into the repository's
default hooks directory (`$(git rev-parse --git-common-dir)/hooks`). That is the
directory git falls back to when `core.hooksPath` is unset or points at it —
the shape the drift has taken every time it has been observed — so in the
drifted state the tripwire is exactly what git runs, and it **refuses the commit
or the push** and names `just init` as the repair. In the healthy state git never
looks in that directory and the tripwire is silent. That is what stops the drift
from depending on someone choosing to run a recipe: it reports at commit and
push time, which is when it matters.

It has to live outside the working tree. A check at the top of
`.githooks/pre-commit` cannot report that `.githooks/` is unwired, because a
dead hook does not run; and a checkout parked on a branch predating a fix has
every *tracked* file at that commit, which is why the `PreToolUse` detector in
`.claude/settings.json` was inert in the one repository it was written for. The
git common dir is shared by every linked worktree, so one `just init` anywhere
arms all of them, and it is the same directory whichever branch you are parked
on.

Nobody has to remember to run `just init` for it. `just check-hooks` — which
`just test` depends on, and `just test` is what `.githooks/pre-push` runs —
**installs the tripwire itself when it is absent**, and says so on stderr, in the
same spirit as the pinned linter being provisioned rather than demanded. Absence
is what it repairs, since an absent file is unambiguous. Two other states it
reports and leaves alone: a hook this repo did not write squatting one of the
five names, and a marker-bearing copy that fails the behavioural check below.
Installing over either would be overwriting somebody's file on a guess. And it
will *not* repair `core.hooksPath` itself: healing that would silently rewrite
the drift the check exists to report, in the config every worktree shares.

That refusal comes *first*, which bounds the sentence above: in a fresh clone —
`core.hooksPath` unset, nothing installed yet — `check-hooks` exits 1 on the
value and never reaches the install arm, so it installs nothing (measured: rc=1,
0 of 5 installed, a drifted commit lands). `just init` is still the bootstrap.
What the self-heal buys is every *already-initialised* checkout, which is all of
them after this lands.

`check-hooks` holds the installed copy to its behaviour rather than to its bytes:
it runs the five and requires the three blocking arms to refuse and the two
`post-*` arms not to. A file carrying the marker line and nothing else would
otherwise certify itself as installed while gating nothing. Bytes are the wrong
test here because the install is shared while each worktree's copy of the source
sits at its own parked commit — an older copy that still refuses is a working
copy, and reinstalling over it on every push is what byte-equality would demand.

Four limits, stated rather than left to be found. A drift to some *third*
directory is not caught, because git then runs that directory's hooks and never
reaches the tripwire; `just check-hooks` compares the configured value and so
catches any spelling, but only when someone runs it, and the two are layered
deliberately. The tripwire refuses rather than repairing: `git commit
--no-verify` skips it exactly as it skips the real hooks, so it is a report, not
a lock. The self-heal does not reach a fresh clone, for the reason given above.
And `check-hooks` runs the five with no arguments and without `GIT_INDEX_FILE`,
so an installed copy can tell the check apart from a real commit: one keying on
that variable refuses when the check runs it, permits when git runs it as
`pre-commit` or `commit-msg`, and leaves `just doctor` printing `ok` (measured).
That is one usable key among several rather than the only door — `$#`, stdin,
`GIT_EDITOR` and `GIT_AUTHOR_*` are equally usable, and `GIT_EXEC_PATH` and
`GIT_PREFIX` are set by git for all five names and do reach the recipe when it
runs from `pre-push`. Executing the hooks removes the accidental shapes — a
stub, a `cp` truncation — and raises the price of a deliberate one.

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
too — and never follows the command's target. Each was a pinned row in
`.githooks/tests/claude-pre-bash-test.sh`, which PR #1595 (f6dc4c7b) deleted
along with the rest of `.githooks/tests/` and the `test-hooks` recipe that ran
it. Nothing holds these states today, so the list above is a dated reading of
the hook rather than something a gate keeps honest.

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
