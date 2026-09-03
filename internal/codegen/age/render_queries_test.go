package age_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"maps"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/codegen"
	"github.com/areqag/gqlc/internal/codegen/age"
	"github.com/areqag/gqlc/internal/queryfile"
)

// TestDollarTagClosesOnlyAtTheEnds pins the delimiter choice against the
// scan an SQL parser performs: having consumed the opening tag, it closes
// on the FIRST occurrence after it, so the property is that the first
// match in text+tag lands at len(text). Each row is a text whose final
// bytes interact with a candidate: an interior occurrence, a straddle
// across the text/tag boundary, a bare dollar that opens no delimiter,
// and a straddle on the escalated candidate so the second turn of the
// loop is exercised.
//
// First-match is not interchangeable with counting the delimiter in
// tag+text+tag, which is how this read until bd gqlc-vqx87.
// strings.Count is non-overlapping: for the straddling row under the tag
// $gqlc$, the two counted matches are the opening tag and the straddle
// across the text/tag boundary, the emission's own closing tag one byte
// later is never counted, and LastIndex still lands at the end. So both
// of the assertions this replaces passed on a delimiter closing five
// bytes early — the B1 failure gqlc-35yu.7 exists to prevent. The `want`
// column caught it and still does; what changed is that the assertions
// past `want` now state the property too, in the one spelling
// TestEmittedQueryTextIsTheBytesTheTagWasChosenOn already uses.
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
			tag := age.DollarTag(tt.text)
			require.Equal(t, tt.want, tag)

			require.Equal(t, len(tt.text), strings.Index(tt.text+tag, tag),
				"the scanner closes %q before the end", tt.text+tag)
		})
	}
}

// TestEmittedQueryTextIsTheBytesTheTagWasChosenOn closes the gap the test
// above structurally cannot see. That one hands dollarTag a string and judges
// the answer against the same string. But the delimiter is chosen at generate
// time on SourceText, while the SQL parser scans the VALUE of the raw string
// literal SourceText was emitted into — and those are not the same bytes
// whenever the text holds a carriage return, which such a literal discards
// (bd gqlc-7f9a). So this renders the file and reads BOTH sides back out of
// the emitted Go: the constant's value, and the tag the method actually
// passes to cypherStmt.
//
// Reading them through go/parser rather than by string surgery is what makes
// the fence honest — it is the decoding the compiler does, so it reports what
// a running program would send rather than the source bytes the emission
// wrote. Recomputing the tag here instead would judge the emission against
// this test's own second opinion, and could not see the emission handing
// dollarTag different bytes from the ones it emits.
func TestEmittedQueryTextIsTheBytesTheTagWasChosenOn(t *testing.T) {
	// Every text prepare admits — the backtick and the carriage return are
	// refused upstream (internal/codegen: ErrOutOfC6Scope), which is what makes
	// this invariant satisfiable at all. The rows are the shapes whose bytes a
	// raw literal or a delimiter search is most likely to disagree over: the
	// delimiter's own spelling, a straddle across the text/tag boundary, an
	// escalated candidate, a backslash, and both quote characters.
	for _, text := range []string{
		"MATCH (p:Person) RETURN p.name",
		"MATCH (p:Person) WHERE p.id = $id RETURN p.name",
		"RETURN '$gqlc$'",
		"RETURN p.name\n// trailing $gqlc",
		"RETURN '$gqlc$' // $gqlc1",
		`RETURN 'a\nb' AS x`,
		"RETURN \"double\" AS x\n\tRETURN 'single' AS y",
	} {
		t.Run(text, func(t *testing.T) {
			p := codegen.Query{
				NamedQuery: codegen.NamedQuery{
					Name:        "Q",
					SourceText:  text,
					Cardinality: queryfile.CardinalityExec,
				},
				MethodName: "Q",
				Bare:       "q",
			}
			rendered := age.RenderCypherFile("p", []codegen.Query{p})

			emitted := constValue(t, rendered, codegen.QueryTextConst(p))
			require.Equal(t, text, emitted,
				"the emitted constant's value is not the text the emission was given")

			// Judged with the delimiter appended, and on the FIRST match,
			// because that is what an SQL scanner does: it closes on the
			// earliest occurrence after the opening tag. Counting matches
			// instead would miss a straddle — strings.Count is
			// non-overlapping, so a text ending $gqlc under the tag $gqlc$
			// composes a body whose two counted matches are the opening tag
			// and the straddle, with the emission's own closing tag never
			// counted and LastIndex still landing at the end.
			tag := emittedDollarTag(t, rendered)
			require.Equal(t, len(emitted), strings.Index(emitted+tag, tag),
				"the scanner closes %q before the end of the EMITTED text", emitted+tag)
		})
	}
}

