package gql

// corpusAreaC holds the corpus entries for edge types. One area variable per
// author so that two authors never edit the same Go file; corpusAreas fixes the
// directories these entries may live in.
var corpusAreaC = []corpusEntry{
	{
		file:    "18.3-edge-type/pattern_directed.gql",
		outcome: resolves,
		feature: "mandatory",
	},
	{
		file:     "18.3-edge-type/pattern_undirected.gql",
		outcome:  unsupported,
		sentinel: ErrUndirectedEdge,
		feature:  "mandatory",
		bead:     "gqlc-h9n.3",
		reason:   "an undirected arc has no canonical source -> target identity, which EdgeKey requires",
	},
	{
		file:    "18.3-edge-type/kind_undirected_arc_directed.gql",
		outcome: resolves,
		feature: "mandatory",
		bead:    "gqlc-h9n.3",
		reason:  "the UNDIRECTED kind is discarded and the edge resolves as if it were DIRECTED",
	},
	{
		file:    "18.3-edge-type/phrase_form.gql",
		outcome: resolves,
		feature: "mandatory",
	},
	{
		file:    "18.3-edge-type/pattern_pointing_left.gql",
		outcome: resolves,
		feature: "mandatory",
	},
	{
		file:    "18.3-edge-type/phrase_form_to.gql",
		outcome: resolves,
		feature: "mandatory",
	},
	{
		file:     "18.3-edge-type/phrase_form_undirected_connector.gql",
		outcome:  unsupported,
		sentinel: ErrUndirectedEdge,
		feature:  "mandatory",
		bead:     "gqlc-h9n.3",
		reason:   "the `~` connector gives endpointPairUndirected, whose two ends have no canonical source -> target identity — the same reason pattern_undirected.gql is rejected, reached through the phrase door",
	},
	{
		file:    "18.3-edge-type/phrase_form_left_arrow.gql",
		outcome: resolves,
		feature: "mandatory",
	},
	{
		file:    "18.3-edge-type/phrase_form_no_name.gql",
		outcome: resolves,
		feature: "mandatory",
	},
	{
		file:     "18.3-edge-type/pattern_properties_only.gql",
		outcome:  unsupported,
		sentinel: ErrUnnamedEdgeType,
		feature:  "mandatory",
		bead:     "gqlc-0ri",
		reason:   "an edge type with property types but no label has no label-set key; deviation-audit bead",
	},
	{
		file:     "18.3-edge-type/key_label_set.gql",
		outcome:  unsupported,
		sentinel: ErrLabelImplication,
		feature:  "mandatory",
		bead:     "gqlc-h9n.9",
		reason:   `edgeTypeKeyLabelSet ("=>" label implication) is rejected; supporting it needs a listener addition that folds implied labels into the key`,
	},
}

// semanticAreaC holds this area's semantic cases: files above that resolve to a
// model known to be wrong.
var semanticAreaC = []semanticCase{
	{
		file: "18.3-edge-type/kind_undirected_arc_directed.gql",
		bead: "gqlc-h9n.3",
		why:  "an UNDIRECTED edge kind on a directed arc resolves to the same EdgeType as DIRECTED, because EdgeType has no undirectedness field; the corpus cannot detect the reinterpretation",
		// Both halves, because each alone is a construct the corpus covers elsewhere
		// and resolves correctly — it is the combination that is the case.
		spelling: "UNDIRECTED EDGE Mentions (a) -[:MENTIONS]-> (b)",
	},
}
