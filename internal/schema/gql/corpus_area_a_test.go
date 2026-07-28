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
		sentinel: ErrLikeGraphSource,
		feature:  "GG04",
		bead:     "gqlc-0ri",
		reason:   "LIKE takes a graphExpression, which reaches CURRENT_GRAPH and binding variables — session state a static generator has none of, so no catalogue would make this resolvable; declined permanently",
	},
	{
		file:     "12.6-graph-type-statement/copy_of_source.gql",
		outcome:  unsupported,
		sentinel: ErrCopyOfSource,
		feature:  "mandatory",
		bead:     "gqlc-h9n.1",
		reason:   "COPY OF names a graph *type* in the catalogue, which is statically resolvable and merely unimplemented — the opposite of LIKE's position, which is why the two stopped sharing a sentinel in gqlc-h9n.12",
	},
	{
		file:     "17-references/copy_of_graph_type_bare.gql",
		outcome:  unsupported,
		sentinel: ErrCopyOfSource,
		feature:  "mandatory",
		bead:     "gqlc-h9n.1",
		reason:   "graphTypeReference alternative 1 with no schema parent — the same COPY OF gap, with only a bare graph type name",
	},
	{
		file:     "17-references/copy_of_param_graph_type.gql",
		outcome:  unsupported,
		sentinel: ErrCopyOfSource,
		feature:  "mandatory",
		bead:     "gqlc-h9n.1",
		reason:   "graphTypeReference alternative 2 — a parameter reference where the graph type name goes",
	},
	{
		file:     "17-references/copy_of_qualified.gql",
		outcome:  unsupported,
		sentinel: ErrCopyOfSource,
		feature:  "mandatory",
		bead:     "gqlc-h9n.1",
		reason:   "catalogObjectParentReference alternative 2 — a dotted-name parent with no schema reference",
	},
	{
		file:     "17-references/copy_of_absolute_bare.gql",
		outcome:  unsupported,
		sentinel: ErrCopyOfSource,
		feature:  "mandatory",
		bead:     "gqlc-h9n.1",
		reason:   "absoluteCatalogSchemaReference alternative 1 — the bare SOLIDUS spelling; alternative 2 is the /a/b/gt form covered by the seed",
	},
	{
		file:     "17-references/copy_of_predefined_current.gql",
		outcome:  unsupported,
		sentinel: ErrCopyOfSource,
		feature:  "mandatory",
		bead:     "gqlc-h9n.1",
		reason:   "predefinedSchemaReference alternative 3 — the bare PERIOD spelling of the current schema",
	},
	{
		file:     "17-references/copy_of_current_schema.gql",
		outcome:  unsupported,
		sentinel: ErrCopyOfSource,
		feature:  "mandatory",
		bead:     "gqlc-h9n.1",
		reason:   "CURRENT_SCHEMA keyword form of predefinedSchemaReference — same COPY OF gap",
	},
	{
		file:     "17-references/copy_of_home_schema.gql",
		outcome:  unsupported,
		sentinel: ErrCopyOfSource,
		feature:  "mandatory",
		bead:     "gqlc-h9n.1",
		reason:   "HOME_SCHEMA keyword form of predefinedSchemaReference — same COPY OF gap",
	},
	{
		file:     "17-references/copy_of_relative_up.gql",
		outcome:  unsupported,
		sentinel: ErrCopyOfSource,
		feature:  "mandatory",
		bead:     "gqlc-h9n.1",
		reason:   "relativeDirectoryPath form of relativeCatalogSchemaReference — ../s reaches DOUBLE_PERIOD, and bare ../gt does not parse",
	},
	{
		file:     "17-references/copy_of_param_schema.gql",
		outcome:  unsupported,
		sentinel: ErrCopyOfSource,
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
