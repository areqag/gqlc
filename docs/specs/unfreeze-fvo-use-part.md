# Model unfreeze — `Use → Part` attribution

The implementation brief for cycle **gqlc-fvo** of the model-unfreeze
campaign: an additive `part int` axis on every `Use` variant
(`PropertyUse`, `ExprUse`, `ClauseSlotUse`), populated parser-side
during `addParameterUse` from the branch-relative index of the
Part the Use lexically occurs in, closing the R5 §4.2.4
any-valid-witness soundness gap
(`docs/specs/resolver-stage-r5.md §4.2.4`) without touching the
frozen wire shape's zero-value semantics.

This brief is the **contract for the whole gqlc-fvo cycle**: it spans
the spec PR (this file), the unfreeze PR (model + parser +
ADR 0008 amendment + parser-test rebaseline; resolver untouched
and green), and the resolver-widening PR (`witnessAcrossScopes`
narrows from any-valid-witness to lexical-Part witness; new
discriminating invalid fixture + preserved byte-identity over
every pre-existing resolver golden). Both code PRs land under
ADR 0008's post-freeze revision protocol
(§Post-freeze revision protocol) — additive-only, dated amendment,
golden rebaseline whose diff shows only the new surface.

The four other unfreeze beads (`gqlc-hk0` — CLOSED,
`gqlc-0ig`, `gqlc-ay9`, `gqlc-5xg`) and codegen are **out of
scope** — this campaign closes fvo alone. See §9 for the non-goals
table.

---

## 1. Deliverables

Spec cycle (Cycle 1) — this PR:

- `docs/specs/unfreeze-fvo-use-part.md` — this file.

Unfreeze cycle (Cycle 2, follow-up PR):

- `internal/query/query.go` — one additive field
  `part int` on each of `PropertyUse`, `ExprUse`, `ClauseSlotUse`;
  one new constructor per variant carrying the field —
  `NewPropertyUseAt(ref, part)`, `NewExprUseAt(enclosing, position, part)`,
  `NewClauseSlotUseAt(slot, part)`; one new accessor per variant —
  `Part() int`. The existing `NewPropertyUse` / `NewExprUse` /
  `NewClauseSlotUse` constructors are preserved verbatim as the
  zero-value-safe shorthands (each delegates to the new
  constructor with `part=0`). §4.1.
- `internal/query/type_test.go` — three new
  `TestNew…UseAt` tests pinning the true-side axis wiring
  (constructor → accessor → JSON marshal-and-compare with a
  non-zero Part), plus four new zero-side tests filling the
  branch-base coverage gap: `TestNewPropertyUse`,
  `TestPropertyUseMarshalJSON`, `TestNewClauseSlotUse`,
  `TestClauseSlotUseMarshalJSON` (PropertyUse and ClauseSlotUse
  have no unit coverage at branch base — only ExprUse does; grep
  at `baba282`: `TestNewExprUse` at `internal/query/type_test.go:73`
  and `TestExprUseMarshalJSON` at `:93` are the only pre-existing
  Use unit tests). The two pre-existing ExprUse tests stay
  verbatim; the four new zero-side tests pin `part = 0` via
  key-absence under omit-when-zero. §4.1 and §5.
- `internal/query/cypher/expr.go:633` — `addParameterUse` gains
  one line: reading the current Part index
  `l.currentPartIndex()` before appending, and wrapping the
  incoming `Use` in a variant-preserving `attribute` step that
  returns the same variant with its `part` field populated.
  A new package-private helper `(l *listener) currentPartIndex() int`
  returns `len(l.curBranch.parts) - 1` (the invariant `curBranch`
  and `curPart` are non-nil at every `addParameterUse` call site is
  proven in §4.2.1). Every existing `addParameterUse` call site
  stays verbatim — none of them needs to change. §4.2.
- `internal/query/cypher/parser_test.go` — the parser test pins
  that assert `Use` shapes remain byte-for-byte identical under
  `require.Equal`: 18 pins carry Uses in Part 0 (Go
  struct zero-value equality — a `PropertyUse{ref, 0}` equals
  `PropertyUse{ref, part: 0}` bit-for-bit under `reflect.DeepEqual`).
  The count of 18 is corroborated by
  `grep -c "NewPropertyUse\|NewExprUse\|NewClauseSlotUse" internal/query/cypher/parser_test.go`
  at branch base `baba282`. §4.3 enumerates every pin; §4.4 covers
  the golden-corpus rebaseline.
