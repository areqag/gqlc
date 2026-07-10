# Stage 8 spec — Cypher query parser: full pattern surface

The implementation brief for Stage 8 of the Cypher implementation of
`query.Parser`. Eighth model evolution after Stage 7 per ADR 0004 (test-first,
evolving until feature-complete), under the curation discipline of ADR 0003 and
the type-interface boundary of ADR 0005. Stage 8 is the third stage of the
ADR-0007 before Stage 14 expansion beyond the read core. It **retires
`ErrUnsupportedPattern`** by widening the pattern model to carry the three
shapes the sentinel currently rejects: **named paths** (`p = ...`), **variable-
length relationships** (`[r*1..3]`, `[*]`), and **multi-type relationships**
(`[r:A|B]`). It adds `TypePath` to the `Type` sum and a `PathBinding` variant
to the `Binding` sum.

This document is a **delta** against Stages 0–7 (referenced individually where
relevant); everything not stated here carries over verbatim. Sections appear
here only where Stage 8 changes something.

Tracking: bead `gqlc-9nl` (GitHub #48). Lands as one graphite branch
(`stage-8-patterns`) with separated commits (spec → model red/green → parser
red/green → unlock+goldens), independently mergeable as a whole: `just test`
is green if this branch lands on `master` alone (AGENTS.md stacked-branch
invariant).

---

## 1. Deliverables

### 1.1 Type sum extension

One new variant joins the sealed `Type` sum on the `query` package: **`TypePath`**
— an empty struct, wire tag `"path"`. It mirrors the Stage-6 scalar variants
exactly: an empty struct, a `String() string` stringer that is the single
source of the wire tag, an `isType()` marker to keep the sum sealed, and a
per-variant `MarshalJSON` routing through the existing `marshalType` helper so
drift is impossible. There is no smart constructor — the variant is an empty
struct, same as `TypeNode` / `TypeEdge`.

Composition into `TypeList` works out of the box: `RETURN [p1, p2]` where
each is a named path types as `list<path>`. `TypePath` cannot appear inside a
map value's typing (maps stay `TypeMap`, heterogeneous by design), and there
is no `path`-flavoured accessor at the parser level — `nodes(p)`, `relationships(p)`,
`length(p)` type as `TypeUnknown` because they are namespaced or unary calls
outside the closed constructor lookup (§4 below), consistent with the
Stage-7 accessor posture.

### 1.2 Binding sum extension

The `Binding` sum grows to **three** variants: `NodeBinding`, `EdgeBinding`,
`PathBinding`. Stage-0..7 fixed the sum at two variants and had
`Kind() graph.EntityKind` on the interface (only Node/Edge exist in the graph
vocabulary — a **path is not a graph entity**, it is a query-level construct
composed of them). Stage 8 introduces:

- A query-side `BindingKind` enum with three values: `BindingNode`,
  `BindingEdge`, `BindingPath`. Its `String() string` is the single source of
  the JSON `"kind"` tag (`"node"`, `"edge"`, `"path"`) — same discipline as
  `AggregateFunc`, `UnionKind`, `graph.EntityKind`. Two of the three tags are
  identical to the graph.EntityKind stringer output, so the wire format is
  preserved for NodeBinding / EdgeBinding (§3).
- `Binding.Kind()` now returns `BindingKind`, not `graph.EntityKind`. The
  narrower type stays on the two entity variants via a new
  `EntityKind() graph.EntityKind` method exposed only on NodeBinding /
  EdgeBinding (the resolver reads it to form the schema `EdgeKey`; the parser
  never uses it). `PathBinding` has no `EntityKind` — a path is not a graph
  entity, so the schema-key formation does not apply.

The **path binding shape**: a named path variable and the ordered, shape-
faithful list of the members the path composes. Example: `MATCH p = (a)-[r]->(b)`
yields `PathBinding{variable: "p", members: [<node a>, <edge r>, <node b>]}` —
the same three node/edge bindings that would have been collected without the
`p =` prefix, plus a `PathBinding` capturing them in textual order. The
members are a **tagged sum** (`PathMember`), not a `[]string`: four variants
—  `NamedNodeMember{variable}`, `NamedEdgeMember{variable}`, `AnonEdgeMember{}`,
`AnonNodeMember{}` — so the anonymous slots never compete with any user-
chosen variable in the `byVar` namespace (a user pattern element may bind
a node named literally `__anon_edge_0`, and the collision would be silent
under a string-only member list). The list is **shape-faithful**: every
element the pattern chain composes appears in order (node, edge, node, edge,
… , node), so codegen later can reconstruct the path shape from
`Members()` alone without walking the pattern chain a second time.

Named members reference the part's entity bindings by variable — the path
binding does not co-own them (their bindings live in the part's `Bindings`
slice as always). Anonymous edges and anonymous intermediate nodes do not
name any binding: an anonymous edge is still its own binding in the part's
`Bindings` slice, but the path member carries only the fact "an anonymous
edge sits at this position"; an anonymous node inside a chain (e.g. the
`()` in `p = (a)-[]-()-[]-(b)`) is a pure filter (no binding is emitted for
it — §C3 stands) but the path member for it records "an anonymous node
sits at this position" so the shape is preserved.

