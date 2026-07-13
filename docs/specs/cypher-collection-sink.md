# Cypher listener collection sink — one seam replaces fourteen guards

The implementation brief for the `gqlc-ls8.2` architecture deepening in the
Cypher parser listener: replace the fourteen per-handler
`if l.subqueryDepth > 0 { return }` guards with a single collection-sink
seam that decides suppression once. Under the curation discipline of
ADR 0003 (no-expression-tree, curated `query.Query`) and the resolver-API
seal of ADR 0008.

The current Cypher listener carries a Stage-11 suppression counter,
`subqueryDepth`, that is incremented at `EnterOC_ExistentialSubquery` and
decremented at its Exit. Fourteen clause-collecting `Enter*` handlers open
with an identical prologue — `if l.subqueryDepth > 0 { return }` — so that
inner clauses of an `EXISTS { … }` subquery never touch the outer
`curPart` state. Every existing and future clause-collecting handler must
independently remember the guard. A single forgotten guard produces the
Stage-11 §1.2 failure mode ADR 0003's Stage-11 note names: a phantom
inner binding enters the outer part's `Bindings`, and the resolver types
it with the wrong scope.

This document specifies a single mutation-routing seam — a
**collection sink** — that owns the suppression decision. Handlers write
through the sink unconditionally; the sink decides whether the write
lands on `curPart` or is discarded. The Stage-11 invariant moves from
fourteen call sites into one, and the walker's per-handler prologue
disappears.

This is a code-structure change, not a model change. `query.Query`,
`query.Part`, `query.Binding`, `query.Effect`, and every exported symbol
in package `query` are unchanged. `cypher.New` and the parser's exported
API are unchanged. Every golden must be byte-identical without
regenerating; the TCK acceptance suite's counts (3,459 parse-green /
438 pending / 0 failed) must not shift.

Tracking: bead `gqlc-ls8.2` (parent epic `gqlc-ls8`). Blocked-by: none.
Blocks: `gqlc-ls8.5` (cypher parser test migration), which stacks on
this branch — history stays clean.

---

## 1. The seam

### 1.1 What the sink owns

The sink owns exactly one decision: **should this mutation land on the
outer `curPart` or be dropped?** It answers with a single predicate —
`l.suppressed()` today reads `l.subqueryDepth > 0` — and routes on that
predicate.

The sink does not own:

- Parameter mining. Parameters inside `EXISTS { … }` are mined at
  `EnterOC_ExistentialSubquery` via `findParameters` and
  `mineClauseSlotParameter` (listener.go:651–675) and enter the
  query-wide `l.params` / `l.byParam` maps. That path is unchanged: the
  sink does not gate parameter accumulation because parameters are
  query-wide (spec §4 of the branch/part spec), not scoped to a part.
  The Stage-11 §1.2 posture on parameters — "parameters inside a
  subquery are recorded once, at Enter of the subquery, not by walking
  its clauses" — stays intact.

- Ordering. The sink preserves the walker's Enter-order semantics
  exactly: a write in walk order N still lands at position N of its
  target slice. The sink does not reorder, deduplicate, or coalesce.

- Validation. Referential-integrity, kind-conflict, and binding-uniqueness
  checks stay in `build.go`. The sink is a mutation gate, not a
  validator.

- The counter. `subqueryDepth` and the two lines that adjust it
  (`EnterOC_ExistentialSubquery`, `ExitOC_ExistentialSubquery`) remain
  on the listener — they are the sink's input, not its concern. The
  bead's acceptance is "zero remaining `subqueryDepth > 0` guards in
  `Enter*` handlers"; the counter itself may survive inside the sink.

### 1.2 Sink interface

The sink is a listener-embedded value with one method per curPart
mutation the guarded handlers currently perform. Every method is
unconditional at the call site — the handler does not check
`subqueryDepth`, does not check `curPart != nil`, does not check
anything else the sink owns. Each method internally consults
`l.suppressed()` and returns early if true, dropping the mutation.

The full method set the sink exposes (an exhaustive enumeration of the
current `l.curPart.*` mutations reachable from a guarded handler and
its callees):

