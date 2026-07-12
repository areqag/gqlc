package cypher

import (
	"strings"

	"github.com/antlr4-go/antlr/v4"

	"github.com/areqag/gqlc/internal/grammar/cypher/gen"
	"github.com/areqag/gqlc/internal/query"
)

// The expression grammar is a tower of single-alternative precedence rules
// (oC_Expression -> oC_OrExpression -> ... -> oC_NonArithmeticOperatorExpression
// -> oC_Atom). A value with no operators threads straight down that tower with
// one child per level, so the shape helpers below collapse the tower and inspect
// the bottom rule. Anything with an operator at any level fails the "bare value"
// test and is rejected (a projection, parameter, etc.) per the spec.

// propertyRefFromAddSub reads var / var.prop from an operand already unwrapped to an
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

// nonArithmeticAtom collapses an expression's precedence tower and returns the
// bottom non-arithmetic operator expression only when it carries no node labels
// and no list operators — the gate every projection classifier shares. It is nil
// when an operator is present at any level (the expression is not a bare value)
// or a label/list operator is attached.
func nonArithmeticAtom(e gen.IOC_ExpressionContext) gen.IOC_NonArithmeticOperatorExpressionContext {
	nae := nonArithmetic(e)
	if nae == nil || nae.OC_NodeLabels() != nil || len(nae.AllOC_ListOperatorExpression()) > 0 {
		return nil
	}
	return nae
}

// isScalarLiteral reports whether a literal is a scalar (number, string, boolean
// or NULL) rather than a list or map literal. A scalar literal projects as a
// LiteralProjection; a list/map literal is Stage 6 material.
func isScalarLiteral(lit gen.IOC_LiteralContext) bool {
	return lit != nil && lit.OC_ListLiteral() == nil && lit.OC_MapLiteral() == nil
}

// literalType returns the Stage-6 result type of a scalar literal: a boolean
// literal is TypeBool, an integer TypeInt, a float TypeFloat, a string
// TypeString, and NULL TypeNull. The caller only passes literals isScalarLiteral
// has approved, so a list/map literal cannot reach here.
func literalType(lit gen.IOC_LiteralContext) query.Type {
	switch {
	case lit.OC_BooleanLiteral() != nil:
		return query.TypeBool{}
	case lit.NULL() != nil:
		return query.TypeNull{}
	case lit.StringLiteral() != nil:
		return query.TypeString{}
	case lit.OC_NumberLiteral() != nil:
		nl := lit.OC_NumberLiteral()
		if nl.OC_DoubleLiteral() != nil {
			return query.TypeFloat{}
		}
		return query.TypeInt{}
	default:
		return query.TypeUnknown{}
	}
}

// functionName reads the bare function name of an invocation, lowercased for the
// case-insensitive aggregate match (the TCK writes cOuNt, aVg). A namespaced name
// (foo.bar) has no bare name, so ok is false — it is not in the aggregate set and
// classifies as a FuncProjection regardless.
func functionName(fi gen.IOC_FunctionInvocationContext) (string, bool) {
	name := fi.OC_FunctionName()
	if name == nil || (name.OC_Namespace() != nil && len(name.OC_Namespace().AllOC_SymbolicName()) > 0) {
		return "", false
	}
	sn := name.OC_SymbolicName()
	if sn == nil {
		return "", false
	}
	return strings.ToLower(sn.GetText()), true
}

