# The `query.Query` surface + resolver API

> _Amendment (2026-07-11, gqlc-5xg): the required-bare-pattern
> re-reference axis on `NodeBinding` / `EdgeBinding` — R4 §7.5.4's
> **Axis 2 shape (b)** (the narrower boolean, not shape (a)'s
> `RequiredReferences []ClauseRef`), filed from the R4 close-out
> as gqlc-5xg — is added to `query.Query`. The wire at Stage 14
> recorded first-introduction facts about each binding but not what
> happened to that binding after: when the same variable appeared in
> a later same-Part clause, `mergeBinding`
> (`internal/query/cypher/pattern.go:388-404`) dedupes the re-reference
> and discards its shape, so the resolver could not distinguish a bare
> re-reference (row-drop witness) from an edge-chain re-reference
> (existing endpoint witness) or from another OPTIONAL clause (no
> witness). Class B (`docs/specs/resolver-stage-r4.md §7.5.3` item 2)
> was this cycle's target. Each of the two node/edge Binding variants
> gains one additive field `referencedInRequiredBarePattern bool` and
> one accessor `ReferencedInRequiredBarePattern() bool`. **No new
> constructor** — the axis is a post-introduction fact
> `mergeBinding`'s merge arm sets on the raw binding, forwarded
> through `toBinding` via an unexported per-variant mutator
> `markReferencedInRequiredBarePattern()`; the nine existing binding
> constructors (six pre-ay9 + three ay9 InGroup) are preserved
> verbatim and continue to mint `false`. The parser sets the flag
> iff (i) the enclosing clause is required (non-OPTIONAL, `group ==
> 0`) and (ii) the pattern element is bare (`len(chain) == 0` at
> `collectPatternElement`; a node has no adjacent edge chain link,
> so the label/property filter on NULL drops the row under
> left-join). On an edge variable the parser never sets the flag —
> an edge occurrence always sits inside `-[...]-` between two node
> positions, so the "bare" predicate is grammatically-unreachable;
> the field is added for wire symmetry and forward compatibility.
> `PathBinding` / `UnwindBinding` / `CallBinding` are untouched
> (path members carry the flag through their referenced node/edge
> bindings; UNWIND has no bare-re-reference form; CALL YIELD is not
> re-referenceable). The JSON encoding is **omit-when-false**
> (`,omitempty`), following the wire convention this ADR's hk0
> amendment established; **zero** of 3199 parser goldens
> rebaseline (the TCK does not exercise the shape), all 3199 are
> byte-identical. The Binding interface stays sealed at
> `Kind()`/`Nullable()`/`isBinding()`; bare-ref recording is a
> per-variant field-and-accessor concern. The resolver widening
> (post-5xg PR) adds a pre-pass to `demoteNullableInPlace` that
> demotes any binding whose `ReferencedInRequiredBarePattern()`
> returns true, orthogonal to ay9's group closure — the two
> demotion channels compose commutatively (both flip the same
> table entry to false; bare-ref demotion never seeds
> `demotedGroups`). **Zero existing resolver goldens flip** (no
> in-tree fixture exercises the shape); four new fixtures land
> covering the canonical Class B case, the label-filtered variant,
> the OPTIONAL-re-reference kill-probe, and the compose-with-ay9
> witness. **Residual**: the edge-side missing-witness gap —
> non-bare edge re-references that drop endpoint state at
> `mergeBinding` — is not closed by this narrower boolean; closing
> it needs shape (a)'s `RequiredReferences []ClauseRef` axis and is
> filed as a follow-up bead at close-out. Class A / gqlc-ay9 is
> closed (2026-07-10; ADR 0008 amendment above). With gqlc-5xg
> adopted, the R4-inherited Class B row across the R4-R7 stage
> specs is retired in the docs-errata cycle (§8.6). See
> `docs/specs/unfreeze-5xg-required-bare-ref.md` for the full
> contract, the 0-golden flip census, the constructor-strategy
> and predicate-derivation decisions, and the fence commands._

