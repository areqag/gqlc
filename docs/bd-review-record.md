# The V.2.0.4 review record

`kingdom/brain/decisions/0003-the-restart.md` binds the town to judge change 1 on
evidence:

> Change 1 is a throughput measure and V.2.0.4 binds the town to judge it as one: a
> defect reaching master through an unreviewed merge repeals or narrows it before
> the next merge. That requires counting — a defect found on merged code records
> whether its PR was reviewed. Without that record there will be no evidence to
> re-tune any of this, and it will be re-tuned on feeling instead, which is how it
> was tuned the first time.

This document is the derivation that produces that record. It is a **recipe run by
hand at a trigger**, not a standing service, and deliberately so: the raw material
accretes passively through protocol step 9's `from-pr:` labels, and the derived
ratio has a consumer only at re-tune time.

Designed and piloted by Արփինէ on 2026-08-26 (bd `gqlc-uvvr`); the patrol question
ruled by Սեդրակ 2026-08-26 and refined 2026-09-05; step (a)'s `class:judge`
exclusion ruled by Արփինէ 2026-09-05 (bd `gqlc-w7uza`); promoted here under bd
`gqlc-vhzx`. Figures carry the date they were measured on. Treat them as readings,
not as states.

**This document is not a gate and must never become one.** Change 1 is on trial and
this is the instrument that tries it. An instrument that blocks merges gets
disabled, and the trial then ends with no data — the precise outcome ADR 0003 was
written to prevent.

## The four verdicts

Per merged PR `N`:

- **REVIEWED(N)** — a **closed** `class:judge` bead names `N`. A closed judge bead
  is the signature of a completed review; an open one is a review still owed.
- **UNREVIEWED-BY-DESIGN(N)** — no judge bead names `N`, but `N`'s execution bead
  carries a `blocks` dependency on a design bead. **Counted, never flagged**: under
  V.2 most merges are unreviewed by choice, and a tool that flags them regresses to
  `gqlc-s4nh`, the cry-wolf reconciler this instrument replaced.
- **UNREVIEWED(N)** — neither. This is the cell the repeal clause watches.
- **UNKNOWN(N)** — linkage or queries failed. Never silently folded into another
  cell.

## Three traps, each already paid for

**1. The review record is not in GitHub.** Measured first-party by Արամազդ over the
60 most recently merged PRs: 0 of 60 carry a non-empty `reviewDecision`, and 0 of 60
carry any `gh` review object. This town records verdicts as bead close reasons and
issue comments. `reviewDecision: ""` is the universal state of a **correctly**
reviewed PR here, so any implementation consulting the GitHub review API is green on
every mocked fixture and wrong on every real PR.

**2. `bd list` hides closed beads, and `-n 0` does not defeat it.** A *closed*
`class:judge` bead is exactly the signature of a completed review, so the default
status filter is blind to the state that refutes an accusation of non-review. See
[bd-ledger-queries.md](bd-ledger-queries.md). This mistake produced a false V.4
escalation on `gqlc-s4nh`, made by a careful citizen twenty minutes after another
citizen documented it on the same bead.

**3. Matching a PR number needs title + description + notes**, and must accept the
`pull/N` URL form as well as `#N` and `PR N`. Matching titles alone produced two
false orphans in Վահագն's first census. Word boundaries matter: `1523` must not
match `15230`.

## Inputs

Two channels, both local.

**Channel 1 — one ledger snapshot.**

    bd list --all -n 0 --json > "$snap"

Both flags are load-bearing and for different reasons (trap 2). The output is
**multiple concatenated JSON documents**, and plain `.[]` iteration dies with
`Cannot iterate over string`. Flatten first:

    jq -s '[.. | objects | select(has("id") and has("labels") and has("status"))]'

`bd` resolves the ledger by **cwd**: run this inside the repository, or set
`BEADS_DIR`. From anywhere else it exits 1 with `no beads database found` on stderr
alone — silent to any caller that discards stderr, which is the failure family this
whole instrument is about.

**Channel 2 — per-PR linkage.**

    gh pr view <N> --json body -q .body | sed -n 's/^Bead: //p'

This yields the execution bead `E(N)`. GitHub is used for **this linkage line
only**, never for verdicts (trap 1). `gh` must run inside a repo worktree or carry
`--repo`; otherwise every lookup silently returns empty. The pilot's first attempt
returned NONE for all ten PRs from `/tmp` for exactly this reason.

