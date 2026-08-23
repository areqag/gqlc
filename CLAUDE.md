# Project Instructions for AI Agents

This file provides instructions and context for AI coding agents working on this project.

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
5. **Clean up** - Clear stashes, prune remote branches
6. **Verify** - All changes committed AND pushed
7. **Hand off** - Provide context for next session

**CRITICAL RULES:**
- Work is NOT complete until `git push` succeeds
- NEVER stop before pushing - that leaves work stranded locally
- NEVER say "ready to push when you are" - YOU must push
- If push fails, resolve and retry until it succeeds
<!-- END BEADS INTEGRATION -->

## Working directory

**Every session that will modify files runs in its own sibling git worktree.** The shared repo cwd (`/home/antranig/Developer/gqlc/gqlc`) is only for read-only research work (grep, read, `bd show`, `git log`) — the moment intent shifts to modification (any `bd create/close/update`, any file write, any branch creation), spin up a sibling worktree.

At bead-claim time, before any modification:

```bash
git worktree add --no-track -b <branch-name> ../<repo>-<bead-slug> origin/master
cd ../<repo>-<bead-slug>
```

The first push publishes the branch and sets its upstream:

```bash
git push -u origin HEAD
```

**`--no-track` is load-bearing.** Without it, `git worktree add -b <branch> origin/master` sets the new branch's upstream to `origin/master`, so a bare `git push` there resolves to **master**, not to the branch. Measured 2026-08-19 in `../gqlc-agt0`: on `fix/vuln-unplaced-stdlib-assertions-covered`, `git rev-parse --abbrev-ref --symbolic-full-name '@{u}'` returned `origin/master`. A sweep of all 21 sibling worktrees later the same day found 4 in that state — agt0 by then not among them, a successful `git push -u` having moved its upstream to its own branch. What stopped the push was `push.default=simple` refusing a name mismatch — and its message does not mention master; it offers `git push origin HEAD:master` as the first remedy, which is the accident. `git branch --unset-upstream` repairs a worktree already in that state. bd `gqlc-tfh1`.

After the PR merges and beads are closed:

```bash
git worktree remove ../<repo>-<bead-slug>
```

**Why:** two agent sessions sharing one cwd share one HEAD, one index, one working tree. Whichever ran `git checkout` last wins — the other session's `git status` / `git log --oneline master..HEAD` silently report the wrong branch. Staged files bleed across branches. `MERGE_HEAD` / `CHERRY_PICK_HEAD` state confuses hook logic. All observed 2026-07-18 (bd `gqlc-2fi`).

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

## PR & GitHub issue hygiene

Beads IDs alone don't auto-close linked GitHub issues on merge — GitHub only recognises `Closes #N` (or `Fixes` / `Resolves`, case-insensitive) with the **GH issue number**.

- **Direct 1:1 bd↔GH issue:** put `Closes #N` in the PR body. Find N via `bd show <id>` (External link).
- **Umbrella / epic GH issues** (multi-stage tracking): child PRs must NOT `Close` them. Either the final PR of the epic writes `Closes #N`, or run `gh issue close N` manually when the beads mirror closes.

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
off, Ռազմիկներ execute test-driven (`/tdd`), and the Դատաւորներ review —
every Ռազմիկ PR merges on one judge's PASS
(`/thermo-nuclear-code-quality-review`).
The disciplines carry over — tests first, adversarial review, merge on
PASS — the ephemeral instances do not. Humans do not block: citizens
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
