# Liveness rows: FuzzGenerate

FuzzGenerate (`internal/codegen/conformance/fuzz_test.go`) is target T8 of
[the nightly fuzzing plan](../specs/nightly-fuzzing-plan.md): a schema and a
query batch the front end admits, through every backend in the composed
registry (neo4j v5, neo4j v6, Apache AGE). These rows answer §10's question
for it: can this harness find a defect at all, and which detector reports it.
They are run by

    just fuzz-liveness FuzzGenerate

which executes `.github/scripts/fuzz-liveness/FuzzGenerate.rows` through
`driver.sh` beside it. This file also records the cross-backend measurement
the target's pinned agreements rest on (plan §5, row T8), and the two codegen
defects the target found while being built.

## The run

Measured 2026-10-28 at `48529d7e` (branch `feat/gqlc-jjot2.8-fuzz-generate`),
go1.27.3, 24 CPUs shared with other fuzzing sessions (load average 15 to 68
over the day), `-parallel 4`, budget `LIVENESS_BUDGET=5m`. Every row and
control started from an empty grown corpus. Wall time includes `just fuzz`'s
own `fuzz-list` discovery.

The md5 columns pin **movement, not identity** (see [README](README.md)).

| row | kind | mutation | md5 before | md5 after | command | verdict | detector | wall |
|---|---|---|---|---|---|---|---|---|
| C1 | control | none | - | - | `go test -count=1 -run '^FuzzGenerate$' ./internal/codegen/conformance` | GREEN | - | 8 s |
| C2 | control | none | - | - | `just fuzz FuzzGenerate 5m 4` | GREEN | - | 321 s |
| R1 | seed-reach | `Prepare` panics | cbf78c96 | 6926ec02 | C1's command | **KILLED** | panic, no crasher (seed pass) | 0 s |
| R2 | mutation-reach | `Prepare` panics on a six-node-type schema | cbf78c96 | a2aabd86 | `just fuzz FuzzGenerate 5m 4` | **KILLED** | panic, 1 crasher written | 4 s |
| R3 | slow call | `Prepare` sleeps 1.5 s on R2's condition | cbf78c96 | 6fc7828a | `just fuzz FuzzGenerate 5m 4` | **KILLED** | `fuzzkit.Guard` Fatalf (4.5 s against 2 s), 1 crasher written | 29 s |
| R3b | hang | `Prepare` never returns on R2's condition | cbf78c96 | 5c423b39 | `just fuzz FuzzGenerate 5m 4` | **KILLED** | engine reports the worker hung (Guard's watchdog), 1 crasher written | 15 s |
| R4a | property | `Finalise` sorts paths descending | 5e2d58b8 | 3ca28b12 | `just fuzz FuzzGenerate 5m 4` | **KILLED** | property `path-order` (seed pass) | 3 s |
| R4b | property | `Finalise` adds a blank line after gofmt | 5e2d58b8 | cf153b60 | `just fuzz FuzzGenerate 5m 4` | **KILLED** | property `gofmt` (seed pass) | 3 s |
| R4c | property | `Header` numbers the files it heads | 5e2d58b8 | bc161f01 | `just fuzz FuzzGenerate 5m 4` | **KILLED** | property `determinism` (seed pass) | 3 s |
| R4d | property | AGE names `graph.go` `graphs.go` | 6dd51468 | 32318970 | `just fuzz FuzzGenerate 5m 4` | **KILLED** | property `age-neo4j` (seed pass) | 3 s |

What each row means:

- **R2 is the row that says the fuzzer, not the corpus, finds things.** Six
  node types is `gen.Schema`'s maximum, and of the 148 seeds (130
  fixtures, 18 generated streams) none the front end admits declares six, which the
  driver re-checked by running the mutant's seeds green before fuzzing. Over 5000 random generated-arm streams,
  90 of the 4048 the front end admitted (2.2 %) had six. Kill times on three
  runs: 4 s, 8 s, 13 s (the last on FuzzGeneratedBuilds, same condition).
