// Package nesting bounds how deeply a source nests brackets before an ANTLR
// parser sees it.
//
// The bound exists for time, not for correctness: ANTLR's adaptive prediction
// costs superlinear time in bracket depth, so a few thousand unclosed brackets
// take seconds and the 256 KiB a caller may hand over take minutes (bd
// gqlc-jjot2.31). The time is spent in AdaptivePredict lookahead before any
// rule is entered, so a parse-tree listener counting rule depth cannot see it;
// the token stream can, and lexing is cheap.
package nesting

import "github.com/antlr4-go/antlr/v4"

// MaxDepth is the deepest bracket nesting a parser admits. Measured 2026-11-04
// at 5f74aed0 over every .cypher and .gql file in the tree and every executing
// TCK query: the deepest Cypher is 40 (the TCK's nested list and map literal
// scenarios), the deepest GQL 5. Source nested exactly this deep costs about
// twice what the same bytes nested 64 deep do (bd gqlc-jjot2.31 notes).
const MaxDepth = 128

// TooDeep lexes lex's whole input and returns the first token that opens a
// bracket past MaxDepth, or nil when none does. It counts the ( [ { each token
// type's literal spells, so a compound token such as GQL's '<-[' opens one;
// a closer below depth 0 is ignored, so leading closers buy no extra depth.
//
// lex's error listeners are removed: lexical errors are the real parse's to
// report, in its own order. The input is rewound to its start afterwards, so
// the caller lexes it again with a fresh lexer.
func TooDeep(lex antlr.Lexer) antlr.Token {
	lex.RemoveErrorListeners()
	defer lex.GetInputStream().Seek(0)
	delta := deltas(lex.GetLiteralNames())
	depth := 0
	for t := lex.NextToken(); t.GetTokenType() != antlr.TokenEOF; t = lex.NextToken() {
		typ := t.GetTokenType()
		if typ < 0 || typ >= len(delta) {
			continue
		}
		depth = max(0, depth+delta[typ])
		if depth > MaxDepth {
			return t
		}
	}
	return nil
}

// deltas maps each token type to the depth its literal opens (positive) or
// closes (negative). A token type with no literal spelling is 0.
func deltas(literalNames []string) []int {
	out := make([]int, len(literalNames))
	for typ, lit := range literalNames {
		for _, r := range lit {
			switch r {
			case '(', '[', '{':
				out[typ]++
			case ')', ']', '}':
				out[typ]--
			}
		}
	}
	return out
}