// fullFunctionName reads the fully-qualified function name of an invocation
// as dot-joined lowercase segments — "date" for a bare call, "duration.between"
// for a namespaced one. Empty string when the invocation has no readable name.
// Stage 7 uses this to match the seven-name temporal constructor set (spec §1,
// §4): every constructor except the namespaced duration.* set has an empty
// namespace, so functionName covers the six bare ones; the namespaced set
// needs the full-name form. Both classify against the same lookup, so drift
// between call sites is impossible.
func fullFunctionName(fi gen.IOC_FunctionInvocationContext) string {
	name := fi.OC_FunctionName()
	if name == nil {
		return ""
	}
	sn := name.OC_SymbolicName()
	if sn == nil {
		return ""
	}
	bare := strings.ToLower(sn.GetText())
	ns := name.OC_Namespace()
	if ns == nil {
		return bare
	}
	parts := ns.AllOC_SymbolicName()
	if len(parts) == 0 {
		return bare
	}
	var b strings.Builder
	for _, p := range parts {
		b.WriteString(strings.ToLower(p.GetText()))
		b.WriteByte('.')
	}
	b.WriteString(bare)
	return b.String()
}

// temporalConstructorType maps a fully-qualified openCypher temporal
// constructor name to its Stage-7 result type (spec §1). Every match is a
// standard openCypher constructor whose return type is settled at the grammar
// level — schema-independent, exactly the posture Stage-3 aggregates take.
// An unmatched name returns TypeUnknown, so callers can chain the lookup with
// the existing "function identity is below the boundary" default without
// branching.
func temporalConstructorType(name string) (query.Type, bool) {
	switch name {
	case "date":
		return query.TypeDate{}, true
	case "time":
		return query.TypeTime{}, true
	case "localtime":
		return query.TypeLocalTime{}, true
	case "datetime":
		return query.TypeDateTime{}, true
	case "localdatetime":
		return query.TypeLocalDateTime{}, true
	case "duration", "duration.between":
		return query.TypeDuration{}, true
	}
	return nil, false
}

// builtinScalarFuncType maps a bare (non-namespaced) openCypher scalar
// builtin's lowercased name and argument-type shape to its result type
// (ADR 0010 D3). Same posture as temporalConstructorType: schema-
// independent, resolver upgrades TypeUnknown into a concrete Go type when
// this table commits. An unmatched name-or-shape returns nil, false so
// callers preserve the "function identity below the boundary" default
// without branching. A variable-length edge binding (TypeList(TypeEdge))
// does not match — elementId over a list is undefined by openCypher / the
// neo4j driver.
func builtinScalarFuncType(name string, argTypes []query.Type) (query.Type, bool) {
	switch name {
	case "elementid":
		if len(argTypes) == 1 && isNodeOrEdge(argTypes[0]) {
			return query.TypeString{}, true
		}
	case "id":
		if len(argTypes) == 1 && isNodeOrEdge(argTypes[0]) {
			return query.TypeInt{}, true
		}
	}
	return nil, false
}

func isNodeOrEdge(t query.Type) bool {
	switch t.(type) {
	case query.TypeNode, query.TypeEdge:
		return true
	}
	return false
}

// aggregateFunc maps a lowercased function name to its AggregateFunc, reporting
// whether the name is an aggregate at all (§4: the openCypher aggregating
// functions are a closed set). stdev/stdevp and percentilecont/percentiledisc
// collapse to one enum each (the model carries the cardinality kind, not the
// variant).
func aggregateFunc(name string) (query.AggregateFunc, bool) {
	switch name {
	case "count":
		return query.AggCount, true
	case "sum":
		return query.AggSum, true
	case "collect":
		return query.AggCollect, true
	case "min":
		return query.AggMin, true
	case "max":
		return query.AggMax, true
	case "avg":
		return query.AggAvg, true
	case "stdev", "stdevp":
		return query.AggStdev, true
	case "percentilecont", "percentiledisc":
		return query.AggPercentile, true
	default:
		return 0, false
	}
}

