package age

import (
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"unicode"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/query/cypher"
)

// A refusal list whose entries have no witness is a guess with a test
// suite around it. Everything in this file exists to make that shape
// impossible: dialectGaps may only grow alongside the live measurements
// that justify it, and the sweep below is what says so.
//
// Where that stops: the sweep reads a witness's code for the probe text
// and the recorded answer. It does not read the witness's control flow,
// so it can say the answer is spelled in the test and cannot say the test
// asserts it — gutting the assertion and leaving the wantMessage literals
// standing leaves this file green (review mutation M18, stated in ADR
// 0028). Reading the code rather than the source bytes is itself a fix
// and not a given; see witnessBodies.
//
// The hazard is asymmetric, which is why the binding is worth its cost.
// A missing refusal costs the author a runtime error they were going to
// get anyway. A wrong one costs them a query that would have worked, and
// generated code runs their text verbatim (ADR 0005) — there is no
// rewrite, no escape hatch and no flag. So the table is allowed to be
// incomplete and is not allowed to be speculative.

const (
	// repoRoot reaches the two artefacts a gap's witness lives in from
	// this package's directory. age_test.go reaches the corpus the same
	// way; nothing in the module graph connects them, because the
	// fixture corpus is a separate Go module on purpose.
	repoRoot = "../../.."
	// liveGlob is where a witness test is written: the codegen module's
	// live battery, behind the codegen_live build tag. These files are
	// read from SOURCE and not imported — they are in another module and
	// under a tag this binary is not built with, so the binding is a
	// source-level one either way. The glob spans the neo4j battery too,
	// which is deliberate: a witness naming a neo4j test has to be
	// findable in order to be rejected by the recipe check, rather than
	// missing and indistinguishable from a typo.
	liveGlob = "test/data/codegen/live_*_test.go"
	// liveBuildTag is the tag every file matching liveGlob is behind. A
	// command line that does not build it compiles none of them, so it
	// runs no witness however its -run reads: measured as review mutation
	// T1, which left the sweep green while `go test` printed
	// "[no test files]" for every package in test/data/codegen.
	liveBuildTag = "codegen_live"
	// justfilePath holds the recipes CI invokes.
	justfilePath = "justfile"
)

// ageLiveRecipes are the recipes that must run every AGE witness. Named
// rather than derived, because the third live recipe
// (test-codegen-live-neo4j) must NOT run them — it is the per-PR half
// and starts no AGE container — so "every live recipe" is the wrong
// rule and would either be false or force the neo4j half to pay for a
// container it has no use for.
var ageLiveRecipes = []string{"test-codegen-live", "test-codegen-live-age"}

// TestEveryDialectGapCarriesItsWitness is the binding itself: every
// construct this backend refuses on the query text is measured against
// the pinned AGE image, and both halves of that sentence are checked
// here rather than believed.
//
// It reads real files. A probe that no live test carries, an answer no
// live test asserts, or a witness no CI recipe runs is a refusal resting
// on a claim nothing re-measures, and each of those is a complaint.
func TestEveryDialectGapCarriesItsWitness(t *testing.T) {
	// The COUNT of the list is pinned rather than its emptiness, and it is
	// one guard of two because the list goes wrong in two directions and
	// each guard catches one. A name REMOVED is caught here: witnessGaps
	// complains once per entry of the map readRecipes returns, so a
	// dropped name is a recipe nobody checks and the sweep goes on passing
	// (review mutation R1, one name left, green). A name REPEATED leaves
	// this count at two and collapses that map to one entry, which this
	// pin cannot see — recipeBodies complains about it instead (review
	// mutation D1, which survived this pin). The empty list fails in
	// recipeBodies too (R2). The third live recipe,
	// test-codegen-live-neo4j, is absent on purpose — see ageLiveRecipes.
	require.Len(t, ageLiveRecipes, 2,
		"both recipes that start an AGE container have to be read, or a witness can "+
			"stop being run by the one that is not")
	bodies := readLiveWitnessBodies(t)
	recipes := readRecipes(t, ageLiveRecipes)
	require.Empty(t, witnessGaps(dialectGaps, bodies, recipes),
		"every refusal this backend makes on the query text has to rest on a live measurement")
}

// TestEveryRefusedFunctionNameIsNamedByItsProbeAnswer closes the loop
// the derivation opens. undefinedFunctions is read out of the probe
// TEXTS, so a probe calling the wrong function would enter the wrong
// name into the catalogue and every other check here would still pass.
// The server's own answer is the independent reading: it names the
// function it could not find, so a probe and its answer agreeing is two
// sources saying the same name.
func TestEveryRefusedFunctionNameIsNamedByItsProbeAnswer(t *testing.T) {
	require.NotEmpty(t, undefinedFunctionProbes)
	for _, p := range undefinedFunctionProbes {
		found := findUndefinedFunctions(p.text)
		require.NotEmpty(t, found, "probe %q must call a function the gate reads", p.text)
		for _, name := range found {
			require.Contains(t, strings.ToLower(p.answer), strings.ToLower(name),
				"probe %q entered %q into the catalogue, but the server's answer does not name it", p.text, name)
		}
	}
	require.Len(t, undefinedFunctions, len(undefinedFunctionProbes),
		"one probe, one name: a probe calling two functions, or two probes calling one, "+
			"makes the catalogue and the evidence stop lining up one for one")
}

