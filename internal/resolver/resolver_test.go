package resolver

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/areqag/gqlc/internal/graph"
	"github.com/areqag/gqlc/internal/procsig"
	"github.com/areqag/gqlc/internal/query"
	"github.com/areqag/gqlc/internal/query/cypher"
	"github.com/areqag/gqlc/internal/schema"
	"github.com/areqag/gqlc/internal/schema/gql"
)

// signaturesR7 declares the procedures the R7 fixture set exercises. Each
// mirrors a Stage-14 corpus signature (parser_test.go §Call*). The wire
// discipline is: parser consumes this registry via cypher.WithRegistry(regR7)
// at loadQuery; the resolver receives it via New(...) for R7's YIELD column
// typing arms (spec §4.2.1) but discards it at resolve.go's registry sink —
// the parser is authoritative on procedure lookup (§4.4 trust posture).
var signaturesR7 = []procsig.Signature{
	{
		Name: "test.labels",
		Results: []procsig.Result{
			{Name: "label", Token: procsig.TokenString, Nullable: true},
		},
	},
	{
		Name: "test.my.proc",
		Params: []procsig.Param{
			{Name: "name", Token: procsig.TokenString, Nullable: true},
			{Name: "id", Token: procsig.TokenInteger, Nullable: true},
		},
		Results: []procsig.Result{
			{Name: "city", Token: procsig.TokenString, Nullable: true},
			{Name: "country_code", Token: procsig.TokenInteger, Nullable: true},
		},
	},
	{
		Name: "test.count",
		Results: []procsig.Result{
			{Name: "n", Token: procsig.TokenInteger, Nullable: true},
		},
	},
	{
		Name: "test.temperature",
		Results: []procsig.Result{
			{Name: "celsius", Token: procsig.TokenFloat, Nullable: true},
		},
	},
	{
		Name: "test.number",
		Results: []procsig.Result{
			{Name: "value", Token: procsig.TokenNumber, Nullable: true},
		},
	},
	{
		Name: "test.constants",
		Results: []procsig.Result{
			{Name: "constant", Token: procsig.TokenString, Nullable: false},
		},
	},
	// 0ig accept-path signatures: FLOAT and NUMBER param axes are absent from
	// the R7 corpus (its Params surface is INTEGER+STRING only via
	// test.my.proc), so the argAssignable loose arms (§8.2.1 INT-at-FLOAT
	// / TCK Call3 [5], NUMBER assignable-from INTEGER-or-FLOAT) have no
	// production reach without new signatures. Test-only surface — the
	// wire registry is authored per deployment.
	{
		Name: "test.float.proc",
		Params: []procsig.Param{
			{Name: "temp", Token: procsig.TokenFloat, Nullable: true},
		},
		Results: []procsig.Result{
			{Name: "reading", Token: procsig.TokenFloat, Nullable: true},
		},
	},
	{
		Name: "test.number.proc",
		Params: []procsig.Param{
			{Name: "value", Token: procsig.TokenNumber, Nullable: true},
		},
		Results: []procsig.Result{
			{Name: "out", Token: procsig.TokenString, Nullable: true},
		},
	},
}

// regR7 is the Registry built from signaturesR7. Package-level so the R7
// fixture suite constructs it once — a construction failure fails the whole
// suite via mustBuildRegistry's panic-on-error posture (spec §6.3 design note).
var regR7 = mustBuildRegistry("R7", signaturesR7)

// regR7Alt is the corpus sweep's second registry (gqlc-2xkf). WithRegistry is
// the resolver's only Option, so a registry is the whole of the axis the sweep
// otherwise holds constant, and a CALL diagnostic reachable only under some
// other signature table moves no cell of the manifest.
//
// It is DERIVED from signaturesR7, not authored beside it. The parser resolves
// a CALL's procedure name, its arity and its YIELD field names against the
// registry it is handed (cypher/call.go: collectCall's three fail-sites), so a
// second literal table that drifted on any of those three would stop a fixture
// PARSING and abort the sweep at require.NoError with a message naming the
// fixture rather than the table. Deriving holds all three identical by
// construction and leaves only what the resolver reads.
var regR7Alt = mustBuildRegistry("R7Alt", perturbSignatures(signaturesR7))

// perturbSignatures rotates every type token one step around the closed
// TypeToken sum and flips every nullability. The rotation is a permutation, so
// no token keeps its value and every result is still inside the sum
// NewRegistry admits.
//
// The two axes reach the resolver's output by different routes, which is why
// TestSweepRegistryDelta asserts a live cell on each rather than on their
// total: a PARAM token reaches argAssignable, where the rotation turns a
// STRING argument at a STRING parameter into one at a NUMBER parameter and
// refuses; a RESULT token and its nullability reach the parser's TypeToken
// bridge and land on the CallBinding's result type, so they move an accepting
// cell's model without moving its verdict.
//
// A NUMBER result is not a shape a deployment would author — the bridge maps
// it to query.TypeUnknown (procsig package doc, Stage 14 §3.2). It is admitted
// here because this table is a perturbation of R7 and not a claim about any
// deployment.
func perturbSignatures(in []procsig.Signature) []procsig.Signature {
	out := make([]procsig.Signature, len(in))
	for i, sig := range in {
		out[i] = procsig.Signature{Name: sig.Name}
		for _, p := range sig.Params {
			out[i].Params = append(out[i].Params, procsig.Param{
				Name: p.Name, Token: rotateToken(p.Token), Nullable: !p.Nullable,
			})
		}
		for _, r := range sig.Results {
			out[i].Results = append(out[i].Results, procsig.Result{
				Name: r.Name, Token: rotateToken(r.Token), Nullable: !r.Nullable,
			})
		}
	}
	return out
}

// rotateToken names every member of the sum and panics on anything else, in
// mustBuildRegistry's posture: a token added to procsig without a case here
// would otherwise fold silently onto an existing one, and two signatures that
// perturb to the same token are a perturbation that moves fewer cells than its
// caller's comment claims.
func rotateToken(t procsig.TypeToken) procsig.TypeToken {
	switch t {
	case procsig.TokenInteger:
		return procsig.TokenFloat
	case procsig.TokenFloat:
		return procsig.TokenString
	case procsig.TokenString:
		return procsig.TokenNumber
	case procsig.TokenNumber:
		return procsig.TokenInteger
	default:
		panic(fmt.Sprintf("resolver_test: TypeToken %d is outside the sum rotateToken names", int(t)))
	}
}

func mustBuildRegistry(name string, sigs []procsig.Signature) procsig.Registry {
	reg, err := procsig.NewRegistry(sigs)
	if err != nil {
		panic("resolver_test: " + name + " signatures failed to build registry: " + err.Error())
	}
	return reg
}

var update = flag.Bool("update", false, "regenerate resolver .validated.golden.json files")

const fixtureDir = "../../test/data/resolver"

// invalidFixtures pairs each negative fixture with the sentinel it must
// produce. Totality against invalid/*.cypher is asserted in TestInvalid so a
// stray fixture or missing map entry fails the suite.
var invalidFixtures = map[string]error{
	"unknown_label.cypher":                                 ErrUnknownLabel,
	"label_satisfy_ambiguous.cypher":                       ErrAmbiguousLabel,
	"unknown_property.cypher":                              ErrUnknownProperty,
	"unknown_edge.cypher":                                  ErrUnknownEdge,
	"unknown_edge_property.cypher":                         ErrUnknownProperty,
	"ambiguous_unlabelled_binding.cypher":                  ErrAmbiguousBinding,
	"unlabelled_binding_no_edge.cypher":                    ErrUnknownLabel,
	"empty_inline_endpoint.cypher":                         ErrUnknownLabel,
	"anonymous_source_endpoint.cypher":                     ErrUnknownLabel,
	"parameter_type_conflict_two_properties.cypher":        ErrParameterTypeConflict,
	"parameter_type_conflict_clause_slot_vs_string.cypher": ErrParameterTypeConflict,
	"parameter_type_conflict_property_vs_expr_bool.cypher": ErrParameterTypeConflict,
	"parameter_type_conflict_nullability.cypher":           ErrParameterTypeConflict,
	// The kind arms of unify's ResolvedScalar / ResolvedTemporal cases. Every
	// other parameter_type_conflict fixture conflicts at the type assertion, so
	// these two are the only ones that reach the kind discriminator at all.
	"parameter_type_conflict_scalar_kind.cypher":         ErrParameterTypeConflict,
	"parameter_type_conflict_temporal_kind.cypher":       ErrParameterTypeConflict,
	"unknown_property_via_expr_use.cypher":               ErrUnknownProperty,
	"parameter_across_with_alias_shadow_reversed.cypher": ErrUnknownProperty,
	"list_of_nodes_projection.cypher":                    ErrOutOfR0Scope,
	"list_of_edges_projection.cypher":                    ErrOutOfR0Scope,
	"ambiguous_edge_orientation.cypher":                  ErrAmbiguousEdgeOrientation,
	"ambiguous_edge_orientation_after_inference.cypher":  ErrAmbiguousEdgeOrientation,
	"path_binding.cypher":                                ErrOutOfR0Scope,
	"unwind_binding.cypher":                              ErrOutOfR0Scope,
	"unknown_edge_undirected.cypher":                     ErrUnknownEdge,
	"unknown_edge_multi_type_all_miss.cypher":            ErrUnknownEdge,
	"unknown_property_union_missing.cypher":              ErrUnknownProperty,
	"unknown_property_union_type_differs.cypher":         ErrUnknownProperty,
	"unknown_property_union_nullability_differs.cypher":  ErrUnknownProperty,
	"unknown_property_union_sibling_branch.cypher":       ErrUnknownProperty,
	"untyped_edge.cypher":                                ErrOutOfR0Scope,
	"var_length_edge_property_projection.cypher":         ErrOutOfR0Scope,
	// R5 additions:
	"union_column_count_mismatch.cypher":       ErrUnionColumnMismatch,
	"union_column_name_mismatch.cypher":        ErrUnionColumnMismatch,
	"union_column_type_mismatch.cypher":        ErrUnionColumnMismatch,
	"union_column_nullability_mismatch.cypher": ErrUnionColumnMismatch,
	"union_unknown_label_branch.cypher":        ErrUnknownLabel,
	// rlt additions. compareBranchColumns checks name before type and both
	// raise ErrUnionColumnMismatch, so union_column_name_mismatch.cypher
	// above — which differs in name AND type — was caught by the type check
	// and left the name check free. This one differs in name only.
	"union_column_name_only_mismatch.cypher": ErrUnionColumnMismatch,
	// eb5j. Both name-arm fixtures above mismatch at column 0, so every index
	// the arm prints is the same digit and no wrong index can show. Here the
	// branches agree on columns 0 and 1 and differ on the name of column 2 —
	// two columns of agreement, so the arm has to walk past them, and an index
	// that matches neither branch 1 nor the 0 the others sit at.
	"union_column_name_mismatch_nonzero_index.cypher": ErrUnionColumnMismatch,
	// No fixture had three branches, so comparing only branch 1 against
	// branch 0 was free.
	"union_third_branch_mismatch.cypher": ErrUnionColumnMismatch,
	// resolvedTypeEqual is reachable only from compareBranchColumns, and no
	// fixture unioned two branches projecting different whole-entity or list
	// types under a common column name. One fixture per unpinned arm.
	"union_node_type_mismatch.cypher":              ErrUnionColumnMismatch,
	"union_edge_union_nullability_mismatch.cypher": ErrUnionColumnMismatch,
	"union_edge_union_keys_mismatch.cypher":        ErrUnionColumnMismatch,
	"union_list_element_mismatch.cypher":           ErrUnionColumnMismatch,
	// Two edge-union columns where one key list is a strict prefix of the other.
	// resolvedTypeEqual's arity check is the only thing separating them, and the
	// two orderings fail differently without it — see the comment on
	// TestEdgeUnionArityFixturesAreLoadBearing.
	"union_edge_union_arity_prefix.cypher":          ErrUnionColumnMismatch,
	"union_edge_union_arity_prefix_reversed.cypher": ErrUnionColumnMismatch,
	"part_binding_type_conflict.cypher":             ErrPartBindingTypeConflict,
	"part_binding_type_conflict_edge.cypher":        ErrPartBindingTypeConflict,
	// oou additions. unify checks property type and nullability side by side
	// and both raise ErrParameterTypeConflict, so
	// parameter_type_conflict_two_properties.cypher — STRING NOT NULL against
	// a nullable INT — was caught by the nullability check and left the type
	// check free. Both properties here are NOT NULL.
	"parameter_type_conflict_two_properties_same_nullability.cypher": ErrParameterTypeConflict,
	// The unlabelled twin of part_binding_type_conflict_call_vs_node.cypher.
	// The labelled arm's collision check fires at Phase A1; inferUnlabelled
	// carries its own copy for bindings that reach it, and that copy had no
	// fixture.
	"part_binding_type_conflict_call_vs_unlabelled.cypher": ErrPartBindingTypeConflict,
	// resolveNodeLabels' empty-satisfying-set arm. Every label here is
	// declared somewhere, so the undeclared-label arm above does not fire,
	// but no declared type carries both.
	"label_satisfy_none.cypher": ErrUnknownLabel,
	// R6 additions:
	"create_unknown_label.cypher":            ErrUnknownLabel,
	"create_unknown_edge.cypher":             ErrUnknownEdge,
	"merge_endpoint_unknown_label.cypher":    ErrUnknownLabel,
	"merge_unknown_edge.cypher":              ErrUnknownEdge,
	"merge_on_match_unknown_property.cypher": ErrUnknownProperty,
	"set_property_unknown_property.cypher":   ErrUnknownProperty,
	// The second of two effects. ValidateEffects walks s.effects in slice
	// order; with only the first validated, this query resolves clean.
	"set_second_effect_unknown_property.cypher":   ErrUnknownProperty,
	"set_property_on_projection_alias.cypher":     ErrInvalidEffectTarget,
	"set_property_on_var_length_edge.cypher":      ErrInvalidEffectTarget,
	"set_entity_on_projection_alias.cypher":       ErrInvalidEffectTarget,
	"set_labels_undeclared.cypher":                ErrUnknownLabel,
	"set_labels_on_edge.cypher":                   ErrInvalidEffectTarget,
	"remove_property_unknown.cypher":              ErrUnknownProperty,
	"remove_property_on_projection_alias.cypher":  ErrInvalidEffectTarget,
	"remove_labels_undeclared.cypher":             ErrUnknownLabel,
	"delete_projection_alias.cypher":              ErrInvalidEffectTarget,
	"delete_bare_property_unknown.cypher":         ErrUnknownProperty,
	"union_writes_vs_returns_column_count.cypher": ErrUnionColumnMismatch,
	// ag5 additions. The write-effect validators were 4/15 under mutation. These
	// nine pin the reachable fail-opens — each query below is rejected today and
	// accepted under the corresponding mutation.
	//
	// ON CREATE effects were validated by code that no test reached: ON MATCH had
	// merge_on_match_unknown_property.cypher, ON CREATE had nothing, so the whole
	// arm could be deleted silently.
	"merge_on_create_unknown_property.cypher": ErrUnknownProperty,
	"merge_on_create_undeclared_label.cypher": ErrUnknownLabel,
	// The variable-length edge guard appears in three validators and was pinned in
	// one of them (set_property_on_var_length_edge.cypher, single-type). Multi-type
	// edges take a different branch to reach it, so both edge shapes are covered.
	"set_property_on_var_length_multi_type_edge.cypher":    ErrInvalidEffectTarget,
	"set_entity_on_var_length_edge.cypher":                 ErrInvalidEffectTarget,
	"set_entity_on_var_length_multi_type_edge.cypher":      ErrInvalidEffectTarget,
	"remove_property_on_var_length_edge.cypher":            ErrInvalidEffectTarget,
	"remove_property_on_var_length_multi_type_edge.cypher": ErrInvalidEffectTarget,
	// Property lookup on a multi-type edge must miss if any union member lacks the
	// property. Covered on the read path (unknown_property_union_*), not the write.
	// The single-candidate edge arm of the SET/REMOVE property check was
	// unpinned: only the multi-candidate (unionProperty) arm had a fixture.
	"set_property_unknown_on_single_type_edge.cypher":    ErrUnknownProperty,
	"remove_property_unknown_on_single_type_edge.cypher": ErrUnknownProperty,
	// Two failing effects with different sentinels: pins that ValidateEffects
	// reports the FIRST failure, not the last.
	"effect_order_first_failure_wins.cypher":            ErrUnknownProperty,
	"set_property_unknown_on_multi_type_edge.cypher":    ErrUnknownProperty,
	"remove_property_unknown_on_multi_type_edge.cypher": ErrUnknownProperty,
	// kq6 additions. ag5 above pinned the SET family; its REMOVE LABELS and
	// DELETE twins kept the same guards with no fixture behind them, so 13 of
	// 26 mutations over the validators still survived.
	//
	// SET labels had set_labels_on_edge.cypher; REMOVE labels had no twin, so
	// both its edge rejection and the node-target gate they share were free.
	"remove_labels_on_edge.cypher": ErrInvalidEffectTarget,
	// Neither SET nor REMOVE pinned the projection-alias arm.
	"set_labels_on_projection_alias.cypher":    ErrInvalidEffectTarget,
	"remove_labels_on_projection_alias.cypher": ErrInvalidEffectTarget,
	// DELETE is the third property validator. SET and REMOVE each pin the
	// var-length guard and the property-existence check on both edge shapes;
	// DELETE pinned neither, on either shape.
	"delete_property_on_var_length_edge.cypher":            ErrInvalidEffectTarget,
	"delete_property_on_var_length_multi_type_edge.cypher": ErrInvalidEffectTarget,
	"delete_edge_property_unknown.cypher":                  ErrUnknownProperty,
	"delete_property_unknown_on_multi_type_edge.cypher":    ErrUnknownProperty,
	"delete_property_on_projection_alias.cypher":           ErrInvalidEffectTarget,
	// Both loops that walk a list of things to validate stopped at the first
	// element with the suite green: DELETE over its targets, MERGE over its
	// ON MATCH effects.
	"delete_second_target_unknown_property.cypher":         ErrUnknownProperty,
	"merge_on_match_second_effect_unknown_property.cypher": ErrUnknownProperty,
	// R7 additions:
	"call_yield_property_lookup.cypher":              ErrUnknownProperty,
	"part_binding_type_conflict_call_vs_node.cypher": ErrPartBindingTypeConflict,
	"part_binding_type_conflict_call_vs_edge.cypher": ErrPartBindingTypeConflict,
	// 0ig addition:
	"call_arg_type_mismatch.cypher": ErrCallArgAssignability,
	// call_arg_type_mismatch passes a good STRING and a bad INTEGER, so the
	// INTEGER arm raises first and the STRING arm is never exercised.
	"call_arg_int_at_string.cypher": ErrCallArgAssignability,
	// 76y additions. scope.go's parameter-witness lane was 4/12 under mutation.
	//
	// partScope.Contains gates which Refs get witnessed at all. Its edge-union arm
	// had no fixture, so a parameter on a multi-type edge property was witnessed by
	// nothing and conflicts went undetected.
	"parameter_conflict_via_multi_type_edge_property.cypher": ErrParameterTypeConflict,
	// The variable-length edge guard exists on both the projection path
	// (var_length_edge_property_projection.cypher) and the witness path. Only the
	// projection one was covered.
	"parameter_use_on_var_length_edge_property.cypher": ErrOutOfR0Scope,
	// The witness demotes a property to nullable when its binding is optional.
	// parameter_type_conflict_nullability.cypher conflicts two non-optional
	// bindings whose *declared* nullability differs, so the demotion term is never
	// what decides it. These two make it the deciding factor, on a NOT NULL
	// property behind OPTIONAL MATCH — see parameter_optional_nullability_agree
	// for the control that keeps the rejection from being blanket.
	"parameter_type_conflict_optional_node_nullability.cypher": ErrParameterTypeConflict,
	"parameter_type_conflict_optional_edge_nullability.cypher": ErrParameterTypeConflict,
	// The witness reports unknown properties for nodes and edges separately; the
	// node arm was pinned and the edge arm was not.
	"parameter_use_unknown_edge_property.cypher": ErrUnknownProperty,
	// h9n.22 additions. Plural label satisfaction (ADR 0022): a query label set
	// that satisfies more than one declared node type is permitted, but property
	// projection requires the property on every satisfying type (intersection),
	// and whole-entity reference is refused.
	"label_satisfy_plural_property.cypher": ErrUnknownProperty,
	"label_satisfy_plural_entity.cypher":   ErrAmbiguousLabel,
	// 0tft. Phase C narrows a plural endpoint to the node types its committed
	// candidates put on that end of the pattern, intersected across every
	// touching edge — so the refusal that survives the widening is the one
	// where that intersection is EMPTY. WORKS_AT is declared only from
	// Employee&Person and REVIEWED only into the bare Person, so the two edges
	// touching `p` pin it to disjoint types and no node satisfies both.
	//
	// The pre-closure satisfying set then stands (an empty intersection is a
	// fact about which rows come back, not about which types the projection can
	// name — refusing the query outright would narrow one gqlc accepts today,
	// ADR 0006), and ADR 0022's whole-entity refusal fires on it unchanged.
	//
	// What this fixture pins, precisely, is that EVERY touching edge is folded
	// in: consult only the first, or only the last, and the surviving set is
	// that one edge's contribution, `p` commits, and the query is accepted.
	// It does NOT separate intersect from union across edges — under a union
	// every candidate survives, nothing is learned, and ErrAmbiguousLabel comes
	// back from ADR 0022 exactly as it does here. That distinction is pinned by
	// TestEdgeClosureNarrowsThePluralEndpointsItPins and by
	// TestDeferredEdgesCloseBeforeTheNarrowing, both of which assert a
	// committed type rather than a sentinel.
	//
	// Its accepted counterpart is valid/plural_endpoint_whole_entity_after_
	// edge_closure.cypher, which lived here until 0tft.
	"plural_endpoint_contradictory_edges_stay_plural.cypher": ErrAmbiguousLabel,
	// The narrowing learns only from an edge EVERY surviving row is guaranteed
	// to have and that the query observed rather than wrote
	// (witnessesItsEndpoints). These six are the shapes that break that
	// guarantee, one per clause of the predicate: each puts the only WORKS_AT
	// declaration (Employee&Person -> Company) next to a plural `(p:Person)`,
	// and each still returns rows whose p is the bare Person type. Committing
	// Employee&Person on any of them names a whole entity — and, for the
	// property row, a NOT NULL column — that those rows do not have.
	//
	// Their accepted twins are in TestNarrowingLearnsOnlyFromEdgesEveryRowHas:
	// the same six queries with the offending clause removed all commit
	// Employee&Person, so nothing here is refused by the shape being
	// unresolvable for an unrelated reason.
	"plural_endpoint_optional_edge_stays_plural.cypher":          ErrAmbiguousLabel,
	"plural_endpoint_optional_edge_property_stays_plural.cypher": ErrUnknownProperty,
	"plural_endpoint_zero_hop_stays_plural.cypher":               ErrAmbiguousLabel,
	"plural_endpoint_zero_lower_bound_stays_plural.cypher":       ErrAmbiguousLabel,
	// `*0..1` is the shape where the two halves of singleHopPattern separate.
	// Its upper bound is one, so every question about hop COUNT passes it; the
	// zero lower bound is the only thing left to refuse it, and its twin `*..1`
	// (absent lower bound, read as one) differs in nothing else.
	"plural_endpoint_zero_lower_bound_one_hop_stays_plural.cypher": ErrAmbiguousLabel,
	"plural_endpoint_merged_edge_stays_plural.cypher":              ErrAmbiguousLabel,
	"plural_endpoint_created_edge_stays_plural.cypher":             ErrAmbiguousLabel,
	// Three types satisfy `(:Person)` and WORKS_AT is declared on two of them,
	// so the narrowing lands on a set that is smaller but still plural — the
	// arm no two-type schema can reach, because on two types every narrowing is
	// either "one survivor" or "no change". The sentinel is the same whether or
	// not that arm ran, so it is not what pins it: see
	// TestNarrowingToASmallerPluralSetIsWhatTheSetSaysItIs, which reads the
	// surviving set back out of this fixture's own message and requires it to be
	// exactly the two, and pairs it with the property projection that resolves
	// only because the third was dropped.
	"plural_endpoint_narrows_to_smaller_plural_set.cypher": ErrAmbiguousLabel,
	// A quantifier of more than one hop. The closure names the ends of ONE
	// declared edge; a multi-hop pattern's ends are one or more hops away from
	// those, so the type it commits is the wrong one — not merely a coarser one.
	// Every other plural-endpoint schema in the corpus hides this, because it
	// declares the label once and the closure then names the pattern's ends by
	// accident whatever the hop count. satisfy_plural_edges_chain.gql declares X
	// twice, A -> B -> C, so the two disagree:
	//
	//   *2   over (p:Node)  ->(c:Node:C) closes B -> C; p is A&Node
	//   *1..2 over (p:Node) ->(c:Node:C) closes B -> C; p is A&Node or B&Node
	//   *    over (p:Node)  ->(c:Node:C) closes B -> C; p is A&Node or B&Node
	//   *1..2 over (p:Node:A)->(c:Node)  closes A -> B; c is B&Node or C&Node
	//
	// and each fixture returns the single-typed property of the type the closure
	// wrongly names, so the mis-commitment is an emitted NOT NULL column rather
	// than a mis-named entity. Their accepted single-hop twins are in
	// TestNarrowingLearnsOnlyFromASingleHopEdge.
	"plural_endpoint_multi_hop_stays_plural.cypher":         ErrUnknownProperty,
	"plural_endpoint_multi_hop_range_stays_plural.cypher":   ErrUnknownProperty,
	"plural_endpoint_unbounded_hops_stays_plural.cypher":    ErrUnknownProperty,
	"plural_endpoint_multi_hop_far_end_stays_plural.cypher": ErrUnknownProperty,
	// An anonymous endpoint spelling `(:Company)` on a schema where Company&Large
	// also satisfies it. Both declared WORKS_AT edges match the pattern, so the
	// closure leaves `p` either person type and the Employee&Person ->
	// Company&Large row has a `p` with no personOnly. The accepted twin — the
	// same query on a schema where the spelled labels really are satisfied by one
	// type — is in TestNarrowingSkipsAnEndpointItCannotEnumerate.
	"plural_endpoint_inline_endpoint_stays_plural.cypher":          ErrAmbiguousLabel,
	"plural_endpoint_inline_endpoint_property_stays_plural.cypher": ErrUnknownProperty,
	// `(:Person)-[r:WORKS_AT]->(c)<-[q:WORKS_AT]-(p:Person)`, where the plurality
	// of the anonymous endpoint reaches the unlabelled `c` between the two hops.
	// Both WORKS_AT declarations are in the closure, so `c` is either company type
	// and Phase B has no single type to infer. That refusal lands on `c` before
	// either projection is reached, which is why the two spellings — `RETURN p`
	// and `RETURN p.personOnly` — now share a sentinel: what separated them was a
	// `p` the resolver got as far as typing.
	"plural_endpoint_unlabelled_hop_stays_plural.cypher":          ErrAmbiguousBinding,
	"plural_endpoint_unlabelled_hop_property_stays_plural.cypher": ErrAmbiguousBinding,
	// Part 1 infers `c` through an OPTIONAL hop and `WITH c` carries it into
	// Part 2, whose `p2` is left over both person types by the two WORKS_AT
	// declarations — so personOnly is missing on one of them. That is the
	// refusal the fixture is named for, and which Part it lands in has moved
	// twice. sxzj moved it OFF Part 2: Phase B stopped committing `c` to
	// Company alone, `WITH c` became a whole-entity projection of a plural
	// binding, and the refusal became Part 1's ErrAmbiguousBinding on `c`
	// before Part 2 was ever resolved. etj6 moved it back, by deferring that
	// projection refusal in a non-final Part — a `WITH` is not a user-visible
	// column, so a plural binding may cross one and be narrowed on the far
	// side. Nothing narrows `c` here, but nothing needs to: `c` is projected
	// nowhere else, and Part 2's own fault is reached first.
	//
	// So this fixture no longer witnesses the widened lane's whole-entity
	// message; unlabelled_optional_hop_whole_entity_stays_wide carries that
	// pin instead, in a single-Part query no carry can move.
	//
	// The carry lane it used to pin (newScope leaving a carried entry out of
	// resolvedCovers) is still live; carried_uncovered_endpoint_stays_plural
	// below reaches it.
	"plural_endpoint_carried_hop_property_stays_plural.cypher": ErrUnknownProperty,
	// The carry lane, reached by the route sxzj left open: `p` is singular from
	// its label, the ONE WORKS_AT out of Person&Employee lands on Company&Large,
	// and no edge touching `c` is trustworthy — the hop is OPTIONAL — so Phase B
	// commits that singleton with no covering mark rather than widening it. `WITH
	// c` then carries a singular type whose provenance did not survive the scope
	// boundary. If newScope seeded resolvedCovers from the carry, Phase C would
	// narrow `p2` through Company&Large to Person&Employee alone and this would
	// ACCEPT; it stays plural, so the projection is refused on the Person branch.
	"carried_uncovered_endpoint_stays_plural.cypher": ErrUnknownProperty,
	// Three candidates of which only the first and third disagree about which
	// of the pattern's endpoints is the source; the second is a plural-endpoint
	// duplicate of the first's side and carries no orientation signal.
	"ambiguous_edge_orientation_plural_endpoints.cypher": ErrAmbiguousEdgeOrientation,
	// The reverse declaration lands on a SUBTYPE of the endpoint the forward
	// one uses, so the two candidates cross the pattern in opposite directions
	// without being exact mirrors of each other. A guard that tests for the
	// mirror {A,L,B}/{B,L,A} admits both of these — the whole-entity form is
	// then bounced by codegen's same-label union guard one stage later, but the
	// property form (below) is caught by nothing and generates compiling code.
	"ambiguous_edge_orientation_reversed_subtype.cypher":          ErrAmbiguousEdgeOrientation,
	"ambiguous_edge_orientation_reversed_subtype_property.cypher": ErrAmbiguousEdgeOrientation,
	// The two endpoints' satisfying sets OVERLAP — one is a strict subset of
	// the other — which is the ordinary shape of a subtype schema and the shape
	// ADR 0022 exists to serve. Classifying a candidate by its Source alone
	// then reads every candidate as sitting on both sides of the pattern, so
	// none is ever classified forward and the disagreement cannot be reported;
	// these two are the exact mirror {A,L,B}/{B,L,A} that the *narrowest*
	// possible guard catches, admitted by a broader one. Neither is reachable
	// by the corpus that shipped before them: every other undirected fixture
	// runs between endpoint sets that are equal or disjoint.
	//
	// Both subset directions are here because they classify different
	// candidates as the forward witness, so they pin the witness order (§4.4)
	// as well as the verdict — see invalidFixtureContains.
	//
	// Property projection, not whole-entity: `RETURN r` is bounced one stage
	// later by codegen.ErrUnrepresentableEdgeUnion, and `RETURN r.rating`
	// reaches nothing that objects (§4.6.1). The property form is the one that
	// generates silently wrong code if this verdict is lost.
	"ambiguous_edge_orientation_overlapping_endpoints.cypher":          ErrAmbiguousEdgeOrientation,
	"ambiguous_edge_orientation_overlapping_endpoints_reversed.cypher": ErrAmbiguousEdgeOrientation,
	// ejn0 additions. An anonymous edge is its own binding, so it reaches the
	// orientation refusal with no variable to name it by — and every undirected
	// fixture that shipped before these binds one. The two differ in what the
	// endpoints are: named bindings on one, an inline label expression on the
	// other, which are the two things a pattern's ends can be.
	"ambiguous_edge_orientation_anonymous.cypher":                 ErrAmbiguousEdgeOrientation,
	"ambiguous_edge_orientation_anonymous_inline_endpoint.cypher": ErrAmbiguousEdgeOrientation,
	// The endpoint-inference refusal is the other fail-site that names the edge
	// binding, and it is the only one an endpoint carrying neither a variable
	// nor labels can reach: an anonymous edge closes over it, so the `()` end
	// has to be describable too.
	"anonymous_edge_uninferable_endpoint.cypher": ErrUnknownLabel,
	// A 2x2 endpoint cross-product with all four keys declared, so the candidate
	// set holds TWO disjoint swapped pairs rather than one. Every undirected
	// fixture before this one has exactly one pair, which makes "the first
	// witness on each side" indistinguishable from "the only witness on each
	// side" — the choice is unobservable until a second pair exists to lose to.
	"ambiguous_edge_orientation_two_swapped_pairs.cypher": ErrAmbiguousEdgeOrientation,
	// h6h7's second direction. The one-token twin of
	// valid/unlabelled_narrowed_by_pinned_far_end.cypher: OPTIONAL on the
	// HAS_DESK hop, so the hop is an outer join and every row that matched
	// without it still comes back. `p` is bare, its only edge is WORKS_AT, and
	// both declared WORKS_AT edges reach a type satisfying `(:Company)` — so the
	// edges prove nothing about which person type `p` is and it must stay WIDE.
	// invalidFixtureContains below requires BOTH person types in the message:
	// the sentinel says the resolver declined to pick, the message says which
	// set it declined over, and it is the second that distinguishes staying wide
	// from narrowing to whichever type happened to be seen first.
	"unlabelled_optional_far_end_stays_wide.cypher": ErrAmbiguousBinding,
	// Two edges out of the same bare `p` whose narrowed far ends pin it to
	// different author types, so the narrowed intersection is empty while the
	// unnarrowed one still holds both. candidateTypes returns the unnarrowed
	// answer there, which is master's, and this sentinel is what that decision
	// is worth: returning the empty narrowed set instead lands on
	// commitUnlabelledRound's case 0, ErrUnknownLabel — "no edge in the pattern
	// reaches a compatible schema node type" — which is false, since both edges
	// reach one. See TestNarrowedEndsThatDisagreeKeepTheWideAnswer for the two
	// halves, each of which narrows `p` on its own and to a different type.
	"unlabelled_narrowed_ends_disagree.cypher": ErrAmbiguousBinding,
	// sxzj. The three fixtures below share one shape — a bare `(c)` reached by
	// a mandatory WORKS_AT and an OPTIONAL HAS_DESK — and differ in what is
	// asked of it. The OPTIONAL hop is the only thing pinning `c` to the bare
	// Company, and it is an outer join, so the Employee&Person -[WORKS_AT]->
	// Company&Large row comes back with h/d null and `c` is Company&Large on it.
	//
	// The next hop: `x` reads `c`, and a `c` committed to Company alone makes
	// `x` the bare Person, whose personOnly the OTHER person type lacks. With
	// `c` left over both company types, both WORKS_AT declarations reach it and
	// Phase B declines to pick `x` at all. invalidFixtureContains requires both
	// person types in the message, for the reason
	// unlabelled_optional_far_end_stays_wide gives.
	"unlabelled_optional_hop_next_hop_stays_wide.cypher": ErrAmbiguousBinding,
	// The same `c`, projected directly. smallOnly is declared on the bare
	// Company and not on Company&Large, so this is the column the OPTIONAL hop
	// buys and the rows it is null on. The plural lane's per-type message is
	// pinned below: it is what says `c` is plural rather than pinned to the
	// type that happens to carry the property.
	"unlabelled_optional_hop_type_only_property.cypher": ErrUnknownProperty,
	// The same `c` again, projected WHOLE. This is the widened lane's other
	// exit: refProjectionType's nodeCands arm with an empty property, which
	// renders the candidate slice itself rather than picking a type out of it.
	// The sentinel is ErrAmbiguousBinding and not ErrAmbiguousLabel because `c`
	// carries no labels — the split is pluralByInference's, and this fixture is
	// the corpus' only whole-entity projection of a binding that reached the
	// lane by inference. Its message pins the slice's ORDER; see below.
	"unlabelled_optional_hop_whole_entity_stays_wide.cypher": ErrAmbiguousBinding,
	// The control for the widening's gate. `(p:Employee)` pins the mandatory
	// hop's far end to Person&Employee, whose WORKS_AT reaches Company&Large
	// only, and the OPTIONAL hop reaches the bare Company only — so the
	// intersection over ALL touching edges is empty and Phase B's case 0 fires.
	// The set the OPTIONAL hop is dropped from is NOT empty (it is
	// {Company&Large}), so a widening that fired on an empty intersection would
	// accept this and offer largeId. The sentinel is what says it does not.
	"unlabelled_optional_hop_empty_intersection.cypher": ErrUnknownLabel,
	// The OTHER conjunct of the same gate, and the only fixture that separates
	// it. Every touching edge here witnesses its endpoints — both hops are
	// mandatory single hops — so witnessesItsEndpoints holds throughout and the
	// far-end covering question alone decides which edges reach `attainable`.
	// `MATCH (dd:Desk) WITH dd` is what makes HAS_DESK's far end uncovered: a
	// carried entry lands in `resolved` and never in resolvedCovers, because
	// branchState carries the type and not how the previous Part arrived at it.
	//
	// So `inferred` is {Company} (HAS_DESK is declared out of the bare Company
	// only) and `attainable` is {Company, Company&Large} (WORKS_AT alone), and
	// the widening refuses smallOnly. Drop `otherCovers` from the gate and
	// HAS_DESK folds into `attainable` too, making it the same singleton as
	// `inferred`, and this query ACCEPTS c.smallOnly as NOT NULL — which is
	// origin/master's answer, since master's covering flag gated only what OTHER
	// bindings learned from `c`.
	//
	// The refusal is the carry lane's conservatism and not a claim about the
	// rows: HAS_DESK is mandatory here, so `c` really is the bare Company on
	// every returned row and master's acceptance was sound. Seeding
	// resolvedCovers from the carry would make this accept again — the same
	// alternative newScope names, and the one
	// carried_uncovered_endpoint_stays_plural is written against.
	"unlabelled_hop_to_carried_far_end_stays_wide.cypher": ErrUnknownProperty,

	// The ref-valued-leaf certificate's acceptance change (spec
	// model-change-f45qn §5). Before it, a rich projection's Refs were never
	// resolved against the schema, so both of these were ACCEPTED as []any and
	// the typo survived to the generated code. Each is its valid twin with one
	// property renamed — certified_list_of_properties and
	// certified_collect_property — so the pair isolates the undeclared property
	// as the only difference between resolving and refusing.
	"certified_list_unknown_property.cypher":    ErrUnknownProperty,
	"certified_collect_unknown_property.cypher": ErrUnknownProperty,
}

