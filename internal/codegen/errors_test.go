package codegen

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
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

// moduleRoot is the repository root, relative to this package.
const moduleRoot = "../.."

// codegenPkgPath is this package's import path. The coverage sweep needs
// it as a -coverpkg argument and as the prefix a profile line carries.
const codegenPkgPath = "github.com/areqag/gqlc/internal/codegen"

// conformancePkgPath runs every fixture under test/data/codegen. The
// corpus sweep derives its package set rather than listing it, so this
// is not a mirror of that set — it is the one member whose absence the
// derivation cannot report, because a sweep that lost the negative
// fixtures still finds every tagged branch unreached and passes.
const conformancePkgPath = "github.com/areqag/gqlc/internal/codegen/conformance"

// sentinelCell matches a first-column cell naming a sentinel.
var sentinelCell = regexp.MustCompile("^`(Err[A-Za-z0-9]+)`$")

// siteCell matches one backticked fail-site name in §3's second column.
var siteCell = regexp.MustCompile("`([a-z][a-z0-9-]*)`")

// unreachableTag matches the directive marking a fail-site §3 records as
// reachable by no input. It sits on its own line directly above the
// return it describes.
var unreachableTag = regexp.MustCompile(`^//gqlc:unreachable ([a-z][a-z0-9-]*)$`)

// The stage specs' two historical anchors, and the note each must carry.
// A reader who greps for a construct or a sentinel lands on one of these
// and has no other way to tell the answer is years old.
var (
	stageSpecGlob     = filepath.Join(moduleRoot, "docs", "specs", "codegen-stage-c*.md")
	stageSentinelHead = regexp.MustCompile(`^## \d+\. Sentinel sets?\b`)
	stageTableHead    = "**Out of scope, routed to the appropriate sentinel:**"
	historicalNote    = "*Historical: what this stage shipped, sentinel names included; it takes " +
		"no further edit. The current answer is " +
		"[docs/specs/codegen-sentinel-taxonomy.md](codegen-sentinel-taxonomy.md), " +
		"which `TestSentinelTaxonomy` holds against the code.*"
)

// failSite is one //gqlc:unreachable tag: where it sits, and which
// sentinel the return it guards carries. The sentinel is read off the
// AST rather than written into the tag — a tag that named its own
// sentinel would be one more hand-maintained mirror.
type failSite struct {
	file     string
	line     int // the tagged return statement's line
	sentinel string
}

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

	// unreachedSites maps each fail-site named in §3's second column to
	// the sentinel of the row that names it, and tagged holds the
	// //gqlc:unreachable tags the package's sources carry.
	unreachedSites map[string]string
	tagged         map[string]failSite
}

func TestSentinelTaxonomy(t *testing.T) {
	suite.Run(t, new(SentinelTaxonomySuite))
}

func (s *SentinelTaxonomySuite) SetupSuite() {
	s.declared, s.tagged = s.scanSources()

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
			"allSentinels holds a value with message %q that no Err* declaration in package codegen constructs; the fence reads those declarations to name the slice's members, so a member must be an Err-named errors.New with a message no other member shares",
			sentinel.Error())
		// A repeated member collapses into identOf and reachable and is
		// invisible to every set comparison below, so the slice's own
		// shape is checked here rather than inferred from them.
		_, dup := s.identOf[sentinel]
		s.Require().False(dup, "allSentinels lists %s twice; the set is closed, so a repeat is an editing slip and hides itself from every check that reads the slice as a set", ident)
		s.identOf[sentinel] = ident
		s.reachable[ident] = true
	}

	doc := s.readDoc()
	s.indexed = s.tableColumn(doc, indexHeading)
	s.taxonomised = s.tableColumn(doc, taxonomyHeading)
	s.excluded = s.tableColumn(doc, excludedHeading)

	s.unreachedSites = make(map[string]string)
	for _, row := range s.tableRows(doc, unreachedHeading) {
		ident := s.sentinelOfRow(row, unreachedHeading)
		s.unreached = append(s.unreached, ident)
		s.Require().Len(row, 4, "%s: row %s under %q must carry sentinel, fail-site, branch and why", taxonomyDoc, ident, unreachedHeading)
		sites := siteCell.FindAllStringSubmatch(row[1], -1)
		s.Require().NotEmpty(sites,
			"%s %s: row %s names no fail-site; column two carries the backticked //gqlc:unreachable site name(s) of the branch(es) the row describes",
			taxonomyDoc, unreachedHeading, ident)
		for _, m := range sites {
			first, dup := s.unreachedSites[m[1]]
			s.Require().False(dup, "%s %s: fail-site %s is named by two rows (%s and %s); one branch, one row", taxonomyDoc, unreachedHeading, m[1], first, ident)
			s.unreachedSites[m[1]] = ident
		}
	}
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

