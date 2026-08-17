package main

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Tests target check() directly — no git shellout, no chdir, no tempdir. The
// git boundary in run() is exercised only by the end-to-end incident replay
// against the real repo (see PR body); gitRefContaining sits behind the same
// boundary and is injected here as a func value (bd gqlc-drvx). Motivation:
// earlier iterations tried to stage a real git repo per test, and the tempdir
// git calls occasionally landed in the outer worktree despite
// GIT_CEILING_DIRECTORIES — pointless blast radius for testing a comparator.

const (
	issueOpenA    = `{"_type":"issue","id":"a","status":"open"}`
	issueOpenB    = `{"_type":"issue","id":"b","status":"open"}`
	issueOpenC    = `{"_type":"issue","id":"c","status":"open"}`
	issueClosedA  = `{"_type":"issue","id":"a","status":"closed"}`
	issueEmptyID  = `{"_type":"issue","id":"","status":"open"}`
	commentRecord = `{"_type":"comment","id":"c1"}`

	// The witnessed shape (bd gqlc-npyp / gqlc-rz0l): a branch-close hook
	// closed against a sha a rebase then discarded.
	closingSHA   = "3dd322dc"
	baseClosedAt = "2026-08-17T07:54:46Z"
	headFixedAt  = "2026-08-17T09:30:00Z"
)

// closedAtBase renders a closed record whose close_reason cites reason and
// whose updated_at is at.
func closedAtBase(id, reason, at string) string {
	return fmt.Sprintf(`{"_type":"issue","id":%q,"status":"closed","updated_at":%q,"close_reason":%q}`, id, at, reason)
}

func reopenedAtHead(id, status, at string) string {
	return fmt.Sprintf(`{"_type":"issue","id":%q,"status":%q,"updated_at":%q}`, id, status, at)
}

// hookClose is what the branch-close hook wrote for gqlc-rz0l, trimmed.
func hookClose(sha string) string {
	return "Closed by branch docs/c1-bare-arg-spec-drift at " + sha + " (12 commits, not yet pushed)."
}

// noRefContains is the probe answer in a checkout that placed the sha on
// nothing — a shallow clone, an absent object, or a genuinely unreferenced
// commit. It grants nothing on its own.
func noRefContains(string) string { return "" }

func refContains(ref string) func(string) string {
	return func(string) string { return ref }
}

// witnessedReopen is the granted case: base closed citing closingSHA, head
// in_progress and written later, declaration naming that sha, no ref holding
// it.
func witnessedReopen() (head, base string, ex exemptions) {
	base = closedAtBase("a", hookClose(closingSHA), baseClosedAt) + "\n"
	head = reopenedAtHead("a", "in_progress", headFixedAt) + "\n"
	ex = exemptions{
		reopens:       map[string]string{"a": closingSHA},
		refContaining: noRefContains,
	}
	return head, base, ex
}

func TestCheck_CleanAdditivePass(t *testing.T) {
	base := issueOpenA + "\n"
	head := issueOpenA + "\n" + issueOpenB + "\n"
	if err := check([]byte(head), []byte(base), "test-base", exemptions{}); err != nil {
		t.Fatalf("expected pass, got: %v", err)
	}
}

func TestCheck_NewClosurePass(t *testing.T) {
	base := issueOpenA + "\n"
	head := issueClosedA + "\n"
	if err := check([]byte(head), []byte(base), "test-base", exemptions{}); err != nil {
		t.Fatalf("expected pass, got: %v", err)
	}
}

func TestCheck_DroppedIssueFails(t *testing.T) {
	base := issueOpenA + "\n" + issueOpenB + "\n"
	head := issueOpenA + "\n"
	err := check([]byte(head), []byte(base), "test-base", exemptions{})
	if err == nil {
		t.Fatal("expected failure, got nil")
	}
	if !strings.Contains(err.Error(), "dropped") || !strings.Contains(err.Error(), "\n    - b\n") {
		t.Errorf("expected dropped id 'b' in message, got: %v", err)
	}
}

