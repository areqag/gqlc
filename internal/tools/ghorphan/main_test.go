package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"regexp"
	"slices"
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
	got, err := plan(issues, beads, defaultWindow, defaultRepo)
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
			got, err := plan(tc.issues, tc.beads, tc.window, defaultRepo)
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

// TestANarrowerWindowCannotTurnAnAmbiguousRefusalIntoAClose is bd gqlc-fzb2's
// reproducer, inverted. Until the ambiguity guard in plan() this test existed
// under the name TestANarrowerWindowTurnsAnAmbiguousRefusalIntoAClose and PINNED
// the flip as reachable, with the same fixture and the same numbers:
//
//	w=60s  REFUSE #814: 2 bound issues match on title, body and creation time
//	w=30s  REFUSE #814: (same)
//	w=10s  CLOSE  #814 -> #816
//	w=5s   CLOSE  #814 -> #816
//
// plan() refuses a window above the default, so widening cannot reach a
// verdict. The obvious next sentence — "so no -window value can turn a refusal
// into a close" — used to be false in the direction the ceiling left open: an
// ambiguity refusal says the tool will not pick between two mirrors, and
// narrowing the window until only one of them is adjacent picked for it. A
// narrower window carries no information about which mirror is canonical; it
// removes information. So the sentence is now made true rather than deleted,
// and this row is what makes it checkable instead of believed.
//
// The 5s and 10s rows are the two the bead measured as CLOSE. 30s is a
// narrowing that was already a refusal on this fixture and stays one, so a
// guard firing on every narrowing rather than on a collapsed match set would
// not be distinguished by this test alone —
// TestNarrowingStillTurnsACloseIntoARefusal is what distinguishes it.
func TestANarrowerWindowCannotTurnAnAmbiguousRefusalIntoAClose(t *testing.T) {
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

	for _, window := range []time.Duration{30 * time.Second, 10 * time.Second, 5 * time.Second, time.Nanosecond} {
		t.Run(window.String(), func(t *testing.T) {
			narrow, err := plan(issues, beads, window, defaultRepo)
			if err != nil {
				t.Fatalf("plan: %v", err)
			}
			if len(narrow) != 1 {
				t.Fatalf("findings = %+v, want exactly one", narrow)
			}
			if narrow[0].Verdict != verdictRefuse {
				t.Fatalf("verdict = %s (canonical #%d); narrowing -window from %s to %s turned "+
					"an ambiguity refusal into a close, which is the tool inventing a certainty "+
					"the default window declined to claim (bd gqlc-fzb2)",
					narrow[0].Verdict, narrow[0].Canonical, defaultWindow, window)
			}
			if narrow[0].Canonical != 0 || len(narrow[0].Beads) != 0 {
				t.Errorf("a refusal named canonical #%d / beads %v", narrow[0].Canonical, narrow[0].Beads)
			}
		})
	}

	// Assert the REASON, not just the verdict: at 5s only #816 is adjacent, so
	// the plain two-match arm cannot be what fired, and without this the guard
	// is indistinguishable from the refusal it stands in for.
	narrow, err := plan(issues, beads, 5*time.Second, defaultRepo)
	if err != nil {
		t.Fatalf("at a 5s window: plan: %v", err)
	}
	for _, want := range []string{"at the 1m0s default window", "the -window 5s in force leaves only #816"} {
		if !strings.Contains(narrow[0].Reason, want) {
			t.Errorf("at a 5s window: reason = %q, want it to contain %q", narrow[0].Reason, want)
		}
	}
}

