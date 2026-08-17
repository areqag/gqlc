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

	// uncarriedSchema and uncarriedQuery project a list whose element
	// width the Apache AGE backend has no carrier for. The property
	// lives on a schema of its own rather than on fixtureSchema, because
	// every target shares that one and this property would fail the AGE
	// arm of the driver axis too.
	uncarriedSchema = `CREATE PROPERTY GRAPH TYPE People AS {
    (:Person {
        id     :: INT64 NOT NULL,
        stamps :: LIST<TIMESTAMP>
    })
}
`
	uncarriedQuery = "// name: PersonStamps :one\nMATCH (p:Person) RETURN p.stamps AS stamps\n"

	// unionSchema and unionQuery project an edge that binds to more than
	// one candidate edge type, each carrying a distinct label. Only a
	// relationship-type alternation names several types on one pattern
	// (Cypher.g4 oC_RelationshipTypes), and Apache AGE 1.7.0's parser
	// refuses the alternation, so the query text the emitted code would
	// carry could never reach that server. Three types, so the refusal's
	// label list has to reach past its first two. On a schema of their own
	// for the reason listSchema has one.
	unionSchema = `CREATE PROPERTY GRAPH TYPE People AS {
    (:Person  { id :: INT64 NOT NULL }),
    (:Company { id :: INT64 NOT NULL }),
    (:Person) -[:FOUNDED { foundedYear :: INT64 NOT NULL }]-> (:Company),
    (:Person) -[:BACKED]-> (:Company),
    (:Person) -[:ADVISED]-> (:Company)
}
`
	unionQuery = "// name: GetAction :one\nMATCH (:Person)-[r:FOUNDED|BACKED|ADVISED]->(:Company) RETURN r\n"

	// narrowedSchema and narrowedQuery are the same shape with one of the
	// pattern's relationship types left undeclared. The resolver drops an
	// undeclared type from the candidate set, so the column binds two of
	// the three the author wrote — which is why the refusal names the
	// candidates rather than reconstructing the author's alternation from
	// them.
	narrowedSchema = `CREATE PROPERTY GRAPH TYPE People AS {
    (:Person { id :: INT64 NOT NULL }),
    (:Post   { id :: INT64 NOT NULL }),
    (:Person) -[:AUTHORED { since :: INT64 NOT NULL }]-> (:Post),
    (:Person) -[:LIKES]-> (:Post)
}
`
	narrowedQuery = "// name: GetAction :one\nMATCH (:Person)-[r:AUTHORED|LIKES|FLAGGED]->(:Post) RETURN r\n"

	// sharedLabelSchema and sharedLabelQuery reach an edge column with
	// more than one candidate the other way: one relationship type whose
	// source endpoint the schema satisfies twice (ADR 0022). There is no
	// alternation in the query, Apache AGE parses it, and what defeats it
	// — an edge value carries its label and never its endpoint types — is
	// true of every backend. Here so that the AGE refusal above has to be
	// the answer to a pattern AGE cannot be sent, and not to every
	// multi-candidate column indiscriminately.
	sharedLabelSchema = `CREATE PROPERTY GRAPH TYPE Founders AS {
    (:Person  { id :: INT64 NOT NULL }),
    (:Company { id :: INT64 NOT NULL }),
    NODE TYPE PersonEmployee (:Person&Employee {
        id         :: INT64 NOT NULL,
        employeeId :: INT64 NOT NULL
    }),
    DIRECTED EDGE TYPE FoundedByPerson   (:Person)          -[:FOUNDED { foundedYear :: INT64 NOT NULL }]-> (:Company),
    DIRECTED EDGE TYPE FoundedByEmployee (:Person&Employee) -[:FOUNDED { foundedYear :: INT64 NOT NULL }]-> (:Company)
}
`
	sharedLabelQuery = "// name: GetFounded :one\nMATCH (p:Person)-[r:FOUNDED]-(c:Company) RETURN r\n"

	// soleTypeSchema declares one of the two relationship types the
	// queries written against it name. The resolver drops the undeclared
	// one (internal/resolver, edgeCandidates), so a pattern spelling an
	// alternation collapses to a single candidate and the column it
	// projects is an ordinary edge the AGE backend serves — while the text
	// the generated code would ship still spells the alternation.
	soleTypeSchema = `CREATE PROPERTY GRAPH TYPE People AS {
    (:Person { id :: INT64 NOT NULL }),
    (:Post   { id :: INT64 NOT NULL }),
    (:Person) -[:AUTHORED { since :: INT64 NOT NULL }]-> (:Post)
}
`

	// The five queries below are the ways an author reaches Apache AGE
	// with a relationship-type alternation in the shipped text and no
	// edge-union column anywhere in the row. The two that need a type the
	// schema does not declare are written against soleTypeSchema and the
	// other three against narrowedSchema, which declares both types their
	// patterns name. Each moves one axis:
	//
	//   unprojected      — every named type is declared; the query
	//                      projects something else, so there is no edge
	//                      column at all.
	//   narrowedToOne    — one named type is undeclared; the survivor is
	//                      a single candidate, so the edge column the
	//                      query does project is a plain ResolvedEdge.
	//   narrowedUnproj   — both at once.
	//   reboundNarrowed  — every named type is declared, but a second
	//                      occurrence of the same variable intersects the
	//                      binding down to one type (internal/query/
	//                      cypher, mergeBinding), so the model carries one
	//                      label and the text carries two.
	//   exec             — a write projecting nothing, so the resolved
	//                      column list is EMPTY. Every row above still
	//                      resolves SOME column; this is the only one
	//                      where a gate reading the columns, or one
	//                      reading the statement kind, has nothing in
	//                      front of it at all.
	unprojectedAlternationQuery = "// name: PostIDs :one\n" +
		"MATCH (:Person)-[r:AUTHORED|LIKES]->(p:Post) RETURN p.id\n"
	narrowedToOneAlternationQuery = "// name: GetAction :one\n" +
		"MATCH (:Person)-[r:AUTHORED|FLAGGED]->(p:Post) RETURN r\n"
	narrowedUnprojectedAlternationQuery = "// name: PostIDs :one\n" +
		"MATCH (:Person)-[r:AUTHORED|FLAGGED]->(p:Post) RETURN p.id\n"
	reboundNarrowedAlternationQuery = "// name: GetAction :one\n" +
		"MATCH (:Person)-[r:AUTHORED|LIKES]->(p:Post), (:Person)-[r:AUTHORED]->(p:Post) RETURN r\n"
	execAlternationQuery = "// name: DropActions :exec\n" +
		"MATCH (p:Person)-[r:AUTHORED|LIKES]->(:Post) DELETE r\n"

	// The three below are spellings rather than shapes: each is a text
	// the grammar accepts, the front end resolves and the emitted code
	// would ship, whose '|' a gate reading the resolved model cannot see.
	//
	//   repeatedType     — the alternation names ONE type twice. A label
	//                      set is a set, so the binding carries one label
	//                      and the column is a plain ResolvedEdge the AGE
	//                      backend serves; the '|' the repeat needs is
	//                      still the character AGE 1.7.0 refuses. A gate
	//                      keyed on DISTINCT type names reports nothing.
	//   repeatedTypeLegacy — the same repeat with the optional colon on
	//                      the second name, which the grammar reads as
	//                      the same two names.
	//   spaced           — SP around the '|'. Pinned because the refusal
	//                      quotes the parser's own text, and the spaced
	//                      rendering is the one place that claim is
	//                      checkable.
	repeatedTypeAlternationQuery = "// name: GetAction :one\n" +
		"MATCH (:Person)-[r:LIKES|LIKES]->(p:Post) RETURN r\n"
	repeatedTypeLegacyAlternationQuery = "// name: GetAction :one\n" +
		"MATCH (:Person)-[r:LIKES|:LIKES]->(p:Post) RETURN r\n"
	spacedAlternationQuery = "// name: PostIDs :one\n" +
		"MATCH (:Person)-[r:AUTHORED | LIKES]->(p:Post) RETURN p.id\n"

	// constructorSchema carries every property the constructor rows read
	// or write, so the front end resolves each of them for real and what
	// answers is the dialect gate rather than an unknown property. The
	// property literally named `datetime` is the false positive a scan
	// for the name would take.
	constructorSchema = `CREATE PROPERTY GRAPH TYPE People AS {
    (:Person {
        id       :: INT64 NOT NULL,
        seenAt   :: INT64 NOT NULL,
        seen     :: INT64,
        toTimestamp :: INT64
    })
}
`
	// The three refused shapes. Each reaches the server with a call AGE
	// has no function for, and the first two reach the resolved columns
	// with nothing to refuse.
	//
	//   predicate — the query model drops predicate structure (ADR 0003),
	//               so the call leaves no column, parameter or binding.
	//   exec      — a write projects nothing at all and ships its whole
	//               text.
	//   projected — the one shape a column CAN see, and so the ordering
	//               row: codegen.Prepare has no carrier for the temporal
	//               column either, and which refusal the author gets is
	//               what says where this gate sits.
	predicateConstructorQuery = "// name: Recent :one\n" +
		"MATCH (p:Person) WHERE p.seenAt < datetime() RETURN p.id AS id\n"
	execConstructorQuery = "// name: Touch :exec\n" +
		"MATCH (p:Person) SET p.seen = localdatetime()\n"
	projectedConstructorQuery = "// name: SeenOn :one\n" +
		"MATCH (p:Person) RETURN p.id AS id, date() AS seenOn\n"

	// The served shapes, which are the bound on all three. A refusal that
	// is wrong costs the author a query that would have worked and ADR
	// 0005 leaves them no rewrite, so what this gate does NOT refuse is
	// the half worth asserting at the seam.
	//
	//   unwitnessed — localtime() is every bit as suspect as datetime()
	//                 and no session has run it, so it is not in the
	//                 table and must generate (bd gqlc-osf1).
	//   namespaced  — Cypher.g4 §oC_FunctionName is `oC_Namespace
	//                 oC_SymbolicName`, so duration.between is a
	//                 different name from the probed `duration`. Two
	//                 spellings, because only one of them can fail: drop
	//                 the namespace guard and `duration.between` reports
	//                 "between", which no probe put in the catalogue, so
	//                 nothing is refused either way. `com.example.
	//                 datetime()` reports "datetime", which IS in it, and
	//                 the author is refused a call to a function they
	//                 defined on the strength of a probe that measured
	//                 another name.
	//   property    — p.toTimestamp spells a refused name and calls
	//                 nothing. Not `datetime`, because GQL reserves that
	//                 word and no schema can declare a property with it;
	//                 `toTimestamp` is a catalogue name the schema
	//                 grammar does admit.
	unwitnessedConstructorQuery = "// name: Clock :one\n" +
		"MATCH (p:Person) WHERE p.seenAt < localtime() RETURN p.id AS id\n"
	namespacedConstructorQuery = "// name: Between :one\n" +
		"MATCH (p:Person) WHERE duration.between(p.seenAt, p.seenAt) > 0 RETURN p.id AS id\n"
	namespacedRefusedNameQuery = "// name: Recent :one\n" +
		"MATCH (p:Person) WHERE p.seenAt < com.example.datetime() RETURN p.id AS id\n"
	constructorNamedPropertyQuery = "// name: Stamps :one\n" +
		"MATCH (p:Person) RETURN p.toTimestamp AS stamp\n"

	// wideColumnAlternationSchema declares narrowedSchema's two
	// relationship types over a Post carrying a BYTES property, so one
	// query can trip the text gate and an unserved COLUMN at once. BYTES
	// is the width because agtype has no carrier for it and never will —
	// a list, by contrast, rides its element's carrier on this backend
	// and so is served. Which of the two refusals the author is told is
	// what TestRunApacheAgeAnswersAnAlternationAheadOfOtherColumnRefusals
	// turns on.
	wideColumnAlternationSchema = `CREATE PROPERTY GRAPH TYPE People AS {
    (:Person { id :: INT64 NOT NULL }),
    (:Post   {
        id      :: INT64 NOT NULL,
        payload :: BYTES
    }),
    (:Person) -[:AUTHORED { since :: INT64 NOT NULL }]-> (:Post),
    (:Person) -[:LIKES]-> (:Post)
}
`
	wideColumnAlternationQuery = "// name: PostPayload :one\n" +
		"MATCH (:Person)-[r:AUTHORED|LIKES]->(p:Post) RETURN p.payload AS payload\n"
	wideColumnNoAlternationQuery = "// name: PostPayload :one\n" +
		"MATCH (:Person)-[r:AUTHORED]->(p:Post) RETURN p.payload AS payload\n"

	// sharedLabelAlternationSchema and sharedLabelAlternationQuery are a
	// verbatim copy of test/data/codegen/invalid/
	// unrepresentable_edge_union_shared_label, which this branch
	// un-enrolled from apache-age-pgx-v5. They trip two refusals at once:
	// the text spells `|`, and the column's three candidates carry two
	// labels, LIKES twice — which the AGE column gate stands aside on and
	// codegen.Prepare answers with the portable
	// ErrUnrepresentableEdgeUnion the manifest names. Which of the two an
	// author gets is decided by where the text gate sits relative to
	// Prepare, and the un-enrolment is only correct on one of the two
	// orderings. Here so that the ordering is asserted rather than assumed
	// by a fixture that no longer runs.
	sharedLabelAlternationSchema = `CREATE PROPERTY GRAPH TYPE UnrepresentableEdgeUnionSharedLabel AS {
    (:Person { id :: INT64 NOT NULL }),
    (:Post   { id :: INT64 NOT NULL }),
    DIRECTED EDGE TYPE LikesFwd (:Person) -[:LIKES { since  :: INT64 NOT NULL }]-> (:Post),
    DIRECTED EDGE TYPE LikesRev (:Post)   -[:LIKES { weight :: INT64 NOT NULL }]-> (:Person),
    DIRECTED EDGE TYPE Wrote    (:Person) -[:WROTE { written :: INT64 NOT NULL }]-> (:Post)
}
`
	sharedLabelAlternationQuery = "// name: GetAction :one\n" +
		"MATCH (x:Person)-[r:LIKES|WROTE]-(y:Post) RETURN r\n"

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
	writeFixtureFile(t, filepath.Join(dir, "schema.gql"), uncarriedSchema)
	writeFixtureFile(t, filepath.Join(dir, "queries", "people.cypher"), uncarriedQuery)
	writeFixtureFile(t, cfgPath, configYAML("people", string(config.DriverApacheAgePgxV5), ""))

	res, err := pipeline.Run(cfgPath, backendRegistry(t))
	require.ErrorIs(t, err, age.ErrUnsupportedQuery)
	require.ErrorContains(t, err, "graph[0]: unsupported query: ")
	require.ErrorContains(t, err, `1 query would be dropped: PersonStamps (column "stamps" projects property:LIST<TIMESTAMP>)`)
	require.Equal(t, pipeline.Result{}, res)
}

