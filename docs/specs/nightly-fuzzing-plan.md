# Nightly fuzzing: the plan, the targets, and the work packages

The plan for epic **gqlc-jjot2**: a scheduled GitHub Actions workflow that
fuzzes the whole pipeline for a long budget every night, reports anything it
finds in a way that cannot be missed, and a triage loop that turns each
finding into a bead, a committed regression seed and a fix. It does not run on
pull requests and it does not run in any git hook.

Written 2026-10-27 against master `2182968b`. Every file:line, count and
throughput figure below was read or measured at that commit; §2 says which is
which. The one probe run while writing this found a real panic in the Cypher
parser inside nine seconds (§3), which is the clearest argument for the rest
of the document.

The owner's request, verbatim, is in the epic. The constraints it fixes: not
in PR CI, not in a hook, nightly plus `workflow_dispatch`, a budget of hours
inside GitHub's six-hour job ceiling, and a fix for every bug the fuzzer
finds.

**The decisions in one paragraph.** Engine: native `go test -fuzz` (§4). One
matrix job per fuzz target, the target list *discovered* from the tree at run
time rather than maintained in a file (§8.2). Budget: `-fuzztime 4h30m` per
leg under `timeout-minutes: 330`, which lands the owner's "about five hours"
of wall time with the cache save and the artifact upload still inside the
ceiling (§8.3). Corpus persistence through explicit `actions/cache/restore` and
`actions/cache/save` steps keyed per target (§8.4). A finding fails the leg,
uploads the crasher and the log as artifacts, and on a scheduled run opens or
reopens one labelled tracking issue, the same mechanism `codegen-live.yml`
already uses (§8.5). Every target ships with a planted-bug row that shows the
harness can find something (§10). Every workflow step is a `just` recipe (§9).

---

## 1. What is in the tree today

Read at `2182968b`:

- **No fuzz target exists.** `grep -rl 'func Fuzz' --include=*_test.go .`
  returns nothing. The word "fuzz" appears in `docs/adr/0026` and in
  `test/data/codegen/go.sum` only.
- **Property-based testing exists**, through `pgregory.net/rapid v1.3.0`, in
  two places: `internal/query/query_test.go` (hand-written generators for the
  query model — `genVariable`, `genLabelSet`, `genBinding`, `genQuery`) and
  `internal/query/cypher/parser_test.go` (corpus-sampling properties:
  `TestPropertyReadCoreParses`, `TestPropertyReferentialIntegrity`, with the
  invariant helpers `assertReferentialIntegrity`, `assertNamedBindingsUnique`,
  `assertParametersDeduped`). Those helpers are reusable from a fuzz target in
  the same `cypher_test` package without being moved.
- **The pipeline's entry points**, each a pure function of its input:
  - `gql.New().Parse(io.Reader) (schema.Schema, error)` and
    `gql.NewLoader(fs.FS).Load(name)` for `COPY OF` chains (ADR 0034);
  - `cypher.New(opts...).Parse(io.Reader) (query.Query, error)`;
  - `resolver.New(schema, opts...).Resolve(query.Query) (ValidatedQuery, error)`;
  - `neo4j.New(WithDriverVersion(v), WithPackageName(n)).Generate(codegen.Input)`
    and `age.New(WithPackageName(n)).Generate(codegen.Input)`, both
    `([]codegen.File, error)`;
  - `queryfile.New().Parse(io.Reader) ([]AnnotatedQuery, error)`,
    `config.Decode(io.Reader)`, `procsig.Decode(io.Reader)`;
  - `pipeline.Run(cfgPath, backends)` (`internal/cli/pipeline`), which composes
    all of the above from files on disk and carries the invariant *Targets is
    non-nil iff Diagnostics is empty and err is nil*;
  - `graph.PropertyType`, a string-encoded type whose constructors `ListOf`,
    `RecordOf`, `UnionOf` canonicalise so that string equality is type
    equality — the resolver unifies on `==`.
- **Sentinel discipline** is uniform: `cypher.AllSentinels()` (9),
  `resolver` (12, `allSentinels`), `codegen.AllSentinels()` (21),
  `age.Sentinels()`, `queryfile.AllSentinels()` (7), `schema/gql/errors.go`
  (25). Fail sites wrap with `fmt.Errorf("%w: ...")`. The resolver's
  `WithRegistry` doc names the exception: registry-miss and arity-mismatch at
  the CALL-argument walk "fire as plain non-sentinel errors (parser-
  authoritative drift arms, unreachable in-corpus)". A fuzzer is not bound by
  the corpus, so that arm is a shape §5's sentinel property has to classify
  rather than refuse outright.
- **Panic handling.** `internal/codegen/age/generate.go:60-70` recovers a
  `codegenBug` panic into an error; `internal/codegen/prepare.go:1052-1070`
  recovers around a `String()` call into code it does not own. Nothing
  recovers in the parsers or the resolver; `internal/resolver/resolve.go:2065`
  and `:2067` and `internal/query/cypher/expr.go:840` are deliberate
  "bug" panics. A fuzzer reaching any of them is a finding.
- **Corpora to seed from** (file counts at `2182968b`):
  - GQL schemas: `test/data/schema/gql/corpus` 138 `.gql`, `valid` 7, `invalid`
    18; `test/data/resolver/{valid,invalid}/schemas/*.gql`;
    `internal/codegen/{neo4j,age}/testdata/*.gql`.
  - Cypher: `test/data/resolver/valid` 433 files (215 `.cypher` plus their
    `.validated.golden.json`), `invalid` 167; the vendored openCypher TCK
    (230 files under `test/data/query/cypher/tck`, `.feature`), which
    `parser_test.go`'s `harvestExecutingQueries` already lifts into query
    strings; `internal/query/cypher/testdata/golden` holds 3199 JSON
    *outputs*, not inputs.
  - Codegen fixtures: `test/data/codegen/valid` 129 and `invalid` 61, each a
    directory with `manifest.json`, `schema.gql`, `queries.cypher` and goldens
    per target.
  - Query files, configs, procsig: `internal/queryfile/testdata`,
    `internal/config/testdata/canonical.gqlc.yaml`,
    `internal/procsig/testdata/canonical.procsig.json`.