func TestCheck_ReopenedIssueFails(t *testing.T) {
	base := issueClosedA + "\n"
	head := issueOpenA + "\n"
	err := check([]byte(head), []byte(base), "test-base", exemptions{})
	if err == nil {
		t.Fatal("expected failure, got nil")
	}
	if !strings.Contains(err.Error(), "reopened") || !strings.Contains(err.Error(), "a (base=closed, head=open)") {
		t.Errorf("expected reopened id 'a' in message, got: %v", err)
	}
}

func TestCheck_BaseAbsentPass(t *testing.T) {
	// Repo-before-bd shape: run() passes nil bytes when the ref lacks the file.
	head := issueOpenA + "\n"
	if err := check([]byte(head), nil, "test-base", exemptions{}); err != nil {
		t.Fatalf("expected pass, got: %v", err)
	}
}

func TestCheck_FormatDriftInBaseFails(t *testing.T) {
	// Base has 3 non-blank lines but zero recognised "issue" records —
	// simulates bd renaming _type or the value "issue". Silent pass would
	// mean the gate is inert; must fail loud.
	base := commentRecord + "\n" + commentRecord + "\n" + commentRecord + "\n"
	head := issueOpenA + "\n"
	err := check([]byte(head), []byte(base), "test-base", exemptions{})
	if err == nil {
		t.Fatal("expected format-drift failure, got nil")
	}
	if !strings.Contains(err.Error(), "format drift") {
		t.Errorf("expected format-drift message, got: %v", err)
	}
}

func TestCheck_FormatDriftInHeadFails(t *testing.T) {
	base := issueOpenA + "\n"
	head := commentRecord + "\n" + commentRecord + "\n"
	err := check([]byte(head), []byte(base), "test-base", exemptions{})
	if err == nil {
		t.Fatal("expected format-drift failure, got nil")
	}
	if !strings.Contains(err.Error(), "format drift") {
		t.Errorf("expected format-drift message, got: %v", err)
	}
}

func TestCheck_AllowlistedDropPasses(t *testing.T) {
	base := issueOpenA + "\n" + issueOpenB + "\n"
	head := issueOpenA + "\n"
	if err := check([]byte(head), []byte(base), "test-base", exemptions{drops: map[string]bool{"b": true}}); err != nil {
		t.Fatalf("expected pass, got: %v", err)
	}
}

func TestCheck_DropAllowlistDoesNotReachReopens(t *testing.T) {
	// allowed-drops.txt declares a deletion. The same id listed there, with a
	// reopen in the diff, must still fail: the two arms take different shapes.
	head, base, _ := witnessedReopen()
	err := check([]byte(head), []byte(base), "test-base", exemptions{drops: map[string]bool{"a": true}})
	if err == nil {
		t.Fatal("expected failure, got nil")
	}
	if !strings.Contains(err.Error(), "a (base=closed, head=in_progress)") {
		t.Errorf("expected reopened id 'a' in message, got: %v", err)
	}
	if strings.Contains(err.Error(), "dropped") {
		t.Errorf("reopen must not be reported as a drop, got: %v", err)
	}
}

func TestCheck_AllowlistSubtractsOnlyListedDrops(t *testing.T) {
	base := issueOpenA + "\n" + issueOpenB + "\n" + issueOpenC + "\n"
	head := issueOpenA + "\n"
	err := check([]byte(head), []byte(base), "test-base", exemptions{drops: map[string]bool{"b": true}})
	if err == nil {
		t.Fatal("expected failure, got nil")
	}
	if !strings.Contains(err.Error(), "\n    - c\n") {
		t.Errorf("expected unlisted drop 'c' in message, got: %v", err)
	}
	if strings.Contains(err.Error(), "\n    - b\n") {
		t.Errorf("listed drop 'b' must not be reported, got: %v", err)
	}
}

