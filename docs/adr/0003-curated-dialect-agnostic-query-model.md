# A curated, dialect-agnostic query model, resolved separately from the schema

> _Note (ADR 0004): the "stable contract" framing below holds once the query
> parser is feature-complete. Until then the query model is provisional and
> evolves slice by slice; no consumer is built against it before the freeze._

The query parser lowers one query into `query.Query` — a curated, gqlc-owned
domain model (the entities a query binds, its parameters, its return items), not
a parser AST. Parsing is schema-agnostic: it records what a query says. A
separate resolution stage — a pure function of `(query.Query, schema.Schema)` —
types the query's parameters and results and rejects anything the schema does not
support. `schema` and `query` are siblings over a shared `graph` vocabulary
(`LabelSet`, `LabelSetKey`, `PropertyType`, `EntityKind`) and never import each
other.

## Considered options

A faithful openCypher AST would be lossless, but it couples the whole downstream
pipeline — resolution and codegen — to one grammar's shape, so a second dialect
or a rewritten parser would force a new model and a new resolver. The curated
model is lossy by design but is the stable contract every dialect lowers into and
that resolution/codegen are written against once. This mirrors the schema side,
where `schema.Schema` is a curated model, not a GQL parse tree.

## Consequences

The model must carry enough that resolution stays a pure function of the two
models. In particular, edge bindings record their **endpoints**: an edge is not
resolvable from its label alone, because `schema.EdgeKey` is the
source/label/target triple, so the resolver needs each endpoint's labels (via a
named binding or inline) to form the key — and this is also what lets it infer an
unlabelled binding's type by walking the edges that touch it. Return items and
parameters carry `Ref`s back to their bindings so the resolver can reach a schema
type. Curation means deciding what to keep; getting it wrong means revisiting the
model. Pattern topology beyond edge endpoints (variable-length paths, predicates)
and the full expression tree are deliberately outside the initial model.

> _Note (Stage 3, ADR 0004): the curated subset now includes a closed
> **projection** sum for return items — a binding reference, a scalar literal, a
> function call, or an aggregate — plus a query-level `RETURN *` wildcard. It
> carries only the `Ref`s the resolver must trace and, for an aggregate, the
> cardinality-bearing kind; the expression tree and a non-aggregate function's
> identity stay below the type-interface boundary (ADR 0005), holding the line
> this ADR draws._
>
> _Note (Stage 4, ADR 0004): the curated subset now includes the query's
> **branch/part structure** — a `Query` is a list of `UNION`-joined branches,
> each an ordered chain of `WITH`-bounded parts, each part holding its own
> bindings and projection. It carries the two structural axes the resolver needs
> — sequential scope chaining (`WITH`) and parallel branches with their join kind
> (`UnionKind`, the cardinality-bearing distinction, like the aggregate kind) —
> and nothing more: not grouping keys, not column union-compatibility, and still
> none of the expression tree. The per-part bindings are also the per-clause
> structure ADR 0006 names for cross-`WITH` nullable flow-typing. The
> no-expression-tree line holds._
>
> _Note (Stage 5, ADR 0004): the curated subset now includes the edge
> **direction marker** — a binary `directed` flag on the edge binding (true for a
> one-arrow `-->`/`<--`; false for an undirected `--` or `<-->`). It carries the
> structural fact that an undirected edge has **no authoritative orientation**, so
> its endpoints are recorded in textual order rather than canonicalised to one
> source→target `EdgeKey`. It does **not** carry the orientation-trial policy
> (try both orientations against the directed schema, and what a multi-match
> means): that is the resolver's, exactly as the unknown-edge-type lookup is. The
> marker is the one bit the resolver branches on — form one candidate key or two —
> and nothing of the arrow spelling or the expression tree. The no-expression-tree
> line holds._
>
> _Note (Stage 6, ADR 0004/0007): the curated subset now includes a closed
> **`Type` sum** for projection result types (`bool`, `int`, `float`, `string`,
> `null`, `list<T>`, `map`, `node`, `edge`, plus a distinguished `unknown` for
> types the parser cannot compute schema-free) and an **`ExprProjection`**
> variant on the `Projection` sum, carrying the result type of a rich
> projection expression and the bindings it touches — **but not the
> expression tree itself**. A property lookup, a function call, an aggregate,
> and any NULL-participating or property-participating arithmetic type as
> `unknown`; the resolver upgrades these from the schema, and the runtime
> re-executes the original text (ADR 0005). `TypeAny` is deliberately absent
> — the parser's honest "I cannot tell" is `TypeUnknown`, and adding two
> names for the same posture would confuse the boundary. The `Type` sum is
> incremental: Stage 7 adds temporal types, Stage 8 adds `PATH`; the freeze
> ADR locks the sum. `Use` gains an **`ExprUse`** variant so a `$param`
> inside a rich expression is recorded (its type comes from the enclosing
> expression, not from a binding). The no-expression-tree line holds._
