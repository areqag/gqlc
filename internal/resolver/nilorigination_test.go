// This file answers bd gqlc-oltq: is a nil Column.Type constructible from user
// input? The answer is no, and these rows are what establish it rather than
// assert it.
//
// The question is owed because two functions in this package accept a nil
// ResolvedType — describeColumnType renders it as "<nil>" and resolvedTypeEqual
// guards its own recursion on it (bd gqlc-t802, PR #2087) — and nil handling
// that nothing can reach is worse than absent: the next reader takes it as
// evidence that nil is expected here and writes more nil handling around it.
// PR #2087 settled the POSTURE (a render called while building a refusal must
// total) and said in prose that no query originates the value. These rows turn
// that prose into a measurement, so the day an arm does originate one, the
// sweep fails instead of the claim quietly going stale.
//
// The argument has three parts, one per row below, and it is inductive over
// Parts rather than a single sweep:
//
//  1. Column.Type is written at exactly one site, from projectionType's return.
//     Every arm of the (ResolvedType, error) surface returns a value it
//     CONSTRUCTS, or nil with a non-nil error, or panics. None returns nil with
//     a nil error.
//  2. The surface is closed under forwarding: every `return someHelper(...)` in
//     it targets a function that is itself in it, so part 1 covers the callees
//     too and there is no unaudited edge out.
//  3. The only arms returning a value they did not construct are enumerated,
//     with the reason each is non-nil. One of them — the carried-alias bypass —
//     returns a previous Part's Column.Type, which is the inductive step: the
//     carry maps are written only from s.columns, so a Part can propagate a nil
//     but never originate one, and Part 0 carries nothing.
//
// What these rows do NOT establish, stated because a sweep's silence is not
// evidence: they are syntactic. A helper returning a nil-valued variable
// through one of the enumerated arms would satisfy every row here, which is
// exactly why part 3 is an enumeration that fails on a new entry rather than a
// pattern that accepts one.
package resolver_test

import (
	"go/ast"
	"go/types"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// auditedSurface is every function in the package returning
// (ResolvedType, error). It is enumerated rather than only discovered so that a
// new one landing fails a row and gets an argument written for it; discovery
// alone would silently widen the claim to cover a function nobody examined.
var auditedSurface = []string{
	"PropertyUseWitness",
	"callProjectionType",
	"projectionType",
	"refProjectionType",
	"resolveType",
	"unionNodeProperty",
	"unionProperty",
}

// nonConstructingReturns enumerates the arms of the audited surface that return
// a value they did not build in the return statement itself, keyed by the text
// of the return. Each needs its own argument for why the value is not nil, and
// a new entry must fail this file rather than join it silently.
//
// The keys are rendered source text, not line numbers, so the rows survive an
// edit elsewhere in the file while still failing when an arm is added, removed
// or reshaped.
var nonConstructingReturns = map[string]string{
	// `var first ResolvedProperty` — a struct value, not an interface variable.
	// It is non-nil in the interface sense however the loop above it runs,
	// including over an empty candidate set, where it stays the zero value.
	"unionProperty: return first, nil": "first is a ResolvedProperty value; a struct in an interface is never a nil interface",

	"unionNodeProperty: return first, nil": "first is a ResolvedProperty value; a struct in an interface is never a nil interface",

	// element is assigned a ResolvedEdge or a ResolvedEdgeUnion composite
	// literal on both sides of the branch above it, with no third path.
	"refProjectionType: return element, nil": "element is assigned a composite literal on every path reaching this return",

	// The inductive step, and the only arm that returns a value originating
	// outside this function. Pinned by the carry-writer row below.
	"refProjectionType: return rt, nil": "rt is a previous Part's Column.Type, propagated through the carry maps; see the carry-writer row",
}

// carryWriters is every assignment into the two maps that move a resolved
// column type from one Part to the next. It is what makes the `return rt, nil`
// entry above an induction rather than an assumption: both maps are fed from
// s.columns, whose element types come from projectionType, which the returns
// row audits. A writer sourced from anywhere else would break the chain, so a
// new one must fail here.
var carryWriters = map[string]string{
	"scope.go: s.carriedResolvedTypes[name] = rt":                        "the receiving half: rt ranges over the previous Part's exportedResolvedTypes",
	"scope.go: out.exportedResolvedTypes[item.Name] = s.columns[i].Type": "the sending half: the value is a Column.Type verbatim",
}

// auditedFuncs returns the FuncDecls in the package's non-test sources whose
// results are exactly (ResolvedType, error), keyed by name.
//
// Test sources are excluded deliberately: the claim is about what a QUERY can
// produce, and a helper written for a test is not on that path. parsePackageSources
// includes in-package test files because the row it was written for needs them.
func auditedFuncs(t *testing.T) map[string]*ast.FuncDecl {
	t.Helper()
	fset, files := parsePackageSources(t)

	out := map[string]*ast.FuncDecl{}
	nonTestFiles := 0
	for _, f := range files {
		if strings.HasSuffix(fset.Position(f.Pos()).Filename, "_test.go") {
			continue
		}
		nonTestFiles++
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil || fn.Type.Results == nil {
				continue
			}
			var results []string
			named := false
			for _, fld := range fn.Type.Results.List {
				n := 1
				if len(fld.Names) > 0 {
					n, named = len(fld.Names), true
				}
				for i := 0; i < n; i++ {
					results = append(results, types.ExprString(fld.Type))
				}
			}
			if len(results) != 2 || results[0] != "ResolvedType" || results[1] != "error" {
				continue
			}
			require.Falsef(t, named,
				"%s: %s declares named results. A named-result function can `return` nakedly, which hands back the zero value of ResolvedType — a nil interface — with a nil error, and the returns row below reads return statements rather than the zero values behind them. Give the results their anonymous form, or teach that row to read a naked return before allowing this",
				fset.Position(fn.Pos()), fn.Name.Name)
			_, dup := out[fn.Name.Name]
			require.Falsef(t, dup,
				"%s: two functions named %s return (ResolvedType, error); this file keys them by name, so one would shadow the other and go unaudited",
				fset.Position(fn.Pos()), fn.Name.Name)
			out[fn.Name.Name] = fn
		}
	}
	require.NotZero(t, nonTestFiles,
		"no non-test sources were parsed, so every row below would pass by having nothing to look at")
	return out
}