- **Emitted code is compiled one module down.** `test/data/codegen` is its
  own module (`github.com/areqag/gqlc/test/data/codegen`) carrying the driver
  dependencies; `TestGoldenBuild` in `internal/codegen/conformance` runs
  `go build ./...` there. `docs/mutation/README.md` records why `go test -c`
  is blind to emitted code and what the sound screen is.
- **The CI shape this has to fit.** `ci.yml` runs lint / test / tidy /
  codegen-fence / actionlint on every PR and every job is independent
  (`needs:` is forbidden there; the file says why). `codegen-live.yml` is the
  nightly precedent: `schedule` + `workflow_dispatch`, and a `nightly-alert`
  job gated on `always() && github.event_name == 'schedule'` that opens or
  reopens a labelled issue. `.github/scripts/check-cron-freshness.sh` watches
  that nightly from the PR-blocking `tidy` job and exits 1 on an *empty* run
  list — relevant in §12 (WP11). `internal/tools/gatesparity` holds `tidy`'s
  steps and the `gates` recipe's tidy arms to one list. `.golangci.yml`'s
  `run.build-tags` is held to the tags the tree uses by
  `check-golangci-build-tags`, so a new build tag is a config change, not a
  file-local choice (§10 avoids build tags for that reason).
- **Local parity**: `just gates` is the pre-PR gate set; every CI step is a
  recipe; `lint-just` shellchecks recipe bodies; `check-justfile-format` is
  `just --fmt --check --unstable` under the pinned just.

## 2. What was measured, and what was not

Measured 2026-10-27 on this machine (`go1.27.3`, 24 CPUs), in a `git archive`
extract of `2182968b` in a `mktemp -d` directory, with two throwaway targets
of the shape `f.Fuzz(func(t, src string) { _, _ = X.Parse(strings.NewReader(src)) })`
and `-parallel 4`:

| target | seeds | throughput | result |
|---|---|---|---|
| `cypher.New().Parse` | 215 (`test/data/resolver/valid/*.cypher`; 212 distinct) | 41k execs/s at 3 s, 30k at 6 s, 11k at 9 s | **panic at 8.5 s, ~241k execs** (§3) |
| `gql.New().Parse` | 135 (`test/data/schema/gql/corpus/*/*.gql`) | 38–48k execs/s, 18 new interesting inputs in 25 s | clean for 25 s |

The falling Cypher rate is the corpus growing: later inputs are larger and
costlier, which is the expected shape, not a hang. Nothing else in this
document is measured: the GitHub runner's throughput, the per-exec cost of a
`go build` on emitted code, Go's own treatment of a hung worker, and the
resolver's and generators' panic-freedom are all left to the packages that
own them, each of which is told to measure before pinning (§5, §10).

## 3. The finding the probe made

Input, in Go's corpus file format:

```
go test fuzz v1
string("RETURN Count()")
```

Panic: `runtime error: slice bounds out of range [1:0]` at
`internal/query/cypher/expr.go:471`, in `(*listener).classifyAggregateCall` —
`for _, arg := range args[1:]` with `len(args) == 0`. The grammar admits a
zero-argument `oC_FunctionInvocation`; the classifier assumes an operand.
Filed as **gqlc-jjot2.1** (P1, child of the epic). It is the first row of the
triage ledger (§11) and the fix is work package **F0** (§12), which lands
before the Cypher target so that the target's seed corpus can carry the
crasher as a green regression seed.

Two things it demonstrated beyond the bug itself. Placing the crasher file
under `internal/query/cypher/testdata/fuzz/FuzzProbeParse/` made a plain
`go test -run '^FuzzProbeParse$'` red in 0.06 s — the regression-seed
mechanism §11 relies on needs no code. And the find took one minute of
author time against a parser that has 3199 golden tests and two rapid
properties over the same corpus: those sample the corpus, and the corpus
never contained a bare `count()`.

## 4. Engine: native `go test -fuzz`

Chosen, over the alternatives below, because:

1. **No new dependency and no build step.** Fuzz targets are ordinary
   `_test.go` functions; `go test -fuzz` is in the toolchain go.mod already
   names. Nothing new enters `go.sum` for `just vuln` to scan.
2. **Crashers are regression tests by construction.** The engine writes a
   failing input to `<pkg>/testdata/fuzz/<FuzzName>/<hash>`, and every file
   in that directory is run as a seed by plain `go test`. Committing the file
   is the whole of "turn the finding into a committed regression seed", and
   PR CI's `just test` then holds the fix forever at no new cost (§11).
3. **Coverage guidance measured at 10–48k execs/s on the parsers** (§2).
   Byte-level mutation found a parser panic in nine seconds that corpus
   sampling had not found in months.
4. **The limitation is schedulable.** `-fuzz` runs exactly one target in
   exactly one package per invocation. That is a matrix dimension, not a
   problem: one job per target, each with its own six-hour ceiling, is the
   shape the owner's budget wants anyway (§8).

Rejected:

- **`dvyukov/go-fuzz`.** Needs `go-fuzz-build`, an instrumentation step and a
  separate corpus format; its advantage over native fuzzing on Go ≥ 1.18 is
  gone.
- **rapid alone.** Structure-aware and already in the tree, but not
  coverage-guided and not time-budgeted — `rapid.Check` runs a fixed number of
  cases. It stays where it is. Where structure matters (§6), the structure
  comes from a generator *driven by the fuzzer's byte stream*, so the engine's
  coverage feedback still steers it.
