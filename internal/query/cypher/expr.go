package cypher

import (
	"fmt"

	"github.com/antlr4-go/antlr/v4"

	"github.com/areqag/gqlc/internal/grammar/cypher/gen"
	"github.com/areqag/gqlc/internal/graph"
	"github.com/areqag/gqlc/internal/query"
)

// --- return items (Cluster E) ---

// collectProjection lowers a RETURN's projection body into result columns. The
// cosmetic parts (ORDER BY, SKIP/LIMIT) are accept-and-ignored, except that a
// parameter in any of them is rejected or bound to a clause slot: a dropped
// $param is a missing generated argument, i.e. a type-interface change (B1).
// DISTINCT (part-distinct-axis spec §4.1) is lifted onto the part's Distinct
// axis: the two grammar sites for DISTINCT (oC_ProjectionBody here vs.
// oC_FunctionInvocation on an aggregate) are read independently, so a
// projection-body DISTINCT sets the part axis without touching any aggregate,
// and an aggregate DISTINCT sets that aggregate's axis without touching the
// part. Each item is classified into a Projection variant: the bare-atom
// classifier (var/var.prop, scalar literal, function, aggregate, count(*))
// handles the shapes each carries as their dedicated variant; rich shapes
// (arithmetic, string/list/null predicates, list/map literals, list indexing/
// slicing, CASE, chained comparisons, parenthesised composites) fall through
// to the Stage-6 rich-expression classifier and produce an ExprProjection with
// a computed result type. The '*' alternative sets ReturnsAll.
func (l *listener) collectProjection(body gen.IOC_ProjectionBodyContext) {
	if body == nil {
		return
	}
	if body.DISTINCT() != nil {
		l.curPart.distinct = true
	}
	items := body.OC_ProjectionItems()
	if items == nil || len(items.AllOC_ProjectionItem()) == 0 {
		// The '*' alternative carries no projection items: a wildcard over the
		// part's in-scope bindings (spec §3), recorded as ReturnsAll on the part.
		l.curPart.returnsAll = true
		return
	}

	for _, item := range items.AllOC_ProjectionItem() {
		l.collectReturnItem(item)
		if l.err != nil {
			return
		}
	}

	// ORDER BY is accept-and-ignored at the clause-structure level (its sort
	// keys do not enter the model — sort-key structure is below the
	// type-interface boundary, ADR 0003), but a parameter under a sort key
	// would be silently dropped if we did not record its use — a type-interface
	// change. Stage 9 registers every ORDER BY parameter as an
	// ExprUse{TypeUnknown, ExprInProjection} via the Stage-6 typer so no
	// parameter is silently dropped. savedRefs is snapshotted around the walk
	// so the sort key's touched bindings do not enter the part's ref list.
	if o := body.OC_Order(); o != nil {
		for _, item := range o.AllOC_SortItem() {
			l.mineSortItemParameters(item.OC_Expression())
			if l.err != nil {
				return
			}
		}
	}
	if s := body.OC_Skip(); s != nil {
		l.mineClauseSlotParameter(s.OC_Expression(), query.ClauseSlotSkip)
	}
	if lim := body.OC_Limit(); lim != nil {
		l.mineClauseSlotParameter(lim.OC_Expression(), query.ClauseSlotLimit)
	}
}

