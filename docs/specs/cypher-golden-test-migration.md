# Cypher parser tests: migrate hand-built shape expectations into the golden corpus

The implementation brief for `internal/query/cypher/parser_test.go` — a
test-topology refactor motivated by the 2026-07-12 audit that named the
file the maintenance outlier of the whole test suite. The parser's
non-test source (`build.go`, `expr.go`, `listener.go`, `pattern.go`,
`shape.go`, `typing.go`, `call.go`, `errors.go`) is untouched by this
change; test helpers and the acceptance harness are fair game.

This document is a **delta** against ADR 0004 (query parser is built
test-first against the openCypher TCK) and ADR 0008 (the `query.Query`
surface has two faces — Go API and JSON wire). Everything not stated here
carries over verbatim. Tracking: bead `gqlc-ls8.5` (GitHub #285), the
fifth of the `gqlc-ls8` five-refactor deepening epic.

> **Revision history.** v1 (commit 3538a90) proposed a ~140-case Class-A
> delete based on a heuristic classifier ("no `authored ` prefix ⇒
> shape-mirror"). Linus PASS review identified the heuristic as
> empirically wrong: the corpus-membership-mechanical classifier finds
> **43** Class-A candidates, not ~140, and 89 pins the heuristic would
> have migrated are in fact authored inputs the doctrine deliberately
> preserves. v2 restates every number from the mechanical census, drops
> the doctrine-amendment ambition (lead ruling: the 112 authored pins
> stay untouched — hand-derived shape is the independent evidence, and
> round-tripping through the parser into goldens launders current
> behavior into "expected"), and refines the delete scope one further
> layer: 9 of the 43 sig-carrying Class-A candidates need per-case sig-
> match audits before deletion, because a pin's authored sig may diverge
> from the TCK Background sig and pin a different shape than the golden.

---

## 0. Prior art in the file itself

The file's preamble (`parser_test.go:15-42`, "Layer-2 rule") pre-commits
the mustParse table to a discipline: mustParse cases come **verbatim from
the corpus**, and the hand-built `want` is the regression layer against
`-update`'s silent rebaseline. §0 asks whether that discipline still
holds at HEAD, and — under the mechanical census below — how much of
the mustParse table's shape-pinning duty can be safely retired to the
wire-face goldens.

The preamble's central hazard argument:

> The hand-built `query.Query` in each entry is the regression layer the
> golden snapshots — which `-update` silently rebaselines — cannot give
> us.

Where this argument holds: an **authored** input whose shape is derived
by hand from the model contract. Round-tripping it through the parser
to mint a golden would launder the parser's *current* behavior into
"expected", making a subsequent `-update` under a broken parser silently
rebaseline the authored shape to the broken output. The independent
evidence is precisely the hand-derived `want` — it was written from the
model contract, not from parser output.

Where this argument does NOT hold: a **verbatim TCK** input whose golden
already exists on disk. The golden was **already** minted by the parser
under `-update` (that is the mechanism goldens are minted through — see
`acceptance_test.go:1038-1060`). Every `-update` run has already
laundered the parser's current output into the golden's "expected". The
authored `want` in the mustParse pin was minted the same way (a human
watched the parser's output while typing the tree) — the two are twin
recordings of the same parser behavior. In this case the redundancy is
real; keeping the hand-built pin adds nothing the golden diff at PR
review time doesn't already carry.

The narrow deletion set this spec defends is exactly the intersection
of those two properties: (1) src is verbatim from the TCK, and (2) an
on-disk golden exists that pins the same shape the hand-built `want`
records. The 112 authored pins are outside that set and stay untouched.

---

## 1. Census — the mechanical numbers

Ran on HEAD (commit 3538a90 at v2 draft time; will re-run at v2 commit
and again per phase). Reproduction command block in §7.

```
Unique corpus queries harvested from readCoreDirs:  3805
Corpus queries with an on-disk golden:              3131
mustParse entries total:                             159

Class A. src verbatim in TCK corpus AND golden exists on disk:   43
Class B. src verbatim in TCK corpus BUT no golden on disk:        4
Class C. src NOT in TCK corpus (authored input):                112
```

### 1.1 Class-A refinement: sig-carrying pins are per-case audits

9 of the 43 Class-A pins carry an authored `sigs` field. An authored
`sigs` can diverge from the TCK's Background-declared signature for the
same scenario, in which case the parser produces a **different shape**
under the two sigs — the pin and the golden then pin different shapes
even though the src is identical. Deleting such a pin loses the
authored-sig shape assertion.

Confirmed instance (spot-verified during v2 drafting):

- Pin `"authored CALL standalone Returns signature-declaration-order"`
  runs `CALL test.my.proc(42)` against an authored sig with **two**
  result columns `(a :: INTEGER?, b :: STRING?)` to force a
  declaration-order test. The TCK's Call3[1] scenario runs the same
  src against a **one**-result-column sig `(out :: STRING?)`. The golden
  `Call3_07c57b301e10.golden.json` pins one CallBinding for `out`; the
  pin's `want` records two CallBindings for `a`, `b`. Deleting the pin
  loses the sig-declaration-order property.

Confirmed non-instance (also spot-verified):

- Pin `"CALL NUMBER accepts INTEGER standalone (Call3[1])"` runs the
  same src with the TCK's `(in :: NUMBER?) :: (out :: STRING?)` sig
  verbatim. The pin's `want` and the golden agree byte-for-byte on
  shape. Pin is safely deletable.

The 9 sig-carrying Class-A pins:

```
1. CALL NUMBER accepts INTEGER standalone (Call3[1])
2. CALL in-query YIELD RETURN (Call1[6])
3. CALL in-query explicit args YIELD RETURN (Call2[1])
4. CALL in-query no-YIELD RETURN prior-match (Call1[3])
5. CALL standalone explicit args YIELD * (Call5[8])
6. CALL standalone explicit args implicit-YIELD (Call2[2])
7. CALL standalone no-args implicit-YIELD (Call1[5])
8. CALL then WITH then CALL (Call6[1])
9. authored CALL standalone Returns signature-declaration-order
```

Delete-safety verdict for each of the 9 lands in §3.2 (the audit
step) — one line per pin, gated by comparing the pin's `sigs` to the
scenario's Background `there exists a procedure` step in the .feature
file. Estimated safe-deletable: ~6-8 of the 9; the sig-order pin
above is a confirmed keep. Actual counts land in the Phase-2 commit
message.

### 1.2 The 34 sig-less Class-A pins

These are the unambiguous shape-mirrors: verbatim TCK src, on-disk
golden, no authored-sig confounder. The full sorted list, extracted
from the census in §7:

```
all quantifier empty list, anonymous edge, arithmetic in return,
comma pattern with aliases, count distinct, create anonymous node,
delete node, detach delete node, directed edge whole entities,
edge inline-labelled endpoints, edge left-pointing canonical,
merge argument handling across with, merge labelled node returning
property, merge with both on branches, merge with inline map
referencing bound var, merge with on create set property, merge with
on match set labels, node, node inline property, node multi-label,
none quantifier empty list, optional match reuses prior binding,
optional match simple, property return no alias, set labels, skip
parameter, typed edge named endpoints, undirected edge whole
entities, unwind empty list, unwind null, unwind range function,
unwind scalar list, where property parameter, with-aggregate-where
scope snapshot (foaf)
```

**34 pins.** Delete-safe on the src+golden criterion; per-case
verification via the census-intact helper in §3.1.

### 1.3 The 4 Class-B pins (verbatim, no golden)

These pins carry TCK-verbatim srcs whose scenarios do not produce
goldens under the current acceptance harness rules. The mechanism
differs per pin:

```
1. limit parameter                          — src: MATCH (p:Person) RETURN p.name AS name LIMIT $_limit
   Scenario: ReturnSkipLimit2[10] "Negative parameter for LIMIT should fail"
   Skiplisted at acceptance_test.go:265 as catValueBelowBoundary — the TCK
   marks this scenario as a runtime fail, the harness routes it through
   shouldBeRejected, no checkGolden call.

2. aggregate count of count star            — src: RETURN count(count(*))
   Scenario: Aggregation1[14] "Aggregates in aggregates"
   Skiplisted at acceptance_test.go:288 as catGroupingKeySemantic — same
   family (TCK marks as fail; no golden minted).

3. CALL standalone no-args no-yields empty-results (Call1[1])
   — src: CALL test.doNothing()
   Scenario: Call1[1] — NOT skiplisted, but the scenario is read-side +
   Then the result should be empty (line 41). noSideEffects at
   acceptance_test.go:928 only calls checkGolden for StatementWrite; this
   is StatementRead, so no golden is minted.

4. CALL standalone implicit no-args empty-results (Call1[2])
   — src: CALL test.doNothing
   Same mechanism as #3 (Call1[2]).
```

**All 4 stay in mustParse.** No golden covers them; deleting them
would leave the shape unpinned for these scenarios entirely. They are
Class-B keepers, not Class-B deletables — the "B" label is descriptive
(verbatim TCK, no golden), not prescriptive.

### 1.4 The 112 Class-C pins (authored, not in corpus)

**Do not touch.** Per lead ruling: for authored inputs the hand-derived
shape IS the independent evidence; round-tripping them through the
parser into goldens would launder current behavior into "expected" and
`-update` would silently rebase them thereafter — exactly the
circularity the preamble forbids. Doctrine holds for this set.

Number, not name: 112 pins across every clause dir. The count is
mechanical (extracted by the census); the doctrine's rationale is in
§0 above.

---

## 2. Deletion arithmetic

**Estimated deletable pins: 34 (sig-less Class-A) + N (sig-carrying
Class-A after per-case audit), where 0 ≤ N ≤ 8.** Best case: 42
deletions; worst case (every sig-carrying pin fails the sig-match
audit): 34 deletions. The `"authored CALL standalone Returns
signature-declaration-order"` pin is a confirmed keep, bounding N at 8.

**Line-count reduction estimate:**
- 42 pins × ~15 lines each = ~630 lines deleted.
- Layer-2 preamble rewrite: net ~0 (roughly one paragraph substituted).
- `TestPropertyReadCoreParses` rewrite: net ~+5 lines.
- Scaffold add-then-remove: net 0.
- File shrinks from 4,151 → ~3,520, **a ~15% reduction**.

**What the bead's original "materially smaller" framing missed:** the
authored pins are the load-bearing majority of the file. A shape-
mirror-only migration cannot approach the 70% reduction the v1 spec
projected. The 15% is the honest number the acceptance criterion
should be measured against.

**What the migration buys:**

1. **Removes 34-42 pins of confirmed pure duplication** — the wire-face
   golden and the Go-face `want` for these pins are twin recordings of
   the same parser output; keeping both doubles the maintenance surface
   for zero incremental defense against parser regressions (the
   goldens' 3131-file diff at PR time is the reviewer's actual defense,
   and adding 42 hand-typed twins to that surface adds nothing).

2. **Truthfully names the model-rebaseline cost for the survivors.**
   Additive-axis rebaselines (hk0: 20 goldens; 0ig: 28; ay9: 100) touch
   BOTH sides today — the goldens rebaseline via `-update`, and the
   hand-built pins get manually edited to match. After this migration,
   the 34-42 deleted pins are one `-update` away; the 116-125 surviving
   pins still need manual edits when the axis they pin changes. The
   ay9 cycle's 100-golden rebaseline would still require touching every
   authored pin whose shape mentions `OptionalGroup`. The migration
   does not close that surface; it only closes the false-flag part of
   it (the twinned mirrors that were rebaselining alongside the goldens
   for no marginal defense).

3. **Aligns the file with its own preamble.** The preamble's "verbatim
   TCK only" rule is honored in 43 of 159 pins at HEAD; the other 112
   are authored exceptions the doctrine survives by way of an implicit
   "unless the corpus is silent on the shape" carve-out that isn't
   stated in the preamble text. Phase 3 of the migration rewrites the
   preamble to name that carve-out explicitly (see §5 phase 3).

---

## 3. Coverage-no-shrink verification

### 3.1 The census-intact helper (Phase 1 addition, Phase 3 removal)

Phase 1 adds `TestMigrationCensusIntact` as a first-class test in
`parser_test.go`. It walks the sig-less Class-A key list (§1.2) —
committed inline in the test as a `[]string` literal — and asserts,
per key:

1. The pin exists in `mustParse` at the current HEAD.
2. The pin's `src` is verbatim in `harvestExecutingQueries(t,
   readCoreDirs)` output (using the existing corpus-harvester the
   property tests already use).
3. The golden file for the (uri, name, query) triple exists on disk.

Phase 2 deletes the 34 sig-less Class-A pins in one commit. The
per-pin deletion order matches the key list, and each pin's removal
removes its census-intact row. Phase 2's commit body carries a
per-pin receipt of the form:
```
<key>                          -> testdata/golden/<goldenPath>
```

Between Phase 2 and Phase 3, the sig-carrying-Class-A audit lands
(§3.2 below). Its output is either:
- an additional per-pin deletion (with receipt), or
- an in-file comment on the surviving pin naming the sig-divergence
  reason (see the `"authored CALL standalone Returns signature-
  declaration-order"` example).

Phase 3 deletes `TestMigrationCensusIntact` alongside the Layer-2
preamble rewrite (both are cleanup — the test's job was to gate the
Phase-2 deletions; once done, it is scaffolding).

### 3.2 The sig-carrying Class-A audit (Phase 2)

For each of the 9 sig-carrying Class-A pins:

1. Read the pin's `sigs` from `parser_test.go`.
2. Read the scenario's Background `there exists a procedure` step from
   the corresponding .feature file (identified by the pin's key
   suffix, e.g. `Call3[1]` → `test/data/query/cypher/tck/features/
   clauses/call/Call3.feature`).
3. Parse both signatures into `procsig.Signature` and compare.
   - If equal: pin is deletable (like `Call3[1]`).
   - If unequal: pin stays, with a comment added naming the sig
     divergence and its shape-assertion purpose (like the sig-order
     pin).

The audit runs as part of Phase 2's commit (not as a phase of its
own — 9 pins is a small enough set to audit in one sitting). Its
result is folded into Phase 2's receipts.

### 3.3 The residual hazard (unchanged from v1 §3.4)

A parser regression that produces broken output for **every** corpus
query would flip every golden under `-update`. This migration does
not close that hazard; it never closed for the ~95% of scenarios with
no mustParse twin. The defense is the golden-diff review at PR time,
which we already rely on for those ~3050 scenarios. Extending the
same defense to the 34-42 that had a redundant twin adds no marginal
risk.

The 116-125 surviving pins (Classes B, C, and any Class-A sig-
carrying keepers) still act as their own defense: each one records
the shape independently of the parser's output, so a broken parser
that rewrote a golden under `-update` would still fail the matching
mustParse case. The proportion of scenarios with this defense drops
slightly (from 159/3199 = 5.0% to ~120/3199 = 3.8%), but the specific
scenarios that lose the defense are precisely those where the golden
already carries an equivalent assertion — a distinction the preamble
does not make and this spec does.

### 3.4 `TestPropertyReadCoreParses` (Blocker-3 fix — Phase 1)

`parser_test.go:3912-3928` today iterates `mustParse` by reference,
sampling `len(mustParse)` pins for the parser-parses precondition
check:
```go
pins := make([]pin, 0, len(mustParse))
for _, c := range mustParse { pins = append(pins, ...) }
```
Deleting 34-42 mustParse entries silently shrinks the property
test's sample space by that amount.

**Fix (Option 1 from the Linus-3 grill):** rewrite the test to
iterate `corpusQueries(t)` directly, matching the test's own docstring
("every curated read-core query must parse") and the corpus-driven
posture `TestPropertyReferentialIntegrity` already uses (see
`parser_test.go:3934`). The rewrite lands in Phase 1 alongside the
census helper — before any mustParse pin is deleted — so no phase
silently degrades this test.

The rewritten test still runs `newParserFor` per pin; the sigs slice
becomes the union of every scenario's Background-declared signatures,
harvested by extending the `thereExistsAProcedure` step handler with
a query-scope collector (the acceptance test already parses these
sigs — the collector is an additive helper, not a re-implementation).
Alternative: build the parser once with an empty registry and skip
CALL-carrying corpus queries (`isCallCarrying(q)` predicate on the
harvest side). Phase 1 picks the simpler of the two based on how many
corpus queries carry CALL clauses (harvest count landing in the
Phase-1 commit body).

**Options 2 and 3 (rejected):**
- **Option 2** — rename to `TestPropertyMustParseCorpusParses` and
  accept the shrink. Rejected: the shrink is silent to future readers
  and the test's docstring lies.
- **Option 3** — delete the test. Rejected: `TestPropertyReferential-
  Integrity` returns nil for corpus queries the parser rejects
  (`parser_test.go:3939`), which means a regression where every query
  suddenly rejects would pass vacuously. The parses-precondition
  guard is a real defense.

---

## 4. Fixture naming/layout

No new goldens (§2 v1 argument unchanged and reinforced by the
narrower scope). The layout under `internal/query/cypher/testdata/
golden/` is unchanged. `TestGoldenOrphans` invariant preserved.

No new file lands from this bead. `TestMigrationCensusIntact` lives
in `parser_test.go` for the duration of Phases 1-3 and is deleted in
Phase 3. No companion Markdown census file (v1's proposal) — the
in-test `[]string` literal is the ledger, and having it live in Go
gives the compiler + test runner the same visibility a reviewer has
into the migration.

---

## 5. Phasing

Three commits. Each leaves `just test` + `just fmt-check` + `just
lint` green, keeps TCK counts unchanged (`3897 scenarios — 3459
passed, 438 pending; 16006 steps — 15568 passed, 438 pending; 0
failed`), and keeps every existing golden byte-identical.

**Phase 1 — scaffold + property-test fix.** One commit.
- Adds `TestMigrationCensusIntact` with the 34-key sig-less
  Class-A list as an inline `[]string`.
- Rewrites `TestPropertyReadCoreParses` per §3.4 to iterate the
  corpus.
- Adds a scratch `census_test.go` (excluded via build tag if
  needed; alternatively runs only in `-run TestMigrationCensus.*`
  mode) that runs the census probe end-to-end, printing the 3-way
  bucket counts. This exists so the numbers in this spec are
  reproducible by any reviewer running one command.
- No mustParse deletions.
- Subject: `test(cypher): census scaffold + corpus-driven parses
  precondition (gqlc-ls8.5 phase 1)`.

**Phase 2 — bulk deletion.** One commit.
- Runs the 9-pin sig-carrying audit (§3.2), producing 0-8 additional
  deletable pins (goal: audit result appears in the commit body).
- Deletes the 34 sig-less Class-A pins + the audit-approved sig-
  carrying subset.
- Updates `TestMigrationCensusIntact`'s inline list (removed rows
  disappear from the list; the test still passes because it now
  asserts intact-ness over the smaller residual).
- Prunes unused test helpers only if any go completely unreferenced
  (the `oneBranch`, `oneWriteBranch`, `must`, `markBareRefNode`
  helpers are load-bearing for the 116-125 survivors — do not
  touch).
- Subject: `test(cypher): delete N shape-mirror parser pins
  (gqlc-ls8.5 phase 2)`, with N = 34 + audit-approved count landing
  in the commit body.

**Phase 3 — preamble rewrite + scaffold retirement.** One commit.
- Rewrites `parser_test.go:15-42` "Layer-2 rule" preamble to name
  the honest post-migration doctrine:
    (a) mustParse pins whose src is verbatim TCK AND whose want
        equals the golden's shape are DELETED (per this bead's
        cleanup);
    (b) mustParse pins whose input is authored (not in the corpus)
        stay — the hand-derived want is the independent evidence
        against `-update` silent-rebaseline;
    (c) mustParse pins whose input is verbatim TCK but whose golden
        does not exist (skiplisted / read-side-empty-result — the
        Class-B set) stay by the same argument as (b);
    (d) mustParse pins whose input is verbatim TCK but whose `sigs`
        diverge from the TCK Background (a specific case of (b))
        stay.