// TestWitnessSweepFailsOnEachBrokenBinding is what keeps the sweep from
// being another green guard looking at nothing. Each row breaks exactly
// one binding in an otherwise sound gap and requires the specific
// complaint back; the last row hands it nothing at all, because a sweep
// an empty table satisfies has measured nothing and would go on passing
// as the real table was emptied.
//
// The rows are built from a gap that PASSES, which is asserted first, so
// a complaint below can only have come from the mutation named in the
// row.
func TestWitnessSweepFailsOnEachBrokenBinding(t *testing.T) {
	const witness = "TestSomethingLive"
	measured := "probe := \"MATCH (:A)-[r:X|Y]->(:B) RETURN r\"\n" +
		"served := \"MATCH (:A)-[r:X]->(:B) RETURN r\"\n" +
		"require.Contains(t, err.Message, `syntax error at or near \"|\"`)\n"
	bodies := map[string]string{
		witness: measured,
		// Declared and never run, carrying what the witness carries plus
		// three bindings of its own. The recipe row needs a witness the
		// live source DOES declare, or the declaration complaint fires
		// too and the row would pass without the recipe check existing;
		// the three extra bindings are what the "some other live test"
		// rows point a gap at.
		witness + "ButUnrun": measured +
			"elsewhere := \"MATCH (:A)-[r:M|N]->(:B) RETURN r\"\n" +
			"require.Contains(t, err.Message, `no relationship type by that name`)\n" +
			"acceptedElsewhere := \"MATCH (:A)-[r:W]->(:B) RETURN r\"\n",
	}
	// Anchored, and the row below is why: -run is an unanchored regexp,
	// so a bare `-run TestSomethingLive` selects TestSomethingLiveButUnrun
	// too and the "no recipe runs it" row would find the witness run.
	// The build tag is here because recipeRuns requires it — a command
	// line that does not build liveBuildTag compiles no live test.
	recipes := map[string]string{"run-it": "go test -tags " + liveBuildTag + " -run '^" + witness + "$'"}
	sound := dialectGap{
		sentinel: ErrRelationshipTypeAlternation,
		find:     findUndefinedFunctionsOrAlternations,
		diagnose: func(int, string, string) string { return "" },
		witness:  witness,
		refused: []dialectProbe{{
			text:   "MATCH (:A)-[r:X|Y]->(:B) RETURN r",
			answer: `syntax error at or near "|"`,
		}},
		served: []string{"MATCH (:A)-[r:X]->(:B) RETURN r"},
	}
	require.Empty(t, witnessGaps([]dialectGap{sound}, bodies, recipes),
		"the row template must pass, or a complaint below could come from the template")

	for _, tc := range []struct {
		name string
		// cut is the one binding this row cuts. A nil gaps slice
		// from it means the row is about the table and not about a gap.
		cut  func(g dialectGap) []dialectGap
		want string
	}{
		{
			name: "a gap with no probe refuses on nothing",
			cut:  func(g dialectGap) []dialectGap { g.refused = nil; return []dialectGap{g} },
			want: "refuses on no probe",
		},
		{
			name: "a probe the gate does not read is not evidence for this gate",
			cut: func(g dialectGap) []dialectGap {
				g.refused = []dialectProbe{{text: "MATCH (:A)-[r:X]->(:B) RETURN r", answer: "boom"}}
				return []dialectGap{g}
			},
			want: "is not read by this gap",
		},
		{
			name: "a probe no live test carries is never re-measured",
			cut: func(g dialectGap) []dialectGap {
				g.refused = []dialectProbe{{
					text:   "MATCH (:A)-[r:P|Q]->(:B) RETURN r",
					answer: `syntax error at or near "|"`,
				}}
				return []dialectGap{g}
			},
			want: "is not carried by",
		},
		{
			name: "an answer no live test asserts is a claim about a server nothing checks",
			cut: func(g dialectGap) []dialectGap {
				g.refused = []dialectProbe{{
					text:   "MATCH (:A)-[r:X|Y]->(:B) RETURN r",
					answer: "some other complaint entirely",
				}}
				return []dialectGap{g}
			},
			want: "is not asserted by",
		},
		{
			// The three rows below are the scoping half. A binding the
			// live SOURCE carries is not a binding the gap's WITNESS
			// carries, and only the second is a re-measurement: a probe
			// sitting in the neo4j battery, or under an AGE test the
			// recipes never name, is never run against the pinned image
			// no matter how many files spell it.
			name: "a probe some other live test carries is not re-measured by this witness",
			cut: func(g dialectGap) []dialectGap {
				g.refused = []dialectProbe{{
					text:   "MATCH (:A)-[r:M|N]->(:B) RETURN r",
					answer: `syntax error at or near "|"`,
				}}
				return []dialectGap{g}
			},
			want: "is not carried by",
		},
		{
			name: "an answer some other live test asserts is not asserted by this witness",
			cut: func(g dialectGap) []dialectGap {
				g.refused = []dialectProbe{{
					text:   "MATCH (:A)-[r:X|Y]->(:B) RETURN r",
					answer: "no relationship type by that name",
				}}
				return []dialectGap{g}
			},
			want: "is not asserted by",
		},
		{
			name: "a served text some other live test carries was not measured as served here",
			cut: func(g dialectGap) []dialectGap {
				g.served = []string{"MATCH (:A)-[r:W]->(:B) RETURN r"}
				return []dialectGap{g}
			},
			want: "is not carried by",
		},
		{
			name: "a gap recording no served text has nothing bounding its find",
			cut:  func(g dialectGap) []dialectGap { g.served = nil; return []dialectGap{g} },
			want: "records no served text",
		},
		{
			name: "a served text the gate refuses is the false positive this table exists to prevent",
			cut: func(g dialectGap) []dialectGap {
				g.served = []string{"MATCH (:A)-[r:X|Y]->(:B) RETURN r"}
				return []dialectGap{g}
			},
			want: "as served and its find refuses it",
		},
		{
			name: "a served text no live test carries was never measured as served",
			cut: func(g dialectGap) []dialectGap {
				g.served = []string{"MATCH (:A)-[r:Z]->(:B) RETURN r"}
				return []dialectGap{g}
			},
			want: "is not carried by",
		},
		{
			name: "a gap naming no witness names nothing to re-measure it",
			cut:  func(g dialectGap) []dialectGap { g.witness = ""; return []dialectGap{g} },
			want: "names no witness test",
		},
		{
			name: "a witness no live file declares does not exist",
			cut:  func(g dialectGap) []dialectGap { g.witness = "TestNotWritten"; return []dialectGap{g} },
			want: "is not declared in any live test file",
		},
		{
			name: "a witness no recipe runs never runs",
			cut: func(g dialectGap) []dialectGap {
				g.witness = "TestSomethingLiveButUnrun"
				return []dialectGap{g}
			},
			// The live file declares this one, so the declaration
			// complaint stays silent and what is left is the recipe's.
			// A witness CI never invokes is a measurement that never
			// happens, which is the failure mode a hardcoded -run
			// allowlist produces every time someone adds a live test.
			want: "is not run by recipe",
		},
		{
			name: "a gap with no sentinel gives a caller nothing to branch on",
			cut:  func(g dialectGap) []dialectGap { g.sentinel = nil; return []dialectGap{g} },
			want: "carries no sentinel",
		},
		{
			name: "a gap with no find reads nothing",
			cut:  func(g dialectGap) []dialectGap { g.find = nil; return []dialectGap{g} },
			want: "has no find",
		},
		{
			name: "a gap with no diagnose tells the author nothing",
			cut:  func(g dialectGap) []dialectGap { g.diagnose = nil; return []dialectGap{g} },
			want: "has no diagnose",
		},
		{
			// The vacuity row. Everything above is a complaint about a
			// gap, so all of them are reachable only through the loop —
			// and a loop over nothing runs no body. Without this the
			// whole sweep is satisfied by deleting the table.
			name: "an empty table has measured nothing",
			cut:  func(dialectGap) []dialectGap { return nil },
			want: "the dialect gap table is empty",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := witnessGaps(tc.cut(sound), bodies, recipes)
			require.NotEmpty(t, got, "this binding is cut, so the sweep has to complain")
			require.Contains(t, strings.Join(got, "\n"), tc.want)
		})
	}
}

// TestWitnessBodiesAreScopedToTheirOwnTest guards the reader rather than
// the sweep. witnessGaps is driven with a hand-built map, so every
// scoping row in TestWitnessSweepFailsOnEachBrokenBinding stays green
// even when readLiveWitnessBodies hands back whole files — which is the
// exact degradation the per-witness binding exists to rule out, and it is
// invisible to every other test here because a wider body is strictly
// more permissive.
//
// Both AGE witnesses are declared in one file and neither carries the
// other's probes, so a reader returning file text in place of body text
// fails here and nowhere else.
func TestWitnessBodiesAreScopedToTheirOwnTest(t *testing.T) {
	bodies := readLiveWitnessBodies(t)
	compared := 0
	for _, g := range dialectGaps {
		body, declared := bodies[g.witness]
		require.True(t, declared, "gap witness %s is not declared in any live test file", g.witness)
		for _, other := range dialectGaps {
			if other.witness == g.witness {
				continue
			}
			for _, p := range other.refused {
				compared++
				require.NotContains(t, body, p.text,
					"%s carries %s's probe %q, so the reader is not scoped to one test's body",
					g.witness, other.witness, p.text)
			}
		}
	}
	require.NotZero(t, compared,
		"one gap, or two sharing a witness: this test compared nothing and would pass on any reader")
}

// The bytes mutation M15 moved from code into a comment: one refused
// probe, the answer beside it, and a served text to keep the sweep's
// other complaints silent while the probe half is cut. Named once so the
// two tests below and the mutation that motivated them are about the same
// strings.
const (
	syntheticWitness    = "TestSomethingLive"
	syntheticProbeText  = "RETURN toTimestamp('2024-01-01')"
	syntheticProbeAns   = "function toTimestamp does not exist"
	syntheticServedText = "RETURN timestamp()"
)

// syntheticProbeRow is what a witness row costs in code: the text it runs
// and the answer it asserts.
const syntheticProbeRow = "\tprobe := \"" + syntheticProbeText + "\"\n" +
	"\trequire.Contains(t, err.Message, \"" + syntheticProbeAns + "\")\n" +
	"\t_ = probe\n"

// liveWitnessSource is a live test file declaring one witness, with the
// served text always in code and the refused probe wherever row puts it.
//
// Written here rather than read from the repo, which is the reason
// witnessBodies is a separate function at all: this repo's live files
// comment no probe out, so nothing in them can tell a reader that keeps
// comments from one that drops them. doc goes above the declaration, row
// inside the body.
func liveWitnessSource(doc, row string) []byte {
	return []byte("//go:build codegen_live\n\npackage fixtures_test\n\n" + doc +
		"func " + syntheticWitness + "(t *testing.T) {\n" +
		"\tserved := \"" + syntheticServedText + "\"\n" +
		row +
		"\t_ = served\n}\n")
}

// commentOut is a block of code behind `//`, byte for byte. Derived
// rather than written out a second time, so the commented row cannot
// drift from the row it is the commenting-out of — a guard whose two
// halves spell different strings would pass for the wrong reason.
func commentOut(block string) string {
	var out strings.Builder
	for _, line := range strings.Split(strings.TrimSuffix(block, "\n"), "\n") {
		out.WriteString("\t//" + line + "\n")
	}
	return out.String()
}

// readSyntheticWitness is witnessBodies over one hand-written file, with
// the parse failure surfaced as a test failure.
func readSyntheticWitness(t *testing.T, src []byte) map[string]string {
	t.Helper()
	bodies, err := witnessBodies(token.NewFileSet(), "live_synthetic_test.go", src)
	require.NoError(t, err, "the source this test writes has to parse")
	require.Contains(t, bodies, syntheticWitness, "the witness this test declares has to be read back")
	return bodies
}

