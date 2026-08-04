package main

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// The groups below are the three beads this program was written for, and each
// assertion is the mutation that reddens it:
//
//   - discover/goDirs — an empty measurement must be an error, not an empty set
//     that makes a downstream comparison pass over nothing (bd gqlc-s3lt).
//   - discover again — a third module in the tree is found without being named
//     anywhere (bd gqlc-oxne).
//   - classify/constraintTags — a term the toolchain owns must never reach
//     `-tags`, and a negated term must never produce its own positive, which is
//     what turned `//go:build !windows` into `-tags windows` (bd gqlc-e7oq).

// write creates path (relative to dir) with the given content, making parents.
func write(t *testing.T, dir, path, content string) {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(full), err)
	}
	if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", full, err)
	}
}

const goMod = "module example.com/x\n\ngo 1.26.5\n"

// --- discovery ---------------------------------------------------------------

func TestDiscoverFindsEveryModuleWithoutBeingToldTheirNames(t *testing.T) {
	// bd gqlc-oxne: the fence named `test/data/codegen` literally while the
	// sweep discovered modules, so a third module got a scan and no fence. Both
	// now read this, and this is what a third module has to walk into.
	root := t.TempDir()
	write(t, root, "go.mod", goMod)
	write(t, root, "a.go", "package p\n")
	write(t, root, "test/data/codegen/go.mod", goMod)
	write(t, root, "test/data/codegen/a.go", "package p\n")
	write(t, root, "test/data/thirdmod/go.mod", goMod)
	write(t, root, "test/data/thirdmod/a.go", "package p\n")

	got, err := discover(root)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	want := []string{".", "test/data/codegen", "test/data/thirdmod"}
	if !slices.Equal(got, want) {
		t.Fatalf("discover = %v, want %v", got, want)
	}
}

func TestDiscoverSkipsDirectoriesGoListCannotReach(t *testing.T) {
	root := t.TempDir()
	write(t, root, "go.mod", goMod)
	write(t, root, "a.go", "package p\n")
	write(t, root, "testdata/fixture/go.mod", goMod)
	write(t, root, "vendor/example.com/dep/go.mod", goMod)
	write(t, root, ".bin/cache/go.mod", goMod)
	write(t, root, "_scratch/go.mod", goMod)

	got, err := discover(root)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if !slices.Equal(got, []string{"."}) {
		t.Fatalf("discover = %v, want only the root module: a module outside `go list ./...`'s "+
			"reach is one the fence would build, vet and lint although nothing else loads it", got)
	}
}

func TestDiscoverRefusesACheckoutWithNoModule(t *testing.T) {
	// The empty measurement. Returning an empty list here would leave every
	// caller looping zero times and exiting 0 (bd gqlc-s3lt).
	root := t.TempDir()
	write(t, root, "notes.txt", "no go.mod anywhere\n")

	if _, err := discover(root); err == nil {
		t.Fatal("discover over a checkout with no go.mod returned no error, so a gate driven " +
			"by it would sweep nothing and report success")
	}
}

func TestDiscoverRefusesACheckoutWhoseRootIsNotAModule(t *testing.T) {
	root := t.TempDir()
	write(t, root, "sub/go.mod", goMod)
	write(t, root, "sub/a.go", "package p\n")

	if _, err := discover(root); err == nil {
		t.Fatal("discover with no go.mod at the root returned no error, so the main module " +
			"would be missing from the set every caller treats as the whole tree")
	}
}

// --- the walk ----------------------------------------------------------------

func TestGoDirsFindsEveryDirectoryHoldingAGoFile(t *testing.T) {
	root := t.TempDir()
	write(t, root, "a.go", "package p\n")
	write(t, root, "internal/deep/b.go", "package q\n")
	write(t, root, "docs/notes.md", "not go\n")

	got, err := goDirs(".", root, nil)
	if err != nil {
		t.Fatalf("goDirs: %v", err)
	}
	want := []string{root, filepath.Join(root, "internal", "deep")}
	if !slices.Equal(got, want) {
		t.Fatalf("goDirs = %v, want %v", got, want)
	}
}

func TestGoDirsStopsAtANestedModuleAndAtGoListsOwnExclusions(t *testing.T) {
	root := t.TempDir()
	write(t, root, "a.go", "package p\n")
	write(t, root, "nested/go.mod", goMod)
	write(t, root, "nested/b.go", "package q\n")
	write(t, root, "testdata/c.go", "package r\n")
	write(t, root, "vendor/d.go", "package s\n")
	write(t, root, ".hidden/e.go", "package t\n")
	write(t, root, "_scratch/f.go", "package u\n")

	got, err := goDirs(".", root, []string{filepath.Join(root, "nested")})
	if err != nil {
		t.Fatalf("goDirs: %v", err)
	}
	if !slices.Equal(got, []string{root}) {
		t.Fatalf("goDirs = %v, want only %q", got, root)
	}
}