- Deletes `TestMigrationCensusIntact` and the scratch `census_test.go`.
- Deletes any test helpers that phase 2's deletions rendered
  unused (recheck: unlikely, but possible for helpers used only by
  the 34-42 deleted cases).
- Subject: `docs(cypher): rewrite parser_test Layer-2 preamble
  post-migration + retire scaffold (gqlc-ls8.5 phase 3)`.

Each phase is pushed as a commit for incremental Linus review, per
workflow-graphite-stacked-branches convention. `just test /
fmt-check / lint` gate every commit.

---

## 6. What remains in `parser_test.go` when done

**Final size estimate:** 4,151 → ~3,520 lines, a ~15% reduction.

Composition of the final file:

| Section | Approx lines | Justification for hand-built survival |
|---|---|---|
| Layer-2 preamble (rewritten) | ~50 | Design charter — states the four-way keep-vs-delete rules |
| `oneBranch` / `oneWriteBranch` / `must` / `markBareRefNode` helpers | ~30 | Constructors used by 116-125 residual pins |
| `mustParse`: Class C (112 authored) | ~2,300 | Load-bearing authored shape assertions; doctrine holds |
| `mustParse`: Class B (4 verbatim-no-golden) | ~80 | Only shape assertion for these scenarios |
| `mustParse`: Class-A sig-carrying keepers | 0-160 | Authored sigs distinguish shape from golden |
| `TestMustParse` runner | ~10 | Unchanged |
| `mustReject` (25 entries) + `TestMustReject` | ~340 | Reject-path pins the sentinel; no shape to outsource |
| `mustRejectGrammar` (3 entries) + runner | ~70 | Grammar-reject; nothing to outsource |
| `allSentinels` + `TestSentinelReachability` | ~35 | Unchanged |
| `corpusQueries` + property tests + assertions | ~240 | Untouched, per bead brief |