// TestUnreachedBranchesAreTagged pairs §3's rows with the
// //gqlc:unreachable tags the package's sources carry: same set of
// fail-sites, and the same sentinel on each. Without it §3 is prose —
// its rows describe branches nothing points at, so any construct could
// be relabelled unreachable by moving a row between two tables.
func (s *SentinelTaxonomySuite) TestUnreachedBranchesAreTagged() {
	for site, ident := range s.unreachedSites {
		tag, ok := s.tagged[site]
		s.Require().True(ok,
			"%s %s names fail-site %s under %s, but no source file in package codegen carries //gqlc:unreachable %s; tag the return that branch fails through, or drop the row",
			taxonomyDoc, unreachedHeading, site, ident, site)
		s.Require().Equal(ident, tag.sentinel,
			"%s %s files fail-site %s under %s, but %s:%d fails with %s; the row and the branch must name one sentinel",
			taxonomyDoc, unreachedHeading, site, ident, tag.file, tag.line, tag.sentinel)
	}
	for site, tag := range s.tagged {
		_, ok := s.unreachedSites[site]
		s.Require().True(ok,
			"%s:%d carries //gqlc:unreachable %s, which %s documents nowhere; add a row under %q naming the site, the branch and why nothing reaches it, or drop the tag",
			tag.file, tag.line, site, taxonomyDoc, unreachedHeading)
	}
}

// TestUnreachedBranchesAreUnreached is the measurement behind §3. Every
// tagged branch must go unexecuted when the corpus runs: the fixture
// conformance suite, the backend suites and the CLI, i.e. every package
// whose test binary links this one.
//
// The set is derived from `go list` rather than listed, so a new consumer
// joins it by existing. This package is excluded on purpose and it is the
// whole point of the exclusion: a test inside package codegen can call an
// unexported function with a hand-built resolver value and reach a branch
// no parser can produce, and counting that as reachability is exactly how
// three unreachable branches came to sit in §2 — one of them the twin of
// a branch that had sat in §3 all along, the two differing in nothing but
// which had attracted a unit test. Excluding this package's own tests is
// what stops that difference from being expressible.
//
// The measurement is one-directional by design. A tagged branch the
// corpus reaches fails; an untagged branch the corpus misses does not,
// because "no fixture covers it yet" and "no input can reach it" are
// different claims and only the second is what a tag asserts. So the
// error it cannot make is the fail-open one: silencing a live refusal.
func (s *SentinelTaxonomySuite) TestUnreachedBranchesAreUnreached() {
	counts := s.corpusCoverage()

	sites := make([]string, 0, len(s.tagged))
	for site := range s.tagged {
		sites = append(sites, site)
	}
	slices.Sort(sites)

	for _, site := range sites {
		tag := s.tagged[site]
		count, ok := counts.blockFor(codegenPkgPath+"/"+tag.file, tag.line)
		s.Require().True(ok,
			"the corpus coverage profile has no block covering %s:%d, the branch tagged //gqlc:unreachable %s; the fence cannot measure a line the profile does not carry",
			tag.file, tag.line, site)
		s.Require().Zero(count,
			"%s:%d is tagged //gqlc:unreachable %s and %s %s says nothing reaches it, but %d of the corpus's test binaries execute it; it is a reachable construct, so move its row to %q with the input that reaches it, and drop the tag",
			tag.file, tag.line, site, taxonomyDoc, unreachedHeading, count, taxonomyHeading)
	}
}

