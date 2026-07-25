package gql

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/grammar/gql/gen"
)

const grammarPath = "../../grammar/gql/GQL.g4"

// coverageRoots are the grammar rules a CREATE GRAPH TYPE statement can enter.
// The corpus' obligation is their transitive closure, so the obligation tracks
// the grammar rather than a hand-maintained list of constructs.
var coverageRoots = []string{
	statementRule,
	"graphTypeSource",
	nestedSpecRule,
}

// coverageCuts are frontier rules: still required to be covered, but not
// descended into.
//
// graphExpression is reached by `graphTypeLikeGraph : LIKE graphExpression` and
// drags in 545 of the grammar's 574 rules — the whole query and expression
// surface, which is out of scope here and has its own corpus (the vendored
// openCypher TCK).
//
// nonReservedWords is reached by `regularIdentifier : REGULAR_IDENTIFIER |
// nonReservedWords`: ~200 keyword tokens usable as an identifier. That is
// identifier lexis, not a schema construct.
//
// A cut still has to be entered by some corpus file, so a cut that stops being
// reachable fails rather than silently masking a subtree. Adding a third cut
// needs the grammar path that justifies it.
var coverageCuts = []string{"graphExpression", "nonReservedWords"}

// The obligation sizes are pinned because the gate below can only catch the
// closure growing (a new rule is uncovered until a corpus file enters it). A
// closure that shrinks — a rule deleted, a reference dropped, a cut swallowing
// more than it used to — would otherwise pass silently while testing less. A
// deliberate grammar change updates these and re-reads the uncovered list.
const (
	wantObligationRules  = 133
	wantObligationTokens = 133
)

// The grammar is scanned as text rather than through the generated parser's ATN
// because the ATN has no notion of the source-level rule references this closure
// walks: it is a flattened state machine, and reconstructing "rule x names rule
// y" from it is strictly harder than reading the .g4.
var (
	reLineComment  = regexp.MustCompile(`//[^\n]*`)
	reBlockComment = regexp.MustCompile(`(?s)/\*.*?\*/`)
	reRuleHead     = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*)\s*(options\s*\{[^}]*\})?\s*$`)
	reRuleEnd      = regexp.MustCompile(`^\s*;\s*$`)
	reLiteral      = regexp.MustCompile(`'(\\.|[^'])*'`)
	reAltLabel     = regexp.MustCompile(`#\s*[A-Za-z_][A-Za-z0-9_]*`)
	reOptions      = regexp.MustCompile(`\boptions\s*\{[^}]*\}`)
	reElementLabel = regexp.MustCompile(`\b([A-Za-z_][A-Za-z0-9_]*)\s*=`)
	reIdentifier   = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*`)
)

// scanGrammarRules returns every parser rule in GQL.g4 as name -> body text. A
// rule block is a line holding nothing but the rule name, then body lines up to
// a line holding nothing but a semicolon.
func scanGrammarRules(t *testing.T) map[string]string {
	t.Helper()

	src, err := os.ReadFile(grammarPath)
	require.NoError(t, err)

	text := reLineComment.ReplaceAllString(string(src), "")
	text = reBlockComment.ReplaceAllString(text, "")

	rules := make(map[string]string)
	name := ""
	var body []string
	for _, line := range strings.Split(text, "\n") {
		if name == "" {
			if m := reRuleHead.FindStringSubmatch(line); m != nil {
				name = m[1]
				body = nil
			}
			continue
		}
		body = append(body, line)
		if reRuleEnd.MatchString(line) {
			rules[name] = strings.Join(body, "\n")
			name = ""
		}
	}

	parserRules := make(map[string]string, len(rules))
	for rule, ruleBody := range rules {
		if isRuleName(rule) {
			parserRules[rule] = ruleBody
		}
	}
	require.NotEmpty(t, parserRules)
	return parserRules
}

// isRuleName reports whether an identifier is an ANTLR parser rule rather than a
// token: parser rules start lower-case, tokens upper-case.
func isRuleName(id string) bool {
	return id[0] >= 'a' && id[0] <= 'z'
}

// grammarRefs splits a rule body into the parser rules and the tokens it names.
// Quoted literals, alternative labels (`# Alt`), options blocks and element
// labels (`name=rule`) are not references and are dropped first.
func grammarRefs(body string, parserRules map[string]string) (rules, tokens map[string]bool) {
	body = reLiteral.ReplaceAllString(body, " ")
	body = reAltLabel.ReplaceAllString(body, " ")
	body = reOptions.ReplaceAllString(body, " ")

	labels := make(map[string]bool)
	for _, m := range reElementLabel.FindAllStringSubmatch(body, -1) {
		labels[m[1]] = true
	}

	rules, tokens = make(map[string]bool), make(map[string]bool)
	for _, id := range reIdentifier.FindAllString(body, -1) {
		switch {
		case labels[id]:
		case isRuleName(id):
			if _, ok := parserRules[id]; ok {
				rules[id] = true
			}
		default:
			tokens[id] = true
		}
	}
	return rules, tokens
}

