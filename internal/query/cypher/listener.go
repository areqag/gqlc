package cypher

import (
	"fmt"

	"github.com/antlr4-go/antlr/v4"

	"github.com/antranig-yeretzian/gqlc/internal/grammar/cypher/gen"
)

// listener is the single error sink and (in run B) collector for a parse: it
// captures the first lexer/parser syntax error and, once the collection pass is
// built, the errors raised during the walk — both funnelling into l.err. The
// walk cannot be stopped mid-traversal (ADR 0001); fail() keeps the first error
// and the caller discards the result once one is set. Mirrors internal/schema/gql.
//
// Run A skeleton: no Enter* collection handlers and no build() yet. The walk runs
// (so syntax errors are still surfaced) but produces nothing; Parse returns
// errNotImplemented after a clean walk.
type listener struct {
	*gen.BaseCypherListener
	*antlr.DefaultErrorListener

	ts *antlr.CommonTokenStream

	err error
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
