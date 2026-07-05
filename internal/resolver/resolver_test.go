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

	"github.com/areqag/gqlc/internal/query"
	"github.com/areqag/gqlc/internal/query/cypher"
	"github.com/areqag/gqlc/internal/schema"
	"github.com/areqag/gqlc/internal/schema/gql"
)

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
	"expr_use_set_value.cypher":                            ErrOutOfR0Scope,
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

// loadQuery parses a Cypher query fixture.
func (s *ResolverSuite) loadQuery(path string) query.Query {
	src, err := os.ReadFile(path)
	s.Require().NoError(err)
	q, err := cypher.New().Parse(bytes.NewReader(src))
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

			vq, err := New(sch).Resolve(q)
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

			vq, err := New(sch).Resolve(q)
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
