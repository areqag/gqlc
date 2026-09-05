package neo4j_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/codegen"
	"github.com/areqag/gqlc/internal/codegen/neo4j"
	"github.com/areqag/gqlc/internal/graph"
)

// paramsOf wraps parameter fields in the smallest Prepared conversionUses reads.
// Entities and RowFields are left empty so every flag the rows below assert can
// only have come from the parameter walk.
func paramsOf(params ...codegen.Param) codegen.Prepared {
	return codegen.Prepared{Queries: []codegen.Query{{ParamFields: params}}}
}

// TestTemporalUsesAccumulatesListPtrRegardlessOfParameterOrder pins the OR in
// render_temporal.go's list-parameter arm as an accumulator rather than an
// assignment.
//
// The distinction is invisible to every committed fixture: it needs one carrier
// bound as both a nullable and a non-nullable list parameter in one batch, and
// temporal_list_param binds days (non-nullable Date) beside spans (nullable
// Duration) — different carriers, so the OR never has two inputs to fold. With
// only one input, `u.listPtr = f.Nullable` and `u.listPtr = u.listPtr ||
// f.Nullable` agree.
//
// The mutant is not equivalent. Under last-wins the nullable-first order reads
// listPtr=false, so from<X>ListPtr is never emitted while paramBindExpr still
// calls it, and the generated package does not compile — a failure that would
// surface as a golden mismatch in some other package, or in a user's build.
// Both orders are asserted because only one of them is wrong: nullable-LAST
// scores listPtr=true under the mutant too, so a single-order row would pass
// over it (bd gqlc-1ddo5).
func TestTemporalUsesAccumulatesListPtrRegardlessOfParameterOrder(t *testing.T) {
	for _, tc := range []struct {
		name   string
		params []codegen.Param
	}{
		{
			name: "nullable list parameter first",
			params: []codegen.Param{
				{RawName: "a", Field: "A", GoType: "[]Date", Nullable: true},
				{RawName: "b", Field: "B", GoType: "[]Date", Nullable: false},
			},
		},
		{
			name: "nullable list parameter last",
			params: []codegen.Param{
				{RawName: "a", Field: "A", GoType: "[]Date", Nullable: false},
				{RawName: "b", Field: "B", GoType: "[]Date", Nullable: true},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prepared := paramsOf(tc.params...)

			require.Contains(t, neo4j.TemporalUseNames(prepared), "Date",
				"Date reached no emission site at all")
			use := neo4j.TemporalUseOf(prepared, "Date")
			require.True(t, use.List, "the list helper is not reached, so there is nothing for listPtr to qualify")
			require.True(t, use.ListPtr,
				"listPtr is false, so from<X>ListPtr is not emitted while paramBindExpr still calls it. "+
					"One nullable list parameter in the batch is enough, wherever it sits in the order")
		})
	}
}

// TestTemporalUsesIgnoresNonCarrierParameters pins the other half of the
// parameter walk: leafType strips the slice, and a leaf that is not one of
// codegen.TemporalCarriers reaches no site. []any and []byte are the two that
// matter — both are slices, so both take the list arm and would mark a carrier
// if the arm marked unconditionally (raised while reviewing PR #2287).
//
// Asserted as an empty map rather than by looking up a name: naming a key here
// would only prove that ONE spelling was ignored, and the claim is that neither
// reaches anything.
func TestTemporalUsesIgnoresNonCarrierParameters(t *testing.T) {
	require.Empty(t, neo4j.TemporalUseNames(paramsOf(
		codegen.Param{RawName: "a", Field: "A", GoType: "[]any", Nullable: true},
		codegen.Param{RawName: "b", Field: "B", GoType: "[]byte", Nullable: false},
	)), "a non-carrier leaf reached an emission site")
}

