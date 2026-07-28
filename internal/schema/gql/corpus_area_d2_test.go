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
		sentinel: ErrRecordValueType,
		feature:  "mandatory",
		bead:     "gqlc-h9n.33",
		reason:   "a record is structured, and graph.PropertyType is a flat enum with nowhere to put the fields (ADR 0019: unimplemented, not declined — gqlc-h9n.33). Bare RECORD is recordType alt 1",
	},
	{
		file:     "18.9-value-type/constructed_record_fields.gql",
		outcome:  unsupported,
		sentinel: ErrRecordValueType,
		feature:  "mandatory",
		bead:     "gqlc-h9n.33",
		reason:   "the same missing field-carrying type as bare RECORD (ADR 0019, gqlc-h9n.33); RECORD { f :: STRING } is recordType alt 2 and also brings the field-type grammar in",
	},
	{
		file:    "18.9-value-type/constructed_dyn_open.gql",
		outcome: resolves,
		feature: "mandatory",
	},
	{
		file:    "18.9-value-type/constructed_dyn_property_value.gql",
		outcome: resolves,
		feature: "mandatory",
	},
	{
		file:     "18.9-value-type/constructed_dyn_closed_union.gql",
		outcome:  unsupported,
		sentinel: ErrDynamicUnionType,
		feature:  "mandatory",
		bead:     "gqlc-h9n.33",
		reason:   "a closed union needs the enum to carry its members, which is gqlc-h9n.33's blocker rather than gqlc-h9n.34's, though ADR 0019 keeps both halves under one ISO-named sentinel. ANY VALUE<STRING | INT> is valueType alt 9 (closedDynamicUnionTypeAtl1) and discharges that alone: declineValueType reads the outermost context and does not descend, so the nested STRING | INT is never dispatched on. Alt 10 is constructed_dyn_closed_union_bare.gql's",
	},
	{
		file:     "18.9-value-type/constructed_dyn_closed_union_bare.gql",
		outcome:  unsupported,
		sentinel: ErrDynamicUnionType,
		feature:  "mandatory",
		bead:     "gqlc-h9n.33",
		reason:   "the bare-bar spelling of the same decline, valueType alt 10 (closedDynamicUnionTypeAtl2). The angle-bracket file nests an Atl2 inside its Atl1 and was read as covering both, but declineValueType does not descend — a panic in the Atl2 arm never fired across the suite — so deleting that arm left nothing red. Without this file the spelling still rejects, on the bare ErrUnsupportedType, and the family ADR 0019 assigns it goes unasserted",
	},
	{
		file:     "18.9-value-type/constructed_null.gql",
		outcome:  unsupported,
		sentinel: ErrImmaterialValueType,
		feature:  "mandatory",
		bead:     "gqlc-0ri",
		reason:   "NULL admits only null, which schema.Property.Nullable already records, so the type carries nothing the model does not have; declined permanently in ADR 0019. Bare NULL is nullType",
	},
	{
		file:     "18.9-value-type/constructed_null_not_null.gql",
		outcome:  unsupported,
		sentinel: ErrImmaterialValueType,
		feature:  "mandatory",
		bead:     "gqlc-0ri",
		reason:   "the empty type admits no value at all, so a property of this type could never be written or read; declined permanently in ADR 0019. NULL NOT NULL is emptyType alt 1 (the notNull is mandatory here, unlike the rest of the value-type grammar)",
	},
	{
		file:     "18.9-value-type/constructed_nothing.gql",
		outcome:  unsupported,
		sentinel: ErrImmaterialValueType,
		feature:  "mandatory",
		bead:     "gqlc-0ri",
		reason:   "the empty type again, and the reason is the same as NULL NOT NULL's; declined permanently in ADR 0019. NOTHING is emptyType alt 2",
	},
	{
		file:     "18.9-value-type/constructed_path.gql",
		outcome:  unsupported,
		sentinel: ErrPathValueType,
		feature:  "mandatory",
		bead:     "gqlc-0ri",
		reason:   "a path is a traversal a query produces, not a value an element stores, so no backend or model change reaches it; declined permanently in ADR 0019. PATH is pathValueType",
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
		sentinel: ErrReferenceValueType,
		feature:  "mandatory",
		bead:     "gqlc-0ri",
		reason:   "a reference is a handle into a graph, and a property holding one would be a relationship no traversal can follow — gqlc has Schema.Edges for exactly that; declined permanently in ADR 0019. ANY GRAPH is openGraphReferenceValueType",
	},
	{
		file:     "18.9-value-type/constructed_graph_closed.gql",
		outcome:  unsupported,
		sentinel: ErrReferenceValueType,
		feature:  "mandatory",
		bead:     "gqlc-0ri",
		reason:   "same permanent decline as ANY GRAPH (ADR 0019); GRAPH { (:X) } is closedGraphReferenceValueType, which nests a whole elementTypeSpecification inside a property type at depth 2 (correctly ignored by the silent-drop guard)",
	},
	{
		file:     "18.9-value-type/constructed_node_open.gql",
		outcome:  unsupported,
		sentinel: ErrReferenceValueType,
		feature:  "mandatory",
		bead:     "gqlc-0ri",
		reason:   "same permanent decline as the graph references (ADR 0019); ANY NODE is openNodeReferenceValueType",
	},
	{
		file:     "18.9-value-type/constructed_node_closed.gql",
		outcome:  unsupported,
		sentinel: ErrReferenceValueType,
		feature:  "mandatory",
		bead:     "gqlc-0ri",
		reason:   "same permanent decline as the graph references (ADR 0019); the bare (:X) inside a property type is closedNodeReferenceValueType, which references nodeTypeSpecification directly (a NODE prefix is a syntax error)",
	},
	{
		file:     "18.9-value-type/constructed_edge_open.gql",
		outcome:  unsupported,
		sentinel: ErrReferenceValueType,
		feature:  "mandatory",
		bead:     "gqlc-0ri",
		reason:   "same permanent decline as the graph references (ADR 0019); ANY EDGE is openEdgeReferenceValueType",
	},
	{
		file:     "18.9-value-type/constructed_edge_closed.gql",
		outcome:  unsupported,
		sentinel: ErrReferenceValueType,
		feature:  "mandatory",
		bead:     "gqlc-0ri",
		reason:   "same permanent decline as the graph references (ADR 0019), and the clearest case of it: an edge as a property is an edge the graph cannot traverse. DIRECTED EDGE R (:X)-[:R]->(:Y) inside a property type is closedEdgeReferenceValueType",
	},
	{
		file:     "18.8-binding-table-type/binding_table.gql",
		outcome:  unsupported,
		sentinel: ErrReferenceValueType,
		feature:  "mandatory",
		bead:     "gqlc-0ri",
		reason:   "a binding table is a query result rather than stored data, which is why it shares the reference sentinel despite not naming an element (ADR 0019); BINDING TABLE { id :: STRING } is bindingTableReferenceValueType wrapping bindingTableType, and it brings BINDING and TABLE tokens in",
	},
	{
		file:     "18.8-binding-table-type/table_no_binding.gql",
		outcome:  unsupported,
		sentinel: ErrReferenceValueType,
		feature:  "mandatory",
		bead:     "gqlc-0ri",
		reason:   "the BINDING-less spelling of the same type (GQL.g4:1713), declined for the reason binding_table.gql is; the keyword is optional rather than distinguishing, so nothing about the decline turns on it",
	},
	{
		file:     "18.9-value-type/constructed_record_two_fields.gql",
		outcome:  unsupported,
		sentinel: ErrRecordValueType,
		feature:  "mandatory",
		bead:     "gqlc-h9n.33",
		reason:   "a record listing two fields, which is fieldTypeList's repetition rather than a second construct; declined where every record is, and the field count is exactly what gqlc-h9n.33 has nowhere to put",
	},
	{
		file:     "18.9-value-type/constructed_record_no_fields.gql",
		outcome:  unsupported,
		sentinel: ErrRecordValueType,
		feature:  "mandatory",
		bead:     "gqlc-h9n.33",
		reason:   "`RECORD {}` is the one record spelling that would not need graph.PropertyType to carry members, and is declined anyway: there is no record type to resolve it to, empty or otherwise, and a special path for it would land a construct codegen has no case for",
	},
	{
		file:     "18.9-value-type/constructed_record_keyword_elided.gql",
		outcome:  unsupported,
		sentinel: ErrRecordValueType,
		feature:  "mandatory",
		bead:     "gqlc-h9n.33",
		reason:   "recordType alternative 2 with RECORD dropped — the brace selects the alternative, not the keyword, so a braced field list where a value type belongs is a record rather than a property block",
	},
	{
		file:     "18.9-value-type/constructed_record_field_typed_elided.gql",
		outcome:  unsupported,
		sentinel: ErrRecordValueType,
		feature:  "mandatory",
		bead:     "gqlc-h9n.33",
		reason:   "the `::`-elided field spelling, and the file clause 18.10 cannot hold because a field type has no surface syntax outside a record or binding table. It reports the record's sentinel and not the field's: declineValueType reads the outermost value type and does not descend, which ADR 0019 argues for",
	},
	{
		file:     "18.9-value-type/constructed_graph_open_property.gql",
		outcome:  unsupported,
		sentinel: ErrReferenceValueType,
		feature:  "mandatory",
		bead:     "gqlc-0ri",
		reason:   "`ANY PROPERTY GRAPH` takes openGraphReferenceValueType's PROPERTY, which both existing graph references elide; same permanent decline as the other references (ADR 0019)",
	},
	{
		file:    "18.9-value-type/constructed_dyn_any_bare.gql",
		outcome: resolves,
		feature: "mandatory",
	},
}

// semanticAreaD2 holds this area's semantic cases: files above that resolve to a
// model known to be wrong. Empty, and declared anyway so that recording one is an
// edit here rather than to the shared corpus_test.go. If the linter reports this
// unused, corpusAreas has lost its `semantic:` entry — TestCorpusManifest says so
// too, by area name. Wire it back rather than deleting this, which is the only
// thing standing between an author and that edit. Keep the []semanticCase{}
// spelling: the manifest requires non-nil, so `var x []semanticCase` reads as a
// lost wiring.
var semanticAreaD2 = []semanticCase{}
