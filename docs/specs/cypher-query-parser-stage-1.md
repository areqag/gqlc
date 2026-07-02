# Stage 1 spec — Cypher query parser: non-property-typed parameters

The implementation brief for Stage 1 of the Cypher implementation of
`query.Parser`. This is the first model evolution after Stage 0 per ADR 0004,
implemented test-first. Stage 1 unlocks bare `SKIP $p` / `LIMIT $p` as
non-property-typed parameter uses — parameters whose type comes from the
clause slot they sit in, rather than from a binding property they pair against.

This document is a **delta** against [Stage 0](./cypher-query-parser-stage-0.md);
everything not stated here carries over verbatim. Sections appear here only
where Stage 1 changes something.

---

## 1. Deliverables

Stage 1 lands across five cycles (see §8 for the inventory). The combined
deliverables are:

- **Model evolution** — `query.Use` sealed sum (`PropertyUse | ClauseSlotUse`),
  `query.ClauseSlot` enum (`ClauseSlotSkip | ClauseSlotLimit`),
  `query.Parameter.Uses` retyped from `[]Ref` to `[]Use`
  (`internal/query/query.go`).
- **Parser change** — `mineClauseSlotParameter` accepts bare `$p` in `SKIP` and
  `LIMIT` as a `ClauseSlotUse`; rejects any other shape (`$p + 1`, `f($p)`)
  via the same helper's `findParameters > 0` branch
  (`internal/query/cypher/expr.go`).
- **Layer-1 corpus widening** — `clauses/return-skip-limit/` added to
  `readCoreDirs`; 18-entry skiplist with a three-group taxonomy (legacy
  pattern-semantics, literal-value, parameter-value); `TestSkiplistOrphans`
  guards against TCK rename/stale entries (`internal/query/cypher/acceptance_test.go`).
- **Layer-2 rule update** — accept- and reject-path discipline split: accept
  cases stay verbatim corpus; reject cases may be `AUTHORED` when no corpus
  query exercises the fail-site. One pinned authored case for the cycle-1/2
  fail-site (non-bare `$p` in `SKIP`/`LIMIT`) (`internal/query/cypher/parser_test.go`).

Nothing downstream of the parser is built (no resolver, no codegen) — ADR 0004.

---

## 2. Model delta (Cluster D evolution)

`query.Parameter.Uses` changes from `[]Ref` to `[]Use`, where `Use` is a
sealed sum:

```
Use = PropertyUse | ClauseSlotUse        (closed; isUse() unexported)
ClauseSlot = ClauseSlotSkip | ClauseSlotLimit   (int-backed, stringer)
```

- `PropertyUse` wraps a `Ref` (the existing case). Mirrors the WHERE-side
  parameter use from Stage 0; constructor is total since the parser only
  builds a `PropertyUse` after `propertyRefFromAddSub` has already validated
  the `Ref`'s variable.
- `ClauseSlotUse` carries a `ClauseSlot` and no other data. Constructor is
  total since `ClauseSlot` is a closed enum.
- `ClauseSlot` has two stringer methods: `String()` returns the JSON tag
  (`"skip"` / `"limit"`); `ClauseName()` returns the error-message clause
  name (`"SKIP"` / `"LIMIT"`). Two derived names, one source.

Smart constructors (`NewPropertyUse`, `NewClauseSlotUse`) are the only way to
populate either variant — variant fields are unexported. Cycle 1's REJECT/fix
established the discipline: `addParameterUse` is the single chokepoint for
parameter dedup-by-`Name` across both variants, so every caller (property
predicate, inline property map, SKIP/LIMIT clause slot) flows through one
helper. Invariant D4 relaxes (see §4).

---

## 3. Accept/reject delta

