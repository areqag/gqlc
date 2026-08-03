package conformance_test

import (
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/areqag/gqlc/internal/cli/backends"
	"github.com/areqag/gqlc/internal/codegen"
	"github.com/areqag/gqlc/internal/graph"
	"github.com/areqag/gqlc/internal/resolver"
	"github.com/areqag/gqlc/internal/schema"
)

// AssembledInputSuite covers the §2 rows whose refused construct has no
// on-disk form: a codegen.Input a consumer of the package assembles and
// hands to Generate directly, carrying a value no .gql schema, no
// .cypher query and no CLI option produces.
//
// These rows sat in §3 until gqlc-h4ug, most of them argued as "the
// resolver would never build this". That argument is about the pipeline,
// and §5.1's criterion is about the contract: Input, NamedQuery,
// ValidatedQuery, Column and every resolver.Resolved* variant are
// exported structs with exported fields, so what the resolver builds does
// not bound what a caller can hand over. The last two rows to move were
// argued the same way one level down — a switch over resolver.ResolvedType
// is not total because the interface is sealed, since the seal is against
// new implementations and not against the pointer forms of the eight it
// has. Each case below is one such hand-off, and every one fired the
// first time it was written.
//
// The suite is the coverage §5 step 3 asks for on those rows, and it is
// half of what TestSentinelTaxonomy measures: it links internal/codegen
// from outside the package, so the fence's corpus sweep counts it. Drop
// a case and the fail-site it reaches goes uncovered, which fails
// TestReachableBranchesAreReached by name.
type AssembledInputSuite struct {
	suite.Suite

	backends codegen.Registry
}

func TestAssembledInputSuite(t *testing.T) {
	suite.Run(t, new(AssembledInputSuite))
}

func (s *AssembledInputSuite) SetupSuite() {
	reg, err := backends.Registry()
	s.Require().NoError(err)
	s.backends = reg
}

// assembledTarget is the backend these cases generate through. neo4j is
// the one that hands its Input to codegen.Prepare unfiltered — the AGE
// backend pre-gates with rejectUnservedQueries before Prepare runs, so
// several of these constructs would be answered by that gate instead and
// the case would measure the wrong refusal.
const assembledTarget = "neo4j-go-v5"

// temporalScanLimit bounds firstUndeclaredTemporal's scan. An enum that
// outgrows it is one the list-elem-temporal case wants rewriting for, and
// TestTemporalScanFindsTheEnumEnd is where that shows.
const temporalScanLimit = 64

// firstUndeclaredTemporal is the lowest resolver.Temporal past the end of
// the declared enum, derived from the only membership signal the type
// exports: Temporal is an int with no count and no iteration, so its
// Stringer's default arm — which answers for every value the constant
// block does not name — is what marks the end. The scan starts at 1
// because TemporalDate is the zero value and shares that arm.
//
// Derived rather than mirrored. A written-out list of the six kinds read
// through len() yields the same 6 and cannot disagree with the enum: add
// a seventh kind upstream and the list still says six, so the case below
// would name a declared kind and quietly stop testing what it claims to.
func firstUndeclaredTemporal() resolver.Temporal {
	undeclared := resolver.Temporal(-1).String() // no iota member is negative
	for k := resolver.Temporal(1); k < temporalScanLimit; k++ {
		if k.String() == undeclared {
			return k
		}
	}
	return -1
}

// probeSchema is the smallest schema that indexes one node type and one
// edge type, so a case can name a type the schema does not declare
// without the entity index being empty for an unrelated reason.
func probeSchema() schema.Schema {
	ek := schema.EdgeKey{Source: "Person", KeyLabels: "KNOWS", Target: "Person"}
	return schema.Schema{
		Name:  "Probe",
		Nodes: map[graph.LabelSetKey]schema.NodeType{"Person": {KeyLabels: "Person", CompleteLabels: "Person"}},
		Edges: map[schema.EdgeKey]schema.EdgeType{ek: {EdgeKey: ek, CompleteLabels: "KNOWS"}},
	}
}

