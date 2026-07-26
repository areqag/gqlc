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
	head, err := parse(headBytes)
	if err != nil {
		return fmt.Errorf("parse head %s: %w", exportPath, err)
	}

	baseBytes, err := showAtRef(ctx, base, exportPath)
	if err != nil {
		// Missing at base is legitimate (repo before bd landed) — treat as empty.
		if errors.Is(err, errPathAbsentAtRef) {
			baseBytes = nil
		} else {
			return fmt.Errorf("read base %s@%s: %w", exportPath, base, err)
		}
	}
	baseIssues, err := parse(baseBytes)
	if err != nil {
		return fmt.Errorf("parse base %s@%s: %w", exportPath, base, err)
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
	fmt.Fprintf(&buf, "bdguard: %s regressed against %s\n", exportPath, base)
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
// types (bd may emit others in future) are ignored so this tool doesn't gate
// on schema drift outside its scope. Blank lines are skipped.
func parse(data []byte) (map[string]string, error) {
	out := make(map[string]string)
	for i, line := range bytes.Split(data, []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var r record
		if err := json.Unmarshal(line, &r); err != nil {
			return nil, fmt.Errorf("line %d: %w", i+1, err)
		}
		if r.Type != "issue" {
			continue
		}
		if r.ID == "" {
			return nil, fmt.Errorf("line %d: issue record with empty id", i+1)
		}
		out[r.ID] = r.Status
	}
	return out, nil
}

var errPathAbsentAtRef = errors.New("path absent at ref")

func showAtRef(ctx context.Context, ref, path string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", "show", ref+":"+path)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		// `git show` prints "fatal: path '...' does not exist in '<ref>'" for
		// missing paths, and "fatal: bad revision '...'" for unknown refs.
		// Distinguish so a missing base file is graceful but a missing ref
		// (usually shallow-clone) is loud.
		if bytes.Contains(stderr.Bytes(), []byte("does not exist")) {
			return nil, errPathAbsentAtRef
		}
		return nil, fmt.Errorf("git show %s:%s: %w: %s", ref, path, err, stderr.String())
	}
	return stdout.Bytes(), nil
}
