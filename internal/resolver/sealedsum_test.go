// This file is an external test package on purpose: the claim under test is
// about what a caller OUTSIDE internal/resolver can construct, and inside the
// package the marker method is writable, so an in-package witness would
// measure nothing.
package resolver_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/resolver"
)

// Each embedder promotes one variant's methods, including the unexported
// marker. None declares a method of its own. The compile-time assignments in
// inhabitants are the first half of the measurement — this file failing to
// build is itself the claim being falsified.
type (
	embedNode      struct{ resolver.ResolvedNode }
	embedProperty  struct{ resolver.ResolvedProperty }
	embedEdge      struct{ resolver.ResolvedEdge }
	embedEdgeUnion struct{ resolver.ResolvedEdgeUnion }
	embedScalar    struct{ resolver.ResolvedScalar }
	embedTemporal  struct{ resolver.ResolvedTemporal }
	embedList      struct{ resolver.ResolvedList }
	embedUnknown   struct{ resolver.ResolvedUnknown }
)

// inhabitant holds the three forms of one variant that satisfy ResolvedType.
// value is the form the switches under audit enumerate; pointer and embedded
// are the two an out-of-package caller reaches without declaring the marker.
type inhabitant struct {
	value    resolver.ResolvedType
	pointer  resolver.ResolvedType
	embedded resolver.ResolvedType
}

// inhabitants is keyed by the variant name that internal/resolver declares.
// The key set is checked against the package's own sources by
// TestResolvedTypeSumIsNotClosed/declared_variants, so adding a ninth variant
// without extending this map fails rather than silently narrowing every row
// below it.
var inhabitants = map[string]inhabitant{
	"ResolvedNode": {
		value:    resolver.ResolvedNode{},
		pointer:  &resolver.ResolvedNode{},
		embedded: embedNode{},
	},
	"ResolvedProperty": {
		value:    resolver.ResolvedProperty{},
		pointer:  &resolver.ResolvedProperty{},
		embedded: embedProperty{},
	},
	"ResolvedEdge": {
		value:    resolver.ResolvedEdge{},
		pointer:  &resolver.ResolvedEdge{},
		embedded: embedEdge{},
	},
	"ResolvedEdgeUnion": {
		value:    resolver.ResolvedEdgeUnion{},
		pointer:  &resolver.ResolvedEdgeUnion{},
		embedded: embedEdgeUnion{},
	},
	"ResolvedScalar": {
		value:    resolver.ResolvedScalar{},
		pointer:  &resolver.ResolvedScalar{},
		embedded: embedScalar{},
	},
	"ResolvedTemporal": {
		value:    resolver.ResolvedTemporal{},
		pointer:  &resolver.ResolvedTemporal{},
		embedded: embedTemporal{},
	},
	"ResolvedList": {
		value:    resolver.ResolvedList{},
		pointer:  &resolver.ResolvedList{},
		embedded: embedList{},
	},
	"ResolvedUnknown": {
		value:    resolver.ResolvedUnknown{},
		pointer:  &resolver.ResolvedUnknown{},
		embedded: embedUnknown{},
	},
}

// matchValueForm is the shape every switch under audit has: one arm per
// declared variant in its value form, and a default. It returns the name of
// the arm that matched, or "" for the default. The audited switches differ in
// what their arms do, not in which types they name, so what this reports about
// a given input holds for all of them.
func matchValueForm(t resolver.ResolvedType) string {
	switch t.(type) {
	case resolver.ResolvedNode:
		return "ResolvedNode"
	case resolver.ResolvedProperty:
		return "ResolvedProperty"
	case resolver.ResolvedEdge:
		return "ResolvedEdge"
	case resolver.ResolvedEdgeUnion:
		return "ResolvedEdgeUnion"
	case resolver.ResolvedScalar:
		return "ResolvedScalar"
	case resolver.ResolvedTemporal:
		return "ResolvedTemporal"
	case resolver.ResolvedList:
		return "ResolvedList"
	case resolver.ResolvedUnknown:
		return "ResolvedUnknown"
	default:
		return ""
	}
}

