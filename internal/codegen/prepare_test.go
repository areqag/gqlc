package codegen

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/graph"
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

func (stubTypeMap) Temporal(k resolver.Temporal) string { return "temporal:" + k.String() }

func (stubTypeMap) Scalar(k resolver.Scalar) string { return "scalar:" + k.String() }

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
func listPlanTestFixture(t *testing.T) ([]Entity, map[entityLookupKey]int) {
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
	entities, index, err := phaseZAdmit(sch, stubTypeMap{})
	require.NoError(t, err)
	return entities, index
}

// listPlanPersonKey returns the Person node's entityLookupKey.
func listPlanPersonKey() entityLookupKey {
	return entityLookupKey{Kind: EntityNode, Labels: graph.LabelSetKey("Person")}
}

// listPlanKnowsKey / listPlanLikesKey mirror the two edgeUnion
// candidates.
func listPlanKnowsKey() entityLookupKey {
	return entityLookupKey{Kind: EntityEdge, EdgeKey: schema.EdgeKey{
		Source:    graph.LabelSetKey("Person"),
		KeyLabels: graph.LabelSetKey("KNOWS"),
		Target:    graph.LabelSetKey("Person"),
	}}
}

func listPlanLikesKey() entityLookupKey {
	return entityLookupKey{Kind: EntityEdge, EdgeKey: schema.EdgeKey{
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
		Kind       ColumnKind
		GoType     string
		EntityName string
		UnionIdx   int
		NestedKind ColumnKind // zero if Nested nil expected
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
		kind ColumnKind
	}{
		{"scalar bool", resolver.ScalarBool, tm.Scalar(resolver.ScalarBool), ColumnScalar},
		{"scalar int", resolver.ScalarInt, tm.Scalar(resolver.ScalarInt), ColumnScalar},
		{"scalar float", resolver.ScalarFloat, tm.Scalar(resolver.ScalarFloat), ColumnScalar},
		{"scalar string", resolver.ScalarString, tm.Scalar(resolver.ScalarString), ColumnScalar},
		{"scalar null", resolver.ScalarNull, "any", ColumnScalarNull},
		{"scalar map", resolver.ScalarMap, tm.Scalar(resolver.ScalarMap), ColumnScalar},
	}

	type positiveRow struct {
		name string
		in   resolver.ResolvedType
		want wantPlan
	}
	positive := []positiveRow{{
		name: "property",
		in:   resolver.ResolvedProperty{Type: graph.TypeInt32},
		want: wantPlan{Kind: ColumnProperty, GoType: "property:INT32"},
	}}
	for _, r := range temporalRows {
		positive = append(positive, positiveRow{
			name: r.name,
			in:   resolver.ResolvedTemporal{Kind: r.k},
			want: wantPlan{Kind: ColumnTemporal, GoType: tm.Temporal(r.k)},
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
			want: wantPlan{Kind: ColumnAny, GoType: "any"},
		},
		positiveRow{
			name: "node", in: resolver.ResolvedNode{Labels: graph.LabelSetKey("Person")},
			want: wantPlan{Kind: ColumnNode, GoType: personName, EntityName: personName},
		},
		positiveRow{
			name: "edge", in: resolver.ResolvedEdge{EdgeKey: knowsKey},
			want: wantPlan{Kind: ColumnEdge, GoType: knowsName, EntityName: knowsName},
		},
		positiveRow{
			name: "edgeUnion", in: resolver.ResolvedEdgeUnion{EdgeKeys: []schema.EdgeKey{knowsKey, likesKey}},
			want: wantPlan{Kind: ColumnEdgeUnion, GoType: "PathActionsUnion", UnionIdx: 42},
		},
		positiveRow{
			name: "nested list of scalar",
			in:   resolver.ResolvedList{Element: resolver.ResolvedList{Element: resolver.ResolvedScalar{Kind: resolver.ScalarInt}}},
			want: wantPlan{
				Kind:       ColumnList,
				GoType:     "[][]" + tm.Scalar(resolver.ScalarInt),
				NestedKind: ColumnList,
				NestedGoTy: "[]" + tm.Scalar(resolver.ScalarInt),
			},
		},
	)

	require.Len(t, positive, 18, "positive table should cover every ResolvedType arm (§4.1)")

	for _, tt := range positive {
		t.Run("positive/"+tt.name, func(t *testing.T) {
			plan, err := buildListElemPlan(tt.in, entities, index, tm, tt.want.UnionIdx, tt.want.GoType)
			require.NoError(t, err)
			require.NotNil(t, plan)
			require.Equal(t, tt.want.Kind, plan.Kind, "Kind")
			require.Equal(t, tt.want.GoType, plan.GoType, "GoType")
			require.Equal(t, tt.want.EntityName, plan.EntityName, "EntityName")
			if tt.want.Kind == ColumnEdgeUnion {
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
		_, err := buildListElemPlan(resolver.ResolvedProperty{Type: graph.TypeDecimal}, entities, index, tm, -1, "")
		require.ErrorIs(t, err, ErrUnrepresentableWidth)
	})

	t.Run("negative/synthetic_malformed_variant", func(t *testing.T) {
		// Wrap ResolvedUnknown so String() promotes through cleanly;
		// the builder's type-switch cases each match a concrete
		// ResolvedType struct by name, so the outer unknownVariant
		// matches none and falls to the default arm.
		_, err := buildListElemPlan(unknownVariant{ResolvedType: resolver.ResolvedUnknown{}}, entities, index, tm, -1, "")
		require.ErrorIs(t, err, ErrOutOfC6Scope)
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
	allowed := map[ColumnKind]string{
		ColumnProperty:   "ColumnProperty",
		ColumnNode:       "ColumnNode",
		ColumnEdge:       "ColumnEdge",
		ColumnTemporal:   "ColumnTemporal",
		ColumnScalar:     "ColumnScalar",
		ColumnScalarNull: "ColumnScalarNull",
		ColumnList:       "ColumnList",
		ColumnAny:        "ColumnAny",
		ColumnEdgeUnion:  "ColumnEdgeUnion",
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
	seen := map[ColumnKind]struct{}{}
	for _, s := range samples {
		plan, err := buildListElemPlan(s, entities, index, stubTypeMap{}, 0, "PathActionsUnion")
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
			q := NamedQuery{
				Name:        "Q",
				Cardinality: CardinalityExec,
				SourceText:  "MATCH (n) DELETE n",
				Validated: resolver.ValidatedQuery{
					Statement: tt.statement,
				},
			}
			out, err := phaseBDerive([]NamedQuery{q}, nil, nil, stubTypeMap{})
			require.NoError(t, err)
			require.Len(t, out, 1)
			require.Equal(t, tt.want, out[0].IsWrite)
		})
	}
}

// TestReservedIdentifiersAreUniformAcrossBackends pins the reserved set
// (spec §4.1) as a whole: every exported name a generated package
// declares at package scope collides, whichever backend is selected.
// DBTX, SessionInit and the graph lifecycle pair are declared only by
// the Apache AGE emission, but a name that is free on one backend and
// taken on another is the renaming scheme D2 refused — so the set stays
// uniform.
func TestReservedIdentifiersAreUniformAcrossBackends(t *testing.T) {
	want := []string{
		"Queries", "New", "WithTx",
		"ReadQuerier", "WriteQuerier", "Querier",
		"ErrNoRows", "ErrMultipleResults",
		"DBTX", "SessionInit", "EnsureGraph", "DropGraph",
	}
	got := make([]string, 0, len(reservedIdentifiers))
	for name := range reservedIdentifiers {
		got = append(got, name)
	}
	require.ElementsMatch(t, want, got)

	for _, name := range want {
		t.Run(name, func(t *testing.T) {
			in := Input{
				Schema:  schema.Schema{Name: "Test"},
				Queries: []NamedQuery{{Name: name, Cardinality: CardinalityExec, SourceText: "MATCH (n) DELETE n"}},
			}
			_, err := Prepare(in, stubTypeMap{}, "")
			require.ErrorIs(t, err, ErrIdentifierCollision)
			require.ErrorContains(t, err, `query "`+name+`" at position 0`)
		})
	}
}
