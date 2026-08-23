package neo4j_test

import (
	"bytes"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/codegen"
	"github.com/areqag/gqlc/internal/codegen/emitscan"
	"github.com/areqag/gqlc/internal/codegen/neo4j"
	"github.com/areqag/gqlc/internal/graph"
	"github.com/areqag/gqlc/internal/resolver"
	"github.com/areqag/gqlc/internal/schema"
	"github.com/areqag/gqlc/internal/schema/gql"
)

// captureProbeParam is the parameter name the baseline emission binds
// while the candidate set is read off it. Deliberately a name no
// emission produces, so the candidates are the corpus's own vocabulary
// rather than one the probe seeded — and feeding it back through the
// sweep is a fixed point, which is the cheapest check that the probe is
// not itself capturing anything.
const captureProbeParam = "gqlcCaptureProbe"

// captureFillerPrefix names the parameters that pad a multi-parameter
// shape out. It is a name no emission produces, so a filler's own field
// name never lands back in the candidate set, and it cannot collide
// with a swept candidate under the §4.2 mangle.
const captureFillerPrefix = "gqlcCaptureFiller"

// captureQuerySuffix selects the emitted files carrying query METHODS.
// db.go, models.go and querier.go do not have a query method's shape —
// a receiver, ctx and exactly one generated argument — so the per-file
// half of the sweep is asked of these alone.
const captureQuerySuffix = ".cypher.go"

// captureArity is one parameter shape the sweep rebinds the corpus to:
// arity many parameters, with the swept name at index swept and
// generated fillers elsewhere.
type captureArity struct {
	label string
	arity int
	swept int
}

// captureArities is every signature arm the neo4j emitter has that
// binds a parameter at all, plus a position within the multi-parameter
// arm that is not the first.
//
// The emitter switches on arity — nothing, one parameter, or a
// generated Params struct — and each arm writes its own signature
// (render_queries.go's writeMethodSignature), so a capture one arm
// performs is invisible to a sweep that only ever binds another. The
// zero-parameter arm is not here: it emits no argument, so there is no
// name for a query to capture, and binding zero parameters would also
// make the sweep's own candidate emission empty. The last-of-three
// entry exists because an arm that named its argument after the FIRST
// parameter and an arm that named it after the LAST are different bugs,
// and only one of them is caught by sweeping position 0.
var captureArities = []captureArity{
	{label: "one", arity: 1, swept: 0},
	{label: "first-of-two", arity: 2, swept: 0},
	{label: "last-of-three", arity: 3, swept: 2},
}

// TestNoEmittedNameTakesAQueryParameterName sweeps the neo4j emission
// for generator-owned names a query parameter could capture.
//
// Two halves, both silent failure modes rather than compile errors.
// First: an emitted method's signature must name its argument itself,
// whatever the query text called the parameter. If any arm ever derives
// the binding from the author's name, everything the body resolves
// under that name resolves to the caller's value instead. Second:
// declaring a name is not what puts it at risk — RESOLVING it is, and
// an emitted body resolves the package-level query-text const without
// ever declaring it. A shadowed query-text const is the worst instance:
// $<bare>QueryText makes Method(ctx, "MATCH (n) DETACH DELETE n") run
// that text, with no concatenation anywhere to find, and the widths
// collide so the compiler says nothing.
//
// Two axes, both read rather than listed. The candidate names are every
// identifier the emission mentions, taken off the syntax tree, so a
// name the emitter starts using later is covered without anyone
// remembering to add it. The emission shapes are the driver corpus's —
// scalar columns, entity columns, edge unions, optional matches,
// variable-length lists and a multi-column row — swept as they stand,
// so a declaration emitted under a branch keyed on row width or column
// kind is reached rather than assumed away. The third axis is parameter
// arity, and captureArities carries it.
//
// This is the age package's TestNoEmittedNameTakesAQueryParameterName
// asked of neo4j, over the analyser in internal/codegen/emitscan rather
// than over a second copy of it. That sweep proved nothing about this
// backend — its machinery lived inside age's own test package — which
// is bd gqlc-3o3p. Migrating age onto emitscan and deleting its copy is
// left to a lane that owns that package.
//
// TestBackendInvariantSurface cannot backstop this: its declaredSurface
// helper keeps only TYPE GenDecls, and the capture class is precisely
// about consts and vars.
func TestNoEmittedNameTakesAQueryParameterName(t *testing.T) {
	t.Parallel()

	sch := captureSchema(t)
	queries := corpusNamedQueries(t, sch)
	require.NotEmpty(t, queries, "the corpus declares no query, so no method binds a parameter")

	// The candidate set is read off the one-parameter shape so it is the
	// corpus's own vocabulary. Reading it off a multi-parameter emission
	// would pull the filler parameters' own field names in, and feeding
	// those back would collide under the §4.2 mangle rather than
	// measuring anything.
	single := captureArities[0]
	require.Equal(t, 1, single.arity, "the candidate set must be read off the one-parameter shape")
	base := captureEmit(t, sch, queries, captureProbeParam, single)
	candidates, err := emitscan.Candidates(captureQueryFiles(base))
	require.NoError(t, err)
	require.NotEmpty(t, candidates, "the emission mentions no identifiers to check")

	// A sweep that never reaches the multi-column row-assembly branch is
	// a hole that would close silently, so the census is that branch's
	// own condition — len(RowFields) >= 2 — evaluated on the branch's own
	// input. Phase B appends exactly one RowField per validated column
	// and fails the whole batch rather than dropping one, so a query's
	// column count is its prepared row-field count. Measuring the INPUT
	// is what makes this unsatisfiable by anything the emitter does;
	// counting emitted package-level types would say the same thing today
	// and stop saying it the day the emitter adds a type for any other
	// reason.
	multiColumn := 0
	for _, q := range queries {
		if len(q.Validated.Columns) >= 2 {
			multiColumn++
		}
	}
	require.Positive(t, multiColumn,
		"no swept query projects two columns, so the multi-column emission branch is unswept")

	for _, shape := range captureArities {
		baseline := captureEmit(t, sch, queries, captureProbeParam, shape)
		for _, name := range candidates {
			t.Run(fmt.Sprintf("%s/%s", name, shape.label), func(t *testing.T) {
				found := emitscan.Sweep{
					Baseline:    baseline,
					Probe:       captureEmit(t, sch, queries, name, shape),
					Name:        name,
					ArgName:     codegen.ParamArg,
					QuerySuffix: captureQuerySuffix,
				}.Run()
				require.Empty(t, found,
					"a query parameter named %q captures a generator-owned name at arity %s:\n%s",
					name, shape.label, emitscan.Findings(found))
			})
		}
	}
}