// aggregateResultType returns the Stage-10 result type of an aggregate call
// given its AggregateFunc and its operand's Stage-6 result type (spec §1.2).
// A nil operand means the count(*) degenerate case; the caller passes nil for
// count(*) and the Stage-6 typed argument otherwise. The rules follow the
// per-aggregate table: count is TypeInt unconditionally; collect(T) is
// list<T> (never bare TypeUnknown — the aggregate always yields a list, so
// list<unknown> is the honest posture when the element is unknown); sum/min/
// max commit to a concrete type when the operand's type commits; avg /
// stDev / percentile* stay TypeUnknown (engine-dependent — a wrong concrete
// would be strictly worse than the honest TypeUnknown the resolver upgrades).
//
// Same table applied at two sites (classifyFunction for a RETURN/WITH
// projection, typeAtom for an aggregate inside a rich expression) so a
// call cannot type differently across positions.
func aggregateResultType(fn query.AggregateFunc, operand query.Type) query.Type {
	switch fn {
	case query.AggCount:
		return query.TypeInt{}
	case query.AggCollect:
		if operand == nil {
			return query.NewTypeList(query.TypeUnknown{})
		}
		return query.NewTypeList(operand)
	case query.AggSum:
		switch operand.(type) {
		case query.TypeInt:
			return query.TypeInt{}
		case query.TypeFloat:
			return query.TypeFloat{}
		case query.TypeDuration:
			return query.TypeDuration{}
		default:
			return query.TypeUnknown{}
		}
	case query.AggMin, query.AggMax:
		switch operand.(type) {
		case query.TypeInt, query.TypeFloat, query.TypeString, query.TypeBool,
			query.TypeDate, query.TypeTime, query.TypeLocalTime,
			query.TypeDateTime, query.TypeLocalDateTime, query.TypeDuration:
			return operand
		default:
			return query.TypeUnknown{}
		}
	case query.AggAvg:
		// avg(duration) is the only spec-committed numeric case; every other
		// operand (int/float, mixed, property) is engine-dependent (int vs
		// float rounding). Honest posture: TypeUnknown for numerics.
		if _, ok := operand.(query.TypeDuration); ok {
			return query.TypeDuration{}
		}
		return query.TypeUnknown{}
	case query.AggStdev, query.AggPercentile:
		return query.TypeUnknown{}
	default:
		return query.TypeUnknown{}
	}
}

