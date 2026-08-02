package pipeline_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/cli/backends"
	"github.com/areqag/gqlc/internal/cli/pipeline"
	"github.com/areqag/gqlc/internal/codegen"
	"github.com/areqag/gqlc/internal/codegen/age"
	"github.com/areqag/gqlc/internal/config"
)

// backendRegistry is the composed backend table a config file's driver
// axis resolves against.
func backendRegistry(t *testing.T) codegen.Registry {
	t.Helper()
	reg, err := backends.Registry()
	require.NoError(t, err)
	return reg
}

// Minimal fixture: mirrors the CLI-1 §7 project shape but callable
// without cobra. One node type, one :many query. Schema name "People"
// mangles to package "people".
const (
	fixtureSchema = `CREATE PROPERTY GRAPH TYPE People AS {
    (:Person {
        id   :: INT64 NOT NULL,
        name :: STRING NOT NULL
    })
}
`
	// The projection is a scalar property because every registered
	// backend serves one, which is what lets the driver axis hold the
	// whole vocabulary against a single project.
	fixtureQuery = "// name: AllPersonNames :many\nMATCH (p:Person) RETURN p.name\n"

	// listSchema and listQuery project a list property, which the Apache
	// AGE backend has no decode arm for. The list lives on a schema of
	// its own rather than on fixtureSchema, because every target shares
	// that one and a list on it would fail the AGE arm of the driver
	// axis too.
	listSchema = `CREATE PROPERTY GRAPH TYPE People AS {
    (:Person {
        id   :: INT64 NOT NULL,
        tags :: LIST<STRING>
    })
}
`
	listQuery = "// name: PersonTags :one\nMATCH (p:Person) RETURN p.tags AS tags\n"

	// unionSchema and unionQuery project an edge that binds to more than
	// one candidate edge type. openCypher spells that a relationship-type
	// alternation and has no other syntax for it (Cypher.g4
	// oC_RelationshipTypes), and Apache AGE 1.7.0's parser refuses the
	// alternation, so the query text the emitted code would carry could
	// never reach that server. On a schema of their own for the reason
	// listSchema has one.
	unionSchema = `CREATE PROPERTY GRAPH TYPE People AS {
    (:Person { id :: INT64 NOT NULL }),
    (:Post   { id :: INT64 NOT NULL }),
    (:Person) -[:AUTHORED { since :: INT64 NOT NULL }]-> (:Post),
    (:Person) -[:LIKES]-> (:Post)
}
`
	unionQuery = "// name: GetAction :one\nMATCH (:Person)-[r:AUTHORED|LIKES]->(:Post) RETURN r\n"

	// The second target's schema declares a label the first one does
	// not, so a query written against either fails against the other —
	// what TestRunStateIsPerTarget turns on.
	invoiceSchema = `CREATE PROPERTY GRAPH TYPE Invoices AS {
    (:Invoice {
        id    :: INT64 NOT NULL,
        total :: INT64 NOT NULL
    })
}
`
	invoiceQuery = "// name: AllInvoices :many\nMATCH (i:Invoice) RETURN i\n"

	callQuery    = "// name: Labels :many\nCALL test.labels() YIELD label\nRETURN label\n"
	callRegistry = `{"signatures":[{"name":"test.labels","params":[],` +
		`"results":[{"name":"label","type":"STRING","nullable":true}]}]}`
)

// targetEntry renders one graph entry. procsig is emitted only when
// non-empty (the key is optional).
func targetEntry(schemaPath, queryDir, pkg, out, driver, procsigPath string) string {
	e := "  - schema: " + schemaPath + "\n" +
		"    schema_language: gql\n" +
		"    queries: " + queryDir + "\n" +
		"    query_language: opencypher\n"
	if procsigPath != "" {
		e += "    procsig: " + procsigPath + "\n"
	}
	return e +
		"    gen:\n" +
		"      go:\n" +
		"        package: " + pkg + "\n" +
		"        out: " + out + "\n" +
		"        driver: " + driver + "\n"
}

