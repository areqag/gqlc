# Model change — OPTIONAL-group membership on Binding

The implementation brief for cycle **gqlc-ay9** of the model-additions
campaign (Cycle 4, after hk0 / fvo / 0ig): an additive
`OptionalGroup int` axis on `query.NodeBinding` and
`query.EdgeBinding`, populated parser-side by the existing OPTIONAL
threading through `collectPattern`, closing the R4 §7.5.3 **Class A**
OPTIONAL-clause-sibling nullability gap
(`docs/specs/resolver-stage-r4.md §7.5.3` items 1 + 3, §7.5.4
**Axis 1**). The axis records which bindings were co-introduced by
the same OPTIONAL MATCH clause; group id `0` is the zero-value wire
default meaning "not in any OPTIONAL group", so every non-OPTIONAL
golden is byte-identical under the campaign's omit-when-zero-value
convention (ADR 0008, hk0 amendment).

This brief is the **contract for the whole gqlc-ay9 cycle**: it spans
the spec PR (this file), the model-change PR (model + parser + ADR 0008
amendment + parser-pin and parser-golden rebaseline; resolver
untouched and green), the resolver-widening PR (group-closure
demotion in `demoteNullableInPlace`; two existing resolver goldens
rebaseline, four new fixtures land), and a docs-errata PR retiring
the "silently under-demoted / gqlc-ay9" rows across the R4–R7 stage
specs. All code PRs land under ADR 0008's later revision
protocol — additive-only, dated amendment, golden rebaseline whose
diff shows only the new surface.

**Class B is out of scope — hard boundary.** The same-Part
second-reference gap (R4 §7.5.3 item 2, §7.5.4 **Axis 2**, bead
**gqlc-5xg**) is a *missing-witness* gap, not a missing-group gap:
`mergeBinding` (`internal/query/cypher/pattern.go:385-401`) discards
every non-first occurrence of a variable within a Part, so no group
axis can resurrect the dropped clause reference. Nothing in this
cycle touches `mergeBinding`'s merge content, adds reference
tracking, or closes item 2. The R4/R5/R6/R7 "Same-Part regime (b)"
rows stay pointed at gqlc-5xg verbatim (§8.6).

---

## 1. Deliverables

Spec cycle (Cycle 1) — this PR:

- `docs/specs/model-change-ay9-optional-group.md` — this file.

Cycle ( 2, follow-up PR):

- `internal/query/query.go` — one additive field `optionalGroup int`
  on `NodeBinding` (tail-append after `nullable`, struct at
  `query.go:313-317`) and on `EdgeBinding` (tail-append after
  `hops`, struct at `query.go:372-380`); three new constructors
  `NewNullableNodeBindingInGroup` / `NewNullableEdgeBindingInGroup`
  / `NewNullableVarLengthEdgeBindingInGroup` (the six existing
  binding constructors at `query.go:321-431` are preserved verbatim;
  the three existing `NewNullable*` forms mint `optionalGroup: 0`);
  two new accessors `OptionalGroup() int` (one per variant); two
  widened `MarshalJSON` structs (`query.go:1616-1623` node,
  `:1630-1641` edge) gaining a trailing
  `OptionalGroup int \`json:"optionalGroup,omitempty"\`` key. §4.1.
- `internal/query/query_test.go` — new tests pinning the InGroup
  constructors' true side, the group<1 rejection, the
  omit-when-zero wire, and the group-carrying wire fragment. The 10
  pre-existing `NewNullable*` pins (3 + 6 + 1 across the three
  ctors) stay verbatim — they pin the preserved group-0 behaviour.
  §5.
- `internal/query/cypher/listener.go` — the `listener` struct
  (`listener.go:31-93`) gains a per-parse `optionalGroupSeq int`
  counter; `EnterOC_Match` (`listener.go:261-270`) mints a fresh
  group id per OPTIONAL clause after the `subqueryDepth` guard.
  §4.2.1.
- `internal/query/cypher/pattern.go` — the `optional bool`
  parameter threaded through `collectPattern` → `collectPatternPart`
  → `collectPatternElement` → `collectNode` / `collectEdge` →
  `mergeBinding` (`pattern.go:37`, `:53`, `:142`, `:187`, `:251`,
  `:385`) is re-typed to `group int` (`0` = required clause;
  `optional := group > 0`); `rawBinding.nullable`
  (`listener.go:164`) is replaced by `rawBinding.optionalGroup int`
  at both literal sites (`pattern.go:297` anonymous edge, `:389`
  mergeBinding first-introduction). All unexported parser-internal
  surface — the ADR 0008 record is untouched. §4.2.2.
- `internal/query/cypher/build.go` — `toBinding`
  (`build.go:263-284`) routes through the InGroup constructors when
  `rb.optionalGroup > 0` (deriving `nullable := optionalGroup > 0`,
  the parser-side invariant). §4.2.3.
- `internal/query/cypher/parser_test.go` — exactly **3 pins flip**
  (the only 3 mustParse pins whose source contains OPTIONAL:
  `"optional match simple"` at `:323-333`, `"optional match reuses
  prior binding"` at `:339-356`, `"count distinct"` at
  `:1188-1198`); their 4 `NewNullable*` invocations (`:327`, `:344`,
  `:349`, `:1192`) become `*InGroup(..., 1)`. The remaining **149**
  Binding-constructor invocations in the file (census §4.3) stay
  verbatim under `require.Equal` — group 0 equals group 0. §4.3.
- Parser goldens (`internal/query/cypher/testdata/golden/`,
  **3199 files** at branch base `56718ff`) — **100 flip** (every
  golden carrying ≥ 1 node/edge binding with `"nullable": true`;
  each such binding gains an `"optionalGroup"` key), **3099
  byte-identical**. §4.4 pins the full 100-file set inline and the
  discovery script that regenerates it.
- `docs/adr/0008-query-model-surface-resolver-api.md` — one dated
  amendment note (2026-07-07, top of file) plus one closed-out
  "Known deferred additions" entry. `OptionalGroup` does **not**
  appear on the ADR's current Known-deferred-additions list
  (verified at branch base — the list names shortestPath, EXISTS
  Use precision, CreateEffect split, and the three adopted axes);
  the amendment adopts the axis directly under the additive rule,
  the same move 0ig made for `CallBinding.Args`. Verbatim text §7.

Resolver-widening cycle (Cycle 3, follow-up PR after the change
PR merges):

