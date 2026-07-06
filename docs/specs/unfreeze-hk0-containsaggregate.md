# Model unfreeze — `ExprProjection.ContainsAggregate` (Shape B)

The implementation brief for cycle **gqlc-hk0** of the model-unfreeze
campaign: an additive `ContainsAggregate bool` axis on
`query.ExprProjection`, populated parser-side during the walk
`classifyRichExpression` already performs, closing the R5 uniform-exclude
grouping-key gap (`docs/specs/resolver-stage-r5.md §4.5.3`) without
touching the frozen wire shape's zero-value semantics.

This brief is the **contract for the whole hk0 cycle**: it spans the
spec PR (this file), the unfreeze PR (model + parser + ADR 0008
amendment + parser-test rebaseline; resolver untouched and green), and
the resolver-widening PR (fillGroupingKeys widens to discriminate
`ContainsAggregate`; new discriminating fixture + adjusted golden set).
Both code PRs land under ADR 0008's post-freeze revision protocol
(§Post-freeze revision protocol) — additive-only, dated amendment,
golden rebaseline whose diff shows only the new surface.

The four other unfreeze beads (`gqlc-fvo`, `gqlc-0ig`, `gqlc-ay9`,
`gqlc-5xg`) and codegen are **out of scope** — this campaign closes
hk0 alone. See §9 for the non-goals table.

---

## 1. Deliverables

Spec cycle (Cycle 1) — this PR:

- `docs/specs/unfreeze-hk0-containsaggregate.md` — this file.

Unfreeze cycle (Cycle 2, follow-up PR):

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
- `internal/query/cypher/typing.go:857-877` —
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
- `docs/adr/0008-query-model-freeze-resolver-api.md` — one dated
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

Resolver-widening cycle (Cycle 3, follow-up PR after the unfreeze PR
merges):

- `internal/resolver/resolve.go:571-602` — `fillGroupingKeys` widens
  its `ExprProjection` arm: an `ExprProjection` with
  `ContainsAggregate() == false` becomes a grouping-key candidate
  (marked `true` under the `hasAggregate` gate); one with
  `ContainsAggregate() == true` continues to be excluded. §7.
- `internal/resolver/validated.go:15-27` — the doc comment on
  `Column.GroupingKey` updates to remove the "uniform-exclude"
  wording; the axis semantics themselves are unchanged. §7.
- `test/data/resolver/valid/aggregate_with_expr_grouping_key.cypher`
  + `.validated.golden.json` — one new fixture, the
  R5-spec-recorded target-discriminating shape
  (`docs/specs/resolver-stage-r5.md §4.5.3.5` post-follow-up target).
  §7.4.
- Every pre-existing resolver-valid golden is byte-identical after
  the widening PR (§7.5). The fence check runs from the worktree:
  `just test` without `-update` over the 116-fixture-pair R0–R7
  corpus at branch base (`ls test/data/resolver/valid/*.cypher |
  wc -l` = 116). Only the one new fixture appears in the diff.

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
`internal/query/cypher/typing.go:857-877`), so no resolver-side
re-parse of the original text — including any of Shape A
(`OriginalText`) or Shape C (`ReturnItem.TextSpan`) — recovers it.

ADR 0008's "Known deferred additions" registered a
`ContainsAggregate` axis on `ExprProjection` as the escape hatch:

> **`ContainsAggregate` axis on `ExprProjection`** — the escape hatch
> recorded on gqlc-gyw. The committed strategy for grouping-key discovery
> over expression residuals (nested aggregates like `count(n) + 1`) is a
> resolver-side re-parse of the projection's original text span; the axis
> is added only if that proves untenable, and is never inferred from `Type`.

`(docs/adr/0008-query-model-freeze-resolver-api.md:166-169)`.

R5 §4.5.3.4 pinned the "untenable" — the escape-hatch condition
fires. This cycle promotes the axis from escape hatch to committed
strategy under the additive-only revision protocol, records the
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
records the post-freeze convention and the fence).
The ADR 0008 amendment records the axis's promotion. The resolver's
`fillGroupingKeys` gains one branch on the new bit.

