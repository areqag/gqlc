package conformance_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
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
// argued the same way one level down — a switch over
// resolver.ResolvedType is total because the interface is sealed. It is
// not sealed: the unexported marker stops another package writing an
// implementation from scratch, but Go promotes an embedded type's
// unexported methods, so struct{ resolver.ResolvedNode } satisfies it
// from here and matches no case arm. The pointer forms below are the
// cheapest witness of the same opening. Each case is one such hand-off,
// and every one fired the first time it was written.
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

// resolverSource declares resolver.Temporal. This suite reads it off disk
// because the fact it needs is the one compilation erases: a Go constant
// has no run-time footprint, so a kind the const block names and
// Temporal.String has no arm for is, to anything running, identical to a
// value nobody ever declared.
//
// That is not a hypothetical. The derivation this replaces scanned for
// the first value String answered with its default arm and called it the
// end of the enum, which is the same thing only while every declared kind
// has an arm. A seventh constant added with no arm left the scan
// answering 6, TestTemporalScanFindsTheEnumEnd comparing 6 to 6, and the
// list-elem-temporal case below feeding a kind the resolver declares
// while its `why` still said otherwise — the whole repo green. Behaviour
// cannot see a missing switch arm, and a missing switch arm is the
// commonest Go enum bug there is.
const resolverSource = "../../resolver/validated.go"

// temporalKinds is resolver.Temporal's constant block as this suite
// believes it stands, in declaration order so an index is a value.
//
// Written out, and that is the point: declaredTemporals derives the same
// list from the source, the two are held equal, and they can disagree.
// The list this replaces was read through len() and could not — six was
// six whatever the enum said. A member added, removed or inserted in the
// middle moves the derivation and not this, and the failure names which.
var temporalKinds = []string{
	"TemporalDate",
	"TemporalTime",
	"TemporalLocalTime",
	"TemporalDateTime",
	"TemporalLocalDateTime",
	"TemporalDuration",
}

// temporalScanLimit bounds the sweep of values past the end of the enum
// in TestTemporalStringerAnswersForDeclaredKindsAlone. Nothing derives
// from it; it is how far past the last member that test looks for an arm
// that should not be there.
const temporalScanLimit = 64

// temporalFallback is what resolver.Temporal.String renders a value its
// constant block does not name as. Asserted in both directions — no
// declared kind may render it, every undeclared one must — so this
// mirroring the resolver's format is what the assertions are for rather
// than something they rest on.
func temporalFallback(v int) string { return "Temporal(" + strconv.Itoa(v) + ")" }

// declaredTemporals returns the names resolver.Temporal's constant block
// declares, in declaration order, so a name's index is its value.
//
// The block's shape is checked rather than assumed. It is a bare iota run
// today, which is what makes position and value the same number; a member
// with its own value, a skipped `_`, or a second name on a line would
// part the two and the count below would silently mean something else.
// Each of those fails here instead.
func (s *AssembledInputSuite) declaredTemporals() []string {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, resolverSource, nil, 0)
	s.Require().NoError(err,
		"cannot parse %s, where resolver.Temporal's constant block lives; this suite derives the enum's members from that block because compilation erases them",
		resolverSource)

	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST || len(gen.Specs) == 0 {
			continue
		}
		first, ok := gen.Specs[0].(*ast.ValueSpec)
		if !ok || !isIotaAnchor(first, "Temporal") {
			continue
		}
		names := make([]string, 0, len(gen.Specs))
		for i, spec := range gen.Specs {
			value, ok := spec.(*ast.ValueSpec)
			s.Require().True(ok, "%s: resolver.Temporal's const block holds a spec the derivation cannot read", resolverSource)
			s.Require().Len(value.Names, 1,
				"%s: member %d of resolver.Temporal's const block declares %d names on one line; the derivation reads a member's value off its position, which holds only one name at a time",
				resolverSource, i, len(value.Names))
			name := value.Names[0].Name
			s.Require().NotEqual("_", name,
				"%s: resolver.Temporal's const block skips a value with _, so position and value have parted company and the count below is no longer the first undeclared kind",
				resolverSource)
			if i > 0 {
				s.Require().Nil(value.Type,
					"%s: %s restates a type inside the iota run; the derivation cannot follow a block that re-anchors", resolverSource, name)
				s.Require().Empty(value.Values,
					"%s: %s carries an explicit value inside the iota run, so its position is no longer its value", resolverSource, name)
			}
			names = append(names, name)
		}
		return names
	}
	s.Require().Fail("resolver.Temporal's constant block not found",
		"%s declares no `<name> Temporal = iota` const block; this suite derives the enum's members from it, and a derivation that found nothing would report an empty enum",
		resolverSource)
	return nil
}

