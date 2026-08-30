package neo4j

import (
	"errors"
	"fmt"

	"github.com/areqag/gqlc/internal/codegen"
)

// nameBackend adds this backend's name to a width the shared phases
// refused. The refusal comes from asking this package's type table, so
// it is this backend's answer and not a property of the schema: a run
// emitting several targets has to say which of them refused, the more
// so for a nested list, which Apache AGE stores happily. Every other
// refusal follows from the input alone, so wrapping it here would
// misattribute it.
//
// neo4j's Temporal() never refuses — types.go says so — so unlike AGE's
// twin this only ever decorates width refusals.
func nameBackend(err error) error {
	if !errors.Is(err, codegen.ErrUnrepresentableWidth) {
		return err
	}
	return fmt.Errorf("%w, which the neo4j backend cannot store as a property", err)
}

// generate is the pure emission kernel. Determinism per §2.3: input
// slices are walked in their author-defined order; the output slice is
// sorted by Path before return. First-error short-circuit: (nil, err)
// on failure.
func generate(in codegen.Input, target driverTarget, packageName string) ([]codegen.File, error) {
	prepared, err := codegen.Prepare(in, typeMap{}, packageName)
	if err != nil {
		return nil, nameBackend(err)
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

	// The neutral temporal carriers and their driver bridge, emitted as
	// a pair and only when the prepared surface references a carrier
	// (ADR 0033). temporal.go is byte-identical across every target;
	// temporal_neo4j.go is this backend's, and holds every dbtype
	// mention the carriers displaced off the public surface.
	if codegen.ReferencesTemporalCarrier(prepared) {
		files = append(files,
			codegen.File{Path: "temporal.go", Contents: codegen.RenderTemporal(pkg)},
			codegen.File{Path: "temporal_neo4j.go", Contents: renderTemporalConversions(pkg, temporalUses(prepared), target)},
		)
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