// configOf renders a v1 config from rendered graph entries.
func configOf(entries ...string) string {
	return "version: 1\ngraph:\n" + strings.Join(entries, "")
}

// configYAML renders a one-entry v1 config wired to the writeProject
// layout.
func configYAML(pkg, driver, procsig string) string {
	return configOf(targetEntry("schema.gql", "queries", pkg, "out", driver, procsig))
}

// writeFixtureFile writes contents at path, creating parent dirs as
// needed.
func writeFixtureFile(t *testing.T, path, contents string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o644))
}

// writeProject lays down the minimal project in a fresh tempdir and
// returns its root plus the config path.
func writeProject(t *testing.T) (dir, cfgPath string) {
	t.Helper()
	dir = t.TempDir()
	writeFixtureFile(t, filepath.Join(dir, "schema.gql"), fixtureSchema)
	writeFixtureFile(t, filepath.Join(dir, "queries", "people.cypher"), fixtureQuery)
	cfgPath = filepath.Join(dir, config.DefaultFilename)
	writeFixtureFile(t, cfgPath, configYAML("people", "neo4j-go-v5", ""))
	return dir, cfgPath
}

// writeTwoTargetProject extends writeProject with a second target that
// shares nothing with the first: its own schema, query directory,
// package, driver and output directory.
func writeTwoTargetProject(t *testing.T) (dir, cfgPath string) {
	t.Helper()
	dir, cfgPath = writeProject(t)
	writeFixtureFile(t, filepath.Join(dir, "invoices.gql"), invoiceSchema)
	writeFixtureFile(t, filepath.Join(dir, "invoices", "invoices.cypher"), invoiceQuery)
	writeFixtureFile(t, cfgPath, configOf(
		targetEntry("schema.gql", "queries", "people", "out", "neo4j-go-v5", ""),
		targetEntry("invoices.gql", "invoices", "invoicedb", "out2", "neo4j-go-v6", ""),
	))
	return dir, cfgPath
}

// only returns the single TargetResult of a one-target run, failing
// the test when the run produced any other number.
func only(t *testing.T, res pipeline.Result) pipeline.TargetResult {
	t.Helper()
	require.Len(t, res.Targets, 1)
	return res.Targets[0]
}

// findFile returns the first TargetResult.Files entry with the given
// Path, or fails the test.
func findFile(t *testing.T, tr pipeline.TargetResult, name string) []byte {
	t.Helper()
	for _, f := range tr.Files {
		if f.Path == name {
			return f.Contents
		}
	}
	t.Fatalf("no file %q in TargetResult.Files (paths: %v)", name, filePaths(tr))
	return nil
}

func filePaths(tr pipeline.TargetResult) []string {
	paths := make([]string, 0, len(tr.Files))
	for _, f := range tr.Files {
		paths = append(paths, f.Path)
	}
	return paths
}

// requireMarkerHeaded asserts the file's first line carries both
// fixed halves of the generation marker (CLI-1 §5.1 step 3).
func requireMarkerHeaded(t *testing.T, name string, contents []byte) {
	t.Helper()
	line, _, _ := strings.Cut(string(contents), "\n")
	require.True(t, strings.HasPrefix(line, "// Code generated by gqlc "), "first line of %s: %q", name, line)
	require.True(t, strings.HasSuffix(line, " DO NOT EDIT."), "first line of %s: %q", name, line)
}

// TestRunHappyPathReturnsFiles: Files is populated, sorted by Path,
// every file marker-headed; Diagnostics empty; OutDir the resolved
// config's output; err nil.
func TestRunHappyPathReturnsFiles(t *testing.T) {
	dir, cfgPath := writeProject(t)

	res, err := pipeline.Run(cfgPath, backendRegistry(t))
	require.NoError(t, err)
	require.Empty(t, res.Diagnostics)
	tr := only(t, res)
	require.NotNil(t, tr.Files)
	require.Equal(t, filepath.Join(dir, "out"), tr.OutDir)

	require.Equal(t, []string{"db.go", "models.go", "people.cypher.go", "querier.go"}, filePaths(tr))
	for _, f := range tr.Files {
		requireMarkerHeaded(t, f.Path, f.Contents)
	}
}

