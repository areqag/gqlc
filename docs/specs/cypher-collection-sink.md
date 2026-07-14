# Cypher listener collection sink — one seam replaces fourteen guards

The implementation brief for the `gqlc-ls8.2` architecture deepening in the
Cypher parser listener: replace the fourteen per-handler
`if l.subqueryDepth > 0 { return }` guards with a single suppression seam
that decides once, for every category of listener state a walked
`EXISTS { … }` body could otherwise leak into the outer query. Under the
curation discipline of ADR 0003 (no-expression-tree, curated
`query.Query`) and the resolver-API seal of ADR 0008.

The current Cypher listener carries a Stage-11 suppression counter,
`subqueryDepth`, incremented at `EnterOC_ExistentialSubquery` and
decremented at its Exit. Fourteen clause-collecting `Enter*` handlers
open with `if l.subqueryDepth > 0 { return }` so their bodies do not
touch the outer part. Every existing and future clause-collecting
handler must independently remember the guard. A forgotten guard
produces the Stage-11 §1.2 phantom-binding failure ADR 0003 names.

The deepening: handlers stop guarding. Every listener state write is
routed through a sink method whose one-line prologue is
`if l.suppressed() { return }`. The suppression predicate is the only
read of `subqueryDepth > 0` outside the counter's own Enter/Exit.

Crucially, this is **not just about `curPart` writes**. A walked
suppressed body can also leak through four other listener state
categories — `l.err` (via `l.fail`), `l.params` / `l.byParam` /
`l.approved` (via `addParameterUse`), `l.branches` / `l.combinators`
(structural), and `l.writeSeen` / `l.optionalGroupSeq` (query-wide
scope-consuming). Every one of these categories must route through
the sink, or a walked EXISTS body will flip an outer parse from
`err == nil` to `err != nil`, mint a spurious parameter Use, or grow
an outer branch. The spec below enumerates each category site by
site, proves suppression-preserving parity for the specific
`l.fail` fail-sites a walked body reaches under EXISTS, and pins the
`EnterOC_ExistentialSubquery` mining reorder that keeps outer-Enter
parameter mining un-suppressed.

This is a code-structure change, not a model change. `query.Query`,
`query.Part`, `query.Binding`, `query.Effect`, and every exported
symbol in package `query` are unchanged. `cypher.New` and the parser's
exported API are unchanged. Every one of the 3,199 parser goldens must
be byte-identical without regenerating; the TCK acceptance suite
counts (3,459 parse-green / 438 pending / 0 failed) must not shift.

Tracking: bead `gqlc-ls8.2` (parent epic `gqlc-ls8`). Blocks
`gqlc-ls8.5` (cypher parser test migration), which stacks on this
branch — history stays clean.

Revision history:
- Rev 1 (aeec115): initial spec. Enumeration missing byVar and
  pathMemberSink; save/restore proof implicit; `l.fail` and
  `addParameterUse` under suppression not addressed.
- Rev 2 (452c6b7): rewritten under linus-2 grill. §1.2 becomes a
  contract; §3 adds a site-by-site parity walk; §4 adds a lint gate.
- Rev 3 (9843af7): Phase-ordering fix. Rev 2's Phase C step 3
  promised byte-identical tests after each handler migration, but
  Rev 2's Phase D deferred the `addParameterUse` sink gate to AFTER
  Phase C. Dropping Match's guard mid-Phase-C surfaced the
  `EnterOC_Match → mineWhere → pairAddSub → addParameterUse` reach
  path under `l.suppressed()` and leaked a spurious PropertyUse into
  the outer parameter, breaking
  `TestMustParse/exists_body_mixed_limit_skip_and_where_params`. The
  same reach exists for With/Set/Delete/Remove. §5 splits Rev 2's
  Phase D — the `addParameterUse` gate lifts into a new Phase B.5
  that MUST land before any Phase C handler migration begins; the
  `l.fail` gate stays as Phase D. §3.4 clarifies that the
  suppression gate (not the `approved` map) is what actually closes
  BLOCKER 5 at handler-reach sites. Ships as three commits on this
  branch (Rev-3 stack): b5e206d (Phase B.5 sink gate), 764dcd8 (Phase
  C Match migration, previously red, now green), and this spec
  commit that formalises the phasing.
- Rev 4 (1bcaa30): Phase-D-before-final-Phase-C reorder. Rev 3's
  §5 placed Phase D (`l.fail` gate) AFTER all Phase C handler
  migrations. During the commit-12 (`EnterOC_InQueryCall`) review
  cycle, linus-2 flagged that the last-remaining Category-D reach
  paths (call.go `l.fail` at :43/:72/:136/:146/:179) were still
  ungated at the tip, and every prior Phase-C commit that dropped a
  handler guard had opened a *latent* Cat-D parity window: any
  walker-reachable `l.fail` under EXISTS between commit-6 and
  commit-11 would have leaked an outer `l.err` — a retroactive spec
  violation the goldens are silent on (goldens cover the
  no-error path; `l.err`-first-write pins are the assertions that
  catch this class). Rev 4 records that Phase D landed as commit-12.5
  (`7af1e6a`) as a **linus-2-imposed review prerequisite** ahead of
  commit-13 (`EnterOC_StandaloneCall`, the final Phase C handler),
  same class as the Rev-3 B.5 hoist that pre-dated it. Phase D
  content itself is unchanged from Rev 3 §5 — this is a *reorder*
  only, not a content amendment. Justification: close the Cat-D
  latent parity window (retroactive spec closure over 12 prior
  commits) before the final handler guard-drop, so commit-13 lands
  against a spec-conforming stack. SyntaxError-ordering safety:
  `walk()` at listener.go:390 checks `l.err != nil` before invoking
  the tree walker, and `subqueryDepth` is definitionally 0 outside
  walker context (only Enter/ExitOC_ExistentialSubquery mutates it),
  so `l.suppressed()` returns false when SyntaxError's `l.fail` call
  fires — the Phase D gate never blocks a real syntax error (§3.2
  BLOCKER 3 remains authoritative). Ships as two commits on this
  branch (Rev-4 stack): 7af1e6a (Phase D fail-gate + 8-entry §4.2
  Test A parity table; retroactive window-close for commits
  75e6846..1182448), and this spec commit that formalises the
  reorder. Commit-13 (StandaloneCall) then follows under the standing
  Phase-C rule.
