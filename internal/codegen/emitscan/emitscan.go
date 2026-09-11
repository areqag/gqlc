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
// than block-scoped: a name bound anywhere in the function counts as
// bound throughout it, so a read outside the block that binds it is not
// free.
//
// The bound set is the function's own receiver, parameters, results and
// type parameters — including a generic receiver's, which are written
// into the receiver's type rather than into a field list — plus what
// DeclaredIdents recognises: short variable declarations, var/const
// declarations, range clauses, type-switch and select bindings (those
// two through the assignment each holds), local type declarations, and
// function literals. Over those constructs the flat reading errs
// towards calling a name bound, which is the direction that makes an
// emission capturing one fail rather than slip through.
//
// Three more constructs write an identifier inside a function without
// binding it anywhere the body can read: a local type's field names, a
// statement label, and a parameter name in a func TYPE. Those are not
// in the bound set either; ReferencedIdents drops them per occurrence,
// which is exact where calling them bound would swallow a genuine read
// of the same name elsewhere in the function. Its doc comment carries
// that reasoning.
//
// Read the two lists above as the enumeration they are, not as a
// property of the analysis: a construct absent from both is unmeasured,
// and TestFreeIdentsBoundSetLimits holds one row per construct named
// here. The enumeration was measured under gqlc-9hrh, which found five
// constructs erring the OTHER way — one of them live, since the age
// emitter emits func agtypeList[T any](...) — and closed under
// gqlc-db0e, which is where the per-occurrence verdicts were decided.
//
// What bounded the damage in the meantime, and still bounds any
// construct nobody has thought of, is the one caller that acts on the
// free set: Scope intersects it with the package's own declarations, so
// a name that is not a package-level declaration is dropped before a
// sweep sees it.
func FreeIdents(fn *ast.FuncDecl) map[string]bool {
	bound := make(map[string]bool)
	for _, l := range []*ast.FieldList{fn.Recv, fn.Type.TypeParams, fn.Type.Params, fn.Type.Results} {
		if l == nil {
			continue
		}
		for _, f := range l.List {
			for _, n := range f.Names {
				bound[n.Name] = true
			}
		}
	}
	for _, id := range receiverTypeParams(fn.Recv) {
		bound[id.Name] = true
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

// receiverTypeParams names the type parameters a generic receiver binds.
// They are written into the receiver's TYPE rather than into a field
// list — `func (q *Q[T]) f()` binds T at the index of Q — so the sweep
// over fn.Recv's names above cannot see them, and without this they read
// as free while the body genuinely resolves them against the receiver.
//
// The index alone is taken, never the operand: Q itself is resolved
// outside the function and must stay free, or the resolved set loses
// every type an emitted method hangs off.
func receiverTypeParams(recv *ast.FieldList) []*ast.Ident {
	if recv == nil {
		return nil
	}
	var out []*ast.Ident
	for _, f := range recv.List {
		typ := f.Type
		if star, ok := typ.(*ast.StarExpr); ok {
			typ = star.X
		}
		switch t := typ.(type) {
		case *ast.IndexExpr:
			out = appendIdent(out, t.Index)
		case *ast.IndexListExpr:
			for _, index := range t.Indices {
				out = appendIdent(out, index)
			}
		}
	}
	return out
}

// appendIdent appends expr only when it is a bare identifier. A receiver
// type parameter always is; anything else at that position is not a
// binding and must not be recorded as one.
func appendIdent(out []*ast.Ident, expr ast.Expr) []*ast.Ident {
	if id, ok := expr.(*ast.Ident); ok {
		return append(out, id)
	}
	return out
}

// ReferencedIdents names every identifier a node mentions in a position
// where scope resolution applies. Five kinds of occurrence are excluded,
// because each resolves somewhere other than the scope a parameter is
// bound in, so no argument name can capture one: a selector suffix, a
// struct-literal key, a field or method name at its declaration site, a
// parameter name written into a func TYPE, and a statement label.
//
// The exclusions recurse rather than sweeping their operand flat,
// because they can hold one another: arg.MinAge inside a map literal is
// a selector under a key-value, and reading that operand flat would
// call the field name MinAge a scope reference. Doing so is not merely
// noise — a package-level declaration is held to being resolvable from
// some function, so a name that only ever appears as a field suffix
// would satisfy that check while nothing resolved it.
//
// The last three arrived with gqlc-db0e, and each is an exclusion rather
// than an addition to FreeIdents' bound set on purpose. The bound set is
// keyed by NAME and so applies to the whole function, while an exclusion
// is per OCCURRENCE: `type row struct{ col int }` beside a genuine read
// of a package-level `col` must leave that read free, and calling `col`
// bound would swallow it. A label is the clearest case — Go resolves
// labels in a namespace of their own, so `Loop:` cannot shadow a
// package-level `Loop` and `break Loop` cannot resolve one. What would
// have to change for that verdict to change: a caller wanting the label
// namespace, which would need a set of its own rather than this one.
//
// A function's or literal's OWN binding names stay in, which is why
// signatureIdents reads them explicitly instead of letting the FuncType
// arm drop them. That is deliberate over-inclusion — a binding is not a
// reference — and it is load-bearing: internal/codegen/age/capture_test.go's
// methodScopes reads this set to assert that renaming a query's
// parameters moves nothing an emitted method resolves, and a signature
// that named its argument after the query is exactly the defect it is
// written to catch. Dropping signature names would blind it wherever
// such a parameter went unread in the body.
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
		case *ast.FuncDecl:
			out = append(out, signatureIdents(e.Recv, e.Type)...)
			if e.Body != nil {
				out = append(out, ReferencedIdents(e.Body)...)
			}
			return false
		case *ast.FuncLit:
			out = append(out, signatureIdents(nil, e.Type)...)
			out = append(out, ReferencedIdents(e.Body)...)
			return false
		case *ast.FuncType:
			// Reached only as a TYPE, since the two arms above consume
			// the signature of a declaration and of a literal first. A
			// name here binds nothing any body can read.
			out = append(out, fieldTypeIdents(e.TypeParams, e.Params, e.Results)...)
			return false
		case *ast.StructType:
			out = append(out, fieldTypeIdents(e.Fields)...)
			return false
		case *ast.InterfaceType:
			out = append(out, fieldTypeIdents(e.Methods)...)
			return false
		case *ast.LabeledStmt:
			out = append(out, ReferencedIdents(e.Stmt)...)
			return false
		case *ast.BranchStmt:
			// break/continue/goto name a label and nothing else.
			return false
		case *ast.Ident:
			out = append(out, e.Name)
		}
		return true
	})
	return out
}