// TestRunPackageNameFromConfig: db.go's package clause comes from the
// config's package key, overriding the schema-name mangle (CLI-1
// §3.4).
func TestRunPackageNameFromConfig(t *testing.T) {
	dir, cfgPath := writeProject(t)
	writeFixtureFile(t, cfgPath, configYAML("peopledb", "neo4j-go-v5", ""))

	res, err := pipeline.Run(cfgPath, backendRegistry(t))
	require.NoError(t, err)
	require.Empty(t, res.Diagnostics)
	tr := only(t, res)
	require.Equal(t, filepath.Join(dir, "out"), tr.OutDir)

	db := findFile(t, tr, "db.go")
	require.Contains(t, string(db), "\npackage peopledb\n")
	require.NotContains(t, string(db), "\npackage people\n")
}

// driverAxis is the client-library import each driver's db.go must
// carry (CLI-1 §3.2). TestRunDriverAxis walks config.DriverValues()
// against it, so registering a backend without recording what it emits
// fails here rather than passing unobserved.
var driverAxis = map[config.Driver]string{
	config.DriverNeo4jGoV5:      `"github.com/neo4j/neo4j-go-driver/v5/neo4j"`,
	config.DriverNeo4jGoV6:      `"github.com/neo4j/neo4j-go-driver/v6/neo4j"`,
	config.DriverApacheAgePgxV5: `"github.com/jackc/pgx/v5"`,
}

// TestRunDriverAxis walks the driver vocabulary rather than a hardcoded
// list, so a value with no recorded outcome fails the sweep.
func TestRunDriverAxis(t *testing.T) {
	drivers := config.DriverValues()
	require.Len(t, driverAxis, len(drivers),
		"every driver in config.DriverValues() needs a driverAxis entry saying what it does")

	for _, d := range drivers {
		wantImport, recorded := driverAxis[d]
		require.Truef(t, recorded, "driver %q has no recorded outcome", d)

		t.Run(string(d), func(t *testing.T) {
			_, cfgPath := writeProject(t)
			writeFixtureFile(t, cfgPath, configYAML("people", string(d), ""))

			res, err := pipeline.Run(cfgPath, backendRegistry(t))
			require.NoError(t, err)
			require.Empty(t, res.Diagnostics)
			db := findFile(t, only(t, res), "db.go")
			require.Contains(t, string(db), wantImport)
		})
	}
}

// TestRunApacheAgeRejectionIsASentinel pins the shape of the Apache AGE
// backend's narrower vocabulary at the pipeline seam: a query it cannot
// serve fails the entry — entry-prefixed like every other target
// failure, with the zero Result the caller must not write — and the
// failure is matchable with errors.Is, so a caller can branch on it
// without reading the message.
func TestRunApacheAgeRejectionIsASentinel(t *testing.T) {
	dir, cfgPath := writeProject(t)
	writeFixtureFile(t, filepath.Join(dir, "schema.gql"), listSchema)
	writeFixtureFile(t, filepath.Join(dir, "queries", "people.cypher"), listQuery)
	writeFixtureFile(t, cfgPath, configYAML("people", string(config.DriverApacheAgePgxV5), ""))

	res, err := pipeline.Run(cfgPath, backendRegistry(t))
	require.ErrorIs(t, err, age.ErrUnsupportedQuery)
	require.ErrorContains(t, err, "graph[0]: unsupported query: ")
	require.ErrorContains(t, err, `1 query would be dropped: PersonTags (column "tags" projects a list property)`)
	require.Equal(t, pipeline.Result{}, res)
}