- Rev 5 (this file): Phase E mechanism swap — forbidigo replaced by
  an AST-based write-fence test. Rev 3 §4.3 specified a `forbidigo`
  rule scoped to package `cypher` with seven regex patterns like
  `l\.err\s*=` intended to fence writes to the six listener
  collection-state fields (`err, branches, combinators, writeSeen,
  params, optionalGroupSeq`). During Phase E implementation the
  mechanism was measured first-party: golangci-lint's `forbidigo` v2
  is *identifier-chain-based*, not source-substring-regex-based. It
  silently drops the `\s*=` context and matches the selector chain
  alone, flagging reads as well as writes. Measured surface across
  package `cypher` (uncapped `--max-issues-per-linter=0
  --max-same-issues=0`):

  | Identifier | Total | Writes | Reads |
  |---|---|---|---|
  | `l.err` | 26 | 1 | 25 (the `if l.err != nil` guard idiom) |
  | `l.params` | 8 | 2 | 6 |
  | `l.branches` | 4 | 1 | 3 |
  | `l.combinators` | 3 | 1 | 2 |
  | `l.writeSeen` | 2 | 1 | 1 |
  | `l.optionalGroupSeq` | 2 | 1 | 1 |
  | **Total** | **45** | 7 | 38 |

  Rev 3 §4.3 anticipated ~15 sink-site `//nolint:forbidigo`
  directives; the actual surface would have required 38 additional
  read-site nolints for zero enforcement value (a `read` of `l.err`
  cannot mutate it). Options considered and rejected: (1) ship
  spec-literal (38 read-site nolints = comment pollution against
  house style), (2) narrow forbidigo to five identifiers and drop
  `l.err` (still 12 read-site nolints AND silently loses the one
  write forbidigo cannot express anyway), (3) defer to a follow-up
  bead (risk of polish-bead rot). Team-lead ruled Option 4: replace
  forbidigo with a Go AST-based test that walks every AssignStmt.LHS
  and IncDecStmt.X on `*listener`-receiver methods, resolves the
  selector chain, and asserts the enclosing func is one of seven
  sanctioned sink methods (`fail, openBranch, recordUnionKind,
  markWriteSeen, mintOptionalGroup, addParameterUse,
  addParameterUseUnsuppressed`). Enforcement is STRONGER than any
  forbidigo option (retains `l.err` write-enforcement); nolint count
  is ZERO. §5 Phase E is updated in-place to reference the fence
  test instead of the forbidigo rule; §4.3 becomes a historical
  paragraph followed by the fence-test description. Intent
  unchanged: "no direct writes to listener collection state outside
  the sinks." Ships as one commit on this branch (Rev-5 stack):
  the commit-14 that installs `sink_fence_test.go` plus this spec
  edit, plus the two Rev-4-cycle nits (parser_test.go Phase-D pin
  sentinel-comment tightening + §3.2 stale `[r*-1]` hop-example
  replacement with the digits-only-overflow shape Test A actually
  pins).
- Rev 6 (this commit): Phase F acceptance sweep. Verification-only —
  no code delta beyond a single §7 mechanism-reference tightening.
  §7 fence line 6 previously read "`just fmt-check` and `just lint`
  green with the `forbidigo` rule enabled"; Rev 5 swapped forbidigo
  for the AST write-fence test (§5 Phase E), so line 6 now names the
  fence test directly (`sink_fence_test.go` green). The commit body
  carries the Phase F transcript: all eight §7 fences run first-party
  against tip (8605ab7), with §7.3 TCK-line parity independently
  verified against master (ddc395d) via ephemeral worktree. Test A
  pin count: 8 (parser_test.go:3115-3229, `exists fail parity — X`,
  committed at 7af1e6a). Test B: not pinned by design — the full
  3,459-entry TCK corpus regression-locks the addParameterUse gate
  installed at commit b5e206d (Phase B.5), and §7.2's golden-diff-empty
  invariant guarantees position parity. Deferred if linus-2 requests
  a targeted regression lock during whole-diff review. Ships as one
  commit on this branch (Rev-6, the Phase F closer).

---

## 1. The seam

### 1.1 Contract (not inventory)

The sink's contract is a single invariant:

> Any listener state write that would, if it landed under
> `subqueryDepth > 0`, produce observable behaviour different from
> master's guarded early-return, MUST go through a sink method that
> gates on `l.suppressed()`.

Concretely, the sink covers **five categories** of write:

**A. Per-part scope writes** — every mutation of `l.curPart.*`. The
Stage-11 §1.2 phantom-binding failure ADR 0003 names is the
leak-into-outer-part class this category prevents. The full list of
`l.curPart.*` field-writes reachable from a guarded handler's body
(grep-verified in §2.1):

- `curPart.bindings` (append)
- `curPart.byVar` (index-assign; atomic with `bindings` append inside
  `mergeBinding`)
- `curPart.pathBindings` (append)
- `curPart.pathMemberSink` (pointer assignment; the pointed-to slice
  is a stack local, but the assignment writes onto the part)
- `*curPart.pathMemberSink` (append to the pointed-to slice; this is
  the write `recordPathNode` / `recordPathEdge` perform)
- `curPart.unwindBindings` (append)
- `curPart.callBindings` (append)
- `curPart.callStandalone` (bool set)
- `curPart.returns` (append)
- `curPart.returnsAll` (bool set)
- `curPart.distinct` (bool set)
- `curPart.refs` (append) — including the save/restore idioms in
  §2.2
- `curPart.effects` (append; the save/restore around
  `collectMergeAction` is in §2.2)

**B. Query-wide structural writes** — writes that grow the query
shape, which a walked EXISTS body must not do:

- `l.branches` (append) — `EnterOC_SingleQuery`'s open, and the
  standalone-CALL priming block in `EnterOC_StandaloneCall`
- `l.curBranch`, `l.curPart` (pointer assign) — the same two paths
- `l.combinators` (append) — `EnterOC_Union`
- Part-append onto `l.curBranch.parts` — `EnterOC_With`'s close-open

**C. Query-wide scope-consuming counters/flags**:

- `l.writeSeen = true` — set by `EnterOC_Create`, `EnterOC_Merge`,
  `EnterOC_Delete`, `EnterOC_Set`, `EnterOC_Remove`. Stage-12's
  `writeSeen` comment already says a suppressed write inside EXISTS
  does not flip the flag; that gate moves to the sink.
- `l.optionalGroupSeq++` — incremented inside `EnterOC_Match` when
  OPTIONAL is present. The ay9 §3.3 invariant "suppressed clauses
  inside EXISTS consume no id" moves to the sink.

**D. Query-wide error** — `l.err` (via `l.fail`):

