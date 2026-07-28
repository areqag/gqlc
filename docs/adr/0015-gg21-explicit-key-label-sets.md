# GG21 explicit key label sets are implemented, minus the inheritance question

gqlc accepts the `=>` form of an element type declaration — optional feature
**GG21, "Explicit element type key label sets"**. The phrase before `=>` is the
key label set and becomes the type's identity; the label set after it is implied
content and joins the complete label set. Property types declared after `=>`
belong to the type itself.

One case is refused: an implied label that is also some declared type's key
label, `ErrImpliedLabelIsKeyLabel`. That is the single point where the two
shipping implementations of this syntax disagree about what it means, and
choosing either answer would make gqlc a dialect of one of them.

The explicitly empty key label set, `(=> :Thing)`, is refused as
`ErrUnnamedNodeType` / `ErrUnnamedEdgeType`.

## Context

- `nodeTypeKeyLabelSet : labelSetPhrase? IMPLIES` (`GQL.g4:1533-1535`), with an
  identical `edgeTypeKeyLabelSet` at 1576-1578. `node type key label set` and
  `node type implied content` are both real productions in ISO's own BNF
  artefact (`internal/schema/gql/isobnf/productions.go`), so the vocabulary is
  the standard's, not a rendering artefact.
- Before this, `rejectLabelImplication` failed on any `IMPLIES` token. The
  rejection was undocumented, and its own comment called the accepted `:Label`
  form "the plain key label set" — inverting the standard, where that form is
  *implied content* whose key label set is inferred (optional feature **GG22**).
- Declining GG21 was legitimate under §24.2 minimum conformance. It was
  nevertheless the only thing making `gqlc-h9n.8`'s key/complete split inert:
  every type gqlc could build had an inferred key label set equal to its
  complete one, so no fixture could distinguish the two fields and the
  divergence had to be hand-built in Go to be tested at all.
- The implied content is entirely **inline** — `nodeTypeImpliedContent` is a
  label set and/or a property types specification written in the same
  declaration (`GQL.g4:1526-1530`). There is no syntax for referring to another
  declared type. So interpreting `=>` requires no cross-declaration reading.

## The divergence

Two implementations, same syntax, incompatible semantics:

- **Microsoft Fabric** — secondary-label properties "are automatically
  inherited". Given `(:Person {name STRING})` beside `(:Engineer => :Person)`,
  Engineer carries `name`.
- **Neo4j** (GA 2026.06) — *identifying label* plus *implied labels*, with **no**
  property inheritance, and a label may not be both identifying and implied.
  The same schema is rejected outright.

Whether ISO/IEC 39075:2024 mandates the inheritance could not be settled from
public sources. The normative prose is in the paid PDF, which `gqlc-lir`
declined to buy. Fabric's own conformance table marks GG21 **No** while its
graph-types article uses `=>` and warns the syntax "isn't currently supported
directly for graph" — the syntax is the standard's, unimplemented there.

Note the disagreement is reachable only when an implied label is also a declared
type's key label. Nowhere else does one declaration's implied content refer to
another declaration at all, so nowhere else is there anything to inherit. That
makes the divergence a **syntactically decidable condition**, not a pervasive
semantic fog — which is what lets the rest of the feature land.

## Considered options

**Decline GG21 and record the rejection.** Rejected. It is conformant, and it
was the status quo, but the epic's goal is that valid ISO GQL is accepted, and
`=>` is the standard's syntax for a concept the model now has a field for. It
would also leave `gqlc-h9n.8`'s split permanently unexercised by any real
schema.

**Implement GG21 with Fabric's inheritance.** Rejected. It picks a vendor's
answer to an open question and calls it the standard, which is the dialect this
epic exists to prevent. It is also the more expensive mistake of the two: a
schema that inherits properties generates struct fields, and removing a field
that user code already reads is breaking. Widening a rejection later is not.

**Implement GG21 with Neo4j's semantics wholesale.** Rejected as
under-specified rather than wrong. "No property inheritance" is the same
observable behaviour as rejecting the schema for every query gqlc can compile,
but it silently discards the author's `:Person` where gqlc's convention
(`gqlc-h9n.18`, `ErrEndpointFillerHasProperties`) is that nothing declared is
dropped without a diagnostic.

**Implement everything the two vendors agree on, reject the rest.** Chosen. The
agreed subset is every declaration whose implied labels are implied only. There,
`=>` means exactly one thing: this type is identified by these labels and its
elements carry those as well. Where the vendors disagree, gqlc says so and names
the label. This is the move `ErrEdgeKindArcMismatch` already makes for the same
reason under the no-dialect principle.

**Accept `(=> :Thing)` by re-inferring the key label set from the implied
content.** Rejected. That is GG22's inference, and it applies precisely when no
key label set was declared. Here one was declared and it is empty; re-inferring
contradicts what the author wrote. An empty key label set is also an empty
identity, which nothing can reference and which collides with every other empty
one, so `Schema.Nodes` has nothing to key it on. It joins the existing
"unlabelled element type" rejection rather than getting a sentinel of its own —
same condition, same reason.

## Consequences

- `ErrLabelImplication` is gone. `ErrImpliedLabelIsKeyLabel` and
  `ErrEndpointFillerImpliesLabels` replace it, both pinned by corpus files.
- Inline edge endpoints written with `=>` name their node type by its **key**
  label set, since `EdgeKey.Source`/`.Target` hold identities. An endpoint that
  also implies labels is rejected: it asserts something about the referenced
  declaration that nothing checks.
- `test/data/schema/gql/valid/key_label_set.gql` is the first fixture whose
  golden shows `key_labels` differing from `complete_labels`.
- The GG22 invariant test was narrowed to the fixtures without a `=>`, and
  `TestExplicitKeyLabelSetsDiverge` was added so "narrowed" cannot decay into
  "narrowed until it passed".
- Property inheritance is **not** implemented, and this ADR is the record of the
  standard's position being unresolved. If the normative prose later settles it,
  the change is to widen `rejectInheritance` — accepting schemas previously
  rejected, which is non-breaking either way it resolves.
