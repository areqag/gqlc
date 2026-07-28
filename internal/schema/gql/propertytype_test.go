package gql

import (
	"sort"
	"strings"
	"testing"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/grammar/gql/gen"
	"github.com/areqag/gqlc/internal/graph"
	"github.com/areqag/gqlc/internal/schema"
)

// parseFirstProperty drives the real grammar to the first propertyType context
// of the first node and returns the property the parser would record for it,
// so these tests exercise the actual spelling extraction, not a hand-built tree.
func parseFirstProperty(t *testing.T, valueType string) (schema.Property, error) {
	t.Helper()
	src := "CREATE PROPERTY GRAPH TYPE T AS { (:A { p :: " + valueType + " }) }"

	errs := &listener{}
	lex := gen.NewGQLLexer(antlr.NewInputStream(src))
	lex.RemoveErrorListeners()
	lex.AddErrorListener(errs)
	ts := antlr.NewCommonTokenStream(lex, antlr.TokenDefaultChannel)
	gp := gen.NewGQLParser(ts)
	gp.RemoveErrorListeners()
	gp.AddErrorListener(errs)
	tree := gp.GqlProgram()
	require.NoError(t, errs.err, "fixture must parse; %q is not grammar-valid", valueType)

	c := &propertyCollector{ts: ts}
	antlr.NewParseTreeWalker().Walk(c, tree)
	require.NotNil(t, c.ctx, "no propertyType context found")
	return property(c.ctx, ts)
}

type propertyCollector struct {
	*gen.BaseGQLListener
	ts  *antlr.CommonTokenStream
	ctx *gen.PropertyTypeContext
}

func (c *propertyCollector) EnterPropertyType(ctx *gen.PropertyTypeContext) {
	if c.ctx == nil {
		c.ctx = ctx
	}
}

// typePin is one spelling asserted to resolve, end to end through the real
// grammar, to a named model constant. typeEquivalence is one pair asserted to
// resolve to the same constant as each other, whichever that turns out to be —
// order carries no meaning, since the relation it feeds is symmetric.
//
// Both exist as package-level tables rather than literals inside their tests so
// that TestTypeSpellingsEveryRowPinned can range over them. That gate is what
// stops a typeSpellings row from being added, or silently repointed, with no
// end-to-end assertion behind it; see its doc comment for why the grounding is
// computed from these two shapes rather than read off typeSpellings itself.
type typePin struct {
	spelling string
	want     graph.PropertyType
}

type typeEquivalence struct{ a, b string }

// typeMappingPins is the base of the grounding relation: spellings whose
// resolved constant is written out by hand here, independently of the map under
// test.
var typeMappingPins = []typePin{
	{"STRING", graph.TypeString},
	{"CHAR", graph.TypeString},
	{"VARCHAR", graph.TypeString},
	{"BYTES", graph.TypeBytes},
	{"BINARY", graph.TypeBytes},
	{"VARBINARY", graph.TypeBytes},
	{"BOOL", graph.TypeBool},
	{"BOOLEAN", graph.TypeBool},
	{"DATE", graph.TypeDate},

	{"ZONED TIME", graph.TypeTime},
	{"TIME WITH TIME ZONE", graph.TypeTime},

	{"LOCAL TIME", graph.TypeLocalTime},
	{"TIME WITHOUT TIME ZONE", graph.TypeLocalTime},

	{"TIMESTAMP", graph.TypeTimestamp},
	{"ZONED DATETIME", graph.TypeTimestamp},
	{"LOCAL DATETIME", graph.TypeTimestamp},
	{"TIMESTAMP WITH TIME ZONE", graph.TypeTimestamp},
	{"TIMESTAMP WITHOUT TIME ZONE", graph.TypeTimestamp},

	{"DURATION(YEAR TO MONTH)", graph.TypeDuration},
	{"DURATION(DAY TO SECOND)", graph.TypeDuration},

	{"INT", graph.TypeInt},
	{"INTEGER", graph.TypeInt},
	{"SMALLINT", graph.TypeInt16},
	{"SMALL INTEGER", graph.TypeInt16},
	{"BIGINT", graph.TypeInt64},
	{"BIG INTEGER", graph.TypeInt64},
	{"INT8", graph.TypeInt8},
	{"INTEGER8", graph.TypeInt8},
	{"INT256", graph.TypeInt256},

	{"UINT", graph.TypeUint},
	{"USMALLINT", graph.TypeUint16},
	{"UBIGINT", graph.TypeUint64},
	{"UINT8", graph.TypeUint8},
	{"UINT256", graph.TypeUint256},

	{"FLOAT", graph.TypeFloat},
	{"REAL", graph.TypeFloat32},
	{"DOUBLE", graph.TypeFloat64},
	{"DOUBLE PRECISION", graph.TypeFloat64},
	{"FLOAT16", graph.TypeFloat16},
	{"FLOAT32", graph.TypeFloat32},
	{"FLOAT64", graph.TypeFloat64},
	{"FLOAT128", graph.TypeFloat128},
	{"FLOAT256", graph.TypeFloat256},

	{"DECIMAL", graph.TypeDecimal},
	{"DEC", graph.TypeDecimal},
}

