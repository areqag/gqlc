# Project Instructions for AI Agents

This file provides instructions and context for AI coding agents working on this project.

## This repository runs Claude Code in bypassPermissions

The checked-in `.claude/settings.json` sets `permissions.defaultMode` to
`bypassPermissions`, so a Claude Code session opened here does not ask before
running a tool. Every clone inherits it — the file is tracked, and it is not
scoped to this town's seats. An interactive session still meets Claude Code's
workspace-trust dialog before project settings apply; whether that dialog gates
this particular setting is undocumented and we have not measured it. A run that
gets no dialog at all, and so inherits the mode silently, is a wider set than
just `claude -p`: its `--help` says the dialog is skipped "via -p, or when
stdout is not a TTY, e.g. piped or redirected output".

It is set because a seat resumed by hand is launched as `claude --resume <uuid>`,
which replays no `--permission-mode` flag, and an unattended agent that stops on
a permission prompt waits until someone kills it (bd `gqlc-keaz`). Under this
mode `PreToolUse` hooks still run and still block on exit 2, so the refusals in
`.githooks/` are unaffected.

To opt out, set your own `permissions.defaultMode` in `.claude/settings.local.json`,
which is untracked and outranks the project file, or take the mode away from the
whole host with `permissions.disableBypassPermissionsMode: "disable"` in managed
settings, which the shipped binary honours above every file here.

<!-- BEGIN BEADS INTEGRATION v:1 profile:minimal hash:7510c1e2 -->
## Beads Issue Tracker

This project uses **bd (beads)** for issue tracking. Run `bd prime` to see full workflow context and commands.

### Quick Reference

```bash
bd ready              # Find available work
bd show <id>          # View issue details
bd update <id> --claim  # Claim work
bd close <id>         # Complete work
```

### Rules

- Use `bd` for ALL task tracking — do NOT use TodoWrite, TaskCreate, or markdown TODO lists
- Run `bd prime` for detailed command reference and session close protocol
- Use `bd remember` for persistent knowledge — do NOT use MEMORY.md files

**Architecture in one line:** issues live in a local Dolt DB; sync uses `refs/dolt/data` on your git remote; `.beads/issues.jsonl` is a passive export. See https://github.com/gastownhall/beads/blob/main/docs/SYNC_CONCEPTS.md for details and anti-patterns.

## Session Completion

**When ending a work session**, you MUST complete ALL steps below. Work is NOT complete until `git push` succeeds.

**MANDATORY WORKFLOW:**

1. **File issues for remaining work** - Create issues for anything that needs follow-up
2. **Run quality gates** (if code changed) - Tests, linters, builds
3. **Update issue status** - Close finished work, update in-progress items
4. **PUSH TO REMOTE** - This is MANDATORY:
   ```bash
   git pull --rebase
   git push
   git status  # MUST show "up to date with origin"
   ```
5. **Clean up** - Prune remote branches. Do NOT run `git stash clear`: every worktree of this repo shares ONE stash namespace, so it deletes other sessions' uncommitted work
6. **Verify** - All changes committed AND pushed
7. **Hand off** - Provide context for next session

**CRITICAL RULES:**
- Work is NOT complete until `git push` succeeds
- NEVER stop before pushing - that leaves work stranded locally
- NEVER say "ready to push when you are" - YOU must push
- If push fails, resolve and retry until it succeeds
<!-- END BEADS INTEGRATION -->

## Querying the bd ledger

`bd list` applies a **status filter** and a **row cap**, and they are two
separate defaults. `-n 0` (`--limit 0`) disables only the second. It reads as
the unfilter and is not one.

- **When you mean every bead, write `bd list --all --json -n 0`.** Measured
  2026-08-23: `bd list -n 0 --json` returned 261 rows and silently omitted 514
  closed ones. This flipped a real diagnosis — an audit reported that PR #1122
  had never had a review bead when it had had two, both closed, and *absent* and
  *closed* call for opposite repairs.
- **`--status open` means the literal status `open`,** not "not closed". It
  excludes `in_progress` and `blocked`.
- **State the status set and the row cap explicitly at every scripted call
  site.** The row cap is disclosed on **stderr** even under `--json`, and every
  scripted call site here redirects `2>/dev/null`, so a silent stderr is not
  evidence that nothing was capped. The status filter is not disclosed at all.
- `bd show <id> --json` returns an **array**; use `.[0]`. It does resolve closed
  beads, so it is a safe way to test whether an id exists.

Full measurements, the deployed-bd contract, and an audit of this repository's
own call sites: [docs/bd-ledger-queries.md](docs/bd-ledger-queries.md).

## Writing to the bd ledger

- **`✓ Updated issue` means accepted, not changed.** An update that moves nothing
  prints the same line. Read the field back — for routability, with `bd ready`.
