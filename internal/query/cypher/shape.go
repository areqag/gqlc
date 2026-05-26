package cypher

import (
	"github.com/antlr4-go/antlr/v4"

	"github.com/antranig-yeretzian/gqlc/internal/grammar/cypher/gen"
	"github.com/antranig-yeretzian/gqlc/internal/query"
)

// The expression grammar is a tower of single-alternative precedence rules
// (oC_Expression -> oC_OrExpression -> ... -> oC_NonArithmeticOperatorExpression
// -> oC_Atom). A value with no operators threads straight down that tower with
// one child per level, so the shape helpers below collapse the tower and inspect
// the bottom rule. Anything with an operator at any level fails the "bare value"
// test and is rejected (a projection, parameter, etc.) per the spec.

// propertyRef returns the Ref for an expression that is a bare variable or a
// single-level property lookup (var / var.prop), the only return-item shapes
// Stage 0 supports (E2). ok is false for anything richer.
func propertyRef(e gen.IOC_ExpressionContext) (query.Ref, bool) {
	nae := nonArithmetic(e)
	return refFromNonArithmetic(nae)
}

// propertyRefFromAddSub is propertyRef for an operand already unwrapped to an
// add-or-subtract expression (the operand level of a comparison).
func propertyRefFromAddSub(a gen.IOC_AddOrSubtractExpressionContext) (query.Ref, bool) {
	return refFromNonArithmetic(nonArithmeticFromAddSub(a))
}

// refFromNonArithmetic reads var / var.prop from a non-arithmetic operator
// expression: its atom must be a variable, with at most one property lookup and
// no list operators or node labels. A second lookup (a.b.c) is multi-level and
// unrepresentable.
func refFromNonArithmetic(nae gen.IOC_NonArithmeticOperatorExpressionContext) (query.Ref, bool) {
	if nae == nil || nae.OC_NodeLabels() != nil || len(nae.AllOC_ListOperatorExpression()) > 0 {
		return query.Ref{}, false
	}
	atom := nae.OC_Atom()
	if atom == nil || atom.OC_Variable() == nil {
		return query.Ref{}, false
	}
	variable := atom.OC_Variable().GetText()

	lookups := nae.AllOC_PropertyLookup()
	switch len(lookups) {
	case 0:
		return query.Ref{Variable: variable}, true
	case 1:
		return query.Ref{Variable: variable, Property: lookups[0].OC_PropertyKeyName().GetText()}, true
	default:
		return query.Ref{}, false
	}
}

// parameterFromAddSub returns the parameter name and node for an operand that is
// a bare $param (no operators, lookups or labels).
func parameterFromAddSub(a gen.IOC_AddOrSubtractExpressionContext) (string, antlr.Tree, bool) {
	return parameterFromNonArithmetic(nonArithmeticFromAddSub(a))
}

// parameterFromExpr returns the parameter name and node for an expression that is
// a bare $param.
func parameterFromExpr(e gen.IOC_ExpressionContext) (string, antlr.Tree, bool) {
	return parameterFromNonArithmetic(nonArithmetic(e))
}

func parameterFromNonArithmetic(nae gen.IOC_NonArithmeticOperatorExpressionContext) (string, antlr.Tree, bool) {
	if nae == nil || nae.OC_NodeLabels() != nil ||
		len(nae.AllOC_ListOperatorExpression()) > 0 || len(nae.AllOC_PropertyLookup()) > 0 {
		return "", nil, false
	}
	atom := nae.OC_Atom()
	if atom == nil || atom.OC_Parameter() == nil {
		return "", nil, false
	}
	p := atom.OC_Parameter()
	return parameterName(p), p, true
}

