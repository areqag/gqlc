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
		file:     "18.2-node-type/phrase_form.gql",
		outcome:  unsupported,
		sentinel: ErrUnsupportedPhraseForm,
		feature:  "mandatory",
		bead:     "gqlc-uhb",
		reason:   "the phrase form is the second alternative of nodeTypeSpecification and carries the same information as the pattern form, so supporting it is a listener addition with no model change",
	},
}

// semanticAreaB holds this area's semantic cases: files above that resolve to a
// model known to be wrong. Empty, and declared anyway so that recording one is an
// edit here rather than to the shared corpus_test.go. If the linter reports this
// unused, corpusAreas has lost its `semantic:` entry — wire it back rather than
// deleting this, which is the only thing standing between an author and that edit.
var semanticAreaB = []semanticCase{}
