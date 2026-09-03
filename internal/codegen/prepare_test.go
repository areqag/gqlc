package codegen_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/codegen"
	"github.com/areqag/gqlc/internal/graph"
	"github.com/areqag/gqlc/internal/queryfile"
	"github.com/areqag/gqlc/internal/resolver"
	"github.com/areqag/gqlc/internal/schema"
)

// stubTypeMap is a TypeMap whose results echo their input, so a plan
// assertion pins the shape the phases commit without pinning any one
// backend's Go-type spelling. graph.TypeDecimal stands in for the
// unrepresentable arm — the phases only ever see the ok=false signal,
// never the width set behind it.
type stubTypeMap struct{}

func (stubTypeMap) Property(pt graph.PropertyType) (string, bool) {
	if pt == graph.TypeDecimal {
		return "", false
	}
	return "property:" + string(pt), true
}

// StorableProperty admits every width. The storage axis is deliberately
// total here so that the eight existing plan assertions keep measuring
// the carrier axis alone; the refusal lives in unstorablePropertyTypeMap,
// which names the width it refuses.
func (stubTypeMap) StorableProperty(graph.PropertyType) bool { return true }

func (stubTypeMap) Temporal(k resolver.Temporal) (string, bool) {
	return "temporal:" + k.String(), true
}

func (stubTypeMap) Scalar(k resolver.Scalar) string { return "scalar:" + k.String() }

// partialTemporalTypeMap refuses exactly one temporal kind and admits
// the other five. This is the shape a real backend takes: agtype has no
// temporal value, so its encodings are committed kind by kind, and the
// spike behind bd gqlc-35yu.11 found faithful ones for some kinds and
// none for a calendar duration — so a table that could only refuse the
// whole enum or admit the whole enum would have nowhere to put that
// answer. Wrapping stubTypeMap keeps the other two axes identical to
// every other plan assertion in this file.
type partialTemporalTypeMap struct {
	stubTypeMap
	refuse resolver.Temporal
}

func (m partialTemporalTypeMap) Temporal(k resolver.Temporal) (string, bool) {
	if k == m.refuse {
		return "", false
	}
	return m.stubTypeMap.Temporal(k)
}

// unstorablePropertyTypeMap refuses exactly one width on the STORAGE
// axis while stubTypeMap's carrier axis still admits it. That split is
// the point: neo4j has a faithful [][]int16 for a nested list and emits
// a working recursive decode for one as a query value, and it is the
// server that will not hold it as a stored property (ADR 0035). A stub
// that refused both axes at once could not tell the two sentinels apart.
type unstorablePropertyTypeMap struct {
	stubTypeMap
	refuse graph.PropertyType
}

func (m unstorablePropertyTypeMap) StorableProperty(pt graph.PropertyType) bool {
	return pt != m.refuse
}

// unknownVariant is a test-local ResolvedType stub satisfying the
// sealed interface by embedding a real ResolvedType (any variant) so
// isResolvedType() promotes through, but wrapping the whole in a
// distinct outer struct that matches no case in the builder's
// type-switch. Used by the mapping-table synthetic-variant row (spec
// §4.1, B6) to prove buildListElemPlan returns a sentinel on an
// unrecognised variant instead of silent success. The embedded value
// is inert — the builder never unwraps because its type-switch cases
// name concrete ResolvedType structs by name, and unknownVariant is
// not among them.
type unknownVariant struct {
	resolver.ResolvedType
}

// listPlanTestFixture builds a minimal schema for the mapping-table
// tests: one Person node (for ResolvedNode), one KNOWS edge
// (Person -[:KNOWS]-> Person, for ResolvedEdge and one edgeUnion
// candidate), and one LIKES edge (Person -[:LIKES]-> Person, second
// edgeUnion candidate). Runs Phase Z to derive the entity cache.
func listPlanTestFixture(t *testing.T) ([]codegen.Entity, map[codegen.EntityLookupKey]int) {
	t.Helper()
	person := graph.LabelSetKey("Person")
	knows := schema.EdgeKey{Source: person, KeyLabels: graph.LabelSetKey("KNOWS"), Target: person}
	likes := schema.EdgeKey{Source: person, KeyLabels: graph.LabelSetKey("LIKES"), Target: person}
	sch := schema.Schema{
		Name: "Test",
		Nodes: map[graph.LabelSetKey]schema.NodeType{
			person: {KeyLabels: person, CompleteLabels: person, Properties: map[string]schema.Property{}},
		},
		Edges: map[schema.EdgeKey]schema.EdgeType{
			knows: {EdgeKey: knows, Properties: map[string]schema.Property{}},
			likes: {EdgeKey: likes, Properties: map[string]schema.Property{}},
		},
	}
	entities, index, err := codegen.PhaseZAdmit(sch, stubTypeMap{})
	require.NoError(t, err)
	return entities, index
}

// listPlanPersonKey returns the Person node's entityLookupKey.
func listPlanPersonKey() codegen.EntityLookupKey {
	return codegen.EntityLookupKey{Kind: codegen.EntityNode, Labels: graph.LabelSetKey("Person")}
}

// listPlanKnowsKey / listPlanLikesKey mirror the two edgeUnion
// candidates.
func listPlanKnowsKey() codegen.EntityLookupKey {
	return codegen.EntityLookupKey{Kind: codegen.EntityEdge, EdgeKey: schema.EdgeKey{
		Source:    graph.LabelSetKey("Person"),
		KeyLabels: graph.LabelSetKey("KNOWS"),
		Target:    graph.LabelSetKey("Person"),
	}}
}

func listPlanLikesKey() codegen.EntityLookupKey {
	return codegen.EntityLookupKey{Kind: codegen.EntityEdge, EdgeKey: schema.EdgeKey{
		Source:    graph.LabelSetKey("Person"),
		KeyLabels: graph.LabelSetKey("LIKES"),
		Target:    graph.LabelSetKey("Person"),
	}}
}

// TestPhaseBCommitsListElemPlan is the mapping-table unit test the
// deepening acceptance criteria call for (spec §4.1, gqlc-ls8.3). 18
// positive rows exercise every arm of the ResolvedType sum: 1 property,
// 6 temporal kinds, 6 scalar kinds (with ScalarNull splitting off to
// ColumnScalarNull), 1 Unknown, 1 Node, 1 Edge, 1 EdgeUnion, 1
// nested-list. 2 negative rows exercise the failure fence: an
// unrepresentable width routing through ErrUnrepresentableWidth, plus a
// synthetic malformed-variant row asserting a foreign ResolvedType
// returns ErrOutOfC6Scope rather than silent success.
func TestPhaseBCommitsListElemPlan(t *testing.T) {
	entities, index := listPlanTestFixture(t)
	knowsKey := schema.EdgeKey{
		Source:    graph.LabelSetKey("Person"),
		KeyLabels: graph.LabelSetKey("KNOWS"),
		Target:    graph.LabelSetKey("Person"),
	}
	likesKey := schema.EdgeKey{
		Source:    graph.LabelSetKey("Person"),
		KeyLabels: graph.LabelSetKey("LIKES"),
		Target:    graph.LabelSetKey("Person"),
	}
	personName := entities[index[listPlanPersonKey()]].Name
	knowsName := entities[index[listPlanKnowsKey()]].Name
	_ = entities[index[listPlanLikesKey()]].Name // fixture presence check; EdgeUnion arm asserts only against the interface name

	tm := stubTypeMap{}

	type wantPlan struct {
		Kind       codegen.ColumnKind
		GoType     string
		EntityName string
		UnionIdx   int
		NestedKind codegen.ColumnKind // zero if Nested nil expected
		NestedGoTy string
	}

	temporalRows := []struct {
		name string
		k    resolver.Temporal
	}{
		{"temporal date", resolver.TemporalDate},
		{"temporal time", resolver.TemporalTime},
		{"temporal localtime", resolver.TemporalLocalTime},
		{"temporal datetime", resolver.TemporalDateTime},
		{"temporal localdatetime", resolver.TemporalLocalDateTime},
		{"temporal duration", resolver.TemporalDuration},
	}

	// 6 scalar kinds. ScalarNull splits to ColumnScalarNull, whose Go
	// type is the phase's own `any` rather than the table's answer.
	scalarRows := []struct {
		name string
		k    resolver.Scalar
		gt   string
		kind codegen.ColumnKind
	}{
		{"scalar bool", resolver.ScalarBool, tm.Scalar(resolver.ScalarBool), codegen.ColumnScalar},
		{"scalar int", resolver.ScalarInt, tm.Scalar(resolver.ScalarInt), codegen.ColumnScalar},
		{"scalar float", resolver.ScalarFloat, tm.Scalar(resolver.ScalarFloat), codegen.ColumnScalar},
		{"scalar string", resolver.ScalarString, tm.Scalar(resolver.ScalarString), codegen.ColumnScalar},
		{"scalar null", resolver.ScalarNull, "any", codegen.ColumnScalarNull},
		{"scalar map", resolver.ScalarMap, tm.Scalar(resolver.ScalarMap), codegen.ColumnScalar},
	}

	type positiveRow struct {
		name string
		in   resolver.ResolvedType
		want wantPlan
	}
	positive := []positiveRow{{
		name: "property",
		in:   resolver.ResolvedProperty{Type: graph.TypeInt32},
		want: wantPlan{Kind: codegen.ColumnProperty, GoType: "property:INT32"},
	}}
	for _, r := range temporalRows {
		goType, ok := tm.Temporal(r.k)
		require.True(t, ok, "the stub table carries every temporal kind")
		positive = append(positive, positiveRow{
			name: r.name,
			in:   resolver.ResolvedTemporal{Kind: r.k},
			want: wantPlan{Kind: codegen.ColumnTemporal, GoType: goType},
		})
	}
	for _, r := range scalarRows {
		positive = append(positive, positiveRow{
			name: r.name,
			in:   resolver.ResolvedScalar{Kind: r.k},
			want: wantPlan{Kind: r.kind, GoType: r.gt},
		})
	}
	positive = append(positive,
		positiveRow{
			name: "unknown", in: resolver.ResolvedUnknown{},
			want: wantPlan{Kind: codegen.ColumnAny, GoType: "any"},
		},
		positiveRow{
			name: "node", in: resolver.ResolvedNode{Labels: graph.LabelSetKey("Person")},
			want: wantPlan{Kind: codegen.ColumnNode, GoType: personName, EntityName: personName},
		},
		positiveRow{
			name: "edge", in: resolver.ResolvedEdge{EdgeKey: knowsKey},
			want: wantPlan{Kind: codegen.ColumnEdge, GoType: knowsName, EntityName: knowsName},
		},
		positiveRow{
			name: "edgeUnion", in: resolver.ResolvedEdgeUnion{EdgeKeys: []schema.EdgeKey{knowsKey, likesKey}},
			want: wantPlan{Kind: codegen.ColumnEdgeUnion, GoType: "PathActionsUnion", UnionIdx: 42},
		},
		positiveRow{
			name: "nested list of scalar",
			in:   resolver.ResolvedList{Element: resolver.ResolvedList{Element: resolver.ResolvedScalar{Kind: resolver.ScalarInt}}},
			want: wantPlan{
				Kind:       codegen.ColumnList,
				GoType:     "[][]" + tm.Scalar(resolver.ScalarInt),
				NestedKind: codegen.ColumnList,
				NestedGoTy: "[]" + tm.Scalar(resolver.ScalarInt),
			},
		},
	)

	require.Len(t, positive, 18, "positive table should cover every ResolvedType arm (§4.1)")

	for _, tt := range positive {
		t.Run("positive/"+tt.name, func(t *testing.T) {
			plan, err := codegen.BuildListElemPlan(tt.in, entities, index, tm, tt.want.UnionIdx, tt.want.GoType)
			require.NoError(t, err)
			require.NotNil(t, plan)
			require.Equal(t, tt.want.Kind, plan.Kind, "Kind")
			require.Equal(t, tt.want.GoType, plan.GoType, "GoType")
			require.Equal(t, tt.want.EntityName, plan.EntityName, "EntityName")
			if tt.want.Kind == codegen.ColumnEdgeUnion {
				require.Equal(t, tt.want.UnionIdx, plan.UnionIdx, "UnionIdx")
			}
			if tt.want.NestedKind != 0 || tt.want.NestedGoTy != "" {
				require.NotNil(t, plan.Nested, "Nested must be non-nil for a list arm")
				require.Equal(t, tt.want.NestedKind, plan.Nested.Kind, "Nested.Kind")
				require.Equal(t, tt.want.NestedGoTy, plan.Nested.GoType, "Nested.GoType")
			}
		})
	}

	t.Run("negative/unrepresentable_width", func(t *testing.T) {
		_, err := codegen.BuildListElemPlan(resolver.ResolvedProperty{Type: graph.TypeDecimal}, entities, index, tm, -1, "")
		require.ErrorIs(t, err, codegen.ErrUnrepresentableWidth)
	})

	t.Run("negative/synthetic_malformed_variant", func(t *testing.T) {
		// Wrap ResolvedUnknown so String() promotes through cleanly;
		// the builder's type-switch cases each match a concrete
		// ResolvedType struct by name, so the outer unknownVariant
		// matches none and falls to the default arm.
		_, err := codegen.BuildListElemPlan(unknownVariant{ResolvedType: resolver.ResolvedUnknown{}}, entities, index, tm, -1, "")
		require.ErrorIs(t, err, codegen.ErrOutOfC6Scope)
	})
}