// collectUnwind lowers an UNWIND clause into the current part as an
// UnwindBinding (Stage 9 spec §1.3, §4.1). The AS variable enters the
// part's scope; the source expression is typed via the Stage-6 typer,
// and its element type (list<T>.Element(), else TypeUnknown) becomes
// the binding's ElementType. Every parameter under the source expression
// records an ExprUse{sourceType, ExprInProjection} so no parameter is
// silently dropped. The source expression's touched refs enter the
// part's refs list so build()'s referential-integrity sweep covers them.
// A same-name entity binding in the same part is a kind conflict
// (byVar collision).
func (l *listener) collectUnwind(c gen.IOC_UnwindContext) {
	if c == nil {
		return
	}
	variable := ""
	if v := c.OC_Variable(); v != nil {
		variable = v.GetText()
	}
	// Grammatical guarantee: an UNWIND without `AS name` never lexes, so
	// variable is always non-empty here. Guard defensively.
	if variable == "" {
		return
	}
	// The three-way collision sweep (entity / path / unwind): the byVar map
	// only covers entity bindings, so path and unwind names have to be checked
	// against their own slices before this UnwindBinding is appended.
	// Spec §4.3 amend: same-name path or same-name earlier UNWIND in the same
	// part is a kind conflict, symmetric with the Stage 8 pathBindings-vs-byVar
	// check in buildPart.
	if _, clash := l.curPart.byVar[variable]; clash {
		l.fail(fmt.Errorf("%w: %q", ErrVariableKindConflict, variable))
		return
	}
	for _, pb := range l.curPart.pathBindings {
		if pb.Variable() == variable {
			l.fail(fmt.Errorf("%w: %q", ErrVariableKindConflict, variable))
			return
		}
	}
	for _, ub := range l.curPart.unwindBindings {
		if ub.Variable() == variable {
			l.fail(fmt.Errorf("%w: %q", ErrVariableKindConflict, variable))
			return
		}
	}
	sourceType, refs, params := l.typeExpressionMining(c.OC_Expression())
	for _, ref := range refs {
		l.curPart.refs = append(l.curPart.refs, varRef{name: ref.Variable})
	}
	for _, p := range params {
		name := parameterName(p)
		if name == "" {
			continue
		}
		l.addParameterUse(name, p, query.NewExprUse(sourceType, query.ExprInProjection))
	}
	elemType := unwindElementType(sourceType)
	ub, err := query.NewUnwindBinding(variable, elemType)
	if err != nil {
		l.fail(err)
		return
	}
	l.curPart.unwindBindings = append(l.curPart.unwindBindings, ub)
}

// unwindElementType extracts the element type from an UNWIND source
// expression's Stage-6 result type: a list<T> yields T; every other
// shape yields TypeUnknown. A wrong concrete element type would be
// strictly worse than an honest TypeUnknown the resolver can upgrade
// from the schema — the Stage-6 posture (spec §1.3, §8).
func unwindElementType(sourceType query.Type) query.Type {
	if list, ok := sourceType.(query.TypeList); ok {
		return list.Element()
	}
	return query.TypeUnknown{}
}

// mineSortItemParameters walks an ORDER BY sort key via the Stage-6
// rich-expression typer and records every parameter it touches as an
// ExprUse{TypeUnknown, ExprInProjection}. The enclosing type is
// TypeUnknown because the parameter's role is a sort-key contributor,
// not a computed value — recording the sort key's computed type would
// be incidental to the parameter's role; TypeUnknown is honest and
// the resolver upgrades from the schema (Stage 9 spec
// §4.2). savedRefs is snapshotted so the sort key's touched bindings
// stay out of the part's refs list.
func (l *listener) mineSortItemParameters(e gen.IOC_ExpressionContext) {
	if e == nil {
		return
	}
	savedRefs := l.curPart.refs
	_, _, params := l.typeExpressionMining(e)
	l.curPart.refs = savedRefs
	for _, p := range params {
		name := parameterName(p)
		if name == "" {
			continue
		}
		l.addParameterUse(name, p, query.NewExprUse(query.TypeUnknown{}, query.ExprInProjection))
	}
}

// collectReturnItem lowers one projection item by classifying its expression
// into a Projection variant: the bare-atom classifier handles var/var.prop,
// scalar literal, function call, aggregate, and count(*); anything else
// (arithmetic, comparisons, string predicates, IS NULL, list/map literals,
// CASE, list indexing/slicing, chained comparisons, parenthesised composites)
// falls through to the Stage-6 rich-expression classifier, which types the
// sub-tree and mines its refs. The column name is the explicit AS alias if
// present, else the verbatim source text of the expression (E1). Stage 11
// (§1.4, gqlc-3r0 fold): a pattern predicate at projection position is a
// bucket-1 parse-shape rejection — Pattern1 [22]/[23] cite
// SyntaxError:UnexpectedSyntax, which the parser owns. The
// isPatternPredicateAtom check fires before the bare-atom classifier so
// the rejection reaches ErrPatternInProjection rather than a silent
// ExprProjection{TypeBool}.
func (l *listener) collectReturnItem(item gen.IOC_ProjectionItemContext) {
	e := item.OC_Expression()
	if isPatternPredicateAtom(e) {
		l.fail(fmt.Errorf("%w: %s", ErrPatternInProjection, originalText(l.ts, e)))
		return
	}
	value, ok := l.classifyProjection(e)
	if !ok {
		value = l.classifyRichExpression(e)
	}

	name := originalText(l.ts, e)
	if alias := item.OC_Variable(); alias != nil {
		name = alias.GetText()
	}

	l.curPart.returns = append(l.curPart.returns, query.ReturnItem{Name: name, Value: value})
}

