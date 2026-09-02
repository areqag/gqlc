// Package corpusrun runs an assembled runtime-corpus module and holds
// its declarations against what the run and the fixture actually
// contain.
//
// A runtime corpus here is the same shape on every backend: emit a
// package from a schema, write the emission beside a hand-written
// driver kept as a .txt so nothing in this repo compiles it in place,
// and run `go test` over the assembled module. What varies per backend
// is which files are assembled and from what; what does not vary is how
// the child run is read and what its declarations have to answer for.
//
// Before this package the invariant half was written twice, by hand,
// once in internal/codegen/neo4j/corpus_test.go and once in
// internal/codegen/age/corpus_test.go. Measured on master 004e4c2a:
// three comment blocks were byte-identical across the two files (17
// lines, 1036 bytes), and the body of runCorpus was identical in both
// modulo the receiver and how it reached a context. Nothing compared
// them, so a correction to one was a correction to one (bd gqlc-vg66).
//
// Nothing here knows about neo4j or AGE, and it must not: a backend
// that has to be named is a backend the next one will not be.
package corpusrun

import (
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// Report is what one child run says about itself.
type Report struct {
	// Passed names the top-level tests that passed, with repeats, so a
	// name reported twice is not the same as a name reported once.
	Passed []string
	// Subtests counts subtest passes per top-level test. A top-level
	// test with no subtests is absent rather than zero, so a tree that
	// goes silent drops a key rather than changing a number.
	Subtests map[string]int
	// Log is the run's output as a reader sees it, for the failure
	// message. It is not a source of truth: see Run.
	Log string
}

// Run runs the assembled corpus module's own tests in dir, reporting the
// top-level tests that passed, how many subtest passes each top-level
// test carried, and the run's output as a reader sees it.
//
// Both sets are read from `go test -json`, whose Test field the testing
// framework fills in, rather than from "--- PASS" lines, which a subtest
// name or anything a test writes to stdout can also spell.
//
// A subtest is counted under its top-level test rather than under the
// parent that ran it, so a tree nested two deep reports as one entry.
//
// GOPROXY is off and GOFLAGS is cleared, so a corpus module that needs
// the network fails rather than reaching for it.
func Run(ctx context.Context, dir string) (Report, error) {
	cmd := exec.CommandContext(ctx, "go", "test", "-json", "-count=1", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS=", "GOPROXY=off")
	out, err := cmd.CombinedOutput()

	var text strings.Builder
	report := Report{Subtests: make(map[string]int)}
	for line := range strings.Lines(string(out)) {
		var event struct {
			Action string `json:"Action"`
			Test   string `json:"Test"`
			Output string `json:"Output"`
		}
		if !strings.HasPrefix(line, "{") || json.Unmarshal([]byte(line), &event) != nil {
			text.WriteString(line)
			continue
		}
		text.WriteString(event.Output)
		if event.Action != "pass" || event.Test == "" {
			continue
		}
		if parent, _, isSubtest := strings.Cut(event.Test, "/"); isSubtest {
			report.Subtests[parent]++
		} else {
			report.Passed = append(report.Passed, event.Test)
		}
	}
	report.Log = text.String()
	return report, err
}

// Table is what one top-level test's in-body range loops iterate over,
// counted from the fixture's syntax tree rather than from the run.
//
// Rows is the total number of elements across every range whose length
// is decidable from the source: a composite literal ranged over
// directly, a composite literal bound to a name the same function body
// binds exactly once and then ranges over, and an integer literal
// ranged over. Dynamic counts the ranges whose length is not decidable
// — a range over a returned value, a parameter, or a name bound more
// than once.
//
// Both halves are carried because either one alone can be gamed by an
// edit the other catches. Emptying a table drops Rows; replacing the
// same table with a call that returns a slice moves the loop from Rows
// to Dynamic, which a Rows-only census would read as the table
// shrinking to nothing and a Dynamic-only census would not see at all.
type Table struct {
	Rows    int
	Dynamic int
}

// Tables censuses a corpus fixture's top-level tests by their in-body
// range tables.
//
// This exists because a top-level name census and a subtest census
// between them still cannot see a table go empty. A top-level test
// passes and carries its name whether or not anything ran inside it, so
// a name census is satisfied; a fixture whose assertions are plain
// in-body `for ... range` loops emits no subtest event at all, so a
// subtest census is satisfied by the test having no key on either side,
// which is equality. Measured 2026-08-23 on the two fixtures in this
// repo: 64 top-level tests between them and exactly one t.Run each, so
// the subtest census held two keys in total. Emptying one table in each
// fixture was then run against origin/master, which has both of the
// other censuses and not this one, and both runs came back green (bd
// gqlc-eum1, the half gqlc-mlf4 left open). Two tables were measured,
// not sixty-two: what generalises is the argument, not the sample.
//
// It reads the syntax tree rather than the source bytes. A grep over
// raw bytes counts a commented-out table as evidence; comments are not
// AST nodes, so commenting a case out here drops the count, which is
// the point, and TestTablesDoesNotCountACommentedOutCase runs it.
//
// A test with no range at all is absent from the result rather than
// present with a zero Table, so the keys carry a distinction the counts
// cannot: a test whose last loop is deleted drops a key, and a test
// that grows its first loop adds one.
//
// What this does NOT see, stated so nobody re-derives it: an assertion
// that is not in a loop, a loop whose body stops asserting, and a case
// swapped for another case. It is a size, not a membership, exactly as
// the subtest census is.
func Tables(filename, src string) (map[string]Table, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filename, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", filename, err)
	}

	fileBound := map[string]ast.Expr{}
	for _, decl := range f.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, spec := range gen.Specs {
			bindValueSpec(fileBound, spec)
		}
	}

	out := map[string]Table{}
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv != nil || fn.Body == nil || !strings.HasPrefix(fn.Name.Name, "Test") {
			continue
		}
		bound := bindings(fn.Body, fileBound)
		var t Table
		seen := false
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			rng, ok := n.(*ast.RangeStmt)
			if !ok || rng.X == nil {
				return true
			}
			seen = true
			if n, ok := staticLen(rng.X, bound); ok {
				t.Rows += n
			} else {
				t.Dynamic++
			}
			return true
		})
		if seen {
			out[fn.Name.Name] = t
		}
	}
	return out, nil
}

