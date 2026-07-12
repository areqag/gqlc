# CLI stack: cobra for commands, huh for the init wizard

gqlc is growing its real CLI surface — `gqlc init` (an interactive config
wizard), `gqlc generate`, and `gqlc version`, with the entrypoint at
`cmd/gqlc/` — replacing the development scratch harness that sat in the
root `main.go`. This ADR fixes the two library choices that shape that
surface: **spf13/cobra** routes commands, flags, and help;
**charm.land/huh/v2** renders the interactive `init` form. Both were
chosen after a dedicated research spike (2026-07-12) across the
2025–2026 Go ecosystem, and both had genuine alternatives; the rejections
are recorded so they are not re-litigated blind.

## Context

- The whole pipeline behind the CLI is finished: schema parser, query
  parser (Stages 1–15), resolver (R0–R7), codegen (C0–C6), and
  `internal/config` with `Load`/`Save`, closed axis vocabularies, and a
  version-probe seam. `internal/config` was written CLI-agnostic with
  explicit `gqlc init` seams: the `*Values()` functions exist so prompts
  can derive their choices, and `Load` wraps `fs.ErrNotExist` so a
  missing file can branch into creation.
- The repo keeps a lean dependency posture: runtime deps before this ADR
  are antlr4-go and yaml.v3. `spf13/pflag` is already an indirect dep.
- `gqlc init` must be interactive: build a new `gqlc.yaml` or edit an
  existing one, pre-filling current values as defaults. Enum prompts,
  free-text path inputs, and inline validation (non-empty paths, Go
  identifier for the package name) are the required field types.

## Decision

**Commands: spf13/cobra.** The research verdict was that nothing
post-2024 beats it for a small codegen CLI: it remains the ecosystem
standard (sqlc — the tool gqlc is the analogue of — plus gh, kubectl,
goreleaser), actively released, with best-in-class help and shell
completions. Via module pruning the binary links only pflag (already
present) and mousetrap. The strongest signal that no successor exists:
Charm, the ecosystem's momentum player, chose to *wrap* cobra
(charmbracelet/fang) rather than replace it.

**Init form: charm.land/huh/v2.** Per the same spike, the one
actively-maintained, first-class form library in Go; GitHub's own `gh`
CLI depends on huh v2 as of the spike date — the modern
interactive-init exemplar. The API fit is
exact: `huh.NewOptions` builds Select fields from the config package's
`*Values()` slices, per-field `Validate` hooks take the loader's checks,
and `Value(&v)` pointer binding pre-fills the edit-existing-config flow
for free. Accessible mode is built in.

## Considered options

- **charmbracelet/fang** (cobra companion): rejected *for now*. Still
  marked experimental; a v1→v2 module-path change within ~9 months; and
  ~20 transitive deps (lipgloss/v2, colorprofile, x/ansi, mango, roff)
  against the lean posture. Fang is purely additive over cobra, so this
  option stays open at zero cost.
- **urfave/cli v3**: healthy and zero-dep, but weaker completions and
  plainer help; its zero-dep advantage evaporates with pflag already in
  the tree.
- **alecthomas/kong**: elegant declarative API, real adopters, but
  single-maintainer bus factor and completions require third-party
  kongplete.
- **peterbourgon/ff**: v4 stalled in beta; wrong shape (flags-first).
- **stdlib flag + hand dispatch**: viable for exactly three commands,
  but hand-rolls help, completions, and every future flag, and defies
  contributor expectations set by the sqlc ecosystem.
- **AlecAivazis/survey**: archived upstream. **manifoldco/promptui**:
  dormant since 2024. **Raw bubbletea**: overkill for a linear form —
  huh embeds into a bubbletea program later if a bespoke wizard is ever
  needed.

## Consequences

- huh/v2 brings ~29 pure-Go modules (bubbletea/lipgloss/bubbles v2 and
  the charm x/* helpers) into the tree — the one deliberate exception
  to the lean-dependency posture, accepted for the wizard UX.
- huh does **not** self-detect the absence of a terminal; the CLI owns
  the guard. `gqlc init` is TTY-only: without a terminal it fails
  cleanly with a message, rather than growing a flag-per-field
  non-interactive matrix in v1.
- Import path is `charm.land/huh/v2` (the vanity path), not the GitHub
  path.
