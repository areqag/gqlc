# An element type with no labels has no identity, and three behaviours the tree had not recorded

`NODE TYPE P (p {id STRING})` is valid ISO GQL and gqlc rejects it. Three other
graph-type DDL behaviours were already implemented, two of them already pinned,
and none of them stated as a choice: a catalogue-qualified graph type name keeps
only its last component, `IF NOT EXISTS` and `OR REPLACE` parse and are ignored,
and `CONNECTING (a TO b)` is read as pointing-right on the strength of ANTLR's
rule order.

`gqlc-h9n.10` collected the four. Individually none justifies a bead; together
they were the difference between an accepted subset that is defined and one that
merely is what the code happens to do.

## Context

### 1. An element type with no labels is rejected

`nodeTypeImpliedContent` (GQL.g4:1526-1530) has three alternatives, and the
middle one is property types with no label set. `edgeTypeImpliedContent` mirrors
it. So `NODE TYPE P (p {id STRING})` parses, walks, and fails in resolution with
`ErrUnnamedNodeType`.

**Decision: keep rejecting, and say why in the error.** The reason is identity,
not omission. `schema.Schema` keys `Nodes` by `graph.LabelSetKey` and `Edges` by
a source/label/target triple; `LabelSet.Key()` on an empty set is the empty
string. That is not "a node type with a blank name" — it is the *same* key for
every unlabelled type in the graph, so a second one collides with the first
rather than being distinguishable from it. There is no identity to record, and
nothing downstream to reach it by.

The rejection is gqlc's, made on the model, and it is stated that way. Whether
ISO/IEC 39075 permits an element type with an empty key label set is a question
for the Syntax Rules, which are in the paid PDF that `gqlc-lir` declined to buy —
so the honest form is "gqlc cannot represent it", not "the standard forbids it".
That distinction is the whole point of the no-dialect principle: a rejection of
grammar-legal GQL is a dialect unless the reason names something real, and the
identity argument does.

ADR 0015 already carried half of this. `(=> :Thing)` — an explicitly empty key
label set under GG21 — is rejected for exactly the same reason and already
reports `ErrUnnamedNodeType`. What the GG22 form (no key label set written at
all) lacked was a statement that it lands there deliberately rather than by
falling through the same branch.

The errors now carry the reason after the colon, in the style
`ErrImpliedLabelIsKeyLabel` and `ErrLikeGraphSource` already use.

### 2. A catalogue-qualified name keeps only its last component

`CREATE GRAPH TYPE /a/b/G AS { ... }` resolves as if it had been written `G`;
`EnterCreateGraphTypeStatement` reads `GraphTypeName().Identifier()` and never
looks at the parent path.

**No code change. Already pinned** by `TestGraphTypeName`, which has both the
dotted (`store.metrics.M`) and simple spellings.

What was missing is why it is defensible. `Schema.Name`'s one consumer is
`derivePackage` (codegen/prepare.go:252), which lowercases it and requires a Go
package identifier — so a name is a *label for the generated package*, never a
key anything is looked up by. `/a/b/G` is not an identifier and would fail with
`ErrInvalidPackageName`; the last component usually is one. Discarding the parent
loses nothing that is read.

**It stops being harmless when `COPY OF` lands.** That form (`gqlc-h9n.1`)
references a graph type *by catalogue path*, at which point `/a/b/G` and
`/c/d/G` become the same name, and the truncation turns from a display choice
into a collision. This paragraph is the note that bead needs to find.

### 3. `IF NOT EXISTS` and `OR REPLACE` parse and are ignored

Both sit in `createGraphTypeStatement` (GQL.g4:344). Both are accepted and
discarded.

**No code change. Already pinned** by the same `TestGraphTypeName`.

Ignoring them is not a shortcut: gqlc reads one graph type from a file and has no
catalogue to check against or replace in, so there is exactly one graph type and
it is always created. Both modifiers are conditions on a state that does not
exist.

Note how this differs from item 2, because the two look alike and age
differently. These are ignored because of gqlc's execution model, and that stays
true when `COPY OF` lands. Item 2 discards information that is currently unread,
and that stops being true.