func TestCheck_AsteriskIsNotAWildcard(t *testing.T) {
	base := issueOpenA + "\n" + issueOpenB + "\n"
	head := issueOpenA + "\n"
	err := check([]byte(head), []byte(base), "test-base", exemptions{drops: map[string]bool{"*": true}})
	if err == nil {
		t.Fatal("expected failure: ids match exactly, '*' is not a wildcard")
	}
	if !strings.Contains(err.Error(), "\n    - b\n") {
		t.Errorf("expected dropped id 'b' in message, got: %v", err)
	}
}

func TestCheck_StaleAllowlistEntryPasses(t *testing.T) {
	// Steady state after a deletion lands on master: the id is absent at base
	// and head alike, so the entry matches nothing. Inert, not an error.
	base := issueOpenA + "\n"
	head := issueOpenA + "\n"
	if err := check([]byte(head), []byte(base), "test-base", exemptions{drops: map[string]bool{"long-gone": true}}); err != nil {
		t.Fatalf("expected pass, got: %v", err)
	}
}

func TestCheck_AllowlistDoesNotMaskFormatDrift(t *testing.T) {
	// Head parses to zero issue records, so every base id looks dropped. With
	// 'a' listed, subtraction alone would turn bd format drift into a pass.
	base := issueOpenA + "\n"
	head := commentRecord + "\n" + commentRecord + "\n"
	err := check([]byte(head), []byte(base), "test-base", exemptions{drops: map[string]bool{"a": true}})
	if err == nil {
		t.Fatal("expected format-drift failure, got nil")
	}
	if !strings.Contains(err.Error(), "format drift") {
		t.Errorf("expected format-drift message, got: %v", err)
	}
}

func TestCheck_DropHintNamesAllowlist(t *testing.T) {
	base := issueOpenA + "\n" + issueOpenB + "\n"
	head := issueOpenA + "\n"
	err := check([]byte(head), []byte(base), "test-base", exemptions{})
	if err == nil {
		t.Fatal("expected failure, got nil")
	}
	if !strings.Contains(err.Error(), "passive bd export") {
		t.Errorf("expected the existing sync hint, got: %v", err)
	}
	if !strings.Contains(err.Error(), allowedDropsPath) {
		t.Errorf("expected %s named as the remedy, got: %v", allowedDropsPath, err)
	}
	if strings.Contains(err.Error(), allowedReopensPath) {
		t.Errorf("drop-only failure must not name %s, got: %v", allowedReopensPath, err)
	}
}

func TestCheck_ReopenHintNamesReopensFileNotDrops(t *testing.T) {
	// Pointing a reopen at allowed-drops.txt would advertise a remedy that
	// does not work there; the reopen remedy has its own file and shape.
	base := issueClosedA + "\n"
	head := issueOpenA + "\n"
	err := check([]byte(head), []byte(base), "test-base", exemptions{})
	if err == nil {
		t.Fatal("expected failure, got nil")
	}
	if strings.Contains(err.Error(), allowedDropsPath) {
		t.Errorf("reopen-only failure must not name %s, got: %v", allowedDropsPath, err)
	}
	if !strings.Contains(err.Error(), allowedReopensPath) {
		t.Errorf("expected %s named as the remedy, got: %v", allowedReopensPath, err)
	}
	if !strings.Contains(err.Error(), "<bead-id> <closing-sha>") {
		t.Errorf("expected the required entry shape in the hint, got: %v", err)
	}
}

// --- the reopened arm's exemption -------------------------------------------

func TestCheck_DeclaredUnearnedCloseReopenPasses(t *testing.T) {
	// The witnessed correction: gqlc-rz0l's close cited a sha a rebase
	// discarded, and the bead was moved back to in_progress afterwards.
	head, base, ex := witnessedReopen()
	if err := check([]byte(head), []byte(base), "test-base", ex); err != nil {
		t.Fatalf("expected pass, got: %v", err)
	}
}

