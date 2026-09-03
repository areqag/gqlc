// Package testcite holds one guard, over two spellings of the same citation: a
// comment that names a test must name a test that exists, and a comment that
// names a test FILE must name a file that exists — or the site is pinned with
// the reason it is not a citation.
//
// This repository routinely settles a question in a comment and cites the test
// holding the answer — "TestX pins the equality", "TestX is why this is `== 1`".
// The citation is prose, so a rename or a deletion leaves it pointing at
// nothing, and the next reader takes the name as evidence the claim is still
// measured. Nothing noticed that until this file (bd gqlc-945h7), and until bd
// gqlc-som6y it read non-test sources alone — which left out the comments most
// likely to name a test by name, the ones a test writes about its neighbours.
//
// The filename half arrived on bd gqlc-9tgiz, and it is the same rot: an
// argument that rests on `foo_test.go` is as dead when that file is deleted as
// one resting on TestFoo when the function is. It was filed from a live
// instance — a WHY ITS OWN FILE paragraph in test/data/codegen still resting on
// live_age_ungated_test.go hours after the file was deleted in PR #1903, found
// by a human reading the paragraph and by nothing here (bd gqlc-4dwao). That
// one was repaired by hand in PR #2401 before this half existed, so the guard
// below finds it clean; what it would have caught is the window between the two.
//
// It is a test rather than a command so that `just test` carries it. A separate
// gate arm would be one more edge somebody can drop and leave the tree green.
//
// It asserts with the standard library alone. An in-package test that imports
// third-party code takes its whole package out of govulncheck's call graph, so
// `just vuln` would report nothing about this package and still exit 0 (bd
// gqlc-m5rc) — testify here bought two helpers and cost the package its
// coverage.
package testcite

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// repoRoot reaches the module root from this package's directory.
const repoRoot = "../../.."

// citationRe is what a cited test name looks like in prose. The trailing class
// deliberately excludes `/`, so `TestX/"a subtest"` yields the top-level name
// alone — the subtest is a string literal in a t.Run call, not a declaration
// this guard can resolve, and demanding it would be a check on something the
// AST does not answer.
//
// A name reached through a dot is a SELECTOR on some value, not a citation, and
// the leading group is what refuses it. RE2 has no lookbehind, so the separator
// is captured and discarded rather than asserted. Measured over this checkout:
// every dot-prefixed occurrence is a go/packages field or an embedded testify
// type — `.TestImports`, `.TestGoFiles`, `.TestSuite` — and not one is a test
// this guard could resolve. Pinning those as illustrative would have recorded
// the wrong reason: they are not test-shaped names meaning something else, they
// are not test names at all.
var citationRe = regexp.MustCompile(`(^|[^.\w])(Test[A-Z][A-Za-z0-9_]*)`)

// filenameRe is what a cited test FILE looks like in prose. The class excludes
// `/`, so a citation written as a path yields its base name alone and the
// directories are discarded.
//
// That is the resolution rule and not an accident of the regex: what this guard
// answers is whether the file EXISTS, not whether the path to it is spelled
// right. Prose here spells the same file at least four ways — bare
// (`split_test.go`), package-relative (`testdata/corpus_test.go.txt`),
// repo-relative, and brace-expanded (`internal/codegen/{age,neo4j}/
// corpus_test.go`) — and a reader keyed on the path calls the last two
// dangling.
//
// Measured against the tree this arrived on, by admitting `/` here and
// resolving a token that contains one with a stat under the repo root: the
// unresolved population goes from 1 distinct name to 3. Neither addition is
// rot. `testdata/corpus_test.go.txt` is correct relative to the two packages
// that write it, and `/corpus_test.go` is the tail of the brace-expanded
// spelling above, which is not a path any reader can stat.
//
// The hole that leaves is real and is a base-name collision: 36 of this
// checkout's 150 test files share a base name with another (`main_test.go`,
// `corpus_test.go`, `export_test.go` and 12 more), so a citation naming a
// DELETED a/x_test.go still resolves while b/x_test.go lives. Path resolution
// would close it and cost a pin on every correctly-spelled citation that is not
// repo-relative, which is most of them.
//
// `.txt` is admitted because `*_test.go.txt` is a real test source here — the
// corpus harnesses assemble those into a module and run them — and it is cited
// by name from the census tables beside them.
var filenameRe = regexp.MustCompile(`[A-Za-z0-9_-]+_test\.go(?:\.txt)?`)