// invalidFixtureContains pins the message arm for fixtures where errors.Is
// alone cannot distinguish which branch of a validator fired. Only entries
// where arm discrimination matters are listed; absent entries skip the check.
var invalidFixtureContains = map[string]string{
	// Effect validators: alias arm vs edge arm vs scope-miss arm all return
	// ErrInvalidEffectTarget, so errors.Is does not distinguish them.
	"set_property_on_projection_alias.cypher":    "projection alias",
	"set_entity_on_projection_alias.cypher":      "projection alias",
	"set_labels_on_projection_alias.cypher":      "projection alias",
	"set_labels_on_edge.cypher":                  "edge binding",
	"remove_property_on_projection_alias.cypher": "projection alias",
	"remove_labels_on_projection_alias.cypher":   "projection alias",
	"remove_labels_on_edge.cypher":               "edge binding",
	"delete_projection_alias.cypher":             "projection alias",
	"delete_property_on_projection_alias.cypher": "projection alias",
	// ErrOutOfR0Scope: path/unwind binding arm (resolve.go:204) vs
	// refProjectionType arm (scope.go:706) — distinguished by "binding" suffix.
	"path_binding.cypher":   "path binding",
	"unwind_binding.cypher": "unwind binding",
	// The three ways the refusal can name the edge it refuses. A named binding
	// is quoted; an anonymous one has no name, so it is placed by the label it
	// carries and the two ends it runs between — which are themselves either a
	// bound variable or an inline label expression. errors.Is passes on all
	// three, and so does a message that has gone back to naming `edge ""`.
	"ambiguous_edge_orientation.cypher":                           `edge "r" matches`,
	"ambiguous_edge_orientation_anonymous.cypher":                 `the [:AUTHORED] edge between p and post matches`,
	"ambiguous_edge_orientation_anonymous_inline_endpoint.cypher": `the [:AUTHORED] edge between p and (:Post) matches`,
	"anonymous_edge_uninferable_endpoint.cypher":                  `of the [:AUTHORED] edge between p and ()`,
	// The message must name one candidate from each side of the disagreement,
	// first-in-candidate-order per side, and leave out the third — which plural
	// satisfaction widened the set with on a side already represented.
	// errors.Is passes on any pair.
	"ambiguous_edge_orientation_plural_endpoints.cypher": `matches Employee&Person-[REVIEWED]->Company left-to-right and Company-[REVIEWED]->Person right-to-left`,
	// Neither key is the other's mirror; the message must still name both
	// sides, so it cannot be produced by a mirror test.
	"ambiguous_edge_orientation_reversed_subtype.cypher":          `matches Author-[REVIEWED]->Book left-to-right and Book-[REVIEWED]->Author&Editor right-to-left`,
	"ambiguous_edge_orientation_reversed_subtype_property.cypher": `matches Author-[REVIEWED]->Book left-to-right and Book-[REVIEWED]->Author&Editor right-to-left`,
	// The same two keys in both fixtures, on one schema, reported in opposite
	// orders: which candidate is the left-to-right witness is decided by the
	// pattern's endpoints and not by the schema's declaration order, so the two
	// pins together say the message is read off the query rather than the
	// candidate list.
	"ambiguous_edge_orientation_overlapping_endpoints.cypher":          `matches Employee&Person-[REVIEWED]->Person left-to-right and Person-[REVIEWED]->Employee&Person right-to-left`,
	"ambiguous_edge_orientation_overlapping_endpoints_reversed.cypher": `matches Person-[REVIEWED]->Employee&Person left-to-right and Employee&Person-[REVIEWED]->Person right-to-left`,
	// Two swapped pairs in one candidate set: both pairs would be a correct
	// answer to "which two candidates disagree", so the pin says *which* the
	// first-witness-per-side scan picks. See
	// TestTwoSwappedPairsReportsTheFirstInCandidateOrder for why the set really
	// holds two.
	"ambiguous_edge_orientation_two_swapped_pairs.cypher": `matches Employee&Person-[REVIEWED]->Company&Startup left-to-right and Company&Startup-[REVIEWED]->Employee&Person right-to-left`,
	// ErrUnionColumnMismatch's type arm, and the one literal copy of
	// unionColumnTypeArm in the repo. The arm's surrounding frame is already
	// pinned by unionTypeArmMessage, but that regexp is BUILT FROM the constant,
	// so it admits the same fixture set whatever the phrase says — rewriting the
	// constant to " zzz " left the whole suite green. The trailing space is the
	// constant's own; the phrase is spelled here in full so a change to it has
	// to be a change someone made on purpose.
	"union_column_type_mismatch.cypher": `column "x" projects `,
	// eb5j. The name arm's index, pinned where it has a value to get wrong.
	// The comparison is positional — other[i] against base[i] — so branch 0's
	// index can never differ from the one already printed, and the arm used to
	// restate it: "branch 1 column 2 named "q"; branch 0 column 2 named "p"".
	// Two numbers a reader is invited to check against each other and can learn
	// nothing from.
	//
	// The mismatch sits at column 2 of branch 1, so the two numbers in this
	// message differ. Both other name-arm fixtures mismatch at column 0 of
	// branch 1, where the column index, branch 0's restated index and the digit
	// 0 are all the same character — and column 1 of branch 1 would have been
	// no better, since the column index and the branch index would then agree.
	// Only a message whose indices disagree can say that the number reported is
	// the mismatching COLUMN rather than the branch, or the constant a
	// coincidence made look right.
	"union_column_name_mismatch_nonzero_index.cypher": `branch 1 column 2 named "q"; branch 0 named "p"`,
	// h6h7's stays-wide direction. ErrAmbiguousBinding says only that Phase B
	// declined to pick; this says it declined over BOTH person types, i.e. that
	// the OPTIONAL hop narrowed nothing. A narrowing that fired here would list
	// one type, and the substring would fail.
	"unlabelled_optional_far_end_stays_wide.cypher": `candidate types: Employee&Person, Person`,
	// The empty narrowed set would have been reported as ErrUnknownLabel; this
	// says the message is the wide lane's, over both author types, and not one
	// of the two singletons the halves reach.
	"unlabelled_narrowed_ends_disagree.cypher": `candidate types: Author, Author&Editor`,
	// sxzj. Same discrimination as the stays-wide entry above, one hop further
	// out: the sentinel says Phase B declined to pick `x`, and this says it
	// declined over both person types — which it can only do if `c` was left
	// over both company types. A `c` pinned to the bare Company makes `x` the
	// bare Person and the query resolve.
	"unlabelled_optional_hop_next_hop_stays_wide.cypher": `candidate types: Employee&Person, Person`,
	// Names the type the property is MISSING on, not the one that carries it.
	// ErrUnknownProperty is also what a `c` pinned to Company&Large would give,
	// with the same variable and property in the message; the "plural-satisfying
	// type" phrase is unionNodeProperty's and is reachable only from the plural
	// lane.
	"unlabelled_optional_hop_type_only_property.cypher": `c.smallOnly missing on plural-satisfying type Company&Large`,
	// Case 0's message, distinguishing it from the deferred-then-ambiguous arm:
	// both are refusals about a bare binding, and only this one says no edge
	// reaches a compatible type.
	"unlabelled_optional_hop_empty_intersection.cypher": `no edge in the pattern reaches a compatible schema node type`,
	// The widened lane's own rendering, and the only fixture that shows one: `c`
	// reaches the whole-entity arm out of t.cands, so this is nodeTypesForKeys'
	// key order verbatim. Ascending is what satisfyingNodeTypes produces for a
	// LABELLED plural binding, and the two lanes feed the same formatter, so a
	// widened `c` that rendered descending would spell the same set two ways.
	"unlabelled_optional_hop_whole_entity_stays_wide.cypher": `node type: Company, Company&Large`,
	// Names `p2` and not `c`: the whole point of the retarget is that the carry
	// no longer refuses, so a bare ErrUnknownProperty would be satisfied by a
	// refusal on either side of the `WITH`. "plural-satisfying type" is
	// unionNodeProperty's phrase and says Part 2 reached the plural lane.
	"plural_endpoint_carried_hop_property_stays_plural.cypher": `p2.personOnly missing on plural-satisfying type Employee&Person`,
	// Names `p2` and the Person branch: the refusal has to come from the plural
	// lane on the binding AFTER the carry, not from `c` itself. `c` is singular
	// here (Person&Employee has one WORKS_AT), so a message about `c` would mean
	// the carry, not the covering mark, was what Phase C could not use.
	"carried_uncovered_endpoint_stays_plural.cypher": `p2.employeeId missing on plural-satisfying type Person`,
	// Says `c` went to the PLURAL lane. Without it the sentinel alone would be
	// satisfied by a `c` pinned to Company&Large — same variable, same property,
	// same ErrUnknownProperty — which is the opposite reading of the same gate.
	"unlabelled_hop_to_carried_far_end_stays_wide.cypher": `c.smallOnly missing on plural-satisfying type Company&Large`,
}

type ResolverSuite struct {
	suite.Suite
}

func TestResolverSuite(t *testing.T) {
	suite.Run(t, new(ResolverSuite))
}

// loadMapping reads a schema.mapping.json in the given fixture subdir.
func (s *ResolverSuite) loadMapping(subdir string) map[string]string {
	path := filepath.Join(fixtureDir, subdir, "schema.mapping.json")
	src, err := os.ReadFile(path)
	s.Require().NoError(err)
	var m map[string]string
	s.Require().NoError(json.Unmarshal(src, &m))
	return m
}

// loadSchema parses a GQL schema fixture from the shared schemas/ subdir.
func (s *ResolverSuite) loadSchema(subdir, name string) schema.Schema {
	path := filepath.Join(fixtureDir, subdir, "schemas", name)
	src, err := os.ReadFile(path)
	s.Require().NoError(err)
	sch, err := gql.New().Parse(bytes.NewReader(src))
	s.Require().NoError(err)
	return sch
}

// loadQuery parses a Cypher query fixture. R7 threads regR7 into the parser
// so CALL fixtures resolve procedure signatures; non-CALL fixtures parse
// identically because the parser consults the registry only inside
// collectCall (verified against internal/query/cypher/call.go:41), so all
// R0–R6 goldens stay byte-identical.
func (s *ResolverSuite) loadQuery(path string) query.Query {
	src, err := os.ReadFile(path)
	s.Require().NoError(err)
	q, err := cypher.New(cypher.WithRegistry(regR7)).Parse(bytes.NewReader(src))
	s.Require().NoError(err)
	return q
}

// TestValid walks valid/*.cypher: parse each, resolve against its paired
// schema, and either write the golden (-update) or JSONEq against it.
func (s *ResolverSuite) TestValid() {
	files, err := filepath.Glob(filepath.Join(fixtureDir, "valid", "*.cypher"))
	s.Require().NoError(err)
	s.Require().NotEmpty(files)

	mapping := s.loadMapping("valid")
	s.Require().Len(mapping, len(files), "schema.mapping.json must be total against valid/*.cypher")

	for _, path := range files {
		name := filepath.Base(path)
		s.Run(name, func() {
			schemaName, ok := mapping[name]
			s.Require().True(ok, "unmapped valid fixture %q", name)

			sch := s.loadSchema("valid", schemaName)
			q := s.loadQuery(path)

			vq, err := New(sch, WithRegistry(regR7)).Resolve(q)
			s.Require().NoError(err)

			got, err := json.MarshalIndent(vq, "", "  ")
			s.Require().NoError(err)

			goldenPath := path + ".validated.golden.json"
			if *update {
				s.Require().NoError(os.WriteFile(goldenPath, append(got, '\n'), 0o644))
				return
			}
			want, err := os.ReadFile(goldenPath)
			s.Require().NoError(err, "missing golden file; run go test -update")
			s.JSONEq(string(want), string(got))
		})
	}
}

// TestInvalid walks invalid/*.cypher: parse each, resolve against its paired
// schema, and assert (a) the returned ValidatedQuery is the zero value and
// (b) the error is the mapped sentinel via errors.Is.
func (s *ResolverSuite) TestInvalid() {
	files, err := filepath.Glob(filepath.Join(fixtureDir, "invalid", "*.cypher"))
	s.Require().NoError(err)
	s.Require().NotEmpty(files)

	mapping := s.loadMapping("invalid")
	s.Require().Len(mapping, len(files), "schema.mapping.json must be total against invalid/*.cypher")
	s.Require().Len(invalidFixtures, len(files), "invalidFixtures must be total against invalid/*.cypher")

	for _, path := range files {
		name := filepath.Base(path)
		s.Run(name, func() {
			schemaName, ok := mapping[name]
			s.Require().True(ok, "unmapped invalid fixture %q", name)
			wantErr, ok := invalidFixtures[name]
			s.Require().True(ok, "invalid fixture %q missing from invalidFixtures", name)

			sch := s.loadSchema("invalid", schemaName)
			q := s.loadQuery(path)

			vq, err := New(sch, WithRegistry(regR7)).Resolve(q)
			s.Require().Error(err)
			s.Equal(ValidatedQuery{}, vq, "model must be the zero value on error")
			s.Require().ErrorIs(err, wantErr)
			if substr, ok := invalidFixtureContains[name]; ok {
				s.Require().ErrorContains(err, substr)
			}
		})
	}
}

// TestDirectionMarkerIsInertOnAPluralOnlyUnion holds the claim the
// plural_endpoint_*_edge_union pair exists to make: on a candidate set the
// direction marker does not change, the marker does not change the verdict
// either. The undirected pattern's two extra probes (`Company -> Person`
// orientations of FOUNDED) are declared by nothing, so undirected and directed
// close to the same two keys, and §4.6 case D must type both.
//
// Neither golden can carry this on its own. Both are machine-written by the
// code under test from the same run, so a change that made the marker decide
// the verdict — an orientation guard that fires on a set with no orientation
// disagreement in it — writes two now-different goldens on the next -update
// and the suite stays green. Byte-equality between them is a relation -update
// cannot repair: it rewrites the files, not the fact that they must match.
//
// Read from disk rather than resolved here on purpose. The committed bytes are
// what a reviewer diffs, and this is the assertion that says a diff touching
// one of them and not the other is a regression.
func (s *ResolverSuite) TestDirectionMarkerIsInertOnAPluralOnlyUnion() {
	golden := func(name string) []byte {
		src, err := os.ReadFile(filepath.Join(fixtureDir, "valid", name+".cypher.validated.golden.json"))
		s.Require().NoError(err)
		return src
	}
	undirected := golden("plural_endpoint_undirected_edge_union")
	directed := golden("plural_endpoint_directed_edge_union")

	// Vacuity guard: byte-equality says nothing unless both goldens really do
	// hold the multi-candidate shape the claim is about.
	s.Require().Contains(string(undirected), `"kind": "edgeUnion"`)

	s.Require().Equal(string(undirected), string(directed),
		"the undirected fixture and its directed control close to the same candidate set, so their goldens must be byte-identical")
}

