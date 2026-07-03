# Stage 15 spec — Cypher query parser: TCK corpus completion and skiplist policy categorization

The implementation brief for Stage 15 of the Cypher implementation of
`query.Parser`. Fifteenth (and final pre-freeze) model evolution after
Stage 14 per ADR 0004, under the curation discipline of ADR 0003 and
the type-interface boundary of ADR 0005. This stage closes out the
`ADR 0007` expansion audit: every TCK feature directory is either
wired into the godog acceptance suite or explicitly documented
out-of-scope, and every skiplist entry carries a typed policy category
so pending unambiguously means "work remaining", never "opaque skip".

Stage 15 is scope-minimal by design: **no model widening, no new
sentinel, no new fail-site, no new parse-shape gate.** The parser's
type-interface surface freezes exactly where Stage 14 left it. This
document is a **delta** against Stages 0–14 in the same spirit as the
Stage-14 spec — sections appear only where Stage 15 changes something.

Tracking: bead `gqlc-x6u` (GitHub #51). Blocks the query.Query freeze
(gqlc-cta). Lands as one graphite branch (`tck-corpus-sweep`) with
separated commits (spec + docs → step registrations + noop wiring →
useCases dir enablement + goldens → skiplist policy enum + Test
policy guard), independently mergeable as a whole: `just test` is
green if this branch lands on `master` alone (AGENTS.md stacked-branch
invariant).

---

## 1. Deliverables

### 1.1 Two new TCK feature directories wired into the acceptance suite

`readCoreDirs` in `internal/query/cypher/acceptance_test.go` gains two
entries:

- `../../../test/data/query/cypher/tck/features/useCases/countingSubgraphMatches`
- `../../../test/data/query/cypher/tck/features/useCases/triadicSelection`

Both dirs exist verbatim in the vendored TCK; both are pure read-side
composition corpora (MATCH … OPTIONAL MATCH … WITH … WHERE … RETURN
count(…)) exercising surfaces Stages 0–14 already carry. **No parser
work is required to make them pass** — a scratch probe against
`cypher.New().Parse(…)` over every `executing query:` doc-string in
both feature files reports 30 / 30 parse-green under the current
parser (see §4.2). The wiring is corpus enablement plus step
registration; no model touch.

### 1.2 Two new Given-step registrations for named-graph fixtures

Both feature files use two `Given` step phrasings not currently
registered:

- `Given the binary-tree-1 graph`
- `Given the binary-tree-2 graph`

The graphs are runtime-only CREATE fixtures (their `.cypher` scripts
live under `test/data/query/cypher/tck/graphs/binary-tree-{1,2}/`),
consumed by an engine, never by us. From a parser standpoint they are
identical to `Given an empty graph` — the parser's disposition on the
per-scenario `executing query:` step does not depend on graph state.

The step registration uses an **enumerated regex** rather than a
generic catch-all:

```go
ctx.Step(`^the (binary-tree-1|binary-tree-2) graph$`, noop)
```

The enumeration is deliberate: a TCK addition of `binary-tree-3` (or
any other named fixture) surfaces as a `TestNoUndefinedSteps` failure
— a corpus-drift signal. A generic `^the .+ graph$` would silently
absorb any future fixture, breaking the harness-gap guard.

### 1.3 `skiplist` reshaped to a typed-category map

The 89-entry `skiplist` gains a per-entry policy category. The map
value type changes from `bool` to a new unexported enum:

```go
type skipCategory int

const (
	catRuntimeError skipCategory = iota + 1  // 1..N — zero value is invalid on purpose
	catResultAssertionOnly
	catValueBelowBoundary
	catGroupingKeySemantic
	catBindingKindConflict
	catWriteShapeConstraint
	catClauseComposition
	catSignatureArgCheck
)

var skiplist = map[string]skipCategory{ ... }
```

The Before hook that consults the map is a membership check, which the
new value type continues to support:

```go
if _, ok := skiplist[sc.Name]; ok {
    st.skipped = true
}
```

The map literal is **grouped by category** with a section header
comment per group, and every entry within a section literally names
its category value (`catRuntimeError` etc.). The group header is for
humans; the enum is for the compiler; the new `TestSkiplistCategoryPolicy`
guard (§1.5) cross-checks the two. See §4 for the complete 89-entry
categorization.

The zero value (`skipCategory(0)`) is deliberately not a valid
category — an accidentally-typed `"[X] Foo": {}` literal is a build
error via a policy-membership assertion at the top of the new guard
test.

### 1.4 New public taxonomy: the eight policy categories are a stable
contract

The eight categories in §1.3 are the exhaustive taxonomy the data
supports. Each category names an actual TCK-rule family; no category
is invented, no category has zero entries (that is a guard failure,
§1.5). The category names are chosen to align with ADR 0007's
"bucket-3" vocabulary (accept-and-defer, engine raises via re-executed
original text per ADR 0005). One-line definitions:

- **`catRuntimeError`** — the TCK's negative outcome is a runtime rule
  the engine detects at execution: entity-not-found on a deleted node,
  constraint-verification failures, TypeErrors on stored property
  values, SemanticErrors like MergeReadOwnWrites. Not a parse-shape
  concern.
- **`catResultAssertionOnly`** — a result-shape / column-set /
  scope-of-projection rule visible only at the produced result set:
  duplicate output column names, UNION branches with different
  columns, RETURN * with nothing in scope, sort-key naming a variable
  not in scope. The type-interface model does not carry column
  identity comparisons or sort-key referential integrity.
- **`catValueBelowBoundary`** — a compile-time-named check whose rule
  fires on the LITERAL VALUE or the RUNTIME-BOUND PARAMETER VALUE, not
  on the shape. SKIP/LIMIT constant/parameter negatives (negative,
  floating-point, non-constant) live here — the parser records the
  parameter's name and lets the engine validate the value on the
  original text (ADR 0005).
- **`catGroupingKeySemantic`** — aggregate-position rules and
  grouping-key correctness: aggregates in WHERE, nested aggregates,
  AmbiguousAggregationExpression at RETURN / WITH / ORDER BY /
  procedure-call argument, aggregates in ORDER BY of non-projected
  columns, `rand()` inside `count(...)`. All ride the same semantic
  rule the type interface does not carry.
- **`catBindingKindConflict`** — VariableTypeConflict and
  VariableAlreadyBound rules that turn on binding kind (node vs edge
  vs path vs value) or on the combination of clause order, labels, and
  inline properties. Includes relationship-uniqueness (reusing a
  relationship variable in the same pattern).
- **`catWriteShapeConstraint`** — CREATE and MERGE pattern-shape
  constraints that the type-interface carries verbatim but the
  write-clause semantic rule reads more narrowly: NoSingleRelationshipType,
  RequiresDirectedRelationship, CreatingVarLength; plus DELETE
  target-shape rules (InvalidDelete on labelled/relationship-type
  targets, InvalidArgumentType on an integer-expression delete
  target).
- **`catClauseComposition`** — cross-clause and cross-position
  composition rules the type-interface does not enforce: mixing UNION
  with UNION ALL, EXISTS-containing-write, pattern predicate as
  function argument (`size(...)`), path.property in WHERE, pattern
  buried in the RHS of a SET item, UnknownFunction (the parser carries
  no function name so cannot distinguish it from a known function).
- **`catSignatureArgCheck`** — Stage-14-specific argument-vs-signature
  checks the parser defers per Stage-14 §4.5: InvalidArgumentType,
  MissingParameter on implicit invocation.

Every skiplist entry is a member of exactly one category (§4). The
categories are grouped so that a future stage adding a widening that
now REJECTS a shape (removing entries) affects one section, not the
whole map.

### 1.5 New guard: `TestSkiplistCategoryPolicy`

A new test in the acceptance-suite file replaces the informal
"no unexplained skiplist entries" convention with a machine check.
It asserts two properties:

1. **Category membership.** Every entry's category (the map value) is
   one of the eight declared constants. The zero value
   (`skipCategory(0)`) is not a member — an unassigned category fails
   the test with the entry name.
2. **No dead categories.** Every declared category has at least one
   entry. A category with zero entries is a dead policy slot — the
   test fails with the category name. This ensures the taxonomy stays
   honest: if a later stage removes the last entry of a category, the
   guard forces the developer to either delete the category or
   document why it stays.

The test enumerates skiplist entries once, computes a
`map[skipCategory]int` count, and drives both assertions off it. No
scenario is skipped or paused; the test runs in milliseconds and adds
to the guard suite alongside `TestSkiplistOrphans`, `TestGoldenOrphans`,
and `TestNoUndefinedSteps`.

### 1.6 One paragraph appended to the `readCoreDirs` comment block

The stage-by-stage narrative above `readCoreDirs` gains a short
paragraph closing out the audit. The non-obvious constraint is that
useCases/* is composition corpus, not a new surface — no model
widening, no skiplist entries, all 30 scenarios parse-green under
Stage 0–14:

```go
// Stage 15 adds useCases/{countingSubgraphMatches,triadicSelection} —
// composition corpus over the Stages 0-14 surface; no model widening,
// no new skiplist entries, all 30 scenarios parse-green at wiring
// time. Stage 15 completes the TCK-corpus audit: every feature dir is
// now wired; every skiplist entry carries a typed policy category
// (see the skipCategory sum below). The pending count is a true
// progress meter — a pending scenario is a documented bucket-3
// accept-and-defer, not opaque.
```

Kept short (Q6 ruling); no aspirational language.

---

## 2. Scope discipline (what Stage 15 does NOT change)

**No model widening.** `query.Query`, `Part`, `Effect`, all sums frozen
exactly at Stage 14. This bead directly blocks the freeze bead
(`gqlc-cta`) — a model change here would defeat the purpose. If the
implementation encounters an unavoidable model change need, the
implementer STOPS and reports before proceeding (per the team-lead
bead-body instruction).

**No new sentinel.** The canonical set stays at 7 sentinels.
`unsupportedSentinels` stays at `[ErrUnsupportedParameter]` — the sole
remaining progress-meter sentinel. No sentinel is retired here.

**No new parser fail-site.** No `l.fail(...)` call is added or moved.
The parser's disposition on every already-wired scenario is
byte-identical before and after Stage 15.

**No consumer changes.** No resolver, codegen, or driver code is
touched. No public API changes. The public `Cypher.Parse` signature is
unchanged.

**No golden mutation of existing scenarios.** No existing `.golden.json`
file is rewritten. Stage 15 mints goldens ONLY for the 30 newly-wired
useCases scenarios; every one of the existing 3169 goldens stays
byte-identical.

**No changes to `.beads/` or `.claude/` files** — the corpus sweep
touches only spec + acceptance-test wiring + goldens.

---

## 3. Corpus census (what "TCK complete" means)

### 3.1 Every feature dir under `test/data/query/cypher/tck/features/`

Enumerated verbatim, marked wired (Stage lists) or newly-wired
(Stage 15) or out-of-scope (with reason):

**Wired at Stages 0-14 (35 dirs):**

- `clauses/match`, `clauses/return`, `clauses/match-where`,
  `clauses/return-skip-limit`, `clauses/union`, `clauses/with` — Stages
  0-4.
- `clauses/return-orderby`, `clauses/with-orderBy`,
  `clauses/with-skip-limit`, `clauses/with-where`, `clauses/unwind` —
  Stage 9.
- `clauses/create`, `clauses/delete`, `clauses/set`, `clauses/remove` —
  Stage 12.
- `clauses/merge` — Stage 13.
- `clauses/call` — Stage 14.
- `expressions/literals`, `expressions/boolean`,
  `expressions/comparison`, `expressions/mathematical`,
  `expressions/string`, `expressions/null`, `expressions/precedence`,
  `expressions/typeConversion`, `expressions/list`, `expressions/map`,
  `expressions/conditional` — Stage 6.
- `expressions/temporal` — Stage 7.
- `expressions/path`, `expressions/pattern`, `expressions/graph` —
  Stage 8.
- `expressions/aggregation` — Stage 10.
- `expressions/quantifier`, `expressions/existentialSubqueries` —
  Stage 11.

**Newly wired (Stage 15, 2 dirs):**

- `useCases/countingSubgraphMatches` (11 scenarios, 1 feature file)
- `useCases/triadicSelection` (19 scenarios, 1 feature file)

**Out-of-scope (0 dirs):** none. Every dir under `features/**` is now
wired.

Leaf dirs holding `.feature` files under `features/**` = 37 (35 wired at
Stages 0-14 + 2 newly wired at Stage 15). `find test/data/query/cypher/tck/features
-maxdepth 2 -mindepth 1 -type d` returns 40 because it also enumerates the three
parent dirs `clauses/`, `expressions/`, `useCases/`; the audited leaf count is
40 - 3 = 37, matching the enumeration above.

### 3.2 Non-feature TCK subtrees intentionally not consumed

Two subtrees under `test/data/query/cypher/tck/` exist but are not
`.feature` corpora and are consumed by the engine at runtime, not by
the parser:

- `tck/graphs/binary-tree-{1,2}/` — CREATE-clause fixture scripts (the
  runtime graph the two useCases feature files reference via `Given
  the binary-tree-N graph`). The parser sees the Given step as a
  noop; the .cypher script is never executed by the harness. Not a
  parse target.
- `tck/{README.adoc,index.adoc,pom.xml,LICENSE}` — TCK metadata. Not a
  parse target.

Both are documented here for the record; neither is a coverage gap.

---

## 4. Count arithmetic (before → after)

All counts verified independently on the pre-Stage-15 baseline (master
`af06790`, worktree `tck-corpus-sweep`). Team-lead recounts everything.

### 4.1 Suite counters

| Counter                                    | Before | After  | Δ    | Reason                                                              |
| ------------------------------------------ | ------ | ------ | ---- | ------------------------------------------------------------------- |
| Total scenarios (godog pretty summary)     | 3867   | 3897   | +30  | 11 counting + 19 triadic pickles newly enumerated                   |
| Passed                                     | 3429   | 3459   | +30  | All 30 useCases scenarios parse-green (§4.2)                        |
| Pending                                    | 438    | 438    | 0    | No new skiplist entries; no new bucket-3 accept                     |
| Failed                                     | 0      | 0      | 0    | Zero regressions                                                    |
| Total steps                                | 15875  | 16006  | +131 | Direct step-line count: 55 (countingSubgraphMatches) + 76 (triadicSelection); neither file has a Background block |

Arithmetic: 3429 + 30 = 3459; 3867 + 30 = 3897; 438 + 0 = 438. All three
end-state numbers match: `3459 + 438 = 3897`. Verified.

### 4.2 The +30 passed derivation (justifies the +30 claim)

Every scenario in both useCases feature files has the shape:

```gherkin
Given the binary-tree-N graph [or `Given an empty graph` + `And having executed:`]
When executing query: """ <read-kind query> """
Then the result should be, in any order: | ... |
And no side effects
```

Confirmed by `grep -h "Then " test/data/query/cypher/tck/features/useCases/*/*.feature | sort -u`:

    Then the result should be, in any order:

**Exactly one Then phrasing** across all 30 scenarios; no "result
should be empty", no negative-outcome step. This is the golden-arithmetic
justification team-lead flagged:

- Every scenario reaches `resultShouldBe`.
- Every scenario's parsed query is `StatementKind == StatementRead`
  (all queries are `MATCH ... RETURN ...`, no CREATE / DELETE / SET /
  REMOVE / MERGE, no CALL).
- `resultShouldBe` always mints a golden if the query parsed
  (`checkGolden(st)` at `acceptance_test.go:952`), regardless of
  StatementKind.
- `noSideEffects` mints a golden ONLY when `StatementKind == StatementWrite`
  (`acceptance_test.go:971`). For these 30 read scenarios,
  `noSideEffects` is a no-op after the parse-outcome guard — no
  second golden.

Result: exactly 30 goldens minted, one per scenario. See §4.3.

Parse-green claim (all 30 scenarios): confirmed via a scratch probe
against `cypher.New().Parse(strings.NewReader(query))` on every
`executing query:` doc-string extracted from both feature files. All
30 return `nil` error. Query shapes exercised (all Stage-0–14 surface):

- countingSubgraphMatches (11): `MATCH ()--()` / `MATCH (n)--(n)` /
  `MATCH (n)-[r]-(n)` / `MATCH (n)-[r]->(n)` / `MATCH ()-->()` /
  `MATCH ()-[]-()-[]-()` / `MATCH (:A)-->()--()` — all RETURN
  `count(*)` / `count(r)` / `count(DISTINCT r)`.
- triadicSelection (19): `MATCH (a:A)-[:KNOWS]->(b)-->(c) OPTIONAL
  MATCH (a)-[r:KNOWS]->(c) WITH c WHERE r IS NULL RETURN c.name`
  and label / rel-type variants (`(b:X)`, `(c:X)`, `(c:Y)`,
  `[:KNOWS|FOLLOWS]`, `[:FOLLOWS]`, unrestricted `[r]`,
  `WHERE r IS NOT NULL`). Every shape covered by Stage 8 pattern
  widening plus Stage 4 WITH chaining plus Stage 5 undirected/OPTIONAL
  MATCH.

### 4.3 Golden counters

| Counter          | Before | After | Δ   | Reason                                              |
| ---------------- | ------ | ----- | --- | --------------------------------------------------- |
| On-disk goldens  | 3169   | 3199  | +30 | One golden per useCases scenario (§4.2 justification) |

Arithmetic: 3169 + 30 = 3199. Verified by hash-key uniqueness: every
newly-minted golden is keyed by `SHA1(uri\x00name\x00query)[:6]` where
`uri` and `query` are unique per scenario (the two useCases feature
files carry no repeat query text). `TestGoldenOrphans` enforces this
by construction — an orphan golden would fail the guard.

### 4.4 Parser pin counters

| Counter               | Before | After | Δ   | Reason                                    |
| --------------------- | ------ | ----- | --- | ----------------------------------------- |
| `mustParse` entries   | 112    | 112   | 0   | No new parser gate; no new shape exercised |
| `mustReject` entries  | 25     | 25    | 0   | No new sentinel                            |
| `mustRejectGrammar`   | 3      | 3     | 0   | No grammar gate change                     |

Zero authored kill-probes are needed: every axis the useCases corpus
exercises has direct corpus coverage of the same axis in the
already-wired dirs. Adding a mustParse pin for a shape the corpus
already covers verbatim (Cluster-A duplication) violates ADR 0004
"corpus-derived, not authored" — the ADR restricts authored pins to
axes the corpus cannot distinguish.

### 4.5 Sentinel and skiplist counters

| Counter                    | Before | After | Δ   | Reason                                              |
| -------------------------- | ------ | ----- | --- | --------------------------------------------------- |
| Canonical sentinels        | 7      | 7     | 0   | No sentinel added or retired                        |
| `unsupportedSentinels`     | 1      | 1     | 0   | Still `[ErrUnsupportedParameter]`                   |
| `skiplist` entries         | 89     | 89    | 0   | No skiplist entry added or removed                  |
| Declared policy categories | 0      | 8     | +8  | The new `skipCategory` enum (§1.3, §4.6)            |

### 4.6 Complete 89-entry categorization

Every entry maps to exactly one category. Distribution:

| Category                     | Count |
| ---------------------------- | ----- |
| `catRuntimeError`            | 7     |
| `catResultAssertionOnly`     | 12    |
| `catValueBelowBoundary`      | 16    |
| `catGroupingKeySemantic`     | 16    |
| `catBindingKindConflict`     | 16    |
| `catWriteShapeConstraint`    | 12    |
| `catClauseComposition`       | 7     |
| `catSignatureArgCheck`       | 3     |
| **Total**                    | **89** |

Arithmetic: 7 + 12 + 16 + 16 + 16 + 12 + 7 + 3 = 89. Verified.

The full per-entry assignment follows. For each entry, the category
name, TCK (kind, phase, detail) triple, and a one-line cause pinned
to the actual rule.

#### catRuntimeError (7)

| Entry | TCK triple | Cause |
| ----- | ---------- | ----- |
| `[15] Fail when returning properties of deleted nodes` | EntityNotFound / runtime / DeletedEntityAccess | Return2 [15]: engine detects deleted-node access at result evaluation. |
| `[16] Fail when returning labels of deleted nodes` | EntityNotFound / runtime / DeletedEntityAccess | Return2 [16]: same rule via labels(n). |
| `[17] Fail when returning properties of deleted relationships` | EntityNotFound / runtime / DeletedEntityAccess | Return2 [17]: edge analogue. |
| `[7] Failing when deleting connected nodes` | ConstraintVerificationFailed / runtime / DeleteConnectedNode | Delete1 [7]: cardinality rule on node-with-edges without DETACH. |
| `[10] Failing when setting a list of maps as a property` | TypeError / runtime / InvalidPropertyType | Set1 [10]: property store does not admit list-of-maps. |
| `[17] Fail on merging node with null property` | SemanticError / runtime / MergeReadOwnWrites | Merge1 [17]: null property in MERGE pattern matches nothing at runtime. |
| `[29] Fail on merging relationship with null property` | SemanticError / runtime / MergeReadOwnWrites | Merge5 [29]: edge analogue of Merge1 [17]. |

#### catResultAssertionOnly (12)

| Entry | TCK triple | Cause |
| ----- | ---------- | ----- |
| `[2] Fail when using RETURN * without variables in scope` | SyntaxError / compile-time / NoVariablesInScope | RETURN * expands to zero columns; a scope/result-set rule. |
| `[10] Fail when returning multiple columns with same name` | SyntaxError / compile-time / ColumnNameConflict | Two projections aliased to the same name — result-shape rule. |
| `[5] Failing when UNION has different columns` | SyntaxError / compile-time / DifferentColumnsInUnion | UNION column compatibility not modelled (ADR 0003). |
| `[5] Failing when UNION ALL has different columns` | SyntaxError / compile-time / DifferentColumnsInUnion | UNION ALL variant. |
| `[4] Fail when forwarding multiple aliases with the same name` | SyntaxError / compile-time / ColumnNameConflict | WITH duplicate-alias rule. |
| `[5] Fail when not aliasing expressions in WITH` | SyntaxError / compile-time / NoExpressionAlias | Parser synthesises Name from source text; must-alias check has nothing to compare. |
| `[8] Fail on sorting by any number of undefined variables in any position #Example: out of scope` | SyntaxError / compile-time / UndefinedVariable | ORDER BY names a variable not in projection's set. |
| `[8] Fail on sorting by any number of undefined variables in any position #Example: never defined` | SyntaxError / compile-time / UndefinedVariable | Outline expansion 2. |
| `[8] Fail on sorting by any number of undefined variables in any position #Example: mixed` | SyntaxError / compile-time / UndefinedVariable | Outline expansion 3. |
| `[46] Fail on sorting by an undefined variable #Example: out of scope` | SyntaxError / compile-time / UndefinedVariable | with-orderBy variant, outline 1. |
| `[46] Fail on sorting by an undefined variable #Example: never defined` | SyntaxError / compile-time / UndefinedVariable | with-orderBy variant, outline 2. |
| `[13] Fail when sorting on variable removed by DISTINCT` | SyntaxError / compile-time / UndefinedVariable | Sort key names a variable removed by DISTINCT. |

#### catValueBelowBoundary (16)

| Entry | TCK triple | Cause |
| ----- | ---------- | ----- |
| `[5] SKIP with an expression that depends on variables should fail` | SyntaxError / compile-time / NonConstantExpression | Value-constraint on SKIP RHS. |
| `[7] Negative SKIP should fail` | SyntaxError / compile-time / NegativeIntegerArgument | Literal-value check. |
| `[9] Floating point SKIP should fail` | SyntaxError / compile-time / InvalidArgumentType | Literal-type check. |
| `[10] Fail when using non-constants in SKIP` | SyntaxError / compile-time / NonConstantExpression | with-skip-limit variant. |
| `[11] Fail when using negative value in SKIP` | SyntaxError / compile-time / NegativeIntegerArgument | with-skip-limit variant. |
| `[9] Fail when using non-constants in LIMIT` | SyntaxError / compile-time / NonConstantExpression | LIMIT analogue. |
| `[12] Fail when using negative value in LIMIT 1` | SyntaxError / compile-time / NegativeIntegerArgument | LIMIT analogue. |
| `[13] Fail when using negative value in LIMIT 2` | SyntaxError / compile-time / NegativeIntegerArgument | LIMIT analogue #2. |
| `[16] Fail when using floating point in LIMIT 1` | SyntaxError / compile-time / InvalidArgumentType | LIMIT analogue. |
| `[17] Fail when using floating point in LIMIT 2` | SyntaxError / compile-time / InvalidArgumentType | LIMIT analogue #2. |
| `[6] Negative parameter for SKIP should fail` | SyntaxError / runtime / NegativeIntegerArgument | Parameter runtime-value check on SKIP. |
| `[8] Floating point parameter for SKIP should fail` | SyntaxError / runtime / InvalidArgumentType | Parameter runtime-type check on SKIP. |
| `[10] Negative parameter for LIMIT should fail` | SyntaxError / runtime / NegativeIntegerArgument | LIMIT analogue. |
| `[11] Negative parameter for LIMIT with ORDER BY should fail` | SyntaxError / runtime / NegativeIntegerArgument | LIMIT + ORDER BY analogue. |
| `[14] Floating point parameter for LIMIT should fail` | SyntaxError / runtime / InvalidArgumentType | LIMIT parameter analogue. |
| `[15] Floating point parameter for LIMIT with ORDER BY should fail` | SyntaxError / runtime / InvalidArgumentType | LIMIT + ORDER BY parameter analogue. |

#### catGroupingKeySemantic (16)

| Entry | TCK triple | Cause |
| ----- | ---------- | ----- |
| `[15] Fail on aggregation in WHERE` | SyntaxError / compile-time / (unspecified) | Aggregate in predicate position — engine grouping rule. |
| `[8] Fail if not projected variables are used inside an expression which contains an aggregation expression` | SyntaxError / compile-time / AmbiguousAggregationExpression | Grouping-key correctness (return context). |
| `[9] Fail if more complex expression, even if projected, are used inside expression which contains an aggregation expression` | SyntaxError / compile-time / AmbiguousAggregationExpression | Grouping-key correctness (complex expr). |
| `[20] Fail if not returned variables are used inside an expression which contains an aggregation expression` | SyntaxError / compile-time / AmbiguousAggregationExpression | Grouping-key correctness (with context). |
| `[21] Fail if more complex expressions, even if returned, are used inside expression which contains an aggregation expression` | SyntaxError / compile-time / AmbiguousAggregationExpression | Grouping-key correctness (with context, complex expr). |
| `[14] Aggregates in aggregates` | SyntaxError / compile-time / NestedAggregation | Nested aggregation semantic rule. |
| `` `[15] Using `rand()` in aggregations` `` | SyntaxError / compile-time / NonConstantExpression | rand() impurity in aggregate. |
| `[14] Fail on aggregation in ORDER BY after RETURN` | SyntaxError / compile-time / InvalidAggregation | Aggregate in ORDER BY without projected aggregate. |
| `[25] Fail on sorting by an aggregation` | SyntaxError / compile-time / InvalidAggregation | ORDER BY at WITH position. |
| `[13] Fail on sorting by a non-projected aggregation on a variable` | SyntaxError / compile-time / InvalidAggregation | Aggregate on non-projected var in ORDER BY. |
| `[14] Fail on sorting by a non-projected aggregation on an expression` | SyntaxError / compile-time / InvalidAggregation | Aggregate on non-projected expr in ORDER BY. |
| `[4] Fail if not returned variables are used inside an order by item which contains an aggregation expression` | SyntaxError / compile-time / AmbiguousAggregationExpression | ORDER BY analogue of return context. |
| `[5] Fail if more complex expressions, even if returned, are used inside an order by item which contains an aggregation expression` | SyntaxError / compile-time / AmbiguousAggregationExpression | ORDER BY complex-expr analogue. |
| `[19] Fail if not projected variables are used inside an order by item which contains an aggregation expression` | SyntaxError / compile-time / AmbiguousAggregationExpression | ORDER BY at WITH position. |
| `[20] Fail if more complex expressions, even if projected, are used inside an order by item which contains an aggregation expression` | SyntaxError / compile-time / AmbiguousAggregationExpression | ORDER BY at WITH complex-expr. |
| `[16] In-query procedure call should fail if one of the argument expressions uses an aggregation function` | SyntaxError / compile-time / InvalidAggregation | Aggregate in CALL argument (same family as agg-in-WHERE). |

#### catBindingKindConflict (16)

| Entry | TCK triple | Cause |
| ----- | ---------- | ----- |
| `[29] Fail when re-using a relationship in the same pattern` | SyntaxError / compile-time / RelationshipUniquenessViolation | Runtime relationship-uniqueness rule. |
| `[11] Fail when matching a node variable bound to a value` | SyntaxError / compile-time / VariableTypeConflict | WITH-projected value re-bound as node; scenario outline (3 pickles same name). |
| `[13] Fail when matching a relationship variable bound to a value` | SyntaxError / compile-time / VariableTypeConflict | Edge analogue of [11]. |
| `[30] Fail when using a list or nodes as a node` | SyntaxError / compile-time / VariableTypeConflict | list-of-nodes alias re-bound as node. |
| `[25] Fail when matching a path variable bound to a value` | SyntaxError / compile-time / VariableAlreadyBound | Path variable re-binding; scenario outline. |
| `[13] Fail when creating a node that is already bound` | SyntaxError / compile-time / VariableAlreadyBound | CREATE re-binds MATCH-bound name with a label. |
| `[14] Fail when creating a node with properties that is already bound` | SyntaxError / compile-time / VariableAlreadyBound | Same with inline properties. |
| `[15] Fail when adding a new label predicate on a node that is already bound 1` | SyntaxError / compile-time / VariableAlreadyBound | New-label predicate on bound var (variant 1). |
| `[16] Fail when adding new label predicate on a node that is already bound 2` | SyntaxError / compile-time / VariableAlreadyBound | Variant 2. |
| `[17] Fail when adding new label predicate on a node that is already bound 3` | SyntaxError / compile-time / VariableAlreadyBound | Variant 3. |
| `[18] Fail when adding new label predicate on a node that is already bound 4` | SyntaxError / compile-time / VariableAlreadyBound | Variant 4. |
| `[19] Fail when adding new label predicate on a node that is already bound 5` | SyntaxError / compile-time / VariableAlreadyBound | Variant 5. |
| `[23] Fail when creating a relationship that is already bound` | SyntaxError / compile-time / VariableAlreadyBound | Edge re-binding. |
| `[15] Fail when merge a node that is already bound` | SyntaxError / compile-time / VariableAlreadyBound | MERGE analogue of CREATE re-bind. |
| `[22] Fail when imposing new predicates on a variable that is already bound` | SyntaxError / compile-time / VariableAlreadyBound | MERGE label predicate on bound var. |
| `[26] Fail when merging relationship that is already bound` | SyntaxError / compile-time / VariableAlreadyBound | MERGE edge re-binding. |

#### catWriteShapeConstraint (12)

| Entry | TCK triple | Cause |
| ----- | ---------- | ----- |
| `[18] Fail when creating a relationship without a type` | SyntaxError / compile-time / NoSingleRelationshipType | CREATE requires exactly one edge type. |
| `[19] Fail when creating a relationship without a direction` | SyntaxError / compile-time / RequiresDirectedRelationship | CREATE requires directed edge. |
| `[20] Fail when creating a relationship with two directions` | SyntaxError / compile-time / RequiresDirectedRelationship | Bidirectional CREATE. |
| `[21] Fail when creating a relationship with more than one type` | SyntaxError / compile-time / NoSingleRelationshipType | Multi-type CREATE edge. |
| `[22] Fail when creating a variable-length relationship` | SyntaxError / compile-time / CreatingVarLength | Hop-range on CREATE edge. |
| `[8] Failing when deleting a label` | SyntaxError / compile-time / InvalidDelete | DELETE target is a label predicate, not a node/edge. |
| `[5] Failing when deleting a relationship type` | SyntaxError / compile-time / InvalidDelete | DELETE target is a rel-type predicate. |
| `[9] Failing when deleting an integer expression` | SyntaxError / compile-time / InvalidArgumentType | DELETE target is `1 + 1`. |
| `[23] Fail when merging relationship without type` | SyntaxError / compile-time / NoSingleRelationshipType | MERGE variant of Create2 [18]. |
| `[24] Fail when merging relationship without type, no colon` | SyntaxError / compile-time / NoSingleRelationshipType | MERGE variant, colon-omitted. |
| `[25] Fail when merging relationship with more than one type` | SyntaxError / compile-time / NoSingleRelationshipType | MERGE variant of Create2 [21]. |
| `[28] Fail when using variable length relationship in MERGE` | SyntaxError / compile-time / CreatingVarLength | MERGE variant of Create2 [22]. |

#### catClauseComposition (7)

| Entry | TCK triple | Cause |
| ----- | ---------- | ----- |
| `[18] Fail on projecting a non-existent function` | SyntaxError / compile-time / UnknownFunction | Parser carries no function name in FuncProjection; cannot distinguish unknown from known. |
| `[1] Failing when mixing UNION and UNION ALL` | SyntaxError / compile-time / InvalidClauseComposition | Model records combinator sequence verbatim. |
| `[2] Failing when mixing UNION ALL and UNION` | SyntaxError / compile-time / InvalidClauseComposition | Symmetric variant. |
| `` `[6] Fail for `size()` on pattern predicates` `` | SyntaxError / compile-time / UnexpectedSyntax | Pattern-predicate as function arg — signature-tied semantic rule. |
| `[14] Fail when filtering path with property predicate` | SyntaxError / compile-time / InvalidArgumentType | path.property in WHERE — path has no properties. |
| `[24] Fail on using pattern in right-hand side of SET` | SyntaxError / compile-time / UnexpectedSyntax | Pattern predicate buried in SET RHS — precedence-tower reach. |
| `[3] Full existential subquery with update clause should fail` | SyntaxError / compile-time / InvalidClauseComposition | SET inside EXISTS { ... } — writes-in-subqueries rule. |

#### catSignatureArgCheck (3)

| Entry | TCK triple | Cause |
| ----- | ---------- | ----- |
| `[5] Standalone call to procedure should fail if input type is wrong` | SyntaxError / compile-time / InvalidArgumentType | Stage 14 §4.5: argument-vs-signature type check deferred. |
| `[6] In-query call to procedure should fail if input type is wrong` | SyntaxError / compile-time / InvalidArgumentType | In-query variant. |
| `[11] Standalone call to procedure should fail if implicit argument is missing` | ParameterMissing / compile-time / MissingParameter | Implicit invocation binds args from `$name` params at runtime. |

Sum verification: 7 + 12 + 16 + 16 + 16 + 12 + 7 + 3 = 89.

---

## 5. Staleness audit (Q8 result)

Every skiplist entry was independently audited against the current
parser. Method: for each of the 89 keys, enumerate the matching pickles
across the wired dirs (via `gherkin.Pickles`, the same path the
acceptance suite uses), collect the `there exists a procedure` steps
into a `procsig.Registry` per-scenario (mirroring `initScenario`'s
`sc.sigs` accumulator), and run `cypher.New(WithRegistry(reg)).Parse(...)`
over every `executing query:` doc-string.

**Result:** 89 / 89 keys match a pickle in a wired dir (no orphans —
independently confirms `TestSkiplistOrphans`). 176 / 176 pickle-hits
(Scenario Outline expansions counted individually) parse-accept.

**Zero staleness:** no skiplist entry is silently masking a rejection
the parser now makes.

The audit revealed one probe-methodology subtlety worth recording: the
four Stage-14 CALL entries (`Call1 [11]`, `Call1 [16]`, `Call2 [5]`,
`Call2 [6]`) fail to parse WITHOUT the per-scenario registry — an
empty registry raises `ErrUnknownProcedure`. With the correct
per-scenario registry (parsed from the `there exists a procedure`
background step, identical to the acceptance-suite path) they all
parse-accept. This is a probe artifact, not a real staleness — the
scoring reflects the WITH-registry results, which mirror the
acceptance suite's runtime behaviour.

---

## 6. Files touched

Stage 15 modifies exactly four files:

- `docs/specs/cypher-query-parser-stage-15.md` — this spec (new).
- `internal/query/cypher/acceptance_test.go`:
  - Two entries appended to `readCoreDirs`.
  - One `ctx.Step(...)` registration added (enumerated regex for
    binary-tree-N).
  - One paragraph appended to the `readCoreDirs` comment block.
  - `skipCategory` enum introduced.
  - `skiplist` value type changed from `bool` to `skipCategory`; every
    entry gains a category value; entries regrouped by category with
    section headers.
  - `TestSkiplistCategoryPolicy` test added.
- `internal/query/cypher/testdata/golden/*.golden.json` — 30 new files
  minted (one per useCases scenario); no existing golden mutated.

Zero touches to: `internal/query/**` non-test source files, `internal/procsig/**`,
`internal/schema/**`, resolver, codegen, driver, `docs/adr/*`, any spec
below Stage 15, any `.beads/*`, any `.claude/*`.

---

## 7. Ordering and gates

Ordering:

1. Spec commit (this file).
2. `readCoreDirs` extension + step registration + `Given the (binary-tree-1|binary-tree-2) graph` noop — RED phase: `go test ./internal/query/cypher -run TestNoUndefinedSteps` must fail without the step; then pass with it.
3. `-update` run to mint the 30 goldens.
4. `skiplist` category enum + regrouped map + `TestSkiplistCategoryPolicy` — no scenarios change disposition; the guard is the acceptance criterion for §1.5.
5. Full gates.

Gates (mandatory in this order — every one must pass at the tip of the
branch):

- `go build ./...`
- `go test ./...`
- `go test -race ./internal/query/...`
- `just fmt-check`
- `just lint`
- `just tidy-check`
- `go test ./internal/query/cypher -run 'TestGoldenOrphans|TestSkiplistOrphans|TestSkiplistCategoryPolicy|TestNoUndefinedSteps'`

The final gate is the Stage-15-specific superset — the four guards
that jointly enforce the "true progress meter" invariant.

---

## 8. Acceptance criteria (verbatim from the bead)

- **Zero unwired dirs.** Enforced by §3.1 audit + `TestNoUndefinedSteps`
  (an unknown Given phrasing tripping a corpus-drift alarm) +
  `TestGoldenOrphans` (a golden with no pickle in a wired dir would
  fail).
- **Zero unexplained skiplist entries.** Enforced by
  `TestSkiplistCategoryPolicy` §1.5 assertion (1) — every entry's
  category must be a declared constant, checked at compile time by
  the enum type and at test time by the whitelist check.
- **Pending unambiguously means work remaining.** Enforced by the
  `TestSkiplistCategoryPolicy` §1.5 assertion (2) — no dead category
  slot, so the taxonomy stays honest; combined with the ADR-0007
  bucket-3 categorical accept for `expressions/*`, the pending set is
  the union of documented bucket-3 accepts plus the sole progress-meter
  sentinel (`ErrUnsupportedParameter`).
- **Suite godog green.** Enforced by `go test ./...` — zero failures,
  438 pending scenarios (unchanged), 3459 passed scenarios (+30 from
  useCases), 0 failed.
