package neo4j_test

import (
	"go/parser"
	"go/token"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/codegen"
	"github.com/areqag/gqlc/internal/codegen/neo4j"
	"github.com/areqag/gqlc/internal/queryfile"
)

// querierImportCase is one batch shape and the import set its emitted
// querier.go must declare.
type querierImportCase struct {
	desc     string
	prepared []codegen.Query
	want     []string
}

// querierQuery builds one prepared query with a chosen cardinality and a
// chosen single-column return type. Everything else is inert: the import
// block reads a method's SIGNATURE, and a signature is the cardinality, the
// parameter type and the row type.
func querierQuery(name string, card queryfile.Cardinality, rowType, paramType string) codegen.Query {
	q := codegen.Query{
		NamedQuery: codegen.NamedQuery{Name: name, Cardinality: card},
		MethodName: name,
		Bare:       strings.ToLower(name[:1]) + name[1:],
		RowFields:  []codegen.Row{{ColumnName: "c", Field: "C", GoType: rowType, Kind: codegen.ColumnProperty}},
	}
	if paramType != "" {
		q.ParamFields = []codegen.Param{{RawName: "p", Field: "P", GoType: paramType}}
	}
	return q
}

// querierImportCases is the table. Every subset of the three carriers that a
// batch can reach is present, because composition is where this went wrong:
// the AGE side emitted its import block from a switch over combinations, and
// a switch needs a case per subset — eight for three carriers — so a new
// carrier silently dropped an old one from the batches that named both.
//
// The single-carrier rows are not redundant with the combined ones. A block
// built by concatenation can be correct for every pair and still emit the
// carriers in an order gofmt would reject, and only a row that names one
// carrier alone distinguishes "not emitted" from "emitted in the wrong
// group".
func querierImportCases() []querierImportCase {
	return []querierImportCase{
		{
			desc: "no queries at all emits no import block",
			want: []string{},
		},
		{
			desc:     "context alone",
			prepared: []codegen.Query{querierQuery("Get", queryfile.CardinalityOne, "string", "")},
			want:     []string{"context"},
		},
		{
			desc:     "iter alone",
			prepared: []codegen.Query{querierQuery("Stream", queryfile.CardinalityIter, "string", "")},
			want:     []string{"context", "iter"},
		},
		{
			desc:     "time alone",
			prepared: []codegen.Query{querierQuery("Get", queryfile.CardinalityOne, "time.Time", "")},
			want:     []string{"context", "time"},
		},
		{
			desc:     "dbtype alone",
			prepared: []codegen.Query{querierQuery("Get", queryfile.CardinalityOne, "dbtype.Point2D", "")},
			want:     []string{"context", "dbtype"},
		},
		{
			desc:     "iter and time on one method",
			prepared: []codegen.Query{querierQuery("Stream", queryfile.CardinalityIter, "time.Time", "")},
			want:     []string{"context", "iter", "time"},
		},
		{
			desc: "iter and time on different methods",
			prepared: []codegen.Query{
				querierQuery("Stream", queryfile.CardinalityIter, "string", ""),
				querierQuery("Get", queryfile.CardinalityOne, "time.Time", ""),
			},
			want: []string{"context", "iter", "time"},
		},
		{
			desc:     "iter and dbtype",
			prepared: []codegen.Query{querierQuery("Stream", queryfile.CardinalityIter, "dbtype.Point2D", "")},
			want:     []string{"context", "iter", "dbtype"},
		},
		{
			desc: "all three",
			prepared: []codegen.Query{
				querierQuery("Stream", queryfile.CardinalityIter, "dbtype.Point2D", ""),
				querierQuery("Get", queryfile.CardinalityOne, "time.Time", ""),
			},
			want: []string{"context", "iter", "time", "dbtype"},
		},
		{
			desc: "a carrier reached only through a lone parameter",
			prepared: []codegen.Query{
				querierQuery("Stream", queryfile.CardinalityIter, "string", "time.Time"),
			},
			want: []string{"context", "iter", "time"},
		},
	}
}

// TestQuerierImportsMatchTheSignatures is the guard for the artefact whose
// absence is invisible until a golden fails to compile.
//
// querier.go declares interfaces and nothing else, so nothing in this
// package's own tests executes it and no assertion about its behaviour can
// exist. It is checked by the Go compiler, and only when someone builds a
// generated package — which is the conformance corpus, one layer out, and
// only for the shapes the corpus happens to carry. A carrier combination no
// fixture reaches is unguarded until a user hits it.
//
// Both directions are failures and both are silent here. A missing import
// emits a file naming a package it never imported; a spurious one emits an
// unused import. Neither is visible in the bytes unless something compiles
// them.
//
// The two assertions are deliberately different in kind. The first is the
// table's — a shape and the import set its author expects — and it catches a
// renderer that changed its mind. The second derives the expectation from
// the emitted bytes themselves, so it holds for a shape the table's author
// never imagined, which is the failure mode the table cannot cover.
func TestQuerierImportsMatchTheSignatures(t *testing.T) {
	for _, c := range querierImportCases() {
		t.Run(c.desc, func(t *testing.T) {
			src := string(neo4j.RenderQuerier("q", c.prepared))

			declared := declaredImports(t, src)
			sort.Strings(declared)
			// A fresh slice rather than append onto nil: appending nothing
			// onto nil yields nil, and require.Equal separates nil from
			// empty, so the zero-query row would fail on its own expectation.
			want := make([]string, len(c.want))
			copy(want, c.want)
			sort.Strings(want)
			require.Equal(t, want, declared,
				"querier.go declared a different import set than this shape's signatures call for")

			require.Equal(t, declared, usedQualifiers(t, src),
				"querier.go's declared imports and the qualifiers its interface bodies actually name disagree; one direction emits a file naming an unimported package, the other an unused import, and both fail at go build in the GENERATED package rather than here")
		})
	}
}

// declaredImports is the set of package names querier.go imports, taken from
// the parsed file rather than from the text, so the grouping and blank lines
// the renderer writes cannot change the answer. The name is the last path
// element, which is what a qualifier in the body spells.
//
// Parsing is also the cheapest available check that the emitted file is
// syntactically Go at all — a renderer that emitted a malformed import block
// would otherwise be reported as a set mismatch, which points at the wrong
// thing.
func declaredImports(t *testing.T, src string) []string {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), "querier.go", src, parser.ImportsOnly)
	require.NoError(t, err, "emitted querier.go does not parse")
	out := []string{}
	for _, spec := range f.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		require.NoError(t, err)
		out = append(out, path[strings.LastIndex(path, "/")+1:])
	}
	sort.Strings(out)
	return out
}

// qualifierRe finds a `pkg.Ident` qualifier. It is deliberately applied to
// the body BELOW the import block: the import lines spell the package name
// too, and matching those would make every declared import vouch for itself.
var qualifierRe = regexp.MustCompile(`\b([a-z][a-z0-9]*)\.[A-Z]`)

// usedQualifiers is the set of package qualifiers the interface declarations
// name. `context` is added unconditionally when the body names any method,
// because every method signature spells `ctx context.Context` and the regex
// above finds it there like any other.
func usedQualifiers(t *testing.T, src string) []string {
	t.Helper()
	body := src
	if i := strings.Index(src, "type ReadQuerier"); i >= 0 {
		body = src[i:]
	}
	seen := map[string]bool{}
	for _, m := range qualifierRe.FindAllStringSubmatch(body, -1) {
		seen[m[1]] = true
	}
	out := []string{}
	for name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