- Every `l.fail(err)` call site reachable from a walked
  currently-guarded handler body. A walked suppressed body must not
  set `l.err`; master's early-return path never does. §3.2 walks
  every reachable fail-site and proves either grammatical
  unreachability or parity via sink-gating.

**E. Query-wide parameter accumulation** — `l.params`, `l.byParam`,
`l.approved` (via `addParameterUse`):

- Every `addParameterUse` call from within a walked currently-guarded
  handler body. Master's guarded early-return means the call never
  fires; a walked suppressed body would fire it with a Use position
  (e.g. `ExprInSetValue`) different from the ExprInPredicate the
  outer `EnterOC_ExistentialSubquery` mining assigns.
- **Exception path**: the mining loops inside
  `EnterOC_ExistentialSubquery` (listener.go:653-674) are the
  designated non-suppressed accumulator for EXISTS-scoped parameters
  (Stage-11 §1.2). They must continue to run un-suppressed. §1.4
  specifies the reorder that keeps them un-suppressed.

`l.suppressed()` returns `l.subqueryDepth > 0`. It is the ONLY read
of `subqueryDepth > 0` after this change. Every other historical
call site becomes a sink-method call.

### 1.2 The seam is a set of methods on `(l *listener)`

The listener continues to own the raw state fields
(`branches`, `combinators`, `curBranch`, `curPart`, `writeSeen`,
`optionalGroupSeq`, `subqueryDepth`, `params`, `byParam`, `approved`,
`err`) because `build.go` reads them directly and every existing
per-part validation walks the raw shape. Introducing a separate type
would create a second place that reads the same fields and gain
nothing.

The seam is **methods** on the listener, each with the single-line
prologue `if l.suppressed() { return }` (or `return 0` for the
group-mint). Listing the method set the sink exposes — one per
write site in §1.1:

Category A (per-part):
- `appendBinding(*rawBinding)` — used only for anonymous edges
  (pattern.go:309); named-binding writes go through
  `mergeBinding` (below), which is itself sink-routed.
- `mergeBinding(variable, kind, labels, source, target, group,
  undirected, hops, bare)` — the existing method becomes
  sink-routed. Owns the ATOMIC `byVar`+`bindings` update; a
  suppressed call is a no-op for both writes together, so BLOCKER
  1's phantom `byVar` entry pointing at a non-existent binding is
  unrepresentable.
- `appendPathBinding(query.PathBinding)`.
- `setPathMemberSink(*[]query.PathMember)` — routes the pointer
  assignment at pattern.go:68 and the nil-clear at pattern.go:71.
  Under suppression the pointer stays nil, so `recordPathNode` /
  `recordPathEdge`'s existing `if l.curPart.pathMemberSink == nil
  { return }` check gates them (BLOCKER 1's pathMemberSink
  concern).
- `appendPathMember(query.PathMember)` — the write to
  `*curPart.pathMemberSink` at pattern.go:106, 114, 127, 135.
  Sink-routed; also a no-op when the sink is nil.
- `appendUnwindBinding(query.UnwindBinding)`.
- `appendCallBinding(query.CallBinding)`.
- `setCallStandalone()`.
- `appendReturnItem(query.ReturnItem)`.
- `setReturnsAll()`.
- `setDistinct()`.
- `appendRef(varRef)` — the sole path to `curPart.refs = append`.
- `appendEffect(query.Effect)`.

Category B (structural):
- `openBranch()` — appends a fresh `rawBranch` and points
  `curBranch`/`curPart` at it. Replaces `EnterOC_SingleQuery` body
  and the standalone-CALL curPart-priming block.
- `recordUnionKind(query.UnionKind)`.
- `closePartOpenNext(imported map[string]query.Type)` — closes
  `curPart`, opens a fresh part in `curBranch.parts`, threads
  `imported`.

Category C (query-wide scope):
- `markWriteSeen()`.
- `mintOptionalGroup() int` — returns 0 when suppressed.

Category D (error):
- `fail(err error)` — the existing `l.fail` becomes sink-gated: it
  writes `l.err` only when NOT suppressed. Under suppression, the
  fail is dropped. §3.2 proves observable parity for each reachable
  fail-site.

Category E (parameters):
- `addParameterUse(name, node, use)` — becomes sink-gated. Under
  suppression, the call is a no-op (parameters are already covered
  by the un-suppressed outer `EnterOC_ExistentialSubquery` mining;
  see §1.4).
- `addParameterUseUnsuppressed(name, node, use)` — the bypass path
  called from the `EnterOC_ExistentialSubquery` mining loops after
  the reorder (§1.4). Same body as `addParameterUse` minus the
  suppression check. This is the ONLY caller of the unsuppressed
  variant.

The save/restore idioms (typing.go:455-457, expr.go:167-169,
expr.go:489-491, listener.go:449-464) stay as direct
`l.curPart.refs = savedRefs` / `l.curPart.effects = saved`
assignments; they are not mutations of new content but restorations
of a captured slice header. §2.2 proves parity for each.

### 1.3 Discipline-enforced, with a lint gate

The seam is **discipline-enforced**: the type system does not
prevent a future handler author from writing `l.curPart.bindings =
append(...)` directly. Nothing in Go's semantics stops it.

To make the discipline bite, the migration adds a package-local
lint rule in `.golangci.yml`: a `forbidigo` entry that fails when a
`l\.curPart\.` write, an `l\.branches\s*=\s*append`, an `l\.combinators\s*=\s*append`,
an `l\.writeSeen`/`l\.optionalGroupSeq` assignment, or a bare
`l\.err\s*=` or `l\.params\s*=\s*append`/`l\.byParam`/`l\.approved`
write appears outside the sink method definitions in
`listener.go`. The rule fires at CI (via `just lint`) and locally
(via the pre-push hook, since `check-hooks` runs at `just test`
time).

The lint scope is package `cypher`. The sink methods live in
`listener.go`, and every other `.go` file in the package must
route through them. `listener.go` itself gets a
`//nolint:forbidigo` file directive on the sink method
definitions (or per-line directives where forbidigo's include-glob
is coarser).

This is the enforcement the bead's "handlers should be UNABLE to
forget the gate" invariant requires. Method calls alone would decay
the next time someone under deadline writes a direct append; the
lint gate makes decay a CI failure. Rev 1 named CI grep as the
enforcement, which was a wish; Rev 2 pins it to a specific
golangci-lint rule.

### 1.4 `EnterOC_ExistentialSubquery` mining reorder

