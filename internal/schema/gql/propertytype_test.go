package gql

import (
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

func TestPropertyTypeMapping(t *testing.T) {
	cases := []struct {
		spelling string
		want     graph.PropertyType
	}{
		{"STRING", graph.TypeString},
		{"CHAR", graph.TypeString},
		{"VARCHAR", graph.TypeString},
		{"BOOL", graph.TypeBool},
		{"BOOLEAN", graph.TypeBool},
		{"DATE", graph.TypeDate},

		{"TIMESTAMP", graph.TypeTimestamp},
		{"ZONED DATETIME", graph.TypeTimestamp},
		{"LOCAL DATETIME", graph.TypeTimestamp},
		{"TIMESTAMP WITH TIME ZONE", graph.TypeTimestamp},
		{"TIMESTAMP WITHOUT TIME ZONE", graph.TypeTimestamp},

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
		{"FLOAT256", graph.TypeFloat256},

		{"DECIMAL", graph.TypeDecimal},
		{"DEC", graph.TypeDecimal},
	}

	for _, tt := range cases {
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
	cases := []struct {
		spelling string
		want     graph.PropertyType
	}{
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

	for _, tt := range cases {
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
//     INT(10)). Lossy — the author said 7 bits, the model says machine int;
//     gqlc-h9n.25 tracks whether that should change.
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
	pairs := []struct{ suffix, parenthesised string }{
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

	for _, pair := range pairs {
		t.Run(pair.suffix+"_vs_"+pair.parenthesised, func(t *testing.T) {
			suf, err := parseFirstProperty(t, pair.suffix)
			require.NoError(t, err)
			par, err := parseFirstProperty(t, pair.parenthesised)
			require.NoError(t, err)
			require.Equal(t, suf.Type, par.Type, "%s and %s must resolve to the same PropertyType", pair.suffix, pair.parenthesised)
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

// TestPropertyUnsupportedType covers grammar-valid value types outside the
// families this model maps; they must surface ErrUnsupportedType (ADR 0002).
func TestPropertyUnsupportedType(t *testing.T) {
	for _, spelling := range []string{
		"TIME WITH TIME ZONE",
		"LIST<INT>",
		"ANY",
		"BYTES",
	} {
		t.Run(spelling, func(t *testing.T) {
			_, err := parseFirstProperty(t, spelling)
			require.ErrorIs(t, err, ErrUnsupportedType)
		})
	}
}