// isIotaAnchor reports whether spec is the `<name> <typeName> = iota`
// line that opens a const block.
func isIotaAnchor(spec *ast.ValueSpec, typeName string) bool {
	ident, ok := spec.Type.(*ast.Ident)
	if !ok || ident.Name != typeName || len(spec.Values) != 1 {
		return false
	}
	anchor, ok := spec.Values[0].(*ast.Ident)
	return ok && anchor.Name == "iota"
}

// firstUndeclaredTemporal is the lowest resolver.Temporal past the end of
// the declared enum. The block is a bare iota run, so the member count is
// the first value it does not name.
func (s *AssembledInputSuite) firstUndeclaredTemporal() resolver.Temporal {
	return resolver.Temporal(len(s.declaredTemporals()))
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
			why:  "The pointer form of a variant. Every variant declares the marker and String with value receivers, so *ResolvedNode satisfies resolver.ResolvedType while `case resolver.ResolvedNode:` does not match it. The same labels in their value form are admitted. The pointer is the cheapest witness rather than the only one: the unexported marker seals the interface against nothing, since an out-of-package struct{ resolver.ResolvedNode } promotes it and reaches this same arm.",
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
			why:  "A resolver.Temporal outside its own constant set, the same shape as the out-of-set Cardinality above. The kind is derived from that block rather than written down, so a member added upstream moves it and TestTemporalEnumIsTheOneThisSuiteKnows says so. The query path is closed for a different reason: only AGE refuses a kind, and rejectUnservedQueries drops a list column before Prepare runs.",
			in: codegen.Input{
				Schema: probeSchema(),
				Queries: []codegen.NamedQuery{probeQuery(resolver.Column{
					Name: "xs",
					Type: resolver.ResolvedList{Element: resolver.ResolvedTemporal{Kind: s.firstUndeclaredTemporal()}},
				})},
			},
			is: codegen.ErrUnrepresentableTemporal,
			// The kind renders as Temporal(6) because it names no member of
			// the constant block, which is the whole of what this case
			// claims. The two temporal tests below are what hold that
			// rendering to the block; here it is pinned as user-facing text.
			msg: `query "Fetch" column 0 "xs": unrepresentable temporal kind: list element projects temporal(Temporal(6))`,
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
			why:  "The same pointer form one level down: buildListElemPlan's switch names the same eight value forms, so the element falls past every arm — as does an embedded variant, on the same argument as the column case above.",
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
			// which is contract — down to how an undeclared temporal kind
			// renders, since list-elem-temporal's whole claim is that its
			// value names no member of the enum.
			s.EqualError(err, tc.msg)
		})
	}
}

// embeddedNode and embeddedProperty implement resolver.ResolvedType from
// outside internal/resolver by embedding a variant. Go promotes an
// embedded type's unexported methods, so isResolvedType() comes along
// with ResolvedNode and the interface is satisfied in one line — from
// this package, or from any package a consumer of gqlc writes.
//
// The compile-time assertions below are half the point. If they ever
// stop compiling, resolver.ResolvedType has become genuinely closed and
// §5.1 step 5 needs rewriting again, in the other direction.
type embeddedNode struct{ resolver.ResolvedNode }

type embeddedProperty struct{ resolver.ResolvedProperty }

var (
	_ resolver.ResolvedType = embeddedNode{}
	_ resolver.ResolvedType = embeddedProperty{}
)

