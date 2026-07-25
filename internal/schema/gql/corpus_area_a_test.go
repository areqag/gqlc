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
		feature:  "GG02",
		bead:     "gqlc-0ri",
		reason:   "LIKE derives the type from a graph *instance*, so it cannot be answered without inspecting live data; declined permanently, and conformant because GG02 is an optional Annex D feature",
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