func TestCheck_StaleExportIsRefusedEvenWhenDeclared(t *testing.T) {
	// The revert this arm has to keep out: same two status columns as the
	// granted case, same declaration, same sha — but head's record predates
	// base's close, which is what an older export copied forward looks like.
	base := closedAtBase("a", hookClose(closingSHA), baseClosedAt) + "\n"
	head := reopenedAtHead("a", "in_progress", "2026-08-16T10:00:00Z") + "\n"
	ex := exemptions{
		reopens:       map[string]string{"a": closingSHA},
		refContaining: noRefContains,
	}
	err := check([]byte(head), []byte(base), "test-base", ex)
	if err == nil {
		t.Fatal("expected failure: a record older than base's close is a revert, not a correction")
	}
	if !strings.Contains(err.Error(), "is not after base") {
		t.Errorf("expected the recency refusal, got: %v", err)
	}
	if !strings.Contains(err.Error(), "2026-08-16T10:00:00Z") || !strings.Contains(err.Error(), baseClosedAt) {
		t.Errorf("expected both timestamps as the witness, got: %v", err)
	}
}

func TestCheck_EqualUpdatedAtIsRefused(t *testing.T) {
	// A reopen is a write, so it moves updated_at. An unchanged one means the
	// record was carried across, not corrected.
	base := closedAtBase("a", hookClose(closingSHA), baseClosedAt) + "\n"
	head := reopenedAtHead("a", "in_progress", baseClosedAt) + "\n"
	ex := exemptions{
		reopens:       map[string]string{"a": closingSHA},
		refContaining: noRefContains,
	}
	err := check([]byte(head), []byte(base), "test-base", ex)
	if err == nil {
		t.Fatal("expected failure on equal updated_at")
	}
	if !strings.Contains(err.Error(), "is not after base") {
		t.Errorf("expected the recency refusal, got: %v", err)
	}
}

func TestCheck_DeclaredSHANotCitedAtBaseIsRefused(t *testing.T) {
	// The declaration has to quote base, which comes out of a git ref. A sha
	// the base record does not mention is an assertion the head made alone.
	base := closedAtBase("a", hookClose("aaaaaaa1"), baseClosedAt) + "\n"
	head := reopenedAtHead("a", "in_progress", headFixedAt) + "\n"
	ex := exemptions{
		reopens:       map[string]string{"a": closingSHA},
		refContaining: noRefContains,
	}
	err := check([]byte(head), []byte(base), "test-base", ex)
	if err == nil {
		t.Fatal("expected failure: declared sha is not cited at base")
	}
	if !strings.Contains(err.Error(), "cites no such sha") {
		t.Errorf("expected the citation refusal, got: %v", err)
	}
}

func TestCheck_LaterCloseUnderADifferentSHADoesNotInheritTheEntry(t *testing.T) {
	// Expiry: the correction landed, the bead was re-closed for real, and a
	// second reopen turns up with the old entry still in the file. The entry
	// names the disputed close, so it does not carry over.
	base := closedAtBase("a", "Closed by branch x at bbbbbbb2 (merged).", baseClosedAt) + "\n"
	head := reopenedAtHead("a", "in_progress", headFixedAt) + "\n"
	ex := exemptions{
		reopens:       map[string]string{"a": closingSHA},
		refContaining: noRefContains,
	}
	err := check([]byte(head), []byte(base), "test-base", ex)
	if err == nil {
		t.Fatal("expected failure: the entry names a close this base does not show")
	}
	if !strings.Contains(err.Error(), "cites no such sha") {
		t.Errorf("expected the citation refusal, got: %v", err)
	}
}

func TestCheck_SHAOnARefIsRefused(t *testing.T) {
	// The veto. Everything else holds; the closing commit is sitting on a
	// remote-tracking ref, so the close cites work that was pushed.
	head, base, ex := witnessedReopen()
	ex.refContaining = refContains("refs/remotes/origin/docs/c1-bare-arg-spec-drift")
	err := check([]byte(head), []byte(base), "test-base", ex)
	if err == nil {
		t.Fatal("expected failure: a sha on a ref is not an unearned close")
	}
	if !strings.Contains(err.Error(), "refs/remotes/origin/docs/c1-bare-arg-spec-drift contains "+closingSHA) {
		t.Errorf("expected the containing ref named as the witness, got: %v", err)
	}
}

