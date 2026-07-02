# Stage 7 spec — Cypher query parser: temporal types

The implementation brief for Stage 7 of the Cypher implementation of
`query.Parser`. Seventh model evolution after Stage 6 per ADR 0004 (test-first,
evolving until feature-complete), under the curation discipline of ADR 0003 and
the type-interface boundary of ADR 0005. Stage 7 is the second stage of the
ADR-0007 pre-freeze expansion beyond the read core. It **extends the `Type` sum
with the six openCypher temporal types** — `DATE`, `TIME`, `LOCAL_TIME`,
`DATETIME`, `LOCAL_DATETIME`, `DURATION` — and teaches the Stage-6
rich-expression typer to recognise the temporal constructor functions, the
`duration.between` namespaced accessor, and the temporal-arithmetic operators
(temporal + duration, duration ± duration, duration × number).

This document is a **delta** against Stages 0–6 (referenced individually where
relevant); everything not stated here carries over verbatim. Sections appear
here only where Stage 7 changes something.

Tracking: bead `gqlc-9b4` (GitHub #47). Lands as one graphite branch
(`stage-7-temporal`) with separated commits (spec → model red/green → parser
red/green → unlock+goldens), independently mergeable as a whole: `just test` is
green if this branch lands on `master` alone (AGENTS.md stacked-branch
invariant).

---

## 1. Deliverables

- **Type sum extension.** Six new variants join the sealed `Type` sum on the
  `query` package: `TypeDate`, `TypeTime`, `TypeLocalTime`, `TypeDateTime`,
  `TypeLocalDateTime`, `TypeDuration`. Each mirrors the Stage-6 scalar variants
  exactly: an empty struct, a `String() string` stringer that is the single
  source of the wire tag, an `isType()` marker to keep the sum sealed, and a
  per-variant `MarshalJSON` routing through the existing `marshalType` helper
  so drift is impossible. There are no new smart constructors — every variant
  is an empty struct, same as `TypeBool` / `TypeDate` friends. `TypeList`
  gains no special-casing for temporal elements; a `list<date>` composes
  through the existing element parameterisation.

- **Temporal constructor recognition** — `typing.go` gains a name-based lookup
  `temporalConstructorType(name string) (Type, bool)` that maps the seven
  openCypher temporal constructor names to their result types. The mapping is:

  ```
  date              → TypeDate
  time              → TypeTime
  localtime         → TypeLocalTime
  datetime          → TypeDateTime
  localdatetime     → TypeLocalDateTime
  duration          → TypeDuration
  duration.between  → TypeDuration
  ```

  The `duration.between`, `duration.inSeconds`, `duration.inDays`,
  `duration.inMonths` set are all namespaced calls under the `duration`
  namespace that return a `DURATION`; the lookup handles the two-part name
  (namespace + bare name) as one string with `.` between the parts.

  Function names are matched case-insensitively (mirroring `aggregateFunc` —
  the TCK freely writes `Date(...)`, `DATETIME(...)`). The lookup is the
  same posture Stage-3 aggregates take: a small closed name set the parser
  commits to, right at the type-interface boundary. Everything else — the
  function's *behaviour*, its argument-type validation, its runtime errors —
  stays below the boundary (ADR 0005). `typeAtom`'s function-call arm
  consults the lookup: a matched name yields the temporal result type; an
  unmatched name yields `TypeUnknown` as today.

- **Temporal arithmetic** — `promoteArith` (used by `typeAddSub`) is extended
  with the openCypher temporal-arithmetic rules the type interface commits to:

  ```
  date               + duration → date
  time               + duration → time
  localtime          + duration → localtime
  datetime           + duration → datetime
  localdatetime      + duration → localdatetime
  duration           + <any-temporal> → <that-temporal>   (commutative)
  duration           + duration → duration
  <any-temporal>     - duration → <that-temporal>
  duration           - duration → duration
  ```

  Multiplication and division of a `DURATION` with a number are folded into
  `typeMulDiv` under the same posture (`duration × int → duration`,
  `duration × float → duration`, `duration / int → duration`,
  `duration / float → duration`; the reverse `number × duration` is also
  `duration` — the operator is commutative). Division is direction-
  restricted: `number / duration` has no committed result type and stays
  `TypeUnknown`. Division of a duration by another duration is not
  committed at the parser level (dialect-specific; Neo4j returns a float
  but the openCypher TCK does not require it) and types as `TypeUnknown`.

  Subtraction is likewise direction-restricted: `duration - <temporal-point>`
  is out of scope of the extended arith table and types as `TypeUnknown`
  (there is no legal openCypher shape for "a duration minus a point-in-time";
  the query author asking for a duration between two temporals uses
  `duration.between`, which types correctly via the constructor lookup
  above). Inventing a concrete result type for the reverse direction
  would be strictly worse than `TypeUnknown`, which the resolver can
  upgrade from the schema.

  Every other combination the parser cannot commit to schema-free — a
  temporal *comparison* still yields `TypeBool` via `typeComparison`; a
  cross-temporal subtraction like `date - date` is out of scope of the
  extended arith table and yields `TypeUnknown` (the `duration.between`
  constructor is how the query author asks for a duration between two
  temporals, and it types correctly via the constructor lookup above).

- **Temporal accessors: parser-level posture unchanged.** Accessors like
  `d.year`, `d.quarter`, `d.month`, `d.day`, `d.timezone`, `d.epochSeconds`
  are single-level property lookups whose result type depends on the
  temporal *value* they read from and the accessor *name*. The parser cannot
  commit to the accessor's result type schema-free — the accessor set is
  large (§3), the mapping is per-accessor, and the resolver (post-freeze)
  has the schema to type them precisely from the referenced binding's
  temporal kind. So temporal accessors continue to type as `TypeUnknown`
  under the Stage-6 `typeNonArithmetic` property-lookup rule, honestly:
  the ref is mined for the property, the resolver upgrades.

  This matches ADR 0007's stated posture ("The typed expression surface is
  small") and keeps `TypeUnknown` as the load-bearing fallback for
  schema-dependent typing. The alternative — a per-accessor lookup table
  in the parser — would embed openCypher's temporal accessor semantics in
  the parser (which accessor returns int vs. string vs. duration on which
  temporal kind), duplicating the resolver's job.

- **Parameter mining unchanged.** A `$param` inside a temporal-typed
  expression (arithmetic or otherwise) records an `ExprUse` on the
  enclosing rich expression, exactly as Stage 6 §4. The `enclosingType`
  now can be one of the six temporal types when the expression types
  concretely, e.g. `RETURN date() + $delta` yields
  `ExprUse{enclosing: TypeDate, position: ExprInProjection}` for `$delta`
  (assuming the arithmetic types to `TypeDate` via `date + duration =
  date`, which requires the parser to also know `$delta` types as
  `TypeDuration` — which it does not, so the arithmetic types to
  `TypeUnknown` and the ExprUse enclosingType is `TypeUnknown`; this is
  the honest posture and matches Stage-6 spec §9 for `TypeUnknown`
  as a load-bearing type).

- **No new sentinel; no sentinel retired.** The temporal types extend the
  vocabulary the model can represent, but they introduce no new
  fail-site — every temporal query the model can now type used to type
  as `TypeUnknown` and typed cleanly under the Stage-6 rich-expression
  classifier. The five-sentinel set from Stage 6 is unchanged:
  `ErrUnsupportedClause`, `ErrUnsupportedPattern`,
  `ErrUnsupportedParameter`, `ErrUnboundVariable`,
  `ErrVariableKindConflict`.

- **Layer-1 corpus** — `readCoreDirs` gains `expressions/temporal` (the
  eleven Stage-6 expression dirs remain; the ADR-0007 direction is
  a growing dir list). The wired dir contains ten `Temporal1..Temporal10`
  feature files, ~89 scenario outlines across constructor construction
  from a map / from a string, projection from other temporals, storage
  (write-clause), component accessors, string rendering, comparison,
  arithmetic, truncation, and duration-between. Every scenario that
  goes through a temporal constructor at RETURN position — even the
  ones that touch `WITH v.date AS d` and later access `d.year` — parses
  cleanly under Stage 7 or under Stage 6's pre-existing typing rules:
  the constructor's return type is now a concrete temporal type
  (whichever), and accessors on it stay `TypeUnknown` per the accessor
  posture above.

  Scenarios that use a Stage-6+7 out-of-scope clause (`UNWIND`, write
  clauses in the query proper) stay PENDING via their surviving
  sentinels. Scenarios asserting a runtime value or error (bucket 3,
  ADR 0007) ride the existing bucket-3 categorical accept-and-defer for
  the `expressions/` dirs (`isBucketThreeDir` still matches).

- **Skiplist** — the `expressions/temporal` dir does NOT get per-scenario
  skiplist entries. Same rationale as Stage 6 (spec §6): the ADR-0007
  categorical bucket-3 rule applies uniformly to every runtime /
  value-level error scenario in the `expressions/` dirs; enumerating them
  per-name would grow the skiplist by many entries for zero
  categorisation gain.

  If a bucket-3 detail token surfaces that isn't in the existing
  `isBucketThreeError` whitelist — the temporal TCK scenarios often
  cite `InvalidArgumentValue` or the like — the whitelist gains it (see
  §5). The whitelist entry is scoped to genuinely runtime/value-level
  concerns; parse-shape SyntaxError details are not added.

- **Layer-2 pins** — new `mustParse` cases for the Stage-7 shapes:

  - `RETURN date('2024-01-01') AS d` — constructor from string,
    result type `TypeDate`, ExprProjection (the function-call atom
    now types concretely via the constructor lookup, so it falls
    through to the rich-expression classifier because the bare-atom
    classifier's `FuncProjection` arm would drop the temporal type —
    see §4).
  - `RETURN duration('P1D') AS d` — constructor of `TypeDuration`.
  - `RETURN date() + duration('P1D') AS d` — arithmetic
    `date + duration → date`. ExprProjection, `TypeDate`.
  - `RETURN duration('P1D') * 3 AS d` — duration × scalar,
    `TypeDuration`. ExprProjection.
  - `RETURN date().year` — a property lookup on a temporally-typed
    atom, result type `TypeUnknown` per the accessor posture, ref
    mined for `year`. ExprProjection.

  No `mustReject` case is added — Stage 7 introduces no new sentinel.
  `TestSentinelReachability` remains against the five-sentinel set.

- **Docs inline** — this spec; CONTEXT.md gets `Temporal type` entries
  (six variants) and a revised `Type` entry noting the expanded
  vocabulary; ADR 0003's amendment notes gain a Stage-7 line
  ("the curated Type sum now carries the six openCypher temporal
  scalars; temporal arithmetic types under a small closed rule set").

Nothing downstream of the parser is built (no resolver, no codegen) —
ADR 0004. The resolver's use of the temporal `Type` variants (upgrading
accessors, unifying `TypeUnknown` propagated from mixed operands) is
resolver work and out of scope; the existing gqlc-lqm-style follow-up
bead covers it.

---

## 2. Why one atomic cycle

Adding six variants to the `Type` sum, teaching `typing.go` the temporal
constructor lookup and the arithmetic promotion table, and wiring the
`expressions/temporal` dir is one restructure of the parser's type
inference. Splitting the sum change from the parser change would leave
six new type values unused (dead code) on one branch; splitting the
parser change from the corpus wiring would leave the acceptance suite
in a mid-migration state where the temporal-passing scenarios have no
goldens. Neither split lands independently on `master` (Stage 4 §1's
argument in miniature), so Stage 7 lands as one branch.

Within the branch, the commit inventory (§7) still separates spec from
model from parser from corpus wiring, so review can proceed
incrementally — the constraint is landing-solo, not commit-of-one.

---

## 3. Temporal type vocabulary

Six variants, one for each openCypher temporal type. The distinction
between the two flavours of each (`TIME` / `LOCAL_TIME`, `DATETIME` /
`LOCAL_DATETIME`) is a **zoned vs. non-zoned** distinction the type
interface carries — a `LOCAL_TIME` has no timezone offset; a `TIME`
does. Codegen post-freeze emits distinct method signatures for the two
because the driver's binding representation differs (a `LOCAL_TIME`
maps to `time.Time` with `UTC` and a zero date component in Go, for
example, while a `TIME` also carries the zone). Collapsing them under
one variant would lose this distinction and reintroduce a lossy
representation at the frozen boundary.

