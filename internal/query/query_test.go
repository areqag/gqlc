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
	require.Equal(t, query.BindingNode, b.Kind())
	require.Equal(t, graph.Node, b.EntityKind())
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
	require.Equal(t, query.BindingNode, b.Kind())
	require.True(t, b.Nullable())
}

func TestNewNullableNodeBindingRejectsEmptyVariable(t *testing.T) {
	// The empty-variable invariant holds across both constructors: an anonymous
	// node is never a binding regardless of nullability (C3).
	_, err := query.NewNullableNodeBinding("", nil)
	require.Error(t, err)
}

func TestNewNullableNodeBindingInGroup(t *testing.T) {
	// The InGroup variant (ay9) carries the introducing OPTIONAL clause's group
	// id on top of the NewNullableNodeBinding invariants.
	b, err := query.NewNullableNodeBindingInGroup("p", graph.LabelSet{"Person"}, 3)
	require.NoError(t, err)
	require.Equal(t, "p", b.Variable())
	require.Equal(t, graph.LabelSet{"Person"}, b.Labels())
	require.True(t, b.Nullable())
	require.Equal(t, 3, b.OptionalGroup())
}

func TestNewNullableNodeBindingInGroupRejectsNonPositiveGroup(t *testing.T) {
	// Group 0 ("no group") is reachable only through NewNullableNodeBinding —
	// "in a group" and "not in a group" stay constructor-disjoint (ay9 §3.2).
	_, err := query.NewNullableNodeBindingInGroup("p", nil, 0)
	require.Error(t, err)
	_, err = query.NewNullableNodeBindingInGroup("p", nil, -1)
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
	require.Equal(t, query.BindingEdge, b.Kind())
	require.Equal(t, graph.Edge, b.EntityKind())
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
	src := must(query.NewVarEndpoint("a"))
	tgt := must(query.NewVarEndpoint("b"))
	b, err := query.NewEdgeBinding("r", nil, src, tgt, true)
	require.NoError(t, err)
	require.False(t, b.Nullable())
}

func TestNewNullableEdgeBinding(t *testing.T) {
	src := must(query.NewVarEndpoint("a"))
	tgt := must(query.NewVarEndpoint("b"))
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
	tgt := must(query.NewVarEndpoint("b"))
	_, err := query.NewNullableEdgeBinding("r", nil, nil, tgt, true)
	require.Error(t, err)
	_, err = query.NewNullableEdgeBinding("r", nil, tgt, nil, true)
	require.Error(t, err)
}

func TestNewNullableEdgeBindingInGroup(t *testing.T) {
	// The InGroup variant (ay9) carries the introducing OPTIONAL clause's group
	// id on top of the NewNullableEdgeBinding invariants.
	src := must(query.NewVarEndpoint("a"))
	tgt := must(query.NewVarEndpoint("b"))
	b, err := query.NewNullableEdgeBindingInGroup("r", graph.LabelSet{"KNOWS"}, src, tgt, true, 2)
	require.NoError(t, err)
	require.Equal(t, "r", b.Variable())
	require.Equal(t, src, b.Source())
	require.Equal(t, tgt, b.Target())
	require.True(t, b.Nullable())
	require.Equal(t, 2, b.OptionalGroup())
}

func TestNewNullableEdgeBindingInGroupRejectsNonPositiveGroup(t *testing.T) {
	src := must(query.NewVarEndpoint("a"))
	tgt := must(query.NewVarEndpoint("b"))
	_, err := query.NewNullableEdgeBindingInGroup("r", nil, src, tgt, true, 0)
	require.Error(t, err)
	_, err = query.NewNullableEdgeBindingInGroup("r", nil, src, tgt, true, -1)
	require.Error(t, err)
}

