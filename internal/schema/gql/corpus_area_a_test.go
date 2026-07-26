package gql

// corpusAreaA holds the corpus entries for the graph type statement itself, the
// references that name a source graph type, and the identifier and nested-body
// grammar every other area builds on. One area variable per author so that two
// authors never edit the same Go file; corpusAreas fixes the directories these
// entries may live in.
var corpusAreaA = []corpusEntry{
	{
		file:    "12.6-graph-type-statement/nested_body.gql",
		outcome: resolves,
		feature: "mandatory",
	},
	{
		file:    "12.6-graph-type-statement/create_if_not_exists.gql",
		outcome: resolves,
		feature: "mandatory",
	},
	{
		file:    "12.6-graph-type-statement/create_or_replace.gql",
		outcome: resolves,
		feature: "mandatory",
	},
	{
		file:    "12.6-graph-type-statement/synonyms.gql",
		outcome: resolves,
		feature: "mandatory",
	},
	{
		file:    "12.6-graph-type-statement/synonyms_node_edge.gql",
		outcome: resolves,
		feature: "mandatory",
	},
	{
		file:    "12.6-graph-type-statement/delimited_identifiers.gql",
		outcome: resolves,
		feature: "mandatory",
	},
	{
		file:    "18.1-nested-graph-type/element_types.gql",
		outcome: resolves,
		feature: "mandatory",
	},
	{
		file:     "12.6-graph-type-statement/like_graph.gql",
		outcome:  unsupported,
		sentinel: ErrUnsupportedSource,
		feature:  "unsourced",
		bead:     "gqlc-0ri",
		reason:   "LIKE derives the type from a graph *instance*, so it cannot be answered without inspecting live data; declined permanently",
	},
	{
		file:     "12.6-graph-type-statement/copy_of_source.gql",
		outcome:  unsupported,
		sentinel: ErrUnsupportedSource,
		feature:  "mandatory",
		bead:     "gqlc-h9n.1",
		reason:   "COPY OF names a graph *type*, so it is a catalogue and multi-file scoping problem rather than a data-inspection one, and is solvable in principle; it shares LIKE's sentinel only because the element types are absent from this file either way",
	},
	{
		file:     "17-references/copy_of_graph_type_bare.gql",
		outcome:  unsupported,
		sentinel: ErrUnsupportedSource,
		feature:  "mandatory",
		bead:     "gqlc-h9n.1",
		reason:   "graphTypeReference alternative 1 with no schema parent — the same COPY OF gap, with only a bare graph type name",
	},
	{
		file:     "17-references/copy_of_param_graph_type.gql",
		outcome:  unsupported,
		sentinel: ErrUnsupportedSource,
		feature:  "mandatory",
		bead:     "gqlc-h9n.1",
		reason:   "graphTypeReference alternative 2 — a parameter reference where the graph type name goes",
	},
	{
		file:     "17-references/copy_of_qualified.gql",
		outcome:  unsupported,
		sentinel: ErrUnsupportedSource,
		feature:  "mandatory",
		bead:     "gqlc-h9n.1",
		reason:   "catalogObjectParentReference alternative 2 — a dotted-name parent with no schema reference",
	},
	{
		file:     "17-references/copy_of_absolute_bare.gql",
		outcome:  unsupported,
		sentinel: ErrUnsupportedSource,
		feature:  "mandatory",
		bead:     "gqlc-h9n.1",
		reason:   "absoluteCatalogSchemaReference alternative 1 — the bare SOLIDUS spelling; alternative 2 is the /a/b/gt form covered by the seed",
	},
	{
		file:     "17-references/copy_of_predefined_current.gql",
		outcome:  unsupported,
		sentinel: ErrUnsupportedSource,
		feature:  "mandatory",
		bead:     "gqlc-h9n.1",
		reason:   "predefinedSchemaReference alternative 3 — the bare PERIOD spelling of the current schema",
	},
	{
		file:     "17-references/copy_of_current_schema.gql",
		outcome:  unsupported,
		sentinel: ErrUnsupportedSource,
		feature:  "mandatory",
		bead:     "gqlc-h9n.1",
		reason:   "CURRENT_SCHEMA keyword form of predefinedSchemaReference — same COPY OF gap",
	},
	{
		file:     "17-references/copy_of_home_schema.gql",
		outcome:  unsupported,
		sentinel: ErrUnsupportedSource,
		feature:  "mandatory",
		bead:     "gqlc-h9n.1",
		reason:   "HOME_SCHEMA keyword form of predefinedSchemaReference — same COPY OF gap",
	},
	{
		file:     "17-references/copy_of_relative_up.gql",
		outcome:  unsupported,
		sentinel: ErrUnsupportedSource,
		feature:  "mandatory",
		bead:     "gqlc-h9n.1",
		reason:   "relativeDirectoryPath form of relativeCatalogSchemaReference — ../s reaches DOUBLE_PERIOD, and bare ../gt does not parse",
	},
	{
		file:     "17-references/copy_of_param_schema.gql",
		outcome:  unsupported,
		sentinel: ErrUnsupportedSource,
		feature:  "mandatory",
		bead:     "gqlc-h9n.1",
		reason:   "schemaReference alternative 3 — a parameter reference where the schema name goes, distinct from graphTypeReference alternative 2",
	},
}

// semanticAreaA holds this area's semantic cases: files above that resolve to a
// model known to be wrong. Empty, and declared anyway so that recording one is an
// edit here rather than to the shared corpus_test.go. If the linter reports this
// unused, corpusAreas has lost its `semantic:` entry — TestCorpusManifest says so
// too, by area name. Wire it back rather than deleting this, which is the only
// thing standing between an author and that edit. Keep the []semanticCase{}
// spelling: the manifest requires non-nil, so `var x []semanticCase` reads as a
// lost wiring.
var semanticAreaA = []semanticCase{}
