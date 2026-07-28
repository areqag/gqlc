# ISO GQL BNF production list — source of `productions.go`

`productions.go` vendors the **names** of the ISO/IEC 39075 BNF productions
reachable from the graph-type DDL entry points. The production **bodies** are
not vendored. The gate that consumes this list needs to answer *"is this
production in the standard"*, not *"what does ISO say it expands to"*, so
reproducing the grammar text carries no benefit against the additional licence
question it raises. This is the same line `annexd/SOURCE.md` draws.

## Provenance

- **URL:** <https://standards.iso.org/iso-iec/39075/ed-1/en/ISO_IEC_39075(en).bnf.txt>
- **Fetched:** 2026-07-26, re-fetched and confirmed byte-identical 2026-07-27
- **SHA-256 of source BNF at fetch time:**
  `d1b56017ee38ee29e1d05655ee16c9113e1b020e9bc038c7ab5fcc0bc41d6ac3`
  (also pinned as `isobnf.SourceSHA256`, so a re-vendor from a different
  artefact cannot land silently)
- **Productions in the whole artefact:** 814
- **Productions in the DDL closure:** 200
- **Licence:** published by ISO under the ISO Customer Licence (Freely
  Available Standards / free-of-charge digital artefacts)

This is the artefact ISO themselves publish as containing the grammar specified
by ISO/IEC 39075. It is not a draft, not a vendor rendering, and not the
community ANTLR adaptation vendored as `internal/grammar/gql/GQL.g4`, whose
provenance is recorded in `internal/grammar/gql/SOURCE.md`.

## Regeneration

```bash
curl -sSL 'https://standards.iso.org/iso-iec/39075/ed-1/en/ISO_IEC_39075(en).bnf.txt' -o /tmp/iso39075.bnf.txt
python3 extract_ddl_closure.py /tmp/iso39075.bnf.txt > productions.go
```

The script is committed beside the output rather than reduced to a shell
one-liner because the closure is a graph walk, and because *how* the subset was
chosen is the part a reviewer needs to audit. Hand-picking the subset would
reintroduce exactly the selection bias this list exists to remove.

## Roots and the one cut

**Roots** — `<create graph type statement>`, `<drop graph type statement>`,
`<nested graph type specification>`.

`<graph type specification>` is the natural name for this concept and the name
`gqlc-h9n.30` used, but **no such production exists** in the artefact. The three
roots above are the actual spellings. They are also minimal: `<graph type
source>`, `<graph type reference>` and `<catalog graph type parent and name>`
are each reachable from them, so adding them as roots leaves the closure at 200.

**Cut** — `<graph expression>`. It enters through exactly one production,
`<graph type like graph>` (the `LIKE` form of a graph type source). Descending
into it takes the closure from **200 to 738** productions, i.e. it pulls in the
entire query language. Query-side coverage is the vendored openCypher TCK's job,
not this gate's. The script fails if the cut is never reached, so the cut cannot
quietly stop applying.

A cut production is a **frontier, not an exclusion**: `<graph expression>` is
itself in the list, because graph-type DDL genuinely references it and we do
want to know whether we implement it. What is excluded is its subtree — the 538
productions reachable only through it. `TestGraphExpressionIsAFrontierNotAMember`
pins both halves.

## What this list is not

The BNF is syntax only. It cannot answer any question of the form *"what does
the standard say this production means"* — that prose is in the paid PDF, which
`gqlc-lir` declined to buy. `gqlc-h9n.28` and `gqlc-h9n.29` are blocked on
exactly that prose and are **not** unblocked by this list.

Coverage of this production list is a **necessary but not sufficient** condition
for conformance. See `docs/adr/0014-iso-bnf-as-coverage-denominator.md`.

One production in the closure, `<character representation>`, has the body
`!! See the Syntax Rules.` — ISO defers even the syntax to the paywalled prose
there. The artefact is complete as a production *list*; it is not complete as a
grammar.

## Drift check

The vendored snapshot goes stale when ISO publishes ISO/IEC 39075:2024/CD Cor 1
or a subsequent edition. `gqlc-4jm` is the open bead for the durable drift check
that will re-fetch the URL and recompute the SHA-256; it was written for
`annexd/` and covers this file too. Until it lands, this file is the manual
record.
