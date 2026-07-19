# Stage CLI-2 spec — `gqlc init`: the interactive config wizard

The implementation brief for CLI-2, the third slice of the CLI epic:
the `init` subcommand, an interactive wizard that creates or updates
the config file and writes nothing else. The library is fixed by ADR
0011 (`charm.land/huh/v2` — the charm.land vanity path, not the GitHub
path) and is not re-argued here. The design points CLI-2 owns are the
three-flow state machine (§3), the wizard/preview split that keeps the
preview byte-exact in both display modes (§5.1), the abort contract
(§2.2), and one config widening (§5.2) without which the preview
cannot promise the exact bytes `Save` will write.

Tracking: bead `gqlc-m1c.3` (epic `gqlc-m1c`). CLI-2 also discharges
two debts CLI-1 assigned it: the missing-config message reword
(CLI-1 §2.3) and the README install/usage section (CLI-1 §8), both in
§6.

Upstream facts cited in this spec were verified against huh v2.0.3
(tag `v2.0.3` of charmbracelet/huh, module `charm.land/huh/v2`, the
latest release as of 2026-07-12); file/line references are to that
tag.

---

## 1. Deliverables

- `internal/cli/init.go` — `newInitCmd` plus the unexported wizard
  helpers (§2–§5). No new package (the CLI-1 §6.1 argument applies
  unchanged: one caller, pure policy).
- `internal/cli/init_test.go` — the §7 suite.
- `internal/cli/root.go` — one line: `root.AddCommand(newInitCmd())`.
- `internal/cli/cli_test.go` — `TestRootHelpCommandList` flips its
  `init` assertion from NotContains to Contains.
- `internal/cli/generate.go` + `generate_test.go` — the missing-config
  message reword (§6.1).
- `internal/config` — `Canonical()` widening (§5.2): the single change
  outside `internal/cli`, byte-neutral (`Save` refactors onto it;
  `TestSaveEmitsFixtureBytes` fences the bytes).
- `README.md` — the install + `init` → `generate` workflow section
  (§6.2).
- `go.mod` / `go.sum` — the §1.1 dependency delta, confined to the
  CLI-2 code PR.
- `test/data/codegen/` — **zero changes** (acceptance fence, §9).

### 1.1 Dependency delta

Two new direct dependencies:

- **`charm.land/huh/v2` v2.0.3** (ADR 0011). Its own go.mod requires
  28 modules (10 further direct: bubbles/v2, bubbletea/v2, lipgloss/v2,
  catppuccin/go, five `charmbracelet/x/*` helpers,
  mitchellh/hashstructure/v2; 18 indirect, including `creack/pty`,
  `golang.org/x/sync`, `golang.org/x/sys`) — all pure Go, none
  overlapping today's tree. ADR 0011's "~29 modules" estimate lands at
  28 in v2.0.3. This is the accepted exception to the lean-dependency
  posture; module pruning keeps the linked set smaller than the go.sum
  set.
- **`golang.org/x/term`** (latest at code time) for the TTY guard
  (§2.1). Absent from go.mod today in any form (the only
  `golang.org/x` entry is an indirect `x/exp`); its sole dependency,
  `golang.org/x/sys`, arrives via huh's graph anyway. huh itself
  depends on `github.com/charmbracelet/x/term`, but the guard is
  gqlc's code and uses the canonical x/term per the ADR 0011 design
  session.

`just tidy-check` fences the delta. The spec PR (this file) touches
neither go.mod nor go.sum.

## 2. Command surface

### 2.1 `init` command and the TTY guard

Exact user-facing copy:

```go
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
Args:  cobra.NoArgs,
```

One flag, the same shape as generate's:

```go
cmd.Flags().StringVarP(&cfgPath, "file", "f", config.DefaultFilename,
	"path to the config file to create or update")
```

`RunE` runs, in order:

1. `cmd.SilenceUsage = true` (the CLI-1 §2.2 per-run posture: parse
   errors keep the usage dump, runtime failures do not).
2. **TTY guard**: unless
   `term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stderr.Fd()))`
   (golang.org/x/term), return the pinned error

   ```
   init requires an interactive terminal
   ```

   Stdin and stderr are the two fds the wizard actually uses: it reads
   answers from stdin and renders on stderr (§2.3). Stdout is
   deliberately not checked — it is empty in every init path, so
   redirecting it is harmless. There is no flag matrix and no
   non-interactive mode in v1 (ADR 0011; §8).
