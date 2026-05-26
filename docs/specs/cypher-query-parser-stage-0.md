# Stage 0 spec — Cypher query parser: the read core

The implementation brief for Stage 0 of the Cypher implementation of
`query.Parser`. Build this **test-first**. Scope, sequencing, and the
model-evolution philosophy are set by ADR 0003 (curated model + separate
resolution), ADR 0004 (test-first, evolving-until-feature-complete), and ADR 0005
(execution uses original text). This document is the precise accept/reject
contract and test harness for Stage 0 only.

Stage 0 lowers a **single read query** into the *current* `query.Query` model
unchanged. Later stages evolve the model (ADR 0004); do not anticipate them here.

---

## 1. Deliverables

- `internal/query/cypher/` — the parser (`New() query.Parser`), mirroring
  `internal/schema/gql`.
- A `just fetch-tck` recipe that vendors the entire openCypher TCK (committed).
- A `just test` recipe that runs the whole suite, including the gherkin run.
- A godog acceptance suite + targeted unit/sentinel tests.

Nothing downstream of the parser is built (no resolver, no codegen) — ADR 0004.

---

## 2. Architecture (Cluster A)

- **Package layout** — `internal/query/cypher/`: `parser.go` (`New() query.Parser`,
  ANTLR lexer/parser wiring, single listener error-sink, walk → build),
  `listener.go` (collection + error sink), `errors.go` (the sentinels). Mirror
  `internal/schema/gql`.
- **Listener, not visitor** (ADR 0001): one full walk, single error channel.
- **One collection pass + `build()`** — the listener collects into ordered slices
  plus variable/param lookup maps; a `build()` method assembles `query.Query` at
  end of walk. There is **no** schema-style `resolve()` second pass: edge
  endpoints record a variable *name* (a string), and labels live on the binding,
  so there is no parse-time endpoint→type lookup. **Nothing in the parser is named
  `resolve`/`resolution`** — that term is reserved for the schema-checking stage
  (CONTEXT.md). `build()` does a self-consistency *validation*, not a resolution.
- **Fail-fast, first error** — single `err` sink, idempotent `fail()`; syntax
  errors arrive via the ANTLR error listener onto the same channel; `walk` returns
  the first recorded error. ANTLR cannot halt mid-walk, so the walk completes but
  the first error wins.
- **Zero value on error** — return `query.Query{}` on any error.

---

## 3. The accept/reject contract (Cluster B)

### B0 — execution uses original text (ADR 0005)

The executed query is the original Cypher with `$param` placeholders intact; the
model is the type interface only. This is why interface-neutral constructs can be
accepted-and-ignored safely.

### B1 — the governing principle

| If a construct… | …then |
|---|---|
| is invalid/malformed openCypher | **reject** (syntax error, or a category-3 sentinel) |
| is valid and does **not** affect the type interface (bindings/params/columns) | **accept & ignore** — execution keeps it via the original text |
| **affects the type interface** but we cannot faithfully represent it yet | **reject** with an `ErrUnsupported…` sentinel — ignoring it would emit wrong types |

### Accepted in Stage 0

| Construct | Treatment |
|---|---|
| `MATCH+ [WHERE]? RETURN`, exactly one `RETURN`, single statement to EOF | the read core |
| `MATCH` (non-`OPTIONAL`), comma-separated pattern parts | bindings |
| Node `(v:A:B {map})` — conjunctive labels | Node binding |
| Rel `(a)-[r:TYPE {map}]->(b)`, both arrow directions (canonicalized); untyped `[r]` allowed | Edge binding + endpoints |
| RETURN item `var` or `var.prop`, optional `AS alias` | return item |
| `DISTINCT`, `ORDER BY …`, `SKIP <int literal>`, `LIMIT <int literal>` | accept & ignore |
| `WHERE <predicate>` | mine parameters; ignore predicate structure |
| inline property map `{prop: $p}` on a node/rel pattern | mine param `Ref{var, prop}` |

### Rejected in Stage 0

| Construct | Sentinel | Returns in stage |
|---|---|---|
| syntax error / more than one statement | *(nil — syntax)* | — |
| `OPTIONAL MATCH` / `WITH` / `UNION` / `CREATE`·`MERGE`·`SET`·`DELETE`·`REMOVE` / `UNWIND` / `CALL` | `ErrUnsupportedClause` | 2 / 4 / 4 / never / never |
| `RETURN *`; aggregations, function calls, arithmetic, literals, `CASE`, comprehensions as return items | `ErrUnsupportedProjection` | 3 |
| `SKIP $p` / `LIMIT $p` / `ORDER BY …$p…`; a parameter not bindable to a property (arithmetic, bare `$x`, `IN $x`) | `ErrUnsupportedParameter` | 1 / 3 |
| multi-type rel `[:A\|B]`, variable-length `[*..]`, named path `p = (…)`, **undirected** `(a)-[r]-(b)` | `ErrUnsupportedPattern` | later / never / 5 |
| unbound variable referenced by a return item, a parameter use, or an edge endpoint (not a variable that appears only inside an ignored `WHERE` predicate) | `ErrUnboundVariable` | — |
| variable used as both node and edge | `ErrVariableKindConflict` | — |

