package cypher

import (
	"fmt"

	"github.com/antlr4-go/antlr/v4"

	"github.com/areqag/gqlc/internal/grammar/cypher/gen"
	"github.com/areqag/gqlc/internal/graph"
	"github.com/areqag/gqlc/internal/query"
)

// listener is the single error sink and collector for a parse: it captures the
// first lexer/parser syntax error and the errors raised during the walk — both
// funnelling into l.err. The walk cannot be stopped mid-traversal (ADR 0001);
// fail() keeps the first error and Parse discards the result once one is set, so
// an Enter* that runs after the first error is harmless. Mirrors schema/gql.
//
// One collection pass + build(): the Enter* handlers collect into a nested
// branch/part structure plus a query-wide parameter table, and build() assembles
// query.Query at end of walk. There is no schema-style resolve() second pass —
// endpoints record a variable name and labels live on the binding, so there is no
// parse-time endpoint->type lookup. build() does a self-consistency validation,
// not a resolution.
//
// Stage 4 makes collection two-axis (spec §2): EnterOC_SingleQuery opens a new
// branch (the first, and one per UNION), EnterOC_With closes the current part and
// opens the next within a branch, and EnterOC_Union records the combinator. Each
// part accumulates its own bindings/returns/refs; parameters stay query-wide.
type listener struct {
	*gen.BaseCypherListener
	*antlr.DefaultErrorListener

	ts *antlr.CommonTokenStream

	// branches are the collected branches in source order. The current branch and
	// current part (the collection targets EnterOC_Match/With/Return write into)
	// are tracked by curBranch/curPart, set when a branch or part opens.
	branches  []*rawBranch
	curBranch *rawBranch
	curPart   *rawPart

	// combinators records how each branch after the first joins its predecessor;
	// it has len(branches)-1 entries (spec §2). EnterOC_Union appends one before
	// the joined branch's EnterOC_SingleQuery fires.
	combinators []query.UnionKind

	// params are the collected parameters in first-appearance order, indexed by
	// name in byParam so repeat uses accumulate onto one Parameter. They are
	// query-wide (deduped across all parts/branches), unaffected by scope
	// boundaries (spec §4).
	params   []*query.Parameter
	byParam  map[string]int
	approved map[antlr.Tree]bool // oC_Parameter nodes mined into a Use

	// exprParams collects the oC_Parameter nodes the current rich-expression
	// typing pass has walked, so the caller can register an ExprUse for each
	// once the enclosing expression's result type is known (Stage 6 §4). Nil
	// outside a rich-expression typing call (typeExpressionMining); populated
	// on entry to that call and restored on return, so nested calls do not
	// leak parameters into an enclosing expression's use list.
	exprParams []antlr.Tree

	// subqueryDepth is the Stage-11 suppression counter for EXISTS { ... }.
	// EnterOC_ExistentialSubquery increments it; every clause-collecting
	// Enter* handler (Match, With, Return, Unwind, Create, Merge, Delete,
	// Set, Remove, InQueryCall, StandaloneCall) early-returns while it is
	// positive, so inner bindings, refs, projections, and per-clause
	// rejections never touch the outer part's state. The counter is
	// depth-counting (not a flag) so nested EXISTS suppress at every level.
	// Parameters inside the subquery are mined once, at EnterOC_ExistentialSubquery,
	// via findParameters — the subquery body itself is not walked for
	// collection.
	subqueryDepth int

	// writeSeen is the Stage-12 query-wide flag build() reads to populate
	// Query.StatementKind. Set true by every outer-scope Enter handler for
	// a write clause (Create / Delete / Set / Remove). A write suppressed
	// inside an EXISTS { ... } subquery does not flip the flag — the outer
	// query does not modify the graph. Stage 13 will also flip it on
	// EnterOC_Merge once MERGE lands; today MERGE stays behind
	// ErrUnsupportedClause.
	writeSeen bool

	err error
}

// rawBranch is a branch under construction: its ordered parts. One per
// oC_SingleQuery.
type rawBranch struct {
	parts []*rawPart
}