3. `accessible := os.Getenv("ACCESSIBLE") != ""` — huh v2 does **not**
   read this variable itself (verified: `NewForm` auto-enables
   accessible mode only for `TERM=dumb`, form.go:129–131); wiring the
   env var is the caller's job, per huh's own README convention. Any
   non-empty value enables, including `"0"` — the upstream convention,
   adopted verbatim rather than half-improved.
4. Delegate to the seam `runInitWizard(in io.Reader, errOut io.Writer,
   accessible bool, cfgPath string) error` with
   `cmd.InOrStdin()` / `cmd.ErrOrStderr()` — the whole interactive
   body behind one function tests drive directly (§7).

### 2.2 Exit codes and the abort contract

| condition                                                    | exit |
|--------------------------------------------------------------|------|
| config file written                                          | 0    |
| user abort: decline at confirm, abort choice in the broken-config dialogue, Ctrl-C (`huh.ErrUserAborted`) | 1 |
| TTY guard failure                                            | 1    |
| any other failure (write error, wizard I/O error)            | 1    |
| parse errors (unknown flag, unexpected args)                 | 1    |

Two codes, per CLI-1 §2.2 — no richer taxonomy. **Abort exits 1**,
pinned: exit 0 must certify "the config file exists now", so that
`gqlc init && gqlc generate` and bootstrap scripts behave; an abort is
a run that did not do the thing the command exists to do. (gh's
prompt-cancel and terraform's interrupt are both non-zero; the
tolerant alternative — abort = 0 — makes success unobservable without
a stat.)

Every abort path returns the one pinned error:

```
init aborted: no file written
```

Ctrl-C in the wizard surfaces as huh's sentinel — verified:
`var ErrUserAborted = errors.New("user aborted")` (form.go:55),
returned from the tea-mode run when the form was cancelled or the
program interrupted (form.go:709) — and `RunE` maps
`errors.Is(err, huh.ErrUserAborted)` to the pinned abort error.
Stated honestly for accessible mode: there huh uses plain cooked-mode
prompts (no bubbletea program), so Ctrl-C is a raw SIGINT that
terminates the process before any gqlc code runs — no `Error:` line,
shell reports signal death. Nothing can have been written, because the
only write happens after the confirm gate (§5.4); no signal handler is
added to prettify this (§8).

### 2.3 stdout/stderr contract and message catalogue

**stdout is byte-empty in every init path** — success, abort, and
failure. The CLI-1 §2.3 posture extends unchanged: init is TTY-gated,
so its output has a human, never a machine, and one uniform "stdout is
empty except `version`" rule across the whole binary is worth more
than a `wrote gqlc.yaml` line on stdout. Everything lands on stderr:
the wizard UI, the broken-config report, the preview, the warnings,
the epilogue, and cobra's single `Error: ` line.

The wizard is explicitly wired `WithOutput(errOut)` and
`WithInput(in)`. In tea mode that matches huh's own default (verified:
`NewForm` seeds `tea.WithOutput(os.Stderr)`, form.go:118); in
accessible mode it **overrides** huh's default of stdout (verified:
`runAccessible` falls back to `os.Stdout`, form.go:678) — without the
override, `ACCESSIBLE=1 gqlc init > log` would swallow the prompts
while the guard still passes (stderr is a TTY). One wiring subtlety,
pinned: `WithAccessible(true)` is called only when `accessible` holds;
an unconditional `WithAccessible(accessible)` with `false` would
clobber huh's own `TERM=dumb` auto-enable (form.go:129–131), which
init keeps.

Message catalogue (`Error: ` prefix implied on error rows):

| condition                    | shape                                                        | stream |
|------------------------------|--------------------------------------------------------------|--------|
| TTY guard failure            | `init requires an interactive terminal`                      | stderr (error) |
| broken-config report         | the `config.Load` error **verbatim**, one diagnostic line    | stderr |
| any abort                    | `init aborted: no file written`                              | stderr (error) |
| save failure                 | verbatim (`config: write <path>: <os error>`)                | stderr (error) |
| soft warnings                | §5.5, diagnostic lines after a successful write              | stderr |
| epilogue                     | §5.6, after a successful write                               | stderr |

