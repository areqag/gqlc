package cypher

import (
	"github.com/areqag/gqlc/internal/grammar/cypher/gen"
	"github.com/areqag/gqlc/internal/query"
)

// The rich-expression typer walks the scalar-expression subtree beneath a
// RETURN or WITH item and produces its Stage-6 result type plus the []Ref every
// var/var.prop atom in the subtree touches. The walk is best-effort: expressions
// whose type the parser cannot compute schema-free (property lookups, function
// calls, NULL propagation, parameters, mixed-type list elements) collapse to
// TypeUnknown, which the resolver upgrades from the schema.
//
// The typer never fails: every valid openCypher expression at Stage-6 scope
// yields either a concrete type or TypeUnknown. Failure to type is not an
// error; it is the honest posture on the type-interface boundary (ADR 0005).

// typeExpression is the entry point: it types an oC_Expression and returns the
// result type plus the refs it touched. Nil expressions type as TypeUnknown.
func (l *listener) typeExpression(e gen.IOC_ExpressionContext) (query.Type, []query.Ref) {
	if e == nil {
		return query.TypeUnknown{}, nil
	}
	var refs []query.Ref
	t := l.typeOr(e.OC_OrExpression(), &refs)
	return t, refs
}

// typeOr types an OR expression: if the rule has multiple alternates joined by
// OR, the result is TypeBool (a boolean operator); a single alternate falls
// through to the next level.
func (l *listener) typeOr(o gen.IOC_OrExpressionContext, refs *[]query.Ref) query.Type {
	if o == nil {
		return query.TypeUnknown{}
	}
	xs := o.AllOC_XorExpression()
	if len(xs) == 1 {
		return l.typeXor(xs[0], refs)
	}
	for _, x := range xs {
		_ = l.typeXor(x, refs)
	}
	return query.TypeBool{}
}

// typeXor types an XOR expression: XOR-joined alternates are boolean.
func (l *listener) typeXor(x gen.IOC_XorExpressionContext, refs *[]query.Ref) query.Type {
	if x == nil {
		return query.TypeUnknown{}
	}
	as := x.AllOC_AndExpression()
	if len(as) == 1 {
		return l.typeAnd(as[0], refs)
	}
	for _, a := range as {
		_ = l.typeAnd(a, refs)
	}
	return query.TypeBool{}
}

// typeAnd types an AND expression: AND-joined alternates are boolean.
func (l *listener) typeAnd(a gen.IOC_AndExpressionContext, refs *[]query.Ref) query.Type {
	if a == nil {
		return query.TypeUnknown{}
	}
	ns := a.AllOC_NotExpression()
	if len(ns) == 1 {
		return l.typeNot(ns[0], refs)
	}
	for _, n := range ns {
		_ = l.typeNot(n, refs)
	}
	return query.TypeBool{}
}

// typeNot types a NOT expression: NOT flips the operand to a boolean result.
func (l *listener) typeNot(n gen.IOC_NotExpressionContext, refs *[]query.Ref) query.Type {
	if n == nil {
		return query.TypeUnknown{}
	}
	inner := l.typeComparison(n.OC_ComparisonExpression(), refs)
	if len(n.AllNOT()) > 0 {
		return query.TypeBool{}
	}
	return inner
}

// typeComparison types a comparison expression: any comparison chain
// (a < b, a = b, a < b < c) yields TypeBool; a plain expression falls through.
func (l *listener) typeComparison(c gen.IOC_ComparisonExpressionContext, refs *[]query.Ref) query.Type {
	if c == nil {
		return query.TypeUnknown{}
	}
	partials := c.AllOC_PartialComparisonExpression()
	base := l.typeStringListNull(c.OC_StringListNullPredicateExpression(), refs)
	if len(partials) == 0 {
		return base
	}
	for _, p := range partials {
		_ = l.typeStringListNull(p.OC_StringListNullPredicateExpression(), refs)
	}
	return query.TypeBool{}
}

// typeStringListNull types a StringListNullPredicateExpression: the presence of
// any string/list/null predicate turns the result to TypeBool (each is a
// predicate); otherwise it falls through to the add/sub expression.
func (l *listener) typeStringListNull(s gen.IOC_StringListNullPredicateExpressionContext, refs *[]query.Ref) query.Type {
	if s == nil {
		return query.TypeUnknown{}
	}
	base := l.typeAddSub(s.OC_AddOrSubtractExpression(), refs)
	if len(s.AllOC_StringPredicateExpression()) > 0 {
		for _, sp := range s.AllOC_StringPredicateExpression() {
			_ = l.typeAddSub(sp.OC_AddOrSubtractExpression(), refs)
		}
		return query.TypeBool{}
	}
	if len(s.AllOC_ListPredicateExpression()) > 0 {
		// A ListPredicateExpression is "IN <list-expr>" — result is boolean.
		for _, lp := range s.AllOC_ListPredicateExpression() {
			_ = l.typeAddSub(lp.OC_AddOrSubtractExpression(), refs)
		}
		return query.TypeBool{}
	}
	if len(s.AllOC_NullPredicateExpression()) > 0 {
		// A NullPredicateExpression is "IS NULL" / "IS NOT NULL" — result is
		// boolean. There is no operand to descend into (the base is the operand).
		return query.TypeBool{}
	}
	return base
}