- **A refused `bd update` prints nothing on stdout.** It exits non-zero, puts the
  reason on stderr alone, and discards the *whole* command, so one bad flag takes
  the valid fields beside it. Drop stderr and ignore the exit status and a
  refusal is indistinguishable from a silent no-op. `-l` is the common trigger:
  labels on `update` are `--add-label` / `--set-labels`; `-l` is `bd create`'s.
- **Name one bead per `bd update` whose success you check by exit status.** Given
  several ids it is best-effort — it exits 0 having skipped the ones it could not
  resolve. `bd close` differs: it refuses the whole command and writes nothing.
- **A bead whose serialised record passes ~65535 bytes never accepts another
  `bd update`.** Every write first records the *whole previous bead* as JSON in a
  `TEXT` column, so the size of your write is irrelevant and shrinking the notes
  does not recover it. Reads stay healthy, so the bead looks fine on every board;
  the only symptom is on the stderr of a write. `bd close` still works, but a
  closed oversized bead can never be reopened. One bead of 1533 was over the line
  on 2026-09-03 and the next largest was 42151, so this is a hazard for
  heavily-worked beads rather than a fleet-wide one.

Measured 2026-08-24 against bd 1.0.4 and 1.2.2, and the size ceiling on
2026-09-03, with the falsifiers and this repository's write call-site audit:
[docs/bd-ledger-writes.md](docs/bd-ledger-writes.md).

## Working directory

**No session modifies files in the shared repo cwd** (`/home/antranig/Developer/gqlc/gqlc`). It is for read-only research work (grep, read, `bd show`, `git log`) — the moment intent shifts to modification (any `bd create/close/update`, any file write, any branch creation), you work somewhere else. WHERE depends on who you are, and there are two answers.

**If you are a seat of the Թագաւորութիւն** (`KINGDOM_SEAT` is set), you already have one: `../gqlc-seat-<you>`, and it is PERMANENT. Do not create another and do not remove it; cut a branch per bead inside it. Under bd `gqlc-w5bh` a seat's directory becomes its own full clone rather than a linked worktree of this checkout, one seat at a time, so a given seat may still be either — the recipe is identical for both, but the shared-namespace warnings below (stash, refs, local config) stop describing a seat once it converts. Your recipe is `kingdom/brain/playbooks/citizen-protocol.md`, "Working a bead" step 1, which is authoritative for seats — the rest of this section is deliberately not a second copy of it, so the two cannot drift apart again (bd `gqlc-wuax`).

**Everyone else** — a human, or a one-off agent like a `/tdd` run or a factory session — has no seat worktree, so make an ephemeral sibling one per session, at bead-claim time, before any modification:

```bash
git worktree add --no-track -b <branch-name> ../<repo>-<bead-slug> origin/master
cd ../<repo>-<bead-slug>
```

**`<branch-name>` must contain the bead id, in the form `<type>/<bead-id>-<slug>`** — for example `fix/gqlc-uz3c-prepush-bead-warning`. Not the bare suffix (`fix/sync-drift-x98l`): CI's reader is anchored on the `gqlc-` prefix and cannot see one without it.