// TestMarkerSealDoesNotCloseTheSum pins the fact §5.1 step 5 rests on and
// two rounds of review got wrong: an unexported marker method does not
// seal an interface whose implementations are exported.
//
// The taxonomy called the switches over resolver.ResolvedType total,
// counting eight variants. A review round found the pointer forms and
// made it sixteen. Both numbers were answers to the wrong question —
// there is no count, because an out-of-package caller reaches these arms
// with a struct literal and an embedded field, and can write as many as
// it likes. That is why §3 has no **Total** row and why step 5 tells a
// classifier not to count but to name the check that answers first.
//
// The three sites are the ones §2 and §3 argue over: Phase A's column
// switch, buildListElemPlan's element switch, and Phase A's parameter
// type assertion. Each answers with its own message, so a switch that
// grew an arm matching the interface itself — the shape of somebody
// deciding the sum is closed after all — reddens here by name.
func (s *AssembledInputSuite) TestMarkerSealDoesNotCloseTheSum() {
	newGen, ok := s.backends.Lookup(assembledTarget)
	s.Require().True(ok, "no backend registered under %q", assembledTarget)

	cases := []struct {
		name string
		col  resolver.Column
		par  *resolver.ResolvedParameter
		msg  string
	}{
		{
			name: "column-unknown-variant",
			col:  resolver.Column{Name: "n", Type: embeddedNode{resolver.ResolvedNode{Labels: "Person"}}},
			msg:  `out of C6 scope: query "Fetch" column 0 "n" resolved as node`,
		},
		{
			name: "list-elem-unknown-variant",
			col: resolver.Column{
				Name: "xs",
				Type: resolver.ResolvedList{Element: embeddedNode{resolver.ResolvedNode{Labels: "Person"}}},
			},
			msg: `query "Fetch" column 0 "xs": out of C6 scope: list element has unknown resolved type node`,
		},
		{
			name: "param-non-property",
			par:  &resolver.ResolvedParameter{Name: "p", Type: embeddedProperty{}},
			msg:  `out of C6 scope: query "Fetch" parameter 0 $p resolved as property: (non-property parameters are post-v1)`,
		},
	}

	for _, tc := range cases {
		s.Run(tc.name, func() {
			q := probeQuery(tc.col)
			if tc.par != nil {
				q = probeParamQuery(*tc.par)
			}
			files, err := newGen("").Generate(codegen.Input{Schema: probeSchema(), Queries: []codegen.NamedQuery{q}})
			s.Nil(files, "a refused Input must emit nothing")
			s.Require().Error(err, "an embedded variant satisfies resolver.ResolvedType and must reach this fail-site")
			s.Require().ErrorIs(err, codegen.ErrOutOfC6Scope)
			s.EqualError(err, tc.msg)
		})
	}
}

// TestTemporalEnumIsTheOneThisSuiteKnows holds resolver.Temporal's
// constant block against the members written out above. It is what makes
// list-elem-temporal's Kind an undeclared one: that case's value is the
// derived member count, so a kind added upstream moves it silently unless
// something notices the enum grew.
//
// Set equality on an ordered list rather than a count, and the ordering
// earns its keep. A seventh member appended and a member inserted in the
// middle are the same number and not the same enum — the second renumbers
// every kind after it, so every value this suite hands over means
// something else. A count cannot tell them apart and this names the
// member that moved.
func (s *AssembledInputSuite) TestTemporalEnumIsTheOneThisSuiteKnows() {
	s.Equal(temporalKinds, s.declaredTemporals(),
		"resolver.Temporal's constant block is no longer the enum this suite was written against; update temporalKinds, then check that list-elem-temporal still reaches its fail-site and that its expected message still names %q",
		temporalFallback(len(temporalKinds)))
}

// TestTemporalStringerAnswersForDeclaredKindsAlone holds Temporal.String's
// arms against the constant block, in both directions, and it is the half
// of this that behaviour can see.
//
// A declared kind that renders as the fallback has no arm — the seventh
// constant somebody adds and forgets to handle. An undeclared value that
// renders as anything else has an arm it should not — a case left behind
// by a retired kind, or one written for a constant never declared. Both
// used to be invisible: the default arm returned "date", a declared
// member's own tag, so the two populations were indistinguishable by
// construction and every value off the end of the enum claimed to be
// TemporalDate.
func (s *AssembledInputSuite) TestTemporalStringerAnswersForDeclaredKindsAlone() {
	declared := s.declaredTemporals()

	tagOf := make(map[string]string, len(declared))
	for v, name := range declared {
		tag := resolver.Temporal(v).String()
		s.NotEqual(temporalFallback(v), tag,
			"resolver.%s is declared but resolver.Temporal.String has no arm for it, so it renders as the form reserved for undeclared values; add the case",
			name)
		first, dup := tagOf[tag]
		s.False(dup, "resolver.%s and resolver.%s both render %q, so the wire tag no longer identifies the kind", first, name, tag)
		tagOf[tag] = name
	}

	for v := len(declared); v < temporalScanLimit; v++ {
		s.Equal(temporalFallback(v), resolver.Temporal(v).String(),
			"resolver.Temporal(%d) is past the end of the constant block but resolver.Temporal.String answers for it; either the constant is missing or the arm is stale, and either way a value nothing declares is rendering as though something does",
			v)
	}
	s.Equal(temporalFallback(-1), resolver.Temporal(-1).String(),
		"no member of an iota run is negative, so resolver.Temporal(-1) must reach the default arm")
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
