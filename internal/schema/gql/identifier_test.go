package gql_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/graph"
	"github.com/areqag/gqlc/internal/schema"
	"github.com/areqag/gqlc/internal/schema/gql"
)

func parseSchema(t *testing.T, src string) (schema.Schema, error) {
	t.Helper()
	return gql.New().Parse(strings.NewReader(src))
}

// TestADelimitedIdentifierDenotesTheSameNameAsTheBareSpelling is the whole of
// bd gqlc-tzu9r in one table. A delimited identifier's delimiters are syntax,
// not content (GQL 18.9), so every row here spells ONE name three ways and the
// three must reach the same schema.
//
// The assertion is on the entire schema.Schema rather than on the one field
// each row varies. That is deliberate: the defect it guards is a name reaching
// the model undecoded, and where such a name LANDS differs per position — a
// graph type name lands in Schema.Name, a label in a map KEY, a property name
// in a map key one level down. Comparing the whole value catches the leak
// wherever it comes to rest, and catches a decode that fixes one position by
// corrupting another.
//
// Comparing against a parse of the bare spelling, rather than against a
// hand-written expected value, is what keeps the row honest about what it
// pins: the claim is that the three spellings AGREE, not that any one of them
// produces some particular literal. A hand-written expectation would have to
// be re-blessed whenever the model changes shape, and would then be re-blessed
// to whatever the code did that day.
func TestADelimitedIdentifierDenotesTheSameNameAsTheBareSpelling(t *testing.T) {
	for _, tt := range []struct {
		position string
		bare     string
		accent   string
		double   string
	}{
		{
			"graph type name",
			"CREATE GRAPH TYPE G { (:A) }",
			"CREATE GRAPH TYPE `G` { (:A) }",
			`CREATE GRAPH TYPE "G" { (:A) }`,
		},
		{
			"node label",
			"CREATE GRAPH TYPE G { (:A) }",
			"CREATE GRAPH TYPE G { (:`A`) }",
			`CREATE GRAPH TYPE G { (:"A") }`,
		},
		{
			"edge label",
			"CREATE GRAPH TYPE G { (:A), (:B), (:A)-[:E]->(:B) }",
			"CREATE GRAPH TYPE G { (:A), (:B), (:A)-[:`E`]->(:B) }",
			`CREATE GRAPH TYPE G { (:A), (:B), (:A)-[:"E"]->(:B) }`,
		},
		{
			"property name",
			"CREATE GRAPH TYPE G { (:A { city :: INT64 }) }",
			"CREATE GRAPH TYPE G { (:A { `city` :: INT64 }) }",
			`CREATE GRAPH TYPE G { (:A { "city" :: INT64 }) }`,
		},
		{
			"record field name",
			"CREATE GRAPH TYPE G { (:A { p :: RECORD { city :: INT64 NOT NULL } }) }",
			"CREATE GRAPH TYPE G { (:A { p :: RECORD { `city` :: INT64 NOT NULL } }) }",
			`CREATE GRAPH TYPE G { (:A { p :: RECORD { "city" :: INT64 NOT NULL } }) }`,
		},
	} {
		t.Run(tt.position, func(t *testing.T) {
			want, err := parseSchema(t, tt.bare)
			require.NoError(t, err,
				"the bare control does not parse, so every row below compares against nothing")

			for _, spelling := range []struct{ kind, src string }{
				{"accent-quoted", tt.accent},
				{"double-quoted", tt.double},
			} {
				t.Run(spelling.kind, func(t *testing.T) {
					got, err := parseSchema(t, spelling.src)
					require.NoError(t, err,
						"the delimited spelling does not parse, so this row tests nothing")
					require.Equal(t, want, got,
						"the %s spelling of the %s reaches the model as different bytes "+
							"from the bare one, so the two spellings declare two types that "+
							"never unify", spelling.kind, tt.position)
				})
			}
		})
	}
}