## Derivation

**a. Defect set `D`** — beads whose labels carry `from-pr:<N>`, **excluding
`class:judge` beads**:

    select(((.labels|tostring)|test("from-pr:"))
           and (((.labels|tostring)|test("class:judge"))|not))

> **The exclusion is the recipe, not a workaround.** On a `class:judge` bead,
> `from-pr:N` means *the PR this review is of*, not *the PR that introduced this*,
> so the query without it counts every dedicated review round as a defect of the
> very PR it reviewed. Measured 2026-09-04 over 1126 records and re-measured
> 2026-09-05 over 1152: 82 `from-pr:` beads both times, the same 5 of them
> `class:judge`, all five review beads by title and all closed — `jmp4`/#1511,
> `dsfi`/#1717, `fhzjw`/#1892, `4ppun` and `nh1k2` both /#2068. `D` is 77.
>
> Ruled by Արփինէ 2026-09-05 on bd `gqlc-w7uza`, against the other candidate
> repair — moving the review-of edge to its own label and taking `from-pr:` off
> judge beads. Her reason: nothing tells a judge to write `from-pr:` on a review
> bead and five filers did it anyway, so that repair legislates against a natural
> reading with nothing enforcing it, while protocol step 8 does require
> `class:judge` on every review bead.
>
> **Two disclosed limits, and the second is the larger one.**
>
> First, a `class:judge` bead whose `from-pr:` genuinely meant *introduced by* — a
> defect a review round itself put on master — is undercounted by this line. Empty
> today: all five above are review records by title. Nothing detects a sixth. When
> the `class:judge` ∩ `from-pr:` count moves off five, read the new bead before
> letting the exclusion take it.
>
> Second, **`class:judge` is attached by hand and can be forgotten**, so it is
> required rather than guaranteed. Protocol step 8 says so itself — "a forgotten
> one leaves the review unwoken" — and step 8's own routing note adds that to a
> label query "an absent label reads exactly like an absent reader". A review bead
> missing its label is wrong in this recipe **twice, in the same direction**: it
> stays in `D` as a phantom defect, and its PR reads UNREVIEWED at step (b). Both
> errors inflate the unreviewed column. That is the fail-loud direction, which is
> why it is disclosed rather than patched, but a runner who finds an implausible
> row should check the label before believing it.

A non-numeric `from-pr:` value must be **skipped and reported, never guessed and
never crashed on**. The pilot met one for real (`gqlc-8hcf` carried
`from-pr:pending`); the arm worked, and the introducing PR was then derived from the
merge history rather than assumed.

**b. Judge coverage `J`** = `{N : ∃` closed `class:judge` bead recording a review of
`N`'s diff **before** the merge`}`.

Getting there takes two steps, and conflating them over-counts. The query below is
the **candidate** query, deliberately wide — it searches title, description and
notes because a narrower search produces false orphans (trap 3):

    [.. | objects | select(has("labels"))]
      | map(select(.labels|tostring|test("class:judge")))
      | .[] | select([.title,.description,.notes]|join(" ")|test("\\b<N>\\b"))

**Its output is not `J`.** Each candidate must then be read against "The patrol
ruling" below. Measured 2026-09-04 for `N = 1511`, the query returns three beads —
`gqlc-t8r6`, `gqlc-gzzn` and `gqlc-jmp4` — of which only `jmp4` is a dedicated
review round; the other two are patrols that mention #1511 in their notes. Taking
the query's output as `J` would mark #1511 reviewed on the strength of two
post-merge sweeps.

**c. Execution bead `E(N)`** from channel 2. If the `Bead:` line is absent, the
verdict is UNKNOWN with the reason recorded — never guessed.

**d. Design-blocked(`E`)** — `bd show E --json` carries the edges; `bd list` carries
only the `*_count` integers. Note the field shape: `bd show E` embeds `E`'s
**dependents** under entries carrying `dependency_type: "blocks"` — the field
describes the edge, not the direction. `E`'s own blockers are `E.dependencies` at
top level. `E` is design-blocked iff any blocker is labelled `class:architect`.

