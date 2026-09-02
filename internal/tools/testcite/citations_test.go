// Package testcite holds one guard: a comment in a non-test source that names
// a test must name a test that exists.
//
// This repository routinely settles a question in a comment and cites the test
// holding the answer — "TestX pins the equality", "TestX is why this is `== 1`".
// The citation is prose, so a rename or a deletion leaves it pointing at
// nothing, and the next reader takes the name as evidence the claim is still
// measured. Nothing noticed that until this file (bd gqlc-945h7).
//
// It is a test rather than a command so that `just test` carries it. A separate
// gate arm would be one more edge somebody can drop and leave the tree green.
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

	"github.com/stretchr/testify/require"
)

// repoRoot reaches the module root from this package's directory.
const repoRoot = "../../.."

// citationRe is what a cited test name looks like in prose. The trailing class
// deliberately excludes `/`, so `TestX/"a subtest"` yields the top-level name
// alone — the subtest is a string literal in a t.Run call, not a declaration
// this guard can resolve, and demanding it would be a check on something the
// AST does not answer.
var citationRe = regexp.MustCompile(`\bTest[A-Z][A-Za-z0-9_]*`)

// illustrative is the set of comment sites that write a test-shaped name
// meaning something other than a test that exists. Each entry needs a reason,
// and an entry naming a site that no longer writes the name fails the guard
// below: a stale exemption is the same rot as a stale citation.
//
// Pinning is deliberately explicit rather than syntactic. A rule like "a name
// in backticks is illustrative" would be defeated by the most natural thing an
// author can do to a real citation — put the identifier in backticks — and it
// would be defeated SILENTLY, which is the failure mode this guard exists to
// remove. Adding a name here is friction, and the friction is the point.
var illustrative = map[string]map[string]string{
	"internal/liverecipes/recipes.go": {
		"TestAGERefuses": "a `go test -run` PATTERN, not a test. Its whole point is that it is a proper prefix of TestAGERefusesTheFunctionsItDoesNotDefine and selects it anyway.",
		"TestFoo":        "`TestFoo(A|B)`, a pattern illustrating the bracket-aware split this reader deliberately does not do.",
	},
	"internal/liverecipes/split.go": {
		"TestLiveSmokeFoo": "a HYPOTHETICAL later test. The sentence is about an allowlist silently covering a test nobody has written yet, so the name must NOT resolve or the prose stops being true.",
	},
}

// citation is one test name written in one comment block.
type citation struct {
	file string
	line int
	name string
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
// and the citations its non-test sources write.
//
// Declarations are collected across module boundaries on purpose. test/data/
// codegen is its own module, and internal/liverecipes cites its TestLiveSmoke
// by name — that citation is exactly as able to rot as a within-module one, and
// skipping the submodule made the guard report it dangling (measured while
// writing this).
func scanRepo(t *testing.T, root string) (declared map[string]bool, cites []citation, files int) {
	t.Helper()
	declared = map[string]bool{}

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		require.NoError(t, err)
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
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		fset := token.NewFileSet()
		f, parseErr := parser.ParseFile(fset, path, nil, parser.ParseComments)
		// Strict on purpose. Skipping a file this reader cannot parse would
		// drop its citations silently, which is the fail-silent shape this
		// guard exists to remove; if a deliberately-malformed Go fixture ever
		// lands here, exempt it by path rather than making the walk lenient.
		require.NoError(t, parseErr, "every .go file in this checkout must parse: %s", path)
		files++

		if strings.HasSuffix(path, "_test.go") {
			for _, decl := range f.Decls {
				if fn, ok := decl.(*ast.FuncDecl); ok {
					// Suite methods count. A reader that took only
					// top-level funcs would call every `func (s *Suite)
					// TestX` citation dangling — which is exactly the
					// reading that put two false positives in this bead.
					declared[fn.Name.Name] = true
				}
			}
			return nil
		}

		rel, relErr := filepath.Rel(root, path)
		require.NoError(t, relErr)
		rel = filepath.ToSlash(rel)
		for _, group := range f.Comments {
			line := fset.Position(group.Pos()).Line
			// Deduplicated per block: a name repeated inside one comment is one
			// citation to repair, and reporting it three times only buries the
			// other findings.
			seen := map[string]bool{}
			for _, name := range citationRe.FindAllString(unwrap(group.Text()), -1) {
				if seen[name] {
					continue
				}
				seen[name] = true
				cites = append(cites, citation{file: rel, line: line, name: name})
			}
		}
		return nil
	})
	require.NoError(t, err)
	return declared, cites, files
}

// TestEveryCitedTestExists is the guard. A comment in a non-test source that
// names a test names one that is declared, or the site is pinned above with a
// reason.
//
// Test sources are out of scope, and not because their comments cannot rot.
// Measured 2026-09-02 on 95935159: extending the same reading to _test.go
// comments takes the unresolved population from 3 to 20, most of them
// hypothetical names in census tables and in prose about `go test -run`. That
// is a different and larger job, filed rather than smuggled in here.
func TestEveryCitedTestExists(t *testing.T) {
	declared, cites, files := scanRepo(t, repoRoot)

	// Non-degeneracy, asserted per source rather than on a total. A walk that
	// found no files, no declarations, or no citations would satisfy the loop
	// below vacuously and report a green guard over nothing.
	require.NotEmpty(t, files, "the walk found no Go files, so the guard examined nothing")
	require.NotEmpty(t, declared, "the walk found no test declarations, so every citation would read as dangling")
	require.NotEmpty(t, cites, "the walk found no citations, so the loop below proves nothing")

	for _, c := range cites {
		if declared[c.name] {
			continue
		}
		if _, pinned := illustrative[c.file][c.name]; pinned {
			continue
		}
		t.Errorf("%s:%d cites %s, which no _test.go in this checkout declares.\n"+
			"Either the test was renamed or deleted — repair or remove the citation, and say so if the test is gone —\n"+
			"or the name is illustrative, in which case pin it in `illustrative` with the reason it is not a citation.",
			c.file, c.line, c.name)
	}

	t.Logf("examined %d Go files: %d declared test functions, %d citations in non-test sources, %d pinned as illustrative",
		files, len(declared), len(cites), pinCount())
}

// TestEveryIllustrativePinIsStillWritten refuses a pin whose site no longer
// writes the name. Without it the map only ever grows: a prose edit that drops
// an illustrative name leaves an exemption behind that quietly excuses the next
// author who writes a real citation of that name in that file.
func TestEveryIllustrativePinIsStillWritten(t *testing.T) {
	_, cites, _ := scanRepo(t, repoRoot)

	written := map[string]map[string]bool{}
	for _, c := range cites {
		if written[c.file] == nil {
			written[c.file] = map[string]bool{}
		}
		written[c.file][c.name] = true
	}

	require.NotEmpty(t, illustrative, "with no pins this test proves nothing")
	for file, names := range illustrative {
		for name := range names {
			require.True(t, written[file][name],
				"%s is pinned as illustrative in %s, but no comment there writes it — drop the pin", name, file)
		}
	}
}

func pinCount() int {
	n := 0
	for _, names := range illustrative {
		n += len(names)
	}
	return n
}