> _Amendment (2026-07-07, gqlc-ay9): the OPTIONAL-group membership
> axis on `NodeBinding` / `EdgeBinding` — R4 §7.5.4's **Axis 1**,
> filed from the R4 close-out as gqlc-ay9 — is added to
> `query.Query`. The wire at Stage 14 recorded per-binding
> `Nullable()` but not which bindings were co-introduced by the same
> OPTIONAL MATCH clause, so
> the resolver's R4 regime-(a) demotion could not propagate a proven
> member's non-nullness to its clause siblings (Class A,
> `docs/specs/resolver-stage-r4.md §7.5.3` items 1 + 3). Each of the
> two OPTIONAL-introducible Binding variants gains one additive
> field `optionalGroup int`, one accessor `OptionalGroup() int`, and
> one new constructor per OPTIONAL-introduced shape
> (`NewNullableNodeBindingInGroup`, `NewNullableEdgeBindingInGroup`,
> `NewNullableVarLengthEdgeBindingInGroup`); the six existing
> binding constructors are preserved verbatim and continue to mint
> group 0 ("not in any OPTIONAL group"). Group ids are minted
> per-parse by the parser — fresh id per OPTIONAL MATCH clause,
> unique across the whole query — and recorded only at first
> introduction, matching ADR 0006's nullability discipline.
> `PathBinding` / `UnwindBinding` / `CallBinding` are untouched
> (their `Nullable()` is not OPTIONAL-derived; a named path's group
> facts flow through its member bindings, the Stage-8 posture). The
> JSON encoding is **omit-when-zero-value** (`,omitempty`),
> following the wire convention this ADR's hk0 amendment
> established; 100 of 3199 parser goldens rebaseline, 3099 are
> byte-identical. The Binding interface stays sealed at
> `Kind()`/`Nullable()`/`isBinding()`; group membership is a
> per-variant field-and-accessor concern. The resolver widening
> (post-ay9 PR) extends `demoteNullableInPlace` to the group-closure
> fixed point — "if a required chain proves any member of an
> OPTIONAL group exists, every member of that group demotes" —
> flipping two resolver goldens (`demote_chained_from_required`,
> `demote_from_anonymous_required_edge`) and adding no sentinel.
> **Residual**: the resolver's cross-Part carry is name-granular and
> does not yet carry group ids, so a WITH-carried binding demotes
> without its co-introduced siblings; closing this needs only a
> resolver-internal `branchState` extension (no further model
> change) and is filed as a follow-up bead at close-out. Class B —
> the same-Part second-reference gap (R4 §7.5.3 item 2, Axis 2,
> gqlc-5xg) — is a missing-witness gap this axis deliberately does
> not close. See `docs/specs/unfreeze-ay9-optional-group.md` for the
> full contract, the 100-golden flip census with spot witnesses, the
> constructor-strategy and id-scope decisions, and the fence
> commands._

> _Amendment (2026-07-07, gqlc-0ig): the per-position CALL-arg
> attribution axis on `CallBinding` — recorded as the R7 §7.1.1
> CALL-arg-attribution deferral — is added to `query.Query`. The
> R7-shipped
> "no arg-site check" posture
> (`docs/specs/resolver-stage-r7.md §7.1.1`) was an honest
> workaround for the wire's missing per-argument attribution; the
> resolver widening (post-0ig) walks each CallBinding's `Args()`
> per Phase A1 and checks each mined type against
> `procsig.Registry.Lookup(procedure).Params[i].Token` under the
> ADR 0007 Stage-14 note's assignability rule
> (`docs/adr/0007-pre-freeze-scope-full-opencypher-surface.md`
> lines 172-174: NUMBER assignable-from INTEGER-or-FLOAT). The
> `CallBinding` sum gains one additive field `args []CallArg`, one
> new positional constructor `NewCallBindingWithArgs`, one new
> accessor `Args() []CallArg`, and one new sub-type `CallArg` with
> `NewCallArg(t Type)` and `Type() Type`. The `CallBinding.args`
> encoding is **omit-when-zero-length** (`,omitempty`), following
> the wire convention this ADR's hk0 amendment established for
> additive axes. `procsig.TypeToken` stays a signature-only
> vocabulary (ADR 0007 Stage-14 note); the wire records only the
> parser-mined `query.Type`, and the resolver bridges by looking up
> the procedure name against the compile-time `procsig.Registry`.
> The Binding interface stays sealed at one method (`isBinding()`);
> Args attribution is a CallBinding-only field-and-accessor concern.
> See `docs/specs/unfreeze-0ig-call-args.md` for the full contract,
> the 28-golden rebaseline accounting with per-scenario spot
> witnesses, the layering divergence from the bead text's
> parser-emits-sig-token proposal, and the semantic-diff-only fence
> commands._