// TestARecordFieldNameReachesTheEncodingDecoded is bd gqlc-tzu9r's own
// falsifier, kept as a row of its own rather than folded into the table above.
// The table asserts that the three spellings AGREE; this asserts what they agree
// ON, which is the half a comparison against a control cannot state. If both the
// bare and the delimited readings regressed together the table would still pass.
//
// The encoding is the thing the bead is about: it is what makes two spellings of
// a record ONE type, so a schema writing `city` and a query writing city declared
// two carriers and two helper pairs for what the author wrote once.
func TestARecordFieldNameReachesTheEncodingDecoded(t *testing.T) {
	for _, spelling := range []string{"city", "`city`", `"city"`} {
		t.Run(spelling, func(t *testing.T) {
			got, err := parseSchema(t,
				"CREATE GRAPH TYPE G { (:A { p :: RECORD { "+spelling+" :: INT64 NOT NULL } }) }")
			require.NoError(t, err)

			node := got.Nodes["A"]
			require.Equal(t, graph.PropertyType("RECORD<city INT64 NOT NULL>"), node.Properties["p"].Type)
		})
	}
}

// TestTwoSpellingsOfOneNameCollide is the consequence the table above cannot
// reach: if the three spellings denote one name, then writing two of them side
// by side declares the same name twice, and the duplicate rules must see it.
//
// It is a separate claim from equality because a decode placed BELOW the
// duplicate check would satisfy the table and fail here — the model would hold
// two identical names and the map that was supposed to notice would have been
// consulted on the source bytes. That is the ordering these rows pin, and the
// reason the decode sits at the identifier read rather than after it.
func TestTwoSpellingsOfOneNameCollide(t *testing.T) {
	t.Run("record field names", func(t *testing.T) {
		_, err := parseSchema(t,
			"CREATE GRAPH TYPE G { (:A { p :: RECORD { city :: INT64, `city` :: STRING } }) }")
		require.ErrorIs(t, err, gql.ErrDuplicateFieldName)
	})
	t.Run("property names", func(t *testing.T) {
		_, err := parseSchema(t,
			"CREATE GRAPH TYPE G { (:A { city :: INT64, `city` :: STRING }) }")
		require.ErrorIs(t, err, gql.ErrDuplicatePropertyName)
	})
}

// TestDelimitedIdentifierEscapes walks the decoder over the forms
// ESCAPED_CHARACTER admits (GQL.g4:3145-3186) and the doubled-delimiter rule
// beside it, one row per decision the byte walk makes.
//
// Each row asserts the DECODED name rather than merely that the source parses,
// because every one of these parsed before this bead too — the lexer has always
// admitted them, and it was the reader above it that took the bytes as the name.
//
// The doubled-delimiter rows are the reason decoding cannot live in the model
// constructors: each decodes to a name that CONTAINS the byte that delimited it,
// so a bare string arriving at graph.RecordOf cannot be asked whether it is
// still quoted.
func TestDelimitedIdentifierEscapes(t *testing.T) {
	for _, tt := range []struct {
		name     string
		spelling string
		want     string
	}{
		{"an undelimited body is itself", "`abc`", "abc"},
		{"a doubled accent is one accent", "`a``b`", "a`b"},
		{"a doubled double quote is one double quote", `"a""b"`, `a"b`},
		{"the other delimiter is ordinary content", "`a\"\"b`", `a""b`},
		{"an escaped accent", "`a\\`b`", "a`b"},
		{"an escaped reverse solidus", `"a\\b"`, `a\b`},
		{"an escaped tab", `"a\tb"`, "a\tb"},
		{"a four-digit unicode escape", "`a" + "\\" + "u0041b`", "aAb"},
		{"a six-digit unicode escape", `"a\U01F600b"`, "a\U0001F600b"},
		{"a non-ASCII body copies through", "`քաղաք`", "քաղաք"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseSchema(t, "CREATE GRAPH TYPE "+tt.spelling+" { (:A) }")
			require.NoError(t, err, "the spelling must parse, or the row asserts nothing")
			require.Equal(t, tt.want, got.Name)
		})
	}
}

