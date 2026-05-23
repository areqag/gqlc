package gql

import (
	"fmt"

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

func (v *visitorSchema) Visit(tree antlr.ParseTree) any {
	switch t := tree.(type) {
	case *gen.PropertyTypeContext:
		return v.VisitPropertyType(t)
	case antlr.TerminalNode:
		return nil
	case antlr.RuleNode:
		return v.VisitChildren(t)
	default:
		return nil
	}
}

func (v *visitorSchema) VisitPropertyType(ctx *gen.PropertyTypeContext) any {
	fmt.Printf("ctx.PropertyName().GetText(): %v\n", ctx.PropertyName().GetText())
	fmt.Printf("ctx.PropertyValueType().GetText(): %v\n", ctx.PropertyValueType().GetText())
	return nil
}

func (v *visitorSchema) VisitChildren(tree antlr.RuleNode) any {
	for _, child := range tree.GetChildren() {
		pt, ok := child.(antlr.ParseTree)
		if !ok {
			continue
		}
		v.Visit(pt)
	}
	return nil
}

func (v *visitorSchema) Parse() (err error) {
	fs, err := antlr.NewFileStream(v.fileSchema)
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

	var visitorErr error
	func() {
		// NOTE: ANTLR visit panics when the file is invalid
		defer func() {
			if r := recover(); r != nil {
				if e, ok := r.(error); ok {
					visitorErr = e
					return
				}
				visitorErr = fmt.Errorf("%v", r)
			}
		}()
		v.Visit(tree)
	}()

	return visitorErr
}
