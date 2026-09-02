package cypher

import (
	"strings"

	"github.com/antlr4-go/antlr/v4"

	"github.com/areqag/gqlc/internal/grammar/cypher/gen"
)

// Call is one function invocation a query text spells, and where in that text
// it is spelled.
type Call struct {
	// Text is the name as the author wrote it, in the author's own case,
	// so a refusal built on this quotes back what is in the file.
	Text string
	// Line and Column locate the name's first character within the text
	// that was scanned. BOTH are 1-based, and they index the SCANNED TEXT
	// and no file, on exactly the terms Alternation's own field doc sets
	// out — see there for why the column is converted here rather than at
	// each caller, and why a caller printing these has to say which text
	// they index.
	Line, Column int
}

// UnqualifiedFunctionCalls reports the function invocations a query text
// spells with no namespace: the name of every oC_FunctionInvocation whose
// oC_FunctionName carries an empty oC_Namespace, quoted as the author wrote it,
// with the position it was written at, in source order. A text spelling none
// returns nil.
//
// Two invocations of ONE name are two answers, not one — they are two calls the
// author has to rewrite, and the position is what makes naming both an answer
// rather than noise. Until the position was carried the repeat was dropped, so
// a query calling `datetime()` twice read as a query calling it once
// (bd gqlc-fpl14).
//
// Namespaced names are absent because they are different names: Cypher.g4
// §oC_FunctionName is `oC_Namespace oC_SymbolicName`, and a server resolving
// `duration.between` resolves nothing about `duration`.
//
// It answers a question about the TEXT, not about the model Parse builds, and
// the two are not the same question. Predicate structure is dropped from the
// model (ADR 0003), so `WHERE p.at < datetime()` leaves no column, parameter or
// binding carrying the call; a write clause projects nothing and still ships
// its whole text; and a call the resolver types is typed by its RESULT, which
// says nothing about whether the server has the function. A caller deciding
// about the bytes it is going to ship — the Apache AGE backend runs the
// author's text verbatim (ADR 0005) — has to read the bytes.
//
// It is a parse and not a scan for a name followed by '(', because neither
// half of that is a witness: a property lookup (`p.datetime`), a label
// (`(d:date)`), a variable and a string literal all spell the name, and a
// PROCEDURE invocation spells the name and the parenthesis both while being
// §oC_ExplicitProcedureInvocation, a production resolved against a different
// catalogue.
//
// Total by construction on the same terms as RelationshipTypeAlternations: the
// error listeners are REMOVED, so a text the grammar cannot parse yields
// whatever the recovering parser built rather than an error the caller has no
// answer for, and nothing is written to os.Stderr from a library function with
// no output channel of its own. Pinned by
// TestUnqualifiedFunctionCallsIsTotal and
// TestUnqualifiedFunctionCallsWritesNothingToStderr.
func UnqualifiedFunctionCalls(src string) []Call {
	lex := gen.NewCypherLexer(antlr.NewInputStream(src))
	cp := gen.NewCypherParser(antlr.NewCommonTokenStream(lex, antlr.TokenDefaultChannel))
	lex.RemoveErrorListeners()
	cp.RemoveErrorListeners()

	scan := &functionScan{BaseCypherListener: &gen.BaseCypherListener{}}
	antlr.NewParseTreeWalker().Walk(scan, cp.OC_Cypher())
	return scan.found
}

// QualifiedCall is one namespaced function invocation a query text spells.
type QualifiedCall struct {
	// Namespace is the invocation's oC_Namespace lowercased, its parts
	// joined by '.'. Lowercased because openCypher resolution is
	// case-insensitive and a caller comparing against a catalogue has to
	// read it the way the resolver does.
	Namespace string
	// Text is the whole oC_FunctionName as the author wrote it, namespace
	// included, so a refusal built on this quotes back what is in the file.
	Text string
	// Line and Column locate the oC_FunctionName's first character — the
	// namespace's, not the bare name's — within the text that was scanned,
	// both 1-based and indexing no file. Alternation's field doc carries
	// the whole of the convention and the reason for it.
	Line, Column int
}

