package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// Tests target plan(), apply() and run() directly. Nothing here reaches
// GitHub or bd: both reads and the one write are injected, and the write leg
// used on every dry-run test fails the test if it is called at all. That is a
// hard constraint rather than a preference — this tool's write is `gh issue
// close` against the live repository, so a test that "just checks the dry run"
// against the real API would be a test that mutates nothing only because it
// happened to plan nothing.
//
// testdata/issues.json and testdata/beads.json are REAL, captured 2026-08-19
// with `gh issue view <n> --json number,title,body,createdAt,state` over the 36
// issues named by bd gqlc-mb8v, and `bd list --status all --limit 0 --json`
// filtered to the 18 canonical beads. One field is edited: the 18 orphans'
// `state` is set back to OPEN, because they were closed by hand on 2026-08-18
// (the manual remediation recorded on bd gqlc-mmej) and the fixture has to hold
// the corpus this tool exists to act on. Title, body and createdAt are
// untouched, and a close does not alter any of them — so the three fields the
// predicate reads are exactly what GitHub holds.

const (
	titleA = "resolver: describeColumnType panics on a nil list element type"
	titleB = "ci: govulncheck is red on master"
	bodyA  = "The sweep walks every column and dereferences ElementType.\n\nBead: gqlc-t802"
	bodyB  = "Different body entirely.\n"

	baseTime = "2026-08-17T05:17:10Z"
)

func at(offset time.Duration) string {
	t, err := time.Parse(time.RFC3339, baseTime)
	if err != nil {
		panic(err)
	}
	return t.Add(offset).UTC().Format(time.RFC3339)
}

func openIssue(number int, title, body, createdAt string) issue {
	return issue{Number: number, Title: title, Body: body, CreatedAt: createdAt, State: "OPEN"}
}

func closedIssue(number int, title, body, createdAt string) issue {
	i := openIssue(number, title, body, createdAt)
	i.State = "CLOSED"
	return i
}

func boundTo(id string, number int) bead {
	return bead{ID: id, ExternalRef: fmt.Sprintf("https://github.com/areqag/gqlc/issues/%d", number)}
}

// filler keeps plan()'s empty-input refusals out of the way of the rows that
// are about the predicate: every table row needs at least one issue and one
// bead that the row itself is not about.
var (
	fillerIssue = closedIssue(9001, "unrelated", "unrelated", at(-time.Hour))
	fillerBead  = boundTo("gqlc-filler", 9001)
)

func planOrFatal(t *testing.T, issues []issue, beads []bead) []finding {
	t.Helper()
	got, err := plan(issues, beads, defaultWindow)
	if err != nil {
		t.Fatalf("plan: unexpected error: %v", err)
	}
	return got
}

