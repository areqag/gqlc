package query_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/areqag/gqlc/internal/graph"
	"github.com/areqag/gqlc/internal/query"
)

// --- constructor invariants (illegal states rejected by constructor) ---

func TestNewNodeBinding(t *testing.T) {
	b, err := query.NewNodeBinding("p", graph.LabelSet{"Person"})
	require.NoError(t, err)
	require.Equal(t, "p", b.Variable())
	require.Equal(t, graph.LabelSet{"Person"}, b.Labels())
	require.Equal(t, graph.Node, b.Kind())
}

func TestNewNodeBindingAllowsEmptyLabels(t *testing.T) {
	// C7: an unlabelled node is allowed; the resolver infers its type later.
	b, err := query.NewNodeBinding("b", nil)
	require.NoError(t, err)
	require.Equal(t, "b", b.Variable())
	require.Empty(t, b.Labels())
}

func TestNewNodeBindingRejectsEmptyVariable(t *testing.T) {
	// C3: an anonymous node is never a binding.
	_, err := query.NewNodeBinding("", graph.LabelSet{"Person"})
	require.Error(t, err)
}

func TestNewNodeBindingDefaultsToNonNullable(t *testing.T) {
	// The non-nullable constructor produces a binding the resolver may treat as
	// always-present; nullability is opt-in via NewNullableNodeBinding (ADR 0006).
	b, err := query.NewNodeBinding("p", graph.LabelSet{"Person"})
	require.NoError(t, err)
	require.False(t, b.Nullable())
}

func TestNewNullableNodeBinding(t *testing.T) {
	// A nullable node binding carries the same data as the non-nullable variant
	// plus the Nullable flag — the OPTIONAL-introduced case (ADR 0006).
	b, err := query.NewNullableNodeBinding("p", graph.LabelSet{"Person"})
	require.NoError(t, err)
	require.Equal(t, "p", b.Variable())
	require.Equal(t, graph.LabelSet{"Person"}, b.Labels())
	require.Equal(t, graph.Node, b.Kind())
	require.True(t, b.Nullable())
}

func TestNewNullableNodeBindingRejectsEmptyVariable(t *testing.T) {
	// The empty-variable invariant holds across both constructors: an anonymous
	// node is never a binding regardless of nullability (C3).
	_, err := query.NewNullableNodeBinding("", nil)
	require.Error(t, err)
}

func TestNewEdgeBinding(t *testing.T) {
	src, err := query.NewVarEndpoint("a")
	require.NoError(t, err)
	tgt, err := query.NewVarEndpoint("b")
	require.NoError(t, err)

	b, err := query.NewEdgeBinding("r", graph.LabelSet{"KNOWS"}, src, tgt, true)
	require.NoError(t, err)
	require.Equal(t, "r", b.Variable())
	require.Equal(t, graph.LabelSet{"KNOWS"}, b.Labels())
	require.Equal(t, src, b.Source())
	require.Equal(t, tgt, b.Target())
	require.Equal(t, graph.Edge, b.Kind())
}

func TestNewEdgeBindingAllowsAnonymousVariableAndUntyped(t *testing.T) {
	// An anonymous edge has an empty variable; C7: an untyped edge has no labels.
	src := query.NewInlineEndpoint(graph.LabelSet{"Person"})
	tgt := query.NewInlineEndpoint(nil) // the fully-anonymous () endpoint

	b, err := query.NewEdgeBinding("", nil, src, tgt, true)
	require.NoError(t, err)
	require.Empty(t, b.Variable())
	require.Empty(t, b.Labels())
}

func TestNewEdgeBindingRejectsMissingEndpoint(t *testing.T) {
	tgt, err := query.NewVarEndpoint("b")
	require.NoError(t, err)

	// A nil source endpoint (the interface zero value) is illegal.
	_, err = query.NewEdgeBinding("r", nil, nil, tgt, true)
	require.Error(t, err)

	// A nil target endpoint is illegal.
	_, err = query.NewEdgeBinding("r", nil, tgt, nil, true)
	require.Error(t, err)
}

