# The neo4j backend refuses a nested list as a stored property

A schema declaring `matrix :: LIST<LIST<FLOAT32>>` on a node type now fails
generation for the neo4j targets:

    unstorable property width: entity "Reading" property "matrix" has
    LIST<LIST<FLOAT32>>, which the neo4j backend cannot store as a property

The same declaration still generates for Apache AGE. Before this, both
backends generated, and the neo4j write could never succeed.

Written 2026-08-30 by Արամազդ, executing the design ruled by Արթուր on bead
`gqlc-v0gk` and amended by him on `gqlc-nxcj9` after two measurements below
falsified parts of the original mechanism.

## The two measurements

Against the pinned images, 2026-08-29 and 2026-08-30:

- **neo4j refuses the write.** `CREATE (:NestedListProbe {xss: [[1],[2,3]]})`
  comes back with *"Collections containing collections can not be stored in
  properties"*. The rule is the server's, not the driver's, which is why one
  arm (v5) witnesses it rather than one per driver version.
- **AGE performs it.** The same property is created, matched back, and
  compares equal to `[[1],[2,3]]`. agtype is JSON-shaped and nests without a
  documented limit.

Both are live rows, not recollections:
`TestNeo4jRefusesANestedListStoredProperty` and
`TestAGEStoresANestedListProperty` in
`test/data/codegen/live_nested_list_property_test.go`. The first is the
premise's tripwire — if an image bump ever serves the write, it reds and the
town re-opens the question with evidence rather than with this document.

## The subtlety that shaped the whole design: storage, not values

neo4j *serves* nested lists perfectly well as query results. `RETURN [[1],
[2,3]] AS xss` works, `collect(collect(...))` works, and gqlc already emits a
working recursive decode for both — the `list_list_int` fixture's live neo4j
row passes. Only the stored-property case is refused.

So the refusal cannot be spelled as "gqlc has no way to handle this width on
neo4j". It has one, and exercises it.

### Rejected: refusing in `TypeMap.Property`

The original design routed this through `Property()`, the existing width
channel, returning `("", false)` for a list whose element is a list. That was
withdrawn. `Property` answers the **carrier** question — *is there a faithful
Go type for this width?* — and for `LIST<LIST<INT16>>` on neo4j the honest
answer is `[][]int16`. Refusing there puts a carrier-absence claim on a width
whose carrier exists and is exercised, and sends the author looking for a
Go-type gap that is not there.

## The decision: a storage axis beside the carrier axis

`TypeMap` gains a second question:

```go
Property(pt graph.PropertyType) (goType string, ok bool)  // carrier
StorableProperty(pt graph.PropertyType) bool              // storage
```