func TestCheck_MissingUpdatedAtIsRefused(t *testing.T) {
	tests := []struct {
		name       string
		base, head string
		want       string
	}{
		{
			name: "head has none",
			base: closedAtBase("a", hookClose(closingSHA), baseClosedAt) + "\n",
			head: `{"_type":"issue","id":"a","status":"in_progress"}` + "\n",
			want: `head updated_at "" is not RFC3339`,
		},
		{
			name: "base has none",
			base: fmt.Sprintf(`{"_type":"issue","id":"a","status":"closed","close_reason":%q}`, hookClose(closingSHA)) + "\n",
			head: reopenedAtHead("a", "in_progress", headFixedAt) + "\n",
			want: `base updated_at "" is not RFC3339`,
		},
		{
			name: "head is not RFC3339",
			base: closedAtBase("a", hookClose(closingSHA), baseClosedAt) + "\n",
			head: reopenedAtHead("a", "in_progress", "17 Aug 2026") + "\n",
			want: `head updated_at "17 Aug 2026" is not RFC3339`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ex := exemptions{
				reopens:       map[string]string{"a": closingSHA},
				refContaining: noRefContains,
			}
			err := check([]byte(tt.head), []byte(tt.base), "test-base", ex)
			if err == nil {
				t.Fatal("expected failure, got nil")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("expected %q, got: %v", tt.want, err)
			}
		})
	}
}

func TestCheck_RefusedDeclarationNamesTheEntry(t *testing.T) {
	// A refusal that only said "reopened" would leave the author unable to
	// tell a rejected declaration from an absent one.
	base := closedAtBase("a", hookClose(closingSHA), baseClosedAt) + "\n"
	head := reopenedAtHead("a", "in_progress", "2026-08-16T10:00:00Z") + "\n"
	ex := exemptions{
		reopens:       map[string]string{"a": closingSHA},
		refContaining: noRefContains,
	}
	err := check([]byte(head), []byte(base), "test-base", ex)
	if err == nil {
		t.Fatal("expected failure, got nil")
	}
	if !strings.Contains(err.Error(), "declaration `a "+closingSHA+"` refused:") {
		t.Errorf("expected the refused entry quoted back, got: %v", err)
	}
}

func TestCheck_UndeclaredReopenAlongsideADeclaredOneStillFails(t *testing.T) {
	// Subtraction has to be per id: granting 'a' must not carry 'b' with it.
	base := closedAtBase("a", hookClose(closingSHA), baseClosedAt) + "\n" +
		closedAtBase("b", hookClose(closingSHA), baseClosedAt) + "\n"
	head := reopenedAtHead("a", "in_progress", headFixedAt) + "\n" +
		reopenedAtHead("b", "in_progress", headFixedAt) + "\n"
	ex := exemptions{
		reopens:       map[string]string{"a": closingSHA},
		refContaining: noRefContains,
	}
	err := check([]byte(head), []byte(base), "test-base", ex)
	if err == nil {
		t.Fatal("expected failure: 'b' was not declared")
	}
	if !strings.Contains(err.Error(), "b (base=closed, head=in_progress)") {
		t.Errorf("expected undeclared reopen 'b' in message, got: %v", err)
	}
	if strings.Contains(err.Error(), "a (base=closed") {
		t.Errorf("declared reopen 'a' must not be reported, got: %v", err)
	}
}

func TestCheck_DeclaredReopenWithNoProbeIsAnError(t *testing.T) {
	// Losing the wiring would silently drop the veto and grant more, so the
	// missing probe fails rather than disabling itself.
	head, base, ex := witnessedReopen()
	ex.refContaining = nil
	err := check([]byte(head), []byte(base), "test-base", ex)
	if err == nil {
		t.Fatal("expected failure when no ref probe is wired")
	}
	if !strings.Contains(err.Error(), "no ref probe was wired") {
		t.Errorf("expected the unwired-probe message, got: %v", err)
	}
}