// TestTemporalUsesSeesACarrierHidingInsideARecord is a reproduction, and
// it fails today.
//
// temporalUses marks on the emitted Go type TEXT, through
// leafType, which strips "[]" prefixes and nothing else. A declared
// record's carrier text is an anonymous struct, so leafType hands back
// the whole struct and isTemporalCarrier says no — the DATE field inside
// it is never marked.
//
// The consequence is not a missing optimisation. renderModels emits the
// record's decode helper with a toDate call in it, and
// renderTemporalConversions is handed a use set that never asked for
// toDate, so the emitted package names a function it does not declare and
// `go build` of generated code fails with no line in the schema to point
// at. The encode direction fails the same way through fromDate and
// fromDatePtr.
//
// RECORD<ANY> is the control: its fields are undeclared, so no carrier
// can be hiding in one and nothing should be marked for it.
func TestTemporalUsesSeesACarrierHidingInsideARecord(t *testing.T) {
	// Both nullabilities, because they owe DIFFERENT helpers: the record's
	// encode body spells its fields by the parameter rules, so the NOT
	// NULL field calls fromDate and the nullable one calls fromDatePtr. A
	// record with only one of them could not tell a walk that marks the
	// right direction from one that marks whichever it happens to.
	width := graph.RecordOf([]graph.RecordField{
		{Name: "at", Type: graph.TypeDate, NotNull: true},
		{Name: "seen", Type: graph.TypeDate},
	})
	structText, ok := neo4j.TypeMap{}.Property(width)
	require.True(t, ok)
	require.Contains(t, structText, "Date", "the premise: the carrier really is inside the emitted text")

	t.Run("a record property owes the decode direction", func(t *testing.T) {
		use := neo4j.TemporalUseOf(codegen.Prepared{
			Entities: []codegen.Entity{{Name: "Place", Fields: []codegen.EntityField{
				{PropName: "addr", Field: "Addr", GoType: structText, Width: width},
			}}},
		}, "Date")
		require.True(t, use.Decode,
			"the record's decode helper calls toDate, so the bridge file owes it")
	})

	t.Run("a record parameter owes the encode direction", func(t *testing.T) {
		use := neo4j.TemporalUseOf(codegen.Prepared{
			Queries: []codegen.Query{{ParamFields: []codegen.Param{
				{RawName: "p", Field: "P", GoType: structText, Width: width},
			}}},
		}, "Date")
		require.True(t, use.Encode, "the NOT NULL field calls fromDate, so the bridge file owes it")
		require.True(t, use.EncodePtr, "the nullable field calls fromDatePtr, which is a separate declaration")
	})

	t.Run("a LIST of records reaches its fields too", func(t *testing.T) {
		// The list level owes no temporal helper of its own — the record's
		// encode<X>List is what walks it — but each element is encoded by
		// encode<X>, which calls the field conversions by name. So a walk
		// that tested the OUTER width would see KindList, descend into
		// nothing, and emit a package naming fromDate undeclared.
		listWidth := graph.ListOf(width, false)
		listText, ok := neo4j.TypeMap{}.Property(listWidth)
		require.True(t, ok)
		use := neo4j.TemporalUseOf(codegen.Prepared{
			Queries: []codegen.Query{{ParamFields: []codegen.Param{
				{RawName: "p", Field: "P", GoType: listText, Width: listWidth},
			}}},
		}, "Date")
		require.True(t, use.Encode, "encode<X>List calls encode<X> per element, which calls fromDate")
		require.True(t, use.EncodePtr, "and fromDatePtr for the nullable field")
		require.False(t, use.List,
			"from<X>List is NOT owed: it is the record's list helper that walks the elements, not a temporal one")
	})

	t.Run("the undeclared record hides nothing", func(t *testing.T) {
		require.Empty(t, neo4j.TemporalUseNames(codegen.Prepared{
			Entities: []codegen.Entity{{Name: "Place", Fields: []codegen.EntityField{
				{PropName: "addr", Field: "Addr", GoType: "map[string]any", Width: graph.TypeAnyRecord},
			}}},
		}), "RECORD<ANY> declares no fields, so no carrier can be hiding in one")
	})
}