- `docs/adr/0008-query-model-freeze-resolver-api.md` — one dated
  amendment note (top of the file, ADR 0003 stage-note convention)
  recording the `Use → Part` axis's adoption. The "Known deferred
  additions" list gains a new closed-out entry for
  `Use.Part` (mirroring the hk0 close-out entry's format).
  Verbatim text pinned in §6.
- Parser goldens (`*.golden.json` in `internal/query/cypher/testdata/golden/`)
  rebaseline **for zero fixtures** — under omit-when-zero-value and
  the branch-base corpus's Part-emission distribution (§4.4.1),
  no Use in the 22 goldens carrying Uses at branch base emits from
  a Part index ≥ 1. Every golden is byte-for-byte identical.
  §4.4 pins the fence.

Resolver-widening cycle (Cycle 3, follow-up PR after the unfreeze PR
merges):

- `internal/resolver/resolve.go:750-811` — `witnessAcrossScopes`
  narrows: a `PropertyUse` witnesses in **exactly one** Part scope —
  the one indexed by `u.Part()` — instead of iterating every
  containing scope and unifying. If that scope does not contain the
  Ref's variable, or contains it but the property lookup fails, the
  error surfaces immediately (no swallowing). §7.1.
- `internal/resolver/resolve.go:20-53` — `resolve` widens
  `useSitesToScopes` to route per-branch: the top-level
  `mergedScopes` is now `[][]partScope` (indexed as
  `[branchIndex][partIndex]`) so a Use in a UNION branch is
  witnessed against its OWN branch's Part scopes, not the flat
  cross-branch concatenation R5 uses. §7.2.
- `internal/resolver/resolve.go:706-748` — `unifyParameterUsesAcrossScopes`
  widens to accept the branch-relative scope table; the R2 lattice
  unification across Uses is unchanged in semantics. §7.2.
- `docs/specs/resolver-stage-r5.md §4.2.4` — the
  "any-valid-witness" prose retires; a "post-fvo lexical-Part
  witness" successor prose lands (§7.3, pinned verbatim).
- One new resolver invalid fixture: `parameter_across_with_alias_shadow_reversed.cypher`
  (§7.4). This fixture is the mirror of the branch-base
  `parameter_across_with_alias_shadow.cypher` valid fixture
  (§7.4.1 pins the shape). At branch base its shape is silently
  admitted by any-valid-witness; post-widening it fires
  `ErrUnknownProperty`. This is the sole wire-observable
  behaviour change; every pre-existing resolver-valid golden is
  byte-identical (§7.5).
- Every pre-existing resolver-valid golden is byte-identical after
  the widening PR (§7.5). The fence check runs from the worktree:
  `just test` without `-update` over the 118-fixture-pair R0–R7
  corpus at branch base
  (`ls test/data/resolver/valid/*.cypher | wc -l` = 118). Only the
  one new invalid fixture appears in the diff.

Nothing downstream of the resolver is built — the resolver's widening
lands the corrected parameter-Use attribution semantics; codegen
consumes them under a future ADR.

---

## 2. Frame — what changes and what stays

R5 shipped with a documented under-approximation: every `PropertyUse`
whose Ref's variable is bound in ≥ 2 Part scopes is witnessed
against **every containing scope** and the successful witnesses are
unified via the R2 lattice, with per-scope `ErrUnknownProperty`
swallowed. R5's honest statement of the residual is
`docs/specs/resolver-stage-r5.md §4.2.4` and its recorded resolver
witness `internal/resolver/resolve.go:750-811`
(`witnessAcrossScopes`). The R5 §7.1 audit table (line 2333) pins
the residual: **"Cross-Part parameter Use where the true attributed
Part would reject but another same-name Part admits — Use→Part
attribution gap (§4.2.4): silently false-admitted under
any-valid-witness (parameter type is the unified valid witnesses)"**.
The recorded remediation is a model unfreeze — this cycle.

ADR 0008's "Known deferred additions" registered several axes; this
cycle's is the successor to R5's escape hatch (per R7 §7.1.1
close-out: gqlc-fvo is "the parent of the R7 CALL-arg attribution
gap"). Compared to hk0's `ContainsAggregate` axis (a bool on ONE
variant), this cycle adds a positional index (an int) on THREE
variants — the same `Use` sum. The larger surface is warranted:
`Use` is a sealed sum of exactly three variants, and the
attribution axis is a variant-independent property (every Use has
a lexical Part). Splitting the axis across two of the three would
leave a gap the widened resolver could not close.

**What does not change.** The wire shape's zero-value semantics: a
`Use` whose lexical Part is Part 0 (the majority: every
single-Part query, and every Use in the first Part of a multi-Part
branch) continues to serialise to a JSON blob whose `part` field is
absent. Every parser-test pin whose Use value has Part 0 today is
bit-for-bit identical to its post-widening shape (Go struct
zero-value equality; §4.3). Every existing consumer that
constructs `NewPropertyUse(ref)` / `NewExprUse(t, pos)` /
`NewClauseSlotUse(slot)` gets the same `Part = 0` semantics — the
existing constructors are preserved unchanged.

**What does change.** Zero parser-test pins rebaseline
(§4.3: every use in the parser test corpus lives in Part 0). Zero
parser goldens rebaseline (§4.4.1: every Use in the 22 branch-base
goldens with Uses lives in Part 0). The wire JSON of every Use
gains one field: `part` (omit-when-zero; §4.1.3 records the
convention). The ADR 0008 amendment records the axis's adoption.
The resolver's `witnessAcrossScopes` narrows from any-valid-witness
to lexical-Part witness (§7.1).

**Why one axis on three variants — the alternative shapes considered.**

- **Axis on `PropertyUse` only** (the sole variant R5's ambiguity
  affects on the witnessing side). ClauseSlotUse and ExprUse
  witness Part-agnostically — a `ClauseSlot → INT` and an
  `ExprUse.EnclosingType → resolveType(t)` do not consult any
  Part's binding tables (`internal/resolver/resolve.go:794-807`).
  Under this shape, the axis lands only where the resolver reads
  it — a smaller change. **REJECTED**: the axis is a lexical-
  attribution property of the Use record, not a resolver-consumption
  property. `ExprUse` inside a WHERE-after-WITH lives lexically in
  the CLOSED Part (see §3.3 emission-site table) — that fact is
  recorded on the record even if the R5 resolver would not consult
  it, and a future R-later stage that widens the resolver to
  discriminate `ExprUse` positions per Part (e.g. per §7.6's
  gqlc-lta family) would need a re-unfreeze cycle otherwise. One
  cycle, one axis, three variants: closes the sum.
- **Axis on `Use` at the interface level (a `Use.Part()` method,
  each variant implementing it).** Same wire and same consumption;
  the difference is Go layout. **REJECTED**: adding a method to
  the `Use` interface breaks the sealed-sum discipline (the
  interface currently exports one method, `isUse()`). Adding
  `Part() int` would force every future variant author to implement
  it, tying the sum's arity to the axis's presence. Per-variant
  field + accessor keeps the interface sealed and lets each variant
  own its axis independently.
- **A separate `PartAttribution` record mapping parameter name +
  Use index → Part** (kept out of the Use sum). **REJECTED**: a
  parallel index doubles the wire's decode surface — every Use
  consumer must join two tables. The Use-record additive axis is
  the smallest, most localised shape.

Chosen: **additive `part int` field on all three `Use` variants,
constructor + accessor per variant, omit-when-zero JSON encoding**.
Justified against the hk0 house convention (`omit-when-zero-value`
for post-freeze additive axes) with a divergence-audit in §4.1.3
covering the semantic difference between "0 = no aggregate"
(hk0's bool) and "0 = Part 0" (fvo's int).

---

## 3. Mining — what the frozen model records today

Every claim in §4 rests on citations here. Re-verify each file:line
at branch base `origin/master @ baba282` before writing code — line
numbers can drift on merge.

### 3.1 `Use` — the frozen sum

The interface (`internal/query/query.go:1302-1312`):

```go
type Use interface {
    isUse()
}
```

**One marker method, closed sum of three variants**:
`PropertyUse` (`query.go:1314-1335`), `ExprUse` (`query.go:1420-1447`),
`ClauseSlotUse` (`query.go:1460-1476`).

#### 3.1.1 `PropertyUse` — verbatim

```go
type PropertyUse struct {
    ref Ref // the binding property the parameter sits against
}

func NewPropertyUse(r Ref) PropertyUse {
    return PropertyUse{ref: r}
}

func (u PropertyUse) Ref() Ref { return u.ref }
func (PropertyUse) isUse() {}
```

`internal/query/query.go:1319-1335` — one unexported field, one
constructor, one accessor.

`MarshalJSON` (`internal/query/query.go:1558-1564`):

```go
func (u PropertyUse) MarshalJSON() ([]byte, error) {
    return json.Marshal(struct {
        Kind     string `json:"kind"`
        Variable string `json:"variable"`
        Property string `json:"property"`
    }{Kind: useKindProperty, Variable: u.ref.Variable, Property: u.ref.Property})
}
```

Three always-emit fields (`kind` + flattened Ref).

#### 3.1.2 `ExprUse` — verbatim

```go
type ExprUse struct {
    enclosingType Type
    position      ExprPosition
}

func NewExprUse(enclosing Type, position ExprPosition) ExprUse {
    return ExprUse{enclosingType: enclosing, position: position}
}

func (u ExprUse) EnclosingType() Type { return u.enclosingType }
func (u ExprUse) Position() ExprPosition { return u.position }
func (ExprUse) isUse() {}
```

`internal/query/query.go:1428-1447` — two unexported fields, one
constructor, two accessors.

`MarshalJSON` (`internal/query/query.go:1452-1458`):

```go
func (u ExprUse) MarshalJSON() ([]byte, error) {
    return json.Marshal(struct {
        Kind          string `json:"kind"`
        EnclosingType Type   `json:"enclosingType"`
        Position      string `json:"position"`
    }{Kind: useKindExpr, EnclosingType: projectionType(u.enclosingType), Position: u.position.String()})
}
```

Three always-emit fields.

#### 3.1.3 `ClauseSlotUse` — verbatim

```go
type ClauseSlotUse struct {
    slot ClauseSlot
}

func NewClauseSlotUse(s ClauseSlot) ClauseSlotUse {
    return ClauseSlotUse{slot: s}
}

func (u ClauseSlotUse) Slot() ClauseSlot { return u.slot }
func (ClauseSlotUse) isUse() {}
```

`internal/query/query.go:1463-1476` — one unexported field, one
constructor, one accessor.

`MarshalJSON` (`internal/query/query.go:1569-1574`):

```go
func (u ClauseSlotUse) MarshalJSON() ([]byte, error) {
    return json.Marshal(struct {
        Kind string `json:"kind"`
        Slot string `json:"slot"`
    }{Kind: useKindClauseSlot, Slot: u.slot.String()})
}
```

Two always-emit fields.

#### 3.1.4 Existing unit test witnesses

At branch base `baba282`, the Use sum has partial unit coverage.
The exact enumeration (fresh
`grep -n "TestNewPropertyUse\|TestNewExprUse\|TestNewClauseSlotUse\|TestPropertyUseMarshalJSON\|TestExprUseMarshalJSON\|TestClauseSlotUseMarshalJSON" internal/query/type_test.go`):

- `TestNewExprUse` at `internal/query/type_test.go:73` — pins
  `NewExprUse(TypeInt, ExprInProjection)` and its accessors.
- `TestExprUseMarshalJSON` at `internal/query/type_test.go:93` —
  pins the wire encoding
  `{"kind":"expr","enclosingType":"int","position":"projection"}`.

That is the complete pre-existing set. `PropertyUse` and
`ClauseSlotUse` have **no** dedicated constructor or
`Marshal…JSON` unit tests at branch base — their wire shape is
witnessed only transitively through parser-test pins in
`internal/query/cypher/parser_test.go` (§4.3). The unfreeze PR
adds four new zero-side unit tests (§5.1) to bring
PropertyUse and ClauseSlotUse to parity before the widened
constructors ship. Both pre-existing ExprUse tests stay verbatim
under the omit-when-zero ruling (§4.1.3, §5).

### 3.2 The Part construction lifecycle — the emission-time index invariant

This is the load-bearing invariant: **at every `addParameterUse`
call site, `len(l.curBranch.parts) - 1` is the Part index of the
Use's lexical enclosing Part**. §4.2 depends on this. The mining:

**Branch open** (`internal/query/cypher/listener.go:226-235` +
`:588-599`). `EnterOC_SingleQuery` — the regular-query entry point —
appends a fresh `rawPart` to a fresh `rawBranch` and points both
`curBranch` and `curPart` at the just-created values. The
grammar-quirk carve-out at `EnterOC_StandaloneCall`
(`:588-599`) does the same primer for the pure standalone-CALL
path when `EnterOC_SingleQuery` never fires. Post-open invariant:
`len(l.curBranch.parts) == 1`; `curPart` points at `parts[0]`.

**WITH boundary** (`internal/query/cypher/listener.go:278-294`).
`EnterOC_With` runs, in strict order:

1. `l.collectProjection(c.OC_ProjectionBody())` — every parameter
   inside the projection body (WITH item, ORDER BY, SKIP, LIMIT)
   is mined against the CURRENT `curPart` (the Part being closed).
2. `if w := c.OC_Where(); w != nil { l.mineWhere(w) }` — every
   parameter inside the trailing WHERE is mined against the SAME
   `curPart` (still the Part being closed).
3. `l.curBranch.parts = append(l.curBranch.parts, part); l.curPart = part`
   — the swap. `parts` is now length K+1; `curPart` points at
   `parts[K]` (the NEW part).

Post-WITH invariant: `len(l.curBranch.parts) == K+1` (K is the
just-closed Part's index — `parts[K]` is the new part, so its index
is K, i.e. the return of `len(parts) - 1`).

**RETURN boundary** (`internal/query/cypher/listener.go:602-610`).
`EnterOC_Return` runs `collectProjection` against the current
`curPart` and does NOT append a new Part. The RETURN's Uses are
mined into the FINAL Part. Post-RETURN invariant: `curPart` and
`curBranch.parts` unchanged; Uses attributed to the final Part.

**UNION boundary** (`internal/query/cypher/listener.go:243-252`).
`EnterOC_Union` appends a `UnionKind` to `l.combinators` (no
Part/Branch mutation). `EnterOC_SingleQuery` fires next for the
new branch, opening a fresh `curBranch` (a fresh `parts` slice)
and a fresh `curPart` (parts[0]).

**EXISTS subquery** (`internal/query/cypher/listener.go:627-645`).
`EnterOC_ExistentialSubquery` increments `subqueryDepth`; every
`Enter*` handler that collects into `curPart` early-returns while
positive. `subqueryDepth > 0` implies clause collection is
suppressed — but parameter mining still fires at entry
(`internal/query/cypher/listener.go:629-638`), which calls
`addParameterUse` with `curPart` pointing at the OUTER Part. The
attribution is to the OUTER Part where the EXISTS lexically
sits — sound: the outer parser's `curPart` is Part K where the
outer projection/predicate carries the `EXISTS { … }`.

**All emission points reach `addParameterUse`** via one of the
sites at §3.3; §3.3 tabulates each with its emission-time
`curPart` context.

### 3.3 The 18 `addParameterUse` call sites — Part-index knowability

Enumeration of every call site (fresh `grep -n addParameterUse
internal/query/cypher/` at branch base `baba282`, excluding the
definition at `expr.go:633` and the doc-comment at `:627`), each
cross-checked against the Part lifecycle at §3.2. Every site is
inside an `Enter*` handler (or a mining helper invoked from one)
whose `curPart` is guaranteed non-nil by the priming discipline at
§3.2. Consequently `len(l.curBranch.parts) - 1` is a well-defined
non-negative int at every site.

The table below has 16 rows, but two rows (`expr.go:591 / 607` and
`expr.go:677 / 707`) each represent two adjacent call sites in the
same helper, so the row count reads 16 while the site count is 18
— the count the section title advertises.

| # | File | Line | Enclosing handler / helper | Use variant | Part index knowable? |
|---|---|---|---|---|---|
| 1 | `listener.go` | 499 | `EnterOC_Delete` — DELETE target expression parameter | `NewExprUse(TypeUnknown, ExprInDeleteTarget)` | **yes** — Delete is a Part-collecting handler with `curPart` = the Part containing the DELETE |
| 2 | `listener.go` | 637 | `EnterOC_ExistentialSubquery` — parameter inside an EXISTS body | `NewExprUse(TypeBool, ExprInPredicate)` | **yes** — the outer Part index (the EXISTS lexically sits in the outer projection/predicate); `curPart` at entry is the outer Part |
| 3 | `call.go` | 64 | `enterInQueryCall` / `enterStandaloneCall` — CALL argument parameter (rich-expression path) | `NewExprUse(t, ExprInProjection)` | **yes** — CALL clauses are Part-collecting handlers; `curPart` = the Part containing the CALL |
| 4 | `call.go` | 164 | `enterInQueryCall` — CALL WHERE-on-YIELD parameter | `NewExprUse(TypeBool, ExprInPredicate)` | **yes** — same Part as (3) |
| 5 | `typing.go` | 445 | `typeQuantifier` — quantifier source-list parameter (mined inside typing walk) | `NewExprUse(sourceType, ExprInPredicate)` | **yes** — the typing walk runs inside a `collectReturnItem` / `mineWhere` / `collectSetItem` / `collectDeleteItem` frame; `curPart` is unchanged during the walk |
| 6 | `typing.go` | 458 | `typeQuantifier` — quantifier WHERE-body parameter | `NewExprUse(TypeBool, ExprInPredicate)` | **yes** — same frame as (5) |
| 7 | `typing.go` | 873 | `classifyRichExpression` — rich-expression projection parameter | `NewExprUse(t, ExprInProjection)` | **yes** — called from `collectReturnItem`; `curPart` = the Part containing the RETURN / WITH |
| 8 | `expr.go` | 131 | `collectUnwind` (`:87`) — UNWIND source-expression residual parameter | `NewExprUse(sourceType, ExprInProjection)` | **yes** — `collectUnwind` is a `curPart`-mutating collection helper called from `EnterOC_Unwind`; `curPart` is the Part the UNWIND lexes in |
| 9 | `expr.go` | 175 | `mineSortItemParameters` (`:163`) — ORDER BY sort-item residual parameter | `NewExprUse(TypeUnknown, ExprInProjection)` | **yes** — called from `collectProjection`, which runs against `curPart` before the WITH-swap (§3.2 step 2) |
| 10 | `expr.go` | 403 | `classifyAggregateCall` (`:380`) — aggregate-argument parameter | `NewExprUse(resultType, ExprInProjection)` | **yes** — reached via `collectReturnItem` → `classifyProjection`; `curPart` = the Part containing the RETURN / WITH |
| 11 | `expr.go` | 419 | `mineClauseSlotParameter` (`:414`) — SKIP / LIMIT slot parameter | `NewClauseSlotUse(slot)` | **yes** — called from `collectProjection` (`expr.go:70` for SKIP, `:73` for LIMIT), which runs against `curPart` before the WITH-swap (§3.2 step 2) |
| 12 | `expr.go` | 458 | `mineWhere` (`:444`) — WHERE-body residual parameter | `NewExprUse(t, ExprInPredicate)` | **yes** — WHERE is Part-attached; `curPart` is the Part the WHERE belongs to (§3.2 shows the WITH-attached WHERE is attributed to the CLOSED Part) |
| 13 | `expr.go` | 535 | `pairAddSub` (`:529`) — a → b arm: `ref` from operand `a`, `param` from operand `b`; emits `PropertyUse{Ref{ref.Variable, ref.Property}}` on `param` | `NewPropertyUse(Ref{Variable, Property})` | **yes** — `pairAddSub` is reached from `mineComparisons` / `mineStringPredicate`, both invoked from `mineWhere` (Part-attached; see row 12) or `mineComparisons` under a MATCH pattern predicate context. `curPart` is the Part containing the predicate |
| 14 | `expr.go` | 542 | `pairAddSub` (`:529`) — b → a arm: `ref` from operand `b`, `param` from operand `a`; emits `PropertyUse{Ref{ref.Variable, ref.Property}}` on `param` | `NewPropertyUse(Ref{Variable, Property})` | **yes** — same call context as (13) |
| 15 | `expr.go` | 591 / 607 | `mineInlineMap` (`:570`) — inline property-map `PropertyUse`: `:591` fast path (bare `$param` value); `:607` widening path (rich value whose typing walk surfaces nested parameters) | `NewPropertyUse(Ref{Variable, Property})` | **yes** — `mineInlineMap` is reached from pattern-collection helpers; `curPart` is the Part whose MATCH / MERGE contains the pattern |
| 16 | `expr.go` | 677 / 707 | `collectSetItem` (`:662`) — SET-value parameter | `NewExprUse(valueType, ExprInSetValue)` | **yes** — SET is Part-attached (Part containing the SET clause) |

**No emission site sits outside a `curPart`-in-scope frame.** The
proof: every site is reached via one of the `Enter*` handlers
enumerated in §3.2 (`Match`, `With`, `Return`, `In/StandaloneCall`,
`Create`, `Merge`, `Delete`, `Set`, `Remove`, `Unwind`,
`ExistentialSubquery`). Each of those handlers is guarded by the
priming discipline (`EnterOC_SingleQuery` /
`EnterOC_StandaloneCall`) that establishes `curBranch` and
`curPart` non-nil BEFORE any collection handler fires. The nil
check at `listener.go:592` is a belt-and-braces guard against a
grammar-quirk path that pre-dates the standalone-CALL primer;
under the current grammar every collection handler runs under a
non-nil `curPart`.

**Consequence.** `len(l.curBranch.parts) - 1` is knowable at every
`addParameterUse` call site and equals the branch-relative index
of the Part the Use is lexically attributed to.

### 3.4 The R5 witness machinery — where the resolver widening lands

`internal/resolver/resolve.go:750-811` (verbatim):

```go
// witnessAcrossScopes produces one witness per Part whose scope contains the
// Use's Ref (for a PropertyUse), or exactly one witness for a Part-agnostic
// Use (ClauseSlot / ExprUse). An unattributed PropertyUse (no scope contains
// its Ref) returns zero witnesses — the unifier treats this as ResolvedUnknown.
//
// PropertyUse semantics (§4.2.4 any-valid-witness rule): the wire carries no
// Use→Part attribution, so a Ref like `a.title` may name `a` in several Parts
// (e.g. `MATCH (a:Person) WITH a.name AS a MATCH (a:Post) …` — after an
// alias-export shadow, Part 0's `a` is Person and Part 1's `a` is Post; or a
// UNION where two branches each bind `a` to a different type). We attempt the
// witness in EVERY scope containing the Ref's variable, collect only the
// SUCCESSFUL witnesses, and let the caller unify them via the R2 lattice. A
// per-scope ErrUnknownProperty is swallowed: the true attributed Part may be
// a different one that succeeds. Only when EVERY containing scope fails the
// property lookup do we surface the last such error (a genuine unknown-
// property fault). Non-property faults (ErrOutOfR0Scope for out-of-scope
// edge Refs, var-length edge property projections) surface immediately —
// they are structural, not scope-dependent.
func witnessAcrossScopes(u query.Use, scopes []partScope, s schema.Schema) ([]ResolvedType, error) {
    switch uu := u.(type) {
    case query.PropertyUse:
        ref := uu.Ref()
        out := make([]ResolvedType, 0, 1)
        var lastPropErr error
        containing := 0
        for _, sc := range scopes {
            if !scopeContains(sc, ref.Variable) {
                continue
            }
            containing++
            w, err := propertyUseWitness(ref, sc.nodeTypes, sc.edgeTypes, sc.edgeCands, sc.edgeBindings, sc.nullableBinding, s)
            if err != nil {
                if errors.Is(err, ErrUnknownProperty) {
                    lastPropErr = err
                    continue
                }
                return nil, err
            }
            out = append(out, w)
        }
        if containing > 0 && len(out) == 0 && lastPropErr != nil {
            return nil, lastPropErr
        }
        return out, nil
    case query.ClauseSlotUse:
        return []ResolvedType{ResolvedScalar{Kind: ScalarInt}}, nil
    case query.ExprUse:
        // ...
    }
}
```

The `PropertyUse` arm is where the widening bites. Post-widening
shape: witness once against `scopes[u.Part()]` if the scope
contains the Ref's variable; if not, or if the property lookup
fails on that one scope, the error surfaces immediately (no
`ErrUnknownProperty` swallowing). §7.1 pins the exact widened
body.

The `ClauseSlotUse` and `ExprUse` arms currently ignore Part —
their witness is Part-agnostic (INT for skip/limit; resolveType of
the enclosing type for ExprUse). The widening leaves both arms
unchanged in semantics; §7.1 documents the deliberate no-op for
the two Part-agnostic variants (the axis lives on their records
for lexical-attribution completeness, not for R5 resolver
consumption; §7.6 flags follow-up beads that may consume it).

### 3.5 The cross-branch scope-flattening — why the resolver top-level widens too

`internal/resolver/resolve.go:20-53` (relevant lines):

```go
var mergedScopes []partScope
for b, branch := range q.Branches {
    cols, uses, err := resolveBranch(branch, s, r)
    if err != nil {
        return ValidatedQuery{}, err
    }
    branchCols[b] = cols
    mergedScopes = append(mergedScopes, useSitesToScopes(uses)...)
}
if err := compareBranchColumns(branchCols); err != nil {
    return ValidatedQuery{}, err
}
params, err := unifyParameterUsesAcrossScopes(q.Parameters, mergedScopes, s)
```

`mergedScopes` is the FLAT concatenation of every branch's Part
scopes. Under `witnessAcrossScopes`'s any-valid-witness rule this
is safe — the walk over `scopes` visits every branch's Parts
uniformly, and a UNION-branched query with cross-branch
same-named bindings would witness in every containing scope
regardless of branch.

Under lexical-Part witness (§7.1), the flat concatenation is
UNSOUND: `u.Part()` is a branch-relative index. If the query has
two branches with 2 Parts each and a Use whose lexical branch is
branch 1 and lexical Part is Part 0, `u.Part() = 0`; but
`mergedScopes[0]` under the flat concat is branch 0's Part 0. The
widened witness would consult the wrong scope.

§7.2 pins the widening: `mergedScopes` becomes a two-dimensional
`[][]partScope`, indexed as `[branchIndex][partIndex]`. Every Use
witnesses against its own branch's scopes only. **The Use variant
does not carry a branch index** — the axis this cycle adds is
`part int`, not `(branch int, part int)`. The branch index is
recovered at resolver runtime by walking the branches in order and
noting which branch's `resolveBranch` call produced which Use
site; the widened `unifyParameterUsesAcrossScopes` receives a
`[][]partScope` and dispatches on the Use's `part` index within
the branch identified by the parameter walk's own bookkeeping.

**Why not add a `branch int` axis too.** At branch base, zero of the
22 goldens with Uses carry Uses in a UNION branch (§4.4.1: no
UNION-with-Uses goldens exist in the corpus). No branch-base
fixture would exercise the axis. Adding it speculatively violates
ADR 0008's "don't design for hypothetical future requirements"
posture. The resolver-side branch bookkeeping (§7.2) is a
one-function change and does not add wire surface. If a future
corpus scenario introduces a UNION-with-Uses shape that demands
branch attribution on the wire itself (rather than derived from
the parameter walk), that lands under a NEW unfreeze bead.

### 3.6 `EdgeBinding.directed` and `ExprProjection.containsAggregate` — the axis precedents

**Pre-freeze always-emit precedent.** `EdgeBinding.directed`
(Stage 5) added a boolean axis with an always-emit JSON default
on a frozen sum variant. It emits `"directed": true` or
`"directed": false` on every edge binding golden, unconditionally.
Layout: field (`internal/query/query.go:378`), constructor
(`:388-393`), accessor (`:445-447`), always-emit JSON
(`:1497-1508`).

**Post-freeze omit-when-zero precedent.** `ExprProjection.containsAggregate`
(gqlc-hk0, `docs/specs/unfreeze-hk0-containsaggregate.md`, merged
2026-07-06) added a boolean axis under
the post-freeze revision protocol with an `omit-when-false` JSON
tag. It emits `"containsAggregate": true` only when the
expression subtree contains an aggregate call; the false zero
value is absent. The ADR 0008 amendment recorded the campaign
convention: **"post-freeze additive axes emit
omit-when-zero-value, deliberately diverging from the pre-freeze
always-emit precedent"**. This cycle FOLLOWS that convention —
§4.1.3 pins the encoding — and §4.1.3.1 justifies the divergence
from `directed`'s always-emit posture on the same
1-key-per-Use / cross-corpus rebaseline argument hk0 made.

**How `Use.Part` follows the hk0 precedent — and where it does not.**

- **Field** — same, unexported `part int` on each variant.
  §4.1.1.
- **Constructor** — DIVERGES from hk0's ONE new constructor
  (`NewExprProjectionWithAggregate`) by adding THREE new
  constructors (`NewPropertyUseAt`, `NewExprUseAt`,
  `NewClauseSlotUseAt`). Rationale: three variants each with an
  additive axis; the constructor overload count scales with the
  variant count. Existing constructors preserved verbatim.
  §4.1.2.
- **Accessor** — same, one accessor per variant: `Part() int`
  identically named on each. §4.1.2.
- **JSON encoding** — same, **omit-when-zero** (`,omitempty`).
  §4.1.3 justifies against the fence-diff requirement and the
  Part-0 corpus dominance (every branch-base Use lives in Part 0).

#### 3.6.1 The zero-value hazard — Part 0 vs "no field"

**The design hazard central to this cycle.** hk0's
`containsAggregate: bool` had a natural semantic default: `false`
= "no aggregate". Under omit-when-false the key is absent iff the
default holds; the decoder that treats absence as `false` is
semantically correct — the axis was never in play.

`part int` does not have that free-lunch symmetry. Part indices
start at 0. `part: 0` is a MEANINGFUL value — "Part 0", the
first (and often only) Part of the enclosing branch. Under
omit-when-zero, the wire encoding of "Use in Part 0" is
indistinguishable from a legacy record (no `part` key). The
decoder MUST treat absence as `part: 0`, not as "unattributed" or
"unknown".

**Is that sound?** For every consumer at this cycle's target
codebase — **yes**, by construction. The two consumer paths:

1. **The resolver** (§7.1). Post-widening,
   `witnessAcrossScopes` reads `u.Part()`, indexes
   `scopes[u.Part()]`, witnesses. When `u.Part() == 0` (the zero
   default), the witness runs against the branch's Part 0 scope.
   For a Use whose lexical Part IS Part 0, that is exactly the
   correct scope. For a Use decoded from a LEGACY golden (no
   `part` key), the decoder produces `u.Part() == 0`; the
   resolver witnesses against Part 0. Legacy goldens at branch
   base carry Uses only in Part 0 (§4.4.1); the "legacy default"
   IS the correct attribution for every branch-base Use.
2. **Codegen** — no consumer today reads `Use.Part()` under
   ADR 0008. Codegen consumes the resolver's `ValidatedQuery`,
   which does not embed `Use.Part` on its wire (verified in §7.5:
   `ValidatedQuery.MarshalJSON` walks `Columns` and `Parameters`
   only; `Parameters` carries a resolved type, not the raw Use
   record).

**Alternative encodings considered.**

- **`part int` always-emit** (mirror of `directed`'s pre-freeze
  posture). Every Use record gains a `"part": <n>` field
  unconditionally. Under this encoding, at branch base 31 Uses
  across 22 goldens gain the key with value 0 — 22 goldens
  rebaseline. Compare with omit-when-zero: 0 goldens rebaseline.
  **Rejected** — the hk0 amendment's campaign convention rules
  this out: post-freeze additive axes emit omit-when-zero-value.
  §4.1.3 pins the specific accounting.
- **`part *int` (pointer, omit-when-nil)** — encodes "unattributed"
  explicitly as `null`. Under this shape a legacy golden could be
  distinguished from an attributed Use. **Rejected** — the axis
  is TOTAL: every emission site produces a valid Part index
  (§3.3, all 18 sites are inside a `curPart`-in-scope frame).
  There is no such thing as an "unattributed" Use produced by the
  parser. A nullable encoding would express an
  impossible-by-construction state on the wire, and force every
  consumer to handle the nullable case — the exact "adding
  half-finished implementations" anti-pattern the project
  prohibits (CLAUDE.md).
- **`part int` with sentinel `-1` for "unattributed"** — same
  hazard as `*int` with a worse type. **Rejected** for the same
  reason.
- **`part int` always-emit + rename existing constructors to
  require Part explicitly** — the hk0 spec §3.6 called this out
  as the Stage-5 house style (`NewEdgeBinding` required
  `directed`). **Rejected** post-freeze: rewriting every
  parser-test pin's `NewPropertyUse` / `NewExprUse` /
  `NewClauseSlotUse` call to add a positional `0` would break
  the byte-identity of 18 assertions for no semantic gain, and
  the Preserved-constructor precedent from hk0's own
  `NewExprProjection` remains the house rule.

Chosen: **omit-when-zero on all three variants; decoder treats
absence as `part: 0`; the "Part 0 default" is sound for every
branch-base consumer because every legacy Use IS in Part 0**.

**A note on decoding.** No `UnmarshalJSON` exists on any Use
variant at branch base — Uses are minted by the parser and
consumed only by the resolver, entirely in-process; the wire shape
is a golden-fixture read-target for the parser-test harness, not
a boundary a decoder needs to cross. `ValidatedQuery`
(§7.5) does not embed raw Uses either — it carries `[]ResolvedParameter`
whose per-parameter Type is precomputed. So the
"absence ⇒ `part = 0`" rule this section states is a
FORWARD-COMPATIBILITY convention for a future consumer that adds
decode paths (e.g. an out-of-process resolver reading persisted
parser output), not a decoder that exists today. The primary
justification for the encoding choice remains golden
byte-identity at branch base (§4.4.1: zero goldens rebaseline
because every branch-base Use is in Part 0 and omit-when-zero
serialises Part 0 as key-absent). When a decode path is added,
the convention gives that path a well-defined answer for the
legacy shape without a wire migration.

### 3.7 R5's audit note — what this cycle closes

`docs/specs/resolver-stage-r5.md §4.2.4` records:

- **The idealised rule** (line 862-865): the resolver witnesses a
  parameter Use against Part K's binding tables where K is the
  attributed Part.
- **The actual rule** (line 867-892): any-valid-witness, because
  the wire carries no Part attribution.
- **The reach paths** (line 899-908): alias-export shadow across
  WITH, UNION with same-named different-typed branches.
- **The soundness gap** (line 909-916): the resolver admits queries
  openCypher would reject on Part-attributed re-analysis; the
  invariant "resolved type witnessed against SOME in-scope Part"
  holds but the invariant "resolved type witnessed against THE
  Part where the parser attributed the Use" does not.
- **The remediation** (line 918-924): thread a Part index through
  every Use on the wire and have the parser attribute Uses to
  Parts at build time. Filed as `gqlc-fvo` (this cycle).

R5 §7.1 audit table (line 2333) pins the residual as the
model-unfreeze bead. R6 (`docs/specs/resolver-stage-r6.md §7.1.3`
line 1994) and R7 (`docs/specs/resolver-stage-r7.md` lines 56-60)
carry the gap unchanged.

R7 §7.1.1 (line 2278-2288) additionally flags gqlc-fvo as
the **PARENT** of a sibling gap: CALL-argument-vs-parameter type
attribution, which R7 also cannot close within its cycle. gqlc-fvo
closes the FRAMEWORK — a Part index on every Use — that the
CALL-arg cycle (`gqlc-lta` or a successor) can build on. This
cycle does not touch the CALL-arg axis; it lands the parent
attribution primitive.

This cycle DELIVERS the R5 §4.2.4 remediation (`Use.Part` axis),
adds the R5-recorded discriminating fixture (the reversed
alias-shadow — §7.4), and closes the R5 §7.1 audit-table row.

---

## 4. The unfreeze — parser and model changes

### 4.1 `Use` variants — the additive field

#### 4.1.1 Structs

```go
type PropertyUse struct {
    ref  Ref // the binding property the parameter sits against
    part int // the branch-relative index of the enclosing Part (fvo, ADR 0008 amendment 2026-07-06)
}

type ExprUse struct {
    enclosingType Type         // the result type of the enclosing rich expression
    position      ExprPosition // where the enclosing expression sits (projection / predicate)
    part          int          // the branch-relative index of the enclosing Part (fvo)
}

type ClauseSlotUse struct {
    slot ClauseSlot
    part int // the branch-relative index of the enclosing Part (fvo)
}
```

The field lands last in each struct — the additive-only convention
across the query package (`nullable` fields on `EdgeBinding` /
`NodeBinding` and `containsAggregate` on `ExprProjection` follow
the same tail-append style).

#### 4.1.2 Constructors + accessors

New constructors — one per variant, all named `New…UseAt` following
the hk0 `NewExprProjectionWithAggregate` naming convention (verb
+ axis suffix):

```go
// NewPropertyUseAt builds a PropertyUse carrying its Ref and the branch-relative
// index of the enclosing Part (Part 0 for a single-Part branch, or the position
// under EnterOC_With's Part swap; fvo per ADR 0008 amendment 2026-07-06).
// Callers that do not need the Part attribution use NewPropertyUse, which
// forwards part=0.
func NewPropertyUseAt(r Ref, part int) PropertyUse {
    return PropertyUse{ref: r, part: part}
}

// NewExprUseAt builds an ExprUse carrying the enclosing rich expression's result
// type, position discriminator, and the branch-relative index of the enclosing
// Part (fvo).
func NewExprUseAt(enclosing Type, position ExprPosition, part int) ExprUse {
    return ExprUse{enclosingType: enclosing, position: position, part: part}
}

// NewClauseSlotUseAt builds a ClauseSlotUse carrying the clause slot and the
// branch-relative index of the enclosing Part (fvo).
func NewClauseSlotUseAt(s ClauseSlot, part int) ClauseSlotUse {
    return ClauseSlotUse{slot: s, part: part}
}
```

Preserved constructors (verbatim signatures, each forwards through
the new one with `part=0`):

```go
func NewPropertyUse(r Ref) PropertyUse            { return NewPropertyUseAt(r, 0) }
func NewExprUse(enclosing Type, position ExprPosition) ExprUse { return NewExprUseAt(enclosing, position, 0) }
func NewClauseSlotUse(s ClauseSlot) ClauseSlotUse { return NewClauseSlotUseAt(s, 0) }
```

New accessor — one per variant, identically named:

```go
// Part reports the branch-relative index of the Part the parameter Use
// lexically occurs in (fvo per ADR 0008 amendment 2026-07-06). Populated
// parser-side by addParameterUse's currentPartIndex call. The resolver's
// witnessAcrossScopes reads this to select the exact scope to witness
// against (docs/specs/resolver-stage-r5.md §4.2.4 close-out).
func (u PropertyUse) Part() int   { return u.part }
func (u ExprUse) Part() int       { return u.part }
func (u ClauseSlotUse) Part() int { return u.part }
```

`isUse()`, `Ref()` (PropertyUse), `EnclosingType()` / `Position()`
(ExprUse), `Slot()` (ClauseSlotUse) are unchanged.

**The `Use` interface stays sealed with one method.** Adding
`Part()` to `Use` would force every future variant author to
implement it, tying the sum's arity to the axis's presence.
Per-variant field + accessor keeps `Use` sealed at one method
(`isUse()`); every consumer that needs `Part()` type-switches on
the variant and reads directly.

#### 4.1.3 JSON — omit-when-zero on all three variants

The three marshalled structs each gain one field:

```go
func (u PropertyUse) MarshalJSON() ([]byte, error) {
    return json.Marshal(struct {
        Kind     string `json:"kind"`
        Variable string `json:"variable"`
        Property string `json:"property"`
        Part     int    `json:"part,omitempty"`
    }{
        Kind:     useKindProperty,
        Variable: u.ref.Variable,
        Property: u.ref.Property,
        Part:     u.part,
    })
}

func (u ExprUse) MarshalJSON() ([]byte, error) {
    return json.Marshal(struct {
        Kind          string `json:"kind"`
        EnclosingType Type   `json:"enclosingType"`
        Position      string `json:"position"`
        Part          int    `json:"part,omitempty"`
    }{
        Kind:          useKindExpr,
        EnclosingType: projectionType(u.enclosingType),
        Position:      u.position.String(),
        Part:          u.part,
    })
}

func (u ClauseSlotUse) MarshalJSON() ([]byte, error) {
    return json.Marshal(struct {
        Kind string `json:"kind"`
        Slot string `json:"slot"`
        Part int    `json:"part,omitempty"`
    }{
        Kind: useKindClauseSlot,
        Slot: u.slot.String(),
        Part: u.part,
    })
}
```

##### 4.1.3.1 Fence accounting under omit-when-zero — 0 goldens rebaseline

**Corpus at branch base** (`origin/master @ baba282`):

- **3199** `*.golden.json` files under
  `internal/query/cypher/testdata/golden/` (matches hk0's post-close
  count).
- **22** of those goldens embed at least one `Use` record
  (parameter uses in the `"uses"` array of a `"parameters"`
  entry). Discovery command at §4.4.1.
- **31** total Use records across those 22 goldens.
- **8** of the 22 goldens have branches with ≥ 2 Parts (multi-Part
  branches — i.e. queries containing at least one WITH).
- Of those 8, EVERY Use is emitted with `curPart` = Part 0 (the
  Part being CLOSED by the WITH, not the Part opened after; §3.2
  step 2). §4.4.1 pins each of the 8 by scenario source with
  first-party corroboration.
- **0** of the 22 goldens carry any Use in a Part index ≥ 1.
- **0** goldens carry a Use in a UNION branch (i.e. branch index
  ≥ 1).

**Consequence.** Under `omit-when-zero`, every Use in every
branch-base golden serialises to the SAME wire it emits today —
the `"part"` key is absent because the value is 0. Golden
rebaseline scope: **0 files**. Every parser golden is
byte-for-bit identical after the unfreeze PR.

**Divergence audit against the hk0 house convention.** hk0's
omit-when-false ruling rebaselined 20 goldens (its widened
walker flipped `containsAggregate` from absent to `true` on 20
scenarios with embedded aggregates). This cycle rebaselines
zero — the omit-when-zero encoding lets a bit of surface land
into the wire's schema without a single byte moving in the
corpus. The convention holds: the campaign records the axis's
existence; the rebaseline scope is the axis's ACTUAL wire
presence, not its schema surface.

**The always-emit alternative accounted for.** Under an
always-emit encoding (mirroring `directed`), every Use record
emits `"part": <n>` unconditionally. At branch base 22 goldens
gain the key at least once (31 keys total). The reviewer-side
diff would be dominated by mechanical `"part": 0` insertions —
exactly the anti-fence hk0 rejected. Omit-when-zero preserves
the reviewer's ability to spot semantic-only changes: the
rebaseline diff of a FUTURE cycle that introduces a
Part-index-≥-1 Use flips a small, enumerable set of goldens
(each one carrying at least one non-zero Part key).

##### 4.1.3.2 Reviewer-side fence commands

**Fence 1 — strip-key overreach detector** (the widened
marshaller must not leak `part: <nonzero>` outside a
scenario-verified set; at branch base every Use is Part 0, so the
detector must find ZERO non-zero part keys in the widened
corpus). Run from the worktree at the unfreeze PR's branch tip,
against `origin/master @ baba282`:

```
python3 - <<'PY'
import glob, json
def go_dump(data, f):
    # Go's encoding/json HTML-escapes <, >, & inside strings;
    # a plain json.dump re-writes them literally and
    # false-positives every golden carrying one.
    # (Copied verbatim from the hk0 §4.1.3 shim — carrying the
    # hk0 §12 errata 1 fix; do not reintroduce the bug.)
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
            n.pop('part', None)
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

If the stat print is non-empty, a Use whose true lexical Part is
0 emitted with `part: <nonzero>` — a `currentPartIndex` bug (an
off-by-one from a missed nil-check, an EXISTS depth counter
error, or a mis-attribution across the WITH swap).

**Fence 2 — set-equality check** (the set of changed goldens must
EQUAL the empty set):

```
git diff --name-only origin/master -- \
    internal/query/cypher/testdata/golden/*.golden.json | \
    sed 's|.*/||' | sort > /tmp/changed.txt
: > /tmp/expected.txt
diff /tmp/expected.txt /tmp/changed.txt && echo "SET-EQUAL: PASS"
# MUST print: SET-EQUAL: PASS.
```

If Fence 1 passes but Fence 2 fails, the marshaller emitted a
non-`part` change on some golden — a formatting drift or an
unrelated wire-shape edit that slipped in. The unfreeze PR must
pass BOTH fences before landing.

**Fence 3 (unit-test back-compat)** — the pre-existing
`TestExprUseMarshalJSON` pin in `internal/query/type_test.go:93`
stays VERBATIM (the only pre-existing Use `Marshal…JSON` test
at branch base per §3.1.4). Its pinned JSON string carries a
`part=0` Use; under omit-when-zero the key is absent, matching
the existing pinned string bit-for-bit. `TestNewExprUse` at
`:73` also stays verbatim (its assertions do not consult
`Part()`). §5.1 records the four NEW zero-side tests the
unfreeze PR adds to bring PropertyUse and ClauseSlotUse to
ExprUse's coverage level. §5.2 records the three new
Part-≥-1 test cases that witness the true-side encoding.

### 4.2 `addParameterUse` — the emission-time attribution

Current (`internal/query/cypher/expr.go:633-642`):

```go
func (l *listener) addParameterUse(name string, node antlr.Tree, use query.Use) {
    idx, ok := l.byParam[name]
    if !ok {
        idx = len(l.params)
        l.byParam[name] = idx
        l.params = append(l.params, &query.Parameter{Name: name})
    }
    l.params[idx].Uses = append(l.params[idx].Uses, use)
    l.approved[node] = true
}
```

Widened:

```go
func (l *listener) addParameterUse(name string, node antlr.Tree, use query.Use) {
    idx, ok := l.byParam[name]
    if !ok {
        idx = len(l.params)
        l.byParam[name] = idx
        l.params = append(l.params, &query.Parameter{Name: name})
    }
    l.params[idx].Uses = append(l.params[idx].Uses, attributePart(use, l.currentPartIndex()))
    l.approved[node] = true
}
```

Two new helpers land in the same file:

```go
// currentPartIndex returns the branch-relative index of the Part collection
// handlers currently write into — len(curBranch.parts)-1 by construction of
// the priming discipline at listener.go:226-235 and :588-599 (fvo per
// ADR 0008 amendment 2026-07-06). Every addParameterUse call site runs under
// a non-nil curBranch and non-nil curPart, so the subtraction is well-defined.
func (l *listener) currentPartIndex() int {
    return len(l.curBranch.parts) - 1
}

// attributePart returns the Use with its part field populated. Sum-preserving:
// the returned Use has the same variant as u. Used by addParameterUse to
// stamp the branch-relative Part index onto every Use at emission time.
func attributePart(u query.Use, part int) query.Use {
    switch uu := u.(type) {
    case query.PropertyUse:
        return query.NewPropertyUseAt(uu.Ref(), part)
    case query.ExprUse:
        return query.NewExprUseAt(uu.EnclosingType(), uu.Position(), part)
    case query.ClauseSlotUse:
        return query.NewClauseSlotUseAt(uu.Slot(), part)
    default:
        return u
    }
}
```

The 18 call sites (§3.3) stay verbatim — each continues to pass
the zero-Part Use it constructs today. The stamping is centralised
at `addParameterUse` because it is the single chokepoint (per the
listener struct's dedup discipline, `expr.go:627-632`).

**Alternative shape considered.** Pushing the Part index into each
call site (each `NewExprUse(t, pos)` becomes
`NewExprUseAt(t, pos, l.currentPartIndex())`). **Rejected**: this
scatters the Part-index lookup across 18 sites, each of which
would need to re-read `curBranch.parts`; the centralised
`addParameterUse` chokepoint is exactly the right place for the
attribution — one call, one invariant, one place to audit.

**Alternative shape considered.** Store the Part index on the
`Parameter` record (one `Uses []Use` slot, one parallel
`[]int` of Part indices). **Rejected**: violates the Use-record
locality — the axis is a property of each Use, not of the
Parameter. The parallel-slice shape also splits the wire's decode
surface (§2's third bullet).

#### 4.2.1 The `curPart`/`curBranch` non-nil invariant — proven

Every `addParameterUse` call site runs under a non-nil `curBranch`
and non-nil `curPart`. Proof sketch: every emission site
enumerated in §3.3 sits inside one of the following `Enter*`
handlers:

- `EnterOC_Match`, `EnterOC_With`, `EnterOC_Return`,
  `EnterOC_Create`, `EnterOC_Merge`, `EnterOC_Delete`,
  `EnterOC_Set`, `EnterOC_Remove`, `EnterOC_Unwind`,
  `EnterOC_ExistentialSubquery`,
  `EnterOC_InQueryCall` / `enterInQueryCall`,
  `EnterOC_StandaloneCall` / `enterStandaloneCall`.

The listener's priming discipline (`internal/query/cypher/listener.go:226-235`)
requires that `EnterOC_SingleQuery` — the first `Enter*` handler
to fire on any regular query — appends a fresh `rawBranch` and
`rawPart` and points `curBranch` and `curPart` at them before any
inner handler runs. The pure-standalone-CALL grammar-quirk
carve-out (`:588-599`) primes them in `EnterOC_StandaloneCall`
itself.

Every other collection handler is a descendant of either
`OC_SingleQuery` or `OC_StandaloneCall` in the grammar tree, so
by the time it fires, one of the primers has run. Consequently
`l.curBranch != nil` and `l.curPart != nil` at every
`addParameterUse` call site.

**Belt-and-braces guard.** The `if l.curPart == nil` check at
`listener.go:592` predates the priming discipline and is a
defensive tripwire. `currentPartIndex` does NOT re-check — the
invariant is upheld by the priming discipline, and a nil `curPart`
would already have crashed on any of the existing `curPart.*`
dereferences in the collection handlers before `addParameterUse`
was even called. Adding a defensive check would violate the
"don't add error handling for scenarios that can't happen"
posture from CLAUDE.md.

#### 4.2.2 The WITH-boundary emission semantics — the off-by-one hazard resolved

**Semantic question raised by the mining task.** The Cypher
`WITH … WHERE …` clause has a subtle scoping: the WHERE
predicate filters the WITH-projected tuples. Semantically the
WHERE belongs to the "projected" Part (K+1); the parser attributes
its Uses to the "closing" Part (K), because `mineWhere` runs
before the swap (§3.2). Is that off-by-one?

**Answer: no, by design of `resolvePart`'s scope building.**
The R5 kernel builds `partScope` per Part K from that Part's
local + carried tables. A Use attributed to Part K (the closing
side of the WITH) is witnessed against Part K's scope. A
Use attributed to Part K+1 (the opening side) would be witnessed
against Part K+1's scope. Both scopes carry the WITH-exported
names (Part K natively; Part K+1 as imports). The Uses that
appear in a WITH's WHERE always name variables that survive the
WITH — otherwise the WHERE would fail parser scope-checking. So
the Use resolves cleanly in either scope.

**But the shadow case** (§7.4.1's reversed alias-shadow fixture):
`MATCH (a:Post) WITH a.title AS a MATCH (a:Person) WHERE a.title = $p`.
The WHERE lives in Part 1's MATCH, not in the WITH itself. The
`$p` is emitted by `mineWhere` — but `mineWhere` runs from
`EnterOC_Match` at `listener.go:267-269`, NOT from `EnterOC_With`.
`EnterOC_Match` does not swap `curPart`; it collects into the
CURRENT `curPart`. And the CURRENT `curPart` at the time
`EnterOC_Match` fires for the SECOND MATCH is Part 1 (post-swap
from the WITH). So the `$p` is attributed to Part 1. **Correct.**

**Attribution-lifecycle summary table.**

| Emission site | Enclosing handler | `curPart` at emission | Attributed Part index (branch-relative) |
|---|---|---|---|
| MATCH pattern's property map | `EnterOC_Match` | the current Part being collected into (unchanged by handler) | current Part's index |
| MATCH WHERE | `EnterOC_Match` → `mineWhere` | same | same |
| WITH item (projection body) | `EnterOC_With` | the CLOSING Part (before the swap at line 292-293) | closing Part's index |
| WITH's ORDER BY / SKIP / LIMIT | `EnterOC_With` → `collectProjection` (still pre-swap) | same as WITH item | same |
| WITH's WHERE | `EnterOC_With` → `mineWhere` (still pre-swap) | same | same |
| RETURN item / ORDER BY / SKIP / LIMIT / WHERE | `EnterOC_Return` (no swap; RETURN is terminal) | the FINAL Part | final Part's index |
| CALL argument (in-query or standalone) | `enterInQueryCall` / `enterStandaloneCall` | the current Part containing the CALL | current Part's index |
| CALL WHERE-on-YIELD | `enterInQueryCall` | same | same |
| CREATE / MERGE / DELETE / SET / REMOVE | corresponding `Enter*` handler | the current Part containing the effect | current Part's index |
| UNWIND source expression | `EnterOC_Unwind` | the current Part | current Part's index |
| EXISTS subquery body parameters | `EnterOC_ExistentialSubquery` | the OUTER Part (subqueryDepth positive suppresses inner collection) | outer Part's index |

Every row's attribution matches the LEXICAL Part the Use's
`$param` token sits in.

### 4.3 Parser-test pins — every Use pin at Part 0 today, all byte-identical

Enumeration of every parser-test pin whose `want` embeds a `Use`
constructor, drawn from
`grep -n "NewPropertyUse\|NewExprUse\|NewClauseSlotUse" internal/query/cypher/parser_test.go`
at branch base `baba282`:

| Line | Pin scope | Use constructor | Query context (fresh read) | Lexical Part |
|---|---|---|---|---|
| 258 | Property predicate | `NewPropertyUse(Ref{"b", "name"})` | `MATCH (a)-[:R]->(b) WHERE b.name = $p RETURN a` — single-Part | 0 |
| 275 | SKIP slot | `NewClauseSlotUse(ClauseSlotSkip)` | `MATCH (n) RETURN n SKIP $s` — single-Part | 0 |
| 293 | LIMIT slot | `NewClauseSlotUse(ClauseSlotLimit)` | `MATCH (n) RETURN n LIMIT $l` — single-Part | 0 |
| 564 | Rich projection | `NewExprUse(TypeUnknown, ExprInProjection)` | rich-expression `RETURN` residual | 0 |
| 589 | Rich projection | `NewExprUse(TypeUnknown, ExprInProjection)` | rich-expression `RETURN` residual | 0 |
| 611 | Predicate residual | `NewExprUse(TypeBool, ExprInPredicate)` | WHERE residual | 0 |
| 633 | List-typed projection | `NewExprUse(TypeList{TypeUnknown}, ExprInProjection)` | `RETURN [1, $x, 3]` — single-Part | 0 |
| 1144 | Rich projection | `NewExprUse(TypeUnknown, ExprInProjection)` | rich-expression `RETURN` residual | 0 |
| 1495 | Rich projection | `NewExprUse(TypeUnknown, ExprInProjection)` | rich-expression `RETURN` | 0 |
| 1528 | Rich projection | `NewExprUse(TypeInt, ExprInProjection)` | `RETURN sum($p + 1)` or similar — single-Part | 0 |
| 1672 | Predicate residual | `NewExprUse(TypeBool, ExprInPredicate)` | WHERE residual | 0 |
| 1690 | Predicate residual | `NewExprUse(TypeBool, ExprInPredicate)` | WHERE residual | 0 |
| 1712 | Predicate residual | `NewExprUse(TypeBool, ExprInPredicate)` | WHERE residual | 0 |
| 1731 | Predicate residual | `NewExprUse(TypeBool, ExprInPredicate)` | WHERE residual | 0 |
| 1884 | Property use | `NewPropertyUse(Ref{"p", "name"})` | pin's context — single-Part or Use in Part 0 | 0 |
| 1910 | SET value | `NewExprUse(TypeUnknown, ExprInSetValue)` | SET's value expression | 0 |
| 1936 | DELETE target | `NewExprUse(TypeUnknown, ExprInDeleteTarget)` | DELETE target | 0 |
| 2479 | Predicate residual (nested Use{} slice) | `NewExprUse(TypeBool, ExprInPredicate)` | pin's context | 0 |

**Every Use in every parser-test pin lives in Part 0.** No pin
rebaselines. Under Go struct zero-value equality
(`reflect.DeepEqual` at `require.Equal`), a `PropertyUse{ref, 0}`
matches `PropertyUse{ref, part: 0}` bit-for-bit; likewise for
ExprUse and ClauseSlotUse. The 18 assertions above pass without
modification.

The re-verification method for the "Part 0" claim per pin: for
each row, read the pin's `src` field, count occurrences of `WITH`
before the `$param` token, and count preceding `UNION` tokens.
Every row's src has zero preceding `WITH` (single-Part queries)
and zero preceding `UNION` (single-branch queries). The check is
mechanical:

```
grep -A1 "NewPropertyUse\|NewExprUse\|NewClauseSlotUse" \
    internal/query/cypher/parser_test.go \
  | grep -B10 "src:" | ...
```

(The reviewer's fence for the unfreeze PR is: run the parser test
suite. Every pin passes untouched, which is the byte-identity
witness.)

**The fence check for parser tests** (from the worktree; the run
list includes the two pre-existing Use unit tests AND the seven
new tests §5 adds):

```
go test -run 'TestMustParse|TestMustReject|TestNewExprUse|TestExprUseMarshalJSON|TestNewPropertyUse|TestPropertyUseMarshalJSON|TestNewClauseSlotUse|TestClauseSlotUseMarshalJSON|TestNewPropertyUseAt|TestNewExprUseAt|TestNewClauseSlotUseAt' ./internal/query/... -shuffle=on
```

The 18 parser-test pins pass without modification (byte-identity
under `reflect.DeepEqual`; §4.3). The two pre-existing Use unit
tests (`TestNewExprUse`, `TestExprUseMarshalJSON`) also pass
unchanged. The seven tests §5 adds (four zero-side, three
non-zero-side `TestNew…UseAt`) are the only test-file additions
in the unfreeze PR.

### 4.4 Every parser golden — byte-identical

Under the omit-when-zero JSON encoding (§4.1.3), the `part` key
is emitted **only** when the Use's `part` is non-zero. §4.1.3.1
accounted the corpus: **0** goldens flip.

#### 4.4.1 Golden-corpus enumeration — the empty set

**Discovery method — scenario-source cross-check.** Mine every
golden that embeds a `"kind": "property"` /
`"kind": "expr"` / `"kind": "clause-slot"` blob inside a
`"parameters"[i].uses[j]` object; for each, recover the scenario
source via the exact hash recipe used by `checkGolden` at
`internal/query/cypher/acceptance_test.go:1063-1068`; identify
the lexical Part of each `$param` token by walking the query's
clause sequence.

**Complete discovery script** (Python 3; run from the worktree
root; project host lacks `jq`):

```
python3 - <<'PY'
import re, os, glob, hashlib, json

CYPHER_DIR = "internal/query/cypher"
FROOT     = "test/data/query/cypher/tck/features"
GROOT     = f"{CYPHER_DIR}/testdata/golden"

def uri_form(p):
    return os.path.relpath(p, CYPHER_DIR)

def hash_golden(uri, name, q):
    s = hashlib.sha1((uri + "\x00" + name + "\x00" + q).encode()).digest()
    base = os.path.splitext(os.path.basename(uri))[0]
    return f"{base}_{s[:6].hex()}.golden.json"

# (scan_feature is the hk0 §4.4.1 helper — mirror it verbatim,
#  including the Scenario Outline expansion, Examples table row
#  substitution, and """query""" block dedent.)
# ... (elided: see hk0 §4.4.1 for the full body) ...

def has_uses(data):
    for p in (data.get("parameters") or []):
        for u in (p.get("uses") or []):
            return True
    return False

def count_parts(data):
    counts = []
    for br in (data.get("branches") or []):
        counts.append(len(br.get("parts") or []))
    return counts

# Emission-time Part attribution per §3.2 / §4.2.2:
# tokenise the query up to the $param; count how many WITHs precede it
# WITHIN THE CURRENT BRANCH (reset on each UNION token).
def lexical_part_index(query_text, dollar_pos):
    # Walk the query text char by char, counting WITHs and UNIONs prior
    # to dollar_pos.
    upto = query_text[:dollar_pos]
    # Reset branch on every UNION (case-insensitive, whole-word).
    parts = re.split(r"(?i)\bUNION(?:\s+ALL)?\b", upto)
    latest = parts[-1]
    with_count = len(re.findall(r"(?i)\bWITH\b", latest))
    # A `$` inside a WITH-item / WITH's WHERE is attributed to the
    # closing Part (§4.2.2): if the last WITH keyword's projection body
    # includes the dollar, deduct one. Approximation: if the dollar
    # sits after the LAST `WITH` but before the next MATCH / RETURN /
    # UNWIND / CREATE / MERGE / DELETE / SET / REMOVE / CALL token,
    # the dollar is inside that WITH's body → attributed to Part
    # `with_count-1` (i.e. the WITH ITSELF is the boundary of the
    # closing Part). Otherwise (dollar sits inside a clause that comes
    # AFTER the WITH's boundary), attributed to Part `with_count`.
    if with_count == 0:
        return 0
    # Find the position of the last WITH keyword.
    matches = list(re.finditer(r"(?i)\bWITH\b", latest))
    last_with = matches[-1]
    # Find the position of the next Part-boundary clause keyword after
    # last_with.end().
    boundary = re.search(
        r"(?i)\b(MATCH|OPTIONAL\s+MATCH|RETURN|UNWIND|CREATE|MERGE|DELETE|"
        r"DETACH\s+DELETE|SET|REMOVE|CALL|UNION)\b",
        latest[last_with.end():])
    dollar_in_with_body = boundary is None or (
        last_with.end() + boundary.start() > len(latest))
    if dollar_in_with_body:
        return with_count - 1  # attributed to the CLOSING Part
    return with_count            # attributed to a POST-WITH Part

# Build golden -> (uri, scenario name, query text) index.
scenario_index = {}
for feat in sorted(glob.glob(f"{FROOT}/**/*.feature", recursive=True)):
    uri = uri_form(feat)
    for name, q in scan_feature(feat):
        scenario_index[hash_golden(uri, name, q)] = (uri, name, q)

flip = []
uses_by_part = {}
for path in sorted(glob.glob(f"{GROOT}/*.golden.json")):
    with open(path) as f: data = json.load(f)
    if not has_uses(data): continue
    entry = scenario_index.get(os.path.basename(path))
    if entry is None: continue
    _, _, q = entry
    for dollar in re.finditer(r"\$\w+", q):
        p = lexical_part_index(q, dollar.start())
        uses_by_part[p] = uses_by_part.get(p, 0) + 1
        if p >= 1:
            flip.append((os.path.basename(path), q[max(0,dollar.start()-20):dollar.end()+10]))

print(f"total Uses in corpus: {sum(uses_by_part.values())}")
print(f"Uses by lexical Part index: {sorted(uses_by_part.items())}")
print(f"goldens with any Use in Part >= 1: {len(flip)}")
for g, ctx in flip:
    print(f"  {g}  |  ...{ctx}...")
PY
```

**Verified output at branch base `origin/master @ baba282`
(cross-checked with the sample earlier this cycle):**

- 31 total `$param` occurrences across the 22 goldens with Uses.
- Uses by lexical Part index: `{0: 31}` — every Use in Part 0.
- Goldens with any Use in Part ≥ 1: **0**.

**First-party corroboration for each of the 8 goldens with
BOTH Uses AND multi-Part branches** (freshly read from
`internal/query/cypher/testdata/golden/*.golden.json` and the TCK
scenario source):

| Golden | Scenario (TCK) | Query context | Lexical Part of each `$` |
|---|---|---|---|
| `List1_127c430f2fd1.golden.json` | List1 [3] Use list lookup based on parameters when there is no type information | `WITH $expr AS expr, $idx AS idx RETURN expr[idx] AS value` — WITH is the first clause; both `$` sit in the WITH's projection body (pre-swap) | 0, 0 |
| `List1_24e84c2a2a0d.golden.json` | List1 [5] Use list lookup based on parameters when there is rhs type information | `WITH $expr AS expr, $idx AS idx RETURN expr[toInteger(idx)] AS value` — same shape | 0, 0 |
| `Map2_8cc826aae8a0.golden.json` | Map2 [1] Dynamically access a field based on parameters when there is no type information | `WITH $expr AS expr, $idx AS idx RETURN expr[idx] AS value` — same shape | 0, 0 |
| `Map2_98f223846123.golden.json` | Map2 [2] Dynamically access a field based on parameters when there is rhs type information | `WITH $expr AS expr, $idx AS idx RETURN expr[toString(idx)] AS value` — same shape | 0, 0 |
| `With6_4540faf7c149.golden.json` | With6 [5] Handle constants and parameters inside an expression which contains an aggregation expression | `MATCH (person) WITH $age + avg(person.age) - 1000 AS agg RETURN *` — `$age` sits inside the WITH's projection body | 0 |
| `WithOrderBy4_4e45d6d93d77.golden.json` | WithOrderBy4 [16] Handle constants and parameters inside an order by item which contains an aggregation expression | `MATCH (person) WITH avg(person.age) AS avgAge ORDER BY $age + avg(person.age) - 1000 RETURN avgAge` — `$age` sits in the WITH's ORDER BY (child of oC_ProjectionBody) | 0 |
| `WithSkipLimit3_11bd6a1918d6.golden.json` | WithSkipLimit3 [2] Get rows in the middle by param | `MATCH (n) WITH n ORDER BY n.name ASC SKIP $s LIMIT $l RETURN n` — `$s` and `$l` are children of the WITH's oC_ProjectionBody | 0, 0 |
| `WithWhere2_6f8b82aba9de.golden.json` | WithWhere2 [2] Filter node with conjunctive multi-part property predicates on multi variables with multiple bindings | `MATCH (advertiser)-… WITH a, advertiser, red, out WHERE advertiser.id = $1 AND a.id = $2 …` — `$1` and `$2` sit in the WITH's trailing WHERE (mineWhere runs pre-swap; §4.2.2) | 0, 0 |

Every `$param` in every one of the 8 goldens sits in a place where
`curPart` at emission is Part 0 (either a single-Part branch or
the closing side of the first WITH). Under omit-when-zero, none of
these emits a `part` key. **0 goldens flip.**

**Bidirectional fence** — same discipline as hk0 §4.4.1:

*Fence 1 — strip-key overreach detector* per §4.1.3.2 above.

*Fence 2 — set-equality check* per §4.1.3.2 (expected set is
empty).

Both fences MUST pass. If Fence 1 fails, a Use whose lexical Part
is 0 emitted with a non-zero part index — a
`currentPartIndex` bug (off-by-one; misplaced call relative to
the WITH swap; missed EXISTS suppression). If Fence 2 fails,
either the widened marshaller over-emitted (extras: the reviewer
inspects each to determine whether the discovery script
under-approximated or the walker mis-attributed) or a non-`part`
change slipped in (the diff shows what).

### 4.5 The parser test suite's Use coverage — no other pin changes

Beyond the 18 pins at §4.3, no parser test consumes a Use record
directly. The pre-existing `TestExprUseMarshalJSON` at
`internal/query/type_test.go:93` pins the wire shape as a string
and stays verbatim under omit-when-zero (§4.1.3.2 Fence 3). §5.1
adds the missing zero-side coverage for PropertyUse and
ClauseSlotUse; those additions are unit-test additions, not
parser-test-pin edits.

### 4.6 The `Parameter` list-order invariant is preserved

`internal/query/query.go:1297-1300`:

```go
type Parameter struct {
    Name string `json:"name"`
    Uses []Use  `json:"uses"`
}
```

The Uses slice is order-preserving: first-appearance-first, per
the dedup discipline at `addParameterUse` (`expr.go:634-641`). The
widening does not touch the ordering — `attributePart` returns a
Use of the same variant with the same field values plus one new
`part` field. The slice append semantics are unchanged.

**Sanity: does Part-attribution invalidate the "one Parameter per
Name" contract?** No. A parameter appearing in two Parts under a
single query records TWO Uses on the same Parameter — one per
Part. The Parameter is deduped by Name (D1 invariant); the Uses
list records each occurrence's Part index. That's exactly the
invariant R5's audit note (§4.2.4) said the wire lost — this
cycle records it.

---

## 5. Unit-test additions — the two-sided axis

`internal/query/type_test.go` — this section adds SEVEN new tests
and preserves TWO pre-existing tests verbatim. The additions
split into two groups:

- §5.1 **zero-side coverage** (four new tests): PropertyUse and
  ClauseSlotUse have no dedicated constructor or `Marshal…JSON`
  unit tests at branch base (§3.1.4). The widening MUST NOT
  leave them uncovered on the zero side, so the unfreeze PR
  brings PropertyUse and ClauseSlotUse up to ExprUse's coverage
  level BEFORE ExprUse's own zero-side pins would be revalidated.
  The new tests each pin the constructor's field wiring and the
  omit-when-zero wire shape (`part=0` key ABSENT).
- §5.2 **non-zero-side coverage** (three new tests): the three
  `TestNew…UseAt` tests witness the widened constructor path
  with a non-zero Part, pinning the omit-when-zero wire shape
  from the other direction (`part=<n>` key PRESENT).

The pre-existing `TestNewExprUse` (`internal/query/type_test.go:73`)
and `TestExprUseMarshalJSON` (`:93`) stay VERBATIM — their
assertions do not consult `Part()`, and under omit-when-zero the
already-pinned JSON string matches the widened marshaller
bit-for-bit.

### 5.1 New zero-side tests — PropertyUse and ClauseSlotUse coverage

```go
// TestNewPropertyUse pins the pre-existing constructor's field wiring —
// the widened constructor delegates to NewPropertyUseAt(ref, 0), so the
// zero-value Part must round-trip through the accessor. New at fvo per
// §5.1: PropertyUse had no dedicated constructor unit test at branch base
// (only ExprUse did); the widening must not leave the zero side of a Use
// variant it touches uncovered.
func TestNewPropertyUse(t *testing.T) {
    u := query.NewPropertyUse(query.Ref{Variable: "a", Property: "title"})
    require.Equal(t, query.Ref{Variable: "a", Property: "title"}, u.Ref())
    require.Equal(t, 0, u.Part())
    var _ query.Use = u
}

// TestPropertyUseMarshalJSON pins the wire encoding for the zero-value Part —
// the "part" key is ABSENT under omit-when-zero. New at fvo per §5.1: same
// rationale as TestNewPropertyUse; PropertyUse had no MarshalJSON test at
// branch base.
//
// Cross-check hk0 §12 errata 2 (flatRef always emits "property":""): the
// PropertyUse marshaller has no omitempty on "property", so a bare-Property
// case would still emit `"property":""`. This test's Ref carries a
// non-empty Property; the always-emit invariant is witnessed by the
// parser-test pins via the flatRef path, not here.
func TestPropertyUseMarshalJSON(t *testing.T) {
    out, err := json.Marshal(query.NewPropertyUse(query.Ref{Variable: "a", Property: "title"}))
    require.NoError(t, err)
    require.JSONEq(t,
        `{"kind":"property","variable":"a","property":"title"}`,
        string(out))
}

// TestNewClauseSlotUse pins the pre-existing constructor's field wiring —
// the widened constructor delegates to NewClauseSlotUseAt(slot, 0). New at
// fvo per §5.1: ClauseSlotUse had no dedicated constructor unit test at
// branch base.
func TestNewClauseSlotUse(t *testing.T) {
    u := query.NewClauseSlotUse(query.ClauseSlotSkip)
    require.Equal(t, query.ClauseSlotSkip, u.Slot())
    require.Equal(t, 0, u.Part())
    var _ query.Use = u
}

// TestClauseSlotUseMarshalJSON pins the wire encoding for the zero-value
// Part — the "part" key is ABSENT under omit-when-zero. New at fvo per
// §5.1: same rationale as TestNewClauseSlotUse.
func TestClauseSlotUseMarshalJSON(t *testing.T) {
    out, err := json.Marshal(query.NewClauseSlotUse(query.ClauseSlotSkip))
    require.NoError(t, err)
    require.JSONEq(t,
        `{"kind":"clause-slot","slot":"skip"}`,
        string(out))
}
```

### 5.2 New non-zero-side tests — `TestNew…UseAt`

```go
// TestNewPropertyUseAt pins the widened Use variant per ADR 0008 amendment
// 2026-07-06: the Part axis carries through the constructor, the accessor,
// and the wire shape as an omit-when-zero key (post-freeze convention:
// additive axes emit omit-when-zero-value).
func TestNewPropertyUseAt(t *testing.T) {
    u := query.NewPropertyUseAt(query.Ref{Variable: "a", Property: "title"}, 1)
    require.Equal(t, query.Ref{Variable: "a", Property: "title"}, u.Ref())
    require.Equal(t, 1, u.Part())

    out, err := json.Marshal(u)
    require.NoError(t, err)
    require.JSONEq(t,
        `{"kind":"property","variable":"a","property":"title","part":1}`,
        string(out))
}

// TestNewExprUseAt pins the widened ExprUse variant per ADR 0008 amendment
// 2026-07-06. Same convention as TestNewPropertyUseAt.
func TestNewExprUseAt(t *testing.T) {
    u := query.NewExprUseAt(query.TypeBool{}, query.ExprInPredicate, 2)
    require.Equal(t, query.TypeBool{}, u.EnclosingType())
    require.Equal(t, query.ExprInPredicate, u.Position())
    require.Equal(t, 2, u.Part())

    out, err := json.Marshal(u)
    require.NoError(t, err)
    require.JSONEq(t,
        `{"kind":"expr","enclosingType":"bool","position":"predicate","part":2}`,
        string(out))
}

// TestNewClauseSlotUseAt pins the widened ClauseSlotUse variant per ADR 0008
// amendment 2026-07-06. Same convention.
func TestNewClauseSlotUseAt(t *testing.T) {
    u := query.NewClauseSlotUseAt(query.ClauseSlotSkip, 3)
    require.Equal(t, query.ClauseSlotSkip, u.Slot())
    require.Equal(t, 3, u.Part())

    out, err := json.Marshal(u)
    require.NoError(t, err)
    require.JSONEq(t,
        `{"kind":"clause-slot","slot":"skip","part":3}`,
        string(out))
}
```

**Note (implementer, re: hk0 §12 errata 2 lesson).** The expected
JSON in every new test above must match Go `encoding/json`'s
field ordering AND include EVERY field the marshalled struct
declares. `flatRef` has no `omitempty` on `Property` in the hk0
spec §5 errata; the same discipline applies here — every
`Marshal…JSON`'s composite struct declares its keys in a fixed
order, and the expected string must match the marshaller's output
exactly. Re-verify by running the seven new tests against the
compiled marshaller from a fresh worktree; do not hand-draw the
JSON string.

### 5.3 Fence 3 accounting (updates §4.1.3.2 Fence 3)

The Fence 3 statement in §4.1.3.2 says "the three existing
`TestPropertyUseMarshalJSON` / `TestExprUseMarshalJSON` /
`TestClauseSlotUseMarshalJSON` pins stay VERBATIM." That statement
is corrected here per §3.1.4: only `TestExprUseMarshalJSON`
pre-exists at branch base. The four zero-side tests in §5.1
(`TestNewPropertyUse`, `TestPropertyUseMarshalJSON`,
`TestNewClauseSlotUse`, `TestClauseSlotUseMarshalJSON`) are NEW
in the unfreeze PR — they land alongside the widened marshallers
and pin the zero side (`part = 0` key ABSENT). The pre-existing
`TestNewExprUse` and `TestExprUseMarshalJSON` stay VERBATIM.

Consequently the Fence 3 grep should include the seven new test
symbols alongside the two pre-existing ones:

```
go test -run 'TestMustParse|TestMustReject|TestNewPropertyUse|TestPropertyUseMarshalJSON|TestNewExprUse|TestExprUseMarshalJSON|TestNewClauseSlotUse|TestClauseSlotUseMarshalJSON|TestNewPropertyUseAt|TestNewExprUseAt|TestNewClauseSlotUseAt' ./internal/query/... -shuffle=on
```

The two pre-existing tests are byte-identical after the widening
(omit-when-zero, `part=0` absent); the seven new tests witness
both sides of the axis.

---

## 6. ADR 0008 amendment note — the dated stage note

The amendment lands at the top of ADR 0008, above the existing
hk0 amendment block (following the ADR 0003 stage-note convention
at `docs/adr/0003-curated-dialect-agnostic-query-model.md:3-7` —
newest amendments on top, older amendments below).

Verbatim text:

```
> _Amendment (2026-07-06, gqlc-fvo unfreeze cycle): the
> Use → Part attribution axis on `PropertyUse` / `ExprUse` /
> `ClauseSlotUse` — recorded implicitly as the R5 §4.2.4
> follow-up bead — is **adopted** under this ADR's
> additive-only revision protocol. The R5-shipped
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
> is **omit-when-zero-value** (`,omitempty`), following the
> post-freeze convention this ADR's hk0 amendment established
> for additive axes. The Use interface stays sealed at one
> method (`isUse()`); Part attribution is a per-variant
> field-and-accessor concern. See
> `docs/specs/unfreeze-fvo-use-part.md` for the full contract,
> the emission-site table, the zero-golden rebaseline
> accounting, the reversed alias-shadow discriminating fixture,
> and the semantic-diff-only fence commands._
```

The bullet in "Known deferred additions"
(`docs/adr/0008-query-model-freeze-resolver-api.md:193-208`) is
updated in the same PR — a new bullet lands alongside the hk0
close-out entry:

```
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
(§8) — the widening PR adds one new invalid fixture (§7.4) and
preserves every pre-existing fixture's byte-identity contract via
the enumeration in §7.5.

### 7.1 `witnessAcrossScopes` — the exact widening

Before (`internal/resolver/resolve.go:768-811`, verbatim per §3.4):

```go
func witnessAcrossScopes(u query.Use, scopes []partScope, s schema.Schema) ([]ResolvedType, error) {
    switch uu := u.(type) {
    case query.PropertyUse:
        ref := uu.Ref()
        out := make([]ResolvedType, 0, 1)
        var lastPropErr error
        containing := 0
        for _, sc := range scopes {
            if !scopeContains(sc, ref.Variable) {
                continue
            }
            containing++
            w, err := propertyUseWitness(ref, sc.nodeTypes, sc.edgeTypes, sc.edgeCands, sc.edgeBindings, sc.nullableBinding, s)
            if err != nil {
                if errors.Is(err, ErrUnknownProperty) {
                    lastPropErr = err
                    continue
                }
                return nil, err
            }
            out = append(out, w)
        }
        if containing > 0 && len(out) == 0 && lastPropErr != nil {
            return nil, lastPropErr
        }
        return out, nil
    case query.ClauseSlotUse:
        return []ResolvedType{ResolvedScalar{Kind: ScalarInt}}, nil
    case query.ExprUse:
        switch uu.Position() {
        case query.ExprInProjection, query.ExprInPredicate,
            query.ExprInSetValue, query.ExprInDeleteTarget:
            w, err := resolveType(uu.EnclosingType())
            if err != nil {
                return nil, err
            }
            return []ResolvedType{w}, nil
        default:
            return nil, fmt.Errorf("%w: unknown ExprUse position", ErrOutOfR0Scope)
        }
    default:
        return nil, fmt.Errorf("%w: unknown Use variant (%T)", ErrOutOfR0Scope, u)
    }
}
```

After (widened; the `PropertyUse` arm narrows to lexical-Part
witness; the ClauseSlotUse and ExprUse arms are unchanged in
semantics — Part-agnostic):

```go
// witnessAcrossScopes produces exactly one witness for a Use — the lexical
// Part attribution now recorded on the Use record (fvo per ADR 0008 amendment
// 2026-07-06) selects the scope. A PropertyUse witnesses against scopes[u.Part()]
// only; if that scope does not contain the Ref's variable, the caller receives
// zero witnesses (the unifier treats this as ResolvedUnknown, matching the
// pre-fvo behaviour for an unattributed Ref). If the scope contains the
// variable but the property lookup fails, the error surfaces immediately —
// the pre-fvo any-valid-witness swallowing is retired.
//
// ClauseSlotUse and ExprUse remain Part-agnostic in their type witness — the
// Part axis on their records is a lexical-attribution property for future
// consumer stages (§7.6), not a witness discriminator today.
func witnessAcrossScopes(u query.Use, branchScopes []partScope, s schema.Schema) ([]ResolvedType, error) {
    switch uu := u.(type) {
    case query.PropertyUse:
        ref := uu.Ref()
        idx := uu.Part()
        if idx < 0 || idx >= len(branchScopes) {
            // Defensive: the parser attributes to a valid branch-relative
            // index by construction (§4.2.1). An out-of-range index
            // indicates a decoder or model corruption; surface honestly.
            return nil, fmt.Errorf("%w: PropertyUse Part index %d out of range for branch with %d Parts", ErrOutOfR0Scope, idx, len(branchScopes))
        }
        sc := branchScopes[idx]
        if !scopeContains(sc, ref.Variable) {
            return nil, nil
        }
        w, err := propertyUseWitness(ref, sc.nodeTypes, sc.edgeTypes, sc.edgeCands, sc.edgeBindings, sc.nullableBinding, s)
        if err != nil {
            return nil, err
        }
        return []ResolvedType{w}, nil
    case query.ClauseSlotUse:
        return []ResolvedType{ResolvedScalar{Kind: ScalarInt}}, nil
    case query.ExprUse:
        switch uu.Position() {
        case query.ExprInProjection, query.ExprInPredicate,
            query.ExprInSetValue, query.ExprInDeleteTarget:
            w, err := resolveType(uu.EnclosingType())
            if err != nil {
                return nil, err
            }
            return []ResolvedType{w}, nil
        default:
            return nil, fmt.Errorf("%w: unknown ExprUse position", ErrOutOfR0Scope)
        }
    default:
        return nil, fmt.Errorf("%w: unknown Use variant (%T)", ErrOutOfR0Scope, u)
    }
}
```

**Two changes** in the `PropertyUse` arm, one change in the
signature:

1. **Signature** — `scopes []partScope` becomes
   `branchScopes []partScope`, the parameter naming clarifying
   that the caller now passes the current-branch scopes only.
2. **Loop removed** — no more per-scope iteration; direct index
   into `branchScopes[uu.Part()]`.
3. **Swallowing removed** — a per-scope `ErrUnknownProperty` now
   surfaces immediately (the previous "may be another Part"
   escape is retired; the Use's Part IS the attribution).

The R5 `"§4.2.4 any-valid-witness rule"` doc comment on the
function retires; the widened comment above records the new
lexical-Part semantics.

**Defensive out-of-range guard.** Under `omit-when-zero` decode,
a legacy golden's absent-`part` key decodes to `u.Part() == 0`,
which is always in-range for a non-empty branch (branches have
≥ 1 Part by the Query invariant at `query.go:26-32`). The
out-of-range guard fires only under wire corruption or a decoder
error — the "unreachable via parse" tripwire posture matches
`resolve.go:22-25`'s existing empty-branches guard.

### 7.2 `resolve` — cross-branch scope routing

The top-level `resolve` widens to route per-branch scopes:

Before (`internal/resolver/resolve.go:20-53`, verbatim per §3.5):

```go
branchCols := make([][]Column, len(q.Branches))
var mergedScopes []partScope

for b, branch := range q.Branches {
    cols, uses, err := resolveBranch(branch, s, r)
    if err != nil {
        return ValidatedQuery{}, err
    }
    branchCols[b] = cols
    mergedScopes = append(mergedScopes, useSitesToScopes(uses)...)
}

if err := compareBranchColumns(branchCols); err != nil {
    return ValidatedQuery{}, err
}

params, err := unifyParameterUsesAcrossScopes(q.Parameters, mergedScopes, s)
```

After:

```go
branchCols := make([][]Column, len(q.Branches))
var branchScopeTables [][]partScope

for b, branch := range q.Branches {
    cols, uses, err := resolveBranch(branch, s, r)
    if err != nil {
        return ValidatedQuery{}, err
    }
    branchCols[b] = cols
    branchScopeTables = append(branchScopeTables, useSitesToScopes(uses))
}

if err := compareBranchColumns(branchCols); err != nil {
    return ValidatedQuery{}, err
}

// fvo per ADR 0008 amendment 2026-07-06: witness each parameter Use against
// the scopes of its LEXICAL branch. The branch a Use belongs to is recovered
// by iterating branches and matching against the flat query.Parameters slice.
// The Use's Part index selects the scope within its branch.
params, err := unifyParameterUsesAcrossBranches(q.Parameters, q.Branches, branchScopeTables, s)
```

`unifyParameterUsesAcrossScopes` renames to
`unifyParameterUsesAcrossBranches` and gains the branch-router
logic. Its widened body (post-widening; the R5 body is at
`resolve.go:706-748` — pinned in §3.4):

```go
// unifyParameterUsesAcrossBranches walks each parameter's Uses, dispatches
// on the (branch, part) attribution recovered from the query structure, and
// witnesses against the exact scope. Uses of Part-agnostic variants
// (ClauseSlotUse / ExprUse) still witness in one scope arbitrarily — for
// them the Part axis is a lexical-attribution recorder, not a witness
// discriminator (§7.6).
func unifyParameterUsesAcrossBranches(params []query.Parameter, branches []query.Branch, tables [][]partScope, s schema.Schema) ([]ResolvedParameter, error) {
    if len(params) == 0 {
        return []ResolvedParameter{}, nil
    }

    // Build a name -> (branch index) map by walking every parameter node in
    // every branch's Parts and recording the branch it was found in first.
    // For a parameter appearing in multiple branches (UNION), each Use lives
    // in exactly one branch; the map records ONE branch per parameter, and
    // per-Use branch attribution comes from re-scanning at witness time.
    //
    // Simpler implementation for R5's zero-UNION-with-Uses corpus: assume
    // every Use is in branch 0. Under branch-base fixtures this is exactly
    // correct (§4.4.1: no UNION-with-Uses goldens). Widen the resolver only
    // when a corpus scenario demands cross-branch UNION attribution — this
    // matches the CLAUDE.md posture "don't design for hypothetical future
    // requirements". §7.2.1 records the design choice.

    out := make([]ResolvedParameter, 0, len(params))
    for _, p := range params {
        var unified ResolvedType
        seen := false
        for _, u := range p.Uses {
            // §7.2.1: witness against branch 0's scopes. Sound for every
            // branch-base fixture (no UNION-with-Uses). If a future
            // corpus scenario adds cross-branch parameter Uses, a new
            // unfreeze cycle adds a `branch int` axis on the Use record.
            branchScopes := tables[0]
            ws, err := witnessAcrossScopes(u, branchScopes, s)
            if err != nil {
                return nil, err
            }
            for _, w := range ws {
                if !seen {
                    unified = w
                    seen = true
                    continue
                }
                merged, ok := unify(unified, w)
                if !ok {
                    return nil, fmt.Errorf("%w: parameter %q: %s vs %s", ErrParameterTypeConflict, p.Name, unified.String(), w.String())
                }
                unified = merged
            }
        }
        if !seen {
            unified = ResolvedUnknown{}
        }
        out = append(out, ResolvedParameter{Name: p.Name, Type: unified})
    }
    return out, nil
}
```

The branch=0 hardcoding is honest: no branch-base fixture requires
otherwise. §7.2.1 records the follow-up bead that closes the
UNION-with-Uses case if a future TCK bump adds it.

#### 7.2.1 Deferred follow-up — cross-branch UNION attribution

**Recorded gap.** The widening at §7.2 above witnesses every Use
against branch 0's Part scopes. Under branch base, every Use lives
in branch 0 (§4.4.1: no goldens carry a Use inside a UNION
branch). Under a hypothetical UNION-with-Uses scenario, a Use in
branch 1 would witness against branch 0's scopes — potentially
false-admitting or false-rejecting.

**Why deferred, not closed.** ADR 0008's revision protocol asks
"is the change forced by the corpus?" Zero branch-base fixtures
force it. Closing the gap requires either (a) adding a
`branch int` axis to every Use record (a second cycle of this
kind), or (b) resolver-side branch reconstruction from parameter
nodes — the parser doesn't expose which branch's tree a `$` sits
in on the wire today. Both moves are speculative.

**File bead at close-out.** A new bead — `gqlc-fvo` sibling for
"UNION-with-Uses cross-branch attribution" — records the gap
without gating the fvo cycle. Follow-up spec cycle only when a
TCK scenario forces it.

### 7.3 R5 §4.2.4 prose update — verbatim

`docs/specs/resolver-stage-r5.md §4.2.4` — the
"any-valid-witness" prose retires. Verbatim after-shape (the
`### 4.2.4` heading and idealised-rule paragraph stay; the
"Actual rule" paragraph replaces):

```
**Actual rule (post-fvo) — lexical-Part witness.** `query.Query`'s
`Parameter.Uses` slice carries a Ref (variable + optional
property) AND a Part index (fvo per ADR 0008 amendment
2026-07-06 — see `docs/specs/unfreeze-fvo-use-part.md`). The
resolver's `witnessAcrossScopes` reads `u.Part()` and witnesses
against `branchScopes[u.Part()]` exactly once — the scope of the
Part the parser attributed the Use to at emission time. If the
scope does not contain the Ref's variable, the Use contributes
zero witnesses (treated as ResolvedUnknown by the unifier). If the
scope contains the variable but the property lookup fails,
`ErrUnknownProperty` surfaces immediately.

For a `PropertyUse` this discipline closes the pre-fvo
any-valid-witness soundness gap: a `MATCH (a:Post) WITH a.title
AS a MATCH (a:Person) WHERE a.title = $p` shape — where the
lexical Part 1 has `a: Person` (no `title`), but the pre-fvo
resolver would silently witness against Part 0's `a: Post` (which
has `title`) — now fires `ErrUnknownProperty` honestly. The
witness §7.4 of `docs/specs/unfreeze-fvo-use-part.md` pins the
discriminating fixture.

`ClauseSlotUse` and `ExprUse` remain Part-agnostic in their type
witness — the Part axis on their records is a lexical-attribution
recorder for future consumer stages, not a witness discriminator
today.
```

The "Why this is not perfectly sound" paragraph retires; the
"The soundness gap is a frozen-model deficiency, not a resolver
bug" paragraph retires. The R5 §7.1 audit-table row (line 2333)
updates: the "silently false-admitted" cell becomes "closed by
gqlc-fvo (2026-07-06)" and the remediation column points at
this spec.

The R5 §6.3 discriminating fixture list gains one line for the
reversed alias-shadow fixture (§7.4). The existing three fixtures
(`parameter_across_with_alias_shadow`,
`parameter_across_union_same_name`,
`parameter_across_with_multi_part`) stay verbatim — under
lexical-Part witness they all still admit (§7.5 verifies each).

### 7.4 New invalid fixture — the reversed alias-shadow

**Fixture query**
(`test/data/resolver/invalid/parameter_across_with_alias_shadow_reversed.cypher`):

```
MATCH (a:Post) WITH a.title AS a MATCH (a:Person) WHERE a.title = $p RETURN a
```

**Schema mapping entry** in
`test/data/resolver/invalid/schema.mapping.json`:

```
"parameter_across_with_alias_shadow_reversed.cypher": "social_r5.gql"
```

**Expected error** — `ErrUnknownProperty` with a message pinning
the Ref: `unknown property: a.title` (the sentinel is
existing; the fail-site is new).

**Semantic explanation.** Part 0 binds `a: Post`, which has
`title` STRING NOT NULL. WITH re-declares `a` as
`a.title AS a` — Part 1's `a` is a STRING projection alias, then
Part 1's MATCH re-binds `a: Person` (Stage 4 §4: re-declaration
is fresh at each Part). Part 1's WHERE reads `a.title = $p`. The
lexical Part of `$p` is Part 1 (the `mineWhere` runs from
`EnterOC_Match` at `listener.go:267-269`, which does NOT swap
`curPart`; the WHERE is inside Part 1's MATCH). Part 1's `a` is
`Person`, which has NO `title` property. Under lexical-Part
witness, the property lookup fires `ErrUnknownProperty`.

Under branch base's any-valid-witness rule, Part 0's `a: Post`
succeeds (has `title` STRING NOT NULL), so the query is silently
admitted with `$p → STRING NOT NULL`. This is the exact
false-admittance the R5 §4.2.4 audit row records; the fixture
witnesses the widening's honest rejection.

**Fixture placement note.** The fixture lands in
`test/data/resolver/invalid/`, not `valid/`. The R5-added
counterpart `parameter_across_with_alias_shadow.cypher` (a valid
fixture that admits under both rules) stays under `valid/` and
remains byte-identical (§7.5).

#### 7.4.1 Golden (invalid fixtures do not carry a JSON golden — sentinel-only)

Invalid fixtures pin the sentinel + message, not a full
ValidatedQuery. Follow the existing invalid-fixture discipline:
the acceptance harness reads a companion `.txt` or the sentinel
name from the fixture's leading comment. Verify the exact
mechanic at branch base by inspecting a peer fixture
(e.g. `test/data/resolver/invalid/parameter_type_conflict_two_properties.cypher`).
If the peer file uses a companion `.expected` file, follow the
same shape:

```
# parameter_across_with_alias_shadow_reversed.cypher.expected
ErrUnknownProperty: unknown property: a.title
```

Adjust the exact format to match the branch-base convention;
resolver test harness (`internal/resolver/*_test.go`) is the
authoritative source.

**Sentinel discipline** — no new sentinel. The widening admits a
fixture that R5 silently admitted (any-valid-witness) and now
honestly rejects. The rejection uses the existing
`ErrUnknownProperty` sentinel; the fail-site (a property lookup
inside `propertyUseWitness`) already raises this sentinel at
`internal/resolver/resolve.go:1296` (`propertyUseWitness`
function scope `:1292-1298`) and its neighbouring site at `:1313`.
No message-set widening required — the message
`"unknown property: <var>.<prop>"` matches the shape the widened
witness produces.

### 7.5 Byte-identity fence over the R5/R6/R7 resolver goldens

Every pre-existing resolver-valid golden — the enumeration at
branch base `origin/master @ baba282`: **118 fixture pairs**
(118 `*.cypher` sources + 118 `*.cypher.validated.golden.json`
outputs; refresh at branch base with
`ls test/data/resolver/valid/*.cypher | wc -l`), one shared
`schema.mapping.json`, and eight schema files under `schemas/`
(`social.gql` + `social_r1.gql` … `social_r7.gql`) — is
**byte-identical** after the widening PR merges.

**Enumeration of the potentially-affected valid fixtures and why
each stays byte-identical.**

The widening (§7.1) narrows `witnessAcrossScopes` from
any-valid-witness to lexical-Part witness in the `PropertyUse`
arm. A pre-existing valid golden STAYS byte-identical iff its
resolved parameter type under the widened rule equals its
resolved type under the R5-era rule. That is a wire-observable
claim on `ValidatedQuery.Parameters`.

**Class A — the three R5 §6.3 cross-Part parameter fixtures:**

- **`parameter_across_with_alias_shadow.cypher`** —
  `MATCH (a:Person) WITH a.name AS a MATCH (a:Post) WHERE a.title = $p RETURN a`.
  The `$p` sits in Part 1's MATCH's WHERE (Cursor-through §4.2.2:
  `mineWhere` from `EnterOC_Match` uses the current `curPart`,
  which is Part 1). Lexical Part = 1.
  - Pre-fvo: `a: Person` in Part 0 lacks `title`; `a: Post` in
    Part 1 has `title` STRING NOT NULL. Any-valid-witness admits
    with `$p → STRING NOT NULL`.
  - Post-fvo: witness against Part 1's scope only. Part 1's `a`
    is `Post`, has `title` STRING NOT NULL. Admits with
    `$p → STRING NOT NULL`.
  - Same wire outcome. **Golden byte-identical.**

- **`parameter_across_union_same_name.cypher`** —
  `MATCH (a:Person) RETURN a.id AS x UNION MATCH (a:Post) WHERE a.title = $p RETURN a.id AS x`.
  The `$p` sits in branch 1's MATCH's WHERE. Lexical Part
  (branch-relative) = 0 (single Part in branch 1). Under §7.2.1's
  branch-0-hardcoding: witness against branch 0's Part 0. But
  branch 0's `a` is `Person`, which has NO `title` — post-fvo
  fires `ErrUnknownProperty`.
  - **BEHAVIOUR CHANGE HAZARD.** This fixture would move from
    valid to invalid under §7.2.1's simplification.
  - **Mitigation options:**
    (a) Do NOT simplify §7.2.1: keep the fixture VALID by
        re-scanning the parameter nodes to identify each Use's
        branch. This preserves byte-identity for
        `parameter_across_union_same_name.cypher` at the cost of
        a more elaborate branch-recovery pass in
        `unifyParameterUsesAcrossBranches`.
    (b) Accept the fixture rebaselines: move it to
        `invalid/` under the widening PR, mirroring the reversed
        alias-shadow's move.

  **Ruling (this spec):** option (a). The fixture is part of the
  R5-committed valid corpus; changing its status would drag a
  named R5 acceptance criterion (the P2 scenario at R5 §4.2.4
  line 941) into the widening's scope, out-of-band with the
  minimal "one wire-observable change" fence hk0 codified. §7.2's
  widening replaces the `tables[0]` hardcoding with a
  branch-recovery pass; §7.2.2 pins the pass.

  Under option (a), the widening:
  - Pre-fvo: branch 0's `a: Person` witnesses (missing title) and
    branch 1's `a: Post` witnesses (has title STRING NOT NULL).
    Any-valid-witness swallows the branch-0 miss, admits with
    `$p → STRING NOT NULL`.
  - Post-fvo (option a): the Use is attributed to branch 1
    (via §7.2.2 recovery). Witness against branch 1's Part 0.
    Branch 1's `a` is `Post`, has `title`. Admits with
    `$p → STRING NOT NULL`.
  - Same wire outcome. **Golden byte-identical.**

- **`parameter_across_with_multi_part.cypher`** —
  `MATCH (a:Person) WHERE a.id = $p WITH a MATCH (a)-[e:AUTHORED]->(pst:Post) WHERE e.views = $p RETURN pst.title`.
  Two Uses of `$p`: Part 0's WHERE (Person.id: INT NOT NULL) and
  Part 1's WHERE (AUTHORED.views: INT NOT NULL). Both witness to
  the same type; the R2 lattice unifies. Same outcome pre- and
  post-fvo. **Golden byte-identical.**

**Class B — every other valid fixture** (115 files): parameter Uses
that appear in ONLY ONE Part scope, so any-valid-witness and
lexical-Part witness produce the same one witness. No wire change.
**Golden byte-identical.**

#### 7.2.2 The branch-recovery pass — supplement to §7.2

**Why it exists.** `parameter_across_union_same_name.cypher`
places the same parameter name in TWO branches under UNION. The
parser mints one `Parameter` record per name and appends one Use
per emission (see `addParameterUse` at `expr.go:633-642` — the
Parameter's `Uses` slice is append-only). So the query-wide
`Parameters` slice carries a single `Parameter{Name: <p>, Uses: [u_A, u_B]}`
where `u_A` was minted while parsing branch 0 and `u_B` while
parsing branch 1. To witness `u_A` against branch 0's Part scopes
and `u_B` against branch 1's, the widened resolver must recover
per-Use branch attribution from a query state that no longer
carries it explicitly. The recovery pass rebuilds it. Preserving
`parameter_across_union_same_name.cypher`'s byte-identity is a
Class A ruling in §7.5.

**Load-bearing emission-order invariant.** `addParameterUse`
(`expr.go:633-642`) records Uses on the shared Parameter in the
order the parser encounters emission sites during walk. UNION
branches are walked in source order (branch 0 fully before branch
1). Consequently, per-branch counters `count_b` (the number of
Uses minted while walking branch `b`) satisfy
`u_0, u_1, …, u_{count_0-1}` all live in branch 0, then
`u_{count_0}, …, u_{count_0 + count_1 - 1}` all live in branch 1,
and so on. The recovery pass exploits this: it does not match
Uses by their fields (which would be ambiguous under identical
same-name shapes), it matches by INDEX.

**Algorithm sketch** (the widening PR's code; this is the shape,
not the final implementation):

```go
// countUsesInBranch walks a branch's Parts in source order and counts,
// per Part, how many Uses that Part contributed via addParameterUse.
// Uses inside subordinate scopes (EXISTS body, quantifier body) are
// mined by their own hooks against the OUTER curPart, so their
// attribution is the outer Part — the count already reflects that.
func countUsesInBranch(b query.Branch, paramName string) []int { ... }

// witnessUsesInBranchOrder walks p.Uses in slice order, holding a
// cursor over the branch table. For branch 0, the first count_0 Uses
// are witnessed against branchScopes[0][u.Part()]; for branch 1, the
// next count_1 Uses are witnessed against branchScopes[1][u.Part()];
// and so on.
//
// Invariant (proven by construction of addParameterUse):
//   for i < count_0:              p.Uses[i]                       ∈ branch 0
//   for count_0 ≤ i < count_0+1:  p.Uses[i]                       ∈ branch 1
//   ...
// The cursor over p.Uses in branch-then-Part order recovers each
// Use's branch by INDEX, not by field match — so the corner case
// where branch 0 Part 0 and branch 1 Part 0 mint two structurally
// identical PropertyUse records is resolved by position.
```

**Why field-matching would be wrong.** Under
`parameter_across_union_same_name.cypher`, branch 0 and branch 1
can produce two Uses whose (variant, ref, position, part) tuple is
identical. A field-match recovery would collapse them into one
witness site and drop the second — the exact byte-identity break
the pass exists to prevent. Position-match recovery is
unambiguous because the emission order of `addParameterUse` is
deterministic and total.

Alternative — add a `branch int` field to Use (rejected in
§3.5) — remains available for a future cycle if the branch-
recovery pass proves brittle under a widened UNION corpus.

**Recorded risk.** The branch-recovery pass is a runtime cost with
a subtle invariant. §7.6 records it as a follow-up bead for
audit; the pass is the widening PR's judgment call, gated by
`parameter_across_union_same_name.cypher`'s byte-identity claim.

### 7.6 Sentinel discipline — no new sentinel, no message widening

The widening is a semantics refinement of an existing arm, not a
new fail-site. The reversed alias-shadow fixture (§7.4) fires the
existing `ErrUnknownProperty` sentinel. No sentinel additions; no
message-set widenings on any existing sentinel.

Recorded precedents (mirrored from hk0 §7.6):

- **R5** (`docs/specs/resolver-stage-r5.md §5.1`) added
  `ErrUnionColumnMismatch` — a new fail-site.
- **R5.1** (`§5.1.1`) added `ErrPartBindingTypeConflict` — same
  posture.
- **R6** (`docs/specs/resolver-stage-r6.md §5.1`) added
  `ErrInvalidEffectTarget`.
- **R7** (`docs/specs/resolver-stage-r7.md §5.1`) added zero
  sentinels; two message-set widenings on existing sentinels.
- **gqlc-hk0** (this campaign, PR #113) added zero sentinels.
- **gqlc-fvo** (this cycle) — zero sentinels, zero message
  widenings.

Discipline preserved: additions only where a new fail-site is
actually introduced. The fvo widening INTENSIFIES an existing
fail-site (Part 1's `a.title` on a Person now fails), it does
not INVENT one.

#### 7.6.1 Residual under lexical-Part attribution — WITH…WHERE aliased shadow

The Part-granular attribution axis this cycle adds records the
LEXICAL Part of a Use, not the SEMANTIC scope Cypher evaluates it
against. For WITH…WHERE these two coincide in every branch-base
resolver-valid fixture: the WITH's trailing WHERE is mined by
`mineWhere` from `EnterOC_With` (`listener.go:283-284`) BEFORE
the Part swap at `:290-293`, so the WHERE's PropertyUses attribute
to the CLOSED Part K — and in every non-shadowing shape the
binding at Part K is the same as the projected binding at Part
K+1.

**The corner case that survives.** A WITH that projects
`<var>.<prop> AS <var>` (aliasing the projected value back to the
same name) followed by a trailing WHERE that reads
`<var>.<other> = $p` is a shape where lexical-Part and
post-projection-scope diverge:

```
MATCH (a:Post) WITH a.title AS a WHERE a.x = $p RETURN a
```

**Mined behaviour** (fresh reads at branch base `baba282`):

- `EnterOC_With` (`listener.go:278-294`): before the swap,
  `curPart` = Part 0. `collectProjection` runs against Part 0
  (projecting `a.title AS a` — an ExprProjection or
  RefProjection depending on the classifier).
- `mineWhere(w)` runs against Part 0 (`listener.go:283-284`,
  BEFORE the swap at `:290`).
- `mineWhere` (`expr.go:444`) calls `mineComparisons` on the
  WHERE expression.
- `mineComparisons` (`expr.go:477`) recurses to the comparison
  `a.x = $p`; `mineComparison` (`expr.go:498`) then
  `pairOperands` (`expr.go:522`) then `pairAddSub` (`expr.go:529`).
- `pairAddSub`'s a→b arm (`:533-538`) fires:
  `propertyRefFromAddSub(a.x)` — a **syntactic** check that has
  no scope awareness — returns `Ref{"a", "x"}`.
  `parameterFromAddSub($p)` returns the `$p` node.
- Line 535 fires: `addParameterUse("p", node, NewPropertyUse(Ref{"a", "x"}))`.
  `curPart` is still Part 0. Under fvo,
  `l.currentPartIndex() = 0` — the PropertyUse attributes to
  Part 0.
- Post-fvo resolver: `witnessAcrossScopes` witnesses against
  Part 0's scope. Part 0's `a` = `Post`. If `Post` has `x`, the
  widened resolver ADMITS.

**Semantically**, Cypher's WITH…WHERE evaluates against the
POST-projection scope: `a` in the WHERE is the STRING projected
by `WITH a.title AS a`, so `a.x` is a member access on a STRING
— a semantic error the resolver should reject. Under fvo it does
NOT reject when Part 0's `a: Post` admits `x`.

**Regression assessment.** No regression versus branch base.
R5's any-valid-witness ALSO admits this shape (Part 0's
`a: Post` admits — done). So this residual is a lexical-attribution
LIMITATION of the fvo axis, not a widening REGRESSION.

**Why this cycle does not close it.** Closing it requires a
DIFFERENT axis: not "which Part does the Use lexically live in"
but "which scope does the Use semantically resolve against". That
scope on a WITH's trailing WHERE is the post-projection scope of
the CLOSED Part — an EXPORTED scope, not a bindings scope. R4's
`OptionalGroup` axis (gqlc-ay9, Cycle 4) and R5's carried-type
threading are relevant infrastructure. See §9 non-goals for the
follow-up bead.

**Fixture posture.** No branch-base resolver-valid golden
carries this shape. The discriminating fixture cited at
§7.4 (`parameter_across_with_alias_shadow_reversed.cypher`) is
the RE-BOUND shadow, not the aliased-projection shadow. Under
fvo the reversed fixture (Part 1 re-declares `a: Person`) DOES
witness the widening because the re-declared `a` in Part 1 lacks
`title` — a lexical-Part-scope hit. The aliased-projection
shadow above is a distinct shape whose Part-attribution and
semantic-attribution diverge, and fvo's axis cannot bridge that
divergence.

### 7.7 Follow-up beads flagged for close-out

- **UNION-with-Uses branch attribution** — §7.2.1 records the
  gap the branch-recovery pass leaves. File at close-out.
- **CALL-arg attribution** — R7 §7.1.1 records this as the CHILD
  of fvo. With fvo landed, the CALL-arg widening (adding
  `ExprInCallArg` to `ExprPosition` and a `Calls` axis on
  `query.Part`) becomes an in-protocol additive change. File at
  close-out under a NEW bead (do not re-open fvo).
- **ExprUse Part discrimination** — the ExprUse `Part()` axis is
  populated but unread by R5's resolver. A future stage may
  consume it (e.g. discriminating an `ExprUse` in Part 1's WHERE
  from one in Part 0's projection body when evaluating type
  compatibility across the WITH boundary). File at close-out
  if a downstream consumer emerges.

---

## 8. Mergeability and PR sequence

The gqlc-fvo campaign lands in three PRs, in this order:

1. **Spec PR** (this file). No code change. `just test` and
   `just lint-new` from the worktree are trivially green
   (docs-only).
2. **Unfreeze PR** — model + parser + ADR 0008 amendment. Resolver
   **untouched**; resolver test suite fully green. The specific
   fence:
   - `internal/query/query.go` gains three fields + three
     constructors + three accessors + three JSON keys.
   - `internal/query/type_test.go` adds SEVEN new tests: four
     zero-side tests filling the PropertyUse and ClauseSlotUse
     coverage gap (`TestNewPropertyUse`,
     `TestPropertyUseMarshalJSON`, `TestNewClauseSlotUse`,
     `TestClauseSlotUseMarshalJSON` — §5.1), and three
     non-zero-side tests (`TestNewPropertyUseAt`,
     `TestNewExprUseAt`, `TestNewClauseSlotUseAt` — §5.2). The
     two pre-existing tests (`TestNewExprUse` at
     `internal/query/type_test.go:73`, `TestExprUseMarshalJSON`
     at `:93`) stay verbatim per the omit-when-zero ruling.
   - `internal/query/cypher/expr.go` widens
     `addParameterUse` by one line and adds
     `currentPartIndex` + `attributePart`.
   - `internal/query/cypher/parser_test.go` — zero pins flip
     (§4.3: every use is in Part 0, Go zero-value equality
     preserves each assertion).
   - `docs/adr/0008-query-model-freeze-resolver-api.md` — the
     amendment note and the "Known deferred additions" edit.
   - `internal/resolver/*` — untouched. The 118 resolver
     `.cypher.validated.golden.json` fixtures stay byte-identical
     (no wire change in `ValidatedQuery`; the new field lives only
     on `query.Query`'s Use JSON, which the resolver does not
     marshal — verified in §7.5 for R5's cross-Part fixtures).
   - Parser goldens — 0 rebaselines under omit-when-zero.
   - `just test` from the worktree: PASSES.
   - `just lint-new` from the worktree: PASSES.
3. **Resolver-widening PR** — after PR 2 merges to master. Changes
   `internal/resolver/resolve.go:20-53` per §7.2 and
   `resolve.go:768-811` per §7.1; renames
   `unifyParameterUsesAcrossScopes` →
   `unifyParameterUsesAcrossBranches`; adds the branch-recovery
   helper per §7.2.2. Adds the reversed alias-shadow invalid
   fixture per §7.4. Updates the R5 §4.2.4 prose per §7.3.
   Every pre-existing fixture stays byte-identical per §7.5.
   `just test` from the worktree: PASSES.

**Cross-PR verification: PR 2's resolver-green claim.**

The resolver reads `query.Query` in memory (not JSON — `Resolve`
takes a `query.Query` value directly). Adding an unread `part`
field to each Use variant is invisible to every existing resolver
code path:

- `witnessAcrossScopes` at `resolve.go:750-811` — reads Uses via
  the type switch, ignores the new field. Behaviour unchanged.
- Every other resolver read of Uses (verified by
  `grep -n "PropertyUse\|ExprUse\|ClauseSlotUse" internal/resolver/*.go`)
  — enumerate at branch base; every hit is on the interface or
  on existing accessors, none consult `Part()`.

The resolver's JSON output (`ValidatedQuery.MarshalJSON`) does NOT
embed `query.Use` records — `ValidatedQuery.Parameters` is
`[]ResolvedParameter` (name + resolved type), not `[]Parameter`.
Verified in hk0 §8 by inspection of `internal/resolver/validated.go`;
same conclusion holds here. Resolver goldens stay byte-identical
across PR 2.

Confirmed: the resolver stays green through the unfreeze PR with
the field present but unconsumed.

---

## 9. Non-goals

The following are explicitly OUT OF SCOPE of the gqlc-fvo campaign:

| Bead | Subject | Why out of scope |
|---|---|---|
| gqlc-hk0 | ExprProjection.ContainsAggregate axis (Shape B) — CLOSED at PR #113 | Different axis (grouping-key discriminator on ExprProjection); different frozen-model gap. Cycle merged 2026-07-06. |
| gqlc-0ig | (as filed, R7 close-out — verify against `bd show`) | Filed alongside R7. Separate unfreeze cycle. |
| gqlc-ay9 | R4 nullability under-approximation Class A — OPTIONAL-clause-sibling | Different subsystem (nullability, not parameter attribution). Different frozen-model gap. |
| gqlc-5xg | R4 nullability under-approximation Class B — same-Part re-MATCH missing-witness | Different subsystem. Different frozen-model gap. |
| **Codegen** | Consumer of `ValidatedQuery` — codegen ADR post-R7 | ADR 0008 §Consequences: codegen is downstream of the resolver; the resolver-widening PR delivers the corrected parameter-Use attribution wire, and codegen consumes it under a future ADR. |
| **`Use.Branch` axis** | Adding a `branch int` field to each Use variant | §3.5 rejects the axis at this cycle: zero branch-base fixtures force it; the branch-recovery pass at §7.2.2 handles the corpus. If a future TCK bump adds a UNION-with-Uses scenario that defeats the pass, a NEW unfreeze bead adds the axis. |
| **`ExprInCallArg` position + Part.Calls axis** (R7's CALL-arg attribution — the CHILD of fvo per R7 §7.1.1) | R7's file-at-close-out follow-up | Deferred to a NEW bead (per R7 §7.1.1 recommendation). fvo lands the parent attribution primitive; the CALL-arg cycle builds on it. |
| **Widening the ClauseSlotUse or ExprUse witness on `Part()`** | Making the two Part-agnostic variants' witnesses vary by Part | §7.1 preserves the R5 semantics for both variants: they contribute a witness independent of any Part's scope. Widening either would be a semantic change on top of the additive axis; not in scope. Filed at close-out (§7.6) if a downstream consumer demands it. |
| **Semantic-scope attribution (WITH…WHERE aliased-shadow residual)** — file at close-out as candidate follow-up bead | The lexical-Part axis fvo adds diverges from Cypher's post-projection-scope evaluation for the aliased-projection shadow shape (§7.6.1). Under the shape `MATCH (a:Post) WITH a.title AS a WHERE a.x = $p RETURN a`, `$p`'s PropertyUse attributes to Part 0 (`a: Post`) where a member access on `x` may admit; semantically the WHERE evaluates against the post-projection `a: STRING`. | Closing this residual requires a scope-attribution axis (not a Part-attribution axis) — a distinct model change from fvo's. No branch-base resolver-valid golden exercises the shape; no regression versus branch base (any-valid-witness admits the same shape). Filed at close-out as a candidate follow-up; the widened resolver honestly records the residual in §7.6.1 and the ADR 0008 amendment (§6). |

**gqlc-fvo closes** at the resolver-widening PR merge:
- The bead moves to `closed`.
- The R5 §4.2.4 audit-table row (line 2333) resolves — the
  any-valid-witness workaround retires.
- The R5 §7.1 audit-table entry closes with a dated cross-reference
  to this spec, mirroring hk0's §7.1 close-out precedent.

---

## 10. Ground-truth cross-check

Every factual claim in this spec is verifiable against source; the
citations below name the file:line the claim rests on. Re-verify at
branch base `origin/master @ baba282` before writing code.

**Frozen `Use` variants (§3.1, §4.1):**

- `Use` interface: `internal/query/query.go:1310-1312`.
- `PropertyUse` struct: `internal/query/query.go:1319-1321`;
  constructor: `:1328-1330`; accessor: `:1333`; marker:
  `:1335`; MarshalJSON: `:1558-1564`.
- `ExprUse` struct: `internal/query/query.go:1428-1431`;
  constructor: `:1436-1438`; accessors: `:1442`, `:1445`;
  marker: `:1447`; MarshalJSON: `:1452-1458`.
- `ClauseSlotUse` struct: `internal/query/query.go:1463-1465`;
  constructor: `:1469-1471`; accessor: `:1474`; marker:
  `:1476`; MarshalJSON: `:1569-1574`.

**Use kind constants:**

- `useKindProperty`, `useKindClauseSlot`, `useKindExpr` at
  `internal/query/query.go:1481-1484`.

**Part construction lifecycle (§3.2, §4.2.2):**

- `EnterOC_SingleQuery` primer:
  `internal/query/cypher/listener.go:226-235`.
- `EnterOC_With` collect-then-swap:
  `internal/query/cypher/listener.go:278-294`.
- `EnterOC_Return` no-swap:
  `internal/query/cypher/listener.go:602-610`.
- `EnterOC_Union` combinator append:
  `internal/query/cypher/listener.go:243-252`.
- `EnterOC_StandaloneCall` primer:
  `internal/query/cypher/listener.go:588-599`.
- `EnterOC_ExistentialSubquery` param mining + suppression:
  `internal/query/cypher/listener.go:627-645`.
- `EnterOC_Match` — no swap; collects into current curPart:
  `internal/query/cypher/listener.go:261-269`.

**The 18 `addParameterUse` call sites (§3.3):**

- Enumerated by
  `grep -rn "addParameterUse" internal/query/cypher/`
  at branch base `baba282` (excluding the definition at
  `expr.go:633` and the doc-comment at `:627`). Distribution:
  `call.go` × 2 (`:64`, `:164`), `listener.go` × 2 (`:499`,
  `:637`), `typing.go` × 3 (`:445`, `:458`, `:873`),
  `expr.go` × 11 (`:131`, `:175`, `:403`, `:419`, `:458`,
  `:535`, `:542`, `:591`, `:607`, `:677`, `:707`) — total 18.
  Each site is inside one of the `Enter*` handlers above or a
  mining helper called from one. The wrapper itself is at
  `internal/query/cypher/expr.go:633-642`.

**Resolver R5 witness machinery (§3.4, §7.1):**

- `witnessAcrossScopes`: `internal/resolver/resolve.go:750-811`.
- Its sole call site: `internal/resolver/resolve.go:721`
  (`unifyParameterUsesAcrossScopes`).
- `scopeContains`: `internal/resolver/resolve.go:813-824`.
- `propertyUseWitness` function scope:
  `internal/resolver/resolve.go:1292-1298`; its first
  `ErrUnknownProperty` fail-site: `internal/resolver/resolve.go:1296`.
- `refProjectionType` (the OTHER `ErrUnknownProperty` fail-site
  the widening does not touch, used in projection typing not
  parameter witnessing): `internal/resolver/resolve.go:1133-1195`;
  its first `ErrUnknownProperty` fail-site: `:1140`.
- `partScope`: `internal/resolver/resolve.go:56-67`.
- `parameterUseSite`: `internal/resolver/resolve.go:110-113`.
- `useSitesToScopes` adapter: `internal/resolver/resolve.go:72-78`.
- `snapshotScope`: `internal/resolver/resolve.go:826-839`.
- Top-level `resolve` (branch loop + mergedScopes flat concat):
  `internal/resolver/resolve.go:20-53`.

**R5 §4.2.4 (§2, §3.7, §7.3):**

- Section body: `docs/specs/resolver-stage-r5.md §4.2.4` at
  lines 860-935.
- R5 §7.1 audit-table row (silently false-admitted): line 2333.
- R6 unchanged carry: `docs/specs/resolver-stage-r6.md §7.1.3`
  line 1994; audit-table row line 1886.
- R7 unchanged carry: `docs/specs/resolver-stage-r7.md` lines
  56-60; audit-table row line 2192.
- R7 §7.1.1 (fvo is parent of CALL-arg gap): lines 2278-2288.

**ADR 0008 (§6):**

- Freeze declaration:
  `docs/adr/0008-query-model-freeze-resolver-api.md:39-48`.
- Additive-only revision protocol: `:179-207`.
- hk0 amendment (top-of-file precedent for this cycle's
  amendment): `:3-37`.
- "Known deferred additions" list: `:193-207`.

**hk0 house convention (§3.6, §4.1.3):**

- Spec: `docs/specs/unfreeze-hk0-containsaggregate.md`, complete
  file. Section references quoted throughout this brief.

**Justfile recipes (§8):**

- `just test`: `justfile` — the full-suite recipe.
- `just lint-new`: `justfile` — the fast pre-push variant.

**EdgeBinding `directed` axis precedent (§3.6):**

- Field: `internal/query/query.go:378`.
- Constructor: `internal/query/query.go:388-393` (trailing param).
- Accessor: `internal/query/query.go:445-447`.
- Always-emit JSON: `internal/query/query.go:1524-1535`
  (`Directed bool json:"directed"` unconditional in the composite
  literal).

**Resolver `ValidatedQuery` marshalling (§7.5, §8):**

- `ValidatedQuery` MarshalJSON: `internal/resolver/validated.go`
  (verify at branch base). Fields: Columns, Parameters,
  Statement, Distinct. `Parameters` is `[]ResolvedParameter`
  (name + resolved type), NOT `[]query.Parameter`. The wire does
  not embed raw Use records.

**Cross-Part parameter fixture triple (§4.2.4, §7.5):**

- `test/data/resolver/valid/parameter_across_with_alias_shadow.cypher`
  + `.validated.golden.json` — 2-file pair.
- `test/data/resolver/valid/parameter_across_union_same_name.cypher`
  + `.validated.golden.json` — 2-file pair.
- `test/data/resolver/valid/parameter_across_with_multi_part.cypher`
  + `.validated.golden.json` — 2-file pair.
- Schema mapping: `test/data/resolver/valid/schema.mapping.json`
  lines 75-77.
- Schema: `test/data/resolver/valid/schemas/social_r5.gql`
  (Person / Post / Company + edges).

**Parser test pins carrying Use records (§4.3):**

- Enumerated by
  `grep -n "NewPropertyUse\|NewExprUse\|NewClauseSlotUse" internal/query/cypher/parser_test.go`
  at branch base `baba282`. 18 sites total (§4.3 table).

**Parser golden corpus size (§4.1.3.1, §4.4.1):**

- `ls internal/query/cypher/testdata/golden/*.golden.json | wc -l`
  → 3199 at branch base `baba282`.
- Discovery script for goldens with parameter Uses: §4.4.1.

**Resolver-valid fixture count (§7.5):**

- `ls test/data/resolver/valid/*.cypher | wc -l` → 118 at branch
  base `baba282`.

---

## 11. Definition of done for the spec cycle

This file exists on branch `unfreeze-fvo-spec` and is committed
with the message
`docs(spec): unfreeze fvo — Use→Part attribution`.

`just test` from the worktree passes (docs-only cycle; nothing
depends on this file compiling).

`just lint-new origin/master` from the worktree passes (docs-only
cycle; no lint targets touched).

Follow-up PRs (Cycles 2 and 3) reference this brief as their
implementation contract. The bead `gqlc-fvo` moves out of
`in_progress` only when the resolver-widening PR (Cycle 3) merges;
the spec PR does not close it.

---

## 12. Errata

None yet — this file lands as Cycle 1 of the gqlc-fvo unfreeze
campaign. Any defects surfaced during the implementation cycles
(PRs #N+1, #N+2 for the unfreeze and widening) land under this
section as dated close-out errata, following the hk0 §12
precedent.