Constructor invariants (illegal states unrepresentable):

- `NewPathBinding(variable string, members []PathMember)` rejects an empty
  `variable` (a path binding is never anonymous — a path with no name is
  just a pattern, no path binding is emitted). Rejects a `members` slice
  with fewer than one element (a path with no members is grammatically
  impossible: a pattern element always has at least one node pattern).
  Rejects a `nil` member entry (every member is one of the four variants
  above). Rejects a named member with an empty variable (`NewNamedNodeMember`
  / `NewNamedEdgeMember` themselves reject the empty case). Rejects a
  **same-name kind conflict**: openCypher lets the same variable appear
  multiple times in a pattern (`MATCH p = (n)-->(k)<--(n)` is legal — the
  triangle revisits `n`), so a same-kind repeat of a named member is
  legal, but two named members that share a variable and disagree on
  `Kind()` (one `NamedNodeMember{"x"}` and one `NamedEdgeMember{"x"}`)
  would collide with the part's byVar — `mergeBinding` catches this at
  the pattern level, and the constructor enforces it defensively so the
  illegal state is unrepresentable at the model boundary too. The
  anonymous variants may repeat freely (they carry no name).
- No `Nullable` on a path binding at Stage 8: the two `MATCH p = (a)`
  scenarios inside `OPTIONAL MATCH` (Path1 [1], Match7 [23..25]) parse
  green because the path binding's kind is `TypePath` — the resolver
  reads member nullability from the member bindings themselves. Adding
  a `Nullable` flag would duplicate that per-member fact; kept explicit
  in the spec as a scope decision, not an omission.

### 1.3 Edge binding extension

Two changes to `EdgeBinding` for the variable-length and multi-type shapes:

- **Multi-type relationships** — `EdgeBinding.Labels()` is already a
  `graph.LabelSet` (an ordered slice), so it can carry N types with no
  representation change. What changes is the constructor: today `relTypes()`
  in `pattern.go` returns `ok=false` when more than one type appears
  (`ErrUnsupportedPattern` fires). Stage 8 removes that gate — a multi-type
  edge collects every type in textual first-appearance order, so
  `[r:A|B|C]` yields `Labels: ["A", "B", "C"]`. The resolver forms one
  candidate `EdgeKey` per type (per Stage-5 undirected orientation-trial
  logic — the resolver's cross-product logic already exists for the
  undirected-orientation axis). The model carries the "widened admissible
  type set" fact via the existing `LabelSet`; no new field.
- **Variable-length relationships** — a new `hops` field on `EdgeBinding`
  captures the range. Its type is `*EdgeHops` (nil for a single-hop
  edge — the Stage-0..7 case; a non-nil pointer to a struct for var-length).
  The pointer captures the presence/absence structurally: illegal states
  (a nil hops with a non-single edge or vice versa) are unrepresentable
  because the collector sets the pointer iff the grammar carried the
  range literal.

  `EdgeHops` carries `min *int` and `max *int`, both optional (unbounded on
  either end):

  ```
  [r*]        → EdgeHops{min: nil, max: nil}
  [r*3]       → EdgeHops{min: 3, max: 3}
  [r*1..3]    → EdgeHops{min: 1, max: 3}
  [r*..5]     → EdgeHops{min: nil, max: 5}
  [r*3..]     → EdgeHops{min: 3, max: nil}
  ```

  Constructor `NewEdgeHops(min, max *int) (EdgeHops, error)` rejects:
  a negative `min` or `max` (openCypher integer literals are non-negative,
  so a negative value could never come from a well-formed range literal —
  this is the sole invariant the type alone cannot express).

  An empty range (`max < min`, e.g. `[*2..1]`) is **accepted**: the openCypher
  TCK includes it as a positive scenario returning zero rows
  (`clauses/match/Match5.feature` [11], [12]), so the runtime rule "no
  valid hop count satisfies the range" sits below the type-interface
  boundary (ADR 0005). The parser records the range as written; the engine
  interprets the empty result. A zero lower bound (`*0..N`) is likewise
  accepted for the same reason.

