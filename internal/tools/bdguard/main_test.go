package main

import (
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
	issueClosedA  = `{"_type":"issue","id":"a","status":"closed"}`
	commentRecord = `{"_type":"comment","id":"c1"}`
)

func TestCheck_CleanAdditivePass(t *testing.T) {
	base := issueOpenA + "\n"
	head := issueOpenA + "\n" + issueOpenB + "\n"
	if err := check([]byte(head), []byte(base), "test-base"); err != nil {
		t.Fatalf("expected pass, got: %v", err)
	}
}

func TestCheck_NewClosurePass(t *testing.T) {
	base := issueOpenA + "\n"
	head := issueClosedA + "\n"
	if err := check([]byte(head), []byte(base), "test-base"); err != nil {
		t.Fatalf("expected pass, got: %v", err)
	}
}

func TestCheck_DroppedIssueFails(t *testing.T) {
	base := issueOpenA + "\n" + issueOpenB + "\n"
	head := issueOpenA + "\n"
	err := check([]byte(head), []byte(base), "test-base")
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
	err := check([]byte(head), []byte(base), "test-base")
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
	if err := check([]byte(head), nil, "test-base"); err != nil {
		t.Fatalf("expected pass, got: %v", err)
	}
}

func TestCheck_FormatDriftInBaseFails(t *testing.T) {
	// Base has 3 non-blank lines but zero recognised "issue" records —
	// simulates bd renaming _type or the value "issue". Silent pass would
	// mean the gate is inert; must fail loud.
	base := commentRecord + "\n" + commentRecord + "\n" + commentRecord + "\n"
	head := issueOpenA + "\n"
	err := check([]byte(head), []byte(base), "test-base")
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
	err := check([]byte(head), []byte(base), "test-base")
	if err == nil {
		t.Fatal("expected format-drift failure, got nil")
	}
	if !strings.Contains(err.Error(), "format drift") {
		t.Errorf("expected format-drift message, got: %v", err)
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