// TestOrientationDisagreementComparesOnlySameLabelCandidates holds
// orientationDisagreement to the precondition its doc comment states, rather
// than to the one its caller happens to supply.
//
// Case C's `len(e.Labels()) == 1` guard means every set the function is handed
// today carries one label, so no corpus fixture can reach the arm below — a
// predicate that ignored the label would pass the whole suite. That is the
// shape the previous revision shipped, and 25 lines of distance between a
// stated precondition and the guard that supplies it is not a guarantee.
//
// The arm matters because the two answers are different verdicts, not two
// spellings of one. Two DIFFERENT edge types running opposite ways across the
// pattern is §4.6 case D's multi-type union — the author wrote `|` and opted
// in — and ErrAmbiguousEdgeOrientation's message ("cannot commit to one
// without erasing the other") would be false of it.
//
// Called directly: the point is the function's answer on a set the resolver
// cannot currently build, so there is no query to write.
func (s *ResolverSuite) TestOrientationDisagreementComparesOnlySameLabelCandidates() {
	key := func(src, label, tgt string) schema.EdgeKey {
		return schema.EdgeKey{
			Source:    graph.LabelSetKey(src),
			KeyLabels: graph.LabelSetKey(label),
			Target:    graph.LabelSetKey(tgt),
		}
	}
	srcs := []graph.LabelSetKey{"Author"}
	tgts := []graph.LabelSetKey{"Book"}

	_, _, differentLabels := orientationDisagreement([]schema.EdgeKey{
		key("Author", "REVIEWED", "Book"),
		key("Book", "EDITED", "Author"),
	}, srcs, tgts)
	s.Require().False(differentLabels,
		"REVIEWED one way and EDITED the other is a multi-type union (§4.6 case D), not one edge type whose direction is undecided")

	// Control: the same two sides, one label, is the disagreement.
	_, _, sameLabel := orientationDisagreement([]schema.EdgeKey{
		key("Author", "REVIEWED", "Book"),
		key("Book", "REVIEWED", "Author"),
	}, srcs, tgts)
	s.Require().True(sameLabel,
		"the assertion above must fail on the label, not on the sides")
}

// TestOrientationDisagreementSkipsOnlyWhatReadsBothWays states the property
// that licenses the skip arm, on the slice shapes that separate it from the
// weaker test it is easy to write instead.
//
// The skip exists because a candidate that reads the same whichever way it is
// read carries no orientation signal, and reporting it would name one key as
// its own counterparty. Reading BOTH ways is a claim about both endpoints. A
// candidate whose SOURCE happens to sit in both slices is a strictly weaker
// condition, and the two come apart the moment the endpoints' satisfying sets
// overlap without being equal — the ordinary shape of a subtype schema, which
// is what ADR 0022 exists to serve.
//
// The overlap rows below are not reachable through the invalid corpus alone in
// the sense that matters: a Source-only test does not merely misjudge one
// candidate on them, it classifies EVERY candidate as signal-free, so the
// function can never return true for any query whose srcs is contained in its
// tgts. The rows say that directly, where the justification for the skip lives.
func (s *ResolverSuite) TestOrientationDisagreementSkipsOnlyWhatReadsBothWays() {
	key := func(src, label, tgt string) schema.EdgeKey {
		return schema.EdgeKey{
			Source:    graph.LabelSetKey(src),
			KeyLabels: graph.LabelSetKey(label),
			Target:    graph.LabelSetKey(tgt),
		}
	}
	mirror := []schema.EdgeKey{
		key("Employee&Person", "REVIEWED", "Person"),
		key("Person", "REVIEWED", "Employee&Person"),
	}
	tests := []struct {
		name string
		srcs []graph.LabelSetKey
		tgts []graph.LabelSetKey
		want bool
		why  string
	}{
		{
			name: "srcs is a strict subset of tgts",
			srcs: []graph.LabelSetKey{"Employee&Person"},
			tgts: []graph.LabelSetKey{"Person", "Employee&Person"},
			want: true,
			why:  "every Source sits in both slices, but the Targets still separate the two readings",
		},
		{
			name: "tgts is a strict subset of srcs",
			srcs: []graph.LabelSetKey{"Person", "Employee&Person"},
			tgts: []graph.LabelSetKey{"Employee&Person"},
			want: true,
			why:  "the mirror image of the row above; the witnesses swap sides, the verdict does not",
		},
		{
			name: "srcs and tgts are equal",
			srcs: []graph.LabelSetKey{"Person", "Employee&Person"},
			tgts: []graph.LabelSetKey{"Person", "Employee&Person"},
			want: false,
			why:  "the directed twin probes the same set, so the arrow is inert and the refusal would be advice the author cannot act on",
		},
		{
			name: "srcs and tgts are disjoint",
			srcs: []graph.LabelSetKey{"Employee&Person"},
			tgts: []graph.LabelSetKey{"Person"},
			want: true,
			why:  "no overlap at all — the shape that worked before ADR 0022, kept as the control",
		},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			fwd, rev, got := orientationDisagreement(mirror, tt.srcs, tt.tgts)
			s.Require().Equalf(tt.want, got, "%s", tt.why)
			if got {
				s.Require().NotEqual(fwd, rev, "the two witnesses must be different keys")
			}
		})
	}

	// The self-loop the equal-slices row generalises: one key that is its own
	// counterparty, which is why the skip is not merely permitted but required.
	_, _, selfLoop := orientationDisagreement(
		[]schema.EdgeKey{key("Person", "KNOWS", "Person")},
		[]graph.LabelSetKey{"Person"}, []graph.LabelSetKey{"Person"})
	s.Require().False(selfLoop,
		"a self-loop key read as a disagreement would name itself on both sides")
}

// TestAmbiguousOrientationRemedyIsTheArrow holds the claim case C's whole
// rationale rests on: the resolver refuses and tells the author to write an
// arrow, so on every schema where it refuses, writing the arrow must decide the
// question. A refusal whose prescribed remedy leaves the query equally
// ambiguous is advice the author cannot act on, and §4.6's plural-endpoint
// carve-out exists precisely to avoid emitting one.
//
// One schema per row, both forms resolved against it. That is what the
// valid/invalid fixture pairs cannot state: the directed control lives in
// valid/ with its own copy of the schema, so nothing says the schema the
// control resolves against is the schema the refusal fires on. Here it is the
// same parsed value, and the directed answer is asserted down to the committed
// EdgeKey rather than left to a machine-written golden.
func (s *ResolverSuite) TestAmbiguousOrientationRemedyIsTheArrow() {
	tests := []struct {
		name       string
		schema     string
		undirected string
		directed   string
		wantKey    schema.EdgeKey
	}{
		{
			name:       "exact mirror between two node types",
			schema:     "social_r3.gql",
			undirected: "MATCH (p:Person)-[r:AUTHORED]-(post:Post) RETURN r",
			directed:   "MATCH (p:Person)-[r:AUTHORED]->(post:Post) RETURN r",
			wantKey:    schema.EdgeKey{Source: "Person", KeyLabels: "AUTHORED", Target: "Post"},
		},
		{
			name:       "reverse declaration lands on a subtype (§4.6.1)",
			schema:     "satisfy_plural_edges_reversed_subtype.gql",
			undirected: "MATCH (a:Author)-[r:REVIEWED]-(b:Book) RETURN r",
			directed:   "MATCH (a:Author)-[r:REVIEWED]->(b:Book) RETURN r",
			wantKey:    schema.EdgeKey{Source: "Author", KeyLabels: "REVIEWED", Target: "Book"},
		},
		{
			name:       "endpoints' satisfying sets overlap, srcs inside tgts",
			schema:     "satisfy_plural_edges_overlapping.gql",
			undirected: "MATCH (a:Employee)-[r:REVIEWED]-(b:Person) RETURN r",
			directed:   "MATCH (a:Employee)-[r:REVIEWED]->(b:Person) RETURN r",
			wantKey:    schema.EdgeKey{Source: "Employee&Person", KeyLabels: "REVIEWED", Target: "Person"},
		},
		{
			name:       "endpoints' satisfying sets overlap, tgts inside srcs",
			schema:     "satisfy_plural_edges_overlapping.gql",
			undirected: "MATCH (a:Person)-[r:REVIEWED]-(b:Employee) RETURN r",
			directed:   "MATCH (a:Person)-[r:REVIEWED]->(b:Employee) RETURN r",
			wantKey:    schema.EdgeKey{Source: "Person", KeyLabels: "REVIEWED", Target: "Employee&Person"},
		},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			// invalid/ on purpose: the refusal is what the schema is for, so
			// that is where the one authoritative copy lives.
			sch := s.loadSchema("invalid", tt.schema)

			parse := func(src string) query.Query {
				q, err := cypher.New(cypher.WithRegistry(regR7)).Parse(bytes.NewReader([]byte(src)))
				s.Require().NoError(err)
				return q
			}

			_, err := New(sch, WithRegistry(regR7)).Resolve(parse(tt.undirected))
			s.Require().ErrorIs(err, ErrAmbiguousEdgeOrientation,
				"the undirected form must be the one that refuses")

			vq, err := New(sch, WithRegistry(regR7)).Resolve(parse(tt.directed))
			s.Require().NoError(err, "writing the arrow must be a remedy that works")
			s.Require().Len(vq.Columns, 1)
			s.Require().Equal(ResolvedEdge{EdgeKey: tt.wantKey}, vq.Columns[0].Type,
				"the arrow must close the set to one candidate — case B, not a union case D would type")
		})
	}
}

// TestMixedSymmetryIsAcceptedAndTheMarkerNarrows holds §4.6's third shape: a
// candidate that reads both ways alongside candidates that read exactly one
// way, all on the same side. Nothing disagrees, so the verdict table accepts —
// and unlike the shape where *every* candidate reads both ways, the arrow is
// not inert, because the one-way candidates sit outside the directed probe set.
// So the author's choice of marker decides the result type: a two-key union
// undirected, a single edge directed.
//
// Both arities are hand-written. The corpus fixtures for this shape carry
// machine-written goldens, so a change that collapsed the two arities into one
// would be absorbed by the next -update run with the goldens still agreeing
// with themselves; the difference between them is the whole claim, so it is
// stated somewhere -update cannot reach.
//
// The three resolves are also what says the shape is mixed rather than either
// pure one, without restating the classification predicate in the test: a
// candidate that reads both ways is in *both* directed twins' candidate sets, a
// candidate that reads one way is in exactly one, and the undirected set is
// their union.
func (s *ResolverSuite) TestMixedSymmetryIsAcceptedAndTheMarkerNarrows() {
	sch := s.loadSchema("valid", "satisfy_plural_edges_mixed_symmetry.gql")
	bothWays := schema.EdgeKey{Source: "Manager&Person&Staff", KeyLabels: "MENTORS", Target: "Engineer&Person&Staff"}
	oneWay := schema.EdgeKey{Source: "Person", KeyLabels: "MENTORS", Target: "Engineer&Person&Staff"}

	resolve := func(src string) ResolvedType {
		q, err := cypher.New(cypher.WithRegistry(regR7)).Parse(bytes.NewReader([]byte(src)))
		s.Require().NoError(err)
		vq, err := New(sch, WithRegistry(regR7)).Resolve(q)
		s.Require().NoErrorf(err, "%s: mixed symmetry is accepted, not refused", src)
		s.Require().Len(vq.Columns, 1)
		return vq.Columns[0].Type
	}

	s.Require().Equal(ResolvedEdgeUnion{EdgeKeys: []schema.EdgeKey{bothWays, oneWay}},
		resolve("MATCH (a:Staff)-[r:MENTORS]-(b:Person) RETURN r"),
		"undirected admits both readings, so both candidates join the union")
	s.Require().Equal(ResolvedEdge{EdgeKey: bothWays},
		resolve("MATCH (a:Staff)-[r:MENTORS]->(b:Person) RETURN r"),
		"the arrow drops the one-way candidate, so the marker is not inert on this shape")
	s.Require().Equal(ResolvedEdgeUnion{EdgeKeys: []schema.EdgeKey{bothWays, oneWay}},
		resolve("MATCH (a:Person)-[r:MENTORS]->(b:Staff) RETURN r"),
		"the reversed arrow keeps both, which is what makes the surviving candidate the symmetric one")
}

// TestEdgeClosureNarrowsThePluralEndpointsItPins holds the widening gqlc-0tft
// is: a committed edge candidate names one node type on each of its ends, so
// the plural binding at that end of the pattern is constrained by the closure
// exactly as an unlabelled one is by Phase B (R3 §4.5.2). Before this, the
// closure wrote only the edge lanes and the node binding stayed plural, so ADR
// 0022's whole-entity refusal fired on a binding the schema had already pinned.
//
// Hand-written rather than left to the corpus goldens. Two of the three rows
// assert an answer no golden can carry without being blessed into whatever the
// code produces — the committed node type and the property that only the
// narrowed type declares — and the third is the one that separates
// intersection-across-edges from union-across-edges, a distinction a golden
// records as bytes rather than as a claim.
//
// The schema lives in invalid/ because that is where the refusals it also
// carries are pinned; the copy is one parsed value here.
func (s *ResolverSuite) TestEdgeClosureNarrowsThePluralEndpointsItPins() {
	sch := s.loadSchema("invalid", "satisfy_plural_edges_reversed.gql")
	resolve := func(src string) []Column {
		q, err := cypher.New(cypher.WithRegistry(regR7)).Parse(bytes.NewReader([]byte(src)))
		s.Require().NoError(err)
		vq, err := New(sch, WithRegistry(regR7)).Resolve(q)
		s.Require().NoErrorf(err, "%s: the closure pins this endpoint, so the resolver has what it needs", src)
		return vq.Columns
	}

	// The only declared WORKS_AT runs Employee&Person -> Company, so `p` cannot
	// be the bare Person type — and the whole entity is nameable once it is not.
	s.Require().Equal([]Column{{Name: "p", Type: ResolvedNode{Labels: "Employee&Person"}}},
		resolve("MATCH (p:Person)-[r:WORKS_AT]-(c:Company) RETURN p"))

	// employeeId is declared on Employee&Person and NOT on Person, so this
	// column exists only because the narrowing dropped Person: the ADR 0022
	// intersection over both candidates has no such property.
	s.Require().Equal([]Column{{Name: "p.employeeId", Type: ResolvedProperty{Type: graph.PropertyType("INT")}}},
		resolve("MATCH (p:Person)-[r:WORKS_AT]-(c:Company) RETURN p.employeeId"))

	// Two edges touch `p`: WORKS_AT contributes {Employee&Person} and the
	// directed REVIEWED contributes {Person, Employee&Person} (both are
	// declared). Their INTERSECTION is the singleton; their union is `p`'s
	// whole satisfying set, which narrows nothing and leaves this refused.
	s.Require().Equal([]Column{{Name: "p", Type: ResolvedNode{Labels: "Employee&Person"}}},
		resolve("MATCH (p:Person)-[w:WORKS_AT]->(co:Company), (p)-[r:REVIEWED]->(c2:Company) RETURN p"))
}

// TestEdgeClosureNarrowingCannotOutrunTheFacts is the other half of the
// widening: the three shapes where the closure does NOT determine the endpoint,
// and the binding must stay plural and stay refused. A widening that also
// accepts these is not a widening, it is a wrong answer — and each row fails on
// a different way of getting the contribution's shape wrong, so together they
// say the rule is "union across the candidate set's two sides, then intersect
// across touching edges" and not any of its neighbours.
//
// Rows 1 and 2 run on the mixed-symmetry schema, whose undirected candidate set
// puts each binding on BOTH sides: `Manager&Person&Staff-[MENTORS]->
// Engineer&Person&Staff` reads the same either way round, and
// `Person-[MENTORS]->Engineer&Person&Staff` reads right-to-left only. Taking
// only the left-to-right reading narrows `a` to Manager&Person&Staff and `b` to
// Engineer&Person&Staff; taking only the right-to-left reading narrows them the
// other way. Both are wrong, both make these rows resolve, and no corpus
// fixture on that schema can see it — every one of them projects the edge.
//
// Row 3 is the empty intersection. Two edges pin `p` to disjoint types, so
// nothing satisfies both and the closure has narrowed the candidate set to
// nothing. The pre-closure satisfying set stands (§4.5.2's intersection is a
// refinement of label satisfaction, not a replacement for it) and the ADR 0022
// refusal fires on it unchanged — see the companion assertion below, which is
// what says the empty case leaves the query resolvable rather than refusing it
// outright.
func (s *ResolverSuite) TestEdgeClosureNarrowingCannotOutrunTheFacts() {
	tests := []struct {
		name   string
		dir    string
		schema string
		query  string
		want   []graph.LabelSetKey
	}{
		{
			name:   "undirected candidate set puts the left endpoint on both sides",
			dir:    "valid",
			schema: "satisfy_plural_edges_mixed_symmetry.gql",
			query:  "MATCH (a:Staff)-[r:MENTORS]-(b:Person) RETURN a",
			want:   []graph.LabelSetKey{"Engineer&Person&Staff", "Manager&Person&Staff"},
		},
		{
			name:   "undirected candidate set puts the right endpoint on both sides",
			dir:    "valid",
			schema: "satisfy_plural_edges_mixed_symmetry.gql",
			query:  "MATCH (a:Staff)-[r:MENTORS]-(b:Person) RETURN b",
			want:   []graph.LabelSetKey{"Engineer&Person&Staff", "Manager&Person&Staff", "Person"},
		},
		{
			name:   "two touching edges pin disjoint types, so the intersection is empty",
			dir:    "invalid",
			schema: "satisfy_plural_edges_reversed.gql",
			query:  "MATCH (p:Person)-[w:WORKS_AT]->(co:Company), (c:Company)-[r:REVIEWED]->(p) RETURN p",
			want:   []graph.LabelSetKey{"Employee&Person", "Person"},
		},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			sch := s.loadSchema(tt.dir, tt.schema)
			q, err := cypher.New(cypher.WithRegistry(regR7)).Parse(bytes.NewReader([]byte(tt.query)))
			s.Require().NoError(err)
			_, err = New(sch, WithRegistry(regR7)).Resolve(q)
			s.Require().ErrorIs(err, ErrAmbiguousLabel,
				"the closure does not determine this endpoint, so the whole-entity refusal stands")
			// The message enumerates the surviving candidates, so it says WHICH
			// set stood — a narrowing that dropped one and still refused (because
			// two remain) passes errors.Is and fails here.
			for _, k := range tt.want {
				s.Require().ErrorContains(err, string(k))
			}
			s.Require().Len(strings.Split(errAmbiguousLabelSet(err), ", "), len(tt.want),
				"the refusal must name exactly the pre-closure satisfying set")
		})
	}

	// The empty intersection leaves the binding resolvable, it does not refuse
	// the query. Refusing would be a narrowing of an accepted query (ADR 0006)
	// with no soundness case behind it: the pattern matches no row, which is a
	// fact about the data, not about the types the projection names.
	sch := s.loadSchema("invalid", "satisfy_plural_edges_reversed.gql")
	q, err := cypher.New(cypher.WithRegistry(regR7)).Parse(bytes.NewReader([]byte(
		"MATCH (p:Person)-[w:WORKS_AT]->(co:Company), (c:Company)-[r:REVIEWED]->(p) RETURN p.name")))
	s.Require().NoError(err)
	vq, err := New(sch, WithRegistry(regR7)).Resolve(q)
	s.Require().NoError(err, "an empty intersection falls back to label satisfaction; it is not a refusal")
	s.Require().Equal([]Column{{Name: "p.name", Type: ResolvedProperty{Type: graph.PropertyType("STRING")}}}, vq.Columns)
}

// TestABareWithCarriesAPluralBindingForwards pins the widening: a non-final
// Part's projection is a CARRY, not a user-visible column, so a bare `WITH v`
// naming a plural binding no longer has to name a single type. The candidate
// set travels in branchState.exportedNodeCands — a lane Export has always
// written and no downstream Part could ever read, because this refusal fired
// one Part earlier (the claim scope.go's pluralByInference comment used to
// rest on).
//
// The first two rows are the two halves of the same fault and only one of them
// is what gqlc-etj6 describes. Row 2 is the bead's query, where Part 1's edge
// narrows `p`. Row 1 has no edge and no narrowing anywhere: it is refused
// purely for crossing a WITH, while the identical one-Part spelling resolves.
// That row is what says the defect is the premature refusal rather than the
// ORDER the narrowing and the carry run in — there is no carry check to
// reorder, and a fix that only moved the narrowing would leave row 1 refused.
func (s *ResolverSuite) TestABareWithCarriesAPluralBindingForwards() {
	sch := s.loadSchema("invalid", "satisfy_plural_edges_reversed.gql")
	resolve := func(src string) []Column {
		q, err := cypher.New(cypher.WithRegistry(regR7)).Parse(bytes.NewReader([]byte(src)))
		s.Require().NoError(err)
		vq, err := New(sch, WithRegistry(regR7)).Resolve(q)
		s.Require().NoErrorf(err, "%s", src)
		return vq.Columns
	}

	id := []Column{{Name: "p.id", Type: ResolvedProperty{Type: graph.PropertyType("INT")}}}
	employeeID := []Column{{Name: "p.employeeId", Type: ResolvedProperty{Type: graph.PropertyType("INT")}}}
	employeePerson := []Column{{Name: "p", Type: ResolvedNode{Labels: "Employee&Person"}}}

	// A WITH is transparent to plural satisfaction. `id` is declared on both
	// candidates, so ADR 0022's intersection names it either way — and the
	// one-Part spelling below already resolves on master. The two must agree:
	// nothing about crossing a Part boundary changes which types satisfy `p`.
	s.Require().Equal(id, resolve("MATCH (p:Person) RETURN p.id"))
	s.Require().Equal(id, resolve("MATCH (p:Person) WITH p RETURN p.id"))

	// gqlc-etj6's own query. The only declared WORKS_AT is
	// Employee&Person -> Company, so Part 1's closure pins `p` — the carried
	// candidate set is narrowed by exactly the mechanism that narrows a
	// locally-bound one.
	s.Require().Equal(id, resolve("MATCH (p:Person) WITH p MATCH (p)-[w:WORKS_AT]->(c:Company) RETURN p.id"))

	// employeeId is declared on Employee&Person and NOT on Person, so this
	// column exists only because the carried set was narrowed downstream. The
	// union over both candidates has no such property, which is what makes
	// this row evidence of narrowing rather than of the carry alone.
	s.Require().Equal(employeeID,
		resolve("MATCH (p:Person) WITH p MATCH (p)-[w:WORKS_AT]->(c:Company) RETURN p.employeeId"))

	// The whole entity is nameable in the final Part once the narrowing has
	// run, so the refusal that used to fire in Part 0 does not simply move
	// downstream.
	s.Require().Equal(employeePerson,
		resolve("MATCH (p:Person) WITH p MATCH (p)-[w:WORKS_AT]->(c:Company) RETURN p"))

	// WITH * reaches the same rule through materialiseReturns' synthesised
	// items rather than through s.returns, so it is a separate code path to
	// the same predicate and not a restatement of the row above.
	s.Require().Equal(employeePerson,
		resolve("MATCH (p:Person) WITH * MATCH (p)-[w:WORKS_AT]->(c:Company) RETURN p"))

	// Two carries in sequence: Part 1 reads `p` from the carry and re-exports
	// it without ever binding it locally, which is the only shape where the
	// carried candidate set has to survive a Part that does not declare it.
	s.Require().Equal(employeePerson,
		resolve("MATCH (p:Person) WITH p WITH p MATCH (p)-[w:WORKS_AT]->(c:Company) RETURN p"))
}

// TestAPluralCarryStillRefusesWhereNothingNarrowsIt is the other half. A
// widening that accepts these is not a widening, it is a wrong answer: each row
// is a shape where the carry must NOT be allowed to launder an unnameable type
// into an accepted query, and each fails on a different way of getting the
// deferral's boundary wrong.
//
// Rows 1 and 2 say the refusal is DEFERRED, not deleted — it fires in the Part
// that actually projects the entity to the user. Row 3 is the aliased carry,
// and it is the reason the rule is spelled on the bare self-named projection
// rather than on "any whole-entity projection in a non-final Part": Export
// records a nodeCands entry only when the alias equals the variable, so
// `WITH p AS q` drops the candidate set on the floor. Deferring there would
// carry a name downstream with its satisfying set lost, which is strictly worse
// than refusing. Row 4, after the loop, is the other conjunct of that same
// spelling: a PROPERTY projection whose alias happens to equal the variable.
func (s *ResolverSuite) TestAPluralCarryStillRefusesWhereNothingNarrowsIt() {
	sch := s.loadSchema("invalid", "satisfy_plural_edges_reversed.gql")
	refuse := func(src string) error {
		q, err := cypher.New(cypher.WithRegistry(regR7)).Parse(bytes.NewReader([]byte(src)))
		s.Require().NoError(err)
		_, err = New(sch, WithRegistry(regR7)).Resolve(q)
		s.Require().Errorf(err, "%s: nothing here names a single type for p", src)
		return err
	}

	for _, src := range []string{
		// The final Part projects the whole entity and no edge ever narrowed
		// it — the deferral has to arrive somewhere.
		"MATCH (p:Person) WITH p RETURN p",
		// Part 1 has an edge, but the directed REVIEWED is declared on BOTH
		// person types, so its contribution is p's whole satisfying set and
		// narrows nothing. This is the row that says the acceptance above
		// comes from the narrowing and not from the carry being permissive.
		"MATCH (p:Person) WITH p MATCH (p)-[r:REVIEWED]->(c:Company) RETURN p",
		// Aliased carry: `q` reaches Part 1 as a projection alias with no
		// candidate set behind it, so the plural fault must be reported where
		// the set still exists.
		"MATCH (p:Person) WITH p AS q RETURN q",
	} {
		err := refuse(src)
		s.Require().ErrorIs(err, ErrAmbiguousLabel, "%s", src)
		// The message enumerates the surviving candidates, so a deferral that
		// dropped one on the way across the boundary and still refused
		// (because two remain) passes errors.Is and fails here.
		s.Require().ErrorContains(err, "Employee&Person", "%s", src)
		s.Require().ErrorContains(err, "Person", "%s", src)
	}

	// `WITH p.employeeId AS p` shadows the binding with a scalar under its own
	// name, so it satisfies the deferral's alias condition and is separated
	// only by the property one. It must still be refused: employeeId is absent
	// from the bare Person, and a property fault belongs to the Part that owns
	// the binding whether or not that Part is final. Deferring it would carry a
	// ResolvedUnknown named `p` past a real error, and Export records no
	// candidate set for a property projection, so no downstream Part could ever
	// report it — the query would ACCEPT. Its corpus twin is
	// valid/parameter_across_with_alias_shadow.cypher, whose three plural cells
	// are what the sweep catches; this row is what catches it without the
	// sweep, which is regenerable.
	err := refuse("MATCH (p:Person) WITH p.employeeId AS p RETURN p")
	s.Require().ErrorIs(err, ErrUnknownProperty)
	s.Require().ErrorContains(err, "p.employeeId missing on plural-satisfying type Person")
}

