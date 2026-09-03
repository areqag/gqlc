package codegen_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/codegen"
)

func TestTypeTextNamesCarrier(t *testing.T) {
	rows := []struct {
		name string
		text string
		want bool
	}{
		{"bare carrier", "Date", true},
		{"slice of carrier", "[]Duration", true},
		{"substring is not a match", "LocalDateTime", true},
		{"non-carrier ident", "Event", false},
		{"qualified driver type", "dbtype.Date", false},
		{"stdlib qualified type", "time.Time", false},

		// A field's Names are declarations, not type references. These are
		// the three places ast.Inspect reaches *ast.Field, and each name-
		// position row is paired with the type-position row it must not
		// cost: under-emission breaks the emitted package, so a walk that
		// stopped reading field TYPES would be a worse defect than the
		// over-emission these rows close.
		{"struct field named after a carrier", "struct{ Date string }", false},
		{"struct field typed as a carrier", "struct{ when Date }", true},
		{"struct field named after a carrier, nested", "struct{ inner struct{ Date string } }", false},
		{"func parameter named after a carrier", "func(Date string) error", false},
		{"func parameter typed as a carrier", "func(when Date) error", true},
		{"func result typed as a carrier", "func() (Date, error)", true},
		{"interface method named after a carrier", "interface{ Date() string }", false},
		{"interface method returning a carrier", "interface{ when() Date }", true},
		{
			// The fail-open arm: an unparseable text answers true because
			// emitting temporal.go unreferenced still compiles while
			// omitting a referenced one does not.
			"unparseable text fails open", "1 +", true,
		},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			require.Equal(t, row.want, codegen.TypeTextNamesCarrier(row.text))
		})
	}
}