// TestPreparedListElemMapsToColumnKind asserts every kind value the
// plan-builder can assign is one of the nine known ColumnKind values
// (spec §4.1 companion test). Explicit enumeration: if a tenth
// ColumnKind arrives without extending the plan-builder, this test
// fails.
func TestPreparedListElemMapsToColumnKind(t *testing.T) {
	// The nine ColumnKind values the plan-builder may assign to
	// ListElem.Kind (spec §1.3, §3). ColumnEdgeUnion,
	// ColumnScalarNull, ColumnAny are the arms whose emission-side
	// dispatch is single-value; every other arm shares its top-level
	// meaning.
	allowed := map[codegen.ColumnKind]string{
		codegen.ColumnProperty:   "ColumnProperty",
		codegen.ColumnNode:       "ColumnNode",
		codegen.ColumnEdge:       "ColumnEdge",
		codegen.ColumnTemporal:   "ColumnTemporal",
		codegen.ColumnScalar:     "ColumnScalar",
		codegen.ColumnScalarNull: "ColumnScalarNull",
		codegen.ColumnList:       "ColumnList",
		codegen.ColumnAny:        "ColumnAny",
		codegen.ColumnEdgeUnion:  "ColumnEdgeUnion",
	}
	require.Len(t, allowed, 9)

	// Sample every arm through the builder and check membership.
	entities, index := listPlanTestFixture(t)
	knowsKey := schema.EdgeKey{
		Source:    graph.LabelSetKey("Person"),
		KeyLabels: graph.LabelSetKey("KNOWS"),
		Target:    graph.LabelSetKey("Person"),
	}
	likesKey := schema.EdgeKey{
		Source:    graph.LabelSetKey("Person"),
		KeyLabels: graph.LabelSetKey("LIKES"),
		Target:    graph.LabelSetKey("Person"),
	}
	samples := []resolver.ResolvedType{
		resolver.ResolvedProperty{Type: graph.TypeString},
		resolver.ResolvedNode{Labels: graph.LabelSetKey("Person")},
		resolver.ResolvedEdge{EdgeKey: knowsKey},
		resolver.ResolvedTemporal{Kind: resolver.TemporalDate},
		resolver.ResolvedScalar{Kind: resolver.ScalarInt},
		resolver.ResolvedScalar{Kind: resolver.ScalarNull},
		resolver.ResolvedList{Element: resolver.ResolvedScalar{Kind: resolver.ScalarInt}},
		resolver.ResolvedUnknown{},
		resolver.ResolvedEdgeUnion{EdgeKeys: []schema.EdgeKey{knowsKey, likesKey}},
	}
	seen := map[codegen.ColumnKind]struct{}{}
	for _, s := range samples {
		plan, err := codegen.BuildListElemPlan(s, entities, index, stubTypeMap{}, 0, "PathActionsUnion")
		require.NoError(t, err)
		require.NotNil(t, plan)
		_, ok := allowed[plan.Kind]
		require.True(t, ok, "plan.Kind %d not in allowed set", plan.Kind)
		seen[plan.Kind] = struct{}{}
	}
	require.Len(t, seen, 9, "every ColumnKind value should be reachable via buildListElemPlan; missing arms indicate a plan-builder gap")
}

// TestPhaseBCommitsIsWrite asserts that phaseBDerive commits the
// StatementWrite axis as a Query.IsWrite bool (spec §1.2). Real
// two-value semantic axis, boolean is the honest type.
func TestPhaseBCommitsIsWrite(t *testing.T) {
	tests := []struct {
		name      string
		statement resolver.StatementKind
		want      bool
	}{
		{"read is not write", resolver.StatementRead, false},
		{"write is write", resolver.StatementWrite, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := codegen.NamedQuery{
				Name:        "Q",
				Cardinality: queryfile.CardinalityExec,
				SourceText:  "MATCH (n) DELETE n",
				Validated: resolver.ValidatedQuery{
					Statement: tt.statement,
				},
			}
			out, err := codegen.PhaseBDerive([]codegen.NamedQuery{q}, nil, nil, stubTypeMap{})
			require.NoError(t, err)
			require.Len(t, out, 1)
			require.Equal(t, tt.want, out[0].IsWrite)
		})
	}
}

// sharedEdgeLabelFixture is a schema whose LikesFwd and LikesRev edge
// types carry one label across two endpoint pairs, alongside a Wrote
// under a label of its own. It returns the keys as (fwd, wrote, rev),
// so a caller passing them straight through gets the two label-sharing
// candidates separated by one that shares with neither.
//
// The explicit Names are load-bearing: Phase Z's Rule 4 refuses an
// unnamed edge type whose label is shared across endpoint pairs, so
// without them nothing here reaches per-column admission.
func sharedEdgeLabelFixture() (schema.Schema, schema.EdgeKey, schema.EdgeKey, schema.EdgeKey) {
	person := graph.LabelSetKey("Person")
	post := graph.LabelSetKey("Post")
	likes := graph.LabelSetKey("LIKES")
	fwd := schema.EdgeKey{Source: person, KeyLabels: likes, Target: post}
	rev := schema.EdgeKey{Source: post, KeyLabels: likes, Target: person}
	wrote := schema.EdgeKey{Source: person, KeyLabels: graph.LabelSetKey("WROTE"), Target: post}
	sch := schema.Schema{
		Name: "Test",
		Nodes: map[graph.LabelSetKey]schema.NodeType{
			person: {KeyLabels: person, CompleteLabels: person, Properties: map[string]schema.Property{}},
			post:   {KeyLabels: post, CompleteLabels: post, Properties: map[string]schema.Property{}},
		},
		Edges: map[schema.EdgeKey]schema.EdgeType{
			fwd:   {EdgeKey: fwd, Name: "LikesFwd", Properties: map[string]schema.Property{}},
			rev:   {EdgeKey: rev, Name: "LikesRev", Properties: map[string]schema.Property{}},
			wrote: {EdgeKey: wrote, Name: "Wrote", Properties: map[string]schema.Property{}},
		},
	}
	return sch, fwd, wrote, rev
}

// TestEdgeUnionCandidatesMustCarryDistinctLabels pins the refusal of an
// edge union two of whose candidates carry one label, at both fail-sites
// and in the words the caller reads. The last row is what holds the gate
// on the column rather than on the schema: one schema serves all three,
// and a candidate set the label tells apart still generates.
func TestEdgeUnionCandidatesMustCarryDistinctLabels(t *testing.T) {
	sch, fwd, wrote, rev := sharedEdgeLabelFixture()

	tests := []struct {
		name    string
		column  resolver.ResolvedType
		wantErr string
	}{
		{
			name:   "column projects two candidates under one label",
			column: resolver.ResolvedEdgeUnion{EdgeKeys: []schema.EdgeKey{fwd, wrote, rev}},
			wantErr: `unrepresentable edge union: query "GetAction" column 0 "r" candidates LikesFwd and LikesRev ` +
				`both carry edge label "LIKES" — an edge value carries its label and its properties, not its ` +
				`endpoint types, so nothing in it tells the two apart; constrain the pattern's endpoints or ` +
				`direction so that at most one candidate carries the label`,
		},
		{
			name:   "list element projects two candidates under one label",
			column: resolver.ResolvedList{Element: resolver.ResolvedEdgeUnion{EdgeKeys: []schema.EdgeKey{fwd, wrote, rev}}},
			wantErr: `query "GetAction" column 0 "r": unrepresentable edge union: list element candidates ` +
				`LikesFwd and LikesRev both carry edge label "LIKES" — an edge value carries its label and its ` +
				`properties, not its endpoint types, so nothing in it tells the two apart; constrain the ` +
				`pattern's endpoints or direction so that at most one candidate carries the label`,
		},
		{
			name:   "candidates under distinct labels are admitted",
			column: resolver.ResolvedEdgeUnion{EdgeKeys: []schema.EdgeKey{fwd, wrote}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := codegen.Input{
				Schema: sch,
				Queries: []codegen.NamedQuery{{
					Name:        "GetAction",
					Cardinality: queryfile.CardinalityOne,
					SourceText:  "MATCH (x:Person)-[r:LIKES|WROTE]-(y:Post) RETURN r",
					Validated:   resolver.ValidatedQuery{Columns: []resolver.Column{{Name: "r", Type: tt.column}}},
				}},
			}
			_, err := codegen.Prepare(in, stubTypeMap{}, "")
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, codegen.ErrUnrepresentableEdgeUnion)
			require.EqualError(t, err, tt.wantErr)
		})
	}
}

