# 0008 — A branch is finished when its PR record says so

Date: 2026-08-29. Designed against bd gqlc-2msl, which gqlc-5t2u raised and
stopped behind at the design gate (Constitution V.1). Executed by gqlc-5t2u
(the held draft PR #1621 resumes) and, for the residual shape, gqlc-guq3.

## The shape of the problem, in plain words

`km seat-refresh` may move a seat's worktree onto origin/master so the seat
picks up merged `.githooks`/`.claude` gates — but only when nothing standing
in that tree is disturbed by the move. Today "on a branch" holds a seat
forever, so a seat whose branch merged is reported exposed forever and
refreshed never; that is gqlc-5t2u's defect, and its fix ("hold only what
carries commits no other ref holds") is correct about data loss and measured.
It is not correct about people: run over the live town it detaches Անահիտ,
who is mid-bead with a clean tree, everything pushed, and an open PR. Nothing
is lost and someone is still forced (VI.2; gqlc-kivo: "nothing should force
any of these trees").

The gap is that the fix answered one question with a predicate that was
believed to answer two:

- **"Is it safe to the data?"** — git answers this. A branch fully held by
  its upstream loses nothing when the worktree detaches; the branch ref and
  the remote both keep every commit, and `checkout --detach` deletes no ref.
- **"Is the work finished?"** — git cannot answer this. A fully-pushed branch
  with an open PR and a fully-pushed branch whose PR merged last week are
  byte-identical to every git query; the difference between them lives in
  the PR record and nowhere else.

**The ruling: the authority for "this branch is finished" is the PR record.**
A branch is finished iff at least one PR exists for it and every PR for it is
closed. Both conjuncts are load-bearing: "no PR yet" is mid-work (Ար's own
5t2u branch sits pushed on the remote with no PR while this design is
written — moving his seat would repeat the Անահիտ case one bead later), and
"any PR open" is mid-work by definition. A branch moves only when git says
*safe* and GitHub says *finished*. Neither authority substitutes for the
other.

## 1. Vocabulary: a fourth freshness state, `published`

`seat_freshness` keeps gqlc-5t2u's predicate and stops over-claiming on the
one shape it cannot judge. On a branch, with a clean tree:

- upstream unresolvable, or `rev-list --count @{u}..HEAD` > 0 → `working`,
  as the draft already has it (no upstream counts as held: nothing was
  *shown* to hold this — the fail-closed pin gqlc-guq3 documents);
- unique count 0 → **`published`**: on a purposeful branch, everything held
  by its upstream, nothing here to lose — and *finished-ness is not git's
  question*, so the word deliberately refuses to say `parked`.

`parked` keeps its meaning (detached, clean, nothing of its own: nobody put
it there on purpose) and remains the only state `seat_freshness` alone can
sentence to a move. The line format is unchanged:
`published <behind_all> <behind_gates> 0 branch <name> fully held by its upstream`.

This preserves gqlc-p1ek's one-derivation ruling: status, doctor and
seat-refresh keep reading the same function, and the new word flows to all
three. `cmd_status`'s TREE column gains a `published` arm rendering
`pub[-N][!]` in the working arm's shape (reported, never counted stale); it
must not fall through to `*)`, which would render "?" and count the seat
unjudged. `cmd_doctor`'s coverage arm reads only the gates count and needs
no change.

## 2. Where the network question lives

**In `seat_refresh_step`, never in `seat_freshness`.** The freshness comment
already rules that a board that stalls on the network is a board nobody runs;
this design extends that ruling rather than bending it. The acting path
already fetches before judging, and km already owns a fail-closed gh call
shape — `hold_open_pr_files` (km:2110): `timeout 30 gh pr list … || return 1`.
The new helper follows it:

    branch_pr_state <branch-name> -> open | finished | none | unanswered

    timeout 30 gh pr list --head "$name" --state all \
        --json state,headRefOid --limit 50

- non-zero exit (gh absent, unauthenticated, offline, timed out) →
  `unanswered`;
- `[]` with exit 0 → `none` (a real answer, distinct from no answer — the
  jq-null shape that reads "no PRs" over a failed query is the known trap,
  so the helper distinguishes exit status from output emptiness);
- any element `OPEN` → `open`;
- otherwise → `finished` (every PR closed or merged).

`headRefOid` is in the field list from day one although this consumer ignores
it: gqlc-guq3's consumer (§4) needs it, and the contract should not reshape
under her.

The branch name queried is the **upstream's** name, not the local one —
`git rev-parse --abbrev-ref --symbolic-full-name @{u}`, first path component
stripped. A PR's head is a remote branch; the local name can differ and a
same-named local branch is not the PR head (town law, measured).

## 3. What `seat_refresh_step` does with the answer

A new `published` case beside `parked`, with these verdicts — each message
names its reason, because two paths to one HOLD must stay tellable apart:

- `open` → held, mid-work. Exit 0 when `behind_gates` is 0, exit 3 otherwise
  — exactly the `working` arm's exposure semantics. This is Անահիտ's case
  and the whole point: an open PR is a citizen mid-bead, whatever git says.
- `none` → held, "published but not shown finished (no PR for <name>)".
  Same exits. This is the pushed-no-PR case, live today on Ար's own seat.
- `unanswered` → held, "GitHub did not answer; refusing to guess". Same
  exits. An unanswered question is not a clean answer — the freshness
  comment's own words, applied to the other network.
- `finished` → the move, with the authority in the journal line:
  "refreshed <old> -> <new> (branch <name> finished: every PR for it is
  closed)". The move is the same `checkout --detach` as `parked`; it deletes
  no ref, and the citizen's branch remains one `git checkout <name>` away.

**One more conjunct before the move: `seat_session_live` must be false.**
km-seat's wake path already states the principle over its own refresh call —
"Wake is the only safe moment to move it: the session has not started, so no
checkout happens under a live claude" (km-seat:178-182) — and this conjunct
is that comment generalised to the sweep and the manual path, which today
have no such guard. A finished branch under a live session belongs to a
citizen who may be closing beads, reading their merged PR, or dead mid-turn
awaiting decision 0007's recovery — and 0007's nudge tells that citizen to
re-verify their state, which a tree that moved underneath them turns into a
trap. The conjunct reads the world (herdr's report), not the status file: a
dead-turn seat is held either way, but a seat whose record says `awake` over
a crashed session is movable, which the record would wrongly strand.
Deferring costs at most a workday: km-seat refreshes before claude starts
(refresh at km-seat:183, launch at :201), so at the wake path the session is
never live and the guard cannot starve the main clearing route. The sweep
holds a live seat's published-finished branch with its own message
("finished, but a live session stands in it; its wake-path refresh will take
it").

`--check` asks the same question and moves nothing, reporting what would
happen; the sweep already fetches in check mode, so a read-only gh query
breaks no expectation that `--check` is offline.

## 4. The residual shape belongs to gqlc-guq3, on the same primitive

The shape this design leaves fail-closed: upstream gone from **both** sides
(remote branch deleted and tracking ref reaped), where `@{u}` is
unresolvable, unique counts as 1, and the seat reads `working` forever. Git
alone cannot distinguish it from a never-published branch — the evidence is
simply absent — and no live seat is in that state today (guq3's measurement).

The cure is the same helper with one more conjunct: query
`branch_pr_state` with the **local** branch name, and treat the branch as
finished iff the answer is `finished` **and** some closed PR's `headRefOid`
equals the local HEAD. In the published case (§3) the upstream witnesses the
data; here nothing does, so GitHub must witness both halves — a closed PR
whose head equals this HEAD means `refs/pull/N/head` holds every commit, and
the data-loss standard is met by GitHub's own ref. Head mismatch → held: a
local branch that moved past its PR's head carries commits the PR does not.
gqlc-guq3 stays Նուարդ's bead, stays blocked behind gqlc-5t2u, and inherits
this contract; the two measured traps recorded on it (squash defeats
ancestry and patch-id both; "pushed" is not "safe") stand as the reasons the
sha-equality conjunct is an equality, not an ancestry test.

## 5. Rejected, with reasons

- **The ledger as authority** (an assigned in_progress bead whose id appears
  in the branch name = mid-work). Fails open exactly where it must not:
  57 of the 60 most recently merged PR head branches carried no bead id
  (measured 2026-08-23, CLAUDE.md), and a closed bead is not a merged bead —
  quota kills strand finished-looking lanes with unfinished branches. Wrong
  in the forcing direction, on the majority shape. As a *supplementary*
  holder it adds nothing the open-PR conjunct does not already hold.
- **Refreshing gate paths without moving HEAD** (`checkout <target> --
  .githooks .claude`). Dissolves the question and breaks two rulings at
  once: it writes into a citizen's index and working tree mid-bead — quieter
  forcing, not less — and it splits coverage from the commit count, so
  `seat_freshness` (which counts commits, per gqlc-p1ek, because content
  probes measured wrong on all 14 seats) would go on reporting a seat
  exposed whose gate bytes are current. A second derivation of coverage is
  the exact defect p1ek exists to forbid.
- **Ancestry or patch-id as the finished test.** Measured wrong in both
  directions on gqlc-guq3: squash merge rewrites history so `--is-ancestor`
  refuses a merged branch forever, and patch-id reported all five of a
  merged branch's commits "not on master", confidently. Neither appears
  anywhere in this design.
- **Asking nothing and shipping the draft as-is.** The draft's own
  measurement rejects it: it moves a citizen with an open PR.

## 6. What ships

**gqlc-5t2u (Ար; draft PR #1621 resumes):** all in `kingdom/bin/km` —

1. `seat_freshness`: the `published` split of the draft's branch arm (§1);
   comment updated to name four states and this decision.
2. `branch_pr_state` beside `hold_open_pr_files`, in its fail-closed shape
   (§2).
3. `seat_refresh_step`: the `published` case (§3), including the
   session-live guard and the four reason-naming verdict lines.
4. `cmd_status`: the `published` TREE arm (§1).
5. No tests, no harness — standing direction. In their place the PR body
   carries the hand-witness battery, **each verdict declared before the run**
   (a detector that exits 0 on everything is not a gate): the Անահիտ-shape
   sandbox re-run expecting *held (PR open)*; a finished shape (closed PR,
   upstream intact) expecting *moved, reason naming the PR record*; a
   pushed-no-PR shape expecting *held (not shown finished)*; a broken-gh run
   (`PATH` without gh, or `GH_TOKEN` invalidated) expecting *held
   (unanswered)* — the run that proves the guard can fail; and the
   session-live guard both ways, via a scratch `herdr` shim on `PATH`
   (mktemp'd, never committed) whose canned `agent_status` makes the sandbox
   seat read live — expect *held (live session)* — and then absent — expect
   *moved*. The sandbox cannot witness a real live session any other way; if
   the shim is judged dishonest, the honest alternative is an inverted-guard
   mutation run plus a stated limitation, never a silently unwitnessed
   conjunct.

**gqlc-guq3 (Նուարդ, after 5t2u):** the no-upstream consumer with the
sha-equality conjunct (§4). Nothing in 5t2u's delivery may close her case
open — the no-upstream branch of the predicate stays fail-closed until her
bead lands.

Vocabulary: `published` is a town word for a machinery state, not a gqlc
domain term; `CONTEXT.md` (the product glossary, by its own header) is
untouched, and the word is defined where the state is derived.

## Precedent

Extends: gqlc-p1ek's one-derivation ruling (the new state flows through the
same function to every reader); the freshness comment's no-network-in-the-
board ruling (the question moves to the path that already fetches);
`hold_open_pr_files`' fail-closed gh shape (timeout, exit-status checked,
absence of an answer distinct from an empty answer); gqlc-kivo's reading of
VI.2 (a tree mid-bead is forced by moving it, not only by losing from it);
km-seat's own wake-refresh comment ("no checkout happens under a live
claude"), generalised into §3's session-live guard; gqlc-5t2u's
unique-commit predicate, kept whole as the data-safety half.

Bends: `seat_refresh_step` gains a network question — deliberately, and only
in the acting path, where a fetch already lives; the board stays offline.
Nothing here bends VI.2: §3's open-PR hold and awake guard are where it is
held.
