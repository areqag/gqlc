package config_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/areqag/gqlc/internal/config"
)

// canonicalPath is the sole on-disk fixture; it pins the canonical Save
// form (multi-target spec §5), so any drift in the emitted bytes (a
// re-ordered key, a quoting change, a lost sequence indent) fails
// TestSaveEmitsFixtureBytes.
const canonicalPath = "testdata/canonical.gqlc.yaml"

// validDoc mirrors the canonical fixture byte-for-byte: two targets,
// one with procsig and one without, a different driver each.
const validDoc = `version: 1
graph:
  - schema: schema.gql
    schema_language: gql
    queries: internal/user/query
    query_language: opencypher
    procsig: procs.procsig.json
    gen:
      go:
        package: userdb
        out: internal/user/gen
        driver: neo4j-go-v5
  - schema: schema.gql
    schema_language: gql
    queries: internal/order/query
    query_language: opencypher
    gen:
      go:
        package: orderdb
        out: internal/order/gen
        driver: neo4j-go-v6
`

// canonicalConfig is the in-memory equivalent of validDoc / the fixture.
var canonicalConfig = config.Config{Targets: []config.Target{
	{
		SchemaPath:  "schema.gql",
		SchemaLang:  config.SchemaLangGQL,
		QueryDir:    "internal/user/query",
		QueryLang:   config.QueryLangOpenCypher,
		ProcsigPath: "procs.procsig.json",
		Go: config.GoGen{
			Package: "userdb",
			Out:     "internal/user/gen",
			Driver:  config.DriverNeo4jGoV5,
		},
	},
	{
		SchemaPath: "schema.gql",
		SchemaLang: config.SchemaLangGQL,
		QueryDir:   "internal/order/query",
		QueryLang:  config.QueryLangOpenCypher,
		Go: config.GoGen{
			Package: "orderdb",
			Out:     "internal/order/gen",
			Driver:  config.DriverNeo4jGoV6,
		},
	},
}}

// oneEntry is the single-target document the rejection table derives its
// malformed variants from. The sequence marker sits on its own line so
// every key line is uniform and dropEntryKey never has to reattach it;
// key lines are therefore version=1, graph=2, "-"=3, schema=4,
// schema_language=5, queries=6, query_language=7, procsig=8, gen=9,
// go=10, package=11, out=12, driver=13.
const oneEntry = `version: 1
graph:
  -
    schema: schema.gql
    schema_language: gql
    queries: queries/
    query_language: opencypher
    procsig: procs.procsig.json
    gen:
      go:
        package: db
        out: internal/db
        driver: neo4j-go-v5
`

// oneEntryConfig is the in-memory equivalent of oneEntry.
var oneEntryConfig = config.Config{Targets: []config.Target{{
	SchemaPath:  "schema.gql",
	SchemaLang:  config.SchemaLangGQL,
	QueryDir:    "queries/",
	QueryLang:   config.QueryLangOpenCypher,
	ProcsigPath: "procs.procsig.json",
	Go: config.GoGen{
		Package: "db",
		Out:     "internal/db",
		Driver:  config.DriverNeo4jGoV5,
	},
}}}

// hasEntryKey reports whether line declares key at the entry level;
// matching the colon keeps "schema" from also matching
// "schema_language".
func hasEntryKey(line, key string) bool {
	return strings.HasPrefix(strings.TrimLeft(line, " "), key+":")
}

