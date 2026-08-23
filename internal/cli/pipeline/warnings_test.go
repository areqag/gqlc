package pipeline_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/cli/pipeline"
	"github.com/areqag/gqlc/internal/config"
)

// A schema declaring two of the three relationship types the drift query names.
// FLAGGED is declared nowhere, so the resolver narrows it away and warns
// (ADR 0032); AUTHORED survives, so the edge still closes and the run succeeds.
const driftSchema = `CREATE PROPERTY GRAPH TYPE Drift AS {
    (:Person { id :: INT64 NOT NULL }),
    (:Post   { id :: INT64 NOT NULL }),
    (:Person) -[:AUTHORED { since :: INT64 NOT NULL }]-> (:Post),
    (:Person) -[:LIKES { rating :: INT64 NOT NULL }]-> (:Post)
}
`

const driftQuery = "// name: ActionOnPost :many\n" +
	"MATCH (:Person)-[r:AUTHORED|LIKES|FLAGGED]->(p:Post) RETURN r\n"

// writeDriftProject is writeProject's shape with a schema the queries have
// drifted ahead of.
func writeDriftProject(t *testing.T) (dir, cfgPath, queryPath string) {
	t.Helper()
	dir = t.TempDir()
	writeFixtureFile(t, filepath.Join(dir, "schema.gql"), driftSchema)
	queryPath = filepath.Join(dir, "queries", "drift.cypher")
	writeFixtureFile(t, queryPath, driftQuery)
	cfgPath = filepath.Join(dir, config.DefaultFilename)
	writeFixtureFile(t, cfgPath, configYAML("drift", "neo4j-go-v5", ""))
	return dir, cfgPath, queryPath
}

// A warning is not a diagnostic: the run succeeds, the batch is generated, and
// the warning still comes back. Assert all three — dropping the collection
// leaves the first two true, and turning the warning into a diagnostic leaves
// the third true.
func TestRunCarriesResolverWarningsWithoutFailing(t *testing.T) {
	_, cfgPath, queryPath := writeDriftProject(t)

	res, err := pipeline.Run(cfgPath, backendRegistry(t))
	require.NoError(t, err)
	require.Empty(t, res.Diagnostics, "a warning must not fail the run")
	require.NotNil(t, only(t, res).Files, "the batch is still generated")

	require.Len(t, res.Warnings, 1)
	require.Equal(t,
		"graph[0]: "+queryPath+": query ActionOnPost: warning: relationship type \"FLAGGED\" is not declared "+
			"in the schema; edge \"r\" was narrowed to its declared types and no decoder is generated for "+
			"\"FLAGGED\", so a row of that type fails at runtime. Fix the spelling if it is misspelled, or "+
			"declare it if the graph has drifted ahead of the schema.",
		res.Warnings[0])
}

// The negative: the ordinary project warns about nothing. Without this the row
// above passes for a pipeline that emits a warning per query.
func TestRunOnACleanProjectWarnsAboutNothing(t *testing.T) {
	_, cfgPath := writeProject(t)

	res, err := pipeline.Run(cfgPath, backendRegistry(t))
	require.NoError(t, err)
	require.Empty(t, res.Warnings)
}

// Warnings carry their own graph[i] prefix, and per target. The two-target
// shape is the one that catches a collector that concatenates every target's
// warnings under the first index.
func TestRunWarningsAreLabelledPerTarget(t *testing.T) {
	dir, cfgPath, queryPath := writeDriftProject(t)
	writeFixtureFile(t, filepath.Join(dir, "second.gql"), driftSchema)
	second := filepath.Join(dir, "second", "drift.cypher")
	writeFixtureFile(t, second, driftQuery)
	writeFixtureFile(t, cfgPath, configOf(
		targetEntry("schema.gql", "queries", "drift", "out", "neo4j-go-v5", ""),
		targetEntry("second.gql", "second", "driftb", "out2", "neo4j-go-v6", ""),
	))

	res, err := pipeline.Run(cfgPath, backendRegistry(t))
	require.NoError(t, err)
	require.Len(t, res.Warnings, 2)
	require.Contains(t, res.Warnings[0], "graph[0]: "+queryPath+": query ActionOnPost: ")
	require.Contains(t, res.Warnings[1], "graph[1]: "+second+": query ActionOnPost: ")
}

// A failing run keeps its warnings. The all-or-nothing rule (§6.2) discards
// every BATCH when a diagnostic lands; it must not discard the advice, because
// the misspelling the warning names may be why the neighbouring query failed.
func TestRunKeepsWarningsAlongsideDiagnostics(t *testing.T) {
	dir, cfgPath, queryPath := writeDriftProject(t)
	bad := filepath.Join(dir, "queries", "zz_ghost.cypher")
	writeFixtureFile(t, bad, "// name: BadLabel :many\nMATCH (g:Ghost) RETURN g\n")

	res, err := pipeline.Run(cfgPath, backendRegistry(t))
	require.NoError(t, err)
	require.Len(t, res.Diagnostics, 1)
	require.Nil(t, res.Targets, "the Targets/Diagnostics invariant is untouched")
	require.Len(t, res.Warnings, 1)
	require.Contains(t, res.Warnings[0], "graph[0]: "+queryPath+": query ActionOnPost: ")
}
