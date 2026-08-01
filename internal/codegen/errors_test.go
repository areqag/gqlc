package codegen

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/suite"
)

// taxonomyDoc is the live index of this package's sentinels. The stage
// specs under docs/specs/codegen-stage-c*.md carry per-stage snapshots
// of the same taxonomy and are deliberately not held against the code:
// they record what each stage shipped, down to sentinel names since
// renamed.
const taxonomyDoc = "../../docs/specs/codegen-sentinel-taxonomy.md"

// The document's four tables, keyed by the heading each sits under.
// Every table carries the sentinel in its first column, so one parser
// serves all four.
const (
	indexHeading     = "## 1. Index — the reachable set"
	taxonomyHeading  = "## 2. Refusal taxonomy — construct to sentinel"
	unreachedHeading = "## 3. Branches no input reaches"
	excludedHeading  = "## 4. Declared and deliberately unreachable"
)

// sentinelCell matches a first-column cell naming a sentinel.
var sentinelCell = regexp.MustCompile("^`(Err[A-Za-z0-9]+)`$")

// SentinelTaxonomySuite fences the documented taxonomy against the code.
// Nothing but this test connects the two, and the tables are what a
// contributor reads to decide which sentinel a new refusal belongs
// under, so drift there misroutes work rather than merely reading
// stale.
type SentinelTaxonomySuite struct {
	suite.Suite

	// declared pairs every Err* value the package's non-test sources
	// declare with the message it was constructed from.
	declared map[string]string
	// identOf names each allSentinels member. Sentinels are opaque
	// values at run time; the errors.New argument is the only text the
	// slice and the document share.
	identOf map[error]string
	// reachable is the set of allSentinels members by name; the rest are
	// the document's four tables, in document order.
	reachable   map[string]bool
	indexed     []string
	taxonomised []string
	unreached   []string
	excluded    []string
}

func TestSentinelTaxonomy(t *testing.T) {
	suite.Run(t, new(SentinelTaxonomySuite))
}

func (s *SentinelTaxonomySuite) SetupSuite() {
	s.declared = s.declaredSentinels()

	byMessage := make(map[string]string, len(s.declared))
	for ident, msg := range s.declared {
		other, dup := byMessage[msg]
		s.Require().False(dup,
			"%s and %s are both errors.New(%q); the fence maps a sentinel value back to its name through that message, so the messages must be distinct",
			other, ident, msg)
		byMessage[msg] = ident
	}

	s.identOf = make(map[error]string, len(allSentinels))
	s.reachable = make(map[string]bool, len(allSentinels))
	for _, sentinel := range allSentinels {
		ident, ok := byMessage[sentinel.Error()]
		s.Require().True(ok,
			"allSentinels holds a value with message %q that no Err* declaration in errors.go constructs; the fence reads that file to name the slice's members",
			sentinel.Error())
		s.identOf[sentinel] = ident
		s.reachable[ident] = true
	}

	doc := s.readDoc()
	s.indexed = s.tableColumn(doc, indexHeading)
	s.taxonomised = s.tableColumn(doc, taxonomyHeading)
	s.unreached = s.tableColumn(doc, unreachedHeading)
	s.excluded = s.tableColumn(doc, excludedHeading)
}

// TestIndexMatchesReachableSet is the fence's core: §1 and allSentinels
// name the same sentinels, neither side holding one the other does not.
func (s *SentinelTaxonomySuite) TestIndexMatchesReachableSet() {
	indexed := make(map[string]bool, len(s.indexed))
	for _, ident := range s.indexed {
		s.Require().False(indexed[ident], "%s has two rows in %s %s", ident, taxonomyDoc, indexHeading)
		indexed[ident] = true
	}

	for _, sentinel := range allSentinels {
		ident := s.identOf[sentinel]
		s.Require().True(indexed[ident],
			"%s is in allSentinels but has no row in %s under %q; add one there naming what it refuses and what introduced it, plus a row per refused construct under %q",
			ident, taxonomyDoc, indexHeading, taxonomyHeading)
	}
	for _, ident := range s.indexed {
		s.Require().True(s.reachable[ident],
			"%s %s documents %s, which is not in allSentinels; either add it to the slice in errors.go (with a negative fixture under test/data/codegen/invalid) or move its row to %q",
			taxonomyDoc, indexHeading, ident, excludedHeading)
	}
}

// TestTaxonomyCoversTheIndex holds §2 against §1 both ways: no sentinel
// without a construct that reaches it, no construct routed to a
// sentinel the index does not carry.
func (s *SentinelTaxonomySuite) TestTaxonomyCoversTheIndex() {
	covered := make(map[string]bool, len(s.taxonomised))
	for _, ident := range s.taxonomised {
		s.Require().True(s.reachable[ident],
			"%s %s routes a construct to %s, which is not in allSentinels; the taxonomy may only name sentinels the reachable set carries",
			taxonomyDoc, taxonomyHeading, ident)
		covered[ident] = true
	}
	for _, sentinel := range allSentinels {
		ident := s.identOf[sentinel]
		s.Require().True(covered[ident],
			"%s is in allSentinels but no row under %q in %s names a construct that reaches it; a sentinel nothing routes to cannot be chosen from the table, and a sentinel whose every branch belongs under %q does not belong in the slice",
			ident, taxonomyHeading, taxonomyDoc, unreachedHeading)
	}
}

// TestUnreachedBranchesNameRealSentinels holds §3 against the code.
// Its rows carry no coverage obligation — nothing can fire them — so the
// only claim to check is that the sentinel each would carry still
// exists and is still one the front end can return.
func (s *SentinelTaxonomySuite) TestUnreachedBranchesNameRealSentinels() {
	for _, ident := range s.unreached {
		s.Require().True(s.reachable[ident],
			"%s %s carries a branch under %s, which is not in allSentinels; a branch nothing reaches still fails with a sentinel the slice must hold",
			taxonomyDoc, unreachedHeading, ident)
	}
}

