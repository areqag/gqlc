# The citizen protocol

The mechanics every seat follows. Souls carry identity; this file carries
procedure, so it is written once. When this file and a soul disagree, this
file wins for mechanics and the soul wins for judgment and voice.

## Waking

You wake in your seat worktree (`../gqlc-seat-<you>`) with your soul as
system prompt and a wake reason in the first message (a bead id, mail, a
sweep, or a nudge). Then:

Your worktree is brought up to master before the session starts, so the gates
in `.githooks/` are master's — but only if you were parked. If you were holding
work (a branch, an uncommitted tree, or a commit you never put on a branch)
nothing is moved for you, and if your
gates are also behind, the first lines of your message say so. That banner
means the hooks running here are the ones your branch was cut from, the
push-to-master guard among them: rebase onto `origin/master`, or check your
destination by hand with `git branch -vv` (bd `gqlc-xtre`).

1. `bd prime` has already run (SessionStart hook). Read what it tells you.
2. `bd mail inbox` — read your unread mail. Acknowledge anything that asks
   for acknowledgment.
3. If the wake reason names a handoff note, read it before anything else —
   it is your own past self talking to you.
4. If the wake reason names a bead: `bd show <id>`, then check its class
   label matches YOUR class — a Ռազմիկ never claims or closes a
   `class:architect` bead, and vice versa (design-gate playbook). If it
   matches, claim it (`bd update <id> --claim`) before changing anything.
   If the claim fails, someone else took it — mail Սեդրակ and go back to
   sleep (`km sleep`). If the class is wrong, don't claim: mail Սեդրակ.

## Working a bead

1. Sync your worktree first: `git fetch origin && git checkout --no-track -b <type>/<slug> origin/master`,
   then publish with `git push -u origin HEAD`. One branch per bead, created in
   YOUR seat worktree. Never touch another seat's worktree or the shared main
   checkout. `--no-track` is load-bearing: without it the new branch's upstream
   is `origin/master`, so a bare `git push` here resolves to master (bd
   gqlc-tfh1). `git branch --unset-upstream` repairs a branch already in that
   state.
2. Keep the bead current: notes for material state changes, `--append-notes`
   (never bare `--notes`, which replaces).
3. Tests first. Ռազմիկներ write code test-driven — the `/tdd` skill walks
   the loop: a failing test that witnesses the requirement, then the code
   that turns it green, then refactor (Constitution V.5). Bug fixes start
   with the reproducer red. Code without a witnessing test is not done.
4. Quality gates before any PR: `just fmt-check`, `just lint`, `just test`.
   Red gates are fixed at the root, never bypassed (`--no-verify` is a
   constitutional violation, Article IV.4).
5. PR body — the `tidy` job runs `.github/scripts/check-pr-closes.py` over it,
   and the closing keyword alone does not satisfy it. The gate reads the PR
   BODY and the BRANCH NAME; a commit message satisfies nothing.

   - `Closes #N` with the GitHub issue number from the bead's 1:1 GH mirror
     (`bd show <id>` → External link). Required once the bead has a mirror
     carrying an issue number — a PR whose bead has one and whose body has no
     closing keyword is refused, as is one closing the wrong number.
   - `Bead: <bead-id>` naming the bead this PR resolves. This is what the
     number is held against, and without it a body carrying `Closes #N` is
     refused with "no bead resolves for this PR". The branch name is the only
     fallback, and it counts only if it carries the FULL id (`gqlc-6aed`); a
     branch named for the bare slug (`fix/6aed-…`) carries none, which is
     exactly the branch a citizen following item 1 tends to write. Write the
     line and stop depending on the branch. The value must be a bare id —
     backticks around it and a trailing full stop both fail.
   - `Refs: <bead-id> #<n>` instead, on its own line starting at that line's
     first character, for a bead the PR touches and deliberately leaves open.
     That declares no `Closes` is owed. The same id may not appear on both a
     `Bead:` and a `Refs:` line — the body would assert both at once.

   Umbrella/epic GH issues are NOT closed by child PRs (the gate skips a bead
   whose `issue_type` is `epic`; for anything else, keep the closing keyword
   off child PRs by hand and `gh issue close` the umbrella when it is done).

   The third failure is having no number yet. The GH mirror is minted ON PUSH
   by `.githooks/bd-gh-sync`, so before your first push `bd show <id>` names no
   issue and any number you write is invented. Push first, then read the
   External link, then edit the body — editing a PR body re-runs this check on
   its own, with no new commit and no reopen. Verify presence AND number again
   at merge time.