// TestStageSpecsReadAsHistory holds the C0–C6 stage specs' sentinel
// sections and construct-to-sentinel tables against the note that says
// they are history. The tables are frozen by design, so nothing else can
// tell a reader grepping for a construct that they have landed in the
// past — C5's table is the series' last and nineteen of its twenty rows
// carry no self-signal at all.
func (s *SentinelTaxonomySuite) TestStageSpecsReadAsHistory() {
	specs, err := filepath.Glob(stageSpecGlob)
	s.Require().NoError(err)
	s.Require().NotEmpty(specs, "no stage specs matched %s; the fence would pass vacuously", stageSpecGlob)

	for _, spec := range specs {
		src, err := os.ReadFile(spec) //nolint:gosec // fixed glob under docs/specs
		s.Require().NoError(err)
		lines := strings.Split(string(src), "\n")

		anchors := 0
		for i, line := range lines {
			trimmed := strings.TrimSpace(line)
			if !stageSentinelHead.MatchString(trimmed) && trimmed != stageTableHead {
				continue
			}
			anchors++
			s.Require().Equal(historicalNote, paragraphAfter(lines, i),
				"%s:%d — %q is a snapshot the fence deliberately does not hold against the code, so it must carry the note that says so, verbatim and as the paragraph directly below it",
				spec, i+1, trimmed)
		}
		s.Require().NotZero(anchors, "%s carries no sentinel section; the fence keys off %q and %q", spec, stageSentinelHead, stageTableHead)
	}
}

// paragraphAfter returns the first non-empty paragraph below lines[i],
// whitespace-normalised so the note may wrap however the document wraps.
func paragraphAfter(lines []string, i int) string {
	var para []string
	for _, line := range lines[i+1:] {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			if len(para) > 0 {
				break
			}
			continue
		}
		para = append(para, trimmed)
	}
	return strings.Join(para, " ")
}

// coverLine matches one block of a Go coverage profile, whose form is
// `<import path>/<file>:<startLine>.<startCol>,<endLine>.<endCol> <statements> <count>`.
var coverLine = regexp.MustCompile(`^(.+):(\d+)\.(\d+),(\d+)\.(\d+) (\d+) (\d+)$`)

// coverCounts holds a merged coverage profile: one summed execution count
// per source block. `go test` over several packages concatenates a
// profile per test binary, so the same block appears once per binary and
// the counts must be added rather than read off the first hit.
type coverCounts map[coverBlock]int

type coverBlock struct {
	file                string
	startLine, startCol int
	endLine, endCol     int
}

// blockFor returns the count of the innermost profiled block containing
// file:line. Innermost because a fail-site's own basic block is the
// tightest range over it, and it is that block's count — not the
// function's — that answers whether the branch ran.
func (c coverCounts) blockFor(file string, line int) (int, bool) {
	var best coverBlock
	found := false
	for block := range c {
		if block.file != file || line < block.startLine || line > block.endLine {
			continue
		}
		if found && block.endLine-block.startLine >= best.endLine-best.startLine {
			continue
		}
		best, found = block, true
	}
	return c[best], found
}

// corpusCoverage runs every package that depends on internal/codegen
// under coverage of internal/codegen, and returns the merged profile.
func (s *SentinelTaxonomySuite) corpusCoverage() coverCounts {
	pkgs := s.corpusPackages()
	profile := filepath.Join(s.T().TempDir(), "corpus.cov")

	args := []string{"test", "-count=1", "-covermode=set", "-coverpkg=" + codegenPkgPath, "-coverprofile=" + profile}
	s.run(append(args, pkgs...)...)

	src, err := os.ReadFile(profile) //nolint:gosec // path built from t.TempDir
	s.Require().NoError(err)

	counts := make(coverCounts)
	for _, line := range strings.Split(string(src), "\n") {
		if line == "" || strings.HasPrefix(line, "mode:") {
			continue
		}
		m := coverLine.FindStringSubmatch(line)
		s.Require().NotNil(m, "unparsable coverage line %q", line)
		block := coverBlock{
			file:      m[1],
			startLine: s.atoi(m[2]),
			startCol:  s.atoi(m[3]),
			endLine:   s.atoi(m[4]),
			endCol:    s.atoi(m[5]),
		}
		counts[block] += s.atoi(m[7])
	}
	s.Require().NotEmpty(counts, "the corpus coverage profile is empty; the measurement behind %s would pass vacuously", unreachedHeading)
	return counts
}

// atoi reads one numeric field of a coverage profile. A malformed field
// fails the suite rather than degrading to zero: zero is the value that
// means "never executed", so a parse that swallowed its error would
// report every tagged branch unreached.
func (s *SentinelTaxonomySuite) atoi(field string) int {
	n, err := strconv.Atoi(field)
	s.Require().NoError(err, "unreadable field %q in the corpus coverage profile", field)
	return n
}