// TestWitnessBodyIsCodeAndNotCommentary guards the reader's second axis.
// TestWitnessBodiesAreScopedToTheirOwnTest rules out a body that is too
// WIDE — one test's body carrying another's. This rules out one that
// spans the right region and holds the wrong CONTENT: the comments inside
// it, which are the only thing in a Go file that can spell a measurement
// without running one.
//
// The first row is the load-bearing one. Every other row asserts an
// absence, and a reader that returned "" would satisfy all of them at
// once while making the whole sweep vacuous.
func TestWitnessBodyIsCodeAndNotCommentary(t *testing.T) {
	for _, tc := range []struct {
		name string
		doc  string
		row  string
		// carried is whether the probe and its answer must be found in
		// the rendered body.
		carried bool
	}{
		{
			name:    "a probe the witness runs is carried",
			row:     syntheticProbeRow,
			carried: true,
		},
		{
			name: "a probe commented out line by line is not carried",
			row:  commentOut(syntheticProbeRow),
		},
		{
			name: "a probe inside a block comment is not carried",
			row:  "\t/*\n" + syntheticProbeRow + "\t*/\n",
		},
		{
			// Outside the braces entirely. A row retired into the
			// witness's doc comment is the tidiest spelling of the same
			// move, and the one a reader keying on Pos()/End() would
			// happen to miss — so it is here for the boundary and not
			// because it is the hard case.
			name: "a probe in the witness's doc comment is not carried",
			doc:  commentOut(syntheticProbeRow),
			row:  "",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := readSyntheticWitness(t, liveWitnessSource(tc.doc, tc.row))[syntheticWitness]
			require.Contains(t, body, syntheticServedText,
				"the body's own code must be read back, or every assertion below passes on an empty string")
			if tc.carried {
				require.Contains(t, body, syntheticProbeText)
				require.Contains(t, body, syntheticProbeAns)
				return
			}
			require.NotContains(t, body, syntheticProbeText,
				"a commented-out probe is spelled, not run, and a body carrying it lets the gap table grow on prose")
			require.NotContains(t, body, syntheticProbeAns,
				"the answer half too: a comment quoting the server is not a test asserting it")
		})
	}
}

// TestACommentedProbeReddensTheSweep runs the reader into the sweep,
// which is where this property lives — neither half holds it alone.
//
// witnessGaps is a pure function over a bodies map, so no row in
// TestWitnessSweepFailsOnEachBrokenBinding can express "the probe is in a
// comment": a map value carrying a comment is a string carrying a
// comment, and Contains finds it. Only the reader can tell code from
// commentary and only the sweep can complain, so the guard has to compose
// them.
//
// This is mutation M15 at the unit level. In the tree it read: replace
// TestAGERefusesTheFunctionsItDoesNotDefine's toTimestamp row with a
// comment spelling the same probe text and the same server answer, and
// TestEveryDialectGapCarriesItsWitness stayed green.
func TestACommentedProbeReddensTheSweep(t *testing.T) {
	gap := dialectGap{
		sentinel: ErrUndefinedFunction,
		find:     findUndefinedFunctions,
		diagnose: func(int, string, string) string { return "" },
		witness:  syntheticWitness,
		refused:  []dialectProbe{{text: syntheticProbeText, answer: syntheticProbeAns}},
		served:   []string{syntheticServedText},
	}
	recipes := map[string]string{"run-it": "go test -count=1 -tags " + liveBuildTag + " -run " + syntheticWitness}

	run := readSyntheticWitness(t, liveWitnessSource("", syntheticProbeRow))
	require.Empty(t, witnessGaps([]dialectGap{gap}, run, recipes),
		"the template must pass, or the complaints below could come from the template")

	spelled := readSyntheticWitness(t, liveWitnessSource("", commentOut(syntheticProbeRow)))
	got := strings.Join(witnessGaps([]dialectGap{gap}, spelled, recipes), "\n")
	require.Contains(t, got, "is not carried by",
		"a probe only a comment spells is measured by nothing")
	require.Contains(t, got, "is not asserted by",
		"and neither is the answer the comment quotes")
}

// findUndefinedFunctionsOrAlternations is the test template's find: it
// reads the alternation the template probes and the function names the
// real table refuses, so a row can cut either binding without needing a
// second template. Test-only — the shipped table pairs one find per gap.
func findUndefinedFunctionsOrAlternations(src string) []string {
	if found := findUndefinedFunctions(src); len(found) > 0 {
		return found
	}
	return cypher.RelationshipTypeAlternations(src)
}

// witnessGaps is the sweep, expressed as the complaints it has rather
// than as a bool, so a failure names the binding that broke and not just
// that one did. Empty means every gap in gaps is witnessed.
//
// It is a pure function of the table and the two artefacts, which is
// what lets TestWitnessSweepFailsOnEachBrokenBinding drive it with a
// deliberately broken table and confirm each complaint is reachable. A
// sweep written inline in its own assertion cannot be tested that way,
// and an untested sweep is exactly the guard this codebase keeps finding
// green because it looks at nothing.
//
// bodies maps a live test's name to the source of its body; recipes maps
// a recipe name to the commands it runs.
//
// Per WITNESS and not over the live corpus as a whole, which is the
// difference between a probe that is re-measured and a probe that is
// merely spelled somewhere. A text carried by the neo4j battery, or by
// an AGE test no recipe runs, is never run against the pinned image —
// and a sweep reading every file at once cannot tell those apart from
// the real thing.
func witnessGaps(gaps []dialectGap, bodies, recipes map[string]string) []string {
	var complaints []string
	say := func(format string, args ...any) {
		complaints = append(complaints, fmt.Sprintf(format, args...))
	}

	// The vacuity guard, and the reason this returns complaints from a
	// slice it was handed rather than reading the package variable
	// itself: every other complaint below is inside the loop, so an
	// empty table would satisfy the sweep by giving it nothing to look
	// at. That is this epic's most common defect and it would be this
	// file's own if the check were missing.
	if len(gaps) == 0 {
		return []string{"the dialect gap table is empty, so this sweep has measured nothing"}
	}

	for i, g := range gaps {
		id := fmt.Sprintf("gap %d", i)
		if g.witness != "" {
			id = fmt.Sprintf("gap %d (%s)", i, g.witness)
		}
		if g.sentinel == nil {
			say("%s carries no sentinel, so a caller has nothing to branch on", id)
		}
		if g.diagnose == nil {
			say("%s has no diagnose, so its refusal would tell the author nothing", id)
		}
		if g.find == nil {
			// Everything below calls find, so this gap can be checked
			// no further.
			say("%s has no find, so it reads nothing out of a query text", id)
			continue
		}

		// Empty where the gap names no witness or names one no live file
		// declares, so every binding below is reported unmeasured too —
		// which is what it is.
		body := bodies[g.witness]
		switch _, declared := bodies[g.witness]; {
		case g.witness == "":
			say("%s names no witness test, so nothing re-measures it against the pinned image", id)
		case !declared:
			say("%s names witness %q, which is not declared in any live test file", id, g.witness)
		}
		if g.witness != "" {
			for name, cmds := range recipes {
				if !recipeRuns(cmds, g.witness) {
					say("%s names witness %q, which is not run by recipe %s", id, g.witness, name)
				}
			}
		}

		if len(g.refused) == 0 {
			say("%s refuses on no probe, so its refusal rests on nothing measured", id)
		}
		for _, p := range g.refused {
			switch {
			case p.text == "":
				say("%s carries a probe with no text", id)
				continue
			case len(g.find(p.text)) == 0:
				say("%s carries probe %q, which is not read by this gap — "+
					"so the measurement is of something the gate does not refuse", id, p.text)
			}
			if !strings.Contains(body, p.text) {
				say("%s carries probe %q, which is not carried by %s", id, p.text, g.witness)
			}
			switch {
			case p.answer == "":
				say("%s carries probe %q with no recorded answer", id, p.text)
			case !strings.Contains(body, p.answer):
				say("%s records answer %q, which is not asserted by %s", id, p.answer, g.witness)
			}
		}

		if len(g.served) == 0 {
			say("%s records no served text, so nothing bounds what its find refuses", id)
		}
		for _, s := range g.served {
			if len(g.find(s)) > 0 {
				say("%s records %q as served and its find refuses it", id, s)
			}
			if !strings.Contains(body, s) {
				say("%s records served text %q, which is not carried by %s", id, s, g.witness)
			}
		}
	}
	return complaints
}