## 3. Flows

### 3.1 Flow selection

One classification, decided before any form renders, from a single
attempt on the target path:

```
cfg, err := config.Load(cfgPath)
err == nil                      → EDIT   (wizard prefilled from cfg)
errors.Is(err, fs.ErrNotExist)  → FRESH  (wizard from defaults)
any other error                 → BROKEN (report, then dialogue)
```

`errors.Is(err, fs.ErrNotExist)` is the exact seam `Load` documents
for this purpose (config.go, `Load` doc comment: "a future `gqlc init`
branches on that to offer creation"). Everything `Load` rejects that
is not file-absence — malformed YAML, unknown keys, out-of-vocabulary
values, unsupported versions, a directory at the path, a permission
failure — is BROKEN: init never second-guesses the loader's verdict.
Alongside classification, init reads the file's raw bytes once
(`os.ReadFile`, errors ignored — absence or unreadability simply means
no comment scan) for the §5.3 comment notice.

The classification is a one-shot: the file is not re-read or re-locked
between classification and write. A file swapped underneath a running
wizard is last-writer-wins (§8).

### 3.2 Fresh flow — defaults

The wizard starts from pinned defaults:

| field             | default        | why                                                        |
|-------------------|----------------|-------------------------------------------------------------|
| `schema`          | `schema.gql`   | the canonical fixture's value (config-file-format §3)       |
| `queries`         | `queries`      | fixture value, sans incidental trailing slash               |
| `output`          | `internal/db`  | fixture value                                               |
| `package`         | `db`           | fixture value                                               |
| `schema_language` | `gql`          | first member of `config.SchemaLangValues()`                 |
| `query_language`  | `opencypher`   | first member of `config.QueryLangValues()`                  |
| `driver`          | `neo4j-go-v5`  | first member of `config.DriverValues()`                     |
| `procsig`         | *(empty)*      | optional key; empty means omitted (§4, field 5)             |

Path/package defaults mirror `internal/config/testdata/canonical.gqlc.yaml`
so the docs, the loader fixture, and the wizard tell one story — with
two disclosed deviations: `queries` drops the fixture's incidental
trailing slash, and `procsig` defaults to empty (key omitted) where
the fixture carries `procs.procsig.json`, because the fixture exists
to pin the optional key's encoding while a fresh project has no
registry. Enum
defaults are pinned as a **rule**, not per-axis values: the first
member of each `*Values()` slice — appending a future vocabulary
member never silently changes a default.

### 3.3 Edit flow — prefill and the Select-binding gotcha

An EDIT wizard binds the loaded `Config`'s fields directly via
`Value(&v)`; every prompt opens showing the file's current value, the
procsig input included (empty when the key was omitted).

The house research spike flagged a huh gotcha, now verified against
v2.0.3 source and **worse than reported**: a `Select` whose bound
value matches no option does not merely land its cursor on index 0 —
`selectValue`/`selectOption` leave `selected` at its zero value on a
missed match, and `updateValue()` then writes
`filteredOptions[0].Value` **back through the accessor** the moment
the field is constructed (field_select.go:98–104, 191–204, 515–519).
An out-of-vocabulary stored value would be silently rewritten to the
first vocabulary member.

Resolution, stated honestly: **no defensive re-check is written,
because the state is unrepresentable.** The prefill is a `Config` that
came out of `config.Load`, whose `UnmarshalYAML` hooks validate every
axis against the very `*Values()` slices the wizard's options are
built from (config.go `enumFromNode`; §4 sources options from the same
slices). Same binary, same slices, single source of truth — a loaded
value that matches no option cannot exist, and a runtime re-check
would be dead code. The invariant is fenced by construction (options
*are* `huh.NewOptions(config.DriverValues()...)` etc., so a new
vocabulary member is an option before it can be a stored value) and by
test (§7 `TestInitEditVocabularyPrefill` drives an edit round-trip
over **every** vocabulary member — the test that fails loudly if this
reasoning ever rots).

### 3.4 Broken flow — report, then exactly two choices

1. Print the `config.Load` error **verbatim** as one stderr diagnostic
   line — it already carries the path, the line number where
   applicable, and the valid vocabularies (config-file-format §6.3);
   init adds nothing and rewrites nothing.
2. Run a one-field choice form (same accessible/output wiring as the
   wizard):

   ```go
   huh.NewSelect[bool]().
   	Title("The config file cannot be loaded. Start fresh?").
   	Options(
   		huh.NewOption("abort — leave "+cfgPath+" untouched", false),
   		huh.NewOption("start fresh — rebuild "+cfgPath+" from defaults", true),
   	).
   	Value(&fresh)
   ```

   `fresh` starts false, so **abort is the default-highlighted
   choice** (and the accessible default answer): the destructive
   option is never one accidental Enter away.
3. Abort → the pinned abort error, exit 1, file untouched. Fresh →
   the FRESH wizard (§3.2 defaults — no salvage decoding, no partial
   prefill from a file the loader rejected); the overwrite happens
   only at the §5.4 write, after preview and confirm, with the §5.3
   comment notice applying if the old bytes contained `#`.

There is no salvage decoder (§8): a file the loader rejects has no
trustworthy values to salvage, and "print the real error, offer fresh
or abort" is the whole contract.

## 4. Wizard form

### 4.1 Structure and field order

One `huh.Form`, two groups (pages); the preview/confirm step is
deliberately **not** a third group (§5.1 explains why):

- **Group 1 — files and package** (five `Input`s): `schema`,
  `queries`, `output`, `package`, `procsig`.
- **Group 2 — tool axes** (three `Select`s): `schema_language`,
  `query_language`, `driver`.

The order is the canonical wire order (config-file-format §3) with one
deviation: `procsig`, wire-last, is asked beside the other paths —
grouping by kind beats wire order mid-wizard, and the preview (§5.3)
shows canonical order regardless.

### 4.2 Per-field contract

Pinned copy, defaults (fresh flow), and validation:

| # | field | huh field | Title | Description | validate |
|---|-------|-----------|-------|-------------|----------|
| 1 | `schema`  | `Input` | `Schema file` | `Path to the graph schema. Relative paths resolve against the config file's directory.` | non-blank |
| 2 | `queries` | `Input` | `Query directory` | `Directory holding *.cypher query files.` | non-blank |
| 3 | `output`  | `Input` | `Output directory` | `Owned exclusively by gqlc: generate replaces its contents.` | non-blank |
| 4 | `package` | `Input` | `Package name` | `Go package name for the generated code.` | §4.3 |
| 5 | `procsig` | `Input` | `Procedure registry (optional)` | `Path to a procsig file; leave empty for none.` | none |
| 6 | `schema_language` | `Select[config.SchemaLang]` | `Schema language` | — | — |
| 7 | `query_language`  | `Select[config.QueryLang]`  | `Query language`  | — | — |
| 8 | `driver`          | `Select[config.Driver]`     | `Driver`          | — | — |

- **Selects source their options from the vocabularies**, mechanically:
  `Options(huh.NewOptions(config.SchemaLangValues()...)...)` and
  likewise for the other two axes. `NewOptions` renders each option's
  key via `fmt.Sprint` (option.go:13–22), which for the `~string`
  vocabulary types is the wire string (`gqlc`, `opencypher`,
  `neo4j-go-v5`, `neo4j-go-v6`) — display and wire value cannot drift.
  The `Value(&…)` binding is typed (`Select[config.Driver]` etc.); no
  string round-trip.
- **Single-member vocabularies still get a Select** (schema and query
  language today): one mechanism for all three axes, zero
  special-casing to unwind when a vocabulary grows, and the fixed
  choice is shown rather than hidden — huh's accessible mode even has
  a dedicated single-option prompt ("There is only one option
  available; enter the number 1:", field_select.go:747ff), so the
  degenerate case is first-class upstream.
- **non-blank** is `strings.TrimSpace(s) == ""` → error
  `must not be empty`. After the form completes, init trims
  leading/trailing whitespace from all five string answers once —
  pinned because the two display modes otherwise diverge (huh's
  accessible `PromptString` trims its return, tea mode does not;
  accessibility.go:164).
- The procsig input takes anything; a blank answer (after the trim)
  means "omit the key", matching `Config.ProcsigPath`'s empty-string
  contract and `Save`'s omitempty. One honest limitation, inherited
  from huh: in accessible **edit** mode a prefilled procsig cannot be
  cleared, because an empty answer means "keep the default"
  (`cmp.Or(input, defaultValue)`, accessibility.go:164); TTY-mode
  editing clears it fine. Documented, not worked around (§8).

### 4.3 Package-name validation

The wizard enforces, in order, with pinned messages:

1. non-blank → `must not be empty`
2. `token.IsIdentifier` — the loader's own posture (config.go:329–331,
   where `decodeV1` rejects non-identifiers, keywords included) →
   `package %q is not a valid Go identifier`
3. codegen's emission grammar `^[a-z][a-z0-9_]*$` (`packageIdent`,
   internal/codegen/prepare.go:19; enforced against configured
   packages by `WithPackageName`, CLI-1 §3.4) →
   `package %q will fail gqlc generate (must match ^[a-z][a-z0-9_]*$)`

