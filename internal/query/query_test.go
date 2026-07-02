package query_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/antranig-yeretzian/gqlc/internal/graph"
	"github.com/antranig-yeretzian/gqlc/internal/query"
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

	b, err := query.NewEdgeBinding("r", graph.LabelSet{"KNOWS"}, src, tgt)
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

	b, err := query.NewEdgeBinding("", nil, src, tgt)
	require.NoError(t, err)
	require.Empty(t, b.Variable())
	require.Empty(t, b.Labels())
}

func TestNewEdgeBindingRejectsMissingEndpoint(t *testing.T) {
	tgt, err := query.NewVarEndpoint("b")
	require.NoError(t, err)

	// A nil source endpoint (the interface zero value) is illegal.
	_, err = query.NewEdgeBinding("r", nil, nil, tgt)
	require.Error(t, err)

	// A nil target endpoint is illegal.
	_, err = query.NewEdgeBinding("r", nil, tgt, nil)
	require.Error(t, err)
}

func TestNewEdgeBindingDefaultsToNonNullable(t *testing.T) {
	src, _ := query.NewVarEndpoint("a")
	tgt, _ := query.NewVarEndpoint("b")
	b, err := query.NewEdgeBinding("r", nil, src, tgt)
	require.NoError(t, err)
	require.False(t, b.Nullable())
}

func TestNewNullableEdgeBinding(t *testing.T) {
	src, _ := query.NewVarEndpoint("a")
	tgt, _ := query.NewVarEndpoint("b")
	b, err := query.NewNullableEdgeBinding("r", graph.LabelSet{"KNOWS"}, src, tgt)
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
	b, err := query.NewNullableEdgeBinding("", nil, src, tgt)
	require.NoError(t, err)
	require.Empty(t, b.Variable())
	require.True(t, b.Nullable())
}

func TestNewNullableEdgeBindingRejectsMissingEndpoint(t *testing.T) {
	tgt, _ := query.NewVarEndpoint("b")
	_, err := query.NewNullableEdgeBinding("r", nil, nil, tgt)
	require.Error(t, err)
	_, err = query.NewNullableEdgeBinding("r", nil, tgt, nil)
	require.Error(t, err)
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
	edge, err := query.NewEdgeBinding("r", nil, src, tgt)
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

// --- deterministic JSON marshalling ---

// representativeQuery exercises both binding variants and both endpoint variants,
// plus refs, return items and parameters.
func representativeQuery(t *testing.T) query.Query {
	t.Helper()

	a, err := query.NewNodeBinding("a", graph.LabelSet{"Person"})
	require.NoError(t, err)
	b, err := query.NewNodeBinding("b", nil)
	require.NoError(t, err)

	srcVar, err := query.NewVarEndpoint("a")
	require.NoError(t, err)
	tgtInline := query.NewInlineEndpoint(graph.LabelSet{"Company"})
	edge, err := query.NewEdgeBinding("r", graph.LabelSet{"WORKS_AT"}, srcVar, tgtInline)
	require.NoError(t, err)

	return query.Query{
		Bindings: []query.Binding{a, b, edge},
		Parameters: []query.Parameter{
			{Name: "id", Uses: []query.Use{
				query.NewPropertyUse(query.Ref{Variable: "a", Property: "id"}),
			}},
		},
		Returns: []query.ReturnItem{
			{Name: "a", Ref: query.Ref{Variable: "a"}},
			{Name: "a.name", Ref: query.Ref{Variable: "a", Property: "name"}},
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
	e, err := query.NewNullableEdgeBinding("r", nil, src, tgt)
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
	edge, err := query.NewEdgeBinding("r", nil, src, tgt)
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
			Bindings: rapid.SliceOf(genBinding()).Draw(t, "bindings"),
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
