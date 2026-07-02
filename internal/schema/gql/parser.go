package gql

import (
	"io"

	"github.com/antlr4-go/antlr/v4"

	"github.com/areqag/gqlc/internal/grammar/gql/gen"
	"github.com/areqag/gqlc/internal/schema"
)

type parser struct{}

// New returns the ANTLR listener-driven GQL schema parser.
func New() schema.Parser {
	return parser{}
}

func (parser) Parse(r io.Reader) (schema.Schema, error) {
	lex := gen.NewGQLLexer(antlr.NewIoStream(r))
	ts := antlr.NewCommonTokenStream(lex, antlr.TokenDefaultChannel)
	gp := gen.NewGQLParser(ts)

	// The listener is the single error sink: it captures lexer/parser syntax
	// errors (SyntaxError) and the collection errors raised during the walk, all
	// on l.err. walk then surfaces the first of them — including "no graph type"
	// via ExitGqlProgram — and resolution runs only on a clean walk.
	l := &listener{ts: ts}
	lex.RemoveErrorListeners()
	lex.AddErrorListener(l)
	gp.RemoveErrorListeners()
	gp.AddErrorListener(l)

	tree := gp.GqlProgram()
	if err := l.walk(tree); err != nil {
		return schema.Schema{}, err
	}

	return l.raw.resolve()
}
