package gql_test

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/schema/gql/isobnf"
)

// The gate in corpus_test.go measures the corpus against GQL.g4. That denominator
// can only report alternatives we implemented and left unexercised; a production
// that is in ISO/IEC 39075 and absent from our grammar is not in it at all, so
// 100% there is reachable while arbitrarily much of the standard is missing.
//
// This gate swaps the denominator for isobnf.DDLClosure and sorts every ISO
// production into three buckets: implemented and exercised, implemented and
// unexercised, and absent. The third is the one no other gate can produce.
//
// The two gates are not redundant and must not be folded together. The g4 one
// answers "is our grammar exercised" and catches dead alternatives; this one
// answers "is the standard covered" and catches unimplemented ones.

// A production can be absent from GQL.g4 for two unrelated reasons, and until
// gqlc-ke7ox both were kept in one list under one ratchet. The list read 14, the
// ratchet capped 14, and the comment claimed the number counted unimplemented
// productions — but 11 of the 14 entries said "implemented" in their own why.
// gqlc-h9n.33 made that measurable: it closed four gaps, and the count did not
// move (`absent from GQL.g4 14`, before and after), because the productions were
// still absent as *names*. A number that does not fall when a gap closes is not
// ratcheting what its comment says it ratchets, so the two kinds are now two
// lists and only the first is ratcheted.
//
// isoGap is the first kind: an ISO DDL-closure production this implementation
// does not have. It is modelled on alternativeExemption in that the entry is only
// answerable because of why, which is prose a reviewer reads — but unlike an
// exemption these are not excuses. A gap is a real hole, and the list is a ratchet
// rather than a waiver: isoGapRatchet is what stops it growing.
type isoGap struct {
	production string
	bead       string
	why        string
}

// isoGapRatchet pins len(isoGaps). It gates the *count* of unimplemented
// productions, deliberately not a percentage: a percentage moves when the
// denominator moves, so adding corpus files could read as progress while nothing
// was implemented. Lower it when a gap closes; never raise it.
//
// It is a pin rather than a cap. As a cap (require.LessOrEqual) the constant may
// sit above reality indefinitely, so "lower it when a gap closes" is a convention
// nothing enforces and a closed gap is invisible — which is half of how the old
// single list held 14 while naming 3 holes. Pinned, closing a gap reds this test
// until the constant comes down, and opening one cannot hide under headroom.
const isoGapRatchet = 3

var isoGaps = []isoGap{
	{
		production: "character representation",
		bead:       "gqlc-lir",
		why:        "ISO gives this production no syntax at all — its entire body is `!! See the Syntax Rules.`, i.e. the free BNF artefact defers to the paywalled prose. Not implementable from the artefact we have, and gqlc-lir declined the purchase",
	},
	{
		production: "external object reference",
		bead:       "gqlc-lir",
		why:        "body is `!! See the Syntax Rules.` — no syntax in the free artefact, same bind as <character representation>",
	},
	{
		production: "other digit",
		bead:       "gqlc-lir",
		why:        "body is `!! See the Syntax Rules.` — ISO means the Unicode Nd category outside ASCII, but the artefact does not say so; GQL.g4 admits only <standard digit>, so unlike the entries in isoInlinedProductions this one names a construct the grammar does not accept. A real hole, not a naming difference: no GQL.g4 symbol spells it, which is why it cannot carry a spelledBy",
	},
}

// isoInlined is the second kind: an ISO DDL-closure production that *is*
// implemented, but whose name is absent from GQL.g4 because ANTLR spells the
// construct as a labelled alternative, a lexer fragment or an inline literal
// rather than as a rule of its own. These are not holes and are deliberately not
// ratcheted — the count moves when ANTLR's rule factoring changes, which nobody
// is trying to ratchet.
//
// spelledBy is what stops this list being the ratchet's escape hatch. Splitting
// isoGaps in two would otherwise make the ratchet dodgeable by moving an entry
// across rather than by implementing anything, so an entry here must name the
// GQL.g4 symbol that carries the construct, and TestISOInlinedProductionsAreSpelled
// fails if that symbol is not one GQL.g4 declares. That is a necessary condition
// and not a sufficient one, exactly as alternativeExemption's stolenBy is: it
// cannot tell that the named symbol is the *right* one, only that the claim is
// about something real. why is what a reviewer reads.
type isoInlined struct {
	production string
	spelledBy  string
	bead       string
	why        string
}

