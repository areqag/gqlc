package gql

import (
	"github.com/antlr4-go/antlr/v4"

	"github.com/areqag/gqlc/internal/grammar/gql/gen"
)

// maxPrefixRun is the longest run of consecutive prefix operators parse admits.
// Measured 2026-11-04 at 5f74aed0 over the 409 .gql files in the tree (fuzz
// corpora excluded), the longest run is 1.
const maxPrefixRun = 32

// prefixRunTooLong lexes lex's whole input and returns the token that extends a
// run of consecutive prefix operators past maxPrefixRun, or nil when none does.
//
// The bound exists for time. Each of + - NOT ! opens a right-recursive
// alternative in GQL.g4 (valueExpression, numericValueExpression,
// labelExpression), so a run of them nests one rule invocation per token, and
// the decision that precedes the run cannot be settled until the run ends. On a
// cold prediction cache that costs time quadratic in the run's length: after
// '[' or '(' or a property map, 2000 '+' took 0.7 to 3.6 s and 16000 took 42 s
// (bd gqlc-jjot2.32). An operand anywhere in the run settles the decision, so
// only an unbroken run is counted; hidden-channel tokens do not break one.
//
// lex's error listeners are removed, since lexical errors are the real parse's
// to report, and its input is rewound to the start for the caller to lex again.
func prefixRunTooLong(lex *gen.GQLLexer) antlr.Token {
	lex.RemoveErrorListeners()
	defer lex.GetInputStream().Seek(0)
	run := 0
	for t := lex.NextToken(); t.GetTokenType() != antlr.TokenEOF; t = lex.NextToken() {
		if t.GetChannel() != antlr.TokenDefaultChannel {
			continue
		}
		switch t.GetTokenType() {
		case gen.GQLLexerPLUS_SIGN, gen.GQLLexerMINUS_SIGN, gen.GQLLexerNOT, gen.GQLLexerEXCLAMATION_MARK:
			run++
			if run > maxPrefixRun {
				return t
			}
		default:
			run = 0
		}
	}
	return nil
}