Structural (branch/part lifecycle):
- `openBranch()` — opens a new `rawBranch` with an initial empty part;
  sets `curBranch` and `curPart`. Replaces `EnterOC_SingleQuery` body
  and the standalone-CALL priming block. Idempotent under suppression:
  a suppressed open is a no-op.
- `recordUnionKind(query.UnionKind)` — appends a combinator to
  `l.combinators`. Replaces `EnterOC_Union` body.
- `closePartOpenNext(imported map[string]query.Type)` — closes the
  current part, opens the next in the current branch, threads the
  exported-type map into the new part's `imported`. Replaces the
  three-line body at the tail of `EnterOC_With`.

Binding / return / ref / part-flag writes on `curPart`:
- `appendBinding(*rawBinding)` — the existing `curPart.bindings` append
  path in `collectNode` / `collectEdge` / `mergeBinding`.
- `appendPathBinding(query.PathBinding)`.
- `appendUnwindBinding(query.UnwindBinding)`.
- `appendCallBinding(query.CallBinding)`.
- `appendReturnItem(query.ReturnItem)`.
- `setReturnsAll()`.
- `setDistinct()`.
- `setCallStandalone()`.
- `appendRef(varRef)` — the existing `curPart.refs` append path.
- `appendEffect(query.Effect)` — the existing `curPart.effects` append
  path.
- `writeToPathMemberSink(query.PathMember)` — routes into the
  `*curPart.pathMemberSink` when the sink is engaged; delegates its
  nil-check discipline to the caller as today.

Query-wide (non-part):
- `markWriteSeen()` — sets `l.writeSeen = true` for write clauses.
  The Stage-12 comment on `writeSeen` is precisely that a suppressed
  write inside `EXISTS { … }` must not flip the flag; this method
  centralises that gate.
- `mintOptionalGroup() int` — increments `l.optionalGroupSeq` and
  returns it (0 when suppressed). Replaces the two-line group-mint
  block at the top of `EnterOC_Match`; the ay9 spec's "suppressed
  clauses inside EXISTS consume no id" invariant becomes a
  single-site fact.

Every method that mutates `curPart` calls `l.suppressed()` first and
returns early if true. Structural methods (`openBranch`,
`recordUnionKind`, `closePartOpenNext`, `markWriteSeen`,
`mintOptionalGroup`) do the same. There is no other code path onto
`curPart` — the sink's methods are the *only* callers of the raw
appends after this change.

### 1.3 What "sink" means concretely

The sink is not a new package, not a new interface value, not a new
struct — it is a set of `(l *listener)` methods with a single-line
prologue calling one predicate. Concretely: `l.appendBinding`,
`l.appendEffect`, `l.openBranch`, and so on. The listener continues
to own the raw state fields (`branches`, `combinators`, `curBranch`,
`curPart`, `writeSeen`, `optionalGroupSeq`, `subqueryDepth`) because
build() reads them and every existing per-part validation walks the
raw shape (§2.2). Introducing a separate type would create a second
place that reads the same fields and gains nothing.

The seam is the **methods**, not a new object. Handlers stop touching
`l.curPart.bindings`, `l.curPart.effects`, `l.branches`, etc. directly;
they call the corresponding sink method. That single call convention
is what makes the suppression check a one-line fact.

---

## 2. What moves and what stays

### 2.1 Enter handlers lose their prologue

Every `Enter*` handler currently guarded — `EnterOC_SingleQuery`,
`EnterOC_Union`, `EnterOC_Match`, `EnterOC_With`, `EnterOC_Create`,
`EnterOC_Merge`, `EnterOC_Delete`, `EnterOC_Set`, `EnterOC_Remove`,
`EnterOC_Unwind`, `EnterOC_InQueryCall`, `EnterOC_StandaloneCall`,
`EnterOC_Return`, plus the sink's `openBranch` seam that fires from
`EnterOC_StandaloneCall`'s curPart-priming block — loses the
`if l.subqueryDepth > 0 { return }` prologue.

