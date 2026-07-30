# ADR 0024 — Label implication: GG21 is accepted, one sub-case is declined

**Status:** Declined (partial — see below)
**Date:** 2026-07-29
**Bead:** gqlc-0ri

## Context

GQL optional feature **GG21, "Explicit element type key label sets"** introduces
the `=>` form:

    CREATE NODE TYPE Person => :LegalEntity { ... }

The phrase before `=>` is the key label set — the type's identity. The phrase
after it is *implied content*: labels and property types whose elements carry
them in addition to the key labels. ISO 39075 §12 and the productions
`node type key label set` / `node type implied content` (both in the normative
BNF artefact at `internal/schema/gql/isobnf/`) define this.

gqlc **accepts** GG21 in full except for one syntactically-decidable sub-case:
an implied label that is also a key label of some other declared element type.
`ErrImpliedLabelIsKeyLabel` is the sentinel for that sub-case. Everything else
— a key label set that differs from the implied label set, inline property
types declared after `=>`, empty implied label sets — resolves without error.

The companion sentinel `ErrEndpointFillerImpliesLabels` applies when an inline
edge endpoint filler carries a `=>` clause. An inline endpoint names a node
type by its key label set; an implied-label clause there asserts something about
the referenced declaration that nothing in the endpoint resolution can check.

## Why the sub-case is declined

Given:

    CREATE PROPERTY GRAPH TYPE T AS {
      (:Person { name :: STRING }),
      (:Engineer => :Person)
    }

`Engineer` implies `:Person`, which is also the key label of the `Person` type.
The question is whether `Engineer` elements inherit `Person`'s `name` property.
Two shipping implementations answer differently:

- **Microsoft Fabric** inherits: `Engineer` carries `name`.
- **Neo4j** (GA 2026.06) forbids the schema outright.

Whether ISO/IEC 39075:2024 mandates the inheritance is unresolved — the
normative Syntax Rules are in the paid PDF, which `gqlc-lir` declined to buy,
and Fabric's own conformance table marks GG21 **No** while its documentation
uses `=>`. The disagreement is reachable only at this syntactically-decidable
condition; everywhere else the two vendors agree, and that is the subset gqlc
accepts.

This follows the same posture as `ErrEdgeKindArcMismatch`: when two vendor
interpretations of a construct are observably incompatible, gqlc rejects the
construct rather than silently pick one vendor's answer and call it the
standard. ADR 0015 argues the full case.

## Considered options

**Implement Fabric's inheritance.** Rejected. Generates struct fields for
property inheritance that user code reads, which makes the decision
irreversible: removing a field the user has built against is breaking. The
cheaper mistake of the two.

**Implement Neo4j's semantics wholesale (reject any cross-label implied
reference).** Considered under gqlc-h9n.15 and rejected as under-specified.
"No property inheritance" produces the same observable output as rejecting the
schema for every construct gqlc can compile, but the mechanism is different and
the diagnostic is absent: nothing declared is silently dropped without an error,
which is gqlc's convention since `gqlc-h9n.18`.

**Accept the sub-case, delay inheritance.** Rejected. A schema gqlc accepts
and generates typed repositories for implicitly promises those repositories are
correct. Accepting `(:Engineer => :Person)` while generating no inheritance
produces a Go type that is incomplete against the standard's own description of
the schema element. An explicit `ErrImpliedLabelIsKeyLabel` is the honest
outcome.

**Decline GG21 entirely.** Not chosen. The conformant subset — implied labels
that do not collide with any declared key label — is well-defined and
unambiguous, and it exercises the key/complete-label-set distinction that the
model already has fields for (ADR 0015). Declining the feature outright would
leave `Schema.NodeType.CompleteLabels` permanently equal to `KeyLabels` for
every reachable schema.

## Consequences

- `ErrImpliedLabelIsKeyLabel` is the permanent-decline sentinel for the
  cross-label case. The corpus entries pointing to it name bead `gqlc-0ri`.
- If the normative prose settles the inheritance question, `rejectInheritance`
  in `resolve.go` is the one place to widen — rejections become acceptances,
  which is non-breaking whichever way it resolves.
- `ErrEndpointFillerImpliesLabels` is a separate sentinel for the inline-
  endpoint case and has its own corpus entry. It is also a permanent decline
  under the no-dialect principle.
- ADR 0015 has the full argument and the divergence table.
