// Command modscope answers the three module-scoped questions this repo's gates
// ask of a checkout: which Go modules it holds, which directories of each hold
// Go files, and which build tags those files constrain themselves by.
//
// It exists because those answers were derived more than once — in `just vuln`'s
// sweep and, by hand, in the fence beside it — and a fact derived twice is a
// fact that can disagree with itself the day a third module appears (bd
// gqlc-oxne).
//
// Every answer is GRADED before it is printed. An empty module set, a module
// with no directory holding a Go file, or a GOOS/GOARCH table that came back
// short is an error rather than an empty line, because a gate handed an empty
// measurement compares two empty sets and reports success over nothing — this
// repo's dominant defect class, and the one `just vuln`'s directory-coverage
// postcondition was itself an instance of (bd gqlc-s3lt).
//
// Usage:
//
//	modscope [-root DIR] modules       # every module's path relative to DIR
//	modscope [-root DIR] dirs MODULE   # every directory of MODULE holding a .go file
//	modscope [-root DIR] tags MODULE   # every custom build tag MODULE's files ask for
package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"go/build/constraint"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "modscope: %v\n", err)
		os.Exit(1)
	}
}

const usage = "usage: modscope [-root DIR] modules|dirs MODULE|tags MODULE"

// run parses argv by hand rather than through flag, because the only flag is
// -root and a flag package misparse would surface as a usage error at the far
// end of a shell pipeline rather than here.
func run(ctx context.Context, args []string, out io.Writer) error {
	root := "."
	for len(args) > 0 && strings.HasPrefix(args[0], "-") {
		name, value, inline := strings.Cut(args[0], "=")
		if name != "-root" && name != "--root" {
			return fmt.Errorf("unknown flag %q\n%s", args[0], usage)
		}
		if !inline {
			if len(args) < 2 {
				return fmt.Errorf("-root needs a directory\n%s", usage)
			}
			value, args = args[1], args[1:]
		}
		root, args = value, args[1:]
	}
	if len(args) == 0 {
		return errors.New(usage)
	}
	switch args[0] {
	case "modules":
		if len(args) != 1 {
			return errors.New(usage)
		}
		mods, err := discover(root)
		if err != nil {
			return err
		}
		return writeLines(out, mods)
	case "dirs":
		if len(args) != 2 {
			return errors.New(usage)
		}
		dirs, err := moduleGoDirs(ctx, root, args[1])
		if err != nil {
			return err
		}
		return writeLines(out, dirs)
	case "tags":
		if len(args) != 2 {
			return errors.New(usage)
		}
		platforms, err := platformTerms(ctx)
		if err != nil {
			return err
		}
		tags, err := moduleTags(ctx, root, args[1], platforms)
		if err != nil {
			return err
		}
		return writeLines(out, tags)
	default:
		return fmt.Errorf("unknown command %q\n%s", args[0], usage)
	}
}

// writeLines prints one item per line and reports the first write that failed.
// Checked rather than fired and forgotten: every caller of this program reads
// its stdout as a set, and a write that stopped part way through is a set that
// came back short — a caller then compares against fewer directories, or scans
// under fewer tags, and nothing says so. That is the defect this program exists
// to refuse, arriving through the door it leaves by.
func writeLines(out io.Writer, items []string) error {
	for _, item := range items {
		if _, err := fmt.Fprintln(out, item); err != nil {
			return fmt.Errorf("writing %q: %w", item, err)
		}
	}
	return nil
}

// skipName reports whether an entry of that name is outside the reach of `go
// list ./...`, which is govulncheck's own scope. Keeping one predicate for it
// is what lets the directories found here and the directories `go list`
// matched be compared as answers to the same question.
func skipName(name string) bool {
	return strings.HasPrefix(name, ".") ||
		strings.HasPrefix(name, "_") ||
		name == "testdata" ||
		name == "vendor"
}