> _Amendment (2026-07-06, gqlc-fvo): the Use → Part attribution
> axis on `PropertyUse` / `ExprUse` / `ClauseSlotUse` — recorded
> implicitly as the R5 §4.2.4 follow-up bead — is added to
> `query.Query`. The R5-shipped
> "any-valid-witness" rule
> (`internal/resolver/resolve.go:750-811`) was an honest
> workaround for the wire's missing Part attribution; the
> resolver widening (post-fvo) narrows to lexical-Part witness
> exactly against `scopes[u.Part()]`, closing the primary
> gap. **Residual**: attribution is Part-granular, not
> post-projection-scope-granular; a WITH…WHERE whose trailing
> WHERE aliases the WITH-projected name back to a same-name
> shadow (e.g. `MATCH (a:Post) WITH a.title AS a WHERE
> a.x = $p RETURN a`) lexes the WHERE's `$p` in the CLOSED
> Part (see §7.6 residual note in the fvo spec). Under a
> shape where the CLOSED-Part scope's binding for the shadowed
> name admits the property lookup, the widened resolver
> still admits a semantically-invalid query — same
> admit-shape as R5 §4.2.4, surviving the widening. No
> regression versus branch base (any-valid-witness also
> admitted this shape). Filed as a follow-up bead (§9
> non-goals in the fvo spec)._
> Each `Use` variant gains an additive `part int` field, one
> new positional constructor (`NewPropertyUseAt` / `NewExprUseAt`
> / `NewClauseSlotUseAt`), one new accessor `Part() int`, and
> one new JSON key `"part"` with `,omitempty`. The existing
> zero-argument-Part constructors are preserved verbatim.
> The parser populates the axis at `addParameterUse`
> (`internal/query/cypher/expr.go:633`) via
> `l.currentPartIndex() = len(l.curBranch.parts) - 1` — the
> branch-relative index of the Part the Use lexically occurs
> in, well-defined at every emission site by the
> priming-and-swap discipline of `EnterOC_SingleQuery` /
> `EnterOC_With` / `EnterOC_StandaloneCall`. The JSON encoding
> is **omit-when-zero-value** (`,omitempty`), following the wire
> convention this ADR's hk0 amendment established for additive
> axes. The Use interface stays sealed at one
> method (`isUse()`); Part attribution is a per-variant
> field-and-accessor concern. See
> `docs/specs/unfreeze-fvo-use-part.md` for the full contract,
> the emission-site table, the zero-golden rebaseline
> accounting, the reversed alias-shadow discriminating fixture,
> and the semantic-diff-only fence commands._

