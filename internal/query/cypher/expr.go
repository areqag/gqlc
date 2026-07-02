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
// that a parameter in any of them is rejected or bound to a clause slot: a
// dropped $param is a missing generated argument, i.e. a type-interface change
// (B1). Each item is classified into a Projection variant: the bare-atom
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
// the resolver upgrades from the schema post-freeze (Stage 9 spec
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
	refs, ok := functionArgRefs(fi)
	if !ok {
		return nil, false
	}
	for _, ref := range refs {
		l.curPart.refs = append(l.curPart.refs, varRef{name: ref.Variable})
	}
	if name, ok := functionName(fi); ok {
		if fn, ok := aggregateFunc(name); ok {
			// The aggregate's operand type is the Stage-6 type of its single
			// expression argument. Multi-argument aggregates (percentile*, whose
			// second argument is the percentile) type as TypeUnknown regardless:
			// the argument list is walked only for the first operand's type,
			// and aggregateResultType(AggPercentile, _) returns TypeUnknown
			// anyway, so no special-case is needed.
			var operand query.Type
			if args := fi.AllOC_Expression(); len(args) > 0 {
				operand, _ = l.typeExpression(args[0])
			}
			distinct := fi.DISTINCT() != nil
			return query.NewAggregateProjection(fn, refs, distinct, aggregateResultType(fn, operand)), true
		}
	}
	resultType := query.Type(query.TypeUnknown{})
	if t, ok := temporalConstructorType(fullFunctionName(fi)); ok {
		resultType = t
	}
	return query.NewFuncProjection(refs, resultType), true
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
