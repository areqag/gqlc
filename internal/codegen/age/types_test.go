package age

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/codegen"
	"github.com/areqag/gqlc/internal/graph"
	"github.com/areqag/gqlc/internal/resolver"
	"github.com/areqag/gqlc/internal/schema"
)

// TestTypeMapProperty pins the property axis of the Go-type table (spec
// §5.1). agtype's scalar vocabulary is narrower than the neo4j driver's,
// so the boundary sits in a different place: BYTES and the five temporal
// widths join the eight oversized numeric widths on the reject side, and
// a caller hitting any of the fourteen gets ErrUnrepresentableWidth
// naming the width. A width added to internal/graph without a row here
// leaves the table silently unswept.
func TestTypeMapProperty(t *testing.T) {
	representable := []struct {
		pt   graph.PropertyType
		want string
	}{
		{graph.TypeString, "string"},
		{graph.TypeBool, "bool"},
		{graph.TypeInt, "int"},
		{graph.TypeInt8, "int8"},
		{graph.TypeInt16, "int16"},
		{graph.TypeInt32, "int32"},
		{graph.TypeInt64, "int64"},
		{graph.TypeUint, "uint"},
		{graph.TypeUint8, "uint8"},
		{graph.TypeUint16, "uint16"},
		{graph.TypeUint32, "uint32"},
		{graph.TypeUint64, "uint64"},
		{graph.TypeFloat, "float64"},
		{graph.TypeFloat32, "float32"},
		{graph.TypeFloat64, "float64"},
		{graph.TypeAnyPropertyValue, "any"},
	}
	require.Len(t, representable, 16)
	for _, tt := range representable {
		t.Run("representable/"+string(tt.pt), func(t *testing.T) {
			got, ok := typeMap{}.Property(tt.pt)
			require.True(t, ok)
			require.Equal(t, tt.want, got)
		})
	}

	unrepresentable := []graph.PropertyType{
		// agtype has no byte-string and no temporal scalar. The wire
		// encoding for the latter is the temporal arm's to commit;
		// admitting them here would emit columns no decoder can fill.
		graph.TypeBytes,
		graph.TypeDate, graph.TypeTime, graph.TypeLocalTime,
		graph.TypeTimestamp, graph.TypeDuration,
		// No faithful Go carrier on any backend (§9).
		graph.TypeInt128, graph.TypeInt256,
		graph.TypeUint128, graph.TypeUint256,
		graph.TypeFloat16, graph.TypeFloat128, graph.TypeFloat256,
		graph.TypeDecimal,
	}
	require.Len(t, unrepresentable, 14)
	for _, pt := range unrepresentable {
		t.Run("unrepresentable/"+string(pt), func(t *testing.T) {
			got, ok := typeMap{}.Property(pt)
			require.False(t, ok)
			require.Empty(t, got)
		})
	}

	t.Run("list recurses element-wise", func(t *testing.T) {
		got, ok := typeMap{}.Property(graph.ListOf(graph.TypeInt32, false))
		require.True(t, ok)
		require.Equal(t, "[]int32", got)
	})

	t.Run("list of unrepresentable element fails", func(t *testing.T) {
		_, ok := typeMap{}.Property(graph.ListOf(graph.TypeDecimal, false))
		require.False(t, ok)
	})

	t.Run("list of temporal element fails", func(t *testing.T) {
		_, ok := typeMap{}.Property(graph.ListOf(graph.TypeTimestamp, false))
		require.False(t, ok)
	})

	t.Run("bare list width", func(t *testing.T) {
		got, ok := typeMap{}.Property(graph.TypeList)
		require.True(t, ok)
		require.Equal(t, "[]any", got)
	})

	// graph.PropertyType is a string type, so a width with no row above
	// costs nothing to construct and compiles. It must reject: the caller
	// turns that into ErrUnrepresentableWidth naming the width.
	t.Run("width outside the table is rejected", func(t *testing.T) {
		got, ok := typeMap{}.Property(graph.PropertyType("QUATERNION"))
		require.False(t, ok)
		require.Empty(t, got)
	})

	t.Run("list of a width outside the table is rejected", func(t *testing.T) {
		got, ok := typeMap{}.Property(graph.ListOf("QUATERNION", false))
		require.False(t, ok)
		require.Empty(t, got)
	})
}