// witnessBodies is every top-level test one live file declares, paired
// with its body rendered back from the parse — the CODE of the body, and
// not the bytes the body occupies.
//
// That distinction is the whole of this function. Taking the body as
// src[fn.Body.Pos():fn.Body.End()] hands back the comments inside it too,
// so a probe row commented out still "appears in the witness's body" and
// the sweep goes on passing over a measurement that no longer runs
// against any server. The gap table could then grow on evidence that is
// spelled rather than run, which is the one property this file exists to
// deny. Measured on this branch as mutation M15: commenting out the
// toTimestamp row of TestAGERefusesTheFunctionsItDoesNotDefine, with the
// probe text and the server's answer both spelled inside the comment,
// left TestEveryDialectGapCarriesItsWitness green.
//
// What drops the comments is format.Node over the BLOCK: a comment is not
// a statement, it hangs off the *ast.File, and printing a bare
// *ast.BlockStmt prints the statements alone. The mode-0 parse is a second
// reason for the same outcome and is measured NOT to be the load-bearing
// one — switching it to parser.ParseComments leaves all four rows of
// TestWitnessBodyIsCodeAndNotCommentary green (mutation M21). Reopening
// the hole takes printing a *printer.CommentedNode instead, which those
// rows do catch (mutation M24).
//
// Parsed rather than scanned for "func <name>(", for the same reason the
// gate it audits parses: a scan cannot tell a declaration from a string
// literal spelling one, and it has no way to find where a body ends
// short of assuming what the formatter puts in column zero. go/parser
// reads the build-tagged file without honouring the tag, which is what
// lets a test binary built without it read one.
//
// Methods are skipped: a witness is a top-level test function, and a
// method sharing its name would put a body under a name `go test -run`
// cannot select.
//
// Split from readLiveWitnessBodies for the reason recipeBodies is split
// from readRecipes: a reader that only ever runs over the repo's own
// files cannot be shown to tell code from commentary, because the repo's
// files comment nothing out. This one takes source a test can write.
func witnessBodies(fset *token.FileSet, path string, src []byte) (map[string]string, error) {
	file, err := parser.ParseFile(fset, path, src, 0)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	bodies := make(map[string]string)
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv != nil || fn.Body == nil {
			continue
		}
		var body strings.Builder
		if err := format.Node(&body, fset, fn.Body); err != nil {
			return nil, fmt.Errorf("render %s's body in %s: %w", fn.Name.Name, path, err)
		}
		bodies[fn.Name.Name] = body.String()
	}
	return bodies, nil
}

// readLiveWitnessBodies is witnessBodies over every live test file. Read
// from disk rather than imported: the files are in another Go module and
// behind a build tag this binary is not compiled with, so a source-level
// read is the only binding available from here — and it is the binding
// that matters, since what has to agree is the probe text on both sides.
//
// A name declared by two files fails here rather than resolving to
// whichever was globbed last. It cannot arise today: every file liveGlob
// matches declares `package fixtures_test`, so Go rejects the clash
// across the whole set, not merely within one file. The guard is for a
// live file that later lands outside that package — liveGlob matches by
// path and says nothing about a package clause.
func readLiveWitnessBodies(t *testing.T) map[string]string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(repoRoot, liveGlob))
	require.NoError(t, err)
	require.NotEmpty(t, paths, "no live test files found at %s — the sweep would pass by reading nothing", liveGlob)

	fset := token.NewFileSet()
	bodies := make(map[string]string)
	for _, p := range paths {
		src, err := os.ReadFile(p) //nolint:gosec // a repo-relative path this test builds itself
		require.NoError(t, err, "read %s", p)
		read, err := witnessBodies(fset, p, src)
		require.NoError(t, err, "read the test bodies in %s", p)
		for name, body := range read {
			require.NotContains(t, bodies, name,
				"two live tests named %s: the sweep would read whichever was parsed last", name)
			bodies[name] = body
		}
	}
	return bodies
}

// recipeRuns reports whether cmds actually runs the live top-level test
// named witness — every probe row of it. Five things have to hold: the
// quoting closes, the body runs a `go test`, and THAT invocation builds
// liveBuildTag, has every -run selecting the witness WHOLE, and has no
// -skip reaching it. Each is a way a recipe was measured running no
// witness while this function said yes.
//
// The last three are read from one invocation's own fields, which is the
// round-4 correction. They were read from the whole body while the
// `go test` was found separately, so a `-tags codegen_live` on any OTHER
// command satisfied the tag requirement for a `go test` carrying none:
// review mutation POOLTAG2 put this justfile's own fence idiom
// (`go vet -tags codegen_live ./...`, line 230) on the line above and
// dropped the tag off the test, which restored review mutation T1 with
// one added line — measured, the recipe then printed "[no test files]"
// for all 154 packages under test/data/codegen and exited 0, starting no
// container and running no witness, while the sweep stayed green.
//
// This is the half strings.Contains could not say, and the same defect
// class as reading a witness's body as source bytes (see witnessBodies).
// Both AGE recipes already carry a -skip, so a witness name in the
// command line is as likely to be there to REMOVE the test as to select
// it; and a name in a comment is not in the command line at all, which
// is why recipeBodies strips comments before this ever sees them.
// Measured as review mutations L18/L19/L20 — all three left the sweep
// green while no CI job ran the witness.
//
// "Whole" is the round-3 correction and it is the asymmetry that makes
// this function worth having. go test reads the elements after the first
// as a SUBTEST filter, and both AGE witnesses run every probe row as a
// subtest (live_age_dialect_test.go), so a -run of `W/toTimestamp` runs
// one probe of five and a -run of `W/x` runs none at all. Both print
// `--- PASS: W` and exit 0. Measured against go1.26.5: the second prints
// `[no tests to run]` ONLY when nothing else on the command line
// matched, so in the shape a live recipe has — `-run 'TestLiveSmoke|W/x'`,
// where the smoke battery still runs — even that tell is absent. Until
// this rule was added, a -skip carving out ONE probe was refused while a
// -run dropping ALL TWELVE was accepted (review mutations MC, MA, MB).
// So an alternative counts as selecting the witness only when it has no
// further elements; a narrowed one is read as not selecting it.
//
// The remaining approximations, in the direction each fails:
//
//   - Within one invocation, every -run must select the witness, where
//     go test honours only the last one. Complaint. (Across invocations
//     the rule is the other way round, and as a statement about
//     SELECTION it is exact: a body whose second `go test` selects the
//     witness is reported as running it. Whether that second command is
//     reached is a different question and a silence — see
//     goTestInvocations, review mutation P3.)
//   - Any -skip alternative whose FIRST element matches the witness
//     counts as skipping it, where go test would drop only a subtest.
//     Complaint, and the same direction as the -run rule above.
//   - Within one invocation, every -tags value must carry liveBuildTag,
//     where go test honours the last. Complaint.
//   - Only the command line is read. Where the package argument points
//     is not checked (review mutation T2) — that is a question about the
//     file system rather than about flags, and it is left open. Silence.
//   - Whether the command line is reached is not read either. A `go test`
//     after `||`, or before a failing `&&`, counts as running the witness
//     (review mutation P3). Silence, and see goTestInvocations for why
//     the same superset is a complaint for the -count=1 rule.
func recipeRuns(cmds, witness string) bool {
	invocations, unterminated := goTestInvocations(cmds)
	// A line this reader could not finish parsing is not a line it can
	// report on. It arises when stripRecipeComment cuts inside a quoted
	// argument, and answering from the fragment is how "nothing to read"
	// became "runs everything" (review mutation V3).
	if unterminated {
		return false
	}
	// A body that runs no `go test` builds no test binary and so runs no
	// witness, and this loop says so by having nothing to iterate rather
	// than by reading empty flag slices off a command line that is not a
	// test run at all: review mutation V1, green while the sweep read no
	// go test invocation whatsoever.
	for _, fields := range invocations {
		if invocationRuns(fields, witness) {
			return true
		}
	}
	return false
}

// invocationRuns is recipeRuns' question about ONE `go test` command
// line, whose fields are the command's own and nobody else's.
func invocationRuns(fields []string, witness string) bool {
	tags := testFlagValues(fields, "tags")
	if len(tags) == 0 {
		return false
	}
	for _, value := range tags {
		if !slices.Contains(strings.FieldsFunc(value, func(r rune) bool {
			return r == ',' || r == ' '
		}), liveBuildTag) {
			return false
		}
	}
	for _, pattern := range testFlagValues(fields, "run") {
		if _, wholly := selects(pattern, witness); !wholly {
			return false
		}
	}
	for _, pattern := range testFlagValues(fields, "skip") {
		if reaches, _ := selects(pattern, witness); reaches {
			return false
		}
	}
	return true
}