var isoInlinedProductions = []isoInlined{
	{
		production: "standard digit",
		spelledBy:  "DIGIT",
		bead:       "gqlc-h9n.30",
		why:        "spelled `<octal digit> | 8 | 9`, i.e. plain 0-9; GQL.g4 folds it into the DIGIT fragment rather than naming it, so the construct is implemented while the production name is not",
	},
	{
		production: "double double quote",
		spelledBy:  "DOUBLE_QUOTED_CHARACTER_REPRESENTATION",
		bead:       "gqlc-h9n.30",
		why:        "the \"\" escape inside a delimited identifier; GQL.g4 spells it as the inline literal '\"\"' in the body of that fragment rather than as a named rule, so the construct is implemented while the production name is not",
	},
	{
		production: "double grave accent",
		spelledBy:  "ACCENT_QUOTED_CHARACTER_REPRESENTATION",
		bead:       "gqlc-h9n.30",
		why:        "the `` escape inside an accent-quoted identifier; inline literal in that fragment's body, as with <double double quote>",
	},
	{
		production: "list value type",
		spelledBy:  "listValueTypeAlt1",
		bead:       "gqlc-h9n.5",
		why:        "implemented: LIST<T>, T LIST, T ARRAY, bare LIST and ARRAY all resolve to graph.ListOf (gqlc-h9n.5). Production name is absent because GQL.g4 spells the three spellings as labelled alternatives of valueType (listValueTypeAlt1, listValueTypeAlt2, listValueTypeAlt3) rather than a named rule, as with <standard digit> and <open dynamic union type>",
	},
	{
		production: "constructed value type",
		spelledBy:  "valueType",
		bead:       "gqlc-h9n.5",
		why:        "implemented: ISO's umbrella for list, record and dynamic-union types, all three of which resolve now — lists at gqlc-h9n.5, records and closed unions at gqlc-h9n.33. Production name is absent because GQL.g4 implements these as labelled alternatives of valueType rather than a named constructedValueType rule, so the umbrella is spelled by the parent rule itself",
	},
	{
		production: "component type",
		spelledBy:  "valueType",
		bead:       "gqlc-h9n.5",
		why:        "implemented: the element type of a list; gqlc-h9n.5 reads it via resolveValueType's recursive call on the element valueType context. Production name is absent because GQL.g4 spells the element type as a recursive valueType argument inside the listValueTypeAlt1/2 alternatives rather than as a named componentType rule",
	},
	{
		production: "component type list",
		spelledBy:  "closedDynamicUnionTypeAtl1",
		bead:       "gqlc-h9n.33",
		why:        "implemented: the member list inside a closed dynamic union (ANY VALUE<A|B>); gqlc-h9n.33 reads it through resolveUnionMembers, which recurses on each member valueType. ISO separates members by comma and GQL.g4 by VERTICAL_BAR, which is a spelling difference the resolution does not see. Production name is absent because GQL.g4 uses a labelled alternative rather than a named rule — the same pattern as the other value-type productions in this list",
	},
	{
		production: "dynamic union type",
		spelledBy:  "valueType",
		bead:       "gqlc-h9n.33",
		why:        "implemented: umbrella for open and closed union spellings. The open ones resolve to graph.TypeAnyPropertyValue (ADR 0020); the closed ones resolve to graph.UnionOf now that a PropertyType carries members (gqlc-h9n.33). Production name is absent because GQL.g4 uses labelled alternatives of valueType rather than a named rule, so the umbrella is spelled by the parent rule itself",
	},
	{
		production: "open dynamic union type",
		spelledBy:  "openDynamicUnionTypeLabel",
		bead:       "gqlc-h9n.34",
		why:        "implemented: ANY VALUE (and bare ANY) now resolve to graph.TypeAnyPropertyValue → Go any (ADR 0020). Production name is absent because GQL.g4 spells this as a labelled alternative of valueType rather than a named rule, as with <standard digit> and the other inlined productions",
	},
	{
		production: "closed dynamic union type",
		spelledBy:  "closedDynamicUnionTypeAtl1",
		bead:       "gqlc-h9n.33",
		why:        "implemented: ANY VALUE<A|B> and bare A|B both resolve to graph.UnionOf (gqlc-h9n.33). The bar it was blocked on — that a flat-string PropertyType cannot carry members — was answered by parameterising the encoding rather than by leaving the string, so the type stays comparable. Production name is absent because GQL.g4 spells both as labelled alternatives of valueType (Atl1 and Atl2) rather than a named rule",
	},
	{
		production: "dynamic property value type",
		spelledBy:  "dynamicPropertyValueTypeLabel",
		bead:       "gqlc-h9n.34",
		why:        "implemented: ANY? PROPERTY VALUE now resolves to graph.TypeAnyPropertyValue → Go any (ADR 0020). Production name is absent because GQL.g4 spells this as a labelled alternative rather than a named rule, as with <standard digit>",
	},
}

