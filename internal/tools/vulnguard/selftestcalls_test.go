package vulnguard_test

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// The `vuln` recipe's selftests each guard a fixture: a directory whose every
// Go file is build-constrained, and a file carrying a negated GOOS term. Their
// BODIES are well witnessed — mutate an assertion inside either and the recipe
// refuses at once — and their CALLS are witnessed by nothing. Measured at
// 620cea66: deleting the `selftest_platformtag` call from the justfile, and
// separately the `selftest_tagblind` call, each leaves `just vuln` at rc=0
// (bd gqlc-65sl, the class bd gqlc-eo46 named).
//
// The recursion that defect invites — a counter around the call site, which is
// itself a deletable line — does not terminate inside the script. So the
// assertion is made from outside it: run the real recipe over a tree in which
// the selftest's PRECONDITION is violated, and require the recipe to refuse and
// to name what it found. There is no line inside the clause that can produce
// that refusal once the clause is not called, so the call is held by it.
//
// The cases are a table rather than one test each because what has to be held
// is the FAMILY, not two members of it: TestEverySelftestInTheVulnRecipeIsWitnessed
// below reconciles this table against the selftests the recipe declares, so a
// new one arrives with a witness or reddens this package.

// selftestCase is one selftest, the tree that violates its precondition, and
// the words the refusal has to carry.
type selftestCase struct {
	// name is the shell function in the recipe, spelled exactly.
	name string

	// spoil violates the precondition inside the throwaway tree.
	spoil func(t *testing.T, dir string)

	// says is a fragment of the refusal the clause writes. Asserting the
	// message and not just the status is what tells this apart from a recipe
	// that died somewhere else over the same spoiled tree.
	says string
}

var selftestCases = []selftestCase{
	{
		// The fixture directory is gone, so nothing in the tree has the one
		// shape `go list ./...` does not match — the shape that exercises both
		// the filesystem walk and the coverage assertion the recipe makes after
		// it. selftest_tagblind's first clause is the only thing that notices:
		// removing the directory removes it from both sides of the comparison
		// the coverage assertion makes, so that assertion stays green over it.
		name: "selftest_tagblind",
		spoil: func(t *testing.T, dir string) {
			t.Helper()
			require.NoError(t, os.RemoveAll(filepath.Join(dir, "test", "data", blindDir)))
		},
		says: "is gone, so nothing in this tree exercises the filesystem walk",
	},
	{
		// The fixture file keeps its directory and loses the negated GOOS term
		// it exists to reproduce. The walk still finds it, the derived tags are
		// unchanged, and every coverage assertion in the recipe passes; the
		// only thing left asserting that this tree still witnesses what the
		// derivation does with a platform term is selftest_platformtag's second
		// clause.
		name: "selftest_platformtag",
		spoil: func(t *testing.T, dir string) {
			t.Helper()
			src := filepath.Join(dir, "test", "data", "platformtag", "platformtag.go")
			before, err := os.ReadFile(src)
			require.NoError(t, err)
			after := strings.Replace(string(before), "//go:build !windows", "//go:build !plan9", 1)
			require.NotEqual(t, string(before), after,
				"the platformtag fixture this harness writes no longer carries the term this "+
					"case rewrites, so the run below would measure nothing")
			require.NoError(t, os.WriteFile(src, []byte(after), 0o600))
		},
		says: "no longer carries '//go:build !windows'",
	},
}

