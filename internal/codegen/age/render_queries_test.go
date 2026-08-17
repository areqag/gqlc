package age

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"maps"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestDollarTagClosesOnlyAtTheEnds pins the delimiter choice against the
// string the SQL parser actually scans, tag + text + tag. Each row is a
// text whose final bytes interact with a candidate: an interior
// occurrence, a straddle across the text/tag boundary, a bare dollar
// that opens no delimiter, and a straddle on the escalated candidate so
// the second turn of the loop is exercised.
func TestDollarTagClosesOnlyAtTheEnds(t *testing.T) {
	tests := []struct {
		name string
		text string
		want string
	}{
		{name: "no dollar", text: "MATCH (p:Person) RETURN p.name", want: "$gqlc$"},
		{name: "parameter reference", text: "MATCH (p:Person) WHERE p.id = $id RETURN p.name", want: "$gqlc$"},
		{name: "interior occurrence", text: "RETURN '$gqlc$'", want: "$gqlc1$"},
		{name: "straddling occurrence", text: "RETURN p.name\n// trailing $gqlc", want: "$gqlc1$"},
		{name: "trailing dollar", text: "SET p.name = $", want: "$gqlc$"},
		{name: "straddle on the escalated candidate", text: "RETURN '$gqlc$' // $gqlc1", want: "$gqlc2$"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tag := dollarTag(tt.text)
			require.Equal(t, tt.want, tag)

			body := tag + tt.text + tag
			require.Equal(t, 2, strings.Count(body, tag),
				"the delimiter occurs somewhere other than the two ends of %q", body)
			require.Equal(t, len(body)-len(tag), strings.LastIndex(body, tag),
				"the scanner would close %q before the end", body)
		})
	}
}

// TestDecodeFuncNamesTheHelperForEveryServedCarrier pins the arm-to-helper
// mapping. It is the unit-level half only: what proves the emission is
// unchanged is the golden corpus, which renders these carriers into
// models.go and the per-source query files.
//
// string is a row because it moved out of the default arm, which is now
// the refusal: of the carriers the type table produces it was the one
// this switch named by falling through rather than by matching.
func TestDecodeFuncNamesTheHelperForEveryServedCarrier(t *testing.T) {
	tests := []struct {
		goType string
		want   string
	}{
		{goType: "bool", want: "agtypeBool"},
		{goType: "string", want: "agtypeString"},
		{goType: "int64", want: "agtypeInt64"},
		{goType: "int8", want: "agtypeInt64"},
		{goType: "uint32", want: "agtypeInt64"},
		{goType: "float64", want: "agtypeFloat64"},
		{goType: "float32", want: "agtypeFloat64"},
		{goType: goInstant, want: "agtypeInstant"},
		{goType: "any", want: "agtypeValue"},
		{goType: "map[string]any", want: "agtypeMap"},
		{goType: "[]string", want: "agtypeListOfString"},
	}
	for _, tt := range tests {
		t.Run(tt.goType, func(t *testing.T) {
			require.Equal(t, tt.want, decodeFunc(tt.goType))
		})
	}
}

// TestDecodeFuncRefusesACarrierItWasNotTaught pins what a carrier with no
// arm produces. The rows spell the message out rather than compute it, so
// a change to the wording is a change to this file.
//
// The Go type and the carrier are named separately because agtypeCarrier
// can rewrite one into the other. It does not rewrite any of these rows —
// its rewrites are the fixed numeric-width list, and every target of one
// has an arm — so the two slots hold the same text here and the rows do
// not tell them apart. What tells them apart is the format string.
func TestDecodeFuncRefusesACarrierItWasNotTaught(t *testing.T) {
	tests := []struct {
		goType string
		want   string
	}{
		{
			goType: "decimal.Decimal",
			want:   `age codegen bug: Go type "decimal.Decimal" carries as "decimal.Decimal", which decodeFunc has no arm for`,
		},
		{
			goType: "byte",
			want:   `age codegen bug: Go type "byte" carries as "byte", which decodeFunc has no arm for`,
		},
		{
			goType: "complex128",
			want:   `age codegen bug: Go type "complex128" carries as "complex128", which decodeFunc has no arm for`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.goType, func(t *testing.T) {
			require.PanicsWithValue(t, tt.want, func() { decodeFunc(tt.goType) })
		})
	}

	// A slice takes its wrapper's name from the text alone, so the slice
	// arm asks nothing about the element and refuses nothing. The element's
	// refusal is one level in, at the decoder the wrapper is written with —
	// which is where the emission reaches it (writeListHelper).
	t.Run("a slice refuses at its element", func(t *testing.T) {
		require.NotPanics(t, func() { decodeFunc("[]byte") })
		require.PanicsWithValue(t,
			`age codegen bug: Go type "byte" carries as "byte", which decodeFunc has no arm for`,
			func() { elemDecoder("byte") })
	})
}