func TestEdgeBindingDirected(t *testing.T) {
	// The direction marker (Stage 5): true for a one-arrow edge, false for an
	// undirected edge. It is a constructor parameter and is always emitted in JSON.
	src := must(query.NewVarEndpoint("a"))
	tgt := must(query.NewVarEndpoint("b"))

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

// --- NewPart invariant (Stage 12 §3.2 / §4.6) ---

func TestNewPartAcceptsWellFormedShapes(t *testing.T) {
	// A part with only bindings, only a projection, only ReturnsAll=true, or
	// only effects — each is a legal complete shape. NewPart passes any of
	// them through unchanged.
	node, err := query.NewNodeBinding("n", nil)
	require.NoError(t, err)

	pBindings, err := query.NewPart([]query.Binding{node}, nil, false, false, nil)
	require.NoError(t, err)
	require.Len(t, pBindings.Bindings, 1)

	pReturns, err := query.NewPart(nil, []query.ReturnItem{
		{Name: "n", Value: query.NewRefProjection(query.Ref{Variable: "n"}, query.TypeNode{})},
	}, false, false, nil)
	require.NoError(t, err)
	require.Len(t, pReturns.Returns, 1)

	pReturnsAll, err := query.NewPart(nil, nil, true, false, nil)
	require.NoError(t, err)
	require.True(t, pReturnsAll.ReturnsAll)

	pEffects, err := query.NewPart(nil, nil, false, false, []query.Effect{
		query.NewCreateEffect([]string{"m"}),
	})
	require.NoError(t, err)
	require.Len(t, pEffects.Effects, 1)
}

func TestNewPartRejectsEmpty(t *testing.T) {
	// The all-empty Part is unrepresentable at construction (Stage 12): no
	// parse path can reach the shape (the grammar rules it out), but the
	// model constructor still refuses it, so illegal states are unrepresentable
	// even under adversarial hand-construction. This is the point of the
	// smart constructor: field-level types cannot express the invariant
	// alone. The Distinct axis (part-distinct-axis spec §3.3) does not
	// satisfy the invariant on its own: DISTINCT is a modifier on a
	// projection that must exist, so distinct=true with every other axis
	// empty is still ErrEmptyPart.
	_, err := query.NewPart(nil, nil, false, false, nil)
	require.ErrorIs(t, err, query.ErrEmptyPart)

	_, err = query.NewPart(nil, nil, false, true, nil)
	require.ErrorIs(t, err, query.ErrEmptyPart)
}

// --- Part.Distinct axis (part-distinct-axis spec §1) ---

func TestNewPartCarriesDistinct(t *testing.T) {
	// The Distinct axis is the fifth axis on Part alongside Bindings /
	// Returns / ReturnsAll / Effects. Constructor threads it through;
	// the field is readable on the returned Part. A returnsAll part
	// with distinct=true is the RETURN DISTINCT * / WITH DISTINCT *
	// composition (spec §1.3 — the two axes compose freely).
	p, err := query.NewPart(nil, []query.ReturnItem{
		{Name: "n", Value: query.NewRefProjection(query.Ref{Variable: "n"}, query.TypeNode{})},
	}, false, true, nil)
	require.NoError(t, err)
	require.True(t, p.Distinct)

	pWildcard, err := query.NewPart(nil, nil, true, true, nil)
	require.NoError(t, err)
	require.True(t, pWildcard.Distinct)
	require.True(t, pWildcard.ReturnsAll)

	pNotDistinct, err := query.NewPart(nil, nil, true, false, nil)
	require.NoError(t, err)
	require.False(t, pNotDistinct.Distinct)
}

func TestPartDistinctAlwaysEmitted(t *testing.T) {
	// The "distinct" key is emitted on every part, matching the always-
	// emit convention (nullable, directed, returnsAll, aggregate distinct).
	// A Distinct=false part's JSON must contain "distinct":false alongside
	// the four existing keys; a Distinct=true part must contain
	// "distinct":true. Existing keys (bindings, returns, returnsAll,
	// effects) stay verbatim: the change is field-addition-only on the
	// wire (spec §1.6, §1.8).
	pFalse, err := query.NewPart(nil, nil, true, false, nil)
	require.NoError(t, err)
	outFalse, err := json.Marshal(pFalse)
	require.NoError(t, err)
	require.Contains(t, string(outFalse), `"distinct":false`)
	require.Contains(t, string(outFalse), `"returnsAll":true`)

	pTrue, err := query.NewPart(nil, nil, true, true, nil)
	require.NoError(t, err)
	outTrue, err := json.Marshal(pTrue)
	require.NoError(t, err)
	require.Contains(t, string(outTrue), `"distinct":true`)
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
	// A property lookup types as TypeUnknown (Stage 6): the schema owns property
	// typing per ADR 0003, so the parser records "cannot tell" honestly.
	p := query.NewRefProjection(query.Ref{Variable: "n", Property: "name"}, query.TypeUnknown{})
	require.Equal(t, query.Ref{Variable: "n", Property: "name"}, p.Ref())
	require.Equal(t, query.TypeUnknown{}, p.Type())
	var _ query.Projection = p
}

func TestNewLiteralProjection(t *testing.T) {
	// Stage 6: LiteralProjection carries its scalar-literal kind. The listener
	// computes the kind from the grammar node; the value stays below the
	// type-interface boundary (ADR 0005). It satisfies the sum and carries no Ref.
	p := query.NewLiteralProjection(query.TypeInt{})
	require.Equal(t, query.TypeInt{}, p.Type())
	var _ query.Projection = p
}

func TestNewFuncProjection(t *testing.T) {
	// Stage 6: FuncProjection carries a result type — TypeUnknown, because
	// function identity is below the boundary (ADR 0005).
	refs := []query.Ref{{Variable: "a", Property: "num"}, {Variable: "b", Property: "num"}}
	p := query.NewFuncProjection(refs, query.TypeUnknown{})
	require.Equal(t, refs, p.Refs())
	require.Equal(t, query.TypeUnknown{}, p.Type())
	var _ query.Projection = p
}

func TestNewFuncProjectionAllowsNoRefs(t *testing.T) {
	// A zero-arg function (or one over no bound variables) carries no Refs.
	p := query.NewFuncProjection(nil, query.TypeUnknown{})
	require.Empty(t, p.Refs())
}

func TestNewAggregateProjection(t *testing.T) {
	// Stage 10: AggregateProjection carries a result type (sum(int) → int per
	// spec §1.2) and a DISTINCT axis (false when not written). The refs list
	// records the var/var.prop arguments the aggregate touches.
	refs := []query.Ref{{Variable: "n", Property: "num"}}
	p := query.NewAggregateProjection(query.AggSum, refs, false, query.TypeInt{})
	require.Equal(t, query.AggSum, p.Func())
	require.Equal(t, refs, p.Refs())
	require.False(t, p.Distinct())
	require.Equal(t, query.TypeInt{}, p.Type())
	var _ query.Projection = p
}

func TestNewAggregateProjectionCountStar(t *testing.T) {
	// Stage 10: count(*) is the degenerate AggCount case — no operand refs. Its
	// result type is TypeInt unconditionally (spec §1.2 rationale: openCypher's
	// count returns an integer by specification).
	p := query.NewAggregateProjection(query.AggCount, nil, false, query.TypeInt{})
	require.Equal(t, query.AggCount, p.Func())
	require.Empty(t, p.Refs())
	require.False(t, p.Distinct())
	require.Equal(t, query.TypeInt{}, p.Type())
}

func TestNewAggregateProjectionDistinct(t *testing.T) {
	// Stage 10: DISTINCT enters the model as a scalar axis. count(DISTINCT a)
	// and count(a) are observably-different queries; the model preserves the
	// distinction so the generated code re-executes the original text against
	// a faithful type interface (spec §1.1).
	p := query.NewAggregateProjection(query.AggCount, []query.Ref{{Variable: "a"}}, true, query.TypeInt{})
	require.True(t, p.Distinct())
	require.Equal(t, query.AggCount, p.Func())
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

	// Stage 6: ExprProjection joins the sum with the same inert-zero-value
	// discipline — Refs is nil, Type() returns nil interface (marshal falls
	// back to TypeUnknown via projectionType).
	var expr query.ExprProjection
	require.Empty(t, expr.Refs())

	// The sealed interface closes the sum: only the five package-defined variants
	// satisfy it, so a projection is always exactly one known shape.
	var _ query.Projection = query.RefProjection{}
	var _ query.Projection = query.LiteralProjection{}
	var _ query.Projection = query.FuncProjection{}
	var _ query.Projection = query.AggregateProjection{}
	var _ query.Projection = query.ExprProjection{}
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
	// Stage 6: RefProjection carries a "type" field (always emitted). A property
	// lookup types as TypeUnknown — the schema owns property typing.
	out, err := json.Marshal(query.NewRefProjection(query.Ref{Variable: "n", Property: "name"}, query.TypeUnknown{}))
	require.NoError(t, err)
	require.JSONEq(t, `{"kind":"ref","variable":"n","property":"name","type":"unknown"}`, string(out))
}

// TestRefProjectionMarshalJSONWholeEntity pins the whole-entity case: a bare
// RETURN n on a node binding types as TypeNode and marshals with "type":"node".
func TestRefProjectionMarshalJSONWholeEntity(t *testing.T) {
	out, err := json.Marshal(query.NewRefProjection(query.Ref{Variable: "n"}, query.TypeNode{}))
	require.NoError(t, err)
	require.JSONEq(t, `{"kind":"ref","variable":"n","property":"","type":"node"}`, string(out))
}

func TestLiteralProjectionMarshalJSON(t *testing.T) {
	// Stage 6: LiteralProjection emits its scalar-literal kind as "type" —
	// always emitted, matching the always-emit convention.
	out, err := json.Marshal(query.NewLiteralProjection(query.TypeInt{}))
	require.NoError(t, err)
	require.JSONEq(t, `{"kind":"literal","type":"int"}`, string(out))
}

func TestFuncProjectionMarshalJSON(t *testing.T) {
	// Stage 6: FuncProjection emits its result type as "type" — TypeUnknown for
	// today, always emitted.
	out, err := json.Marshal(query.NewFuncProjection([]query.Ref{
		{Variable: "a", Property: "num"},
		{Variable: "b"},
	}, query.TypeUnknown{}))
	require.NoError(t, err)
	require.JSONEq(t,
		`{"kind":"func","refs":[{"variable":"a","property":"num"},{"variable":"b","property":""}],"type":"unknown"}`,
		string(out))
}

func TestAggregateProjectionMarshalJSON(t *testing.T) {
	// Stage 10: AggregateProjection emits its DISTINCT axis and its per-aggregate
	// result type (sum over an unknown-typed operand stays TypeUnknown).
	out, err := json.Marshal(query.NewAggregateProjection(query.AggSum, []query.Ref{
		{Variable: "n", Property: "num"},
	}, false, query.TypeUnknown{}))
	require.NoError(t, err)
	require.JSONEq(t,
		`{"kind":"aggregate","func":"sum","refs":[{"variable":"n","property":"num"}],"distinct":false,"type":"unknown"}`,
		string(out))
}

func TestAggregateProjectionMarshalJSONCountStar(t *testing.T) {
	// Stage 10: count(*) marshals AggCount with a null refs array (the always-emit
	// posture the other sums follow for nil slices) and TypeInt as its
	// unconditional result type.
	out, err := json.Marshal(query.NewAggregateProjection(query.AggCount, nil, false, query.TypeInt{}))
	require.NoError(t, err)
	require.JSONEq(t, `{"kind":"aggregate","func":"count","refs":null,"distinct":false,"type":"int"}`, string(out))
}

func TestAggregateProjectionMarshalJSONDistinct(t *testing.T) {
	// Stage 10: the DISTINCT axis is always emitted, so it is present as
	// "distinct":true here (and false in every other test).
	out, err := json.Marshal(query.NewAggregateProjection(query.AggCollect, []query.Ref{
		{Variable: "n", Property: "name"},
	}, true, query.NewTypeList(query.TypeUnknown{})))
	require.NoError(t, err)
	require.JSONEq(t,
		`{"kind":"aggregate","func":"collect","refs":[{"variable":"n","property":"name"}],"distinct":true,"type":"list<unknown>"}`,
		string(out))
}

func TestReturnItemMarshalJSON(t *testing.T) {
	// A ReturnItem marshals one level deep: lowercase "name" and "value", the
	// value carrying its own "kind" discriminator (§5). Stage 6: the projection
	// now emits its result type as "type" — always emitted.
	item := query.ReturnItem{
		Name:  "name",
		Value: query.NewRefProjection(query.Ref{Variable: "p", Property: "name"}, query.TypeUnknown{}),
	}
	out, err := json.Marshal(item)
	require.NoError(t, err)
	require.JSONEq(t,
		`{"name":"name","value":{"kind":"ref","variable":"p","property":"name","type":"unknown"}}`,
		string(out))
}

func TestPartMarshalJSONEmitsReturnsAll(t *testing.T) {
	// "returnsAll" is always emitted (no omitempty) on a part, matching the
	// always-emit convention. A plain part (no RETURN *) serialises it as false.
	part := query.Part{
		Returns: []query.ReturnItem{
			{Name: "n", Value: query.NewRefProjection(query.Ref{Variable: "n"}, query.TypeNode{})},
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
		Branches: []query.Branch{
			{Parts: []query.Part{{
				Bindings: []query.Binding{must(query.NewNodeBinding("a", nil))},
				Returns:  []query.ReturnItem{{Name: "a", Value: query.NewRefProjection(query.Ref{Variable: "a"}, query.TypeNode{})}},
			}}},
			{Parts: []query.Part{{
				Bindings: []query.Binding{must(query.NewNodeBinding("b", nil))},
				Returns:  []query.ReturnItem{{Name: "b", Value: query.NewRefProjection(query.Ref{Variable: "b"}, query.TypeNode{})}},
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
		Branches: []query.Branch{
			{Parts: []query.Part{{
				Bindings: []query.Binding{a, b, edge},
				Returns: []query.ReturnItem{
					{Name: "a", Value: query.NewRefProjection(query.Ref{Variable: "a"}, query.TypeNode{})},
					{Name: "a.name", Value: query.NewRefProjection(query.Ref{Variable: "a", Property: "name"}, query.TypeUnknown{})},
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
	src := must(query.NewVarEndpoint("a"))
	tgt := must(query.NewVarEndpoint("b"))
	e, err := query.NewNullableEdgeBinding("r", nil, src, tgt, true)
	require.NoError(t, err)

	outA := must(json.Marshal(a))
	outB := must(json.Marshal(b))
	outE := must(json.Marshal(e))
	require.Contains(t, string(outA), `"nullable":false`)
	require.Contains(t, string(outB), `"nullable":true`)
	require.Contains(t, string(outE), `"nullable":true`)
}

// TestOptionalGroupZeroOnLegacyConstructors pins the ay9 zero-value posture:
// every one of the six preserved binding constructors yields OptionalGroup 0
// — including the nullable-without-group legacy state, which the resolver
// treats exactly as pre-ay9 (nullable, no sibling propagation).
func TestOptionalGroupZeroOnLegacyConstructors(t *testing.T) {
	src := must(query.NewVarEndpoint("a"))
	tgt := must(query.NewVarEndpoint("b"))
	hops := must(query.NewEdgeHops(nil, nil))

	require.Equal(t, 0, must(query.NewNodeBinding("p", nil)).OptionalGroup())
	require.Equal(t, 0, must(query.NewNullableNodeBinding("p", nil)).OptionalGroup())
	require.Equal(t, 0, must(query.NewEdgeBinding("r", nil, src, tgt, true)).OptionalGroup())
	require.Equal(t, 0, must(query.NewNullableEdgeBinding("r", nil, src, tgt, true)).OptionalGroup())
	require.Equal(t, 0, must(query.NewVarLengthEdgeBinding("r", nil, src, tgt, true, hops)).OptionalGroup())
	require.Equal(t, 0, must(query.NewNullableVarLengthEdgeBinding("r", nil, src, tgt, true, hops)).OptionalGroup())
}

// TestMarshalJSONEmitsOptionalGroup pins the group-carrying wire fragment
// (ay9): the key is the tail key — after "nullable" on a node, after "hops"
// on an edge.
func TestMarshalJSONEmitsOptionalGroup(t *testing.T) {
	n := must(query.NewNullableNodeBindingInGroup("p", nil, 2))
	outN := must(json.Marshal(n))
	require.Contains(t, string(outN), `"nullable":true,"optionalGroup":2`)

	src := must(query.NewVarEndpoint("a"))
	tgt := must(query.NewVarEndpoint("b"))
	e := must(query.NewNullableEdgeBindingInGroup("r", nil, src, tgt, true, 1))
	outE := must(json.Marshal(e))
	require.Contains(t, string(outE), `"hops":null,"optionalGroup":1`)
}

// TestMarshalJSONOmitsZeroOptionalGroup is the wire-compat fence in unit
// form (ay9): a group-0 binding — every preserved constructor, nullable
// included — serialises without the "optionalGroup" key, so pre-ay9 goldens
// stay byte-identical.
func TestMarshalJSONOmitsZeroOptionalGroup(t *testing.T) {
	src := must(query.NewVarEndpoint("a"))
	tgt := must(query.NewVarEndpoint("b"))

	for _, b := range []query.Binding{
		must(query.NewNodeBinding("p", nil)),
		must(query.NewNullableNodeBinding("p", nil)),
		must(query.NewEdgeBinding("r", nil, src, tgt, true)),
	} {
		out := must(json.Marshal(b))
		require.NotContains(t, string(out), `"optionalGroup"`)
	}
}

// TestNodeBindingReferencedInRequiredBarePatternZeroDefault pins the 5xg
// zero-value posture on the node side: every one of the three preserved
// node constructors (pre-ay9 + ay9 InGroup) yields
// ReferencedInRequiredBarePattern() == false. Bare-ref demotion is a
// derived post-introduction fact; the constructors don't take the flag.
func TestNodeBindingReferencedInRequiredBarePatternZeroDefault(t *testing.T) {
	require.False(t, must(query.NewNodeBinding("p", nil)).ReferencedInRequiredBarePattern())
	require.False(t, must(query.NewNullableNodeBinding("p", nil)).ReferencedInRequiredBarePattern())
	require.False(t, must(query.NewNullableNodeBindingInGroup("p", nil, 1)).ReferencedInRequiredBarePattern())
}

// TestEdgeBindingReferencedInRequiredBarePatternZeroDefault pins the 5xg
// zero-value posture on the edge side: every one of the six preserved
// edge constructors yields ReferencedInRequiredBarePattern() == false.
// The edge-side true value is grammatically-unreachable (§2.3, §2.5); the
// zero-value pin fences the wire-compat guarantee for the parser corpus.
func TestEdgeBindingReferencedInRequiredBarePatternZeroDefault(t *testing.T) {
	src := must(query.NewVarEndpoint("a"))
	tgt := must(query.NewVarEndpoint("b"))
	hops := must(query.NewEdgeHops(nil, nil))

	require.False(t, must(query.NewEdgeBinding("r", nil, src, tgt, true)).ReferencedInRequiredBarePattern())
	require.False(t, must(query.NewNullableEdgeBinding("r", nil, src, tgt, true)).ReferencedInRequiredBarePattern())
	require.False(t, must(query.NewNullableEdgeBindingInGroup("r", nil, src, tgt, true, 1)).ReferencedInRequiredBarePattern())
	require.False(t, must(query.NewVarLengthEdgeBinding("r", nil, src, tgt, true, hops)).ReferencedInRequiredBarePattern())
	require.False(t, must(query.NewNullableVarLengthEdgeBinding("r", nil, src, tgt, true, hops)).ReferencedInRequiredBarePattern())
	require.False(t, must(query.NewNullableVarLengthEdgeBindingInGroup("r", nil, src, tgt, true, hops, 1)).ReferencedInRequiredBarePattern())
}

// TestMarshalJSONOmitsReferencedInRequiredBarePatternWhenFalse is the 5xg
// wire-compat fence in unit form: a binding whose bare-ref flag is false
// — every preserved constructor's output — serialises without the
// "referencedInRequiredBarePattern" key, so every non-bare-re-referenced
// parser golden stays byte-identical.
func TestMarshalJSONOmitsReferencedInRequiredBarePatternWhenFalse(t *testing.T) {
	src := must(query.NewVarEndpoint("a"))
	tgt := must(query.NewVarEndpoint("b"))
	hops := must(query.NewEdgeHops(nil, nil))

	for _, b := range []query.Binding{
		must(query.NewNodeBinding("p", nil)),
		must(query.NewNullableNodeBinding("p", nil)),
		must(query.NewNullableNodeBindingInGroup("p", nil, 1)),
		must(query.NewEdgeBinding("r", nil, src, tgt, true)),
		must(query.NewNullableEdgeBinding("r", nil, src, tgt, true)),
		must(query.NewNullableEdgeBindingInGroup("r", nil, src, tgt, true, 1)),
		must(query.NewVarLengthEdgeBinding("r", nil, src, tgt, true, hops)),
		must(query.NewNullableVarLengthEdgeBinding("r", nil, src, tgt, true, hops)),
		must(query.NewNullableVarLengthEdgeBindingInGroup("r", nil, src, tgt, true, hops, 1)),
	} {
		out := must(json.Marshal(b))
		require.NotContains(t, string(out), `"referencedInRequiredBarePattern"`)
	}
}

// TestMarshalJSONEmitsReferencedInRequiredBarePatternWhenTrue pins the
// bare-ref-carrying wire fragment (5xg): the key is the tail key —
// after "optionalGroup" on a node, after "optionalGroup" on an edge. The
// parser-side mark path (query.MarkNodeBindingReferencedInRequiredBarePattern)
// is the same symbol build.go uses; a round-trip through the mark keeps
// wire and Go in lockstep.
func TestMarshalJSONEmitsReferencedInRequiredBarePatternWhenTrue(t *testing.T) {
	n := must(query.NewNullableNodeBindingInGroup("b", nil, 1))
	query.MarkNodeBindingReferencedInRequiredBarePattern(&n)
	require.True(t, n.ReferencedInRequiredBarePattern())
	outN := must(json.Marshal(n))
	require.Contains(t, string(outN), `"optionalGroup":1,"referencedInRequiredBarePattern":true`)

	src := must(query.NewVarEndpoint("a"))
	tgt := must(query.NewVarEndpoint("b"))
	e := must(query.NewNullableEdgeBindingInGroup("r", nil, src, tgt, true, 1))
	query.MarkEdgeBindingReferencedInRequiredBarePattern(&e)
	require.True(t, e.ReferencedInRequiredBarePattern())
	outE := must(json.Marshal(e))
	require.Contains(t, string(outE), `"optionalGroup":1,"referencedInRequiredBarePattern":true`)
}

// TestBindingDiscriminatorTracksEntityKind pins the binding "kind" tag to
// graph.EntityKind.String, the single source it derives from, so the serialised
// tag can never drift from Kind().
func TestBindingDiscriminatorTracksEntityKind(t *testing.T) {
	node, err := query.NewNodeBinding("p", nil)
	require.NoError(t, err)
	src := must(query.NewVarEndpoint("a"))
	tgt := must(query.NewVarEndpoint("b"))
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
			Branches: []query.Branch{
				{Parts: []query.Part{{
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

// --- Stage 8: BindingKind, PathBinding, EdgeHops, EdgeBinding.Hops ---

// TestBindingKindString pins the wire tags the JSON "kind" discriminator
// derives from — "node", "edge", "path". Two of the three tags overlap with
// graph.EntityKind (wire-compat); "path" is new at Stage 8.
func TestBindingKindString(t *testing.T) {
	require.Equal(t, "node", query.BindingNode.String())
	require.Equal(t, "edge", query.BindingEdge.String())
	require.Equal(t, "path", query.BindingPath.String())
}

// TestNodeBindingKindReturnsBindingNode pins the Stage-8 interface widening:
// Binding.Kind() returns a BindingKind, not graph.EntityKind. Existing entity
// bindings project onto their matching BindingKind value; a node projects
// onto BindingNode.
func TestNodeBindingKindReturnsBindingNode(t *testing.T) {
	b, err := query.NewNodeBinding("n", nil)
	require.NoError(t, err)
	require.Equal(t, query.BindingNode, b.Kind())
	require.Equal(t, graph.Node, b.EntityKind())
}

// TestEdgeBindingKindReturnsBindingEdge pins the edge side: an edge binding's
// Kind() is BindingEdge; its EntityKind() (only on entity bindings, not on
// PathBinding) is graph.Edge for schema-key formation.
func TestEdgeBindingKindReturnsBindingEdge(t *testing.T) {
	src := must(query.NewVarEndpoint("a"))
	tgt := must(query.NewVarEndpoint("b"))
	b, err := query.NewEdgeBinding("r", nil, src, tgt, true)
	require.NoError(t, err)
	require.Equal(t, query.BindingEdge, b.Kind())
	require.Equal(t, graph.Edge, b.EntityKind())
}

// TestNewPathBinding pins the Stage-8 PathBinding variant: a path variable
// name plus the shape-faithful, tagged-sum list of members. A single-node
// path (MATCH p = (a)) is legal — one NamedNodeMember.
func TestNewPathBinding(t *testing.T) {
	b, err := query.NewPathBinding("p", []query.PathMember{
		must(query.NewNamedNodeMember("a")),
		must(query.NewNamedEdgeMember("r")),
		must(query.NewNamedNodeMember("b")),
	})
	require.NoError(t, err)
	require.Equal(t, "p", b.Variable())
	require.Len(t, b.Members(), 3)
	require.Equal(t, query.BindingNode, b.Members()[0].Kind())
	require.Equal(t, "a", b.Members()[0].Variable())
	require.Equal(t, query.BindingEdge, b.Members()[1].Kind())
	require.Equal(t, "r", b.Members()[1].Variable())
	require.Equal(t, query.BindingNode, b.Members()[2].Kind())
	require.Equal(t, "b", b.Members()[2].Variable())
	require.False(t, b.Members()[0].Anonymous())
	require.Equal(t, query.BindingPath, b.Kind())
	require.False(t, b.Nullable())
	var _ query.Binding = b
}

// TestPathMembersAnonymousVariants pins the two anonymous variants: they
// report their kind (node / edge), an empty Variable(), and Anonymous() true.
// They are what the collector emits for a `-[]-` link and for an
// intermediate `()` node inside a named path.
func TestPathMembersAnonymousVariants(t *testing.T) {
	e := query.AnonEdgeMember{}
	require.Equal(t, query.BindingEdge, e.Kind())
	require.Empty(t, e.Variable())
	require.True(t, e.Anonymous())
	var _ query.PathMember = e

	n := query.AnonNodeMember{}
	require.Equal(t, query.BindingNode, n.Kind())
	require.Empty(t, n.Variable())
	require.True(t, n.Anonymous())
	var _ query.PathMember = n
}

// TestNewNamedMemberRejectsEmpty pins the constructor invariants for the
// named variants: an empty variable is unrepresentable — the anonymous
// variants exist for the empty case.
func TestNewNamedMemberRejectsEmpty(t *testing.T) {
	_, err := query.NewNamedNodeMember("")
	require.Error(t, err)
	_, err = query.NewNamedEdgeMember("")
	require.Error(t, err)
}

// TestNewPathBindingRejectsEmptyVariable pins the invariant: a path with no
// name is not a binding — the parser emits no PathBinding for an unnamed
// pattern. Empty variable is unrepresentable.
func TestNewPathBindingRejectsEmptyVariable(t *testing.T) {
	_, err := query.NewPathBinding("", []query.PathMember{must(query.NewNamedNodeMember("a"))})
	require.Error(t, err)
}

// TestNewPathBindingRejectsEmptyMembers pins the "at least one member"
// invariant: a pattern element grammatically has at least one node, so
// a path binding always references at least one member.
func TestNewPathBindingRejectsEmptyMembers(t *testing.T) {
	_, err := query.NewPathBinding("p", nil)
	require.Error(t, err)
	_, err = query.NewPathBinding("p", []query.PathMember{})
	require.Error(t, err)
}

// TestNewPathBindingRejectsNilMember pins the "no nil member" invariant:
// every member is one of the four tagged-sum variants; a nil in the slice
// is a programmer error the constructor catches.
func TestNewPathBindingRejectsNilMember(t *testing.T) {
	_, err := query.NewPathBinding("p", []query.PathMember{
		must(query.NewNamedNodeMember("a")),
		nil,
	})
	require.Error(t, err)
}

// TestNewPathBindingAllowsSameKindRepeat pins that openCypher's legal
// same-variable revisit (e.g. `MATCH (n)-->(k)<--(n)` inside a named
// path) parses: both occurrences of `n` are NamedNodeMember, they
// agree on Kind() (BindingNode), so the constructor accepts.
func TestNewPathBindingAllowsSameKindRepeat(t *testing.T) {
	b, err := query.NewPathBinding("p", []query.PathMember{
		must(query.NewNamedNodeMember("n")),
		must(query.NewNamedEdgeMember("r1")),
		must(query.NewNamedNodeMember("k")),
		must(query.NewNamedEdgeMember("r2")),
		must(query.NewNamedNodeMember("n")),
	})
	require.NoError(t, err)
	require.Len(t, b.Members(), 5)
}

// TestNewPathBindingRejectsKindConflictOnSameName pins the actual byVar
// integrity invariant: two named members with the same variable but
// disagreeing on Kind() (one node, one edge) would collide with the
// part's byVar — mergeBinding rejects this as a kind conflict at the
// pattern level; the constructor also rejects it defensively so a
// hand-constructed PathBinding cannot express the illegal state.
func TestNewPathBindingRejectsKindConflictOnSameName(t *testing.T) {
	_, err := query.NewPathBinding("p", []query.PathMember{
		must(query.NewNamedNodeMember("x")),
		must(query.NewNamedEdgeMember("x")),
	})
	require.Error(t, err)
}

// TestNewPathBindingAllowsRepeatedAnonymousMembers pins the shape-faithful
// case: a chain with several anonymous edges (`p = (a)-[]-()-[]-(b)`)
// records every AnonEdgeMember / AnonNodeMember, and the duplicate-name
// check does not apply to them.
func TestNewPathBindingAllowsRepeatedAnonymousMembers(t *testing.T) {
	b, err := query.NewPathBinding("p", []query.PathMember{
		must(query.NewNamedNodeMember("a")),
		query.AnonEdgeMember{},
		query.AnonNodeMember{},
		query.AnonEdgeMember{},
		must(query.NewNamedNodeMember("b")),
	})
	require.NoError(t, err)
	require.Len(t, b.Members(), 5)
}

// TestPathBindingMarshalJSON pins the wire shape: kind="path", the variable,
// the members array as tagged sums (named-node / named-edge / anon-node /
// anon-edge), and the always-emitted nullable flag (false at Stage 8).
// The two anonymous variants use distinct discriminators (`anon-node` /
// `anon-edge`) so a consumer never confuses an anonymous slot with a named
// member of an empty variable.
func TestPathBindingMarshalJSON(t *testing.T) {
	b := must(query.NewPathBinding("p", []query.PathMember{
		must(query.NewNamedNodeMember("a")),
		must(query.NewNamedEdgeMember("r")),
		must(query.NewNamedNodeMember("b")),
	}))
	out, err := json.Marshal(b)
	require.NoError(t, err)
	require.JSONEq(t,
		`{"kind":"path","variable":"p","members":[`+
			`{"kind":"node","variable":"a"},`+
			`{"kind":"edge","variable":"r"},`+
			`{"kind":"node","variable":"b"}`+
			`],"nullable":false}`,
		string(out))
}

// TestPathBindingMarshalJSONShapeFaithful pins the anonymous-variant wire
// shape and the shape-faithful ordering for a chain with intermediate
// anonymous elements.
func TestPathBindingMarshalJSONShapeFaithful(t *testing.T) {
	b := must(query.NewPathBinding("p", []query.PathMember{
		must(query.NewNamedNodeMember("a")),
		query.AnonEdgeMember{},
		query.AnonNodeMember{},
		query.AnonEdgeMember{},
		must(query.NewNamedNodeMember("b")),
	}))
	out, err := json.Marshal(b)
	require.NoError(t, err)
	require.JSONEq(t,
		`{"kind":"path","variable":"p","members":[`+
			`{"kind":"node","variable":"a"},`+
			`{"kind":"anon-edge"},`+
			`{"kind":"anon-node"},`+
			`{"kind":"anon-edge"},`+
			`{"kind":"node","variable":"b"}`+
			`],"nullable":false}`,
		string(out))
}

// TestNewEdgeHopsUnbounded pins the [*] case: both bounds nil.
func TestNewEdgeHopsUnbounded(t *testing.T) {
	h, err := query.NewEdgeHops(nil, nil)
	require.NoError(t, err)
	require.Nil(t, h.Min())
	require.Nil(t, h.Max())
}

// TestNewEdgeHopsBounded pins the [*1..3] case: min and max both set.
func TestNewEdgeHopsBounded(t *testing.T) {
	one, three := 1, 3
	h, err := query.NewEdgeHops(&one, &three)
	require.NoError(t, err)
	require.Equal(t, 1, *h.Min())
	require.Equal(t, 3, *h.Max())
}

// TestNewEdgeHopsLowerOnly pins the [*3..] case: only min set (max unbounded).
func TestNewEdgeHopsLowerOnly(t *testing.T) {
	three := 3
	h, err := query.NewEdgeHops(&three, nil)
	require.NoError(t, err)
	require.Equal(t, 3, *h.Min())
	require.Nil(t, h.Max())
}

// TestNewEdgeHopsUpperOnly pins the [*..5] case: only max set (min unbounded).
func TestNewEdgeHopsUpperOnly(t *testing.T) {
	five := 5
	h, err := query.NewEdgeHops(nil, &five)
	require.NoError(t, err)
	require.Nil(t, h.Min())
	require.Equal(t, 5, *h.Max())
}

// TestNewEdgeHopsRejectsNegative pins the constructor invariant: a negative
// bound is grammatically impossible (openCypher integer literals are
// non-negative), and would misrepresent the "empty range" case.
func TestNewEdgeHopsRejectsNegative(t *testing.T) {
	minusOne := -1
	one := 1
	_, err := query.NewEdgeHops(&minusOne, &one)
	require.Error(t, err)
	_, err = query.NewEdgeHops(&one, &minusOne)
	require.Error(t, err)
}

// TestNewEdgeHopsAllowsEmptyRange pins that a max<min range parses: openCypher
// admits `[*2..1]` grammatically and the TCK's positive scenarios treat it as
// zero-row-yielding at runtime (ADR 0005). The parser records the range as
// written; the engine interprets the empty result.
func TestNewEdgeHopsAllowsEmptyRange(t *testing.T) {
	three, one := 3, 1
	h, err := query.NewEdgeHops(&three, &one)
	require.NoError(t, err)
	require.Equal(t, 3, *h.Min())
	require.Equal(t, 1, *h.Max())
}

// TestNewEdgeHopsAllowsEqualBounds pins the [*3] case (which grammatically
// parses as [*3..3]): min == max is a fixed hop count.
func TestNewEdgeHopsAllowsEqualBounds(t *testing.T) {
	three := 3
	h, err := query.NewEdgeHops(&three, &three)
	require.NoError(t, err)
	require.Equal(t, 3, *h.Min())
	require.Equal(t, 3, *h.Max())
}

// TestNewVarLengthEdgeBinding pins the Stage-8 var-length constructor: an
// edge binding carrying a non-nil Hops. Hops() reads back the range.
func TestNewVarLengthEdgeBinding(t *testing.T) {
	src := must(query.NewVarEndpoint("a"))
	tgt := must(query.NewVarEndpoint("b"))
	hops := must(query.NewEdgeHops(nil, nil))
	b, err := query.NewVarLengthEdgeBinding("r", nil, src, tgt, true, hops)
	require.NoError(t, err)
	require.Equal(t, "r", b.Variable())
	require.NotNil(t, b.Hops())
	require.Nil(t, b.Hops().Min())
	require.Nil(t, b.Hops().Max())
}

// TestEdgeBindingHopsNilForSingleHop pins the Stages 0..7 case: a
// single-hop edge binding has Hops() == nil. The wire encoding "hops":null
// preserves wire compatibility for the pre-Stage-8 shape.
func TestEdgeBindingHopsNilForSingleHop(t *testing.T) {
	src := must(query.NewVarEndpoint("a"))
	tgt := must(query.NewVarEndpoint("b"))
	b, err := query.NewEdgeBinding("r", nil, src, tgt, true)
	require.NoError(t, err)
	require.Nil(t, b.Hops())
}

// TestNewNullableVarLengthEdgeBinding pins the OPTIONAL-introduced variant:
// same var-length invariants as NewVarLengthEdgeBinding, with Nullable set.
func TestNewNullableVarLengthEdgeBinding(t *testing.T) {
	src := must(query.NewVarEndpoint("a"))
	tgt := must(query.NewVarEndpoint("b"))
	one, three := 1, 3
	hops := must(query.NewEdgeHops(&one, &three))
	b, err := query.NewNullableVarLengthEdgeBinding("r", nil, src, tgt, true, hops)
	require.NoError(t, err)
	require.True(t, b.Nullable())
	require.NotNil(t, b.Hops())
	require.Equal(t, 1, *b.Hops().Min())
	require.Equal(t, 3, *b.Hops().Max())
}

// TestNewNullableVarLengthEdgeBindingInGroup pins the ay9 InGroup variant:
// the group id rides alongside the var-length invariants (hops preserved).
func TestNewNullableVarLengthEdgeBindingInGroup(t *testing.T) {
	src := must(query.NewVarEndpoint("a"))
	tgt := must(query.NewVarEndpoint("b"))
	one, three := 1, 3
	hops := must(query.NewEdgeHops(&one, &three))
	b, err := query.NewNullableVarLengthEdgeBindingInGroup("r", nil, src, tgt, true, hops, 4)
	require.NoError(t, err)
	require.True(t, b.Nullable())
	require.Equal(t, 4, b.OptionalGroup())
	require.NotNil(t, b.Hops())
	require.Equal(t, 1, *b.Hops().Min())
	require.Equal(t, 3, *b.Hops().Max())
}

func TestNewNullableVarLengthEdgeBindingInGroupRejectsNonPositiveGroup(t *testing.T) {
	src := must(query.NewVarEndpoint("a"))
	tgt := must(query.NewVarEndpoint("b"))
	hops := must(query.NewEdgeHops(nil, nil))
	_, err := query.NewNullableVarLengthEdgeBindingInGroup("r", nil, src, tgt, true, hops, 0)
	require.Error(t, err)
	_, err = query.NewNullableVarLengthEdgeBindingInGroup("r", nil, src, tgt, true, hops, -1)
	require.Error(t, err)
}

// TestVarLengthEdgeBindingMarshalJSON pins the wire shape: the same fields
// as a single-hop edge plus a "hops" object carrying "min"/"max" (null for
// unbounded).
func TestVarLengthEdgeBindingMarshalJSON(t *testing.T) {
	src := must(query.NewVarEndpoint("a"))
	tgt := must(query.NewVarEndpoint("b"))
	one, three := 1, 3
	hops := must(query.NewEdgeHops(&one, &three))
	b := must(query.NewVarLengthEdgeBinding("r", nil, src, tgt, true, hops))
	out, err := json.Marshal(b)
	require.NoError(t, err)
	// The hops field is a nested object with min/max.
	require.Contains(t, string(out), `"hops":{"min":1,"max":3}`)
	require.Contains(t, string(out), `"kind":"edge"`)
}

// TestVarLengthEdgeBindingMarshalJSONUnbounded pins the [*] case wire shape:
// hops object with both members explicit null.
func TestVarLengthEdgeBindingMarshalJSONUnbounded(t *testing.T) {
	src := must(query.NewVarEndpoint("a"))
	tgt := must(query.NewVarEndpoint("b"))
	hops := must(query.NewEdgeHops(nil, nil))
	b := must(query.NewVarLengthEdgeBinding("r", nil, src, tgt, true, hops))
	out, err := json.Marshal(b)
	require.NoError(t, err)
	require.Contains(t, string(out), `"hops":{"min":null,"max":null}`)
}

// TestEdgeBindingMarshalJSONHopsNullForSingleHop pins the wire back-compat
// for single-hop edges: "hops":null, so pre-Stage-8 goldens still match
// under the always-emit convention (nullable/directed/returnsAll).
func TestEdgeBindingMarshalJSONHopsNullForSingleHop(t *testing.T) {
	src := must(query.NewVarEndpoint("a"))
	tgt := must(query.NewVarEndpoint("b"))
	b := must(query.NewEdgeBinding("r", nil, src, tgt, true))
	out, err := json.Marshal(b)
	require.NoError(t, err)
	require.Contains(t, string(out), `"hops":null`)
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

// --- Stage 9: BindingUnwind, UnwindBinding ---

// TestBindingKindStringUnwind pins the Stage-9 wire tag: "unwind" joins
// the discriminator vocabulary alongside "node"/"edge"/"path". The other
// three tags are unchanged.
func TestBindingKindStringUnwind(t *testing.T) {
	require.Equal(t, "unwind", query.BindingUnwind.String())
}

// TestNewUnwindBinding pins the Stage-9 constructor: a non-empty variable
// and a computed element type. The element type is the Stage-6 result of
// the source expression's list element.
func TestNewUnwindBinding(t *testing.T) {
	b, err := query.NewUnwindBinding("x", query.TypeInt{})
	require.NoError(t, err)
	require.Equal(t, "x", b.Variable())
	require.Equal(t, query.TypeInt{}, b.ElementType())
	require.Equal(t, query.BindingUnwind, b.Kind())
	require.False(t, b.Nullable())
	var _ query.Binding = b
}

// TestNewUnwindBindingRejectsEmptyVariable pins the invariant: UNWIND
// grammatically requires an `AS name`, so an anonymous UnwindBinding is
// unrepresentable — the constructor rejects the empty variable, mirroring
// NodeBinding / PathBinding.
func TestNewUnwindBindingRejectsEmptyVariable(t *testing.T) {
	_, err := query.NewUnwindBinding("", query.TypeInt{})
	require.Error(t, err)
}

// TestNewUnwindBindingNormalisesNilTypeToUnknown pins the "cannot tell"
// posture: a nil ElementType at construction is normalised to TypeUnknown
// (the "cannot tell" case is never a nil Type on the wire). Mirrors
// NewTypeList's convention.
func TestNewUnwindBindingNormalisesNilTypeToUnknown(t *testing.T) {
	b, err := query.NewUnwindBinding("x", nil)
	require.NoError(t, err)
	require.Equal(t, query.TypeUnknown{}, b.ElementType())
}

// TestUnwindBindingElementTypeUnknown pins the honest-posture case: an
// UNWIND source whose Stage-6 type collapses to TypeUnknown (e.g. a
// `range(1, 3)` bare function call, whose result type is TypeUnknown
// since function identity is below the boundary) yields an element
// type of TypeUnknown — the resolver upgrades from the schema.
func TestUnwindBindingElementTypeUnknown(t *testing.T) {
	b, err := query.NewUnwindBinding("x", query.TypeUnknown{})
	require.NoError(t, err)
	require.Equal(t, query.TypeUnknown{}, b.ElementType())
}

// TestUnwindBindingElementTypeList pins that an UNWIND'd list of lists
// (`UNWIND [[1,2],[3,4]] AS x`) yields an element type of `list<int>` —
// the element type is a Type in its own right, so it can nest through
// TypeList. Mirrors the Stage-6 list-typing posture.
func TestUnwindBindingElementTypeList(t *testing.T) {
	b, err := query.NewUnwindBinding("x", query.NewTypeList(query.TypeInt{}))
	require.NoError(t, err)
	require.Equal(t, query.NewTypeList(query.TypeInt{}), b.ElementType())
}

// TestUnwindBindingMarshalJSON pins the wire shape: kind="unwind", the
// variable, the element type as the canonical wire tag, and the
// always-emitted nullable flag (false at Stage 9, matching the discipline
// nullable / directed / hops / returnsAll follow).
func TestUnwindBindingMarshalJSON(t *testing.T) {
	b := must(query.NewUnwindBinding("x", query.TypeInt{}))
	out, err := json.Marshal(b)
	require.NoError(t, err)
	require.JSONEq(t,
		`{"kind":"unwind","variable":"x","elemType":"int","nullable":false}`,
		string(out))
}

// TestUnwindBindingMarshalJSONUnknownType pins the honest-posture wire
// shape: an UnwindBinding whose element type is TypeUnknown emits
// "elemType":"unknown" — no null, no missing key, just the honest tag.
func TestUnwindBindingMarshalJSONUnknownType(t *testing.T) {
	b := must(query.NewUnwindBinding("x", query.TypeUnknown{}))
	out, err := json.Marshal(b)
	require.NoError(t, err)
	require.JSONEq(t,
		`{"kind":"unwind","variable":"x","elemType":"unknown","nullable":false}`,
		string(out))
}

// TestUnwindBindingMarshalJSONNestedListType pins the nested-type wire
// shape: an UNWIND of a list-of-lists yields "elemType":"list<int>",
// composing through the Type sum's stringer.
func TestUnwindBindingMarshalJSONNestedListType(t *testing.T) {
	b := must(query.NewUnwindBinding("x", query.NewTypeList(query.TypeInt{})))
	out, err := json.Marshal(b)
	require.NoError(t, err)
	require.JSONEq(t,
		`{"kind":"unwind","variable":"x","elemType":"list<int>","nullable":false}`,
		string(out))
}

// TestNewCallBinding pins the Stage-14 constructor: a well-formed
// CallBinding round-trips its variable, procedure, sourceField,
// resultType, and nullable flag verbatim, and reports BindingCall as
// its kind.
func TestNewCallBinding(t *testing.T) {
	b, err := query.NewCallBinding("out", "test.my.proc", "out", query.TypeString{}, true)
	require.NoError(t, err)
	require.Equal(t, "out", b.Variable())
	require.Equal(t, "test.my.proc", b.Procedure())
	require.Equal(t, "out", b.SourceField())
	require.Equal(t, query.TypeString{}, b.ResultType())
	require.True(t, b.Nullable())
	require.Equal(t, query.BindingCall, b.Kind())
	var _ query.Binding = b
}

// TestNewCallBindingRejectsEmptyVariable pins the invariant: an
// anonymous YIELD item is grammatically impossible (oC_YieldItem
// requires oC_Variable), so an empty variable is unrepresentable at
// the model boundary.
func TestNewCallBindingRejectsEmptyVariable(t *testing.T) {
	_, err := query.NewCallBinding("", "test.my.proc", "out", query.TypeString{}, false)
	require.Error(t, err)
}

// TestNewCallBindingRejectsEmptyProcedure pins the invariant: the
// procedure name is the registry lookup key, so a CallBinding without
// a procedure is meaningless at codegen.
func TestNewCallBindingRejectsEmptyProcedure(t *testing.T) {
	_, err := query.NewCallBinding("out", "", "out", query.TypeString{}, false)
	require.Error(t, err)
}

// TestNewCallBindingRejectsEmptySourceField pins the invariant: the
// source field is the driver-visible column name; a rename
// (`YIELD out AS x`) still carries a source field, so an empty one
// is unrepresentable.
func TestNewCallBindingRejectsEmptySourceField(t *testing.T) {
	_, err := query.NewCallBinding("x", "test.my.proc", "", query.TypeString{}, false)
	require.Error(t, err)
}

// TestNewCallBindingNormalisesNilTypeToUnknown pins the "cannot tell"
// posture: a nil resultType at construction is normalised to
// TypeUnknown, matching NewUnwindBinding's convention. The bridge
// from procsig.TokenNumber flows through this path (§3.2 of the
// stage-14 spec).
func TestNewCallBindingNormalisesNilTypeToUnknown(t *testing.T) {
	b, err := query.NewCallBinding("out", "test.my.proc", "out", nil, false)
	require.NoError(t, err)
	require.Equal(t, query.TypeUnknown{}, b.ResultType())
}

// TestCallBindingMarshalJSON pins the wire shape: kind="call", every
// field emitted (including sourceField even when equal to variable),
// resultType via the canonical Type wire tag, nullable always-
// emitted. Matches the always-emit discipline other binding variants
// follow.
func TestCallBindingMarshalJSON(t *testing.T) {
	b := must(query.NewCallBinding("out", "test.my.proc", "out", query.TypeString{}, true))
	out, err := json.Marshal(b)
	require.NoError(t, err)
	require.JSONEq(t,
		`{"kind":"call","variable":"out","procedure":"test.my.proc","sourceField":"out","resultType":"string","nullable":true}`,
		string(out))
}

// TestCallBindingMarshalJSONAlias pins that a YIELD-AS rename keeps
// sourceField distinct from variable on the wire — codegen needs
// both names to route the driver-visible column into
// the caller-visible one.
func TestCallBindingMarshalJSONAlias(t *testing.T) {
	b := must(query.NewCallBinding("x", "test.my.proc", "out", query.TypeInt{}, false))
	out, err := json.Marshal(b)
	require.NoError(t, err)
	require.JSONEq(t,
		`{"kind":"call","variable":"x","procedure":"test.my.proc","sourceField":"out","resultType":"int","nullable":false}`,
		string(out))
}

// TestCallBindingMarshalJSONNumberBridgesToUnknown pins the bridge:
// a signature-time NUMBER token maps to TypeUnknown on the wire
// (spec §3.2 / Q3 ruling), so no NUMBER identity leaks into
// query.Type. The registry stays the source of
// truth for NUMBER's assignable-from semantics.
func TestCallBindingMarshalJSONNumberBridgesToUnknown(t *testing.T) {
	b := must(query.NewCallBinding("out", "test.my.proc", "out", query.TypeUnknown{}, true))
	out, err := json.Marshal(b)
	require.NoError(t, err)
	require.JSONEq(t,
		`{"kind":"call","variable":"out","procedure":"test.my.proc","sourceField":"out","resultType":"unknown","nullable":true}`,
		string(out))
}

// TestBindingCallKindString pins the wire discriminator: "call". The
// stringer is the single source the JSON kind field derives from, so
// this test locks the discriminator against silent renames.
func TestBindingCallKindString(t *testing.T) {
	require.Equal(t, "call", query.BindingCall.String())
}

// TestNewCallBindingWithArgsRoundTrip pins the 0ig additive axis: the
// args slice supplied at construction is retained verbatim and
// returned by Args(). Verifies the shared-slice convention: two
// bindings from the same CALL clause share one slice value.
func TestNewCallBindingWithArgsRoundTrip(t *testing.T) {
	args := []query.CallArg{
		query.NewCallArg(query.TypeString{}),
		query.NewCallArg(query.TypeInt{}),
	}
	a, err := query.NewCallBindingWithArgs("city", "test.my.proc", "city", query.TypeString{}, true, args)
	require.NoError(t, err)
	b, err := query.NewCallBindingWithArgs("country_code", "test.my.proc", "country_code", query.TypeInt{}, true, args)
	require.NoError(t, err)
	require.Equal(t, args, a.Args())
	require.Equal(t, args, b.Args())
	require.Equal(t, query.TypeString{}, a.Args()[0].Type())
	require.Equal(t, query.TypeInt{}, a.Args()[1].Type())
}

// TestNewCallBindingArgsNilIsEquivalent pins the zero-value posture:
// the args-less constructor NewCallBinding delegates through
// NewCallBindingWithArgs with a nil args slice, so an omit-when-zero-
// length CallBinding is bit-for-bit indistinguishable from the pre-0ig
// wire under reflect.DeepEqual.
func TestNewCallBindingArgsNilIsEquivalent(t *testing.T) {
	pre := must(query.NewCallBinding("out", "test.my.proc", "out", query.TypeString{}, true))
	post := must(query.NewCallBindingWithArgs("out", "test.my.proc", "out", query.TypeString{}, true, nil))
	require.Equal(t, pre, post)
	require.Nil(t, post.Args())
}

// TestNewCallArgNormalisesNilTypeToUnknown pins the CallArg
// constructor's "cannot tell" fallback: nil input becomes TypeUnknown,
// mirroring NewCallBinding's resultType normalisation. Guards against
// a bare-nil Type ever reaching MarshalJSON.
func TestNewCallArgNormalisesNilTypeToUnknown(t *testing.T) {
	a := query.NewCallArg(nil)
	require.Equal(t, query.TypeUnknown{}, a.Type())
}

// TestCallBindingMarshalJSONWithArgs pins the wire shape under the
// 0ig axis: args emits as a JSON array with one object per position,
// each carrying a canonical Type wire tag. Verifies the omit-when-
// zero-length convention: the args key IS present when the slice
// has length ≥ 1.
func TestCallBindingMarshalJSONWithArgs(t *testing.T) {
	b := must(query.NewCallBindingWithArgs("out", "test.my.proc", "out", query.TypeString{}, true, []query.CallArg{
		query.NewCallArg(query.TypeInt{}),
		query.NewCallArg(query.TypeString{}),
	}))
	out, err := json.Marshal(b)
	require.NoError(t, err)
	require.JSONEq(t,
		`{"kind":"call","variable":"out","procedure":"test.my.proc","sourceField":"out","resultType":"string","nullable":true,"args":[{"type":"int"},{"type":"string"}]}`,
		string(out))
}

// TestCallBindingMarshalJSONZeroArgsOmitsKey pins the omit-when-zero-
// length convention on the zero-args (pre-0ig-shape) path: a nil args
// slice serialises with the "args" key ABSENT — matching the pre-0ig
// wire byte-for-byte. This is the fence that makes the 4 zero-arg
// goldens (§4.1.3.1) byte-identical under the axis.
func TestCallBindingMarshalJSONZeroArgsOmitsKey(t *testing.T) {
	b := must(query.NewCallBinding("label", "test.labels", "label", query.TypeString{}, true))
	out, err := json.Marshal(b)
	require.NoError(t, err)
	require.JSONEq(t,
		`{"kind":"call","variable":"label","procedure":"test.labels","sourceField":"label","resultType":"string","nullable":true}`,
		string(out))
}

// TestCallArgMarshalJSON pins the CallArg wire shape: {"type": <wire
// tag>} — atomic; unaware of position (position IS the slice index).
func TestCallArgMarshalJSON(t *testing.T) {
	a := query.NewCallArg(query.TypeInt{})
	out, err := json.Marshal(a)
	require.NoError(t, err)
	require.JSONEq(t, `{"type":"int"}`, string(out))
}

// TestCallArgMarshalJSONUnknown pins the TypeUnknown case: a $param
// or n.name argument mines to TypeUnknown at parse time, and its
// wire tag is "unknown" (same tag CallBinding.resultType uses for the
// NUMBER bridge — no new vocabulary).
func TestCallArgMarshalJSONUnknown(t *testing.T) {
	a := query.NewCallArg(query.TypeUnknown{})
	out, err := json.Marshal(a)
	require.NoError(t, err)
	require.JSONEq(t, `{"type":"unknown"}`, string(out))
}