// ghostEdge is an edge key probeSchema does not declare.
func ghostEdge() schema.EdgeKey {
	return schema.EdgeKey{Source: "Ghost", KeyLabels: "GHOSTED", Target: "Ghost"}
}

// probeQuery wraps one column in an otherwise admissible query, so the
// column's own resolved type is the only axis that can refuse.
func probeQuery(col resolver.Column) codegen.NamedQuery {
	return codegen.NamedQuery{
		Name:        "Fetch",
		Cardinality: codegen.CardinalityMany,
		SourceFile:  "probe.cypher",
		SourceText:  "MATCH (n) RETURN n",
		Validated:   resolver.ValidatedQuery{Columns: []resolver.Column{col}},
	}
}

// probeParamQuery wraps one parameter beside one admissible column, so
// the parameter is the only axis that can refuse. A column is required:
// a :many query with none is refused by the cardinality-shape gate,
// which runs first.
func probeParamQuery(param resolver.ResolvedParameter) codegen.NamedQuery {
	q := probeQuery(resolver.Column{Name: "n", Type: resolver.ResolvedNode{Labels: "Person"}})
	q.Validated.Parameters = []resolver.ResolvedParameter{param}
	return q
}

func (s *AssembledInputSuite) TestAssembledInput() {
	cases := []struct {
		// name is the §3-style fail-site name the case reaches. It is
		// not a tag any more — these sites carry no //gqlc:unreachable —
		// but naming the site keeps a failing subtest pointing at one
		// return statement rather than at one sentinel shared by eight.
		name string
		in   codegen.Input
		is   error
		msg  string
		why  string
	}{
		{
			name: "entity-empty-node-labels",
			why:  "Schema.Nodes keyed by the empty LabelSetKey. schema/gql refuses one with ErrUnnamedNodeType, so no parse carries it.",
			in: codegen.Input{Schema: schema.Schema{
				Name:  "Probe",
				Nodes: map[graph.LabelSetKey]schema.NodeType{"": {}},
			}},
			is:  codegen.ErrUnnamedMultiLabelType,
			msg: "unnamed multi-label type: node type with empty label set requires an explicit Name",
		},
		{
			name: "entity-multi-label-edge",
			why:  "An EdgeKey whose KeyLabels is a conjunction. Cypher has no conjunction syntax for edge labels and schema/gql refuses the key with ErrMultiLabelEdgeType.",
			in: func() codegen.Input {
				sc := probeSchema()
				ek := schema.EdgeKey{Source: "Person", KeyLabels: "Aye&Bee", Target: "Person"}
				sc.Edges = map[schema.EdgeKey]schema.EdgeType{ek: {EdgeKey: ek, CompleteLabels: "Aye&Bee"}}
				return codegen.Input{Schema: sc}
			}(),
			is:  codegen.ErrUnnamedMultiLabelType,
			msg: "unnamed multi-label type: multi-label edge type (Person -[:Aye&Bee]-> Person) requires an explicit Name",
		},
		{
			name: "entity-empty-edge-label",
			why:  "An EdgeKey whose KeyLabels is empty; schema/gql refuses one with ErrUnnamedEdgeType.",
			in: func() codegen.Input {
				sc := probeSchema()
				ek := schema.EdgeKey{Source: "Person", KeyLabels: "", Target: "Person"}
				sc.Edges = map[schema.EdgeKey]schema.EdgeType{ek: {EdgeKey: ek}}
				return codegen.Input{Schema: sc}
			}(),
			is:  codegen.ErrUnnamedMultiLabelType,
			msg: "unnamed multi-label type: edge type with empty label requires an explicit Name",
		},
		{
			name: "cardinality-not-in-set",
			why:  "A Cardinality outside its own constant set. queryfile.parseCardinality yields the three members or refuses the annotation, so no parse produces a fourth.",
			in: codegen.Input{
				Schema: probeSchema(),
				Queries: []codegen.NamedQuery{{
					Name:        "Fetch",
					Cardinality: codegen.Cardinality(7),
					SourceFile:  "probe.cypher",
					SourceText:  "MATCH (n) RETURN n",
				}},
			},
			is:  codegen.ErrInvalidCardinality,
			msg: `invalid cardinality: query "Fetch" at position 0 has unrecognised cardinality 7`,
		},
		{
			name: "column-width",
			why:  "A column carrying a width no schema property declares. Phase Z walks the schema, so a column backed by a declared property loses there first.",
			in: codegen.Input{
				Schema:  probeSchema(),
				Queries: []codegen.NamedQuery{probeQuery(resolver.Column{Name: "x", Type: resolver.ResolvedProperty{Type: graph.TypeInt128}})},
			},
			is:  codegen.ErrUnrepresentableWidth,
			msg: `unrepresentable property width: query "Fetch" column 0 "x" has INT128`,
		},
		{
			name: "column-unknown-node",
			why:  "A ResolvedNode naming labels the schema does not declare. The resolver resolves against the same schema Phase Z indexed.",
			in: codegen.Input{
				Schema:  probeSchema(),
				Queries: []codegen.NamedQuery{probeQuery(resolver.Column{Name: "n", Type: resolver.ResolvedNode{Labels: "Ghost"}})},
			},
			is:  codegen.ErrOutOfC6Scope,
			msg: `out of C6 scope: query "Fetch" column 0 "n" references unknown node type "Ghost"`,
		},
		{
			name: "column-unknown-edge",
			why:  "A ResolvedEdge naming an edge key the schema does not declare, on the same argument.",
			in: codegen.Input{
				Schema:  probeSchema(),
				Queries: []codegen.NamedQuery{probeQuery(resolver.Column{Name: "e", Type: resolver.ResolvedEdge{EdgeKey: ghostEdge()}})},
			},
			is:  codegen.ErrOutOfC6Scope,
			msg: `out of C6 scope: query "Fetch" column 0 "e" references unknown edge type Ghost -[:GHOSTED]-> Ghost`,
		},
		{
			name: "column-unknown-variant",
			why:  "The pointer form of a variant. resolver.ResolvedType is sealed against new implementations by an unexported marker method, but every variant declares that marker and String with value receivers, so *ResolvedNode satisfies the interface — while `case resolver.ResolvedNode:` does not match it. The same labels in their value form are admitted.",
			in: codegen.Input{
				Schema:  probeSchema(),
				Queries: []codegen.NamedQuery{probeQuery(resolver.Column{Name: "n", Type: &resolver.ResolvedNode{Labels: "Person"}})},
			},
			is:  codegen.ErrOutOfC6Scope,
			msg: `out of C6 scope: query "Fetch" column 0 "n" resolved as node`,
		},
		{
			name: "param-width",
			why:  "A parameter carrying a width no schema property declares. The resolver draws a parameter's ResolvedProperty from a schema property or from callProjectionType, and both yield widths Phase Z has passed.",
			in: codegen.Input{
				Schema: probeSchema(),
				Queries: []codegen.NamedQuery{probeParamQuery(resolver.ResolvedParameter{
					Name: "p",
					Type: resolver.ResolvedProperty{Type: graph.TypeInt128},
				})},
			},
			is:  codegen.ErrUnrepresentableWidth,
			msg: `unrepresentable property width: query "Fetch" parameter 0 $p has INT128`,
		},
		{
			name: "edge-union-arity",
			why:  "A ResolvedEdgeUnion with one candidate. The resolver collapses a lone candidate to ResolvedEdge (R3 spec §4.4).",
			in: codegen.Input{
				Schema: probeSchema(),
				Queries: []codegen.NamedQuery{probeQuery(resolver.Column{
					Name: "e",
					Type: resolver.ResolvedEdgeUnion{EdgeKeys: []schema.EdgeKey{{Source: "Person", KeyLabels: "KNOWS", Target: "Person"}}},
				})},
			},
			is:  codegen.ErrOutOfC6Scope,
			msg: `out of C6 scope: query "Fetch" column 0 "e" resolved as edgeUnion with only 1 candidate(s) — resolver invariant violated (expected >= 2)`,
		},
		{
			name: "edge-union-undeclared",
			why:  "A ResolvedEdgeUnion naming a candidate the schema does not declare. The resolver commits only declared edges.",
			in: codegen.Input{
				Schema: probeSchema(),
				Queries: []codegen.NamedQuery{probeQuery(resolver.Column{
					Name: "e",
					Type: resolver.ResolvedEdgeUnion{EdgeKeys: []schema.EdgeKey{
						{Source: "Person", KeyLabels: "KNOWS", Target: "Person"},
						ghostEdge(),
					}},
				})},
			},
			is:  codegen.ErrOutOfC6Scope,
			msg: `out of C6 scope: query "Fetch" column 0 "e" edgeUnion candidate Ghost -[:GHOSTED]-> Ghost not declared by schema`,
		},
		{
			name: "list-elem-width",
			why:  "A ResolvedList over a ResolvedProperty, a shape resolveType has no arm for. The one ResolvedProperty element that a schema produces is the one Phase B splits off a LIST property, whose width Phase Z has passed.",
			in: codegen.Input{
				Schema: probeSchema(),
				Queries: []codegen.NamedQuery{probeQuery(resolver.Column{
					Name: "xs",
					Type: resolver.ResolvedList{Element: resolver.ResolvedProperty{Type: graph.TypeInt128}},
				})},
			},
			is:  codegen.ErrUnrepresentableWidth,
			msg: `query "Fetch" column 0 "xs": unrepresentable property width: list element has unrepresentable property width INT128`,
		},
		{
			name: "list-elem-temporal",
			why:  "A resolver.Temporal outside its own six-member constant set, the same shape as the out-of-set Cardinality above. The query path is closed for a different reason: only AGE refuses a kind, and rejectUnservedQueries drops a list column before Prepare runs.",
			in: codegen.Input{
				Schema: probeSchema(),
				Queries: []codegen.NamedQuery{probeQuery(resolver.Column{
					Name: "xs",
					Type: resolver.ResolvedList{Element: resolver.ResolvedTemporal{Kind: firstUndeclaredTemporal()}},
				})},
			},
			is: codegen.ErrUnrepresentableTemporal,
			// resolver.Temporal.String defaults to "date" off the end of
			// the enum, so the message names a kind the value is not. That
			// is the resolver's Stringer, not this package's, and pinning
			// it here means a fix there is visible rather than silent.
			msg: `query "Fetch" column 0 "xs": unrepresentable temporal kind: list element projects temporal(date)`,
		},
		{
			name: "list-elem-unknown-node",
			why:  "A list element naming a node type the schema does not declare.",
			in: codegen.Input{
				Schema: probeSchema(),
				Queries: []codegen.NamedQuery{probeQuery(resolver.Column{
					Name: "xs",
					Type: resolver.ResolvedList{Element: resolver.ResolvedNode{Labels: "Ghost"}},
				})},
			},
			is:  codegen.ErrOutOfC6Scope,
			msg: `query "Fetch" column 0 "xs": out of C6 scope: list element references unknown node type "Ghost"`,
		},
		{
			name: "list-elem-unknown-edge",
			why:  "A list element naming an edge type the schema does not declare.",
			in: codegen.Input{
				Schema: probeSchema(),
				Queries: []codegen.NamedQuery{probeQuery(resolver.Column{
					Name: "xs",
					Type: resolver.ResolvedList{Element: resolver.ResolvedEdge{EdgeKey: ghostEdge()}},
				})},
			},
			is:  codegen.ErrOutOfC6Scope,
			msg: `query "Fetch" column 0 "xs": out of C6 scope: list element references unknown edge type Ghost -[:GHOSTED]-> Ghost`,
		},
		{
			name: "list-elem-unknown-variant",
			why:  "The same pointer form one level down: buildListElemPlan's switch names the same eight value forms, so the element falls past every arm.",
			in: codegen.Input{
				Schema: probeSchema(),
				Queries: []codegen.NamedQuery{probeQuery(resolver.Column{
					Name: "xs",
					Type: resolver.ResolvedList{Element: &resolver.ResolvedNode{Labels: "Person"}},
				})},
			},
			is:  codegen.ErrOutOfC6Scope,
			msg: `query "Fetch" column 0 "xs": out of C6 scope: list element has unknown resolved type node`,
		},
	}

	newGen, ok := s.backends.Lookup(assembledTarget)
	s.Require().True(ok, "no backend registered under %q", assembledTarget)

	for _, tc := range cases {
		s.Run(tc.name, func() {
			files, err := newGen("").Generate(tc.in)
			s.Nil(files, "a refused Input must emit nothing")
			s.Require().Error(err, "reaching input: %s", tc.why)
			s.Require().ErrorIs(err, tc.is)
			// Change-detection on the user-facing text, and not what
			// tells these cases apart: eight of them share
			// ErrOutOfC6Scope, and the discriminator is
			// TestReachableBranchesAreReached's line-level measurement,
			// which names a distinct prepare.go line per case and
			// reddens when one goes dark. What this pins is the wording,
			// which is contract — list-elem-temporal's message names a
			// kind the value is not, and that is the resolver's
			// off-the-end Stringer showing through rather than a typo.
			s.EqualError(err, tc.msg)
		})
	}
}