Pinned decision: the wizard **enforces** the stricter grammar rather
than warning. A huh `Validate` hook is a hard gate — there is no soft
"warn and continue" seam — and a wizard that writes a config
`generate` is guaranteed to reject is a wizard defect; failing at the
prompt, with the grammar in the message, is strictly kinder than
failing at the next `generate`. Both checks 2 and 3 are needed —
neither subsumes the other: `Db` passes `IsIdentifier` and fails the
grammar; `func` matches the grammar and fails `IsIdentifier` (keyword).
Consequence for the edit flow, stated plainly: a hand-written config
with `package: Db` loads (loader grammar) and prefills, but the wizard
refuses to proceed until it is fixed — which is the point.

## 5. Preview-confirm and write

### 5.1 Why the preview is not a form field

The locked design says the final step shows the **exact canonical
bytes** `Save` will write. huh's `Note` field cannot carry that
promise — verified twice over in v2.0.3: `Note.View` pipes its
description through a homegrown markdown renderer in which `_…_`
toggles italics (field_note.go, `render`), so `schema_language:` /
`query_language:` lines would be visibly mangled; and
`Note.RunAccessible` prints only the *static* `description.val`, so a
`DescriptionFunc`-computed preview (required, since the values bind in
earlier groups) renders as nothing in accessible mode. `Confirm`'s
accessible renderer likewise drops descriptions (field_confirm.go,
`RunAccessible` prints title + `[y/N]` only).

