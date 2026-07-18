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
git worktree add ../<repo>-<bead-slug> -b <branch-name> origin/master
cd ../<repo>-<bead-slug>
```

After the PR merges and beads are closed:

```bash
git worktree remove ../<repo>-<bead-slug>
```

**Why:** two agent sessions sharing one cwd share one HEAD, one index, one working tree. Whichever ran `git checkout` last wins — the other session's `git status` / `git log --oneline master..HEAD` silently report the wrong branch. Staged files bleed across branches. `MERGE_HEAD` / `CHERRY_PICK_HEAD` state confuses hook logic. All observed 2026-07-18 (bd `gqlc-2fi`).

Nest sibling worktrees at a sibling path (`../<repo>-<slug>`), never nested inside the main cwd — nesting breaks Go tooling paths and creates stale LSP diagnostics.

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
