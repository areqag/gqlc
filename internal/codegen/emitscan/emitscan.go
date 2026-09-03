// Package emitscan reads an emitted Go package as a syntax tree and
// answers the one question a backend's own tests cannot answer by
// reading their goldens: can a name the QUERY AUTHOR chose displace a
// name the GENERATOR owns?
//
// The class is capture. An emitted query method binds its argument in
// the same scope its body resolves the generator's own package-level
// declarations in — the query-text const above all. If any part of the
// emitter ever derives a binding from a parameter name, or if a body
// local ever takes the argument's name, the caller's value silently
// stands in for the generator's. Neither half is reliably a compile
// error, because the widths collide: a STRING parameter over a
// string-typed const assigns rather than fails, and a caller passing
// "MATCH (n) DETACH DELETE n" then has that text run as the statement,
// with no concatenation anywhere to find.
//
// The machinery here is the analyser half only. It takes emitted files
// as text and returns findings; it decides nothing about which fixtures
// to sweep, which arities to bind, or how to report. Those are the
// backend's, because the fixtures and the signature arms are the
// backend's. What is shared is what a Go package's scope is, and that
// does not vary by backend — which is why an equivalent of this living
// inside one backend's test package (as it did, in
// internal/codegen/age) leaves every other backend unswept whatever it
// proves about that one.
package emitscan

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strings"
)

// Finding is one way a swept emission fails to be uncapturable.
//
// A slice of these rather than an error because a sweep answers about a
// whole package and more than one file can be wrong at once; collapsing
// them onto the first would make the second invisible until the first
// was fixed.
type Finding struct {
	// Path is the emitted file the finding was read off, or "" when the
	// finding is about the package rather than a file.
	Path string
	// Detail says what was found, in the reader's terms.
	Detail string
}

func (f Finding) String() string {
	if f.Path == "" {
		return f.Detail
	}
	return f.Path + ": " + f.Detail
}

// Findings renders a sweep's result as one line per finding, for a
// failure message.
func Findings(found []Finding) string {
	out := make([]string, 0, len(found))
	for _, f := range found {
		out = append(out, f.String())
	}
	return strings.Join(out, "\n")
}

// Sweep holds one emission against one candidate parameter name.
//
// Baseline is the same batch emitted under a parameter name no emitter
// produces, and it is what makes the reachability half of the sweep
// DIFFERENTIAL rather than absolute. Capture's signature is a
// declaration some method used to resolve and no longer does. The
// absolute form of that claim — "every package-level declaration is
// resolved by somebody" — is false of a generated package for an
// innocent reason: the consumer-facing surface (a Querier interface, a
// constructor) is resolved by the CALLER and by nothing the emitter
// writes. An allowlist of those names would need maintaining by hand
// and would admit the next one silently. Against a baseline no list is
// needed, and the claim is strictly stronger: a name the baseline does
// not resolve cannot be captured out of a body that never read it.
type Sweep struct {
	// Baseline is the emitted package under a probe parameter name.
	Baseline map[string]string
	// Probe is the same batch emitted under the candidate name.
	Probe map[string]string
	// Name is the candidate parameter name Probe was emitted under.
	Name string
	// ArgName is the identifier every emitted query method must bind its
	// argument as, whatever the query text said. codegen.ParamArg.
	ArgName string
	// QuerySuffix selects the files carrying query METHODS, whose shape
	// — a receiver, ctx and exactly one generated argument — the
	// per-file half reads. A backend's db.go does not have that shape.
	QuerySuffix string
}