// goTestInvocations is every `go test` command a recipe body CONTAINS in
// command position, each as the fields of that command ALONE — up to the
// operator that ends it, and not one field further. unterminated says
// some line's quoting never closed, which makes every field of that line
// a guess.
//
// Contains, not runs: this is a SUPERSET of what the body executes,
// because a command after `||` runs only when the one before it failed,
// and a command before `&&` can leave the rest of the line unreached.
// The two callers take that in opposite directions, so it is not one
// safe direction:
//
//   - recipeBodies requires -count=1 of EVERY invocation, so a command
//     that is counted and not run adds a requirement. Complaint.
//   - recipeRuns is satisfied by SOME invocation selecting the witness,
//     so a command that is counted and not run can answer yes for a body
//     that runs no witness. Silence, measured: review mutation P3 put
//     `go test -run 'TestLiveSmoke' … || go test -run '<the full set>' …`
//     in the live recipe and the sweep stayed green over a body whose
//     witness invocation runs only on the smoke battery's failure.
//
// Running is also not gating. `… || true` appended to the real recipe
// (review mutation P4; `|| true` is one of this justfile's own idioms)
// leaves the witness running and the recipe failing on nothing, and this
// reader says nothing about it, because its claim is about what runs.
//
// Reading flags from the command that runs them is the whole of this
// function, and it is why callers get segments rather than a yes/no. The
// predecessor pair — "does a `go test` appear anywhere" plus "does a
// -tags appear anywhere" — is satisfied by a body where those are two
// different commands (review mutations POOLTAG2, POOLTAG, CNT1, CNT2,
// POOLRUN2), and every one of them is silence.
//
// A command starts a line of the body or follows `&&`, `||`, `;` or `|`,
// and runs `go test` when its first field is `go` — or a path ending in
// one — and its second is `test`. Command position is what stops
// `echo go test -run W` from counting: searching every argument for `go`
// beside `test` counts a line that only prints the words.
// `cd test/data/codegen && go test …` is the shape both AGE recipes have,
// and a body that puts the `cd` on its own line works for the other
// reason.
//
// Three things are consequently read as running no go test at all, and
// so as a recipe that does not run the witness — all complaints: a
// compiled test binary invoked directly, a script that runs go test
// inside itself, and an environment prefix (`GOFLAGS=… go test`), which
// none of the three live recipes writes today. An operator not
// surrounded by spaces (`a&&go
// test`) is a fourth: the fields are split on whitespace, so `a&&go` is
// one field and no command starts there.
func goTestInvocations(cmds string) (invocations [][]string, unterminated bool) {
	for _, line := range strings.Split(cmds, "\n") {
		fields, quoted := shellFields(line)
		if quoted {
			unterminated = true
		}
		start := 0
		for i := 0; i <= len(fields); i++ {
			if i < len(fields) {
				switch fields[i] {
				case "&&", "||", ";", "|":
				default:
					continue
				}
			}
			if command := fields[start:i]; len(command) > 1 &&
				(command[0] == "go" || strings.HasSuffix(command[0], "/go")) &&
				command[1] == "test" {
				invocations = append(invocations, command)
			}
			start = i + 1
		}
	}
	return invocations, unterminated
}

// selects reports how a `go test -run` / `-skip` pattern reaches the
// TOP-LEVEL test named name: reaches is whether some alternative matches
// it at all, wholly is whether some alternative matches it with no
// further elements. The two differ exactly when a pattern narrows to
// subtests, and that difference is what recipeRuns is built on — a -skip
// that reaches counts as skipping, a -run must select wholly.
//
// go test splits a pattern on top-level `|` into alternatives first and
// each alternative on `/` into elements, then matches element i against
// the i'th part of a test's name with an UNANCHORED regexp match
// (testing/match.go: splitRegexp, alternationMatch.matches,
// simpleMatch.matches). So a pattern reaches a top-level test when some
// alternative's first element matches it as a regexp: `TestAGERefuses`
// selects TestAGERefusesTheFunctionsItDoesNotDefine, and string equality
// is the wrong question in both directions. Verified against go1.26.5
// rather than read off the docs — `-skip 'TestLiveSmoke/neo4j|W'` skips
// W outright, because the appended text is a second alternative and not
// a second element; and `-run 'TestLiveSmoke|W/x'` leaves W selected,
// running none of its subtests, at exit 0.
//
// Every alternative is read even after one matches, because a narrowed
// alternative can be followed by a whole one. A pattern with an
// uncompilable alternative is therefore refused even when an earlier
// alternative already matched: doubt REACHES everything and selects
// nothing whole, which is the refusal both callers want — a -run this
// reader cannot compile does not select the witness, and a -skip it
// cannot compile might drop it. A complaint either way, never silence,
// and both directions have a row ("a -run this reader cannot compile",
// "a -skip this reader cannot compile").
//
// That is why there is no error return. There was one until round 4, and
// only the -skip loop could act on it: an error came back with reaches
// false, which is the answer that LETS a -skip through, while for -run
// wholly was false already. So the -run loop carried an err arm that
// nothing could make true (review mutation G3b, dead) and the -skip
// loop carried the only live one, unprobed until its row was written
// (G3). Two truths in one value made one of them unreachable.
//
// The split here is naive where go test's is bracket-aware: a `|` or `/`
// inside `[...]` or `(...)` is top-level to this function and is not to
// go test. That direction is deliberate. The pieces a naive split makes
// of `TestFoo(A|B)` do not compile.
func selects(pattern, name string) (reaches, wholly bool) {
	for _, alt := range strings.Split(pattern, "|") {
		head, _, narrowed := strings.Cut(alt, "/")
		re, err := regexp.Compile(head)
		if err != nil {
			return true, false
		}
		if !re.MatchString(name) {
			continue
		}
		reaches = true
		if !narrowed {
			wholly = true
		}
	}
	return reaches, wholly
}

// testFlagValues is every value a command line gives one go test flag,
// in any of the spellings go's flag package accepts: `-flag value`,
// `-flag=value`, `--flag`, and the `-test.flag` form a compiled binary
// takes.
func testFlagValues(fields []string, flag string) []string {
	var values []string
	for i := 0; i < len(fields); i++ {
		name, value, assigned := strings.Cut(fields[i], "=")
		if strings.TrimPrefix(strings.TrimLeft(name, "-"), "test.") != flag {
			continue
		}
		switch {
		case assigned:
			values = append(values, value)
		case i+1 < len(fields):
			values = append(values, fields[i+1])
			i++
		default:
			// A flag with nothing after it. go test rejects the command
			// line, but this reader still has to answer, and the empty
			// pattern is the one that selects everything — which for a
			// -skip means the witness is skipped and complained about.
			values = append(values, "")
		}
	}
	return values
}

// shellFields splits a recipe's command line into arguments, honouring
// single and double quotes so `-run 'A|B'` stays one argument. The
// second return says a quote was still open at the end — the line the
// caller handed over is not a line this reader finished.
//
// Not a shell: no escapes, no expansion, no operators, no substitution.
// It exists to find -run, -skip and -tags and the values beside them.
//
// The two flags fail in OPPOSITE directions when it is defeated, and
// naming only the safe one is how round 3 found this comment wrong.
// Expansion is the case that matters, because a recipe may write
// `SKIP='TestLiveSmoke/neo4j|W' && go test … -skip "$SKIP"`, which is one
// executable line:
//
//   - For -run, an argument this reader takes literally selects nothing,
//     and recipeRuns reports a witness that does not run. Complaint.
//   - For -skip, the same literal argument SKIPS nothing, so the witness
//     reads as running while the recipe skips it outright. Silence, and
//     the shape this file exists to forbid. Measured as review mutation
//     V4b: `$SKIP` and `${SKIP}` both compile as regexps and match no
//     test name, `$` being the end-of-text anchor.
//   - For -tags, a literal `$TAGS` carries no build tag, so the recipe
//     reads as building none of the live battery. Complaint.
//
// Closing that means being a shell, which is not a cost this check is
// worth; it is stated rather than fixed. No live recipe expands a
// variable into a test pattern today, and the justfile's own shell
// variables are elsewhere.
func shellFields(s string) (fields []string, unterminated bool) {
	var (
		cur   strings.Builder
		quote rune
		open  bool
	)
	for _, r := range s {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote, open = r, true
		case unicode.IsSpace(r):
			if open {
				fields = append(fields, cur.String())
				cur.Reset()
				open = false
			}
		default:
			cur.WriteRune(r)
			open = true
		}
	}
	if open {
		fields = append(fields, cur.String())
	}
	return fields, quote != 0
}

// stripRecipeComment drops a recipe line's shell comment: everything
// from the first `#` that starts a word outside quotes, which is where
// sh starts one. This is the recipe artefact's half of the property
// witnessBodies holds for Go source: text that is spelled is not text
// that runs.
//
// It cut at the FIRST `#` of any kind until round 3, and the claim that
// this was "the safe direction — the worst it can do is complain about a
// witness the recipe does run" was false. Review mutation V3:
// `go test -count=1 -tags codegen_live -ldflags '-X main.p=a#b' -run 'TestLiveSmoke' …`
// keeps -count=1 and loses -run, and recipeRuns read the flagless
// remainder as selecting everything. Silence over a recipe that runs no
// witness — the exact shape of L20 with the cut moved.
//
// Word-start-outside-quotes is sh's own rule, so for the shapes this
// reader models the cut is where sh puts it — an unquoted `-X p=a#b`
// keeps its `#`, because that one does not start a word.
//
// Of those two halves only word-start carries the answer. Dropping it
// cuts a live -run away and the flagless remainder runs everything
// ("an unquoted # inside a word is not a comment either" — mutation
// G14w). Dropping the QUOTE half changes no answer, measured: a cut
// inside a quoted argument removes that argument's closing quote along
// with everything after it, so shellFields reports unterminated and
// recipeRuns refuses the line anyway (mutation G14q survives). It is
// kept because it is sh's rule and because the redundancy belongs to
// the unterminated check rather than to this function — but it is
// redundancy, not a second guard, and both paths are complaints.
//
// What is NOT modelled is backslash escapes, command substitution,
// heredocs and here-strings. This justfile uses all four, so the bound
// is not "the artefact has none of them"; it is that this reader only
// ever sees ageLiveRecipes' two bodies, and those use none of them.
// That is a property of two single-line recipes today and not a law
// about the file, so it is held by
// TestTheRecipesThisReaderParsesStayInsideTheShellItModels rather than
// asserted here.
//
// Each cuts where sh would not: `\#` is a literal to sh, a `#` inside
// `$(…)` opens a comment in the substitution and not in the line around
// it, and every `#` in heredoc or here-string data is data. The remainder
// after a wrong cut either fails to close its quoting, which recipeRuns
// refuses, or it parses with fewer flags than the shell runs — and the
// second is silence, not a complaint, when what was lost is a -run that
// did not select the witness. That is V3's shape again, one layer down;
// it is bounded rather than closed.
func stripRecipeComment(line string) string {
	var quote rune
	startsWord := true
	for i, r := range line {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			}
		case r == '\'' || r == '"':
			quote = r
		case r == '#' && startsWord:
			return line[:i]
		}
		startsWord = quote == 0 && unicode.IsSpace(r)
	}
	return line
}

