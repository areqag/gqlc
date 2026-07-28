# ISO's own BNF is the coverage denominator for graph-type DDL

Grammar coverage for graph-type DDL is measured against the 200 ISO/IEC 39075
productions reachable from the graph-type DDL entry points, vendored as bare
names in `internal/schema/gql/isobnf`. Every production is sorted into one of
three buckets — implemented and exercised, implemented and unexercised, absent
from `GQL.g4` — and the third is ratcheted so it cannot grow.

**Coverage of this production list is a necessary but not sufficient condition
for conformance.** A green number here means we have a grammar rule for every
production ISO names; it does not mean we implement what the standard says those
productions mean.

## Context

- The epic's stated goal is that if it is valid ISO GQL, we should accept it.
  The existing coverage gate (`TestCorpusGrammarCoverage`) measures the corpus
  against `internal/grammar/gql/GQL.g4`.
- A denominator taken from our own grammar can only report alternatives we
  implemented and did not exercise. A production that is in the standard and
  absent from our grammar is *not in the denominator*, so 100% coverage is
  reachable while arbitrarily much of the standard is missing. "Have we covered
  our grammar" is a different question from "have we covered the standard", and
  only the second one is the epic's.
- ISO publishes a free-of-charge digital artefact,
  `ISO_IEC_39075(en).bnf.txt`, under the ISO Customer Licence: 814 productions,
  normative, machine-readable. It is not a draft, not a vendor rendering, and
  not the community ANTLR adaptation vendored as `internal/grammar/gql/GQL.g4`
  (provenance: `internal/grammar/gql/SOURCE.md`). This makes the correct
  denominator available for free.
- `gqlc-lir` declined to buy the standard itself, so the normative *prose* —
  the Syntax Rules, the General Rules, the conformance clauses — is unavailable.
  `gqlc-h9n.28` and `gqlc-h9n.29` are blocked on exactly that prose.

## Considered options

**Keep only the `GQL.g4` denominator.** Rejected: it is structurally incapable
of producing the third bucket, which is the one that measures the epic.

**Replace the `GQL.g4` denominator.** Rejected: the two gates catch different
defects. The g4 one finds dead alternatives — grammar we wrote that nothing
reaches — which the ISO gate cannot see, because a dead alternative is still an
implemented production. Both are kept and they are deliberately not folded
together.

**Hand-pick the DDL-relevant productions.** Rejected. A human-chosen subset
reintroduces exactly the selection bias the ISO denominator exists to remove:
the productions a reader forgets to list are correlated with the ones we forgot
to implement. The subset is derived mechanically by
`isobnf/extract_ddl_closure.py`, which is committed beside its output so the
derivation is auditable rather than asserted.

**Gate on a coverage percentage.** Rejected. A percentage moves when the
denominator moves, so adding corpus files would read as progress while nothing
was implemented. The ratchet is on the *count* of absent productions.

**Start life as a hard zero-gate.** Rejected. The absent bucket is non-empty
today, so a zero-gate would force either a mass of stub implementations or an
exemption list long enough to be meaningless — both destroy the measurement.
It lands as a recorded inventory with a ratchet, and the count is driven down
bead by bead.

## Decision

**The denominator is `isobnf.DDLClosure`**: the transitive closure from
`<create graph type statement>`, `<drop graph type statement>` and
`<nested graph type specification>`, with `<graph expression>` as a frontier.
200 productions of the artefact's 814. `SOURCE.md` records the URL, the fetch
date and the SHA-256, which is also pinned in Go so a re-vendor from a different
artefact cannot land silently.

**Only production names are vendored, never bodies.** `annexd/` drew this line
for ISO's feature descriptions and it is drawn again here: the gate needs to
answer *"is this production in the standard"*, not *"what does ISO say it
expands to"*, so reproducing the grammar text buys nothing against the extra
licence question it raises.

**Three buckets, and the third is the point.** A production matches if `GQL.g4`
declares a parser rule or a lexer rule (fragments included) whose name matches
after case and separator normalisation. Matched-and-covered and
matched-and-uncovered are reported; unmatched is `isoGaps`.

**`isoGaps` is an exact-set assertion plus a count ratchet**, modelled on the
`alternativeExemption` design from `gqlc-h9n.2` amendment A9. The exact-set half
is bidirectional — a production that becomes implemented leaves a stale entry
that fails, and a newly absent one has no entry and fails — so the inventory is
a record rather than a number someone edits. The count half (`isoGapRatchet`)
stops the exact-set half being satisfied by appending entries. Every entry names
a bead and a reason, because what makes a gap answerable is prose a reviewer
reads.

**Coverage here is necessary, not sufficient, for conformance.** The BNF is
syntax only. It cannot answer any question of the form "what does the standard
say this production means". A green ISO coverage number must not be cited as a
conformance claim, and it does not unblock `gqlc-h9n.28` or `gqlc-h9n.29`.

## Consequences

- The epic gains an objective, mechanically checkable measure. At landing:
  **164 exercised, 22 unexercised, 14 absent** of 200.
- The 14 absent productions are attributed: 4 to `gqlc-h9n.5` (the parameterised
  type model — `<list value type>` and friends), 4 to `gqlc-h9n.6` (the dynamic
  union family), 3 to `gqlc-lir`, and 3 that are implemented in substance but
  not as named rules.
- Closing a gap is now a two-line diff — delete the entry, lower the ratchet —
  which gives several sibling beads an objective done condition they lacked.
- **The free artefact is a production list, not a complete grammar.** Five of
  the 200 productions in the closure (`<character representation>`,
  `<external object reference>`, `<identifier start>`, `<identifier extend>`,
  `<other digit>`) have `!! See the Syntax Rules.` as their *entire* body; 16 do
  across the whole artefact. For those, even the syntax is paywalled, so three
  of them are attributed to `gqlc-lir` rather than to an implementation bead.
  This bounds what any BNF-derived gate can ever assert, and is a second,
  independent reason the necessary-not-sufficient caveat above is not a
  formality.
- `<graph type specification>` — the name `gqlc-h9n.30` used for the root — does
  not exist as a production in the artefact. The roots above are the actual
  spellings.
- The vendored snapshot goes stale when ISO publishes a new edition. `gqlc-4jm`
  is the drift-check bead; it was written for `annexd/` and covers `isobnf/` too.