// reISOName strips everything a production name and a grammar rule name spell
// differently: ISO writes `node type pattern`, ANTLR writes `nodeTypePattern`,
// and the lexer writes `NODE_TYPE_PATTERN`.
var reISOName = regexp.MustCompile(`[^a-z0-9]`)

func normalizeISOName(s string) string {
	return reISOName.ReplaceAllString(strings.ToLower(s), "")
}

// reGrammarFragment matches an ANTLR fragment declaration. Fragments are the one
// thing this gate needs that the generated tables cannot supply — a fragment has
// no token type, so it appears in no name table — and they are not optional:
// GQL.g4 implements many lexical productions in them, and a scan without them
// reports 36 absent productions where the truth is 14.
//
// A `fragment` keyword can only begin a declaration, so unlike a bare-identifier
// pattern this cannot match a line inside a rule body.
var reGrammarFragment = regexp.MustCompile(`(?m)^fragment\s+([A-Za-z_][A-Za-z0-9_]*)`)

// grammarRuleNames returns every name GQL.g4 declares: parser rules and tokens
// from the generated parser's own tables, plus fragments scanned from the .g4.
//
// The tables are used in preference to scanning because they are what ANTLR
// actually built, so no regex can disagree with the grammar about what a rule is.
// An earlier version scanned the .g4 for every name with a bare-identifier
// pattern; it admitted one identifier that appears alone on a line inside a rule
// body (SIMPLE_COMMENT_MINUS). That changed no bucket today, but the defect it
// invites is a production reported as implemented because some rule body happens
// to mention its name — a false negative in the one bucket this gate exists to
// keep honest.
func grammarRuleNames(t *testing.T) map[string]string {
	t.Helper()

	names := make(map[string]string)
	add := func(n string) {
		if n != "" {
			names[normalizeISOName(n)] = n
		}
	}

	ruleNames, symbolicNames := parserNameTables()
	for _, n := range ruleNames {
		add(n)
	}
	for _, n := range symbolicNames {
		add(n)
	}

	src, err := os.ReadFile(grammarPath)
	require.NoError(t, err)
	for _, m := range reGrammarFragment.FindAllStringSubmatch(string(src), -1) {
		add(m[1])
	}

	require.NotEmpty(t, ruleNames, "the generated parser reports no rule names")
	return names
}

// reGrammarAltLabel matches an ANTLR alternative label. Labels are the one thing
// grammarSpellings needs that neither the generated tables nor reGrammarFragment
// supply: a label names an alternative rather than a rule, so it produces a
// context type and no entry in either name table — and labels are where GQL.g4
// puts most of the value-type productions isoInlinedProductions points at.
//
// All 44 `#` characters in GQL.g4 today begin a label, so this cannot currently
// match anything else. The shape it would over-accept is a `#` inside a string
// literal followed by a letter (`'#foo'`); ANTLR has no such literal here, and
// the direction of that error is to admit a spelledBy that names nothing, which
// weakens this guard rather than reddening it.
var reGrammarAltLabel = regexp.MustCompile(`#([A-Za-z_][A-Za-z0-9_]*)`)