func TestNewEdgeBindingDefaultsToNonNullable(t *testing.T) {
	src, _ := query.NewVarEndpoint("a")
	tgt, _ := query.NewVarEndpoint("b")
	b, err := query.NewEdgeBinding("r", nil, src, tgt, true)
	require.NoError(t, err)
	require.False(t, b.Nullable())
}

func TestNewNullableEdgeBinding(t *testing.T) {
	src, _ := query.NewVarEndpoint("a")
	tgt, _ := query.NewVarEndpoint("b")
	b, err := query.NewNullableEdgeBinding("r", graph.LabelSet{"KNOWS"}, src, tgt, true)
	require.NoError(t, err)
	require.Equal(t, "r", b.Variable())
	require.Equal(t, graph.LabelSet{"KNOWS"}, b.Labels())
	require.Equal(t, src, b.Source())
	require.Equal(t, tgt, b.Target())
	require.True(t, b.Nullable())
}

func TestNewNullableEdgeBindingAllowsAnonymousVariableAndUntyped(t *testing.T) {
	// An anonymous edge introduced in OPTIONAL MATCH still carries the Nullable
	// flag even though no Ref can reference it — the flag is on every binding
	// the clause introduces (ADR 0006).
	src := query.NewInlineEndpoint(graph.LabelSet{"Person"})
	tgt := query.NewInlineEndpoint(nil)
	b, err := query.NewNullableEdgeBinding("", nil, src, tgt, true)
	require.NoError(t, err)
	require.Empty(t, b.Variable())
	require.True(t, b.Nullable())
}

func TestNewNullableEdgeBindingRejectsMissingEndpoint(t *testing.T) {
	tgt, _ := query.NewVarEndpoint("b")
	_, err := query.NewNullableEdgeBinding("r", nil, nil, tgt, true)
	require.Error(t, err)
	_, err = query.NewNullableEdgeBinding("r", nil, tgt, nil, true)
	require.Error(t, err)
}

func TestEdgeBindingDirected(t *testing.T) {
	// The direction marker (Stage 5): true for a one-arrow edge, false for an
	// undirected edge. It is a constructor parameter and is always emitted in JSON.
	src, _ := query.NewVarEndpoint("a")
	tgt, _ := query.NewVarEndpoint("b")

	directed, err := query.NewEdgeBinding("r", nil, src, tgt, true)
	require.NoError(t, err)
	require.True(t, directed.Directed())
	outD, err := json.Marshal(directed)
	require.NoError(t, err)
	require.Contains(t, string(outD), `"directed":true`)

	undirected, err := query.NewEdgeBinding("r", nil, src, tgt, false)
	require.NoError(t, err)
	require.False(t, undirected.Directed())
	outU, err := json.Marshal(undirected)
	require.NoError(t, err)
	require.Contains(t, string(outU), `"directed":false`)

	// The nullable variant forwards the directed marker.
	nullableUndirected, err := query.NewNullableEdgeBinding("r", nil, src, tgt, false)
	require.NoError(t, err)
	require.False(t, nullableUndirected.Directed())
	require.True(t, nullableUndirected.Nullable())
}

func TestNewVarEndpoint(t *testing.T) {
	e, err := query.NewVarEndpoint("a")
	require.NoError(t, err)
	require.Equal(t, "a", e.Variable())
}

func TestNewVarEndpointRejectsEmptyVariable(t *testing.T) {
	// A variable endpoint must name a binding; the empty name is the inline case.
	_, err := query.NewVarEndpoint("")
	require.Error(t, err)
}

func TestNewInlineEndpoint(t *testing.T) {
	e := query.NewInlineEndpoint(graph.LabelSet{"Person"})
	require.Equal(t, graph.LabelSet{"Person"}, e.Labels())
}

func TestNewInlineEndpointAllowsEmptyLabels(t *testing.T) {
	// C4: the fully-anonymous () endpoint carries no labels.
	e := query.NewInlineEndpoint(nil)
	require.Empty(t, e.Labels())
}