// pinned is the set of comment sites that write a test-shaped name, or a
// test-file-shaped name, the guards below must not demand a declaration or a
// file for. Each entry needs a reason, and an entry naming a site that no
// longer writes the name fails the guard: a stale exemption is the same rot as
// a stale citation.
//
// One map carries both spellings. They cannot collide — a cited name begins
// `Test` and an upper-case letter, a cited file ends `_test.go` — and a second
// map would double the mechanism to hold a distinction the key already makes.
//
// Three kinds live here, and the reasons say which:
//
//   - ILLUSTRATIVE — the name denotes no test at all. A `go test -run` pattern,
//     a stand-in in a sentence about naming, this file's own TestX.
//   - HYPOTHETICAL — the name denotes a test nobody has written, and the prose
//     is only true while it does not resolve.
//   - HISTORICAL — the name denoted a real test, and the sentence is ABOUT its
//     removal or replacement. These are the ones that make one map necessary
//     rather than sufficient: a plain "does it resolve" reading calls them rot,
//     and repairing them the way rot is repaired would delete the fact the
//     sentence exists to record.
//
// The third kind arrived with this file's widening to _test.go comments, where
// a test's own prose routinely says what it replaced. Nothing distinguishes it
// syntactically from the first two, so it is not given its own map — the reason
// text is where the distinction is kept, and a reader repairing a dangling
// citation needs to read it either way.
//
// Pinning is deliberately explicit rather than syntactic. A rule like "a name
// in backticks is illustrative" would be defeated by the most natural thing an
// author can do to a real citation — put the identifier in backticks — and it
// would be defeated SILENTLY, which is the failure mode this guard exists to
// remove. Adding a name here is friction, and the friction is the point.
//
// The key is file plus name, not file plus line, so a pin covers every site in
// its file that writes the name. That is coarser than the finding it excuses:
// where a file writes one name in two roles, repairing the rotten site leaves a
// pin that would silently excuse a real citation of that name written there
// later. ghorphan's entry below is exactly that case and says so.
var pinned = map[string]map[string]string{
	"internal/liverecipes/recipes.go": {
		"TestAGERefuses": "a `go test -run` PATTERN, not a test. Its whole point is that it is a proper prefix of TestAGERefusesTheFunctionsItDoesNotDefine and selects it anyway.",
		"TestFoo":        "`TestFoo(A|B)`, a pattern illustrating the bracket-aware split this reader deliberately does not do.",
	},
	"internal/liverecipes/split.go": {
		"TestLiveSmokeFoo": "HYPOTHETICAL. The sentence is about an allowlist silently covering a test nobody has written yet, so the name must NOT resolve or the prose stops being true.",
	},
	"internal/codegen/age/dialect_test.go": {
		"TestSomethingLive":         "ILLUSTRATIVE. The name of a test the harness WRITES into a throwaway source; `witness` is that literal. The comment is about `-run` being unanchored, so it must name both spellings.",
		"TestSomethingLiveButUnrun": "ILLUSTRATIVE. The second synthetic test in the same throwaway source, named for the same reason.",
	},
	"internal/codegen/corpusrun/corpusrun_test.go": {
		"TestA": "ILLUSTRATIVE. A `Declared.Tests` fixture value two lines above, quoted so the comment's claim about sort order is checkable against the bytes beside it.",
		"TestB": "ILLUSTRATIVE. The other half of the same fixture pair.",
	},
	"internal/codegen/neo4j/corpus_test.go": {
		"TestFoo": "ILLUSTRATIVE. A stand-in in `renaming TestFoo to Testfoo`, a sentence about what `go test`'s vet pass refuses to build.",
	},
	"internal/tools/testcite/citations_test.go": {
		"TestX":                    "ILLUSTRATIVE. This guard's own prose, at three sites: the shapes a citation takes and the subtest form this reader deliberately does not resolve.",
		"TestFoo":                  "ILLUSTRATIVE. The name half of `foo_test.go`/TestFoo, the pair this file's own doc uses to say the two spellings rot the same way.",
		"foo_test.go":              "ILLUSTRATIVE. The file half of that same pair, at two sites: the package doc and this guard's own doc comment.",
		"x_test.go":                "ILLUSTRATIVE. A stand-in in `a/x_test.go` and `b/x_test.go`, the collision that base-name resolution cannot see. It must NOT resolve — a real file of that name would make the sentence's own example the thing it warns about.",
		"live_x_test.go":           "ILLUSTRATIVE. Quoted from a fixture TABLE in internal/liverecipes/split_test.go, in the sentence saying table data is not prose. It is quoted precisely because this reader never sees it as a citation.",
		"live_age_ungated_test.go": "HISTORICAL. The deleted file the filing instance rested on. Repaired in PR #2401, so the only thing left writing the name is this file's account of why the filename half exists — which is the sentence a repair would delete.",
	},
	"internal/cli/init_test.go": {
		"TestInitRefusesMultiTargetEdit": "HISTORICAL. TestInitRefusalNamesAddFlag replaced it; the sentence records that the two were not kept side by side because they differ only in the expected message.",
	},
	"internal/codegen/conformance/assembled_input_test.go": {
		"TestTemporalScanFindsTheEnumEnd": "HISTORICAL. Names the derivation this suite replaced — the one that scanned for String's first default-arm value. Resolving it would mean the replacement never happened.",
	},
	"internal/query/query_test.go": {
		"TestBindingDiscriminatorTracksEntityKind": "HISTORICAL. The name TestBindingDiscriminatorDerivesFromKind carried until bd gqlc-slde4, written in that test's own comment because the old name asserted something the body never did — it tracked BindingKind, not graph.EntityKind — and the record of that being the second correction to the same mistake is the point of the sentence. Resolving it would mean the rename never happened. The coarseness the doc comment above warns about is harmless here: a later REAL citation of this name in this file would itself be rot, since nothing will declare it again.",
	},
	"internal/resolver/undeclaredreltype_test.go": {
		"TestEndpointNarrowedButDeclaredTypeIsSilent": "HISTORICAL. The row that asserted the opposite boundary, replaced deliberately rather than deleted; the comment is the record of that choice.",
	},
	"internal/tools/ghorphan/main_test.go": {
		"TestANarrowerWindowTurnsAnAmbiguousRefusalIntoAClose": "HISTORICAL. The name this test carried before the ambiguity guard in plan(), when it pinned the flip as REACHABLE. NOTE the coarseness the doc comment above warns about: this file wrote the same name at a second site as genuine rot (a stale citation of the renamed test, repaired with this pin), so a real citation of it written here later would be excused silently.",
	},
	"internal/tools/modscope/justfile_test.go": {
		"hooktests_test.go": "HISTORICAL. The sentence is the record of this citation being DROPPED rather than repointed: the file went with the CI scaffolding in PR #1595 and the argument never needed it (bd gqlc-chep). Repairing it the way rot is repaired would delete the fact it exists to carry.",
	},
}

