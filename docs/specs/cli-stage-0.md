# Stage CLI-0 spec — cmd/gqlc skeleton: cobra root + version

The implementation brief for CLI-0, the first slice of the CLI epic:
cobra root command, `version` subcommand, real entrypoint at
`cmd/gqlc/`, root `main.go` dev harness deleted. Library choice is
fixed by ADR 0011 (cobra; huh arrives at CLI-2) — not re-argued here.
The one real design point CLI-0 owns is the **version seam** (§4):
one variable, one `-ldflags -X` target, serving both the
generated-file header and `gqlc version`.

Tracking: bead `gqlc-m1c.1` (epic `gqlc-m1c`). `generate` (CLI-1,
`gqlc-m1c.2`) and `init` (CLI-2, `gqlc-m1c.3`) are out of scope beyond
the root command being shaped for subcommand growth.

---

## 1. Deliverables

- `cmd/gqlc/main.go` — thin `package main`: `os.Exit(cli.Main())` and
  nothing else. Preserves the old harness's convention: main holds the
  only `os.Exit`, so defers in the command layer always fire.
- `internal/cli/` — the command package (§3): `root.go` (`Main`,
  `newRootCmd`), `version.go` (`newVersionCmd`), `cli_test.go`.
- `internal/version/` — the version seam (§4): `version.go` carrying
  the exported `Version = "dev"` variable.
- `internal/codegen/version.go` — **deleted**; `render.go` reads
  `version.Version` instead (§4.2).
- Root `main.go` — **deleted** wholesale (§5).
- `go.mod` — `github.com/spf13/cobra` enters as a direct dependency
  (ADR 0011). `pflag` is already in the tree (indirect) and stays
  indirect; `mousetrap` arrives indirect. `just tidy-check` fences the
  delta.

No justfile or workflow changes (§6). No golden changes (§4.3, §7).

## 2. Command surface

### 2.1 Root command

Constructed fresh per invocation by `newRootCmd()` — no package-level
command variables (shared mutable command state breaks parallel
tests; constructors are the cobra-idiomatic seam for test injection).

Exact user-facing copy:

```go
Use:   "gqlc",
Short: "Generate type-safe Go from a graph schema file and openCypher queries",
Long: `gqlc generates type-safe Go from a graph schema file and a directory of
openCypher query files, the way sqlc does for SQL.

A project declares its generation pipeline in a config file (gqlc.yaml):
the schema path, the query directory, the output directory, and the
schema-language / query-language / driver axes.`,
```

- **No `Run` on the root.** Cobra's default fires: bare `gqlc` prints
  help and exits 0. When `generate` lands (CLI-1) the root stays
  Run-less — gqlc is subcommand-shaped, like sqlc.
- **No `rootCmd.Version`, so no `--version` flag.** One way to ask:
  the `version` subcommand. Setting both creates two output formats to
  keep in sync for zero gain.
- `CompletionOptions.HiddenDefaultCmd = true` — completions keep
  working, unadvertised in a three-command CLI. Cobra's `help`
  command stays visible: hiding it fights the framework's
  discoverability idiom. This reads the bead's "help lists version
  only" as "no gqlc-authored command other than version".
- `SilenceUsage`/`SilenceErrors` stay false. The only CLI-0 errors are
  parse errors (unknown command/flag), where cobra's error-plus-usage
  dump is exactly right. Revisit at CLI-1 when runtime errors
  (config load, generation) arrive and a usage dump would be noise.

### 2.2 `version` subcommand

```go
Use:   "version",
Short: "Print the gqlc version",
Args:  cobra.NoArgs,
```

Output is the bare version followed by one LF — **exactly
`dev\n`** on a default build:

```go
fmt.Fprintln(cmd.OutOrStdout(), version.Version)
```

Bare `<version>\n`, not `gqlc <version>\n`: sqlc prints the bare
version, and `$(gqlc version)` then interpolates directly into
scripts and bug reports without field-splitting. Nothing is written
to stderr on success.