// TestDecodeFuncHasAnArmForEveryCarrierTheTypeTableProduces is the closure
// pin: it reads every Go type text the backend's type table can name and
// requires decodeFunc to have an arm for the carrier of each.
//
// decodeFunc is downstream of admission — a Go type reaches it only
// because the table produced it — so this is the seam where a carrier the
// table gains without an arm here becomes visible. Without it the refusal
// fires only once some fixture happens to project the new width, and a
// table entry with no fixture behind it lands silently.
//
// What this adds over its neighbours is the Scalar half. On the Property
// half it is the WEAKER of two: TestTheDecodeAndTheGateReadTheSameSidecarKeys
// already sweeps declaredPropertyTypes through typeMap.Property into
// writeEntityFieldDecode, and so reaches decodeFunc for every declared
// property width keyed on the graph type constants rather than on what
// types.go returns. Nothing else is keyed on typeMap.Scalar or
// typeMap.Temporal.
//
// The counts below are per method rather than over the map, because one
// assertion over three sources fires only when all three fall silent: a
// walk that dropped Scalar and Temporal would still hand back a
// non-empty map with 17 rows in it. A count pins the SIZE of the sweep
// and nothing about its membership; membership is what the subtests
// pin, one per Go type text.
//
// Temporal's zero is an assertion about this backend, not a category
// that happens to be empty. Every arm of typeMap.Temporal answers
// ok=false, which is why Prepare refuses a temporal column with
// ErrUnrepresentableTemporal and no temporal carrier reaches decodeFunc
// at all. The number is pinned so that a temporal width gaining a
// carrier cannot pass through here silently: it fails that line, and a
// carrier with no arm fails at its subtest first.
//
// The counts come after the subtests deliberately. require aborts the
// test at the first failure, so a count read ahead of the loop would
// answer a table that grew an unserved carrier with "the size changed"
// and never reach the row that says which carrier has no arm. Read after,
// both are reported and the row is read first. The method set is read
// ahead of the loop instead, because it says what the sweep ranged over
// at all.
func TestDecodeFuncHasAnArmForEveryCarrierTheTypeTableProduces(t *testing.T) {
	byMethod := typeTableGoTypes(t)

	require.Equal(t, []string{"Property", "Scalar", "Temporal"}, slices.Sorted(maps.Keys(byMethod)),
		"these are the typeMap methods the walk read out of types.go; an empty set means it read none "+
			"and the sweep ranged over nothing, and a longer one means the table grew a method unread here")

	for _, method := range slices.Sorted(maps.Keys(byMethod)) {
		for _, goType := range byMethod[method] {
			t.Run(method+"/"+goType, func(t *testing.T) {
				requireCarrierHasAnArm(t, goType)
			})
		}
	}

	require.Len(t, byMethod["Property"], 17, "typeMap.Property named %v", byMethod["Property"])
	require.Len(t, byMethod["Scalar"], 6, "typeMap.Scalar named %v", byMethod["Scalar"])
	require.Empty(t, byMethod["Temporal"], "typeMap.Temporal named %v, so this backend now carries a temporal "+
		"width: read it against decodeFunc before moving this number", byMethod["Temporal"])
}

// requireCarrierHasAnArm requires decodeFunc to name a helper for one Go
// type text and, where that text is a slice, for the element it decodes
// through at every depth.
func requireCarrierHasAnArm(t *testing.T, goType string) {
	t.Helper()
	for {
		require.NotPanics(t, func() { decodeFunc(goType) },
			"the type table names Go type %q, which decodeFunc has no arm for the carrier of", goType)
		elem, ok := strings.CutPrefix(goType, "[]")
		if !ok {
			return
		}
		goType = elem
	}
}