func TestCheck_StaleReopenEntryPasses(t *testing.T) {
	// Steady state after a correction lands: the bead is no longer closed at
	// base, so the arm does not fire and the entry matches nothing.
	base := reopenedAtHead("a", "in_progress", baseClosedAt) + "\n"
	head := reopenedAtHead("a", "in_progress", headFixedAt) + "\n"
	ex := exemptions{
		reopens:       map[string]string{"a": closingSHA},
		refContaining: noRefContains,
	}
	if err := check([]byte(head), []byte(base), "test-base", ex); err != nil {
		t.Fatalf("expected pass, got: %v", err)
	}
}

func TestCheck_ReopenExemptionDoesNotReachDrops(t *testing.T) {
	// Mirror of the drops/reopens separation: an id declared for reopen that
	// is instead absent at head is a drop, and drops are declared elsewhere.
	base := closedAtBase("a", hookClose(closingSHA), baseClosedAt) + "\n"
	head := issueOpenB + "\n"
	ex := exemptions{
		reopens:       map[string]string{"a": closingSHA},
		refContaining: noRefContains,
	}
	err := check([]byte(head), []byte(base), "test-base", ex)
	if err == nil {
		t.Fatal("expected failure: a reopen declaration does not exempt a deletion")
	}
	if !strings.Contains(err.Error(), "dropped") || !strings.Contains(err.Error(), "\n    - a\n") {
		t.Errorf("expected dropped id 'a' in message, got: %v", err)
	}
}

func TestCitesSHA(t *testing.T) {
	tests := []struct {
		name string
		text string
		sha  string
		want bool
	}{
		{"hook wording", hookClose("3dd322dc"), "3dd322dc", true},
		{"end of text", "closed at 3dd322dc", "3dd322dc", true},
		{"whole text", "3dd322dc", "3dd322dc", true},
		{"absent", hookClose("aaaaaaa1"), "3dd322dc", false},
		{"empty close_reason", "", "3dd322dc", false},
		// An abbreviation of a cited sha is a different token: 3dd322dc is a
		// prefix of 3dd322dcab, and prefix-matching would let a declaration
		// name a commit the close never mentioned.
		{"declared is a prefix of the cited token", "closed at 3dd322dcab.", "3dd322dc", false},
		{"declared is a suffix of the cited token", "closed at ff3dd322dc.", "3dd322dc", false},
		{"embedded in a word", "branch feature/3dd322dcx closed", "3dd322dc", false},
		{"parenthesised", "closed (3dd322dc)", "3dd322dc", true},
		{"comma delimited", "3dd322dc, then a rebase", "3dd322dc", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := citesSHA(tt.text, tt.sha); got != tt.want {
				t.Errorf("citesSHA(%q, %q) = %v, want %v", tt.text, tt.sha, got, tt.want)
			}
		})
	}
}

