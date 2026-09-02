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

// --- qualified function calls in the query TEXT (gqlc-dy40s) ---
//
// The other half of the same reading. A namespaced call is invisible to the
// scan above by design, and the pinned Apache AGE image refuses one — as
// SQLSTATE 3F000 `schema "duration" does not exist`, an answer about the
// NAMESPACE raised at qualifier resolution before any function lookup. So the
// caller deciding about the bytes needs the namespace, and needs the whole
// name as written to quote back.

// TestQualifiedFunctionCallsReadsTheText walks the shapes where the namespace
// reading and a scan for the characters would answer differently.
func TestQualifiedFunctionCallsReadsTheText(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
		want []cypher.QualifiedCall
	}{
		{
			name: "no call is no answer",
			src:  "MATCH (p:Person) RETURN p.name",
			want: nil,
		},
		{
			name: "a qualified call is reported by namespace and spelling",
			src:  "MATCH (p:Person) RETURN duration.between(p.a, p.b) AS d",
			want: []cypher.QualifiedCall{{Namespace: "duration", Text: "duration.between"}},
		},
		{
			// Load-bearing, not politeness. shape.go lowercases namespace
			// parts before typing a temporal, so this spelling resolves to
			// a temporal column further down the pipeline; a case-sensitive
			// reading here would let it past the gate that must answer it.
			name: "the namespace is lowercased and the spelling is the author's",
			src:  "MATCH (p:Person) RETURN Duration.Between(p.a, p.b) AS d",
			want: []cypher.QualifiedCall{{Namespace: "duration", Text: "Duration.Between"}},
		},
		{
			// The partition the two scans rest on: an invocation is
			// qualified or it is not, so neither scan can claim the
			// other's call.
			name: "an unqualified call is not a qualified one",
			src:  "RETURN datetime() AS now",
			want: nil,
		},
		{
			// Cypher.g4 §oC_Namespace is `( oC_SymbolicName '.' )*`, so a
			// namespace has any number of parts. Joined rather than
			// truncated to the first: the parts are the name the server is
			// given, and dropping one would make two namespaces compare
			// equal.
			name: "a multi-part namespace is joined by dots",
			src:  "MATCH (p:Person) RETURN com.example.between(p.a, p.b) AS d",
			want: []cypher.QualifiedCall{{Namespace: "com.example", Text: "com.example.between"}},
		},
		{
			// The shape the model cannot answer for. Predicate structure
			// is dropped (ADR 0003), so nothing but the text carries this.
			name: "a call in a predicate the model drops still counts",
			src:  "MATCH (p:Person) WHERE p.d < duration.between(p.a, p.b) RETURN p.id",
			want: []cypher.QualifiedCall{{Namespace: "duration", Text: "duration.between"}},
		},
		{
			name: "a call in a write clause that projects nothing still counts",
			src:  "MATCH (p:Person) CREATE (e:Event {d: duration.between(p.a, p.b)})",
			want: []cypher.QualifiedCall{{Namespace: "duration", Text: "duration.between"}},
		},
		{
			name: "a call under SET still counts",
			src:  "MATCH (p:Person) SET p.d = duration.between(p.a, p.b)",
			want: []cypher.QualifiedCall{{Namespace: "duration", Text: "duration.between"}},
		},
		{
			name: "a call inside EXISTS counts, because the server parses it too",
			src:  "MATCH (p:Person) WHERE exists { (p)-[:AUTHORED]->(o:Post) WHERE o.d > duration.between(p.a, p.b) }\nRETURN p.id",
			want: []cypher.QualifiedCall{{Namespace: "duration", Text: "duration.between"}},
		},
		{
			name: "a nested call reports both, outermost first",
			src:  "MATCH (p:Person) RETURN duration.between(p.a, duration.inSeconds(p.b)) AS d",
			want: []cypher.QualifiedCall{
				{Namespace: "duration", Text: "duration.between"},
				{Namespace: "duration", Text: "duration.inSeconds"},
			},
		},
		{
			name: "two calls are reported in source order",
			src:  "MATCH (p:Person) WHERE p.a < duration.between(p.x, p.y) AND p.b < point.distance(p.u, p.v) RETURN p.id",
			want: []cypher.QualifiedCall{
				{Namespace: "duration", Text: "duration.between"},
				{Namespace: "point", Text: "point.distance"},
			},
		},
		{
			name: "the same spelling written twice is reported once",
			src:  "MATCH (p:Person) WHERE p.a < duration.between(p.x, p.y) AND p.b < duration.between(p.u, p.v) RETURN p.id",
			want: []cypher.QualifiedCall{{Namespace: "duration", Text: "duration.between"}},
		},
		{
			// Two spellings of one name are two answers, on the same terms
			// as the unqualified scan: the refusal quotes what the author
			// wrote and here there are two texts to quote.
			name: "two cases of one name are both reported",
			src:  "MATCH (p:Person) WHERE p.a < duration.between(p.x, p.y) AND p.b < Duration.Between(p.u, p.v) RETURN p.id",
			want: []cypher.QualifiedCall{
				{Namespace: "duration", Text: "duration.between"},
				{Namespace: "duration", Text: "Duration.Between"},
			},
		},
		{
			// The false positive a scan for `duration.` would take, and
			// the accessor/property distinction CONTEXT.md already draws.
			name: "a property named like a namespace is not a call",
			src:  "MATCH (p:Person) RETURN p.duration",
			want: nil,
		},
		{
			name: "a property chain spelling a qualified name is not a call",
			src:  "MATCH (p:Person) RETURN p.duration.between",
			want: nil,
		},
		{
			// The trap this scan exists to avoid taking. oC_ProcedureName
			// is `oC_Namespace oC_SymbolicName` too — the same shape,
			// resolved against a different catalogue.
			name: "a qualified procedure invocation is not a function call",
			src:  "MATCH (p:Person) CALL duration.between(p.a, p.b) YIELD x RETURN x",
			want: nil,
		},
		{
			name: "a name inside a string literal is not a call",
			src:  "MATCH (p:Person) WHERE p.name = 'duration.between()' RETURN p.id",
			want: nil,
		},
		{
			name: "a name inside a comment is not a call",
			src:  "// duration.between()\nMATCH (p:Person) RETURN p.id",
			want: nil,
		},
		{
			// SP? sits between oC_FunctionName and '(' in
			// §oC_FunctionInvocation, so the space is outside the name and
			// the quoted spelling does not carry it. §oC_Namespace admits
			// no SP at all, so there is no whitespace to preserve INSIDE a
			// qualified name.
			name: "a space before the paren is outside the quoted name",
			src:  "MATCH (p:Person) RETURN duration.between (p.a, p.b) AS d",
			want: []cypher.QualifiedCall{{Namespace: "duration", Text: "duration.between"}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, cypher.QualifiedFunctionCalls(tc.src))
		})
	}
}