// corpusPackages asks the toolchain which packages link this one, minus
// this one. The set is derived so that a consumer joins it by existing;
// hand-kept, it would shrink silently every time one was added, and a
// smaller sweep finds more branches unreached.
//
// `-test` is what makes the answer complete, and its absence is not
// visible from the result. Without it `go list` reports only what a
// package's non-test build imports, and `internal/codegen/conformance`
// — the package that runs every fixture under test/data/codegen,
// including all twenty-nine negative ones — holds nothing but
// conformance_test.go. It would drop out of the sweep, taking the whole
// negative corpus with it and leaving a fence that measures the
// backends and the CLI while reporting that it measured the fixtures.
// With `-test` the listing gains the synthesised test binaries, whose
// dependency closure is the one the measurement needs.
func (s *SentinelTaxonomySuite) corpusPackages() []string {
	out := s.run("list", "-test", "-f", "{{.ImportPath}}|{{join .Deps \" \"}}", "./...")

	seen := make(map[string]bool)
	var pkgs []string
	for _, line := range strings.Split(out, "\n") {
		importPath, deps, ok := strings.Cut(line, "|")
		if !ok || !slices.Contains(strings.Fields(deps), codegenPkgPath) {
			continue
		}
		pkg := corpusPackageOf(importPath)
		if pkg == codegenPkgPath || seen[pkg] {
			continue
		}
		seen[pkg] = true
		pkgs = append(pkgs, pkg)
	}
	s.Require().NotEmpty(pkgs, "no package outside %s links it; the measurement behind %s would pass vacuously", codegenPkgPath, unreachedHeading)
	s.Require().Contains(pkgs, conformancePkgPath,
		"%s is not in the corpus sweep, so the negative fixtures under test/data/codegen do not run under coverage and every tagged branch would read as unreached; the listing lost the synthesised test binaries",
		conformancePkgPath)
	return pkgs
}

// corpusPackageOf reduces one `go list -test` import path to the package
// `go test` accepts. The listing names a package under test four ways —
// plainly, as the test binary `p.test`, and as the variants `p [p.test]`
// and `p_test [p.test]` — and all four are one package to run.
func corpusPackageOf(importPath string) string {
	if variant := strings.Index(importPath, " ["); variant >= 0 {
		importPath = importPath[:variant]
	}
	importPath = strings.TrimSuffix(importPath, ".test")
	return strings.TrimSuffix(importPath, "_test")
}

// run executes the go tool at the module root and returns its stdout.
// Failure is fatal rather than skipped: a fence that quietly stops
// measuring is the failure mode this document is about.
func (s *SentinelTaxonomySuite) run(args ...string) string {
	cmd := exec.CommandContext(s.T().Context(), "go", args...)
	cmd.Dir = moduleRoot
	out, err := cmd.Output()
	var stderr string
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		stderr = string(exit.Stderr)
	}
	s.Require().NoError(err, "go %s failed: %s", strings.Join(args, " "), stderr)
	return string(out)
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

// scanSources reads the package's non-test sources once, returning every
// package-level Err* value paired with its errors.New argument, and every
// //gqlc:unreachable tag keyed by fail-site name. Read from source rather
// than through hand-written tables because such a table is one more
// mirror of the same set, and would drift the way the document did.
//
// Every source file, not just errors.go: nothing stops a sentinel being
// declared beside the code that returns it, and a sweep that reads one
// file would call such a sentinel undeclared and pass. The tags are
// spread across the package by nature.
func (s *SentinelTaxonomySuite) scanSources() (map[string]string, map[string]failSite) {
	entries, err := os.ReadDir(".")
	s.Require().NoError(err)

	declared := make(map[string]string)
	tagged := make(map[string]failSite)
	files := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		files++
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, name, nil, parser.ParseComments)
		s.Require().NoError(err)
		s.collectSentinels(name, file, declared)
		s.collectUnreachableTags(name, fset, file, tagged)
	}
	s.Require().NotZero(files, "no non-test .go files found; the fence ran from the wrong directory and would pass vacuously")
	s.Require().NotEmpty(declared, "no Err* declarations found in package codegen — the fence would pass vacuously")
	return declared, tagged
}