// TestRunApacheAgeRefusesEdgeUnions pins the other half of that
// vocabulary at the same seam, for the one column kind whose refusal is
// a property of the server's parser rather than of the wire format.
//
// An edge column whose candidates carry distinct labels is reachable in
// openCypher only through a relationship-type alternation, and Apache
// AGE 1.7.0 answers `-[r:FOUNDED|BACKED]->` with `ERROR: syntax error at
// or near "|"` (SQLSTATE 42601), measured against the image
// test/data/codegen pins. Generated code runs the author's query text
// verbatim (ADR 0005), so emitting for this column would hand back a
// package that compiles and whose every call fails at the server.
// Refusing at generation is what turns that into an answer the author
// gets before they ship.
//
// The whole message is asserted, not a substring of it, because the
// defect this replaced was in what the sentence claimed rather than in
// which query it fired on: it told an author who wrote no '|' that their
// query was an alternation.
//
// The rows run the front end for real, so the labels the message carries
// are the ones the resolver committed for the author's pattern. The
// second row is where those two things come apart: a pattern may name a
// relationship type the schema does not declare, the resolver drops it
// from the candidate set, and a message reconstructing ":A|B" from the
// survivors quotes an alternation nobody wrote. Its labels also differ
// from the first row's, so a message built from a fixed list fails one
// of them.
func TestRunApacheAgeRefusesEdgeUnions(t *testing.T) {
	for _, tc := range []struct {
		name   string
		schema string
		query  string
		// names is the prose label list the refusal must carry, and is
		// the discriminator in both rows: it is asserted inside the
		// whole message, so a gate that stopped reading the column's own
		// candidates cannot satisfy both.
		names string
		// absent is a relationship type the pattern names and the
		// message must not. It is a guard against a FUTURE message
		// quoting the author's alternation back, not a witness against
		// the one this replaced — that sentence named no type at all, so
		// it would have passed this too. What caught it is names.
		absent string
	}{
		{
			name:   "every candidate the column binds is named",
			schema: unionSchema,
			query:  unionQuery,
			names:  "FOUNDED, BACKED and ADVISED",
		},
		{
			name:   "a relationship type the schema does not declare is not among them",
			schema: narrowedSchema,
			query:  narrowedQuery,
			names:  "AUTHORED and LIKES",
			absent: "FLAGGED",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir, cfgPath := writeProject(t)
			writeFixtureFile(t, filepath.Join(dir, "schema.gql"), tc.schema)
			writeFixtureFile(t, filepath.Join(dir, "queries", "people.cypher"), tc.query)
			writeFixtureFile(t, cfgPath, configYAML("people", string(config.DriverApacheAgePgxV5), ""))

			res, err := pipeline.Run(cfgPath, backendRegistry(t))
			require.ErrorIs(t, err, age.ErrUnsupportedQuery)
			require.ErrorContains(t, err, `1 query would be dropped: GetAction (column "r" `+
				`binds more than one relationship type — `+tc.names+`, the candidates the schema `+
				`declares for its pattern — which openCypher spells only as an alternation, and `+
				`Apache AGE 1.7.0's parser has no "|" in a relationship pattern: it answers one `+
				`with "syntax error at or near \"|\"" (SQLSTATE 42601))`)
			require.Equal(t, pipeline.Result{}, res)
			if tc.absent != "" {
				require.NotContains(t, err.Error(), tc.absent,
					"the message must not name a type the column does not bind; the missing diagnostic for the drop is gqlc-1dmu")
			}

			// The same project on a driver whose server parses the
			// alternation generates, so what failed above is this backend's
			// answer and not the schema, the query or the resolver.
			writeFixtureFile(t, cfgPath, configYAML("people", string(config.DriverNeo4jGoV5), ""))
			res, err = pipeline.Run(cfgPath, backendRegistry(t))
			require.NoError(t, err)
			require.Empty(t, res.Diagnostics)
		})
	}
}

