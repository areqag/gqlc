# Model change — the ref-valued-leaf certificate

The ruling for **gqlc-f45qn** ("can the resolver upgrade a rich projection's
`TypeUnknown` from the schema at all, given `ExprProjection` carries no
expression tree") and the implementation brief for its execution bead
**gqlc-t0bk** ("`collect([p.id, p.age])` is `[][]any`"). Every file:line in
this document was read on master `1c31c10a`.

The shape of the problem, before any file path: a property's declared type is
knowable at exactly the point the model discards it. `RETURN p.id` types from
the schema because `RefProjection` reaches a schema-aware resolver arm;
`RETURN [p.id, p.age]` and `collect(p.id)` do not, because `ExprProjection`
and `AggregateProjection` carry only a result type and a flat `[]Ref`, and the
resolver maps that result type through `resolveType`
(`internal/resolver/resolve.go:1725`), which takes no schema. ADR 0003's
Stage-6 sentence — "the resolver upgrades these from the schema" — is true
for a bare `var.prop` and structurally false for every rich expression. This
ruling makes it true for the one class of rich expression where it can be made
true without a tree, and rules `[]any` **permanent and correct** for the rest.

---

## 1. The ruling — answers to the three questions gqlc-f45qn poses

**1. Is a rich projection's `TypeUnknown` upgradable from the schema in
principle?** Yes, exactly once-narrowly: only where every value that will sit
at the committed result type's `TypeUnknown` leaf is *literally a ref lookup*
— nothing computed, folded, or engine-dependent stands between the schema's
declared type and the projected value. For every other shape — heterogeneous
elements (`[p.id, 3]`), folds (`p.id + p.age`, `sum(p.id)`), rich operands
(`collect(size(p.tags))`), engine-dependent aggregates (`avg`) — `[]any` /
`any` is the correct **permanent** answer, not a gap awaiting a cleverer
inference. The flat ref set cannot express those, and a wrong concrete type is
strictly worse than an honest `TypeUnknown` (`internal/query/query.go:1443`).

**2. What may the curated model carry, and what is the stopping rule?** One
additive boolean axis per rich-projection variant: the **ref-valued-leaf
certificate**. Its meaning (normative, §2) is a claim about data the model
*already carries* — that the `Refs()` slice is not merely "touched bindings"
but is exhaustive and leaf-exact. The stopping rule, and the reason the
no-expression-tree line holds: **the certificate may never carry structure.**
Structure lives solely in the committed `Type` (whose only parameterised
variant is `TypeList`, pinned by `TestTypeListIsTheOnlyVariantDeclaringAField`
— so a committed type has at most one `TypeUnknown` leaf). The moment a shape
would need per-position or per-element typing to upgrade — mixed depth, mixed
types, a fold — it is beyond the certificate, forever. This is the same
admission ADR 0003 makes for the undirected-edge marker, `ContainsAggregate`,
and `Distinct`: a scalar, value-affecting axis on a variant; never a tree
fragment. Depth is deliberately **not** the stopping rule: a uniform nested
literal (`[[p.id],[p.age]]`) certifies with the same single bit, because the
list spine is already committed in the `Type` and the bit adds nothing —
whereas a depth cap would be an arbitrary line defended by nothing.

**3. One ruling for both variants, or a separable smaller yes?** One
certificate concept, two carriers. `ExprProjection` mints it for uniform
ref-tree list literals; `AggregateProjection` mints it for `collect` alone,
because `collect(T) = list<T>` puts the *operand's values* at the leaf
verbatim — the only aggregate for which the certificate's claim is true.
`sum`/`min`/`max` over a certified operand are **declined here**: their
committed `TypeUnknown` is the *result of a fold*, and upgrading it requires
the per-aggregate result table (`aggregateResultType`,
`internal/query/cypher/shape.go:242`) on the resolver's side of the boundary —
a real package-surface question filed as its own design pair (§9), not
smuggled through this one.

## 2. Certificate semantics (normative)

A projection carrying `LeavesAreRefs() == true` asserts, all three:

1. **Leaf-exactness.** Every value that appears at a `TypeUnknown` leaf of the
   projection's committed `Type()` is exactly the value of a bare
   `var` / `var.prop` lookup — one of the projection's `Refs()`.
2. **Exhaustiveness.** `Refs()` is non-empty and consists exactly of those
   leaf lookups (order and multiplicity per the existing element-order mining;
   the resolver does not depend on order — §5 unifies, it never indexes).
3. **Structural agreement.** The committed `Type()`'s list spine agrees with
   the literal's actual value structure — guaranteed by the uniform-depth mint
   predicate (§4), and load-bearing: without it, `[[p.id], p.age]` commits
   `list<unknown>` and a fill would type the `[1]` element as `int64`,
   confidently wrong.

False asserts nothing (the today-state for every projection).

## 3. Model change (`internal/query/query.go`)

Following the hk0 / fvo additive-axis convention (ADR 0008 amendments
2026-07-06, 2026-07-11) verbatim:

- `ExprProjection` and `AggregateProjection` each gain an unexported
  `leavesAreRefs bool` field and a `LeavesAreRefs() bool` accessor.
- One new constructor per variant carrying every axis; the existing
  constructors are preserved verbatim as zero-value-safe shorthands
  delegating `leavesAreRefs=false`. Exact names are the warrior's within this
  convention; suggested: `NewExprProjectionWithAxes(refs, t,
  containsAggregate, leavesAreRefs)` and
  `NewAggregateProjectionWithLeafRefs(fn, refs, distinct, t)`.
- `MarshalJSON`: one new key `"leavesAreRefs"` with `,omitempty` — the
  omit-when-false campaign convention, so every golden whose query mints no
  certificate stays **byte-identical** (the hk0 note at `query.go:1500-1504`
  records exactly this and is the precedent to cite).

## 4. Parser change (`internal/query/cypher/`)

One new predicate plus two mint sites. The predicate is a post-hoc walk over
the grammar node, the exact shape of `subtreeContainsAggregate`
(`typing.go:901`) — do **not** thread state through `typeExpressionMining`.

**The predicate.** `refValuedShape(e)` returns (depth, ok): `ok` iff the
expression is, recursively, either a bare `var` / `var.prop`
(depth 0 — reuse the `nonArithmeticAtom` + `refFromNonArithmetic` pair,
`expr.go:383-393`) or a list literal with ≥ 1 element whose elements are all
`ok` **at one common depth** (yielding that depth + 1). Anything else — empty
list, parameter, literal scalar, function call, arithmetic, map literal,
parenthesised composite — is not `ok`. Two elements `ok` at different depths
⇒ the list is not `ok` (§2 clause 3).

**Mint site 1** — `classifyRichExpression` (`typing.go:889`): mint iff
`refValuedShape(e)` is `ok` with depth ≥ 1 (a depth-0 bare ref classifies as
`RefProjection` before ever reaching this function; `(p.id)` under parentheses
is a non-goal, §9).

**Mint site 2** — `classifyAggregateCall` (`expr.go:413`): mint iff
`fn == query.AggCollect` and `len(args) == 1` and `refValuedShape(args[0])`
is `ok` (any depth — `collect(p.id)` and `collect([p.id, p.age])` both
qualify). No other aggregate ever mints (§1 answer 3). `DISTINCT` is
orthogonal and does not block minting.

ANTLR caution for the predicate (measured town knowledge): a non-nil accessor
does not mean non-empty — walk to the leaf and check lengths, and pair every
refusal row in the test table with an ALLOW pin beside it.

## 5. Resolver change (`internal/resolver/scope.go`)

In `projectionType` (`scope.go:885`), the `ExprProjection` and
`AggregateProjection` arms route certified projections to one new helper
before the existing `resolveType(pp.Type())` fallthrough:

1. `base, err := resolveType(pp.Type())` — errors propagate unchanged (so
   `[p, q]` still refuses "list-of-nodes projection"; nothing this ruling adds
   reaches that path, because a committed `TypeNode` leaf is not a
   `TypeUnknown` leaf).
2. Resolve **every** ref through `s.refProjectionType(ref, sch)` — reuse
   verbatim, never reimplement. This is the consistency invariant the whole
   design hangs on: **a certified projection's leaf type is byte-identical to
   what the same ref projected bare would resolve to** — same nullability OR
   (`prop.Nullable || s.nullableBinding[v]`), same plural-candidate
   intersection via `unionNodeProperty`, same carried-alias and CALL-YIELD
   lanes, same refusals. Errors **propagate** (see acceptance change below).
3. Unify the resolved ref types **strictly**: identical `ResolvedType` modulo
   the `Nullable` field, which ORs across refs. `ResolvedProperty{int64}` vs
   `ResolvedProperty{int32}` do not unify (ADR 0002 preserves widths;
   numeric widening is deliberately refused). `ResolvedProperty` vs
   `ResolvedScalar` do not unify even when both "mean int".
4. If unification fails: **degrade** — return `base` unchanged (the honest
   `[]any`; the certificate is spent, not an error).
5. If unification succeeds: return `base` with each `ResolvedUnknown` leaf
   **strictly under at least one `ResolvedList`** replaced by the unified
   type. The under-a-list-spine condition is a one-line belt with a comment:
   a certified projection today always commits a list spine, and a future
   mint site that certified a bare-`TypeUnknown` projection (an `avg`-like
   engine-dependent result, or a fold) must fail toward `any`, never toward a
   confidently wrong concrete. Never fill a bare unknown.

**Acceptance change, named loudly:** a certified projection whose ref fails
schema resolution now **refuses** where it was silently accepted as `[]any`.
`RETURN [p.nosuch]` today resolves (the `ExprProjection` arm never looks at
refs — verified: the only resolver `Refs()` walk is the effects path,
`resolve.go:2264-2266`, which skips); after this change it refuses
`ErrUnknownProperty: p.nosuch`, exactly as bare `RETURN p.nosuch` already
does. Same for the plural-candidate intersection miss. This is the
"rejects anything the schema does not support" posture of ADR 0003 applied
consistently, and it is deliberate. The warrior must sweep existing resolver
fixtures for certified-shape queries over undeclared properties and move any
found from valid to invalid fixtures. (`RETURN [d.year]` on a carried alias is
**not** in this class — the parser's referential-integrity sweep already
rejects a property lookup on a non-binding name, per the note at
`scope.go:953`.)

## 6. Codegen (`internal/codegen/`)

Expected **zero code change**. `buildListElemPlan` (`prepare.go:1239`) already
renders `ResolvedProperty` elements with width mapping, nested lists, and the
`ErrUnrepresentableWidth` refusal. Two things the warrior verifies rather than
assumes:

- What `ResolvedList{ResolvedProperty{Nullable: true}}` renders as — element
  nullability may be flattened by the list-element plan today; whatever the
  existing behaviour is for stored list properties (`ElemNotNull`,
  `prepare.go:1253-1257`) is the behaviour to match, and the golden diff is
  the witness either way.
- The gqlc-t0bk reproducer fixture's golden moves from `[][]any` to
  `[][]int64` (or the schema's declared widths) — this diff IS the bead's
  acceptance test.

## 7. Tests the execution owes

Parser (`internal/query/cypher`):

| query | expected |
|---|---|
| `RETURN [p.id, p.age]` | `ExprProjection`, `list<unknown>`, certificate **minted** |
| `RETURN [[p.id],[p.age]]` | `list<list<unknown>>`, **minted** (uniform depth 2) |
| `RETURN [[p.id, p.age], [p.id]]` | **minted** (ragged lengths are fine; depth is uniform) |
| `RETURN [[p.id], p.age]` | **not minted** (mixed depth — §2 clause 3's falsifier) |
| `RETURN [p.id, 3]` / `[p.id, $x]` / `[p.id + p.age]` / `[]` / `[size(p.tags)]` | **not minted** |
| `RETURN collect(p.id)` / `collect(DISTINCT p.id)` / `collect([p.id, p.age])` | **minted** |
| `RETURN collect(size(p.tags))` / `sum(p.id)` / `min(p.id)` / `avg(p.id)` | **not minted** |
| `WITH [p.id, p.age] AS xs RETURN xs` | WITH-position projection **minted** (flows to the carried alias for free) |

Golden accounting: with `,omitempty`, only fixtures containing minted shapes
rebaseline, and their diff shows exactly the one new key. State the counts in
the PR body per the fvo convention.

Resolver (`internal/resolver`) — each row pins the concrete resolved type,
not just "not unknown":

- `[p.id, p.age]` (both `int64`) → `list<int64>`, nullability OR of the two.
- `collect(p.id)` → `list<int64>`; `collect([p.id, p.age])` → `list<list<int64>>`.
- `[p.id, p.name]` (`int64` vs `string`) → **degrades** to `list<any>`, no error.
- Width mismatch `int32` vs `int64` → degrades (ADR 0002 row).
- `[p.nosuch]` and `collect(p.nosuch)` → `ErrUnknownProperty` (the acceptance
  change, both sides pinned: the same query minus the typo resolves).
- Uncertified control: `[p.id + p.age]` still `list<any>` — the row that
  witnesses the resolver never infers from the flat ref set alone.

The mint predicate and the fill rule are **guards**; the PR owes the mutation
battery of citizen-protocol step 3 (declared victims: the mixed-depth row for
the predicate's depth check; the `[p.id, p.name]` degrade row for strict
unification; the `sum(p.id)` row for the collect-only mint; the bare-unknown
belt via a synthetic certified bare-unknown projection if constructible, else
"no red obtainable, here is what I tried").

## 8. ADR notes owed by the execution PR (drafts)

- **ADR 0008**, one amendment note in the established form: additive
  `leavesAreRefs` axis on `ExprProjection` / `AggregateProjection`, new
  constructors, `,omitempty` wire key, golden-rebaseline counts (measured at
  land time, not invented now), pointer to this spec and bead gqlc-t0bk.
- **ADR 0003**, one stage-note paragraph: "the curated subset now includes
  the **ref-valued-leaf certificate** (ADR 0008 amendment, spec
  model-change-f45qn) — one additive bit per rich-projection variant
  asserting the `Refs()` slice is leaf-exact and exhaustive, letting the
  resolver fill the committed type's unknown leaf from the schema for uniform
  ref-tree literals and `collect`. The certificate carries no structure — the
  spine stays in the committed `Type` — so the no-expression-tree line
  holds."

## 9. Rejected alternatives, and non-goals

Rejected (each with its falsifier):

- **Thread the schema into the expression typer** — contradicts ADR 0003's
  sibling-packages rule; not re-litigated.
- **Infer from the flat ref set without a certificate** — falsified by
  `collect(size(p.tags))` and `[p.id, 3]`: same ref shapes, wrong answers.
- **"All leaves are refs" without uniform depth** — falsified by
  `[[p.id], p.age]`: commits `list<unknown>`, fill would be confidently wrong.
- **Fill any certified unknown leaf (no list-spine belt)** — falsified by
  `sum`/`avg`: a bare unknown can mean "engine-dependent", which no schema
  lookup may overwrite.
- **A new `RefListProjection` sum variant instead of the bit** — covers the
  `ExprProjection` half elegantly but cannot express `collect([p.id, p.age])`
  without giving `AggregateProjection` an operand-projection field, which IS
  a one-node expression tree; two mechanisms where the bit is one.
- **Do nothing** — defensible in isolation, but it leaves ADR 0003's Stage-6
  sentence half-false forever and wont-fixes a P2 with a common real shape;
  declining was the coin-flip this ruling exists to call.

Non-goals (file residue only if someone hits them):

- `sum`/`min`/`max` upgrade over certified operands — **filed as a separate
  design+execution pair at P3** (needs `aggregateResultType` shared across
  the parser/resolver boundary; see §1 answer 3).
- Parenthesised bare refs (`RETURN (p.id)`), map-literal values (`TypeMap`
  carries no value types — no leaf to fill), WHERE-position expressions,
  UNWIND source lists, `collect`'s null-skipping element-nullability
  sharpening.
