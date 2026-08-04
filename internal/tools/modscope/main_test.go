package main

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// The groups below are the beads this program was written for, and each
// assertion is the mutation that reddens it:
//
//   - discover/goDirs — an empty measurement must be an error, not an empty set
//     that makes a downstream comparison pass over nothing (bd gqlc-s3lt).
//   - discover again — a third module in the tree is found without being named
//     anywhere (bd gqlc-oxne).

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