// signatureIdents reads the binding lists of a function declaration or
// literal: the names they introduce AND the types written beside them.
// Both halves are references for this analysis' purposes — see
// ReferencedIdents' doc comment for why the names are kept.
func signatureIdents(recv *ast.FieldList, typ *ast.FuncType) []string {
	var out []string
	for _, l := range []*ast.FieldList{recv, typ.TypeParams, typ.Params, typ.Results} {
		if l == nil {
			continue
		}
		for _, f := range l.List {
			for _, name := range f.Names {
				out = append(out, name.Name)
			}
			out = append(out, ReferencedIdents(f.Type)...)
		}
	}
	return out
}

// fieldTypeIdents reads the types in field lists whose names are never
// scope bindings: a struct's fields, an interface's methods, and the
// parameters, results and type parameters of a func type.
func fieldTypeIdents(lists ...*ast.FieldList) []string {
	var out []string
	for _, l := range lists {
		if l == nil {
			continue
		}
		for _, f := range l.List {
			out = append(out, ReferencedIdents(f.Type)...)
		}
	}
	return out
}

// DeclaredIdents returns the identifiers a node binds. Short variable
// declarations, var/const and local type declarations, range clauses
// and function literals are what an emitted body uses to introduce a
// name; for what it does NOT cover, see FreeIdents' doc comment, which
// enumerates the bound set this feeds. Binding is only half of what a
// parameter can capture, though — see ReferencedIdents for the other
// half.
//
// The function-literal arm reads the literal's parameters AND its
// results, because a named result binds just as a parameter does. It
// exists for an emission that has not landed yet: a closure parameter
// was a name no arm recorded, so FreeIdents called it free and
// BodyLocals did not call it a local — the one construct where this
// analysis erred towards calling a name unbound, which is the direction
// a capture slips through the sweep rather than failing it.
func DeclaredIdents(n ast.Node) []*ast.Ident {
	var out []*ast.Ident
	switch stmt := n.(type) {
	case *ast.TypeSpec:
		// A type declared inside a body binds its name in the function's
		// scope like any local, so a read of it is not free. Its FIELD
		// names are not bound here: they are not in the function's scope
		// at all, and ReferencedIdents excludes them per occurrence.
		out = append(out, stmt.Name)
	case *ast.FuncLit:
		for _, l := range []*ast.FieldList{stmt.Type.Params, stmt.Type.Results} {
			if l == nil {
				continue
			}
			for _, f := range l.List {
				out = append(out, f.Names...)
			}
		}
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
