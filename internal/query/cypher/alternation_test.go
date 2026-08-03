package cypher_test

import (
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
			name: "the same alternation written twice is reported once",
			src: "MATCH (:Person)-[r:AUTHORED|LIKES]->(p:Post), " +
				"(:Person)-[s:AUTHORED|LIKES]->(p) RETURN p.id",
			want: []string{":AUTHORED|LIKES"},
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
	for _, src := range []string{"", "not a query at all", "MATCH (:Person)-[r:A|B]->", "((("} {
		require.NotPanics(t, func() { cypher.RelationshipTypeAlternations(src) }, "src %q", src)
	}
}