// recipeBodies is what each named recipe runs, read out of justfile
// source, alongside the complaints about the ones that are missing or
// that would report on a cached run.
//
// Comments are stripped as the body is read, so everything downstream —
// the -count=1 check here and recipeRuns over the result — is reading
// what the shell executes rather than what the justfile spells.
//
// Split from readRecipes for the reason witnessGaps is split from its
// assertion: a require written inline in a helper cannot be shown to
// fail, and this file's own argument is that a guard nothing can falsify
// is a guard looking at nothing.
func recipeBodies(src string, names []string) (map[string]string, []string) {
	var complaints []string
	// The vacuity guard. Every complaint below is inside the loop, so an
	// empty name list would make "no recipe runs this witness" unsayable
	// while the sweep went on passing.
	if len(names) == 0 {
		complaints = append(complaints, "no recipe was named, so nothing checks that a witness is ever run")
	}

	lines := strings.Split(src, "\n")
	out := make(map[string]string, len(names))
	seen := make(map[string]bool, len(names))
	for _, name := range names {
		// One name, one entry. What this returns is keyed by name and
		// witnessGaps complains once per ENTRY, so a name given twice
		// hands back a map one short while every count over the name LIST
		// is still right, and a recipe leaves the sweep: review mutation
		// D1 replaced "test-codegen-live" with a second
		// "test-codegen-live-age" (two names, one distinct) and left this
		// package green. D2CONSEQ then gutted test-codegen-live's -run
		// down to a non-witness with that duplicate in place — a live
		// recipe that had stopped running its witness, complained about
		// by nothing.
		if seen[name] {
			complaints = append(complaints,
				fmt.Sprintf("recipe %s is named twice, so one recipe is read where two were named", name))
			continue
		}
		seen[name] = true

		var body []string
		for i, line := range lines {
			if line != name+":" {
				continue
			}
			for _, next := range lines[i+1:] {
				if next == "" || !strings.HasPrefix(next, " ") && !strings.HasPrefix(next, "\t") {
					break
				}
				body = append(body, stripRecipeComment(next))
			}
			break
		}
		if len(body) == 0 {
			complaints = append(complaints,
				fmt.Sprintf("recipe %s is not in the justfile, so nothing this sweep says about it is true", name))
			continue
		}
		joined := strings.Join(body, "\n")
		// EVERY go test this recipe runs, and not "the bytes -count=1
		// appear somewhere in the body". The byte-level form is this
		// file's own condemned defect one property over, and it is
		// silent: review mutation CNT1 moved the real -count=1 into an
		// -ldflags value, and CNT2 put it on a separate
		// `go test ./valid/...` ahead of the live invocation. Both left
		// the sweep green over a recipe whose live run reports on a
		// cache. "Every" rather than "some" is what ties the flag to the
		// invocation recipeRuns cares about without this function
		// knowing which witness that is.
		//
		// A body running no go test at all is vacuously silent here, and
		// deliberately: "this recipe runs no test" is recipeRuns'
		// answer, and the sweep asks it of every gap.
		//
		// unterminated is dropped rather than answered: a line whose
		// quoting never closes makes recipeRuns refuse the whole body, so
		// the sweep already complains that this recipe runs no witness,
		// and it does so for every gap, since a gap naming no witness is
		// its own complaint. The row is "a command line whose quoting
		// never closes" in TestRecipeRunsOnlyWhatTheCommandLineSelects.
		invocations, _ := goTestInvocations(joined)
		for _, fields := range invocations {
			if !slices.Contains(testFlagValues(fields, "count"), "1") {
				complaints = append(complaints,
					fmt.Sprintf("recipe %s reports on a cached run: a witness is a measurement or it is nothing", name))
				break
			}
		}
		out[name] = joined
	}
	return out, complaints
}

// readRecipes is recipeBodies over the repo's own justfile. A recipe that
// is gone fails here rather than skipping a check silently, because the
// sweep's recipe complaint is written over what this returns: an empty
// map would make "no recipe runs this witness" unsayable.
func readRecipes(t *testing.T, names []string) map[string]string {
	t.Helper()
	src, err := os.ReadFile(filepath.Join(repoRoot, justfilePath))
	require.NoError(t, err, "read the justfile")
	out, complaints := recipeBodies(string(src), names)
	require.Empty(t, complaints, "the recipes this sweep reads have to be the ones CI runs")
	return out
}

// TestRecipeReaderComplainsOnEachBrokenRecipe cuts each recipe binding in
// turn against justfile source it writes itself, so the reader is shown
// to fail rather than assumed to. Without it the -count=1 requirement is
// an assertion nothing can falsify — deleting it leaves the whole package
// green, which is measured (bd note on this branch: mutation M8).
func TestRecipeReaderComplainsOnEachBrokenRecipe(t *testing.T) {
	const recipe = "test-codegen-live-age"
	const sound = "some-earlier-recipe:\n    go build ./...\n\n" +
		recipe + ":\n    cd test/data/codegen && go test -count=1 -run 'TestAGERefuses' ./...\n"
	names := []string{recipe}

	bodies, complaints := recipeBodies(sound, names)
	require.Empty(t, complaints, "the template must pass, or a complaint below could come from the template")
	require.Contains(t, bodies[recipe], "TestAGERefuses", "the body read must be the recipe's own")
	require.NotContains(t, bodies[recipe], "go build", "a recipe's body stops at the next recipe")

	for _, tc := range []struct {
		name  string
		src   string
		names []string
		want  string
	}{
		{
			name:  "a recipe that is not in the justfile",
			src:   "some-other-recipe:\n    go test -count=1 ./...\n",
			names: names,
			want:  "is not in the justfile",
		},
		{
			name:  "a recipe that would report on a cached run",
			src:   recipe + ":\n    cd test/data/codegen && go test -run 'TestAGERefuses' ./...\n",
			names: names,
			want:  "reports on a cached run",
		},
		{
			// The comment axis on the recipe artefact, the same property
			// witnessBodies holds for Go source. A reader keeping comments
			// finds -count=1 here and stays silent over a recipe that
			// caches — and, worse, would find a witness name in a comment
			// and call it run (review mutation L20).
			name:  "a -count=1 only a comment spells",
			src:   recipe + ":\n    # -count=1 is what this used to pass\n    cd test/data/codegen && go test -run 'TestAGERefuses' ./...\n",
			names: names,
			want:  "reports on a cached run",
		},
		{
			name:  "a trailing comment does not carry -count=1 either",
			src:   recipe + ":\n    cd test/data/codegen && go test -run 'TestAGERefuses' ./...  # was -count=1\n",
			names: names,
			want:  "reports on a cached run",
		},
		{
			// Review mutation CNT1. The bytes stay and the flag goes:
			// searching the body for "-count=1" found it inside an
			// -ldflags value, which is the byte-level reading this file
			// exists to refuse, one property over. The reader asks the
			// invocation for its own count flag instead.
			name:  "a -count=1 only an -ldflags value spells",
			src:   recipe + ":\n    cd test/data/codegen && go test -ldflags '-X main.x=-count=1' -run 'TestAGERefuses' ./...\n",
			names: names,
			want:  "reports on a cached run",
		},
		{
			// Review mutation CNT2. The flag is real, and it is on a
			// different command from the one that runs the battery. This
			// row is why the requirement is EVERY invocation and not
			// some: this function is not told which witness matters, so
			// "some invocation is uncached" would leave the one that
			// runs the witness free to report on a cache.
			name: "a -count=1 on a different go test",
			src: recipe + ":\n    cd test/data/codegen && go test -count=1 ./valid/...\n" +
				"    cd test/data/codegen && go test -run 'TestAGERefuses' ./...\n",
			names: names,
			want:  "reports on a cached run",
		},
		{
			// Review mutation D1, and the direction a count over the name
			// list cannot see: two names, one distinct. The map comes back
			// one entry short, witnessGaps complains once per entry, and
			// the recipe that lost its name is checked by nobody while
			// require.Len(ageLiveRecipes, 2) goes on passing.
			name:  "a recipe named twice is one recipe, not two",
			src:   sound,
			names: []string{recipe, recipe},
			want:  "is named twice",
		},
		{
			// The vacuity row: naming no recipe leaves every complaint
			// above unreachable, so the sweep would pass by checking none.
			name:  "naming no recipe checks nothing",
			src:   sound,
			names: nil,
			want:  "no recipe was named",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, got := recipeBodies(tc.src, tc.names)
			require.NotEmpty(t, got, "this binding is cut, so the reader has to complain")
			require.Contains(t, strings.Join(got, "\n"), tc.want)
		})
	}
}

