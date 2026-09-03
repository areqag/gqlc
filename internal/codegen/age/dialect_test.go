package age_test

import (
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/codegen/age"
	"github.com/areqag/gqlc/internal/liverecipes"
	"github.com/areqag/gqlc/internal/query/cypher"
)

// A refusal list whose entries have no witness is a guess with a test
// suite around it. Everything in this file exists to make that shape
// impossible: dialectGaps may only grow alongside the live measurements
// that justify it, and the sweep below is what says so.
//
// The sweep reads a witness's code, and reads it at two scopes: a probe
// text and a served text have to be RUN, so containment in the body is
// the evidence; a recorded answer has to be CHECKED, so it is looked for
// only in what an assertion reads (assertedText). Neither is a given —
// reading the code rather than the source bytes took a fix (review
// mutation M15, see witnessBodies), and narrowing the answer to the
// assertions took another (M18, bd gqlc-35yu.17).
//
// Where it still stops: assertedText follows a name one hop, and it
// reaches an assertion through a suite gateway or through a helper the
// same file declares but no further — a helper behind a second helper, a
// helper in a sibling live file, and the bare promoted form
// `s.Contains(...)` all still report their answers unasserted (bd
// gqlc-bpew narrowed the first three shapes, not these). That is a false
// red, which is the safe direction here — see the hazard argument below.
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
	// "[no test files]" for every package in test/data/codegen. Taken from
	// liverecipes rather than respelled, so a sweep reading one tag and a
	// recipe reader reading another is not a state this tree has.
	liveBuildTag = liverecipes.LiveBuildTag
	// justfilePath holds the recipes CI invokes.
	justfilePath = "justfile"
)

// ageLiveRecipes are the recipes that must run every AGE witness. Named
// rather than derived, because the live neo4j recipe
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
	// The LENGTH of the list is pinned rather than its emptiness, and it
	// is one guard of two because the list goes wrong in two directions
	// and each guard catches one. A name REMOVED is caught here:
	// witnessGaps complains once per entry of the map readRecipes returns,
	// so a dropped name is a recipe nobody checks and the sweep goes on
	// passing (review mutation R1, one name left, green). A name REPEATED
	// leaves this length unchanged and collapses that map by one entry,
	// which this pin cannot see — recipeBodies complains about it instead
	// (review mutation D1, which survived this pin). The empty list fails
	// in recipeBodies too (R2). The live neo4j recipe,
	// test-codegen-live-neo4j, is absent on purpose — see ageLiveRecipes.
	//
	// Do not restate this length in prose. Sentences here and in ADR 0028
	// did, until mutation GROW — ageLiveRecipes 2 names/2 distinct to 3
	// names/3 distinct, the pin literal moved to 3, and a third justfile
	// recipe running the same witnesses — left this package green at exit
	// 0 while they went false.
	require.Len(t, ageLiveRecipes, 2,
		"a name dropped from this list is a recipe nobody checks, and a witness "+
			"can stop being run by it")
	bodies, methods := readLiveWitnessBodies(t)
	recipes := readRecipes(t, ageLiveRecipes)
	require.Empty(t, witnessGaps(age.DialectGaps, bodies, methods, recipes),
		"every refusal this backend makes on the query text has to rest on a live measurement")
}

// TestEveryRefusedFunctionNameIsNamedByItsProbeAnswer closes the loop
// the derivation opens. undefinedFunctions is read out of the probe
// TEXTS, so a probe calling the wrong function would enter the wrong
// name into the catalogue and every other check here would still pass.
// The server's own answer is the independent reading: it names the
// function it could not find, so a probe and its answer agreeing is two
// sources saying the same name.
// Both function catalogues are read, because the property belongs to the
// derivation and there are now two of it. A gap added with its own
// probes and its own parse inherits the whole hazard and none of the
// check unless it is named here.
func TestEveryRefusedFunctionNameIsNamedByItsProbeAnswer(t *testing.T) {
	for _, cat := range functionCatalogues {
		t.Run(cat.name, func(t *testing.T) {
			require.NotEmpty(t, cat.probes)
			for _, p := range cat.probes {
				found := cat.find(p.Text())
				require.NotEmpty(t, found, "probe %q must call a function the gate reads", p.Text())
				for _, f := range found {
					require.Contains(t, strings.ToLower(p.Answer()), strings.ToLower(f.Text()),
						"probe %q entered %q into the catalogue, but the server's answer does not name it", p.Text(), f.Text())
				}
			}
			require.Len(t, cat.names, len(cat.probes),
				"one probe, one name: a probe calling two functions, or two probes calling one, "+
					"makes the catalogue and the evidence stop lining up one for one")
		})
	}
}

// TestTheFunctionCataloguesAreDisjoint is the cost of there being two of
// them. Each gap's prose is a claim about its own kind of call, so a name
// in both would be answered by whichever gap the table reaches first
// and told about the other one's fix — the exact confusion the second
// sentinel was minted to prevent.
func TestTheFunctionCataloguesAreDisjoint(t *testing.T) {
	require.Greater(t, len(functionCatalogues), 1,
		"one catalogue, or none: this test compared nothing and would pass on any table")
	seen := make(map[string]string)
	for _, cat := range functionCatalogues {
		for name := range cat.names {
			previous, repeated := seen[name]
			require.False(t, repeated,
				"%q is in both the %s and %s catalogues, so one gap answers it with the other's remedy",
				name, previous, cat.name)
			seen[name] = cat.name
		}
	}
}

// TestEveryRefusedNamespaceIsNamedByItsProbeAnswer is the namespace
// catalogue's half of the same discipline, and a parallel guard rather
// than a widening of the one above.
//
// The two contracts differ in what the server's answer has to name. A
// function gap's probe answers `function <name> does not exist`, so the
// name in the catalogue is checkable against it. This gap's probe
// answers `schema "duration" does not exist` and names no function at
// all — the refusal belongs to the qualifier. Teaching the guard above
// to accept either shape would make it satisfiable by a row that names
// nothing, which is the exact hazard both guards exist to forbid, so
// there are two guards with one contract each.
//
// The one-qualified-call clause is the fail-closed half. A probe text
// that yields NO qualified call would leave the loop below comparing
// nothing and passing, so the assertion that can never silently find
// nothing to compare is the assertion that the probe calls exactly one.
func TestEveryRefusedNamespaceIsNamedByItsProbeAnswer(t *testing.T) {
	require.NotEmpty(t, age.NamespaceProbes,
		"no probes: this guard would compare nothing and pass on an empty catalogue")
	for _, p := range age.NamespaceProbes {
		calls := cypher.QualifiedFunctionCalls(p.Text())
		require.Len(t, calls, 1,
			"probe %q must make exactly one qualified call: a probe making none leaves this "+
				"guard nothing to compare, and one making two stops the probes and the "+
				"namespaces lining up one for one", p.Text())
		ns := calls[0].Namespace
		require.NotEmpty(t, ns, "probe %q entered an empty namespace into the catalogue", p.Text())
		require.Contains(t, strings.ToLower(p.Answer()), strings.ToLower(ns),
			"probe %q entered %q into the catalogue, but the server's answer does not name it",
			p.Text(), ns)
	}
	require.Len(t, age.UndefinedNamespaces, len(age.NamespaceProbes),
		"one probe, one namespace: two probes under one namespace, or a probe naming two, "+
			"makes the catalogue and the evidence stop lining up one for one")
}

// TestTheNamespaceGapIsNotAFunctionCatalogue pins the exemption rather
// than leaving it to be noticed. functionCatalogues drives two checks
// whose contract this gap cannot meet — its probe's answer names no
// function — so it is deliberately absent, and an absence nothing
// asserts is indistinguishable from an omission.
//
// The disjointness those checks enforce is not lost, it is held by a
// different argument: the two scans partition the calls by SHAPE, so
// `duration` the refused function name and `duration` the refused
// namespace are facts about two different spellings and no call can be
// claimed by both.
//
// TestNoCallIsClaimedByBothGaps below is where that is measured, and it
// took bd gqlc-794sz to get there: the sentence originally cited a name no
// commit has ever declared, and the correction that removed the citation
// (gqlc-som6y) replaced it with "measured nowhere", which understated the
// tree. One direction of the property was already held, by the ordering
// pins in age_test.go — see the row below for which one, and for why the
// other could not be held there.
func TestTheNamespaceGapIsNotAFunctionCatalogue(t *testing.T) {
	for _, cat := range functionCatalogues {
		require.NotEqual(t, "namespace", cat.name,
			"the namespace gap answers with a message naming no function, so the function "+
				"catalogue checks cannot hold it")
	}
	// The reason it is exempt, asserted rather than asserted-about: the
	// probe's answer names the namespace and does NOT name a function,
	// which is what the guard above requires and this one forbids.
	for _, p := range age.NamespaceProbes {
		require.NotContains(t, strings.ToLower(p.Answer()), "function",
			"probe %q answers naming a function, so it belongs in a function catalogue "+
				"after all and this exemption is stale", p.Text())
	}
}

