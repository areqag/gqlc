package gql

// corpusAreaA holds the corpus entries for the graph type statement itself, the
// references that name a source graph type, and the identifier and nested-body
// grammar every other area builds on. One area variable per author so that two
// authors never edit the same Go file; corpusAreas fixes the directories these
// entries may live in.
var corpusAreaA = []corpusEntry{
	{
		file:    "12.6-graph-type-statement/nested_body.gql",
		outcome: resolves,
		feature: "mandatory",
	},
	{
		file:     "12.6-graph-type-statement/like_graph.gql",
		outcome:  unsupported,
		sentinel: ErrUnsupportedSource,
		feature:  "unsourced",
		bead:     "gqlc-0ri",
		reason:   "LIKE derives the type from a graph *instance*, so it cannot be answered without inspecting live data; declined permanently",
	},
	{
		file:     "12.6-graph-type-statement/copy_of_source.gql",
		outcome:  unsupported,
		sentinel: ErrUnsupportedSource,
		feature:  "mandatory",
		bead:     "gqlc-h9n.1",
		reason:   "COPY OF names a graph *type*, so it is a catalogue and multi-file scoping problem rather than a data-inspection one, and is solvable in principle; it shares LIKE's sentinel only because the element types are absent from this file either way",
	},
}

// semanticAreaA holds this area's semantic cases: files above that resolve to a
// model known to be wrong. Empty, and declared anyway so that recording one is an
// edit here rather than to the shared corpus_test.go. If the linter reports this
// unused, corpusAreas has lost its `semantic:` entry — wire it back rather than
// deleting this, which is the only thing standing between an author and that edit.
var semanticAreaA = []semanticCase{
	{
		file: "12.6-graph-type-statement/nested_body.gql",
		bead: "gqlc-h9n.99",
		why:  "fabricated row to test the unwired-var case",
	},
}