| Construct                                  | Stage 0 | Stage 1                              |
|--------------------------------------------|---------|--------------------------------------|
| bare `SKIP $p`                             | reject  | accept → `ClauseSlotUse{Skip}`       |
| bare `LIMIT $p`                            | reject  | accept → `ClauseSlotUse{Limit}`      |
| non-bare `SKIP $p+1` / `LIMIT f($p)`       | reject  | reject (via `mineClauseSlotParameter`'s `findParameters > 0` branch) |

The third row is a delta because the **rejection mechanism** moved, even
though the outcome did not. In Stage 0 these shapes rejected via
`rejectClauseParameter` (the accept-and-ignore guard for clauses whose
expression must not contain a parameter); in Stage 1 SKIP and LIMIT route
through `mineClauseSlotParameter`, which accepts the bare-atom shape and
falls through to the same `findParameters > 0` reject for any other shape.
The cycle-4 AUTHORED mustReject case pins this new path.

**Unchanged from Stage 0:**

- `ORDER BY $p`, every other deferred-parameter construct on a clause whose
  value is accept-and-ignored, parameter-in-arithmetic on the WHERE side,
  `IN $p`, params nested in lists or maps — all still
  `ErrUnsupportedParameter`.
- Every other clause-rejection sentinel (clauses outside the read core,
  RETURN *, multi-type relationships, undirected, unbound variable,
  variable kind conflict) — unchanged.

---

## 4. Cluster D rules amended

**D1 — capture rule.** Stage 0's (a) and (b) carry over verbatim. Stage 1
adds:

  (c) the entire value of a `SKIP` or `LIMIT` clause is a bare `$p` (no
      operators, no enclosing function call) — yields `Use = ClauseSlotUse{slot}`
      where `slot` is `ClauseSlotSkip` or `ClauseSlotLimit`. Any non-bare
      `$p` in the same clause expression is `ErrUnsupportedParameter`.

D2, D3 — unchanged.

**D4 — invariant.** Restated to reflect the sum:

> Every `Parameter` has ≥1 `Use`, and every `Use` is a closed-sum member
> (`PropertyUse` or `ClauseSlotUse`). Mixed-kind uses on one `Parameter`
> are allowed at parse time — the resolver judges type unification
> post-freeze (ADR 0003).

The deferred list shrinks to: parameter-in-arithmetic; bare-predicate
params; param-vs-param/literal; `a.prop IN $p`; params nested in
lists/maps; multi-level property access; a param as a RETURN item;
`ORDER BY …$p…`.

---

## 5. Wire format (JSON shapes)

Two new tagged-union encodings, mirroring the Stage 0 `Binding`/`Endpoint`
pattern (`"kind"` discriminator, one level deep):

```
PropertyUse   →  {"kind": "property",    "variable": "...", "property": "..."}
ClauseSlotUse →  {"kind": "clause-slot", "slot": "skip" | "limit"}
```

The `"slot"` value derives from `ClauseSlot.String()`, the single source the
JSON discriminator follows. The `"kind"` discriminators live as
package-level constants (`useKindProperty`, `useKindClauseSlot`) next to
`endpointKindVar`/`endpointKindInline`.

This is a wire-format change for `Parameter.Uses` elements: any caller that
serialized a Stage 0 `Ref` directly under `Uses` sees the new tagged-union
shape instead. The golden snapshots regenerated in cycle 1 (three
`MatchWhere*` snapshots) and cycle 3 (eleven new `ReturnSkipLimit*`
snapshots) reflect the change. No other shapes moved.

---

## 6. Test corpus and skiplist

`readCoreDirs` widens by one directory:

```
clauses/match
clauses/return
clauses/match-where
clauses/return-skip-limit        ← Stage 1 addition
```

The new directory contains 31 scenarios. The cycle-3 audit classified them:

| Class | Count | Outcome                                                                |
|-------|-------|------------------------------------------------------------------------|
| (a)   |    11 | parse-green; golden snapshot in `internal/query/cypher/testdata/golden/` |
| (b)   |     4 | PENDING via existing `ErrUnsupportedClause` / `ErrUnsupportedProjection` (WITH/UNWIND/aggregation-in-RETURN) |
| (c)   |     0 | true parse-rejection by this parser (none — see §6 audit note)         |
| (d)   |    16 | accept-then-runtime-or-compile-time-value-error → skiplist             |

**Audit note on (c)=0.** Every "compile time SyntaxError" TCK scenario in
this dir is a value-constraint check (negative literal, floating-point
literal, non-constant expression) — the value lives below the type-interface
boundary per B1, so this parser accept-and-ignores. The executed Cypher
text retains the offending value (ADR 0005), so an engine reading the
generated method body raises the same error. None of these are parse-shape
errors.

`TestSkiplistOrphans` guards against TCK rename or stale entries: every
skiplist key must match at least one scenario in `readCoreDirs`. A TCK bump
that renames a scenario surfaces as an explicit test failure rather than
silently un-skipping the scenario.

### Layer-2 rule (cycle-4 split)

The Layer-2 rule (`internal/query/cypher/parser_test.go`) splits by direction:

- **Accept-path** cases (`mustParse`) come VERBATIM from the corpus. The
  hand-built `query.Query` is the regression layer the golden snapshots
  cannot give us, but the shape we pin against must come from a
  committee-authored input — otherwise we would be asserting the shape we
  want against the input we chose to produce it.
- **Reject-path** cases (`mustReject`) come VERBATIM from the corpus where
  the corpus exercises the fail-site; otherwise they are `AUTHORED` with an
  inline `// AUTHORED:` marker naming the fail-site by domain. The sentinel
  taxonomy is ours; the only assertion is ABSENCE of a model, so the
  accept-path's circularity concern does not apply.

Authored `mustReject` cases are bounded: at most one per fail-site, and
only when no verbatim corpus query exercises that fail-site at the pinned
TCK tag. Both rules carry the revisit-on-TCK-bump obligation.

### Layer-2 coverage gaps

`ErrUnsupportedParameter` is reachable by five distinct fail-sites in the
listener. Stage 1 pins the cycle-introduced one via an AUTHORED case
(`"skip non-bare param"` in `mustReject`). The whole-property-map
fail-site is covered verbatim by Match1 [6] (`"unsupported parameter"`
in `mustReject`). The remaining three have no verbatim corpus query at
the pinned TCK tag and remain uncovered:

- `$p` inside an `ORDER BY` expression
- `$p` as a value in an anonymous pattern element's inline map
  (e.g. `(:Label $p)`)
- `$p` in a `WHERE` shape the property miner cannot bind (param-vs-param,
  `IN $p`, nested-in-list)

If a TCK bump adds a verbatim query for any fail-site here, author one
targeted `mustReject` case per the `parser_test.go` Layer-2 rule.

---

## 7. Definition of done for Stage 1

Stage 1 closes when all of the following hold:

1. The Stage 1 PR (this branch's PR) has merged to master, landing cycles
   1-5: `Use` sum + `ClauseSlotUse` + `ClauseSlot` enum with total
   constructors, `mineClauseSlotParameter` wiring both SKIP and LIMIT,
   `return-skip-limit/` in `readCoreDirs` with the 18-entry skiplist,
   `TestSkiplistOrphans`, the cycle-4 Layer-2 rule + AUTHORED `mustReject`
   pin, and the documentation bundle in this cycle.
2. `just test` is green: Layer-1 godog (509 scenarios, 315 passed / 0
   failed / 194 pending), Layer-2 `mustParse`/`mustReject` pins, the
   rapid property tests, and `TestSkiplistOrphans`.
3. `TestSkiplistOrphans` guards the 16 new skiplist entries — green
   against the cycle-3 corpus, and any TCK bump after Stage 1 surfaces
   orphans as explicit failures.
4. Documentation deliverables landed: this spec doc; CONTEXT.md "Use"
   entry + refined "Parameter"; Stage 0 spec §4 C2 Stage 1 note.
5. Both gated-swarm reviewers approved every cycle's plan AND diff.
6. `gqlc-4n2` closes in beads.

---

## 8. Closing inventory

| Cycle | Commit  | Scope                                                          |
|-------|---------|----------------------------------------------------------------|
| 1     | f1e4cb0 | `Use` sum + chokepoint dedup at `addParameterUse` + bare `SKIP $p` |
| 2     | b27dbe5 | bare `LIMIT $p` (one-line wire change after cycle-1's chokepoint) |
| 3     | a9ae6b5 | TCK dir widening + 18-entry skiplist + `TestSkiplistOrphans`   |
| 4     | 0603950 | directional Layer-2 rule + AUTHORED pin for non-bare `SKIP`/`LIMIT $p` |
| 5     | _this_  | Stage 1 spec + CONTEXT.md `Use` entry + Stage 0 §4 C2 Stage 1 note |

Each commit:scope row is a trace path for future readers walking back
from this spec to specific code changes.