// Run performs the sweep and returns what it found. An empty result is
// the pass.
//
// Two halves, and they are asked of different scopes on purpose. The
// signature and body-local checks are per file, because a method
// captures in the file it is written in. Reachability is not per file:
// Go's package scope is package-wide, so "does some method still
// resolve this declaration" has to be asked of every file's
// declarations against every file's bodies at once. Asked per file it
// reports two different falsehoods — a constructor resolved only from
// another file reads as captured, while a string-typed const declared
// in one file and captured out of another's body reads as fine.
//
// Two halves is not two independent measurements, and a reader
// deciding what a green sweep licenses needs that distinction. The
// reachability half compares two emissions of ONE batch differing only
// in a parameter's name, so emitter behaviour that is not a function of
// that name is present on both sides and moves neither differential.
// What is left is derivation: a package-level name taken from the
// parameter, or a body local taken from it that shadows one. That is
// the class ArgName closes upstream and the per-file half guards
// directly, so these are two layers over one invariant.
//
// The per-file half is the wider of the two, which is the part that is
// easy to get backwards. Its shadow arm fires just as well when the
// EMITTER names a local of its own ArgName, with no query author
// involved.
//
// Measured on the AGE emitter, one fixture, each row naming the arm it
// fired (gqlc-9o4t). Deriving the query-text const's name from the
// author's parameter fires both differentials; naming a body local
// after the parameter fires the resolved one alone; naming a
// generator-owned local ArgName fires the per-file shadow arm and
// neither differential. Two emitter changes fired nothing: renaming
// that const by a fixed suffix, and dropping the const entirely to
// inline its text — the declaration this sweep exists to protect,
// deleted, is absent from both sides of every differential. A backend's
// goldens catch that; this does not.
//
// The two emptiness pins in that half are not a third axis. They are
// over the whole package, so any emitted file still declaring a type or
// resolving one keeps them silent, and no mutation confined to query
// rendering reached either.
func (s Sweep) Run() []Finding {
	var found []Finding

	if len(s.Probe) == 0 {
		return append(found, Finding{Detail: "the emission has no files to sweep"})
	}

	queryFiles := 0
	for _, path := range sortedKeys(s.Probe) {
		if !strings.HasSuffix(path, s.QuerySuffix) {
			continue
		}
		queryFiles++
		found = append(found, s.fileFindings(path, s.Probe[path])...)
	}
	// The per-file half is conditional on a suffix, and a conditional
	// guard is silent when its condition never holds. A batch with no
	// query file has no method for a parameter to be bound in, so
	// reaching here with none means the caller swept the wrong thing.
	if queryFiles == 0 {
		found = append(found, Finding{
			Detail: fmt.Sprintf("no %s file was swept, so the per-file half ran on nothing", s.QuerySuffix),
		})
	}

	wantDeclared, wantResolved, err := Scope(s.Baseline)
	if err != nil {
		return append(found, Finding{Detail: "baseline: " + err.Error()})
	}
	gotDeclared, gotResolved, err := Scope(s.Probe)
	if err != nil {
		return append(found, Finding{Detail: "probe: " + err.Error()})
	}

	// A sweep over a collapsed parse set agrees with every emission
	// there could have been, so both halves of the differential are
	// pinned non-empty before they are compared.
	if len(wantDeclared) == 0 {
		found = append(found, Finding{Detail: "the baseline package declares nothing at package level"})
	}
	if len(wantResolved) == 0 {
		found = append(found, Finding{Detail: "no method in the baseline package resolves a package-level name"})
	}

	if !equalStrings(wantDeclared, gotDeclared) {
		found = append(found, Finding{Detail: fmt.Sprintf(
			"renaming a query parameter to %q changed what the emitted package declares: %v became %v",
			s.Name, wantDeclared, gotDeclared)})
	}
	if !equalStrings(wantResolved, gotResolved) {
		found = append(found, Finding{Detail: fmt.Sprintf(
			"renaming a query parameter to %q changed which package-level names the emitted package's "+
				"methods resolve, so the caller's argument captured one: %v became %v",
			s.Name, wantResolved, gotResolved)})
	}
	return found
}

// fileFindings is the per-file half: the signature names the argument
// itself whatever the query said, and no body local shadows it.
//
// The candidate name is not itself excluded from the body's locals, and
// must not be. Every body local is generator-owned and positionally
// named, so the sweep feeds names like err and stmt straight back in. A
// body local called err is not a capture — the parameter is not an
// identifier the body resolves at all, which is the whole point. What
// would be a capture is that local displacing the caller's argument,
// and the argument's name is generator-owned whatever the query said,
// so that is the name the shadow check is anchored on.
func (s Sweep) fileFindings(path, body string) []Finding {
	var found []Finding
	file, err := Parse(path, body)
	if err != nil {
		return append(found, Finding{Path: path, Detail: err.Error()})
	}
	for _, arg := range methodArgs(file) {
		if arg.name != s.ArgName {
			found = append(found, Finding{Path: path, Detail: fmt.Sprintf(
				"method %s took its argument name from the query text: bound %q under parameter %q, want %q",
				arg.method, arg.name, s.Name, s.ArgName)})
		}
	}
	for _, local := range BodyLocals(file) {
		if local == s.ArgName {
			found = append(found, Finding{Path: path, Detail: fmt.Sprintf(
				"a body local named %q shadows the caller's argument", s.ArgName)})
		}
	}
	return found
}