// TestRunApacheAgeRefusesEdgeUnions pins the other half of that
// vocabulary at the same seam, for the one column kind whose refusal is
// a property of the server's parser rather than of the wire format.
//
// An edge column with more than one candidate edge type is reachable in
// openCypher only through a relationship-type alternation, and Apache
// AGE 1.7.0 answers `-[r:AUTHORED|LIKES]->` with `ERROR: syntax error at
// or near "|"` (SQLSTATE 42601), measured against the image
// test/data/codegen pins. Generated code runs the author's query text
// verbatim (ADR 0005), so emitting for this column would hand back a
// package that compiles and whose every call fails at the server.
// Refusing at generation is what turns that into an answer the author
// gets before they ship.
func TestRunApacheAgeRefusesEdgeUnions(t *testing.T) {
	dir, cfgPath := writeProject(t)
	writeFixtureFile(t, filepath.Join(dir, "schema.gql"), unionSchema)
	writeFixtureFile(t, filepath.Join(dir, "queries", "people.cypher"), unionQuery)
	writeFixtureFile(t, cfgPath, configYAML("people", string(config.DriverApacheAgePgxV5), ""))

	res, err := pipeline.Run(cfgPath, backendRegistry(t))
	require.ErrorIs(t, err, age.ErrUnsupportedQuery)
	require.ErrorContains(t, err, `1 query would be dropped: GetAction (column "r" `+
		`binds to more than one candidate edge type, which openCypher expresses only as a `+
		`relationship-type alternation and Apache AGE's parser refuses)`)
	require.Equal(t, pipeline.Result{}, res)

	// The same project on a driver whose server parses the alternation
	// generates, so what failed above is this backend's answer and not
	// the schema, the query or the resolver.
	writeFixtureFile(t, cfgPath, configYAML("people", string(config.DriverNeo4jGoV5), ""))
	res, err = pipeline.Run(cfgPath, backendRegistry(t))
	require.NoError(t, err)
	require.Empty(t, res.Diagnostics)
}

// TestRunUnregisteredDriver: the driver axis resolves through the
// registry the caller passes, so a value the config vocabulary admits
// but the registry does not carry fails its target — entry-prefixed,
// with the zero Result the caller must not write.
func TestRunUnregisteredDriver(t *testing.T) {
	_, cfgPath := writeProject(t)
	writeFixtureFile(t, cfgPath, configYAML("people", "neo4j-go-v6", ""))

	res, err := pipeline.Run(cfgPath, codegen.Registry{})
	require.ErrorContains(t, err, `graph[0]: internal: no pipeline mapping for driver "neo4j-go-v6"`)
	require.Equal(t, pipeline.Result{}, res)
}

// TestRunProcsigWiredThroughFrontEnd: a CALL query resolves when the
// config declares a procsig registry; the identical project without
// the key surfaces one query-diag in Diagnostics (unknown procedure
// — the correct diagnosis, CLI-1 §3.1 stage 4).
func TestRunProcsigWiredThroughFrontEnd(t *testing.T) {
	t.Run("with procsig key", func(t *testing.T) {
		dir, cfgPath := writeProject(t)
		writeFixtureFile(t, filepath.Join(dir, "queries", "calls.cypher"), callQuery)
		writeFixtureFile(t, filepath.Join(dir, "procsig.json"), callRegistry)
		writeFixtureFile(t, cfgPath, configYAML("people", "neo4j-go-v5", "procsig.json"))

		res, err := pipeline.Run(cfgPath, backendRegistry(t))
		require.NoError(t, err)
		require.Empty(t, res.Diagnostics)
		// calls.cypher generates a per-source file.
		require.Contains(t, filePaths(only(t, res)), "calls.cypher.go")
	})

	t.Run("without procsig key", func(t *testing.T) {
		dir, cfgPath := writeProject(t)
		writeFixtureFile(t, filepath.Join(dir, "queries", "calls.cypher"), callQuery)

		res, err := pipeline.Run(cfgPath, backendRegistry(t))
		require.NoError(t, err)
		require.Nil(t, res.Targets)
		require.Len(t, res.Diagnostics, 1)
		require.Contains(t, res.Diagnostics[0],
			"graph[0]: "+filepath.Join(dir, "queries", "calls.cypher")+": query Labels: unknown procedure")
	})
}

