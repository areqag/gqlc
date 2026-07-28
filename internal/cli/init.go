package cli

import (
	"bytes"
	"errors"
	"fmt"
	"go/token"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"charm.land/huh/v2"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/areqag/gqlc/internal/config"
)

// errInitAborted is the one pinned abort error (CLI-2 spec §2.2):
// exit 0 must certify "the config file exists now", so every abort —
// confirm decline, broken-dialogue abort, Ctrl-C — exits 1 with this
// message.
var errInitAborted = errors.New("init aborted: no file written")

// packageIdentPattern is codegen's emission grammar
// (internal/codegen/prepare.go packageIdent); the wizard enforces it
// so a config it writes is never one gqlc generate rejects (§4.3).
var packageIdentPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// newInitCmd builds the init command: the interactive config wizard
// (CLI-2 spec). TTY-gated; there is no non-interactive mode (§8).
func newInitCmd() *cobra.Command {
	var (
		cfgPath string
		add     bool
	)
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Create or update the gqlc config file interactively",
		Long: `init creates or updates the gqlc config file through an interactive
wizard: it prompts for the schema path, the query directory, the
output directory and package name, and the three tool axes, shows the
exact file it will write, and writes only after confirmation.

init writes the config file and nothing else — it never creates the
schema file, the query directory, or the output directory — and it
requires an interactive terminal.

Set ACCESSIBLE (any non-empty value) for a screen-reader-friendly
numbered-prompt mode.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cmd.SilenceUsage = true
			// Stdin and stderr are the two fds the wizard uses: answers
			// come from stdin, rendering lands on stderr. Stdout is
			// deliberately unchecked — it is empty in every init path,
			// so redirecting it is harmless (§2.1).
			if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stderr.Fd())) {
				return errors.New("init requires an interactive terminal")
			}
			// huh does not read ACCESSIBLE itself; wiring the env var is
			// the caller's job, any non-empty value enabling — the
			// upstream convention, adopted verbatim (§2.1).
			accessible := os.Getenv("ACCESSIBLE") != ""
			err := runInit(cmd.InOrStdin(), cmd.ErrOrStderr(), accessible, add, cfgPath)
			if errors.Is(err, huh.ErrUserAborted) {
				return errInitAborted
			}
			return err
		},
	}
	cmd.Flags().StringVarP(&cfgPath, "file", "f", config.DefaultFilename,
		"path to the config file to create or update")
	cmd.Flags().BoolVar(&add, "add", false,
		"append a generation target to the existing config file")
	return cmd
}

// runInit picks the flow the flags ask for. The two differ only in what
// starting state they accept and what they bind the form to; everything
// from the form onward is runTargetForm.
func runInit(in io.Reader, errOut io.Writer, accessible, add bool, cfgPath string) error {
	if add {
		return runInitAdd(in, errOut, accessible, cfgPath)
	}
	return runInitWizard(in, errOut, accessible, cfgPath)
}

// initFlow is the §3.1 classification of the wizard's starting state.
type initFlow int

const (
	flowFresh initFlow = iota
	flowEdit
	flowBroken
)

// classifyConfig makes the single config.Load attempt that selects
// the flow (§3.1). loadErr is non-nil only for flowBroken; init never
// second-guesses the loader's verdict.
func classifyConfig(cfgPath string) (initFlow, config.Config, error) {
	cfg, err := config.Load(cfgPath)
	switch {
	case err == nil:
		return flowEdit, cfg, nil
	case errors.Is(err, fs.ErrNotExist):
		return flowFresh, initDefaults(), nil
	default:
		return flowBroken, initDefaults(), err
	}
}

// initDefaults is the §3.2 fresh-flow Config: one generation target,
// its path and package defaults mirroring the canonical fixture; enum
// defaults are the first member of each *Values() slice by rule, so
// appending a vocabulary member never silently changes a default.
func initDefaults() config.Config {
	return config.Config{Targets: []config.Target{{
		SchemaPath: "schema.gql",
		SchemaLang: config.SchemaLangValues()[0],
		QueryDir:   "queries",
		QueryLang:  config.QueryLangValues()[0],
		Go: config.GoGen{
			Package: "db",
			Out:     "internal/db",
			Driver:  config.DriverValues()[0],
		},
	}}}
}

// runInitWizard is the whole interactive body of a bare `init` behind
// one seam (§2.1): tests drive it directly with accessible=true and a
// scripted reader.
func runInitWizard(in io.Reader, errOut io.Writer, accessible bool, cfgPath string) error {
	flow, cfg, loadErr := classifyConfig(cfgPath)
	// Before any form renders (§8.1): the wizard expresses one target,
	// so prefilling from the first and writing the canonical form would
	// silently delete the rest. Testing != 1 rather than > 1 is what
	// carries this path to runTargetForm's index without resting on the
	// loader's rejection of an empty graph, a cross-package invariant;
	// --add reaches that index by its own route, where the append
	// supplies the entry.
	if len(cfg.Targets) != 1 {
		return fmt.Errorf("%s declares %d generation targets; init edits only a single-target config (edit it by hand, or run gqlc init --add to append another)", cfgPath, len(cfg.Targets))
	}

	if flow == flowBroken {
		fresh, err := runBrokenDialogue(in, errOut, accessible, cfgPath, loadErr)
		if err != nil {
			return err
		}
		if !fresh {
			return errInitAborted
		}
	}
	// No prior entries: the file's one target is the one being edited, so
	// including it would make its own out an operand in its own check —
	// an unchanged output directory then reads as a collision with
	// itself (TestInitEditPrefillRoundTrip).
	return runTargetForm(in, errOut, accessible, cfgPath, cfg, config.Config{})
}

// runInitAdd is the §8.2 `--add` flow. Flow selection is stricter than
// a bare init's because appending presupposes a file that loads: there
// is no fresh flow, and no broken-config dialogue, whose "start fresh"
// would replace the file the flag promises to append to.
func runInitAdd(in io.Reader, errOut io.Writer, accessible bool, cfgPath string) error {
	loaded, err := config.Load(cfgPath)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return missingConfig(cfgPath)
	case err != nil:
		return err
	}
	cfg := config.Config{Targets: append(slices.Clone(loaded.Targets), addPrefill(loaded.Targets))}
	// loaded, not cfg: the appended target must not overlap-check
	// against itself.
	return runTargetForm(in, errOut, accessible, cfgPath, cfg, loaded)
}

// addPrefill is the §8.2 prefill for an appended target: the last
// entry's values for the fields a second target usually shares, and
// empty for the three fields a second target normally gives its own.
// Only gen.go.out is enforced; shared query directories and package
// names are legal.
//
// With no entry to carry from there is nothing shared to carry, so the
// §3.2 defaults stand in. The loader rejects a config declaring no
// target, but that is the loader's invariant to keep rather than one
// this function indexes on.
func addPrefill(targets []config.Target) config.Target {
	t := initDefaults().Targets[0]
	if n := len(targets); n > 0 {
		t = targets[n-1]
	}
	t.QueryDir = ""
	t.Go.Out = ""
	t.Go.Package = ""
	return t
}

// runTargetForm is the tail both flows share (§4 through §5.6): the
// form, the preview of the whole resulting file, the confirm gate, the
// write, and the trailer. The target a run authors is cfg's last entry
// either way — the only one under a bare init, the appended one under
// --add — and it is that target the warnings and the epilogue name.
//
// prior holds the entries a proposed output directory must not overlap
// (§8.2). The cfg.Save call is the only filesystem mutation in the
// command, unreachable except through the confirm gate (§5.4).
func runTargetForm(in io.Reader, errOut io.Writer, accessible bool, cfgPath string, cfg, prior config.Config) error {
	target := &cfg.Targets[len(cfg.Targets)-1]
	// Raw bytes feed only the §5.3 comment notice; absence or
	// unreadability simply means no comment scan (§3.1).
	raw, _ := os.ReadFile(cfgPath) //nolint:errcheck // §3.1: read errors mean no comment scan, nothing more

	if err := runForm(newWizardForm(target, prior), in, errOut, accessible); err != nil {
		return err
	}
	// One post-form trim: huh's accessible prompts trim their returns
	// and tea mode does not, so without this the two display modes
	// would diverge (§4.2).
	target.SchemaPath = strings.TrimSpace(target.SchemaPath)
	target.QueryDir = strings.TrimSpace(target.QueryDir)
	target.Go.Out = strings.TrimSpace(target.Go.Out)
	target.Go.Package = strings.TrimSpace(target.Go.Package)
	target.ProcsigPath = strings.TrimSpace(target.ProcsigPath)

	canonical, err := cfg.Canonical()
	if err != nil {
		return err
	}
	// The preview is a raw print between two forms, not a form field:
	// huh's Note mangles underscored keys through its markdown renderer
	// and drops computed descriptions in accessible mode (§5.1).
	if _, err := fmt.Fprint(errOut, previewBlock(cfgPath, canonical, raw)); err != nil {
		return err
	}

	write := false
	confirm := huh.NewForm(huh.NewGroup(
		huh.NewConfirm().
			Title("Write " + cfgPath + "?").
			Affirmative("Write").
			Negative("Abort").
			Value(&write),
	))
	if err := runForm(confirm, in, errOut, accessible); err != nil {
		return err
	}
	if !write {
		return errInitAborted
	}
	if err := cfg.Save(cfgPath); err != nil {
		return err
	}
	_, err = fmt.Fprint(errOut, warningsText(cfgPath, *target)+epilogueText(cfgPath, *target))
	return err
}

// runBrokenDialogue is the §3.4 contract: the loader's error verbatim
// as one diagnostic line, then exactly two choices. fresh starts
// false, so abort is the default-highlighted choice — the destructive
// option is never one accidental Enter away.
func runBrokenDialogue(in io.Reader, errOut io.Writer, accessible bool, cfgPath string, loadErr error) (bool, error) {
	if _, err := fmt.Fprintln(errOut, loadErr); err != nil {
		return false, err
	}
	fresh := false
	form := huh.NewForm(huh.NewGroup(
		huh.NewSelect[bool]().
			Title("The config file cannot be loaded. Start fresh?").
			Options(
				huh.NewOption("abort — leave "+cfgPath+" untouched", false),
				huh.NewOption("start fresh — rebuild "+cfgPath+" from defaults", true),
			).
			Value(&fresh),
	))
	if err := runForm(form, in, errOut, accessible); err != nil {
		return false, err
	}
	return fresh, nil
}

// newWizardForm builds the §4 form: one huh.Form, two groups — files
// and package, then the tool axes. Selects source their options from
// the *Values() vocabularies mechanically, so display and wire value
// cannot drift and a new vocabulary member is an option before it can
// be a stored value — which is why an edit prefill needs no defensive
// vocabulary re-check (§3.3).
func newWizardForm(t *config.Target, prior config.Config) *huh.Form {
	return huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Schema file").
				Description("Path to the graph schema. Relative paths resolve against the config file's directory.").
				Validate(validateNonBlank).
				Value(&t.SchemaPath),
			huh.NewInput().
				Title("Query directory").
				Description("Directory holding *.cypher query files.").
				Validate(validateNonBlank).
				Value(&t.QueryDir),
			huh.NewInput().
				Title("Output directory").
				Description("Owned exclusively by gqlc: generate replaces its contents.").
				Validate(validateOut(prior)).
				Value(&t.Go.Out),
			huh.NewInput().
				Title("Package name").
				Description("Go package name for the generated code.").
				Validate(validatePackage).
				Value(&t.Go.Package),
			huh.NewInput().
				Title("Procedure registry (optional)").
				Description("Path to a procsig file; leave empty for none.").
				Value(&t.ProcsigPath),
		),
		huh.NewGroup(
			huh.NewSelect[config.SchemaLang]().
				Title("Schema language").
				Options(huh.NewOptions(config.SchemaLangValues()...)...).
				Value(&t.SchemaLang),
			huh.NewSelect[config.QueryLang]().
				Title("Query language").
				Options(huh.NewOptions(config.QueryLangValues()...)...).
				Value(&t.QueryLang),
			huh.NewSelect[config.Driver]().
				Title("Driver").
				Options(huh.NewOptions(config.DriverValues()...)...).
				Value(&t.Go.Driver),
		),
	)
}

// runForm applies the §2.3 wiring to every form: input and output
// explicit (the output override matters in accessible mode, where huh
// defaults to stdout), and WithAccessible only when accessible holds —
// an unconditional WithAccessible(false) would clobber huh's own
// TERM=dumb auto-enable.
func runForm(form *huh.Form, in io.Reader, errOut io.Writer, accessible bool) error {
	form = form.WithInput(in).WithOutput(errOut)
	if accessible {
		form = form.WithAccessible(true)
	}
	return form.Run()
}

// validateNonBlank is the §4.2 non-blank rule for the path Inputs.
func validateNonBlank(s string) error {
	if strings.TrimSpace(s) == "" {
		return errors.New("must not be empty")
	}
	return nil
}

// validateOut is the §4.2 non-blank rule plus the §8.2 overlap check
// against prior's entries. It calls the loader's own rule rather than
// restating it: a second implementation drifts, and the first thing it
// drifts on is a nested directory whose relative path opens with the
// two characters ".." (config-multi-target §4.3).
func validateOut(prior config.Config) func(string) error {
	return func(s string) error {
		if err := validateNonBlank(s); err != nil {
			return err
		}
		// The trimmed value is the one §4.2's post-form trim will write,
		// so it is the one the check has to see.
		return prior.CheckOutAgainst(strings.TrimSpace(s))
	}
}

// validatePackage enforces the §4.3 gates in order: non-blank, the
// loader's own identifier posture, then codegen's stricter emission
// grammar. Neither identifier check subsumes the other: "Db" passes
// IsIdentifier and fails the grammar; "func" matches the grammar and
// fails IsIdentifier.
func validatePackage(s string) error {
	if err := validateNonBlank(s); err != nil {
		return err
	}
	if !token.IsIdentifier(s) {
		return fmt.Errorf("package %q is not a valid Go identifier", s)
	}
	if !packageIdentPattern.MatchString(s) {
		return fmt.Errorf("package %q will fail gqlc generate (must match ^[a-z][a-z0-9_]*$)", s)
	}
	return nil
}

// previewBlock renders the §5.3 preview: the exact canonical bytes
// Save will write, raw and unstyled so both display modes show the
// same bytes. The comment notice fires on a plain byte scan of the
// old file: a YAML comment necessarily contains '#', so a comment is
// never missed; a '#' inside a quoted scalar costs one harmless
// notice line.
func previewBlock(cfgPath string, canonical, raw []byte) string {
	s := "gqlc init will write " + cfgPath + ":\n\n" + string(canonical)
	if bytes.ContainsRune(raw, '#') {
		s += "note: comments in " + cfgPath + " will not survive; gqlc init writes the canonical form\n"
	}
	return s
}

// warningsText renders the §5.5 soft warnings: one line per missing
// project input, resolved the way generate resolves paths (relative
// to the config file's directory). The output directory (generate
// creates and owns it, ADR 0012) and the procsig path are
// deliberately unchecked.
func warningsText(cfgPath string, t config.Target) string {
	baseDir := filepath.Dir(cfgPath)
	var b strings.Builder
	schemaPath := resolvePath(baseDir, t.SchemaPath)
	if _, err := os.Stat(schemaPath); errors.Is(err, fs.ErrNotExist) {
		b.WriteString("warning: schema file " + schemaPath + " does not exist yet; create it before running gqlc generate\n")
	}
	queryDir := resolvePath(baseDir, t.QueryDir)
	if _, err := os.Stat(queryDir); errors.Is(err, fs.ErrNotExist) {
		b.WriteString("warning: query directory " + queryDir + " does not exist yet; create it before running gqlc generate\n")
	}
	return b.String()
}

// epilogueText renders the §5.6 epilogue; schema and queries are the
// config values as written (the user's own words, file-relative), not
// resolved paths.
func epilogueText(cfgPath string, t config.Target) string {
	return "wrote " + cfgPath + "\n" +
		"next steps:\n" +
		"  1. put your schema at " + t.SchemaPath + "\n" +
		"  2. add *.cypher query files under " + t.QueryDir + "\n" +
		"  3. run gqlc generate\n"
}