The four categories earning hand-built status post-migration:

1. **Class C — authored inputs** (112 pins): the hand-derived shape
   IS the independent evidence; the doctrine's core case.
2. **Class B — verbatim-no-golden** (4 pins): no wire-face pin
   exists, so the hand-built pin is the only shape assertion.
3. **Class A sig-divergent** (0-8 pins after audit): src matches
   TCK but the authored `sigs` field produces a shape the golden
   does not carry.
4. **Reject-path pins** (25 mustReject + 3 mustRejectGrammar):
   assertion is emptiness of the model on rejection; nothing to
   outsource to the golden surface.

A future model-change rebaseline (ay9-style: 100 goldens) still
requires manual edits to any residual pin whose shape mentions the
changed axis. The migration does not remove this cost — 116-125
pins can still touch `optionalGroup`, `containsAggregate`,
`args[]CallArg`, etc. What it removes is the false-flag cost: 34-42
mirror pins that were touching every additive axis for zero
marginal defense.

---

## 7. Fence commands + reproduction

**Census reproduction.** Requires the worktree root as cwd:
```
go run /tmp/censusprobe/main.go   # Linus's probe, preserved for reference
                                  # (or the census_test.go added in Phase 1)
```
Expected output at HEAD (commit 3538a90):
```
Unique corpus queries harvested: 3805
Corpus queries with an on-disk golden: 3131
mustParse srcs: 159
  A. in corpus AND golden exists: 43
  B. in corpus BUT no golden: 4
  C. NOT in corpus: 112
```

