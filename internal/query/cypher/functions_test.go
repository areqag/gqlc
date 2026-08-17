package cypher_test

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/query/cypher"
)

// --- unqualified function calls in the query TEXT (gqlc-35yu.12) ---
//
// A backend shipping the author's text verbatim (ADR 0005) onto a server whose
// function catalogue is not openCypher's has to decide from the text, and the
// query model cannot answer for it: a constructor in a WHERE predicate or in a
// write clause reaches no column at all, and the model drops predicate
// structure by design (ADR 0003).

// TestUnqualifiedFunctionCallsReadsTheText walks the shapes where a scan for
// the name would answer differently from the grammar.
func TestUnqualifiedFunctionCallsReadsTheText(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
		want []string
	}{
		{
			name: "no call is no answer",
			src:  "MATCH (p:Person) RETURN p.name",
			want: nil,
		},
		{
			name: "a call is reported by its name as written",
			src:  "RETURN datetime() AS now",
			want: []string{"datetime"},
		},
		{
			name: "the name is reported in the author's own case",
			src:  "RETURN DateTime() AS now",
			want: []string{"DateTime"},
		},
		{
			// The load-bearing shape. Predicate structure is dropped from
			// the model (ADR 0003), so no column, parameter or binding
			// carries this call and only the text does.
			name: "a call in a predicate the model drops still counts",
			src:  "MATCH (p:Person) WHERE p.at < datetime() RETURN p.id",
			want: []string{"datetime"},
		},
		{
			name: "a call in a write clause that projects nothing still counts",
			src:  "CREATE (e:Event {at: datetime()})",
			want: []string{"datetime"},
		},
		{
			name: "a call under SET still counts",
			src:  "MATCH (p:Person) SET p.seen = datetime()",
			want: []string{"datetime"},
		},
		{
			name: "a call inside EXISTS counts, because the server parses it too",
			src:  "MATCH (p:Person) WHERE exists { (p)-[:AUTHORED]->(o:Post) WHERE o.at > datetime() }\nRETURN p.id",
			want: []string{"datetime"},
		},
		{
			// Nesting is pre-order, so the outer name comes first. Both are
			// the author's, and a caller deciding about the bytes has to
			// see the inner one even when the outer one is served.
			name: "a nested call reports both names, outermost first",
			src:  "RETURN toString(datetime()) AS s",
			want: []string{"toString", "datetime"},
		},
		{
			name: "two calls are reported in source order",
			src:  "MATCH (p:Person) WHERE p.a < date() AND p.b < duration({days: 1}) RETURN p.id",
			want: []string{"date", "duration"},
		},
		{
			name: "the same name written twice is reported once",
			src:  "MATCH (p:Person) WHERE p.a < date() AND p.b < date() RETURN p.id",
			want: []string{"date"},
		},
		{
			// Two spellings of one name are two answers, because the
			// refusal built on this quotes what the author wrote and there
			// are two texts to quote.
			name: "two cases of one name are both reported",
			src:  "MATCH (p:Person) WHERE p.a < date() AND p.b < DATE() RETURN p.id",
			want: []string{"date", "DATE"},
		},
		{
			// A namespaced name is a different name. Cypher.g4
			// §oC_FunctionName is `oC_Namespace oC_SymbolicName`, and a
			// server resolving `duration.between` is not resolving
			// `duration`.
			name: "a namespaced call is not an unqualified one",
			src:  "RETURN duration.between(p.a, p.b) AS d",
			want: nil,
		},
		{
			// The false positive a scan for the name would take. A property
			// is oC_PropertyLookup and names no function.
			name: "a property named like a function is not a call",
			src:  "MATCH (p:Person) RETURN p.datetime",
			want: nil,
		},
		{
			name: "a variable named like a function is not a call",
			src:  "MATCH (p:Person) WHERE p.at < datetime RETURN p.id",
			want: nil,
		},
		{
			// The other false positive. A procedure is
			// §oC_ExplicitProcedureInvocation, a production of its own, and
			// what resolves it is not what resolves a function.
			name: "a procedure invocation is not a function call",
			src:  "MATCH (p:Person) CALL datetime() YIELD x RETURN x",
			want: nil,
		},
		{
			name: "a label named like a function is not a call",
			src:  "MATCH (d:date) RETURN d.id",
			want: nil,
		},
		{
			name: "a name inside a string literal is not a call",
			src:  "MATCH (p:Person) WHERE p.name = 'datetime()' RETURN p.id",
			want: nil,
		},
		{
			name: "a name inside a comment is not a call",
			src:  "// datetime()\nMATCH (p:Person) RETURN p.id",
			want: nil,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, cypher.UnqualifiedFunctionCalls(tc.src))
		})
	}
}

// TestUnqualifiedFunctionCallsAgreesWithTheParserOnTheAcceptedTexts is the
// cross-check that keeps the scan honest about what it must never do: name a
// call in a text the grammar reads as carrying none. Every source here is one
// Parse accepts, so two readings of the same bytes are compared rather than
// the scan being trusted alone.
func TestUnqualifiedFunctionCallsAgreesWithTheParserOnTheAcceptedTexts(t *testing.T) {
	for _, src := range []string{
		"MATCH (p:Person) RETURN p.datetime",
		"MATCH (d:date) RETURN d.id",
		"MATCH (p:Person) WHERE p.name = 'datetime()' RETURN p.id",
	} {
		t.Run(src, func(t *testing.T) {
			_, err := cypher.New().Parse(strings.NewReader(src))
			require.NoError(t, err, "the fixture must be a text the grammar accepts")
			require.Empty(t, cypher.UnqualifiedFunctionCalls(src))
		})
	}
}

// TestUnqualifiedFunctionCallsIsTotal pins the branch the CLI cannot reach: a
// text the grammar refuses gets an answer rather than a panic.
func TestUnqualifiedFunctionCallsIsTotal(t *testing.T) {
	for _, src := range malformedTexts {
		require.NotPanics(t, func() { cypher.UnqualifiedFunctionCalls(src) }, "src %q", src)
	}
}

// TestUnqualifiedFunctionCallsWritesNothingToStderr pins the other half of
// that totality, on the same terms as the alternation scan: ANTLR's default
// console listener writes a raw grammar diagnostic to os.Stderr and returns
// nothing, which a library function with no output channel of its own cannot
// offer a caller and has no business printing over `gqlc generate`.
func TestUnqualifiedFunctionCallsWritesNothingToStderr(t *testing.T) {
	for _, src := range malformedTexts {
		r, w, err := os.Pipe()
		require.NoError(t, err)
		saved := os.Stderr
		os.Stderr = w
		cypher.UnqualifiedFunctionCalls(src)
		os.Stderr = saved
		require.NoError(t, w.Close())
		out, err := io.ReadAll(r)
		require.NoError(t, err)
		require.NoError(t, r.Close())
		require.Empty(t, string(out), "src %q", src)
	}
}