`fmt.Fprintln` through `cmd.OutOrStdout()`, never `fmt.Println`:
forbidigo bans bare `fmt.Print*` repo-wide (.golangci.yml), and the
command's writer is the seam the tests capture.

### 2.3 Exit codes

| condition                                             | exit |
|-------------------------------------------------------|------|
| bare root, `--help`, `help`, `version` success        | 0    |
| unknown command, unknown flag, args where none allowed | 1    |

`cli.Main() int` maps `Execute() error` to 0/1; `cmd/gqlc/main.go`
passes it to `os.Exit`. Two codes only at CLI-0; a richer taxonomy
(config error vs generation error) is a CLI-1 question if it is a
question at all.

## 3. Package layout

`internal/cli`, not `cmd/gqlc` proper: `cmd/` holds only the
two-line shim, so the command layer is a normal internal package —
testable in-process, importable by nothing else (internal), and the
epic's later commands (`generate.go`, `init.go`) land as siblings of
`version.go`. `Main` is the package's single exported symbol at CLI-0.

## 4. The version seam

### 4.1 `internal/version`

The version string moves to its own leaf package with an **exported**
variable — the single `-ldflags -X` target for the whole module:

```go
// Package version holds the gqlc version string: the single
// -ldflags -X substitution target serving both the generated-file
// header (internal/codegen) and the `gqlc version` command.
package version

// Version is "dev" unless a release build overrides it:
//
//	go build -ldflags "-X github.com/areqag/gqlc/internal/version.Version=$(git describe --tags)"
//
// var, not const — -ldflags -X only overrides string variables
// (C6 §4.1). The codegen golden corpus pins the default: it must
// stay exactly "dev" or every golden header changes.
var Version = "dev"
```

Rejected shapes, one line each:

- **Export from `internal/codegen`** (`codegen.Version`): wrong owner —
  the version is binary identity, not generator behaviour, and C6 §4.1
  deliberately kept codegen's copy unexported.
- **Own it in `internal/cli`**, codegen imports the CLI: inverted
  dependency; the generator must stay CLI-agnostic (the
  config-file-format §8 boundary).
- **Two variables, two `-X` flags**: drift between header and
  `version` output is exactly the silent-no-op failure class C6 fixed;
  one symbol makes it unrepresentable.

### 4.2 Codegen delta

`internal/codegen/version.go` is deleted; `render.go`'s header
emission reads `version.Version` directly. No behaviour change: same
default bytes, same link-time override semantics, still a var read
(no I/O, no runtime lookup), so the C0 §2.3 double-run determinism
contract is untouched.

### 4.3 Golden-byte constraint

The default value stays byte-exactly `"dev"`. Golden files under
`test/data/codegen/` pin `// Code generated by gqlc dev. DO NOT
EDIT.\n\n`, and `TestGeneratedHeaderFormat` asserts that prefix
byte-exactly against the skeleton fixture. **The CLI-0 PR contains
zero changes under `test/data/codegen/`** — the golden suite and
`just test-codegen-fence` passing unmodified is the acceptance fence.

### 4.4 Errata carried against the C6 spec

No `-ldflags` reference exists in the justfile or any workflow
(verified at 3eaae6a) — the C6 "release recipe" was normative-future,
so nothing moves; the recipe, when it lands, uses the new target.
Historical specs stay as written (the C6 posture toward C0); this
table is the forward pointer:

| C6 statement (codegen-stage-c6.md)                                        | superseded by                                                      |
|---------------------------------------------------------------------------|--------------------------------------------------------------------|
| §4.1: source of truth is `internal/codegen/version.go`, unexported `version` | `internal/version/version.go`, exported `Version`                  |
| §1, §4.1: `-X github.com/areqag/gqlc/internal/codegen.version=...`        | `-X github.com/areqag/gqlc/internal/version.Version=...`           |
| §4.1 "Unexported … nothing else touches it"                               | two readers: the generated-file header and `gqlc version`          |

