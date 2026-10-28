package lexfold_test

import (
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stretchr/testify/require"

	cyphergen "github.com/areqag/gqlc/internal/grammar/cypher/gen"
	gqlgen "github.com/areqag/gqlc/internal/grammar/gql/gen"
	"github.com/areqag/gqlc/internal/grammar/lexfold"
)

// pieces mixes the syntax that opens each lexer context with a rune from every
// class in both tables, folded, kept and default, and an invalid byte.
var pieces = []string{
	"a", "Z", "_", "1", " ", "\n", "\"", "'", "`", "\\", "@", "$", "-", "<", ">", "/", "*",
	"//", "/*", "*/", "--", "MATCH ", "NODE ", "x'", "\\u00e9",
	"\u00e9", "\u00aa", "\u00b7", "\u00a0", "\u00a2", "\u00ad", "\u0080", // Latin-1, unfolded
	"\u6f22", "\u0300", "\u203f", "\ufe4d", "\u1680", "\u3000", "\u20ac", // folded
	"\u2014", "\u2212", "\u27e8", "\uff1e", // kept (Cypher)
	"\U0001f600", "\U00016800", "\ufffd", "\x9f", "\xff",
}

type lexed struct {
	Type, Channel, Start, Stop, Line, Column int
	Text                                     string
}

func tokens(lex antlr.Lexer) []lexed {
	var out []lexed
	for {
		t := lex.NextToken()
		out = append(out, lexed{t.GetTokenType(), t.GetChannel(), t.GetStart(), t.GetStop(), t.GetLine(), t.GetColumn(), t.GetText()})
		if t.GetTokenType() == antlr.TokenEOF {
			return out
		}
	}
}

// errorLog records the lexer's own error reports, which carry the offending
// text.
type errorLog struct {
	*antlr.DefaultErrorListener
	msgs []string
}

func (e *errorLog) SyntaxError(_ antlr.Recognizer, _ any, line, column int, msg string, _ antlr.RecognitionException) {
	e.msgs = append(e.msgs, fmt.Sprintf("%d:%d %s", line, column, msg))
}

func run(newLexer func(antlr.CharStream) antlr.Lexer, in antlr.CharStream) ([]lexed, []string) {
	lex := newLexer(in)
	log := &errorLog{DefaultErrorListener: antlr.NewDefaultErrorListener()}
	lex.RemoveErrorListeners()
	lex.AddErrorListener(log)
	return tokens(lex), log.msgs
}

// TestFoldPreservesTokens is the check that does not trust the derivation:
// every token and every lexer error, with its text and position, is the same
// through a Stream as through the stream the parsers used before.
func TestFoldPreservesTokens(t *testing.T) {
	lexers := []struct {
		name  string
		table lexfold.Table
		new   func(antlr.CharStream) antlr.Lexer
	}{
		{"gql", lexfold.GQL, func(s antlr.CharStream) antlr.Lexer { return gqlgen.NewGQLLexer(s) }},
		{"cypher", lexfold.Cypher, func(s antlr.CharStream) antlr.Lexer { return cyphergen.NewCypherLexer(s) }},
	}
	rng := rand.New(rand.NewPCG(1, 2))
	for _, l := range lexers {
		for i := range 3000 {
			var b strings.Builder
			for range 1 + rng.IntN(24) {
				b.WriteString(pieces[rng.IntN(len(pieces))])
			}
			src := b.String()
			wantToks, wantErrs := run(l.new, antlr.NewIoStream(strings.NewReader(src)))
			gotToks, gotErrs := run(l.new, lexfold.NewStream(strings.NewReader(src), l.table))
			require.Equal(t, wantToks, gotToks, "%s #%d tokens of %q", l.name, i, src)
			require.Equal(t, wantErrs, gotErrs, "%s #%d errors of %q", l.name, i, src)
		}
	}
}
