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
