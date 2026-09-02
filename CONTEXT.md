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
A graph type may instead be declared as a copy of another (`CREATE GRAPH TYPE
<name> COPY OF <reference>`); the reference is resolved against the
**catalogue** (ADR 0034), and the result is the referenced graph type's body
under the new declared name.
_Avoid_: schema (overloaded — reserve for the parsed in-memory model).

**Catalogue**:
The space of files a graph type reference (`COPY OF /path/Name`) resolves
against. gqlc's catalogue is the filesystem: a directory tree rooted at the
directory of the configured schema file, one graph type per `.gql` file, the
reference's trailing segment naming both the file (plus `.gql`) and the graph
type declared inside it. The current schema of a file is that file's own
directory; a reference climbing past the root is refused, not resolved
elsewhere. Decided in ADR 0034.
_Avoid_: registry, search path (there is none — one root, no fallback list).

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
supported. (The query-side **Direction** differs: a query may write an undirected
pattern, which the parser lowers as a marker and the resolver tries both ways
against this directed schema — see the query-language **Direction** entry.)

## Query language

**Query**:
A single openCypher query — the unit the parser consumes and lowers into the
structural model. Exactly one query per parse call; a `UNION` of single queries
is still one query, modelled as several **branches** under that one query. A
query is one or more branches (combined by `UNION`), each an ordered chain of
one or more **query parts** (split by `WITH`); the query also carries its
**parameters**, which are query-wide and not scoped to a part, and its
**statement kind** — read vs. write — the binary axis the driver's transaction
mode is chosen from (Stage 12). A source file may hold many queries, but
splitting a file into individual queries (and naming them) is an orchestration
concern, outside the parser.
_Avoid_: statement (reserve for the grammar's `oC_Statement`).

**Branch**:
One of the parallel result-producing arms of a query (`oC_SingleQuery`), as
joined by `UNION`. A query without `UNION` is one branch; *N* `UNION`s make
*N+1* branches, combined left to right. All branches of a query produce
union-compatible result columns; the first branch names the result columns (an
openCypher rule the resolver enforces — the parser records each branch's columns
verbatim and does not merge them). A branch is itself an ordered chain of one or
more **query parts**.
_Avoid_: arm, leg; alternative (reserve for grammar alternatives). Distinct from
the schema-side **endpoint**/**alias** vocabulary — a branch is a query-level
structure only.

**Query part**:
One scope segment of a branch, bounded by `WITH`. A branch is a sequence of
parts: every non-final part ends in a `WITH` (an intermediate projection that is
also a scope boundary), and the final part ends in a `RETURN` — **or**, since
Stage 12, in one or more write clauses with no `RETURN` (a projection-less part
whose result is zero columns). Each part carries its **own** bindings (the
entities its own `MATCH` or `CREATE` clauses introduce), its return items,
its `RETURN *`/`WITH *` wildcard flag — the flat single-scope shape,
now scoped to one part — and its **effects** (the write clauses of that part in
walk order, Stage 12). A `Ref` in a part resolves against that part's bindings
or a name the previous part's `WITH` carried forward; a name not carried by a
`WITH` is out of scope in later parts.
_Avoid_: segment, stage (reserve **stage** for the staged build plan, ADR 0004);
scope (use "the part's scope" descriptively, but the noun is **query part**).

**Union**:
The combinator that joins two **branches** into one query. Carried as a kind —
`UNION` (distinct: collapses duplicate result rows) versus `UNION ALL` (keeps
duplicates) — because that distinction changes result cardinality, which the
generated code models; it is the branch-level analogue of the **aggregate**
kind. Column union-compatibility across branches is a value-level result-shape
check below the type-interface boundary, not carried by the model.
_Avoid_: join (reserve for graph edge traversal / SQL vocabulary), merge
(reserve for the `MERGE` write clause).

**Binding**:
A query variable bound to a graph entity — a node or an edge — or (Stage 8)
to a **named path**, or (Stage 9) to the current value of an **UNWIND**
source list — within a single **query part**, carrying its labels as
written and, for an edge, its endpoints. The query-side analogue of the
schema's **alias**, and the anchor a return item or parameter traces back to
so the resolver can reach a schema type. A binding is scoped to the part
whose `MATCH` (or `UNWIND`) introduced it; it reaches a later part only if
that part's preceding `WITH` carries its variable forward (a binding not
carried by a `WITH` is out of scope downstream). Re-`MATCH`ing a carried
name in a later part is a fresh binding in that part, distinct from the
original — which is the per-part structure the resolver uses to flow-type
nullability across a `WITH` (ADR 0006). Several occurrences of one variable
in one part are conjunctive — they describe the same entity — so a node
binding's labels accumulate to their union while an edge binding's types
narrow to their intersection, a relationship having exactly one type; an
empty intersection is refused as unsatisfiable. Labels may be empty: an
unlabelled binding's type is inferred from the edges that touch it. An edge
binding also carries a **direction** marker (a directed edge stores its endpoints
canonically source→target; an undirected edge stores them in textual order
with the resolver trying both orientations, see **Direction**, query side)
and an **edge cardinality** axis (see **Hop range**) distinguishing
single-hop edges from variable-length edges.
_Avoid_: match (reserve for the MATCH clause); node/edge type (reserve for the
schema's element types).

**Named path**:
A query variable bound to the path a pattern element composes (the `p` in
`MATCH p = (a)-[r]->(b) RETURN p`). Modelled as a **path binding** —
kind `path` — holding the path variable and the shape-faithful, tagged-sum
ordered list of its members: named nodes and edges reference the part's own
entity bindings by variable (so a path binding does not co-own them); an
anonymous edge inside a named path surfaces as an anonymous-edge slot with
no name (so it never competes with a user-chosen variable in the part's
`byVar` namespace, unlike a synthetic-string scheme), and an anonymous
intermediate node inside a named path surfaces as an anonymous-node slot
so the members list is shape-faithful (codegen can reconstruct the whole
path shape from the members alone). A path binding's projected result type
is the `path` variant of **type** — the Stage-8 addition to the type sum.
Never nullable at the binding level: `OPTIONAL MATCH p = ...` flows
nullability through the member bindings themselves, not the path binding.
_Avoid_: `p` (a variable-name colloquialism); named-path variable (use "path
binding" for the modelled entity, "named path" for the source-level clause).

**Hop range**:
An **edge binding**'s optional variable-length hop range — the `hops` axis
on an edge — recording the `min..max` bounds of a variable-length
relationship (`-[r*1..3]->`, `-[r*]-`). Absent (nil) for a single-hop edge —
the pre-Stage-8 case, where an edge binding refers to one graph edge; present
for a variable-length edge, where the binding refers to a **list of graph
edges** and its projected result type is `list<edge>` rather than `edge`.
Either bound may be absent (unbounded); a fixed hop count (`-[r*3]->`) sets
`min = max`; a negative bound is unrepresentable (the constructor rejects
it — the only invariant the type alone cannot express). Compose freely with
the edge's direction axis and label set: a var-length undirected multi-type
edge is one binding carrying all three facts. Codegen reads `Hops()` to emit
list-typed method results for var-length edges without adding a distinct
binding variant — the cardinality axis mirrors the direction axis in that
respect.
_Avoid_: length (colloquial; reserve for "path length", a separate concept);
range (ambiguous with SKIP/LIMIT paging).

**Unwind binding**:
A query variable bound to the current value drawn from an **`UNWIND`** clause's
source list (the `x` in `UNWIND [1, 2, 3] AS x`). Modelled as an **unwind
binding** — kind `unwind` — carrying the AS variable and the source
expression's **element type** (`TypeInt` for `[1, 2, 3]`, `TypeUnknown` for
`range(1, 3)` or `null` or a `$param` — the parser records the honest
"cannot tell" instead of guessing, and the resolver upgrades from the
schema). An UNWIND is a reading clause distinct from `MATCH`, so an unwind binding is
not a graph entity: it has no labels, no endpoints, no `EntityKind()` — and
the resolver never forms a schema key from it. Never nullable at Stage 9:
an empty or null source list yields zero rows at runtime, a row-cardinality
fact below the type-interface boundary (ADR 0005) rather than a per-binding
static nullability.
_Avoid_: unwind variable (use "unwind binding" for the modelled entity,
"UNWIND source" for the list expression the clause draws from).

**Variable**:
The bare name a query author writes for a binding (the `p` in `(p:Person)`). A
binding is a variable plus the entity it is bound to. A `WITH` may also introduce
a name that is not a binding: `WITH a.x AS n` carries the scalar `n` into the
next part (a projected name, not an entity), whereas `WITH a` carries the
*binding* `a` forward by its variable. The next part's in-scope names are exactly
those a `WITH` carries — bindings and projected names alike — which is why a name
the `WITH` drops is unbound downstream.

**Nullable**:
A flag on a binding signalling that it may have no match on a given row — the
binding, and every return item that traces back to it, may be absent. Set on
bindings first introduced inside an `OPTIONAL MATCH` clause; the surface
keyword is `OPTIONAL`, the lowered attribute is **nullable**, mirroring the
schema side's type-system posture. Codegen emits the corresponding result as
a pointer or option type.
_Avoid_: optional (reserve for the Cypher keyword `OPTIONAL MATCH`).

**Endpoint**:
The source or target of an edge binding, written as a reference to a named node
binding or as inline labels for an anonymous node. For a **directed** edge the
endpoints are canonicalised to source→target order (a left arrow flips), so the
pair forms a single `schema.EdgeKey`. For an **undirected** edge there is no
canonical order to pick: the endpoints are recorded in **textual order** and the
edge's `directed` marker tells the resolver that order is not authoritative — it
forms *two* candidate keys (both orientations) and accepts the edge if either
resolves. An edge is not identified by its label alone — the same label may
connect different endpoint pairs — which is why an edge binding carries its
endpoints.
_Avoid_: the schema-side **endpoint** sense (always resolved to a label set; the
query side may carry an unresolved inline anonymous endpoint).

**Direction**:
Whether an edge binding has an authoritative orientation. Carried as a marker
(`directed`): a one-arrow pattern (`-->` / `<--`) is **directed** — its left/right
orientation is discharged at lowering into a canonical source→target endpoint
pair; a no-arrow (`--`) **or** both-arrow (`<-->`) pattern is **undirected** —
openCypher treats both spellings as undirected, so they collapse to one marker
state, and the endpoints stay in textual order for the resolver to try both ways.
Distinct from the schema-side **Direction**: the schema is directed-only (left
arrows canonicalise, undirected is unsupported, every edge type is one
`schema.EdgeKey`), whereas the query side *admits* undirected patterns and lowers
them as a deferred-orientation marker — orientation against the directed schema is
a resolver concern, not the parser's. The marker is binary; the model carries no
left-vs-right distinction (the canonical flip erases it) and no `--`-vs-`<-->`
distinction (no read-surface semantics depend on the spelling).
_Avoid_: orientation (use for the resolver's per-match source→target choice, which
the model does not carry); the schema-side directed-only **Direction** sense.

**Use**:
One position where a parameter appears in a query. A marker-sealed sum of
PropertyUse, ClauseSlotUse, and ExprUse — the whole set of types declaring the
`isUse` marker, which is narrower than the set satisfying the interface
(AGENTS.md, "Closed sum types"): a property use binds the
parameter to a binding property (the `$threshold` in
`WHERE a.age > $threshold`); a clause-slot use places the parameter in a
SKIP or LIMIT clause whose type is fixed by the clause (an integer)
rather than inferred from a binding; an expression use records that the
parameter appears inside a rich scalar expression whose enclosing result
**type** the model carries — the parameter's own type is inferred by the
resolver from the expression it participates in. Every parameter carries a
list of uses in first-appearance order, which the resolver unifies into a
single type.
_Avoid_: site (overloaded with the spec's "fail-site"), occurrence.

**Parameter**:
A query input (openCypher `$name`), deduplicated across the query in
first-appearance order. Carries its **uses** — value-positions it appears
in, each either a property reference, a clause slot, or an expression use
(including value expressions on the right-hand side of a **write clause**,
Stage 12: a `$param` under a `SET n.age = $param` records an `ExprUse`
against the value expression's Stage-6 type, position `ExprInSetValue`
— the producer-side write-value axis distinct from a projection column's
consumer role; a `$param` under a rich DELETE target records
`ExprInDeleteTarget`, and the resolver keys on the producer/consumer axis
alongside the enclosing expression's type). Becomes a generated method
argument.
_Avoid_: argument (reserve for the generated code).

**Statement kind**:
A query-wide binary axis distinguishing a read query (`StatementRead`) from
a query that modifies the graph (`StatementWrite`) — the axis a driver
branches on to pick a transaction mode. Set to `StatementWrite` iff the
query contains at least one **write clause** at any part of any branch
(a write clause suppressed inside an `EXISTS { ... }` subquery does NOT
flip the axis — the outer query does not modify the graph, and openCypher
rejects that composition anyway). Introduced in Stage 12 alongside the
write-clause surface; before Stage 12 every query was implicitly a read,
and the axis is a strictly additive `Query` field with `read` as the
zero-value default.
_Avoid_: read-write (three-state axes conflate two distinct decisions —
the driver's tx mode is binary); statement (the grammar's `oC_Statement`
sense; the axis is on the parsed `Query`).

**Effect**:
One write operation the query performs at a specific **query part** — the
per-part analogue of a **return item**. A marker-sealed sum — the whole set of
types declaring the `isEffect` marker, which is narrower than the set
satisfying the interface (AGENTS.md, "Closed sum types") — of `CreateEffect`
(one `CREATE` clause, carrying the ordered list of binding variables the
clause introduced), `MergeEffect` (one `MERGE` clause — a match-or-create
alternation, kept distinct from `CreateEffect` so the "match this if you can"
half survives — carrying the ordered list of binding variables the clause
introduced plus the `ON MATCH` and `ON CREATE` actions, each a `SetEffect`),
`DeleteEffect` (one `DELETE` / `DETACH DELETE`,
carrying the targeted Refs (bare `var` / `var.prop` targets), the
rich-expression refs (everything else — the two slices partition the
DELETE expressions, never both, never neither, so no delete is silently
absent), and a `Detach` flag), `SetPropertyEffect` (one `SET n.prop = value`
item, carrying the property target Ref, the value's Stage-6 type, and its
touched refs; a nested LHS `n.a.b` rejects with `ErrNestedPropertyTarget`
rather than truncating),
`SetEntityEffect` (one `SET var = value` or `SET var += value` item,
carrying the target variable, a `SetOp` axis for `=` vs `+=`, the value's
Stage-6 type, and its refs — one variant, one axis, mirroring
`EdgeBinding.directed`), `SetLabelsEffect` (one `SET var:Labels` item,
carrying the variable and the label set), `RemovePropertyEffect` (one
`REMOVE var.prop` item, carrying the property target Ref), and
`RemoveLabelsEffect` (one `REMOVE var:Labels` item, carrying the variable
and the labels). Each part's `Effects` slice preserves textual walk
order — a driver replaying the query executes them in that order — and
carries no expression tree (ADR 0003): a rich SET value's `structure`
lives below the type-interface boundary (ADR 0005), while its
`result type` and the bindings it touches enter the model.
_Avoid_: mutation (colloquial), side effect (the TCK's runtime-assertion
term for row-count deltas — reserve for that specific TCK vocabulary).

**Write clause**:
A clause that produces one or more **effects** rather than a projection —
one of `CREATE`, `DELETE` / `DETACH DELETE`, `SET`, `REMOVE`, and (Stage 13)
`MERGE`. `CREATE` alone introduces bindings (its pattern reuses `MATCH`'s
grammar and enters the same per-part `Bindings` slice); the others operate
against already-bound entities. A **query part** with at least one write
clause is projection-legal — a `RETURN` after `SET` is a read-back of the
mutated bindings — and also **projection-less-legal**: a part whose only
clauses are writes (no `RETURN`, no `WITH`) produces zero result columns
and is the shape codegen emits a no-result method for.
_Avoid_: updating clause (the grammar's `oC_UpdatingClause`, which
subsumes `MERGE` — reserve for the grammar); mutating clause (colloquial).

**Return item**:
One column of a query's result, named by an explicit alias or derived from its
source text. It carries a **projection** describing what it projects — a binding
reference, a scalar literal, a non-aggregate function call, an aggregate, or a
rich scalar expression — plus, via the projection, its **result type**. A query's
result is an ordered, duplicate-preserving list of return items, or the `RETURN *`
wildcard over all in-scope bindings; it becomes a generated result.

**Projection**:
What a return item projects: a marker-sealed sum of a binding reference (`var` /
`var.prop`), a scalar literal, a non-aggregate function call, an aggregate, or a
rich scalar expression — the whole set of types declaring the `isProjection`
marker, which is narrower than the set satisfying the interface (AGENTS.md,
"Closed sum types"). It carries only the bindings the resolver must trace (the
`var` / `var.prop` refs), the projection's **result type**, and — for an
aggregate — the cardinality-bearing kind. It never carries the expression tree
(ADR 0003) nor a non-aggregate function's identity, which live below the
type-interface boundary (ADR 0005) and are re-executed from the original query
text. A `RETURN *` is not a projection but a query-level wildcard (see **Return
item**).
_Avoid_: expression (reserve for the grammar's `oC_Expression`; a projection is
the curated subset the model carries).

**Ref-valued-leaf certificate**:
A single additive bit on a rich projection (an expression projection or an
aggregate) asserting that its flat ref slice is not merely "the bindings the
expression touches" but is **leaf-exact and exhaustive**: every value at the
committed **type**'s `unknown` leaf is literally the value of one of the
projection's `var` / `var.prop` refs. Minted by the parser only for uniform
ref-tree shapes — `[p.id, p.age]`, `collect(p.id)`, `collect([p.id, p.age])`,
homogeneous-depth nested list literals — and never for folds, heterogeneous or
mixed-depth literals, or engine-dependent aggregates. The certificate is what
lets the resolver fill the unknown leaf from the schema without an expression
tree: it carries no structure (the list spine already lives in the committed
type), so ADR 0003's no-expression-tree line holds. Ruled in
`docs/specs/model-change-f45qn-ref-valued-leaves.md` (bead gqlc-f45qn); lands
with bead gqlc-t0bk.
_Avoid_: marker bit (the Stage-5 direction marker is a different axis);
inferring ref-valuedness from the flat ref set (falsified —
`collect(size(p.tags))` has the same ref shape as `collect(p.id)` and a
different answer).

**Type**:
The result type of a **projection**: a marker-sealed sum of `bool`, `int`,
`float`, `string`, `null`, `list<T>` (parameterised over an element type),
`map`, `node`, `edge`, the six **temporal types** (`date`, `time`, `localtime`,
`datetime`, `localdatetime`, `duration`), `path`, and a distinguished
`unknown` for types the parser cannot compute schema-free — the whole set of
types declaring the `isType` marker, which is narrower than the set satisfying
the interface (AGENTS.md, "Closed sum types"). It is the type
vocabulary the resolver reads from a parsed query: a
`RefProjection` on a whole entity types as `node` or `edge`; on a
**named path** as `path`; on a variable-length edge (see **Hop range**)
as `list<edge>`; a scalar literal types as its literal kind; a rich
expression carries the result of the parser's constant folding over the
scalar-expression grammar. `unknown` is the parser's honest posture on
the type-interface boundary (ADR 0005) for property lookups, function
calls, aggregates, and any expression touching a property or `null` —
the resolver upgrades these from the schema where it can reach one: a
bare `var.prop` ref, or the leaves of a projection carrying the
**ref-valued-leaf certificate**; every other rich shape stays honestly
unknown, permanently. Incremental: Stage 7 added
the six temporal types, Stage 8 added `path`; ADR 0008 records the sum
at seventeen as of Stage 14 completion.
_Avoid_: `any` (use `unknown` — the parser's "I cannot tell"); property
type (reserve for the schema-side scalar type `PropertyType`).

**Temporal type**:
One of the six openCypher temporal types the parser carries in the
**type** sum: `date`, `time`, `localtime`, `datetime`, `localdatetime`,
`duration`. The `time`/`localtime` and `datetime`/`localdatetime` pairs
are the **zoned vs. non-zoned** distinction the type interface preserves
so codegen can emit distinct Go binding signatures. The
parser types a **temporal constructor** call (`date(...)`, `time(...)`,
`duration.between(...)`, etc.) to its concrete temporal result type via
a closed name-based lookup; it also types **temporal arithmetic** under
a small closed rule table: `<temporal-point> ± duration → <temporal-point>`,
`duration ± duration → duration`, `duration × number → duration`
(commutative), `duration ÷ number → duration`. The reverse directions
(`duration - <temporal-point>`, `number ÷ duration`, `duration ÷ duration`)
are not committed and type as `unknown` — the honest posture the resolver
can upgrade. Temporal accessors (`d.year`, `d.timezone`, …) type as
`unknown` for the same reason: the accessor set is large and per-kind,
so the resolver types them from the schema.
_Avoid_: "datetime type" (colloquial for the whole family; the family is
"temporal type"); "duration accessor" (duration accessors like
`duration.between` are constructor calls in the type interface — the
namespaced function returns a `duration` — not property lookups).
The call/lookup distinction is load-bearing on the Apache AGE backend and
not only in prose: its dialect gate refuses `duration.between(...)` for
its **namespace**, since PostgreSQL reads `duration` as a schema
qualifier and answers `schema "duration" does not exist` (SQLSTATE 3F000,
naming no function at all), while `p.duration` is a property lookup that
calls nothing and is served. Both are pinned, and the gate reads the
grammar rather than the text, so `duration.` is never matched as a
prefix. ADR 0028 item 4.

**Neutral temporal carrier**:
One of the five gqlc-owned Go types (`Date`, `Time`, `LocalTime`,
`LocalDateTime`, `Duration`) emitted as `temporal.go` into a generated
package when its surface references one, plus `time.Time` for TIMESTAMP.
These are what the emitted public surface names for temporal widths —
never a driver type; conversion to `dbtype.*` or agtype encodings is
backend-private inside decode/encode helpers. Representation, placement,
and the AGE admission policy are ADR 0033. The five names are reserved
in every generated package, even one that emits no `temporal.go` — a
name that works in one batch but not another is the renaming scheme
`reservedIdentifiers` already refused (ADR 0033).
_Avoid_: "dbtype" in any description of the public surface (that is the
neo4j driver's package, a private carrier behind the conversion boundary);
"runtime type" (there is no gqlc runtime module — the carriers are emitted,
not imported).

**Emission question / carrier question / storage question**:
The three independent things asked about a **property type**, and the
reason three sentinels answer where one used to. Only the last two are
asked of a backend.

The emission question is "has gqlc built an emission for this KIND at
all?" and a `no` routes to `ErrUnimplementedTypeKind`. It is asked first,
of gqlc rather than of a target, and the answer is the same on every
target at once — today `no` for `RECORD` and the closed dynamic unions,
which the schema front end resolves and nothing renders (`gqlc-h9n.33`).
Asking it first is what stops a kind reaching a type table that has no
case for it and coming back as a missing carrier, which would name an
edit — change the declared width — that cannot help, because no width of
that kind is emitted anywhere. It is the one refusal of the three that a
future stage retires.

The carrier question is "is there a faithful Go type for this width?" and
a `no` routes to `ErrUnrepresentableWidth`. The storage question is "will
this backend's store hold a value of this width in a property?" and a
`no` routes to `ErrUnstorableProperty`. They are not the same question
and neither implies the other: neo4j has a faithful `[][]int16` for
`LIST<LIST<INT16>>` and emits a working decode for one arriving as a
query column, and the server still refuses to STORE it (ADR 0035).

The storage question is asked of **declared entity properties only**,
after the carrier question and never before it. A column and a parameter
are asked the emission and carrier questions alone, because neither
stores anything —
so a width may be refused as a property and admitted, in the same
package, as a projected column. That asymmetry is the whole content of
the distinction; a reader who collapses the two questions will read the
refusal as a missing Go type and look for a carrier that is already
there. Being one backend's storage rule rather than a fact about the
schema, an `ErrUnstorableProperty` names the backend that refused; the
width sentinel does not, reading the same wherever a carrier is missing.
_Avoid_: "unsupported type" / "unsupported width" (states none of the
three, and is false of the storage refusal — the type is supported, the
STORE is what declines); "invalid schema" (the same declaration is valid
on Apache AGE, which stores it); "unsupported width" for a record or a
union in particular (there is no width in question — it is the kind that
has no emission, and on every backend, so the phrase points at a
per-target answer that does not exist).

**Result type**:
The **type** a return item's **projection** commits to for the column that
becomes a generated method result field. Distinct from the schema-side
**property type**: a property type describes a stored value; a result type
describes a projected column, which may be a whole entity (`node`/`edge`),
a scalar (`bool`/`int`/…), a collection (`list<T>`/`map`), or `unknown`.
The result type is what codegen emits — not what the query author wrote,
which is the projection's structural discriminator instead.
_Avoid_: column type (colloquial; use "result type"); return type (used
generally for a method's return, ambiguous with the whole result row).

**Aggregate**:
A projection over an aggregating function (`count`, `sum`, `collect`, `min`,
`max`, `avg`, `stDev`/`stDevP`, `percentileCont`/`percentileDisc`). Distinguished
from an ordinary function call because it collapses matched rows into groups,
changing result cardinality — the one function distinction the model
carries. `count(*)` is the degenerate case: a count over rows that references
no binding. Also carries a **DISTINCT axis** (`count(DISTINCT x)`,
`collect(DISTINCT y)`, …) as a single-bit annotation: DISTINCT deduplicates
the aggregate's input before aggregation, so `count(DISTINCT a)` and
`count(a)` are observably-different queries and the model preserves the
distinction — a wire-emitted `distinct` field on `AggregateProjection`,
analogous to `EdgeBinding.directed`. Result type follows a per-aggregate
table computed against the operand's Stage-6 type: `count` types as `int`
unconditionally; `collect(T)` types as `list<T>` (never bare `unknown` —
the aggregate always yields a list, and `list<unknown>` is the honest
posture when the element type is unknown); `sum` and `min`/`max` commit
to a concrete numeric or scalar type when the operand's type commits,
else `unknown`; `avg`, `stDev`/`stDevP`, and `percentile*` stay
`unknown` (engine-dependent — a wrong concrete type is strictly worse
than an honest `unknown` the resolver upgrades from the schema).
Grouping-key semantics — which non-aggregate columns form the group —
are a resolver concern, entangled with `WITH`, and out of scope until then.

**Predicate expression**:
A boolean atom the openCypher grammar admits inside `WHERE` and the
right-hand side of any expression — the four list **quantifiers**
(`all(x IN xs WHERE p)`, `any`, `none`, `single`), the **existential
subquery** (`exists { ... }`), and the **pattern predicate**
(`(n)-->(m)` used as a boolean). Every predicate expression types as
`bool`; none introduces a new **type** or **projection** variant. The
model records only its result type — never its structure — so the
iteration variable of a quantifier (`x`) and every inner binding of an
`exists {}` are **scoped locally** to the predicate expression and do
not enter the enclosing **query part**'s bindings, refs, or
`byVar` / `pathBindings` / `unwindBindings`. The parser enforces the
scope structurally (a suppression counter around `exists {}`; a
discarded refs sink around a quantifier's filter body), not as a
name-collision rule, so a shadowing iteration variable is
accept-and-defer. Pattern predicates are legal only inside a boolean
position; using one as a scalar projection (`RETURN (n)-->()`) is a
bucket-1 parse-shape rejection via `ErrPatternInProjection`.
_Avoid_: "pattern comprehension" (that's the `[(n)-->() | n.name]`
list-yielding shape, not a predicate); "existential quantifier"
(reserve for the mathematical sense; openCypher's `exists {}` is
"existential subquery" and the four list quantifiers are the
"quantifier" family).

**Resolver**:
The stage that resolves a parsed query against the model, typing each return item
and parameter — a pure function of `(query.Query, schema.Schema[, procsig.Registry])`.
Lives in `internal/resolver` (a sibling of `query` and `schema`, importing both
plus `procsig`; none import it back). API pinned by ADR 0008:
`resolver.New(s schema.Schema, opts ...Option)` with `WithRegistry(procsig.Registry)`
binds the compile-time inputs; `(*Resolver).Resolve(q query.Query)
(ValidatedQuery, error)` resolves one query — no I/O, no state mutation.
Resolution that completes without error is what makes a query a
**validated query**; a query it cannot resolve is rejected.

**Validated query**:
A query the resolver has resolved against the model (`schema.Schema`) without
error — the trustworthy, schema-checked invariant. Materialised as the
`ValidatedQuery` type in `internal/resolver` (the resolver's output vocabulary,
not the parser's — ADR 0008). Resolution is a distinct stage
that runs after parsing; the parser is schema-agnostic and never produces a
validated query on its own. Once it passes, every query in the application is
valid or the application halts.

**Wrong-orientation drop**:
A relationship type a pattern names that the schema declares only with its two
ends the other way round, so endpoint narrowing drops it from that edge's
candidate set. `(:Person)-[r:AUTHORED|REPORTED]->(:Post)` against a schema
declaring `(:Post)-[:REPORTED]->(:Person)` is one: REPORTED cannot match the
arrow as drawn. The generated code is well-typed either way — no decoder arm is
emitted for the dropped type — but the query text still asks the server for it,
so the author matches less than they believe. Warned about, not refused
(ADR 0038), and only when the type survives on no other edge in the query; the
broader class of *any* declared-but-dropped type is ordinary narrowing (ADR 0022)
and stays silent. Distinct from the **undeclared relationship type** of ADR 0032,
which is declared nowhere at all.

## Generation language

**Config file**:
The hand-written YAML manifest (canonically `gqlc.yaml`) that declares a
project's **generation targets**: a format version and a `graph` list of
one or more targets, with nothing else at the document root. The file is
versioned (only version 1 exists); older versions keep loading when newer
ones appear. Relative paths are relative to the config file's directory,
not the invoking process's working directory.
_Avoid_: settings, manifest, project file (colloquial); config (bare — fine
informally, but the artifact is the **config file**).

**Generation target**:
One entry of the **config file**'s `graph` list: a schema file path, a
query directory, an optional procedure-signature registry path, the three
**tool axes**, and a `gen.go` block naming the generated package, its
**output directory** and its driver. One target produces one generated Go
package; a project needing several generated packages declares several
targets, which share nothing but the file they are declared in. The
loader accepts any number, and generation runs every one of them in
document order under a two-phase write: every target's output directory
is inspected before any target's is modified, so a diagnostic at any
target, or a tripwire abort at any target, ends the run with the tree
unchanged. Failures in the commit phase itself are the residue — the
multi-target spec §7.3 enumerates what each of them can leave behind.
Each axis is a closed vocabulary: the schema and query language axes
have one member today (GQL; openCypher), the driver axis has three
(the Neo4j Go v5 and v6 drivers, and Apache AGE over pgx v5). The axes
exist so each target states its whole pipeline explicitly, whether or
not an axis offers a choice. Every key except the procedure-signature
path is required — omission, an out-of-vocabulary axis value, or an
unsupported version is rejected with the valid choices spelled out. The
one cross-target rule is
that no two targets may share, or nest, an **output directory**; the
loader enforces it lexically, rejecting every overlapping pair the two
`out` strings decide between them, and config-file-format §4 names the
two classes they do not.
_Avoid_: target (unqualified — collides with the config file being
classified and with the output directory); output target (the output
directory's old name); typed repository (ADR 0010 — the generated
artifact one target produces, not the config entry).

**Query file**:
A source file holding one or more **named queries**, each introduced by an
annotation. Splitting a query file into its named queries — and naming
them — is the generation front end's job, outside the parser, which
consumes exactly one query per call (see **Query**).

**Named query**:
The unit of generation: one query plus what its annotation declares — its
name and its **cardinality**. The name becomes the generated
method's identity within the generated repository; a query file may hold
many named queries.
_Avoid_: statement (grammar sense), operation.

**Cardinality**:
The author-declared consumer-side row axis of a **named query** — how many
result rows the generated method hands its caller: `one` (exactly one
row), `many` (a list of rows), `exec` (no rows — a projection-less write).
An open axis: `iter` (streamed rows) is reserved. Declared in the query's
annotation, never inferred; a declaration that contradicts the query's
shape (`exec` on a column-producing query, `one`/`many` on a zero-column
write) is rejected outright.
_Avoid_: the edge-binding variable-length sense (use **hop range**);
"result cardinality" (runtime row multiplicity — the **Aggregate** /
**Union** sense, always qualified); command (sqlc's term — collides with
CLI vocabulary); arity.

**Output directory**:
The directory one **generation target**'s code is written to (the entry's
`gen.go.out` key), owned exclusively by gqlc: every file in it is
generated output carrying the generated-code marker (`Code generated by
gqlc … DO NOT EDIT.`), and each generation run replaces its contents
wholesale (ADR 0012). A file without the marker found there is an
error — never a deletion — so hand-written extensions live outside the
directory, wrapping the generated package rather than joining it. Because
the wipe is wholesale, two targets may not share an output directory or
nest one inside another. The loader enforces this lexically —
`filepath.Rel` both ways, no filesystem access — so it rejects every
overlapping pair the two `out` strings decide between them, and accepts
the two classes config-file-format §4 enumerates as undecidable without
the working directory (an absolute path against a relative one; an
escaping path that re-enters through the working directory's own name).
The two overlaps that slip through cost different things. Two targets
naming the **same** directory both generate into it: every file either
of them writes carries the marker, so the sweep has nothing to object to
and the run succeeds. The damage is in the contents. Where the two
packages use the same file name — `db.go`, `models.go`, `querier.go` —
the later target's version is the one that survives, so the earlier
target's package is never intact there; whether its *non*-colliding
files survive alternates from run to run, because phase A computes both
wipe lists before either commit runs. On the runs where they do survive,
the directory holds two `package` clauses and does not compile. Two
targets where one **nests** inside the other fail differently, and
worse. The first run into a clean tree writes both packages and clobbers
nothing; after that the parent's sweep finds the child's directory,
cannot prove gqlc wrote it, and aborts — `output directory … contains
entries not generated by gqlc: sub/`. That message says to move or
delete the entry, but the entry is the other target's output directory:
deleting it buys exactly one successful run, which recreates it and
restores the abort.
_Avoid_: gen dir (colloquial); output package (the generated Go
package's *name* — the entry's `gen.go.package` key, related but
distinct); output target (this directory's old name, retired with the
`output` key).

**Wire family**:
The equivalence class of declared widths that one backend's driver
delivers as a single indistinguishable wire shape. On neo4j every
integer width arrives as `int64` and every float as `float64`; on AGE
a DATE arrives as ISO text (sharing the string family with STRING)
and TIMESTAMP, LOCAL TIME and DURATION all arrive as integer
microsecond counts (sharing the integer family with the INT widths
and each other). A per-backend fact, never a property of the schema:
two widths in one family on AGE may be distinct families on neo4j.
Load-bearing for closed dynamic unions — a union is emittable on a
backend only where its members land in pairwise-distinct wire
families there, because decode has nothing but the wire shape to
dispatch on (`docs/specs/codegen-record-union-carriers.md` §4).
_Avoid_: wire type (suggests one width, not a class); carrier (the
emitted Go type — related, but a family can hold widths whose
carriers differ, e.g. `int32` and `int64` both riding neo4j's
`int64`).

**Transaction handle (Tx)**:
The object the generated repository's `Begin` returns: an open, always
write-mode transaction finished by exactly one `Commit` or `Rollback`.
The query methods are called on it directly — `tx.GetPerson(ctx, id)` —
because they are promoted from an unexported core the `Tx` embeds, which
the root handle embeds too (`docs/specs/codegen-tx-embedded-querier.md`).
Emitted per generated package with a byte-identical exported surface on
every backend; the driver appears only in unexported fields, so swapping
targets is an import swap. `Commit` after finish returns the package's
`ErrTxDone` sentinel; `Rollback` after finish returns nil, so a deferred
`Rollback` is unconditionally safe.
Distinct from **`WithTx`**, which binds a repository handle to a
transaction the *caller* opened and owns — `Begin`/`Tx` is gqlc opening
and owning one. Nesting is closed off in two different ways, and the
difference matters. `Begin` and `WithTx` are not in a `Tx`'s method set
at all, so `tx.Begin(ctx)` does not compile. On a handle that is
transaction-bound some *other* way — by `WithTx`, or on AGE by a
`pgx.Tx` handed straight to `New` — `Begin` is refused at runtime on
every backend, even where the driver could nest, because that boundness
is a dynamic-type fact the compiler cannot see.
_Avoid_: transaction wrapper (says closure, the rejected shape); managed
transaction (the neo4j driver's term for its retrying closure path,
which this is not).

## Flagged ambiguities

- **"Parsed"** splits into a syntactic step and a schema-checked invariant.
  Reserve **parsed** for "syntactically lowered into the query model" — what the
  Cypher parser produces, schema-free; use **validated** for "checked against the
  model and supported by it". A parsed query is not yet a validated query.
- **"Schema"** means three things: the GQL source construct (use **graph
  type**), the parsed Go model `schema.Schema` (use **the model**), and a
  **generation target**'s `schema:` key, which names the schema *file's
  path* (say **schema path**). Keep them distinct.
- **"Target"** unqualified is ambiguous three ways: the **generation
  target** (a `graph` entry), the config file `gqlc init` classifies, and
  the **output directory** (once "the output target"). Only the first
  keeps the word; say **config file** and **output directory** for the
  others.
- **"Name"** is overloaded across the explicit **type name**, the local
  **alias**, and the **label set**. "A node/edge must have a name" resolves to
  "must have a non-empty label set"; the explicit type name remains optional.
- **"Cardinality"** unqualified is the **named query** axis (one / many /
  exec). The edge binding's variable-length axis is **hop range** ("edge
  cardinality" only informally); runtime row multiplicity is always the
  qualified "result cardinality" (**Aggregate**, **Union**).
