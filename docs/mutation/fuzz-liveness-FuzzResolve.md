# Liveness rows: FuzzResolve

FuzzResolve (`internal/resolver/fuzz_test.go`) is target T7 of
[the nightly fuzzing plan](../specs/nightly-fuzzing-plan.md). These rows
answer §10's question for it: can this harness find a defect at all, and
which detector reports it. They are run by

    just fuzz-liveness FuzzResolve

which executes `.github/scripts/fuzz-liveness/FuzzResolve.rows` through
`driver.sh` beside it. Each row plants one `sed` mutation in a
`git archive HEAD` extract under `mktemp -d`, never in a checkout.

## The run

Measured 2026-10-28 at `bff104ac` (branch `feat/gqlc-jjot2.7-fuzz-resolve`),
go1.27.3, 24 CPUs shared with five other agents (load average 50–66 during
the runs), `-parallel 4`, budget `LIVENESS_BUDGET=5m`. Every row and control
started from an empty grown corpus (`GQLC_FUZZ_CACHEDIR` set per row), so the
times are from the committed seeds alone. Wall time includes `just fuzz`'s
own `fuzz-list` discovery.

The md5 columns pin **movement, not identity** (see
[README](README.md)): they are the first 8 hex digits of the file before and
after the `sed`, and the diffs below are the bytes.

| row | kind | mutation | md5 before | md5 after | command | verdict | detector | wall |
|---|---|---|---|---|---|---|---|---|
| C1 | control | none | - | - | `go test -count=1 -run '^FuzzResolve$' ./internal/resolver` | GREEN | - | 7 s |
| C2 | control | none | - | - | `just fuzz FuzzResolve 5m 4` | GREEN | - | 322 s |
| R1 | seed-reach | `resolve` panics on entry | 897d867e | f8730f6d | C1's command | **KILLED** | panic, no crasher (seed pass) | 1 s |
| R2 | mutation-reach | `resolve` panics on four parameters, or two over three parts | 897d867e | e5137437 | `just fuzz FuzzResolve 5m 4` | **KILLED** | panic, 1 crasher written | 7 s |
| R3 | slow call | R2's condition sleeps 3 s, once per worker process | 897d867e | a1920c71 | `just fuzz FuzzResolve 5m 4` | **KILLED** | `fuzzkit.Guard` Fatalf, 1 crasher written | 12 s |
| R3b | hang | R2's condition never returns | 897d867e | 355e45ae | `just fuzz FuzzResolve 5m 4` | **KILLED** | Guard's watchdog ends the worker; engine reports it as hung while minimizing, 1 crasher written | 17 s |
| R4a | property | a branch refusal returns a non-zero model | 897d867e | c8bad8f7 | `just fuzz FuzzResolve 5m 4` | **KILLED** | property `zero on error` (seed pass) | 3 s |
| R4b | property | a branch refusal loses its sentinel (`%v`) | 897d867e | b12e3965 | `just fuzz FuzzResolve 5m 4` | **KILLED** | property `error class` (seed pass) | 3 s |
| R4c | property | every accepted model gains an untyped column | 897d867e | b50cc8dc | `just fuzz FuzzResolve 5m 4` | **KILLED** | property `well-formed` (seed pass) | 3 s |
| R4d | property | `StatementKind` refuses to marshal | 96f347d6 | e1f24b76 | `just fuzz FuzzResolve 5m 4` | **KILLED** | property `marshal` (seed pass) | 3 s |
| R4e | property | every second successful `Resolve` flips `Distinct` | 897d867e | 083c34e5 | `just fuzz FuzzResolve 5m 4` | **KILLED** | property `determinism` (seed pass) | 3 s |

What each row means:

- **R2 is the row that says the fuzzer, not the corpus, finds things.** Its
  condition was chosen by measurement, printing the counts at the planted
  line: the 626 committed seeds resolve with 0, 1, 2, 3 or 6 parameters and
  never with four, and never with two over three parts; 352 of 5000 random
  generated-arm streams (7 %) do one or the other. The driver re-ran the
  mutant's seeds before fuzzing and they were green. Two earlier conditions
  are recorded because they failed: **seven columns** SURVIVED 5 m (the
  generated arm projects at most four columns, and the text arm needs a whole
  projection inserted), and **four parameters alone** (2.5 % of streams)
  killed R2 in 12 s but left R3 SURVIVED in the same run.