**Why Shape B and not Shape A.** Shape A (promote nested-aggregate
residuals to `AggregateProjection`) is a semantic widening of the
existing sum variant — every downstream consumer of
`AggregateProjection` would need to be audited. Shape B is a bit on
one existing variant — one new field, one new accessor, one new
constructor overload, one JSON key. Shape C is retired (`R5 §4.5.3.3`
final ranking): text-based recovery cannot re-materialise the
aggregate structure that `classifyRichExpression` dropped.

---

## 3. Mining — what the frozen model records today

Every claim in §4 rests on citations here. Re-verify each file:line at
branch base `origin/master @ e77f33e` before writing code — line
numbers can drift on merge.

### 3.1 `ExprProjection` — the frozen shape

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

The `switch item.Value.(type)` at line 594-598 is the exact widening
site: adding an `ExprProjection` arm that consults
`item.Value.(query.ExprProjection).ContainsAggregate()` and marks the
key when false is the entire resolver-side change. The `hasAggregate`
gate is unchanged — the presence of an `AggregateProjection` still
gates grouping mode. §7.1 pins the widened body.

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
JSON default on a frozen sum variant is `EdgeBinding.directed`
(added at Stage 5; frozen at ADR 0008). Its layout:

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
  `NewEdgeBinding` because Stage 5 was pre-freeze; every parser call
  site was rewritten in the same cycle. `ContainsAggregate` lands
  post-freeze, so an additive constructor overload is preferable to
  a breaking parameter addition. **The existing
  `NewExprProjection(refs, t)` constructor stays** as the
  zero-value-default shorthand; a new
  `NewExprProjectionWithAggregate(refs, t, containsAggregate)` takes
  the third parameter. Justification: the additive-only revision
  protocol (`ADR 0008 §Post-freeze revision protocol`) is
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
  cycle establishes the campaign convention that post-freeze
  additive axes emit **omit-when-zero-value**, deliberately not
  following the pre-freeze `directed` precedent — a decision the
  ADR 0008 amendment note in §6 records verbatim.

### 3.7 R5's audit note — what this cycle closes

`docs/specs/resolver-stage-r5.md §4.5.3` records:

- **§4.5.3.1** — the shape of the frozen model at classification;
- **§4.5.3.2** — R5's uniform-exclude posture, the
  preserved-vs-violated split (grouping-key SUBSET preserved;
  result-set semantics for non-aggregate residuals violated);
- **§4.5.3.3** — the two unfreeze options (Shape A, Shape B; Shape C
  retired), with Shape B ranked first;
- **§4.5.3.5** — the target discriminating fixture
  `aggregate_with_expr_grouping_key.cypher` — `MATCH (n:Person)
  RETURN 1 + n.age, count(n)` — which R5 excludes because under the
  uniform-exclude posture it would encode the R5 gap directly.

This cycle DELIVERS the R5 §4.5.3.3 recommendation (Shape B),
adds the R5 §4.5.3.5 target fixture (§7.4), and closes the R5
§7.1.5 follow-up bead.

---

## 4. The unfreeze — parser and model changes

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

#### 4.1.3 JSON — omit-when-false, deliberately diverging from pre-freeze precedent

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
an always-emit encoding the unfreeze PR would rebaseline
**2055 goldens** whose semantic content is unchanged — the diff
would be dominated by mechanical `"containsAggregate":false` key
insertions, not the semantic change ADR 0008's "diff shows only the
new surface" fence targets.

**Ruling: omit-when-false (`,omitempty`).** Under this encoding the
**2048** ExprProjection-bearing goldens whose top-level Returns
position embeds no aggregate call remain byte-identical, and the
rebaseline diff surfaces **exactly** the goldens whose semantic
content actually changes — i.e. those whose ExprProjection now
carries `ContainsAggregate = true`. The rebaseline IS the semantic
change; nothing else moves.

