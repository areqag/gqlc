# Questions held for the paid ISO text are settled as gqlc decisions

Three questions were held open for the semantics volume of ISO/IEC 39075: the
General Rules and Syntax Rules, which are in the paid PDF (CHF 227) and not in
the free BNF or Annex D. `gqlc-lir` declined that purchase on 2026-07-27, and
the three beads were then held, deferred, as "permanently blocked":

| Bead | GH | Question | Shipped behaviour |
|---|---|---|---|
| `gqlc-h9n.28` | #420 | Does `FLOAT(p)`'s precision count bits or decimal digits? | `FLOAT(p)` resolves to `Float` (ADR 0002, ADR 0017) |
| `gqlc-h9n.29` | #427 | Do properties on an inline endpoint filler declare or constrain? | rejected, `ErrEndpointFillerHasProperties` |
| `gqlc-tlbo` | #1305 | Is a repeated property name an error, or does one declaration win? | rejected, `ErrDuplicatePropertyName` (ADR 0030); fields likewise, `ErrDuplicateFieldName` (ADR 0039) |

The shipped behaviour in each case was labelled provisional or interim, pending
a reading nobody was going to obtain.

## The owner direction

On 2026-10-27 the repository owner ruled, verbatim:

> I am not paying for an ISO. we should do whatever we can with the free stuff
> that we got

So the paid text is not coming, and holding a shipped behaviour as
"provisional until the Syntax Rules are read" now means provisional forever.
This ADR settles all three as **gqlc's own decisions**. None of them is a
claim about what ISO/IEC 39075 says. Each rests only on the free BNF
(`ISO_IEC_39075(en).bnf.txt`, the artefact pinned by `isobnf.SourceSHA256`),
the vendored grammar, and the reasoning already recorded in the beads and ADRs
named below. Where the free artefacts are silent, this ADR says so rather than
filling the gap.

This changes no behaviour. It changes what the behaviour is called.

## What the free artefacts settle

For each question the free BNF shows the construct parses and does not show what
it means:

- `<approximate numeric type>` gives `FLOAT [ <left paren> <precision> [ <comma>
  <scale> ] <right paren> ]` as a sibling of `FLOAT16 … FLOAT256`, while its
  parenthetical has the precision-plus-optional-scale shape of `DECIMAL(p, s)`.
  It is consistent with both readings and decides neither (read 2026-07-26,
  recorded on `gqlc-h9n.28`).
- `<source node type reference>` and `<destination node type reference>` admit a
  full `<node type filler>` in their second alternative. Whether that filler
  declares a node type or refers to one is not in the BNF (read 2026-07-26,
  recorded on `gqlc-h9n.29`).
- `<property type list> ::= <property type> [ { <comma> <property type> }... ]`
  states no uniqueness constraint, and neither does `<field type list>`. The
  artefact carries no uniqueness prose (read 2026-08-22, recorded on
  `gqlc-tlbo` and in ADR 0030).

## Decision 1: `FLOAT(p)` resolves to `Float`, and its precision is dropped

gqlc does not fold `FLOAT(p)` onto a width. `FLOAT(p)` and `FLOAT(p, s)` resolve
to `Float`, the type bare `FLOAT` resolves to, and the parenthetical is dropped,
as the length and decimal-digit qualifiers on `VARCHAR(n)`, `DECIMAL(p, s)` and
the rest already are (ADR 0002). gqlc does not decide whether `p` counts bits
or digits, and nothing in gqlc depends on the answer.

The reason is the one `gqlc-h9n.28` already recorded: a fold needs a reading,
and the wrong reading narrows silently. Folding `FLOAT(16)` to `Float16` is right
if `p` counts bits and wrong if it counts digits, where a schema asking for 16
digits would get a type that holds about three. Not folding needs no reading.

**The limit, stated.** `Float` emits as Go `float64`. That holds at least what
the author asked for under either reading only while `p` is at most 53 under
the bits reading and at most 15 under the digits reading. Above that,
`FLOAT(p)` gets the same `float64` bare `FLOAT` gets, so it holds less than the
author asked for. That is not new with this decision; ADR 0002 and ADR 0017
shipped it. Note the contrast: an explicit `FLOAT128` or `FLOAT256` resolves to a
model type that both codegen targets refuse, while `FLOAT(113)` reaches
`float64`. Measured 2026-10-27 at `a9f625cd` in
`internal/codegen/neo4j/types.go` and `internal/codegen/age/types.go`. This ADR
records the limit and does not change it.

ADR 0017's semantic case on `scalar_float.gql` stays: the precision really is
lost, and a model that carried it would be the fix. This decision does not move
that case's owning bead. Whether a carried precision belongs with `gqlc-h9n.31`
(bit widths) or `gqlc-5md` (length, precision and scale) still depends on what
`p` counts, and gqlc has decided not to answer that.

## Decision 2: properties on an inline endpoint filler stay rejected