- **A grammar-driven generator compiled from the `.g4` files.** `GQL.g4` is
  3797 lines and `Cypher.g4` 805; a faithful generator is a project of its
  own and most of what it would generate is text the resolver refuses at the
  first clause. §6's hand-written generators over the admitted subset reach
  the resolver and the generators, which is where the bugs that matter to a
  user of gqlc live. Revisit if the parser targets go quiet for a month and
  the structured targets do not.

## 5. Fuzz targets

Each target is a `func FuzzXxx(f *testing.F)` in an external test package
(`package xxx_test`) in the package it exercises, with the exact name below —
the name is the matrix key (§8.2) and the `just fuzz <target>` argument (§9),
so it must be unique across the tree. Every target checks **every** property
in its row, not only "does not panic": a property that holds on the corpus
and fails under fuzzing is a finding of the same rank as a crash.

Common rules, held by `internal/fuzzkit` (§6.1) so that no target re-implements
them:

- **Input size cap.** An input over 256 KiB is `t.Skip`ped. Stated limit: a
  defect that needs a larger input to show is not found by this harness.
- **Slow-input detector.** The call under test is wrapped in
  `fuzzkit.Guard(t, 2*time.Second, fn)`; an input that takes longer is
  reported with `t.Fatalf` and so saved as a crasher. A call that never
  returns cannot reach that report, so Guard also arms a watchdog: if the call
  is still running at `fuzzkit.WatchdogFactor` (5) times the limit, the
  watchdog panics on its own goroutine and ends the process, and the engine
  writes the input it was running as a crasher. Measured 2026-10-27 (WP1,
  `docs/mutation/fuzz-liveness-FuzzStream.md`): without the watchdog, a worker
  spinning forever on one input left `just fuzz FuzzStream 45s` green with no
  crasher, because the engine stops workers at `-fuzztime` without reporting
  the input they hold. `-fuzztime` and the job's `timeout-minutes` stay as
  backstops; §10's R3 and R3b rows measure which detector fires.
- **Determinism.** The call is made twice on the same input and the two
  results must be `reflect.DeepEqual` (or byte-equal for `[]File`). Pure
  functions that disagree with themselves are findings.
- **Sentinel classification.** `fuzzkit.ClassifyError(err, sentinels...)`
  returns `sentinel`, `syntax`, `bug` or `plain`. A `bug` (an error whose
  text carries "bug:" — the `codegenBug` and `resolver bug:` convention) is a
  finding, and the marker is tested before the sentinels, so an internal bug
  a fail site wrapped in a sentinel (a gofmt failure under
  `codegen.ErrFormatFailure`) is still a bug. A `plain` error is a finding **unless** the target's row says
  otherwise, in which case it is counted and reported in the step summary
  rather than failed; the first full run (§12, WP10) decides per class
  whether to promote it.

| id | target (package) | input and seeds | properties beyond "no panic" | tier | priority |
|---|---|---|---|---|---|
| T1 | `FuzzCypherParse` (`internal/query/cypher`) | `string`; seeds: resolver valid+invalid `.cypher`, codegen fixtures' `queries.cypher` bodies, a bounded sample of TCK queries via `harvestExecutingQueries` (cap 500, so `just test` stays fast) | err ⇒ zero `Query`; err class ∈ {sentinel, syntax}; ok ⇒ the three `assert*` invariants from `parser_test.go`, `json.Marshal` succeeds, determinism | A (bytes) | P1 — it already found one |
| T2 | `FuzzGQLParse` (`internal/schema/gql`) | `string`; seeds: every `.gql` under `test/data` and `internal/codegen/*/testdata` | err ⇒ zero `Schema`; err class ∈ {sentinel, syntax}; ok ⇒ every edge endpoint names a declared node type, label-set keys unique, `Schema.MarshalJSON` succeeds, determinism | A | P1 |
| T3 | `FuzzGQLLoad` (`internal/schema/gql`) | `(a, b, c string)` written into an `fstest.MapFS` as `a.gql`, `b.gql`, `c.gql`; `Load("a.gql")`; seeds: `COPY OF` chains and a cycle from the loader tests | never hangs on a cycle (Guard); err class ∈ {sentinel, syntax, `fs` errors}; ok ⇒ same invariants as T2 | A | P2 |
| T4 | `FuzzQueryfileParse` (`internal/queryfile`) | `string`; seeds: `internal/queryfile/testdata`, codegen fixtures' `queries.cypher` | err ⇒ nil slice; err class = sentinel; ok ⇒ every `Name` matches `^[A-Z][A-Za-z0-9]*$`, every `Cardinality` is a declared value, and **round-trip**: the annotation lines plus each `SourceText` re-concatenate to the input (the C0 spec's "exact text between this annotation and the next" claim) | A | P2 |
| T5 | `FuzzConfigDecode` (`internal/config`) and `FuzzProcsigDecode` (`internal/procsig`) | `string`; seeds: the canonical fixtures | err class = sentinel; ok ⇒ every enum field holds a declared value; `procsig.NewRegistry` on the decoded signatures agrees with `Decode`; determinism | A | P3 |
| T6 | `FuzzPropertyType` (`internal/graph`) | `[]byte` → `fuzzkit.Stream` → a random tree of scalar / `ListOf` / `RecordOf` / `UnionOf` | `Kind()`, `Elem()`, `Fields()`, `Members()` read back what was built; two constructions of the same tree with members or fields in a different order are `==` (canonicalisation is what makes `==` mean type equality); `KindScalar` never has a `<` | B (structured) | P2 |
| T7 | `FuzzResolve` (`internal/resolver`) | `[]byte` → `gen.Schema` + `gen.Query` (§6.2), *or* a corpus schema plus fuzzed query text — the stream's first byte picks; seeds: `sweep.manifest.tsv` pairs | parse errors are skipped (T1 owns them); resolve err class ∈ {sentinel} or the documented CALL-registry `plain` arm (counted, not failed); ok ⇒ `ValidatedQuery` well-formed: every column has a non-nil `ResolvedType`, column names unique, `MarshalJSON` succeeds, determinism | B | P1 |
| T8 | `FuzzGenerate` (`internal/codegen/conformance`) | as T7, then through resolver, then **all three** backends from the composed registry (neo4j v5, v6, AGE) | err class ∈ {sentinel (codegen or age)} — a `bug` class is the `codegenBug` recover firing and is a finding; ok ⇒ paths sorted and unique, every file is `gofmt`-clean (`format.Source(b)` equals `b`) and `go/parser`-clean, determinism; **cross-backend**: v5 and v6 both succeed or both fail with the same sentinel and the same path set — pinned only after the package measures it on the 129 valid fixtures; AGE-vs-neo4j agreement at schema admission is measured the same way and pinned only where it holds (ADR 0027 says it does not hold for multi-label schemas) | B | P1 |
| T9 | `FuzzGeneratedBuilds` (`internal/codegen/conformance`) | as T8 for inputs all backends accept; the emitted files are written under a `TempDir` inside `test/data/codegen` and `go build ./...` is run there (the `TestGoldenBuild` recipe) | build succeeds. Seeds: three fixtures only, so `just test` pays three warm builds | B, slow (seconds per exec — a sampler, not a mutator; the same `-fuzztime` applies and it simply runs fewer execs) | P1 |
| T10 | `FuzzPipelineRun` (`internal/cli/pipeline`) | `[]byte` → a config, a schema and a query file written into a `TempDir`; `Run(cfg, backends)` | the package-doc invariant: `Targets != nil` iff `len(Diagnostics)==0 && err==nil`; `Warnings` empty when `err != nil`; one `TargetResult` per declared target, each `Files` non-nil and sorted; determinism | B | P2 |
| T0 | `FuzzStream` (`internal/fuzzkit`) | `[]byte` | `Stream` never panics on any bytes, exhaustion is deterministic (zeros), `Pick` on an empty slice is a declared refusal not a panic | — | ships with WP1 so the workflow has a target on day one |