// TestEdgeUnionInterfaceNamesMustNotCoincideAcrossQueries pins source 6
// of the identifier sweep in the words the caller reads. The interface
// name derives from author text on both halves — the query's method name
// and the column's row field — so two queries whose mangles meet emit one
// package-level name twice, and the refusal is what closes the scope.
//
// The pair here is a boundary shift: Get + "userName" and GetUser +
// "name" both mangle to GetUserName. What makes the message worth pinning
// is that neither query is at fault alone, so a message naming one of
// them leaves the reader without the rename to make.
func TestEdgeUnionInterfaceNamesMustNotCoincideAcrossQueries(t *testing.T) {
	sch, fwd, wrote, _ := sharedEdgeLabelFixture()
	column := resolver.ResolvedEdgeUnion{EdgeKeys: []schema.EdgeKey{fwd, wrote}}

	in := codegen.Input{
		Schema: sch,
		Queries: []codegen.NamedQuery{
			{
				Name:        "Get",
				Cardinality: queryfile.CardinalityOne,
				SourceText:  "MATCH (x:Person)-[r:LIKES|WROTE]-(y:Post) RETURN r AS userName",
				Validated:   resolver.ValidatedQuery{Columns: []resolver.Column{{Name: "userName", Type: column}}},
			},
			{
				Name:        "GetUser",
				Cardinality: queryfile.CardinalityOne,
				SourceText:  "MATCH (x:Person)-[r:LIKES|WROTE]-(y:Post) RETURN r AS name",
				Validated:   resolver.ValidatedQuery{Columns: []resolver.Column{{Name: "name", Type: column}}},
			},
		},
	}

	_, err := codegen.Prepare(in, stubTypeMap{}, "")
	require.ErrorIs(t, err, codegen.ErrIdentifierCollision)
	// One ordered substring, not two independent ones: source 6 inserts
	// in Input.Queries order, so which query lands on the message's
	// "first" side is part of the contract. Two unordered contains would
	// read the same either way round.
	require.ErrorContains(t, err,
		`emitted by both edgeUnion interface "GetUserName" for query "Get" column 0 "userName" `+
			`and edgeUnion interface "GetUserName" for query "GetUser" column 0 "name"`)
}

// TestTemporalKindRefusalReachesTheCaller pins the contract the ok=false
// half of the temporal row exists for, at both fail-sites and in the
// words the caller reads. Without it the phase has no way to be told
// "no carrier" and carries the table's answer onto the prepared surface
// whatever it is, so a backend with nothing to say emits a column no
// decoder can fill, at no error.
//
// The admitted rows are the load-bearing half: the type map refuses one
// kind and every other kind still generates, which is what a backend
// committing its encodings one at a time needs (bd gqlc-35yu.11 —
// faithful encodings for some kinds, none for a calendar duration). A
// design refusing the enum whole would pass the first two rows and fail
// these.
func TestTemporalKindRefusalReachesTheCaller(t *testing.T) {
	refused := resolver.TemporalDuration

	tests := []struct {
		name    string
		column  resolver.ResolvedType
		wantErr string
	}{
		{
			name:    "column projects a kind with no carrier",
			column:  resolver.ResolvedTemporal{Kind: refused},
			wantErr: `unrepresentable temporal kind: query "GetWhen" column 0 "t" projects temporal(duration)`,
		},
		{
			name:   "list element projects a kind with no carrier",
			column: resolver.ResolvedList{Element: resolver.ResolvedTemporal{Kind: refused}},
			wantErr: `query "GetWhen" column 0 "t": unrepresentable temporal kind: ` +
				`list element projects temporal(duration)`,
		},
		{
			name:   "nested list element projects a kind with no carrier",
			column: resolver.ResolvedList{Element: resolver.ResolvedList{Element: resolver.ResolvedTemporal{Kind: refused}}},
			wantErr: `query "GetWhen" column 0 "t": unrepresentable temporal kind: ` +
				`list element projects temporal(duration)`,
		},
	}
	for _, k := range []resolver.Temporal{
		resolver.TemporalDate,
		resolver.TemporalTime,
		resolver.TemporalLocalTime,
		resolver.TemporalDateTime,
		resolver.TemporalLocalDateTime,
	} {
		tests = append(tests,
			struct {
				name    string
				column  resolver.ResolvedType
				wantErr string
			}{
				name:   "column projects " + k.String() + ", which has a carrier",
				column: resolver.ResolvedTemporal{Kind: k},
			},
			struct {
				name    string
				column  resolver.ResolvedType
				wantErr string
			}{
				name:   "list element projects " + k.String() + ", which has a carrier",
				column: resolver.ResolvedList{Element: resolver.ResolvedTemporal{Kind: k}},
			},
		)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := codegen.Input{
				Schema: schema.Schema{Name: "Test"},
				Queries: []codegen.NamedQuery{{
					Name:        "GetWhen",
					Cardinality: queryfile.CardinalityOne,
					SourceText:  "MATCH (n) RETURN duration({days: 1}) AS t",
					Validated:   resolver.ValidatedQuery{Columns: []resolver.Column{{Name: "t", Type: tt.column}}},
				}},
			}
			tm := partialTemporalTypeMap{refuse: refused}
			prepared, err := codegen.Prepare(in, tm, "")
			if tt.wantErr == "" {
				require.NoError(t, err)
				require.NotEmpty(t, prepared.Queries[0].RowFields[0].GoType)
				return
			}
			require.ErrorIs(t, err, codegen.ErrUnrepresentableTemporal)
			require.EqualError(t, err, tt.wantErr)
		})
	}
}

// TestStorageRefusalReachesTheCallerAsItsOwnSentinel holds the storage
// axis apart from the carrier axis at the one place both are asked.
//
// Every row runs the SAME width, LIST<LIST<INT16>>, and varies only the
// typeMap and the position the width occupies. That is what makes the
// axes separable: a suite that refused a width no carrier admits would
// not be able to say which question answered.
//
// The admits row is not decoration. Without it the refused row is
// satisfied by a pipeline that refuses a nested list outright, which is
// the over-refusal the design forbids — neo4j has a faithful [][]int16
// and decodes one arriving as a query value.
func TestStorageRefusalReachesTheCallerAsItsOwnSentinel(t *testing.T) {
	nested := graph.ListOf(graph.ListOf(graph.TypeInt16, true), true)
	person := graph.LabelSetKey("Person")
	schemaWith := func(pt graph.PropertyType) schema.Schema {
		return schema.Schema{
			Name: "Test",
			Nodes: map[graph.LabelSetKey]schema.NodeType{
				person: {KeyLabels: person, CompleteLabels: person, Properties: map[string]schema.Property{
					"matrix": {Name: "matrix", Type: pt},
				}},
			},
		}
	}

	t.Run("a stored property the store will not hold", func(t *testing.T) {
		_, _, err := codegen.PhaseZAdmit(schemaWith(nested), unstorablePropertyTypeMap{refuse: nested})
		require.ErrorIs(t, err, codegen.ErrUnstorableProperty)
		require.NotErrorIs(t, err, codegen.ErrUnrepresentableWidth,
			"the carrier admits this width; reporting the carrier sentinel would send the caller to a Go-type gap that is not there")
		require.EqualError(t, err,
			`unstorable property width: entity "Person" property "matrix" has `+string(nested))
	})

	t.Run("the same width where the store holds it", func(t *testing.T) {
		_, _, err := codegen.PhaseZAdmit(schemaWith(nested), stubTypeMap{})
		require.NoError(t, err,
			"nothing else in the pipeline refuses this width, so the row above measured the storage answer and not the width")
	})

	t.Run("the carrier question is asked first", func(t *testing.T) {
		// Refused on BOTH axes. prepareEntityFields asks the carrier
		// first, so the caller is told the narrower thing: a backend
		// with no Go type for a width cannot store it either, and
		// reporting the storage gap would hide that there is no
		// carrier to fall back to.
		_, _, err := codegen.PhaseZAdmit(schemaWith(graph.TypeDecimal), unstorablePropertyTypeMap{refuse: graph.TypeDecimal})
		require.ErrorIs(t, err, codegen.ErrUnrepresentableWidth)
		require.NotErrorIs(t, err, codegen.ErrUnstorableProperty)
	})

	// The storage rule is about what the store keeps, and neither a
	// column nor a parameter keeps anything. These two rows are what
	// would go red if the sweep were folded in beside the column and
	// parameter checks rather than beside the entity ones.
	//
	// Both carry the width as a ResolvedProperty, which is the only
	// column and parameter shape holding a graph.PropertyType and so the
	// only one a storage sweep could be asked about. Measured: written
	// instead as a ResolvedList over a ResolvedScalar — the shape the
	// query text below actually resolves to — the column row passed
	// against a prepare.go that DID ask the question in the column
	// sweep, because that shape reaches no PropertyType to ask about.
	for _, tt := range []struct {
		name    string
		queries []codegen.NamedQuery
	}{
		{"a query column of the same width is not asked", []codegen.NamedQuery{{
			Name:        "Nested",
			Cardinality: queryfile.CardinalityOne,
			SourceText:  "RETURN [[1]] AS xss",
			Validated: resolver.ValidatedQuery{Columns: []resolver.Column{{
				Name: "xss",
				Type: resolver.ResolvedProperty{Type: nested},
			}}},
		}}},
		{"a query parameter of the same width is not asked", []codegen.NamedQuery{{
			Name:        "Nested",
			Cardinality: queryfile.CardinalityOne,
			SourceText:  "MATCH (p:Person) WHERE p.matrix = $xss RETURN p.matrix AS xss",
			Validated: resolver.ValidatedQuery{
				Columns: []resolver.Column{{Name: "xss", Type: resolver.ResolvedScalar{Kind: resolver.ScalarInt}}},
				Parameters: []resolver.ResolvedParameter{{
					Name: "xss",
					Type: resolver.ResolvedProperty{Type: nested},
				}},
			},
		}}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			in := codegen.Input{Schema: schemaWith(graph.TypeInt64), Queries: tt.queries}
			_, err := codegen.Prepare(in, unstorablePropertyTypeMap{refuse: nested}, "")
			require.NoError(t, err)
		})
	}
}