**TCK-counts fence.** Must remain unchanged at every phase:
```
go test ./internal/query/cypher/ -run TestReadCoreAcceptance -v 2>&1 | tail -20
# expect: 3897 scenarios (3459 passed, 438 pending, 0 failed);
#         16006 steps (15568 passed, 438 pending, 0 failed).
```

**Golden byte-identity fence.** Zero changed files:
```
git diff --stat master internal/query/cypher/testdata/golden/
```

**Orphan-golden fence.** Must pass at every phase:
```
go test ./internal/query/cypher/ -run TestGoldenOrphans
```

**Standard commit gates.** Every commit:
```
just test && just fmt-check && just lint
```

**Invariant sweep sanity.** Spot-run at each phase to confirm
property coverage did not silently shrink:
```
go test ./internal/query/cypher/ -run TestPropertyReferentialIntegrity \
    -v -rapid.checks=1000
```

**Sig-audit reference** (Phase 2). For each of the 9 sig-carrying
Class-A pins, compare pin `sigs` to the scenario's TCK Background:
```
# For pin "CALL NUMBER accepts INTEGER standalone (Call3[1])":
grep -A5 "there exists a procedure" \
    test/data/query/cypher/tck/features/clauses/call/Call3.feature | head -20
# Compare textually with the pin's sigs field at parser_test.go.
```