Not targeted, and why: the CLI's cobra/huh surface (`internal/cli`) is
interactive plumbing over `pipeline.Run`; the `tools/` packages are
repository guards, not user-input readers; the generated ANTLR packages are
reached through T1–T3.

## 6. The shared kit and the structured generators

### 6.1 `internal/fuzzkit` (WP1)

A small non-test package imported only by `_test.go` files. Its exported
surface is fixed here so that target packages can be written in parallel:

```go
package fuzzkit

const MaxInput = 256 << 10

// Skip calls t.Skip when len(b) > MaxInput.
func Skip(t *testing.T, b []byte)

// Guard runs fn on the calling goroutine and t.Fatalf's, naming the elapsed
// time, when it takes longer than limit. It does not interrupt fn; if fn is
// still running at WatchdogFactor × limit, a watchdog panics on its own
// goroutine, ending the process so the engine records the input as a crasher.
const WatchdogFactor = 5
func Guard(t *testing.T, limit time.Duration, fn func())

type ErrClass int
const (ClassNil ErrClass = iota; ClassSentinel; ClassSyntax; ClassBug; ClassPlain)

// ClassifyError: nil → ClassNil; text containing "bug:" → ClassBug; errors.Is
// any of sentinels → ClassSentinel; isSyntax(err) → ClassSyntax; else ClassPlain.
// isSyntax is a per-package predicate the caller passes, because what a syntax
// error looks like is each parser's own (both parsers route ANTLR errors
// through a listener; WP2/WP4 pin the shape).
func ClassifyError(err error, isSyntax func(error) bool, sentinels ...error) ErrClass

// Stream reads fuzz bytes as a sequence of small decisions. Exhaustion
// returns zero values forever, so a generator is total.
type Stream struct{ /* unexported */ }
func NewStream(b []byte) *Stream
func (s *Stream) Byte() byte
func (s *Stream) Int(n int) int            // 0 <= r < n; n <= 0 → 0
func (s *Stream) Bool() bool
func (s *Stream) Pick(xs []string) string   // "" on empty xs
func (s *Stream) Ident(alphabet string, maxLen int) string

// SeedFiles adds every file matching the globs (relative to the test's cwd)
// to f with f.Add(string(contents)); it fails f if no file matched, so a
// moved corpus directory cannot silently seed nothing. (Implemented per glob:
// any one glob matching no file fails f.)
func SeedFiles(f *testing.F, globs ...string)

// SeedFilesBytes is SeedFiles for a []byte target (T6–T8, T10): f.Add(contents).
func SeedFilesBytes(f *testing.F, globs ...string)
```

### 6.2 `internal/fuzzkit/gen` (WP3)

Byte-steered generators for the admitted subset, each total and
deterministic in its `*Stream`:

- `gen.Schema(s *Stream) (text string, v Vocab)`: 1–6 node types with label
  sets drawn from a small alphabet (`Person`, `Post`, `Tag`, …), 0–6
  properties each over the full type lattice (`INT8..INT64`, `UINT*`,
  `FLOAT*`, `STRING`, `BOOL`, temporals, `UUID`, `LIST<…>`, `RECORD<…>`,
  `UNION<…>`, with `NOT NULL` flags), 0–6 edge types between declared node
  types with the four arc spellings, multi-label node types and shared edge
  labels at a low rate (they exercise ADR 0027 and the `Name` requirement).
  `Vocab` lists labels, relationship types and `(label, property, type)`
  triples for `Query` to draw from.