func TestPropertyTypeMapping(t *testing.T) {
	for _, tt := range typeMappingPins {
		t.Run(tt.spelling, func(t *testing.T) {
			got, err := parseFirstProperty(t, tt.spelling)
			require.NoError(t, err)
			require.Equal(t, tt.want, got.Type)
			require.Equal(t, "p", got.Name)
			require.True(t, got.Nullable, "GQL is nullable by default")
		})
	}
}

// TestPropertyLengthQualifiersDropped covers the length/precision parenthetical
// being stripped before normalisation (ADR 0002).
func TestPropertyLengthQualifiersDropped(t *testing.T) {
	cases := []struct {
		spelling string
		want     graph.PropertyType
	}{
		{"VARCHAR(255)", graph.TypeString},
		{"CHAR(8)", graph.TypeString},
		{"STRING(100)", graph.TypeString},
		{"DECIMAL(10, 2)", graph.TypeDecimal},
		{"FLOAT(10)", graph.TypeFloat},
	}

	for _, tt := range cases {
		t.Run(tt.spelling, func(t *testing.T) {
			got, err := parseFirstProperty(t, tt.spelling)
			require.NoError(t, err)
			require.Equal(t, tt.want, got.Type)
		})
	}
}

// widthFoldPins is the second base of the grounding relation: parenthesised
// widths whose folded constant is written out by hand, so a typeSpellings row
// reached only through truncateParenthetical still counts as pinned.
var widthFoldPins = []typePin{
	{"INT(8)", graph.TypeInt8},
	{"INT(16)", graph.TypeInt16},
	{"INT(32)", graph.TypeInt32},
	{"INT(64)", graph.TypeInt64},
	{"INT(128)", graph.TypeInt128},
	{"INT(256)", graph.TypeInt256},

	{"INTEGER(8)", graph.TypeInt8},
	{"INTEGER(16)", graph.TypeInt16},
	{"INTEGER(32)", graph.TypeInt32},
	{"INTEGER(64)", graph.TypeInt64},
	{"INTEGER(128)", graph.TypeInt128},
	{"INTEGER(256)", graph.TypeInt256},

	{"UINT(8)", graph.TypeUint8},
	{"UINT(16)", graph.TypeUint16},
	{"UINT(32)", graph.TypeUint32},
	{"UINT(64)", graph.TypeUint64},
	{"UINT(128)", graph.TypeUint128},
	{"UINT(256)", graph.TypeUint256},
}

// TestPropertyBinaryWidthParenthesisedFolds covers the ISO GQL binary-width
// parenthetical folding onto the corresponding width constant, so `INT(8)` and
// `INT8` resolve to the same PropertyType. The fold applies only where the
// grammar makes the parenthesised form a sibling of an explicit width token
// AND the parenthetical admits no scale: signedBinaryExactNumericType
// (GQL.g4:1801), unsignedBinaryExactNumericType (:1814), and
// verboseBinaryExactNumericType (:1827). It does NOT apply to FLOAT(p), whose
// parenthetical carries a scale slot (:1849) — that case is pinned in
// TestPropertyBinaryWidthNonFolds. Distinct from TestPropertyLengthQualifiersDropped,
// which pins the decimal/character-length parenthetical as still being dropped.
func TestPropertyBinaryWidthParenthesisedFolds(t *testing.T) {
	for _, tt := range widthFoldPins {
		t.Run(tt.spelling, func(t *testing.T) {
			got, err := parseFirstProperty(t, tt.spelling)
			require.NoError(t, err)
			require.Equal(t, tt.want, got.Type)
		})
	}
}

