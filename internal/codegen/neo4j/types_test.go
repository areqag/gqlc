package neo4j

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/graph"
	"github.com/areqag/gqlc/internal/resolver"
)

// TestTypeMapProperty pins the driver's Go-type table for the property
// axis (spec §5.1): 21 representable widths each map to their Go
// carrier, the eight unrepresentable widths report ok=false so the
// caller routes to ErrUnrepresentableWidth, and a list width recurses
// element-wise. A width added to internal/graph without a row here
// leaves the table silently unswept.
func TestTypeMapProperty(t *testing.T) {
	representable := []struct {
		pt   graph.PropertyType
		want string
	}{
		{graph.TypeString, "string"},
		{graph.TypeBytes, "[]byte"},
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
		{graph.TypeDate, "dbtype.Date"},
		{graph.TypeTime, "dbtype.Time"},
		{graph.TypeLocalTime, "dbtype.LocalTime"},
		{graph.TypeTimestamp, "time.Time"},
		{graph.TypeDuration, "dbtype.Duration"},
	}
	require.Len(t, representable, 21)
	for _, tt := range representable {
		t.Run("representable/"+string(tt.pt), func(t *testing.T) {
			got, ok := typeMap{}.Property(tt.pt)
			require.True(t, ok)
			require.Equal(t, tt.want, got)
		})
	}

	unrepresentable := []graph.PropertyType{
		graph.TypeInt128, graph.TypeInt256,
		graph.TypeUint128, graph.TypeUint256,
		graph.TypeFloat16, graph.TypeFloat128, graph.TypeFloat256,
		graph.TypeDecimal,
	}
	require.Len(t, unrepresentable, 8)
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
}

// TestTypeMapTemporal pins the temporal column-shape row of the table
// (spec §5.1): five dbtype carriers plus time.Time for the zoned
// datetime.
func TestTypeMapTemporal(t *testing.T) {
	tests := []struct {
		k    resolver.Temporal
		want string
	}{
		{resolver.TemporalDate, "dbtype.Date"},
		{resolver.TemporalTime, "dbtype.Time"},
		{resolver.TemporalLocalTime, "dbtype.LocalTime"},
		{resolver.TemporalDateTime, "time.Time"},
		{resolver.TemporalLocalDateTime, "dbtype.LocalDateTime"},
		{resolver.TemporalDuration, "dbtype.Duration"},
	}
	require.Len(t, tests, 6)
	for _, tt := range tests {
		t.Run(tt.k.String(), func(t *testing.T) {
			require.Equal(t, tt.want, typeMap{}.Temporal(tt.k))
		})
	}
}

// TestTypeMapScalar pins the scalar column-shape row of the table
// (spec §5.1).
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
}

// TestDriverCarrier pins the widen-on-decode contract: every integer
// family widens to int64, every float family to float64, and every
// other emitted Go type passes through unchanged (so the emission
// sites elide the narrowing conversion).
func TestDriverCarrier(t *testing.T) {
	tests := []struct {
		goType string
		want   string
	}{
		{"int", "int64"},
		{"int8", "int64"},
		{"int16", "int64"},
		{"int32", "int64"},
		{"int64", "int64"},
		{"uint", "int64"},
		{"uint8", "int64"},
		{"uint16", "int64"},
		{"uint32", "int64"},
		{"uint64", "int64"},
		{"float32", "float64"},
		{"float64", "float64"},
		{"string", "string"},
		{"bool", "bool"},
		{"[]byte", "[]byte"},
		{"dbtype.Date", "dbtype.Date"},
		{"time.Time", "time.Time"},
	}
	for _, tt := range tests {
		t.Run(tt.goType, func(t *testing.T) {
			require.Equal(t, tt.want, driverCarrier(tt.goType))
		})
	}
}

// TestAccessModeText asserts the run-call's fourth argument follows the
// committed IsWrite axis (spec §1.1). One row per value; the dispatch
// must never consult the resolver's StatementKind directly.
func TestAccessModeText(t *testing.T) {
	tests := []struct {
		name    string
		isWrite bool
		want    string
	}{
		{"read maps to AccessModeRead", false, "neo4j.AccessModeRead"},
		{"write maps to AccessModeWrite", true, "neo4j.AccessModeWrite"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, accessModeText(tt.isWrite))
		})
	}
}
