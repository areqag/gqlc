package age_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/codegen"
	"github.com/areqag/gqlc/internal/codegen/age"
	"github.com/areqag/gqlc/internal/graph"
	"github.com/areqag/gqlc/internal/resolver"
	"github.com/areqag/gqlc/internal/schema"
	"github.com/areqag/gqlc/internal/schema/gql"
)

// listColumnInput is a one-query batch over the corpus skeleton schema
// projecting the column under test. The gate these tests exercise runs
// ahead of codegen.Prepare (generate.go), so a column naming a vertex or
// an edge type the skeleton does not declare still reaches it — the
// refusal is read off the resolved column and never off the entity
// index.
func listColumnInput(t *testing.T, col resolver.Column) codegen.Input {
	t.Helper()

	src, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "data", "codegen", "valid", "skeleton", "schema.gql"))
	require.NoError(t, err)
	sch, err := gql.New().Parse(strings.NewReader(string(src)))
	require.NoError(t, err)

	return codegen.Input{
		Schema: sch,
		Queries: []codegen.NamedQuery{{
			Name:        "Tags",
			Cardinality: codegen.CardinalityMany,
			SourceFile:  "q.cypher",
			SourceText:  "MATCH (p:Person) RETURN collect(p.name) AS xs\n",
			Validated:   resolver.ValidatedQuery{Columns: []resolver.Column{col}},
		}},
	}
}

// schemaEdgeKeyAuthored is one concrete edge key, for the list element
// that is a whole edge. Its endpoints need not be declared: see
// listColumnInput.
func schemaEdgeKeyAuthored() schema.EdgeKey {
	return schema.EdgeKey{Source: "Person", KeyLabels: "AUTHORED", Target: "Post"}
}