// emittedDollarTag returns the delimiter the rendered method hands cypherStmt
// — the bytes the SQL parser will be told to close on, as opposed to whatever
// dollarTag would answer if asked again here.
func emittedDollarTag(t *testing.T, src []byte) string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "rendered.go", src, 0)
	require.NoError(t, err, "the emission is not parseable Go:\n%s", src)
	var tags []string
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "cypherStmt" {
			return true
		}
		lit, ok := call.Args[0].(*ast.BasicLit)
		require.True(t, ok, "cypherStmt's delimiter argument is not a literal")
		val, err := strconv.Unquote(lit.Value)
		require.NoError(t, err)
		tags = append(tags, val)
		return true
	})
	require.Len(t, tags, 1, "the emission should compose exactly one statement:\n%s", src)
	return tags[0]
}

// constValue compiles-by-parsing one rendered file and returns the value of
// the named string constant.
func constValue(t *testing.T, src []byte, name string) string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "rendered.go", src, 0)
	require.NoError(t, err, "the emission is not parseable Go:\n%s", src)
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			require.True(t, ok, "a const spec is not a value spec")
			if vs.Names[0].Name != name {
				continue
			}
			lit, ok := vs.Values[0].(*ast.BasicLit)
			require.True(t, ok, "const %s is not a literal", name)
			val, err := strconv.Unquote(lit.Value)
			require.NoError(t, err)
			return val
		}
	}
	t.Fatalf("the emission declares no const %s:\n%s", name, src)
	return ""
}

// TestDecodeFuncNamesTheHelperForEveryServedCarrier pins the arm-to-helper
// mapping. It is the unit-level half only: what proves the emission is
// unchanged is the golden corpus, which renders these carriers into
// models.go and the per-source query files.
//
// string is a row because it moved out of the default arm, which is now
// the refusal: of the carriers the type table produces it was the one
// this switch named by falling through rather than by matching.
//
// A width narrower than its carrier names the checked narrowing rather
// than the carrier's own helper, which is what makes a stored value the
// width cannot hold fail the read instead of wrapping (ADR 0037). Both
// sides of that split are rows here: int64 and float64 are the widths
// that ARE their carrier and still name the plain helper, so a change
// that routed everything through the narrowing would go red too.
func TestDecodeFuncNamesTheHelperForEveryServedCarrier(t *testing.T) {
	tests := []struct {
		goType string
		want   string
	}{
		{goType: "bool", want: "agtypeBool"},
		{goType: "string", want: "agtypeString"},
		{goType: "int64", want: "agtypeInt64"},
		{goType: "int8", want: "agtypeIntAs[int8]"},
		{goType: "uint32", want: "agtypeIntAs[uint32]"},
		{goType: "uint64", want: "agtypeIntAs[uint64]"},
		{goType: "float64", want: "agtypeFloat64"},
		{goType: "float32", want: "agtypeFloat32"},
		{goType: age.GoInstant, want: "agtypeInstant"},
		{goType: "any", want: "agtypeValue"},
		{goType: "map[string]any", want: "agtypeMap"},
		{goType: "[]string", want: "agtypeListOfString"},
	}
	for _, tt := range tests {
		t.Run(tt.goType, func(t *testing.T) {
			require.Equal(t, tt.want, age.DecodeFunc(tt.goType))
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
			require.PanicsWithValue(t, tt.want, func() { age.DecodeFunc(tt.goType) })
		})
	}

	// A slice takes its wrapper's name from the text alone, so the slice
	// arm asks nothing about the element and refuses nothing. The element's
	// refusal is one level in: writeListHelper writes the wrapper with the
	// ELEMENT's decoder, so "[]byte" is named here and refused there. That
	// second half is not asserted in this test, because asserting it here
	// would only re-run the "byte" row above — writeListHelper's routing of
	// the element is held by TestEmittedHelpersDecodeTheAgtypeCorpus and
	// TestListExpressionColumnDecodesThroughTheSliceWrapper, both of which
	// go red when the wrapper is written with the slice type instead.
	t.Run("a slice names a wrapper without asking about its element", func(t *testing.T) {
		require.NotPanics(t, func() { age.DecodeFunc("[]byte") })
	})
}