- **Var-length edges' binding cardinality changes.** A var-length edge
  variable binds to a **list of edges** at runtime, not a single edge —
  the crucial model question. Codegen later needs to know a
  `RETURN r` on a var-length `r` emits a list-of-edge column, not an
  edge column. The model records this via `refType` in the parser
  (`internal/query/cypher/expr.go`): when a `RefProjection`'s ref names
  an EdgeBinding whose `Hops()` is non-nil, the projection type is
  **`TypeList(TypeEdge)`**, not `TypeEdge`. The **`EdgeBinding` itself**
  carries the `hops` field; the **projection's `Type()`** is computed from
  it at classify time. No new "list-of-edge binding" variant — the edge
  binding is still one binding, its cardinality axis is a fact on the
  binding, and the type it projects to reflects that fact.

  Rationale: a var-length edge is still fundamentally an edge binding
  (it has a source, a target, a type set, a name); it is a **different
  cardinality** than a single edge. Adding a new binding variant for it
  would triplicate the resolver's edge-binding logic. Adding the cardinality
  axis to `EdgeBinding` mirrors the Stage-5 direction axis — a fact about
  the binding the type interface reads.

### 1.4 Direction and multi-type interaction with var-length

The Stage-5 `directed` flag continues to mean the same thing: `true` for
one-arrow, `false` for undirected. A var-length undirected edge
(`(a)-[*]-(b)`) is `directed=false, hops=<hops>`; a var-length directed
edge (`(a)-[*]->(b)`) is `directed=true, hops=<hops>`. The resolver's
orientation-trial logic (Stage 5) composes with the hop range: for an
undirected var-length, the resolver tries both orientations at every hop
(dialect-specific, ADR 0003 boundary); the parser records the two facts
independently and does not model the combined orientation-trial policy.

Multi-type edges compose with both direction and var-length the same way:
`[r:A|B*1..3]` yields `Labels: ["A", "B"], hops: {1, 3}, directed: <arrow>`.

### 1.5 Parser widening — three rejections retired

Three current fail-sites in the listener/pattern collector are retired:

1. `collectPattern` — the `part.OC_Variable() != nil` branch that rejected
   named paths. Stage 8 collects the named-path variable, walks the pattern
   element normally (so member bindings enter the part's binding list), and
   emits a `PathBinding` capturing the variable and every member name.

2. `EnterOC_RangeLiteral` — the whole-listener rejection of variable-length
   relationships. Stage 8 removes the `EnterOC_RangeLiteral` handler and
   parses the range literal at the point where the edge is collected
   (`collectEdge`), reading the `OC_RangeLiteral` from the relationship
   detail and constructing an `EdgeHops` value.

3. `collectEdge` — the `ok=false` return from `relTypes` for multi-type
   relationships. Stage 8's `relTypes` returns every type in textual order
   (no rejection); `collectEdge` proceeds normally.

### 1.6 Sentinel status — ErrUnsupportedPattern retirement

All three rejections above were the fail-sites for `ErrUnsupportedPattern`.
After Stage 8, **no code path in the parser fires `ErrUnsupportedPattern`**.
Per ADR 0007 §7 the sentinel is retired the same way `ErrUnsupportedProjection`
was retired at Stage 6: the sentinel is **deleted** from `errors.go` and from
the canonical `allSentinels` / `unsupportedSentinels` sets.

The four remaining sentinels are:
- `ErrUnsupportedClause` (retired by Stages 12–14: writes, `UNWIND`, `CALL`)
- `ErrUnsupportedParameter` (retired incrementally as the expression surface widens)
- `ErrUnboundVariable` (real rejection, unaffected)
- `ErrVariableKindConflict` (real rejection, unaffected — extended to cover
  a path binding kind conflicting with a node or edge binding of the same
  variable name)

`TestSentinelReachability` runs against the four-sentinel set; the
`"multi-type relationship"` `mustReject` case is deleted from `parser_test.go`
(it now parses under the reversed rejection); no new `mustReject` case for
Stage 8 (nothing added rejects; the sentinel gap is a scope decision, not
an omission).

### 1.7 Layer-1 corpus

`readCoreDirs` gains three dirs:
- `expressions/path` — 7 scenarios across 3 files (`nodes()`, `relationships()`,
  `length()` on paths). Each is `MATCH p = ... RETURN <fn>(p)`.
- `expressions/pattern` — 36 scenarios across 2 files (`Pattern1` — pattern
  predicates in WHERE; `Pattern2` — pattern comprehensions). Every scenario
  is `MATCH (n) WHERE (n)-[...]->(...)  RETURN ...`, so the pattern predicate
  atom's typing is unchanged (Stage-6 §9 catches it as `TypeUnknown`).
- `expressions/graph` — 48 scenarios across 9 files (`Graph1` .. `Graph9`).
  Graph functions: `id`, `labels`, `type`, `keys`, `nodes`, `relationships`,
  `length`, `properties`. All are function invocations, `TypeUnknown` at
  the parser level (Stage 6 posture).

Additionally the **existing wired dirs** contain scenarios that were
PENDING via the retired sentinel. The unlock commit removes those from the
skiplist and lets them go green:

- `clauses/match/Match6.feature` — 25 named-path scenarios (all now parse).
- `clauses/match/Match7.feature` — the OPTIONAL-MATCH scenarios that use
  `p = (a)-->(b)` or `OPTIONAL MATCH (a)-[*]->(b)` (a subset of Match7's 31
  scenarios).
- `clauses/match/Match2.feature` — 12+ scenarios exercising `[*]` and named
  paths in the "Match errors" feature file.
- `clauses/match/Match9.feature` — 9 scenarios exercising deprecated
  var-length shapes.
- Selected scenarios from `clauses/match-where`, `clauses/with`,
  `clauses/union`, `clauses/return-orderby`, `clauses/return-skip-limit`
  that use one of the three retired shapes.

Baseline suite before Stage 8: **2399 scenarios / 1941 passed / 458 pending /
0 failed**. Stage 8 adds the three new dirs and reverses the retired-shape
scenarios in existing dirs. **Zero FAIL is mandatory**; the exact passed /
pending flip counts are pinned by the unlock commit's `-update` run and
recorded in the bead.

### 1.8 Skiplist

Every skiplist entry from Stages 0..7 stays unless the scenario now parses
green. The unlock commit sweeps the skiplist for entries that were pinned to
the retired shapes:

- Any entry named "…named path…" or "…relationship variable in named path…"
  where the fail-site was `ErrUnsupportedPattern` is removed.
- Any entry named "…variable length…" or "…relationship pattern range…"
  where the fail-site was `ErrUnsupportedPattern` is removed.

Every remaining skiplist entry keeps its Stage-6-conformant "// Stage-X
comment naming which future stage would absorb it" if that stage's arrival
is what would flip it. Where an entry stays because the scenario asserts a
runtime/value-level rule below the type-interface boundary (bucket 3), the
comment says so explicitly.

The three new dirs sit under `expressions/`, so their bucket-3
runtime-error scenarios ride the categorical `isBucketThreeDir` accept-and-
defer per Stage 6 §7 without per-name skiplist entries — same posture as
Stage 6 and Stage 7. If a bucket-3 detail token surfaces that isn't in the
existing `isBucketThreeError` whitelist (candidates from a scan of the three
new dirs: `PatternExpressionInScalarPosition`, `InvalidRelationshipPattern`),
the whitelist gains it with a comment naming the runtime rule it labels.

### 1.9 Layer-2 pins

New `mustParse` cases for the Stage-8 shapes:

- **Named path — projected as PATH**: `MATCH p = (a)-[r]->(b) RETURN p` →
  the part carries three bindings (a NodeBinding a, an EdgeBinding r, a
  NodeBinding b) **plus** a PathBinding p whose members are the tagged
  sum `[NamedNode(a), NamedEdge(r), NamedNode(b)]`; the return item is a
  RefProjection carrying `Ref{Variable: "p"}` and `Type: TypePath{}`.
- **Named path — anonymous edge**: `MATCH p = (a)-[]-(b) RETURN p` →
  the anonymous edge surfaces as an `AnonEdgeMember{}` in the members
  slice, not as a named member with any synthetic string (see §1.2).
- **Named path — anonymous intermediate node**:
  `MATCH p = (a)-[]-()-[]-(b) RETURN p` → the anonymous middle node
  surfaces as an `AnonNodeMember{}`, so the members slice has five
  entries `[NamedNode(a), AnonEdge, AnonNode, AnonEdge, NamedNode(b)]`
  — shape-faithful.
- **Named path — user pattern with a `__anon_edge_0` identifier**:
  `MATCH p = (__anon_edge_0)-[]-(b) RETURN p` → the user's node binding
  `__anon_edge_0` occupies the byVar namespace normally; the anonymous
  edge member sits alongside it as an `AnonEdgeMember{}`, and the two
  never collide. Pins the collision resistance the tagged sum buys.
- **Var-length edge — bare `[*]`**: `MATCH (a)-[r*]->(b) RETURN r` →
  the edge binding carries `hops: {nil, nil}`; the return item's type is
  `TypeList(TypeEdge)`.
- **Var-length edge — bounded**: `MATCH (a)-[r*1..3]->(b) RETURN r` →
  the edge binding carries `hops: {1, 3}`; the return item's type is
  `TypeList(TypeEdge)`.
- **Var-length edge — undirected**: `MATCH (a)-[r*]-(b) RETURN r` →
  the edge binding carries `directed: false, hops: {nil, nil}`; the
  return item's type is `TypeList(TypeEdge)`.
- **Var-length edge — anonymous**: `MATCH (a)-[*]->(b) RETURN a, b` →
  the anonymous edge binding carries `hops: {nil, nil}, directed: true`;
  no RETURN references the edge, so no list-of-edge type appears.
- **Multi-type edge**: `MATCH (a)-[r:KNOWS|LOVES]->(b) RETURN r` → the
  edge binding carries `Labels: ["KNOWS", "LOVES"]`; the return item's
  type is `TypeEdge`.
- **Named path over var-length**: `MATCH p = (a)-[*1..3]->(b) RETURN p`
  → the anonymous var-length edge gets a synthetic member name; the
  path binding references [a, __anon_edge_0, b] and the return type is
  `TypePath{}`.

No `mustReject` case is added for Stage 8. The retired `"multi-type
relationship"` reject is deleted (it now parses).

### 1.10 Docs inline

- This spec.
- ADR 0003's amendment notes gain a Stage-8 line ("the curated Type sum
  now carries `TypePath`; the Binding sum grows a `PathBinding` variant;
  the EdgeBinding carries an optional hop range for variable-length
  relationships and admits a multi-type LabelSet").
- ADR 0007 already declared `ErrUnsupportedPattern` retirement in Stage 8;
  no ADR text change needed.
- CONTEXT.md gets a `PathBinding` entry, a `TypePath` entry, and a note
  on the edge cardinality axis.

Nothing downstream of the parser is built (no resolver, no codegen) —
ADR 0004. The resolver's use of the new pattern shapes (forming candidate
`EdgeKey`s for multi-type edges, computing per-hop endpoint types for
var-length, unifying path member types) is resolver work and out of scope;
the existing gqlc-lqm-style follow-up bead covers it or a new one is filed.