// grammarSpellings returns every symbol GQL.g4 declares, verbatim: parser rules
// and tokens from the generated tables, plus fragments and alternative labels
// scanned from the .g4.
//
// It is deliberately not the normalized map grammarRuleNames builds. That map
// exists to match ISO's `node type pattern` against ANTLR's `nodeTypePattern`,
// where collapsing case and punctuation is the point. A spelledBy names one
// symbol and is written by the same hand that reads the grammar, so it is held to
// the exact spelling — normalizing would let `dynamicuniontype` satisfy a claim
// about `DYNAMIC_UNION_TYPE`, and an entry that names a symbol only up to case is
// not evidence its author found the symbol.
func grammarSpellings(t *testing.T) map[string]bool {
	t.Helper()

	ruleNames, symbolicNames := parserNameTables()
	spellings := make(map[string]bool, len(ruleNames)+len(symbolicNames))
	for _, n := range ruleNames {
		spellings[n] = true
	}
	for _, n := range symbolicNames {
		spellings[n] = true
	}

	src, err := os.ReadFile(grammarPath)
	require.NoError(t, err)
	for _, m := range reGrammarFragment.FindAllStringSubmatch(string(src), -1) {
		spellings[m[1]] = true
	}
	for _, m := range reGrammarAltLabel.FindAllStringSubmatch(string(src), -1) {
		spellings[m[1]] = true
	}

	delete(spellings, "")
	require.NotEmpty(t, ruleNames, "the generated parser reports no rule names")
	return spellings
}

// TestISOProductionInventory is the gate. It reports the buckets and fails if the
// absent set is not exactly isoGaps plus isoInlinedProductions.
//
// The match is exact-set rather than a bare count so that both directions are
// checked: a production that becomes implemented leaves a stale entry behind, and
// a production that regresses to absent has no entry. Either is a failure, which
// is what makes the inventory a record rather than a number someone edits.
//
// The staleness half is then asserted again per list. That is not extra detection
// and is not claimed as any: the union comparison is set equality over sorted
// slices, so it already reds on every stale entry, on every newly absent
// production, and on a duplicate. What the per-list loops add is the remedy, which
// differs by kind and is the thing gqlc-ke7ox found people got wrong — deleting a
// gap must lower isoGapRatchet, and deleting an inlined entry must not touch it.
// A reader who gets that backwards moves the number for a grammar rename, which
// is how the old single list came to hold 14 while naming 3 holes.
func TestISOProductionInventory(t *testing.T) {
	grammar := grammarRuleNames(t)
	got := corpusCoverage(t)

	covered := make(map[string]bool, len(got.rules)+len(got.tokens))
	for name := range got.rules {
		covered[normalizeISOName(name)] = true
	}
	for name := range got.tokens {
		covered[normalizeISOName(name)] = true
	}

	var exercised, unexercised, absent []string
	for _, production := range isobnf.DDLClosure {
		key := normalizeISOName(production)
		switch {
		case grammar[key] == "":
			absent = append(absent, production)
		case covered[key]:
			exercised = append(exercised, production)
		default:
			unexercised = append(unexercised, production)
		}
	}

	t.Logf("ISO/IEC 39075 graph-type DDL closure: %d productions of %d in the artefact",
		len(isobnf.DDLClosure), isobnf.TotalProductions)
	t.Logf("  implemented and exercised   %d", len(exercised))
	t.Logf("  implemented and unexercised %d", len(unexercised))
	t.Logf("  absent from GQL.g4          %d", len(absent))
	t.Logf("    of which real holes       %d (isoGaps, ratcheted)", len(isoGaps))
	t.Logf("    of which named otherwise  %d (isoInlinedProductions, not ratcheted)", len(isoInlinedProductions))

	sort.Strings(absent)

	// Per kind, so that a stale entry in one list cannot be masked by a newly
	// absent production in the other.
	for _, gap := range isoGaps {
		require.Contains(t, absent, gap.production,
			"isoGaps entry %q is no longer absent from GQL.g4: the production is implemented under its own name now.\n"+
				"Delete the entry and lower isoGapRatchet — a gap closing is the one thing that number exists to record.",
			gap.production)
	}
	for _, inlined := range isoInlinedProductions {
		require.Contains(t, absent, inlined.production,
			"isoInlinedProductions entry %q is no longer absent from GQL.g4: the production now has a rule of its own.\n"+
				"Delete the entry. Do not touch isoGapRatchet — this list is grammar naming, not implementation, and nothing was implemented by ANTLR renaming a rule.",
			inlined.production)
	}

	want := make([]string, 0, len(isoGaps)+len(isoInlinedProductions))
	for _, gap := range isoGaps {
		want = append(want, gap.production)
	}
	for _, inlined := range isoInlinedProductions {
		want = append(want, inlined.production)
	}
	sort.Strings(want)

	require.Equal(t, want, absent,
		"the set of ISO productions absent from GQL.g4 changed.\n"+
			"A production that is newly absent needs an entry in one of the two lists.\n"+
			"If we do not implement it, that is isoGaps — and note the ratchet forbids growing that list.\n"+
			"If we do implement it under a different grammar name, that is isoInlinedProductions, which must name the GQL.g4 symbol that spells it.")
}