Every other C6 §4 decision (var not const, `-ldflags` over
`runtime/debug.BuildInfo`, `"dev"` default, no input hash) stands and
transfers to the new package unchanged.

## 5. Root `main.go` and the logger

The 49-line dev harness (opened and parsed the sample schema file) is
deleted, not relocated — its job ended when the golden harness
arrived. Its one surviving convention is §1's
main-holds-the-only-os.Exit shape.

`internal/logger` stays but gains no caller at CLI-0: no CLI-0
command emits a log record, and initialising a JSON logger that can
never fire is dead wiring — `generate` (CLI-1) owns wiring
`logger.Init` and the level/verbosity flag surface. Lint tolerates
the caller-less package (`unused` does not flag exported symbols).

## 6. justfile and CI

No changes. `just test`'s `go build ./...` link-checks `cmd/gqlc`
exactly as it link-checked the root `main.go` (the recipe comment
"package main … has no tests" stays true: `cmd/gqlc` has no tests;
`internal/cli` carries them). The parity rule binds CI checks to just
recipes — it does not demand a recipe per developer action, and
`go install ./cmd/gqlc` is the plain toolchain, so no install recipe.
The go.mod delta rides the existing `tidy` job.

## 7. Test plan

All tests drive `newRootCmd()` in-process via `SetArgs` / `SetOut` /
`SetErr` into `bytes.Buffer`s, then `Execute` (or `ExecuteC` where the
resolved command's identity is asserted):

| test                        | proves                                                                     |
|-----------------------------|-----------------------------------------------------------------------------|
| `TestRootBareInvocation`    | bare `gqlc` prints help (contains the Short line, lists `version`), exit 0  |
| `TestRootHelpCommandList`   | `--help` lists `version`; `completion` hidden; no `generate`/`init` yet     |
| `TestVersionOutput`         | `version` writes exactly `version.Version + "\n"` to stdout, empty stderr   |
| `TestVersionRejectsArgs`    | `gqlc version extra` errors (NoArgs), exit 1                                |
| `TestUnknownCommand`        | `gqlc frobnicate` errors naming the command, exit 1                         |
| `TestVersionLdflagsOverride`| real `go build` of `cmd/gqlc` with the §4.1 `-X` flag into `t.TempDir()`, exec, output is the overridden value + LF |

`TestVersionLdflagsOverride` is the suite's only process-spawning
test (~1–2 s, build-cached). It stays in the default suite: it is the
sole automated fence for acceptance criterion 3 and for the
const-regression class C6 §4.1 fixed (an override that silently
no-ops). If pre-push wall-time regresses noticeably, gating it
behind `testing.Short` is the code PR's call.

Gate: zero golden updates anywhere in the PR (§4.3).

## 8. Non-goals

- `generate` and `init` commands (CLI-1 / CLI-2) — the root command
  merely leaves room for them. `generate`'s output-directory
  semantics are already fixed (ADR 0012); nothing here touches them.
- Logger wiring, verbosity flags, any flag beyond cobra's built-ins.
- A root `--version` flag (§2.1).
- Exit codes beyond 0/1 (§2.3).
- Release recipe / goreleaser wiring; only the `-X` target string is
  pinned here (§4.1).
- README / user-facing install docs — the README has no install
  section today; it gains one when the binary does something (CLI-1).
- charmbracelet/fang (ADR 0011: open option, deliberately not now).

## 9. Acceptance criteria

1. Root `main.go` gone; `go build ./...` green.
2. `go install ./cmd/gqlc` yields a `gqlc` binary (and, once merged,
   `go install github.com/areqag/gqlc/cmd/gqlc@latest` resolves).
3. `gqlc version` prints `dev\n` on a default build and `v1.2.3\n`
   when built with
   `-ldflags "-X github.com/areqag/gqlc/internal/version.Version=v1.2.3"`;
   the generated-file header carries the same value from the same
   variable.
4. `gqlc --help` lists no gqlc-authored command other than `version`
   (`help` visible, `completion` hidden).
5. All CI jobs green, `test/data/codegen/` byte-identical to master.