- **The first R2 condition was too unreliable to keep.** A query name holding
  a letter no fixture's name does (`X`, then any of `JXYjqz`) is reached only
  by byte mutation of the text arm. It was reached in 49 s and 110 s on two
  runs and not at all within 5 minutes on two others (once as R3b, once as
  R3, both recorded SURVIVED with no crasher). A row that fails half the time
  cannot tell a dead detector from an unlucky run.
- **R3** is Guard's after-the-fact report: two or three backends reach
  `Prepare` per guarded call (AGE refuses some batches before it), so the call
  took 4.5 s against the 2 s limit and under the 10 s watchdog.
- **R3b**'s evidence is the coordinator's `fuzzing process hung or terminated
  unexpectedly while minimizing` line plus the crasher; the watchdog's own
  panic text is not relayed by the coordinator (as in FuzzStream's R3b).
- **R4a–R4d** each break one property without a panic on a shape every seed
  carries, so the seed pass reports them: they show each property is checked
  and name the one that fired. R4c's first version appended a numbered comment
  to each file; gofmt did not keep it byte-stable, so the `gofmt` property
  fired first and the row read SURVIVED. Numbering the header, which goes
  through go/format with the file, isolates determinism.

Not given a row: the `v5-v6` property and the `sentinel` / `error-nil-files`
/ `go-file` / `non-empty` arms of `emissionViolation`. They are written but
no mutation was run against them.

## Findings

The target found two codegen defects while these rows were being run. Both
were fixed on this branch, with the reduced input committed as a regression
seed under `internal/codegen/conformance/testdata/fuzz/FuzzGenerate/` named
for its bead.

| bead | found by | input | was | fix |
|---|---|---|---|---|
| gqlc-jjot2.21 | R3's run, unrelated to its plant | a query binding `$0` and `$1` | `ErrFormatFailure` on all three backends: the Params struct gets a field named `0` | `deriveParamFields` refuses any mangle that is not a Go field name, not only the empty one (gqlc-2m2v's arm), as `ErrOutOfC6Scope` |
| gqlc-jjot2.23 | C2 control, unmutated tree | an entity property `` `a"b` `` (also `` `a-b` ``, `` `1x` ``, `` `my prop` ``) | `ErrFormatFailure` on all three backends: the entity struct gets a field that is not a Go identifier | `prepareEntityFields` refuses it as `ErrPropertyFieldCollision`, the entity-side mirror of the record check; invalid fixture `property_field_unspellable` |

Both crashers were lost with the liveness scratch copy (the next row's
restore deletes `testdata/fuzz/<name>`; bd gqlc-jjot2.24) and were reproduced
by hand from the failure text.

After both fixes, `just fuzz FuzzGenerate 10m 4` at `48529d7e` was green:
883,447 execs, 152 new interesting inputs. The same run at `27c4aa0e`, before
either fix, found neither defect: 1,045,956 execs, 136 new interesting inputs, green. Of those 136, 110
were text-arm inputs and only 21 passed the front end (17 admitted by at least
one backend, 12 by all three): the grown corpus is dominated by inputs the
parsers refuse, because that is where new coverage is cheapest. The text arm
still earns its place — gqlc-jjot2.21 and .23 are both shapes `gen` does not
write — but the generated arm's share of the budget is smaller than its share
of the seeds.

## Cross-backend agreement: what was measured, what is pinned

Measured 2026-10-28 over `test/data/codegen/valid` (130 fixtures; `skeleton`
declares no query file, so the queryfile front end refuses its empty text and
129 reach codegen), each fixture as a text-arm input through
`fuzzFrontEnd` and all three backends, whatever the fixture's own manifest
enrols.