// dropEntryKey returns oneEntry without the entry's line for key.
func dropEntryKey(key string) string {
	var out []string
	for line := range strings.Lines(oneEntry) {
		if hasEntryKey(line, key) {
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "")
}

// setEntryKey returns oneEntry with key's scalar value replaced by
// value, preserving the line's indentation.
func setEntryKey(key, value string) string {
	var out []string
	for line := range strings.Lines(oneEntry) {
		if hasEntryKey(line, key) {
			indent := line[:len(line)-len(strings.TrimLeft(line, " "))]
			line = indent + key + ": " + value + "\n"
		}
		out = append(out, line)
	}
	return strings.Join(out, "")
}

// twoEntryDoc renders two targets differing only in their output
// directory — the document the cross-entry overlap sweep judges.
func twoEntryDoc(outA, outB string) string {
	entry := func(pkg, out string) string {
		return "  -\n" +
			"    schema: schema.gql\n" +
			"    schema_language: gql\n" +
			"    queries: queries\n" +
			"    query_language: opencypher\n" +
			"    gen:\n" +
			"      go:\n" +
			"        package: " + pkg + "\n" +
			"        out: '" + out + "'\n" +
			"        driver: neo4j-go-v5\n"
	}
	return "version: 1\ngraph:\n" + entry("a", outA) + entry("b", outB)
}

// TestDefaultFilename pins the canonical name: no .yml/.json variants,
// no search logic (config-file-format §2).
func TestDefaultFilename(t *testing.T) {
	if config.DefaultFilename != "gqlc.yaml" {
		t.Fatalf("DefaultFilename = %q; want %q", config.DefaultFilename, "gqlc.yaml")
	}
}

// TestEnumValues locks each axis vocabulary. Error messages, `gqlc init`
// prompts and CheckOutAgainst's callers all derive from these slices, so
// growing an axis must be a deliberate, test-visible change.
func TestEnumValues(t *testing.T) {
	if got, want := config.SchemaLangValues(), []config.SchemaLang{config.SchemaLangGQL}; !slices.Equal(got, want) {
		t.Errorf("SchemaLangValues() = %v; want %v", got, want)
	}
	if got, want := config.QueryLangValues(), []config.QueryLang{config.QueryLangOpenCypher}; !slices.Equal(got, want) {
		t.Errorf("QueryLangValues() = %v; want %v", got, want)
	}
	if got, want := config.DriverValues(), []config.Driver{config.DriverNeo4jGoV5, config.DriverNeo4jGoV6}; !slices.Equal(got, want) {
		t.Errorf("DriverValues() = %v; want %v", got, want)
	}
}

// wantConfig fails t unless got equals want target for target.
func wantConfig(t *testing.T, got, want config.Config) {
	t.Helper()
	if len(got.Targets) != len(want.Targets) {
		t.Fatalf("got %d targets, want %d: %+v", len(got.Targets), len(want.Targets), got)
	}
	for i := range want.Targets {
		if got.Targets[i] != want.Targets[i] {
			t.Errorf("target %d = %+v; want %+v", i, got.Targets[i], want.Targets[i])
		}
	}
}

// TestLoadCanonicalFixture asserts the §2.4 fixture loads into the
// expected two-Target Config, field by field — a silent value loss, a
// key mix-up or a lost entry fails here (§10).
func TestLoadCanonicalFixture(t *testing.T) {
	got, err := config.Load(canonicalPath)
	if err != nil {
		t.Fatalf("Load(%q): unexpected error %v", canonicalPath, err)
	}
	wantConfig(t, got, canonicalConfig)
}

// TestDecodeValid covers the accepting surface via the stream entry
// point: with and without the optional procsig key, an exported-case
// package name (casing is not enforced), the uniform null rule (a
// dangling procsig: is YAML null, equivalent to omitting the key), and
// two entries sharing a schema and a query directory — the only
// cross-entry rule is output overlap (§2.2).
func TestDecodeValid(t *testing.T) {
	withoutProcsig := oneEntryConfig
	withoutProcsig.Targets = slices.Clone(oneEntryConfig.Targets)
	withoutProcsig.Targets[0].ProcsigPath = ""
	exportedPackage := oneEntryConfig
	exportedPackage.Targets = slices.Clone(oneEntryConfig.Targets)
	exportedPackage.Targets[0].Go.Package = "Db"

	cases := []struct {
		name string
		body string
		want config.Config
	}{
		{name: "with procsig", body: oneEntry, want: oneEntryConfig},
		{name: "two targets", body: validDoc, want: canonicalConfig},
		{name: "without procsig", body: dropEntryKey("procsig"), want: withoutProcsig},
		{name: "dangling procsig key is null, treated as omitted", body: setEntryKey("procsig", ""), want: withoutProcsig},
		{name: "exported-case package accepted", body: setEntryKey("package", "Db"), want: exportedPackage},
		{name: "shared schema and query directory", body: twoEntryDoc("internal/a", "internal/b"), want: config.Config{Targets: []config.Target{
			{SchemaPath: "schema.gql", SchemaLang: config.SchemaLangGQL, QueryDir: "queries", QueryLang: config.QueryLangOpenCypher, Go: config.GoGen{Package: "a", Out: "internal/a", Driver: config.DriverNeo4jGoV5}},
			{SchemaPath: "schema.gql", SchemaLang: config.SchemaLangGQL, QueryDir: "queries", QueryLang: config.QueryLangOpenCypher, Go: config.GoGen{Package: "b", Out: "internal/b", Driver: config.DriverNeo4jGoV5}},
		}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := config.Decode(strings.NewReader(tc.body))
			if err != nil {
				t.Fatalf("Decode: unexpected error %v", err)
			}
			wantConfig(t, got, tc.want)
		})
	}
}

// TestLoadPreservesRawPaths: the loader resolves nothing and cleans
// nothing (config-file-format §4), so trailing slashes and "./" prefixes
// reach Config exactly as written. Only the overlap comparison sees
// cleaned paths (§4.3).
func TestLoadPreservesRawPaths(t *testing.T) {
	body := `version: 1
graph:
  -
    schema: ./schema.gql
    schema_language: gql
    queries: ./queries/
    query_language: opencypher
    procsig: ./procs.procsig.json
    gen:
      go:
        package: db
        out: ./internal/db/
        driver: neo4j-go-v5
`
	got, err := config.Decode(strings.NewReader(body))
	if err != nil {
		t.Fatalf("Decode: unexpected error %v", err)
	}
	wantConfig(t, got, config.Config{Targets: []config.Target{{
		SchemaPath:  "./schema.gql",
		SchemaLang:  config.SchemaLangGQL,
		QueryDir:    "./queries/",
		QueryLang:   config.QueryLangOpenCypher,
		ProcsigPath: "./procs.procsig.json",
		Go:          config.GoGen{Package: "db", Out: "./internal/db/", Driver: config.DriverNeo4jGoV5},
	}}})
}

// oldFlatDoc is the previous format's canonical file: version 1
// truthfully, every former top-level key present.
const oldFlatDoc = `version: 1
schema: schema.gql
queries: queries/
output: internal/db
package: db
schema_language: gql
query_language: opencypher
driver: neo4j-go-v5
procsig: procs.procsig.json
`

// flatShapeTail is the §4.2 message from the offending key onward.
const flatShapeTail = ` is not a top-level key; version 1 declares a "graph" sequence of generation targets, each carrying its own schema, queries, and gen.go block`

// TestRejectOldFlatShape: a file written for the previous format names
// the shape change, not a list of unknown keys (§4.2). The key list is
// frozen and reported in its own order, not document order, at the
// offending key's own line.
func TestRejectOldFlatShape(t *testing.T) {
	t.Run("previous canonical file", func(t *testing.T) {
		_, err := config.Decode(strings.NewReader(oldFlatDoc))
		if err == nil {
			t.Fatal("expected the old flat shape to be rejected")
		}
		want := `config: <stream>: line 2: "schema"` + flatShapeTail
		if err.Error() != want {
			t.Fatalf("error = %q; want %q", err.Error(), want)
		}
		if strings.Contains(err.Error(), "not found in type") {
			t.Errorf("error %q fell through to the unknown-key wall", err.Error())
		}
	})

	for _, key := range []string{"schema", "queries", "output", "package", "schema_language", "query_language", "driver", "procsig"} {
		t.Run("lone "+key, func(t *testing.T) {
			_, err := config.Decode(strings.NewReader("version: 1\n" + key + ": x\n"))
			if err == nil {
				t.Fatalf("expected a lone %q to be rejected", key)
			}
			want := `config: <stream>: line 2: "` + key + `"` + flatShapeTail
			if err.Error() != want {
				t.Fatalf("error = %q; want %q", err.Error(), want)
			}
		})
	}

	t.Run("reports the frozen list order, not document order", func(t *testing.T) {
		_, err := config.Decode(strings.NewReader("version: 1\ndriver: neo4j-go-v5\nqueries: q\n"))
		if err == nil {
			t.Fatal("expected rejection")
		}
		want := `config: <stream>: line 3: "queries"` + flatShapeTail
		if err.Error() != want {
			t.Fatalf("error = %q; want %q", err.Error(), want)
		}
	})

	t.Run("a graph plus a stray former key is an unknown key, not this case", func(t *testing.T) {
		_, err := config.Decode(strings.NewReader(validDoc + "output: internal/db\n"))
		if err == nil {
			t.Fatal("expected rejection")
		}
		if !strings.Contains(err.Error(), "field output not found in type config.wireV1") {
			t.Fatalf("error = %q; want the unknown-key wall", err.Error())
		}
	})
}

// TestRejectionTable walks every §4.5 row a config file can reach.
// Loader-formatted rows are asserted whole (want); rows yaml.v3 formats
// are asserted by substring (wantSubs), because their exact wording is
// the library's. The internal count row is the one row no document can
// reach — TestEntryCountInvariant pins its wording instead.
func TestRejectionTable(t *testing.T) {
	cases := []struct {
		name     string
		body     string
		want     string
		wantSubs []string
	}{
		// Document level.
		{
			name:     "empty file",
			body:     "",
			wantSubs: []string{"is empty"},
		},
		{
			name:     "malformed YAML carries yaml line info",
			body:     "version: 1\n\tgraph: []\n",
			wantSubs: []string{"yaml", "line 2"},
		},
		{
			name:     "non-mapping document cites a readable probe type",
			body:     "hello\n",
			wantSubs: []string{"cannot unmarshal !!str `hello`", "config.versionProbe"},
		},
		{
			name: "missing version",
			body: "graph: []\n",
			want: `config: <stream>: missing required field "version" (this gqlc supports version 1)`,
		},
		{
			name: "version 0",
			body: "version: 0\ngraph: []\n",
			want: "config: <stream>: declares version 0; only version 1 is supported",
		},
		{
			name: "version quoted string is not coerced",
			body: "version: \"1\"\ngraph: []\n",
			want: `config: <stream>: line 1: field "version" must be a YAML integer (got !!str "1")`,
		},
		{
			name: "version float is not truncated",
			body: "version: 1.5\ngraph: []\n",
			want: `config: <stream>: line 1: field "version" must be a YAML integer (got !!float "1.5")`,
		},
		{
			name: "non-scalar version",
			body: "version: [1]\ngraph: []\n",
			want: `config: <stream>: line 1: field "version" must be a YAML integer (got a YAML sequence)`,
		},
		{
			name:     "version overflowing Go int surfaces the yaml error",
			body:     "version: 9223372036854775808\ngraph: []\n",
			wantSubs: []string{`field "version": yaml: unmarshal errors:`, "line 1: cannot unmarshal !!int `9223372...` into int"},
		},
		{
			name: "graph omitted",
			body: "version: 1\n",
			want: `config: <stream>: missing required field "graph"`,
		},
		{
			name: "graph null",
			body: "version: 1\ngraph:\n",
			want: `config: <stream>: missing required field "graph"`,
		},
		{
			name: "graph explicitly null",
			body: "version: 1\ngraph: ~\n",
			want: `config: <stream>: missing required field "graph"`,
		},
		{
			name: "graph empty flow sequence",
			body: "version: 1\ngraph: []\n",
			want: `config: <stream>: line 2: field "graph" must not be empty; declare at least one generation target`,
		},
		{
			name: "graph empty sequence written below the key",
			body: "version: 1\ngraph:\n  []\n",
			want: `config: <stream>: line 3: field "graph" must not be empty; declare at least one generation target`,
		},
		{
			name:     "an entry is not a mapping",
			body:     "version: 1\ngraph:\n  - x\n",
			wantSubs: []string{"line 3: cannot unmarshal !!str `x` into config.wireTarget"},
		},
		{
			name:     "an empty-string entry is a wrong-typed entry, not a null one",
			body:     "version: 1\ngraph:\n  - \"\"\n",
			wantSubs: []string{"cannot unmarshal !!str `` into config.wireTarget"},
		},
		// Entry level, loader-formatted. The prefix is unconditional, so
		// these single-entry documents all carry graph[0].
		{
			name: "missing schema",
			body: dropEntryKey("schema"),
			want: `config: <stream>: graph[0]: missing required field "schema"`,
		},
		{
			name: "missing schema_language",
			body: dropEntryKey("schema_language"),
			want: `config: <stream>: graph[0]: missing required field "schema_language" (valid values: gql)`,
		},
		{
			name: "missing queries",
			body: dropEntryKey("queries"),
			want: `config: <stream>: graph[0]: missing required field "queries"`,
		},
		{
			name: "missing query_language",
			body: dropEntryKey("query_language"),
			want: `config: <stream>: graph[0]: missing required field "query_language" (valid values: opencypher)`,
		},
		{
			name: "missing gen",
			body: "version: 1\ngraph:\n  -\n    schema: s.gql\n    schema_language: gql\n    queries: q\n    query_language: opencypher\n",
			want: `config: <stream>: graph[0]: missing required field "gen"`,
		},
		{
			name: "null gen",
			body: "version: 1\ngraph:\n  -\n    schema: s.gql\n    schema_language: gql\n    queries: q\n    query_language: opencypher\n    gen:\n",
			want: `config: <stream>: graph[0]: missing required field "gen"`,
		},
		{
			name: "missing gen.go",
			body: "version: 1\ngraph:\n  -\n    schema: s.gql\n    schema_language: gql\n    queries: q\n    query_language: opencypher\n    gen: {}\n",
			want: `config: <stream>: graph[0]: missing required field "gen.go"`,
		},
		{
			name: "missing gen.go.package",
			body: "version: 1\ngraph:\n  -\n    schema: s.gql\n    schema_language: gql\n    queries: q\n    query_language: opencypher\n    gen:\n      go:\n        out: internal/db\n        driver: neo4j-go-v5\n",
			want: `config: <stream>: graph[0]: missing required field "gen.go.package"`,
		},
		{
			name: "missing gen.go.out",
			body: "version: 1\ngraph:\n  -\n    schema: s.gql\n    schema_language: gql\n    queries: q\n    query_language: opencypher\n    gen:\n      go:\n        package: db\n        driver: neo4j-go-v5\n",
			want: `config: <stream>: graph[0]: missing required field "gen.go.out"`,
		},
		{
			name: "missing gen.go.driver",
			body: "version: 1\ngraph:\n  -\n    schema: s.gql\n    schema_language: gql\n    queries: q\n    query_language: opencypher\n    gen:\n      go:\n        package: db\n        out: internal/db\n",
			want: `config: <stream>: graph[0]: missing required field "gen.go.driver" (valid values: neo4j-go-v5, neo4j-go-v6)`,
		},
		{
			name: "empty schema",
			body: setEntryKey("schema", `""`),
			want: `config: <stream>: graph[0]: field "schema" must not be empty`,
		},
		{
			name: "empty queries",
			body: setEntryKey("queries", `""`),
			want: `config: <stream>: graph[0]: field "queries" must not be empty`,
		},
		{
			name: "empty gen.go.package",
			body: setEntryKey("package", `""`),
			want: `config: <stream>: graph[0]: field "gen.go.package" must not be empty`,
		},
		{
			name: "empty gen.go.out",
			body: setEntryKey("out", `""`),
			want: `config: <stream>: graph[0]: field "gen.go.out" must not be empty`,
		},
		{
			name: "empty procsig",
			body: setEntryKey("procsig", `""`),
			want: `config: <stream>: graph[0]: field "procsig" is empty; omit the key when no procsig file is used`,
		},
		{
			name: "package with a hyphen",
			body: setEntryKey("package", "my-db"),
			want: `config: <stream>: graph[0]: package "my-db" is not a valid Go identifier`,
		},
		{
			name: "package is a Go keyword",
			body: setEntryKey("package", "func"),
			want: `config: <stream>: graph[0]: package "func" is not a valid Go identifier`,
		},
		{
			name: "package starts with a digit",
			body: setEntryKey("package", "123abc"),
			want: `config: <stream>: graph[0]: package "123abc" is not a valid Go identifier`,
		},
		// The prefix indexes the offending entry of a two-entry document.
		{
			name: "second entry names graph[1]",
			body: strings.Replace(validDoc, "    queries: internal/order/query\n", "", 1),
			want: `config: <stream>: graph[1]: missing required field "queries"`,
		},
		{
			name: "second entry's package is validated too",
			body: strings.Replace(validDoc, "package: orderdb", "package: order-db", 1),
			want: `config: <stream>: graph[1]: package "order-db" is not a valid Go identifier`,
		},
		// Entry level, yaml.v3-formatted: no index prefix, a line instead.
		{
			name:     "unknown key in an entry",
			body:     setEntryKey("queries", "q\n    bogus: x"),
			wantSubs: []string{"line 7: field bogus not found in type config.wireTarget"},
		},
		{
			name:     "unknown key under gen",
			body:     "version: 1\ngraph:\n  -\n    gen:\n      rust: {}\n",
			wantSubs: []string{"line 5: field rust not found in type config.wireGen"},
		},
		{
			name:     "unknown key under gen.go",
			body:     "version: 1\ngraph:\n  -\n    gen:\n      go:\n        bogus: x\n",
			wantSubs: []string{"line 6: field bogus not found in type config.wireGo"},
		},
		{
			name:     "unknown top-level key alongside graph",
			body:     validDoc + "bogus: x\n",
			wantSubs: []string{"line 22: field bogus not found in type config.wireV1"},
		},
		{
			name:     "duplicate key in an entry",
			body:     setEntryKey("queries", "q\n    queries: q2"),
			wantSubs: []string{`line 7: mapping key "queries" already defined at line 6`},
		},
		{
			name:     "non-scalar path value",
			body:     setEntryKey("schema", "[a]"),
			wantSubs: []string{"line 4", "cannot unmarshal !!seq into string"},
		},
		{
			name:     "invalid schema_language",
			body:     setEntryKey("schema_language", "graphql"),
			wantSubs: []string{`line 5: invalid schema_language "graphql" (valid values: gql)`},
		},
		{
			name:     "invalid query_language",
			body:     setEntryKey("query_language", "sql"),
			wantSubs: []string{`line 7: invalid query_language "sql" (valid values: opencypher)`},
		},
		{
			name:     "invalid driver",
			body:     setEntryKey("driver", "neo4j-go-v4"),
			wantSubs: []string{`line 13: invalid driver "neo4j-go-v4" (valid values: neo4j-go-v5, neo4j-go-v6)`},
		},
		{
			name:     "sequence-valued driver named as such",
			body:     setEntryKey("driver", "[x]"),
			wantSubs: []string{"line 13: invalid driver: expected a scalar value, got a YAML sequence"},
		},
		{
			name:     "mapping-valued driver named as such",
			body:     setEntryKey("driver", "{a: b}"),
			wantSubs: []string{"line 13: invalid driver: expected a scalar value, got a YAML mapping"},
		},
		// Cross-entry.
		{
			name: "two entries share an output directory",
			body: twoEntryDoc("internal/db", "internal/db"),
			want: `config: <stream>: graph[1]: out "internal/db" is already graph[0]'s output directory`,
		},
		{
			name: "the later output directory is inside the earlier",
			body: twoEntryDoc("internal/db", "internal/db/sub"),
			want: `config: <stream>: graph[1]: out "internal/db/sub" is inside graph[0]'s output directory "internal/db"`,
		},
		{
			name: "the later output directory contains the earlier",
			body: twoEntryDoc("internal/db/sub", "internal/db"),
			want: `config: <stream>: graph[1]: out "internal/db" contains graph[0]'s output directory "internal/db/sub"`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := config.Decode(strings.NewReader(tc.body))
			if err == nil {
				t.Fatalf("expected an error, got nil")
			}
			msg := err.Error()
			if !strings.HasPrefix(msg, "config: ") {
				t.Errorf("error %q lacks the \"config: \" prefix", msg)
			}
			if !strings.Contains(msg, "<stream>") {
				t.Errorf("error %q does not name the <stream> source", msg)
			}
			if tc.want != "" && msg != tc.want {
				t.Fatalf("error = %q; want %q", msg, tc.want)
			}
			for _, sub := range tc.wantSubs {
				if !strings.Contains(msg, sub) {
					t.Errorf("error %q does not contain %q", msg, sub)
				}
			}
		})
	}
}

