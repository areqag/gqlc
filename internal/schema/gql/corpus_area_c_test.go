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
		bead:     "gqlc-xtq",
		reason:   "the declared edgeKind (UNDIRECTED) contradicts the arc direction (->); rejected rather than silently reinterpreted as DIRECTED. Provisional deviation: whether ISO Syntax Rules permit or forbid this is undecided without the paid PDF; implementation-defined.xml does not list it (127 items checked). See gqlc-xtq",
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
		bead:     "gqlc-xtq",
		reason:   "kind=DIRECTED with the `~` (undirected) connector is a contradiction: the mismatch fires before the accepted-subset check that would otherwise report ErrUndirectedEdge for the bare connector. Provisional deviation: whether ISO Syntax Rules permit or forbid this is undecided without the paid PDF. See gqlc-xtq",
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
		file:    "18.3-edge-type/implied_label_is_node_key_label.gql",
		outcome: resolves,
		feature: "GG21",
	},
	{
		file:     "18.3-edge-type/implied_label_is_edge_key_label.gql",
		outcome:  unsupported,
		sentinel: ErrImpliedLabelIsKeyLabel,
		feature:  "GG21",
		bead:     "gqlc-0ri",
		reason:   "the edge half of ADR 0015's decline, which had no case of its own. resolve() calls rejectInheritance once per kind, and implied_label_is_node_key_label.gql pins only the half that resolves — an edge implying a NODE key label, the two vocabularies being separate. Nothing pinned that an edge implying an EDGE key label does not, so the edge call could be deleted with the suite green and only that file's comment still claiming the rule ran both ways",
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
		file:    "18.3-edge-type/endpoint_empty_property_block.gql",
		outcome: resolves,
		feature: "mandatory",
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
		file:    "18.3-edge-type/phrase_key_label_set_only.gql",
		outcome: resolves,
		feature: "GG21",
		reason:  "the phrase spelling of key_label_set_only.gql, and the edge counterpart of 18.2's phrase_key_label_set_only.gql. The two edge collectors copy fillerContent's fields through separate hand-written assignments, so the pattern file grounds only its own; every other edge key-label-set file is a pattern-form arc, and narrowing EnterEdgeTypePhrase's hasKeyLabelSet copy to a constant false left the suite green",
	},
	{
		file:     "18.3-edge-type/pattern_unsupported_property_type.gql",
		outcome:  unsupported,
		sentinel: ErrUnsupportedType,
		feature:  "mandatory",
		bead:     "gqlc-h9n.5",
		reason:   "no corpus file put an unsupported property type on an edge at all, so edgeContent's error arm never returned a non-nil error across the whole suite; deleting the propagation from edgeContent and from EnterEdgeTypePattern's arm both left the tree green, silently dropping the edge and the relationship with it. The value type is incidental — constructed_list_angle.gql pins that LIST declines on a node",
	},
	{
		file:     "18.3-edge-type/phrase_unsupported_property_type.gql",
		outcome:  unsupported,
		sentinel: ErrUnsupportedType,
		feature:  "mandatory",
		bead:     "gqlc-h9n.5",
		reason:   "the phrase spelling of pattern_unsupported_property_type.gql. The two edge collectors forward edgeContent's error through separate arms, so the pattern file grounds only its own; dropping the l.fail from EnterEdgeTypePhrase's arm left the suite green even with the pattern-form file present",
	},
	{
		file:     "18.3-edge-type/endpoint_no_filler.gql",
		outcome:  unsupported,
		sentinel: ErrUnknownEndpoint,
		feature:  "mandatory",
		bead:     "gqlc-0ri",
		reason:   "the endpoint `()` names the node type with an empty key label set (ADR 0021: `nodeTypeFiller` absent = empty specification = empty label set). ADR 0018 makes such a type undeclarable, so `resolve()` returns ErrUnknownEndpoint accurately. The competing reading — an unconstrained endpoint — is also declined (Schema.Edges has no wildcard end), and ADR 0021 prefers the grammar-consistent reading 1 while the Syntax Rules remain unavailable",
	},
	{
		file:     "18.3-edge-type/endpoint_property_block_only.gql",
		outcome:  unsupported,
		sentinel: ErrUnknownEndpoint,
		feature:  "mandatory",
		bead:     "gqlc-0ri",
		reason:   "`({ })` reaches endpoint_no_filler.gql's outcome through the other arm: the filler is present but says nothing, so fillerLabels runs its whole nil-guard chain and returns no labels from the trailing bail rather than from the f == nil one. Nothing exercised that bail — neutralising it left the suite green, where any file taking this path would have panicked on a nil LabelSetPhrase. The block being empty is load-bearing: a non-empty one is rejected earlier as ErrEndpointFillerHasProperties",
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
