package gql

// corpusArea126GraphTypeStatement holds the corpus entries for
// test/data/schema/gql/corpus/12.6-graph-type-statement/ — ISO GQL clause 12.6
// <graph type statement>. One area variable per clause so authors never share a
// file; corpusAreas requires every entry here to live under that directory.
var corpusArea126GraphTypeStatement = []corpusEntry{
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
