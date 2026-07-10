package cypher

import (
	"fmt"

	"github.com/areqag/gqlc/internal/grammar/cypher/gen"
	"github.com/areqag/gqlc/internal/procsig"
	"github.com/areqag/gqlc/internal/query"
)

// collectCall is the shared collection path for standalone and in-query
// CALL clauses (Stage 14 spec §4.2). It resolves the procedure name
// against the listener's registry, mines argument expressions for
// parameter uses and variable refs, checks the arity (explicit
// invocations only), and — when YIELD is present — mints one
// CallBinding per yielded column. Standalone CALL with no YIELD (or
// YIELD *) expands implicitly: one CallBinding per signature Result in
// declaration order. In-query CALL with no YIELD produces no
// CallBinding (spec §4.2 step 9); a downstream RETURN that references
// a would-be result column fails via ErrUnboundVariable at buildPart.
//
// Fail-sites (in order): unknown procedure (ErrUnknownProcedure),
// arity mismatch on explicit invocation (ErrProcedureArity), unknown
// YIELD result field (ErrUnknownProcedure — one sentinel covers both
// name and field miss per Q1 ruling). Intra-YIELD name collision is
// caught at buildPart's five-way sweep (build.go: `if callByVar[v]`),
// not here. Every fail-site fires before any CallBinding enters
// curPart.callBindings, so a failed lookup leaves the part unchanged.
func (l *listener) collectCall(
	procName gen.IOC_ProcedureNameContext,
	argExprs []gen.IOC_ExpressionContext,
	explicit bool,
	yieldStar bool,
	yieldItems gen.IOC_YieldItemsContext,
	standalone bool,
) {
	name := extractProcedureName(procName)
	if name == "" {
		return
	}
	sig, ok := l.registry.Lookup(name)
	if !ok {
		l.fail(fmt.Errorf("%w: %s", ErrUnknownProcedure, name))
		return
	}

	// Argument mining: refs, parameter uses, and per-position mined
	// types. The args slice (0ig) is retained on every CallBinding
	// minted from this CALL clause and consumed by the resolver's
	// argument-site assignability walk. Refs/params routing preserves
	// the Stage-13 "no silent parser info drop" invariant.
	var args []query.CallArg
	if len(argExprs) > 0 {
		args = make([]query.CallArg, 0, len(argExprs))
	}
	for _, e := range argExprs {
		t, _, params := l.typeExpressionMining(e)
		args = append(args, query.NewCallArg(t))
		for _, p := range params {
			n := parameterName(p)
			if n == "" {
				continue
			}
			l.addParameterUse(n, p, query.NewExprUse(t, query.ExprInProjection))
		}
	}

	// Arity check (explicit invocations only): implicit invocation
	// binds args from parameters at runtime, so its arity is
	// uncountable at parse time (spec §4.2 step 4, Q4 ruling).
	if explicit && len(argExprs) != len(sig.Params) {
		l.fail(fmt.Errorf("%w: %s expects %d arguments, got %d",
			ErrProcedureArity, name, len(sig.Params), len(argExprs)))
		return
	}

	// YIELD expansion. Three shapes:
	//   1. Explicit YIELD (items present): iterate items, resolve
	//      each against sig.Results.
	//   2. YIELD * (standalone-only, grammar-enforced): iterate
	//      sig.Results in declaration order.
	//   3. No YIELD: standalone expands like YIELD *; in-query
	//      produces no CallBinding.
	if yieldItems != nil {
		l.collectYieldItems(name, sig, yieldItems, args)
	} else if yieldStar || standalone {
		l.expandAllResults(name, sig, args)
	}
	// In-query CALL without YIELD: fall through — no CallBinding.

	if standalone && l.err == nil {
		// Standalone CALL populates Part.Returns from the CallBindings
		// at build time (spec §4.3). Set returnsAll so buildPart's
		// synthetic-Returns branch fires.
		l.curPart.callStandalone = true
	}
}

// extractProcedureName concatenates the oC_Namespace tokens with '.' and
// appends the trailing oC_SymbolicName: `test.my.proc` for
// `test.my.proc`. Namespace fragments follow the same dot-separated
// convention the grammar's oC_Namespace production admits (each
// oC_SymbolicName followed by '.').
func extractProcedureName(procName gen.IOC_ProcedureNameContext) string {
	if procName == nil {
		return ""
	}
	// The whole procedure name is the text of the procName context
	// itself, with all whitespace stripped. The grammar defines
	// procedureName as oC_Namespace oC_SymbolicName and namespace as
	// (oC_SymbolicName '.')*, so the concatenated text is exactly
	// the dotted fully-qualified name (no whitespace admits between
	// symbolic names and dots per the grammar).
	return procName.GetText()
}

// collectYieldItems iterates the YIELD list, resolving each item
// against the signature's Results and appending one CallBinding per
// item to curPart.callBindings. Intra-YIELD name collisions are
// caught downstream at buildPart's five-way sweep, not here. A YIELD
// trailing WHERE (grammar-legal, corpus-silent) is walked for
// parameter mining. Spec §4.2 step 6.
func (l *listener) collectYieldItems(
	procName string,
	sig procsig.Signature,
	items gen.IOC_YieldItemsContext,
	args []query.CallArg,
) {
	for _, item := range items.AllOC_YieldItem() {
		variable, sourceField := extractYieldItem(item)
		if variable == "" || sourceField == "" {
			continue
		}
		result, ok := findResultByName(sig.Results, sourceField)
		if !ok {
			l.fail(fmt.Errorf("%w result field: %s on %s",
				ErrUnknownProcedure, sourceField, procName))
			return
		}
		cb, err := query.NewCallBindingWithArgs(
			variable, procName, sourceField,
			typeForToken(result.Token), result.Nullable,
			args,
		)
		if err != nil {
			l.fail(err)
			return
		}
		l.curPart.callBindings = append(l.curPart.callBindings, cb)
	}
	// Trailing WHERE (grammar-legal, corpus-silent): walk for
	// parameter mining only. The predicate structure itself lives
	// below the type-interface boundary (ADR 0005) — same posture
	// as a WHERE under MATCH.
	if w := items.OC_Where(); w != nil {
		for _, p := range findParameters(w) {
			n := parameterName(p)
			if n == "" {
				continue
			}
			l.addParameterUse(n, p, query.NewExprUse(query.TypeBool{}, query.ExprInPredicate))
		}
	}
}