// TestTypeMapPropertyRejectionReachesTheCaller pins the contract the
// ok=false half of the table exists for: generation fails with
// ErrUnrepresentableWidth naming the entity, the property and the width,
// rather than emitting a field nothing can decode. The two widths here
// are the ones AGE rejects and neo4j accepts — the divergence that has
// no corpus fixture, because a fixture directory is valid or invalid for
// every target at once.
func TestTypeMapPropertyRejectionReachesTheCaller(t *testing.T) {
	for _, pt := range []graph.PropertyType{graph.TypeBytes, graph.TypeTimestamp} {
		t.Run(string(pt), func(t *testing.T) {
			files, err := generate(codegen.Input{Schema: schemaWithPayload(pt)}, "age")
			require.ErrorIs(t, err, codegen.ErrUnrepresentableWidth)
			require.ErrorContains(t, err, `entity "Blob" property "payload" has `+string(pt))
			require.Nil(t, files)
		})
	}
}

// schemaWithPayload is a one-node schema whose single property carries
// pt, so the width sweep is the only thing that can fail.
func schemaWithPayload(pt graph.PropertyType) schema.Schema {
	labels := graph.LabelSetKey("Blob")
	return schema.Schema{
		Name: "Widths",
		Nodes: map[graph.LabelSetKey]schema.NodeType{
			labels: {
				KeyLabels:      labels,
				CompleteLabels: labels,
				Name:           "Blob",
				Properties: map[string]schema.Property{
					"payload": {Name: "payload", Type: pt},
				},
			},
		},
	}
}

// TestTypeMapTemporal pins the temporal column-shape row (spec §5.1).
// agtype has no temporal scalar, so every kind projects undecoded until
// the temporal arm commits an encoding — one row per kind so that
// commitment cannot land silently.
func TestTypeMapTemporal(t *testing.T) {
	tests := []struct {
		k    resolver.Temporal
		want string
	}{
		{resolver.TemporalDate, "any"},
		{resolver.TemporalTime, "any"},
		{resolver.TemporalLocalTime, "any"},
		{resolver.TemporalDateTime, "any"},
		{resolver.TemporalLocalDateTime, "any"},
		{resolver.TemporalDuration, "any"},
	}
	require.Len(t, tests, 6)
	for _, tt := range tests {
		t.Run(tt.k.String(), func(t *testing.T) {
			require.Equal(t, tt.want, typeMap{}.Temporal(tt.k))
		})
	}
}

// TestTypeMapScalar pins the scalar column-shape row (spec §5.1), one
// row per agtype scalar.
func TestTypeMapScalar(t *testing.T) {
	tests := []struct {
		k    resolver.Scalar
		want string
	}{
		{resolver.ScalarBool, "bool"},
		{resolver.ScalarInt, "int64"},
		{resolver.ScalarFloat, "float64"},
		{resolver.ScalarString, "string"},
		{resolver.ScalarNull, "any"},
		{resolver.ScalarMap, "map[string]any"},
	}
	require.Len(t, tests, 6)
	for _, tt := range tests {
		t.Run(tt.k.String(), func(t *testing.T) {
			require.Equal(t, tt.want, typeMap{}.Scalar(tt.k))
		})
	}

	// The compiler requires a return past the last arm of a closed-enum
	// switch, so the backstop exists whether or not the resolver can
	// reach it. Pinned so its value is a decision and not an accident.
	t.Run("kind outside the vocabulary projects undecoded", func(t *testing.T) {
		require.Equal(t, "any", typeMap{}.Scalar(resolver.ScalarMap+1))
	})
}
