package age_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/codegen"
	"github.com/areqag/gqlc/internal/codegen/age"
	"github.com/areqag/gqlc/internal/codegen/typescan"
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
		// Not a width claim: a record is refused a step earlier, by
		// prepare.go's kind walk (codegen.ErrUnimplementedTypeKind,
		// ADR 0039), so this arm is unreachable. The row pins that it is
		// fail-closed — reached, it must not hand back a carrier.
		graph.TypeAnyRecord,
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

	// Every constant the switch has an arm for owes a row above, and every
	// row owes an arm. The two walks read different files — PropertyArms
	// the case expressions in this backend's type table, PropertyTypes the
	// const specs in internal/graph — so a name in an arm that no constant
	// declares fails here too, rather than quietly widening the obligation
	// by a spelling nothing upstream holds.
	//
	// The doc comment above says the constant SET is held by Property's
	// switch having no default arm. That is a claim about the SET and not
	// this obligation: it concerns a constant internal/graph gains, and
	// says nothing about what an arm that already exists answers. Measured
	// 2026-09-02 (bd gqlc-ozdkx): deleting the TypeUint16 row with its arm
	// left in place, and separately adding an arm no row names, both left
	// this suite green while the identical mutations reddened neo4j's.
	declared := agePropertyTypes(t)
	covered := make(map[string]bool)
	for _, tt := range representable {
		name, known := declared[tt.pt]
		require.True(t, known, "the representable table names %s, which %s declares no constant for", tt.pt, graphPropertyTypeSource)
		require.False(t, covered[name], "graph.%s has two rows in this table", name)
		covered[name] = true
	}
	for _, pt := range unrepresentable {
		name, known := declared[pt]
		require.True(t, known, "the unrepresentable table names %s, which %s declares no constant for", pt, graphPropertyTypeSource)
		require.False(t, covered[name], "graph.%s has two rows in this table", name)
		covered[name] = true
	}
	arms := agePropertyArms(t)
	require.NotEmpty(t, arms,
		"the walk read no case expression off %s, so the obligation below is satisfied by any table at all", typeTableSource)
	for name := range arms {
		require.True(t, covered[name],
			"typeMap.Property has an arm for graph.%s and no row above names it, so what that arm answers is "+
				"unswept: add it to the representable table with the Go carrier it returns, or to the "+
				"unrepresentable one", name)
	}
	for name := range covered {
		require.True(t, arms[name],
			"a row above names graph.%s and typeMap.Property has no arm for it, so the row is measuring the "+
				"fallthrough rather than a decision the table makes", name)
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
// because the query called a temporal constructor, and AGE 1.7.0
// defines none of the ones openCypher spells. The rows exist so
// admitting one lands as a failure rather than as a quiet change of
// column type.
func TestTypeMapTemporal(t *testing.T) {
	// Membership, not size. require.Len against the count passes on a
	// table naming one kind twice and another not at all — the shape a
	// hand-edited table actually takes — and names nothing about what was
	// lost when it fires. ElementsMatch is a multiset compare, so it
	// catches both directions and reports the kind (bd gqlc-fb4a).
	require.ElementsMatch(t, resolver.TemporalValues(), temporalKinds,
		"the sweep must cover the resolver's whole temporal vocabulary, once each")

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
// The Input is ASSEMBLED and its SourceText spells no constructor at
// all, which is the whole of what this test can be since bd gqlc-dy40s.
// Every temporal constructor openCypher spells is now refused on the
// TEXT by the dialect gate, which runs ahead of Prepare: the bare names
// by the temporal gap and the namespaced ones by the namespace gap. The
// namespaced spelling this test used to carry —
// duration.between(b.a, b.z) — was the last text that reached the
// carrier question, and it is refused now, so a query text that reaches
// here does not exist.
//
// That is not a hole in the coverage, it is the reachability this
// sentinel HAS. A hand-assembled codegen.Input handed to Generate is a
// reachable path by the taxonomy's own law (docs/specs/
// codegen-sentinel-taxonomy.md §5.1, the gqlc-h4ug precedent), and it is
// the path any future backend with partial temporal support would be
// answered on. The column below is therefore declared directly, and the
// text is a plain property lookup so that nothing in it is doing work
// the column is supposed to do.
func TestTemporalProjectionNamesThisBackend(t *testing.T) {
	files, err := age.Generate(codegen.Input{
		Schema: schemaWithPayload(graph.TypeString),
		Queries: []codegen.NamedQuery{{
			Name:        "When",
			Cardinality: codegen.CardinalityMany,
			SourceFile:  "q.cypher",
			SourceText:  "MATCH (b:Blob) RETURN b.span AS t\n",
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
	// This table had NO coverage assertion at all: gqlc-35yu.6 removed the
	// hand-counted require.Len and nothing replaced it, so deleting a row
	// stopped testing a kind and failed nothing. Membership restores the
	// obligation without restoring the count (bd gqlc-fb4a).
	swept := make([]resolver.Scalar, 0, len(tests))
	for _, tt := range tests {
		swept = append(swept, tt.k)
	}
	require.ElementsMatch(t, resolver.ScalarValues(), swept,
		"the sweep must cover the resolver's whole scalar vocabulary, once each")

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

// graphPropertyTypeSource is where internal/graph declares the normalised
// property types, and typeTableSource is where this backend declares
// which of them it emits a carrier for. Both obligations above are read
// off those files rather than restated here, so a width added upstream,
// or an arm added below, joins the obligation without an edit in this
// file.
const (
	graphPropertyTypeSource = "../../graph/propertytype.go"
	typeTableSource         = "types.go"
)

// agePropertyTypes and agePropertyArms are the walks internal/codegen/neo4j
// reads its own obligation with. Shared rather than copied: what a const
// block is and what a switch arm is do not vary by backend, and this
// backend having no walk at all is what bd gqlc-ozdkx was filed for.
func agePropertyTypes(t *testing.T) map[graph.PropertyType]string {
	t.Helper()
	out, err := typescan.PropertyTypes(graphPropertyTypeSource)
	require.NoError(t, err)
	return out
}

func agePropertyArms(t *testing.T) map[string]bool {
	t.Helper()
	out, err := typescan.PropertyArms(typeTableSource, "Property")
	require.NoError(t, err)
	return out
}
