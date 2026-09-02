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
//	tmpreap [-root DIR] [-repo DIR] -apply -apply-above PCT   # the timer's form
//	tmpreap -worktrees [-repo DIR]      # the registry, not a scan root
//	tmpreap -worktrees [-repo DIR] -apply
//
// -worktrees is a second mode over a different population, and the reason it
// lives in this command rather than beside it is that every hard question was
// already here and tested: the /proc occupancy scan, the content-on-base
// landing test, and a decision table whose every gate fails toward retention.
// It decides over the worktrees -repo has REGISTERED, excludes the main
// checkout and the permanent seats, and removes through `git worktree remove`
// — never with --force, never with os.RemoveAll. See worktrees.go, which
// carries the incident that shaped it.
//
// Nothing is deleted without -apply, and with -apply-above nothing is even
// SCANNED until the filesystem is at or past PCT in whichever of bytes and
// inodes is fuller. That gate is what lets an unattended cadence run this: see
// `just tmp-reap-cadence`, which km's guard sweep invokes once per tick.
//
// -apply archives regular text files
// under -archive-max-file to a tarball outside the scan root first, up to a
// total of -archive-max-total, then reports what it could not archive: every
// dropped regular file is counted, in a category that says why, and the first
// pathsListed of each category are named with the remainder disclosed as a
// count. Reaching the total budget is the one drop that is not merely reported
// — it leaves entries unread, so the deletion is refused rather than run over
// an incomplete record. The rest are unrecoverable once the deletion runs, so
// a run that does not say so is a run that lied. Two things go unreported, and
// neither loses readable content: both are absences of a report line within
// what the tool actually removes or attempts. A non-regular entry — symlink,
// FIFO, socket — inside a directory being removed is skipped by the walk
// before the archive sees it and deleted with its directory, named by no
// report line of its own; removing a symlink removes only the link, never its
// target. A non-regular entry at the top of the scan root is never a candidate
// for removal at all: the scan retains it outright. And a regular file that
// vanished between the walk and the read goes unreported because there is
// nothing left to delete by then.
//
// -apply additionally refuses any -root that is not one of the two directories
// compiled in as scratch (/tmp and /var/tmp — the environment does not get a
// vote, see scratchCandidates), and any root on the same path chain as the
// user's home directory. -root is a positional argument of `just tmp-reap`, and
// the decision table happily REAPs Downloads and Pictures.
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
	root    string
	repo    string
	base    string
	maxAge  time.Duration
	apply   bool
	check   bool
	archive string
	top     int
	warnPct float64
	failPct float64
	// applyAbove gates -apply on measured pressure, and is what makes this tool
	// safe to put on a timer: below the threshold the run stops at the statfs and
	// never walks, so a cadence costs microseconds and deletes nothing on the
	// overwhelming majority of ticks. A negative value disables the gate, which
	// is the default and is what every hand-typed `just tmp-reap apply` gets —
	// an operator who typed the word apply has already decided.
	applyAbove float64
	// worktrees selects the registry mode. It is a mode and not a filter: the
	// population it decides over is `git worktree list`, which has no -root and
	// no filesystem to measure the pressure of.
	worktrees bool
	archiveL  archiveLimits
	// procDir is the procfs mount the in-use scan reads. It is not a flag: an
	// operator has no reason to move it, and a wrong value silently empties the
	// held set, which reads exactly like "nothing is in use". It exists so plan
	// and apply can be shown to fail CLOSED when the process table is
	// unreadable. procRefsIn already had that seam; its two callers did not, so
	// deleting either fail-closed clause left the whole suite green — both
	// mutations measured SURVIVED on 2026-08-23 (bd gqlc-2459).
	procDir string
}

// defaultArchiveMaxFile bounds one file's contribution to the archive, and so
// bounds this process's peak memory: takeFile reads a whole file before it
// writes it, one at a time.
//
// It is 64 MiB and not the 8 MiB this shipped with because 8 MiB sits inside
// the working size of the artefact the archive exists for. Measured read-only
// on the live /tmp (bd gqlc-osuz round 2, 2026-08-19): the largest agent
// .output logs there are 6.6 MiB, 79% of the old cap, mid-session and still
// being written; 291 text files sit between 1 and 8 MiB; 47 files exceed 8 MiB
// of which 46 are binary; and the single text file above it is 24.2 MiB, three
// times the cap. Nothing on that filesystem is text above 64 MiB.
//
// The 24.2 MiB file is in an entry the decision table RETAINs today, for an
// unrelated reason (it holds a nested .git), so this is a measurement of file
// sizes on the filesystem and not of losses already incurred.
//
// The aggregate -archive-max-total, which IS refusal-worthy, is unchanged and
// remains the real ceiling on the tarball.
const defaultArchiveMaxFile = 64 << 20

