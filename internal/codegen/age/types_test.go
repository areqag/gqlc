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
// a caller hitting any of them gets ErrUnrepresentableWidth naming the
// width. The two structured widths are on the admit side, emitting what
// every other backend emits for them — agtype has a list and a map of
// its own, so a list of a carried element width and Go's any (ADR 0020)
// both have something to decode from. The rows pin each width's
// mapping; the constant set they range over is held by Property's
// switch, which has no default arm and so fails the exhaustive linter
// when internal/graph grows one.
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
		// The two structured widths. TypeList is the bare LIST, which is
		// LIST<ANY> spelled out, so it maps through the list arm.
		{graph.TypeAnyPropertyValue, "any"},
		{graph.TypeList, "[]any"},
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
	}
	for _, pt := range unrepresentable {
		t.Run("unrepresentable/"+string(pt), func(t *testing.T) {
			got, ok := typeMap{}.Property(pt)
			require.False(t, ok)
			require.Empty(t, got)
		})
	}

	// A list is admitted exactly when its element width is, at whatever
	// depth, and the text it produces is the one every other backend
	// produces for the same declaration — the surface a caller writes
	// against does not vary by backend, only what fills it does. An
	// element's NOT NULL qualifier is not part of that text: a Go slice
	// element is not a pointer either way.
	t.Run("a list rides its element's carrier at whatever depth", func(t *testing.T) {
		cases := map[graph.PropertyType]string{
			graph.ListOf(graph.TypeString, false):                                       "[]string",
			graph.ListOf(graph.TypeString, true):                                        "[]string",
			graph.ListOf(graph.TypeInt32, false):                                        "[]int32",
			graph.ListOf(graph.TypeFloat32, false):                                      "[]float32",
			graph.ListOf(graph.TypeAnyPropertyValue, false):                             "[]any",
			graph.ListOf(graph.ListOf(graph.TypeInt64, false), false):                   "[][]int64",
			graph.ListOf(graph.ListOf(graph.ListOf(graph.TypeBool, true), false), true): "[][][]bool",
		}
		for pt, want := range cases {
			got, ok := typeMap{}.Property(pt)
			require.True(t, ok, "%s", pt)
			require.Equal(t, want, got, "%s", pt)
		}
	})

	// A width with no carrier does not acquire one by being wrapped in a
	// list: the elements would reach a slice no helper can fill.
	t.Run("a list of an uncarried element width is rejected", func(t *testing.T) {
		for _, elem := range []graph.PropertyType{graph.TypeDecimal, graph.TypeTimestamp, graph.TypeBytes} {
			for _, pt := range []graph.PropertyType{
				graph.ListOf(elem, false),
				graph.ListOf(graph.ListOf(elem, false), false),
			} {
				got, ok := typeMap{}.Property(pt)
				require.False(t, ok, "%s", pt)
				require.Empty(t, got)
			}
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
// the backend name is what says which. The list row is what says the
// report names the property's declared width and not the element's:
// LIST<TIMESTAMP> is what the author wrote, and TIMESTAMP alone is not
// a line they could go and find.
func TestTypeMapPropertyRejectionReachesTheCaller(t *testing.T) {
	for _, pt := range []graph.PropertyType{graph.TypeBytes, graph.TypeTimestamp, graph.ListOf(graph.TypeTimestamp, false)} {
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
// a list of a width with no carrier stays unserved whatever the rest of
// the schema's widths are, so repairing them leaves the batch exactly
// where it was. Only the query rejection says so.
func TestUnservedQueriesOutrankUnrepresentableWidths(t *testing.T) {
	files, err := generate(codegen.Input{
		Schema: schemaWithPayload(graph.TypeTimestamp),
		Queries: []codegen.NamedQuery{{
			Name: "Wipe",
			Validated: resolver.ValidatedQuery{
				Statement: resolver.StatementRead,
				Columns: []resolver.Column{{
					Name: "t",
					Type: resolver.ResolvedProperty{Type: graph.ListOf(graph.TypeTimestamp, false)},
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

// temporalKinds is the resolver's whole temporal vocabulary, written out
// so the sweeps below range over every kind rather than a sample.
var temporalKinds = []resolver.Temporal{
	resolver.TemporalDate,
	resolver.TemporalTime,
	resolver.TemporalLocalTime,
	resolver.TemporalDateTime,
	resolver.TemporalLocalDateTime,
	resolver.TemporalDuration,
}

// TestTypeMapTemporal pins the temporal column-shape row (spec §5.1).
// agtype has no temporal scalar, so no kind has a carrier here and every
// one of them refuses. The rows exist so the temporal arm's commitment
// lands as six failures rather than a quiet change of column type.
func TestTypeMapTemporal(t *testing.T) {
	for _, k := range temporalKinds {
		t.Run(k.String(), func(t *testing.T) {
			got, ok := typeMap{}.Temporal(k)
			require.False(t, ok)
			require.Empty(t, got)
		})
	}

	// The compiler requires a return past the last arm of a closed-enum
	// switch, so the backstop exists whether or not the resolver can
	// reach it. Pinned so its value is a decision and not an accident:
	// refusing is what stops a kind added upstream from inheriting an
	// answer chosen for the kinds that came before it.
	t.Run("kind outside the vocabulary is refused", func(t *testing.T) {
		got, ok := typeMap{}.Temporal(resolver.TemporalDuration + 1)
		require.False(t, ok)
		require.Empty(t, got)
	})
}

// TestTemporalProjectionIsRefusedNamingTheKind pins the contract the
// ok=false half of the temporal row exists for, at the site that
// consumes it. Without a rejection channel the shared phase has no way
// to be told "no carrier", so it carries the table's answer onto the
// prepared surface whatever that answer is — a column typed `any` that
// no decoder can fill, emitted at no error. The assertion is on that
// observable: the projection must not reach emission at all.
//
// Driven through Prepare rather than through generate so the backend's
// unserved-query gate is not what is being measured. That gate answers
// "does this backend emit a method for this query"; this test is about
// what happens once a temporal reaches the type table, which is the
// state gqlc-35yu.11 creates when it lifts the gate one kind at a time.
func TestTemporalProjectionIsRefusedNamingTheKind(t *testing.T) {
	for _, k := range temporalKinds {
		t.Run(k.String(), func(t *testing.T) {
			in := codegen.Input{
				Schema: schemaWithPayload(graph.TypeString),
				Queries: []codegen.NamedQuery{{
					Name:        "When",
					Cardinality: codegen.CardinalityMany,
					SourceFile:  "q.cypher",
					SourceText:  "MATCH (b:Blob) RETURN date() AS t\n",
					Validated: resolver.ValidatedQuery{
						Columns: []resolver.Column{{
							Name: "t", Type: resolver.ResolvedTemporal{Kind: k},
						}},
					},
				}},
			}
			prepared, err := codegen.Prepare(in, typeMap{}, "age")
			if err == nil {
				t.Fatalf("temporal projection reached emission typed %q, at no error",
					prepared.Queries[0].RowFields[0].GoType)
			}
			require.ErrorIs(t, err, codegen.ErrUnrepresentableTemporal)
			require.EqualError(t, err,
				`unrepresentable temporal kind: query "When" column 0 "t" projects temporal(`+k.String()+`)`)
		})
	}
}

// TestTemporalProjectionNamesThisBackend is the same refusal seen from
// where an author stands: the whole emission, entered the way the CLI
// enters it. The message has to say which backend has no carrier,
// because a config naming several targets fails on one of them and the
// kind alone does not say which — the same reason the width refusal
// carries a backend name.
func TestTemporalProjectionNamesThisBackend(t *testing.T) {
	files, err := generate(codegen.Input{
		Schema: schemaWithPayload(graph.TypeString),
		Queries: []codegen.NamedQuery{{
			Name:        "When",
			Cardinality: codegen.CardinalityMany,
			SourceFile:  "q.cypher",
			SourceText:  "MATCH (b:Blob) RETURN date() AS t\n",
			Validated: resolver.ValidatedQuery{
				Columns: []resolver.Column{{
					Name: "t", Type: resolver.ResolvedTemporal{Kind: resolver.TemporalDate},
				}},
			},
		}},
	}, "age")
	require.ErrorIs(t, err, codegen.ErrUnrepresentableTemporal)
	require.EqualError(t, err,
		`unrepresentable temporal kind: query "When" column 0 "t" projects temporal(date), `+
			`which the Apache AGE backend has no carrier for`)
	require.Nil(t, files)
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