- `gen.Query(s *Stream, v Vocab) string`: a chain of parts — `MATCH` /
  `OPTIONAL MATCH` patterns over `v` (0–3 hops, variable-length at a low
  rate), `WHERE` with `$param` comparisons against typed properties, `WITH`
  (aliases, `DISTINCT`, aggregates, `ORDER BY`, `SKIP`/`LIMIT`), `UNWIND`,
  `RETURN` with aliased refs, expressions and aggregates, and a write arm
  (`CREATE` / `SET` / `DELETE`) at a low rate; `UNION` between branches at a
  low rate. Every identifier it writes comes from `v`, so the resolver's
  unknown-label and unknown-property refusals are reached deliberately, not
  on every input.
- `gen.QueryFile(s *Stream, v Vocab) string`: 1–4 annotated queries
  (`// name: Ident :one|:many|:exec|:iter`).

Acceptance for WP3 is a measurement, not a feeling: over 10 000 streams from
`rand`, the share of generated (schema, query) pairs that parse *and*
resolve must be reported in the package doc, and must be above 30 %, or the
generator is tuned until it is. Below that the resolver and codegen targets
spend their budget on refusals the parser targets already cover.

## 7. Why structure-aware generation, and where not

Byte mutation over seeds (tier A) is enough for the two text parsers: the
corpus is large, the lexer and parser are where byte-level damage lands, and
§3 is the evidence. It is not enough for the resolver and the generators: a
random edit to a valid query is refused at parse or at the first unknown
name, so almost no exec reaches `Resolve`, let alone `Generate`. Tier B's
generators spend the fuzzer's bytes on decisions the resolver *admits*, so
coverage feedback steers the schema and query shapes rather than the
spelling. T7 keeps the corpus-text arm as well, chosen by the first byte, so
the two approaches share one budget and the engine decides which pays.

## 8. The workflow: `.github/workflows/fuzz.yml`

### 8.1 Triggers and what it must not do

```yaml
on:
  schedule:
    - cron: "37 0 * * *"     # 00:37 UTC; codegen-live's nightly is 04:17 and on its own runner
  workflow_dispatch:
    inputs:
      fuzztime: { default: "4h30m" }   # any Go duration; "2m" is the smoke run
      targets:  { default: "" }        # comma-separated FuzzNames; empty = every discovered target
      parallel: { default: "" }        # -parallel; empty = GOMAXPROCS
permissions: { contents: read }
concurrency: { group: fuzz, cancel-in-progress: false }
```

No `pull_request`, no `merge_group`, no `push`. The job ids here must never
be added to master's required contexts (ADR 0023 governs that list): a
context that only reports from a schedule leaves every PR pending. Nothing
in `.githooks/` calls any recipe from §9.

### 8.2 Discovery

A first job, `discover`, checks out, provisions Go through
`./.github/actions/setup-go`, and runs `just fuzz-list`, which derives the
target list from the tree (`go test -list '^Fuzz' ./...`, parsed into
`name<TAB>package` lines) and prints it as a JSON matrix. The output shape
to parse, read 2026-10-27 under go1.27.3: each matching name on its own
line, followed by `ok  \t<pkg>\t<time>` for that package; a package with no
match prints only its `ok` line; a package with no test files prints
`?   \t<pkg>\t[no test files]`. A name therefore belongs to the next `ok`
line below it. It **refuses an
empty list** — a workflow that fuzzes nothing and goes green is the shape
this repository keeps finding — and refuses a duplicate name. The
`workflow_dispatch` `targets` input filters it, and asking for a name the
tree does not have is a refusal, not an empty matrix. The list is discovered
rather than maintained because a maintained list is one more thing a new
target can be left out of silently (the same argument `internal/tools/modscope`
makes for the module set).

### 8.3 The fuzz job and its budget

```yaml
fuzz:
  needs: discover
  strategy:
    fail-fast: false
    matrix: { include: ${{ fromJSON(needs.discover.outputs.matrix) }} }
  timeout-minutes: 330
  steps:
    - checkout; setup-go (cache-key-suffix: -${{ hashFiles('test/data/codegen/go.sum') }}); setup-just
    - actions/cache/restore  (§8.4)
    - run: just fuzz-corpus-import "${{ matrix.name }}"
    - run: just fuzz "${{ matrix.name }}" "${{ inputs.fuzztime || '4h30m' }}" "${{ inputs.parallel }}"
    - run: just fuzz-corpus-export "${{ matrix.name }}"        # if: always()
    - actions/cache/save                                        # if: always()
    - upload-artifact fuzz-crasher-<name>: new files under **/testdata/fuzz/** + the log   # if: failure()
    - upload-artifact fuzz-result-<name>: one line "<name> <result> <last fuzz: line>"     # if: always()
```

**Budget.** `-fuzztime 4h30m` under `timeout-minutes: 330` (5 h 30 m).
Wall time per leg is then about 4 h 45 m — the owner's "maybe 5 hrs" — and
the 45 minutes between the deadline and the kill are for the restore, the
baseline pass over the seeds, the corpus export, the cache save and the
artifact upload, plus one over-running exec on T9, whose execs are whole
`go build`s and whose deadline is only checked between them. A job killed by
GitHub's 360-minute ceiling loses its `always()` steps, so the cache save
and the crasher upload are exactly what the headroom protects. Not 5 h of
fuzzing because that headroom would be 30 minutes and T9 could eat it; not
less because the owner asked for hours and the corpus carries forward, so a
long night continues the previous one rather than restarting it (§8.4).

The same budget for every target, deliberately: the matrix has one
dimension and `workflow_dispatch` overrides one number. Per-target budgets
can come later with a measured reason.

**Parallelism** is across legs (one runner each, GitHub's concurrency limit
for a public repository permitting — 10 or 11 legs at 4 h 45 m is ~50
runner-hours a night on free standard runners) and within a leg through
`-parallel`, defaulting to the runner's core count.

### 8.4 Corpus persistence

