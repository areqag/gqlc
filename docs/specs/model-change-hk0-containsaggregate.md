# Model change — `ExprProjection.ContainsAggregate` (Shape B)

The implementation brief for cycle **gqlc-hk0** of the model-additions
campaign: an additive `ContainsAggregate bool` axis on
`query.ExprProjection`, populated parser-side during the walk
`classifyRichExpression` already performs, closing the R5 uniform-exclude
grouping-key gap (`docs/specs/resolver-stage-r5.md §4.5.3`) without
touching the wire shape's zero-value semantics.

This brief is the **contract for the whole hk0 cycle**: it spans the
spec PR (this file), the model-change PR (model + parser + ADR 0008
amendment + parser-test rebaseline; resolver untouched and green), and
the resolver-widening PR (fillGroupingKeys widens to discriminate
`ContainsAggregate`; new discriminating fixture + adjusted golden set).
Both code PRs land as coordinated model + resolver changes
(§Additions since Stage 14) — coordinated change with consumers, dated amendment,
golden rebaseline whose diff shows only the new surface.

The four other change beads (`gqlc-fvo`, `gqlc-0ig`, `gqlc-ay9`,
`gqlc-5xg`) and codegen are **out of scope** — this campaign closes
hk0 alone. See §9 for the non-goals table.

---

## 1. Deliverables

Spec cycle (Cycle 1) — this PR:

- `docs/specs/model-change-hk0-containsaggregate.md` — this file.

Cycle ( 2, follow-up PR):

