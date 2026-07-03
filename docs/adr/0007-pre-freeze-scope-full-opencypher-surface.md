# Pre-freeze scope grows from the read core to the full openCypher surface

ADR 0004's feature-complete target — read core plus the four capabilities of
Stages 1–5 — is no longer the freeze gate. The product goal is generating
type-safe repository code for practically any openCypher query, **reads and
writes**, and every capability the corpus exercises has to land in `query.Query`
**before** the freeze (bead `gqlc-cta`). Nine further stages (6–14) extend the
model to writes, `MERGE`, `CALL`, the full pattern surface, the remaining read
clauses, aggregations, quantifier/existential predicates, and a typed expression
surface. The type vocabulary — lists, maps, temporal scalars, `PATH` — becomes
part of the frozen interface and is settled by these stages, not deferred.

## Context

The Stage-5 progress meter is 550 TCK read-core scenarios, 380 parse-green, 170
PENDING via the four `ErrUnsupported*` sentinels. Every pending is valid
openCypher the current model cannot carry; each is now in scope for the freeze.
ADR 0004 was written when "feature-complete" meant the read core, so its
Stages-to-feature-complete list stops at 5 and its "out of scope throughout"
paragraph names writes, variable-length paths, and the wider predicate tree —
the very set the product goal now requires.

Retrofitting these after the freeze is the exact anti-cascade ADR 0004 was
written to prevent: it would reopen `query.Query` *with the resolver, codegen,
and generated driver code already attached to it*. The cheapest time to shape
the write-side and expression-typed model is now, while the parser is the only
consumer.

## Considered options

**Freeze at Stage 5 and add writes/expressions as post-freeze model revisions.**
Rejected: it inverts ADR 0004's own argument. The lock is meant to precede
consumer construction, so that model shape is decided against the corpus alone.
Freezing at Stage 5 would lock a model everyone knows is incomplete, then
reopen it once the resolver and codegen are written against it — cascading
churn through the downstream pipeline for every subsequent capability. The
freeze stops being a freeze.

**Freeze at Stage 5 and support the rest by string-passthrough only.** The
generated code already re-executes the original query text (ADR 0005), so a
write or a `CALL` could in principle be handed to the driver as an opaque
string. Rejected: a `CREATE`, a `MERGE`, or a `CALL` still has a **type
interface** — bound parameters and returned columns (for `CALL … YIELD`) —
that codegen must know. ADR 0005 justifies dropping *interface-neutral*
constructs from the model, not the interface itself. Without a modelled write
effect and a modelled procedure signature there is no argument list to type and
no result columns to emit; the "string passthrough" strategy collapses into a
proposal to not generate typed methods for these queries at all, which
contradicts the product goal.

**Extend Stages 1–5 to Stages 6–14 and freeze at feature-complete over the
whole corpus.** Chosen. The build discipline of ADR 0004 — test-first,
one-capability-per-slice, model churn cheap while the parser is the only
consumer — is exactly the right discipline for the expanded surface. What
changes is the endpoint, not the method.

## Decision

### Scope, in three buckets against the TCK

Every scenario in the TCK falls into one of three buckets. The buckets are the
scope boundary — not the sentinel taxonomy, which is an implementation detail
of the parser.

1. **Must-model.** Scenarios whose type interface the current model cannot
   express. This is what the four `ErrUnsupported*` sentinels flag today, and
   this is what Stages 6–14 add:
   - The write clauses `CREATE`, `MERGE`, `SET`, `DELETE`, `REMOVE` and the
     read-side clauses `UNWIND` and `CALL` — currently
     `ErrUnsupportedClause`. Writes introduce a statement kind (read vs. write)
     and modelled effects; a projection-less write statement is a legal query,
     so the "every query has a `Returns`" invariant of the read core must relax.
   - Pattern shapes the model cannot carry: named paths, variable-length
     relationships, and multi-type relationships — currently
     `ErrUnsupportedPattern`. A named path introduces `PATH` as a first-class
     type in the vocabulary; a variable-length edge changes the edge binding's
     cardinality; a multi-type edge widens the label set the resolver must
     match against `EdgeKey`.
   - Rich `RETURN`/`WITH` items outside the current projection sum —
     currently `ErrUnsupportedProjection`. These require a typed expression
     surface, because the projection's *result type* is what codegen emits.
   - Parameters outside the current binding-property + clause-slot set —
     currently `ErrUnsupportedParameter`. Expression-typed parameters
     (arithmetic, `IN` lists, list/map arguments) get a type from the
     expression they participate in, not from a binding property.

