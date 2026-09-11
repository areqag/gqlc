package cypher

import (
	"fmt"
	"strings"

	"github.com/antlr4-go/antlr/v4"

	"github.com/areqag/gqlc/internal/grammar/cypher/gen"
)

// symbolicName reads one oC_SymbolicName as the name it denotes.
//
// A delimited identifier's delimiters are syntax and not content, so n and the
// backtick-delimited spelling of n are two spellings of ONE name and must reach
// the model as the same bytes. The schema front end settled this at gqlc-tzu9r
// (gql.identifierName); until this function existed the query front end was its
// pre-tzu9r twin, so a schema declaring Person and a query naming the delimited
// spelling of Person declared two types that never unified and the author got
// ErrUnknownLabel for a label that exists (bd gqlc-y25yo).
//
// Decoding here rather than in query's model constructors is forced, not
// preferred, and for the reason gqlc-935qa gives: a doubled delimiter denotes
// one, so a decoded name can hold the very byte that delimited it, and a bare
// string arriving at query.NewVarEndpoint cannot be asked whether it is still
// quoted. Only the token knows its own delimiter kind.
//
// The escape set here is NOT GQL's. Cypher.g4 §EscapedSymbolicName is
// `( '`' (~[`])* '`' )+` — one delimiter, a doubled delimiter as its only
// escape, no reverse-solidus forms and no introducer-prefixed strings. So
// gqlc-935qa's refusal inventory does not transfer: ErrNoEscapeIdentifier and
// ErrIdentifierEscape have no fail-site in this grammar and are deliberately
// not copied.
//
// Case is untouched in both arms, matching the schema side: an undelimited
// identifier reaches the model as its source bytes and so does a decoded
// delimited one.
//
// No ampersand refusal is copied here either, and that is the design rather
// than an omission. graph.LabelSetKey quotes a label whose bytes are the
// encoding's own, so the delimited spelling of A&B keys distinctly from the two
// labels A and B (bd gqlc-649co, executing gqlc-yd4ba). A refusal would have to
// be copied into every producer of labels; quoting at the one point every
// producer flows through removes the hazard once.
func symbolicName(sn gen.IOC_SymbolicNameContext) string {
	if sn == nil {
		return ""
	}
	// Structural, not textual: the token kind is read off the grammar rather
	// than guessed from a leading byte. UnescapedSymbolicName cannot begin with
	// a backtick (IdentifierStart is ID_Start | Pc) and neither can HexLetter or
	// any of the six keyword alternatives, so the two tests agree today — but a
	// grammar change is visible to this one and invisible to the other.
	if sn.EscapedSymbolicName() == nil {
		return sn.GetText()
	}
	return decodeEscaped(sn.GetText())
}

// decodeEscaped resolves an EscapedSymbolicName token's text to the name it
// denotes: the outer delimiters are dropped and each doubled delimiter within
// becomes one.
//
// ReplaceAll is exact here rather than merely close, which is why this does not
// carry the schema side's pairwise walk. The production is a repetition of
// delimited groups, so every backtick interior to the token is either the close
// of one group or the open of the next; they occur only in pairs, and no run of
// them can have odd length. There is no third case for a left-to-right
// replacement to mis-split.
//
// The slice is unguarded because its precondition is the caller's, not a fact
// about the argument: both callers reach here only for a context whose
// EscapedSymbolicName is non-nil, and that production's smallest match is the
// two-character empty name. A length guard was written here first and removed
// after a mutation row deleting it survived the whole suite — nothing can reach
// it, so it was mechanism that read as a check while checking nothing (bd
// gqlc-y25yo).
func decodeEscaped(text string) string {
	return strings.ReplaceAll(text[1:len(text)-1], "``", "`")
}

// variableName reads an oC_Variable as the name it denotes.
func variableName(v gen.IOC_VariableContext) string {
	if v == nil {
		return ""
	}
	return symbolicName(v.OC_SymbolicName())
}

