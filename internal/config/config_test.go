package config_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/areqag/gqlc/internal/config"
)

// canonicalPath is the sole on-disk fixture; it pins the canonical Save
// form (spec §7), so any drift in the emitted bytes (a re-ordered key,
// a quoting change) fails TestSaveEmitsFixtureBytes.
const canonicalPath = "testdata/canonical.gqlc.yaml"

// validDoc mirrors the canonical fixture byte-for-byte; the rejection
// table derives each malformed variant from it, so key line numbers
// (version=1 ... procsig=9) are stable for the line-info assertions.
const validDoc = `version: 1
schema: schema.gql
queries: queries/
output: internal/db
package: db
schema_language: gqlc
query_language: opencypher
driver: neo4j-go-v5
procsig: procs.procsig.json
`

// canonicalConfig is the in-memory equivalent of validDoc / the fixture.
var canonicalConfig = config.Config{
	SchemaPath:    "schema.gql",
	QueryDir:      "queries/",
	OutputDir:     "internal/db",
	OutputPackage: "db",
	ProcsigPath:   "procs.procsig.json",
	SchemaLang:    config.SchemaLangGQLC,
	QueryLang:     config.QueryLangOpenCypher,
	Driver:        config.DriverNeo4jGoV5,
}

// dropKey returns validDoc without the top-level line for key.
func dropKey(key string) string {
	var out []string
	for line := range strings.Lines(validDoc) {
		if strings.HasPrefix(line, key+":") {
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "")
}

// setKey returns validDoc with key's scalar value replaced by value.
func setKey(key, value string) string {
	var out []string
	for line := range strings.Lines(validDoc) {
		if strings.HasPrefix(line, key+":") {
			line = key + ": " + value + "\n"
		}
		out = append(out, line)
	}
	return strings.Join(out, "")
}

// TestDefaultFilename pins the canonical name: no .yml/.json variants,
// no search logic (spec §2).
func TestDefaultFilename(t *testing.T) {
	if config.DefaultFilename != "gqlc.yaml" {
		t.Fatalf("DefaultFilename = %q; want %q", config.DefaultFilename, "gqlc.yaml")
	}
}

// TestEnumValues locks each axis vocabulary. Error messages and future
// `gqlc init` prompts derive from these slices, so growing an axis must
// be a deliberate, test-visible change.
func TestEnumValues(t *testing.T) {
	if got, want := config.SchemaLangValues(), []config.SchemaLang{config.SchemaLangGQLC}; !slices.Equal(got, want) {
		t.Errorf("SchemaLangValues() = %v; want %v", got, want)
	}
	if got, want := config.QueryLangValues(), []config.QueryLang{config.QueryLangOpenCypher}; !slices.Equal(got, want) {
		t.Errorf("QueryLangValues() = %v; want %v", got, want)
	}
	if got, want := config.DriverValues(), []config.Driver{config.DriverNeo4jGoV5, config.DriverNeo4jGoV6}; !slices.Equal(got, want) {
		t.Errorf("DriverValues() = %v; want %v", got, want)
	}
}

// TestLoadCanonical asserts the fixture loads into the exact canonical
// Config, field by field — a silent value loss or key mix-up fails here.
func TestLoadCanonical(t *testing.T) {
	got, err := config.Load(canonicalPath)
	if err != nil {
		t.Fatalf("Load(%q): unexpected error %v", canonicalPath, err)
	}
	if got != canonicalConfig {
		t.Fatalf("Load(%q) = %+v; want %+v", canonicalPath, got, canonicalConfig)
	}
}

// TestDecodeValid covers the accepting surface via the stream entry
// point: with and without the optional procsig key, an exported-case
// package name (casing is not enforced — any valid Go identifier is
// accepted), and the uniform null rule (spec §6.2): a dangling
// `procsig:` is YAML null, equivalent to omitting the key.
func TestDecodeValid(t *testing.T) {
	withoutProcsig := canonicalConfig
	withoutProcsig.ProcsigPath = ""
	exportedPackage := canonicalConfig
	exportedPackage.OutputPackage = "Db"
	v6Driver := canonicalConfig
	v6Driver.Driver = config.DriverNeo4jGoV6

	cases := []struct {
		name string
		body string
		want config.Config
	}{
		{name: "with procsig", body: validDoc, want: canonicalConfig},
		{name: "without procsig", body: dropKey("procsig"), want: withoutProcsig},
		{name: "dangling procsig key is null, treated as omitted", body: setKey("procsig", ""), want: withoutProcsig},
		{name: "exported-case package accepted", body: setKey("package", "Db"), want: exportedPackage},
		{name: "v6 driver accepted", body: setKey("driver", "neo4j-go-v6"), want: v6Driver},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := config.Decode(strings.NewReader(tc.body))
			if err != nil {
				t.Fatalf("Decode: unexpected error %v", err)
			}
			if got != tc.want {
				t.Fatalf("Decode = %+v; want %+v", got, tc.want)
			}
		})
	}
}

