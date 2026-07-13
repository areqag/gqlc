# Spec — extract the `generate` pipeline from the cobra command

Design for bead `gqlc-ls8.4` (parent epic `gqlc-ls8`, the 2026-07-12
architecture deepening pass). Names the module that ADR 0010 D1 already
assigned to the CLI in production and unbinds it from cobra so it is
callable — and testable — without a command, its writers, or its exit
codes. CLI-1 (`docs/specs/cli-stage-1.md`) is not re-argued; every
user-facing contract of §2 (surface), §3 (stages), and §5 (tripwire) is
preserved verbatim. Explicit **non-goal, restated**: no parser
registry/factory — each tool axis has one adapter today; the hardcoded
picks at stages 3, 5, and 8 stay honest.

## 1. Deliverables

- `internal/cli/pipeline/pipeline.go` — the extracted module (§3, §4).
  Exports: `Run`, `Result`, `ErrConfigMissing`.
- `internal/cli/pipeline/pipeline_test.go` — in-memory tests (§5.1).
- `internal/cli/generate.go` — reduced to the cobra surface (§6.1):
  flags, `SilenceUsage`, calling the pipeline, mapping
  `pipeline.ErrConfigMissing` to the "run gqlc init" hint, printing
  diagnostics, forming the summary, invoking the tripwire-guarded
  write. The write path (`writeOutput`, `writeFiles`, `markedFile`,
  marker constants) **stays in `internal/cli`** — ADR 0012 tripwire
  is CLI-owned.
- `internal/cli/generate_test.go` — unchanged in behaviour; may
  shed cases superseded by pipeline tests (§5.2 fence).
- No changes to `internal/codegen`, `internal/config`, or any other
  package. No new dependencies.

## 2. Where the seam sits

Two adapters justify the module (bead design):

- **Production adapter** — the cobra `RunE` in `generate.go` calls the
  pipeline for the config-file → `[]codegen.File` transform, then owns
  the tripwire sweep and disk writes.
- **Test adapter** — `pipeline_test.go` builds fixture directories in
  `t.TempDir()` for schema and query files (unavoidable — the pipeline
  opens them by path, per spec §3.1 stage 2), points a `pipeline.Run`
  call at a config path, and asserts on the returned `[]codegen.File`
  and diagnostics slice without spawning cobra, capturing stderr, or
  writing to an output directory.

What stays cobra-side, non-negotiable:

- The `-f` flag and `config.DefaultFilename` fallback (spec §2.1).
- `SilenceUsage = true` at the top of `RunE`; `SilenceErrors` false
  (spec §2.2).
- Writing diagnostic lines to `cmd.ErrOrStderr()` and computing the
  singular/plural summary error text (spec §2.3).
- The whole ADR 0012 tripwire sweep and every filesystem write (spec
  §5) — pipeline returns files in memory only.
- Exit-code mapping via cobra's `Execute` (spec §2.2).

What moves into the pipeline module:

- Stages 1–8 of spec §3.1 (config load → resolve paths → parse schema →
  load procsig → construct front end → discover query files →
  front-end walk → codegen `Generate`).
- The axis-switch drift guards (spec §3.2).
- The error-accumulation loop of spec §3.3 — because it is where diag
  strings are formed, but the pipeline **does not** print them or
  compute the summary text.

## 3. Module surface

Package `pipeline`, import path
`github.com/areqag/gqlc/internal/cli/pipeline`. All symbols below are
exported; nothing else is.