// TestPropertyBinaryWidthNonFolds pins the parenthesised spellings that fall
// through to the truncated-spelling lookup and land on the machine-word or
// bare type. Three flavours here, kept in one table because they share the
// fallback path:
//
//   - Bit-width binary integers with no exact model counterpart (INT(7),
//     INT(10)). Lossy — the author said 7 bits, the model says machine int.
//     ADR 0017 decides that this is accepted rather than rejected or rounded
//     up, so these rows pin a decision; gqlc-h9n.31 is what would change them.
//   - FLOAT(p) and FLOAT(p, s) (:1849). The parenthetical is byte-identical
//     in shape to DECIMAL(p, s) at :1832, not to the binary integers, and
//     nothing in the grammar establishes that FLOAT's precision counts bits.
//     FLOAT(16) therefore resolves to TypeFloat — 64-bit, a safe superset —
//     rather than the narrower TypeFloat16. gqlc-h9n.28 tracks the ISO
//     citation that would decide the fold direction.
//   - Length-qualifier and DECIMAL forms (STRING(5), VARCHAR(255), CHAR(8),
//     DEC(8), DECIMAL(10, 2)). ADR 0002 drops these; overlap with
//     TestPropertyLengthQualifiersDropped is intentional — this table pins
//     that they still land on the fallback even after the width fold.
func TestPropertyBinaryWidthNonFolds(t *testing.T) {
	cases := []struct {
		spelling string
		want     graph.PropertyType
	}{
		{"INT(7)", graph.TypeInt},
		{"INT(10)", graph.TypeInt},
		{"FLOAT(16)", graph.TypeFloat},
		{"FLOAT(24)", graph.TypeFloat},
		{"FLOAT(32)", graph.TypeFloat},
		{"FLOAT(64)", graph.TypeFloat},
		{"FLOAT(10, 2)", graph.TypeFloat},
		{"DEC(8)", graph.TypeDecimal},
		{"STRING(5)", graph.TypeString},
		{"VARCHAR(255)", graph.TypeString},
		{"CHAR(8)", graph.TypeString},
		{"DECIMAL(10, 2)", graph.TypeDecimal},
	}

	for _, tt := range cases {
		t.Run(tt.spelling, func(t *testing.T) {
			got, err := parseFirstProperty(t, tt.spelling)
			require.NoError(t, err)
			require.Equal(t, tt.want, got.Type)
		})
	}
}

// TestPropertyBinaryWidthWhitespaceAndCase covers spelling variants where
// the same construct can be written with internal whitespace inside the
// parenthetical, a trailing NOT NULL, or lowercased input, plus one row from
// the decimal branch: decimalExactNumericType (GQL.g4:1832) puts notNull
// inside the parenthesised alternative rather than as a sibling of it, so the
// canonicalSpelling ordering (trim `NOT NULL` first, collapse the
// parenthetical after) is what keeps `DECIMAL(10, 2) NOT NULL` reaching the
// truncated fallback rather than landing on a stray key.
func TestPropertyBinaryWidthWhitespaceAndCase(t *testing.T) {
	cases := []struct {
		spelling string
		want     graph.PropertyType
		nullable bool
	}{
		{"INT ( 8 )", graph.TypeInt8, true},
		{"INTEGER(8) NOT NULL", graph.TypeInt8, false},
		{"int(8)", graph.TypeInt8, true},
		{"DECIMAL(10, 2) NOT NULL", graph.TypeDecimal, false},
	}

	for _, tt := range cases {
		t.Run(tt.spelling, func(t *testing.T) {
			got, err := parseFirstProperty(t, tt.spelling)
			require.NoError(t, err)
			require.Equal(t, tt.want, got.Type)
			require.Equal(t, tt.nullable, got.Nullable)
		})
	}
}