// TestDecodeRejects walks the rejection surface (spec §6.3). Every
// error must carry the "config: " prefix and name the "<stream>"
// source; each case additionally asserts the message substrings that
// make it actionable (offending key, offending value, line info,
// valid-value lists).
func TestDecodeRejects(t *testing.T) {
	cases := []struct {
		name     string
		body     string
		wantSubs []string
	}{
		{
			name:     "empty file",
			body:     "",
			wantSubs: []string{"is empty"},
		},
		{
			name:     "malformed YAML carries yaml line info",
			body:     "version: 1\n\tschema: schema.gql\n",
			wantSubs: []string{"yaml", "line 2"},
		},
		{
			name:     "missing version",
			body:     dropKey("version"),
			wantSubs: []string{`missing required field "version"`, "version 1"},
		},
		{
			name:     "version 0",
			body:     setKey("version", "0"),
			wantSubs: []string{"declares version 0; only version 1 is supported"},
		},
		{
			name:     "version 2",
			body:     setKey("version", "2"),
			wantSubs: []string{"declares version 2; only version 1 is supported"},
		},
		{
			name:     "version quoted string is not coerced",
			body:     setKey("version", `"1"`),
			wantSubs: []string{`line 1: field "version" must be a YAML integer`, `!!str "1"`},
		},
		{
			name:     "version float 1.0 is not coerced",
			body:     setKey("version", "1.0"),
			wantSubs: []string{`line 1: field "version" must be a YAML integer`, `!!float "1.0"`},
		},
		{
			name:     "version float 1.5 is not truncated",
			body:     setKey("version", "1.5"),
			wantSubs: []string{`line 1: field "version" must be a YAML integer`, `!!float "1.5"`},
		},
		{
			name:     "version scientific 1e0 is not coerced",
			body:     setKey("version", "1e0"),
			wantSubs: []string{`line 1: field "version" must be a YAML integer`, `!!float "1e0"`},
		},
		{
			name:     "non-scalar version",
			body:     setKey("version", "[1]"),
			wantSubs: []string{`line 1: field "version" must be a YAML integer`, "got a YAML sequence"},
		},
		{
			name:     "version overflowing Go int surfaces the yaml error",
			body:     setKey("version", "9223372036854775808"),
			wantSubs: []string{`field "version": yaml: unmarshal errors:`, "line 1: cannot unmarshal !!int `9223372...` into int"},
		},
		{
			name:     "non-mapping document cites a readable probe type",
			body:     "hello\n",
			wantSubs: []string{"cannot unmarshal !!str `hello`", "config.versionProbe"},
		},
		{
			name:     "unknown field (typo)",
			body:     strings.Replace(validDoc, "package: db", "packge: db", 1), //nolint:misspell // the typo is the test: unknown keys must reject
			wantSubs: []string{"field packge not found"},                        //nolint:misspell // the typo is the test: unknown keys must reject
		},
		{
			name:     "missing schema",
			body:     dropKey("schema"),
			wantSubs: []string{`missing required field "schema"`},
		},
		{
			name:     "missing queries",
			body:     dropKey("queries"),
			wantSubs: []string{`missing required field "queries"`},
		},
		{
			name:     "missing output",
			body:     dropKey("output"),
			wantSubs: []string{`missing required field "output"`},
		},
		{
			name:     "missing package",
			body:     dropKey("package"),
			wantSubs: []string{`missing required field "package"`},
		},
		{
			name:     "missing schema_language",
			body:     dropKey("schema_language"),
			wantSubs: []string{`missing required field "schema_language"`, "valid values: gqlc"},
		},
		{
			name:     "missing query_language",
			body:     dropKey("query_language"),
			wantSubs: []string{`missing required field "query_language"`, "valid values: opencypher"},
		},
		{
			name:     "missing driver",
			body:     dropKey("driver"),
			wantSubs: []string{`missing required field "driver"`, "valid values: neo4j-go-v5, neo4j-go-v6"},
		},
		{
			name:     "invalid schema_language",
			body:     setKey("schema_language", "graphql"),
			wantSubs: []string{`line 6: invalid schema_language "graphql" (valid values: gqlc)`},
		},
		{
			name:     "invalid query_language",
			body:     setKey("query_language", "sql"),
			wantSubs: []string{`line 7: invalid query_language "sql" (valid values: opencypher)`},
		},
		{
			name:     "invalid driver",
			body:     setKey("driver", "neo4j-go-v4"),
			wantSubs: []string{`line 8: invalid driver "neo4j-go-v4" (valid values: neo4j-go-v5, neo4j-go-v6)`},
		},
		{
			name:     "sequence-valued driver named as such",
			body:     setKey("driver", "[x]"),
			wantSubs: []string{"line 8: invalid driver: expected a scalar value, got a YAML sequence"},
		},
		{
			name:     "mapping-valued driver named as such",
			body:     setKey("driver", "{a: b}"),
			wantSubs: []string{"line 8: invalid driver: expected a scalar value, got a YAML mapping"},
		},
		{
			name:     "sequence-valued path field carries yaml line info",
			body:     setKey("schema", "[a]"),
			wantSubs: []string{"line 2", "cannot unmarshal !!seq into string"},
		},
		{
			name:     "duplicate key",
			body:     validDoc + "driver: neo4j-go-v5\n",
			wantSubs: []string{`mapping key "driver" already defined`},
		},
		{
			name:     "empty schema",
			body:     setKey("schema", `""`),
			wantSubs: []string{`field "schema" must not be empty`},
		},
		{
			name:     "empty queries",
			body:     setKey("queries", `""`),
			wantSubs: []string{`field "queries" must not be empty`},
		},
		{
			name:     "empty output",
			body:     setKey("output", `""`),
			wantSubs: []string{`field "output" must not be empty`},
		},
		{
			name:     "empty package",
			body:     setKey("package", `""`),
			wantSubs: []string{`field "package" must not be empty`},
		},
		{
			name:     "empty procsig",
			body:     setKey("procsig", `""`),
			wantSubs: []string{`field "procsig" is empty`, "omit the key"},
		},
		{
			name:     "package with hyphen",
			body:     setKey("package", "my-db"),
			wantSubs: []string{`package "my-db" is not a valid Go identifier`},
		},
		{
			name:     "package is a Go keyword",
			body:     setKey("package", "func"),
			wantSubs: []string{`package "func" is not a valid Go identifier`},
		},
		{
			name:     "package starts with a digit",
			body:     setKey("package", "123abc"),
			wantSubs: []string{`package "123abc" is not a valid Go identifier`},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := config.Decode(strings.NewReader(tc.body))
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantSubs)
			}
			msg := err.Error()
			if !strings.HasPrefix(msg, "config: ") {
				t.Errorf("error %q lacks the \"config: \" prefix", msg)
			}
			if !strings.Contains(msg, "<stream>") {
				t.Errorf("error %q does not name the <stream> source", msg)
			}
			for _, sub := range tc.wantSubs {
				if !strings.Contains(msg, sub) {
					t.Errorf("error %q does not contain %q", msg, sub)
				}
			}
		})
	}
}

