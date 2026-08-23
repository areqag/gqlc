# A repeated property name is rejected, because the free artefacts cannot decide a precedence

`{ id :: INT NOT NULL, id :: STRING }` is rejected with
`ErrDuplicatePropertyName`. It previously resolved, to one property carrying
only the second declaration's type and nullability, with no diagnostic.

This is a **provisional** deviation, in the same class as
`ErrEdgeKindArcMismatch`: the question is semantic, the semantic clauses are
paywalled, and rejection is the reading that cannot be wrong silently.
`gqlc-tlbo` is the bead that revisits it.

## What the free artefacts settle, and what they do not

The question was put to the normative text rather than to taste, per
`iso-gql-conformance-no-dialect`: gqlc implements ISO/IEC 39075, not a gqlc
dialect, so "error or precedence?" is a question about the standard.

ISO publishes the complete GQL BNF free of charge. Fetched 2026-08-22 from
`https://standards.iso.org/iso-iec/39075/ed-1/en/ISO_IEC_39075(en).bnf.txt`,
SHA-256 `d1b56017ee38ee29e1d05655ee16c9113e1b020e9bc038c7ab5fcc0bc41d6ac3` —
byte-identical to `isobnf.SourceSHA256`, so this is the same artefact the
coverage gate is already built on rather than a second rendering of it. The
relevant chain is three productions:

    <property types specification> ::= <left brace> [ <property type list> ] <right brace>
    <property type list>           ::= <property type> [ { <comma> <property type> }... ]
    <property type>                ::= <property name> [ <typed> ] <property value type>

No uniqueness constraint appears anywhere in that chain, and the artefact
carries no uniqueness prose at all — the only `UNIQUE` in 3502 lines is a
keyword in an unrelated production. `<field type list>`, the binding-table
analogue, is spelled identically and is equally silent.

So the BNF settles that the construct **parses**, and settles nothing else.
Whether a conforming implementation must reject a repeat is Syntax Rules prose,
which lives in the paid PDF `gqlc-lir` declined to buy at CHF 227. **This ADR
therefore takes the second path: the question is undecidable from the free
artefacts, and what follows is a conservative interim behaviour rather than a
claim about what ISO says.**

## Decision

Reject, with a sentinel. `listener.properties` returns
`ErrDuplicatePropertyName` wrapped with the offending name the first time a
property name recurs within one `<property type list>`.

### Why rejection rather than a decided precedence

The two candidates were not symmetric in what they cost when wrong.

**A decided precedence can be wrong silently.** If gqlc pins last-wins and the
Syntax Rules turn out to say first-wins — or to forbid the construct — then
every schema with a repeat compiles to a model that disagrees with the standard,
and there is no artefact anywhere that says so. The failure surfaces as
generated Go with the wrong field type, at a call site far from the schema.

**A rejection cannot.** If gqlc rejects and the Syntax Rules turn out to define
a precedence, the author gets an error naming the exact line, on a construct
they can rewrite unambiguously in one edit. The cost of being wrong is an error
message that should not have been there, which is visible, local, and
recoverable. This is the argument ADR 0015 and ADR 0016 both make, and the one
`ErrEdgeKindArcMismatch` is already parked on.

Two further considerations, neither load-bearing on its own:

- **Consistency.** `resolve()` already rejects a duplicate node type and a
  duplicate edge type on precisely this reasoning — a map keyed by identity
  cannot hold two things under one key. A property map is keyed by name and has
  the same problem. This was the one duplicate gqlc did not reject.
- **The status quo was not a decision.** Last-wins was an accident of
  `out[p.Name] = p` writing in walk order. Choosing (b) would have meant
  ratifying an accident; `gqlc-v1t` had already pinned it with a semantic case
  precisely so that it could not flip to first-wins unnoticed, which is a
  measure taken against behaviour nobody had endorsed.

### Why the check lives in the listener

`listener.properties` is the only place the property **list** is still visible.
The map is the thing that loses the collision, so a guard placed downstream of
it — in `resolve()`, where the node and edge duplicate checks live — would have
nothing left to compare.

### Why the error names the property

The entire point of the rejection is that an author who changed a property's
type and left the old line behind is told so. A bare sentinel would let the
message drop the name while every `errors.Is` assertion still passed, so the
name is asserted separately in `TestRepeatedPropertyNameIsRejected`.

## Consequences

- `18.2-node-type/property_name_repeated.gql` moves from a `resolves` corpus
  entry to an `unsupported` one carrying the sentinel. The file stays; only its
  declared outcome moved. `wantCorpusResolving` 68 → 67.
- `semanticAreaB` empties. Its one case was this construct, and a rejected
  construct is not a model known to be wrong. `wantSemanticCases` 18 → 17. This
  is the once-per-case legitimate drop that pin exists to make an author account
  for; it is the same passage `kind_undirected_arc_directed` made under
  `gqlc-h9n.3`.
- Both the node and the edge collector are covered. `nodeContent` and
  `edgeContent` reach `listener.properties` through separate hand-written paths
  — the grammar gives node and edge fillers distinct generated types — so a
  guard in one and not the other would leave the diagnostic missing on half the
  surface with the corpus green. The test has a row for each.
- A schema that previously compiled now fails. That is the intent: the model it
  compiled to had already discarded a declaration the author wrote.

## What would change this

`gqlc-tlbo`, if the Syntax Rules are ever bought:

- **Forbidden:** nothing changes; the sentinel stops being provisional and the
  hedging comes out of `errors.go` and this file.
- **A precedence is defined:** the sentinel is deleted, `listener.properties`
  resolves per that precedence, and the corpus entry flips back to `resolves`.
  No semantic case is reinstated — a defined precedence is not a blind spot.
- **Silent or implementation-defined:** rejection stands, and this ADR is
  amended to record that the standard declines to answer, which is a stronger
  statement than "we have not read it".

`ErrEdgeKindArcMismatch` (`gqlc-xtq`) is blocked on the same prose, so both are
settled in one sitting.

## Provenance

Decided under `gqlc-oowt`, implemented under `gqlc-4np`. Filed as a numbered ADR
here in `docs/adr/` rather than in `kingdom/brain/decisions/`, which holds the
society's own constitutional decisions; this is a decision about the compiler.