// nonArithmetic collapses the precedence tower below an expression to its single
// non-arithmetic operator expression, or nil if any level branches (i.e. an
// operator is present), in which case the expression is not a bare value.
func nonArithmetic(e gen.IOC_ExpressionContext) gen.IOC_NonArithmeticOperatorExpressionContext {
	if e == nil {
		return nil
	}
	or := e.OC_OrExpression()
	if or == nil || len(or.AllOC_XorExpression()) != 1 {
		return nil
	}
	xor := or.OC_XorExpression(0)
	if len(xor.AllOC_AndExpression()) != 1 {
		return nil
	}
	and := xor.OC_AndExpression(0)
	if len(and.AllOC_NotExpression()) != 1 {
		return nil
	}
	not := and.OC_NotExpression(0)
	if len(not.AllNOT()) > 0 {
		return nil
	}
	cmp := not.OC_ComparisonExpression()
	if len(cmp.AllOC_PartialComparisonExpression()) > 0 {
		return nil
	}
	return nonArithmeticFromStringListNull(cmp.OC_StringListNullPredicateExpression())
}

// nonArithmeticFromStringListNull collapses a string/list/null predicate base to
// its non-arithmetic operator expression, or nil if a predicate is attached.
func nonArithmeticFromStringListNull(s gen.IOC_StringListNullPredicateExpressionContext) gen.IOC_NonArithmeticOperatorExpressionContext {
	if s == nil ||
		len(s.AllOC_StringPredicateExpression()) > 0 ||
		len(s.AllOC_ListPredicateExpression()) > 0 ||
		len(s.AllOC_NullPredicateExpression()) > 0 {
		return nil
	}
	return nonArithmeticFromAddSub(s.OC_AddOrSubtractExpression())
}

// nonArithmeticFromAddSub collapses the arithmetic tower (add/sub, mul/div, power,
// unary) to its single non-arithmetic operator expression, or nil if any level
// has an operator (arithmetic is not a bindable shape).
func nonArithmeticFromAddSub(a gen.IOC_AddOrSubtractExpressionContext) gen.IOC_NonArithmeticOperatorExpressionContext {
	if a == nil || len(a.AllOC_MultiplyDivideModuloExpression()) != 1 {
		return nil
	}
	md := a.OC_MultiplyDivideModuloExpression(0)
	if len(md.AllOC_PowerOfExpression()) != 1 {
		return nil
	}
	pw := md.OC_PowerOfExpression(0)
	if len(pw.AllOC_UnaryAddOrSubtractExpression()) != 1 {
		return nil
	}
	unary := pw.OC_UnaryAddOrSubtractExpression(0)
	return unary.OC_NonArithmeticOperatorExpression()
}

// stringListNullBase is the operand base of a comparison side — its
// add-or-subtract expression, used to pair operands when there is no string/list/
// null predicate attached.
func stringListNullBase(s gen.IOC_StringListNullPredicateExpressionContext) gen.IOC_AddOrSubtractExpressionContext {
	if s == nil {
		return nil
	}
	return s.OC_AddOrSubtractExpression()
}

// parameterName reads the name of a parameter ($name or $0): the text after '$'.
func parameterName(p antlr.Tree) string {
	pc, ok := p.(gen.IOC_ParameterContext)
	if !ok {
		return ""
	}
	if sn := pc.OC_SymbolicName(); sn != nil {
		return sn.GetText()
	}
	if di := pc.DecimalInteger(); di != nil {
		return di.GetText()
	}
	return ""
}

// findParameters returns every oC_Parameter node under tree, so the parameter
// approval sweep can reject any occurrence not mined into a property Use.
func findParameters(tree antlr.Tree) []antlr.Tree {
	var out []antlr.Tree
	var walk func(antlr.Tree)
	walk = func(t antlr.Tree) {
		if t == nil {
			return
		}
		if _, ok := t.(gen.IOC_ParameterContext); ok {
			out = append(out, t)
			return // a parameter has no nested parameter
		}
		for i := 0; i < t.GetChildCount(); i++ {
			walk(t.GetChild(i))
		}
	}
	walk(tree)
	return out
}

// originalText returns the verbatim source slice spanning a rule context, the
// exact text the author wrote (so a column name like "p.name" is "p.name", not
// the token-joined "pname"). It reads the token interval from the stream,
// mirroring schema/gql's property type-text extraction.
func originalText(ts *antlr.CommonTokenStream, ctx antlr.ParserRuleContext) string {
	if ctx == nil {
		return ""
	}
	return ts.GetTextFromInterval(ctx.GetSourceInterval())
}
