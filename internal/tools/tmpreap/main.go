// Command tmpreap reports what is holding a shared scratch filesystem, and
// reclaims the part of it that is provably abandoned.
//
// It exists because /tmp on this host is a 16G tmpfs shared by every agent
// working on this repo, and nothing reaps agent scratch. Exhaustion does not
// present as a full disk (bd gqlc-osuz): `git worktree add` fails "No space
// left on device" while `df -h` shows gigabytes free, because it is INODES that
// ran out; `just test` reports "build failed" with no error text on a different
// package set each run, because the build harness cannot write either; and the
// agent's own tool process dies. Three agents read those as broken trees before
// anyone read them as a full filesystem.
//
// The reporting half is therefore the valuable half. Both numbers are printed
// on every invocation, inodes first, because a reader who checks only bytes is
// the reader this tool exists for.
//
// Usage:
//
//	tmpreap [-root DIR] -check          # pressure only; non-zero at the fail threshold
//	tmpreap [-root DIR] [-repo DIR]     # pressure, composition, and the reap plan
//	tmpreap [-root DIR] [-repo DIR] -apply
//
// Nothing is deleted without -apply, and -apply archives every text artefact it
// is about to destroy to a tarball outside the scan root first.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "tmpreap: %v\n", err)
		os.Exit(1)
	}
}

// options is the whole surface; every field has a default that is safe to run
// on a machine nobody has configured.
type options struct {
	root     string
	repo     string
	base     string
	maxAge   time.Duration
	apply    bool
	check    bool
	archive  string
	top      int
	warnPct  float64
	failPct  float64
	archiveL archiveLimits
}