Master's `EnterOC_ExistentialSubquery` (listener.go:651-675)
increments `subqueryDepth` at line 652, THEN mines
`findNodesOfType[IOC_SkipContext]`, `findNodesOfType[IOC_LimitContext]`,
and `findParameters(c)`. In master this ordering is safe because
`mineClauseSlotParameter` and `addParameterUse` are not gated on
`subqueryDepth`.

Under Rev-2's regime, `addParameterUse` IS gated (Category E,
required to prevent BLOCKER 5's ExprInSetValue leak). So mining at
depth>0 would silently drop every parameter in the subquery.

Fix: reorder to **mine first, then increment**. Concretely,
`EnterOC_ExistentialSubquery` becomes:

```
func (l *listener) EnterOC_ExistentialSubquery(c *gen.OC_ExistentialSubqueryContext) {
    // Mine before incrementing so the un-suppressed addParameterUse
    // path fires. The three loops walk manually (not via the ANTLR
    // walker) so no clause-collecting Enter runs during this pass.
    for _, s := range findNodesOfType[gen.IOC_SkipContext](c) {
        l.mineClauseSlotParameter(s.OC_Expression(), query.ClauseSlotSkip)
        if l.err != nil { break }
    }
    if l.err == nil {
        for _, lim := range findNodesOfType[gen.IOC_LimitContext](c) {
            l.mineClauseSlotParameter(lim.OC_Expression(), query.ClauseSlotLimit)
            if l.err != nil { break }
        }
    }
    if l.err == nil {
        for _, p := range findParameters(c) {
            if l.approved[p] { continue }
            name := parameterName(p)
            if name == "" { continue }
            l.addParameterUse(name, p, query.NewExprUse(query.TypeBool{}, query.ExprInPredicate))
        }
    }
    // Now enter the suppressed scope; the walker will descend and
    // fire clause Enters, whose bodies write through gated sink
    // methods and produce no outer state.
    l.subqueryDepth++
}
```

Proof this reorder is safe under nesting: `findParameters(c)` walks
the ENTIRE subtree of the outer subquery, including bodies of
nested inner `EXISTS { … }`. Its output includes every parameter at
every nesting level (shape.go:431-448 confirms the recursive walk).
The `l.approved[p]` dedup at each subsequent Enter idempotently
skips already-covered parameters. So an inner
`EnterOC_ExistentialSubquery` mining at outer-post-increment depth
= 1 IS suppressed (`addParameterUse` no-ops) — and is redundant,
because the outer's pre-increment sweep already recorded every
parameter in the inner subtree with `ExprInPredicate` and marked
them approved. `findNodesOfType[IOC_SkipContext]` and
`findNodesOfType[IOC_LimitContext]` similarly walk the whole
subtree, so outer covers every SKIP/LIMIT position too, including
those inside nested EXISTS.

`mineClauseSlotParameter` calls `addParameterUse` internally (via
expr.go:450-482); its gating discipline follows the same rule —
gated by default, but called from the outer un-suppressed
pre-increment context, it fires. Concretely: since the outer Enter
mines before incrementing, `l.suppressed()` is false during those
calls, and `mineClauseSlotParameter → addParameterUse` runs
normally.

The Exit decrement (listener.go:678) stays unchanged; balance is
preserved.

---

## 2. What moves and what stays

### 2.1 Enter handlers lose their prologue

Every `Enter*` handler currently guarded — `EnterOC_SingleQuery`,
`EnterOC_Union`, `EnterOC_Match`, `EnterOC_With`, `EnterOC_Create`,
`EnterOC_Merge`, `EnterOC_Delete`, `EnterOC_Set`, `EnterOC_Remove`,
`EnterOC_Unwind`, `EnterOC_InQueryCall`, `EnterOC_StandaloneCall`,
`EnterOC_Return`, plus the standalone-CALL curPart-priming block —
loses the `if l.subqueryDepth > 0 { return }` prologue.

The handler body is otherwise **structurally** unchanged: same
control flow, same order of collector calls, same computations. The
only edits are:

- Every direct `l.curPart.*`, `l.branches`, `l.combinators`,
  `l.writeSeen`, `l.optionalGroupSeq`, `l.fail`, `l.approved`,
  `l.params`, `l.byParam` mutation becomes a call to the
  corresponding sink method.
- The `EnterOC_StandaloneCall` curPart-priming block becomes
  `l.openBranch()` (sink-routed).
- The `if l.err != nil` short-circuit checks stay; sink-gated
  `l.fail` still writes `l.err` when un-suppressed, so the loop
  still short-circuits at outer scope. Under suppression these
  checks continue to see `l.err == nil` (since fail is gated), so
  the loop runs to completion — which is fine, because every
  mutation the loop attempts is a no-op.

The collectors called from those handlers migrate the same way:
`collectPattern`, `collectPatternPart`, `collectPatternElement`,
`collectNode`, `collectEdge`, `mergeBinding`, `recordEndpointRefs`,
`collectProjection`, `collectReturnItem`, `collectUnwind`,
`collectSetItem`, `collectRemoveItem`, `collectMergeAction`,
`collectCall`, `collectYieldItems`, `expandAllResults`,
`typeExpressionMining` and its callees, `mineWhere`,
`mineSortItemParameters`, `mineClauseSlotParameter`,
`mineComparisons`, `mineComparison`, `mineStringPredicate`,
`pairOperands`, `pairAddSub`, `mineInlineMap`,
`requireAllParametersApproved`, `recordPathNode`, `recordPathEdge`.
Their direct field writes migrate to sink calls; their signatures
and control flow are unchanged.

### 2.2 Save/restore proof (BLOCKER 2)

Three sites capture a slice header, run a walk, and restore. Each
must be proved a no-op under suppression:

**Site 1 — `typeQuantifier` (typing.go:455-457)**:

```
savedOuter := l.curPart.refs
_, _, params := l.typeExpressionMining(w.OC_Expression())
l.curPart.refs = savedOuter // discard filter-body refs
```

Under suppression: `savedOuter` captures the current
`l.curPart.refs` slice header. `typeExpressionMining` walks the
expression tree; every `l.curPart.refs = append(...)` inside is
routed through `appendRef`, which no-ops under suppression. So
`l.curPart.refs` is unchanged when the walk returns. The restore
writes the same header back — a strict no-op. The subsequent
`addParameterUse` calls in the `for _, p := range params` loop
no-op (gated). Outcome: `l.curPart.refs`, `l.params`, `l.byParam`,
`l.approved`, `l.err` all unchanged from before the block. Parity
preserved.