// TestACarriedPluralBindingKeepsItsOwnSentinel pins the lane the widening
// forces into existence. errors.go makes the two plural sentinels
// category-grained: ErrAmbiguousLabel for a LABEL SET with several satisfying
// types, ErrAmbiguousBinding for an UNLABELLED binding Phase B could not
// narrow. Which one fires is read off scope.pluralByInference, and nodeCands
// cannot answer it — its values are the same []schema.NodeType either way.
//
// Before this change no carry could hold a plural name at all, so that lane
// deliberately had no carry seed. Now that one can, an unlabelled binding
// crossing a WITH would come out the far side reported as a LABEL fault: the
// query names no label for it, so telling its author to disambiguate a label
// set is advice they cannot act on.
//
// The rows are asserted on the MESSAGE and not only the sentinel, because the
// two faults reach the reader through two different refusals and only one of
// them is the carried lane's. Phase B's terminal deferral ("cannot uniquely
// infer type of unlabelled binding") is also ErrAmbiguousBinding, and it fires
// in the Part that owns the binding — before any WITH, and without consulting
// pluralByInference at all. A row that lands there is green whether or not the
// lane crosses the boundary; the earlier version of this test was three such
// rows, and dropping both halves of the carry survived it. The rows that bite
// are the WIDENED ones, whose message is refProjectionType's
// "is satisfied by more than one declared node type".
func (s *ResolverSuite) TestACarriedPluralBindingKeepsItsOwnSentinel() {
	sentinel := func(sch schema.Schema, src string) error {
		q, err := cypher.New(cypher.WithRegistry(regR7)).Parse(bytes.NewReader([]byte(src)))
		s.Require().NoError(err)
		_, err = New(sch, WithRegistry(regR7)).Resolve(q)
		s.Require().Errorf(err, "%s", src)
		return err
	}

	// Phase B declines to pick a type for `p` at all, so the refusal never
	// reaches a projection. Stable across WITHs for the trivial reason that no
	// WITH is reached — which is exactly why these rows cannot stand alone.
	reversed := s.loadSchema("invalid", "satisfy_plural_edges_reversed.gql")
	for _, src := range []string{
		"MATCH (p)-[r:REVIEWED]->(c:Company) RETURN p",
		"MATCH (p)-[r:REVIEWED]->(c:Company) WITH p RETURN p",
		"MATCH (p)-[r:REVIEWED]->(c:Company) WITH p WITH p RETURN p",
	} {
		err := sentinel(reversed, src)
		s.Require().ErrorIs(err, ErrAmbiguousBinding,
			"%s: p carries no label, so this is an inference fault either side of a WITH", src)
		s.Require().NotErrorIs(err, ErrAmbiguousLabel, "%s", src)
		s.Require().ErrorContains(err, `cannot uniquely infer type of unlabelled binding "p"`, "%s", src)
	}

	// The lane's own rows. `c` is WIDENED here rather than deferred — the
	// OPTIONAL HAS_DESK hop is not a witness, so Phase B commits both company
	// types into nodeCands and marks pluralByInference — and the whole-entity
	// projection that refuses it is now deferred across every WITH in front of
	// it. The zero-WITH row is the control that says the message and set are
	// the Part's own answer before any carry is involved; the others say the
	// carry reproduces it verbatim.
	inline := s.loadSchema("invalid", "satisfy_plural_edges_inline_subtype.gql")
	const widened = "MATCH (p:Person)-[q:WORKS_AT]->(c) OPTIONAL MATCH (c)-[h:HAS_DESK]->(d:Desk) "
	for _, src := range []string{
		widened + "RETURN c",
		widened + "WITH c RETURN c",
		widened + "WITH c WITH c RETURN c",
	} {
		err := sentinel(inline, src)
		s.Require().ErrorIs(err, ErrAmbiguousBinding,
			"%s: c carries no label, so the widened set is an inference fault", src)
		s.Require().NotErrorIs(err, ErrAmbiguousLabel, "%s", src)
		s.Require().ErrorContains(err,
			"c is satisfied by more than one declared node type: Company, Company&Large", "%s", src)
	}

	// The discriminator. Same schema, same candidate set, same message tail,
	// same number of WITHs to cross — and a LABEL fault, because `c` is spelled
	// with one. Without this row every assertion above is satisfied by a
	// resolver that answers ErrAmbiguousBinding unconditionally.
	for _, src := range []string{
		"MATCH (c:Company) RETURN c",
		"MATCH (c:Company) WITH c RETURN c",
		"MATCH (c:Company) WITH c WITH c RETURN c",
	} {
		err := sentinel(inline, src)
		s.Require().ErrorIs(err, ErrAmbiguousLabel,
			"%s: c is spelled with a label, so the same set is a label fault", src)
		s.Require().NotErrorIs(err, ErrAmbiguousBinding, "%s", src)
		s.Require().ErrorContains(err,
			"c is satisfied by more than one declared node type: Company, Company&Large", "%s", src)
	}
}

// TestNarrowingLearnsOnlyFromEdgesEveryRowHas is the accepted half of the six
// invalid/plural_endpoint_*_stays_plural fixtures: the same query with the one
// clause that breaks the guarantee removed, which must still commit.
//
// An edge binding names a relationship between two declared node types, but it
// only says anything about the ROWS the query returns when every returned row
// is guaranteed to have that edge. The fixtures are the shapes where it is not,
// and the pairs below are what say each of them is refused BECAUSE of that and
// not because the shape is unresolvable for some unrelated reason — a fixture
// refused for the wrong reason passes the sentinel check and covers nothing.
//
// They are equally the guard against the predicate being applied too widely.
// Every accepted twin here is a query that the widening exists to accept, so a
// witnessesItsEndpoints that answered false too often shows up as a refusal on
// this side rather than as silence.
//
// Read alongside TestEdgeClosureNarrowsThePluralEndpointsItPins, which pins the
// plain mandatory single-hop edge on the same schema.
func (s *ResolverSuite) TestNarrowingLearnsOnlyFromEdgesEveryRowHas() {
	sch := s.loadSchema("invalid", "satisfy_plural_edges_reversed.gql")
	resolve := func(src string) ([]Column, error) {
		q, err := cypher.New(cypher.WithRegistry(regR7)).Parse(bytes.NewReader([]byte(src)))
		s.Require().NoError(err)
		vq, err := New(sch, WithRegistry(regR7)).Resolve(q)
		return vq.Columns, err
	}
	employeePerson := []Column{{Name: "p", Type: ResolvedNode{Labels: "Employee&Person"}}}

	tests := []struct {
		name    string
		fixture string
		twin    string
		want    []Column
	}{
		{
			// OPTIONAL MATCH is an outer join: a bare Person with no WORKS_AT
			// still produces a row, and that row's p is not an Employee.
			name:    "an OPTIONAL edge is not a witness, a required one is",
			fixture: "plural_endpoint_optional_edge_stays_plural.cypher",
			twin:    "MATCH (p:Person) MATCH (p)-[r:WORKS_AT]->(c:Company) RETURN p",
			want:    employeePerson,
		},
		{
			// The sharp end. employeeId is declared on Employee&Person and not
			// on Person, so narrowing off the OPTIONAL edge does not merely
			// mis-name an entity — it emits a NOT NULL column that is null for
			// every bare-Person row.
			name:    "the property form is where the wrong type reaches emitted code",
			fixture: "plural_endpoint_optional_edge_property_stays_plural.cypher",
			twin:    "MATCH (p:Person) MATCH (p)-[r:WORKS_AT]->(c:Company) RETURN p.employeeId",
			want:    []Column{{Name: "p.employeeId", Type: ResolvedProperty{Type: graph.PropertyType("INT")}}},
		},
		{
			// A zero lower bound admits the empty path, which degenerates to
			// p == c and declares nothing about either end. No OPTIONAL MATCH
			// is involved: a plain MATCH with this quantifier is enough.
			name:    "a zero-hop quantifier is not a witness, a one-hop one is",
			fixture: "plural_endpoint_zero_hop_stays_plural.cypher",
			twin:    "MATCH (p:Person)-[w:WORKS_AT*1]->(c:Company) RETURN p",
			want:    employeePerson,
		},
		{
			// The twin is `*1..1` and not `*1..2`. A one-hop LOWER bound is not
			// enough: the closure names the ends of one declared edge, and a range
			// that admits two hops puts the pattern's ends somewhere else. On this
			// schema `*1..2` happens to commit the right type anyway — no WORKS_AT
			// leaves Company, so no two-hop path exists to disagree — which is
			// exactly why it cannot be the twin here. It would assert a rule the
			// corpus structurally cannot falsify. The rule is pinned where it can
			// be falsified, on the chain schema, by
			// TestNarrowingLearnsOnlyFromASingleHopEdge.
			name:    "a zero-lower-bound range is not a witness, a range of exactly one hop is",
			fixture: "plural_endpoint_zero_lower_bound_stays_plural.cypher",
			twin:    "MATCH (p:Person)-[w:WORKS_AT*1..1]->(c:Company) RETURN p",
			want:    employeePerson,
		},
		{
			// The pair that separates singleHopPattern's two questions. Both
			// sides have an upper bound of one, so the hop-count half admits
			// both and only the lower bound decides: `*0..1` admits the empty
			// path, `*..1` has no lower bound written and openCypher reads that
			// as one, making it the closed range [1,1]. Without this pair the
			// zero-lower-bound test could be dropped from the predicate and the
			// remaining fixtures (`*0`, `*0..2`) would still be refused — by the
			// hop-count half, for the wrong reason.
			name:    "a zero lower bound is not a witness even under a one-hop ceiling, an absent one is",
			fixture: "plural_endpoint_zero_lower_bound_one_hop_stays_plural.cypher",
			twin:    "MATCH (p:Person)-[w:WORKS_AT*..1]->(c:Company) RETURN p",
			want:    employeePerson,
		},
		{
			// MERGE creates the pattern on miss, so every input row survives
			// it whatever its type. The edge is caused by the query, not
			// observed by it.
			name:    "a MERGEd edge is not a witness, a matched one is",
			fixture: "plural_endpoint_merged_edge_stays_plural.cypher",
			twin:    "MATCH (p:Person) MATCH (p)-[w:WORKS_AT]->(c:Company) RETURN p",
			want:    employeePerson,
		},
		{
			name:    "a CREATEd edge is not a witness, a matched one is",
			fixture: "plural_endpoint_created_edge_stays_plural.cypher",
			twin:    "MATCH (p:Person), (c:Company) MATCH (p)-[w:WORKS_AT]->(c) RETURN p",
			want:    employeePerson,
		},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			// The fixture is read from disk so the pair cannot drift: what is
			// refused in invalid/ and what is accepted here are one edit apart.
			src, err := os.ReadFile(filepath.Join(fixtureDir, "invalid", tt.fixture))
			s.Require().NoError(err)
			_, err = resolve(string(src))
			s.Require().Error(err, "%s must stay refused", tt.fixture)

			got, err := resolve(tt.twin)
			s.Require().NoError(err, "the twin removes only the clause that breaks the guarantee, so it must still commit")
			s.Require().Equal(tt.want, got)
		})
	}
}

// TestAnAnonymousWrittenBindingSilencesAnAnonymousMatchedEdge pins the
// over-approximation writtenBindings makes for ANONYMOUS positions, which
// every row above misses because all of them name their bindings.
//
// scope.writtenBindings reads CreateEffect.Variables / MergeEffect.Variables,
// and an anonymous position enters as the EMPTY STRING. witnessesItsEndpoints
// then asks `written[e.Variable()]`, and an anonymous MATCHED edge's Variable()
// is the empty string too. So a Part that writes any anonymous binding silences
// every anonymous matched edge in it, whether or not it is the same one.
//
// The pair below is the whole claim, and the two rows differ by ONE EDIT: the
// CREATE's bindings are named in the second and anonymous in the first. The
// MATCH is byte-identical across both, so nothing about the matched edge can
// explain the difference — only the contents of the written set can. That is
// what makes this a pin on writtenBindings rather than on the narrowing.
//
// THE FIRST ROW IS NOT ASSERTED TO BE CORRECT. It is asserted to be what the
// resolver does today. The widening is the SAFE direction — membership means
// "do not learn from this edge", which lands on the pre-narrowing answer, so
// the cost is a query refused that could have been accepted, never a wrong type
// committed. Both the implementer and the reviewer of
// feat/resolver-narrow-plural-endpoints measured the reachable shapes as
// landing on master's answer, which is why this was filed as residue rather
// than as a defect (bd gqlc-f2h7). Narrowing writtenBindings to skip the empty
// string would make the first row ACCEPT, and that is a deliberate change to
// Phase B's answer — this test is here so it cannot be made silently.
func (s *ResolverSuite) TestAnAnonymousWrittenBindingSilencesAnAnonymousMatchedEdge() {
	sch := s.loadSchema("invalid", "satisfy_plural_edges_reversed.gql")
	resolve := func(src string) ([]Column, error) {
		q, err := cypher.New(cypher.WithRegistry(regR7)).Parse(bytes.NewReader([]byte(src)))
		s.Require().NoError(err)
		vq, err := New(sch, WithRegistry(regR7)).Resolve(q)
		return vq.Columns, err
	}
	employeePerson := []Column{{Name: "p", Type: ResolvedNode{Labels: "Employee&Person"}}}

	const matched = "MATCH (p:Person)-[:WORKS_AT]->(c:Company) "

	// The baseline, and the row that makes the other two mean anything: the
	// MATCHed edge is anonymous and it narrows on its own. Without it, the
	// refusal below could be the anonymous MATCH failing to narrow for a reason
	// of its own rather than the CREATE silencing it.
	got, err := resolve(matched + "RETURN p")
	s.Require().NoError(err, "an anonymous single-hop mandatory edge is a witness when nothing is written")
	s.Require().Equal(employeePerson, got)

	// The over-approximation, isolated. Both CREATEd NODES are named, so the
	// only anonymous position the CREATE contributes is its EDGE. The CREATE's
	// pattern shares no binding with the MATCH — it writes a different edge
	// between two freshly created nodes — so the only thing it has in common
	// with the MATCHed edge is that anonymity.
	_, err = resolve(matched + "CREATE (a:Person:Employee)-[:WORKS_AT]->(b:Company) RETURN p")
	s.Require().ErrorIs(err, ErrAmbiguousLabel,
		"the CREATE writes an anonymous edge, which enters writtenBindings as the empty string; "+
			"the MATCHed edge is anonymous too, so it tests written and stops being a witness, and p stays plural")
	s.Require().ErrorContains(err, "p is satisfied by more than one declared node type: Employee&Person, Person",
		"the refusal must be p staying plural — refused for any other reason, the row passes its sentinel and pins nothing")

	// One edit from the row above, and it is one edit: the edge is NAMED and
	// nothing else moves. That is what makes the pair evidence about the edge
	// rather than about anonymity in general.
	got, err = resolve(matched + "CREATE (a:Person:Employee)-[w:WORKS_AT]->(b:Company) RETURN p")
	s.Require().NoError(err,
		"naming the written EDGE takes the empty string out of the written set, so the same "+
			"anonymous MATCHed edge becomes a witness again — the answer turns on the WRITTEN side alone")
	s.Require().Equal(employeePerson, got)

	// The control for the other half of writtenBindings' doc comment, and the
	// row that says WHICH half does the work. The empty string has exactly one
	// source: an anonymous node is not a binding at all (C3), so it never
	// reaches CreateEffect.Variables, and only an anonymous EDGE puts the
	// empty string in the written set. So a CREATE carrying no edge cannot
	// silence a matched one however it spells its nodes — which is what
	// separates this row from the refusal above, where the sole edit was the
	// edge. Without this row the distinction is an argument; with it, a
	// measurement.
	got, err = resolve(matched + "CREATE (:Company) RETURN p")
	s.Require().NoError(err,
		"an anonymous node is not a binding, so it contributes nothing to the written set — "+
			"only an anonymous EDGE enters it as the empty string")
	s.Require().Equal(employeePerson, got)
}

// TestNarrowingLearnsOnlyFromASingleHopEdge is the accepted half of the four
// invalid/plural_endpoint_*hop*_stays_plural fixtures, and the reason the
// witness predicate asks for a single hop rather than for a non-zero lower
// bound.
//
// The narrowing reads a committed candidate set as a statement about the
// PATTERN's two ends. That reading is only sound when the pattern IS one
// declared edge. Under `*2` the pattern is two of them chained, and the ends
// the closure names are the ends of the last hop, not of the pattern — so the
// binding at the far end commits a type that is one hop away from the truth.
// It is not a coarser answer that a later stage could refine; it is the wrong
// type, and since these fixtures project a property declared on exactly one
// node type, it reaches emitted code as a NOT NULL column that is null.
//
// No schema already in the corpus can show it, and the reason is NOT how often
// each declares its label — every one of them declares some label more than
// once: FOUNDED twice in valid/satisfy_plural_edges.gql, KNOWS twice in
// _symmetric, MENTORS twice in _mixed_symmetry, WORKS_AT twice in _three_types,
// REVIEWED twice in _overlapping and _reversed_subtype, three times in
// _reversed and four times in _two_swapped_pairs. Multiplicity cannot be what
// matters, because edgeCandidates never reads the hop count at all: the closure
// of a `*2` pattern is the same set as the closure of the one-hop pattern with
// the same two endpoints, however many declarations carry the label.
//
// What matters is whether that set can hold a declaration which is not the
// pattern's FIRST hop. satisfy_plural_edges_chain.gql is the only schema where
// it can: X runs A -> B and B -> C, a chain through three DISTINCT types that
// `(:Node)` satisfies alike. So `(p:Node)-[:X*2]->(c:Node:C)` has
// srcs = {A&Node, B&Node, C&Node}, tgts = {C&Node}, and B -> C sits inside that
// box while the real first hop A -> B does not. The closure names B&Node; the
// only two-hop path into C&Node starts at A&Node.
//
// Every other plural-endpoint schema is missing one of those two ingredients.
// satisfy_plural_edges, _mixed_symmetry and _three_types declare their label
// only INTO a type no declaration leaves, so there is no chain to walk.
// _symmetric, _overlapping and _two_swapped_pairs do chain, but every A -> B
// -> C they admit has two of A, B, C equal, so there is no third type for the
// closure to name. _reversed and _reversed_subtype do chain through a third
// distinct type — Person&Employee -> Company -> Person and Author -> Book ->
// Author&Editor — but that middle type is a Company and a Book, and neither
// satisfies the plural endpoint's `(:Person)` or `(:Author)`, so it is not in
// srcs and the closure can never name it.
//
// The twins are the same queries with the quantifier deleted. They must stay
// accepted with the SAME property type the multi-hop form wrongly emitted:
// that is what says the fixture above is refused because of the hop count and
// not because the chain schema is unresolvable, and what stops the fix from
// being "switch the narrowing off for anything var-length-shaped".
func (s *ResolverSuite) TestNarrowingLearnsOnlyFromASingleHopEdge() {
	sch := s.loadSchema("invalid", "satisfy_plural_edges_chain.gql")
	resolve := func(src string) ([]Column, error) {
		q, err := cypher.New(cypher.WithRegistry(regR7)).Parse(bytes.NewReader([]byte(src)))
		s.Require().NoError(err)
		vq, err := New(sch, WithRegistry(regR7)).Resolve(q)
		return vq.Columns, err
	}

	tests := []struct {
		name    string
		fixture string
		twin    string
		want    []Column
	}{
		{
			// Exactly two hops. p is A&Node on every row; the closure names
			// B&Node, the source of the LAST hop.
			name:    "an exact multi-hop count is not a witness, an unquantified edge is",
			fixture: "plural_endpoint_multi_hop_stays_plural.cypher",
			twin:    "MATCH (p:Node)-[w:X]->(c:Node:C) RETURN p.bOnly",
			want:    []Column{{Name: "p.bOnly", Type: ResolvedProperty{Type: graph.PropertyType("STRING")}}},
		},
		{
			// One-or-two hops. The lower bound is one, which is all
			// qualifiedDemoter ever asked for — and the two-hop member of the
			// range is enough to put p on A&Node.
			name:    "a range that admits two hops is not a witness even with a one-hop lower bound",
			fixture: "plural_endpoint_multi_hop_range_stays_plural.cypher",
			twin:    "MATCH (p:Node)-[w:X]->(c:Node:C) RETURN p.bOnly",
			want:    []Column{{Name: "p.bOnly", Type: ResolvedProperty{Type: graph.PropertyType("STRING")}}},
		},
		{
			// `*` is Min() == nil AND Max() == nil. openCypher reads the nil
			// lower bound as one, so every lower-bound test passes it; the
			// unbounded UPPER bound is what disqualifies it, and a predicate
			// that only consulted Min() would admit the widest shape there is.
			name:    "an unbounded quantifier is not a witness",
			fixture: "plural_endpoint_unbounded_hops_stays_plural.cypher",
			twin:    "MATCH (p:Node)-[w:X]->(c:Node:C) RETURN p.bOnly",
			want:    []Column{{Name: "p.bOnly", Type: ResolvedProperty{Type: graph.PropertyType("STRING")}}},
		},
		{
			// The far end is symmetric and is asserted separately: the two ends
			// are narrowed by two different calls to endpointContribution, so a
			// fix applied to one side only is green on the three rows above.
			name:    "the far end of a multi-hop pattern is no more pinned than the near end",
			fixture: "plural_endpoint_multi_hop_far_end_stays_plural.cypher",
			twin:    "MATCH (p:Node:A)-[w:X]->(c:Node) RETURN c.bOnly",
			want:    []Column{{Name: "c.bOnly", Type: ResolvedProperty{Type: graph.PropertyType("STRING")}}},
		},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			// Read from disk so the pair cannot drift: what is refused in
			// invalid/ and what is accepted here are one quantifier apart.
			src, err := os.ReadFile(filepath.Join(fixtureDir, "invalid", tt.fixture))
			s.Require().NoError(err)
			_, err = resolve(string(src))
			s.Require().ErrorIs(err, ErrUnknownProperty,
				"%s names a property of the type the closure would wrongly commit, so it must stay refused", tt.fixture)

			got, err := resolve(tt.twin)
			s.Require().NoError(err, "the twin removes only the quantifier, so the closure really is the pattern and must still commit")
			s.Require().Equal(tt.want, got)
		})
	}
}

// TestNarrowedEndsThatDisagreeKeepTheWideAnswer pins candidateTypes' fallback
// from the empty narrowed accumulator to the unnarrowed one.
//
// The sentinel on unlabelled_narrowed_ends_disagree.cypher already flips
// without the fallback (ErrAmbiguousBinding becomes ErrUnknownLabel), but the
// sentinel alone does not say the narrowing RAN — a lane that never fired would
// give the same ErrAmbiguousBinding for the ordinary reason. The two halves are
// what say it fired: each is a proper prefix of the fixture's own text, each
// narrows `p` on its own, and they narrow it to DIFFERENT types. That is the
// disagreement, and it is why the intersection over both is empty.
//
// The halves are sliced out of the fixture rather than spelled here so the
// three queries cannot drift apart; the line count is asserted so a fixture
// edit fails loudly instead of slicing the wrong clauses.
func (s *ResolverSuite) TestNarrowedEndsThatDisagreeKeepTheWideAnswer() {
	sch := s.loadSchema("invalid", "narrowed_ends_disagree.gql")
	resolve := func(src string) ([]Column, error) {
		q, err := cypher.New(cypher.WithRegistry(regR7)).Parse(bytes.NewReader([]byte(src)))
		s.Require().NoError(err)
		vq, err := New(sch, WithRegistry(regR7)).Resolve(q)
		return vq.Columns, err
	}

	src, err := os.ReadFile(filepath.Join(fixtureDir, "invalid", "unlabelled_narrowed_ends_disagree.cypher"))
	s.Require().NoError(err)
	lines := strings.Split(strings.TrimRight(string(src), "\n"), "\n")
	s.Require().Len(lines, 5, "the halves are sliced by line: two MATCH clauses each, then RETURN p")

	_, err = resolve(string(src))
	s.Require().ErrorIs(err, ErrAmbiguousBinding)
	s.Require().Contains(err.Error(), "candidate types: Author, Author&Editor")

	// authorOnly is declared on Author and not on Author&Editor; editorId the
	// other way round. Each half therefore accepts only if `p` narrowed, and
	// only if it narrowed to that half's type.
	wrote := strings.Join(lines[0:2], "\n") + "\nRETURN p.authorOnly"
	got, err := resolve(wrote)
	s.Require().NoError(err, "the WROTE half alone pins b to Book and p to Author")
	s.Require().Equal([]Column{{Name: "p.authorOnly", Type: ResolvedProperty{Type: graph.PropertyType("STRING")}}}, got)

	spoke := strings.Join(lines[2:4], "\n") + "\nRETURN p.editorId"
	got, err = resolve(spoke)
	s.Require().NoError(err, "the SPOKE_AT half alone pins v to Venue and p to Author&Editor")
	s.Require().Equal([]Column{{Name: "p.editorId", Type: ResolvedProperty{Type: graph.PropertyType("INT")}}}, got)

	// The two halves' projections are each refused on the OTHER half, which is
	// what makes "different types" a fact about the schema and not about which
	// property name was chosen.
	_, err = resolve(strings.Join(lines[0:2], "\n") + "\nRETURN p.editorId")
	s.Require().ErrorIs(err, ErrUnknownProperty)
	_, err = resolve(strings.Join(lines[2:4], "\n") + "\nRETURN p.authorOnly")
	s.Require().ErrorIs(err, ErrUnknownProperty)
}