// TestNoCallIsClaimedByBothGaps measures the shape argument the exemption
// above rests on: one name is in a function catalogue AND in the namespace
// catalogue, and the two gaps still take one call each, because what
// separates them is how the call is SPELLED and not what it is called.
//
// It runs on a collision the tree actually has rather than on a
// constructed one, which is why the pairs are derived and the emptiness of
// that derivation is fatal. `duration` is in both today — the temporal gap
// probed `duration({days:1})` and the namespace gap probed
// `duration.between(null, null)` — and if a future tree has no such name
// then the property here is unreachable and this row would pass having
// compared two sets that could not have overlapped anyway.
//
// WHY IT IS NOT ENOUGH THAT THE GATE HAS ORDERING PINS. age_test.go's
// EmissionSuite pins a query spelling both, and its refusal quotes the
// bare name alone, so a temporal gap that also took the namespaced call is
// caught there — that direction has eleven rows and is not the gap.
//
// The mirror direction has none, and the ordering is exactly what hides
// it. The namespace gap answers LAST, so on any text where both would fire
// an earlier gap has already answered; a namespace gap that also took the
// BARE call changes nothing any suite row can see. Measured on the tree
// this arrived on, by teaching findUndefinedNamespaces to claim a bare call
// whose name is a refused namespace: ./internal/codegen/... stays green.
// That is a reader with no reader, and it is what this row is for.
func TestNoCallIsClaimedByBothGaps(t *testing.T) {
	type collision struct {
		catalogue string
		name      string
		find      func(string) []age.Finding
	}
	var collisions []collision
	for _, cat := range functionCatalogues {
		for name := range cat.names {
			if _, both := age.UndefinedNamespaces[name]; both {
				collisions = append(collisions, collision{cat.name, name, cat.find})
			}
		}
	}
	require.NotEmpty(t, collisions,
		"no name is in a function catalogue and in the namespace catalogue at once, so the two "+
			"gaps could not have claimed one call whatever they did and this row measured nothing")

	for _, c := range collisions {
		t.Run(c.catalogue+"/"+c.name, func(t *testing.T) {
			// One text spelling the name both ways. The qualified half
			// calls `f` because the catalogue is of NAMESPACES: which
			// function sits under the qualifier is a thing the server
			// never reaches, so naming a real one would suggest the
			// refusal turned on it.
			src := "RETURN " + c.name + "(1), " + c.name + ".f(1)"
			bare, qualified := c.name, c.name+".f"

			byFunction := c.find(src)
			require.Len(t, byFunction, 1,
				"the %s gap claims %d calls in %q; it may claim the bare call and nothing else",
				c.catalogue, len(byFunction), src)
			require.Equal(t, bare, byFunction[0].Text(),
				"the %s gap claims %q, which is the namespaced spelling: a call the namespace gap "+
					"answers is being refused for a function name too", c.catalogue, byFunction[0].Text())

			byNamespace := age.FindUndefinedNamespaces(src)
			require.Len(t, byNamespace, 1,
				"the namespace gap claims %d calls in %q; it may claim the qualified call and nothing else",
				len(byNamespace), src)
			require.Equal(t, qualified, byNamespace[0].Text(),
				"the namespace gap claims %q, which is the bare spelling: a call the %s gap answers "+
					"is being refused for a namespace too", byNamespace[0].Text(), c.catalogue)

			// The positions, because the spellings alone do not settle it:
			// a gap quoting the right text off the wrong call would satisfy
			// everything above, and it is the CALL the argument is about.
			require.NotEqual(t, byFunction[0].Column(), byNamespace[0].Column(),
				"both gaps claim the call at column %d, so one call is being answered twice",
				byFunction[0].Column())
		})
	}
}