func parseOptions(args []string, errOut io.Writer) (options, error) {
	// applyAbove is NOT initialised here: flag.Float64Var writes its own default
	// over the field as it registers, so a value set here would be dead and would
	// read as the authority on the default. The default is the -1 below.
	o := options{procDir: "/proc"}
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
	fs.Float64Var(&o.applyAbove, "apply-above", -1, "with -apply, reap only when bytes or inodes are at or past this percentage (negative: always reap)")
	fs.BoolVar(&o.worktrees, "worktrees", false, "decide over the worktrees -repo has registered instead of over -root's children")
	fs.Int64Var(&o.archiveL.maxFileBytes, "archive-max-file", defaultArchiveMaxFile, "largest single file the archive will take; a text file above it is reported as unrecoverable and deleted anyway")
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
	// Refused rather than ignored. A caller who wrote -apply-above wrote it as a
	// brake, and a brake silently absent from a run that does not delete anyway
	// is harmless — but the same command line one word later is `-apply
	// -apply-above 75`, and a reader who has seen the brake accepted without
	// -apply has no reason to check the order. The cadence recipe passes both.
	if o.applyAbove >= 0 && !o.apply {
		return options{}, fmt.Errorf("-apply-above %.0f was given without -apply, so it gates nothing", o.applyAbove)
	}
	if o.applyAbove > 100 {
		return options{}, fmt.Errorf("-apply-above %.0f can never be reached: a filesystem does not exceed 100%%", o.applyAbove)
	}
	// Refused rather than ignored, the same call as -apply-above above and for
	// the same reason: a flag accepted in a mode that never reads it teaches a
	// reader it was honoured. -root is the sharpest of them — someone who
	// writes `-worktrees -root /tmp` has said which population they mean, and
	// this mode would silently decide over a different one.
	if o.worktrees {
		var dead []string
		fs.Visit(func(f *flag.Flag) {
			switch f.Name {
			case "root", "check", "apply-above", "archive", "archive-max-file", "archive-max-total", "top", "warn", "fail":
				dead = append(dead, "-"+f.Name)
			}
		})
		if len(dead) != 0 {
			return options{}, fmt.Errorf("-worktrees does not read %s: it decides over the worktrees -repo has "+
				"registered, which have no scan root and no filesystem pressure, so %s gates nothing here",
				strings.Join(dead, ", "), pluralGates(dead))
		}
	}
	return o, nil
}

func pluralGates(dead []string) string {
	if len(dead) == 1 {
		return dead[0]
	}
	return "each of them"
}

