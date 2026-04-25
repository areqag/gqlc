package gql

import (
	"github.com/antlr4-go/antlr/v4"
	gen "github.com/antranig-yeretzian/gqlc/internal/grammar/gql/gen/gql"
)

type listenerSchema struct {
	*gen.BaseGQLListener

	// fileSchema is the path to the file in which the gql schema lives
	fileSchema string
}

func NewSchemaVisitor(fileSchema string) *listenerSchema {
	return &listenerSchema{fileSchema: fileSchema}
}

func (l *listenerSchema) Parse() (err error) {
	fs, err := antlr.NewFileStream(l.fileSchema)
	if err != nil {
		return err
	}

	// TODO: add custom error listener here
	lex := gen.NewGQLLexer(fs)

	ts := antlr.NewCommonTokenStream(lex, antlr.TokenDefaultChannel)
	ts.Fill() // NOTE: forces a full lex of the file to find errors

	// TODO: add custom error listener here
	p := gen.NewGQLParser(ts)
	tree := p.GqlProgram()

	antlr.ParseTreeWalkerDefault.Walk(l, tree)
	return nil
}
