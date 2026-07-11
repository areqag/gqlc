package cypher

import (
	"github.com/antlr4-go/antlr/v4"

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
// Parameter mining is separate: use typeExpressionMining when the caller must
// register ExprUse for every $param the walk saw.
func (l *listener) typeExpression(e gen.IOC_ExpressionContext) (query.Type, []query.Ref) {
	if e == nil {
		return query.TypeUnknown{}, nil
	}
	var refs []query.Ref
	t := l.typeOr(e.OC_OrExpression(), &refs)
	return t, refs
}

// typeExpressionMining walks the expression like typeExpression but also
// collects every oC_Parameter node the walk touched — the caller registers
// each as an ExprUse against the enclosing rich expression's result type once
// that type is known. Approving the parameter (l.approved[node] = true)
// happens in typeAtom regardless, so a param appearing inside a rich
// expression is never counted as "not approved" by
// requireAllParametersApproved.
func (l *listener) typeExpressionMining(e gen.IOC_ExpressionContext) (query.Type, []query.Ref, []antlr.Tree) {
	if e == nil {
		return query.TypeUnknown{}, nil, nil
	}
	saved := l.exprParams
	l.exprParams = nil
	var refs []query.Ref
	t := l.typeOr(e.OC_OrExpression(), &refs)
	params := l.exprParams
	l.exprParams = saved
	return t, refs, params
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

// typeAddSub types an addition/subtraction chain. The chain is folded
// left-to-right, dispatching per-operator so the type-interface honours the
// asymmetry of temporal subtraction: temporal - duration is a temporal, but
// duration - temporal is undefined in the spec §1 rule table and stays
// TypeUnknown (a concrete wrong-kind result would be strictly worse than
// TypeUnknown, which the resolver can upgrade). Numeric int/float promotion
// and string concatenation are operator-symmetric and route through the
// shared promoteBase, applied to both + and -.
//
// The ANTLR rule context does not expose typed PLUS/MINUS accessors, so we
// walk the terminal-token children in source order and pair each operator
// token with the next operand context.
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
	acc := l.typeMulDiv(ms[0], refs)
	next := 1
	for _, op := range addSubOperators(a) {
		if next >= len(ms) {
			break
		}
		rhs := l.typeMulDiv(ms[next], refs)
		next++
		switch op {
		case addOp:
			acc = promoteAdd(acc, rhs)
		case subOp:
			acc = promoteSub(acc, rhs)
		}
	}
	return acc
}

// typeMulDiv types a multiplication/division/modulo chain, dispatching
// per-operator so duration÷number stays a duration but number÷duration is
// left honestly TypeUnknown (spec §1 commits division only with the
// duration on the left; number/duration has no committed type). Numeric
// int/float promotion and duration×number (both directions — the operator
// is commutative) route through promoteMul.
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
	next := 1
	for _, op := range mulDivModOperators(m) {
		if next >= len(ps) {
			break
		}
		rhs := l.typePower(ps[next], refs)
		next++
		switch op {
		case mulOp:
			acc = promoteMul(acc, rhs)
		case divOp:
			acc = promoteDiv(acc, rhs)
		case modOp:
			acc = promoteMod(acc, rhs)
		}
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
// approved and yields TypeUnknown, a function invocation yields its
// Stage-7 temporal-constructor type or its Stage-10 aggregate-result type
// when the name matches (spec §1.3), else TypeUnknown, a parenthesised
// expression falls through, a CASE expression yields the common type of its
// alternatives (TypeUnknown when they diverge), and a count(*) star-atom
// yields TypeInt via the aggregate table (Stage 10).
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
		p := a.OC_Parameter()
		// A parameter already approved by an earlier miner (a WHERE var.prop=$p
		// pair caught by mineComparisons) keeps that PropertyUse and is not
		// re-recorded here. Every other occurrence is queued for an ExprUse
		// against the enclosing rich expression's result type (Stage 6 §4).
		if !l.approved[p] {
			l.approved[p] = true
			l.exprParams = append(l.exprParams, p)
		}
		return query.TypeUnknown{}
	case a.COUNT() != nil:
		// Stage 10: a count(*) star-atom inside a rich expression (e.g.
		// count(*) + 1) types as TypeInt via the aggregate table, matching
		// what classifyProjection returns for a bare count(*) at RETURN
		// position. The atom is grammatical-only — no refs to mine.
		return aggregateResultType(query.AggCount, nil)
	case a.OC_FunctionInvocation() != nil:
		fi := a.OC_FunctionInvocation()
		l.mineFunctionArgs(fi, refs)
		if t, ok := temporalConstructorType(fullFunctionName(fi)); ok {
			return t
		}
		// Stage 10: an aggregate inside a rich expression types via the same
		// per-aggregate table classifyFunction uses at RETURN/WITH position, so
		// the two positions cannot disagree on the same call's result type. A
		// bare count() atom is handled via a.COUNT() (the star-atom
		// alternative); every other aggregate is a normal function-invocation
		// atom whose name matches the closed set.
		if name, ok := functionName(fi); ok {
			if fn, ok := aggregateFunc(name); ok {
				var operand query.Type
				if args := fi.AllOC_Expression(); len(args) > 0 {
					operand, _ = l.typeExpression(args[0])
				}
				return aggregateResultType(fn, operand)
			}
			// gqlc-v5t: the same builtin scalar-function widening
			// classifyFunction uses at RETURN/WITH position, applied to the
			// rich-expression atom path — so `elementId(p) + '-suffix'` and
			// bare `elementId(p)` cannot type differently on the same call.
			if t, ok := builtinScalarFuncType(name, l.builtinArgTypes(fi)); ok {
				return t
			}
		}
		return query.TypeUnknown{}
	case a.OC_ParenthesizedExpression() != nil:
		t, inner := l.typeExpression(a.OC_ParenthesizedExpression().OC_Expression())
		*refs = append(*refs, inner...)
		return t
	case a.OC_CaseExpression() != nil:
		return l.typeCase(a.OC_CaseExpression(), refs)
	case a.OC_Quantifier() != nil:
		// Stage 11 §1.1: the four list quantifiers (ALL/ANY/NONE/SINGLE)
		// return a boolean. Refs from the source list flow to the caller's
		// slice (outer scope), refs from the filter body are discarded
		// (iteration-variable scoping), and parameters are mined on both
		// sides so a $param anywhere under the quantifier records an
		// ExprUse against the enclosing rich expression's result type.
		return l.typeQuantifier(a.OC_Quantifier(), refs)
	case a.OC_ExistentialSubquery() != nil:
		// Stage 11 §1.2: EXISTS { ... } returns a boolean. Parameters
		// inside the subquery are mined at EnterOC_ExistentialSubquery
		// (listener.go); inner clauses are suppressed by the
		// subqueryDepth counter and never enter the outer part's state,
		// so no refs walk the subquery body here.
		return query.TypeBool{}
	case a.OC_PatternPredicate() != nil:
		// Stage 11 §1.3: a pattern predicate at WHERE / rich-expression
		// predicate position is boolean. The atom's inner refs (n / m /
		// endpoints) are runtime-scope and do not enter the outer refs
		// slice — the model does not carry pattern-predicate structure
		// (ADR 0003). A pattern predicate at RETURN / WITH projection
		// position is rejected earlier via ErrPatternInProjection
		// (collectReturnItem), so this arm never runs in that context.
		return query.TypeBool{}
	case a.OC_ListComprehension() != nil, a.OC_PatternComprehension() != nil:
		// Comprehensions carry a list result whose element type depends
		// on the projection sub-tree; a wrong concrete would be strictly
		// worse than an honest TypeUnknown (the Stage-6 posture, ADR
		// 0005). Stage 11 does not tighten this.
		return query.TypeUnknown{}
	default:
		return query.TypeUnknown{}
	}
}