So init runs **two forms with a plain print between them**:

1. the §4 wizard form;
2. the preview block, written raw to the wizard's output writer with
   `fmt.Fprint` — byte-exact, unstyled, identical in both modes;
3. a one-field confirm form.

### 5.2 Config widening: `Canonical()`

The preview needs `Save`'s bytes without writing. `internal/config`
gains:

```go
// Canonical returns the exact bytes Save writes: the §7 canonical
// form of the config-file-format spec.
func (c Config) Canonical() ([]byte, error)
```

`Save` becomes `Canonical` + `os.WriteFile(path, b, 0o644)` — a pure
refactor; `TestSaveEmitsFixtureBytes` (byte-equality against
`testdata/canonical.gqlc.yaml`) fences that nothing drifts, and the
preview/write identity is by construction, not by parallel encoders.

### 5.3 Preview block

Pinned shape (stderr, like everything):

```
gqlc init will write <path>:

<canonical bytes, verbatim>
```

`<path>` is the `-f` value as given. The canonical bytes end in the
encoder's own trailing newline; nothing is indented, styled, or
wrapped.

When the target file already exists and its raw bytes (§3.1) contain
`#`, one pinned notice line follows the block:

```
note: comments in <path> will not survive; gqlc init writes the canonical form
```

Detection is the simple byte scan — `bytes.ContainsRune(raw, '#')` —
chosen honestly over string-context parsing: a YAML comment necessarily
contains `#`, so a comment is never missed; the false positive (a `#`
inside a quoted scalar value) costs one harmless notice line. Applies
in both the edit flow and the broken flow's fresh-start overwrite.

### 5.4 Confirm and write

```go
huh.NewConfirm().
	Title("Write " + cfgPath + "?").
	Affirmative("Write").
	Negative("Abort").
	Value(&write)
```