// TestVersionProbeUnaffected is the failure the document scan exists to
// avoid (§4): the probe stays lenient about graph, so a version 2 file
// reports its version even when its graph is malformed. A typed graph
// field on the probe would make this report a shape complaint instead.
func TestVersionProbeUnaffected(t *testing.T) {
	_, err := config.Decode(strings.NewReader("version: 2\ngraph: nope\n"))
	if err == nil {
		t.Fatal("expected a version error")
	}
	want := "config: <stream>: declares version 2; only version 1 is supported"
	if err.Error() != want {
		t.Fatalf("error = %q; want %q", err.Error(), want)
	}
}

// TestGraphNotASequence: the loader's own message names a YAML kind and
// leaks no yaml.v3 or Go type name, and the kind it names is the
// resolved one — `graph: *g` aliasing a scalar reports "scalar", never
// "alias", at the alias's own line rather than the anchor's (§4).
func TestGraphNotASequence(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{
			name: "scalar",
			body: "version: 1\ngraph: nope\n",
			want: `config: <stream>: line 2: field "graph" must be a sequence of generation targets (got a YAML scalar)`,
		},
		{
			name: "mapping",
			body: "version: 1\ngraph:\n  a: b\n",
			want: `config: <stream>: line 3: field "graph" must be a sequence of generation targets (got a YAML mapping)`,
		},
		{
			name: "alias to a scalar reports the resolved kind at the alias's line",
			body: "version: 1\nanchors:\n  g: &g nope\ngraph: *g\n",
			want: `config: <stream>: line 4: field "graph" must be a sequence of generation targets (got a YAML scalar)`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := config.Decode(strings.NewReader(tc.body))
			if err == nil {
				t.Fatal("expected an error, got nil")
			}
			if err.Error() != tc.want {
				t.Fatalf("error = %q; want %q", err.Error(), tc.want)
			}
			for _, leak := range []string{"yaml:", "config.wire", "[]"} {
				if strings.Contains(err.Error(), leak) {
					t.Errorf("error %q leaks %q", err.Error(), leak)
				}
			}
		})
	}
}

