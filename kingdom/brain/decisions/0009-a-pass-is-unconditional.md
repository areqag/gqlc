# 0009 — A verdict is PASS or FAIL, and a PASS is unconditional

Date: 2026-08-29. Designed against bd gqlc-vnil, under decision 0003. Executed by
gqlc-zfb0. Witnesses: PR #1712 / gqlc-ftxp / gqlc-mpgw (Վահագն's letter of
2026-08-29T17:17:46Z), gqlc-o3gj, gqlc-sniv.

## The shape of the problem, in plain words

A judge who has read a PR and wants two more things sometimes writes "PASS,
conditioned on X and Y" and closes the review bead. The author reads the
headline, merges, and the conditions go wherever prose goes. On 2026-08-29
PR #1712 merged with neither condition of its PASS landed; the condition that
evaporated was not cosmetic — it was the difference between an escalation
witness that works and one that silently confirms failed deliveries. Nothing
was red anywhere: the review bead was closed, the PR was merged, the gates
were green. A conditioned PASS that merges unconditioned is invisible by
construction.

That invisibility is not an accident of one judge's phrasing. `close_reason`
appears nowhere in `kingdom/bin/km` — no dispatch pass, no doctor row, no
sweep reads it (measured 2026-08-29 by grep over the file). A condition
written there is machine-invisible the moment it is written, and the merge
path — an author acting on "merge on the Դատաւոր's PASS" (Constitution V.2) —
reads the headline of a verdict, not its clauses.

And this is established practice, not a one-off. Of 93 closed `class:judge`
beads on the ledger 2026-08-29, eight closed with a genuine conditioned PASS:
gqlc-sk5j (three binding conditions), gqlc-sq0d, gqlc-qxhp, gqlc-uwwg,
gqlc-j87i, gqlc-4juf, gqlc-ib5y, gqlc-ftxp. gqlc-o3gj records the town
already paying for the shape a second way: a filer misread a
closed-with-conditioned-verdict bead as verdict-less and woke two judge seats
for one round. So a design that treats the shape as an aberration owes an
answer to why judges keep reaching for it. That answer is below, and it is
sympathetic: the need is real; the carrier is wrong.

## What a condition actually is, measured against the eight

Reading the eight verdicts, every condition in the wild is one of three
things:

1. **Work the merge must wait for.** Code or record bytes that must change
   before this PR may land — gqlc-ftxp's from-filtered delivery witness,
   gqlc-qxhp's disclosure paragraph, gqlc-ib5y's PR-body clause.
2. **Work the merge need not wait for.** A change the judge wants and would
   sign the merge without — follow-up work in verdict clothing.
3. **A fact to verify at merge time.** "Conditional on CI green"
   (gqlc-4juf), "conditional on the successor delta being content-identical"
   (gqlc-uwwg), "verified unmoved" — instructions to the merging author about
   the state of the world, not about new work.

Each of the three already has a carrier in this town, and each carrier is
better than prose:

1. Work the merge must wait for **is a FAIL**. Constitution V.4: a FAIL on an
   open PR blocks its merge until answered, and is answered by the judge who
   wrote it. That is precisely the semantics the judge wanted the condition
   to have.
2. Work the merge need not wait for **is review residue**, and the carrier
   for residue is a bead — filed by the judge before the close, with a
   `discovered-from` edge, exactly as citizen-protocol step 9 already
   prescribes. A bead is routable, board-visible, and survives the squash;
   a sentence in a close reason is none of those.
3. A fact to verify at merge time **is the standing merge protocol**, which
   already binds every author on every PR: gates green, `Closes` present and
   correct, and the head not moved past the PASS (decision 0006). Restating one
   is a reminder and is harmless. Minting a new one is either case 1 or an
   attempt to legislate per-PR, which a verdict is not for.

The sorting question, one per condition, is: **would I sign this merge if
the condition never happens?** No → it is a FAIL. Yes → it is a residue
bead. Not work at all → it is the standing protocol, already binding.

## Why the pre-merge condition cannot be kept even in a repaired form

