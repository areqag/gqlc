# Rescuing the work of a killed session

Sessions here end without warning. A quota wall or a stall watchdog takes a
citizen mid-bead, and what they had done is still on disk — on a branch nobody
will push, or on no branch at all.

You are reading this because you are the rescuer, and you are almost certainly
reading it in a hurry. So the shape of this playbook is: the one order that
matters, then the two questions, then the proxies that will lie to you.

## The one thing to do before anything else

**Build the picture before you create a single ref.**

The instinct on finding loose commits is to pin them somewhere safe. That
instinct is right, and doing it in the wrong order destroys the evidence you
were about to gather: `git log --all` reads everything under `refs/`, including
the refs you just made. Pin 48 candidates to `refs/rescue/<sha>` first and every
one of them matches itself. The run then reports a tidy pile of duplicates and
you learn nothing — the only tell is a row reading `ff31a0e54 vs ff31a0e54`.

So either classify first and pin after, or build your comparison universe
explicitly and never from `--all`:

```bash
git for-each-ref --format='%(refname)' refs/heads refs/remotes
```

Pinning itself is still the right move once you are past that: `git update-ref
refs/rescue/<sha> <sha>` is instant, invisible to `git branch`, and makes the
set gc-proof while you think. **Delete the pins when you are done** — left
behind, they poison `git log --all` for the next rescuer too.

## Where the work is, and it depends on the seat

A seat is either a linked worktree of the shared checkout or its own clone, and
the roster converts one seat at a time — so yours may be either today.

That distinction changes where a killed session's commits are visible. A clone's
refs are its own, so a single `git log --all` in the shared checkout no longer
finds them; you walk the seats themselves, `git -C ../gqlc-seat-<s> log --all`
per seat. Their directories are still there to read.

The citizen-protocol paragraph on clones is the authority for that and for the
`git rev-parse --path-format=absolute --git-common-dir` test that tells you
which kind you are looking at. Read it there rather than trusting a copy here.

Also read the dead session's bead: `bd show <id>` notes routinely carry the
worktree path, the HEAD and the rescue path, written by the citizen before the
kill. That is cheaper than any search.

## Question 1 — did it already land?

Ask this **first**, every time. Most "stranded" work is not stranded, and the
cost of getting it wrong is a rescue brief whose central premise is false: the
recipient spends its opening act disproving you, and you have burned someone
else's context to do it.

The answer is always a **content** test. Ask GitHub, then diff:

```bash
gh pr list --state all --head <branch>                          # never-opened vs opened-and-merged
gh pr view <N> --json state,mergeCommit,headRefOid              # and does your tip match its head?
base=$(git merge-base <mergeCommit> <branch>)
files=$(git diff --name-only "$base" <branch>)
git diff --name-only <branch> <mergeCommit> -- $files           # empty means landed
```

**Restrict the diff to the files that branch touched.** The whole-tree diff is
master's own advancement since the branch was cut — measured 2026-08-22 at 495
and 953 changed lines on two branches that had landed completely — and it reads
exactly like hundreds of stranded lines. It will talk you out of a safe cleanup.

The details, the measurement and the full sequence live in the bd memory
`squash-landed-check-restrict-to-touched-files`. Read it before a large cleanup.

## Question 2 — it did not land, so find it

Now the loose commits. `git fsck --no-reflogs --dangling` is the raw set; expect
it to be mostly residue. On the one full sweep run here (2026-08-18), 225
dangling commits yielded 48 survivors of the cheap filters and **all 48 were
residue** — the only 3 real orphans that day were found by a citizen reading a
specific worktree, not by the sweep. Calibrate your expectations accordingly,
and do not talk yourself into a find.

The cheap filters, in order:

1. Age within three days, and `git branch -a --contains <sha>` empty. (This also
   excludes anything already rescued onto a branch.)
2. Drop merge commits (`%p` word-count > 1) and stash/WIP subjects (`WIP*`,
   `On *`, `tmp*`, `probe*`, `index on*`). Probe merges and stashes dominate:
   this step alone took 62 candidates to 48.
3. For each survivor, take its added lines of real length (≥45 chars, from
   `git show --format= --unified=0`) and `git grep -qF` each one against **all
   branch tips**.

**Test by content, never by subject, and never against master alone.** Both
naive spellings fail, in opposite and equally costly directions:

- **By subject → a false "landed".** A squash merge retitles, so a commit that
  landed is absent from master's log under its own subject. This is the
  dangerous direction: it discards real work.
- **Against `origin/master` only → a false "stranded".** Work on an *open* PR is
  correctly absent from master. One candidate matched 0 of 7 lines against
  master and looked like the strongest find in the set; it was a pre-rebase twin
  of a commit sitting on the head of an open PR.

Grouping by subject is still useful, as long as you never let it deliver the
verdict. Bucket each survivor into `TWIN-identical` (a same-subject commit on
some branch, and `git patch-id --stable` agrees — pure residue), `TWIN-differs`
(same subject, different patch: a superseded pre-amend draft), or `UNIQUE` (no
same-subject commit anywhere). The sweep here ran 24 / 16 / 8. Only `UNIQUE` is
worth reading — and all 8 of those turned out to have their content on a branch
already, under a reworded subject, which is exactly why the content test and not
the bucket is what closes the question.

## The proxies that will lie to you

This repo squash-merges and auto-deletes the head branch. That single
configuration defeats every cheap "is it merged?" test:

| you might reach for | why it lies |
| --- | --- |
| `git ls-remote --heads origin <branch>` | empty — the branch was auto-deleted **on merge** |
| `git merge-base --is-ancestor <sha> master` | NO — the squash made a new commit, so no tip survives |
| subject match against master's log | the squash retitled it |
| the commit body | squash keeps only the PR **title**; every commit body is deleted |

The last one has a consequence beyond triage: a correction written into a commit
body does not reach master. See `squash-merge-erases-commit-bodies` for the
durable carriers (the code, the PR page, the bead notes) and treat a commit
message as the non-durable fourth.

## Before you dispatch anyone

**Verify the rescue is still needed.** `git grep` the rescued commit's added
lines against the target head first. Two commits scored 0 of 10 against their
targets, and that is what justified the cherry-picks — had they scored high, the
fixers would have re-applied work already present and generated conflicts for
nothing.

And when you brief the rescuer, give them the content evidence, not the branch
name. A confidently wrong brief costs more than no brief.

## Related

- Bead notes on the dead session's bead — the cheapest source, read it first.
- `bd memories squash-landed-check-restrict-to-touched-files` — the did-it-land
  sequence in full.
- `bd memories absent-remote-branch-not-stranded` — why branch existence and
  ancestry are both meaningless here.
- `bd memories squash-merge-erases-commit-bodies` — the durable carriers.
- `kingdom/brain/playbooks/citizen-protocol.md` — clones vs worktrees, and what
  a seat can see.