func TestPlanDecidesEachShapeOfCandidate(t *testing.T) {
	tests := []struct {
		name string
		// issues and beads exclude the filler, which is appended.
		issues []issue
		beads  []bead
		// want is one line per finding: "CLOSE 452->453" or "REFUSE 452".
		want []string
		// wantReason, when set, must be a substring of the sole finding's
		// Reason; it is what stops two different refusals passing each other's
		// row.
		wantReason string
	}{
		{
			// The shape all 16 clean pairs have: same title, same body bytes,
			// created a second apart, and only the later one is in the ledger.
			name: "true orphan closes in favour of the bound twin",
			issues: []issue{
				openIssue(814, titleA, bodyA, at(0)),
				openIssue(816, titleA, bodyA, at(time.Second)),
			},
			beads: []bead{boundTo("gqlc-u91z", 816)},
			want:  []string{"CLOSE 814->816"},
		},
		{
			name: "title collision with a different body is refused",
			issues: []issue{
				openIssue(814, titleA, bodyA, at(0)),
				openIssue(816, titleA, bodyB, at(time.Second)),
			},
			beads:      []bead{boundTo("gqlc-u91z", 816)},
			want:       []string{"REFUSE 814"},
			wantReason: "none of which matches on both body and creation time",
		},
		{
			name: "title and body collision outside the window is refused",
			issues: []issue{
				openIssue(814, titleA, bodyA, at(0)),
				openIssue(816, titleA, bodyA, at(61*time.Second)),
			},
			beads:      []bead{boundTo("gqlc-u91z", 816)},
			want:       []string{"REFUSE 814"},
			wantReason: "none of which matches on both body and creation time",
		},
		{
			// Direction must not matter: the race does not fix which run's
			// issue is created first (see the package comment).
			name: "an orphan created after its canonical still closes",
			issues: []issue{
				openIssue(816, titleA, bodyA, at(0)),
				openIssue(814, titleA, bodyA, at(14*time.Second)),
			},
			beads: []bead{boundTo("gqlc-u91z", 816)},
			want:  []string{"CLOSE 814->816"},
		},
		{
			name: "exactly at the window closes",
			issues: []issue{
				openIssue(814, titleA, bodyA, at(0)),
				openIssue(816, titleA, bodyA, at(60*time.Second)),
			},
			beads: []bead{boundTo("gqlc-u91z", 816)},
			want:  []string{"CLOSE 814->816"},
		},
		{
			name: "one nanosecond past the window is refused",
			issues: []issue{
				{Number: 814, Title: titleA, Body: bodyA, CreatedAt: "2026-08-17T05:17:10Z", State: "OPEN"},
				{Number: 816, Title: titleA, Body: bodyA, CreatedAt: "2026-08-17T05:18:10.000000001Z", State: "OPEN"},
			},
			beads:      []bead{boundTo("gqlc-u91z", 816)},
			want:       []string{"REFUSE 814"},
			wantReason: "none of which matches on both body and creation time",
		},
		{
			// The absolute rule: a bead names it, so it is out of scope even
			// though it is a byte-for-byte twin of another bound issue.
			name: "an issue a bead points at is never a candidate",
			issues: []issue{
				openIssue(814, titleA, bodyA, at(0)),
				openIssue(816, titleA, bodyA, at(time.Second)),
			},
			beads: []bead{boundTo("gqlc-u91z", 816), boundTo("gqlc-other", 814)},
			want:  nil,
		},
		{
			// Idempotence. Same corpus as the closing row with the orphan's
			// state moved on, which is what the first run leaves behind.
			name: "an already-closed orphan plans nothing",
			issues: []issue{
				closedIssue(814, titleA, bodyA, at(0)),
				openIssue(816, titleA, bodyA, at(time.Second)),
			},
			beads: []bead{boundTo("gqlc-u91z", 816)},
			want:  nil,
		},
		{
			name: "two equally matching bound issues are refused as ambiguous",
			issues: []issue{
				openIssue(814, titleA, bodyA, at(0)),
				openIssue(816, titleA, bodyA, at(time.Second)),
				openIssue(818, titleA, bodyA, at(2*time.Second)),
			},
			beads:      []bead{boundTo("gqlc-u91z", 816), boundTo("gqlc-f1yf", 818)},
			want:       []string{"REFUSE 814"},
			wantReason: "nothing here says which mirror is canonical",
		},
		{
			name: "an unbound open issue with no title twin is not reported",
			issues: []issue{
				openIssue(814, titleA, bodyA, at(0)),
				openIssue(816, titleB, bodyA, at(time.Second)),
			},
			beads: []bead{boundTo("gqlc-u91z", 816)},
			want:  nil,
		},
		{
			// Two unbound duplicates give no reason to prefer either, so
			// neither is closed and neither is reported.
			name: "an unbound twin is not a canonical",
			issues: []issue{
				openIssue(814, titleA, bodyA, at(0)),
				openIssue(816, titleA, bodyA, at(time.Second)),
			},
			beads: []bead{boundTo("gqlc-u91z", 820)},
			want:  nil,
		},
		{
			// 4 of the 18 known pairs have a canonical that is already closed.
			name: "a closed canonical still receives its orphan",
			issues: []issue{
				openIssue(452, titleA, bodyA, at(0)),
				closedIssue(453, titleA, bodyA, at(time.Second)),
			},
			beads: []bead{boundTo("gqlc-fer", 453)},
			want:  []string{"CLOSE 452->453"},
		},
		{
			// Titles are compared as bytes. A trailing space is a different
			// title, and a rule that trimmed would start deciding which
			// whitespace is meaningful.
			name: "a title differing only by trailing space is not a collision",
			issues: []issue{
				openIssue(814, titleA+" ", bodyA, at(0)),
				openIssue(816, titleA, bodyA, at(time.Second)),
			},
			beads: []bead{boundTo("gqlc-u91z", 816)},
			want:  nil,
		},
		{
			// Bodies are compared as bytes too: a trailing newline is a
			// difference, and an issue GitHub or a human re-saved is not
			// evidence of the same mint.
			name: "a body differing only by a trailing newline is refused",
			issues: []issue{
				openIssue(814, titleA, bodyA+"\n", at(0)),
				openIssue(816, titleA, bodyA, at(time.Second)),
			},
			beads:      []bead{boundTo("gqlc-u91z", 816)},
			want:       []string{"REFUSE 814"},
			wantReason: "none of which matches on both body and creation time",
		},
		{
			name: "a lowercase state from the API is still open",
			issues: []issue{
				{Number: 814, Title: titleA, Body: bodyA, CreatedAt: at(0), State: "open"},
				openIssue(816, titleA, bodyA, at(time.Second)),
			},
			beads: []bead{boundTo("gqlc-u91z", 816)},
			want:  []string{"CLOSE 814->816"},
		},
		{
			// Neither OPEN nor CLOSED: the safe reading is "not a candidate".
			name: "an unrecognised state is not treated as open",
			issues: []issue{
				{Number: 814, Title: titleA, Body: bodyA, CreatedAt: at(0), State: "DRAFT"},
				openIssue(816, titleA, bodyA, at(time.Second)),
			},
			beads: []bead{boundTo("gqlc-u91z", 816)},
			want:  nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			issues := append(append([]issue{}, tc.issues...), fillerIssue)
			beads := append(append([]bead{}, tc.beads...), fillerBead)
			got := planOrFatal(t, issues, beads)

			var lines []string
			for _, f := range got {
				if f.Verdict == verdictClose {
					lines = append(lines, fmt.Sprintf("CLOSE %d->%d", f.Orphan, f.Canonical))
				} else {
					lines = append(lines, fmt.Sprintf("REFUSE %d", f.Orphan))
				}
			}
			if strings.Join(lines, "; ") != strings.Join(tc.want, "; ") {
				t.Fatalf("plan findings = %v, want %v", lines, tc.want)
			}
			if tc.wantReason != "" {
				if len(got) != 1 {
					t.Fatalf("wantReason set but got %d findings", len(got))
				}
				if !strings.Contains(got[0].Reason, tc.wantReason) {
					t.Errorf("reason = %q, want it to contain %q", got[0].Reason, tc.wantReason)
				}
			}
			for _, f := range got {
				if f.Verdict == verdictClose && len(f.Beads) == 0 {
					t.Errorf("#%d closes in favour of #%d but names no bead", f.Orphan, f.Canonical)
				}
			}
		})
	}
}