Un-suppressed (outer scope): behaviour identical to master —
`appendRef` writes normally, restore discards them, quantifier
scoping preserved.

**Site 2 — `mineWhere` (expr.go:489-491)**:

```
savedRefs := l.curPart.refs
l.typeExpressionMining(w.OC_Expression())
l.curPart.refs = savedRefs
```

Same proof as Site 1. Under suppression, `l.curPart.refs` is
never mutated inside the walk; the restore is a no-op. Parity.

**Site 3 — `mineSortItemParameters` (expr.go:167-169)**:

```
savedRefs := l.curPart.refs
l.typeExpressionMining(e)
l.curPart.refs = savedRefs
```

Same proof. Parity.

**Site 4 — `collectMergeAction` (listener.go:449-464)**:

This one is subtly different: it sets `l.curPart.effects = nil` at
the top, then reads it after the walk, then restores. The
intermediate `nil` set is a direct field write.

Concrete proof under suppression: at entry to `collectMergeAction`
under suppression, `l.curPart.effects` holds whatever the outer
part carries. The save (`saved := l.curPart.effects`) captures
that. `l.curPart.effects = nil` clears the header. `l.collectSetItem`
runs; every append to `l.curPart.effects` inside is routed through
`appendEffect` (gated, no-op). The `collected := l.curPart.effects`
read returns `nil`. The loop over `collected` runs zero iterations.
Both restore points (the two `l.curPart.effects = saved` lines)
write the original outer value back. Net effect on the outer part:
zero. Parity preserved.

The parser is single-threaded within one Parse call (ADR 0001), so
the intermediate `nil` state is not observable from any concurrent
reader. Save/restore is bracket-balanced within one function.
Safe.

### 2.3 What does NOT move at all

`build.go` is untouched. Its inputs are the raw slices as they
exist today. The sink writes into those slices; build reads out.

`EnterOC_ExistentialSubquery` / `ExitOC_ExistentialSubquery` bodies
are unchanged EXCEPT for the mining-then-increment reorder in §1.4.

Comments referring to "the guard" get updated where they explain a
specific line's guard now removed. The Stage-11 §1.2 note and the
Stage-12 `writeSeen` comment stay; their invariants are now
enforced by the sink rather than by fourteen prologue lines.

---

## 3. Parity walk

### 3.1 The invariant

> When `l.subqueryDepth > 0`, no listener state write lands EXCEPT
> parameter accumulation from the un-suppressed pre-increment
> mining path in `EnterOC_ExistentialSubquery` (§1.4).

Every category A-E write is gated. `l.err` is gated. `l.params` is
gated except through the mining-path bypass.

### 3.2 Suppression body execution: what runs, what doesn't (BLOCKER 3 proof)

The bead's byte-identity fence is only preserved if every walked
suppressed handler body produces observably-zero external state.
This section walks each `l.fail()` call site reachable from a
guarded handler under suppression and proves the gated fail is
correct.

**Listener.go fail-sites reachable under suppression:**

- listener.go:421 — `NewMergeEffect` error in `EnterOC_Merge`. In
  master: unreachable (handler early-returns). In new regime:
  reachable. Sink-gated fail: no-op. Outer parse unaffected.
  Concrete example that would trigger without gating:
  `MATCH (n) WHERE EXISTS { MERGE (m) ON MATCH SET n = m RETURN true }`
  — if `NewMergeEffect` rejects an invalid effect combination,
  master never sees it, new regime with gated fail silently drops
  the error. Parity preserved.
- listener.go:473 — internal MERGE-ON-non-Set-effect belt-and-braces
  guard. In master: unreachable. In new: reachable-but-gated.
  Parity preserved (the belt-and-braces guard exists for a future
  grammar widening; a walked suppressed body cannot trigger it
  today because the grammar rules it out).

**Pattern.go fail-sites:**

- pattern.go:84 — `ErrVariableKindConflict` on
  path-vs-unwind collision inside `collectPatternPart`. Master
  unreachable under EXISTS. New regime: reachable-but-gated. To
  trigger without gating: `MATCH (n) WHERE EXISTS { UNWIND [1] AS
  p MATCH p = ()-->() RETURN true }`. Under gated fail, no error.
  Parity preserved.
- pattern.go:90 — `NewPathBinding` constructor error. Same
  argument. Parity.
- pattern.go:111, 132 — `NewNamedNodeMember` / `NewNamedEdgeMember`
  constructor errors from within recordPathNode/recordPathEdge.
  Under new regime these only fire if `pathMemberSink != nil` (the
  existing check). Under suppression, `setPathMemberSink` no-ops,
  so `pathMemberSink` stays nil, so recordPathNode/recordPathEdge
  early-return at the existing nil-check BEFORE calling
  NewNamed*Member. **This is the key mechanism** for BLOCKER 1's
  pathMemberSink concern: the pointer assignment being sink-gated
  keeps the sink nil, and the existing nil-check inside
  recordPathNode/recordPathEdge stops the writes downstream. Both
  the append and the fail are prevented. Parity.
- pattern.go:292 — invalid hop range (`edgeHopsFromRangeLiteral`
  error) inside `collectEdge`. To trigger:
  `WHERE EXISTS { MATCH ()-[r*99999999999999999999]->() RETURN r }`
  (digits-only overflow: `strconv.Atoi` fails with
  ErrRange). The negative-hop shape `[r*-1]` initially seemed
  intuitive but is grammar-blocked (minus is not admitted by
  IntegerLiteral, so ANTLR rejects it at SyntaxError before
  EnterOC_Match fires); digits-only overflow is the
  actually-walker-reachable trigger, and matches what Test A's
  `exists_fail_parity_—_hop_range_integer_overflow` entry pins.
  Master: unreachable (EnterOC_Match early-returns). New regime:
  `edgeHopsFromRangeLiteral` returns error; sink-gated `l.fail`
  no-ops; `l.err` stays nil; parse succeeds identically to master.
  Parity preserved.
- pattern.go:368 — `NewVarEndpoint` constructor error inside
  `endpoint`. Sink-gated. Parity.