// citation is one cited token written in one comment block — a test name or a
// test file name, told apart by isFile. The two share a struct because they
// share a pin map and a staleness check; they are told apart where the
// resolution set and the repair advice differ, which is only in the two guards.
type citation struct {
	file   string
	line   int
	name   string
	isFile bool
}

// unwrap joins a comment block into a single line so a name split across two
// lines is read whole. Two shapes are joined differently: an ordinary line
// break becomes a space, and a line ending in `-` is a typographic
// hyphenation whose parts rejoin with neither the hyphen nor a space.
//
// Both are load-bearing, and the second is why this guard reads blocks rather
// than lines. internal/codegen/prepare.go hyphenates
// TestBlankParameterReachesOnlyTheSingleParameterForm across two lines; a
// line-at-a-time reader sees a truncated name, finds no declaration, and
// reports a citation nobody wrote.
func unwrap(text string) string {
	var b strings.Builder
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		switch cur := b.String(); {
		case strings.HasSuffix(cur, "-"):
			b.Reset()
			b.WriteString(strings.TrimSuffix(cur, "-"))
		case cur != "":
			b.WriteString(" ")
		}
		b.WriteString(line)
	}
	return b.String()
}

// scanRepo walks the whole checkout and returns the test functions it declares
// and the citations its comments write.
//
// Declarations are collected across module boundaries on purpose. test/data/
// codegen is its own module, and internal/liverecipes cites its TestLiveSmoke
// by name — that citation is exactly as able to rot as a within-module one, and
// skipping the submodule made the guard report it dangling (measured while
// writing this).
//
// They are also collected out of `*_test.go.txt`, which is a source file the
// corpus harnesses assemble into a module and run. Those are real tests, cited
// by name from the census tables in internal/codegen/{age,neo4j}/corpus_test.go,
// and a reader keyed on the `.go` suffix cannot see a single one of them: it
// called four true citations dangling.
//
// A fixture is read exactly like any other test source, comments included, and
// the two are not separable here: a fixture's prose cites the fixture's own
// tests, so a reader that collected the declarations and skipped the comments
// would be holding the half that cannot rot. The skip was there for one
// revision and no mutation could kill it — 1126 comment lines went unread and
// every citation in them already resolved.
func scanRepo(t *testing.T, root string) (declared, present map[string]bool, cites []citation, files int) {
	t.Helper()
	declared = map[string]bool{}
	present = map[string]bool{}

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			t.Fatalf("walking %s: %v", path, err)
		}
		if !d.IsDir() {
			// Every file's base name, recorded before the parse filter below
			// drops the ones this reader has no citations to collect from,
			// because the question a cited name is resolved against is "does
			// this file EXIST" and the filter answers "can this file be READ".
			//
			// Today those two sets coincide over everything filenameRe can
			// produce, since a token it matches ends `_test.go` or
			// `_test.go.txt` and the filter admits both: moving this line below
			// the filter is a mutation that survives, measured. It is here for
			// the question it answers and not for a difference it currently
			// makes.
			present[d.Name()] = true
		}
		if d.IsDir() {
			// The root is exempt from both skips. It is reached as a relative
			// path whose own base name starts with a dot, so a dot check
			// applied here prunes the whole walk — which is what the
			// non-degeneracy assertions below caught when it did.
			if path == root {
				return nil
			}
			if strings.HasPrefix(d.Name(), ".") {
				return fs.SkipDir
			}
			return nil
		}
		fixture := strings.HasSuffix(path, "_test.go.txt")
		if !strings.HasSuffix(path, ".go") && !fixture {
			return nil
		}
		fset := token.NewFileSet()
		f, parseErr := parser.ParseFile(fset, path, nil, parser.ParseComments)
		// Strict on purpose. Skipping a file this reader cannot parse would
		// drop its citations silently, which is the fail-silent shape this
		// guard exists to remove; if a deliberately-malformed Go fixture ever
		// lands here, exempt it by path rather than making the walk lenient.
		if parseErr != nil {
			t.Fatalf("every .go file in this checkout must parse: %s: %v", path, parseErr)
		}
		files++

		if strings.HasSuffix(path, "_test.go") || fixture {
			for _, decl := range f.Decls {
				if fn, ok := decl.(*ast.FuncDecl); ok {
					// Suite methods count. A reader that took only
					// top-level funcs would call every `func (s *Suite)
					// TestX` citation dangling — which is exactly the
					// reading that put two false positives in this bead.
					declared[fn.Name.Name] = true
				}
			}
		}

		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			t.Fatalf("relative path for %s: %v", path, relErr)
		}
		rel = filepath.ToSlash(rel)
		for _, group := range f.Comments {
			line := fset.Position(group.Pos()).Line
			// Deduplicated per block: a name repeated inside one comment is one
			// citation to repair, and reporting it three times only buries the
			// other findings.
			seen := map[string]bool{}
			text := unwrap(group.Text())
			for _, m := range citationRe.FindAllStringSubmatch(text, -1) {
				name := m[2]
				if seen[name] {
					continue
				}
				seen[name] = true
				cites = append(cites, citation{file: rel, line: line, name: name})
			}
			for _, m := range filenameRe.FindAllString(text, -1) {
				if seen[m] {
					continue
				}
				seen[m] = true
				cites = append(cites, citation{file: rel, line: line, name: m, isFile: true})
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}
	return declared, present, cites, files
}

