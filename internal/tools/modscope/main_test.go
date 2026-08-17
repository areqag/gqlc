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

func TestATruncatedDistListMakesUndeclaredPlatformTermsUnplaceableNotTags(t *testing.T) {
	// The reviewer's P2, and the reason the anchors below cannot be the fix.
	// gradePlatformTerms ACCEPTS this two-entry table — it holds linux, windows
	// and amd64, which is every anchor — and under the old rule `darwin` and
	// `arm64` then fell out of the platform vocabulary and into `-tags`. That is
	// bd gqlc-e7oq surviving the guard written against it, so the assertion is
	// not that the table is refused (it is not, and adding darwin and arm64 to
	// the anchor list only moves the truncation one term along) but that the
	// terms it lost are refused.
	//
	// UNDECLARED is in the name because it is a precondition, not a description
	// of the fixture that happens to hold. `declared` above names no platform
	// term, so a term this table loses is left with no vocabulary at all — and
	// that is what makes it unplaceable, not the truncation on its own. Declare
	// the lost term and it is placeable again, as a custom tag — which is why
	// gradePlatformTerms is not where that composition gets closed. It is closed
	// one function up, in platformTerms, and the table below is gradePlatformTerms'
	// own output, read before the floor is unioned in
	// (TestALostPlatformTermTheFloorHoldsIsStillAPlatformTerm).
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

// declaredWithGOOS is the overlap `platforms` and `declared` deliberately lack.
// The `overlapping` table at TestDeclaringATermTheToolchainOwnsDoesNotMakeItATag
// also overlaps the platform vocabulary, on `linux` and `arm64`, and was added
// for that — but against an INTACT `platforms`, where the answer is decided by
// classify's ordering alone. This one overlaps on a spelling a narrowed table can
// lose, which is the only shape that witnesses the two vocabularies competing for
// a term neither the toolchain's answer nor the config can be trusted to hold.
//
// A GOOS in run.build-tags is not a contrived typo. golangci-lint will not load a
// `//go:build darwin` file on a linux runner unless the tag is in that key, so a
// developer who wants such a file linted has a reason to put it there.
var declaredWithGOOS = map[string]struct{}{
	"codegen_live": {}, "tagblind": {}, "darwin": {},
}

func TestADeclaredPlatformTermIsNeverDerivedSoTheStaleClauseNamesIt(t *testing.T) {
	// Leg one of the composition, on its own. This is the guard that actually
	// stands between a GOOS in run.build-tags and a `-tags darwin` scan, and it
	// is NOT classify's refusal of an unplaceable term — a declared term is
	// perfectly placeable. It is check-golangci-build-tags' `stale` direction,
	// `comm -13 derived configured`, recomputed here over the same two sets.
	//
	// Single-fault, and it does not need the entry to constrain nothing: while
	// the dist list still holds `darwin`, classify places it as a platform, so
	// `darwin` can never enter `derived` no matter how many files ask for it, so
	// the difference always names it. The darwin-constrained file below is in the
	// tree precisely to show that it does not rescue the entry.
	//
	// What this does NOT witness: the recipe's own `comm -13`. The difference is
	// recomputed here from the same two sets, so a change to the justfile clause
	// itself — a flipped comm direction, a dropped refusal — passes this test.
	// Both were measured SURVIVING it. That half is covered where it lives, by
	// check-golangci-build-tags' own witness: the recipe adds a term no file
	// constrains to the configured set on every run and refuses to proceed unless
	// its clause both refuses and names it. This test is the derivation half —
	// that `darwin` never reaches `derived` in the first place — and that is all
	// it should be read as.
	root := t.TempDir()
	write(t, root, "go.mod", goMod)
	write(t, root, "mac/mac.go", "//go:build darwin\n\npackage mac\n")
	write(t, root, "blind/blind.go", "//go:build tagblind\n\npackage blind\n")

	derived, err := moduleTags(t.Context(), root, ".", platforms, declaredWithGOOS)
	if err != nil {
		t.Fatalf("moduleTags: %v", err)
	}
	// Exact, not "does not contain darwin": a derivation that came back EMPTY
	// would satisfy both assertions below while having looked at nothing, which
	// is the vacuous measurement this program exists to refuse.
	if !slices.Equal(derived, []string{"tagblind"}) {
		t.Fatalf("moduleTags = %v, want [tagblind]: `darwin` reached -tags because run.build-tags "+
			"named it, and -tags darwin on a linux scan excludes every //go:build !darwin file "+
			"that actually ships (bd gqlc-e7oq)", derived)
	}
	var stale []string
	for _, term := range sortedTerms(declaredWithGOOS) {
		if !slices.Contains(derived, term) {
			stale = append(stale, term)
		}
	}
	if !slices.Contains(stale, "darwin") {
		t.Fatalf("`comm -13 derived configured` = %v over derived %v: the recipe clause that "+
			"refuses a declared GOOS no longer names it, so the entry sits in run.build-tags "+
			"green, waiting for the dist list to lose that term (bd gqlc-oxne)", stale, derived)
	}
}

func TestALostPlatformTermTheFloorHoldsIsStillAPlatformTerm(t *testing.T) {
	// bd gqlc-e7oq's fail-open reassembled from two halves — a dist list that lost
	// a GOOS, and a run.build-tags entry naming the term it lost — which this
	// branch carried as a PIN asserting the fail-open until platformTerms grew a
	// floor. It asserts the REFUSAL now.
	//
	// And it asserts it through platformTerms, which is what run() calls. The
	// shape it replaces went through gradePlatformTerms, one function BELOW the
	// floor, so it stayed green while the class was closed above it — measured on
	// this branch, not supposed. A pin whose written promise is "if this goes red
	// the composition has been closed" has to be able to see the closure, and
	// which function it observes is the whole of whether it can.
	narrowed := []string{"linux/amd64", "windows/amd64", "darwin/"}

	// Vacuity guard, and it is load-bearing: the grading platformTerms is built on
	// really does lose `darwin` over this table. Without it every assertion below
	// would pass over a table that never narrowed.
	raw, err := gradePlatformTerms(narrowed)
	if err != nil {
		t.Fatalf("gradePlatformTerms over a table with a malformed darwin line: %v", err)
	}
	if _, ok := raw["darwin"]; ok {
		t.Fatal("the fixture table still holds darwin BEFORE the floor, so nothing below is " +
			"being measured — this test would pass on the strength of nothing")
	}

	platformsAfterFloor, err := platformTerms(narrowed)
	if err != nil {
		t.Fatalf("platformTerms over a narrowed table: %v", err)
	}
	if _, ok := platformsAfterFloor["darwin"]; !ok {
		t.Fatal("platformTerms did not put `darwin` back after the grading lost it, so the floor " +
			"is not reaching the vocabulary run() classifies against")
	}
	got, err := constraintTags("//go:build darwin", platformsAfterFloor, declaredWithGOOS, origin)
	if err != nil || got != nil {
		t.Fatalf("constraintTags(//go:build darwin) under a narrowed table with darwin declared "+
			"in run.build-tags = %v, %v, want nil, nil: that is `-tags darwin` on a linux scan, "+
			"which excludes every //go:build !darwin file that actually ships (bd gqlc-e7oq)",
			got, err)
	}
	// The intact table, where classify's platform-first ordering carries it alone
	// and the floor adds nothing. Both readings, because a floor that had somehow
	// replaced the toolchain's answer rather than being unioned into it would pass
	// the narrowed case above and break this one.
	intact, err := platformTerms([]string{"linux/amd64", "windows/amd64", "darwin/arm64"})
	if err != nil {
		t.Fatalf("platformTerms over an intact table: %v", err)
	}
	if got, err := constraintTags("//go:build darwin", intact, declaredWithGOOS, origin); err != nil || got != nil {
		t.Fatalf("constraintTags(//go:build darwin) with an intact table = %v, %v, want nil, nil",
			got, err)
	}
	if got, err := constraintTags("//go:build tagblind", intact, declaredWithGOOS, origin); err != nil ||
		!slices.Equal(got, []string{"tagblind"}) {
		t.Fatalf("constraintTags(//go:build tagblind) = %v, %v, want [tagblind], nil: a floor that "+
			"swallowed the declared vocabulary would make every custom tag vanish and every "+
			"assertion above pass on nothing being derived at all", got, err)
	}
}

func TestAPlatformSpellingTheFloorNeverHeardOfIsStillTheResidual(t *testing.T) {
	// The floor's honest limit, pinned where a closure would trip it rather than
	// left in prose. `zzos` stands in for a GOOS the toolchain gains after
	// platformFloor was last synced against internal/syslist: the composition
	// needs THREE faults now — the dist list loses the term, run.build-tags names
	// it, and the floor has never heard of it — where before the floor it needed
	// two. That is a demotion, not a closure, and this says so in a form that
	// reddens if it ever becomes one.
	//
	// TestThePlatformFloorCoversEveryTermTheLiveToolchainReports is what keeps
	// the third fault from arriving quietly: a toolchain that grows a platform the
	// copy above has not been re-synced for reddens there, in `just test`.
	const unknown = "zzos"
	if slices.Contains(platformFloor, unknown) {
		t.Fatalf("%q is in platformFloor, so this measures the closed case and not the residual "+
			"it is named for", unknown)
	}
	narrowed, err := platformTerms([]string{"linux/amd64", "windows/amd64"})
	if err != nil {
		t.Fatalf("platformTerms: %v", err)
	}
	if _, ok := narrowed[unknown]; ok {
		t.Fatalf("%q is already a platform term, so the composition below is not being measured",
			unknown)
	}
	declaredWithUnknown := map[string]struct{}{"codegen_live": {}, unknown: {}}
	got, err := constraintTags("//go:build "+unknown, narrowed, declaredWithUnknown, origin)
	if err != nil {
		t.Fatalf("constraintTags over a floor-unknown declared term: %v", err)
	}
	if !slices.Equal(got, []string{unknown}) {
		t.Fatalf("constraintTags(//go:build %s) = %v, want [%s]: the residual documented in "+
			"main.go's platformTerms comment is no longer reachable, which is good news — that "+
			"concession is now false in the other direction and goes, along with this test",
			unknown, got, unknown)
	}
}

func TestAnUndeclaredFloorOnlySpellingIsDroppedWhereItUsedToBeRefused(t *testing.T) {
	// The floor's cost, pinned as a behaviour change rather than left in main.go's
	// prose. `hurd` is one of the thirteen syslist spellings the live dist list
	// does not carry: BEFORE the floor an undeclared `//go:build hurd` was
	// unplaceable and exited 1 naming the file, the term and the constraint; AFTER
	// it the term is a platform term and is dropped with nothing said. Both halves
	// are asserted, because a pin on the drop alone would pass just as well over a
	// table that had never refused it.
	//
	// The loud half of the class is untouched and is the half that matters:
	// DECLARING `hurd` puts it in run.build-tags, classPlatform keeps it out of
	// `derived`, and check-golangci-build-tags' stale clause then refuses the entry
	// by name. Undeclared, golangci-lint never loads the file at all.
	const floorOnly = "hurd"
	if !slices.Contains(platformFloor, floorOnly) {
		t.Fatalf("%q is not in platformFloor, so nothing below measures the floor", floorOnly)
	}
	lines := []string{"linux/amd64", "windows/amd64"}
	graded, err := gradePlatformTerms(lines)
	if err != nil {
		t.Fatalf("gradePlatformTerms: %v", err)
	}
	if _, ok := graded[floorOnly]; ok {
		t.Fatalf("%q is already a graded platform term, so the floor adds nothing here and the "+
			"change of answer below is not being measured", floorOnly)
	}
	if _, err := constraintTags("//go:build "+floorOnly, graded, nil, origin); err == nil {
		t.Fatalf("constraintTags(//go:build %s) without the floor and undeclared returned no "+
			"error: the refusal this arm used to give is gone from below the floor too, so the "+
			"assertion after it records no change at all", floorOnly)
	}
	floored, err := platformTerms(lines)
	if err != nil {
		t.Fatalf("platformTerms: %v", err)
	}
	if got, err := constraintTags("//go:build "+floorOnly, floored, nil, origin); err != nil || got != nil {
		t.Fatalf("constraintTags(//go:build %s) under the floor, undeclared = %v, %v, want nil, "+
			"nil: the silent-drop arm the floor widened has moved. If it moved back to a refusal "+
			"that is a narrowing and this pin goes with it; if it moved to deriving %q as a custom "+
			"tag that is bd gqlc-e7oq reopened", floorOnly, got, err, floorOnly)
	}
}

func TestThePlatformFloorCoversEveryTermTheLiveToolchainReports(t *testing.T) {
	// The floor is a union, so a term it lacks costs nothing TODAY — it just
	// behaves as it did before the floor existed. What it costs is the residual
	// above, and the only way that residual arrives is drift: a toolchain upgrade
	// adds a platform and this file's copy of internal/syslist does not move. This
	// is that drift, made loud, in the cheapest place it can be made loud.
	lines, err := distList(t.Context())
	if err != nil {
		t.Fatalf("distList: %v", err)
	}
	live, err := gradePlatformTerms(lines)
	if err != nil {
		t.Fatalf("gradePlatformTerms over the live dist list: %v", err)
	}
	// Vacuity guard: a dist list that came back nearly empty would satisfy the
	// coverage assertion below by having almost nothing to cover.
	if len(live) < 20 {
		t.Fatalf("the live `go tool dist list` yielded only %d platform terms, so the coverage "+
			"assertion below is passing over a table that is itself broken", len(live))
	}
	var missing []string
	for _, term := range sortedTerms(live) {
		if !slices.Contains(platformFloor, term) {
			missing = append(missing, term)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("`go tool dist list` reports platform terms platformFloor has never heard of "+
			"(%s), so the floor is out of sync with the toolchain. Re-sync it against "+
			"GOROOT/src/internal/syslist/syslist.go's KnownOS and KnownArch. Until then each of "+
			"those spellings is one narrowed dist list and one run.build-tags entry away from "+
			"being asserted as a custom build tag (bd gqlc-e7oq)", strings.Join(missing, " "))
	}
}

func TestPlatformTermsUnionsTheFloorIntoTheLiveToolchainsAnswer(t *testing.T) {
	// platformTerms had no direct test at all: its only caller was run(), and the
	// test named after it exercised gradePlatformTerms three times and this
	// function never. So the function handing the live `platforms` set to the
	// whole pipeline was measured by nothing — which is exactly why a defect in it
	// (a narrowed live table) and a fix in it (the floor) were both invisible.
	lines, err := distList(t.Context())
	if err != nil {
		t.Fatalf("distList: %v", err)
	}
	live, err := platformTerms(lines)
	if err != nil {
		t.Fatalf("platformTerms over the live dist list: %v", err)
	}
	// linux/amd64 come from the toolchain on any machine this runs on; hurd, zos,
	// sparc64 and nacl are in internal/syslist and not in this toolchain's `go
	// tool dist list`, so they are here only because the union happened on the
	// live path.
	for _, want := range []string{"linux", "windows", "darwin", "amd64", "arm64", "hurd", "zos", "sparc64", "nacl"} {
		if _, ok := live[want]; !ok {
			t.Fatalf("platformTerms over the live dist list has no %q: either the grading dropped "+
				"a term the toolchain reported, or the floor is not being unioned in at all", want)
		}
	}
	// And it is not a vocabulary of everything: a set that answered yes to every
	// term would satisfy the loop above while making every custom tag a platform
	// value and `scope tags` print nothing at all.
	for _, notPlatform := range []string{"codegen_live", "tagblind", "unix", "go1.24", "", "mystery"} {
		if _, ok := live[notPlatform]; ok {
			t.Fatalf("platformTerms over the live dist list holds %q, which is not a platform "+
				"term — a vocabulary that claims everything derives nothing", notPlatform)
		}
	}
	// End to end on the live path: a declared GOOS still never reaches -tags.
	if got, err := constraintTags("//go:build darwin && codegen_live", live, declaredWithGOOS, origin); err != nil ||
		!slices.Equal(got, []string{"codegen_live"}) {
		t.Fatalf("constraintTags over the live vocabulary = %v, %v, want [codegen_live], nil",
			got, err)
	}
	// The grading is not bypassed by the floor. It nearly could be: the floor is a
	// superset of every term the live table yields, so a platformTerms that
	// ignored the graded table entirely would return the same set here and every
	// assertion above would still pass — while `go tool dist list` printing
	// nothing at all became an accepted answer, which is this program's whole
	// defect class arriving through its own fix.
	for _, broken := range [][]string{nil, {}, {"linux/amd64"}, {"", "  ", "js"}} {
		if got, err := platformTerms(broken); err == nil {
			t.Fatalf("platformTerms(%q) = %v with no error: the anchor refusal in the grading is "+
				"not reaching this function, so a subprocess that answered nothing would be "+
				"papered over by the floor and reported as a working vocabulary", broken, len(got))
		}
	}
}

func TestGradePlatformTermsDoesNotTakeHalfALineAsATerm(t *testing.T) {
	// `go tool dist list` prints goos/goarch and nothing else, so a line with an
	// empty half is this program misreading the table. Half of it is not taken:
	// the empty string would become a vocabulary entry no build constraint can
	// name, and `darwin` from a `darwin/` line would be a GOOS asserted on the
	// strength of a line the toolchain never emits.
	//
	// What that costs is NOT "the terms it lost become unplaceable, not custom
	// tags". A lost term is unplaceable only while no other vocabulary claims it,
	// and run.build-tags is a vocabulary that can: declare `darwin` there and a
	// table that dropped it would derive `-tags darwin` on a linux scan. That
	// composition is closed one function up, by the floor platformTerms unions in
	// (TestALostPlatformTermTheFloorHoldsIsStillAPlatformTerm) — which is why the
	// fixture below reads gradePlatformTerms' own output, before the floor.
	//
	// The two faults are NOT symmetric, and this comment used to claim they were
	// ("each pinned on its own", "only both together are silent"). Measured:
	//
	//   - The DECLARATION alone is loud with no further condition. A declared
	//     platform term cannot enter `derived` while the vocabulary holds it, so
	//     check-golangci-build-tags' `stale` direction names it and reddens
	//     `just lint` whether or not any file constrains it
	//     (TestADeclaredPlatformTermIsNeverDerivedSoTheStaleClauseNamesIt, and
	//     the recipe's own witness for the clause itself).
	//   - The NARROWING alone is SILENT, unless some file in the tree constrains
	//     itself by the very term the table lost. Only then does the derivation
	//     reach classify with it and refuse by name — which is why
	//     TestATruncatedDistListMakesUndeclaredPlatformTermsUnplaceableNotTags
	//     supplies such a file. No file in this tree can: the only platform term
	//     any of them names is `windows` (test/data/platformtag), and `windows` is
	//     one of the three anchors above, so a table losing it is refused outright
	//     rather than narrowed silently. Narrowing this program's live dist list
	//     by any other spelling and changing nothing else leaves `go test ./...`,
	//     `just lint` and `just vuln` all green — measured on this branch, by
	//     mangling every `darwin/` line inside platformTerms.
	//
	// So the depth is one unconditional leg and one conditional one, not two.
	//
	// Tightening this `continue` into a refusal would close the malformed-line
	// spelling of the narrowing and nothing else: a table short by whole LINES
	// narrows identically, with no malformed line to catch.
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

func TestGradePlatformTermsIsGradedAgainstAnchorsItMustContain(t *testing.T) {
	// Named for the function it calls. It used to be named for platformTerms and
	// call this one, which is how platformTerms came to have a test named after it
	// and no test of it (bd NB-A); the coverage of platformTerms itself is
	// TestPlatformTermsUnionsTheFloorIntoTheLiveToolchainsAnswer.
	//
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
	// Through run(), which is the surface the shell recipes actually read — and
	// through every arm of it, because each is a separate `return writeLines(...)`
	// and a check on one says nothing about the other three. Each fixture below
	// is built so its arm has at least one line to print: an arm with nothing to
	// write never reaches the failing writer and passes on silence.
	root := t.TempDir()
	write(t, root, "go.mod", goMod)
	write(t, root, "a.go", "//go:build codegen_live\n\npackage p\n")
	write(t, root, ".golangci.yml", "run:\n  build-tags:\n    - codegen_live\n")
	write(t, root, "nested/go.mod", goMod)
	write(t, root, "nested/b.go", "package q\n")
	for _, argv := range [][]string{{"modules"}, {"dirs", "."}, {"tags", "."}, {"declared"}} {
		// Errorf: each arm is an independent reading, and a run that stops at the
		// first says nothing about the three behind it.
		if err := run(t.Context(), append([]string{"-root", root}, argv...), &errWriter{failFrom: 1}); err == nil {
			t.Errorf("run %v returned no error although stdout refused every write, so `scope %v` "+
				"would print nothing, exit 0, and the recipe reading it would compare against the "+
				"empty set", argv, argv)
		}
	}
	// The guard on the guard: with a writer that accepts, every arm above must
	// actually have printed something. An arm that prints nothing would satisfy
	// the loop by never writing at all.
	for _, argv := range [][]string{{"modules"}, {"dirs", "."}, {"tags", "."}, {"declared"}} {
		var out bytes.Buffer
		if err := run(t.Context(), append([]string{"-root", root}, argv...), &out); err != nil {
			t.Fatalf("run %v over the fixture: %v", argv, err)
		}
		if out.Len() == 0 {
			t.Fatalf("run %v printed nothing over this fixture, so the failing-writer case above "+
				"never reached a write and passes on silence", argv)
		}
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