- pattern.go:410 — `ErrVariableKindConflict` inside `mergeBinding`
  when a variable re-appears with a different EntityKind. Under
  suppression, the sweep never runs because `mergeBinding`'s first
  action is the sink-gated no-op — the byVar lookup at line 400
  IS reachable, but the fail at 410 is sink-gated, so no `l.err`.
  Additionally, `part.byVar[variable]` under suppression reads the
  OUTER part's byVar; if the inner variable clashes with an outer
  one, the sweep would find it. Concrete case:
  `MATCH (x) WHERE EXISTS { MATCH ()-[x]->() RETURN x }` — outer
  `x` is a node, inner `x` is an edge. Master: EnterOC_Match
  early-returns, no sweep. New: sweep runs, finds the outer `x`,
  sees the kind mismatch, tries `l.fail(...)` — sink-gated, no-op.
  Parity preserved.

**Expr.go fail-sites:**

- expr.go:107, 112, 118 — `ErrVariableKindConflict` sweeps inside
  `collectUnwind` (byVar/pathBindings/unwindBindings). Under
  suppression the byVar/pathBindings/unwindBindings the sweep
  reads are the CURRENT part's — the outer part, which contains
  no inner-EXISTS bindings (because their appends are gated). The
  sweep may see a collision against an OUTER binding (e.g.
  outer `MATCH (x) WHERE EXISTS { UNWIND [1] AS x ...}`).
  Master: EnterOC_Unwind early-returns, no sweep. New: sweep runs,
  sees outer `x`, sink-gated fail no-ops. Master and new both
  parse-succeed at outer. Parity preserved.
- expr.go:136 — `NewUnwindBinding` constructor error. Sink-gated.
  Parity.
- expr.go:196 — `ErrPatternInProjection` inside
  `collectReturnItem`. To trigger: `WHERE EXISTS { RETURN (n)-->()
  }` — a pattern predicate as a projection inside EXISTS. Master:
  EnterOC_Return early-returns, never reaches `collectReturnItem`.
  New regime: reaches it, sink-gated fail no-ops, parse succeeds
  identically. Parity preserved.
  **This deserves emphasis**: master's guarded behavior means a
  pattern-predicate projection inside EXISTS parses cleanly (the
  outer parse succeeds); the resolver may reject the query later,
  or the engine may. New regime with sink-gated fail preserves
  that same posture. This is the specific BLOCKER 3 counter-example
  linus-2 named; gating `l.fail` is what closes it.
- expr.go:462 — `ErrUnsupportedParameter` inside
  `mineClauseSlotParameter`. This is called from
  `EnterOC_ExistentialSubquery`'s mining loops (un-suppressed
  pre-increment) AND from mining paths inside guarded handler
  bodies. Under suppression from a walked handler body, sink-gated
  fail no-ops. From the pre-increment mining path, l.suppressed()
  is false — fail fires normally. Parity.
- expr.go:614, 627, 643, 660 — `ErrUnsupportedParameter` from
  `mineInlineMap` and `requireAllParametersApproved`. Under
  suppression, sink-gated. Parity.
- expr.go:744, 816 — `ErrNestedPropertyTarget` inside
  `collectSetItem` / `collectRemoveItem`. To trigger:
  `WHERE EXISTS { MATCH (n) SET n.a.b = 1 }`. Master: EnterOC_Set
  early-returns. New: sink-gated fail no-ops. Parity preserved.
- expr.go:758, 769, 788, 807, 822 — `Effect` constructor errors
  from within `collectSetItem` / `collectRemoveItem`. Sink-gated.
  Parity.

**Call.go fail-sites:**

- call.go:43 — `ErrUnknownProcedure`. Called from CALL clauses.
  Under suppression, sink-gated fail no-ops. Parity.
- call.go:72 — `ErrProcedureArity`. Sink-gated. Parity.
- call.go:136 — `ErrUnknownProcedure` on unknown YIELD field.
  Sink-gated. Parity.
- call.go:146, 179 — `NewCallBinding` constructor errors.
  Sink-gated. Parity.

**Listener.go SyntaxError (line 210, 213)**: NOT reachable from
walked handler bodies; called by ANTLR's error listener during
parse, not during walk. Not sink-gated (must fire before walk).
Parity by construction.

Verdict: every `l.fail` call site reachable from a walked
currently-guarded handler body, when sink-gated, produces
observable-zero effect on `l.err` under suppression — matching
master's guarded early-return path. BLOCKER 3 is closed.

### 3.3 BLOCKER 4 proof: ay9 §3.3 under `collectPattern` running with group=0

Under new regime, `EnterOC_Match` calls `l.mintOptionalGroup()`
which returns 0 under suppression. It then calls
`collectPattern(pattern, 0)`. `collectPattern` walks and calls
`mergeBinding` (sink-routed).

`mergeBinding` under suppression is a no-op for BOTH the `byVar`
index-assign and the `bindings` append (Category A atomicity — the
sink method wraps both writes; BLOCKER 1). So no `rawBinding` with
`optionalGroup: 0` enters the outer part. The ay9 §3.3 invariant
"suppressed clauses inside EXISTS consume no id" holds because
`optionalGroupSeq` is not incremented (mintOptionalGroup gated)
AND no binding with optionalGroup=0 is created (mergeBinding
gated). The invariant is DOUBLE-enforced. Parity preserved.

### 3.4 BLOCKER 5 proof: parameter Use position under gated addParameterUse

Under new regime, `EnterOC_Set` under suppression runs
`collectSetItem`, which types a value expression and calls
`addParameterUse(name, p, ExprUse{valueType, ExprInSetValue})`.
Sink-gated `addParameterUse` no-ops. `l.params`, `l.byParam`,
`l.approved` unchanged from this call.

The outer `EnterOC_ExistentialSubquery` mining (pre-increment,
per §1.4) has already fired for this subquery's subtree. If the
same `$p` node was found by `findParameters(c)`,
`addParameterUse(name, p, ExprUse{TypeBool, ExprInPredicate})`
recorded it with `ExprInPredicate` and set `approved[p] = true`.

In master, EnterOC_Set early-returns, so its `addParameterUse` never
fires; the parameter is recorded solely by the mining path with
`ExprInPredicate`. New regime with gated addParameterUse: same
outcome — parameter recorded solely by the mining path with
`ExprInPredicate`. **Byte-identical param.Uses list.** Parity
preserved.

The `approved` map's `if l.approved[p] { continue }` check lives
inside `EnterOC_ExistentialSubquery`'s own mining loop
(listener.go:839) — it dedupes within that loop only. It is NOT
consulted by `addParameterUse` on entry; `addParameterUse` writes
`approved[node] = true` unconditionally as a side effect. So downstream
handler-body reach paths — `EnterOC_Match → mineWhere → pairAddSub →
addParameterUse`, `EnterOC_Set → collectSetItem → addParameterUse`,
etc. — receive no protection from `approved` and would leak a Use
into the outer parameter the moment their handler guard is dropped.
Sink-gating `addParameterUse` with `if l.suppressed() { return }` is
what actually closes BLOCKER 5. That gate lands in Phase B.5 (§5),
BEFORE any Phase C handler migration begins, precisely so no
guard-drop reveals a live suppressed reach path. Confirmed.