`(:Person)-[:AUTHORED]->(:Post { extra :: STRING })` is rejected with
`ErrEndpointFillerHasProperties`, permanently. `(:Post {})` with empty braces
stays accepted, and source and destination stay symmetric, as PR #422 and
`TestEndpointFillerEmptyBraceAllowed` already pin.

The reasoning was recorded when the sentinel landed, and it still holds:

- **Accept-and-discard is wrong under both readings.** The properties had no
  consumer and the model was byte-identical to the label-only spelling. That is
  the silent wrong model `gqlc-h9n.18` closed.
- **The declaration reading would add machinery nobody needs.** It needs merge
  semantics against the node type's standalone declaration, plus a rule for a
  conflicting redeclaration. No consumer asks for a second place to declare a
  node type's properties.
- **The constraint reading would need a new check.** It needs satisfaction
  checking against the referenced declaration, plus a new error. The
  standalone declaration already says everything that check would verify.
- **The cost of rejecting wrongly is loud and local.** If the standard does
  permit the construct, an author gets an error on the line, and can remove the
  braces or move the properties to the node type's declaration in one edit.

## Decision 3: a repeated property or field name stays rejected

`{ id :: INT NOT NULL, id :: STRING }` is rejected with
`ErrDuplicatePropertyName`, and `RECORD { a :: INT, a :: STRING }` with
`ErrDuplicateFieldName`, permanently. ADR 0030 gave the argument. A precedence
(first-wins or last-wins) can be wrong silently; a rejection fails on the line
and is fixed in one edit. ADR 0030 recorded the rejection as interim only
because a reading of the Syntax Rules might replace it. None is coming, so the
rejection is final.

## What this costs

Decisions 2 and 3 may reject text the standard accepts. ADR 0017 calls a
rejection of valid ISO GQL a dialect, and this is a narrow one, taken
knowingly: it covers two constructs whose meaning the free artefacts do not
fix, and both fail loudly with a sentinel that names the construct. The
alternative is to accept them under a meaning gqlc would have to invent. That
is also a dialect, and a silent one.

## What would reopen these

Each decision is reopened by evidence, not by time passing:

- **Someone obtains the General Rules or Syntax Rules** (the paid PDF, or a
  published corrigendum of it; `ISO/IEC 39075:2024/CD Cor 1` was at Committee
  Draft when `gqlc-h9n.28` was researched), and they speak to one of these
  questions:
  - If `FLOAT(p)` counts bits, a fold like the integer one becomes possible. It
    would still be a choice, made because a consumer wants the narrower carrier.
    If it counts digits, nothing changes.
  - If an endpoint filler's properties have a defined meaning, the rejection is
    reconsidered against that meaning. The guardrails on `gqlc-h9n.29` still
    apply: lifting it has to be a local change, keep the empty-brace pin, and
    keep the two sides symmetric.
  - If the rules define a precedence for a repeated name, the sentinel is
    deleted and the corpus entries flip to `resolves`, as ADR 0030's "What
    would change this" lays out. If the rules forbid a repeat or are silent,
    nothing changes.
- **A consumer needs the dropped `FLOAT` precision.** That reopens it through
  `gqlc-5md` or `gqlc-h9n.31`, on the consumer bar both already carry. It does
  not reopen the bits-or-digits question.

A web paraphrase, a vendor conformance table, or an analogy to SQL does not
reopen any of these. `gqlc-h9n.28` said so, and that still applies.

## Out of scope

`ErrEdgeKindArcMismatch` was held on the same Syntax Rules prose (ADR 0030
names it as such; `gqlc-xtq`). It is not one of the three beads this ADR
settles, and its doc comment still says to revisit it when the Syntax Rules are
available. The owner direction plausibly reaches it too. Settling it is left to
a decision of its own, not folded in here.

## Consequences

- `gqlc-h9n.28`, `gqlc-h9n.29` and `gqlc-tlbo` (GH #420, #427, #1305) resolve to
  this ADR.
- ADR 0002, 0014, 0017, 0018, 0030 and 0039 carry a note pointing here. They
  no longer describe these three questions as open or provisional.
- The doc comments on `ErrDuplicatePropertyName`, `ErrDuplicateFieldName` and
  the `FLOAT(p)` paragraph in `propertytype.go` state a decision instead of a
  pending one. `ErrEndpointFillerHasProperties` gains a comment naming this ADR.
- The two repeated-name corpus entries name `gqlc-0ri`, which `corpus_test.go`
  documents as the bead an unsupported entry names when it is declined
  permanently, in place of `gqlc-tlbo`. The ADR 0018 entries took the same step.
- No sentinel, resolution rule, corpus outcome or count changes.

## Provenance

Owner direction 2026-10-27; recorded under `gqlc-tlbo`. Filed as a numbered ADR
in `docs/adr/`, which holds decisions about the compiler.