2. **Must-parse, thin model impact.** Most scenarios under
   `expressions/*`. Per ADR 0005 the parser drops interface-neutral structure,
   so an expression's *value semantics* pass through as original text and only
   its **result type** enters the model. A scalar expression at a `RETURN`
   or `WITH` position contributes a type; the same expression buried in a
   `WHERE` still contributes only its parameter references. The typed
   expression surface is small (a `Type` sum: scalars, `LIST`, `MAP`,
   temporals, `NODE`, `EDGE`, `PATH`, plus nullability) even though the
   expression grammar it types is large. Bucket 2 is where ADR 0005's
   accept-and-ignore posture does the most work.

3. **Out of scope.** Scenarios asserting a **runtime error** (division by
   zero, coercion failure at execution, non-null constraint violated on
   `CREATE`, uniqueness clashes on `MERGE`, procedure not found at runtime)
   and scenarios asserting **runtime result values or side effects** on a
   simulated graph. The parser is not an engine; per ADR 0005 the original
   text runs against the driver, which raises the runtime error verbatim. At
   the parse-level suite these reinterpret as: the query parses (positive) or
   the type interface is invalid (rejection at generation time). This is the
   same reinterpretation the read-core suite already applies to
   `NegativeIntegerArgument` / `InvalidArgumentType` on `SKIP`/`LIMIT` — the
   parser accepts, the runtime raises.

### Reversal of the read-core skiplist exclusions

Stages 0–5 treated four exclusions as deliberate scope cuts, expressed today
via three of the four `ErrUnsupported*` sentinels and their skiplist entries
(the current pending set — 170 of 550 read-core scenarios — is dominated by
these). All four exclusions are now in scope:

- **Named paths** (`p = (a)-[r]->(b)`) — `ErrUnsupportedPattern`. Requires
  `PATH` in the type vocabulary and a path binding kind alongside node and
  edge. Lands in Stage 8.
- **Variable-length relationships** (`-[*1..3]->`) —
  `ErrUnsupportedPattern`. Changes the edge binding's cardinality axis and
  the resolver's `EdgeKey` lookup (an endpoint pair with a hop range, not a
  single triple). Lands in Stage 8.
- **Multi-type relationships** (`-[:R1|R2]->`) —
  `ErrUnsupportedPattern`. Widens `LabelSet` on the edge binding; the
  resolver forms one candidate `EdgeKey` per type. Lands in Stage 8.
- **Rich `RETURN`/`WITH` expressions** — `ErrUnsupportedProjection`.
  Arithmetic over a projection, list/map literals, `CASE`, comprehensions,
  label predicates, nested aggregates, non-trivial function arguments.
  Requires the typed expression surface. Lands in Stage 6 (result typing) and
  Stage 10 (aggregations).
- **Write clauses** — `ErrUnsupportedClause`. Requires the statement-kind
  split and modelled effects. Lands in Stages 12–13.

Skiplist entries pinned to *value-level* rules the type interface does not
carry (ADR 0005, principle B1) — `NoVariablesInScope`, `UnknownFunction`,
`ColumnNameConflict`, `VariableTypeConflict`, `DifferentColumnsInUnion`,
`InvalidClauseComposition`, `NoExpressionAlias`, `NonConstantExpression`,
`NegativeIntegerArgument`, `InvalidArgumentType`, and `UndefinedVariable`
inside `ORDER BY` (the sort-key structure is snapshotted around a
parameter-only walk, so a sort-key name not present in the projected scope
never triggers `ErrUnboundVariable`) — are **not** reversed. They remain
accept-and-ignore, because generation-time acceptance plus runtime
rejection on the original text is still the correct treatment. Bucket 3
above is the same posture applied to the wider corpus.

### Procedure signatures as a compile-time input artifact

Standalone `CALL proc.name(args)` and `CALL proc.name(args) YIELD *` are
untypeable without knowing the procedure's argument and result types: neither
the parameter list nor the projection can be typed from the query text alone.
The TCK acknowledges this by declaring signatures as background: the step
`there exists a procedure test.my.proc(in :: NUMBER?) :: (out :: STRING?)`
appears verbatim in `clauses/call/Call3.feature` and its neighbours.

