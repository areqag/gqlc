package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/config"
)

// abortedMsg is the §2.2 pinned abort error, spelled out so a drift
// in errInitAborted fails against the spec, not against itself.
const abortedMsg = "init aborted: no file written"

// editFixtureConfig is the acceptance-criterion-2 shape (v6 driver,
// procsig present) with non-default paths, so an edit run that
// silently fell back to the §3.2 defaults cannot pass.
func editFixtureConfig() config.Config {
	return config.Config{
		SchemaPath:    "graph.gql",
		QueryDir:      "cypher",
		OutputDir:     "gen/graphdb",
		OutputPackage: "graphdb",
		ProcsigPath:   "procs.procsig.json",
		SchemaLang:    config.SchemaLangGQLC,
		QueryLang:     config.QueryLangOpenCypher,
		Driver:        config.DriverNeo4jGoV6,
	}
}

// wizardScript renders the §7 per-prompt script contract for one full
// wizard pass: explicit value lines for the four validated Inputs
// (derived from the Config the test asserts against, never
// hand-copied, so a fixture edit cannot desynchronise script and
// assertion), an empty line for the unvalidated procsig Input, empty
// lines for the three Selects (the empty line takes the default that
// derives from the pointer binding — the §3.3 prefill seam), and the
// confirm answer.
func wizardScript(cfg config.Config, confirm string) string {
	return strings.Join([]string{
		cfg.SchemaPath,
		cfg.QueryDir,
		cfg.OutputDir,
		cfg.OutputPackage,
		"", // procsig: empty accepts the bound default
		"", // schema_language: empty accepts the prefilled Select default
		"", // query_language
		"", // driver
		confirm,
	}, "\n") + "\n"
}

// runWizard drives runInitWizard in accessible mode with a scripted
// reader (§7 layer 2). The OneByteReader wrap is load-bearing: every
// accessible prompt re-reads the shared reader through a fresh
// bufio.Scanner, so a buffered read-ahead would swallow the rest of
// the script.
func runWizard(t *testing.T, script, cfgPath string) (string, error) {
	t.Helper()
	var out strings.Builder
	err := runInitWizard(iotest.OneByteReader(strings.NewReader(script)), &out, true, cfgPath)
	return out.String(), err
}

func TestInitClassifyTarget(t *testing.T) {
	t.Run("absent file is fresh with defaults", func(t *testing.T) {
		flow, cfg, loadErr := classifyTarget(filepath.Join(t.TempDir(), config.DefaultFilename))
		require.Equal(t, flowFresh, flow)
		require.NoError(t, loadErr)
		require.Equal(t, initDefaults(), cfg)
	})

	t.Run("loadable file is edit with its values", func(t *testing.T) {
		cfgPath := filepath.Join(t.TempDir(), config.DefaultFilename)
		want := editFixtureConfig()
		require.NoError(t, want.Save(cfgPath))
		flow, cfg, loadErr := classifyTarget(cfgPath)
		require.Equal(t, flowEdit, flow)
		require.NoError(t, loadErr)
		require.Equal(t, want, cfg)
	})

	brokenBodies := map[string]string{
		"malformed yaml":      "version: 1\n\tschema: schema.gql\n",
		"bad vocabulary":      "version: 1\nschema: s.gql\nqueries: q\noutput: o\npackage: db\nschema_language: gqlc\nquery_language: opencypher\ndriver: neo4j-go-v4\n",
		"unsupported version": "version: 99\n",
	}
	for name, body := range brokenBodies {
		t.Run(name+" is broken with the loader's error", func(t *testing.T) {
			cfgPath := filepath.Join(t.TempDir(), config.DefaultFilename)
			require.NoError(t, os.WriteFile(cfgPath, []byte(body), 0o644))
			_, wantErr := config.Load(cfgPath)
			require.Error(t, wantErr)

			flow, cfg, loadErr := classifyTarget(cfgPath)
			require.Equal(t, flowBroken, flow)
			require.EqualError(t, loadErr, wantErr.Error())
			require.Equal(t, initDefaults(), cfg)
		})
	}

	t.Run("directory at path is broken", func(t *testing.T) {
		dir := t.TempDir()
		flow, _, loadErr := classifyTarget(dir)
		require.Equal(t, flowBroken, flow)
		require.Error(t, loadErr)
		require.Contains(t, loadErr.Error(), dir)
	})
}

