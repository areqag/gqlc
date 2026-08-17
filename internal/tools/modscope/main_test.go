package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
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

func TestGoDirsSkipsFilesGoListWillNotMatchEither(t *testing.T) {
	// The exclusions are per-FILE as well as per-directory, and for the same
	// reason: `go list ./...` ignores a Go file whose name starts with `_` or
	// `.`, so a directory holding only those is one go list does not match.
	// Letting it into this walk makes `just vuln`'s directory-coverage
	// postcondition report it as unlisted — a gate red over nothing, which is
	// how a gate gets switched off.
	root := t.TempDir()
	write(t, root, "a.go", "package p\n")
	write(t, root, "scratch/_work.go", "package scratch\n")
	write(t, root, "scratch/.draft.go", "package scratch\n")

	got, err := goDirs(".", root, nil)
	if err != nil {
		t.Fatalf("goDirs: %v", err)
	}
	if !slices.Equal(got, []string{root}) {
		t.Fatalf("goDirs = %v, want only %q: a directory whose only Go files are ones go list "+
			"ignores is not a directory go list matched", got, root)
	}
}

func TestGradeModuleDirRefusesAnEmptyAnswer(t *testing.T) {
	if _, err := gradeModuleDir("test/data/codegen", []byte("  \n")); err == nil {
		t.Fatal("gradeModuleDir accepted an empty answer from a `go list -m` that exited 0, so " +
			"the walk downstream is rooted at \"\" and fails naming neither the module nor the " +
			"subprocess that went quiet")
	}
	got, err := gradeModuleDir("test/data/codegen", []byte("/x/y\n"))
	if err != nil {
		t.Fatalf("gradeModuleDir over a real answer: %v", err)
	}
	if got != "/x/y" {
		t.Fatalf("gradeModuleDir = %q, want %q", got, "/x/y")
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

// declared stands in for .golangci.yml's run.build-tags: the vocabulary of build
// tags this repo owns. A term outside it, and outside the toolchain's own
// vocabularies, is not a tag — it is unplaceable.
var declared = map[string]struct{}{
	"codegen_live": {}, "tagblind": {}, "legacy_tag": {}, "authoritative": {},
}

const origin = "fixture/example.go"

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
		// The other half, measured on the parent commit: a positive GOOS term
		// is ACCEPTED by `go list -tags windows`, so both of the recipe's
		// coverage postconditions pass over a build that cannot happen. Only
		// the postconditions — parent `just vuln` still exited 1 there, because
		// -tags windows drags in runtime/cgo's gcc_windows_amd64.c and
		// govulncheck stops on the missing windows.h. What was observed is two
		// guards reporting success, not the gate turning green.
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
			got, err := constraintTags(tc.line, platforms, declared, origin)
			if err != nil {
				t.Fatalf("constraintTags(%q): %v", tc.line, err)
			}
			if !slices.Equal(got, tc.want) {
				t.Fatalf("constraintTags(%q) = %v, want %v", tc.line, got, tc.want)
			}
		})
	}
}

func TestDeclaringATermTheToolchainOwnsDoesNotMakeItATag(t *testing.T) {
	// classify consults its own vocabularies before the declared one, and every
	// fixture above is blind to that ordering: their `declared` and `platforms`
	// sets are disjoint, so the two orders agree on every line in the table.
	// This vocabulary overlaps on purpose.
	//
	// run.build-tags is hand-maintained, so a GOOS spelling landing in it is a
	// typo away. Under the reverse ordering `//go:build linux` then derives
	// `-tags linux`, `go list` matches a Linux-only directory whatever the
	// machine is, and both coverage postconditions pass over a build nobody
	// ran — bd gqlc-e7oq's positive-GOOS fail-open, restored by one line of
	// precedence.
	overlapping := map[string]struct{}{
		"codegen_live": {}, "linux": {}, "arm64": {}, "unix": {}, "go1.24": {},
	}
	for _, tc := range []struct {
		term string
		want termClass
	}{
		{"linux", classPlatform},
		{"arm64", classPlatform},
		{"unix", classToolchain},
		{"go1.24", classRelease},
		{"codegen_live", classCustom},
	} {
		// Errorf, so the consequence below is still measured when the rule
		// above it breaks: the two halves are the classification and what the
		// derivation does with it, and a mutation should be seen to move both.
		if got := classify(tc.term, platforms, overlapping); got != tc.want {
			t.Errorf("classify(%q), with %q declared in run.build-tags, = %d, want %d: what a "+
				"term IS does not change because the config also names it", tc.term, tc.term, got, tc.want)
		}
	}
	for _, line := range []string{
		"//go:build linux && codegen_live",
		"//go:build arm64 && codegen_live",
		"//go:build unix && codegen_live",
		"//go:build go1.24 && codegen_live",
	} {
		got, err := constraintTags(line, platforms, overlapping, origin)
		if err != nil {
			t.Fatalf("constraintTags(%q): %v", line, err)
		}
		if !slices.Equal(got, []string{"codegen_live"}) {
			t.Fatalf("constraintTags(%q) = %v, want [codegen_live]: a term the toolchain owns "+
				"reached -tags because run.build-tags happened to name it, and a -tags argument "+
				"asserting a platform is a lie about the machine (bd gqlc-e7oq)", line, got)
		}
	}
}

