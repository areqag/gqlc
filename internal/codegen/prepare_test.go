package codegen

import (
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"path/filepath"
	"slices"
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
		goType, ok := tm.Temporal(r.k)
		require.True(t, ok, "the stub table carries every temporal kind")
		positive = append(positive, positiveRow{
			name: r.name,
			in:   resolver.ResolvedTemporal{Kind: r.k},
			want: wantPlan{Kind: ColumnTemporal, GoType: goType},
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
			in := Input{
				Schema: sch,
				Queries: []NamedQuery{{
					Name:        "GetAction",
					Cardinality: CardinalityOne,
					SourceText:  "MATCH (x:Person)-[r:LIKES|WROTE]-(y:Post) RETURN r",
					Validated:   resolver.ValidatedQuery{Columns: []resolver.Column{{Name: "r", Type: tt.column}}},
				}},
			}
			_, err := Prepare(in, stubTypeMap{}, "")
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, ErrUnrepresentableEdgeUnion)
			require.EqualError(t, err, tt.wantErr)
		})
	}
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
			in := Input{
				Schema: schema.Schema{Name: "Test"},
				Queries: []NamedQuery{{
					Name:        "GetWhen",
					Cardinality: CardinalityOne,
					SourceText:  "MATCH (n) RETURN duration({days: 1}) AS t",
					Validated:   resolver.ValidatedQuery{Columns: []resolver.Column{{Name: "t", Type: tt.column}}},
				}},
			}
			tm := partialTemporalTypeMap{refuse: refused}
			prepared, err := Prepare(in, tm, "")
			if tt.wantErr == "" {
				require.NoError(t, err)
				require.NotEmpty(t, prepared.Queries[0].RowFields[0].GoType)
				return
			}
			require.ErrorIs(t, err, ErrUnrepresentableTemporal)
			require.EqualError(t, err, tt.wantErr)
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
var reservedIdentifierRows = []struct {
	name       string
	scope      identifierScope
	declaredBy []string // nil means every target the corpus emits
}{
	{"Queries", scopePackage, nil},
	{"New", scopePackage, nil},
	{"WithTx", scopeMethod, nil},
	{"ReadQuerier", scopePackage, nil},
	{"WriteQuerier", scopePackage, nil},
	{"Querier", scopePackage, nil},
	{"ErrNoRows", scopePackage, nil},
	{"ErrMultipleResults", scopePackage, nil},
	{"DBTX", scopePackage, ageOnlyTargets},
	{"SessionInit", scopePackage, ageOnlyTargets},
	{"EnsureGraph", scopeMethod, ageOnlyTargets},
	{"DropGraph", scopeMethod, ageOnlyTargets},
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
	got := make([]string, 0, len(reservedIdentifiers))
	for name := range reservedIdentifiers {
		got = append(got, name)
	}
	require.ElementsMatch(t, want, got)

	for _, row := range reservedIdentifierRows {
		t.Run(row.name, func(t *testing.T) {
			require.Equal(t, row.scope, reservedIdentifiers[row.name])

			in := Input{
				Schema:  schema.Schema{Name: "Test"},
				Queries: []NamedQuery{{Name: row.name, Cardinality: CardinalityExec, SourceText: "MATCH (n) DELETE n"}},
			}
			_, err := Prepare(in, stubTypeMap{}, "")
			require.ErrorIs(t, err, ErrIdentifierCollision)
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
		{"edge", func(name string) schema.Schema {
			key := schema.EdgeKey{Source: person, KeyLabels: graph.LabelSetKey(name), Target: person}
			return schema.Schema{
				Name: "Test",
				Nodes: map[graph.LabelSetKey]schema.NodeType{
					person: {KeyLabels: person, CompleteLabels: person, Properties: map[string]schema.Property{}},
				},
				Edges: map[schema.EdgeKey]schema.EdgeType{
					key: {EdgeKey: key, Properties: map[string]schema.Property{}},
				},
			}
		}},
	}

	for _, src := range sources {
		for _, row := range reservedIdentifierRows {
			t.Run(src.axis+"/"+row.name, func(t *testing.T) {
				prepared, err := Prepare(Input{Schema: src.schema(row.name)}, stubTypeMap{}, "")
				if row.scope == scopeMethod {
					require.NoError(t, err)
					names := make([]string, 0, len(prepared.Entities))
					for _, e := range prepared.Entities {
						names = append(names, e.Name)
					}
					require.Contains(t, names, row.name)
					return
				}
				require.ErrorIs(t, err, ErrIdentifierCollision)
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
func scopeName(s identifierScope) string {
	if s == scopeMethod {
		return "scopeMethod"
	}
	return "scopePackage"
}

// TestReservedScopeMatchesTheEmittedGoldens holds both table columns to
// the corpus rather than to a claim about the templates. Three checks:
//
//  1. every declaration of a reserved name sits at the scope the table
//     records;
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
// receiver type is not consulted: what sweepIdentifiers needs to know is
// only whether an entity struct of the same name would redeclare it.
func TestReservedScopeMatchesTheEmittedGoldens(t *testing.T) {
	paths, err := filepath.Glob(goldenCorpusGlob)
	require.NoError(t, err)
	require.NotEmpty(t, paths, "no golden Go under %s, so this sweep holds nothing", goldenCorpusGlob)

	corpusTargets := map[string]struct{}{}
	// name -> scope -> one path declaring it that way, for the fail message.
	declared := make(map[string]map[identifierScope]string)
	// name -> set of targets declaring it.
	declaredBy := make(map[string]map[string]struct{})
	record := func(name, path string, scope identifierScope) {
		if _, reserved := reservedIdentifiers[name]; !reserved {
			return
		}
		if declared[name] == nil {
			declared[name] = make(map[identifierScope]string)
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
		for _, decl := range file.Decls {
			switch d := decl.(type) {
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					switch s := spec.(type) {
					case *ast.TypeSpec:
						record(s.Name.Name, path, scopePackage)
					case *ast.ValueSpec:
						for _, n := range s.Names {
							record(n.Name, path, scopePackage)
						}
					}
				}
			case *ast.FuncDecl:
				scope := scopeMethod
				if d.Recv == nil {
					scope = scopePackage
				}
				record(d.Name.Name, path, scope)
			}
		}
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
			for scope, path := range at {
				require.Equal(t, row.scope, scope,
					"%s declares %q at %s, but the reserved set records %s",
					path, row.name, scopeName(scope), scopeName(row.scope))
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
// declarations are the emitter's own and the same for every batch: the
// handle and its seam in db.go, the Apache AGE graph lifecycle in
// graph.go, the three interfaces in querier.go. Every other emitted file
// declares names derived from the input — entity structs, query methods,
// Params, Row, edge-union interfaces — which are sources 1-6 of the
// sweep, not source 0.
var fixedDeclarationFiles = map[string]bool{"db.go": true, "graph.go": true, "querier.go": true}

// TestEveryEmittedFixedDeclarationIsReserved is the direction
// TestReservedScopeMatchesTheEmittedGoldens does not run. That one takes
// each reserved row and finds its declaration; this one takes each
// exported declaration in fixedDeclarationFiles and requires a row for
// it. Without it the set can be complete today and quietly stop being
// complete: an emitter that grows a new exported package-level
// declaration leaves every other guard here green, and `NODE TYPE
// <thatName>` then emits a package that does not compile — the defect
// gqlc-e6mh closed, reopened by addition rather than by edit.
//
// Membership is all this asserts. The scope column is held by the sweep
// above, which covers any name once it is a row, so repeating the scope
// check here would only duplicate its fail. A method is recorded
// whatever its receiver, so one on a receiver other than *Queries — the
// corpus carries none — would be forced into reservedIdentifiers, where
// Phase A refuses a query on membership alone.
func TestEveryEmittedFixedDeclarationIsReserved(t *testing.T) {
	paths, err := filepath.Glob(goldenCorpusGlob)
	require.NoError(t, err)
	require.NotEmpty(t, paths, "no golden Go under %s, so this sweep holds nothing", goldenCorpusGlob)
	require.NotEmpty(t, fixedDeclarationFiles,
		"fixedDeclarationFiles names no file, so the sweep reads nothing and agrees with every possible reserved set")

	// name -> one path declaring it, for the fail message.
	found := map[string]string{}
	// basename -> goldens read under it. Per entry rather than in
	// aggregate: one counter over the whole set stays non-zero while any
	// one entry matches nothing, so a stale name goes unread in silence.
	swept := map[string]int{}
	fset := token.NewFileSet()
	for _, path := range paths {
		base := filepath.Base(path)
		if !fixedDeclarationFiles[base] {
			continue
		}
		swept[base]++
		file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		require.NoError(t, err, "parsing %s", path)
		record := func(name string) {
			if !ast.IsExported(name) {
				return
			}
			if _, seen := found[name]; !seen {
				found[name] = path
			}
		}
		for _, decl := range file.Decls {
			switch d := decl.(type) {
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					switch s := spec.(type) {
					case *ast.TypeSpec:
						record(s.Name.Name)
					case *ast.ValueSpec:
						for _, n := range s.Names {
							record(n.Name)
						}
					}
				}
			case *ast.FuncDecl:
				record(d.Name.Name)
			}
		}
	}
	for _, name := range slices.Sorted(maps.Keys(fixedDeclarationFiles)) {
		require.NotZero(t, swept[name],
			"no golden under %s is named %s, so every exported declaration that file emits went unread; either the emitter renamed it and fixedDeclarationFiles kept the old name, or the corpus lost every fixture emitting it",
			goldenCorpusGlob, name)
	}

	for name, path := range found {
		_, reserved := reservedIdentifiers[name]
		require.True(t, reserved,
			"%s declares exported %q, which reservedIdentifiers does not hold; a schema element deriving that name would redeclare it and the emitted package would not compile",
			path, name)
	}
}