func TestGoDirsRefusesAWalkThatFoundNothing(t *testing.T) {
	// bd gqlc-s3lt, the whole bead. The directory-coverage postcondition in
	// `just vuln` is `comm` between the directories this walk found and the
	// directories `go list ./...` matched. An empty walk makes both sides empty,
	// the comparison passes, and the gate certifies having verified nothing.
	root := t.TempDir()
	write(t, root, "go.mod", goMod)
	write(t, root, "README.md", "a module with no Go file in it\n")

	if _, err := goDirs("example", root, nil); err == nil {
		t.Fatal("goDirs returned no error over a module holding no Go file at all, so the " +
			"directory-coverage postcondition downstream would compare two empty sets and pass")
	}
}

func TestGoDirsRefusesAWalkEmptiedByOverEagerPruning(t *testing.T) {
	// The realistic shape of the same failure: the prune list, not the corpus,
	// is what went wrong. Every Go file is still on disk; the walk stopped
	// before reaching any of them.
	root := t.TempDir()
	write(t, root, "go.mod", goMod)
	write(t, root, "src/a.go", "package p\n")
	write(t, root, "src/deep/b.go", "package q\n")

	if _, err := goDirs("example", root, nil); err != nil {
		t.Fatalf("an unpruned walk should find src and src/deep: %v", err)
	}
	if _, err := goDirs("example", root, []string{filepath.Join(root, "src")}); err == nil {
		t.Fatal("goDirs returned no error when the prune list swallowed every Go directory, " +
			"which is exactly the emptied walk the postcondition cannot see (bd gqlc-s3lt)")
	}
}

// --- the tag derivation ------------------------------------------------------

// platforms is a stand-in for `go tool dist list`, small enough to read and
// containing the two terms the bead is about.
var platforms = map[string]struct{}{
	"windows": {}, "linux": {}, "darwin": {}, "amd64": {}, "arm64": {}, "386": {},
}

func TestConstraintTagsKeepsOnlyPositiveCustomTerms(t *testing.T) {
	for _, tc := range []struct {
		name string
		line string
		want []string
	}{
		// bd gqlc-e7oq, the headline. The naive derivation stripped '!' and
		// emitted `windows`, and `-tags windows` satisfies `windows`, so the
		// file's own constraint went false and the scan compiled it nowhere.
		{"negated GOOS", "//go:build !windows", nil},
		// The other half, measured on the branch: a positive GOOS term is
		// ACCEPTED by `go list -tags windows`, so both coverage postconditions
		// pass and govulncheck scans Windows-only code on Linux — a build that
		// cannot happen, certified as if it had.
		{"positive GOOS", "//go:build windows", nil},
		{"GOARCH", "//go:build arm64", nil},
		{"release tag", "//go:build go1.24", nil},
		{"toolchain-derived", "//go:build unix && cgo && gc", nil},
		{"race instrumentation", "//go:build race", nil},
		{"never built", "//go:build ignore", nil},
		{"custom tag", "//go:build codegen_live", []string{"codegen_live"}},
		// The sharp end: `!foo` where foo genuinely IS a custom tag. Adding
		// `foo` cannot satisfy `!foo` — it falsifies it. The default
		// configuration, in which foo is absent, is the one the author wrote
		// for, so a negated term contributes nothing.
		{"negated custom tag", "//go:build !codegen_live", nil},
		// Polarity is tracked, not detected by counting '!' characters: the
		// negation of a disjunction puts BOTH terms negative, and a negation
		// under a negation puts its term positive again.
		{"negation distributes over a group", "//go:build !(codegen_live || tagblind)", nil},
		{"negation under negation is positive", "//go:build !(!codegen_live)", []string{"codegen_live"}},
		{"negated term inside a negated group", "//go:build !(tagblind && !codegen_live)", []string{"codegen_live"}},
		{"mixed polarity in one expression", "//go:build tagblind && !windows", []string{"tagblind"}},
		{"custom term beside a platform term", "//go:build linux && codegen_live", []string{"codegen_live"}},
		{"or of two custom terms", "//go:build codegen_live || tagblind", []string{"codegen_live", "tagblind"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := constraintTags(tc.line, platforms)
			if err != nil {
				t.Fatalf("constraintTags(%q): %v", tc.line, err)
			}
			if !slices.Equal(got, tc.want) {
				t.Fatalf("constraintTags(%q) = %v, want %v", tc.line, got, tc.want)
			}
		})
	}
}

func TestConstraintTagsRefusesALineItCannotRead(t *testing.T) {
	// A constraint this derivation cannot parse is the derivation going quiet
	// over a file, and a quiet derivation drops tags rather than reporting that
	// it dropped them — the scan then runs over less and exits 0.
	if _, err := constraintTags("//go:build linux &&", platforms); err == nil {
		t.Fatal("constraintTags accepted a malformed constraint, so a file the go " +
			"toolchain would reject contributes no tags and nothing says so")
	}
}