**Campaign convention — recorded here.** Post-freeze additive axes
emit **omit-when-zero-value**, deliberately diverging from the
pre-freeze always-emit precedent (`directed`, `nullable`,
`returnsAll`, `hops` — verified verbatim at query.go:1483-1490 for
NodeBinding, :1497-1508 for EdgeBinding, :1613-1619 for the
returnsAll block). The reason for the divergence is the constraint
flip that ADR 0008's freeze codifies:

- **Pre-freeze:** parser was the only consumer; model churn was
  cheap; the wire-consistency argument for always-emit dominated
  because rebaseline cost was near-zero.
- **Post-freeze:** golden rebaselines are the primary auditability
  surface for every additive change. The remaining unfreeze
  campaign axes (Use→Part attribution on `PropertyUse`/`ExprUse`/
  `ClauseSlotUse` per gqlc-fvo; per-position CALL-arg records on
  `CallBinding` per gqlc-0ig; group fields on `Binding` per gqlc-ay9
  and gqlc-5xg) sit on records present in **nearly every golden**.
  An always-emit posture across the campaign forces near-total
  rebaselines of the 3199-file corpus on each cycle. Post-freeze
  auditability is the load-bearing constraint, and it is the
  constraint the omit-when-zero-value convention protects.

**The convention in one line.** For any additive axis landing under
the ADR 0008 revision protocol whose zero value is the semantic
default of the pre-freeze corpus, the JSON encoding is
`,omitempty`; the always-emit precedent applies only to axes
already frozen at the freeze snapshot.

**Consequences for the unfreeze PR fence.**

- **Parser-golden rebaseline scope.** The unfreeze PR rebaselines
  **exactly the 7 goldens** whose top-level Returns position embeds
  an ExprProjection whose expression text contains an aggregate
  function call, enumerated in §4.4 (mined by the discovery command
  recorded there). Every other ExprProjection-bearing golden — 2048
  files — stays byte-identical.
- **The reviewer-side fence.** Strip the `containsAggregate` key
  from all goldens and diff against the branch base. The result
  MUST be byte-identical. Recorded fence command (run from the
  worktree at the unfreeze PR's branch tip, against
  `origin/master @ e77f33e`):

  ```
  python3 - <<'PY'
  import glob, json, sys
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
          json.dump(data, f, indent=2); f.write('\n')
  PY
  git diff --stat origin/master -- \
      internal/query/cypher/testdata/golden/*.golden.json
  # MUST print: no changes.
  git checkout -- internal/query/cypher/testdata/golden/
  ```

  If the stat print is non-empty, some non-aggregate ExprProjection
  golden regenerated — the encoding leaked a `containsAggregate:
  true` where the semantic default should hold, or a spurious
  formatting change slipped in. The unfreeze PR is buggy.

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
but violates the "additive-only" reading of the revision protocol
in spirit — it modifies the internal signature of every typing
helper. The walk-based helper leaves the existing walk untouched
and localises the change to two files.

#### 4.2.1 Boundary posture — the walker is typing-scoped

`subtreeContainsAggregate` MUST mirror the typing walk's scope
boundaries. Concretely, at every ANTLR node whose typing behaviour
in `typeNonArithmetic` refuses to descend into the sub-tree because
it opens a nested scope, the aggregate probe MUST likewise NOT
descend. Three specific stops, verified verbatim against
`internal/query/cypher/typing.go:382-403`:

- **`OC_ExistentialSubquery`** — `EXISTS { … }` returns a boolean
  at `typing.go:382-388`; parameters inside the subquery are mined
  at `EnterOC_ExistentialSubquery` (listener.go, `subqueryDepth`
  counter) and never enter the outer part's state. An aggregate
  inside `EXISTS { RETURN count(n) }` aggregates over the
  subquery's rows, not over the outer Part's rows, so it does not
  discriminate the outer projection.