That is not cosmetic, and it is the half of this recipe that was wrong until 2026-08-23 (bd `gqlc-uz3c`). CI's `tidy` job runs `.github/scripts/check-pr-closes.py`, which resolves the PR's bead from a `Bead:` / `Refs:` line in the body and falls back to a bead id in the **branch** name. The recipe above put the slug in the DIRECTORY and left the branch descriptive, so the fallback never fired: measured over the 60 most recently merged PRs on 2026-08-23, 57 head branches carried no `gqlc-` id. A body that then says `Closes #N` fails the gate, under a job whose name says nothing about bead ids — three PRs in one night each burned about an hour on it and one lane died before diagnosing it. (It was worse then: `tidy` still carried `needs:` children, so lint/test/codegen-fence reported `skipping` and the check table read like a run in progress. PR #1323 removed those edges the same night, so today it is one red cell.) `.githooks/pre-push` now warns when it publishes a branch whose name carries no id — advisory only, since it cannot see a PR body that does not exist yet.

If you are already on a branch without one, do not re-cut it: put a `Bead: <bead-id>` line in the PR body. Editing the body re-runs the gate with no new commit and no new push.

The first push publishes the branch and sets its upstream:

```bash
git push -u origin HEAD
```

**`--no-track` is load-bearing.** Without it, `git worktree add -b <branch> origin/master` sets the new branch's upstream to `origin/master`, so a bare `git push` there resolves to **master**, not to the branch. Measured 2026-08-19 in `../gqlc-agt0`: on `fix/vuln-unplaced-stdlib-assertions-covered`, `git rev-parse --abbrev-ref --symbolic-full-name '@{u}'` returned `origin/master`. A sweep of all 21 sibling worktrees later the same day found 4 in that state — agt0 by then not among them, a successful `git push -u` having moved its upstream to its own branch. What stopped the push was `push.default=simple` refusing a name mismatch — and its message does not mention master; it offers `git push origin HEAD:master` as the first remedy, which is the accident. `git branch --unset-upstream` repairs a worktree already in that state. bd `gqlc-tfh1`.

After the PR merges and beads are closed:

```bash
git worktree remove ../<repo>-<bead-slug>
```

**That removal is best-effort, and the leak it fails to prevent is expected.** It runs only if the session survives to run it, and sessions here are killed without warning by quota walls and stall watchdogs, so it is frequently never reached: 70 stale worktrees were removed by hand once, and a later sweep found 93 registered worktrees among 621 stale `/tmp` directories (bd `gqlc-osuz`). So do not read the line above as the mechanism that keeps the disk clean. The mechanisms are `git worktree prune` for the registrations, and `just tmp-report` / `just tmp-reap apply` for the directories — run them when you find yourself in the shared checkout, not only when you leaked one. The per-seat model has none of this failure mode, because there is nothing to reap; that is the argument for it, and the reason a seat must not adopt the recipe above.

**Why a worktree at all:** two agent sessions sharing one cwd share one HEAD, one index, one working tree. Whichever ran `git checkout` last wins — the other session's `git status` / `git log --oneline master..HEAD` silently report the wrong branch. Staged files bleed across branches. `MERGE_HEAD` / `CHERRY_PICK_HEAD` state confuses hook logic. All observed 2026-07-18 (bd `gqlc-2fi`).

Nest sibling worktrees at a sibling path (`../<repo>-<slug>`), never nested inside the main cwd — nesting breaks Go tooling paths and creates stale LSP diagnostics.

## Scratch space

The worktree rule isolates the tree. It does not isolate `/tmp`, which every
session shares. Two rules, both mechanical:

**1. Every scratch path is unique per invocation.** Allocate it, never spell it:

```bash
scratch=$(mktemp -d)                    # a directory
orig=$(mktemp)                          # a single file
```

Never a fixed path — no `/tmp/km.orig`, no `/tmp/probe.jsonl`, no
`/tmp/verdict.md`. A chosen name is not yours: sixteen seats doing the same kind
of work pick the same obvious name, and the loser of the race reads the winner's
bytes.

Measured 2026-08-22 (bd `gqlc-b8gd`): two sessions ran the same mutation ritual
over `kingdom/bin/km` — copy aside to `/tmp/km.orig`, mutate, copy back. The
second write landed on the first, and the restore copied one session's
uncommitted work into the other's worktree. Both branches shared a base, so it
applied cleanly with no conflict to raise an alarm; `git diff` was the only thing
between that and a commit containing another session's work. It also falsified a
mutation battery: a row read KILLED because the feature under test had been
overwritten out of the tree, not because the mutation killed it. A no-op mutation
reporting KILLED is a row vouching for a guard nobody tested.

**2. Delete it when you are done**, on the failure path too:

```bash
scratch=$(mktemp -d); trap 'rm -rf "$scratch"' EXIT
```

`/tmp` here is a 16 GiB tmpfs capped at 1048576 inodes. On 2026-08-22 it hit 99%
of that cap and began refusing writes town-wide with ENOSPC while `df -h` still
showed 5.9 G free — a third of the inode budget was held by abandoned agent
scratch directories nobody ever reaped (bd `gqlc-vze6`). If you ever see ENOSPC
with free space, run `df -i` before anything else. `just tmp-report` shows what
is holding the filesystem in both currencies; `just tmp-reap apply` reclaims what
it can prove is abandoned. Neither is a substitute for cleaning up your own.

Both rules still hold the older one they replace: scratch goes outside the tree
you are working on or judging, never a transient file dropped into it.

## The shared stash namespace

The worktree rule isolates the tree, and the section above says `/tmp` is not
isolated either. Neither is the stash. It lives in the common git dir, so it is
ONE namespace shared by the main checkout and every seat worktree at once.

- `git stash list` in your worktree lists **every** worktree's stashes. It is a
  read and it is safe; read it freely.
- `git stash clear` from any worktree deletes **all** of them, with no
  confirmation and nothing to recover from.

So: **do not stash.** Commit to your own branch instead — there is nothing the
shared stash namespace does for you that a commit does not do more safely. If
you must drop an entry, read its subject first and drop it by index only once
the branch it names carries your own bead id. A bare `git stash drop` takes
`stash@{0}`, which is whoever stashed most recently.

Measured 2026-08-29, both halves. Անահիտ found the hazard: `git rev-parse
--git-common-dir` returns the shared `.git` from every seat, and in her own list
`stash@{0}` read `WIP on fix/gqlc-m5rc-root-in-package-tests` — another seat's
branch, on a bead that was in progress — then vanished as its owner popped it,
which is how she knew it was live work rather than an abandoned entry. Anyone
running the cleanup step in that window would have destroyed it silently, with a
green cleanup conscience. Confirmed from seat `ayg` by writing rather than
watching: one stash pushed in `../gqlc-seat-ayg` was then listed verbatim by
`git -C ../gqlc stash list` and by `git -C ../gqlc-seat-anahit stash list`, and
`refs/stash` with its reflog exists only under the common `.git` — the
per-worktree `refs/` directory is empty (bd `gqlc-96lf0`).

The other half of that cleanup sentence, `prune remote branches`, is over a
shared namespace too, since remote-tracking refs also live in the common dir. It
is kept because what it deletes is a cache of the remote rather than work: all
16 refs `git remote prune --dry-run origin` offered to drop that day were head
branches of merged PRs. Do not reach for `git merge-base --is-ancestor` to check
that yourself — it called all 16 unmerged, because a squash merge leaves no
branch tip on master. `gh pr list --head <branch> --state all` is the test that
answers.

## PR & GitHub issue hygiene

Beads IDs alone don't auto-close linked GitHub issues on merge — GitHub only recognises `Closes #N` (or `Fixes` / `Resolves`, case-insensitive) with the **GH issue number**.

- **Direct 1:1 bd↔GH issue:** put `Closes #N` in the PR body. Find N via `bd show <id>` (External link).
- **Umbrella / epic GH issues** (multi-stage tracking): child PRs must NOT `Close` them. Either the final PR of the epic writes `Closes #N`, or run `gh issue close N` manually when the beads mirror closes.
- **A `Bead: <id>` line does not satisfy this.** It answers a different question
  (which bead holds the PR) and CI's `tidy` gate reads it only to find the bead —
  then, if that bead has a GH mirror, `.github/scripts/check-pr-closes.py` still
  demands `Closes #N` with the mirror's number. A body carrying `Bead:` alone
  fails `tidy` (measured 2026-08-29, PR #1614).
- **The mirror lives in JSON field `external_ref`, not `external_link`.**
  `jq '.[0].external_link'` on a misnamed key prints `null` with exit 0, which
  reads exactly like "no mirror, so no Closes needed" — the same probe that
  should catch the miss confirms it instead. The plain renderer's `External:`
  line and `jq '.[0].external_ref'` agree; trust those.

See `bd memories pr-body-closes-gh-issue` for the full note and the incidents that motivated it.

## AI attribution

Do not add AI-authorship attribution to commits or PR bodies:

- **No `Co-Authored-By: Claude ...` trailer** on commits. AI use is a given here; explicit disclosure is noise, and a bot co-author line pollutes GitHub's contributor list on the repo.
- **No `🤖 Generated with [Claude Code]` footer** in PR bodies.

The commit-trailer half is enforced at commit time by `.githooks/commit-msg`, which rejects any `Co-Authored-By` value mentioning `claude` or an `@anthropic.com` email. The PR-body footer half cannot be hook-enforced (PR bodies bypass local git); follow the rule.

## The Թագաւորութիւն (agent society)

This repo is also worked by an autonomous agent society — seats with souls,
file-based mail, a beads-routed dispatcher, and a constitution. Charter and
machinery live in `kingdom/` (start with `kingdom/README.md`). If you are a
seat (`KINGDOM_SEAT` is set), your procedure is
`kingdom/brain/playbooks/citizen-protocol.md`; all Armenian prose in this
repo is Western Armenian, classical orthography. `just kingdom` shows the
town at a glance.

**This model supersedes the earlier ephemeral-team pattern.** Previously,
work was executed by spawning per-bead agent teams (implementer +
adversarial reviewer, the "Carmack + Linus" shape). Under the kingdom,
work is taken by the persistent seats instead: Ճարտարապետներ design and hand
off, Ռազմիկներ execute test-driven (`/tdd`), and the Դատաւորներ review
(`/thermo-nuclear-code-quality-review`). Review follows the design gate: a PR
is read by a judge if its bead was blocked by a design bead, if it amends the
constitution, or if a citizen asks — everything else merges on green gates
(Constitution V.2, `kingdom/brain/decisions/0003-the-restart.md`).
The disciplines carry over — tests first, adversarial review where it is
owed — the ephemeral instances do not. Humans do not block: citizens
decide, merge, and amend their own constitution.

## Build & Test

_Add your build and test commands here_

```bash
# Example:
# npm install
# npm test
```

## Architecture Overview

_Add a brief overview of your project architecture_

## Conventions & Patterns

_Add your project-specific conventions here_
