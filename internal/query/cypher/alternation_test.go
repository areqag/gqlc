package cypher_test

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/query/cypher"
)

// --- relationship-type alternations in the query TEXT (gqlc-35yu.14) ---
//
// A backend that ships the author's text verbatim (ADR 0005) onto a server
// whose parser has no `|` in a relationship detail has to decide from the
// text. Three things make the text and the parsed model disagree, and each
// gets a case below: an alternation the author never projects reaches no
// column; a re-bound relationship variable's occurrences intersect, so the
// binding can carry one label while the text carries two; and `|` appears in
// two productions that name no relationship type at all.

// TestRelationshipTypeAlternationsReadsTheText walks the shapes where the
// answer differs from anything the query model could be asked.
func TestRelationshipTypeAlternationsReadsTheText(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
		want []string
	}{
		{
			name: "no alternation is no answer",
			src:  "MATCH (:Person)-[r:AUTHORED]->(p:Post) RETURN r",
			want: nil,
		},
		{
			name: "an alternation is reported as written",
			src:  "MATCH (:Person)-[r:AUTHORED|LIKES]->(p:Post) RETURN r",
			want: []string{":AUTHORED|LIKES"},
		},
		{
			name: "an alternation the query never projects still counts",
			src:  "MATCH (:Person)-[r:AUTHORED|LIKES]->(p:Post) RETURN p.id",
			want: []string{":AUTHORED|LIKES"},
		},
		{
			name: "an anonymous edge binds nothing and still counts",
			src:  "MATCH (:Person)-[:AUTHORED|LIKES]->(p:Post) RETURN p.id",
			want: []string{":AUTHORED|LIKES"},
		},
		{
			name: "a re-bound variable narrows the binding, not the text",
			src:  "MATCH (:Person)-[r:AUTHORED|LIKES]->(p:Post), (:Person)-[r:AUTHORED]->(p:Post) RETURN r",
			want: []string{":AUTHORED|LIKES"},
		},
		{
			name: "a type the schema does not declare is still in the text",
			src:  "MATCH (:Person)-[r:AUTHORED|FLAGGED]->(p:Post) RETURN r",
			want: []string{":AUTHORED|FLAGGED"},
		},
		{
			name: "three types are one alternation",
			src:  "MATCH (:Person)-[r:AUTHORED|LIKES|REPOSTED]->(p:Post) RETURN r",
			want: []string{":AUTHORED|LIKES|REPOSTED"},
		},
		{
			name: "the optional colon before a later type is reported as written",
			src:  "MATCH (:Person)-[r:AUTHORED|:LIKES]->(p:Post) RETURN r",
			want: []string{":AUTHORED|:LIKES"},
		},
		{
			// The witness is how many times the production spells a
			// relationship-type NAME, not how many DISTINCT names it
			// spells. Cypher.g4 §oC_RelationshipTypes is
			// `':' SP? oC_RelTypeName ( SP? '|' ':'? SP? oC_RelTypeName )*`,
			// which admits a repeat, and the '|' the repeat needs is the
			// same character AGE 1.7.0's parser has no production for.
			// Downstream the repeat is invisible: the resolver narrows the
			// candidates to one ResolvedEdge, which the AGE column gate
			// serves — so a scan counting distinct names reports nothing
			// here and lets `gqlc generate` exit 0 over a package whose
			// every call is SQLSTATE 42601.
			name: "a type repeated across the alternation still counts",
			src:  "MATCH (:Person)-[r:LIKES|LIKES]->(p:Post) RETURN r",
			want: []string{":LIKES|LIKES"},
		},
		{
			// The same repeat in the legacy spelling, where the second
			// name carries its own colon: a scan comparing the names as
			// the grammar reads them collapses this one too.
			name: "a type repeated in the legacy spelling still counts",
			src:  "MATCH (:Person)-[r:LIKES|:LIKES]->(p:Post) RETURN r",
			want: []string{":LIKES|:LIKES"},
		},
		{
			// Whitespace inside the alternation is REPORTED, not dropped.
			// SP is a default-channel token in this grammar, so the
			// context's text concatenation keeps it, and the refusal built
			// on this prints only characters the author wrote. Pinned here
			// because nothing else pins the spaced rendering, and a
			// reconstruction that dropped the spaces would print a query
			// nobody wrote.
			name: "whitespace inside the alternation is reported as written",
			src:  "MATCH (:Person)-[r:AUTHORED | LIKES]->(p:Post) RETURN r",
			want: []string{":AUTHORED | LIKES"},
		},
		{
			name: "a variable-length alternation counts",
			src:  "MATCH (:Person)-[r:AUTHORED|LIKES*1..2]->(p:Post) RETURN p.id",
			want: []string{":AUTHORED|LIKES"},
		},
		{
			name: "an alternation inside EXISTS counts, because the server parses it too",
			src:  "MATCH (p:Person) WHERE exists { (p)-[:AUTHORED|LIKES]->(:Post) }\nRETURN p.id",
			want: []string{":AUTHORED|LIKES"},
		},
		{
			name: "an alternation in a write clause counts",
			src:  "MATCH (p:Person)-[r:AUTHORED|LIKES]->(:Post) DELETE r",
			want: []string{":AUTHORED|LIKES"},
		},
		{
			name: "two different alternations are both reported, in source order",
			src: "MATCH (:Person)-[r:LIKES|REPOSTED]->(p:Post), " +
				"(:Person)-[s:AUTHORED|FLAGGED]->(p) RETURN p.id",
			want: []string{":LIKES|REPOSTED", ":AUTHORED|FLAGGED"},
		},
		{
			// Twice, not once. Two occurrences are two patterns the
			// author has to rewrite, and the position each carries is
			// what makes naming both an answer rather than the same
			// answer printed twice (bd gqlc-rmzg). The positions are
			// asserted by TestRelationshipTypeAlternationsLocatesEachOne;
			// what this row holds is that neither is dropped.
			name: "the same alternation written twice is reported twice",
			src: "MATCH (:Person)-[r:AUTHORED|LIKES]->(p:Post), " +
				"(:Person)-[s:AUTHORED|LIKES]->(p) RETURN p.id",
			want: []string{":AUTHORED|LIKES", ":AUTHORED|LIKES"},
		},
		{
			name: "a list comprehension's '|' names no relationship type",
			src:  "MATCH (p:Person) RETURN [x IN p.tags | x] AS t",
			want: nil,
		},
		{
			name: "a pattern comprehension's '|' names no relationship type",
			src:  "MATCH (p:Person) RETURN [(p)-[:AUTHORED]->(o:Post) | o.id] AS ids",
			want: nil,
		},
		{
			name: "a pattern comprehension carrying an alternation counts once",
			src:  "MATCH (p:Person) RETURN [(p)-[:AUTHORED|LIKES]->(o:Post) | o.id] AS ids",
			want: []string{":AUTHORED|LIKES"},
		},
		{
			name: "a '|' inside a string literal names no relationship type",
			src:  "MATCH (p:Person) WHERE p.name = 'AUTHORED|LIKES' RETURN p.id",
			want: nil,
		},
		{
			name: "a '|' inside a comment names no relationship type",
			src:  "// AUTHORED|LIKES\nMATCH (:Person)-[r:AUTHORED]->(p:Post) RETURN p.id",
			want: nil,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, alternationTexts(cypher.RelationshipTypeAlternations(tc.src)))
		})
	}
}

