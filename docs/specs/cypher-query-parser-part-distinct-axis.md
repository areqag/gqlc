# Part-level DISTINCT axis — Cypher query parser

The implementation brief for the pre-freeze `query.Part` model fix that lifts
projection-list `DISTINCT` into the curated model. One of the last two
pre-freeze model changes before the `query.Query` API freeze, under the
curation discipline of ADR 0003 and the type-interface boundary of ADR 0005.
This is not a numbered stage: it fixes a known gap in Stage 4's part
structure that Stage 10 (aggregate DISTINCT) rediscovered but did not close.

`MATCH (n) RETURN DISTINCT n` currently lowers to the same `query.Query` as
`MATCH (n) RETURN n`. The `DISTINCT` keyword on a `RETURN` or `WITH`
projection body is silently dropped: `collectProjection` in `expr.go` walks
`oC_ProjectionBody` and never inspects the projection-body-level
`DISTINCT` alternative (the terminal at `Cypher.g4 §oC_ProjectionBody`
line 167). The Stage-10 rationale for modelling aggregate-DISTINCT applies
verbatim here, one turn up the tree: `RETURN DISTINCT n` deduplicates the
result rows before returning, so the two queries produce different result
cardinalities on the same dataset — the same "two observably-different
queries must not lower to indistinguishable models" hazard §1.1 of the
Stage-10 spec warns against, at the part level rather than inside a
single aggregate.

This document is a **delta** against Stages 0–15 (referenced individually
where relevant); everything not stated here carries over verbatim.
Sections appear here only where this change adds something.

