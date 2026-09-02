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

// edgeLabelFieldName derives the entity struct name for an edge type's
// label (spec §4.5 Rule 3: `ACTED_IN` → `ActedIn`, `KNOWS` → `Knows`).
//
// It differs from paramFieldName in exactly one disposition, and the
// difference is the point rather than an inconsistency: §4.2 preserves an
// ALL-CAPS segment because a parameter acronym ($ID, $URL) wants its case
// kept, and Rule 2 inherits that for node labels (`PERSON` → `PERSON`).
// An edge label is a second caller with the opposite convention — Neo4j
// relationship types are SCREAMING_SNAKE by convention, so preserving the
// case there yields a Go type named LINKED, or runs the segments together
// into ACTEDIN with no word boundary left. Both are valid exported
// identifiers, so nothing downstream refuses them (bd gqlc-ghdz).
//
// Lower-casing an ALL-CAPS segment before the shared mangle, rather than
// re-implementing the walk, keeps the '_' split, the empty-segment drop
// and the leading-digit failure disposition in one place.
func edgeLabelFieldName(raw string) string {
	segments := strings.Split(raw, "_")
	for i, seg := range segments {
		if isAllCaps(seg) {
			segments[i] = strings.ToLower(seg)
		}
	}
	return paramFieldName(strings.Join(segments, "_"))
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
// package-internal query-text const name (Query.Bare).
func LowerFirstRune(s string) string {
	if s == "" {
		return ""
	}
	runes := []rune(s)
	runes[0] = unicode.ToLower(runes[0])
	return string(runes)
}

// ParamArg is the identifier an emitted query method binds its
// parameters to, whatever their number. The two-or-more form has always
// bound a generated *Params struct under this name; the single-parameter
// form binds the value itself under it, rather than under the name the
// query author wrote after the dollar sign.
//
// Deriving that argument from the query text put an author-chosen
// identifier into the same scope as the receiver, the context argument,
// every local the emitted body declares and every package-level name it
// resolves. $q and $ctx redeclared the two the signature binds itself;
// $err, $records, $out, $stmt and $rows displaced a body local; $fmt and
// $agtypeArgs displaced an import and a helper; $_ mangled to the empty
// string and reached gofmt as a parameter with no name. Generation
// reported none of it, because the format gate parses the emission and
// does not type-check it.
//
// Renaming the generator's own names out of the way instead would need a
// reserved set kept in sync with every future change to the emitted
// body, and that set is not enumerable: fmt cannot be renamed without
// aliasing the import, and the decode<Entity> helpers vary with the
// user's schema. One generator-owned argument name closes the class
// instead of shrinking it.
//
// Nothing the author wrote is lost. The parameter name stays the
// driver-binding key, so the server still substitutes what the query
// says; it stays the *Params and *Row field name, which is exported and
// reached qualified (arg.MinAge) and so cannot be captured by anything;
// and the Go argument is positional at every call site, so only godoc
// and an editor hint ever read it.
//
// Shared rather than per backend because the signature it appears in is
// backend-invariant surface (TestBackendInvariantSurface): every target
// emits the same one.
const ParamArg = "arg"

// QueryTextConst names the package-level const holding a query's source
// text, derived from the method name the author declares in the
// //  name: annotation.
//
// Method names are unique across the package; the names derived from them
// are not. This one lands in the unexported namespace the decode<Entity>
// helpers occupy, and those derive from schema labels rather than from
// method names, so the two can meet: a node label FooQueryText alongside
// a query named DecodeFoo emits decodeFooQueryText as both a const and a
// func. The const is off sweepIdentifiers, so generation exits 0 and the
// redeclaration surfaces at go build. That collision is gqlc-igs4.
func QueryTextConst(p Query) string {
	return p.Bare + "QueryText"
}