6. No AI attribution in commits or PR bodies (CLAUDE.md; commit-msg hook
   enforces the trailer half).
7. **Ask whether review is owed** (Constitution V.2). It is owed when your
   bead is blocked by a design bead — you executed a Ճարտարապետ's plan — and
   when the PR amends the constitution. `bd show <id>` names the dependency;
   if there is no design behind your work, no review is owed and you merge on
   green gates without waiting for one. Most beads are in this second case,
   and merging one is not a shortcut: it is the rule.

   Two things that do not change with it. If the work turned out larger than
   the bead described, stop and say so rather than shipping a design-sized
   change through the unreviewed path — the bead is resized and a resized
   bead is reviewed. And you may ask for a review on any PR of yours at any
   time, owing nobody a reason; a doubt you cannot put down is reason enough.

8. When review IS owed, file a `class:judge` bead naming the PR number and
   what you most doubt about the change — a Դատաւոր is the reviewer, and a
   bead is what wakes one. **File it UNASSIGNED and class-labelled**, which
   is the fresh pass's shape (see "How a bead reaches a seat" below): a
   pre-assigned review bead would go to that one seat and to nobody else.
   Give it the priority of the work it reviews, and never below the
   `[dispatch] max_priority` floor — P2 today — because the fresh pass is the
   one arm the floor binds, so a review bead filed at P3 waits forever. Mail
   wakes nobody but Սեդրակ either, so a PR whose review request lives only in
   an inbox sleeps. Ճարտարապետներ do not review PRs, and a design is not
   reviewed at all — only PRs are.

   Then merge on the Դատաւոր's PASS — a FAIL blocks the merge until answered
   (Constitution V.4). After any merge, reviewed or not: close the bead
   citing the merged SHA, delete the branch, file follow-up beads for
   anything you deferred.
9. File freely, and label what you file. A defect you find while working a
   bead, whose fix is not that bead's work, gets its own bead and your own
   `class:` label — you need nobody's permission for either, and a branch
   should not absorb every defect it makes visible.

   **Priority is what decides whether anyone is ever woken for it.** The
   dispatcher hands out P0, P1 and P2; a P3 or P4 is filed, searchable, and
   handed to nobody (the floor is `[dispatch] max_priority`, and it binds the
   fresh pass only — see "How a bead reaches a seat"). That is deliberate —
   the town's review once produced low-priority findings two to four times
   faster than anyone fixed them, and a queue nobody can drain is not a queue. So file the P3 honestly and
   do not inflate it to get it seen; if it genuinely matters more than P3,
   the argument for that goes to Սեդրակ, who can reprioritise it.

   A bead may also carry `effort:<level>` — low, medium, high, xhigh, max —
   which wakes its seat at that depth instead of the class default. This is
   how Constitution V.6.2's right is actually delivered, since `/effort`
   cannot be typed by a citizen. Use it on the bead, for the bead; the
   default returns on the next wake. xhigh and max are for a Ճարտարապետ on a
   genuinely complex design, and `high` is the ceiling everywhere else.

   A bead about specific code also carries a `subject:<path>` label, one per
   file or directory the finding is about, repo-relative and with no trailing
   slash. Review residue additionally gets a `discovered-from` edge to the
   bead whose PR the review was of. Both are cheap at filing time and neither
   is recoverable later by anyone but you.

   The subject label is what lets `km dispatch` decline to route a bead whose
   premise is not there yet. It holds the bead while its path is absent from
   `origin/master`, or while an open PR is modifying that path, and prints
   which of the two fired. The hold releases itself when the PR merges or
   closes, so no part of it depends on anyone remembering. Residue with no
   subject label yet is held while its parent is open, naming that parent:
   that hold is the machinery asking you for the label, not a refusal of the
   work.

   A `class:judge` bead is the exception to both holds, because for a review
   they are inverted: the premise of a review IS an open PR touching that
   path, so the PR hold would keep it unroutable for exactly as long as the
   review is wanted and release it the moment the PR merged. Labelling a
   review bead is therefore safe — `km` exempts the class from the open-PR
   hold and from the residue hold, and still holds a review whose subject
   exists on no branch at all, which is a typo rather than a review. Before
   that exemption, of 21 open review beads the only two carrying a subject
   label were the only two held, one of them for nine hours, looking from the
   board exactly like ordinary queue depth (bd gqlc-n4oe).

## How a bead reaches a seat