// functionCatalogues is the derived name sets and the probes each was
// read out of, so the two checks above run over every one of them rather
// than over the one that existed when they were written.
var functionCatalogues = []struct {
	name   string
	probes []age.DialectProbe
	names  map[string]struct{}
	find   func(string) []age.Finding
}{
	{"temporal", age.UndefinedFunctionProbes, age.UndefinedFunctions, age.FindUndefinedFunctions},
	{"spatial", age.SpatialFunctionProbes, age.UndefinedSpatialFunctions, age.FindUndefinedSpatialFunctions},
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
	const (
		witness = "TestSomethingLive"
		// The probe and the served text are code the witness runs; the
		// answer is code an assertion reads. Split because witnessBody's
		// two scopes are, and hand-built here rather than parsed: a row
		// below cuts a binding in the GAP, so what it needs from the
		// bodies map is that the map be sound, not that it be read off a
		// file. The reader is driven over real source by
		// TestACommentedProbeReddensTheSweep and
		// TestAnUnassertedAnswerReddensTheSweep.
		measuredCode   = "probe := \"MATCH (:A)-[r:X|Y]->(:B) RETURN r\"\nserved := \"MATCH (:A)-[r:X]->(:B) RETURN r\"\n"
		measuredAssert = "`syntax error at or near \"|\"`\n"
		// Three bindings of another test's own, which is what the "some
		// other live test" rows point a gap at.
		elsewhereCode   = "elsewhere := \"MATCH (:A)-[r:M|N]->(:B) RETURN r\"\nacceptedElsewhere := \"MATCH (:A)-[r:W]->(:B) RETURN r\"\n"
		elsewhereAssert = "`no relationship type by that name`\n"
	)
	bodies := map[string]witnessBody{
		witness: {code: measuredCode + measuredAssert, asserted: measuredAssert},
		// Declared and never run. The recipe row needs a witness the
		// live source DOES declare, or the declaration complaint fires
		// too and the row would pass without the recipe check existing.
		witness + "ButUnrun": {
			code:     measuredCode + measuredAssert + elsewhereCode + elsewhereAssert,
			asserted: measuredAssert + elsewhereAssert,
		},
	}
	// Anchored, and the row below is why: -run is an unanchored regexp,
	// so a bare `-run TestSomethingLive` selects TestSomethingLiveButUnrun
	// too and the "no recipe runs it" row would find the witness run.
	// The build tag is here because recipeRuns requires it — a command
	// line that does not build liveBuildTag compiles no live test.
	recipes := map[string]string{"run-it": "go test -tags " + liveBuildTag + " -run '^" + witness + "$'"}
	// Written, and on a suite, so it is neither a witness nor an absence.
	// Deliberately NOT in bodies as well: that split is the state under
	// test, since witnessBodies puts a method in one map and not the
	// other.
	methods := map[string]string{witness + "AsAMethod": "*liveSuite"}
	sound := age.DialectGapFields{
		Sentinel: age.ErrRelationshipTypeAlternation,
		Find:     findUndefinedFunctionsOrAlternations,
		Diagnose: func(int, string, string) string { return "" },
		Witness:  witness,
		Refused:  []age.DialectProbe{age.NewDialectProbe("MATCH (:A)-[r:X|Y]->(:B) RETURN r", `syntax error at or near "|"`)},
		Served:   []string{"MATCH (:A)-[r:X]->(:B) RETURN r"},
	}.Build()
	require.Empty(t, witnessGaps([]age.DialectGap{sound}, bodies, methods, recipes),
		"the row template must pass, or a complaint below could come from the template")

	for _, tc := range []struct {
		name string
		// cut is the one binding this row cuts. A nil gaps slice
		// from it means the row is about the table and not about a gap.
		cut  func(g age.DialectGap) []age.DialectGap
		want string
	}{
		{
			name: "a gap with no probe refuses on nothing",
			cut:  func(g age.DialectGap) []age.DialectGap { g.SetRefused(nil); return []age.DialectGap{g} },
			want: "refuses on no probe",
		},
		{
			name: "a probe the gate does not read is not evidence for this gate",
			cut: func(g age.DialectGap) []age.DialectGap {
				g.SetRefused([]age.DialectProbe{age.NewDialectProbe("MATCH (:A)-[r:X]->(:B) RETURN r", "boom")})
				return []age.DialectGap{g}
			},
			want: "is not read by this gap",
		},
		{
			name: "a probe no live test carries is never re-measured",
			cut: func(g age.DialectGap) []age.DialectGap {
				g.SetRefused([]age.DialectProbe{age.NewDialectProbe("MATCH (:A)-[r:P|Q]->(:B) RETURN r", `syntax error at or near "|"`)})
				return []age.DialectGap{g}
			},
			want: "is not carried by",
		},
		{
			name: "an answer no live test asserts is a claim about a server nothing checks",
			cut: func(g age.DialectGap) []age.DialectGap {
				g.SetRefused([]age.DialectProbe{age.NewDialectProbe("MATCH (:A)-[r:X|Y]->(:B) RETURN r", "some other complaint entirely")})
				return []age.DialectGap{g}
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
			cut: func(g age.DialectGap) []age.DialectGap {
				g.SetRefused([]age.DialectProbe{age.NewDialectProbe("MATCH (:A)-[r:M|N]->(:B) RETURN r", `syntax error at or near "|"`)})
				return []age.DialectGap{g}
			},
			want: "is not carried by",
		},
		{
			name: "an answer some other live test asserts is not asserted by this witness",
			cut: func(g age.DialectGap) []age.DialectGap {
				g.SetRefused([]age.DialectProbe{age.NewDialectProbe("MATCH (:A)-[r:X|Y]->(:B) RETURN r", "no relationship type by that name")})
				return []age.DialectGap{g}
			},
			want: "is not asserted by",
		},
		{
			name: "a served text some other live test carries was not measured as served here",
			cut: func(g age.DialectGap) []age.DialectGap {
				g.SetServed([]string{"MATCH (:A)-[r:W]->(:B) RETURN r"})
				return []age.DialectGap{g}
			},
			want: "is not carried by",
		},
		{
			name: "a gap recording no served text has nothing bounding its find",
			cut:  func(g age.DialectGap) []age.DialectGap { g.SetServed(nil); return []age.DialectGap{g} },
			want: "records no served text",
		},
		{
			name: "a served text the gate refuses is the false positive this table exists to prevent",
			cut: func(g age.DialectGap) []age.DialectGap {
				g.SetServed([]string{"MATCH (:A)-[r:X|Y]->(:B) RETURN r"})
				return []age.DialectGap{g}
			},
			want: "as served and its find refuses it",
		},
		{
			name: "a served text no live test carries was never measured as served",
			cut: func(g age.DialectGap) []age.DialectGap {
				g.SetServed([]string{"MATCH (:A)-[r:Z]->(:B) RETURN r"})
				return []age.DialectGap{g}
			},
			want: "is not carried by",
		},
		{
			name: "a gap naming no witness names nothing to re-measure it",
			cut:  func(g age.DialectGap) []age.DialectGap { g.SetWitness(""); return []age.DialectGap{g} },
			want: "names no witness test",
		},
		{
			name: "a witness no live file declares does not exist",
			cut:  func(g age.DialectGap) []age.DialectGap { g.SetWitness("TestNotWritten"); return []age.DialectGap{g} },
			want: "is not declared in any live test file",
		},
		{
			// The pair to the row above, and the reason both are here: a
			// witness written as a suite method used to get that row's
			// complaint, which is false — the file DOES declare it — and
			// sends the author hunting for a test that is in front of
			// them. The two rows together are what shows the sweep tells
			// the worlds apart rather than collapsing them.
			name: "a witness declared only as a method is written, and still cannot be a witness",
			cut: func(g age.DialectGap) []age.DialectGap {
				g.SetWitness(witness + "AsAMethod")
				return []age.DialectGap{g}
			},
			want: "is declared as a method on *liveSuite",
		},
		{
			name: "a witness no recipe runs never runs",
			cut: func(g age.DialectGap) []age.DialectGap {
				g.SetWitness("TestSomethingLiveButUnrun")
				return []age.DialectGap{g}
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
			cut:  func(g age.DialectGap) []age.DialectGap { g.SetSentinel(nil); return []age.DialectGap{g} },
			want: "carries no sentinel",
		},
		{
			name: "a gap with no find reads nothing",
			cut:  func(g age.DialectGap) []age.DialectGap { g.SetFind(nil); return []age.DialectGap{g} },
			want: "has no find",
		},
		{
			name: "a gap with no diagnose tells the author nothing",
			cut:  func(g age.DialectGap) []age.DialectGap { g.SetDiagnose(nil); return []age.DialectGap{g} },
			want: "has no diagnose",
		},
		{
			// The vacuity row. Everything above is a complaint about a
			// gap, so all of them are reachable only through the loop —
			// and a loop over nothing runs no body. Without this the
			// whole sweep is satisfied by deleting the table.
			name: "an empty table has measured nothing",
			cut:  func(age.DialectGap) []age.DialectGap { return nil },
			want: "the dialect gap table is empty",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := witnessGaps(tc.cut(sound), bodies, methods, recipes)
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
	bodies, _ := readLiveWitnessBodies(t)
	compared := 0
	for _, g := range age.DialectGaps {
		body, declared := bodies[g.Witness()]
		require.True(t, declared, "gap witness %s is not declared in any live test file", g.Witness())
		for _, other := range age.DialectGaps {
			if other.Witness() == g.Witness() {
				continue
			}
			for _, p := range other.Refused() {
				compared++
				require.NotContains(t, body.code, p.Text(),
					"%s carries %s's probe %q, so the reader is not scoped to one test's body",
					g.Witness(), other.Witness(), p.Text())
			}
		}
	}
	require.NotZero(t, compared,
		"one gap, or two sharing a witness: this test compared nothing and would pass on any reader")
}

// TestAMethodIsReportedRatherThanDropped guards the reader's third axis,
// and it is the one that decides whether the sweep can tell an unwritten
// witness from a written-but-unselectable one.
//
// witnessBodies has always skipped methods, correctly: `go test -run`
// selects a method as Suite/Method, so a recipe cannot run one whole and
// recipeRuns is right to refuse it. What it did wrong was skip them
// SILENTLY, which left the sweep with one answer for two worlds and made
// it tell an author that a test they can see in their own file "is not
// declared in any live test file" (bd gqlc-qw57d).
//
// The three assertions are separate claims about one parse, so they are
// assert and not require: under require the first failure hides the rest,
// and a reader that returned the method as a BODY would fail the first
// two while the third — the one that says the skip still happens — never
// ran.
func TestAMethodIsReportedRatherThanDropped(t *testing.T) {
	src := []byte(`package fixtures_test

import "testing"

type liveSuite struct{ t *testing.T }

func (s *liveSuite) TestOnASuite(t *testing.T) { _ = "` + syntheticProbeText + `" }

func TestTopLevel(t *testing.T) { _ = "top" }
`)
	bodies, methods, err := witnessBodies(token.NewFileSet(), "live_synthetic_test.go", src)
	require.NoError(t, err, "the source this test writes has to parse")

	assert.NotContains(t, bodies, "TestOnASuite",
		"a method reached the bodies map, so recipeRuns would be asked to run a name -run cannot select whole")
	assert.Equal(t, "*liveSuite", methods["TestOnASuite"],
		"the method was dropped rather than reported, so the sweep cannot tell it from a witness nobody wrote")
	assert.Contains(t, bodies, "TestTopLevel",
		"the top-level test stopped being read, so this file's whole binding is gone")
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

// syntheticColumnRow is the answer as a table column an assertion selects,
// which is the shape both AGE witnesses actually use — the answer reaches
// require.Contains as tc.wantMessage and never as a literal. Named because
// the reader has to be shown reading it, and shown not reading it out of a
// comment, and those are two rows.
const syntheticColumnRow = "\tfor _, tc := range []struct{ want string }{{want: \"" + syntheticProbeAns + "\"}} {\n" +
	"\t\trequire.Contains(t, msg, tc.want)\n\t}\n"

// syntheticUnassertedRow is the same row with the answer moved out of the
// assertion and left standing as a local nothing reads. That is mutation
// M18 spelled as code, and every byte the sweep read before this bead is
// still there: the probe text is in the body and so is the answer.
const syntheticUnassertedRow = "\tprobe := \"" + syntheticProbeText + "\"\n" +
	"\tanswer := \"" + syntheticProbeAns + "\"\n" +
	"\trequire.Error(t, err)\n" +
	"\t_ = probe\n" +
	"\t_ = answer\n"

// syntheticSuiteRow is the answer asserted through the testify suite
// form. Every byte of the assertion is there and none of it is rooted at
// the identifier `require`: the call is a method on whatever `s` is, and
// the package name appears nowhere in the row.
const syntheticSuiteRow = "\ts.Require().Contains(msg, \"" + syntheticProbeAns + "\")\n"

// syntheticSuiteNonAssertionRow is a method call on the same receiver
// that asserts nothing. It is the widening this bead's fix has to not be:
// a reader that took every method call on `s` for an assertion would put
// this row's argument in the corpus, and with it every argument of every
// setup call a suite-form witness makes.
const syntheticSuiteNonAssertionRow = "\ts.loadSchema(\"" + syntheticProbeAns + "\")\n"

// syntheticChainedNonAssertionRow reaches a non-assertion method through
// a chained call, which is the suite form's own SHAPE with a different
// name in the middle. It is what makes the gateway list a list rather
// than "any call in selector position".
const syntheticChainedNonAssertionRow = "\ts.harness().loadSchema(\"" + syntheticProbeAns + "\")\n"

// syntheticBoundObjectRow is the answer asserted through an assertion
// object bound to an ordinary local, which is the spelling testify's own
// documentation opens with. Neither shape above sees it: the selector
// base is the identifier `r`, which is not an assertion package, and no
// gateway call stands between that base and the assertion.
const syntheticBoundObjectRow = "\tr := require.New(t)\n" +
	"\tr.Contains(msg, \"" + syntheticProbeAns + "\")\n"

// The three rows below are the negative half of the row above, one per
// condition isAssertionConstructor tests. They are three and not one
// because a single negative can only kill a single mutation: dropping any
// one of the three checks leaves the other two answering, so a lone row
// certifies a lone condition and infers the rest.
//
// Each binds a local, calls the same method name on it, and puts the
// answer in that call's arguments. They differ only in the right-hand
// side, which is the thing the reader is supposed to be deciding on.

// syntheticBoundNonAssertionRow is bound to a bare call — no selector at
// all. It holds the requirement that the right-hand side be a call
// through a package.
const syntheticBoundNonAssertionRow = "\tr := newHarness(t)\n" +
	"\tr.Contains(msg, \"" + syntheticProbeAns + "\")\n"

// syntheticBoundForeignPackageRow is a New, on a package that is not an
// assertion package. It holds the assertionPackages check: `New` is an
// unremarkable constructor name and any package may have one.
const syntheticBoundForeignPackageRow = "\tr := harness.New(t)\n" +
	"\tr.Contains(msg, \"" + syntheticProbeAns + "\")\n"

// syntheticBoundNonConstructorRow is an assertion package, and not New.
// It holds the method-name check. The binding call is itself an assertion
// by the package rule, so its OWN arguments are read — and they are not
// the answer, which is what makes this row about the local rather than
// about the call it is bound to.
const syntheticBoundNonConstructorRow = "\tr := assert.ObjectsAreEqual(msg, want)\n" +
	"\tr.Contains(msg, \"" + syntheticProbeAns + "\")\n"

// syntheticHelperRow is the answer asserted through a helper the witness
// declares itself, so the answer reaches require one call deeper than the
// witness's own body goes.
const syntheticHelperRow = "\tassertRefusal(t, msg, \"" + syntheticProbeAns + "\")\n"

// syntheticHelperDecl is the helper syntheticHelperRow calls. It is a
// sibling declaration and not a closure, because that is the shape the
// bead names and the shape a reader given one function body cannot see.
const syntheticHelperDecl = "\nfunc assertRefusal(t *testing.T, msg, want string) {\n" +
	"\trequire.Contains(t, msg, want)\n}\n"

// syntheticInertHelperRow calls a sibling declaration that asserts
// nothing. The negative half of the helper form: what makes a helper
// count is that an assertion is reached through it, not that the witness
// called something declared nearby.
const syntheticInertHelperRow = "\trecordRefusal(t, \"" + syntheticProbeAns + "\")\n"

// syntheticInertHelperDecl is syntheticHelperDecl with the assertion
// taken out and nothing else changed, so the two rows differ in the one
// thing this reader is supposed to be deciding on.
const syntheticInertHelperDecl = "\nfunc recordRefusal(t *testing.T, note string) {\n" +
	"\tt.Log(note)\n}\n"

// syntheticBoundHelperRow is the answer asserted through a helper that
// binds its OWN assertion object — the helper form and the bound-object
// form composed. It is the shape neither call site of isAssertionCall saw
// on its own: assertionHelpers decides what the helpers are before any
// body has been read, so it asks with no bases, and the only call inside
// this helper is a method on a local (bd gqlc-k9v3k).
const syntheticBoundHelperRow = "\tassertRefusalBound(t, msg, \"" + syntheticProbeAns + "\")\n"

// syntheticBoundHelperDecl is syntheticHelperDecl with the assertion
// routed through a bound object instead of through the package, and
// nothing else changed, so what separates it from the inert helper is
// still the one thing this reader decides on.
const syntheticBoundHelperDecl = "\nfunc assertRefusalBound(t *testing.T, msg, want string) {\n" +
	"\tr := require.New(t)\n" +
	"\tr.Contains(msg, want)\n}\n"

// syntheticInertBoundHelperRow is the widening the row above must not be,
// and the one it was until bd gqlc-k9v3k: the helper BINDS an assertion
// object and then asserts nothing through it. Constructing an assertion
// object is not asserting, so a witness handing this helper the answer has
// not checked it.
//
// This is the FALSE GREEN direction — an unasserted answer passing — which
// is why it is spelled out rather than left to the inert helper above. That
// one is inert by having no assertion package in it at all; this one is
// inert while naming the package on its first line, which is what the
// constructor rule had to be taught to tell apart.
const syntheticInertBoundHelperRow = "\trecordRefusalBound(t, \"" + syntheticProbeAns + "\")\n"

// syntheticInertBoundHelperDecl is syntheticBoundHelperDecl with the
// assertion taken out and the binding left standing, so the pair differs
// in the one thing this reader decides on.
const syntheticInertBoundHelperDecl = "\nfunc recordRefusalBound(t *testing.T, note string) {\n" +
	"\tr := require.New(t)\n" +
	"\t_ = r\n" +
	"\tt.Log(note)\n}\n"

// liveWitnessSource is a live test file declaring one witness, with the
// served text always in code and the refused probe wherever row puts it.
//
// Written here rather than read from the repo, which is the reason
// witnessBodies is a separate function at all: this repo's live files
// comment no probe out, so nothing in them can tell a reader that keeps
// comments from one that drops them. doc goes above the declaration, row
// inside the body, decls after it as siblings of the witness.
func liveWitnessSource(doc, row, decls string) []byte {
	return []byte("//go:build codegen_live\n\npackage fixtures_test\n\n" + doc +
		"func " + syntheticWitness + "(t *testing.T) {\n" +
		"\tserved := \"" + syntheticServedText + "\"\n" +
		row +
		"\t_ = served\n}\n" + decls)
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

// syntheticGap is a sound gap over the synthetic witness, with the recipe
// that runs it. Shared by the two tests that run the reader into the
// sweep, so a difference between them can only be the source each writes
// — two hand-built gaps that drifted apart would pass for the wrong
// reason, which is the argument commentOut is derived for.
func syntheticGap() (age.DialectGap, map[string]string) {
	return age.DialectGapFields{
			Sentinel: age.ErrUndefinedFunction,
			Find:     age.FindUndefinedFunctions,
			Diagnose: func(int, string, string) string { return "" },
			Witness:  syntheticWitness,
			Refused:  []age.DialectProbe{age.NewDialectProbe(syntheticProbeText, syntheticProbeAns)},
			Served:   []string{syntheticServedText},
		}.Build(),
		map[string]string{"run-it": "go test -count=1 -tags " + liveBuildTag + " -run " + syntheticWitness}
}

// readSyntheticWitness is witnessBodies over one hand-written file, with
// the parse failure surfaced as a test failure.
func readSyntheticWitness(t *testing.T, src []byte) map[string]witnessBody {
	t.Helper()
	bodies, _, err := witnessBodies(token.NewFileSet(), "live_synthetic_test.go", src)
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
			body := readSyntheticWitness(t, liveWitnessSource(tc.doc, tc.row, ""))[syntheticWitness].code
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

// TestAssertedTextIsWhatAnAssertionReads guards the reader's third axis.
// The other two are about the body's EXTENT — too wide across tests
// (TestWitnessBodiesAreScopedToTheirOwnTest) and too generous about
// comments (TestWitnessBodyIsCodeAndNotCommentary). This one is about
// what the code inside it does with the string.
//
// The first three rows are the load-bearing ones: a reader returning the
// whole body would fail the last three, and one returning "" would fail
// these. Both directions are how the sweep goes wrong — too wide and
// mutation M18 lives, too narrow and a real witness is refused.
//
// Rows one, four and six reuse the shared synthetic rows rather than
// respelling them, so the shape this test calls asserted is the same
// shape TestAnUnassertedAnswerReddensTheSweep runs into the sweep.
func TestAssertedTextIsWhatAnAssertionReads(t *testing.T) {
	for _, tc := range []struct {
		name string
		doc  string
		row  string
		// decls are sibling declarations placed after the witness, for
		// the rows whose assertion is reached through one.
		decls string
		// asserted is whether the answer must be found in the part of
		// the body an assertion reads.
		asserted bool
		// spelled is whether the answer is in the body's code at all.
		// False only where the row hides it in a comment; every other
		// row has it in the code and differs solely in what reads it,
		// which is what makes this test about assertedText and not about
		// the body reader underneath it.
		spelled bool
	}{
		{
			name:     "a literal argument is read by the assertion it is an argument of",
			row:      syntheticProbeRow,
			asserted: true,
			spelled:  true,
		},
		{
			name:     "a table column the assertion selects is read through the column's name",
			row:      syntheticColumnRow,
			asserted: true,
			spelled:  true,
		},
		{
			name: "a local the assertion names is read through the name",
			row: "\tanswer := \"" + syntheticProbeAns + "\"\n" +
				"\trequire.Contains(t, msg, answer)\n",
			asserted: true,
			spelled:  true,
		},
		{
			// Mutation M18 itself. The answer is in the body, the body
			// asserts something, and nothing connects the two.
			name:    "a local no assertion names is read by nothing",
			row:     syntheticUnassertedRow,
			spelled: true,
		},
		{
			// The decay path bd gqlc-35yu.17 names: a witness rewritten
			// to assert on a code instead of a message, with the message
			// column left standing in the table.
			name: "a table column no assertion selects is read by nothing",
			row: "\tfor _, tc := range []struct{ note string }{{note: \"" + syntheticProbeAns + "\"}} {\n" +
				"\t\trequire.NotNil(t, tc)\n\t}\n",
			spelled: true,
		},
		{
			name: "a commented-out assertion reads nothing",
			row:  commentOut(syntheticProbeRow),
		},
		{
			// The column shape is the one both AGE witnesses use, so it
			// owes the commented-out row too: a table left standing in a
			// comment must reach the reader as nothing, exactly as a
			// commented-out literal does.
			name: "a commented-out table column reads nothing",
			row:  commentOut(syntheticColumnRow),
		},
		{
			// bd gqlc-bpew, shape 1. The selector base is a receiver
			// rather than the package, so the whole assertion was
			// invisible and the answer reported unasserted.
			name:     "an answer asserted through the suite form is read",
			row:      syntheticSuiteRow,
			asserted: true,
			spelled:  true,
		},
		{
			// The widening the row above must not be. A suite-form
			// witness reaches its fixtures through methods on the same
			// receiver, and taking those for assertions would put every
			// setup argument in the corpus.
			name:    "a suite method that asserts nothing reads nothing",
			row:     syntheticSuiteNonAssertionRow,
			spelled: true,
		},
		{
			// The same widening one shape over: this row has the suite
			// form's chained selector and a name that is not a gateway,
			// so it is what the gateway list itself is holding.
			name:    "a chained call through a non-gateway reads nothing",
			row:     syntheticChainedNonAssertionRow,
			spelled: true,
		},
		{
			// bd gqlc-qo6ul, the spelling testify documents first. The
			// selector base is an ordinary local, so neither the package
			// rule nor the gateway rule reaches it.
			name:     "an answer asserted through an assertion object bound to a local is read",
			row:      syntheticBoundObjectRow,
			asserted: true,
			spelled:  true,
		},
		{
			// The widening the row above must not be. What makes a local
			// an assertion base is the call it was bound to, not that a
			// method was called on it.
			name:    "a local bound to a call with no package reads nothing",
			row:     syntheticBoundNonAssertionRow,
			spelled: true,
		},
		{
			// The same widening, one condition over: New alone is not the
			// signal, or every package's constructor would hand out
			// assertion bases.
			name:    "a local bound to a New on a foreign package reads nothing",
			row:     syntheticBoundForeignPackageRow,
			spelled: true,
		},
		{
			// And the third: the assertion package alone is not the
			// signal either. The binding call here is a real assertion by
			// the package rule, so this row also holds that reading a
			// call's own arguments does not make its RESULT a base.
			name:    "a local bound to an assertion package call that is not New reads nothing",
			row:     syntheticBoundNonConstructorRow,
			spelled: true,
		},
		{
			// bd gqlc-bpew, shape 2. The answer is an argument here and
			// reaches require one call deeper, in a declaration a reader
			// given one function body cannot see.
			name:     "an answer asserted through the witness's own helper is read",
			row:      syntheticHelperRow,
			decls:    syntheticHelperDecl,
			asserted: true,
			spelled:  true,
		},
		{
			// The widening the row above must not be. What earns a
			// helper its arguments is that an assertion is reached
			// through it, not that the witness called a sibling.
			name:    "a helper that asserts nothing reads nothing",
			row:     syntheticInertHelperRow,
			decls:   syntheticInertHelperDecl,
			spelled: true,
		},
		{
			// bd gqlc-k9v3k, the helper form and the bound-object form
			// composed. Each was already read on its own; the composition
			// was not, because assertionHelpers asks isAssertionCall with
			// no bases while it is still deciding what the helpers are.
			// So the only call inside this helper — a method on a local it
			// bound itself — answered no, the helper went unregistered,
			// and a witness calling it read nothing.
			name:     "an answer asserted through a helper that binds its own assertion object is read",
			row:      syntheticBoundHelperRow,
			decls:    syntheticBoundHelperDecl,
			asserted: true,
			spelled:  true,
		},
		{
			// The widening the row above must not be, and the one this
			// reader was: binding an assertion object is not asserting
			// through it. Registering this helper laundered its caller's
			// answer into the asserted text, which is the false GREEN
			// direction and the reason the row above is not enough on its
			// own — the two differ only in what the helper does with the
			// object it bound.
			name:    "a helper that binds an assertion object and asserts nothing reads nothing",
			row:     syntheticInertBoundHelperRow,
			decls:   syntheticInertBoundHelperDecl,
			spelled: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := readSyntheticWitness(t, liveWitnessSource(tc.doc, tc.row, tc.decls))[syntheticWitness]
			require.Equal(t, tc.spelled, strings.Contains(body.code, syntheticProbeAns),
				"this row is about what reads the answer, so whether the code spells it at all must be the stated one")
			if tc.asserted {
				require.Contains(t, body.asserted, syntheticProbeAns)
				return
			}
			require.NotContains(t, body.asserted, syntheticProbeAns,
				"an answer nothing reads is a string in a test, not a measurement the test checks")
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
	gap, recipes := syntheticGap()

	run := readSyntheticWitness(t, liveWitnessSource("", syntheticProbeRow, ""))
	require.Empty(t, witnessGaps([]age.DialectGap{gap}, run, nil, recipes),
		"the template must pass, or the complaints below could come from the template")

	spelled := readSyntheticWitness(t, liveWitnessSource("", commentOut(syntheticProbeRow), ""))
	got := strings.Join(witnessGaps([]age.DialectGap{gap}, spelled, nil, recipes), "\n")
	require.Contains(t, got, "is not carried by",
		"a probe only a comment spells is measured by nothing")
	require.Contains(t, got, "is not asserted by",
		"and neither is the answer the comment quotes")
}

// TestAnUnassertedAnswerReddensTheSweep is the other half of the same
// composition, one axis over: TestACommentedProbeReddensTheSweep asks
// whether the answer is CODE, this asks whether the code does anything
// with it.
//
// This is mutation M18 at the unit level, and it is the mutation the
// branch that shipped this table disclosed rather than fixed. In the tree
// it read: move a recorded answer out of its assertion in
// TestAGERefusesTheFunctionsItDoesNotDefine and leave the string standing
// in the same body, and TestEveryDialectGapCarriesItsWitness stayed
// green. The realistic way there is not vandalism — it is rewriting the
// witness to assert on the SQLSTATE instead of the message (bd gqlc-osf1
// is that rewrite) and leaving the message rows behind.
//
// The second assertion is what scopes the row. The probe text is still
// carried and still has to be, so a "not carried by" complaint would mean
// the answer's binding was cut by breaking something else.
func TestAnUnassertedAnswerReddensTheSweep(t *testing.T) {
	gap, recipes := syntheticGap()

	run := readSyntheticWitness(t, liveWitnessSource("", syntheticProbeRow, ""))
	require.Empty(t, witnessGaps([]age.DialectGap{gap}, run, nil, recipes),
		"the template must pass, or the complaints below could come from the template")

	spelled := readSyntheticWitness(t, liveWitnessSource("", syntheticUnassertedRow, ""))
	got := strings.Join(witnessGaps([]age.DialectGap{gap}, spelled, nil, recipes), "\n")
	require.Contains(t, got, "is not asserted by",
		"an answer no assertion reads is a claim about the server that nothing re-measures")
	require.NotContains(t, got, "is not carried by",
		"only the answer's binding is cut here — a complaint about the probe means this row measured something else")
}

// findUndefinedFunctionsOrAlternations is the test template's find: it
// reads the alternation the template probes and the function names the
// real table refuses, so a row can cut either binding without needing a
// second template. Test-only — the shipped table pairs one find per gap.
func findUndefinedFunctionsOrAlternations(src string) []age.Finding {
	if found := age.FindUndefinedFunctions(src); len(found) > 0 {
		return found
	}
	return age.FindRelationshipTypeAlternations(src)
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
// bodies maps a live test's name to the source of its body; methods maps
// a name the live files declare as a METHOD to its receiver type, which
// is not a witness but is not an absence either; recipes maps a recipe
// name to the commands it runs.
//
// Per WITNESS and not over the live corpus as a whole, which is the
// difference between a probe that is re-measured and a probe that is
// merely spelled somewhere. A text carried by the neo4j battery, or by
// an AGE test no recipe runs, is never run against the pinned image —
// and a sweep reading every file at once cannot tell those apart from
// the real thing.
func witnessGaps(gaps []age.DialectGap, bodies map[string]witnessBody, methods, recipes map[string]string) []string {
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
		if g.Witness() != "" {
			id = fmt.Sprintf("gap %d (%s)", i, g.Witness())
		}
		if g.Sentinel() == nil {
			say("%s carries no sentinel, so a caller has nothing to branch on", id)
		}
		if g.Diagnose() == nil {
			say("%s has no diagnose, so its refusal would tell the author nothing", id)
		}
		if g.Find() == nil {
			// Everything below calls find, so this gap can be checked
			// no further.
			say("%s has no find, so it reads nothing out of a query text", id)
			continue
		}

		// Empty where the gap names no witness or names one no live file
		// declares, so every binding below is reported unmeasured too —
		// which is what it is.
		body := bodies[g.Witness()]
		recv, isMethod := methods[g.Witness()]
		switch _, declared := bodies[g.Witness()]; {
		case g.Witness() == "":
			say("%s names no witness test, so nothing re-measures it against the pinned image", id)
		case declared:
			// Read as a top-level test. Every binding below is checked.
		case isMethod:
			// Written, and the author can see it — so saying it is not
			// declared would send them hunting for a test that is in
			// front of them. The remedy is what has to be said instead.
			say("%s names witness %q, which is declared as a method on %s: a witness must be a "+
				"top-level test function, because `go test -run` selects a method as Suite/Method "+
				"and no recipe can then run the witness whole", id, g.Witness(), recv)
		default:
			say("%s names witness %q, which is not declared in any live test file", id, g.Witness())
		}
		if g.Witness() != "" {
			for name, cmds := range recipes {
				if !recipeRuns(cmds, g.Witness()) {
					say("%s names witness %q, which is not run by recipe %s", id, g.Witness(), name)
				}
			}
		}

		if len(g.Refused()) == 0 {
			say("%s refuses on no probe, so its refusal rests on nothing measured", id)
		}
		for _, p := range g.Refused() {
			switch {
			case p.Text() == "":
				say("%s carries a probe with no text", id)
				continue
			case len(g.Find()(p.Text())) == 0:
				say("%s carries probe %q, which is not read by this gap — "+
					"so the measurement is of something the gate does not refuse", id, p.Text())
			}
			if !strings.Contains(body.code, p.Text()) {
				say("%s carries probe %q, which is not carried by %s", id, p.Text(), g.Witness())
			}
			switch {
			case p.Answer() == "":
				say("%s carries probe %q with no recorded answer", id, p.Text())
			case !strings.Contains(body.asserted, p.Answer()):
				// The narrow scope, and the one binding of the four that
				// is not containment in the body: an answer spelled in
				// the witness and read by nothing is a claim about the
				// server nothing re-measures (assertedText, mutation
				// M18).
				say("%s records answer %q, which is not asserted by %s", id, p.Answer(), g.Witness())
			}
		}

		if len(g.Served()) == 0 {
			say("%s records no served text, so nothing bounds what its find refuses", id)
		}
		for _, s := range g.Served() {
			if len(g.Find()(s)) > 0 {
				say("%s records %q as served and its find refuses it", id, s)
			}
			if !strings.Contains(body.code, s) {
				say("%s records served text %q, which is not carried by %s", id, s, g.Witness())
			}
		}
	}
	return complaints
}

// witnessBody is one live test's code, in the two scopes the sweep reads
// it at. Two and not one because the bindings differ in what they need:
// a probe text and a served text have to be RUN by the witness, which
// containment in its code is the available evidence for, while a recorded
// answer has to be CHECKED, which containment cannot see.
type witnessBody struct {
	// code is the whole body rendered from the parse — see witnessBodies.
	code string
	// asserted is the part of code an assertion reads — see assertedText.
	asserted string
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
// Methods are not bodies: a witness is a top-level test function, and a
// method sharing its name would put a body under a name `go test -run`
// cannot select whole — it selects one as Suite/Method. They are
// REPORTED rather than dropped, in the second map, keyed by name and
// holding the receiver type. The sweep needs them to tell two worlds
// apart that it used to answer identically: a witness nobody wrote, and
// a witness written as a method by an author who reasonably believes it
// exists. Dropping them silently made the second one read as the first
// (bd gqlc-qw57d).
//
// Split from readLiveWitnessBodies for the reason recipeBodies is split
// from readRecipes: a reader that only ever runs over the repo's own
// files cannot be shown to tell code from commentary, because the repo's
// files comment nothing out. This one takes source a test can write.
func witnessBodies(fset *token.FileSet, path string, src []byte) (map[string]witnessBody, map[string]string, error) {
	file, err := parser.ParseFile(fset, path, src, 0)
	if err != nil {
		return nil, nil, fmt.Errorf("parse %s: %w", path, err)
	}
	helpers := assertionHelpers(file)
	bodies := make(map[string]witnessBody)
	methods := make(map[string]string)
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		if fn.Recv != nil {
			recv, err := receiverText(fset, fn.Recv)
			if err != nil {
				return nil, nil, fmt.Errorf("render %s's receiver in %s: %w", fn.Name.Name, path, err)
			}
			methods[fn.Name.Name] = recv
			continue
		}
		var code strings.Builder
		if err := format.Node(&code, fset, fn.Body); err != nil {
			return nil, nil, fmt.Errorf("render %s's body in %s: %w", fn.Name.Name, path, err)
		}
		asserted, err := assertedText(fset, fn.Body, helpers)
		if err != nil {
			return nil, nil, fmt.Errorf("render %s's assertions in %s: %w", fn.Name.Name, path, err)
		}
		bodies[fn.Name.Name] = witnessBody{code: code.String(), asserted: asserted}
	}
	return bodies, methods, nil
}

// receiverText renders a method's receiver TYPE, so a complaint can name
// the suite a witness was written on rather than merely saying it has
// one. The pointer star is kept: `*AGESuite` is what the author reads in
// their own file, and normalising it away would make the message name a
// declaration that is not there.
func receiverText(fset *token.FileSet, recv *ast.FieldList) (string, error) {
	if recv == nil || len(recv.List) == 0 {
		return "", nil
	}
	var text strings.Builder
	if err := format.Node(&text, fset, recv.List[0].Type); err != nil {
		return "", err
	}
	return text.String(), nil
}

// assertionPackages are the identifiers a call can be rooted at to count
// as an assertion here.
var assertionPackages = map[string]bool{"require": true, "assert": true}

// assertionGateways are the no-argument methods testify's suite type
// hands an assertion object back from, so `s.Require().Contains(...)` is
// an assertion however `s` is named. Keyed on the gateway rather than on
// the assertion method after it: a list of testify's assertion names
// would rot as testify grows one, and a receiver-name rule would take
// every method call on `s` — including the setup calls a suite-form
// witness reaches its fixtures through.
//
// The bare form `s.Contains(...)`, promoted from an embedded suite.Suite,
// is NOT recognised. Deciding it needs the method set of `s`, which is a
// type this reader cannot resolve: the live files are in another module
// and behind a build tag, so all it has is their syntax. It stays the
// false RED bd gqlc-bpew describes, one shape narrower.
var assertionGateways = map[string]bool{"Require": true, "Assert": true}

// isAssertionCall reports whether call is one this reader takes the
// arguments of. Four shapes: a call on an assertion package, a call on a
// local an assertion object was bound to, a call through a suite gateway,
// and a call to a helper that reaches any of those. helpers may be nil —
// that is how assertionHelpers asks the question, while it is still
// deciding what the helpers are.
func isAssertionCall(call *ast.CallExpr, helpers, bases map[string]bool) bool {
	// A constructor is not an assertion, though it is rooted at an
	// assertion package and the rule below would otherwise take it for
	// one. That let a helper which only BINDS an object and asserts
	// nothing through it register as an assertion helper, so a witness
	// calling such a helper had its answer laundered into the asserted
	// text — an unasserted answer passing the sweep, which is the one
	// direction this fence exists to deny (bd gqlc-k9v3k).
	if isAssertionConstructor(call) {
		return false
	}
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		return helpers[fun.Name]
	case *ast.SelectorExpr:
		if pkg, ok := fun.X.(*ast.Ident); ok {
			return assertionPackages[pkg.Name] || bases[pkg.Name]
		}
		gateway, ok := fun.X.(*ast.CallExpr)
		if !ok {
			return false
		}
		sel, ok := gateway.Fun.(*ast.SelectorExpr)
		return ok && assertionGateways[sel.Sel.Name]
	}
	return false
}

// assertionBases are the names a body binds an assertion OBJECT to, so a
// call on one of them is an assertion however the local is spelled. This
// is testify's first-documented form, `r := require.New(t)`, where the
// selector base is an ordinary local: no package identifier to root the
// call at and no gateway call to reach it through (bd gqlc-qo6ul).
//
// Keyed on what the name was bound TO, never on the name itself. A rule
// reading `r` or `req` would take every method call on any local that
// happened to be spelled that way, which is the widening the gateway list
// is already holding one shape over.
//
// It is syntax and not types, like everything else here: the live files
// are in another module behind a build tag, so a reader has their source
// and no method sets. That bounds it — an object returned by anything but
// a literal require.New/assert.New call, or handed across a function
// boundary, is not followed, and stays the false RED bd gqlc-bpew
// describes rather than a way for an unasserted answer to pass.
func assertionBases(body *ast.BlockStmt) map[string]bool {
	bases := make(map[string]bool)
	bind := func(names []ast.Expr, values []ast.Expr) {
		for i, name := range names {
			id, ok := name.(*ast.Ident)
			if !ok || i >= len(values) || !isAssertionConstructor(values[i]) {
				continue
			}
			bases[id.Name] = true
		}
	}
	ast.Inspect(body, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.AssignStmt:
			bind(v.Lhs, v.Rhs)
		case *ast.ValueSpec:
			names := make([]ast.Expr, len(v.Names))
			for i, name := range v.Names {
				names[i] = name
			}
			bind(names, v.Values)
		}
		return true
	})
	return bases
}

// isAssertionConstructor reports whether expr is a literal call to an
// assertion package's New — the only right-hand side that earns a name a
// place in assertionBases.
func isAssertionConstructor(expr ast.Expr) bool {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "New" {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && assertionPackages[pkg.Name]
}

// assertionHelpers are the functions this file declares that reach an
// assertion, so a witness calling one is asserting through it.
//
// One hop and one file. A helper calling a second helper is not followed,
// and neither is a helper declared in a sibling live file — each is a
// false RED of the kind bd gqlc-bpew narrows rather than a way for an
// unasserted answer to pass, because what widens the corpus here is
// finding MORE assertions, never fewer.
func assertionHelpers(file *ast.File) map[string]bool {
	helpers := make(map[string]bool)
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			// nil helpers, because that is the question being answered;
			// but the body's OWN bases, because a helper may assert
			// through an object it bound itself, and since a constructor
			// stopped counting as an assertion nothing else in such a
			// helper reaches one (bd gqlc-k9v3k).
			if !ok || !isAssertionCall(call, nil, assertionBases(fn.Body)) {
				return true
			}
			helpers[fn.Name.Name] = true
			return false
		})
	}
	return helpers
}

// assertedText is the part of a witness's body an assertion reads, and
// the answer half of the sweep is checked against this rather than
// against the whole body.
//
// Why it is not the whole body: a body carrying the server's answer as a
// dead local, a log line or a table column nothing looks at satisfies a
// containment check exactly as well as one asserting it, so the sweep
// could say the answer is spelled in the witness and could not say the
// witness checks it. Measured as review mutation M18 on the branch that
// shipped this table, which disclosed it rather than fixing it; bd
// gqlc-35yu.17 is the fix, and TestAnUnassertedAnswerReddensTheSweep is
// the row that would have caught it.
//
// It is ONE HOP and not a dataflow analysis. The corpus is every
// assertion call's arguments — see isAssertionCall for what counts as one
// — plus the values bound to any name those arguments read, through an
// assignment, a var declaration, or a keyed field in a composite literal.
// That is enough for the two shapes this repo's witnesses use: a literal
// passed straight to require.Contains, and a table column reached as
// tc.wantMessage.
//
// A range variable is deliberately NOT resolved back to the slice it
// ranges over. Doing so would pull every field of every row of the table
// into the corpus the moment any assertion mentioned tc at all, which
// puts the unasserted columns back in and makes this function a slower
// spelling of the body it replaced.
func assertedText(fset *token.FileSet, body *ast.BlockStmt, helpers map[string]bool) (string, error) {
	var out strings.Builder
	var failed error
	render := func(n ast.Node) {
		var buf strings.Builder
		if err := format.Node(&buf, fset, n); err != nil {
			failed = err
			return
		}
		out.WriteString(buf.String())
		out.WriteByte('\n')
	}

	bases := assertionBases(body)

	// The names an assertion reads, which is what makes the second pass
	// a hop rather than a second copy of the body.
	read := make(map[string]bool)
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || !isAssertionCall(call, helpers, bases) {
			return true
		}
		for _, arg := range call.Args {
			render(arg)
			ast.Inspect(arg, func(n ast.Node) bool {
				if id, ok := n.(*ast.Ident); ok {
					read[id.Name] = true
				}
				return true
			})
		}
		return true
	})

	ast.Inspect(body, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.KeyValueExpr:
			if key, ok := v.Key.(*ast.Ident); ok && read[key.Name] {
				render(v.Value)
			}
		case *ast.AssignStmt:
			for i, lhs := range v.Lhs {
				id, ok := lhs.(*ast.Ident)
				if ok && read[id.Name] && i < len(v.Rhs) {
					render(v.Rhs[i])
				}
			}
		case *ast.ValueSpec:
			for i, id := range v.Names {
				if read[id.Name] && i < len(v.Values) {
					render(v.Values[i])
				}
			}
		}
		return true
	})
	return out.String(), failed
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
//
// The method names carry no such guard, and must not: two suites may each
// declare a method of the same name and that is legal Go, so requiring
// uniqueness would red a correct pair of live files. The first wins, which
// is arbitrary only in the message — the remedy a method-declared witness
// gets is "make it top-level", and that does not turn on which suite
// holds it.
func readLiveWitnessBodies(t *testing.T) (map[string]witnessBody, map[string]string) {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(repoRoot, liveGlob))
	require.NoError(t, err)
	require.NotEmpty(t, paths, "no live test files found at %s — the sweep would pass by reading nothing", liveGlob)

	fset := token.NewFileSet()
	bodies := make(map[string]witnessBody)
	methods := make(map[string]string)
	for _, p := range paths {
		src, err := os.ReadFile(p) //nolint:gosec // a repo-relative path this test builds itself
		require.NoError(t, err, "read %s", p)
		read, readMethods, err := witnessBodies(fset, p, src)
		require.NoError(t, err, "read the test bodies in %s", p)
		for name, body := range read {
			require.NotContains(t, bodies, name,
				"two live tests named %s: the sweep would read whichever was parsed last", name)
			bodies[name] = body
		}
		for name, recv := range readMethods {
			if _, seen := methods[name]; !seen {
				methods[name] = recv
			}
		}
	}
	return bodies, methods
}