// TestTemporalScanFindsTheEnumEnd holds firstUndeclaredTemporal's scan
// against the constant block's last member. The two read the enum by
// different routes — the scan through Temporal.String's default arm, this
// assertion through the name of the last declared kind — so they can
// disagree, which is the point a list read through len() cannot serve. A
// seventh kind added upstream moves the scan and not this line, and
// list-elem-temporal learns here that the value it used to name is now a
// kind the resolver declares.
func (s *AssembledInputSuite) TestTemporalScanFindsTheEnumEnd() {
	s.Equal(resolver.TemporalDuration+1, firstUndeclaredTemporal(),
		"the scan and resolver.Temporal's constant block disagree about where the enum ends; if a kind was added, name it here and check that list-elem-temporal still reaches its fail-site")
}

// TestAgeGateAnswersBeforePrepare pins the claim §2's list-elem-temporal
// row rests on: what closes the *query* path to that fail-site is the
// Apache AGE backend's own pre-gate, not a phase inside codegen.
//
// The row said Phase Z for three stages and Phase Z was never involved —
// it walks the widths of schema properties, and the element of a
// collect(...) column comes from an expression, so a schema declaring no
// LIST property gives Phase Z nothing to refuse. The gate that does
// answer is age.rejectUnservedQueries, which generate calls before
// codegen.Prepare. Widen it — let unservedColumn serve ResolvedList —
// and the temporal walk refuses instead, which is what this test would
// then report.
//
// Asserted on the message rather than on age.ErrUnsupportedQuery: this
// package sits above the backends and resolves them through the composed
// registry, so importing one would break the rule that keeps it there.
// The message names the gate unambiguously; no phase inside codegen
// produces that wording.
func (s *AssembledInputSuite) TestAgeGateAnswersBeforePrepare() {
	const ageTarget = "apache-age-pgx-v5"
	newGen, ok := s.backends.Lookup(ageTarget)
	s.Require().True(ok, "no backend registered under %q", ageTarget)

	// A collect(date()) column: a list whose element is a declared
	// temporal kind. AGE has no carrier for that kind, so if the gate
	// stopped dropping list columns the element walk would refuse it.
	in := codegen.Input{
		Schema: probeSchema(),
		Queries: []codegen.NamedQuery{probeQuery(resolver.Column{
			Name: "xs",
			Type: resolver.ResolvedList{Element: resolver.ResolvedTemporal{Kind: resolver.TemporalDate}},
		})},
	}

	files, err := newGen("").Generate(in)
	s.Nil(files)
	s.Require().Error(err)
	s.EqualError(err, `unsupported query: the Apache AGE backend serves scalar and entity columns, `+
		`so 1 query would be dropped: Fetch (column "xs" projects list)`)
}