// TestDecodeFuncHasAnArmForEveryCarrierTheTypeTableProduces is the closure
// pin: it reads the Go type texts the backend's type table names as
// returned string literals, in every .go file of this package, and
// requires decodeFunc to have an arm for the carrier of each. A return
// the walk cannot read that way is refused at its position rather than
// passed over, so a row spelled through a constant goes red as well —
// at the refusal rather than at a subtest of its own.
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
// types.go returns. Nothing else reaches decodeFunc through typeMap.Scalar
// or typeMap.Temporal: TestTypeMapScalar (types_test.go) does read that
// method, but reads its arms against expected texts, so a width Scalar
// gains without an arm here is a row that file grows rather than a
// failure it raises.
//
// The counts below are per method rather than over the map, because one
// assertion over three sources fires only when all three fall silent: a
// walk that dropped Scalar and Temporal would still hand back a
// non-empty map with 17 rows in it. A count pins the SIZE of the sweep
// and nothing about its membership; membership is what the subtests
// pin, one per Go type text.
//
// Temporal's zero is an assertion about this backend, not a category
// that happens to be empty, and it is narrower than it reads. It ranges
// over temporal EXPRESSION kinds — the `date()` and `duration()`
// constructors — every arm of which answers ok=false, so Prepare refuses
// such a column with ErrUnrepresentableTemporal and none reaches
// decodeFunc. Temporal PROPERTY widths are a separate question answered
// by Property, and three of them do reach decodeFunc: DATE, LOCAL TIME
// and DURATION ride the neutral carriers. The number is pinned so that a
// constructor gaining a carrier cannot pass through here silently: it
// fails that line, and a carrier with no arm fails at its subtest first.
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
		"these are the typeMap methods the walk read out of the package's .go files; an empty set means "+
			"it read none and the sweep ranged over nothing, and a longer one means the table grew a "+
			"method the counts below have not been read against")

	for _, method := range slices.Sorted(maps.Keys(byMethod)) {
		for _, goType := range byMethod[method] {
			t.Run(method+"/"+goType, func(t *testing.T) {
				requireCarrierHasAnArm(t, goType)
			})
		}
	}

	require.Len(t, byMethod["Property"], 21, "typeMap.Property named %v", byMethod["Property"])
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
		require.NotPanics(t, func() { age.DecodeFunc(goType) },
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
// Read out of the AST rather than listed here: a list would be a copy of
// the table, and a copy goes stale in the case this sweep exists for.
// Reading the AST also means a commented-out arm contributes nothing,
// which a scan of the source bytes could not tell.
//
// The walk parses every .go file the package directory holds, not
// types.go alone. Naming one file was a hole of the shape this sweep
// exists against: a typeMap method declared in a sibling file entered no
// key at all, so the method-set pin still read the same three names and
// the Go types that method returned were swept by nothing. Measured
// before the widening — a second file returning a carrier decodeFunc
// panics on, spelled as a plain literal, left the package green. A file
// the walk never opens is skipped, and a skip is what this walk exists
// to refuse.
//
// _test.go files are read too, deliberately: dropping a class of file
// reopens that hole in a smaller place, and a method on typeMap is the
// table wherever in the package it is declared. What the walk leaves out
// is what is not a .go file of this directory: every entry that is a
// directory is skipped, unconditionally and by shape rather than by name.
// testdata/ carries no exclusion of its own — it is simply the only
// subdirectory there is.
//
// Which is worth saying, because this comment used to call that tree the
// generator's OUTPUT and it is not: it holds four .gql generator INPUTS
// and corpus_test.go.txt, a hand-written driver, while the generator's
// actual output goes to a t.TempDir(). What puts it out of this table's
// reach is neither of those but the go tool's own rule — a directory named
// testdata is ignored whole, so nothing under it is in package age and
// nothing under it can declare a method on typeMap. Measured rather than
// recited: a testdata/*.go declaring `package age`, a duplicate typeMap
// and a line of invalid Go leaves `go build` on this package green and
// never enters `go list`'s GoFiles. That is a guarantee and not a count,
// which is why nothing here asserts that testdata/ holds no .go file: it
// holds none today, and one appearing would still not be a hole
// (bd gqlc-ww8u).
//
// Selection is by directory listing and not by build list, so a .go file
// the go tool itself skips — a build tag, a leading underscore — is read
// here and must parse. That is a claim about files IN this directory and
// it does not reach downwards; it is the reason the directory skip has to
// be unconditional rather than a judgement about any one subtree, since a
// listing-driven walk that descended would read a testdata file the go
// tool never compiles.
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
//
// That second shape is where the walk's blind spot is, and it is
// narrower than the whole shape. Where the right-hand side is itself a
// string literal the tree names the type outright, so `"[]" +
// "decimal.Decimal"` is read and swept like any other literal; it used
// to be dropped, which made a carrier spelled that way compile,
// preserve the arm count, and reach no subtest (bd gqlc-geny).
//
// What the AST cannot close is an RHS naming a VALUE: `"[]" + elemTy`
// and `"[]" + goDecimal` are the same tree, so what that value holds is
// invisible without type resolution. Today it holds Property's own
// recursive answer, which is a literal of an arm beside it (types.go,
// the KindList branch). A list arm composed with some other value would
// be accepted here and swept by nothing.
func typeTableGoTypes(t *testing.T) map[string][]string {
	t.Helper()

	// A go test runs in the directory of the package under test, which is
	// the directory the table lives in.
	entries, err := os.ReadDir(".")
	require.NoError(t, err, "the package directory does not read, so the walk has no files to range over")

	fset := token.NewFileSet()
	byMethod := map[string][]string{}
	read := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		read++
		file, err := parser.ParseFile(fset, entry.Name(), nil, parser.SkipObjectResolution)
		require.NoError(t, err, "%s does not parse", entry.Name())
		collectTypeMapGoTypes(t, fset, file, byMethod)
	}
	require.NotZero(t, read, "the package directory held no .go file, so the walk ranged over nothing")

	return byMethod
}

