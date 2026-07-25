# Spec — multi-target config: one schema file, many generated packages

The implementation brief for the change ADR 0013 records: the config
file grows from one generation target to a list of them, and the
loader, the pipeline, the output-write protocol and `gqlc init` follow.
The wire shape, the `graph` key name, the per-entry `schema`, the
version-1 breaking change, the fail-fast-per-entry error posture, and
the `init --add` flag are fixed by ADR 0013 and are not re-argued here.
The design points this spec owns are the old-flat-shape detection
(§4.2), the output-overlap rule and its honest limit (§4.3), the
entry-prefix rule (§4.1), the widened pipeline contract (§6), the
two-phase write (§7), and the branch split that keeps master coherent
(§9).

Tracking: epic `gqlc-0gb`, with children `gqlc-0gb.1` (this spec),
`gqlc-0gb.2` (config loader), `gqlc-0gb.3` (pipeline and the write
protocol), `gqlc-0gb.4` (`init --add`). Lands as a four-branch stack in
that order.

---

## 1. Purpose and scope

A **generation target** is one entry of the config file's `graph`
sequence: one schema, one query directory, one language pair, and one
Go package written into one output directory. `gqlc generate` runs
every target the file declares, in document order. The term names the
config-file entry throughout this spec and every message it pins;
"typed repository" (ADR 0010) stays the name of the generated artifact,
of which one target produces a package-full.

In scope: `internal/config` (wire shape, loader, canonical form),
`internal/cli/pipeline` (run every target), `internal/cli` (write
protocol, `init`). Out of scope: `internal/codegen`, `internal/
resolver`, and the front end, none of which learn that targets exist —
each target drives them exactly as the single-target pipeline does
today.

## 2. Wire schema (version 1)

A YAML mapping with two keys. Nested levels are described in §2.2 and
§2.3; the order of every table is the canonical `Save` emission order
(§5).

### 2.1 Document

| # | key       | type     | required | valid values | semantics                                                                 |
|---|-----------|----------|----------|--------------|---------------------------------------------------------------------------|
| 1 | `version` | int      | yes      | `1`          | on-disk format version; wire-only, never part of `Config`; must be a true YAML integer scalar (`!!int`), never coerced (config-file-format §5, §6.2) |
| 2 | `graph`   | sequence | yes      | ≥ 1 entry    | the generation targets, in order (`Config.Targets`); an empty sequence is rejected |

### 2.2 Entry (`graph[i]`)

| # | key               | type    | required | valid values                 | semantics                                                              |
|---|-------------------|---------|----------|------------------------------|-------------------------------------------------------------------------|
| 1 | `schema`          | string  | yes      | non-empty                    | path to this target's schema file (`Target.SchemaPath`)                 |
| 2 | `schema_language` | enum    | yes      | `gql`                        | language the schema file is written in (`Target.SchemaLang`)            |
| 3 | `queries`         | string  | yes      | non-empty                    | directory holding this target's query files (`Target.QueryDir`)         |
| 4 | `query_language`  | enum    | yes      | `opencypher`                 | language the query files are written in (`Target.QueryLang`)            |
| 5 | `procsig`         | string  | no       | non-empty when present       | path to a procedure-signature registry file (`Target.ProcsigPath`); omit the key when unused — an explicit `""` is rejected, a null value equals omission |
| 6 | `gen`             | mapping | yes      | see §2.3                     | the code-generation block for this target                               |

Entries are independent: two targets may name the same schema file,
the same query directory, or the same package name. The one cross-entry
rule is §4.3.

### 2.3 `gen` and `gen.go`

`gen` has exactly one key, `go`, required, a mapping. The level exists
because a second generated language would be a sibling key here, which
is where sqlc puts it; today any other key under `gen` is an unknown
key and rejects.

| # | key       | type   | required | valid values                 | semantics                                                                      |
|---|-----------|--------|----------|------------------------------|----------------------------------------------------------------------------------|
| 1 | `package` | string | yes      | a valid Go identifier        | generated package name (`Target.Go.Package`); `go/token.IsIdentifier`, so Go keywords are rejected; casing is not policed |
| 2 | `out`     | string | yes      | non-empty; §4.3              | directory generated code is written to (`Target.Go.Out`), owned exclusively by gqlc (ADR 0012) |
| 3 | `driver`  | enum   | yes      | `neo4j-go-v5`, `neo4j-go-v6` | client library the generated code targets (`Target.Go.Driver`)                  |