// TestANativelyPluralBindingDefersEvenWhenEveryCandidateDeclaresTheProperty
// pins the asymmetry between the two ways an unlabelled binding's candidate set
// reaches two or more types. Both are recorded here because nothing else in
// this package separates them, and PR #1032's body named only two moved classes
// while this is a third (bead gqlc-icfq).
//
// The WIDENED case — commit() substituting `attainable` for a one-element
// `inferred` — commits the plural set, and the projection path then resolves a
// property every candidate declares. The NATIVE case — `inferred` itself
// holding two or more — never reaches the plural lane at all, because
// commitUnlabelledRound's default arm enters it only on commit()'s `widened`
// flag, so the round defers and Phase B eventually refuses.
//
// The refusal is fail-closed, not unsound: `id` really is INT NOT NULL on both
// candidates, so accepting it would be a widening in the same direction as the
// widened case. It is left in place rather than narrowed because the deferral
// is ErrAmbiguousBinding's only reachable site. Measured while preparing this
// pin: with the plural commit extended to the native case at Phase B's terminal
// refusal, the corpus sweep moves ZERO verdicts and fourteen sentinels, and
// TestSweepReachesEverySentinel then fails because no cell of the cross product
// refuses with ErrAmbiguousBinding at all. Retiring a sentinel that
// docs/specs/resolver-stage-r1.md declares, and defends by name against
// ErrAmbiguousLabel, is a posture decision this pin deliberately does not take;
// it records the current answer so the decision cannot be taken by accident.
// Guarding the extension on `covered` does not separate the two cases — all
// fourteen moved cells are covered.
//
// MEMBER_OF is the native shape: it runs from Author and from Author&Editor to
// the one Guild type, so `(p)` folds to both and no widening was involved.
// `id` is declared INT NOT NULL on each of them.
func (s *ResolverSuite) TestANativelyPluralBindingDefersEvenWhenEveryCandidateDeclaresTheProperty() {
	sch := s.loadSchema("invalid", "narrowed_ends_disagree.gql")
	resolve := func(sc schema.Schema, src string) ([]Column, error) {
		q, err := cypher.New(cypher.WithRegistry(regR7)).Parse(bytes.NewReader([]byte(src)))
		s.Require().NoError(err)
		vq, err := New(sc, WithRegistry(regR7)).Resolve(q)
		return vq.Columns, err
	}

	// Both candidates declare `id`, so the column is answerable without knowing
	// which type the row carries. It is refused anyway.
	_, err := resolve(sch, "MATCH (p)-[m:MEMBER_OF]->(g:Guild)\nRETURN p.id")
	s.Require().ErrorIs(err, ErrAmbiguousBinding,
		"the native plural set never enters the plural lane, so the shared property is not consulted")
	s.Require().Contains(err.Error(), "candidate types: Author, Author&Editor")

	// The unshared property refuses with the SAME sentinel, which is what says
	// the refusal is the deferral and not the property check: were the plural
	// lane entered, this one would be ErrUnknownProperty and the one above
	// would be accepted.
	_, err = resolve(sch, "MATCH (p)-[m:MEMBER_OF]->(g:Guild)\nRETURN p.authorOnly")
	s.Require().ErrorIs(err, ErrAmbiguousBinding,
		"authorOnly is declared on Author alone; reaching ErrUnknownProperty here would mean the plural lane ran")

	// The widened direction, read from disk so the contrast cannot drift: the
	// same "every candidate declares it" reasoning, applied where commit()
	// substituted attainable for a singleton inferred, ACCEPTS.
	src, err := os.ReadFile(filepath.Join(fixtureDir, "valid", "unlabelled_optional_hop_shared_property.cypher"))
	s.Require().NoError(err)
	cols, err := resolve(s.loadSchema("valid", "satisfy_plural_edges_inline_subtype.gql"), string(src))
	s.Require().NoError(err,
		"the widened plural binding resolves a property every candidate declares; the native one above does not")
	s.Require().NotEmpty(cols)
}

// TestPhaseBsPluralCommitLeavesNoResolvedCoversMark pins the lane write the
// plural arm of commitUnlabelledRound is the one writer not to make.
//
// resolvedCovers qualifies `resolved` alone (nodeTable says so), so a mark must
// not outlive the `resolved` entry it qualifies. Four siblings state that rule
// in their own words and act on it: BindNodeCands, whose delete "keeps the two
// lanes from ever both claiming v"; BindEdge and BindCall on their shadow
// cascades; and the SINGULAR arm of this same round, whose `else` deletes so
// "the absence is asserted rather than assumed". The plural arm moves a name
// into `cands` — BindNodeCands' own transition — and left the lane alone.
//
// The mark is seeded here because no input produces it: every writer of the
// lane writes `resolved` in the same breath, and Phase B filters a name already
// in `resolved` out of `pending` before the first round. So this test does not
// claim the state arises today. It claims the arm clears it when it is there,
// which is exactly what the four siblings were written for, and what seeding
// the lane from the carry — the alternative newScope names and declines — would
// make live.
func (s *ResolverSuite) TestPhaseBsPluralCommitLeavesNoResolvedCoversMark() {
	sch := s.loadSchema("valid", "satisfy_plural_edges_inline_subtype.gql")

	// unlabelled_optional_hop_shared_property.cypher's shape, built directly so
	// the mark can be seeded: a MANDATORY WORKS_AT off a `(:Person)` inline end
	// makes both company types attainable, and an OPTIONAL HAS_DESK — which
	// witnesses no endpoint, so it folds into `inferred` alone — narrows the
	// inference to the bare Company. That is commit()'s widened case, which is
	// the only way into the plural arm.
	c, err := query.NewNodeBinding("c", graph.LabelSet{})
	s.Require().NoError(err)
	cEnd, err := query.NewVarEndpoint("c")
	s.Require().NoError(err)
	worksAt, err := query.NewEdgeBinding("q", graph.LabelSet{"WORKS_AT"},
		query.NewInlineEndpoint(graph.LabelSet{"Person"}), cEnd, true)
	s.Require().NoError(err)
	hasDesk, err := query.NewNullableEdgeBinding("h", graph.LabelSet{"HAS_DESK"},
		cEnd, query.NewInlineEndpoint(graph.LabelSet{"Desk"}), true)
	s.Require().NoError(err)

	table := nodeTable{
		resolved:          map[string]schema.NodeType{},
		cands:             map[string][]schema.NodeType{},
		pluralByInference: map[string]bool{},
		resolvedCovers:    map[string]struct{}{"c": {}},
	}
	s.Require().NoError(inferUnlabelled(
		[]query.NodeBinding{c}, []query.EdgeBinding{worksAt, hasDesk},
		sch, table, map[string]callBindingSlot{}, map[string]struct{}{}))

	// The tripwire. Without these the assertion below passes for a `c` that
	// never reached the plural arm at all.
	s.Require().Len(table.cands["c"], 2,
		"c must commit to the plural lane, or the arm under test did not run")
	s.Require().NotContains(table.resolved, "c",
		"the plural arm writes no singular entry, so no mark of its making qualifies one")

	s.Require().NotContains(table.resolvedCovers, "c",
		"a mark qualifying a `resolved` entry the plural arm did not write must not survive it")
}

// TestPhaseBsUncoveredSingularCommitClearsAResolvedCoversMark pins the `else`
// of the singular arm's covered/uncovered branch — the fourth sibling of the
// three scope-level deletes and of the plural arm above.
//
// The arm writes `resolved` and must NOT mark it: an uncovered commitment is
// exactly the one the narrowing may not learn from, and that is expressed as the
// ABSENCE of a mark. Its comment says the absence is asserted rather than
// assumed, which is only true if something asserts it.
//
// Seeded for the same reason as the plural test: a name leaves `pending` on the
// iteration it commits, so there is no second commit to overwrite, and the
// lane's other writers never see an unlabelled binding. The state is
// constructible only from a hand-seeded table.
func (s *ResolverSuite) TestPhaseBsUncoveredSingularCommitClearsAResolvedCoversMark() {
	sch := s.loadSchema("valid", "satisfy_plural_edges_inline_subtype.gql")

	// The transitivity the arm's own comment describes, one hop wide. `c` is
	// already committed and UNMARKED, so endpointLabels hands `d`'s only edge a
	// far end that reports covers=false and candidateTypes' conjunction fails on
	// its first conjunct — the edge folds into `inferred` but not `attainable`,
	// and commit() returns the unattested reading with covered=false. The hop is
	// MANDATORY, so the binding is constrained rather than reaching case 0's
	// OPTIONAL-only refusal; and HAS_DESK is declared once, so `inferred` is the
	// singleton that takes the singular arm rather than widening to the plural
	// one.
	d, err := query.NewNodeBinding("d", graph.LabelSet{})
	s.Require().NoError(err)
	cEnd, err := query.NewVarEndpoint("c")
	s.Require().NoError(err)
	dEnd, err := query.NewVarEndpoint("d")
	s.Require().NoError(err)
	hasDesk, err := query.NewEdgeBinding("h", graph.LabelSet{"HAS_DESK"}, cEnd, dEnd, true)
	s.Require().NoError(err)

	table := nodeTable{
		resolved:          map[string]schema.NodeType{"c": sch.Nodes[graph.LabelSet{"Company"}.Key()]},
		cands:             map[string][]schema.NodeType{},
		pluralByInference: map[string]bool{},
		resolvedCovers:    map[string]struct{}{"d": {}},
	}
	s.Require().NoError(inferUnlabelled(
		[]query.NodeBinding{d}, []query.EdgeBinding{hasDesk},
		sch, table, map[string]callBindingSlot{}, map[string]struct{}{}))

	// Tripwires. Without them the assertion below passes for a `d` that took the
	// plural arm — already pinned above — or that never committed at all, and it
	// passes for a `d` that committed COVERED, which is the other branch.
	s.Require().Contains(table.resolved, "d",
		"d must commit singular, or the arm under test did not run")
	s.Require().NotContains(table.cands, "d",
		"d must not have taken the plural arm, which is a different sibling's delete")
	s.Require().NotContains(table.resolvedCovers, "c",
		"the seeded far end must stay unmarked, or d's edge covered and the covered branch ran instead")

	s.Require().NotContains(table.resolvedCovers, "d",
		"an uncovered commitment must not inherit a mark: the absence IS how the resolver says the narrowing may not learn from it")
}

// TestAWholeEntityProjectionNamesTheFaultItActuallyHas pins which of the two
// ambiguity sentinels the plural whole-entity refusal carries, on the axis that
// selects it: whether the binding carried labels.
//
// errors.go draws that line and the code did not. ErrAmbiguousLabel is
// documented as "a query's LABEL SET has no exact match on a declared node type
// key and its proper-superset satisfying set has more than one element", and
// disclaims the other case in the same comment — "Not overloaded onto
// ErrAmbiguousBinding either, whose documented meaning is Phase B inference
// failure at an unlabelled binding". ErrAmbiguousBinding is documented as "an
// unlabelled node binding cannot be uniquely typed from the edges that touch
// it: either its candidate set (intersected across touching edges) has more
// than one node type, or ...". A whole-entity projection of an unlabelled
// binding whose committed candidate set is plural is the first arm of that
// sentence, verbatim, and refProjectionType returned the other sentinel for it.
//
// The falsifier is the first two rows: ONE binding, ONE candidate set, ONE
// projection, two sentinels selected by an OPTIONAL MATCH that touches neither.
// Measured at 412b9612, on valid/schemas/satisfy_plural_edges_inline_subtype.gql:
//
//	MATCH (p:Person)-[q:WORKS_AT]->(c)
//	OPTIONAL MATCH (c)-[h:HAS_DESK]->(d:Desk)
//	RETURN c
//	  -> ambiguous label: c is satisfied by more than one declared node
//	     type: Company, Company&Large
//
//	MATCH (p:Person)-[q:WORKS_AT]->(c)
//	RETURN c
//	  -> ambiguous binding: cannot uniquely infer type of unlabelled
//	     binding "c" — candidate types: Company, Company&Large
//
// The second is Phase B's terminal deferral; the first is the widened lane, in
// which commit() substituted `attainable` for a singleton `inferred` and bound
// the plural set, so the refusal moved to projection time. Same fault, same
// candidates, different name.
//
// The labelled row is the guard, not decoration: it is what a fix collapsing
// both arms onto ErrAmbiguousBinding dies against. That collapse is not
// available — the nodeCands whole-entity arm is ErrAmbiguousLabel's only
// fail-site in the package, so taking it wholesale retires a sentinel
// docs/specs/resolver-stage-r1.md declares and defends by name, which is the
// posture gqlc-yxqq refused for the mirror-image case.
//
// What this does NOT claim: that the deferral is the only route to
// ErrAmbiguousBinding. It is not, after this change — that is the point. Bead
// gqlc-yxqq asked whether ErrAmbiguousBinding survives a future widening of the
// natively-plural deferral (bead gqlc-icfq, pinned by the test above), given
// that the deferral was its ONLY reachable site and the widening retires it.
// This gives it a second site that the widening does not touch, so the answer
// is that it survives and the two beads are independent.
func (s *ResolverSuite) TestAWholeEntityProjectionNamesTheFaultItActuallyHas() {
	resolve := func(dir, schemaFile, src string) error {
		q, err := cypher.New(cypher.WithRegistry(regR7)).Parse(bytes.NewReader([]byte(src)))
		s.Require().NoError(err)
		_, err = New(s.loadSchema(dir, schemaFile), WithRegistry(regR7)).Resolve(q)
		return err
	}

	const widened = "MATCH (p:Person)-[q:WORKS_AT]->(c)\n" +
		"OPTIONAL MATCH (c)-[h:HAS_DESK]->(d:Desk)\n"
	const inlineSubtype = "satisfy_plural_edges_inline_subtype.gql"

	err := resolve("valid", inlineSubtype, widened+"RETURN c")
	s.Require().ErrorIs(err, ErrAmbiguousBinding,
		"`c` carries no labels, so its plural candidate set is a Phase B inference failure and not an ambiguous label set")
	s.Require().NotErrorIs(err, ErrAmbiguousLabel,
		"asserting the absence too: a widening that folds in the old sentinel keeps every errors.Is(ErrAmbiguousBinding) row green")
	s.Require().ErrorContains(err, "Company, Company&Large",
		"the refusal names the committed set, so a narrowing that dropped one and still refused fails here")

	// Same binding, same candidates, same projection, without the widening —
	// the deferral. Both routes must now answer with one name.
	err = resolve("valid", inlineSubtype, "MATCH (p:Person)-[q:WORKS_AT]->(c)\nRETURN c")
	s.Require().ErrorIs(err, ErrAmbiguousBinding)

	// A property projection of the widened binding is untouched: it still goes
	// through unionNodeProperty, which is what says the change is confined to
	// the whole-entity arm and did not swallow the property lane.
	s.Require().ErrorIs(resolve("valid", inlineSubtype, widened+"RETURN c.largeOnly"), ErrUnknownProperty)
	s.Require().NoError(resolve("valid", inlineSubtype, widened+"RETURN c.name"))

	// THE GUARD. `p` carries a label, its satisfying set has two elements, and
	// that is ErrAmbiguousLabel's documented fault. A fix that reads the arm
	// rather than the binding takes this row with it.
	err = resolve("invalid", "satisfy_singular.gql", "MATCH (p:Person) RETURN p")
	s.Require().ErrorIs(err, ErrAmbiguousLabel)
	s.Require().NotErrorIs(err, ErrAmbiguousBinding)
}

// TestAnUnnarrowedFarEndKeepsItsKeys pins narrowedEndpointKeys' two
// leave-it-alone arms: a far end that is not a variable, and a variable
// endpointNarrowing gave no entry.
//
// Both are the same claim — an end nothing narrowed contributes its whole
// satisfying set, not the empty set — and getting either wrong empties the
// narrowed intersection. That mistake is SILENT, because candidateTypes then
// falls back to the wide answer and the query refuses for ambiguity exactly as
// it did before the lane existed: no sentinel moves, no message changes, and
// the whole suite stays green. Both arms SURVIVED mutation until this test.
//
// The shape needed is a bare binding touching a narrowed plural far end AND an
// unnarrowed end at once; nothing else in the corpus has it. MEMBER_OF supplies
// the unnarrowed end — declared from both author types to the one Guild type,
// so it narrows nothing itself, and Guild is singular so `g` never enters the
// plural-candidate table.
func (s *ResolverSuite) TestAnUnnarrowedFarEndKeepsItsKeys() {
	sch := s.loadSchema("invalid", "narrowed_ends_disagree.gql")
	resolve := func(src string) ([]Column, error) {
		q, err := cypher.New(cypher.WithRegistry(regR7)).Parse(bytes.NewReader([]byte(src)))
		s.Require().NoError(err)
		vq, err := New(sch, WithRegistry(regR7)).Resolve(q)
		return vq.Columns, err
	}
	const wroteHalf = "MATCH (p)-[w:WROTE]->(b:Book)\nMATCH (b)-[s:SHELVED_IN]->(sh:Shelf)\n"

	tests := []struct {
		name string
		// alone must refuse: the MEMBER_OF hop narrows nothing by itself, which
		// is what makes each pair a test of the intersection rather than the
		// WROTE half over again.
		alone string
		// pair is alone prefixed by the WROTE half, whose narrowed reading is
		// {Author}. Intersected with MEMBER_OF's unchanged {Author,
		// Author&Editor} that is {Author}; read as empty it becomes {}, and the
		// fallback turns it back into `alone`'s refusal.
		pair string
	}{
		{
			name:  "a variable far end with no narrowing entry",
			alone: "MATCH (p)-[m:MEMBER_OF]->(g:Guild)\nRETURN p.authorOnly",
			pair:  wroteHalf + "MATCH (p)-[m:MEMBER_OF]->(g:Guild)\nRETURN p.authorOnly",
		},
		{
			// Not a VarEndpoint at all, so it never reaches the narrowing map.
			// The two arms are separate returns and a fix applied to one leaves
			// the other; a fully anonymous `()` cannot stand in, because
			// endpointLabels cannot read it either — `alone` is then refused by
			// Phase B's OWN case 0 ("no edge in the pattern reaches a compatible
			// schema node type"), and `pair` by CloseEdges' deferred close AFTER
			// Phase B ("cannot infer type of target endpoint"). Neither shape
			// reaches the arm under test.
			name:  "an inline far end that is not a variable",
			alone: "MATCH (p)-[m:MEMBER_OF]->(:Guild)\nRETURN p.authorOnly",
			pair:  wroteHalf + "MATCH (p)-[m:MEMBER_OF]->(:Guild)\nRETURN p.authorOnly",
		},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			_, err := resolve(tt.alone)
			s.Require().ErrorIs(err, ErrAmbiguousBinding, "the MEMBER_OF hop must narrow nothing on its own")

			got, err := resolve(tt.pair)
			s.Require().NoError(err, "an unnarrowed far end must not empty the narrowed intersection")
			s.Require().Equal([]Column{{Name: "p.authorOnly", Type: ResolvedProperty{Type: graph.PropertyType("STRING")}}}, got)
		})
	}
}

// TestAnEmptyHopRangeIsNotAWitness pins the EQUALITY in singleHopPattern's
// lower-bound test. It is the one arm of that predicate no fixture reaches:
// relax `*lower == 1` to `*lower >= 1` and the whole suite stays green.
//
// `*2..1` parses — the grammar does not require min <= max, and Hops() reports
// Min()==2, Max()==1 — and it matches nothing, since no path length is at once
// at least two and at most one. Nothing else in the corpus separates the two
// spellings: the four multi-hop fixtures are refused by the upper bound and the
// three zero-lower-bound ones by a lower bound of zero, so `*2..1` is the only
// quantifier whose verdict turns on == versus >=.
//
// Admitting it would not be UNSOUND — an empty range returns no rows, so there
// is no row whose p could be the wrong type. It is pinned anyway because the
// difference is visible in emitted code rather than in a row: under `>= 1` the
// query below is ACCEPTED and grows a NOT NULL STRING column, so the signature
// of a generated method would turn on a predicate no test reads. It also keeps
// singleHopPattern honest about the contract witnessesItsEndpoints quotes —
// "a range that admits exactly one hop and no other count". `*2..1` admits no
// count at all, so only the equality states that contract.
//
// The `*1..1` twin says the refusal is about the range being EMPTY and not
// about explicit ranges being refused wholesale.
func (s *ResolverSuite) TestAnEmptyHopRangeIsNotAWitness() {
	sch := s.loadSchema("invalid", "satisfy_plural_edges_chain.gql")
	resolve := func(src string) ([]Column, error) {
		q, err := cypher.New(cypher.WithRegistry(regR7)).Parse(bytes.NewReader([]byte(src)))
		s.Require().NoError(err)
		vq, err := New(sch, WithRegistry(regR7)).Resolve(q)
		return vq.Columns, err
	}

	_, err := resolve("MATCH (p:Node)-[w:X*2..1]->(c:Node:C) RETURN p.bOnly")
	s.Require().ErrorIs(err, ErrUnknownProperty,
		"`*2..1` is not one declared edge, so it witnesses nothing and p stays plural over all three types")

	got, err := resolve("MATCH (p:Node)-[w:X*1..1]->(c:Node:C) RETURN p.bOnly")
	s.Require().NoError(err, "`*1..1` is the non-empty range of exactly one hop, so the closure really is the pattern")
	s.Require().Equal([]Column{{Name: "p.bOnly", Type: ResolvedProperty{Type: graph.PropertyType("STRING")}}}, got)
}

// TestANonWitnessEdgeSilencesItselfNotTheBinding pins that
// witnessesItsEndpoints is asked once PER EDGE. A binding touched by a
// witnessing edge AND a non-witnessing one is narrowed by the first; the second
// contributes nothing and takes nothing away.
//
// Nothing else separates that from the coarser rule "a binding with any
// non-witnessing touching edge is not narrowed at all", which is a plausible
// reading of the same paragraph and a materially different resolver. Every
// invalid/plural_endpoint_*_stays_plural fixture puts its plural binding behind
// exactly ONE edge, and every accepted twin removes that edge's disqualifier,
// so on all eleven of them "this edge is silent" and "this binding is silent"
// are the same sentence. Implemented, the coarse rule leaves the suite green.
//
// The two rows are two different disqualifiers on purpose. They are separate
// arms of witnessesItsEndpoints, so a rule that poisoned the binding on only
// one of them still passes the other row, and only the pair says the question
// is per-edge for the predicate rather than for one of its clauses.
//
// The first row says more than that the multi-hop edge fails to poison: it says
// the edge contributes NOTHING. `X` runs A -> B and B -> C, so the `*2` close
// names B&Node while the single hop into `(:Node:B)` names A&Node. Were the
// quantified edge folded in at all, the intersection would be empty, the
// narrowing would fall back to the pre-closure set, and `p.aOnly` would be
// refused exactly as the coarse rule refuses it.
//
// Each row is paired with the same query minus the witnessing edge, which must
// be refused — otherwise the acceptance above could be the plural set answering
// on its own and the row would pin nothing.
func (s *ResolverSuite) TestANonWitnessEdgeSilencesItselfNotTheBinding() {
	tests := []struct {
		name        string
		schema      string
		query       string
		unwitnessed string
		want        []Column
	}{
		{
			name:        "a quantified edge alongside a single-hop one",
			schema:      "satisfy_plural_edges_chain.gql",
			query:       "MATCH (p:Node)-[w:X]->(b:Node:B), (p)-[v:X*2]->(z:Node:C) RETURN p.aOnly",
			unwitnessed: "MATCH (p:Node)-[v:X*2]->(z:Node:C) RETURN p.aOnly",
			want:        []Column{{Name: "p.aOnly", Type: ResolvedProperty{Type: graph.PropertyType("STRING")}}},
		},
		{
			name:   "an OPTIONAL edge alongside a mandatory one",
			schema: "satisfy_plural_edges_reversed.gql",
			query: "MATCH (p:Person)-[w:WORKS_AT]->(c:Company) " +
				"OPTIONAL MATCH (p)-[r:REVIEWED]->(c2:Company) RETURN p.employeeId",
			unwitnessed: "MATCH (p:Person) OPTIONAL MATCH (p)-[r:REVIEWED]->(c2:Company) RETURN p.employeeId",
			want:        []Column{{Name: "p.employeeId", Type: ResolvedProperty{Type: graph.PropertyType("INT")}}},
		},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			sch := s.loadSchema("invalid", tt.schema)
			resolve := func(src string) ([]Column, error) {
				q, err := cypher.New(cypher.WithRegistry(regR7)).Parse(bytes.NewReader([]byte(src)))
				s.Require().NoError(err)
				vq, err := New(sch, WithRegistry(regR7)).Resolve(q)
				return vq.Columns, err
			}

			got, err := resolve(tt.query)
			s.Require().NoError(err, "the witnessing edge pins p on its own; the other edge is not evidence, which is not the same as being an objection")
			s.Require().Equal(tt.want, got)

			_, err = resolve(tt.unwitnessed)
			s.Require().ErrorIs(err, ErrUnknownProperty,
				"without the witnessing edge nothing narrows p, so the property is the plural intersection's and the row above is not passing on its own")
		})
	}
}

// TestNarrowingSkipsAnEndpointItCannotEnumerate pins the precondition the whole
// narrowing rests on: srcs and tgts must be SUPERSETS of the keys a matching row
// can put at those ends. edgeProbes builds its box from those two slices, and
// the argument that a row's true edge key is in `cands` is "the row's key was
// probed" — which needs every attainable endpoint key to be in the box, not
// merely every boxed key to be attainable.
//
// Both arms of endpointLabels hold that up: a VarEndpoint returns its binding's
// whole satisfying set, and an InlineEndpoint returns the types satisfying the
// expression written there. The bit the narrowing consults is therefore about
// the BINDING TABLE, not about which arm answered — a VarEndpoint standing on a
// singular commitment the resolver inferred rather than derived is the endpoint
// it declines to read, and
// TestAnInferredEndpointIsTrustedOnlyWhenItsOwnEndsWereEnumerated pins that.
//
// The two rows here are the same query on two schemas that differ only in
// whether `(:Company)` is satisfied by one declared type or two. Between them
// they say the inline endpoint enumerates itself in both cases and neither
// answer is bought by refusing the other: report an inline endpoint uncovered
// and "the spelled labels are satisfied by exactly one type" goes red, because
// the narrowing that pins `p.employeeId` stops running.
//
// The var-spelling control matters beyond the usual "is the acceptance real"
// check: before the satisfying-set reading the inline and var spellings of one
// pattern gave different answers, and the inline one was the wrong one.
func (s *ResolverSuite) TestNarrowingSkipsAnEndpointItCannotEnumerate() {
	tests := []struct {
		name    string
		schema  string
		inline  string
		varSpel string
		want    []Column
		wantErr error
	}{
		{
			name:    "the spelled labels are satisfied by two types",
			schema:  "satisfy_plural_edges_inline_subtype.gql",
			inline:  "MATCH (p:Person)-[r:WORKS_AT]->(:Company) RETURN p.personOnly",
			varSpel: "MATCH (p:Person)-[r:WORKS_AT]->(c:Company) RETURN p.personOnly",
			wantErr: ErrUnknownProperty,
		},
		{
			name:    "the spelled labels are satisfied by exactly one type",
			schema:  "satisfy_plural_edges_reversed.gql",
			inline:  "MATCH (p:Person)-[r:WORKS_AT]->(:Company) RETURN p.employeeId",
			varSpel: "MATCH (p:Person)-[r:WORKS_AT]->(c:Company) RETURN p.employeeId",
			want:    []Column{{Name: "p.employeeId", Type: ResolvedProperty{Type: graph.PropertyType("INT")}}},
		},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			sch := s.loadSchema("invalid", tt.schema)
			resolve := func(src string) ([]Column, error) {
				q, err := cypher.New(cypher.WithRegistry(regR7)).Parse(bytes.NewReader([]byte(src)))
				s.Require().NoError(err)
				vq, err := New(sch, WithRegistry(regR7)).Resolve(q)
				return vq.Columns, err
			}

			for _, src := range []string{tt.inline, tt.varSpel} {
				got, err := resolve(src)
				if tt.wantErr != nil {
					s.Require().ErrorIsf(err, tt.wantErr,
						"%s: Employee&Person -[WORKS_AT]-> Company&Large matches this pattern and its p has no personOnly", src)
					continue
				}
				s.Require().NoErrorf(err, "%s: the inline endpoint enumerates its own satisfying set here, so the closure pins p", src)
				s.Require().Equal(tt.want, got, src)
			}
		})
	}
}

