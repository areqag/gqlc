package age

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestDollarTagClosesOnlyAtTheEnds pins the delimiter choice against the
// string the SQL parser actually scans, tag + text + tag. Each row is a
// text whose final bytes interact with a candidate: an interior
// occurrence, a straddle across the text/tag boundary, a bare dollar
// that opens no delimiter, and a straddle on the escalated candidate so
// the second turn of the loop is exercised.
func TestDollarTagClosesOnlyAtTheEnds(t *testing.T) {
	tests := []struct {
		name string
		text string
		want string
	}{
		{name: "no dollar", text: "MATCH (p:Person) RETURN p.name", want: "$gqlc$"},
		{name: "parameter reference", text: "MATCH (p:Person) WHERE p.id = $id RETURN p.name", want: "$gqlc$"},
		{name: "interior occurrence", text: "RETURN '$gqlc$'", want: "$gqlc1$"},
		{name: "straddling occurrence", text: "RETURN p.name\n// trailing $gqlc", want: "$gqlc1$"},
		{name: "trailing dollar", text: "SET p.name = $", want: "$gqlc$"},
		{name: "straddle on the escalated candidate", text: "RETURN '$gqlc$' // $gqlc1", want: "$gqlc2$"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tag := dollarTag(tt.text)
			require.Equal(t, tt.want, tag)

			body := tag + tt.text + tag
			require.Equal(t, 2, strings.Count(body, tag),
				"the delimiter occurs somewhere other than the two ends of %q", body)
			require.Equal(t, len(body)-len(tag), strings.LastIndex(body, tag),
				"the scanner would close %q before the end", body)
		})
	}
}
