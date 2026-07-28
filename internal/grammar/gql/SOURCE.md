# GQL.g4 — provenance

`GQL.g4` is a **community-contributed ANTLR grammar**, vendored unmodified from
`antlr/grammars-v4`. It is not a WG3 artefact, not an ISO publication, and not
derived from either by anyone whose method we can inspect.

It has been described in triage notes and bead bodies as "a faithful ISO GQL
grammar". That claim is unsupported, and this file replaces it. The difference
is not pedantry: "the grammar is faithful, so the grammar is not the problem"
was used once to move an investigation from the grammar to the listener. That
happened to be the right move, but the same reasoning suppresses a genuine
grammar defect, and two candidates are listed below.

## Provenance

- **URL:**
  <https://raw.githubusercontent.com/antlr/grammars-v4/master/gql/GQL.g4>
- **Upstream directory:** <https://github.com/antlr/grammars-v4/tree/master/gql>
- **Author:** Miotto Nicola (<https://github.com/miottonicola>), per the
  upstream `gql/README.md`
- **Upstream last updated:** 2026-06-13, per that README
- **Fetched and confirmed byte-identical to the vendored copy:** 2026-07-28
- **SHA-256:**
  `e1b4a24c6b88dedddc0a1fff97df0fc30bf118cea51539e26d71c717cb737bbf`
- **Lines:** 3774
- **Licence:** MIT, at the repository level; the `.g4` carries no header of its
  own and begins at `grammar GQL;`
- **Upstream's own cited reference:** ISO/IEC 39075:2024

To re-verify byte-identity:

```bash
curl -sSL 'https://raw.githubusercontent.com/antlr/grammars-v4/master/gql/GQL.g4' \
    | sha256sum
sha256sum internal/grammar/gql/GQL.g4
```

The vendored copy is not edited in place. Everything below is a statement about
the upstream file, so a local edit would leave this note describing a grammar we
no longer have; `TestVendoredGrammarIsUnmodified` pins the hash so that cannot
happen quietly. A needed change belongs upstream first.

Two things the upstream metadata does not supply. There is no statement of
method — how the transcription was made, from which text, or whether anything
checked it. And the upstream `gql/README.md`'s own "Description" and "Features"
sections describe a generic SQL-like language (joins, subqueries, indexing,
encryption) and never mention graphs, so they are not about this grammar's
contents at all. Neither observation is evidence against the grammar. Both are
reasons the word "faithful" was never earned.

## Known ANTLR adaptations and transcription artefacts

1. **`procedureSpecification` collapsed** (lines 143–149). ISO gives three
   alternatives; the file keeps `procedureBody` and comments the other three
   out, under a comment explaining that the distinction "has to be made
   sematically, in code". A deliberate adaptation, and stated as one.
2. **`connectorPointingRight` overlaps `connectorUndirected`** — `TO |
   RIGHT_ARROW` (1659–1662) against `TO | TILDE` (1664–1667). Both are reachable
   from `endpointPair` (1637–1640), which lists `endpointPairDirected` first, so
   ANTLR's ordered choice makes `CONNECTING (a TO b)` parse as directed and
   leaves the undirected reading of `TO` unreachable. ISO resolves the same
   overlap in Syntax Rules, which are prose we do not have. This is the one item
   here that changes what the parser accepts. `gqlc-h9n.9` item 4.
3. **`// 19.9 <labled predicate>`** (2076) — misspelled. Its *placement* is
   correct: it sits directly above `labeledPredicate` (2078) and the 19.x
   headings run in order from 19.1 to 19.13. `gqlc-h9n.11` recorded this comment
   as misplaced as well as misspelled; that half was checked on 2026-07-28 and
   is wrong.
4. **"sematically"** — a second misspelling, in the comment above item 1.

The list is what has been found, not what exists. Finding more of it is the
standing job of `gqlc-h9n.9`.

## What has been verified, and what has not

**Verified — byte-identity with upstream.** Above, and reproducible from the
two commands there.

**Verified — production-name coverage against ISO's own artefact.** ISO
publishes the BNF free of charge; `internal/schema/gql/isobnf` vendors the 200
production names reachable from the graph-type DDL entry points, and
`TestISOProductionInventory` sorts every one into implemented-and-exercised,
implemented-and-unexercised, or absent. 14 are absent, and that set is
ratcheted so it cannot grow. See `docs/adr/0014-iso-bnf-as-coverage-denominator.md`.
This is a check on *names*, so it can only report that a production is missing —
never that a present one is transcribed correctly.

**Not verified — fidelity within a production.** Structural divergence from
ISO clause 18 is measured and zero (`bd memories
gql-g4-clause18-structurally-complete`). Whether any individual rule body
transcribes its ISO production correctly is unmeasured, and the two must not be
conflated.

**Not corroboration — TuGraph.** `gqlc-h9n.11` recorded that the graph-type DDL
section matches, rule for rule, TuGraph's independently maintained ISO GQL
grammar, and proposed stating that corroboration *instead of* the faithfulness
claim. Checked on 2026-07-28 against `TuGraph-family/gql-grammar`: **it does
not, and it cannot.**

That repository's README says its files "are based on the draft of version
2023.03" and that the standard "is currently in the draft stage"; its last
commit is 2023-08-09. ISO/IEC 39075 was published in April 2024. It is a
pre-publication draft grammar, so agreement with it is evidence about the draft,
not about the standard.

Comparing rule bodies over the 133 graph-type DDL rules reachable in `GQL.g4`
(whitespace and ANTLR alternative labels normalised away): **49 identical, 41
differing, 43 with no same-named rule on the TuGraph side.** Some of the 41 are
cosmetic — `NOT NULL_KW` against `NOT NULL`, `WITHOUT TIMEZONE` against
`WITHOUT TIME ZONE` — but the absences are not, and they cluster exactly where
corroboration would have to hold to be worth anything. TuGraph's draft has no
rule for `labelSetPhrase`, `nodeTypePhraseFiller`, `elementTypeSpecification`,
`propertyTypesSpecification`, `nodeTypeKeyLabelSet`, `localNodeTypeAlias` or
`immaterialValueType`, all of which ISO's published production list names.
Phrase-form element type declarations — the construct `gqlc-uhb` found silently
dropped, a P1 — are among the things the draft did not yet have in the shape the
published edition gives them.

**Not verifiable — the remaining lines.** A rule-by-rule check of all 3774 lines
against Annex A requires the standard's text, which is paywalled; `gqlc-lir`
declined to buy it. That is not a gap anyone here can close by working harder.

## This grammar cannot answer a prose question

A grammar states what parses. Whether what parses *means* what we assume is in
the Syntax Rules and General Rules, which are exactly the part we do not have.
Where a decision has turned on reading the grammar as if it were the standard,
that decision is a reading and should be recorded as one.

**Worked example — `gqlc-uhb`, endpoint aliases.** `GQL.g4` spells a phrase-form
endpoint as `LEFT_PAREN sourceNodeTypeAlias connectorPointingRight
destinationNodeTypeAlias RIGHT_PAREN` (1648), where the pattern form's
`sourceNodeTypeReference` (1618) also admits an inline `nodeTypeFiller`. From
the slot's *name*, `gqlc-uhb` concluded that a `CONNECTING` endpoint naming a
node type rather than an alias is an error, and added `ErrEndpointNotAlias`
rather than a type-name fallback.

The conclusion is well supported — ISO's published production list contains both
`<source node type alias>` and `<source node type reference>` as distinct
productions, so the distinction is the standard's and not the adapter's. But the
support comes from ISO's artefact, not from this file, and the two grammars
available disagree: TuGraph's draft puts `sourceNodeTypeName` in that same slot,
which is the fallback we declined to implement. Had that grammar been the one
consulted, the same style of reasoning would have produced the opposite answer
with the same confidence.

What would settle it is the Syntax Rules for `<endpoint pair>`. Nothing in this
directory can.

## Drift check

Upstream is a live file — its README already records an update after ISO/IEC
39075's publication — so the SHA-256 above goes stale without warning. There is
no automated re-fetch for it yet; `gqlc-4jm` covers the ISO artefacts under
`internal/schema/gql/annexd` and `internal/schema/gql/isobnf` and does not cover
this one. Until that changes, the two commands under **Provenance** are the
manual check, and this file is the record.