// recipeRuns reports whether cmds actually runs the live top-level test
// named witness — every probe row of it. Five things have to hold: the
// quoting closes, the body runs a `go test`, and THAT invocation builds
// liveBuildTag, has every -run selecting the witness WHOLE, and has no
// -skip reaching it. Each is a way a recipe was measured running no
// witness while this function said yes.
//
// The last three are read from one invocation's own fields, never pooled
// across the body. Pooled, with the `go test` found separately, a
// `-tags codegen_live` on any OTHER command satisfies the tag
// requirement for a `go test` carrying none:
// review mutation POOLTAG2 put this justfile's own fence idiom
// (`go vet -tags codegen_live ./...`, line 230) on the line above and
// dropped the tag off the test, which restored review mutation T1 with
// one added line — measured, the recipe then printed "[no test files]"
// for all 154 packages under test/data/codegen and exited 0, starting no
// container and running no witness, while the sweep stayed green.
//
// This is the half strings.Contains could not say, and the same defect
// class as reading a witness's body as source bytes (see witnessBodies).
// An AGE recipe already carries a -skip, so a witness name in the
// command line is as likely to be there to REMOVE the test as to select
// it; and a name in a comment is not in the command line at all, which
// is why recipeBodies strips comments before this ever sees them.
// Measured as review mutations L18/L19/L20 — all three left the sweep
// green while no CI job ran the witness.
//
// "Whole" is the asymmetry that makes this function worth having.
// go test reads the elements after the first
// as a SUBTEST filter, and both AGE witnesses run every probe row as a
// subtest (live_age_dialect_test.go), so a -run of `W/toTimestamp` runs
// one probe of five and a -run of `W/x` runs none at all. Both print
// `--- PASS: W` and exit 0. Measured against go1.26.5: the second prints
// `[no tests to run]` ONLY when nothing else on the command line
// matched, so in the shape a live recipe has — `-run 'TestLiveSmoke|W/x'`,
// where the smoke battery still runs — even that tell is absent. Without
// this rule a -skip carving out ONE probe is refused while a -run
// dropping ALL TWELVE is accepted (review mutations MC, MA, MB).
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
//     liverecipes.GoTestInvocations, review mutation P3.)
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
//     (review mutation P3). Silence, and see
//     liverecipes.GoTestInvocations for why the same superset is a
//     complaint for the -count=1 rule.
func recipeRuns(cmds, witness string) bool {
	invocations, unterminated := liverecipes.GoTestInvocations(cmds)
	// A line this reader could not finish parsing is not a line it can
	// report on. It arises when liverecipes.StripComment cuts inside a
	// quoted argument, and answering from the fragment is how "nothing
	// to read" became "runs everything" (review mutation V3).
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
	tags := liverecipes.FlagValues(fields, "tags")
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
	for _, pattern := range liverecipes.FlagValues(fields, "run") {
		if _, wholly := liverecipes.Selects(pattern, witness); !wholly {
			return false
		}
	}
	for _, pattern := range liverecipes.FlagValues(fields, "skip") {
		if reaches, _ := liverecipes.Selects(pattern, witness); reaches {
			return false
		}
	}
	return true
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

		joined, found := liverecipes.RecipeBody(src, name)
		if !found {
			complaints = append(complaints,
				fmt.Sprintf("recipe %s is not in the justfile, so nothing this sweep says about it is true", name))
			continue
		}
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
		invocations, _ := liverecipes.GoTestInvocations(joined)
		for _, fields := range invocations {
			if !slices.Contains(liverecipes.FlagValues(fields, "count"), "1") {
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
			// the length pin over ageLiveRecipes goes on passing.
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
// liverecipes.StripComment and liverecipes.Fields to the artefact they
// are pointed at. Neither models backslash escapes, command
// substitution, heredocs or here-strings, and neither expands a
// variable.
//
// The justfile uses all of those, in recipes this reader never sees, so
// "no recipe here uses them" is false of the file. Line numbers naming
// where would be worse than none: nothing checks them, and one edit
// above the first would make this comment, liverecipes.StripComment's
// and ADR 0028 wrong at once. What is true is narrower and is checked
// here: recipeBodies reads the recipes ageLiveRecipes names and nothing
// else, and those use none of them. That is a fact about
// single-line recipes on a day, not a law, which is why it is a
// test.
//
// The direction each would fail in is why the bound is worth holding: a
// cut or a quote in the wrong place leaves the reader FEWER flags than
// the shell runs, an absent -run selects everything, and the recipe then
// reads as running a witness it does not run (review mutations V3, V4b).
// Silence, and this file exists to refuse it.
func TestTheRecipesThisReaderParsesStayInsideTheShellItModels(t *testing.T) {
	// The shapes liverecipes.StripComment and liverecipes.Fields do not
	// model. This list is the whole of what this test asserts, so it is
	// guarded before it is used: review mutation A9 deleted all four rows
	// and the package stayed green at exit 0, this test among it, printing
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
	// name). That argument is about THIS loop and no other: it says
	// nothing about the shapes list built above, which is why that list
	// carries the require.Len of its own.
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
// because strings.Contains could not tell these shapes apart: an AGE
// recipe names a witness AND carries a -skip, so "the name appears" says
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
			// Making liverecipes.StripComment the identity leaves both green
			// (review mutation N1); deleting recipeRuns' -run loop kills
			// both (N3). The comment half — text that is spelled is not
			// text that runs — is carried by ONE row of
			// TestRecipeReaderComplainsOnEachBrokenRecipe: "a trailing
			// comment does not carry -count=1 either", which is what
			// dies to N1. Its whole-line sibling does NOT die to N1,
			// because a comment on its own line is not a command and
			// reading flags per invocation drops it before the comment
			// strip is asked — so the trailing-comment row is the only
			// one carrying that half.
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
			// The row above's mirror. The identical shape on -run
			// removes strictly MORE measurement, so accepting it while
			// refusing the -skip would be backwards. go test runs one
			// probe of five here (review mutation MA).
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
			// The other half of liverecipes.StripComment's rule: sh starts a
			// comment at a `#` that STARTS A WORD, so an unquoted
			// `p=a#b` keeps its `#`
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
			// Review mutation POOLTAG2. The tag is on a command that is
			// not the test run — this justfile's own fence idiom,
			// `go vet -tags codegen_live ./...` (line 230) — so the
			// battery is compiled by nothing and `go test` prints
			// "[no test files]" for every package under
			// test/data/codegen at exit 0. That is review mutation T1
			// with one line added, and it survives any reading that
			// pools the tag across the body. Its
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
