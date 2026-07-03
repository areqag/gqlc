# Stage 12 spec — Cypher query parser: write clauses

The implementation brief for Stage 12 of the Cypher implementation of
`query.Parser`. Twelfth model evolution after Stage 11 per ADR 0004
(test-first, evolving until feature-complete), under the curation
discipline of ADR 0003 and the type-interface boundary of ADR 0005.
Stage 12 is the seventh stage of the ADR-0007 pre-freeze expansion
beyond the read core. It **adds the four write clauses**:
`CREATE`, `DELETE` (with `DETACH`), `SET`, and `REMOVE`. `MERGE` is
its own stage (13) because the read/create alternation is a distinct
modelling decision.

Every write clause is a query-side **effect**: it does not project a
column, and a query composed only of writes is a legal complete query
that yields zero result columns. Stage 12 therefore relaxes the
Stage-0..11 invariant that every branch's final part ends in a
`RETURN` (a `RETURN *` or an explicit column list) — a projection-less
statement is now a first-class variant. Two structural additions to
`query.Query` follow: a **statement-kind** axis at the query level
(read vs. write, the axis driver code branches on to pick a
transaction mode), and an **effects list** per `Part`, holding the
write clauses in walk order alongside the part's `Bindings` and
`Returns`.

The typed-write contract — the core of the repository-pattern goal —
comes for free from Stage 6's existing rich-expression typer: a
`SET n.age = $newAge` types the value expression through
`typeExpressionMining`, records the SET item against the typed value,
and hangs an `ExprUse{sourceType, ExprInProjection}` on the parameter.
An inline `CREATE (p:Person {name: $name})` mines its map through the
existing `mineInlineMap` and produces a `PropertyUse{p, name}` — the
resolver upgrades $name's type from the schema's `Person.name` after
the freeze. No new parameter mining machinery is introduced; every
value expression rides an existing path.

This document is a **delta** against Stages 0–11 (referenced
individually where relevant); everything not stated here carries over
verbatim. Sections appear here only where Stage 12 changes something.