// --- the constructors are the only entry point (illegal zero values
// unconstructible outside the package) ---
//
// These tests are the point of this branch: the variant fields are unexported, so
// a foreign package (this _test package) cannot fabricate an illegal value with a
// struct literal. The zero value of each variant carries no data, and the
// constructors are the sole way to populate one — so every non-zero value that
// can exist has passed the invariant checks.

func TestVariantZeroValuesCarryNoData(t *testing.T) {
	// The exported zero value is the only struct literal a foreign package can
	// write (all data fields are unexported). It is inert: empty variable, empty
	// labels, no usable endpoints.
	var node query.NodeBinding
	require.Empty(t, node.Variable())
	require.Empty(t, node.Labels())

	var edge query.EdgeBinding
	require.Empty(t, edge.Variable())
	require.Empty(t, edge.Labels())
	require.Nil(t, edge.Source())
	require.Nil(t, edge.Target())

	var varEnd query.VarEndpoint
	require.Empty(t, varEnd.Variable())

	var inline query.InlineEndpoint
	require.Empty(t, inline.Labels())
}

func TestConstructorsAreSoleSourceOfData(t *testing.T) {
	// A populated value is reachable only through a constructor: there is no other
	// way to set the unexported fields. The presence of data is therefore proof
	// the invariants ran.
	node, err := query.NewNodeBinding("p", graph.LabelSet{"Person"})
	require.NoError(t, err)
	require.NotEmpty(t, node.Variable())

	src, err := query.NewVarEndpoint("a")
	require.NoError(t, err)
	require.NotEmpty(t, src.Variable())

	tgt := query.NewInlineEndpoint(graph.LabelSet{"Company"})
	edge, err := query.NewEdgeBinding("r", nil, src, tgt, true)
	require.NoError(t, err)
	require.NotNil(t, edge.Source())
	require.NotNil(t, edge.Target())

	// The sealed interfaces close the sums: only the four package-defined variants
	// satisfy them, so a binding/endpoint is always exactly one known shape.
	var _ query.Binding = node
	var _ query.Binding = edge
	var _ query.Endpoint = src
	var _ query.Endpoint = tgt
}

// --- projection sum: constructors and the AggregateFunc stringer ---

func TestNewRefProjection(t *testing.T) {
	p := query.NewRefProjection(query.Ref{Variable: "n", Property: "name"})
	require.Equal(t, query.Ref{Variable: "n", Property: "name"}, p.Ref())
	var _ query.Projection = p
}

func TestNewLiteralProjection(t *testing.T) {
	// A literal carries no structured value (§2): the variant exists only so the
	// column is counted and named. It satisfies the sum and carries no Ref.
	p := query.NewLiteralProjection()
	var _ query.Projection = p
}

func TestNewFuncProjection(t *testing.T) {
	refs := []query.Ref{{Variable: "a", Property: "num"}, {Variable: "b", Property: "num"}}
	p := query.NewFuncProjection(refs)
	require.Equal(t, refs, p.Refs())
	var _ query.Projection = p
}

func TestNewFuncProjectionAllowsNoRefs(t *testing.T) {
	// A zero-arg function (or one over no bound variables) carries no Refs.
	p := query.NewFuncProjection(nil)
	require.Empty(t, p.Refs())
}

func TestNewAggregateProjection(t *testing.T) {
	refs := []query.Ref{{Variable: "n", Property: "num"}}
	p := query.NewAggregateProjection(query.AggSum, refs)
	require.Equal(t, query.AggSum, p.Func())
	require.Equal(t, refs, p.Refs())
	var _ query.Projection = p
}

func TestNewAggregateProjectionCountStar(t *testing.T) {
	// count(*) is the degenerate case — AggCount with an empty []Ref (§2).
	p := query.NewAggregateProjection(query.AggCount, nil)
	require.Equal(t, query.AggCount, p.Func())
	require.Empty(t, p.Refs())
}