Tracking: bead `gqlc-33k.2` (GitHub #75). Blocks the `query.Query` freeze
(gqlc-cta) alongside `gqlc-33k.1`.

---

## 1. Deliverables

### 1.1 `Part` grows a `Distinct` axis

The current `Part` has four exported facets: `Bindings`, `Returns`,
`ReturnsAll`, and `Effects`. This change adds a fifth: `Distinct bool` —
true iff the part's own projection body carried the `DISTINCT` keyword
(`RETURN DISTINCT …` or `WITH DISTINCT …`).

Rationale — why DISTINCT enters the model:

- `RETURN DISTINCT n` and `RETURN n` are semantically different queries:
  the former deduplicates the result rows on the projection columns
  before returning, the latter does not, and the two produce different
  row counts on the same dataset. Neither the existing `Returns` slice
  nor the `ReturnsAll` flag captures this: two queries with identical
  `Returns` and identical `ReturnsAll` can still differ in whether the
  result is deduplicated. Dropping DISTINCT lets two observably-
  different queries lower to indistinguishable models, which breaks the
  model's promise that the generated code re-executes the original text
  against a faithful type interface (ADR 0005): the generated method's
  *signature* is unchanged, but its *result contract* is not.

- ADR 0003's no-expression-tree rule is not violated: DISTINCT is a
  single-bit annotation on one node of the model, not a tree fragment.
  It is the exact analogue of `EdgeBinding.Directed()` (Stage 5),
  `AggregateProjection.Distinct()` (Stage 10), and `Query.Combinators[i]`
  (Stage 4) — a scalar value-affecting axis on a specific variant that
  changes result cardinality without dragging the expression tree into
  the model. The precedent is dense: at the branch level (`UnionKind`
  distinct vs. all), at the aggregate level (aggregate DISTINCT), at
  the edge-binding level (directed vs. undirected). The part level is
  the one remaining hole.

- The three DISTINCT axes are **independent** and each on a distinct
  model surface. See §1.3 for the disambiguation table.

### 1.2 Field vs. constructor threading — decision

`Part` today is a product type with **exported fields** (`Bindings`,
`Returns`, `ReturnsAll`, `Effects`), not an opaque smart-constructor
variant with unexported fields. The Stage-12 smart constructor
`NewPart(bindings, returns, returnsAll, effects) (Part, error)` guards
one invariant (`ErrEmptyPart` — at least one field non-empty) that
field-level types cannot express alone; it does not hide the fields.
Direct struct-literal construction is a supported production pattern
for `mustParse` hand-built expectations (`query.Part{Bindings: …,
Returns: …}` throughout `parser_test.go`), which the constructor
comment at `query.go` ~:77 documents as a trusted-caller path.

**Decision: `Distinct` is an exported field, threaded through `NewPart`
as an added parameter.** Both, not either.

- Exported field, because `Part`'s existing shape is a public product
  type — hiding one axis behind a `Distinct()` accessor while
  `Returns` / `ReturnsAll` / `Bindings` / `Effects` stay exported would
  break the single-shape convention and force every `mustParse` pin to
  reach for a constructor call instead of the field-literal shape it
  uses today. The `EdgeBinding.directed` precedent does not apply:
  `EdgeBinding` is a smart-constructor variant with entirely
  unexported fields (line 357 of `query.go` — every field is lowercase),
  so an unexported `directed` field is the local convention. `Part`'s
  local convention is the opposite.

- Constructor-threaded, because the smart constructor is the single
  parser-side entry point (`buildPart` in `build.go` ~:236 calls
  `query.NewPart(…)`) and it must observe the new axis so a future
  invariant involving `Distinct` (there are none today, but the
  discipline stands) has one call site to enforce at. Ignoring
  `Distinct` in `NewPart` would create a silent path where the
  constructor accepts a `Distinct=true` field on the returned Part
  without inspection, which is the wrong signal.

Constructor signature update:

```go
func NewPart(bindings []Binding, returns []ReturnItem, returnsAll bool, distinct bool, effects []Effect) (Part, error)
```

The parameter order groups the projection-related facets together —
`returns`, `returnsAll`, `distinct` — and keeps `effects` last, matching
the "reads before writes" reading order the existing signature
established. `distinct` sits between `returnsAll` and `effects` because
it modifies the projection (like `returnsAll`), not the effects list.

**Blast radius.** One production caller (`build.go:236`) and five test
call sites (`query_test.go:204/208/214/218/232`) — no wire-shape
consumers outside the parser package (ADR 0004: nothing downstream of
the parser is built pre-freeze). The signature change is a one-hop
mechanical update.

### 1.3 Semantics — which part owns the DISTINCT

**The DISTINCT axis belongs to the part whose own projection body
carries the `DISTINCT` keyword — each part records its own.** A
`WITH DISTINCT` inside a branch flags the part it terminates (an
intermediate part); a `RETURN DISTINCT` flags the final part (the one
`RETURN` terminates). A branch is a sequence of parts, and each part's
Distinct axis is independent of its siblings' — a query is free to have
`MATCH (a) WITH DISTINCT a MATCH (a)-->(b) RETURN b` (first part
distinct, second not) or `MATCH (a) WITH a MATCH (a)-->(b) RETURN
DISTINCT b` (first part not distinct, second distinct) or both
distinct or neither, and every combination is a legal, observably
different query.

**Grammatical scope: a projection body's DISTINCT modifies exactly
that projection body's rows.** Per `Cypher.g4 §oC_ProjectionBody`
(line 167): `( SP? DISTINCT )? SP oC_ProjectionItems …`. The
`DISTINCT` token is part of the same `oC_ProjectionBody` rule that
`WITH` (`§oC_With` line 157) and `RETURN` (`§oC_Return` line 162)
share. So there is exactly one DISTINCT-carrying rule, and it appears
exactly once per part (a part has exactly one projection body — the
`WITH` that terminates it or the `RETURN` that ends the branch).

The invariant is symmetric with `ReturnsAll`: a part's projection body
is either the `*` alternative (`ReturnsAll=true`) or an explicit
items list (`ReturnsAll=false`), and either can carry the DISTINCT
keyword. `RETURN DISTINCT *` and `WITH DISTINCT *` are legal openCypher
shapes the grammar admits, so the two axes compose freely: the four
combinations `{ReturnsAll ∈ {true,false}, Distinct ∈ {true,false}}`
are all reachable, and none subsumes the other.

### 1.4 Disambiguation from the two existing DISTINCT axes

Three DISTINCT axes now live in the model, each on a distinct surface,
each capturing a different cardinality-affecting decision. They are
independent — no axis subsumes any other, and every combination is
reachable via a source-level query.

| Axis                                | Model surface                | Grammar origin                                        | What it deduplicates                                            | Introduced |
|-------------------------------------|------------------------------|-------------------------------------------------------|-----------------------------------------------------------------|------------|
| Aggregate DISTINCT                  | `AggregateProjection.Distinct` | `oC_FunctionInvocation` `DISTINCT` (Cypher.g4 §426)   | The aggregate function's **input rows** (before aggregation)    | Stage 10   |
| UNION vs. UNION ALL                 | `Query.Combinators[i]` (`UnionKind`) | `oC_Union` `ALL` presence (Cypher.g4 §~UNION rule)   | The **union of two branches' output rows** (across the combinator) | Stage 4  |
| Part projection DISTINCT (**this**) | `Part.Distinct`              | `oC_ProjectionBody` `DISTINCT` (Cypher.g4 §167)       | The **part's own projection rows** (before WITH-forwarding or RETURN) | This change |

Three-way witness — a single query pressing all three axes:

```
MATCH (a)
RETURN DISTINCT count(DISTINCT a) AS c
UNION ALL
MATCH (b)
RETURN count(b) AS c
```

- First branch's final part: `Distinct=true` (RETURN DISTINCT); its
  return item's `AggregateProjection.Distinct=true` (count(DISTINCT a)).
- Combinator between the two branches: `UnionAll` (not UnionDistinct).
- Second branch's final part: `Distinct=false`, its `AggregateProjection.
  Distinct=false`.

Every axis carries a value the others cannot: the aggregate DISTINCT
does not deduplicate the projection's result rows (the outer part might
still have duplicate `c` values across matched rows if the aggregate's
non-input columns vary — modulo grouping, a resolver concern); the
part DISTINCT does not deduplicate the aggregate's input; the union
kind does not deduplicate within a single branch's rows.

### 1.5 Aggregate-DISTINCT independence — the negative pin

An aggregate-DISTINCT in the projection does **not** flip the part's
`Distinct`. `RETURN count(DISTINCT n)` has `AggregateProjection.
Distinct=true` and `Part.Distinct=false`. `RETURN DISTINCT count(n)`
has `AggregateProjection.Distinct=false` and `Part.Distinct=true`.
`RETURN DISTINCT count(DISTINCT n)` has both true.

This is a **negative test** the RED phase must exercise (§4.4). The
listener has one place where `DISTINCT` appears — inside
`classifyFunction` in `expr.go` ~:348 (reading `fi.DISTINCT()` from
`oC_FunctionInvocation`) — and this change adds a second place, at
`collectProjection` for `oC_ProjectionBody.DISTINCT()`. The two sites
must not cross-contaminate: the aggregate's DISTINCT lives on the
function invocation, the part's DISTINCT lives on the projection body,
and each reads its own grammar accessor.

### 1.6 Wire format — always-emit

`Part` gains `"distinct": bool` as an always-emitted JSON field,
alongside `bindings`, `returns`, `returnsAll`, `effects`. Always-emit
matches the convention (`nullable`, `returnsAll`, `directed`, `hops`,
aggregate `distinct`). The new field is a strict additive extension —
existing consumers see every existing key unchanged; a Distinct=false
part serialises with `"distinct": false` alongside the four existing
keys.

The `Part` struct today uses default `encoding/json` marshaling driven
by struct tags (there is no `MarshalJSON` method on `Part` — grep
confirms). Adding `Distinct bool \`json:"distinct"\`` at the field
position between `ReturnsAll` and `Effects` puts the wire order
"bindings, returns, returnsAll, distinct, effects", matching the field
order in the Go struct. Existing goldens gain one line per part.

### 1.7 Grammar source of truth

The DISTINCT token in the projection body is at `Cypher.g4 §oC_ProjectionBody`
line 167:

```
oC_ProjectionBody
    :  ( SP? DISTINCT )? SP oC_ProjectionItems ( SP oC_Order )? ( SP oC_Skip )? ( SP oC_Limit )? ;
```

The DISTINCT is optional, appears exactly once per body, and precedes
the projection items. ANTLR generates a `DISTINCT()` accessor on
`OC_ProjectionBodyContext` that returns a non-nil terminal node when
the keyword was present. This is the sole listener-side signal: the
lowering site is `collectProjection` in `internal/query/cypher/expr.go`
(line 26), which today reads `OC_ProjectionItems()` and
`OC_Order/Skip/Limit()` from the same body but ignores `DISTINCT()`.

The change is one line at the top of `collectProjection`: read the
DISTINCT accessor and stash it on the current rawPart, exactly
parallel to how `returnsAll` is stashed at line 34. Then `buildPart`
in `build.go` passes it through `NewPart`.

**Grammatical guarantee.** The DISTINCT-in-projection-body rule is the
only projection-level DISTINCT the grammar defines; the aggregate
DISTINCT lives on `oC_FunctionInvocation` (line 426), a strictly
lower grammar node. A DISTINCT keyword outside these two contexts is
a syntax error — the grammar (line 601) lists DISTINCT as a keyword
reserved for these two productions and nothing else.

### 1.8 Golden-impact statement

The ~3199 golden JSON snapshots under
`internal/query/cypher/testdata/golden/` gain **one field per part**
(`"distinct": <bool>`). No key is renamed, no value shape changes,
no key is removed. The diff must be **field-addition-only**:

- `git diff --stat` should show ~3199 files touched with a very tight
  insertions-per-file distribution centred around `2 * <parts-per-query>
  + 0` deletions (two lines per part: one for the added key, one for
  the trailing comma on the previous key).
- A grep over the modified goldens for any change to `"bindings"`,
  `"returns"`, `"returnsAll"`, `"effects"`, `"kind"`, `"func"`, `"refs"`,
  aggregate `"distinct"`, or any binding/projection field must yield
  zero non-cosmetic hits.

The verification approach (§4.5): after rebaselining, run a scripted
grep that fails the diff if any key other than the new part-level
`"distinct"` differs from master. Purity of the diff is a hard
precondition for GREEN.

Of the ~3199 goldens, exactly `Return14_*` / `ReturnOrderBy*` /
`With6_*` and similar named parts under
`clauses/return`/`clauses/with` will change *value* — those are the
scenarios that press the DISTINCT keyword. Every other golden gains
the field with value `false`.

### 1.9 Test coverage

Five test surfaces across the two packages — three model unit tests
and two parser `mustParse` pins — plus one amended existing pin that
carries the aggregate-vs-part independence invariant:

**Model unit tests** (`internal/query/query_test.go`):

1. `TestNewPartCarriesDistinct` — `NewPart(nil, returns, false, true, nil)`
   returns a `Part` with `Distinct=true`; the field is readable.
2. `TestPartDistinctAlwaysEmitted` — `json.Marshal` on a
   `Distinct=false` part contains `"distinct":false`; on a `Distinct=true`
   part contains `"distinct":true`. Both keys are always emitted.
3. `TestNewPartRejectsEmpty` (existing) updated to the new signature —
   `NewPart(nil, nil, false, false, nil)` still returns `ErrEmptyPart`;
   `NewPart(nil, nil, false, true, nil)` **also** returns `ErrEmptyPart`.
   Rationale: `Distinct=true` alone is not a projection — it modifies a
   projection that must exist. A DISTINCT with no projection items and
   no wildcard is grammatically impossible (the `oC_ProjectionBody`
   rule requires items or a wildcard between DISTINCT and the terminator),
   so the constructor closes the door on it too.

**Parser tests** (`internal/query/cypher/parser_test.go`):

4. `return distinct` — `MATCH (n) RETURN DISTINCT n` lowers to one
   part with `Distinct=true` and one `RefProjection{Ref{"n"}, TypeNode{}}`.
5. `with distinct` — `MATCH (a) WITH DISTINCT a MATCH (a)-->(b) RETURN b`
   lowers to two parts, the first with `Distinct=true` and one
   `RefProjection{Ref{"a"}, TypeNode{}}`, the second with
   `Distinct=false`.
6. Negative-pin coverage lives on the **existing** `count distinct`
   mustParse pin (parser_test.go:1188), not a new test. That pin
   exercises `RETURN count(DISTINCT a)` and asserts
   `AggregateProjection.Distinct=true`; because it uses a bare
   `query.Part{Bindings: …, Returns: …}` struct literal (no `Distinct`
   field set), it also asserts `Part.Distinct=false` by the go-cmp
   deep-equal at the zero value — the same shape it did before this
   change. Cross-contamination between the two grammar sites
   (`oC_ProjectionBody.DISTINCT()` vs.
   `oC_FunctionInvocation.DISTINCT()`) would flip `Part.Distinct` to
   `true` and break this equality. The pin's comment is amended to
   name the invariant so a future reader sees why the pin now covers
   two axes (aggregate DISTINCT true, part DISTINCT false). No
   dedicated new test is added: adding one would duplicate the
   coverage already earned by the existing pin's shape.

### 1.10 Sentinel status

No new sentinel. `TestSentinelReachability` runs against the four-
sentinel set (`ErrUnsupportedClause`, `ErrUnsupportedParameter`,
`ErrUnboundVariable`, `ErrVariableKindConflict`) — none is touched
(Stage 14 retired `ErrUnsupportedClause`; this change touches none
of the four remaining). The Distinct axis is accept-and-record: every
grammar-legal DISTINCT surfaces on the model. No parse-time rejection
is added.

### 1.11 Corpus wiring

No new dir enters `readCoreDirs`. The 3897-scenario / 3459-passed /
438-pending / 0-failed baseline must hold verbatim after the change:
this fix reshapes an already-lowered part, it does not gate scenarios
that previously deferred. Any drift in the summary line is a
regression to reconcile before commit.

### 1.12 Docs inline

- This spec.
- ADR 0003's amendment notes gain a line: "the `Part` product type
  gains a `Distinct` axis capturing the projection-body-level DISTINCT
  keyword; independent of the two existing DISTINCT axes (aggregate
  DISTINCT at Stage 10, UNION-vs-UNION-ALL at Stage 4)."
- CONTEXT.md's **Query part** entry gains a mention of the Distinct
  axis alongside `ReturnsAll` — the two are grammar siblings and
  compose freely.

Nothing downstream of the parser is built (no resolver, no codegen)
— ADR 0004. The resolver's DISTINCT interpretation is a resolver
transformation the model now honestly hands off, exactly like the
Stage-10 aggregate-DISTINCT and Stage-4 UnionKind cases.

---

## 2. Why one atomic cycle

Adding a `Distinct` field to `Part`, threading it through `NewPart`,
reading it from `oC_ProjectionBody` in the listener, and rebaselining
the ~3199 goldens is one restructure of the part's projection story.
Splitting the model change from the parser change would leave the model
underused; splitting the golden rebaseline from either would leave the
acceptance suite in a mid-migration state where every part's JSON
disagrees with the wire the model emits. This change lands as one
branch.

Within the branch, the commit inventory (§6) separates spec from model
from parser from goldens so review can proceed incrementally, matching
the Stage-10 / Stage-13 template.

---

## 3. Model shape

### 3.1 `Part` with Distinct axis

```go
type Part struct {
    Bindings   []Binding    `json:"bindings"`
    Returns    []ReturnItem `json:"returns"`
    ReturnsAll bool         `json:"returnsAll"`
    Distinct   bool         `json:"distinct"`   // new
    Effects    []Effect     `json:"effects"`
}

func NewPart(bindings []Binding, returns []ReturnItem, returnsAll bool, distinct bool, effects []Effect) (Part, error)
```

Wire encoding for a plain read part:

```
{
  "bindings": [...],
  "returns":  [...],
  "returnsAll": false,
  "distinct":   false,
  "effects":   null
}
```

The `distinct` key is always emitted (matches the always-emit
convention).

### 3.2 `rawPart` widening in the listener

`rawPart` gains one field:

```go
type rawPart struct {
    // ... existing fields ...
    distinct bool // set by collectProjection when oC_ProjectionBody carries DISTINCT
}
```

`newRawPart()` at listener.go ~:140 leaves it zero (false), matching
the default. `buildPart` reads `rp.distinct` and passes it to
`NewPart`.

### 3.3 `NewPart` invariant unchanged in spirit

`ErrEmptyPart` still guards the all-zero shape. A DISTINCT with no
projection is not representable via the constructor (`NewPart(nil, nil,
false, true, nil)` returns `ErrEmptyPart`) — because the field-level
check for "at least one of bindings, returns, returnsAll, effects" is
unchanged, and `Distinct=true` alone does not satisfy any of those
four (DISTINCT is a modifier on a projection, not a projection). This
mirrors the grammar: DISTINCT cannot appear without an items list or
a wildcard in the same body.

Grammatical guarantee: no parse path can hand `buildPart` a rawPart
with `distinct=true` and every other field empty — `collectProjection`
only sets `distinct` after it has confirmed a non-empty projection
body (items or wildcard). The constructor's guard is belt-and-braces.

### 3.4 `exportedTypes` unchanged

`exportedTypes` (listener.go ~:298) computes what names the closed
part exports into the next part's scope, based on `returnsAll` and the
return items' types. DISTINCT does not affect the exported set: a
`WITH DISTINCT a` still exports the name `a` with its Stage-6 type,
and a `WITH DISTINCT *` still forwards every in-scope name. DISTINCT
deduplicates rows, not the schema of what carries forward. No code
path change.

---

## 4. Parser widening

### 4.1 DISTINCT read from `oC_ProjectionBody` (§1.7)

`collectProjection` in `expr.go` reads `body.DISTINCT()` — an ANTLR
terminal node accessor generated on `OC_ProjectionBodyContext`. A
non-nil accessor means the DISTINCT keyword was present. The
classifier stashes the boolean on the current rawPart, at the same
level `returnsAll` is stashed today.

Code shape (one addition, immediately after the nil guard, before the
wildcard classifier):

```go
if body.DISTINCT() != nil {
    l.curPart.distinct = true
}
```

The setter is idempotent (a projection body carries at most one
DISTINCT — the grammar admits only one; a repeated set is a no-op).
The write happens before the `*` alternative's `returnsAll = true`
setter so a `RETURN DISTINCT *` records both flags on the same part.

### 4.2 `buildPart` update

`build.go` ~:236 changes from:

```go
part, err := query.NewPart(partBindings, rp.returns, rp.returnsAll, partEffects)
```

to:

```go
part, err := query.NewPart(partBindings, rp.returns, rp.returnsAll, rp.distinct, partEffects)
```

Every other line of `buildPart` is unchanged.

### 4.3 No aggregate-DISTINCT change

`classifyFunction` in `expr.go` ~:348 continues to read `fi.DISTINCT()`
from `oC_FunctionInvocation` and pass it to `NewAggregateProjection`.
This site does not touch `l.curPart.distinct` — the two DISTINCT
grammar sites are read independently, matching §1.5.

### 4.4 Negative pin — aggregate DISTINCT does not set part DISTINCT

The parser test `count distinct` (parser_test.go:1188) continues to
exercise `RETURN count(DISTINCT a)` and expects
`AggregateProjection.Distinct=true`. Because the pin builds its
expectation with a bare `query.Part{Bindings: …, Returns: …}` struct
literal — no `Distinct:` field set — the deep-equal check also
asserts `Part.Distinct=false` at the zero value. If the two grammar
sites were ever cross-wired (both set `l.curPart.distinct`, or both
consulted the same rawPart flag), the parser would flip
`Part.Distinct` to `true` and the equality would break — a structural
guarantee at the test level, no dedicated new test required. See
§1.9 #6 for the amended pin's comment and rationale.

### 4.5 Golden purity check

After `go test -update ./internal/query/cypher/`, run:

```bash
git diff --stat internal/query/cypher/testdata/golden/ | tail
```

to confirm the file count (~3199) and the insertion/deletion
distribution. Then run:

```bash
# Sanity: every diff hunk mentions "distinct" (either the added key
# or a pre-existing aggregate-distinct value unchanged).
git diff internal/query/cypher/testdata/golden/ | grep -E "^[-+]" | \
    grep -v -E '"distinct"|^\+\+\+|^---|^[+-]\s*$|^[+-]\s*}\s*,?$'
```

The second grep should print **nothing** — every changed line must
either be the added `"distinct":` line or a trivial trailing-comma
adjustment on the preceding line (the preceding value was the last
before `effects`; now `effects` is preceded by the new `distinct`
line). Any other output is a purity violation.

---

## 5. Definition of done

1. Spec commit lands (`docs(spec): part DISTINCT axis`).
2. RED commit lands (six failing tests §1.9; the exact failure text
   recorded in the RED verification output).
3. GREEN commit lands (model + parser + goldens; every RED test
   passes).
4. `just test` green.
5. `just lint` green.
6. `just fmt-check` green.
7. Godog summary line reads verbatim
   `3897 scenarios (3459 passed, 438 pending)` /
   `16006 steps (15568 passed, 438 pending)`. Any drift is a
   regression to reconcile before commit.
8. Golden diff is field-addition-only per §1.8 / §4.5.
9. GitHub issue #75 closed via `Closes #75` in the branch's cover
   commit / PR body.

---

## 6. Commit inventory (single branch `part-distinct-axis`)

| Commit | Scope |
|--------|-------|
| spec   | this spec |
| RED    | Failing model + parser tests for the Distinct axis |
| GREEN  | `Part.Distinct` field + `NewPart` signature update + `collectProjection` reads DISTINCT + `buildPart` threads it + `NewPart` invariant test updated + goldens rebaselined |
| gates  | `just test` / `just lint` / `just fmt-check` verification pass (recorded in report) |

Each commit is green in isolation of the ones after it: the spec
commit is docs-only; the RED commit's failing tests do not touch
production code so `just build` still passes; the GREEN commit
routes through the new signature and rebaselines goldens in one
atomic step.

---

## 7. Weakest point (recorded honestly per ADR 0004)

**The part-level Distinct axis captures the DISTINCT keyword's
presence but not its cross-part interaction with grouping.** A
`WITH DISTINCT n` deduplicates on the projected columns before the
part boundary, which is a *grouping-key* fact the resolver has to
compose with the aggregate-DISTINCT and the implicit-grouping rule
(bead `gqlc-gyw`). The model records "this part deduplicates its
output"; it does not carry which columns form the dedup key (the
answer is "all of the projected columns" — trivially derivable from
`Returns`, so no extra field is needed at parse time). If a future
openCypher feature (or a dialect extension) introduces
per-column DISTINCT keys (`RETURN DISTINCT ON (a) a, b` PostgreSQL-
style), the parser would need a wider axis; the current single bit
lines up with openCypher's grammatical reality.

