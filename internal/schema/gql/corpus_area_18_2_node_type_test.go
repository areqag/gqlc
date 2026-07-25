package gql

// corpusArea182NodeType holds the corpus entries for
// test/data/schema/gql/corpus/18.2-node-type/ — ISO GQL clause 18.2 <node type
// specification>. One area variable per clause so authors never share a file;
// corpusAreas requires every entry here to live under that directory.
var corpusArea182NodeType = []corpusEntry{
	{
		file:    "18.2-node-type/pattern_multi_label.gql",
		outcome: resolves,
		feature: "mandatory",
	},
}