// QualifiedFunctionCalls reports the function invocations a query text spells
// WITH a namespace — the exact complement of UnqualifiedFunctionCalls over the
// same production, each with the position it was written at, in source order. A
// text spelling none returns nil.
//
// Two invocations of ONE spelling are two answers, not one, for the reason the
// unqualified scan gives: two calls to rewrite, and only the position tells the
// author which is which (bd gqlc-fpl14).
//
// The pair partitions the calls rather than overlapping: Cypher.g4
// §oC_FunctionName is `oC_Namespace oC_SymbolicName` and an invocation's
// namespace is empty or it is not, so no call is reported by both and none is
// reported by neither. Pinned by TestTheTwoFunctionScansPartitionTheCalls.
//
// It is a separate reading rather than a widening of the other because the two
// answer to different evidence. Apache AGE refuses `duration.between` as
// SQLSTATE 3F000 `schema "duration" does not exist` — Postgres resolves the
// namespace as a schema qualifier and fails there, BEFORE it looks for any
// function, so the server's answer names no function and a caller matching it
// has to match on the namespace (ADR 0028, bd gqlc-dy40s).
//
// Everything the unqualified scan says about being a parse and not a scan for
// characters holds here and one thing more: §oC_ProcedureName is
// `oC_Namespace oC_SymbolicName` as well, so `CALL db.labels()` spells this
// shape exactly while being resolved against a different catalogue. Only
// §oC_FunctionInvocation is read.
//
// Total by construction on the same terms: the error listeners are REMOVED, so
// a text the grammar cannot parse yields whatever the recovering parser built
// and nothing is written to os.Stderr. Pinned by
// TestQualifiedFunctionCallsIsTotal and
// TestQualifiedFunctionCallsWritesNothingToStderr.
func QualifiedFunctionCalls(src string) []QualifiedCall {
	lex := gen.NewCypherLexer(antlr.NewInputStream(src))
	cp := gen.NewCypherParser(antlr.NewCommonTokenStream(lex, antlr.TokenDefaultChannel))
	lex.RemoveErrorListeners()
	cp.RemoveErrorListeners()

	scan := &qualifiedScan{BaseCypherListener: &gen.BaseCypherListener{}}
	antlr.NewParseTreeWalker().Walk(scan, cp.OC_Cypher())
	return scan.found
}

// qualifiedScan collects the calls QualifiedFunctionCalls reports, on the same
// terms as functionScan: the generated base listener makes every other
// production a no-op and the walker reaches the whole tree.
type qualifiedScan struct {
	*gen.BaseCypherListener

	found []QualifiedCall
}

// EnterOC_FunctionInvocation records an invocation's namespace, spelling and
// position when the invocation carries a namespace.
//
// The position comes off oC_FunctionName's own start token, which is the first
// character of the NAMESPACE — the whole of what Text quotes, so the coordinate
// names the same characters.
func (q *qualifiedScan) EnterOC_FunctionInvocation(c *gen.OC_FunctionInvocationContext) {
	name := c.OC_FunctionName()
	if name == nil {
		return
	}
	ns := name.OC_Namespace()
	if ns == nil {
		return
	}
	parts := ns.AllOC_SymbolicName()
	if len(parts) == 0 {
		return
	}
	lowered := make([]string, len(parts))
	for i, p := range parts {
		lowered[i] = strings.ToLower(p.GetText())
	}
	// The whole name off the grammar rather than rebuilt from the parts:
	// §oC_Namespace admits no SP, so the context's text is the author's
	// bytes and reassembling them could only introduce a difference.
	text := name.GetText()
	start := name.GetStart()
	q.found = append(q.found, QualifiedCall{
		Namespace: strings.Join(lowered, "."),
		Text:      text,
		Line:      start.GetLine(),
		// +1 because ANTLR counts a line's characters from 0 while it
		// counts the lines from 1.
		Column: start.GetColumn() + 1,
	})
}

// functionScan collects the names UnqualifiedFunctionCalls reports. It embeds
// the generated base listener so every production it does not care about is a
// no-op, and the walker reaches the WHOLE tree: a call inside EXISTS { … },
// inside a comprehension, or in a clause the collecting listener suppresses is
// still text the server has to parse.
type functionScan struct {
	*gen.BaseCypherListener

	found []Call
}

// EnterOC_FunctionInvocation records an invocation's name when the invocation
// carries no namespace.
//
// The name is recorded as WRITTEN rather than lowercased, because the refusal
// built on this quotes it back and every character it prints has to be one the
// author typed. Two cases of one name are therefore two entries; a caller
// matching a catalogue lowercases its own comparison.
//
// The position comes off oC_SymbolicName's own start token, so it names the
// first character of the very text GetText quotes, and nothing is searched for.
func (f *functionScan) EnterOC_FunctionInvocation(c *gen.OC_FunctionInvocationContext) {
	name := c.OC_FunctionName()
	if name == nil {
		return
	}
	if ns := name.OC_Namespace(); ns != nil && len(ns.AllOC_SymbolicName()) > 0 {
		return
	}
	sn := name.OC_SymbolicName()
	if sn == nil {
		return
	}
	start := sn.GetStart()
	f.found = append(f.found, Call{
		Text: sn.GetText(),
		Line: start.GetLine(),
		// +1 because ANTLR counts a line's characters from 0 while it
		// counts the lines from 1.
		Column: start.GetColumn() + 1,
	})
}