Go keeps the grown corpus at `$(go env GOCACHE)/fuzz/<module>/<pkg>/<Fuzz>`.
`setup-go` pins `GOCACHE=~/.cache/go-build` and caches that whole directory
under a per-`github.job` rolling key — and for a matrix, every leg shares one
`github.job`, so legs would race to save one key and the fastest leg's corpus
would win (the same race `setup-go`'s comment describes between jobs). So the
corpus does not ride that cache. Instead:

- `just fuzz-corpus-export <name>` copies the target's corpus directory to
  `.fuzz-corpus/<name>` (gitignored); `just fuzz-corpus-import <name>` copies
  it back before fuzzing.
- `actions/cache/restore` with `key: fuzz-corpus-<name>-${{ github.run_id }}`
  and `restore-keys: fuzz-corpus-<name>-`, and an explicit
  `actions/cache/save` under `if: always()` with the same primary key. The
  rolling key is for the same reason `setup-go` rolls its key — `actions/cache`
  saves only on a primary miss — and the explicit save is so that a night
  that *found* something still carries its corpus forward; a failing step
  must not cost the inputs that led to it.
- The first night is cold by construction, and so is the first night after
  any change to the export path (memory: a cache `path:` change costs one
  cold run per scope). Judge steady state on the second run.
- The corpus is the engine's minimised set, not every input; measured sizes
  are for WP10 to record. If a target's corpus ever exceeds 100 MB, WP10
  prunes it with `go test -run=^$ -fuzz=X -fuzzminimizetime=...` rather than
  letting the cache grow without bound.

### 8.5 Reporting: loud, and impossible to miss

A leg that finds anything **fails**: `go test -fuzz` exits non-zero on a
crasher or a slow input, and `just fuzz` passes that status through. Then:

1. The crasher file(s) and the full log are an artifact named
   `fuzz-crasher-<name>` (14-day retention is enough; the issue carries the
   link).
2. Every leg, pass or fail, writes `fuzz-result-<name>`.
3. `fuzz-alert`, `needs: [discover, fuzz]`, runs under
   `always() && github.event_name == 'schedule' && (contains(needs.*.result, 'failure') || contains(needs.*.result, 'cancelled'))`,
   downloads every `fuzz-result-*`, and opens or reopens the issue labelled
   `fuzz-red` titled "nightly fuzz is red", commenting the per-target lines
   and the run URL. `cancelled` is in the condition because a leg killed by
   `timeout-minutes` is a hang candidate and silence is the failure mode.
   Scheduled runs only, for `codegen-live.yml`'s reason: a person who
   dispatched a run is watching it.
4. The step summary carries the last `fuzz: elapsed …` line per leg (execs,
   rate, corpus size) so a green night is also legible.

A discovery refusal (§8.2) fails `discover`, which fails `fuzz` by `needs`
and so reaches `fuzz-alert` as well.

## 9. Local parity: the recipes

All in the `justfile`, bodies shellcheck-clean under `just lint-just`, file
formatted under the pinned just:

| recipe | does |
|---|---|
| `fuzz-list` | derive `name<TAB>pkg` for every `Fuzz*` in the tree; `--json` prints the matrix; refuses empty and duplicates |
| `fuzz name fuzztime="1m" parallel=""` | resolve `name` through `fuzz-list`; `go test -run '^$' -fuzz "^<name>$" -fuzztime <d> [-parallel n] <pkg>`; exit status passed through; prints the corpus path at the end |
| `fuzz-smoke fuzztime="20s"` | every target, sequentially, for `fuzztime` each — the pre-PR check for a new target |
| `fuzz-seeds` | `go test -run '^Fuzz' <every fuzz pkg>` — the seed-only pass PR CI already performs through `just test`, isolated so a slow seed set is visible |
| `fuzz-repro name hash` | `go test -run "^<name>$/^<hash>$" <pkg>` — replay one crasher |
| `fuzz-corpus-path name` / `fuzz-corpus-export name` / `fuzz-corpus-import name` | §8.4 |
| `fuzz-liveness [name]` | run `.github/scripts/fuzz-liveness/<name>.rows` for one or every target (§10) |

`just gates` is **not** extended: none of this is a required context, and
`gates` is the required set. `fuzz-smoke` is what a target's author runs
before opening the PR, and the PR body says so.

## 10. Liveness: proving each target can find something

A fuzz target that never fails is indistinguishable from one that cannot.
Every target ships with rows, in `.github/scripts/fuzz-liveness/<Fuzz>.rows`
(a bash script in the `.rows` convention of `.github/scripts/*.rows`; `ci.yml`'s
`just lint-hooks .github/scripts` walks subdirectories with `find -type f`,
read 2026-10-27, so each file needs the `#!/usr/bin/env bash` shebang that
recipe recognises or it fails the tidy job), recorded in
`docs/mutation/fuzz-liveness-<Fuzz>.md` in the
`docs/mutation` table shape, with the diff of every mutation published
beside the table.

Protocol per row, run by `just fuzz-liveness`:

1. `git archive HEAD | tar -x` into `mktemp -d` (never the tree, never a
   fixed path; `trap` removes it). Assert `$PWD` is the copy before any
   mutation — a failed `cd` has run mutations against a real checkout before.
2. Apply one `sed` mutation to the copy. Record `md5` before and after; the
   row is void if they are equal. Restore is `cp` from the pristine archive,
   not `git checkout`, which is a no-op in an extract.
3. Run the declared command with the declared budget. Verdict: **KILLED** if
   the harness reports the planted defect (red exit, crasher written, stack
   naming the planted line); **SURVIVED** otherwise. SURVIVED is the finding.
4. Negative control: the same command on the unmutated copy must be green.

Rows every target carries:

- **R1 seed-reach.** A `panic("planted")` on a site the seed corpus reaches
  (for T1, inside `build()`). Command: `just fuzz-seeds`-equivalent for the
  package. Must be KILLED: proves the seeds run and a red is reported.
- **R2 mutation-reach.** A panic on a condition **no seed meets but a short
  mutation of a seed does** — chosen by the author per target, published in
  the diff, and checked against the seeds first (the row is void if a seed
  already trips it). For T1 the example is exactly §3's class: a condition on
  an argument count. Command: `just fuzz <name> 5m`. Must be KILLED within
  the budget; the row records time-to-kill. This is the row that says the
  fuzzer, not the corpus, finds things.
- **R3 slow call.** A `time.Sleep` on the R2 condition, long enough to pass
  the target's Guard limit and short of its watchdog (5× the limit). Command:
  `just fuzz <name> 5m`. Must be KILLED by Guard's `Fatalf`, with a crasher
  written, and the row records *which* detector fired.
- **R3b hang.** A call that never returns (`for {}`) on the R2 condition.
  Command: `just fuzz <name> 5m`. Must be KILLED with a crasher written: that
  is Guard's watchdog ending the worker. Without the watchdog this row was
  measured SURVIVED (green, no crasher) — the case §5's watchdog exists for.
- **R4 property.** For a target with a property beyond panic-freedom, a
  mutation that breaks the property without panicking (for T4: drop one byte
  from `SourceText`; for T8: emit a file out of order). Must be KILLED by the
  property, named in the row.

Why `sed` in a scratch copy and not a planted bug behind a build tag: a tag
is derived by `modscope` and must then be declared in `.golangci.yml`, read by
`just vuln` and `check-goldens-unused`, and it leaves a planted panic in the
shipped tree forever. A mutation row leaves nothing and is re-runnable by
anyone from its diff.

## 11. The triage loop

Runs from the first full night (WP10) until the stop condition.

1. **Read the issue, not the inbox.** The `fuzz-red` issue lists the red
   legs; each has a `fuzz-crasher-<name>` artifact.
2. **One bead per distinct finding.** Dedup key, in order: for a panic, the
   first in-repo frame (`file:line`) of the stack — two inputs with the same
   frame are one bead; for a property violation, the property name and the
   first differing field path; for a slow input, the first in-repo frame of a
   `SIGQUIT` dump taken under `just fuzz-repro` with `-timeout`. Two beads
   with the same key after a fix lands are re-keyed, not merged — a second
   stack behind the first is a second bug. Beads are `-t bug`, `--parent
   gqlc-jjot2`, label `fuzz` plus the target's package, P1 for a panic or a
   hang, P2 for a property, P3 for an unclassified `plain` error (§5).
3. **The reproducer is the seed.** `gh run download -n fuzz-crasher-<name>`;
   copy the file into `<pkg>/testdata/fuzz/<Fuzz>/` on the fix branch
   **renamed to the bead id** (`gqlc-xxxx`), so the seed names its bead
   forever; minimise by hand only if the input is unreadable (`-fuzzminimizetime`
   already ran). Commit it in the fix PR: red before the fix, green after,
   held by `just test` from then on. The fix PR body carries `Bead:
   <id>`; the bead's GH mirror, if minted, takes `Closes #N`.
4. **A fix is reviewed like any other PR**, and its reviewer re-runs
   `just fuzz-repro` and `just fuzz <name> 2m` on the fixed tree.
5. **An accepted shape is recorded, not silenced.** A `plain` error class the
   owner rules acceptable (the documented CALL-registry arm is the candidate)
   is added to that target's allowlist with the ruling's bead id beside it;
   an allowlist entry without a bead id is refused by the kit.
6. **Stop condition.** The epic closes when three consecutive **scheduled**
   nights are green on every discovered target with the corpus carried
   forward (the restore step's log says "Cache restored" on each), and no
   `fuzz-red` issue is open. The workflow does not stop; the loop goes back
   to step 1 on the next red night, outside the epic.

## 12. Work packages

Each is one agent, one worktree, one PR; the PR body names the bead with
`Bead:` (a `Refs:` line if it advances the epic without resolving it). No two
packages in the same parallel group own the same file. Target names, kit
signatures and recipe names are fixed above so the packages can be written
against this document without waiting on each other's code; a package that
finds the fixed surface unworkable says so in its PR and the plan moves, not
the package.

| id | title | owns (and nothing else) | depends on | group |
|---|---|---|---|---|
| **F0** | fix gqlc-jjot2.1: zero-argument aggregate call panics | `internal/query/cypher/expr.go`; new `internal/query/cypher/aggregate_arity_test.go` | — | A |
| **WP1** | fuzzkit, the recipes and discovery | new `internal/fuzzkit/*.go` (incl. `FuzzStream` in `stream_test.go` and `testdata/fuzz/FuzzStream/`); `justfile` (the §9 recipes, one new section); `.gitignore` (`.fuzz-corpus/`); new `.github/scripts/fuzz-liveness/` (driver + `FuzzStream.rows`); new `docs/mutation/fuzz-liveness-FuzzStream.md` | — | A |
| **WP3** | structured generators | new `internal/fuzzkit/gen/*.go` and their tests | WP1 | B |
| **WP2** | T1 `FuzzCypherParse` | new `internal/query/cypher/fuzz_test.go`; new `internal/query/cypher/testdata/fuzz/FuzzCypherParse/` (includes F0's crasher, named `gqlc-jjot2.1`); `.github/scripts/fuzz-liveness/FuzzCypherParse.rows`; `docs/mutation/fuzz-liveness-FuzzCypherParse.md` | WP1, F0 | B |
| **WP4** | T2+T3 `FuzzGQLParse`, `FuzzGQLLoad` | new `internal/schema/gql/fuzz_test.go` and `testdata/fuzz/{FuzzGQLParse,FuzzGQLLoad}/`; their `.rows` and `docs/mutation` files | WP1 | B |
| **WP5** | T4+T5+T6 small front ends | new `fuzz_test.go` (and `testdata/fuzz/`) in `internal/queryfile`, `internal/config`, `internal/procsig`, `internal/graph`; their `.rows` and `docs/mutation` files | WP1 | B |
| **WP9** | the workflow | new `.github/workflows/fuzz.yml`; `actionlint` must pass; a dispatch with `fuzztime=2m` on the PR branch is the acceptance run (the `schedule` path cannot be tried before merge; the first scheduled night is WP10's first row) | WP1 | B |
| **WP6** | T7 `FuzzResolve` | new `internal/resolver/fuzz_test.go` + `testdata/fuzz/FuzzResolve/`; `.rows`, `docs/mutation` | WP3 | C |
| **WP7** | T8+T9 `FuzzGenerate`, `FuzzGeneratedBuilds` | new `internal/codegen/conformance/fuzz_test.go` + `testdata/fuzz/…`; `.rows`, `docs/mutation`; the measured cross-backend table goes in the `docs/mutation` file | WP3 | C |
| **WP8** | T10 `FuzzPipelineRun` | new `internal/cli/pipeline/fuzz_test.go` + `testdata/fuzz/…`; `.rows`, `docs/mutation` | WP3 | C |
| **WP10** | first full run and triage | dispatch a full-budget run; read the first scheduled night; file one bead per finding under the epic (§11); record corpus sizes, per-leg throughput and the `plain`-class census in a new `docs/specs/nightly-fuzzing-first-run.md`; propose per-target allowlists for the owner's ruling. Owns no code; every fix is its own package, filed by this one | WP9 and at least WP2 merged; the rest join as they land | D |
| **WP11** | enrol the nightly in cron-freshness | `ci.yml` (`check-cron-freshness.sh fuzz.yml 14` beside the codegen-live line) and the matching `internal/tools/gatesparity` entry | ≥ 14 days of scheduled history for `fuzz.yml` — the script exits 1 on an empty run list, so landing this with WP9 would red every PR until the first night | D (time-gated) |

**Parallel groups:** A = {F0, WP1}; B = {WP2, WP3, WP4, WP5, WP9}, all
after WP1 (WP2 also after F0); C = {WP6, WP7, WP8} after WP3; D = {WP10,
WP11} as gated above. Fix packages filed by WP10 are each their own group-D
member and may run concurrently when they own different files.

### Acceptance, common to every target package (WP2, WP4–WP8)

- `just fuzz-seeds` green, and the package's seed pass adds under 5 s to
  `go test` on this machine (the number goes in the PR body).
- `just fuzz <name> 2m` green on the unmutated tree, **and** every liveness
  row in §10 recorded with its verdict and diff — an all-KILLED table whose
  R2 row killed in under a second is re-checked against the seeds, because
  that is what a seed already tripping the condition looks like.
- `just gates` green; `just lint` sees the new files (external test packages
  are in the default build; no tag).
- Properties measured before pinned: any cross-backend or round-trip claim
  the target asserts is first run over the relevant committed corpus, and the
  PR body states the count it held on.
- The PR body names the first `fuzz-list` line the target adds, so the
  reviewer can see it is discoverable.

### Acceptance for WP1

- `just fuzz-list` prints exactly one line, `FuzzStream`, and `--json` is
  valid `fromJSON` input; emptied of targets (a scratch copy with the test
  deleted) it exits non-zero.
- `just fuzz FuzzStream 30s`, `just fuzz-smoke 5s`, `just fuzz-seeds`,
  `just fuzz-corpus-export FuzzStream` then `-import`, `just fuzz-liveness`
  all green; `just lint-just`, `just check-justfile-format`, `just lint`
  green.
- `fuzzkit.Guard`'s own unit test plants a `time.Sleep` and sees the
  `Fatalf`, and plants a call that never returns and sees the watchdog;
  `ClassifyError`'s test covers all five classes.

### Acceptance for WP9

- `just actionlint` green.
- One `workflow_dispatch` run on the PR branch with `fuzztime=2m` and
  `targets=FuzzStream`, linked in the PR body, showing: discovery, a cache
  miss then a save, the result artifact, and the step summary line. A second
  dispatch on the same branch showing the cache **restored** — the second run
  is the one that proves §8.4, the first is cold by construction.
- A third dispatch against a scratch branch carrying a deliberately failing
  seed (an R1 row applied and committed on that branch only), showing the
  leg red, the `fuzz-crasher-*` artifact present, and `fuzz-alert` **not**
  firing on dispatch; the alert's own body text is reviewed by reading, since
  `schedule` cannot be dispatched. Delete the scratch branch afterwards.

## 13. Limits and falsifiers

- **This document does not claim any target is panic-free.** T1 is known not
  to be (§3). The others are unmeasured until their packages run.
- **The budget figure (4 h 30 m) is a chosen ceiling, not a measured
  optimum.** What would move it: WP10's record showing the corpus stops
  growing (`new interesting` flat) well inside the budget on every target
  for a week — then the budget is waste and should shrink; or a target whose
  `always()` steps are being killed at the ceiling — then it is too long.
- **The throughput figures in §2 are this machine's**, with 4 workers; a
  GitHub runner has 4 vCPUs of unknown speed. WP10 records the real rates.
- **Discovery through `go test -list` compiles every test package** — on the
  order of a minute. If that ever dominates a smoke run, the list can be
  cached per commit; it is not cached now because a stale list is the defect
  discovery exists to prevent.
- **A finding the harness cannot see:** anything behind a `t.Skip` (inputs
  over 256 KiB), anything in the CLI above `pipeline.Run`, and any
  cross-backend disagreement a package measured on the corpus and chose not
  to pin. Each package's `docs/mutation` file lists what it declined to pin
  and why.
- **What falsifies §4's engine choice:** a month of nightly runs in which
  tier A targets find nothing and the §6.2 generators' admitted share stays
  under 30 % despite tuning — then grammar-driven generation (rejected in §4)
  is the next thing to try, and this document is amended, not replaced.
