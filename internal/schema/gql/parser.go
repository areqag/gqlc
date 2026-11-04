package gql

import (
	"fmt"
	"io"

	"github.com/antlr4-go/antlr/v4"

	"github.com/areqag/gqlc/internal/grammar/gql/gen"
	"github.com/areqag/gqlc/internal/grammar/lexfold"
	"github.com/areqag/gqlc/internal/grammar/nesting"
	"github.com/areqag/gqlc/internal/schema"
)

type parser struct{}

// New returns the ANTLR listener-driven GQL schema parser.
func New() schema.Parser {
	return parser{}
}

// Parse reads one schema file with no catalogue behind it, so a COPY OF source
// is lowered and then refused: ErrCopyOfSource says the reference names a graph
// type this call cannot reach, which is literally true of an io.Reader. Load is
// the entry point that has somewhere to look (ADR 0034 §3.6).
func (parser) Parse(r io.Reader) (schema.Schema, error) {
	raw, err := parse(r)
	if err != nil {
		return schema.Schema{}, err
	}
	if raw.copyRef != nil {
		return schema.Schema{}, ErrCopyOfSource
	}
	return raw.resolve()
}

// parse runs the walk and returns the unresolved schema, so that Parse and the
// Loader share one reading of a file and differ only in what they do with a
// reference.
func parse(r io.Reader) (rawSchema, error) {
	in := lexfold.NewStream(r, lexfold.GQL)
	if t := nesting.TooDeep(gen.NewGQLLexer(in)); t != nil {
		return rawSchema{}, fmt.Errorf("%w: bracket at %d:%d opens past depth %d", ErrNestingTooDeep, t.GetLine(), t.GetColumn(), nesting.MaxDepth)
	}
	if t := prefixRunTooLong(gen.NewGQLLexer(in)); t != nil {
		return rawSchema{}, fmt.Errorf("%w: operator at %d:%d extends a run past %d", ErrPrefixRunTooLong, t.GetLine(), t.GetColumn(), maxPrefixRun)
	}
	lex := gen.NewGQLLexer(in)
	ts := antlr.NewCommonTokenStream(lex, antlr.TokenDefaultChannel)
	gp := gen.NewGQLParser(ts)

	// The listener is the single error sink: it captures lexer/parser syntax
	// errors (SyntaxError) and the collection errors raised during the walk, all
	// on l.err. walk then surfaces the first of them — including "no graph type"
	// via ExitGqlProgram — and resolution runs only on a clean walk.
	l := newListener(ts)
	lex.RemoveErrorListeners()
	lex.AddErrorListener(l)
	gp.RemoveErrorListeners()
	gp.AddErrorListener(l)

	tree := gp.GqlProgram()
	if err := l.walk(tree); err != nil {
		return rawSchema{}, err
	}

	return l.raw, nil
}
