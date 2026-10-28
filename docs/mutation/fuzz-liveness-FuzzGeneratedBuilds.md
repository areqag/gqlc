# Liveness rows: FuzzGeneratedBuilds

FuzzGeneratedBuilds (`internal/codegen/conformance/fuzz_test.go`) is target T9
of [the nightly fuzzing plan](../specs/nightly-fuzzing-plan.md): for an input
every registered backend admits, the emitted packages are written under a
fresh `_fuzzbuild-*` directory inside the `test/data/codegen` module and
compiled there with `go build`, as `TestGoldenBuild` compiles the goldens.
FuzzGenerate (T8, [its rows](fuzz-liveness-FuzzGenerate.md)) checks
everything about an emission short of compiling it; this target is that step.
The rows are run by

    just fuzz-liveness FuzzGeneratedBuilds

## The run

Measured 2026-10-28 at `48529d7e`, go1.27.3, 24 CPUs shared with other
fuzzing sessions, `-parallel 2`, budget `LIVENESS_BUDGET=5m`, every row from an
empty grown corpus. The md5 columns pin movement, not identity.

| row | kind | mutation | md5 before | md5 after | command | verdict | detector | wall |
|---|---|---|---|---|---|---|---|---|
| C1 | control | none | - | - | `go test -count=1 -run '^FuzzGeneratedBuilds$' ./internal/codegen/conformance` | GREEN | - | 7 s |
| C2 | control | none | - | - | `just fuzz FuzzGeneratedBuilds 5m 2` | GREEN | - | 324 s |
| R1 | seed-reach | `Prepare` panics | cbf78c96 | 6926ec02 | C1's command | **KILLED** | panic, no crasher (seed pass) | 1 s |
| R2 | mutation-reach | `Prepare` panics on a six-node-type schema | cbf78c96 | a2aabd86 | `just fuzz FuzzGeneratedBuilds 5m 2` | **KILLED** | panic, 1 crasher written | 19 s |
| R3 | slow call | `Prepare` sleeps 11 s on R2's condition, once per process | cbf78c96 | 3db95059 | `just fuzz FuzzGeneratedBuilds 5m 2` | **KILLED** | `fuzzkit.Guard` Fatalf (11.0 s against 10 s), 1 crasher written | 53 s |
| R3b | hang | `Prepare` never returns on R2's condition | cbf78c96 | 5c423b39 | `just fuzz FuzzGeneratedBuilds 5m 2` | **KILLED** | engine reports the worker hung (Guard's watchdog at 50 s), 1 crasher written | 59 s |
| R4 | property | neo4j's `WithTx` builds its `txDB` with an undeclared field | 611049e1 | e4d537ef | `just fuzz FuzzGeneratedBuilds 5m 2` | **KILLED** | property `builds` (seed pass) | 3 s |

What each row means:

- **R2**: the three seeds are text-arm fixtures with at most five node types;
  the driver ran the mutant's seeds green first. The engine reached six node
  types by flipping the first byte to the generated arm, in 19 s here and
  13 s on an earlier run.
- **R3 sleeps once per process, and that is the second version.** The first
  slept 6 s on every six-node `Prepare` (12 to 18 s an exec, past the 10 s
  limit). It was measured SURVIVED twice: green, no crasher, 23 execs in
  5 minutes and 11 in 3, although a six-node seed replayed under the same
  mutant failed Guard in 12 s. Not measured, read from
  `internal/fuzz/worker.go`: the engine minimises every new interesting input,
  its candidates rewrite a stream's leading bytes and so often land on six
  node types, and every one paid the sleep, so the budget went on
  minimisation. With one sleep per process the exec that pays it is reported
  and nothing slow is left to replay. **What this leaves open:** a real slow
  input on this target that the minimiser keeps re-hitting costs the night
  throughput rather than going unreported for good; nothing measured here
  says how much.
- **R4 is the target's reason to exist.** The mutant's emission parses and is
  gofmt-clean, so FuzzGenerate passes it; only the compiler refuses it.
  Every seed emits `WithTx`, so
  the seed pass reports it.

## buildLimit, and what an exec costs

`buildLimit` is 10 s over the whole exec — front end, three emissions and the
`go build` — with the watchdog at 50 s. Measured 2026-10-28 on this machine:

- a `go build` of three freshly written emitted packages, warm cache: 0.04 to
  0.07 s (seven runs); one outlier of 7.2 s wall with 0.2 s CPU, under a load
  average near 60, which is waiting rather than compiling;
- the same build on an empty `GOCACHE`: 4.8 s wall, 28 s CPU, almost all of
  it the driver dependencies (pgx, neo4j v5 and v6). `warmBuild` compiles the
  three seed fixtures' committed goldens before `f.Fuzz`, so no guarded exec
  pays for them; warm, that costs 0.2 s.

10 s is two orders over the warm cost and leaves room for the outlier; a
GitHub runner's 4 vCPUs make the warm compile slower, not the dependency
build, which `warmBuild` has already paid. If WP10's first nights show
`slow input` crashers on this target whose replay is fast, the limit is too
tight for the runner, not the input too slow.

Seed pass: 0.28 to 0.31 s for the three seeds, warm (`go test -run
'^FuzzGeneratedBuilds$'`), which is what `just test` pays.

Throughput under `-fuzz`: 10,733 execs in a 5-minute `-parallel 2` run at
`27c4aa0e` (before the two codegen fixes FuzzGenerate's page records), 10 new
interesting inputs, green; C2 above is the same budget at `48529d7e`, green. Most execs are refused by the front end or by
a backend and never reach `go build`; of the 10 grown-corpus entries, 2 reach
the build.

## The diffs

Generated by `driver.sh` in the run above. The `sed` scripts are in
`FuzzGeneratedBuilds.rows`.

R1:

```diff
@@ -383,6 +383,7 @@
 //
 // packageName overrides the Schema.Name derivation when non-empty.
 func Prepare(in Input, tm TypeMap, packageName string) (Prepared, error) {
+	panic("planted: R1")
 	pkg, err := emittedPackage(in.Schema.Name, packageName)
 	if err != nil {
 		return Prepared{}, err
```

R2:

```diff
@@ -383,6 +383,9 @@
 //
 // packageName overrides the Schema.Name derivation when non-empty.
 func Prepare(in Input, tm TypeMap, packageName string) (Prepared, error) {
+	if len(in.Schema.Nodes) == 6 {
+		panic("planted: R2")
+	}
 	pkg, err := emittedPackage(in.Schema.Name, packageName)
 	if err != nil {
 		return Prepared{}, err
```

R3:

```diff
@@ -6,6 +6,8 @@
 	"path/filepath"
 	"slices"
 	"strings"
+	"sync"
+	"time"
 	"unicode/utf8"
 
 	"github.com/areqag/gqlc/internal/graph"
@@ -373,6 +375,8 @@
 	Width graph.PropertyType
 }
 
+var plantedSlow sync.Once
+
 // Prepare runs every shared phase over one batch and returns the
 // backend-neutral derivation its render layer consumes: package-
 // identifier derivation, Phase Z (schema-shape admission + entity
@@ -383,6 +387,9 @@
 //
 // packageName overrides the Schema.Name derivation when non-empty.
 func Prepare(in Input, tm TypeMap, packageName string) (Prepared, error) {
+	if len(in.Schema.Nodes) == 6 {
+		plantedSlow.Do(func() { time.Sleep(11 * time.Second) })
+	}
 	pkg, err := emittedPackage(in.Schema.Name, packageName)
 	if err != nil {
 		return Prepared{}, err
```

R3b:

```diff
@@ -383,6 +383,10 @@
 //
 // packageName overrides the Schema.Name derivation when non-empty.
 func Prepare(in Input, tm TypeMap, packageName string) (Prepared, error) {
+	if len(in.Schema.Nodes) == 6 {
+		for {
+		}
+	}
 	pkg, err := emittedPackage(in.Schema.Name, packageName)
 	if err != nil {
 		return Prepared{}, err
```

R4:

```diff
@@ -180,7 +180,7 @@
 }
 
 func (q *Queries) WithTx(tx neo4j.ManagedTransaction) *Queries {
-	return &Queries{queries: queries{db: txDB{tx: tx}}}
+	return &Queries{queries: queries{db: txDB{tx: tx, planted: 1}}}
 }
 
 // driverOrTx is the unexported run indirection: every generated query
```
