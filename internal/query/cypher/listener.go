package cypher

import (
	"fmt"

	"github.com/antlr4-go/antlr/v4"

	"github.com/antranig-yeretzian/gqlc/internal/grammar/cypher/gen"
	"github.com/antranig-yeretzian/gqlc/internal/graph"
	"github.com/antranig-yeretzian/gqlc/internal/query"
)

// listener is the single error sink and collector for a parse: it captures the
// first lexer/parser syntax error and the errors raised during the walk — both
// funnelling into l.err. The walk cannot be stopped mid-traversal (ADR 0001);
// fail() keeps the first error and Parse discards the result once one is set, so
// an Enter* that runs after the first error is harmless. Mirrors schema/gql.
//
// One collection pass + build(): the Enter* handlers collect into ordered slices
// plus a variable lookup map, and build() assembles query.Query at end of walk.
// There is no schema-style resolve() second pass — endpoints record a variable
// name and labels live on the binding, so there is no parse-time endpoint->type
// lookup. build() does a self-consistency validation, not a resolution.
type listener struct {
	*gen.BaseCypherListener
	*antlr.DefaultErrorListener

	ts *antlr.CommonTokenStream

	// bindings are the collected bindings in first-appearance order. A named
	// binding has an index in byVar so repeat occurrences merge into it; an
	// anonymous edge has no entry and is appended directly.
	bindings []*rawBinding
	byVar    map[string]int

	// params are the collected parameters in first-appearance order, indexed by
	// name in byParam so repeat uses accumulate onto one Parameter.
	params   []*query.Parameter
	byParam  map[string]int
	approved map[antlr.Tree]bool // oC_Parameter nodes mined into a Use

	// returns are the collected result columns in source order.
	returns []query.ReturnItem

	// refs are every variable reference build() must check against a binding:
	// return items, parameter uses, and edge endpoints. Collected with their kind
	// so build() can raise ErrUnboundVariable / ErrVariableKindConflict.
	refs []varRef

	err error
}

// rawBinding is a binding under construction: its variable, accumulated labels
// (ordered union, first appearance), kind, and — for an edge — its endpoints.
// nullable records the static, parser-time fact that the binding was first
// introduced inside an OPTIONAL MATCH clause (ADR 0006). Once set, later
// re-uses of the same variable in non-OPTIONAL clauses never demote it; that
// is the resolver's job (see gqlc-lqm).
type rawBinding struct {
	variable string
	labels   graph.LabelSet
	seen     map[string]bool // labels already merged, for the ordered union
	kind     graph.EntityKind
	source   query.Endpoint
	target   query.Endpoint
	nullable bool
}

// varRef is a use of a variable name that build() must resolve to a binding. An
// endpointRef must resolve to a node binding (an edge endpoint only references a
// node); any other ref (a return item or parameter use) accepts either kind.
type varRef struct {
	name        string
	endpointRef bool
}

func newListener(ts *antlr.CommonTokenStream) *listener {
	return &listener{
		ts:       ts,
		byVar:    map[string]int{},
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

// --- clause rejections (spec §3, category-grained sentinels) ---

// EnterOC_Match collects one MATCH or OPTIONAL MATCH clause's pattern and
// WHERE. Bindings first introduced inside an OPTIONAL clause are marked
// nullable (ADR 0006); the WHERE itself does not introduce bindings, so it
// reads parameters the same way in either case. Collection runs here, in
// walk order, so first appearance of a variable/parameter is the source
// order across all MATCHes.
func (l *listener) EnterOC_Match(c *gen.OC_MatchContext) {
	optional := c.OPTIONAL() != nil
	l.collectPattern(c.OC_Pattern(), optional)
	if w := c.OC_Where(); w != nil {
		l.mineWhere(w)
	}
}

func (l *listener) EnterOC_With(*gen.OC_WithContext) {
	l.fail(fmt.Errorf("%w: WITH", ErrUnsupportedClause))
}

func (l *listener) EnterOC_Union(*gen.OC_UnionContext) {
	l.fail(fmt.Errorf("%w: UNION", ErrUnsupportedClause))
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

// EnterOC_Return collects the result columns. RETURN is the read core's single
// projection; WITH (the other projection) is already rejected.
func (l *listener) EnterOC_Return(c *gen.OC_ReturnContext) {
	l.collectProjection(c.OC_ProjectionBody())
}