// classifyProjection maps a RETURN-item expression to a bare-atom Projection
// variant — the shapes that have a dedicated Projection: var/var.prop
// (RefProjection), scalar literal (LiteralProjection), function invocation
// (FuncProjection or AggregateProjection), and count(*) (the degenerate
// AggregateProjection). ok is false for every richer shape (arithmetic, list/
// map literals, IS NULL, list indexing, CASE, comprehensions, parenthesised
// expressions, chained comparisons, a $param, a label predicate); the caller
// falls through to classifyRichExpression, which produces an ExprProjection
// with the whole sub-tree's computed result type. Every accepted variant
// appends a varRef for the bindings it references so build()'s referential-
// integrity sweep covers them.
func (l *listener) classifyProjection(e gen.IOC_ExpressionContext) (query.Projection, bool) {
	nae := nonArithmeticAtom(e)
	if nae == nil {
		return nil, false
	}
	atom := nae.OC_Atom()
	if atom == nil {
		return nil, false
	}
	lookups := len(nae.AllOC_PropertyLookup())

	switch {
	case atom.OC_Variable() != nil:
		ref, ok := refFromNonArithmetic(nae)
		if !ok {
			return nil, false
		}
		l.curPart.refs = append(l.curPart.refs, varRef{name: ref.Variable})
		return query.NewRefProjection(ref, l.refType(ref)), true

	case atom.COUNT() != nil:
		// The count-star atom count(*): the degenerate aggregate, AggCount with no
		// referenced binding. A property lookup on it (count(*).x) is residual.
		// Stage 10: count(*) types as TypeInt unconditionally — openCypher's count
		// returns an integer by specification, so the schema-agnostic parser can
		// commit here without ever being wrong (spec §1.2).
		if lookups > 0 {
			return nil, false
		}
		return query.NewAggregateProjection(query.AggCount, nil, false, aggregateResultType(query.AggCount, nil)), true

	case atom.OC_Literal() != nil:
		if lookups > 0 || !isScalarLiteral(atom.OC_Literal()) {
			return nil, false
		}
		return query.NewLiteralProjection(literalType(atom.OC_Literal())), true

	case atom.OC_FunctionInvocation() != nil:
		if lookups > 0 {
			return nil, false
		}
		return l.classifyFunction(atom.OC_FunctionInvocation())

	default:
		return nil, false
	}
}

// refType computes the Stage-6 result type of a RefProjection: TypeNode /
// TypeEdge when the ref names a whole entity binding in the current part,
// TypeList(TypeEdge) for a variable-length edge binding (Stage 8; the
// projected value is a list of edges rather than a single edge), TypePath
// when the ref names a path binding (Stage 8), the recorded element type
// when the ref names an UnwindBinding (Stage 9; the projected value is
// one element of the source list), TypeUnknown for a property lookup
// (property typing is a schema concern per ADR 0003), and the imported
// alias's type when the name comes from a prior part's WITH. The lookup
// is per-part: the current part's own entity bindings first, then path
// bindings, then unwind bindings, then the imported map WITH exported
// into this part.
func (l *listener) refType(r query.Ref) query.Type {
	if r.Property != "" {
		return query.TypeUnknown{}
	}
	if idx, ok := l.curPart.byVar[r.Variable]; ok {
		rb := l.curPart.bindings[idx]
		switch rb.kind {
		case graph.Node:
			return query.TypeNode{}
		case graph.Edge:
			if rb.hops != nil {
				return query.NewTypeList(query.TypeEdge{})
			}
			return query.TypeEdge{}
		}
	}
	for _, pb := range l.curPart.pathBindings {
		if pb.Variable() == r.Variable {
			return query.TypePath{}
		}
	}
	for _, ub := range l.curPart.unwindBindings {
		if ub.Variable() == r.Variable {
			return ub.ElementType()
		}
	}
	// Stage 14: a bare RETURN on a CALL YIELD variable types as the
	// CallBinding's ResultType — mirrors UnwindBinding's ElementType
	// participation above. A property lookup on the CallBinding falls
	// through to TypeUnknown at the top of the function.
	for _, cb := range l.curPart.callBindings {
		if cb.Variable() == r.Variable {
			return cb.ResultType()
		}
	}
	if t, ok := l.curPart.imported[r.Variable]; ok {
		return t
	}
	return query.TypeUnknown{}
}

