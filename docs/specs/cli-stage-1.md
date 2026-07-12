# Stage CLI-1 spec — `gqlc generate`: config-driven pipeline orchestration

The implementation brief for CLI-1, the second slice of the CLI epic:
the `generate` subcommand, which runs the whole finished pipeline —
config load, schema parse, query-file front end, resolution, codegen —
and writes the generated package under the ADR 0012 output-directory
protocol. The command stack is fixed by ADR 0011 and the CLI-0 spec
(cobra, fresh-per-invocation constructors, output through the
command's writers); neither is re-argued here. The design points CLI-1
owns are the error-surface contract (§2.3), the query-file discovery
rule (§4), the tripwire algorithm as executable steps (§5), and one
codegen widening (§3.4) without which the config file's `package` key
would be silently ignored.

Tracking: bead `gqlc-m1c.2` (epic `gqlc-m1c`). `init` (CLI-2,
`gqlc-m1c.3`) stays out of scope. The old procsig-threading bead
`gqlc-aqb` is superseded: the registry arrives via the config file's
`procsig` key (§3.1 stage 4), not a flag.

---

## 1. Deliverables

- `internal/cli/generate.go` — `newGenerateCmd` plus the unexported
  pipeline helpers (§2, §3, §5). No new package (§6.1).
- `internal/cli/generate_test.go` — the §7 suite.
- `internal/cli/root.go` — one line: `root.AddCommand(newGenerateCmd())`.
- `internal/cli/cli_test.go` — `TestRootHelpCommandList` flips its
  `generate` assertion from NotContains to Contains (`init` stays
  NotContains).
- `internal/codegen` — `WithPackageName` option (§3.4): the single
  change outside `internal/cli`, zero-value-compatible, so the golden
  corpus is untouched.
- `internal/logger/` — **deleted** (§6.2); the two forbidigo messages
  in `.golangci.yml` that name it are reworded (the ban itself stays).
- `go.mod` — unchanged; no new dependencies.
- `test/data/codegen/` — **zero changes** (acceptance fence, §9).

## 2. Command surface

### 2.1 `generate` command

Exact user-facing copy:

```go
Use:   "generate",
Short: "Generate Go code from the schema and queries the config file declares",
Long: `generate runs the pipeline the config file declares: parse the schema,
parse and resolve every named query in the queries directory, and write
the generated Go package to the output directory.

The output directory belongs to gqlc alone: each run replaces its
contents, and a run aborts — deleting nothing — if the directory holds
anything gqlc cannot prove it generated.

Relative paths in the config file are relative to the config file's
directory, not the working directory.`,
Args:  cobra.NoArgs,
```

One flag:

```go
cmd.Flags().StringVarP(&cfgPath, "file", "f", config.DefaultFilename,
	"path to the config file")
```

- The default is the literal `gqlc.yaml` (`config.DefaultFilename`),
  which the OS reads relative to the working directory — the sqlc
  convention. An explicit `-f` value is passed to `config.Load`
  verbatim; there is no search logic, matching the config-file-format
  spec §2 posture (one name, one answer).
- The paths *inside* the config file resolve against the config
  file's directory (§3.1 stage 2) — `-f ../proj/gqlc.yaml` run from
  anywhere behaves identically to running in `../proj`. The
  config-file-format spec §4 declares this rule and assigns its
  implementation to the CLI; this spec owns it.
- `Args: cobra.NoArgs` — inputs come from the config file only.

### 2.2 Exit codes and the silence posture

| condition                                              | exit |
|--------------------------------------------------------|------|
| full pipeline success                                  | 0    |
| any failure (config, pipeline, tripwire, write)        | 1    |
| parse errors (unknown flag, unexpected args)           | 1    |

Two codes, unchanged from CLI-0 §2.3. The richer-taxonomy question
CLI-0 deferred is answered: no. The diagnostic text carries the
failure class; scripts branch on pass/fail, and no consumer of a
numeric class exists.

CLI-0 §2.1 deferred the `SilenceUsage`/`SilenceErrors` decision to
CLI-1. Answer:

- **`SilenceErrors` stays false everywhere.** Cobra prints exactly one
  `Error: <text>` line per failed invocation; the command never
  prints its own copy of the returned error.
- **`SilenceUsage` is set per-run, not per-command**: the first
  statement of generate's `RunE` is `cmd.SilenceUsage = true`. Flag
  and `Args` validation run before `RunE`, so a parse error
  (`gqlc generate --bogus`, `gqlc generate extra`) keeps CLI-0's
  error-plus-usage dump — exactly right for a usage mistake — while a
  runtime failure (config, pipeline, filesystem) prints diagnostics
  with no usage noise. Setting the field statically on the command
  would silence the usage dump for parse errors too.

### 2.3 stdout/stderr contract and error text shapes

**stdout is always empty** — on success and on failure. Success is
silent (exit 0 is the signal, the sqlc convention); a summary line
would be a format contract with no consumer. All diagnostics go to
stderr via `fmt.Fprintln(cmd.ErrOrStderr(), …)` (forbidigo bans bare
`fmt.Print*`; the command writers are the test seam).

stderr on failure is **zero or more diagnostic lines followed by
exactly one `Error: ` line** (cobra's print of the returned error):

- **Singular-stage failure** (§3.1 stages 1–6, 8, 9): no diagnostic
  lines; the stage's error is returned as the `RunE` error and cobra
  prints it — `Error: <message>`.
- **Accumulated front-end failure** (§3.1 stage 7, §3.3): one
  diagnostic line per failure, in pipeline order, then a summary
  error is returned. Exact shapes:

```
file-diag  = <path>: <message>                      (queryfile.Parse failure)
query-diag = <path>: query <Name>: <message>        (cypher parse or resolution failure)
summary    = generate: <n> error | generate: <n> errors
```

`<path>` is the query-file path the CLI opened (the config-dir join,
§3.1 stage 2) — deterministic and clickable in editors. `<Name>` is
the annotation-declared query name. `<message>` is the underlying
error text verbatim: the CLI adds file/query context and never
rewrites package messages. One failure, one line: every underlying
error today is a single-line `fmt.Errorf` chain. Pluralisation is
pinned: `1 error`, else `<n> errors`.

Position semantics inside `<message>`, stated honestly: queryfile
messages carry file-absolute `line <n>`; cypher messages carry
`<line>:<col>` **relative to the query body** (the text sliced
between annotations), because `queryfile.AnnotatedQuery` does not
record the body's starting line. Translating to file-absolute
positions needs a queryfile widening — deferred (§8).

Singular-stage message catalogue (`Error: ` prefix implied):

| stage failure                        | shape                                                                                   |
|--------------------------------------|-----------------------------------------------------------------------------------------|
| config file absent (`fs.ErrNotExist`)| `no config file at <path> (gqlc init, which creates one, arrives in a later release)`   |
| any other `config.Load` error        | verbatim (`config: …` — already carries path and line)                                  |
| schema file read                     | `schema: <os error>` (the os error carries the path)                                    |
| schema parse                         | `schema <path>: <message>`                                                              |
| procsig load                         | verbatim (`procsig: …` — already carries the path)                                      |
| queries dir unreadable               | `queries: <os error>`                                                                   |
| zero query files after filtering     | `no query files (*.cypher) in <dir>`                                                    |
| `codegen.Generate` error             | verbatim (codegen messages name the query / entity / position)                          |
| axis-mapping drift guard (§3.2)      | `internal: no pipeline mapping for <axis> "<value>"`                                    |
| output path exists, not a directory  | `output: <dir> is not a directory`                                                      |
| tripwire abort                       | §5.3                                                                                    |
| mkdir / wipe / write failure         | `output: <os error>`                                                                    |

The missing-config branch is taken only when
`errors.Is(err, fs.ErrNotExist)` holds — the exact seam
`config.Load` documents for this purpose. The message names `gqlc
init` as forthcoming; CLI-2 rewords it when the command lands.

## 3. Pipeline

### 3.1 Stages

Sequential, single-goroutine — the ordering contracts (§3.3, §4, §5)
come for free and the corpus front-ends in milliseconds. Stages 1–8
perform **no filesystem writes**; the first write is stage 9, after
every check has passed.

| # | stage                | seam                                                              | failure class |
|---|----------------------|-------------------------------------------------------------------|---------------|
| 1 | load config          | `config.Load(cfgPath)`                                            | singular      |
| 2 | resolve paths        | `filepath.Dir(cfgPath)` + `filepath.Join`                         | —             |
| 3 | parse schema         | per `SchemaLang` (§3.2): `gql.New().Parse(bytes.NewReader(b))`    | singular      |
| 4 | load procsig         | iff `ProcsigPath != ""`: `procsig.Load(resolved)`                 | singular      |
| 5 | construct front end  | `cypher.New(cypher.WithRegistry(reg))`, `resolver.New(sch, resolver.WithRegistry(reg))` | — |
| 6 | discover query files | `os.ReadDir(resolved QueryDir)` + filter (§4)                     | singular      |
| 7 | front-end walk       | `queryfile.New().Parse` → cypher `Parse` → `Resolve`, per §3.3    | accumulating  |
| 8 | generate             | `codegen.New(opts…).Generate(codegen.Input{Schema: sch, Queries: batch})` | singular |
| 9 | write output         | §5 protocol                                                       | singular      |

Stage 1: the CLI is version-agnostic — `Load`'s probe-then-dispatch
seam normalises every accepted on-disk version into the one `Config`,
so nothing here inspects or branches on the config version.

Stage 2: for each of `SchemaPath`, `QueryDir`, `OutputDir`,
`ProcsigPath` — absolute paths pass through unchanged; relative paths
become `filepath.Join(dir(cfgPath), p)` (Join cleans). No existence
checks here; each consuming stage owns its own open failure.

Stage 4: when the `procsig` key is absent the pipeline runs with the
zero `procsig.Registry` — documented to miss on every `Lookup` — so a
CALL query in a registry-less project fails at cypher parse with
`ErrUnknownProcedure`, which is the correct diagnosis. The same
registry value feeds both the parser and the resolver (stage 5),
mirroring the codegen test harness's reference pipeline.

Stage 5: both constructions happen once, outside the query loop.
`(*Resolver)` is documented safe for reuse; the cypher parser builds
all per-parse state inside `Parse`.

Stage 7 lowers each fully-successful query into the codegen envelope:

```go
codegen.NamedQuery{
	Name:        aq.Name,
	Cardinality: aq.Cardinality,
	SourceFile:  name,     // the file's basename, e.g. "people.cypher"
	SourceText:  aq.Text,
	Validated:   vq,
}
```

`SourceFile` carries the basename, per the field's own contract. Flat
discovery (§4) makes basenames unique by construction, so codegen's
`ErrDuplicateSourceFile` is unreachable from this front end. A query
name duplicated **across** files is codegen's batch-level
`ErrDuplicateQueryName`, surfacing as a singular stage-8 error.

Batch order is discovery order × annotation order — the order codegen
walks for method emission and per-source grouping, so §4's ordering
rule pins the output bytes.

### 3.2 Axis mapping

Each config axis maps through an exhaustive switch with a drift-guard
default (`internal: no pipeline mapping for <axis> "<value>"`) — the
loader guarantees vocabulary membership, so the default arm only
fires when `internal/config` grows a member before the CLI learns it:

| config value                  | pipeline binding                              |
|-------------------------------|-----------------------------------------------|
| `config.SchemaLangGQLC`       | `gql.New()`                                   |
| `config.QueryLangOpenCypher`  | `cypher.New(cypher.WithRegistry(reg))`        |
| `config.DriverNeo4jGoV5`      | `codegen.WithDriverVersion(codegen.DriverV5)` |
| `config.DriverNeo4jGoV6`      | `codegen.WithDriverVersion(codegen.DriverV6)` |

### 3.3 Error accumulation semantics

Within stage 7, failures **accumulate across files and queries**; the
underlying packages short-circuit internally and that is fine — the
contract is that one broken query never hides another:

- A file whose `queryfile.Parse` fails contributes exactly one
  `file-diag` and the walk moves to the next file (its queries cannot
  be enumerated).
- A query whose cypher parse fails contributes one `query-diag`;
  resolution is not attempted (it would re-report the same defect).
  A query whose parse succeeds and whose resolution fails contributes
  one `query-diag`. Either way the walk continues with the next
  query. At most one diagnostic per query — the first failing stage.
- After the full walk, if any diagnostic accumulated: print all of
  them in order (files lexicographic per §4, queries in annotation
  order within a file), return the summary error (§2.3), exit 1.
  **Stage 8 does not run; nothing is written; the output directory is
  not created, read, or modified.**

### 3.4 codegen widening: `WithPackageName`

`codegen` today derives the emitted package identifier from
`Schema.Name` (`derivePackage`: `ToLower`, then the
`^[a-z][a-z0-9_]*$` grammar) and exposes no seam for the config
file's required `package` key — which the config-file-format spec §3
documents as "generated package name" and CONTEXT.md carries as the
output-package vocabulary. Without a seam, `generate` would silently
ignore a required, validated field. CLI-1 adds the option:

```go
// WithPackageName overrides the Schema.Name-derived package
// identifier with an explicitly configured one. The empty string —
// the zero value — keeps the derivation.
func WithPackageName(name string) Option
```

Semantics: when non-empty, `generate()` uses the value instead of
calling `derivePackage`, after validating it against the same
`packageIdent` grammar — failure is the existing
`ErrInvalidPackageName`, wrapped as
`fmt.Errorf("%w: configured package %q", ErrInvalidPackageName, name)`.
The loader's `token.IsIdentifier` check is looser (it admits `Db`,
Unicode letters); the emission grammar is enforced at generation, the
same loader-is-mechanism / consumer-is-policy split the
config-file-format spec §8 draws. Zero-value compatibility means no
existing caller changes and the golden corpus stays byte-identical —
the fence for this widening. The CLI always passes
`codegen.WithPackageName(cfg.OutputPackage)` (the loader rejects an
empty `package`).

Rejected alternative: validating `OutputPackage ==
derivePackage(Schema.Name)` in the CLI — turns a working config into
an error whenever the schema is renamed, and leaves the config key
decorative.

## 4. Query-file discovery

The discovery rule, pinned as contract:

> A query file is a **non-directory** entry of the resolved queries
> directory whose name **ends in `.cypher`** and does **not begin
> with `.`**. Discovery does **not recurse**. Files are processed in
> `os.ReadDir` order — lexical by filename — which is therefore the
> diagnostic order (§3.3) and the codegen batch order (§3.1).

One clause, one reason:

- **`.cypher`**: the extension the entire golden corpus already uses
  (`test/data/codegen/valid/*/*.cypher`) and the one codegen's
  per-source emission bakes into generated file names
  (`<stem>.cypher.go`). Pinned per query language; a future second
  `QueryLang` member brings its own extension.
- **No recursion**: a flat directory makes `SourceFile` basenames
  unique by construction, so codegen's basename-collision sentinel
  (`ErrDuplicateSourceFile`) is structurally unreachable and no
  subdirectory-disambiguation scheme is needed. Widening to recursion
  later is backward-compatible — it accepts strictly more trees.
- **No dotfiles**: hidden files are not sources, and editor artefacts
  match the suffix — an emacs lockfile (`.#people.cypher`) is a
  dangling symlink whose open would fail the whole run. The clause
  also excludes the degenerate name `.cypher` (empty stem).
- **Ordering**: `os.ReadDir` guarantees entries sorted by filename,
  so the rule inherits a stdlib determinism guarantee instead of
  defining one.

Non-matching entries (subdirectories, `README.md`, dotfiles) are
skipped silently. Zero matches after filtering is the singular error
`no query files (*.cypher) in <dir>` — an empty queries directory is
a misconfiguration, not a successful empty generation (the same
reject-don't-guess posture as `queryfile.ErrNoQueries`).

## 5. Output-directory protocol (ADR 0012)

### 5.1 Tripwire algorithm

Runs only after stage 8 returned its `[]File`. Steps, in order:

1. `os.Stat` the resolved `OutputDir`. Absent → `os.MkdirAll` (0o755)
   and go to step 5. Present but not a directory → abort
   (`output: <dir> is not a directory`).
2. `os.ReadDir` the directory. Empty → step 5.
3. Sweep every entry in `ReadDir` order. An entry is **marked** iff
   it is a regular file whose **first line** (the bytes before the
   first LF; a file without an LF is its own first line) starts with
   `// Code generated by gqlc ` **and** ends with ` DO NOT EDIT.` —
   the two fixed halves of `render.go`'s marker, version-agnostic in
   the middle so output written by an older gqlc release passes.
   A subdirectory is never marked (a directory cannot carry the
   marker). The sweep collects **every** offender, not the first —
   the abort names them all (ADR 0012: "naming the offending files").
4. Offenders present → abort with the §5.3 message. **Nothing is
   deleted.**
5. Wipe: `os.Remove` every existing entry (after step 3 they are all
   proven-marked files; after step 1-absent or step 2-empty there are
   none). The directory itself is kept — its permissions and identity
   are not gqlc's to churn.
6. Write each `File` to `filepath.Join(outDir, f.Path)`, mode 0o644
   (the `config.Save` precedent), in slice order — `Generate`'s
   contract sorts the slice by `Path`, so write order is
   deterministic too.

The version-agnostic acceptance in step 3 is deliberate and pinned by
a test (§7): checking against the *current* `version.Version` would
make every gqlc upgrade brick its own output directory.

### 5.2 Ordering guarantee and failure windows

Because `codegen.Generate` is pure (no I/O, full `[]File` in memory),
**every** config, schema, front-end, and generation error surfaces
before step 5 — a failed run wipes nothing and writes nothing, the
ADR 0012 consequence restated here as a contract with tests.

The residual windows are stated honestly rather than engineered away:

- Killed between wipe (5) and write (6): only proven-gqlc-generated
  files were removed; the next run regenerates them. No user work is
  at risk.
- Killed mid-write, or a failed write syscall (disk full): a partial
  file may lack the marker, so the **next** run's tripwire names it
  and aborts; the remedy is deleting the named file. Safe but not
  self-healing; an atomic write-and-rename protocol is a non-goal
  (§8).

A config typo pointing `output` at a source tree — the ADR's
motivating scenario — hits step 3 (unmarked files, subdirectories)
and produces an abort that names them, never a deletion.

### 5.3 Abort message

```
output directory <dir> contains entries not generated by gqlc: <e1>, <e2>; move or delete them and re-run
```

`<dir>` is the resolved output path; entries are basenames in
`ReadDir` (lexical) order, subdirectories suffixed with `/`
(`helpers.go, tx/`). Singular-stage shape: no preceding diagnostics,
one `Error: ` line, grep-friendly and deterministic.

## 6. Package layout and the logger

### 6.1 Layout

`generate.go` lands beside `version.go` in `internal/cli`, exactly as
CLI-0 §3 planned. No separate orchestration package: the pipeline
*is* CLI policy — path resolution, error accumulation, ADR 0012 I/O —
it has exactly one caller, and every mechanism it composes already
lives in its own tested package. A package boundary here would add an
export surface with no second consumer. The command constructor plus
pipeline helpers stay unexported; `Main` remains the package's only
export.

### 6.2 `internal/logger` deleted

CLI-0 §5 left `internal/logger` caller-less and deferred its fate to
CLI-1. Disposition: **deleted**. Three facts decide it: the package
has zero importers (its only caller was the root `main.go` harness
CLI-0 removed — today only a `.golangci.yml` message string names
it); its JSON handler writes to **stdout**, which §2.3 pins empty;
and generate's diagnostic surface is the line-oriented stderr
contract, which slog would fight, not serve. Dead wiring with a
contradicting contract gets removed — internal APIs carry no
compatibility burden. If a `--verbose` surface is ever designed, it
arrives with its own spec. The two forbidigo messages reword to point
at the command writers, e.g.
`route output through cmd.OutOrStdout()/cmd.ErrOrStderr()`; the
banned patterns themselves do not change.

## 7. Test plan

All CLI tests drive `newRootCmd()` in-process (the CLI-0
`executeRoot` harness) against fixture trees built in `t.TempDir()` —
a helper writes a minimal project (schema.gql: one node type; one
`.cypher` file: one `:many` query; a config wired to them) and each
test perturbs it. Real front end and codegen throughout; no seams
faked.

| test                                  | proves                                                                                                   |
|---------------------------------------|----------------------------------------------------------------------------------------------------------|
| `TestGenerateHappyPath`               | exit 0; stdout and stderr empty; output dir holds exactly `db.go`, `models.go`, `querier.go`, `<stem>.cypher.go`; every file marker-headed |
| `TestGenerateDeterministic`           | two runs produce byte-identical trees                                                                     |
| `TestGenerateConfigMissing`           | pinned missing-config message (mentions `gqlc init`), exit 1                                              |
| `TestGeneratePathResolution`          | config in a subdirectory with relative keys, invoked via `-f` from elsewhere: paths resolve against the config's directory, not the cwd |
| `TestGeneratePackageFromConfig`       | generated package clause equals the config `package` key where the schema-name mangle would differ        |
| `TestGenerateDriverAxis`              | table: v5 / v6 configs emit the matching `neo4j-go-driver/v5|v6` import in `db.go`                        |
| `TestGenerateProcsigWiring`           | a CALL query with a `procsig` key generates; the same project without the key fails with an unknown-procedure `query-diag` |
| `TestGenerateAccumulation`            | two broken files + one good: every diagnostic present, file-then-annotation order, `generate: <n> errors` summary, exit 1, output dir never created |
| `TestGenerateDiagnosticShapes`        | exact-match one `file-diag` (malformed annotation) and one `query-diag` (unknown label) against §2.3      |
| `TestGenerateDiscoveryFilter`         | queries dir seeded with `README.md`, a subdirectory holding a `.cypher`, and `.hidden.cypher`: only top-level non-dot `*.cypher` files are consumed |
| `TestGenerateNoQueryFiles`            | empty queries dir → pinned message, exit 1                                                                |
| `TestGenerateTripwire`                | table: unmarked file / subdirectory / marker-less empty file → abort naming every offender; all pre-existing bytes untouched |
| `TestGenerateTripwireOldVersion`      | a file marked `// Code generated by gqlc v0.0.1. DO NOT EDIT.` passes the sweep and is replaced           |
| `TestGenerateWipesStale`              | a pre-existing marked `stale.cypher.go` is gone after a successful run — no phantom files                 |
| `TestGenerateFailedRunTouchesNothing` | a resolution error with a populated marked output dir: contents byte-identical after the failed run       |
| `TestWithPackageName` (codegen)       | option honoured; empty keeps the `Schema.Name` derivation; a grammar-violating value is `ErrInvalidPackageName` naming the configured string |

Gate: zero changes under `test/data/codegen/` — `WithPackageName`'s
zero value and the untouched fixtures keep every golden byte.

## 8. Non-goals

- `gqlc init` (CLI-2) — the missing-config message forward-references
  it and CLI-2 rewords that message.
- Watch mode, incremental generation, parallel front-ending —
  sequential single-shot keeps every ordering contract trivial at
  today's corpus sizes.
- Logging and verbosity flags — the logger is deleted (§6.2); the
  stderr line contract is the whole diagnostic surface.
- Exit codes beyond 0/1 (§2.2).
- File-absolute positions in `query-diag` messages — needs
  `queryfile.AnnotatedQuery` to carry each body's starting line; a
  front-end widening with its own bead, not a CLI concern.
- Atomic output writes — the §5.2 partial-write window aborts the
  next run by name instead of self-healing; write-to-temp-and-rename
  is complexity without a data-loss risk to buy off.
- Recursive discovery, additional extensions, config-declared globs
  (§4 — each is a compatible later widening).
- README install/usage section. CLI-0 §8 pointed it at CLI-1; it
  moves to CLI-2 deliberately: the documented workflow starts with a
  hand-written config until `init` exists, so the README documents
  the whole `init` → `generate` flow once, when it is real.

## 9. Acceptance criteria

1. In a configured project, `gqlc generate` exits 0 with empty stdout
   and stderr and writes the generated package — every file
   marker-headed, package clause taken from the config `package` key —
   into the output directory; running it twice yields byte-identical
   output.
2. A project with several broken queries reports **all** of them —
   `<path>: query <Name>: <message>` lines in pipeline order, one
   summary `Error:` line — exits 1, and writes nothing.
3. An output directory containing any entry without the gqlc marker
   aborts the run with a message naming every offender; no file is
   deleted or modified. A directory of only marked files (any
   version) is wiped and rewritten.
4. Relative config paths resolve against the config file's directory:
   `-f` from an unrelated cwd behaves identically to running in the
   config's directory.
5. `internal/logger` is gone; `just lint`, `just test`, and
   `just tidy-check` are green with the reworded forbidigo messages.
6. `gqlc --help` lists `generate` beside `version`; `init` is still
   absent.
7. All CI jobs green; `test/data/codegen/` byte-identical to master.