// typeTableGoTypes is every Go type text the type table names as a
// returned string literal, keyed by the typeMap method that names it. A
// method that names none is keyed to an empty entry, so the caller can
// tell a method that produces no Go type from one the walk did not reach.
//
// Read out of types.go's AST rather than listed here: a list would be a
// copy of the table, and a copy goes stale in the case this sweep exists
// for. Reading the AST also means a commented-out arm contributes
// nothing, which a scan of the source bytes could not tell.
//
// "As a returned string literal" is the whole of what this reads, and it
// is why the walk REFUSES a return it cannot read rather than passing
// over it. A Go type text returned through a constant, a variable or a
// call is one this cannot see, and a sweep that skipped such a row would
// go green because it was looking at nothing — which is the defect this
// sweep exists against, one level up. So a return in a typeMap method is
// one of the two shapes below or it fails the test at its position.
//
// The two shapes: a string literal, which names a Go type and is
// collected — the refusing arms' empty literal names none and is dropped
// — and `"[]" + elem`, the list arm's composed text, which is accepted
// and contributes nothing of its own. decodeFunc peels a slice down to
// its element, and the element widths that arm composes with are the
// literals of the scalar arms beside it.
func typeTableGoTypes(t *testing.T) map[string][]string {
	t.Helper()

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "types.go", nil, parser.SkipObjectResolution)
	require.NoError(t, err, "types.go does not parse")

	byMethod := map[string][]string{}
	for _, d := range file.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Body == nil || !isTypeMapMethod(fn) {
			continue
		}
		if _, dup := byMethod[fn.Name.Name]; !dup {
			byMethod[fn.Name.Name] = nil
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			ret, ok := n.(*ast.ReturnStmt)
			if !ok {
				return true
			}
			text, names := returnedGoType(t, fset, fn.Name.Name, ret)
			if !names || slices.Contains(byMethod[fn.Name.Name], text) {
				return true
			}
			byMethod[fn.Name.Name] = append(byMethod[fn.Name.Name], text)
			return true
		})
	}
	return byMethod
}

// returnedGoType is the Go type text one return of a typeMap method
// names, and whether it names one at all. It fails the test on a return
// whose shape this walk cannot read: see typeTableGoTypes for why the
// answer there is a refusal and not a false.
//
// The failing expression is rendered from the AST rather than sliced out
// of the source bytes, so what the message quotes is what the compiler
// sees and not what a comment beside it says.
func returnedGoType(t *testing.T, fset *token.FileSet, method string, ret *ast.ReturnStmt) (string, bool) {
	t.Helper()

	const cannotRead = "this walk cannot read the return at %s: %s returns %s, and this reads a string " +
		"literal or the list arm's `\"[]\" + elem` and nothing else — a Go type named any other way is " +
		"invisible here, so the walk refuses rather than skip it"
	where := fset.Position(ret.Pos())

	require.NotEmpty(t, ret.Results, cannotRead, where, method, "no value")

	switch first := ret.Results[0].(type) {
	case *ast.BasicLit:
		require.Equal(t, token.STRING, first.Kind, cannotRead, where, method, first.Value)
		text, err := strconv.Unquote(first.Value)
		require.NoError(t, err, "%s returns a string literal that does not unquote: %s", method, first.Value)
		// The refusing arms return the empty text beside ok=false, which
		// names no Go type.
		return text, text != ""
	case *ast.BinaryExpr:
		lhs, isLit := first.X.(*ast.BasicLit)
		require.True(t, isLit && first.Op == token.ADD && lhs.Kind == token.STRING && lhs.Value == `"[]"`,
			cannotRead, where, method, types.ExprString(first))
		return "", false
	default:
		require.Fail(t, "unreadable return in the type table", cannotRead, where, method, types.ExprString(first))
		return "", false
	}
}

// isTypeMapMethod reports whether a declaration is a method on typeMap,
// which is the whole of the backend's Go-type table.
func isTypeMapMethod(fn *ast.FuncDecl) bool {
	if fn.Recv == nil || len(fn.Recv.List) != 1 {
		return false
	}
	recv := fn.Recv.List[0].Type
	if star, ok := recv.(*ast.StarExpr); ok {
		recv = star.X
	}
	id, ok := recv.(*ast.Ident)
	return ok && id.Name == "typeMap"
}