### 4. `CONNECTING (a TO b)` is pointing-right by rule order

    connectorPointingRight : TO | RIGHT_ARROW      GQL.g4:1659-1662
    connectorUndirected    : TO | TILDE            GQL.g4:1664-1667

`TO` is an alternative of both. Both connectors are reachable from `endpointPair`
(GQL.g4:1637-1640), which lists `endpointPairDirected` first, so ANTLR's ordered
choice takes the directed parse and the undirected reading of `TO` is
unreachable.

**Decision: keep the reading, and pin the consequence.** `TO` reads as a
direction in English, and every `CONNECTING (X TO Y)` ever written against gqlc
depends on it: under the other reading each one would hit `ErrUndirectedEdge`
instead of resolving.

But the grammar does not entitle us to that reading. ISO resolves the same
overlap in Syntax Rules, which we do not have; what resolves it here is the order
two alternatives happen to appear in a vendored community grammar
(`internal/grammar/gql/SOURCE.md`). So it is pinned as behaviour rather than
asserted as meaning: `TestConnectorToResolvesPointingRight` asserts the resolved
endpoints, not merely that the parse succeeds, because a flip in rule order or a
regeneration from a reordered upstream would otherwise change every schema
silently.

The corpus gains `kind_undirected_connector_to.gql`, the phrase-form twin of
`kind_undirected_arc_directed.gql`. `UNDIRECTED ... CONNECTING (a TO b)` reports
`ErrEdgeKindArcMismatch` today — the declared kind contradicting a direction the
*parser* chose. Under the other reading of the same text it would report
`ErrUndirectedEdge`. Both reject, which is why the ambiguity has never surfaced
as a defect and why it could have gone on not surfacing.

## Considered options

**Item 1 — accept an unlabelled element type and key it on the empty set.**
Rejected: the first one works and the second one silently overwrites it, or
reports `ErrDuplicateNodeType` against a type the author never said was the same.

**Item 1 — synthesise a label from the node type name.** Rejected: it invents a
label the author did not write, and ISO's `nodeTypeName` is a catalogue name
rather than a label. The name is optional too, so it does not cover the case.

**Item 2 — reject a catalogue-qualified name.** Rejected: it is valid ISO GQL,
and rejecting a whole schema over the spelling of a name that is only ever used
to derive a package identifier is the dialect this epic exists to remove.

**Item 2 — keep the whole path as `Schema.Name`.** Deferred rather than
rejected: it is what `COPY OF` will need, and doing it now would fail
`derivePackage` for every qualified name with no consumer asking. `gqlc-h9n.1`
owns it.

**Item 4 — reject `TO` as ambiguous.** Rejected: the ambiguity is the grammar's,
not the author's, and `CONNECTING (a TO b)` is the phrase form's most ordinary
spelling.

**Record nothing, since three of the four already behave sensibly.** Rejected:
that is the state `gqlc-h9n.10` was filed against. Two of the three were pinned
by tests whose names say what happens and not that anyone chose it, and a pin
without a reason is indistinguishable from an accident that nobody has hit yet.

## Consequences

- `ErrUnnamedNodeType` and `ErrUnnamedEdgeType` state the identity reason. The
  sentinels, their reachability and every corpus entry pointing at them are
  unchanged; only the messages move.
- Two corpus entries — `18.2-node-type/pattern_properties_only.gql` and
  `18.3-edge-type/pattern_properties_only.gql` — keep `gqlc-0ri` and cite this
  ADR, so the deviation register records them as decided rather than as pending
  a decision.
- The corpus gains one file, `18.3-edge-type/kind_undirected_connector_to.gql`,
  and `wantCorpusEntries` moves from 91 to 92.
- `gqlc-h9n.1` inherits item 2's note. It cannot land `COPY OF` without either
  keeping the parent path or recording why two paths ending in the same
  component may collide.
- Nothing here unblocks `gqlc-h9n.28` or `gqlc-h9n.29`. Items 1 and 4 are both
  decided *without* the prose that would settle them, and both say so.
