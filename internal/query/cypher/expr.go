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
// cosmetic parts (DISTINCT, ORDER BY, SKIP/LIMIT) are accept-and-ignored, except
// that a parameter in any of them is rejected: a dropped $param is a missing
// generated argument, i.e. a type-interface change (B1). Each item is classified
// into a Projection variant (var/var.prop, scalar literal, function or
// aggregate); residual rich shapes are rejected (ErrUnsupportedProjection). The
// '*' alternative sets ReturnsAll.
func (l *listener) collectProjection(body gen.IOC_ProjectionBodyContext) {
	if body == nil {
		return
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

	// ORDER BY / SKIP / LIMIT are accept-and-ignored, but a parameter in any of
	// them would be silently dropped from the model — a type-interface change — so
	// it is rejected. (No verbatim TCK read-core query reaches the ORDER BY case:
	// the TCK's ORDER-BY-parameter queries all reject earlier on an aggregation
	// projection or a WITH clause. The guard is correct-by-construction, just not
	// corpus-exercised at Stage 0.)
	if o := body.OC_Order(); o != nil {
		for _, item := range o.AllOC_SortItem() {
			l.rejectClauseParameter(item.OC_Expression(), "ORDER BY")
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

// collectReturnItem lowers one projection item by classifying its expression
// into a Projection variant (var/var.prop, scalar literal, function call or
// aggregate); residual rich shapes are rejected (ErrUnsupportedProjection). The
// column name is the explicit AS alias if present, else the verbatim source text
// of the expression (E1).
func (l *listener) collectReturnItem(item gen.IOC_ProjectionItemContext) {
	value, ok := l.classifyProjection(item.OC_Expression())
	if !ok {
		l.fail(fmt.Errorf("%w: %s", ErrUnsupportedProjection, originalText(l.ts, item.OC_Expression())))
		return
	}

	name := originalText(l.ts, item.OC_Expression())
	if alias := item.OC_Variable(); alias != nil {
		name = alias.GetText()
	}

	l.curPart.returns = append(l.curPart.returns, query.ReturnItem{Name: name, Value: value})
}

// classifyProjection maps a RETURN-item expression to its Projection variant,
// appending a varRef for every binding the projection references so build()'s
// referential-integrity sweep covers them. ok is false for a residual shape
// (arithmetic over a projection, a list/map literal, a label predicate, a
// function argument that is not a bare var/var.prop or scalar literal, CASE,
// comprehensions, parameters): the caller raises ErrUnsupportedProjection.
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
		// Stage 6: TypeUnknown — aggregate return types are below the boundary
		// (ADR 0005); a real engine yields int, the resolver upgrades from the
		// schema.
		if lookups > 0 {
			return nil, false
		}
		return query.NewAggregateProjection(query.AggCount, nil, query.TypeUnknown{}), true

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
// TypeUnknown for a property lookup (property typing is a schema concern per
// ADR 0003), and the imported alias's type when the name comes from a prior
// part's WITH. The lookup is per-part: the current part's own bindings first,
// then the imported map WITH exported into this part.
func (l *listener) refType(r query.Ref) query.Type {
	if r.Property != "" {
		return query.TypeUnknown{}
	}
	if idx, ok := l.curPart.byVar[r.Variable]; ok {
		switch l.curPart.bindings[idx].kind {
		case graph.Node:
			return query.TypeNode{}
		case graph.Edge:
			return query.TypeEdge{}
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
// (no expression tree, no nested aggregates, spec §4/§9). Stage 6: both
// variants carry TypeUnknown — function identity and aggregate return type
// live below the type-interface boundary (ADR 0005).
func (l *listener) classifyFunction(fi gen.IOC_FunctionInvocationContext) (query.Projection, bool) {
	refs, ok := functionArgRefs(fi)
	if !ok {
		return nil, false
	}
	for _, ref := range refs {
		l.curPart.refs = append(l.curPart.refs, varRef{name: ref.Variable})
	}
	if name, ok := functionName(fi); ok {
		if fn, ok := aggregateFunc(name); ok {
			return query.NewAggregateProjection(fn, refs, query.TypeUnknown{}), true
		}
	}
	return query.NewFuncProjection(refs, query.TypeUnknown{}), true
}

// rejectClauseParameter fails if the ORDER BY expression contains a parameter:
// the parameter would be dropped from the model rather than bound to a slot,
// so it is unsupported (Cluster D). SKIP and LIMIT have their own miner
// (mineClauseSlotParameter) that accepts a bare $p as a ClauseSlotUse.
func (l *listener) rejectClauseParameter(e gen.IOC_ExpressionContext, clause string) {
	if e == nil {
		return
	}
	if len(findParameters(e)) > 0 {
		l.fail(fmt.Errorf("%w: %s $param", ErrUnsupportedParameter, clause))
	}
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

// mineWhere mines parameter uses from a WHERE predicate (D1a) and then verifies
// every parameter in the predicate was mined: any other occurrence is unsupported.
func (l *listener) mineWhere(w gen.IOC_WhereContext) {
	if w == nil {
		return
	}
	l.mineComparisons(w.OC_Expression())
	l.requireAllParametersApproved(w.OC_Expression())
}

// mineComparisons walks the predicate's comparison expressions, recording a
// parameter use when a comparison or string predicate pairs a single-level
// var.prop with a $param (D1a). It does not interpret predicate structure beyond
// finding these pairs; everything else is left for the approval sweep.
func (l *listener) mineComparisons(e antlr.Tree) {
	if e == nil {
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
		param, node, ok := parameterFromExpr(exprs[i])
		if !ok {
			continue
		}
		if variable == "" {
			l.fail(fmt.Errorf("%w: %s in an anonymous pattern element", ErrUnsupportedParameter, param))
			return
		}
		l.addParameterUse(param, node, query.NewPropertyUse(query.Ref{Variable: variable, Property: keys[i].GetText()}))
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
// discipline lives in exactly one place.
func (l *listener) addParameterUse(name string, node antlr.Tree, use query.Use) {
	idx, ok := l.byParam[name]
	if !ok {
		idx = len(l.params)
		l.byParam[name] = idx
		l.params = append(l.params, &query.Parameter{Name: name})
	}
	l.params[idx].Uses = append(l.params[idx].Uses, use)
	l.approved[node] = true
}
