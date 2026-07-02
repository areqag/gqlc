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
// resolves against an alias rather than a binding.
type rawPart struct {
	bindings   []*rawBinding
	byVar      map[string]int
	returns    []query.ReturnItem
	returnsAll bool
	refs       []varRef
	imported   map[string]query.Type
}

func newRawPart() *rawPart {
	return &rawPart{byVar: map[string]int{}, imported: map[string]query.Type{}}
}

// rawBinding is a binding under construction: its variable, accumulated labels
// (ordered union, first appearance), kind, and — for an edge — its endpoints.
// nullable records the static, parser-time fact that the binding was first
// introduced inside an OPTIONAL MATCH clause (ADR 0006). Once set, later
// re-uses of the same variable in non-OPTIONAL clauses never demote it; that
// is the resolver's job (see gqlc-lqm).
type rawBinding struct {
	variable   string
	labels     graph.LabelSet
	seen       map[string]bool // labels already merged, for the ordered union
	kind       graph.EntityKind
	source     query.Endpoint
	target     query.Endpoint
	nullable   bool
	undirected bool // zero value false == directed; set true only on the undirected branch (inverted to keep existing literals zero-value-safe, see §4)
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
func (l *listener) EnterOC_SingleQuery(*gen.OC_SingleQueryContext) {
	part := newRawPart()
	br := &rawBranch{parts: []*rawPart{part}}
	l.branches = append(l.branches, br)
	l.curBranch = br
	l.curPart = part
}

// EnterOC_Union records the combinator joining the branch about to open to the
// current one: UnionAll if the ALL token is present, else UnionDistinct. It fires
// before the joined branch's EnterOC_SingleQuery, so the combinator precedes its
// branch and the i-th entry joins branch i+1 to branch i (spec §2).
func (l *listener) EnterOC_Union(c *gen.OC_UnionContext) {
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
// in-scope name — bindings by their node/edge kind and any prior imports
// verbatim — because the resolver expands * downstream (Stage 4 §4). Explicit
// items export each return item's Name against its Value.Type().
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
				out[rb.variable] = query.TypeEdge{}
			}
		}
		return out
	}
	for _, r := range closed.returns {
		out[r.Name] = projectionType(r.Value)
	}
	return out
}

// projectionType reads a Projection's Stage-6 result type via the accessor each
// variant now carries; a nil interface value (not reachable from a listener-built
// projection) falls back to TypeUnknown.
func projectionType(p query.Projection) query.Type {
	type typed interface{ Type() query.Type }
	if t, ok := p.(typed); ok {
		if got := t.Type(); got != nil {
			return got
		}
	}
	return query.TypeUnknown{}
}

func (l *listener) EnterOC_Create(*gen.OC_CreateContext) {
	l.fail(fmt.Errorf("%w: CREATE", ErrUnsupportedClause))
}

func (l *listener) EnterOC_Merge(*gen.OC_MergeContext) {
	l.fail(fmt.Errorf("%w: MERGE", ErrUnsupportedClause))
}

func (l *listener) EnterOC_Delete(*gen.OC_DeleteContext) {
	l.fail(fmt.Errorf("%w: DELETE", ErrUnsupportedClause))
}

func (l *listener) EnterOC_Set(*gen.OC_SetContext) {
	l.fail(fmt.Errorf("%w: SET", ErrUnsupportedClause))
}

func (l *listener) EnterOC_Remove(*gen.OC_RemoveContext) {
	l.fail(fmt.Errorf("%w: REMOVE", ErrUnsupportedClause))
}

func (l *listener) EnterOC_Unwind(*gen.OC_UnwindContext) {
	l.fail(fmt.Errorf("%w: UNWIND", ErrUnsupportedClause))
}

func (l *listener) EnterOC_InQueryCall(*gen.OC_InQueryCallContext) {
	l.fail(fmt.Errorf("%w: CALL", ErrUnsupportedClause))
}

func (l *listener) EnterOC_StandaloneCall(*gen.OC_StandaloneCallContext) {
	l.fail(fmt.Errorf("%w: CALL", ErrUnsupportedClause))
}

// EnterOC_RangeLiteral rejects a variable-length relationship ([*..]); the range
// literal appears only inside a relationship detail.
func (l *listener) EnterOC_RangeLiteral(*gen.OC_RangeLiteralContext) {
	l.fail(fmt.Errorf("%w: variable-length relationship", ErrUnsupportedPattern))
}

// EnterOC_Return collects the result columns into the current (final) part of
// the current branch. RETURN terminates a branch; WITH terminates an
// intermediate part (both share oC_ProjectionBody via collectProjection).
func (l *listener) EnterOC_Return(c *gen.OC_ReturnContext) {
	l.collectProjection(c.OC_ProjectionBody())
}
