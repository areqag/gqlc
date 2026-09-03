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
//
// The last row is the one that is not merely a fifth copy. A list column
// comes from two different sites, and they differ in a way no other
// fixture here would show: a list PROPERTY (prepare.go's ResolvedProperty
// arm) sets the row's own Width to the whole list type, so a walk that
// only ever looked at Width would still reach the element by stripping
// the list levels off it — while a list the QUERY builds, collect(...)
// over a record-valued property, leaves the row's Width empty and hands
// the element type to ListElem alone. Only that second shape can witness
// the ListElem descent: measured 2026-09-02, deleting the descent from
// conversionUses changed no test outcome anywhere in internal/codegen
// until this row existed.
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
	collectRec := graph.RecordOf([]graph.RecordField{
		{Name: "seen", Type: graph.TypeBool, NotNull: true},
	})

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
				{
					// No Width, deliberately: this is the collect(...)
					// shape, and prepare.go leaves the row's own width
					// empty there. Stripping list levels off it reaches
					// nothing, so only the ListElem descent finds this
					// record.
					ColumnName: "seenAll", Field: "SeenAll",
					GoType:   "[]" + recordText(t, collectRec),
					Kind:     codegen.ColumnList,
					ListElem: &codegen.ListElem{GoType: recordText(t, collectRec), Width: collectRec},
				},
			},
		}},
	}
	return prepared, []graph.PropertyType{inner, entityRec, paramRec, rowRec, elemRec, collectRec}
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
//
// Every row keys on a helper's PARAMETER, never on its name. The four
// encode helpers are named by extending the plain one — encodeRecord<h>,
// then ...Ptr, ...List, ...ListPtr — so "func encodeRecord" is a prefix
// of all four and is satisfied by any one of them. Asserting it would
// have made this test blind to the very gate it guards: dropping the
// encodePtr and list disjuncts, so that a Ptr wrapper is emitted calling
// a plain helper nothing declares, left every row green (measured
// 2026-09-02). The parameter distinguishes them — record, *record,
// []record, *[]record are four different texts with no prefix among
// them.
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
			absent:  []string{"(v record", "Ptr(v *record", "List(v []record"},
			wantFmt: true,
		},
		{
			name:    "encode only",
			use:     neo4j.CarrierUseFlags{Encode: true},
			present: []string{"(v record"},
			absent:  []string{"func decodeRecord", "Ptr(v *record", "List(v []record"},
			wantFmt: false,
		},
		{
			name: "nullable parameter only",
			use:  neo4j.CarrierUseFlags{EncodePtr: true},
			// The plain encode stands under the Ptr wrapper, which calls
			// it — so EncodePtr alone owes BOTH, and a gate that emitted
			// only the wrapper would name an undeclared function.
			present: []string{"(v record", "Ptr(v *record"},
			absent:  []string{"func decodeRecord", "List(v []record"},
			wantFmt: false,
		},
		{
			name:    "list parameter only",
			use:     neo4j.CarrierUseFlags{List: true},
			present: []string{"(v record", "List(v []record"},
			absent:  []string{"func decodeRecord", "ListPtr(v *[]record"},
			wantFmt: false,
		},
		{
			name:    "nullable list parameter",
			use:     neo4j.CarrierUseFlags{Encode: true, List: true, ListPtr: true},
			present: []string{"(v record", "List(v []record", "ListPtr(v *[]record"},
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
	// Read off the declarations below rather than restated, so a field
	// added to the fixture is a field the format is held against.
	var fieldNames []string
	record := func(fields ...graph.RecordField) graph.PropertyType {
		for _, f := range fields {
			fieldNames = append(fieldNames, f.Name)
		}
		return graph.RecordOf(fields)
	}

	inner := record(graph.RecordField{Name: "zip", Type: graph.TypeInt32, NotNull: true})
	// Every arm of writeRecordValueDecode at once: a nested record, a
	// list (which is the arm that carries a depth), a narrowed width, a
	// temporal carrier, an ANY leaf and a nullable field.
	pt := record(
		graph.RecordField{Name: "addr", Type: inner, NotNull: true},
		graph.RecordField{Name: "born", Type: graph.TypeDate, NotNull: true},
		graph.RecordField{Name: "misc", Type: graph.TypeAnyPropertyValue},
		graph.RecordField{Name: "rank", Type: graph.TypeInt32, NotNull: true},
		graph.RecordField{Name: "tags", Type: graph.ListOf(graph.TypeString, true), NotNull: true},
		graph.RecordField{Name: "nick", Type: graph.TypeString},
	)
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
		// The balance above cannot witness the thing recordFail's doc
		// actually claims. Pasting an author-derived half into the format
		// removes a verb and its argument together, so the count still
		// agrees — measured 2026-09-02: rendering the field name into the
		// format instead of passing it left this test green. What the
		// paste costs is the escaping: a field named pct%s becomes a verb
		// the call has no value for, which `go vet` of the emitted package
		// fails on. So the format is held to naming neither half.
		for _, half := range append(fieldNames, "RECORD") {
			require.NotContains(t, format, half,
				"%s: %q is author-derived and must reach the message as an ARGUMENT, not inside the format",
				fset.Position(call.Pos()), half)
		}
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
