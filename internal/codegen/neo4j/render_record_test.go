package neo4j_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/codegen"
	"github.com/areqag/gqlc/internal/codegen/neo4j"
	"github.com/areqag/gqlc/internal/graph"
)

// recordText is the emitted carrier text for a width, which every
// Prepared field below has to carry alongside its Width: the use walk
// reads the TEXT to decide whether a leaf is a carrier, and the encoding
// set reads the WIDTH. Deriving the text from the width here is what
// stops a test from feeding the two halves inputs that disagree — which
// would let a real disagreement between them pass.
func recordText(t *testing.T, pt graph.PropertyType) string {
	t.Helper()
	text, ok := neo4j.TypeMap{}.Property(pt)
	require.True(t, ok, "this backend carries %s", pt)
	return text
}

// richPrepared is a batch that reaches records through every position
// either walk looks at, so the two can be held against each other on
// more than one shape: an entity field, a query parameter, a row field,
// and a row's list element. Each position carries a DIFFERENT record, so
// a walk that missed one position would come up short rather than be
// covered by another position's answer.
func richPrepared(t *testing.T) (codegen.Prepared, []graph.PropertyType) {
	t.Helper()
	inner := graph.RecordOf([]graph.RecordField{
		{Name: "zip", Type: graph.TypeInt32, NotNull: true},
	})
	entityRec := graph.RecordOf([]graph.RecordField{
		{Name: "addr", Type: inner, NotNull: true},
		{Name: "born", Type: graph.TypeDate, NotNull: true},
	})
	paramRec := graph.RecordOf([]graph.RecordField{
		{Name: "tag", Type: graph.TypeString},
	})
	rowRec := graph.RecordOf([]graph.RecordField{
		{Name: "score", Type: graph.TypeFloat32, NotNull: true},
	})
	elemRec := graph.RecordOf([]graph.RecordField{
		{Name: "note", Type: graph.TypeString, NotNull: true},
	})
	elemList := graph.ListOf(elemRec, true)

	prepared := codegen.Prepared{
		Entities: []codegen.Entity{{Name: "Person", Fields: []codegen.EntityField{
			{PropName: "home", Field: "Home", GoType: recordText(t, entityRec), Width: entityRec},
		}}},
		Queries: []codegen.Query{{
			ParamFields: []codegen.Param{
				{RawName: "p", Field: "P", GoType: recordText(t, paramRec), Width: paramRec},
			},
			RowFields: []codegen.Row{
				{ColumnName: "r", Field: "R", GoType: recordText(t, rowRec), Width: rowRec},
				{
					ColumnName: "notes", Field: "Notes",
					GoType:   recordText(t, elemList),
					Width:    elemList,
					Kind:     codegen.ColumnList,
					ListElem: &codegen.ListElem{GoType: recordText(t, elemRec), Width: elemRec},
				},
			},
		}},
	}
	return prepared, []graph.PropertyType{inner, entityRec, paramRec, rowRec, elemRec}
}

// TestRecordUseAnswersForExactlyTheSharedEncodingSet binds the two halves
// of the record emission to each other. codegen.RecordEncodings decides
// WHICH encodings the batch reaches — it is shared, so both backends emit
// one helper pair per entry — and this backend's conversionUses decides
// WHICH DIRECTIONS each of them is reached in. They are separate walks
// over the same batch, and they can only disagree in one of two ways,
// both of which reach the author as a build failure of GENERATED code
// with no line in their schema to point at:
//
//   - an encoding in the set that the use walk never marked emits an
//     alias and no helper, so a decode site names decodeRecord<hash> and
//     nothing declares it;
//   - an encoding the use walk marked that is not in the set is never
//     emitted at all, the same failure one file over.
//
// Equality rather than subset in either direction, because both failures
// are real and a one-sided assertion would witness only one of them.
func TestRecordUseAnswersForExactlyTheSharedEncodingSet(t *testing.T) {
	prepared, want := richPrepared(t)

	shared := codegen.RecordEncodings(prepared.Entities, prepared.Queries)
	require.ElementsMatch(t, want, shared,
		"the premise: the fixture really does reach all five records, so neither walk can be right by reaching none")

	require.Equal(t, shared, neo4j.RecordUseEncodings(prepared),
		"conversionUses must answer for exactly the encodings RecordEncodings names — both are sorted, so this compares order too")
}

