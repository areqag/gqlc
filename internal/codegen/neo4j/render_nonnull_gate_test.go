package neo4j_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/codegen"
	"github.com/areqag/gqlc/internal/codegen/neo4j"
	"github.com/areqag/gqlc/internal/graph"
	"github.com/areqag/gqlc/internal/schema"
)

// gateLine is the refusal a non-nullable column that arrived null owes
// its caller, spelled as the emitted source reads it for the query and
// column this sweep renders. Matching the whole line, not the phrase,
// binds the gate to the right method and the right column: a lane that
// names a different column, or refuses in different words, is a failure
// here rather than a silent pass.
const gateLine = `return zeroValue, fmt.Errorf("SweepQuery: column %q is non-nullable but arrived null", "col")`

// nonNullGateCase is one lane of the row-assembly dispatch, reached by a
// codegen.Row that routes there.
//
// lane is a substring unique to the arm the row should reach. Without it
// a row that misroutes would still find some other lane's gate in the
// output and pass, and the sweep would certify a lane it never ran —
// the whole set could collapse onto writeSingleColumnDecodeIndent
// unnoticed.
type nonNullGateCase struct {
	desc     string
	kindName string
	row      codegen.Row
	lane     string
}

// nonNullGateCases covers every arm writeSingleColumnDecodeIndent can
// dispatch to, keyed by the codegen.ColumnKind that reaches it. There
// are more cases than kinds: codegen.ColumnProperty routes to two
// different lanes depending on whether its Go type rides a driver
// carrier, and both owe the gate.
func nonNullGateCases() []nonNullGateCase {
	return []nonNullGateCase{
		{
			desc:     "property riding a driver carrier",
			kindName: "ColumnProperty",
			row:      codegen.Row{ColumnName: "col", Field: "Col", GoType: "string", Kind: codegen.ColumnProperty},
			lane:     `neo4j.GetRecordValue[string](`,
		},
		{
			desc:     "property of no declared shape",
			kindName: "ColumnProperty",
			row:      codegen.Row{ColumnName: "col", Field: "Col", GoType: "any", Kind: codegen.ColumnProperty},
			lane:     `value, ok := rec.Get("col")`,
		},
		{
			desc:     "whole-node projection",
			kindName: "ColumnNode",
			row:      codegen.Row{ColumnName: "col", Field: "Col", GoType: "Person", Kind: codegen.ColumnNode},
			lane:     `neo4j.GetRecordValue[dbtype.Node](`,
		},
		{
			desc:     "whole-edge projection",
			kindName: "ColumnEdge",
			row:      codegen.Row{ColumnName: "col", Field: "Col", GoType: "Knows", Kind: codegen.ColumnEdge},
			lane:     `neo4j.GetRecordValue[dbtype.Relationship](`,
		},
		{
			desc:     "temporal expression",
			kindName: "ColumnTemporal",
			row:      codegen.Row{ColumnName: "col", Field: "Col", GoType: "time.Time", Kind: codegen.ColumnTemporal},
			lane:     `neo4j.GetRecordValue[time.Time](`,
		},
		{
			desc:     "scalar expression",
			kindName: "ColumnScalar",
			row:      codegen.Row{ColumnName: "col", Field: "Col", GoType: "int64", Kind: codegen.ColumnScalar},
			lane:     `neo4j.GetRecordValue[int64](`,
		},
		{
			desc:     "null scalar",
			kindName: "ColumnScalarNull",
			row:      codegen.Row{ColumnName: "col", Field: "Col", GoType: "any", Kind: codegen.ColumnScalarNull},
			lane:     `value, ok := rec.Get("col")`,
		},
		{
			desc:     "list column",
			kindName: "ColumnList",
			row: codegen.Row{
				ColumnName: "col", Field: "Col", GoType: "[]string", Kind: codegen.ColumnList,
				ListElem: &codegen.ListElem{Kind: codegen.ColumnScalar, GoType: "string"},
			},
			lane: `neo4j.GetRecordValue[[]any](`,
		},
		{
			desc:     "honest-any column",
			kindName: "ColumnAny",
			row:      codegen.Row{ColumnName: "col", Field: "Col", GoType: "any", Kind: codegen.ColumnAny},
			lane:     `value, ok := rec.Get("col")`,
		},
		{
			desc:     "multi-candidate edge column",
			kindName: "ColumnEdgeUnion",
			row: codegen.Row{
				ColumnName: "col", Field: "Col", GoType: "SweepQueryCol", Kind: codegen.ColumnEdgeUnion,
				EdgeKeys: []schema.EdgeKey{{KeyLabels: graph.LabelSetKey("KNOWS")}},
			},
			lane: `raw, ok := rec.Get("col")`,
		},
	}
}

