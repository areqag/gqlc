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