// TestEveryCitedTestExists is the guard. A comment in a non-test source that
// names a test names one that is declared, or the site is pinned above with a
// reason.
//
// Test sources are in scope. They were not until bd gqlc-som6y: the reading
// stopped at non-test sources, and a test's own prose is where this repository
// most often says what a neighbouring test replaced, so it is where a rename
// most often rots. Widening it took the unresolved population to 22 on master,
// of which 6 were the reader's own blindness (four census names cited out of
// `*_test.go.txt` fixtures, two `.TestImports` selectors), 4 were real rot, and
// 12 are pinned above.
func TestEveryCitedTestExists(t *testing.T) {
	declared, _, cites, files := scanRepo(t, repoRoot)

	// Non-degeneracy, asserted per source rather than on a total. A walk that
	// found no files, no declarations, or no citations would satisfy the loop
	// below vacuously and report a green guard over nothing.
	if files == 0 {
		t.Fatal("the walk found no Go files, so the guard examined nothing")
	}
	if len(declared) == 0 {
		t.Fatal("the walk found no test declarations, so every citation would read as dangling")
	}
	named := 0
	for _, c := range cites {
		if !c.isFile {
			named++
		}
	}
	if named == 0 {
		t.Fatal("the walk found no cited test names, so the loop below proves nothing")
	}

	for _, c := range cites {
		if c.isFile || declared[c.name] {
			continue
		}
		if _, ok := pinned[c.file][c.name]; ok {
			continue
		}
		t.Errorf("%s:%d cites %s, which no test in this checkout declares.\n"+
			"Either the test was renamed or deleted — repair the citation, or remove it and say so if the test is gone —\n"+
			"or the name is not a live citation, in which case add it to `pinned` with the reason: illustrative,\n"+
			"hypothetical, or historical (a sentence ABOUT a test that was deliberately replaced).",
			c.file, c.line, c.name)
	}

	t.Logf("examined %d Go files: %d declared test functions, %d citations, %d pinned",
		files, len(declared), len(cites), pinCount())
}