// TestNarrowingStillTurnsACloseIntoARefusal is the other side of the guard. It
// must not have been built by making every narrowed run refuse: narrowing is
// still allowed, and the operator asking for a stricter run must still get one
// rather than a blanket refusal. Here the default window plans a CLOSE on a
// single unambiguous candidate; a window below the pair's separation drops it
// to a no-match refusal, and one above it still closes.
func TestNarrowingStillTurnsACloseIntoARefusal(t *testing.T) {
	issues := []issue{
		openIssue(814, titleA, bodyA, at(0)),
		openIssue(816, titleA, bodyA, at(10*time.Second)),
		fillerIssue,
	}
	beads := []bead{boundTo("gqlc-u91z", 816), fillerBead}

	wide := planOrFatal(t, issues, beads)
	if len(wide) != 1 || wide[0].Verdict != verdictClose || wide[0].Canonical != 816 {
		t.Fatalf("at the default window: findings = %+v, want CLOSE 814->816", wide)
	}

	narrow, err := plan(issues, beads, 5*time.Second, defaultRepo)
	if err != nil {
		t.Fatalf("at a 5s window: plan: %v", err)
	}
	if len(narrow) != 1 || narrow[0].Verdict != verdictRefuse {
		t.Fatalf("at a 5s window: findings = %+v, want the close narrowed away to a REFUSE", narrow)
	}
	if !strings.Contains(narrow[0].Reason, "none of which matches on both body and creation time") {
		t.Errorf("at a 5s window: reason = %q, want the no-match refusal rather than the ambiguity guard", narrow[0].Reason)
	}

	// The row that stops the guard being written as "refuse every narrowed
	// close": 20s is narrower than the default and leaves the single candidate
	// inside the window, so it must still close.
	still, err := plan(issues, beads, 20*time.Second, defaultRepo)
	if err != nil {
		t.Fatalf("at a 20s window: plan: %v", err)
	}
	if len(still) != 1 || still[0].Verdict != verdictClose || still[0].Canonical != 816 {
		t.Fatalf("at a 20s window: findings = %+v, want CLOSE 814->816 — narrowing is still allowed", still)
	}
}

// TestTheWindowHelpSaysNarrowingCannotManufactureAClose pins the -window stanza
// of `-h`. That stanza is the only account of the flag most operators will
// read, and it has been wrong twice: it ended "so this narrows the predicate or
// leaves it alone" until bd gqlc-mb8v's round-2 review, then described a flip
// that bd gqlc-fzb2's guard has since removed. Both times the sentence and the
// behaviour drifted apart silently.
//
// The division of labour is that
// TestANarrowerWindowCannotTurnAnAmbiguousRefusalIntoAClose owns whether the
// sentence is true and this owns whether an operator is shown it. It renders
// through registerFlags rather than comparing a constant to itself, and
// TestMainRegistersNoFlagOfItsOwn is what makes registerFlags the help an
// operator gets — without it, main() could re-inline its own strings and this
// would stay green over a function nothing calls.
//
// This is an exact pin, so it fails on a harmless rewording too. That is
// deliberate — the failure it exists for is a silent one — but it means the
// test cannot tell you a new wording is honest, only that it is new. The
// trailing "(default 1m0s)" is PrintDefaults' own and is pinned with the rest:
// the default is also the ceiling, so an operator reading the stanza is reading
// the largest window they can ask for.
func TestTheWindowHelpSaysNarrowingCannotManufactureAClose(t *testing.T) {
	const want = "maximum creation-time separation between an orphan and its canonical; " +
		"a larger value is refused, so this only ever narrows. Narrowing cannot make the " +
		"tool more decisive than the default window is: a title whose bound mirrors are " +
		"ambiguous at 1m0s stays refused however far this is narrowed, so the only verdict " +
		"this flag can move is a close into a refusal (default 1m0s)"

	fs := flag.NewFlagSet("ghorphan", flag.ContinueOnError)
	registerFlags(fs)
	var help strings.Builder
	fs.SetOutput(&help)
	fs.PrintDefaults()

	// PrintDefaults writes "  -window duration\n    \tusage (default 1m0s)\n".
	// Cutting at the header and taking the line after it needs no not-found
	// branch: a miss leaves got empty, which fails the comparison below with a
	// value the message names. A branch here would be one no passing run can
	// reach, and so one no mutation of it could be caught by.
	_, afterHeader, _ := strings.Cut(help.String(), "  -window duration\n    \t")
	got, _, _ := strings.Cut(afterHeader, "\n")

	if got != want {
		t.Errorf("`-h` describes -window as\n\t%q\nwant\n\t%q\n"+
			"(an empty value means the -window stanza was not in the rendered help at all).\n"+
			"What the flag can and cannot do to a verdict is disclosed here and nowhere "+
			"else an operator reads. If the wording is being changed on purpose, change it here too.", got, want)
	}
}