// TestRecordHelpersAreEmittedOnlyWhereCalled pins the gating. An
// unexported function nothing calls fails the emitted package's own lint
// fence, so a helper emitted for a direction the batch never reaches is
// not a harmless spare — it is a red fixture. The reverse is the build
// failure the invariant above describes.
//
// The imports are asserted with the same rows because they are gated on
// the same reading: an encode-only file names fmt nowhere, since only a
// decode reports a failure through it.
func TestRecordHelpersAreEmittedOnlyWhereCalled(t *testing.T) {
	pt := graph.RecordOf([]graph.RecordField{
		{Name: "tag", Type: graph.TypeString, NotNull: true},
	})
	all := []graph.PropertyType{pt}

	for _, tc := range []struct {
		name    string
		use     neo4j.CarrierUseFlags
		present []string
		absent  []string
		wantFmt bool
	}{
		{
			name:    "decode only",
			use:     neo4j.CarrierUseFlags{Decode: true},
			present: []string{"func decodeRecord"},
			absent:  []string{"func encodeRecord"},
			wantFmt: true,
		},
		{
			name:    "encode only",
			use:     neo4j.CarrierUseFlags{Encode: true},
			present: []string{"func encodeRecord"},
			absent:  []string{"func decodeRecord", "func encodeRecord" + "d0d0d0Ptr"},
			wantFmt: false,
		},
		{
			name: "nullable parameter only",
			use:  neo4j.CarrierUseFlags{EncodePtr: true},
			// The plain encode stands under the Ptr wrapper, which calls
			// it — so EncodePtr alone owes BOTH, and a gate that emitted
			// only the wrapper would name an undeclared function.
			present: []string{"func encodeRecord", "Ptr(v *record"},
			absent:  []string{"func decodeRecord", "List(v []record"},
			wantFmt: false,
		},
		{
			name:    "list parameter only",
			use:     neo4j.CarrierUseFlags{List: true},
			present: []string{"func encodeRecord", "List(v []record"},
			absent:  []string{"func decodeRecord", "ListPtr(v *[]record"},
			wantFmt: false,
		},
		{
			name:    "nullable list parameter",
			use:     neo4j.CarrierUseFlags{Encode: true, List: true, ListPtr: true},
			present: []string{"List(v []record", "ListPtr(v *[]record"},
			absent:  []string{"func decodeRecord"},
			wantFmt: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := string(neo4j.RenderRecordHelpers("db", all,
				map[graph.PropertyType]neo4j.CarrierUseFlags{pt: tc.use}))

			require.Contains(t, out, "type record",
				"the alias is owed whichever direction is reached — every signature names it")
			for _, want := range tc.present {
				require.Contains(t, out, want)
			}
			for _, unwanted := range tc.absent {
				require.NotContains(t, out, unwanted)
			}
			require.Equal(t, tc.wantFmt, strings.Contains(out, `"fmt"`),
				"fmt is owed by a decode's failure reporting and by nothing else; an import nothing names does not compile")
		})
	}
}

