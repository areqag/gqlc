package gql

import (
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// The obligation set counts labelled alternatives (`rule#N`), and gqlc-h9n.2
// established that this undercounts the branching surface. The wider measure, over
// the reachable rules: 174 top-level alternatives, 8 branches across 4 nested groups,
// and 137 optionality points — 319 branch points. The first two are already the
// obligation's subject. This file is about the 137.
//
// 129 of the 137 have neither branch demanded by anything. The other 8 have their
// present branch forced, because the optional element contains a name occurring
// nowhere else in the reachable grammar, so rule or token coverage cannot be
// discharged without taking the branch. Nothing forces an absent branch anywhere.
//
// Gating all 137 is the mistake A1§3 and A9 both had to correct: it demands 274
// spellings, and most of them are the same spelling written again. `notNull` alone is
// an optionality point in about forty rules — every numeric, temporal and reference
// value type carries it — and the fortieth `NOT NULL` file exercises the code the
// first one did. An obligation nobody can close is not read; it is waived.
//
// So the criterion, which is mechanical rather than case-by-case: an optionality point
// is classed by the pair (ISO clause of the rule containing it, spelling of the
// optional element), and the class owes one file taking the branch and one file
// eliding it. Both halves matter.
//
//   - The element rather than the position, because `NOT NULL` after INT and after
//     FLOAT reach the same code by way of the same sub-rule. Deduplicating collapses
//     the ~40 notNull points to the one obligation they are.
//   - The clause rather than the whole grammar, because the same element under a
//     different clause is a different construct's handling. `nodeTypeFiller?` under
//     18.2 is a node pattern with no filler; under 18.3 it is an edge endpoint with no
//     filler, and gqlc-h9n.26 found a real defect in exactly that second position. The
//     clause is also the unit the corpus is already partitioned by, so a class names a
//     clause an author owns — which is what makes the report a worklist.
//
// That takes 137 points to 49 classes. 32 are already exercised both ways by the
// corpus and cost nothing; owedOptionality registers the other 17.
//
// What this deliberately does not catch: two rules in one clause that the resolver
// handles separately. `TYPE?` elides in both nodeTypePhrase and nodeTypePattern, which
// have distinct listener methods, and one file discharges 18.2 for both. Tightening to
// the individual position raises the debt from 17 classes to 95 points, which is the
// re-brief A10 refused. gqlc-h9n.32 carries the residue.

// branchState is what the corpus has done with one class's two branches.
type branchState struct {
	present bool
	absent  bool
}

// optionalityPoint is one `x?` or `x*` in one alternative of one reachable rule.
type optionalityPoint struct {
	rule    string
	clause  string
	element string
	// withElement and withoutElement are the containing alternative with this point
	// forced to each branch, so that "the corpus took this branch" is decided by
	// matching a whole observed child sequence rather than by looking for the
	// element's symbols in it. The distinction is not academic: `x? x` puts the same
	// symbol on both branches, and a substring test would call it exercised.
	withElement    ebnf
	withoutElement ebnf
}

func (p optionalityPoint) class() string { return p.clause + " :: " + p.element }

// optionalitySymbols flattens an expression to the symbols under it, which is how a
// class names its element. Two elements that flatten alike merge into one class, and
// the golden below records the merge rather than hiding it.
func optionalitySymbols(e ebnf) []string {
	var syms []string
	var walk func(ebnf)
	walk = func(x ebnf) {
		switch v := x.(type) {
		case ebnfSymbol:
			syms = append(syms, string(v))
		case ebnfSeq:
			for _, item := range v {
				walk(item)
			}
		case ebnfChoice:
			for _, item := range v {
				walk(item)
			}
		case ebnfRepeat:
			walk(v.inner)
		}
	}
	walk(e)
	return syms
}

// forceOptional rebuilds expr with its nth optional node (in pre-order) pinned to one
// branch, and reports the symbols under that node. It returns ok=false once n is past
// the last optional, which is how the caller enumerates them without counting first.
//
// Forcing present keeps `*` repeating as `+` rather than collapsing to one occurrence,
// so a corpus sequence with three of them still matches the present branch.
func forceOptional(expr ebnf, n int, present bool) (out ebnf, element []string, ok bool) {
	seen := 0
	var rebuild func(ebnf) ebnf
	rebuild = func(x ebnf) ebnf {
		switch v := x.(type) {
		case ebnfSeq:
			next := make(ebnfSeq, len(v))
			for i, item := range v {
				next[i] = rebuild(item)
			}
			return next
		case ebnfChoice:
			next := make(ebnfChoice, len(v))
			for i, item := range v {
				next[i] = rebuild(item)
			}
			return next
		case ebnfRepeat:
			if v.optional {
				if seen == n {
					seen++
					ok = true
					element = optionalitySymbols(v.inner)
					if !present {
						return ebnfSeq{}
					}
					return ebnfRepeat{inner: v.inner, repeated: v.repeated}
				}
				seen++
			}
			return ebnfRepeat{inner: rebuild(v.inner), optional: v.optional, repeated: v.repeated}
		}
		return x
	}
	out = rebuild(expr)
	return out, element, ok
}

// optionalityPoints scans the reachable rules for optionality points. It reads
// obligation.nonFrontier rather than obligation.rules so that a cut rule, which the
// closure stopped at and no corpus file is asked to descend into, contributes none.
func optionalityPoints(t *testing.T) []optionalityPoint {
	t.Helper()

	rules, err := scanGrammarRules()
	require.NoError(t, err)
	sections := scanRuleSections(t)

	names := append([]string{}, grammarObligation(t).nonFrontier...)
	sort.Strings(names)

	var points []optionalityPoint
	for _, rule := range names {
		body, ok := rules[rule]
		require.True(t, ok, "reachable rule %s has no body in the grammar scan", rule)

		clause := sectionNumber(sections[rule])
		require.NotEmpty(t, clause, "rule %s sits under no numbered clause, so its optionality points cannot be attributed to an author", rule)

		alternatives, _ := ruleAlternatives(body)
		for _, alt := range alternatives {
			expr, err := parseEBNF(alt)
			require.NoError(t, err, "rule %s: %s", rule, alt)
			for n := 0; ; n++ {
				with, element, ok := forceOptional(expr, n, true)
				if !ok {
					break
				}
				without, _, _ := forceOptional(expr, n, false)
				points = append(points, optionalityPoint{
					rule:           rule,
					clause:         clause,
					element:        strings.Join(element, " "),
					withElement:    with,
					withoutElement: without,
				})
			}
		}
	}
	return points
}

// optionalityCoverage classes the points and folds in what the corpus parsed. A class
// is exercised on a branch as soon as any one of its points was, which is the whole
// content of the deduplication: the class is the claim that its points reach the same
// code.
func optionalityCoverage(t *testing.T) (map[string]branchState, int) {
	t.Helper()

	observed := corpusCoverage(t).shapes
	points := optionalityPoints(t)

	classes := make(map[string]branchState)
	for _, point := range points {
		state := classes[point.class()]
		for shape := range observed[point.rule] {
			var children []string
			if shape != "" {
				children = strings.Split(shape, " ")
			}
			if point.withElement.matches(children) {
				state.present = true
			}
			if point.withoutElement.matches(children) {
				state.absent = true
			}
		}
		classes[point.class()] = state
	}
	return classes, len(points)
}

// wantOptionalityPoints is the number of optionality points in the reachable grammar.
// It is pinned beside the class golden and not derived from it, because the two fail
// differently: a scanner that stops descending into repetitions loses points while
// leaving every class represented, and the golden alone would stay green.
const wantOptionalityPoints = 137

// optionalityClassGolden is every class the reachable grammar defines. Checked in as
// membership, not as a count, on the same argument the obligation goldens are: a size
// pin passes for any wrong 49.
//
// This golden is stable under corpus authoring, which is what makes it checked-in
// rather than opt-in like TestCorpusShapes. It is derived from GQL.g4 and the reachable
// set alone — no corpus file appears in its derivation — so adding a file cannot move
// it, and the merge-conflict objection that kept shape recording a report (amendment
// A10) does not reach it. It moves when the grammar moves, which is when someone should
// be reading it.
var optionalityClassGolden = []string{
	"12.6 :: AS",
	"12.6 :: IF NOT EXISTS",
	"12.6 :: PROPERTY",
	"17.1 :: SOLIDUS DOUBLE_PERIOD",
	"17.1 :: simpleDirectoryPath",
	"17.3 :: catalogObjectParentReference",
	"17.6 :: SOLIDUS",
	"17.6 :: objectName PERIOD",
	"18.1 :: COMMA elementTypeSpecification",
	"18.10 :: typed",
	"18.2 :: AS localNodeTypeAlias",
	"18.2 :: TYPE",
	"18.2 :: labelSetPhrase",
	"18.2 :: localNodeTypeAlias",
	"18.2 :: nodeSynonym TYPE nodeTypeName",
	"18.2 :: nodeTypeFiller",
	"18.2 :: nodeTypeImpliedContent",
	"18.3 :: TYPE",
	"18.3 :: edgeKind",
	"18.3 :: edgeKind edgeSynonym TYPE edgeTypeName",
	"18.3 :: edgeTypeFiller",
	"18.3 :: edgeTypeImpliedContent",
	"18.3 :: labelSetPhrase",
	"18.3 :: nodeTypeFiller",
	"18.4 :: AMPERSAND labelName",
	"18.5 :: COMMA propertyType",
	"18.5 :: propertyTypeList",
	"18.6 :: typed",
	"18.8 :: BINDING",
	"18.9 :: ANY",
	"18.9 :: COMMA fieldType",
	"18.9 :: COMMA scale",
	"18.9 :: LEFT_BRACKET maxLength RIGHT_BRACKET",
	"18.9 :: LEFT_PAREN fixedLength RIGHT_PAREN",
	"18.9 :: LEFT_PAREN maxLength RIGHT_PAREN",
	"18.9 :: LEFT_PAREN minLength COMMA maxLength RIGHT_PAREN",
	"18.9 :: LEFT_PAREN precision COMMA scale RIGHT_PAREN",
	"18.9 :: LEFT_PAREN precision COMMA scale RIGHT_PAREN notNull",
	"18.9 :: LEFT_PAREN precision RIGHT_PAREN",
	"18.9 :: PRECISION",
	"18.9 :: PROPERTY",
	"18.9 :: RECORD",
	"18.9 :: SIGNED",
	"18.9 :: VALUE",
	"18.9 :: VERTICAL_BAR valueType",
	"18.9 :: WITHOUT TIME ZONE",
	"18.9 :: fieldTypeList",
	"18.9 :: minLength COMMA",
	"18.9 :: notNull",
}

// owedOptionality is a class the corpus exercises on at most one branch. Each entry is
// a spelling no file has ever parsed, named so that it is a worklist item rather than a
// number in a log.
//
// It is a pin to repin: an entry whose class becomes exercised both ways fails, and the
// fix is to delete the entry. That is the only direction this list is allowed to move
// without a grammar change, and it is what stops the register from becoming the place
// unexercised spellings go to be forgotten.
type owedBranch struct {
	class string
	bead  string
	why   string
}

var owedOptionality = []owedBranch{
	{"18.10 :: typed", "gqlc-h9n.32", "fieldType's `::`-elided spelling, `{ name INT }` (GQL.g4:1997). Reachable only through binding table and record types, so the file records a decline rather than a resolve"},
	{"18.3 :: edgeTypeFiller", "gqlc-h9n.32", "an edge type phrase that names the type and stops, with no filler after the name (GQL.g4:1562)"},
	{"18.3 :: edgeTypeImpliedContent", "gqlc-h9n.32", "an edge type filler that is a key label set and nothing else (GQL.g4:1567)"},
	{"18.3 :: nodeTypeFiller", "gqlc-h9n.32", "an endpoint reference spelling `()` with no filler. The same element as 18.2's, kept separate because this is the position gqlc-h9n.26 found a live defect in"},
	{"18.8 :: BINDING", "gqlc-h9n.32", "`TABLE { ... }` without the BINDING keyword (GQL.g4:1714); declined with ErrUnsupportedType, so the file pins the decline"},
	{"18.9 :: COMMA fieldType", "gqlc-h9n.32", "a record type listing more than one field"},
	{"18.9 :: PROPERTY", "gqlc-h9n.32", "a graph reference value type spelling GRAPH without the PROPERTY keyword"},
	{"18.9 :: RECORD", "gqlc-h9n.32", "the RECORD-elided record type: a bare `{ f :: INT }` where the keyword is dropped (GQL.g4:1979)"},
	{"18.9 :: VALUE", "gqlc-h9n.32", "a dynamic union type spelled `ANY` rather than `ANY VALUE` (GQL.g4:1728, 1731)"},
	{"18.9 :: VERTICAL_BAR valueType", "gqlc-h9n.32", "a closed dynamic union listing more than one member, `ANY VALUE<A|B>` (GQL.g4:1731)"},
	{"18.9 :: fieldTypeList", "gqlc-h9n.32", "the empty record type `RECORD {}` (GQL.g4:1983)"},
}

// TestOptionalityClassGolden pins the classes the grammar defines, and the point count
// behind them. Both are what makes the coverage gate fail-closed: a scan that returns
// nothing satisfies "every class is exercised or owed" vacuously, and would report a
// clean bill for a broken scanner.
func TestOptionalityClassGolden(t *testing.T) {
	classes, points := optionalityCoverage(t)

	require.Equal(t, wantOptionalityPoints, points,
		"the reachable grammar's optionality points moved; if that is a grammar change, repin this and reread optionalityClassGolden, and if it is not, the scan has stopped descending somewhere")

	got := make([]string, 0, len(classes))
	for class := range classes {
		got = append(got, class)
	}
	require.ElementsMatch(t, optionalityClassGolden, got,
		"the optionality classes the grammar defines have changed; each is a clause and an optional element, and a new one arrives owing two spellings")
}

// TestOptionalityCoverage is the gate: every class is exercised on both branches, or is
// registered as owed. The registration is the part that has to expire, so an owed class
// the corpus has since covered fails here rather than sitting in the list looking like
// work.
func TestOptionalityCoverage(t *testing.T) {
	classes, _ := optionalityCoverage(t)

	owed := make(map[string]bool, len(owedOptionality))
	for _, entry := range owedOptionality {
		require.NotEmpty(t, entry.why, "owed class %q must name the spelling no file parses", entry.class)
		require.NotEmpty(t, entry.bead, "owed class %q must name the bead that discharges it", entry.class)
		require.Contains(t, optionalityClassGolden, entry.class,
			"owed class %q is not a class the grammar defines, so nothing can ever discharge it", entry.class)
		require.False(t, owed[entry.class], "class %q is registered twice", entry.class)
		owed[entry.class] = true

		state := classes[entry.class]
		require.False(t, state.present && state.absent,
			"class %q is registered as owed but the corpus now parses it both ways: delete the entry", entry.class)
	}

	var undischarged []string
	for class, state := range classes {
		if state.present && state.absent {
			continue
		}
		if owed[class] {
			continue
		}
		missing := "eliding the element"
		if !state.present {
			missing = "taking the element"
		}
		undischarged = append(undischarged, class+" (no file "+missing+")")
	}
	sort.Strings(undischarged)
	require.Empty(t, undischarged,
		"an optionality class is exercised on one branch only: write the missing spelling under the named clause, or register it in owedOptionality with the bead that will")
}