**e. Ratio** = defects per unreviewed merge : defects per reviewed merge, with
UNREVIEWED-BY-DESIGN reported **beside** the two denominators and never folded into
either. A count of one side alone answers nothing.

## The patrol ruling

**A patrol read does not make a merge reviewed.** Ruled by Սեդրակ 2026-08-26,
re-affirmed and sharpened 2026-09-05.

The cell measures whether the **pre-merge gate ran**. That is the V.2 invariant and
the only thing the number is for. A post-merge patrol cannot make a merge have been
gated, however adversarially it read; its findings are real, but they are evidence
about a different question. Patrol findings already enter this instrument through
their own `from-pr:` labels, and counting the patrol *itself* as a review would
decay the unreviewed column on patrol's schedule rather than on defect evidence.

**The cell keys on what the bead did, not on what it is called.** A `class:judge`
bead titled "patrol" that reviewed a diff **before** the merge counts. A post-merge
read does not, however good.

The decisive case, and the one that fixes the boundary: `gqlc-xs40` read PR #1523
"in full, adversarially" and filed two defects against it. Its own close reason
calls #1523 *"unreviewed (no judge bead ever named it)"* — the patrol testifying
that the gate did not run. An instrument that read that testimony as proof the gate
**did** run would report the opposite of its own source.

**This ruling makes the town's number look worse than the alternative reading does,
and that is deliberate.** Do not "fix" it. The failure modes are not symmetric:
under-counting costs a duplicate review, while over-counting lets a PR that owes a
review read as satisfied by a sweep that never opened the diff before merge. For an
instrument the constitution leans on, take the error that fails loud.

## Baseline row 1 — 2026-08-26

Snapshot of 528 records; 15 linked defect beads over 11 distinct PRs. **A historical
reading, not a current figure.**

| PR | exec bead | design-blocked | judge coverage | verdict — defects |
| --- | --- | --- | --- | --- |
| 1057 | `gqlc-osuz` | no | `sscv`, closed, title-named | REVIEWED — nmak |
| 1397 | `gqlc-ferp` | no | none | UNREVIEWED — 8qxy, gsr9 |
| 1407 | `gqlc-wz47` | no | none | UNREVIEWED — 8hcf |
| 1452 | `gqlc-49hu` (itself `class:architect`) | no | none | UNREVIEWED — ebp3 |
| 1454 | `gqlc-d26r` | no | none | UNREVIEWED — effn |
| 1464 | `gqlc-7i3g` | no | `jwt4`, closed, title-named | REVIEWED — in4f |
| 1478 | `gqlc-pwly` (itself `class:architect`) | no | none | UNREVIEWED — pyc6 |
| 1489 | `gqlc-3d0l` | yes (`h0lw`, `class:architect`) | `6fwl` + `nx81`, closed | REVIEWED — 5ask |
| 1511 | `gqlc-gsr9` | no | patrol mentions only | UNREVIEWED — jmp4 (†) |
| 1523 | `gqlc-xjki` | no | patrol mentions only | UNREVIEWED — 8ll6, pykf, vwga |
| 809 | none — body predates the `Bead:` convention | — | none | UNKNOWN ×2 — 35yu.16, 35yu.17 |

**REVIEWED 3 : UNREVIEWED 10**, UNKNOWN 2, BY-DESIGN 0. Defect counts, not PR
counts: the three reviewed PRs contributed three defects between them.

`n` is far too small to move any clause. This is recorded as baseline row 1, not as
evidence of anything.

**(†) This row does not survive step (a)'s exclusion, and both figures are kept on
purpose.** `gqlc-jmp4` is a `class:judge` review record, so the ruled step (a)
drops it from `D`; #1511 carries no other `from-pr:1511` bead (re-checked
2026-09-05), so the whole row leaves the table. That gives **14 defects over 10
PRs, REVIEWED 3 : UNREVIEWED 9**, UNKNOWN 2, BY-DESIGN 0 — every other row
unchanged, verified per-PR against the ledger rather than by subtraction.

The table above is left as the pilot took it because Սեդրակ's patrol ruling was
published against exactly these numbers ("Reading B stays 3:10"), and a figure a
ruling was made on is not silently overwritten. The two disagree by one defect and
by one PR, and by nothing else. Baseline row 2, whenever it is taken, is taken
under the corrected step (a) alone.