The handler body is otherwise unchanged. `EnterOC_Match` still mints an
OPTIONAL group (via `l.mintOptionalGroup()`), still calls
`collectPattern` and `mineWhere`. `EnterOC_With` still calls
`collectProjection`, still mines its WHERE, still routes through the
sink's `closePartOpenNext` to open the next part. `EnterOC_Delete`
still walks its expressions, still computes `targets` / `refs`, still
appends a `DeleteEffect` — but through `l.appendEffect(...)` and
`l.markWriteSeen()`. `EnterOC_StandaloneCall`'s priming block for the
curPart-nil case becomes `l.openBranch()` (which no-ops if suppressed).

Suppression semantics preserve verbatim: a suppressed
`EnterOC_Match` does no useful work today (early-return); after the
change, its calls to `collectPattern` / `mineWhere` still run, but
every mutation they attempt is a sink call that no-ops. **The observable
outcome is identical.**

### 2.2 What does NOT move

`build.go` is untouched. Its inputs are the raw slices (`l.branches`,
`l.combinators`, `l.params`, and per-part `rawPart` fields) exactly as
they exist today. The sink writes into those slices; build reads out
of them. No indirection is introduced.

`typing.go`, `expr.go`, `pattern.go`, `call.go` keep their public
surface. Internal mutations (`l.curPart.refs = append(...)`,
`l.curPart.bindings = append(...)`, etc.) migrate to the sink method
call, but the collectors' signatures and control flow are unchanged.
`typeQuantifier`'s save/restore of `curPart.refs` continues to work —
the sink's `appendRef` writes into the same `curPart.refs` slice
save/restore observes, so the discard-on-exit idiom is preserved.

The two save/restore idioms — `typeQuantifier`'s in typing.go and
`collectMergeAction`'s in listener.go — DO stay as direct field access.
Save/restore is a call-site local pattern that captures the slice
header, not a mutation, and the sink's job is to route mutations. Both
sites are load-bearing and correctly scoped to the handler; the sink
would gain nothing by wrapping them.

`EnterOC_ExistentialSubquery` / `ExitOC_ExistentialSubquery` are not
guarded handlers. They own the counter itself and their bodies stay
untouched. The counter's home is here (listener.go); the sink reads
it via `l.suppressed()`.

Comments referring to "the subqueryDepth guard" survive verbatim in
comment prose where they explain historical behaviour — the guard is
gone from the code path but the invariant it enforced is now enforced
by the sink. Comments that name a specific line's guard get updated
to say "routed through the sink" so future readers can trace the
enforcement to one place.

---

## 3. Suppression invariant

The single fact the sink enforces:

> When `l.subqueryDepth > 0`, **no** curPart mutation lands, and no
> query-wide *scope-consuming* mutation lands
> (`writeSeen`, `optionalGroupSeq`, `combinators`, `branches` grow).
> Query-wide *deduplicated* mutations (`params`, `byParam`, `approved`)
> continue to accumulate — parameter mining under `EXISTS { … }` is
> the Stage-11 §1.2 promise the parser makes.

The `optionalGroupSeq` mint under suppression is a no-op returning 0
(matching the ay9 §3.3 note). `combinators` under a UNION inside
`EXISTS { … }` is a no-op (matching the Stage-11 §1.2 note on
`EnterOC_Union`). `branches` under a `oC_SingleQuery` inside
`EXISTS { … }` is a no-op (Stage-11 §1.2 note on `EnterOC_SingleQuery`).
`writeSeen` under a write inside `EXISTS { … }` is a no-op (Stage-12
note on `writeSeen`).

There is exactly one place in the file that reads `subqueryDepth`
after this change: `l.suppressed()`. Every other reference is a
comment.

---

## 4. Test plan

The interface is the test surface.

### 4.1 Existing tests are the primary fence

All 3,199 parser goldens must pass byte-identical WITHOUT `-update`.
The `-update` flag is forbidden by the bead's acceptance criteria; a
byte-diff on any golden means the change altered observable behaviour
and must be reverted or root-caused before the branch merges.

TCK acceptance suite counts (assertable via `just test`):
3,459 parse-green, 438 pending, 0 failed — unchanged.

Referential-integrity property tests (`TestPropertyReferentialIntegrity`
and its child assertions `assertReferentialIntegrity`,
`assertNamedBindingsUnique`, `assertParametersDeduped`,
`assertPathMemberKindAgrees`) — green.