// TestInlineEndpointsAgreeWithTheirVarSpelling states the relation the two rows
// above only imply: whatever the answer is, writing an endpoint's labels inline
// and binding them to a variable are the same pattern and must resolve the same
// way. The rows assert an answer each; this asserts they cannot drift apart,
// which is the shape the defect actually took — the var spelling stayed correct
// throughout and only the inline one moved.
//
// The comparison is on the outcome, not on a named sentinel, because the pairs
// below have three different right answers between them and the relation is the
// claim. What stops "both sides refuse everything" from satisfying it is
// TestInlineEndpointCommitsOnTheTypesSatisfyingIt, which pins each answer.
func (s *ResolverSuite) TestInlineEndpointsAgreeWithTheirVarSpelling() {
	tests := []struct {
		name           string
		lane, schema   string
		inline, varSpe string
	}{
		{
			// Paired with the row below because an endpoint feeds the narrowing of
			// the end OPPOSITE it, so which side it is written on must not matter.
			name: "the far end of a narrowing", lane: "invalid", schema: "satisfy_plural_edges_inline_subtype.gql",
			inline: "MATCH (p:Person)-[r:WORKS_AT]->(:Company) RETURN p",
			varSpe: "MATCH (p:Person)-[r:WORKS_AT]->(c:Company) RETURN p",
		},
		{
			name: "the near end of a narrowing", lane: "invalid", schema: "satisfy_plural_edges_inline_subtype.gql",
			inline: "MATCH (:Person)-[r:WORKS_AT]->(c:Company) RETURN c",
			varSpe: "MATCH (p:Person)-[r:WORKS_AT]->(c:Company) RETURN c",
		},
		{
			name: "the far end of an unlabelled inference", lane: "invalid", schema: "satisfy_plural_edges_inline_subtype.gql",
			inline: "MATCH (:Person)-[r:WORKS_AT]->(c) RETURN c.smallOnly",
			varSpe: "MATCH (p:Person)-[r:WORKS_AT]->(c) RETURN c.smallOnly",
		},
		{
			name: "the endpoint an edge closes against", lane: "invalid", schema: "satisfy_plural_edges_inline_subtype.gql",
			inline: "MATCH (p:Person)-[r:WORKS_AT]->(:Company) RETURN r",
			varSpe: "MATCH (p:Person)-[r:WORKS_AT]->(c:Company) RETURN r",
		},
		{
			// The only pair whose two sides sat on opposite sides of the
			// accept/refuse line: nothing declared is named `Staff`, so the
			// inline spelling probed a key no EdgeKey carries.
			name: "an endpoint named by an implied label", lane: "valid", schema: "satisfy_implied_label_endpoint.gql",
			inline: "MATCH (:Staff)-[r:WORKS_AT]->(c:Company) RETURN c",
			varSpe: "MATCH (e:Staff)-[r:WORKS_AT]->(c:Company) RETURN c",
		},
		{
			// Both sides refuse, so the pair is not about the accept/refuse line
			// but about WHICH refusal. No declared type carries both Company and
			// Desk, so the label set is unsatisfiable and there was never an edge
			// to look for; the var spelling says so at Phase A1. Until the inline
			// spelling was given the same satisfiability check it reached edge
			// closure with an empty satisfying set, fell through to the spelled
			// key and reported a missing EDGE — telling the author to go declare
			// `Person-[WORKS_AT]->Company&Desk`, which no node could ever be an
			// endpoint of (bd gqlc-jqix).
			name: "an endpoint no declared type satisfies", lane: "invalid", schema: "satisfy_plural_edges_inline_subtype.gql",
			inline: "MATCH (p:Person)-[r:WORKS_AT]->(:Company:Desk) RETURN p",
			varSpe: "MATCH (p:Person)-[r:WORKS_AT]->(z:Company:Desk) RETURN p",
		},
		{
			// The row above on the other side of the arrow. An endpoint is asked
			// the question because it is an endpoint, not because of which end it
			// is, and a check that reads only the target passes every row above.
			name: "an unsatisfiable endpoint written on the source", lane: "invalid", schema: "satisfy_plural_edges_inline_subtype.gql",
			inline: "MATCH (:Company:Desk)-[r:HAS_DESK]->(d:Desk) RETURN d",
			varSpe: "MATCH (z:Company:Desk)-[r:HAS_DESK]->(d:Desk) RETURN d",
		},
		{
			// The other way a label set yields no type. Company&Desk is
			// unsatisfiable although both its labels are declared; Nope is not
			// declared at all. They are one question — no type can stand here —
			// and asking only the first leaves the inline spelling reporting a
			// missing edge to a type nothing declares, which is the whole defect
			// in its second shape.
			name: "an endpoint label nothing declares", lane: "invalid", schema: "satisfy_plural_edges_inline_subtype.gql",
			inline: "MATCH (p:Person)-[r:WORKS_AT]->(:Nope) RETURN p",
			varSpe: "MATCH (p:Person)-[r:WORKS_AT]->(z:Nope) RETURN p",
		},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			sch := s.loadSchema(tt.lane, tt.schema)
			resolve := func(src string) (string, []Column) {
				q, err := cypher.New(cypher.WithRegistry(regR7)).Parse(bytes.NewReader([]byte(src)))
				s.Require().NoError(err)
				vq, err := New(sch, WithRegistry(regR7)).Resolve(q)
				if err != nil {
					return err.Error(), nil
				}
				return "", vq.Columns
			}
			inlineErr, inlineCols := resolve(tt.inline)
			varErr, varCols := resolve(tt.varSpe)
			s.Require().Equal(varErr, inlineErr, "%s\nvs\n%s", tt.inline, tt.varSpe)
			s.Require().Equal(varCols, inlineCols, "%s\nvs\n%s", tt.inline, tt.varSpe)
		})
	}
}

// TestInlineEndpointCommitsOnTheTypesSatisfyingIt pins the answers the pairs
// above only require to match. An inline endpoint names the node types whose
// complete label set satisfies the expression written there (ADR 0022), and
// every one of those types is a key the closure has to probe.
//
// Each row is a shape where the spelled key and the satisfying set are
// different sets, and each is a case the resolver used to answer from the
// spelled key alone:
//
//   - `c.smallOnly` was a column, typed STRING NOT NULL, on a query whose
//     Employee&Person -[WORKS_AT]-> Company&Large rows have no such property
//     (gqlc-3uof);
//   - `r` was the single Person-[WORKS_AT]->Company when both declarations
//     match the pattern (gqlc-qlr2);
//   - `(:Staff)` reached no declared edge at all, because the identity that
//     carries the label is Engineer.
//
// The conjunction row is the negative: satisfaction is a superset test over
// each declared type's complete label set, not the whole schema, so spelling
// both labels leaves one satisfying type and the answer is singular again.
//
// The last row is the arm where there is no satisfying set at all, so the
// endpoint commits on nothing and the answer is a refusal. Which refusal is the
// whole of it: satisfiability is a property of the expression and the schema
// alone, so an unsatisfiable set means no row can stand there and there was
// never an edge to look for. It is asked at Phase A1, where the same question
// on a var spelling is asked, and the message is the same one (bd gqlc-jqix).
//
// This row used to assert `unknown edge: ...Company&Desk`, from an endpointLabels
// arm that yielded the SPELLED key once nothing satisfied the expression. That
// arm is now unreachable, and asserting the whole message here is what keeps the
// wording pinned: TestInlineEndpointsAgreeWithTheirVarSpelling compares the two
// spellings to each other and would pass on any message they regressed to
// together.
func (s *ResolverSuite) TestInlineEndpointCommitsOnTheTypesSatisfyingIt() {
	worksAtKeys := []schema.EdgeKey{
		{Source: "Employee&Person", KeyLabels: "WORKS_AT", Target: "Company&Large"},
		{Source: "Person", KeyLabels: "WORKS_AT", Target: "Company"},
	}
	tests := []struct {
		name         string
		lane, schema string
		query        string
		want         []Column
		wantErr      error
		wantMsg      string
	}{
		{
			name: "an unlabelled binding inferred through it", lane: "invalid", schema: "satisfy_plural_edges_inline_subtype.gql",
			query:   "MATCH (:Person)-[r:WORKS_AT]->(c) RETURN c.smallOnly",
			wantErr: ErrAmbiguousBinding,
			wantMsg: `ambiguous binding: cannot uniquely infer type of unlabelled binding "c" — candidate types: Company, Company&Large`,
		},
		{
			name: "the edge closed against it", lane: "invalid", schema: "satisfy_plural_edges_inline_subtype.gql",
			query: "MATCH (p:Person)-[r:WORKS_AT]->(:Company) RETURN r",
			want:  []Column{{Name: "r", Type: ResolvedEdgeUnion{EdgeKeys: worksAtKeys}}},
		},
		{
			name: "an implied label that names no declared identity", lane: "valid", schema: "satisfy_implied_label_endpoint.gql",
			query: "MATCH (:Staff)-[r:WORKS_AT]->(c:Company) RETURN c",
			want:  []Column{{Name: "c", Type: ResolvedNode{Labels: "Company"}}},
		},
		{
			name: "a conjunction only one type satisfies", lane: "invalid", schema: "satisfy_plural_edges_inline_subtype.gql",
			query: "MATCH (:Person:Employee)-[r:WORKS_AT]->(c) RETURN c.largeId",
			want:  []Column{{Name: "c.largeId", Type: ResolvedProperty{Type: graph.PropertyType("INT")}}},
		},
		{
			name: "a conjunction no type satisfies", lane: "invalid", schema: "satisfy_plural_edges_inline_subtype.gql",
			query:   "MATCH (p:Person)-[r:WORKS_AT]->(:Company:Desk) RETURN p",
			wantErr: ErrUnknownLabel,
			wantMsg: "unknown label: no node type satisfies Company&Desk; " +
				"declared types carrying these labels: Company, Company&Large, Desk",
		},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			sch := s.loadSchema(tt.lane, tt.schema)
			q, err := cypher.New(cypher.WithRegistry(regR7)).Parse(bytes.NewReader([]byte(tt.query)))
			s.Require().NoError(err)
			vq, err := New(sch, WithRegistry(regR7)).Resolve(q)
			if tt.wantErr != nil {
				s.Require().ErrorIs(err, tt.wantErr, tt.query)
				s.Require().EqualError(err, tt.wantMsg, tt.query)
				return
			}
			s.Require().NoError(err, tt.query)
			s.Require().Equal(tt.want, vq.Columns, tt.query)
		})
	}
}

// TestUnlabelledBindingRefusedWhenNoEdgeIsEvidence pins gqlc-6aed: a binding
// whose touching edges are ALL non-witnessing is unconstrained on the returned
// rows, so no type may be inferred for it.
//
// candidateTypes folds an edge into `inferred` whether or not the edge is
// evidence about every returned row — that set is left exactly as master
// computes it. `attested` records that some edge passed BOTH conjuncts, and
// commit() consulted it only to gate the WIDENING. Nothing consulted the
// witnessing conjunct on its own, so when it failed on EVERY touching edge the
// fallback still returned `inferred` and a singleton committed.
//
// The two rows that separate the defect from correct behaviour differ in one
// token, OPTIONAL, and before this test both returned `c.smallOnly STRING NOT
// NULL`:
//
//   - the mandatory hop is an inner join. It filters out every row whose `c`
//     is not a HAS_DESK source, so `c` really is the bare Company on all of
//     them and NOT NULL is sound.
//   - the OPTIONAL hop is an outer join and filters nothing. `MATCH (c)` binds
//     every node in the graph, so a returned row's `c` is a Person, a Desk or a
//     Company&Large just as readily, and none of those declares smallOnly.
//
// WHY THE VERDICT IS REFUSAL AND NOT A NULLABLE COLUMN. Nullability cannot
// express this defect. The `RETURN c` row is the witness: the resolver answers
// ResolvedNode{Labels: "Company"} for a binding that can hold a Person node, and
// a nullable Company is still not a Person. The unsoundness is in the TYPE, and
// only refusing to infer one repairs it. That also lands the shape where the
// resolver already was for a binding with no touching edge at all — the last row
// — which is the same evidentiary position reached by a different route.
//
// The message is asserted rather than the sentinel alone because two arms now
// return ErrUnknownLabel from the same `case 0`, and they are true of different
// queries: `no edge in the pattern reaches a compatible schema node type` is
// false of these queries, whose OPTIONAL hop reaches Company perfectly well.
func (s *ResolverSuite) TestUnlabelledBindingRefusedWhenNoEdgeIsEvidence() {
	const sch = "satisfy_plural_edges_inline_subtype.gql"
	tests := []struct {
		name    string
		query   string
		want    []Column
		wantErr error
		wantMsg string
	}{
		{
			name:    "an OPTIONAL hop is the only edge, property projected",
			query:   "MATCH (c) OPTIONAL MATCH (c)-[h:HAS_DESK]->(d:Desk) RETURN c.smallOnly",
			wantErr: ErrUnknownLabel,
			wantMsg: `unknown label: cannot infer type of unlabelled binding "c" — ` +
				`every edge reaching it is an OPTIONAL match, which drops no row, ` +
				`so its type is unconstrained`,
		},
		{
			name:    "an OPTIONAL hop is the only edge, binding projected",
			query:   "MATCH (c) OPTIONAL MATCH (c)-[h:HAS_DESK]->(d:Desk) RETURN c",
			wantErr: ErrUnknownLabel,
			wantMsg: `unknown label: cannot infer type of unlabelled binding "c" — ` +
				`every edge reaching it is an OPTIONAL match, which drops no row, ` +
				`so its type is unconstrained`,
		},
		{
			name:  "the same hop made mandatory still commits",
			query: "MATCH (c) MATCH (c)-[h:HAS_DESK]->(d:Desk) RETURN c.smallOnly",
			want:  []Column{{Name: "c.smallOnly", Type: ResolvedProperty{Type: graph.PropertyType("STRING")}}},
		},
		{
			// A minimum-1 variable-length hop is still an inner join: it drops
			// every row whose `c` sources no HAS_DESK. The guard is spelled
			// !e.Nullable() and not witnessesItsEndpoints for exactly this row —
			// the stricter predicate is false here, and reusing it would refuse a
			// query master answers soundly.
			name:  "a variable-length hop filters rows, so it still commits",
			query: "MATCH (c) MATCH (c)-[h:HAS_DESK*1..2]->(d:Desk) RETURN c.smallOnly",
			want:  []Column{{Name: "c.smallOnly", Type: ResolvedProperty{Type: graph.PropertyType("STRING")}}},
		},
		{
			// The binding the OPTIONAL hop INTRODUCES is the one shape an outer
			// join does constrain: `d` is null on the rows the join missed and a
			// Desk on the rest, so the TYPE is sound and nullability already
			// carries the miss. Refusing here would regress master.
			name:  "a binding introduced by the OPTIONAL hop is nullable, not refused",
			query: "MATCH (c:Company) OPTIONAL MATCH (c)-[h:HAS_DESK]->(d) RETURN d.id",
			want:  []Column{{Name: "d.id", Type: ResolvedProperty{Type: graph.PropertyType("INT"), Nullable: true}}},
		},
		{
			name:  "one witnessing edge is enough, the OPTIONAL one alongside it is not needed",
			query: "MATCH (p:Person)-[q:WORKS_AT]->(c) OPTIONAL MATCH (c)-[h:HAS_DESK]->(d:Desk) RETURN c.name",
			want:  []Column{{Name: "c.name", Type: ResolvedProperty{Type: graph.PropertyType("STRING")}}},
		},
		{
			name:    "no touching edge at all, the arm that already refused",
			query:   "MATCH (c) RETURN c.smallOnly",
			wantErr: ErrUnknownLabel,
			wantMsg: `unknown label: cannot infer type of unlabelled binding "c" — ` +
				`no edge in the pattern reaches a compatible schema node type`,
		},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			schema := s.loadSchema("invalid", sch)
			q, err := cypher.New(cypher.WithRegistry(regR7)).Parse(bytes.NewReader([]byte(tt.query)))
			s.Require().NoError(err)
			vq, err := New(schema, WithRegistry(regR7)).Resolve(q)
			if tt.wantErr != nil {
				s.Require().ErrorIs(err, tt.wantErr, tt.query)
				s.Require().EqualError(err, tt.wantMsg, tt.query)
				return
			}
			s.Require().NoError(err, tt.query)
			s.Require().Equal(tt.want, vq.Columns, tt.query)
		})
	}
}

// TestAnInferredEndpointIsTrustedOnlyWhenItsOwnEndsWereEnumerated closes the
// second half of the precondition TestNarrowingSkipsAnEndpointItCannotEnumerate
// opened. That test guards the endpoint the narrowing reads DIRECTLY. This one
// guards the endpoint it reads through Phase B: an unlabelled binding is typed
// by candidateTypes, which intersects across its touching edges and reads each
// far end by the keys endpointLabels gives. A far end that could not enumerate
// itself pins the unlabelled binding to one type when two are attainable, and
// the narrowing then reads that singleton as a satisfying set. The var endpoint
// the gate waves through is a laundered uncovered one.
//
// An uncovered far end now reaches this only through the VarEndpoint arm: the
// inline arm reports covering on both its returns, and the var arm reports what
// resolvedCovers holds. Four sites write the resolved node lane and two of them
// leave resolvedCovers alone — inferUnlabelled when candidateTypes did not
// report covering, and newScope's carry seed. Since sxzj the first of those is
// narrower than it reads: when the intersection is a singleton and some edge
// touching the binding IS trustworthy, Phase B commits the ATTAINABLE set to
// the plural lane instead, so the uncovered singleton survives only where no
// touching edge witnesses its endpoints at all. The first row below now lands
// on that widening rather than on the laundering it was written for — it chains
// an inferred `c` into a second inference `x`, and a plural `c` leaves `x`
// undetermined. carried_uncovered_endpoint_stays_plural.cypher in the corpus
// pins the carry-seed site, which the widening does not touch.
//
// The accepting rows are what stop the fix being "an inferred binding is never
// trusted". `MATCH (p:Person)-[r:WORKS_AT]->(c)` reads a plural var far end
// whose whole candidate slice IS the satisfying set, so `c` covers and the
// narrowing pins `p` through it; and the refusing row with its OPTIONAL made
// mandatory is accepted on the same schema one token away.
//
// The last row says covering is a CONJUNCTION and the rows above test one
// conjunct of it. Enumerating both far ends is not enough on its own: an edge
// that no returned row is guaranteed to have drops a type the surviving rows
// carry however perfectly its ends were enumerated. Only that row separates
// "every contributing edge's far end covered" from the rule the resolver needs.
func (s *ResolverSuite) TestAnInferredEndpointIsTrustedOnlyWhenItsOwnEndsWereEnumerated() {
	tests := []struct {
		name    string
		schema  string
		query   string
		want    []Column
		wantErr error
	}{
		{
			name:   "the inference read an uncovered inferred endpoint",
			schema: "satisfy_plural_edges_inline_subtype.gql",
			query: "MATCH (p:Person)-[q:WORKS_AT]->(c) " +
				"OPTIONAL MATCH (c)-[h:HAS_DESK]->(d:Desk) " +
				"MATCH (c)<-[w:WORKS_AT]-(x) " +
				"MATCH (x)-[w2:WORKS_AT]->(y:Company) RETURN y.smallOnly",
			// Employee&Person -[WORKS_AT]-> Company&Large is a matching row with
			// h/d null, and on it y is Company&Large, which has no smallOnly.
			//
			// sxzj moved WHERE the chain breaks, not whether. Phase B no longer
			// commits `c` to Company alone: WORKS_AT is trustworthy and enumerates
			// both company types, so `c` goes plural and the singleton this row
			// was written to catch is never written. `x` then reads a plural `c`
			// and Phase B declines it by name, one binding earlier and without
			// having to reach `y`. Both the old verdict and this one are refusals;
			// this one names the binding whose type the query never determined.
			wantErr: ErrAmbiguousBinding,
		},
		{
			name:   "the same chain on a far end every returned row has",
			schema: "satisfy_plural_edges_inline_subtype.gql",
			query: "MATCH (p:Person)-[q:WORKS_AT]->(c) " +
				"MATCH (c)-[h:HAS_DESK]->(d:Desk) " +
				"MATCH (c)<-[w:WORKS_AT]-(x) " +
				"MATCH (x)-[w2:WORKS_AT]->(y:Company) RETURN y.smallOnly",
			// One token apart from the row above. HAS_DESK is declared from the
			// bare Company only and every returned row now has one, so c really
			// is Company on all of them and the chain through it is sound.
			want: []Column{{Name: "y.smallOnly", Type: ResolvedProperty{Type: graph.PropertyType("STRING")}}},
		},
		{
			name:   "the inference read a plural var endpoint",
			schema: "satisfy_plural_edges_reversed.gql",
			query:  "MATCH (p:Person)-[r:WORKS_AT]->(c) RETURN p.employeeId",
			// WORKS_AT is declared from Employee&Person only, so `c` infers to
			// Company from a covering end and the closure pins `p`.
			want: []Column{{Name: "p.employeeId", Type: ResolvedProperty{Type: graph.PropertyType("INT")}}},
		},
		{
			name:   "the inference folded an edge no returned row has",
			schema: "satisfy_plural_edges_inline_subtype.gql",
			query: "MATCH (p:Person)-[q:WORKS_AT]->(c) " +
				"OPTIONAL MATCH (c)-[h:HAS_DESK]->(d:Desk) RETURN p.personOnly",
			// The third row is the other half of covering, and it is the half
			// the first two do not reach. Every far end here enumerates itself
			// perfectly — `(d:Desk)` is a singular BindNode and `p` is a plural
			// var — so the enumeration test both rows above turn on passes on
			// both edges. What fails is the row guarantee: HAS_DESK is declared
			// from the bare Company only, so folding it pins `c` to Company,
			// but the hop is OPTIONAL and filters nothing. The Employee&Person
			// -[WORKS_AT]-> Company&Large row comes back with h/d null, its `c`
			// is Company&Large, and its `p` has no personOnly.
			wantErr: ErrUnknownProperty,
		},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			sch := s.loadSchema("invalid", tt.schema)
			q, err := cypher.New(cypher.WithRegistry(regR7)).Parse(bytes.NewReader([]byte(tt.query)))
			s.Require().NoError(err)
			vq, err := New(sch, WithRegistry(regR7)).Resolve(q)
			if tt.wantErr != nil {
				s.Require().ErrorIs(err, tt.wantErr, tt.query)
				return
			}
			s.Require().NoError(err, tt.query)
			s.Require().Equal(tt.want, vq.Columns, tt.query)
		})
	}
}

// TestPhaseBAsksTheWholeWitnessPredicateNotOneArmOfIt covers the arms of
// witnessesItsEndpoints that the OPTIONAL row in
// TestAnInferredEndpointIsTrustedOnlyWhenItsOwnEndsWereEnumerated does not
// reach. Its own OPTIONAL arm is the only one with a demonstrated witness, and
// a fix that pattern-matched on Nullable() alone would leave that row green and
// these four wrong.
//
// Each row is one disqualifier, paired with the SAME query carrying a plain
// mandatory single hop instead. The pair is what makes each row say anything:
// on the witnessing spelling every returned row really does have a HAS_DESK out
// of `c`, HAS_DESK is declared from the bare Company only, so `c` is Company on
// every row, the only WORKS_AT into Company comes from the bare Person, and
// `p.personOnly` is a column every row has. That acceptance is the branch's own
// win, on the same schema, one token away from each refusal — so none of these
// rows can be passing because Phase B stopped covering altogether.
//
// The CREATE and MERGE rows also say the `written` set reaches this far at all:
// a written edge is a binding like any other and does enter Phase B's edge
// list, so before this it was folded in and reported as covering.
func (s *ResolverSuite) TestPhaseBAsksTheWholeWitnessPredicateNotOneArmOfIt() {
	const witnessed = "MATCH (p:Person)-[q:WORKS_AT]->(c) " +
		"MATCH (c)-[h:HAS_DESK]->(d:Desk) RETURN p.personOnly"
	tests := []struct {
		name  string
		query string
	}{
		{
			name: "a zero lower bound",
			query: "MATCH (p:Person)-[q:WORKS_AT]->(c) " +
				"MATCH (c)-[h:HAS_DESK*0..1]->(d:Desk) RETURN p.personOnly",
		},
		{
			name: "an exact zero quantifier",
			query: "MATCH (p:Person)-[q:WORKS_AT]->(c) " +
				"MATCH (c)-[h:HAS_DESK*0]->(d:Desk) RETURN p.personOnly",
		},
		{
			name: "a count above one",
			query: "MATCH (p:Person)-[q:WORKS_AT]->(c) " +
				"MATCH (c)-[h:HAS_DESK*2]->(d:Desk) RETURN p.personOnly",
		},
		{
			name: "a CREATE",
			query: "MATCH (p:Person)-[q:WORKS_AT]->(c) " +
				"CREATE (c)-[h:HAS_DESK]->(d:Desk) RETURN p.personOnly",
		},
		{
			name: "a MERGE",
			query: "MATCH (p:Person)-[q:WORKS_AT]->(c) " +
				"MERGE (c)-[h:HAS_DESK]->(d:Desk) RETURN p.personOnly",
		},
	}
	sch := s.loadSchema("invalid", "satisfy_plural_edges_inline_subtype.gql")
	resolve := func(src string) ([]Column, error) {
		q, err := cypher.New(cypher.WithRegistry(regR7)).Parse(bytes.NewReader([]byte(src)))
		s.Require().NoError(err)
		vq, err := New(sch, WithRegistry(regR7)).Resolve(q)
		return vq.Columns, err
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			_, err := resolve(tt.query)
			s.Require().ErrorIs(err, ErrUnknownProperty, tt.query)

			got, err := resolve(witnessed)
			s.Require().NoError(err,
				"the same query with a mandatory single hop IS evidence about every row, so the refusal above is this disqualifier's and not Phase B refusing to cover at all")
			s.Require().Equal([]Column{{Name: "p.personOnly", Type: ResolvedProperty{Type: graph.PropertyType("STRING")}}}, got)
		})
	}
}