Two shapes in the table are reported, not judged, and the bench may want to look at
them: #1452 and #1478 executed `class:architect` beads directly, so no design
blocker was possible; and `E(1511) = gsr9` is itself a `from-pr:1397` defect bead,
making a defect-fix chain visible. Both are legal under this recipe.

### Defects found after merge, by patrol

This column prices the misses instead of merely counting them, and it is the sharper
argument for the gate than the coverage number is. It is **defined here and not yet
derived** over the whole pilot; supply no figure for it that you have not taken.

Measured for one PR, 2026-09-04: of #1523's three `from-pr:1523` defects, **two —
`gqlc-pykf` (P1) and `gqlc-vwga` (P3) — were filed by the patrol `gqlc-xs40`**.
`gqlc-8ll6` reached the ledger by another route and is not attributed to that
patrol. `gqlc-cz0z`, also filed by that round, is adjacent machinery and carries no
`from-pr:1523` label, so it is not in `D`.

## The defect this instrument had in itself

Recorded here because an instrument that hides its own faults is worth less than one
that states them, and kept after the repair because the shape recurs.

As piloted, step (a) counted review beads as defects (bd `gqlc-w7uza`, ruled
2026-09-05). The sharp case is `gqlc-jmp4`: closed, `class:judge`, close
reason `PASS`, and its **title names #1511**. Under this document's own step (b)
that is the signature of a completed review — yet the pilot had it inside `D` as a
*defect of #1511*, while #1511's coverage column reads "patrol mentions only".
**The defect-set query swallowed the review record the instrument exists to
measure**, and it did so in the direction that flatters nobody: a phantom defect
charged to a PR the same bead proves was read.

That is what decided the repair's shape. Of the two candidates — exclude
`class:judge` from `D`, or take `from-pr:` off judge beads and give the review-of
edge its own label — the one-line exclusion was ruled in, because the same line
that shrinks `D` un-swallows `jmp4` as step (b) evidence. It is now step (a), with
its limit disclosed there.

The general form, for whoever extends this recipe: **a label that means two things
depending on the bead it sits on will be read as one thing by any query that keys
on the label alone.** `from-pr:` was the first; it is unlikely to be the last.

## When to run it

At the **first** defect filed against code merged through a plausibly unreviewed PR.
Do not wait for a batch: V.2.0.4 says repeal-or-narrow **before the next merge**.

Running this by hand is the point. Pain here is the measured evidence that justifies
building a standing tool; ease here is proof the town never owed one. The pilot took
about ten minutes including two wrong turns, both now written into the recipe above.

## Honest limits

- Baseline row 1 is a **single snapshot** taken around 08:5xZ on 2026-08-26. The
  board moves; a later run disagreeing with its totals does not correct it.
- Judge matching is a regex over title, description and notes. A judge bead that
  names its PR **only in its close reason** would be missed — `close_reason` is a
  separate field and was not searched. The next runner should add it.
- Baseline row 1 was taken under step (a) **before** its `class:judge` exclusion.
  The corrected figure is stated beside it; the table itself is the pre-exclusion
  reading and is marked at the one row that moves.
- Step (a) undercounts a `class:judge` bead that genuinely introduced a defect.
  Empty today across all five such beads; nothing detects a sixth.
- Steps (a) and (b) both key on the `class:judge` **label**, which is attached by
  hand. A review bead missing it inflates the unreviewed column twice over. The
  citizen protocol's own remedy for this is a second pass that asks the roster who
  the judges are instead of asking the bead what it is; this recipe does not run
  that pass, and a runner chasing an implausible row should.
- The "defects found after merge by patrol" column is defined but derived for one PR
  only.
- Everything here was measured against the deployed `bd`, not against its source.

## A note on where rulings live

The patrol ruling was made on 2026-08-26 and **appended to `gqlc-uvvr` three hours
after that bead closed** (`closed_at` 09:21:53Z, `updated_at` 12:44:40Z). It then
could not be found — not by the bead that was waiting on it, not by the citizen who
searched for it on 2026-09-03, and not by the mayor who had written it, who
apologised on 2026-09-04 for a delay he had not caused and ruled a second time.

A ruling that lives only in a closed bead's notes has not been recorded; it has been
buried. That is the reason this document exists, and the reason a ruling belongs on
the bead that is still open and asking, in the words the asker used.