| comparison | holds | does not hold |
|---|---|---|
| v5 and v6 both succeed or both fail | 128 | 1 (`vector_property`: v5 `neo4j.ErrUnrepresentableOnDriverVersion`, v6 ok) |
| ... both fail with the same sentinel set | 12 of 12 (all `ErrUnstorableProperty`, identical text) | 0 |
| ... both succeed with the same path set | 116 of 116 | 0 |
| AGE admits iff neo4j (v6) admits | 87 (all both-admit) | 30 (AGE refuses, neo4j admits: 18 `age.ErrUnsupportedQuery`, 8 `age.ErrUndefinedFunction`, 2 `age.ErrRelationshipTypeAlternation`, 1 `age.ErrUnsupportedSchema`, 1 `ErrUnrepresentableWidth`) + 12 (neo4j refuses with `ErrUnstorableProperty`, AGE admits) |
| where AGE and neo4j both succeed, AGE's paths minus `graph.go` equal neo4j's minus `*_neo4j.go` | 87 of 87 | 0 |

Pinned (`crossBackendViolation`):

- **v5 vs v6**: same outcome, same sentinel set on failure, same path set on
  success — except where either side's error is
  `ErrUnrepresentableOnDriverVersion`, which is the refusal that names the
  difference between the majors (ADR 0051).
- **AGE vs neo4j file sets**: AGE always emits `graph.go`, and apart from it
  and the `*_neo4j.go` carrier bridges the two emit the same files, so the
  shared carriers (`temporal.go`, `uuid.go`) are emitted by both or neither.

Not pinned: admission agreement between AGE and neo4j, which fails in both
directions on the corpus by design (ADR 0027's multi-label refusal, AGE's
dialect gaps, neo4j's storage refusals). The plan's falsifier for this
section: a fuzzed input on which the v5/v6 or file-set agreement fails for a
reason the backends' authors call correct, which moves the pin, not the code.

## The diffs

Generated by `driver.sh` in the run above. The `sed` scripts are in
`FuzzGenerate.rows`.

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
@@ -6,6 +6,7 @@
 	"path/filepath"
 	"slices"
 	"strings"
+	"time"
 	"unicode/utf8"
 
 	"github.com/areqag/gqlc/internal/graph"
@@ -383,6 +384,9 @@
 //
 // packageName overrides the Schema.Name derivation when non-empty.
 func Prepare(in Input, tm TypeMap, packageName string) (Prepared, error) {
+	if len(in.Schema.Nodes) == 6 {
+		time.Sleep(1500 * time.Millisecond)
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

R4a:

```diff
@@ -29,6 +29,6 @@
 		}
 		files[i].Contents = formatted
 	}
-	slices.SortFunc(files, func(a, b File) int { return cmp.Compare(a.Path, b.Path) })
+	slices.SortFunc(files, func(a, b File) int { return cmp.Compare(b.Path, a.Path) })
 	return files, nil
 }
```

R4b:

```diff
@@ -27,7 +27,7 @@
 		if err != nil {
 			return nil, fmt.Errorf("%w: %s: %w", ErrFormatFailure, f.Path, err)
 		}
-		files[i].Contents = formatted
+		files[i].Contents = append(formatted, '\n')
 	}
 	slices.SortFunc(files, func(a, b File) int { return cmp.Compare(a.Path, b.Path) })
 	return files, nil
```

R4c:

```diff
@@ -13,8 +13,11 @@
 // byte-identical across files in a batch. The Go toolchain regex
 // (^// Code generated .* DO NOT EDIT\.$) matches so gofmt / linters
 // treat the files as generated.
+var plantedHeaders int
+
 func Header() string {
-	return fmt.Sprintf("// Code generated by gqlc %s. DO NOT EDIT.\n\n", version.Version)
+	plantedHeaders++
+	return fmt.Sprintf("// Code generated by gqlc %s (%d). DO NOT EDIT.\n\n", version.Version, plantedHeaders)
 }
 
 // Finalise gofmts every file's contents and sorts the set by Path — the
```

R4d:

```diff
@@ -130,7 +130,7 @@
 
 	files = []codegen.File{
 		{Path: "db.go", Contents: renderDB(pkg, len(prepared.Queries) > 0, hasOne)},
-		{Path: "graph.go", Contents: renderGraph(pkg)},
+		{Path: "graphs.go", Contents: renderGraph(pkg)},
 		{Path: "querier.go", Contents: renderQuerier(pkg, prepared.Queries)},
 		{Path: "models.go", Contents: renderModels(pkg, entities, h)},
 	}
```
