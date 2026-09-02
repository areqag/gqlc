package neo4j_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/codegen"
	"github.com/areqag/gqlc/internal/codegen/neo4j"
	"github.com/areqag/gqlc/internal/graph"
	"github.com/areqag/gqlc/internal/resolver"
	"github.com/areqag/gqlc/internal/schema"
)

// TestTypeMapProperty pins the driver's Go-type table for the property
// axis (spec §5.1): the representable widths each map to their Go
// carrier, the unrepresentable ones report ok=false so the caller routes
// to ErrUnrepresentableWidth, and a list width recurses element-wise.
//
// Which widths those are is derived, not counted. The two tables below
// used to be held to the literals 21 and 8, and a count is a size rather
// than a membership: it says nothing about which constants are in the
// table, so graph.TypeAnyPropertyValue and graph.TypeList sat outside
// both for as long as the arms answering `any` and `[]any` have existed
// (bd gqlc-2l8v — 29 of internal/graph's 31 constants were named here).
// The obligation is now read off typeMap.Property's own case
// expressions by propertyArmNames, so a constant that gains an arm
// there arrives owing a row, and one that loses its arm stops owing one.
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
		{graph.TypeDate, "Date"},
		{graph.TypeTime, "Time"},
		{graph.TypeLocalTime, "LocalTime"},
		{graph.TypeTimestamp, "time.Time"},
		{graph.TypeDuration, "Duration"},
		// A property of no declared shape. `any` is the one answer that
		// rides neither of the driver's constrained generics, which is
		// what ridesADriverCarrier turns on and what routes the decode
		// through the Props map.
		{graph.TypeAnyPropertyValue, "any"},
		// The bare LIST / ARRAY. Its arm in the switch is unreachable —
		// TypeList is spelled "LIST<ANY>", so Kind() reports KindList and
		// the recursion at the top of Property takes it, answering
		// "[]" + Property(TypeAnyPropertyValue). The row is here because
		// what a caller declaring a bare LIST gets is the table's answer
		// however it is reached; the arm's own text is pinned by
		// TestTypeMapPropertyListArmIsUnreachable below.
		{graph.TypeList, "[]any"},
	}
	for _, tt := range representable {
		t.Run("representable/"+string(tt.pt), func(t *testing.T) {
			got, ok := neo4j.TypeMap{}.Property(tt.pt)
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
	for _, pt := range unrepresentable {
		t.Run("unrepresentable/"+string(pt), func(t *testing.T) {
			got, ok := neo4j.TypeMap{}.Property(pt)
			require.False(t, ok)
			require.Empty(t, got)
		})
	}

	// Every constant the switch has an arm for owes a row above. The two
	// walks read different files — propertyArmNames the case expressions
	// in types.go, graphPropertyTypes the const specs in internal/graph —
	// so a name in an arm that no constant declares fails here too,
	// rather than quietly widening the obligation by a spelling nothing
	// upstream holds.
	declared := graphPropertyTypes(t)
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
	arms := propertyArmNames(t)
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

	t.Run("list recurses element-wise", func(t *testing.T) {
		got, ok := neo4j.TypeMap{}.Property(graph.ListOf(graph.TypeInt32, false))
		require.True(t, ok)
		require.Equal(t, "[]int32", got)
	})

	t.Run("list of unrepresentable element fails", func(t *testing.T) {
		_, ok := neo4j.TypeMap{}.Property(graph.ListOf(graph.TypeDecimal, false))
		require.False(t, ok)
	})
}

// TestStorablePropertyRefusesANestedList pins the refusal that is not
// about a Go carrier (ADR 0035, bd gqlc-v0gk). [][]float32 is a
// perfectly good Go type; what refuses it is the neo4j server, which
// stores a property value only if it is a scalar or a flat list of
// scalars — measured 2026-08-29 against the pinned image, which answers
// a nested write with "Collections containing collections can not be
// stored in properties". Generating for a property no writer can ever
// fill emits a decoder that can never see data, so the refusal moves to
// generation time where it can name the property.
//
// EVERY ROW ASSERTS BOTH AXES, and that is the point of the test rather
// than a flourish. This refusal lives on StorableProperty precisely
// because Property still answers "[][]float32" and is right to: the same
// backend emits a working recursive decode for a nested list arriving as
// a QUERY VALUE. A row asserting only StorableProperty==false would go
// on passing if someone later moved the refusal back onto the carrier
// axis, which is the mistake this mechanism was revised to undo — it
// would put a carrier-absence claim on a width whose carrier is
// exercised. Pinning Property==true beside it is what makes the split
// real rather than nominal.
//
// The refused rows are chosen for the ways the check could be written
// too narrowly rather than for coverage of the width table:
//
//   - the NOT NULL row is here because Elem() strips the qualifier, so
//     a check reading the raw text instead would miss it;
//   - the depth-3 row is here because the refusal is at the outer level
//     and does not recurse to find the nesting;
//   - LIST<LIST<ANY>> is here because a bare LIST is spelled "LIST<ANY>"
//     upstream, so it reaches the guard as an element whose Kind() is
//     KindList like any other.
//
// The admitted rows are the other half, and they are what says this is
// a refusal of NESTING rather than of lists: a flat list of any
// representable width is stored, LIST<ANY> included. Over-refusing here
// would be the expensive mistake — the server SERVES nested lists as
// query values even though it will not store them, which is why the
// scope is the storage axis and not the plan walker.
func TestStorablePropertyRefusesANestedList(t *testing.T) {
	refused := []graph.PropertyType{
		graph.ListOf(graph.ListOf(graph.TypeInt16, false), false),
		graph.ListOf(graph.ListOf(graph.TypeFloat32, false), false),
		// LIST<LIST<INT16> NOT NULL> — the qualifier sits on the outer
		// list's element, which Elem() strips.
		graph.ListOf(graph.ListOf(graph.TypeInt16, false), true),
		graph.ListOf(graph.ListOf(graph.ListOf(graph.TypeInt, false), false), false),
		// The schema language spells this LIST<LIST<ANY VALUE>>;
		// graph.TypeAnyPropertyValue normalises to "ANY".
		graph.ListOf(graph.ListOf(graph.TypeAnyPropertyValue, false), false),
	}
	for _, pt := range refused {
		t.Run("refused/"+string(pt), func(t *testing.T) {
			require.Equal(t, graph.KindList, pt.Elem().Kind(),
				"this row's element is not itself a list, so it is not the shape this test is about")
			require.False(t, neo4j.TypeMap{}.StorableProperty(pt),
				"the neo4j server cannot store %s as a property value, so the table must refuse it "+
					"rather than emit a decoder no writer can ever fill (ADR 0035)", pt)

			got, ok := neo4j.TypeMap{}.Property(pt)
			require.Truef(t, ok,
				"%s must stay CARRIED: this backend emits a recursive decode for one as a query value, "+
					"and putting the refusal on the carrier axis would claim a carrier that exists does not", pt)
			require.NotEmpty(t, got)
		})
	}

	admitted := []struct {
		pt   graph.PropertyType
		want string
	}{
		{graph.ListOf(graph.TypeInt16, false), "[]int16"},
		{graph.ListOf(graph.TypeString, true), "[]string"},
		{graph.ListOf(graph.TypeAnyPropertyValue, false), "[]any"},
	}
	for _, tt := range admitted {
		t.Run("admitted/"+string(tt.pt), func(t *testing.T) {
			require.Truef(t, neo4j.TypeMap{}.StorableProperty(tt.pt),
				"%s is a flat list of a representable width, which the server stores; refusing it here "+
					"means the nested-list check is reading the outer list rather than its element", tt.pt)
			got, ok := neo4j.TypeMap{}.Property(tt.pt)
			require.True(t, ok)
			require.Equal(t, tt.want, got)
		})
	}
}

// TestNestedListPropertyRejectionReachesTheCaller pins what an author
// actually sees, which is the half of the refusal that has to be right:
// a table returning ok=false is only useful if the failure travels out
// of generation naming the entity, the property, the declared type, and
// the backend that refused.
//
// The backend name is the part this test exists for. The refusal is
// this backend's answer and not a property of the schema — AGE stores
// the same nested list happily — so a run emitting both targets fails
// on one of them, and only the name says which. The wording is
// deliberately NOT AGE's "has no carrier for": [][]float32 is a carrier
// and Go has it. What refuses is the server's storage rule, which is
// why this rides its own sentinel and its own words.
//
// The NotErrorIs is load-bearing and is not the ErrorIs restated. The
// two sentinels are both refusals of a declared property, and an
// assertion that only names the one expected cannot tell a width this
// axis DECLINED from a width no axis takes — if the storage clause were
// deleted and Property's list arm made to refuse nesting again, the
// caller would still see a refusal naming this entity and this property,
// and an ErrorIs-only test would report success for the mechanism the
// design amendment withdrew.
func TestNestedListPropertyRejectionReachesTheCaller(t *testing.T) {
	nested := graph.ListOf(graph.ListOf(graph.TypeFloat32, false), false)
	files, err := neo4j.New().Generate(codegen.Input{Schema: schemaWithPayload(nested)})

	require.ErrorIs(t, err, codegen.ErrUnstorableProperty,
		"the nested-list refusal is a STORAGE refusal and rides its own sentinel; prepare.go's entity "+
			"sweep routes it and already names the property on")
	require.NotErrorIs(t, err, codegen.ErrUnrepresentableWidth,
		"this width has a faithful Go carrier ([][]float32) that render_queries.go exercises for a "+
			"query-valued nested list, so claiming the carrier channel here would be false")
	require.ErrorContains(t, err,
		`entity "Blob" property "payload" has `+string(nested)+
			`, which the neo4j backend cannot store as a property`)
	require.Nil(t, files)
}

// schemaWithPayload is a one-node schema whose single property carries
// pt, so the entity width sweep is the only thing that can fail.
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

// TestTypeMapPropertyListArmIsUnreachable pins the claim types.go makes
// about its own graph.TypeList arm — "intercepted by the Kind() guard
// above; unreachable here" — which rests on one fact upstream: TypeList
// is spelled "LIST<ANY>", so Kind() reports KindList and the recursion
// takes it before the switch is entered.
//
// It is worth pinning because the arm and the recursion answer the same
// text today, so respelling the constant would move which of the two
// runs without moving the result, and the comment would be false with
// nothing red. The row in TestTypeMapProperty's table cannot catch it:
// it asks what a caller gets, and both paths give "[]any".
func TestTypeMapPropertyListArmIsUnreachable(t *testing.T) {
	require.Equal(t, graph.KindList, graph.TypeList.Kind(),
		"graph.TypeList no longer reports KindList, so typeMap.Property's recursion no longer intercepts it "+
			"and the case arm types.go documents as unreachable is now the one that answers")
	require.Equal(t, graph.TypeAnyPropertyValue, graph.TypeList.Elem(),
		"a bare LIST's element type is no longer the open property-value union, so the recursion answers "+
			"through some other arm than the one this table's row was written against")
}

// TestTypeMapTemporal pins the temporal column-shape row of the table
// (spec §5.1): five dbtype carriers plus time.Time for the zoned
// datetime, each answering ok=true. The driver ships a carrier per kind,
// so this backend never takes the refusal channel — a fact about this
// driver rather than about the table, pinned here so it stays one that
// was checked rather than one that was assumed.
func TestTypeMapTemporal(t *testing.T) {
	tests := []struct {
		k    resolver.Temporal
		want string
	}{
		{resolver.TemporalDate, "Date"},
		{resolver.TemporalTime, "Time"},
		{resolver.TemporalLocalTime, "LocalTime"},
		{resolver.TemporalDateTime, "time.Time"},
		{resolver.TemporalLocalDateTime, "LocalDateTime"},
		{resolver.TemporalDuration, "Duration"},
	}
	// Membership, not size. This assertion used to be
	// require.Len(tests, resolver.TemporalCount), which passes on a table
	// naming one kind twice and another not at all — the shape a
	// hand-edited table actually takes — and when it does fire it names
	// nothing about which kind was lost. ElementsMatch against the
	// vocabulary answers that by name, and is a multiset compare, so the
	// duplicate is caught too (bd gqlc-fb4a).
	swept := make([]resolver.Temporal, 0, len(tests))
	for _, tt := range tests {
		swept = append(swept, tt.k)
	}
	require.ElementsMatch(t, resolver.TemporalValues(), swept,
		"the sweep must cover the resolver's whole temporal vocabulary, once each")

	for _, tt := range tests {
		t.Run(tt.k.String(), func(t *testing.T) {
			got, ok := neo4j.TypeMap{}.Temporal(tt.k)
			require.True(t, ok)
			require.Equal(t, tt.want, got)
		})
	}

	// The compiler requires a return past the last arm of a closed-enum
	// switch. It refuses rather than naming a carrier, so a kind added
	// upstream and left without an arm here fails generation instead of
	// arriving as some other kind's dbtype.
	t.Run("kind outside the vocabulary is refused", func(t *testing.T) {
		got, ok := neo4j.TypeMap{}.Temporal(resolver.Temporal(resolver.TemporalCount))
		require.False(t, ok)
		require.Empty(t, got)
	})
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
	// Membership, not size; see TestTypeMapTemporal above for why. This
	// one was worse than its temporal twin — the literal 6 was counted by
	// hand, so it did not even move when the vocabulary did.
	swept := make([]resolver.Scalar, 0, len(tests))
	for _, tt := range tests {
		swept = append(swept, tt.k)
	}
	require.ElementsMatch(t, resolver.ScalarValues(), swept,
		"the sweep must cover the resolver's whole scalar vocabulary, once each")

	for _, tt := range tests {
		t.Run(tt.k.String(), func(t *testing.T) {
			require.Equal(t, tt.want, neo4j.TypeMap{}.Scalar(tt.k))
		})
	}

	// The compiler requires a return past the last arm of a closed-enum
	// switch, so the backstop exists whether or not the resolver can
	// reach it. Pinned so its value is a decision and not an accident.
	t.Run("kind outside the vocabulary projects undecoded", func(t *testing.T) {
		require.Equal(t, "any", neo4j.TypeMap{}.Scalar(resolver.ScalarMap+1))
	})
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
		{"any", "any"},
		{"map[string]any", "map[string]any"},
		{"time.Time", "time.Time"},
		// The five neutral temporal carriers (ADR 0033). Unlike every
		// other narrowing row here, the carrier and the emitted type
		// are not conversion-compatible: narrowExpr / widenExpr route
		// them through the emitted to<X> / from<X> helpers.
		{"Date", "dbtype.Date"},
		{"Time", "dbtype.Time"},
		{"LocalTime", "dbtype.LocalTime"},
		{"LocalDateTime", "dbtype.LocalDateTime"},
		{"Duration", "dbtype.Duration"},
	}
	require.Len(t, tests, 23)
	for _, tt := range tests {
		t.Run(tt.goType, func(t *testing.T) {
			require.Equal(t, tt.want, neo4j.DriverCarrier(tt.goType))
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
			require.Equal(t, tt.want, neo4j.AccessModeText(tt.isWrite))
		})
	}
}