// alternationTexts is the spellings alone, for the rows above, which are
// about WHAT is read out of a text rather than where each one sits.
func alternationTexts(alts []cypher.Alternation) []string {
	if len(alts) == 0 {
		return nil
	}
	out := make([]string, len(alts))
	for i, a := range alts {
		out[i] = a.Text
	}
	return out
}

// TestRelationshipTypeAlternationsLocatesEachOne is the other half: the
// coordinate, which is the only thing separating two occurrences of one
// spelling.
//
// Every expected position here was counted off the source by hand rather
// than recorded from a run, because a coordinate copied out of the answer it
// is meant to check certifies itself. Two conventions are asserted at once
// and both are off-by-one hazards: the line is ANTLR's, which counts from 1,
// and the column is NOT — ANTLR counts a line's characters from 0, so every
// row here would be one short if the conversion were dropped.
//
// The rows vary the two axes independently on purpose. Two alternations
// sharing a line differ only in the column, two on different lines at one
// column differ only in the line, and one row sits where the line and the
// column are not equal, so a coordinate that swapped them reds.
func TestRelationshipTypeAlternationsLocatesEachOne(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
		want []cypher.Alternation
	}{
		{
			// "MATCH " is 6, "(:Person)" takes 7-15 and "-[r" takes
			// 16-18, so the ':' opening the detail is the 19th character.
			name: "the position is the ':' that opens the relationship detail",
			src:  "MATCH (:Person)-[r:AUTHORED|LIKES]->(p:Post) RETURN r",
			want: []cypher.Alternation{{Text: ":AUTHORED|LIKES", Line: 1, Column: 19}},
		},
		{
			// The same prefix again after ", ": ":AUTHORED|LIKES" is 15
			// characters ending at 33, "]->" ends at 36, "(p:Post)" at
			// 44, "," at 45, " " at 46, "(:Person)" at 55 and "-[s" at
			// 58 — so the second ':' is the 59th.
			name: "two occurrences of one spelling on one line differ by the column",
			src: "MATCH (:Person)-[r:AUTHORED|LIKES]->(p:Post), " +
				"(:Person)-[s:AUTHORED|LIKES]->(p) RETURN p.id",
			want: []cypher.Alternation{
				{Text: ":AUTHORED|LIKES", Line: 1, Column: 19},
				{Text: ":AUTHORED|LIKES", Line: 1, Column: 59},
			},
		},
		{
			name: "two occurrences of one spelling on two lines differ by the line",
			src: "MATCH (:Person)-[r:AUTHORED|LIKES]->(p:Post)\n" +
				"MATCH (:Person)-[s:AUTHORED|LIKES]->(p)\n" +
				"RETURN p.id",
			want: []cypher.Alternation{
				{Text: ":AUTHORED|LIKES", Line: 1, Column: 19},
				{Text: ":AUTHORED|LIKES", Line: 2, Column: 19},
			},
		},
		{
			// Line 2 is "MATCH (p)-[r:LIKES|REPOSTED]->(q:Post)": "MATCH "
			// is 6, "(p)" takes 7-9 and "-[r" takes 10-12, so the ':' is
			// the 13th. Neither axis equals the other, so a coordinate
			// reporting the column as the line reds here and nowhere else.
			name: "a line and a column that are not equal",
			src: "MATCH (p:Person)\n" +
				"MATCH (p)-[r:LIKES|REPOSTED]->(q:Post)\n" +
				"RETURN q.id",
			want: []cypher.Alternation{{Text: ":LIKES|REPOSTED", Line: 2, Column: 13}},
		},
		{
			// Whitespace inside the alternation moves what follows it,
			// and the position is of the START, so it does not move.
			name: "spaces inside the alternation do not move its position",
			src:  "MATCH (:Person)-[r:AUTHORED | LIKES]->(p:Post) RETURN p.id",
			want: []cypher.Alternation{{Text: ":AUTHORED | LIKES", Line: 1, Column: 19}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, cypher.RelationshipTypeAlternations(tc.src))
		})
	}
}

