# gqlc

gqlc is an analogue of sqlc for graph query languages (GQL, Cypher): it reads
graph schema files and queries and generates type-safe code. This glossary
covers the language of the GQL **schema** side.

## Language

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
Edge types are directed. A left-pointing edge `(a) <-[:X]- (b)` is canonicalized
to its source→target form (`b` → `a`), so an edge type's stored identity is
independent of the direction it was written in. Undirected edges are not
supported.

## Flagged ambiguities

- **"Schema"** means two things: the GQL source construct (use **graph type**)
  and the parsed Go model `schema.Schema` (use **the model**). Keep them
  distinct.
- **"Name"** is overloaded across the explicit **type name**, the local
  **alias**, and the **label set**. "A node/edge must have a name" resolves to
  "must have a non-empty label set"; the explicit type name remains optional.