// declaredMarkers reads internal/resolver's own non-test sources and returns
// one sorted entry per isResolvedType declaration: the receiver type's name
// for a value receiver, and that name prefixed with "*" for a pointer
// receiver. Encoding the receiver form into the entry rather than asserting it
// separately keeps both drift modes — a variant appearing or disappearing, and
// a marker moving to a pointer receiver — on the single comparison below.
//
// It walks the AST rather than grepping the sources because a commented-out
// declaration satisfies a grep, which would let the enumeration above drift
// behind a comment.
func declaredMarkers(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(".")
	require.NoError(t, err)

	fset := token.NewFileSet()
	var got []string
	files := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(".", name), nil, 0)
		require.NoErrorf(t, err, "parsing %s", name)
		files++
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || len(fn.Recv.List) != 1 || fn.Name.Name != "isResolvedType" {
				continue
			}
			recv := fn.Recv.List[0].Type
			prefix := ""
			if star, isPointer := recv.(*ast.StarExpr); isPointer {
				recv, prefix = star.X, "*"
			}
			id, ok := recv.(*ast.Ident)
			require.Truef(t, ok, "%s: unexpected receiver shape %T on isResolvedType", name, recv)
			got = append(got, prefix+id.Name)
		}
	}
	// A filter that matched nothing would return an empty set, which agrees
	// with an empty enumeration rather than contradicting it.
	require.NotZero(t, files, "no non-test sources parsed from the package directory")
	sort.Strings(got)
	return got
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// TestResolvedTypeSumIsNotClosed measures what ResolvedType's unexported
// marker does and does not buy, and is what the type's doc comment cites.
//
// What it buys: an out-of-package type cannot declare isResolvedType, so the
// eight variants are the whole set of types that DECLARE the marker. That half
// is enforced by the compiler and has no row here.
//
// What it does not buy: two constructions inhabit ResolvedType without
// declaring the marker — the pointer form of a variant, and a struct embedding
// one. Neither matches an arm naming the value form, so a switch over the
// eight reaches its default on both.
func TestResolvedTypeSumIsNotClosed(t *testing.T) {
	t.Run("declared variants", func(t *testing.T) {
		require.Equal(t, sortedKeys(inhabitants), declaredMarkers(t),
			"the set of isResolvedType declarations and the set this file enumerates have diverged. A bare name added or removed means a variant landed or left: extend inhabitants and matchValueForm, or drop the stale entries. A name reported with a leading \"*\" means that marker moved to a pointer receiver, which is the mechanism the pointer-form rows below and the doc comment on ResolvedType both rest on")
	})

	// The ALLOW half. Without it every REFUSE row below is satisfied by a
	// matchValueForm that returns "" for everything.
	t.Run("value form matches its own arm", func(t *testing.T) {
		for _, name := range sortedKeys(inhabitants) {
			require.Equalf(t, name, matchValueForm(inhabitants[name].value),
				"%s in its value form must match the arm naming it", name)
		}
	})

	t.Run("pointer form inhabits but does not match", func(t *testing.T) {
		for _, name := range sortedKeys(inhabitants) {
			in := inhabitants[name]
			require.NotNilf(t, in.pointer, "%s: no pointer form enumerated", name)
			require.Emptyf(t, matchValueForm(in.pointer),
				"*%s reached the arm naming %s; a pointer form matching the value arm would make the default narrower than this test claims", name, name)
		}
	})

	t.Run("embedded form inhabits but does not match", func(t *testing.T) {
		for _, name := range sortedKeys(inhabitants) {
			in := inhabitants[name]
			require.NotNilf(t, in.embedded, "%s: no embedding form enumerated", name)
			require.Emptyf(t, matchValueForm(in.embedded),
				"a struct embedding %s reached the arm naming it; the embedder promotes the marker but is a distinct type, so it is expected at the default", name)
		}
	})
}