// TestProjectionZeroValuesCarryNoData mirrors the binding/endpoint discipline:
// the exported zero value of each variant is the only struct literal a foreign
// package can write (data fields are unexported), and it is inert.
func TestProjectionZeroValuesCarryNoData(t *testing.T) {
	var ref query.RefProjection
	require.Equal(t, query.Ref{}, ref.Ref())

	var fn query.FuncProjection
	require.Empty(t, fn.Refs())

	var agg query.AggregateProjection
	require.Equal(t, query.AggCount, agg.Func()) // the iota-zero aggregate
	require.Empty(t, agg.Refs())

	// The sealed interface closes the sum: only the four package-defined variants
	// satisfy it, so a projection is always exactly one known shape.
	var _ query.Projection = query.RefProjection{}
	var _ query.Projection = query.LiteralProjection{}
	var _ query.Projection = query.FuncProjection{}
	var _ query.Projection = query.AggregateProjection{}
}

// TestAggregateFuncString pins the lowercase names the JSON "func" discriminator
// derives from, so the serialised name can never drift from the enum.
func TestAggregateFuncString(t *testing.T) {
	for _, tc := range []struct {
		fn   query.AggregateFunc
		want string
	}{
		{query.AggCount, "count"},
		{query.AggSum, "sum"},
		{query.AggCollect, "collect"},
		{query.AggMin, "min"},
		{query.AggMax, "max"},
		{query.AggAvg, "avg"},
		{query.AggStdev, "stdev"},
		{query.AggPercentile, "percentile"},
	} {
		require.Equal(t, tc.want, tc.fn.String())
	}
}

// --- projection sum: JSON shapes (§5) ---

func TestRefProjectionMarshalJSON(t *testing.T) {
	out, err := json.Marshal(query.NewRefProjection(query.Ref{Variable: "n", Property: "name"}))
	require.NoError(t, err)
	require.JSONEq(t, `{"kind":"ref","variable":"n","property":"name"}`, string(out))
}

func TestLiteralProjectionMarshalJSON(t *testing.T) {
	out, err := json.Marshal(query.NewLiteralProjection())
	require.NoError(t, err)
	require.JSONEq(t, `{"kind":"literal"}`, string(out))
}

func TestFuncProjectionMarshalJSON(t *testing.T) {
	out, err := json.Marshal(query.NewFuncProjection([]query.Ref{
		{Variable: "a", Property: "num"},
		{Variable: "b"},
	}))
	require.NoError(t, err)
	require.JSONEq(t,
		`{"kind":"func","refs":[{"variable":"a","property":"num"},{"variable":"b","property":""}]}`,
		string(out))
}

func TestAggregateProjectionMarshalJSON(t *testing.T) {
	out, err := json.Marshal(query.NewAggregateProjection(query.AggSum, []query.Ref{
		{Variable: "n", Property: "num"},
	}))
	require.NoError(t, err)
	require.JSONEq(t,
		`{"kind":"aggregate","func":"sum","refs":[{"variable":"n","property":"num"}]}`,
		string(out))
}

func TestAggregateProjectionMarshalJSONCountStar(t *testing.T) {
	// count(*) marshals AggCount with an empty refs array (null, the always-emit
	// posture the other sums follow for nil slices).
	out, err := json.Marshal(query.NewAggregateProjection(query.AggCount, nil))
	require.NoError(t, err)
	require.JSONEq(t, `{"kind":"aggregate","func":"count","refs":null}`, string(out))
}

func TestReturnItemMarshalJSON(t *testing.T) {
	// A ReturnItem marshals one level deep: lowercase "name" and "value", the
	// value carrying its own "kind" discriminator (§5).
	item := query.ReturnItem{
		Name:  "name",
		Value: query.NewRefProjection(query.Ref{Variable: "p", Property: "name"}),
	}
	out, err := json.Marshal(item)
	require.NoError(t, err)
	require.JSONEq(t,
		`{"name":"name","value":{"kind":"ref","variable":"p","property":"name"}}`,
		string(out))
}

