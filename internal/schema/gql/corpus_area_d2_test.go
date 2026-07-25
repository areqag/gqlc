package gql

// corpusAreaD2 holds the corpus entries for constructed, reference and immaterial
// value types — lists, records, graph/node/edge/binding-table references, paths and
// the null and empty types. It shares 18.9-value-type/ with area D1, which takes
// the predefined scalars; the two are disjoint by file, not by directory. One area
// variable per author so that two authors never edit the same Go file; corpusAreas
// fixes the directories these entries may live in.
var corpusAreaD2 = []corpusEntry{
	{
		file:     "18.9-value-type/constructed_list_angle.gql",
		outcome:  unsupported,
		sentinel: ErrUnsupportedType,
		feature:  "mandatory",
		bead:     "gqlc-h9n.5",
		reason:   "LIST/ARRAY needs a parameterised type model, which gqlc does not yet have; the angle-bracket spelling is valueType alt 3 (listValueTypeAlt1)",
	},
	{
		file:     "18.9-value-type/constructed_list_postfix.gql",
		outcome:  unsupported,
		sentinel: ErrUnsupportedType,
		feature:  "mandatory",
		bead:     "gqlc-h9n.5",
		reason:   "LIST/ARRAY needs a parameterised type model; STRING LIST[5] is valueType alt 4 (listValueTypeAlt2), and its [5] quantifier is the only reachable carrier of LEFT_BRACKET and RIGHT_BRACKET from a CREATE GRAPH TYPE body",
	},
	{
		file:     "18.9-value-type/constructed_list_bare.gql",
		outcome:  unsupported,
		sentinel: ErrUnsupportedType,
		feature:  "mandatory",
		bead:     "gqlc-h9n.5",
		reason:   "LIST/ARRAY needs a parameterised type model; the bare spelling with no element type is valueType alt 5 (listValueTypeAlt3)",
	},
	{
		file:     "18.9-value-type/constructed_record_bare.gql",
		outcome:  unsupported,
		sentinel: ErrUnsupportedType,
		feature:  "mandatory",
		bead:     "gqlc-h9n.6",
		reason:   "recordType is one of the type families gqlc declines; bare RECORD is recordType alt 1",
	},
	{
		file:     "18.9-value-type/constructed_record_fields.gql",
		outcome:  unsupported,
		sentinel: ErrUnsupportedType,
		feature:  "mandatory",
		bead:     "gqlc-h9n.6",
		reason:   "recordType is one of the type families gqlc declines; RECORD { f :: STRING } is recordType alt 2 and also brings the field-type grammar in",
	},
	{
		file:     "18.9-value-type/constructed_dyn_open.gql",
		outcome:  unsupported,
		sentinel: ErrUnsupportedType,
		feature:  "mandatory",
		bead:     "gqlc-h9n.6",
		reason:   "open dynamic union types are one of the type families gqlc declines; ANY VALUE is valueType alt 7 (openDynamicUnionTypeLabel)",
	},
	{
		file:     "18.9-value-type/constructed_dyn_property_value.gql",
		outcome:  unsupported,
		sentinel: ErrUnsupportedType,
		feature:  "mandatory",
		bead:     "gqlc-h9n.6",
		reason:   "the dynamic property-value type is one of the type families gqlc declines; PROPERTY VALUE is valueType alt 8 (dynamicPropertyValueTypeLabel)",
	},
	{
		file:     "18.9-value-type/constructed_dyn_closed_union.gql",
		outcome:  unsupported,
		sentinel: ErrUnsupportedType,
		feature:  "mandatory",
		bead:     "gqlc-h9n.6",
		reason:   "closed dynamic union types are one of the type families gqlc declines; ANY VALUE<STRING | INT> discharges valueType alts 9 and 10 in one parse, because the inner STRING | INT is itself a nested closedDynamicUnionTypeAtl2",
	},
	{
		file:     "18.9-value-type/constructed_null.gql",
		outcome:  unsupported,
		sentinel: ErrUnsupportedType,
		feature:  "mandatory",
		bead:     "gqlc-h9n.6",
		reason:   "immaterial value types are one of the type families gqlc declines; bare NULL is nullType",
	},
	{
		file:     "18.9-value-type/constructed_null_not_null.gql",
		outcome:  unsupported,
		sentinel: ErrUnsupportedType,
		feature:  "mandatory",
		bead:     "gqlc-h9n.6",
		reason:   "immaterial value types are one of the type families gqlc declines; NULL NOT NULL is emptyType alt 1 (the notNull is mandatory here, unlike the rest of the value-type grammar)",
	},
	{
		file:     "18.9-value-type/constructed_nothing.gql",
		outcome:  unsupported,
		sentinel: ErrUnsupportedType,
		feature:  "mandatory",
		bead:     "gqlc-h9n.6",
		reason:   "immaterial value types are one of the type families gqlc declines; NOTHING is emptyType alt 2",
	},
	{
		file:     "18.9-value-type/constructed_path.gql",
		outcome:  unsupported,
		sentinel: ErrUnsupportedType,
		feature:  "mandatory",
		bead:     "gqlc-h9n.6",
		reason:   "path value types are one of the type families gqlc declines; PATH is pathValueType",
	},
	{
		file:     "18.9-value-type/constructed_list_array_synonym.gql",
		outcome:  unsupported,
		sentinel: ErrUnsupportedType,
		feature:  "mandatory",
		bead:     "gqlc-h9n.5",
		reason:   "ARRAY is a synonym of LIST throughout, so this file discharges the same alternative as LIST<STRING>; it exists to enter the ARRAY token, which nothing in the LIST spellings does",
	},
	{
		file:     "18.9-value-type/constructed_graph_open.gql",
		outcome:  unsupported,
		sentinel: ErrUnsupportedType,
		feature:  "mandatory",
		bead:     "gqlc-h9n.6",
		reason:   "graph reference value types are one of the type families gqlc declines; ANY GRAPH is openGraphReferenceValueType",
	},
	{
		file:     "18.9-value-type/constructed_graph_closed.gql",
		outcome:  unsupported,
		sentinel: ErrUnsupportedType,
		feature:  "mandatory",
		bead:     "gqlc-h9n.6",
		reason:   "graph reference value types are one of the type families gqlc declines; GRAPH { (:X) } is closedGraphReferenceValueType, which nests a whole elementTypeSpecification inside a property type at depth 2 (correctly ignored by the silent-drop guard)",
	},
	{
		file:     "18.9-value-type/constructed_node_open.gql",
		outcome:  unsupported,
		sentinel: ErrUnsupportedType,
		feature:  "mandatory",
		bead:     "gqlc-h9n.6",
		reason:   "node reference value types are one of the type families gqlc declines; ANY NODE is openNodeReferenceValueType",
	},
	{
		file:     "18.9-value-type/constructed_node_closed.gql",
		outcome:  unsupported,
		sentinel: ErrUnsupportedType,
		feature:  "mandatory",
		bead:     "gqlc-h9n.6",
		reason:   "node reference value types are one of the type families gqlc declines; the bare (:X) inside a property type is closedNodeReferenceValueType, which references nodeTypeSpecification directly (a NODE prefix is a syntax error)",
	},
	{
		file:     "18.9-value-type/constructed_edge_open.gql",
		outcome:  unsupported,
		sentinel: ErrUnsupportedType,
		feature:  "mandatory",
		bead:     "gqlc-h9n.6",
		reason:   "edge reference value types are one of the type families gqlc declines; ANY EDGE is openEdgeReferenceValueType",
	},
	{
		file:     "18.9-value-type/constructed_edge_closed.gql",
		outcome:  unsupported,
		sentinel: ErrUnsupportedType,
		feature:  "mandatory",
		bead:     "gqlc-h9n.6",
		reason:   "edge reference value types are one of the type families gqlc declines; DIRECTED EDGE R (:X)-[:R]->(:Y) inside a property type is closedEdgeReferenceValueType",
	},
	{
		file:     "18.8-binding-table-type/binding_table.gql",
		outcome:  unsupported,
		sentinel: ErrUnsupportedType,
		feature:  "mandatory",
		bead:     "gqlc-h9n.6",
		reason:   "binding table types are one of the type families gqlc declines; BINDING TABLE { id :: STRING } is bindingTableReferenceValueType wrapping bindingTableType, and it brings BINDING and TABLE tokens in",
	},
}

// semanticAreaD2 holds this area's semantic cases: files above that resolve to a
// model known to be wrong. Empty, and declared anyway so that recording one is an
// edit here rather than to the shared corpus_test.go. If the linter reports this
// unused, corpusAreas has lost its `semantic:` entry — wire it back rather than
// deleting this, which is the only thing standing between an author and that edit.
var semanticAreaD2 = []semanticCase{}