// typeAddSub types an addition/subtraction chain: numeric arithmetic on
// integers yields TypeInt, mixing with float yields TypeFloat, string
// concatenation yields TypeString. Any operand of TypeUnknown or TypeNull, or
// any mixed-kind pairing the parser does not recognise, yields TypeUnknown.
func (l *listener) typeAddSub(a gen.IOC_AddOrSubtractExpressionContext, refs *[]query.Ref) query.Type {
	if a == nil {
		return query.TypeUnknown{}
	}
	ms := a.AllOC_MultiplyDivideModuloExpression()
	if len(ms) == 0 {
		return query.TypeUnknown{}
	}
	if len(ms) == 1 {
		return l.typeMulDiv(ms[0], refs)
	}
	// Chained + / -. The parser walks left-to-right, joining the running result
	// type with each next operand under promoteArith (integer + integer = int,
	// integer + float = float, string + string = string, else unknown).
	acc := l.typeMulDiv(ms[0], refs)
	for i := 1; i < len(ms); i++ {
		acc = promoteArith(acc, l.typeMulDiv(ms[i], refs))
	}
	return acc
}

// typeMulDiv types a multiplication/division/modulo chain: numeric arithmetic
// on integers yields TypeInt; mixing with float yields TypeFloat; anything
// else yields TypeUnknown.
func (l *listener) typeMulDiv(m gen.IOC_MultiplyDivideModuloExpressionContext, refs *[]query.Ref) query.Type {
	if m == nil {
		return query.TypeUnknown{}
	}
	ps := m.AllOC_PowerOfExpression()
	if len(ps) == 0 {
		return query.TypeUnknown{}
	}
	if len(ps) == 1 {
		return l.typePower(ps[0], refs)
	}
	acc := l.typePower(ps[0], refs)
	for i := 1; i < len(ps); i++ {
		acc = promoteArith(acc, l.typePower(ps[i], refs))
	}
	return acc
}

// typePower types a power-of chain (^): openCypher pow is a floating-point
// operator, so any use of ^ yields TypeFloat regardless of operand types (the
// resolver may narrow if it can prove otherwise). A single operand falls
// through to the unary level.
func (l *listener) typePower(p gen.IOC_PowerOfExpressionContext, refs *[]query.Ref) query.Type {
	if p == nil {
		return query.TypeUnknown{}
	}
	us := p.AllOC_UnaryAddOrSubtractExpression()
	if len(us) == 0 {
		return query.TypeUnknown{}
	}
	if len(us) == 1 {
		return l.typeUnary(us[0], refs)
	}
	for _, u := range us {
		_ = l.typeUnary(u, refs)
	}
	return query.TypeFloat{}
}

// typeUnary types a UnaryAddOrSubtractExpression: leading sign(s) do not change
// the operand's arithmetic type (a signed int is still int, a signed float
// still float). Falls through to the non-arithmetic operator expression.
func (l *listener) typeUnary(u gen.IOC_UnaryAddOrSubtractExpressionContext, refs *[]query.Ref) query.Type {
	if u == nil {
		return query.TypeUnknown{}
	}
	return l.typeNonArithmetic(u.OC_NonArithmeticOperatorExpression(), refs)
}

// typeNonArithmetic types a NonArithmeticOperatorExpression: the atom's type,
// modified by any suffix (property lookup, list index/slice, node labels).
// Property lookups collapse to TypeUnknown; list indexing on a typed list
// yields its element type; list slicing preserves the list type; node-label
// predicates yield TypeBool. A single property lookup on a bare-variable atom
// upgrades the last-mined Ref from {Variable: v} to {Variable: v, Property: k}
// so build()'s referential-integrity sweep tracks the property, matching what
// classifyProjection would emit for a var/var.prop bare case.
func (l *listener) typeNonArithmetic(n gen.IOC_NonArithmeticOperatorExpressionContext, refs *[]query.Ref) query.Type {
	if n == nil {
		return query.TypeUnknown{}
	}
	lookups := n.AllOC_PropertyLookup()
	preAtomRefLen := len(*refs)
	t := l.typeAtom(n.OC_Atom(), refs)
	listOps := n.AllOC_ListOperatorExpression()
	labels := n.OC_NodeLabels()
	// A node-label predicate "n:Foo" is boolean regardless of preceding suffixes.
	if labels != nil {
		return query.TypeBool{}
	}
	// Property lookups collapse to TypeUnknown (schema-owned typing). A single
	// lookup on a bare-variable atom (Property was empty) upgrades the ref to
	// carry the property so referential integrity holds for var.prop.
	if len(lookups) > 0 {
		if len(lookups) == 1 && n.OC_Atom() != nil && n.OC_Atom().OC_Variable() != nil &&
			len(*refs) == preAtomRefLen+1 && (*refs)[preAtomRefLen].Property == "" {
			(*refs)[preAtomRefLen] = query.Ref{
				Variable: (*refs)[preAtomRefLen].Variable,
				Property: lookups[0].OC_PropertyKeyName().GetText(),
			}
		}
		t = query.TypeUnknown{}
	}
	for _, op := range listOps {
		t = applyListOp(t, op)
	}
	return t
}

