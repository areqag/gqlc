# Empty edge endpoint `()` names the empty-label node type; ErrUnknownEndpoint stays

An edge endpoint spelled `()` — `nodeTypeFiller?` elided — resolves under
reading 1: it names the node type whose key label set is empty. That type is
undeclarable (ADR 0018, `ErrUnnamedNodeType`), so `resolve()` cannot find it and
correctly returns `ErrUnknownEndpoint`. No code change is needed.

## Context

`sourceNodeTypeReference` and `destinationNodeTypeReference` each have two
alternatives (GQL.g4:1618-1626):

    sourceNodeTypeReference
        : LEFT_PAREN sourceNodeTypeAlias RIGHT_PAREN       // 1 — named alias
        | LEFT_PAREN nodeTypeFiller? RIGHT_PAREN           // 2 — inline or bare ()
        ;

When `nodeTypeFiller?` is elided, alternative 2 produces the bare `()`. The
question is what `()` means. Two readings are possible:

**Reading 1.** `()` names the node type whose key label set is empty. The
`nodeTypeFiller` rule specifies a node type by its key label set and implied
content; when the entire filler is absent, the specification is empty, which
means the empty label set. `resolve()` takes `ref.labels.Key()` on the empty set,
gets the empty key, and does not find it in `idx.types` — correctly, because ADR
0018 makes every such declaration reject with `ErrUnnamedNodeType`.

**Reading 2.** `()` is an unconstrained endpoint: "match any node type." This
reading treats the optional filler as a wildcard signal rather than as the absent
filler of a specific (empty-labelled) type.

The decline is not in question under either reading. Schema.Edges is keyed on a
source/label/target triple with no wildcard for either end, so reading 2 would
also be declined; and the empty-label node type of reading 1 cannot be declared.
Only the sentinel and message turn on the choice.

## Decision

**Adopt reading 1.** The grammar gives the clearest available signal:
`nodeTypeFiller` *specifies* a node type — it is what you write to say which
type the endpoint names. Writing it as optional does not mean "any type if
omitted"; it means the specification may be absent, in which case no labels are
specified, which is the empty label set. Reading 2 would require the optional
filler to carry a wildcard meaning it does not grammatically have.

The Syntax Rules (in the paid ISO/IEC 39075 PDF that `gqlc-lir` declined) might
confirm or refine this. If they establish reading 2 as correct, a new sentinel
(`ErrEmptyEndpoint` or similar) would be needed then; the grammar-consistent
reading 1 is the best available position without them, and ADR 0018 item 4 sets
the precedent for pinning a grammar-backed reading while the Syntax Rules are
unavailable.

`ErrUnknownEndpoint` — "edge endpoint references an undeclared node type" — is
accurate under reading 1: the empty-label node type is genuinely undeclared (and
undeclarable). The message is weak in not distinguishing *undeclarable* from
merely *undeclared*, but that is a quality-of-life gap rather than a wrong
report.

## Consequences

- No changes to `resolve.go` or `errors.go`.
- `18.3-edge-type/endpoint_no_filler.gql` and its corpus entry update to note
  that reading 1 is now the stated position (ADR 0021), removing the `gqlc-h9n.35`
  deferral.
- `gqlc-h9n.35` closes.