// ageOnlyTargets is the golden target set for the four names only the
// Apache AGE emission declares.
var ageOnlyTargets = []string{"apache-age-pgx-v5"}

// reservedIdentifierRows is the reserved set written out longhand, with
// the scope each name's emitted declaration occupies and the golden
// targets that declare it. Both columns are read off the committed
// goldens rather than off the templates by eye:
// TestReservedScopeMatchesTheEmittedGoldens holds every row to what the
// corpus declares, and fails a row the corpus declares nowhere.
//
// The declaredBy column is where the set stops being symmetric, and it
// bounds what the reservation is worth. Four rows are emitted by Apache
// AGE alone, so refusing `NODE TYPE DBTX` on a neo4j-only batch refuses
// a name neo4j's emission leaves free — a false refusal on that target.
// The set is uniform anyway, per D2 Resolved.
//
// The five temporal carriers are not among the asymmetric rows even
// though the backends admit different temporal widths. temporal.go
// declares all five together whichever width triggered it, so Apache AGE
// declares Time while still refusing a zoned TIME column (gqlc-oeqi):
// what reserves the name is the emission, not the admission.
var reservedIdentifierRows = []struct {
	name       string
	scope      codegen.IdentifierScope
	declaredBy []string // nil means every target the corpus emits
}{
	{"Queries", codegen.ScopePackage, nil},
	{"New", codegen.ScopePackage, nil},
	{"WithTx", codegen.ScopeMethod, nil},
	{"ReadQuerier", codegen.ScopePackage, nil},
	{"WriteQuerier", codegen.ScopePackage, nil},
	{"Querier", codegen.ScopePackage, nil},
	{"ErrNoRows", codegen.ScopePackage, nil},
	{"ErrMultipleResults", codegen.ScopePackage, nil},
	{"DBTX", codegen.ScopePackage, ageOnlyTargets},
	{"SessionInit", codegen.ScopePackage, ageOnlyTargets},
	{"EnsureGraph", codegen.ScopeMethod, ageOnlyTargets},
	{"DropGraph", codegen.ScopeMethod, ageOnlyTargets},
	{"Tx", codegen.ScopePackage, nil},
	{"ErrTxDone", codegen.ScopePackage, nil},
	{"Begin", codegen.ScopeMethod, nil},
	{"Commit", codegen.ScopeMethod, nil},
	{"Rollback", codegen.ScopeMethod, nil},
	{"Date", codegen.ScopePackage, nil},
	{"Time", codegen.ScopePackage, nil},
	{"LocalTime", codegen.ScopePackage, nil},
	{"LocalDateTime", codegen.ScopePackage, nil},
	{"Duration", codegen.ScopePackage, nil},
}

// TestReservedIdentifiersAreUniformAcrossBackends pins the reserved set
// (spec §4.1) as a whole: every exported name a generated package
// declares collides with a query name, whichever backend is selected.
//
// Uniform is not the same as universally colliding, and the difference
// is a cost this test locks in rather than hides. DBTX, SessionInit and
// the graph lifecycle pair are declared only by the Apache AGE emission
// — reservedIdentifierRows carries that as a measured column — so
// refusing them on a neo4j-only batch refuses a name neo4j leaves free.
// The set stays uniform anyway, per D2 Resolved.
func TestReservedIdentifiersAreUniformAcrossBackends(t *testing.T) {
	want := make([]string, 0, len(reservedIdentifierRows))
	for _, row := range reservedIdentifierRows {
		want = append(want, row.name)
	}
	got := make([]string, 0, len(codegen.ReservedIdentifiers))
	for name := range codegen.ReservedIdentifiers {
		got = append(got, name)
	}
	require.ElementsMatch(t, want, got)

	for _, row := range reservedIdentifierRows {
		t.Run(row.name, func(t *testing.T) {
			require.Equal(t, row.scope, codegen.ReservedIdentifiers[row.name])

			in := codegen.Input{
				Schema:  schema.Schema{Name: "Test"},
				Queries: []codegen.NamedQuery{{Name: row.name, Cardinality: queryfile.CardinalityExec, SourceText: "MATCH (n) DELETE n"}},
			}
			_, err := codegen.Prepare(in, stubTypeMap{}, "")
			require.ErrorIs(t, err, codegen.ErrIdentifierCollision)
			require.ErrorContains(t, err, `query "`+row.name+`" at position 0`)
		})
	}
}

// TestReservedScopeDecidesWhichEntityNamesCollide runs both halves of
// the scope column over the entity axis, on the node and the edge
// source alike. A schema whose element type names a package-scope
// declaration is refused; one naming a method on *Queries generates,
// and the entity keeps the name it asked for.
//
// The allow half is what holds the gate to the scope it measured: a
// blanket reserve over the whole set refuses three schemas the emitter
// serves.
func TestReservedScopeDecidesWhichEntityNamesCollide(t *testing.T) {
	person := graph.LabelSetKey("Person")

	sources := []struct {
		axis   string
		schema func(name string) schema.Schema
	}{
		{"node", func(name string) schema.Schema {
			label := graph.LabelSetKey(name)
			return schema.Schema{
				Name: "Test",
				Nodes: map[graph.LabelSetKey]schema.NodeType{
					label: {KeyLabels: label, CompleteLabels: label, Properties: map[string]schema.Property{}},
				},
			}
		}},
		// The edge half names its entity explicitly (Rule 1) rather than
		// through the label, because Rule 3 title-cases an ALL-CAPS edge
		// label — so no label mangles to DBTX, and driving this axis
		// through the label would silently stop testing that row. The
		// node half still reaches every row through the label mangle,
		// which is where Rule 2's identity disposition is covered.
		{"edge", func(name string) schema.Schema {
			key := schema.EdgeKey{Source: person, KeyLabels: graph.LabelSetKey(name), Target: person}
			return schema.Schema{
				Name: "Test",
				Nodes: map[graph.LabelSetKey]schema.NodeType{
					person: {KeyLabels: person, CompleteLabels: person, Properties: map[string]schema.Property{}},
				},
				Edges: map[schema.EdgeKey]schema.EdgeType{
					key: {EdgeKey: key, Name: name, Properties: map[string]schema.Property{}},
				},
			}
		}},
	}

	for _, src := range sources {
		for _, row := range reservedIdentifierRows {
			t.Run(src.axis+"/"+row.name, func(t *testing.T) {
				prepared, err := codegen.Prepare(codegen.Input{Schema: src.schema(row.name)}, stubTypeMap{}, "")
				if row.scope == codegen.ScopeMethod {
					require.NoError(t, err)
					names := make([]string, 0, len(prepared.Entities))
					for _, e := range prepared.Entities {
						names = append(names, e.Name)
					}
					require.Contains(t, names, row.name)
					return
				}
				require.ErrorIs(t, err, codegen.ErrIdentifierCollision)
				// One ordered substring, not two independent ones: the
				// seed exists so the fixed declaration lands on the
				// message's "first" side, and two unordered contains
				// would read the same either way round.
				require.ErrorContains(t, err,
					`emitted by both the generated package's fixed declaration "`+row.name+
						`" and entity struct "`+row.name+`"`)
			})
		}
	}
}

// TestQueryTextConstCollidesWithADecodeHelper pins source 7 of the
// identifier sweep. Both colliding names are generator-owned — the const
// is derived from a method name, the helper from a schema label — which
// is why the capture guards do not reach it: those police author-chosen
// identifiers against generator-owned ones, and here there is no author
// identifier on either side to police.
//
// The pair is a boundary shift, like source 6's. A label FooQueryText
// mangles to entity FooQueryText and so to helper decodeFooQueryText; a
// query DecodeFoo takes the const decodeFooQueryText. Neither name is at
// fault alone, so a message naming one of them leaves the reader without
// the rename to make.
//
// Before this source existed the sweep passed and both declarations were
// emitted into one package, so `gqlc generate` exited 0 over Go that
// `go build` then refused with "decodeFooQueryText redeclared in this
// block" (bd gqlc-igs4).
func TestQueryTextConstCollidesWithADecodeHelper(t *testing.T) {
	label := graph.LabelSetKey("FooQueryText")
	in := codegen.Input{
		Schema: schema.Schema{
			Name: "Test",
			Nodes: map[graph.LabelSetKey]schema.NodeType{
				label: {KeyLabels: label, CompleteLabels: label, Properties: map[string]schema.Property{}},
			},
		},
		Queries: []codegen.NamedQuery{
			{Name: "DecodeFoo", Cardinality: queryfile.CardinalityExec, SourceText: "MATCH (n) DELETE n"},
		},
	}

	_, err := codegen.Prepare(in, stubTypeMap{}, "")
	require.ErrorIs(t, err, codegen.ErrIdentifierCollision)
	// One ordered substring, not two independent ones: source 7 inserts
	// after source 2, so which construct lands on the message's "first"
	// side is part of the contract. Two unordered contains would read the
	// same either way round.
	require.ErrorContains(t, err,
		`emitted by both entity decode helper "decodeFooQueryText" for entity struct "FooQueryText" `+
			`and query "DecodeFoo" query-text const "decodeFooQueryText"`)
}

// goldenCorpusGlob reaches the committed golden trees from this package.
// The conformance suite reads the same corpus through its own root, which
// an env var can redirect at a copy; this sweep wants the tracked trees
// specifically, because what it measures is what the repo ships.
const goldenCorpusGlob = "../../test/data/codegen/valid/*/golden/*/*.go"