// TestDeclaredSentinelsAreAccounted holds the document against the whole
// of errors.go, not just the slice: a sentinel declared and left out of
// allSentinels is the drift §1 alone cannot see, because both sides
// agree on a set that never grew.
func (s *SentinelTaxonomySuite) TestDeclaredSentinelsAreAccounted() {
	accounted := make(map[string]bool, len(s.indexed)+len(s.excluded))
	for _, ident := range s.indexed {
		accounted[ident] = true
	}
	for _, ident := range s.excluded {
		s.Require().False(s.reachable[ident],
			"%s %s calls %s unreachable, but allSentinels holds it; the two claims cannot both stand",
			taxonomyDoc, excludedHeading, ident)
		s.Require().False(accounted[ident], "%s has a row under both %q and %q in %s", ident, indexHeading, excludedHeading, taxonomyDoc)
		accounted[ident] = true
	}

	for ident := range s.declared {
		s.Require().True(accounted[ident],
			"package codegen declares %s, which %s documents nowhere; add it to allSentinels and to %q, or leave it out of the slice and say why under %q",
			ident, taxonomyDoc, indexHeading, excludedHeading)
	}
	for ident := range accounted {
		_, ok := s.declared[ident]
		s.Require().True(ok, "%s documents %s, which package codegen no longer declares", taxonomyDoc, ident)
	}
}

// declaredSentinels pairs every package-level Err* value in the package's
// non-test sources with its errors.New argument. Read from source rather
// than through a hand-written name table because such a table is one more
// mirror of the same set, and would drift the way the document did.
//
// Every source file, not just errors.go: nothing stops a sentinel being
// declared beside the code that returns it, and a sweep that reads one
// file would call such a sentinel undeclared and pass.
func (s *SentinelTaxonomySuite) declaredSentinels() map[string]string {
	entries, err := os.ReadDir(".")
	s.Require().NoError(err)

	out := make(map[string]string)
	files := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		files++
		s.collectSentinels(name, out)
	}
	s.Require().NotZero(files, "no non-test .go files found; the fence ran from the wrong directory and would pass vacuously")
	s.Require().NotEmpty(out, "no Err* declarations found in package codegen — the fence would pass vacuously")
	return out
}

// collectSentinels adds one file's package-level Err* declarations to out.
func (s *SentinelTaxonomySuite) collectSentinels(path string, out map[string]string) {
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	s.Require().NoError(err)

	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.VAR {
			continue
		}
		for _, spec := range gen.Specs {
			value, ok := spec.(*ast.ValueSpec)
			s.Require().True(ok, "%s: a var declaration the fence cannot read", path)
			if !slices.ContainsFunc(value.Names, isSentinelIdent) {
				continue
			}
			s.Require().Len(value.Values, len(value.Names),
				"%s: a sentinel is declared in a spec whose names and values do not pair up one to one; the fence reads each name's own errors.New message",
				path)
			for i, ident := range value.Names {
				if !isSentinelIdent(ident) {
					continue
				}
				msg, ok := errorsNewArgument(value.Values[i])
				s.Require().True(ok, "%s: %s is declared as something other than errors.New(<string literal>); the fence reads that literal to name the value", path, ident.Name)
				out[ident.Name] = msg
			}
		}
	}
}

func isSentinelIdent(ident *ast.Ident) bool { return strings.HasPrefix(ident.Name, "Err") }

// errorsNewArgument returns the string literal an errors.New call was
// given.
func errorsNewArgument(expr ast.Expr) (string, bool) {
	call, ok := expr.(*ast.CallExpr)
	if !ok || len(call.Args) != 1 {
		return "", false
	}
	fn, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || fn.Sel.Name != "New" {
		return "", false
	}
	pkg, ok := fn.X.(*ast.Ident)
	if !ok || pkg.Name != "errors" {
		return "", false
	}
	lit, ok := call.Args[0].(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	msg, err := strconv.Unquote(lit.Value)
	return msg, err == nil
}

func (s *SentinelTaxonomySuite) readDoc() []string {
	src, err := os.ReadFile(taxonomyDoc)
	s.Require().NoError(err)
	return strings.Split(string(src), "\n")
}

// tableColumn returns the sentinel named by each row of the table under
// heading, in document order. A row whose first cell is neither the
// column header, the separator, nor a backticked Err* name fails rather
// than being skipped — a typo must not read as an absent row.
func (s *SentinelTaxonomySuite) tableColumn(doc []string, heading string) []string {
	var out []string
	found := false
	for _, line := range doc {
		if strings.HasPrefix(line, "## ") {
			if found {
				break
			}
			found = line == heading
			continue
		}
		if !found || !strings.HasPrefix(line, "|") {
			continue
		}
		cell := strings.TrimSpace(strings.Split(strings.Trim(line, "|"), "|")[0])
		if cell == "Sentinel" || strings.Trim(cell, "-:") == "" {
			continue
		}
		m := sentinelCell.FindStringSubmatch(cell)
		s.Require().NotNil(m, "%s: row under %q starts with %q, which is not a backticked sentinel name", taxonomyDoc, heading, cell)
		out = append(out, m[1])
	}
	s.Require().True(found, "%s has no %q heading; the fence keys its tables off the heading text", taxonomyDoc, heading)
	s.Require().NotEmpty(out, "%s: the table under %q has no rows", taxonomyDoc, heading)
	return out
}