---

## 8. Non-goals

- No parser source changes (`build.go`, `call.go`, `expr.go`,
  `listener.go`, `parser.go`, `pattern.go`, `shape.go`, `typing.go`,
  `errors.go`). Test helpers may be pruned in Phase 3 if any go
  unused, but only via evidence, not speculation.
- No doctrine amendment for the 112 Class-C authored pins. Per lead
  ruling, the doctrine stands.
- No changes to the golden hashing scheme, orphan check, scenario-
  URI keying, or acceptance harness's outer shape (aside from the
  `TestPropertyReadCoreParses` corpus rewrite in Phase 1).
- No mustReject side migration. `require.Equal(t, query.Query{},
  got)` asserts emptiness; there is no shape to outsource. This side
  is not the bead's target.
- No changes to `sink_fence_test.go`, `TestSentinelReachability`,
  `TestPropertyReferentialIntegrity`.
- No introduction of hand-authored `.golden.json` files. No second
  golden directory.
- No new TCK vendoring, feature dir, or `.feature` file edit. TCK
  count baseline is fixed for the duration of this bead.

---

## 9. Open questions

**Q1 (Phase-2 output → §6 residual estimate).** After the sig-
carrying audit, the residual mustParse count is 116 + (9 - N) where N
is the number of sig-carrying pins the audit approves for deletion.
The final line-count estimate in §6 is 3,520 assuming N ≈ 6. If N is
materially different (say N = 0 or N = 9), the Phase-3 preamble
rewrite absorbs the difference in its own text but the ~15% file-
reduction claim is a floor. Q1 is a note, not a decision.

**Q2 (Class-B semantics naming).** Two of the 4 Class-B pins are
skiplisted; two are read-side-empty-result. The Phase-3 preamble
rewrite must name both mechanisms honestly (avoid the "skiplisted"
label as a superset — it's a proper subset). Language locked in
Phase 3 with Linus in the loop.

**Q3 (follow-up bead scope).** If the maintenance-outlier headline
of gqlc-ls8.5 is only partially closed by a ~15% reduction (which it
is), the natural follow-up is a bead scoped to "authored pin
consolidation across staged specs" — a doctrine-preserving
refactor that groups the 112 by stage (Stage 4 UNION pins, Stage 12
write pins, Stage 14 CALL pins, etc.) so a model-axis change at a
specific stage touches a contiguous section of the file rather than
scattered pins. Not in this bead's scope. Filed as follow-up at
ls8.5 close-out, per lead's ruling on scope discipline.

---
