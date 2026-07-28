package cli

import (
	"os"
	"path/filepath"
	"slices"
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
	return config.Config{Targets: []config.Target{{
		SchemaPath:  "graph.gql",
		SchemaLang:  config.SchemaLangGQL,
		QueryDir:    "cypher",
		QueryLang:   config.QueryLangOpenCypher,
		ProcsigPath: "procs.procsig.json",
		Go: config.GoGen{
			Package: "graphdb",
			Out:     "gen/graphdb",
			Driver:  config.DriverNeo4jGoV6,
		},
	}}}
}

// addFixtureConfig is the config-multi-target §8.2 --add fixture: two
// entries whose prefilled fields diverge where an accessible script can
// observe them. procsig and the driver are the two the script observes —
// both are answered with an empty line, so the value that lands is the
// prefill — and entry 1 is the only source of "order.procsig.json" and
// v6: entry 0 carries neither, and neither do the §3.2 defaults, so an
// append reading the wrong one is caught. The two language axes hold a
// single vocabulary value each and discriminate nothing here.
func addFixtureConfig() config.Config {
	return config.Config{Targets: []config.Target{
		{
			SchemaPath: "user.gql",
			SchemaLang: config.SchemaLangGQL,
			QueryDir:   "cypher/user",
			QueryLang:  config.QueryLangOpenCypher,
			Go: config.GoGen{
				Package: "userdb",
				Out:     "gen/user",
				Driver:  config.DriverNeo4jGoV5,
			},
		},
		{
			SchemaPath:  "order.gql",
			SchemaLang:  config.SchemaLangGQL,
			QueryDir:    "cypher/order",
			QueryLang:   config.QueryLangOpenCypher,
			ProcsigPath: "order.procsig.json",
			Go: config.GoGen{
				Package: "orderdb",
				Out:     "gen/order",
				Driver:  config.DriverNeo4jGoV6,
			},
		},
	}}
}

// addedTarget is the target the --add tests author over
// addFixtureConfig. The four validated Inputs are typed at the prompts,
// schema included — an accessible Input does not display its bound
// default, so re-typing the carried value is how the script answers it,
// and the carry itself is pinned on addPrefill directly. procsig and
// the three Selects are empty lines, so their values here are the
// prefill and nothing else.
func addedTarget() config.Target {
	return config.Target{
		SchemaPath:  "order.gql",
		SchemaLang:  config.SchemaLangGQL,
		QueryDir:    "cypher/report",
		QueryLang:   config.QueryLangOpenCypher,
		ProcsigPath: "order.procsig.json",
		Go: config.GoGen{
			Package: "reportdb",
			Out:     "gen/report",
			Driver:  config.DriverNeo4jGoV6,
		},
	}
}