// TestLoadMissingFile asserts the open error wraps the underlying
// fs error (spec §6.1): a future `gqlc init` branches on
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

// TestLoadErrorsNameThePath asserts Load labels decode-stage errors
// with the file path (not "<stream>").
func TestLoadErrorsNameThePath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.gqlc.yaml")
	if err := os.WriteFile(path, []byte(dropKey("driver")), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	_, err := config.Load(path)
	if err == nil {
		t.Fatal("expected error for missing driver")
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("error %q does not name the source path", err.Error())
	}
}

// TestSaveEmitsFixtureBytes is the Load∘Save byte-identity round trip
// (spec §7): the fixture is the source of truth for the canonical form,
// and Save of the loaded Config must reproduce it exactly.
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
// writes (cli-stage-2 §5.2) — `gqlc init`'s preview/write identity is
// by construction, not parallel encoders — and both still match the
// fixture.
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

// TestSaveLoadRoundTrip is the from-Go direction (`gqlc init`'s path):
// a Go-constructed Config must Save and Load back identically, and an
// empty ProcsigPath must omit the procsig key entirely rather than
// writing the rejected explicit-empty form.
func TestSaveLoadRoundTrip(t *testing.T) {
	cfg := canonicalConfig
	cfg.ProcsigPath = ""
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
	if got != cfg {
		t.Fatalf("round trip drift: got %+v; want %+v", got, cfg)
	}
}
