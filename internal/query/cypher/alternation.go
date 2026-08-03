package cypher

import (
	"github.com/antlr4-go/antlr/v4"

	"github.com/areqag/gqlc/internal/grammar/cypher/gen"
)

// RelationshipTypeAlternations reports the relationship-type alternations a
// query text spells: the source text of every oC_RelationshipTypes production
// naming more than one type, in first-appearance order with repeats dropped.
// A text spelling none returns nil.
//
// It answers a question about the TEXT, not about the model Parse builds, and
// the two are not the same question. Parse merges a re-bound relationship
// variable's occurrences by intersection (pattern.go, mergeBinding), so
// `MATCH (a)-[r:A|B]->(b), (a)-[r:A]->(b)` leaves one label on the binding
// while the text still spells `|`; and an alternation the author never
// projects contributes no column at all. A caller that has to decide something
// about the bytes it is going to ship — the Apache AGE backend runs the
// author's text verbatim (ADR 0005), and AGE 1.7.0's parser has no `|` in a
// relationship detail — has to read the bytes.
//
// It is a parse and not a scan for `|`, because the character is not a witness
// on its own: Cypher.g4 spells it in three productions — oC_RelationshipTypes
// (line 255), the list/filter comprehension (395) and the pattern
// comprehension (398) — so `[x IN xs | x.n]` carries one and names no
// relationship type. It is also not a walk of query.Query, which is downstream
// of the merge above and carries only what became a binding.
//
// Total by construction: it installs no error listener, so a text the grammar
// cannot parse yields whatever the recovering parser built rather than an
// error the caller has no answer for. That branch is unreachable through the
// CLI, where the same grammar has already accepted the text (internal/cli/
// pipeline, frontEndWalk) before any backend sees it. It consults no procedure
// signature registry, unlike Parse: the walk is syntactic, so a CALL that only
// resolves against a registry is read the same way with or without one.
func RelationshipTypeAlternations(src string) []string {
	lex := gen.NewCypherLexer(antlr.NewInputStream(src))
	cp := gen.NewCypherParser(antlr.NewCommonTokenStream(lex, antlr.TokenDefaultChannel))
	lex.RemoveErrorListeners()
	cp.RemoveErrorListeners()

	scan := &alternationScan{BaseCypherListener: &gen.BaseCypherListener{}, seen: map[string]struct{}{}}
	antlr.NewParseTreeWalker().Walk(scan, cp.OC_Cypher())
	return scan.found
}

// alternationScan collects the alternations RelationshipTypeAlternations
// reports. It embeds the generated base listener so every production it does
// not care about is a no-op, and the walker reaches the WHOLE tree: a
// relationship pattern inside EXISTS { … }, inside a pattern comprehension, or
// in a clause the collecting listener suppresses is still text the server has
// to parse.
type alternationScan struct {
	*gen.BaseCypherListener

	found []string
	seen  map[string]struct{}
}

// EnterOC_RelationshipTypes records the production's text when it names more
// than one type. AllOC_RelTypeName is the arity the grammar admits only after
// a `|` (Cypher.g4 §oC_RelationshipTypes), so its count is the witness and the
// character never has to be searched for.
func (a *alternationScan) EnterOC_RelationshipTypes(c *gen.OC_RelationshipTypesContext) {
	if len(c.AllOC_RelTypeName()) < 2 {
		return
	}
	text := c.GetText()
	if _, dup := a.seen[text]; dup {
		return
	}
	a.seen[text] = struct{}{}
	a.found = append(a.found, text)
}