// goldenTarget is the backend a golden Go file was emitted for, read off
// the path: test/data/codegen/valid/<fixture>/golden/<target>/<file>.go.
func goldenTarget(t *testing.T, path string) string {
	t.Helper()
	target := filepath.Base(filepath.Dir(path))
	require.Equal(t, "golden", filepath.Base(filepath.Dir(filepath.Dir(path))),
		"%s does not sit under a golden/<target>/ directory, so its target cannot be read off the path", path)
	return target
}

// scopeName renders an identifierScope for a fail message, so that a
// reader does not have to know which iota is which.
func scopeName(s codegen.IdentifierScope) string {
	if s == codegen.ScopeMethod {
		return "scopeMethod"
	}
	return "scopePackage"
}

// goldenFixture is the fixture a golden Go file was emitted from, read
// off the path beside goldenTarget:
// test/data/codegen/valid/<fixture>/golden/<target>/<file>.go.
//
// The target is not part of the key, so a fixture enrolled in three
// targets is one fixture here rather than three.
func goldenFixture(t *testing.T, path string) string {
	t.Helper()
	require.Equal(t, "golden", filepath.Base(filepath.Dir(filepath.Dir(path))),
		"%s does not sit under a golden/<target>/ directory, so its fixture cannot be read off the path", path)
	return filepath.Base(filepath.Dir(filepath.Dir(filepath.Dir(path))))
}

// eachDecl calls record for every named declaration of file with the
// scope it occupies. A func with a receiver is scopeMethod; a type, a
// var, a const and a plain func are all package-level.
func eachDecl(file *ast.File, record func(name string, scope codegen.IdentifierScope)) {
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					record(s.Name.Name, codegen.ScopePackage)
				case *ast.ValueSpec:
					for _, n := range s.Names {
						record(n.Name, codegen.ScopePackage)
					}
				}
			}
		case *ast.FuncDecl:
			scope := codegen.ScopeMethod
			if d.Recv == nil {
				scope = codegen.ScopePackage
			}
			record(d.Name.Name, scope)
		}
	}
}

// TestReservedScopeMatchesTheEmittedGoldens holds both table columns to
// the corpus rather than to a claim about the templates. Three checks:
//
//  1. a reserved name is recorded scopePackage exactly when some golden
//     declares it package-level;
//  2. the targets declaring it are exactly the ones the table records,
//     which is what keeps the four Apache AGE-only rows from reading as
//     symmetric with the other eight;
//  3. every row is declared by at least one golden — a name the corpus
//     never declares would agree with either scope and with any target
//     set, so without this half a row that had silently stopped being
//     emitted would still pass.
//
// A nil declaredBy claims every target in the corpus, which is the
// strict end: a row added without the column fails unless every target
// really does declare it.
//
// A method occupies no package block whatever its receiver, so the
// receiver type is not consulted. The scope this pins is what
// sweepIdentifiers reads to seed source 0; a query taking a method-scope
// name is refused at Phase A on membership, which does not read scope.
//
// Check 1 is an equality over every declaration, in both directions: a
// scopePackage row is declared package-level and nowhere as a method, a
// scopeMethod row nowhere package-level. PR #1489 relaxed the first half
// to a biconditional for the one corpus declaration that needed it,
// (*Tx).Queries — the handle accessor it introduced — and gqlc-f4hf then
// removed that accessor, which reservedIdentifiers' comment on the
// Queries row records. No corpus name is declared at both scopes today,
// so the relaxation permits nothing and costs a direction.
//
// The two halves catch opposite faults, and only one of them is about
// miscompilation. A scopeMethod row declared package-level lets sources
// 1-6 take a name the package block holds. A scopePackage row declared
// as a method compiles and collides correctly — Phase A reads membership
// — but it means the emitter grew an exported method reusing a reserved
// name, and no other guard over these goldens reports that: txsurface
// skips non-Begin *Queries methods by design, and the fixed-declaration
// sweep does not force a name that is already a row (gqlc-5ask).
func TestReservedScopeMatchesTheEmittedGoldens(t *testing.T) {
	paths, err := filepath.Glob(goldenCorpusGlob)
	require.NoError(t, err)
	require.NotEmpty(t, paths, "no golden Go under %s, so this sweep holds nothing", goldenCorpusGlob)

	corpusTargets := map[string]struct{}{}
	// name -> scope -> one path declaring it that way, for the fail message.
	declared := make(map[string]map[codegen.IdentifierScope]string)
	// name -> set of targets declaring it.
	declaredBy := make(map[string]map[string]struct{})
	record := func(name, path string, scope codegen.IdentifierScope) {
		if _, reserved := codegen.ReservedIdentifiers[name]; !reserved {
			return
		}
		if declared[name] == nil {
			declared[name] = make(map[codegen.IdentifierScope]string)
			declaredBy[name] = make(map[string]struct{})
		}
		if _, seen := declared[name][scope]; !seen {
			declared[name][scope] = path
		}
		declaredBy[name][goldenTarget(t, path)] = struct{}{}
	}

	fset := token.NewFileSet()
	for _, path := range paths {
		corpusTargets[goldenTarget(t, path)] = struct{}{}
		file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		require.NoError(t, err, "parsing %s", path)
		eachDecl(file, func(name string, scope codegen.IdentifierScope) { record(name, path, scope) })
	}

	everyTarget := make([]string, 0, len(corpusTargets))
	for target := range corpusTargets {
		everyTarget = append(everyTarget, target)
	}
	require.NotEmpty(t, everyTarget)

	for _, row := range reservedIdentifierRows {
		t.Run(row.name, func(t *testing.T) {
			at := declared[row.name]
			require.NotEmpty(t, at,
				"no golden declares %q, so its columns rest on nothing; either the corpus lost the fixture that emitted it or the name is no longer emitter-fixed and belongs out of reservedIdentifiers",
				row.name)
			pkgPath, atPackage := at[codegen.ScopePackage]
			methPath, atMethod := at[codegen.ScopeMethod]
			if row.scope == codegen.ScopePackage {
				require.True(t, atPackage,
					"the reserved set records %q at scopePackage, but no golden declares it package-level — %s declares it as a method; seeding source 0 with it reserves the package block against a name that does not hold it",
					row.name, at[codegen.ScopeMethod])
				require.False(t, atMethod,
					"%s declares %q as a method while the reserved set records it scopePackage; the name stays reserved, so nothing collides and nothing else over these goldens reports it — decide here whether the emitter should be reusing a package-scope reserved name at method scope",
					methPath, row.name)
			} else {
				require.False(t, atPackage,
					"%s declares %q package-level, but the reserved set records it %s, which lets sources 1-6 take a name the package block already holds",
					pkgPath, row.name, scopeName(row.scope))
			}

			wantTargets := row.declaredBy
			if wantTargets == nil {
				wantTargets = everyTarget
			}
			gotTargets := make([]string, 0, len(declaredBy[row.name]))
			for target := range declaredBy[row.name] {
				gotTargets = append(gotTargets, target)
			}
			require.ElementsMatch(t, wantTargets, gotTargets,
				"%q is declared by a different target set than the reserved set records; a name that changed which backends emit it changes whether reserving it on the others is a false refusal",
				row.name)
		})
	}
}

// fixedDeclarationFiles names the emitted files whose exported
// declarations are the emitter's own rather than derived from the
// batch's names: the handle and its seam in db.go, the Apache AGE graph
// lifecycle in graph.go, the three interfaces in querier.go. Whether a
// given one is emitted can still turn on the batch — db.go carries
// ErrNoRows and ErrMultipleResults only for a batch with a `:one` query
// — which is why the measurement below reads names declared by every
// fixture and not names declared by some.
var fixedDeclarationFiles = map[string]bool{
	"db.go": true, "graph.go": true, "querier.go": true,
	// The five neutral temporal carriers in temporal.go and the
	// unexported driver bridge in temporal_neo4j.go (ADR 0033). Both are
	// emitter-fixed: which of them a batch emits turns on the widths the
	// surface names, but never on a name the batch chose. The bridge
	// exports nothing today, and classifying it here rather than as
	// input-derived is what would force a reserved row if it ever did.
	"temporal.go": true, "temporal_neo4j.go": true,
}

// inputDerivedFiles names the emitted files whose exported declarations
// follow from the batch instead: entity structs and their decode
// helpers in models.go, and the query surface in each queryFileSuffix
// file. Those are sources 1-6 of the sweep, not source 0.
var inputDerivedFiles = map[string]bool{"models.go": true}

// queryFileSuffix ends every emitted per-source query file. The stem
// before it is the query source's own basename, so that side of the
// partition is a suffix rather than a set of names (§5.5).
const queryFileSuffix = ".cypher.go"

// querySourceOf is the query source a per-source query golden was
// emitted from: the emitter names the file after the source's own
// basename, so `people.cypher` yields `people.cypher.go`. The source is
// read from the emitting fixture's own directory, beside its
// manifest.json, which is where every one of the corpus's query sources
// sits.
//
// The second result is false for a golden that is not per-source shaped
// at all, which is how db.go and models.go leave this arm before the
// filesystem is consulted.
func querySourceOf(path string) (string, bool) {
	base := filepath.Base(path)
	if !strings.HasSuffix(base, queryFileSuffix) {
		return "", false
	}
	fixtureDir := filepath.Dir(filepath.Dir(filepath.Dir(path)))
	return filepath.Join(fixtureDir, strings.TrimSuffix(base, ".go")), true
}