// TestRecordHelperErrorsBalanceTheirVerbs walks the emitted decode
// failures as an AST and holds each fmt.Errorf's verb count against its
// argument count.
//
// It is the guard on recordFail's shape rather than on its wording. The
// two author-derived halves — the record's canonical encoding and the
// declared field name — are passed as ARGUMENTS precisely so that a name
// carrying a '%' cannot become a verb the call has no argument for, which
// `go vet` of the generated package fails on. recordFieldLegality now
// refuses such a name upstream, so this is the second of two locks; it is
// kept because the two answer to different things, and the one that would
// notice a NEW failure wording growing a verb without its value is this
// one.
//
// The check reads the AST rather than grepping the text, because a
// declared field name is author text and appears inside these very calls
// — a substring sweep would be reading the schema's spelling back to
// itself.
func TestRecordHelperErrorsBalanceTheirVerbs(t *testing.T) {
	inner := graph.RecordOf([]graph.RecordField{
		{Name: "zip", Type: graph.TypeInt32, NotNull: true},
	})
	// Every arm of writeRecordValueDecode at once: a nested record, a
	// list (which is the arm that carries a depth), a narrowed width, a
	// temporal carrier, an ANY leaf and a nullable field.
	pt := graph.RecordOf([]graph.RecordField{
		{Name: "addr", Type: inner, NotNull: true},
		{Name: "born", Type: graph.TypeDate, NotNull: true},
		{Name: "misc", Type: graph.TypeAnyPropertyValue},
		{Name: "rank", Type: graph.TypeInt32, NotNull: true},
		{Name: "tags", Type: graph.ListOf(graph.TypeString, true), NotNull: true},
		{Name: "nick", Type: graph.TypeString},
	})
	use := neo4j.CarrierUseFlags{Decode: true, Encode: true}
	out := neo4j.RenderRecordHelpers("db", []graph.PropertyType{pt, inner},
		map[graph.PropertyType]neo4j.CarrierUseFlags{pt: use, inner: use})

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "record_neo4j.go", out, 0)
	require.NoError(t, err, "the emitted file must parse before anything can be read out of it")

	calls := 0
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Errorf" {
			return true
		}
		require.NotEmpty(t, call.Args)
		lit, ok := call.Args[0].(*ast.BasicLit)
		require.True(t, ok, "the format must be a literal this test can read, not a computed expression")
		format, uerr := strconv.Unquote(lit.Value)
		require.NoError(t, uerr)
		calls++
		require.Equal(t, countVerbs(format), len(call.Args)-1,
			"%s: format %q takes %d verbs and the call supplies %d arguments",
			fset.Position(call.Pos()), format, countVerbs(format), len(call.Args)-1)
		return true
	})
	require.Positive(t, calls,
		"a walk that found no fmt.Errorf would pass vacuously; this fixture decodes six fields and must reach several")
}

// countVerbs counts the format verbs in a Printf format, which for these
// emissions is every '%' not doubled. Deliberately not a general parser:
// recordFail's formats use %s, %q, %T and %w and nothing with a width or
// a flag, so a count of undoubled '%' is exact here and a format that
// grew a flag would be a change this test should be re-read for.
func countVerbs(format string) int {
	n := 0
	for i := 0; i < len(format); i++ {
		if format[i] != '%' {
			continue
		}
		if i+1 < len(format) && format[i+1] == '%' {
			i++
			continue
		}
		n++
	}
	return n
}

// TestRecordAliasNameIsTheHelperSuffixUnexported holds the carrier's name
// to its helpers'. They must come from one digest: the alias is what
// every helper signature names, so a second hash here would emit
// encodeRecord<a> taking a record<b> that nothing declares.
func TestRecordAliasNameIsTheHelperSuffixUnexported(t *testing.T) {
	for _, pt := range []graph.PropertyType{
		graph.RecordOf(nil),
		graph.RecordOf([]graph.RecordField{{Name: "a", Type: graph.TypeInt32}}),
		graph.RecordOf([]graph.RecordField{{Name: "b", Type: graph.TypeString, NotNull: true}}),
	} {
		suffix := codegen.RecordHelperSuffix(pt)
		alias := neo4j.RecordAliasName(pt)
		require.Equal(t, strings.ToLower(suffix[:1])+suffix[1:], alias)
		require.NotEqual(t, suffix, alias,
			"the alias must be unexported: it is an implementation detail of the emitted package, not part of its surface")
		require.Equal(t, strings.ToLower(suffix), strings.ToLower(alias),
			"one digest, differing only in the first byte's case")
	}
}