// methodArg is one emitted method's last parameter.
type methodArg struct {
	method string
	name   string
}

// methodArgs names the argument each emitted method takes after ctx. A
// swept batch binds parameters on every query, so every method carries
// exactly the receiver, ctx and that argument; a method of any other
// shape is reported rather than skipped, because skipping it is how a
// signature arm leaves the sweep silently.
func methodArgs(file *ast.File) []methodArg {
	var out []methodArg
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		params := fn.Type.Params.List
		if len(params) != 2 || len(params[1].Names) != 1 {
			out = append(out, methodArg{method: fn.Name.Name, name: ""})
			continue
		}
		out = append(out, methodArg{method: fn.Name.Name, name: params[1].Names[0].Name})
	}
	return out
}

// Candidates is every identifier the given emitted files mention,
// declared or referenced, deduplicated and ordered.
//
// Deliberately the widest set the syntax tree offers rather than a list
// of names anyone thought of, so a name the emitter starts using later
// is swept without anybody remembering to add it.
//
// The blank identifier is not a candidate: a query cannot usefully be
// written against it, and generation fails on $_ for an unrelated
// reason.
func Candidates(files map[string]string) ([]string, error) {
	seen := make(map[string]bool)
	for _, path := range sortedKeys(files) {
		file, err := Parse(path, files[path])
		if err != nil {
			return nil, err
		}
		ast.Inspect(file, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok && id.Name != "_" {
				seen[id.Name] = true
			}
			return true
		})
	}
	return sortedSet(seen), nil
}

// Scope reads an emitted package's two name sets: what it declares at
// package level, and which of those declarations some function in the
// package resolves. Both sorted, both package-wide, both read off the
// syntax tree.
//
// The second is intersected with the first on purpose. A body resolves
// plenty of names the package does not declare — imported ones,
// universe ones — and leaving those in would make the differential move
// whenever an unrelated import did.
func Scope(files map[string]string) (declared, resolved []string, err error) {
	declaredSet := make(map[string]bool)
	free := make(map[string]bool)
	for _, path := range sortedKeys(files) {
		file, err := Parse(path, files[path])
		if err != nil {
			return nil, nil, err
		}
		for _, decl := range PackageDecls(file) {
			declaredSet[decl] = true
		}
		for _, d := range file.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok {
				continue
			}
			for ident := range FreeIdents(fn) {
				free[ident] = true
			}
		}
	}
	for _, decl := range sortedSet(declaredSet) {
		declared = append(declared, decl)
		if free[decl] {
			resolved = append(resolved, decl)
		}
	}
	return declared, resolved, nil
}

// PackageDecls names what an emitted file declares at package level:
// consts, vars and types alike. Read off the file so a declaration the
// emitter adds later is held by the same assertion.
//
// Consts and vars are here, and that is the whole point of reading
// declarations for this question rather than reusing a "surface"
// helper: the capture class is precisely about consts and vars, and a
// helper that keeps only TYPE declarations cannot see it.
//
// Measured, not argued. Making the neo4j emitter name its query-text
// const after the query's first parameter — the emitter deriving a
// package-level identifier from a name the query AUTHOR chose, which is
// the whole class — reddens 143 of the neo4j sweep's subtests. Narrow
// this function's ValueSpec arm away, leaving the TypeSpec arm alone
// (which is what conformance's declaredSurface keeps), and that same
// emitter mutation passes.
func PackageDecls(file *ast.File) []string {
	var out []string
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok == token.IMPORT {
			continue
		}
		for _, spec := range gen.Specs {
			switch sp := spec.(type) {
			case *ast.ValueSpec:
				for _, n := range sp.Names {
					if n.Name != "_" {
						out = append(out, n.Name)
					}
				}
			case *ast.TypeSpec:
				out = append(out, sp.Name.Name)
			}
		}
	}
	return out
}