// TestNullEntryRejected: yaml.v3 drops a null sequence element, so every
// later entry's index would renumber. Each null spelling is rejected by
// its own Content index — 1 in a three-element sequence, not the 0 the
// shortened slice would suggest — and a non-null scalar entry is not
// this case (§4.4).
func TestNullEntryRejected(t *testing.T) {
	entry := "  -\n" +
		"    schema: schema.gql\n" +
		"    schema_language: gql\n" +
		"    queries: queries\n" +
		"    query_language: opencypher\n" +
		"    gen:\n" +
		"      go:\n" +
		"        package: db\n" +
		"        out: internal/db\n" +
		"        driver: neo4j-go-v5\n"

	for _, spelling := range []string{"~", "null", "Null", "NULL", ""} {
		t.Run("spelling "+strconv.Quote(spelling), func(t *testing.T) {
			body := "version: 1\ngraph:\n" + entry + "  - " + spelling + "\n" + entry
			_, err := config.Decode(strings.NewReader(body))
			if err == nil {
				t.Fatal("expected a null entry to be rejected")
			}
			want := "config: <stream>: graph[1]: line 13: entry is null"
			if err.Error() != want {
				t.Fatalf("error = %q; want %q", err.Error(), want)
			}
		})
	}

	t.Run("an empty-string entry is not a null entry", func(t *testing.T) {
		_, err := config.Decode(strings.NewReader("version: 1\ngraph:\n" + entry + "  - \"\"\n"))
		if err == nil {
			t.Fatal("expected an error")
		}
		if strings.Contains(err.Error(), "entry is null") {
			t.Fatalf("error = %q; a present !!str entry is a wrong-typed entry", err.Error())
		}
	})
}