// TestInitDefaults pins the §3.2 table exactly, and the rule that the
// enum defaults are Values()[0] — a vocabulary reorder must move the
// default with it, and an appended member must not.
func TestInitDefaults(t *testing.T) {
	require.Equal(t, config.Config{
		SchemaPath:    "schema.gql",
		QueryDir:      "queries",
		OutputDir:     "internal/db",
		OutputPackage: "db",
		SchemaLang:    config.SchemaLangGQLC,
		QueryLang:     config.QueryLangOpenCypher,
		Driver:        config.DriverNeo4jGoV5,
	}, initDefaults())

	got := initDefaults()
	require.Equal(t, config.SchemaLangValues()[0], got.SchemaLang)
	require.Equal(t, config.QueryLangValues()[0], got.QueryLang)
	require.Equal(t, config.DriverValues()[0], got.Driver)
}

// TestInitPackageValidator maps each probe to its §4.3 clause: blank,
// Go-identifier (keywords and leading digits included), and codegen's
// emission grammar. "Db" and "über" pass IsIdentifier but fail the
// grammar; "func" matches the grammar but is a keyword.
func TestInitPackageValidator(t *testing.T) {
	cases := []struct {
		pkg     string
		wantErr string // empty means valid
	}{
		{pkg: "", wantErr: "must not be empty"},
		{pkg: "   ", wantErr: "must not be empty"},
		{pkg: "db", wantErr: ""},
		{pkg: "db_1", wantErr: ""},
		{pkg: "Db", wantErr: `package "Db" will fail gqlc generate (must match ^[a-z][a-z0-9_]*$)`},
		{pkg: "func", wantErr: `package "func" is not a valid Go identifier`},
		{pkg: "1db", wantErr: `package "1db" is not a valid Go identifier`},
		{pkg: "über", wantErr: `package "über" will fail gqlc generate (must match ^[a-z][a-z0-9_]*$)`},
	}
	for _, tc := range cases {
		t.Run("package "+tc.pkg, func(t *testing.T) {
			err := validatePackage(tc.pkg)
			if tc.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.EqualError(t, err, tc.wantErr)
		})
	}
}

// TestInitCommentDetection pins the §5.3 byte scan: any '#' in the old
// file's raw bytes triggers the notice — a '#' inside a quoted scalar
// is the honest false positive, one harmless line.
func TestInitCommentDetection(t *testing.T) {
	const notice = "note: comments in gqlc.yaml will not survive; gqlc init writes the canonical form\n"
	cases := []struct {
		name string
		raw  string
		want bool
	}{
		{name: "comment line", raw: "# hand-tuned\nversion: 1\n", want: true},
		{name: "hash inside quoted value (honest false positive)", raw: "schema: \"a#b.gql\"\n", want: true},
		{name: "no hash", raw: "version: 1\nschema: schema.gql\n", want: false},
		{name: "no raw bytes (absent file)", raw: "", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := previewBlock("gqlc.yaml", []byte("version: 1\n"), []byte(tc.raw))
			if tc.want {
				require.Contains(t, out, notice)
			} else {
				require.NotContains(t, out, notice)
			}
		})
	}
}

// TestInitPreviewBlock pins the §5.3 shape byte-exactly: header, blank
// line, the canonical bytes verbatim (ending in the encoder's own
// trailing newline), and the notice line iff flagged.
func TestInitPreviewBlock(t *testing.T) {
	canonical, err := initDefaults().Canonical()
	require.NoError(t, err)

	t.Run("without notice", func(t *testing.T) {
		want := "gqlc init will write gqlc.yaml:\n\n" + string(canonical)
		require.Equal(t, want, previewBlock("gqlc.yaml", canonical, nil))
	})

	t.Run("with notice", func(t *testing.T) {
		want := "gqlc init will write gqlc.yaml:\n\n" + string(canonical) +
			"note: comments in gqlc.yaml will not survive; gqlc init writes the canonical form\n"
		require.Equal(t, want, previewBlock("gqlc.yaml", canonical, []byte("# c\n")))
	})
}

