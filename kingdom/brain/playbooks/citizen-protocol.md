# The citizen protocol

The mechanics every seat follows. Souls carry identity; this file carries
procedure, so it is written once. When this file and a soul disagree, this
file wins for mechanics and the soul wins for judgment and voice.

## Waking

You wake in your seat worktree (`../gqlc-seat-<you>`) with your soul as
system prompt and a wake reason in the first message (a bead id, mail, a
sweep, or a nudge). Then:

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
5. PR body: `Closes #N` with the GitHub issue number when the bead has a 1:1
   GH mirror (`bd show <id>` → External link). Umbrella/epic GH issues are
   NOT closed by child PRs. Verify presence AND number at merge time.
6. No AI attribution in commits or PR bodies (CLAUDE.md; commit-msg hook
   enforces the trailer half).
7. Request review by filing a `class:judge` bead naming the PR number and what
   you most doubt about the change — a Դատաւոր is the reviewer, and a bead
   is what wakes one. **File it UNASSIGNED**: the dispatcher's fresh pass
   selects `.assignee == null` (`cmd_dispatch` in km) and its resume pass reads only
   `in_progress`, so a pre-assigned review bead is ready, labelled, and
   invisible to both — it wakes nobody, silently, at any cap level. Give it
   the priority of the work it reviews. Mail wakes nobody but Սեդրակ either,
   so a PR whose review request lives only in an inbox sleeps. Ճարտարապետներ
   do not review PRs, and a design is not reviewed at all — only PRs are.
8. Merge on a Դատաւոր's review PASS — a FAIL blocks the merge until answered
   (Constitution V.4). After merge: close the bead citing the merged SHA,
   delete the branch, file follow-up beads for anything you deferred.
9. File freely, and label what you file. A defect you find while working a
   bead, whose fix is not that bead's work, gets its own bead and your own
   `class:` label — you need nobody's permission for either, and a branch
   should not absorb every defect it makes visible.

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

## Mail

`bd mail send <seat> -s "subject"` with the body on stdin; `bd mail inbox`;
`bd mail read <id>`. Etiquette in `mail.md`. Check mail at wake and at
natural boundaries (after a PR opens, after a review lands, before sleep).

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