// typeAtom types an atom and mines any ref it contains: a variable atom yields
// TypeNode/TypeEdge (or TypeUnknown for an imported alias whose kind is a
// scalar), a literal yields its scalar-or-list-or-map kind, a parameter is
// approved and yields TypeUnknown, a function invocation yields TypeUnknown,
// a parenthesised expression falls through, a CASE expression yields the
// common type of its alternatives (TypeUnknown when they diverge), and a
// count(*) yields TypeUnknown (aggregate return types are below the boundary).
func (l *listener) typeAtom(a gen.IOC_AtomContext, refs *[]query.Ref) query.Type {
	if a == nil {
		return query.TypeUnknown{}
	}
	switch {
	case a.OC_Variable() != nil:
		name := a.OC_Variable().GetText()
		l.curPart.refs = append(l.curPart.refs, varRef{name: name})
		*refs = append(*refs, query.Ref{Variable: name})
		return l.refType(query.Ref{Variable: name})
	case a.OC_Literal() != nil:
		return literalOrCollectionType(a.OC_Literal(), l, refs)
	case a.OC_Parameter() != nil:
		l.approved[a.OC_Parameter()] = true
		return query.TypeUnknown{}
	case a.COUNT() != nil:
		return query.TypeUnknown{}
	case a.OC_FunctionInvocation() != nil:
		l.mineFunctionArgs(a.OC_FunctionInvocation(), refs)
		return query.TypeUnknown{}
	case a.OC_ParenthesizedExpression() != nil:
		t, inner := l.typeExpression(a.OC_ParenthesizedExpression().OC_Expression())
		*refs = append(*refs, inner...)
		return t
	case a.OC_CaseExpression() != nil:
		return l.typeCase(a.OC_CaseExpression(), refs)
	case a.OC_ListComprehension() != nil, a.OC_PatternComprehension() != nil,
		a.OC_PatternPredicate() != nil, a.OC_ExistentialSubquery() != nil:
		return query.TypeUnknown{}
	default:
		return query.TypeUnknown{}
	}
}

// typeCase types a CASE expression: the result is the common type of the
// THEN and ELSE arms (all arms of the same type ⇒ that type; else TypeUnknown).
// The WHEN expressions are boolean predicates but do not contribute to the
// result type; their refs are still mined.
func (l *listener) typeCase(c gen.IOC_CaseExpressionContext, refs *[]query.Ref) query.Type {
	if c == nil {
		return query.TypeUnknown{}
	}
	// A CASE has an optional test expression, alternatives, and an optional
	// ELSE. The grammar's CASE production groups these under a single rule; we
	// walk every Expression child recursively to mine refs and read the arm
	// types via a scan for THEN/ELSE positions.
	var armTypes []query.Type
	for _, e := range c.AllOC_Expression() {
		t, inner := l.typeExpression(e)
		*refs = append(*refs, inner...)
		armTypes = append(armTypes, t)
	}
	for _, alt := range c.AllOC_CaseAlternative() {
		for _, e := range alt.AllOC_Expression() {
			t, inner := l.typeExpression(e)
			*refs = append(*refs, inner...)
			armTypes = append(armTypes, t)
		}
	}
	// Result type: the common type across all arm expressions the walk saw. The
	// grammar's Expression collection under a CASE conflates the WHEN and THEN
	// arms, so a mixed set is honestly TypeUnknown at this stage — the resolver
	// walks the CASE structure and computes a tighter type post-freeze if
	// needed.
	if len(armTypes) == 0 {
		return query.TypeUnknown{}
	}
	return commonType(armTypes)
}

// mineFunctionArgs mines refs from a function invocation's arguments; the
// call itself types as TypeUnknown (function identity is below the boundary,
// ADR 0005). Arguments that are var/var.prop still contribute refs so the
// resolver can trace them.
func (l *listener) mineFunctionArgs(fi gen.IOC_FunctionInvocationContext, refs *[]query.Ref) {
	for _, arg := range fi.AllOC_Expression() {
		_, inner := l.typeExpression(arg)
		*refs = append(*refs, inner...)
	}
}