### 3.5 BLOCKER 2 proof: save/restore

Site-by-site proofs in §2.2. Each save/restore under suppression is
a no-op because the intervening walk cannot mutate the field
(every mutation is sink-gated). BLOCKER 2 is closed.

### 3.6 BLOCKER 6 stance: discipline-enforced, with a lint gate

The seam is not structurally-enforced. §1.3 acknowledges this
explicitly; §4.3 pins a `forbidigo` lint rule that makes decay a
CI failure. This is the honest answer to BLOCKER 6.

---

## 4. Test plan

### 4.1 Existing tests are the primary fence

All 3,199 parser goldens must pass byte-identical WITHOUT `-update`.
`-update` is forbidden by the bead's acceptance.

TCK acceptance suite counts: 3,459 parse-green, 438 pending, 0
failed — unchanged.

Property tests (`TestPropertyReferentialIntegrity`,
`assertReferentialIntegrity`, `assertNamedBindingsUnique`,
`assertParametersDeduped`, `assertPathMemberKindAgrees`) — green.

`TestSentinelReachability`, `TestMustParse`, `TestMustReject`,
`TestMustRejectGrammar` — green.

The existing corpus already exercises every one of the fourteen
guarded handlers, both at outer scope and under `EXISTS { … }`. If
they all pass byte-identical, the parity §3.2-§3.5 proves is
observed at the wire.

### 4.2 New tests — targeted parity cases

Two additions in `parser_test.go`:

**Test A — EXISTS-nested error-triggering shapes parse-succeed at
the outer:** a table-driven test with one entry per §3.2 fail-site
that could plausibly be triggered by well-formed source under
suppression. Each entry is a query with an EXISTS containing a
shape that would fire a specific `l.fail` at outer scope; the
assertion is `Parse(query)` returns nil error and the resulting
`query.Query` is structurally identical to the same query with a
placeholder EXISTS body (an `EXISTS { MATCH (x) RETURN x }` say).
This pins the parity §3.2 proves — future refactors that ungate a
fail-site will fail these tests.

Entries:
- negative hop inside EXISTS
- pattern-predicate projection inside EXISTS
- nested-property-target SET inside EXISTS
- kind-conflict variable re-use inside EXISTS
- path-vs-unwind kind conflict inside EXISTS
- MERGE inside EXISTS (rejects at engine, parses at outer)

**Test B — parameter Use position parity:** a small case pair.
Query 1: `MATCH (n) SET n.a = $p RETURN n` — records `$p` with
`ExprInSetValue`. Query 2: `MATCH (n) WHERE EXISTS { MATCH (m)
SET m.a = $p } RETURN n` — records `$p` with `ExprInPredicate` (via
the mining path). The assertion: Query 2's `Parameters[0].Uses[0]`
is `ExprUse{TypeBool, ExprInPredicate}`, NOT
`ExprUse{_, ExprInSetValue}`. This pins BLOCKER 5's parity.

### 4.3 Write-fence test (§1.3)

Per Rev 5 mechanism swap: package `cypher` gains a Go
AST-based test (`sink_fence_test.go`) that walks every
`AssignStmt.LHS` and `IncDecStmt.X` on `*listener`-receiver
methods, resolves the selector chain, and asserts the enclosing
function is one of seven sanctioned sink methods:

- `fail` (writes `err`)
- `openBranch` (writes `branches`)
- `recordUnionKind` (writes `combinators`)
- `markWriteSeen` (writes `writeSeen`)
- `mintOptionalGroup` (writes `optionalGroupSeq` via `++`)
- `addParameterUse` (writes `params`, gated by `l.suppressed()`)
- `addParameterUseUnsuppressed` (writes `params`, Category-E
  bypass path per §1.4)

Fenced fields: exactly the six listener collection-state fields
whose sink-routing this spec enforces: `err, branches,
combinators, writeSeen, params, optionalGroupSeq`. Reads are
unrestricted — `build.go` materialisers and `if l.err != nil`
guard idioms are legitimate everywhere.

Failure message names `file:line: l.<field> written in <func>
(not a sanctioned sink)`.

Save/restore idiom (§2.2) is not a concern: those restore forms
write to `l.curPart.refs` / `l.curPart.effects`, not to any of
the six fenced fields. The fence has nothing to say about them.

**Why not `forbidigo`.** Rev 3 §4.3 originally specified a
`forbidigo` rule with seven regex-shaped patterns. First-party
measurement during Phase E implementation showed `forbidigo` v2 is
identifier-chain-based, not source-substring-regex-based: it
silently drops the `\s*=` context and flags reads as well as
writes (26 `l.err` hits, 25 of which were `if l.err != nil` read
sites). Rev 5's revision-history stanza has the full 45/7/38
measurement and rationale for the mechanism swap. The AST fence
is strictly stronger: it retains write-enforcement on all six
fields (including `l.err`, which the narrowest forbidigo option
would have dropped) and adds zero `//nolint` directives.

Grep-verify in CI: `git grep -nE "l\.subqueryDepth > 0"
internal/query/cypher/*.go` returns exactly two hits — the
`suppressed()` seam at listener.go:224 and
`ExitOC_ExistentialSubquery`'s balance-safe decrement — with
zero remaining in `Enter*` handlers.

### 4.4 What we do NOT test

We do not add fault-injection or mock-sink tests. Correctness is
observable through the corpus.

---

## 5. Migration path

TDD, small commits, red-green-refactor. Every step ends
`just test && just fmt-check && just lint` green.

**Phase A — Introduce sink methods as dead code.** Add the sink
methods on `(l *listener)` — every method in §1.2, plus
`suppressed()`. Each method wraps the same mutation the guarded
handlers do today, with `if l.suppressed() { return }` prologue.
DO NOT yet enable the forbidigo rule; migrations in Phase C would
temporarily fail it.

Commit: `refactor(cypher): introduce collection-sink methods on listener`.

**Phase B — `EnterOC_ExistentialSubquery` mining reorder.** Move
mining before increment per §1.4. The findParameters loop calls
`addParameterUseUnsuppressed` (Category-E bypass, listener.go:371)
directly so it stays live once Phase B.5 gates the sink. Test:
existing property tests pass (parameter dedup / attribution
unchanged); the reorder is observably-nil because master's
post-increment ordering worked only because master's addParameterUse
was ungated, and Phase B hasn't yet gated it. This is a "no-op prep"
commit that sets up Phase B.5.

