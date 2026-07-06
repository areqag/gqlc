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
	"unknown_property.cypher":                              ErrUnknownProperty,
	"unknown_edge.cypher":                                  ErrUnknownEdge,
	"unknown_edge_property.cypher":                         ErrUnknownProperty,
	"ambiguous_unlabelled_binding.cypher":                  ErrAmbiguousBinding,
	"unlabelled_binding_no_edge.cypher":                    ErrUnknownLabel,
	"empty_inline_endpoint.cypher":                         ErrUnknownLabel,
	"parameter_type_conflict_two_properties.cypher":        ErrParameterTypeConflict,
	"parameter_type_conflict_clause_slot_vs_string.cypher": ErrParameterTypeConflict,
	"parameter_type_conflict_property_vs_expr_bool.cypher": ErrParameterTypeConflict,
	"parameter_type_conflict_nullability.cypher":           ErrParameterTypeConflict,
	"unknown_property_via_expr_use.cypher":                 ErrUnknownProperty,
	"list_of_nodes_projection.cypher":                      ErrOutOfR0Scope,
	"list_of_edges_projection.cypher":                      ErrOutOfR0Scope,
	"ambiguous_edge_orientation.cypher":                    ErrAmbiguousEdgeOrientation,
	"unknown_edge_undirected.cypher":                       ErrUnknownEdge,
	"unknown_edge_multi_type_all_miss.cypher":              ErrUnknownEdge,
	"unknown_property_union_missing.cypher":                ErrUnknownProperty,
	"unknown_property_union_type_differs.cypher":           ErrUnknownProperty,
	"untyped_edge.cypher":                                  ErrOutOfR0Scope,
	"var_length_edge_property_projection.cypher":           ErrOutOfR0Scope,
	// R5 additions:
	"union_column_count_mismatch.cypher":       ErrUnionColumnMismatch,
	"union_column_name_mismatch.cypher":        ErrUnionColumnMismatch,
	"union_column_type_mismatch.cypher":        ErrUnionColumnMismatch,
	"union_column_nullability_mismatch.cypher": ErrUnionColumnMismatch,
	"union_unknown_label_branch.cypher":        ErrUnknownLabel,
	"part_binding_type_conflict.cypher":        ErrPartBindingTypeConflict,
	"part_binding_type_conflict_edge.cypher":   ErrPartBindingTypeConflict,
	// R6 additions:
	"create_unknown_label.cypher":                 ErrUnknownLabel,
	"create_unknown_edge.cypher":                  ErrUnknownEdge,
	"merge_endpoint_unknown_label.cypher":         ErrUnknownLabel,
	"merge_unknown_edge.cypher":                   ErrUnknownEdge,
	"merge_on_match_unknown_property.cypher":      ErrUnknownProperty,
	"set_property_unknown_property.cypher":        ErrUnknownProperty,
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
	// R7 additions:
	"call_yield_property_lookup.cypher":              ErrUnknownProperty,
	"part_binding_type_conflict_call_vs_node.cypher": ErrPartBindingTypeConflict,
	"part_binding_type_conflict_call_vs_edge.cypher": ErrPartBindingTypeConflict,
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