func TestPlanRefusesInputsItCannotStandOn(t *testing.T) {
	good := []issue{openIssue(814, titleA, bodyA, at(0)), openIssue(816, titleA, bodyA, at(time.Second))}
	goodBeads := []bead{boundTo("gqlc-u91z", 816)}

	tests := []struct {
		name    string
		issues  []issue
		beads   []bead
		window  time.Duration
		wantMsg string
	}{
		{
			name:    "empty issue listing",
			issues:  nil,
			beads:   goodBeads,
			window:  defaultWindow,
			wantMsg: "issue listing came back empty",
		},
		{
			name:    "empty bead ledger",
			issues:  good,
			beads:   nil,
			window:  defaultWindow,
			wantMsg: "bead ledger came back empty",
		},
		{
			name:    "negative window",
			issues:  good,
			beads:   goodBeads,
			window:  -time.Second,
			wantMsg: "negative window",
		},
		{
			// The ceiling, one nanosecond over it. `good` is a pair the window
			// does not separate at any legal value, so what fails here is the
			// window and not the pair.
			name:    "window one nanosecond above the ceiling",
			issues:  good,
			beads:   goodBeads,
			window:  defaultWindow + time.Nanosecond,
			wantMsg: fmt.Sprintf("above the %s ceiling", defaultWindow),
		},
		{
			// The one-liner bd gqlc-mb8v's round-1 review composed out of the
			// two findings: `just gh-orphans -close -window 8760h`, through a
			// recipe whose `just --list` line said it mutated nothing. Its
			// first half is refused in the justfile (see justfile_test.go) and
			// its second half here.
			name:    "a one-year window",
			issues:  good,
			beads:   goodBeads,
			window:  8760 * time.Hour,
			wantMsg: "can turn a refusal into a close",
		},
		{
			name:    "duplicate issue number",
			issues:  append(append([]issue{}, good...), openIssue(814, titleA, bodyA, at(0))),
			beads:   goodBeads,
			window:  defaultWindow,
			wantMsg: "holds #814 twice",
		},
		{
			name:    "unparseable createdAt",
			issues:  []issue{openIssue(814, titleA, bodyA, "17 August"), good[1]},
			beads:   goodBeads,
			window:  defaultWindow,
			wantMsg: "not RFC3339",
		},
		{
			name:    "empty createdAt",
			issues:  []issue{openIssue(814, titleA, bodyA, ""), good[1]},
			beads:   goodBeads,
			window:  defaultWindow,
			wantMsg: "not RFC3339",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := plan(tc.issues, tc.beads, tc.window)
			if err == nil {
				t.Fatalf("plan returned no error; findings = %v", got)
			}
			if got != nil {
				t.Errorf("plan returned %d finding(s) alongside its error; a refused run must plan nothing", len(got))
			}
			if !strings.Contains(err.Error(), tc.wantMsg) {
				t.Errorf("error = %q, want it to contain %q", err, tc.wantMsg)
			}
		})
	}
}

// TestANarrowerWindowTurnsAnAmbiguousRefusalIntoAClose is a disclosure rather
// than a guard. plan() now refuses a window above the default, and the obvious
// next sentence — "so no -window value can turn a refusal into a close" — is
// false in the direction the ceiling leaves open. An ambiguity refusal says the
// tool will not pick between two mirrors; narrowing the window until only one
// of them is adjacent picks for it.
//
// It is pinned here so the paragraph in the package comment that says so is
// checkable against a row instead of believed, and so that removing the
// ceiling's counterpart claim without removing this behaviour is noisy.
func TestANarrowerWindowTurnsAnAmbiguousRefusalIntoAClose(t *testing.T) {
	issues := []issue{
		openIssue(814, titleA, bodyA, at(0)),
		openIssue(816, titleA, bodyA, at(time.Second)),
		openIssue(818, titleA, bodyA, at(30*time.Second)),
		fillerIssue,
	}
	beads := []bead{boundTo("gqlc-u91z", 816), boundTo("gqlc-f1yf", 818), fillerBead}

	wide := planOrFatal(t, issues, beads)
	if len(wide) != 1 || wide[0].Verdict != verdictRefuse {
		t.Fatalf("at the default window: findings = %+v, want one REFUSE", wide)
	}
	if !strings.Contains(wide[0].Reason, "nothing here says which mirror is canonical") {
		t.Fatalf("at the default window: reason = %q, want the ambiguity refusal", wide[0].Reason)
	}

	narrow, err := plan(issues, beads, 5*time.Second)
	if err != nil {
		t.Fatalf("at a 5s window: plan: %v", err)
	}
	if len(narrow) != 1 || narrow[0].Verdict != verdictClose || narrow[0].Canonical != 816 {
		t.Fatalf("at a 5s window: findings = %+v, want CLOSE 814->816", narrow)
	}
}

