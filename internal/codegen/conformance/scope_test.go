package conformance_test

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/cli/backends"
	"github.com/areqag/gqlc/internal/codegen"
	"github.com/areqag/gqlc/internal/graph"
	"github.com/areqag/gqlc/internal/procsig"
	"github.com/areqag/gqlc/internal/query/cypher"
	"github.com/areqag/gqlc/internal/queryfile"
	"github.com/areqag/gqlc/internal/resolver"
	"github.com/areqag/gqlc/internal/schema"
	"github.com/areqag/gqlc/internal/schema/gql"
)

// The graph-type elements the probe shapes are written against. Held one
// per constant rather than as a single schema because a shape names the
// elements its query needs: a target with no carrier for one of them
// drops that shape, and the element goes with it.
//
// Person carries a nullable property and a narrow width alongside their
// non-null wide counterparts, so one node type reaches the pointer-wrap
// and the narrowing-conversion arms as well as the plain one.
const (
	probePersonElement = `(:Person {
        name     :: STRING NOT NULL,
        age      :: INT NOT NULL,
        nickname :: STRING,
        score    :: INT
    })`
	probeDocElement = `(:Doc {
        title :: STRING NOT NULL,
        tags  :: LIST<STRING>
    })`
	probePostElement     = `(:Post { id :: INT64 NOT NULL })`
	probeAuthoredElement = `(:Person) -[:AUTHORED { since :: INT64 NOT NULL }]-> (:Post)`
	probeLikesElement    = `(:Person) -[:LIKES]-> (:Post)`
)

// scopeProbeShape is one emitted body the sweep has to reach: the
// graph-type elements its query needs, and the query.
type scopeProbeShape struct {
	// name is the query's own method name, so a shape a backend refuses
	// is logged under the name that backend's refusal already reports.
	name     string
	elements []string
	// query is one whole annotated query. %[1]s is the parameter name
	// under test — every shape binds exactly one parameter, because the
	// single-parameter form is the only one whose Go argument was ever
	// derived from the query text.
	query string
}

// probeShapeName reads a shape's name off its own annotation, so the two
// cannot drift apart.
var probeShapeName = regexp.MustCompile(`^// name: (\w+) `)

// shapesOver is the probe shapes that share one set of graph-type
// elements. Grouping by element set rather than repeating it per shape
// keeps the census below readable as the census it is.
func shapesOver(elements []string, queries ...string) []scopeProbeShape {
	out := make([]scopeProbeShape, len(queries))
	for i, q := range queries {
		m := probeShapeName.FindStringSubmatch(q)
		if m == nil {
			panic("probe shape does not open with a name annotation: " + q)
		}
		out[i] = scopeProbeShape{name: m[1], elements: elements, query: q}
	}
	return out
}

