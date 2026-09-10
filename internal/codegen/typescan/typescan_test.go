package typescan_test

import (
	"io/fs"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/codegen/typescan"
	"github.com/areqag/gqlc/internal/graph"
)

// The fixtures under testdata are synthetic sources, not goldens, and they
// are on disk rather than in string constants because the API takes a path:
// PropertyTypes and PropertyArms hand it to parser.ParseFile with a nil
// source, so the only way to drive them is a file that exists.
//
// They carry the `.go.txt` suffix this tree's other codegen fixtures carry,
// and for a reason of their own on top of that one: two of them hold shapes
// a Go compiler refuses, and go/parser does not type-check, so a `.go` name
// would be a standing error report for anything that reads the directory
// without knowing testdata is not built. The parser is indifferent to the
// extension; the assertions below read whatever path they were handed.
//
// They exist because internal/graph/propertytype.go reaches none of the
// refusals below and must not be bent into reaching them — two of these
// shapes a compiler would refuse outright, and the backends' walks therefore
// run this package in its passing direction only. The mutation battery on bd
// gqlc-ozdkx screened both walks THROUGH age and neo4j, which is real
// coverage of the happy paths and cannot be coverage of these (bd
// gqlc-2aoik).
//
// Every row holds the WHOLE message rather than a substring of it. The
// refusals interpolate a name set, but in source order, so there is nothing
// here for the walk to choose and no reason to assert less than the message;
// and the file the message names is the property the package doc claims and
// the one a substring match on the complaint alone would not notice losing.
func fixture(name string) string { return filepath.Join("testdata", name) }

// TestPropertyTypes drives each of PropertyTypes' returns from its own
// fixture, and holds the clean one to an exact table so a refusal that
// stopped refusing could not pass as a smaller answer.
func TestPropertyTypes(t *testing.T) {
	// C0. The control reads clean, and reads ONLY the PropertyType block:
	// its second const block declares a family this walk cannot take a value
	// off, which is the case the `read != 0` half of the refusal exists to
	// tolerate. A row asserting merely that no error came back would pass
	// with an empty table.
	t.Run("a graph-like source reads its table and nothing else", func(t *testing.T) {
		got, err := typescan.PropertyTypes(fixture("graph_like.go.txt"))
		require.NoError(t, err)
		require.Equal(t, map[graph.PropertyType]string{
			"STRING": "TypeString",
			"BOOL":   "TypeBool",
		}, got)
	})

	// Both shapes PropertyTypes' doc names, in one block: the message must
	// carry both, because a refusal that reported only the first would leave
	// the second dropped in exactly the way the doc says it must not be.
	t.Run("a block mixing readable and unreadable specs", func(t *testing.T) {
		source := fixture("untyped_and_inherited.go.txt")
		_, err := typescan.PropertyTypes(source)
		require.EqualError(t, err, source+": a const block declaring PropertyType constants also "+
			"declares [TypeAlias TypeUntyped], which this walk cannot read a PropertyType value off")
	})

	// The length half of the readability test. Without it the walk indexes
	// the second name into a one-element value list, so what this row holds
	// off is a panic rather than a wrong answer.
	t.Run("a spec with more names than values", func(t *testing.T) {
		source := fixture("names_without_values.go.txt")
		_, err := typescan.PropertyTypes(source)
		require.EqualError(t, err, source+": a const block declaring PropertyType constants also "+
			"declares [TypePair TypeSpare], which this walk cannot read a PropertyType value off")
	})

	t.Run("a value that is not a literal", func(t *testing.T) {
		source := fixture("value_not_a_literal.go.txt")
		_, err := typescan.PropertyTypes(source)
		require.EqualError(t, err, source+": constant TypeDerived is not a literal")
	})

	// A literal go/parser reports like any other and strconv.Unquote
	// refuses, which is the only thing separating this row from a string.
	t.Run("a literal that will not unquote", func(t *testing.T) {
		source := fixture("value_not_a_string.go.txt")
		_, err := typescan.PropertyTypes(source)
		require.EqualError(t, err, source+": constant TypeNumeric: invalid syntax")
		require.ErrorIs(t, err, strconv.ErrSyntax,
			"the unquote failure is not wrapped, so a caller cannot tell it from a refusal this package composed itself")
	})

	// The realistic trigger for the parse arm is the one the package doc is
	// written about: a backend's table moves and the path the caller passes
	// no longer names a file. Held by containment rather than equality
	// because the rest of the text is the OS's.
	t.Run("a source that cannot be read", func(t *testing.T) {
		source := fixture("no_such_file.go.txt")
		_, err := typescan.PropertyTypes(source)
		require.ErrorIs(t, err, fs.ErrNotExist)
		require.ErrorContains(t, err, source+" does not parse",
			"the failure does not name the file it was read from, so a caller holding two sources cannot tell which moved")
	})
}

// TestPropertyArmsNamesTheSourceItCannotRead covers PropertyArms' only
// return that is not a walk of a well-formed file. Its collecting half is
// screened through both backends, whose suites redden when the arm collector
// is blinded (bd gqlc-ozdkx); this is the half that walk cannot reach,
// because a backend's type table is a file that is there.
func TestPropertyArmsNamesTheSourceItCannotRead(t *testing.T) {
	source := fixture("no_such_file.go.txt")
	_, err := typescan.PropertyArms(source, "Property")
	require.ErrorIs(t, err, fs.ErrNotExist)
	require.ErrorContains(t, err, source+" does not parse",
		"the failure does not name the file it was read from, so a caller holding two sources cannot tell which moved")
}