func TestConstraintTagsRefusesALineItCannotRead(t *testing.T) {
	// A constraint this derivation cannot parse is the derivation going quiet
	// over a file, and a quiet derivation drops tags rather than reporting that
	// it dropped them — the scan then runs over less and exits 0.
	if _, err := constraintTags("//go:build linux &&", platforms, declared, origin); err == nil {
		t.Fatal("constraintTags accepted a malformed constraint, so a file the go " +
			"toolchain would reject contributes no tags and nothing says so")
	}
}

func TestConstraintTagsRefusesAnEmptyPlatformTable(t *testing.T) {
	// The empty measurement again, one level down, and named as the subprocess
	// failure it is rather than as a file full of unplaceable terms.
	if _, err := constraintTags("//go:build !windows", nil, declared, origin); err == nil {
		t.Fatal("constraintTags accepted an empty platform table, so the reason nothing in " +
			"the tree could be classified would be reported as the tree's fault")
	}
}

func TestConstraintTagsRefusesATermItCannotPlace(t *testing.T) {
	// The root fix for bd gqlc-e7oq's fail-open. The old rule made "custom build
	// tag" the DEFAULT case, so every failure of the vocabularies above it — a
	// short dist list, a typo, a term nobody declared — came out the far end as
	// a `-tags` argument. There is no default case now.
	for _, tc := range []struct{ name, line string }{
		{"undeclared term", "//go:build mystery"},
		// Graded at either polarity: `derive nothing` is the right action for a
		// negated term only once the term is known to be a custom tag or a
		// platform value, which is what unplaceable means it is not.
		{"undeclared term, negated", "//go:build !mystery"},
		{"undeclared term beside a declared one", "//go:build tagblind && mystery"},
		{"a typo of a declared term", "//go:build codegen_liv"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := constraintTags(tc.line, platforms, declared, origin)
			if err == nil {
				t.Fatalf("constraintTags(%q) = %v with no error: an undeclared term reaching "+
					"-tags is how a term the derivation never understood ends up asserted "+
					"true (bd gqlc-e7oq)", tc.line, got)
			}
			if !strings.Contains(err.Error(), origin) {
				t.Fatalf("the refusal does not name the file it came from, which is the only "+
					"thing that makes it actionable: %v", err)
			}
		})
	}
}

func TestATruncatedDistListMakesPlatformTermsUnplaceableNotTags(t *testing.T) {
	// The reviewer's P2, and the reason the anchors below cannot be the fix.
	// gradePlatformTerms ACCEPTS this two-entry table — it holds linux, windows
	// and amd64, which is every anchor — and under the old rule `darwin` and
	// `arm64` then fell out of the platform vocabulary and into `-tags`. That is
	// bd gqlc-e7oq surviving the guard written against it, so the assertion is
	// not that the table is refused (it is not, and adding darwin and arm64 to
	// the anchor list only moves the truncation one term along) but that the
	// terms it lost are refused.
	truncated, err := gradePlatformTerms([]string{"linux/amd64", "windows/amd64"})
	if err != nil {
		t.Fatalf("gradePlatformTerms over a truncated table: %v", err)
	}
	for _, line := range []string{"//go:build darwin", "//go:build arm64", "//go:build !darwin"} {
		got, err := constraintTags(line, truncated, declared, origin)
		if err == nil {
			t.Fatalf("constraintTags(%q) under a truncated dist list = %v with no error: the "+
				"platform terms the table lost have reclassified into -tags arguments, which "+
				"is the defect its own fix leaves by (bd gqlc-e7oq)", line, got)
		}
	}
	// And the same table still classifies what it DID keep, so the refusal is
	// about the missing terms rather than about truncation in general.
	if _, err := constraintTags("//go:build linux && codegen_live", truncated, declared, origin); err != nil {
		t.Fatalf("a term the truncated table still holds must still be a platform term: %v", err)
	}
}