> _Amendment (2026-07-06, gqlc-hk0): the ContainsAggregate axis
> on `ExprProjection` — recorded as an escape hatch in the "Later
> additions" list — is added to `query.Query`. Shape A (promote
> nested-aggregate residuals to
> `AggregateProjection`) was second-ranked in `docs/specs/
> resolver-stage-r5.md §4.5.3.3` and is **retired**: it is a semantic
> widening of an existing sum variant, requiring every downstream
> consumer of `AggregateProjection` to audit. Shape C (`ReturnItem.
> TextSpan` / `ExprProjection.OriginalText` text-based recovery) is
> **retired**: R5 §4.5.3.4 (B7 evidence) demonstrates that no
> resolver-side re-parse of the recovered text can recover the
> aggregate structure `classifyRichExpression` drops at
> classification — the parser must emit the discriminator. The
> committed strategy is therefore Shape B: an additive
> `containsAggregate bool` field on `ExprProjection`, set
> parser-side during `classifyRichExpression`'s walk of the
> expression subtree (a boolean scan for the two aggregate arms
> `typeAtom` already recognises — `internal/query/cypher/typing.go:
> 340` and `:358-365`), with the walker respecting the typing
> walk's sub-scope boundaries (`OC_ExistentialSubquery`,
> `OC_PatternPredicate`, `OC_ListComprehension`,
> `OC_PatternComprehension` — mirroring `typing.go:382-403`). The
> JSON encoding is **omit-when-false** (`,omitempty`), which
> establishes the wire convention for later additive axes: they
> emit **omit-when-zero-value**, diverging from the Stage-0–14
> always-emit precedent (`directed`, `nullable`, `returnsAll`,
> `hops`) because golden rebaselines are the primary auditability
> surface and always-emit forces near-total 3199-file rebaselines
> on each additive cycle. See
> `docs/specs/unfreeze-hk0-containsaggregate.md` for the full
> contract, the walker boundaries, the 20-golden rebaseline set,
> and the semantic-diff-only fence command._

ADR 0004's feature-complete target is reached. All fifteen parser stages (the
read core of Stages 0–5 plus the ADR 0007 expansion through Stage 14) and the
TCK corpus sweep are complete, and the two cardinality fixes — the Part-level
DISTINCT axis and aggregate-kind preservation over rich arguments — have
landed. This ADR records the `query.Query` surface at Stage 14 completion and
pins the resolver's package and API (discharging the deferral in ADR 0006's
consequences), opening consumer work — resolver, codegen, generated driver.
The additions inventoried in the amendment notes above have landed since;
each is a coordinated model + resolver change tracked by a bead and
documented in `docs/specs/`.

## Context

The progress meter at Stage 14 completion: godog over the entire vendored
TCK reports **3897 scenarios — 3459 parse-green, 438 pending, 0 failed**.
Every pending scenario is skiplist-pinned to bucket 3 of ADR 0007 — runtime
semantics below the type-interface boundary (ADR 0005), where the parser
accepts and the driver raises on the original text. No scenario is pending
for "not supported yet". Of the four `ErrUnsupported*` sentinels that
carried that meaning, three are retired — `ErrUnsupportedProjection`
(Stage 6), `ErrUnsupportedPattern` (Stage 8), `ErrUnsupportedClause`
(Stage 14). `ErrUnsupportedParameter` remains exported, with fail-sites for
the parameter shapes the model deliberately does not carry (a `$param`
occupying a clause slot's expression, a whole-property-map parameter, a
parameter on an anonymous pattern element); at Stage 14 every corpus
occurrence of those shapes is a negative (`Fail when …`) scenario, where the
rejection is the asserted behaviour — zero pending scenarios route through
it. It stays in the acceptance harness's progress-meter set deliberately: a
future corpus shape that reaches it from a positive scenario surfaces as
PENDING rather than as a silent mis-model.

Two model fixes were pulled in before the resolver opens because they are
cardinality-critical for the resolver's grouping-key work, where an aggregate
column must be distinguishable from a plain expression column:

- **Part-level DISTINCT axis** (`Part.Distinct`): `RETURN DISTINCT` / `WITH
  DISTINCT` as a first-class bit, independent of the aggregate-input and
  UNION deduplication axes.
- **Aggregate kind over rich arguments**: `sum(x + 1)` lowers as an
  `AggregateProjection` with its `AggregateFunc` kind, refs, and DISTINCT
  preserved, instead of decaying to an `ExprProjection`. Nested aggregates
  inside a rich expression (`count(n) + 1`) remain `ExprProjection` — a
  documented deferral with a recorded resolver strategy (see the revision
  protocol below).

ADR 0004's economic argument now applies: while the parser was the only
consumer, model churn was cheap; from here every change propagates through
the resolver, codegen, and generated driver code. This ADR records the
shape as of Stage 14 completion — decided against the corpus alone, before
any consumer exists — as the reference for the coupled downstream work.

