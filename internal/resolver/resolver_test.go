package resolver

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

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