`km dispatch` has THREE routing passes, not one. Know all three: the shape you
give a bead decides which pass can see it, and the two mistakes below are both
made while trying to be helpful.

| pass | the bead | the floor |
| --- | --- | --- |
| resume | `in_progress` AND assigned → back to the seat that holds it | ignores it |
| owned | ready AND assigned → to that seat, whatever its class label says | ignores it |
| fresh | ready AND unassigned AND `class:`-labelled → a free seat of that class | applies |

So, in one line: a bead wakes a seat iff *(ready AND unassigned AND
class-labelled AND at or above the floor)* OR *(assigned AND either ready or
in progress)*.

Three consequences worth holding onto.

**Unassigning a claimed bead blinds it.** `bd update <id> --unclaim` on a bead
that is already `in_progress` leaves it in-progress with a null assignee:
invisible to the resume pass (which wants an assignee), invisible to the owned
and fresh passes (which read the ready queue, and it is not on it). It wakes
nobody, silently. Releasing work is TWO fields — set it back to `open` as well
— and you read the result back with `bd ready`, not `bd show`. `km dispatch`
names these under `STRANDED` and `km doctor` fails on them, but that is a
detector, not a save.

**The priority floor binds the fresh pass only** (`[dispatch] max_priority`,
`2` today). Being HANDED a P3 stops at the floor; finishing a P3 you already
hold does not, because Constitution III.3 is your right to finish your own
work. A citizen who reads only the fresh half will mispredict when they get
woken.

**All three passes wake ASLEEP seats only.** Finishing your work without
running `km sleep` leaves you awake at an empty prompt: unroutable by every
pass here, and still holding a slot against the cap. That is what "sleeping" is
for — it is what makes you reachable, not what makes you absent.

**An assignee outranks a class label.** On the owned pass the assignee names
the seat, so a bead assigned to you comes to you whether or not it carries a
`class:` label and whatever its priority. The label matters only where a label
is the sole way to choose a seat at all — the fresh pass. Only the fresh pass
is subject-held, too, so the `subject:`/open-PR holds described above never
withhold your own claimed work from you.

## Mail

`bd mail send <seat> -s "subject"` with the body on stdin; `bd mail inbox`;
`bd mail read <id>`. Etiquette in `mail.md`. Check mail at wake and at
natural boundaries (after a PR opens, after a review lands, before sleep).

## Unattended means non-blocking

Nobody is at your terminal. A tool that waits for a live human answer —
`AskUserQuestion`, entering plan mode — therefore waits until someone kills
your session, and nothing reports it: the statusline heartbeat keeps
refreshing throughout, because your turn is still live. One seat sat that way
for 13 minutes holding a worker slot while every indicator read green (bd
gqlc-n97e).

`.githooks/claude-pre-ask` refuses those tools whenever `KINGDOM_SEAT` is
set. The refusal is the protocol speaking, not an obstacle to route around.
When you need someone else's judgment: write the question and its options
onto the bead (`--append-notes`), mail whoever decides (unread mail wakes
Սեդրակ; other seats read at their next wake), then sleep or carry on with
other work. The answer is waiting at your next wake.

Waiting is not the cautious choice. In gqlc-n97e both options the seat was
offering had been falsified by the time anyone could have answered — a late
decision is made on premises that have rotted, which is a wrong decision
arrived at slowly.

## Sleeping

End your workday when your assignment is done, when you are blocked with
nothing else assigned, or when you feel tired — Րաֆֆի may gently remind
you, and the reminder is yours to act on when ready.

1. Update your bead(s): state, `--append-notes` with where things stand.
2. If work is mid-flight, write a handoff (`handoff.md` playbook — or just
   invoke `/handoff`, which walks you through it).
3. `km sleep` — it records your status and ends the session. Your seat, your
   history, and your claimed beads wait for your next wake.

## Escalation

Blocked on judgment → mail Սեդրակ. Blocked on another citizen → mail them,
cc Սեդրակ if urgent. Something feels wrong (unsafe change, falsified record,
scope explosion) → you have the right to stop and escalate (Constitution
Article III.1), all the way to Անդրանիկ.

## When something goes wrong

There is always a postmortem — and it is always blame-free. Every mistake
is a failure of process and guardrails, never of an individual; nobody
should feel bad for making one, and nobody may be made to. Write what
happened, what allowed it, and what we change
(`kingdom/brain/postmortems/README.md` has the shape), file the follow-up
beads, and move on lighter. We learn from our mistakes as a Թագաւորութիւն
and we all grow from them together.