// TestAliasEntryToNullRejected pins the resolution half of §4: an alias
// to a null anchor is dropped by yaml.v3 exactly as a written null is,
// while carrying an empty Tag of its own. A Tag == "!!null" test on the
// unresolved node lets it through. The line reported is the alias's, not
// the anchor's — the anchor may be legitimate elsewhere.
func TestAliasEntryToNullRejected(t *testing.T) {
	body := `version: 1
anchors:
  none: &none ~
graph:
  - *none
`
	_, err := config.Decode(strings.NewReader(body))
	if err == nil {
		t.Fatal("expected the aliased null entry to be rejected")
	}
	want := "config: <stream>: graph[0]: line 5: entry is null"
	if err.Error() != want {
		t.Fatalf("error = %q; want %q", err.Error(), want)
	}
}

// TestMergeKeyEntryLoads: resolving aliases changes what the scan looks
// at, never what it rejects. An entry assembled with `<<: *base` is a
// mapping to the scan and a full entry to the strict decode, so with a
// distinct out it loads (§4).
func TestMergeKeyEntryLoads(t *testing.T) {
	body := `version: 1
graph:
  - &base
    schema: schema.gql
    schema_language: gql
    queries: queries
    query_language: opencypher
    gen:
      go:
        package: db
        out: internal/db
        driver: neo4j-go-v5
  - <<: *base
    gen:
      go:
        package: db2
        out: internal/db2
        driver: neo4j-go-v6
`
	got, err := config.Decode(strings.NewReader(body))
	if err != nil {
		t.Fatalf("Decode: unexpected error %v", err)
	}
	wantConfig(t, got, config.Config{Targets: []config.Target{
		{SchemaPath: "schema.gql", SchemaLang: config.SchemaLangGQL, QueryDir: "queries", QueryLang: config.QueryLangOpenCypher, Go: config.GoGen{Package: "db", Out: "internal/db", Driver: config.DriverNeo4jGoV5}},
		{SchemaPath: "schema.gql", SchemaLang: config.SchemaLangGQL, QueryDir: "queries", QueryLang: config.QueryLangOpenCypher, Go: config.GoGen{Package: "db2", Out: "internal/db2", Driver: config.DriverNeo4jGoV6}},
	}})
}

