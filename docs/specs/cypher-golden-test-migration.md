# Cypher parser tests: migrate hand-built shape expectations into the golden corpus

The implementation brief for `internal/query/cypher/parser_test.go` — a test-topology
refactor motivated by the 2026-07-12 audit that named the file the maintenance
outlier of the whole test suite. The parser's non-test source (`build.go`,
`expr.go`, `listener.go`, `pattern.go`, `shape.go`, `typing.go`, `call.go`,
`errors.go`) is untouched by this change; test helpers and the acceptance
harness are fair game.

This document is a **delta** against ADR 0004 (query parser is built test-first
against the openCypher TCK) and ADR 0008 (the `query.Query` surface has two
faces — Go API and JSON wire). Everything not stated here carries over
verbatim. Tracking: bead `gqlc-ls8.5` (GitHub #285), the fifth of the
`gqlc-ls8` five-refactor deepening epic.

---

## 0. Prior art in the file itself

The file's own preamble (`parser_test.go:15-42`, "Layer-2 rule") argues
**against** the migration this spec undertakes. It has to be answered squarely
before design, not around.

The preamble's claim:

> The hand-built `query.Query` in each entry is the regression layer the
> golden snapshots — which `-update` silently rebaselines — cannot give
> us.

The preamble is right about a real hazard (a `-update` under a broken parser
would rebaseline the goldens to the broken output silently) and wrong about
the remedy. Two facts the preamble does not weigh:

**Fact 1 — `-update` is a full-file operation, not per-fixture.** Running
`go test -update ./internal/query/cypher/...` rewrites **every** `.golden.json`
that changed shape, in a single pass. If a parser regression flipped one
scenario's output, `-update` flips its golden the same way it flips the other
3199. The current 159 hand-built mustParse pins guard against exactly 159 of
those 3199 scenarios — 5% coverage. The **other 95%** ride entirely on the
golden-diff review that lands in the same PR as the parser change. The
argument that hand-built pins are an *effective* line of defense against a
silent-rebaseline hazard requires them to cover the hazard's actual surface;
they cover a token 5% slice.

**Fact 2 — the actual defense is the golden diff itself.** Recent additive
axes (`hk0`: 20 goldens; `0ig`: 28; `ay9`: 100; `5xg` / `qcc` / `fvo`: 0)
land as amendment notes on ADR 0008 with the golden rebaseline **inventoried
in the amendment** and reviewed line-by-line. That is where a parser
regression would be caught: not in a randomly-selected 5% of scenarios
handwired into `parser_test.go`, but in the PR reviewer looking at the
diff of the JSON that will be emitted to every future consumer.

The preamble's own commentary weakens the "regression layer" claim in
another direction. It restricts mustParse cases to **verbatim TCK queries**
("REJECT-PATH cases … come VERBATIM from the corpus … We add a mustParse
case only when the corpus supplies one"), which means every case whose
handbuilt expectation the preamble defends is a query the acceptance suite
**also parses**, producing a golden. The two files agree on the input; they
disagree on the expected output only if the parser is broken. In the
broken-parser world, they will *both* be wrong — one via silent `-update`,
the other via a manual expectation table the same PR author will hand-edit
to match.

The preamble is correct that hand-built pins offer *some* value the goldens
do not: they express the shape in Go source, which is grep-able, diff-able
against the model surface at review time, and self-documenting through the
constructor names (`must(query.NewNullableEdgeBindingInGroup(...))`). This
spec preserves that value where it earns its keep — for shapes that pin
axes not otherwise visibly asserted in the test surface — and retires it
where the goldens already carry the exact same shape assertion.

The net effect: the shape-pinning duty consolidates on the wire-face
goldens (ADR 0008's naming — the "single shape-pinning surface" per the
bead brief); the Go-face tests shrink to invariants (referential integrity,
uniqueness, dedup — all already in `TestPropertyReferentialIntegrity`),
smart-constructor probes (adversarial inputs that must reject), and
authored kill probes (fail-sites the TCK does not exercise at the pinned
tag).

---

## 1. Census

The file has 4,151 lines, three top-level maps of test cases, one property
test file's worth of invariant assertions, and 26 uses of the string
"AUTHORED" flagging hand-authored (non-verbatim-TCK) inputs.

| Map | Lines | Entries | Verbatim TCK | Authored (`"authored …"` key) |
|---|---|---|---|---|
| `mustParse` | 79-3367 | 159 | 149 | 10 |
| `mustReject` | 3420-3748 | 25 | ~15 | ~10 (see below) |
| `mustRejectGrammar` | 3768-3822 | 3 | 3 | 0 |

The 10 named-`authored` mustParse cases (grepped by `^\t"authored`):

```
authored merge in exists does not flip statement kind        (line 2825)
authored merge branch-leak kill probe                        (line 2848)
authored create inline map var prop bound guard              (line 2876)
authored CALL inside EXISTS suppression                      (line 3086)
authored CALL bound-var argument regression lock             (line 3253)
authored CALL standalone Returns signature-declaration-order (line 3278)
authored CALL YIELD trailing WHERE parameter-mining probe    (line 3343)
```
(+ three more named-`authored` mustReject entries not counted above.)

Beyond the map-key `authored` prefix, some verbatim-TCK-named cases still
carry semantic content the golden cannot express — they force adjacent
shapes that would otherwise not co-occur in a single scenario. Two examples:

- `"required chain re-reference does not set bare-ref flag"`
  (`parser_test.go:435`) is a `5xg` **kill probe** whose *point* is that a
  specific bit is `false`. Its golden encodes the axis with
  `,omitempty`, so `false` is byte-identical to "not emitted" — the golden
  literally cannot distinguish "flag is false" from "field is absent". The
  Go-face assertion carries a positive-negative axis the wire deliberately
  drops.

- `"5xg spec §5.1.2 kill probe"` cases in the same section
  (`parser_test.go:411-461`): both are verbatim TCK, but the pin's *point*
  is that one sets a flag and the other doesn't — a two-case discrimination
  the golden cannot express in a single pin.

Everything else in mustParse is a shape-mirror of exactly one golden:
`(src, want)` where the golden is `sha1(uri + "\x00" + name + "\x00" +
src)`. Under the "verbatim TCK" rule, the src is a query the acceptance
suite is already running, and the golden for its scenario is on disk.

**Migration classification:**

| Class | Count | Disposition |
|---|---|---|
| A. Shape-mirror of an existing golden | ~140 | **Migrate — delete** |
| B. Positive-negative discrimination (a bit that must be false / absent) | ~10 | **Keep as hand-built** — the golden cannot express it |
| C. Named `authored` (no TCK scenario at the pinned tag) | 10 | **Keep as hand-built** — no golden to migrate to |
| D. mustReject verbatim | ~15 | **Keep** — reject-path asserts absence of a model; nothing to pin |
| E. mustReject authored | ~10 | **Keep** — same as D, with the additional revisit-on-TCK-bump obligation |
| F. mustRejectGrammar | 3 | **Keep** — grammar-level assertion |

Class A is the migration target. Numbers are floor estimates; the actual
per-case classification lives in the §5 phase logs (each commit reports
its delta).

---

## 2. What "migration" means, mechanically

The bead brief phrases migration as "shape-pinning cases into the golden
corpus (new fixtures where an existing golden does not already cover the
case)". Reading the acceptance harness in detail (`acceptance_test.go:597-660`,
`checkGolden`, `goldenPath`, `TestGoldenOrphans`) exposes a hard constraint
the brief's phrasing hides: **new goldens cannot be minted by hand-authored
Go tests**. Every golden on disk is checked against the TCK feature files
by `TestGoldenOrphans` (`acceptance_test.go:609`): any file with no
corresponding `(uri, name, query)` triple in the corpus fails the test.

So "migrate the case into the golden corpus" resolves to one of two moves,
not one:

**Move M1 — delete the mustParse case** (Class A). If the case's `src` is
verbatim from a TCK scenario, its golden is already on disk and its shape
is already pinned by `checkGolden`. Deleting the mustParse entry loses only
the redundant hand-built shape-mirror; the invariant properties in
`TestPropertyReferentialIntegrity` continue to sample every corpus query
that parses, so the sampling density does not shrink.

**Move M2 — keep the case** (Classes B, C, D, E, F). The case pins
something the golden corpus cannot express (a two-case bit discrimination,
an authored non-TCK shape, or a reject-path). It stays in `parser_test.go`
verbatim.

There is **no third move** where we mint a hand-authored golden file for a
non-TCK query. `TestGoldenOrphans` forbids it, and rewriting the orphan
check would either weaken the check (an orphan golden's decay would go
silent — the exact hazard the check exists to prevent) or add a second
golden directory the check partitions, which is more machinery than the
migration deserves.

**Consequence for the bead brief's "new fixtures where an existing golden
does not already cover the case" clause:** at the pinned TCK tag, we do not
mint new goldens. Every Class-A case has an existing-golden counterpart
(that is the enabling property that puts it in Class A); every case
without one is not Class A and stays hand-built. The migration is
**subtractive only** on the mustParse table, additive nowhere.

---

## 3. Coverage-no-shrink verification (the crux)

The bead brief calls out this section as "the crux linus-3 will grill
hardest. Hand-waving dies in review." Fair. The verification is
mechanical, not narrative:

### 3.1 The mapping table

For every mustParse case in Class A, before deleting it, the migration
commit adds a row to `docs/specs/cypher-golden-test-migration-census.md`
(a companion appendix to this spec, not this file) with these five
columns:

| mustParse key | TCK feature file (`uri`) | TCK scenario (`name`) | goldenPath | shape assertion witnessed by |
|---|---|---|---|---|
| `"node"` | `Match1.feature` | `Match non-existent nodes returns empty` | `Match1_dbe2f0d6bd84.golden.json` | `checkGolden` step in `resultShouldBe` |

The last column names the acceptance harness step that assertion-fires on
this golden on every `go test` run — proof that the shape is under an
active assertion after the mustParse entry is deleted, not just present in
a static file.

Each row is verifiable in three ways any reviewer can run in seconds
(commands below):

1. The golden file exists on disk at the named path.
2. The golden's SHA1 hash reproduces from the scenario's URI, name, and
   query text (via `python -c 'import hashlib; print(hashlib.sha1(...))'`
   or an equivalent one-liner).
3. Running just the acceptance suite (`go test ./internal/query/cypher/
   -run TestReadCoreAcceptance`) with the golden **corrupted** by a
   single-character edit produces a failing assertion for that scenario
   name.

Rows are validated mechanically by a one-shot helper `go test -run
TestMigrationCensusIntact` added in the first migration commit and removed
in the final commit (see §5 phasing). The helper reads the census, walks
each row, and asserts (1) and (2) machine-checkably; (3) is a
one-per-commit spot check the migration commit's message names.

### 3.2 The kill-probe residual: what the census cannot witness

The kill-probe cases (Class B, ~10 entries) pin the *absence* of a bit
that the wire's `,omitempty` collapses to "not emitted". These are
**not migrated**. The census does not list them (they are not Class A).
Preserved in place, they continue to defend against a parser regression
that would flip the bit true on the negative case.

### 3.3 What the invariant sweep already covers

`TestPropertyReferentialIntegrity` (`parser_test.go:3934-3946`) samples
the entire read-core corpus (every executing-query docstring from every
feature dir), running `assertReferentialIntegrity`,
`assertNamedBindingsUnique`, `assertParametersDeduped`. That is a
mechanically-generated referential-integrity + uniqueness + dedup
assertion over every corpus query that parses, not just the 149 verbatim
mustParse pins. **Deleting the Class-A mustParse cases does not shrink
the invariant sampling density** because the invariant tests were never
reading the mustParse table — they read the corpus directly. Coverage
of the *invariant* dimension is unchanged by the migration.

Coverage of the *exact-shape* dimension is what the goldens carry.
Coverage of the *positive-negative bit discrimination* dimension is
what Class B keeps.

### 3.4 The one hazard the migration does not close

The preamble's stated hazard remains real: a parser regression that
produces broken output for **every** corpus query would flip every
golden under `-update`. The migration does not close this hazard; it
never fully closed for the ~95% of scenarios with no mustParse pin
already. The defense is the golden-diff review at PR time, which we
already rely on for the 3050 scenarios that never had a mustParse
counterpart. We take on no additional risk by extending the same
defense to the remaining ~140 that had a redundant twin.

The migration is not a claim that the goldens are a *stronger* defense
than the mustParse pins were. It is a claim that the pins were, in
Class A, a defense the goldens **also** offered, and the maintenance
cost of maintaining both was disproportionate to the marginal defense
the Go-face pins added.

---

## 4. Fixture naming/layout

No new goldens (§2). The layout under `internal/query/cypher/testdata/golden/`
is unchanged. No naming convention needs to be introduced.

The one new file this migration adds is the census appendix
`docs/specs/cypher-golden-test-migration-census.md`, a Markdown table
serialised in the order the migration commits process it (see §5). The
census is deleted in the final commit — it is scaffolding for the review,
not a lasting artefact.

The one new test this migration adds is `TestMigrationCensusIntact` in
`internal/query/cypher/parser_test.go`, the helper that mechanically
validates the census against the corpus + on-disk goldens. It lives in
the tree only for the duration of the migration and is deleted in the
final commit alongside the census.

---

## 5. Phasing

Six independently-gated commits. Each commit leaves `just test` +
`just fmt-check` + `just lint` green, keeps the TCK counts unchanged
(`3897 scenarios — 3459 passed, 438 pending; 16006 steps — 15568 passed,
438 pending; 0 failed`), and keeps every existing golden byte-identical.

**Phase 1 — census scaffold (no deletions yet).**
Adds `docs/specs/cypher-golden-test-migration-census.md` populated with
every mustParse key + its inferred `(uri, name, query, goldenPath)` row,
generated by a scratch script committed alongside as
`internal/query/cypher/gen_census_test.go` (a `_test.go` file so it does
not enter production builds). Adds `TestMigrationCensusIntact` in
`parser_test.go` — it walks the census and asserts each row's golden
exists and hashes correctly. First commit is green because no cases are
deleted; the census is data, the intact-check is a new test that passes
against the current tree. Subject: `test(cypher): census the parser
mustParse pins for golden migration (gqlc-ls8.5 phase 1)`.

**Phase 2 — red receipts for Class A.**
For a **sample of 5 diverse cases** from Class A (chosen to span
`Match`, `Return`, `With`, `Create`, `Call` — one per major clause dir),
add adjacent kill-probe commentary demonstrating each case's shape
assertion is actually witnessed by its golden. The demonstration is a
locally-run receipt captured in the commit message: (a) delete the
mustParse case, (b) perturb the parser to change the shape (single-line
`git stash`-safe edit), (c) show the acceptance suite's checkGolden
fails on the named scenario, (d) revert the perturbation. The
perturbation is NOT committed; the receipts live in the commit message
prose. This phase's commit removes the 5 sampled cases and updates the
census. Subject: `test(cypher): migrate 5-way sample of parser mustParse
pins to goldens (gqlc-ls8.5 phase 2)`.

The red-receipt phase is deliberately smaller than the bulk phase so the
technique is validated on a small blast radius before the batch. If the
sample surfaces a case where the golden does not in fact pin what the
mustParse pin pinned, that case is reclassified (probably Class B) and
the spec is amended before phase 3.

**Phase 3 — bulk migration, clause-dir batches.**
Delete the remaining Class-A cases in three commits, one per group of
clause dirs:

- **3a**: `Match`, `Return`, `MatchWhere`, `ReturnSkipLimit` (~60 cases)
- **3b**: `With`, `Union`, `Unwind`, `ReturnOrderby`, all `expressions/*`
  aggregate + expression pins (~50 cases)
- **3c**: `Create`, `Delete`, `Set`, `Remove`, `Merge`, `Call` write &
  procedure pins (~25 cases)

Each commit deletes its cases, updates the census (removes the migrated
rows), and re-runs `TestMigrationCensusIntact` to keep the residual
rows honest. Subjects:
`test(cypher): migrate read-core parser mustParse pins to goldens
(gqlc-ls8.5 phase 3a)`, `... expression + WITH ... (phase 3b)`,
`... write + CALL ... (phase 3c)`.

**Phase 4 — Layer-2 preamble rewrite.**
The `parser_test.go` preamble (lines 15-42) is the file's design charter
and now describes a topology this migration retired. It is rewritten to
name the new rule set (`Class B kill probes stay hand-built`, `Class C
authored stays hand-built until TCK bump`, `wire-face goldens are the
shape-pinning surface`), with the historical hazard note preserved as a
"we deliberately do not close" paragraph pointing at the ADR 0008
amendment-diff review as the defense. Subject: `docs(cypher): rewrite
parser_test Layer-2 preamble to reflect post-migration test topology
(gqlc-ls8.5 phase 4)`.

**Phase 5 — scaffold retirement.**
Deletes `internal/query/cypher/gen_census_test.go`,
`TestMigrationCensusIntact`, and
`docs/specs/cypher-golden-test-migration-census.md`. The census's job
was to give phases 2 + 3's reviewers a machine-checkable ledger of what
was being deleted; once the deletions land, the ledger is a rusting
appendix. Subject: `test(cypher): retire golden-migration scaffold
(gqlc-ls8.5 phase 5)`.

Each phase is pushed as a stacked commit for incremental Linus review,
per the workflow-graphite-stacked-branches convention. `just test /
fmt-check / lint` gate every phase.

The TCK count regression check is trivial: `go test
./internal/query/cypher/ -run TestReadCoreAcceptance -v 2>&1 | tail -20`
reports the counters at the end of every run. Any change to those counts
fails the phase.

---

## 6. What remains in `parser_test.go` when done

**Estimated final size:** ~1,000-1,200 lines (a ~70% reduction from
4,151). Rough decomposition:

| Section | Approx lines | Why hand-built survives |
|---|---|---|
| Layer-2 preamble (rewritten) | ~40 | Design charter for the file, one paragraph shorter |
| `oneBranch` / `oneWriteBranch` / `must` / `markBareRefNode` helpers | ~20 | Constructors used by residual pins |
| `mustParse`: Class B kill probes | ~150 | Positive-negative bit discriminations goldens cannot express |
| `mustParse`: Class C authored | ~200 | Non-TCK shapes with revisit-on-bump obligation |
| `TestMustParse` runner | ~10 | Unchanged |
| `mustReject`: verbatim + authored | ~330 | Reject-path pins the sentinel + zero-value Query |
| `mustRejectGrammar` | ~55 | Grammar-reject pins |
| `TestSentinelReachability` + `allSentinels` | ~35 | Bidirectional sweep unchanged |
| `corpusQueries` + property tests + assertions | ~240 | Untouched, per bead brief |
| `TestMustReject` / `TestMustRejectGrammar` runners | ~15 | Unchanged |

The four category earners for hand-built status, per the rewritten
preamble:

1. **Adversarial inputs** (mustReject verbatim + authored, mustRejectGrammar
   — the reject path has no shape to outsource to a wire assertion, per
   the current preamble's own argument, which survives the migration
   verbatim on the reject side).
2. **Smart-constructor invariants** (Class B kill probes — a
   positive-negative bit discrimination the wire's `,omitempty`
   deliberately collapses; the Go-face assertion is the only place the
   discrimination lives).
3. **Property tests** (`TestPropertyReadCoreParses`,
   `TestPropertyReferentialIntegrity` and the four assertion helpers —
   these sample the corpus and check invariants that are quantifier
   statements, not shape statements, so they belong in Go source
   naturally).
4. **Sentinel-reachability sweep** (`TestSentinelReachability` — a
   set-theoretic property over the sentinel catalog and the mustReject
   coverage set; likewise a Go-native concern).

A future model-change rebaseline (the recent ay9 cycle rebaselined 100
goldens, hk0 rebaselined 20, 0ig rebaselined 28) should be exactly one
`go test -update ./internal/query/cypher/...` plus **zero** manual edits
to `parser_test.go`'s expectation tables — the maintenance-outlier
condition the bead named as the acceptance-criteria pass criterion.

Class B and Class C cases may still need manual edits if the model
change affects the axis they pin (the ay9 cycle almost certainly touched
`optionalGroup` in the Class B pins at `parser_test.go:411-461`, for
example). Those edits are load-bearing — the pin exists to assert on
the axis that changed — and are not a maintenance overhead the
migration set out to remove.

---

## 7. Fence commands

- `go test ./internal/query/cypher/... -run TestReadCoreAcceptance -v
  2>&1 | tail -20` — reports the TCK counts; must be unchanged from the
  master baseline (`3897 scenarios — 3459 passed, 438 pending; 16006
  steps — 15568 passed, 438 pending; 0 failed`) at the end of every
  phase.
- `git diff --stat master internal/query/cypher/testdata/golden/`
  — must show zero changed files at the end of every phase (byte-
  identical existing goldens; the migration adds no new goldens).
- `go test ./internal/query/cypher/... -run TestGoldenOrphans` — must
  pass at every phase; a new orphan is a red-flag misplaced test golden.
- `just test && just fmt-check && just lint` — gates every commit.
- `go test ./internal/query/cypher/... -run TestPropertyReferentialIntegrity
  -v -rapid.checks=1000` — the invariant sweep at 10× the default
  sample count, spot-run at each phase-3 batch commit to confirm the
  invariant coverage did not silently shrink.

---

## 8. Non-goals

- No parser source changes (`build.go`, `call.go`, `expr.go`,
  `listener.go`, `parser.go`, `pattern.go`, `shape.go`, `typing.go`,
  `errors.go`). Test helpers (`oneBranch`, `oneWriteBranch`, `must`,
  `markBareRefNode`) may be pruned if any go unused after phase 3 but
  not otherwise edited.
- No changes to the golden hashing scheme, the orphan check, the
  scenario-URI keying, or the acceptance harness's outer shape.
- No changes to the property tests or the sentinel-reachability sweep.
- No changes to the TCK vendoring or the feature-dir list. No changes
  to any `.feature` file.
- No changes to `sink_fence_test.go`.
- No introduction of hand-authored `.golden.json` files in
  `testdata/golden/`.
- No introduction of a second golden directory or a parallel
  hand-authored fixture format. The `TestGoldenOrphans` invariant is
  preserved.

---

## 9. Open questions

**Q1: Should Class B kill-probe cases be lifted into a dedicated
`TestKillProbes` block with commentary that names the axis they pin,
instead of surviving amid a shrunk `mustParse` table?**

Deferred to phase 4's preamble rewrite. Argument for: the shrunk
mustParse loses its "all read-core corpus samples" character and becomes
"the axes we pin against the wire's `,omitempty` collapse", which
deserves its own name. Argument against: gratuitous restructuring that
churns diffs without changing what runs. Decision made during phase 4
with Linus in the loop, when the shape of the residual table is
observable.

**Q2: Should the mustReject side migrate parallel to mustParse?**

Not in this bead. The reject-path pins have no shape to outsource
(`require.Equal(t, query.Query{}, got, "model must be the zero value on
error")` — the assertion is *emptiness*), so the "goldens duplicate the
shape" argument doesn't apply. Some mustReject cases could potentially
migrate to a "TCK scenario X should reject with sentinel Y" table in
the acceptance harness, but that is a separate structural question with
its own coverage-no-shrink proof to construct, and out of scope for a
bead named "migrate shape expectations".

---
