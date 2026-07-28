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
		file:    "18.4-label-set/label_forms.gql",
		outcome: resolves,
		feature: "mandatory",
	},
}

// semanticAreaB holds this area's semantic cases: files above that resolve to a
// model known to be wrong. Empty, and declared anyway so that recording one is an
// edit here rather than to the shared corpus_test.go. If the linter reports this
// unused, corpusAreas has lost its `semantic:` entry — TestCorpusManifest says so
// too, by area name. Wire it back rather than deleting this, which is the only
// thing standing between an author and that edit. Keep the []semanticCase{}
// spelling: the manifest requires non-nil, so `var x []semanticCase` reads as a
// lost wiring.
var semanticAreaB = []semanticCase{}