// TestRunApacheAgeRefusesRelationshipTypeAlternation pins the gate that
// reads the query TEXT, at the same seam and through the same front end.
//
// The refusal above keys on the resolved column, and the hazard is not a
// property of the column. Apache AGE 1.7.0's parser has no '|' in a
// relationship detail whatever surrounds it, and generated code runs the
// author's query text verbatim (ADR 0005) — so every row below reaches
// that server with a '|' in the statement while reaching the column gate
// with nothing to refuse. Each was `gqlc generate` exiting 0 over a
// package whose every call is SQLSTATE 42601.
//
// The rows are the axes on which the text and the resolved columns come
// apart. An alternation the author does project but does not RETURN
// still makes some other column; one the resolver narrowed to a single
// candidate makes a plain edge column; and a :exec write makes no column
// whatsoever. The narrowing has two causes — a type the schema does not
// declare is dropped (internal/resolver, edgeCandidates) and a re-bound
// relationship variable's occurrences intersect (internal/query/cypher,
// mergeBinding) — so each gets a row, and one row moves both axes at
// once.
//
// They are not claimed to be every text an author could write. What they
// are is one row for each way the gate could be narrowed and stay green
// on the others: to queries that project, to queries that read, and to
// queries whose columns hold an edge union.
//
// The whole message is asserted, so the alternation it quotes has to be
// the text the author wrote rather than anything rebuilt from the
// candidates that survived.
func TestRunApacheAgeRefusesRelationshipTypeAlternation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		schema string
		query  string
		// dropped is the "<Name> (<alternations>)" list the refusal ends
		// with, written out per case rather than derived.
		dropped string
	}{
		{
			name:    "an alternation the query never projects",
			schema:  narrowedSchema,
			query:   unprojectedAlternationQuery,
			dropped: `PostIDs (":AUTHORED|LIKES")`,
		},
		{
			name:    "an alternation the resolver narrowed to one declared candidate",
			schema:  soleTypeSchema,
			query:   narrowedToOneAlternationQuery,
			dropped: `GetAction (":AUTHORED|FLAGGED")`,
		},
		{
			name:    "both at once",
			schema:  soleTypeSchema,
			query:   narrowedUnprojectedAlternationQuery,
			dropped: `PostIDs (":AUTHORED|FLAGGED")`,
		},
		{
			name:    "an alternation a re-bound variable narrowed to one type",
			schema:  narrowedSchema,
			query:   reboundNarrowedAlternationQuery,
			dropped: `GetAction (":AUTHORED|LIKES")`,
		},
		{
			// The zero-column row: a :exec write projects nothing, so
			// there is no resolved column here for any reading of the
			// columns to arrive at, and the whole statement still reaches
			// the server. Without it, a gate skipping column-less or
			// write queries keeps every row above green while leaving
			// `gqlc generate` exiting 0 over a DELETE that is SQLSTATE
			// 42601 on every call.
			name:    "an alternation in a write that projects nothing",
			schema:  narrowedSchema,
			query:   execAlternationQuery,
			dropped: `DropActions (":AUTHORED|LIKES")`,
		},
		{
			// The arity row, and the one the front end has to be run for
			// to be worth anything. `-[r:LIKES|LIKES]->` spells the '|'
			// AGE 1.7.0 refuses, and a label set is a set — so the
			// resolver hands the column gate a single ResolvedEdge it
			// serves, and every gate downstream of the parse is blind.
			// A scan counting DISTINCT type names instead of
			// oC_RelTypeName productions leaves this generating cleanly.
			name:    "an alternation naming one type twice",
			schema:  narrowedSchema,
			query:   repeatedTypeAlternationQuery,
			dropped: `GetAction (":LIKES|LIKES")`,
		},
		{
			name:    "the same repeat in the legacy spelling",
			schema:  narrowedSchema,
			query:   repeatedTypeLegacyAlternationQuery,
			dropped: `GetAction (":LIKES|:LIKES")`,
		},
		{
			// The spaced rendering. SP is a default-channel token, so
			// the quoted text keeps it and the message prints only
			// characters the author wrote. Unprojected so that what
			// answers is this gate and not the column.
			name:    "an alternation written with spaces around the '|'",
			schema:  narrowedSchema,
			query:   spacedAlternationQuery,
			dropped: `PostIDs (":AUTHORED | LIKES")`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir, cfgPath := writeProject(t)
			writeFixtureFile(t, filepath.Join(dir, "schema.gql"), tc.schema)
			writeFixtureFile(t, filepath.Join(dir, "queries", "people.cypher"), tc.query)
			writeFixtureFile(t, cfgPath, configYAML("people", string(config.DriverApacheAgePgxV5), ""))

			res, err := pipeline.Run(cfgPath, backendRegistry(t))
			require.ErrorIs(t, err, age.ErrRelationshipTypeAlternation)
			require.NotErrorIs(t, err, age.ErrUnsupportedQuery,
				"the text refusal and the column refusal have different fixes and must be tellable apart")
			require.EqualError(t, err, "graph[0]: relationship type alternation: generated code runs "+
				"the author's query text verbatim (ADR 0005) and Apache AGE 1.7.0's parser has no "+
				`"|" in a relationship pattern, so every call on 1 query would answer `+
				`"syntax error at or near \"|\"" (SQLSTATE 42601) — write each relationship type as `+
				"its own query: "+tc.dropped)
			require.Equal(t, pipeline.Result{}, res)

			// The same project on a driver whose server parses the
			// alternation generates, so what failed above is this backend's
			// answer and not the schema, the query or the resolver.
			writeFixtureFile(t, cfgPath, configYAML("people", string(config.DriverNeo4jGoV5), ""))
			res, err = pipeline.Run(cfgPath, backendRegistry(t))
			require.NoError(t, err)
			require.Empty(t, res.Diagnostics)
		})
	}
}

