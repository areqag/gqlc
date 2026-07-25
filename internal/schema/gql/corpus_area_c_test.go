package gql

// corpusAreaC holds the corpus entries for edge types. One area variable per
// author so that two authors never edit the same Go file; corpusAreas fixes the
// directories these entries may live in.
var corpusAreaC = []corpusEntry{
	{
		file:    "18.3-edge-type/pattern_directed.gql",
		outcome: resolves,
		feature: "mandatory",
	},
	{
		file:     "18.3-edge-type/pattern_undirected.gql",
		outcome:  unsupported,
		sentinel: ErrUndirectedEdge,
		feature:  "mandatory",
		bead:     "gqlc-h9n.3",
		reason:   "an undirected arc has no canonical source -> target identity, which EdgeKey requires",
	},
	{
		file:    "18.3-edge-type/kind_undirected_arc_directed.gql",
		outcome: resolves,
		feature: "mandatory",
		bead:    "gqlc-h9n.3",
		reason:  "the UNDIRECTED kind is discarded and the edge resolves as if it were DIRECTED",
	},
	{
		file:     "18.3-edge-type/phrase_form.gql",
		outcome:  unsupported,
		sentinel: ErrUnsupportedPhraseForm,
		feature:  "mandatory",
		bead:     "gqlc-uhb",
		reason:   "the phrase form is the second alternative of edgeTypeSpecification and names its endpoints with CONNECTING instead of an arc, so supporting it is a listener addition with no model change",
	},
}
