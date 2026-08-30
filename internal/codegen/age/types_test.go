package age_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/codegen"
	"github.com/areqag/gqlc/internal/codegen/age"
	"github.com/areqag/gqlc/internal/graph"
	"github.com/areqag/gqlc/internal/resolver"
	"github.com/areqag/gqlc/internal/schema"
)

// TestTypeMapProperty pins the property axis of the Go-type table (spec
// §5.1). agtype's scalar vocabulary is narrower than the neo4j driver's,
// so the boundary sits in a different place: BYTES and four of the five
// temporal widths join the eight oversized numeric widths on the reject
// side, and a caller hitting any of them gets ErrUnrepresentableWidth
// naming the width. TIMESTAMP is the one temporal width that crosses, on
// an encoding this package owns. The two structured widths are on the
// admit side, emitting what every other backend emits for them — agtype
// has a list and a map of its own, so a list of a carried element width
// and Go's any (ADR 0020) both have something to decode from. The rows
// pin each width's mapping; the constant set they range over is held by
// Property's switch, which has no default arm and so fails the
// exhaustive linter when internal/graph grows one.
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
		{graph.TypeTimestamp, "time.Time"},
		// The three widths that ride the neutral carriers temporal.go
		// declares, which is what a Postgres-through-pgx backend can
		// spell where it cannot spell a neo4j driver type.
		{graph.TypeDate, "Date"},
		{graph.TypeLocalTime, "LocalTime"},
		{graph.TypeTime, "Time"},
		{graph.TypeDuration, "Duration"},
	}
	for _, tt := range representable {
		t.Run("representable/"+string(tt.pt), func(t *testing.T) {
			got, ok := age.TypeMap{}.Property(tt.pt)
			require.True(t, ok)
			require.Equal(t, tt.want, got)
		})
	}

	unrepresentable := []graph.PropertyType{
		// agtype has no byte-string scalar.
		graph.TypeBytes,
		// No faithful Go carrier on any backend (§9).
		graph.TypeInt128, graph.TypeInt256,
		graph.TypeUint128, graph.TypeUint256,
		graph.TypeFloat16, graph.TypeFloat128, graph.TypeFloat256,
		graph.TypeDecimal,
	}
	for _, pt := range unrepresentable {
		t.Run("unrepresentable/"+string(pt), func(t *testing.T) {
			got, ok := age.TypeMap{}.Property(pt)
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
			// These three admitted temporal widths carry no zone, so unlike
			// the instant and TIME they have nothing that a list's single
			// name would have to hold once per element.
			graph.ListOf(graph.TypeDate, false):      "[]Date",
			graph.ListOf(graph.TypeLocalTime, false): "[]LocalTime",
			graph.ListOf(graph.TypeDuration, true):   "[]Duration",
		}
		for pt, want := range cases {
			got, ok := age.TypeMap{}.Property(pt)
			require.True(t, ok, "%s", pt)
			require.Equal(t, want, got, "%s", pt)
		}
	})

	// A width with no carrier does not acquire one by being wrapped in a
	// list: the elements would reach a slice no helper can fill. TIMESTAMP
	// and TIME are here despite carrying as properties, because the zone
	// sidecar is named after the property and a list has one name for every
	// element — carriesZone is the one predicate that says so, and these
	// rows are what stops admitting a zoned width from admitting a list of
	// it in the same edit.
	t.Run("a list of an uncarried element width is rejected", func(t *testing.T) {
		for _, elem := range []graph.PropertyType{graph.TypeDecimal, graph.TypeTime, graph.TypeBytes, graph.TypeTimestamp} {
			for _, pt := range []graph.PropertyType{
				graph.ListOf(elem, false),
				graph.ListOf(graph.ListOf(elem, false), false),
			} {
				got, ok := age.TypeMap{}.Property(pt)
				require.False(t, ok, "%s", pt)
				require.Empty(t, got)
			}
		}
	})

	// graph.PropertyType is a string type, so a width with no row above
	// costs nothing to construct and compiles. It must reject: the caller
	// turns that into ErrUnrepresentableWidth naming the width.
	t.Run("width outside the table is rejected", func(t *testing.T) {
		got, ok := age.TypeMap{}.Property(graph.PropertyType("QUATERNION"))
		require.False(t, ok)
		require.Empty(t, got)
	})

	t.Run("list of a width outside the table is rejected", func(t *testing.T) {
		got, ok := age.TypeMap{}.Property(graph.ListOf("QUATERNION", false))
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
// LIST<TIME> is what the author wrote, and TIME alone is not
// a line they could go and find — the more so now that TIME carries on
// its own, so the element width names a row that would have succeeded.
func TestTypeMapPropertyRejectionReachesTheCaller(t *testing.T) {
	for _, pt := range []graph.PropertyType{graph.TypeBytes, graph.ListOf(graph.TypeTime, false)} {
		t.Run(string(pt), func(t *testing.T) {
			files, err := age.Generate(codegen.Input{Schema: schemaWithPayload(pt)}, "age")
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
	files, err := age.Generate(codegen.Input{
		Schema: schemaWithPayload(graph.TypeBytes),
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
	require.ErrorIs(t, err, age.ErrUnsupportedQuery)
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
// so the sweeps below range over every kind rather than a sample. The
// length check against resolver.TemporalCount is what keeps it whole: a
// kind the resolver gains is a compile failure in the type table's
// switch and a failure here.
var temporalKinds = []resolver.Temporal{
	resolver.TemporalDate,
	resolver.TemporalTime,
	resolver.TemporalLocalTime,
	resolver.TemporalDateTime,
	resolver.TemporalLocalDateTime,
	resolver.TemporalDuration,
}

// TestTypeMapTemporal pins the temporal column-shape row (spec §5.1).
// Every kind refuses, and lifting the TIMESTAMP property width to a real
// encoding did not change that: a column of this shape exists only
// because the query called a temporal constructor, and AGE 1.7.0 has
// none to call. The rows exist so admitting one lands as a failure
// rather than as a quiet change of column type.
func TestTypeMapTemporal(t *testing.T) {
	require.Len(t, temporalKinds, resolver.TemporalCount,
		"the sweep must cover the resolver's whole temporal vocabulary")

	for _, k := range temporalKinds {
		t.Run(k.String(), func(t *testing.T) {
			got, ok := age.TypeMap{}.Temporal(k)
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
		got, ok := age.TypeMap{}.Temporal(resolver.Temporal(resolver.TemporalCount))
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
			prepared, err := codegen.Prepare(in, age.TypeMap{}, "age")
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
//
// The constructor is NAMESPACED, because every bare temporal
// constructor openCypher spells is a name the dialect gate refuses on
// the TEXT (dialect.go), which runs ahead of Prepare and would answer
// this query before the carrier was ever asked about. A namespaced call
// is a different name (Cypher.g4 §oC_FunctionName is `oC_Namespace
// oC_SymbolicName`) and cypher.UnqualifiedFunctionCalls drops it, so it
// is the one temporal spelling that still reaches the carrier question —
// which makes this test the other half of the bound
// TestRejectsUndefinedFunctions/"a namespaced call is a different name
// and is not refused" states: an unrefused name reaches the carrier
// question, and this is the answer it gets. bd gqlc-dy40s is the bead that would close that
// gap; see the sibling fixture
// test/data/codegen/invalid/unrepresentable_temporal_duration_column for
// what it owes.
func TestTemporalProjectionNamesThisBackend(t *testing.T) {
	files, err := age.Generate(codegen.Input{
		Schema: schemaWithPayload(graph.TypeString),
		Queries: []codegen.NamedQuery{{
			Name:        "When",
			Cardinality: codegen.CardinalityMany,
			SourceFile:  "q.cypher",
			SourceText:  "MATCH (b:Blob) RETURN duration.between(b.a, b.z) AS t\n",
			Validated: resolver.ValidatedQuery{
				Columns: []resolver.Column{{
					Name: "t", Type: resolver.ResolvedTemporal{Kind: resolver.TemporalDuration},
				}},
			},
		}},
	}, "age")
	require.ErrorIs(t, err, codegen.ErrUnrepresentableTemporal)
	require.EqualError(t, err,
		`unrepresentable temporal kind: query "When" column 0 "t" projects temporal(duration), `+
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
			require.Equal(t, tt.want, age.TypeMap{}.Scalar(tt.k))
		})
	}

	// The compiler requires a return past the last arm of a closed-enum
	// switch, so the backstop exists whether or not the resolver can
	// reach it. Pinned so its value is a decision and not an accident.
	t.Run("kind outside the vocabulary projects undecoded", func(t *testing.T) {
		require.Equal(t, "any", age.TypeMap{}.Scalar(resolver.ScalarMap+1))
	})
}