// classifyFunction maps a function invocation to a FuncProjection or, when its
// name matches the closed aggregate set (case-insensitively, §4), an
// AggregateProjection. Either way it mines the call's referenced bindings; any
// argument that is not a bare var/var.prop or scalar literal makes it residual
// (no expression tree, no nested aggregates, spec §4/§9). Stage 10: aggregate
// results carry the per-aggregate Stage-10 result type computed from the
// single expression argument's Stage-6 result type via aggregateResultType,
// and the DISTINCT keyword (fi.DISTINCT() non-nil) enters the model as the
// aggregate's Distinct axis. Stage 7 widens a plain FuncProjection's result
// type when the call's full name matches the seven-name temporal constructor
// set (spec §4). Every other function call keeps TypeUnknown — function
// identity is below the type-interface boundary (ADR 0005).
func (l *listener) classifyFunction(fi gen.IOC_FunctionInvocationContext) (query.Projection, bool) {
	// aggregate-kind-rich-exprs spec §4.1: the aggregate-name check runs
	// BEFORE functionArgRefs so an aggregate call with a rich argument
	// (sum(x+1), count(n.age+1), collect(a OR b), min([1,2,3]),
	// sum(range(1,3)), count(count(*))) lowers as an AggregateProjection
	// with refs mined through the Stage-6 walker, instead of type-erasing
	// through classifyRichExpression's ExprProjection fallback. Non-
	// aggregate function calls keep the strict functionArgRefs path
	// (§1.11: FuncProjection identity is below the type-interface
	// boundary — widening it to carry rich arguments is out of scope).
	if name, ok := functionName(fi); ok {
		if fn, ok := aggregateFunc(name); ok {
			return l.classifyAggregateCall(fi, fn), true
		}
	}
	refs, ok := functionArgRefs(fi)
	if !ok {
		return nil, false
	}
	for _, ref := range refs {
		l.curPart.refs = append(l.curPart.refs, varRef{name: ref.Variable})
	}
	resultType := query.Type(query.TypeUnknown{})
	if t, ok := temporalConstructorType(fullFunctionName(fi)); ok {
		resultType = t
	} else if name, ok := functionName(fi); ok {
		// Builtin scalar-function widening (gqlc-v5t): the bare-name lookup
		// upgrades the honest TypeUnknown to a concrete grammar-level type
		// when the arg shapes commit. `functionName` returns ok=false for a
		// namespaced call, so a shadowing `foo.elementId` cannot match.
		// Arg types are computed from the grammatical argument list — not
		// the refs slice, which drops scalar-literal args — so a mismatched
		// arity (`elementId('lit', n)`) does not silently promote.
		if t, ok := builtinScalarFuncType(name, l.builtinArgTypes(fi)); ok {
			resultType = t
		}
	}
	return query.NewFuncProjection(refs, resultType), true
}

// builtinArgTypes computes the Stage-6 result type of each grammatical
// argument of a function invocation, preserving argument order. Only the
// bare-variable atom shape (Ref{Variable}) participates via l.refType — a
// scalar-literal, property lookup, expression, or any other atom stays
// TypeUnknown so the caller's arg-kind check does not silently accept a
// mismatched shape. The parser has already gated the call through
// functionArgRefs (§4/§9: bare var / var.prop / scalar literal only), so
// unreachable atoms cannot appear here.
func (l *listener) builtinArgTypes(fi gen.IOC_FunctionInvocationContext) []query.Type {
	args := fi.AllOC_Expression()
	out := make([]query.Type, len(args))
	for i, arg := range args {
		nae := nonArithmeticAtom(arg)
		if nae == nil {
			out[i] = query.TypeUnknown{}
			continue
		}
		ref, ok := refFromNonArithmetic(nae)
		if !ok {
			out[i] = query.TypeUnknown{}
			continue
		}
		out[i] = l.refType(ref)
	}
	return out
}

