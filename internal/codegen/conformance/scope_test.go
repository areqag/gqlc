package conformance_test

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/cli/backends"
	"github.com/areqag/gqlc/internal/codegen"
	"github.com/areqag/gqlc/internal/procsig"
	"github.com/areqag/gqlc/internal/query/cypher"
	"github.com/areqag/gqlc/internal/queryfile"
	"github.com/areqag/gqlc/internal/resolver"
	"github.com/areqag/gqlc/internal/schema"
	"github.com/areqag/gqlc/internal/schema/gql"
)

// scopeProbeSchema is the smallest graph type that reaches every body an
// emitted query method has: a scalar property to project bare, a second
// one to widen a row struct, and the node itself to project as an entity.
const scopeProbeSchema = `CREATE PROPERTY GRAPH TYPE ScopeProbe AS {
    (:Person {
        name :: STRING NOT NULL,
        age  :: INT64 NOT NULL
    })
}`

// scopeProbeQueries is one single-parameter query per emitted body shape:
// a bare column at each read cardinality, a multi-column row, an entity
// column, and a write. %[1]s is the parameter name under test — every
// query binds exactly one parameter, because the single-parameter form is
// the only one whose Go argument was ever derived from the query text.
const scopeProbeQueries = `// name: ProbeOne :one
MATCH (p:Person) WHERE p.name = $%[1]s RETURN p.name

// name: ProbeMany :many
MATCH (p:Person) WHERE p.name = $%[1]s RETURN p.name

// name: ProbeColumns :many
MATCH (p:Person) WHERE p.name = $%[1]s RETURN p.name, p.age

// name: ProbeEntity :one
MATCH (p:Person) WHERE p.name = $%[1]s RETURN p

// name: ProbeExec :exec
MATCH (p:Person) WHERE p.name = $%[1]s DELETE p
`

// unclaimedParam is a parameter name no emission mentions, so the scopes
// read off an emission spelled with it are the reference every other
// spelling is measured against.
const unclaimedParam = "alpha"

// TestEmittedScopeIsGeneratorOwned pins the whole of what a query author
// can put into an emitted method's scope, which is nothing.
//
// A generated query method carries exactly one identifier the author
// chose: the single-parameter form used to name its Go argument after the
// $param in the Cypher text. That name landed in the same scope as the
// receiver, the context argument, every local the body declares and every
// package-level name the body resolves — so $q, $ctx, $err, $records,
// $fmt and $agtypeArgs each emitted a package that does not compile, and
// $_ emitted an empty identifier that gofmt rejected. Generation exited 0
// over all but the last, because the format gate parses the emission and
// does not type-check it.
//
// The assertion is scope EQUALITY rather than disjointness against a list
// of reserved names. A list has to be kept in sync with every future
// change to the emitted body — which is exactly the failure that produced
// this defect — and the true surface is unbounded anyway, since it
// includes every import and every decode<Entity> helper the user's schema
// happens to generate. Equality needs to know none of that: it reads the
// bound names off one emission and requires every other emission to bind
// the same ones, so an emitter change that let an author-chosen name back
// in fails here whatever the emitter chose to call anything.
//
// The candidate names are likewise read off an emission rather than
// listed, and the sweep runs per target, so each backend is probed with
// its own vocabulary. Nothing here reads the golden corpus: -update
// cannot make this test pass.
func TestEmittedScopeIsGeneratorOwned(t *testing.T) {
	probe := newScopeProbe(t)
	for _, target := range probe.targets {
		t.Run(target, func(t *testing.T) {
			reference := probe.emit(t, target, unclaimedParam)
			want := boundScopes(t, reference)
			require.NotEmpty(t, want, "the emission binds no identifiers to compare")

			candidates := mentionedIdents(t, reference)
			require.NotEmpty(t, candidates, "the emission mentions no identifiers to probe")

			for _, name := range candidates {
				t.Run(name, func(t *testing.T) {
					in, err := probe.input(name)
					if err != nil {
						// A name the query grammar cannot spell after a
						// dollar sign is one no author can reach.
						t.Skipf("no query parameter can be named %q: %v", name, err)
					}
					files := probe.generate(t, target, in)
					requireParameterReachesTheWire(t, files, name)
					require.Equal(t, want, boundScopes(t, files),
						"a query parameter named %q changed what the emission binds", name)
				})
			}
		})
	}
}

// scopeProbe emits the probe batch through every registered backend. The
// schema is parsed once and the resolver built once: only the query text
// changes between emissions, and re-parsing a graph type per candidate
// would dominate the sweep.
type scopeProbe struct {
	targets []string
	lookup  func(string) (func(pkg string) codegen.Generator, bool)
	schema  schema.Schema
	res     *resolver.Resolver
}