// obligation is what the corpus must enter: every reachable parser rule, and
// every token named by a reachable non-frontier rule.
type obligation struct {
	rules  map[string]bool
	tokens map[string]bool
	// tokenRefs maps a token to the reachable rules naming it, which is how an
	// uncovered token is reported against a rule an author can write a file for.
	tokenRefs map[string][]string
}

// grammarObligation computes the reachability closure from coverageRoots,
// stopping at coverageCuts.
func grammarObligation(t *testing.T) obligation {
	t.Helper()

	parserRules := scanGrammarRules(t)

	cuts := make(map[string]bool, len(coverageCuts))
	for _, cut := range coverageCuts {
		require.Contains(t, parserRules, cut, "frontier cut is not a grammar rule")
		cuts[cut] = true
	}

	rules := make(map[string]bool, len(coverageRoots))
	queue := make([]string, 0, len(coverageRoots))
	for _, root := range coverageRoots {
		require.Contains(t, parserRules, root, "coverage root is not a grammar rule")
		rules[root] = true
		queue = append(queue, root)
	}

	tokens := make(map[string]bool)
	tokenRefs := make(map[string][]string)
	cutsReached := make(map[string]bool, len(cuts))
	for len(queue) > 0 {
		rule := queue[0]
		queue = queue[1:]
		if cuts[rule] {
			cutsReached[rule] = true
			continue
		}

		refRules, refTokens := grammarRefs(parserRules[rule], parserRules)
		for token := range refTokens {
			tokens[token] = true
			tokenRefs[token] = append(tokenRefs[token], rule)
		}
		for ref := range refRules {
			if !rules[ref] {
				rules[ref] = true
				queue = append(queue, ref)
			}
		}
	}

	for _, cut := range coverageCuts {
		require.True(t, cutsReached[cut],
			"frontier cut %q is no longer reachable: drop it from coverageCuts", cut)
	}
	for _, refs := range tokenRefs {
		sort.Strings(refs)
	}

	return obligation{rules: rules, tokens: tokens, tokenRefs: tokenRefs}
}

var reSectionHeader = regexp.MustCompile(`^//\s*(\d+(?:\.\d+)?\s+\S.*?)\s*$`)

// scanRuleSections maps each parser rule to the ISO clause heading it is declared
// under (`// 18.3 <edge type specification>`). Unlike scanGrammarRules this reads
// the raw text, because the headings are comments; a commented-out rule block
// cannot be mistaken for a rule head since a `//` line matches neither pattern.
func scanRuleSections(t *testing.T) map[string]string {
	t.Helper()

	src, err := os.ReadFile(grammarPath)
	require.NoError(t, err)

	sections := make(map[string]string)
	heading := ""
	inRule := false
	for _, line := range strings.Split(string(src), "\n") {
		switch {
		case reSectionHeader.MatchString(line):
			heading = reSectionHeader.FindStringSubmatch(line)[1]
		case inRule:
			inRule = !reRuleEnd.MatchString(line)
		default:
			if m := reRuleHead.FindStringSubmatch(line); m != nil {
				sections[m[1]] = heading
				inRule = true
			}
		}
	}
	require.NotEmpty(t, sections)
	return sections
}

// TestGrammarObligation pins the closure's size and cross-checks every name in
// it against the generated parser's own tables. The cross-check is what proves
// the text scan reads the grammar the way ANTLR does: a scanner that picked up a
// stray identifier, or missed a rename, produces a name the parser does not know.
func TestGrammarObligation(t *testing.T) {
	got := grammarObligation(t)

	require.Len(t, got.rules, wantObligationRules,
		"reachable rule count changed; re-read the grammar before repinning")
	require.Len(t, got.tokens, wantObligationTokens,
		"reachable token count changed; re-read the grammar before repinning")

	ruleNames, symbolicNames := parserNameTables()
	known := make(map[string]bool, len(ruleNames)+len(symbolicNames))
	for _, name := range ruleNames {
		known[name] = true
	}
	for rule := range got.rules {
		require.True(t, known[rule], "scanned rule %q is not a rule of the generated parser", rule)
	}

	known = make(map[string]bool, len(symbolicNames))
	for _, name := range symbolicNames {
		known[name] = true
	}
	for token := range got.tokens {
		require.True(t, known[token], "scanned token %q is not a token of the generated parser", token)
	}
}

// parserNameTables returns the generated parser's rule-index and token-type name
// tables, which the coverage collector maps parse-tree indices through.
func parserNameTables() (ruleNames, symbolicNames []string) {
	_, p := newGrammarParser("")
	return p.GetRuleNames(), p.GetSymbolicNames()
}

func newGrammarParser(src string) (*gen.GQLLexer, *gen.GQLParser) {
	lex := gen.NewGQLLexer(antlr.NewInputStream(src))
	return lex, gen.NewGQLParser(antlr.NewCommonTokenStream(lex, antlr.TokenDefaultChannel))
}
