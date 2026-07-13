package codegen

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/graph"
	"github.com/areqag/gqlc/internal/resolver"
	"github.com/areqag/gqlc/internal/schema"
)

// TestPhaseBCommitsAccessMode asserts that phaseBDerive commits the
// access mode as the prepare-side closed enum (spec §1.1). One row per
// StatementKind value; every future StatementKind addition must extend
// the table.
func TestPhaseBCommitsAccessMode(t *testing.T) {
	tests := []struct {
		name      string
		statement resolver.StatementKind
		want      accessMode
	}{
		{"read maps to accessModeRead", resolver.StatementRead, accessModeRead},
		{"write maps to accessModeWrite", resolver.StatementWrite, accessModeWrite},
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
			out, err := phaseBDerive([]NamedQuery{q}, nil, nil)
			require.NoError(t, err)
			require.Len(t, out, 1)
			require.Equal(t, tt.want, out[0].AccessMode)
		})
	}
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
func listPlanTestFixture(t *testing.T) ([]preparedEntity, map[entityLookupKey]int) {
	t.Helper()
	person := graph.LabelSetKey("Person")
	knows := schema.EdgeKey{Source: person, Label: graph.LabelSetKey("KNOWS"), Target: person}
	likes := schema.EdgeKey{Source: person, Label: graph.LabelSetKey("LIKES"), Target: person}
	sch := schema.Schema{
		Name: "Test",
		Nodes: map[graph.LabelSetKey]schema.NodeType{
			person: {Labels: person, Properties: map[string]schema.Property{}},
		},
		Edges: map[schema.EdgeKey]schema.EdgeType{
			knows: {EdgeKey: knows, Properties: map[string]schema.Property{}},
			likes: {EdgeKey: likes, Properties: map[string]schema.Property{}},
		},
	}
	entities, index, err := phaseZAdmit(sch)
	require.NoError(t, err)
	return entities, index
}

// listPlanPersonKey returns the Person node's entityLookupKey.
func listPlanPersonKey() entityLookupKey {
	return entityLookupKey{Kind: entityNode, Labels: graph.LabelSetKey("Person")}
}

// listPlanKnowsKey / listPlanLikesKey mirror the two edgeUnion
// candidates.
func listPlanKnowsKey() entityLookupKey {
	return entityLookupKey{Kind: entityEdge, EdgeKey: schema.EdgeKey{
		Source: graph.LabelSetKey("Person"),
		Label:  graph.LabelSetKey("KNOWS"),
		Target: graph.LabelSetKey("Person"),
	}}
}

func listPlanLikesKey() entityLookupKey {
	return entityLookupKey{Kind: entityEdge, EdgeKey: schema.EdgeKey{
		Source: graph.LabelSetKey("Person"),
		Label:  graph.LabelSetKey("LIKES"),
		Target: graph.LabelSetKey("Person"),
	}}
}