func TestQueryPartMarshalJSONEmitsReturnsAll(t *testing.T) {
	// "returnsAll" is always emitted (no omitempty) on a part, matching the
	// always-emit convention. A plain part (no RETURN *) serialises it as false.
	part := query.QueryPart{
		Returns: []query.ReturnItem{
			{Name: "n", Value: query.NewRefProjection(query.Ref{Variable: "n"})},
		},
	}
	out, err := json.Marshal(part)
	require.NoError(t, err)
	require.Contains(t, string(out), `"returnsAll":false`)

	part.ReturnsAll = true
	part.Returns = nil // RETURN * / WITH * does not mix with explicit items (§3)
	out, err = json.Marshal(part)
	require.NoError(t, err)
	require.Contains(t, string(out), `"returnsAll":true`)
}

// --- branch/part structure and UnionKind (Stage 4) ---

// TestUnionKindString pins the wire names the JSON value derives from, so the
// serialised combinator can never drift from the enum.
func TestUnionKindString(t *testing.T) {
	require.Equal(t, "union", query.UnionDistinct.String())
	require.Equal(t, "unionAll", query.UnionAll.String())
}

func TestUnionKindMarshalJSON(t *testing.T) {
	out, err := json.Marshal(query.UnionDistinct)
	require.NoError(t, err)
	require.JSONEq(t, `"union"`, string(out))

	out, err = json.Marshal(query.UnionAll)
	require.NoError(t, err)
	require.JSONEq(t, `"unionAll"`, string(out))
}

func TestQueryMarshalJSONShape(t *testing.T) {
	// The new top-level shape: lowercase "branches"/"combinators"/"parameters";
	// each branch a {"parts": [...]}; each part {"bindings","returns","returnsAll"}.
	q := representativeQuery(t)
	out, err := json.Marshal(q)
	require.NoError(t, err)
	s := string(out)
	require.Contains(t, s, `"branches"`)
	require.Contains(t, s, `"combinators"`)
	require.Contains(t, s, `"parameters"`)
	require.Contains(t, s, `"parts"`)
	require.Contains(t, s, `"bindings"`)
	require.Contains(t, s, `"returns"`)
}

func TestQueryMarshalJSONEmitsCombinatorsNullForOneBranch(t *testing.T) {
	// Combinators is always emitted; with one branch it is null (nil slice),
	// matching the always-emit convention.
	q := representativeQuery(t)
	require.Nil(t, q.Combinators)
	out, err := json.Marshal(q)
	require.NoError(t, err)
	require.Contains(t, string(out), `"combinators":null`)
}

func TestQueryMarshalJSONEmitsCombinatorsForUnion(t *testing.T) {
	// Two branches joined by UNION ALL: Combinators has one entry, marshalled via
	// the stringer.
	q := query.Query{
		Branches: []query.QueryBranch{
			{Parts: []query.QueryPart{{
				Bindings: []query.Binding{must(query.NewNodeBinding("a", nil))},
				Returns:  []query.ReturnItem{{Name: "a", Value: query.NewRefProjection(query.Ref{Variable: "a"})}},
			}}},
			{Parts: []query.QueryPart{{
				Bindings: []query.Binding{must(query.NewNodeBinding("b", nil))},
				Returns:  []query.ReturnItem{{Name: "b", Value: query.NewRefProjection(query.Ref{Variable: "b"})}},
			}}},
		},
		Combinators: []query.UnionKind{query.UnionAll},
	}
	out, err := json.Marshal(q)
	require.NoError(t, err)
	require.Contains(t, string(out), `"combinators":["unionAll"]`)
}

// --- deterministic JSON marshalling ---

// must lifts a fallible model constructor into an expression usable in a struct
// literal: it panics if err is non-nil. The hand-built test values are hard-coded
// valid, so any error here is a programmer error and panic is the honest signal.
func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

