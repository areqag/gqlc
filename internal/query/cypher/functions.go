package cypher

import (
	"github.com/antlr4-go/antlr/v4"

	"github.com/areqag/gqlc/internal/grammar/cypher/gen"
)

// UnqualifiedFunctionCalls reports the function invocations a query text
// spells with no namespace: the name of every oC_FunctionInvocation whose
// oC_FunctionName carries an empty oC_Namespace, quoted as the author wrote it,
// in first-appearance order with repeated texts dropped. A text spelling none
// returns nil.
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
func UnqualifiedFunctionCalls(src string) []string {
	lex := gen.NewCypherLexer(antlr.NewInputStream(src))
	cp := gen.NewCypherParser(antlr.NewCommonTokenStream(lex, antlr.TokenDefaultChannel))
	lex.RemoveErrorListeners()
	cp.RemoveErrorListeners()

	scan := &functionScan{BaseCypherListener: &gen.BaseCypherListener{}, seen: map[string]struct{}{}}
	antlr.NewParseTreeWalker().Walk(scan, cp.OC_Cypher())
	return scan.found
}

// functionScan collects the names UnqualifiedFunctionCalls reports. It embeds
// the generated base listener so every production it does not care about is a
// no-op, and the walker reaches the WHOLE tree: a call inside EXISTS { … },
// inside a comprehension, or in a clause the collecting listener suppresses is
// still text the server has to parse.
type functionScan struct {
	*gen.BaseCypherListener

	found []string
	seen  map[string]struct{}
}

// EnterOC_FunctionInvocation records an invocation's name when the invocation
// carries no namespace.
//
// The name is recorded as WRITTEN rather than lowercased, because the refusal
// built on this quotes it back and every character it prints has to be one the
// author typed. Two cases of one name are therefore two entries; a caller
// matching a catalogue lowercases its own comparison.
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
	text := sn.GetText()
	if _, dup := f.seen[text]; dup {
		return
	}
	f.seen[text] = struct{}{}
	f.found = append(f.found, text)
}
