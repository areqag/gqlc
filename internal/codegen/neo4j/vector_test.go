package neo4j_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/codegen"
	"github.com/areqag/gqlc/internal/codegen/neo4j"
	"github.com/areqag/gqlc/internal/graph"
	"github.com/areqag/gqlc/internal/schema"
)

func vectorOf(t *testing.T, elem graph.PropertyType) graph.PropertyType {
	t.Helper()
	vec, ok := graph.VectorOf(elem)
	require.True(t, ok, "graph.VectorOf refused %s", elem)
	return vec
}

// TestVectorCarrierIsASliceOnV6Alone pins the per-major answer ADR 0051
// records: neo4j-go-v6 carries every vector as a plain slice of its element's
// own Go type, and neo4j-go-v5, whose driver has no dbtype.Vector, carries
// none. The six rows are the whole of graph.VectorElements, so the table is
// held to the admitted set rather than to a sample of it.
func TestVectorCarrierIsASliceOnV6Alone(t *testing.T) {
	want := map[graph.PropertyType]string{
		graph.TypeInt8:    "[]int8",
		graph.TypeInt16:   "[]int16",
		graph.TypeInt32:   "[]int32",
		graph.TypeInt64:   "[]int64",
		graph.TypeFloat32: "[]float32",
		graph.TypeFloat64: "[]float64",
	}
	require.Len(t, want, len(graph.VectorElements), "a row per admitted element, no more and no fewer")
	for _, elem := range graph.VectorElements {
		vec := vectorOf(t, elem)
		got, ok := neo4j.TargetV6.Types().Property(vec)
		require.True(t, ok, "neo4j-go-v6 refused %s", vec)
		require.Equal(t, want[elem], got, "neo4j-go-v6 carrier of %s", vec)

		got, ok = neo4j.TargetV5.Types().Property(vec)
		require.False(t, ok, "neo4j-go-v5 carried %s, which its driver has no type for", vec)
		require.Empty(t, got)
	}
	// The zero table is the v5 answer, which is the direction that cannot
	// emit a dbtype.Vector a driver does not declare.
	_, ok := neo4j.TypeMap{}.Property(vectorOf(t, graph.TypeFloat32))
	require.False(t, ok, "a bare typeMap{} carries VECTOR; the zero value must refuse it")
}

// TestVectorIsRefusedInsideEveryContainer pins typeMap.contained on the major
// that carries the bare width: a vector as a list element, a record field or
// a union member is refused, so the slice carrier never rides a walk built
// for LIST<T>.
func TestVectorIsRefusedInsideEveryContainer(t *testing.T) {
	vec := vectorOf(t, graph.TypeFloat32)
	for _, pt := range []graph.PropertyType{
		graph.ListOf(vec, true),
		graph.ListOf(vec, false),
		graph.ListOf(graph.ListOf(vec, true), true),
		graph.RecordOf([]graph.RecordField{{Name: "v", Type: vec, NotNull: true}}),
		graph.UnionOf([]graph.UnionMember{{Type: vec}, {Type: graph.TypeString}}),
	} {
		_, ok := neo4j.TargetV6.Types().Property(pt)
		require.False(t, ok, "neo4j-go-v6 carried %s, a vector inside a container", pt)
	}
}

// TestVectorIsStorable holds the storage axis: a vector is a property value
// in its own right on a server that has the type, not an array the
// homogeneity rule could refuse.
func TestVectorIsStorable(t *testing.T) {
	require.True(t, neo4j.TargetV6.Types().StorableProperty(vectorOf(t, graph.TypeInt8)))
}

// TestVectorBindsThroughTheBridge pins the two encode spellings. A vector bound
// bare would reach the driver as a []T and be packed as a LIST, which the
// server stores as a list; only fromVector makes it a Bolt VECTOR.
func TestVectorBindsThroughTheBridge(t *testing.T) {
	vec := vectorOf(t, graph.TypeFloat32)
	require.Equal(t, "fromVector(arg)",
		neo4j.ParamBindExpr(codegen.Param{GoType: "[]float32", Width: vec}, "arg"))
	require.Equal(t, "fromVectorPtr(arg.V)",
		neo4j.ParamBindExpr(codegen.Param{GoType: "[]float32", Width: vec, Nullable: true}, "arg.V"))
	// The control: the same Go type over a LIST width is not a vector.
	require.NotContains(t,
		neo4j.ParamBindExpr(codegen.Param{GoType: "[]float32", Width: graph.ListOf(graph.TypeFloat32, true)}, "arg"),
		"Vector")
}

// TestV5RefusesAVectorUnderItsOwnSentinel is the driver-version refusal end
// to end through Generate: v5 answers ErrUnrepresentableOnDriverVersion naming
// the major that does carry it, and NOT the width sentinel, whose message
// would send an author to a different backend.
func TestV5RefusesAVectorUnderItsOwnSentinel(t *testing.T) {
	labels := graph.LabelSetKey("Doc")
	in := codegen.Input{Schema: schema.Schema{
		Name: "Vectors",
		Nodes: map[graph.LabelSetKey]schema.NodeType{labels: {
			KeyLabels: labels, CompleteLabels: labels, Name: "Doc",
			Properties: map[string]schema.Property{
				"embedding": {Name: "embedding", Type: vectorOf(t, graph.TypeFloat32)},
			},
		}},
	}}

	_, err := neo4j.New(neo4j.WithDriverVersion(neo4j.DriverV5)).Generate(in)
	require.ErrorIs(t, err, neo4j.ErrUnrepresentableOnDriverVersion)
	require.NotErrorIs(t, err, codegen.ErrUnrepresentableWidth,
		"the v5 refusal also cites the width sentinel, which says no carrier exists anywhere in this backend")
	require.ErrorContains(t, err, "neo4j-go-v6")

	// v6 emits the same batch. No query decodes the entity, so no helper
	// is called and vector_neo4j.go is not emitted: each helper is gated
	// on a call site, and the file on any helper.
	files, err := neo4j.New(neo4j.WithDriverVersion(neo4j.DriverV6)).Generate(in)
	require.NoError(t, err)
	paths := make([]string, 0, len(files))
	for _, f := range files {
		paths = append(paths, f.Path)
	}
	require.NotContains(t, paths, "vector_neo4j.go")
}