The three enum axes keep their exported Go types (`SchemaLang`,
`QueryLang`, `Driver`), their constants, and their `XxxValues()`
functions unchanged: they remain the single source of truth shared by
loader messages and `gqlc init` prompts.

### 2.4 Worked example

The canonical fixture, `internal/config/testdata/canonical.gqlc.yaml`
(§5), is the motivating project: one schema, two modules, two
packages, one driver version each.

```yaml
version: 1
graph:
  - schema: schema.gql
    schema_language: gql
    queries: internal/user/query
    query_language: opencypher
    procsig: procs.procsig.json
    gen:
      go:
        package: userdb
        out: internal/user/gen
        driver: neo4j-go-v5
  - schema: schema.gql
    schema_language: gql
    queries: internal/order/query
    query_language: opencypher
    gen:
      go:
        package: orderdb
        out: internal/order/gen
        driver: neo4j-go-v6
```

## 3. In-memory shape

```go
// Config is the canonical in-memory form of a config file: the
// generation targets it declares, in document order.
type Config struct {
	Targets []Target
}

// Target is one generation target: one schema and query directory
// front-ended into one generated Go package.
type Target struct {
	SchemaPath  string
	SchemaLang  SchemaLang
	QueryDir    string
	QueryLang   QueryLang
	ProcsigPath string
	Go          GoGen
}

// GoGen is a target's gen.go block.
type GoGen struct {
	Package string
	Out     string
	Driver  Driver
}
```

Field order per struct is wire order per level. There is still no
`Version` field and no path resolution: `Config` carries the raw
strings the file holds, and relative paths are resolved against the
config file's directory by the CLI (config-file-format §4, unchanged).

`Load`, `Decode`, `Save` and `Canonical` keep their signatures.

## 4. Loader semantics

Everything config-file-format §6 pins holds unchanged except where
this section states otherwise: the `config: ` prefix on every message,
no error sentinels, the `fs.ErrNotExist` wrap on open failures, the
tag-strict version probe, `KnownFields(true)` on the v1 decode
(recursive: an unknown key inside an entry or inside `gen.go` rejects
with the same shape), null-equals-omission for every key, and
omitted-versus-empty distinguished by pointer-typed wire fields.

The wire structs are `wireV1` (document, with `Graph *[]wireTarget` so
an omitted or null `graph` stays distinguishable from an empty
sequence), `wireTarget`, `wireGen`, and `wireGo`. Their names reach
users through yaml's unknown-key messages (§4.5).

### 4.1 The entry prefix

Every message the loader itself formats about one entry carries a
`graph[i]: ` prefix after the source label, whatever the entry count —
`config: gqlc.yaml: graph[1]: missing required field "queries"`. The
prefix is unconditional by design: a shape that varies with the entry
count is one more thing to test and one fewer thing to grep for.

Messages yaml.v3 formats — unknown key, duplicate key, wrong scalar
type, and the enum errors raised from `UnmarshalYAML` — carry no
prefix. They carry `line <L>` instead, which localises the fault
better than an index does. The split is not an accident of the library:
an error about an *absent* key has no node and therefore no line, so
the index is all there is; an error about a *present* node has a line
and does not need one. Where the loader has both, it prints both, index
first (`graph[1]: line 12: entry is null`).

Rejected: typing the enum wire fields as `*yaml.Node` and validating
them post-decode, purely to get the index onto enum errors. It moves
three vocabularies' validation away from the types that own them and
buys a locator the message already has.

### 4.2 The old flat shape

A file written for the previous format declares `version: 1`
truthfully, so the version probe passes it and the strict decode
reports one unknown-key line for every top-level key except `version`,
which still resolves. The result names each key the file happens to
carry and never says the format changed. The loader detects the shape
instead.

**Rule.** After the version probe and before the strict decode: if the
document has no `graph` key and carries at least one of the eight
former top-level keys — `schema`, `queries`, `output`, `package`,
`schema_language`, `query_language`, `driver`, `procsig` — the loader
reports the first of those keys in that order, with the line its value
sits on:

```
config: <src>: line 2: "schema" is not a top-level key; version 1 declares a "graph" sequence of generation targets, each carrying its own schema, queries, and gen.go block
```

The key list is frozen — it is the version-1-flat wire vocabulary, and
nothing will be added to it. A document that has both `graph` and a
stray top-level `queries` is not this case: it is a new-format file
with a leftover key, and the unknown-key error says so.

The message names no subcommand. `internal/config` stays CLI-agnostic
(config-file-format §1, §8), so the hint that `gqlc init` can rewrite
the file is not the loader's to give — and adding an error sentinel so
the CLI could give it would break the package's no-sentinel posture for
one line of copy.