// collectTypeMapGoTypes adds one file's typeMap methods to byMethod. A
// method is keyed the moment it is found, before any of its returns are
// read, so a method that names no Go type is told apart from one the
// walk did not reach.
func collectTypeMapGoTypes(t *testing.T, fset *token.FileSet, file *ast.File, byMethod map[string][]string) {
	t.Helper()

	for _, d := range file.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Body == nil || !isTypeMapMethod(fn) {
			continue
		}
		if !namesAGoType(t, fset, fn) {
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
		// A right-hand side that is itself a string literal names its Go
		// type here as plainly as the BasicLit arm above does, so it is
		// read rather than dropped: `"[]" + "decimal.Decimal"` compiles,
		// preserves the arm count, and was invisible to this walk (bd
		// gqlc-geny). What stays invisible is an RHS naming a VALUE —
		// `"[]" + elemTy` — where the tree says nothing about what the
		// value holds.
		rhs, isLit := first.Y.(*ast.BasicLit)
		if !isLit {
			return "", false
		}
		elem, err := strconv.Unquote(rhs.Value)
		require.NoError(t, err, "%s returns a literal this walk cannot read as a Go type text: %s", method, rhs.Value)
		return "[]" + elem, true
	default:
		require.Fail(t, "unreadable return in the type table", cannotRead, where, method, types.ExprString(first))
		return "", false
	}
}

// namesAGoType reports whether a typeMap method is one of the carrier
// methods this walk reads — the ones answering the CARRIER question with
// Go type text — as opposed to StorableProperty, which answers the
// storage question with a bool and names no Go type (ADR 0035).
//
// Told apart by the declared result shape rather than by method name. A
// name list would go stale silently the next time the table grows, which
// is the failure this walk's own refusal exists to prevent; the shape is
// read off the same AST as everything else here.
//
// Exactly two shapes are recognised and a third is REFUSED, not skipped.
// Skipping an unrecognised shape is how a carrier method would leave the
// census unnoticed and take its Go types with it — the walk would then
// report an arm census over a table it had silently narrowed.
func namesAGoType(t *testing.T, fset *token.FileSet, fn *ast.FuncDecl) bool {
	t.Helper()

	results := fn.Type.Results
	require.NotNil(t, results, "typeMap method %s returns nothing, so it is neither a carrier nor a predicate", fn.Name.Name)
	require.NotEmpty(t, results.List, "typeMap method %s returns nothing, so it is neither a carrier nor a predicate", fn.Name.Name)

	first, ok := results.List[0].Type.(*ast.Ident)
	require.True(t, ok,
		"typeMap method %s at %s returns %s first, and this walk knows only `string` (a carrier) and "+
			"`bool` (the storage predicate); a third shape is refused rather than skipped, because "+
			"skipping one would drop a carrier method out of the arm census in silence",
		fn.Name.Name, fset.Position(fn.Pos()), types.ExprString(results.List[0].Type))

	switch first.Name {
	case "string":
		return true
	case "bool":
		return false
	}
	require.Fail(t, "unreadable result in the type table",
		"typeMap method %s at %s returns %s first, and this walk knows only `string` (a carrier) and "+
			"`bool` (the storage predicate); a third shape is refused rather than skipped, because "+
			"skipping one would drop a carrier method out of the arm census in silence",
		fn.Name.Name, fset.Position(fn.Pos()), first.Name)
	return false
}