// TestPlanOrdersFindingsByIssueNumber pins the sort. `gh issue list` does not
// promise an order, so without it two runs over the same corpus print the same
// decisions in different sequences, and the diff between two reports stops
// meaning anything.
func TestPlanOrdersFindingsByIssueNumber(t *testing.T) {
	// Deliberately handed to plan in descending order.
	issues := []issue{
		openIssue(897, titleB, bodyB, at(0)),
		openIssue(900, titleB, bodyB, at(time.Second)),
		openIssue(814, titleA, bodyA, at(0)),
		openIssue(816, titleA, bodyA, at(time.Second)),
	}
	beads := []bead{boundTo("gqlc-komy", 900), boundTo("gqlc-u91z", 816)}

	got := planOrFatal(t, issues, beads)
	var order []int
	for _, f := range got {
		order = append(order, f.Orphan)
	}
	if len(order) != 2 || order[0] != 814 || order[1] != 897 {
		t.Errorf("finding order = %v, want [814 897]", order)
	}
}

func TestBoundIssuesReadsEveryClaimOnANumber(t *testing.T) {
	beads := []bead{
		boundTo("gqlc-b", 816),
		boundTo("gqlc-a", 816),
		{ID: "gqlc-none", ExternalRef: ""},
		{ID: "gqlc-notanissue", ExternalRef: "https://github.com/areqag/gqlc/pull/1037"},
		// A ref naming another repository still marks the local number as
		// spoken for; that can only protect an issue from being closed.
		{ID: "gqlc-elsewhere", ExternalRef: "https://github.com/other/repo/issues/452"},
	}
	got := boundIssues(beads)

	if want := []string{"gqlc-a", "gqlc-b"}; strings.Join(got[816], ",") != strings.Join(want, ",") {
		t.Errorf("bound[816] = %v, want %v (sorted, both claimants kept)", got[816], want)
	}
	if len(got[452]) != 1 || got[452][0] != "gqlc-elsewhere" {
		t.Errorf("bound[452] = %v, want a cross-repository ref to still bind #452", got[452])
	}
	if len(got) != 2 {
		t.Errorf("bound has %d entries (%v), want 2 — a pull URL and an empty ref bind nothing", len(got), got)
	}
}

// recordingCloser is the write leg. failIfCalled is what makes a dry-run test a
// test: a run that mutates nothing because it planned nothing would otherwise
// pass identically.
type recordingCloser struct {
	t            *testing.T
	failIfCalled bool
	calls        []int
	comments     map[int]string
	fail         map[int]error
}

func newCloser(t *testing.T) *recordingCloser {
	t.Helper()
	return &recordingCloser{t: t, comments: map[int]string{}, fail: map[int]error{}}
}

func (r *recordingCloser) closeIssue(_ context.Context, number int, comment string) error {
	if r.failIfCalled {
		r.t.Errorf("closeIssue(#%d) was called on a run that must mutate nothing", number)
	}
	r.calls = append(r.calls, number)
	r.comments[number] = comment
	return r.fail[number]
}

func fixedSource(issues []issue, beads []bead, closer *recordingCloser) source {
	return source{
		issues:     func(context.Context, int) ([]issue, error) { return issues, nil },
		beads:      func(context.Context) ([]bead, error) { return beads, nil },
		closeIssue: closer.closeIssue,
	}
}

func TestRunIsADryRunUnlessToldOtherwise(t *testing.T) {
	issues := []issue{openIssue(814, titleA, bodyA, at(0)), openIssue(816, titleA, bodyA, at(time.Second))}
	beads := []bead{boundTo("gqlc-u91z", 816)}

	closer := newCloser(t)
	closer.failIfCalled = true
	var out strings.Builder
	if err := run(context.Background(), &out, config{act: false, window: defaultWindow, limit: defaultLimit}, fixedSource(issues, beads, closer)); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(closer.calls) != 0 {
		t.Fatalf("dry run closed %v", closer.calls)
	}
	for _, want := range []string{"DRY RUN", "1 issue(s) would be closed", "CLOSE  #814 -> #816"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("dry-run report missing %q; got:\n%s", want, out.String())
		}
	}
}