// TestListExpressionColumnIsJudgedByItsElement pins what replaced the
// unconditional refusal of resolver.ResolvedList (bd gqlc-p6cb, GH #711).
//
// The refusal was correct when this backend had no list decode path at
// all. It has one: a schema property of list width is served at whatever
// nesting depth its element width is served at, which is what put
// agtypeList and the per-slice named wrappers in models.go. A list
// EXPRESSION column — `RETURN [1, 2, 3] AS xs`, `collect(p.name)` —
// reaches emission through a different provenance (resolver.ResolvedList,
// built by callProjectionType rather than from a schema property) and
// decodes through exactly the same wrappers, so refusing it by column
// kind refused a capability already paid for.
//
// What replaced it is a per-element judgement, so the reason an author
// gets names the ELEMENT that has no carrier rather than the fact that
// they wrote a list. The rows below are the whole of that judgement:
// each names a resolved element type and the verdict it earns. A row
// that says "generates" is checked by generating; a row that says
// refused is checked against the exact text, so widening the gate
// without widening this table reddens here.
//
// The element vocabulary is resolver.ResolvedType's, minus the two that
// cannot appear under a list expression column in this gate's hands:
// nothing here recurses into resolver.ResolvedTemporal, because the
// shared phase answers a temporal element with ErrUnrepresentableTemporal
// naming the kind, which is more than this gate can say (ADR 0025) — the
// same standing-aside the top-level temporal arm does, pinned by its own
// row below.
func TestListExpressionColumnIsJudgedByItsElement(t *testing.T) {
	t.Parallel()

	person := graph.LabelSetKey("Person")

	listOf := func(elem resolver.ResolvedType) resolver.Column {
		return resolver.Column{Name: "xs", Type: resolver.ResolvedList{Element: elem}}
	}

	cases := []struct {
		name string
		col  resolver.Column
		// wantReason is the text after `column "xs" ` in the drop
		// report, or "" when the column is served.
		wantReason string
	}{
		{
			name: "a list of string scalars generates",
			col:  listOf(resolver.ResolvedScalar{Kind: resolver.ScalarString}),
		},
		{
			name: "a list of integer scalars generates",
			col:  listOf(resolver.ResolvedScalar{Kind: resolver.ScalarInt}),
		},
		{
			name: "a list of float scalars generates",
			col:  listOf(resolver.ResolvedScalar{Kind: resolver.ScalarFloat}),
		},
		{
			name: "a list of boolean scalars generates",
			col:  listOf(resolver.ResolvedScalar{Kind: resolver.ScalarBool}),
		},
		{
			name: "a nested list of scalars generates",
			col:  listOf(resolver.ResolvedList{Element: resolver.ResolvedScalar{Kind: resolver.ScalarInt}}),
		},
		{
			name: "a list of a carried property width generates",
			col:  listOf(resolver.ResolvedProperty{Type: graph.TypeInt32}),
		},
		{
			name: "a list of values of no declared shape generates",
			col:  listOf(resolver.ResolvedUnknown{}),
		},
		{
			// The element's width, not the column kind: the author is
			// told which carrier is missing, and BYTES is the line they
			// can go and find.
			name:       "a list of an uncarried property width names the width",
			col:        listOf(resolver.ResolvedProperty{Type: graph.TypeBytes}),
			wantReason: "projects a list of property:BYTES",
		},
		{
			// An instant is served as a property and refused as a list
			// element, at every depth. The zone rides a sidecar named
			// after the property; a list has one name for all of its
			// elements, so there is nowhere to put the zone of any
			// element but the first. typeMap.Property already refuses a
			// LIST<TIMESTAMP> property for this reason, and an
			// expression list of instants is the same defect reached by
			// the other provenance.
			name:       "a list of instants is refused for want of anywhere to put the zone",
			col:        listOf(resolver.ResolvedProperty{Type: graph.TypeTimestamp}),
			wantReason: "projects a list of property:TIMESTAMP, whose zone has no sidecar to ride in",
		},
		{
			name:       "a list of null scalars names the element",
			col:        listOf(resolver.ResolvedScalar{Kind: resolver.ScalarNull}),
			wantReason: "projects a list of scalar(null)",
		},
		{
			name:       "a list of map scalars names the element",
			col:        listOf(resolver.ResolvedScalar{Kind: resolver.ScalarMap}),
			wantReason: "projects a list of scalar(map)",
		},
		{
			// A whole vertex decodes through the entity's own helper,
			// which columnDecoder reaches only for a top-level column;
			// elemDecoder has no arm for an entity struct name and
			// decodeFunc panics on one. So the element is refused here
			// rather than emitted into a package that cannot be built.
			name:       "a list of whole vertices names the element",
			col:        listOf(resolver.ResolvedNode{Labels: person}),
			wantReason: "projects a list of node",
		},
		{
			name:       "a list of whole edges names the element",
			col:        listOf(resolver.ResolvedEdge{EdgeKey: schemaEdgeKeyAuthored()}),
			wantReason: "projects a list of edge",
		},
		{
			// The nested refusal names the offending element, not the
			// outermost list: a list of lists of vertices is refused for
			// the vertex.
			name:       "a nested list names the element that has no carrier",
			col:        listOf(resolver.ResolvedList{Element: resolver.ResolvedNode{Labels: person}}),
			wantReason: "projects a list of node",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			in := listColumnInput(t, tc.col)
			files, err := age.New(age.WithPackageName("listexpr")).Generate(in)

			if tc.wantReason == "" {
				require.NoError(t, err)
				require.NotEmpty(t, files)
				return
			}
			require.Error(t, err)
			require.Nil(t, files, "a rejected batch must not return a partial file set")
			require.ErrorIs(t, err, age.ErrUnsupportedQuery)
			require.Contains(t, err.Error(), `Tags (column "xs" `+tc.wantReason+`)`)
		})
	}
}

