package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// wtState is everything the reaper asks git about a registered worktree.
type wtState struct {
	dirty        bool
	dirtyDetail  string
	landed       bool
	landedDetail string
}

// worktreeOracle is the seam the classifier reaches git through, so the decision
// table can be tested without a repository and the git predicates can be tested
// without a decision table.
type worktreeOracle func(ctx context.Context, path string) (wtState, error)

// gitOracle answers from a real repository.
type gitOracle struct{ base string }

func (g gitOracle) state(ctx context.Context, path string) (wtState, error) {
	status, err := git(ctx, path, "status", "--porcelain")
	if err != nil {
		return wtState{}, err
	}
	if status != "" {
		return wtState{dirty: true, dirtyDetail: firstLines(status, 3)}, nil
	}
	landed, detail, err := g.landedOn(ctx, path)
	if err != nil {
		return wtState{}, err
	}
	return wtState{landed: landed, landedDetail: detail}, nil
}

// landedOn answers whether every path the worktree's branch touched is already
// present and equal on the base ref.
//
// It is a CONTENT test, not an ancestry one, and that is the whole point. This
// repo squash-merges with auto-delete-branch, so a merged PR head is not an
// ancestor of master, its commits are on no remote ref, and every ancestry or
// "unpushed commits" test calls it stranded. Five month-old branches that
// `git merge-base --is-ancestor` called unmerged were verified fully landed by
// this test (bd gqlc-osuz).
//
// Two git invocations rather than one per path: the touched set is the branch's
// own diff from its merge-base, the differing set is the whole-tree diff against
// base, and the branch has landed exactly when those two sets do not intersect.
// `git diff` has no --pathspec-from-file, so passing the touched set back as a
// pathspec would be an argv-length limit on a branch with enough files.
func (g gitOracle) landedOn(ctx context.Context, path string) (bool, string, error) {
	mergeBase, err := git(ctx, path, "merge-base", g.base, "HEAD")
	if err != nil {
		return false, "", err
	}
	touchedOut, err := git(ctx, path, "diff", "--name-only", mergeBase, "HEAD")
	if err != nil {
		return false, "", err
	}
	differingOut, err := git(ctx, path, "diff", "--name-only", "HEAD", g.base)
	if err != nil {
		return false, "", err
	}

	touched := splitLines(touchedOut)
	differing := make(map[string]bool, len(differingOut))
	for _, p := range splitLines(differingOut) {
		differing[p] = true
	}
	var unlanded []string
	for _, p := range touched {
		if differing[p] {
			unlanded = append(unlanded, p)
		}
	}
	if len(unlanded) == 0 {
		return true, fmt.Sprintf("%d touched path(s), all equal on %s", len(touched), g.base), nil
	}
	sample := unlanded[0]
	if len(unlanded) > 1 {
		sample = fmt.Sprintf("%s and %d more", sample, len(unlanded)-1)
	}
	return false, fmt.Sprintf("%d of %d touched path(s) differ from %s: %s", len(unlanded), len(touched), g.base, sample), nil
}

// listWorktrees returns the resolved path of every worktree the repo has
// registered, including its own. An empty answer is refused rather than
// returned: the main worktree is always in that list, so an empty one means the
// repo was not found — and a classifier handed an empty registry treats every
// worktree under the root as plain scratch.
func listWorktrees(ctx context.Context, repo string) (map[string]bool, error) {
	out, err := git(ctx, repo, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, fmt.Errorf("list worktrees of %s: %w", repo, err)
	}
	return parseWorktreeList(out, repo)
}

// parseWorktreeList is split out because the emptiness refusal below cannot be
// reached through real git — `git worktree list` either errors or names the main
// worktree — and an unreachable guard is an untested one.
func parseWorktreeList(out, repo string) (map[string]bool, error) {
	set := make(map[string]bool)
	for _, reg := range parseWorktreeRegistrations(out) {
		// A registration whose checkout is gone cannot be resolved; it is also
		// not on disk, so it can never match an entry under the root.
		if resolved, err := filepath.EvalSymlinks(reg.path); err == nil {
			set[resolved] = true
		}
		set[filepath.Clean(reg.path)] = true
	}
	if len(set) == 0 {
		return nil, fmt.Errorf("`git worktree list` in %s named no worktree at all, not even the main one, "+
			"so nothing under the scan root could be recognised as a worktree", repo)
	}
	return set, nil
}