// returnsOf collects every return statement in a function body, rendered back
// to source text. Nested function literals are included: one returning through
// the enclosing signature is as much a part of the surface as a top-level arm.
func returnsOf(t *testing.T, fn *ast.FuncDecl) []*ast.ReturnStmt {
	t.Helper()
	var out []*ast.ReturnStmt
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if ret, ok := n.(*ast.ReturnStmt); ok {
			out = append(out, ret)
		}
		return true
	})
	return out
}

func renderReturn(ret *ast.ReturnStmt) string {
	parts := make([]string, 0, len(ret.Results))
	for _, r := range ret.Results {
		parts = append(parts, types.ExprString(r))
	}
	return "return " + strings.Join(parts, ", ")
}

// calleeName reports the name of the function a single-expression return
// forwards to, and whether the return has that shape at all.
func calleeName(ret *ast.ReturnStmt) (string, bool) {
	if len(ret.Results) != 1 {
		return "", false
	}
	call, ok := ret.Results[0].(*ast.CallExpr)
	if !ok {
		return "", false
	}
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		return fun.Name, true
	case *ast.SelectorExpr:
		return fun.Sel.Name, true
	}
	return "", false
}

func isNilIdent(e ast.Expr) bool {
	id, ok := e.(*ast.Ident)
	return ok && id.Name == "nil"
}