// scopeProbeShapes reaches every decode arm codegen commits a column to,
// at both read cardinalities, single-column and inside a row struct,
// nullable and not, plus the write and :exec bodies. Coverage of the arms
// is asserted rather than claimed — see TestScopeProbeReachesEveryDecodeArm.
//
// Breadth is the whole point: a body the probe never emits declares
// locals the sweep never sees, so a parameter named after one of them is
// never probed. That is a proper subset of the scope masquerading as the
// whole of it, which is the one thing this file must not be.
var scopeProbeShapes = slices.Concat(
	shapesOver([]string{probePersonElement},
		"// name: ProbeScalarOne :one\nMATCH (p:Person) WHERE p.name = $%[1]s RETURN p.name",
		"// name: ProbeScalarMany :many\nMATCH (p:Person) WHERE p.name = $%[1]s RETURN p.name",
		"// name: ProbeScalarRow :many\nMATCH (p:Person) WHERE p.name = $%[1]s RETURN p.name, p.age",
		"// name: ProbeNullableColumn :one\nMATCH (p:Person) WHERE p.name = $%[1]s RETURN p.nickname",
		"// name: ProbeNullableRow :many\nMATCH (p:Person) WHERE p.name = $%[1]s RETURN p.nickname, p.score",
		"// name: ProbeNarrowParam :one\nMATCH (p:Person) WHERE p.age = $%[1]s RETURN p.name",
		"// name: ProbeNullableParam :one\nMATCH (p:Person) WHERE p.score = $%[1]s RETURN p.name",
		"// name: ProbeNodeColumn :one\nMATCH (p:Person) WHERE p.name = $%[1]s RETURN p",
		"// name: ProbeNullableNodeColumn :one\nOPTIONAL MATCH (p:Person) WHERE p.name = $%[1]s RETURN p",
		"// name: ProbeNodeColumnRow :many\nMATCH (p:Person) WHERE p.name = $%[1]s RETURN p, p.age",
		"// name: ProbeNestedList :many\nMATCH (p:Person) WHERE p.name = $%[1]s RETURN [[[1]]] AS xs",
		"// name: ProbeListOfNull :one\nMATCH (p:Person) WHERE p.name = $%[1]s RETURN [null] AS xs",
		"// name: ProbeListOfUnknown :one\nMATCH (p:Person) WHERE p.name = $%[1]s RETURN [foo(p.age)] AS xs",
		"// name: ProbeListOfTemporal :one\nMATCH (p:Person) WHERE p.name = $%[1]s RETURN [date()] AS xs",
		"// name: ProbeTemporalColumn :one\nMATCH (p:Person) WHERE p.name = $%[1]s RETURN datetime() AS now",
		"// name: ProbeTemporalCarrierColumn :one\nMATCH (p:Person) WHERE p.name = $%[1]s RETURN date() AS d",
		"// name: ProbeMapColumn :one\nMATCH (p:Person) WHERE p.name = $%[1]s RETURN {a: 1} AS m",
		"// name: ProbeAnyColumn :many\nMATCH (p:Person) WHERE p.name = $%[1]s RETURN foo(p.age) AS r",
		"// name: ProbeNullColumn :one\nMATCH (p:Person) WHERE p.name = $%[1]s RETURN null AS n",
		"// name: ProbeExec :exec\nMATCH (p:Person) WHERE p.name = $%[1]s DELETE p",
		"// name: ProbeWriteProjectionOne :one\nCREATE (p:Person {name: $%[1]s}) RETURN p",
		"// name: ProbeWriteProjectionMany :many\nMATCH (p:Person) WHERE p.name = $%[1]s SET p.age = 1 RETURN p.name",
	),
	// The list property lives on its own node type: a backend with no
	// carrier for LIST refuses the whole graph type at admission, which
	// would drop every shape above with it.
	shapesOver([]string{probeDocElement},
		"// name: ProbeNullableListProperty :one\nMATCH (d:Doc) WHERE d.title = $%[1]s RETURN d.tags",
	),
	shapesOver([]string{probePersonElement, probePostElement, probeAuthoredElement},
		"// name: ProbeEdgeColumn :one\nMATCH (p:Person)-[r:AUTHORED]->(:Post) WHERE p.name = $%[1]s RETURN r",
		"// name: ProbeListOfEdge :one\nMATCH (p:Person)-[r:AUTHORED*]->(:Post) WHERE p.name = $%[1]s RETURN r",
	),
	shapesOver([]string{probePersonElement, probePostElement, probeAuthoredElement, probeLikesElement},
		"// name: ProbeEdgeUnionOne :one\nMATCH (p:Person)-[r:AUTHORED|LIKES]->(:Post) WHERE p.name = $%[1]s RETURN r",
		"// name: ProbeEdgeUnionMany :many\nMATCH (p:Person)-[r:AUTHORED|LIKES]->(:Post) WHERE p.name = $%[1]s RETURN r",
		"// name: ProbeEdgeUnionRow :one\nMATCH (p:Person)-[r:AUTHORED|LIKES]->(:Post) WHERE p.name = $%[1]s RETURN p, r",
		"// name: ProbeNullableEdgeUnion :one\nMATCH (p:Person) WHERE p.name = $%[1]s\nOPTIONAL MATCH (p)-[r:AUTHORED|LIKES]->(:Post)\nRETURN r",
		"// name: ProbeListOfEdgeUnion :one\nMATCH (p:Person)-[r:AUTHORED|LIKES*]->(:Post) WHERE p.name = $%[1]s RETURN r",
	),
)

// unclaimedParam is a parameter name no emission mentions, so the scopes
// read off an emission spelled with it are the reference every other
// spelling is measured against.
const unclaimedParam = "alpha"