---

## 2. Why one atomic cycle

Adding one variant to the `Type` sum, one to the `Binding` sum, an optional
hops field on `EdgeBinding`, teaching the parser three pattern widenings,
retiring one sentinel, and wiring three new dirs is one restructure of the
parser's pattern model. Splitting the sum changes from the parser change
would leave the new model types unused (dead code) on one branch;
splitting the parser change from the corpus wiring would leave the
acceptance suite in a mid-migration state where the three new dirs and
the retired-shape scenarios in existing dirs have no goldens. Neither
split lands independently on `master` (Stage 4 §1's argument in miniature),
so Stage 8 lands as one branch.

Within the branch, the commit inventory (§7) separates spec from model
from parser from corpus wiring so review can proceed incrementally.

---

## 3. Model shape

### 3.1 BindingKind

```
type BindingKind int

const (
    BindingNode BindingKind = iota
    BindingEdge
    BindingPath
)

func (k BindingKind) String() string { ... }  // "node"/"edge"/"path"
```

`Kind()` on the `Binding` interface returns `BindingKind`. `NodeBinding`
gains an `EntityKind() graph.EntityKind` that returns `graph.Node`;
`EdgeBinding` returns `graph.Edge`. `PathBinding` has no `EntityKind`
(a path is not a graph entity).

Wire encoding for NodeBinding / EdgeBinding is preserved: `BindingKind.String()`
matches `graph.EntityKind.String()` for the two shared values. PathBinding
adds `"path"` to the discriminator vocabulary.

### 3.2 PathBinding and PathMember

```
type PathMember interface {
    Kind() BindingKind  // BindingNode / BindingEdge — a path member is one or the other
    Variable() string   // the named-member variable; empty for the anonymous variants
    Anonymous() bool    // true iff this is an AnonEdgeMember / AnonNodeMember
    isPathMember()
}

type NamedNodeMember struct { variable string }  // non-empty by constructor
type NamedEdgeMember struct { variable string }  // non-empty by constructor
type AnonEdgeMember  struct {}                   // positional slot only
type AnonNodeMember  struct {}                   // positional slot only

func NewNamedNodeMember(variable string) (NamedNodeMember, error)
func NewNamedEdgeMember(variable string) (NamedEdgeMember, error)
// AnonEdgeMember / AnonNodeMember are empty structs — no constructor.

type PathBinding struct {
    variable string        // the path variable name (non-empty)
    members  []PathMember  // the members in shape-faithful textual order
}

func NewPathBinding(variable string, members []PathMember) (PathBinding, error)
func (b PathBinding) Variable() string
func (b PathBinding) Members() []PathMember
func (b PathBinding) Kind() BindingKind
func (b PathBinding) Nullable() bool  // always false at Stage 8
```

Wire encoding for a PathBinding:

```
{"kind":"path","variable":"p","members":[
  {"kind":"node","variable":"a"},
  {"kind":"edge","variable":"r"},
  {"kind":"node","variable":"b"}
],"nullable":false}
```

Wire encoding for the anonymous variants uses distinct discriminators so a
consumer never confuses an anonymous slot with a named member of an empty
variable — the two shapes are:

```
{"kind":"anon-edge"}
{"kind":"anon-node"}
```

### 3.3 EdgeHops

```
type EdgeHops struct {
    min *int  // lower bound, nil for unbounded
    max *int  // upper bound, nil for unbounded
}

func NewEdgeHops(min, max *int) (EdgeHops, error)
func (h EdgeHops) Min() *int
func (h EdgeHops) Max() *int
```

Rejects negative bounds and `max < min` when both non-nil. Both nil is the
`[r*]` case (fully unbounded).

Wire encoding on an EdgeBinding: `"hops":{"min":1,"max":3}` for a bounded
range; `"hops":{"min":null,"max":null}` for `[*]`; `"hops":null` for a
single-hop edge (the Stages 0..7 case). Every EdgeBinding gains a `hops`
key regardless of variant, per the always-emit convention `nullable` /
`directed` / `returnsAll` follow: this is a **wire-shape change** — the
key is always present, so every pre-Stage-8 golden gains `"hops":null` at
regeneration. It is not a keyed-back-compat change (no consumer that
reads the pre-Stage-8 shape sees the same JSON keys); recorded honestly
here rather than described as "wire-compat".

### 3.4 EdgeBinding widening

`EdgeBinding` gains a `hops *EdgeHops` field. `NewEdgeBinding` /
`NewNullableEdgeBinding` get a new variant `NewVarLengthEdgeBinding` /
`NewNullableVarLengthEdgeBinding` that additionally takes an `EdgeHops`:

```
func NewEdgeBinding(variable, labels, source, target, directed) (EdgeBinding, error)          // single-hop
func NewVarLengthEdgeBinding(variable, labels, source, target, directed, hops) (EdgeBinding, error)
func NewNullableEdgeBinding(...) (EdgeBinding, error)
func NewNullableVarLengthEdgeBinding(...) (EdgeBinding, error)
```

Accessor: `func (b EdgeBinding) Hops() *EdgeHops`. Nil for single-hop, non-nil
for var-length. The parser's ref-typing (`refType` in `expr.go`) reads
`Hops() != nil` to project a `TypeList(TypeEdge)` instead of `TypeEdge`.

The four-variant constructor set is verbose but keeps illegal states
unrepresentable: a "kind of edge binding" cannot silently drop the hops or
nullable info. The `must` test helper masks the verbosity in tests.

### 3.5 Labels — multi-type

No representation change. `Labels()` is already `graph.LabelSet` (ordered
slice); a multi-type edge carries every type in textual first-appearance
order. `relTypes()` in the listener stops rejecting the >1 case. The
grammar's legacy alternation form `[r:A|:B]` (an obsolete-but-accepted
spelling of `[r:A|B]`) resolves the same way — every `oC_RelTypeName`
child contributes its name to the ordered set, so `A|:B` and `A|B`
produce the same `LabelSet ["A","B"]`. A repeated type in the source
(`[r:A|A]`) dedups via `rawBinding.mergeLabels`'s ordered union, so the
LabelSet is `["A"]`; the source spelling is not preserved (the model
records the admissible type set, not the textual alternation form).

