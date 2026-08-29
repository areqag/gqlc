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

// isoGap is an ISO DDL-closure production with no counterpart in GQL.g4. It is
// modelled on alternativeExemption: the entry is only answerable because of why,
// which is prose a reviewer reads, so expect this list to be reviewed rather than
// merely to pass.
//
// Unlike alternativeExemption these are not excuses — a gap is a real hole, and
// the list is a ratchet rather than a waiver. isoGapRatchet is what stops it
// growing; an entry may be replaced, never added to.
type isoGap struct {
	production string
	bead       string
	why        string
}

// isoGapRatchet caps len(isoGaps). It gates the *count* of unimplemented
// productions, deliberately not a percentage: a percentage moves when the
// denominator moves, so adding corpus files could read as progress while nothing
// was implemented. Lower it when a gap closes; never raise it.
const isoGapRatchet = 14

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
		why:        "body is `!! See the Syntax Rules.` — ISO means the Unicode Nd category outside ASCII, but the artefact does not say so; GQL.g4 admits only <standard digit>",
	},
	{
		production: "standard digit",
		bead:       "gqlc-h9n.30",
		why:        "spelled `<octal digit> | 8 | 9`, i.e. plain 0-9; GQL.g4 folds it into the DIGIT fragment rather than naming it, so the construct is implemented while the production name is not",
	},
	{
		production: "double double quote",
		bead:       "gqlc-h9n.30",
		why:        "the \"\" escape inside a delimited identifier; GQL.g4 spells it as an inline literal inside the quoted-string lexer tokens rather than as a named rule, so the construct is implemented while the production name is not",
	},
	{
		production: "double grave accent",
		bead:       "gqlc-h9n.30",
		why:        "the `` escape inside an accent-quoted identifier; inline literal in the lexer, as with <double double quote>",
	},
	{
		production: "list value type",
		bead:       "gqlc-h9n.5",
		why:        "implemented: LIST<T>, T LIST, T ARRAY, bare LIST and ARRAY all resolve to graph.ListOf (gqlc-h9n.5). Production name is absent because GQL.g4 spells the three spellings as labelled alternatives of valueType (listValueTypeAlt1, listValueTypeAlt2, listValueTypeAlt3) rather than a named rule, as with <standard digit> and <open dynamic union type>",
	},
	{
		production: "constructed value type",
		bead:       "gqlc-h9n.5",
		why:        "ISO umbrella for list, record, and dynamic-union types. Lists are implemented (gqlc-h9n.5); records and closed unions are gqlc-h9n.33's. Production name is absent because GQL.g4 implements these as labelled alternatives of valueType rather than a named constructedValueType rule",
	},
	{
		production: "component type",
		bead:       "gqlc-h9n.5",
		why:        "implemented: the element type of a list; gqlc-h9n.5 reads it via resolveValueType's recursive call on the element valueType context. Production name is absent because GQL.g4 spells the element type as a recursive valueType argument inside the listValueTypeAlt1/2 alternatives rather than as a named componentType rule",
	},
	{
		production: "component type list",
		bead:       "gqlc-h9n.33",
		why:        "the comma-separated member list inside a closed dynamic union (ANY VALUE<A|B>); not implemented because the closed-union family awaits gqlc-h9n.33. Production name is absent because GQL.g4 uses a labelled alternative rather than a named rule — the same pattern as the other value-type productions in this list",
	},
	{
		production: "dynamic union type",
		bead:       "gqlc-h9n.33",
		why:        "umbrella for open and closed union spellings; the open ones (#7, #8) now resolve to graph.TypeAnyPropertyValue (ADR 0020), but the closed ones (#9, #10) need the enum to carry members, which gqlc-h9n.33 builds. Production name is absent because GQL.g4 uses labelled alternatives of valueType rather than a named rule",
	},
	{
		production: "open dynamic union type",
		bead:       "gqlc-h9n.34",
		why:        "implemented: ANY VALUE (and bare ANY) now resolve to graph.TypeAnyPropertyValue → Go any (ADR 0020). Production name is absent because GQL.g4 spells this as a labelled alternative of valueType (openDynamicUnionTypeLabel) rather than a named rule, as with <standard digit> and the other inlined productions",
	},
	{
		production: "closed dynamic union type",
		bead:       "gqlc-h9n.33",
		why:        "ANY VALUE<A|B> and bare A|B need the enum to carry members, which graph.PropertyType as a flat string cannot do. Unimplemented; gqlc-h9n.33 is the bead that fixes it",
	},
	{
		production: "dynamic property value type",
		bead:       "gqlc-h9n.34",
		why:        "implemented: ANY? PROPERTY VALUE now resolves to graph.TypeAnyPropertyValue → Go any (ADR 0020). Production name is absent because GQL.g4 spells this as a labelled alternative (dynamicPropertyValueTypeLabel) rather than a named rule, as with <standard digit>",
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

// TestISOProductionInventory is the gate. It reports the three buckets and fails
// if the absent set is not exactly isoGaps.
//
// The match is exact-set rather than a bare count so that both directions are
// checked: a production that becomes implemented leaves a stale entry behind, and
// a production that regresses to absent has no entry. Either is a failure, which
// is what makes the inventory a record rather than a number someone edits.
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

	want := make([]string, 0, len(isoGaps))
	for _, gap := range isoGaps {
		want = append(want, gap.production)
	}
	sort.Strings(want)
	sort.Strings(absent)

	require.Equal(t, want, absent,
		"the set of ISO productions absent from GQL.g4 changed.\n"+
			"A production that became implemented needs its isoGaps entry deleted and isoGapRatchet lowered.\n"+
			"A production that is newly absent needs an entry naming the bead that will implement it — and note the ratchet forbids growing the list.")
}

// TestISOGapRatchet is the half that stops the inventory being satisfied by
// writing more entries. TestISOProductionInventory demands isoGaps match reality;
// without this, making it match by appending would pass.
func TestISOGapRatchet(t *testing.T) {
	require.LessOrEqual(t, len(isoGaps), isoGapRatchet,
		"isoGaps grew past the ratchet: %d entries, limit %d. The count of unimplemented ISO productions may not increase",
		len(isoGaps), isoGapRatchet)
}

// TestISOGapsAreAnswerable pins the shape of an entry. A gap with no bead is a
// hole nobody owns, and one with no why is unreviewable — the failure mode this
// list has is silently becoming a dumping ground.
func TestISOGapsAreAnswerable(t *testing.T) {
	seen := make(map[string]bool, len(isoGaps))
	for _, gap := range isoGaps {
		require.NotEmpty(t, gap.bead, "isoGaps entry %q has no bead", gap.production)
		require.NotEmpty(t, gap.why, "isoGaps entry %q has no reason", gap.production)
		require.False(t, seen[gap.production], "isoGaps has duplicate %q", gap.production)
		seen[gap.production] = true

		require.Contains(t, isobnf.DDLClosure, gap.production,
			"isoGaps entry %q is not a production in the DDL closure; delete it", gap.production)
	}
}
