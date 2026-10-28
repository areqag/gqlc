# Liveness rows: FuzzPipelineRun

FuzzPipelineRun (`internal/cli/pipeline/fuzz_test.go`) is target T10 of
[the nightly fuzzing plan](../specs/nightly-fuzzing-plan.md). It draws a whole
project from the fuzz bytes — a `gqlc.yaml` written by `config.Config.Canonical`,
schemas from `gen.Schema`, query files from `gen.QueryFile`, and the backend
registry handed to `pipeline.Run` — and checks `Run`'s Result contract on it.
These rows answer §10's question for it: can this harness find a defect at
all, and which detector reports it. They are run by

    just fuzz-liveness FuzzPipelineRun

which executes `.github/scripts/fuzz-liveness/FuzzPipelineRun.rows` through
`driver.sh` beside it. Each row plants one `sed` mutation in
`internal/cli/pipeline/pipeline.go`, in a `git archive HEAD` extract under
`mktemp -d`, never in a checkout.

## What the target checks, and what it normalises

Properties, each named in the failure text the rows match on:
`error-zero` (an error comes with the zero Result), `targets-iff` (Targets
non-nil iff Diagnostics empty, err nil), `targets` (one TargetResult per
declared target; Files non-nil, sorted by Path, no repeats; OutDir the
configured out joined to the config file's directory), `provenance` (every
Diagnostics and Warnings line names a declared `graph[i]`, and none names a
file discovery must skip), `error-class` (no `bug:` marker; an `internal:`
error only for a driver the handed registry lacks; no config file means
`ErrConfigMissing`) and `determinism`.

**Determinism is checked across two directories, with normalisation.** Each
exec lays the same project down in two `t.TempDir()`s and runs `Run` on each.
The project directory leaks into the error text, Diagnostics, Warnings and
OutDir, so each run's own directory is replaced with a token in those strings
before the two Results are compared with `reflect.DeepEqual`. Generated file
bytes are **not** normalised: a project path that reaches emitted code is a
finding.

**How far the generated projects get.** `TestPipelineRunOutcomes` runs 300
projects drawn from PCG(1, 2) streams of 64–1024 bytes through `Run` and puts a
5 % floor on each of the three branches. Measured 2026-10-28 at `57dde5a3`:
success 31, diagnostics 145, error 124. It runs on every `go test`, so if a
generator change starves a branch the test goes red. The refusals behind
the error share are mostly deliberate: gen's own schema and query-file
mutations, backend refusals (AGE's column and multi-label limits, neo4j's
RECORD widths, `:exec` on a projecting query), and genProject's low-rate arms
(no config, missing schema, empty query directory, a missing procsig file,
shared or colliding output directories, a non-identifier package, a registry
missing a backend).

## The run

Measured 2026-10-28 at `e71bcf5f` (branch `feat/gqlc-jjot2.9-fuzz-pipeline`),
go1.27.3, 24 CPUs shared with five other agents' fuzzing (load average 30–66),
`-parallel 4`, budget `LIVENESS_BUDGET=5m`. Every row and control started
from an empty grown corpus (`GQLC_FUZZ_CACHEDIR` set per row). Wall time
includes `just fuzz`'s own `fuzz-list` discovery. The run was made twice; the
table is the second, and the first's times are in the last column.

The md5 columns pin **movement, not identity** (see [README](README.md)).

| row | kind | mutation | md5 before | md5 after | command | verdict | detector | wall (run 2) | wall (run 1) |
|---|---|---|---|---|---|---|---|---|---|
| C1 | control | none | - | - | `go test -count=1 -run '^FuzzPipelineRun$' ./internal/cli/pipeline` | GREEN | - | 9 s | 20 s |
| C2 | control | none | - | - | `just fuzz FuzzPipelineRun 5m 4` | GREEN | - | 324 s | 359 s |
| R1 | seed-reach | `runTarget` panics | 266f60f0 | 3a26d054 | C1's command | **KILLED** | panic, no crasher (seed pass) | 0 s | 1 s |
| R2 | mutation-reach | panic when a target names a procsig file | 266f60f0 | 09b76737 | `just fuzz FuzzPipelineRun 5m 4` | **KILLED** | panic, 1 crasher written | 43 s | 18 s |
| R3 | slow call | R2's condition sleeps 2.5 s | 266f60f0 | 6d92d4ae | `just fuzz FuzzPipelineRun 5m 4` | **SURVIVED** — bd `gqlc-jjot2.25` | none: green, no crasher | 304 s | 304 s |
| R3b | hang | R2's condition never returns | 266f60f0 | 7156b022 | `just fuzz FuzzPipelineRun 5m 4` | **KILLED** | Guard's watchdog ends the worker; engine reports it as hung, 1 crasher written | 14 s | 15 s |
| R4 | property | the accumulating branch keeps its batches | 266f60f0 | 6e4958e7 | `just fuzz FuzzPipelineRun 5m 4` | **KILLED** | property `targets-iff` (seed pass) | 3 s | 4 s |
| R4b | property | OutDir is the configured out, unresolved | 266f60f0 | 3531652f | `just fuzz FuzzPipelineRun 5m 4` | **KILLED** | property `targets` (OutDir; seed pass) | 3 s | 3 s |

What each row means:

