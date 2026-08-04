// Command modscope answers the module-scoped questions this repo's gates ask of
// a checkout: which Go modules it holds, and which directories of each hold Go
// files.
//
// It exists because those answers were derived more than once — in `just vuln`'s
// sweep and, by hand, in the fence beside it — and a fact derived twice is a
// fact that can disagree with itself the day a third module appears.
//
// Every answer is GRADED before it is printed. An empty module set, and a module
// with no directory holding a Go file, are errors rather than empty lines,
// because a gate handed an empty
// measurement compares two empty sets and reports success over nothing — this
// repo's dominant defect class, and the one `just vuln`'s directory-coverage
// postcondition was itself an instance of (bd gqlc-s3lt).
//
// Usage:
//
//	modscope [-root DIR] modules       # every module's path relative to DIR
//	modscope [-root DIR] dirs MODULE   # every directory of MODULE holding a .go file
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "modscope: %v\n", err)
		os.Exit(1)
	}
}

const usage = "usage: modscope [-root DIR] modules|dirs MODULE"

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

// moduleGoDirs is the `dirs` command's front half: discover the modules, resolve
// the named one, and walk it bounded by the others.
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