// TestRunConfigMissing: the pipeline surfaces ErrConfigMissing with
// fs.ErrNotExist preserved in the wrap chain; the CLI's "run gqlc
// init" copy is NOT in the pipeline error (that hint lives only in
// generate.go, verified by TestGenerateConfigMissing).
func TestRunConfigMissing(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), config.DefaultFilename)

	res, err := pipeline.Run(cfgPath, backendRegistry(t))
	require.Error(t, err)
	require.ErrorIs(t, err, pipeline.ErrConfigMissing)
	require.ErrorIs(t, err, fs.ErrNotExist, "fs.ErrNotExist must remain in the wrap chain")
	require.Contains(t, err.Error(), cfgPath, "wrapped error should carry cfgPath")
	require.NotContains(t, err.Error(), "run gqlc init", "the sibling-subcommand hint stays CLI-side")
	require.Equal(t, pipeline.Result{}, res)
}

// TestRunNoQueryFiles: an empty queries dir → the pinned singular
// error under its entry prefix; Result is the zero value (§6).
func TestRunNoQueryFiles(t *testing.T) {
	dir, cfgPath := writeProject(t)
	require.NoError(t, os.Remove(filepath.Join(dir, "queries", "people.cypher")))

	res, err := pipeline.Run(cfgPath, backendRegistry(t))
	require.EqualError(t, err, "graph[0]: no query files (*.cypher) in "+filepath.Join(dir, "queries"))
	require.Equal(t, pipeline.Result{}, res)
}

// TestRunAccumulatesDiagnostics: broken-file + broken-queries +
// one-good fixture: Diagnostics holds every failure in pipeline order
// (file-then-annotation), Files nil, err nil — the CLI turns the
// accumulation into the summary error (CLI-1 §3.3).
func TestRunAccumulatesDiagnostics(t *testing.T) {
	dir, cfgPath := writeProject(t)
	q := filepath.Join(dir, "queries")
	writeFixtureFile(t, filepath.Join(q, "a.cypher"), "// name: Broken\nMATCH (n) RETURN n\n")
	writeFixtureFile(t, filepath.Join(q, "b.cypher"),
		"// name: BadOne :many\nMATCH (g:Ghost) RETURN g\n// name: BadTwo :many\nMATCH (w:Wraith) RETURN w\n")

	res, err := pipeline.Run(cfgPath, backendRegistry(t))
	require.NoError(t, err)
	require.Nil(t, res.Targets)
	require.Len(t, res.Diagnostics, 3)
	require.True(t, strings.HasPrefix(res.Diagnostics[0], "graph[0]: "+filepath.Join(q, "a.cypher")+": "),
		"diag[0]: %q", res.Diagnostics[0])
	require.True(t, strings.HasPrefix(res.Diagnostics[1], "graph[0]: "+filepath.Join(q, "b.cypher")+": query BadOne: "),
		"diag[1]: %q", res.Diagnostics[1])
	require.True(t, strings.HasPrefix(res.Diagnostics[2], "graph[0]: "+filepath.Join(q, "b.cypher")+": query BadTwo: "),
		"diag[2]: %q", res.Diagnostics[2])
}

// TestRunDiagnosticShapes: exact-match one file-diag (malformed
// annotation) and one query-diag (unknown label) against CLI-1 §2.3.
func TestRunDiagnosticShapes(t *testing.T) {
	dir, cfgPath := writeProject(t)
	q := filepath.Join(dir, "queries")
	writeFixtureFile(t, filepath.Join(q, "broken.cypher"), "// name: Broken\nMATCH (n) RETURN n\n")
	writeFixtureFile(t, filepath.Join(q, "ghost.cypher"), "// name: BadLabel :many\nMATCH (g:Ghost) RETURN g\n")

	res, err := pipeline.Run(cfgPath, backendRegistry(t))
	require.NoError(t, err)
	require.Nil(t, res.Targets)
	// people.cypher succeeds; broken.cypher and ghost.cypher fail.
	// Discovery order is lexical: broken, ghost, people.
	require.Len(t, res.Diagnostics, 2)
	require.Equal(t,
		"graph[0]: "+filepath.Join(q, "broken.cypher")+`: malformed query annotation: line 1: "// name: Broken"`,
		res.Diagnostics[0])
	require.Equal(t,
		"graph[0]: "+filepath.Join(q, "ghost.cypher")+": query BadLabel: unknown label: Ghost is not declared on any node type",
		res.Diagnostics[1])
}