// TestInitWarningsAndEpilogue pins the §5.5 warning lines (resolved
// against the config file's directory, the way generate resolves
// paths) and the §5.6 epilogue (config values as written, unresolved).
func TestInitWarningsAndEpilogue(t *testing.T) {
	proj := filepath.Join(t.TempDir(), "proj")
	require.NoError(t, os.MkdirAll(proj, 0o755))
	cfgPath := filepath.Join(proj, config.DefaultFilename)
	cfg := initDefaults()

	t.Run("both inputs missing", func(t *testing.T) {
		want := "warning: schema file " + filepath.Join(proj, "schema.gql") +
			" does not exist yet; create it before running gqlc generate\n" +
			"warning: query directory " + filepath.Join(proj, "queries") +
			" does not exist yet; create it before running gqlc generate\n"
		require.Equal(t, want, warningsText(cfgPath, cfg))
	})

	t.Run("query directory missing only", func(t *testing.T) {
		require.NoError(t, os.WriteFile(filepath.Join(proj, "schema.gql"), []byte("x"), 0o644))
		want := "warning: query directory " + filepath.Join(proj, "queries") +
			" does not exist yet; create it before running gqlc generate\n"
		require.Equal(t, want, warningsText(cfgPath, cfg))
	})

	t.Run("both present warn nothing", func(t *testing.T) {
		require.NoError(t, os.MkdirAll(filepath.Join(proj, "queries"), 0o755))
		require.Empty(t, warningsText(cfgPath, cfg))
	})

	t.Run("epilogue", func(t *testing.T) {
		want := "wrote " + cfgPath + "\n" +
			"next steps:\n" +
			"  1. put your schema at schema.gql\n" +
			"  2. add *.cypher query files under queries\n" +
			"  3. run gqlc generate\n"
		require.Equal(t, want, epilogueText(cfgPath, cfg))
	})
}

// TestInitNonTTY: under `go test` the process's stdin is not a
// terminal, so the §2.1 guard fires deterministically in-process.
func TestInitNonTTY(t *testing.T) {
	stdout, stderr, err := executeRoot(t, "init")
	require.Error(t, err)
	require.EqualError(t, err, "init requires an interactive terminal")
	require.Equal(t, "Error: init requires an interactive terminal\n", stderr)
	require.Empty(t, stdout)
}

// TestInitFreshWritesCanonical is the fresh-flow end-to-end: the §7
// script over the §3.2 defaults, `y` at confirm → the file holds
// Canonical() of the defaults, Load round-trips it, and the preview,
// warnings, and epilogue all landed on the wizard's writer (exit-0
// path: runInitWizard returns nil).
func TestInitFreshWritesCanonical(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), config.DefaultFilename)
	want := initDefaults()

	out, err := runWizard(t, wizardScript(want, "y"), cfgPath)
	require.NoError(t, err)

	wantBytes, err := want.Canonical()
	require.NoError(t, err)
	got, err := os.ReadFile(cfgPath)
	require.NoError(t, err)
	require.Equal(t, string(wantBytes), string(got))

	loaded, err := config.Load(cfgPath)
	require.NoError(t, err)
	require.Equal(t, want, loaded)

	require.Contains(t, out, "gqlc init will write "+cfgPath+":\n\n"+string(wantBytes))
	require.Contains(t, out, "warning: schema file ")
	require.Contains(t, out, "warning: query directory ")
	require.Contains(t, out, "wrote "+cfgPath+"\n")
}

// TestInitFreshDecline: Abort at the confirm — explicit `n` or the
// empty line that takes the bound false default — yields the pinned
// abort error and creates nothing (§2.2, §5.4).
func TestInitFreshDecline(t *testing.T) {
	for _, answer := range []string{"n", ""} {
		name := "explicit n"
		if answer == "" {
			name = "empty line takes the Abort default"
		}
		t.Run(name, func(t *testing.T) {
			cfgPath := filepath.Join(t.TempDir(), config.DefaultFilename)
			_, err := runWizard(t, wizardScript(initDefaults(), answer), cfgPath)
			require.EqualError(t, err, abortedMsg)
			require.NoFileExists(t, cfgPath)
		})
	}
}

// TestInitEditPrefillRoundTrip: an accept-everything edit over a
// loadable config (v6 driver, procsig present) rewrites the file
// byte-identically. The empty-line answers prove the prefill: procsig
// keeps the stored value through the accessible default substitution,
// and the v6 driver — index 1 of its vocabulary — survives the Select
// default derivation (§3.3).
func TestInitEditPrefillRoundTrip(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), config.DefaultFilename)
	fixture := editFixtureConfig()
	require.NoError(t, fixture.Save(cfgPath))
	before, err := os.ReadFile(cfgPath)
	require.NoError(t, err)

	_, err = runWizard(t, wizardScript(fixture, "y"), cfgPath)
	require.NoError(t, err)

	after, err := os.ReadFile(cfgPath)
	require.NoError(t, err)
	require.Equal(t, string(before), string(after))

	loaded, err := config.Load(cfgPath)
	require.NoError(t, err)
	require.Equal(t, fixture, loaded)
}