func TestGradePlatformTermsDoesNotTakeHalfALineAsATerm(t *testing.T) {
	// `go tool dist list` prints goos/goarch and nothing else, so a line with an
	// empty half is this program misreading the table. Half of it is not taken:
	// the empty string would become a vocabulary entry no build constraint can
	// name, and `darwin` from a `darwin/` line would be a GOOS asserted on the
	// strength of a line the toolchain never emits.
	//
	// What that costs is bounded and already pinned: the narrowed vocabulary
	// makes the terms it lost unplaceable, not custom tags — see
	// TestATruncatedDistListMakesPlatformTermsUnplaceableNotTags.
	got, err := gradePlatformTerms([]string{"linux/amd64", "windows/amd64", "darwin/", "/arm64", "js", ""})
	if err != nil {
		t.Fatalf("gradePlatformTerms: %v", err)
	}
	for _, absent := range []string{"", "darwin", "arm64", "js"} {
		if _, ok := got[absent]; ok {
			t.Fatalf("gradePlatformTerms took %q from a line that is not goos/goarch, so a "+
				"misread table becomes a vocabulary rather than a refusal", absent)
		}
	}
	for _, present := range []string{"linux", "windows", "amd64"} {
		if _, ok := got[present]; !ok {
			t.Fatalf("gradePlatformTerms dropped %q, which came from a well-formed line", present)
		}
	}
}

