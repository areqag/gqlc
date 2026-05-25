package gql

import (
	"testing"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stretchr/testify/require"

	"github.com/antranig-yeretzian/gqlc/internal/grammar/gql/gen"
	"github.com/antranig-yeretzian/gqlc/internal/schema"
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
		want     schema.PropertyType
	}{
		{"STRING", schema.TypeString},
		{"CHAR", schema.TypeString},
		{"VARCHAR", schema.TypeString},
		{"BOOL", schema.TypeBool},
		{"BOOLEAN", schema.TypeBool},
		{"DATE", schema.TypeDate},

		{"TIMESTAMP", schema.TypeTimestamp},
		{"ZONED DATETIME", schema.TypeTimestamp},
		{"LOCAL DATETIME", schema.TypeTimestamp},
		{"TIMESTAMP WITH TIME ZONE", schema.TypeTimestamp},
		{"TIMESTAMP WITHOUT TIME ZONE", schema.TypeTimestamp},

		{"INT", schema.TypeInt},
		{"INTEGER", schema.TypeInt},
		{"SMALLINT", schema.TypeInt16},
		{"SMALL INTEGER", schema.TypeInt16},
		{"BIGINT", schema.TypeInt64},
		{"BIG INTEGER", schema.TypeInt64},
		{"INT8", schema.TypeInt8},
		{"INTEGER8", schema.TypeInt8},
		{"INT256", schema.TypeInt256},

		{"UINT", schema.TypeUint},
		{"USMALLINT", schema.TypeUint16},
		{"UBIGINT", schema.TypeUint64},
		{"UINT8", schema.TypeUint8},
		{"UINT256", schema.TypeUint256},

		{"FLOAT", schema.TypeFloat},
		{"REAL", schema.TypeFloat32},
		{"DOUBLE", schema.TypeFloat64},
		{"DOUBLE PRECISION", schema.TypeFloat64},
		{"FLOAT16", schema.TypeFloat16},
		{"FLOAT256", schema.TypeFloat256},

		{"DECIMAL", schema.TypeDecimal},
		{"DEC", schema.TypeDecimal},
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
// being stripped before normalization (ADR 0002).
func TestPropertyLengthQualifiersDropped(t *testing.T) {
	cases := []struct {
		spelling string
		want     schema.PropertyType
	}{
		{"VARCHAR(255)", schema.TypeString},
		{"CHAR(8)", schema.TypeString},
		{"STRING(100)", schema.TypeString},
		{"DECIMAL(10, 2)", schema.TypeDecimal},
		{"FLOAT(10)", schema.TypeFloat},
	}

	for _, tt := range cases {
		t.Run(tt.spelling, func(t *testing.T) {
			got, err := parseFirstProperty(t, tt.spelling)
			require.NoError(t, err)
			require.Equal(t, tt.want, got.Type)
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
	require.Equal(t, schema.TypeInt, notNull.Type, "NOT NULL must not corrupt the type")
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