// expandAllResults produces one CallBinding per signature Result in
// declaration order — the shared path for YIELD * (standalone only)
// and no-YIELD standalone (spec §4.2 steps 7/8). Every binding uses
// its Result.Name as both variable and sourceField (no aliasing is
// possible in these shapes).
func (l *listener) expandAllResults(procName string, sig procsig.Signature, args []query.CallArg) {
	for _, result := range sig.Results {
		cb, err := query.NewCallBindingWithArgs(
			result.Name, procName, result.Name,
			typeForToken(result.Token), result.Nullable,
			args,
		)
		if err != nil {
			l.fail(err)
			return
		}
		l.curPart.callBindings = append(l.curPart.callBindings, cb)
	}
}

// extractYieldItem pulls the (variable, sourceField) pair out of an
// oC_YieldItem context. The grammar admits two shapes:
//
//   - `oC_ProcedureResultField SP AS SP oC_Variable`: sourceField is
//     the result field, variable is the AS-alias.
//   - `oC_Variable`: bare — sourceField == variable, no alias.
//
// The AS() terminal distinguishes the two; when present, the result
// field is a required earlier child. When absent, the variable's
// text serves as both names.
func extractYieldItem(item gen.IOC_YieldItemContext) (variable, sourceField string) {
	if item == nil {
		return "", ""
	}
	if v := item.OC_Variable(); v != nil {
		variable = v.GetText()
	}
	if item.AS() != nil {
		if f := item.OC_ProcedureResultField(); f != nil {
			sourceField = f.GetText()
		}
	} else {
		sourceField = variable
	}
	return variable, sourceField
}

// findResultByName is a linear scan over the signature's Results —
// the Results slices are small (Call5's five-column signature is the
// widest at Stage 14), so a map keyed lookup would be premature.
func findResultByName(results []procsig.Result, name string) (procsig.Result, bool) {
	for _, r := range results {
		if r.Name == name {
			return r, true
		}
	}
	return procsig.Result{}, false
}

// typeForToken bridges a procsig.TypeToken to the corresponding
// query.Type. NUMBER maps to TypeUnknown — the wire-honest translation
// (Q3 ruling): NUMBER is a signature-time marker with no honest
// result-column identity; post-freeze codegen consults the registry
// directly, so no information is lost. The default arm is a
// belt-and-braces guard against a future TypeToken widening reaching
// this bridge without a token addition here.
func typeForToken(tok procsig.TypeToken) query.Type {
	switch tok {
	case procsig.TokenFloat:
		return query.TypeFloat{}
	case procsig.TokenString:
		return query.TypeString{}
	case procsig.TokenNumber:
		return query.TypeUnknown{}
	case procsig.TokenInteger:
		return query.TypeInt{}
	default:
		return query.TypeUnknown{}
	}
}

// callArgs extracts the argument expression list from an explicit
// procedure invocation context, in source order.
func callArgs(inv gen.IOC_ExplicitProcedureInvocationContext) []gen.IOC_ExpressionContext {
	if inv == nil {
		return nil
	}
	return inv.AllOC_Expression()
}

// callProcName pulls the oC_ProcedureName off an explicit or implicit
// invocation context, whichever is non-nil.
func callProcName(
	explicit gen.IOC_ExplicitProcedureInvocationContext,
	implicit gen.IOC_ImplicitProcedureInvocationContext,
) gen.IOC_ProcedureNameContext {
	if explicit != nil {
		return explicit.OC_ProcedureName()
	}
	if implicit != nil {
		return implicit.OC_ProcedureName()
	}
	return nil
}

// enterInQueryCall is the collectCall entry point for oC_InQueryCall.
// The grammar (§140) forbids implicit invocation and YIELD *; both
// pass through mustRejectGrammar as ANTLR-level parse errors, so this
// entry point only ever sees an explicit invocation with either
// no YIELD or an oC_YieldItems block.
func (l *listener) enterInQueryCall(c *gen.OC_InQueryCallContext) {
	inv := c.OC_ExplicitProcedureInvocation()
	procName := callProcName(inv, nil)
	l.collectCall(
		procName,
		callArgs(inv),
		true, // explicit invocation
		false,
		c.OC_YieldItems(),
		false, // in-query
	)
}

// enterStandaloneCall is the collectCall entry point for
// oC_StandaloneCall. The grammar admits both explicit and implicit
// invocations, and either no YIELD, `YIELD *`, or `YIELD items`.
func (l *listener) enterStandaloneCall(c *gen.OC_StandaloneCallContext) {
	explicit := c.OC_ExplicitProcedureInvocation()
	implicit := c.OC_ImplicitProcedureInvocation()
	procName := callProcName(explicit, implicit)
	args := callArgs(explicit)
	yieldItems := c.OC_YieldItems()
	yieldStar := c.YIELD() != nil && yieldItems == nil
	l.collectCall(
		procName,
		args,
		explicit != nil, // explicit iff the explicit invocation branch fired
		yieldStar,
		yieldItems,
		true, // standalone
	)
}
