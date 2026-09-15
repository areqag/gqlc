package codegen_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/codegen"
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
