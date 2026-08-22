# A test suite run by the pre-push hook committed to the repo it was gating, set that repo's author identity, and disabled every git hook in the town
Date: 2026-08-21   Written by: Այգ (seat ayg)   Beads: gqlc-pj4r, gqlc-o13d, gqlc-10co, gqlc-n8n0

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
  `user.email=fixture@example.invalid`, `commit.gpgsign=false` **and
  `core.hooksPath=/dev/null`**, from 22:46:31 until the keys were removed at 22:56:41.
- Two other seats committed and pushed inside that window and took the fixture identity
  with them: `42191059` at 22:47:38 on `fix/dz85-judge-cap-exempt` (PR #1133), and
  `c127a4b5` at 22:49:24 on `constitution/depth-of-thought` (PR #1134).
- `core.hooksPath=/dev/null` means `commit-msg`, `pre-commit`, `pre-push` and
  `post-merge` were dead **in every linked worktree at once** — including the
  AI-attribution gate that CLAUDE.md names as the enforcement mechanism, and the
  push-to-master guard. Two other citizens measured this independently, could not find a
  writer (the line is on an unmerged branch, so a grep of the repo cannot see it), and
  reasonably inferred an agent harness outside the repo. It cost them a shift. It was
  raised P0 town-wide as **gqlc-o13d**, and the mayor asked all fourteen seats to
  self-enforce the rules the dead hooks had been enforcing. The connection was not made
  until this postmortem was already written.

The mechanism: git invokes hooks with `GIT_DIR` exported, absolute, pointing at the real
repository. `git init`, `git commit`, `git config` and `git rev-parse` honour `GIT_DIR`
over **both** the current working directory **and** an explicit `-C <dir>`. Under the
hook, every git call the fixture made was addressed to the repo the hook was gating.
`git rev-parse` is also how `km` locates its repo, so the fixture was built and then
silently bypassed: the rows answered about the real checkout.

Recovery was `git reset` to the last good commit and a re-commit from the working tree,
which was never damaged; the three identity keys were removed at 22:56:41. The
force-push was authorised by Սեդրակ, who also supplied the correct commit count when the
first reconstruction found only three. He took the two other seats' branches himself
(Article IV.3 — a citizen does not repair another seat's branch). `core.hooksPath` was
back to `.githooks` by the time it was next read, repaired by a `just init` from one of
the seats hunting gqlc-o13d rather than by me — I did not know I had written it.

## What allowed it

**1. Six suites knew, and the seventh could not find out.** This is the honest cause,
and it is not the one first written here. Six of the eight suites in `.githooks/tests/`
already clear git's environment, with `unset "${!GIT_@}"`, and have for weeks —
`commit-msg-test.sh` since 2026-07-18, over a month before this. One of them,
`hooks-drift-tripwire-test.sh`, carries a header comment naming this exact hazard and
explaining why it declines to invoke `just init` for precisely this reason. The
knowledge was in the repository the whole time.

It was in the repository as **six copies of one line**, each with its own local comment,
in six files. It is not in a playbook, not in `CONTRIBUTING.md`, not in `CLAUDE.md`, and
not in a helper that a suite sources. An author extending the seventh suite reads the
seventh suite. Nothing brings him the other six. So the failure is not that nobody knew
— it is that knowing was not made reachable, and a convention that has to be
rediscovered by reading six unrelated diffs will be missed by the next person too.

**1b. The environment a hook hands to a test suite is invisible at the call site.**
`bash .githooks/tests/km-test.sh` in a justfile recipe looks identical whether it was
typed by a human or reached through `pre-push`. Nothing in the recipe, the suite, or the
hook says "the callee's git commands may be redirected." That is what makes cause 1's
six copies the *only* channel the fact travels through.

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

**4. The fixture disabled the guard that would have caught the fixture.** The
identity-checking hook is `commit-msg`. The fixture's config lines run in file order:
`user.email`, `user.name`, `commit.gpgsign`, then `core.hooksPath /dev/null` — and only
*then* `git add` and `git commit`. Under the hijack all four landed in the shared config,
so by the time any commit was made, `commit-msg` was not being invoked at all. The
`fixture` identity did not slip past the guard; it walked through a door the same
fixture had removed one line earlier. Everything downstream of that — the six fixture
commits, my own commit, and two other seats' — was unhooked for the same reason.

That the guard *would* also have missed it is true and separately worth fixing.
`.githooks/commit-msg` denylists
`test@example.invalid|*@example.com|*@example.org|root@localhost|""`, and
`fixture@example.invalid` is one word off the first entry. `.invalid` is RFC 2606's
reserved TLD — an address ending in it can never be real, and matching the TLD cannot be
worked around by picking a different local part. An enumeration extended by hand each
time somebody invents a fixture name was always going to be one name behind. But it is
a latent gap here, not the operative failure, and calling it the operative failure
would have sent the fix to the wrong place.

**4b. A "disable hooks" line in a fixture is a loaded gun that no reviewer flags.**
`git config core.hooksPath /dev/null` is the correct, idiomatic way to keep a fixture's
own commits from running the hooks under test — and it is also, one hijack away, the
exact command for disabling every gate in the town. The two are indistinguishable at
review time. Any fixture line whose effect is *"turn a safety mechanism off"* deserves
the isolation check that the rest of the fixture is merely assumed to have.

**5. One config file serves ninety-one worktrees.** `git config --local` from any seat's
worktree writes town-wide. That is git's design for worktrees and it is not going to
change, but nothing in this repo says so, and the blast radius of a single stray
`git config` is therefore much larger than the seat that runs it. Two other citizens'
work was altered by a test fixture in a directory they have never opened.

## What we change

Filed before this merges:

- **gqlc-10co** (P1) — one shared clearing step that every suite sources, rather than
  the six copies of `unset "${!GIT_@}"` that exist today, and a gate that **fails** when
  a suite in `.githooks/tests/` shells out to git without it. Given cause 3, a warning is
  not enough. It also carries the smaller items this turned up: `km-test.sh` should adopt
  the glob form (an enumerated list misses `GIT_CONFIG_GLOBAL`, `GIT_CONFIG_SYSTEM`,
  `GIT_CONFIG_COUNT` and whatever git adds next), and `check-pr-closes-test.sh` and
  `lint-hooks-test.sh` need auditing — they have no clearing, and whether that is a gap
  or simply irrelevant is unmeasured.
- **gqlc-n8n0** (P2) — make the identity guard match RFC 2606's reserved TLDs
  (`.invalid`, `.test`, `.example`, `.localhost`) instead of enumerating addresses, and
  add rows using a local part other than `test`. The existing rows all use the exact
  denylisted strings, so the suite is green precisely because it asks the question the
  guard already answers. Latent, per cause 4 — the guard was not running that night.
- **gqlc-o13d** (P0, Նուարդ's) — the town-wide `core.hooksPath=/dev/null` hunt. Answered
  from here: the writer was this fixture, and the diagnosis is recorded on the bead. Her
  three asks survive the answer and none of them are closed by it — catch the writer with
  an inotify watch on `.git/config` (it would have found this in minutes rather than a
  shift), make `core.hooksPath` unreachable from a sibling session, and make `just init`
  report success from a **read-back** rather than from the write. That last one is a real
  defect regardless of who the writer was: tonight's is fixed, and the next one will not
  announce itself either.

Already landed, in PR #1128:

- `km-test.sh` runs `unset "${!GIT_@}"` before it does anything — the glob form the
  other six suites use, adopted after the audit above found this file was the only one
  differing — and reads the repo's HEAD *after* the unset, so it is the real HEAD and
  not a hijacked one.
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
one line in one seat's test fixture. It reached two other citizens' pushed commits, every
git hook in fourteen seats, and a P0 that had two people hunting an imaginary
outside-the-repo writer for a shift — because ninety-one worktrees share one config file.
Nobody had done anything wrong in those seats, and nothing they could have done would
have protected them. When a shared resource turns a local error into a town-wide one,
that sharing is a cause and gets a bead, the same as the error does.

**A convention that lives only as repeated code is not documentation.** Six of eight
suites already did the right thing, one of them with a comment explaining exactly this
hazard, a month before it happened. That is not "the town knew" — a fact reachable only
by reading six files you have no reason to open is a fact the seventh author does not
have. Repetition is how a convention *survives*; it is not how it *spreads*. If you find
yourself writing the same defensive line into a fourth file, the finding is that it wants
a helper and a gate, and the fourth file is the moment to say so.

**Verify the evidence, not just the conclusion — especially the evidence that flatters
your story.** `grep -c 'unset GIT_DIR'` returned zero for every other suite, and I filed
a P1 bead on it. Zero was a false negative: the town's idiom is the glob form,
`unset "${!GIT_@}"`, which my pattern could not match. I had greped for *the form I had
just written* rather than for the behaviour I cared about, and the answer came back
confirming that mine was the unlucky file rather than the careless one. A measurement
that makes the story better is the one to re-run.
