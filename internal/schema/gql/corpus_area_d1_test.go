package gql

// corpusAreaD1 holds the corpus entries for predefined scalar value types —
// booleans, character and byte strings, exact and approximate numerics, temporal
// types and their qualifiers. It shares 18.9-value-type/ with area D2, which takes
// the constructed and reference types; the two are disjoint by file, not by
// directory. One area variable per author so that two authors never edit the same
// Go file; corpusAreas fixes the directories these entries may live in.
var corpusAreaD1 = []corpusEntry{}