func parseOptions(args []string, errOut io.Writer) (options, error) {
	var o options
	fs := flag.NewFlagSet("tmpreap", flag.ContinueOnError)
	fs.SetOutput(errOut)
	fs.StringVar(&o.root, "root", "/tmp", "scratch filesystem to report on")
	fs.StringVar(&o.repo, "repo", ".", "git repository whose worktrees are recognised under -root")
	fs.StringVar(&o.base, "base", "origin/master", "ref a worktree's content must already be equal to before it can be reaped")
	fs.DurationVar(&o.maxAge, "age", 12*time.Hour, "an entry is a candidate only after nothing in it has been modified for this long")
	fs.BoolVar(&o.apply, "apply", false, "archive and then delete the planned entries")
	fs.BoolVar(&o.check, "check", false, "report pressure only, and exit non-zero at the fail threshold")
	fs.StringVar(&o.archive, "archive", "", "tarball to write before deleting (default: $XDG_STATE_HOME/gqlc/tmpreap/<timestamp>.tar.gz)")
	fs.IntVar(&o.top, "top", 15, "how many entries to list per section")
	fs.Float64Var(&o.warnPct, "warn", 85, "usage percentage, of bytes or inodes, at which -check warns")
	fs.Float64Var(&o.failPct, "fail", 95, "usage percentage, of bytes or inodes, at which -check fails")
	fs.Int64Var(&o.archiveL.maxFileBytes, "archive-max-file", 8<<20, "largest single file the archive will take")
	fs.Int64Var(&o.archiveL.maxTotalBytes, "archive-max-total", 2<<30, "largest total input the archive will take")
	if err := fs.Parse(args); err != nil {
		return options{}, err
	}
	if fs.NArg() != 0 {
		return options{}, fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	if o.warnPct > o.failPct {
		return options{}, fmt.Errorf("-warn (%.0f) is above -fail (%.0f), so the warning can never be reached", o.warnPct, o.failPct)
	}
	return o, nil
}

func run(ctx context.Context, args []string, out, errOut io.Writer) error {
	o, err := parseOptions(args, errOut)
	if err != nil {
		return err
	}

	// EvalSymlinks before anything else: `git worktree list` prints resolved
	// paths, and a root spelled differently from the resolved one would match
	// no worktree at all — every worktree would then be classified plain and
	// lose its dirty and landed checks.
	root, err := filepath.EvalSymlinks(o.root)
	if err != nil {
		return fmt.Errorf("resolve -root %s: %w", o.root, err)
	}

	p, err := readPressure(root)
	if err != nil {
		return err
	}
	level, summary := gradePressure(p, o.warnPct, o.failPct)
	pr := &printer{w: out}
	pr.printf("%s\n", summary)
	if o.check {
		if level == pressureFail {
			return errors.Join(pr.err, errors.New("scratch filesystem is past the fail threshold; run `just tmp-report` for what is holding it"))
		}
		return pr.err
	}

	started := time.Now()
	entries, err := plan(ctx, o, root)
	if err != nil {
		return err
	}
	report(pr, o, root, entries, time.Since(started))

	if !o.apply {
		pr.printf("\nnothing was deleted. Re-run with -apply (or `just tmp-reap apply`) to reclaim.\n")
		return pr.err
	}
	return errors.Join(apply(ctx, o, root, entries, pr), pr.err)
}

// printer collects the first write error, so a multi-line report reads as a
// report rather than as an error check per line. The caller checks once.
type printer struct {
	w   io.Writer
	err error
}

func (p *printer) printf(format string, args ...any) {
	if p.err != nil {
		return
	}
	_, p.err = fmt.Fprintf(p.w, format, args...)
}

// plan measures the root and decides over every top-level entry. It performs no
// deletion and is what both the report and the apply path read.
func plan(ctx context.Context, o options, root string) ([]entry, error) {
	worktrees, err := listWorktrees(ctx, o.repo)
	if err != nil {
		return nil, err
	}
	refs, err := procRefs(root)
	if err != nil {
		return nil, err
	}
	cfg := scanConfig{
		root:   root,
		base:   o.base,
		maxAge: o.maxAge,
		now:    time.Now(),
		uid:    os.Getuid(),
	}
	oracle := gitOracle{base: o.base}
	return scanRoot(ctx, cfg, worktrees, oracle.state, heldTopLevel(root, refs))
}

func report(pr *printer, o options, root string, entries []entry, took time.Duration) {
	var reap, retain []entry
	for _, e := range entries {
		if e.reap {
			reap = append(reap, e)
		} else {
			retain = append(retain, e)
		}
	}
	pr.printf("\n%s: %d top-level entries, %s, %s inodes (scanned in %s)\n",
		root, len(entries), humanBytes(totalBytes(entries)), humanCount(totalInodes(entries)), took.Round(time.Millisecond))
	pr.printf("reap threshold: nothing modified for %s, and no live process inside\n", o.maxAge)

	for _, section := range []struct {
		label   string
		entries []entry
	}{{"REAP", reap}, {"RETAIN", retain}} {
		pr.printf("\n%s %d entries, %s, %s inodes\n", section.label, len(section.entries),
			humanBytes(totalBytes(section.entries)), humanCount(totalInodes(section.entries)))
		for _, k := range kindsOf(section.entries) {
			sub := ofKind(section.entries, k)
			pr.printf("  %-13s %4d  %10s  %8s inodes\n", k, len(sub), humanBytes(totalBytes(sub)), humanCount(totalInodes(sub)))
		}
		listTop(pr, section.entries, o.top)
	}
}

// listTop prints the biggest entries of a set with the reason each was decided
// on. Biggest, because the question a reader arrives with is what is holding the
// filesystem, and a set that is only counted is one nobody can act on.
func listTop(pr *printer, entries []entry, n int) {
	sorted := append([]entry(nil), entries...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].bytes != sorted[j].bytes {
			return sorted[i].bytes > sorted[j].bytes
		}
		return sorted[i].path < sorted[j].path
	})
	if len(sorted) > n {
		sorted = sorted[:n]
	}
	for _, e := range sorted {
		pr.printf("    %10s  %8s  %-13s %s\n      %s\n",
			humanBytes(e.bytes), humanCount(e.inodes), e.kind, e.path, e.reason)
	}
}