### B3 — sentinels (six, category-grained)

```
ErrUnsupportedClause
ErrUnsupportedProjection
ErrUnsupportedPattern
ErrUnsupportedParameter
ErrUnboundVariable
ErrVariableKindConflict
```

Each carries **specific message text** naming the offending construct. Category
(not per-construct) sentinels keep churn low: when a later stage starts supporting
a construct, we delete one `Enter*` handler — no sentinel renames. A
sentinel-reachability sweep (as in `internal/schema/gql`) guards the set.

---

## 4. Binding construction (Cluster C)

- **C1 — dedup & order.** One `Binding` per named variable, keyed by `Variable`,
  in first-appearance order. Each anonymous edge (no variable) is its own
  `Binding` (`Variable==""`).
- **C2 — label merge.** Repeated occurrences of a variable union their labels
  (`(a:Person) … (a:Employee)` → `[Employee, Person]`) — openCypher treats them as
  additional conjunctive constraints. Merge as an **ordered union** (first
  appearance), never via a map, for deterministic golden output.
- **C3 — anonymous nodes are not bindings.** Only anonymous *edges* become
  bindings. An anonymous node inside a relationship contributes its labels inline
  on the edge endpoint; a standalone anonymous node (`MATCH (:Person)`) is a pure
  filter → ignored in the model (still executed via original text).
- **C4 — endpoint representation.** Node endpoint with a variable →
  `Endpoint{Variable: x}` (labels live on x's `Binding`, not duplicated).
  Anonymous-with-labels → `Endpoint{Labels: […]}`. Fully anonymous `()` → empty
  `Endpoint`.
- **C5 — undirected rejected.** `(a)-[r]-(b)` → `ErrUnsupportedPattern` (the
  schema is directed-only; revisited in stage 5).
- **C6 — kind-conflict.** Track each variable's kind while merging; a variable
  used as both node and edge → `ErrVariableKindConflict`.
- **C7 — untyped edge / unlabelled node.** Allowed; `Labels` is empty (resolver
  infers later).
- **Direction.** Right-pointing `(a)-[r]->(b)` → `Source=a, Target=b`.
  Left-pointing `(a)<-[r]-(b)` → canonicalized to `Source=b, Target=a` (mirrors
  the schema side).
- **Minor:** relationship-variable reuse is not special-cased — first occurrence
  defines endpoints; later occurrences merge labels only.

---

## 5. Parameters (Cluster D)

- **D1 — capture rule.** A `$param` yields a `Use = Ref{variable, property}` iff it
  appears as **either**:
  - (a) one operand of a comparison (`= <> < <= > >=`) or string predicate
    (`STARTS WITH` / `ENDS WITH` / `CONTAINS`) whose *other* operand is a
    single-level property lookup `variable.property` on a bound variable; **or**
  - (b) the direct value of a key in an inline property map on a node/rel pattern
    element — `(a:Person {id: $id})`, `[r:KNOWS {since: $s}]`.

  Any other `$param` occurrence → `ErrUnsupportedParameter`.
- **D2 — dedup & order.** Dedup by `Name`, first-appearance order; one `Parameter`
  carrying all `Uses` in appearance order.
- **D3 — no type-checking in the parser.** Record `Uses` only. Unifying types
  across uses is the resolver's job (it needs the schema).
- **D4 — Stage-0 invariant.** Every `Parameter` has ≥1 `Use`, and every `Use` is a
  property `Ref`.
- **Deferred → `ErrUnsupportedParameter`:** `SKIP/LIMIT $n`; params in arithmetic;
  bare-predicate params; param-vs-param/literal; `a.prop IN $p`; params nested in
  lists/maps; multi-level property access (`a.b.c`); a param as a RETURN item.

---

## 6. Return items (Cluster E)

- **Supported expression shapes:** a bare variable `var`, or a single-level
  property lookup `var.prop`. Anything richer → `ErrUnsupportedProjection`.
- **E1 — column name.** The explicit `AS` alias if present; otherwise the verbatim
  source text of the expression — `RETURN p` → `"p"`, `RETURN p.name` →
  `"p.name"`, `RETURN p.name AS firstName` → `"firstName"`. This is the column
  name openCypher itself produces. Go-identifier sanitization is a codegen concern
  (post-freeze); the model stores the openCypher name.
- **E2 — Ref mapping.** `var` → `Ref{var, ""}`; `var.prop` → `Ref{var, prop}`.
- **E3 — whole-entity return.** `RETURN p` → `Ref{p, ""}` allowed; the resolver
  later expands it.
- **E4 — `DISTINCT`** accepted & ignored.
- **E5 — duplicates** kept in order, not rejected (the lowerer is not a validator).
- **E8 — aliases are not bindings** — in a final RETURN, `AS alias` is only a
  column name.

---

## 7. Testing, corpus & justfile (Cluster F)

### Corpus

The openCypher **TCK** (Technology Compatibility Kit) — the conformance suite that
certifies an implementation against the openCypher spec. Vendored **entire and
committed** so coverage can widen each stage with no extra fetching.

- **`just fetch-tck`** vendors the whole `tck/` subtree at a **pinned release tag**
  (a `justfile` variable, never `master`) into `test/data/query/cypher/tck/`,
  including the TCK `LICENSE` (Apache-2.0) for attribution. Plain git
  sparse+shallow checkout; no new deps. The recipe only vendors — godog reads the
  `.feature` files directly, so there is **no extraction step**.
- `fetch-tck` is run for initial population and deliberate version bumps; the
  result is committed.

### Layout: data vs code

```
test/data/query/cypher/tck/        # vendored TCK (data) — `just fetch-tck`
internal/query/cypher/
  parser.go  listener.go  errors.go
  acceptance_test.go               # godog runner + step defs + feature selection + skiplist
  parser_test.go                   # targeted unit + sentinel assertions
```

The godog runner references the TCK with a relative path (e.g.
`../../../test/data/query/cypher/tck/features/…`), matching the schema test's
`../../../test/data/schema/gql` style.

### The TCK is an execution suite — adapt, don't adopt

The TCK's `Then` steps assert **result rows** and **runtime/compile errors** (it
targets a full engine). We are a parser. So:

- positive `Then …` (`result should be`, `no side effects`) → assert the query
  **parsed**, and snapshot the resulting `query.Query` to a golden (structural
  check; `-update` to regenerate, human-reviewed). `query.Query` itself needs no
  custom `MarshalJSON` — its members are order-preserving slices/strings,
  deterministic given first-appearance ordering and ordered label-union (C2). Its
  `Binding`/`Endpoint` sum types, however, each carry a deterministic tagged-union
  `MarshalJSON` (a `"kind"` discriminator): the model was restructured to sum
  types to make illegal states unrepresentable — same capability, type-safe
  encoding — and `encoding/json` cannot render an interface as a tagged union on
  its own.
- negative `Then a SyntaxError/SemanticError should be raised` → assert the parser
  **rejected** (some error).
- setup steps (`Given an empty graph`, `having executed: …`) → no-ops (we hold no
  graph).

### Two-layer suite

- **Layer 1 — godog over the TCK.** Runs verbatim TCK scenarios. **Scope is the
  progress meter:** Stage 0 points godog at the read-core feature dirs
  (`clauses/match`, `clauses/return`, `clauses/match-where`); a **skiplist** excludes
  valid-but-out-of-scope scenarios inside them. Each later stage includes more
  dirs and shrinks the skiplist. Selection and skiplist live in
  `acceptance_test.go` — the corpus data is never edited.
- **Layer 2 — targeted sentinel checks.** The TCK does not encode our 6-sentinel
  taxonomy, so a thin mapping pairs specific **verbatim** corpus queries with their
  expected sentinel, plus the sentinel-reachability sweep. (Selecting and labelling
  corpus queries — never authoring Cypher.)

### `just test`

```just
test:
    go test ./...
```

Runs everything (unit, golden snapshots, godog) in one shot, since the godog suite
is an ordinary `go test`. It does **not** depend on `fetch-tck` (the TCK is
vendored — no network at test time). The full corpus is on disk but only the
selected subset executes (Layer 1 selection).

---

## 8. Definition of done for Stage 0

1. `just fetch-tck` vendors the pinned TCK; the tree is committed.
2. `internal/query/cypher` parses the read core into `query.Query` per §§2–6.
3. The six sentinels exist, are reachable, and reject per §3.
4. Layer 1 (godog over the read-core feature dirs + skiplist) is green; parsed
   scenarios have reviewed golden `query.Query` snapshots.
5. Layer 2 targeted sentinel + reachability tests are green.
6. `just test` runs the whole suite green.
7. `query.Query` gains no new capability in Stage 0. Its encoding is restructured
   for type-safety — `Binding` and `Endpoint` are sealed sum types built only
   through smart constructors, so illegal states are unrepresentable — which
   requires a deterministic tagged-union `MarshalJSON` on each variant. This is a
   type-safety refactor, not model evolution; the represented information is the
   same.
