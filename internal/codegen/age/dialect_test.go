package age

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/query/cypher"
)

// A refusal list whose entries have no witness is a guess with a test
// suite around it. Everything in this file exists to make that shape
// impossible: dialectGaps may only grow alongside the live measurements
// that justify it, and the sweep below is what says so.
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
	recipes := map[string]string{"run-it": "go test -run " + witness}
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
// an AGE test no recipe names, is never run against the pinned image —
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
				if !strings.Contains(cmds, g.witness) {
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

// readLiveWitnessBodies is every live test's name paired with the source
// of its body. Read from disk rather than imported: the files are in
// another Go module and behind a build tag this binary is not compiled
// with, so a source-level read is the only binding available from here —
// and it is the binding that matters, since what has to agree is the
// probe text on both sides.
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
		file, err := parser.ParseFile(fset, p, src, 0)
		require.NoError(t, err, "parse %s", p)
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || fn.Body == nil {
				continue
			}
			from := fset.Position(fn.Body.Pos()).Offset
			to := fset.Position(fn.Body.End()).Offset
			require.NotContains(t, bodies, fn.Name.Name,
				"two live tests named %s: the sweep would read whichever was parsed last", fn.Name.Name)
			bodies[fn.Name.Name] = string(src[from:to])
		}
	}
	return bodies
}

// recipeBodies is what each named recipe runs, read out of justfile
// source, alongside the complaints about the ones that are missing or
// that would report on a cached run.
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
	for _, name := range names {
		var body []string
		for i, line := range lines {
			if line != name+":" {
				continue
			}
			for _, next := range lines[i+1:] {
				if next == "" || !strings.HasPrefix(next, " ") && !strings.HasPrefix(next, "\t") {
					break
				}
				body = append(body, next)
			}
			break
		}
		if len(body) == 0 {
			complaints = append(complaints,
				fmt.Sprintf("recipe %s is not in the justfile, so nothing this sweep says about it is true", name))
			continue
		}
		joined := strings.Join(body, "\n")
		if !strings.Contains(joined, "-count=1") {
			complaints = append(complaints,
				fmt.Sprintf("recipe %s reports on a cached run: a witness is a measurement or it is nothing", name))
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