// TestCanonicalSpellingCommaSpacingIrrelevant pins the one clause of
// collapseParenthetical's contract that no end-to-end fixture can reach: comma
// spacing inside a parenthetical must not change the lookup key.
//
// It asserts on canonicalSpelling directly, which the rest of this file only
// does through typeSpellingsRowFor, because the behaviour is currently
// unobservable from property(). The suite already drives every comma-bearing
// grammar arm — FLOAT(10, 2), DECIMAL(10, 2), DECIMAL(10, 2) NOT NULL, plus
// STRING(2, 5) and BYTES(1, 10) in corpus_area_d1_test.go — and none of them
// can see it, because no typeSpellings row holds a comma. Every comma-bearing
// spelling therefore misses the full lookup and resolves down
// truncateParenthetical, which discards the parenthetical the collapse just
// rewrote. Deleting both ReplaceAll calls leaves the tree green.
//
// The clause becomes load-bearing the moment a comma-bearing row exists, which
// is exactly what gqlc-5md adds: DECIMAL(10,2) would start hitting the full
// lookup, and an uncollapsed DECIMAL(10, 2) would miss it and fall back to bare
// DECIMAL. Hence a pin now rather than a deletion. gqlc-825 carries the proof
// that the two lines cannot change any output today.
//
// Asserting the equivalence rather than the literal key is deliberate: the
// canonical form is an internal spelling, and pinning it verbatim would fail on
// a re-spelling that kept the property that matters. Unlike gqlc-h9n.21, this
// stays green after gqlc-5md lands.
func TestCanonicalSpellingCommaSpacingIrrelevant(t *testing.T) {
	for _, spaced := range []string{
		"DECIMAL(10, 2)",
		"DECIMAL(10 ,2)",
		"DECIMAL(10 , 2)",
		"DECIMAL( 10,2 )",
	} {
		t.Run(spaced, func(t *testing.T) {
			require.Equal(t, canonicalSpelling("DECIMAL(10,2)"), canonicalSpelling(spaced))
		})
	}
}

// widthSpellingEquivalences pairs each explicit width token with its
// parenthesised spelling, read suffix-first: {"INT8", "INT(8)"}.
var widthSpellingEquivalences = []typeEquivalence{
	{"INT8", "INT(8)"},
	{"INT16", "INT(16)"},
	{"INT32", "INT(32)"},
	{"INT64", "INT(64)"},
	{"INT128", "INT(128)"},
	{"INT256", "INT(256)"},

	{"INTEGER8", "INTEGER(8)"},
	{"INTEGER16", "INTEGER(16)"},
	{"INTEGER32", "INTEGER(32)"},
	{"INTEGER64", "INTEGER(64)"},
	{"INTEGER128", "INTEGER(128)"},
	{"INTEGER256", "INTEGER(256)"},

	{"UINT8", "UINT(8)"},
	{"UINT16", "UINT(16)"},
	{"UINT32", "UINT(32)"},
	{"UINT64", "UINT(64)"},
	{"UINT128", "UINT(128)"},
	{"UINT256", "UINT(256)"},
}

// TestPropertyBinaryWidthSuffixAndParenthesisedEquivalent asserts the property
// the bead is actually about: for the binary integer branches whose grammar
// makes the parenthesised form a sibling of an explicit width token, the two
// spellings resolve to the same PropertyType. A future edit that breaks the
// equivalence fails on this test, not on an incidental constant.
//
// FLOAT is deliberately excluded: FLOAT(p) does NOT fold onto FLOATp (see
// TestPropertyBinaryWidthNonFolds and the typeSpellings doc comment). Adding
// FLOAT pairs here would assert an equivalence the code intentionally does
// not honour.
func TestPropertyBinaryWidthSuffixAndParenthesisedEquivalent(t *testing.T) {
	for _, pair := range widthSpellingEquivalences {
		t.Run(pair.a+"_vs_"+pair.b, func(t *testing.T) {
			suf, err := parseFirstProperty(t, pair.a)
			require.NoError(t, err)
			par, err := parseFirstProperty(t, pair.b)
			require.NoError(t, err)
			require.Equal(t, suf.Type, par.Type, "%s and %s must resolve to the same PropertyType", pair.a, pair.b)
		})
	}
}

// TestPropertyNullability covers the nullable-by-default rule: a property is
// nullable unless its value type carries NOT NULL.
func TestPropertyNullability(t *testing.T) {
	nullable, err := parseFirstProperty(t, "INT")
	require.NoError(t, err)
	require.True(t, nullable.Nullable)

	notNull, err := parseFirstProperty(t, "INT NOT NULL")
	require.NoError(t, err)
	require.False(t, notNull.Nullable)
	require.Equal(t, graph.TypeInt, notNull.Type, "NOT NULL must not corrupt the type")
}

