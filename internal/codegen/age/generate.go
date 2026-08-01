package age

import (
	"github.com/areqag/gqlc/internal/codegen"
)

// generate is the pure emission kernel. Determinism per §2.3: the output
// slice is sorted by Path before return. First-error short-circuit:
// (nil, err) on failure.
func generate(in codegen.Input, packageName string) ([]codegen.File, error) {
	prepared, err := codegen.Prepare(in, typeMap{}, packageName)
	if err != nil {
		return nil, err
	}
	if err := rejectUnservedQueries(prepared.Queries); err != nil {
		return nil, err
	}
	pkg := prepared.Package
	return codegen.Finalise([]codegen.File{
		{Path: "db.go", Contents: renderDB(pkg)},
		{Path: "graph.go", Contents: renderGraph(pkg)},
		{Path: "querier.go", Contents: renderQuerier(pkg)},
		{Path: "models.go", Contents: renderModels(pkg)},
	})
}
