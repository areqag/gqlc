package cli

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/config"
)

// A project whose query names FLAGGED, which its schema declares nowhere.
// Generation succeeds — AUTHORED still closes the edge — so this is the shape
// that used to be silently wrong (ADR 0032).
const (
	driftSchema = `CREATE PROPERTY GRAPH TYPE Drift AS {
    (:Person { id :: INT64 NOT NULL }),
    (:Post   { id :: INT64 NOT NULL }),
    (:Person) -[:AUTHORED { since :: INT64 NOT NULL }]-> (:Post),
    (:Person) -[:LIKES { rating :: INT64 NOT NULL }]-> (:Post)
}
`
	driftQuery = "// name: ActionOnPost :many\n" +
		"MATCH (:Person)-[r:AUTHORED|LIKES|FLAGGED]->(p:Post) RETURN r\n"
)

func writeDriftProject(t *testing.T) (dir, queryPath string) {
	t.Helper()
	dir = t.TempDir()
	writeFixtureFile(t, filepath.Join(dir, "schema.gql"), driftSchema)
	queryPath = filepath.Join(dir, "queries", "drift.cypher")
	writeFixtureFile(t, queryPath, driftQuery)
	writeFixtureFile(t, filepath.Join(dir, config.DefaultFilename),
		configYAML("drift", "neo4j-go-v5", ""))
	return dir, queryPath
}

// The whole point of the bead: the generate that used to say nothing now says
// what it did, on stderr, and still succeeds and still writes.
func TestGeneratePrintsWarningsAndStillSucceeds(t *testing.T) {
	dir, queryPath := writeDriftProject(t)

	stdout, stderr, err := runGenerateIn(t, dir)
	require.NoError(t, err)
	require.Empty(t, stdout, "a warning belongs on stderr, not in the tool's output stream")
	require.DirExists(t, filepath.Join(dir, "out"), "the batch is still written")

	require.Equal(t,
		"graph[0]: "+queryPath+": query ActionOnPost: warning: relationship type \"FLAGGED\" is not "+
			"declared in the schema; edge \"r\" was narrowed to its declared types and no decoder is "+
			"generated for \"FLAGGED\", so a row of that type fails at runtime. Fix the spelling if it "+
			"is misspelled, or declare it if the graph has drifted ahead of the schema.\n",
		stderr)
}

// The negative half. The ordinary project's stderr stays empty, so the row
// above cannot pass for a CLI that prints on every run.
func TestGenerateOnACleanProjectPrintsNothing(t *testing.T) {
	dir := writeProject(t)

	stdout, stderr, err := runGenerateIn(t, dir)
	require.NoError(t, err)
	require.Empty(t, stdout)
	require.Empty(t, stderr)
}

// Warnings are printed before the diagnostics and are not counted by the
// summary error: "generate: 1 error", not 2. A warning folded into the count
// would make the exit status say a compile failed when it did not.
func TestGenerateWarningsPrintBeforeDiagnosticsAndAreNotCounted(t *testing.T) {
	dir, _ := writeDriftProject(t)
	writeFixtureFile(t, filepath.Join(dir, "queries", "zz_ghost.cypher"),
		"// name: BadLabel :many\nMATCH (g:Ghost) RETURN g\n")

	_, stderr, err := runGenerateIn(t, dir)
	require.Error(t, err)
	require.Equal(t, "generate: 1 error", err.Error())

	lines := strings.Split(strings.TrimSuffix(stderr, "\n"), "\n")
	require.Len(t, lines, 3)
	require.Contains(t, lines[0], "warning: relationship type \"FLAGGED\"")
	require.Contains(t, lines[1], "query BadLabel: unknown label: Ghost")
	require.Equal(t, "Error: generate: 1 error", lines[2])
	require.NoDirExists(t, filepath.Join(dir, "out"), "a failed run still writes nothing")
}