// verboseSignednessEquivalences pairs each explicitly-signed verbose spelling
// with its bare counterpart, read prefixed-first: {"SIGNED INTEGER", "INTEGER"}.
//
// The parenthesised block at the end is the composition of this table's property
// with widthSpellingEquivalences', and it is listed rather than left to follow
// from the two because it did not follow: the prefix rows all used a suffix width
// and the parenthetical rows all used no prefix, so `SIGNED INTEGER(8)` appeared
// in neither and resolved to TypeInt, the machine word, with its declared eight
// bits dropped. Two properties each pinned across a whole width range can still
// leave their conjunction unpinned, and that is where the defect sat.
var verboseSignednessEquivalences = []typeEquivalence{
	{"SIGNED INTEGER", "INTEGER"},
	{"SIGNED INTEGER8", "INTEGER8"},
	{"SIGNED INTEGER16", "INTEGER16"},
	{"SIGNED INTEGER32", "INTEGER32"},
	{"SIGNED INTEGER64", "INTEGER64"},
	{"SIGNED INTEGER128", "INTEGER128"},
	{"SIGNED INTEGER256", "INTEGER256"},
	{"SIGNED SMALL INTEGER", "SMALL INTEGER"},
	{"SIGNED BIG INTEGER", "BIG INTEGER"},

	{"UNSIGNED INTEGER", "UINT"},
	{"UNSIGNED INTEGER8", "UINT8"},
	{"UNSIGNED INTEGER16", "UINT16"},
	{"UNSIGNED INTEGER32", "UINT32"},
	{"UNSIGNED INTEGER64", "UINT64"},
	{"UNSIGNED INTEGER128", "UINT128"},
	{"UNSIGNED INTEGER256", "UINT256"},
	{"UNSIGNED SMALL INTEGER", "USMALLINT"},
	{"UNSIGNED BIG INTEGER", "UBIGINT"},

	{"SIGNED INTEGER(8)", "INTEGER(8)"},
	{"SIGNED INTEGER(16)", "INTEGER(16)"},
	{"SIGNED INTEGER(32)", "INTEGER(32)"},
	{"SIGNED INTEGER(64)", "INTEGER(64)"},
	{"SIGNED INTEGER(128)", "INTEGER(128)"},
	{"SIGNED INTEGER(256)", "INTEGER(256)"},

	{"UNSIGNED INTEGER(8)", "UINT(8)"},
	{"UNSIGNED INTEGER(16)", "UINT(16)"},
	{"UNSIGNED INTEGER(32)", "UINT(32)"},
	{"UNSIGNED INTEGER(64)", "UINT(64)"},
	{"UNSIGNED INTEGER(128)", "UINT(128)"},
	{"UNSIGNED INTEGER(256)", "UINT(256)"},
}

// TestPropertyVerboseIntegerSignedness covers the SIGNED / UNSIGNED prefix on
// verboseBinaryExactNumericType (GQL.g4:1803 for SIGNED?, :1816 for UNSIGNED).
// Both signednesses are equivalent to their bare verbose counterpart under the
// no-dialect principle: SIGNED INTEGER32 is INT32, UNSIGNED INTEGER8 is UINT8,
// and so on across the six width slots plus the bare / SMALL / BIG spellings.
// A mutation that drops any single alias row fails the corresponding named
// subtest here rather than a bulk count.
//
// SIGNED is optional in the grammar; UNSIGNED is mandatory. So bare `INTEGER`
// pins the SIGNED-absent case implicitly (it's already covered by
// TestPropertyTypeMapping), and the SIGNED-present rows here pin the explicit
// keyword equivalence.
func TestPropertyVerboseIntegerSignedness(t *testing.T) {
	for _, pair := range verboseSignednessEquivalences {
		t.Run(pair.a+"_equals_"+pair.b, func(t *testing.T) {
			pref, err := parseFirstProperty(t, pair.a)
			require.NoError(t, err)
			bare, err := parseFirstProperty(t, pair.b)
			require.NoError(t, err)
			require.Equal(t, bare.Type, pref.Type, "%s must resolve to the same PropertyType as %s", pair.a, pair.b)
		})
	}
}

// TestPropertyBareDurationRejectedAtParse pins the invariant this bead's
// duration-qualifier drop depends on: bare `DURATION` (without the mandatory
// `(YEAR TO MONTH)` / `(DAY TO SECOND)` qualifier) cannot reach the schema
// mapper because the grammar (GQL.g4:1893, `DURATION LEFT_PAREN
// temporalDurationQualifier RIGHT_PAREN`) requires the parenthetical. If a
// future grammar edit made the qualifier optional, `truncateParenthetical`
// would silently accept bare `DURATION` on the strength of the alias row
// typeSpellings["DURATION"], and this test would tell us — the invariant we
// rely on to justify a single TypeDuration constant would have shifted under
// us. Positive assertion: the parser's error listener produced its
// "syntax error at ..." message, not merely that some error occurred and not
// merely that the word "unsupported" is absent.
func TestPropertyBareDurationRejectedAtParse(t *testing.T) {
	src := `CREATE PROPERTY GRAPH TYPE T AS { (:A { p :: DURATION }) }`
	_, err := New().Parse(strings.NewReader(src))
	require.Error(t, err)
	require.True(t,
		strings.HasPrefix(err.Error(), "syntax error at "),
		"bare DURATION must be rejected by the parser's error listener (grammar requires the qualifier); got: %v", err,
	)
}