// representativeQuery exercises both binding variants and both endpoint variants,
// plus refs, return items and parameters — now in the one-branch/one-part shape.
func representativeQuery(t *testing.T) query.Query {
	t.Helper()

	a, err := query.NewNodeBinding("a", graph.LabelSet{"Person"})
	require.NoError(t, err)
	b, err := query.NewNodeBinding("b", nil)
	require.NoError(t, err)

	srcVar, err := query.NewVarEndpoint("a")
	require.NoError(t, err)
	tgtInline := query.NewInlineEndpoint(graph.LabelSet{"Company"})
	edge, err := query.NewEdgeBinding("r", graph.LabelSet{"WORKS_AT"}, srcVar, tgtInline, true)
	require.NoError(t, err)

	return query.Query{
		Branches: []query.QueryBranch{
			{Parts: []query.QueryPart{{
				Bindings: []query.Binding{a, b, edge},
				Returns: []query.ReturnItem{
					{Name: "a", Value: query.NewRefProjection(query.Ref{Variable: "a"})},
					{Name: "a.name", Value: query.NewRefProjection(query.Ref{Variable: "a", Property: "name"})},
				},
			}}},
		},
		Parameters: []query.Parameter{
			{Name: "id", Uses: []query.Use{
				query.NewPropertyUse(query.Ref{Variable: "a", Property: "id"}),
			}},
		},
	}
}

func TestMarshalJSONIsStable(t *testing.T) {
	q := representativeQuery(t)

	first, err := json.MarshalIndent(q, "", "  ")
	require.NoError(t, err)
	for range 8 {
		next, err := json.MarshalIndent(q, "", "  ")
		require.NoError(t, err)
		require.Equal(t, string(first), string(next))
	}
}

func TestMarshalJSONDiscriminators(t *testing.T) {
	q := representativeQuery(t)
	out, err := json.Marshal(q)
	require.NoError(t, err)
	s := string(out)

	require.Contains(t, s, `"kind":"node"`)
	require.Contains(t, s, `"kind":"edge"`)
	require.Contains(t, s, `"kind":"var"`)
	require.Contains(t, s, `"kind":"inline"`)
}

func TestMarshalJSONEmitsNullable(t *testing.T) {
	// The nullable flag is always emitted (no omitempty), mirroring the
	// existing convention of always-emit for `labels: null` and `variable: ""`.
	// Bindings from the non-nullable constructors serialise as false.
	a, err := query.NewNodeBinding("a", nil)
	require.NoError(t, err)
	b, err := query.NewNullableNodeBinding("b", nil)
	require.NoError(t, err)
	src, _ := query.NewVarEndpoint("a")
	tgt, _ := query.NewVarEndpoint("b")
	e, err := query.NewNullableEdgeBinding("r", nil, src, tgt, true)
	require.NoError(t, err)

	outA, _ := json.Marshal(a)
	outB, _ := json.Marshal(b)
	outE, _ := json.Marshal(e)
	require.Contains(t, string(outA), `"nullable":false`)
	require.Contains(t, string(outB), `"nullable":true`)
	require.Contains(t, string(outE), `"nullable":true`)
}

// TestBindingDiscriminatorTracksEntityKind pins the binding "kind" tag to
// graph.EntityKind.String, the single source it derives from, so the serialised
// tag can never drift from Kind().
func TestBindingDiscriminatorTracksEntityKind(t *testing.T) {
	node, err := query.NewNodeBinding("p", nil)
	require.NoError(t, err)
	src, _ := query.NewVarEndpoint("a")
	tgt, _ := query.NewVarEndpoint("b")
	edge, err := query.NewEdgeBinding("r", nil, src, tgt, true)
	require.NoError(t, err)

	for _, b := range []query.Binding{node, edge} {
		out, err := json.Marshal(b)
		require.NoError(t, err)
		require.Contains(t, string(out), `"kind":"`+b.Kind().String()+`"`)
	}
}

// --- property-based tests (rapid) ---