// TestTheRecipesThisReaderParsesStayInsideTheShellItModels bounds
// stripRecipeComment and shellFields to the artefact they are pointed
// at. Neither models backslash escapes, command substitution, heredocs
// or here-strings, and neither expands a variable.
//
// The justfile uses all of those, in recipes this reader never sees, so
// "no recipe here uses them" is false of the file and was shipped as a
// comment anyway (round-4 review, FINDING 2). Line numbers for them
// stood here until round 5 and were accurate on the day; they are gone
// because nothing checked them, and one edit above the first of them
// would make this comment, stripRecipeComment's and ADR 0028 wrong at
// once. What is true is narrower and is checked here instead:
// recipeBodies reads the two recipes ageLiveRecipes names
// and nothing else, and those two use none of them. That is a fact about
// two single-line recipes on a day, not a law, which is why it is a
// test.
//
// The direction each would fail in is why the bound is worth holding: a
// cut or a quote in the wrong place leaves the reader FEWER flags than
// the shell runs, an absent -run selects everything, and the recipe then
// reads as running a witness it does not run (review mutations V3, V4b).
// Silence, and this file exists to refuse it.
func TestTheRecipesThisReaderParsesStayInsideTheShellItModels(t *testing.T) {
	// The shapes stripRecipeComment and shellFields do not model. This
	// list is the whole of what this test asserts, so it is guarded
	// before it is used: review mutation A9 deleted all four rows and the
	// package stayed green at exit 0, this test among it, printing
	// `--- PASS` over an assertion it no longer made. A9b deleted one row
	// and was the same silence one shape at a time, which is why the
	// guard is a count and not require.NotEmpty.
	//
	// Distinct texts, because four rows naming three shapes is four rows
	// and one hole. A row leaves here when the reader starts modelling
	// its shape, and then this count moves with it — that is the edit
	// the guard is meant to make loud.
	constructs := []struct{ text, what string }{
		{"$", "variable expansion or command substitution"},
		{"`", "command substitution"},
		{"<<", "a heredoc"},
		{`\`, "a backslash escape"},
	}
	shapes := make(map[string]bool, len(constructs))
	for _, construct := range constructs {
		shapes[construct.text] = true
	}
	require.Len(t, shapes, 4,
		"the shell this reader does not model is checked one shape per row, and a row "+
			"that goes quiet takes the bound with it")

	// The loop below needs no vacuity guard of its own: readRecipes
	// complains when a name is missing from the justfile and when no name
	// was given at all, so it cannot silently run over nothing (review
	// mutation R2 — the empty name list fails this test and the sweep, by
	// name). That argument is about THIS loop and no other. It stood here
	// as the reason the test needed no guard at all until round 5, while
	// the list that carries the claim sat inside the loop unpinned; the
	// require.Len above is the guard the argument was being used to
	// excuse.
	recipes := readRecipes(t, ageLiveRecipes)
	for name, cmds := range recipes {
		require.NotEmpty(t, cmds, "recipe %s has no body to read", name)
		for _, construct := range constructs {
			require.NotContains(t, cmds, construct.text,
				"recipe %s uses %s, which this reader does not model: "+
					"the flags it reads are no longer the flags the shell runs",
				name, construct.what)
		}
	}
}

// TestRecipeRunsOnlyWhatTheCommandLineSelects is the recipe artefact's
// half of the property witnessBodies holds for Go source, and it is here
// because strings.Contains could not tell these shapes apart: both AGE
// recipes name a witness AND carry a -skip, so "the name appears" says
// nothing about whether the test runs.
//
// Most rows are a review mutation that once survived. L18 and L19 are
// the two -skip moves that survived the byte-level check; the two
// comment rows are L20's selection half (their own comments say why they
// are not its comment half); MA and MB are the -run narrowings that
// survived the first version of recipeRuns; V1, V3 and T1 are three ways
// a command line ran no witness while every flag loop read an empty
// slice and said yes; POOLTAG2 is the fourth, where the loops read a
// flag that belonged to a different command. The others are the two
// directions of the unanchored match, the -skip subtest carve-out the
// approximation is chosen to catch, and the shapes a rule must not cost
// — a cd on its own line, a quoted #, and a second go test that does run
// the witness.
//
// Driven through recipeBodies from justfile source written here rather
// than over recipeRuns alone, because the comment rows are the reader's
// property and the -skip rows are the selector's, and only the two
// composed hold "this recipe runs this witness".
func TestRecipeRunsOnlyWhatTheCommandLineSelects(t *testing.T) {
	const (
		recipe  = "test-codegen-live-age"
		witness = "TestAGERefusesTheFunctionsItDoesNotDefine"
		// The justfile's own -run and -skip, so the sound row is the
		// shape CI actually invokes and not a convenient simplification.
		liveRun  = "TestLiveSmoke|TestAGESessionInit|TestAGERefusesRelationshipTypeAlternation|" + witness
		liveSkip = "TestLiveSmoke/neo4j"
	)
	// cmdTagged is one recipe body: the build tag and the flags under
	// test, always with -count=1 so the reader's own complaint stays
	// silent and a row can only be about what it says it is about. An
	// empty tags argument writes no -tags at all.
	cmdTagged := func(tags, flags string) string {
		invocation := "cd test/data/codegen && go test -count=1 "
		if tags != "" {
			invocation += "-tags " + tags + " "
		}
		return recipe + ":\n    " + invocation + flags + " ./...\n"
	}
	cmd := func(flags string) string { return cmdTagged(liveBuildTag, flags) }
	sound := cmd("-run '" + liveRun + "' -skip '" + liveSkip + "'")

	for _, tc := range []struct {
		name string
		src  string
		// run is whether the recipe body actually runs the witness.
		run bool
	}{
		{
			name: "the justfile's own shape runs it",
			src:  sound,
			run:  true,
		},
		{
			// -run is a regexp and not a name list: the prefix reaches
			// the witness, so requiring the full name to appear would
			// complain about a recipe that runs it.
			name: "an unanchored -run prefix selects it",
			src:  cmd("-run 'TestAGERefuses' -skip '" + liveSkip + "'"),
			run:  true,
		},
		{
			name: "no -run at all runs everything, including it",
			src:  cmd("-skip '" + liveSkip + "'"),
			run:  true,
		},
		{
			name: "the -run=value spelling selects it too",
			src:  cmd("-run='" + liveRun + "'"),
			run:  true,
		},
		{
			// L18: one token appended to the -skip already there. The
			// appended text is a second ALTERNATIVE, not a second
			// element, so go test drops the witness outright — measured
			// against go1.26.5, not assumed.
			name: "L18: appended to the existing -skip",
			src:  cmd("-run '" + liveRun + "' -skip '" + liveSkip + "|" + witness + "'"),
		},
		{
			name: "L19: named only by -skip",
			src: cmd("-run 'TestLiveSmoke|TestAGESessionInit|TestAGERefusesRelationshipTypeAlternation' " +
				"-skip '" + liveSkip + "|" + witness + "'"),
		},
		{
			// L20 was "delete the witness from -run and leave the name in
			// a justfile comment". These two rows are the SELECTION half
			// of that and not the comment half: they hold whether or not
			// comments are stripped, because a comment introduces no -run
			// and the -run that is here does not reach the witness.
			// Making stripRecipeComment the identity leaves both green
			// (review mutation N1); deleting recipeRuns' -run loop kills
			// both (N3). The comment half — text that is spelled is not
			// text that runs — is carried by ONE row of
			// TestRecipeReaderComplainsOnEachBrokenRecipe: "a trailing
			// comment does not carry -count=1 either", which is what
			// dies to N1 now. Its whole-line sibling stopped dying to N1
			// in round 4: a comment on its own line is not a command, so
			// reading flags per invocation drops it before the comment
			// strip is asked. They were named for L20 until round 3.
			name: "a name in a comment line is not a -run that selects it",
			src: recipe + ":\n    # runs " + witness + " nightly\n" +
				"    cd test/data/codegen && go test -count=1 -tags codegen_live " +
				"-run 'TestLiveSmoke' -skip '" + liveSkip + "' ./...\n",
		},
		{
			name: "a name in a trailing comment is not a -run either",
			src:  cmd("-run 'TestLiveSmoke' -skip '" + liveSkip + "'  # and " + witness),
		},
		{
			// The approximation recipeRuns documents: go test would drop
			// only this subtest, and the top-level would still pass. It
			// is the subtest carrying the probe, so the sweep refuses it.
			name: "a -skip carving out one of its subtests",
			src:  cmd("-run '" + liveRun + "' -skip '" + witness + "/toTimestamp'"),
		},
		{
			// The row above's mirror, and the one round 3 found missing:
			// the identical shape on -run removes strictly MORE
			// measurement, so accepting it while refusing the -skip was
			// backwards. go test runs one probe of five here (review
			// mutation MA).
			name: "a -run narrowing it to one of its subtests",
			src:  cmd("-run '" + liveRun + "/toTimestamp' -skip '" + liveSkip + "'"),
		},
		{
			// And the whole loss: no subtest is named x, so the witness
			// runs ZERO probes, prints `--- PASS` for the top-level with
			// no subtest lines under it, and exits 0 — without even a
			// `[no tests to run]`, because the smoke battery in the first
			// alternative did run something (review mutation MB, measured
			// against go1.26.5).
			name: "a -run narrowing it to a subtest that does not exist",
			src:  cmd("-run '" + liveRun + "/x' -skip '" + liveSkip + "'"),
		},
		{
			name: "a -skip prefix that reaches it unanchored",
			src:  cmd("-run '" + liveRun + "' -skip 'TestAGERefusesThe'"),
		},
		{
			name: "a -run that no longer names it",
			src:  cmd("-run 'TestLiveSmoke|TestAGESessionInit'"),
		},
		{
			// Doubt is not a run. A naive split makes uncompilable
			// pieces of a bracketed alternation, and this reader says so
			// rather than guessing.
			name: "a -run this reader cannot compile",
			src:  cmd("-run 'TestAGE(Refuses|Session'"),
		},
		{
			// And doubt is not a skip. This is the row that made the
			// -skip side of "cannot compile" falsifiable at all: until
			// it existed, selects reported the case through an error
			// value, the -run loop's arm for that error could not be
			// made true (its wholly was already false), and the -skip
			// loop's arm — the only live one — had nothing probing it
			// (review mutations G3b and G3).
			name: "a -skip this reader cannot compile",
			src:  cmd("-run '" + liveRun + "' -skip 'TestAGE(Refuses|Session'"),
		},
		{
			// The question every check here has to answer: what does it
			// say when it finds nothing? This body carries -count=1, the
			// tag and a -run that selects the witness, and runs a script
			// instead of go test. Reading no flags is not reading flags
			// that select everything (review mutation V1).
			name: "a body that never invokes go test",
			src: recipe + ":\n    cd test/data/codegen && ./scripts/live-age.sh " +
				"-count=1 -tags " + liveBuildTag + " -run '" + liveRun + "'\n",
		},
		{
			// The words are not the command. Searching every argument for
			// `go` next to `test` counts a line that only prints them.
			name: "a go test that is another command's argument",
			src: recipe + ":\n    cd test/data/codegen && echo go test " +
				"-count=1 -tags " + liveBuildTag + " -run '" + liveRun + "'\n",
		},
		{
			// And the shape that rule must not cost: the cd on its own
			// line, so `go` opens the next one rather than following an
			// operator.
			name: "go test opening a line of its own runs it",
			src: recipe + ":\n    cd test/data/codegen\n    go test -count=1 -tags " +
				liveBuildTag + " -run '" + liveRun + "' -skip '" + liveSkip + "' ./...\n",
			run: true,
		},
		{
			// A quoted `#` is a literal to sh and was a comment to this
			// reader, which carried the -run away and left the flagless
			// remainder reading as "selects everything" (review mutation
			// V3). This row is the positive control on that fix: the
			// recipe does run the witness, and a quote-blind cut here
			// leaves an unterminated quote the row below refuses.
			name: "a quoted # is not a comment, and the -run after it survives",
			src:  cmd("-ldflags '-X main.p=a#b' -run '" + liveRun + "' -skip '" + liveSkip + "'"),
			run:  true,
		},
		{
			name: "a quoted # in front of a -run that does not select it",
			src:  cmd("-ldflags '-X main.p=a#b' -run 'TestLiveSmoke' -skip '" + liveSkip + "'"),
		},
		{
			// The other half of stripRecipeComment's rule, and the half
			// nothing probed until round 4: sh starts a comment at a `#`
			// that STARTS A WORD, so an unquoted `p=a#b` keeps its `#`
			// too. Reading this one as a comment cuts the -run away, and
			// a body with no -run runs everything — so the answer would
			// flip from "does not select it" to "runs it". Silence,
			// which is why the row asserts the shape that does not run.
			name: "an unquoted # inside a word is not a comment either",
			src:  cmd("-ldflags -X main.p=a#b -run 'TestLiveSmoke' -skip '" + liveSkip + "'"),
		},
		{
			// sh rejects this line outright, so the recipe runs nothing.
			// Without the check, the unterminated -skip swallows the rest
			// of the line into one pattern that reaches no test, no -run
			// is found at all, and the witness reads as running.
			name: "a command line whose quoting never closes",
			src:  cmd("-skip '" + liveSkip),
		},
		{
			// Every live_*_test.go is behind liveBuildTag, so this
			// command line compiles none of them and the -run selects
			// nothing that exists (review mutation T1).
			name: "no -tags builds none of the live battery",
			src:  cmdTagged("", "-run '"+liveRun+"' -skip '"+liveSkip+"'"),
		},
		{
			name: "a -tags that does not carry the live tag",
			src:  cmdTagged("integration", "-run '"+liveRun+"' -skip '"+liveSkip+"'"),
		},
		{
			// The tag among others is still the tag: the value is a list.
			name: "the live tag in a list of tags still builds it",
			src:  cmdTagged("integration,"+liveBuildTag, "-run '"+liveRun+"' -skip '"+liveSkip+"'"),
			run:  true,
		},
		{
			// Review mutation POOLTAG2, and the round-4 blocking
			// finding. The tag is on a command that is not the test run
			// — this justfile's own fence idiom, `go vet -tags
			// codegen_live ./...` (line 230) — so the battery is
			// compiled by nothing and `go test` prints "[no test files]"
			// for every package under test/data/codegen at exit 0. That
			// is review mutation T1 with one line added, and it survived
			// for as long as the tag was read from the body. Its
			// `go build -tags codegen_live` spelling (POOLTAG) is the
			// same shape.
			name: "a -tags on another line builds nothing for this go test",
			src: recipe + ":\n    cd test/data/codegen && go vet -tags " + liveBuildTag + " ./...\n" +
				"    cd test/data/codegen && go test -count=1 -run '" + liveRun + "' " +
				"-skip '" + liveSkip + "' ./...\n",
		},
		{
			// And the same tag one `&&` away rather than one line away,
			// which is the shape a reader splitting only on lines still
			// pools. Both spellings are one recipe body away from the
			// justfile's own test-codegen-fence.
			name: "a -tags earlier in the same line builds nothing for this go test",
			src: recipe + ":\n    cd test/data/codegen && go vet -tags " + liveBuildTag +
				" ./... && go test -count=1 -run '" + liveRun + "' " +
				"-skip '" + liveSkip + "' ./...\n",
		},
		{
			// The approximation the same rewrite REMOVED, so this row
			// is the one that changed answer. Reading flags from the
			// body made two batteries one command line, and a recipe
			// whose second `go test` covers the witness read as not
			// running it, because the first one's -run does not select
			// it. Per invocation it plainly does run it.
			name: "a second go test that runs it runs it",
			src: recipe + ":\n    cd test/data/codegen && go test -count=1 -tags " + liveBuildTag +
				" -run 'TestLiveSmoke' ./...\n" +
				"    cd test/data/codegen && go test -count=1 -tags " + liveBuildTag +
				" -run '" + liveRun + "' -skip '" + liveSkip + "' ./...\n",
			run: true,
		},
		{
			// Its mirror, and the pair is what pins SOME invocation
			// rather than the first or the last: reading only the last
			// leaves this one reading as not run, reading only the first
			// leaves the row above reading as not run.
			name: "a first go test that runs it runs it",
			src: recipe + ":\n    cd test/data/codegen && go test -count=1 -tags " + liveBuildTag +
				" -run '" + liveRun + "' -skip '" + liveSkip + "' ./...\n" +
				"    cd test/data/codegen && go test -count=1 -tags " + liveBuildTag +
				" -run 'TestLiveSmoke' ./...\n",
			run: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bodies, complaints := recipeBodies(tc.src, []string{recipe})
			require.Empty(t, complaints, "this row is about selection, so the reader must have no complaint of its own")
			require.Equal(t, tc.run, recipeRuns(bodies[recipe], witness))
		})
	}
}