// collectSentinels adds one file's package-level Err* declarations to out.
func (s *SentinelTaxonomySuite) collectSentinels(path string, file *ast.File, out map[string]string) {
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

// isSentinelIdent recognises a sentinel by name: "Err" followed by an
// upper-case rune, which is how all sixteen are written.
//
// Narrower than the bare "Err" prefix on purpose. That prefix also
// matches the "Error…" family — an ErrorContext, an ErrorFormat, an
// Errors slice — none of which are errors.New calls, so each would
// reach the hard Require below and fail the suite. The effect would be a
// documentation fence forbidding a name in production code it does not
// document. Requiring the upper-case rune costs nothing on this side:
// it still admits every sentinel, and a sentinel that ever were named
// otherwise would be caught anyway, one loop up, as an allSentinels
// member whose message no declaration constructs.
func isSentinelIdent(ident *ast.Ident) bool {
	rest, ok := strings.CutPrefix(ident.Name, "Err")
	if !ok || rest == "" {
		return false
	}
	return rest[0] >= 'A' && rest[0] <= 'Z'
}

// collectUnreachableTags adds one file's //gqlc:unreachable tags to out,
// resolving each to the return statement on the line below it and to the
// sentinel that return carries.
func (s *SentinelTaxonomySuite) collectUnreachableTags(path string, fset *token.FileSet, file *ast.File, out map[string]failSite) {
	returns := make(map[int]*ast.ReturnStmt)
	ast.Inspect(file, func(n ast.Node) bool {
		if ret, ok := n.(*ast.ReturnStmt); ok {
			returns[fset.Position(ret.Pos()).Line] = ret
		}
		return true
	})

	for _, group := range file.Comments {
		for _, comment := range group.List {
			m := unreachableTag.FindStringSubmatch(comment.Text)
			if m == nil {
				continue
			}
			site := m[1]
			line := fset.Position(comment.End()).Line + 1
			ret, ok := returns[line]
			s.Require().True(ok,
				"%s:%d: //gqlc:unreachable %s does not sit directly above a return statement; the fence reads the return on the next line to learn which sentinel the branch carries",
				path, fset.Position(comment.Pos()).Line, site)

			var sentinel string
			ast.Inspect(ret, func(n ast.Node) bool {
				if ident, ok := n.(*ast.Ident); ok && sentinel == "" && isSentinelIdent(ident) {
					sentinel = ident.Name
				}
				return sentinel == ""
			})
			s.Require().NotEmpty(sentinel,
				"%s:%d: //gqlc:unreachable %s tags a return that names no sentinel; %s %s records which sentinel each unreachable branch would fail with",
				path, line, site, taxonomyDoc, unreachedHeading)

			first, dup := out[site]
			s.Require().False(dup, "fail-site %s is tagged twice (%s:%d and %s:%d); site names are the fence's keys and must be unique", site, first.file, first.line, path, line)
			out[site] = failSite{file: path, line: line, sentinel: sentinel}
		}
	}
}

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
	rows := s.tableRows(doc, heading)
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, s.sentinelOfRow(row, heading))
	}
	return out
}

// sentinelOfRow reads a data row's first cell as a backticked sentinel.
func (s *SentinelTaxonomySuite) sentinelOfRow(row []string, heading string) string {
	m := sentinelCell.FindStringSubmatch(row[0])
	s.Require().NotNil(m, "%s: row under %q starts with %q, which is not a backticked sentinel name", taxonomyDoc, heading, row[0])
	return m[1]
}

// tableRows returns the trimmed cells of every data row of the table
// under heading, in document order, with the column header and the
// separator dropped.
//
// A row is recognised after trimming leading whitespace: markdown accepts
// up to three spaces of indent before a table row, so keying off the raw
// first byte let an indented row through unparsed — the same fail-open
// the tables themselves exist to close.
func (s *SentinelTaxonomySuite) tableRows(doc []string, heading string) [][]string {
	var out [][]string
	found := false
	for _, line := range doc {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "## ") {
			if found {
				break
			}
			found = trimmed == heading
			continue
		}
		if !found || !strings.HasPrefix(trimmed, "|") {
			continue
		}
		cells := strings.Split(strings.Trim(trimmed, "|"), "|")
		for i, cell := range cells {
			cells[i] = strings.TrimSpace(cell)
		}
		if cells[0] == "Sentinel" || strings.Trim(cells[0], "-:") == "" {
			continue
		}
		out = append(out, cells)
	}
	s.Require().True(found, "%s has no %q heading; the fence keys its tables off the heading text", taxonomyDoc, heading)
	s.Require().NotEmpty(out, "%s: the table under %q has no rows", taxonomyDoc, heading)
	return out
}
