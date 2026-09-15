package neo4j

import (
	"github.com/areqag/gqlc/internal/codegen"
	"github.com/areqag/gqlc/internal/queryfile"
)

// generate is the pure emission kernel. Determinism per §2.3: input
// slices are walked in their author-defined order; the output slice is
// sorted by Path before return. First-error short-circuit: (nil, err)
// on failure.
func generate(in codegen.Input, target driverTarget, packageName string) ([]codegen.File, error) {
	prepared, err := codegen.Prepare(in, target.types(), packageName)
	if err != nil {
		return nil, refuse(err, target)
	}

	pkg := prepared.Package
	// hasOne gates renderDB's :one sentinels; hasIter gates its streaming
	// seam. Both are "does the batch hold at least one", and neither can
	// break early now that two answers come out of the walk.
	hasOne, hasIter := false, false
	for _, p := range prepared.Queries {
		switch p.Cardinality {
		case queryfile.CardinalityOne:
			hasOne = true
		case queryfile.CardinalityIter:
			hasIter = true
		case queryfile.CardinalityMany, queryfile.CardinalityExec:
		}
	}

	// One walk answers both conversion kinds, so models.go's record
	// helpers and temporal_neo4j.go's carrier bridges are gated off the
	// same reading of the batch (see conversionUses).
	neutralUse, recordUse, unionUse := conversionUses(prepared, target.types())

	files := []codegen.File{
		{Path: "db.go", Contents: renderDB(pkg, hasOne, hasIter, target)},
		{Path: "querier.go", Contents: renderQuerier(pkg, prepared.Queries, target)},
		{Path: "models.go", Contents: renderModels(pkg, prepared.Entities, prepared.Queries, target)},
	}

	// The declared records' carriers and helpers, in their own file for
	// the reason the temporal bridge has one: they are emitted only when
	// the batch reaches a record, and a batch can reach one through a
	// query parameter alone — which models.go, whose whole body is gated
	// on the schema declaring an entity, would emit nothing for.
	if encodings := codegen.RecordEncodings(prepared.Entities, prepared.Queries); len(encodings) > 0 {
		files = append(files, codegen.File{
			Path:     "record_neo4j.go",
			Contents: renderRecordHelpers(pkg, encodings, recordUse, target),
		})
	}

	// The closed unions' validation and dispatch helpers, in their own
	// file for the reason the records have one: they are emitted only
	// when the batch reaches a union, and a batch can reach one through a
	// query parameter alone — which models.go, whose whole body is gated
	// on the schema declaring an entity, would emit nothing for.
	if encodings := codegen.UnionEncodings(prepared.Entities, prepared.Queries); len(encodings) > 0 {
		files = append(files, codegen.File{
			Path:     "union_neo4j.go",
			Contents: renderUnionHelpers(pkg, encodings, unionUse, target),
		})
	}

	// The neutral temporal carriers and their driver bridge, emitted as
	// a pair and only when the prepared surface references a carrier
	// (ADR 0033). temporal.go is byte-identical across every target;
	// temporal_neo4j.go is this backend's, and holds every dbtype
	// mention the carriers displaced off the public surface.
	if codegen.ReferencesTemporalCarrier(prepared) {
		files = append(files,
			codegen.File{Path: "temporal.go", Contents: codegen.RenderTemporal(pkg)},
			codegen.File{Path: "temporal_neo4j.go", Contents: renderTemporalConversions(pkg, neutralUse, target)},
		)
	}

	// The neutral UUID carrier and its driver bridge, on exactly the
	// terms the temporal pair above stands on and triggered separately
	// from it: uuid.go is byte-identical across every target,
	// uuid_neo4j.go is this backend's and holds every dbtype mention the
	// carrier displaced off the public surface. Only the v6 target
	// reaches here — v5 has no carrier for the width, so Prepare refuses
	// the batch before emission.
	if codegen.ReferencesUUIDCarrier(prepared) {
		files = append(files,
			codegen.File{Path: "uuid.go", Contents: codegen.RenderUUID(pkg)},
			codegen.File{Path: "uuid_neo4j.go", Contents: renderUUIDConversions(pkg, neutralUse, target)},
		)
	}

	// Per-source `<name>.cypher.go` file emission — grouped by
	// SourceFile basename in first-appearance order (§5.5). Basename
	// stripped of extension.
	for _, group := range groupBySource(prepared.Queries) {
		needDbtype, needTime, needFmt, needIter := groupImports(group.queries)
		files = append(files, codegen.File{
			Path:     group.filename,
			Contents: renderCypherFile(pkg, group.queries, needDbtype, needTime, needFmt, needIter, target),
		})
	}

	return codegen.Finalise(files)
}