// ambiguous marks a name the enclosing body binds more than once, so a
// range over it is not decidable from any one of those bindings.
var ambiguous = &ast.BadExpr{}

// bindings collects the names a function body binds exactly once to a
// composite literal, starting from the file-level ones. A name bound
// twice is recorded as ambiguous rather than dropped: dropping it would
// let an inner shadow of a file-level table be counted as the outer
// one.
func bindings(body *ast.BlockStmt, fileBound map[string]ast.Expr) map[string]ast.Expr {
	bound := make(map[string]ast.Expr, len(fileBound))
	for k, v := range fileBound {
		bound[k] = v
	}
	ast.Inspect(body, func(n ast.Node) bool {
		switch s := n.(type) {
		case *ast.AssignStmt:
			if len(s.Lhs) != len(s.Rhs) {
				return true
			}
			for i, lhs := range s.Lhs {
				if id, ok := lhs.(*ast.Ident); ok {
					bind(bound, id.Name, s.Rhs[i])
				}
			}
		case *ast.ValueSpec:
			bindValueSpec(bound, s)
		}
		return true
	})
	return bound
}

func bindValueSpec(bound map[string]ast.Expr, spec ast.Spec) {
	vs, ok := spec.(*ast.ValueSpec)
	if !ok || len(vs.Names) != len(vs.Values) {
		return
	}
	for i, name := range vs.Names {
		bind(bound, name.Name, vs.Values[i])
	}
}

func bind(bound map[string]ast.Expr, name string, value ast.Expr) {
	if name == "_" {
		return
	}
	if _, already := bound[name]; already {
		bound[name] = ambiguous
		return
	}
	bound[name] = value
}

// staticLen is how many times a range over expr runs, when the source
// decides that. The bool is whether it does.
func staticLen(expr ast.Expr, bound map[string]ast.Expr) (int, bool) {
	switch e := expr.(type) {
	case *ast.CompositeLit:
		return len(e.Elts), true
	case *ast.Ident:
		target, ok := bound[e.Name]
		if !ok || target == ambiguous {
			return 0, false
		}
		if _, isIdent := target.(*ast.Ident); isIdent {
			// One hop only. A chain of aliases is rare enough that
			// following it would be untested machinery, and reading it
			// as dynamic is the safe direction: it moves the loop into
			// Dynamic, which is declared, rather than out of both.
			return 0, false
		}
		return staticLen(target, bound)
	case *ast.BasicLit:
		if e.Kind != token.INT {
			return 0, false
		}
		n, err := strconv.Atoi(e.Value)
		if err != nil || n < 0 {
			return 0, false
		}
		return n, true
	case *ast.ParenExpr:
		return staticLen(e.X, bound)
	}
	return 0, false
}