// TestTheTwoFunctionScansPartitionTheCalls is the disjointness the gap table
// leans on: an invocation is qualified or it is not, so one text carrying both
// spellings is read once by each scan and never twice by either. Asserted on
// one text rather than inferred from the two tables above, because what the
// gate needs is that these two readings of the SAME bytes cannot collide.
func TestTheTwoFunctionScansPartitionTheCalls(t *testing.T) {
	const src = "MATCH (p:Person) WHERE p.a < duration({days: 1}) AND p.b < duration.between(p.x, p.y) RETURN p.id"

	require.Equal(t, []string{"duration"}, cypher.UnqualifiedFunctionCalls(src))
	require.Equal(t,
		[]cypher.QualifiedCall{{Namespace: "duration", Text: "duration.between"}},
		cypher.QualifiedFunctionCalls(src))
}

// TestQualifiedFunctionCallsAgreesWithTheParserOnTheAcceptedTexts is the
// cross-check, on the same terms as the unqualified scan's: every source is
// one Parse accepts, so the claim that these texts call nothing qualified is
// two readings of the same bytes rather than the scan trusted alone.
func TestQualifiedFunctionCallsAgreesWithTheParserOnTheAcceptedTexts(t *testing.T) {
	for _, src := range []string{
		"MATCH (p:Person) RETURN p.duration",
		"MATCH (p:Person) RETURN p.duration.between",
		"MATCH (p:Person) WHERE p.name = 'duration.between()' RETURN p.id",
	} {
		t.Run(src, func(t *testing.T) {
			_, err := cypher.New().Parse(strings.NewReader(src))
			require.NoError(t, err, "the fixture must be a text the grammar accepts")
			require.Empty(t, cypher.QualifiedFunctionCalls(src))
		})
	}
}

// TestAQualifiedProcedureIsReadAsAProcedure keeps the procedure row above from
// passing for the wrong reason. §oC_ProcedureName is `oC_Namespace
// oC_SymbolicName`, the same shape the scan reads, so a text spelling one has
// to be shown to REACH that production before the scan's silence on it means
// anything — a text the grammar failed to read would be silent too.
//
// Parse refuses this text, but at the resolver and not the parser, and its
// message quotes the procedure name back. That is the witness: the grammar
// read `duration.between` as a procedure name, so the tree the scan walked
// holds the shape, and the scan declining it is a discrimination.
func TestAQualifiedProcedureIsReadAsAProcedure(t *testing.T) {
	const src = "MATCH (p:Person) CALL duration.between(p.a, p.b) YIELD x RETURN x"

	_, err := cypher.New().Parse(strings.NewReader(src))
	require.EqualError(t, err, "unknown procedure: duration.between")
	require.Empty(t, cypher.QualifiedFunctionCalls(src))
}

// TestQualifiedFunctionCallsIsTotal pins the branch the CLI cannot reach: a
// text the grammar refuses gets an answer rather than a panic.
func TestQualifiedFunctionCallsIsTotal(t *testing.T) {
	for _, src := range malformedTexts {
		require.NotPanics(t, func() { cypher.QualifiedFunctionCalls(src) }, "src %q", src)
	}
}

// TestQualifiedFunctionCallsWritesNothingToStderr pins the other half of that
// totality: ANTLR's default console listener writes a raw grammar diagnostic
// to os.Stderr and returns nothing, which a library function with no output
// channel of its own cannot offer a caller.
func TestQualifiedFunctionCallsWritesNothingToStderr(t *testing.T) {
	for _, src := range malformedTexts {
		r, w, err := os.Pipe()
		require.NoError(t, err)
		saved := os.Stderr
		os.Stderr = w
		cypher.QualifiedFunctionCalls(src)
		os.Stderr = saved
		require.NoError(t, w.Close())
		out, err := io.ReadAll(r)
		require.NoError(t, err)
		require.NoError(t, r.Close())
		require.Empty(t, string(out), "src %q", src)
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
