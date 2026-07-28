# Undirected edges and LIKE graph sources are declined, for reasons that do not transfer

gqlc rejects undirected edges (`ErrUndirectedEdge`) and the `LIKE` graph type
source (`ErrLikeGraphSource`). Both are permanent, and neither is grounded in
"gqlc cannot represent this" — the reason each was originally given, and in both
cases the wrong one.

- **Undirected edges** are declined because GQL's data model makes them a
  *distinct element kind*, not a directed edge matched from either end, and the
  distinction is observable at runtime. This is a whole-language decision, not a
  graph-type-DDL one.
- **`LIKE`** is declined because `graphExpression` reaches session state that a
  static generator does not have. That reason applies to `LIKE` alone.

`COPY OF` shared `ErrUnsupportedSource` with `LIKE` and does *not* share the
reason: it names a catalogue entry, which is statically resolvable. The sentinel
is split so each rejection carries its own justification.

## Context

### The reason to avoid for undirected edges

The rejection read: *an undirected arc has no canonical source → target
identity, which `EdgeKey` requires.* That overstates the case, and a reviewer
can refute it from the grammar. Both undirected productions keep their endpoint
*types* in written order:

    edge type pattern undirected : sourceNodeTypeReference arcTypeUndirected
                                   destinationNodeTypeReference
    endpoint pair undirected     : ( sourceNodeTypeAlias connectorUndirected
                                     destinationNodeTypeAlias )

So a canonical stored direction plus symmetric matching is mechanically
available. Arguing unrepresentability invites exactly that reply, and loses.

### The reason that holds

An undirected edge is a separate kind of thing. *A Researcher's Digest of GQL*
(ICDT 2023; "We follow the formal definition adapted by the GQL Standard"),
Definition 2, gives a property graph as

    ⟨N, Ed, Eu, lab, endpoints, src, tgt, prop⟩

where `Eu` is a **separate** set of undirected edge ids, `src, tgt : Ed → N` are
defined only on the directed ones, and `endpoints : Eu → 2^N` with
`|endpoints(e)| ∈ {1,2}` is *unordered*.

The distinction is not internal bookkeeping — GQL exposes it:

- `<edge reference> IS [NOT] DIRECTED` (§19.8, `directedPredicatePart2`)
- `<node reference> IS [NOT] SOURCE OF <edge reference>` (§19.10), and the
  matching `IS DESTINATION OF`

An undirected edge stored with a canonical direction answers all three
**incorrectly**, not imprecisely. That is the difference between a lossy
encoding and a wrong one, and it is what makes the decision defensible.

Neo4j has no such element kind: every relationship has a `startNode` and an
`endNode`. So this is not a case where the ecosystem has settled on an answer
gqlc could adopt.

### The decision is language-wide, not DDL-only

Undirectedness reaches well past graph types. GQL distinguishes six edge pattern
directions, and the undirected ones are separate productions throughout:

| Spelling | Production | Includes undirected |
|---|---|---|
| `<-[]-` | `fullEdgePointingLeft` | no |
| `-[]->` | `fullEdgePointingRight` | no |
| `<-[]->` | `fullEdgeLeftOrRight` | no |
| `~[]~` | `fullEdgeUndirected` | yes |
| `<~[]~` / `~[]~>` | `fullEdgeLeftOrUndirected` / `fullEdgeUndirectedOrRight` | yes |
| `-[]-` | `fullEdgeAnyDirection` | yes |

The row that matters for future query support: **Cypher's `-[r]-` is GQL's
`<-[]->`, not `-[]-`.** A Cypher-shaped reading of `-[]-` would silently pull in
undirected matching. Recording the decision here, rather than in the graph-type
DDL alone, is what stops that.

### The Annex D citation, and how far it goes

**GH02, "Undirected edge patterns"** is the only undirected-related feature id
in the 228-entry Annex D list (verified against ISO's free XML artefact; see
`internal/schema/gql/annexd/SOURCE.md`). Declining an optional feature is
conformant, so citing it converts "gqlc declines something mandatory" into
"gqlc declines something optional".

**The citation is an inference and is labelled as such in the corpus entry.**
ISO's free artefact carries codes and descriptions only — no subclause map — and
the normative Annex D prose is paywalled (`gqlc-lir`). "Undirected edge
patterns" names `edge type pattern undirected` on its face, but could be read as
covering only the query-side `<edge pattern>` productions. What makes the narrow
reading implausible is that it would leave undirected edge *types* mandatory
while undirected edge *patterns* were optional — an implementation obliged to
accept the declaration but not to match it. That is not a coherent conformance
boundary, so GH02 is taken to gate undirectedness language-wide.

