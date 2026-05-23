package gql

import (
	"fmt"
	"io"

	"github.com/antlr4-go/antlr/v4"
	"github.com/antranig-yeretzian/gqlc/internal/grammar/gql/gen"
	"github.com/antranig-yeretzian/gqlc/internal/schema"
)

type parser struct {
	v visitor
}

var _ schema.Parser = parser{}

func New() parser {
	return parser{v: visitor{}}
}

func (p parser) Parse(r io.Reader) (schema.Schema, error) {
	input := antlr.NewIoStream(r)

	// TODO: add custom error listener here
	lex := gen.NewGQLLexer(input)

	ts := antlr.NewCommonTokenStream(lex, antlr.TokenDefaultChannel)
	ts.Fill() // NOTE: forces a full lex of the source to find errors

	// TODO: add custom error listener here
	gp := gen.NewGQLParser(ts)
	tree := gp.GqlProgram()

	var visitorErr error
	func() {
		// NOTE: ANTLR visit panics when the source is invalid
		defer func() {
			if rec := recover(); rec != nil {
				if e, ok := rec.(error); ok {
					visitorErr = e
					return
				}
				visitorErr = fmt.Errorf("%v", rec)
			}
		}()
		p.v.Visit(tree)
	}()

	return schema.Schema{}, visitorErr
}