// TestFixedDeclarationSweepEqualsTheReservedSet reads the exported
// declarations of the goldens fixedDeclarationFiles names and holds that
// set equal to reservedIdentifiers, in both directions.
//
// Forwards is against reopening by addition the defect gqlc-e6mh closed
// by edit: an emitter that grows a new exported declaration with no
// matching row leaves every other guard here green, and `NODE TYPE
// <thatName>` then emits a package that does not compile. Backwards is
// against the sweep going dark: a reserved name no swept file declares
// means the file declaring it left fixedDeclarationFiles.
// TestReservedScopeMatchesTheEmittedGoldens does not report that — it
// globs the whole corpus with no file filter, so it still finds the name
// in the file this sweep stopped reading.
//
// Membership is all this asserts. The scope column is held by the sweep
// above, which covers any name once it is a row. A method is recorded
// whatever its receiver, and exportedness rather than the receiver is
// what the filter below applies, so an exported method on a receiver
// other than *Queries is forced into reservedIdentifiers, where Phase A
// refuses a query on membership alone. Two are, both on *Tx: Commit and
// Rollback. That is correct rather than tolerated — a query taking one
// of those names is emitted on the core and promotes into *Tx, where the
// depth-0 method shadows it silently (spec codegen-tx-embedded-querier.md
// §5). Redeclaration was never the ground, so the receiver is not what
// the reservation turns on. TestSweptMethodReceiversEmbedTheQueryCore
// fences the antecedent that makes this so. The run methods the neo4j
// targets' db.go declares on driverDB and on txDB are dropped by the
// filter below for being unexported, not for their receiver.
//
// Which side of the partition a file sits on is measured, not only
// declared, and the queryFileSuffix arm is measured twice over. The
// suffix says only what SHAPE a file has; what admits it is a query
// source of that name in its own fixture (querySourceOf). So the arm
// admits exactly the per-source files the batch's own sources justify,
// and an emitted file that merely ends in the suffix is unclassified —
// which is what closes gqlc-laoy. That evidence is per file rather than
// counted across fixtures, so a file one fixture alone emits is held to
// it like any other.
//
// The second measurement is the older one and still earns its place on
// what the first does not cover: a basename more than one fixture emits,
// and an exported name every one of those fixtures declares, is a name
// that does not follow from the batch however well the FILE is
// justified, so it belongs in fixedDeclarationFiles. A basename a single
// fixture emits carries no such evidence and is skipped there — today
// the two per-source query files of multi_source_files. One fixture is
// one fixture directory across every target — see goldenFixture — so
// qualifying that key by target puts each of those two at two fixtures
// with an everywhere-name apiece, and that loop then demands they enter
// fixedDeclarationFiles though both are genuinely input-derived. Closing
// gqlc-laoy does not make that key safe to change; it removes the reason
// to want to.
//
// It reads the committed goldens, not the emitter. A target with no
// enrolled fixture declares nothing here, and a rename in a template
// reaches this sweep only once the goldens are regenerated.
func TestFixedDeclarationSweepEqualsTheReservedSet(t *testing.T) {
	paths, err := filepath.Glob(goldenCorpusGlob)
	require.NoError(t, err)
	require.NotEmpty(t, paths, "no golden Go under %s, so this sweep holds nothing", goldenCorpusGlob)

	// basename -> fixtures emitting it, and basename -> exported name ->
	// fixtures declaring it, so the partition can be read off the corpus.
	emittedBy := map[string]map[string]bool{}
	declaredBy := map[string]map[string]map[string]bool{}
	// name -> one swept path declaring it, for the fail message.
	found := map[string]string{}
	// basename -> one emitting path whose own fixture carries no query
	// source of that name, and the source it looked for. A basename absent
	// here is one every emitting fixture justifies, so a file justified
	// under one fixture and not another is still reported.
	unjustified := map[string][2]string{}
	perSource := 0
	fset := token.NewFileSet()
	for _, path := range paths {
		base, fixture := filepath.Base(path), goldenFixture(t, path)
		if src, isQueryFile := querySourceOf(path); isQueryFile {
			if _, err := os.Stat(src); err == nil {
				perSource++
			} else if _, seen := unjustified[base]; !seen {
				unjustified[base] = [2]string{path, src}
			}
		}
		file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		require.NoError(t, err, "parsing %s", path)
		if emittedBy[base] == nil {
			emittedBy[base] = map[string]bool{}
			declaredBy[base] = map[string]map[string]bool{}
		}
		emittedBy[base][fixture] = true
		eachDecl(file, func(name string, _ codegen.IdentifierScope) {
			if !ast.IsExported(name) {
				return
			}
			if declaredBy[base][name] == nil {
				declaredBy[base][name] = map[string]bool{}
			}
			declaredBy[base][name][fixture] = true
			if _, seen := found[name]; !seen && fixedDeclarationFiles[base] {
				found[name] = path
			}
		})
	}

	for _, base := range slices.Sorted(maps.Keys(emittedBy)) {
		require.True(t,
			fixedDeclarationFiles[base] || inputDerivedFiles[base] || strings.HasSuffix(base, queryFileSuffix),
			"the corpus emits %s, which neither fixedDeclarationFiles nor inputDerivedFiles classifies; an unclassified file is one this sweep never reads, so every exported name it declares owes no reserved row",
			base)
		at := unjustified[base]
		require.Emptyf(t, at[0],
			"%s is emitted, but its fixture carries no query source at %s, so no source in the batch justifies that file; the %s suffix alone admits any emitted file whose name ends in it, and every exported name this one declares would then owe no reserved row (gqlc-laoy)",
			at[0], at[1], queryFileSuffix)
	}
	require.NotZero(t, perSource,
		"no golden under %s is the emitted counterpart of a query source in its own fixture, so this arm of the partition admits nothing; the emitter renamed the per-source query file, or the fixtures moved their query sources out of the fixture directory",
		goldenCorpusGlob)
	for _, base := range slices.Sorted(maps.Keys(fixedDeclarationFiles)) {
		require.NotEmpty(t, emittedBy[base],
			"no golden under %s is named %s, so every exported declaration that file emits went unread; the emitter renamed it and this set kept the old name, or the corpus lost every fixture emitting it",
			goldenCorpusGlob, base)
	}
	for _, base := range slices.Sorted(maps.Keys(inputDerivedFiles)) {
		require.NotEmpty(t, emittedBy[base],
			"no golden under %s is named %s, so classifying it input-derived excludes nothing; the name is stale, and a file that is emitted under some other name is now unclassified",
			goldenCorpusGlob, base)
	}

	for _, base := range slices.Sorted(maps.Keys(emittedBy)) {
		if len(emittedBy[base]) < 2 {
			continue
		}
		var everywhere []string
		for name, fixtures := range declaredBy[base] {
			if len(fixtures) == len(emittedBy[base]) {
				everywhere = append(everywhere, name)
			}
		}
		if len(everywhere) == 0 {
			continue
		}
		slices.Sort(everywhere)
		require.True(t, fixedDeclarationFiles[base],
			"every one of the %d fixtures emitting %s declares %v, so those names do not follow from the batch, but %s is classified out of fixedDeclarationFiles and this sweep never reads it",
			len(emittedBy[base]), base, everywhere, base)
	}

	for name, path := range found {
		_, reserved := codegen.ReservedIdentifiers[name]
		require.True(t, reserved,
			"%s declares exported %q, which reservedIdentifiers does not hold; a schema element deriving that name would redeclare it and the emitted package would not compile",
			path, name)
	}
	for _, name := range slices.Sorted(maps.Keys(codegen.ReservedIdentifiers)) {
		_, swept := found[name]
		require.True(t, swept,
			"no golden this sweep read declares reserved %q, so the file declaring it is classified out of fixedDeclarationFiles and owes nothing here",
			name)
	}
}

// queryCoreType is the unexported struct carrying the emitted query
// methods. Both exported handles embed it, which is the whole mechanism
// §5 rests on.
const queryCoreType = "queries"

// namedType reads the type name out of a receiver or an embedded field,
// through a pointer and through type parameters. The second result is
// false for a shape that names no single type, which no emitted receiver
// is today.
func namedType(expr ast.Expr) (string, bool) {
	for {
		switch e := expr.(type) {
		case *ast.StarExpr:
			expr = e.X
		case *ast.IndexExpr:
			expr = e.X
		case *ast.IndexListExpr:
			expr = e.X
		case *ast.Ident:
			return e.Name, true
		default:
			return "", false
		}
	}
}

// TestSweptMethodReceiversEmbedTheQueryCore fences the antecedent that
// makes the sweep above correct.
//
// That sweep demands a reservedIdentifiers row for every exported
// declaration in fixedDeclarationFiles, methods included, whatever the
// receiver — and Phase A refuses a query on membership alone. The
// refusal is right only because every receiver carrying an exported
// fixed method embeds queryCoreType: a user query is emitted on the core
// and promotes into that receiver's method set, where the fixed method
// at depth 0 shadows it with no diagnostic (spec
// codegen-tx-embedded-querier.md §5). An exported method on a receiver
// that does NOT embed the core promotes nothing and shadows nothing, so
// the row the sweep would demand for it buys a refusal of a query name
// that generates fine.
//
// Nothing enforced that until this test: the limit was stated in prose
// and a new receiver type would have arrived as a row someone added
// rather than a decision someone made (gqlc-tisj).
//
// The embedding is read per emitted package directory rather than per
// file, because the receiver's type declaration and its methods need not
// share a file. Unexported methods are out of scope for the same reason
// the sweep skips them — they occupy no name a schema element can
// derive.
func TestSweptMethodReceiversEmbedTheQueryCore(t *testing.T) {
	paths, err := filepath.Glob(goldenCorpusGlob)
	require.NoError(t, err)
	require.NotEmpty(t, paths, "no golden Go under %s, so this fence holds nothing", goldenCorpusGlob)

	// package dir -> type name -> embeds the core.
	embedsCore := map[string]map[string]bool{}
	// package dir -> receiver type name -> one exported method it carries.
	receivers := map[string]map[string]string{}
	fset := token.NewFileSet()
	for _, path := range paths {
		file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		require.NoError(t, err, "parsing %s", path)
		dir := filepath.Dir(path)
		if embedsCore[dir] == nil {
			embedsCore[dir] = map[string]bool{}
			receivers[dir] = map[string]string{}
		}

		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok {
				continue
			}
			for _, spec := range gen.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				st, ok := ts.Type.(*ast.StructType)
				if !ok {
					continue
				}
				for _, f := range st.Fields.List {
					// An embedded field is the one with no name of its own.
					if len(f.Names) > 0 {
						continue
					}
					if name, ok := namedType(f.Type); ok && name == queryCoreType {
						embedsCore[dir][ts.Name.Name] = true
					}
				}
			}
		}

		if !fixedDeclarationFiles[filepath.Base(path)] {
			continue
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || len(fn.Recv.List) == 0 || !ast.IsExported(fn.Name.Name) {
				continue
			}
			recv, ok := namedType(fn.Recv.List[0].Type)
			require.Truef(t, ok,
				"%s declares exported method %s on a receiver naming no single type, which this fence cannot classify; teach it that shape or the receiver goes unchecked",
				path, fn.Name.Name)
			receivers[dir][recv] = fn.Name.Name
		}
	}

	checked := 0
	for _, dir := range slices.Sorted(maps.Keys(receivers)) {
		for _, recv := range slices.Sorted(maps.Keys(receivers[dir])) {
			checked++
			require.Truef(t, embedsCore[dir][recv],
				"%s declares exported method %s on %s, which does not embed %s. The fixed-declaration sweep will demand a reservedIdentifiers row for %s, and Phase A then refuses any query of that name — but a query emitted on the core neither promotes into %s nor is shadowed by it, so that refusal rejects a name which generates fine. Either %s embeds the core, or %s must be excluded from the sweep rather than reserved",
				dir, receivers[dir][recv], recv, queryCoreType, receivers[dir][recv], recv, recv, receivers[dir][recv])
		}
	}
	require.NotZero(t, checked,
		"no golden under %s declares an exported method in a fixedDeclarationFiles file, so this fence excluded nothing; the emitter stopped emitting the fixed method surface, or fixedDeclarationFiles no longer names the file carrying it",
		goldenCorpusGlob)
}