// rawPart is one WITH-bounded scope segment under construction: its bindings (in
// first-appearance order, with byVar indexing named ones for merge), its return
// items / wildcard flag, and the variable refs build() must resolve against this
// part's scope. byVar is per-part: a name re-MATCHed in a later part is a fresh
// binding there (spec §3). imported records the exported name → Stage-6 result
// type from the prior part's WITH; classifyProjection consults it when a ref
// resolves against an alias rather than a binding. Stage 8 adds pathBindings
// (the PathBinding values collected from named-path patterns in this part —
// appended to bindings at build time) and pathMemberSink, a scratch pointer
// collectNode / collectEdge push shape-faithful PathMember entries onto while
// walking a named-path pattern part (nil outside a named path). Stage 9 adds
// unwindBindings (the UnwindBinding values collected from UNWIND clauses in
// this part — appended to bindings at build time, mirroring pathBindings).
type rawPart struct {
	bindings       []*rawBinding
	byVar          map[string]int
	returns        []query.ReturnItem
	returnsAll     bool
	refs           []varRef
	imported       map[string]query.Type
	pathBindings   []query.PathBinding
	pathMemberSink *[]query.PathMember
	unwindBindings []query.UnwindBinding
	effects        []query.Effect // Stage 12: per-part write clauses in walk order
}

func newRawPart() *rawPart {
	return &rawPart{byVar: map[string]int{}, imported: map[string]query.Type{}}
}

// rawBinding is a binding under construction: its variable, accumulated labels
// (ordered union, first appearance), kind, and — for an edge — its endpoints.
// nullable records the static, parser-time fact that the binding was first
// introduced inside an OPTIONAL MATCH clause (ADR 0006). Once set, later
// re-uses of the same variable in non-OPTIONAL clauses never demote it; that
// is the resolver's job (see gqlc-lqm). Stage 8: hops carries the var-length
// hop range (nil for single-hop; a var-length edge projects as list<edge>).
type rawBinding struct {
	variable   string
	labels     graph.LabelSet
	seen       map[string]bool // labels already merged, for the ordered union
	kind       graph.EntityKind
	source     query.Endpoint
	target     query.Endpoint
	nullable   bool
	undirected bool            // zero value false == directed; set true only on the undirected branch (inverted to keep existing literals zero-value-safe, see §4)
	hops       *query.EdgeHops // Stage 8: non-nil for a variable-length edge
}

// varRef is a use of a variable name that build() must resolve to a binding. An
// endpointRef must resolve to a node binding (an edge endpoint only references a
// node); any other ref (a return item) accepts either kind.
type varRef struct {
	name        string
	endpointRef bool
}

func newListener(ts *antlr.CommonTokenStream) *listener {
	return &listener{
		ts:       ts,
		byParam:  map[string]int{},
		approved: map[antlr.Tree]bool{},
	}
}

// fail records the first error and is idempotent thereafter: the error found
// first in walk order is the one Parse returns, and later failures are dropped.
func (l *listener) fail(err error) {
	if l.err == nil {
		l.err = err
	}
}

// SyntaxError records the first lexer/parser syntax error onto the same l.err
// channel as every collection error. ANTLR keeps reporting after the first, so
// fail() (idempotent) keeps only the first. Naming the offending token alongside
// line:column makes the location concrete for a query author scanning their source.
func (l *listener) SyntaxError(_ antlr.Recognizer, offendingSymbol any, line, column int, msg string, _ antlr.RecognitionException) {
	if tok, ok := offendingSymbol.(antlr.Token); ok && tok.GetText() != "" {
		l.fail(fmt.Errorf("syntax error at %d:%d near %q: %s", line, column, tok.GetText(), msg))
		return
	}
	l.fail(fmt.Errorf("syntax error at %d:%d: %s", line, column, msg))
}

// walk drives the ParseTreeWalker over the tree and returns the first error the
// listener recorded — turning ANTLR's void, side-effecting walk into an ordinary
// error-returning call. A syntax error recorded during lexing/parsing means the
// tree is unreliable, so we surface it and never walk.
func (l *listener) walk(tree antlr.Tree) error {
	if l.err != nil {
		return l.err
	}
	antlr.NewParseTreeWalker().Walk(l, tree)
	return l.err
}

// --- branch/part structure (spec §2) ---