// TestPropertyUnsupportedType covers grammar-valid value types outside the
// families this model maps; they must surface ErrUnsupportedType (ADR 0002).
// Scalar time-of-day / byte-string / duration spellings that once lived here
// are now supported (gqlc-h9n.4) and appear in TestPropertyTypeMapping instead;
// the remaining shapes are constructed / reference / dynamic types the model
// does not carry.
func TestPropertyUnsupportedType(t *testing.T) {
	for _, spelling := range []string{
		"LIST<INT>",
		"ANY",
	} {
		t.Run(spelling, func(t *testing.T) {
			_, err := parseFirstProperty(t, spelling)
			require.ErrorIs(t, err, ErrUnsupportedType)
		})
	}
}

// typeSpellingsRowFor resolves a spelling to the typeSpellings key normaliseType
// would read for it, mirroring that function's two-step lookup so that a row
// reached only through truncateParenthetical still counts. This is the one place
// the completeness gate touches the map under test, and the split matters: the
// map decides only *which* row a pin covers, never *whether* the pin is right,
// because every typePin's want is written out by hand.
func typeSpellingsRowFor(spelling string) (string, bool) {
	full := canonicalSpelling(spelling)
	if _, ok := typeSpellings[full]; ok {
		return full, true
	}
	truncated := truncateParenthetical(full)
	if _, ok := typeSpellings[truncated]; ok {
		return truncated, true
	}
	return "", false
}

// TestTypeSpellingsEveryRowPinned is the completeness gate: every row of
// typeSpellings must be grounded in a hand-written end-to-end assertion. A row
// is grounded if some typePin resolves to it, or if an equivalence pair links it
// to a row that is — the relation is symmetric and closed to a fixpoint, so
// `UNSIGNED INTEGER32` inherits its grounding from `UINT32` inheriting it from
// `TestPropertyTypeMapping`.
//
// The grounding is computed from the pin and equivalence tables rather than read
// off typeSpellings, because reading the expected constants off the map would
// make the gate agree with any repointing of it — the evidentiary circularity
// gqlc-exl rules out for golden snapshots, in a smaller form. What this catches
// instead is the silent failure mode of the status quo: a row added, or
// repointed onto an existing row, with no assertion behind it. A corpus entry
// saying the fixture "resolves" cannot catch that, which is the decision
// recorded next to the manifest in corpus_test.go.
//
// DURATION is grounded without an exemption: bare DURATION is a syntax error
// (TestPropertyBareDurationRejectedAtParse), but DURATION(YEAR TO MONTH) is
// pinned and reaches the row down the same fallback the production code takes.
func TestTypeSpellingsEveryRowPinned(t *testing.T) {
	grounded := make(map[string]bool, len(typeSpellings))
	row := func(spelling string) string {
		got, ok := typeSpellingsRowFor(spelling)
		require.True(t, ok, "pinned spelling %q reaches no typeSpellings row, so it grounds nothing", spelling)
		return got
	}

	for _, table := range [][]typePin{typeMappingPins, widthFoldPins} {
		for _, pin := range table {
			grounded[row(pin.spelling)] = true
		}
	}

	var linked [][2]string
	for _, table := range [][]typeEquivalence{widthSpellingEquivalences, verboseSignednessEquivalences} {
		for _, pair := range table {
			linked = append(linked, [2]string{row(pair.a), row(pair.b)})
		}
	}
	for changed := true; changed; {
		changed = false
		for _, pair := range linked {
			if grounded[pair[0]] != grounded[pair[1]] {
				grounded[pair[0]], grounded[pair[1]] = true, true
				changed = true
			}
		}
	}

	ungrounded := make([]string, 0, len(typeSpellings))
	for key := range typeSpellings {
		if !grounded[key] {
			ungrounded = append(ungrounded, key)
		}
	}
	sort.Strings(ungrounded)
	require.Empty(t, ungrounded,
		"every typeSpellings row needs an end-to-end pin: add the spelling to typeMappingPins or widthFoldPins with the constant you expect, "+
			"or to an equivalence table if it is an alias of a row that already has one")
}