func TestRunClosesOnlyWithTheOptIn(t *testing.T) {
	issues := []issue{openIssue(814, titleA, bodyA, at(0)), openIssue(816, titleA, bodyA, at(time.Second))}
	beads := []bead{boundTo("gqlc-u91z", 816)}

	closer := newCloser(t)
	var out strings.Builder
	if err := run(context.Background(), &out, config{act: true, window: defaultWindow, limit: defaultLimit}, fixedSource(issues, beads, closer)); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(closer.calls) != 1 || closer.calls[0] != 814 {
		t.Fatalf("closed %v, want [814]", closer.calls)
	}
	if !strings.Contains(out.String(), "closed 1 of 1 planned issue(s)") {
		t.Errorf("summary missing; got:\n%s", out.String())
	}
	comment := closer.comments[814]
	for _, want := range []string{"#816", "gqlc-u91z", "external_ref", "ghorphan"} {
		if !strings.Contains(comment, want) {
			t.Errorf("close comment missing %q; got:\n%s", want, comment)
		}
	}
	if strings.Contains(comment, "#814") {
		t.Errorf("close comment names the issue it is closing, which reads as a self-reference:\n%s", comment)
	}
}

func TestRunSurfacesAReadFailureRatherThanPlanningOnPartialInput(t *testing.T) {
	closer := newCloser(t)
	closer.failIfCalled = true
	boom := errors.New("gh: rate limited")

	src := fixedSource(nil, nil, closer)
	src.issues = func(context.Context, int) ([]issue, error) { return nil, boom }
	var out strings.Builder
	err := run(context.Background(), &out, config{act: true, window: defaultWindow, limit: defaultLimit}, src)
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want it to wrap %v", err, boom)
	}

	src = fixedSource([]issue{openIssue(814, titleA, bodyA, at(0))}, nil, closer)
	src.beads = func(context.Context) ([]bead, error) { return nil, boom }
	err = run(context.Background(), &out, config{act: true, window: defaultWindow, limit: defaultLimit}, src)
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want it to wrap %v", err, boom)
	}

	// A refusal from plan() has to abort the run too, not report an empty plan
	// and then act on it. Both reads succeed here; the ledger is what is empty.
	src = fixedSource([]issue{openIssue(814, titleA, bodyA, at(0))}, nil, closer)
	err = run(context.Background(), &out, config{act: true, window: defaultWindow, limit: defaultLimit}, src)
	if err == nil || !strings.Contains(err.Error(), "bead ledger came back empty") {
		t.Fatalf("err = %v, want plan's refusal surfaced", err)
	}
}

// brokenWriter fails every write. A report that reached nobody must not read as
// a run that had nothing to report.
type brokenWriter struct{ writes int }

func (b *brokenWriter) Write([]byte) (int, error) {
	b.writes++
	return 0, errors.New("no such device")
}

func TestRunSurfacesAReportThatWentNowhere(t *testing.T) {
	issues := []issue{openIssue(814, titleA, bodyA, at(0)), openIssue(816, titleA, bodyA, at(time.Second))}
	beads := []bead{boundTo("gqlc-u91z", 816)}

	closer := newCloser(t)
	closer.failIfCalled = true
	w := &brokenWriter{}
	err := run(context.Background(), w, config{act: false, window: defaultWindow, limit: defaultLimit}, fixedSource(issues, beads, closer))
	if err == nil || !strings.Contains(err.Error(), "no such device") {
		t.Fatalf("err = %v, want the write failure surfaced", err)
	}
	// The first failure sticks: later lines are dropped rather than each
	// re-attempting and each producing its own error.
	if w.writes != 1 {
		t.Errorf("writes = %d, want 1 — the reporter kept writing after its first failure", w.writes)
	}
}

func TestApplyRefusesAPlanThatDisagreesWithTheLedger(t *testing.T) {
	// Hand-built: plan() cannot produce this, and that is the point — the
	// second bound check exists for the case where plan() is wrong.
	findings := []finding{
		{Orphan: 814, Verdict: verdictClose, Canonical: 816, Beads: []string{"gqlc-u91z"}},
		{Orphan: 820, Verdict: verdictClose, Canonical: 821, Beads: []string{"gqlc-rxq9"}},
	}
	bound := map[int][]string{814: {"gqlc-someone"}}

	closer := newCloser(t)
	closer.failIfCalled = true
	var out strings.Builder
	closed, err := apply(context.Background(), &reporter{w: &out}, findings, bound, closer.closeIssue)
	if err == nil {
		t.Fatal("apply closed a bead-bound issue")
	}
	if closed != 0 {
		t.Errorf("closed = %d, want 0", closed)
	}
	if !strings.Contains(err.Error(), "gqlc-someone") {
		t.Errorf("error = %q, want it to name the claiming bead", err)
	}
	// The abort has to stop the run, not skip the one entry: the rest of the
	// plan came out of the same logic.
	if strings.Contains(err.Error(), "#820") || len(closer.calls) != 0 {
		t.Errorf("apply continued past the disagreement: calls=%v err=%v", closer.calls, err)
	}
}