// captureEmit rebinds every corpus query to the given parameter shape,
// carrying the swept name at that shape's swept index, and returns the
// whole emitted package by path.
//
// Every parameter is STRING, which is the width that makes a collision
// silent: it is what the composed statement and the query-text const
// both are, so an argument that displaces one is assigned over rather
// than rejected by the compiler. A narrower width would turn the
// interesting case into a build failure and the sweep would stop
// measuring it.
//
// The whole package is returned, not the query files alone, because Go
// package scope is package-wide: the edge-union sum types are declared
// in models.go and resolved from the .cypher.go bodies, and a parse set
// stopping at .cypher.go would call those references undeclared — and
// would never see a string-typed cross-file declaration captured out of
// a method body, which is the silent case.
//
// One driver target is emitted rather than both. The corpus test above
// holds the v5 and v6 emissions equal modulo the driver module path and
// the driver handle's name, so a name swept here is the same name on
// v6; if that ever stops holding, that test is what fails.
func captureEmit(
	t *testing.T,
	sch schema.Schema,
	queries []codegen.NamedQuery,
	param string,
	shape captureArity,
) map[string]string {
	t.Helper()
	require.Positive(t, shape.arity, "shape %s binds no parameter", shape.label)
	require.Less(t, shape.swept, shape.arity, "shape %s sweeps past its own arity", shape.label)

	params := make([]resolver.ResolvedParameter, 0, shape.arity)
	for i := range shape.arity {
		name := fmt.Sprintf("%s%d", captureFillerPrefix, i)
		if i == shape.swept {
			name = param
		}
		params = append(params, resolver.ResolvedParameter{
			Name: name,
			Type: resolver.ResolvedProperty{Type: graph.TypeString},
		})
	}

	bound := slices.Clone(queries)
	for i := range bound {
		bound[i].Validated.Parameters = params
	}

	files, err := neo4j.New(neo4j.WithPackageName(corpusPackage)).
		Generate(codegen.Input{Schema: sch, Queries: bound})
	require.NoError(t, err, "emitting with %s as %s", param, shape.label)
	out := make(map[string]string, len(files))
	for _, f := range files {
		if strings.HasSuffix(f.Path, ".go") {
			out[f.Path] = string(f.Contents)
		}
	}
	return out
}

// captureQueryFiles keeps the .cypher.go files of an emission. Used to
// read the corpus's own vocabulary: reading it off the whole package
// would feed db.go's boilerplate names back into the sweep, multiplying
// the runtime by a set that carries no query shape at all.
func captureQueryFiles(files map[string]string) map[string]string {
	out := make(map[string]string)
	for path, body := range files {
		if strings.HasSuffix(path, captureQuerySuffix) {
			out[path] = body
		}
	}
	return out
}

// captureSchema parses the driver corpus's schema. It is the widest
// schema this package carries — node and edge types, narrowing widths,
// nullables, lists at three depths and shapeless properties — so the
// emission it drives mentions the widest vocabulary any neo4j emission
// does.
func captureSchema(t *testing.T) schema.Schema {
	t.Helper()
	sch, err := gql.New().Parse(bytes.NewReader([]byte(corpusFile(t, corpusSchema))))
	require.NoError(t, err)
	return sch
}