func run(ctx context.Context, args []string, out, errOut io.Writer) error {
	o, err := parseOptions(args, errOut)
	if err != nil {
		return err
	}

	// Before the root is resolved and before anything is measured: this mode
	// has no root and no filesystem of its own to statfs. Its population is the
	// registry, which lives wherever the repository put it.
	if o.worktrees {
		return runWorktrees(ctx, o, out)
	}

	// EvalSymlinks before anything else: `git worktree list` prints resolved
	// paths, and a root spelled differently from the resolved one would match
	// no worktree at all — every worktree would then be classified plain and
	// lose its dirty and landed checks.
	//
	// Abs first because EvalSymlinks does not absolutise: it returned a
	// relative -root still relative, which then matched no scratch directory
	// and was refused with a message saying the directory was not scratch. The
	// directory was; the spelling was all that was wrong (bd gqlc-cxhw).
	// Everything downstream compares against absolute paths, so this is where
	// the spelling stops mattering — and the refusal below now names the
	// directory it judged rather than the abbreviation the operator typed.
	abs, err := filepath.Abs(o.root)
	if err != nil {
		return fmt.Errorf("resolve -root %s: %w", o.root, err)
	}
	root, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return fmt.Errorf("resolve -root %s: %w", o.root, err)
	}
	// Only the deleting mode is constrained, and it is constrained here rather
	// than beside the delete loop so a mistyped root costs milliseconds instead
	// of a full walk. Reporting stays available over any directory: that is what
	// `just tmp-report` and the `check-tmp` gate are, and narrowing them would
	// retire the half of this tool that pays.
	if o.apply {
		if err := refuseNonScratchRoot(root); err != nil {
			return err
		}
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

	// The cadence's brake, and it sits ABOVE the walk deliberately: a timer that
	// scanned a 500k-inode filesystem every quarter hour to decide it had nothing
	// to do would be its own load. Below the threshold this returns after one
	// statfs, having opened nothing.
	//
	// It is also the fail-closed half. Everything that can go wrong before this
	// point — an unresolvable root, a filesystem with no inode table, a statfs
	// refusal — has already returned an error, so there is no path on which an
	// unmeasured filesystem is read as "past the threshold" and reaped. The
	// dangerous shape for an unattended reaper is deleting because it could not
	// measure, and the measurement is the first thing this program does.
	if o.applyAbove >= 0 {
		worst, currency := p.worst()
		if worst < o.applyAbove {
			pr.printf("\nunder the %.0f%% reap threshold — %s are the fuller currency at %.0f%%. Nothing was scanned and nothing was deleted.\n",
				o.applyAbove, currency, worst)
			return pr.err
		}
		pr.printf("\nat or past the %.0f%% reap threshold — %s are %.0f%% used. Reaping.\n", o.applyAbove, currency, worst)
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
	refs, err := procRefsIn(o.procDir, root)
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

// apply archives first and deletes second, and re-reads the live process set
// immediately before the delete loop: an agent that started using a directory
// after the plan was taken must not lose it.
//
// The re-read used to sit before the archive, which protected the scan and not
// the archive — and the archive is the long half. Writing a tarball over 1.4 GiB
// takes minutes; a full-tmpfs scan takes about a minute. So the window a
// pre-archive read leaves open is the larger of the two (bd gqlc-kg5i). Reading
// last shrinks the window to the delete loop itself.
//
// The cost is that the tarball is written over a set slightly larger than what
// is deleted: an entry claimed during the archive is archived and then kept.
// That is the safe direction — a spare copy of a directory that survives, rather
// than a deletion with no copy — and it is what
// TestApply_ReReadsTheProcessTableAfterArchivingNotBefore pins, because the
// tarball's contents are the only externally visible witness of the ordering.
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

	dest := o.archive
	if dest == "" {
		dest = defaultArchivePath(time.Now())
	}
	stats, err := archiveEntries(dest, root, doomed, o.archiveL)
	if err != nil {
		return fmt.Errorf("archive: %w", err)
	}
	pr.printf("\narchived %d text file(s), %s of input, to %s\n", stats.files, humanBytes(stats.bytes), dest)
	reportUnarchived(pr, stats, o.archiveL)
	if stats.truncated {
		return fmt.Errorf("archive stopped at the -archive-max-total limit of %s with entries left to read, so it is "+
			"not a complete record of what would be deleted; raise the limit or narrow -root, and nothing was deleted",
			humanBytes(o.archiveL.maxTotalBytes))
	}

	// The last thing before the first deletion. Fail-closed: an unreadable
	// process table yields an empty held set, which is indistinguishable from
	// "nothing is in use" and would delete every planned entry.
	refs, err := procRefsIn(o.procDir, root)
	if err != nil {
		return err
	}
	held := heldTopLevel(root, refs)
	kept := doomed[:0]
	for _, e := range doomed {
		if who, ok := held[filepath.Base(e.path)]; ok {
			pr.printf("skipping %s: a process started using it after it was planned (%s)\n", e.path, who)
			continue
		}
		kept = append(kept, e)
	}
	doomed = kept

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

// reportUnarchived names what the archive did not take, immediately after the
// line saying what it did. Those files are deleted moments later with no copy
// anywhere, and an archive that is silently partial is worse than no archive:
// it is the one a reader believes.
//
// Every category prints the same three things — a count of all of them, their
// total size, and the first pathsListed of them by name followed by how many
// were not named. The header is printed by the first non-empty category rather
// than by a test over all of them, so a category cannot be present without it
// and it cannot appear over nothing: a warning printed on every clean run is one
// nobody reads on the run that matters.
//
// Disclosure and not refusal, unlike the aggregate truncation above. One stray
// oversize log would otherwise block every reclamation, and the moment anyone
// runs this is the moment the filesystem is already full — a gate that fires
// exactly then is a gate that gets bypassed with -archive-max-file 1e18.
func reportUnarchived(pr *printer, stats archiveStats, lim archiveLimits) {
	headed := false
	for _, cat := range []struct {
		d    drop
		noun string
		why  string
	}{
		{stats.large, "text file(s)", fmt.Sprintf(", over -archive-max-file (%s) — raise it and re-run to keep these", humanBytes(lim.maxFileBytes))},
		{stats.binary, "binary file(s)", " — this archive takes text only, by design"},
		{stats.unreadable, "unreadable file(s)", " — present but unreadable, which is a permission or I/O failure and not a file that vanished; the deletion takes them anyway"},
	} {
		if cat.d.files == 0 {
			continue
		}
		if !headed {
			pr.printf("NOT archived, and therefore unrecoverable once deleted:\n")
			headed = true
		}
		pr.printf("  %d %s, %s%s\n", cat.d.files, cat.noun, humanBytes(cat.d.bytes), cat.why)
		for _, p := range cat.d.paths {
			pr.printf("      %s\n", p)
		}
		if more := cat.d.files - len(cat.d.paths); more > 0 {
			pr.printf("      ... and %d more\n", more)
		}
	}
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