// TestInitEditVocabularyPrefill is the §3.3 fence against the Select
// index-0 clobber: every vocabulary member of every axis, stored in a
// config, survives an edit whose script empty-line-accepts the Select
// defaults. If a prefilled Select ever fell back to index 0, every
// non-first member would be rewritten and fail here.
func TestInitEditVocabularyPrefill(t *testing.T) {
	for _, sl := range config.SchemaLangValues() {
		for _, ql := range config.QueryLangValues() {
			for _, dr := range config.DriverValues() {
				name := string(sl) + "/" + string(ql) + "/" + string(dr)
				t.Run(name, func(t *testing.T) {
					cfgPath := filepath.Join(t.TempDir(), config.DefaultFilename)
					cfg := initDefaults()
					cfg.SchemaLang, cfg.QueryLang, cfg.Driver = sl, ql, dr
					require.NoError(t, cfg.Save(cfgPath))

					_, err := runWizard(t, wizardScript(cfg, "y"), cfgPath)
					require.NoError(t, err)

					loaded, err := config.Load(cfgPath)
					require.NoError(t, err)
					require.Equal(t, cfg, loaded)
				})
			}
		}
	}
}

// TestInitCanonicalisesNonCanonical is the §5.7 migration claim in its
// testable v1 instance: a loadable file with comments, reordered keys,
// and quoted scalars comes out of an edit run in canonical bytes, with
// the comment notice shown.
func TestInitCanonicalisesNonCanonical(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), config.DefaultFilename)
	const nonCanonical = `# hand-written config
driver: "neo4j-go-v6"
version: 1
package: db
output: 'internal/db'
queries: queries
schema: schema.gql # the schema
query_language: opencypher
schema_language: gqlc
`
	require.NoError(t, os.WriteFile(cfgPath, []byte(nonCanonical), 0o644))
	loaded, err := config.Load(cfgPath)
	require.NoError(t, err)

	out, err := runWizard(t, wizardScript(loaded, "y"), cfgPath)
	require.NoError(t, err)

	want, err := loaded.Canonical()
	require.NoError(t, err)
	got, err := os.ReadFile(cfgPath)
	require.NoError(t, err)
	require.Equal(t, string(want), string(got))
	require.Contains(t, out,
		"note: comments in "+cfgPath+" will not survive; gqlc init writes the canonical form\n")
}

// brokenBody is a §3.4 fixture: loadable YAML, out-of-vocabulary
// driver — the loader's verdict carries line info and the vocabulary.
const brokenBody = "version: 1\n" +
	"schema: schema.gql\n" +
	"queries: queries\n" +
	"output: internal/db\n" +
	"package: db\n" +
	"schema_language: gqlc\n" +
	"query_language: opencypher\n" +
	"driver: neo4j-go-v4\n"

// TestInitBrokenAbort: the loader's error verbatim, then the dialogue
// whose default — taken here by an empty line — is abort: pinned
// error, file byte-untouched.
func TestInitBrokenAbort(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), config.DefaultFilename)
	require.NoError(t, os.WriteFile(cfgPath, []byte(brokenBody), 0o644))
	_, wantErr := config.Load(cfgPath)
	require.Error(t, wantErr)

	out, err := runWizard(t, "\n", cfgPath)
	require.EqualError(t, err, abortedMsg)
	require.Contains(t, out, wantErr.Error()+"\n")

	after, err := os.ReadFile(cfgPath)
	require.NoError(t, err)
	require.Equal(t, brokenBody, string(after))
}

// TestInitBrokenFresh: choosing start-fresh (option 2) runs the
// defaults wizard — no salvage, no partial prefill — and the confirmed
// write overwrites with the canonical defaults.
func TestInitBrokenFresh(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), config.DefaultFilename)
	require.NoError(t, os.WriteFile(cfgPath, []byte(brokenBody), 0o644))
	want := initDefaults()

	_, err := runWizard(t, "2\n"+wizardScript(want, "y"), cfgPath)
	require.NoError(t, err)

	wantBytes, err := want.Canonical()
	require.NoError(t, err)
	got, err := os.ReadFile(cfgPath)
	require.NoError(t, err)
	require.Equal(t, string(wantBytes), string(got))
}

// TestInitInputStarvation pins the §5.4 structural property: on input
// EOF the accessible prompts return their defaults without validating,
// the defaults cascade to the confirm, the confirm's bound false
// default answers Abort — an input-starved run can never write.
func TestInitInputStarvation(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), config.DefaultFilename)
	var out strings.Builder
	err := runInitWizard(iotest.OneByteReader(strings.NewReader("")), &out, true, cfgPath)
	require.EqualError(t, err, abortedMsg)
	require.NoFileExists(t, cfgPath)
}
