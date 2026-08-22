# A test suite run by the pre-push hook committed to the repo it was gating, and set that repo's author identity for the whole town
Date: 2026-08-21   Written by: Այգ (seat ayg)   Beads: gqlc-pj4r, gqlc-10co, gqlc-n8n0

## What happened

`.githooks/tests/km-test.sh` was extended to build a git fixture — a small repo with
its own bare origin — so that its rows would stop depending on whether the developer's
checkout happened to have an `origin/master` ref. (CI has no such ref; ten rows had
just gone red there for that reason.) The fixture was created with `git init` under
`$TMPDIR`, configured with `git -C "$FIXTURE" config user.email fixture@example.invalid`,
and committed to with `git -C "$FIXTURE" commit`.

Run directly, the suite passed: 56 rows, 0 failures.

Run under `git push`, it reported 55 passed, 1 failed. The one red row was the row that
asks about a ref the real repo does not have.

Three `git push` attempts were made between 22:46:31 and 22:49:23. Afterwards:

- Six commits authored `fixture <fixture@example.invalid>` sat on the branch being
  pushed — `e43c9c13`, `bcc91fe5`, `b76e76f8`, `01ada6a7`, `7c5a4940`, `3c7d6341`, three
  repeated pairs, one pair per hook invocation. The next real commit, `40ed7e74`, sat on
  top of them, and its tree therefore recorded the deletion of most of the repository:
  `git diff --shortstat ff6203e2 40ed7e74` gives 5506 files changed, 499 insertions,
  635,802 deletions. `40ed7e74` was authored `fixture` too — by then the shared config
  said so, and my own commit took the identity like anyone else's. All of it was pushed.
- `/home/antranig/Developer/gqlc/gqlc/.git/config` — the single config file that all
  ~91 seat worktrees share — carried `user.name=fixture`,
  `user.email=fixture@example.invalid` and `commit.gpgsign=false`, from 22:46:31 until
  the keys were removed at 22:56:41.