// isTypeMapMethod reports whether a declaration is a method on typeMap.
// Not every one is part of the Go-type table — namesAGoType is what
// separates the carrier methods from StorableProperty's storage
// predicate.
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

// TestUnsignedParamsAboveTheAgtypeRangeAreRefusedAtBind witnesses the
// encode half of the posture the read path already states in its own
// words: agtypeIntAs documents that "a UINT64 property's readable set is
// therefore [0, MaxInt64], since agtype's integer scalar is signed
// 64-bit and a larger value is UNSTORABLE rather than unreadable".
// Unstorable is a claim about this path, and until now nothing here
// enforced it — encodeParam returned every non-temporal bare and
// agtypeArgs is json.Marshal, which writes a uint64 exactly and never
// errors on magnitude. So the too-large value left gqlc intact and what
// the server made of it was the server's business (bd gqlc-tzjqu).
//
// Asserted on the rendered emission rather than on the encoder helper,
// because the requirement is that the METHOD cannot send the value: a
// checked encoder nothing routes through would satisfy a unit test and
// change nothing that reaches a database.
//
// The neo4j backend needs no equivalent — its driver refuses this at the
// packer — and the two facts are not in tension. gqlc has to carry the
// refusal wherever the transport does not, and json.Marshal does not.
// Each row pins the WHOLE bind expression rather than the encoder's
// name, because the four shapes differ only in what is composed around
// the same leaf, and a substring check for "agtypeUnsigned" passes on
// every one of them however badly the composition is assembled. The
// nullable list is the row that makes this concrete: it is the only shape
// whose closure has to spell the inner encoder's return type, which comes
// from a table entry no other assertion here reads, and emptying that
// entry emits `([], error)` — invalid Go that the substring form still
// called a pass (measured, mutation row C1).
func TestUnsignedParamsAboveTheAgtypeRangeAreRefusedAtBind(t *testing.T) {
	for _, tt := range []struct {
		name     string
		goType   string
		nullable bool
		bind     string
	}{
		{"uint64", "uint64", false, "agtypeUnsigned(arg)"},
		{"uint", "uint", false, "agtypeUnsigned(arg)"},
		{"nullable uint64", "uint64", true, "agtypeEncodedNullable(arg, agtypeUnsigned)"},
		{"uint64 list", "[]uint64", false, "agtypeEncodedList(arg, agtypeUnsigned)"},
		{
			"nullable uint64 list", "[]uint64", true,
			"agtypeEncodedNullable(arg, func(in []uint64) ([]int64, error) {\n" +
				"\t\treturn agtypeEncodedList(in, agtypeUnsigned)\n" +
				"\t})",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p := codegen.Query{
				NamedQuery: codegen.NamedQuery{
					Name:        "Q",
					SourceText:  "MATCH (r:Row) WHERE r.u = $u RETURN r.id",
					Cardinality: queryfile.CardinalityExec,
				},
				MethodName: "Q",
				Bare:       "q",
				ParamFields: []codegen.Param{
					{RawName: "u", Field: "U", GoType: tt.goType, Nullable: tt.nullable},
				},
			}
			rendered := string(age.RenderCypherFile("p", []codegen.Query{p}))

			require.NotContains(t, rendered, `map[string]any{"u": arg}`,
				"the parameter crosses bare, so a value above MaxInt64 reaches the server "+
					"as a literal agtype cannot hold")
			require.Contains(t, rendered, "err := "+tt.bind+"\n",
				"the bind does not compose the checked unsigned encoder for this shape")
			require.Contains(t, rendered, `Q: parameter $u:`,
				"the refusal does not name the parameter the author wrote")
		})
	}
}
