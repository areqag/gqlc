package age

import (
	"github.com/areqag/gqlc/internal/codegen"
)

// generate is the pure emission kernel. Determinism per §2.3: the output
// slice is sorted by Path before return. First-error short-circuit:
// (nil, err) on failure.
func generate(in codegen.Input, packageName string) ([]codegen.File, error) {
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
		if p.Cardinality == codegen.CardinalityOne {
			hasOne = true
		}
		if len(p.ParamFields) > 0 {
			h.args = true
		}
		h.forParams(p.ParamFields)
		for _, f := range p.RowFields {
			h.need(f.GoType)
		}
	}

	files := []codegen.File{
		{Path: "db.go", Contents: renderDB(pkg, len(prepared.Queries) > 0, hasOne)},
		{Path: "graph.go", Contents: renderGraph(pkg)},
		{Path: "querier.go", Contents: renderQuerier(pkg, prepared.Queries)},
		{Path: "models.go", Contents: renderModels(pkg, entities, h)},
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