func newScopeProbe(t *testing.T) *scopeProbe {
	t.Helper()
	reg, err := backends.Registry()
	require.NoError(t, err)
	targets := reg.Keys()
	require.NotEmpty(t, targets, "no backend is registered, so this test holds nothing")

	sch, err := gql.New().Parse(strings.NewReader(scopeProbeSchema))
	require.NoError(t, err)
	procs, err := procsig.NewRegistry(nil)
	require.NoError(t, err)

	return &scopeProbe{
		targets: targets,
		lookup:  reg.Lookup,
		schema:  sch,
		res:     resolver.New(sch, resolver.WithRegistry(procs)),
	}
}

// input lowers the probe batch spelled around one parameter name. The
// error is the front end's alone — a name the query grammar rejects is
// not this test's business — so a caller skips on it rather than failing.
func (p *scopeProbe) input(param string) (codegen.Input, error) {
	src := fmt.Sprintf(scopeProbeQueries, param)
	annotated, err := queryfile.New().Parse(strings.NewReader(src))
	if err != nil {
		return codegen.Input{}, err
	}
	procs, err := procsig.NewRegistry(nil)
	if err != nil {
		return codegen.Input{}, err
	}
	queries := make([]codegen.NamedQuery, 0, len(annotated))
	for _, aq := range annotated {
		q, err := cypher.New(cypher.WithRegistry(procs)).Parse(bytes.NewReader([]byte(aq.Text)))
		if err != nil {
			return codegen.Input{}, err
		}
		vq, err := p.res.Resolve(q)
		if err != nil {
			return codegen.Input{}, err
		}
		queries = append(queries, codegen.NamedQuery{
			Name:        aq.Name,
			Cardinality: aq.Cardinality,
			SourceFile:  "queries.cypher",
			SourceText:  aq.Text,
			Validated:   vq,
		})
	}
	return codegen.Input{Schema: p.schema, Queries: queries}, nil
}

// emit is input plus generate for a parameter name the batch must accept.
func (p *scopeProbe) emit(t *testing.T, target, param string) []codegen.File {
	t.Helper()
	in, err := p.input(param)
	require.NoError(t, err, "the probe batch does not lower under parameter %q", param)
	return p.generate(t, target, in)
}

// generate runs the batch through one backend. A generation that failed
// is this test's business and fails it: $_ used to reach gofmt as an
// empty parameter name, and the report named a column in querier.go
// rather than the parameter that caused it.
func (p *scopeProbe) generate(t *testing.T, target string, in codegen.Input) []codegen.File {
	t.Helper()
	newGen, ok := p.lookup(target)
	require.True(t, ok, "no backend is registered under %q", target)
	files, err := newGen("probe").Generate(in)
	require.NoError(t, err, "generation failed for target %q", target)
	require.NotEmpty(t, files, "target %q emitted nothing", target)
	return files
}

// requireParameterReachesTheWire holds the half of the parameter name
// that must NOT become generator-owned. The Go argument is positional and
// so free to be renamed, but the name the query text wrote is what the
// driver substitutes, and a fix that stopped emitting it would pass every
// scope comparison in this file while silently unbinding the query.
func requireParameterReachesTheWire(t *testing.T, files []codegen.File, param string) {
	t.Helper()
	quoted := strconv.Quote(param)
	for _, f := range files {
		if bytes.Contains(f.Contents, []byte(quoted)) {
			return
		}
	}
	require.Failf(t, "parameter dropped",
		"no emitted file binds %s, so the query parameter no longer reaches the driver", quoted)
}

// boundScopes is every identifier the emission binds, keyed by the
// declaration that binds it. Function declarations contribute their
// receiver, arguments, results and every local their body declares;
// interface methods contribute their signature, which is where an empty
// argument name reached gofmt. Struct fields are deliberately absent: a
// *Params or *Row field is exported and reached qualified, so it is
// structurally uncapturable and keeps deriving from the query text.
//
// Blank is dropped: `_` binds nothing, and an emitted `_, err := ...` is
// not a name anything can resolve.
func boundScopes(t *testing.T, files []codegen.File) map[string][]string {
	t.Helper()
	out := make(map[string][]string)
	fset := token.NewFileSet()
	for _, f := range files {
		if !strings.HasSuffix(f.Path, ".go") {
			continue
		}
		file, err := parser.ParseFile(fset, f.Path, f.Contents, parser.SkipObjectResolution)
		require.NoError(t, err, "emitted %s does not parse", f.Path)
		for _, decl := range file.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				out[f.Path+": "+funcKey(d)] = sortedNames(funcScope(d))
			case *ast.GenDecl:
				collectInterfaceScopes(out, f.Path, d)
			}
		}
	}
	return out
}