// TestRunApacheAgeRefusesUndefinedFunctions pins the gap table's second
// entry at the same seam, and it is the entry that needed the seam most.
//
// Every other test of this gate hands the backend a codegen.NamedQuery
// built by hand, with a SourceText and a column list written beside each
// other. That is enough to test the gate and not enough to test that an
// author ever reaches it: a query calling a function nothing declares
// has several earlier places it could die — the parse, the resolver's
// typing of the call, the property lookups around it — and if it died in
// any of them, the whole undefined-function gap would be machinery
// guarding a door nobody walks through. These rows run the real front
// end over files on disk, so reaching the refusal is part of the claim.
//
// The refused rows are one per way a call escapes the resolved model:
// a predicate the query model drops (ADR 0003), a write that projects
// nothing, and a projection — which is also the ordering row, since
// codegen.Prepare would answer that same column with the portable
// ErrUnrepresentableTemporal if this gate ran later.
//
// The served rows are the half the asymmetry makes important. A missing
// refusal costs the author a runtime error they were getting anyway; a
// wrong one costs them a working query, and ADR 0005 leaves no rewrite,
// no escape hatch and no flag. So an unwitnessed constructor, a
// namespaced call and a property spelling a constructor's name all have
// to generate, at the seam, against this backend.
func TestRunApacheAgeRefusesUndefinedFunctions(t *testing.T) {
	for _, tc := range []struct {
		name  string
		query string
		// dropped is the "<Name> (<names>)" list the refusal ends with,
		// written out per row rather than derived.
		dropped string
		// portable is the shared refusal this gate must answer ahead of,
		// or nil where no other gate applies to the row.
		portable error
	}{
		{
			name:    "a constructor in a predicate reaches no column",
			query:   predicateConstructorQuery,
			dropped: `Recent ("datetime")`,
		},
		{
			name:    "a constructor in a write that projects nothing",
			query:   execConstructorQuery,
			dropped: `Touch ("localdatetime")`,
		},
		{
			name:     "a projected constructor is answered here, ahead of the carrier",
			query:    projectedConstructorQuery,
			dropped:  `SeenOn ("date")`,
			portable: codegen.ErrUnrepresentableTemporal,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir, cfgPath := writeProject(t)
			writeFixtureFile(t, filepath.Join(dir, "schema.gql"), constructorSchema)
			writeFixtureFile(t, filepath.Join(dir, "queries", "people.cypher"), tc.query)
			writeFixtureFile(t, cfgPath, configYAML("people", string(config.DriverApacheAgePgxV5), ""))

			res, err := pipeline.Run(cfgPath, backendRegistry(t))
			require.ErrorIs(t, err, age.ErrUndefinedFunction)
			require.NotErrorIs(t, err, age.ErrRelationshipTypeAlternation,
				"two gaps with two fixes, and a caller must be able to tell them apart")
			require.NotErrorIs(t, err, age.ErrUnsupportedQuery)
			if tc.portable != nil {
				require.NotErrorIs(t, err, tc.portable,
					"the carrier is not yet the obstacle: the statement never parses on this server")
			}
			require.EqualError(t, err, "graph[0]: undefined function: generated code runs the author's "+
				"query text verbatim (ADR 0005) and Apache AGE 1.7.0 defines no temporal constructor at "+
				`all, so every call on 1 query would answer "function <name> does not exist" — AGE's `+
				"whole temporal surface is timestamp(), which returns epoch milliseconds as an integer, "+
				"so compute the value in Go and bind it as a parameter, or generate against a neo4j "+
				"target: "+tc.dropped)
			require.Equal(t, pipeline.Result{}, res)

			// The same project on a driver whose server defines the
			// constructor generates, so what failed above is this backend's
			// answer and not the schema, the query or the resolver.
			writeFixtureFile(t, cfgPath, configYAML("people", string(config.DriverNeo4jGoV5), ""))
			res, err = pipeline.Run(cfgPath, backendRegistry(t))
			require.NoError(t, err)
			require.Empty(t, res.Diagnostics)
		})
	}

	for _, tc := range []struct {
		name  string
		query string
	}{
		{"an unwitnessed constructor is not refused", unwitnessedConstructorQuery},
		{"a namespaced call whose namespace is a refused name", namespacedConstructorQuery},
		{"a namespaced call whose symbolic name is a refused name", namespacedRefusedNameQuery},
		{"a property named like a constructor is not a call", constructorNamedPropertyQuery},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir, cfgPath := writeProject(t)
			writeFixtureFile(t, filepath.Join(dir, "schema.gql"), constructorSchema)
			writeFixtureFile(t, filepath.Join(dir, "queries", "people.cypher"), tc.query)
			writeFixtureFile(t, cfgPath, configYAML("people", string(config.DriverApacheAgePgxV5), ""))

			res, err := pipeline.Run(cfgPath, backendRegistry(t))
			require.NoError(t, err, "a suspicion is not a witness, and this backend must not refuse on one")
			require.Empty(t, res.Diagnostics)
		})
	}
}

