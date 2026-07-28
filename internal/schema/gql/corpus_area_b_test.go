package gql

// corpusAreaB holds the corpus entries for node types, label sets and property
// types. One area variable per author so that two authors never edit the same Go
// file; corpusAreas fixes the directories these entries may live in.
var corpusAreaB = []corpusEntry{
	{
		file:    "18.2-node-type/pattern_multi_label.gql",
		outcome: resolves,
		feature: "mandatory",
	},
	{
		file:     "18.2-node-type/pattern_properties_only.gql",
		outcome:  unsupported,
		sentinel: ErrUnnamedNodeType,
		feature:  "mandatory",
		bead:     "gqlc-0ri",
		reason:   "property types with no label set — the schema model keys node types by label set (resolve.go:35-37), and an empty key is not a blank name but the same key for every unlabelled type, so a second one would collide rather than differ. Decided in ADR 0018: declined on gqlc's model, not on a reading of the standard. nodeTypeImpliedContent alternative 2 has no other route",
	},
	{
		file:    "18.2-node-type/pattern_label_implication.gql",
		outcome: resolves,
		feature: "GG21",
	},
	{
		file:     "18.2-node-type/implied_key_label.gql",
		outcome:  unsupported,
		sentinel: ErrImpliedLabelIsKeyLabel,
		feature:  "GG21",
		bead:     "gqlc-0ri",
		reason:   "an implied label that is also a declared type's key label is the one part of GG21 gqlc declines: Fabric inherits that type's properties, Neo4j forbids the schema, and the standard's position is unresolved — see ADR 0015",
	},
	{
		file:     "18.2-node-type/implied_key_label_declared_later.gql",
		outcome:  unsupported,
		sentinel: ErrImpliedLabelIsKeyLabel,
		feature:  "GG21",
		bead:     "gqlc-0ri",
		reason:   "implied_key_label.gql in the order that discriminates: the implying declaration comes first, so the key label it collides with is only read afterwards. resolve() defers rejectInheritance past the node loop for exactly this, a collision being a property of the whole body rather than of a prefix of it",
	},
	{
		file:     "18.2-node-type/implied_key_label_not_first.gql",
		outcome:  unsupported,
		sentinel: ErrImpliedLabelIsKeyLabel,
		feature:  "GG21",
		bead:     "gqlc-0ri",
		reason:   "implied_key_label.gql with the collision moved off the front of the implied label set. rejectInheritance checks every label a declaration implies rather than only the first, and its comment says so — `(:A&B)` holds both A and B as key labels, so implying either collides — but every other GG21 file collides on the first label, so narrowing the loop to that one left nothing red. Zeta is second as written and in sort order, so the case discriminates however the implied set comes to be ordered",
	},
	{
		file:     "18.2-node-type/implied_key_label_second_implying_declaration.gql",
		outcome:  unsupported,
		sentinel: ErrImpliedLabelIsKeyLabel,
		feature:  "GG21",
		bead:     "gqlc-0ri",
		reason:   "implied_key_label_not_first.gql moved out one level: rejectInheritance walks the implying declarations and then each one's implied labels, and the outer loop needed its own case. Manager implies Staff, which nothing holds as a key label, so a check that stops at the first implying declaration accepts a body whose second one collides on Person",
	},
	{
		file:     "18.2-node-type/empty_key_label_set.gql",
		outcome:  unsupported,
		sentinel: ErrUnnamedNodeType,
		feature:  "GG21",
		bead:     "gqlc-0ri",
		reason:   "`(=> :Thing)` declares an explicitly empty key label set, and a node type with no identity has nothing to key Schema.Nodes on; GG22's inference does not apply because a key label set was declared",
	},
	{
		file:    "18.2-node-type/phrase_form.gql",
		outcome: resolves,
		feature: "mandatory",
	},
	{
		file:    "18.2-node-type/phrase_unnamed.gql",
		outcome: resolves,
		feature: "mandatory",
	},
	{
		file:     "18.2-node-type/phrase_name_only.gql",
		outcome:  unsupported,
		sentinel: ErrUnnamedNodeType,
		feature:  "mandatory",
		bead:     "gqlc-0ri",
		reason:   "`NODE TYPE Person` names the type and declares no labels, so it lands where every unlabelled type lands. The name reaches NodeType.Name and still confers no identity, Schema.Nodes being keyed on the key label set — ADR 0018's argument, reached through nodeTypePhraseFiller alternative 1 rather than through a pattern",
	},
	{
		file:    "18.2-node-type/pattern_key_label_set_only.gql",
		outcome: resolves,
		feature: "GG21",
	},
	{
		file:    "18.2-node-type/pattern_empty_property_block.gql",
		outcome: resolves,
		feature: "mandatory",
	},
	{
		file:    "18.2-node-type/pattern_property_typed_elided.gql",
		outcome: resolves,
		feature: "mandatory",
	},
	{
		file:    "18.2-node-type/property_name_repeated.gql",
		outcome: resolves,
		feature: "mandatory",
		bead:    "gqlc-4np",
		reason:  "the second `id` overwrites the first in listener.properties' name-keyed map, so the declared INT and its NOT NULL are gone from the model with no diagnostic. gqlc rejects a duplicate node type and a duplicate edge type on the same reasoning and does not reject this one. Whether the answer is a sentinel or a decided precedence is gqlc-4np's ADR; the entry exists so the accidental last-wins cannot flip to first-wins with the suite green",
	},
	{
		file:    "18.4-label-set/label_forms.gql",
		outcome: resolves,
		feature: "mandatory",
	},
}

// semanticAreaB holds this area's semantic cases: files above that resolve to a
// model known to be wrong. If the linter reports this unused, corpusAreas has lost
// its `semantic:` entry — TestCorpusManifest says so too, by area name. Wire it
// back rather than deleting this. Should the list ever empty out again, keep the
// []semanticCase{} spelling: the manifest requires non-nil, so
// `var x []semanticCase` reads as a lost wiring.
var semanticAreaB = []semanticCase{
	{
		file:     "18.2-node-type/property_name_repeated.gql",
		bead:     "gqlc-4np",
		why:      "two declarations of `id` resolve to one property carrying only the second's type and nullability; the first is discarded, so the model cannot be told apart from one that never declared it",
		spelling: "id :: INT NOT NULL, id :: STRING",
		siblings: []string{"id :: STRING"},
	},
}
