package neo4j

import (
	"github.com/areqag/gqlc/internal/codegen"
)

// generate is the pure emission kernel. Determinism per §2.3: input
// slices are walked in their author-defined order; the output slice is
// sorted by Path before return. First-error short-circuit: (nil, err)
// on failure.
func generate(in codegen.Input, target driverTarget, packageName string) ([]codegen.File, error) {
	prepared, err := codegen.Prepare(in, typeMap{}, packageName)
	if err != nil {
		return nil, err
	}

	pkg := prepared.Package
	hasOne := false
	for _, p := range prepared.Queries {
		if p.Cardinality == codegen.CardinalityOne {
			hasOne = true
			break
		}
	}

	files := []codegen.File{
		{Path: "db.go", Contents: renderDB(pkg, hasOne, target)},
		{Path: "querier.go", Contents: renderQuerier(pkg, prepared.Queries, target)},
		{Path: "models.go", Contents: renderModels(pkg, prepared.Entities, prepared.Queries, target)},
	}

	// Per-source `<name>.cypher.go` file emission — grouped by
	// SourceFile basename in first-appearance order (§5.5). Basename
	// stripped of extension.
	for _, group := range groupBySource(prepared.Queries) {
		needDbtype, needTime, needFmt := groupImports(group.queries)
		files = append(files, codegen.File{
			Path:     group.filename,
			Contents: renderCypherFile(pkg, group.queries, needDbtype, needTime, needFmt, target),
		})
	}

	return codegen.Finalise(files)
}