// TestRunApacheAgeAnswersAnAlternationAheadOfOtherColumnRefusals pins
// the other half of the text gate's position: which unserved COLUMNS
// outrank it, and which do not.
//
// One does. TestRunApacheAgeRefusesEdgeUnions above sends AGE a pattern
// spelling '|' and gets the edge-union column's answer, because that
// answer names the candidates the schema declares for the pattern — more
// than the text can say about the same defect.
//
// Nothing else does, and that is what this asserts. A column of a width
// agtype cannot carry is a different defect from an unparseable
// statement. Answering it first costs the author two rounds on one
// query: change the projection, regenerate, and only then find out the
// query never parsed on this backend. The alternation is the obstacle
// underneath — the statement has to be rewritten before any projection
// can be put to this server — so it is the answer.
//
// The second half is the discriminator and is not optional. Strip the
// '|' from the same query against the same schema and the same column is
// still unserved; if the width refusal does not come back, what the
// first half measured was a column arm that stopped firing rather than a
// gate order.
func TestRunApacheAgeAnswersAnAlternationAheadOfOtherColumnRefusals(t *testing.T) {
	dir, cfgPath := writeProject(t)
	writeFixtureFile(t, filepath.Join(dir, "schema.gql"), wideColumnAlternationSchema)
	writeFixtureFile(t, cfgPath, configYAML("people", string(config.DriverApacheAgePgxV5), ""))

	queryPath := filepath.Join(dir, "queries", "people.cypher")
	writeFixtureFile(t, queryPath, wideColumnAlternationQuery)

	res, err := pipeline.Run(cfgPath, backendRegistry(t))
	require.ErrorIs(t, err, age.ErrRelationshipTypeAlternation,
		"the column gate yields to the text on every reason but the edge union, so the author is not sent to fix a projection first")
	require.NotErrorIs(t, err, age.ErrUnsupportedQuery)
	require.ErrorContains(t, err, `PostPayload (":AUTHORED|LIKES")`)
	require.NotContains(t, err.Error(), "property:BYTES",
		"the projection is not the obstacle yet; the statement is")
	require.Equal(t, pipeline.Result{}, res)

	writeFixtureFile(t, queryPath, wideColumnNoAlternationQuery)

	res, err = pipeline.Run(cfgPath, backendRegistry(t))
	require.ErrorIs(t, err, age.ErrUnsupportedQuery,
		"the same column with no '|' in the text is still the column gate's, so the yield above is an ordering and not a hole in the width arm")
	require.NotErrorIs(t, err, age.ErrRelationshipTypeAlternation)
	require.ErrorContains(t, err, `PostPayload (column "payload" projects property:BYTES)`)
	require.Equal(t, pipeline.Result{}, res)
}