// allCapsNameFixture builds a one-node schema, optionally with one edge
// from that node to itself, and returns the derived entity names by
// lookup key. Phase Z derives every entity eagerly (§4.5 Rule 4), so no
// query is needed to reach the naming rules.
func allCapsNameFixture(t *testing.T, nodeLabel, edgeLabel string) map[codegen.EntityLookupKey]string {
	t.Helper()
	node := graph.LabelSetKey(nodeLabel)
	sch := schema.Schema{
		Name: "Test",
		Nodes: map[graph.LabelSetKey]schema.NodeType{
			node: {KeyLabels: node, CompleteLabels: node, Properties: map[string]schema.Property{}},
		},
		Edges: map[schema.EdgeKey]schema.EdgeType{},
	}
	if edgeLabel != "" {
		key := schema.EdgeKey{Source: node, KeyLabels: graph.LabelSetKey(edgeLabel), Target: node}
		sch.Edges[key] = schema.EdgeType{EdgeKey: key, Properties: map[string]schema.Property{}}
	}
	entities, index, err := codegen.PhaseZAdmit(sch, stubTypeMap{})
	require.NoError(t, err)
	names := make(map[codegen.EntityLookupKey]string, len(index))
	for key, i := range index {
		names[key] = entities[i].Name
	}
	return names
}

// TestAllCapsEdgeLabelTitleCasesPerRule3 pins spec §4.5 Rule 3's worked
// examples for an ALL-CAPS edge label — `ACTED_IN` -> `ActedIn`,
// `KNOWS` -> `Knows`. Neo4j's own convention is SCREAMING_SNAKE
// relationship types, so this is the shape a real schema hits first.
//
// The node rows are not decoration: Rule 2 pins the OPPOSITE disposition
// for a node label (`PERSON` -> `PERSON`), because a node label runs
// through §4.2's parameter mangle where preserving an acronym is the
// point ($ID, $URL). They are the regression guard for that shared
// function — the fix must reach edge labels without touching it.
func TestAllCapsEdgeLabelTitleCasesPerRule3(t *testing.T) {
	edgeCases := []struct{ label, want string }{
		{label: "KNOWS", want: "Knows"},
		{label: "ACTED_IN", want: "ActedIn"},
		{label: "LINKED", want: "Linked"},
		{label: "WORKS_FOR_NOW", want: "WorksForNow"},
		{label: "Follows", want: "Follows"},
		{label: "acted_in", want: "ActedIn"},
	}
	for _, tc := range edgeCases {
		t.Run("edge/"+tc.label, func(t *testing.T) {
			names := allCapsNameFixture(t, "Person", tc.label)
			key := codegen.EntityLookupKey{Kind: codegen.EntityEdge, EdgeKey: schema.EdgeKey{
				Source:    graph.LabelSetKey("Person"),
				KeyLabels: graph.LabelSetKey(tc.label),
				Target:    graph.LabelSetKey("Person"),
			}}
			require.Equal(t, tc.want, names[key],
				"§4.5 Rule 3: edge label %q must derive the entity struct name %q", tc.label, tc.want)
		})
	}

	nodeCases := []struct{ label, want string }{
		{label: "PERSON", want: "PERSON"},
		{label: "Person", want: "Person"},
		{label: "Person_type", want: "PersonType"},
	}
	for _, tc := range nodeCases {
		t.Run("node/"+tc.label, func(t *testing.T) {
			names := allCapsNameFixture(t, tc.label, "")
			key := codegen.EntityLookupKey{Kind: codegen.EntityNode, Labels: graph.LabelSetKey(tc.label)}
			require.Equal(t, tc.want, names[key],
				"§4.5 Rule 2: node label %q must derive the entity struct name %q", tc.label, tc.want)
		})
	}
}

// TestPhaseAAdmitNamesTheByteItRefuses pins each refusal's MESSAGE, which the
// fence below cannot: that test sorts a candidate into a bucket and then asks
// only for ErrOutOfC6Scope, so a refusal naming the wrong byte — or one arm's
// message emitted from another arm — passes it unchanged.
//
// The message is the whole of the remedy for the two unparseable bytes. What
// the user is told without one is a go/format failure against a file they did
// not write: `format failure: q.cypher.go: 14:8: illegal character NUL`,
// measured 2026-09-03 by driving cmd/gqlc over a query carrying one. That
// names a line in the GENERATED file and neither the query, the source byte,
// nor where in the .cypher file it came from.
func TestPhaseAAdmitNamesTheByteItRefuses(t *testing.T) {
	for _, tc := range []struct {
		name string
		mid  string
		want string
	}{
		{"backtick", "`", `has a backtick in its source text`},
		{"carriage return", "\r", `has a carriage return in its source text`},
		{"NUL", "\x00", `has a NUL in its source text`},
		{"lone invalid byte", "\xff", `is not valid UTF-8`},
		{"truncated UTF-8 sequence", "\xc3(", `is not valid UTF-8`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := codegen.Prepare(codegen.Input{
				Queries: []codegen.NamedQuery{{
					Name:        "Q",
					Cardinality: queryfile.CardinalityExec,
					SourceText:  "MATCH (p:Person)" + tc.mid + "RETURN 1",
				}},
			}, stubTypeMap{}, "p")

			require.ErrorIs(t, err, codegen.ErrOutOfC6Scope)
			require.EqualError(t, err, `out of C6 scope: query "Q" at position 0 `+tc.want)
		})
	}
}

// TestARawStringLiteralChangesNoTextPrepareAdmits is the fence over the four
// refusals above it. Both backends emit SourceText through a Go RAW string
// literal, and the byte that hurts is not the one such a literal cannot hold
// but the one it holds DIFFERENTLY: a carriage return is dropped from the
// literal's VALUE, so the emission compiles and the constant is not the query
// that was parsed and resolved (bd gqlc-7f9a). Naming bytes in phaseAAdmit is
// a claim about a SET, so the set is what this measures — a third such byte
// would pass the guard and every fixture alike, because no fixture can be
// written for a byte nobody has thought of.
//
// Each candidate is emitted into a raw literal and read back the way the
// compiler would, which sorts it into exactly one bucket:
//
//	carried  — parses and round-trips. Prepare must ADMIT it, or the guard
//	           refuses texts nothing justifies refusing.
//	changed  — parses, and the value differs. Prepare must REFUSE it, or a
//	           corrupt query ships silently. \r is the only member, and this
//	           is the bucket it was admitted from.
//	unparsed — the emission is not Go at all. Nothing ships, so this is loud
//	           rather than wrong and the refusal is a courtesy: every member
//	           now gets one (bd gqlc-32n53). Pinned by name AND by that
//	           answer, so both a byte joining the bucket and a refusal
//	           leaving one already in it fail here.
func TestARawStringLiteralChangesNoTextPrepareAdmits(t *testing.T) {
	// Emission does not parse → whether Prepare refuses it. NUL is a
	// documented implementation restriction on Go source; the last two are a
	// lone invalid byte and a truncated sequence, since Go source must be
	// UTF-8. All four are refused, so this map no longer distinguishes
	// members — it is the roster the sweep checks it reached, and the
	// per-byte MESSAGES are pinned by the test above.
	unparseable := map[string]bool{"`": true, "\x00": true, "\xff": true, "\xc3(": true}

	var carried, changed, unparsed int
	for _, mid := range rawLiteralCandidates() {
		text := "MATCH (p:Person)" + mid + "RETURN 1"

		emitted, parses := rawLiteralValue(text)
		_, err := codegen.Prepare(codegen.Input{
			Queries: []codegen.NamedQuery{{
				Name:        "Q",
				Cardinality: queryfile.CardinalityExec,
				SourceText:  text,
			}},
		}, stubTypeMap{}, "p")

		switch {
		case !parses:
			unparsed++
			refused, pinned := unparseable[mid]
			require.True(t, pinned,
				"emitting %q yields Go the parser rejects and this test does not name it — refuse it in "+
					"phaseAAdmit, or pin it here with a bead (bd gqlc-32n53)", mid)
			if refused {
				require.ErrorIs(t, err, codegen.ErrOutOfC6Scope, "%q is pinned as refused but Prepare admits it", mid)
			} else {
				require.NoError(t, err, "%q is pinned as admitted but Prepare refuses it; move it in the map (bd gqlc-32n53)", mid)
			}
		case emitted != text:
			changed++
			require.ErrorIs(t, err, codegen.ErrOutOfC6Scope,
				"a raw string literal silently changes %q, so the emitted constant would not be the query that "+
					"was parsed and resolved, yet Prepare admits it", mid)
		default:
			carried++
			require.NoError(t, err, "a raw string literal carries %q unchanged, so nothing justifies refusing it", mid)
		}
	}

	// A degenerate sweep — every candidate in one bucket — asserts nothing
	// while passing, so each bucket is pinned occupied.
	require.NotZero(t, carried, "no candidate round-tripped; the harness is not reaching the emission")
	require.Equal(t, 1, changed, "the carriage return should be the only byte a raw literal silently changes")
	require.Len(t, unparseable, unparsed, "every pinned unparseable byte should have been reached")
}

// rawLiteralCandidates is every ASCII rune plus the non-ASCII shapes a raw
// literal or the Go source encoding could treat specially: an invalid byte, a
// truncated UTF-8 sequence, an accented rune, an astral rune, and a
// zero-width space.
func rawLiteralCandidates() []string {
	out := make([]string, 0, 0x80+5)
	for r := rune(0); r < 0x80; r++ {
		out = append(out, string(r))
	}
	return append(out, "\xff", "\xc3(", "é", "\U0001F600", "\u200b")
}