func TestApplyRefusesASelfDuplicate(t *testing.T) {
	findings := []finding{{Orphan: 814, Verdict: verdictClose, Canonical: 814, Beads: []string{"gqlc-u91z"}}}
	closer := newCloser(t)
	closer.failIfCalled = true
	var out strings.Builder
	closed, err := apply(context.Background(), &reporter{w: &out}, findings, map[int][]string{}, closer.closeIssue)
	if err == nil || !strings.Contains(err.Error(), "duplicate of itself") {
		t.Fatalf("err = %v, want a self-duplicate refusal", err)
	}
	if closed != 0 {
		t.Errorf("closed = %d, want 0", closed)
	}
}

func TestApplySkipsRefusalsAndCarriesOnPastAFailedClose(t *testing.T) {
	findings := []finding{
		{Orphan: 814, Verdict: verdictClose, Canonical: 816, Beads: []string{"gqlc-u91z"}},
		{Orphan: 815, Verdict: verdictRefuse, Reason: "bodies differ"},
		{Orphan: 817, Verdict: verdictClose, Canonical: 819, Beads: []string{"gqlc-65sl"}},
	}
	closer := newCloser(t)
	closer.fail[814] = errors.New("gh: 403")

	var out strings.Builder
	closed, err := apply(context.Background(), &reporter{w: &out}, findings, map[int][]string{}, closer.closeIssue)
	if closed != 1 {
		t.Errorf("closed = %d, want 1", closed)
	}
	if err == nil || !strings.Contains(err.Error(), "close #814") {
		t.Fatalf("err = %v, want the failed close reported", err)
	}
	if len(closer.calls) != 2 || closer.calls[0] != 814 || closer.calls[1] != 817 {
		t.Errorf("calls = %v, want [814 817] — the refusal is skipped, the failure is not fatal", closer.calls)
	}
}

func TestCheckTruncationRefusesAFullPage(t *testing.T) {
	if err := checkTruncation(999, 1000); err != nil {
		t.Errorf("a short read is a set: %v", err)
	}
	err := checkTruncation(1000, 1000)
	if err == nil {
		t.Fatal("a listing of exactly the cap was accepted as a set")
	}
	if !strings.Contains(err.Error(), "page rather than a set") {
		t.Errorf("error = %q", err)
	}
	if err := checkTruncation(1001, 1000); err == nil {
		t.Error("a listing over the cap was accepted")
	}
}

func TestBodyRelationNamesTheShapeOfTheDifference(t *testing.T) {
	tests := []struct {
		name       string
		a, b       string
		wantSubstr string
	}{
		{"identical", "abc", "abc", "byte-identical"},
		{"a is a prefix of b", "abc", "abcdef", "#1's is a byte-exact prefix of #2's, which carries 3 more byte(s)"},
		{"b is a prefix of a", "abcdef", "abc", "#2's is a byte-exact prefix of #1's, which carries 3 more byte(s)"},
		{"divergent", "abcXef", "abcdef", "differs, first at byte 3"},
		{"divergent at zero", "x", "y", "differs, first at byte 0"},
		{"empty against non-empty", "", "abc", "#1's is a byte-exact prefix of #2's, which carries 3 more byte(s)"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := bodyRelation(1, tc.a, 2, tc.b); got != tc.wantSubstr {
				t.Errorf("bodyRelation = %q, want %q", got, tc.wantSubstr)
			}
		})
	}
}

// TestCandidateEvidenceCallsAPairAMatchOnlyOnBothHalves pins the word the
// report leads each evidence line with. It is the only part of the line a
// reader skims, so a line that says "match" under a REFUSE — or the reverse —
// is worse than no line: it contradicts the verdict three characters in.
func TestCandidateEvidenceCallsAPairAMatchOnlyOnBothHalves(t *testing.T) {
	o := openIssue(814, titleA, bodyA, at(0))
	c := openIssue(816, titleA, bodyA, at(time.Second))
	tests := []struct {
		bodyOK, adjacent bool
		want             string
	}{
		{true, true, "match"},
		{true, false, "no match"},
		{false, true, "no match"},
		{false, false, "no match"},
	}
	for _, tc := range tests {
		t.Run(fmt.Sprintf("body=%v adjacent=%v", tc.bodyOK, tc.adjacent), func(t *testing.T) {
			got := candidateEvidence(o, c, tc.bodyOK, tc.adjacent, time.Second)
			lead, _, _ := strings.Cut(strings.TrimPrefix(got, "#814 vs #816: "), " —")
			if lead != tc.want {
				t.Errorf("evidence leads with %q, want %q; full line: %s", lead, tc.want, got)
			}
			wantWindow := "within the window"
			if !tc.adjacent {
				wantWindow = "outside the window"
			}
			if !strings.Contains(got, wantWindow) {
				t.Errorf("evidence does not say %q: %s", wantWindow, got)
			}
		})
	}
}

// TestRenderPrintsTheEvidenceUnderEveryFinding pins the half of the report that
// is not the verdict. A CLOSE with its evidence dropped is a decision nobody can
// check, and a REFUSE with its evidence dropped hands the operator a number and
// nothing to do with it.
func TestRenderPrintsTheEvidenceUnderEveryFinding(t *testing.T) {
	got := render([]finding{
		{Orphan: 814, Verdict: verdictClose, Canonical: 816, Beads: []string{"gqlc-u91z"}, Evidence: []string{"e-close-1"}},
		{Orphan: 895, Verdict: verdictRefuse, Reason: "bodies differ", Evidence: []string{"e-refuse-1", "e-refuse-2"}},
	})
	want := "CLOSE  #814 -> #816 (bead gqlc-u91z)\n" +
		"         e-close-1\n" +
		"REFUSE #895: bodies differ\n" +
		"         e-refuse-1\n" +
		"         e-refuse-2\n"
	if got != want {
		t.Errorf("render =\n%s\nwant\n%s", got, want)
	}
}

