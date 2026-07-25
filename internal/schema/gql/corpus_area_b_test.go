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
}
