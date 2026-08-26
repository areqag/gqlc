package codegen

import (
	"testing"

	"github.com/stretchr/testify/require"
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
		{
			// The fail-open arm: an unparseable text answers true because
			// emitting temporal.go unreferenced still compiles while
			// omitting a referenced one does not.
			"unparseable text fails open", "1 +", true,
		},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			require.Equal(t, row.want, typeTextNamesCarrier(row.text))
		})
	}
}