func TestDigestIsAnMD5Prefix(t *testing.T) {
	// `printf 'abc' | md5sum` -> 900150983cd24fb0d6963f7d28e17f72
	if got := digest("abc"); got != "90015098" {
		t.Errorf("digest(abc) = %q, want 90015098", got)
	}
	if digest("abc") == digest("abd") {
		t.Error("digest does not distinguish two different bodies")
	}
}

// --- the real corpus -------------------------------------------------------

func loadCorpus(t *testing.T) ([]issue, []bead) {
	t.Helper()
	var issues []issue
	readJSON(t, "testdata/issues.json", &issues)
	var beads []bead
	readJSON(t, "testdata/beads.json", &beads)
	if len(issues) != 36 || len(beads) != 18 {
		t.Fatalf("fixture holds %d issue(s) and %d bead(s), want 36 and 18 — the 18 pairs bd gqlc-mb8v names", len(issues), len(beads))
	}
	return issues, beads
}

func readJSON(t *testing.T, path string, into any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if err := json.Unmarshal(data, into); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
}

// TestPlanOverTheRealEighteenPairs is the claim this tool is judged on: over the
// corpus as it stood on 2026-08-18, it closes the 16 pairs bd gqlc-mmej measured
// as byte-identical and refuses the two it flags as not — 505/503 (a hand-filed
// issue plus a minted mirror, bodies differing by a `Tracked as bd` footer) and
// 895/899 (a genuine race pair whose canonical was edited afterwards, so 895's
// body is a byte-exact prefix of 899's). Both refusals are stated as refusals
// with the evidence attached, not guessed at.
func TestPlanOverTheRealEighteenPairs(t *testing.T) {
	issues, beads := loadCorpus(t)
	findings := planOrFatal(t, issues, beads)

	wantClose := map[int]int{
		452: 453, 814: 816, 815: 818, 817: 819, 820: 821, 875: 876,
		877: 878, 879: 880, 887: 890, 888: 892, 889: 894, 891: 896,
		893: 898, 897: 900, 928: 930, 929: 931,
	}
	wantRefuse := map[int]bool{505: true, 895: true}

	if len(findings) != len(wantClose)+len(wantRefuse) {
		t.Fatalf("planned %d finding(s), want %d", len(findings), len(wantClose)+len(wantRefuse))
	}
	closes := 0
	for _, f := range findings {
		switch {
		case wantRefuse[f.Orphan]:
			if f.Verdict != verdictRefuse {
				t.Errorf("#%d: verdict %s, want REFUSE — its body is not byte-identical to its twin's", f.Orphan, f.Verdict)
			}
			if len(f.Evidence) == 0 {
				t.Errorf("#%d refused with no evidence for a human to act on", f.Orphan)
			}
		case wantClose[f.Orphan] != 0:
			closes++
			if f.Verdict != verdictClose {
				t.Errorf("#%d: verdict %s, want CLOSE", f.Orphan, f.Verdict)
			}
			if f.Canonical != wantClose[f.Orphan] {
				t.Errorf("#%d -> #%d, want #%d", f.Orphan, f.Canonical, wantClose[f.Orphan])
			}
		default:
			t.Errorf("#%d: unexpected finding %+v", f.Orphan, f)
		}
	}
	if closes != 16 {
		t.Errorf("planned %d close(s) over the real corpus, want 16", closes)
	}

	// The two refusals are the tool declining to decide, so the report has to
	// carry the shape of each difference — that is the whole handoff to the
	// human.
	byOrphan := map[int]finding{}
	for _, f := range findings {
		byOrphan[f.Orphan] = f
	}
	if ev := strings.Join(byOrphan[505].Evidence, " "); !strings.Contains(ev, "prefix") {
		t.Errorf("#505 evidence does not name the body relation: %s", ev)
	}
	if ev := strings.Join(byOrphan[895].Evidence, " "); !strings.Contains(ev, "#895's is a byte-exact prefix of #899's") {
		t.Errorf("#895 evidence does not name the body relation: %s", ev)
	}
}