// Declared is what a backend's corpus test writes down, in the parent,
// about the fixture and the run it is checking.
//
// All three are written in the parent rather than censused out of the
// fixture, because the fixture is the artefact under check: a name the
// fixture stops declaring is a name a census of it stops naming, so
// both sides of such a comparison lose it at once. Held against a
// literal in a file the child module is not given, they do not move
// together.
//
// They are declarations rather than gates. A test can leave the fixture
// and its entries leave these literals in one commit's edit, and the
// comparison stays green. What the edit costs the remover is writing
// the removal down.
type Declared struct {
	// Tests names the top-level tests the child run has to report as
	// passing, with repeats significant.
	Tests []string
	// Subtests counts the subtest passes each top-level test has to
	// report. Absent means none.
	Subtests map[string]int
	// Tables counts the in-body range tables each top-level test
	// declares, as Tables censuses them. Absent means the test has no
	// range at all.
	Tables map[string]Table
}

// Check holds a Declared against one run and the fixture source that
// run was assembled from, returning the first disagreement or nil.
//
// Tests is required non-empty before anything is compared. A fixture
// emptied to its package clause is a child run that reports success —
// `go test` prints `[no tests to run]` and exits 0 over a _test.go
// declaring no tests, so the child's exit status says nothing about it
// — and an empty Tests would match that run's empty pass set, two empty
// sets being equal.
func (d Declared) Check(r Report, fixtureName, fixtureSrc string) error {
	if len(d.Tests) == 0 {
		return fmt.Errorf("Declared.Tests names no test, so the comparison below is satisfied by a child run that ran none")
	}
	// Compared as a multiset, so a name reported twice differs from the
	// same name reported once.
	if err := diffMultiset("Declared.Tests", d.Tests, r.Passed); err != nil {
		return err
	}
	// Compared whole rather than per key, because the two disagreements
	// this has to catch are a key whose count fell and a key only one
	// side holds.
	if err := diffCounts("Declared.Subtests", d.Subtests, r.Subtests); err != nil {
		return err
	}
	got, err := Tables(fixtureName, fixtureSrc)
	if err != nil {
		return err
	}
	return diffTables("Declared.Tables", d.Tables, got)
}

func diffMultiset(what string, want, got []string) error {
	w, g := slices.Clone(want), slices.Clone(got)
	sort.Strings(w)
	sort.Strings(g)
	if slices.Equal(w, g) {
		return nil
	}
	return fmt.Errorf("the child run's passing tests are not what %s names: the comparison is by multiset — same names, same number of repeats, order not compared\n"+
		"both lists below are sorted for reading, so neither is in declaration order\n"+
		"declared: %v\nran:      %v", what, w, g)
}

func diffCounts(what string, want, got map[string]int) error {
	var bad []string
	for _, k := range union(keys(want), keys(got)) {
		w, wOK := want[k]
		g, gOK := got[k]
		if wOK == gOK && w == g {
			continue
		}
		bad = append(bad, fmt.Sprintf("  %s: declared %s, ran %s", k, count(w, wOK), count(g, gOK)))
	}
	if bad == nil {
		return nil
	}
	return fmt.Errorf("the child run's subtest passes are not what %s declares, top-level test by top-level test:\n%s",
		what, strings.Join(bad, "\n"))
}

func diffTables(what string, want, got map[string]Table) error {
	var bad []string
	for _, k := range union(keys(want), keys(got)) {
		w, wOK := want[k]
		g, gOK := got[k]
		if wOK == gOK && w == g {
			continue
		}
		bad = append(bad, fmt.Sprintf("  %s: declared %s, fixture has %s", k, table(w, wOK), table(g, gOK)))
	}
	if bad == nil {
		return nil
	}
	// The fixture's own census is printed as a Go literal because
	// pasting it is the intended repair for a deliberate fixture edit,
	// and transcribing it by hand is where a wrong number would come
	// from.
	return fmt.Errorf("the fixture's in-body range tables are not what %s declares, top-level test by top-level test:\n%s\n\n"+
		"the fixture currently censuses as:\n%s", what, strings.Join(bad, "\n"), Literal(got))
}

// Literal renders a table census as the Go composite literal that
// declares it.
func Literal(tables map[string]Table) string {
	var b strings.Builder
	b.WriteString("map[string]corpusrun.Table{\n")
	for _, k := range keys(tables) {
		t := tables[k]
		fmt.Fprintf(&b, "\t%q: {Rows: %d", k, t.Rows)
		if t.Dynamic != 0 {
			fmt.Fprintf(&b, ", Dynamic: %d", t.Dynamic)
		}
		b.WriteString("},\n")
	}
	b.WriteString("}")
	return b.String()
}

func count(n int, present bool) string {
	if !present {
		return "no entry"
	}
	return strconv.Itoa(n)
}

func table(t Table, present bool) string {
	if !present {
		return "no entry"
	}
	return fmt.Sprintf("{Rows: %d, Dynamic: %d}", t.Rows, t.Dynamic)
}

func keys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func union(a, b []string) []string {
	out := append(slices.Clone(a), b...)
	sort.Strings(out)
	return slices.Compact(out)
}