// TestACommitmentFromTheAttainableSetIsLEARNEDFrom pins the second return value
// of commit(): a commitment made from `attainable` is marked COVERING, whatever
// the per-edge conjunction said. That mark is the difference between the two
// accepts below and origin/master, which refuses both — so this is the one place
// that states sxzj widens what the resolver accepts as well as narrowing it.
//
// The shape: HAS_DESK is mandatory and reaches an inline `(d:Desk)`, so it
// witnesses its endpoints and its far end covers; it alone pins `c` to the bare
// Company, and every returned row really has it. The OPTIONAL WORKS_AT is the
// edge that fails the gate, so `inf.covered` is false and master leaves `c` in
// `resolved` without a resolvedCovers entry — an inference the narrowing is not
// allowed to read. On this branch `inferred` is the singleton {Company},
// `attainable` is the same set reached over the mandatory edges alone, and the
// commitment carries the covering mark that set earns. NarrowPluralEndpoints
// then reads `c` and pins the person side.
//
// Substituting `i.covered` for the `true` in commit() turns both rows into
// refusals, which is what makes them a pin rather than two more green fixtures:
// the goldens under valid/ redden on it too (TestValid requires NoError before
// it reaches a golden, so `-update` cannot bless the refusal), and these rows
// additionally hold the COLUMN and its nullability, which `-update` would
// rewrite.
//
// The refusing row is the control. One token — HAS_DESK made OPTIONAL — leaves
// no edge that both witnesses its endpoints and pins `c`, so `attainable` is
// the plural set and the widening commits it to the plural lane instead. Both
// accepts therefore rest on the mandatory hop being evidence, not on Phase B
// having stopped discriminating.
func (s *ResolverSuite) TestACommitmentFromTheAttainableSetIsLEARNEDFrom() {
	tests := []struct {
		name  string
		query string
		want  []Column
	}{
		{
			name: "the next hop out",
			query: "MATCH (c)-[h:HAS_DESK]->(d:Desk) " +
				"OPTIONAL MATCH (p:Person)-[q:WORKS_AT]->(c) " +
				"MATCH (c)<-[w:WORKS_AT]-(x:Person) RETURN x.personOnly",
			// `c` is the bare Company on every row, the only WORKS_AT into it is
			// declared out of the bare Person, so `x` is Person and personOnly is a
			// column every row has.
			want: []Column{{Name: "x.personOnly", Type: ResolvedProperty{Type: graph.PropertyType("STRING")}}},
		},
		{
			name: "two hops out",
			query: "MATCH (c)-[h:HAS_DESK]->(d:Desk) " +
				"OPTIONAL MATCH (p:Person)-[q:WORKS_AT]->(c) " +
				"MATCH (c)<-[w:WORKS_AT]-(x) " +
				"MATCH (x)-[w2:WORKS_AT]->(y:Company) RETURN y.smallOnly",
			// The same chain one link longer: `x` is inferred rather than labelled,
			// and `y` is narrowed from it. This is the shape
			// TestAnInferredEndpointIsTrustedOnlyWhenItsOwnEndsWereEnumerated
			// refuses when the pinning hop is the OPTIONAL one.
			want: []Column{{Name: "y.smallOnly", Type: ResolvedProperty{Type: graph.PropertyType("STRING")}}},
		},
	}
	const control = "OPTIONAL MATCH (c)-[h:HAS_DESK]->(d:Desk) " +
		"OPTIONAL MATCH (p:Person)-[q:WORKS_AT]->(c) " +
		"MATCH (c)<-[w:WORKS_AT]-(x:Person) RETURN x.personOnly"
	sch := s.loadSchema("valid", "satisfy_plural_edges_inline_subtype.gql")
	resolve := func(src string) ([]Column, error) {
		q, err := cypher.New(cypher.WithRegistry(regR7)).Parse(bytes.NewReader([]byte(src)))
		s.Require().NoError(err)
		vq, err := New(sch, WithRegistry(regR7)).Resolve(q)
		return vq.Columns, err
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			got, err := resolve(tt.query)
			s.Require().NoError(err, tt.query)
			s.Require().Equal(tt.want, got)
		})
	}
	s.Run("the control with no witnessing hop", func() {
		_, err := resolve(control)
		s.Require().ErrorIs(err, ErrUnknownProperty,
			"with HAS_DESK optional no edge both witnesses its endpoints and pins `c`, so the widening commits the plural set and `x` stays over both person types")
	})
}

// TestAnonymousEdgeNarrowsToTheTypeItCloses pins the TYPE that
// valid/plural_endpoint_anonymous_edge_closes_singular.cypher resolves to,
// rather than leaving it to that fixture's stored golden.
//
// Anonymous edges drive the narrowing — they are bindings like any other, with
// "" for a variable — and the fixture exists to say so. But a golden is not a
// behavioural pin in this repo: `go test -update` rewrites it, so a change that
// narrowed `p` to the WRONG one of the two satisfying types shows up as a diff
// a reviewer has to notice rather than as a red test. Only the refusal half of
// that fixture is self-defending: turn the narrowing off entirely and the query
// becomes ErrAmbiguousLabel, which -update cannot bless into a golden.
//
// So the type is asserted here, in source, where -update does not reach. The
// interesting half of the claim is which type: Employee&Person is the source of
// the only declared WORKS_AT, and the bare Person type satisfies `(:Person)`
// just as well.
func (s *ResolverSuite) TestAnonymousEdgeNarrowsToTheTypeItCloses() {
	src, err := os.ReadFile(filepath.Join(fixtureDir, "valid", "plural_endpoint_anonymous_edge_closes_singular.cypher"))
	s.Require().NoError(err)
	sch := s.loadSchema("valid", "satisfy_plural_edges.gql")
	q, err := cypher.New(cypher.WithRegistry(regR7)).Parse(bytes.NewReader(src))
	s.Require().NoError(err)
	vq, err := New(sch, WithRegistry(regR7)).Resolve(q)
	s.Require().NoError(err)
	s.Require().Equal([]Column{{Name: "p", Type: ResolvedNode{Labels: "Employee&Person"}}}, vq.Columns,
		"the anonymous WORKS_AT closes from Employee&Person, so that is the type p commits to — not the other type (:Person) satisfies")
}

// TestNarrowingASingularBindingCollidesWithALaterPluralRebind pins the message
// a narrowed binding produces when a later Part re-declares it with the label
// expression it started from.
//
// Before the narrowing `p` stayed plural across both Parts and the query was
// refused with ErrAmbiguousLabel. Now Part 0's WORKS_AT determines it, so Part
// 1's `(p:Person)` — the same text that bound it plural the first time — is a
// singular-to-plural re-bind, and R5's carry check refuses it instead. Both
// refuse, so this is not a regression; the point is that the second message is
// newly REACHABLE, from a widening that was shipped with nothing looking at it.
//
// The wording is correct as it stands and is left alone: `p` genuinely was
// carried as a singular node type, and the later Part genuinely does re-bind it
// as plural. It is also not this pass's message — scope.go's carry check owns
// it and other paths reach it — so changing it here would be a non-local edit
// to satisfy a local surprise.
func (s *ResolverSuite) TestNarrowingASingularBindingCollidesWithALaterPluralRebind() {
	sch := s.loadSchema("invalid", "satisfy_plural_edges_reversed.gql")
	q, err := cypher.New(cypher.WithRegistry(regR7)).Parse(bytes.NewReader([]byte(
		"MATCH (p:Person)-[w:WORKS_AT]->(c:Company) WITH p MATCH (p:Person) RETURN p.id")))
	s.Require().NoError(err)
	_, err = New(sch, WithRegistry(regR7)).Resolve(q)
	s.Require().ErrorIs(err, ErrPartBindingTypeConflict)
	s.Require().EqualError(err,
		`part binding type conflict: variable "p" carried as singular node type, re-bound as plural`)
}

// TestNarrowingToASmallerPluralSetIsWhatTheSetSaysItIs covers the arm that
// narrows a plural binding to a set that is smaller and still plural. It needs
// three satisfying types to exist at all: on two, "some but not all survive"
// and "exactly one survives" are the same sentence, and every other plural-
// endpoint schema in the corpus declares two.
//
// Neither half can be carried by a sentinel. The whole-entity form is refused
// with ErrAmbiguousLabel whether the set was narrowed to two or left at three,
// so what is asserted is the set the message enumerates — read back out, and
// required to be exactly the two, by content and by length. The property form
// is the other direction: staffId is declared on the two staff types and not on
// the bare person type, so ADR 0022's intersection over the pre-narrowing set
// of three has no such property and the column exists only because the third
// was dropped.
func (s *ResolverSuite) TestNarrowingToASmallerPluralSetIsWhatTheSetSaysItIs() {
	sch := s.loadSchema("invalid", "satisfy_plural_edges_three_types.gql")
	resolve := func(src string) ([]Column, error) {
		q, err := cypher.New(cypher.WithRegistry(regR7)).Parse(bytes.NewReader([]byte(src)))
		s.Require().NoError(err)
		vq, err := New(sch, WithRegistry(regR7)).Resolve(q)
		return vq.Columns, err
	}

	src, err := os.ReadFile(filepath.Join(fixtureDir, "invalid", "plural_endpoint_narrows_to_smaller_plural_set.cypher"))
	s.Require().NoError(err)
	_, err = resolve(string(src))
	s.Require().ErrorIs(err, ErrAmbiguousLabel)
	s.Require().Equal("Contractor&Person, Employee&Person", errAmbiguousLabelSet(err),
		"WORKS_AT is declared on the two staff types only, so the surviving set is those two — not the three the label satisfies, and not one of them")

	got, err := resolve("MATCH (p:Person)-[r:WORKS_AT]->(c:Company) RETURN p.staffId")
	s.Require().NoError(err, "staffId is on both survivors, so the intersection over the narrowed set has it")
	s.Require().Equal([]Column{{Name: "p.staffId", Type: ResolvedProperty{Type: graph.PropertyType("INT")}}}, got)
}

// TestDeferredEdgesCloseBeforeTheNarrowing pins the position of the narrowing
// inside Phase C: it runs after the deferred-close loop, so a deferred edge is
// closed against the pre-narrowing binding tables.
//
// The order is the one §4.6.2 states and the one NarrowPluralEndpoints' own
// comment gives a reason for — a narrowed endpoint slice re-classifies the
// readings of candidates that previously read both ways, so re-closing against
// it can manufacture an orientation disagreement the first close did not find.
// Nothing tested it. Moving the call above the deferred loop left the suite
// green, which makes both the comment and the spec paragraph an unbacked claim.
//
// This query separates the two positions. `x` is unlabelled, so REVIEWED is
// deferred to Phase C's second pass; WORKS_AT is not, and it narrows `p` to
// Employee&Person. Close REVIEWED first and it sees p's full satisfying set and
// commits both declared keys — an edge union. Narrow first and it sees one type
// and commits one key, so the same query answers a different type for `r`.
//
// Both columns are asserted together on purpose: `p` says the narrowing really
// did fire (or the test would pass by the narrowing being off), and `r` says
// the deferred close did not see it.
func (s *ResolverSuite) TestDeferredEdgesCloseBeforeTheNarrowing() {
	sch := s.loadSchema("invalid", "satisfy_plural_edges_reversed.gql")
	q, err := cypher.New(cypher.WithRegistry(regR7)).Parse(bytes.NewReader([]byte(
		"MATCH (p:Person)-[w:WORKS_AT]->(c:Company), (p)-[r:REVIEWED]->(x) RETURN p, r")))
	s.Require().NoError(err)
	vq, err := New(sch, WithRegistry(regR7)).Resolve(q)
	s.Require().NoError(err)

	s.Require().Equal([]Column{
		{Name: "p", Type: ResolvedNode{Labels: "Employee&Person"}},
		{Name: "r", Type: ResolvedEdgeUnion{EdgeKeys: []schema.EdgeKey{
			{Source: "Employee&Person", KeyLabels: "REVIEWED", Target: "Company"},
			{Source: "Person", KeyLabels: "REVIEWED", Target: "Company"},
		}}},
	}, vq.Columns)
}

// TestADirectedCloseDropsTheRightToLeftReading is the arrow's half of the
// endpoint narrowing: on a directed edge the pattern's left end gets Sources and
// its right end gets Targets, and neither end may collect the other's.
//
// The bug this pins (bd gqlc-pv0u) needed srcs and tgts to OVERLAP before it
// could show. edgeProbes already builds one orientation per directed edge, so
// every candidate satisfies the left-to-right reading by construction; the
// right-to-left reading is then not a second source of candidates but a second
// question asked of the same ones, and where the two endpoint slices share a key
// it answers yes and contributes the candidate's far end to the near end of the
// pattern. `(:Node)` satisfying all three types is what makes the slices equal
// here, which is the ordinary shape of a supertype label, not a contrived one.
//
// The two ends are asserted together because that is what makes this the ARROW's
// doing rather than a narrowing that merely got smaller. X is declared A->B and
// B->C, so following the arrow the left end is {A,B} — C has no outgoing X — and
// the right end is {B,C} — A has no incoming X. They are different sets, both
// proper subsets of the three the label satisfies, and each is the other's
// mirror. A narrowing that lost the right-to-left reading everywhere, or that
// applied the arrow to one end only, cannot produce that pair.
//
// The refusal itself is not the subject and does not move: two types survive at
// each end, so ErrAmbiguousLabel stands before and after. What changes is which
// types it names, which is why the assertion reads the enumeration rather than
// the sentinel — and why no query in this schema goes from refused to accepted,
// since A, B and C share only `id`. The narrowing is precision, in ADR 0006's
// safe direction; it is not a new acceptance.
func (s *ResolverSuite) TestADirectedCloseDropsTheRightToLeftReading() {
	sch := s.loadSchema("invalid", "satisfy_plural_edges_chain.gql")
	ambiguousSet := func(src string) string {
		q, err := cypher.New(cypher.WithRegistry(regR7)).Parse(bytes.NewReader([]byte(src)))
		s.Require().NoError(err)
		_, err = New(sch, WithRegistry(regR7)).Resolve(q)
		s.Require().ErrorIs(err, ErrAmbiguousLabel,
			"two types survive at each end, so the whole-entity refusal stands either way")
		return errAmbiguousLabelSet(err)
	}

	s.Require().Equal("A&Node, B&Node",
		ambiguousSet("MATCH (p:Node)-[r:X]->(q:Node) RETURN p"),
		"the arrow puts p on a Source of X, and C&Node has no outgoing X")
	s.Require().Equal("B&Node, C&Node",
		ambiguousSet("MATCH (p:Node)-[r:X]->(q:Node) RETURN q"),
		"the arrow puts q on a Target of X, and A&Node has no incoming X")

	// Control: the same pattern written undirected must keep all three at both
	// ends. Without this row the two above are also satisfied by a narrowing that
	// dropped the right-to-left reading unconditionally, which would be a
	// soundness bug on the undirected form -- there the reading is not a spurious
	// second answer but the whole reason edgeProbes emits both orientations.
	s.Require().Equal("A&Node, B&Node, C&Node",
		ambiguousSet("MATCH (p:Node)-[r:X]-(q:Node) RETURN p"),
		"an undirected close probes both orientations, so every type the label satisfies still stands")

	// The user-visible half. Above, both ends stay plural and only the named set
	// moves; here the left end narrows to ONE type, so a query the resolver used
	// to refuse now answers. KNOWS is declared Person->Person and
	// Person->Employee&Person, so every candidate's Source is Person and no
	// Employee&Person has an outgoing KNOWS — `a` was never ambiguous, and the
	// refusal was the right-to-left reading inventing a second type for it.
	//
	// `b` is asserted in the same breath because it is what makes this the arrow
	// and not the narrowing simply collapsing everything: the right end still
	// holds both types, since both are declared Targets. One end singular and the
	// other plural, from one pattern, is a shape neither the old behaviour nor a
	// blanket narrowing can produce.
	sym := s.loadSchema("valid", "satisfy_plural_edges_symmetric.gql")
	q, err := cypher.New(cypher.WithRegistry(regR7)).Parse(bytes.NewReader([]byte(
		"MATCH (a:Person)-[r:KNOWS]->(b:Person) RETURN a")))
	s.Require().NoError(err)
	vq, err := New(sym, WithRegistry(regR7)).Resolve(q)
	s.Require().NoError(err,
		"KNOWS runs only out of Person, so the arrow determines a and there is nothing to refuse")
	s.Require().Equal([]Column{{Name: "a", Type: ResolvedNode{Labels: "Person"}}}, vq.Columns)

	q, err = cypher.New(cypher.WithRegistry(regR7)).Parse(bytes.NewReader([]byte(
		"MATCH (a:Person)-[r:KNOWS]->(b:Person) RETURN b")))
	s.Require().NoError(err)
	_, err = New(sym, WithRegistry(regR7)).Resolve(q)
	s.Require().ErrorIs(err, ErrAmbiguousLabel)
	s.Require().Equal("Employee&Person, Person", errAmbiguousLabelSet(err),
		"both types are declared Targets of KNOWS, so the arrow settles nothing at the right end")
}

// errAmbiguousLabelSet lifts the candidate enumeration out of an
// ErrAmbiguousLabel message: everything after the final ": ".
func errAmbiguousLabelSet(err error) string {
	msg := err.Error()
	i := strings.LastIndex(msg, ": ")
	if i < 0 {
		return msg
	}
	return msg[i+2:]
}

// TestEndpointContributionUnionsTheTwoReadings states the per-edge contribution
// rule directly, on candidate sets the corpus cannot build.
//
// The resolver's own inputs always satisfy `contribution ⊆ that endpoint's
// satisfying set`, because every candidate was probed from it — so on real
// input a contribution that dropped one reading is still a plausible-looking
// subset, and only a schema where the two readings differ shows it. The rows
// below are those, written as the function's answer rather than as a query, and
// the mixed row is the one that separates union from either reading alone.
//
// Every row here is the UNDIRECTED rule, which is why each passes directed=false
// explicitly rather than letting a zero value speak for it. The directed rule is
// the last row, and TestADirectedCloseDropsTheRightToLeftReading is the same
// claim reached through a query.
func (s *ResolverSuite) TestEndpointContributionUnionsTheTwoReadings() {
	key := func(src, tgt string) schema.EdgeKey {
		return schema.EdgeKey{Source: graph.LabelSetKey(src), KeyLabels: "MENTORS", Target: graph.LabelSetKey(tgt)}
	}
	srcs := []graph.LabelSetKey{"Engineer&Person&Staff", "Manager&Person&Staff"}
	tgts := []graph.LabelSetKey{"Engineer&Person&Staff", "Manager&Person&Staff", "Person"}
	cands := []schema.EdgeKey{
		// Reads both ways: both its ends sit in both slices.
		key("Manager&Person&Staff", "Engineer&Person&Staff"),
		// Reads right-to-left only: Person is on the pattern's right and
		// nowhere on its left.
		key("Person", "Engineer&Person&Staff"),
	}

	keys := func(set map[graph.LabelSetKey]struct{}) []graph.LabelSetKey {
		out := make([]graph.LabelSetKey, 0, len(set))
		for k := range set {
			out = append(out, k)
		}
		slices.Sort(out)
		return out
	}

	s.Require().Equal([]graph.LabelSetKey{"Engineer&Person&Staff", "Manager&Person&Staff"},
		keys(endpointContribution(cands, srcs, tgts, patternLeft, false)),
		"left-to-right offers Manager and right-to-left offers Engineer; taking either alone loses a type the schema permits")
	s.Require().Equal([]graph.LabelSetKey{"Engineer&Person&Staff", "Manager&Person&Staff", "Person"},
		keys(endpointContribution(cands, srcs, tgts, patternRight, false)),
		"the right end reads the same candidates from the other side, and the right-to-left candidate offers Person there")

	// Control: on a candidate that reads one way only, the two ends disagree
	// about which type they get — so the rows above are not both sides
	// collapsing to one answer.
	oneWay := []schema.EdgeKey{key("Manager&Person&Staff", "Person")}
	s.Require().Equal([]graph.LabelSetKey{"Manager&Person&Staff"},
		keys(endpointContribution(oneWay, srcs, tgts, patternLeft, false)))
	s.Require().Equal([]graph.LabelSetKey{"Person"},
		keys(endpointContribution(oneWay, srcs, tgts, patternRight, false)))

	// Equal slices: the shape a pattern takes when ONE variable names both ends
	// of an edge, because endpointLabels then hands both ends the same keys.
	// Every candidate reads both ways, so the two ends' contributions coincide
	// — which is what makes NarrowPluralEndpoints' union across a binding's two
	// end-occupancies a no-op no query can observe. Stated here because it is
	// the reason that union cannot be covered where it is written.
	equal := []graph.LabelSetKey{"Employee&Person", "Person"}
	loop := []schema.EdgeKey{key("Employee&Person", "Person")}
	s.Require().Equal(
		keys(endpointContribution(loop, equal, equal, patternLeft, false)),
		keys(endpointContribution(loop, equal, equal, patternRight, false)),
		"with srcs == tgts a candidate's two readings are the same predicate, so neither end can learn more than the other")
	s.Require().Equal([]graph.LabelSetKey{"Employee&Person", "Person"},
		keys(endpointContribution(loop, equal, equal, patternLeft, false)),
		"and what they both learn is both of the key's ends, or the equality above holds by being empty twice")

	// Directed, on the very row above: with srcs == tgts every candidate reads
	// both ways, so this is where dropping the second reading moves the answer by
	// the widest margin the function admits. The two ends now disagree — the left
	// gets the key's Source and the right its Target — which is the arrow being
	// obeyed, and is the equality five lines up broken on purpose.
	s.Require().Equal([]graph.LabelSetKey{"Employee&Person"},
		keys(endpointContribution(loop, equal, equal, patternLeft, true)),
		"a directed close puts the pattern's left end on the candidate's Source alone")
	s.Require().Equal([]graph.LabelSetKey{"Person"},
		keys(endpointContribution(loop, equal, equal, patternRight, true)),
		"and its right end on the Target alone")
}

// TestTwoSwappedPairsReportsTheFirstInCandidateOrder holds §4.4's determinism
// claim on the only input that can observe it: a candidate set carrying two
// disjoint swapped pairs, where each pair on its own is a truthful answer to
// "which two candidates disagree" and nothing but the scan order picks between
// them.
//
// Every undirected fixture that shipped before this one carries exactly one
// pair, so on all of them "the first witness on each side" and "the only
// witness on each side" are the same sentence, and the message pins say nothing
// about order. Here they come apart: a scan that kept the last witness on
// either side, or that walked the two sides independently, reports the other
// pair while still reporting a true disagreement.
//
// The two directed twins are the guard against this covering nothing. A schema
// that in fact produced one pair would satisfy the message pin just as well, so
// the claim "two pairs" is asserted rather than assumed: each twin must close
// to two candidates, each of the forward twin's keys must have its mirror in
// the reverse twin's, and the four together must be four distinct keys. That is
// two swapped pairs, stated as the property rather than as a count.
//
// The twins are *read off the fixture's own pattern* (undirectedEdgePattern),
// not written out here. A guard built from its own hard-coded endpoints proves
// two pairs exist somewhere in the schema and holds nothing against the query
// under test: narrowing the fixture's right endpoint to a single satisfying type
// leaves one pair and a message pin that still passes, which is the
// green-because-it-is-looking-at-nothing mode this fixture exists to close.
//
// What is read off is each endpoint's label expression as written, which the
// twins put back through node satisfaction. On this fixture's two VarEndpoints
// that is the relation the resolver used too, so the twins probe the set the
// fixture closed over; on an inline endpoint it would not be. See
// endpointLabels below.
//
// The message is read with edgeKeyInMessage and matched as a set, because one
// key's rendering is a substring of another's — Person-[REVIEWED]->Company sits
// inside Employee&Person-[REVIEWED]->Company&Startup — so a Contains/NotContains
// pair would silently be answering a different question.
func (s *ResolverSuite) TestTwoSwappedPairsReportsTheFirstInCandidateOrder() {
	const fixture = "ambiguous_edge_orientation_two_swapped_pairs.cypher"

	mapping := s.loadMapping("invalid")
	schemaName, ok := mapping[fixture]
	s.Require().True(ok, "unmapped invalid fixture %q", fixture)
	sch := s.loadSchema("invalid", schemaName)
	q := s.loadQuery(filepath.Join(fixtureDir, "invalid", fixture))

	pattern := s.undirectedEdgePattern(q)

	unionKeys := func(src string) []schema.EdgeKey {
		pq, err := cypher.New(cypher.WithRegistry(regR7)).Parse(bytes.NewReader([]byte(src)))
		s.Require().NoError(err)
		vq, err := New(sch, WithRegistry(regR7)).Resolve(pq)
		s.Require().NoErrorf(err, "%s: the directed twin of the fixture's pattern must resolve", src)
		s.Require().Len(vq.Columns, 1)
		u, ok := vq.Columns[0].Type.(ResolvedEdgeUnion)
		s.Require().Truef(ok, "%s: want an edge union, got %T — this reading contributes fewer than two candidates, so the fixture carries at most one swapped pair", src, vq.Columns[0].Type)
		return u.EdgeKeys
	}
	mirror := func(k schema.EdgeKey) schema.EdgeKey {
		return schema.EdgeKey{Source: k.Target, KeyLabels: k.KeyLabels, Target: k.Source}
	}

	l2r := unionKeys(pattern.directed(pattern.source, pattern.target))
	r2l := unionKeys(pattern.directed(pattern.target, pattern.source))
	s.Require().Len(l2r, 2, "the left-to-right reading must contribute two candidates")
	s.Require().Len(r2l, 2, "the right-to-left reading must contribute two candidates")
	distinct := make(map[schema.EdgeKey]struct{}, 4)
	for _, k := range l2r {
		s.Require().Containsf(r2l, mirror(k),
			"%s has no counterparty, so it is not half of a swapped pair", formatEdgeKey(k))
		distinct[k] = struct{}{}
	}
	for _, k := range r2l {
		distinct[k] = struct{}{}
	}
	s.Require().Len(distinct, 4, "the two pairs must be disjoint, or there is only one pair here")

	_, err := New(sch, WithRegistry(regR7)).Resolve(q)
	s.Require().ErrorIs(err, ErrAmbiguousEdgeOrientation)
	s.Require().ElementsMatch(
		[]string{
			"Employee&Person-[REVIEWED]->Company&Startup",
			"Company&Startup-[REVIEWED]->Employee&Person",
		},
		edgeKeyInMessage.FindAllString(err.Error(), -1),
		"the reported pair must be the first witness on each side, in candidate order")
}

