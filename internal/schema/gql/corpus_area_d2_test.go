package gql

// corpusAreaD2 holds the corpus entries for constructed, reference and immaterial
// value types — lists, records, graph/node/edge/binding-table references, paths and
// the null and empty types. It shares 18.9-value-type/ with area D1, which takes
// the predefined scalars; the two are disjoint by file, not by directory. One area
// variable per author so that two authors never edit the same Go file; corpusAreas
// fixes the directories these entries may live in.
var corpusAreaD2 = []corpusEntry{}