// EnterOC_SingleQuery opens a new branch with one initial empty part and makes
// both current. It fires once per branch: the first branch, and each post-UNION
// branch (EnterOC_Union runs first and has already recorded the combinator).
// Stage 11 §1.2: inside EXISTS { oC_RegularQuery } the ANTLR walker fires
// EnterOC_SingleQuery for the subquery — we skip it so the outer branch/part
// pointers stay stable and no phantom branch enters l.branches.
func (l *listener) EnterOC_SingleQuery(*gen.OC_SingleQueryContext) {
	if l.subqueryDepth > 0 {
		return
	}
	part := newRawPart()
	br := &rawBranch{parts: []*rawPart{part}}
	l.branches = append(l.branches, br)
	l.curBranch = br
	l.curPart = part
}

// EnterOC_Union records the combinator joining the branch about to open to the
// current one: UnionAll if the ALL token is present, else UnionDistinct. It fires
// before the joined branch's EnterOC_SingleQuery, so the combinator precedes its
// branch and the i-th entry joins branch i+1 to branch i (spec §2). Stage 11
// §1.2: a UNION inside an EXISTS subquery is likewise suppressed so no phantom
// combinator enters the outer query's list.
func (l *listener) EnterOC_Union(c *gen.OC_UnionContext) {
	if l.subqueryDepth > 0 {
		return
	}
	kind := query.UnionDistinct
	if c.ALL() != nil {
		kind = query.UnionAll
	}
	l.combinators = append(l.combinators, kind)
}

// --- clause collection / rejections (spec §2/§3, category-grained sentinels) ---

// EnterOC_Match collects one MATCH or OPTIONAL MATCH clause's pattern and WHERE
// into the current part. Bindings first introduced inside an OPTIONAL clause are
// marked nullable (ADR 0006); the WHERE itself does not introduce bindings, so it
// reads parameters the same way in either case. Collection runs here, in walk
// order, so first appearance of a variable is the source order within the part.
func (l *listener) EnterOC_Match(c *gen.OC_MatchContext) {
	if l.subqueryDepth > 0 {
		return // Stage 11 §1.2: EXISTS { ... } suppresses inner clause collection.
	}
	optional := c.OPTIONAL() != nil
	l.collectPattern(c.OC_Pattern(), optional)
	if w := c.OC_Where(); w != nil {
		l.mineWhere(w)
	}
}

// EnterOC_With collects its projection into the current part (a WITH item is a
// RETURN item — they share oC_ProjectionBody), mines its optional WHERE for
// parameters, then CLOSES the current part and OPENS a fresh empty part in the
// current branch. The closed part's returns are the names it exports into the
// next part's scope (spec §4); Stage 6 also carries their result types so the
// next part's classifier can type a bare-alias RefProjection.
func (l *listener) EnterOC_With(c *gen.OC_WithContext) {
	if l.subqueryDepth > 0 {
		return // Stage 11 §1.2: EXISTS { ... } suppresses inner clause collection.
	}
	l.collectProjection(c.OC_ProjectionBody())
	if w := c.OC_Where(); w != nil {
		l.mineWhere(w)
	}
	if l.err != nil {
		return
	}
	closed := l.curPart
	part := newRawPart()
	part.imported = exportedTypes(closed)
	l.curBranch.parts = append(l.curBranch.parts, part)
	l.curPart = part
}

// exportedTypes computes the name → Stage-6 result type map the closed part
// exports into the next part's scope. WITH * (returnsAll) forwards every
// in-scope name — entity bindings by their node/edge kind (with a
// var-length edge exporting as list<edge>, Stage 8), path bindings by
// TypePath, UNWIND bindings by their recorded element type (Stage 9),
// and any prior imports verbatim — because the resolver expands *
// downstream (Stage 4 §4). Explicit items export each return item's Name
// against its Value.Type().
func exportedTypes(closed *rawPart) map[string]query.Type {
	out := map[string]query.Type{}
	if closed.returnsAll {
		for name, t := range closed.imported {
			out[name] = t
		}
		for _, rb := range closed.bindings {
			if rb.variable == "" {
				continue
			}
			switch rb.kind {
			case graph.Node:
				out[rb.variable] = query.TypeNode{}
			case graph.Edge:
				if rb.hops != nil {
					out[rb.variable] = query.NewTypeList(query.TypeEdge{})
				} else {
					out[rb.variable] = query.TypeEdge{}
				}
			}
		}
		for _, pb := range closed.pathBindings {
			out[pb.Variable()] = query.TypePath{}
		}
		for _, ub := range closed.unwindBindings {
			out[ub.Variable()] = ub.ElementType()
		}
		return out
	}
	for _, r := range closed.returns {
		t := r.Value.Type()
		if t == nil {
			t = query.TypeUnknown{}
		}
		out[r.Name] = t
	}
	return out
}