- **R3 sleeps once per worker process, and the reason is an engine finding,
  gqlc-jjot2.26.** With a sleep on every hit, R3 SURVIVED in two liveness runs
  (rc 0, no crasher), and two direct runs of that mutant, instrumented to log
  each hit, logged 179 and 161 three-second hits and still printed PASS. The
  condition is usually first met while the engine minimizes a new-coverage
  input. A failing candidate switches that minimization to crash mode, every
  later candidate is slow too, and minimization runs into
  `-fuzzminimizetime`. On that context timeout, `worker.minimize` in go1.27.3
  returns the original entry with its original, empty crash message, so the
  crash is dropped. A panic (R2) minimizes fast and a hang (R3b) ends the
  process, so neither is lost. With the sleep once per process, later
  candidates are fast, minimization ends inside its budget, and Guard's
  `Fatalf` text is the crasher's message. So this row shows that Guard reports
  a slow call. It does **not** show that a persistently slow input is
  reported. That case is the open bead's.
- **R3b**'s evidence is the coordinator's line plus the crasher, as in
  FuzzStream's R3b: the watchdog's panic text is a worker's stderr, which the
  coordinator does not relay.
- **R4a–R4e** break one property each without a panic, on shapes the seeds
  already carry, so the seed pass reports them before the engine writes
  anything. They show each property is checked and name the one that fired;
  they do not show mutation reaches it.

## The diffs

Generated by `driver.sh` in the run above (`diff -u` against the pristine
extract). The `sed` scripts themselves are in `FuzzResolve.rows`.

R1:

```diff
@@ -31,6 +31,7 @@
 		return ValidatedQuery{}, fmt.Errorf("%w: path binding %q in a pattern comprehension", ErrOutOfR0Scope, q.ComprehensionPaths[0])
 	}
 
+	panic("planted: R1")
 	branchCols := make([][]Column, len(q.Branches))
 	branchScopeTables := make([][]partScope, len(q.Branches))
 
```

R2:

```diff
@@ -50,6 +50,9 @@
 		orientation.absorb(ev)
 	}
 
+	if n := len(q.Parameters); n == 4 || (n == 2 && len(q.Branches[0].Parts) == 3) {
+		panic("planted: R2")
+	}
 	if err := compareBranchColumns(branchCols); err != nil {
 		return ValidatedQuery{}, err
 	}
```

R3:

```diff
@@ -5,6 +5,7 @@
 	"slices"
 	"sort"
 	"strings"
+	"time"
 
 	"github.com/areqag/gqlc/internal/graph"
 	"github.com/areqag/gqlc/internal/procsig"
@@ -18,6 +19,8 @@
 // exact lexical Part's scope in its lexical branch (fvo per ADR 0008 amendment
 // 2026-07-06; the pre-fvo any-valid-witness rule at §4.2.4 retires), and folds
 // Part.Distinct + UnionKind into ValidatedQuery.Distinct (§3.2/§4.7).
+var plantedSlow bool
+
 func resolve(q query.Query, s schema.Schema, r procsig.Registry) (ValidatedQuery, error) {
 	if len(q.Branches) == 0 {
 		// Defensive tripwire; the parser's buildBranch guarantees >= 1
@@ -50,6 +53,12 @@
 		orientation.absorb(ev)
 	}
 
+	if n := len(q.Parameters); n == 4 || (n == 2 && len(q.Branches[0].Parts) == 3) {
+		if !plantedSlow {
+			plantedSlow = true
+			time.Sleep(3 * time.Second)
+		}
+	}
 	if err := compareBranchColumns(branchCols); err != nil {
 		return ValidatedQuery{}, err
 	}
```

R3b:

```diff
@@ -50,6 +50,10 @@
 		orientation.absorb(ev)
 	}
 
+	if n := len(q.Parameters); n == 4 || (n == 2 && len(q.Branches[0].Parts) == 3) {
+		for {
+		}
+	}
 	if err := compareBranchColumns(branchCols); err != nil {
 		return ValidatedQuery{}, err
 	}
```

