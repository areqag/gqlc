package codegen

import (
	"regexp"
	"strings"
	"unicode"
)

// packageIdent is the Go package-identifier grammar (spec §5.1). Digits
// inside are legal; underscores are legal; digit-leading is not; non-ASCII
// is not.
var packageIdent = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// exportedGoIdentRe is the ASCII exported Go identifier grammar (spec §4.5
// Rule 1). Explicit entity Names must satisfy it; the single-label mangle
// (Rule 2 / Rule 3) also lands its result on this predicate. C1's queryfile
// front end uses the same grammar for method names — deliberately, so a
// schema-side identifier reads the same as a query-side one.
var exportedGoIdentRe = regexp.MustCompile(`^[A-Z][A-Za-z0-9]*$`)

// rowBareIdent matches column text of shape "name" — a bare identifier
// projection like RETURN n or RETURN name (spec §4.3 shape 1). Anchored so
// substring matches are impossible.
var rowBareIdent = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// rowPropAccess matches column text of shape "n.name" — a single-dot
// property access projection like RETURN p.name (spec §4.3 shape 2).
// Anchored so substring matches are impossible.
var rowPropAccess = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*\.[A-Za-z_][A-Za-z0-9_]*$`)

// exportedGoIdent reports whether s matches ^[A-Z][A-Za-z0-9]*$ — the
// exported-Go-identifier grammar spec §4.5 Rule 1 pins for entity names.
// ASCII-only; Unicode escape hatch lives on field-name mangle only.
func exportedGoIdent(s string) bool {
	return exportedGoIdentRe.MatchString(s)
}

// paramFieldName derives the Params-struct field name for a parameter
// whose annotation was $<raw> (spec §4.2). Splits on '_', capitalises
// the first rune of each non-empty segment, preserves internal case of
// non-ALL-CAPS segments; ALL-CAPS segments stay ALL-CAPS.
func paramFieldName(raw string) string {
	if raw == "" {
		return ""
	}
	var b strings.Builder
	segments := strings.Split(raw, "_")
	for _, seg := range segments {
		if seg == "" {
			continue
		}
		if isAllCaps(seg) {
			b.WriteString(seg)
			continue
		}
		runes := []rune(seg)
		runes[0] = unicode.ToUpper(runes[0])
		b.WriteString(string(runes))
	}
	return b.String()
}

// isAllCaps reports whether every letter rune in s is uppercase (and s
// contains at least one letter). ALL-CAPS segments preserve their case
// under §4.2 so acronyms like API / URL / ID keep their form.
func isAllCaps(s string) bool {
	hasLetter := false
	for _, r := range s {
		if unicode.IsLetter(r) {
			hasLetter = true
			if !unicode.IsUpper(r) {
				return false
			}
		}
	}
	return hasLetter
}

// rowFieldName derives the Row-struct field name for a column whose
// text is one of the two clean shapes (spec §4.3). Returns "", false
// for anything else — the caller emits ErrAliasRequired.
func rowFieldName(colText string) (string, bool) {
	if rowBareIdent.MatchString(colText) {
		return paramFieldName(colText), true
	}
	if rowPropAccess.MatchString(colText) {
		dot := strings.IndexByte(colText, '.')
		return paramFieldName(colText[dot+1:]), true
	}
	return "", false
}

// LowerFirstRune lowercases the first rune of s. Used for the
// package-internal query-text const name (Query.Bare) and for
// single-parameter argument names.
func LowerFirstRune(s string) string {
	if s == "" {
		return ""
	}
	runes := []rune(s)
	runes[0] = unicode.ToLower(runes[0])
	return string(runes)
}

// ParamLocal names the one identifier in an emitted method's signature
// that the query text chose. A single-parameter method names its
// argument after the parameter the author wrote; everything else in the
// signature — the receiver, ctx, the Params struct of the two-or-more
// form — is the generator's own name. Shared rather than per backend
// because the signature it describes is backend-invariant surface: every
// target emits this form, so every target inherits the collision.
func ParamLocal(p Query) string {
	if len(p.ParamFields) != 1 {
		return ""
	}
	return LowerFirstRune(p.ParamFields[0].Field)
}

// Unshadowed adapts one generator-owned identifier so that the caller's
// argument cannot capture it. Every identifier an emitted method body
// resolves has to go through this — the locals it declares and the
// package-level names it references alike, because Go resolves both in
// the scope the parameter is bound in.
//
// A collision is not reliably a compile error. For
// MATCH (p:Person) WHERE p.name = $stmt the argument is a string named
// stmt and the statement composition assigns the composed SQL over it;
// for $<bare>QueryText the argument silently *becomes* the statement,
// since the const it displaces is a string and the composer takes a
// string. The same shadowing against an INT64 property is only a build
// failure, which is to say the property's width decided whether the
// defect was loud.
//
// Underscores are appended until the name is free. That terminates on
// the first pass, because a signature contributes at most one
// query-chosen identifier — but the loop is what makes the result
// unconditionally clear of it rather than clear by that argument.
func Unshadowed(p Query, name string) string {
	for name == ParamLocal(p) {
		name += "_"
	}
	return name
}

// QueryTextConst names the package-level const holding a query's source
// text. LowerFirstRune derives it and the single-parameter argument name
// both, so $<bare>QueryText reproduces it exactly; the const is the side
// that is free to move, being generator-owned and no part of the surface
// a caller writes against.
func QueryTextConst(p Query) string {
	return Unshadowed(p, p.Bare+"QueryText")
}
