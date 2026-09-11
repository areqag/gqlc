package age

import (
	"errors"

	"github.com/areqag/gqlc/internal/codegen"
	"github.com/areqag/gqlc/internal/queryfile"
)

// codegenBug is the panic value decodeFunc raises for a carrier it has no
// arm for, and the only species generate converts into a refusal below.
//
// The type is what makes the conversion selective, and it is declared here
// beside the recover that consumes it rather than beside the panic that
// raises it, because the recover is the half with the narrow contract:
// raised only by decodeFunc (render_queries.go), recovered only by
// generate. A panic of any other type crosses this seam untouched, which
// is what unserved_nil_type_test.go's generateUnserved depends on — it
// takes no recover of its own so that a fault inside the render surfaces
// as that test's own panic, naming the site.
type codegenBug string

// renderFaultHook, when non-nil, panics in place of the render phase. It
// is nil in production and set only by this package's own tests.
//
// It exists because the fault it stands in for is not reachable from any
// assembled Input: a carrier with no arm is a change to the type table
// itself (decodeFunc's comment says so at length), not anything an author
// can write in a schema or a query. Without a trigger the recover below
// would have no standing witness, so deleting it would redden nothing —
// the inert-guard failure this seam exists to end (bd gqlc-h0vqx). The
// hook is what keeps the guard falsifiable in both directions.
var renderFaultHook func()

// generate is the pure emission kernel. Determinism per §2.3: the output
// slice is sorted by Path before return. First-error short-circuit:
// (nil, err) on failure.
//
// A codegen bug raised during the render phase is converted to a refusal
// here rather than taking the process. Production surfacing is unchanged
// in substance: the one caller (internal/cli/pipeline) propagates the
// error, the CLI refuses the run and emits no package, and the panic's own
// sentence is the error's text, byte for byte. What changes is that a test
// binary survives it — before this seam, one untaught carrier reaching any
// of the 69 Generate call sites in this package's tests killed the whole
// binary, so the pin whose job is to NAME the lost carrier never ran and a
// second, unrelated regression stayed invisible behind the first.
//
// This seam is NOT the whole fence, and the design that proposed it said it
// was. Twelve tests call the render layer directly through the export_test
// bridge, below generate, and a codegen bug reaching one of those still
// took the binary with this recover in place — measured, not argued. The
// other half is in render_fence_test.go.
//
// The recover is same-goroutine, which is sound only because the render
// phase is synchronous: no non-test file in this package spells `go func`,
// `sync.` or `errgroup`. Render work moved onto another goroutine would
// bypass this defer and take the process again.
func generate(in codegen.Input, packageName string) (files []codegen.File, err error) {
	defer func() {
		v := recover()
		if v == nil {
			return
		}
		bug, ok := v.(codegenBug)
		if !ok {
			panic(v)
		}
		// Both returns are assigned: Generate's contract is (nil, err)
		// on failure, never a partial slice.
		files, err = nil, errors.New(string(bug))
	}()
	// Ahead of Prepare: a batch carrying a query this backend cannot serve
	// is not improved by first being told which of its property widths do
	// not map, and that report sends the author to fix a schema that was
	// never the obstacle.
	if err := rejectUnservedQueries(in.Queries); err != nil {
		return nil, err
	}
	// Second, and also ahead of Prepare: the gate above reads the resolved
	// column shape, and these hazards are properties of the query TEXT —
	// an alternation the author never projects, or a constructor in a
	// predicate the query model drops (ADR 0003), reaches no column at all
	// and is still a statement the server will not accept. It runs second
	// only so that an edge-union column wins, which names the candidates
	// the schema declares and so says more about the same defect; on every
	// other reason the gate above yields to this one rather than send the
	// author to fix a projection before they learn the statement never
	// parsed (rejectUnservedQueries).
	//
	// Both halves of that position are load-bearing and both are pinned by
	// what the author is told, not by any reading of this file. Ahead of
	// Prepare: TestRejectsRelationshipTypeAlternation/"a column shared
	// admission refuses is answered here, because this runs first",
	// TestRejectsUndefinedFunctions/"a projected constructor is answered
	// here, ahead of the portable temporal refusal",
	// TestRunApacheAgeAnswersAnAlternationAheadOfSharedAdmission and
	// TestRunApacheAgeRefusesUndefinedFunctions/"a projected constructor
	// is answered here, ahead of the carrier". Behind
	// rejectUnservedQueries for the edge union alone: the same tests' "an
	// edge-union column is answered by the column gate, which says more"
	// and "an unserved column that is not an edge union yields to the
	// text", plus, at the CLI seam,
	// TestRunApacheAgeAnswersAnAlternationAheadOfOtherColumnRefusals.
	if err := rejectDialectGaps(in.Queries); err != nil {
		return nil, err
	}
	prepared, err := codegen.Prepare(in, typeMap{}, packageName)
	if err != nil {
		return nil, nameBackend(err)
	}
	entities, err := wireEntities(prepared.Entities, len(prepared.Queries))
	if err != nil {
		return nil, err
	}
	if err := rejectOffsetSidecarCollisions(prepared.Entities); err != nil {
		return nil, err
	}

	pkg := prepared.Package
	hasOne := false
	var h helpers
	h.forEntities(entities)
	for _, p := range prepared.Queries {
		if p.Cardinality == queryfile.CardinalityOne {
			hasOne = true
		}
		if len(p.ParamFields) > 0 {
			h.args = true
		}
		h.forParams(p.ParamFields)
		for _, f := range p.RowFields {
			h.need(f.GoType, f.Width)
		}
	}

	files = []codegen.File{
		{Path: "db.go", Contents: renderDB(pkg, len(prepared.Queries) > 0, hasOne)},
		{Path: "graph.go", Contents: renderGraph(pkg)},
		{Path: "querier.go", Contents: renderQuerier(pkg, prepared.Queries)},
		{Path: "models.go", Contents: renderModels(pkg, entities, h)},
	}
	// Sited HERE, with files already holding four entries and the
	// per-source emission below still to come, because a real fault can
	// arrive at exactly this depth: renderCypherFile reaches decodeFunc
	// through writeMethod, writeOneBody, writeColumnDecode and
	// columnDecoder, and that loop appends. A carrier untaught in a QUERY
	// column therefore unwinds from the middle of the append with files
	// PARTIALLY built, which is the case the recover's `files = nil`
	// exists for. A hook sited before the slice was built would find
	// files already nil and could not tell that assignment from a missing
	// one.
	if renderFaultHook != nil {
		renderFaultHook()
	}
	// The neutral temporal carriers, emitted byte-identically on every
	// backend and only when the prepared surface references one (ADR
	// 0033). No driver bridge follows them here: the neo4j targets pair
	// this file with a temporal_<driver>.go converting to and from the
	// driver's own temporal types, and agtype has none — a carrier
	// reaches the wire through the agtype encode and decode helpers in
	// models.go, which are where this backend's encoding lives.
	if codegen.ReferencesTemporalCarrier(prepared) {
		files = append(files, codegen.File{Path: "temporal.go", Contents: codegen.RenderTemporal(pkg)})
	}
	// Per-source `<name>.cypher.go` emission — grouped by SourceFile
	// basename in first-appearance order (§5.5).
	for _, group := range groupBySource(prepared.Queries) {
		files = append(files, codegen.File{
			Path:     group.filename,
			Contents: renderCypherFile(pkg, group.queries),
		})
	}
	return codegen.Finalise(files)
}