func TestPlatformTermsIsGradedAgainstAnchorsItMustContain(t *testing.T) {
	// `go tool dist list` is a subprocess, and a subprocess that prints a short
	// answer is the failure mode this whole program exists to refuse. The
	// anchors are an early, legible refusal for the gross case only; the test
	// above is the one that covers a table short by less than an anchor.
	if _, err := gradePlatformTerms(nil); err == nil {
		t.Fatal("an empty `go tool dist list` was accepted")
	}
	if _, err := gradePlatformTerms([]string{"linux/amd64"}); err == nil {
		t.Fatal("a dist list holding no windows platform was accepted, so 'windows' would " +
			"be unplaceable in every file mentioning it, reported as the tree's fault")
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

// --- the declared vocabulary -------------------------------------------------

func TestDeclaredTagsReadsRunBuildTagsAndNothingElse(t *testing.T) {
	// Anchored on the run.build-tags key rather than on "every `- item` in the
	// file": .golangci.yml is mostly lists of linter names, and a vocabulary
	// that swallowed those would place `staticcheck` as a build tag and accept
	// it as one.
	root := t.TempDir()
	write(t, root, ".golangci.yml", `version: "2"

run:
  build-tags:
    # a comment between entries
    - codegen_live
    - tagblind

linters:
  enable:
    - staticcheck
    - govet
`)
	got, err := declaredTags(root)
	if err != nil {
		t.Fatalf("declaredTags: %v", err)
	}
	if !slices.Equal(sortedTerms(got), []string{"codegen_live", "tagblind"}) {
		t.Fatalf("declaredTags = %v, want [codegen_live tagblind]", sortedTerms(got))
	}
}

func TestDeclaredTagsRefusesAConfigItCannotFind(t *testing.T) {
	// Fail-closed on the vocabulary's absence, because the alternative is an
	// empty vocabulary — under which every custom tag in the tree is unplaceable
	// anyway, but reported as a Go file's fault rather than as the missing file
	// it is.
	if _, err := declaredTags(t.TempDir()); err == nil {
		t.Fatal("declaredTags accepted a checkout with no .golangci.yml")
	}
}

func TestAnEmptyVocabularyFailsClosedWithoutAGradingClause(t *testing.T) {
	// The property that let check-golangci-build-tags' hand-written extraction
	// drop its own emptiness check: a vocabulary that came back empty does not
	// agree with everything here, it places nothing.
	if _, err := constraintTags("//go:build codegen_live", platforms, nil, origin); err == nil {
		t.Fatal("an empty vocabulary accepted a custom tag, so an extraction that went quiet " +
			"would derive tags nobody declared")
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

	got, err := moduleTags(t.Context(), root, ".", platforms, declared)
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

	got, err := moduleTags(t.Context(), root, ".", platforms, declared)
	if err != nil {
		t.Fatalf("moduleTags: %v", err)
	}
	if !slices.Equal(got, []string{"authoritative"}) {
		t.Fatalf("moduleTags = %v, want [authoritative]", got)
	}
}

func TestModuleTagsIgnoresFilesTheGoCommandNeverBuilds(t *testing.T) {
	// The per-file exclusion again, on the tag side, where it fails in the
	// UNSAFE direction: a `_`- or `.`-prefixed file is one the go command never
	// compiles, so a constraint in it describes no build. Deriving from it puts
	// a tag into `-tags` on the strength of a file the scan will not load.
	root := t.TempDir()
	write(t, root, "go.mod", goMod)
	write(t, root, "a.go", "//go:build codegen_live\n\npackage p\n")
	write(t, root, "_old.go", "//go:build legacy_tag\n\npackage p\n")
	write(t, root, ".draft.go", "//go:build authoritative\n\npackage p\n")

	got, err := moduleTags(t.Context(), root, ".", platforms, declared)
	if err != nil {
		t.Fatalf("moduleTags: %v", err)
	}
	if !slices.Equal(got, []string{"codegen_live"}) {
		t.Fatalf("moduleTags = %v, want [codegen_live]: a constraint in a file the go command "+
			"never builds is not a tag this scan should assert", got)
	}
}

func TestFileConstraintStopsAtThePackageClause(t *testing.T) {
	// Constraints live in the header. Below the package clause a line that
	// looks like one is prose — this program's own source quotes constraint
	// spellings in doc comments — and reading it would derive a tag from a line
	// the compiler never honoured, against which the coverage postconditions
	// downstream then compare.
	root := t.TempDir()
	for _, tc := range []struct{ name, body string }{
		{"go_build_below_the_clause", "package p\n\n// The spelling is:\n//go:build tagblind\n"},
		{"plus_build_below_the_clause", "package p\n\n// The old spelling is:\n// +build legacy_tag\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			write(t, root, tc.name+".go", tc.body)
			got, err := fileConstraint(filepath.Join(root, tc.name+".go"))
			if err != nil {
				t.Fatalf("fileConstraint: %v", err)
			}
			if got != "" {
				t.Fatalf("fileConstraint = %q, want \"\": a constraint-shaped line in the body "+
					"is not a constraint, and the go command does not read it as one", got)
			}
		})
	}
}

// --- the command surface -----------------------------------------------------

// errWriter fails from its nth write on, so a set that came back short can be
// observed as one.
type errWriter struct{ writes, failFrom int }

func (w *errWriter) Write(p []byte) (int, error) {
	w.writes++
	if w.writes >= w.failFrom {
		return 0, errors.New("no space left on device")
	}
	return len(p), nil
}

func TestWriteLinesReportsAWriteThatStoppedPartWayThrough(t *testing.T) {
	// Every caller reads this program's stdout as a set, so a write that failed
	// half way is a set that came back short: the caller compares against fewer
	// directories, or scans under fewer tags, and nothing says so. That is the
	// defect this program exists to refuse, arriving through the door it leaves
	// by.
	if err := writeLines(&errWriter{failFrom: 2}, []string{"a", "b", "c"}); err == nil {
		t.Fatal("writeLines returned no error after a failed write, so a truncated set reaches " +
			"the caller looking exactly like a complete one")
	}
	// Through run(), which is the surface the shell recipes actually read.
	root := t.TempDir()
	write(t, root, "go.mod", goMod)
	write(t, root, "a.go", "package p\n")
	write(t, root, "nested/go.mod", goMod)
	write(t, root, "nested/b.go", "package q\n")
	if err := run(t.Context(), []string{"-root", root, "modules"}, &errWriter{failFrom: 1}); err == nil {
		t.Fatal("run modules returned no error although stdout refused every write, so `scope " +
			"modules` would print nothing, exit 0, and the recipe reading it would sweep nothing")
	}
}

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
