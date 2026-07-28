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
// That takes 137 points to 49 classes. 48 are exercised both ways by the corpus. The
// 49th cannot be, its present branch being unreachable under ALL(*) prediction rather
// than merely unwritten, so optionalityExemptions records it — and owedOptionality, the
// worklist the other 16 passed through, is empty.
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
//
// An entry here is the claim that a file is owed, so a class no file can discharge does
// not belong in it: such an entry can never be deleted, and a worklist with a permanent
// item on it is the log this register was written against. Those go to
// optionalityExemptions, which asks for the reason no file can be written instead of the
// bead that will write one.
type owedBranch struct {
	class string
	bead  string
	why   string
}

// Empty today, every registered class having been discharged by a file except the one
// that moved to optionalityExemptions. It stays declared rather than nil so that the
// gate's message can keep naming it, and so the next one-sided class has somewhere
// obvious to land.
var owedOptionality = []owedBranch{}

// optionalityExemption records an optionality class whose missing branch no corpus file
// can take, because ALL(*) prediction hands that branch's input to another alternative
// at every position it could appear. Such a branch is dead rather than unwritten, and
// demanding a file for it would make the gate uncloseable — the same argument
// alternativeExemptions makes for alternatives, in the vocabulary of branches.
//
// stolenBy names the alternative that takes the input instead, as a `rule#N` tag, and
// TestOptionalityExemptions asserts that a corpus file covers it. That is what stops an
// exemption from excusing an untested construct: if nothing takes the thief either, then
// nothing exercises the spelling at all, and the exemption has excused the whole
// construct rather than one unreachable branch of it. The demand is made here rather
// than routed through the alternative obligation the way alternativeExemptions routes
// its, because classes and tags are different vocabularies and nothing carries a
// stolenBy from one into the other.
//
// As with alternativeExemptions, naming a covered thief is necessary and not sufficient:
// it would be satisfied by naming any alternative the corpus happens to take. What makes
// an entry answerable is why, which is prose a reviewer reads against the grammar.
type optionalityExemption struct {
	class    string
	stolenBy string
	bead     string
	why      string
}

var optionalityExemptions = []optionalityExemption{
	{
		class:    "18.9 :: VERTICAL_BAR valueType",
		stolenBy: "valueType#10",
		bead:     "gqlc-h9n.32",
		why:      "the `(VERTICAL_BAR valueType)*` repetition in closedDynamicUnionTypeAtl1 (GQL.g4:1731) is never entered, because atl2 is the left-recursive `valueType VERTICAL_BAR valueType` (GQL.g4:1732) and binds any `A | B` inside the angle brackets before the repetition is consulted. `ANY VALUE<STRING | INT>` parses as atl1 over a single valueType child that is itself an atl2, and `<A | B | C>` nests two of them left-associated, so the repetition takes zero iterations however many members are listed. constructed_dyn_closed_union.gql is the file, and its manifest entry already described the nesting without drawing the consequence",
	},
}

// exemptOptionalityClasses indexes the exemption list by class for the coverage gate. It
// validates nothing: TestOptionalityExemptions does that, so a malformed entry fails the
// test that owns the register rather than the gate that consults it.
func exemptOptionalityClasses() map[string]bool {
	exempt := make(map[string]bool, len(optionalityExemptions))
	for _, ex := range optionalityExemptions {
		exempt[ex.class] = true
	}
	return exempt
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

	exempt := exemptOptionalityClasses()

	var undischarged []string
	for class, state := range classes {
		if state.present && state.absent {
			continue
		}
		if owed[class] || exempt[class] {
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
		"an optionality class is exercised on one branch only: write the missing spelling under the named clause, or register it in owedOptionality with the bead that will. If the spelling you write keeps landing on the branch already covered, that is evidence about the grammar rather than the file — a branch can be unreachable under ALL(*) prediction, which goes in optionalityExemptions naming the alternative that takes its input")
}

// TestOptionalityExemptions owns the exemption register: it checks the entries are
// well-formed, sweeps them for staleness, and demands the thief.
//
// Staleness is the predicate owedOptionality is held to, a class the corpus exercises
// both ways, and it means something stronger here. An owed class that becomes covered is
// a worklist item someone completed; an exempted one that becomes covered is a claim the
// corpus has refuted, since the entry says no file can reach the branch. That is how a
// grammar change reviving it gets noticed rather than keeping an excuse it no longer
// needs.
func TestOptionalityExemptions(t *testing.T) {
	classes, _ := optionalityCoverage(t)
	covered := corpusCoverage(t).alternatives

	owed := make(map[string]bool, len(owedOptionality))
	for _, entry := range owedOptionality {
		owed[entry.class] = true
	}

	seen := make(map[string]bool, len(optionalityExemptions))
	for _, ex := range optionalityExemptions {
		require.NotEmpty(t, ex.why, "exempt class %q must say why no file can take the branch", ex.class)
		require.NotEmpty(t, ex.bead, "exempt class %q must name the bead holding the finding", ex.class)
		require.NotEmpty(t, ex.stolenBy, "exempt class %q must name the alternative that takes its input", ex.class)
		require.Contains(t, optionalityClassGolden, ex.class,
			"exempt class %q is not a class the grammar defines, so it excuses nothing; delete the entry", ex.class)
		require.False(t, seen[ex.class], "class %q is exempted twice", ex.class)
		require.False(t, owed[ex.class],
			"class %q is both owed and exempted: it is either a file someone will write or a branch nobody can reach", ex.class)
		seen[ex.class] = true

		require.False(t, classes[ex.class].present && classes[ex.class].absent,
			"class %q is exempted as unreachable but the corpus now parses it both ways; delete the exemption (%s)", ex.class, ex.bead)
		require.True(t, covered[ex.stolenBy],
			"class %q is excused because %s takes its input, but no corpus file takes %s either, so nothing exercises the spelling at all",
			ex.class, ex.stolenBy, ex.stolenBy)
	}
}