---

## 4. Var-length edge type projection

The one subtle rule is at `refType()` in `internal/query/cypher/expr.go`:

- A `Ref{Variable: v}` (whole entity) where `v` names a NodeBinding →
  `TypeNode` (unchanged).
- A `Ref{Variable: v}` (whole entity) where `v` names an EdgeBinding
  whose `Hops() == nil` → `TypeEdge` (unchanged).
- A `Ref{Variable: v}` (whole entity) where `v` names an EdgeBinding
  whose `Hops() != nil` → **`TypeList(TypeEdge)`** (Stage 8 new).
- A `Ref{Variable: v}` (whole entity) where `v` names a PathBinding →
  **`TypePath`** (Stage 8 new).
- A property lookup `Ref{Variable: v, Property: p}` → `TypeUnknown`
  (unchanged; schema-owned per ADR 0003).

The imported map (`curPart.imported`) carries the type through WITH the
same way — the classifier reads the type from `imported[name]` when the
name comes from a prior WITH.

---

## 5. Corpus and bucket-3 whitelist

The three new dirs are `expressions/path` (7 scenarios), `expressions/pattern`
(36 scenarios), `expressions/graph` (48 scenarios): ~91 scenarios total.

- **`expressions/path`** — Uses `MATCH p = (a)-[r]->(b) RETURN nodes(p)` or
  variants. The path binding is emitted, `nodes()` types as `TypeUnknown`
  (function identity below the boundary, ADR 0005). Every positive
  scenario parses green.