`write` starts false: the default is **Abort** (`[y/N]` in accessible
mode). Overwrite safety beats one keystroke of convenience, and the
false default buys a structural property for free: huh's accessible
prompts return the default on input EOF and its form runner discards
field errors entirely (`_ = field.RunAccessible(w, r)`,
form.go:720–738; "no way to bubble up errors", accessibility.go:148),
so an input-starved accessible run cascades defaults to the confirm,
answers Abort, and **can never write** (§7 pins this as a test).

- Abort → pinned abort error (§2.2), exit 1, nothing written.
- Write → `cfg.Save(cfgPath)`; a failure is the verbatim
  `config: write <path>: <os error>`, exit 1. init does not create
  parent directories for an `-f` path in a nonexistent directory —
  config-file-only writes; the write error says exactly what happened.

The write is the **only** filesystem mutation in the entire command,
and it is unreachable except through the confirm gate.

### 5.5 Soft warnings

After a successful write, init stats the two project inputs, resolved
the way generate resolves them (relative to
`filepath.Dir(cfgPath)`, CLI-1 §3.1 stage 2), and prints one pinned
line per missing one:

```
warning: schema file <resolved> does not exist yet; create it before running gqlc generate
warning: query directory <resolved> does not exist yet; create it before running gqlc generate
```