// classifyAggregateCall lowers an aggregate function invocation, whether its
// arguments are bare (var/var.prop/scalar-literal) or rich (arithmetic,
// nested calls, list/map literals, parameters, CASE, comprehensions,
// count(*)). Refs are mined via typeExpressionMining — the same Stage-6
// walker classifyRichExpression uses — so a bare argument mines the same
// Ref{Variable, Property} the old functionArgRefs path produced (spec §1.4,
// bit-identity traced at shape.go:29-48 vs. typing.go:322-326 +
// typing.go:292-300), while a rich argument mines every var/var.prop atom in
// depth-first, left-to-right traversal order with duplicates preserved.
// Parameters encountered under the argument sub-tree are registered as
// ExprUse{aggregateResultType(fn, operand), ExprInProjection} — Stage 6 §4
// "no parameter is silently dropped" preserved verbatim, with the aggregate
// call's own result type as the enclosingType (the analogue of
// classifyRichExpression's whole-expression type; critical for count($p)
// where operand=TypeUnknown but the aggregate result is TypeInt).
func (l *listener) classifyAggregateCall(fi gen.IOC_FunctionInvocationContext, fn query.AggregateFunc) query.Projection {
	var operand query.Type
	var refs []query.Ref
	var params []antlr.Tree
	args := fi.AllOC_Expression()
	if len(args) > 0 {
		var opRefs []query.Ref
		var opParams []antlr.Tree
		operand, opRefs, opParams = l.typeExpressionMining(args[0])
		refs = append(refs, opRefs...)
		params = append(params, opParams...)
	}
	for _, arg := range args[1:] {
		_, more, moreParams := l.typeExpressionMining(arg)
		refs = append(refs, more...)
		params = append(params, moreParams...)
	}
	resultType := aggregateResultType(fn, operand)
	for _, p := range params {
		name := parameterName(p)
		if name == "" {
			continue
		}
		l.addParameterUse(name, p, query.NewExprUse(resultType, query.ExprInProjection))
	}
	distinct := fi.DISTINCT() != nil
	return query.NewAggregateProjection(fn, refs, distinct, resultType)
}

// mineClauseSlotParameter mines a bare $p atom from a SKIP or LIMIT expression,
// recording it as a ClauseSlotUse on the named parameter. Any non-bare $p in
// the expression (e.g. SKIP $p + 1, LIMIT f($p)) is unsupported and surfaces
// as ErrUnsupportedParameter, mirroring rejectClauseParameter's discipline for
// non-bare cases on the remaining accept-and-ignored clause (ORDER BY).
func (l *listener) mineClauseSlotParameter(e gen.IOC_ExpressionContext, slot query.ClauseSlot) {
	if e == nil {
		return
	}
	if name, node, ok := parameterFromExpr(e); ok {
		l.addParameterUse(name, node, query.NewClauseSlotUse(slot))
		return
	}
	if len(findParameters(e)) > 0 {
		l.fail(fmt.Errorf("%w: %s $param", ErrUnsupportedParameter, slot.ClauseName()))
	}
}

// --- parameters (Cluster D) ---

// mineWhere mines parameter uses from a WHERE predicate. Two layers: (i) the
// comparison-pair miner records a PropertyUse on every var.prop-vs-$p pair
// it recognises (D1a — the honest, resolvable case); (ii) any residual $p the
// pair miner did not approve is recorded as an ExprUse{ enclosingType,
// ExprInPredicate } via the rich-expression typer (Stage 6 §4). The
// enclosingType is the WHERE predicate's result type — TypeBool for a normal
// predicate, TypeUnknown for a shape the typer cannot commit to.
//
// Only parameter mining runs here; the WHERE's refs are NOT recorded on the
// current part (predicate structure is intentionally not modelled — B1,
// ADR 0003). The rich-typer sweep would otherwise append every variable in
// the predicate as a curPart ref and force it into scope, breaking WHERE
// occurrences of names bound by an intermediate WITH's aggregation alias.
// The listener's refs slice is snapshotted around the mining call so any
// varRefs the typer appended for its own recursion are discarded.
func (l *listener) mineWhere(w gen.IOC_WhereContext) {
	if w == nil {
		return
	}
	l.mineComparisons(w.OC_Expression())
	e := w.OC_Expression()
	savedRefs := l.curPart.refs
	t, _, params := l.typeExpressionMining(e)
	l.curPart.refs = savedRefs
	for _, p := range params {
		name := parameterName(p)
		if name == "" {
			continue
		}
		l.addParameterUse(name, p, query.NewExprUse(t, query.ExprInPredicate))
	}
}

// mineComparisons walks the predicate's comparison expressions, recording a
// parameter use when a comparison or string predicate pairs a single-level
// var.prop with a $param (D1a). It does not interpret predicate structure beyond
// finding these pairs; everything else is left for the approval sweep.
//
// Stage 11 §1.2 / §1.5: the walk stops at an EXISTS { ... } or list-quantifier
// (ALL / ANY / NONE / SINGLE) subtree boundary. Comparisons inside a predicate
// subquery / a quantifier filter body live in a nested scope — an inner
// var.prop = $p pair inside them would (a) mint the wrong Use variant on the
// outer parameter (a PropertyUse against an inner var, silently dropping the
// spec §1.5 ExprUse{TypeBool, ExprInPredicate}), and (b) leak the inner
// variable into curPart.refs via pairAddSub, breaking legal queries with
// ErrUnboundVariable at build's referential-integrity sweep. Parameters inside
// these subtrees are mined by their own hooks: EnterOC_ExistentialSubquery for
// EXISTS { ... }, typeQuantifier for the four quantifiers.
func (l *listener) mineComparisons(e antlr.Tree) {
	if e == nil {
		return
	}
	switch e.(type) {
	case gen.IOC_ExistentialSubqueryContext, gen.IOC_QuantifierContext:
		return
	}
	if cmp, ok := e.(gen.IOC_ComparisonExpressionContext); ok {
		l.mineComparison(cmp)
	}
	for i := 0; i < e.GetChildCount(); i++ {
		l.mineComparisons(e.GetChild(i))
	}
}