- **`OC_ListComprehension`** — carries a list result whose element
  type is honestly `TypeUnknown` (Stage 11's ADR-0005-aligned
  posture at `typing.go:398-403`); the projection sub-tree runs in
  the iteration variable's local scope. An aggregate inside
  `[x IN xs WHERE count(x) > 0]` — grammar-valid in the vendored
  Cypher but semantically closed over the comprehension's inner
  rows — does not aggregate over the outer Part.
- **`OC_PatternComprehension`** — same posture as
  `OC_ListComprehension` at the same site.

The probe MUST stop at these three atoms and return `false` for
their subtrees, mirroring the typing walk's own refusal to descend.
Formally: `aggregateProbe.EnterOC_ExistentialSubquery`,
`aggregateProbe.EnterOC_ListComprehension`, and
`aggregateProbe.EnterOC_PatternComprehension` MUST set a
`skipChildren` flag on the walker, or override the enter callbacks
to return early WITHOUT recursing into the sub-tree's atoms.

**Semantic justification.** `ExprProjection.ContainsAggregate`
answers exactly one question — "does the resolver's
`fillGroupingKeys` exclude this projection from the grouping key
set of the enclosing Part?" — and only aggregates that aggregate
over the enclosing Part's rows can motivate that exclusion. An
aggregate inside a sub-scope aggregates over the sub-scope, so
poisoning the outer projection's grouping-key eligibility on that
basis is a soundness error. The boundary is not an optimisation;
it is a correctness requirement.

**Boundary pins.** No parser-test pin exercises these boundaries
at branch base (they are grammar-valid but corpus-scenario-rare in
the vendored TCK). §5 adds no pin for them either — the walker is
a private helper, and its boundaries are witnessed indirectly by
the frozen shape of every existing `EXISTS` / comprehension pin
staying `ContainsAggregate = false` even when they textually
contain `count(…)`. If a future TCK bump adds a scenario whose
outer projection embeds an `EXISTS { … count … }`, the pin's `want`
authors `ContainsAggregate = false` on the outer ExprProjection.

### 4.3 The deferral pin — flip its bit

`internal/query/cypher/parser_test.go:1308-1327` — verbatim
before-state per §3.5. After the unfreeze:

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

#### 4.4.1 Golden-corpus rebaseline — the 7 flip fixtures

Under the omit-when-false JSON encoding (§4.1.3), the
`containsAggregate` key is emitted **only** when the ExprProjection's
walk returns `true`. The `internal/query/cypher/testdata/golden/`
corpus at branch base `origin/master @ e77f33e` contains **2055**
goldens that embed a `"kind": "expr"` blob; of those, exactly **7**
have an ExprProjection at a top-level Returns position whose
projected expression text (the ReturnItem `name`) embeds an aggregate
function call and therefore flips the bit under the widened walker
(§4.2). The 2048 remaining ExprProjection-bearing goldens carry the
zero value and stay byte-identical.