// EnterOC_Create collects the CREATE clause's pattern into the current part
// via the same collectPattern path MATCH uses (Stage 12 spec §4.1). Every
// binding the pattern introduces enters curPart.bindings verbatim; the delta
// [before..len(bindings)] captures which bindings this specific clause
// introduced, so the CreateEffect can record them for post-freeze codegen
// (which needs the create/match distinction per clause, not per binding).
// A named binding contributes its variable; an anonymous edge contributes an
// empty string. An anonymous node is not a binding (C3) and thus does not
// enter the CreateEffect's variables list — matching the read-side discipline.
// nullable is unconditionally false: openCypher has no OPTIONAL CREATE.
func (l *listener) EnterOC_Create(c *gen.OC_CreateContext) {
	if l.subqueryDepth > 0 {
		return // Stage 11 §1.6: writes inside EXISTS { ... } parse-accept; bucket-3 engine-side.
	}
	before := len(l.curPart.bindings)
	l.collectPattern(c.OC_Pattern(), false)
	if l.err != nil {
		return
	}
	var vars []string
	for i := before; i < len(l.curPart.bindings); i++ {
		vars = append(vars, l.curPart.bindings[i].variable)
	}
	l.curPart.effects = append(l.curPart.effects, query.NewCreateEffect(vars))
	l.writeSeen = true
}

func (l *listener) EnterOC_Merge(*gen.OC_MergeContext) {
	if l.subqueryDepth > 0 {
		return
	}
	l.fail(fmt.Errorf("%w: MERGE", ErrUnsupportedClause))
}

// EnterOC_Delete collects DELETE / DETACH DELETE targets (Stage 12 spec §4.2).
// Each expression in the target list is inspected: bare var / var.prop shapes
// enter DeleteEffect.Targets so the resolver can trace each to a schema entity
// kind; every other shape (list index, arithmetic, function call) is a rich
// target whose refs enter DeleteEffect.Refs and whose parameters record
// ExprUse{TypeUnknown, ExprInDeleteTarget} — TypeUnknown is the honest posture
// (the parameter's role is a delete target whose entity kind the parser
// cannot commit to schema-free); ExprInDeleteTarget names the position honestly
// as a consumer role distinct from a projection column (spec §4.2 amend).
// The Detach axis mirrors the DETACH token verbatim. Every DELETE expression
// the query names appears in EXACTLY ONE of Targets / Refs — never both, never
// neither — so no delete the query performs is silently absent from Effects.
func (l *listener) EnterOC_Delete(c *gen.OC_DeleteContext) {
	if l.subqueryDepth > 0 {
		return
	}
	detach := c.DETACH() != nil
	var targets, refs []query.Ref
	for _, e := range c.AllOC_Expression() {
		if nae := nonArithmeticAtom(e); nae != nil {
			if ref, ok := refFromNonArithmetic(nae); ok {
				targets = append(targets, ref)
				l.curPart.refs = append(l.curPart.refs, varRef{name: ref.Variable})
				continue
			}
		}
		_, expRefs, params := l.typeExpressionMining(e)
		refs = append(refs, expRefs...)
		for _, p := range params {
			name := parameterName(p)
			if name == "" {
				continue
			}
			l.addParameterUse(name, p, query.NewExprUse(query.TypeUnknown{}, query.ExprInDeleteTarget))
		}
	}
	l.curPart.effects = append(l.curPart.effects, query.NewDeleteEffect(targets, refs, detach))
	l.writeSeen = true
}