// schemaName reads an oC_SchemaName — the production behind every label,
// relationship type and property key — as the name it denotes. Its reserved-word
// alternative is a keyword token, which carries no delimiter and no escape, so
// its source bytes are already the name.
func schemaName(s gen.IOC_SchemaNameContext) string {
	if s == nil {
		return ""
	}
	if sn := s.OC_SymbolicName(); sn != nil {
		return symbolicName(sn)
	}
	return s.GetText()
}

// labelName reads an oC_LabelName as the label it denotes.
func labelName(l gen.IOC_LabelNameContext) string {
	if l == nil {
		return ""
	}
	return schemaName(l.OC_SchemaName())
}

// relTypeName reads an oC_RelTypeName as the relationship type it denotes.
func relTypeName(r gen.IOC_RelTypeNameContext) string {
	if r == nil {
		return ""
	}
	return schemaName(r.OC_SchemaName())
}

// propertyKeyName reads an oC_PropertyKeyName as the property it denotes.
func propertyKeyName(p gen.IOC_PropertyKeyNameContext) string {
	if p == nil {
		return ""
	}
	return schemaName(p.OC_SchemaName())
}

// procedureResultFieldName reads an oC_ProcedureResultField as the field it
// denotes.
func procedureResultFieldName(f gen.IOC_ProcedureResultFieldContext) string {
	if f == nil {
		return ""
	}
	return symbolicName(f.OC_SymbolicName())
}

// procedureNameOf reads an oC_ProcedureName as the dotted, fully-qualified name
// it denotes: each namespace segment decoded, then the trailing symbolic name,
// joined by '.'.
//
// Rebuilding from the segments is required rather than tidy. The dots belong to
// oC_Namespace and not to any segment, so the context's own text carries them
// interleaved with the delimiters this has to strip, and there is no way to
// decode that text as one string without re-finding the segment boundaries the
// parse has already found.
func procedureNameOf(p gen.IOC_ProcedureNameContext) string {
	if p == nil {
		return ""
	}
	var b strings.Builder
	if ns := p.OC_Namespace(); ns != nil {
		for _, seg := range ns.AllOC_SymbolicName() {
			b.WriteString(symbolicName(seg))
			b.WriteByte('.')
		}
	}
	b.WriteString(symbolicName(p.OC_SymbolicName()))
	return b.String()
}

// refuseEmptyIdentifiers fails the parse when any delimited identifier in the
// tree denotes the empty name.
//
// The empty name is refused rather than made representable, which is the
// opposite of the ruling graph.LabelSetKey executes for an ampersand-bearing
// label — and the two are not in tension, because the model has a spelling for
// a label carrying an ampersand and has none for an empty name. Emptiness is
// the model's own sentinel for ABSENCE in two positions a decoded identifier
// reaches: rawBinding.variable == "" is how collectNode and collectEdge spell
// an anonymous pattern element, and query.Ref.Property == "" is how a bare
// variable reference is distinguished from a property lookup. Decoding an empty
// delimited identifier into either would not report a name the model cannot
// hold; it would report a DIFFERENT query. The schema front end refuses the
// same shape (gql.ErrEmptyIdentifier), so a name no schema can declare is a
// name no query can name, on both fronts.
//
// It runs before the collection walk, not as a handler within it, for two
// reasons. Its answer is a fact about the text in the same category as a syntax
// error, so it belongs where fail() documents syntax errors as living — outside
// walker context, where subqueryDepth is definitionally 0 and EXISTS
// suppression cannot drop it. And running first makes it the first error rather
// than a shadowed second: an empty variable name otherwise collects as an
// anonymous element and surfaces downstream as ErrUnboundVariable, naming a
// symptom instead of the cause.
func (l *listener) refuseEmptyIdentifiers(tree antlr.Tree) {
	for _, sn := range findNodesOfType[gen.IOC_SymbolicNameContext](tree) {
		if sn.EscapedSymbolicName() == nil || decodeEscaped(sn.GetText()) != "" {
			continue
		}
		l.fail(fmt.Errorf("%w: %s", ErrEmptyIdentifier, sn.GetText()))
		return
	}
}