`CALL` therefore takes a **procedure registry** as a second compile-time
input alongside the schema. The registry is a set of typed signatures
(`name → (in-types, out-types)`) supplied by the user, in the same
"user-authored, machine-read at generation time" role the schema already
plays. An unknown procedure at generation time is an error by design — the
symmetric analogue of a `MATCH` against an unknown label. The registry's
concrete on-disk format and package placement are Stage-14's job (spec
`docs/specs/cypher-query-parser-stage-14.md`); this ADR commits only to the
registry being a **compile-time input**, not runtime discovery.

> _Note (Stage 14, ADR 0004): the registry lives in a new package,
> `internal/procsig`, dependency-independent of both `internal/query`
> and `internal/schema`. Its `TypeToken` sum
> (`INTEGER`, `FLOAT`, `STRING`, `NUMBER`) is decoupled from
> `query.Type` — sizing to the TCK corpus census (INTEGER?×47,
> STRING?×30, NUMBER?×4, FLOAT?×2, zero BOOLEAN — no gold-plating).
> `NUMBER` stays a signature-only marker (assignable-from
> `INTEGER`-or-`FLOAT` at the argument site); the cypher-package
> bridge maps it to `TypeUnknown` on the wire so no signature-time
> vocabulary leaks into the freeze surface. `cypher.New` widens to
> `func New(opts ...Option) query.Parser` with a `WithRegistry`
> option — `Parse`'s signature is untouched. The on-disk format
> (YAML / JSON / `.procsig`) is intentionally deferred out of
> Stage 14: codegen consumes `procsig.Registry` values directly, so
> the freeze cut proceeds on the in-memory API alone; a follow-up
> bead tracks the file surface for the CLI consumer._

### Stages 6–14, ordered by dependency

