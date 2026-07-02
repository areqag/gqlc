package query_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/query"
)

// The Type sum is the freeze-locked type vocabulary the resolver reads (Stage 6
// spec §3). Each variant carries a stringer that is the single source of the
// wire tag, mirroring AggregateFunc / UnionKind.

func TestTypeIntString(t *testing.T) {
	// TypeInt is the integer scalar: an integer literal, arithmetic over integer
	// operands. Its wire tag is "int".
	require.Equal(t, "int", query.TypeInt{}.String())
	var _ query.Type = query.TypeInt{}
}

// TestTypeListString pins TypeList's stringer: "list<" + elem.String() + ">"
// so a typed list is recognisable on the wire ("list<int>"), an untyped list
// falls back to "list<unknown>", and a nested list composes ("list<list<int>>").
func TestTypeListString(t *testing.T) {
	require.Equal(t, "list<int>", query.NewTypeList(query.TypeInt{}).String())
	require.Equal(t, "list<string>", query.NewTypeList(query.TypeString{}).String())
	require.Equal(t, "list<unknown>", query.NewTypeList(query.TypeUnknown{}).String())
	// Nested: composition through the element's stringer.
	require.Equal(t, "list<list<int>>",
		query.NewTypeList(query.NewTypeList(query.TypeInt{})).String())

	var _ query.Type = query.NewTypeList(query.TypeInt{})
}

// TestTypeListElement pins the accessor: the constructor is total and the
// element type is retrievable, so the resolver can walk into a typed list.
func TestTypeListElement(t *testing.T) {
	l := query.NewTypeList(query.TypeInt{})
	require.Equal(t, query.TypeInt{}, l.Element())
}

// TestRefProjectionType pins the Stage-6 accessor: RefProjection carries its
// result type as the fourth exported datum (after variable, property, and the
// Ref shape it already had). A whole-entity node ref types as TypeNode; the
// listener passes the correct type via the widened constructor.
func TestRefProjectionType(t *testing.T) {
	p := query.NewRefProjection(query.Ref{Variable: "n"}, query.TypeNode{})
	require.Equal(t, query.TypeNode{}, p.Type())
}

// TestFuncProjectionType pins the Stage-6 accessor: FuncProjection carries its
// result type. Function identity is below the boundary (ADR 0005), so the
// listener passes TypeUnknown for any function whose return type it cannot
// compute schema-free — which today is every function.
func TestFuncProjectionType(t *testing.T) {
	p := query.NewFuncProjection([]query.Ref{{Variable: "a", Property: "num"}}, query.TypeUnknown{})
	require.Equal(t, query.TypeUnknown{}, p.Type())
}

// TestAggregateProjectionType pins the accessor: AggregateProjection also
// carries a result type, TypeUnknown because the aggregate's return type
// depends on the argument type (below the boundary).
func TestAggregateProjectionType(t *testing.T) {
	p := query.NewAggregateProjection(query.AggSum, []query.Ref{{Variable: "n", Property: "num"}}, query.TypeUnknown{})
	require.Equal(t, query.TypeUnknown{}, p.Type())
}

// TestLiteralProjectionType pins the Stage-6 accessor: LiteralProjection carries
// its scalar literal kind as its only exported datum. A boolean literal types as
// TypeBool; an integer as TypeInt; the null literal as TypeNull; and so on. The
// listener computes the type at classification time from the grammar node.
func TestLiteralProjectionType(t *testing.T) {
	for _, tc := range []struct {
		lit  query.LiteralProjection
		want query.Type
	}{
		{query.NewLiteralProjection(query.TypeBool{}), query.TypeBool{}},
		{query.NewLiteralProjection(query.TypeInt{}), query.TypeInt{}},
		{query.NewLiteralProjection(query.TypeFloat{}), query.TypeFloat{}},
		{query.NewLiteralProjection(query.TypeString{}), query.TypeString{}},
		{query.NewLiteralProjection(query.TypeNull{}), query.TypeNull{}},
	} {
		require.Equal(t, tc.want, tc.lit.Type())
	}
}

// TestTypeMarshalJSON pins the wire encoding: every Type marshals as its
// stringer value, quoted as a JSON string. The stringer is the single source
// so drift is impossible.
func TestTypeMarshalJSON(t *testing.T) {
	for _, tc := range []struct {
		t    query.Type
		want string
	}{
		{query.TypeBool{}, `"bool"`},
		{query.TypeInt{}, `"int"`},
		{query.TypeFloat{}, `"float"`},
		{query.TypeString{}, `"string"`},
		{query.TypeNull{}, `"null"`},
		{query.TypeMap{}, `"map"`},
		{query.TypeNode{}, `"node"`},
		{query.TypeEdge{}, `"edge"`},
		{query.TypeUnknown{}, `"unknown"`},
		{query.NewTypeList(query.TypeInt{}), `"list<int>"`},
		{query.NewTypeList(query.NewTypeList(query.TypeString{})), `"list<list<string>>"`},
	} {
		out, err := json.Marshal(tc.t)
		require.NoError(t, err)
		require.JSONEq(t, tc.want, string(out))
	}
}

// TestScalarAndEntityTypeString pins the lowercase wire name for every
// non-parameterised Type variant. String is the single source the JSON
// discriminator derives from, so the serialised name can never drift from the
// Go type.
func TestScalarAndEntityTypeString(t *testing.T) {
	for _, tc := range []struct {
		t    query.Type
		want string
	}{
		{query.TypeBool{}, "bool"},
		{query.TypeInt{}, "int"},
		{query.TypeFloat{}, "float"},
		{query.TypeString{}, "string"},
		{query.TypeNull{}, "null"},
		{query.TypeMap{}, "map"},
		{query.TypeNode{}, "node"},
		{query.TypeEdge{}, "edge"},
		{query.TypeUnknown{}, "unknown"},
	} {
		require.Equal(t, tc.want, tc.t.String())
	}
}
