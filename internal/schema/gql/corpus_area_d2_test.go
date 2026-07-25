package gql

// corpusAreaD2 holds the corpus entries for constructed, reference and immaterial
// value types — lists, records, graph/node/edge/binding-table references, paths and
// the null and empty types. It shares 18.9-value-type/ with area D1, which takes
// the predefined scalars; the two are disjoint by file, not by directory. One area
// variable per author so that two authors never edit the same Go file; corpusAreas
// fixes the directories these entries may live in.
var corpusAreaD2 = []corpusEntry{}

// semanticAreaD2 holds this area's semantic cases: files above that resolve to a
// model known to be wrong. Empty, and declared anyway so that recording one is an
// edit here rather than to the shared corpus_test.go. If the linter reports this
// unused, corpusAreas has lost its `semantic:` entry — TestCorpusManifest says so
// too, by area name. Wire it back rather than deleting this, which is the only
// thing standing between an author and that edit. Keep the []semanticCase{}
// spelling: the manifest requires non-nil, so `var x []semanticCase` reads as a
// lost wiring.
var semanticAreaD2 = []semanticCase{}