### 4.3 Output-directory overlap

Two targets whose output directories overlap destroy each other's
output: the later target's ADR 0012 wipe deletes what the earlier one
just wrote. The loader rejects overlap.

**Rule.** For each entry in order, compared against every earlier
entry, on `filepath.Rel(earlier.Out, later.Out)`:

| result                        | verdict                          |
|-------------------------------|----------------------------------|
| `.`                           | the same directory — reject      |
| a path not starting with `..` | later is inside earlier — reject |
| a path starting with `..`     | disjoint — accept                |
| an error                      | not comparable — accept (below)  |

The reverse direction (earlier inside later) is covered by running the
comparison both ways.

`filepath.Rel` cleans both operands, so `internal/db`,
`internal/db/`, `./internal/db`, `internal//db` and
`./a/../internal/db` are one directory to this check, and
`internal/db` against `internal/dbgen` is correctly two. It is pure
string manipulation with no filesystem access, so config-file-format
§4's rule — the loader resolves no paths and stats nothing — survives
intact. `Config` stores the raw strings; only the comparison sees
cleaned ones.

**The honest limit.** `Rel` cannot relate an absolute path to a
relative one, nor a `../`-escaping path to a contained one; those pairs
return an error and are accepted as disjoint. Symlinked aliases, two
paths that differ only by case on a case-insensitive filesystem, and
bind mounts are equally invisible — every one of them needs the
filesystem, which a loader that is a pure function of the file's bytes
will not touch. What escapes the check costs generated files, never
hand-written ones: the tripwire still refuses to delete anything it
cannot prove gqlc wrote.

### 4.4 Null entries

yaml.v3 drops a null sequence element (`- ~`, or a bare `-`) rather
than decoding it into the zero struct, which would silently shift every
later entry's index away from the one the error messages print. The
loader rejects it: the version probe, which already parses the whole
document leniently, gains a `Graph *[]yaml.Node` field, and any element
whose tag is `!!null` is reported by its document index before the
strict decode runs.

### 4.5 Error catalogue

Every message the loader can produce, with `<src>` a file path or
`<stream>`. Rows unchanged from config-file-format §6.3 are marked
*(unchanged)*; the rest are new or reshaped by the nesting.

Document level:

| condition                                   | message shape                                                                          |
|---------------------------------------------|-----------------------------------------------------------------------------------------|
| file open failure                           | `config: open <path>: <os error>` (wraps, so `errors.Is` works) *(unchanged)*           |
| stream read failure                         | `config: read <src>: <error>` *(unchanged)*                                             |
| zero-byte input                             | `config: <src> is empty (expected a gqlc config declaring version: 1)` *(unchanged)*    |
| malformed YAML                              | `config: <src>: yaml: ...` *(unchanged)*                                                |
| document is not a mapping                   | `config: <src>: yaml: unmarshal errors: line <L>: cannot unmarshal <tag> ... into config.versionProbe` *(unchanged)* |
| `version` omitted (or null)                 | `config: <src>: missing required field "version" (this gqlc supports version 1)` *(unchanged)* |
| `version` not a `!!int` scalar              | `config: <src>: line <L>: field "version" must be a YAML integer (got !!float "1.5")` *(unchanged)* |
| `version` a `!!int` that overflows Go `int` | ``config: <src>: field "version": yaml: unmarshal errors: line <L>: cannot unmarshal !!int `9223372...` into int`` *(unchanged)* |
| `version` ≠ 1                               | `config: <src>: declares version <v>; only version 1 is supported` *(unchanged)*        |
| old flat shape (no `graph`, a former top-level key present) | `config: <src>: line <L>: "<key>" is not a top-level key; version 1 declares a "graph" sequence of generation targets, each carrying its own schema, queries, and gen.go block` |
| `graph` omitted (or null)                   | `config: <src>: missing required field "graph"`                                        |
| `graph` present but an empty sequence       | `config: <src>: field "graph" must not be empty; declare at least one generation target` |
| `graph` is not a sequence                   | `config: <src>: yaml: unmarshal errors: line <L>: cannot unmarshal <tag> into []config.wireTarget` |
| a null entry                                | `config: <src>: graph[<i>]: line <L>: entry is null`                                   |
| an entry is not a mapping                   | ``config: <src>: yaml: unmarshal errors: line <L>: cannot unmarshal !!str `x` into config.wireTarget`` |

Entry level. Every row is prefixed `config: <src>: graph[<i>]: `,
elided below; nested keys are named by their dotted path:

| condition                          | message shape                                                                     |
|------------------------------------|-------------------------------------------------------------------------------------|
| required key omitted (or null)     | `missing required field "<key>"` — one of `schema`, `queries`, `gen`, `gen.go`, `gen.go.package`, `gen.go.out` |
| required enum key omitted          | `missing required field "<key>" (valid values: <list>)` — `schema_language`, `query_language`, `gen.go.driver` |
| path/package key present but empty | `field "<key>" must not be empty`                                                   |
| `procsig` present but empty        | `field "procsig" is empty; omit the key when no procsig file is used`               |
| `gen.go.package` not a Go identifier | `package "<val>" is not a valid Go identifier`                                    |

Entry-level errors yaml.v3 formats, unprefixed (§4.1):

| condition                | message shape                                                                        |
|--------------------------|----------------------------------------------------------------------------------------|
| unknown key              | `config: <src>: yaml: unmarshal errors: line <L>: field <key> not found in type config.<wireV1\|wireTarget\|wireGen\|wireGo>` |
| duplicate key            | `config: <src>: yaml: unmarshal errors: line <L>: mapping key "<key>" already defined at line <M>` |
| non-scalar path/package  | `config: <src>: yaml: unmarshal errors: line <L>: cannot unmarshal <tag> into string` |
| non-scalar enum value    | `config: <src>: line <L>: invalid <key>: expected a scalar value, got a YAML <kind>` |
| invalid enum value       | `config: <src>: line <L>: invalid <key> "<val>" (valid values: <list>)`              |

Cross-entry (§4.3), prefixed with the *later* entry's index:

| condition                | message shape                                                                                   |
|--------------------------|---------------------------------------------------------------------------------------------------|
| two entries share `out`  | `config: <src>: graph[<j>]: out "<p>" is already graph[<i>]'s output directory; each generation target must own its own` |
| one entry's `out` is inside another's | `config: <src>: graph[<j>]: out "<p>" is inside graph[<i>]'s output directory "<q>"; each generation target must own its own` |

### 4.6 Check order

Checks run in stages and the loader reports the first stage that fails:

1. the version probe (so a v2 file reports its version, not a shape
   complaint);
2. the old-flat-shape check (§4.2, before the strict decode, so the
   targeted message beats the unknown-key wall);
3. the null-entry check (§4.4);
4. `graph` present and non-empty;
5. the strict v1 decode;
6. per-entry post-decode checks, entries in index order and each
   entry's keys in the §2.2/§2.3 wire order;
7. the cross-entry `out` sweep (§4.3), reporting the first overlap in
   `(later, earlier)` index order.

Within the strict decode, ordering is not document order and is
yaml.v3's, exactly as config-file-format §6.3 describes: a
custom-unmarshal failure (an invalid enum) aborts the decode
immediately, while unknown-key, duplicate-key and wrong-type errors
accumulate and surface together only if the decode otherwise completes.

## 5. Canonical `Save` form

`Save` writes `version: 1`, then `graph`, then each target in order
with the keys of §2.2 and §2.3, `procsig` omitted when
`ProcsigPath` is empty; two-space indent, yaml.v3's plain scalars,
sequence items indented two spaces under `graph:`, a trailing newline,
file mode `0o644`. `Canonical` returns those bytes without writing;
`Save` is `Canonical` plus `os.WriteFile`, unchanged.

The §2.4 example is exactly `Canonical`'s output for its `Config` and
becomes the new `internal/config/testdata/canonical.gqlc.yaml`,
pinned byte-for-byte by `TestSaveEmitsFixtureBytes`. It is deliberately
a two-entry document with one `procsig` present and one absent and a
different driver per entry: the fixture that pins the nesting, the
sequence indentation, the `omitempty` behaviour inside a sequence
element, and the per-entry axis in one file.

For any `Config` the loader would accept, `Load(Save(c))` returns `c`
exactly and saving a loaded canonical file reproduces its bytes.
`Canonical` validates nothing — a zero `Config` emits `graph: []`,
which `Load` then rejects (§4.5); callers that assemble a `Config` in
memory own its validity (§8).

## 6. Pipeline contract

`internal/cli/pipeline` runs every target in document order.