Each stage is a TDD slice per ADR 0004, with a spec in `docs/specs/` written
before the slice starts (the Stage-0–5 convention). Stages 1–5 are unchanged
and are complete; 6–14 extend the list. Each carries its bead ID and the
GitHub issue it closes; the epic is `gqlc-33k` (#40).

6. **Expression core and result typing** — `gqlc-97z` (#49). The typed
   expression surface: a `Type` sum covering scalars, `LIST`, `MAP`, `NODE`,
   `EDGE`, plus nullability. Rich `RETURN`/`WITH` projections become
   typed, retiring `ErrUnsupportedProjection` for the non-aggregate cases.
   Prerequisite for Stages 9–11.
7. **Temporal types** — `gqlc-9b4` (#47). Extends the `Type` sum with
   `DATE`, `TIME`, `DATETIME`, `LOCAL_TIME`, `LOCAL_DATETIME`, `DURATION`,
   and a temporal literal grammar. Independent slice: temporal constructors
   are library functions whose return types the Stage 6 sum must recognise.
8. **Full pattern surface** — `gqlc-9nl` (#48). Named paths (introducing
   `PATH` into the `Type` sum and a path binding kind), variable-length
   relationships (edge cardinality axis), multi-type relationships (widened
   `LabelSet` on the edge binding). Retires `ErrUnsupportedPattern`.
9. **Read-clause completion** — `gqlc-ydk` (#46). `UNWIND`,
   post-`WITH` `WHERE`, and `ORDER BY` in `WITH`. `UNWIND` binds a scalar
   whose element type comes from the source list — depends on Stage 6.
10. **Aggregations** — `gqlc-2av` (#45). The full aggregate surface
    (`count`, `sum`, `avg`, `min`, `max`, `collect`, `stdev`, `percentile*`)
    inside the expression grammar, not only at the RETURN top level as in
    Stage 3. Grouping semantics stay below the type-interface boundary
    (ADR 0005, B1); the model records the aggregate's result type and
    cardinality-bearing kind. Depends on Stage 6.
11. **Predicate expressions** — `gqlc-665` (#44). Quantifiers (`ALL`,
    `ANY`, `NONE`, `SINGLE`), pattern predicates, and existential subqueries
    (`EXISTS { ... }`). Predicates contribute parameter uses and, for
    `EXISTS`, a boolean result type; predicate *structure* stays below the
    boundary per ADR 0003/0005. Depends on Stage 6.
12. **Write clauses** — `gqlc-mkv` (#43). `CREATE`, `DELETE`,
    `DETACH DELETE`, `SET`, `REMOVE`. Introduces a statement-kind split
    (`Read` vs. `Write`) on `Query`, a modelled effect per write clause, and
    projection-less statements (a write with no `RETURN` is a valid
    complete query — codegen emits a no-result method). Retires
    `ErrUnsupportedClause` for the write set.
13. **MERGE** — `gqlc-cqh` (#42). `MERGE` with `ON CREATE SET` /
    `ON MATCH SET` branches. Requires the write-effect vocabulary of
    Stage 12; the two `ON …` branches are two effect sequences on the same
    matched binding. Layered after 12 rather than merged into it because
    the read/create alternation is a distinct modelling decision (the
    matched binding is not nullable in the way `OPTIONAL MATCH` is).
14. **CALL and the procedure registry** — `gqlc-im1` (#41). Standalone
    `CALL`, `CALL … YIELD`, in-query `CALL`, and the procedure registry as
    a second compile-time input. Retires the last of `ErrUnsupportedClause`.

After Stage 14, a corpus-sweep task (`gqlc-x6u`, #51) runs the whole TCK
(not only the read-core dirs) against the parser, resolves the remaining
skiplist against the three buckets above, and confirms every scenario is
either parse-green, pinned to bucket 3, or bug-tracked. The freeze
(`gqlc-cta`) then locks `query.Query`, revises ADR 0003 and CONTEXT.md, and
opens the resolver, codegen, and generated driver work.

Ordering between stages is by dependency, not corpus size, and may be
resequenced as the corpus dictates (per ADR 0004). Stage 6 is a hard
prerequisite for Stages 9–11; Stage 12 for Stage 13; the rest are
independent.

### Type vocabulary is part of the freeze

The freeze locks not only the model's structure but also its **type sum**.
`LIST`, `MAP`, the six temporal scalars, and `PATH` must exist in that sum
before `query.Query` is locked, because adding a type to the vocabulary
after the resolver and codegen are written against it is precisely the
downstream cascade ADR 0004 exists to prevent. Stages 6, 7, and 8 own the
respective additions; Stage 12 owns nothing new in the type sum but adds a
non-projecting statement kind, which is a structural completion the freeze
also depends on.

## Consequences

- ADR 0004's "Stages to feature-complete" list is extended, not replaced.
  Stages 0–5 remain the read-core build, complete as of Stage 5. Stages 6–14
  extend the same build discipline (test-first, one capability per slice,
  model unlocked) to the write side and the full expression surface. The
  freeze gate moves to the end of Stage 14 + corpus sweep. See the amendment
  note on ADR 0004.
- **Codegen builds against a larger, later, more complete model.** The
  freeze is delayed, but the cost of delay is bounded by the same discipline
  ADR 0004 justifies: while the parser is the only consumer, model churn is
  cheap. What we buy is not having to revise the model — and every consumer
  written against it — nine more times after codegen exists.
- The parser gains a second compile-time input (the procedure registry)
  alongside the schema. `internal/schema` and the registry occupy the same
  role — user-authored, machine-read at generation time — so the CLI
  gains a registry input flag and the codegen orchestrator gains one more
  argument. The registry package is new (Stage 14) and sits under
  `internal/` alongside `schema` and `query`; it does **not** import
  either.
- The `ErrUnsupported*` sentinels remain the progress meter throughout
  Stages 6–14 and shrink as each stage lands, exactly as during Stages
  0–5. `ErrUnsupportedPattern` is retired by Stage 8;
  `ErrUnsupportedProjection` largely by Stage 6 (aggregate residues in
  Stage 10); `ErrUnsupportedParameter` incrementally as the expression
  surface widens; `ErrUnsupportedClause` by Stages 12–14. The two real
  rejections (`ErrUnboundVariable`, `ErrVariableKindConflict`) are
  unaffected.
- The type-interface boundary (ADR 0005, principle B1) is the load-bearing
  line for bucket 3 and for the residual skiplist. Every value- or
  runtime-error scenario that lands in bucket 3 is a case of "the parser
  accepts, the driver raises on the original text." Widening the corpus
  widens the surface B1 covers, but does not change the principle.
- The skiplist grows beyond the read-core set as new dirs enter the suite
  in Stages 6–14, but every new entry is pinned to bucket 3 (a runtime or
  value-level rule below the type-interface boundary), never to "not
  supported yet" — the sentinels carry that meaning.