// TestNilColumnTypeIsNotConstructible is the measurement bd gqlc-oltq exists to
// produce. Its four rows are one argument in four parts; each is stated at the
// row it belongs to.
func TestNilColumnTypeIsNotConstructible(t *testing.T) {
	funcs := auditedFuncs(t)

	// The baseline. Every row below reads `funcs`, so a discovery bug that
	// returned an empty or narrowed map would satisfy all of them at once.
	t.Run("the audited surface is the enumerated one", func(t *testing.T) {
		got := make([]string, 0, len(funcs))
		for name := range funcs {
			got = append(got, name)
		}
		sort.Strings(got)
		require.Equal(t, auditedSurface, got,
			"the set of functions returning (ResolvedType, error) has drifted from the set this file enumerates. A NEW name means an arm nobody has argued about can now produce a column type: read its returns and add it here. A MISSING name means the enumeration is claiming coverage of something that no longer exists")
	})

	// Part 1, and the claim itself.
	t.Run("no arm returns a nil type with a nil error", func(t *testing.T) {
		for _, name := range auditedSurface {
			fn, ok := funcs[name]
			require.Truef(t, ok, "%s is enumerated but was not found in the package sources", name)
			for _, ret := range returnsOf(t, fn) {
				if len(ret.Results) != 2 {
					continue
				}
				require.Falsef(t, isNilIdent(ret.Results[0]) && isNilIdent(ret.Results[1]),
					"%s: `return nil, nil` hands the caller a nil ResolvedType on the success path. That value reaches Column.Type, and from there compareBranchColumns renders it while building ErrUnionColumnMismatch. It is the originator bd gqlc-oltq established does not exist — adding one makes the nil handling in describeColumnType and resolvedTypeEqual live, and it needs a fixture proving the refusal rather than a crash. Return a concrete variant, or nil with an error",
					name)
			}
		}
	})

	// Part 2: the surface has no unaudited edge out.
	t.Run("forwarding returns stay inside the audited surface", func(t *testing.T) {
		forwarded := 0
		for _, name := range auditedSurface {
			for _, ret := range returnsOf(t, funcs[name]) {
				callee, ok := calleeName(ret)
				if !ok {
					continue
				}
				forwarded++
				_, audited := funcs[callee]
				require.Truef(t, audited,
					"%s forwards its results to %s, which is not in the audited surface. Part 1 reads only the functions it enumerates, so a forward to an unaudited callee is a hole: whatever that function returns becomes this one's return value unexamined. Either it returns (ResolvedType, error) and belongs in the enumeration, or the call should be assigned and its result returned explicitly",
					name, callee)
			}
		}
		require.NotZero(t, forwarded,
			"no forwarding returns were found at all. The row is then vacuous, which is not the same as the surface having no forwards — check that returnsOf and calleeName still recognise the shape")
	})

	// Part 3: the arms returning a value from elsewhere are enumerated, not
	// pattern-matched, so a new one arrives as a failure with a name on it.
	t.Run("returns of a non-constructed value are enumerated", func(t *testing.T) {
		seen := map[string]bool{}
		for _, name := range auditedSurface {
			for _, ret := range returnsOf(t, funcs[name]) {
				if len(ret.Results) != 2 || !isNilIdent(ret.Results[1]) {
					continue
				}
				if _, isLiteral := ret.Results[0].(*ast.CompositeLit); isLiteral {
					continue
				}
				key := name + ": " + renderReturn(ret)
				seen[key] = true
				_, enumerated := nonConstructingReturns[key]
				require.Truef(t, enumerated,
					"%q returns a value on the success path that it does not construct in the return statement, and no argument is recorded for why that value is not nil. Add an entry to nonConstructingReturns saying why — or, if it can be nil, this is the originator bd gqlc-oltq looked for and did not find",
					key)
			}
		}
		for key := range nonConstructingReturns {
			require.Truef(t, seen[key],
				"nonConstructingReturns enumerates %q, which no longer appears in the sources. A stale entry silently licenses nothing and hides that the arm it argued about is gone", key)
		}
	})

	// The inductive step's premise: the carry maps move column types between
	// Parts and are fed from nothing but s.columns.
	t.Run("the carry maps are written only from column types", func(t *testing.T) {
		fset, files := parsePackageSources(t)
		seen := map[string]bool{}
		for _, f := range files {
			file := fset.Position(f.Pos()).Filename
			if strings.HasSuffix(file, "_test.go") {
				continue
			}
			base := file[strings.LastIndexByte(file, '/')+1:]
			ast.Inspect(f, func(n ast.Node) bool {
				assign, ok := n.(*ast.AssignStmt)
				if !ok {
					return true
				}
				for _, lhs := range assign.Lhs {
					idx, ok := lhs.(*ast.IndexExpr)
					if !ok {
						continue
					}
					sel, ok := idx.X.(*ast.SelectorExpr)
					if !ok || (sel.Sel.Name != "carriedResolvedTypes" && sel.Sel.Name != "exportedResolvedTypes") {
						continue
					}
					require.Lenf(t, assign.Rhs, 1,
						"%s: a multi-value assignment into %s is not a shape this row can read", fset.Position(assign.Pos()), sel.Sel.Name)
					key := base + ": " + types.ExprString(lhs) + " = " + types.ExprString(assign.Rhs[0])
					seen[key] = true
					_, enumerated := carryWriters[key]
					require.Truef(t, enumerated,
						"%s writes into a carry map from a source this file has not accounted for: %q. The `return rt, nil` entry in nonConstructingReturns rests on these maps holding nothing but a previous Part's Column.Type. A writer with another source can seed a nil that projectionType never produced, which would make a nil column type reachable one Part later",
						fset.Position(assign.Pos()), key)
				}
				return true
			})
		}
		for key := range carryWriters {
			require.Truef(t, seen[key],
				"carryWriters enumerates %q, which no longer appears in the sources. The induction rests on this being the complete writer set, so a stale entry means the chain is no longer the one described", key)
		}
	})
}
