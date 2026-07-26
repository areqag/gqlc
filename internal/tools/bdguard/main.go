// bdguard fails when `.beads/issues.jsonl` regresses vs a base ref: dropping
// an issue or reopening one that was closed at base. Additions and new
// closures pass, so `chore(bd)` sync PRs need no exemption. Motivated by
// bd gqlc-v2p (PR #422 pre-fix carried a stale export that would have
// reopened two closed beads and dropped one).
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"sort"
)

const exportPath = ".beads/issues.jsonl"

type record struct {
	Type   string `json:"_type"`
	ID     string `json:"id"`
	Status string `json:"status"`
}

func main() {
	base := flag.String("base", "", "git ref to compare against (e.g. origin/master or a merge-base sha)")
	flag.Parse()
	if *base == "" {
		fmt.Fprintln(os.Stderr, "bdguard: -base is required")
		os.Exit(2)
	}
	if err := run(context.Background(), *base); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, base string) error {
	headBytes, err := os.ReadFile(exportPath)
	if err != nil {
		return fmt.Errorf("read head %s: %w", exportPath, err)
	}
	baseBytes, err := showAtRef(ctx, base, exportPath)
	if err != nil {
		if errors.Is(err, errPathAbsentAtRef) {
			// Missing at base is legitimate (repo before bd landed) — empty.
			baseBytes = nil
		} else {
			return fmt.Errorf("read base %s@%s: %w", exportPath, base, err)
		}
	}
	return check(headBytes, baseBytes, base)
}

// check is the pure comparator: given both files' bytes, decide whether the
// head regressed vs the base. Kept free of I/O so tests can hit every branch
// with no `git` shellout — the git-boundary of run() is exercised only by the
// end-to-end incident replay against the real repo (see PR body).
func check(headBytes, baseBytes []byte, baseLabel string) error {
	head, headLines, err := parse(headBytes)
	if err != nil {
		return fmt.Errorf("parse head %s: %w", exportPath, err)
	}
	if headLines > 0 && len(head) == 0 {
		return fmt.Errorf("bdguard: head %s has %d non-blank lines but zero issue records — suspected bd format drift (rename of _type or of the \"issue\" tag). Refusing to run: a silent pass here would be indistinguishable from a clean export", exportPath, headLines)
	}
	baseIssues, baseLines, err := parse(baseBytes)
	if err != nil {
		return fmt.Errorf("parse base %s@%s: %w", exportPath, baseLabel, err)
	}
	if baseLines > 0 && len(baseIssues) == 0 {
		return fmt.Errorf("bdguard: base %s@%s has %d non-blank lines but zero issue records — suspected bd format drift (rename of _type or of the \"issue\" tag). Refusing to run: a silent pass here would be indistinguishable from a clean export", exportPath, baseLabel, baseLines)
	}

	var dropped, reopened []string
	for id, baseStatus := range baseIssues {
		headStatus, ok := head[id]
		if !ok {
			dropped = append(dropped, id)
			continue
		}
		if baseStatus == "closed" && headStatus != "closed" {
			reopened = append(reopened, fmt.Sprintf("%s (base=closed, head=%s)", id, headStatus))
		}
	}
	if len(dropped) == 0 && len(reopened) == 0 {
		return nil
	}

	sort.Strings(dropped)
	sort.Strings(reopened)
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "bdguard: %s regressed against %s\n", exportPath, baseLabel)
	if len(dropped) > 0 {
		fmt.Fprintf(&buf, "  dropped issues (present at base, absent at head):\n")
		for _, id := range dropped {
			fmt.Fprintf(&buf, "    - %s\n", id)
		}
	}
	if len(reopened) > 0 {
		fmt.Fprintf(&buf, "  reopened issues (closed at base, not closed at head):\n")
		for _, id := range reopened {
			fmt.Fprintf(&buf, "    - %s\n", id)
		}
	}
	fmt.Fprintf(&buf, "hint: %s is a passive bd export; if a real sync, run bd commands and stage only that file", exportPath)
	return errors.New(buf.String())
}

// parse extracts {id: status} for _type=="issue" records. Non-issue record
// types (bd may emit others) are ignored. Blank lines are skipped.
// A non-empty input yielding zero issue records is reported via nonZeroLines
// so the caller can fail loud on format drift (bd renames _type or "issue"),
// rather than silently returning "no regressions" — this whole tool exists to
// catch silently-passing gates.
func parse(data []byte) (issues map[string]string, nonZeroLines int, err error) {
	issues = make(map[string]string)
	for i, line := range bytes.Split(data, []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		nonZeroLines++
		var r record
		if err := json.Unmarshal(line, &r); err != nil {
			return nil, 0, fmt.Errorf("line %d: %w", i+1, err)
		}
		if r.Type != "issue" {
			continue
		}
		if r.ID == "" {
			return nil, 0, fmt.Errorf("line %d: issue record with empty id", i+1)
		}
		issues[r.ID] = r.Status
	}
	return issues, nonZeroLines, nil
}

var errPathAbsentAtRef = errors.New("path absent at ref")

func showAtRef(ctx context.Context, ref, path string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", "show", ref+":"+path)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		// `git show` prints either "fatal: path '...' does not exist in '<ref>'"
		// (path never existed at that commit) or "fatal: path '...' exists on
		// disk, but not in '<ref>'" (path exists in the worktree but not the
		// commit). Both mean "absent at ref" for our purposes. Anything else
		// (unknown ref from a shallow clone emits "fatal: invalid object
		// name", but we don't match on it — we fall through and bubble it up
		// loud rather than silently classifying it as absent).
		msg := stderr.Bytes()
		if bytes.Contains(msg, []byte("does not exist in")) || bytes.Contains(msg, []byte("exists on disk, but not in")) {
			return nil, errPathAbsentAtRef
		}
		return nil, fmt.Errorf("git show %s:%s: %w: %s", ref, path, err, stderr.String())
	}
	return stdout.Bytes(), nil
}
