# Stage 2 spec — Cypher query parser: nullability via OPTIONAL MATCH

The implementation brief for Stage 2 of the Cypher implementation of
`query.Parser`. This is the second model evolution after Stage 1 per ADR 0004
(test-first, evolving until feature-complete) and ADR 0006 (nullability is a
static introduction fact in the parser; flow-typing belongs to the resolver).
Stage 2 unlocks `OPTIONAL MATCH` and marks every binding the clause first
introduces as nullable.

This document is a **delta** against
[Stage 0](./cypher-query-parser-stage-0.md) and
[Stage 1](./cypher-query-parser-stage-1.md); everything not stated here
carries over verbatim. Sections appear here only where Stage 2 changes
something.

---

## 1. Deliverables

Stage 2 lands across three branches in the graphite stack (see §8 for the
inventory). The combined deliverables are:

- **Model evolution** — `Binding` interface gains `Nullable() bool`;
  `NodeBinding` and `EdgeBinding` each gain an unexported `nullable bool`
  field; paired constructors `NewNullableNodeBinding` /
  `NewNullableEdgeBinding` populate it. `MarshalJSON` emits
  `"nullable": true|false` on every binding. All 63 existing golden
  snapshots regenerated to carry `"nullable": false`
  (`internal/query/query.go`,
  `internal/query/cypher/testdata/golden/**`).
- **Parser change** — `EnterOC_Match` no longer rejects OPTIONAL; it
  threads an `optional bool` through `collectPattern → collectPatternElement
  → collectNode / collectEdge → mergeBinding`. `rawBinding` grows a
  `nullable` field; `toBinding()` picks the
  `NewNullable*` constructor when set. The flag is honoured only on **first
  introduction** of a variable; later re-uses leave it alone (ADR 0006)
  (`internal/query/cypher/listener.go`,
  `internal/query/cypher/pattern.go`,
  `internal/query/cypher/build.go`).
- **Layer-1 corpus** — no new directory in `readCoreDirs`; Match7's 31
  OPTIONAL MATCH scenarios were already in the corpus, pending via
  `ErrUnsupportedClause: OPTIONAL MATCH`. The rejection's removal moves 14
  previously-pending scenarios to passing across `clauses/match/Match7.feature`
  and `clauses/match-where/MatchWhere6.feature`. Skiplist unchanged: every
  unlocked scenario the parser accepts parses clean. Counts move from
  315 / 0 / 194 (passed / failed / pending) to 329 / 0 / 180.
- **Layer-2 pin update** — the OPTIONAL MATCH `mustReject` case (verbatim
  Match3 [27]) is replaced by a `with clause` case using the same verbatim
  query: it now rejects via WITH (still Stage 4), still exercising
  `ErrUnsupportedClause` at the new fail-site. Two new `mustParse` cases
  pin canonical OPTIONAL MATCH shapes (verbatim Match7 [1] and [2])
  (`internal/query/cypher/parser_test.go`).
- **Sentinel doc trim** — `ErrUnsupportedClause`'s docstring drops
  OPTIONAL MATCH from its list of rejected clauses
  (`internal/query/cypher/errors.go`).

Nothing downstream of the parser is built (no resolver, no codegen) —
ADR 0004.

---

## 2. Model delta

`Binding` becomes:

```
Binding ::= NodeBinding | EdgeBinding             (closed; isBinding() unexported)

Binding.Kind() graph.EntityKind                   (carried over)
Binding.Nullable() bool                           (new, ADR 0006)
```

- `Nullable()` is on the **interface**, not just on each variant: every
  binding answers the question, anonymous-or-not. Uniform rule.
- The field is **unexported** on each variant; the paired constructors are
  the only writer. The non-null constructors (`NewNodeBinding`,
  `NewEdgeBinding`) are unchanged — existing call sites don't grow a `false`
  parameter; the new constructors delegate to them for invariant
  enforcement and set the flag.
- An anonymous edge introduced in `OPTIONAL MATCH` still carries the flag,
  even though no `Ref` will ever read it. The rule is one rule for every
  binding the clause introduces.

---

## 3. Accept/reject delta

| Construct                                  | Stage 1 | Stage 2                              |
|--------------------------------------------|---------|--------------------------------------|
| `OPTIONAL MATCH (...)` (clause)            | reject  | accept → bindings flagged `Nullable` |
| Re-use of OPTIONAL-introduced variable in a later required `MATCH` | n/a     | the binding stays `Nullable` (no flow demotion — ADR 0006) |
| `WITH`, `UNION`, write clauses, UNWIND, CALL | reject | reject (unchanged — `ErrUnsupportedClause`) |

The OPTIONAL MATCH row is the only behavioural change. The second row pins
the **conservative parser** stance: flow-typing (`b` becomes effectively
non-null when a later required clause references it) is the resolver's
job, not the parser's. Tracked: `gqlc-lqm`.

**Unchanged from Stage 1:**

- Every other clause-rejection sentinel (RETURN *, multi-type
  relationships, undirected, unbound variable, variable kind conflict).
- Every parameter-mining rule (D1, D2, D3, D4 from the Stage 1 spec).
- Property-mining inside an OPTIONAL clause's `WHERE` is unchanged: the
  WHERE introduces no bindings, so `optional` does not flow into
  `mineWhere`; parameter uses pair with their referenced bindings the
  same way regardless of clause nullability.

---

## 4. The first-introduction rule (ADR 0006)

A binding is `Nullable` **iff its variable is first introduced inside an
`OPTIONAL MATCH` clause's pattern.** Anonymous edges (no variable) take
the nullability of the clause they appear in.

Consequences pinned by Layer-2 `mustParse` cases:

- **Match7 [1]** `OPTIONAL MATCH (n) RETURN n` — `n` is nullable.
- **Match7 [2]** `MATCH (n) OPTIONAL MATCH (n)-[:NOT_EXIST]->(x) RETURN n, x`
  — `n` is non-nullable (introduced in the required `MATCH`); the
  anonymous `:NOT_EXIST` edge and `x` are both nullable (first introduced
  in the OPTIONAL clause). The anonymous edge carries the flag even
  though no `Ref` reads it.

**The parser never demotes.** A binding first introduced in OPTIONAL and
then re-referenced (as an endpoint, as a bare pattern, or in WHERE) keeps
its `Nullable: true`. Demotion is a resolver capability — see ADR 0006
and `gqlc-lqm`. Codegen is conservative until the resolver lands flow-
typing; the conservative API can be relaxed non-breakingly later.

---

## 5. Wire format (JSON shapes)

Every `NodeBinding` and `EdgeBinding` JSON object gains a `"nullable"`
boolean field, always emitted (no `omitempty`), matching the existing
always-emit convention (`"labels": null`, `"variable": ""`):

```
NodeBinding   →  {"kind": "node", "variable": "...", "labels": ..., "nullable": false|true}
EdgeBinding   →  {"kind": "edge", "variable": "...", "labels": ..., "source": ..., "target": ..., "nullable": false|true}
```

The field is the last on the object so existing fields keep their order
in the goldens. All 63 Stage-0/Stage-1 goldens were regenerated to add
`"nullable": false` on every binding (the model branch's only diff
outside of `internal/query/`). New goldens for the 14 newly-unlocked
Match7 / MatchWhere6 scenarios were created on the unlock branch.

`Endpoint` shapes (`VarEndpoint`, `InlineEndpoint`) are **unchanged** —
nullability lives on the binding, never on the endpoint reference.

---

## 6. Test corpus and skiplist

**`readCoreDirs` is unchanged.** Stage 1 had widened it by adding
`clauses/return-skip-limit`; Stage 2 does not widen it because every
OPTIONAL MATCH scenario already lives inside the existing dirs
(`clauses/match/Match7.feature` and a handful in
`clauses/match-where/MatchWhere6.feature`).

**Skiplist is unchanged.** Every newly-unlocked scenario the parser
accepts parses clean — no B1 (value-constraint) violations, no novel
fail-sites. The 14 scenarios moving to PASSING are pure parser-accept
wins; the remaining ~17 OPTIONAL MATCH scenarios in Match7 stay PENDING
via the existing `ErrUnsupportedClause` (for `WITH`/`UNION`),
`ErrUnsupportedProjection` (for aggregation in RETURN), or
`ErrUnsupportedPattern` (for variable-length `[*]`).

`TestSkiplistOrphans` stays green: no new entries to orphan.

### Layer-2 rule

Stage 1's directional rule applies unchanged:

- **Accept-path** (`mustParse`) cases come VERBATIM from the corpus. Stage 2
  adds two: Match7 [1] (the minimal nullable-node shape) and Match7 [2]
  (the reuse rule).
- **Reject-path** (`mustReject`) cases come VERBATIM where the corpus
  exercises the fail-site, else `AUTHORED`. Stage 2 replaces the
  OPTIONAL-MATCH case with a `with clause` case using the same verbatim
  Match3 [27] query: the OPTIONAL token is now accepted, but the trailing
  WITH still fires `ErrUnsupportedClause`. **Net Layer-2 change: −1
  mustReject (the OPTIONAL one), +1 mustReject (the WITH-firing one,
  same verbatim query, different reason), +2 mustParse.**

`TestSentinelReachability` stays green: `ErrUnsupportedClause` still has
a `mustReject` case via the renamed entry.

---

## 7. Definition of done for Stage 2

Stage 2 closes when all of the following hold:

1. Three substantive branches merge to master in order
   (`stage-2-prep` → `stage-2-model` → `stage-2-unlock`), plus this
   docs branch (`stage-2-docs`). Each branch must leave `master` green
   when merged solo (the AGENTS.md stacked-branch invariant).
2. `just test` is green: query-package unit tests (constructors,
   accessor, JSON shape, property-based), cypher-package
   `TestMustParse` / `TestMustReject` / `TestReadCoreAcceptance` /
   `TestNoUndefinedSteps` / `TestSkiplistOrphans` / `TestSentinelReachability`.
3. Layer-1 godog reports 329 passed / 0 failed / 180 pending (up from
   Stage 1's 315 / 0 / 194).
4. Documentation deliverables landed: ADR 0006 (stage-2-prep), CONTEXT.md
   "Nullable" entry (stage-2-prep), AGENTS.md "Workflow: stacked branches"
   section (stage-2-prep), this spec doc (stage-2-docs).
5. Beads: `gqlc-s2t` closed. `gqlc-lqm` (resolver-side flow-typing) filed
   with dependencies on Stage 14 + resolver-exists + Stage 4 clause
   structure.

---

## 8. Closing inventory

| Branch              | Scope                                                          |
|---------------------|----------------------------------------------------------------|
| `stage-2-prep`      | ADR 0006 + CONTEXT.md `Nullable` entry + AGENTS.md stacked-branch workflow section + beads (`gqlc-cta` description grows resolver-API note; `gqlc-lqm` filed) |
| `stage-2-model`     | `Nullable()` on `Binding`, paired constructors, JSON tag, all 63 existing goldens regenerated |
| `stage-2-unlock`    | listener accepts OPTIONAL MATCH, threads `optional`, picks `NewNullable*`; Layer-2 mustReject swap + mustParse adds; 14 new Match7/MatchWhere6 goldens; `ErrUnsupportedClause` docstring trim |
| `stage-2-docs`      | this spec doc; close `gqlc-s2t` |

Each row is a trace path for future readers walking from this spec back
to the specific code changes.