with its own sentinel, `codegen.ErrUnstorableProperty` ("unstorable property
width"), beside `ErrUnrepresentableWidth`. neo4j's implementation is the whole
rule:

```go
func (typeMap) StorableProperty(pt graph.PropertyType) bool {
	return !(pt.Kind() == graph.KindList && pt.Elem().Kind() == graph.KindList)
}
```

AGE's returns `true`.

`Elem()` strips the `NOT NULL` suffix, so `LIST<LIST<INT16> NOT NULL>` is
caught the same as `LIST<LIST<FLOAT32>>`; a depth-3 list is caught at its outer
level, its element being a list; `LIST<LIST<ANY VALUE>>` is caught for the same
reason.

The method is **required** rather than an optional interface a backend may
omit. An optional one defaults silently to storable, so a backend that never
implemented it would be indistinguishable from one whose store holds
everything. Required, the compiler names every implementation that still owes
an answer.

## Where the question is asked, and where it is not

At exactly one call site: `prepare.go`'s entity sweep, immediately after the
carrier question and only if it succeeded.

```go
ty, ok := tm.Property(p.Type)
if !ok { ...ErrUnrepresentableWidth... }
if !tm.StorableProperty(p.Type) { ...ErrUnstorableProperty... }
```

The carrier question comes first because a width with no Go carrier has a
carrier problem whether or not the store would hold it, and reporting the
storage sentinel there would send the author to the wrong axis.

Query columns and query parameters are **not** asked. They are read and bound,
never stored, so a storage rule has nothing to say about them and asking there
would refuse values the backend serves. The consequence is a deliberate
asymmetry: a width may be refused as a property and admitted, in the same
package, as a projected column.

The falsifier for over-refusal is the `list_list_int` fixture, whose nested
list is a query literal and whose schema declares no nested property: all three
targets must regenerate byte-identically. They do — measured with a tripwire
appended to a golden first, to confirm the targets were actually rewritten
rather than skipped, since an empty diff from a generator that did not run
proves nothing.

## The suffix names the backend, and only on this sentinel

`neo4j/generate.go` appends ", which the neo4j backend cannot store as a
property" to `ErrUnstorableProperty` alone. The refusal is that package's type
table answering rather than a property of the schema — AGE stores the same
declaration — so a run emitting several targets has to say which of them
refused.

Width refusals are returned unwrapped. The original design attributed those
instead, and it was measured false: `ErrUnrepresentableWidth` is raised by
three sweeps — entity, query-column, query-parameter — and on the latter two
the sentence would lie. A projected `INT128` column is not a stored property,
and it is refused for want of a carrier. Three conformance rows in
`assembled_input_test.go` read those exact strings back and went red on the
changed text, which is how the claim was falsified rather than argued.

## The emitter asymmetry, and one arm that is now unreachable

neo4j has **two** recursive nested-list decoders, in different emitters,
binding different local families. Neither witnesses the other:

| | emitter | locals | reached by |
|---|---|---|---|
| property path | `render_models.go` `writeSliceNarrow` | `elem<n>`/`i<n>`/`nested<n>`/`acc<n>`/`v<n>` | a declared nested-list property |
| query-column path | `render_queries.go` `walkListElemPlan` | `inner<n>`/`innerAcc<n>` | a nested list arriving as a query value |

Both callers of `writeSliceNarrow` pass an `EntityField`'s `GoType`, so its
`if isSliceType(elem)` recursive branch fires only for a declared nested-list
property — the declaration this ADR refuses. That arm is therefore **dead but
present** on this backend: unreachable by construction, not by accident. It is
kept here and deleted by bead `gqlc-52w8l`, which updates this sentence.

This is worth stating because the obvious repair is wrong. Re-pointing the
property path's tests at query columns would not relocate the same coverage —
it would probe the other emitter and leave the property path's arm both
unreachable and unguarded.

What the neo4j decode corpus lost with the refused declarations, named here
rather than left as silence: the nullable nested-column arm, and ordering
evidence for a depth-2 parameter bind declared before a depth-1 one over the
same carrier — the emitter keeps one list depth per carrier and must keep the
deepest, and with every remaining bind at depth 1 nothing now distinguishes
keeping the deepest from keeping the last. The corpus files say so at each site
rather than closing over the gap.

A **third** arm falls the same way, and it was not anticipated by the design.
`render_temporal.go`'s depth≥2 encode helpers (`from<X>List2` and up) are
reached from one call site, `sliceParamBindExpr`, whose Go type comes from a
query parameter — and a parameter's type is derived from the property it is
compared against, which this ADR now refuses at that width. `render_temporal.go`
is a neo4j file with no AGE counterpart, so there is no other backend reaching
it either. Measured: no golden in the corpus emits a `from<X>List<n>` helper for
any n. Those helpers are therefore unreachable by construction, exactly as
`writeSliceNarrow`'s recursive arm is, and for the same reason. They are kept
untouched here — bead `gqlc-tlc3e` carries the question of whether they should
go the way `gqlc-52w8l` takes the other arm, and it is deliberately not settled
in this PR.

## Considered and rejected

**Document the limitation and generate anyway.** This leaves in place exactly
the runtime write failure gqlc exists to move to generation time. The author
learns about it from a server error in production.

**Encode the value — JSON string, or flattening with a shape sidecar.** Both
make the stored graph unreadable to plain Cypher and to every other client: a
gqlc-private storage dialect in a database the author did not agree to stop
sharing. The sidecar variant additionally has nowhere to put the shape, which
is the same nowhere ADR 0033 and AGE's zoned-list refusal already document.

**A backend-local sweep in `generate.go` with a new sentinel, ADR 0027's
shape.** ADR 0027 takes that shape for a rule whose arity does not split per
property — a multi-label schema is refused whole. Here the arity *does* split:
one property is refused and its siblings are fine. That is exactly the case
ADR 0027 says belongs in the type-table channel.

## A named limit, not chased

`LIST<ANY VALUE>` is admitted, and a value of that width can carry a nested
list at runtime. No static check can see it. That write fails at the server
exactly as it does today.

This is named rather than fixed because the fix is a runtime check on every
list element written, on a width whose entire purpose is to be unconstrained.
The declaration is honest about admitting anything; the server is the one that
declines, and the author who chose `LIST<ANY VALUE>` asked for that trade.

## Consequences

- Four `valid/` fixtures declaring nested-list properties were trimmed, and the
  shapes they carried moved to a new AGE-only fixture `nested_list_property`,
  so the AGE nested-stored-property emission keeps its coverage. Measured: the
  set of `agtypeListOfListOf*` helpers across every AGE golden is identical
  before and after.
- A new invalid fixture `unstorable_property_nested_list` (neo4j v5 and v6)
  pins the sentinel and the message.
- `gqlc-415l`'s depth-3 live decoder scenario is unaffected and stays live: its
  scenario is query-shaped, and this refusal is property-scoped. Its fixture
  must simply not declare a nested-list property for a neo4j target.