// TestRunPathResolution: config sits in a subdirectory with relative
// keys, cfgPath is the absolute path — reads succeed against the
// config's directory, not the test process's cwd.
func TestRunPathResolution(t *testing.T) {
	proj := filepath.Join(t.TempDir(), "proj")
	require.NoError(t, os.MkdirAll(proj, 0o755))
	writeFixtureFile(t, filepath.Join(proj, "schema.gql"), fixtureSchema)
	writeFixtureFile(t, filepath.Join(proj, "queries", "people.cypher"), fixtureQuery)
	cfgPath := filepath.Join(proj, config.DefaultFilename)
	writeFixtureFile(t, cfgPath, configYAML("people", "neo4j-go-v5", ""))

	res, err := pipeline.Run(cfgPath, backendRegistry(t))
	require.NoError(t, err)
	require.Empty(t, res.Diagnostics)
	tr := only(t, res)
	require.Equal(t, filepath.Join(proj, "out"), tr.OutDir)
	require.NotEmpty(t, tr.Files)
}

// TestRunNoWrites: the seam's central promise. The config's output
// directory does not exist before or after Run — the pipeline
// performs no filesystem write. This is the fence against a caller
// (or a second consumer) accidentally invoking pipeline.Run for its
// side effects.
func TestRunNoWrites(t *testing.T) {
	dir, cfgPath := writeProject(t)
	out := filepath.Join(dir, "out")
	require.NoDirExists(t, out, "precondition: out must not exist before Run")

	res, err := pipeline.Run(cfgPath, backendRegistry(t))
	require.NoError(t, err)
	tr := only(t, res)
	require.NotEmpty(t, tr.Files)
	require.Equal(t, out, tr.OutDir)
	require.NoDirExists(t, out, "Run must not create the output directory — that is the CLI's job under ADR 0012")
}

// TestRunDiscoveryFilter: queries dir seeded with a README, a
// subdirectory holding a .cypher, and a .hidden.cypher — only the
// top-level non-dot *.cypher file is consumed (CLI-1 §4). The
// non-matching entries carry poison content that would fail the run
// if consumed.
func TestRunDiscoveryFilter(t *testing.T) {
	dir, cfgPath := writeProject(t)
	q := filepath.Join(dir, "queries")
	const poison = "// name: Broken\nnot a query\n"
	writeFixtureFile(t, filepath.Join(q, "README.md"), "docs, not queries\n")
	writeFixtureFile(t, filepath.Join(q, "sub", "nested.cypher"), poison)
	writeFixtureFile(t, filepath.Join(q, ".hidden.cypher"), poison)

	res, err := pipeline.Run(cfgPath, backendRegistry(t))
	require.NoError(t, err)
	require.Empty(t, res.Diagnostics)
	require.Equal(t, []string{"db.go", "models.go", "people.cypher.go", "querier.go"}, filePaths(only(t, res)))
}

