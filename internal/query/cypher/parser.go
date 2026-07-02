package cypher

import (
	"io"

	"github.com/antlr4-go/antlr/v4"

	"github.com/areqag/gqlc/internal/grammar/cypher/gen"
	"github.com/areqag/gqlc/internal/query"
)

type parser struct{}

// New returns a query.Parser that lowers a single read query from openCypher
// into query.Query, per the Stage-0 spec. It mirrors internal/schema/gql.
func New() query.Parser {
	return parser{}
}

// Parse wires the ANTLR lexer, parser and a single syntax-error sink (mirroring
// gql), walks the tree to collect the read core, and lowers it into query.Query
// via build(). A syntax error, an unsupported construct, or an inconsistency
// (unbound variable / kind conflict) surfaces as a non-nil error with a zero
// Query. The executed query stays the original text (ADR 0005); this model is the
// type interface only.
func (parser) Parse(r io.Reader) (query.Query, error) {
	lex := gen.NewCypherLexer(antlr.NewIoStream(r))
	ts := antlr.NewCommonTokenStream(lex, antlr.TokenDefaultChannel)
	cp := gen.NewCypherParser(ts)

	// The listener is the single error sink: it captures lexer/parser syntax
	// errors and the collection errors raised during the walk, all on l.err. walk
	// surfaces the first of them.
	l := newListener(ts)
	lex.RemoveErrorListeners()
	lex.AddErrorListener(l)
	cp.RemoveErrorListeners()
	cp.AddErrorListener(l)

	tree := cp.OC_Cypher()
	if err := l.walk(tree); err != nil {
		return query.Query{}, err
	}

	return l.build()
}