// TestPhaseBCommitsListElemPlan is the mapping-table unit test the
// deepening acceptance criteria call for (spec §4.1, gqlc-ls8.3). 34
// positive rows exercise every arm of the ResolvedType sum: 17
// representable property widths, 6 temporal kinds, 6 scalar kinds
// (with ScalarNull splitting off to columnScalarNull), 1 Unknown, 1
// Node, 1 Edge, 1 EdgeUnion, 1 nested-list-of-int64. 9 negative rows
// exercise the failure fence: 8 unrepresentable property widths each
// expected to route through ErrUnrepresentableWidth, plus 1 synthetic
// malformed-variant row asserting a foreign ResolvedType returns
// ErrOutOfC6Scope rather than silent success. Total: 43 rows.
func TestPhaseBCommitsListElemPlan(t *testing.T) {
	entities, index := listPlanTestFixture(t)
	knowsKey := schema.EdgeKey{
		Source: graph.LabelSetKey("Person"),
		Label:  graph.LabelSetKey("KNOWS"),
		Target: graph.LabelSetKey("Person"),
	}
	likesKey := schema.EdgeKey{
		Source: graph.LabelSetKey("Person"),
		Label:  graph.LabelSetKey("LIKES"),
		Target: graph.LabelSetKey("Person"),
	}
	personName := entities[index[listPlanPersonKey()]].Name
	knowsName := entities[index[listPlanKnowsKey()]].Name
	_ = entities[index[listPlanLikesKey()]].Name // fixture presence check; EdgeUnion arm asserts only against the interface name

	type wantPlan struct {
		Kind        columnKind
		GoType      string
		Carrier     string
		UsesConvert bool
		EntityName  string
		UnionIdx    int
		NestedKind  columnKind // zero if Nested nil expected
		NestedGoTy  string
	}

	// 17 representable property widths per internal/graph/propertytype.go.
	propRows := []struct {
		name    string
		pt      graph.PropertyType
		goType  string
		carrier string
		convert bool
	}{
		{"string", graph.TypeString, "string", "", false},
		{"bool", graph.TypeBool, "bool", "", false},
		{"int", graph.TypeInt, "int", "int64", true},
		{"int8", graph.TypeInt8, "int8", "int64", true},
		{"int16", graph.TypeInt16, "int16", "int64", true},
		{"int32", graph.TypeInt32, "int32", "int64", true},
		{"int64", graph.TypeInt64, "int64", "", false},
		{"uint", graph.TypeUint, "uint", "int64", true},
		{"uint8", graph.TypeUint8, "uint8", "int64", true},
		{"uint16", graph.TypeUint16, "uint16", "int64", true},
		{"uint32", graph.TypeUint32, "uint32", "int64", true},
		{"uint64", graph.TypeUint64, "uint64", "int64", true},
		{"float", graph.TypeFloat, "float64", "", false},
		{"float32", graph.TypeFloat32, "float32", "float64", true},
		{"float64", graph.TypeFloat64, "float64", "", false},
		{"date", graph.TypeDate, "dbtype.Date", "", false},
		{"timestamp", graph.TypeTimestamp, "time.Time", "", false},
	}

	// 6 temporal kinds.
	temporalRows := []struct {
		name string
		k    resolver.Temporal
		gt   string
	}{
		{"temporal date", resolver.TemporalDate, "dbtype.Date"},
		{"temporal time", resolver.TemporalTime, "dbtype.Time"},
		{"temporal localtime", resolver.TemporalLocalTime, "dbtype.LocalTime"},
		{"temporal datetime", resolver.TemporalDateTime, "time.Time"},
		{"temporal localdatetime", resolver.TemporalLocalDateTime, "dbtype.LocalDateTime"},
		{"temporal duration", resolver.TemporalDuration, "dbtype.Duration"},
	}

	// 6 scalar kinds. ScalarNull splits to columnScalarNull.
	scalarRows := []struct {
		name string
		k    resolver.Scalar
		gt   string
		kind columnKind
	}{
		{"scalar bool", resolver.ScalarBool, "bool", columnScalar},
		{"scalar int", resolver.ScalarInt, "int64", columnScalar},
		{"scalar float", resolver.ScalarFloat, "float64", columnScalar},
		{"scalar string", resolver.ScalarString, "string", columnScalar},
		{"scalar null", resolver.ScalarNull, "any", columnScalarNull},
		{"scalar map", resolver.ScalarMap, "map[string]any", columnScalar},
	}

	// Positive rows: 17 + 6 + 6 + 1 + 1 + 1 + 1 + 1 = 34.
	positive := []struct {
		name string
		in   resolver.ResolvedType
		want wantPlan
	}{}
	for _, r := range propRows {
		positive = append(positive, struct {
			name string
			in   resolver.ResolvedType
			want wantPlan
		}{
			name: "property " + r.name,
			in:   resolver.ResolvedProperty{Type: r.pt},
			want: wantPlan{Kind: columnProperty, GoType: r.goType, Carrier: r.carrier, UsesConvert: r.convert},
		})
	}
	for _, r := range temporalRows {
		positive = append(positive, struct {
			name string
			in   resolver.ResolvedType
			want wantPlan
		}{name: r.name, in: resolver.ResolvedTemporal{Kind: r.k}, want: wantPlan{Kind: columnTemporal, GoType: r.gt}})
	}
	for _, r := range scalarRows {
		positive = append(positive, struct {
			name string
			in   resolver.ResolvedType
			want wantPlan
		}{name: r.name, in: resolver.ResolvedScalar{Kind: r.k}, want: wantPlan{Kind: r.kind, GoType: r.gt}})
	}
	positive = append(positive,
		struct {
			name string
			in   resolver.ResolvedType
			want wantPlan
		}{
			name: "unknown", in: resolver.ResolvedUnknown{},
			want: wantPlan{Kind: columnAny, GoType: "any"},
		},
		struct {
			name string
			in   resolver.ResolvedType
			want wantPlan
		}{
			name: "node", in: resolver.ResolvedNode{Labels: graph.LabelSetKey("Person")},
			want: wantPlan{Kind: columnNode, GoType: personName, EntityName: personName},
		},
		struct {
			name string
			in   resolver.ResolvedType
			want wantPlan
		}{
			name: "edge", in: resolver.ResolvedEdge{EdgeKey: knowsKey},
			want: wantPlan{Kind: columnEdge, GoType: knowsName, EntityName: knowsName},
		},
		struct {
			name string
			in   resolver.ResolvedType
			want wantPlan
		}{
			name: "edgeUnion", in: resolver.ResolvedEdgeUnion{EdgeKeys: []schema.EdgeKey{knowsKey, likesKey}},
			want: wantPlan{Kind: columnEdgeUnion, GoType: "PathActionsUnion", UnionIdx: 42},
		},
		struct {
			name string
			in   resolver.ResolvedType
			want wantPlan
		}{
			name: "nested list of int64",
			in:   resolver.ResolvedList{Element: resolver.ResolvedList{Element: resolver.ResolvedScalar{Kind: resolver.ScalarInt}}},
			want: wantPlan{Kind: columnList, GoType: "[][]int64", NestedKind: columnList, NestedGoTy: "[]int64"},
		},
	)

	require.Len(t, positive, 34, "positive table should have 34 rows (§4.1)")

	for _, tt := range positive {
		t.Run("positive/"+tt.name, func(t *testing.T) {
			plan, err := buildListElemPlan(tt.in, entities, index, tt.want.UnionIdx, tt.want.GoType)
			require.NoError(t, err)
			require.NotNil(t, plan)
			require.Equal(t, tt.want.Kind, plan.Kind, "Kind")
			require.Equal(t, tt.want.GoType, plan.GoType, "GoType")
			require.Equal(t, tt.want.Carrier, plan.Carrier, "Carrier")
			require.Equal(t, tt.want.UsesConvert, plan.UsesConvert, "UsesConvert")
			require.Equal(t, tt.want.EntityName, plan.EntityName, "EntityName")
			if tt.want.Kind == columnEdgeUnion {
				require.Equal(t, tt.want.UnionIdx, plan.UnionIdx, "UnionIdx")
			}
			if tt.want.NestedKind != 0 || tt.want.NestedGoTy != "" {
				require.NotNil(t, plan.Nested, "Nested must be non-nil for a list arm")
				require.Equal(t, tt.want.NestedKind, plan.Nested.Kind, "Nested.Kind")
				require.Equal(t, tt.want.NestedGoTy, plan.Nested.GoType, "Nested.GoType")
			}
		})
	}

	// 9 negative rows: 8 unrepresentable widths + 1 synthetic-malformed
	// variant.
	unrepresentable := []graph.PropertyType{
		graph.TypeInt128, graph.TypeInt256,
		graph.TypeUint128, graph.TypeUint256,
		graph.TypeFloat16, graph.TypeFloat128, graph.TypeFloat256,
		graph.TypeDecimal,
	}
	for _, pt := range unrepresentable {
		t.Run("negative/unrepresentable_"+string(pt), func(t *testing.T) {
			_, err := buildListElemPlan(resolver.ResolvedProperty{Type: pt}, entities, index, -1, "")
			require.ErrorIs(t, err, ErrUnrepresentableWidth)
		})
	}

	t.Run("negative/synthetic_malformed_variant", func(t *testing.T) {
		// Wrap ResolvedUnknown so String() promotes through cleanly;
		// the builder's type-switch cases each match a concrete
		// ResolvedType struct by name, so the outer unknownVariant
		// matches none and falls to the default arm.
		_, err := buildListElemPlan(unknownVariant{ResolvedType: resolver.ResolvedUnknown{}}, entities, index, -1, "")
		require.ErrorIs(t, err, ErrOutOfC6Scope)
	})
}