**Discovery command** (Python 3, run from the worktree root; the
project's host lacks `jq`):

```
python3 - <<'PY'
import glob, json, os, re
AGGS = r"(?:count|sum|collect|min|max|avg|stDev|stDevP|percentileCont|percentileDisc|percentile)"
call = re.compile(rf"\b{AGGS}\s*\(", re.IGNORECASE)

def find_returns(n, out):
    if isinstance(n, dict):
        if isinstance(n.get("returns"), list):
            for ri in n["returns"]:
                if isinstance(ri, dict) and "value" in ri and "name" in ri:
                    out.append(ri)
        for v in n.values(): find_returns(v, out)
    elif isinstance(n, list):
        for v in n: find_returns(v, out)

flips = []
for path in sorted(glob.glob(
        "internal/query/cypher/testdata/golden/*.golden.json")):
    with open(path) as f: data = json.load(f)
    ris = []
    find_returns(data, ris)
    hits = [ri["name"] for ri in ris
            if isinstance(ri.get("value"), dict)
            and ri["value"].get("kind") == "expr"
            and call.search(ri.get("name", ""))]
    if hits:
        flips.append((os.path.basename(path), hits))
print(f"flip goldens: {len(flips)}")
for f, hits in flips:
    print(f"  {f} -> {hits}")
PY
```

**The 7 flip goldens** (verbatim command output at branch base
`origin/master @ e77f33e`):

| Golden | Aggregate-embedding expression (from ReturnItem `name`) |
|---|---|
| `Return2_0d955bc4a162.golden.json` | `count(a) > 0` |
| `Return6_1544b3f065a8.golden.json` | `size(collect(a))` |
| `Return6_1620cd819bff.golden.json` | `$age + avg(person.age) - 1000` |
| `Return6_29fccd6d88dd.golden.json` | `me.age + count(you.age)` |
| `Return6_830281f0127e.golden.json` | `age + count(you.age)` |
| `Return6_c27310dae8c0.golden.json` | `count(a) + 3` |
| `Return6_d980d0acf9b2.golden.json` | `{foo: a.name='Andres', kids: collect(child.name)}` |

Under the widened `classifyRichExpression` (§4.2) each of these
seven ExprProjection blobs picks up `"containsAggregate": true`;
under omit-when-false that key is emitted, and the golden is
regenerated. Every other ExprProjection-bearing golden — the
remaining **2048** files — stays byte-identical because the walker
returns `false` and the omitempty tag suppresses the key.

**Semantic-diff-only fence** (the reviewer's proof that the
rebaseline surfaces only the semantic change):

```
python3 - <<'PY'
import glob, json
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
        json.dump(data, f, indent=2); f.write("\n")
PY
git diff --stat origin/master -- \
    internal/query/cypher/testdata/golden/*.golden.json
# MUST print: no changes.
git checkout -- internal/query/cypher/testdata/golden/
```

If that stat is non-empty, the unfreeze PR either leaked a
`containsAggregate: true` into a non-aggregate golden (walker
bug — §4.2 boundary violation, likely a missed sub-scope stop from
§4.2.1) or introduced a formatting drift into the marshaller
(unrelated to this axis but forbidden by the ADR 0008 fence). The
PR is buggy.

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
// TestNewExprProjectionWithAggregateTrue pins the widened Stage-6
// variant per ADR 0008 amendment 2026-07-06: the ContainsAggregate
// axis carries through the constructor, the accessor, and the wire
// shape as an omit-when-false key (§4.1.3 — post-freeze convention:
// additive axes emit omit-when-zero-value). Complements
// TestExprProjectionMarshalJSON (which pins the containsAggregate=false
// zero-value default as an ABSENT key — that test stays verbatim).
func TestNewExprProjectionWithAggregateTrue(t *testing.T) {
    refs := []query.Ref{{Variable: "n"}}
    p := query.NewExprProjectionWithAggregate(refs, query.TypeInt{}, true)
    require.Equal(t, refs, p.Refs())
    require.Equal(t, query.TypeInt{}, p.Type())
    require.True(t, p.ContainsAggregate())

    out, err := json.Marshal(p)
    require.NoError(t, err)
    require.JSONEq(t,
        `{"kind":"expr","refs":[{"variable":"n"}],"type":"int","containsAggregate":true}`,
        string(out))
}
```

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
> _Amendment (2026-07-06, gqlc-hk0 unfreeze cycle): the
> ContainsAggregate axis on `ExprProjection` — recorded as an
> escape hatch in the "Known deferred additions" list — is
> **adopted** under this ADR's additive-only revision protocol.
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
> `OC_ListComprehension`, `OC_PatternComprehension` — mirroring
> `typing.go:382-403`). The JSON encoding is **omit-when-false**
> (`,omitempty`), which establishes the post-freeze wire
> convention for the remainder of this ADR's revision protocol:
> additive axes emit **omit-when-zero-value**, deliberately
> diverging from the pre-freeze always-emit precedent
> (`directed`, `nullable`, `returnsAll`, `hops`) because
> post-freeze golden rebaselines are the primary auditability
> surface and always-emit forces near-total 3199-file rebaselines
> on each additive cycle. See
> `docs/specs/unfreeze-hk0-containsaggregate.md` for the full
> contract, the walker boundaries, the 7-golden rebaseline set,
> and the semantic-diff-only fence command._
```

The bullet in "Known deferred additions"
(`docs/adr/0008-query-model-freeze-resolver-api.md:166-169`) is
updated in the same PR:

```
- **`ContainsAggregate` axis on `ExprProjection`** — adopted
  2026-07-06 (see the amendment note above and
  `docs/specs/unfreeze-hk0-containsaggregate.md`). Populated
  parser-side by `classifyRichExpression`'s subtree walk; consumed
  by the resolver's `fillGroupingKeys` (`internal/resolver/resolve.go`)
  to discriminate aggregate-carrying residuals from grouping-key
  candidates. Never inferred from `Type`.
```

No other ADR text changes. ADR 0003, ADR 0004, ADR 0006, and
ADR 0009 are untouched — the freeze contract's shape and the
resolver's staged-build discipline are unaffected by the additive
axis. The freeze protocol itself is exercised, not amended.

---

## 7. Resolver widening — the follow-up PR after the unfreeze merges

The resolver widening is spec'd here but LANDS as a separate PR
after the unfreeze PR merges. It cannot bundle with the unfreeze
because the unfreeze PR's fence is "resolver stays byte-identical"
(§8) — the widening PR flips one column of one new fixture and one
existing fixture's byte-identity contract is preserved via the
enumeration in §7.5.

### 7.1 `fillGroupingKeys` — the exact widening

Before (`internal/resolver/resolve.go:571-602`, verbatim per §3.4):

```go
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
// Grouping applies. Non-aggregate items are keys; ExprProjection is
// a key iff it does NOT contain a nested aggregate (Shape B, ADR 0008
// amendment 2026-07-06; docs/specs/unfreeze-hk0-containsaggregate.md).
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

The `hasAggregate` gate above (`resolve.go:583-591`) is unchanged.
An `ExprProjection` whose `ContainsAggregate() == true` still gates
grouping mode ON ONLY if an `AggregateProjection` is also present —
this preserves R5's non-grouping-mode semantics for a pure
ExprProjection Part (no `AggregateProjection`; no grouping). §7.2
enumerates the four sub-cases.

The doc comment on `fillGroupingKeys` at
`internal/resolver/resolve.go:571-573` updates:

```go
// fillGroupingKeys populates Column.GroupingKey for branch 0's final Part per
// §4.5.2. hasAggregate gate: at least one AggregateProjection in Returns.
// Discrimination posture (ADR 0008 amendment 2026-07-06): ExprProjection is
// a grouping key iff ContainsAggregate() == false.
```

The R5 `// Uniform-exclude posture: ExprProjection is NEVER a
grouping key.` line RETIRES.

### 7.2 The four sub-cases the widening handles

| Part.Returns | ExprProjection.ContainsAggregate | fillGroupingKeys behaviour |
|---|---|---|
| No AggregateProjection, no ExprProjection | n/a | `hasAggregate=false` gate returns early — no keys marked. Unchanged from R5. |
| No AggregateProjection, ExprProjection(s) present | either | `hasAggregate=false` gate returns early — no keys marked. Unchanged from R5. Consistent with openCypher: grouping only fires when at least one aggregate is present. |
| AggregateProjection present, ExprProjection present, all ExprProjections have ContainsAggregate=true (e.g. `RETURN 1 + count(n), count(m)`) | true | ExprProjection excluded (aggregate residual is not a key). Behaviour matches R5 uniform-exclude. Golden UNCHANGED. |
| AggregateProjection present, ExprProjection present, at least one ExprProjection has ContainsAggregate=false (e.g. `RETURN 1 + n.age, count(n)`) | false | The non-aggregate ExprProjection is marked as a key. **Behaviour differs from R5.** Golden CHANGES for the new fixture (§7.4). |

Only sub-case 4 has a wire-observable behaviour change. Sub-case 3
matches by coincidence (both R5 and the widening exclude — the
widening's exclusion is honest, R5's is uniform). Sub-cases 1 and 2
are gated out before the widening branch runs.

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

### 7.4 New fixture — `aggregate_with_expr_grouping_key.cypher`

The R5-spec-recorded target fixture (`docs/specs/resolver-stage-r5.md
§4.5.3.5`), landed by this cycle.

**Fixture query** (`test/data/resolver/valid/aggregate_with_expr_grouping_key.cypher`):

```
MATCH (n:Person) RETURN 1 + n.age, count(n)
```

**Fixture schema mapping** — uses `social_r7.gql` (which extends
`social_r6.gql` per R7 §6.1 and defines a `Person` node with an
`age: INT` property). The mapping in
`test/data/resolver/valid/schema.mapping.json` gains one entry:

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
        "kind": "scalar",
        "scalar": "int"
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
against the widening table in §7.2 (sub-case 4).

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
(a) treat `ExprProjection.ContainsAggregate() == true` as inducing
grouping in the `hasAggregate` scan; (b) exclude such projections
from the grouping-key set. Search the 116-fixture enumeration for
each pre-widening path:

- `aggregate_with_expr_residual.cypher` — `MATCH (n:Person)
  RETURN 1 + count(n)`. One column; the Return has no
  `AggregateProjection` sibling (the aggregate is nested inside the
  ExprProjection, not lifted). **Pre-widening path**: the
  `hasAggregate` scan at `internal/resolver/resolve.go:583-589`
  finds no `AggregateProjection` in `part.Returns`, so
  `hasAggregate = false`; the function returns early at
  `resolve.go:590-592` and every column's `GroupingKey` stays at
  its default `false`. R5 §4.5.2's uniform-exclude of ExprProjection
  at `resolve.go:594-601` is NOT reached for this fixture. **Post-
  widening path**: `ExprProjection.ContainsAggregate() = true` on
  the sole column, so the widened `hasAggregate` scan flips to
  `true`; execution reaches the grouping-key loop; the ExprProjection
  is excluded on the ContainsAggregate discriminator. Different
  path, same result: `GroupingKey = false`. Golden byte-identical.
- `expr_projection_bool` — boolean expression, no aggregate, no
  AggregateProjection in Returns. Under widening
  `ContainsAggregate = false` → same early return at
  `resolve.go:590` as pre-widening (single-column Part with no
  aggregate present). Golden unchanged.
- `expr_projection_list` — list literal, no aggregate, no
  AggregateProjection in Returns. Same as above. Golden unchanged.
- `expr_projection_unknown` — an expression typing as TypeUnknown,
  no aggregate. Same as above. Golden unchanged.
- `literal_int_projection`, `literal_string_projection`,
  `literal_only_return` — LiteralProjection, not ExprProjection.
  Unaffected.
- Every other fixture — inspection confirms no
  ExprProjection whose ContainsAggregate would flip AND which
  co-occurs with an AggregateProjection sibling that would trigger
  the R5 uniform-exclude branch pre-widening. The two-path
  divergence above is `aggregate_with_expr_residual`-specific;
  every other fixture reaches the same grouping-key branch under
  both paths.

**The byte-identity fence check** (from the worktree, on the
widening PR's branch tip):

```
just test          # runs the full suite with -update absent
git diff test/data/resolver/valid/*.validated.golden.json
```

The diff MUST show:

- Exactly ONE new file:
  `aggregate_with_expr_grouping_key.cypher.validated.golden.json`.
- Exactly one modification to
  `schema.mapping.json` (the mapping entry for the new fixture).
- Zero modifications to any other `.validated.golden.json`.

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
2. **Unfreeze PR** — model + parser + ADR 0008 amendment +
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
   - `docs/adr/0008-query-model-freeze-resolver-api.md` — the
     amendment note and the "Known deferred additions" edit.
   - `internal/resolver/*` — untouched. The 116 resolver
     `.cypher.validated.golden.json` fixtures stay byte-identical
     (no wire change in `ValidatedQuery`; the new field lives only
     on `query.Query`'s ExprProjection JSON, which the resolver
     does not marshal — verified in §4.1.3).
   - `just test` from the worktree: PASSES.
   - `just lint-new` from the worktree: PASSES.
3. **Resolver-widening PR** — after PR 2 merges to master. Changes
   `internal/resolver/resolve.go:571-602` per §7.1; adds one fixture
   per §7.4; updates one doc comment per §7.1. Every pre-existing
   fixture stays byte-identical per §7.5. `just test` from the
   worktree: PASSES.

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

Confirmed: the resolver stays green through the unfreeze PR with the
field present but unconsumed.

---

## 9. Non-goals

The following are explicitly OUT OF SCOPE of the gqlc-hk0 campaign:

| Bead | Subject | Why out of scope |
|---|---|---|
| gqlc-fvo | Use→Part attribution across WITH — parameter Use type conflict silently admitted under any-valid-witness | Different axis (parameter Use vs projection classification); different frozen-model gap. Filed alongside R5 close-out. Independent unfreeze bead. |
| gqlc-0ig | (as filed, R7 close-out — verify against `bd show`) | Filed alongside R7. Separate unfreeze cycle. |
| gqlc-ay9 | R4 nullability under-approximation Class A — OPTIONAL-clause-sibling | Different subsystem (nullability, not grouping). Different frozen-model gap. |
| gqlc-5xg | R4 nullability under-approximation Class B — same-Part re-MATCH missing-witness | Different subsystem (nullability, not grouping). Different frozen-model gap. |
| **Codegen** | Consumer of `ValidatedQuery` — codegen ADR post-R7 | ADR 0008 §Consequences: codegen is downstream of the resolver; the resolver-widening PR delivers the correct grouping-key wire, and codegen consumes it under a future ADR. |
| **`ContainsAggregate` at ANY other Projection variant** | Adding the axis to `AggregateProjection`, `FuncProjection`, etc. | `AggregateProjection` IS an aggregate by construction — the bit would be a tautology. `FuncProjection` cannot contain an aggregate under the frozen model (`classifyFunction` at `expr.go:346-349` routes aggregate names to `AggregateProjection` before `FuncProjection` — verified verbatim). No other variant would benefit; no bead. |

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

**Frozen `ExprProjection` shape (§3.1, §4.1):**

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

- Freeze declaration: `docs/adr/0008-query-model-freeze-resolver-api.
  md:7-12`.
- Additive-only revision protocol:
  `docs/adr/0008-query-model-freeze-resolver-api.md:144-155`.
- `ContainsAggregate` in "Known deferred additions":
  `docs/adr/0008-query-model-freeze-resolver-api.md:166-169`.

**R5 uniform-exclude posture (§2, §3.7):**

- `docs/specs/resolver-stage-r5.md §4.5.3.1` (frozen model records
  the residual) at line 1255.
- `docs/specs/resolver-stage-r5.md §4.5.3.2` (R5 uniform-exclude
  posture; preserved-vs-violated split) at line 1301.
- `docs/specs/resolver-stage-r5.md §4.5.3.3` (unfreeze options —
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

This file exists on branch `unfreeze-hk0-spec` and is committed with
the message `docs(spec): unfreeze hk0 — ExprProjection.ContainsAggregate (Shape B)`.

`just test` from the worktree passes (docs-only cycle; nothing
depends on this file compiling).

`just lint-new origin/master` from the worktree passes (docs-only
cycle; no lint targets touched).

Follow-up PRs (Cycles 2 and 3) reference this brief as their
implementation contract. The bead `gqlc-hk0` moves out of
`in_progress` only when the resolver-widening PR (Cycle 3) merges;
the spec PR does not close it.
