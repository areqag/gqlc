# GQL.g4 — provenance

`GQL.g4` is a **community-contributed ANTLR grammar**, vendored from
`antlr/grammars-v4` and since `gqlc-eg4b` carrying gqlc additions on top
(**Local modifications**, below). It is not a WG3 artefact, not an ISO
publication, and not derived from either by anyone whose method we can inspect.

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
- **Upstream SHA-256:**
  `e1b4a24c6b88dedddc0a1fff97df0fc30bf118cea51539e26d71c717cb737bbf`
- **Vendored-file SHA-256** (upstream plus **Local modifications**):
  `283e1301f5b0dea55165755fbb91e625a357aa59a066ad302a2485db498a8c14`
- **Lines:** 3774 upstream, 3819 as vendored
- **Licence:** MIT, at the repository level; the `.g4` carries no header of its
  own and begins at `grammar GQL;`
- **Upstream's own cited reference:** ISO/IEC 39075:2024

To re-verify the upstream half — note that the second command hashes the file
with gqlc's insertions removed, not the file as it sits in the tree, and that
the two hashes above are different values on purpose:

```bash
curl -sSL 'https://raw.githubusercontent.com/antlr/grammars-v4/master/gql/GQL.g4' \
    | sha256sum
go test ./internal/schema/gql/ -run TestVendoredGrammarLocalDeltaIsDeclared -v
```

Everything in this file outside **Local modifications** is a statement about the
*upstream* file, so an undeclared local edit would leave the note describing a
grammar we no longer have. Two pins stop that happening quietly, and they answer
different questions. `TestVendoredGrammarIsUnmodified` hashes the whole file, so
it fails on any edit at all. `TestVendoredGrammarLocalDeltaIsDeclared` deletes
every insertion gqlc declares and requires what remains to hash to the upstream
value — so an in-place edit to an *upstream* line fails it even if someone
re-pinned the whole-file hash, which is the failure a bare re-pin invites.

A change that ISO or upstream should own still belongs upstream first. The
Local modifications section is for constructs upstream cannot be expected to
carry because they are not in the standard at all.

Two things the upstream metadata does not supply. There is no statement of
method — how the transcription was made, from which text, or whether anything
checked it. And the upstream `gql/README.md`'s own "Description" and "Features"
sections describe a generic SQL-like language (joins, subqueries, indexing,
encryption) and never mention graphs, so they are not about this grammar's
contents at all. Neither observation is evidence against the grammar. Both are
reasons the word "faithful" was never earned.

## Local modifications

Two: UUID, added by `gqlc-eg4b` (stage 1 of `gqlc-do1`), and VECTOR, added by
`gqlc-do1` itself. Every hunk is a pure insertion; no upstream line is edited,
reordered or removed, which is the property the delta test relies on. The
VECTOR rule hunk sits inside the UUID one, so the delta test removes it first.

