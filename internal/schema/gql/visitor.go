package gql

import (
	"fmt"

	"github.com/antlr4-go/antlr/v4"
	"github.com/antranig-yeretzian/gqlc/internal/grammar/gql/gen"
)

type visitor struct {
	*gen.BaseGQLVisitor

	// err holds the first unsupported construct encountered during the walk.
	// Once set, the remaining traversal is short-circuited.
	err error
}

func (v *visitor) Visit(tree antlr.ParseTree) any {
	if v.err != nil {
		return nil
	}
	switch t := tree.(type) {
	case *gen.NodeTypeKeyLabelSetContext, *gen.EdgeTypeKeyLabelSetContext:
		v.err = ErrLabelImplication
		return nil
	case *gen.EdgeTypePatternUndirectedContext:
		v.err = ErrUndirectedEdge
		return nil
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
		if v.err != nil {
			return nil
		}
		pt, ok := child.(antlr.ParseTree)
		if !ok {
			continue
		}
		v.Visit(pt)
	}
	return nil
}
