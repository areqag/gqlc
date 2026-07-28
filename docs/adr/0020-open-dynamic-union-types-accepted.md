# ANY VALUE and ANY PROPERTY VALUE are accepted and emit Go's any

`ANY VALUE` (valueType alt 7, `openDynamicUnionType`) and `ANY? PROPERTY VALUE`
(alt 8, `dynamicPropertyValueType`) both resolve. The closed dynamic unions and
`RECORD` are unchanged.

## Context

ADR 0019 split `ErrUnsupportedType` into five named family sentinels and filed
the open dynamic unions under `ErrDynamicUnionType` with a "yet" in the message
— "ANY VALUE and the closed unions need a property type that carries
alternatives, which this model does not have yet." `gqlc-h9n.34` is the bead
that revisits it, having established that the open unions need only a decision,
not a model change.

The decision has two parts:

**What `ANY VALUE` and `ANY PROPERTY VALUE` mean when used as a property type.**
`ANY VALUE` is ISO GQL's open dynamic union of all value types, including paths
and element references, which §1–2 of ADR 0019 classified as non-storable.
`ANY PROPERTY VALUE` is narrower: by construction, the union of property value
types only — the storable ones. The difference is precise in the standard's type
theory but invisible in practice, because no graph backend can store a path or an
element reference as a property value. A property typed `ANY VALUE` will, at
runtime, hold only what the store admits.

**The owner's principle.** "If it is valid ISO GQL, we should allow it." Both
spellings are grammatically valid and semantically defined as property value
types in ISO/IEC 39075:2024. Declining them would require a justification
symmetric with the permanent declines in ADR 0019 (ADR 0016 for undirected
edges, etc.); no such justification exists here.

## Decision

Both `ANY VALUE` (and bare `ANY`, which is `openDynamicUnionType` with `VALUE`
elided) and `ANY? PROPERTY VALUE` resolve to `graph.TypeAnyPropertyValue`, a new
constant on the flat `graph.PropertyType` enum. Codegen emits Go's `any`.

The four accepted spellings — `ANY VALUE`, `ANY`, `PROPERTY VALUE`,
`ANY PROPERTY VALUE` — are all spelled the same in the model, because they
denote the same domain: "this property may hold any storable value." A codegen
consumer that needs to distinguish them cannot: the model has no field for the
distinction, and ADR 0002's precedent of losing qualifiers that add no model
field applies.

`ErrDynamicUnionType` survives for the closed unions (`ANY VALUE<A|B>`, bare
`A|B`, alts 9 and 10), which need the enum to carry members. ADR 0019's
argument against splitting the sentinel on gqlc's internals — the ISO taxonomy
should name the error, not the implementation detail — stands; the sentinel now
covers only the closed family, and it keeps its name because that name is the
ISO production it declines.

## Consequences

- `graph.TypeAnyPropertyValue` (`"ANY"`) is the new constant.
- `declineValueType` no longer intercepts `OpenDynamicUnionTypeLabelContext`
  and `DynamicPropertyValueTypeLabelContext`; they fall through to
  `normaliseType` via four `typeSpellings` rows.
- `goType` emits `"any"` for `TypeAnyPropertyValue`.
- Three corpus entries (constructed\_dyn\_open.gql, constructed\_dyn\_property\_value.gql,
  constructed\_dyn\_any\_bare.gql) move from `unsupported` to `resolves`.
- `declinedCarriers[ErrDynamicUnionType]` loses `valueType#7`, `valueType#8`
  and `VALUE` (now covered by resolving files). `declinedCarriers[ErrUnsupportedType]`
  loses `ANY` (same reason).
- The four `dynamic*` entries in `isoGaps` are updated. The two open-family
  entries note the construct is implemented while the ISO production name
  remains absent from GQL.g4 (labelled alternatives, not named rules — the same
  structural fact as `<standard digit>` and `<double double quote>`).
- `wantCorpusResolving` rises from 61 to 64.
- `isoGapRatchet` is unchanged at 14: the four ISO productions remain absent by
  name from GQL.g4.
