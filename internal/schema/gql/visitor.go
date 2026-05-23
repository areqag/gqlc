package gql

import (
	"fmt"

	"github.com/antlr4-go/antlr/v4"
	"github.com/antranig-yeretzian/gqlc/internal/grammar/gql/gen"
)

type visitor struct {
	*gen.BaseGQLVisitor
}

func (v *visitor) Visit(tree antlr.ParseTree) any {
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

func (v *visitor) VisitPropertyType(ctx *gen.PropertyTypeContext) any {
	fmt.Printf("ctx.PropertyName().GetText(): %v\n", ctx.PropertyName().GetText())
	fmt.Printf("ctx.PropertyValueType().GetText(): %v\n", ctx.PropertyValueType().GetText())
	return nil
}

func (v *visitor) VisitChildren(tree antlr.RuleNode) any {
	for _, child := range tree.GetChildren() {
		pt, ok := child.(antlr.ParseTree)
		if !ok {
			continue
		}
		v.Visit(pt)
	}
	return nil
}