Warnings, never creations, never failures — the exit code stays 0.
Not checked, deliberately: the output directory (generate creates and
owns it, ADR 0012 — a warning would nudge users toward pre-creating
it, the wrong direction) and the procsig path (optional, expert
surface; generate's own procsig error is verbatim and precise).

### 5.6 Epilogue

After the write and any warnings, pinned (stderr):

```
wrote <path>
next steps:
  1. put your schema at <schema>
  2. add *.cypher query files under <queries>
  3. run gqlc generate
```

`<schema>` and `<queries>` are the config values as written (the
user's own words, file-relative), not resolved paths.

### 5.7 Version migration is free

The wizard is version-agnostic by construction: `Load` normalises
every accepted on-disk version into the one `Config` (the
probe-then-dispatch seam, config-file-format §5), the wizard edits
that `Config`, and `Save`/`Canonical` always emit the latest format —
so any loadable old-version file round-trips to the current canonical
form by walking through the wizard, no migration code existing
anywhere. Stated honestly: only version 1 exists today, so the
testable instance of this claim is a **non-canonical v1** file
(comments, reordered keys, quoting) canonicalised by an edit run
(§7); the cross-version leg activates the day a v2 decoder lands,
through the same seam, with zero init changes.

## 6. Inherited debts

### 6.1 generate's missing-config reword

CLI-1 §2.3 pinned
`no config file at <path> (gqlc init, which creates one, arrives in a later release)`
and assigned CLI-2 the reword. The new pinned message:

```
no config file at <path> (run gqlc init to create one)
```

Changes: the message literal in `runGenerate` (generate.go:72) and the
`want` string in `TestGenerateConfigMissing` (generate_test.go).
Errata pointer, CLI-0 §4.4 style — the historical spec stays as
written:

| CLI-1 statement (cli-stage-1.md)                      | superseded by |
|--------------------------------------------------------|---------------|
| §2.3 missing-config row: `…(gqlc init, which creates one, arrives in a later release)` | `…(run gqlc init to create one)` (this spec) |

### 6.2 README install/usage section

CLI-0 §8 deferred the README to CLI-1; CLI-1 §8 moved it here "once,
when it is real". CLI-2 delivers it. Pinned scope (prose drafted in
the code PR, not here): installation via
`go install github.com/areqag/gqlc/cmd/gqlc@latest`; the
`init` → `generate` workflow (run `gqlc init`, answer the prompts,
write schema and queries, run `gqlc generate`); a minimal example — a
few-line schema, one annotated query, the config `init` writes, and
the generated files' names; a pointer at the output-directory
ownership rule (ADR 0012). No man-page ambitions, no per-flag
reference (cobra's `--help` is that).

## 7. Test plan

The wizard's testability rests on three verified facts about huh
v2.0.3: accessible mode is a plain `io.Reader`/`io.Writer` prompt loop
with **no TTY requirement** (`runAccessible`, form.go:677–679 — the
seam `WithInput`/`WithOutput` feed); tea mode *does* need a terminal
(and huh's own tests bypass it by pumping `tea.KeyPressMsg` values
into `Form.Update` — coupling to huh's keymap internals this suite
declines); and every prompt helper re-reads from the shared reader
per call with a fresh `bufio.Scanner` (`PromptString`,
accessibility.go:131, scanner at :138), so a buffered script reader
would be swallowed by the first prompt's read-ahead — scripted tests
wrap their input in `iotest.OneByteReader`, which keeps every unread
script byte available to the next prompt's scanner.

Strategy, in layers:

1. **Pure helpers, exhaustively** — flow classification, defaults,
   validators, comment detection, preview/warning/epilogue text: no
   forms, no TTY, table tests.
2. **Accessible-driven end-to-end** — `runInitWizard` called directly
   with `accessible=true`, a `OneByteReader`-wrapped script, an output
   buffer, and a `t.TempDir()` path: real huh forms, real validators,
   real `config.Save`, everything except cobra and the guard.

   The script contract, pinned per prompt type. Empty lines do **not**
   universally take defaults: `PromptString` runs the field validator
   on the raw scanned line (accessibility.go:156) *before* the
   `cmp.Or(strings.TrimSpace(input), defaultValue)` substitution
   (:164), and accessible Inputs print only their Title, never the
   bound default (field_input.go:442–445). So:

   - **Validated Inputs (fields 1–4)**: an empty line fails the
     non-blank validator and re-prompts — consuming the next script
     line and misaligning everything after it. Scripts send an
     **explicit value line** for each of the four. Edit-flow scripts
     re-type the stored values, and the pinned choice is to *derive*
     those lines from the same `Config` the test asserts against,
     never hand-copy them, so a fixture edit cannot desynchronise
     script and assertion.
   - **procsig Input (no validator)**: an empty line is accepted and
     the substitution yields the bound default — empty in the fresh
     flow, the stored value in the edit flow (§4.2's unclearable
     caveat).
   - **Selects**: an empty line takes the numbered default —
     `PromptInt` accepts empty when a default exists
     (accessibility.go:38–41), and that default derives from the
     pointer binding (field_select.go:757–763) — exactly the seam the
     §3.3 prefill fence must exercise, so Select answers stay empty
     lines by design.
   - **Confirm**: `y` writes; an empty line or `n` yields the bound
     `false` default → abort.
3. **Guard and surface via `executeRoot`** — under `go test` the
   process's stdin is not a terminal, so the in-process guard test is
   deterministic in CI.

| test                                | proves                                                                                       |
|-------------------------------------|-----------------------------------------------------------------------------------------------|
| `TestInitClassifyTarget`            | table: absent → fresh; loadable → edit (values); malformed / bad-vocab / directory-at-path → broken with the loader's error |
| `TestInitDefaults`                  | §3.2 table exactly; enum defaults are `Values()[0]` by rule                                    |
| `TestInitPackageValidator`          | table: `""`, `db`, `db_1`, `Db`, `func`, `1db`, a Unicode-letter identifier — each mapped to its §4.3 clause and message |
| `TestInitCommentDetection`          | `#` comment → notice; `#` inside a quoted value → notice (honest false positive); no `#` → none |
| `TestInitPreviewBlock`              | §5.3 shape byte-exact, canonical bytes verbatim, notice line iff flagged                        |
| `TestInitWarningsAndEpilogue`       | §5.5 lines exactly, resolved against the config's directory; §5.6 text exactly                 |
| `TestInitNonTTY`                    | `executeRoot(t, "init")` → pinned guard error, empty stdout                                     |
| `TestRootHelpCommandList`           | `init` flips to Contains (cli_test.go)                                                          |
| `TestInitFreshWritesCanonical`      | fresh flow: script types the four §3.2 path/package defaults, empty-lines procsig and the Selects, `y` at confirm → file bytes == `Canonical()` of the §3.2 defaults; `config.Load` round-trips; exit-0 path |
| `TestInitFreshDecline`              | confirm Abort → pinned abort error, no file on disk                                             |
| `TestInitEditPrefillRoundTrip`      | canonical file (v6 driver + procsig) → script re-types the stored Input values (derived from the fixture `Config`), empty-line-accepts procsig and the Selects → byte-identical rewrite |
| `TestInitEditVocabularyPrefill`     | table over **every** `SchemaLang`/`QueryLang`/`Driver` member: prefilled value survives an edit whose script empty-line-accepts the Select defaults — the §3.3 fence against the Select index-0 clobber |
| `TestInitCanonicalisesNonCanonical` | v1 file with comments, reordered keys, quoted scalars → edit per the §7 script contract → canonical bytes; comment notice present (§5.7 migration claim) |
| `TestInitBrokenAbort`               | malformed file → loader error verbatim in output, default choice (abort) → pinned error, file byte-untouched |
| `TestInitBrokenFresh`               | choose start-fresh → defaults wizard (script as in `TestInitFreshWritesCanonical`) → confirm → canonical defaults overwrite |
| `TestInitInputStarvation`           | empty input reader, accessible → EOF bypasses validation (`PromptString` breaks before validating on a failed scan), defaults cascade, confirm defaults to Abort, nothing written (§5.4 property) |
| `TestCanonicalMatchesSave` (config) | `Canonical()` bytes == the file `Save` writes; fixture equality intact                          |
| `TestGenerateConfigMissing`         | updated pinned message (§6.1)                                                                   |

Not attempted, honestly: a tea-mode keystroke-driven end-to-end — it
needs a real PTY (bubbletea raw mode) or huh-internal `Update`
pumping; the accessible path exercises the same bindings, validators,
preview, and write seam, and the tea renderer is huh's tested code,
not ours (§8).

## 8. Non-goals

- Non-interactive mode: no flag-per-field matrix, no `--yes`, no
  stdin-JSON. TTY-only is the ADR 0011 v1 posture; a scripted project
  writes `gqlc.yaml` with an editor.
- A salvage decoder for broken configs (§3.4) — verbatim error, fresh
  or abort, nothing in between.
- Comment and formatting preservation on rewrite — `Save` has exactly
  one canonical form; the §5.3 notice is the mitigation.
- Creating the schema file, query directory, output directory, or the
  `-f` path's parent directories — init writes one file (locked
  design; ADR 0012 keeps init's hands off output directories
  entirely).
- A procsig-existence warning (§5.5).
- Signal choreography: no SIGINT handler; tea-mode Ctrl-C is
  `huh.ErrUserAborted`, accessible-mode Ctrl-C is process death before
  any write can have happened (§2.2).
- Timeouts (`WithTimeout` unused; `ErrTimeout`/`ErrTimeoutUnsupported`
  unreachable), themes, custom keymaps.
- Concurrent-edit/TOCTOU guarding between classification and write
  (§3.1) — last writer wins on a file the user is interactively
  editing.
- A tea-mode PTY end-to-end test (§7).
- Working around huh's accessible-mode quirks (unclearable prefilled
  optional field §4.2; discarded field errors §5.4) — upstream
  behaviour, documented and fenced by the starvation test instead.
