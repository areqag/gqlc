package vulnguard_test

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// The control. With nothing edited, the recipe runs to the end over the
// throwaway tree and exits 0.
//
// Without it a harness that ran nothing — a justfile just could not parse, a
// stub the recipe dies on before reaching what it was asked about, a fixture
// tree the selftests refuse — would satisfy every "refused, and said this"
// below while measuring nothing, because the recipe would be failing for the
// harness's reasons on each of them.
func TestVulnAcceptsAScanThatPlacedTheStandardLibrary(t *testing.T) {
	run := runVuln(t)
	require.Zerof(t, run.status, "`%s` exited %d over an unedited throwaway tree, so every "+
		"refusal measured below could be this harness rather than the defect it applied. "+
		"Output:\n%s", vulnRecipe, run.status, run.output)
}

// The grading's accepting arm takes anything spelled `go`, so the rendering an
// unplaced standard library produces is accepted.
//
// The first `expect_refusal` call is what catches this, and nothing else does:
// the clause still refuses the header-less shape the second call hands it, and
// the witness after them reads the header out of an ACCEPTANCE, which carries
// it. Deleting that call leaves this run green (bd gqlc-agt0 row M13).
func TestVulnRefusesAGradingThatAcceptsTheUnplacedRendering(t *testing.T) {
	run := runVuln(t, edit{
		old: `            go?*) printf '%s\n%s\n' "${where}" "${line}"; return 0 ;;`,
		new: `            go*) printf '%s\n%s\n' "${where}" "${line}"; return 0 ;;`,
	})
	requireRefusal(t, run, "was ACCEPTED, so the standard-library half of every scan")
}

// The grading's absent-header branch is disabled, so output carrying no scan
// header falls through to the refusal written for a header that placed no
// version. It is still refused; the reason it gives is the wrong one.
//
// The second `expect_refusal` call catches this, through the clause that
// asserts WHICH refusal fired. Deleting the call leaves this run green (row
// M14), and so does widening that clause's `case` to `*)` (row M9).
func TestVulnRefusesAGradingThatNamesTheWrongCause(t *testing.T) {
	run := runVuln(t, edit{
		old: `        if [ -z "${line}" ]; then`,
		new: `        if false; then`,
	})
	requireRefusal(t, run, `the message does not say "printed no line"`)
}

// The grading refuses without quoting the header it read, which is the
// evidence a real trip carries.
//
// Both `expect_refusal` calls pass over this: the markers they match on are in
// the message text, which is untouched. The witness after them is what catches
// it, and widening its `case` to `*)` leaves this run green (row M16).
func TestVulnRefusesAGradingThatQuotesNoHeader(t *testing.T) {
	run := runVuln(t, edit{
		old: `        echo "         ${line}" >&2`,
		new: `        echo "         (withheld)" >&2`,
	})
	requireRefusal(t, run, "refused without quoting the header it")
}

// The grading's accepting arm prints the name it graded and drops the header
// line, so an acceptance is tied to no scan.
//
// The refusing paths are untouched, so the two `expect_refusal` calls and the
// witness all pass. The two-line acceptance contract is what catches it, and
// replacing that contract with `if false` leaves this run green (row MW) —
// `graded_line` is then the empty string and the call site's `grep -qxF --`
// matches one of the blank lines the scan output carries.
func TestVulnRefusesAnAcceptanceThatEchoesBackNoHeader(t *testing.T) {
	run := runVuln(t, edit{
		old: `            go?*) printf '%s\n%s\n' "${where}" "${line}"; return 0 ;;`,
		new: `            go?*) printf '%s\n' "${where}"; return 0 ;;`,
	})
	requireRefusal(t, run, "accepted a placed header without echoing back")
}

// The loop stops accumulating the name each grading printed, so every module
// is scanned and none is recorded as graded.
//
// The per-module tally after the loop is what catches it. Replacing its
// emptiness test with `if false` leaves this run green (row M19), and so does
// turning its `comm -13` into `comm -23` (row MA): the mis-directed comm asks
// which names were graded and not scanned, which is empty here, so the guard
// mis-answers while looking intact.
func TestVulnRefusesAModuleScannedWithoutBeingGraded(t *testing.T) {
	run := runVuln(t, edit{
		old: `            graded+="${graded_where}"$'\n'`,
		new: `            graded+=""`,
	})
	requireRefusal(t, run, "these modules were scanned but not graded for whether govulncheck placed")
}

// The call site hands the grading the witness's own fabricated header instead
// of the scan it just ran, so the acceptance describes output this module
// never produced.
//
// The header the grading reports is matched back against the scan, and that is
// what catches it — the fabricated header names 2 modules where the scan names
// more. Replacing the match with `if false` leaves this run green (row MG),
// and so does reading its haystack from the grading's own output rather than
// the scan's (row MP), which makes the match compare the acceptance against
// itself.
func TestVulnRefusesAGradingOfSomeOtherOutput(t *testing.T) {
	run := runVuln(t, edit{
		old: `        if stdlib_graded="$(refuse_unplaced_stdlib "${out}" "${dir}")"; then`,
		new: `        if stdlib_graded="$(refuse_unplaced_stdlib "${placed_header}" "${dir}")"; then`,
	})
	requireRefusal(t, run, "reports a header the scan of")
}

// The header the grading reported is trimmed before it is matched back, so it
// is a substring of a line the scan printed and not a line of it.
//
// The match is anchored to whole lines, which is what catches it. Replacing
// the match with `if false` leaves this run green (row MG), and so does
// dropping the `x` from `grep -qxF` (row ML), which is the anchoring itself.
func TestVulnRefusesAHeaderThatIsOnlyPartOfALineTheScanPrinted(t *testing.T) {
	run := runVuln(t, edit{
		old: `            graded_line="$(sed -n '2p' <<<"${stdlib_graded}")"`,
		new: `            graded_line="$(sed -n '2p' <<<"${stdlib_graded}" | sed 's/:$//')"`,
	})
	requireRefusal(t, run, "reports a header the scan of")
}

// requireRefusal: the run failed, and it named what it found.
//
// The status alone is not enough. A recipe that died three hundred lines
// earlier for a reason nothing here applied also exits non-zero, and reading
// that as a refusal is how a test reports on a defect it never reached.
func requireRefusal(t *testing.T, run vulnRun, says string) {
	t.Helper()
	require.NotZerof(t, run.status, "`%s` exited 0 with a deliberate defect applied. The "+
		"assertion that catches it is gone, downgraded or silenced, and the standard-library "+
		"half of the scan is being accepted unexamined (bd gqlc-agt0). It should have said "+
		"%q. Output:\n%s", vulnRecipe, says, run.output)
	require.Containsf(t, run.output, says, "`%s` refused, and said something other than %q, "+
		"so what stopped it is not what this test applied (bd gqlc-agt0). Output:\n%s",
		vulnRecipe, says, run.output)
}
