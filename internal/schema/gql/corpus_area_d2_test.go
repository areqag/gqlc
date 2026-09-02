package gql_test

import "github.com/areqag/gqlc/internal/schema/gql"

// corpusAreaD2 holds the corpus entries for constructed, reference and immaterial
// value types — lists, records, graph/node/edge/binding-table references, paths and
// the null and empty types. It shares 18.9-value-type/ with area D1, which takes
// the predefined scalars; the two are disjoint by file, not by directory. One area
// variable per author so that two authors never edit the same Go file; corpusAreas
// fixes the directories these entries may live in.
var corpusAreaD2 = []corpusEntry{
	{
		file:    "18.9-value-type/constructed_list_angle.gql",
		outcome: resolves,
		feature: "mandatory",
	},
	{
		file:    "18.9-value-type/constructed_list_postfix.gql",
		outcome: resolves,
		feature: "mandatory",
	},
	{
		file:    "18.9-value-type/constructed_list_bare.gql",
		outcome: resolves,
		feature: "mandatory",
	},
	{
		file:    "18.9-value-type/constructed_record_bare.gql",
		outcome: resolves,
		feature: "mandatory",
	},
	{
		file:    "18.9-value-type/constructed_record_fields.gql",
		outcome: resolves,
		feature: "mandatory",
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
		file:    "18.9-value-type/constructed_dyn_closed_union.gql",
		outcome: resolves,
		feature: "mandatory",
	},
	{
		file:    "18.9-value-type/constructed_dyn_closed_union_bare.gql",
		outcome: resolves,
		feature: "mandatory",
	},
	{
		file:     "18.9-value-type/constructed_null.gql",
		outcome:  unsupported,
		sentinel: gql.ErrImmaterialValueType,
		feature:  "mandatory",
		bead:     "gqlc-0ri",
		reason:   "NULL admits only null, which schema.Property.Nullable already records, so the type carries nothing the model does not have; declined permanently in ADR 0019. Bare NULL is nullType",
	},
	{
		file:     "18.9-value-type/constructed_null_not_null.gql",
		outcome:  unsupported,
		sentinel: gql.ErrImmaterialValueType,
		feature:  "mandatory",
		bead:     "gqlc-0ri",
		reason:   "the empty type admits no value at all, so a property of this type could never be written or read; declined permanently in ADR 0019. NULL NOT NULL is emptyType alt 1 (the notNull is mandatory here, unlike the rest of the value-type grammar)",
	},
	{
		file:     "18.9-value-type/constructed_nothing.gql",
		outcome:  unsupported,
		sentinel: gql.ErrImmaterialValueType,
		feature:  "mandatory",
		bead:     "gqlc-0ri",
		reason:   "the empty type again, and the reason is the same as NULL NOT NULL's; declined permanently in ADR 0019. NOTHING is emptyType alt 2",
	},
	{
		file:     "18.9-value-type/constructed_path.gql",
		outcome:  unsupported,
		sentinel: gql.ErrPathValueType,
		feature:  "mandatory",
		bead:     "gqlc-0ri",
		reason:   "a path is a traversal a query produces, not a value an element stores, so no backend or model change reaches it; declined permanently in ADR 0019. PATH is pathValueType",
	},
	{
		file:    "18.9-value-type/constructed_list_array_synonym.gql",
		outcome: resolves,
		feature: "mandatory",
	},
	{
		file:     "18.9-value-type/constructed_graph_open.gql",
		outcome:  unsupported,
		sentinel: gql.ErrReferenceValueType,
		feature:  "mandatory",
		bead:     "gqlc-0ri",
		reason:   "a reference is a handle into a graph, and a property holding one would be a relationship no traversal can follow — gqlc has Schema.Edges for exactly that; declined permanently in ADR 0019. ANY GRAPH is openGraphReferenceValueType",
	},
	{
		file:     "18.9-value-type/constructed_graph_closed.gql",
		outcome:  unsupported,
		sentinel: gql.ErrReferenceValueType,
		feature:  "mandatory",
		bead:     "gqlc-0ri",
		reason:   "same permanent decline as ANY GRAPH (ADR 0019); GRAPH { (:X) } is closedGraphReferenceValueType, which nests a whole elementTypeSpecification inside a property type at depth 2 (correctly ignored by the silent-drop guard)",
	},
	{
		file:     "18.9-value-type/constructed_node_open.gql",
		outcome:  unsupported,
		sentinel: gql.ErrReferenceValueType,
		feature:  "mandatory",
		bead:     "gqlc-0ri",
		reason:   "same permanent decline as the graph references (ADR 0019); ANY NODE is openNodeReferenceValueType",
	},
	{
		file:     "18.9-value-type/constructed_node_closed.gql",
		outcome:  unsupported,
		sentinel: gql.ErrReferenceValueType,
		feature:  "mandatory",
		bead:     "gqlc-0ri",
		reason:   "same permanent decline as the graph references (ADR 0019); the bare (:X) inside a property type is closedNodeReferenceValueType, which references nodeTypeSpecification directly (a NODE prefix is a syntax error)",
	},
	{
		file:     "18.9-value-type/constructed_edge_open.gql",
		outcome:  unsupported,
		sentinel: gql.ErrReferenceValueType,
		feature:  "mandatory",
		bead:     "gqlc-0ri",
		reason:   "same permanent decline as the graph references (ADR 0019); ANY EDGE is openEdgeReferenceValueType",
	},
	{
		file:     "18.9-value-type/constructed_edge_closed.gql",
		outcome:  unsupported,
		sentinel: gql.ErrReferenceValueType,
		feature:  "mandatory",
		bead:     "gqlc-0ri",
		reason:   "same permanent decline as the graph references (ADR 0019), and the clearest case of it: an edge as a property is an edge the graph cannot traverse. DIRECTED EDGE R (:X)-[:R]->(:Y) inside a property type is closedEdgeReferenceValueType",
	},
	{
		file:     "18.8-binding-table-type/binding_table.gql",
		outcome:  unsupported,
		sentinel: gql.ErrReferenceValueType,
		feature:  "mandatory",
		bead:     "gqlc-0ri",
		reason:   "a binding table is a query result rather than stored data, which is why it shares the reference sentinel despite not naming an element (ADR 0019); BINDING TABLE { id :: STRING } is bindingTableReferenceValueType wrapping bindingTableType, and it brings BINDING and TABLE tokens in",
	},
	{
		file:     "18.8-binding-table-type/table_no_binding.gql",
		outcome:  unsupported,
		sentinel: gql.ErrReferenceValueType,
		feature:  "mandatory",
		bead:     "gqlc-0ri",
		reason:   "the BINDING-less spelling of the same type (GQL.g4:1713), declined for the reason binding_table.gql is; the keyword is optional rather than distinguishing, so nothing about the decline turns on it",
	},
	{
		file:    "18.9-value-type/constructed_record_two_fields.gql",
		outcome: resolves,
		feature: "mandatory",
	},
	{
		file:    "18.9-value-type/constructed_record_no_fields.gql",
		outcome: resolves,
		feature: "mandatory",
	},
	{
		file:    "18.9-value-type/constructed_record_keyword_elided.gql",
		outcome: resolves,
		feature: "mandatory",
	},
	{
		file:    "18.9-value-type/constructed_record_field_typed_elided.gql",
		outcome: resolves,
		feature: "mandatory",
	},
	{
		file:     "18.9-value-type/constructed_record_field_name_repeated.gql",
		outcome:  unsupported,
		sentinel: gql.ErrDuplicateFieldName,
		feature:  "mandatory",
		bead:     "gqlc-tlbo",
		reason:   "ADR 0030's reading of a repeated property name applied one level down, for the same reason and with the same provisional standing: <field type list> states no uniqueness constraint either, so which reading the standard intends is Syntax Rules prose gqlc-lir declined to buy, and rejection is the interim posture because keeping one field and discarding the other is the only reading that can be wrong silently. New with gqlc-h9n.33 — while a record was declined at its outermost context its fields were never read, so a duplicate among them was unreachable",
	},
	{
		file:     "18.9-value-type/constructed_graph_open_property.gql",
		outcome:  unsupported,
		sentinel: gql.ErrReferenceValueType,
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
