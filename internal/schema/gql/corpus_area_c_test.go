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
		file:    "18.3-edge-type/pattern_endpoint_alias.gql",
		outcome: resolves,
		feature: "mandatory",
	},
	{
		file:     "18.3-edge-type/pattern_undirected.gql",
		outcome:  unsupported,
		sentinel: ErrUndirectedEdge,
		feature:  "GH02",
		bead:     "gqlc-0ri",
		reason:   "an undirected edge is a distinct element kind, and the distinction is observable through IS DIRECTED and IS SOURCE OF, so encoding one with a canonical direction would answer those wrongly rather than imprecisely; declined permanently. The GH02 citation is inferred rather than sourced to a subclause — ADR 0016 states the inference",
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
		file:    "18.3-edge-type/pattern_name_no_kind.gql",
		outcome: resolves,
		feature: "mandatory",
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
		file:     "18.3-edge-type/kind_undirected_connector_to.gql",
		outcome:  unsupported,
		sentinel: ErrEdgeKindArcMismatch,
		feature:  "mandatory",
		bead:     "gqlc-h9n.10",
		reason:   "the phrase-form twin of kind_undirected_arc_directed.gql, and it differs in one way that matters: `->` is unambiguously directed, but `TO` is an alternative of both connectorPointingRight and connectorUndirected (GQL.g4:1659-1667). The sentinel here is therefore ANTLR's ordered choice speaking, not a reading of the source — endpointPair lists endpointPairDirected first, so the directed parse wins and contradicts the declared UNDIRECTED. Under the other reading of the same text this file would be ErrUndirectedEdge. Both reject, which is why the ambiguity has never surfaced as a defect; the entry exists so that it cannot start doing so silently",
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
		reason:   "an edge type with property types but no label has no label-set key, so EdgeKey holds a source and a target with nothing between them to tell two such edges apart. Decided in ADR 0018: declined on gqlc's model, not on a reading of the standard",
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
	{
		file:     "18.3-edge-type/phrase_name_only.gql",
		outcome:  unsupported,
		sentinel: ErrUnnamedEdgeType,
		feature:  "mandatory",
		bead:     "gqlc-0ri",
		reason:   "an edge type phrase that names the type and goes straight to CONNECTING, reaching edgeTypePhraseFiller alternative 1 with no filler. Identity is the key label set together with the endpoints, so good endpoints do not rescue a missing label set — the edge half of 18.2's phrase_name_only.gql",
	},
	{
		file:    "18.3-edge-type/key_label_set_only.gql",
		outcome: resolves,
		feature: "GG21",
	},
	{
		file:     "18.3-edge-type/endpoint_no_filler.gql",
		outcome:  unsupported,
		sentinel: ErrUnknownEndpoint,
		feature:  "mandatory",
		bead:     "gqlc-0ri",
		reason:   "the endpoint `()` names the node type with an empty key label set, which ADR 0018 makes undeclarable rather than merely undeclared. The competing reading — an unconstrained endpoint — is declined too, Schema.Edges having no wildcard end, so only the diagnostic turns on which is right; gqlc-h9n.35 holds that question",
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
