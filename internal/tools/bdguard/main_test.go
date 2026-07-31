package main

import (
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Tests target check() directly — no git shellout, no chdir, no tempdir. The
// git boundary in run() is exercised only by the end-to-end incident replay
// against the real repo (see PR body). Motivation: earlier iterations tried
// to stage a real git repo per test, and the tempdir git calls occasionally
// landed in the outer worktree despite GIT_CEILING_DIRECTORIES — pointless
// blast radius for testing a pure comparator.

const (
	issueOpenA    = `{"_type":"issue","id":"a","status":"open"}`
	issueOpenB    = `{"_type":"issue","id":"b","status":"open"}`
	issueOpenC    = `{"_type":"issue","id":"c","status":"open"}`
	issueClosedA  = `{"_type":"issue","id":"a","status":"closed"}`
	issueEmptyID  = `{"_type":"issue","id":"","status":"open"}`
	commentRecord = `{"_type":"comment","id":"c1"}`
)

func TestCheck_CleanAdditivePass(t *testing.T) {
	base := issueOpenA + "\n"
	head := issueOpenA + "\n" + issueOpenB + "\n"
	if err := check([]byte(head), []byte(base), "test-base", nil); err != nil {
		t.Fatalf("expected pass, got: %v", err)
	}
}

func TestCheck_NewClosurePass(t *testing.T) {
	base := issueOpenA + "\n"
	head := issueClosedA + "\n"
	if err := check([]byte(head), []byte(base), "test-base", nil); err != nil {
		t.Fatalf("expected pass, got: %v", err)
	}
}

func TestCheck_DroppedIssueFails(t *testing.T) {
	base := issueOpenA + "\n" + issueOpenB + "\n"
	head := issueOpenA + "\n"
	err := check([]byte(head), []byte(base), "test-base", nil)
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
	err := check([]byte(head), []byte(base), "test-base", nil)
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
	if err := check([]byte(head), nil, "test-base", nil); err != nil {
		t.Fatalf("expected pass, got: %v", err)
	}
}

func TestCheck_FormatDriftInBaseFails(t *testing.T) {
	// Base has 3 non-blank lines but zero recognised "issue" records —
	// simulates bd renaming _type or the value "issue". Silent pass would
	// mean the gate is inert; must fail loud.
	base := commentRecord + "\n" + commentRecord + "\n" + commentRecord + "\n"
	head := issueOpenA + "\n"
	err := check([]byte(head), []byte(base), "test-base", nil)
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
	err := check([]byte(head), []byte(base), "test-base", nil)
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
	if err := check([]byte(head), []byte(base), "test-base", map[string]bool{"b": true}); err != nil {
		t.Fatalf("expected pass, got: %v", err)
	}
}

func TestCheck_AllowlistedReopenStillFails(t *testing.T) {
	// The allowlist declares a deletion, not a status change: a reopen with the
	// same id in the list must still fail.
	base := issueClosedA + "\n"
	head := issueOpenA + "\n"
	err := check([]byte(head), []byte(base), "test-base", map[string]bool{"a": true})
	if err == nil {
		t.Fatal("expected failure, got nil")
	}
	if !strings.Contains(err.Error(), "a (base=closed, head=open)") {
		t.Errorf("expected reopened id 'a' in message, got: %v", err)
	}
	if strings.Contains(err.Error(), "dropped") {
		t.Errorf("reopen must not be reported as a drop, got: %v", err)
	}
}

func TestCheck_AllowlistSubtractsOnlyListedDrops(t *testing.T) {
	base := issueOpenA + "\n" + issueOpenB + "\n" + issueOpenC + "\n"
	head := issueOpenA + "\n"
	err := check([]byte(head), []byte(base), "test-base", map[string]bool{"b": true})
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
	err := check([]byte(head), []byte(base), "test-base", map[string]bool{"*": true})
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
	if err := check([]byte(head), []byte(base), "test-base", map[string]bool{"long-gone": true}); err != nil {
		t.Fatalf("expected pass, got: %v", err)
	}
}

func TestCheck_AllowlistDoesNotMaskFormatDrift(t *testing.T) {
	// Head parses to zero issue records, so every base id looks dropped. With
	// 'a' listed, subtraction alone would turn bd format drift into a pass.
	base := issueOpenA + "\n"
	head := commentRecord + "\n" + commentRecord + "\n"
	err := check([]byte(head), []byte(base), "test-base", map[string]bool{"a": true})
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
	err := check([]byte(head), []byte(base), "test-base", nil)
	if err == nil {
		t.Fatal("expected failure, got nil")
	}
	if !strings.Contains(err.Error(), "passive bd export") {
		t.Errorf("expected the existing sync hint, got: %v", err)
	}
	if !strings.Contains(err.Error(), allowedDropsPath) {
		t.Errorf("expected %s named as the remedy, got: %v", allowedDropsPath, err)
	}
}

func TestCheck_ReopenHintOmitsAllowlist(t *testing.T) {
	// Pointing a reopen at the allowlist would advertise a remedy that does
	// not work there.
	base := issueClosedA + "\n"
	head := issueOpenA + "\n"
	err := check([]byte(head), []byte(base), "test-base", nil)
	if err == nil {
		t.Fatal("expected failure, got nil")
	}
	if strings.Contains(err.Error(), allowedDropsPath) {
		t.Errorf("reopen-only failure must not name %s, got: %v", allowedDropsPath, err)
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

// The three readAllowedDrops tests take a path argument, so they need no
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
	if len(got) != 1 || got["a"] != "open" {
		t.Errorf("issues = %v, want {a:open}", got)
	}
}
