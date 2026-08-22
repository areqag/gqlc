# A test suite written to prove the town's code was deployed rewrote the repo it ran in
Date: 2026-08-21   Written by: Նուարդ   Beads: gqlc-ed2u, gqlc-2zet, gqlc-kl2d

## What happened

While gqlc-ed2u was being worked, a new suite of rows was added to
`.githooks/tests/km-test.sh` to pin the deploy seam. The fixtures those rows
stand on build throwaway git repositories under `$TMP`.

The suite runs under `just test`, which runs under `.githooks/pre-push`. Git
exports `GIT_DIR` into every hook environment, and for a seat working in a
linked worktree that value is `<repo>/.git/worktrees/<seat>`. Under that
environment the fixture builders did not act on `$TMP`:

- `git init -q "$1.seed"` re-initialised the exported `GIT_DIR` and set
  `core.bare = true` in the repository's SHARED config. Every checkout on that
  repository then reported "this operation must be run in a work tree".
- `git -C "$1.seed" commit` committed into the seat worktree's HEAD, force-moving
  `fix/km-deploy-drift` onto a fixture commit and orphaning the real work.
- `git -C "$2" switch -qc parked` parked the seat worktree on a fixture branch,
  after which all 5505 real files read as untracked.
- `git config user.email km@test` wrote the fixture identity into the shared
  config, where it stayed until a commit hook refused the address days later.

Separately, a mutation row that removed km's `KM_DEPLOY_ROOT` override was run
against the real tree. With the override gone the deploy root resolved to the
real shared checkout, and `km deploy` fast-forwarded it from c08fc7f0 to
10fc022c. That checkout needed deploying and the resulting state was correct,
but it was not a deliberate act and should not be read as one.

Recovery: `core.bare` was set back to false; all 5505 files were verified
byte-identical to the orphaned commit before anything was forced; the branch was
restored with `git checkout -f -B`; the fixture branch was deleted. No commit was
ever authored under the fixture identity — the AI-attribution hook rejected the
address before one could land.

## What allowed it

**The first diagnosis was wrong, and the screen that should have caught that
agreed with it.** `git init --bare` was named as the culprit. It is not a vector
at all: `--bare` re-points `GIT_DIR` at `"."` after its chdir. Neither is
`git clone`. The actual vector is the plain `git init <dir>`, because git guesses
bareness from the gitdir's own NAME (`guess_repository_type`) and a linked
worktree's gitdir does not end in `.git`. The mutation screen built to test this
used a plain repository as its sandbox — where the guess cannot fire — so it
reported the two live rows as SURVIVED and the wrong explanation stood. A screen
whose sandbox is the wrong shape does not merely miss defects; it certifies the
wrong story about them.

**Guards were written against the assumption instead of the mechanism.** The
decoy row asserted `core.bare` was still false against a `GIT_DIR` ending in
`.git`, the one shape where the flip is impossible. It passed on a technicality
for as long as it existed.

**Nothing anywhere proves that running the tests leaves the repository alone.**
This is the load-bearing gap. Every fact above was established by hand, after a
seat happened to notice its own branch had moved. Six of the eight hook suites
scrub the git environment and always did; the rule is real and widely followed,
and it is enforced nowhere. A suite that forgets it gets silent write access to
the developer's own repository.

**The reproduction was reasoned about rather than run.** Whether pre-push
actually exports `GIT_DIR` — the premise the entire diagnosis rests on — was
assumed for most of a working session and only confirmed at the end, by a
five-line hook that prints its environment. It took two minutes.

## What we change

- **gqlc-ed2u** (this branch): km's `main_root()` resolves through the scrubbing
  helper, because `deploy_root()` is its first caller that WRITES to what it
  returns. The suite's decoy is now a linked worktree left behind its own origin
  — the shape that can witness the flip, and an attractive victim for an
  unscrubbed fetch-and-merge. Rows pin deploy under an exported `GIT_DIR`, the
  derived root, and drift in BOTH directions, because a decoy that happens to
  agree with the root lets the leak pass. The suite adopted the house idiom
  `unset "${!GIT_@}"`; km cannot, since origin is ssh and `GIT_SSH_COMMAND` must
  survive the scrub, and it now says so where a reader will look.
- **gqlc-2zet** (P1, filed): a gate that snapshots the repository's own state
  around the test run and FAILS when the run changed it.
- **gqlc-kl2d** (P2, filed): the "every git call is scrubbed" rule, currently
  enforced inside km-test.sh alone, applied to every suite under
  `.githooks/tests`.

## What we learned

**A mutation screen has a sandbox, and the sandbox is part of the claim.** The
rows were live, the harness restored cleanly, controls survived, the arithmetic
was right — and the verdict was still wrong, because the sandbox was a plain
repository and the defect only exists in a linked worktree. SURVIVED does not
self-certify. It means either the guard is weak or the screen cannot see the
defect, and those two are not distinguishable from inside the screen.

**Ask what shape the defect needs, then check the fixture has it.** `guess_repository_type`
returns "bare" for any gitdir whose name is not `.git`. That one line of git
decides whether this incident is reproducible, and no amount of re-running the
screen would have surfaced it.

**Two guards can agree for a reason unrelated to either.** Making the decoy
attractive to a fetch-and-merge accidentally made it agree with a drifted root,
and a row that had been killing a mutant quietly stopped. Drift now gets asserted
in both directions: a single row only catches the sign it happens to share with
the leak.

**The cheap experiment first.** One hook that echoes `GIT_DIR` settles in two
minutes what a session of careful reasoning got wrong.