// EnterOC_Set collects one SET clause: one Effect per SetItem, dispatched by
// the item's grammar shape (Stage 12 spec §4.3). The four alternatives are
// propExpr = expr / var = expr / var += expr / var :Labels — the first three
// share a value expression that rides typeExpressionMining, so its Stage-6
// result type becomes the Effect's ValueType and its parameters record
// ExprUse{valueType, ExprInSetValue} — the typed-write contract, with the
// producer-side position distinct from a projection column (spec §1.5 amend).
// A nested propertyExpression LHS (n.a.b) rejects with
// ErrNestedPropertyTarget via collectSetItem.
func (l *listener) EnterOC_Set(c *gen.OC_SetContext) {
	if l.subqueryDepth > 0 {
		return
	}
	for _, item := range c.AllOC_SetItem() {
		l.collectSetItem(item)
		if l.err != nil {
			return
		}
	}
	l.writeSeen = true
}

// EnterOC_Remove collects one REMOVE clause: one Effect per RemoveItem
// (Stage 12 spec §4.4). Two alternatives: var :Labels → RemoveLabelsEffect,
// propertyExpression → RemovePropertyEffect. REMOVE takes no value expression,
// so no parameter mining runs.
func (l *listener) EnterOC_Remove(c *gen.OC_RemoveContext) {
	if l.subqueryDepth > 0 {
		return
	}
	for _, item := range c.AllOC_RemoveItem() {
		l.collectRemoveItem(item)
		if l.err != nil {
			return
		}
	}
	l.writeSeen = true
}

// EnterOC_Unwind collects the UNWIND clause into the current part as an
// UnwindBinding (Stage 9 spec §1.3). The AS variable enters the part's
// scope; the source expression is typed via the Stage-6 rich typer, and
// its element type (list<T>.Element(), else TypeUnknown) becomes the
// binding's ElementType. Every parameter under the source expression
// records an ExprUse{sourceType, ExprInProjection}, so no parameter is
// silently dropped.
func (l *listener) EnterOC_Unwind(c *gen.OC_UnwindContext) {
	if l.subqueryDepth > 0 {
		return
	}
	l.collectUnwind(c)
}

func (l *listener) EnterOC_InQueryCall(*gen.OC_InQueryCallContext) {
	if l.subqueryDepth > 0 {
		return
	}
	l.fail(fmt.Errorf("%w: CALL", ErrUnsupportedClause))
}

func (l *listener) EnterOC_StandaloneCall(*gen.OC_StandaloneCallContext) {
	if l.subqueryDepth > 0 {
		return
	}
	l.fail(fmt.Errorf("%w: CALL", ErrUnsupportedClause))
}

// EnterOC_Return collects the result columns into the current (final) part of
// the current branch. RETURN terminates a branch; WITH terminates an
// intermediate part (both share oC_ProjectionBody via collectProjection).
func (l *listener) EnterOC_Return(c *gen.OC_ReturnContext) {
	if l.subqueryDepth > 0 {
		return
	}
	l.collectProjection(c.OC_ProjectionBody())
}

// EnterOC_ExistentialSubquery opens a nested boolean-typed scope: outer
// variables remain visible inside (correlated references — the engine
// re-executing the original text honours them, ADR 0005) but inner
// bindings — the node/edge/path/unwind bindings any inner clause would
// otherwise write to l.curPart — must not leak into the outer part.
// The suppression counter is the enforcer (§1.2): every clause-collecting
// Enter* handler early-returns while it is positive, so no inner
// collection touches curPart's state.
//
// Parameter mining still runs at Stage 11 — the subquery body's clauses
// do not, so a $param inside EXISTS { MATCH (n) WHERE $threshold ... }
// would be silently dropped without this sweep. findParameters walks the
// whole subtree once at entry and records ExprUse{TypeBool,
// ExprInPredicate} against every $param, matching how mineWhere handles
// parameters at the WHERE level.
func (l *listener) EnterOC_ExistentialSubquery(c *gen.OC_ExistentialSubqueryContext) {
	l.subqueryDepth++
	for _, p := range findParameters(c) {
		if l.approved[p] {
			continue
		}
		name := parameterName(p)
		if name == "" {
			continue
		}
		l.addParameterUse(name, p, query.NewExprUse(query.TypeBool{}, query.ExprInPredicate))
	}
}

func (l *listener) ExitOC_ExistentialSubquery(*gen.OC_ExistentialSubqueryContext) {
	if l.subqueryDepth > 0 {
		l.subqueryDepth--
	}
}