// TestEmittedScopeIsGeneratorOwned pins what a query author can put into
// an emitted method's scope, which is nothing.
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
// What is pinned is bounded by what the probe emits, so the probe emits
// every decode arm the target serves: the shapes it drops are the ones
// that target refuses to generate at all, and the arms the shapes reach
// are asserted whole in TestScopeProbeReachesEveryDecodeArm. The
// candidate names are likewise read off an emission rather than listed,
// and the sweep runs per target, so each backend is probed with its own
// vocabulary. Nothing here reads the golden corpus: -update cannot make
// this test pass.
func TestEmittedScopeIsGeneratorOwned(t *testing.T) {
	probe := newScopeProbe(t)
	for _, target := range probe.targets {
		t.Run(target, func(t *testing.T) {
			batch := probe.batch(target)
			reference := probe.emit(t, target, unclaimedParam)
			want := boundScopes(t, reference)
			require.NotEmpty(t, want, "the emission binds no identifiers to compare")

			candidates := mentionedIdents(t, reference)
			require.NotEmpty(t, candidates, "the emission mentions no identifiers to probe")

			for _, name := range candidates {
				t.Run(name, func(t *testing.T) {
					in, err := batch.input(name)
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

// TestScopeProbeReachesEveryDecodeArm holds the probe to the claim the
// sweep rests on. An arm no shape reaches is a body whose locals never
// enter the candidate set, so a parameter named after one of them is
// never swept and the sweep's silence about it means nothing.
//
// The arms are read off Phase B rather than off any one backend, because
// what a shape reaches is a property of the resolved column and not of
// the target that renders it; a target that refuses an arm drops the
// shape at newScopeProbe instead.
func TestScopeProbeReachesEveryDecodeArm(t *testing.T) {
	for i, arm := range probeDecodeArms {
		require.Equal(t, codegen.ColumnKind(i), arm,
			"probeDecodeArms is not codegen.ColumnKind in declaration order")
		require.NotEmpty(t, decodeArmName(arm), "arm %d has no name", i)
	}
	require.Empty(t, decodeArmName(codegen.ColumnKind(len(probeDecodeArms))),
		"codegen.ColumnKind holds a member past the end of probeDecodeArms: name it there and add a probe shape that reaches it")

	sch, res := probeGraph(t, scopeProbeShapes)
	queries := make([]codegen.NamedQuery, 0, len(scopeProbeShapes))
	for _, shape := range scopeProbeShapes {
		lowered, err := lowerProbeQueries(res, shape.query, unclaimedParam)
		require.NoError(t, err, "shape %q does not lower", shape.name)
		queries = append(queries, lowered...)
	}
	prepared, err := codegen.Prepare(codegen.Input{Schema: sch, Queries: queries}, probeTypeMap{}, "probe")
	require.NoError(t, err)

	reached := make(map[codegen.ColumnKind]string)
	for _, q := range prepared.Queries {
		for _, f := range q.RowFields {
			reached[f.Kind] = q.MethodName
			for e := f.ListElem; e != nil; e = e.Nested {
				reached[e.Kind] = q.MethodName
			}
		}
	}
	for _, arm := range probeDecodeArms {
		require.Contains(t, reached, arm, "no probe shape reaches the %s arm", decodeArmName(arm))
	}
}

// probeDecodeArms is every arm codegen commits a column to. Go cannot
// enumerate an iota enum, so the membership is written out and then
// proved whole: decodeArmName names each of these and nothing past them,
// which for a contiguous enum is the whole of it.
var probeDecodeArms = []codegen.ColumnKind{
	codegen.ColumnProperty,
	codegen.ColumnNode,
	codegen.ColumnEdge,
	codegen.ColumnTemporal,
	codegen.ColumnScalar,
	codegen.ColumnScalarNull,
	codegen.ColumnList,
	codegen.ColumnAny,
	codegen.ColumnEdgeUnion,
}

// decodeArmName names one committed column kind for a failure message.
// The switch carries no default, so a kind added to codegen.ColumnKind
// fails the exhaustiveness check here; the empty fallback is what makes a
// value past the end of the enum nameless, which is how
// TestScopeProbeReachesEveryDecodeArm proves probeDecodeArms holds all of it.
func decodeArmName(k codegen.ColumnKind) string {
	switch k {
	case codegen.ColumnProperty:
		return "schema property"
	case codegen.ColumnNode:
		return "node entity"
	case codegen.ColumnEdge:
		return "edge entity"
	case codegen.ColumnTemporal:
		return "temporal expression"
	case codegen.ColumnScalar:
		return "scalar expression"
	case codegen.ColumnScalarNull:
		return "null list element"
	case codegen.ColumnList:
		return "list"
	case codegen.ColumnAny:
		return "undecided"
	case codegen.ColumnEdgeUnion:
		return "edge union"
	}
	return ""
}

// probeTypeMap admits every width, so the arm a column lands on is Phase
// B's answer alone. A backend's own table refuses widths it has no
// carrier for, which would drop shapes from the arm census and hide the
// very gap the census exists to find.
type probeTypeMap struct{}

func (probeTypeMap) Property(pt graph.PropertyType) (string, bool) { return string(pt), true }
func (probeTypeMap) Temporal(k resolver.Temporal) string           { return fmt.Sprintf("temporal%d", k) }
func (probeTypeMap) Scalar(k resolver.Scalar) string               { return fmt.Sprintf("scalar%d", k) }

// scopeProbe emits the probe batch through every registered backend.
type scopeProbe struct {
	targets []string
	lookup  func(string) (func(pkg string) codegen.Generator, bool)
	// byTarget maps a target onto the batch it emits. Targets that serve
	// the same shapes share one batch, so the schema is parsed once and
	// the lowered inputs are memoised across them.
	byTarget map[string]*scopeProbeBatch
}

// scopeProbeBatch is the shapes one target emits, the graph type they
// need, and the lowered inputs read off them. The resolver is built once
// and the lowering memoised per parameter name: only the query text
// changes between emissions, and re-parsing a graph type per candidate
// would dominate the sweep.
type scopeProbeBatch struct {
	shapes []scopeProbeShape
	schema schema.Schema
	res    *resolver.Resolver
	inputs map[string]probeLowering
}

// probeLowering is one memoised lowering. The error is the front end's
// alone — a name the query grammar rejects is not this test's business —
// so a caller skips on it rather than failing.
type probeLowering struct {
	in  codegen.Input
	err error
}

func newScopeProbe(t *testing.T) *scopeProbe {
	t.Helper()
	reg, err := backends.Registry()
	require.NoError(t, err)
	targets := reg.Keys()
	require.NotEmpty(t, targets, "no backend is registered, so this test holds nothing")

	p := &scopeProbe{targets: targets, lookup: reg.Lookup, byTarget: make(map[string]*scopeProbeBatch, len(targets))}
	byShapes := make(map[string]*scopeProbeBatch)
	served := make(map[string]bool, len(scopeProbeShapes))
	for _, target := range targets {
		shapes := p.servedShapes(t, target, served)
		require.NotEmpty(t, shapes, "target %q emits none of the probe shapes", target)
		key := strings.Join(shapeNames(shapes), ",")
		batch, ok := byShapes[key]
		if !ok {
			sch, res := probeGraph(t, shapes)
			batch = &scopeProbeBatch{shapes: shapes, schema: sch, res: res, inputs: make(map[string]probeLowering)}
			byShapes[key] = batch
		}
		p.byTarget[target] = batch
		// The assembled batch must emit even though every shape in it
		// emitted alone: a refusal that only fires on the whole batch
		// would otherwise drop the sweep to whatever still generated.
		in, err := batch.input(unclaimedParam)
		require.NoError(t, err, "the probe batch for %q does not lower", target)
		p.generate(t, target, in)
	}
	for _, shape := range scopeProbeShapes {
		require.True(t, served[shape.name],
			"no registered target emits shape %q, so it contributes nothing to any sweep", shape.name)
	}
	return p
}

// servedShapes is the shapes target emits, each judged on its own: a
// shape that generates alone is in, and one the backend refuses is out
// along with the graph-type elements only it needed. Judging by the
// backend's own admission rather than by a per-target list is what keeps
// the sweep as wide as the backend is, without this file having to know
// which widths each one carries.
func (p *scopeProbe) servedShapes(t *testing.T, target string, served map[string]bool) []scopeProbeShape {
	t.Helper()
	newGen, ok := p.lookup(target)
	require.True(t, ok, "no backend is registered under %q", target)
	var out []scopeProbeShape
	for _, shape := range scopeProbeShapes {
		sch, res := probeGraph(t, []scopeProbeShape{shape})
		queries, err := lowerProbeQueries(res, shape.query, unclaimedParam)
		require.NoError(t, err, "shape %q does not lower", shape.name)
		files, err := newGen("probe").Generate(codegen.Input{Schema: sch, Queries: queries})
		if err != nil {
			require.Nil(t, files, "target %q refused shape %q and still returned files", target, shape.name)
			t.Logf("target %q does not emit shape %q: %v", target, shape.name, err)
			continue
		}
		served[shape.name] = true
		out = append(out, shape)
	}
	return out
}

func (p *scopeProbe) batch(target string) *scopeProbeBatch { return p.byTarget[target] }

// emit is input plus generate for a parameter name the batch must accept.
func (p *scopeProbe) emit(t *testing.T, target, param string) []codegen.File {
	t.Helper()
	in, err := p.byTarget[target].input(param)
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

// input lowers the batch spelled around one parameter name.
func (b *scopeProbeBatch) input(param string) (codegen.Input, error) {
	if got, ok := b.inputs[param]; ok {
		return got.in, got.err
	}
	queries := make([]codegen.NamedQuery, 0, len(b.shapes))
	var lowered probeLowering
	for _, shape := range b.shapes {
		qs, err := lowerProbeQueries(b.res, shape.query, param)
		if err != nil {
			lowered = probeLowering{err: err}
			b.inputs[param] = lowered
			return lowered.in, lowered.err
		}
		queries = append(queries, qs...)
	}
	lowered = probeLowering{in: codegen.Input{Schema: b.schema, Queries: queries}}
	b.inputs[param] = lowered
	return lowered.in, nil
}

// probeGraph parses the graph type the given shapes need, in
// first-appearance order and once per element, and builds the resolver
// over it.
func probeGraph(t *testing.T, shapes []scopeProbeShape) (schema.Schema, *resolver.Resolver) {
	t.Helper()
	var elements []string
	for _, shape := range shapes {
		for _, e := range shape.elements {
			if !slices.Contains(elements, e) {
				elements = append(elements, e)
			}
		}
	}
	src := "CREATE PROPERTY GRAPH TYPE ScopeProbe AS {\n    " + strings.Join(elements, ",\n    ") + "\n}"
	sch, err := gql.New().Parse(strings.NewReader(src))
	require.NoError(t, err, "the probe graph type does not parse:\n%s", src)
	procs, err := procsig.NewRegistry(nil)
	require.NoError(t, err)
	return sch, resolver.New(sch, resolver.WithRegistry(procs))
}

// lowerProbeQueries runs one shape's query text through the front end
// spelled around param.
func lowerProbeQueries(res *resolver.Resolver, query, param string) ([]codegen.NamedQuery, error) {
	annotated, err := queryfile.New().Parse(strings.NewReader(fmt.Sprintf(query, param)))
	if err != nil {
		return nil, err
	}
	procs, err := procsig.NewRegistry(nil)
	if err != nil {
		return nil, err
	}
	out := make([]codegen.NamedQuery, 0, len(annotated))
	for _, aq := range annotated {
		q, err := cypher.New(cypher.WithRegistry(procs)).Parse(bytes.NewReader([]byte(aq.Text)))
		if err != nil {
			return nil, err
		}
		vq, err := res.Resolve(q)
		if err != nil {
			return nil, err
		}
		out = append(out, codegen.NamedQuery{
			Name:        aq.Name,
			Cardinality: aq.Cardinality,
			SourceFile:  "queries.cypher",
			SourceText:  aq.Text,
			Validated:   vq,
		})
	}
	return out, nil
}

// shapeNames is a shape slice's names, which identify the slice: the
// shapes are drawn from one ordered set, so equal name lists mean equal
// batches.
func shapeNames(shapes []scopeProbeShape) []string {
	out := make([]string, len(shapes))
	for i, s := range shapes {
		out[i] = s.name
	}
	return out
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