// apply archives first and deletes second, and re-reads the live process set in
// between: the scan can take a minute over a full tmpfs, and an agent that
// started using a directory during it must not lose it.
func apply(ctx context.Context, o options, root string, entries []entry, pr *printer) error {
	var doomed []entry
	for _, e := range entries {
		if e.reap {
			doomed = append(doomed, e)
		}
	}
	if len(doomed) == 0 {
		pr.printf("\nnothing to reclaim.\n")
		return nil
	}

	refs, err := procRefs(root)
	if err != nil {
		return err
	}
	held := heldTopLevel(root, refs)
	kept := doomed[:0]
	for _, e := range doomed {
		if who, ok := held[filepath.Base(e.path)]; ok {
			pr.printf("skipping %s: a process started using it during the scan (%s)\n", e.path, who)
			continue
		}
		kept = append(kept, e)
	}
	doomed = kept

	dest := o.archive
	if dest == "" {
		dest = defaultArchivePath(time.Now())
	}
	stats, err := archiveEntries(dest, root, doomed, o.archiveL)
	if err != nil {
		return fmt.Errorf("archive: %w", err)
	}
	pr.printf("\narchived %d text file(s), %s of input, to %s\n", stats.files, humanBytes(stats.bytes), dest)
	if stats.truncated {
		return fmt.Errorf("archive stopped at the -archive-max-total limit of %s with entries left to read, so it is "+
			"not a complete record of what would be deleted; raise the limit or narrow -root, and nothing was deleted",
			humanBytes(o.archiveL.maxTotalBytes))
	}

	var failures []string
	var freed, freedInodes int64
	for _, e := range doomed {
		if err := remove(ctx, o.repo, e); err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", e.path, err))
			continue
		}
		freed += e.bytes
		freedInodes += e.inodes
	}
	pr.printf("reclaimed %s and %s inodes across %d entries\n", humanBytes(freed), humanCount(freedInodes), len(doomed)-len(failures))
	if len(failures) != 0 {
		return fmt.Errorf("%d entries could not be removed:\n  %s", len(failures), strings.Join(failures, "\n  "))
	}
	return nil
}

// remove takes a registered worktree out through git, which refuses on a
// worktree that is dirty. That refusal is a second line of defence behind the
// dirty check in classify, so --force is never passed: the two would have to
// fail together to lose work.
func remove(ctx context.Context, repo string, e entry) error {
	if e.kind == kindWorktree {
		if _, err := git(ctx, repo, "worktree", "remove", e.path); err != nil {
			return err
		}
		// The checkout is all that goes. The branch ref and every commit on it
		// live in the shared object store and survive (verified on a fixture in
		// TestGitWorktreeRemoveKeepsCommitsAndRef).
		return nil
	}
	return os.RemoveAll(e.path)
}

func defaultArchivePath(now time.Time) string {
	stateHome := os.Getenv("XDG_STATE_HOME")
	if stateHome == "" {
		stateHome = filepath.Join(os.Getenv("HOME"), ".local", "state")
	}
	return filepath.Join(stateHome, "gqlc", "tmpreap", now.UTC().Format("20060102T150405Z")+".tar.gz")
}

func totalBytes(entries []entry) int64 {
	var n int64
	for _, e := range entries {
		n += e.bytes
	}
	return n
}

func totalInodes(entries []entry) int64 {
	var n int64
	for _, e := range entries {
		n += e.inodes
	}
	return n
}

// kindsOf returns the kinds present, in a fixed order, so two runs over the same
// tree print the same rows in the same places.
func kindsOf(entries []entry) []kind {
	present := make(map[kind]bool, len(entries))
	for _, e := range entries {
		present[e.kind] = true
	}
	var out []kind
	for _, k := range allKinds {
		if present[k] {
			out = append(out, k)
		}
	}
	return out
}

func ofKind(entries []entry, k kind) []entry {
	var out []entry
	for _, e := range entries {
		if e.kind == k {
			out = append(out, e)
		}
	}
	return out
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for x := n / unit; x >= unit; x /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

func humanCount(n int64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 1_000:
		return fmt.Sprintf("%.1fk", float64(n)/1e3)
	default:
		return fmt.Sprintf("%d", n)
	}
}