- **R2 is the row that says the fuzzer, not the corpus, finds things.** Before
  fuzzing, the driver ran the mutant's committed seeds and they were green:
  none of the five seed byte strings draws a procsig file. genProject draws
  one at 1 in 16 per target, so the engine has to change a decision byte to
  reach it, and it did, in 18 s and in 43 s.
- **R3 SURVIVED, in both runs, and that is a finding about the harness.** It
  is filed as **gqlc-jjot2.25**. The same mutant (md5 `6d92d4ae`, sometimes
  with a hit counter added) was run eight more times: four under `driver.sh`
  cut down to C1 and R3, and four by hand under `go test -fuzz` or
  `just fuzz FuzzPipelineRun 5m 4`. Of the ten runs in all, four were KILLED
  (62 s, 138 s, 139 s, 282 s) and six SURVIVED. Two surviving runs had the
  slow arm instrumented: one executed the 2.5 s sleep 146 times, the other
  200, and neither wrote a crasher. With `GODEBUG=fuzzdebug=1`, the 200 fall
  inside the engine's minimization of *interesting* inputs, which started at
  second 0, and every one of those minimizations came back `crasher: false`.
  With `-fuzzminimizetime 10x`, the mutant was KILLED 3 of 3 times, in
  17–33 s, by `fuzzkit.Guard: slow input`. So the detector works. What is
  lost is the report of an input that first turns up slow while the engine
  is minimizing something else, under go's default 60 s minimize budget,
  which `just fuzz` does not override. The fix is in `fuzz-run`, the WP1
  recipe, and this row is the regression check for it. The other targets'
  R3 rows run under the same default, so a KILLED verdict there may have been
  the lucky half.
- **R3b** is Guard's watchdog: the worker dies at 5 × 2 s and the
  coordinator reports `fuzzing process hung or terminated unexpectedly`. A
  hang kills the worker process, so it does not depend on minimization.
- **R4 and R4b** break the contract without a panic, and the seed pass reports
  both before the engine writes anything. That shows the properties are
  checked, not that mutation reaches them. R4 needs a seed whose queries draw
  a diagnostic, and R4b one that generates; both exist among the five seeds.

## The diffs

Generated by `driver.sh` in the run above (`diff -u` against the pristine
extract). The `sed` scripts themselves are in `FuzzPipelineRun.rows`.

R1:

```diff
@@ -167,6 +167,7 @@
 func runTarget(baseDir string, tgt config.Target, backends codegen.Registry, genBatch bool) (TargetResult, []string, []string, error) {
+	panic("planted: R1")
 	// Stage 2 — resolve paths against the config file's directory.
```

R2:

```diff
@@ -184,6 +184,9 @@
 	// the correct diagnosis (spec §3.1).
+	if tgt.ProcsigPath != "" {
+		panic("planted: R2")
+	}
 	var reg procsig.Registry
```

R3:

```diff
@@ -29,6 +29,7 @@
 	"strings"
+	"time"
 
@@ -184,6 +185,9 @@
 	// the correct diagnosis (spec §3.1).
+	if tgt.ProcsigPath != "" {
+		time.Sleep(2500 * time.Millisecond)
+	}
 	var reg procsig.Registry
```

R3b:

```diff
@@ -184,6 +184,10 @@
 	// the correct diagnosis (spec §3.1).
+	if tgt.ProcsigPath != "" {
+		for {
+		}
+	}
 	var reg procsig.Registry
```

R4:

```diff
@@ -152,7 +152,7 @@
 	if len(diags) > 0 {
-		return Result{Diagnostics: diags, Warnings: warns}, nil
+		return Result{Targets: targets, Diagnostics: diags, Warnings: warns}, nil
 	}
```

R4b:

```diff
@@ -172,7 +172,7 @@
 	queryDir := resolvePath(baseDir, tgt.QueryDir)
-	outDir := resolvePath(baseDir, tgt.Go.Out)
+	outDir := tgt.Go.Out
```

## The 10-minute run

`GQLC_FUZZ_CACHEDIR=$(mktemp -d) just fuzz FuzzPipelineRun 10m 4` on the
unmutated tree at `57dde5a3`, 2026-10-28, under the same shared load: green,
35 758 execs, 70 new interesting inputs (75 in the corpus), no finding. That
is about 60 execs/s. Most of the gap from the 1500 execs/s seen unloaded
in the first seconds is the minimization stalls gqlc-jjot2.25 describes.

## Limits

- Each time above is a single run on a heavily shared machine. R2's time
  depends on the engine's random mutation order. A re-run in which R2 or R3b
  reports SURVIVED at the 5 m budget would falsify the table. A re-run in
  which R3 reports KILLED would not, because R3 was KILLED in 4 of 10 runs
  under the default minimize budget.
- The rows run against `HEAD`, not the working tree.
- No row plants a determinism defect. `pipeline.go` has no state to carry
  across calls, so there is nothing a one-line `sed` could make
  nondeterministic without also planting a panic.
- What the target cannot see: anything gen does not generate (gen's package doc
  lists CALL, MERGE, comprehensions and more), so with gen's queries the
  procsig file is loaded but never used; schemas reached through COPY OF
  chains longer than one hop; config shapes `Canonical` does not write
  (FuzzConfigDecode owns those); and anything above `pipeline.Run` in the CLI.