// genVariable generates non-empty variable names, the precondition the binding
// and var-endpoint constructors enforce.
func genVariable() *rapid.Generator[string] {
	return rapid.StringMatching(`[a-z][a-zA-Z0-9_]*`)
}

func genLabelSet() *rapid.Generator[graph.LabelSet] {
	return rapid.Custom(func(t *rapid.T) graph.LabelSet {
		labels := rapid.SliceOf(rapid.StringMatching(`[A-Z][a-zA-Z0-9_]*`)).Draw(t, "labels")
		if len(labels) == 0 {
			return nil
		}
		return graph.LabelSet(labels)
	})
}

func genEndpoint() *rapid.Generator[query.Endpoint] {
	return rapid.Custom(func(t *rapid.T) query.Endpoint {
		if rapid.Bool().Draw(t, "isVar") {
			e, err := query.NewVarEndpoint(genVariable().Draw(t, "var"))
			if err != nil {
				t.Fatalf("NewVarEndpoint rejected a non-empty variable: %v", err)
			}
			return e
		}
		return query.NewInlineEndpoint(genLabelSet().Draw(t, "labels"))
	})
}

func genBinding() *rapid.Generator[query.Binding] {
	return rapid.Custom(func(t *rapid.T) query.Binding {
		if rapid.Bool().Draw(t, "isNode") {
			b, err := query.NewNodeBinding(genVariable().Draw(t, "var"), genLabelSet().Draw(t, "labels"))
			if err != nil {
				t.Fatalf("NewNodeBinding rejected a valid node: %v", err)
			}
			return b
		}
		// Edges may be anonymous, so draw the variable from "" plus the named space.
		variable := rapid.OneOf(rapid.Just(""), genVariable()).Draw(t, "var")
		b, err := query.NewEdgeBinding(
			variable,
			genLabelSet().Draw(t, "labels"),
			genEndpoint().Draw(t, "source"),
			genEndpoint().Draw(t, "target"),
			rapid.Bool().Draw(t, "directed"),
		)
		if err != nil {
			t.Fatalf("NewEdgeBinding rejected a valid edge: %v", err)
		}
		return b
	})
}

func genQuery() *rapid.Generator[query.Query] {
	return rapid.Custom(func(t *rapid.T) query.Query {
		return query.Query{
			Branches: []query.QueryBranch{
				{Parts: []query.QueryPart{{
					Bindings: rapid.SliceOf(genBinding()).Draw(t, "bindings"),
				}}},
			},
		}
	})
}

// TestMarshalJSONDeterministicOverRandomQueries is the property-based guard: any
// valid Query marshals identically every time, so a regenerated golden never
// flaps regardless of map iteration order.
func TestMarshalJSONDeterministicOverRandomQueries(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		q := genQuery().Draw(t, "query")
		first, err := json.Marshal(q)
		if err != nil {
			t.Fatalf("marshal failed: %v", err)
		}
		next, err := json.Marshal(q)
		if err != nil {
			t.Fatalf("marshal failed: %v", err)
		}
		if string(first) != string(next) {
			t.Fatalf("non-deterministic marshal:\n%s\n%s", first, next)
		}
	})
}

// TestConstructorsRejectEmptyVariable is the property-based guard for the
// type-alone-can't-express invariant: NodeBinding and VarEndpoint always reject
// the empty variable and always accept a non-empty one.
func TestConstructorsRejectEmptyVariable(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		v := genVariable().Draw(t, "var")
		if _, err := query.NewNodeBinding(v, nil); err != nil {
			t.Fatalf("NewNodeBinding rejected non-empty variable %q: %v", v, err)
		}
		if _, err := query.NewVarEndpoint(v); err != nil {
			t.Fatalf("NewVarEndpoint rejected non-empty variable %q: %v", v, err)
		}
		if _, err := query.NewNodeBinding("", nil); err == nil {
			t.Fatal("NewNodeBinding accepted the empty variable")
		}
		if _, err := query.NewVarEndpoint(""); err == nil {
			t.Fatal("NewVarEndpoint accepted the empty variable")
		}
	})
}