```go
// Package pipeline runs stages 1–8 of the CLI-1 generate pipeline —
// config load through codegen — and returns the file batch in memory.
// The package is deliberately subcommand-agnostic: it names no
// sibling command (the "run gqlc init" hint on a missing config lives
// in the CLI, which owns UX copy). Callers own all filesystem writes
// under the ADR 0012 tripwire.
//
// Result caller invariant, non-negotiable:
//
//   Files is non-nil iff Diagnostics is empty AND err is nil.
//   Callers MUST NOT write Result.Files when len(Result.Diagnostics)
//   > 0; that state means "errors accumulated, batch discarded" and
//   Files is nil in that branch. Ignoring the invariant lets the ADR
//   0012 tripwire wipe a marked output directory to write zero files
//   — the exact footgun the split exists to prevent.
package pipeline

// Result is what a successful or diagnostic-accumulating pipeline run
// yields: the codegen batch and the ordered per-failure diagnostic
// lines from stage 7. Both slices preserve pipeline order — the
// caller writes Diagnostics to stderr in order and writes Files to
// disk in slice order.
//
// Field invariant (package doc, restated): Files is non-nil iff
// Diagnostics is empty and the corresponding Run call returned a nil
// error.
type Result struct {
    Files       []codegen.File
    Diagnostics []string
}

// Run executes stages 1–8 of the generate pipeline (CLI-1 §3.1)
// against the config file at cfgPath. It performs no filesystem
// writes — the caller writes Result.Files under the ADR 0012
// tripwire.
//
// Return contract, exhaustive:
//
//   - err != nil                              → singular-stage failure
//     (CLI-1 §3.1 stages 1–6, 8, plus any axis-mapping drift). Result
//     is the zero value (Files nil, Diagnostics nil). A missing
//     config file surfaces as fs.ErrNotExist wrapped with cfgPath —
//     the CLI's RunE tests with errors.Is and rewrites to the §2.3
//     user-facing hint.
//   - err == nil, len(Diagnostics) > 0        → stage-7 accumulation.
//     Files is nil. Caller prints each diagnostic line and returns
//     its own summary error (CLI-1 §2.3).
//   - err == nil, len(Diagnostics) == 0       → success. Files is
//     non-nil, sorted by Path (codegen.Generate's contract), ready
//     for the caller's tripwire-guarded write.
//
// No other combinations exist; the caller may rely on this.
func Run(cfgPath string) (Result, error)
```

Rationales, one per surface decision:

- **`cfgPath string` in, not a decoded `config.Config`.** The path
  resolution rule (spec §3.1 stage 2) needs `filepath.Dir(cfgPath)` —
  callers cannot supply a decoded config without also supplying that
  base directory, and a two-argument constructor would ossify a hidden
  invariant (`baseDir == filepath.Dir(cfgPath)`). One argument keeps
  the seam honest.
- **`Result` struct, not two return values.** Files-plus-diagnostics is
  one logical output shape, and a struct leaves room to add
  fields — a `Config` echo for reuse by the CLI's writer — without a
  signature churn. Zero value is a valid "empty run" for the
  singular-error branch.
- **`Diagnostics []string`, not a `[]Diagnostic` struct.** The strings
  are already the CLI-1 §2.3 wire format; the pipeline forms them in
  the exact positions the walk visits them. A structured type would
  add a re-format layer and duplicate the pinned shapes — pure churn,
  since the CLI wants nothing structured out of them.
- **Summary error text lives in the CLI, not the pipeline.** Spec §2.3
  pins `generate: <n> error | <n> errors` — plural noun choice, the
  literal command name — and cobra prints it as `Error: <text>`. The
  pipeline returning nil-error + populated Diagnostics lets the CLI
  own the whole diagnostic surface: it prints the lines and forms the
  summary. If Run formed the summary too, the CLI would either re-emit
  it or the two would drift.
- **Singular-stage errors returned verbatim, with one carve-out.** No
  wrapping; the spec §2.3 catalogue already names each error's shape
  and the pipeline produces them as it does today. **Exception**:
  a missing config file. Today `runGenerate` catches
  `errors.Is(err, fs.ErrNotExist)` from `config.Load` and rewrites to
  `no config file at <path> (run gqlc init to create one)` — a
  message that names a sibling subcommand. The pipeline is
  subcommand-agnostic (package doc, above), so it returns the raw
  `fs.ErrNotExist`-wrapped error carrying `cfgPath` (`fmt.Errorf(...,
  cfgPath, err)`; the wrapped error preserves `errors.Is` matching),
  and the CLI's `RunE` does the mapping:

  ```go
  res, err := pipeline.Run(cfgPath)
  if err != nil {
      if errors.Is(err, fs.ErrNotExist) && /* from config.Load */ {
          return fmt.Errorf("no config file at %s (run gqlc init to create one)", cfgPath)
      }
      return err
  }
  ```

  The "from config.Load" branch is discriminated by a **sentinel
  error type**, not by string matching: pipeline exports a package
  var `ErrConfigMissing` used as the wrapping token
  (`fmt.Errorf("%w: %s: %w", ErrConfigMissing, cfgPath, err)`;
  `errors.Is` finds both). The CLI matches `errors.Is(err,
  pipeline.ErrConfigMissing)` — no coupling to the underlying
  fs.ErrNotExist path. Rationale: schema and queries dir reads can
  also produce ErrNotExist (spec §2.3 wraps them differently as
  `schema: <os error>` and `queries: <os error>`); the sentinel
  distinguishes the config-missing case cleanly. This is the smallest
  possible seam that keeps the sibling-subcommand string in the CLI
  where it belongs.