// FreeIdents names the identifiers a function resolves outside itself —
// every identifier it mentions, less every name it binds. Flat rather
// than block-scoped, which errs towards calling a name bound: an
// emission that captures one therefore fails rather than slips through.
func FreeIdents(fn *ast.FuncDecl) map[string]bool {
	bound := make(map[string]bool)
	for _, l := range []*ast.FieldList{fn.Recv, fn.Type.Params, fn.Type.Results} {
		if l == nil {
			continue
		}
		for _, f := range l.List {
			for _, n := range f.Names {
				bound[n.Name] = true
			}
		}
	}
	ast.Inspect(fn, func(n ast.Node) bool {
		for _, id := range DeclaredIdents(n) {
			bound[id.Name] = true
		}
		return true
	})

	free := make(map[string]bool)
	for _, name := range ReferencedIdents(fn) {
		if !bound[name] {
			free[name] = true
		}
	}
	return free
}

// ReferencedIdents names every identifier a node mentions in a position
// where scope resolution applies. Selector suffixes and struct-literal
// keys are excluded: those resolve against a type, not against the
// scope the parameter is bound in, so no argument name can capture
// them.
//
// The two exclusions recurse rather than sweeping their operand flat,
// because either can hold the other: arg.MinAge inside a map literal is
// a selector under a key-value, and reading that operand flat would
// call the field name MinAge a scope reference. Doing so is not merely
// noise — a package-level declaration is held to being resolvable from
// some function, so a name that only ever appears as a field suffix
// would satisfy that check while nothing resolved it.
func ReferencedIdents(n ast.Node) []string {
	var out []string
	ast.Inspect(n, func(n ast.Node) bool {
		switch e := n.(type) {
		case *ast.SelectorExpr:
			out = append(out, ReferencedIdents(e.X)...)
			return false
		case *ast.KeyValueExpr:
			out = append(out, ReferencedIdents(e.Value)...)
			return false
		case *ast.Ident:
			out = append(out, e.Name)
		}
		return true
	})
	return out
}

// DeclaredIdents returns the identifiers a node binds. Short variable
// declarations, var/const declarations and range clauses are the whole
// of what an emitted body uses to introduce a name. Binding is only
// half of what a parameter can capture, though — see ReferencedIdents
// for the other half.
func DeclaredIdents(n ast.Node) []*ast.Ident {
	var out []*ast.Ident
	switch stmt := n.(type) {
	case *ast.AssignStmt:
		if stmt.Tok != token.DEFINE {
			return nil
		}
		for _, lhs := range stmt.Lhs {
			if id, ok := lhs.(*ast.Ident); ok {
				out = append(out, id)
			}
		}
	case *ast.ValueSpec:
		out = append(out, stmt.Names...)
	case *ast.RangeStmt:
		if stmt.Tok != token.DEFINE {
			return nil
		}
		for _, e := range []ast.Expr{stmt.Key, stmt.Value} {
			if id, ok := e.(*ast.Ident); ok {
				out = append(out, id)
			}
		}
	}
	return out
}

// BodyLocals names every identifier the function bodies in an emitted
// file declare, deduplicated and ordered. The blank identifier is not
// one: it binds nothing and cannot collide.
func BodyLocals(file *ast.File) []string {
	seen := make(map[string]bool)
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			for _, id := range DeclaredIdents(n) {
				if id.Name != "_" {
					seen[id.Name] = true
				}
			}
			return true
		})
	}
	return sortedSet(seen)
}

// Parse parses an emitted file. An emission that does not parse is a
// finding rather than a panic, because the sweep runs over emissions
// generated under names the emitter has never seen.
func Parse(path, body string) (*ast.File, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, body, parser.SkipObjectResolution)
	if err != nil {
		return nil, fmt.Errorf("the emitted file does not parse: %w", err)
	}
	return f, nil
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedSet(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
