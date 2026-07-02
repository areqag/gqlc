# gqlc

gqlc is an analogue of sqlc for graph query languages (GQL, Cypher): it reads
graph schema files and queries and generates type-safe code. This glossary
covers gqlc's domain language: the GQL **schema** side and the Cypher **query**
side.

## Schema language

**Graph type**:
The named, typed shape of a property graph, declared by `CREATE PROPERTY GRAPH
TYPE <name> AS { ... }`. Contains node types and edge types. Its body is an
**order-independent set** of element types: reordering definitions does not change
the graph type, and an edge may reference a node type declared later in the body.
That forward-reference allowance is why endpoint resolution is a post-walk pass
(see `internal/schema/gql/resolve.go`), not done inline during collection. The
body must hold at least one element type — the grammar's `elementTypeList` rejects
an empty `AS {}`, so an empty graph type is not representable and surfaces as a
syntax error, not a special case.
_Avoid_: schema (overloaded — reserve for the parsed in-memory model).

**Node type**:
A kind of vertex in the graph type, defined by a label set and a set of
properties. Must carry at least one label.

**Edge type**:
A kind of directed relationship between two node types, defined by a source
endpoint, a label set, a target endpoint, and a set of properties. Must carry
at least one label.
_Avoid_: relationship, arc.

**Label set**:
The set of labels that identifies a node type or edge type. Its canonical,
order- and duplicate-independent form is the identity used to key node and edge
types.

**Type name**:
An optional explicit identifier for a node or edge type (the `nodeTypeName` /
`edgeTypeName` grammar tokens), stored on the type's `Name` field. Distinct
from both the label set and the local alias; absent in idiomatic patterns like
`(p :Person {...})`. A type is "named" by virtue of its labels, not this field.

**Alias**:
A local name bound to a node type within a single graph type declaration (the
`p` in `(p :Person { ... })`). Used by edge patterns to reference their
endpoints; it is declaration-local and not part of the persisted identity.

**Endpoint**:
The source or target of an edge type, written in source as an alias or an
inline node-type filler, and resolved to the referenced node type's label set.
An endpoint that resolves to a node type that was never declared is an error.

**Direction**:
Edge types are directed. A left-pointing edge `(a) <-[:X]- (b)` is canonicalised
to its source→target form (`b` → `a`), so an edge type's stored identity is
independent of the direction it was written in. Undirected edges are not
supported.

## Query language

**Query**:
A single openCypher query — the unit the parser consumes and lowers into the
structural model. Exactly one query per parse call; a `UNION` of single queries
is still one query. A source file may hold many queries, but splitting a file
into individual queries (and naming them) is an orchestration concern, outside
the parser.
_Avoid_: statement (reserve for the grammar's `oC_Statement`).

**Binding**:
A query variable bound to a graph entity — a node or an edge — within a single
query, carrying its labels as written and, for an edge, its endpoints. The
query-side analogue of the schema's **alias**, and the anchor a return item or
parameter traces back to so the resolver can reach a schema type. Labels may be
empty: an unlabelled binding's type is inferred from the edges that touch it.
_Avoid_: match (reserve for the MATCH clause); node/edge type (reserve for the
schema's element types).

**Variable**:
The bare name a query author writes for a binding (the `p` in `(p:Person)`). A
binding is a variable plus the entity it is bound to.

**Nullable**:
A flag on a binding signalling that it may have no match on a given row — the
binding, and every return item that traces back to it, may be absent. Set on
bindings first introduced inside an `OPTIONAL MATCH` clause; the surface
keyword is `OPTIONAL`, the lowered attribute is **nullable**, mirroring the
schema side's type-system posture. Codegen emits the corresponding result as
a pointer or option type post-freeze.
_Avoid_: optional (reserve for the Cypher keyword `OPTIONAL MATCH`).

**Endpoint**:
The source or target of an edge binding, written as a reference to a named node
binding or as inline labels for an anonymous node, and canonicalised to
source→target order so it forms a `schema.EdgeKey`. An edge is not identified by
its label alone — the same label may connect different endpoint pairs — which is
why an edge binding carries its endpoints.

**Use**:
One position where a parameter appears in a query. A closed sum of
PropertyUse and ClauseSlotUse: a property use binds the parameter to a
binding property (the `$threshold` in `WHERE a.age > $threshold`); a
clause-slot use places the parameter in a SKIP or LIMIT clause whose type
is fixed by the clause (an integer) rather than inferred from a binding.
Every parameter carries a list of uses in first-appearance order, which
the resolver unifies into a single type post-freeze.
_Avoid_: site (overloaded with the spec's "fail-site"), occurrence.

**Parameter**:
A query input (openCypher `$name`), deduplicated across the query in
first-appearance order. Carries its **uses** — value-positions it appears
in, each either a property reference or a clause slot — so the resolver
can infer its type. Becomes a generated method argument.
_Avoid_: argument (reserve for the generated code).

**Return item**:
One column of a query's result, named by an explicit alias or derived from its
source text. It carries a **projection** describing what it projects — a binding
reference, a scalar literal, a function call, or an aggregate. A query's result is
an ordered, duplicate-preserving list of return items, or the `RETURN *` wildcard
over all in-scope bindings; it becomes a generated result.

**Projection**:
What a return item projects: a closed sum of a binding reference (`var` /
`var.prop`), a scalar literal, a non-aggregate function call, or an aggregate. It
carries only the bindings the resolver must trace (the `var` / `var.prop` refs)
and, for an aggregate, the cardinality-bearing kind — never the expression tree
(ADR 0003) nor a non-aggregate function's identity, which live below the
type-interface boundary (ADR 0005) and are re-executed from the original query
text. A `RETURN *` is not a projection but a query-level wildcard (see **Return
item**).
_Avoid_: expression (reserve for the grammar's `oC_Expression`; a projection is
the curated subset the model carries).

**Aggregate**:
A projection over an aggregating function (`count`, `sum`, `collect`, `min`,
`max`, `avg`, `stDev`/`stDevP`, `percentileCont`/`percentileDisc`). Distinguished
from an ordinary function call because it collapses matched rows into groups,
changing result cardinality — the one function distinction the model carries
pre-freeze. `count(*)` is the degenerate case: a count over rows that references
no binding. Grouping-key semantics — which non-aggregate columns form the group —
are a resolver concern, entangled with `WITH`, and out of scope until then.

**Resolver**:
The stage that resolves a parsed query against the model, typing each return item
and parameter — a pure function of `(query.Query, schema.Schema)`. Resolution
that completes without error is what makes a query a **validated query**; a query
it cannot resolve is rejected.

**Validated query**:
A query the resolver has resolved against the model (`schema.Schema`) without
error — the trustworthy, schema-checked invariant. Resolution is a distinct stage
that runs after parsing; the parser is schema-agnostic and never produces a
validated query on its own. Once it passes, every query in the application is
valid or the application halts.

## Flagged ambiguities

- **"Parsed"** splits into a syntactic step and a schema-checked invariant.
  Reserve **parsed** for "syntactically lowered into the query model" — what the
  Cypher parser produces, schema-free; use **validated** for "checked against the
  model and supported by it". A parsed query is not yet a validated query.
- **"Schema"** means two things: the GQL source construct (use **graph type**)
  and the parsed Go model `schema.Schema` (use **the model**). Keep them
  distinct.
- **"Name"** is overloaded across the explicit **type name**, the local
  **alias**, and the **label set**. "A node/edge must have a name" resolves to
  "must have a non-empty label set"; the explicit type name remains optional.