Tracking: bead `gqlc-mkv` (GitHub #43). Lands as one graphite branch
(`stage-12-writes`) with separated commits (spec + docs → parser red
→ parser green → TCK dir unlock + goldens), independently mergeable
as a whole: `just test` is green if this branch lands on `master`
alone (AGENTS.md stacked-branch invariant).

---

## 1. Deliverables

### 1.1 Statement-kind axis on `Query` (§3.1)

`query.Query` gains a `StatementKind` axis: `StatementRead` (the
default) or `StatementWrite` (the query contains at least one write
clause anywhere across its branches and parts). It is int-backed with
a stringer, mirroring `UnionKind` / `AggregateFunc` / `ClauseSlot`, so
the JSON tag (`"statementKind"`) derives from one source and cannot
drift.

**Two states, not three.** A query whose write clause is followed by
a `RETURN` (`CREATE (n) RETURN n`) is still a **write** — the driver's
transaction mode is binary (a `readTx` cannot execute a `CREATE`), so
"readwrite" collapses to "write" at the type-interface boundary. A
three-state axis would push the collapse into codegen without gaining
new information, and would leave undefined the case where a UNION
mixes a write branch with a read branch (openCypher rejects that
mixing at compile time via `InvalidClauseComposition`; the type
interface does not carry the rule, so an engine re-executing the
original text raises it — bucket 3). One axis derived from a whole-
query predicate keeps the model unambiguous.

**Wire encoding.** `Query`'s existing marshaller gains an emitted
`"statementKind"` string field, always emitted (default `"read"`),
matching the always-emit convention `combinators` / `parameters`
follow.

### 1.2 Write effects on `Part` (§3.2)

Every `Part` gains an ordered `Effects []Effect` slice, populated by
the walk in source order. `Effect` is a closed sum: five variants
covering the write-clause surface. The variants are

- `CreateEffect`: one `CREATE` clause; carries the ordered list of
  binding variable names the clause introduced. A `CREATE` clause is
  a pattern (identical grammar to `MATCH`'s), so its bindings enter
  the part's `Bindings` slice via the existing `collectPattern`
  path — a `CreateEffect` records only which of those bindings were
  introduced by this clause (the same variable may also appear in a
  prior `MATCH`, in which case the `CREATE` is a "match this, create
  what's missing" alternation the engine handles). The variant is
  otherwise purely a marker.
- `DeleteEffect`: one `DELETE` or `DETACH DELETE` clause; carries
  the ordered list of `Ref`s the clause names as targets and a
  `Detach bool` flag. Each expression the DELETE names is typed via
  `typeExpressionMining`; when the expression is a bare `var` or
  `var.prop` shape the Ref enters `Targets`; every other shape
  (arithmetic, function call, list index) types as a rich
  expression whose refs enter the part's ref list (so referential
  integrity holds) but the value's structure is below the boundary
  (ADR 0005) — the Effect carries a Refs slice for these,
  distinguished from Targets by shape. Every `$param` under a DELETE
  expression records `ExprUse{TypeUnknown, ExprInProjection}` — the
  parameter's type comes from the entity kind of the value it
  ultimately references, which the resolver upgrades from the schema.
- `SetPropertyEffect`: one SET item of the shape
  `propertyExpression = expression` — e.g. `SET n.age = 42`. Carries
  the property target (a `Ref{Variable, Property}`), the value's
  Stage-6 result type, and the `Refs` the value expression touched.
  Parameters under the value expression record
  `ExprUse{valueType, ExprInProjection}` — the typed-write contract.
- `SetEntityEffect`: one SET item of the shape `variable = expression`
  or `variable += expression` — a whole-entity replace / map-merge.
  Carries the target variable name, a `SetOp` axis
  (`SetOpReplace` for `=`, `SetOpMerge` for `+=`), the value's
  Stage-6 result type (typically `TypeMap` for a literal map, or
  `TypeUnknown` for `$param`), and the `Refs` the value touched.
- `SetLabelsEffect`: one SET item of the shape `variable :Labels` —
  e.g. `SET n:Foo:Bar`. Carries the target variable name and the
  labels as written (a `graph.LabelSet`).
- `RemovePropertyEffect`: one REMOVE item of shape `propertyExpression`
  — e.g. `REMOVE n.age`. Carries a `Ref{Variable, Property}`.
- `RemoveLabelsEffect`: one REMOVE item of shape `variable :Labels`
  — e.g. `REMOVE n:Foo`. Carries the target variable name and the
  labels.

Every Effect wire-encodes as a tagged union member discriminated by
`"kind"` (`"create"`, `"delete"`, `"setProperty"`, `"setEntity"`,
`"setLabels"`, `"removeProperty"`, `"removeLabels"`), matching the
`Binding` / `Projection` / `Use` marshal convention. One SET clause
with N items produces N Effects; one REMOVE clause with M items
produces M Effects. This preserves textual order (one item per Effect)
which is what a driver replaying the query needs.

**Effects and projection are orthogonal.** A part with Effects need
not have Returns / ReturnsAll; a part with Returns / ReturnsAll may
also have Effects. The read-core invariant "a final part ends in
RETURN and non-final parts end in WITH" relaxes to: a part is
**projection-less** iff `Effects` is non-empty AND `Returns` is
empty AND `ReturnsAll` is false. A branch's final part may be
projection-less; when it is, the branch produces zero result columns
and codegen emits a no-result method.

### 1.3 CREATE reuses the read-side pattern (§4.1)

`oC_Create`'s grammar is `CREATE oC_Pattern`, verbatim the same
non-terminal `oC_Match` uses. Stage 12 reuses `collectPattern` for
CREATE with a `writeCreate` flag threaded through: every binding
`collectPattern` accumulates enters `curPart.bindings` as if it were
a MATCH binding, and the same referential-integrity sweep in
`buildPart` covers CREATE-bound names verbatim. The CREATE-vs-MATCH
distinction — matter or effect? — is not carried on the binding, only
on the `CreateEffect` recording which bindings this clause introduced.

**Binding a name already bound.** `CREATE (n)` after `MATCH (n)`
reuses the MATCH-bound `n`, per openCypher (the pattern is anchored
to the matched name and creates nothing under it — a semantic detail
the type interface honours by simply routing through `mergeBinding`).
The CreateEffect records `n` in its variables list either way, so a
downstream consumer can distinguish "created here" from "matched
earlier" by cross-checking the ordered clause list — a coarser signal
than a per-binding annotation, and closer to what codegen actually
needs (per-clause is enough for the driver to know "this SET happens
after this CREATE").

**Anonymous CREATE bindings.** `CREATE ()-->()` creates two nodes and
one edge; the two anonymous nodes are pure filters on the read side
but are actual creations on the write side. They enter `Bindings` as
anonymous EdgeBindings today (an anonymous node is not a binding —
C3); Stage 12 preserves that discipline — the `CreateEffect` records
only the **named** bindings the clause introduced (the anonymous
edge case: variable is empty, so the effect's variable list carries
"" — which is legal per the Effect variant's semantics: a caller
wanting to inspect "which bindings were created here" iterates the
Effect's variables and skips empty strings). A future Stage 13 review
may consolidate this into a richer per-effect binding index; for
Stage 12 the coarse list is correct.

**Nullability.** `CREATE` never introduces a nullable binding — an
`OPTIONAL CREATE` is not openCypher grammar. The `collectPattern`
`optional` flag stays false at the CREATE call site.

### 1.4 DELETE typing (§4.2)

`oC_Delete` is `[DETACH] DELETE Expression (, Expression)*`. Each
expression is typed via `typeExpressionMining`; the shape falls into
two categories:

- **Bare `var` or `var.prop`.** The expression resolves via the
  existing `nonArithmeticAtom` / `refFromNonArithmetic` gates. The
  Ref enters `DeleteEffect.Targets` and the part's `curPart.refs`
  list (so referential integrity covers it). No parameter mining
  runs for this shape (a bare-atom expression has no parameter to
  mine).
- **Rich expression.** Anything else — a list index
  (`friends[$friendIndex]`), a function call, arithmetic. The
  expression's refs enter the Effect's `Refs` slice (also the part's
  ref list), and parameters record
  `ExprUse{TypeUnknown, ExprInProjection}` — the parameter's role
  is a delete target whose entity kind the parser cannot commit to
  schema-free, so `TypeUnknown` is the honest posture the resolver
  upgrades post-freeze.

The `Detach` flag mirrors the grammar's `DETACH` token: true for
`DETACH DELETE`, false for `DELETE`. openCypher's semantic
distinction — DETACH deletes an entity and every edge that touches
it, whereas plain DELETE requires the entity's edges to be already
gone — is a runtime rule (bucket 3, ADR 0007 §III), not a shape
Stage 12 encodes beyond preserving the axis.

### 1.5 SET typing (§4.3)

`oC_Set` is `SET SetItem (, SetItem)*`. Each SetItem is one of four
alternatives (§4.3 has the ANTLR shapes verbatim). Every SetItem
produces one Effect:

1. `propertyExpression = expression` → `SetPropertyEffect`. The
   propertyExpression must have exactly ONE property lookup (`n.age`,
   not `n.a.b`); a multi-level propertyExpression on the left-hand
   side is accepted-and-defer (bucket 3): the Ref records the
   variable and the **first** lookup name only, an honest single-level
   posture the resolver can distinguish from a valid single-level
   SET by cross-checking the original text. The pinned-tag TCK
   corpus does not exercise multi-level SET propertyExpression, so
   the accept-with-first-level-only posture is safe today; a future
   TCK bump that adds a multi-level case would need to revisit
   (recorded in §8).
2. `variable = expression` → `SetEntityEffect{op: SetOpReplace}`.
   The value expression is typed via `typeExpressionMining`; every
   parameter under it records
   `ExprUse{valueType, ExprInProjection}`.
3. `variable += expression` → `SetEntityEffect{op: SetOpMerge}`.
   Same typing as `SetOpReplace`; the operator distinction changes
   result semantics (replace vs. merge), which the model preserves.
4. `variable :Labels` → `SetLabelsEffect`. Labels are read via the
   existing `nodeLabels` helper (verbatim the reader-side path).

**Parameter typing under SET.** Every `$param` in a SET value
expression records `ExprUse{ enclosingType, ExprInProjection }` —
`ExprInProjection` because the SET value is a **producer** of a
value the engine writes to the graph, semantically closer to a
RETURN item's role than to a WHERE predicate's role. A future
refinement could split "projection" into "projection" and "write
value" for clarity, but the two roles collapse to the same
resolver logic (unify the parameter's type against the enclosing
expression's type), so the finer distinction adds no observable
information.

**References inside SET value expressions.** Rich SET values may
touch other bindings (`SET a.numbers = a.numbers + [4, 5]`). The
value expression's refs enter `curPart.refs` via the standard rich-
typer path so `buildPart`'s referential-integrity sweep covers them.
This is the same discipline `collectReturnItem`'s rich path uses.

### 1.6 REMOVE typing (§4.4)

`oC_Remove` is `REMOVE RemoveItem (, RemoveItem)*`. Each RemoveItem
is one of two alternatives:

1. `variable :Labels` → `RemoveLabelsEffect`. Labels via `nodeLabels`.
2. `propertyExpression` → `RemovePropertyEffect{Ref{var, prop}}`.
   Same single-level narrowing as SET §1.5 case 1: multi-level
   propertyExpression records the first lookup only.

REMOVE takes no value expression, so there is no parameter mining
under it beyond the variable's own ref (recorded onto `curPart.refs`
for referential integrity).

### 1.7 Retiring `ErrUnsupportedClause` for the write set

Stage 12 removes the four Enter handlers (`EnterOC_Create`,
`EnterOC_Delete`, `EnterOC_Set`, `EnterOC_Remove`) that today emit
`ErrUnsupportedClause: CREATE/DELETE/SET/REMOVE`. The sentinel stays
declared and reachable via UNWIND — wait, no: UNWIND retired at
Stage 9. The sentinel's remaining reach after Stage 12 is `MERGE`
(Stage 13) and `CALL` (Stage 14) — both still emit
`ErrUnsupportedClause`. The sentinel therefore stays in the
`unsupportedSentinels` list and in `allSentinels`; its fail-sites
shrink to `EnterOC_Merge`, `EnterOC_InQueryCall`, and
`EnterOC_StandaloneCall`.

The `mustReject` "write clause" pin currently uses
`CREATE (n) RETURN n` to exercise `ErrUnsupportedClause` (see
parser_test.go:1441). Stage 12 replaces it with `MERGE (n) RETURN n`
— MERGE stays unsupported through Stage 12 — preserving the
sentinel's reachability.

### 1.8 Sentinel status

`ErrUnsupportedClause` stays; the write set's fail-sites move onto
the parse-green path. No new sentinel is added. The four other
sentinels (`ErrUnsupportedParameter`, `ErrUnboundVariable`,
`ErrVariableKindConflict`, `ErrPatternInProjection`) are unchanged
in meaning and reach. `TestSentinelReachability` continues to run
against the five-sentinel set; the `mustReject` pin that reaches
`ErrUnsupportedClause` moves from CREATE to MERGE.

### 1.9 Corpus wiring

`readCoreDirs` gains **four** dirs:

- `clauses/create` — 6 feature files, 78 scenarios.
- `clauses/delete` — 6 feature files, 41 scenarios.
- `clauses/set` — 6 feature files, 53 scenarios.
- `clauses/remove` — 3 feature files, 33 scenarios.

Total ~205 scenario outlines (matches the bead's count). These dirs
are NOT under `expressions/*`, so `isBucketThreeDir` does not
categorically accept their negatives — every negative scenario needs
an explicit skiplist entry with a bucket-3 rationale, or must
actually reject with a real sentinel.

The negative scenarios cluster into:

- **Runtime-shape rules (bucket 3).** DELETE of a non-node/edge value
  (`DELETE 1 + 1` — `SyntaxError:InvalidDelete`); DELETE of a labelled
  variable (`DELETE n:Person` — the label predicate makes the value
  a boolean, not a graph entity, so `SyntaxError:InvalidDelete`).
  Both accept at the parser and route through the re-executed original
  text to the engine (ADR 0005).
- **Value/type constraints (bucket 3).** `SET n.x = <invalid value>`
  scenarios where the value expression's runtime type violates a
  storage rule (`SET a.name = missing` — `SyntaxError:UndefinedVariable`
  is a variable-scope rule bucket-3-eligible under the existing
  `isBucketThreeError` gate).
- **Composition rules (bucket 3).** Mixing writes with reading clauses
  in ways the engine rejects at compile time — `InvalidClauseComposition`.

Every negative not covered by a categorical rule needs a skiplist
entry. The precise list lands with the parser-green commit after a
red-lit survey pass; the spec commits to the shape (per-entry
rationale citing ADR 0007 bucket, sentinel absence, and TCK error
class), not the exact scenario names.

The acceptance-suite step `the result should be empty` (registered at
acceptance_test.go:572 today) currently expects an
`ErrUnsupported*`-PENDING outcome; Stage 12 changes its semantics to
"the query must have parsed AND the golden matches" — a projection-
less write no longer routes through PENDING. The `no side effects`
step follows the same transition. The `the side effects should be:`
step stays a no-op (parse-level assertion, per gqlc-39s: side-effect
tables are runtime assertions the parser does not carry).

### 1.10 Layer-2 pins

New `mustParse` cases exercising the Stage-12 shapes. Every entry is
verbatim from the TCK unless marked `// AUTHORED:` per the
parser_test.go layer-2 rule; authored cases are used for the
parameter-typed write shape which the pinned corpus does not
exercise (§1.5 discussed the finding).

- **CREATE bare node** (`Create1 [1]`): `CREATE ()` — a
  projection-less write, one anonymous NodeBinding, zero Returns,
  one CreateEffect with an empty-string variable, StatementWrite.
- **CREATE named node with label** (survey): `CREATE (n:Label)` —
  one named NodeBinding, one CreateEffect with `["n"]`.
- **CREATE followed by RETURN** (survey): `CREATE (n) RETURN n` —
  one NodeBinding, one CreateEffect, one RefProjection, StatementWrite.
- **DELETE named node** (`Delete1 [1]`): `MATCH (n) DELETE n` — one
  NodeBinding from MATCH, one DeleteEffect targeting `Ref{n}`,
  Detach=false, zero Returns.
- **DETACH DELETE named node** (`Delete1 [2]`): `MATCH (n) DETACH
  DELETE n` — same shape, Detach=true.
- **SET property to literal** (`Set1 [1]`-esque): a full
  `MATCH ... SET n.name = 'Michael' RETURN n` shape — one MATCH-
  binding, one SetPropertyEffect{Ref{n, name}, TypeString}, one
  RefProjection.
- **SET entity replace** (`Set4` survey): `MATCH (n) SET n = {a: 1}
  RETURN n` — one MATCH-binding, one SetEntityEffect{n, SetOpReplace,
  TypeMap}, one RefProjection.
- **SET labels** (`Set5` survey): `MATCH (n) SET n:Foo RETURN n` —
  one SetLabelsEffect{n, ["Foo"]}, one RefProjection.
- **REMOVE property** (`Remove1 [1]`-esque): `MATCH (n) REMOVE n.num`
  — one MATCH-binding, one RemovePropertyEffect{Ref{n, num}}, zero
  Returns.
- **REMOVE labels** (`Remove2 [1]`-esque): `MATCH (n) REMOVE n:L` —
  one MATCH-binding, one RemoveLabelsEffect{n, ["L"]}.
- **AUTHORED: CREATE with inline-map $param** —
  `CREATE (p:Person {name: $name})` — pins the typed-Create story:
  one NodeBinding p, one CreateEffect ["p"], one Parameter `name`
  with PropertyUse{Ref{p, name}}, StatementWrite. No verbatim
  corpus query exercises this shape at the pinned tag (grep
  confirmed zero `\$` inside a CREATE inline map).
- **AUTHORED: SET property with $param on the RHS** —
  `MATCH (n) SET n.age = $newAge RETURN n` — pins the typed-Update
  story: one Parameter `newAge` with ExprUse{TypeUnknown,
  ExprInProjection} (TypeUnknown because a bare `$newAge` has no
  enclosing arithmetic that pins a concrete type; the resolver
  upgrades from `n.age` via the schema post-freeze). No verbatim
  corpus query exercises this shape at the pinned tag.

Approximate count: 11 new `mustParse` pins; the number is a ceiling
committed by the spec (verbatim-corpus rule caps at what the corpus
supplies, ≤ 8 verbatim + 2 authored). The final count is chosen
against the pinned-tag corpus once the parser is red-lit.

Updated `mustReject` case:

- **write clause** (moved from CREATE to MERGE): `MERGE (n) RETURN n`
  — Stage 12 retires CREATE from `ErrUnsupportedClause`'s reach;
  MERGE preserves the sentinel's reachability until Stage 13 (the
  MERGE dedicated stage).

`count`s update summary:

- `mustParse`: 75 → up to 86 (11 new pins for Stage 12 shapes; the
  authored count is capped at 2).
- `mustReject`: 13 → 13 (one pin replaced in-place: CREATE → MERGE).
- Sentinels: 5 → 5 (no additions, no retirements).

### 1.11 Docs inline

- This spec.
- ADR 0003 gains a Stage-12 amendment note: `Query` gains
  a `StatementKind` axis (read vs. write, the driver's transaction
  mode is a query-wide fact); `Part` gains an `Effects []Effect`
  slice (the write clauses of that part, in walk order); the
  invariant "every part ends in a projection" relaxes to "every
  part has a projection or at least one effect"; the `Effect` sum
  is closed at five variants (create / delete / setProperty /
  setEntity / setLabels / removeProperty / removeLabels — spelled
  out; the two "set entity" cases collapse into one variant with a
  SetOp axis). No new `Type` sum variant is added. No new
  `Projection` sum variant is added.
- ADR 0007 already names Stage 12 (write clauses); no header change.
- CONTEXT.md gains three new entries — **Statement**, **Effect**,
  **Write clause** — describing the axis, the sum, and the
  clause-to-effect mapping. The existing **Query** and **Query
  part** entries pick up notes on the projection-less relaxation
  and the effects list. The **Parameter** entry picks up a note on
  the typed-write role (`$param` in SET value expressions and in
  CREATE inline maps).

Nothing downstream of the parser is built (no resolver, no codegen)
— ADR 0004.

---

## 2. Why one atomic cycle

The four write clauses share the same three questions and the same
answers: does the clause introduce a binding (CREATE yes, the
others no), does it produce a value column (none of them do), and
does its parameter-mining ride the Stage-6 rich typer (yes, all
four). Splitting CREATE from DELETE from SET from REMOVE would
leave the model in a state where `Query.StatementKind` is defined
but only one of the four clauses actually flips it — a wrong split.
Splitting the model changes (statement kind + effects list) from
the clause enablement would land a wire shape with no fail-site
exercising it — untestable. Stage 12 lands as one branch.

Within the branch, the commit inventory (§7) separates spec from
red pins from parser changes from goldens so review can proceed
incrementally without re-running the whole diff at each step.

---

## 3. Model shape

### 3.1 `StatementKind` axis on `Query`

```go
type StatementKind int

const (
    StatementRead StatementKind = iota
    StatementWrite
)

func (k StatementKind) String() string {
    if k == StatementWrite {
        return "write"
    }
    return "read"
}

func (k StatementKind) MarshalJSON() ([]byte, error) {
    return json.Marshal(k.String())
}
```

Mirrors `UnionKind`. Added to `Query` as a public field:

```go
type Query struct {
    Branches     []Branch      `json:"branches"`
    Combinators  []UnionKind   `json:"combinators"`
    Parameters   []Parameter   `json:"parameters"`
    StatementKind StatementKind `json:"statementKind"`
}
```

The default (zero-value) is `StatementRead`, so every pre-Stage-12
golden emits `"statementKind": "read"` after a `-update` pass — the
wire shape is a strict additive extension.

### 3.2 `Effect` sum and `Part.Effects`

```go
type Effect interface {
    isEffect()
}

type CreateEffect struct {
    variables []string
}
func NewCreateEffect(variables []string) CreateEffect { ... }
func (e CreateEffect) Variables() []string { return e.variables }
func (CreateEffect) isEffect() {}

type DeleteEffect struct {
    targets []Ref
    refs    []Ref
    detach  bool
}
func NewDeleteEffect(targets, refs []Ref, detach bool) DeleteEffect { ... }
func (e DeleteEffect) Targets() []Ref { return e.targets }
func (e DeleteEffect) Refs() []Ref { return e.refs }
func (e DeleteEffect) Detach() bool { return e.detach }
func (DeleteEffect) isEffect() {}

type SetOp int
const (
    SetOpReplace SetOp = iota // =
    SetOpMerge                // +=
)
func (o SetOp) String() string { ... }
func (o SetOp) MarshalJSON() ([]byte, error) { ... }

type SetPropertyEffect struct {
    target     Ref
    valueType  Type
    refs       []Ref
}
func NewSetPropertyEffect(target Ref, valueType Type, refs []Ref) SetPropertyEffect { ... }
func (e SetPropertyEffect) Target() Ref { return e.target }
func (e SetPropertyEffect) ValueType() Type { return e.valueType }
func (e SetPropertyEffect) Refs() []Ref { return e.refs }
func (SetPropertyEffect) isEffect() {}

type SetEntityEffect struct {
    targetVar string
    op        SetOp
    valueType Type
    refs      []Ref
}
// similar accessors

type SetLabelsEffect struct {
    targetVar string
    labels    graph.LabelSet
}
// similar accessors

type RemovePropertyEffect struct {
    target Ref
}
// accessors

type RemoveLabelsEffect struct {
    targetVar string
    labels    graph.LabelSet
}
// accessors
```

Every constructor is a smart constructor that rejects the empty
target variable (an empty `Variable` on a Ref target is a parser bug,
so `NewSetPropertyEffect` returns an error if `target.Variable == ""`),
matching the discipline of Stage 8's `NewPathBinding` / Stage 9's
`NewUnwindBinding`. `Refs` slices may be nil.

`Part` grows one field:

```go
type Part struct {
    Bindings   []Binding    `json:"bindings"`
    Returns    []ReturnItem `json:"returns"`
    ReturnsAll bool         `json:"returnsAll"`
    Effects    []Effect     `json:"effects"`
}
```

Effects is always emitted (an empty slice, `null`, or the effect
array — matches the always-emit convention).

**Invariant relaxed.** buildPart's implicit assumption "every part
has some projection" (the read-core rule) relaxes to: a part is valid
iff it carries at least one binding OR at least one effect OR a
projection (Returns non-empty or ReturnsAll). A part with only
Effects (no bindings, no returns) is a legal shape — e.g. a part in
a chain where WITH exports names into the next part, then the next
part is a pure `SET n.x = ...` write. The zero-projection zero-binding
zero-effect Part is still invalid (it would parse only from a truly
empty query fragment, which the grammar rejects).

### 3.3 Effect wire encoding

Each Effect variant marshals as `{"kind": <tag>, ...}`. Tags:

- `CreateEffect`: `"create"`, `variables: [string...]`.
- `DeleteEffect`: `"delete"`, `targets: [flatRef...]`, `refs: [flatRef...]`, `detach: bool`.
- `SetPropertyEffect`: `"setProperty"`, `target: flatRef`, `type: <Type>`, `refs: [flatRef...]`.
- `SetEntityEffect`: `"setEntity"`, `variable: string`, `op: "replace"|"merge"`, `type: <Type>`, `refs: [flatRef...]`.
- `SetLabelsEffect`: `"setLabels"`, `variable: string`, `labels: [string...]`.
- `RemovePropertyEffect`: `"removeProperty"`, `target: flatRef`.
- `RemoveLabelsEffect`: `"removeLabels"`, `variable: string`, `labels: [string...]`.

Effect constants sit next to the other kind constants at the bottom
of `internal/query/query.go` (§3.2 of the Stage-8 spec's convention):

```go
const (
    effectKindCreate         = "create"
    effectKindDelete         = "delete"
    effectKindSetProperty    = "setProperty"
    effectKindSetEntity      = "setEntity"
    effectKindSetLabels      = "setLabels"
    effectKindRemoveProperty = "removeProperty"
    effectKindRemoveLabels   = "removeLabels"
)
```

### 3.4 Listener state additions

`rawPart` grows one field:

```go
type rawPart struct {
    // ... existing fields
    effects []query.Effect
}
```

No new depth counter or scope stack — write clauses live at the
same walk depth as MATCH / WITH / RETURN and share the same
suppression discipline (§4.5): they early-return when
`subqueryDepth > 0`, so a write inside an EXISTS subquery is
suppressed at the parser boundary (Stage 11 §1.6 already documents
this).

---

## 4. Parser widening

### 4.1 EnterOC_Create replaces its rejection with pattern collection

```go
func (l *listener) EnterOC_Create(c *gen.OC_CreateContext) {
    if l.subqueryDepth > 0 {
        return // Stage 11 §1.6: writes inside EXISTS { ... } are suppressed
    }
    before := len(l.curPart.bindings)
    l.collectPattern(c.OC_Pattern(), false)
    if l.err != nil {
        return
    }
    var vars []string
    for i := before; i < len(l.curPart.bindings); i++ {
        vars = append(vars, l.curPart.bindings[i].variable)
    }
    l.curPart.effects = append(l.curPart.effects, query.NewCreateEffect(vars))
    l.markWrite()
}
```

`markWrite` (new helper on the listener) sets a `writeSeen` bool
that `build()` reads to populate `Query.StatementKind`. `writeSeen`
starts false; every write-clause Enter handler flips it to true.

**Anonymous binding capture.** The delta `before..len(bindings)`
captures every raw binding this CREATE introduced, named or
anonymous (an anonymous edge or an anonymous node the pattern
composed). The `vars` slice thus carries empty strings for anonymous
positions — which is correct: the CreateEffect records "this clause
created N entities, K of which are named," and iteration on the
Effect walks the actual creation set.

The bindings themselves stay in `curPart.bindings`; downstream
referential integrity, refType, and export logic work verbatim.

### 4.2 EnterOC_Delete parses targets

```go
func (l *listener) EnterOC_Delete(c *gen.OC_DeleteContext) {
    if l.subqueryDepth > 0 {
        return
    }
    detach := c.DETACH() != nil
    var targets, refs []query.Ref
    for _, e := range c.AllOC_Expression() {
        // Try the bare var / var.prop shape first.
        if nae := nonArithmeticAtom(e); nae != nil {
            if ref, ok := refFromNonArithmetic(nae); ok {
                targets = append(targets, ref)
                l.curPart.refs = append(l.curPart.refs, varRef{name: ref.Variable})
                continue
            }
        }
        // Rich shape: mine refs and parameters.
        _, expRefs, params := l.typeExpressionMining(e)
        refs = append(refs, expRefs...)
        for _, p := range params {
            name := parameterName(p)
            if name == "" {
                continue
            }
            l.addParameterUse(name, p, query.NewExprUse(query.TypeUnknown{}, query.ExprInProjection))
        }
    }
    eff, err := query.NewDeleteEffect(targets, refs, detach)
    if err != nil {
        l.fail(err)
        return
    }
    l.curPart.effects = append(l.curPart.effects, eff)
    l.markWrite()
}
```

`typeExpressionMining` already pushes rich-expression refs onto
`curPart.refs`, so no separate ref-append pass is needed for the
rich case; the local `expRefs` copy is what the Effect records.

### 4.3 EnterOC_Set

```go
func (l *listener) EnterOC_Set(c *gen.OC_SetContext) {
    if l.subqueryDepth > 0 {
        return
    }
    for _, item := range c.AllOC_SetItem() {
        l.collectSetItem(item)
        if l.err != nil {
            return
        }
    }
    l.markWrite()
}

func (l *listener) collectSetItem(item gen.IOC_SetItemContext) {
    switch {
    case item.OC_PropertyExpression() != nil && item.OC_Expression() != nil:
        // propertyExpression = expression
        target, ok := propertyExpressionRef(item.OC_PropertyExpression())
        if !ok {
            // multi-level or unrecognised shape — bucket-3 accept-and-defer:
            // record the leftmost single-level view (the atom's variable, no
            // property) so referential integrity holds against the variable;
            // the runtime raises against the multi-level shape (ADR 0005).
            target = leftmostRef(item.OC_PropertyExpression())
        }
        l.curPart.refs = append(l.curPart.refs, varRef{name: target.Variable})
        valueType, refs, params := l.typeExpressionMining(item.OC_Expression())
        for _, p := range params {
            name := parameterName(p)
            if name == "" {
                continue
            }
            l.addParameterUse(name, p, query.NewExprUse(valueType, query.ExprInProjection))
        }
        eff, err := query.NewSetPropertyEffect(target, valueType, refs)
        if err != nil {
            l.fail(err)
            return
        }
        l.curPart.effects = append(l.curPart.effects, eff)

    case item.OC_Variable() != nil && item.OC_NodeLabels() != nil:
        // variable :Labels
        variable := item.OC_Variable().GetText()
        l.curPart.refs = append(l.curPart.refs, varRef{name: variable})
        labels := nodeLabels(item.OC_NodeLabels())
        eff, err := query.NewSetLabelsEffect(variable, labels)
        if err != nil {
            l.fail(err)
            return
        }
        l.curPart.effects = append(l.curPart.effects, eff)

    case item.OC_Variable() != nil && item.OC_Expression() != nil:
        // variable = expression OR variable += expression
        // The grammar's SetItem alternatives 2 and 3 both match this branch;
        // the '=' vs '+=' token is inspected via the item's direct children
        // (T__2 is '=', T__3 is '+=').
        variable := item.OC_Variable().GetText()
        l.curPart.refs = append(l.curPart.refs, varRef{name: variable})
        op := setItemOp(item) // reads the T__2/T__3 terminal, defaults to Replace
        valueType, refs, params := l.typeExpressionMining(item.OC_Expression())
        for _, p := range params {
            name := parameterName(p)
            if name == "" {
                continue
            }
            l.addParameterUse(name, p, query.NewExprUse(valueType, query.ExprInProjection))
        }
        eff, err := query.NewSetEntityEffect(variable, op, valueType, refs)
        if err != nil {
            l.fail(err)
            return
        }
        l.curPart.effects = append(l.curPart.effects, eff)
    }
}
```

`propertyExpressionRef` is a new helper in `shape.go`: it reads a
propertyExpression that has exactly one lookup and returns the
`Ref{Variable, Property}`; ok is false for any other shape
(multi-level, missing variable, atom that isn't a variable).

`leftmostRef` extracts the atom's variable as a bare Ref
(Property empty); used only in the multi-level bucket-3 fallback.

`setItemOp` walks the SetItem's direct children and returns
`SetOpReplace` for a `=` terminal (T__2), `SetOpMerge` for `+=`
(T__3). Defaults to Replace when neither terminal is found (which
the grammar rules out; the default is defensive).

### 4.4 EnterOC_Remove

```go
func (l *listener) EnterOC_Remove(c *gen.OC_RemoveContext) {
    if l.subqueryDepth > 0 {
        return
    }
    for _, item := range c.AllOC_RemoveItem() {
        l.collectRemoveItem(item)
        if l.err != nil {
            return
        }
    }
    l.markWrite()
}

func (l *listener) collectRemoveItem(item gen.IOC_RemoveItemContext) {
    if item.OC_Variable() != nil && item.OC_NodeLabels() != nil {
        variable := item.OC_Variable().GetText()
        l.curPart.refs = append(l.curPart.refs, varRef{name: variable})
        labels := nodeLabels(item.OC_NodeLabels())
        eff, err := query.NewRemoveLabelsEffect(variable, labels)
        if err != nil {
            l.fail(err)
            return
        }
        l.curPart.effects = append(l.curPart.effects, eff)
        return
    }
    if pe := item.OC_PropertyExpression(); pe != nil {
        target, ok := propertyExpressionRef(pe)
        if !ok {
            target = leftmostRef(pe)
        }
        l.curPart.refs = append(l.curPart.refs, varRef{name: target.Variable})
        eff, err := query.NewRemovePropertyEffect(target)
        if err != nil {
            l.fail(err)
            return
        }
        l.curPart.effects = append(l.curPart.effects, eff)
        return
    }
}
```

### 4.5 EXISTS-subquery suppression preserved

Stage 11's suppression counter on the four write-clause Enter
handlers stays in place — the top-of-handler `if l.subqueryDepth > 0
{ return }` early-return is verbatim what the current code does.
Inside EXISTS a write clause parses (Stage 11 §1.6 rationale
unchanged); at outer scope, Stage 12 replaces the previous
`l.fail(ErrUnsupportedClause: ...)` line with the collection code
above.

### 4.6 `buildPart` relaxes the projection invariant

The check `part.Bindings + part.Effects + (Returns or ReturnsAll)`
must have at least one non-empty. In practice `buildPart` today
already permits an empty `Returns` with `ReturnsAll=true`, and a
part with zero bindings would fail referential-integrity on any
downstream ref — so the only new admitted shape is "at least one
Effect and no projection." Formally: no test in `buildPart` is
removed; the new admission is a consequence of the field being
optional.

The exported-names set (`buildBranch`'s left-to-right threading)
falls through unchanged: a projection-less part exports the same
set as a part with only its own bindings — its Bindings' variables
carry into the next part's scope only via a WITH, which a
projection-less part does not have. So a projection-less part
exports the empty set (no imported names, no returns to name), and
the next part starts fresh.

### 4.7 `build()` populates `StatementKind`

```go
func (l *listener) build() (query.Query, error) {
    // ... existing branch/part assembly ...
    q := query.Query{Branches: branches}
    // ... combinators, params ...
    if l.writeSeen {
        q.StatementKind = query.StatementWrite
    }
    return q, nil
}
```

### 4.8 `nameBoundAsUnwind` interaction with CREATE

CREATE reuses `collectPattern`, which calls `nameBoundAsUnwind` for
UNWIND-bound names before appending a new binding. For a CREATE
under a prior UNWIND that bound a variable to a scalar, this yields
the same byVar-collision → ErrVariableKindConflict path Stage 9
already documents (§1.5 pattern-vs-unwind). Stage 12 does not
change this — a CREATE naming a scalar-bound UNWIND variable rejects
the same way a MATCH would.

---

## 5. Corpus and bucket-3 skiplist

`clauses/{create,delete,set,remove}` enter `readCoreDirs`. These
dirs are NOT under `expressions/*`, so `isBucketThreeDir` does not
categorically accept their negatives; each negative scenario needs
either an explicit skiplist entry (bucket-3 pinned) or a real
rejection.

Common categories the negatives cluster into:

- **`SyntaxError:InvalidDelete`.** `DELETE 1 + 1`, `DELETE n:Person`
  — a delete target whose runtime type is not a node or edge. The
  parser accepts (the expression rides `typeExpressionMining`); the
  engine raises. Bucket 3, per-scenario entry.
- **`SyntaxError:InvalidArgumentType`.** SET / REMOVE targeting a
  value whose runtime type violates the storage rule. Bucket 3, per
  the existing `isBucketThreeError` gate (already lists
  `InvalidArgumentType`).
- **`SyntaxError:UndefinedVariable`.** Value-scope rules on the
  right-hand side of SET, or delete targets naming an out-of-scope
  variable. Bucket 3 per `isBucketThreeError`.

Any negative citing a SyntaxError with a parse-shape detail
(`UnexpectedSyntax`, `InvalidClauseComposition`, and similar) is
parser-owned and must reject (or an explicit skiplist entry with
justification). The precise enumeration lands with the parser-green
commit after a survey pass; the spec commits to the enumeration
shape (one entry per scenario, rationale citing ADR 0007 bucket 3
and the TCK error class), not the exact list.

The existing skiplist entries covering non-write scenarios are all
unchanged. The Stage 11 `[3] Full existential subquery with update
clause should fail` entry stays (a `SET` inside an `EXISTS` — the
outer suppression still handles it, unchanged).

The `mustReject` `write clause` pin moves from CREATE to MERGE:

```go
"write clause": {
    query: "MERGE (n)\nRETURN n",
    want:  cypher.ErrUnsupportedClause,
},
```

---

## 6. Definition of done for Stage 12

1. `stage-12-writes` lands green and independently mergeable;
   `master` is green if it lands solo.
2. `just test` green: query-package unit tests (unchanged shape of
   the read-core sums; additive `StatementKind`, `Effect`,
   `Part.Effects`), the cypher-package parser tests, the
   `mustParse` pins, the acceptance / orphan / reachability suites,
   the property tests.
3. `just lint` green: zero issues.
4. `just fmt-check` green: zero diffs.
5. Layer-1 godog count rises by the four write dirs' ~205
   scenarios, less the bucket-3 categorical/skiplist negatives.
   Zero FAIL is mandatory.
6. Documentation: this spec; CONTEXT.md entries for **Statement**,
   **Effect**, **Write clause**; ADR 0003 amendment note.
7. Beads: `gqlc-mkv` closed.

---

## 7. Commit inventory (single branch `stage-12-writes`)

| Commit | Scope |
|--------|-------|
| prep / spec | this spec + ADR 0003 note + CONTEXT.md entries (docs land in the branch, matching the DoD) |
| parser (red) | Failing `mustParse` pins for the Stage-12 shapes (CREATE binds, DETACH DELETE, SET prop-and-param, SET labels, REMOVE prop/labels, plus two authored parameter-typed pins); `mustReject` pin swapped CREATE→MERGE. `query.StatementKind`, `query.Effect` sum, `query.Part.Effects` added but Enter handlers still emit ErrUnsupportedClause, so the pins fail |
| parser (green) | Enter handlers for Create/Delete/Set/Remove parse into effects; `SetItem` alternatives dispatched; `RemoveItem` alternatives dispatched; `writeSeen` bool populates `StatementKind`; `buildPart` invariant relaxation; goldens regenerated for scenarios newly parse-green |
| unlock (dirs + skiplist) | `readCoreDirs` gains `clauses/{create,delete,set,remove}`; skiplist entries per bucket-3 negative with ADR 0007 rationale; goldens audited; acceptance-suite step `the result should be empty` transitions from PENDING-on-write to golden-check |

Each commit is green in isolation of the ones after it — the parser
red commit adds the model surface and pins that fail; the parser
green commit adds the handlers; the unlock commit wires the dirs.

---

## 8. Weakest points recorded honestly (per ADR 0004)

**The most fragile part of Stage 12 is the multi-level
propertyExpression on the LHS of SET / REMOVE.** The grammar admits
`n.a.b.c` as a propertyExpression; the model's Ref carries a single
Property name, so a multi-level LHS silently truncates to the first
lookup (n.a). The pinned-tag TCK exercises zero multi-level SET LHS
(a full grep on `SET .*\.\w+\.\w+` returned nothing), so the
truncation is unobservable today. A future TCK bump that adds a
multi-level case would need a real fix: widening Ref to hold a []Property
path (cascading through PropertyUse, RefProjection, and every consumer
of Ref) or a Stage-12-time rejection (a new sentinel, breaking the
"Stage 12 adds no new sentinel" clean line). Either is a bigger
model surgery than the value-level rule warrants at Stage 12; the
choice belongs to whichever stage the corpus first presses the
shape.

**The next-most fragile part is CREATE's anonymous-binding
capture.** CREATE reuses `collectPattern`, which appends every raw
binding it encounters (named or anonymous). The delta
`before..len(bindings)` captures both the named creations and the
anonymous edge bindings a chain pattern composes. An anonymous
node inside a CREATE pattern (`CREATE ()`) does NOT enter
`curPart.bindings` (C3, anonymous nodes are pure filters on the
read side); at write time the anonymous node IS being created, but
the model reflects it only implicitly (the CreateEffect's variable
list carries "" for the anonymous EDGE, which is the closest
observable signal). A downstream consumer that iterates a
CreateEffect's variables to count "how many nodes were created"
would undercount anonymous nodes. Stage 12's answer is that
counting is not a Stage-12 concern — the CreateEffect's role is
"which named bindings does the caller need to track post-write" —
and a future stage that presses per-effect counting would need a
richer per-element index.

The lesser risks, recorded for completeness:

- **`StatementKind` is a query-wide fact, but the walk determines
  it via a listener flag.** A write clause inside an EXISTS
  suppresses (Stage 11 §1.6), so a query like
  `MATCH (n) WHERE exists { CREATE (m) RETURN true } RETURN n`
  does NOT flip `writeSeen` — the outer EnterOC_Create early-returns
  under `subqueryDepth > 0` before `markWrite` runs. This is the
  right answer: the OUTER query does not modify the graph
  (openCypher rejects the composition at compile time anyway — the
  Stage 11 skiplist already lists the case), so classifying the
  outer as read is consistent with what a well-formed engine would
  do. A future consumer that wants "any write anywhere including
  in subqueries" would need a second axis; Stage 12 does not
  provide one, and no known consumer needs it.
- **`SetEntityEffect` collapses `variable = expression` and
  `variable += expression` into one variant with a SetOp axis.**
  This mirrors `EdgeBinding.directed`'s posture (one variant, a
  scalar axis on the value that changes semantics). A future
  refinement that wants two distinct variants (matching every
  grammar alternative one-to-one) would split the axis into two
  variants without loss; the axis-in-one-variant form is chosen
  for the same reason `EdgeBinding` is one type — sharing the
  fields (target, refs, valueType) is the majority of the shape.
- **The `mustParse` authored pins for parameter-typed writes lean
  on the resolver upgrading a bare `$name` from a schema-known
  property type.** The parser records `PropertyUse{Ref{p, name}}`
  for the CREATE inline-map case and `ExprUse{TypeUnknown,
  ExprInProjection}` for the bare-`$newAge` SET case. The
  RESOLVER's job is to unify these into a concrete parameter type
  post-freeze; the PARSER's contract is only "no parameter is
  silently dropped, and every use carries enough for the resolver
  to unify from the schema." A downstream consumer that reads the
  parameter's Uses list before the freeze would see TypeUnknown
  on the bare-SET case and rightly conclude "the parser cannot
  tell" — which is the honest posture (ADR 0005).
- **The `the result should be empty` acceptance-suite step
  transitions in-place.** Its current code checks for
  `ErrUnsupported*` PENDING and only otherwise routes through
  `noSideEffects`; Stage 12 makes it route through `checkGolden`
  for any parsed write. A scenario relying on the old PENDING
  behaviour (a write scenario that has NOT been snapshotted) would
  fail loudly on the first Stage 12 run rather than silently
  passing — which is what `TestGoldenOrphans` and the
  `-update` pass catch. The transition is safe; the risk is that
  a Stage-13 (MERGE) or Stage-14 (CALL) scenario with `the result
  should be empty` and no golden would fail Stage 12's harness. A
  narrow fix would gate the `checkGolden` path on the parse
  succeeding AND `q.StatementKind == StatementWrite` — otherwise
  fall back to the current PENDING behaviour. §7's parser-green
  commit implements this gate.
- **`Effects` is always emitted, even when nil.** A pre-Stage-12
  golden's wire shape gains `"effects": null` after a `-update`
  pass. This is a strictly additive change — no consumer can be
  reading `effects` today — but the total goldens regenerated is
  every read-core scenario snapshot (hundreds of files). The
  `-update` pass is mechanical; the reviewer verifies via
  `TestGoldenOrphans` that every regenerated file corresponds to a
  live scenario.