// funcKey names one function declaration independently of anything a
// query text chooses, so the map compares like with like across
// emissions.
func funcKey(d *ast.FuncDecl) string {
	if d.Recv == nil {
		return "func " + d.Name.Name
	}
	expr := d.Recv.List[0].Type
	if star, ok := expr.(*ast.StarExpr); ok {
		expr = star.X
	}
	recv := "?"
	if id, ok := expr.(*ast.Ident); ok {
		recv = id.Name
	}
	return "method " + recv + "." + d.Name.Name
}

// funcScope is every name a function declaration binds: its signature's
// own identifiers plus everything its body introduces. Flat rather than
// block-scoped, because the question is which names an author can reach,
// and a name bound in any block of the body is one the argument shares a
// lookup chain with.
func funcScope(d *ast.FuncDecl) map[string]bool {
	bound := make(map[string]bool)
	addFieldNames(bound, d.Recv, d.Type.Params, d.Type.Results)
	if d.Body == nil {
		return bound
	}
	ast.Inspect(d.Body, func(n ast.Node) bool {
		switch stmt := n.(type) {
		case *ast.AssignStmt:
			if stmt.Tok == token.DEFINE {
				addIdentExprs(bound, stmt.Lhs...)
			}
		case *ast.ValueSpec:
			for _, id := range stmt.Names {
				addName(bound, id.Name)
			}
		case *ast.TypeSpec:
			addName(bound, stmt.Name.Name)
		case *ast.RangeStmt:
			if stmt.Tok == token.DEFINE {
				addIdentExprs(bound, stmt.Key, stmt.Value)
			}
		case *ast.LabeledStmt:
			addName(bound, stmt.Label.Name)
		case *ast.FuncLit:
			addFieldNames(bound, stmt.Type.Params, stmt.Type.Results)
		}
		return true
	})
	return bound
}

// collectInterfaceScopes records the signature of every interface method
// the declaration holds. querier.go is an interface and nothing else, and
// its method signatures carry the same argument name the method
// definitions do.
func collectInterfaceScopes(out map[string][]string, path string, d *ast.GenDecl) {
	if d.Tok != token.TYPE {
		return
	}
	for _, spec := range d.Specs {
		ts, ok := spec.(*ast.TypeSpec)
		if !ok {
			continue
		}
		it, ok := ts.Type.(*ast.InterfaceType)
		if !ok {
			continue
		}
		for _, m := range it.Methods.List {
			ft, isFunc := m.Type.(*ast.FuncType)
			if !isFunc || len(m.Names) != 1 {
				continue
			}
			bound := make(map[string]bool)
			addFieldNames(bound, ft.Params, ft.Results)
			out[path+": interface "+ts.Name.Name+"."+m.Names[0].Name] = sortedNames(bound)
		}
	}
}

// mentionedIdents is every identifier the emission spells anywhere, plus
// blank. These are the candidate parameter names: a name the emitter
// never writes cannot be captured by one, and blank is included because
// it is the one candidate that never appears as a name — it appeared as
// an empty one.
func mentionedIdents(t *testing.T, files []codegen.File) []string {
	t.Helper()
	seen := map[string]bool{"_": true}
	fset := token.NewFileSet()
	for _, f := range files {
		if !strings.HasSuffix(f.Path, ".go") {
			continue
		}
		file, err := parser.ParseFile(fset, f.Path, f.Contents, parser.SkipObjectResolution)
		require.NoError(t, err, "emitted %s does not parse", f.Path)
		ast.Inspect(file, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok {
				seen[id.Name] = true
			}
			return true
		})
	}
	return slices.Sorted(maps.Keys(seen))
}

// addFieldNames records the names a signature's field lists bind. A nil
// list (no receiver, no results) contributes nothing.
func addFieldNames(bound map[string]bool, lists ...*ast.FieldList) {
	for _, l := range lists {
		if l == nil {
			continue
		}
		for _, f := range l.List {
			for _, n := range f.Names {
				addName(bound, n.Name)
			}
		}
	}
}

// addIdentExprs records the plain identifiers among exprs, skipping the
// selectors and index expressions an assignment can also target.
func addIdentExprs(bound map[string]bool, exprs ...ast.Expr) {
	for _, e := range exprs {
		if id, ok := e.(*ast.Ident); ok {
			addName(bound, id.Name)
		}
	}
}

// addName records one bound name, dropping blank.
func addName(bound map[string]bool, name string) {
	if name != "_" {
		bound[name] = true
	}
}

// sortedNames is a bound set as a stable slice, so a comparison reports a
// difference in names rather than in iteration order.
func sortedNames(bound map[string]bool) []string {
	return slices.Sorted(maps.Keys(bound))
}