// TestRunEveryTarget: a two-target config yields two TargetResults in
// document order, each carrying its own package clause, driver import
// and resolved OutDir (§6).
func TestRunEveryTarget(t *testing.T) {
	dir, cfgPath := writeTwoTargetProject(t)

	res, err := pipeline.Run(cfgPath, backendRegistry(t))
	require.NoError(t, err)
	require.Empty(t, res.Diagnostics)
	require.Len(t, res.Targets, 2)

	require.Equal(t, filepath.Join(dir, "out"), res.Targets[0].OutDir)
	require.Equal(t, []string{"db.go", "models.go", "people.cypher.go", "querier.go"},
		filePaths(res.Targets[0]))
	db0 := string(findFile(t, res.Targets[0], "db.go"))
	require.Contains(t, db0, "\npackage people\n")
	require.Contains(t, db0, `"github.com/neo4j/neo4j-go-driver/v5/neo4j"`)
	require.Contains(t, string(findFile(t, res.Targets[0], "models.go")), "type Person struct")

	require.Equal(t, filepath.Join(dir, "out2"), res.Targets[1].OutDir)
	require.Equal(t, []string{"db.go", "invoices.cypher.go", "models.go", "querier.go"},
		filePaths(res.Targets[1]))
	db1 := string(findFile(t, res.Targets[1], "db.go"))
	require.Contains(t, db1, "\npackage invoicedb\n")
	require.Contains(t, db1, `"github.com/neo4j/neo4j-go-driver/v6/neo4j"`)
	require.Contains(t, string(findFile(t, res.Targets[1], "models.go")), "type Invoice struct")
}

// TestRunSetupFailureFailsFast: a singular failure aborts the whole
// run at the offending entry, under that entry's prefix and with the
// zero Result — including when an earlier target already accumulated
// diagnostics, which the abort discards (§6.1).
func TestRunSetupFailureFailsFast(t *testing.T) {
	t.Run("unreadable schema at entry 1", func(t *testing.T) {
		dir, cfgPath := writeTwoTargetProject(t)
		require.NoError(t, os.Remove(filepath.Join(dir, "invoices.gql")))

		res, err := pipeline.Run(cfgPath, backendRegistry(t))
		require.Error(t, err)
		require.True(t, strings.HasPrefix(err.Error(), "graph[1]: schema: "), "err: %q", err)
		require.ErrorIs(t, err, fs.ErrNotExist)
		require.Equal(t, pipeline.Result{}, res)
	})

	t.Run("discards diagnostics accumulated before it", func(t *testing.T) {
		dir, cfgPath := writeTwoTargetProject(t)
		writeFixtureFile(t, filepath.Join(dir, "queries", "ghost.cypher"),
			"// name: BadLabel :many\nMATCH (g:Ghost) RETURN g\n")
		require.NoError(t, os.Remove(filepath.Join(dir, "invoices.gql")))

		res, err := pipeline.Run(cfgPath, backendRegistry(t))
		require.Error(t, err)
		require.True(t, strings.HasPrefix(err.Error(), "graph[1]: schema: "), "err: %q", err)
		require.Equal(t, pipeline.Result{}, res)
	})
}

// TestRunDiagnosticsSpanTargets: broken queries in both entries report
// every line, in target-then-file-then-annotation order, each under
// its own entry prefix; Targets is nil (§6.1, §6.2).
func TestRunDiagnosticsSpanTargets(t *testing.T) {
	dir, cfgPath := writeTwoTargetProject(t)
	q0, q1 := filepath.Join(dir, "queries"), filepath.Join(dir, "invoices")
	writeFixtureFile(t, filepath.Join(q0, "ghost.cypher"),
		"// name: BadLabel :many\nMATCH (g:Ghost) RETURN g\n")
	writeFixtureFile(t, filepath.Join(q1, "broken.cypher"),
		"// name: Broken\nMATCH (n) RETURN n\n")
	writeFixtureFile(t, filepath.Join(q1, "wraith.cypher"),
		"// name: BadOne :many\nMATCH (w:Wraith) RETURN w\n// name: BadTwo :many\nMATCH (s:Spectre) RETURN s\n")

	res, err := pipeline.Run(cfgPath, backendRegistry(t))
	require.NoError(t, err)
	require.Nil(t, res.Targets)
	require.Equal(t, []string{
		"graph[0]: " + filepath.Join(q0, "ghost.cypher") + ": query BadLabel: unknown label: Ghost is not declared on any node type",
		"graph[1]: " + filepath.Join(q1, "broken.cypher") + `: malformed query annotation: line 1: "// name: Broken"`,
		"graph[1]: " + filepath.Join(q1, "wraith.cypher") + ": query BadOne: unknown label: Wraith is not declared on any node type",
		"graph[1]: " + filepath.Join(q1, "wraith.cypher") + ": query BadTwo: unknown label: Spectre is not declared on any node type",
	}, res.Diagnostics)
}