```go
// Result is what a clean or diagnostic-accumulating run yields: one
// TargetResult per generation target the config declares, in document
// order, and the ordered diagnostic lines from every target's
// front-end walk.
//
// Caller invariant, non-negotiable: Targets is non-nil iff
// Diagnostics is empty AND err is nil. Every other outcome is the
// zero Result. Callers MUST NOT write any TargetResult when
// len(Diagnostics) > 0; that state means "errors accumulated, every
// batch discarded". Ignoring the invariant lets the ADR 0012 tripwire
// wipe marked output directories to write zero files.
type Result struct {
	Targets     []TargetResult
	Diagnostics []string
}

// TargetResult is one target's generated batch and the resolved
// directory the caller writes it to.
type TargetResult struct {
	Files  []codegen.File
	OutDir string
}
```

`Run(cfgPath string) (Result, error)` keeps its signature and its
subcommand-agnostic posture, `ErrConfigMissing` included. Return
contract, exhaustive:

- `err != nil` → `Result` is the zero value. This covers the stage-1
  config failures (`ErrConfigMissing` and every other `config.Load`
  error) and every per-target setup failure, which is wrapped
  `graph[<i>]: ` (§6.1).
- `err == nil, len(Diagnostics) > 0` → front-end accumulation.
  `Targets` is nil. The caller prints each line and returns its own
  summary error (CLI-1 §2.3); nothing is written.
- `err == nil, len(Diagnostics) == 0` → success. `Targets` has exactly
  one entry per config target, in document order; each `Files` is
  non-nil and sorted by `Path`, each `OutDir` is resolved against
  `filepath.Dir(cfgPath)`.

No other combinations exist; the caller may rely on this.

The `OutDir`-on-the-error-path clause of the single-target contract is
gone. It was never read — the CLI returns on a non-nil error — and with
N targets a partially populated `Targets` on a failed run is a
wipe-and-write footgun the zero value removes by construction.

### 6.1 Per-target stages and failure classes

For each target `i`, the pipeline runs CLI-1 §3.1 stages 3–8 against
that target's fields, with stage 2's path resolution applied to
`SchemaPath`, `QueryDir`, `ProcsigPath` and `Go.Out`. Every singular
failure — unmapped axis, schema read, schema parse, procsig load, empty
query directory, codegen — is wrapped `fmt.Errorf("graph[%d]: %w", i,
err)` and aborts the whole run at that entry, leaving the messages
CLI-1 §2.3 catalogues otherwise verbatim:

```
graph[1]: schema internal/order/schema.gql: unknown node type "Order"
graph[2]: no query files (*.cypher) in internal/audit/query
graph[0]: internal: no pipeline mapping for driver "neo4j-go-v7"
```

Stage 7 diagnostics accumulate across every target as they do across
every file, each line prefixed with its entry:

```
file-diag  = graph[<i>]: <path>: <message>
query-diag = graph[<i>]: <path>: query <Name>: <message>
```

Two consequences, stated rather than engineered away:

- **A setup failure discards the diagnostics accumulated before it.**
  If target 0 accumulates query diagnostics and target 1's schema is
  unreadable, the run reports the schema error alone; the query
  diagnostics resurface on the next run. A contract in which both a
  non-nil error and diagnostics can be populated buys the user one
  round-trip and costs every caller a fourth branch.
- **Stage 8 is skipped once any diagnostic exists.** The batch is
  discarded wholesale anyway (§6.2), so generating it could only
  produce a codegen error for a run that is already failing.

### 6.2 All-or-nothing

Any diagnostic from any target means nothing is written for any target.
This is the single-target invariant unchanged in spirit: the batch is
written only when the whole run is clean, with "the run" now spanning
every target the file declares.

### 6.3 No schema-parse cache

Each target parses its own schema file, even when ten entries name one
path. The rationale and the condition for revisiting it are ADR 0013's;
the code carries no cache, no memo map, and no comment implying one is
missing.

## 7. CLI output protocol

`runGenerate`'s diagnostic branch is unchanged: print `Diagnostics` in
order, return `generate: <n> error|errors`. The write branch becomes
two phases over `Result.Targets`. Phase A performs **no filesystem
mutation**; every step of phase B is a mutation.

### 7.1 Phase A — inspect

For each target `i` in order, with `dir` its `OutDir`:

1. `os.Stat(dir)`. Absent → record `create` for this target and
   continue with the next target. Present but not a directory → abort
   `graph[<i>]: output: <dir> is not a directory`. Any other stat
   failure → abort `graph[<i>]: output: <os error>`.
2. `os.ReadDir(dir)`; a failure aborts `graph[<i>]: output: <os
   error>`.
3. Sweep every entry in `ReadDir` order under the CLI-1 §5.1 step-3
   marker rule, unchanged and still version-agnostic: an entry is
   marked iff it is a regular file whose first line starts with
   `// Code generated by gqlc ` and ends with ` DO NOT EDIT.`. A read
   failure aborts `graph[<i>]: output: <os error>`. The sweep collects
   every offender, not the first.