func TestParseAllowedDrops(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want map[string]bool
	}{
		{"empty", "", map[string]bool{}},
		{"blank lines", "\n   \n\t\n", map[string]bool{}},
		{"comments", "# ghost bead\n   # indented\n", map[string]bool{}},
		{"ids trimmed", "  gqlc-1  \n\tgqlc-2\t\n", map[string]bool{"gqlc-1": true, "gqlc-2": true}},
		{"crlf", "gqlc-1\r\ngqlc-2\r\n", map[string]bool{"gqlc-1": true, "gqlc-2": true}},
		{"no trailing newline", "gqlc-1", map[string]bool{"gqlc-1": true}},
		{"duplicates collapse", "gqlc-1\ngqlc-1\n", map[string]bool{"gqlc-1": true}},
		// '#' opens a comment only at the start of a line. A trailing one
		// leaves an entry matching no id, so the drop still fails.
		{"inline hash is part of the id", "gqlc-1 # ghost\n", map[string]bool{"gqlc-1 # ghost": true}},
		{"asterisk is a literal id", "*\n", map[string]bool{"*": true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseAllowedDrops([]byte(tt.in))
			if !maps.Equal(got, tt.want) {
				t.Errorf("parseAllowedDrops(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestParseAllowedReopens(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want map[string]string
	}{
		{"empty", "", map[string]string{}},
		{"blank lines", "\n   \n\t\n", map[string]string{}},
		{"comments", "# unearned close\n   # indented\n", map[string]string{}},
		{"pair", "gqlc-rz0l 3dd322dc\n", map[string]string{"gqlc-rz0l": "3dd322dc"}},
		{"trimmed and tab separated", "  gqlc-1\t3dd322dc  \n", map[string]string{"gqlc-1": "3dd322dc"}},
		{"crlf", "gqlc-1 3dd322dc\r\n", map[string]string{"gqlc-1": "3dd322dc"}},
		{"no trailing newline", "gqlc-1 3dd322dc", map[string]string{"gqlc-1": "3dd322dc"}},
		{"full length sha", "gqlc-1 " + strings.Repeat("a", 40) + "\n", map[string]string{"gqlc-1": strings.Repeat("a", 40)}},
		{"two ids", "gqlc-1 3dd322dc\ngqlc-2 aaaaaaa1\n", map[string]string{"gqlc-1": "3dd322dc", "gqlc-2": "aaaaaaa1"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseAllowedReopens([]byte(tt.in))
			if err != nil {
				t.Fatalf("parseAllowedReopens(%q): %v", tt.in, err)
			}
			if !maps.Equal(got, tt.want) {
				t.Errorf("parseAllowedReopens(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestParseAllowedReopens_MalformedIsAnError(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		// The load-bearing row: a bare id is allowed-drops.txt's shape, and
		// taking it here would make this arm a bare-id allowlist.
		{"bare id", "gqlc-rz0l\n", "want `<bead-id> <closing-sha>`, got 1 field"},
		{"three fields", "gqlc-1 3dd322dc extra\n", "want `<bead-id> <closing-sha>`, got 3 field"},
		{"sha too short", "gqlc-1 3dd322\n", "not a 7-40 character lowercase hex sha"},
		{"sha too long", "gqlc-1 " + strings.Repeat("a", 41) + "\n", "not a 7-40 character lowercase hex sha"},
		{"sha not hex", "gqlc-1 3dd322dg\n", "not a 7-40 character lowercase hex sha"},
		{"sha uppercase", "gqlc-1 3DD322DC\n", "not a 7-40 character lowercase hex sha"},
		{"asterisk is not a wildcard sha", "gqlc-1 *\n", "not a 7-40 character lowercase hex sha"},
		{"duplicate id", "gqlc-1 3dd322dc\ngqlc-1 aaaaaaa1\n", "declared twice"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseAllowedReopens([]byte(tt.in))
			if err == nil {
				t.Fatalf("parseAllowedReopens(%q) = nil error, want a rejection", tt.in)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("expected %q, got: %v", tt.want, err)
			}
		})
	}
}

func TestParseAllowedReopens_ErrorNamesTheLine(t *testing.T) {
	_, err := parseAllowedReopens([]byte("# header\n\ngqlc-1 3dd322dc\ngqlc-2\n"))
	if err == nil {
		t.Fatal("expected a rejection")
	}
	if !strings.Contains(err.Error(), "line 4:") {
		t.Errorf("expected the 1-based line number 4, got: %v", err)
	}
}

// The four allowlist-reading tests take a path argument, so they need no
// chdir — the cwd hazard called out at the top of this file does not apply.

func TestReadAllowedDrops_AbsentFileIsEmpty(t *testing.T) {
	got, err := readAllowedDrops(filepath.Join(t.TempDir(), "allowed-drops.txt"))
	if err != nil {
		t.Fatalf("absent file must not be an error, got: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("allowed = %v, want empty", got)
	}
}

func TestReadAllowedDrops_ReadsIDs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "allowed-drops.txt")
	if err := os.WriteFile(path, []byte("# ghost from a sync bug\ngqlc-1\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	got, err := readAllowedDrops(path)
	if err != nil {
		t.Fatalf("readAllowedDrops: %v", err)
	}
	if !maps.Equal(got, map[string]bool{"gqlc-1": true}) {
		t.Errorf("allowed = %v, want {gqlc-1:true}", got)
	}
}

func TestReadAllowedDrops_UnreadableIsError(t *testing.T) {
	// Only os.ErrNotExist means "no allowlist"; any other read failure must
	// surface rather than degrade into an empty allowlist.
	dir := t.TempDir()
	if _, err := readAllowedDrops(dir); err == nil {
		t.Fatalf("expected error reading %s as a file, got nil", dir)
	}
}

func TestReadAllowedReopens(t *testing.T) {
	t.Run("absent file is empty", func(t *testing.T) {
		got, err := readAllowedReopens(filepath.Join(t.TempDir(), "allowed-reopens.txt"))
		if err != nil {
			t.Fatalf("absent file must not be an error, got: %v", err)
		}
		if len(got) != 0 {
			t.Errorf("allowed = %v, want empty", got)
		}
	})
	t.Run("reads pairs", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "allowed-reopens.txt")
		if err := os.WriteFile(path, []byte("# unearned close\ngqlc-1 3dd322dc\n"), 0o644); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
		got, err := readAllowedReopens(path)
		if err != nil {
			t.Fatalf("readAllowedReopens: %v", err)
		}
		if !maps.Equal(got, map[string]string{"gqlc-1": "3dd322dc"}) {
			t.Errorf("allowed = %v, want {gqlc-1:3dd322dc}", got)
		}
	})
	t.Run("unreadable is error", func(t *testing.T) {
		dir := t.TempDir()
		if _, err := readAllowedReopens(dir); err == nil {
			t.Fatalf("expected error reading %s as a file, got nil", dir)
		}
	})
}

func TestParse_EmptyIssueIDFails(t *testing.T) {
	// An issue record with a missing/empty id would silently collapse into
	// the same map slot on every ingest — the drop/reopen accounting would
	// then compare whichever such record appeared last. bd exports are
	// machine-generated today, but the parser's job is to notice when that
	// stops being true.
	data := issueEmptyID + "\n"
	_, _, err := parse([]byte(data))
	if err == nil {
		t.Fatal("expected empty-id failure, got nil")
	}
	if !strings.Contains(err.Error(), "empty id") {
		t.Errorf("expected empty-id message, got: %v", err)
	}
}

func TestParse_IgnoresNonIssueRecordsButCountsThem(t *testing.T) {
	// parse() must count non-issue lines so the caller can distinguish
	// "file empty" (silent pass acceptable) from "file has content, none of
	// it looked like an issue" (format drift, must fail).
	data := commentRecord + "\n" + issueOpenA + "\n"
	got, lines, err := parse([]byte(data))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if lines != 2 {
		t.Errorf("nonZeroLines = %d, want 2", lines)
	}
	if len(got) != 1 || got["a"].Status != "open" {
		t.Errorf("issues = %v, want {a:open}", got)
	}
}

func TestParse_KeepsTheFieldsTheReopenArmReads(t *testing.T) {
	// The reopen exemption reads close_reason and updated_at off the parsed
	// record; dropping either from the struct would make every declaration
	// refuse for the wrong reason.
	data := closedAtBase("a", hookClose(closingSHA), baseClosedAt) + "\n"
	got, _, err := parse([]byte(data))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got["a"].UpdatedAt != baseClosedAt {
		t.Errorf("UpdatedAt = %q, want %q", got["a"].UpdatedAt, baseClosedAt)
	}
	if got["a"].CloseReason != hookClose(closingSHA) {
		t.Errorf("CloseReason = %q, want the hook wording", got["a"].CloseReason)
	}
}
