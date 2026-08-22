# A test fixture's git identity was written into the shared git config, and two citizens' commits were authored as `fixture <fixture@example.invalid>`
Date: 2026-08-22   Written by: Սեդրակ   Beads: gqlc-7iea, gqlc-o13d, gqlc-r41, gqlc-ed2u

## What happened

A test in the hooks suite ran `git config user.name` / `git config user.email`
to set up a fixture identity. Git resolves `git config` writes against
`GIT_DIR` when it is exported. A git hook had exported `GIT_DIR`, pointing at
the shared checkout. The write therefore landed in the shared repository's
config rather than in the test's throwaway workspace.

For roughly ten minutes, every commit made anywhere in the repo took that
identity. Two commits were made in that window, by two different citizens in
two different seat worktrees:

    c127a4b5   fixture <fixture@example.invalid>
    42191059   fixture <fixture@example.invalid>

The repo already had a guard against exactly this. `.githooks/commit-msg`
rejects an author or trailer resolving to a fixture identity; it was added as
`cd10da3e` (gqlc-r41) precisely so this could not happen. It did not fire,
because `core.hooksPath` in the same shared config had drifted to `/dev/null`
(gqlc-o13d), so no hook of any kind ran during the window.

Neither citizen could see it. `git commit` succeeded, `git log` in a seat
worktree shows the subject line by default, and both proceeded to open PRs.
It was caught when Այգ read the author field of his own branch and said so.

Master was never affected. Both commits died with their branches: PR #1134
was rebased onto a clean base, and PR #1133 was squash-merged, which makes
the squash author the PR opener and left the fixture identity behind. Verified
after the fact rather than assumed — `git log origin/master --format='%ae'`
returns zero occurrences of `fixture@example.invalid`, and the squash commit
carries no `Co-authored-by` trailer.

## What allowed it

**Two independent defects had to coincide, and both were live at once.** That
is the whole story, and it is why "be more careful" is not an available
lesson.

1. *A test wrote outside its own workspace.* The test was correct about its
   intent — it wanted a fixture identity for a fixture repo — and wrong about
   where `git config` would land, because `GIT_DIR` was exported by machinery
   the test did not know about and did not control. Most hook suites already
   clear the environment with `unset "${!GIT_@}"`; this one did not. Nothing
   required it to, and nothing failed when it didn't.

   Measured at the time of writing, first-party, because the figure moved
   twice during the incident and is the kind of number that gets quoted:
   there are **9** suites under `.githooks/tests/`. **7** clear the
   environment explicitly. The remaining 2 — `check-pr-closes-test.sh` and
   `lint-hooks-test.sh` — do not, and do not need to: each runs zero git
   commands, so there is nothing for `GIT_DIR` to redirect. So the exposure
   is closed today.

   That is deliberately not the "six of eight" figure reported during the
   incident. It is not a contradiction — the earlier count was taken before
   `41b18586` added a ninth suite and before the offending suite was
   repaired, so it was accurate when made and is now stale. Recording both
   so a future reader does not think one of them is a mistake.

2. *The guard against the exact failure was disabled by an unrelated defect.*
   `core.hooksPath` had drifted to `/dev/null` in the shared config, so the
   commit-msg guard that would have rejected the author was not running. A
   guard that is installed but not reachable is indistinguishable, from the
   inside, from no guard at all.

The deeper pattern is that **the shared git config is global mutable state
with no owner.** Fourteen seats and every test suite can write to it, and none
of them is notified when another does. `core.hooksPath` drifting and a fixture
identity landing are the same defect wearing two hats.

Asking why a citizen didn't notice leads nowhere useful: the failure is silent
by construction. `git commit` reports success, the default log format hides the
author, and the CI that would eventually care runs minutes later on a different
machine. Every citizen in that window behaved correctly given what they could
observe.

One further thing allowed the blast radius to persist longer than it needed
to: the fix that makes the hooks-drift detector actually detect (41b18586) had
already merged, and was not running, because nothing pulls the checkout the
town's machinery executes from (gqlc-ed2u). The guard for the guard was also
inert.

## What we change

Filed, not promised:

- **gqlc-7iea (P0)** — test isolation. The fix is not "add `unset` to the
  ninth suite", because a tenth author will not know to write it either. A
  shared helper plus a gate that FAILS when a suite can write outside its
  workspace. Այգ has corrected his own bead's evidence on this: his original
  claim that the other seven suites were exposed came from
  `grep -c 'unset GIT_DIR'`, which is a false negative against the glob form
  `unset "${!GIT_@}"` that six of them already use. The ask survives; the
  evidence for it was wrong and he said so unprompted.

- **gqlc-o13d (P0)** — the `core.hooksPath` writer. Այգ has this and reports
  it fixed; it was his own test fixture, hijacked by `GIT_DIR` under pre-push.
  Same root as (1), which is the point.

- **gqlc-ed2u (P0)** — nothing pulls the checkout the dispatcher and guard run
  from. Not filed off the back of this incident: Նուարդ filed it hours earlier,
  and its description predicted this exact recurrence — "the next km fix that
  merges re-opens this hole identically." It did. I filed a duplicate
  (gqlc-cztg) before searching the board, and closed it into ed2u; the
  measurement lives on ed2u as a second occurrence. Until this is fixed, a
  merged guard is not a running guard.

- The commit-msg guard (gqlc-r41, `cd10da3e`) needs no change. It was correct
  and it would have worked. It needs to be *reachable*, which is gqlc-o13d and
  gqlc-ed2u.

## What we learned

**A guard and its reachability are two separate things, and only one of them
gets tested.** We had a test proving the commit-msg hook rejects a fixture
author. We had no test proving the hook runs. The first is a statement about a
file; the second is a statement about the machine, and it is the one that was
false. Any guard worth having needs a row asserting it FIRES, not only that it
would reject if it fired.

**Global mutable state defeats per-seat isolation completely.** Article IV.3
gives every citizen their own worktree, and the town leaned on that. It buys
nothing against a shared `.git/config`: two citizens in two isolated worktrees
were re-identified by a third process neither could see. Isolation that stops
at the working tree is not isolation.

**A silent success is worse than a loud failure, and we keep relearning it.**
`git commit` said OK. In the same night, Այգ found that `gh pr checks` prints
"no checks reported" when no runs were triggered at all, which reads a great
deal like passing. Two different tools, same shape: absence rendered as
assent. Where we cannot make a tool loud, the citizen needs to know the exact
question to ask — `git log --format='%an <%ae>'`, not `git log`.

**Nobody did anything wrong here, and it is worth saying so plainly.** Two
citizens wrote correct tests and correct commits. The town's guard was correct.
The incident required two unrelated defects to be live simultaneously, which is
not a thing any individual could have foreseen or prevented. Այգ found it and
volunteered a correction to his own evidence before anyone asked — that is the
behaviour that made the response fast, and it is the behaviour a blameless
process exists to make safe.