// TestRelationshipTypeAlternationsAgreesWithTheParserOnTheAcceptedTexts is the
// cross-check that keeps the scan honest about the one thing it must never do:
// report an alternation in a text the grammar reads as naming a single
// relationship type per pattern. Every source above that this asserts nothing
// on is one Parse also accepts, so the two readings of the same bytes are
// compared rather than the scan being trusted alone.
func TestRelationshipTypeAlternationsAgreesWithTheParserOnTheAcceptedTexts(t *testing.T) {
	for _, src := range []string{
		"MATCH (:Person)-[r:AUTHORED]->(p:Post) RETURN r",
		"MATCH (p:Person) RETURN [x IN p.tags | x] AS t",
		"MATCH (p:Person) WHERE p.name = 'AUTHORED|LIKES' RETURN p.id",
	} {
		t.Run(src, func(t *testing.T) {
			_, err := cypher.New().Parse(strings.NewReader(src))
			require.NoError(t, err, "the fixture must be a text the grammar accepts")
			require.Empty(t, cypher.RelationshipTypeAlternations(src))
		})
	}
}

// TestRelationshipTypeAlternationsIsTotal pins the branch the CLI cannot
// reach: a text the grammar refuses gets an answer rather than a panic, so a
// caller holding bytes from anywhere never has to have an error path it has no
// answer for. The recovering parser's reading of a broken text is not pinned —
// only that asking is safe.
func TestRelationshipTypeAlternationsIsTotal(t *testing.T) {
	for _, src := range malformedTexts {
		require.NotPanics(t, func() { cypher.RelationshipTypeAlternations(src) }, "src %q", src)
	}
}

// malformedTexts are texts the grammar cannot read. Being total on them is
// one requirement and being quiet on them is the other, so the two tests
// range over the same list.
//
// The last two are the ones the LEXER refuses rather than the parser — an
// unterminated string literal and a character the token vocabulary has no
// rule for. Every text above them tokenises cleanly and fails only in the
// parse, so a list without them measures one of the two console listeners
// and reports on both.
var malformedTexts = []string{
	"", "not a query at all", "MATCH (:Person)-[r:A|B]->", "(((",
	"MATCH (p:Person) RETURN 'unterminated",
	"MATCH (p:Person) RETURN #",
}

// TestRelationshipTypeAlternationsWritesNothingToStderr pins the other half
// of that totality. ANTLR installs a console error listener by default, on
// the lexer and on the parser alike, and it writes to os.Stderr rather than
// returning anything — so a scan that left them attached would answer the
// caller correctly and print a raw grammar diagnostic over the top of
// whatever `gqlc generate` was saying, from a library function with no
// output channel of its own and no way for the caller to suppress it.
//
// Removing either RemoveErrorListeners call reddens this and nothing else:
// the answers are unchanged, because ANTLR recovers and walks the tree it
// built either way, which is exactly why the noise is invisible to every
// assertion about the return value.
func TestRelationshipTypeAlternationsWritesNothingToStderr(t *testing.T) {
	for _, src := range malformedTexts {
		t.Run(src, func(t *testing.T) {
			r, w, err := os.Pipe()
			require.NoError(t, err)
			saved := os.Stderr
			os.Stderr = w
			// Restored before the read: the scan is synchronous and the
			// pipe buffer holds far more than a grammar diagnostic, so
			// closing the writer first is what makes the read terminate.
			cypher.RelationshipTypeAlternations(src)
			os.Stderr = saved
			require.NoError(t, w.Close())

			printed, err := io.ReadAll(r)
			require.NoError(t, err)
			require.NoError(t, r.Close())
			require.Empty(t, string(printed),
				"the scan must answer its caller and say nothing to anyone else")
		})
	}
}