func TestConstraintTagsRefusesAnEmptyPlatformTable(t *testing.T) {
	// The empty measurement again, one level down. With no platform table every
	// GOOS term classifies as a custom build tag and `!windows` becomes `-tags
	// windows` once more — the bug reintroduced by a table that came back short
	// rather than by a code change (bd gqlc-e7oq).
	if _, err := constraintTags("//go:build !windows", nil); err == nil {
		t.Fatal("constraintTags accepted an empty platform table, so GOOS and GOARCH terms " +
			"would be indistinguishable from custom build tags and pass through to -tags")
	}
}

func TestPlatformTermsIsGradedAgainstAnchorsItMustContain(t *testing.T) {
	// `go tool dist list` is a subprocess, and a subprocess that prints a short
	// answer is the failure mode this whole program exists to refuse.
	if _, err := gradePlatformTerms(nil); err == nil {
		t.Fatal("an empty `go tool dist list` was accepted")
	}
	if _, err := gradePlatformTerms([]string{"linux/amd64"}); err == nil {
		t.Fatal("a dist list holding no windows platform was accepted, so 'windows' would " +
			"classify as a custom build tag — the exact term bd gqlc-e7oq is about")
	}
	got, err := gradePlatformTerms([]string{"linux/amd64", "windows/amd64", "darwin/arm64"})
	if err != nil {
		t.Fatalf("gradePlatformTerms: %v", err)
	}
	for _, want := range []string{"linux", "windows", "darwin", "amd64", "arm64"} {
		if _, ok := got[want]; !ok {
			t.Fatalf("gradePlatformTerms dropped %q; both halves of GOOS/GOARCH are terms", want)
		}
	}
}

func TestModuleTagsReadsWhollyConstrainedDirectoriesAndDropsPlatformTerms(t *testing.T) {
	root := t.TempDir()
	write(t, root, "go.mod", goMod)
	write(t, root, "a.go", "package p\n")
	// The tagblind shape: a directory `go list ./...` matches not at all.
	write(t, root, "blind/blind.go", "//go:build tagblind\n\npackage blind\n")
	// The bead's file: guarded to build everywhere except Windows.
	write(t, root, "unix/unixonly.go", "//go:build !windows\n\npackage unixonly\n")
	// The pre-1.17 spelling, which the go toolchain still honours.
	write(t, root, "old/old.go", "// +build legacy_tag\n\npackage old\n")

	got, err := moduleTags(t.Context(), root, ".", platforms)
	if err != nil {
		t.Fatalf("moduleTags: %v", err)
	}
	want := []string{"legacy_tag", "tagblind"}
	if !slices.Equal(got, want) {
		t.Fatalf("moduleTags = %v, want %v: a GOOS term must never reach -tags, and a "+
			"negated one must never produce its own positive (bd gqlc-e7oq)", got, want)
	}
}

func TestModuleTagsPrefersGoBuildOverPlusBuildInOneFile(t *testing.T) {
	// The go toolchain ignores `// +build` entirely once a `//go:build` line is
	// present. Reading both would derive a tag from a line the compiler does not
	// honour, and the coverage postcondition downstream compares against what
	// the compiler did.
	root := t.TempDir()
	write(t, root, "go.mod", goMod)
	write(t, root, "a.go", "//go:build authoritative\n// +build stale\n\npackage p\n")

	got, err := moduleTags(t.Context(), root, ".", platforms)
	if err != nil {
		t.Fatalf("moduleTags: %v", err)
	}
	if !slices.Equal(got, []string{"authoritative"}) {
		t.Fatalf("moduleTags = %v, want [authoritative]", got)
	}
}

// --- the command surface -----------------------------------------------------

func TestRunModulesPrintsTheDiscoveredSet(t *testing.T) {
	root := t.TempDir()
	write(t, root, "go.mod", goMod)
	write(t, root, "a.go", "package p\n")
	write(t, root, "nested/go.mod", goMod)
	write(t, root, "nested/b.go", "package q\n")

	var out bytes.Buffer
	if err := run(t.Context(), []string{"-root", root, "modules"}, &out); err != nil {
		t.Fatalf("run modules: %v", err)
	}
	if got := out.String(); got != ".\nnested\n" {
		t.Fatalf("run modules printed %q, want \".\\nnested\\n\"", got)
	}
}

func TestRunRejectsAModuleItDidNotDiscover(t *testing.T) {
	root := t.TempDir()
	write(t, root, "go.mod", goMod)
	write(t, root, "a.go", "package p\n")

	var out bytes.Buffer
	if err := run(t.Context(), []string{"-root", root, "dirs", "nested"}, &out); err == nil {
		t.Fatal("run dirs over a module that is not in the discovered set returned no error, " +
			"so a caller could ask about a directory the sweep does not cover and get an answer")
	}
}