- **`expressions/pattern`** — Uses `MATCH (n) WHERE (n)-[]->() RETURN n`.
  The pattern predicate atom types as `TypeUnknown` (Stage 6, `typeAtom`'s
  `OC_PatternPredicate` arm). Every positive scenario parses green.
- **`expressions/graph`** — Uses `MATCH (n) RETURN id(n)` or similar.
  Function invocations type as `TypeUnknown` (Stage 6 posture); every
  positive scenario parses green.

Scenarios asserting a runtime error (a `TypeError`, an `ArgumentError` for
`nodes(<non-path>)`, a `SemanticError` on a pattern predicate misuse) ride
the bucket-3 categorical accept — `isBucketThreeDir` matches
`/features/expressions/`, so the harness reports PENDING when the query
parses and the scenario expected a rejection with an error whose kind /
detail is bucket-3-eligible.

Layer-2 rule (Stage 1 §6). Stage 8 adds the `mustParse` cases §1.9 names.
The `"multi-type relationship"` `mustReject` case is deleted.
`TestSentinelReachability` runs against the four-sentinel set.

---

## 6. Definition of done for Stage 8

1. `stage-8-patterns` lands green and independently mergeable; `master` is
   green if it lands solo.
2. `just test` green: query-package unit tests (new `TypePath`, new
   `PathBinding`, new `EdgeHops`, extended `EdgeBinding`), the cypher-
   package listener tests, the `mustParse` pins, the acceptance / orphan /
   reachability suites, the property tests.
3. Layer-1 godog count rises by the three new dirs' scenarios (~91)
   **plus** the retired-sentinel-shape scenarios in the existing wired dirs
   that flip from PENDING to passed. Zero FAIL is mandatory. Success
   metric: every scenario whose only Stage-8 need was the three retired
   pattern shapes now flips PASSING or is bucket-3 accepted with a
   runtime-detail whitelist match; the rest stay PENDING via
   `ErrUnsupportedClause` (writes / `UNWIND` / `CALL`) or
   `ErrUnsupportedParameter`.
4. Documentation: this spec; CONTEXT.md entries; ADR 0003 note.
5. Beads: `gqlc-9nl` closed.

---

## 7. Commit inventory (single branch `stage-8-patterns`)