Wire encoding — the six new tags, quoted lowercase, join the existing
scalar tags:

```
TypeDate          → "date"
TypeTime          → "time"
TypeLocalTime     → "localtime"
TypeDateTime      → "datetime"
TypeLocalDateTime → "localdatetime"
TypeDuration      → "duration"
```

Composition into `TypeList` works out of the box: `RETURN [date()]`
types as `list<date>` (the element is `TypeDate`; the list literal
walks its elements via `commonType` per Stage 6 §3).

The rich-expression accessor posture (a property lookup on a
temporal-typed value like `d.year`, `d.hour`, `d.timezone`,
`d.epochSeconds`, `d.nanosecondsOfSecond`) continues to type as
`TypeUnknown`: the accessor set is large (Temporal5's accessor scenario
alone reads twenty distinct accessor names off a duration), and the
per-accessor result type depends on both the accessor name and the
temporal kind. The resolver, holding the schema, will type these post-
freeze; the parser records the ref (mined by the existing lookup arm
in `typeNonArithmetic`) and leaves the type honest.

`TypePath` (Stage 8) is unaffected by this stage — a temporal type is
never held inside a `path` variable, and named paths are a
Stage-8-owned addition.

---

## 4. Constructor call arm — bare-atom vs. rich classification

Stage-6 classified a bare function call (a `FuncProjection`) as its own
Projection variant, with `TypeUnknown` unconditionally. Stage 7 wants a
`RETURN date('2024-01-01')` projection to carry `TypeDate` on the wire
— **but the `FuncProjection` variant's `Type()` returns whatever the
listener passed at construction**, so the widening lives naturally
there: `classifyFunction` consults the temporal constructor lookup and
passes the concrete result type when the name matches, `TypeUnknown`
otherwise.