// discover returns every Go module under root, as a slash path relative to
// root, sorted, with "." for the root module itself.
//
// Both failures are graded rather than returned as an empty list: a checkout
// with no go.mod is discovery broken, and a checkout whose root is not a module
// means every caller is about to work on a subset of the tree it thinks is the
// whole of it.
func discover(root string) ([]string, error) {
	var mods []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != root && skipName(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() != "go.mod" {
			return nil
		}
		rel, rerr := filepath.Rel(root, filepath.Dir(path))
		if rerr != nil {
			return rerr
		}
		mods = append(mods, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walking %s for go.mod: %w", root, err)
	}
	slices.Sort(mods)
	if len(mods) == 0 {
		return nil, fmt.Errorf("no go.mod under %s, so every module-scoped gate driven "+
			"by this discovery would run over nothing and exit 0 (bd gqlc-s3lt)", root)
	}
	if mods[0] != "." {
		return nil, fmt.Errorf("no go.mod at %s itself, so the main module is outside the "+
			"set this discovery found (%s) and is unscanned (bd gqlc-s3lt)",
			root, strings.Join(mods, " "))
	}
	return mods, nil
}

// moduleDir asks the go command where a module's root is, rather than deriving
// it from the path walked to get there: `go list ./...` prints .Dir the same
// way, symlinks and all, and the two are compared downstream.
func moduleDir(ctx context.Context, dir string) (string, error) {
	cmd := exec.CommandContext(ctx, "go", "list", "-m", "-f", "{{.Dir}}")
	cmd.Dir = dir
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("go list -m in %s: %w: %s", dir, err, strings.TrimSpace(stderr.String()))
	}
	got := strings.TrimSpace(string(out))
	if got == "" {
		return "", fmt.Errorf("go list -m in %s printed no directory", dir)
	}
	return got, nil
}

// moduleGoDirs is the `dirs` and `tags` commands' shared front half: discover
// the modules, resolve the named one, and walk it bounded by the others.
func moduleGoDirs(ctx context.Context, root, module string) ([]string, error) {
	mods, err := discover(root)
	if err != nil {
		return nil, err
	}
	module = filepath.ToSlash(filepath.Clean(module))
	if !slices.Contains(mods, module) {
		return nil, fmt.Errorf("%s is not one of the modules discovered under %s (%s)",
			module, root, strings.Join(mods, " "))
	}
	abs, err := moduleDir(ctx, filepath.Join(root, module))
	if err != nil {
		return nil, err
	}
	// Nested module roots are addressed relative to the module being walked, so
	// the prune paths are the very paths the walk below will produce. Asking the
	// go command for each nested module's .Dir instead would reintroduce the
	// chance of a symlink-resolved answer that never matches anything.
	var nested []string
	for _, m := range mods {
		if m == module {
			continue
		}
		rel, err := filepath.Rel(module, m)
		if err != nil || rel == ".." || strings.HasPrefix(rel, "../") {
			continue
		}
		nested = append(nested, filepath.Join(abs, rel))
	}
	return goDirs(module, abs, nested)
}