// TestListOfEdgeUnionStaysRefused pins the one element the recursion is
// NOT allowed to admit on the strength of a carrier. A list whose leaf is
// an edge union makes Phase B synthesise a sealed interface for the
// column (prepare.go, findEdgeUnionLeaf), and this package emits no
// sealed interface and no marker method at all (render_models.go,
// writeEntities), so admitting it would exit 0 over Go that references a
// type nothing declares — the failure the top-level edge-union refusal
// exists to prevent, reached one level in.
func TestListOfEdgeUnionStaysRefused(t *testing.T) {
	t.Parallel()

	in := listColumnInput(t, resolver.Column{
		Name: "xs",
		Type: resolver.ResolvedList{Element: resolver.ResolvedEdgeUnion{
			EdgeKeys: []schema.EdgeKey{
				{Source: "Person", KeyLabels: "AUTHORED", Target: "Post"},
				{Source: "Person", KeyLabels: "LIKES", Target: "Post"},
			},
		}},
	})
	files, err := age.New(age.WithPackageName("listexpr")).Generate(in)
	require.Error(t, err)
	require.Nil(t, files)
	require.ErrorIs(t, err, age.ErrUnsupportedQuery)
	require.Contains(t, err.Error(), `column "xs" projects a list of edgeUnion`)
}

// TestListOfTemporalYieldsToTheTypeTable pins the standing-aside. A
// temporal element is not this gate's to refuse: the shared phase asks
// the type table and answers with ErrUnrepresentableTemporal naming the
// kind, which this gate cannot do because it reports one reason per
// query. The top-level temporal arm stands aside for the same reason and
// has done since the gate was written; the list arm inherits it, and this
// row is what stops the recursion from inventing a second, worse answer.
//
// The column below is DECLARED, and since bd gqlc-dy40s that is the only
// way to reach this arm. A list of temporals arrives from a query text as
// collect() over a temporal constructor, and every spelling of one — bare
// or namespaced — is now refused on the text by the dialect gate ahead of
// this phase (ADR 0028 item 4). So this row pins the standing-aside for
// assembled input, which is the reachability the sentinel has here, not a
// path an author's .cypher file can still walk.
func TestListOfTemporalYieldsToTheTypeTable(t *testing.T) {
	t.Parallel()

	in := listColumnInput(t, resolver.Column{
		Name: "xs",
		Type: resolver.ResolvedList{Element: resolver.ResolvedTemporal{Kind: resolver.TemporalDate}},
	})
	files, err := age.New(age.WithPackageName("listexpr")).Generate(in)
	require.Error(t, err)
	require.Nil(t, files)
	require.ErrorIs(t, err, codegen.ErrUnrepresentableTemporal)
	require.NotErrorIs(t, err, age.ErrUnsupportedQuery)
	require.Contains(t, err.Error(), "list element projects temporal(date)")
}

// TestListExpressionColumnDecodesThroughTheSliceWrapper is the other half
// of the unblocking: that the served rows above do not merely pass the
// gate but reach the decode path .13 built. The emitted method has to
// read the column through the named list wrapper, and models.go has to
// carry that wrapper and the generic walk it is built on — otherwise the
// gate was widened onto an emission that does not exist.
func TestListExpressionColumnDecodesThroughTheSliceWrapper(t *testing.T) {
	t.Parallel()

	in := listColumnInput(t, resolver.Column{
		Name: "xs",
		Type: resolver.ResolvedList{Element: resolver.ResolvedScalar{Kind: resolver.ScalarString}},
	})
	files, err := age.New(age.WithPackageName("listexpr")).Generate(in)
	require.NoError(t, err)

	emitted := make(map[string]string, len(files))
	for _, f := range files {
		emitted[f.Path] = string(f.Contents)
	}

	models := emitted["models.go"]
	require.Contains(t, models, "func agtypeList[T any](", "the generic list walk is not in models.go")
	require.Contains(t, models, "agtypeListOfString", "the named wrapper for []string is not emitted")
	require.Contains(t, models, "return agtypeList(raw, agtypeString)",
		"the wrapper does not bind the element decoder")

	var queries string
	for path, contents := range emitted {
		if strings.HasSuffix(path, ".cypher.go") {
			queries = contents
		}
	}
	require.NotEmpty(t, queries, "no query file was emitted")
	require.Contains(t, queries, "agtypeListOfString(", "the column does not decode through the wrapper")
	require.Contains(t, queries, "[]string", "the row field does not carry the slice type")
}
