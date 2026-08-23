package main

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Tests target check() directly — no git shellout, no chdir, no git repo
// staged on disk. The six `t.TempDir()` calls are all in the readAllowed*
// tests, which write and read a plain file; none runs `git init`. The git
// boundary in run() is exercised only by a manual incident replay against a
// scratch repo outside the worktree; gitRefContaining sits behind the same
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
// nothing: the object is outside the fetched pack, or it is present and no ref
// reaches it. Depth-1 does not by itself produce this answer — a sha that came
// down in the pack and sits on a ref answers positively there (see
// gitRefContaining). It grants nothing on its own.
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
		reopens:       map[string]declaration{"a": {sha: closingSHA}},
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

func TestCheck_GrantDoesNotDependOnWhichNonClosedStatusHeadCarries(t *testing.T) {
	// The arm fires on `base closed, head not closed`, and refuseReopen reads
	// the two timestamps, the close_reason and the sha — not the head status.
	// `in_progress` is the witnessed correction's status, but a bead moved back
	// to `open` or `blocked` is the same correction, so pinning the grant to
	// one label would refuse the others for no reason the evidence supports.
	base := closedAtBase("a", hookClose(closingSHA), baseClosedAt) + "\n"
	for _, status := range []string{"open", "blocked"} {
		t.Run(status, func(t *testing.T) {
			head := reopenedAtHead("a", status, headFixedAt) + "\n"
			ex := exemptions{
				reopens:       map[string]declaration{"a": {sha: closingSHA}},
				refContaining: noRefContains,
			}
			if err := check([]byte(head), []byte(base), "test-base", ex); err != nil {
				t.Fatalf("expected pass for head status %q, got: %v", status, err)
			}
		})
	}
}