The lesser risks, recorded for completeness:

- **DISTINCT on a projection-less write part is grammatically
  unreachable.** A projection-less part (Stage 12 §3.2 relaxation) has
  no `oC_ProjectionBody`, so no DISTINCT — `l.curPart.distinct` stays
  false by default. `NewPart(bindings, nil, false, true, effects)` is
  representable at the constructor boundary but never at parse. The
  smart constructor accepts it (the invariant is "at least one of the
  five non-Distinct axes"), matching how `EdgeBinding.Directed=false`
  is accepted on any endpoints combination — the model does not
  encode "DISTINCT requires a projection" as a runtime invariant, and
  the grammar is the source of truth for that constraint. Trusted-
  caller struct-literal construction can still hand-write the shape
  in a test fixture; that is the pattern the whole model follows.

- **`RETURN DISTINCT *` and `WITH DISTINCT *` compose freely.** The
  two axes are independent as documented (§1.3), and the mustParse
  set would grow one more pin if a scenario in the corpus presses
  the wildcard-with-DISTINCT shape. A scratch check against the
  TCK corpus (`grep -rn 'DISTINCT \*' test/data/query/cypher/tck/features/`)
  shows the shape does appear (`RETURN DISTINCT *` in
  `clauses/return/Return*.feature`, `WITH DISTINCT *` in
  `clauses/with/With*.feature` — the goldens for those files carry the
  proof). If any of those scenarios were passing pre-change and go
  pending post-change, the diff is not field-addition-only — a hard
  precondition of §4.5 that the summary-line invariant of §5 §7
  double-checks.
