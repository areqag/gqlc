package gql

import (
	"fmt"
	"testing"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stretchr/testify/require"
)

const (
	// statementRule is the corpus' subject. Requiring every file to enter it is
	// what stops a file that parses as some other statement from passing every
	// assertion while exercising none of the graph type grammar.
	statementRule = "createGraphTypeStatement"
	// elementTypeRule is the grammar rule one node or edge type declaration
	// produces; counting its contexts gives the silent-drop guard's expected
	// element count.
	elementTypeRule = "elementTypeSpecification"
	// nestedSpecRule delimits a graph type body. closedGraphReferenceValueType
	// (GQL.g4:1926) admits a whole nested specification as a property value type,
	// so both it and elementTypeRule recur, and depth is what distinguishes an
	// element of this graph type from an element of one nested inside a property.
	nestedSpecRule = "nestedGraphTypeSpecification"
)

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

	// elementTypes is the number of element type declarations belonging to the
	// statement's own graph type, counted here rather than read back from the
	// parser under test so the expected count is independent of what that parser
	// collected. Declarations inside a nested graph-typed property are excluded:
	// they are elements of that graph type, not of this one.
	elementTypes int

	// declaresElements records that the graph type source is the nested-body
	// alternative. COPY OF and LIKE sources declare no elements inline at all, so
	// for them the element count carries no information and the silent-drop guard
	// has nothing to compare against.
	declaresElements bool
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

	switch name {
	case elementTypeRule:
		if c.nestingDepth(ctx) == 1 {
			c.elementTypes++
		}
	case nestedSpecRule:
		if c.nestingDepth(ctx) == 0 {
			c.declaresElements = true
		}
	}
}

// nestingDepth is the number of nestedSpecRule contexts above ctx.
func (c *coverage) nestingDepth(ctx antlr.ParserRuleContext) int {
	depth := 0
	for parent := ctx.GetParent(); parent != nil; parent = parent.GetParent() {
		rule, ok := parent.(antlr.ParserRuleContext)
		if !ok {
			break
		}
		if c.ruleNames[rule.GetRuleIndex()] == nestedSpecRule {
			depth++
		}
	}
	return depth
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

// merge folds another file's coverage into c. elementTypes and declaresElements
// describe one statement, so they are deliberately not accumulated.
func (c *coverage) merge(other *coverage) {
	for rule := range other.rules {
		c.rules[rule] = true
	}
	for token := range other.tokens {
		c.tokens[token] = true
	}
}

// TestCoverageElementCount pins the measurement every other corpus assertion is
// compared against. The nested cases are the ones that matter: a count that
// ignored nesting depth would demand the inner graph's node types from the outer
// schema and so fail a correct implementation.
func TestCoverageElementCount(t *testing.T) {
	for _, tc := range []struct {
		name             string
		src              string
		elements         int
		declaresElements bool
	}{
		{"one node", "CREATE GRAPH TYPE t { (:A) }", 1, true},
		{"node and edge", "CREATE GRAPH TYPE t { (:A), (:B), (:A)-[:R]->(:B) }", 3, true},
		{"graph typed property", "CREATE GRAPH TYPE t { (:A { p :: GRAPH { (:B) } }) }", 1, true},
		{"graph typed property with edge", "CREATE GRAPH TYPE t { (:A { p :: GRAPH { (:B), (:C), (:B)-[:R]->(:C) } }) }", 1, true},
		{"twice nested", "CREATE GRAPH TYPE t { (:A { p :: GRAPH { (:B { q :: GRAPH { (:C) } }) } }) }", 1, true},
		{"copy of", "CREATE GRAPH TYPE t COPY OF CURRENT_SCHEMA/gt", 0, false},
		{"like", "CREATE GRAPH TYPE t LIKE g", 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := measureCoverage(t, tc.src)
			require.Equal(t, tc.elements, got.elementTypes)
			require.Equal(t, tc.declaresElements, got.declaresElements)
		})
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

// walkCoverage parses src and walks the resulting tree, returning what the tree
// entered and any syntax errors. It parses independently of Parse because the
// obligation must be measured against the grammar's tree, not against the part of
// it the listener under test chose to visit.
func walkCoverage(t *testing.T, src string) (*coverage, []string) {
	t.Helper()

	lex, p := newGrammarParser(src)
	errs := &syntaxErrors{}
	lex.RemoveErrorListeners()
	lex.AddErrorListener(errs)
	p.RemoveErrorListeners()
	p.AddErrorListener(errs)

	tree := p.GqlProgram()
	c := newCoverage(p.GetRuleNames(), p.GetSymbolicNames())
	antlr.ParseTreeWalkerDefault.Walk(c, tree)
	return c, errs.msgs
}

// measureCoverage is walkCoverage plus the two things every corpus file owes: it
// is valid ISO GQL, and it is a graph type statement.
func measureCoverage(t *testing.T, src string) *coverage {
	t.Helper()

	c, errs := walkCoverage(t, src)
	require.Empty(t, errs, "corpus files must be syntactically valid ISO GQL")
	require.True(t, c.rules[statementRule],
		"parsed with no syntax error but as some other statement, so it exercises no graph type grammar; the usual cause is AS before COPY OF")
	return c
}
