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
		reason:   "property types with no label set — the schema model keys node types by label set (resolve.go:35-37) and has nowhere to put an unlabelled one, so this is a modelling deviation to be recorded or removed rather than a bug with a fix pending; nodeTypeImpliedContent alternative 2 has no other route",
	},
	{
		file:     "18.2-node-type/pattern_label_implication.gql",
		outcome:  unsupported,
		sentinel: ErrLabelImplication,
		feature:  "mandatory",
		bead:     "gqlc-h9n.9",
		reason:   "the `=>` label-implication form of a key label set is not supported; nodeTypeKeyLabelSet is `labelSetPhrase? IMPLIES` and IMPLIES is reachable through no other rule",
	},
	{
		file:     "18.2-node-type/phrase_form.gql",
		outcome:  unsupported,
		sentinel: ErrUnsupportedPhraseForm,
		feature:  "mandatory",
		bead:     "gqlc-uhb",
		reason:   "the phrase form is the second alternative of nodeTypeSpecification and carries the same information as the pattern form, so supporting it is a listener addition with no model change",
	},
	{
		file:     "18.2-node-type/phrase_unnamed.gql",
		outcome:  unsupported,
		sentinel: ErrUnsupportedPhraseForm,
		feature:  "mandatory",
		bead:     "gqlc-uhb",
		reason:   "the nameless spelling of the phrase form: `NODE TYPE :A` gives nodeTypePhraseFiller alternative 2 (`[nodeTypeFiller]`), which differs from the named `NODE TYPE n :A` (alternative 1) only in whether a name is present and is otherwise the same unsupported form",
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