// worktreeReg is one registration, with the attributes the -worktrees decision
// table reads. The two that matter beyond the path are `locked` and `prunable`,
// and both can appear with or without a reason — `locked` bare is a lock nobody
// annotated — so each carries a separate "was it present" bool. Reading the
// reason alone would make an unannotated lock indistinguishable from no lock,
// which is the direction that removes someone's held worktree.
type worktreeReg struct {
	path       string
	head       string
	branch     string
	detached   bool
	locked     string
	isLocked   bool
	prunable   string
	isPrunable bool
}

// at names what the worktree has checked out, for the report. Several
// registrations in this town are on detached HEADs — a parked seat is
// deliberately detached — so a report that could only name branches would be
// silent about exactly those.
func (w worktreeReg) at() string {
	switch {
	case w.branch != "":
		return strings.TrimPrefix(w.branch, "refs/heads/")
	case w.detached && len(w.head) >= 8:
		return "detached at " + w.head[:8]
	case w.detached:
		return "detached"
	default:
		return "no checkout"
	}
}

// parseWorktreeRegistrations reads `git worktree list --porcelain`. The format
// is one blank-line-separated stanza per worktree, each opening with a
// `worktree <path>` line; git documents the main worktree as the first stanza,
// which is how the decision table recognises it.
//
// Unknown attribute lines are ignored rather than refused: git has added
// attributes to this format before, and a reaper that fails to parse is a
// reaper that reports nothing, while one that ignores an attribute it does not
// know still holds everything its own gates hold.
func parseWorktreeRegistrations(out string) []worktreeReg {
	var regs []worktreeReg
	for _, line := range splitLines(out) {
		key, value, _ := strings.Cut(line, " ")
		switch key {
		case "worktree":
			regs = append(regs, worktreeReg{path: filepath.Clean(value)})
			continue
		case "":
			continue
		}
		if len(regs) == 0 {
			continue
		}
		cur := &regs[len(regs)-1]
		switch key {
		case "HEAD":
			cur.head = value
		case "branch":
			cur.branch = value
		case "detached":
			cur.detached = true
		case "locked":
			cur.isLocked, cur.locked = true, value
		case "prunable":
			cur.isPrunable, cur.prunable = true, value
		}
	}
	return regs
}

// listWorktreeRegistrations is listWorktrees' sibling for the -worktrees mode,
// which decides over the registry itself rather than over a scan root's
// children.
func listWorktreeRegistrations(ctx context.Context, repo string) ([]worktreeReg, error) {
	out, err := git(ctx, repo, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, fmt.Errorf("list worktrees of %s: %w", repo, err)
	}
	return parseRegistry(out, repo)
}

// parseRegistry is split out for the same reason parseWorktreeList is, and it
// carries the same refusal: git always names the main worktree, so an empty
// list means the repository was not found, and a plan over an empty registry
// reports success over nothing. Real git can produce neither an error nor an
// empty list, so the refusal is unreachable from listWorktreeRegistrations —
// and an unreachable guard is an untested one.
func parseRegistry(out, repo string) ([]worktreeReg, error) {
	regs := parseWorktreeRegistrations(out)
	if len(regs) == 0 {
		return nil, fmt.Errorf("`git worktree list` in %s named no worktree at all, not even the main one, "+
			"so there is no registry to decide over", repo)
	}
	return regs, nil
}

func git(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = err.Error()
		}
		return "", fmt.Errorf("git %s in %s: %w (%s)", strings.Join(args, " "), dir, errGit, detail)
	}
	return strings.TrimRight(stdout.String(), "\n"), nil
}

var errGit = errors.New("git failed")

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func firstLines(s string, n int) string {
	lines := splitLines(s)
	if len(lines) <= n {
		return strings.Join(lines, "; ")
	}
	return strings.Join(lines[:n], "; ") + fmt.Sprintf("; and %d more", len(lines)-n)
}