// TestISOGapRatchet is the half that stops the inventory being satisfied by
// writing more entries. TestISOProductionInventory demands the two lists match
// reality; without this, making them match by appending to isoGaps would pass.
//
// It measures isoGaps alone. Measuring both lists together is what gqlc-ke7ox
// found: the total is dominated by grammar naming, which moves under ANTLR
// factoring nobody is ratcheting, and it absorbed four gaps closing at
// gqlc-h9n.33 without moving. isoInlinedProductions is deliberately unratcheted —
// TestISOInlinedProductionsAreSpelled is what holds it honest instead.
//
// Equality, not LessOrEqual: see isoGapRatchet. It also means this test is not
// vacuous on an empty isoGaps, which a cap would be.
func TestISOGapRatchet(t *testing.T) {
	require.Len(t, isoGaps, isoGapRatchet,
		"isoGaps has %d entries and isoGapRatchet is %d.\n"+
			"More entries than the ratchet: the count of unimplemented ISO productions may not increase.\n"+
			"Fewer: a gap closed, so lower isoGapRatchet to match — that is the movement this number exists to record.",
		len(isoGaps), isoGapRatchet)
}

// TestISOInlinedProductionsAreSpelled is what stops isoInlinedProductions being
// the ratchet's escape hatch. isoGaps is pinned, so the cheap way to satisfy it
// after adding a real hole is to file the hole as merely inlined instead; this
// makes that cost naming a GQL.g4 symbol that exists.
//
// It is a necessary condition and not a sufficient one — nothing here can tell
// that valueType is the *right* symbol for <component type>, only that it is a
// symbol. That is the same bound alternativeExemption's stolenBy carries, and why
// is what a reviewer reads.
func TestISOInlinedProductionsAreSpelled(t *testing.T) {
	spellings := grammarSpellings(t)

	// Every entry must be checked against a populated symbol set: an empty one
	// would fail every row for the wrong reason, and a near-empty one (tables
	// present, .g4 unreadable) would fail only the fragment and label rows.
	require.Contains(t, spellings, "valueType", "grammarSpellings is missing a known parser rule")
	require.Contains(t, spellings, "DIGIT", "grammarSpellings is missing a known lexer fragment")
	require.Contains(t, spellings, "listValueTypeAlt1", "grammarSpellings is missing a known alternative label")

	for _, inlined := range isoInlinedProductions {
		require.NotEmpty(t, inlined.spelledBy,
			"isoInlinedProductions entry %q names no GQL.g4 symbol. If nothing spells it, it is not inlined — it is a gap",
			inlined.production)
		require.Contains(t, spellings, inlined.spelledBy,
			"isoInlinedProductions entry %q claims GQL.g4 spells it as %q, and GQL.g4 declares no such rule, token, fragment or alternative label.\n"+
				"Either the symbol was renamed, or the production is not implemented at all and belongs in isoGaps.",
			inlined.production, inlined.spelledBy)
	}
}

// TestISOGapsAreAnswerable pins the shape of an entry in either list. An entry
// with no bead is a hole nobody owns, and one with no why is unreviewable — the
// failure mode these lists have is silently becoming a dumping ground.
//
// Duplicates are checked across both lists together, not within each. A
// production filed in both would otherwise satisfy the inventory twice over and
// let one entry be deleted with nothing reddening.
func TestISOGapsAreAnswerable(t *testing.T) {
	seen := make(map[string]bool, len(isoGaps)+len(isoInlinedProductions))
	check := func(list, production, bead, why string) {
		t.Helper()
		require.NotEmpty(t, bead, "%s entry %q has no bead", list, production)
		require.NotEmpty(t, why, "%s entry %q has no reason", list, production)
		require.False(t, seen[production], "%q is listed twice; a production is a gap or it is inlined, never both", production)
		seen[production] = true

		require.Contains(t, isobnf.DDLClosure, production,
			"%s entry %q is not a production in the DDL closure; delete it", list, production)
	}

	for _, gap := range isoGaps {
		check("isoGaps", gap.production, gap.bead, gap.why)
	}
	for _, inlined := range isoInlinedProductions {
		check("isoInlinedProductions", inlined.production, inlined.bead, inlined.why)
	}
}