4. Offenders present → abort
   `graph[<i>]: output directory <dir> contains entries not generated by gqlc: <e1>, <e2>; move or delete them and re-run`
   (CLI-1 §5.3's message, prefixed; basenames in `ReadDir` order,
   subdirectories suffixed `/`).
5. Record this target's swept entries as its wipe list.

Phase A aborts at the first target that fails. Within a target the
abort still names every offender — the two are different questions and
CLI-1 §5.3's answer to the second is unchanged.

### 7.2 Phase B — commit

Only after every target passed phase A, for each target `i` in order,
using the plan phase A recorded:

6. `create` → `os.MkdirAll(dir, 0o755)`; failure aborts
   `graph[<i>]: output: <os error>`.
7. `os.Remove` each entry on this target's wipe list — **exactly** the
   entries phase A proved marked, never a superset.
8. Write each `File` to `filepath.Join(dir, f.Path)`, mode `0o644`, in
   slice order.

Wipe and write are interleaved per target rather than wiping every
target and then writing every target: each directory then spends the
shortest possible time empty.

### 7.3 What the split guarantees, and what it does not

- **An abort under the tripwire mutates nothing.** Every stat, read and
  sweep for every target precedes every mkdir, remove and write, so the
  half-generated project the interleaved algorithm allows cannot
  happen. With one target this is CLI-1 §5.2's guarantee restated; with
  N it is the only way to keep it, because `codegen.Generate`'s purity
  no longer orders target 2's checks before target 0's writes.
- **gqlc still deletes only what it proved it generated.** Step 7 uses
  phase A's list, so a file that appears in the gap between the phases
  is not deleted — it survives the run and the next run's sweep names
  it. That is the one-directional guarantee: nothing unproven is
  deleted. It is not a claim that the directories cannot change
  underneath a run. gqlc takes no locks, and two concurrent `generate`
  runs over one config are outside the contract.
- **A commit-phase failure leaves a partial write.** Disk full at
  target 2's third file leaves targets 0 and 1 rewritten and target 2
  partial; a truncated file may lack the marker, so the next run aborts
  naming it and the remedy is deleting it. This is CLI-1 §5.2's
  residual window, widened from one directory to a prefix of them, and
  accepted on the same terms — atomic write-and-rename stays a non-goal
  (§11).
- **An output directory that cannot be created is discovered in phase
  B**, after earlier targets were rewritten, because `MkdirAll` is a
  mutation and phase A performs none. Pre-creating directories in
  phase A would shrink that window at the cost of the property the
  split exists for: that a run which aborts before commit leaves every
  byte of the tree unchanged, which is what the tests assert.

## 8. `gqlc init`

The wizard, its two groups, its eight prompts, the validators, the
preview/confirm split, the TTY guard, the abort contract, and the
accessible-mode wiring are CLI-2's and are unchanged. What changes is
what the wizard is bound to and how many targets a run may touch.

### 8.1 One target per run

`initDefaults()` returns a `Config` with exactly one `Target`; the
per-field defaults of CLI-2 §3.2 are unchanged except that `output`
is now `gen.go.out`. The form binds that target's fields, so a FRESH
run writes a one-entry `graph` list.

An EDIT run classifies as today and then checks the count. With one
target it prefills from it, exactly as CLI-2 §3.3 describes. With more
than one it **refuses before any form renders** — exit 1, nothing
written:

```
<path> declares <n> generation targets; init edits only a single-target config (edit it by hand, or run gqlc init --add to append another)
```

The wizard can express one target; prefilling from the first and
writing the canonical form would silently delete the rest.

### 8.2 `--add`

```go
cmd.Flags().BoolVar(&add, "add", false,
	"append a generation target to the existing config file")
```

`--add` prompts for one target and appends it to the file's existing
list. Flow selection is stricter than bare `init`'s, because appending
presupposes a file that loads:

| classification              | behaviour                                                                 |
|-----------------------------|---------------------------------------------------------------------------|
| loadable (any target count) | run the wizard, append, preview, confirm, write                           |
| `fs.ErrNotExist`            | `no config file at <path> (run gqlc init to create one)` — the message `generate` uses, from one shared unexported helper in `internal/cli` |
| any other load failure      | the `config.Load` error verbatim, exit 1                                  |

There is no broken-config dialogue under `--add`: "start fresh" would
replace the file the flag promises to append to.