The corpus harness already records that a real code on the wrong construct is
the failure mode `isValidFeature` cannot catch (GE03 was cited on undirected
patterns before it existed). Hence the explicit hedge rather than a bare
citation.

**The decision does not rest on GH02.** If the code turns out to gate only query
patterns, the element-kind argument stands unchanged and the corpus entry
reverts to `"mandatory"` — the rejection stays, only its conformance claim
weakens.

### LIKE, and why COPY OF is not the same case

    graphTypeSource    : AS? copyOfGraphType | graphTypeLikeGraph
                       | AS? nestedGraphTypeSpecification
    graphTypeLikeGraph : LIKE graphExpression
    graphExpression    : graphReference | objectExpressionPrimary
                       | objectNameOrBindingVariable | currentGraph

`graphExpression` reaches `CURRENT_GRAPH` and binding variables. Those are
session state. gqlc runs at build time against files, with no session, so `LIKE`
is unresolvable in principle — no catalogue, no multi-file scoping, and no
amount of implementation changes that.

`COPY OF` takes a `graphTypeReference`, which is a catalogue path. It is
statically resolvable and merely unimplemented; `gqlc-h9n.1` is the bead that
would implement it, and eleven corpus entries name that bead.

Until now both reported `ErrUnsupportedSource`. A single sentinel carrying
"never possible" and "not yet built" cannot say which applies to a given error
value, which is precisely what a caller — or a deviation table — needs to know.

## Considered options

**Ground the undirected rejection in `EdgeKey`'s shape.** Rejected: it is
refutable from the grammar, as above, and it also frames a modelling decision as
an implementation limit, which invites "then change `EdgeKey`".

**Support undirected edges with a canonical stored direction.** Rejected: it
answers `IS DIRECTED`, `IS SOURCE OF` and `IS DESTINATION OF` wrongly. A wrong
answer to a standard predicate is a dialect, which is the thing this epic exists
to avoid.

**Model undirected edges properly, as a second element kind.** Not rejected on
merit — rejected on scope. It would need a separate `Eu` set through the schema
model, the resolver and codegen, and a driver story for stores that have no such
concept. GH02 makes declining it conformant, so the cost is not forced.

**Leave `ErrUnsupportedSource` as one sentinel and document both readings.**
Rejected: the deviation table has one row per sentinel, so one sentinel with two
justifications produces a row that is half wrong whichever way it is written.

**Remove `ErrUnsupportedSource` outright, exporting only the two leaves.**
Rejected: it is exported API, and a caller matching "the source was rejected"
would silently stop matching. Wrapping keeps that caller correct — the split
widens the surface instead of narrowing it (the same reasoning as ADR 0006 on
non-breaking change).

## Consequences

- `ErrUndirectedEdge`'s message becomes "undirected edges are a distinct element
  kind, which gqlc does not model". The corpus entry for
  `18.3-edge-type/pattern_undirected.gql` moves from `"mandatory"` to `"GH02"`
  and from bead `gqlc-h9n.3` to `gqlc-0ri`, the permanent-decline bead.
- `ErrUnsupportedSource` becomes an error *class*. `ErrLikeGraphSource` and
  `ErrCopyOfSource` wrap it, so `errors.Is(err, ErrUnsupportedSource)` still
  matches both. It is deliberately absent from `allSentinels`, which lists
  leaves; `TestGraphTypeSourceErrorsWrapTheClass` is the pin it gets instead.
- A `graphTypeSource` alternative added to the grammar later reports the bare
  class error. The guard still tests for the *supported* alternative, so such an
  alternative is rejected rather than silently dropped — which of the errors it
  gets only decides the wording.
- `12.6-graph-type-statement/like_graph.gql` moves from `"unsourced"` to
  `"GG04"`, *"Graph type like a graph"* — an exact match to `graphTypeLikeGraph`,
  verified against the same artefact.
- The invalid fixture `unsupported_source.gql` is renamed `like_graph_source.gql`
  for the construct it actually pins.
- Query-language support, if it lands, inherits the undirected decision:
  `~[]~`, `<~[]~`, `~[]~>` and `-[]-` are all out, and `-[]-` in particular must
  not be read as Cypher's `-[r]-`.
