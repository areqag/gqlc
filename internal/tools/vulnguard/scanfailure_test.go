package vulnguard_test

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// scanAnchor is the invocation whose status the whole gate turns on. Replaced
// per row below by a stub that produces chosen bytes and a chosen status, which
// is the only way to reach the failure branch from here: the harness's stub
// toolchain answers a healthy scan and exits 0.
const scanAnchor = `        out="$(cd "${dir}" && go run golang.org/x/vuln/cmd/govulncheck@latest "${tagflag[@]}" -test -show verbose ./... 2>&1)" || rc=$?`

// failingScan is that anchor rewritten to emit `out` and exit non-zero without
// running anything.
//
// `%b` rather than `%s`: the escapes below have to become real line breaks. The
// recipe's discriminator is anchored to the start of a line, and a fixture that
// delivered one long line would take the not-reported branch whatever the bytes
// said — the completed row would then pass for a reason no reader could see.
func failingScan(out string) edit {
	return edit{
		old: scanAnchor,
		new: `        out="$(printf '%b' ` + "'" + out + "'" + `)"; rc=1`,
	}
}

// A scan that ran to completion and found a called vulnerability is reported as
// exactly that.
func TestVulnNamesACalledVulnerabilityWhenTheScanCompleted(t *testing.T) {
	run := runVuln(t, failingScan(
		"Vulnerability #1: GO-2026-6355\\n"+
			"Your code is affected by 2 vulnerabilities from 1 module.\\n"))
	requireRefusal(t, run, "CALLS a known vulnerability")
}

// The row that is the regression. A scan that never produced a report at all —
// the module would not load, or `go run` could not fetch govulncheck — used to
// be reported in the wording reserved for a called vulnerability, because the
// recipe named two causes and then delegated the choice between them to the
// reader with "the output above says which".
//
// All three causes exit 1 (measured 2026-09-02, bd gqlc-y2dgv), so the status
// cannot separate them and the message cannot be derived from it. Telling a
// reader that this tree CALLS something, when nothing was scanned, is the
// expensive direction of the two: it is a claim about the code made from an
// absence of evidence about the code.
func TestVulnDoesNotClaimACallWhenTheScanNeverReported(t *testing.T) {
	run := runVuln(t, failingScan(
		"a.go:3:8: no required module provides package gqlc.invalid/nonexistent/pkg\\n"))
	requireRefusal(t, run, "has NOT been shown to call anything")
	require.NotContainsf(t, run.output, "CALLS a known vulnerability",
		"`%s` reported a scan that produced no findings at all in the wording reserved "+
			"for a tree that calls one. That is the defect this row exists for: the reader "+
			"is told something about their code that the run did not measure (bd gqlc-y2dgv). "+
			"Output:\n%s", vulnRecipe, run.output)
}

// The discriminator is the summary line, so a row has to witness that it is
// read rather than assumed — otherwise a recipe that hardcoded either branch
// would satisfy one row above and be caught only by the other, and a reader
// could not tell which branch the message came from.
//
// This is the same bytes as the completed row with the summary line removed and
// the vulnerability header left in place. A recipe keying on "Vulnerability #"
// rather than on completion takes the wrong branch here.
func TestVulnKeysOnTheSummaryLineRatherThanOnAVulnerabilityHeader(t *testing.T) {
	run := runVuln(t, failingScan("Vulnerability #1: GO-2026-6355\\n"))
	requireRefusal(t, run, "has NOT been shown to call anything")
}