This keeps the bare-atom classifier's output stable — a bare
`date(...)` at RETURN is still a `FuncProjection` (no `ExprProjection`
promotion), just now with a temporal `Type`. The rich-expression
classifier's `typeAtom` also consults the same lookup so a nested
`date(...)` inside arithmetic (like `date() + duration('P1D')`) types
correctly. One name-based lookup, two call sites.

Trade-off recorded: this makes `FuncProjection`'s `Type()` no longer
uniformly `TypeUnknown`. That was Stage-6's honest posture (function
identity is below the boundary, ADR 0005), and the exception is
narrow — only the seven-name temporal constructor set widens it. The
alternative (always classify a temporal constructor as
`ExprProjection`) would move the wire shape for a bare `RETURN date()`
away from `FuncProjection` — a wire regression against Stage 6 for no
type-interface benefit. Widening `FuncProjection`'s `Type()` under a
closed lookup is the cheaper, less invasive choice; if the lookup ever
grows into schema-dependent territory (which it will not, temporal
constructors are pure grammar-level) the resolver takes over.

---

## 5. Corpus and bucket-3 whitelist

`expressions/temporal` is the only dir Stage 7 wires. Its 89 scenario
outlines break down as follows (rough audit, TCK 2024.3):

- `Temporal1` — construct temporals from a map. ~40 outlines. Each
  is `RETURN <ctor>({...}) AS d`. Parse-green under Stage 7; the
  runtime asserts the produced value equals a target — bucket 3.