// mineComparison records uses for one comparison expression: each partial
// comparison ('=' '<>' '<' '<=' '>' '>=') pairs the running left operand with its
// right operand, and a string predicate (STARTS WITH / ENDS WITH / CONTAINS)
// pairs its base with its argument. A pair of (var.prop, $param) in either order
// yields Use=Ref{var, prop}.
func (l *listener) mineComparison(c gen.IOC_ComparisonExpressionContext) {
	left := c.OC_StringListNullPredicateExpression()
	l.mineStringPredicate(left)
	for _, partial := range c.AllOC_PartialComparisonExpression() {
		right := partial.OC_StringListNullPredicateExpression()
		l.pairOperands(left, right)
		l.mineStringPredicate(right)
		left = right
	}
}

// mineStringPredicate records a use for a STARTS WITH / ENDS WITH / CONTAINS
// predicate, pairing its base operand with the predicate argument.
func (l *listener) mineStringPredicate(p gen.IOC_StringListNullPredicateExpressionContext) {
	if p == nil {
		return
	}
	for _, sp := range p.AllOC_StringPredicateExpression() {
		l.pairAddSub(p.OC_AddOrSubtractExpression(), sp.OC_AddOrSubtractExpression())
	}
}

// pairOperands records a use if one of the two comparison operands is a
// single-level var.prop and the other is a $param.
func (l *listener) pairOperands(a, b gen.IOC_StringListNullPredicateExpressionContext) {
	l.pairAddSub(stringListNullBase(a), stringListNullBase(b))
}

// pairAddSub records a use if one operand is a single-level var.prop on a bound
// variable and the other is a parameter $name: it adds a PropertyUse{Ref{var,
// prop}} to that parameter's uses and approves the parameter node.
func (l *listener) pairAddSub(a, b gen.IOC_AddOrSubtractExpressionContext) {
	if a == nil || b == nil {
		return
	}
	if ref, ok := propertyRefFromAddSub(a); ok {
		if param, node, ok := parameterFromAddSub(b); ok {
			l.addParameterUse(param, node, query.NewPropertyUse(query.Ref{Variable: ref.Variable, Property: ref.Property}))
			l.curPart.refs = append(l.curPart.refs, varRef{name: ref.Variable})
		}
		return
	}
	if ref, ok := propertyRefFromAddSub(b); ok {
		if param, node, ok := parameterFromAddSub(a); ok {
			l.addParameterUse(param, node, query.NewPropertyUse(query.Ref{Variable: ref.Variable, Property: ref.Property}))
			l.curPart.refs = append(l.curPart.refs, varRef{name: ref.Variable})
		}
	}
}

