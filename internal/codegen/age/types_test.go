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
// so the boundary sits in a different place: BYTES, the five temporal
// widths, and the two structured widths join the eight oversized numeric
// widths on the reject side, and a caller hitting any of them gets
// ErrUnrepresentableWidth naming the width. The rows pin each width's
// mapping; the constant set
// they range over is held by Property's switch, which has no default
// arm and so fails the exhaustive linter when internal/graph grows one.
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
	}
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
		// The decode vocabulary is one helper per agtype scalar, so a
		// property whose value is a list or is of no declared shape at
		// all would reach a struct field with no helper to fill it.
		graph.TypeList,
		graph.TypeAnyPropertyValue,
	}
	for _, pt := range unrepresentable {
		t.Run("unrepresentable/"+string(pt), func(t *testing.T) {
			got, ok := typeMap{}.Property(pt)
			require.False(t, ok)
			require.Empty(t, got)
		})
	}

	t.Run("a list is rejected whatever its element", func(t *testing.T) {
		for _, elem := range []graph.PropertyType{graph.TypeInt32, graph.TypeDecimal, graph.TypeTimestamp} {
			got, ok := typeMap{}.Property(graph.ListOf(elem, false))
			require.False(t, ok, "element %s", elem)
			require.Empty(t, got)
		}
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
// ErrUnrepresentableWidth naming the entity, the property, the width, and
// the backend with no carrier for it, rather than emitting a field
// nothing can decode. The widths here are ones AGE rejects and neo4j
// accepts, so a config declaring both targets fails on one of them and
// the backend name is what says which.
func TestTypeMapPropertyRejectionReachesTheCaller(t *testing.T) {
	for _, pt := range []graph.PropertyType{graph.TypeBytes, graph.TypeTimestamp, graph.ListOf(graph.TypeString, false)} {
		t.Run(string(pt), func(t *testing.T) {
			files, err := generate(codegen.Input{Schema: schemaWithPayload(pt)}, "age")
			require.ErrorIs(t, err, codegen.ErrUnrepresentableWidth)
			require.ErrorContains(t, err,
				`entity "Blob" property "payload" has `+string(pt)+`, which the Apache AGE backend has no carrier for`)
			require.Nil(t, files)
		})
	}
}

// TestUnservedQueriesOutrankUnrepresentableWidths pins which of the two
// rejections a batch failing both reports. The width sweep would send
// the author to a schema that was never the obstacle: a query projecting
// a list stays unserved whatever the schema's widths are, so repairing
// them leaves the batch exactly where it was. Only the query rejection
// says so.
func TestUnservedQueriesOutrankUnrepresentableWidths(t *testing.T) {
	files, err := generate(codegen.Input{
		Schema: schemaWithPayload(graph.TypeTimestamp),
		Queries: []codegen.NamedQuery{{
			Name: "Wipe",
			Validated: resolver.ValidatedQuery{
				Statement: resolver.StatementRead,
				Columns: []resolver.Column{{
					Name: "t",
					Type: resolver.ResolvedProperty{Type: graph.ListOf(graph.TypeString, false)},
				}},
			},
		}},
	}, "age")
	require.ErrorIs(t, err, ErrUnsupportedQuery)
	require.NotErrorIs(t, err, codegen.ErrUnrepresentableWidth)
	require.Nil(t, files)
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
// the temporal arm commits an encoding. Temporal returns without
// branching, so the rows exist to make that commitment land as six
// failures rather than a quiet change of column type.
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
