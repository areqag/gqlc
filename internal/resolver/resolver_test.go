package resolver

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"regexp"
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
// suite via mustBuildRegR7's panic-on-error posture (spec §6.3 design note).
var regR7 = mustBuildRegR7()

func mustBuildRegR7() procsig.Registry {
	reg, err := procsig.NewRegistry(signaturesR7)
	if err != nil {
		panic("resolver_test: R7 signatures failed to build registry: " + err.Error())
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
	// kph4 additions. Plural endpoints reach edge closure only on a schema
	// that both declares edges and declares more than one node type
	// satisfying the pattern's label expression; no fixture combined the two.
	//
	// Committing one edge type names the node type on each of its ends, but
	// the endpoints are not narrowed to it: WORKS_AT closes to the single key
	// Employee&Person-[WORKS_AT]->Company and `p` stays plural, so ADR 0022's
	// whole-entity refusal still fires on a binding the schema pins. Refusing
	// is the safe half of the answer; widening it is gqlc-0tft.
	"plural_endpoint_whole_entity_after_edge_closure.cypher": ErrAmbiguousLabel,
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

// TestUnionColumnMismatchNamesEachArm holds ErrUnionColumnMismatch's type arm
// to naming what each branch projected, branch 0 first.
//
// The renderings the message is built from are ResolvedType's Stringers, and
// those are wire tags: every ResolvedNode is "node" whichever type it holds,
// every ResolvedEdgeUnion is "edgeUnion" whichever keys it committed. So on
// each row below — one per resolvedTypeEqual arm that can fail on two values
// sharing a tag — the message read "has type edgeUnion; branch 0 has type
// edgeUnion", which tells the author the arms disagree and nothing about how.
//
// Each row hand-writes both renderings rather than deriving them from the
// resolved value, so the row fails if the renderer stops carrying the axis that
// separates the two branches. The assertions are the three claims: both
// projections appear, they are different text, and branch 0's comes first —
// the order the author wrote the arms in. Nothing here reads a golden, so
// -update cannot bless a regression.
func (s *ResolverSuite) TestUnionColumnMismatchNamesEachArm() {
	tests := []struct {
		fixture string
		first   string // what branch 0 projected
		second  string // what the branch that failed the comparison projected
	}{
		{
			fixture: "union_node_type_mismatch.cypher",
			first:   "node Person (not null)",
			second:  "node Post (not null)",
		},
		{
			fixture: "union_edge_union_keys_mismatch.cypher",
			first:   "edgeUnion {Person-[AUTHORED]->Post, Person-[LIKES]->Post} (not null)",
			second:  "edgeUnion {Person-[AUTHORED]->Note, Person-[LIKES]->Note} (not null)",
		},
		{
			// Same keys on both sides: nullability is the only axis that
			// separates them, so a rendering that drops it re-collapses the two.
			fixture: "union_edge_union_nullability_mismatch.cypher",
			first:   "edgeUnion {Person-[AUTHORED]->Post, Person-[LIKES]->Post} (not null)",
			second:  "edgeUnion {Person-[AUTHORED]->Post, Person-[LIKES]->Post} (nullable)",
		},
		{
			// One key list is a strict prefix of the other, so the delimiters
			// have to close the list: without them the shorter rendering is a
			// substring of the longer and the two are no longer told apart.
			fixture: "union_edge_union_arity_prefix.cypher",
			first:   "edgeUnion {Person-[AUTHORED]->Post, Person-[LIKES]->Post, Person-[SHARED]->Post} (not null)",
			second:  "edgeUnion {Person-[AUTHORED]->Post, Person-[LIKES]->Post} (not null)",
		},
		{
			fixture: "union_edge_union_arity_prefix_reversed.cypher",
			first:   "edgeUnion {Person-[AUTHORED]->Post, Person-[LIKES]->Post} (not null)",
			second:  "edgeUnion {Person-[AUTHORED]->Post, Person-[LIKES]->Post, Person-[SHARED]->Post} (not null)",
		},
		{
			fixture: "union_column_nullability_mismatch.cypher",
			first:   "property:STRING (not null)",
			second:  "property:STRING (nullable)",
		},
		{
			fixture: "union_list_element_mismatch.cypher",
			first:   "list of edge Person-[KNOWS]->Person (not null)",
			second:  "list of edge Person-[AUTHORED]->Post (not null)",
		},
	}

	mapping := s.loadMapping("invalid")
	for _, tt := range tests {
		s.Run(tt.fixture, func() {
			schemaName, ok := mapping[tt.fixture]
			s.Require().True(ok, "unmapped invalid fixture %q", tt.fixture)

			_, err := New(s.loadSchema("invalid", schemaName), WithRegistry(regR7)).
				Resolve(s.loadQuery(filepath.Join(fixtureDir, "invalid", tt.fixture)))
			s.Require().ErrorIs(err, ErrUnionColumnMismatch)
			msg := err.Error()

			s.Require().NotEqual(tt.first, tt.second,
				"the two renderings must differ, or this row asserts nothing")
			first := strings.Index(msg, tt.first)
			second := strings.Index(msg, tt.second)
			s.Require().GreaterOrEqualf(first, 0, "branch 0 projected %q, absent from %q", tt.first, msg)
			s.Require().GreaterOrEqualf(second, 0, "the failing branch projected %q, absent from %q", tt.second, msg)
			s.Require().Lessf(first, second,
				"branch 0 was written first, so its projection must be named first, in %q", msg)
		})
	}
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