1. **`UUID` as a predefined type.** A lexer token `UUID: 'UUID';` in the
   keyword block, a `uuidType : UUID notNull? ;` rule, and `| uuidType`
   appended as the **last** alternative of `predefinedType`.

   ISO/IEC 39075 has no UUID value type — it is absent from the published
   production list, not merely unimplemented here — and neither does Cypher 25,
   so there is no upstream home for this and no ISO spelling to transcribe. It
   is gqlc's own construct, and the corpus classifies it as `feature:
   "extension"` for that reason rather than as `mandatory` or `unsourced`.

   Appended last rather than inserted: ANTLR resolves an ambiguity between
   alternatives by their order, and item 2 under **Known ANTLR adaptations**
   below is this grammar's existing instance of an alternative made unreachable
   that way. Verified after regeneration by diffing the generated parser, not
   by a green test run — the emitted `PredefinedType()` is a `switch` on the
   lookahead token and the delta is a single appended `case GQLParserUUID:`,
   with every pre-existing case byte-identical modulo state renumbering.

   `UUID` is reserved, like every other type keyword this grammar declares and
   unlike ISO's `<non-reserved word>` list, which cannot name it. The escape
   hatch for a property or label spelled `uuid` is the delimited identifier.

2. **`VECTOR` as a predefined type.** A lexer token `VECTOR: 'VECTOR';` in the
   keyword block, a `vectorType : VECTOR '<' valueType '>' '(' unsignedInteger
   ')' notNull? ;` rule, and `| vectorType` appended after `| uuidType` as the
   new last alternative of `predefinedType`, for the reason given in item 1.

   The spelling is Cypher 25's (`VECTOR<FLOAT32>(1024)`, Neo4j 2025.10 and
   later). ISO/IEC 39075 has no vector value type, so this is an extension in
   the same sense UUID is and the corpus file is `feature: "extension"`. Both
   parameters are mandatory, as they are in Cypher's own property type
   constraints. The element is a full `valueType`, so that an element no target
   can carry is refused by schema admission under a sentinel that can list the
   admitted ones, rather than by the parser. The dimension is parsed and
   dropped (ADR 0049).

   Verified the same way: the regenerated `PredefinedType()` gains a single
   `case GQLParserVECTOR:` and loses none. `VECTOR` is reserved like `UUID`, and
   `TestPropertyVectorReservedWordEscapeHatch` pins the delimited escape hatch.

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
5. **`booleanValueExpression` sits under `// 20.1 <value expression>`**
   (2196–2198). ISO's production order puts `<boolean value expression>` between
   `<field>` and `<truth value>`, which is the 20.20 heading's place in this file
   (2554), so this is a rule filed under a clause that does not declare it. It
   moves no ownership in the corpus gates, which assign nothing under clause 20,
   and fixing it means reordering upstream lines, which the delta test above
   refuses by design — so it is recorded here and not moved. `gqlc-h9n.27`.
6. **Heading labels in clause 12.** `// 12.4 <insert graph statement>` (311)
   names no ISO production; the rule under it is `createGraphStatement`,
   ISO's `<create graph statement>`. `// 12.7 <drop graph statement>` (357)
   repeats 12.5's label over `dropGraphTypeStatement`, ISO's
   `<drop graph type statement>`. The section *numbers* match ISO's order; the
   labels do not, and the corpus gates read only the numbers.

`gqlc-h9n.27` swept every heading for item 5's class on 2026-10-27, at
`9884911b`. The method: take ISO's free BNF (see **What has been verified**
below) in its published order, keep the 563 productions whose camel-cased name
is a rule here, and require the heading numbers to be non-decreasing along that
order. Item 5 was the only break. The sweep cannot see the 36 rules here with no
same-named ISO production (12 parser rules, among them gqlc's `uuidType` and the
`characterOrByteStringFunction` family under 20.1, and 24 lexer rules), and it
cannot place a rule that sits at a clause boundary, since either neighbouring
heading keeps the order. Ownership of those is unmeasured, not confirmed.

That sweep also refuted the bead's original premise. `graphTypeLikeGraph` (and
`LIKE` through it) under 12.4, and `isOrColon` (and `IS`, `COLON`) under 16.7,
were thought to be misfiled for typesetting reasons. ISO declares
`<graph type like graph>` in 12.4, between `<of graph type>` and
`<graph source>`, and `<is or colon>` in 16.7, beside `<is label expression>`.
The headings are right. What was missing was an area owning those two clauses,
and that is `ownershipOnlyPrefixes` in `internal/schema/gql/corpus_test.go`.

The list is what has been found, not what exists. Finding more of it is the
standing job of `gqlc-h9n.9`.

## What has been verified, and what has not

**Verified — the upstream half is byte-identical to upstream.** Above, and
reproducible from the two commands there. What is verified is the file minus the
declared local insertions; the file as it sits in the tree is deliberately not
byte-identical to upstream and has not been since `gqlc-eg4b`.

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
39075's publication — so the upstream SHA-256 above goes stale without warning. There is
no automated re-fetch for it yet; `gqlc-4jm` covers the ISO artefacts under
`internal/schema/gql/annexd` and `internal/schema/gql/isobnf` and does not cover
this one. Until that changes, the two commands under **Provenance** are the
manual check, and this file is the record.