Sentinel reachability sweep (`TestSentinelReachability`) — green.

`TestMustParse`, `TestMustReject`, `TestMustRejectGrammar` — green.

These tests already exercise every one of the fourteen guarded
handlers, both at outer scope and under `EXISTS { … }`. They are the
authoritative regression fence. If they all pass byte-identical, the
change preserves behaviour by construction — the sink is a routing
seam, not new semantics.

### 4.2 New tests (targeted for the seam)

Two additions, both in `parser_test.go`:

**Grep-verifiable structural test** (source-form assertion, not a
runtime test): a `//go:generate`-style comment or a note in the
listener package that the guard pattern is gone. Rather than a
runtime test, the acceptance criteria's grep — zero remaining
`if l.subqueryDepth > 0` in Enter handlers — is verified in CI via
`just lint` (a linter rule) or manually via `grep -n "subqueryDepth > 0"
internal/query/cypher/listener.go`. The bead names grep-verifiability
as the acceptance mechanism; no new test file is required for it.

**Behavioural test for the sink's discipline** (already implicit in the
existing corpus, but pinned by a small direct case):
one table-driven test that constructs each of the fourteen shapes
under `EXISTS { … }` (a MATCH, a WITH, a CREATE, a MERGE, a SET, a
REMOVE, a DELETE, an UNWIND, an in-query CALL, a standalone CALL, a
RETURN, a UNION, a nested SingleQuery, and a nested EXISTS) and
asserts, for each, that the outer part carries exactly the outer
bindings (zero from inside the subquery) and that `StatementKind` is
`StatementRead` when the inner is a write. This test exists implicitly
in `parser_test.go` (see e.g. the Stage-11 test cases named around
lines 1923, 1935, 2031, 2470, 2733). Confirm the fourteen shapes are
covered before writing; if any shape is missing, add exactly that
case. Do **not** rewrite existing tests.

### 4.3 What we do NOT test

We do not add a fault-injection test that removes the guard from a
single handler and checks the failure mode — the bead's whole point
is that a forgotten guard is now unrepresentable (the sink is the
only mutation path). There is nothing to inject at.

We do not add a mock sink or a sink-behaviour unit test. The sink is
methods on the listener; its correctness is observable through the
listener's outputs (the corpus goldens, the TCK, the property tests).

---

## 5. Migration path

TDD, small commits, red-green-refactor. The migration is mechanical
per site and can be done in phases without breaking the build at any
step.

**Phase A — Introduce the sink methods (no callers).** Add the sink
methods on `(l *listener)` — `openBranch`, `recordUnionKind`,
`closePartOpenNext`, `appendBinding`, `appendPathBinding`,
`appendUnwindBinding`, `appendCallBinding`, `appendReturnItem`,
`setReturnsAll`, `setDistinct`, `setCallStandalone`, `appendRef`,
`appendEffect`, `writeToPathMemberSink`, `markWriteSeen`,
`mintOptionalGroup`, and `suppressed`. Each method wraps the same
mutation the guarded handlers do today, guarded by
`if l.suppressed() { return }`. No caller migrates yet.

Commit: `refactor(cypher): introduce collection-sink methods on listener`.
Gates: `just test && just fmt-check && just lint` all green — the new
methods compile and are dead code.

**Phase B — Migrate the fourteen Enter handlers, one at a time.** For
each guarded handler in turn:

1. Remove the `if l.subqueryDepth > 0 { return }` prologue.
2. Replace every direct `l.curPart.*` mutation the body performs (and
   every `l.branches`, `l.combinators`, `l.writeSeen`,
   `l.optionalGroupSeq` mutation) with the corresponding sink method
   call.
3. Run `just test`. All 3,199 goldens byte-identical, TCK unchanged.
4. Commit: `refactor(cypher): route <ClauseName> through collection sink`.

Handler-by-handler order (stable, mechanical):
`EnterOC_SingleQuery`, `EnterOC_Union`, `EnterOC_Match`,
`EnterOC_With`, `EnterOC_Create`, `EnterOC_Merge`, `EnterOC_Delete`,
`EnterOC_Set`, `EnterOC_Remove`, `EnterOC_Unwind`,
`EnterOC_InQueryCall`, `EnterOC_StandaloneCall`, `EnterOC_Return`,
and the standalone-CALL curPart-priming block.