- `Temporal2` — construct temporals from a string. ~20 outlines.
  Same shape (`RETURN <ctor>('...') AS result`); bucket 3.
- `Temporal3` — project temporals from other temporals. Uses a
  `WITH <ctor>(...) AS other` before RETURN; parse-green under
  Stages 4+6+7 (WITH chain, temporal constructor return type).
- `Temporal4` — store temporals. Writes via `CREATE (...)` — stays
  PENDING via `ErrUnsupportedClause` (Stage 12's scope).
- `Temporal5` — access components. Uses `MATCH (v:Val)` + `WITH
  v.date AS d`; the `d.year` etc. accessors type as `TypeUnknown`
  per the accessor posture (§3), which is honest. Parse-green.
- `Temporal6` — render as string. Uses temporal → `toString(t)`
  function; the `toString` result types as `TypeUnknown` (no
  special-case in the constructor lookup). Parse-green.
- `Temporal7` — comparison. `WITH date(<map>) AS x, date(<map2>) AS
  d RETURN x > d, ...`. Comparison types as `TypeBool` (Stage 6's
  `typeComparison` unchanged). Parse-green.
- `Temporal8` — arithmetic. `WITH date(...) AS x MATCH (d:Duration)
  RETURN x + d.dur AS sum, x - d.dur AS diff`. Arithmetic types
  under the extended `promoteArith` — `x` types as `TypeDate` via
  WITH's exportedTypes / imported map, `d.dur` types as
  `TypeUnknown` (property lookup, resolver-owned), so the arithmetic
  types as `TypeUnknown`. That is honest: the parser knows the LHS
  is a date, the RHS could be a duration but isn't proven so
  schema-free. Parse-green.
- `Temporal9` — truncation. `d.truncate('year', <temp>)` etc.,
  namespaced under the temporal type. Function calls type as
  `TypeUnknown` (only the constructor set is in the lookup);
  parse-green.
- `Temporal10` — `duration.between(<t1>, <t2>)`. Namespaced call;
  the lookup handles the `duration.between` name and returns
  `TypeDuration`. Parse-green.

Scenarios that assert a runtime error (a `TypeError`, an
`InvalidArgumentValue` for a bad component like `{year: 1984, month:
13}`, an `InvalidArgumentType` for a mismatched constructor argument)
ride the bucket-3 categorical accept — `isBucketThreeDir` matches
`/features/expressions/`, so the harness reports PENDING when the
query parses and the scenario expected a rejection with an error
whose kind/detail is bucket-3-eligible.

If a temporal scenario cites a `SyntaxError` detail not currently in
`isBucketThreeError` — the TCK reuses `SyntaxError` broadly, including
for runtime-only rules — the whitelist grows to admit that detail,
with a comment naming the runtime rule it labels. Candidates from a
scan of the ten features (verify against the unlock's actual pending
list): `InvalidUnit`, `UnsupportedTemporalUnit`, `InvalidDate`,
`InvalidDuration` — all value-level rules the engine raises on the
constructor's argument. The whitelist is scoped to genuinely
runtime/value-level details; parse-shape SyntaxError details
(`UnexpectedSyntax`, `InvalidClauseComposition`) are not added.

The unlock commit pins the exact whitelist delta and the exact flip
counts per the Stage-3/5/6 discipline (each stage's spec §6).

### Layer-2 rule

Unchanged directional discipline (Stage 1 §6). Stage 7 adds the
`mustParse` cases §1 names and no `mustReject` cases.
`TestSentinelReachability` continues to check the five-sentinel
canonical set.

---

## 6. Definition of done for Stage 7

1. `stage-7-temporal` lands green and independently mergeable; `master`
   is green if it lands solo.
2. `just test` green: query-package unit tests (the six new `Type`
   variants + their JSON encoding), the cypher-package typing tests
   (temporal constructor lookup, temporal arithmetic promotion), the
   `mustParse` pins, the acceptance / orphan / reachability suites,
   the property tests.
3. Layer-1 godog count rises by the temporal dir's scenario outline
   count (89 scenarios); exact PASS / PENDING / FAIL split pinned by
   the unlock commit's `-update` run and recorded in the bead. Zero
   FAIL is mandatory. Success metric: every temporal scenario whose
   only Stage-7 need was concrete temporal typing now flips PASSING
   or is bucket-3 accepted with a runtime-detail whitelist match; the
   rest stay PENDING via `ErrUnsupportedClause` (writes / `UNWIND`).
4. Documentation: this spec; CONTEXT.md `Temporal type` entries +
   revised `Type` note; ADR 0003 note.
5. Beads: `gqlc-9b4` closed; the resolver-side "temporal accessor
   type inference from the schema" follow-up filed or confirmed to
   cover Stage 7's TypeUnknown load.

---

## 7. Commit inventory (single branch `stage-7-temporal`)

| Commit | Scope |
|--------|-------|
| prep / spec | this spec; CONTEXT.md temporal type entries; ADR 0003 note |
| model (red) | Failing unit tests for the six temporal Type variants: String, MarshalJSON, sealed-sum marker |
| model (green) | Six variants added to `internal/query/type.go`; existing goldens unchanged (no wire shape moves — the temporal tags are new values, not moved keys) |
| parser (red) | Failing `mustParse` cases for temporal constructor projections, temporal arithmetic, duration-scalar arithmetic, temporal accessor |
| parser (green) | `typing.go` gains `temporalConstructorType` lookup and the extended `promoteArith` (temporal + duration, duration × number, etc.); `classifyFunction` consults the constructor lookup so the bare-atom `FuncProjection` variant carries the concrete temporal `Type()` |
| unlock (dir + whitelist + goldens) | `readCoreDirs` gains `expressions/temporal`; new goldens for the parse-green scenarios; bucket-3 whitelist grows with the temporal-only runtime detail tokens if the corpus surfaces any |

Each commit is green in isolation of the ones after it — the model
commits leave `temporalConstructorType` unreferenced until the parser
commits use it; the parser commits leave the new dir unwired until
the unlock commit.

---

## 8. Weakest point (recorded honestly per ADR 0004)

Two weakest points, both recorded.

**Constructor lookup coverage vs. dialect drift.** The constructor set
is closed and small at the openCypher standard level (`date`, `time`,
`localtime`, `datetime`, `localdatetime`, `duration`,
`duration.between`), but dialects add utility names — Neo4j has
`duration.inDays`, `duration.inSeconds`, `duration.inMonths`, and a
handful of `duration.<truncate>` variants. Stage 7 commits to the
standard set only. A dialect-specific constructor call at RETURN types
as `TypeUnknown`, which is honest (a query author who calls
`duration.inDays(...)` will see the resolver upgrade the type against
the driver's known return type post-freeze), but it does mean the
progress meter for the ninth Temporal9 feature stays at
`TypeUnknown` for `d.truncate(...)`. The mitigation is exactly the
Stage-3 aggregate posture: the lookup is a small closed name set the
freeze locks against openCypher, and dialect extensions ride
`TypeUnknown`, which the resolver types from the schema/driver
knowledge.

**Temporal arithmetic without explicit duration proof.** The parser's
`promoteArith` needs to know both sides' concrete types to fold
`date + duration → date`. In the common case where the RHS is a
property lookup (`x + d.dur`), the RHS types as `TypeUnknown` and the
result also collapses to `TypeUnknown` — even though a human reader
knows the query is well-typed. This is exactly the same posture
Stage 6 §9 names for `RETURN 1 + p.age`: `TypeUnknown` on the wire,
the resolver reconstructs from the schema (`d.dur`'s property is a
`DURATION` by the label's schema; the arithmetic result type is
recovered). The Stage-6 spec's mitigation carries: the parser hands
off the original text (ADR 0005) and its computed type; the resolver
walks the expression tree at generation time to refine `TypeUnknown`
against the schema. If the resolver ever proves it needs the parser
to model temporal arithmetic more aggressively (a `TypeUnknown` on
the wire but the resolver would benefit from a "this is a temporal
addition" marker), that is a Stage-6-style widening: one field on
`ExprProjection`, no restructure. Not needed now.

The lesser risks, recorded for completeness:

- **`FuncProjection.Type()` no longer uniformly `TypeUnknown`.** The
  constructor lookup widens it for seven names. Anyone consuming
  `FuncProjection.Type()` and expecting `TypeUnknown` now sees a
  concrete temporal type. There is no such consumer today (the
  resolver is not built); adding one after Stage 7 must handle both
  cases. Recorded here so it is not a surprise.
- **Temporal comparison is `TypeBool` regardless of side types.** A
  malformed comparison (`date() > 'string'`) types as `TypeBool` on
  the wire — the type-interface commits to the predicate's shape,
  not its operand well-formedness. The runtime raises on the
  original text (ADR 0005). Same posture as every other comparison
  in Stage 6.
- **`duration / duration` uncommitted.** A duration ratio is a
  numeric quantity at runtime, but the openCypher TCK does not
  require the parser to type it, and dialect behaviour differs
  (Neo4j returns a float; the openCypher standard is silent). The
  parser types it as `TypeUnknown`. Cheap widening later if a
  dialect commits.