R4a:

```diff
@@ -43,7 +43,7 @@
 	for b, branch := range q.Branches {
 		cols, uses, ev, err := resolveBranch(branch, s, r)
 		if err != nil {
-			return ValidatedQuery{}, err
+			return ValidatedQuery{Distinct: true}, err
 		}
 		branchCols[b] = cols
 		branchScopeTables[b] = useSitesToScopes(uses)
```

R4b:

```diff
@@ -43,7 +43,7 @@
 	for b, branch := range q.Branches {
 		cols, uses, ev, err := resolveBranch(branch, s, r)
 		if err != nil {
-			return ValidatedQuery{}, err
+			return ValidatedQuery{}, fmt.Errorf("%v", err)
 		}
 		branchCols[b] = cols
 		branchScopeTables[b] = useSitesToScopes(uses)
```

R4c:

```diff
@@ -60,7 +60,7 @@
 	}
 
 	return ValidatedQuery{
-		Columns:    branchCols[0],
+		Columns:    append(slices.Clip(branchCols[0]), Column{Name: "planted"}),
 		Parameters: params,
 		Statement:  StatementKind(q.StatementKind),
 		Distinct:   computeDistinct(q),
```

R4d:

```diff
@@ -105,7 +105,7 @@
 
 // MarshalJSON renders a StatementKind as its wire string.
 func (k StatementKind) MarshalJSON() ([]byte, error) {
-	return json.Marshal(k.String())
+	return json.Marshal(func() {})
 }
 
 // ResolvedType is the sum of resolved types. Each variant carries a
```

R4e:

```diff
@@ -18,6 +18,8 @@
 // exact lexical Part's scope in its lexical branch (fvo per ADR 0008 amendment
 // 2026-07-06; the pre-fvo any-valid-witness rule at §4.2.4 retires), and folds
 // Part.Distinct + UnionKind into ValidatedQuery.Distinct (§3.2/§4.7).
+var plantedCalls int
+
 func resolve(q query.Query, s schema.Schema, r procsig.Registry) (ValidatedQuery, error) {
 	if len(q.Branches) == 0 {
 		// Defensive tripwire; the parser's buildBranch guarantees >= 1
@@ -59,11 +61,12 @@
 		return ValidatedQuery{}, err
 	}
 
+	plantedCalls++
 	return ValidatedQuery{
 		Columns:    branchCols[0],
 		Parameters: params,
 		Statement:  StatementKind(q.StatementKind),
-		Distinct:   computeDistinct(q),
+		Distinct:   computeDistinct(q) != (plantedCalls%2 == 0),
 		// Last, and only on the success path: a query that fails resolution
 		// gets one verdict, the error (ADR 0032).
 		//
```

## Open findings this target tolerates

FuzzResolve's first runs found two resolver defects that need an owner
ruling. Each is tolerated by an arm of `openFinding` in `fuzz_test.go` keyed to
its bead, and its crasher is committed under
`internal/resolver/testdata/fuzz/FuzzResolve/<bead id>`. The fix PR deletes
the arm (plan §11).

| bead | property | what the arm hides |
|---|---|---|
| gqlc-jjot2.19 | `well-formed`: column names unique | duplicate result column names (`RETURN 0,0`); openCypher's ColumnNameConflict is checked by neither the parser nor the resolver |
| gqlc-jjot2.20 | `determinism` | two `Resolve` calls that differ only in `"nullable"` fields; OPTIONAL group demotion depends on map order |

## Limits

- Every time above is one run on one loaded host. R2, R3 and R3b depend on the
  engine's random mutation order. What would falsify the table is a re-run in
  which any of them reports SURVIVED at the 5 m budget.
- The rows run against `HEAD`, not the working tree.
- No row plants a defect in the CALL-registry plain arm. A query that reaches
  it is logged, not failed (`callRegistryArm`), and the generated arm writes no
  CALL. No row mutates either tolerance arm.
- The R2 condition counts only queries that resolve that far. It sits after
  every branch has resolved, so a query refused inside a branch never meets
  it.