// edgePattern is one undirected edge pattern reduced to the three label
// expressions a directed twin can be rebuilt from: the two ends as the author
// wrote them and the edge's own alternation.
type edgePattern struct {
	source graph.LabelSetKey
	edge   string // the alternation, "|"-joined as written
	target graph.LabelSetKey
}

// directed spells a single-column query running from one end to the other with
// an explicit arrow. Fresh variable names: the twin is a probe of the schema,
// not a rewrite of the fixture, and the fixture's own names are irrelevant to
// which candidates close.
func (p edgePattern) directed(from, to graph.LabelSetKey) string {
	return fmt.Sprintf("MATCH (x:%s)-[e:%s]->(y:%s) RETURN e", from, p.edge, to)
}

// undirectedEdgePattern reads the one undirected edge binding out of a parsed
// query and returns its two endpoints' label expressions plus its own.
//
// This is what keeps a fixture's guard pointed at that fixture. A guard that
// names its endpoints itself is a second copy of the fixture, and the two drift
// on the next edit to either — the fixture narrows, the guard keeps resolving
// the twins it was born with, and the suite stays green over a query that no
// longer has the shape the guard reports.
//
// An endpoint's labels live on the NodeBinding it names, or inline on the
// endpoint itself, and both are read — but they are not the same thing to the
// closure, which is endpointLabels' subject.
func (s *ResolverSuite) undirectedEdgePattern(q query.Query) edgePattern {
	var (
		got   edgePattern
		found bool
	)
	for _, branch := range q.Branches {
		for _, part := range branch.Parts {
			nodes := make(map[string]graph.LabelSetKey, len(part.Bindings))
			for _, b := range part.Bindings {
				if nb, ok := b.(query.NodeBinding); ok {
					nodes[nb.Variable()] = nb.Labels().Key()
				}
			}
			for _, b := range part.Bindings {
				eb, ok := b.(query.EdgeBinding)
				if !ok || eb.Directed() {
					continue
				}
				s.Require().False(found, "the fixture must carry exactly one undirected edge binding")
				found = true
				got = edgePattern{
					source: s.endpointLabels(eb.Source(), nodes),
					edge:   strings.Join(eb.Labels(), "|"),
					target: s.endpointLabels(eb.Target(), nodes),
				}
			}
		}
	}
	s.Require().True(found, "the fixture must carry an undirected edge binding, or there is no orientation to be ambiguous about")
	s.Require().NotEmpty(got.edge, "the edge must carry a label alternation")
	return got
}

// endpointLabels is the label expression one end of a pattern was written with:
// the labels of the NodeBinding a variable endpoint names, or the inline set. It
// is the spelled expression, not the key the resolver closed the edge over, and
// directed() re-spells it as `(x:Labels)` — so the twin puts it back through
// node satisfaction.
//
// For a VarEndpoint that is faithful: the resolver reaches its key by satisfying
// the same spelled labels, so the twin probes the set the fixture closed over.
// For an InlineEndpoint it is not. The resolver's own endpointLabels keys an
// inline end on the labels as spelled — an exact match against declared
// identity rather than a satisfaction test, a known gap tracked as gqlc-h9n.23 —
// so a twin of an inline endpoint can probe a strictly wider candidate set than
// the fixture itself saw. No fixture reaching here carries one today; rewriting
// an end to the inline form would need this helper reconciled with the resolver
// first, or the twins stop reporting on the query under test.
func (s *ResolverSuite) endpointLabels(e query.Endpoint, nodes map[string]graph.LabelSetKey) graph.LabelSetKey {
	switch ep := e.(type) {
	case query.VarEndpoint:
		labels, ok := nodes[ep.Variable()]
		s.Require().Truef(ok, "endpoint %q names no node binding in this part", ep.Variable())
		s.Require().NotEmptyf(labels, "endpoint %q carries no labels, so it has no directed twin to probe", ep.Variable())
		return labels
	case query.InlineEndpoint:
		labels := ep.Labels().Key()
		s.Require().NotEmpty(labels, "an inline endpoint with no labels has no directed twin to probe")
		return labels
	default:
		s.Require().Failf("unhandled endpoint", "%T", e)
		return ""
	}
}

// TestEdgeUnionKeysAreASet asserts ResolvedEdgeUnion.EdgeKeys carries each
// schema EdgeKey at most once, over the whole valid corpus.
//
// The goldens cannot carry this: they are machine-written, so a regression
// that reintroduces a repeat is absorbed by the next -update run and the
// suite stays green. Everything downstream reads the candidate count as the
// count of distinct schema edge types — §4.6's verdict table dispatches on
// it, and codegen emits one dispatch branch per member, which a repeat turns
// into a duplicate case that does not compile.
func (s *ResolverSuite) TestEdgeUnionKeysAreASet() {
	files, err := filepath.Glob(filepath.Join(fixtureDir, "valid", "*.cypher"))
	s.Require().NoError(err)
	s.Require().NotEmpty(files)

	mapping := s.loadMapping("valid")
	seenUnion := false

	for _, path := range files {
		name := filepath.Base(path)
		schemaName, ok := mapping[name]
		s.Require().True(ok, "unmapped valid fixture %q", name)

		vq, err := New(s.loadSchema("valid", schemaName), WithRegistry(regR7)).Resolve(s.loadQuery(path))
		s.Require().NoError(err)

		for _, col := range vq.Columns {
			for _, u := range collectEdgeUnions(col.Type) {
				seenUnion = true
				seenKey := make(map[schema.EdgeKey]struct{}, len(u.EdgeKeys))
				for _, k := range u.EdgeKeys {
					_, dup := seenKey[k]
					s.Require().Falsef(dup, "%s column %q: edge key %s appears twice in %v", name, col.Name, formatEdgeKey(k), u.EdgeKeys)
					seenKey[k] = struct{}{}
				}
			}
		}
	}
	s.Require().True(seenUnion, "no fixture projects an edge union — the assertion above is vacuous")
}

// collectEdgeUnions gathers every ResolvedEdgeUnion reachable from a column
// type, descending list-element chains (a var-length multi-type edge nests
// the union one or more levels down).
func collectEdgeUnions(t ResolvedType) []ResolvedEdgeUnion {
	switch tt := t.(type) {
	case ResolvedEdgeUnion:
		return []ResolvedEdgeUnion{tt}
	case ResolvedList:
		return collectEdgeUnions(tt.Element)
	default:
		return nil
	}
}

// edgeKeyInMessage matches one formatEdgeKey rendering inside a fail-message.
var edgeKeyInMessage = regexp.MustCompile(`[^\s,]+-\[[^\]]+\]->[^\s,]+`)

// TestEdgeFailMessagesListEachTriedKeyOnce holds both edge fail-messages that
// enumerate EdgeKeys to one entry per key. A message reading "matches
// Person-[KNOWS]->Person left-to-right and Person-[KNOWS]->Person
// right-to-left" tells the reader their query
// is ambiguous between a thing and itself, and the remedy it prescribes —
// constrain the endpoints — cannot be followed when the endpoints are already
// equal. No corpus fixture covers either message on that input: every
// undirected fixture in invalid/ runs between two different node types.
//
// The rows brace edgeProbes' dedupe from both sides, so it cannot be widened
// or narrowed silently. The self-loop row fails if the dedupe is dropped —
// nothing else pins describeTriedEdges, which enumerates probes the schema
// rejected and so never reaches the candidate set. The two-node-type row is
// the genuine §4.6 case C, and it fails if the dedupe keys on the label
// instead of the whole EdgeKey: the two orientations collapse to one, the
// candidate count drops to 1, and an ambiguous query resolves as case B.
func (s *ResolverSuite) TestEdgeFailMessagesListEachTriedKeyOnce() {
	tests := []struct {
		name    string
		schema  string
		query   string
		wantErr error
	}{
		{
			name:    "unknown edge names each tried orientation once",
			schema:  "social_r3.gql",
			query:   "MATCH (a:Person)-[r:MISSING]-(b:Person) RETURN r",
			wantErr: ErrUnknownEdge,
		},
		{
			name:    "ambiguous orientation names each matched key once",
			schema:  "social_r3.gql",
			query:   "MATCH (a:Person)-[r:AUTHORED]-(b:Post) RETURN r",
			wantErr: ErrAmbiguousEdgeOrientation,
		},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			q, err := cypher.New(cypher.WithRegistry(regR7)).Parse(bytes.NewReader([]byte(tt.query)))
			s.Require().NoError(err)

			_, err = New(s.loadSchema("valid", tt.schema), WithRegistry(regR7)).Resolve(q)
			s.Require().ErrorIs(err, tt.wantErr)

			// Match the keys out of the prose rather than splitting on the
			// separator: both messages glue their first key to a sentinel
			// prefix, which would make an exact repeat compare unequal.
			keys := edgeKeyInMessage.FindAllString(err.Error(), -1)
			s.Require().NotEmpty(keys, "message enumerated no edge keys — the assertion below is vacuous")
			seen := make(map[string]struct{}, len(keys))
			for _, k := range keys {
				_, dup := seen[k]
				s.Require().Falsef(dup, "edge key %q listed twice in %q", k, err.Error())
				seen[k] = struct{}{}
			}
		})
	}
}

// TestUnlabelledIntersectionFixtureIsLoadBearing guards the one property that
// makes valid/unlabelled_via_edge_intersection.cypher worth having: each of its
// two edges leaves "a" ambiguous on its own, so only the intersection of their
// candidate sets picks a type.
//
// intersect exists to narrow across several edges, but every other unlabelled
// fixture has exactly one edge touching the unlabelled variable — the
// accumulator is seeded and never intersected, leaving the function dead for the
// whole suite. Replacing intersect with the identity, the union, or the empty
// map all left the suite green before this fixture existed.
//
// The golden alone would not keep it that way. Narrow either edge to a single
// candidate and the query still resolves to Person, the golden still matches,
// and intersect goes back to being untested silently. So the halves are asserted
// here rather than left as a property of the fixture text.
func (s *ResolverSuite) TestUnlabelledIntersectionFixtureIsLoadBearing() {
	sch := s.loadSchema("valid", "social_r7.gql")

	fixture := filepath.Join(fixtureDir, "valid", "unlabelled_via_edge_intersection.cypher")
	src, err := os.ReadFile(fixture)
	s.Require().NoError(err)

	// The fixture's two path patterns, each resolved alone.
	for _, pattern := range []string{
		"(a)-[:AUTHORED|KNOWS]->(x:Person)",
		"(a)-[:EMPLOYS|KNOWS]->(y:Person)",
	} {
		s.Run(pattern, func() {
			s.Require().Contains(string(src), pattern, "the fixture must still contain this pattern")

			q, err := cypher.New(cypher.WithRegistry(regR7)).Parse(
				bytes.NewReader([]byte("MATCH " + pattern + " RETURN a")))
			s.Require().NoError(err)

			_, err = New(sch, WithRegistry(regR7)).Resolve(q)
			s.Require().ErrorIs(err, ErrAmbiguousBinding,
				"this edge alone must leave the binding ambiguous, or the fixture stops testing intersect")
		})
	}
}

// TestEdgeUnionArityFixturesAreLoadBearing guards the property the two
// union_edge_union_arity_prefix fixtures rest on: one branch's edge-key list is
// a strict PREFIX of the other's, so only resolvedTypeEqual's arity check
// separates them.
//
// Prefix-ness is the whole point. The obvious mismatched pair — directed
// AUTHORED|LIKES against its undirected form — differs at index 1 because the
// reverse-direction key is interleaved, so the element-wise loop rejects it with
// the arity check already deleted. Nothing in the corpus was a prefix pair,
// which is why deleting that check left the suite green.
//
// Ordering matters too, and the two fixtures cover one case each. The call is
// resolvedTypeEqual(other, base), so with the arity check gone: branch 0 longer
// makes the loop run over the shorter list, match, and return true — two
// different edge-union types declared equal, and the model emits branch 0's type
// for a query whose other branch cannot produce it. Branch 0 shorter runs the
// loop off the end of base and panics on user input.
func (s *ResolverSuite) TestEdgeUnionArityFixturesAreLoadBearing() {
	sch := s.loadSchema("invalid", "social_edgeunion.gql")

	resolveKeys := func(pattern string) []schema.EdgeKey {
		q, err := cypher.New(cypher.WithRegistry(regR7)).Parse(
			bytes.NewReader([]byte("MATCH " + pattern + " RETURN r")))
		s.Require().NoError(err)
		vq, err := New(sch, WithRegistry(regR7)).Resolve(q)
		s.Require().NoError(err)
		u, ok := vq.Columns[0].Type.(ResolvedEdgeUnion)
		s.Require().True(ok, "%s must resolve r to an edge union, not %T", pattern, vq.Columns[0].Type)
		return u.EdgeKeys
	}

	short := resolveKeys("(p:Person)-[r:AUTHORED|LIKES]->(post:Post)")
	long := resolveKeys("(p:Person)-[r:AUTHORED|LIKES|SHARED]->(post:Post)")

	s.Require().Less(len(short), len(long), "the two branches must differ in arity")
	s.Require().Equal(long[:len(short)], short,
		"the shorter key list must be a strict prefix of the longer, or the element-wise loop catches the mismatch on its own and the arity check goes untested")

	// And both fixtures really do pair those two branches, so the assertion above
	// is about the queries under test rather than a pattern nothing uses.
	for _, name := range []string{
		"union_edge_union_arity_prefix.cypher",
		"union_edge_union_arity_prefix_reversed.cypher",
	} {
		src, err := os.ReadFile(filepath.Join(fixtureDir, "invalid", name))
		s.Require().NoError(err)
		s.Require().Contains(string(src), "[r:AUTHORED|LIKES]->", name)
		s.Require().Contains(string(src), "[r:AUTHORED|LIKES|SHARED]->", name)
	}
}

// TestRelationshipTypeConflictIsRefusedOnBothSidesOfWITH pins the division of
// labour between the two stages that can see a relationship variable re-bound
// to a conflicting type (gqlc-rrtl). The verdict is the same either way —
// refusal, because a relationship has exactly one type — but the fail-site is
// not, and neither stage can take the other's case.
//
// Within one part the parser owns it: byVar dedup is per-part by design (spec
// §3), so the parser is the only stage that still sees the separate
// occurrences. It collapses them into one binding, which is precisely why the
// resolver cannot re-derive the conflict downstream.
//
// Across a WITH the resolver owns it: the parser deliberately treats a name
// re-MATCHed in a later part as a fresh binding, and the carried type lives in
// the resolver's branchState. Making the parser reach that would mean
// duplicating branchState in the listener.
//
// The negative half is the load-bearing one: the parser must NOT refuse the
// cross-part twin. If it started to, the resolver's cross-part guard would go
// dark with the suite green, because the query would never reach it.
func (s *ResolverSuite) TestRelationshipTypeConflictIsRefusedOnBothSidesOfWITH() {
	sch := s.loadSchema("invalid", "social_edgeunion.gql")

	const withinPart = "MATCH (:Person)-[r:AUTHORED]->(:Post), (:Person)-[r:LIKES]->(:Post) RETURN r"
	const acrossWITH = "MATCH (:Person)-[r:AUTHORED]->(:Post) WITH r MATCH (:Person)-[r:LIKES]->(:Post) RETURN r"

	_, err := cypher.New(cypher.WithRegistry(regR7)).Parse(bytes.NewReader([]byte(withinPart)))
	s.Require().ErrorIs(err, cypher.ErrUnsatisfiableRelationshipType,
		"the within-part conflict is the parser's: it is the last stage that sees two occurrences")

	q, err := cypher.New(cypher.WithRegistry(regR7)).Parse(bytes.NewReader([]byte(acrossWITH)))
	s.Require().NoError(err,
		"the parser must leave the cross-part conflict alone, or the resolver's guard below stops being reached")
	_, err = New(sch, WithRegistry(regR7)).Resolve(q)
	s.Require().ErrorIs(err, ErrPartBindingTypeConflict,
		"the cross-part conflict is the resolver's: only it carries the type across the WITH")
}

// unionTypeArmRow is one invalid fixture that reaches ErrUnionColumnMismatch's
// type arm, with both projections written out by hand.
type unionTypeArmRow struct {
	fixture string
	failing string // what the branch that failed the comparison projected
	branch0 string // what branch 0 projected
}

// unionTypeArmRows is the hand-written expectation for every invalid fixture
// that reaches the type arm. TestUnionColumnMismatchTypeArmRowsCoverTheArm
// holds it total against the corpus, so a fixture that starts reaching the arm
// cannot slip in unpinned.
//
// The renderings are hand-written rather than derived from the resolved value:
// a derived expectation passes whatever the renderer emits, which is the defect
// this table exists to catch.
var unionTypeArmRows = []unionTypeArmRow{
	{
		fixture: "union_node_type_mismatch.cypher",
		failing: "node Post (not null)",
		branch0: "node Person (not null)",
	},
	{
		fixture: "union_edge_union_keys_mismatch.cypher",
		failing: "edgeUnion {Person-[AUTHORED]->Note, Person-[LIKES]->Note} (not null)",
		branch0: "edgeUnion {Person-[AUTHORED]->Post, Person-[LIKES]->Post} (not null)",
	},
	{
		// Same keys on both sides: nullability is the only axis that
		// separates them, so a rendering that drops it re-collapses the two.
		fixture: "union_edge_union_nullability_mismatch.cypher",
		failing: "edgeUnion {Person-[AUTHORED]->Post, Person-[LIKES]->Post} (nullable)",
		branch0: "edgeUnion {Person-[AUTHORED]->Post, Person-[LIKES]->Post} (not null)",
	},
	{
		// One key list is a strict prefix of the other. The two still render
		// apart without the braces — nullabilityNote terminates the list — so
		// these two rows pin the arity axis, not the delimiters.
		fixture: "union_edge_union_arity_prefix.cypher",
		failing: "edgeUnion {Person-[AUTHORED]->Post, Person-[LIKES]->Post} (not null)",
		branch0: "edgeUnion {Person-[AUTHORED]->Post, Person-[LIKES]->Post, Person-[SHARED]->Post} (not null)",
	},
	{
		fixture: "union_edge_union_arity_prefix_reversed.cypher",
		failing: "edgeUnion {Person-[AUTHORED]->Post, Person-[LIKES]->Post, Person-[SHARED]->Post} (not null)",
		branch0: "edgeUnion {Person-[AUTHORED]->Post, Person-[LIKES]->Post} (not null)",
	},
	{
		// Nullability is the separating axis; the scalar token agrees.
		fixture: "union_column_nullability_mismatch.cypher",
		failing: "property:STRING (nullable)",
		branch0: "property:STRING (not null)",
	},
	{
		// The scalar token is the separating axis; nullability agrees on one
		// side and disagrees on the other, so between this row and the one
		// above neither axis can be dropped without a row noticing.
		fixture: "union_column_type_mismatch.cypher",
		failing: "property:INT (nullable)",
		branch0: "property:STRING (not null)",
	},
	{
		// Three branches, and the mismatch is against branch 2 — the only row
		// where the failing branch is not branch 1. Both sides are NOT NULL, so
		// the scalar token is the *only* thing telling them apart: this is the
		// fixture that collides outright if ResolvedProperty stops rendering it.
		fixture: "union_third_branch_mismatch.cypher",
		failing: "property:INT (not null)",
		branch0: "property:STRING (not null)",
	},
	{
		fixture: "union_list_element_mismatch.cypher",
		failing: "list of edge Person-[AUTHORED]->Post (not null)",
		branch0: "list of edge Person-[KNOWS]->Person (not null)",
	},
}

// unionTypeArmMessage matches ErrUnionColumnMismatch's type arm and captures the
// two renderings it names. The frame is built from unionColumnTypeArm, the same
// constant resolve.go splices into the format string, so the match cannot drift
// from the message.
var unionTypeArmMessage = regexp.MustCompile(
	`column "[^"]*"` + regexp.QuoteMeta(unionColumnTypeArm) + `(.+?) in branch \d+ but (.+) in branch 0`)

// TestUnionColumnMismatchNamesEachArm holds ErrUnionColumnMismatch's type arm
// to naming what each branch projected, the failing branch first.
//
// The renderings the message is built from are ResolvedType's Stringers, and
// those are wire tags: every ResolvedNode is "node" whichever type it holds,
// every ResolvedEdgeUnion is "edgeUnion" whichever keys it committed. So on
// each row — one per resolvedTypeEqual arm that can fail on two values sharing
// a tag — the message read "has type edgeUnion; branch 0 has type edgeUnion",
// which tells the author the arms disagree and nothing about how.
//
// Two things are asserted per row, and they fail for different reasons. The
// transcription check says the message names exactly the text this table
// predicts, in the arm order the other two arms use. The collision check reads
// the two renderings back *out of the message* and requires them to differ
// there — so a renderer that makes two mismatching types print alike fails on
// the defect itself, not merely on a hand-written string going stale.
//
// Nothing here reads a golden, so -update cannot bless a regression.
func (s *ResolverSuite) TestUnionColumnMismatchNamesEachArm() {
	mapping := s.loadMapping("invalid")
	for _, tt := range unionTypeArmRows {
		s.Run(tt.fixture, func() {
			schemaName, ok := mapping[tt.fixture]
			s.Require().True(ok, "unmapped invalid fixture %q", tt.fixture)

			_, err := New(s.loadSchema("invalid", schemaName), WithRegistry(regR7)).
				Resolve(s.loadQuery(filepath.Join(fixtureDir, "invalid", tt.fixture)))
			s.Require().ErrorIs(err, ErrUnionColumnMismatch)
			msg := err.Error()

			s.Require().NotEqual(tt.failing, tt.branch0,
				"the two renderings must differ, or this row asserts nothing")
			failing := strings.Index(msg, tt.failing)
			branch0 := strings.Index(msg, tt.branch0)
			s.Require().GreaterOrEqualf(failing, 0, "the failing branch projected %q, absent from %q", tt.failing, msg)
			s.Require().GreaterOrEqualf(branch0, 0, "branch 0 projected %q, absent from %q", tt.branch0, msg)
			s.Require().Lessf(failing, branch0,
				"the count and name arms lead with the failing branch, so this one must too, in %q", msg)

			// The collision check: the two renderings as the message actually
			// carries them, not as this table transcribes them.
			m := unionTypeArmMessage.FindStringSubmatch(msg)
			s.Require().Lenf(m, 3, "the type arm's message must still parse: %q", msg)
			s.Require().NotEqualf(m[1], m[2],
				"the two branches rendered as the same text, so the message says they disagree and nothing about how: %q", msg)
		})
	}
}

// TestUnionColumnMismatchTypeArmRowsCoverTheArm holds unionTypeArmRows total
// against the corpus: every invalid fixture that reaches ErrUnionColumnMismatch's
// type arm has a row, and every row names a fixture that still reaches it.
//
// The table is hand-maintained, which is what makes it worth the rows — a
// derived expectation would pass whatever the renderer emitted. But a
// hand-maintained table silently falls behind the corpus, and it had: two of the
// nine fixtures that reach this arm had no row, including the one that collides
// outright when ResolvedProperty stops rendering its scalar token. A fixture can
// start reaching this arm without anyone editing this file — adding a UNION
// fixture for some other sentinel is enough — so totality is asserted rather
// than remembered.
//
// The reached set is derived by resolving every invalid fixture and matching the
// type arm's own message frame; nothing here is a count. A count is satisfiable
// by the wrong nine, and this repo has been bitten by exactly that (declaredTypeCount,
// TestStageSpecsReadAsHistory's NotZero). Set equality also makes its own
// vacuity guard: if the sweep matched nothing, every row would be reported extra.
func (s *ResolverSuite) TestUnionColumnMismatchTypeArmRowsCoverTheArm() {
	files, err := filepath.Glob(filepath.Join(fixtureDir, "invalid", "*.cypher"))
	s.Require().NoError(err)
	s.Require().NotEmpty(files)
	mapping := s.loadMapping("invalid")

	reaches := make(map[string]bool, len(files))
	for _, path := range files {
		name := filepath.Base(path)
		schemaName, ok := mapping[name]
		s.Require().True(ok, "unmapped invalid fixture %q", name)

		_, err := New(s.loadSchema("invalid", schemaName), WithRegistry(regR7)).
			Resolve(s.loadQuery(path))
		if err == nil || !errors.Is(err, ErrUnionColumnMismatch) {
			continue
		}
		if unionTypeArmMessage.MatchString(err.Error()) {
			reaches[name] = true
		}
	}

	tabled := make(map[string]bool, len(unionTypeArmRows))
	for _, row := range unionTypeArmRows {
		s.Require().Falsef(tabled[row.fixture], "duplicate row for %q", row.fixture)
		tabled[row.fixture] = true
	}

	var missing, stale []string
	for name := range reaches {
		if !tabled[name] {
			missing = append(missing, name)
		}
	}
	for name := range tabled {
		if !reaches[name] {
			stale = append(stale, name)
		}
	}
	slices.Sort(missing)
	slices.Sort(stale)

	s.Require().Emptyf(missing,
		"these invalid fixtures reach ErrUnionColumnMismatch's type arm with no row in unionTypeArmRows, so nothing pins what the message says about them: %v", missing)
	s.Require().Emptyf(stale,
		"these unionTypeArmRows rows name a fixture that no longer reaches the type arm, so the row asserts nothing: %v", stale)
}

// TestSentinelReachability is the bidirectional sweep: every allSentinels
// member must have at least one invalid fixture; every mapped sentinel must
// be in allSentinels.
func TestSentinelReachability(t *testing.T) {
	covered := make(map[error]bool)
	for _, sentinel := range invalidFixtures {
		if sentinel != nil {
			covered[sentinel] = true
		}
	}
	canonical := make(map[error]bool, len(allSentinels))
	for _, sentinel := range allSentinels {
		canonical[sentinel] = true
	}
	for _, sentinel := range allSentinels {
		require.True(t, covered[sentinel], "sentinel %q has no negative fixture", sentinel)
	}
	for sentinel := range covered {
		require.True(t, canonical[sentinel], "fixture maps to non-canonical sentinel %q", sentinel)
	}
}
