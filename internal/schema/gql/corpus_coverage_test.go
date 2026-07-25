package gql

import (
	"fmt"
	"testing"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stretchr/testify/require"
)

// elementTypeRule is the grammar rule one node or edge type declaration produces;
// counting its contexts gives the silent-drop guard's expected element count.
const elementTypeRule = "elementTypeSpecification"

// coverage records which grammar rules and tokens a parse tree entered. It
// implements the bare antlr.ParseTreeListener rather than the generated
// GQLListener so that nothing in it names a grammar rule: the measurement cannot
// drift from the grammar the way a hand-written listener does, which is the
// defect class this corpus exists to catch.
type coverage struct {
	ruleNames     []string
	symbolicNames []string

	rules  map[string]bool
	tokens map[string]bool

	// elementTypes is the number of elementTypeSpecification contexts in the
	// tree, counted here rather than read back from the parser under test so the
	// expected count is independent of what that parser collected.
	elementTypes int
}

func newCoverage(ruleNames, symbolicNames []string) *coverage {
	return &coverage{
		ruleNames:     ruleNames,
		symbolicNames: symbolicNames,
		rules:         make(map[string]bool),
		tokens:        make(map[string]bool),
	}
}

func (c *coverage) EnterEveryRule(ctx antlr.ParserRuleContext) {
	name := c.ruleNames[ctx.GetRuleIndex()]
	c.rules[name] = true
	if name == elementTypeRule {
		c.elementTypes++
	}
}

func (c *coverage) VisitTerminal(node antlr.TerminalNode) {
	// EOF is token type -1, and a token defined only as a literal has an empty
	// symbolic name. Neither is part of the obligation.
	tokenType := node.GetSymbol().GetTokenType()
	if tokenType < 0 || tokenType >= len(c.symbolicNames) {
		return
	}
	if name := c.symbolicNames[tokenType]; name != "" {
		c.tokens[name] = true
	}
}

func (c *coverage) ExitEveryRule(antlr.ParserRuleContext) {}

func (c *coverage) VisitErrorNode(antlr.ErrorNode) {}

// merge folds another file's coverage into c. elementTypes is per-file and is
// deliberately not accumulated.
func (c *coverage) merge(other *coverage) {
	for rule := range other.rules {
		c.rules[rule] = true
	}
	for token := range other.tokens {
		c.tokens[token] = true
	}
}

// syntaxErrors collects every lexer and parser syntax error. Corpus files are
// valid ISO GQL by definition, so any entry here is a broken corpus file.
type syntaxErrors struct {
	*antlr.DefaultErrorListener
	msgs []string
}

func (s *syntaxErrors) SyntaxError(_ antlr.Recognizer, _ any, line, column int, msg string, _ antlr.RecognitionException) {
	s.msgs = append(s.msgs, fmt.Sprintf("%d:%d: %s", line, column, msg))
}

// measureCoverage parses src and walks the resulting tree. It parses
// independently of Parse because the obligation must be measured against the
// grammar's tree, not against the part of it the listener under test chose to
// visit.
func measureCoverage(t *testing.T, src string) *coverage {
	t.Helper()

	lex, p := newGrammarParser(src)
	errs := &syntaxErrors{}
	lex.RemoveErrorListeners()
	lex.AddErrorListener(errs)
	p.RemoveErrorListeners()
	p.AddErrorListener(errs)

	tree := p.GqlProgram()
	require.Empty(t, errs.msgs, "corpus files must be syntactically valid ISO GQL")

	c := newCoverage(p.GetRuleNames(), p.GetSymbolicNames())
	antlr.ParseTreeWalkerDefault.Walk(c, tree)
	return c
}
