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
	var cfgPath string
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
			err := runInitWizard(cmd.InOrStdin(), cmd.ErrOrStderr(), accessible, cfgPath)
			if errors.Is(err, huh.ErrUserAborted) {
				return errInitAborted
			}
			return err
		},
	}
	cmd.Flags().StringVarP(&cfgPath, "file", "f", config.DefaultFilename,
		"path to the config file to create or update")
	return cmd
}

// initFlow is the §3.1 classification of the wizard's starting state.
type initFlow int

const (
	flowFresh initFlow = iota
	flowEdit
	flowBroken
)

// classifyTarget makes the single config.Load attempt that selects
// the flow (§3.1). loadErr is non-nil only for flowBroken; init never
// second-guesses the loader's verdict.
func classifyTarget(cfgPath string) (initFlow, config.Config, error) {
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

// initDefaults is the §3.2 fresh-flow Config: path and package
// defaults mirror the canonical fixture; enum defaults are the first
// member of each *Values() slice by rule, so appending a vocabulary
// member never silently changes a default.
func initDefaults() config.Config {
	return config.Config{
		SchemaPath:    "schema.gql",
		QueryDir:      "queries",
		OutputDir:     "internal/db",
		OutputPackage: "db",
		SchemaLang:    config.SchemaLangValues()[0],
		QueryLang:     config.QueryLangValues()[0],
		Driver:        config.DriverValues()[0],
	}
}

// runInitWizard is the whole interactive body behind one seam (§2.1):
// tests drive it directly with accessible=true and a scripted reader.
// The cfg.Save call is the only filesystem mutation in the command,
// unreachable except through the confirm gate (§5.4).
func runInitWizard(in io.Reader, errOut io.Writer, accessible bool, cfgPath string) error {
	flow, cfg, loadErr := classifyTarget(cfgPath)
	// Raw bytes feed only the §5.3 comment notice; absence or
	// unreadability simply means no comment scan (§3.1).
	raw, _ := os.ReadFile(cfgPath) //nolint:errcheck // §3.1: read errors mean no comment scan, nothing more

	if flow == flowBroken {
		fresh, err := runBrokenDialogue(in, errOut, accessible, cfgPath, loadErr)
		if err != nil {
			return err
		}
		if !fresh {
			return errInitAborted
		}
	}

	if err := runForm(newWizardForm(&cfg), in, errOut, accessible); err != nil {
		return err
	}
	// One post-form trim: huh's accessible prompts trim their returns
	// and tea mode does not, so without this the two display modes
	// would diverge (§4.2).
	cfg.SchemaPath = strings.TrimSpace(cfg.SchemaPath)
	cfg.QueryDir = strings.TrimSpace(cfg.QueryDir)
	cfg.OutputDir = strings.TrimSpace(cfg.OutputDir)
	cfg.OutputPackage = strings.TrimSpace(cfg.OutputPackage)
	cfg.ProcsigPath = strings.TrimSpace(cfg.ProcsigPath)

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
	_, err = fmt.Fprint(errOut, warningsText(cfgPath, cfg)+epilogueText(cfgPath, cfg))
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
func newWizardForm(cfg *config.Config) *huh.Form {
	return huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Schema file").
				Description("Path to the graph schema. Relative paths resolve against the config file's directory.").
				Validate(validateNonBlank).
				Value(&cfg.SchemaPath),
			huh.NewInput().
				Title("Query directory").
				Description("Directory holding *.cypher query files.").
				Validate(validateNonBlank).
				Value(&cfg.QueryDir),
			huh.NewInput().
				Title("Output directory").
				Description("Owned exclusively by gqlc: generate replaces its contents.").
				Validate(validateNonBlank).
				Value(&cfg.OutputDir),
			huh.NewInput().
				Title("Package name").
				Description("Go package name for the generated code.").
				Validate(validatePackage).
				Value(&cfg.OutputPackage),
			huh.NewInput().
				Title("Procedure registry (optional)").
				Description("Path to a procsig file; leave empty for none.").
				Value(&cfg.ProcsigPath),
		),
		huh.NewGroup(
			huh.NewSelect[config.SchemaLang]().
				Title("Schema language").
				Options(huh.NewOptions(config.SchemaLangValues()...)...).
				Value(&cfg.SchemaLang),
			huh.NewSelect[config.QueryLang]().
				Title("Query language").
				Options(huh.NewOptions(config.QueryLangValues()...)...).
				Value(&cfg.QueryLang),
			huh.NewSelect[config.Driver]().
				Title("Driver").
				Options(huh.NewOptions(config.DriverValues()...)...).
				Value(&cfg.Driver),
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
func warningsText(cfgPath string, cfg config.Config) string {
	baseDir := filepath.Dir(cfgPath)
	var b strings.Builder
	schemaPath := resolvePath(baseDir, cfg.SchemaPath)
	if _, err := os.Stat(schemaPath); errors.Is(err, fs.ErrNotExist) {
		b.WriteString("warning: schema file " + schemaPath + " does not exist yet; create it before running gqlc generate\n")
	}
	queryDir := resolvePath(baseDir, cfg.QueryDir)
	if _, err := os.Stat(queryDir); errors.Is(err, fs.ErrNotExist) {
		b.WriteString("warning: query directory " + queryDir + " does not exist yet; create it before running gqlc generate\n")
	}
	return b.String()
}

// epilogueText renders the §5.6 epilogue; schema and queries are the
// config values as written (the user's own words, file-relative), not
// resolved paths.
func epilogueText(cfgPath string, cfg config.Config) string {
	return "wrote " + cfgPath + "\n" +
		"next steps:\n" +
		"  1. put your schema at " + cfg.SchemaPath + "\n" +
		"  2. add *.cypher query files under " + cfg.QueryDir + "\n" +
		"  3. run gqlc generate\n"
}