It is tempting to keep the shape and fix the carrier — leave the review bead
open with conditions enumerated, and let the merge gate read the bead
(gqlc-vnil's option 2). Two measured facts kill it.

First, **the open bead starves the author's wake**. The close of the review
bead IS the author's resume wake; the resume pass skips an in-progress bead
carrying an open blocks-dependency (`kingdom/bin/km`, dispatch — the
actionable-iff-no-open-blocks-dep filter). A judge who holds the bead open
while asking for changes has ensured the one seat that can make them is not
woken, and mail wakes nobody but Սեդրակ. The bead must close to wake the
author, and the only closing verdict that blocks the merge is a FAIL.

Second, **a PASS spent on bytes that must then change is a PASS spent on a
doomed SHA** — decision 0006's exact finding, arrived at from the other side. If
the conditions land on the branch before the merge, what merges differs from
what the judge read by the author's implementation of the conditions: new,
unread bytes inside a merge that owes review by construction. The judge
imagined the condition's implementation; they did not read it. gqlc-mpgw's
own history shows condition-sized changes carrying subtle defects (the
delivery-witness fix needed a fix). So the pre-merge condition obliges a
re-read of the delta either way — which is the FAIL cycle, with the FAIL's
record replaced by prose and the FAIL's blocking force removed. The repaired
form reduces to a FAIL with more machinery and fewer guarantees.

The after-the-fact form (option 3 — a sweep over merged PRs against open
conditions, mailing the mayor) fails on its economics: its entire useful
output is "file a bead per unlanded condition", which the judge could have
done at verdict time for free. It is a machine for recovering from prose
what should never have been put in prose, and the town's standing bias is to
delete mechanism, not to build mechanism that launders a worse habit.

## The ruling

1. **A verdict is PASS or FAIL. There is no third shape.** A PASS is
   unconditional: it asserts the judge would sign the merge of the SHA they
   read, as it stands. (Practice sometimes writes REVISE for the blocking
   verdict; this ruling treats any non-PASS closing verdict as V.4's FAIL
   and does not legislate the synonym.)
2. **Binding content travels in bead ids, never in prose.** A judge whose
   read produces wants-but-not-blockers files them as residue beads before
   closing, and the close reason may point at them: `PASS — verdict of
   record: <link>; residue: gqlc-xxxx, gqlc-yyyy`. A close reason that
   carries an obligation only in words carries it to nobody.
3. **A FAIL prices at its delta.** The standing objection — "a trivial
   condition does not deserve a whole round" — mistakes the round's cost as
   fixed. Answering a FAIL that names one missing paragraph is an author
   editing a body and a judge reading it back: minutes, because decision 0006's
   merge-base comparison scopes the re-read to the delta. The re-review bead
   for a FAIL's answer is filed **assigned to the judge who wrote the
   FAIL** — V.4 makes that judge the adjudicator of the answer, and the
   owned pass routes an assigned bead regardless of the priority floor.
   Meanwhile gqlc-o3gj shows the conditioned PASS is not even cheap in
   practice: one of them cost two full duplicate judge rounds through
   misreading. The shape's economy is an illusion held up by not counting
   what it costs later.
4. **Why judges reached for it, answered.** The middle shape between
   block-everything and let-it-go is genuinely needed — and it already
   exists: the residue bead. A conditioned PASS is a residue bead that never
   got filed, fused with a FAIL that did not want to cost a round. gqlc-ib5y
   is the exception that proves it: its condition was satisfied and
   *verified by readback* before the merge — that judge did the FAIL cycle's
   work, minutes of it, and the shape "worked" exactly because its record
   was the only thing missing. This ruling asks for the record.

## The detector, and its deliberate modesty

One row in `km doctor` (`cmd_doctor`, kingdom/bin/km:3790): a closed
`class:judge` bead whose `closed_at` postdates this decision's merge and whose
close reason matches `conditio` (case-insensitive) is named in a **warn**
row. Time-scoped so the eight historical verdicts do not ring it forever.

Warn, not FAIL, by the doctor's own written doctrine: the stranded-bead arm
FAILs because it rests on the dispatcher's arithmetic; the
unreachable-assignee arm warns because it rests on a roster a human
maintains. This row rests on wording a human wrote, so it warns. It is made
exact rather than fuzzy by ruling 2's close-reason format: under this decision a
compliant close reason has no reason to contain the word at all — do not
write "no conditions"; the unqualified PASS says it — so a match is either
the abolished shape or a format drift worth a glance. A detector that fires
on correct behaviour trains the town to scroll; this one is designed to fire
on nothing while the ruling holds, and the execution bead is required to
watch it fire once against a synthetic violation before shipping it
(citizen-protocol step 3: a guard nobody watched fail is inert).

## The standing debt

Eight conditioned verdicts are on the record and their conditions were never
converted. gqlc-ftxp's are already carried by gqlc-mpgw (in flight);
gqlc-ib5y's was verified satisfied at close; gqlc-4juf's was CI state,
verifiable trivially. The remainder — gqlc-sk5j and gqlc-sq0d (three binding
conditions on PR #1481), gqlc-qxhp (#1707), gqlc-j87i (#1679, whose
condition "decision 0005 body edit" is ambiguous about which body — itself
evidence for ruling 2), gqlc-uwwg (#1485's successor) — are audited by the
execution bead: read each verdict of record, verify each condition **by
content** against master, and file a bead per unlanded condition with
`from-pr:<N>`. Until that audit closes, the ledger's count of
defects-reaching-master-through-unreviewed-merges (V.2.0.4) is understated
by however many of these are real.

## What this does not fix

**The open-bead hole is not this hole.** PR #1481 merged while its blocking
review bead sat open (gqlc-sniv) — a merge that outran its verdict, where
this decision's subject is a verdict that outran its own content. gqlc-sniv
remains open and this ruling closes no part of it; it does make sniv's
eventual detector cleaner, since after this decision an open judge bead against a
merged PR is unambiguous (there is no "it closed conditioned" middle state
to classify). sniv's own notes prefer a merge-path refusal over an
after-the-fact detector, and nothing here prejudges that.

**Vocabulary.** CONTEXT.md is the product's glossary (gqlc's schema and
query language) and carries no society terms; the society's vocabulary lives
in the constitution and these decisions, so "conditioned PASS", "residue",
and the PASS/FAIL shapes are defined here and CONTEXT.md is deliberately
untouched.

**The constitution is not amended.** V.2 ("merges on a Դատաւոր's PASS") and
V.4 ("a FAIL blocks until answered") already carry this ruling's whole
weight; the conditioned PASS was never constitutional text, only practice.
A decision is how this town retires a practice (0002, 0006 are precedents), and
an amendment would add review cost without adding force.
