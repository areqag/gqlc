package age

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/codegen"
	"github.com/areqag/gqlc/internal/graph"
)

// propertyWidths is every property type this backend carries, one row
// per emitted Go type. Taken from the property table rather than written
// out here, so a width the table gains is a row these sweeps gain.
var propertyWidths = []graph.PropertyType{
	graph.TypeString, graph.TypeBool, graph.TypeInt32, graph.TypeFloat32,
	graph.TypeAnyPropertyValue, graph.ListOf(graph.TypeInt64, false),
	graph.TypeTimestamp,
}

// TestZoneIsMarkedOnlyBesideTheInstant pins the invariant temporal leans
// on. The sidecar read is marked on an entity field whose Go type is the
// instant, and that same field marks the instant decode; a zone marked
// without one would answer temporal false and leave models.go naming
// time with no import for it.
//
// The rows are every emitted Go type an entity field can carry, taken
// from the property table rather than written out here, so a width the
// table gains is a row this sweep gains.
func TestZoneIsMarkedOnlyBesideTheInstant(t *testing.T) {
	sawInstant := false
	for _, pt := range propertyWidths {
		goType, ok := typeMap{}.Property(pt)
		require.True(t, ok, "%s", pt)

		var h helpers
		h.forEntities([]wiredEntity{{Entity: codegen.Entity{
			Name:   "E",
			Fields: []codegen.EntityField{{PropName: "p", Field: "P", GoType: goType}},
		}}})

		if h.zone {
			require.True(t, h.instant, "%s marks the sidecar read with no instant beside it", pt)
			sawInstant = true
		}
		require.Equal(t, h.instant, h.temporal(), "%s: temporal disagrees with the instant it was marked from", pt)
	}
	require.True(t, sawInstant, "no width in the sweep marked the sidecar read, so the invariant went untested")
}

// TestEmittedHelpersAreClosedOverWhatTheyCall renders models.go for a
// batch carrying one width and nothing else, and requires every agtype
// helper the file names to be one the file declares.
//
// A helper is emitted only when the batch marks it, and one helper is
// allowed to be written in terms of another — agtypeInstant reads its
// micros through agtypeInt64 — so a width that marks the caller without
// also marking the callee emits a package that does not compile. Every
// schema fixture in this package carries several widths at once, so each
// of them supplies the callee by some other route and none of them can
// see that hole; a batch of exactly one width is what exposes it.
func TestEmittedHelpersAreClosedOverWhatTheyCall(t *testing.T) {
	for _, pt := range propertyWidths {
		for _, nullable := range []bool{false, true} {
			goType, ok := typeMap{}.Property(pt)
			require.True(t, ok, "%s", pt)

			e := wiredEntity{
				Entity: codegen.Entity{
					Name:   "E",
					Kind:   codegen.EntityNode,
					Fields: []codegen.EntityField{{PropName: "p", Field: "P", GoType: goType, Nullable: nullable}},
				},
				label:      "E",
				annotation: vertexAnnotation,
			}
			var h helpers
			h.forEntities([]wiredEntity{e})
			h.forParams([]codegen.Param{{RawName: "p", Field: "P", GoType: goType, Nullable: nullable}})

			src := renderModels("models", []wiredEntity{e}, h)
			require.Empty(t, undeclaredAgtypeIdents(t, src),
				"a batch of one %s property (nullable=%t) names an agtype helper it does not declare, so the emitted package does not compile",
				pt, nullable)
		}
	}
}

// undeclaredAgtypeIdents parses one emitted models.go and returns every
// agtype helper it names without declaring. A helper reaches the list
// whether it is called outright or passed by value as a decoder
// argument, because either one is a reference the compiler resolves.
func undeclaredAgtypeIdents(t *testing.T, src []byte) []string {
	t.Helper()

	file, err := parser.ParseFile(token.NewFileSet(), "models.go", src, parser.SkipObjectResolution)
	require.NoError(t, err, "the emitted models.go does not parse:\n%s", src)

	declared := map[string]struct{}{}
	for _, d := range file.Decls {
		if fn, ok := d.(*ast.FuncDecl); ok && fn.Recv == nil {
			declared[fn.Name.Name] = struct{}{}
		}
	}

	var missing []string
	seen := map[string]struct{}{}
	record := func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if !ok {
			return true
		}
		// An identifier is a leaf, so the walk stops here either way.
		if _, isDeclared := declared[id.Name]; isDeclared || !strings.HasPrefix(id.Name, "agtype") {
			return false
		}
		if _, dup := seen[id.Name]; !dup {
			seen[id.Name] = struct{}{}
			missing = append(missing, id.Name)
		}
		return false
	}
	ast.Inspect(file, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if !ok {
			return true
		}
		// A declaration's own name is not a reference to it, so the walk
		// skips the name and descends into the signature and the body.
		ast.Inspect(fn.Type, record)
		if fn.Body != nil {
			ast.Inspect(fn.Body, record)
		}
		return false
	})
	return missing
}
