package gql

import (
	"github.com/antlr4-go/antlr/v4"
	gen "github.com/antranig-yeretzian/gqlc/internal/grammar/gql/gen/gql"
)

type visitorSchema struct {
	*gen.BaseGQLVisitor

	// fileSchema is the path to the file in which the gql schema lives
	fileSchema string
}

func NewSchemaVisitor(fileSchema string) *visitorSchema {
	return &visitorSchema{fileSchema: fileSchema}
}

func (v *visitorSchema) Parse() {
	fs, err := antlr.NewFileStream(v.fileSchema)
	if err != nil {
		return
	}

	ts := antlr.NewCommonTokenStream(gen.NewGQLLexer(fs), antlr.TokenDefaultChannel)
	p := gen.NewGQLParser(ts)

	tree := p.GqlProgram()

	tree.Accept(v)
}
