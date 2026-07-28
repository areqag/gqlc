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
		file:     "18.3-edge-type/kind_undirected_arc_directed.gql",
		outcome:  unsupported,
		sentinel: ErrEdgeKindArcMismatch,
		feature:  "mandatory",
		bead:     "gqlc-h9n.3",
		reason:   "the declared edgeKind (UNDIRECTED) contradicts the arc direction (->); rejected rather than silently reinterpreted as DIRECTED",
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
		sentinel: ErrEdgeKindArcMismatch,
		feature:  "mandatory",
		bead:     "gqlc-h9n.3",
		reason:   "kind=DIRECTED with the `~` (undirected) connector is a contradiction: the mismatch fires before the accepted-subset check that would otherwise report ErrUndirectedEdge for the bare connector",
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
		file:    "18.3-edge-type/key_label_set.gql",
		outcome: resolves,
		feature: "GG21",
	},
	{
		file:     "18.3-edge-type/empty_key_label_set.gql",
		outcome:  unsupported,
		sentinel: ErrUnnamedEdgeType,
		feature:  "GG21",
		bead:     "gqlc-0ri",
		reason:   "`-[=> :AUTHORED]->` declares an explicitly empty key label set on an edge type, leaving EdgeKey.KeyLabels nothing to hold; the node form is rejected for the same reason",
	},
	{
		file:     "18.3-edge-type/endpoint_key_label_set.gql",
		outcome:  unsupported,
		sentinel: ErrEndpointFillerImpliesLabels,
		feature:  "GG21",
		bead:     "gqlc-0ri",
		reason:   "an inline endpoint names a node type by its key label set, so implied labels there assert something about the referenced declaration that nothing checks; rejected rather than silently discarded, for the reason ErrEndpointFillerHasProperties already is (gqlc-h9n.18)",
	},
}

// semanticAreaC holds this area's semantic cases: files above that resolve to a
// model known to be wrong. Empty today: gqlc-h9n.3 promoted the one entry that
// lived here from "resolves-but-wrong" to a proper rejection with a sentinel,
// so the corpus itself now records the outcome and no semantic case is needed.
// The list stays declared (rather than nil) because corpusManifest requires
// every area to name its semantic slice, so a future case cannot land here
// silently — see corpus_test.go.
var semanticAreaC = []semanticCase{}