// TestAliasEntryToMappingReachesOverlap: an element aliasing an earlier
// entry resolves to a mapping, so §4.4 leaves it alone and it decodes
// into a second target — carrying the earlier entry's out, which §4.3
// rejects. Asserting that it loads would assert what §4.3 forbids, and
// exempting aliased entries from the overlap sweep is the bug this
// pins (§4).
func TestAliasEntryToMappingReachesOverlap(t *testing.T) {
	body := `version: 1
graph:
  - &t
    schema: schema.gql
    schema_language: gql
    queries: queries
    query_language: opencypher
    gen:
      go:
        package: db
        out: internal/db
        driver: neo4j-go-v5
  - *t
`
	_, err := config.Decode(strings.NewReader(body))
	if err == nil {
		t.Fatal("expected the aliased entry to reach the overlap sweep")
	}
	want := `config: <stream>: graph[1]: out "internal/db" is already graph[0]'s output directory`
	if err.Error() != want {
		t.Fatalf("error = %q; want %q", err.Error(), want)
	}
}

// outPairs is the §4.3 comparison surface, shared by the loader sweep
// and the exported seam so the two cannot disagree. wantErr is
// CheckOutAgainst's own text, empty when the pair is disjoint.
var outPairs = []struct {
	name           string
	earlier, later string
	wantErr        string
}{
	{
		name: "equal", earlier: "internal/db", later: "internal/db",
		wantErr: `out "internal/db" is already graph[0]'s output directory`,
	},
	{
		name: "trailing slash", earlier: "internal/db", later: "internal/db/",
		wantErr: `out "internal/db/" is already graph[0]'s output directory`,
	},
	{
		name: "dot-slash prefix", earlier: "internal/db", later: "./internal/db",
		wantErr: `out "./internal/db" is already graph[0]'s output directory`,
	},
	{
		name: "doubled separator", earlier: "internal/db", later: "internal//db",
		wantErr: `out "internal//db" is already graph[0]'s output directory`,
	},
	{
		name: "obscured by a dot-dot component", earlier: "internal/db", later: "./a/../internal/db",
		wantErr: `out "./a/../internal/db" is already graph[0]'s output directory`,
	},
	{
		name: "later nested inside earlier", earlier: "internal/db", later: "internal/db/sub",
		wantErr: `out "internal/db/sub" is inside graph[0]'s output directory "internal/db"`,
	},
	{
		name: "earlier nested inside later", earlier: "internal/db/sub", later: "internal/db",
		wantErr: `out "internal/db" contains graph[0]'s output directory "internal/db/sub"`,
	},
	{
		// filepath.Rel returns "..foo", whose first two characters a
		// string-prefix disjointness test would read as an escape.
		name: "a directory whose name starts with dot-dot", earlier: "internal/db", later: "internal/db/..foo",
		wantErr: `out "internal/db/..foo" is inside graph[0]'s output directory "internal/db"`,
	},
	{
		name: "the same trap one level deeper", earlier: "internal/db", later: "internal/db/..foo/x",
		wantErr: `out "internal/db/..foo/x" is inside graph[0]'s output directory "internal/db"`,
	},
	{
		name: "escaping paths compare normally", earlier: "../a", later: "../a/b",
		wantErr: `out "../a/b" is inside graph[0]'s output directory "../a"`,
	},
	{
		// filepath.Rel refuses a base that escapes its own root, so
		// Rel("..", "a") errors while Rel("a", "..") returns the
		// escaping "../..": both directions fall through and an
		// unanchored comparison reads plain containment as disjoint.
		name: "a rooted path inside an escaping base", earlier: "..", later: "a",
		wantErr: `out "a" is inside graph[0]'s output directory ".."`,
	},
	{
		name: "both operands escape, one contains the other", earlier: "../..", later: "../a",
		wantErr: `out "../a" is inside graph[0]'s output directory "../.."`,
	},
	{
		name: "the working directory itself inside an escaping base", earlier: "..", later: ".",
		wantErr: `out "." is inside graph[0]'s output directory ".."`,
	},
	{
		name: "an escaping base reached the other way round", earlier: "a", later: "..",
		wantErr: `out ".." contains graph[0]'s output directory "a"`,
	},
	{
		// The rebasing depth is the deeper operand's, not the first
		// one's: taking it from "." would sink both onto the root and
		// report these as the same directory.
		name: "the later entry is the deeper escape", earlier: ".", later: "..",
		wantErr: `out ".." contains graph[0]'s output directory "."`,
	},
	{name: "siblings", earlier: "internal/db", later: "internal/user"},
	{name: "a name prefix is not a path prefix", earlier: "internal/db", later: "internal/dbgen"},
	{name: "an escaping path against a rooted one", earlier: "b", later: "../a"},
	{name: "absolute against relative (the honest limit)", earlier: "/tmp/gqlc/db", later: "internal/db"},
	{
		// Rebasing a relative path is only sound against another
		// relative one: joining "internal/db" and "/internal/db" onto a
		// shared base makes them one directory, which they are only
		// when the working directory is the root.
		name: "a shared suffix does not make absolute meet relative", earlier: "/internal/db", later: "internal/db",
	},
	{
		// The surviving limit that is not abs-vs-rel: both pairs
		// overlap when the working directory carries a particular
		// name — "b" for the first, "db" for the second — which no
		// lexical comparison can see. Rebasing onto a base one segment
		// deep keeps them apart; a base fixed at the root would report
		// "../db" and "db" as one directory.
		name: "re-entry through an unknown ancestor name (the honest limit)", earlier: "../b/db", later: "db",
	},
	{name: "an escaping path beside its rooted namesake", earlier: "../db", later: "db"},
	{
		// What the NUL in the anchor segment buys. Spelled without it,
		// these anchor to "/gqlc-anchor-0/gqlc-anchor-0" and
		// "/gqlc-anchor-0", which Rel reads as containment — a disjoint
		// pair rejected because an operand happens to name the base.
		name: "a directory named like the synthetic anchor", earlier: "gqlc-anchor-0", later: "../gqlc-anchor-0",
	},
}

