// bdguard fails when `.beads/issues.jsonl` regresses vs a base ref: dropping
// an issue or reopening one that was closed at base. Additions and new
// closures pass, so `chore(bd)` sync PRs need no exemption. Motivated by
// bd gqlc-v2p (PR #422 pre-fix carried a stale export that would have
// reopened two closed beads and dropped one).
//
// Deleting a bead on purpose (bd gqlc-w29n: a sync bug minted a duplicate
// ghost bead) is declared by listing its id in `.beads/allowed-drops.txt`,
// which exempts that id from the dropped arm alone. That file is read at the
// PR head, so the exemption arrives in the diff a reviewer already reads.
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

const (
	exportPath       = ".beads/issues.jsonl"
	allowedDropsPath = ".beads/allowed-drops.txt"
)

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
	allowedDrops, err := readAllowedDrops(allowedDropsPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", allowedDropsPath, err)
	}
	return check(headBytes, baseBytes, base, allowedDrops)
}

// readAllowedDrops treats an absent file as an empty allowlist: most branches
// delete no bead and carry no such file. Any other read failure is returned
// rather than folded into "absent", so a file that exists but cannot be read
// surfaces as itself instead of as an unexplained drop.
func readAllowedDrops(path string) (map[string]bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	return parseAllowedDrops(data), nil
}

// parseAllowedDrops reads one bead id per line, ignoring blank lines and lines
// whose first non-blank byte is '#'. Ids are matched exactly: an entry exempts
// that one id, with no wildcard or prefix form that could exempt a set.
func parseAllowedDrops(data []byte) map[string]bool {
	allowed := make(map[string]bool)
	for _, line := range bytes.Split(data, []byte("\n")) {
		id := bytes.TrimSpace(line)
		if len(id) == 0 || id[0] == '#' {
			continue
		}
		allowed[string(id)] = true
	}
	return allowed
}

// check is the pure comparator: given both files' bytes, decide whether the
// head regressed vs the base. Kept free of I/O so tests can hit every branch
// with no `git` shellout — the git-boundary of run() is exercised only by the
// end-to-end incident replay against the real repo (see PR body).
//
// allowedDrops exempts ids from the dropped arm only: a bead still present but
// no longer closed is a status regression, not the deletion the list declares.
// A listed id that was not dropped is inert rather than an error — once the
// deletion lands on master the id is absent at base and head alike, so entries
// go stale by design, and failing on them would fail every later PR until
// someone pruned the file.
func check(headBytes, baseBytes []byte, baseLabel string, allowedDrops map[string]bool) error {
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
			if !allowedDrops[id] {
				dropped = append(dropped, id)
			}
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
	if len(dropped) > 0 {
		fmt.Fprintf(&buf, "\nhint: a deliberate deletion is declared by listing the exact id in %s (drops only — a reopen is not exempted there)", allowedDropsPath)
	}
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