- `internal/query/query.go` — one additive field
  `containsAggregate bool` on `ExprProjection`, one new constructor
  `NewExprProjectionWithAggregate(refs, t, containsAggregate)`, one new
  accessor `ContainsAggregate() bool`, one new field in the
  `MarshalJSON` struct (`ContainsAggregate bool `json:"containsAggregate"`).
  The existing `NewExprProjection(refs, t)` constructor is preserved
  verbatim as the zero-value-safe shorthand (delegates to the new
  constructor with `containsAggregate=false`) — §4.1.
- `internal/query/type_test.go` — new test
  `TestNewExprProjectionWithAggregateTrue` pinning the axis wiring
  (constructor → accessor → JSON round-trip). Existing
  `TestNewExprProjection`, `TestNewExprProjectionAllowsNoRefs`,
  `TestExprProjectionMarshalJSON` stay verbatim — they exercise the
  zero-value default and pin the wire back-compatibility fence. §4.1
  and §5.
- `internal/query/cypher/typing.go:866-876` —
  `classifyRichExpression` gains a boolean scan of the sub-tree for
  aggregate calls (a helper `subtreeContainsAggregate` alongside
  `typeExpressionMining`), passing the bit to
  `NewExprProjectionWithAggregate`. Total lines added: ≤ 30. §4.2.
- `internal/query/cypher/parser_test.go` — the deferral pin
  `"count in arithmetic"` (`parser_test.go:1319-1327`) flips from
  `NewExprProjection([]query.Ref{{Variable: "n"}}, query.TypeInt{})`
  to `NewExprProjectionWithAggregate([]query.Ref{{Variable: "n"}},
  query.TypeInt{}, true)`. Its narrative comment updates
  (§4.3). All other parser-test pins using `NewExprProjection`
  (24 assertions, §4.4) stay verbatim — they carry non-aggregate
  expressions whose bit is zero, i.e. the existing constructor still
  produces the correct shape.
- `docs/adr/0008-query-model-surface-resolver-api.md` — one dated
  amendment note (top of the file, ADR 0003 stage-note convention)
  recording Shape B's adoption and the A/C retirement rationale.
  The "Known deferred additions" entry for `ContainsAggregate`
  updates to point at the amendment. Verbatim text pinned in §6.
- Parser goldens (in-code table `mustParse` values) rebaseline **only
  for the one deferral pin** (§4.3); every other pin whose value
  contains an `ExprProjection` continues to compare via
  `require.Equal` against a zero-value `ContainsAggregate` — a
  bit-for-bit match because Go struct zero-value equality holds.
  §4.4 enumerates the affected pins.

Resolver-widening cycle (Cycle 3, follow-up PR after the model-change PR
merges):

- `internal/resolver/resolve.go:571-602` — `fillGroupingKeys` widens
  its `ExprProjection` arm: an `ExprProjection` with
  `ContainsAggregate() == false` becomes a grouping-key candidate
  (marked `true` under the `hasAggregate` gate); one with
  `ContainsAggregate() == true` continues to be excluded. §7.
- `internal/resolver/validated.go:15-27` — the doc comment on
  `Column.GroupingKey` updates to remove the "uniform-exclude"
  wording; the axis semantics themselves are unchanged. §7.
- Two new resolver fixtures, one per newly-observable sub-case:
  `aggregate_with_expr_grouping_key.cypher` + golden (sub-case 4 —
  the R5-spec-recorded target at
  `docs/specs/resolver-stage-r5.md §4.5.3.5`), and
  `aggregate_with_expr_only_grouping.cypher` + golden (sub-case 5 —
  the gate-widening round-2 ruling). §7.4a and §7.4b.
- Every pre-existing resolver-valid golden is byte-identical after
  the widening PR (§7.5). The fence check runs from the worktree:
  `just test` without `-update` over the 116-fixture-pair R0–R7
  corpus at branch base (`ls test/data/resolver/valid/*.cypher |
  wc -l` = 116). Only the two new fixtures appear in the diff.

Nothing downstream of the resolver is built — the resolver's widening
lands the corrected grouping-key semantics; codegen consumes them
under a future ADR.

---

## 2. Frame — what changes and what stays

R5 shipped with a documented under-approximation: every
`ExprProjection` residual — aggregate-carrying or not — is uniformly
excluded from the grouping-key set (`docs/specs/resolver-stage-r5.md
§4.5.2`, `§4.5.3.2`; witness `internal/resolver/resolve.go:571-602`).
The R5 spec pins the source (`§4.5.3.4`): the parser's
`classifyRichExpression` drops the aggregate structure at
classification (verified verbatim at
`internal/query/cypher/typing.go:866-876`), so no resolver-side
re-parse of the original text — including any of Shape A
(`OriginalText`) or Shape C (`ReturnItem.TextSpan`) — recovers it.

ADR 0008's "Known deferred additions" registered a
`ContainsAggregate` axis on `ExprProjection` as the escape hatch:

> **`ContainsAggregate` axis on `ExprProjection`** — the escape hatch
> recorded on gqlc-gyw. The committed strategy for grouping-key discovery
> over expression residuals (nested aggregates like `count(n) + 1`) is a
> resolver-side re-parse of the projection's original text span; the axis
> is added only if that proves untenable, and is never inferred from `Type`.

`(docs/adr/0008-query-model-surface-resolver-api.md:166-169)`.

R5 §4.5.3.4 pinned the "untenable" — the escape-hatch condition
fires. This cycle promotes the axis from escape hatch to committed
strategy under the coordinated change with consumers, records the
promotion on ADR 0008 with a dated amendment note (§6), lands the
axis parser-side (§4), and widens the resolver's grouping-key rule
to consume it (§7).

**What does not change.** The wire shape's zero-value semantics: a
plain `ExprProjection` (arithmetic, list literal, quantifier — no
nested aggregate) continues to serialise to a JSON blob whose
`containsAggregate` field is `false`. Every parser-test pin whose
`ExprProjection` value has no nested aggregate today is bit-for-bit
identical to its post-widening shape (Go struct zero-value
equality; §4.4). Every existing consumer that constructs
`NewExprProjection(refs, t)` gets the same `ContainsAggregate =
false` semantics — the existing constructor is preserved unchanged.

**What does change.** The one parser-test pin `"count in arithmetic"`
(`parser_test.go:1319-1327`) rebaselines — its `ExprProjection` now
carries `ContainsAggregate = true`. The wire JSON of ExprProjection
gains one field: `containsAggregate` (omit-when-false; §4.1.3
records the wire convention and the fence).
The ADR 0008 amendment records the axis's promotion. The resolver's
`fillGroupingKeys` gains a gate arm and a loop arm on the new bit
(§7.1: the top-level presence gate widens with an `ExprProjection`
`ContainsAggregate=true` disjunct, and the per-column key-emission
loop widens with an `ExprProjection` `ContainsAggregate=false`
INCLUDE arm alongside the existing `RefProjection` /
`LiteralProjection` / `FuncProjection` include arms —
`AggregateProjection` remains excluded by case-absence, not by an
explicit arm).

**Why Shape B and not Shape A.** Shape A (promote nested-aggregate
residuals to `AggregateProjection`) is a semantic widening of the
existing sum variant — every downstream consumer of
`AggregateProjection` would need to be audited. Shape B is a bit on
one existing variant — one new field, one new accessor, one new
constructor overload, one JSON key. Shape C is retired (`R5 §4.5.3.3`
final ranking): text-based recovery cannot re-materialise the
aggregate structure that `classifyRichExpression` dropped.

---

## 3. Mining — what the query model records today

Every claim in §4 rests on citations here. Re-verify each file:line at
branch base `origin/master @ e77f33e` before writing code — line
numbers can drift on merge.

### 3.1 `ExprProjection` — the query.Query surface

```
type ExprProjection struct {
    refs       []Ref // the var/var.prop bindings the expression touches
    resultType Type  // the parser-computed result type; TypeUnknown when it cannot commit
}
```
`internal/query/query.go:1171-1174` — two unexported fields, no
constructor tag beyond `NewExprProjection(refs, t)`.

Accessor convention: `Refs()`, `Type()`, `isProjection()` at
`internal/query/query.go:1185-1192`. No other accessor.

Constructor:
```
func NewExprProjection(refs []Ref, t Type) ExprProjection {
    return ExprProjection{refs: refs, resultType: t}
}
```
`internal/query/query.go:1176-1181` — total function; no validation.

`MarshalJSON`:
```
func (p ExprProjection) MarshalJSON() ([]byte, error) {
    return json.Marshal(struct {
        Kind string    `json:"kind"`
        Refs []flatRef `json:"refs"`
        Type Type      `json:"type"`
    }{Kind: projectionKindExpr, Refs: flattenRefs(p.refs), Type: projectionType(p.resultType)})
}
```
`internal/query/query.go:1194-1203` — three fields on the wire, all
always-emit.

**JSON test-witness for the wire shape** —
`internal/query/type_test.go:124-131`:
```
`{"kind":"expr","refs":[{"variable":"a","property":"n"}],"type":"int"}`
```

### 3.2 Where the walk sees an aggregate today

`classifyRichExpression` is the top of the walk
(`internal/query/cypher/typing.go:866-876`); it delegates to
`typeExpressionMining` (typing.go:41-51), which walks
`OC_OrExpression` down through `typeXor / typeAnd / typeNot /
typeComparison / typeStringListNull / typeAddSub / typeMulDiv /
typePower / typeUnary / typeNonArithmetic → typeAtom`.

`typeAtom` (`internal/query/cypher/typing.go:317-407`) is the leaf.
Two of its arms fire on an aggregate:

- **`a.COUNT() != nil` at typing.go:340-345** — the `count(*)` star
  atom, which the aggregate table (`aggregateResultType(AggCount,
  nil)`) types as `TypeInt`. Direct hit — this atom is an aggregate.
- **`a.OC_FunctionInvocation() != nil` at typing.go:346-367**, with
  the inner check at typing.go:358-365:
  ```
  if name, ok := functionName(fi); ok {
      if fn, ok := aggregateFunc(name); ok {
          var operand query.Type
          if args := fi.AllOC_Expression(); len(args) > 0 {
              operand, _ = l.typeExpression(args[0])
          }
          return aggregateResultType(fn, operand)
      }
  }
  ```
  When `aggregateFunc(name)` returns `(fn, true)` — one of the eight
  `AggregateFunc` variants at `internal/query/query.go:1213-1230` —
  the atom is an aggregate.

The recursion through `typeExpression → typeOr → …` means an
aggregate at ANY depth in the expression tree lands at one of these
two arms. Both nested cases the spec must handle route through:

| Shape | Route to a typeAtom aggregate arm | Verification |
|---|---|---|
| `count(n) + 1` | `typeAddSub → typeMulDiv → typePower → typeUnary → typeNonArithmetic → typeAtom(count(n))` hits the `OC_FunctionInvocation` arm | pin `"count in arithmetic"` parser_test.go:1319-1327 |
| `1 + count(n)` | Same route, right operand hits the same arm | grammar-mirror of the above |
| `collect(x)[0]` | `typeAddSub → … → typeUnary → typeNonArithmetic` with a list-indexing op; `typeAtom(collect(x))` hits the `OC_FunctionInvocation` arm | first-party (typing.go:279-306 for the list-op wrapper; `collect` is `AggCollect` at query.go:1217) |
| `CASE WHEN true THEN count(n) ELSE 0 END` | `typeAtom(CASE)` at typing.go:372 → `typeCase` (typing.go:476-511) walks each THEN via `typeExpression`, which recurses back through `typeAtom` for the aggregate arm | typeCase itself is not an aggregate — the walk descends |
| `toFloat(count(n))` | `typeAtom(toFloat(...))` at the `OC_FunctionInvocation` arm; `aggregateFunc("toFloat")` returns false (not in the closed AggregateFunc set); the temporal/`TypeUnknown` fall-through path applies, but `mineFunctionArgs` (typing.go:348) walks the args via `typeExpression`, which recurses to `typeAtom(count(n))` — the aggregate arm | mineFunctionArgs is at internal/query/cypher/shape.go (verify at branch base); the recursion path is the same one refs mining takes today |

**Coverage claim.** Every nested-aggregate shape that today produces
`ExprProjection{refs, TypeInt/TypeUnknown/…}` reaches one of the two
aggregate arms in typeAtom at some point during its walk. A
`subtreeContainsAggregate` helper that raises a flag when either arm
fires during a walk over the `oC_Expression` subtree is exactly the
information the parser has and dropped.

### 3.3 `count(*)` inside a rich expression — the star-atom path

The `a.COUNT()` arm at typing.go:340 fires for the count-star atom
`count(*)`. Two shapes reach it:

- **`RETURN count(*) + 1`** — the typing walk descends into an
  arithmetic expression whose atom is `count(*)`. The atom arm
  returns `TypeInt`; `promoteAdd(TypeInt, TypeInt) = TypeInt`; the
  outer classifyRichExpression yields `ExprProjection{nil, TypeInt}`.
  Under the widening: the same walk detects the aggregate hit;
  emits `ExprProjection{nil, TypeInt, ContainsAggregate: true}`.
- **`RETURN count(count(*))`** — the outer aggregate arm's operand
  walk (typeAtom at 346-367) descends via `l.typeExpression(args[0])`
  into an atom that is `count(*)`. This is the pin
  `"aggregate count of count star"` (`parser_test.go:1569-1581`);
  the outer aggregate captures the whole call, so this is an
  `AggregateProjection`, not an `ExprProjection` — the widening
  does not touch it.

### 3.4 The R5 grouping-key machinery — where the resolver widening lands

`internal/resolver/resolve.go:571-602`:

```go
func fillGroupingKeys(cols []Column, part query.Part) {
    if part.ReturnsAll {
        return
    }
    hasAggregate := false
    for _, item := range part.Returns {
        if _, ok := item.Value.(query.AggregateProjection); ok {
            hasAggregate = true
            break
        }
    }
    if !hasAggregate {
        return
    }
    // Grouping applies. Non-aggregate, non-ExprProjection items are keys.
    for i, item := range part.Returns {
        switch item.Value.(type) {
        case query.RefProjection, query.LiteralProjection, query.FuncProjection:
            cols[i].GroupingKey = true
        }
        // AggregateProjection and ExprProjection remain false (§4.5.2
        // uniform-exclude).
    }
}
```

The `switch item.Value.(type)` at line 594-598 is the widening's
grouping-key site: adding an `ExprProjection` arm that consults
`item.Value.(query.ExprProjection).ContainsAggregate()` and marks
the key when false. §7.1 also widens the `hasAggregate` gate above
(lines 583-591) so an `ExprProjection` with
`ContainsAggregate() == true` flips grouping mode ON even without a
top-level `AggregateProjection` sibling — the round-2 design ruling
that motivates §7.4b's new sub-case-5 fixture. §7.1 pins the fully
widened body.

### 3.5 The deferral pin — verbatim quotation

`internal/query/cypher/parser_test.go:1308-1327`:

```go
// Stage 10 — an aggregate inside a rich expression types via the
// same table: count(n) types as TypeInt, so count(n) + 1 types as
// TypeInt via promoteAdd(TypeInt, TypeInt). The ExprProjection carries
// the aggregate's touched ref and the promoted result type.
//
// aggregate-kind-rich-exprs spec §4.5 pin #7 — the deferral lock: the
// outer expression is not an aggregate call, so per §1.3 the model does
// NOT lift the inner count(n) kind through a rich-expression wrapper.
// This pin stays GREEN both pre- and post-widening; a future change
// that silently introduces an inner-aggregate axis on ExprProjection
// breaks the pin structurally.
"count in arithmetic": {
    src: "MATCH (n)\nRETURN count(n) + 1",
    want: oneBranch(query.Part{
        Bindings: []query.Binding{must(query.NewNodeBinding("n", nil))},
        Returns: []query.ReturnItem{
            {Name: "count(n) + 1", Value: query.NewExprProjection([]query.Ref{{Variable: "n"}}, query.TypeInt{})},
        },
    }),
},
```

The comment "This pin stays GREEN both pre- and post-widening; a future
change that silently introduces an inner-aggregate axis on
`ExprProjection` breaks the pin structurally" was written under the
aggregate-kind-rich-exprs assumption that a follow-up cycle would
promote nested aggregates to `AggregateProjection` (Shape A). This
cycle chooses Shape B instead, so the pin's shape DOES change: the
`ExprProjection`'s `ContainsAggregate` axis flips from `false` (implicit
zero) to `true`. §4.3 records the exact rebaseline of both the value
and the surrounding narrative comment.

### 3.6 EdgeBinding's `directed` — the axis-precedent house style

The only precedent for an additive boolean axis with an always-emit
JSON default on a sum variant is `EdgeBinding.directed`
(added at Stage 5; recorded in ADR 0008). Its layout:

- **Field:** `directed bool` on the struct (`internal/query/query.go:378`).
- **Constructor:** trailing positional parameter — `NewEdgeBinding(
  variable, labels, source, target, directed)`
  (`internal/query/query.go:388-393`). The parameter is required, not
  variadic; there is no zero-value shorthand.
- **Accessor:** `Directed() bool` (`internal/query/query.go:445-447`).
- **JSON:** always-emit key `"directed": bool` in the marshalled
  struct (`internal/query/query.go:1497-1508`, `Directed bool
  json:"directed"`, unconditional in the composite literal). Every
  edge binding golden line prints `"directed": true` or `"directed":
  false` — no omit-when-false convention.

**How `ContainsAggregate` follows the precedent — and where it does
not.**

- **Field** — same, unexported `containsAggregate bool` on
  `ExprProjection`. §4.1.1.
- **Constructor** — DIVERGES. `directed` is required on
  `NewEdgeBinding` because Stage 5 was before Stage 14; every parser call
  site was rewritten in the same cycle. `ContainsAggregate` lands
  later, so an additive constructor overload is preferable to
  a breaking parameter addition. **The existing
  `NewExprProjection(refs, t)` constructor stays** as the
  zero-value-default shorthand; a new
  `NewExprProjectionWithAggregate(refs, t, containsAggregate)` takes
  the third parameter. Justification: the additive-only revision
  protocol (`ADR 0008 §Additions since Stage 14`) is
  zero-value-safe by design; a constructor rewrite would touch every
  parser-test pin whose ExprProjection has `containsAggregate =
  false`, breaking the byte-identity of 24 assertions for no
  semantic gain. Precedent for keeping the shorthand:
  `NewNodeBinding` / `NewNullableNodeBinding` at
  `internal/query/query.go` (Stage 6 ADR 0006 added the nullable
  axis via a parallel constructor, not a signature change).
- **Accessor** — same, `ContainsAggregate() bool`. §4.1.2.
- **JSON** — DIVERGES. Decided **omit-when-false** (`,omitempty`).
  §4.1.3 justifies against the fence-diff requirement: 2055 of 3199
  goldens embed `"kind": "expr"` blobs, and an always-emit posture
  would rebaseline all of them for zero semantic content. This
  cycle establishes the campaign convention that later
  additive axes emit **omit-when-zero-value**, deliberately not
  following the earlier `directed` precedent — a decision the
  ADR 0008 amendment note in §6 records verbatim.

### 3.7 R5's audit note — what this cycle closes

`docs/specs/resolver-stage-r5.md §4.5.3` records:

- **§4.5.3.1** — the shape of the query model at classification;
- **§4.5.3.2** — R5's uniform-exclude posture, the
  preserved-vs-violated split (grouping-key SUBSET preserved;
  result-set semantics for non-aggregate residuals violated);
- **§4.5.3.3** — the two change options (Shape A, Shape B; Shape C
  retired), with Shape B ranked first;
- **§4.5.3.5** — the target discriminating fixture
  `aggregate_with_expr_grouping_key.cypher` — `MATCH (n:Person)
  RETURN 1 + n.age, count(n)` — which R5 excludes because under the
  uniform-exclude posture it would encode the R5 gap directly.

This cycle DELIVERS the R5 §4.5.3.3 recommendation (Shape B),
adds the R5 §4.5.3.5 target fixture (§7.4), and closes the R5
§7.1.5 follow-up bead.

---

## 4. The change — parser and model changes

### 4.1 `ExprProjection` — the additive field

#### 4.1.1 Struct

```go
type ExprProjection struct {
    refs              []Ref // the var/var.prop bindings the expression touches
    resultType        Type  // the parser-computed result type; TypeUnknown when it cannot commit
    containsAggregate bool  // true iff the expression subtree contains at least one aggregate call (Shape B, ADR 0008 amendment 2026-07-06)
}
```

The field lands last in the struct — the additive-only convention
across the query package (`nullable` fields on `EdgeBinding` /
`NodeBinding` follow the same tail-append style).

#### 4.1.2 Constructor + accessor

New constructor:

```go
// NewExprProjectionWithAggregate builds an ExprProjection carrying its
// result type, touched refs, and the ContainsAggregate bit — true iff
// the expression subtree contains at least one aggregate function call
// (Shape B per ADR 0008 amendment 2026-07-06). Callers that do not
// need the bit use NewExprProjection, which forwards containsAggregate
// = false.
func NewExprProjectionWithAggregate(refs []Ref, t Type, containsAggregate bool) ExprProjection {
    return ExprProjection{refs: refs, resultType: t, containsAggregate: containsAggregate}
}
```

Preserved constructor (verbatim signature, forwards through the new one):

```go
func NewExprProjection(refs []Ref, t Type) ExprProjection {
    return NewExprProjectionWithAggregate(refs, t, false)
}
```

New accessor:

```go
// ContainsAggregate reports whether the expression subtree contains at
// least one aggregate function call. Populated parser-side during
// classifyRichExpression's walk (Shape B, ADR 0008 amendment
// 2026-07-06). The resolver's grouping-key discriminator reads this bit
// (docs/specs/resolver-stage-r5.md §4.5.3 close-out).
func (p ExprProjection) ContainsAggregate() bool { return p.containsAggregate }
```

`isProjection()`, `Refs()`, `Type()` are unchanged.

#### 4.1.3 JSON — omit-when-false, deliberately diverging from before Stage 14 precedent

The marshalled struct gains one field:

```go
func (p ExprProjection) MarshalJSON() ([]byte, error) {
    return json.Marshal(struct {
        Kind              string    `json:"kind"`
        Refs              []flatRef `json:"refs"`
        Type              Type      `json:"type"`
        ContainsAggregate bool      `json:"containsAggregate,omitempty"`
    }{
        Kind:              projectionKindExpr,
        Refs:              flattenRefs(p.refs),
        Type:              projectionType(p.resultType),
        ContainsAggregate: p.containsAggregate,
    })
}
```

**Round-1 correction (2026-07-06).** An earlier draft of this
subsection asserted "no parser JSON goldens" and ruled always-emit.
That premise was wrong: `internal/query/cypher/testdata/golden/`
holds **3199 `*.golden.json` files**, of which **2055 contain
`"kind": "expr"` blobs**, and `checkGolden`
(`internal/query/cypher/acceptance_test.go:1052`) is a byte-exact
`strings.TrimRight(string(want), "\n") != string(got)` diff. Under
an always-emit encoding the model-change PR would rebaseline
**2055 goldens** whose semantic content is unchanged — the diff
would be dominated by mechanical `"containsAggregate":false` key
insertions, not the semantic change ADR 0008's "diff shows only the
new surface" fence targets.

**Ruling: omit-when-false (`,omitempty`).** Under this encoding the
**2049** ExprProjection-bearing goldens whose top-level Returns
position embeds no aggregate call remain byte-identical, and the
rebaseline diff surfaces **exactly** the goldens whose semantic
content actually changes — i.e. those whose ExprProjection now
carries `ContainsAggregate = true`. The rebaseline IS the semantic
change; nothing else moves.

**Campaign convention — recorded here.** Later additive axes
emit **omit-when-zero-value**, deliberately diverging from the
earlier always-emit precedent (`directed`, `nullable`,
`returnsAll`, `hops` — verified verbatim at query.go:1483-1490 for
NodeBinding, :1497-1508 for EdgeBinding, :1613-1619 for the
returnsAll block). The reason for the divergence is the constraint
flip that ADR 0008 records:

- **Before Stage 14:** parser was the only consumer; model churn was
  cheap; the wire-consistency argument for always-emit dominated
  because rebaseline cost was near-zero.
- **After Stage 14:** golden rebaselines are the primary auditability
  surface for every additive change. The remaining change
  campaign axes (Use→Part attribution on `PropertyUse`/`ExprUse`/
  `ClauseSlotUse` per gqlc-fvo; per-position CALL-arg records on
  `CallBinding` per gqlc-0ig; group fields on `Binding` per gqlc-ay9
  and gqlc-5xg) sit on records present in **nearly every golden**.
  An always-emit posture across the campaign forces near-total
  rebaselines of the 3199-file corpus on each cycle. After Stage 14
  auditability is the load-bearing constraint, and it is the
  constraint the omit-when-zero-value convention protects.

**The convention in one line.** For any additive axis landing under
the ADR 0008 additions convention whose zero value is the semantic
default of the before Stage 14 corpus, the JSON encoding is
`,omitempty`; the always-emit precedent applies only to axes
already recorded at Stage 14 completion.

**Consequences for the model-change PR fence.**

- **Parser-golden rebaseline scope.** The model-change PR rebaselines
  **exactly the 20 goldens** whose top-level Returns position embeds
  an ExprProjection whose expression text contains an aggregate
  function call surviving the §4.2.1 boundary walk, enumerated in
  §4.4.1 (mined by the discovery command recorded there). Every
  other ExprProjection-bearing golden — 2029 files — stays
  byte-identical.
- **The reviewer-side fence.** Strip the `containsAggregate` key
  from all goldens and diff against the branch base. The result
  MUST be byte-identical. Recorded fence command (run from the
  worktree at the model-change PR's branch tip, against
  `origin/master @ e77f33e`):

  ```
  python3 - <<'PY'
  import glob, json
  def go_dump(data, f):
      # Go's encoding/json HTML-escapes <, >, & inside strings;
      # a plain json.dump re-writes them literally and
      # false-positives every golden carrying one (243 files).
      s = json.dumps(data, indent=2, ensure_ascii=False)
      s = (s.replace('&', '\\u0026')
            .replace('<', '\\u003c')
            .replace('>', '\\u003e'))
      f.write(s); f.write('\n')
  for path in sorted(glob.glob(
          'internal/query/cypher/testdata/golden/*.golden.json')):
      with open(path) as f: data = json.load(f)
      def strip(n):
          if isinstance(n, dict):
              n.pop('containsAggregate', None)
              for v in n.values(): strip(v)
          elif isinstance(n, list):
              for v in n: strip(v)
      strip(data)
      with open(path, 'w') as f:
          go_dump(data, f)
  PY
  git diff --stat origin/master -- \
      internal/query/cypher/testdata/golden/*.golden.json
  # MUST print: no changes.
  git checkout -- internal/query/cypher/testdata/golden/
  ```

  If the stat print is non-empty, some non-aggregate ExprProjection
  golden regenerated — the encoding leaked a `containsAggregate:
  true` where the semantic default should hold, or a spurious
  formatting change slipped in. The model-change PR is buggy.

- **`TestExprProjectionMarshalJSON` at `internal/query/type_test.go:
  124-131` stays VERBATIM.** The test's blob has
  `containsAggregate = false`; under omit-when-false the key is
  absent, matching the current pinned string
  `{"kind":"expr","refs":[…],"type":"int"}` bit-for-bit. §5's new
  test `TestNewExprProjectionWithAggregateTrue` is the sole witness
  for the true side of the axis's wire encoding.

- **`ValidatedQuery` unaffected.** No resolver-side golden marshals
  `query.Query`; verified by `grep -l '"kind":"expr"' test/data/
  resolver/valid/*.json` yielding zero matches at branch base. §7.5
  covers the resolver byte-identity fence separately.

### 4.2 `classifyRichExpression` — the walk that already has the answer

Current (`internal/query/cypher/typing.go:866-876`):

```go
func (l *listener) classifyRichExpression(e gen.IOC_ExpressionContext) query.Projection {
    t, refs, params := l.typeExpressionMining(e)
    for _, p := range params {
        name := parameterName(p)
        if name == "" {
            continue
        }
        l.addParameterUse(name, p, query.NewExprUse(t, query.ExprInProjection))
    }
    return query.NewExprProjection(refs, t)
}
```

Widened:

```go
func (l *listener) classifyRichExpression(e gen.IOC_ExpressionContext) query.Projection {
    t, refs, params := l.typeExpressionMining(e)
    for _, p := range params {
        name := parameterName(p)
        if name == "" {
            continue
        }
        l.addParameterUse(name, p, query.NewExprUse(t, query.ExprInProjection))
    }
    return query.NewExprProjectionWithAggregate(refs, t, subtreeContainsAggregate(e))
}
```

New helper `subtreeContainsAggregate` — a boolean scan of the
`oC_Expression` subtree for the two aggregate arms typeAtom
recognises (§3.2). One implementation shape:

```go
// subtreeContainsAggregate reports whether the expression subtree
// contains at least one aggregate function call (either the count(*)
// star atom or a name in the closed AggregateFunc set — mirror of
// typeAtom's typing.go:340 and typing.go:358-365 arms). Used by
// classifyRichExpression to seed ExprProjection.ContainsAggregate
// per ADR 0008 amendment 2026-07-06.
func subtreeContainsAggregate(e gen.IOC_ExpressionContext) bool {
    found := false
    antlr.NewParseTreeWalker().Walk(&aggregateProbe{onHit: func() { found = true }}, e)
    return found
}
```

where `aggregateProbe` is a listener overriding `EnterOC_Atom` (or
the narrower entry points ANTLR generates for the count-star and
function-invocation atoms) and calling `onHit` when it observes:

- an `OC_Atom` with `atom.COUNT() != nil` (star atom), or
- an `OC_FunctionInvocation` whose `functionName(fi)` returns a name
  and `aggregateFunc(name)` returns `(_, true)`.

**Justification for a walk-based helper rather than reading a flag
mined during `typeExpressionMining`.**

Threading a flag through the existing walk would require touching
`typeOr → typeXor → typeAnd → typeNot → typeComparison →
typeStringListNull → typeAddSub → typeMulDiv → typePower → typeUnary
→ typeNonArithmetic → typeAtom`: eleven functions, seven pointer
receivers, forty-plus lines of diff. A separate walk is a smaller
diff (one helper, ≤ 30 lines total), reads the SAME grammar the
existing walk reads, and cannot drift from the existing walk's
aggregate-arm semantics because it dispatches on the same two
predicates (`atom.COUNT()`, `aggregateFunc(name)`). The performance
delta is a single second pass over the expression subtree — for
projection walks this is proportional to the projection's grammar
depth, which is bounded by the length of the return item.

**Alternative shape.** A one-line change threading a `*bool` pointer
through `typeExpressionMining` and each recursive callee would work
but violates the "additive-only" reading of the additions convention
in spirit — it modifies the internal signature of every typing
helper. The walk-based helper leaves the existing walk untouched
and localises the change to two files.

#### 4.2.1 Boundary posture — the probe descends exactly where the typing walk descends

`subtreeContainsAggregate` MUST mirror the typing walk's scope
boundaries. The general principle: **the probe descends exactly
where the typing walk descends** — every arm where the typing walk
either refuses to descend or discards mined refs is a boundary the
probe MUST also honour.

**The full arm-by-arm sweep** across `typeNonArithmetic` /
`typeAtom` / `typeQuantifier`, cited at branch base
`origin/master @ e77f33e`:

| # | Grammar node | Typing walk (typing.go:line) | Refs recorded? | Probe descends? |
|---|---|---|---|---|
| 1 | `OC_Atom` — variable | `typing.go:322-333` | yes (outer) | **yes** — no sub-scope |
| 2 | `OC_Atom` — count-star `count(*)` | `typing.go:340` | no ref | **yes — hit reported** (aggregate arm #1) |
| 3 | `OC_Atom` — scalar literal | `typing.go:335-338` | no ref | **yes** — no sub-scope, no aggregate possible |
| 4 | `OC_Atom` — parameter | `typing.go:347-355` | no ref | **yes** — no sub-scope |
| 5 | `OC_Atom` — function invocation | `typing.go:358-365` (aggregateFunc check) | outer (args) | **yes — hit reported for aggregate names** (aggregate arm #2), otherwise descend into args |
| 6 | `OC_Atom` — parenthesised expression | `typing.go:367-370` | outer | **yes** — pure precedence node, no sub-scope |
| 7 | `OC_Atom` — CASE | `typeCase` at `typing.go:475+` | outer | **yes** — subject / WHEN / THEN / ELSE are all outer-scope |
| 8 | `OC_Atom` — list literal `[e1, e2, …]` | `typing.go` (list literal walk) | outer | **yes** — element expressions are outer-scope |
| 9 | `OC_Atom` — map literal `{k: e1, …}` | (map literal walk) | outer | **yes** — value expressions are outer-scope |
| 10 | `OC_Atom` — quantifier `all/any/none/single(x IN src WHERE pred)` | `typeQuantifier` at `typing.go:409-462` | source list: **outer** (`typing.go:437`); WHERE filter body: **discarded** (`savedOuter/restore` at `typing.go:449-452`) | **partial** — descend into source list, **STOP at the WHERE-body OC_Where** |
| 11 | `OC_Atom` — existential subquery `EXISTS { … }` | `typing.go:382-388` returns `TypeBool` without descending | none | **NO — full stop at `OC_ExistentialSubquery`** |
| 12 | `OC_Atom` — pattern predicate | `typing.go:389-397` returns `TypeBool` without descending; the pattern-atom refs are runtime-scope | none | **NO — full stop at `OC_PatternPredicate`** |
| 13 | `OC_Atom` — list comprehension `[x IN src \| body]` or `[x IN src WHERE pred]` | `typing.go:398-403` returns `TypeUnknown` without descending | none | **NO — full stop at `OC_ListComprehension`** |
| 14 | `OC_Atom` — pattern comprehension | `typing.go:398-403` (same arm as list comprehension) | none | **NO — full stop at `OC_PatternComprehension`** |
| 15 | Arithmetic / precedence towers (add/sub/mul/div/power/unary/comparison/string-list-null/and/or/xor/not) | `typing.go:210-315` (typing sub-walk) | outer at every level | **yes** — pure precedence, no sub-scope |

**The five hard stops** (rows 10 partial + 11 + 12 + 13 + 14):

- **10 — `OC_Quantifier` WHERE-body.** For `all(x IN xs WHERE p) /
  any / none / single`, the source list is outer-scope, but the
  filter body's refs are discarded to enforce iteration-variable
  scoping (`typing.go:449-452`: `savedOuter := l.curPart.refs; ...
  l.curPart.refs = savedOuter`). The probe MUST descend into the
  quantifier's source list (`x IN <src>`) but MUST NOT descend
  into the `OC_Where` sub-node of the `OC_FilterExpression`. This
  matches the typing walk exactly: an aggregate over the source
  list aggregates over the outer Part's rows (`all(x IN
  collect(n) WHERE …)` would flip the outer projection), while an
  aggregate inside the WHERE body aggregates over the iteration
  scope and is not the outer Part's concern.
- **11 — `OC_ExistentialSubquery`.** `EXISTS { RETURN count(n) }`
  aggregates over the subquery's rows; the outer projection is a
  boolean. The typing walk returns `TypeBool` without descending
  (`typing.go:382-388`) and parameters are mined at
  `EnterOC_ExistentialSubquery` via the `subqueryDepth` counter,
  never entering the outer part's state.
- **12 — `OC_PatternPredicate`.** `(a)-->(b)` at predicate
  position is boolean; the inner pattern's refs are runtime-scope
  (Stage 11 §1.3). At projection position the atom is rejected
  earlier via `ErrPatternInProjection`
  (`collectReturnItem`, `expr.go:196-199`), so this arm never
  reaches `classifyRichExpression` in practice — but the probe
  still declares the stop explicitly for symmetry with the typing
  walk.
- **13 — `OC_ListComprehension`.** `[x IN xs | e]` /
  `[x IN xs WHERE p]` / `[x IN xs WHERE p | e]` — the typing walk
  at `typing.go:398-403` returns `TypeUnknown` without walking the
  sub-tree, so BOTH the source list `xs` AND the body `e` /
  predicate `p` are opaque to the outer walk. Aggregates ANYWHERE
  inside a list comprehension do not reach the outer Part; the
  probe stops fully at `OC_ListComprehension`. The wire-observable
  witness: `Return6` scenario `[3] Size of list comprehension`
  (`RETURN size([x IN collect(r) WHERE x <> null]) AS cn`)
  produces an `ExprProjection` with `ContainsAggregate = false` at
  branch base — the `collect(r)` inside the comprehension does
  not flip the outer bit, matching the typing walk's opacity.
- **14 — `OC_PatternComprehension`.** Same site as
  `OC_ListComprehension`, same stop.

**Implementation directive.** `aggregateProbe` MUST override
`EnterOC_Quantifier` to walk only the source list sub-node and
suppress recursion into the filter body's `OC_Where`; MUST override
`EnterOC_ExistentialSubquery`, `EnterOC_ListComprehension`,
`EnterOC_PatternComprehension`, and `EnterOC_PatternPredicate` to
return WITHOUT recursing into the sub-tree's children. ANTLR's
`ParseTreeWalker` does not natively expose a "skip children" flag;
the standard workaround is to maintain a `skipDepth int` counter on
the probe struct: incremented at the stop's `Enter*`, decremented
at the corresponding `Exit*`, and consulted at every `Enter*` to
early-return when non-zero. Alternatively, drive the walk with a
manual pre-order recursion over `antlr.Tree`-typed children and
skip the specific `IOC_*Context` types at the visit site — a
smaller implementation than the counter for a five-stop probe.

**Semantic justification.** `ExprProjection.ContainsAggregate`
answers exactly one question — "does the resolver's
`fillGroupingKeys` exclude this projection from the grouping key
set of the enclosing Part?" — and only aggregates that aggregate
over the enclosing Part's rows can motivate that exclusion. An
aggregate inside a sub-scope aggregates over the sub-scope, so
poisoning the outer projection's grouping-key eligibility on that
basis is a soundness error. The boundary is not an optimisation;
it is a correctness requirement.

**Boundary pins — plain statement.** No parser-test pin at branch
base exercises any of the five stops (the vendored TCK's
comprehension / quantifier / EXISTS scenarios that co-occur with an
outer projection use OC_Atom shapes that never reach
`classifyRichExpression`, or the outer projection is a bare
aggregate at grammar-valid position). §5 adds no pin for the stops
either — the walker is a private helper, and its boundaries are
witnessed indirectly by the §4.4.1 golden enumeration: **every
List12 / List13 golden with an inner-comprehension aggregate stays
byte-identical** (source-verified at §4.4.1; Return6 `[3] Size of
list comprehension`'s golden `List12_33d76b6f508c.golden.json`
shows `"kind": "expr"` with no `containsAggregate` key at branch
base and post-widening — the first-party audit that proves the
stop). The row-10 partial case (quantifier source-list) is NOT
inside this boundary set — see §4.4.1's "Not a boundary case:
quantifier source-list" paragraph, which pins `List11_f7c0a30b582c`
as a flip (row 20). If a future TCK bump adds
a scenario whose outer projection embeds an `EXISTS { RETURN
count(…) }` or `all(x IN collect(…) WHERE …)` at RETURN /
WITH-projection position, the pin's `want` MUST author
`ContainsAggregate = false` on the outer ExprProjection for the
comprehension / EXISTS cases (stops 11/13/14) and
`ContainsAggregate = true` for the quantifier-source-list case
(stop 10 partial).

### 4.3 The deferral pin — flip its bit

`internal/query/cypher/parser_test.go:1308-1327` — verbatim
before-state per §3.5. After the change:

```go
// Stage 10 — an aggregate inside a rich expression types via the
// same table: count(n) types as TypeInt, so count(n) + 1 types as
// TypeInt via promoteAdd(TypeInt, TypeInt). The ExprProjection carries
// the aggregate's touched ref, the promoted result type, and the
// ContainsAggregate=true bit (Shape B per ADR 0008 amendment
// 2026-07-06).
//
// aggregate-kind-rich-exprs spec §4.5 pin #7 — closed. The outer
// expression is not an aggregate call, so per §1.3 the model still
// does NOT lift the inner count(n) kind as an AggregateProjection.
// Instead the parser sets ContainsAggregate=true so the resolver's
// grouping-key discriminator (fillGroupingKeys, R5 §4.5.3) can
// exclude the residual honestly.
"count in arithmetic": {
    src: "MATCH (n)\nRETURN count(n) + 1",
    want: oneBranch(query.Part{
        Bindings: []query.Binding{must(query.NewNodeBinding("n", nil))},
        Returns: []query.ReturnItem{
            {Name: "count(n) + 1", Value: query.NewExprProjectionWithAggregate([]query.Ref{{Variable: "n"}}, query.TypeInt{}, true)},
        },
    }),
},
```

The narrative comment updates in three places:

1. First paragraph — adds the `ContainsAggregate=true` claim.
2. Second paragraph (the "deferral lock") — flips from "This pin
   stays GREEN both pre- and post-widening" to "closed" language,
   because the pin's shape DOES change under this cycle's Shape B.
3. The "a future change that silently introduces an inner-aggregate
   axis on ExprProjection breaks the pin structurally" clause is
   retracted; the axis is no longer silent — it is the axis this
   cycle adds, and the pin is its most direct witness.

**One-line summary of the pin change:**
`NewExprProjection([]query.Ref{{Variable: "n"}}, query.TypeInt{})` →
`NewExprProjectionWithAggregate([]query.Ref{{Variable: "n"}}, query.TypeInt{}, true)`.

### 4.4 Every other parser-test ExprProjection pin — byte-identical

Enumeration of every parser-test pin whose `want` embeds
`NewExprProjection`, drawn from
`grep -n "NewExprProjection" internal/query/cypher/parser_test.go`
at branch base `e77f33e`:

| Line | Pin name | Expression | Aggregate present? | ContainsAggregate |
|---|---|---|---|---|
| 484 | (arithmetic literal-only projection) | `12 / 4 * 3 - 2 * 4` | no | false |
| 498 | (rich IS NULL) | `n.missing IS NULL` | no | false |
| 515 | (arithmetic with property) | `n.num + 1` | no | false |
| 531 | (unary minus) | `-n.num` | no | false |
| 544 | (list literal) | `[1, 2, 3]` | no | false |
| 559 | (bare parameter) | `$x` | no | false |
| 581 | (arithmetic with parameter) | `a.n + $delta` | no | false |
| 627 | (list literal with parameter) | `[1, $x, 3]` | no | false |
| 648 | (CASE literal arms) | `CASE WHEN true THEN 'a' ELSE 'b' END` | no | false |
| 706 | (temporal date) | `d` (via WITH) | no | false |
| 716 | (temporal duration) | `d` | no | false |
| 730 | (temporal unknown) | `y` | no | false |
| 753 | (temporal date, WITH-carried) | `d` | no | false |
| 763 | (temporal duration, WITH-carried) | `d` | no | false |
| 774 | (temporal duration, WITH-carried) | `d` | no | false |
| 787 | (temporal unknown, WITH-carried) | `d` | no | false |
| 798 | (temporal unknown, WITH-carried) | `d` | no | false |
| 1111 | (list-of-list literal) | `lol` (via nested list literal) | no | false |
| **1324** | **`"count in arithmetic"` — the deferral pin** | **`count(n) + 1`** | **YES** | **true** |
| 1592-1594 | (ALL quantifier trio) | `all(x IN [] WHERE …)` × 3 | no | false |
| 1605-1607 | (NONE quantifier trio) | `none(x IN [] WHERE …)` × 3 | no | false |

**24 assertions** carry non-aggregate expressions; **one** carries the
`count(n) + 1` aggregate.

Under the additive-only construction (§4.1.2), the 24 non-aggregate
assertions continue to use `NewExprProjection(refs, t)`, which
forwards `containsAggregate = false`. Go struct equality
(`reflect.DeepEqual` at `require.Equal`) matches
`ExprProjection{refs, t, false}` bit-for-bit — no rebaseline needed.

**The fence check for parser tests** (from the worktree):

```
go test -run 'TestMustParse|TestMustReject|TestExprProjectionMarshalJSON|TestNewExprProjection' ./internal/query/... -shuffle=on
```

The 24 unchanged pins pass without modification. Only the one flipped
pin (§4.3) plus the new `TestNewExprProjectionWithAggregateTrue`
(§5) show a diff. `TestExprProjectionMarshalJSON` stays verbatim per
the omit-when-false ruling (§4.1.3).

#### 4.4.1 Golden-corpus rebaseline — the 20 flip fixtures (scenario-source discovery)

Under the omit-when-false JSON encoding (§4.1.3), the
`containsAggregate` key is emitted **only** when the ExprProjection's
walk returns `true`. The `internal/query/cypher/testdata/golden/`
corpus at branch base `origin/master @ e77f33e` contains **2055**
goldens that embed a `"kind": "expr"` blob anywhere; of those,
**2049** hold an ExprProjection at top-level Returns (§4.1.3's
scope), and exactly **20** of those top-level cases are flipped by
the widened walker (§4.2 + §4.2.1 boundary posture) once
cross-verified against the scenario source. The **2029** remaining
top-level ExprProjection-bearing goldens carry the zero value and
stay byte-identical.

**Round-1 correction (why not 7).** An earlier draft here
enumerated 7 flip goldens by regex-matching the ReturnItem `name`
field for an aggregate call. That undercounts by design:
`ReturnItem.Name` records the AS-alias when present
(`expr.go:204-207` — `if alias := item.OC_Variable(); alias != nil
{ name = alias.GetText() }`), so aliased embedded aggregates are
invisible to a name-regex. First-party corroboration of goldens
that carry `"kind": "expr"` but were missed by the round-1
regex: `With4_6a5eec4aec12.golden.json` (`head(collect(...)) AS
latestLike`), `With6_361998ddbe36.golden.json` +
`With6_997ef885e794.golden.json` (`me.age + count(you.age) AS
agg`), `With6_4540faf7c149.golden.json` (`$age + avg(person.age) -
1000 AS agg`). The name-based scanner misses each of these — the
alias `latestLike` / `agg` contains no aggregate function name.

**Discovery method — scenario-source cross-check.** The correct
discovery mines the TCK feature files (the ORIGINAL projection
text, before AS-aliasing), computes each scenario's golden filename
via the exact hash recipe used by `checkGolden` at
`internal/query/cypher/acceptance_test.go:1063-1068`
(`base + "_" + hex(sha1(uri + "\x00" + name + "\x00" + query)[:6])`,
where `uri` is the feature file's path relative to
`internal/query/cypher/` — `../../../test/data/query/cypher/tck/
features/...`), and matches back to existing goldens whose top-
level Returns position embeds an `ExprProjection` whose SOURCE
expression (source text, not alias) contains an aggregate call
surviving the §4.2.1 boundary redaction (comprehensions, EXISTS,
pattern predicates, and quantifier WHERE-bodies redacted;
quantifier source-lists preserved).

**Complete discovery script** (Python 3; run from the worktree
root; project host lacks `jq`):

```
python3 - <<'PY'
import re, os, glob, hashlib, json

CYPHER_DIR = "internal/query/cypher"
FROOT     = "test/data/query/cypher/tck/features"
GROOT     = f"{CYPHER_DIR}/testdata/golden"
AGGS      = r"(count|sum|collect|min|max|avg|stDev|stDevP|percentileCont|percentileDisc|percentile)"
call = re.compile(rf"\b{AGGS}\s*\(", re.IGNORECASE)

def uri_form(p):
    return os.path.relpath(p, CYPHER_DIR)

def hash_golden(uri, name, q):
    s = hashlib.sha1((uri + "\x00" + name + "\x00" + q).encode()).digest()
    base = os.path.splitext(os.path.basename(uri))[0]
    return f"{base}_{s[:6].hex()}.golden.json"

def scan_feature(path):
    with open(path) as f: text = f.read()
    out = []
    parts = re.split(r"(?m)^\s*(Scenario(?: Outline)?:\s*.+)$", text)
    for i in range(1, len(parts), 2):
        header, body = parts[i], parts[i+1] if i+1 < len(parts) else ""
        name = header.split(":", 1)[1].strip()
        is_outline = header.startswith("Scenario Outline:")
        examples = []
        if is_outline:
            em = re.search(r"(?ms)^\s*Examples:\s*\n((?:\s*\|.*\n?)+)", body)
            if em:
                lines = [l.strip() for l in em.group(1).strip().splitlines()
                         if l.strip()]
                if lines:
                    hdr = [c.strip() for c in lines[0].strip("|").split("|")]
                    for row in lines[1:]:
                        cells = [c.strip() for c in row.strip("|").split("|")]
                        examples.append(dict(zip(hdr, cells)))
                    body = body[:em.start()]
        ds = []
        for m in re.finditer(r'"""\s*\n(.*?)\n\s*"""', body, re.DOTALL):
            q = m.group(1)
            lines = q.splitlines()
            if lines:
                nb = [l for l in lines if l.strip()]
                if nb:
                    ind = min(len(l) - len(l.lstrip()) for l in nb)
                    q = "\n".join(l[ind:] if len(l) >= ind else l
                                  for l in lines)
            ds.append(q)
        if not is_outline:
            for q in ds: out.append((name, q))
        else:
            for row in examples:
                for q in ds:
                    qs = q
                    for k, v in row.items():
                        qs = qs.replace(f"<{k}>", v)
                    out.append((name, qs))
    return out

# Build golden -> (uri, scenario name, query text) index.
scenario_index = {}
for feat in sorted(glob.glob(f"{FROOT}/**/*.feature", recursive=True)):
    uri = uri_form(feat)
    for name, q in scan_feature(feat):
        scenario_index[hash_golden(uri, name, q)] = (uri, name, q)

# Redact §4.2.1 boundary regions: EXISTS { ... }, list/pattern
# comprehensions [ ... IN ... ], quantifiers all/any/none/single(...).
def redact(expr):
    e = re.sub(r"(?is)\bEXISTS\s*\{[^{}]*\}", " ", expr)
    changed = True
    while changed:
        changed = False
        i = 0
        while i < len(e):
            if e[i] == "[":
                d = 1; j = i + 1
                while j < len(e) and d > 0:
                    if e[j] == "[": d += 1
                    elif e[j] == "]": d -= 1
                    j += 1
                inner = e[i+1:j-1]
                d2 = 0; is_comp = False
                for k in range(len(inner)):
                    ic = inner[k]
                    if ic in "([{": d2 += 1
                    elif ic in ")]}": d2 -= 1
                    if (d2 == 0 and inner[k:k+4].upper() == " IN "
                            and re.match(r"\w+\s*$", inner[:k])):
                        is_comp = True; break
                if is_comp:
                    e = e[:i] + " " + e[j:]
                    changed = True; break
                i = j
            else:
                i += 1
    # Quantifiers all/any/none/single(x IN src [WHERE pred]) — §4.2.1 row
    # 10 partial stop: keep the source list (walker descends into it via
    # typing.go:437-438), blank only the WHERE-body (walker discards it
    # via the savedOuter/restore idiom at typing.go:449-452). Hand-rolled
    # balanced-paren walker rather than a regex — round-4: List11's actual
    # depth-3 form `ALL(ok IN collect((size(list) = 0) = empty) WHERE ok)`
    # over-runs a fixed-depth regex, so we mirror the bracket walker above.
    qpat = re.compile(r"(?i)\b(all|any|none|single)\s*\(")
    changed = True
    while changed:
        changed = False
        m = qpat.search(e)
        while m:
            i = m.end() - 1                 # position of the opening '('
            d = 1; j = i + 1
            while j < len(e) and d > 0:
                if e[j] == "(": d += 1
                elif e[j] == ")": d -= 1
                j += 1
            if d != 0:                       # unbalanced — bail on this hit
                m = qpat.search(e, j)
                continue
            body = e[i+1:j-1]
            # Depth-0 " WHERE " scan.
            d2 = 0; wpos = -1; k = 0
            while k < len(body):
                c = body[k]
                if c in "([{": d2 += 1
                elif c in ")]}": d2 -= 1
                if d2 == 0 and re.match(r"\bWHERE\b", body[k:], re.IGNORECASE):
                    wpos = k; break
                k += 1
            if wpos >= 0:
                new_body = body[:wpos]
                e = e[:i+1] + new_body + e[j-1:]
                changed = True
                break                        # restart from top of outer loop
            m = qpat.search(e, j)
    return e

def outer_is_bare_aggregate(expr):
    e = re.sub(r"^\s*DISTINCT\s+", "", expr, flags=re.IGNORECASE)
    m = re.match(rf"^{AGGS}\s*\(", e, re.IGNORECASE)
    if not m: return False
    d = 0; i = m.end() - 1
    while i < len(e):
        c = e[i]
        if c == "(": d += 1
        elif c == ")":
            d -= 1
            if d == 0: break
        i += 1
    return e[i+1:].strip() == ""

def has_embedded_agg(expr):
    reduced = redact(expr)
    if not re.search(rf"\b{AGGS}\s*\(", reduced, re.IGNORECASE):
        return False
    return not outer_is_bare_aggregate(expr)

def split_at_top_commas(s):
    parts, d, buf = [], 0, ""
    for c in s:
        if c in "([{": d += 1
        elif c in ")]}": d -= 1
        if c == "," and d == 0:
            parts.append(buf); buf = ""
        else: buf += c
    parts.append(buf)
    return [p.strip() for p in parts if p.strip()]

def alias_split(item):
    m = re.search(r"^(.*?)\s+AS\s+(\w+)\s*$", item,
                  re.IGNORECASE | re.DOTALL)
    if m: return m.group(1).strip(), m.group(2).strip()
    return item.strip(), item.strip()

def clause_projections(q):
    # Terminator lookahead includes DETACH so that a `DETACH DELETE …`
    # line after a projection ends the RETURN/WITH body — the DELETE
    # keyword is mid-line, and the newline-anchored lookahead needs the
    # leading DETACH to stop the greedy body match (Delete5's
    # `{key: collect(u)}` projection was missed by an earlier draft
    # that omitted DETACH here).
    for m in re.finditer(
        r"\b(RETURN|WITH)\b(.+?)(?=\n\s*(?:ORDER|SKIP|LIMIT|WHERE|"
        r"MATCH|WITH|RETURN|UNWIND|MERGE|CREATE|DETACH|DELETE|SET|REMOVE|"
        r"CALL|UNION)\b|$)", q, re.IGNORECASE | re.DOTALL):
        clause = re.sub(r"^\s*DISTINCT\s+", "", m.group(2),
                        flags=re.IGNORECASE)
        for item in split_at_top_commas(clause):
            yield alias_split(item)

def expr_returnitem_names(data):
    names = []
    def walk(n):
        if isinstance(n, dict):
            if isinstance(n.get("returns"), list):
                for ri in n["returns"]:
                    if (isinstance(ri, dict)
                        and isinstance(ri.get("value"), dict)
                        and ri["value"].get("kind") == "expr"):
                        names.append(ri.get("name", ""))
            for v in n.values(): walk(v)
        elif isinstance(n, list):
            for v in n: walk(v)
    walk(data)
    return names

flip = []
for path in sorted(glob.glob(f"{GROOT}/*.golden.json")):
    with open(path) as f: data = json.load(f)
    names = expr_returnitem_names(data)
    if not names: continue
    g = os.path.basename(path)
    entry = scenario_index.get(g)
    if entry is None: continue
    _, _, q = entry
    projs = list(clause_projections(q))
    for ename in names:
        for src, alias in projs:
            if alias == ename or src == ename:
                if has_embedded_agg(src):
                    flip.append((g, src))
                break

flip.sort()
print(f"flip goldens: {len(flip)}")
for g, src in flip:
    print(f"  {g}  |  {src[:80]}")
PY
```

**The 20 flip goldens** (verbatim command output at branch base
`origin/master @ e77f33e`):

| Golden | Aggregate-embedding SOURCE expression |
|---|---|
| `Delete5_2fdf511bf8c3.golden.json` | `{key: {key: collect(r)}}` |
| `Delete5_31014ff88e69.golden.json` | `{key: collect(p)}` |
| `Delete5_656387555487.golden.json` | `{key: collect(u)}` |
| `List11_f7c0a30b582c.golden.json` | `ALL(ok IN collect((size(list) = 0) = empty) WHERE ok)` |
| `Return2_0d955bc4a162.golden.json` | `count(a) > 0` |
| `Return4_643233009cfd.golden.json` | `{name: count(b)}` |
| `Return4_7465ea25655a.golden.json` | `head(collect({likeTime: likeTime}))` |
| `Return6_1544b3f065a8.golden.json` | `size(collect(a))` |
| `Return6_1620cd819bff.golden.json` | `$age + avg(person.age) - 1000` |
| `Return6_29fccd6d88dd.golden.json` | `me.age + count(you.age)` |
| `Return6_3645a3cd4799.golden.json` | `count(*) * 10` |
| `Return6_830281f0127e.golden.json` | `age + count(you.age)` |
| `Return6_c27310dae8c0.golden.json` | `count(a) + 3` |
| `Return6_d980d0acf9b2.golden.json` | `{foo: a.name='Andres', kids: collect(child.name)}` |
| `Return6_fd6e60a49215.golden.json` | `count(n) / 60 / 60` |
| `ReturnOrderBy2_418ea26a331b.golden.json` | `count(a) * 10 + count(b) * 5` |
| `With4_6a5eec4aec12.golden.json` | `head(collect({likeTime: likeTime}))` |
| `With6_361998ddbe36.golden.json` | `age + count(you.age)` |
| `With6_4540faf7c149.golden.json` | `$age + avg(person.age) - 1000` |
| `With6_997ef885e794.golden.json` | `me.age + count(you.age)` |

Under the widened `classifyRichExpression` (§4.2) each of these
20 ExprProjection blobs picks up `"containsAggregate": true`;
under omit-when-false that key is emitted, and the golden is
regenerated. Every other top-level ExprProjection-bearing golden —
the remaining **2029** files — stays byte-identical because the
walker returns `false` and the omitempty tag suppresses the key.

**Boundary-preserved witnesses** (goldens that carry an
ExprProjection whose SOURCE text contains an aggregate call but
whose aggregate lives entirely inside a §4.2.1 boundary — they
stay byte-identical, mirroring the typing walk's opacity): the
List12 / List13 scenarios that project `size([x IN collect(r)
WHERE …])` / `[x IN xs | count(x)]` / similar — a list or pattern
comprehension is a full stop at row 13, so any aggregate inside
its brackets is invisible to the walker regardless of whether it
sits in the source list or in the WHERE-body / projection body.
The first-party audit witness at branch base:
`List12_33d76b6f508c.golden.json` (`RETURN size([x IN collect(r)
WHERE x <> null]) AS cn`) shows `"kind": "expr"` with no
`containsAggregate` key both before and after — proving the
stop from §4.2.1 row 13 is load-bearing, not decorative.

**Not a boundary case: quantifier source-list** — a quantifier
`all(x IN src WHERE p)` is row 10 partial: the walker DOES descend
into `src` (`typing.go:437-438`) and DOES stop at the WHERE-body
(`savedOuter/restore` at `typing.go:449-452`). Consequently
`List11_f7c0a30b582c` (`RETURN ALL(ok IN collect((size(list) = 0)
= empty) WHERE ok) AS okay` — the `collect(...)` sits
in the ALL source list, not in its WHERE-body) is a flip
(row 20 of the table), NOT a boundary witness. A quantifier
scenario whose aggregate lived only inside the WHERE-body (e.g.
`all(x IN xs WHERE count(x) > 0)` — no such scenario in the
current TCK read-core corpus) WOULD be a boundary witness.

**Bidirectional fence** (per round-2 ruling: reviewer must both
detect overreach AND detect misses):

*Fence 1 — strip-key overreach detector* (the widened marshaller
must not leak `containsAggregate: true` outside the 20-golden
set):

```
python3 - <<'PY'
import glob, json
def go_dump(data, f):
    # Go's encoding/json HTML-escapes <, >, & inside strings;
    # a plain json.dump re-writes them literally and
    # false-positives every golden carrying one (243 files).
    s = json.dumps(data, indent=2, ensure_ascii=False)
    s = (s.replace("&", "\\u0026")
          .replace("<", "\\u003c")
          .replace(">", "\\u003e"))
    f.write(s); f.write("\n")
for path in sorted(glob.glob(
        "internal/query/cypher/testdata/golden/*.golden.json")):
    with open(path) as f: data = json.load(f)
    def strip(n):
        if isinstance(n, dict):
            n.pop("containsAggregate", None)
            for v in n.values(): strip(v)
        elif isinstance(n, list):
            for v in n: strip(v)
    strip(data)
    with open(path, "w") as f:
        go_dump(data, f)
PY
git diff --stat origin/master -- \
    internal/query/cypher/testdata/golden/*.golden.json
# MUST print: no changes.
git checkout -- internal/query/cypher/testdata/golden/
```

*Fence 2 — set-equality check* (the set of changed goldens must
EQUAL the enumeration above — no extras, no misses):

```
git diff --name-only origin/master -- \
    internal/query/cypher/testdata/golden/*.golden.json | \
    sed 's|.*/||' | sort > /tmp/changed.txt
cat > /tmp/expected.txt <<'EOF'
Delete5_2fdf511bf8c3.golden.json
Delete5_31014ff88e69.golden.json
Delete5_656387555487.golden.json
List11_f7c0a30b582c.golden.json
Return2_0d955bc4a162.golden.json
Return4_643233009cfd.golden.json
Return4_7465ea25655a.golden.json
Return6_1544b3f065a8.golden.json
Return6_1620cd819bff.golden.json
Return6_29fccd6d88dd.golden.json
Return6_3645a3cd4799.golden.json
Return6_830281f0127e.golden.json
Return6_c27310dae8c0.golden.json
Return6_d980d0acf9b2.golden.json
Return6_fd6e60a49215.golden.json
ReturnOrderBy2_418ea26a331b.golden.json
With4_6a5eec4aec12.golden.json
With6_361998ddbe36.golden.json
With6_4540faf7c149.golden.json
With6_997ef885e794.golden.json
EOF
diff /tmp/expected.txt /tmp/changed.txt && echo "SET-EQUAL: PASS"
# MUST print: SET-EQUAL: PASS.
```

If Fence 1 fails (stat is non-empty), the widened marshaller
leaked `containsAggregate: true` into a golden whose semantic
content is unchanged after key-stripping — a walker bug (likely a
missed §4.2.1 boundary stop) or a formatting drift in the
marshaller. If Fence 2 fails (extra files, missing files, or a
non-empty diff), either the walker over-flipped (extras: reviewer
inspects each extra to determine whether the discovery script was
under-approximating or the walker over-fired) or under-flipped
(misses: reviewer inspects each missing golden — likely a walker
boundary that should NOT have stopped, or a discovery-script false
positive). The model-change PR must pass BOTH fences before landing.

### 4.5 The parser test suite's aggregate coverage — no other pin changes

Beyond the deferral pin at line 1324, the parser test suite carries
aggregate assertions at lines 1300 (Stage 10 min), 1444
(sum(DISTINCT n.age + 1)), 1477 (sum($p + 1)), 1511 (count($p)),
1541 (sum(n.age) regression), 1569 (count(count(*))) — all of which
assert `AggregateProjection`, not `ExprProjection`. The widening
does not touch `AggregateProjection`'s wire shape or classification
path (`classifyFunction → classifyAggregateCall` at
`internal/query/cypher/expr.go:346-349` is unchanged), so every
`AggregateProjection`-asserting pin is bit-for-bit unchanged.

The count-star inside a rich expression (`count(*) + 1`) has no
parser-test pin today; it is grammar-valid but corpus-scenario-
distinct from `"count in arithmetic"`. If a future TCK bump adds a
pin for it, that pin authors under the widened shape with
`ContainsAggregate = true`.

---

## 5. New `TestNewExprProjectionWithAggregateTrue` — witnessing the true side

`internal/query/type_test.go` — one new test appended after the
existing three ExprProjection tests:

```go
// TestNewExprProjectionWithAggregateTrue pins the widened Stage-6 variant per
// ADR 0008 amendment 2026-07-06: the ContainsAggregate axis carries through
// the constructor, the accessor, and the wire shape as an omit-when-false key
// (wire convention: additive axes emit omit-when-zero-value).
// Complements TestExprProjectionMarshalJSON (which pins the
// containsAggregate=false zero-value default as an ABSENT key — that test
// stays verbatim).
func TestNewExprProjectionWithAggregateTrue(t *testing.T) {
    refs := []query.Ref{{Variable: "n"}}
    p := query.NewExprProjectionWithAggregate(refs, query.TypeInt{}, true)
    require.Equal(t, refs, p.Refs())
    require.Equal(t, query.TypeInt{}, p.Type())
    require.True(t, p.ContainsAggregate())

    out, err := json.Marshal(p)
    require.NoError(t, err)
    require.JSONEq(t,
        `{"kind":"expr","refs":[{"variable":"n","property":""}],"type":"int","containsAggregate":true}`,
        string(out))
}
```

Note (erratum, cycle close-out): the expected JSON carries
`"property":""` — `flatRef` (`internal/query/query.go:1580`) declares
`Property string` with **no** `omitempty`, so every wire ref always
emits the `property` key. The spec's original draft omitted it; the
landed test matches the marshaller, the only additive-safe direction.

The existing `TestExprProjectionMarshalJSON` (type_test.go:124-131)
stays **VERBATIM** — under the omit-when-false ruling in §4.1.3, the
`containsAggregate = false` zero value is not emitted, so the
already-pinned string
`{"kind":"expr","refs":[{"variable":"a","property":"n"}],"type":"int"}`
matches the widened marshaller bit-for-bit. That test is the sole
witness for the false side (encoded as key-absent); the new test
above is the sole witness for the true side.

`TestNewExprProjection` and `TestNewExprProjectionAllowsNoRefs`
(type_test.go:105-120) are UNCHANGED. Their assertions
(`require.Equal(t, refs, p.Refs())`, `require.Equal(t,
query.TypeInt{}, p.Type())`) do not consult `ContainsAggregate`;
the zero-value default the preserved `NewExprProjection` constructor
supplies matches implicitly.

---

## 6. ADR 0008 amendment note — the dated stage note

The amendment lands at the top of ADR 0008, above the existing
`> _Note (…):` block if any (following the ADR 0003 stage-note
convention at `docs/adr/0003-curated-dialect-agnostic-query-model.md:3-7`).

Verbatim text:

```
> _Amendment (2026-07-06, gqlc-hk0 model-change cycle): the
> ContainsAggregate axis on `ExprProjection` — recorded as an
> escape hatch in the "Known deferred additions" list — is
> **adopted** under this ADR's coordinated change with consumers.
> Shape A (promote nested-aggregate residuals to
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
> JSON encoding is **omit-when-false**
> (`,omitempty`), which establishes the wire
> convention for the remainder of this ADR's additions convention:
> additive axes emit **omit-when-zero-value**, deliberately
> diverging from the earlier always-emit precedent
> (`directed`, `nullable`, `returnsAll`, `hops`) because
> golden rebaselines are the primary auditability
> surface and always-emit forces near-total 3199-file rebaselines
> on each additive cycle. See
> `docs/specs/model-change-hk0-containsaggregate.md` for the full
> contract, the walker boundaries, the 20-golden rebaseline set,
> and the semantic-diff-only fence command._
```

The bullet in "Known deferred additions"
(`docs/adr/0008-query-model-surface-resolver-api.md:166-169`) is
updated in the same PR:

```
- **`ContainsAggregate` axis on `ExprProjection`** — adopted
  2026-07-06 (see the amendment note above and
  `docs/specs/model-change-hk0-containsaggregate.md`). Populated
  parser-side by `classifyRichExpression`'s subtree walk; consumed
  by the resolver's `fillGroupingKeys` (`internal/resolver/resolve.go`)
  to discriminate aggregate-carrying residuals from grouping-key
  candidates. Never inferred from `Type`.
```

No other ADR text changes. ADR 0003, ADR 0004, ADR 0006, and
ADR 0009 are untouched — the ADR 0008 record's shape and the
resolver's staged-build discipline are unaffected by the additive
axis. The additions convention itself is exercised, not amended.

---

## 7. Resolver widening — the follow-up PR after the change merges

The resolver widening is spec'd here but LANDS as a separate PR
after the model-change PR merges. It cannot bundle with the change
because the model-change PR's fence is "resolver stays byte-identical"
(§8) — the widening PR adds two new fixtures (§7.4a and §7.4b) and
preserves every pre-existing fixture's byte-identity contract via
the enumeration in §7.5.

### 7.1 `fillGroupingKeys` — the exact widening (gate + loop)

Before (`internal/resolver/resolve.go:571-602`, verbatim per §3.4):

```go
// R5 §4.5.2 — grouping applies when at least one AggregateProjection is
// present.
if part.ReturnsAll { return }
hasAggregate := false
for _, item := range part.Returns {
    if _, ok := item.Value.(query.AggregateProjection); ok {
        hasAggregate = true
        break
    }
}
if !hasAggregate { return }
// Grouping applies. Non-aggregate, non-ExprProjection items are keys.
for i, item := range part.Returns {
    switch item.Value.(type) {
    case query.RefProjection, query.LiteralProjection, query.FuncProjection:
        cols[i].GroupingKey = true
    }
    // AggregateProjection and ExprProjection remain false (§4.5.2
    // uniform-exclude).
}
```

After:

```go
// Grouping applies when at least one aggregate is present anywhere in
// Returns — either as a top-level AggregateProjection OR embedded inside
// an ExprProjection (Shape B, ADR 0008 amendment 2026-07-06;
// docs/specs/model-change-hk0-containsaggregate.md).
if part.ReturnsAll { return }
hasAggregate := false
for _, item := range part.Returns {
    switch v := item.Value.(type) {
    case query.AggregateProjection:
        hasAggregate = true
    case query.ExprProjection:
        if v.ContainsAggregate() {
            hasAggregate = true
        }
    }
    if hasAggregate { break }
}
if !hasAggregate { return }
// Grouping applies. Non-aggregate items are keys; ExprProjection is a
// key iff it does NOT contain a nested aggregate.
for i, item := range part.Returns {
    switch v := item.Value.(type) {
    case query.RefProjection, query.LiteralProjection, query.FuncProjection:
        cols[i].GroupingKey = true
    case query.ExprProjection:
        if !v.ContainsAggregate() {
            cols[i].GroupingKey = true
        }
    }
    // AggregateProjection stays false (the aggregate itself is not a key).
}
```

**Two changes**, not one:

1. **`hasAggregate` gate widens.** An `ExprProjection` whose
   `ContainsAggregate() == true` now flips the gate independently
   of any `AggregateProjection` sibling.
2. **Grouping-key loop widens.** An `ExprProjection` whose
   `ContainsAggregate() == false` is marked as a grouping key
   (was: uniformly excluded).

**Why the gate widens** (round-2 design ruling). Without the gate
widening, `RETURN n.x, count(n)+1` under-groups: the `count(n)+1`
ExprProjection has `ContainsAggregate=true`, but no
`AggregateProjection` sibling, so the pre-widening gate returns
early and `n.x` is never marked as a key. Meanwhile, `RETURN n.x,
count(n), count(n)+1` correctly groups because the top-level
`count(n)` flips the pre-widening gate. Two queries that agree on
"row groups keyed by `n.x` when aggregating over the group" would
disagree on `n.x`'s wire-observable `GroupingKey` — an
inconsistency that violates gqlc-hk0's "exact grouping-key
semantics for mixed Parts" acceptance criterion. Widening the gate
resolves the split: any aggregate — top-level or embedded — makes
`n.x` a grouping key when it stands alongside.

The doc comment on `fillGroupingKeys` at
`internal/resolver/resolve.go:571-573` updates:

```go
// fillGroupingKeys populates Column.GroupingKey for branch 0's final Part per
// §4.5.2. Grouping mode is entered when Returns contains at least one
// aggregate — either as a top-level AggregateProjection OR embedded inside
// an ExprProjection (ContainsAggregate() == true). In grouping mode,
// ExprProjection is a grouping key iff ContainsAggregate() == false
// (ADR 0008 amendment 2026-07-06).
```

The R5 `// Uniform-exclude posture: ExprProjection is NEVER a
grouping key.` line RETIRES.

**FuncProjection-vs-ExprProjection soundness verification.** The
widened loop marks `FuncProjection` unconditionally as a grouping
key. This is sound only if `FuncProjection` cannot smuggle a nested
aggregate — verified first-party against the parser at branch base:

- `classifyFunction` at `internal/query/cypher/expr.go:336-363`
  branches on `aggregateFunc(name)`: if the outermost function
  name is in the closed aggregate set (`count, sum, collect, min,
  max, avg, stDev, stDevP, percentileCont, percentileDisc,
  percentile`), the call lowers as `AggregateProjection` via
  `classifyAggregateCall`; otherwise it calls `functionArgRefs`.
- `functionArgRefs` at
  `internal/query/cypher/shape.go:260-279` requires each argument
  to be either a bare var / var.prop (yielding a Ref) or a scalar
  literal. Any argument that is anything else — a nested function
  call (`toFloat(count(n))`), arithmetic (`toFloat(n.x + 1)`), a
  parameter, a list literal — makes `functionArgRefs` return
  `nil, false`.
- When `functionArgRefs` fails, `classifyFunction` returns
  `nil, false`; `classifyProjection` at
  `internal/query/cypher/expr.go:260-264` then also returns
  `nil, false`; and `collectReturnItem` at
  `internal/query/cypher/expr.go:200-201` falls through to
  `classifyRichExpression`, producing an `ExprProjection` — NOT
  a `FuncProjection`.

Therefore `FuncProjection` can only ever carry
bare-var/prop/literal arguments; a nested aggregate (`toFloat(
count(n))`) is classified as `ExprProjection` whose
`ContainsAggregate()` correctly returns `true` via the widened
walk. The unconditional grouping-key mark on `FuncProjection` is
sound. **No soundness hole.**

### 7.2 The five sub-cases the widening handles

| # | Part.Returns pattern | AggProj present? | ExprProj.ContainsAggregate | `hasAggregate` gate | Grouping-key marking (this cycle vs R5) |
|---|---|---|---|---|---|
| 1 | No AggregateProjection, no ExprProjection | no | n/a | false — early return | no keys marked. Unchanged from R5. |
| 2 | No AggregateProjection, ExprProjection(s), all `ContainsAggregate=false` (e.g. `RETURN 1 + n.age`) | no | false | false — early return | no keys marked. Unchanged from R5. Consistent with openCypher: no aggregate → no grouping. |
| 3 | AggregateProjection present, ExprProjection present, all ExprProjections have `ContainsAggregate=true` (e.g. `RETURN 1 + count(n), count(m)`) | yes | true | true (via AggProj) | ExprProjection excluded (aggregate residual is not a key). Matches R5 uniform-exclude by coincidence — the widening's exclusion is honest, R5's is uniform. Golden UNCHANGED. |
| 4 | AggregateProjection present, ExprProjection present, at least one ExprProjection has `ContainsAggregate=false` (e.g. `RETURN 1 + n.age, count(n)`) | yes | false (mixed) | true (via AggProj) | The non-aggregate ExprProjection is marked as a key. **Behaviour differs from R5.** Golden CHANGES for the new fixture (§7.4a). |
| 5 | **No AggregateProjection**, at least one ExprProjection has `ContainsAggregate=true`, plus one or more key-eligible siblings (e.g. `RETURN n.age, 1 + count(n)`) | no | true | **true (via widened gate — new)** | Non-aggregate siblings marked as keys; the embedded-aggregate ExprProjection excluded. **Behaviour differs from R5 (which returned early).** Golden CHANGES for the new fixture (§7.4b). |

Sub-cases 4 and 5 have wire-observable behaviour changes. Sub-case
3 matches by coincidence. Sub-cases 1 and 2 are gated out before
the loop runs. **Sub-case 5 is new to round 2** — the gate-widening
ruling motivates it.

### 7.3 R5 R6 R7 grouping-key semantics — the exact decision sites that widen

The grouping-key machinery has ONE decision site: `fillGroupingKeys`
at `internal/resolver/resolve.go:571-602`. All grouping-key witness
sites — the `GroupingKey` field on `Column` at
`internal/resolver/validated.go:15-27` — are populated by this
function alone. Verified: no other site in `internal/resolver`
writes `cols[i].GroupingKey = true`. Search:

```
grep -n "GroupingKey" internal/resolver/*.go
```

yields hits only at:

- `internal/resolver/validated.go` — the field declaration and
  serialisation.
- `internal/resolver/resolve.go:151` — the call site
  (`fillGroupingKeys(finalCols, finalPart)`).
- `internal/resolver/resolve.go:571-602` — the function itself.
- `internal/resolver/resolve.go:359` — a comment noting the R5
  projection walk leaves GroupingKey false (unchanged; the walk
  does not participate).

**Conclusion:** the widening is one-site. The R5 `§4.5.3` gap
closes exactly here; no other resolver machinery is touched.

### 7.4 New fixtures — sub-case 4 and sub-case 5 witnesses

Two new fixtures land in the widening PR: one per newly-observable
sub-case. Both use `social_r7.gql`.

#### 7.4a `aggregate_with_expr_grouping_key.cypher` — sub-case 4

The R5-spec-recorded target fixture (`docs/specs/resolver-stage-r5.md
§4.5.3.5`) — witnesses the sub-case 4 change.

**Fixture query** (`test/data/resolver/valid/aggregate_with_expr_grouping_key.cypher`):

```
MATCH (n:Person) RETURN 1 + n.age, count(n)
```

**Schema mapping entry** in
`test/data/resolver/valid/schema.mapping.json`:

```
"aggregate_with_expr_grouping_key.cypher": "social_r7.gql"
```

**Expected golden**
(`test/data/resolver/valid/aggregate_with_expr_grouping_key.cypher.validated.golden.json`):

```json
{
  "columns": [
    {
      "name": "1 + n.age",
      "type": {
        "kind": "unknown"
      },
      "groupingKey": true
    },
    {
      "name": "count(n)",
      "type": {
        "kind": "scalar",
        "scalar": "int"
      },
      "groupingKey": false
    }
  ],
  "parameters": [],
  "statement": "read",
  "distinct": false
}
```

The `1 + n.age` ExprProjection carries `ContainsAggregate=false`
(pure arithmetic over a property lookup — no aggregate in the
subtree), so the widened fillGroupingKeys marks it as a grouping
key. The `count(n)` AggregateProjection stays a non-key. Confirmed
against §7.2 sub-case 4.

Note (erratum, cycle close-out): the column type is
`{"kind": "unknown"}`, not scalar int as originally hand-drawn. The
parser types property lookups as `TypeUnknown`, and `promoteBase`
(`internal/query/cypher/typing.go:805-810`) propagates unknown
through arithmetic when either operand is unknown; the resolver's
ExprProjection column-type dispatch
(`internal/resolver/resolve.go:1116-1117`) is a `resolveType`
pass-through. Typing behavior — the widening changes only
`groupingKey` bits.

#### 7.4b `aggregate_with_expr_only_grouping.cypher` — sub-case 5 (round-2 addition)

Witnesses the gate-widening ruling: an embedded aggregate inside
an ExprProjection is enough to enter grouping mode, and its key-
eligible sibling `n.age` gains `GroupingKey=true`.

**Fixture query** (`test/data/resolver/valid/aggregate_with_expr_only_grouping.cypher`):

```
MATCH (n:Person) RETURN n.age, 1 + count(n)
```

**Schema mapping entry** in
`test/data/resolver/valid/schema.mapping.json`:

```
"aggregate_with_expr_only_grouping.cypher": "social_r7.gql"
```

**Expected golden**
(`test/data/resolver/valid/aggregate_with_expr_only_grouping.cypher.validated.golden.json`):

```json
{
  "columns": [
    {
      "name": "n.age",
      "type": {
        "kind": "property",
        "type": "INT",
        "nullable": true
      },
      "groupingKey": true
    },
    {
      "name": "1 + count(n)",
      "type": {
        "kind": "scalar",
        "scalar": "int"
      },
      "groupingKey": false
    }
  ],
  "parameters": [],
  "statement": "read",
  "distinct": false
}
```

The RefProjection `n.age` is marked as a grouping key by the
widened loop; the ExprProjection `1 + count(n)` carries
`ContainsAggregate=true`, flipping the gate (previously false — no
AggregateProjection sibling), and is excluded on the discriminator.
Confirmed against §7.2 sub-case 5.

Note (erratum, cycle close-out): the `n.age` column type is the
house property shape `{"kind": "property", "type": "INT",
"nullable": true}` — a RefProjection over a schema property (cf.
`call_standalone_yield_items`), not scalar int as originally
hand-drawn.

**Invalid fixture set** — no additions. The widening does not
introduce a new sentinel; every fail-site remains covered by the
existing sentinel taxonomy. No `ContainsAggregate`-driven fail-site
exists at the resolver: the axis is a grouping-key discriminator,
not a validity gate.

### 7.5 Byte-identity fence over the R5/R6/R7 resolver goldens

Every pre-existing resolver-valid golden — the enumeration at branch
base `origin/master @ e77f33e`: **116 fixture pairs** (116
`*.cypher` sources + 116 `*.cypher.validated.golden.json` outputs),
one shared `schema.mapping.json`, and eight schema files under
`schemas/` (`social.gql` + `social_r1.gql` … `social_r7.gql`), for a
total of 234 files under `test/data/resolver/valid/` at branch
base — is **byte-identical** after the widening PR merges. Refresh
the fixture count at branch base by running
`ls test/data/resolver/valid/*.cypher | wc -l` (yields 116; each
pairs with a `*.cypher.validated.golden.json`).

**Enumeration of the potentially-affected fixtures and why each stays
byte-identical.**

The widening (§7.1) changes `fillGroupingKeys` in two ways:
(a) the `hasAggregate` gate scans `ExprProjection.ContainsAggregate()
== true` as inducing grouping (in addition to any top-level
`AggregateProjection`); (b) the loop treats an `ExprProjection`
with `ContainsAggregate() == false` as a grouping key rather than
uniformly excluding all ExprProjections. A pre-existing golden
STAYS byte-identical iff its wire outcome under the widened
`fillGroupingKeys` matches its wire outcome under the R5-era one.
That's a wire-observable claim on `GroupingKey`, not a
path-equivalence claim — the two implementations may reach the
same wire via different branches.

**Class A — fixtures with an embedded-only aggregate** (the sole
class where the gate widening is load-bearing at branch base):

- `aggregate_with_expr_residual.cypher` — `MATCH (n:Person)
  RETURN 1 + count(n)`. One column; no `AggregateProjection`
  sibling (the aggregate is nested inside the ExprProjection, not
  lifted). Enumeration proof at branch base: mining
  `test/data/resolver/valid/*.cypher` for queries whose
  RETURN/WITH projections carry an aggregate call embedded in a
  rich expression AND whose siblings contain no bare aggregate
  call yields **exactly this one fixture**. Discovery command
  (Python 3, run from the worktree root):

  ```
  python3 - <<'PY'
  import re, os, glob
  AGGS = r"(count|sum|collect|min|max|avg|stDev|stDevP|percentileCont|percentileDisc|percentile)"
  call = re.compile(rf"\b{AGGS}\s*\(", re.IGNORECASE)
  root = "test/data/resolver/valid"
  hits = []
  for path in sorted(glob.glob(f"{root}/*.cypher")):
      with open(path) as f: q = f.read()
      # Any embedded aggregate in a RETURN/WITH projection?
      embedded = False; standalone = False
      for m in re.finditer(
              r"\b(RETURN|WITH)\b(.+?)(?=\n\s*(?:ORDER|SKIP|LIMIT|"
              r"WHERE|MATCH|WITH|RETURN|UNWIND|MERGE|CREATE|DELETE|"
              r"SET|REMOVE|CALL|UNION)\b|$)",
              q, re.IGNORECASE | re.DOTALL):
          clause = re.sub(r"^\s*DISTINCT\s+", "", m.group(2),
                          flags=re.IGNORECASE)
          parts, d, buf = [], 0, ""
          for c in clause:
              if c in "([{": d += 1
              elif c in ")]}": d -= 1
              if c == "," and d == 0:
                  parts.append(buf); buf = ""
              else: buf += c
          parts.append(buf)
          for p in parts:
              ps = re.sub(r"\s+AS\s+\w+\s*$", "", p.strip(),
                          flags=re.IGNORECASE).strip()
              if not ps or not call.search(ps): continue
              m2 = re.match(rf"^{AGGS}\s*\(", ps, re.IGNORECASE)
              outermost_agg = False
              if m2:
                  dp = 0; i = m2.end() - 1
                  while i < len(ps):
                      c2 = ps[i]
                      if c2 == "(": dp += 1
                      elif c2 == ")":
                          dp -= 1
                          if dp == 0: break
                      i += 1
                  if ps[i+1:].strip() == "":
                      outermost_agg = True
              if outermost_agg: standalone = True
              else: embedded = True
      if embedded and not standalone:
          hits.append(os.path.basename(path))
  print(f"embedded-only aggregate fixtures: {hits}")
  PY
  ```

  Verified output at branch base `origin/master @ e77f33e`:
  `['aggregate_with_expr_residual.cypher']`.

  **Pre-widening path**: no `AggregateProjection` in Returns →
  `hasAggregate = false` (R5 gate) → early return at
  `internal/resolver/resolve.go:590-592` → every column's
  `GroupingKey` stays at its default `false`. R5 §4.5.2's
  uniform-exclude branch is NOT reached.
  **Post-widening path**: `ExprProjection.ContainsAggregate() =
  true` on the sole column → widened gate flips `hasAggregate =
  true` at the ExprProjection arm of the gate scan → execution
  reaches the widened grouping-key loop → the ExprProjection is
  excluded on the `ContainsAggregate` discriminator → the sole
  column's `GroupingKey` stays at its default `false`.
  Different code path, same wire outcome. **Golden byte-identical.**

**Class B — fixtures with no aggregate at all** (the vast
majority; enumerated by exclusion from Class A):

- `expr_projection_bool`, `expr_projection_list`,
  `expr_projection_unknown` — expressions typing as bool /
  list / TypeUnknown; no aggregate anywhere. Under both
  pre-widening and post-widening: `hasAggregate = false` at the
  gate → early return → every `GroupingKey` stays `false`.
  Golden unchanged.
- `literal_int_projection`, `literal_string_projection`,
  `literal_only_return` — `LiteralProjection`, not
  `ExprProjection`. The gate/loop doesn't inspect them (gate
  scans only `AggregateProjection` and `ExprProjection`, loop
  marks `Literal` unconditionally as a key when reached). Under
  both paths: gate stays `false` (no aggregate anywhere) →
  early return → `GroupingKey = false`. Golden unchanged.

**Class C — fixtures with a top-level `AggregateProjection`
sibling** (R5's original grouping-mode fixtures):

At branch base, mining `test/data/resolver/valid/*.cypher` for
queries whose RETURN/WITH contains at least one bare aggregate
call at a top-level projection position yields the R5/R6/R7
grouping-mode set — every fixture whose golden already carries
`groupingKey: true` on any column plus every fixture whose golden
has the `hasAggregate` gate open. For each such fixture:

- **All ExprProjection siblings have `ContainsAggregate = false`**
  (pre-widening: uniformly excluded via R5 §4.5.2; post-widening:
  marked as grouping keys via the widened loop). This is
  **sub-case 4** in §7.2, and it would CHANGE the wire outcome —
  the ExprProjection column's `GroupingKey` would go from `false`
  to `true`. **Enumeration proof at branch base**: mining for
  R5/R6/R7 fixtures matching sub-case 4 — a top-level aggregate
  sibling alongside a non-aggregate ExprProjection sibling —
  yields **zero fixtures**. Every existing R5-era grouping-mode
  fixture pairs the top-level aggregate with `RefProjection`,
  `LiteralProjection`, or `FuncProjection` siblings, never with a
  non-aggregate `ExprProjection`. Sub-case 4 is UNCOVERED at
  branch base; §7.4a's `aggregate_with_expr_grouping_key.cypher`
  is its first witness. Discovery command:

  ```
  python3 - <<'PY'
  import re, os, glob, json
  root = "test/data/resolver/valid"
  # A fixture matches sub-case 4 iff its golden has grouping mode ON
  # AND a column whose name is a non-aggregate expression that is not
  # a bare ref/literal/function call. Heuristic: golden has any column
  # with groupingKey=true, and any other column whose name embeds
  # arithmetic (+ - * / etc.) at depth 0 AND no aggregate call in the
  # expression text.
  AGGS = r"(count|sum|collect|min|max|avg|stDev|stDevP|percentileCont|percentileDisc|percentile)"
  call = re.compile(rf"\b{AGGS}\s*\(", re.IGNORECASE)
  arith = re.compile(r"[+\-*/]")
  hits = []
  for path in sorted(glob.glob(f"{root}/*.validated.golden.json")):
      with open(path) as f: g = json.load(f)
      cols = g.get("columns", [])
      any_grouping = any(c.get("groupingKey") for c in cols)
      if not any_grouping: continue
      for c in cols:
          n = c.get("name","")
          if arith.search(n) and not call.search(n):
              hits.append(os.path.basename(path))
              break
  print(f"sub-case 4 pre-existing fixtures: {hits}")
  PY
  ```

  Verified output at branch base: `[]`.

- **At least one ExprProjection sibling has `ContainsAggregate =
  true`** (**sub-case 3** in §7.2). Both pre-widening (R5
  uniform-exclude) and post-widening (honest-exclude) exclude
  the ExprProjection from the grouping-key set. Same wire.
  Golden unchanged.

**Class D — fixtures whose ONLY aggregate is embedded in an
ExprProjection, PLUS a key-eligible sibling** (sub-case 5 in
§7.2). Enumeration proof at branch base: the Class A discovery
command above returned exactly one fixture
(`aggregate_with_expr_residual`), which has NO key-eligible
sibling (its single column IS the embedded-aggregate
ExprProjection). Class D is therefore UNCOVERED at branch base;
§7.4b's `aggregate_with_expr_only_grouping.cypher` is its first
witness.

**Conclusion.** Class A has one fixture, and its wire outcome is
preserved via a different code path. Classes B and C stay
byte-identical by class-invariant reasoning. Classes 4 and 5 —
the newly-observable behaviour — are UNCOVERED at branch base;
the §7.4a / §7.4b fixtures are their first witnesses. Every
pre-existing golden stays byte-identical.

**The byte-identity fence check** (from the worktree, on the
widening PR's branch tip):

```
just test          # runs the full suite with -update absent
git diff test/data/resolver/valid/*.validated.golden.json
```

The diff MUST show:

- Exactly TWO new files:
  `aggregate_with_expr_grouping_key.cypher.validated.golden.json`
  and
  `aggregate_with_expr_only_grouping.cypher.validated.golden.json`,
  plus their two `.cypher` sources.
- Exactly two entries added to `schema.mapping.json` (one per
  new fixture, both pointing to `social_r7.gql`).
- Zero modifications to any pre-existing `.validated.golden.json`
  (the 116 branch-base pairs).

If any pre-existing golden regenerates, the widening PR is buggy.
The commit fails review.

### 7.6 Sentinel discipline — no new sentinel, no message widening

The widening is a semantics refinement of an existing arm, not a
new fail-site. `fillGroupingKeys` does not raise any sentinel today
(it's a pure setter over the `GroupingKey` field), and the widened
version does not either — the additive discriminator improves
correctness silently, matching the R5 `§4.5.3.2` preserved-vs-violated
split's PRESERVED axis (grouping-key subset). No sentinel additions;
no message-set widenings on any existing sentinel.

Recorded precedents:

- **R5** (`docs/specs/resolver-stage-r5.md §5.1`) added
  `ErrUnionColumnMismatch` — a new fail-site that had no prior
  sentinel.
- **R5.1** (`§5.1.1`) added `ErrPartBindingTypeConflict` — same
  posture.
- **R6** (`docs/specs/resolver-stage-r6.md §5.1`) added
  `ErrInvalidEffectTarget` — a new fail-site (the only class of
  write-shape failure no R0–R5 sentinel named honestly).
- **R7** (`docs/specs/resolver-stage-r7.md §5.1`) added zero
  sentinels; two message-set widenings on existing sentinels
  (`ErrUnknownProperty`, `ErrPartBindingTypeConflict`) covered new
  fail-sites.

This cycle adds zero sentinels and zero message widenings — the
widening ADMITS a fixture that R5 admitted (the aggregate-residual
sub-case) and CHANGES its wire semantics; the two other sub-cases
(no aggregate present; no ExprProjection present) are unchanged.
Discipline preserved: additions only where a new fail-site is
actually introduced.

---

## 8. Mergeability and PR sequence

The gqlc-hk0 campaign lands in three PRs, in this order:

1. **Spec PR** (this file). No code change. `just test` and
   `just lint-new` from the worktree are trivially green (docs-only).
2. **Change PR** — model + parser + ADR 0008 amendment +
   parser-test rebaseline. Resolver **untouched**; resolver test
   suite fully green. The specific fence:
   - `internal/query/query.go` gains the field + constructor +
     accessor + JSON key.
   - `internal/query/type_test.go` rebaselines
     `TestExprProjectionMarshalJSON` (one line) and adds
     `TestNewExprProjectionWithAggregateTrue` (one new test).
   - `internal/query/cypher/typing.go` widens
     `classifyRichExpression` and adds `subtreeContainsAggregate`.
   - `internal/query/cypher/parser_test.go:1319-1327` — the one
     pin flips per §4.3.
   - `docs/adr/0008-query-model-surface-resolver-api.md` — the
     amendment note and the "Known deferred additions" edit.
   - `internal/resolver/*` — untouched. The 116 resolver
     `.cypher.validated.golden.json` fixtures stay byte-identical
     (no wire change in `ValidatedQuery`; the new field lives only
     on `query.Query`'s ExprProjection JSON, which the resolver
     does not marshal — verified in §4.1.3).
   - `just test` from the worktree: PASSES.
   - `just lint-new` from the worktree: PASSES.
3. **Resolver-widening PR** — after PR 2 merges to master. Changes
   `internal/resolver/resolve.go:571-602` per §7.1 (both the
   `hasAggregate` gate and the grouping-key loop widen); adds two
   fixtures per §7.4a and §7.4b; updates one doc comment per §7.1.
   Every pre-existing fixture stays byte-identical per §7.5.
   `just test` from the worktree: PASSES.

**Cross-PR verification: PR 2's resolver-green claim.**

The resolver reads `query.Query` in memory (not JSON — `Resolve`
takes a `query.Query` value directly, per `internal/resolver/resolver.
go:26-32`). Adding an unread `containsAggregate` field to
`ExprProjection` is invisible to every existing resolver code path:

- `fillGroupingKeys` at `resolve.go:571-602` — reads `ExprProjection`
  via the type switch, ignores the new field. Behaviour unchanged.
- `refProjectionType` at `resolve.go:1102` (the `case query.
  ExprProjection` in the projection-type dispatcher) — reads
  `Type()`, ignores the new field. Behaviour unchanged.
- Every other resolver read of `ExprProjection` (verified by
  `grep -n ExprProjection internal/resolver/*.go`) — three hits,
  all in doc comments (validated.go:27, resolve.go:573,
  resolve.go:593, resolve.go:599) or in the `refProjectionType`
  type-switch (resolve.go:1102-1103). None consult the new field.

The resolver's JSON output (`ValidatedQuery.MarshalJSON`) does NOT
embed a `query.Query.Projection` — verified: `internal/resolver/
validated.go`'s marshalling walks `Columns` and `Parameters` only,
neither of which carries an ExprProjection. Resolver goldens stay
byte-identical across PR 2.

Confirmed: the resolver stays green through the model-change PR with the
field present but unconsumed.

---

## 9. Non-goals

The following are explicitly OUT OF SCOPE of the gqlc-hk0 campaign:

| Bead | Subject | Why out of scope |
|---|---|---|
| gqlc-fvo | Use→Part attribution across WITH — parameter Use type conflict silently admitted under any-valid-witness | Different axis (parameter Use vs projection classification); different model gap. Filed alongside R5 close-out. Independent change bead. |
| gqlc-0ig | (as filed, R7 close-out — verify against `bd show`) | Filed alongside R7. Separate model-change cycle. |
| gqlc-ay9 | R4 nullability under-approximation Class A — OPTIONAL-clause-sibling | Different subsystem (nullability, not grouping). Different model gap. |
| gqlc-5xg | R4 nullability under-approximation Class B — same-Part re-MATCH missing-witness | Different subsystem (nullability, not grouping). Different model gap. |
| **Codegen** | Consumer of `ValidatedQuery` — codegen ADR post-R7 | ADR 0008 §Consequences: codegen is downstream of the resolver; the resolver-widening PR delivers the correct grouping-key wire, and codegen consumes it under a future ADR. |
| **`ContainsAggregate` at ANY other Projection variant** | Adding the axis to `AggregateProjection`, `FuncProjection`, etc. | `AggregateProjection` IS an aggregate by construction — the bit would be a tautology. `FuncProjection` cannot contain an aggregate under the query model (`classifyFunction` at `expr.go:346-349` routes aggregate names to `AggregateProjection` before `FuncProjection` — verified verbatim). No other variant would benefit; no bead. |

**gqlc-hk0 closes** at the resolver-widening PR merge:
- The bead moves to `closed`.
- The R5 §7.1.5 follow-up bead reference resolves.
- The R6 §7.1.3 "R5-inherited gaps carry unchanged" claim revises
  to record hk0's closure in the R-later spec (if a subsequent
  R-later stage authors one).

---

## 10. Ground-truth cross-check

Every factual claim in this spec is verifiable against source; the
citations below name the file:line the claim rests on. Re-verify at
branch base `origin/master @ e77f33e` before writing code.

**`ExprProjection` shape (§3.1, §4.1):**

- Struct: `internal/query/query.go:1171-1174` (two fields).
- `NewExprProjection` constructor:
  `internal/query/query.go:1176-1181`.
- `Refs()`, `Type()`, `isProjection()` accessors:
  `internal/query/query.go:1185-1192`.
- `MarshalJSON`: `internal/query/query.go:1194-1203` (three
  always-emit fields).
- `projectionKindExpr` constant: `internal/query/query.go:1475`.
- `flatRef` + `flattenRefs`: `internal/query/query.go:1553-1571`.
- JSON test witness: `internal/query/type_test.go:124-131`.
- ExprProjection unit tests:
  `internal/query/type_test.go:101-131` (three tests).

**Parser-side aggregate detection (§3.2, §4.2):**

- `classifyRichExpression`: `internal/query/cypher/typing.go:866-876`.
- Its sole call site: `internal/query/cypher/expr.go:201`
  (`collectReturnItem` fallback).
- `typeExpressionMining`: `internal/query/cypher/typing.go:41-51`.
- `typeAtom` COUNT arm: `internal/query/cypher/typing.go:340-345`.
- `typeAtom` aggregate function arm:
  `internal/query/cypher/typing.go:346-367`, with the aggregate check
  at `:358-365`.
- `aggregateFunc`: `internal/query/cypher/shape.go` (verify at
  branch base).
- Aggregate closed set: `internal/query/query.go:1211-1230`
  (`AggCount`, `AggSum`, `AggCollect`, `AggMin`, `AggMax`, `AggAvg`,
  `AggStdev`, `AggPercentile`).
- `classifyFunction`: `internal/query/cypher/expr.go:336-363`.
- `classifyAggregateCall`: `internal/query/cypher/expr.go:380-407`.

**Parser deferral pin (§3.5, §4.3):**

- Location: `internal/query/cypher/parser_test.go:1308-1327`
  (comment header at :1308-1318; pin body at :1319-1327).
- `NewExprProjection` call in the pin's `want`:
  `parser_test.go:1324`.

**Resolver grouping-key machinery (§3.4, §7.1, §7.3):**

- `fillGroupingKeys`: `internal/resolver/resolve.go:571-602`.
- Its sole call site: `internal/resolver/resolve.go:151`
  (`resolveBranch` fillGroupingKeys(finalCols, finalPart)).
- `Column.GroupingKey`: `internal/resolver/validated.go:15-27`.
- No other write site — search: `grep -n "GroupingKey" internal/
  resolver/*.go`.

**ADR 0008 (§6):**

- ADR 0008: `docs/adr/0008-query-model-surface-resolver-api.
  md:7-12`.
- Additive-only additions convention:
  `docs/adr/0008-query-model-surface-resolver-api.md:144-155`.
- `ContainsAggregate` in "Known deferred additions":
  `docs/adr/0008-query-model-surface-resolver-api.md:166-169`.

**R5 uniform-exclude posture (§2, §3.7):**

- `docs/specs/resolver-stage-r5.md §4.5.3.1` (query model records
  the residual) at line 1255.
- `docs/specs/resolver-stage-r5.md §4.5.3.2` (R5 uniform-exclude
  posture; preserved-vs-violated split) at line 1301.
- `docs/specs/resolver-stage-r5.md §4.5.3.3` (change options —
  Shape B ranked first) at line 1377.
- `docs/specs/resolver-stage-r5.md §4.5.3.4` (why not re-parse; B7
  evidence) at line 1463.
- `docs/specs/resolver-stage-r5.md §4.5.3.5` (target discriminating
  fixture, deferred) at line 1498.
- `docs/specs/resolver-stage-r5.md §7.1.5` audit note (table entry
  at line 2332).

**R6/R7 R5-inherited gaps carry (§2):**

- `docs/specs/resolver-stage-r6.md §7.1.3` line 1994.
- `docs/specs/resolver-stage-r7.md` lines 56-60.

**Justfile recipes (§8):**

- `just test`: `justfile` — the full-suite recipe.
- `just lint-new`: `justfile` — the fast pre-push variant.

**EdgeBinding `directed` axis precedent (§3.6):**

- Field: `internal/query/query.go:378`.
- Constructor: `internal/query/query.go:388-393` (trailing param).
- Accessor: `internal/query/query.go:445-447`.
- Always-emit JSON: `internal/query/query.go:1497-1508`
  (`Directed bool json:"directed"` unconditional in the composite
  literal).

**Resolver reads of ExprProjection (§8):**

- `internal/resolver/resolve.go:1102` — `case query.ExprProjection`
  in `refProjectionType`.
- `internal/resolver/resolve.go:573` — doc comment
  (uniform-exclude, retires under §7.1).
- `internal/resolver/resolve.go:593`, `:599` — doc comments in
  `fillGroupingKeys`.
- `internal/resolver/validated.go:27` — doc comment on
  `Column.GroupingKey`.

**Parser tests — the 25 `NewExprProjection` assertions (§4.4):**

- Enumerated by
  `grep -n "NewExprProjection" internal/query/cypher/parser_test.go`
  at branch base `e77f33e`; the 24 non-aggregate assertions plus
  the one deferral pin at line 1324.

---

## 11. Definition of done for the spec cycle

This file exists on branch `model-change-hk0-spec` and is committed with
the message `docs(spec): change hk0 — ExprProjection.ContainsAggregate (Shape B)`.

`just test` from the worktree passes (docs-only cycle; nothing
depends on this file compiling).

`just lint-new origin/master` from the worktree passes (docs-only
cycle; no lint targets touched).

Follow-up PRs (Cycles 2 and 3) reference this brief as their
implementation contract. The bead `gqlc-hk0` moves out of
`in_progress` only when the resolver-widening PR (Cycle 3) merges;
the spec PR does not close it.

---

## 12. Errata (2026-07-06, cycle close-out)

Five defects found during the adversarial implementation cycle
(PRs #111/#112/#113), corrected in place above after all three PRs
merged. None affected landed code — each was a spec-text divergence
from resolver/marshaller reality caught by first-party corroboration.

1. **Fence strip-key command (both copies — §4.1.3 and §4.4.1
   Fence 1):** the original `json.dump` re-write lacked Go
   `encoding/json`'s HTML escaping of `<`, `>`, `&`, false-positiving
   243 goldens. Both copies now use the `go_dump` shim
   (`ensure_ascii=False` + escape the three HTML chars).
2. **§5 expected JSON:** omitted `"property":""` — `flatRef`
   (`internal/query/query.go:1580`) has no `omitempty` on `Property`,
   so refs always emit the key. Block now matches the landed test
   verbatim.
3. **§6 ADR amendment note (and the landed ADR text, fixed in the
   same commit):** listed three of the four full walker stops,
   omitting `OC_PatternPredicate` (`typing.go:389`).
4. **§7.4a golden:** `1 + n.age` column type is `{"kind": "unknown"}`
   (`promoteBase` unknown-propagation), not scalar int.
5. **§7.4b golden:** `n.age` column type is the house property shape
   `{"kind": "property", "type": "INT", "nullable": true}`, not
   scalar int.