// TestMainRegistersNoFlagOfItsOwn reads main.go's syntax tree and holds main()
// to calling registerFlags and registering nothing itself. Without it,
// TestTheWindowHelpSaysNarrowingCannotManufactureAClose pins a function that
// main() need not call: re-inlining flag.Duration("window", …, "narrows the predicate")
// in main() leaves both green while `-h` prints the wording round 2 removed.
// Measured — that mutation survived until this test existed.
//
// It matches on method name and argument count, not on types, because
// go/parser carries none: a registration spelled some other way, or reached
// through an alias, is not caught. Reading the AST rather than the source bytes
// is deliberate, so a commented-out registration cannot read as a live one.
func TestMainRegistersNoFlagOfItsOwn(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "main.go", nil, 0)
	if err != nil {
		t.Fatalf("parse main.go: %v", err)
	}

	var mainFn *ast.FuncDecl
	for _, decl := range file.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Recv == nil && fn.Name.Name == "main" {
			mainFn = fn
		}
	}
	if mainFn == nil {
		t.Fatalf("no func main in main.go; this test would pass on silence")
	}

	// flag's registrars all take at least three arguments, so a nullary
	// String() on some other value is not mistaken for one.
	registrar := map[string]bool{
		"Bool": true, "BoolFunc": true, "Duration": true, "Float64": true,
		"Func": true, "Int": true, "Int64": true, "String": true,
		"TextVar": true, "Uint": true, "Uint64": true, "Var": true,
	}
	callsRegisterFlags := false
	var own []string
	ast.Inspect(mainFn, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch fn := call.Fun.(type) {
		case *ast.Ident:
			if fn.Name == "registerFlags" {
				callsRegisterFlags = true
			}
		case *ast.SelectorExpr:
			if registrar[fn.Sel.Name] && len(call.Args) >= 3 {
				own = append(own, fn.Sel.Name)
			}
		}
		return true
	})

	if !callsRegisterFlags {
		t.Errorf("main() does not call registerFlags, so the flag help the tests pin is not the help `-h` prints")
	}
	if len(own) != 0 {
		t.Errorf("main() registers %v itself; every flag goes through registerFlags, which is where "+
			"TestTheWindowHelpSaysNarrowingCannotManufactureAClose can see the usage text", own)
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
		// A repository whose name merely ENDS with the pinned one. The local
		// pattern is anchored at a path separator, so this is foreign.
		{ID: "gqlc-lookalike", ExternalRef: "https://github.com/evil/not-areqag/gqlc/issues/777"},
		// The API spelling of a local ref. Same suffix, so it is local.
		{ID: "gqlc-api", ExternalRef: "https://api.github.com/repos/areqag/gqlc/issues/303"},
	}
	got := boundIssues(beads, defaultRepo)

	if want := []string{"gqlc-a", "gqlc-b"}; strings.Join(got.any[816], ",") != strings.Join(want, ",") {
		t.Errorf("any[816] = %v, want %v (sorted, both claimants kept)", got.any[816], want)
	}
	if want := []string{"gqlc-a", "gqlc-b"}; strings.Join(got.local[816], ",") != strings.Join(want, ",") {
		t.Errorf("local[816] = %v, want %v", got.local[816], want)
	}
	if len(got.any[452]) != 1 || got.any[452][0] != "gqlc-elsewhere" {
		t.Errorf("any[452] = %v, want a cross-repository ref to still bind #452", got.any[452])
	}
	if len(got.any) != 4 {
		t.Errorf("any has %d entries (%v), want 4 — a pull URL and an empty ref bind nothing", len(got.any), got.any)
	}

	// The asymmetry (bd gqlc-hgos). A foreign ref vetoes the local number and
	// must not make it eligible as a canonical: a canonical is what the closing
	// comment points a human at, and its bead ids come straight off local.
	for _, n := range []int{452, 777} {
		if ids := got.local[n]; len(ids) != 0 {
			t.Errorf("local[%d] = %v, want empty — a ref naming another repository must not qualify #%d as a canonical", n, ids, n)
		}
		if len(got.any[n]) == 0 {
			t.Errorf("any[%d] is empty, want the foreign ref to still veto closing #%d", n, n)
		}
	}
	if ids := got.local[303]; len(ids) != 1 || ids[0] != "gqlc-api" {
		t.Errorf("local[303] = %v, want the api.github.com spelling of a local ref to be local", ids)
	}
}