| Commit | Scope |
|--------|-------|
| prep / spec | this spec |
| model (red) | Failing unit tests for TypePath, BindingKind, PathBinding, EdgeHops, EdgeBinding.Hops(), var-length constructors |
| model (green) | TypePath variant added; BindingKind introduced; PathBinding added to Binding sum; EdgeHops added; EdgeBinding gains Hops field + var-length constructors |
| parser (red) | Failing `mustParse` cases for named path, var-length (three variants), multi-type, mixed shapes |
| parser (green) | `collectPattern` accepts named paths; `collectEdge` accepts multi-type and var-length; `EnterOC_RangeLiteral` handler removed; `relTypes` widened; `refType` types var-length edges as list-of-edge; `ErrUnsupportedPattern` deleted |
| unlock (dir + whitelist + goldens) | `readCoreDirs` gains expressions/{path,pattern,graph}; skiplist entries for retired shapes swept; new goldens for parse-green scenarios; bucket-3 whitelist grows if the corpus surfaces new runtime detail tokens |

Each commit is green in isolation of the ones after it — the model
commits leave TypePath/PathBinding unreferenced by the parser until the
parser commits use them; the parser commits leave the new dirs unwired
until the unlock commit.

---

## 8. Weakest point (recorded honestly per ADR 0004)

**Two weakest points, both recorded.**

**Path member representation.** The choice to store `PathBinding.members`
as a tagged sum (`[]PathMember` with named-node / named-edge / anon-edge /
anon-node variants) rather than `[]Binding` (values) makes the path
binding a **shape + name projection** into the part's binding list.
Resolving a named path member goes through the same `byVar` map the
resolver already uses; anonymous members carry no name at all, so the
`byVar` namespace cannot be silently invaded by a synthetic anonymous
identifier (an earlier design that used strings including a
`__anon_edge_<index>` prefix collided with the openCypher symbolic-name
grammar — `oC_SymbolicName` accepts `[A-Za-z_][A-Za-z0-9_]*`, so
`__anon_edge_0` is a legal bare identifier a user could write; the
tagged sum makes the collision unrepresentable).

The trade-off: `PathBinding` cannot be resolved standalone (you need
the part to look up named members). But the alternatives are worse:
`[]Binding` would co-own the members (two owners of the same binding,
feature-complete concern); `[]int` (indices into `Part.Bindings`) would couple
the path to a slice position that reordering a printer might change; a
flat `[]string` would collide as described. The tagged sum is the
minimal representation that carries what codegen needs (shape + named
identity for the resolver-visible members).

Shape-faithfulness: the members slice is emitted for every element the
pattern chain composes, in textual order. An anonymous intermediate
node (`p = (a)-[]-()-[]-(b)`) surfaces as `AnonNodeMember{}` at its
position, not as a dropped element. Codegen reading `Members()` can
reconstruct the whole path shape (5 members for the example above)
without walking the pattern chain a second time.

**Var-length edge is one binding with a cardinality axis.** The choice
to fold the "single edge vs list-of-edges" distinction into
`EdgeBinding.Hops()` rather than a new binding variant means the sum
stays at three variants, and every consumer of `EdgeBinding` (including
the resolver later) reads `Hops()` to branch. The alternative
(a `VarLengthEdgeBinding` variant) would double the resolver's edge-
binding logic to reproduce every read (source, target, direction,
labels) on a second type. The cardinality axis on the existing binding
mirrors the Stage-5 direction axis and the Stage-6/7 type-widening
posture: one fact, one field, read where relevant.

The lesser risks, recorded for completeness:

- **`PathBinding.Kind()` returns a query-side `BindingKind`, not
  `graph.EntityKind`.** Any external consumer (there are none today,
  but there will be at codegen time) that expected the graph-vocabulary
  return type now sees a query-side one. The wire format is unchanged
  for the two shared values ("node"/"edge"); "path" is new. Adding a
  consumer that mishandles a path binding is a codegen concern the
  Stage 14 will target.
- **Zero-hops (`*0..N`) parses.** The openCypher grammar accepts a
  zero-lower-bound range literal; the runtime engine interprets zero
  hops as "the source node itself." The parser accepts the shape and
  records `min: 0`; the engine's interpretation is below the boundary
  (ADR 0005).
- **Multi-type edge `LabelSet` order is textual first-appearance.**
  The resolver forms one candidate `EdgeKey` per type; the order of the
  types on the wire is not semantically meaningful (`[r:A|B]` and
  `[r:B|A]` match the same edges). Recording textual order is honest
  (the source said `A|B`, not `B|A`) and deterministic; a canonicalised
  sorted order would be a lossy widening Stage 14 does not need.