func TestCheck_StaleExportIsRefusedEvenWhenDeclared(t *testing.T) {
	// The revert this arm has to keep out: same two status columns as the
	// granted case, same declaration, same sha — but head's record predates
	// base's close, which is what an older export copied forward looks like.
	base := closedAtBase("a", hookClose(closingSHA), baseClosedAt) + "\n"
	head := reopenedAtHead("a", "in_progress", "2026-08-16T10:00:00Z") + "\n"
	ex := exemptions{
		reopens:       map[string]declaration{"a": {sha: closingSHA}},
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
		reopens:       map[string]declaration{"a": {sha: closingSHA}},
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
		reopens:       map[string]declaration{"a": {sha: closingSHA}},
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
		reopens:       map[string]declaration{"a": {sha: closingSHA}},
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
		{
			// Base is refused before head is read, so this row also pins the
			// order: an unreadable base is reported as itself and not as the
			// recency comparison it would otherwise feed.
			name: "base is not RFC3339",
			base: closedAtBase("a", hookClose(closingSHA), "17 Aug 2026") + "\n",
			head: reopenedAtHead("a", "in_progress", headFixedAt) + "\n",
			want: `base updated_at "17 Aug 2026" is not RFC3339`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ex := exemptions{
				reopens:       map[string]declaration{"a": {sha: closingSHA}},
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
		reopens:       map[string]declaration{"a": {sha: closingSHA}},
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
		reopens:       map[string]declaration{"a": {sha: closingSHA}},
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
		reopens:       map[string]declaration{"a": {sha: closingSHA}},
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
		reopens:       map[string]declaration{"a": {sha: closingSHA}},
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
		want map[string]declaration
	}{
		{"empty", "", map[string]declaration{}},
		{"blank lines", "\n   \n\t\n", map[string]declaration{}},
		{"comments", "# unearned close\n   # indented\n", map[string]declaration{}},
		{"pair", "gqlc-rz0l 3dd322dc\n", map[string]declaration{"gqlc-rz0l": {sha: "3dd322dc"}}},
		{"trimmed and tab separated", "  gqlc-1\t3dd322dc  \n", map[string]declaration{"gqlc-1": {sha: "3dd322dc"}}},
		{"crlf", "gqlc-1 3dd322dc\r\n", map[string]declaration{"gqlc-1": {sha: "3dd322dc"}}},
		{"no trailing newline", "gqlc-1 3dd322dc", map[string]declaration{"gqlc-1": {sha: "3dd322dc"}}},
		{"full length sha", "gqlc-1 " + strings.Repeat("a", 40) + "\n", map[string]declaration{"gqlc-1": {sha: strings.Repeat("a", 40)}}},
		{"two ids", "gqlc-1 3dd322dc\ngqlc-2 aaaaaaa1\n", map[string]declaration{"gqlc-1": {sha: "3dd322dc"}, "gqlc-2": {sha: "aaaaaaa1"}}},
		// The second shape (bd gqlc-j068). Which field it lands in is decided
		// by the token, so these rows also pin that the two do not collide.
		{"timestamp", "gqlc-1 2026-08-01T10:00:00Z\n", map[string]declaration{"gqlc-1": {closedAt: "2026-08-01T10:00:00Z"}}},
		{"timestamp with offset", "gqlc-1 2026-08-01T12:00:00+02:00\n", map[string]declaration{"gqlc-1": {closedAt: "2026-08-01T12:00:00+02:00"}}},
		{"one of each", "gqlc-1 3dd322dc\ngqlc-2 2026-08-01T10:00:00Z\n", map[string]declaration{"gqlc-1": {sha: "3dd322dc"}, "gqlc-2": {closedAt: "2026-08-01T10:00:00Z"}}},
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
		{"bare id", "gqlc-rz0l\n", "got 1 field"},
		{"three fields", "gqlc-1 3dd322dc extra\n", "got 3 field"},
		{"sha too short", "gqlc-1 3dd322\n", "neither a 7-40 character lowercase hex sha nor an RFC3339 timestamp"},
		{"sha too long", "gqlc-1 " + strings.Repeat("a", 41) + "\n", "neither a 7-40 character lowercase hex sha nor an RFC3339 timestamp"},
		{"sha not hex", "gqlc-1 3dd322dg\n", "neither a 7-40 character lowercase hex sha nor an RFC3339 timestamp"},
		{"sha uppercase", "gqlc-1 3DD322DC\n", "neither a 7-40 character lowercase hex sha nor an RFC3339 timestamp"},
		{"asterisk is not a wildcard sha", "gqlc-1 *\n", "neither a 7-40 character lowercase hex sha nor an RFC3339 timestamp"},
		// The timestamp shape must not become a second door for a loose token:
		// a bare date, or a time with no zone, is not RFC3339 and is refused.
		{"bare date", "gqlc-1 2026-08-01\n", "neither a 7-40 character lowercase hex sha nor an RFC3339 timestamp"},
		{"timestamp with no zone", "gqlc-1 2026-08-01T10:00:00\n", "neither a 7-40 character lowercase hex sha nor an RFC3339 timestamp"},
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
		if !maps.Equal(got, map[string]declaration{"gqlc-1": {sha: "3dd322dc"}}) {
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

func TestParse_MalformedJSONFailsAndNamesTheLine(t *testing.T) {
	// A truncated or otherwise unparseable line must stop the run rather than
	// be skipped: a skipped line is a record that silently left the
	// comparison, which is the drop this tool exists to catch. The reported
	// number is 1-based and counts blank lines, so it matches what an editor
	// shows.
	data := issueOpenA + "\n\n" + `{"_type":"issue","id":"b"` + "\n"
	_, _, err := parse([]byte(data))
	if err == nil {
		t.Fatal("expected a failure on malformed JSON, got nil")
	}
	if !strings.Contains(err.Error(), "line 3:") {
		t.Errorf("expected the 1-based line number 3, got: %v", err)
	}
}

func TestCheck_MalformedJSONIsReportedAgainstTheSideItIsOn(t *testing.T) {
	// Both parse failures reach the caller, and each says which file it read.
	// Reporting a base failure as a head failure would send the author to
	// edit a file that is fine — base comes out of a git ref, not out of
	// this PR's tree.
	const malformed = `{"_type":"issue","id":"a"`

	t.Run("head", func(t *testing.T) {
		err := check([]byte(malformed+"\n"), []byte(issueOpenA+"\n"), "test-base", exemptions{})
		if err == nil {
			t.Fatal("expected a failure, got nil")
		}
		if !strings.Contains(err.Error(), "parse head "+exportPath+": line 1:") {
			t.Errorf("expected the head parse error, got: %v", err)
		}
	})

	t.Run("base", func(t *testing.T) {
		err := check([]byte(issueOpenA+"\n"), []byte(malformed+"\n"), "test-base", exemptions{})
		if err == nil {
			t.Fatal("expected a failure, got nil")
		}
		if !strings.Contains(err.Error(), "parse base "+exportPath+"@test-base: line 1:") {
			t.Errorf("expected the base parse error naming the base label, got: %v", err)
		}
	})
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

// --- the timestamp declaration shape (bd gqlc-j068) -------------------------
// The sha shape reaches only closes whose reason cites a sha, which on the
// export at 92ebb9a7 was 86 of 290 closed beads. A close written by hand — a
// prose reason, no sha anywhere in it — had no declaration bdguard would grant,
// so a wrong close there had to be carried in the export until someone re-closed
// the bead for real. These rows are that gap and the fence around it.

// proseClose is what a hand-written close looks like: a reason that names no
// commit at all, which is what makes the sha shape unusable for it.
const proseClose = "Closed as answered rather than coded; the behaviour was already covered."

func TestCheck_TimestampDeclarationGrantsAProseClose(t *testing.T) {
	base := closedAtBase("a", proseClose, baseClosedAt) + "\n"
	head := reopenedAtHead("a", "in_progress", headFixedAt) + "\n"
	ex := exemptions{
		reopens:       map[string]declaration{"a": {closedAt: baseClosedAt}},
		refContaining: noRefContains,
	}
	if err := check([]byte(head), []byte(base), "test-base", ex); err != nil {
		t.Fatalf("expected pass, got: %v", err)
	}
}

func TestCheck_TimestampDeclarationIsTheSAMEINSTANTNotTheSameBytes(t *testing.T) {
	// A declaration quotes a fact, not a spelling. bd writes UTC, but a
	// timestamp read from another tool can carry an offset, and refusing that
	// would be refusing the right answer for being written differently.
	base := closedAtBase("a", proseClose, "2026-08-17T07:54:46Z") + "\n"
	head := reopenedAtHead("a", "in_progress", headFixedAt) + "\n"
	ex := exemptions{
		reopens:       map[string]declaration{"a": {closedAt: "2026-08-17T09:54:46+02:00"}},
		refContaining: noRefContains,
	}
	if err := check([]byte(head), []byte(base), "test-base", ex); err != nil {
		t.Fatalf("expected pass for the same instant spelled with an offset, got: %v", err)
	}
}

func TestCheck_TimestampDeclarationMustQuoteBase(t *testing.T) {
	// The property that stops this being a bare-id allowlist: base comes out of
	// a git ref, so the entry has to name something already committed there. A
	// timestamp nobody recorded is an assertion the head made alone.
	base := closedAtBase("a", proseClose, baseClosedAt) + "\n"
	head := reopenedAtHead("a", "in_progress", headFixedAt) + "\n"
	ex := exemptions{
		reopens:       map[string]declaration{"a": {closedAt: "2026-08-17T07:54:47Z"}},
		refContaining: noRefContains,
	}
	err := check([]byte(head), []byte(base), "test-base", ex)
	if err == nil {
		t.Fatal("expected failure: the declared timestamp is not base's updated_at")
	}
	if !strings.Contains(err.Error(), "is not base's updated_at") {
		t.Errorf("expected the quoting refusal, got: %v", err)
	}
	// Both values, so the reader can see which one is wrong without opening
	// the export at base by hand.
	if !strings.Contains(err.Error(), "2026-08-17T07:54:47Z") || !strings.Contains(err.Error(), baseClosedAt) {
		t.Errorf("expected both timestamps as the witness, got: %v", err)
	}
}

func TestCheck_TimestampDeclarationStillObeysRecency(t *testing.T) {
	// The revert/correction separator is shared by both shapes. A head record
	// older than base's close is an older export copied forward, whichever way
	// the declaration is written.
	base := closedAtBase("a", proseClose, baseClosedAt) + "\n"
	head := reopenedAtHead("a", "in_progress", "2026-08-16T10:00:00Z") + "\n"
	ex := exemptions{
		reopens:       map[string]declaration{"a": {closedAt: baseClosedAt}},
		refContaining: noRefContains,
	}
	err := check([]byte(head), []byte(base), "test-base", ex)
	if err == nil {
		t.Fatal("expected failure: a record older than base's close is a revert")
	}
	if !strings.Contains(err.Error(), "is not after base") {
		t.Errorf("expected the recency refusal, got: %v", err)
	}
}

func TestCheck_TimestampDeclarationCannotDodgeTheSHAVeto(t *testing.T) {
	// The reason the two shapes are kept disjoint. The sha shape has a veto —
	// a ref containing the closing sha means the close was earned — and the
	// timestamp shape has nothing to apply it to. Left open, an author whose
	// sha declaration was vetoed could quote the timestamp instead and be
	// granted. So a close_reason citing a sha that IS on a ref refuses the
	// timestamp shape outright.
	base := closedAtBase("a", hookClose(closingSHA), baseClosedAt) + "\n"
	head := reopenedAtHead("a", "in_progress", headFixedAt) + "\n"
	ex := exemptions{
		reopens:       map[string]declaration{"a": {closedAt: baseClosedAt}},
		refContaining: refContains("refs/remotes/origin/master"),
	}
	err := check([]byte(head), []byte(base), "test-base", ex)
	if err == nil {
		t.Fatal("expected failure: the timestamp shape must not grant what the sha shape's veto refuses")
	}
	if !strings.Contains(err.Error(), "cannot be granted over that") {
		t.Errorf("expected the veto-dodge refusal, got: %v", err)
	}
	// Named, so the refusal is actionable: the reader needs the sha and the
	// ref, not just the fact of a refusal.
	if !strings.Contains(err.Error(), closingSHA) || !strings.Contains(err.Error(), "refs/remotes/origin/master") {
		t.Errorf("expected the sha and the ref named, got: %v", err)
	}
}

func TestCheck_TimestampDeclarationIsGrantedOverAnOrphanedSHA(t *testing.T) {
	// The other side of the row above, and it is what stops that fence being
	// a blanket ban on the timestamp shape for any reason mentioning a commit.
	// An orphaned sha is exactly the witnessed case (gqlc-rz0l), where the veto
	// stays silent — so the two shapes agree and either may be used.
	base := closedAtBase("a", hookClose(closingSHA), baseClosedAt) + "\n"
	head := reopenedAtHead("a", "in_progress", headFixedAt) + "\n"
	ex := exemptions{
		reopens:       map[string]declaration{"a": {closedAt: baseClosedAt}},
		refContaining: noRefContains,
	}
	if err := check([]byte(head), []byte(base), "test-base", ex); err != nil {
		t.Fatalf("expected pass over an orphaned sha, got: %v", err)
	}
}

func TestCitedSHATokens(t *testing.T) {
	// The veto-dodge fence reads this, so a scanner that found nothing would
	// turn that fence into a pass with no visible symptom.
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{"none", "Closed as answered rather than coded.", nil},
		{"one", hookClose(closingSHA), []string{closingSHA}},
		{"two", "merged as 3dd322dc after aaaaaaa1", []string{"3dd322dc", "aaaaaaa1"}},
		// The delimiting is citesSHA's, so a token embedded in a longer word
		// is not a citation.
		{"embedded in a longer alnum run", "x3dd322dcx", nil},
		{"too short", "closed at 3dd322", nil},
		{"not hex", "closed at 3dd322dg", nil},
		// gqlc-7fn3's shape: an all-decimal token is sha-shaped here on
		// purpose, since about four in a hundred real abbreviations are.
		{"all decimal", "the cache is 1048576 bytes", []string{"1048576"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := citedSHATokens(tt.in)
			if len(got) != len(tt.want) {
				t.Fatalf("citedSHATokens(%q) = %v, want %v", tt.in, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("citedSHATokens(%q)[%d] = %q, want %q", tt.in, i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestCheck_ReopenHintNamesBothShapes(t *testing.T) {
	// The hint is the whole of what an author sees when they hit this. It named
	// only the sha shape, which is how a prose close read as "no remedy exists".
	base := closedAtBase("a", proseClose, baseClosedAt) + "\n"
	head := reopenedAtHead("a", "in_progress", headFixedAt) + "\n"
	err := check([]byte(head), []byte(base), "test-base", exemptions{refContaining: noRefContains})
	if err == nil {
		t.Fatal("expected an undeclared reopen to fail")
	}
	for _, want := range []string{"<bead-id> <closing-sha>", "<bead-id> <base-updated-at>", "RFC3339"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("expected the hint to carry %q, got: %v", want, err)
		}
	}
}

func TestRefuseDeclaredFact_UnparseableTimestampsAreRefused(t *testing.T) {
	// Neither branch is reachable through parseAllowedReopens, which stores
	// only a token that already parsed, nor through a bd export, which writes
	// RFC3339. They are refusals rather than skips because the whole
	// discrimination rests on comparing two instants: a value that will not
	// parse has to stop the grant, not be assumed in either direction.
	t.Run("declared", func(t *testing.T) {
		base := record{Status: "closed", UpdatedAt: baseClosedAt, CloseReason: proseClose}
		got := refuseDeclaredFact(base, declaration{closedAt: "yesterday"}, noRefContains)
		if !strings.Contains(got, "declared timestamp") || !strings.Contains(got, "not RFC3339") {
			t.Errorf("expected the declared-timestamp refusal, got: %q", got)
		}
	})
	t.Run("base", func(t *testing.T) {
		base := record{Status: "closed", UpdatedAt: "not-a-time", CloseReason: proseClose}
		got := refuseDeclaredFact(base, declaration{closedAt: baseClosedAt}, noRefContains)
		if !strings.Contains(got, "base updated_at") || !strings.Contains(got, "not RFC3339") {
			t.Errorf("expected the base-timestamp refusal, got: %q", got)
		}
	})
}