// TestRunApacheAgeAnswersAnAlternationAheadOfSharedAdmission pins where
// the text gate sits relative to codegen.Prepare, by the only thing that
// makes the position observable: which of two applicable refusals an
// author gets.
//
// Its input is a verbatim copy of test/data/codegen/invalid/
// unrepresentable_edge_union_shared_label, and it trips both. The text
// spells `-[r:LIKES|WROTE]-`, so the text gate applies. The column binds
// three candidates carrying two labels — LikesFwd, LikesRev and Wrote —
// so the AGE column gate stands aside on the repeat and Prepare applies,
// with the portable ErrUnrepresentableEdgeUnion. The text gate runs
// ahead of Prepare, so the alternation is the answer, and that is the
// right one: the statement has to be rewritten before the column
// question can be put to this server at all.
//
// This is what the corpus un-enrolment rests on. A manifest names one
// expectedError for every target it enrols, so that fixture can carry an
// apache-age-pgx-v5 target only while AGE's answer is the portable
// sentinel — which is to say only on the other ordering. Moving the gate
// below Prepare makes the deleted enrolment valid again, and nothing
// else in the tree notices. Asserted here so the deletion is justified
// by something that runs.
//
// Both targets are asserted, because the claim has two halves: AGE gives
// the sentinel the manifest cannot name, and neo4j-go-v5 — the target
// the manifest still enrols — gives the one it does.
func TestRunApacheAgeAnswersAnAlternationAheadOfSharedAdmission(t *testing.T) {
	dir, cfgPath := writeProject(t)
	writeFixtureFile(t, filepath.Join(dir, "schema.gql"), sharedLabelAlternationSchema)
	writeFixtureFile(t, filepath.Join(dir, "queries", "people.cypher"), sharedLabelAlternationQuery)
	writeFixtureFile(t, cfgPath, configYAML("people", string(config.DriverApacheAgePgxV5), ""))

	res, err := pipeline.Run(cfgPath, backendRegistry(t))
	require.ErrorIs(t, err, age.ErrRelationshipTypeAlternation,
		"the text gate runs ahead of Prepare, so the statement the server cannot parse is the answer")
	require.NotErrorIs(t, err, codegen.ErrUnrepresentableEdgeUnion,
		"reaching shared admission first would give this fixture the sentinel its manifest names, and its apache-age-pgx-v5 enrolment would be correct after all")
	require.NotErrorIs(t, err, age.ErrUnsupportedQuery)
	require.ErrorContains(t, err, `GetAction (":LIKES|WROTE")`)
	require.Equal(t, pipeline.Result{}, res)

	writeFixtureFile(t, cfgPath, configYAML("people", string(config.DriverNeo4jGoV5), ""))
	res, err = pipeline.Run(cfgPath, backendRegistry(t))
	require.ErrorIs(t, err, codegen.ErrUnrepresentableEdgeUnion,
		"the enrolment the manifest keeps: a server that parses the alternation still cannot tell two LIKES candidates apart")
	require.NotErrorIs(t, err, age.ErrRelationshipTypeAlternation)
	require.Equal(t, pipeline.Result{}, res)
}

