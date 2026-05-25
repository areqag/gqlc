# A curated, dialect-agnostic query model, resolved separately from the schema

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
