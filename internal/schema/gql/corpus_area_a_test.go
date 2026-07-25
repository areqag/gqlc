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
		bead:     "wontfix",
		reason:   "LIKE derives the type from an existing graph, which needs a live catalogue to inspect; gqlc generates from schema text alone",
	},
}