// functionArgRefs mines the bindings a function/aggregate call references: each
// argument must be either a bare var/var.prop (yielding a Ref) or a scalar
// literal (yielding no Ref). ok is false if any argument is something else
// (arithmetic, nested call, list/map literal, parameter, CASE, comprehension,
// '*') — the "no expression tree, no nested aggregates" discipline (spec §4/§9).
func functionArgRefs(fi gen.IOC_FunctionInvocationContext) ([]query.Ref, bool) {
	var refs []query.Ref
	for _, arg := range fi.AllOC_Expression() {
		nae := nonArithmeticAtom(arg)
		if nae == nil {
			return nil, false
		}
		if ref, ok := refFromNonArithmetic(nae); ok {
			refs = append(refs, ref)
			continue
		}
		atom := nae.OC_Atom()
		if atom != nil && len(nae.AllOC_PropertyLookup()) == 0 &&
			atom.OC_Literal() != nil && isScalarLiteral(atom.OC_Literal()) {
			continue
		}
		return nil, false
	}
	return refs, true
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
	// The sign tokens are anonymous grammar literals with no named getter, so a
	// leading '+'/'-' is detected structurally: a signless operand is the rule's
	// only child.
	if unary.GetChildCount() != 1 {
		return nil
	}
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

// findNodesOfType returns every subtree of concrete ANTLR context type T
// under tree, matching depth-first pre-order and stopping descent at each
// match (matches do not nest inside themselves for the contexts this is
// used on — oC_Skip, oC_Limit). Callers that need the parent scope of
// each match rely on that stop-at-match; see EnterOC_ExistentialSubquery.
func findNodesOfType[T antlr.Tree](tree antlr.Tree) []T {
	var out []T
	var walk func(antlr.Tree)
	walk = func(t antlr.Tree) {
		if t == nil {
			return
		}
		if node, ok := t.(T); ok {
			out = append(out, node)
			return
		}
		for i := 0; i < t.GetChildCount(); i++ {
			walk(t.GetChild(i))
		}
	}
	walk(tree)
	return out
}

// isPatternPredicateAtom reports whether the expression is a bare
// pattern-predicate atom in projection position — the shape Pattern1
// [22]/[23] rejects. The precedence tower is collapsed via nonArithmetic;
// a pattern predicate at any operator-attached level (arithmetic,
// comparison, list operator) is not "bare" and falls through to the
// classifiers, matching the narrow charter of gqlc-3r0. Stage 11.
func isPatternPredicateAtom(e gen.IOC_ExpressionContext) bool {
	nae := nonArithmetic(e)
	if nae == nil {
		return false
	}
	a := nae.OC_Atom()
	return a != nil && a.OC_PatternPredicate() != nil
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

// propertyExpressionRef reads a Ref{Variable, Property} from a single-level
// propertyExpression on the LHS of a SET or REMOVE item (Stage 12 spec §4.3,
// §4.4). It accepts a bare-variable atom or a parenthesised bare-variable
// atom (`(n).name` — semantically identical to `n.name`; the openCypher TCK
// exercises both shapes). ok is false when the propertyExpression carries
// zero or more-than-one lookup, when the atom (after unwrapping parens)
// is not a bare variable, or when any list operator suffix is attached
// beyond the parenthesised path. A nested LHS (n.a.b) yields ok=false; the
// caller rejects with ErrNestedPropertyTarget (Stage 12 amend §1.5 / §1.6) —
// accept-and-truncate would claim the wrong concrete target field, which
// repository codegen consumes as the property to write.
func propertyExpressionRef(pe gen.IOC_PropertyExpressionContext) (query.Ref, bool) {
	if pe == nil {
		return query.Ref{}, false
	}
	lookups := pe.AllOC_PropertyLookup()
	if len(lookups) != 1 {
		return query.Ref{}, false
	}
	variable, ok := bareVariableFromAtom(pe.OC_Atom())
	if !ok {
		return query.Ref{}, false
	}
	return query.Ref{Variable: variable, Property: lookups[0].OC_PropertyKeyName().GetText()}, true
}

// bareVariableFromAtom unwraps an atom to its underlying variable name, if the
// atom is a bare variable — either directly (n) or through a parenthesised
// expression that itself collapses to a bare variable ((n), ((n))). Returns
// ("", false) for any other atom shape (a literal, a function invocation, a
// list operator, a parenthesised expression whose body is not a bare
// variable). Used by propertyExpressionRef to accept the parenthesised-atom
// SET/REMOVE LHS shape the openCypher TCK exercises.
func bareVariableFromAtom(a gen.IOC_AtomContext) (string, bool) {
	if a == nil {
		return "", false
	}
	if v := a.OC_Variable(); v != nil {
		return v.GetText(), true
	}
	pe := a.OC_ParenthesizedExpression()
	if pe == nil {
		return "", false
	}
	nae := nonArithmetic(pe.OC_Expression())
	if nae == nil || nae.OC_NodeLabels() != nil ||
		len(nae.AllOC_ListOperatorExpression()) > 0 ||
		len(nae.AllOC_PropertyLookup()) > 0 {
		return "", false
	}
	return bareVariableFromAtom(nae.OC_Atom())
}

// setItemOp reads the SET-item's `=` vs `+=` alternative by inspecting the
// direct-child terminal tokens (T__2 is `=`, T__3 is `+=`). The grammar
// guarantees one of the two is present when the item's shape is variable +
// expression (SetItem alternatives 2 and 3); the default (SetOpReplace) is
// defensive.
func setItemOp(item gen.IOC_SetItemContext) query.SetOp {
	for i := 0; i < item.GetChildCount(); i++ {
		tn, ok := item.GetChild(i).(antlr.TerminalNode)
		if !ok {
			continue
		}
		switch tn.GetSymbol().GetTokenType() {
		case gen.CypherParserT__2: // '='
			return query.SetOpReplace
		case gen.CypherParserT__3: // '+='
			return query.SetOpMerge
		}
	}
	return query.SetOpReplace
}