// typeQuantifier types a Stage-11 quantifier atom (ALL / ANY / NONE /
// SINGLE) as TypeBool while enforcing the iteration-variable scoping
// invariant (§1.1). Two sub-expressions matter:
//
//   - The source list (`x IN xs`): outer-scope refs, so its typer walks
//     into the caller's refs slice; parameters record an ExprUse against
//     the source list's Stage-6 type (a $xs source types as TypeUnknown
//     and the ExprUse honours that).
//   - The filter WHERE (`WHERE p(x)`): the iteration variable and any
//     x.prop it touches must not enter the outer refs slice, so refs the
//     typer accumulated on curPart.refs are rolled back after the walk
//     (the same idiom mineWhere / mineSortItemParameters use). Every
//     $param records an ExprUse{TypeBool, ExprInPredicate} — the filter
//     WHERE is boolean by definition.
//
// The caller's refs slice receives source-list refs only. The quantifier
// itself types as TypeBool unconditionally.
func (l *listener) typeQuantifier(q gen.IOC_QuantifierContext, refs *[]query.Ref) query.Type {
	filter := q.OC_FilterExpression()
	if filter == nil {
		return query.TypeBool{}
	}
	if idInColl := filter.OC_IdInColl(); idInColl != nil {
		if src := idInColl.OC_Expression(); src != nil {
			sourceType, _, params := l.typeExpressionMining(src)
			// Type the source once more against the caller's refs slice
			// so source-list refs (an outer var / var.prop the source
			// expression names) enter the outer part. typeExpressionMining
			// pushes refs onto a local slice and onto curPart.refs; the
			// second walk is what threads them into the caller's slice.
			_ = l.typeOr(src.OC_OrExpression(), refs)
			for _, p := range params {
				name := parameterName(p)
				if name == "" {
					continue
				}
				l.addParameterUse(name, p, query.NewExprUse(sourceType, query.ExprInPredicate))
			}
		}
	}
	if w := filter.OC_Where(); w != nil {
		savedOuter := l.curPart.refs
		_, _, params := l.typeExpressionMining(w.OC_Expression())
		l.curPart.refs = savedOuter // discard filter-body refs (iteration-variable scoping)
		for _, p := range params {
			name := parameterName(p)
			if name == "" {
				continue
			}
			l.addParameterUse(name, p, query.NewExprUse(query.TypeBool{}, query.ExprInPredicate))
		}
	}
	return query.TypeBool{}
}