// TestDryRunOverTheRealCorpusMutatesNothingAndSaysWhatItWould is the shape an
// operator actually invokes: no flag, no writes. `go test -run
// '^TestDryRunOverTheRealCorpusMutatesNothingAndSaysWhatItWould$' -v
// ./internal/tools/ghorphan` prints the report the live corpus would produce.
func TestDryRunOverTheRealCorpusMutatesNothingAndSaysWhatItWould(t *testing.T) {
	issues, beads := loadCorpus(t)
	closer := newCloser(t)
	closer.failIfCalled = true

	var out strings.Builder
	if err := run(context.Background(), &out, config{act: false, window: defaultWindow, limit: defaultLimit}, fixedSource(issues, beads, closer)); err != nil {
		t.Fatalf("run: %v", err)
	}
	t.Log("\n" + out.String())

	if len(closer.calls) != 0 {
		t.Fatalf("the default invocation closed %v", closer.calls)
	}
	want := "ghorphan: DRY RUN — nothing was mutated. 16 issue(s) would be closed, 2 refused"
	if !strings.Contains(out.String(), want) {
		t.Errorf("report missing %q; got:\n%s", want, out.String())
	}
	if n := strings.Count(out.String(), "\nCLOSE  #"); n != 15 {
		// 16 CLOSE lines, the first of which has no preceding newline.
		t.Errorf("report holds %d non-leading CLOSE line(s), want 15", n)
	}
}

// TestTheDryRunSummaryMakesNoClaimAboutWhatAFlagCanDo holds the summary line to
// saying what happened and not what could have. It carried "(a refusal needs a
// human, not a wider flag)" until bd gqlc-mb8v's round-1 review, which is a
// claim about the flag surface printed underneath a report the flags had
// already shaped — and false in both directions, per plan()'s ceiling comment
// and TestANarrowerWindowTurnsAnAmbiguousRefusalIntoAClose.
//
// The word is what is looked for, not the sentence: a rephrasing of the same
// claim is what this is here to catch. It looks at the summary line alone, not
// the whole report — the CLOSE lines carry bead ids and a four-character bd
// suffix can spell anything, so searching them for a word would be a test that
// fails on a bead name.
func TestTheDryRunSummaryMakesNoClaimAboutWhatAFlagCanDo(t *testing.T) {
	issues, beads := loadCorpus(t)
	closer := newCloser(t)
	closer.failIfCalled = true

	var out strings.Builder
	if err := run(context.Background(), &out, config{act: false, window: defaultWindow, limit: defaultLimit}, fixedSource(issues, beads, closer)); err != nil {
		t.Fatalf("run: %v", err)
	}

	var summary string
	for _, line := range strings.Split(out.String(), "\n") {
		if strings.HasPrefix(line, "ghorphan: DRY RUN") {
			summary = line
		}
	}
	if summary == "" {
		t.Fatalf("no summary line in the report; this test would pass on silence:\n%s", out.String())
	}
	if strings.Contains(summary, "flag") {
		t.Errorf("the dry-run summary %q talks about a flag. The operator reads this "+
			"line while deciding what to run next, so a sentence here about what a "+
			"flag can or cannot do has to hold for both -window directions and for "+
			"-limit; say nothing instead.", summary)
	}
}

// TestReconcilingTheRealCorpusIsIdempotent runs the tool against the fixture
// with -close, applies the closes to the corpus the way GitHub would, and runs
// it again. The second run must plan nothing at all.
func TestReconcilingTheRealCorpusIsIdempotent(t *testing.T) {
	issues, beads := loadCorpus(t)

	closer := newCloser(t)
	var out strings.Builder
	if err := run(context.Background(), &out, config{act: true, window: defaultWindow, limit: defaultLimit}, fixedSource(issues, beads, closer)); err != nil {
		t.Fatalf("first run: %v", err)
	}
	if len(closer.calls) != 16 {
		t.Fatalf("first run closed %d issue(s), want 16", len(closer.calls))
	}

	closed := map[int]bool{}
	for _, n := range closer.calls {
		closed[n] = true
	}
	after := make([]issue, 0, len(issues))
	for _, i := range issues {
		if closed[i.Number] {
			i.State = "CLOSED"
		}
		after = append(after, i)
	}

	second := newCloser(t)
	second.failIfCalled = true
	var out2 strings.Builder
	if err := run(context.Background(), &out2, config{act: true, window: defaultWindow, limit: defaultLimit}, fixedSource(after, beads, second)); err != nil {
		t.Fatalf("second run: %v", err)
	}
	if len(second.calls) != 0 {
		t.Fatalf("second run closed %v; reconciling twice is not a no-op", second.calls)
	}
	// The two refusals stay refusals forever — that is the residue a human owns.
	if !strings.Contains(out2.String(), "REFUSE #505") || !strings.Contains(out2.String(), "REFUSE #895") {
		t.Errorf("second run stopped reporting the residue:\n%s", out2.String())
	}
}

// TestNoBoundIssueInTheRealCorpusIsEverPlanned is the absolute rule measured
// over real data rather than a constructed row: 18 of the 36 issues are named
// by a bead, and none of them may appear as an orphan.
func TestNoBoundIssueInTheRealCorpusIsEverPlanned(t *testing.T) {
	issues, beads := loadCorpus(t)
	bound := boundIssues(beads)
	if len(bound) != 18 {
		t.Fatalf("the fixture binds %d issue(s), want 18", len(bound))
	}
	for _, f := range planOrFatal(t, issues, beads) {
		if ids := bound[f.Orphan]; len(ids) > 0 {
			t.Errorf("#%d is named by bead(s) %v and was still planned as an orphan", f.Orphan, ids)
		}
	}
}