// mineInlineMap mines parameter uses from an inline property map on a pattern
// element bound to variable (D1b): each key whose value is a $param yields a
// PropertyUse{Ref{variable, key}}. A parameter standing for the whole map
// ((a {$p})) is unsupported, as is a $param in the map of an anonymous element
// (no variable to bind the property to) — both surface as ErrUnsupportedParameter.
// mineInlineMap records refs and parameter uses that an inline property map
// carries. The fast path (Stage 12) handles bare-$param values — each records
// a PropertyUse{Ref{variable, key}} and does not push a ref to curPart.refs
// (a parameter is not a binding). The widening (Stage 13 §4.3): every other
// value shape (bare variable, var.prop, rich expression) routes through the
// Stage-6 rich typer so its var/var.prop atoms flow onto curPart.refs via
// typeAtom, and any nested parameters record PropertyUse{Ref{variable, key}}
// — the same shape the fast path uses, because the inline-map key gives the
// parser a concrete property target the SET-value case does not have.
//
// Motivated by MERGE (Merge1[11]: `MERGE (city:City {name: person.bornIn})`
// — without the widening, `person` is silently absent from curPart.refs and
// buildPart's referential-integrity sweep does not press). Applies uniformly
// to MATCH / CREATE / MERGE because the same mining function services all
// three, so the widening is consistent-recording, not a MERGE-specific
// carve-out. Also converts `CREATE (a {name: b.c})` (b unbound) from a
// silent parse to an ErrUnboundVariable rejection at the buildPart sweep.
func (l *listener) mineInlineMap(variable string, p gen.IOC_PropertiesContext) {
	if p == nil {
		return
	}
	if p.OC_Parameter() != nil {
		l.fail(fmt.Errorf("%w: parameter as a whole property map", ErrUnsupportedParameter))
		return
	}
	m := p.OC_MapLiteral()
	if m == nil {
		return
	}
	keys := m.AllOC_PropertyKeyName()
	exprs := m.AllOC_Expression()
	for i := range keys {
		// Fast path: value is a bare $param → PropertyUse{Ref{var, key}}.
		if param, node, ok := parameterFromExpr(exprs[i]); ok {
			if variable == "" {
				l.fail(fmt.Errorf("%w: %s in an anonymous pattern element", ErrUnsupportedParameter, param))
				return
			}
			l.addParameterUse(param, node, query.NewPropertyUse(query.Ref{Variable: variable, Property: keys[i].GetText()}))
			continue
		}
		// Widening (§4.3): route the value expression through the rich typer so
		// var / var.prop atoms flow onto curPart.refs, and any nested parameters
		// record PropertyUse{Ref{var, key}} — the same shape the fast path uses.
		_, _, params := l.typeExpressionMining(exprs[i])
		for _, node := range params {
			name := parameterName(node)
			if name == "" {
				continue
			}
			if variable == "" {
				l.fail(fmt.Errorf("%w: %s in an anonymous pattern element", ErrUnsupportedParameter, name))
				return
			}
			l.addParameterUse(name, node, query.NewPropertyUse(query.Ref{Variable: variable, Property: keys[i].GetText()}))
		}
	}
	// Any parameter under this map that was not a direct key value (e.g. nested in
	// a list) is unsupported.
	l.requireAllParametersApproved(m)
}

// requireAllParametersApproved fails if any parameter under e was not mined into
// a property Use: a parameter that is not bindable to a single-level property is
// unsupported (D1).
func (l *listener) requireAllParametersApproved(e antlr.Tree) {
	for _, node := range findParameters(e) {
		if !l.approved[node] {
			l.fail(fmt.Errorf("%w: %s", ErrUnsupportedParameter, parameterName(node)))
			return
		}
	}
}

// addParameterUse appends a Use to the named parameter — creating it in
// first-appearance order on first sight — and marks the parameter node
// approved. The single chokepoint for parameter dedup-by-Name across both
// Use variants: every caller (a property predicate, an inline property map,
// a SKIP/LIMIT clause slot) flows through here so the dedup-and-order
// discipline lives in exactly one place. Stamps the branch-relative Part index
// via attributePart at emission time (fvo per ADR 0008 amendment 2026-07-06).
func (l *listener) addParameterUse(name string, node antlr.Tree, use query.Use) {
	idx, ok := l.byParam[name]
	if !ok {
		idx = len(l.params)
		l.byParam[name] = idx
		l.params = append(l.params, &query.Parameter{Name: name})
	}
	l.params[idx].Uses = append(l.params[idx].Uses, attributePart(use, l.currentPartIndex()))
	l.approved[node] = true
}

// currentPartIndex returns the branch-relative index of the Part collection
// handlers currently write into — len(curBranch.parts)-1 by construction of
// the priming discipline at listener.go:EnterOC_SingleQuery and
// EnterOC_StandaloneCall (fvo per ADR 0008 amendment 2026-07-06). Every
// addParameterUse call site runs under a non-nil curBranch and non-nil
// curPart, so the subtraction is well-defined.
func (l *listener) currentPartIndex() int {
	return len(l.curBranch.parts) - 1
}

// attributePart returns the Use with its part field populated. Sum-preserving:
// the returned Use has the same variant as u. Used by addParameterUse to
// stamp the branch-relative Part index onto every Use at emission time.
func attributePart(u query.Use, part int) query.Use {
	switch uu := u.(type) {
	case query.PropertyUse:
		return query.NewPropertyUseAt(uu.Ref(), part)
	case query.ExprUse:
		return query.NewExprUseAt(uu.EnclosingType(), uu.Position(), part)
	case query.ClauseSlotUse:
		return query.NewClauseSlotUseAt(uu.Slot(), part)
	default:
		return u
	}
}

// --- write clauses (Stage 12) ---