// rawLiteralValue emits text into a raw string literal, parses the result, and
// returns the constant's value together with whether the emission was Go at
// all. It reads the literal back through go/parser rather than by string
// surgery so the answer is the one the compiler would give.
func rawLiteralValue(text string) (string, bool) {
	src := "package p\n\nconst x = `" + text + "`\n"
	f, err := parser.ParseFile(token.NewFileSet(), "p.go", src, 0)
	if err != nil {
		return "", false
	}
	for _, d := range f.Decls {
		g, ok := d.(*ast.GenDecl)
		if !ok || g.Tok != token.CONST {
			continue
		}
		lit, ok := g.Specs[0].(*ast.ValueSpec).Values[0].(*ast.BasicLit)
		if !ok {
			return "", false
		}
		val, err := strconv.Unquote(lit.Value)
		if err != nil {
			return "", false
		}
		return val, true
	}
	return "", false
}

// unknownWidthTypeMap refuses one width outright and admits the rest,
// which is what a real dialect table does with a width it has no case
// for: neo4j's Property falls off the end of its switch and returns
// ok=false (internal/codegen/neo4j/types.go:30). stubTypeMap admits
// every width but DECIMAL, so without this wrapper the rows below could
// only measure the permissive answer, and the ordering claim — that the
// kind question is asked BEFORE the carrier question — would have
// nothing to bite on.
type unknownWidthTypeMap struct {
	stubTypeMap
	refuse graph.PropertyType
}

func (m unknownWidthTypeMap) Property(pt graph.PropertyType) (string, bool) {
	if pt == m.refuse {
		return "", false
	}
	return m.stubTypeMap.Property(pt)
}

// TestUnimplementedKindRefusedBeforeTheCarrierQuestion pins the refusal
// gqlc-h9n.33 owes the rest of the pipeline. The schema front end now
// resolves RECORD and closed dynamic unions to a PropertyType carrying
// its parts, and no backend emits either kind, so every position that
// asks a TypeMap for a carrier has to refuse the kind first.
//
// Two different things go wrong without that, and they need separate
// typeMaps to tell apart.
//
// Under a dialect table the kind reaches a switch with no case for it
// and leaves as ok=false, so the caller is told ErrUnrepresentableWidth
// — that the backend has no Go type WIDE enough, which sends them to
// change the declared width. There is no width to change: the answer is
// that gqlc has built no emission for the kind at all. Every row's
// NotErrorIs is what witnesses that, and it is the reason the refusal
// cannot simply be left to the tables.
//
// Under a permissive table the kind is not refused at all — stubTypeMap
// answers "property:RECORD<a INT32>", and generation would emit that
// string as a Go type. So the refusal must not depend on a table
// refusing, which is a claim only a table that admits everything can
// make.
//
// The list rows are the shallow-walk falsifier: a check that asks
// Kind() at the top and stops admits LIST<RECORD<…>>, whose record then
// dies inside the table as a width error — the exact confusion this
// test exists to forbid, one level down and invisible to every other
// row.
func TestUnimplementedKindRefusedBeforeTheCarrierQuestion(t *testing.T) {
	entities, index := listPlanTestFixture(t)
	person := graph.LabelSetKey("Person")

	record := graph.RecordOf([]graph.RecordField{{Name: "a", Type: graph.TypeInt32}})
	union := graph.UnionOf([]graph.UnionMember{
		{Type: graph.TypeInt32},
		{Type: graph.TypeString},
	})

	// Every position that asks a TypeMap for a property carrier. The
	// four are prepare.go's entity-property, query-column,
	// query-parameter and list-element sweeps; a fifth appearing later
	// with no row here is a hole this test cannot see, so a new
	// tm.Property call site owes this table an entry.
	positions := []struct {
		name string
		run  func(pt graph.PropertyType, tm codegen.TypeMap) error
	}{{
		name: "entity property",
		run: func(pt graph.PropertyType, tm codegen.TypeMap) error {
			_, _, err := codegen.PhaseZAdmit(schema.Schema{
				Name: "Test",
				Nodes: map[graph.LabelSetKey]schema.NodeType{
					person: {KeyLabels: person, CompleteLabels: person, Properties: map[string]schema.Property{
						"p": {Name: "p", Type: pt},
					}},
				},
			}, tm)
			return err
		},
	}, {
		name: "query column",
		run: func(pt graph.PropertyType, tm codegen.TypeMap) error {
			_, err := codegen.Prepare(codegen.Input{
				Schema: schema.Schema{Name: "Test"},
				Queries: []codegen.NamedQuery{{
					Name:        "GetP",
					Cardinality: queryfile.CardinalityOne,
					SourceText:  "MATCH (n) RETURN n.p AS p",
					Validated: resolver.ValidatedQuery{
						Columns: []resolver.Column{{Name: "p", Type: resolver.ResolvedProperty{Type: pt}}},
					},
				}},
			}, tm, "")
			return err
		},
	}, {
		name: "query parameter",
		run: func(pt graph.PropertyType, tm codegen.TypeMap) error {
			_, err := codegen.Prepare(codegen.Input{
				Schema: schema.Schema{Name: "Test"},
				Queries: []codegen.NamedQuery{{
					Name:        "GetP",
					Cardinality: queryfile.CardinalityOne,
					SourceText:  "MATCH (n) WHERE n.p = $p RETURN n.q AS q",
					Validated: resolver.ValidatedQuery{
						Columns:    []resolver.Column{{Name: "q", Type: resolver.ResolvedProperty{Type: graph.TypeInt32}}},
						Parameters: []resolver.ResolvedParameter{{Name: "p", Type: resolver.ResolvedProperty{Type: pt}}},
					},
				}},
			}, tm, "")
			return err
		},
	}, {
		name: "list element",
		run: func(pt graph.PropertyType, tm codegen.TypeMap) error {
			_, err := codegen.BuildListElemPlan(resolver.ResolvedProperty{Type: pt}, entities, index, tm, -1, "")
			return err
		},
	}}

	refused := []struct {
		name string
		pt   graph.PropertyType
	}{
		{"a record with fields", record},
		{"a record whose fields are undeclared", graph.TypeAnyRecord},
		{"a record with no fields", graph.RecordOf(nil)},
		{"a union", union},
		{"a record under a list", graph.ListOf(record, true)},
		{"a union under a list", graph.ListOf(union, true)},
		{"a record under two lists", graph.ListOf(graph.ListOf(record, true), true)},
	}

	tables := []struct {
		name string
		of   func(pt graph.PropertyType) codegen.TypeMap
	}{{
		// The dialect's answer. Asserts ORDER: the kind is refused
		// before the table is asked, so moving the walk below
		// tm.Property reds every row here.
		name: "a table with no case for the width",
		of:   func(pt graph.PropertyType) codegen.TypeMap { return unknownWidthTypeMap{refuse: pt} },
	}, {
		// Asserts INDEPENDENCE: the refusal is gqlc's own and does
		// not borrow the table's. Deleting the walk entirely leaves
		// these rows generating a Go type spelled
		// "property:RECORD<a INT32>".
		name: "a table that admits every width",
		of:   func(graph.PropertyType) codegen.TypeMap { return stubTypeMap{} },
	}}

	for _, pos := range positions {
		for _, tbl := range tables {
			for _, r := range refused {
				t.Run(pos.name+"/"+tbl.name+"/"+r.name, func(t *testing.T) {
					err := pos.run(r.pt, tbl.of(r.pt))
					require.ErrorIs(t, err, codegen.ErrUnimplementedTypeKind)
					require.NotErrorIs(t, err, codegen.ErrUnrepresentableWidth,
						"the caller has no width to change; reporting the carrier gap sends them to an edit that cannot help")
				})
			}
		}
	}

	// The over-refusal fence. A walk that answered "not a scalar" rather
	// than "a record or a union" refuses these too, and every row above
	// stays green while lists stop generating.
	admitted := []struct {
		name string
		pt   graph.PropertyType
	}{
		{"a scalar", graph.TypeInt32},
		{"a list of scalars", graph.ListOf(graph.TypeInt32, true)},
		{"a list of lists of scalars", graph.ListOf(graph.ListOf(graph.TypeInt32, true), true)},
	}
	for _, pos := range positions {
		for _, a := range admitted {
			t.Run(pos.name+"/admitted/"+a.name, func(t *testing.T) {
				require.NoError(t, pos.run(a.pt, stubTypeMap{}))
			})
		}
	}
}

// TestUnimplementedTypeKindNamesTheKindAndTheSite holds the message to
// the two things a reader needs from it: WHICH kind stopped generation,
// and where it was declared.
//
// The nested row is the one that costs anything. There the declared
// width and the unbuilt kind are different strings, and a message naming
// only one leaves the reader guessing which level to edit — the list
// they can see in the schema, or the record inside it they cannot. The
// bare row is the other branch of the same rendering, and it is here
// because a message that appended `whose RECORD<a INT32> has no
// emission` to a property already declared as exactly that would be
// noise no reader gains from.
func TestUnimplementedTypeKindNamesTheKindAndTheSite(t *testing.T) {
	person := graph.LabelSetKey("Person")
	record := graph.RecordOf([]graph.RecordField{{Name: "a", Type: graph.TypeInt32}})

	tests := []struct {
		name    string
		pt      graph.PropertyType
		wantErr string
	}{{
		name:    "the property is the unbuilt kind",
		pt:      record,
		wantErr: `property type kind not implemented yet: entity "Person" property "p" has ` + string(record),
	}, {
		name: "the unbuilt kind is inside the declared width",
		pt:   graph.ListOf(record, true),
		wantErr: `property type kind not implemented yet: entity "Person" property "p" has ` +
			string(graph.ListOf(record, true)) + `, whose ` + string(record) + ` has no emission`,
	}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := codegen.PhaseZAdmit(schema.Schema{
				Name: "Test",
				Nodes: map[graph.LabelSetKey]schema.NodeType{
					person: {KeyLabels: person, CompleteLabels: person, Properties: map[string]schema.Property{
						"p": {Name: "p", Type: tt.pt},
					}},
				},
			}, stubTypeMap{})
			require.ErrorIs(t, err, codegen.ErrUnimplementedTypeKind)
			require.EqualError(t, err, tt.wantErr)
		})
	}
}