// wizardScript renders the §7 per-prompt script contract for one full
// wizard pass: explicit value lines for the four validated Inputs
// (derived from the Target the test asserts against, never
// hand-copied, so a fixture edit cannot desynchronise script and
// assertion), an empty line for the unvalidated procsig Input, empty
// lines for the three Selects (the empty line takes the default that
// derives from the pointer binding — the §3.3 prefill seam), and the
// confirm answer.
func wizardScript(t config.Target, confirm string) string {
	return strings.Join([]string{
		t.SchemaPath,
		t.QueryDir,
		t.Go.Out,
		t.Go.Package,
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

// runAdd drives the §8.2 append flow the way runWizard drives the bare
// one, same reader wrapping and same reasons.
func runAdd(t *testing.T, script, cfgPath string) (string, error) {
	t.Helper()
	var out strings.Builder
	err := runInitAdd(iotest.OneByteReader(strings.NewReader(script)), &out, true, cfgPath)
	return out.String(), err
}

func TestInitClassifyConfig(t *testing.T) {
	t.Run("absent file is fresh with defaults", func(t *testing.T) {
		flow, cfg, loadErr := classifyConfig(filepath.Join(t.TempDir(), config.DefaultFilename))
		require.Equal(t, flowFresh, flow)
		require.NoError(t, loadErr)
		require.Equal(t, initDefaults(), cfg)
	})

	t.Run("loadable file is edit with its values", func(t *testing.T) {
		cfgPath := filepath.Join(t.TempDir(), config.DefaultFilename)
		want := editFixtureConfig()
		require.NoError(t, want.Save(cfgPath))
		flow, cfg, loadErr := classifyConfig(cfgPath)
		require.Equal(t, flowEdit, flow)
		require.NoError(t, loadErr)
		require.Equal(t, want, cfg)
	})

	brokenBodies := map[string]string{
		"malformed yaml":      "version: 1\n\tschema: schema.gql\n",
		"bad vocabulary":      brokenBody,
		"unsupported version": "version: 99\n",
	}
	for name, body := range brokenBodies {
		t.Run(name+" is broken with the loader's error", func(t *testing.T) {
			cfgPath := filepath.Join(t.TempDir(), config.DefaultFilename)
			require.NoError(t, os.WriteFile(cfgPath, []byte(body), 0o644))
			_, wantErr := config.Load(cfgPath)
			require.Error(t, wantErr)

			flow, cfg, loadErr := classifyConfig(cfgPath)
			require.Equal(t, flowBroken, flow)
			require.EqualError(t, loadErr, wantErr.Error())
			require.Equal(t, initDefaults(), cfg)
		})
	}

	t.Run("directory at path is broken", func(t *testing.T) {
		dir := t.TempDir()
		flow, _, loadErr := classifyConfig(dir)
		require.Equal(t, flowBroken, flow)
		require.Error(t, loadErr)
		require.Contains(t, loadErr.Error(), dir)
	})
}

// TestInitDefaults pins the §3.2 table exactly, and the rule that the
// enum defaults are Values()[0] — a vocabulary reorder must move the
// default with it, and an appended member must not.
func TestInitDefaults(t *testing.T) {
	require.Equal(t, config.Config{Targets: []config.Target{{
		SchemaPath: "schema.gql",
		SchemaLang: config.SchemaLangGQL,
		QueryDir:   "queries",
		QueryLang:  config.QueryLangOpenCypher,
		Go: config.GoGen{
			Package: "db",
			Out:     "internal/db",
			Driver:  config.DriverNeo4jGoV5,
		},
	}}}, initDefaults())

	got := initDefaults().Targets[0]
	require.Equal(t, config.SchemaLangValues()[0], got.SchemaLang)
	require.Equal(t, config.QueryLangValues()[0], got.QueryLang)
	require.Equal(t, config.DriverValues()[0], got.Go.Driver)
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

// TestInitOutValidator pins the §4.2 non-blank rule order in validateOut:
// validateNonBlank runs before CheckOutAgainst, so a blank or whitespace-only out
// gets "must not be empty" rather than a misleading overlap message or nil.
func TestInitOutValidator(t *testing.T) {
	prior := addFixtureConfig()
	cases := []struct {
		out     string
		wantErr string // empty means valid
	}{
		{out: "", wantErr: "must not be empty"},
		{out: "   ", wantErr: "must not be empty"},
		{out: "gen/newdir", wantErr: ""},
	}
	validate := validateOut(prior)
	for _, tc := range cases {
		t.Run("out "+tc.out, func(t *testing.T) {
			err := validate(tc.out)
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
	cfg := initDefaults().Targets[0]

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

	out, err := runWizard(t, wizardScript(want.Targets[0], "y"), cfgPath)
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
			_, err := runWizard(t, wizardScript(initDefaults().Targets[0], answer), cfgPath)
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

	_, err = runWizard(t, wizardScript(fixture.Targets[0], "y"), cfgPath)
	require.NoError(t, err)

	after, err := os.ReadFile(cfgPath)
	require.NoError(t, err)
	require.Equal(t, string(before), string(after))

	loaded, err := config.Load(cfgPath)
	require.NoError(t, err)
	require.Equal(t, fixture, loaded)
}

// TestInitRefusalNamesAddFlag pins the §8.1 refusal: the wizard
// expresses one target, so a multi-target file is refused before any
// prompt renders, and the parenthetical names the --add flag this
// binary now has. It replaces branch 2's TestInitRefusesMultiTargetEdit
// rather than joining it: the two differ only in the expectation, and
// keeping both would leave the suite asserting two spellings of one
// message. The script is a full accept-everything pass: if the refusal
// ever moved behind the form, the run would reach the confirm and
// rewrite the file with one entry.
func TestInitRefusalNamesAddFlag(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), config.DefaultFilename)
	cfg := editFixtureConfig()
	second := cfg.Targets[0]
	second.Go.Out = "gen/other"
	second.Go.Package = "otherdb"
	cfg.Targets = append(cfg.Targets, second)
	require.NoError(t, cfg.Save(cfgPath))
	before, err := os.ReadFile(cfgPath)
	require.NoError(t, err)

	out, err := runWizard(t, wizardScript(cfg.Targets[0], "y"), cfgPath)
	require.EqualError(t, err, cfgPath+" declares 2 generation targets; init edits only a single-target config (edit it by hand, or run gqlc init --add to append another)")
	require.Empty(t, out)

	after, err := os.ReadFile(cfgPath)
	require.NoError(t, err)
	require.Equal(t, string(before), string(after))
}

// TestInitAddFlag pins the §8.2 flag declaration: name, default and
// usage string. `gqlc init --add` reaching the TTY guard is what proves
// cobra parses it at all — an unregistered flag fails earlier, at parse
// time, with a different message.
func TestInitAddFlag(t *testing.T) {
	f := newInitCmd().Flags().Lookup("add")
	require.NotNil(t, f)
	require.Equal(t, "false", f.DefValue)
	require.Equal(t, "append a generation target to the existing config file", f.Usage)

	stdout, _, err := executeRoot(t, "init", "--add")
	require.EqualError(t, err, "init requires an interactive terminal")
	require.Empty(t, stdout)
}

// TestInitAdd is the §8.2 append round trip.
func TestInitAdd(t *testing.T) {
	t.Run("prefill carries the last entry and blanks the distinguishing three", func(t *testing.T) {
		require.Equal(t, config.Target{
			SchemaPath:  "order.gql",
			SchemaLang:  config.SchemaLangGQL,
			QueryLang:   config.QueryLangOpenCypher,
			ProcsigPath: "order.procsig.json",
			Go:          config.GoGen{Driver: config.DriverNeo4jGoV6},
		}, addPrefill(addFixtureConfig().Targets))
	})

	t.Run("nothing to carry from falls back to the fresh defaults", func(t *testing.T) {
		want := initDefaults().Targets[0]
		want.QueryDir, want.Go.Out, want.Go.Package = "", "", ""
		require.Equal(t, want, addPrefill(nil))
	})

	t.Run("appends to a two-entry file", func(t *testing.T) {
		cfgPath := filepath.Join(t.TempDir(), config.DefaultFilename)
		fixture := addFixtureConfig()
		require.NoError(t, fixture.Save(cfgPath))
		want := config.Config{Targets: append(slices.Clone(fixture.Targets), addedTarget())}

		out, err := runAdd(t, wizardScript(addedTarget(), "y"), cfgPath)
		require.NoError(t, err)

		wantBytes, err := want.Canonical()
		require.NoError(t, err)
		got, err := os.ReadFile(cfgPath)
		require.NoError(t, err)
		require.Equal(t, string(wantBytes), string(got))

		loaded, err := config.Load(cfgPath)
		require.NoError(t, err)
		require.Equal(t, want, loaded)

		// The preview is the whole resulting file, every entry (§8.2).
		require.Contains(t, out, "gqlc init will write "+cfgPath+":\n\n"+string(wantBytes))
		// The warnings and the epilogue name the target this run
		// authored — the appended one, whose query directory is the only
		// one of the three the fixture does not already declare.
		require.Contains(t, out, "warning: query directory "+
			filepath.Join(filepath.Dir(cfgPath), "cypher", "report")+
			" does not exist yet; create it before running gqlc generate\n")
		require.Contains(t, out, "wrote "+cfgPath+"\n"+
			"next steps:\n"+
			"  1. put your schema at order.gql\n"+
			"  2. add *.cypher query files under cypher/report\n"+
			"  3. run gqlc generate\n")
	})

	t.Run("appends a second target to a one-entry file", func(t *testing.T) {
		cfgPath := filepath.Join(t.TempDir(), config.DefaultFilename)
		fixture := editFixtureConfig()
		require.NoError(t, fixture.Save(cfgPath))
		// Derived from the only entry: everything but the three typed
		// fields is what --add must carry, procsig included — and the
		// script answers procsig with an empty line, so a prefill that
		// dropped it would write nothing there.
		second := fixture.Targets[0]
		second.QueryDir = "cypher/second"
		second.Go.Out = "gen/second"
		second.Go.Package = "seconddb"
		want := config.Config{Targets: append(slices.Clone(fixture.Targets), second)}

		_, err := runAdd(t, wizardScript(second, "y"), cfgPath)
		require.NoError(t, err)

		loaded, err := config.Load(cfgPath)
		require.NoError(t, err)
		require.Equal(t, want, loaded)
	})

	t.Run("the seam routes --add to the append flow", func(t *testing.T) {
		// One file and one script, both flows: bare init refuses a
		// two-entry config (§8.1) where --add appends to it, so the
		// dispatch is observable without a terminal.
		cfgPath := filepath.Join(t.TempDir(), config.DefaultFilename)
		require.NoError(t, addFixtureConfig().Save(cfgPath))
		script := wizardScript(addedTarget(), "y")

		var bare strings.Builder
		err := runInit(iotest.OneByteReader(strings.NewReader(script)), &bare, true, false, cfgPath)
		require.ErrorContains(t, err, "init edits only a single-target config")

		var added strings.Builder
		require.NoError(t, runInit(iotest.OneByteReader(strings.NewReader(script)), &added, true, true, cfgPath))
		loaded, err := config.Load(cfgPath)
		require.NoError(t, err)
		require.Len(t, loaded.Targets, 3)
	})
}

// TestInitAddOverlapRejectedAtPrompt: the out prompt runs the loader's
// own overlap rule (config.CheckOutAgainst) against the entries already
// in the file (§8.2). Each case types an overlapping directory, reads
// the rejection, and retypes a disjoint one — a rejection that failed to
// re-prompt would leave that retyped line to the package prompt,
// shifting the answers after it by one.
func TestInitAddOverlapRejectedAtPrompt(t *testing.T) {
	cases := []struct{ name, bad, wantMsg string }{
		{
			name:    "equal to entry 0",
			bad:     "gen/user",
			wantMsg: `out "gen/user" is already graph[0]'s output directory`,
		},
		{
			// Spelled uncleaned, and against the *second* entry: a
			// string-equality restatement of the rule accepts this, and
			// the loader then rejects the file the wizard just wrote.
			name:    "equal to entry 1, spelled differently",
			bad:     "./gen/order/",
			wantMsg: `out "./gen/order/" is already graph[1]'s output directory`,
		},
		{
			name:    "inside entry 1",
			bad:     "gen/order/sub",
			wantMsg: `out "gen/order/sub" is inside graph[1]'s output directory "gen/order"`,
		},
		{
			// The hook sees the raw line, but §4.2's post-form trim is
			// what gets written: a check on the untrimmed value accepts
			// this and writes a file the loader then rejects.
			name:    "equal to entry 0, padded with the whitespace the form trims",
			bad:     "  gen/user  ",
			wantMsg: `out "gen/user" is already graph[0]'s output directory`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfgPath := filepath.Join(t.TempDir(), config.DefaultFilename)
			fixture := addFixtureConfig()
			require.NoError(t, fixture.Save(cfgPath))
			want := config.Config{Targets: append(slices.Clone(fixture.Targets), addedTarget())}

			added := addedTarget()
			script := strings.Join([]string{
				added.SchemaPath,
				added.QueryDir,
				tc.bad,       // rejected at the prompt
				added.Go.Out, // consumed by the re-prompt
				added.Go.Package,
				"", "", "", "", // procsig and the three Selects
				"y",
			}, "\n") + "\n"

			out, err := runAdd(t, script, cfgPath)
			require.NoError(t, err)
			require.Contains(t, out, tc.wantMsg+"\n")

			loaded, err := config.Load(cfgPath)
			require.NoError(t, err)
			require.Equal(t, want, loaded)
		})
	}

	t.Run("input starved after the rejection writes nothing", func(t *testing.T) {
		// huh's PromptString keeps the last scanned line when the next
		// Scan hits EOF (internal/accessibility/accessibility.go:145-164),
		// so the rejected out does reach the bound target; what keeps it
		// off disk is the §5.4 confirm gate defaulting to Abort.
		cfgPath := filepath.Join(t.TempDir(), config.DefaultFilename)
		require.NoError(t, addFixtureConfig().Save(cfgPath))
		before, err := os.ReadFile(cfgPath)
		require.NoError(t, err)

		out, err := runAdd(t, "report.gql\ncypher/report\ngen/user\n", cfgPath)
		require.EqualError(t, err, abortedMsg)
		require.Contains(t, out, `out "gen/user" is already graph[0]'s output directory`+"\n")

		after, err := os.ReadFile(cfgPath)
		require.NoError(t, err)
		require.Equal(t, string(before), string(after))
	})
}

// TestInitAddNoConfig: --add has nothing to append to, so it reports
// the absent-file message §8.2 borrows from generate — before any
// prompt renders.
func TestInitAddNoConfig(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), config.DefaultFilename)

	out, err := runAdd(t, wizardScript(addedTarget(), "y"), cfgPath)
	want := "no config file at " + cfgPath + " (run gqlc init to create one)"
	require.EqualError(t, err, want)
	require.Empty(t, out)
	require.NoFileExists(t, cfgPath)

	// The same bytes generate prints for the same condition, which is
	// the whole point of the shared helper: a second copy of the string
	// satisfies both commands' own expectations and fails here.
	_, stderr, gerr := executeRoot(t, "generate", "-f", cfgPath)
	require.EqualError(t, gerr, want)
	require.Equal(t, "Error: "+want+"\n", stderr)
}

// TestInitAddBrokenConfig: a config that does not load is the loader's
// error verbatim and nothing else. The script opens with the
// start-fresh choice, so a broken-config dialogue wired into this flow
// would answer itself and overwrite the file --add exists to extend.
func TestInitAddBrokenConfig(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), config.DefaultFilename)
	require.NoError(t, os.WriteFile(cfgPath, []byte(brokenBody), 0o644))
	_, wantErr := config.Load(cfgPath)
	require.Error(t, wantErr)

	out, err := runAdd(t, "2\n"+wizardScript(addedTarget(), "y"), cfgPath)
	require.EqualError(t, err, wantErr.Error())
	require.Empty(t, out)

	after, err := os.ReadFile(cfgPath)
	require.NoError(t, err)
	require.Equal(t, brokenBody, string(after))
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
					cfg.Targets[0].SchemaLang = sl
					cfg.Targets[0].QueryLang = ql
					cfg.Targets[0].Go.Driver = dr
					require.NoError(t, cfg.Save(cfgPath))

					_, err := runWizard(t, wizardScript(cfg.Targets[0], "y"), cfgPath)
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
graph:
  - gen:
      go:
        driver: "neo4j-go-v6"
        out: 'internal/db'
        package: db
    queries: queries
    schema: schema.gql # the schema
    query_language: opencypher
    schema_language: gql
version: 1
`
	require.NoError(t, os.WriteFile(cfgPath, []byte(nonCanonical), 0o644))
	loaded, err := config.Load(cfgPath)
	require.NoError(t, err)

	out, err := runWizard(t, wizardScript(loaded.Targets[0], "y"), cfgPath)
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
	"graph:\n" +
	"  - schema: schema.gql\n" +
	"    schema_language: gql\n" +
	"    queries: queries\n" +
	"    query_language: opencypher\n" +
	"    gen:\n" +
	"      go:\n" +
	"        package: db\n" +
	"        out: internal/db\n" +
	"        driver: neo4j-go-v4\n"

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

	_, err := runWizard(t, "2\n"+wizardScript(want.Targets[0], "y"), cfgPath)
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