- Exit codes beyond 0/1 (§2.2).
- charmbracelet/fang (ADR 0011: open option, still not now).

## 9. Acceptance criteria

1. In an empty directory on a terminal, `gqlc init` walks the eight
   §4 prompts from the §3.2 defaults, previews the exact canonical
   bytes, and on Write exits 0 having created `gqlc.yaml` — which
   `config.Load` loads and `gqlc generate` consumes without edits.
   stdout is empty; declining at the confirm exits 1 with
   `init aborted: no file written` and creates nothing.
2. With an existing loadable config (v6 driver, procsig present),
   `gqlc init` opens every prompt on the stored value and an
   accept-everything run rewrites the file byte-identically.
3. With a malformed config, init prints the loader's error verbatim,
   defaults to abort (file byte-untouched, exit 1), and on start-fresh
   overwrites with the confirmed defaults.
4. `gqlc init < /dev/null` (and any non-TTY stdin/stderr) exits 1
   with exactly `Error: init requires an interactive terminal` and
   empty stdout.
5. `ACCESSIBLE=1 gqlc init` completes the same flows through
   numbered prompts and writes identical bytes.
6. `gqlc generate` with no config prints the §6.1 reworded message;
   `gqlc --help` lists `init` beside `generate` and `version`.
7. The README documents install and the `init` → `generate` workflow.
8. go.mod gains exactly `charm.land/huh/v2` and `golang.org/x/term`
   as direct dependencies, in the code PR only; `just lint`,
   `just test`, `just tidy-check` green; `test/data/codegen/`
   byte-identical to master.