Commit: `refactor(cypher): mine EXISTS parameters before incrementing depth`.

**Phase B.5 — Gate `addParameterUse` under EXISTS suppression.** Add
`if l.suppressed() { return }` as the first line of `addParameterUse`
(expr.go:675). Observably nil at this stack tip: every reach path
into `addParameterUse` from a handler body — `mineWhere`,
`collectSetItem`, `typeExpressionMining`, `mineInlineMap` — sits
inside a handler that still carries its Phase-A subqueryDepth guard,
so no live suppressed caller exists. The only live suppressed-reach
call is `EnterOC_ExistentialSubquery`'s own mining, which already
routes through `addParameterUseUnsuppressed` per Phase B and is
unaffected.

This ordering is load-bearing. Rev-2 attempted to defer this gate
into Phase D (post-migration); dropping any of the five reach-path
handlers' (Match, With, Set, Delete, Remove) guards would then
surface `addParameterUse` under `l.suppressed()` and leak a spurious
PropertyUse / ExprInSetValue / etc. into the outer parameter — a
parity break asserted directly by `TestMustParse` inline `require.Equal`
cases (e.g. `exists_body_mixed_limit_skip_and_where_params`) that
cannot be regenerated. Gating in B.5 keeps every subsequent Phase-C
migration a pure structural swap with no observable behaviour change.

Test: `just test` remains green; all 3,199 goldens byte-identical;
TCK line unchanged. This is the second "no-op prep" commit.

Commit: `refactor(cypher): gate addParameterUse under EXISTS suppression (Phase D advance)`.

**Phase C — Migrate the fourteen Enter handlers, one at a time.**
For each guarded handler in turn:

1. Remove the `if l.subqueryDepth > 0 { return }` prologue.
2. Replace every direct Category A / B / C mutation the body
   performs with the corresponding sink method call.
3. Run `just test`. All 3,199 goldens byte-identical, TCK
   unchanged.
4. Commit: `refactor(cypher): route <ClauseName> through collection sink`.

Handler-by-handler order (mechanical):
`EnterOC_SingleQuery`, `EnterOC_Union`, `EnterOC_Match`,
`EnterOC_With`, `EnterOC_Create`, `EnterOC_Merge`, `EnterOC_Delete`,
`EnterOC_Set`, `EnterOC_Remove`, `EnterOC_Unwind`,
`EnterOC_InQueryCall`, `EnterOC_StandaloneCall`, `EnterOC_Return`,
plus the standalone-CALL curPart-priming block.

Callees migrate in the same commit as the caller that reaches them
(so each commit is handler-scoped and reviewable).

**Phase D — Gate `l.fail`.** With every guarded handler now routing
curPart / structural / scope writes through the sink, gate the last
remaining category: `l.fail` (Category D). `addParameterUse`
(Category E) is already gated by Phase B.5; only `l.fail` remains.

Commit: `refactor(cypher): gate l.fail under EXISTS suppression`.

Test at this step: add Test A + Test B from §4.2 in the same commit
(or a preceding red commit if strict TDD is required — since Test A
asserts what's true today at master too via the guarded
early-return, the tests pass on master, so strict red-first is not
possible; commit them as green-forward instead).

**Phase E — Install the write-fence test.** Per Rev 5 mechanism
swap: add `sink_fence_test.go` in package `cypher` (external
`cypher_test` package) implementing the AST fence described in
§4.3. Enforces the six-field / seven-sink invariant with zero
`//nolint` directives — reads are unrestricted by construction.
No `.golangci.yml` change. RED-analogue receipts required (both
directions: injected non-sink write → fence FAILS naming it;
sink temporarily removed from whitelist → fence FAILS on the
sink's own write; byte-identical restore after each experiment).

Commit: `refactor(cypher): enforce sink-only writes via AST fence test`.

**Phase F — Verify acceptance fences.**

- `grep -n "if l\.subqueryDepth > 0" internal/query/cypher/*.go` —
  only ExitOC_ExistentialSubquery's balance-safe conditional
  decrement remains.
- `git diff master -- internal/query/cypher/testdata/golden/` empty.
- `just test`: 3,459 / 438 / 0 TCK line unchanged.
- `just fmt-check && just lint` green (forbidigo enabled).
- `cypher.New` / `cypher.Parse` signatures unchanged
  (`git diff master -- internal/query/cypher/parser.go`).

Re-run `just fmt-check && just lint` after **any** subsequent edit
per the worktree feedback discipline. LSP diagnostics do not track
disk state and are unreliable after agent edits; fmt-check and
lint are authoritative.

---

## 6. Non-goals

- No `query.Query` model changes; ADR 0008's seal holds.
- No new sentinels; the seventeen existing ones remain reachable
  through the same handlers.
- No performance target; the sink is a per-call `if` on
  `subqueryDepth > 0`, the same instruction the guards perform
  today.
- No rename of `subqueryDepth`. Fourteen reads collapse to one; the
  reader is not renamed.
- No absorption of the `EnterOC_ExistentialSubquery` mining path
  into the sink. It stays un-suppressed (Category E bypass) — its
  purpose is precisely to accumulate parameters that clause-body
  Enters cannot.
- No structural (type-system) enforcement. The lint gate is the
  chosen enforcement mechanism (§1.3, §3.6).

---

## 7. Acceptance summary

Concrete, grep- and gate-verifiable fences:

- `grep -n "if l\.subqueryDepth > 0" internal/query/cypher/*.go`
  returns one hit: `ExitOC_ExistentialSubquery`'s balance-safe
  conditional decrement. Zero hits in `Enter*` handlers.
- `git diff master -- internal/query/cypher/testdata/golden/` empty.
- `just test` prints the same TCK line as master (3,459 / 438 / 0).
- Test A (EXISTS-nested error-triggering shapes parse-succeed) and
  Test B (parameter Use position parity) pass.
- `TestPropertyReferentialIntegrity`, `TestSentinelReachability`,
  `TestMustParse`, `TestMustReject`, `TestMustRejectGrammar` all
  pass.
- `just fmt-check` and `just lint` green; the Rev-5 AST write-fence
  test (`internal/query/cypher/sink_fence_test.go`) green.
- `cypher.New` / `cypher.Parse` signatures byte-identical to master.

When all eight are true, the deepening is done.
