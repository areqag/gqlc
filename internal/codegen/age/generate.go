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
	prepared, err := codegen.Prepare(in, typeMap{}, packageName)
	if err != nil {
		return nil, err
	}

	pkg := prepared.Package
	hasOne := false
	var h helpers
	for _, p := range prepared.Queries {
		if p.Cardinality == codegen.CardinalityOne {
			hasOne = true
		}
		if len(p.ParamFields) > 0 {
			h.args = true
		}
		for _, f := range p.RowFields {
			h.need(f.GoType)
		}
	}

	files := []codegen.File{
		{Path: "db.go", Contents: renderDB(pkg, len(prepared.Queries) > 0, hasOne)},
		{Path: "graph.go", Contents: renderGraph(pkg)},
		{Path: "querier.go", Contents: renderQuerier(pkg, prepared.Queries)},
		{Path: "models.go", Contents: renderModels(pkg, h)},
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