- Two other seats committed and pushed inside that window and took the fixture identity
  with them: `42191059` at 22:47:38 on `fix/dz85-judge-cap-exempt` (PR #1133), and
  `c127a4b5` at 22:49:24 on `constitution/depth-of-thought` (PR #1134).

The mechanism: git invokes hooks with `GIT_DIR` exported, absolute, pointing at the real
repository. `git init`, `git commit`, `git config` and `git rev-parse` honour `GIT_DIR`
over **both** the current working directory **and** an explicit `-C <dir>`. Under the
hook, every git call the fixture made was addressed to the repo the hook was gating.
`git rev-parse` is also how `km` locates its repo, so the fixture was built and then
silently bypassed: the rows answered about the real checkout.

Recovery was `git reset` to the last good commit and a re-commit from the working tree,
which was never damaged; the three config keys were removed; the force-push was
authorised by Սեդրակ, who also supplied the correct commit count when the first
reconstruction found only three. He took the two other seats' branches himself
(Article IV.3 — a citizen does not repair another seat's branch).

## What allowed it

**1. The environment a hook hands to a test suite is invisible at the call site.**
`bash .githooks/tests/km-test.sh` in a justfile recipe looks identical whether it was
typed by a human or reached through `pre-push`. Nothing in the recipe, the suite, or the
hook says "the callee's git commands may be redirected." The suite author would have had
to know a fact about git that is not written down anywhere in this repo — and now is, in
bd memory `pre-push-hook-exports-GIT_DIR-into-test-suites`.

**2. The obvious defensive habit is not sufficient here, and looks like it is.**
`git -C "$FIXTURE"` is what a careful author writes to keep a fixture out of the ambient
repo, and it is what was written. `-C` loses to `GIT_DIR`. A guardrail that the careful
version of the mistake still walks into is not a guardrail.

**3. The damage was 98% green.** Fifty-five of fifty-six rows passed while the suite was
hijacked wholesale, because the paths those rows ask about exist in the real repository
too. The single red row was red by luck of subject matter. Had the suite not happened to
contain one row about a ref only the fixture has, three pushes would have gone through
clean, the six commits would have merged, and the shared identity would have stayed set
until someone noticed a contributor named `fixture` on GitHub. **The signal was
proportional to nothing.**

**4. The bogus-identity guard was live and let it through.** `.githooks/commit-msg`
denylists `test@example.invalid|*@example.com|*@example.org|root@localhost|""`.
`fixture@example.invalid` is one word off the first entry. `.invalid` is RFC 2606's
reserved TLD — an address ending in it can never be real, and matching the TLD cannot be
worked around by picking a different local part. An enumeration that must be extended by
hand each time somebody invents a fixture name was always going to be one name behind.

**5. One config file serves ninety-one worktrees.** `git config --local` from any seat's
worktree writes town-wide. That is git's design for worktrees and it is not going to
change, but nothing in this repo says so, and the blast radius of a single stray
`git config` is therefore much larger than the seat that runs it. Two other citizens'
work was altered by a test fixture in a directory they have never opened.

## What we change

Filed before this merges:

- **gqlc-10co** (P1) — the other suites are still exposed. `just test` runs all eight,
  `pre-push` runs `just test`, and `grep -c 'unset GIT_DIR'` returns zero for
  `commit-msg-test.sh`, `bd-gh-sync-test.sh`, `claude-pre-bash-test.sh` and
  `worktree-upstream-test.sh`; three of those build git fixtures. The bead asks for one
  shared clearing step rather than eight copies of an unset line — a copy is a thing the
  ninth suite will forget — and for a gate that **fails** when a suite shells out to git
  without it. Given cause 3, a warning is not enough.
- **gqlc-n8n0** (P2) — make the identity guard match RFC 2606's reserved TLDs
  (`.invalid`, `.test`, `.example`, `.localhost`) instead of enumerating addresses, and
  add rows using a local part other than `test`. The existing rows all use the exact
  denylisted strings, so the suite is green precisely because it asks the question the
  guard already answers.

Already landed, in PR #1128:

- `km-test.sh` unsets `GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE GIT_COMMON_DIR
  GIT_OBJECT_DIRECTORY GIT_ALTERNATE_OBJECT_DIRECTORIES GIT_PREFIX GIT_NAMESPACE`
  before it does anything, and reads the repo's HEAD *after* the unset so it is the real
  HEAD and not a hijacked one.
- A final row asserts that the repo the suite runs in has the HEAD the suite found it
  with. It reports **skip**, not pass, when there is no HEAD to watch — otherwise both
  sides read `none` and the row banks a green having compared nothing. This is the row
  that catches the next version of this from the other side, whatever redirects the
  fixture next time.
- The fix was proved by the push that landed it: the same hook, the same suite, HEAD
  unmoved.

## What we learned

**A suite can be hijacked in full and still read 98% green.** The proportion of rows
that fail tells you about the subject matter of the rows, not about the size of the
problem. When something goes wrong in the harness rather than in the code, expect the
number to be small and misleading — and build the one row whose job is to notice the
harness itself, because the code-facing rows will not.

**`-C` is not isolation.** Neither is `cd`. If a subprocess must not touch the ambient
repository, the ambient repository has to be removed from its *environment*, not merely
from its arguments. The careful-looking version of the call is the one that fails.

**Reproduce with the real value, not a plausible one.** The first attempt to reproduce
this used `GIT_DIR=.git`, which re-resolves harmlessly inside the fixture and shows
nothing. Git exports the absolute path. A near-miss reproduction that comes back clean
is worse than no reproduction, because it retires the hypothesis.

**A denylist of instances is a guard against the past.** `test@example.invalid` was on
the list; `fixture@example.invalid` was not. The property that makes both unusable —
RFC 2606 reserved the TLD so that no real address could ever end in it — was available
to be matched directly and was not.

**Blast radius belongs in the failure analysis, not in the aftermath.** The mistake was
one line in one seat's test fixture. It reached two other citizens' pushed commits
because ninety-one worktrees share one config file. Nobody had done anything wrong in
those two seats, and nothing they could have done would have protected them. When a
shared resource turns a local error into a town-wide one, that sharing is a cause and
gets a bead, the same as the error does.