// TestVulnRefusesATreeItsSelftestsAreThereToRefuse runs each case and requires
// the recipe to refuse by name.
//
// Deleting the corresponding call from the justfile turns the matching row red
// — which is the whole content of bd gqlc-65sl, and is what nothing asserted
// before this file.
func TestVulnRefusesATreeItsSelftestsAreThereToRefuse(t *testing.T) {
	for _, c := range selftestCases {
		t.Run(c.name, func(t *testing.T) {
			run := runVulnOver(t, c.spoil)
			require.NotZerof(t, run.status, "`%s` exited 0 over a tree that violates %s's "+
				"precondition, so that selftest did not run: its call site has been deleted, "+
				"commented out, or moved behind a branch this tree does not take. The clause "+
				"itself may be intact — its body is witnessed elsewhere in this package — and "+
				"a clause nothing calls is a deleted clause (bd gqlc-65sl, bd gqlc-eo46). It "+
				"should have said %q. Output:\n%s", vulnRecipe, c.name, c.says, run.output)
			require.Containsf(t, run.output, c.says, "`%s` refused over a tree that violates "+
				"%s's precondition, and said something other than %q, so what stopped it is "+
				"not that selftest and this row is measuring some other failure "+
				"(bd gqlc-65sl). Output:\n%s", vulnRecipe, c.name, c.says, run.output)
		})
	}
}

// selftestDefinition and selftestCall read the two halves out of the recipe
// body. A definition is a shell function declaration at the recipe's own
// indentation; a call is the bare name on a line of its own, which is how every
// one of them is invoked today.
var (
	selftestDefinition = regexp.MustCompile(`(?m)^\s*(selftest_[A-Za-z0-9_]+)\(\)\s*\{`)
	selftestCall       = regexp.MustCompile(`(?m)^\s*(selftest_[A-Za-z0-9_]+)\s*$`)
)

// TestEverySelftestInTheVulnRecipeIsWitnessed reconciles the table above
// against the recipe, in both directions.
//
// Without it the table is a list of two selftests that happened to be thought
// of, and the next one added to the recipe arrives with its call site
// unwitnessed — which is exactly how the two above came to be that way. It is
// also what keeps the table from outliving a selftest that was deleted, since
// a case whose spoiled tree no clause refuses would fail above for a reason
// whose message would send the reader looking for a deleted call rather than a
// deleted clause.
//
// The call half is checked too: a selftest defined and never called is the
// defect this file exists for, arriving fully formed, and the row above cannot
// see the difference between that and a missing case.
func TestEverySelftestInTheVulnRecipeIsWitnessed(t *testing.T) {
	src, err := os.ReadFile(filepath.Join(repoRoot, justfile))
	require.NoError(t, err)
	lines := strings.Split(string(src), "\n")
	start, end := recipeSpan(t, lines, vulnRecipe)
	body := strings.Join(lines[start+1:end], "\n")

	defined := namesIn(selftestDefinition, body)
	require.NotEmptyf(t, defined, "no `selftest_*` function was read out of the `%s` recipe. "+
		"Either they are gone — in which case delete this file with them — or they are spelled "+
		"a way selftestDefinition does not read, and every reconciliation below is then between "+
		"two sets neither of which was measured (bd gqlc-65sl)", vulnRecipe)

	called := namesIn(selftestCall, body)
	witnessed := make([]string, 0, len(selftestCases))
	for _, c := range selftestCases {
		witnessed = append(witnessed, c.name)
	}
	sort.Strings(witnessed)

	require.Equalf(t, defined, witnessed, "the selftests the `%s` recipe declares and the ones "+
		"selftestCases witnesses have parted. A declared selftest with no case is one whose CALL "+
		"nothing holds — deleting that call leaves the gate green, which is bd gqlc-65sl arriving "+
		"again; add a case whose spoiled tree the clause refuses. A case naming no declared "+
		"selftest is a witness for a clause that is gone; delete it.", vulnRecipe)

	require.Equalf(t, defined, called, "the `%s` recipe declares a selftest it does not call on "+
		"a line of its own, or calls one it does not declare. The first is the defect this file "+
		"exists to catch, and it is reported here rather than by the runs above because a clause "+
		"that is never called cannot refuse anything and every case would then read as a missing "+
		"case (bd gqlc-65sl).", vulnRecipe)
}

// namesIn collects the first submatch of every match, sorted and deduplicated.
func namesIn(re *regexp.Regexp, body string) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range re.FindAllStringSubmatch(body, -1) {
		if seen[m[1]] {
			continue
		}
		seen[m[1]] = true
		out = append(out, m[1])
	}
	sort.Strings(out)
	return out
}