// literalOrCollectionType types a literal or a list/map literal. Scalar
// literals delegate to literalType. A list literal walks its element
// expressions to compute the element type (TypeUnknown when mixed or empty).
// A map literal is TypeMap (heterogeneous by design, spec §3).
func literalOrCollectionType(lit gen.IOC_LiteralContext, l *listener, refs *[]query.Ref) query.Type {
	if lit == nil {
		return query.TypeUnknown{}
	}
	if lit.OC_ListLiteral() != nil {
		return listLiteralType(lit.OC_ListLiteral(), l, refs)
	}
	if lit.OC_MapLiteral() != nil {
		// Mine refs from the map's value expressions so referential integrity
		// still holds for a rich map like {k: n.x}.
		for _, e := range lit.OC_MapLiteral().AllOC_Expression() {
			_, inner := l.typeExpression(e)
			*refs = append(*refs, inner...)
		}
		return query.TypeMap{}
	}
	return literalType(lit)
}

// listLiteralType types a list literal by unifying its element types: uniform
// element types collapse to LIST<T>; mixed or empty yields LIST<UNKNOWN>.
func listLiteralType(ll gen.IOC_ListLiteralContext, l *listener, refs *[]query.Ref) query.Type {
	if ll == nil {
		return query.NewTypeList(query.TypeUnknown{})
	}
	var elemTypes []query.Type
	for _, e := range ll.AllOC_Expression() {
		t, inner := l.typeExpression(e)
		*refs = append(*refs, inner...)
		elemTypes = append(elemTypes, t)
	}
	return query.NewTypeList(commonType(elemTypes))
}

// applyListOp updates a running type after a list operator suffix: [i] takes an
// element type off a typed list; [i..j] preserves the list. Non-list suffixes
// or a non-list base collapse to TypeUnknown.
func applyListOp(base query.Type, op gen.IOC_ListOperatorExpressionContext) query.Type {
	if op == nil {
		return base
	}
	list, ok := base.(query.TypeList)
	if !ok {
		return query.TypeUnknown{}
	}
	// Two expressions ⇒ slice [lo..hi]; one ⇒ index [i]. Slicing on a list is
	// still a list of the same element type; indexing yields the element type.
	if len(op.AllOC_Expression()) == 1 {
		return list.Element()
	}
	return base
}

// commonType computes the common type across a slice of types: if every entry
// is the same type (via String equality — the closed sum has no other axis),
// that type; else TypeUnknown. A single-entry slice returns its lone type.
func commonType(ts []query.Type) query.Type {
	if len(ts) == 0 {
		return query.TypeUnknown{}
	}
	first := ts[0]
	for _, t := range ts[1:] {
		if t.String() != first.String() {
			return query.TypeUnknown{}
		}
	}
	return first
}

// promoteArith joins two arithmetic operand types under the openCypher promotion
// rules the parser can commit to schema-free: int+int=int, int+float=float,
// float+float=float, string+string=string (concatenation via +), anything
// touching TypeUnknown or TypeNull collapses to TypeUnknown.
func promoteArith(a, b query.Type) query.Type {
	if _, ok := a.(query.TypeUnknown); ok {
		return query.TypeUnknown{}
	}
	if _, ok := b.(query.TypeUnknown); ok {
		return query.TypeUnknown{}
	}
	if _, ok := a.(query.TypeNull); ok {
		return query.TypeUnknown{}
	}
	if _, ok := b.(query.TypeNull); ok {
		return query.TypeUnknown{}
	}
	if isNumeric(a) && isNumeric(b) {
		if _, ok := a.(query.TypeFloat); ok {
			return query.TypeFloat{}
		}
		if _, ok := b.(query.TypeFloat); ok {
			return query.TypeFloat{}
		}
		return query.TypeInt{}
	}
	if _, ok := a.(query.TypeString); ok {
		if _, ok := b.(query.TypeString); ok {
			return query.TypeString{}
		}
	}
	return query.TypeUnknown{}
}

func isNumeric(t query.Type) bool {
	switch t.(type) {
	case query.TypeInt, query.TypeFloat:
		return true
	default:
		return false
	}
}

// classifyRichExpression is the Stage-6 residual classifier: when the bare-atom
// classifier declines an expression (any shape not var/var.prop, scalar
// literal, function call, or aggregate) we type the whole sub-tree and return
// an ExprProjection. The typer never fails; a truly opaque expression yields
// TypeUnknown. Refs are mined for every var/var.prop atom the walk touches so
// build()'s referential-integrity sweep covers them.
func (l *listener) classifyRichExpression(e gen.IOC_ExpressionContext) query.Projection {
	t, refs := l.typeExpression(e)
	return query.NewExprProjection(refs, t)
}
