package gql

// corpusArea183EdgeType holds the corpus entries for
// test/data/schema/gql/corpus/18.3-edge-type/ — ISO GQL clause 18.3 <edge type
// specification>. One area variable per clause so authors never share a file;
// corpusAreas requires every entry here to live under that directory.
var corpusArea183EdgeType = []corpusEntry{
	{
		file:    "18.3-edge-type/pattern_directed.gql",
		outcome: resolves,
		feature: "mandatory",
		golden:  true,
	},
	{
		file:     "18.3-edge-type/pattern_undirected.gql",
		outcome:  unsupported,
		sentinel: ErrUndirectedEdge,
		feature:  "GE03",
		bead:     "gqlc-h9n.3",
		reason:   "an undirected arc has no canonical source -> target identity, which EdgeKey requires",
	},
}