// TestOutOverlap drives the §4.3 sweep through the loader: a rejected
// pair names both entries — the later in the prefix, the earlier in the
// message — and an accepted pair loads.
func TestOutOverlap(t *testing.T) {
	for _, tc := range outPairs {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := config.Decode(strings.NewReader(twoEntryDoc(tc.earlier, tc.later)))
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("Decode: unexpected error %v", err)
				}
				if len(cfg.Targets) != 2 {
					t.Fatalf("got %d targets, want 2", len(cfg.Targets))
				}
				return
			}
			if err == nil {
				t.Fatal("expected the overlapping pair to be rejected")
			}
			want := "config: <stream>: graph[1]: " + tc.wantErr
			if err.Error() != want {
				t.Fatalf("error = %q; want %q", err.Error(), want)
			}
		})
	}
}

// TestCheckOutAgainst drives the same surface through the exported seam
// `init --add` validates with: the catalogue message for the first
// overlapping index, nil for a disjoint out, and byte-identical text to
// the loader's sweep on every row above (which is why the function
// exists — one implementation of overlap in the tree).
func TestCheckOutAgainst(t *testing.T) {
	for _, tc := range outPairs {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.Config{Targets: []config.Target{{Go: config.GoGen{Out: tc.earlier}}}}
			err := cfg.CheckOutAgainst(tc.later)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("CheckOutAgainst(%q) = %v; want nil", tc.later, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("CheckOutAgainst(%q) = nil; want %q", tc.later, tc.wantErr)
			}
			if err.Error() != tc.wantErr {
				t.Fatalf("CheckOutAgainst(%q) = %q; want %q", tc.later, err.Error(), tc.wantErr)
			}
		})
	}

	t.Run("empty config accepts anything", func(t *testing.T) {
		if err := (config.Config{}).CheckOutAgainst("internal/db"); err != nil {
			t.Fatalf("CheckOutAgainst on a zero Config = %v; want nil", err)
		}
	})

	t.Run("names the first overlapping index", func(t *testing.T) {
		cfg := config.Config{Targets: []config.Target{
			{Go: config.GoGen{Out: "internal/a"}},
			{Go: config.GoGen{Out: "internal/db"}},
			{Go: config.GoGen{Out: "internal/db/sub"}},
		}}
		err := cfg.CheckOutAgainst("internal/db")
		if err == nil {
			t.Fatal("expected an overlap")
		}
		want := `out "internal/db" is already graph[1]'s output directory`
		if err.Error() != want {
			t.Fatalf("CheckOutAgainst = %q; want %q", err.Error(), want)
		}
	})
}