**Prefill.** The appended target starts from the file's **last** entry
for the fields a second target usually shares — `schema`,
`schema_language`, `query_language`, `procsig`, `gen.go.driver` — and
**empty** for the three that must distinguish it: `queries`,
`gen.go.out`, `gen.go.package`. An empty prompt is the honest default
for a directory that must differ from every existing target's; the
non-blank validators already refuse to let it through.

**Validation.** The `out` input's `Validate` hook gains the §4.3
overlap check against the existing entries' `out` values, so `--add`
cannot write a config the loader would then reject — the same reason
CLI-2 §4.3 made the wizard enforce codegen's package grammar rather
than warn. Message shape at the prompt:

```
out "internal/user/gen" is already graph[0]'s output directory
out "internal/user/gen/sub" is inside graph[0]'s output directory "internal/user/gen"
```

**Preview, warnings, epilogue.** The preview block shows the whole
resulting file — every entry, canonical — at the existing confirm gate,
so an append is reviewed in the context it lands in. The comment notice
(CLI-2 §5.3) applies as always. The soft warnings and the epilogue
name the target the run authored: under `--add` the appended one,
under bare `init` the only one.

## 9. Branches and the documents each one updates

`AGENTS.md` requires every branch to be independently mergeable and
green. `internal/config`'s type change breaks its two consumers at
compile time, so branch 2 necessarily carries a mechanical adaptation
of each (§9.1). Branch 3 replaces the `pipeline` half; the `init` half
is permanent, because refusing a multi-target edit is a ratified
behaviour (§8.1), not a stopgap.

| branch | bead | code | documents |
|--------|------|------|-----------|
| 1 | `gqlc-0gb.1` | none | ADR 0013, this spec |
| 2 | `gqlc-0gb.2` | `internal/config` (§2–§5); consumers adapted mechanically (§9.1) | `config-file-format.md`, the CONTEXT.md "Config file" entry and the new "Generation target" entry |
| 3 | `gqlc-0gb.3` | `internal/cli/pipeline` (§6) **and** the two-phase write (§7); the single-target guard deleted | `cli-generate-pipeline.md`, `cli-stage-1.md`, `README.md` |
| 4 | `gqlc-0gb.4` | `init --add` (§8.2) | `cli-stage-2.md` |

The N-target pipeline and the two-phase write are one branch because
neither is shippable without the other. A `Result` carrying N targets
driven through the single-target write path writes and wipes target 0
before target 2 is inspected, which is exactly the half-generated abort
ADR 0013 forbids; and a two-phase write over a one-target `Result` is
a rename with no second target to protect. Splitting them would produce
two branches that only make sense merged — the shape `AGENTS.md`'s
independent-mergeability rule exists to prevent.

Branch 4 is `--add` alone. The one-target wizard binding and the
multi-target-edit refusal (§8.1) sit on branch 2, where they are
load-bearing rather than deferrable (§9.1).

Each behaviour document travels with the branch that makes it true.
This spec and the ADR land first and describe the whole change; no
other document is edited on branch 1, so master never carries a spec
that contradicts its own binary.

### 9.1 Branch 2's mechanical adaptation

Branch 2 rewrites the loader and leaves the rest of the tree
compiling, green, and honest about what it can do:

- `pipeline.Run` keeps its single-target `Result` and gains one guard
  after stage 1: `len(cfg.Targets) != 1` returns
  `config declares <n> generation targets; this gqlc runs one (multi-target generation lands in the next release)`.
  Its body reads `cfg.Targets[0]`. Branch 3 deletes the guard with the
  loop that replaces it.
- `init` binds the wizard to one target and refuses a multi-target
  edit (§8.1). The refusal is not deferrable: without it, branch 2's
  `init` would drop targets from a file the loader now accepts.
  `--add` stays on branch 4.
- The CLI, pipeline and config test fixtures are rewritten to the new
  wire shape.

At every point in the stack, `generate` either runs one target through
the existing write path (branches 1–2) or runs N through the two-phase
one (branch 3 onward). No merge leaves a version of `gqlc` that can
half-generate a project.

## 10. Test obligations

Per branch, the cases without which the change is not proven. Each
branch's PR carries the full suite in its package's existing style.

