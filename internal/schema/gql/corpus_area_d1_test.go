package gql

// corpusAreaD1 holds the corpus entries for predefined scalar value types —
// booleans, character and byte strings, exact and approximate numerics, temporal
// types and their qualifiers. It shares 18.9-value-type/ with area D2, which takes
// the constructed and reference types; the two are disjoint by file, not by
// directory. One area variable per author so that two authors never edit the same
// Go file; corpusAreas fixes the directories these entries may live in.
var corpusAreaD1 = []corpusEntry{
	{
		file:    "18.9-value-type/scalar_decimal_precision_scale.gql",
		outcome: resolves,
		feature: "mandatory",
		bead:    "gqlc-h9n.16",
		reason:  "the precision and scale are discarded, so DECIMAL(10,2) resolves to a PropertyType byte-identical to bare DECIMAL; the two are indistinguishable downstream and codegen cannot emit a width",
	},
}

// semanticAreaD1 holds this area's semantic cases: files above that resolve to a
// model known to be wrong.
var semanticAreaD1 = []semanticCase{
	{
		file: "18.9-value-type/scalar_decimal_precision_scale.gql",
		bead: "gqlc-h9n.16",
		why:  "DECIMAL(10,2) resolves to the same PropertyType as bare DECIMAL, because PropertyType has no length field; the discarded precision and scale are unrecoverable downstream",
	},
}
