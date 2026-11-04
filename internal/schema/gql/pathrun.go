package gql

import (
	"github.com/antlr4-go/antlr/v4"

	"github.com/areqag/gqlc/internal/grammar/gql/gen"
)

// maxPathRun is the most separators one dotted or slashed path may carry.
// Measured 2026-11-04 at b03688e5 over the 411 .gql files in the tree (fuzz
// corpora excluded), the longest path carries 5
// (corpus/17-references/copy_of_relative_up_twice.gql).
const maxPathRun = 32

// pathRunTooLong lexes lex's whole input and returns the separator that takes a
// path past maxPathRun, or nil when none does. A path is names joined by / . or
// .. — a catalog reference such as /a/b/c or a.b.c, but equally a property
// chain or a run of divisions, which the scan cannot tell apart and need not:
// it counts the separators in a run that never has two non-separators in a
// row, so `a/b/c` is one path of two and `a.b c.d` is two paths of one.
//
// The bound exists for time. GQL.g4 cannot tell where a catalog reference's
// directory path ends and its schema and object names begin until the path
// does (catalogObjectParentReference, simpleDirectoryPath), so each segment's
// decision looks ahead to the end of the path: a 32 KiB `COPY OF /a/a/…/b` took
// 4.5 s and a 256 KiB one 4.6 minutes in the ANTLR parse alone (bd
// gqlc-jjot2.36).
//
// lex's error listeners are removed, since lexical errors are the real parse's
// to report, and its input is rewound to the start for the caller to lex again.
func pathRunTooLong(lex *gen.GQLLexer) antlr.Token {
	lex.RemoveErrorListeners()
	defer lex.GetInputStream().Seek(0)
	seps, prevSep := 0, false
	for t := lex.NextToken(); t.GetTokenType() != antlr.TokenEOF; t = lex.NextToken() {
		if t.GetChannel() != antlr.TokenDefaultChannel {
			continue
		}
		switch t.GetTokenType() {
		case gen.GQLLexerSOLIDUS, gen.GQLLexerPERIOD, gen.GQLLexerDOUBLE_PERIOD:
			seps++
			if seps > maxPathRun {
				return t
			}
			prevSep = true
		default:
			if !prevSep {
				seps = 0
			}
			prevSep = false
		}
	}
	return nil
}