// sweepQuery is the one-column query the cases render against. The
// EdgeUnions entry is inert for every case but the edgeUnion one, which
// resolves its candidate struct name through it.
func sweepQuery(f codegen.Row) codegen.Query {
	return codegen.Query{
		MethodName: "SweepQuery",
		RowFields:  []codegen.Row{f},
		EdgeUnions: []*codegen.EdgeUnion{{
			QueryName:     "SweepQuery",
			ColumnName:    "col",
			FieldName:     "Col",
			InterfaceName: "SweepQueryCol",
			EdgeKeys:      []schema.EdgeKey{{KeyLabels: graph.LabelSetKey("KNOWS")}},
			Candidates:    []string{"Knows"},
		}},
	}
}

// renderColumn runs the row-assembly dispatch for one column and returns
// the emitted Go source.
func renderColumn(f codegen.Row) string {
	var b strings.Builder
	neo4j.WriteSingleColumnDecodeIndent(&b, sweepQuery(f), f, "rec", "zeroValue", "\tout.Col = ", "\n", "\t")
	return b.String()
}

// TestEveryColumnKindGatesArrivedNull is the structural half of bd
// gqlc-tez0: every codegen.ColumnKind's neo4j decode lane must refuse a
// non-nullable column that arrived null, and none may refuse a nullable
// one — a nullable column carries the graph's null in its pointer.
//
// The behavioural live row pins ANY VALUE alone. This is what stops the
// next lane from skipping the gate the way writeAnyColumnDecodeIndent
// did: `any` was the width where the omission was observable, but the
// omission itself was a lane's, not a width's.
func TestEveryColumnKindGatesArrivedNull(t *testing.T) {
	for _, c := range nonNullGateCases() {
		t.Run(c.kindName+"/"+c.desc, func(t *testing.T) {
			f := c.row
			f.Nullable = false
			nonNullable := renderColumn(f)
			require.Contains(t, nonNullable, c.lane,
				"row did not reach the lane this case names; the case is testing some other arm's gate")
			require.Contains(t, nonNullable, gateLine,
				"a non-nullable column's lane must refuse a value that arrived null")

			f.Nullable = true
			nullable := renderColumn(f)
			require.Contains(t, nullable, c.lane,
				"nullable row did not reach the lane this case names")
			require.NotContains(t, nullable, gateLine,
				"a nullable column carries the graph's null in its pointer; refusing it would make the declared nullability unreachable")
		})
	}
}

// TestColumnKindSweepIsExhaustive pins the sweep's coverage against the
// enum as declared, not against a number copied from it. A kind added to
// codegen without a case here fails this test, and a sweep whose table
// emptied out or whose parse found nothing fails it too — a count that
// derives from the same table it is checking would certify nothing.
func TestColumnKindSweepIsExhaustive(t *testing.T) {
	declared := declaredColumnKinds(t)
	require.NotEmpty(t, declared,
		"parse found no ColumnKind constants; a coverage check derived from an empty enum passes vacuously")

	covered := map[string]bool{}
	for _, c := range nonNullGateCases() {
		covered[c.kindName] = true
	}
	var coveredNames []string
	for k := range covered {
		coveredNames = append(coveredNames, k)
	}
	require.ElementsMatch(t, declared, coveredNames,
		"every codegen.ColumnKind needs a case in nonNullGateCases; a new kind owes the arrived-null gate like every other")
}

// declaredColumnKinds reads the codegen package's ColumnKind constant
// names off the AST. Reading the declarations rather than the file's
// bytes means a commented-out constant is not a kind, and an iota block
// is counted by its specs rather than by a maintained tally.
func declaredColumnKinds(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir("..")
	require.NoError(t, err)

	var names []string
	sawFile := false
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		path := filepath.Join("..", e.Name())
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		require.NoError(t, err, "parse %s", path)
		sawFile = true
		for _, d := range file.Decls {
			gd, ok := d.(*ast.GenDecl)
			if !ok || gd.Tok != token.CONST {
				continue
			}
			// An iota block names its type on the first spec only; later
			// specs inherit it, so the type carries down the block.
			specType := ""
			for _, s := range gd.Specs {
				vs, ok := s.(*ast.ValueSpec)
				if !ok {
					continue
				}
				if vs.Type != nil {
					specType = ""
					if id, ok := vs.Type.(*ast.Ident); ok {
						specType = id.Name
					}
				}
				if specType != "ColumnKind" {
					continue
				}
				for _, n := range vs.Names {
					names = append(names, n.Name)
				}
			}
		}
	}
	require.True(t, sawFile,
		"walk of .. encountered no non-test .go file; the enum's declaration site must be readable from here")
	return names
}
