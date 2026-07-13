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
- `internal/cli/pipeline/pipeline_test.go` — in-memory tests (§5.1).
- `internal/cli/generate.go` — reduced to the cobra surface (§6.1):
  flags, `SilenceUsage`, calling the pipeline, printing diagnostics,
  invoking the tripwire-guarded write. The write path (`writeOutput`,
  `writeFiles`, `markedFile`, marker constants) **stays in
  `internal/cli`** — ADR 0012 tripwire is CLI-owned.
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
// Result is what a successful or diagnostic-accumulating pipeline run
// yields: the codegen batch (populated iff Diagnostics is empty) and
// the ordered per-failure diagnostic lines from stage 7. Both slices
// preserve pipeline order — the caller writes Diagnostics to stderr in
// order and writes Files to disk in slice order.
type Result struct {
    Files       []codegen.File
    Diagnostics []string
}

// Run executes stages 1–8 of the generate pipeline (CLI-1 §3.1)
// against the config file at cfgPath. It performs no filesystem
// writes — the caller writes Result.Files under the ADR 0012 tripwire.
//
// A non-nil error is a singular-stage failure (CLI-1 §2.3): stages
// 1–6, 8, plus any axis-mapping drift, surface as the returned error
// with a Result whose fields are both nil. Stage 7 accumulation is
// distinct: Run returns nil error, Files nil, and Diagnostics
// populated; the caller prints each line and returns its own summary
// error carrying the count.
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
- **Singular-stage errors returned verbatim.** No wrapping; the spec
  §2.3 catalogue already names each error's shape and the pipeline
  produces them as it does today.

## 4. Internals — what the module is

`pipeline.go` is `generate.go` today with the last stage (§5 write)
excised and the diag-printing loop excised. The nine internal helpers
(`resolvePath`, `discoverQueryFiles`, `frontEndWalk`) move with it.
Nothing is renamed; nothing is generalised. The stage numbering in
comments is preserved — the CLI-1 spec is authoritative and existing
code comments cross-reference its section numbers, which stay valid.

No parser registry, no factory, no adapter interface: the two
axis switches at CLI-1 §3.2 stay as switches (`config.SchemaLangGQLC →
gql.New()`; `config.QueryLangOpenCypher → cypher.New(...)`;
`config.DriverNeo4jGoV5|V6 → codegen.WithDriverVersion(...)`). Each
axis has exactly one member today for schema and query language, two
for driver; a switch is the honest posture (bead NON-GOAL, restated in
this spec's opening).

Package doc (a short comment above `package pipeline`) points readers
back to `docs/specs/cli-stage-1.md` and this spec.

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
| `TestRunConfigMissing`                  | error carries the exact spec §2.3 message; `Result` is the zero value                                      |
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

Six commits, each keeping the working tree buildable, tests green,
`just fmt-check && just lint` clean:

1. **This spec.** `docs(spec): CLI-1 pipeline extraction (gqlc-ls8.4)`.
   No production change; Linus grills before any code.
2. **Create the empty pipeline package.** `package pipeline` with the
   package doc; no exports yet. `refactor(cli/pipeline): scaffold the
   pipeline package`. Test gate + lint green.
3. **Move stages 1–8 verbatim into `pipeline.Run` + helpers.**
   `generate.go`'s `RunE` calls `pipeline.Run` and handles the
   accumulation by printing `Result.Diagnostics` + returning the summary
   error, then hands `Result.Files` to the existing `writeOutput`.
   `refactor(cli): extract generate pipeline into internal/cli/pipeline`.
   All CLI-1 tests green byte-identically.
4. **Add pipeline tests** (§5.1). `test(cli/pipeline): direct in-memory
   tests for the pipeline seam`. Green.
5. **Any test-only simplifications in `generate_test.go`** (optional,
   only if a case's *value* is now trivially subsumed by a pipeline
   test — no coverage shrinkage).
6. **Final gate + review.** Full `just test && just fmt-check && just
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