// TestRunAllOrNothing: one broken query in entry 1 discards entry 0's
// batch too — any diagnostic anywhere means no target is written
// (§6.2).
func TestRunAllOrNothing(t *testing.T) {
	dir, cfgPath := writeTwoTargetProject(t)
	writeFixtureFile(t, filepath.Join(dir, "invoices", "ghost.cypher"),
		"// name: BadLabel :many\nMATCH (g:Ghost) RETURN g\n")

	res, err := pipeline.Run(cfgPath, backendRegistry(t))
	require.NoError(t, err)
	require.Nil(t, res.Targets, "entry 0's clean batch is discarded with entry 1's")
	require.Len(t, res.Diagnostics, 1)
}

// TestRunSkipsCodegenAfterDiagnostic pins the observable half of §6.1's
// stage-8 skip: entry 1's batch would fail codegen (two files declaring
// the same query name collide in the batch, though neither file is
// malformed), and that error must not displace entry 0's diagnostic in
// a run that is already failing.
func TestRunSkipsCodegenAfterDiagnostic(t *testing.T) {
	dir, cfgPath := writeTwoTargetProject(t)
	writeFixtureFile(t, filepath.Join(dir, "queries", "ghost.cypher"),
		"// name: BadLabel :many\nMATCH (g:Ghost) RETURN g\n")
	writeFixtureFile(t, filepath.Join(dir, "invoices", "dup.cypher"), invoiceQuery)

	res, err := pipeline.Run(cfgPath, backendRegistry(t))
	require.NoError(t, err)
	require.Nil(t, res.Targets)
	require.Equal(t, []string{
		"graph[0]: " + filepath.Join(dir, "queries", "ghost.cypher") + ": query BadLabel: unknown label: Ghost is not declared on any node type",
	}, res.Diagnostics)
}

// TestRunStateIsPerTarget: every front-end value the loop builds —
// the parsed schema, the procsig registry, the query parser and the
// resolver — belongs to one target. Both sub-tests fail if any of
// them is hoisted out of the loop, because each target's queries are
// unresolvable against the other target's state.
func TestRunStateIsPerTarget(t *testing.T) {
	t.Run("schema and resolver", func(t *testing.T) {
		// Entry 1's :Invoice query would fail with "unknown label:
		// Invoice" against entry 0's People schema, and entry 0's
		// :Person query would fail against entry 1's Invoices schema.
		_, cfgPath := writeTwoTargetProject(t)

		res, err := pipeline.Run(cfgPath, backendRegistry(t))
		require.Empty(t, res.Diagnostics)
		require.NoError(t, err)
		require.Len(t, res.Targets, 2)
	})

	t.Run("procsig registry and query parser", func(t *testing.T) {
		// Entry 0 declares a registry holding test.labels; entry 1
		// declares none, so its CALL to the same procedure must fail
		// — it would resolve if entry 0's registry leaked into entry
		// 1's parser.
		dir, cfgPath := writeTwoTargetProject(t)
		writeFixtureFile(t, filepath.Join(dir, "procsig.json"), callRegistry)
		writeFixtureFile(t, filepath.Join(dir, "queries", "calls.cypher"), callQuery)
		writeFixtureFile(t, filepath.Join(dir, "invoices", "calls.cypher"), callQuery)
		writeFixtureFile(t, cfgPath, configOf(
			targetEntry("schema.gql", "queries", "people", "out", "neo4j-go-v5", "procsig.json"),
			targetEntry("invoices.gql", "invoices", "invoicedb", "out2", "neo4j-go-v6", ""),
		))

		res, err := pipeline.Run(cfgPath, backendRegistry(t))
		require.NoError(t, err)
		require.Nil(t, res.Targets)
		require.Len(t, res.Diagnostics, 1)
		require.Contains(t, res.Diagnostics[0],
			"graph[1]: "+filepath.Join(dir, "invoices", "calls.cypher")+": query Labels: unknown procedure")
	})
}
