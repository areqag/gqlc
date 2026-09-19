package codegen_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/codegen"
	"github.com/areqag/gqlc/internal/graph"
)

// TestTypeTextNamesCarrier drives the walk over the temporal set, and
// over the UUID set on the rows where the two sets have to disagree.
// The set is a parameter of the walk since uuid.go joined temporal.go as
// an independently triggered carrier file, so a row that does not say
// which set it asked would be answering a question the production caller
// does not ask.
func TestTypeTextNamesCarrier(t *testing.T) {
	rows := []struct {
		name string
		text string
		set  map[string]struct{}
		want bool
	}{
		{"bare carrier", "Date", codegen.TemporalCarrierSet, true},
		{"slice of carrier", "[]Duration", codegen.TemporalCarrierSet, true},
		{"substring is not a match", "LocalDateTime", codegen.TemporalCarrierSet, true},
		{"non-carrier ident", "Event", codegen.TemporalCarrierSet, false},
		{"qualified driver type", "dbtype.Date", codegen.TemporalCarrierSet, false},
		{"stdlib qualified type", "time.Time", codegen.TemporalCarrierSet, false},

		// The two sets are disjoint, and each has to answer false for the
		// other's names or the two carrier files are emitted together —
		// the combined trigger ADR 0033's placement argument declines.
		{"bare UUID carrier", "UUID", codegen.UUIDCarrierSet, true},
		{"nullable list of UUID carriers", "*[]*UUID", codegen.UUIDCarrierSet, true},
		{"qualified driver UUID", "dbtype.UUID", codegen.UUIDCarrierSet, false},
		{"UUID is not a temporal carrier", "UUID", codegen.TemporalCarrierSet, false},
		{"Date is not the UUID carrier", "Date", codegen.UUIDCarrierSet, false},

		// A field's Names are declarations, not type references. These are
		// the three places ast.Inspect reaches *ast.Field, and each name-
		// position row is paired with the type-position row it must not
		// cost: under-emission breaks the emitted package, so a walk that
		// stopped reading field TYPES would be a worse defect than the
		// over-emission these rows close.
		{"struct field named after a carrier", "struct{ Date string }", codegen.TemporalCarrierSet, false},
		{"struct field typed as a carrier", "struct{ when Date }", codegen.TemporalCarrierSet, true},
		{"struct field named after a carrier, nested", "struct{ inner struct{ Date string } }", codegen.TemporalCarrierSet, false},
		{"func parameter named after a carrier", "func(Date string) error", codegen.TemporalCarrierSet, false},
		{"func parameter typed as a carrier", "func(when Date) error", codegen.TemporalCarrierSet, true},
		{"func result typed as a carrier", "func() (Date, error)", codegen.TemporalCarrierSet, true},
		{"interface method named after a carrier", "interface{ Date() string }", codegen.TemporalCarrierSet, false},
		{"interface method returning a carrier", "interface{ when() Date }", codegen.TemporalCarrierSet, true},
		{
			// The fail-open arm: an unparseable text answers true because
			// emitting temporal.go unreferenced still compiles while
			// omitting a referenced one does not.
			"unparseable text fails open", "1 +", codegen.TemporalCarrierSet, true,
		},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			require.Equal(t, row.want, codegen.TypeTextNamesCarrier(row.text, row.set))
		})
	}
}

// TestCarrierTriggersReadUnionMembers holds the two emission triggers over
// a batch whose one property is a closed union, so its surface text is
// `any` and the members are the only place a carrier can be named
// (bd gqlc-o8p3).
//
// The emit rows are also held end to end, by TestGoldenBuild over
// test/data/codegen/valid/union_only_*, which is where an omitted file is
// an emitted package that does not compile. What those fixtures cannot
// hold is here: the refused-member arm, which no batch reaches because
// preparation refuses it first, and each trigger answering false for the
// other family's member in one table rather than across two golden trees.
func TestCarrierTriggersReadUnionMembers(t *testing.T) {
	carriers := map[graph.PropertyType]string{
		graph.TypeDate:   "Date",
		graph.TypeUUID:   codegen.UUIDCarrier,
		graph.TypeBool:   "bool",
		graph.TypeInt64:  "int64",
		graph.TypeString: "string",
	}
	carrier := func(pt graph.PropertyType) (string, bool) {
		text, ok := carriers[pt]
		return text, ok
	}
	unionOf := func(members ...graph.PropertyType) graph.PropertyType {
		out := make([]graph.UnionMember, 0, len(members))
		for _, m := range members {
			out = append(out, graph.UnionMember{Type: m})
		}
		return graph.UnionOf(out)
	}
	batch := func(widths ...graph.PropertyType) codegen.Prepared {
		fields := make([]codegen.EntityField, 0, len(widths))
		for i, width := range widths {
			goType := codegen.UnionCarrierText
			if width.Kind() == graph.KindList {
				goType = "[]" + goType
			}
			name := fmt.Sprintf("either%d", i)
			fields = append(fields, codegen.EntityField{PropName: name, Field: name, GoType: goType, Width: width})
		}
		return codegen.Prepared{Entities: []codegen.Entity{{Name: "Account", Fields: fields}}}
	}

	temporal, plain := unionOf(graph.TypeDate, graph.TypeInt64), unionOf(graph.TypeInt64, graph.TypeString)
	rows := []struct {
		name         string
		widths       []graph.PropertyType
		wantTemporal bool
		wantUUID     bool
	}{
		{"no member is a carrier", []graph.PropertyType{plain}, false, false},
		{"a temporal member", []graph.PropertyType{temporal}, true, false},
		{"a UUID member", []graph.PropertyType{unionOf(graph.TypeUUID, graph.TypeInt64)}, false, true},
		{"a list of unions", []graph.PropertyType{graph.ListOf(temporal, false)}, true, false},
		{
			// BYTES is a width the carrier above refuses. Both triggers
			// answer true for it, on typeTextNamesCarrier's ground: a
			// carrier file nothing references still compiles.
			"a member the carrier refuses fails open", []graph.PropertyType{unionOf(graph.TypeBytes, graph.TypeInt64)}, true, true,
		},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			p := batch(row.widths...)
			require.Equal(t, row.wantTemporal, codegen.ReferencesTemporalCarrier(p, carrier))
			require.Equal(t, row.wantUUID, codegen.ReferencesUUIDCarrier(p, carrier))
		})
	}

	// Two unions, with the carrier in the one UnionEncodings hands over
	// LAST. A reading that stopped after the first encoding answers false
	// here and true on every row above, each of which holds one union. The
	// order is asserted rather than assumed, in both declaration orders, so
	// a change to the canonical sort cannot turn this into a first-entry
	// row without saying so.
	t.Run("the carrier is in the later-sorting union", func(t *testing.T) {
		early := unionOf(graph.TypeBool, graph.TypeInt64)
		for _, widths := range [][]graph.PropertyType{{early, temporal}, {temporal, early}} {
			p := batch(widths...)
			require.Equal(t, []graph.PropertyType{early, temporal}, codegen.UnionEncodings(p.Entities, p.Queries))
			require.True(t, codegen.ReferencesTemporalCarrier(p, carrier))
			require.False(t, codegen.ReferencesUUIDCarrier(p, carrier))
		}
	})
}