// TestEveryCitedTestFileExists is the filename half. A comment that rests its
// argument on `foo_test.go` names a file this checkout has, or the site is
// pinned above with a reason.
//
// It is a sibling of TestEveryCitedTestExists rather than a branch inside it
// because the two resolve against different sets — a declared function against
// a file on disk — and hand back different repair advice. Sharing scanRepo is
// what keeps them one reader; sharing a loop would make each one's
// non-degeneracy check answer for the other's population, and it is exactly
// that substitution this pair is guarding against.
//
// Measured at the commit that added this: 31 distinct cited file names over 65
// sites, of which one is unresolved and pinned. The bead reported six; five of
// those are `path: "live_x_test.go"` rows in a fixture TABLE, which a comment
// reader never sees and which pinning would have recorded as prose that does
// not exist.
func TestEveryCitedTestFileExists(t *testing.T) {
	_, present, cites, files := scanRepo(t, repoRoot)

	if files == 0 {
		t.Fatal("the walk found no Go files, so the guard examined nothing")
	}
	if len(present) == 0 {
		t.Fatal("the walk recorded no file names, so every cited file would read as deleted")
	}
	named := 0
	for _, c := range cites {
		if c.isFile {
			named++
		}
	}
	if named == 0 {
		t.Fatal("the walk found no cited file names, so the loop below proves nothing")
	}

	for _, c := range cites {
		if !c.isFile || present[c.name] {
			continue
		}
		if _, ok := pinned[c.file][c.name]; ok {
			continue
		}
		t.Errorf("%s:%d cites %s, which no file in this checkout is named.\n"+
			"Either the file was renamed or deleted — repoint the citation, or drop it and say so if the argument\n"+
			"no longer needs it — or the name is not a live citation, in which case add it to `pinned` with the\n"+
			"reason: illustrative, hypothetical, or historical (a sentence ABOUT a file that was deliberately removed).",
			c.file, c.line, c.name)
	}

	t.Logf("examined %d Go files: %d distinct file names, %d cited file names", files, len(present), named)
}

// TestEveryPinIsStillWritten refuses a pin whose site no longer writes the
// name. Without it the map only ever grows: a prose edit that drops a pinned
// name leaves an exemption behind that quietly excuses the next author who
// writes a real citation of that name in that file.
func TestEveryPinIsStillWritten(t *testing.T) {
	_, _, cites, _ := scanRepo(t, repoRoot)

	written := map[string]map[string]bool{}
	for _, c := range cites {
		if written[c.file] == nil {
			written[c.file] = map[string]bool{}
		}
		written[c.file][c.name] = true
	}

	if len(pinned) == 0 {
		t.Fatal("with no pins this test proves nothing")
	}
	for file, names := range pinned {
		for name := range names {
			if !written[file][name] {
				t.Errorf("%s is pinned in %s, but no comment there writes it — drop the pin", name, file)
			}
		}
	}
}

func pinCount() int {
	n := 0
	for _, names := range pinned {
		n += len(names)
	}
	return n
}