// TestPreparedListElemMapsToColumnKind asserts every kind value the
// plan-builder can assign is one of the nine known columnKind values
// (spec §4.1 companion test). Explicit enumeration: if a tenth
// columnKind arrives without extending the plan-builder, this test
// fails.
func TestPreparedListElemMapsToColumnKind(t *testing.T) {
	// The nine columnKind values the plan-builder may assign to
	// preparedListElem.Kind (spec §1.3, §3). columnEdgeUnion,
	// columnScalarNull, columnAny are the arms whose emission-side
	// dispatch is single-value; every other arm shares its top-level
	// meaning.
	allowed := map[columnKind]string{
		columnProperty:   "columnProperty",
		columnNode:       "columnNode",
		columnEdge:       "columnEdge",
		columnTemporal:   "columnTemporal",
		columnScalar:     "columnScalar",
		columnScalarNull: "columnScalarNull",
		columnList:       "columnList",
		columnAny:        "columnAny",
		columnEdgeUnion:  "columnEdgeUnion",
	}
	require.Len(t, allowed, 9)

	// Sample every arm through the builder and check membership.
	entities, index := listPlanTestFixture(t)
	knowsKey := schema.EdgeKey{
		Source: graph.LabelSetKey("Person"),
		Label:  graph.LabelSetKey("KNOWS"),
		Target: graph.LabelSetKey("Person"),
	}
	likesKey := schema.EdgeKey{
		Source: graph.LabelSetKey("Person"),
		Label:  graph.LabelSetKey("LIKES"),
		Target: graph.LabelSetKey("Person"),
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
	seen := map[columnKind]struct{}{}
	for _, s := range samples {
		plan, err := buildListElemPlan(s, entities, index, 0, "PathActionsUnion")
		require.NoError(t, err)
		require.NotNil(t, plan)
		_, ok := allowed[plan.Kind]
		require.True(t, ok, "plan.Kind %d not in allowed set", plan.Kind)
		seen[plan.Kind] = struct{}{}
	}
	require.Len(t, seen, 9, "every columnKind value should be reachable via buildListElemPlan; missing arms indicate a plan-builder gap")
}

// TestPhaseBCommitsIsWrite asserts that phaseBDerive commits the
// StatementWrite axis as a preparedQuery.IsWrite bool (spec §1.2). Real
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
			out, err := phaseBDerive([]NamedQuery{q}, nil, nil)
			require.NoError(t, err)
			require.Len(t, out, 1)
			require.Equal(t, tt.want, out[0].IsWrite)
		})
	}
}
