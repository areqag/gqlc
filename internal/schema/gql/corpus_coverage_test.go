package gql_test

import (
	"fmt"
	"reflect"
	"strings"
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
	alts          *alternativeIndex

	rules        map[string]bool
	tokens       map[string]bool
	alternatives map[string]bool
	// shapes are the distinct direct-child sequences seen per rule. Recorded, not
	// gated: a shape appearing for the first time is usually a new corpus file
	// doing its job, so this is a report until the corpus stops growing
	// (gqlc-h9n.13).
	shapes map[string]map[string]bool
	// unattributed holds the rules whose alternative could not be identified. A
	// miss is not benign — it means a file exercised something the harness cannot
	// account for — so callers that own valid input turn this into a failure.
	unattributed []string

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

func newCoverage(ruleNames, symbolicNames []string, alts *alternativeIndex) *coverage {
	return &coverage{
		ruleNames:     ruleNames,
		symbolicNames: symbolicNames,
		alts:          alts,
		rules:         make(map[string]bool),
		tokens:        make(map[string]bool),
		alternatives:  make(map[string]bool),
		shapes:        make(map[string]map[string]bool),
	}
}

func (c *coverage) EnterEveryRule(ctx antlr.ParserRuleContext) {
	name := c.ruleNames[ctx.GetRuleIndex()]
	c.rules[name] = true

	children := c.children(ctx)
	if c.shapes[name] == nil {
		c.shapes[name] = make(map[string]bool)
	}
	c.shapes[name][strings.Join(children, " ")] = true

	switch tag, err := c.alts.tag(name, reflect.TypeOf(ctx).Elem().Name(), children); {
	case err != nil:
		c.unattributed = append(c.unattributed, err.Error())
	case tag != "":
		c.alternatives[tag] = true
	}

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

// children is ctx's direct children named the way a grammar alternative names
// them: a rule name for a subtree, a token's symbolic name for a terminal. An
// error node deliberately yields no name, so a tree built by error recovery
// matches no alternative rather than being attributed to a plausible one.
func (c *coverage) children(ctx antlr.ParserRuleContext) []string {
	names := make([]string, 0, ctx.GetChildCount())
	for i := range ctx.GetChildCount() {
		var name string
		switch child := ctx.GetChild(i).(type) {
		case antlr.ErrorNode:
		case antlr.TerminalNode:
			name = c.tokenName(child)
		case antlr.ParserRuleContext:
			name = c.ruleNames[child.GetRuleIndex()]
		}
		names = append(names, name)
	}
	return names
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
	if name := c.tokenName(node); name != "" {
		c.tokens[name] = true
	}
}

// tokenName is the name a grammar rule refers to a terminal by. EOF has token type
// -1 and so no entry in the symbolic table, but rules do name it, so a child
// sequence has to as well. A token defined only as a literal has no symbolic name
// at all and yields "", which no alternative's element text can match either —
// deliberately, since cleanRuleBody strips literals.
func (c *coverage) tokenName(node antlr.TerminalNode) string {
	switch tokenType := node.GetSymbol().GetTokenType(); {
	case tokenType == antlr.TokenEOF:
		return "EOF"
	case tokenType < 0 || tokenType >= len(c.symbolicNames):
		return ""
	default:
		return c.symbolicNames[tokenType]
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
	for tag := range other.alternatives {
		c.alternatives[tag] = true
	}
	for rule, shapes := range other.shapes {
		if c.shapes[rule] == nil {
			c.shapes[rule] = make(map[string]bool, len(shapes))
		}
		for shape := range shapes {
			c.shapes[rule][shape] = true
		}
	}
	c.unattributed = append(c.unattributed, other.unattributed...)
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

// coverageT is the part of *testing.T the measurement helpers use. It is an
// interface only so that TestMeasureCoverageRejects can watch a guard fire; every
// other caller passes a *testing.T.
type coverageT interface {
	require.TestingT
	Helper()
}

// walkCoverage parses src and walks the resulting tree, returning what the tree
// entered and any syntax errors. It parses independently of Parse because the
// obligation must be measured against the grammar's tree, not against the part of
// it the listener under test chose to visit.
func walkCoverage(t coverageT, src string) (*coverage, []string) {
	t.Helper()

	lex, p := newGrammarParser(src)
	errs := &syntaxErrors{}
	lex.RemoveErrorListeners()
	lex.AddErrorListener(errs)
	p.RemoveErrorListeners()
	p.AddErrorListener(errs)

	tree := p.GqlProgram()
	c := newCoverage(p.GetRuleNames(), p.GetSymbolicNames(), grammarObligation(t).alternatives)
	antlr.ParseTreeWalkerDefault.Walk(c, tree)
	return c, errs.msgs
}

// measureCoverage is walkCoverage plus the three things every corpus file owes: it
// is valid ISO GQL, it is a graph type statement, and every alternative it took can
// be named. The last is what keeps the third gate honest: a rule the index cannot
// attribute would otherwise contribute nothing and look like an authoring gap.
func measureCoverage(t coverageT, src string) *coverage {
	t.Helper()

	c, errs := walkCoverage(t, src)
	require.Empty(t, errs, "corpus files must be syntactically valid ISO GQL")
	require.True(t, c.rules[statementRule],
		"parsed with no syntax error but as some other statement, so it exercises no graph type grammar; the usual cause is AS before COPY OF")
	require.Empty(t, c.unattributed,
		"the alternative index could not name what this file parsed as")
	return c
}

// TestMeasureCoverageRejects pins all three guards above, none of which a corpus
// file can trip today — which is what leaves them free to be deleted without any
// test noticing. Each witness passes the other two, so nothing but the guard under
// test can account for the rejection.
//
// The statement guard's witness names no graph type, so TYPE is read as the name of
// a graph and the file is a valid CREATE GRAPH. Every element type it declares is
// then measured against a statement the corpus is not about. The attribution
// guard's witness reaches numericValueExpression, whose first alternative is
// `sign = (PLUS_SIGN | MINUS_SIGN) ...`; parseEBNF refuses that element syntax
// rather than approximating it, so the index cannot name what the file parsed as.
//
// The syntax guard is the fail-fast the other two rest on: a file that does not
// parse still yields a tree, built by ANTLR's error recovery, and measuring it
// scores grammar the file never contained. TestFabricatedTokenDoesNotScore and
// TestFabricatedTokenDoesNotAttribute defend the collector against exactly that,
// and they are the second line — this guard is what keeps a broken file from being
// measured at all.
func TestMeasureCoverageRejects(t *testing.T) {
	t.Run("a file that does not parse", func(t *testing.T) {
		// An unterminated body: recovery supplies the missing RIGHT_BRACE at EOF, so
		// everything before it walks and attributes cleanly and the other two guards
		// pass. Truncating mid-element instead would leave a blanked error node whose
		// parent matches no alternative, which the attribution guard rejects first.
		const src = "CREATE PROPERTY GRAPH TYPE t AS { (:A { id :: INT }) "

		got, errs := walkCoverage(t, src)
		require.NotEmpty(t, errs, "the premise is that this spelling is not valid GQL")
		require.True(t, got.rules[statementRule], "so only the syntax guard can reject it")
		require.Empty(t, got.unattributed, "so only the syntax guard can reject it")

		require.Contains(t, measurementFailure(t, src), "must be syntactically valid ISO GQL")
	})

	t.Run("a file that parses as some other statement", func(t *testing.T) {
		const src = "CREATE GRAPH TYPE { (:A) }"

		got, errs := walkCoverage(t, src)
		require.Empty(t, errs, "the premise is that this spelling is valid GQL")
		require.Empty(t, got.unattributed, "so only the statement guard can reject it")

		require.Contains(t, measurementFailure(t, src), "exercises no graph type grammar")
	})

	t.Run("a file the alternative index cannot name", func(t *testing.T) {
		const src = "CREATE GRAPH TYPE t LIKE (SQRT(1))"

		got, errs := walkCoverage(t, src)
		require.Empty(t, errs, "the premise is that this spelling is valid GQL")
		require.True(t, got.rules[statementRule], "so only the attribution guard can reject it")

		require.Contains(t, measurementFailure(t, src), "could not name what this file parsed as")
	})
}

// measurementFailure is what measureCoverage reported when it rejected src, and a
// failure of t when it accepted it.
func measurementFailure(t *testing.T, src string) string {
	t.Helper()

	rec := &recordingT{}
	func() {
		defer func() {
			r := recover()
			if _, ok := r.(failedNow); r != nil && !ok {
				panic(r)
			}
		}()
		measureCoverage(rec, src)
	}()

	require.NotEmpty(t, rec.msgs, "measureCoverage accepted a source that measures nothing it claims to")
	return strings.Join(rec.msgs, "\n")
}

// recordingT collects what a require reported instead of failing the test, so that
// a guard asserting on its caller's behalf can be tested rather than only run.
//
// FailNow must not return. It panics rather than calling runtime.Goexit because
// Goexit needs the measurement on a goroutine of its own, and an assertion inside a
// goroutine is indistinguishable — to a reader and to testifylint — from the mistake
// where a failure is reported to a test that has already finished.
type recordingT struct{ msgs []string }

// failedNow is the panic value, distinct so that a panic from anywhere else in the
// measurement is re-raised rather than read as a rejection.
type failedNow struct{}

func (r *recordingT) Errorf(format string, args ...any) {
	r.msgs = append(r.msgs, fmt.Sprintf(format, args...))
}

func (r *recordingT) FailNow() { panic(failedNow{}) }

func (r *recordingT) Helper() {}