## Decision

### The `query.Query` surface

`query.Query` is the model as of Stage 14 completion — sealed sums over
marker-method interfaces, smart constructors that make illegal states
unrepresentable, and tagged-union JSON marshalling (the golden-file wire
shape). The inventory:

**Top-level structure.** `Query` = `Branches []Branch` (UNION-joined arms,
always at least one) + `Combinators []UnionKind` (how each subsequent branch
joins its predecessor) + `Parameters []Parameter` (deduplicated query-wide in
first-appearance order) + `StatementKind` (the driver's transaction-mode
axis). `Branch` = `Parts []Part` (WITH-bounded scope segments). `Part` =
`Bindings` + `Returns []ReturnItem` + `ReturnsAll bool` + `Distinct bool` +
`Effects []Effect`, guarded by `NewPart`'s at-least-one-of invariant.

**Sealed sums** (variant counts as of Stage 14):

| Sum | Variants |
|---|---|
| `Binding` ×5 | `NodeBinding`, `EdgeBinding`, `PathBinding`, `UnwindBinding`, `CallBinding` |
| `PathMember` ×4 | `NamedNodeMember`, `NamedEdgeMember`, `AnonNodeMember`, `AnonEdgeMember` |
| `Endpoint` ×2 | `VarEndpoint`, `InlineEndpoint` |
| `Projection` ×5 | `RefProjection`, `LiteralProjection`, `FuncProjection`, `AggregateProjection`, `ExprProjection` |
| `Type` ×17 | `bool`, `int`, `float`, `string`, `null`, `map`, `node`, `edge`, `list<T>` (parameterised over an element `Type`; the wire emits the instantiation — `list<int>`, `list<edge>`, …), `unknown`, `date`, `time`, `localtime`, `datetime`, `localdatetime`, `duration`, `path` |
| `Use` ×3 | `PropertyUse`, `ExprUse`, `ClauseSlotUse` |
| `Effect` ×8 | `CreateEffect`, `DeleteEffect`, `SetPropertyEffect`, `SetEntityEffect`, `SetLabelsEffect`, `RemovePropertyEffect`, `RemoveLabelsEffect`, `MergeEffect` |
| `SetEffect` ×3 | the three `Set*Effect`s — the sub-sum `MergeEffect`'s `ON MATCH` / `ON CREATE` branches carry |

**Axes and enums:**

- `UnionKind` ×2 — `union` / `unionAll`.
- `StatementKind` ×2 — `read` / `write`; write iff any outer-scope write
  clause in any branch (a write inside `EXISTS { … }` does not flip it).
- `EdgeHops` — the variable-length hop range (`min`/`max`, either absent);
  negative bounds rejected at construction, empty ranges accepted.
- `directed` on `EdgeBinding` — canonical source→target order when set;
  textual order for the resolver's two-orientation trial when not.
- Three independent DISTINCT axes — `Part.Distinct` (projection body),
  `AggregateProjection.Distinct` (aggregate input), `UnionKind`
  (cross-branch); each is a different cardinality decision on a different
  model surface.
- `AggregateFunc` ×8 — `count`, `sum`, `collect`, `min`, `max`, `avg`,
  `stdev` (covering `stDev` / `stDevP`), `percentile` (covering
  `percentileCont` / `percentileDisc`).
- `ClauseSlot` ×2 — `skip`, `limit`.
- `ExprPosition` ×4 — `projection`, `predicate`, `setValue`, `deleteTarget`
  (the producer/consumer axis on `ExprUse`).
- `SetOp` ×2 — `replace` (`=`) / `merge` (`+=`) on `SetEntityEffect`.

**Two faces.** The `query.Query` surface has two faces: the exported Go API
of `internal/query` (types, marker methods, constructors, accessors) and
the JSON wire shape the golden suite pins (tagged unions, wire conventions,
key names). Consumer packages depend on either.

### The resolver API

ADR 0006 deferred the resolver's package path, constructor signature, and
output type to this ADR. Pinned:

- **Package `internal/resolver`** — a sibling of `internal/query` and
  `internal/schema`, importing both plus `internal/procsig`. None of the
  three import it back; the model packages stay consumer-free.
- **`resolver.New(s schema.Schema, opts ...Option) *Resolver`**, with
  **`WithRegistry(r procsig.Registry) Option`** supplying the procedure
  registry. The constructor binds the per-application compile-time inputs —
  the same functional-options shape as `cypher.New`.
- **`(*Resolver).Resolve(q query.Query) (ValidatedQuery, error)`** — a pure
  function of the constructor's inputs and its argument. No I/O, no state
  mutation; resolving the same query twice yields the same result.
- **`ValidatedQuery` lives in `internal/resolver`.** It is the resolver's
  output vocabulary, not the parser's: the parser must stay ignorant of
  schema-resolved types (ADR 0003's sibling rule), and codegen consumes the
  resolver's output, so the type belongs to the package that produces it.

A bare free function `Resolve(q, s)` — ADR 0003's original
`(query.Query, schema.Schema)` phrasing — was considered and rejected: the
procedure registry (ADR 0007) is a second compile-time input of the same
"user-authored, machine-read at generation time" kind as the schema, and
folding both into a constructor keeps the per-query call site one-argument
and gives future compile-time inputs a home that does not break every call
site. Purity is unaffected: `Resolve` remains a function of
`(query.Query, schema.Schema[, procsig.Registry])`, merely spelled as a
method.

The resolver's build approach — staging, test strategy, error posture,
`ValidatedQuery`'s internal vocabulary — is ADR 0009's subject, not this
one's. This ADR pins only the surface consumers see.

### Additions since Stage 14

The additions inventoried below have landed after the shape recorded above,
each documented in the amendment notes at the top of this ADR. `query.Query`
is an internal package (`internal/query`) — there is no external consumer to
protect and no formal revision protocol; changes are coordinated with the
resolver, codegen, and driver code the same way any coupled internal
packages are (compile errors, test failures, PR review). Additions land as
dated amendment notes on this ADR (the ADR 0003 stage-note convention) with
their golden rebaseline included in the amendment PR. Removals or re-typings
of existing surface are rewrites of coupled packages and are recorded here
the same way, without a formal ceremony beyond documenting what changed.

The additive-only, omit-when-zero-value wire convention this ADR's hk0
amendment established (see the amendment note) is preserved for later
additions on its own merits — omit-when-zero-value keeps golden diffs
scoped to fields that actually changed, rather than churning all 3199 files
on each additive cycle.

**Later additions** — inventory:

- **shortestPath selector axis** on `PathBinding` (see posture below).
- **`EXISTS { … }` Use precision** (gqlc-33k.3): parameters inside an
  existential subquery currently record coarse `ExprUse`s.
- **`CreateEffect` created-vs-prebound split** (gqlc-33k.4): deferred until
  a consumer demonstrates the need — no speculative modelling.
- **`ContainsAggregate` axis on `ExprProjection`** — adopted
  2026-07-06 (see the amendment note above and
  `docs/specs/unfreeze-hk0-containsaggregate.md`). Populated
  parser-side by `classifyRichExpression`'s subtree walk; consumed
  by the resolver's `fillGroupingKeys` (`internal/resolver/resolve.go`)
  to discriminate aggregate-carrying residuals from grouping-key
  candidates. Never inferred from `Type`.
- **`Use.Part` attribution axis on `PropertyUse` / `ExprUse` /
  `ClauseSlotUse`** — adopted 2026-07-06 (see the amendment
  note above and `docs/specs/unfreeze-fvo-use-part.md`).
  Populated parser-side by `addParameterUse` from
  `l.currentPartIndex()`; consumed by the resolver's
  `witnessAcrossScopes` (`internal/resolver/resolve.go:750-811`)
  to witness a PropertyUse against exactly the lexical Part's
  scope, closing R5's any-valid-witness gap over the primary
  shape. Residual (WITH…WHERE aliased-shadow across the
  CLOSED Part) is honestly recorded in the amendment note
  above and filed as a follow-up (§7.6 and §9 in the fvo
  spec).
- **`CallBinding.Args` axis** — adopted 2026-07-07 (see the
  amendment note above and `docs/specs/unfreeze-0ig-call-args.md`).
  Populated parser-side by `collectCall`'s existing arg-mining
  loop capturing the mined `query.Type` per argument position;
  consumed by the resolver's Phase A1 arg-site assignability walk
  (`internal/resolver/resolve.go` CallBinding arm — see 0ig
  resolver-widening PR) which looks up
  `procsig.Registry.Lookup(procedure).Params[i].Token` and checks
  under ADR 0007's NUMBER-assignable-from-INTEGER-or-FLOAT rule.
  `procsig.TypeToken` stays signature-only vocabulary; the wire
  records only `query.Type`. The R7 §7.1.1 deferral is closed.
- **`OptionalGroup` axis on `NodeBinding` / `EdgeBinding`** —
  adopted 2026-07-07 (see the amendment note above and
  `docs/specs/unfreeze-ay9-optional-group.md`). Populated
  parser-side by the OPTIONAL threading through `collectPattern`
  (fresh id per OPTIONAL MATCH clause, query-scoped, minted in
  `EnterOC_Match`); consumed by the resolver's Phase D
  group-closure demotion (`demoteNullableInPlace`,
  `internal/resolver/resolve.go` — see the ay9 resolver-widening
  PR), closing R4 §7.5.3 Class A (items 1 + 3). Class B (item 2)
  remains open under gqlc-5xg.
- **`ReferencedInRequiredBarePattern` axis on `NodeBinding` /
  `EdgeBinding`** — adopted 2026-07-11 (see the amendment note
  above and `docs/specs/unfreeze-5xg-required-bare-ref.md`).
  Populated parser-side by `mergeBinding`'s merge arm at
  `internal/query/cypher/pattern.go:388-404` when the current
  occurrence is required and bare; consumed by the resolver's
  Phase D bare-ref pre-pass in `demoteNullableInPlace`
  (`internal/resolver/resolve.go` — see the 5xg resolver-widening
  PR), closing R4 §7.5.3 Class B (item 2). The edge-side non-bare
  missing-witness residual is filed as a follow-up bead at
  close-out. With ay9 (Class A, closed 2026-07-10), R4 §7.5.3's two
  named classes are closed.

### shortestPath is a dialect extension

`shortestPath()` / `allShortestPaths()` are Neo4j dialect extensions, not
openCypher: the vendored grammar (`internal/grammar/cypher/Cypher.g4`) has no
rule for either, and the vendored TCK contains zero occurrences. They are
therefore not a gap in the corpus sweep. If dialect support is taken up,
the expected shape is an additive selector axis on `PathBinding` — the
same move `EdgeHops` made on `EdgeBinding`.

## Consequences

- **Consumer work is open.** The resolver (ADR 0009), codegen, and the
  generated driver may now be written against the shape recorded above —
  the payoff ADR 0004 deferred them for.
- **ADR 0003's provisional header note is replaced** with a milestone note
  pointing here; its "stable contract" framing describes the parser-to-
  resolver interface as recorded. The stage-note diary on ADR 0003 remains
  as history. CONTEXT.md's **Resolver** and **Validated query** entries
  carry the pinned API.
- **Model changes propagate through coupled internal packages.** Anything
  the corpus did not force into the model arrives as a later addition on
  this ADR (see "Additions since Stage 14"); a change is judged against
  the churn it creates in the resolver, codegen, and driver code. Normal
  Go tooling (compile errors, test failures, PR review) surfaces the
  coupling; `internal/` has no external consumer, so there is no formal
  contract preventing changes — only the coupling cost, and the discipline
  of keeping additions coordinated with their consumers.
- **The 438 pending scenarios are a stable posture, not debt.** They assert
  runtime semantics the type interface deliberately does not carry
  (ADR 0005); they stay pending permanently unless the boundary itself is
  revisited.