// collectSetItem dispatches one SET item into the appropriate Effect variant
// (Stage 12 spec §4.3). The four grammar alternatives are:
//
//  1. propertyExpression = expression → SetPropertyEffect
//  2. variable = expression           → SetEntityEffect{op: Replace}
//  3. variable += expression          → SetEntityEffect{op: Merge}
//  4. variable :Labels                → SetLabelsEffect
//
// Cases 1 and (2/3) both carry a value expression; both mine parameters via
// the Stage-6 rich typer against the value's Stage-6 result type. Value
// parameters record ExprInSetValue — SET values are producers of values the
// engine writes to the graph, semantically opposite to a projection column's
// consumer role, so the position discriminator distinguishes the roles the
// resolver may key on (spec §1.5 amend). Case 1 rejects a nested LHS
// (n.a.b) via ErrNestedPropertyTarget: the model's Ref carries a single
// Property, so a nested LHS has no honest single-Ref shape.
func (l *listener) collectSetItem(item gen.IOC_SetItemContext) {
	switch {
	case item.OC_PropertyExpression() != nil && item.OC_Expression() != nil:
		target, ok := propertyExpressionRef(item.OC_PropertyExpression())
		if !ok {
			l.fail(fmt.Errorf("%w: SET %s", ErrNestedPropertyTarget, item.OC_PropertyExpression().GetText()))
			return
		}
		l.curPart.refs = append(l.curPart.refs, varRef{name: target.Variable})
		valueType, refs, params := l.typeExpressionMining(item.OC_Expression())
		for _, p := range params {
			name := parameterName(p)
			if name == "" {
				continue
			}
			l.addParameterUse(name, p, query.NewExprUse(valueType, query.ExprInSetValue))
		}
		eff, err := query.NewSetPropertyEffect(target, valueType, refs)
		if err != nil {
			l.fail(err)
			return
		}
		l.curPart.effects = append(l.curPart.effects, eff)

	case item.OC_Variable() != nil && item.OC_NodeLabels() != nil:
		variable := item.OC_Variable().GetText()
		l.curPart.refs = append(l.curPart.refs, varRef{name: variable})
		labels := nodeLabels(item.OC_NodeLabels())
		eff, err := query.NewSetLabelsEffect(variable, labels)
		if err != nil {
			l.fail(err)
			return
		}
		l.curPart.effects = append(l.curPart.effects, eff)

	case item.OC_Variable() != nil && item.OC_Expression() != nil:
		variable := item.OC_Variable().GetText()
		l.curPart.refs = append(l.curPart.refs, varRef{name: variable})
		op := setItemOp(item)
		valueType, refs, params := l.typeExpressionMining(item.OC_Expression())
		for _, p := range params {
			name := parameterName(p)
			if name == "" {
				continue
			}
			l.addParameterUse(name, p, query.NewExprUse(valueType, query.ExprInSetValue))
		}
		eff, err := query.NewSetEntityEffect(variable, op, valueType, refs)
		if err != nil {
			l.fail(err)
			return
		}
		l.curPart.effects = append(l.curPart.effects, eff)
	}
}

// collectRemoveItem dispatches one REMOVE item (Stage 12 spec §4.4). Two
// grammar alternatives: variable :Labels → RemoveLabelsEffect,
// propertyExpression → RemovePropertyEffect. A nested propertyExpression
// (n.a.b) rejects via ErrNestedPropertyTarget — same shape rule as SET
// (spec §1.6 amend).
func (l *listener) collectRemoveItem(item gen.IOC_RemoveItemContext) {
	if item.OC_Variable() != nil && item.OC_NodeLabels() != nil {
		variable := item.OC_Variable().GetText()
		l.curPart.refs = append(l.curPart.refs, varRef{name: variable})
		labels := nodeLabels(item.OC_NodeLabels())
		eff, err := query.NewRemoveLabelsEffect(variable, labels)
		if err != nil {
			l.fail(err)
			return
		}
		l.curPart.effects = append(l.curPart.effects, eff)
		return
	}
	if pe := item.OC_PropertyExpression(); pe != nil {
		target, ok := propertyExpressionRef(pe)
		if !ok {
			l.fail(fmt.Errorf("%w: REMOVE %s", ErrNestedPropertyTarget, pe.GetText()))
			return
		}
		l.curPart.refs = append(l.curPart.refs, varRef{name: target.Variable})
		eff, err := query.NewRemovePropertyEffect(target)
		if err != nil {
			l.fail(err)
			return
		}
		l.curPart.effects = append(l.curPart.effects, eff)
	}
}