- `internal/resolver/resolve.go` — `demoteNullableInPlace`
  (`resolve.go:1446-1469`) widens from the single-pass per-edge
  endpoint walk to the group-closure fixed point: *if a required
  chain proves any member of an OPTIONAL group exists, every member
  of that group demotes*, and a group-demoted OPTIONAL edge becomes
  an effective witness for its own endpoints (the cascade R4
  §4.4.2's rule (i) anticipated). No signature change, no new
  sentinel, no wire change on `ValidatedQuery`. §8.1.
- Resolver goldens — exactly **2 of 122** valid goldens flip:
  `demote_chained_from_required.cypher.validated.golden.json`
  (`p`, `r1` go `nullable: true → false`) and
  `demote_from_anonymous_required_edge.cypher.validated.golden.json`
  (`b` goes `nullable: true → false`). The other 120 valid goldens
  and all 50 invalid fixtures are byte-identical. §8.3 derives each.
- Four new valid fixtures + goldens + `schema.mapping.json` entries
  (all against the existing `social_r4.gql`): arity-5 group,
  two-distinct-groups discrimination, untouched-group no-demotion
  guard, and the two-group cascade. §8.4.

Docs-errata cycle (Cycle 4, rides with or immediately after the
widening PR):

- `docs/specs/resolver-stage-r4.md` — §4.4 "Pass 2" prose, §7.4
  table row `:1185`, §7.5.5 bead-1 language and the
  `demote_chained_from_required` fixture note (`:1534-1541`) get
  closed-out successor prose.
- `docs/specs/resolver-stage-r5.md` — Class A rows at `:610`
  (§4.1.1 table) and `:2311` (§7 capability table), plus the §4.6.2
  paragraph at `:1718-1719`.
- `docs/specs/resolver-stage-r6.md` — Class A row at `:1884` (§7).
- `docs/specs/resolver-stage-r7.md` — Class A row at `:2190` (§7).
- Class B rows (gqlc-5xg) in the same tables stay **verbatim**. §8.6.

Nothing downstream of the resolver is built; codegen consumes the
widened nullability under a future ADR.

---

## 2. Frame — what changes and what stays

### 2.1 The gap in one paragraph

`query.Query` records `Nullable()` per binding as a static fact
about the binding's first-introduction clause
(`internal/query/query.go:354-356` node, `:462-464` edge; ADR 0006)
but not *which bindings were co-introduced by the same OPTIONAL
MATCH clause*. openCypher's row model is left-join: when an OPTIONAL
clause fails, **all** of its bindings are NULL on that row; when any
one of them is proven non-NULL on a surviving row, the clause
matched and **all** of them are non-NULL (R4 §7.5.1). R4's
regime-(a) demotion (`internal/resolver/resolve.go:1446-1469`)
walks per-edge witnesses — a required edge demotes its two named
endpoints — but cannot walk from a demoted binding to its
OPTIONAL-clause siblings, because the flat `part.Bindings` does not
record the sibling relation. Canonical example (R4 §7.5, encoded as
fixture `test/data/resolver/valid/demote_chained_from_required.cypher`):

```
OPTIONAL MATCH (p:Person)-[r1:AUTHORED]->(post:Post) MATCH (post)-[r2:AUTHORED]->(author:Person) RETURN p, r1, post, r2, author
```

Required `r2` proves `post` and `author`; ideal flow-typing also
demotes `p` and `r1` (siblings of the proven `post`). The landed
golden pins the under-approximation: `p` and `r1` carry
`"nullable": true` while `post`, `r2`, `author` carry
`"nullable": false`.

### 2.2 The one wire axis — additive, omit-when-zero-value

**One field on two sum variants**:

- `NodeBinding` gains `optionalGroup int`.
- `EdgeBinding` gains `optionalGroup int`.

Semantics: `0` = "not introduced by any OPTIONAL clause" (every
required-clause binding; the Go zero value; the wire-absent form).
`n ≥ 1` = "introduced by the query's *n*-th OPTIONAL MATCH clause"
— a fresh id minted per OPTIONAL clause, **unique per query**
(§3.3 pins the allocation-scope decision). All bindings first
introduced by one OPTIONAL clause — across all of its
comma-separated pattern parts, named and anonymous alike — share
one id, because openCypher matches the clause's whole pattern
conjunction as a unit: the clause either matches (all bindings
non-NULL) or fails (all NULL).

Parser-side invariant: for node/edge bindings,
`Nullable() == true ⇔ OptionalGroup() ≥ 1` — both facts are set
together at first introduction and only there (`mergeBinding`
honours neither on re-occurrence, matching the ADR 0006
discipline stated at `pattern.go:379-384`).

The JSON encoding is **omit-when-zero-value** (`,omitempty` on an
`int`), following the wire convention hk0 established
(omit-when-false), fvo continued (omit-when-zero-int — the direct
precedent: `Use.part`), and 0ig continued (omit-when-zero-length).
A required binding's wire is byte-identical to the pre-ay9 wire;
only OPTIONAL-introduced bindings gain the key. §4.4 counts the
corpus under this encoding: 100 of 3199 parser goldens flip; 3099
are byte-identical.

### 2.3 Which variants get the axis — and which do not

Only `NodeBinding` and `EdgeBinding` can be OPTIONAL-introduced.
Grounding, variant by variant (all verified at branch base):

- **`PathBinding`** — `Nullable()` is hardcoded `false`
  (`query.go:698-700`): "the OPTIONAL-introduced case flows through
  the member bindings themselves (Stage 8 spec §1.2)". A named path
  in an OPTIONAL clause (`OPTIONAL MATCH p = (a)-[r]->(b)`)
  contributes its group facts via its member node/edge bindings,
  which the demotion table already keys. Adding a group to
  `PathBinding` would contradict the Stage-8 posture for no
  consumer benefit — the axis is **not** added.
- **`UnwindBinding`** — `Nullable()` hardcoded `false`
  (`query.go:803-806`); UNWIND has no OPTIONAL form in the vendored
  grammar. Not added.
- **`CallBinding`** — `nullable` mirrors the *signature's* trailing
  `?` (`query.go:950-952`), a different semantic axis entirely; the
  vendored grammar's only OPTIONAL site is `oC_Match`
  (`internal/grammar/cypher/Cypher.g4:81`; the token's only other
  appearance is the symbolic-name alternative at `:588`). Not
  added.
- The **`Binding` interface stays sealed** as-is — `Kind()`,
  `Nullable()`, `isBinding()` (`query.go:297-306`). `OptionalGroup`
  is a per-variant field-and-accessor concern, the same layering
  hk0/fvo/0ig used ("the Use interface stays sealed at one
  method"); the resolver already type-switches on concrete variants
  (`resolve.go:1447-1451`, `:1471-1482`), so no interface widening
  is needed.

### 2.4 The resolver-widening semantics — group-closure demotion

The widening lives entirely in `demoteNullableInPlace`
(`resolve.go:1446-1469`), called once per Part at Phase D
(`resolve.go:357-366`). Widened rule, replacing R4 §4.4's formal
rule:

> A binding `b` is effectively non-nullable iff derivable from:
> (i) `b.Nullable() == false` (parser-static, seed);
> (ii) `b` is a named `VarEndpoint` of an *effectively required*
> edge `e` in `part.Bindings` that passes the §4.4.3 hop gate
> (`qualifiedDemoter`, `resolve.go:1484-1494`, unchanged); or
> (iii) `b.OptionalGroup() == g ≥ 1` and any binding with group `g`
> is effectively non-nullable.
>
> An edge `e` is *effectively required* iff `e.Nullable() == false`
> or `e`'s group is demoted under (iii).

(ii) is R4's existing per-edge witness verbatim. (iii) is the new
group closure. The edge clause of (iii)→(ii) is what makes the rule
a genuine fixed point: proving a member of group g₂ demotes g₂'s
OPTIONAL edge, whose endpoints may name a member of an earlier
group g₁, demoting g₁ in turn (§8.4's cascade fixture). Termination
is monotone: table entries only flip true→false, demoted-group
entries only flip false→true, both finite. The lattice invariant
(R4 §4.2 — never demote incorrectly) holds: every derivation step
is justified by left-join row semantics (§2.1), so the widening
stays a non-breaking refinement — valid queries stay valid, no
column changes except `Nullable` bits moving true→false.

Group ids `0` participate in nothing: a group-0 binding (required,
or a hand-constructed legacy nullable binding from the preserved
`NewNullable*` ctors) gets exactly R4's pre-ay9 behaviour.

### 2.5 What this cycle does NOT touch

- **Class B / gqlc-5xg / Axis 2** — see the boundary paragraph at
  the top. `mergeBinding`'s merge content is unchanged; no clause
  references are preserved; `OPTIONAL MATCH (a)-[:R]->(b) MATCH (b)
  RETURN b` (bare re-MATCH, same Part) still under-demotes `b`
  after this cycle, by design, and its stage-table rows stay
  pointed at gqlc-5xg.
- **Cross-Part group carry** — the resolver's carry
  (`branchState.exportedNullableBinding`, consumed at
  `resolve.go:357-361`) is name → bool; it does not carry group
  ids, so `OPTIONAL MATCH (a)-[r1:AUTHORED]->(b) WITH a, b MATCH
  (b)-[r2:AUTHORED]->(c) RETURN a, b, c` demotes carried `b` but
  not its co-introduced `a`. This is a safe residual (same
  direction as R4's), recorded in the amendment (§7) and filed as
  a follow-up at close-out. Closing it needs **no further model
  change** — `branchState` is resolver-internal — which is one of
  the two reasons ids are query-scoped (§3.3).
- **`Binding` stays sealed at five variants; `Use` at three;
  `Type` at seventeen** — a field is added, not a variant.
- **Parser sentinels** — no new fail-site; the group id is derived
  state, never validated at parse (there is nothing to reject:
  every OPTIONAL clause gets an id unconditionally).
- **Resolver sentinels** — none added or widened (§8.2).
- **`PathBinding` / `UnwindBinding` / `CallBinding`** wire — 
  byte-identical (§2.3).

---

## 3. Mining — what the repo records today

### 3.1 The OPTIONAL threading — re-verified line refs

R4 §7.5.2 cites `listener.go:261-270` and `build.go:258-286`.
Re-derived at branch base `56718ff`:

- `EnterOC_Match` — `listener.go:261-270`. **Still accurate.** The
  clause handler reads `optional := c.OPTIONAL() != nil` (`:265`)
  after the Stage-11 `subqueryDepth` guard (`:262-264`) and threads
  it into `l.collectPattern(c.OC_Pattern(), optional)` (`:266`).
  This is the *only* OPTIONAL source: `EnterOC_Create` passes
  `false` (`listener.go:358`; comment at `:352`: "openCypher has no
  OPTIONAL CREATE"), `EnterOC_Merge` passes `false`
  (`listener.go:383`).
- The threading chain — `collectPattern` (`pattern.go:37-47`, loops
  comma-parts) → `collectPatternPart` (`:53-94`) →
  `collectPatternElement` (`:142` head) → `collectNode` (`:187-200`,
  → `mergeBinding` at `:197`) and `collectEdge` (`:251-307`, →
  anonymous-edge `rawBinding` literal at `:297` or `mergeBinding`
  at `:304`).
- `mergeBinding` — `pattern.go:385-401` (R4 cited `:373-401`
  including the doc comment; both still accurate). First
  introduction mints the `rawBinding` with `nullable: optional`
  (`:389`); re-occurrence merges labels only — "optional is
  honoured only on first introduction (ADR 0006)" (`:379`).
- `toBinding` — `build.go:263-284` (**drifted** from R4's
  `:258-286`; the doc comment now starts at `:257`). The four-way
  nullable × var-length routing through the six smart constructors.

The group id therefore rides exactly the wire the `optional` bool
already rides — no new traversal, no new state outside the listener
counter.

### 3.2 Constructor strategy — new ctors, existing preserved verbatim

Two precedents exist:

- **Before Stage 14 trailing-param** (`EdgeBinding.directed`, Stage 5):
  append a parameter to the existing constructors and touch every
  call site.
- **After Stage 14 new-ctor** (hk0 / fvo `NewPropertyUseAt` et al. /
  0ig `NewCallBindingWithArgs`): add a new constructor; preserve
  the existing one verbatim (0ig: `NewCallBinding` forwards through
  `NewCallBindingWithArgs` with `args=nil`, `query.go:891-893`).

**Decision: new-ctor.** Two grounds, in order of force:

1. **Protocol.** ADR 0008's additions convention is additive-only —
   "never rename, remove, or re-type what exists" — over "the
   exported Go API of `internal/query` (types, marker methods,
   constructors, accessors)". Appending a parameter to
   `NewNullableNodeBinding` / `NewNullableEdgeBinding` /
   `NewNullableVarLengthEdgeBinding` re-types three exported
   functions: an incompatible API change requiring a superseding
   ADR. The `directed` precedent was Stage 5, before Stage 14; it is not
   available later. Every later cycle (hk0, fvo, 0ig)
   used new-ctor for exactly this reason.
2. **Blast radius, counted.** Existing `NewNullable*` call sites at
   branch base (declarations excluded): trailing-param would edit
   **17 sites** — `build.go` 3 (the `toBinding` arms, which change
   under either strategy), `internal/query/query_test.go` 10
   (3 node at `:50`/`:61`/`:710` + 6 edge at
   `:116`/`:131`/`:139`/`:141`/`:166`/`:714` + 1 var-length at
   `:1140`), `parser_test.go` 4 (`:327`, `:344`, `:349`, `:1192`).
   New-ctor edits only the sites that *must* change because parser
   behaviour changes: `build.go`'s 3 arms and `parser_test.go`'s 4
   invocations (§4.3); the 10 query_test pins stay verbatim,
   pinning the preserved ctors' group-0 behaviour.

The three new constructors (§4.1.2) reject `group < 1` — group 0 is
reachable only through the preserved legacy ctors, keeping "in a
group" and "not in a group" constructor-disjoint.

### 3.3 Group-id allocation scope — per-query, pinned

Two candidate scopes: reset the counter per Part, or run it
per-query (per-parse). **Decision: per-query** — one
monotonically-increasing counter on the listener (one listener per
parse, `newListener` at `listener.go:177`), never reset. Grounds:

1. **How the resolver consumes ids.** The Phase D demotion walk is
   per-Part (`resolvePart`, `resolve.go:357-366`), and an OPTIONAL
   clause lives in exactly one Part, so *within* a Part the two
   scopes are indistinguishable. But the recorded residual (§2.5)
   — carrying group membership across WITH in resolver-internal
   `branchState` — is only well-defined if a carried id cannot
   collide with a local id. Per-Part reset makes "group 1 of Part
   0" and "group 1 of Part 1" the same integer; per-query ids make
   the id globally meaningful, so the follow-up lands without
   re-minting. Golden witness that the collision is real:
   `Match7_91ea67e28e2f` ("[21] Handling optional matches between
   nulls", `OPTIONAL MATCH (a:NotThere) OPTIONAL MATCH (b:NotThere)
   WITH a, b OPTIONAL MATCH (b)-[r:NOR_THIS]->(a) RETURN a, b, r`)
   has groups 1 and 2 in Part 0 and group 3 in Part 1 — under
   per-Part reset, Part 1's group would also be "1".
2. **Less mechanism.** Per-Part reset needs a hook in the
   part-close path (`EnterOC_With` and friends); per-query is one
   `l.optionalGroupSeq++` at one site.

Ids are minted **after** the `subqueryDepth` guard in
`EnterOC_Match`, so an OPTIONAL MATCH suppressed inside
`EXISTS { … }` (Stage 11) consumes no id and golden ids stay dense
and deterministic. UNION branches share the per-query counter
(branch 2's first OPTIONAL clause continues from branch 1's last
id) — ids are unique across the whole `query.Query`, which is
strictly stronger than any consumer requires.

### 3.4 Zero-value analysis

- **Go side.** `optionalGroup` zero value is `0` = "no group". A
  `NodeBinding{}` or any binding from the six preserved ctors is
  bit-identical to its pre-ay9 value plus a zero field —
  `require.Equal` / `reflect.DeepEqual` pins over non-OPTIONAL
  constructions are untouched (the mechanism behind §4.3's 149
  preserved pins).
- **Legacy nullable + group 0.** Reachable via the preserved
  `NewNullable*` ctors (protocol forbids removing them). The
  resolver treats it exactly as R4 did: nullable, no sibling
  propagation. This is the correct degraded semantics, not an
  error state — pinned by a unit test (§5).
- **Wire side.** `,omitempty` on `int` elides `0`. A non-OPTIONAL
  golden's bytes cannot change; an OPTIONAL binding gains one key.
  Real bytes from the corpus at branch base — the node binding of
  `Null1_98c5fae83b11` ("[3] Property null check on null node",
  query `OPTIONAL MATCH (n)\nRETURN n.missing IS NULL`):

  ```json
  {
    "kind": "node",
    "variable": "n",
    "labels": null,
    "nullable": true
  }
  ```

  post-change (tail-append, §4.1.3):

  ```json
  {
    "kind": "node",
    "variable": "n",
    "labels": null,
    "nullable": true,
    "optionalGroup": 1
  }
  ```

---

## 4. The change — model and parser changes

### 4.1 `internal/query/query.go`

#### 4.1.1 Fields

`NodeBinding` (`query.go:313-317`) appends:

```go
optionalGroup int // ≥1: id of the introducing OPTIONAL clause (ay9); 0: not OPTIONAL-introduced
```

`EdgeBinding` (`query.go:372-380`) appends the same field after
`hops`. Tail-append is the campaign convention (0ig appended `args`
last on `CallBinding`, `query.go:849`).

#### 4.1.2 Constructors + accessors

```go
// NewNullableNodeBindingInGroup builds the OPTIONAL-introduced variant
// carrying its introducing clause's group id (ay9, ADR 0008 amendment
// 2026-07-07). group identifies the OPTIONAL MATCH clause that first
// introduced this binding — unique per query, minted by the parser —
// and must be ≥ 1: group 0 ("no group") is reachable only through
// NewNullableNodeBinding, preserved verbatim below.
func NewNullableNodeBindingInGroup(variable string, labels graph.LabelSet, group int) (NodeBinding, error)

func NewNullableEdgeBindingInGroup(variable string, labels graph.LabelSet, source, target Endpoint, directed bool, group int) (EdgeBinding, error)

func NewNullableVarLengthEdgeBindingInGroup(variable string, labels graph.LabelSet, source, target Endpoint, directed bool, hops EdgeHops, group int) (EdgeBinding, error)
```

Each forwards through its existing `NewNullable*` counterpart
(`query.go:330-337`, `:399-406`, `:424-431`) and then sets
`optionalGroup`, after rejecting `group < 1` with an error
(`"query: optional group id must be ≥ 1"` — model-invariant guard;
the parser never mints 0 here). The six existing constructors are
**byte-identical** — including doc comments — so every API
consumer and every group-0 pin is untouched.

Accessors:

```go
// OptionalGroup is the id of the OPTIONAL MATCH clause that first
// introduced this binding (unique per query), or 0 when the binding
// was not OPTIONAL-introduced. Nullable() == true ⇔ OptionalGroup() ≥ 1
// for every parser-produced binding; the resolver's group-closure
// demotion (ay9 widening) reads it to propagate a proven member's
// non-nullness to its clause siblings.
func (b NodeBinding) OptionalGroup() int { return b.optionalGroup }
func (b EdgeBinding) OptionalGroup() int { return b.optionalGroup }
```

#### 4.1.3 JSON — omit-when-zero on `optionalGroup`

`NodeBinding.MarshalJSON` (`query.go:1616-1623`) and
`EdgeBinding.MarshalJSON` (`:1630-1641`) each append one struct
field:

```go
OptionalGroup int `json:"optionalGroup,omitempty"`
```

as the **last** key (node key order becomes `kind, variable,
labels, nullable, optionalGroup`; edge: `kind, variable, labels,
source, target, nullable, directed, hops, optionalGroup`). The
before Stage 14 always-emit convention (`nullable`, `directed`, `hops`)
is deliberately not followed — ADR 0008's hk0 amendment fixed
omit-when-zero-value as the wire convention precisely so
additive cycles do not force near-total 3199-file rebaselines.

### 4.2 Parser threading

#### 4.2.1 `listener.go` — the counter and the mint site

The `listener` struct (`listener.go:31-93`) gains:

```go
// optionalGroupSeq mints per-query OPTIONAL-clause group ids (ay9).
// Incremented once per collected OPTIONAL MATCH clause; never reset
// (ids are query-scoped — see the ay9 spec §3.3). Suppressed clauses
// inside EXISTS { … } consume no id (the mint sits after the
// subqueryDepth guard).
optionalGroupSeq int
```

`EnterOC_Match` (`listener.go:261-270`) becomes:

```go
func (l *listener) EnterOC_Match(c *gen.OC_MatchContext) {
	if l.subqueryDepth > 0 {
		return // Stage 11 §1.2: EXISTS { ... } suppresses inner clause collection.
	}
	group := 0
	if c.OPTIONAL() != nil {
		l.optionalGroupSeq++
		group = l.optionalGroupSeq
	}
	l.collectPattern(c.OC_Pattern(), group)
	if w := c.OC_Where(); w != nil {
		l.mineWhere(w)
	}
}
```

An id is minted per OPTIONAL clause unconditionally — even when
every variable in the clause is pre-bound and no binding records
the id (e.g. Match7 [30]'s `OPTIONAL MATCH (p)-[s:SUPPORTS]->(team)`
records the id only on `s`). Ids on the wire are therefore not
guaranteed contiguous, only ordered; no consumer reads contiguity.

#### 4.2.2 `pattern.go` — re-typing the threaded parameter

`optional bool` → `group int` on the six signatures
(`collectPattern :37`, `collectPatternPart :53`,
`collectPatternElement :142`, `collectNode :187`,
`collectEdge :251`, `mergeBinding :385`). Where the bool is
consumed today, `group` is stored instead:

- anonymous-edge literal (`pattern.go:297`): `nullable: optional`
  → `optionalGroup: group`;
- `mergeBinding` first-introduction (`:389`): same substitution.

`rawBinding` (`listener.go:157-167`) replaces `nullable bool` with
`optionalGroup int` — parser-internal, single source of truth, so
the `nullable ⇔ group ≥ 1` invariant cannot be violated by a
divergent pair of fields. `mergeBinding`'s re-occurrence arm is
untouched: labels merge, group (like nullable before it) is
first-introduction-only.

CREATE (`listener.go:358`) and MERGE (`:383`) pass `0`.

#### 4.2.3 `build.go` — `toBinding` routes to the InGroup ctors

```go
func (rb *rawBinding) toBinding() (query.Binding, error) {
	if rb.kind == graph.Edge {
		directed := !rb.undirected
		if rb.hops != nil {
			if rb.optionalGroup > 0 {
				return query.NewNullableVarLengthEdgeBindingInGroup(rb.variable, rb.labels, rb.source, rb.target, directed, *rb.hops, rb.optionalGroup)
			}
			return query.NewVarLengthEdgeBinding(rb.variable, rb.labels, rb.source, rb.target, directed, *rb.hops)
		}
		if rb.optionalGroup > 0 {
			return query.NewNullableEdgeBindingInGroup(rb.variable, rb.labels, rb.source, rb.target, directed, rb.optionalGroup)
		}
		return query.NewEdgeBinding(rb.variable, rb.labels, rb.source, rb.target, directed)
	}
	if rb.optionalGroup > 0 {
		return query.NewNullableNodeBindingInGroup(rb.variable, rb.labels, rb.optionalGroup)
	}
	return query.NewNodeBinding(rb.variable, rb.labels)
}
```

Named paths need no change: `NewPathBinding` is constructed at
`pattern.go:87` from members only; OPTIONAL flows through the
member bindings (§2.3).

### 4.3 Parser-test pins — 3 flip, 149 preserved

Full Binding-constructor invocation census over
`internal/query/cypher/parser_test.go` at branch base (grep
`query.New*(`, declarations excluded):

| Constructor | Invocations | Flip |
|---|---:|---:|
| `NewNodeBinding` | 101 | 0 |
| `NewNullableNodeBinding` | 3 (`:327`, `:349`, `:1192`) | 3 |
| `NewEdgeBinding` | 15 | 0 |
| `NewNullableEdgeBinding` | 1 (`:344`) | 1 |
| `NewVarLengthEdgeBinding` | 4 | 0 |
| `NewNullableVarLengthEdgeBinding` | 0 | 0 |
| `NewPathBinding` | 5 | 0 |
| `NewUnwindBinding` | 9 | 0 |
| `NewCallBinding` | 5 | 0 |
| `NewCallBindingWithArgs` | 10 | 0 |
| **Total** | **153** | **4** |

The 4 flipped invocations live in the only 3 mustParse pins whose
source text contains OPTIONAL (grep-verified: `:324`, `:340`,
`:1189` are the only OPTIONAL-carrying `src:` lines in the file).
Each query has exactly one OPTIONAL clause, so each rebaseline is
mechanical — the invocation gains `InGroup` and a trailing `1`:

- `"optional match simple"` (`:323-333`, `OPTIONAL MATCH (n)
  RETURN n`): `NewNullableNodeBinding("n", nil)` →
  `NewNullableNodeBindingInGroup("n", nil, 1)`.
- `"optional match reuses prior binding"` (`:339-356`, `MATCH (n)
  OPTIONAL MATCH (n)-[:NOT_EXIST]->(x) RETURN n, x`): the anonymous
  edge (`:344`) and `x` (`:349`) both take group 1; `n` (`:343`,
  required first introduction) stays `NewNodeBinding` — group 0.
- `"count distinct"` (`:1188-1198`, `OPTIONAL MATCH (a)
  RETURN count(DISTINCT a)`): `a` takes group 1.

The remaining 149 invocations construct group-0 bindings and
compare equal to parser output bit-for-bit (§3.4); they stay
verbatim. Classification format for the model-change PR body follows
0ig §4.3.2: each flipped pin cited by test name + line + new group
value.

### 4.4 Parser goldens — 100 flip, 3099 byte-identical

Discovery method (rerunnable; this is how the numbers below were
derived, not a paraphrase of another document): walk every
`internal/query/cypher/testdata/golden/*.golden.json`, flag the
golden iff any `branches[].parts[].bindings[]` entry has
`"kind": "node"` or `"kind": "edge"` and `"nullable": true`. That
predicate is exact for this cycle because (a) for node/edge
bindings `nullable` is set iff OPTIONAL-introduced (§3.1 — the only
`optional=true` source is `EnterOC_Match`), and (b) the axis is
recorded exactly on OPTIONAL-introduced node/edge bindings (§2.2
invariant), and (c) `omitempty` hides group 0 everywhere else. A
`CallBinding` with `"nullable": true` (signature `?`) does not
match the predicate's kind gate and does not flip.

Result at branch base `56718ff`: **3199 total goldens, 100 flip,
3099 byte-identical.** Flip distribution by feature file:

| Feature | Flips | Feature | Flips |
|---|---:|---|---:|
| Match7 | 31 | Null1, Null2, Match3, Match9, Graph4, Graph8, Delete2, Remove1, With1, WithWhere1, Aggregation5 | 2 each |
| TriadicSelection1 | 18 | Aggregation8, Delete3, Graph3, Graph5, Graph9, List12, Match8, Path1, Path2, Remove2, Set1, Set3, Set4, Set5 | 1 each |
| MatchWhere6 | 8 | | |
| Graph6 | 4 | | |
| Delete1 | 3 | | |

Full 100-file flip set (the fence's expected set — a rebaseline
touching any file outside it, or missing any file inside it, FAILS
review):

```
Aggregation5_a104ddb1778f Aggregation5_db0ab4b2a34e Aggregation8_b9697a4bacf2
Delete1_2ecb46009d6a Delete1_a18d09d345df Delete1_e1af701a4e3d
Delete2_04b307b0cb7d Delete2_6af6b56e1b9b Delete3_eff529440090
Graph3_e73f683a2770 Graph4_0ac977402dee Graph4_8abfe74853b0
Graph5_87075e4193a9 Graph6_2094d59c190d Graph6_2c9d65dfb77c
Graph6_6940aa52f026 Graph6_950ee65a02f8 Graph8_84b838195798
Graph8_8e685ea63903 Graph9_aea1d2b4078d List12_33d76b6f508c
Match3_b1029d7643e1 Match3_b684f974876f
Match7_0067d1619b4b Match7_0f4fffb1c389 Match7_110b9455ccd0
Match7_14335f66705e Match7_1a6ea325f56c Match7_2864ddd6afc1
Match7_2988fb4cf3d8 Match7_31766d5c1d05 Match7_41d81a880692
Match7_5d92e8bfe43b Match7_625829a1e961 Match7_805e221f5275
Match7_897995667cef Match7_89b7187b1d0e Match7_8c387dc16044
Match7_8cb685af6367 Match7_912b4aed0f10 Match7_91ea67e28e2f
Match7_92cb05eec485 Match7_a897d1a8d01d Match7_b14310a68930
Match7_b41b814be9d5 Match7_b544b45d180d Match7_c4aede9ffbe7
Match7_c768be81cf7c Match7_c8b4140fc475 Match7_cba9d062e673
Match7_df367e237f6b Match7_e86d92e939fe Match7_f68665b5b8df
Match7_f8ccf8494d7d Match8_0069fa8007e5
Match9_38d12397ae84 Match9_76269c890fb1
MatchWhere6_171bfad3b4fd MatchWhere6_1f9af2a32a26 MatchWhere6_24af3716870d
MatchWhere6_30a782be2581 MatchWhere6_4d610d8aaab8 MatchWhere6_550888f26d2b
MatchWhere6_8a2f5dd8abfa MatchWhere6_c64955c110d2
Null1_98c5fae83b11 Null1_9b566a68279e Null2_4b6c12a16fc1 Null2_a8766a87c846
Path1_c2f7539c7274 Path2_6458b879650d
Remove1_0b82d9aaa6c6 Remove1_431e14b4c153 Remove2_cc8bf7ab3495
Set1_b6aea8f518e2 Set3_21565a8351a4 Set4_e0901190c8f3 Set5_fb1f2b832025
TriadicSelection1_348d8090448b TriadicSelection1_3d04b535adfd
TriadicSelection1_5abdce39d355 TriadicSelection1_5bd8c30a656c
TriadicSelection1_6d103e6957cd TriadicSelection1_700e98f98a5a
TriadicSelection1_718e2fc1893a TriadicSelection1_71f8b5d4f477
TriadicSelection1_7f4d2c3af777 TriadicSelection1_801e97f43a6d
TriadicSelection1_8a6701a15d28 TriadicSelection1_a748d43afc48
TriadicSelection1_c42243df5fec TriadicSelection1_c67756853101
TriadicSelection1_cc6c1bebdbf2 TriadicSelection1_cd84bc4b8df4
TriadicSelection1_e3829e0570e9 TriadicSelection1_f781b787e249
With1_c6943cfb093d With1_f104f88c4a91
WithWhere1_67ee406bcc6d WithWhere1_edbd5fd5e657
```

(Each name abbreviates `<name>.golden.json`.)

#### 4.4.1 Spot witnesses — scenario, query, expected wire delta

Scenario names and queries below are re-derived from the vendored
feature files by replicating `goldenPath`'s hash
(`acceptance_test.go:1063-1068`: SHA1 over
`uri + "\x00" + name + "\x00" + query`, first 6 bytes) — the
replication was validated by reproducing all 31 on-disk Match7
golden filenames before use.

1. **`Match7_c4aede9ffbe7`** — "[24] Optionally matching
   self-loops", `MATCH (a:B)\nOPTIONAL MATCH (a)-[r]-(a)\nRETURN r`.
   Current bytes (edge binding, abridged to the tail):
   `"nullable": true, "directed": false, "hops": null`. Expected:
   gains `"optionalGroup": 1` after `"hops"`. The `a` node binding
   (`"nullable": false`) is byte-identical.
2. **`Null1_98c5fae83b11`** — "[3] Property null check on null
   node", `OPTIONAL MATCH (n)\nRETURN n.missing IS NULL`. Wire
   delta quoted in full at §3.4.
3. **`MatchWhere6_8a2f5dd8abfa`** — "[8] Join nodes on non-equality
   of properties – Two OPTIONAL MATCH clauses and WHERE",
   `MATCH (x:X)\nOPTIONAL MATCH (x)-[:E1]->(y:Y)\nOPTIONAL MATCH
   (y)-[:E2]->(z:Z)\nWHERE x.val < z.val\nRETURN x, y, z`. The
   distinct-ids witness: anonymous `:E1` edge and `y` gain
   `"optionalGroup": 1`; anonymous `:E2` edge and `z` gain
   `"optionalGroup": 2`; `x` byte-identical.
4. **`Match7_91ea67e28e2f`** — "[21] Handling optional matches
   between nulls" (query at §3.3). The per-query-scope witness:
   Part 0 carries groups 1 (`a`) and 2 (`b`); Part 1's re-introduced
   `b`, `r`, `a` (all `"nullable": true` at branch base — verified
   against the on-disk golden) all gain `"optionalGroup": 3`.
5. **`Match7_0067d1619b4b`** — "[30] Satisfies the open world
   assumption, single relationship", `MATCH
   (p:Player)-[:PLAYS_FOR]->(team:Team)\nOPTIONAL MATCH
   (p)-[s:SUPPORTS]->(team)\nRETURN count(*) AS matches, s IS NULL
   AS optMatch`. The pre-bound-variables witness: only `s` gains
   `"optionalGroup": 1`; `p`, `team`, and the required anonymous
   edge are byte-identical.

#### 4.4.2 Reviewer-side fence commands

From the model-change-PR branch tip:

```sh
# Fence A — rebaseline reproducibility: regenerate and expect no drift.
go test ./internal/query/cypher/ -run TestAcceptance -update
git status --porcelain internal/query/cypher/testdata/golden/
# MUST print: no changes.

# Fence B — flip-set equality: the diff against the merge base names
# exactly the 100 files of §4.4, no more, no fewer.
git diff --name-only $(git merge-base HEAD origin/master) -- internal/query/cypher/testdata/golden/ | sort > /tmp/flipped
# compare against the §4.4 list (SET-EQUAL required).

# Fence C — the new key appears nowhere outside the flip set, and in
# every member of it.
grep -rl '"optionalGroup"' internal/query/cypher/testdata/golden/ | sort > /tmp/carrying
diff /tmp/flipped /tmp/carrying
# MUST print: empty.

# Fence D — resolver is untouched and green with zero golden churn.
go test ./internal/resolver/
git status --porcelain test/data/resolver/
# MUST print: ok + no changes.
```

(The exact `-run` filter names follow the test files' current
top-level test functions; the PR body pins the literal commands it
ran.)

---

## 5. Unit-test additions — the two-sided axis

New tests in `internal/query/query_test.go`, mirroring the campaign
convention (0ig §5: pin both the zero and non-zero sides of a new
axis):

- `TestNewNullableNodeBindingInGroup` — group 3 round-trips through
  `OptionalGroup()`; `Nullable()` is true; variable/labels
  invariants inherited from the forwarding target.
- `TestNewNullableNodeBindingInGroupRejectsNonPositiveGroup` —
  group 0 and −1 error.
- `TestNewNullableEdgeBindingInGroup` /
  `TestNewNullableVarLengthEdgeBindingInGroup` — same shape, plus
  hops preserved on the var-length form.
- `TestOptionalGroupZeroOnLegacyConstructors` — every one of the six
  preserved ctors yields `OptionalGroup() == 0` (the §3.4 legacy
  state, pinned as supported).
- `TestMarshalJSONEmitsOptionalGroup` — a group-2 node binding's
  bytes contain `"optionalGroup":2`; a group-1 edge binding's bytes
  contain `"optionalGroup":1` positioned after `"hops"`.
- `TestMarshalJSONOmitsZeroOptionalGroup` — bindings from
  `NewNodeBinding` / `NewNullableNodeBinding` / `NewEdgeBinding`
  serialise **without** the key (byte-level `require.NotContains`),
  which is the wire-compat fence in unit form.

The pre-existing `TestMarshalJSONEmitsNullable`
(`query_test.go:704-723`) and the 10 `NewNullable*` pins stay
verbatim.

---

## 6. (reserved)

Numbering aligned with the 0ig template; this cycle has no
listener-vs-builder split decision to record — the mint site is
`EnterOC_Match` (§4.2.1) and there is no alternative site to argue
against, because it is the grammar's only OPTIONAL production
(§2.3).

---

## 7. ADR 0008 amendment note — the dated stage note

Verbatim text to add at the top of
`docs/adr/0008-query-model-surface-resolver-api.md`, above the 0ig
note, following the established amendment format:

```markdown
> _Amendment (2026-07-07, gqlc-ay9 model-change cycle): the
> OPTIONAL-group membership axis on `NodeBinding` / `EdgeBinding` —
> R4 §7.5.4's **Axis 1**, filed from the R4 close-out as gqlc-ay9 —
> is **adopted** under this ADR's coordinated change with consumers.
> The wire recorded per-binding `Nullable()` but not which
> bindings were co-introduced by the same OPTIONAL MATCH clause, so
> the resolver's R4 regime-(a) demotion could not propagate a proven
> member's non-nullness to its clause siblings (Class A,
> `docs/specs/resolver-stage-r4.md §7.5.3` items 1 + 3). Each of the
> two OPTIONAL-introducible Binding variants gains one additive
> field `optionalGroup int`, one accessor `OptionalGroup() int`, and
> one new constructor per OPTIONAL-introduced shape
> (`NewNullableNodeBindingInGroup`, `NewNullableEdgeBindingInGroup`,
> `NewNullableVarLengthEdgeBindingInGroup`); the six existing
> binding constructors are preserved verbatim and continue to mint
> group 0 ("not in any OPTIONAL group"). Group ids are minted
> per-parse by the parser — fresh id per OPTIONAL MATCH clause,
> unique across the whole query — and recorded only at first
> introduction, matching ADR 0006's nullability discipline.
> `PathBinding` / `UnwindBinding` / `CallBinding` are untouched
> (their `Nullable()` is not OPTIONAL-derived; a named path's group
> facts flow through its member bindings, the Stage-8 posture). The
> JSON encoding is **omit-when-zero-value** (`,omitempty`),
> following the wire convention this ADR's hk0 amendment
> established; 100 of 3199 parser goldens rebaseline, 3099 are
> byte-identical. The Binding interface stays sealed at
> `Kind()`/`Nullable()`/`isBinding()`; group membership is a
> per-variant field-and-accessor concern. The resolver widening
> (post-ay9 PR) extends `demoteNullableInPlace` to the group-closure
> fixed point — "if a required chain proves any member of an
> OPTIONAL group exists, every member of that group demotes" —
> flipping two resolver goldens (`demote_chained_from_required`,
> `demote_from_anonymous_required_edge`) and adding no sentinel.
> **Residual**: the resolver's cross-Part carry is name-granular and
> does not yet carry group ids, so a WITH-carried binding demotes
> without its co-introduced siblings; closing this needs only a
> resolver-internal `branchState` extension (no further model
> change) and is filed as a follow-up bead at close-out. Class B —
> the same-Part second-reference gap (R4 §7.5.3 item 2, Axis 2,
> gqlc-5xg) — is a missing-witness gap this axis deliberately does
> not close. See `docs/specs/model-change-ay9-optional-group.md` for the
> full contract, the 100-golden flip census with spot witnesses, the
> constructor-strategy and id-scope decisions, and the fence
> commands._
```

The Known-deferred-additions entry (appended to the list; note
`OptionalGroup` was **not** previously on the list — the entry is
added closed-out, the same move the 0ig cycle made for
`CallBinding.Args`):

```markdown
- **`OptionalGroup` axis on `NodeBinding` / `EdgeBinding`** —
  adopted 2026-07-07 (see the amendment note above and
  `docs/specs/model-change-ay9-optional-group.md`). Populated
  parser-side by the OPTIONAL threading through `collectPattern`
  (fresh id per OPTIONAL MATCH clause, query-scoped, minted in
  `EnterOC_Match`); consumed by the resolver's Phase D
  group-closure demotion (`demoteNullableInPlace`,
  `internal/resolver/resolve.go` — see the ay9 resolver-widening
  PR), closing R4 §7.5.3 Class A (items 1 + 3). Class B (item 2)
  remains open under gqlc-5xg.
```

---

## 8. Resolver widening — the follow-up PR after the change merges

The model-change PR lands with the resolver green and unchanged: the
kernel reads `Nullable()` only, `OptionalGroup()` is populated but
unconsumed. The widening PR then extends Phase D. This section pins
the semantics; the PR implements them.

### 8.1 `demoteNullableInPlace` — the exact widening

Current (`resolve.go:1446-1469`): one pass over `part.Bindings`;
each edge with `!e.Nullable() && qualifiedDemoter(e)` demotes its
named `VarEndpoint` variables in the table. Widened:

```go
// demoteNullableInPlace runs the ay9-widened regime-(a) demotion on
// part.Bindings against a pre-seeded table: per-edge endpoint
// witnessing (R4 §4.4) plus OPTIONAL-group closure (ay9 — a proven
// member demotes its whole introducing clause). The loop is a genuine
// fixed point: a group-demoted OPTIONAL edge becomes an effective
// witness for its own endpoints, which may prove an earlier group.
// Monotone (table entries flip true→false, demotedGroups false→true),
// so it terminates.
func demoteNullableInPlace(bindings []query.Binding, table map[string]bool) {
	members := map[int][]string{} // group id → named members
	groupOf := map[string]int{}   // named member → group id
	for _, b := range bindings {
		switch bb := b.(type) {
		case query.NodeBinding:
			if g := bb.OptionalGroup(); g > 0 {
				members[g] = append(members[g], bb.Variable())
				groupOf[bb.Variable()] = g
			}
		case query.EdgeBinding:
			if g := bb.OptionalGroup(); g > 0 && bb.Variable() != "" {
				members[g] = append(members[g], bb.Variable())
				groupOf[bb.Variable()] = g
			}
		}
	}
	demotedGroups := map[int]bool{}
	demoteGroup := func(g int) bool {
		if g == 0 || demotedGroups[g] {
			return false
		}
		demotedGroups[g] = true
		for _, m := range members[g] {
			if _, present := table[m]; present {
				table[m] = false
			}
		}
		return true
	}
	for changed := true; changed; {
		changed = false
		for _, b := range bindings {
			e, ok := b.(query.EdgeBinding)
			if !ok {
				continue
			}
			// ay9: an OPTIONAL edge whose group is proven is an
			// effective witness (its existence on surviving rows is
			// established); the §4.4.3 hop gate applies unchanged.
			if (e.Nullable() && !demotedGroups[e.OptionalGroup()]) || !qualifiedDemoter(e) {
				continue
			}
			for _, side := range [2]query.Endpoint{e.Source(), e.Target()} {
				ve, ok := side.(query.VarEndpoint)
				if !ok {
					continue
				}
				v := ve.Variable()
				if v == "" {
					continue
				}
				if nb, present := table[v]; present && nb {
					table[v] = false
					changed = true
				}
				if demoteGroup(groupOf[v]) {
					changed = true
				}
			}
		}
	}
}
```

Notes pinned for the implementer:

- `seedLocalNullability` (`resolve.go:1432-1440`) and
  `qualifiedDemoter` (`:1484-1494`) are unchanged; the carry-seed →
  local-override → demote order at the Phase D call site
  (`:357-366`) is unchanged.
- Group closure demotes *named* members via the table; an anonymous
  OPTIONAL edge (no table entry) participates through
  `demotedGroups`, which is what upgrades it to an effective
  witness. Endpoint-proving by an anonymous required edge already
  worked at R4 (`demote_from_anonymous_required_edge`); the ay9
  addition is that a proven endpoint now pulls its group.
- `groupOf[v]` for a required or carried `v` is 0 and
  `demoteGroup(0)` is a no-op — group-0 bindings get exactly R4
  behaviour (§2.4).
- The hop gate deliberately still governs the *edge-as-witness*
  step even for group-demoted var-length edges (conservative,
  consistent with §4.4.3); group *membership* demotion of the edge
  itself is unconditional (a matched clause yields a non-NULL —
  possibly empty — list for a `*0..` member).

### 8.2 Sentinel discipline — zero new, verified

Expected posture: **no new sentinel, no new failure mode.** The
entire R4 demotion path is error-free at branch base —
`seedLocalNullability` and `demoteNullableInPlace` return nothing
(`resolve.go:1432`, `:1446`), and Phase D (`:357-366`) has no error
branch. Group closure adds pure map arithmetic; a demotion widening
is a refinement (fewer nullable columns), not a new rejection
class. `allSentinels` (`internal/resolver/errors.go:107-119`)
stays at its current **11 entries**. This intentionally contrasts
with 0ig, whose widening added a genuine rejection class
(`ErrCallArgAssignability`, errata NIT7) — there is no analogous
class here, and a PR that adds one is off-spec.

### 8.3 Existing resolver goldens — exactly 2 flip, derived

All 122 valid fixtures were swept; 14 contain OPTIONAL
(grep-verified). Per-fixture derivation of the widened rule over
the 14:

**Flips (2):**

1. `demote_chained_from_required.cypher` — group 1 =
   `{p, r1, post}`; required `r2` proves `post` → closure demotes
   `p`, `r1`. Golden delta: `p` column `"nullable": true → false`,
   `r1` column `"nullable": true → false`; `post`/`r2`/`author`
   already false. (Current under-demoted bytes quoted at §2.1.)
2. `demote_from_anonymous_required_edge.cypher` —
   `OPTIONAL MATCH (a:Person)-[:AUTHORED]->(b:Post) MATCH
   (a)-[:AUTHORED]->(c:Post) RETURN a, b, c`; group 1 =
   `{a, anon-edge, b}`; the required anonymous edge proves `a` →
   closure demotes `b`. Golden delta: `b` column
   `"nullable": true → false` (current golden: `a` false, `b`
   **true**, `c` false — verified). R4 §7.5.5 names only fixture 1
   as the widening's golden change; this second flip is a spec-cycle
   finding (§10 item 3).

**Byte-identical (12):** `carry_nullable_binding` (no witness
anywhere), `demote_cross_with_remerge` (Part 1's re-MATCHed `b` is
a fresh required binding — `RETURN b` already false; Part 0 has no
witness), `demote_var_length_unbounded_lower` /
`no_demote_var_length_zero_min` (arity-1 groups — closure adds
nothing beyond the endpoint the witness already demotes, or the hop
gate blocks), `optional_edge_property`, `optional_edge_whole_entity`,
`optional_multi_type_union`, `optional_node_nullable_property`,
`optional_node_property`, `optional_node_whole_entity`,
`optional_var_length_whole_entity`, `set_property_on_nullable`
(single OPTIONAL clause, no required edge after — no witness, no
closure seed). The remaining 108 valid fixtures contain no OPTIONAL
and cannot move. All 50 invalid fixtures keep their sentinel —
demotion runs on valid paths only.

Fence: `go test ./internal/resolver/ -update` then
`git status --porcelain test/data/resolver/` shows exactly the 2
flip goldens + the 4 new fixture pairs + `schema.mapping.json`.

### 8.4 New fixtures — names, queries, expected columns

All four map to the existing `social_r4.gql`
(`test/data/resolver/valid/schemas/`, edges: Person-AUTHORED→Post,
Person-LIKES→Post, Post-AUTHORED→Person, Person-KNOWS→Person); four
entries join `schema.mapping.json`.

1. **`demote_group_arity_five.cypher`** — §7.5.3 item 3's "larger
   group" (k = 5):

   ```
   OPTIONAL MATCH (a:Person)-[r1:KNOWS]->(b:Person)-[r2:AUTHORED]->(c:Post) MATCH (c)-[r3:AUTHORED]->(d:Person) RETURN a, r1, b, r2, c, d
   ```

   Group 1 = `{a, r1, b, r2, c}`; required `r3` proves `c` → all
   five demote. Expected: every column `"nullable": false`.

2. **`demote_two_groups_one_proven.cypher`** — distinct ids
   discriminate:

   ```
   OPTIONAL MATCH (a:Person)-[r1:AUTHORED]->(b:Post) OPTIONAL MATCH (c:Person)-[r2:LIKES]->(d:Post) MATCH (c)-[r3:KNOWS]->(e:Person) RETURN a, b, c, d, e
   ```

   `r3` proves `c` (group 2) → `c`, `r2`, `d` demote; group 1
   (`a`, `r1`, `b`) is untouched. Expected: `a` true, `b` true,
   `c` false, `d` false, `e` false. This fixture fails on any
   implementation that conflates groups (e.g. "any OPTIONAL binding
   demotes when any OPTIONAL binding is proven").

3. **`no_demote_unproven_group.cypher`** — the no-proof guard:

   ```
   OPTIONAL MATCH (a:Person)-[r1:AUTHORED]->(b:Post) MATCH (c:Person)-[r2:KNOWS]->(d:Person) RETURN a, b, c, d
   ```

   The required chain touches no member of group 1. Expected: `a`
   true, `b` true, `c` false, `d` false — the widening must not
   over-demote (lattice invariant, R4 §4.2).

4. **`demote_group_cascade.cypher`** — the fixed point earns its
   loop:

   ```
   OPTIONAL MATCH (a:Person)-[r1:KNOWS]->(b:Person) OPTIONAL MATCH (b)-[r2:KNOWS]->(c:Person) MATCH (c)-[r3:KNOWS]->(d:Person) RETURN a, r1, b, r2, c, d
   ```

   `r3` proves `c` (group 2) → `r2` demotes by closure → `r2` is
   now an effective witness proving its endpoint `b` (group 1) →
   `a`, `r1` demote. Expected: all six columns
   `"nullable": false`. A single-pass implementation leaves `a`,
   `r1`, `b` nullable and fails this golden. (Left-join soundness:
   `c` non-NULL ⇒ clause 2 matched ⇒ `r2`, and its endpoint `b`,
   non-NULL ⇒ clause 1 matched ⇒ `a`, `r1` non-NULL.)

### 8.5 Byte-identity fence over the untouched corpus

```sh
# From the widening-PR branch tip.
go test ./internal/resolver/ -update
git status --porcelain test/data/resolver/ | grep -v \
  -e demote_chained_from_required -e demote_from_anonymous_required_edge \
  -e demote_group_arity_five -e demote_two_groups_one_proven \
  -e no_demote_unproven_group -e demote_group_cascade \
  -e schema.mapping.json
# MUST print: empty.
go test ./internal/query/... && git status --porcelain internal/query/
# MUST print: ok + empty (the widening PR touches no parser surface).
```

### 8.6 Stage-spec successor prose — the docs-errata targets

Rows to retire (Class A only; every Class B / gqlc-5xg row stays
verbatim), located at branch base:

| File | Site | Current text (abridged) |
|---|---|---|
| `resolver-stage-r4.md:1185` | §7.4 table | "OPTIONAL-clause-sibling demotion … silently under-demoted … §7.5.5 bead 1" |
| `resolver-stage-r4.md` §4.4 (`:528-539`) | "Pass 2" prose | "not across OPTIONAL-clause siblings … the query model does not record that" |
| `resolver-stage-r4.md:1534-1541` | §7.5.5 fixture note | "If bead 1 lands … that fixture's golden is updated on the widening PR" — discharge |
| `resolver-stage-r5.md:610` | §4.1.1 table | "OPTIONAL-clause-sibling nullability under-demote … gqlc-ay9" |
| `resolver-stage-r5.md:1718-1719` | §4.6.2 | "Class A … still under-approximates at R5. Same story: gqlc-ay9" |
| `resolver-stage-r5.md:2311` | §7 table | "Nullability upgrades (OPTIONAL-clause-sibling …) … gqlc-ay9" |
| `resolver-stage-r6.md:1884` | §7 table | "… gqlc-ay9 (Class A, model change — unchanged from R5)" |
| `resolver-stage-r7.md:2190` | §7 table | "… gqlc-ay9 (unchanged)" |
| `resolver-stage-r5.md:40` | §1 prose | "sibling under-approximation (gap tracked on gqlc-ay9) is also unchanged" |
| `resolver-stage-r6.md:29` | §1 prose | "same-Part regime (b) nullability under-approximations (`gqlc-ay9`, …" |
| `resolver-stage-r7.md:2335` | §7.1.2 prose | "R4-inherited gaps (`gqlc-ay9` Class A, … persist at R7 unchanged" |

Successor wording per row: "closed by the ay9 change + widening
(`docs/specs/model-change-ay9-optional-group.md`); residual cross-Part
carry gap filed as <follow-up bead id>". The R4 §7.5.5 bead-1
paragraph gains a dated closure note rather than deletion
(stage-spec history stays readable, the 0ig §8.6 precedent).

---

## 9. Non-goals

| Out of scope | Where it lives |
|---|---|
| Class B same-Part second-reference gap (Axis 2) | gqlc-5xg — separate spec cycle; boundary pinned at top and §2.5 |
| Cross-Part group carry in `branchState` | follow-up bead filed at close-out (§2.5, §7 residual) |
| `PathBinding` group/nullability axis | Stage-8 posture (§2.3) |
| EXISTS-suppressed OPTIONAL clauses | Stage-11 posture unchanged; no ids minted (§4.2.1) |
| Codegen consumption of widened nullability | future ADR |
| apidiff CI gate | pre-existing ADR 0008 bead, unchanged by this cycle |
| Other change beads' surfaces (`hk0`/`fvo`/`0ig` axes) | closed; untouched |

---

## 10. Ground-truth cross-check

Every claim above rests on the citations below, all re-derived at
branch base `56718ff` (worktree `model-change-ay9-spec`); counts were
produced by the scripts described in §4.4 and §4.4.1, not copied
from prior documents.

- `Binding` interface — `internal/query/query.go:297-306`.
- `NodeBinding` struct / ctors / accessor / marshal —
  `query.go:313-317` / `:321-337` / `:354-356` / `:1616-1623`.
- `EdgeBinding` struct / ctors / accessor / marshal —
  `query.go:372-380` / `:388-431` / `:462-464` / `:1630-1641`.
- `PathBinding.Nullable()` hardcoded false — `query.go:698-700`
  (posture comment `:641-642`); `UnwindBinding` — `:803-806`;
  `CallBinding.Nullable()` signature-`?` semantics — `:950-952`.
- 0ig ctor precedent (`NewCallBinding` forwards, preserved) —
  `query.go:891-893`.
- Grammar: OPTIONAL only on `oC_Match` —
  `internal/grammar/cypher/Cypher.g4:81` (token def `:83`,
  symbolic-name alternative `:588`).
- Listener struct / `rawBinding` / `EnterOC_Match` / CREATE false /
  MERGE false — `internal/query/cypher/listener.go:31-93` /
  `:157-167` / `:261-270` / `:358` / `:383`.
- Threading chain + anonymous edge + `mergeBinding` —
  `internal/query/cypher/pattern.go:37-47`, `:53-94`, `:142-166`,
  `:187-200`, `:251-307` (literal `:297`), `:385-401`.
- `toBinding` — `internal/query/cypher/build.go:263-284`.
- Phase D order / seed / demote / hop gate / bindingVariable —
  `internal/resolver/resolve.go:357-366` / `:1432-1440` /
  `:1446-1469` / `:1484-1494` / `:1471-1482`.
- `allSentinels` (11 entries) —
  `internal/resolver/errors.go:107-119`.
- Golden hashing replicated for witness derivation —
  `internal/query/cypher/acceptance_test.go:1058-1068`; update
  flags — `acceptance_test.go:29`, `resolver_test.go:108`.
- Fixture + golden quotes —
  `test/data/resolver/valid/demote_chained_from_required.cypher{,.validated.golden.json}`,
  `…/demote_from_anonymous_required_edge.cypher{,.validated.golden.json}`;
  schema — `…/schemas/social_r4.gql`.
- Census inputs — 3199 parser goldens (glob count), 100-file flip
  set (§4.4 predicate), 153/4 parser-pin census (§4.3 grep), 122
  valid + 50 invalid resolver fixtures (glob count), 14
  OPTIONAL-carrying resolver fixtures (grep -il OPTIONAL).
- Design source — `docs/specs/resolver-stage-r4.md` §4.4
  (`:492-559`), §7.4 (`:1184-1185`), §7.5 (`:1270-1541`).
- Campaign precedents — `docs/adr/0008-query-model-surface-resolver-api.md`
  (protocol + three amendment notes + Known-deferred list),
  `docs/specs/model-change-0ig-call-args.md` (template + errata §12),
  `docs/specs/model-change-fvo-use-part.md:2459` (names this axis as
  "gqlc-ay9, Cycle 4").

Spec-vs-repo divergences found while mining (carried into the
docs-errata cycle where they touch stage-spec text):

1. R4 §7.5.2/§7.5.4 cite `build.go:258-286` for the OPTIONAL
   lowering; the landed `toBinding` spans `:257-284` (doc comment
   `:257-262`, body `:263-284`).
2. R4 §7.5.4's illustrative shape says "each `Binding` variant
   gains an `OptionalGroup int` accessor"; only two of the five
   variants can be OPTIONAL-introduced (§2.3) — this spec pins
   Node + Edge only.
3. R4 §7.5.5 names only `demote_chained_from_required` as the
   golden the widening updates; the sweep (§8.3) shows
   `demote_from_anonymous_required_edge` also flips.
4. R4 §4.4.2 titles the demotion a "fixed-point loop"; the landed
   `demoteNullableInPlace` is single-pass (sufficient pre-ay9 —
   witnesses were static). The ay9 widening is what first makes the
   loop iterate (§8.4 fixture 4).

---

## 11. Definition of done for the spec cycle

The spec PR is done when:

- This file lands on master under
  `docs/specs/model-change-ay9-optional-group.md`.
- No behavioural code changes (spec-only cycle).
- Review explicitly ACKs the four pinned decisions: variant set
  (Node + Edge only, §2.3), constructor strategy (new InGroup
  ctors, §3.2), id scope (per-query, §3.3), and the two-golden
  resolver flip set (§8.3) — the last because it diverges from R4
  §7.5.5's one-golden language.

The model-change PR (Cycle 2) then implements §4-§7 verbatim; the
resolver-widening PR (Cycle 3) implements §8; the docs-errata PR
(Cycle 4) lands §8.6's successor prose.