// TestDelimitedIdentifierRefusals is the other half of the decoder: the names it
// can read the shape of and not the meaning of.
//
// Every source here parses — the lexer admits all four — so before this bead each
// produced a name, silently. The rows are per position as well as per refusal
// because the refusal must reach every identifier read: nine sites were converted
// and a conversion missed at one of them leaves that position minting the old
// name while the others refuse.
func TestDelimitedIdentifierRefusals(t *testing.T) {
	for _, tt := range []struct {
		name string
		src  string
		want error
	}{
		{
			"a no-escape graph type name",
			`CREATE GRAPH TYPE @"G" { (:A) }`,
			gql.ErrNoEscapeIdentifier,
		},
		{
			"a no-escape label",
			`CREATE GRAPH TYPE G { (:@"A") }`,
			gql.ErrNoEscapeIdentifier,
		},
		{
			"a no-escape property name",
			`CREATE GRAPH TYPE G { (:A { @"city" :: INT64 }) }`,
			gql.ErrNoEscapeIdentifier,
		},
		{
			"a no-escape record field name",
			`CREATE GRAPH TYPE G { (:A { p :: RECORD { @"city" :: INT64 } }) }`,
			gql.ErrNoEscapeIdentifier,
		},
		{
			"an empty graph type name",
			`CREATE GRAPH TYPE "" { (:A) }`,
			gql.ErrEmptyIdentifier,
		},
		{
			"an empty label",
			`CREATE GRAPH TYPE G { (:"") }`,
			gql.ErrEmptyIdentifier,
		},
		{
			"an empty property name",
			"CREATE GRAPH TYPE G { (:A { `` :: INT64 }) }",
			gql.ErrEmptyIdentifier,
		},
		{
			"an unpaired surrogate",
			`CREATE GRAPH TYPE "\uD800" { (:A) }`,
			gql.ErrIdentifierEscape,
		},
		{
			"a six-digit escape above the Unicode maximum",
			`CREATE GRAPH TYPE "\U110000" { (:A) }`,
			gql.ErrIdentifierEscape,
		},
		{
			"a label carrying the key separator",
			"CREATE GRAPH TYPE G { (:`A&B`) }",
			gql.ErrAmpersandInLabel,
		},
		{
			"an edge label carrying the key separator",
			"CREATE GRAPH TYPE G { (:A), (:B), (:A)-[:`E&F`]->(:B) }",
			gql.ErrAmpersandInLabel,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseSchema(t, tt.src)
			require.ErrorIs(t, err, tt.want)
		})
	}
}

// TestAnAmpersandRefusalIsAboutTheAmpersand is ErrAmpersandInLabel's negative
// control, and it is the row that says the refusal is narrow. A guard spelled
// "refuse every delimited label" would pass every row of the table above and be
// wrong about the whole feature this bead exists to add.
//
// The two names differ by exactly the one byte the guard is about, and the
// admitted one keys as the single label AB — not as the two-label set the refused
// spelling would have forged, which is the collision itself stated as a value.
func TestAnAmpersandRefusalIsAboutTheAmpersand(t *testing.T) {
	got, err := parseSchema(t, "CREATE GRAPH TYPE G { (:`AB`) }")
	require.NoError(t, err)
	require.Contains(t, got.Nodes, graph.LabelSetKey("AB"))

	require.Equal(t, graph.LabelSet{"A", "B"}.Key(), graph.LabelSet{"A&B"}.Key(),
		"the forgery the refusal prevents: if these ever differ, bd gqlc-yd4ba has landed and the refusal can be narrowed or dropped")
}

// TestTheCorpusDelimitedIdentifiersFixtureResolvesDecoded pins the model that
// corpus file 12.6-graph-type-statement/delimited_identifiers.gql produces.
//
// The corpus manifest already lists it as `resolves`, and that is all it says: a
// resolving entry names no schema, so the file went on passing while both its
// names carried their delimiters (corpus_test.go states this limit outright —
// the corpus is a coverage instrument, not a model-identity instrument).
//
// This is the hand-written pin the corpus asks for in its place. It is not a
// golden snapshot: gqlc-exl's standing argument is that round-tripping launders
// current behaviour into "expected", and this file's current behaviour is
// precisely what was wrong. The two names below are written out, and the reason
// the file is worth a row of its own is that it spells BOTH delimiter kinds in
// one declaration — the mixed source no single-spelling row reaches.
func TestTheCorpusDelimitedIdentifiersFixtureResolvesDecoded(t *testing.T) {
	src, err := os.ReadFile(filepath.Join(
		fixtureDir, "corpus", "12.6-graph-type-statement", "delimited_identifiers.gql"))
	require.NoError(t, err)

	got, err := parseSchema(t, string(src))
	require.NoError(t, err)
	require.Equal(t, "Seed", got.Name, "the accent-quoted graph type name")
	require.Contains(t, got.Nodes, graph.LabelSetKey("Person"), "the double-quoted label")
}