// TestLoadMissingFile asserts the open error wraps the underlying fs
// error (config-file-format §6.1): `gqlc init` branches on
// errors.Is(err, fs.ErrNotExist) to offer creating the file.
func TestLoadMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nope.yaml")
	_, err := config.Load(path)
	if err == nil {
		t.Fatal("expected error opening missing file")
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("errors.Is(err, fs.ErrNotExist) = false; want true (err = %v)", err)
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("error %q does not mention the offending path", err.Error())
	}
}

// TestLoadErrorsNameThePath asserts Load labels decode-stage errors with
// the file path (not "<stream>").
func TestLoadErrorsNameThePath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.gqlc.yaml")
	if err := os.WriteFile(path, []byte(dropEntryKey("schema")), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	_, err := config.Load(path)
	if err == nil {
		t.Fatal("expected error for a missing schema")
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("error %q does not name the source path", err.Error())
	}
}

// TestSaveEmitsFixtureBytes is the Load∘Save byte-identity round trip
// (§5): the fixture is the source of truth for the canonical form —
// nesting, sequence indent, and the procsig key omitted on the second
// entry — and Save of the loaded Config must reproduce it exactly.
func TestSaveEmitsFixtureBytes(t *testing.T) {
	cfg, err := config.Load(canonicalPath)
	if err != nil {
		t.Fatalf("Load fixture: %v", err)
	}
	path := filepath.Join(t.TempDir(), "out.gqlc.yaml")
	if err := cfg.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read saved: %v", err)
	}
	want, err := os.ReadFile(canonicalPath)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("Save output drifts from fixture:\n--- want ---\n%s\n--- got ---\n%s", want, got)
	}
}

// TestCanonicalMatchesSave: Canonical returns exactly the bytes Save
// writes (cli-stage-2 §5.2) — `gqlc init`'s preview/write identity is by
// construction, not parallel encoders — and both still match the fixture.
func TestCanonicalMatchesSave(t *testing.T) {
	cfg, err := config.Load(canonicalPath)
	if err != nil {
		t.Fatalf("Load fixture: %v", err)
	}
	canon, err := cfg.Canonical()
	if err != nil {
		t.Fatalf("Canonical: %v", err)
	}
	path := filepath.Join(t.TempDir(), "out.gqlc.yaml")
	if err := cfg.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read saved: %v", err)
	}
	if string(canon) != string(saved) {
		t.Fatalf("Canonical drifts from Save:\n--- canonical ---\n%s\n--- saved ---\n%s", canon, saved)
	}
	fixture, err := os.ReadFile(canonicalPath)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	if string(canon) != string(fixture) {
		t.Fatalf("Canonical drifts from fixture:\n--- canonical ---\n%s\n--- fixture ---\n%s", canon, fixture)
	}
}

// TestSaveLoadRoundTrip is the from-Go direction (`gqlc init`'s path): a
// Go-constructed Config must Save and Load back identically, and an
// empty ProcsigPath must omit the procsig key entirely rather than
// writing the rejected explicit-empty form.
func TestSaveLoadRoundTrip(t *testing.T) {
	cfg := config.Config{Targets: []config.Target{{
		SchemaPath: "schema.gql",
		SchemaLang: config.SchemaLangGQL,
		QueryDir:   "queries",
		QueryLang:  config.QueryLangOpenCypher,
		Go:         config.GoGen{Package: "db", Out: "internal/db", Driver: config.DriverNeo4jGoV5},
	}}}
	path := filepath.Join(t.TempDir(), "out.gqlc.yaml")
	if err := cfg.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	blob, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read saved: %v", err)
	}
	if strings.Contains(string(blob), "procsig") {
		t.Errorf("Save emitted a procsig key for an empty ProcsigPath:\n%s", blob)
	}
	got, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load(saved): %v", err)
	}
	wantConfig(t, got, cfg)
}

// TestCanonicalOfZeroConfig: Canonical validates nothing, and the value
// type of wireV1.Graph is what makes a zero Config emit "graph: []" —
// the empty-sequence complaint Load then answers with. A *[]wireTarget
// would emit "graph: null" and Load would claim the key is absent.
func TestCanonicalOfZeroConfig(t *testing.T) {
	b, err := config.Config{}.Canonical()
	if err != nil {
		t.Fatalf("Canonical: %v", err)
	}
	if got, want := string(b), "version: 1\ngraph: []\n"; got != want {
		t.Fatalf("Canonical of a zero Config = %q; want %q", got, want)
	}
	_, err = config.Decode(strings.NewReader(string(b)))
	if err == nil {
		t.Fatal("expected the zero Config's bytes to be rejected")
	}
	want := `config: <stream>: line 2: field "graph" must not be empty; declare at least one generation target`
	if err.Error() != want {
		t.Fatalf("error = %q; want %q", err.Error(), want)
	}
}