For the collectors called from those handlers (`collectPattern`,
`collectPatternPart`, `collectPatternElement`, `collectNode`,
`collectEdge`, `mergeBinding`, `recordEndpointRefs`,
`collectProjection`, `collectReturnItem`, `collectUnwind`,
`collectSetItem`, `collectRemoveItem`, `collectMergeAction`,
`collectCall`, `collectYieldItems`, `expandAllResults`,
`typeExpressionMining` and its callees, `mineWhere`,
`mineSortItemParameters`, `mineClauseSlotParameter`,
`mineComparisons`, `mineComparison`, `mineStringPredicate`,
`pairOperands`, `pairAddSub`, `mineInlineMap`,
`requireAllParametersApproved`, `addParameterUse`,
`recordPathNode`, `recordPathEdge`), migrate their direct-field
writes to sink calls in the same commit as the caller they migrate
under. This keeps commit diffs handler-scoped and reviewable.

**Phase C — Remove `subqueryDepth` reads outside the sink.** After
every handler has been migrated, grep should show zero
`subqueryDepth > 0` outside `l.suppressed()` in
`Enter*` / `Exit*` handlers. Update comment prose that referred to
"the guard" to say "routed through the sink" where the guard is what
they were explaining.

Commit: `refactor(cypher): centralise EXISTS suppression in one predicate`.

**Phase D — Verify acceptance fences.**

- Grep: `grep -n "subqueryDepth" internal/query/cypher/*.go` — expect
  only the sink's `suppressed` method, the counter's Enter/Exit
  adjustments, and comment prose.
- Golden byte-identity: `just test` with no `-update`. Zero diff.
- TCK counts: `just test` output shows 3,459/438/0.
- API surface: `cypher.New` / `cypher.Parser.Parse` signatures
  unchanged (verifiable by `git diff master -- internal/query/cypher/parser.go`).
- Quality gates: `just test && just fmt-check && just lint` — green.

Re-run `just fmt-check && just lint` after **any** subsequent edit,
even one character, per the worktree feedback discipline. LSP
diagnostics do not track disk state and are unreliable after agent
edits; fmt-check and lint are authoritative.

---

## 6. Non-goals

- No change to `query.Query`, `query.Part`, or any sealed sum in
  package `query`. ADR 0008 records the model surface as pinned; this
  bead does not touch it.
- No change to what `EXISTS { … }` observably suppresses. The invariant
  is preserved; the enforcement moves.
- No new sentinel errors. `ErrPatternInProjection`,
  `ErrNestedPropertyTarget`, and the fifteen others remain reachable
  through the same handlers as today.
- No performance target. The sink is a per-call `if` on a `bool` (or
  `int > 0`) — the same instruction the guards perform today, on the
  same code path. No allocations, no indirection.
- No renaming of `subqueryDepth`. The counter's name is load-bearing
  in comments and existing tests; the deepening is that fourteen
  reads collapse to one, not that the reader is renamed.
- No absorption of the parameter-mining path into the sink. Parameter
  mining under `EXISTS { … }` is a separate discipline (§1.1) and
  moving it would enlarge scope without benefit.

---

## 7. Acceptance summary

Concrete, grep- and gate-verifiable fences:

- `grep -cE "if l\.subqueryDepth > 0" internal/query/cypher/*.go` in
  `Enter*`/`Exit*` handlers returns 0 for Enter handlers; the Exit
  handler's conditional-decrement may keep its comparison (it is
  balance-safe, not a scope guard).
- `git diff master -- internal/query/cypher/testdata/golden/` empty.
- `just test` prints the same TCK line as master (3,459 / 438 / 0).
- `TestPropertyReferentialIntegrity`,
  `TestSentinelReachability`, `TestMustParse`, `TestMustReject`,
  `TestMustRejectGrammar` all pass.
- `just fmt-check` and `just lint` green.
- `cypher.New` / `cypher.Parse` signatures byte-identical to master.

When all seven are true, the deepening is done.