// TestOnlyALocalRefMakesAnIssueACanonical takes the split all the way through
// plan(). #816 is named only by a ref pointing at some other repository's
// issue 816, so the number is spoken for — #814 must not be closed against it,
// and #816 must not itself be planned as an orphan either.
func TestOnlyALocalRefMakesAnIssueACanonical(t *testing.T) {
	issues := []issue{
		openIssue(814, titleA, bodyA, at(0)),
		openIssue(816, titleA, bodyA, at(time.Second)),
		fillerIssue,
	}
	foreign := bead{ID: "gqlc-elsewhere", ExternalRef: "https://github.com/other/repo/issues/816"}

	// The control: with a LOCAL ref on #816 this corpus plans CLOSE 814->816.
	if got := planOrFatal(t, issues, []bead{boundTo("gqlc-u91z", 816), fillerBead}); len(got) != 1 ||
		got[0].Verdict != verdictClose || got[0].Canonical != 816 {
		t.Fatalf("control: findings = %+v, want CLOSE 814->816; without this the case below passes vacuously", got)
	}

	got := planOrFatal(t, issues, []bead{foreign, fillerBead})
	for _, f := range got {
		t.Errorf("planned %+v; #816 is named only by a ref into another repository, so it is "+
			"not a canonical here, and #814 has no bound twin left to be an orphan of (bd gqlc-hgos)", f)
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

func (r *recordingCloser) closeIssue(_ context.Context, _ string, number int, comment string) error {
	if r.failIfCalled {
		r.t.Errorf("closeIssue(#%d) was called on a run that must mutate nothing", number)
	}
	r.calls = append(r.calls, number)
	r.comments[number] = comment
	return r.fail[number]
}

func fixedSource(issues []issue, beads []bead, closer *recordingCloser) source {
	return source{
		issues:     func(context.Context, string, int) ([]issue, error) { return issues, nil },
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
	if err := run(context.Background(), &out, config{act: false, window: defaultWindow, limit: defaultLimit, repo: defaultRepo}, fixedSource(issues, beads, closer)); err != nil {
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
	if err := run(context.Background(), &out, config{act: true, window: defaultWindow, limit: defaultLimit, repo: defaultRepo}, fixedSource(issues, beads, closer)); err != nil {
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
	src.issues = func(context.Context, string, int) ([]issue, error) { return nil, boom }
	var out strings.Builder
	err := run(context.Background(), &out, config{act: true, window: defaultWindow, limit: defaultLimit, repo: defaultRepo}, src)
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want it to wrap %v", err, boom)
	}

	src = fixedSource([]issue{openIssue(814, titleA, bodyA, at(0))}, nil, closer)
	src.beads = func(context.Context) ([]bead, error) { return nil, boom }
	err = run(context.Background(), &out, config{act: true, window: defaultWindow, limit: defaultLimit, repo: defaultRepo}, src)
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want it to wrap %v", err, boom)
	}

	// A refusal from plan() has to abort the run too, not report an empty plan
	// and then act on it. Both reads succeed here; the ledger is what is empty.
	src = fixedSource([]issue{openIssue(814, titleA, bodyA, at(0))}, nil, closer)
	err = run(context.Background(), &out, config{act: true, window: defaultWindow, limit: defaultLimit, repo: defaultRepo}, src)
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
	err := run(context.Background(), w, config{act: false, window: defaultWindow, limit: defaultLimit, repo: defaultRepo}, fixedSource(issues, beads, closer))
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
	closed, err := apply(context.Background(), &reporter{w: &out}, findings, bound, closer.closeIssue, defaultRepo)
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
	closed, err := apply(context.Background(), &reporter{w: &out}, findings, map[int][]string{}, closer.closeIssue, defaultRepo)
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
	closed, err := apply(context.Background(), &reporter{w: &out}, findings, map[int][]string{}, closer.closeIssue, defaultRepo)
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

// ledgerReads is a bead leg that hands out a different snapshot per call, so a
// test can move the ledger between the read plan() sees and the read taken at
// the write boundary. It records how many times it was asked, which is the
// other half of bd gqlc-345j: a run that re-reads and a run that reuses one
// snapshot are indistinguishable from their output alone when the ledger did
// not in fact move.
type ledgerReads struct {
	t         *testing.T
	snapshots [][]bead
	calls     int
}

func (l *ledgerReads) read(context.Context) ([]bead, error) {
	l.calls++
	if l.calls > len(l.snapshots) {
		l.t.Fatalf("the bead ledger was read %d time(s); only %d snapshot(s) were provided, "+
			"so this run reads the ledger more often than the test can account for", l.calls, len(l.snapshots))
	}
	return l.snapshots[l.calls-1], nil
}

// TestTheWriteBoundaryReReadsTheLedger is bd gqlc-345j. Before it, run() built
// one bound map from one `bd list` and handed it to both plan() and apply(), so
// apply()'s re-check was a self-consistency assertion over a value already read
// — it agreed with itself under exactly the drift it existed to catch, and no
// input could make it fire.
//
// The drift modelled here is the concrete hole the bead names: the mint race on
// bd gqlc-mmej takes #814 and #816 for one bead, the first `bd github push`
// writes external_ref=816, this tool snapshots and plans CLOSE #814, and the
// second push then writes external_ref=814 — making #814 the bead's live
// canonical a moment before this run would have closed it.
//
// What is asserted is that the run FAILS, having closed nothing. It is not
// asserted that the race is closed, because it is not: the boundary read is a
// snapshot too. See run()'s comment at the read.
func TestTheWriteBoundaryReReadsTheLedger(t *testing.T) {
	issues := []issue{
		openIssue(814, titleA, bodyA, at(0)),
		openIssue(816, titleA, bodyA, at(time.Second)),
	}
	planned := []bead{boundTo("gqlc-u91z", 816)}
	// The losing mint's write landing after the plan was built.
	drifted := []bead{{ID: "gqlc-u91z", ExternalRef: "https://github.com/areqag/gqlc/issues/814"}}

	t.Run("the orphan gains a claimant between the two reads", func(t *testing.T) {
		closer := newCloser(t)
		closer.failIfCalled = true
		ledger := &ledgerReads{t: t, snapshots: [][]bead{planned, drifted}}
		src := fixedSource(issues, nil, closer)
		src.beads = ledger.read

		var out strings.Builder
		err := run(context.Background(), &out, config{act: true, window: defaultWindow, limit: defaultLimit, repo: defaultRepo}, src)
		if err == nil {
			t.Fatalf("run closed #814 after a bead took it; output:\n%s", out.String())
		}
		if !strings.Contains(err.Error(), "the bead ledger moved") || !strings.Contains(err.Error(), "#814") {
			t.Errorf("err = %q, want it to name the drift and the issue it refused to close", err)
		}
		if ledger.calls != 2 {
			t.Errorf("the ledger was read %d time(s), want 2 — one for the plan and one at the write boundary. "+
				"With a single read the check below is a map compared against itself (bd gqlc-345j)", ledger.calls)
		}
	})

	t.Run("the canonical loses its claimant between the two reads", func(t *testing.T) {
		// The other direction: nobody takes #814, but #816 stops being the
		// mirror the ledger tracks, so the bead ids the closing comment would
		// carry are stale and the CLOSE has lost its justification.
		closer := newCloser(t)
		closer.failIfCalled = true
		reassigned := []bead{boundTo("gqlc-other", 816)}
		ledger := &ledgerReads{t: t, snapshots: [][]bead{planned, reassigned}}
		src := fixedSource(issues, nil, closer)
		src.beads = ledger.read

		var out strings.Builder
		err := run(context.Background(), &out, config{act: true, window: defaultWindow, limit: defaultLimit, repo: defaultRepo}, src)
		if err == nil {
			t.Fatalf("run closed #814 against a canonical whose beads had changed; output:\n%s", out.String())
		}
		if !strings.Contains(err.Error(), "#816") {
			t.Errorf("err = %q, want it to name the canonical that moved", err)
		}
	})

	t.Run("an empty boundary read is refused rather than read as nothing bound", func(t *testing.T) {
		closer := newCloser(t)
		closer.failIfCalled = true
		ledger := &ledgerReads{t: t, snapshots: [][]bead{planned, nil}}
		src := fixedSource(issues, nil, closer)
		src.beads = ledger.read

		var out strings.Builder
		err := run(context.Background(), &out, config{act: true, window: defaultWindow, limit: defaultLimit, repo: defaultRepo}, src)
		if err == nil || !strings.Contains(err.Error(), "came back empty") {
			t.Fatalf("err = %v, want the empty boundary read refused", err)
		}
	})

	t.Run("a failing boundary read aborts before any close", func(t *testing.T) {
		closer := newCloser(t)
		closer.failIfCalled = true
		boom := errors.New("bd: database is locked")
		calls := 0
		src := fixedSource(issues, nil, closer)
		src.beads = func(context.Context) ([]bead, error) {
			calls++
			if calls == 1 {
				return planned, nil
			}
			return nil, boom
		}

		var out strings.Builder
		err := run(context.Background(), &out, config{act: true, window: defaultWindow, limit: defaultLimit, repo: defaultRepo}, src)
		if !errors.Is(err, boom) {
			t.Fatalf("err = %v, want it to wrap %v", err, boom)
		}
		if !strings.Contains(err.Error(), "write boundary") {
			t.Errorf("err = %q, want it to say which of the two reads failed", err)
		}
	})

	t.Run("an unmoved ledger still closes", func(t *testing.T) {
		// The control. Without it every case above passes on a run() that
		// refuses unconditionally at the write boundary.
		closer := newCloser(t)
		ledger := &ledgerReads{t: t, snapshots: [][]bead{planned, planned}}
		src := fixedSource(issues, nil, closer)
		src.beads = ledger.read

		var out strings.Builder
		if err := run(context.Background(), &out, config{act: true, window: defaultWindow, limit: defaultLimit, repo: defaultRepo}, src); err != nil {
			t.Fatalf("run: %v", err)
		}
		if len(closer.calls) != 1 || closer.calls[0] != 814 {
			t.Fatalf("closed %v, want [814]", closer.calls)
		}
	})
}

// TestADryRunDoesNotReReadTheLedger holds the boundary read to the arm that has
// a boundary. A dry run writes nothing, so a second `bd list` there is a
// subprocess bought for no property — and it would make the counts in the
// report disagree with the plan they were rendered from.
func TestADryRunDoesNotReReadTheLedger(t *testing.T) {
	issues := []issue{openIssue(814, titleA, bodyA, at(0)), openIssue(816, titleA, bodyA, at(time.Second))}
	closer := newCloser(t)
	closer.failIfCalled = true
	ledger := &ledgerReads{t: t, snapshots: [][]bead{{boundTo("gqlc-u91z", 816)}}}
	src := fixedSource(issues, nil, closer)
	src.beads = ledger.read

	var out strings.Builder
	if err := run(context.Background(), &out, config{act: false, window: defaultWindow, limit: defaultLimit, repo: defaultRepo}, src); err != nil {
		t.Fatalf("run: %v", err)
	}
	if ledger.calls != 1 {
		t.Errorf("a dry run read the ledger %d time(s), want 1", ledger.calls)
	}
}

// TestCheckLedgerDriftIgnoresMovementItIsNotAbout is the guard's own bound. The
// ledger here moves constantly — beads are created and closed by every session
// — so a whole-ledger comparison would abort on activity that has nothing to do
// with the numbers being written to, and an operator would learn to re-run
// until it happened to pass.
func TestCheckLedgerDriftIgnoresMovementItIsNotAbout(t *testing.T) {
	findings := []finding{
		{Orphan: 814, Verdict: verdictClose, Canonical: 816, Beads: []string{"gqlc-u91z"}},
		// A refusal's numbers are never written to, so drift on them is not
		// this guard's business either.
		{Orphan: 900, Verdict: verdictRefuse, Reason: "bodies differ"},
	}
	planned := boundIssues([]bead{boundTo("gqlc-u91z", 816), boundTo("gqlc-old", 500)}, defaultRepo)
	fresh := boundIssues([]bead{
		boundTo("gqlc-u91z", 816),
		// gqlc-old's issue #500 has gone and a wholly new bead has arrived.
		boundTo("gqlc-new", 999),
		// And a bead has taken the number of an issue only a REFUSE names.
		boundTo("gqlc-late", 900),
	}, defaultRepo)

	if err := checkLedgerDrift(findings, planned, fresh); err != nil {
		t.Errorf("checkLedgerDrift refused over movement outside the numbers being written to: %v", err)
	}
}

// TestBothGhLegsPinTheRepository is bd gqlc-hgos. Neither leg carried --repo,
// so both resolved the repository from gh's own order — the caller's cwd, its
// upstream remote, or GH_REPO. Agents here run from a dozen sibling worktrees,
// so a run that happened to resolve correctly witnesses nothing about the next
// one, and the write leg is `gh issue close`.
//
// It reads the argv builders rather than running gh, because running the write
// leg is the thing no test in this package may do.
func TestBothGhLegsPinTheRepository(t *testing.T) {
	const repo = "someone/elsewhere"

	list := ghIssueListArgs(repo, 7)
	closeArgs := ghIssueCloseArgs(repo, 814, "a comment")

	for name, args := range map[string][]string{"issue list": list, "issue close": closeArgs} {
		i := slices.Index(args, "--repo")
		if i < 0 {
			t.Errorf("`gh %s` argv is %v, which carries no --repo; gh would resolve the repository from the caller's cwd", name, args)
			continue
		}
		if got := args[i+1]; got != repo {
			t.Errorf("`gh %s` passes --repo %q, want %q", name, got, repo)
		}
	}

	// The rest of each argv still has to be the invocation it was, or the pin
	// above passes over a command that no longer reads or closes anything.
	if !slices.Contains(list, "--json") || !slices.Contains(list, "number,title,body,createdAt,state") {
		t.Errorf("`gh issue list` argv is %v, want the five fields the predicate reads", list)
	}
	if !slices.Contains(list, "7") {
		t.Errorf("`gh issue list` argv is %v, want the limit forwarded", list)
	}
	if !slices.Contains(closeArgs, "814") || !slices.Contains(closeArgs, "not planned") || !slices.Contains(closeArgs, "a comment") {
		t.Errorf("`gh issue close` argv is %v, want the number, the reason and the comment", closeArgs)
	}
}

// TestRunRefusesARepoGhWouldNotUnderstand. gh falls back to ambient resolution
// for a value it cannot parse as OWNER/NAME, which is the state -repo exists to
// remove — so a malformed flag has to abort rather than be handed over.
func TestRunRefusesARepoGhWouldNotUnderstand(t *testing.T) {
	for _, repo := range []string{"", "gqlc", "areqag/gqlc/extra", "areqag /gqlc", "https://github.com/areqag/gqlc"} {
		t.Run(fmt.Sprintf("%q", repo), func(t *testing.T) {
			closer := newCloser(t)
			closer.failIfCalled = true
			issuesRead := false
			src := fixedSource(nil, nil, closer)
			src.issues = func(context.Context, string, int) ([]issue, error) {
				issuesRead = true
				return nil, nil
			}

			var out strings.Builder
			err := run(context.Background(), &out, config{act: true, window: defaultWindow, limit: defaultLimit, repo: repo}, src)
			if err == nil || !strings.Contains(err.Error(), "not OWNER/NAME") {
				t.Fatalf("err = %v, want -repo %q refused", err, repo)
			}
			if issuesRead {
				t.Errorf("the read leg ran before -repo was checked; gh had already resolved a repository by then")
			}
		})
	}

	// The control: the default is accepted, so the pattern above is not simply
	// refusing everything.
	closer := newCloser(t)
	closer.failIfCalled = true
	issues := []issue{openIssue(814, titleA, bodyA, at(0)), openIssue(816, titleA, bodyA, at(time.Second))}
	var out strings.Builder
	if err := run(context.Background(), &out, config{act: false, window: defaultWindow, limit: defaultLimit, repo: defaultRepo},
		fixedSource(issues, []bead{boundTo("gqlc-u91z", 816)}, closer)); err != nil {
		t.Fatalf("the default -repo was refused: %v", err)
	}
}

// TestTheRepoFlagReachesBothLegs closes the gap between the argv builders and
// run(): the builders can pin --repo perfectly while run() hands them a
// different string, or none. Both legs record what they were given.
func TestTheRepoFlagReachesBothLegs(t *testing.T) {
	const repo = "someone/elsewhere"
	issues := []issue{openIssue(814, titleA, bodyA, at(0)), openIssue(816, titleA, bodyA, at(time.Second))}
	beads := []bead{{ID: "gqlc-u91z", ExternalRef: "https://github.com/someone/elsewhere/issues/816"}}

	var readRepo, closeRepo string
	src := source{
		issues: func(_ context.Context, r string, _ int) ([]issue, error) {
			readRepo = r
			return issues, nil
		},
		beads: func(context.Context) ([]bead, error) { return beads, nil },
		closeIssue: func(_ context.Context, r string, _ int, _ string) error {
			closeRepo = r
			return nil
		},
	}

	var out strings.Builder
	if err := run(context.Background(), &out, config{act: true, window: defaultWindow, limit: defaultLimit, repo: repo}, src); err != nil {
		t.Fatalf("run: %v", err)
	}
	if readRepo != repo {
		t.Errorf("the read leg was given repo %q, want %q", readRepo, repo)
	}
	if closeRepo != repo {
		t.Errorf("the write leg was given repo %q, want %q — an empty value means it was never called, "+
			"so the ledger's local-ref pattern did not follow -repo either", closeRepo, repo)
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
	if err := run(context.Background(), &out, config{act: false, window: defaultWindow, limit: defaultLimit, repo: defaultRepo}, fixedSource(issues, beads, closer)); err != nil {
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

// TestTheDryRunSummarySaysWhatHappenedAndNothingElse holds the summary line to
// an exact shape. It carried "(a refusal needs a human, not a wider flag)"
// until bd gqlc-mb8v's round-1 review — a claim about the flag surface printed
// underneath a report the flags had already shaped, and false in both
// directions per plan()'s ceiling comment and
// TestANarrowerWindowCannotTurnAnAmbiguousRefusalIntoAClose.
//
// Round 2 deleted the sentence and pinned the deletion by searching the line
// for the token "flag". Round-2 review broke that pin: five rewordings of the
// same claim that avoid the token left the suite green, among them the round-1
// string with one word changed — "a refusal needs a human, not a wider window",
// which names the flag that actually does move refusals. An absence check can
// only ever name spellings, and the set of spellings is unbounded, so the check
// is replaced rather than extended.
//
// What is pinned instead is the shape: the line carries the two counts and
// nothing else, so an added clause fails whatever words it is built from. What
// this does NOT do is judge whether an added sentence is true — it declines all
// of them, including true ones. The cost is that a deliberate edit to the
// summary has to be made here too, which is the point: this is the line an
// operator reads while deciding what to run next.
//
// The pattern owns the wording only; the counts in it are pinned by
// TestDryRunOverTheRealCorpusMutatesNothingAndSaysWhatItWould. It reads the
// summary line alone, not the whole report — CLOSE lines carry bead ids and a
// four-character bd suffix can spell anything.
func TestTheDryRunSummarySaysWhatHappenedAndNothingElse(t *testing.T) {
	issues, beads := loadCorpus(t)
	closer := newCloser(t)
	closer.failIfCalled = true

	var out strings.Builder
	if err := run(context.Background(), &out, config{act: false, window: defaultWindow, limit: defaultLimit, repo: defaultRepo}, fixedSource(issues, beads, closer)); err != nil {
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
	shape := regexp.MustCompile(`^ghorphan: DRY RUN — nothing was mutated\. \d+ issue\(s\) would be closed, \d+ refused\. Re-run with -close to act\.$`)
	if !shape.MatchString(summary) {
		t.Errorf("the dry-run summary is\n\t%q\nand the only shape allowed here is\n\t%q\n"+
			"Anything beyond the two counts is a claim made at the moment the operator "+
			"decides what to run next, and the last one was false in both -window "+
			"directions. If this line is being changed on purpose, change it here too.",
			summary, shape)
	}
}

// TestReconcilingTheRealCorpusIsIdempotent runs the tool against the fixture
// with -close, applies the closes to the corpus the way GitHub would, and runs
// it again. The second run must plan nothing at all.
func TestReconcilingTheRealCorpusIsIdempotent(t *testing.T) {
	issues, beads := loadCorpus(t)

	closer := newCloser(t)
	var out strings.Builder
	if err := run(context.Background(), &out, config{act: true, window: defaultWindow, limit: defaultLimit, repo: defaultRepo}, fixedSource(issues, beads, closer)); err != nil {
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
	if err := run(context.Background(), &out2, config{act: true, window: defaultWindow, limit: defaultLimit, repo: defaultRepo}, fixedSource(after, beads, second)); err != nil {
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
	bound := boundIssues(beads, defaultRepo)
	if len(bound.any) != 18 {
		t.Fatalf("the fixture binds %d issue(s), want 18", len(bound.any))
	}
	// Every ref in the real fixture names areqag/gqlc, so the two maps agree
	// here. A drift between them would mean the fixture has grown a foreign
	// ref and the count above stopped meaning what the next loop reads.
	if len(bound.local) != 18 {
		t.Fatalf("the fixture binds %d issue(s) LOCALLY, want 18 — every captured ref names %s", len(bound.local), defaultRepo)
	}
	for _, f := range planOrFatal(t, issues, beads) {
		if ids := bound.any[f.Orphan]; len(ids) > 0 {
			t.Errorf("#%d is named by bead(s) %v and was still planned as an orphan", f.Orphan, ids)
		}
	}
}