// TestRunApacheAgeLeavesSharedLabelUnionsToSharedAdmission is the bound
// on both refusals above. A multi-candidate edge column is reachable
// without any alternation — one relationship type whose endpoint the
// schema satisfies twice (ADR 0022) — and Apache AGE parses that
// statement. So the query must fail on what is actually wrong with it,
// in the same words every other backend uses, and the author must not be
// told their query contains a '|' it does not contain.
//
// The column gate stands aside on the repeated label rather than on the
// absent '|', and the text gate answers instead whenever the query does
// spell one — which is why the corpus witnesses this class through
// invalid/plural_endpoint_edge_union_shared_label, whose pattern names a
// single relationship type. This query is that same no-alternation shape,
// which is the shape whose message can be checked for the word.
//
// Asserted against neo4j-go-v5 as well as AGE: the two must give the
// same answer, because the obstacle is a property of what a Go edge value
// can carry and not of any server.
func TestRunApacheAgeLeavesSharedLabelUnionsToSharedAdmission(t *testing.T) {
	dir, cfgPath := writeProject(t)
	writeFixtureFile(t, filepath.Join(dir, "schema.gql"), sharedLabelSchema)
	writeFixtureFile(t, filepath.Join(dir, "queries", "people.cypher"), sharedLabelQuery)

	for _, driver := range []config.Driver{config.DriverApacheAgePgxV5, config.DriverNeo4jGoV5} {
		t.Run(string(driver), func(t *testing.T) {
			writeFixtureFile(t, cfgPath, configYAML("people", string(driver), ""))

			res, err := pipeline.Run(cfgPath, backendRegistry(t))
			require.ErrorIs(t, err, codegen.ErrUnrepresentableEdgeUnion)
			require.NotErrorIs(t, err, age.ErrUnsupportedQuery)
			require.ErrorContains(t, err, `both carry edge label "FOUNDED"`)
			require.NotContains(t, err.Error(), "alternation",
				"the query names one relationship type and spells no '|'")
			require.Equal(t, pipeline.Result{}, res)
		})
	}
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