// typeCase types a CASE expression: the result is the common type of the
// value-producing arms — the THEN of every alternative and the ELSE if
// present. WHEN predicates and the optional case-subject (`CASE n WHEN 1 …`)
// are typed for ref mining but their types do not contribute to the arm-type
// unification (a boolean WHEN paired with a string THEN otherwise collapses
// every CASE to TypeUnknown).
//
// The grammar puts the optional subject and the optional ELSE expression as
// top-level OC_Expression children of the CaseExpression, and each
// OC_CaseAlternative carries exactly two OC_Expression children in order:
// WHEN then THEN. When ELSE is present, the last top-level OC_Expression is
// the ELSE arm; any preceding top-level OC_Expression is the subject.
func (l *listener) typeCase(c gen.IOC_CaseExpressionContext, refs *[]query.Ref) query.Type {
	if c == nil {
		return query.TypeUnknown{}
	}
	topExprs := c.AllOC_Expression()
	var elseExpr gen.IOC_ExpressionContext
	if c.ELSE() != nil && len(topExprs) > 0 {
		elseExpr = topExprs[len(topExprs)-1]
		topExprs = topExprs[:len(topExprs)-1]
	}
	// Whatever remains is the optional case-subject: walk for refs only.
	for _, e := range topExprs {
		_, inner := l.typeExpression(e)
		*refs = append(*refs, inner...)
	}

	var armTypes []query.Type
	for _, alt := range c.AllOC_CaseAlternative() {
		altExprs := alt.AllOC_Expression()
		// A well-formed alternative has [WHEN, THEN]; if the grammar produced
		// fewer we walk what we have for refs and skip THEN typing for the
		// missing element.
		for i, e := range altExprs {
			t, inner := l.typeExpression(e)
			*refs = append(*refs, inner...)
			if i == 1 { // THEN position — the arm's produced value
				armTypes = append(armTypes, t)
			}
		}
	}
	if elseExpr != nil {
		t, inner := l.typeExpression(elseExpr)
		*refs = append(*refs, inner...)
		armTypes = append(armTypes, t)
	}
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

// addSubOp / mulDivModOp are the operator tags typeAddSub / typeMulDiv
// walk from the parse tree in source order so the per-step promotion is
// operator-aware (spec §1 rule table).
type addSubOp int

const (
	addOp addSubOp = iota
	subOp
)

type mulDivModOp int

const (
	mulOp mulDivModOp = iota
	divOp
	modOp
)

// addSubOperators walks the AddOrSubtract rule node's direct children in
// source order, extracting the sequence of + / - tokens. The ANTLR context
// mixes MultiplyDivideModuloExpression subtrees with terminal SP and +/-
// tokens; only the +/- tokens contribute to the returned slice, one per
// operand pair. The slice length equals len(operands)-1 for a well-formed
// chain.
func addSubOperators(a gen.IOC_AddOrSubtractExpressionContext) []addSubOp {
	var ops []addSubOp
	for i := 0; i < a.GetChildCount(); i++ {
		if op, ok := addSubTokenOp(a.GetChild(i)); ok {
			ops = append(ops, op)
		}
	}
	return ops
}

// mulDivModOperators walks the MultiplyDivideModulo rule node's direct
// children in source order, extracting the sequence of * / % tokens
// (identical shape to addSubOperators — the operators differ).
func mulDivModOperators(m gen.IOC_MultiplyDivideModuloExpressionContext) []mulDivModOp {
	var ops []mulDivModOp
	for i := 0; i < m.GetChildCount(); i++ {
		if op, ok := mulDivModTokenOp(m.GetChild(i)); ok {
			ops = append(ops, op)
		}
	}
	return ops
}

// addSubTokenOp reports whether tree is a + or - terminal token, and which.
// Non-terminal children (the MultiplyDivideModulo subtree) and other
// terminals (SP) return false.
func addSubTokenOp(tree antlr.Tree) (addSubOp, bool) {
	tn, ok := tree.(antlr.TerminalNode)
	if !ok {
		return 0, false
	}
	switch tn.GetSymbol().GetTokenType() {
	case gen.CypherParserT__17: // '+'
		return addOp, true
	case gen.CypherParserT__18: // '-'
		return subOp, true
	}
	return 0, false
}

// mulDivModTokenOp reports whether tree is a *, /, or % terminal token, and
// which. Non-terminal children (the PowerOf subtree) and other terminals
// (SP) return false.
func mulDivModTokenOp(tree antlr.Tree) (mulDivModOp, bool) {
	tn, ok := tree.(antlr.TerminalNode)
	if !ok {
		return 0, false
	}
	switch tn.GetSymbol().GetTokenType() {
	case gen.CypherParserT__4: // '*'
		return mulOp, true
	case gen.CypherParserT__19: // '/'
		return divOp, true
	case gen.CypherParserT__20: // '%'
		return modOp, true
	}
	return 0, false
}

// promoteAdd joins two operand types under the openCypher + promotion rules
// the parser can commit to schema-free: int+int=int, int+float=float,
// float+float=float, string+string=string (concatenation), and the Stage 7
// temporal rules — <temporal-point> + duration → <temporal-point>,
// duration + <temporal-point> → <temporal-point> (commutative), and
// duration + duration → duration (spec §1). Anything touching TypeUnknown
// or TypeNull collapses to TypeUnknown.
func promoteAdd(a, b query.Type) query.Type {
	if t, ok := promoteBase(a, b); ok {
		return t
	}
	if isTemporalPoint(a) {
		if _, ok := b.(query.TypeDuration); ok {
			return a
		}
	}
	if _, ok := a.(query.TypeDuration); ok {
		if isTemporalPoint(b) {
			return b
		}
		if _, ok := b.(query.TypeDuration); ok {
			return query.TypeDuration{}
		}
	}
	return query.TypeUnknown{}
}

// promoteSub joins two operand types under the openCypher - promotion
// rules. Spec §1 commits temporal subtraction one way only:
//
//	<temporal-point> - duration → <temporal-point>
//	duration         - duration → duration
//
// The reverse (duration - <temporal-point>) has no legal openCypher meaning
// and stays TypeUnknown — inventing a concrete result type here would be
// strictly worse than TypeUnknown, which the resolver can upgrade from the
// schema. Numeric int/float and string+string base rules stay through
// promoteBase (openCypher subtracts numbers but not strings; a
// string-string base pass would type as string, which is spurious for
// subtraction, so we do not call promoteBase for that shape — the numeric
// pass is what we want, delegated below).
func promoteSub(a, b query.Type) query.Type {
	// String concatenation is + only, not -. Route numerics and
	// null/unknown propagation through promoteBase, then reject a
	// string-string base result (which is a + rule) by falling through to
	// the temporal arm.
	if t, ok := promoteBase(a, b); ok {
		if _, isStr := t.(query.TypeString); isStr {
			// string - string is not defined in openCypher; leave the
			// result honest.
			return query.TypeUnknown{}
		}
		return t
	}
	if isTemporalPoint(a) {
		if _, ok := b.(query.TypeDuration); ok {
			return a
		}
	}
	if _, ok := a.(query.TypeDuration); ok {
		if _, ok := b.(query.TypeDuration); ok {
			return query.TypeDuration{}
		}
	}
	return query.TypeUnknown{}
}

// promoteMul joins two operand types under the openCypher * promotion
// rules. Numeric int/float promotion as promoteBase; Stage 7's duration ×
// number is commutative and yields duration (spec §1).
func promoteMul(a, b query.Type) query.Type {
	if t, ok := promoteBase(a, b); ok {
		if _, isStr := t.(query.TypeString); isStr {
			return query.TypeUnknown{}
		}
		return t
	}
	if _, ok := a.(query.TypeDuration); ok && isNumeric(b) {
		return query.TypeDuration{}
	}
	if _, ok := b.(query.TypeDuration); ok && isNumeric(a) {
		return query.TypeDuration{}
	}
	return query.TypeUnknown{}
}

// promoteDiv joins two operand types under the openCypher / promotion
// rules. Spec §1 commits duration ÷ number → duration only in that
// direction; number ÷ duration has no committed type and stays
// TypeUnknown. duration ÷ duration is dialect-specific (Neo4j returns a
// float; openCypher standard is silent) and also stays TypeUnknown.
func promoteDiv(a, b query.Type) query.Type {
	if t, ok := promoteBase(a, b); ok {
		if _, isStr := t.(query.TypeString); isStr {
			return query.TypeUnknown{}
		}
		return t
	}
	if _, ok := a.(query.TypeDuration); ok && isNumeric(b) {
		return query.TypeDuration{}
	}
	return query.TypeUnknown{}
}

// promoteMod joins two operand types under the openCypher % promotion
// rules. Modulo is numeric only; no temporal rule applies.
func promoteMod(a, b query.Type) query.Type {
	if t, ok := promoteBase(a, b); ok {
		if _, isStr := t.(query.TypeString); isStr {
			return query.TypeUnknown{}
		}
		return t
	}
	return query.TypeUnknown{}
}

// promoteBase handles the shared numeric / string / null-or-unknown rules
// both +/- and */÷ obey. The bool return is false when neither side is
// numeric-or-string-or-null-or-unknown, so the operator-specific fallthrough
// can attempt the temporal rules.
func promoteBase(a, b query.Type) (query.Type, bool) {
	if _, ok := a.(query.TypeUnknown); ok {
		return query.TypeUnknown{}, true
	}
	if _, ok := b.(query.TypeUnknown); ok {
		return query.TypeUnknown{}, true
	}
	if _, ok := a.(query.TypeNull); ok {
		return query.TypeUnknown{}, true
	}
	if _, ok := b.(query.TypeNull); ok {
		return query.TypeUnknown{}, true
	}
	if isNumeric(a) && isNumeric(b) {
		if _, ok := a.(query.TypeFloat); ok {
			return query.TypeFloat{}, true
		}
		if _, ok := b.(query.TypeFloat); ok {
			return query.TypeFloat{}, true
		}
		return query.TypeInt{}, true
	}
	if _, ok := a.(query.TypeString); ok {
		if _, ok := b.(query.TypeString); ok {
			return query.TypeString{}, true
		}
	}
	return nil, false
}

func isNumeric(t query.Type) bool {
	switch t.(type) {
	case query.TypeInt, query.TypeFloat:
		return true
	default:
		return false
	}
}

// isTemporalPoint reports whether t is a point-in-time temporal type — one of
// TypeDate, TypeTime, TypeLocalTime, TypeDateTime, TypeLocalDateTime.
// Duration is deliberately excluded — it is the additive companion, not a
// point-in-time — so promoteAddSub can special-case "temporal + duration"
// without treating duration as its own left operand.
func isTemporalPoint(t query.Type) bool {
	switch t.(type) {
	case query.TypeDate, query.TypeTime, query.TypeLocalTime, query.TypeDateTime, query.TypeLocalDateTime:
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
// build()'s referential-integrity sweep covers them. Any $param the walk
// encountered is registered as an ExprUse against the rich expression's
// result type and ExprInProjection — Stage 6 spec §4: no parameter is
// silently dropped.
func (l *listener) classifyRichExpression(e gen.IOC_ExpressionContext) query.Projection {
	t, refs, params := l.typeExpressionMining(e)
	for _, p := range params {
		name := parameterName(p)
		if name == "" {
			continue
		}
		l.addParameterUse(name, p, query.NewExprUse(t, query.ExprInProjection))
	}
	return query.NewExprProjectionWithAggregate(refs, t, subtreeContainsAggregate(e))
}

// subtreeContainsAggregate reports whether the expression subtree contains at
// least one aggregate function call (Shape B per ADR 0008 amendment
// 2026-07-06). Mirrors typeAtom's two aggregate arms (typing.go: the a.COUNT()
// star-atom above and the aggregateFunc(name) arm below), and mirrors the
// typing walk's sub-scope boundaries — the probe descends exactly where the
// typing walk descends. The five stops are: OC_ExistentialSubquery,
// OC_PatternPredicate, OC_ListComprehension, OC_PatternComprehension (all
// four are full stops — the typing walk returns without descending, so
// aggregates inside them aggregate over a sub-scope and cannot poison the
// outer projection's grouping-key eligibility), and the OC_Where of an
// OC_Quantifier's OC_FilterExpression (partial stop — descend into the
// x IN <src> source list, skip the WHERE filter body, mirroring
// typeQuantifier's savedOuter/restore idiom).
func subtreeContainsAggregate(e gen.IOC_ExpressionContext) bool {
	if e == nil {
		return false
	}
	return walkForAggregate(e)
}

// walkForAggregate is a manual pre-order recursion over the ANTLR parse tree,
// honouring the five §4.2.1 boundary stops in-place. Cheaper than a
// skipDepth counter on a ParseTreeWalker for a five-stop probe, and it
// keeps the boundary semantics visible on the fault line where the
// recursion decides whether to descend.
func walkForAggregate(node antlr.Tree) bool {
	switch n := node.(type) {
	case gen.IOC_ExistentialSubqueryContext, gen.IOC_PatternPredicateContext,
		gen.IOC_ListComprehensionContext, gen.IOC_PatternComprehensionContext:
		// Row 11 / 12 / 13 / 14 — full stop: the typing walk returns
		// without descending, so aggregates inside are opaque to the outer
		// Part.
		_ = n
		return false
	case gen.IOC_AtomContext:
		if n.COUNT() != nil {
			// Row 2 — count(*) star atom hit.
			return true
		}
	case gen.IOC_FunctionInvocationContext:
		if name, ok := functionName(n); ok {
			if _, isAgg := aggregateFunc(name); isAgg {
				// Row 5 — named-aggregate arm hit. Do not descend into
				// args; a nested aggregate inside an aggregate call
				// (count(count(*))) is caught by classifyFunction as an
				// AggregateProjection at the outer position, so this
				// probe only needs to answer whether the ExprProjection's
				// subtree has an aggregate anywhere — one hit is enough.
				return true
			}
		}
	case gen.IOC_QuantifierContext:
		// Row 10 partial: descend into the source list, skip the WHERE
		// filter body (which mirrors typeQuantifier's savedOuter/restore
		// at typing.go:449-452).
		filter := n.OC_FilterExpression()
		if filter == nil {
			return false
		}
		if idInColl := filter.OC_IdInColl(); idInColl != nil {
			if src := idInColl.OC_Expression(); src != nil {
				if walkForAggregate(src) {
					return true
				}
			}
		}
		return false
	}
	for i := 0; i < node.GetChildCount(); i++ {
		if walkForAggregate(node.GetChild(i)) {
			return true
		}
	}
	return false
}