| branch | test | proves |
|--------|------|--------|
| 2 | `TestLoadCanonicalFixture`         | the §2.4 fixture loads into the expected two-`Target` `Config` |
| 2 | `TestSaveEmitsFixtureBytes`        | `Canonical` reproduces the fixture byte-for-byte (nesting, sequence indent, omitted `procsig`) |
| 2 | `TestLoadPreservesRawPaths`        | trailing slashes and `./` prefixes survive into `Config` unaltered |
| 2 | `TestRejectOldFlatShape`           | the previous format's canonical file produces the §4.2 message, not an unknown-key list |
| 2 | `TestRejectionTable`               | every §4.5 row, message-exact, including the `graph[i]:` prefix on entry 1 of a two-entry document |
| 2 | `TestOutOverlap` (table)           | equal, trailing-slash, `./a/../`-obscured, nested-either-way → rejected naming both indices; sibling, `dbgen`, and abs-vs-relative → accepted |
| 2 | `TestNullEntryRejected`            | `- ~` between two entries reports index 1, not a shifted index |
| 2 | `TestInitRefusesMultiTargetEdit`   | pinned §8.1 message, exit 1, file byte-untouched |
| 3 | `TestRunEveryTarget`               | a two-target config yields two `TargetResult`s in document order, each with the right package clause and driver import |
| 3 | `TestRunSetupFailureFailsFast`     | an unreadable schema at entry 1 → `graph[1]: schema: ...`, zero `Result` |
| 3 | `TestRunDiagnosticsSpanTargets`    | broken queries in entries 0 and 1 → every line present, in target-then-file-then-annotation order, each `graph[i]:`-prefixed; `Targets` nil |
| 3 | `TestRunAllOrNothing`              | one broken query in entry 1 → entry 0's files are not returned |
| 3 | `TestGenerateMultiTarget`          | both packages written; a second run byte-identical |
| 3 | `TestGenerateAbortsBeforeAnyWrite` | an unmarked file in entry 1's output directory → entry 0's directory byte-identical after the failed run, entry 1 untouched |
| 3 | `TestGenerateWipeListIsPhaseAs`    | a file created between inspect and commit survives the wipe (the §7.3 guarantee, driven through the seam) |
| 4 | `TestInitAdd`                      | append round-trip: prefilled fields carried from the last entry, blank distinguishing fields, preview shows every entry, written file loads |
| 4 | `TestInitAddOverlapRejectedAtPrompt`| an `out` equal to an existing target's re-prompts with the §8.2 message and never writes |
| 4 | `TestInitAddNoConfig`              | missing file → the `generate` hint, verbatim, from the shared helper |

## 11. Non-goals

- **A version 2 of the format.** Version 1 changes shape (ADR 0013);
  the probe-then-dispatch seam is untouched and unused by this change.
- **A hoisted document-root `schema` key**, or a document-root default
  for any other per-entry key. Every entry states its own inputs.
- **Per-target overrides of anything outside the entry.** There is
  nothing outside the entry but `version`.
- **A schema-parse cache** (§6.3), and any other cross-target sharing
  of parsed or generated state.
- **Cross-target codegen deduplication.** Two targets over one schema
  generate identical `models.go` bytes into two directories; that is
  the expected outcome of two independent packages, not duplication to
  factor out.
- **A multi-target `init` wizard loop.** One target per invocation:
  `init` or `init --add`.
- **Cross-entry checks beyond output overlap.** Shared query
  directories, repeated package names, and repeated schema paths are
  all legal.
- **Concurrent or parallel target execution.** Targets run in document
  order in one goroutine; the ordering contracts of CLI-1 §3.3 and §4
  survive unchanged because of it.
- **Atomic output writes** (§7.3), **file-absolute query positions**,
  **recursive query discovery**, and every other CLI-1 §8 non-goal.
- **A migration command** for the old flat shape. The §4.2 message
  states the new shape; the file is nine lines.

## 12. Acceptance criteria

1. The §2.4 example loads into a two-target `Config`, round-trips
   through `Save` byte-identically, and drives `gqlc generate` to two
   generated packages in two directories; a second run is
   byte-identical.
2. A config file in the previous format fails with the §4.2 message —
   naming the shape change, not the version, and not a list of unknown
   keys.
3. Two entries whose `out` values are equal or nested are rejected at
   load, naming both entries.
4. A run that aborts under the tripwire — for any target — leaves every
   output directory byte-identical, and the abort message names the
   entry and every offending file.
5. Broken queries in several targets report every diagnostic, each
   prefixed with its entry, and write nothing anywhere.
6. `gqlc init` writes a one-entry file, refuses to edit a multi-target
   one, and `gqlc init --add` appends a second target whose output
   directory cannot overlap an existing one.
7. `just test`, `just fmt-check`, `just lint` and `just tidy-check`
   green on every branch of the stack; `test/data/codegen/`
   byte-identical to master throughout.