// goDirs returns every directory at or under moduleRoot that holds a Go file,
// absolute and sorted, stopping at each of the nested module roots so their
// files are not filed under this module.
//
// Off disk rather than out of `go list`, because the `./...` wildcard does not
// match a directory whose Go files are ALL excluded by build constraints: no
// package, no error, and no IgnoredGoFiles either. That directory shape is the
// only reason this walk exists.
//
// An empty result is an error. A Go module with no directory holding a Go file
// is not a thing, and the postcondition this walk feeds — every directory
// holding a Go file was matched by `go list` — passes trivially when the walk
// comes back empty, certifying that nothing was verified (bd gqlc-s3lt).
func goDirs(module, moduleRoot string, nested []string) ([]string, error) {
	prune := make(map[string]struct{}, len(nested))
	for _, n := range nested {
		prune[filepath.Clean(n)] = struct{}{}
	}
	seen := make(map[string]struct{})
	err := filepath.WalkDir(moduleRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path == moduleRoot {
				return nil
			}
			if skipName(d.Name()) {
				return filepath.SkipDir
			}
			if _, ok := prune[filepath.Clean(path)]; ok {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(d.Name(), ".go") && !skipName(d.Name()) {
			seen[filepath.Dir(path)] = struct{}{}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walking %s for Go files: %w", moduleRoot, err)
	}
	dirs := make([]string, 0, len(seen))
	for d := range seen {
		dirs = append(dirs, d)
	}
	slices.Sort(dirs)
	if len(dirs) == 0 {
		return nil, fmt.Errorf("the walk of module %s at %s found no directory holding a Go "+
			"file. A Go module with no such directory is not a thing, so this is the walk "+
			"broken rather than the module empty — and an empty walk is the input that makes "+
			"the directory-coverage postcondition pass by comparing two empty sets, green "+
			"over unscanned code (bd gqlc-s3lt)", module, moduleRoot)
	}
	return dirs, nil
}

// --- the tag derivation ------------------------------------------------------

// releaseTag matches the terms the toolchain derives from its own version. The
// go command sets one per release up to the one running, so naming one in -tags
// either restates a fact it already knows or asserts a release it is not.
var releaseTag = regexp.MustCompile(`^go1\.[0-9]+(\.[0-9]+)?$`)

// toolchainTerms are the terms the go command answers from the build
// configuration rather than from -tags: the operating system family, cgo, the
// compiler, and the sanitiser and race instrumentations. They are dropped for
// the same reason GOOS and GOARCH values are — see classify.
var toolchainTerms = map[string]struct{}{
	"unix": {}, "cgo": {}, "gc": {}, "gccgo": {},
	"race": {}, "msan": {}, "asan": {},
	// `ignore` is the conventional marker for a file that is part of no build
	// at all. Enabling it would compile files their authors excluded from
	// every configuration, which is the opposite of scanning what ships.
	"ignore": {},
}

// classify reports whether term belongs in a -tags argument.
//
// A -tags list is a monotone "assert this term true" knob, and the go command
// will honour it for ANY spelling, including a GOOS. That is the whole defect
// (bd gqlc-e7oq): the question is not what the flag accepts but what asserting
// a term MEANS.
//
// Three classes, one of which belongs:
//
//   - GOOS and GOARCH values, per `go tool dist list`. The platform a scan runs
//     on is chosen by GOOS/GOARCH, not by -tags; naming one as a tag tells the
//     constraint evaluator it is on a platform it is not. On today's tree
//     `//go:build windows` derived `-tags windows` and `go list` duly compiled
//     Windows-only code on Linux, both coverage postconditions passing over a
//     build that cannot exist.
//   - `go1.N` release tags and the toolchain-derived terms above. Same
//     argument, different fact: the toolchain owns them, and -tags can only
//     disagree with it.
//   - everything else — a custom build tag, which is exactly the thing -tags
//     exists to turn on. Passed through.
func classify(term string, platforms map[string]struct{}) bool {
	if _, ok := platforms[term]; ok {
		return false
	}
	if _, ok := toolchainTerms[term]; ok {
		return false
	}
	return !releaseTag.MatchString(term)
}

// collect walks a parsed constraint and records every term appearing at POSITIVE
// polarity, flipping at each `!`.
//
// Polarity is the second half of the classification, and it is the half the
// naive derivation had no notion of: it deleted `!` along with the parentheses
// and emitted `windows` for `//go:build !windows`, which is not a weaker answer
// but the inverse one. -tags can only make a term true, so it can only ever
// satisfy a positive occurrence; on a negated occurrence it does the opposite
// of what was asked.
//
// This is the right answer for `!foo` where foo genuinely is a custom tag, not
// only for `!windows`. `!foo` is satisfied by foo's ABSENCE, which is the
// default configuration, so contributing nothing is contributing exactly what
// that file needs. If some sibling file asks for `foo` positively then no single
// build covers both, and the caller's coverage postconditions redden and name
// the files rather than this derivation silently picking a side.
func collect(e constraint.Expr, positive bool, into map[string]struct{}) {
	switch x := e.(type) {
	case *constraint.TagExpr:
		if positive {
			into[x.Tag] = struct{}{}
		}
	case *constraint.NotExpr:
		collect(x.X, !positive, into)
	case *constraint.AndExpr:
		collect(x.X, positive, into)
		collect(x.Y, positive, into)
	case *constraint.OrExpr:
		collect(x.X, positive, into)
		collect(x.Y, positive, into)
	}
}

// constraintTags returns the custom build tags a single constraint line asks
// for, sorted. line may be either spelling; constraint.Parse reads both.
func constraintTags(line string, platforms map[string]struct{}) ([]string, error) {
	// The table is an input, and an input that came back empty reclassifies
	// every GOOS term as a custom build tag — reintroducing this bead's bug
	// without a code change. Refused here rather than at the one call site, so
	// it stays refused for the next one.
	if len(platforms) == 0 {
		return nil, errors.New("the GOOS/GOARCH table is empty, so every platform term would " +
			"classify as a custom build tag and `//go:build !windows` would derive `-tags " +
			"windows` again (bd gqlc-e7oq)")
	}
	e, err := constraint.Parse(line)
	if err != nil {
		return nil, fmt.Errorf("parsing build constraint %q: %w", line, err)
	}
	terms := make(map[string]struct{})
	collect(e, true, terms)
	var tags []string
	for t := range terms {
		if classify(t, platforms) {
			tags = append(tags, t)
		}
	}
	slices.Sort(tags)
	return tags, nil
}

// fileConstraint returns the build constraint governing a Go file, in the go
// toolchain's own precedence: the first `//go:build` line if the header has one,
// otherwise the conjunction of every `// +build` line. Returns "" when the file
// carries no constraint.
//
// Reading both spellings at once would derive a tag from a line the compiler
// does not honour, and the coverage postconditions downstream compare against
// what the compiler actually did.
func fileConstraint(path string) (string, error) {
	f, err := os.Open(path) //nolint:gosec // path comes from this program's own walk.
	if err != nil {
		return "", fmt.Errorf("reading build constraints from %s: %w", path, err)
	}
	defer f.Close() //nolint:errcheck // read-only.

	var plus []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		// Constraints live in the header, above the package clause. Stopping
		// there keeps a string in the body that happens to look like one from
		// entering the derivation.
		if strings.HasPrefix(line, "package ") || line == "package" {
			break
		}
		switch {
		case constraint.IsGoBuild(line):
			return line, nil
		case constraint.IsPlusBuild(line):
			plus = append(plus, line)
		}
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("reading build constraints from %s: %w", path, err)
	}
	if len(plus) == 0 {
		return "", nil
	}
	return strings.Join(plus, "\n"), nil
}

// gradePlatformTerms turns `go tool dist list` output into the set of GOOS and
// GOARCH spellings, and refuses a table that cannot be what it claims to be.
//
// The grading is the point rather than a courtesy. This table is the only thing
// separating a platform term from a custom build tag, so a table that came back
// empty or short does not fail — it silently reclassifies, and `-tags windows`
// comes back. `windows` and `linux` are the anchors because they are the terms
// the bead is about and no plausible dist list omits them.
func gradePlatformTerms(lines []string) (map[string]struct{}, error) {
	terms := make(map[string]struct{})
	for _, l := range lines {
		goos, goarch, ok := strings.Cut(strings.TrimSpace(l), "/")
		if !ok || goos == "" || goarch == "" {
			continue
		}
		terms[goos] = struct{}{}
		terms[goarch] = struct{}{}
	}
	for _, anchor := range []string{"linux", "windows", "amd64"} {
		if _, ok := terms[anchor]; !ok {
			return nil, fmt.Errorf("`go tool dist list` yielded %d platform terms and none of "+
				"them is %q, so this table cannot be the toolchain's. A short table does not "+
				"fail, it reclassifies: every GOOS term it is missing passes through to -tags "+
				"as if it were a custom build tag (bd gqlc-e7oq)", len(terms), anchor)
		}
	}
	return terms, nil
}

// platformTerms asks the toolchain which platforms exist. Authoritative rather
// than a list kept here, which would go stale in the direction that matters: a
// GOOS this file had not heard of would classify as a custom tag.
func platformTerms(ctx context.Context) (map[string]struct{}, error) {
	out, err := exec.CommandContext(ctx, "go", "tool", "dist", "list").Output()
	if err != nil {
		return nil, fmt.Errorf("go tool dist list: %w", err)
	}
	return gradePlatformTerms(strings.Split(string(out), "\n"))
}

// moduleTags returns every custom build tag the files of one module constrain
// themselves by, sorted and deduplicated.
//
// Read off disk rather than out of `go list ./...`, because the wildcard does
// not match a directory whose Go files are ALL constrained: such a directory
// carries tags no listing would ever mention. That is what test/data/tagblind
// is in the tree to witness.
//
// An empty result is legitimate here, unlike the walk it is built on — a module
// whose files carry no constraints asks for no tags. What is not legitimate is
// an empty result arrived at by not looking, and goDirs already refuses that.
func moduleTags(ctx context.Context, root, module string, platforms map[string]struct{}) ([]string, error) {
	dirs, err := moduleGoDirs(ctx, root, module)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{})
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return nil, fmt.Errorf("listing %s: %w", dir, err)
		}
		for _, entry := range entries {
			name := entry.Name()
			if entry.IsDir() || !strings.HasSuffix(name, ".go") || skipName(name) {
				continue
			}
			line, err := fileConstraint(filepath.Join(dir, name))
			if err != nil {
				return nil, err
			}
			if line == "" {
				continue
			}
			tags, err := constraintTags(line, platforms)
			if err != nil {
				return nil, err
			}
			for _, t := range tags {
				seen[t] = struct{}{}
			}
		}
	}
	tags := make([]string, 0, len(seen))
	for t := range seen {
		tags = append(tags, t)
	}
	slices.Sort(tags)
	return tags, nil
}