## 4. Internals — what the module is

`pipeline.go` is `generate.go` today with the last stage (§5 write)
excised and the diag-printing loop excised. The internal helpers
(`resolvePath`, `discoverQueryFiles`, `frontEndWalk`) move with it.
Nothing is renamed; nothing is generalised. The one behavioural change
is at stage 1: `pipeline.Run` wraps `fs.ErrNotExist` from
`config.Load` as `pipeline.ErrConfigMissing` (see §3), instead of
rewriting the message inline — so the sibling-subcommand copy stays
in the CLI.

**Cross-references preserved on move.** Existing code comments name
CLI-1 spec sections (`§2.3`, `§3.1`, `§3.3`, `§4`, `§5.1`, `§5.3`).
Every such comment moves with its code and stays valid: no stage
renumbering, no shape change. The reviewer verifies this in the diff.

No parser registry, no factory, no adapter interface: the two
axis switches at CLI-1 §3.2 stay as switches (`config.SchemaLangGQLC →
gql.New()`; `config.QueryLangOpenCypher → cypher.New(...)`;
`config.DriverNeo4jGoV5|V6 → codegen.WithDriverVersion(...)`). Each
axis has exactly one member today for schema and query language, two
for driver; a switch is the honest posture (bead NON-GOAL, restated in
this spec's opening).

The package doc above `package pipeline` (§3) states the Result
invariant and the subcommand-agnostic posture; it also points readers
back to `docs/specs/cli-stage-1.md` for the authoritative user-facing
contracts.

## 5. Test plan

### 5.1 New pipeline tests (`internal/cli/pipeline/pipeline_test.go`)

Every case builds a minimal fixture tree in `t.TempDir()` — the
pipeline opens files by path, so a tempdir is the minimum test
surface. **No cobra, no root command, no `executeRoot`, no stdout or
stderr capture.** The tests are direct calls to `pipeline.Run(cfgPath)`
that assert on the returned `Result` and error.

| test                                    | proves                                                                                                    |
|-----------------------------------------|-----------------------------------------------------------------------------------------------------------|
| `TestRunHappyPathReturnsFiles`          | `Result.Files` non-empty, sorted by `Path`, every file marker-headed via a first-line check; `Diagnostics` empty; error nil |
| `TestRunPackageNameFromConfig`          | `Files["db.go"]` contains the config-declared `package` clause                                            |
| `TestRunDriverAxis` (table v5/v6)       | driver import in `db.go` matches the config axis                                                          |
| `TestRunProcsigWiredThroughFrontEnd`    | CALL query resolves with a procsig config; same project sans key surfaces one `query-diag` in `Diagnostics` (unknown procedure) |
| `TestRunConfigMissing`                  | `errors.Is(err, pipeline.ErrConfigMissing)` true; `errors.Is(err, fs.ErrNotExist)` true (wrap chain preserved); error message names `cfgPath`; `Result` is the zero value; the CLI-1 "run gqlc init" copy is NOT in the pipeline error — that string lives only in `generate.go`, verified by `generate_test.go`'s existing `TestGenerateConfigMissing` |
| `TestRunNoQueryFiles`                   | error is the pinned `no query files` string; `Result` is the zero value                                   |
| `TestRunAccumulatesDiagnostics`         | broken-file + broken-queries + one-good fixture: `Diagnostics` holds every failure in pipeline order (file-then-annotation); `Files` nil; error nil (the CLI turns the accumulation into the summary error) |
| `TestRunDiagnosticShapes`               | exact-match one `file-diag` and one `query-diag` against spec §2.3                                        |
| `TestRunPathResolution`                 | config in a subdirectory with relative keys, `cfgPath` an unrelated absolute path: reads succeed from the config's dir |
| `TestRunNoWrites`                       | the `output:` directory named in the config does not exist before or after Run — the pipeline writes nothing |
| `TestRunDiscoveryFilter`                | queries dir seeded with `README.md`, a subdirectory `.cypher`, and `.hidden.cypher`: only top-level non-dot `*.cypher` files are consumed |

The `TestRunNoWrites` case is the seam's central promise made
mechanically checkable: a `require.NoDirExists` on the config's
`output:` value bracketing every other happy-path call would work too,
but a dedicated test names the invariant.

### 5.2 Existing CLI tests (`internal/cli/generate_test.go`) — the fence

Every CLI-1 test passes byte-identically after the extraction. Rationale:
they drive `newRootCmd()` in-process, and the cobra `RunE` still
executes stages 1–9 in the same order with the same error text — spec
§2.3, §5.1, §5.3 preserved verbatim. The tests may be *simplified* only
where they duplicated a case now covered in-memory by
`pipeline_test.go` — but tightening or reducing coverage is not
required; the acceptance criterion is that CLI coverage does not
shrink, and the simpler course is to leave `generate_test.go` alone.

Concretely: `TestGenerateTripwire*`, `TestGenerateWipesStale`, and
`TestGenerateFailedRunTouchesNothing` remain the only home for the ADR
0012 protocol; they stay in `generate_test.go` because the tripwire is
CLI-owned. `TestGenerateConfigMissing`, `TestGenerateAccumulation`,
`TestGenerateDiagnosticShapes`, `TestGenerateNoQueryFiles`,
`TestGeneratePathResolution` all still assert the *stderr* and *exit
code* surface — behaviour that only exists once the CLI wraps the
pipeline — so they stay too.

### 5.3 Quality gates

`just test && just fmt-check && just lint` — green after every edit,
per team-lead protocol.

## 6. Migration path

Small commits, each keeping the working tree buildable, tests green,
`just fmt-check && just lint` clean. The empty-package scaffold commit
is folded into the move so the intermediate state is never a zero-value
directory.

1. **This spec.** `docs(spec): CLI-1 pipeline extraction (gqlc-ls8.4)`.
   No production change; Linus grills before any code.
2. **Move stages 1–8 into `pipeline.Run` + helpers, and rewire
   generate.go.** `generate.go`'s `RunE` calls `pipeline.Run`, maps
   `pipeline.ErrConfigMissing` to the "run gqlc init" hint, handles
   the accumulation by printing `Result.Diagnostics` + returning the
   summary error, then hands `Result.Files` to the existing
   `writeOutput`. Preserves every stage comment's CLI-1 §-reference
   at the move site — **the reviewer verifies this in the diff, and
   the mover confirms it before requesting review.**
   `refactor(cli): extract generate pipeline into internal/cli/pipeline`.
   All CLI-1 tests green byte-identically.
3. **Add pipeline tests** (§5.1). `test(cli/pipeline): direct in-memory
   tests for the pipeline seam`. Green.
4. **Any test-only simplifications in `generate_test.go`** (optional,
   only if a case's *value* is now trivially subsumed by a pipeline
   test — no coverage shrinkage).
5. **Final gate + review.** Full `just test && just fmt-check && just
   lint`.

## 7. Acceptance fences (bead-verbatim, restated)

- CLI behaviour unchanged end-to-end: every CLI-1 test passes; flags,
  exit codes, error text, and tripwire semantics identical.
- Pipeline testable fully in memory (§5.1): no cobra, no stdout/stderr
  capture, no output-directory writes.
- ADR 0012 tripwire stays CLI-owned: `writeOutput`, `writeFiles`,
  `markedFile`, and the marker constants remain in `internal/cli`.
- No parser registry / factory / adapter interface: the axis switches
  stay switches.
- `just test && just fmt-check && just lint` green.

## 8. Non-goals

- Renaming or restructuring stage helpers beyond the move.
- A `pipeline.Config` decoded-input constructor (see §3, "one argument
  keeps the seam honest").
- Structured diagnostics (see §3, "strings are already the wire
  format").
- Any second consumer of the pipeline (there is none; the seam exists
  for test-adapter parity).
- Recursive or multi-extension discovery, watch mode, atomic writes —
  all CLI-1 §8 non-goals still non-goals.
