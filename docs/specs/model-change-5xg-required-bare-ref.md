# Model change — required-bare-pattern re-reference on Binding

The implementation brief for cycle **gqlc-5xg** of the model-additions
campaign (Cycle 5, after hk0 / fvo / 0ig / ay9): an additive
`ReferencedInRequiredBarePattern bool` axis on `query.NodeBinding`
and `query.EdgeBinding`, populated parser-side by `mergeBinding`'s
existing merge arm, closing the R4 §7.5.3 **Class B**
same-Part second-reference nullability gap
(`docs/specs/resolver-stage-r4.md §7.5.3` item 2, §7.5.4 **Axis 2**
shape **(b)** — the narrower boolean, not shape (a)'s
`RequiredReferences []ClauseRef`). The axis records the static
parser-time fact "at least one non-first occurrence of this variable
was a required (non-OPTIONAL) bare pattern", where "bare" means the
occurrence sits alone in its pattern element (no adjacent edge chain
link); zero value `false` is the wire-absent default meaning "no
required bare re-reference recorded", so every non-bare-reused golden
is byte-identical under the campaign's omit-when-false convention
(ADR 0008 hk0 amendment).

This brief is the **contract for the whole gqlc-5xg cycle**: it spans
the spec PR (this file), the model-change PR (model + parser + ADR 0008
amendment + parser-pin and parser-golden verification; resolver
untouched and green), the resolver-widening PR (bare-ref demotion in
`demoteNullableInPlace`; zero existing resolver goldens rebaseline,
four new fixtures land), and a docs-errata PR retiring the
"silently under-demoted / gqlc-5xg" rows across the R4–R7 stage
specs. All code PRs land under ADR 0008's later revision
protocol — additive-only, dated amendment, golden rebaseline whose
diff shows only the new surface.

**Class B is the last outstanding planned change bead.** ay9 closed
Class A (OPTIONAL-clause-sibling gap) on 2026-07-10 by adding the
`OptionalGroup` axis and widening the resolver to a group-closure
fixed point. 5xg closes Class B (same-Part second-reference gap) by
recording the required-bare-pattern re-reference on the binding it
targets — the missing-witness content the query model discarded at
`mergeBinding` — and widening the resolver to demote any binding
carrying that flag. The two axes are orthogonal: a binding can be
group-demoted (ay9) AND bare-ref-demoted (5xg) simultaneously; both
flip the same table entry to `false`, so the widenings compose
without ordering constraints (§8.1).

---

## 1. Deliverables

Spec cycle (Cycle 1) — this PR:

- `docs/specs/model-change-5xg-required-bare-ref.md` — this file.

Cycle ( 2, follow-up PR):

- `internal/query/query.go` — one additive field
  `referencedInRequiredBarePattern bool` on `NodeBinding`
  (tail-append after `optionalGroup`, struct at `query.go:313-318`)
  and on `EdgeBinding` (tail-append after `optionalGroup`, struct at
  `query.go:399-408`); one new accessor
  `ReferencedInRequiredBarePattern() bool` per variant; two widened
  `MarshalJSON` structs (`query.go:1686-1694` node,
  `:1703-1715` edge) gaining a trailing
  `ReferencedInRequiredBarePattern bool \`json:"referencedInRequiredBarePattern,omitempty"\``
  key. **No new constructor**: the flag is a post-introduction axis
  the parser records at merge time on the *existing* binding —
  §3.2 walks the constructor-strategy decision (post-introduction
  axis ⇒ mutator-shaped, not ctor-shaped). §4.1.
- `internal/query/query_test.go` — new tests pinning the mutator's
  true side, the accessor's zero side, the wire fragment on the true
  side, and the omit-when-false wire on the false side. The 10
  pre-existing `NewNullable*` pins and the ay9 `*InGroup` pins stay
  verbatim — they pin the flag's zero-value default. §5.
- `internal/query/cypher/pattern.go` — `collectPatternElement`
  (`pattern.go:143-172`) computes `headBare := len(chain) == 0` once
  per pattern element and passes it as a new `bare bool` parameter
  to `collectNode` for the head node call at `:152`; the loop's
  non-head `collectNode` call at `:166` passes `bare=false`
  unconditionally (a node with an adjacent edge on either side is
  never bare). `collectNode` (`pattern.go:188-201`) gains the `bare`
  parameter and forwards it to `mergeBinding`. `mergeBinding`
  (`pattern.go:388-404`) gains the `bare bool` parameter and on the
  merge arm (`rb := part.bindings[idx]`, `:398-403`) sets
  `rb.referencedInRequiredBarePattern = true` iff `group == 0 &&
  bare` (required-clause bare re-reference; the first-introduction
  arm never sets it — that's a first occurrence). §4.2.
- `internal/query/cypher/listener.go` — `rawBinding`
  (`listener.go:157-176`) gains one field
  `referencedInRequiredBarePattern bool`, tail-appended after `hops`;
  parser-internal. §4.2.
- `internal/query/cypher/build.go` — `toBinding`
  (`build.go:264-285`) forwards
  `rb.referencedInRequiredBarePattern` onto the constructed binding
  via a post-construction mutator call
  (`b.markReferencedInRequiredBarePattern()`), applied uniformly on
  every arm (node ctor, edge ctor, var-length edge ctor,
  OPTIONAL-introduced × var-length variants — all six routes). The
  mutator is package-private on the sum variants; the mutator on a
  binding whose `rawBinding.referencedInRequiredBarePattern` is
  false is a no-op (checked, so no wire drift on the common case).
  §4.3.
- `internal/query/cypher/parser_test.go` — **zero pins flip.** No
  TCK-derived mustParse pin's source contains a same-Part
  bare-re-reference of an OPTIONAL-introduced variable (§4.4
  census). Two new pins land, exercising the new axis's true side:
  `"required bare re-reference sets flag on optional node"`
  (§5.1.1) and `"required bare re-reference not set for edge chain
  re-reference"` (§5.1.2, kill-probe). §5.1.
- Parser goldens (`internal/query/cypher/testdata/golden/`,
  **3199 files** at branch base `d8d2818`) — **zero flip.** Full
  census in §4.4: no TCK-vendored query exercises the same-Part
  bare-re-reference-of-OPTIONAL shape (14 OPTIONAL-carrying
  query-shape classes, none same-Part-bare-re-referencing), so no
  golden gains the new key. §4.4 pins the discovery method and the
  0-file result; a rebaseline touching any golden FAILS review.
- `docs/adr/0008-query-model-surface-resolver-api.md` — one dated
  amendment note (2026-07-11, top of file, above the ay9 note) plus
  one closed-out "Known deferred additions" entry.
  `ReferencedInRequiredBarePattern` does **not** appear on the ADR's
  current Known-deferred-additions list (verified at branch base —
  the list names shortestPath, EXISTS Use precision, CreateEffect
  split, and the four adopted axes hk0/fvo/0ig/ay9); the amendment
  adopts the axis directly under the additive rule, the same move
  0ig and ay9 made for their axes. Verbatim text §7.

Resolver-widening cycle (Cycle 3, follow-up PR after the change
PR merges):

- `internal/resolver/resolve.go` — `demoteNullableInPlace`
  (`resolve.go:1442-1512`, post-ay9) gains one pre-pass over
  `part.Bindings` that demotes any binding whose
  `ReferencedInRequiredBarePattern()` returns `true`. The pass runs
  before the existing per-edge/group-closure fixed-point loop and
  is independent of it: bare-ref demotion never triggers a group
  demotion in `demotedGroups` (the two axes are orthogonal, §8.1),
  and the fixed-point loop's monotonicity is unaffected by a
  pre-flipped table entry. No signature change, no new sentinel, no
  wire change on `ValidatedQuery`. §8.1.
- Resolver goldens — **zero flips.** All 126 valid fixtures and 50
  invalid fixtures are byte-identical: no existing fixture exercises
  the same-Part bare-re-reference-of-OPTIONAL shape (grep-verified
  §8.3). The four new fixtures land the shape and its kill-probes.
- Four new valid fixtures + goldens + `schema.mapping.json` entries
  (all against the existing `social_r4.gql`): the canonical
  Class B case, its labelled/bare-with-label variant, an OPTIONAL
  bare re-reference kill-probe, and the compose-with-ay9-group
  witness. §8.4.

Docs-errata cycle (Cycle 4, rides with or immediately after the
widening PR):

- `docs/specs/resolver-stage-r4.md` — §4.4 "Pass 2" prose,
  §4.4.2 pseudocode note (`:657-660`), §6.3 fixture note
  (`:1042-1044`), §7.4 item 2 (`:1239-1256`), §7.5.3 Class B block
  (`:1367-1397`), §7.5.4 Axis 2 (`:1443-1470`), §7.5.5 bead 2
  (`:1531-1535`, `:1549-1552`), §7 table row `:1191`, get
  closed-out successor prose.
- `docs/specs/resolver-stage-r5.md` — Class B rows at `:37-46`
  (§1 banner), `:613` (§4.1.1 table), `:1713-1721` (§4.6.1),
  `:2321` (§7 table), `:2401`, `:2418`, `:2501`, `:2618`, `:2705`,
  `:2718`.
- `docs/specs/resolver-stage-r6.md` — Class B rows at `:29-30`
  (§1 banner), `:1883` (§7 table), `:2014`, `:2270`.
- `docs/specs/resolver-stage-r7.md` — Class B rows at `:56-57`
  (§1 banner), `:2189` (§7 table), `:2336-2340` (§7.1.2 prose),
  `:2362`, `:2527`.

Every Class-A / gqlc-ay9 row is already closed (ay9 close-out PR
#130); Class-B row retirement here is the campaign's final planned
stage-spec sweep.

Nothing downstream of the resolver is built; codegen consumes the
widened nullability under a future ADR.

---

## 2. Frame — what changes and what stays

### 2.1 The gap in one paragraph

`query.Query` records `Nullable()` per binding as a static
first-introduction fact (`internal/query/query.go:373-375` node,
`:524-526` edge; ADR 0006) and, since ay9, records the introducing
OPTIONAL clause's group id via `OptionalGroup()` (`:377-383` node,
`:528-532` edge). But it does not record what happens to a binding
*after* its first introduction. Specifically: when the same variable
appears again in the same Part in a required (non-OPTIONAL) bare
pattern, `mergeBinding` (`internal/query/cypher/pattern.go:388-404`)
dedupes the second occurrence into the existing binding and keeps
only the label union — the re-reference itself is discarded (§3.1).
This is the missing-witness gap R4 §7.5.3 names as **Class B**:
sibling demotion (Class A, ay9) walks bindings that are all present
in the model; bare-ref demotion (Class B, 5xg) needs a fact about a
reference the query model does not preserve. Canonical example (R4
§7.5.1, encoded as fixture
`test/data/resolver/valid/demote_bare_reference_from_optional.cypher`,
new §8.4):

```
OPTIONAL MATCH (a:Person)-[:AUTHORED]->(b:Post) MATCH (b) RETURN b
```

Required bare `MATCH (b)` filters NULL-`b` rows (label check on NULL
is NULL → row-drop under WHERE-like semantics); ideal flow-typing
demotes `b`. The pre-5xg landed golden (§8.3) would pin the
under-approximation: `b` carries `"nullable": true`. After the 5xg
axis + resolver widening, `b` carries `"nullable": false`.

`a` stays nullable in this scenario: no required bare re-reference of
`a`, and ay9's group closure has no proven member of `a`'s OPTIONAL
group (`b` is proven by the bare re-ref, but bare-ref demotion is
orthogonal to `demotedGroups` — §8.1 explains the deliberate
separation). The chain-cascade fixture (§8.4 fixture 4) is where 5xg
and ay9 *do* compose.

### 2.2 The one wire axis — additive, omit-when-false

**One field on two sum variants**:

- `NodeBinding` gains `referencedInRequiredBarePattern bool`.
- `EdgeBinding` gains `referencedInRequiredBarePattern bool`.

Semantics: `false` = "no required bare pattern re-referenced this
binding" (the Go zero value; the wire-absent form; every binding at
first introduction; every binding that is not re-referenced or that
is re-referenced only in edge-chain positions or in OPTIONAL
clauses). `true` = "at least one non-first occurrence of this
variable in the same Part was a required (non-OPTIONAL) bare pattern"
(§2.3 pins the "bare" and "required" predicates).

Parser-side invariant: for every binding produced by the parser,
`ReferencedInRequiredBarePattern() == true` implies at least two
distinct occurrences of the variable exist in one Part, the first
introducing the binding and at least one subsequent occurrence in a
required bare pattern. The flag is monotone: once set, it stays true
for the life of the parse (a later re-reference cannot unset it —
`mergeBinding` only writes `true`, never `false`). It is silent about
first-introduction: a binding first-introduced in a required bare
pattern (e.g. `MATCH (n) RETURN n`) has the flag false unless a later
same-Part bare re-reference sets it (which would be `MATCH (n) MATCH
(n) RETURN n` — a legal but pathological shape).

The JSON encoding is **omit-when-false** (`,omitempty` on `bool`),
following the wire convention hk0 established
(omit-when-false; the direct precedent: `ContainsAggregate`), fvo
continued (omit-when-zero-int — `Use.part`), 0ig continued
(omit-when-zero-length — `CallBinding.args`), and ay9 continued
(omit-when-zero-int — `optionalGroup`). A binding not
required-bare-re-referenced has byte-identical wire to the pre-5xg
wire; only bindings with the flag set gain the key. §4.4 counts the
corpus under this encoding: **zero** parser goldens flip (the TCK
does not exercise the shape); all 3199 are byte-identical.

### 2.3 What "bare" and "required" mean — the pinned definitions

The parser sets the flag on `mergeBinding`'s merge arm iff the
current occurrence satisfies **both**:

- **required**: the enclosing clause is a non-OPTIONAL `MATCH` (i.e.
  `group == 0` at the `mergeBinding` call site, §4.2.3's decision
  table row R). `OPTIONAL MATCH` occurrences never set the flag —
  `EnterOC_Match` mints a fresh `group > 0` for the OPTIONAL clause
  and threads it through `collectPattern` (`listener.go:271-284`,
  ay9). `CREATE` and `MERGE` also pass `group == 0` (they enter via
  the same `collectPattern` path with `group=0`, `listener.go:372`
  and `:397`), but their pattern element chains for a *re-referenced
  variable* either introduce a new required binding (kind conflict or
  legitimate reuse) or fall under Stage 12 write semantics — either
  way, a bare re-reference under CREATE/MERGE that reaches
  `mergeBinding`'s merge arm sets the flag legitimately (a required
  bare-reference-plus-write yields the same demotion witness as under
  MATCH — the row-drop-on-NULL invariant is preserved). §2.5 pins
  this as intentionally in-scope; the alternative (guard the flag on
  MATCH only) would need a `writeContext bool` threaded through
  `mergeBinding`, and no fixture demonstrates the guard's benefit.
- **bare**: the current occurrence sits in a pattern element with no
  adjacent edge chain link on either side. Pinned by the parser's
  chain traversal in `collectPatternElement`
  (`pattern.go:143-172`): the head node `prev` is bare iff
  `len(e.AllOC_PatternElementChain()) == 0` (no relationship link
  follows it); every non-head node has an adjacent edge on its left
  (it is the target of the immediately-preceding chain link), so is
  never bare. §4.2.1 pins this as the "head-with-empty-chain rule",
  and §4 §4.2.4 gives a full decision table.

The pattern-shape decision table for a NODE variable `v`:

| Pattern occurrence | Bare? | Required? (assumes non-OPTIONAL clause) | Flag set on merge? |
|---|:---:|:---:|:---:|
| `MATCH (v)` | yes (empty chain) | yes | **yes** |
| `MATCH (v:Label)` | yes (empty chain; label filter qualifies) | yes | **yes** |
| `MATCH (v {p: 'x'})` | yes (empty chain; property filter qualifies) | yes | **yes** |
| `MATCH (v:Label {p: 'x'})` | yes (combined filter) | yes | **yes** |
| `MATCH (v)-[r:R]->(w)` | no (v has adjacent edge right) | yes | no |
| `MATCH (u)-[r:R]->(v)` | no (v has adjacent edge left; non-head) | yes | no |
| `MATCH (u)-[r:R]->(v)-[s:S]->(w)` | no (v adjacent edges both sides) | yes | no |
| `OPTIONAL MATCH (v)` | yes | no (OPTIONAL clause) | no |
| `OPTIONAL MATCH (v)-[r]->(w)` | no | no | no |

For an EDGE variable, "bare" is grammatically-unreachable: an edge
always appears inside `-[...]-` between two node positions, so every
edge occurrence has adjacent nodes on both sides. The flag on
`EdgeBinding` is added for wire symmetry with `NodeBinding` (§2.5),
but its true side is unreachable via parse today.

Anonymous nodes (`MATCH ()`, `MATCH (:Label)`) never enter
`mergeBinding` (`collectNode` short-circuits when `variable == ""`,
`pattern.go:197`); the flag mechanism never applies to anonymous
positions. Anonymous edges enter `collectEdge`'s literal-`rawBinding`
path at `pattern.go:300-305`, not `mergeBinding`.

### 2.4 The resolver-widening semantics — pre-pass bare-ref demotion

The widening lives entirely in `demoteNullableInPlace`
(`resolve.go:1442-1512`), called once per Part at Phase D
(`resolve.go:357-366`). Widened rule, replacing the ay9-widened
rule's seed step:

> A binding `b` is effectively non-nullable iff derivable from:
> (i) `b.Nullable() == false` (parser-static, seed);
> (ii) `b.ReferencedInRequiredBarePattern() == true` (5xg —
> **new** — a required bare re-reference filters NULL-`b` rows);
> (iii) `b` is a named `VarEndpoint` of an *effectively required*
> edge `e` in `part.Bindings` that passes the §4.4.3 hop gate
> (`qualifiedDemoter`, `resolve.go:1527-1537`, unchanged); or
> (iv) `b.OptionalGroup() == g ≥ 1` and any binding with group `g`
> is effectively non-nullable (ay9 — group closure).

(i) and (iii) are R4's original two rules verbatim. (iv) is ay9's
group closure verbatim. (ii) is the new 5xg pre-pass. The rules
compose commutatively: (ii) flips table entries for bare-re-referenced
bindings independently of (iv)'s `demotedGroups` map; (iv) may
subsequently observe those flipped entries and demote co-introduced
siblings. The reverse composition is also legal — (iv) may flip
entries that (ii) has already flipped, a no-op — but the deliberate
implementation order runs (ii) first as a pre-pass because it depends
on no dynamic state (it reads only per-binding flags), so the
fixed-point loop can assume the bare-ref demotions are already
seeded. §8.1 gives the exact Go snippet.

Monotone termination is unchanged: (ii) flips a finite set of table
entries `true → false`; (iv) flips a finite subset of the same;
neither can flip back. The lattice invariant (R4 §4.2 — never demote
incorrectly) holds: every derivation step is justified by left-join
row semantics (§2.1), so the widening stays a non-breaking refinement
— valid queries stay valid, no column changes except `Nullable` bits
moving true→false.

### 2.5 What this cycle does NOT touch

- **Non-bare edge re-references** — an OPTIONAL-introduced edge
  variable re-referenced in an edge pattern
  (`OPTIONAL MATCH (a)-[r:R]->(b) MATCH (a)-[r]->(c) RETURN r`) has
  its second occurrence's endpoints DROPPED by `mergeBinding` (the
  first-introduction-only endpoint rule, `pattern.go:392`). This is
  the same missing-witness shape Class B addresses at the node
  level, but the resolver could already prove `r`'s existence from
  the second `-[r]->` occurrence's own endpoint-adjacency if it
  were preserved — a strictly stronger demotion channel than the
  bare-node case. The narrower `bool` this cycle adopts does not
  distinguish "which pattern shape re-referenced this binding" and
  intentionally does not close the edge-pattern case. Recording it
  would need shape (a)'s `RequiredReferences []ClauseRef` axis (R4
  §7.5.4). Filed as a residual at close-out; not blocking codegen.
- **CREATE / MERGE bare re-reference nuances** — §2.3 admits
  CREATE/MERGE-reached merge-arm calls to set the flag; the R6
  write pass reads the demoted `Nullable()` after this widening.
  No sentinel or wire change downstream.
- **`Binding` interface stays sealed** at `Kind()` / `Nullable()`
  / `isBinding()` (`query.go:297-306`).
  `ReferencedInRequiredBarePattern` is a per-variant
  field-and-accessor concern (the hk0/fvo/0ig/ay9 layering pattern).
- **Parser sentinels** — no new fail-site; the flag is derived
  state, never validated at parse (there is nothing to reject: a
  required bare re-reference is a well-formed query, its demotion
  is what 5xg adds).
- **Resolver sentinels** — none added or widened (§8.2).
- **`PathBinding` / `UnwindBinding` / `CallBinding`** wire —
  byte-identical (`PathBinding.Nullable()` is hardcoded false,
  `query.go:766-768`; UNWIND has no bare-re-reference form —
  `collectUnwind` handles a fresh AS-variable; CALL YIELD is not
  re-referenceable in a bare pattern — the yielded variable enters
  the part's scope but its re-reference would be a fresh binding
  under `mergeBinding` kind conflict with the CallBinding).
- **Class A / gqlc-ay9 / Axis 1** — closed 2026-07-10. The
  `OptionalGroup` axis and group-closure widening are unchanged by
  this cycle; §8.1's pre-pass runs BEFORE the ay9 fixed-point loop
  and does not touch `demotedGroups`. Every ay9 golden byte
  survives.

---

## 3. Mining — what the repo records today

### 3.1 The re-reference discard site — re-verified line refs

R4 §7.4 item 2 cites `internal/query/cypher/pattern.go:373-401` for
the merge rule. Re-derived at branch base `d8d2818`:

- `mergeBinding` — `pattern.go:388-404` (drifted from R4's
  `:373-401`; the doc comment now starts at `:376`, ay9 amended it
  to reference `optionalGroup`). Merge arm at `:398-403`: fetches
  the existing `rawBinding` by `part.byVar[variable]`, verifies the
  kind matches (`ErrVariableKindConflict` at `:400`), then merges
  labels only. The `source`, `target`, `group`, `undirected`, and
  `hops` parameters are silently dropped on the merge arm — this is
  the ADR 0006 first-introduction discipline the merge rule pins.
  §4.2.3 is where 5xg's insertion sits: after label merge, an
  additional line sets `rb.referencedInRequiredBarePattern = true`
  iff `group == 0 && bare`.
- The `mergeBinding` call sites — three, all in `pattern.go`:
  - `collectNode :198`, currently
    `l.mergeBinding(variable, graph.Node, nodeLabels(...), nil, nil,
    group, false, nil)` — the head node of a pattern element and
    every intermediate/target node of a chain link. This is the
    site the `bare` parameter threads through.
  - `collectEdge :307`, currently
    `l.mergeBinding(variable, graph.Edge, labels, source, target,
    group, !directed, hops)` — every named edge. Edges are never
    bare (§2.3), so the parameter passes `false`.
  - No other caller (grep-verified).
- `collectPatternElement` — `pattern.go:143-172`. Head node
  processed at `:151-155`, chain loop at `:157-171`. The head
  node's bareness is exactly `len(e.AllOC_PatternElementChain()) ==
  0`, captured once before the head `collectNode` call. Non-head
  nodes are always the target of the preceding chain link, so
  their `bare` parameter is unconditionally `false`. §4.2.1 pins
  the code shape.

### 3.2 Constructor strategy — post-introduction axis, mutator-shaped

Precedents: hk0/fvo/0ig/ay9 each landed **first-introduction** axes
using new smart constructors — the fact is known at construction, so
a new ctor takes it as an argument and the preserved ctors mint the
zero value. 5xg is the first **post-introduction** axis: the fact is
derived only after `mergeBinding` sees a re-reference, which runs
against a `rawBinding` (`listener.go:157-176`) — not yet lowered to
a `query.Binding` sum variant.

**Decision: single unexported mutator on each sum variant, called by
`toBinding` after construction, gated on the raw flag.** Grounds:

1. **Semantically post-construction.** No constructor has enough
   information: at first-introduction time no re-reference has
   occurred. A ctor parameter would always be `false` at first
   introduction, or would force a two-phase construction on
   re-reference — either breaks the "value type, no post-construction
   mutation" pin the sum variants otherwise maintain.
2. **Additive-in-protocol.** ADR 0008 admits new accessors and
   unexported field additions; an unexported mutator called only
   from the package's own `build.go` is on the same footing as an
   unexported field, visible only through the accessor.
3. **Blast radius.** One call site per variant (`toBinding`,
   `build.go:264-285`), and only when the raw flag is true. Zero
   call-site edits at coupled consumers.

The mutator is named `markReferencedInRequiredBarePattern` on
each variant (unexported, so package-local convention takes any
name; the descriptive verb matches the later "mark"
convention 0ig chose for its post-construction paths). Its body
is one field write:

```go
func (b *NodeBinding) markReferencedInRequiredBarePattern() {
    b.referencedInRequiredBarePattern = true
}
```

The pointer receiver is admissible because `toBinding` holds the
value locally before appending to `part.bindings` (it does not
observe the mutation on any prior copy). §4.3 pins the call
snippet.

Alternatives rejected: **(a) extra trailing-parameter on every
constructor** — a rename under the additive protocol (ay9 §3.2 gave
the same argument). **(b) new `NewNodeBindingWithBareRef` family** —
adds six exported symbols with a use pattern no external caller has
(the flag isn't set-able before parse completes); no benefit over
the mutator.

### 3.3 The "bare" predicate — where the fact is derivable

Two candidate sites:

- **In `collectPatternElement`**, before the head `collectNode`
  call, compute `headBare := len(chain) == 0` and thread it as a
  new `bare bool` parameter to `collectNode`. **Chosen.**
- **In `mergeBinding`**, take a `bare bool` parameter and rely on
  the callers to compute it. Rejected because the fact is
  chain-shape-local — only `collectPatternElement` knows the chain
  length; `collectNode` cannot derive it from the
  `IOC_NodePatternContext` alone (the node pattern context does
  not know about the surrounding element's chain).

Threading `bare` from `collectPatternElement` through `collectNode`
into `mergeBinding` is a one-parameter addition on three signatures.
No new state on `listener`, no new `curPart` field. §4.2 gives the
exact signatures.

### 3.4 Zero-value analysis

- **Go side.** `referencedInRequiredBarePattern` zero value is
  `false` = "not required-bare-re-referenced". A `NodeBinding{}` or
  any binding from the seven preserved ctors (six original + one
  ay9 InGroup per variant) is bit-identical to its pre-5xg value
  plus a zero field — `require.Equal` / `reflect.DeepEqual` pins
  over non-bare-re-referenced constructions are untouched (the
  mechanism behind §4.3's 149 preserved parser_test pins).
- **Legacy nullable + flag false.** Reachable at construction:
  every pre-5xg binding was built with the flag implicitly false
  (the field didn't exist). Post-5xg, the same binding is
  bit-identical (the mutator is only called when the raw flag is
  true). The resolver treats it exactly as R4/ay9 did: nullable if
  `Nullable() == true`, no bare-ref pre-pass demotion. This is the
  correct degraded semantics, pinned by a unit test (§5.2).
- **Wire side.** `,omitempty` on `bool` elides `false`. A
  non-bare-re-referenced golden's bytes cannot change; a
  bare-re-referenced binding gains one key. Since the TCK does not
  exercise the shape (§4.4), no parser golden changes. Real bytes
  from a hand-constructed witness — the node binding of a
  `demote_bare_reference_from_optional`-shape test binding (§5):

  ```json
  {
    "kind": "node",
    "variable": "b",
    "labels": ["Post"],
    "nullable": true,
    "optionalGroup": 1
  }
  ```

  post-change (tail-append, §4.1.3):

  ```json
  {
    "kind": "node",
    "variable": "b",
    "labels": ["Post"],
    "nullable": true,
    "optionalGroup": 1,
    "referencedInRequiredBarePattern": true
  }
  ```

  A binding without the required bare re-reference retains its
  pre-5xg bytes byte-for-byte.

---

## 4. The change — model and parser changes

### 4.1 `internal/query/query.go`

#### 4.1.1 Fields

`NodeBinding` (`query.go:313-318`) appends:

```go
referencedInRequiredBarePattern bool // set by mergeBinding when a later same-Part occurrence is a required bare pattern (5xg)
```

`EdgeBinding` (`query.go:399-408`) appends the same field after
`optionalGroup`. Tail-append is the campaign convention (ay9
appended `optionalGroup` last; 0ig appended `args`).

#### 4.1.2 Accessors + mutator

```go
// ReferencedInRequiredBarePattern reports whether at least one
// non-first occurrence of this variable in the same Part was a
// required (non-OPTIONAL) bare pattern (5xg, ADR 0008 amendment
// 2026-07-11). A bare pattern is one whose element has no adjacent
// edge chain link on either side — MATCH (v), MATCH (v:Label),
// MATCH (v {prop: 'x'}) — where the re-referenced variable's row is
// filtered on NULL (label / property lookup on NULL → row-drop).
// The resolver's 5xg widening reads this to demote the binding
// non-nullable in the local table (parallel to the OPTIONAL-group
// closure ay9 added, orthogonal in its effect on demotedGroups).
func (b NodeBinding) ReferencedInRequiredBarePattern() bool {
    return b.referencedInRequiredBarePattern
}

func (b EdgeBinding) ReferencedInRequiredBarePattern() bool {
    return b.referencedInRequiredBarePattern
}
```

Mutators (unexported, package-local; called only by
`internal/query/cypher/build.go`'s `toBinding`):

```go
// markReferencedInRequiredBarePattern sets the flag on a binding
// whose raw form recorded a same-Part required bare re-reference
// (5xg). The mutator is unexported and called at exactly one site
// (toBinding, build.go:264-285); the accessor is the only exported
// read path. Public callers of the sum-variant constructors cannot
// reach the mutator, so first-introduction values remain immutable
// through their public API.
func (b *NodeBinding) markReferencedInRequiredBarePattern() {
    b.referencedInRequiredBarePattern = true
}

func (b *EdgeBinding) markReferencedInRequiredBarePattern() {
    b.referencedInRequiredBarePattern = true
}
```

The six existing constructors (`NewNodeBinding`,
`NewNullableNodeBinding`, `NewNullableNodeBindingInGroup`,
`NewEdgeBinding`, `NewNullableEdgeBinding`,
`NewNullableEdgeBindingInGroup`, `NewVarLengthEdgeBinding`,
`NewNullableVarLengthEdgeBinding`,
`NewNullableVarLengthEdgeBindingInGroup`) are **byte-identical** —
including doc comments — so every API consumer is
untouched and every group-0-and-flag-false pin is preserved.

#### 4.1.3 JSON — omit-when-false on `referencedInRequiredBarePattern`

`NodeBinding.MarshalJSON` (`query.go:1686-1694`) and
`EdgeBinding.MarshalJSON` (`:1703-1715`) each append one struct
field:

```go
ReferencedInRequiredBarePattern bool `json:"referencedInRequiredBarePattern,omitempty"`
```

as the **last** key (node key order becomes `kind, variable,
labels, nullable, optionalGroup, referencedInRequiredBarePattern`;
edge: `kind, variable, labels, source, target, nullable, directed,
hops, optionalGroup, referencedInRequiredBarePattern`). The
later omit-when-false convention (hk0's `ContainsAggregate`,
the direct precedent) is followed; the before Stage 14 always-emit
convention (`nullable`, `directed`, `hops`) does not apply — ADR
0008's hk0 amendment fixed omit-when-zero-value as the later
convention precisely so additive cycles do not force
near-total rebaselines.

### 4.2 Parser threading

#### 4.2.1 `pattern.go` — the `bare` parameter and its computation

`collectPatternElement` (`pattern.go:143-172`) computes `headBare`
once per element:

```go
func (l *listener) collectPatternElement(e gen.IOC_PatternElementContext, group int) {
    for e != nil && e.OC_NodePattern() == nil {
        e = e.OC_PatternElement() // unwrap '(' patternElement ')'
    }
    if e == nil {
        return
    }

    chain := e.AllOC_PatternElementChain()
    headBare := len(chain) == 0 // 5xg: node is bare iff no chain link follows

    prev := e.OC_NodePattern()
    l.collectNode(prev, group, headBare)
    if l.err != nil {
        return
    }

    for _, link := range chain {
        next := link.OC_NodePattern()
        l.collectEdge(link.OC_RelationshipPattern(), prev, next, group)
        if l.err != nil {
            return
        }
        // A next node is the target of an edge, so it always has an
        // adjacent chain link on its left: never bare.
        l.collectNode(next, group, false)
        if l.err != nil {
            return
        }
        prev = next
    }
}
```

The single derivation `len(chain) == 0 ⇒ headBare == true` is the
parser's whole "bare" pin. §2.3's decision table maps every
grammatically-reachable shape to a decision by inspection of this
computation.

`collectNode` (`pattern.go:188-201`) becomes:

```go
func (l *listener) collectNode(n gen.IOC_NodePatternContext, group int, bare bool) {
    if n == nil {
        return
    }
    variable := ""
    if v := n.OC_Variable(); v != nil {
        variable = v.GetText()
    }
    l.mineInlineMap(variable, n.OC_Properties())
    if variable != "" && !l.nameBoundAsUnwind(variable) {
        l.mergeBinding(variable, graph.Node, nodeLabels(n.OC_NodeLabels()), nil, nil, group, false, nil, bare)
    }
    l.recordPathNode(variable)
}
```

The `bare` parameter is forwarded to `mergeBinding`; it is unused
outside the merge case (an anonymous node has no `mergeBinding`
call, so `bare` is silently ignored — no state leak).

#### 4.2.2 `pattern.go` — `collectEdge` passes `bare=false`

`collectEdge` (`pattern.go:253-310`) calls `mergeBinding` at `:307`
with `bare=false`:

```go
if !l.nameBoundAsUnwind(variable) {
    l.mergeBinding(variable, graph.Edge, labels, source, target, group, !directed, hops, false)
}
```

Every named edge is inside a `-[...]-` grammar production, which by
definition sits between two node positions. An edge is never bare
in the openCypher grammar (§2.3), so the parameter is a compile-time
constant `false` at this site.

The anonymous-edge literal at `:300-303` sets no bare-ref flag
(anonymous, no `mergeBinding` call, no `mergeBinding` merge arm to
set anything). The `rawBinding` literal at `:300` does not need the
new field set at construction — `false` is the Go zero value.

#### 4.2.3 `pattern.go` — the `mergeBinding` decision table

`mergeBinding` (`pattern.go:388-404`) gains the `bare bool`
parameter (last position, additive) and, on the merge arm, sets the
raw flag under the pinned condition:

```go
func (l *listener) mergeBinding(variable string, kind graph.EntityKind, labels graph.LabelSet, source, target query.Endpoint, group int, undirected bool, hops *query.EdgeHops, bare bool) {
    part := l.curPart
    idx, ok := part.byVar[variable]
    if !ok {
        rb := &rawBinding{variable: variable, kind: kind, seen: map[string]bool{}, source: source, target: target, optionalGroup: group, undirected: undirected, hops: hops}
        rb.mergeLabels(labels)
        part.byVar[variable] = len(part.bindings)
        part.bindings = append(part.bindings, rb)
        return
    }
    rb := part.bindings[idx]
    if rb.kind != kind {
        l.fail(fmt.Errorf("%w: %q", ErrVariableKindConflict, variable))
        return
    }
    rb.mergeLabels(labels)
    if group == 0 && bare {
        // 5xg: the current occurrence is a required (non-OPTIONAL) bare
        // pattern re-reference of a binding that was previously
        // introduced. The flag is monotone (once set, stays true), so
        // repeated bare re-references are idempotent.
        rb.referencedInRequiredBarePattern = true
    }
}
```

Decision table for the merge arm (assumes existing binding at
`part.byVar[variable]`, kind matches — non-conflict path):

| Arm's `group` | Arm's `bare` | Rationale | Flag write? |
|:---:|:---:|---|:---:|
| 0 (required clause) | true (bare re-ref) | required bare re-reference; witness under left-join | **flag ← true** |
| 0 (required clause) | false (edge-chain re-ref) | required chain, but re-referenced position has an adjacent edge; existing R4 rule already demotes via edge endpoint | no write |
| > 0 (OPTIONAL clause) | true (bare re-ref) | second occurrence is also OPTIONAL; row can be NULL for both | no write |
| > 0 (OPTIONAL clause) | false | second occurrence is chained inside OPTIONAL; ay9's group closure captures | no write |

The first-introduction arm (`!ok` at `:391`) never sets the flag —
by definition, no re-reference has occurred yet.

#### 4.2.4 `listener.go` — `rawBinding` gains the field

`rawBinding` (`listener.go:157-176`) appends after `hops`:

```go
type rawBinding struct {
    variable                        string
    labels                          graph.LabelSet
    seen                            map[string]bool
    kind                            graph.EntityKind
    source                          query.Endpoint
    target                          query.Endpoint
    optionalGroup                   int
    undirected                      bool
    hops                            *query.EdgeHops
    referencedInRequiredBarePattern bool // 5xg: set by mergeBinding's merge arm on required-bare-re-reference
}
```

Parser-internal, single source of truth. No new listener state; no
new `rawPart` field.

### 4.3 `build.go` — `toBinding` forwards the flag

`toBinding` (`build.go:264-285`, post-ay9) — the seven-arm
constructor routing (Node × {required, InGroup} × Edge ×
{single-hop, var-length} × {required, InGroup}) — is refactored to
capture the constructed binding in a local `b`, then append the
5xg mutator call uniformly across every arm:

```go
func (rb *rawBinding) toBinding() (query.Binding, error) {
    var b query.Binding
    var err error
    // ... existing ay9 seven-arm routing captured into (b, err)
    // instead of returning early ...
    if err != nil {
        return nil, err
    }
    if rb.referencedInRequiredBarePattern {
        switch bb := b.(type) {
        case query.NodeBinding:
            bb.markReferencedInRequiredBarePattern()
            b = bb
        case query.EdgeBinding:
            bb.markReferencedInRequiredBarePattern()
            b = bb
        }
    }
    return b, nil
}
```

Value-vs-pointer semantics force the type-switch: the mutator has a
pointer receiver, and `b`'s dynamic value is not addressable through
the interface. The `bb` capture rebinds to an addressable local; the
mutation is committed back via `b = bb`. The seven ay9 constructor
call sites move verbatim into the routing branches; only the
outer function shape changes.

Named paths need no change: `NewPathBinding` wraps member bindings
whose `toBinding` route already carries the flag through.

### 4.4 Parser goldens — zero flip, byte-identity fence

Discovery method (rerunnable; this is how the number below was
derived, not a paraphrase): walk every
`internal/query/cypher/testdata/golden/*.golden.json`; flag the
golden iff any `branches[].parts[].bindings[]` entry has
`"referencedInRequiredBarePattern": true` in its wire. The set is
empty iff no TCK-vendored query exercises the same-Part
required-bare-re-reference-of-an-already-introduced-variable shape.

Result at branch base `d8d2818`: **3199 total goldens, 0 flip,
3199 byte-identical.** Verification:

- Grep every TCK feature file's query bodies for the shape signature
  `OPTIONAL MATCH ... MATCH (v)` where `v` was previously bound
  (a same-Part bare re-reference). Method: extract each
  `When executing query` body, isolate multi-clause queries, look for
  a required `MATCH` clause whose pattern element is a single
  node-pattern (no adjacent chain), whose variable matches a
  previously-introduced variable.
- No TCK query matches (**30 feature files carry OPTIONAL MATCH;
  none has a same-Part required-bare-re-reference of a previously-
  introduced variable**). Sample analysis:
  - `Match7.feature` — 44 OPTIONAL clauses across 33 scenarios; all
    re-references of previously-introduced variables occur inside
    edge-chain positions (`OPTIONAL MATCH (b)-[r:R]->(c)`) or in a
    subsequent OPTIONAL clause, not a required bare.
  - `Match3.feature` — `OPTIONAL MATCH (a) WITH a MATCH (a)-->(b)`
    is cross-WITH (Part 1's `MATCH (a)-->(b)` has `a` in an edge
    chain; also cross-Part per R5). Not same-Part bare.
  - `With1.feature` — `OPTIONAL MATCH (a:A) WITH a AS a MATCH (b:B)`
    — re-reference is `b`, a fresh required-clause binding. Not a
    re-reference.
  - `MatchWhere6.feature` — `MATCH (x:X) OPTIONAL MATCH (x)-[:E1]-> ...`
    — `x` was required-first-introduced; second occurrence is
    OPTIONAL. First introduction non-nullable already; flag
    irrelevant.

The empty flip set is the fence: any parser golden change under 5xg
FAILS review. §4.4.1 gives the reviewer-side fence commands.

#### 4.4.1 Reviewer-side fence commands

From the model-change-PR branch tip:

```sh
# Fence A — rebaseline reproducibility: regenerate and expect no drift.
go test ./internal/query/cypher/ -run TestAcceptance -update
git status --porcelain internal/query/cypher/testdata/golden/
# MUST print: no changes.

# Fence B — the new key appears in zero goldens (TCK does not
# exercise the shape).
grep -rl '"referencedInRequiredBarePattern"' internal/query/cypher/testdata/golden/ | wc -l
# MUST print: 0.

# Fence C — parser tests pass with only the two new pins added.
go test ./internal/query/cypher/
# MUST print: ok; two new pins (§5.1.1, §5.1.2) run green.

# Fence D — resolver is untouched and green with zero golden churn.
go test ./internal/resolver/
git status --porcelain test/data/resolver/
# MUST print: ok + no changes.

# Fence E — no residual same-Part-bare shape entered the TCK vendor
# tree during the cycle window. If this fails, the flip set is no
# longer 0; re-run §8.3 and update the spec.
git log --since="2026-06-01" --name-only -- test/data/query/cypher/tck/features/ | wc -l
# Expect: 0 (TCK vendor is stable for the campaign).
```

---

## 5. Unit-test additions — the two-sided axis

### 5.1 New parser tests — the axis's true side

Two new mustParse pins in `internal/query/cypher/parser_test.go`,
mirroring the campaign convention (0ig §5, ay9 §5: pin both the zero
and non-zero sides of a new axis):

#### 5.1.1 `"required bare re-reference sets flag on optional node"`

```go
"required bare re-reference sets flag on optional node": {
    src: "OPTIONAL MATCH (a)-[:R]->(b)\nMATCH (b)\nRETURN b",
    want: oneBranch(query.Part{
        Bindings: []query.Binding{
            must(query.NewNullableNodeBindingInGroup("a", nil, 1)),
            must(query.NewNullableEdgeBindingInGroup("", graph.LabelSet{"R"},
                must(query.NewVarEndpoint("a")),
                must(query.NewVarEndpoint("b")),
                true,
                1,
            )),
            markBareRefNode(must(query.NewNullableNodeBindingInGroup("b", nil, 1))),
        },
        Returns: []query.ReturnItem{
            {Name: "b", Value: query.NewRefProjection(query.Ref{Variable: "b"}, query.TypeNode{})},
        },
    }),
},
```

`markBareRefNode` is a test helper (new, next to `must`) that
applies the mutator through a small addressable route — the pin
covers both the ay9-carried group id (1) and the 5xg-set flag on
`b`, without needing a public constructor. `a` and the anonymous
edge stay flag-false (the second MATCH is bare for `b` only; the
edge and `a` are not re-referenced).

#### 5.1.2 `"required chain re-reference does not set bare-ref flag"`

Kill-probe pin against the over-firing case: re-reference in an
edge chain position should NOT set the flag (existing R4 rule
already demotes via edge witness).

```go
"required chain re-reference does not set bare-ref flag": {
    src: "OPTIONAL MATCH (a)-[:R]->(b)\nMATCH (b)-[:S]->(c)\nRETURN b, c",
    want: oneBranch(query.Part{
        Bindings: []query.Binding{
            must(query.NewNullableNodeBindingInGroup("a", nil, 1)),
            must(query.NewNullableEdgeBindingInGroup("", graph.LabelSet{"R"},
                must(query.NewVarEndpoint("a")),
                must(query.NewVarEndpoint("b")),
                true,
                1,
            )),
            // b's flag is NOT set — the re-reference is chain-headed,
            // not bare. Existing R4 endpoint-witnessing demotes b via
            // the required :S edge; 5xg's axis stays out of the way.
            must(query.NewNullableNodeBindingInGroup("b", nil, 1)),
            must(query.NewEdgeBinding("", graph.LabelSet{"S"},
                must(query.NewVarEndpoint("b")),
                must(query.NewVarEndpoint("c")),
                true,
            )),
            must(query.NewNodeBinding("c", nil)),
        },
        Returns: []query.ReturnItem{
            {Name: "b", Value: query.NewRefProjection(query.Ref{Variable: "b"}, query.TypeNode{})},
            {Name: "c", Value: query.NewRefProjection(query.Ref{Variable: "c"}, query.TypeNode{})},
        },
    }),
},
```

A parser regression that flips 5xg on non-bare chain re-references
fails this pin loudly. This is the canonical Class A shape (chain
witness for `b`); Class B needs the bare form only.

### 5.2 New query unit tests — the axis's read/write shape

New tests in `internal/query/query_test.go`:

- `TestNodeBindingReferencedInRequiredBarePatternZeroDefault` — a
  binding constructed via each of the three preserved node ctors
  (`NewNodeBinding`, `NewNullableNodeBinding`,
  `NewNullableNodeBindingInGroup`) returns
  `ReferencedInRequiredBarePattern() == false`.
- `TestEdgeBindingReferencedInRequiredBarePatternZeroDefault` —
  same for the six preserved edge ctors.
- `TestMarshalJSONOmitsReferencedInRequiredBarePatternWhenFalse` —
  bindings from every preserved ctor serialise **without** the key
  (byte-level `require.NotContains`); the wire-compat fence in unit
  form.
- `TestMarshalJSONEmitsReferencedInRequiredBarePatternWhenTrue` — a
  hand-marked binding's bytes contain
  `"referencedInRequiredBarePattern":true` positioned after
  `"optionalGroup"` on a node and after `"optionalGroup"` on an
  edge. The mutator is invoked via a package-internal test helper
  (see §5.3).

### 5.3 Test helper — the addressable mutator route

The unexported mutator is not reachable from `query_test.go` under
external testing without either exposing it or using an internal-
test file. Convention chosen: **internal-test file**
(`query_internal_test.go`) — no exported API bloat, mutator stays
package-local. The helper:

```go
// package query, file query_internal_test.go
package query

// MarkBareRefForTest exposes the unexported mutator to the query
// external-tests package. It is guarded by _test.go build tag so
// it never enters production binaries.
func MarkBareRefForTest(b *NodeBinding) { b.markReferencedInRequiredBarePattern() }
```

The `internal/query/cypher/parser_test.go` pin at §5.1.1's
`markBareRefNode` helper wraps this call. The parser test suite is
external (`package cypher_test`), so the wrapper crosses the
package boundary via `query.MarkBareRefForTest` — legal because
that symbol only exists under `_test.go` build tags. This is the
same discipline used to test other unexported mutation paths in the
codebase (0ig used a similar internal-test file to pin CallBinding's
post-construction args-list attribution).

The pre-existing `TestMarshalJSONEmitsNullable`
(`query_test.go:704-723`, verbatim), the ay9-added
`TestNewNullableNodeBindingInGroup` family, and the 10 `NewNullable*`
pins stay verbatim.

---

## 6. (reserved)

Numbering aligned with the ay9/0ig template; this cycle has no
listener-vs-builder split decision to record — the mint site is
`mergeBinding`'s merge arm (§4.2.3) and there is no alternative
site to argue against, because it is the parser's only re-reference
observation point (`byVar` lookup at `pattern.go:390`).

---

## 7. ADR 0008 amendment note — the dated stage note

Verbatim text to add at the top of
`docs/adr/0008-query-model-surface-resolver-api.md`, above the ay9
note, following the established amendment format:

```markdown
> _Amendment (2026-07-11, gqlc-5xg model-change cycle): the required-
> bare-pattern re-reference axis on `NodeBinding` / `EdgeBinding` —
> R4 §7.5.4's **Axis 2 shape (b)** (the narrower boolean, not
> shape (a)'s `RequiredReferences []ClauseRef`), filed from the R4
> close-out as gqlc-5xg — is **adopted** under this ADR's additive-
> only additions convention. The wire recorded first-introduction
> facts about each binding but not what happened to that binding
> after: when the same variable appeared in a later same-Part
> clause, `mergeBinding` (`internal/query/cypher/pattern.go:388-404`)
> dedupes the re-reference and discards its shape, so the resolver
> could not distinguish a bare re-reference (row-drop witness) from
> an edge-chain re-reference (existing endpoint witness) or from
> another OPTIONAL clause (no witness). Class B
> (`docs/specs/resolver-stage-r4.md §7.5.3` item 2) was this cycle's
> target. Each of the two node/edge Binding variants gains one
> additive field `referencedInRequiredBarePattern bool` and one
> accessor `ReferencedInRequiredBarePattern() bool`. **No new
> constructor** — the axis is a post-introduction fact
> `mergeBinding`'s merge arm sets on the raw binding, forwarded
> through `toBinding` via an unexported per-variant mutator
> `markReferencedInRequiredBarePattern()`; the nine existing binding
> constructors (six pre-ay9 + three ay9 InGroup) are preserved
> verbatim and continue to mint `false`. The parser sets the flag
> iff (i) the enclosing clause is required (non-OPTIONAL, `group ==
> 0`) and (ii) the pattern element is bare (`len(chain) == 0` at
> `collectPatternElement`; a node has no adjacent edge chain link,
> so the label/property filter on NULL drops the row under
> left-join). On an edge variable the parser never sets the flag —
> an edge occurrence always sits inside `-[...]-` between two node
> positions, so the "bare" predicate is grammatically-unreachable;
> the field is added for wire symmetry and forward compatibility.
> `PathBinding` / `UnwindBinding` / `CallBinding` are untouched
> (path members carry the flag through their referenced node/edge
> bindings; UNWIND has no bare-re-reference form; CALL YIELD is not
> re-referenceable). The JSON encoding is **omit-when-false**
> (`,omitempty`), following the wire convention this ADR's
> hk0 amendment established; **zero** of 3199 parser goldens
> rebaseline (the TCK does not exercise the shape), all 3199 are
> byte-identical. The Binding interface stays sealed at
> `Kind()`/`Nullable()`/`isBinding()`; bare-ref recording is a
> per-variant field-and-accessor concern. The resolver widening
> (post-5xg PR) adds a pre-pass to `demoteNullableInPlace` that
> demotes any binding whose `ReferencedInRequiredBarePattern()`
> returns true, orthogonal to ay9's group closure — the two
> demotion channels compose commutatively (both flip the same
> table entry to false; bare-ref demotion never seeds
> `demotedGroups`). **Zero existing resolver goldens flip** (no
> in-tree fixture exercises the shape); four new fixtures land
> covering the canonical Class B case, the label-filtered variant,
> the OPTIONAL-re-reference kill-probe, and the compose-with-ay9
> witness. **Residual**: the edge-side missing-witness gap —
> non-bare edge re-references that drop endpoint state at
> `mergeBinding` — is not closed by this narrower boolean; closing
> it needs shape (a)'s `RequiredReferences []ClauseRef` axis and is
> filed as a follow-up bead at close-out. Class A / gqlc-ay9 is
> closed (2026-07-10; ADR 0008 amendment above). With gqlc-5xg
> adopted, the R4-inherited Class B row across the R4-R7 stage
> specs is retired in the docs-errata cycle (§8.6); the model-
> model-additions campaign's planned scope is fully discharged. See
> `docs/specs/model-change-5xg-required-bare-ref.md` for the full
> contract, the 0-golden flip census, the constructor-strategy
> and predicate-derivation decisions, and the fence commands._
```

The Known-deferred-additions entry (appended to the list; note
`ReferencedInRequiredBarePattern` was **not** previously on the
list — the entry is added closed-out, the same move ay9 and 0ig
made for their axes):

```markdown
- **`ReferencedInRequiredBarePattern` axis on `NodeBinding` /
  `EdgeBinding`** — adopted 2026-07-11 (see the amendment note
  above and `docs/specs/model-change-5xg-required-bare-ref.md`).
  Populated parser-side by `mergeBinding`'s merge arm at
  `internal/query/cypher/pattern.go:388-404` when the current
  occurrence is required and bare; consumed by the resolver's
  Phase D bare-ref pre-pass in `demoteNullableInPlace`
  (`internal/resolver/resolve.go` — see the 5xg resolver-widening
  PR), closing R4 §7.5.3 Class B (item 2). The edge-side non-bare
  missing-witness residual is filed as a follow-up bead at
  close-out. With ay9 (Class A, closed 2026-07-10), R4 §7.5.3's two
  named classes are closed; the planned model-model-additions campaign is
  discharged.
```

---

## 8. Resolver widening — the follow-up PR after the change merges

The model-change PR lands with the resolver green and unchanged: the
kernel reads `Nullable()` and `OptionalGroup()`,
`ReferencedInRequiredBarePattern()` is populated but unconsumed. The
widening PR then extends Phase D. This section pins the semantics;
the PR implements them.

### 8.1 `demoteNullableInPlace` — the exact widening

Current (`resolve.go:1442-1512`, post-ay9): one pre-collection pass
over `part.Bindings` recording group membership, then a fixed-point
loop that demotes per-edge endpoints and closes over demoted groups.
Widened (5xg pre-pass added before the existing group scan):

```go
// demoteNullableInPlace runs the ay9+5xg-widened regime-(a) demotion
// on part.Bindings against a pre-seeded table: bare-ref demotion
// (5xg — a required bare re-reference is a witness, flipping the
// re-referenced binding's table entry directly), plus per-edge
// endpoint witnessing (R4 §4.4), plus OPTIONAL-group closure (ay9).
// The 5xg pre-pass runs before the group-closure fixed point and
// does not touch demotedGroups; the two demotion channels are
// orthogonal (both write false to the same table, both are monotone,
// composition is order-independent). The subsequent fixed-point loop
// may observe 5xg's flipped entries and demote co-introduced siblings
// via (iv), producing the compose-with-group cascade §8.4 fixture 4
// witnesses.
func demoteNullableInPlace(bindings []query.Binding, table map[string]bool) {
    // 5xg pre-pass: bare-ref demotion. A binding whose parser-time
    // flag is true was re-referenced in a required bare pattern; the
    // row-drop witness demotes it. Anonymous bindings (v == "") skip
    // — they carry no table entry.
    for _, b := range bindings {
        switch bb := b.(type) {
        case query.NodeBinding:
            if bb.ReferencedInRequiredBarePattern() && bb.Variable() != "" {
                if _, present := table[bb.Variable()]; present {
                    table[bb.Variable()] = false
                }
            }
        case query.EdgeBinding:
            if bb.ReferencedInRequiredBarePattern() && bb.Variable() != "" {
                if _, present := table[bb.Variable()]; present {
                    table[bb.Variable()] = false
                }
            }
        }
    }
    // ay9 pre-pass: OPTIONAL-group membership scan (unchanged).
    members := map[int][]string{}
    groupOf := map[string]int{}
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
    // ... ay9's fixed-point loop unchanged from here — the current
    // demotedGroups map, demoteGroup closure, and the for-changed loop
    // are byte-identical to the post-ay9 landed body. The pre-pass
    // above only writes to `table`; nothing in the loop's control
    // flow observes the pre-pass except through table[v] reads.
}
```

Notes pinned for the implementer:

- `seedLocalNullability` (`resolve.go:1432-1440`) and
  `qualifiedDemoter` (`resolve.go:1527-1537`) are unchanged; the
  carry-seed → local-override → demote order at the Phase D call
  site (`:357-366`) is unchanged.
- The 5xg pre-pass demotes *only via the table*; it does not seed
  `demotedGroups`. This preserves the ay9-only guarantee that a
  group's demotion requires a proven WITNESS EDGE (not just a
  proven table entry) — the group closure step at ay9's `demoteGroup`
  call site (post-widening, inside the loop) reads `groupOf[v]`
  when an edge witness fires. A bare-ref-demoted binding therefore
  demotes its own table entry but does not by itself demote the
  whole group (matching R4's semantic — the bare re-reference
  witnesses the binding's existence, not that its edge exists).
- **Conservative composition, pinned.** For
  `OPTIONAL MATCH (a)-[r1:R]->(b) MATCH (b) RETURN a, r1, b`, 5xg
  demotes only `b` (bare-ref). ay9's group closure demotes group 1
  iff an OPTIONAL edge in the group is proven through the
  edge-witnessing pass; `b`'s bare-ref demotion does not itself
  flag the group. Result: `a`, `r1` stay nullable in this shape.
  Closing this residual needs "any group member with `table[v] ==
  false` demotes the group" — an ay9-refinement, out of scope for
  5xg (§8.4 fixture 4 pins the boundary).
- Group-0 bindings fall through both pre-passes untouched — the
  ay9 pre-pass filters `g > 0`; the 5xg pre-pass writes only when
  the flag is set (never at first introduction on group-0).

### 8.2 Sentinel discipline — zero new, verified

Expected posture: **no new sentinel, no new failure mode.** The
entire R4+ay9 demotion path is error-free at branch base —
`seedLocalNullability` and `demoteNullableInPlace` return nothing
(`resolve.go:1432`, `:1450`), and Phase D (`:357-366`) has no error
branch. The 5xg pre-pass adds a linear scan writing `false` to a
map. `allSentinels` (`internal/resolver/errors.go:103-119`) stays at
its current **11 entries**. This is the same posture ay9 held (§8.2,
verbatim precedent); the campaign's later rejection class
count is unchanged.

### 8.3 Existing resolver goldens — exactly 0 flip, derived

All 126 valid fixtures were swept; 18 contain OPTIONAL (grep-verified
via `grep -il OPTIONAL test/data/resolver/valid/*.cypher`). Per-
fixture derivation of the widened rule over the 18:

**Flips (0):** No existing fixture exercises the same-Part
required-bare-re-reference-of-an-already-introduced-variable shape.
Verification per fixture:

- `carry_nullable_binding` (`OPTIONAL MATCH (a:Person) WITH a RETURN
  a`) — single OPTIONAL, no re-reference. `a` in `RETURN` is a
  Ref, not a MATCH occurrence.
- `demote_chained_from_required` — the ay9 fixture. Second MATCH is
  `(post)-[r2:AUTHORED]->(author)` — `post` at edge-chain head, not
  bare.
- `demote_cross_with_remerge` — WITH-based cross-Part; Part 1's
  `MATCH (b)-[:AUTHORED]->(c:Person)` is edge-chain, not bare. R5
  already handles.
- `demote_from_anonymous_required_edge` — second MATCH is
  `(a)-[:AUTHORED]->(c:Post)` — `a` at edge-chain head, not bare.
- `demote_group_arity_five` / `demote_two_groups_one_proven` /
  `no_demote_unproven_group` / `demote_group_cascade` — all ay9
  fixtures; every re-reference is in an edge-chain position.
- `demote_var_length_unbounded_lower` / `no_demote_var_length_zero_min`
  — `OPTIONAL MATCH (p:Person) MATCH (p)-[r:KNOWS*]->(q:Person)` —
  `p` at edge-chain head (var-length edge follows), not bare. The
  latter fixture's `p` stays nullable because the var-length hop
  gate's zero-min disqualifies the edge witness; 5xg's rule is
  unrelated (not bare).
- `optional_edge_property` / `optional_edge_whole_entity` /
  `optional_multi_type_union` / `optional_node_nullable_property` /
  `optional_node_property` / `optional_node_whole_entity` /
  `optional_var_length_whole_entity` / `set_property_on_nullable`
  — single-OPTIONAL-clause shapes with no re-reference at all.

Every OPTIONAL-carrying fixture is byte-identical under 5xg. The
remaining 108 valid fixtures contain no OPTIONAL and cannot move.
All 50 invalid fixtures keep their sentinel — demotion runs on
valid paths only.

Fence: `go test ./internal/resolver/ -update` then
`git status --porcelain test/data/resolver/` shows exactly the 4
new fixture pairs + `schema.mapping.json` — no existing golden
changes.

### 8.4 New fixtures — names, queries, expected columns

All four map to the existing `social_r4.gql`
(`test/data/resolver/valid/schemas/`, edges: Person-AUTHORED→Post,
Person-LIKES→Post, Post-AUTHORED→Person, Person-KNOWS→Person); four
entries join `schema.mapping.json`.

1. **`demote_bare_reference_from_optional.cypher`** — the canonical
   Class B case (R4 §7.5.1 canonical example):

   ```
   OPTIONAL MATCH (a:Person)-[:AUTHORED]->(b:Post) MATCH (b) RETURN b
   ```

   Parser output: `a` gets group 1, flag false; anonymous edge gets
   group 1, flag false; `b` gets group 1 AND
   `referencedInRequiredBarePattern = true` (bare re-reference in
   required clause). Resolver: 5xg pre-pass demotes `b` (table
   flips true → false). ay9 loop: no OPTIONAL edge is proven
   through R4's per-edge rule (`b` has no adjacent required edge in
   this query), so `demotedGroups` stays empty; `a` and `r1`
   (anonymous) stay nullable. Expected columns: only `b` in RETURN,
   `"nullable": false`.

2. **`demote_bare_reference_with_label.cypher`** — variant confirming
   the label filter qualifies as bare (§2.3):

   ```
   OPTIONAL MATCH (a:Person)-[:AUTHORED]->(b:Post) MATCH (b:Post) RETURN b
   ```

   Parser output: same as fixture 1, plus the second occurrence's
   label `Post` merges into `b`'s `LabelSet`; `b`'s flag is set
   (bare, has label filter, required). Resolver: `b` demotes to
   non-nullable. Expected columns: `b` with `"labels": "Post"`
   (unchanged from fixture 1 — labels merged idempotently) and
   `"nullable": false`. This fixture fails on any implementation
   that mis-classifies `MATCH (v:Label)` as non-bare.

3. **`no_demote_optional_bare_reference.cypher`** — kill-probe:
   OPTIONAL bare re-reference must NOT set the flag:

   ```
   OPTIONAL MATCH (a:Person)-[:AUTHORED]->(b:Post) OPTIONAL MATCH (b) RETURN b
   ```

   Parser output: `a` gets group 1, anonymous edge group 1, `b` gets
   group 1 (first introduction). Second OPTIONAL MATCH mints group
   2; its bare `MATCH (b)` re-references `b` with `group == 2`
   (OPTIONAL) — merge arm at `mergeBinding` fires but the decision
   table's `group == 0 && bare` condition is FALSE (group is 2),
   so the flag is NOT set. Resolver: no bare-ref demotion; ay9 loop
   has no witness (group 2's bindings have no required-chain
   witness). Expected: `b` stays `"nullable": true`. This fixture
   fails on any implementation that fires 5xg's flag on OPTIONAL
   re-references.

4. **`demote_bare_reference_composes_with_group.cypher`** — the
   interaction fixture pinning §8.1's orthogonality:

   ```
   OPTIONAL MATCH (a:Person)-[r1:AUTHORED]->(b:Post) MATCH (b) RETURN a, r1, b
   ```

   5xg pre-pass demotes `b`. ay9 loop finds no proven OPTIONAL
   edge in group 1 (bare-ref does not seed `demotedGroups`), so
   `a` and `r1` stay nullable. Expected: `a`, `r1` `"nullable":
   true`; `b` `"nullable": false`. A future ay9-refinement lifting
   the conservative boundary (§8.1's "table[v] == false ⇒
   demoteGroup") would flip this golden — signal, not bug.

Not added: an edge-side fifth fixture
(`OPTIONAL MATCH (a)-[r:R]->(b) MATCH (a)-[r]->(x)`). `r` re-appears
in an edge pattern position, not bare (§2.3); `collectEdge` calls
`mergeBinding` with `bare=false`, so the flag stays false. This is
the edge-side residual (§9), out of 5xg's scope.

### 8.5 Byte-identity fence over the untouched corpus

```sh
# From the widening-PR branch tip.
go test ./internal/resolver/ -update
git status --porcelain test/data/resolver/ | grep -v \
  -e demote_bare_reference_from_optional \
  -e demote_bare_reference_with_label \
  -e no_demote_optional_bare_reference \
  -e demote_bare_reference_composes_with_group \
  -e schema.mapping.json
# MUST print: empty.
go test ./internal/query/... && git status --porcelain internal/query/
# MUST print: ok + empty (the widening PR touches no parser surface).
```

### 8.6 Stage-spec successor prose — the docs-errata targets

Rows to retire (Class B / gqlc-5xg only; every other row already
retired or stayed verbatim), located at branch base `d8d2818`:

| File | Site | Current text (abridged) |
|---|---|---|
| `resolver-stage-r4.md:1191` | §7 table | "Nullability upgrades (regime (b), same-Part re-MATCH — Class B: missing-witness model gap) … §7.5.5 bead 2 (Axis 2 change)" |
| `resolver-stage-r4.md:1273` | §7.4 wording-revision note | "same-Part regime (b) is retitled and gated on §7.5.5 bead 2" — stale gate reference post-5xg (bead is closed, not gated on) |
| `resolver-stage-r4.md:1042-1044` | §6.3 fixture note | "Same-part reuse of `a` as a bare pattern doesn't help — that is Class B … the parser discards the second occurrence at `pattern.go:373-401`" |
| `resolver-stage-r4.md:1239-1256` | §7.4 item 2 | "Same-Part regime (b) — a distinct model gap (missing witness, not missing grouping)" |
| `resolver-stage-r4.md:1258-1267` | §7.4 conclusion | "cross-WITH regime (b), same-Part regime (b), and Class-A OPTIONAL-sibling under-demotion are three distinct problems …" |
| `resolver-stage-r4.md:1367-1397` | §7.5.3 Class B block | full-block prose describing Class B and the missing witness |
| `resolver-stage-r4.md:1443-1470` | §7.5.4 Axis 2 | "Axis 2 — same-Part second-reference preservation (closes Class B, item 2)" — full paragraph |
| `resolver-stage-r4.md:1489-1516` | §7.5.5 recommendation | Class B references in the "both classes are real" defense |
| `resolver-stage-r4.md:1531-1535` | §7.5.5 bead 2 | "Model: preserve same-Part second-reference facts on `Binding` (either shape (a) or shape (b) per §7.5.4 Axis 2)" |
| `resolver-stage-r4.md:1549-1552` | §7.5.5 fixture note | "Class B has no dedicated fixture at R4 (there is no fixture in §6.3 that exercises same-Part bare-pattern re-MATCH …)" — discharge |
| `resolver-stage-r4.md:1687-1695` | §9 close-out block | "bead 2 for Class B … Class B same-Part missing-witness gap → §7.5.5 bead 2 (Axis 2 change)" |
| `resolver-stage-r5.md:37-46` | §1 banner | "same-Part regime (b) nullability under-approximation surviving from R4 (§7.5 Class B, gap tracked on gqlc-5xg) is unchanged at R5" |
| `resolver-stage-r5.md:613` | §4.1.1 table | "Same-Part regime (b) nullability under-demote … gqlc-5xg (Class B, model change)" |
| `resolver-stage-r5.md:617` | §4.1.1 R4 §7.4 explanation | "same-Part regime (b) and Class A both remain safe under-approximations gated on the two model-additions beads" — factually wrong post-5xg (both closed) |
| `resolver-stage-r5.md:1713-1721` | §4.6.1 | "Same-Part regime (b) still under-approximates at R5. R4 §7.5 Class B — same-Part bare-pattern re-MATCH of an OPTIONAL binding — is unchanged (the parser's merge rule …)" |
| `resolver-stage-r5.md:2321` | §7 table | "Nullability upgrades (regime (b), same-Part re-MATCH — Class B: missing-witness model gap) … gqlc-5xg (model change)" |
| `resolver-stage-r5.md:2401,2418,2501,2618,2705,2718` | close-out prose | "gqlc-ay9 and gqlc-5xg remain OPEN; R5 does not close them" and siblings |
| `resolver-stage-r6.md:29-30` | §1 banner | "Class B same-Part regime (b) nullability under-approximation (`gqlc-5xg`)" |
| `resolver-stage-r6.md:1883` | §7 table | "Same-Part regime (b) nullability under-demote … gqlc-5xg (Class B, model change — unchanged from R5)" |
| `resolver-stage-r6.md:2014,2270` | close-out prose | "gqlc-5xg … remain OPEN unchanged" siblings |
| `resolver-stage-r7.md:56-57` | §1 banner | "Class B same-Part regime (b) nullability under-approximations (`gqlc-ay9`, `gqlc-5xg`)" |
| `resolver-stage-r7.md:2189` | §7 table | "Same-Part regime (b) nullability under-demote … gqlc-5xg (unchanged)" |
| `resolver-stage-r7.md:2336-2340` | §7.1.2 prose | "Class B (`gqlc-5xg`) persists at R7 unchanged" |
| `resolver-stage-r7.md:2362,2527` | close-out prose | "gqlc-5xg, … remain OPEN" siblings |

Successor wording per row: "closed by the 5xg change + widening
(`docs/specs/model-change-5xg-required-bare-ref.md`); edge-side non-bare
missing-witness residual filed as <follow-up bead id>". The R4
§7.5.5 bead-2 paragraph and the §7.5.4 Axis 2 block gain dated
closure notes rather than deletion (stage-spec history stays
readable, the ay9 §8.6 precedent). Every "same-Part regime (b)"
mention gains a strikethrough with a closure annotation pointing to
this spec, matching the ay9 close-out PR's format on Class A rows.

**Rows kept verbatim (definitional; grep-matches but not stale
post-5xg).** These four lines match the DoD grep pattern but
describe historically-true class definitions and inter-cycle
independence claims that remain accurate after 5xg closes. They
receive no strikethrough:

| File | Site | Why kept verbatim |
|---|---|---|
| `resolver-stage-r4.md:561` | §7.5 walk-order preamble | Class B name-and-definition ("missing witness") — historically-true class label, definitional |
| `resolver-stage-r4.md:1370` | §7.5.3 two-classes intro | "Class A is the OPTIONAL-clause-sibling gap; Class B is the same-Part second-reference gap" — definitional class taxonomy, permanent (line shifted from :1341 by errata insertions) |
| `resolver-stage-r4.md:1486` | §7.5.4 Axis 1 non-closure | "does **not** close item 2, because Class B's problem is a missing witness, not a missing group" — historically-true statement about the ay9 axis's scope, remains accurate post-5xg (line shifted from :1440 by errata insertions) |
| `resolver-stage-r5.md:1450` | §4.5.3.3 hk0 residual note | "Independent of gqlc-ay9 / gqlc-5xg" — passing scope reference in an unrelated (hk0/Shape B) residual, historically-true independence claim (line shifted from :1439 by errata insertions) |

Enumeration is exhaustive across both tables. The definition-of-
done grep in §11 must whitelist the four definitional lines above:

```sh
grep -nE "gqlc-5xg|Class B|same-Part regime \(b\)|Same-Part regime \(b\)" \
  docs/specs/resolver-stage-r[4-7].md \
  | grep -v \
    -e "resolver-stage-r4.md:561:" \
    -e "resolver-stage-r4.md:1370:" \
    -e "resolver-stage-r4.md:1486:" \
    -e "resolver-stage-r5.md:1450:"
# MUST print no matches outside strikethrough-annotated regions
# after the docs-errata PR merges.
```

§11 pins this as a definition-of-done fence.

---

## 9. Non-goals

| Out of scope | Where it lives |
|---|---|
| Edge-side non-bare missing-witness gap (Class B for edges) | follow-up bead filed at close-out (§7 residual, §2.5) — needs shape (a)'s `RequiredReferences` axis |
| Bare-ref demotion feeding ay9 group closure | intentional §8.1 conservative boundary; fixture 4 pins the residual |
| Cross-Part carry of bare-ref flag | resolver-internal `branchState` (name → bool) already carries the demoted-to-false table entry across WITH; no model change needed |
| `PathBinding` bare-ref axis | Stage-8 posture (`Nullable()` hardcoded false; a named path's bindings carry the flag through their referenced node/edge bindings) |
| `UnwindBinding` / `CallBinding` bare-ref axis | no re-reference form; UNWIND has no bare-re-reference grammar shape (the AS variable enters scope but its re-reference in a MATCH is a fresh required binding, kind conflict); CALL YIELD variables enter scope but their re-reference in a bare MATCH is a kind conflict caught in buildPart |
| EXISTS-suppressed bare re-references | Stage-11 posture unchanged; `mergeBinding` runs outside `subqueryDepth > 0`, so a bare re-reference inside `EXISTS { ... }` never reaches the flag-setting site |
| Codegen consumption of widened nullability | future ADR |
| apidiff CI gate | pre-existing ADR 0008 bead, unchanged by this cycle |
| Other change beads' surfaces (`hk0`/`fvo`/`0ig`/`ay9` axes) | closed; untouched |

---

## 10. Ground-truth cross-check

Every claim above rests on the citations below, all re-derived at
branch base `d8d2818` (worktree `model-change-5xg-spec`); counts were
produced by the scripts described in §4.4 and §8.3, not copied from
prior documents.

- `Binding` interface — `internal/query/query.go:297-306`.
- `NodeBinding` struct / ctors / accessor / marshal (post-ay9) —
  `query.go:313-318` (struct) / `:322-356` (ctors) / `:375`
  (Nullable) / `:383` (OptionalGroup) / `:1686-1694` (marshal).
- `EdgeBinding` struct / ctors / accessor / marshal (post-ay9) —
  `query.go:399-408` (struct) / `:416-493` (ctors) / `:526`
  (Nullable) / `:532` (OptionalGroup) / `:1703-1715` (marshal).
- `PathBinding.Nullable()` hardcoded false — `query.go:766-768`.
- ay9 InGroup ctors — `query.go:346-356`, `:441-451`, `:483-493`
  (three new post-ay9 InGroup constructors, preserved for 5xg).
- Grammar: OPTIONAL only on `oC_Match` —
  `internal/grammar/cypher/Cypher.g4:81`.
- Listener struct / `rawBinding` (post-ay9) / `EnterOC_Match` /
  CREATE false / MERGE false —
  `internal/query/cypher/listener.go:31-100` / `:157-176` /
  `:271-284` / `:372` / `:397`.
- Threading chain + head-bare derivation + `mergeBinding` (post-ay9)
  — `internal/query/cypher/pattern.go:38-48` / `:54-95` / `:143-172`
  (chain traversal) / `:188-201` (collectNode) / `:253-310`
  (collectEdge, literal at `:300-303`) / `:388-404` (mergeBinding).
- `toBinding` (post-ay9) —
  `internal/query/cypher/build.go:264-285`.
- Phase D order / seed / demote (post-ay9) / hop gate —
  `internal/resolver/resolve.go:357-366` / `:1432-1440` /
  `:1442-1512` / `:1527-1537`.
- `allSentinels` (11 entries) —
  `internal/resolver/errors.go:103-119`.
- ay9 spec, referenced heavily —
  `docs/specs/model-change-ay9-optional-group.md`, especially §2.3
  variant-set justification, §3.4 zero-value analysis, §8.1
  widening prose (this spec inherits ay9's fixed-point loop and
  adds an orthogonal pre-pass).
- Class B definition and Axis 2 shape (a)/(b) discussion —
  `docs/specs/resolver-stage-r4.md §7.5.3` (`:1367-1397`),
  §7.5.4 (`:1443-1470`), §7.5.5 bead 2 (`:1531-1535`), §7.4 item 2
  (`:1239-1256`).
- Resolver fixture inventory — 126 valid + 50 invalid — `ls
  test/data/resolver/valid/*.cypher | wc -l`, same for invalid.
- OPTIONAL-carrying resolver fixture list (18) — `grep -il
  optional test/data/resolver/valid/*.cypher`, enumeration in §8.3.
- TCK feature-file inventory (30 with OPTIONAL) — `grep -rl "OPTIONAL
  MATCH" test/data/query/cypher/tck/features/`.
- Parser goldens (3199 at branch base `d8d2818`) —
  `ls internal/query/cypher/testdata/golden | wc -l`.
- ADR 0008 additions convention + Known-deferred-additions list —
  `docs/adr/0008-query-model-surface-resolver-api.md:300-361`.
- Campaign precedents — `docs/specs/model-change-ay9-optional-group.md`
  (template, ~1274 lines), `docs/specs/model-change-0ig-call-args.md`
  (constructor-strategy precedent for post-construction axis),
  `docs/specs/model-change-fvo-use-part.md` (omit-when-zero convention),
  `docs/specs/model-change-hk0-containsaggregate.md` (omit-when-false
  bool wire — direct precedent).

Spec-vs-repo divergences found while mining (carried into the
docs-errata cycle where they touch stage-spec text):

1. R4 §7.5.2 and §7.5.4 cite `mergeBinding` at
   `internal/query/cypher/pattern.go:373-401`; the landed function
   spans `:388-404` (ay9 amended the doc comment which now starts at
   `:376`). No semantic drift; R4's referenced behaviour is
   preserved.
2. R4 §7.5.4 illustrative shape (b) says "`mergeBinding` sets a
   boolean when the second occurrence is a required non-anonymous
   bare pattern". "Non-anonymous" is implicit — an anonymous
   occurrence has no `variable`, so `mergeBinding` never fires for it
   (`collectNode` early-returns at `:197`; `collectEdge`'s anonymous
   branch bypasses `mergeBinding` at `:300-303`). This spec makes
   the anonymity gate explicit at §2.3 rather than restate R4's
   language.
3. R4 §7.5.4 Axis 2 shape (b) shows the resolver rule as "a binding
   whose `ReferencedInRequiredBarePattern` is true is demoted." This
   spec's §8.1 implements the rule as a pre-pass distinct from the
   ay9 fixed-point loop, and pins §8.1's conservative composition
   (bare-ref demotion does not seed `demotedGroups`) as an
   intentional boundary; R4's terse rule statement is honestly
   silent on the ay9 interaction (which did not exist when R4 was
   written).
4. R4 §7.5.4 Axis 2 says "each variant gains a
   `ReferencedInRequiredBarePattern bool` accessor"; only two of the
   five variants can be re-referenced in a same-Part bare pattern
   (§2.5) — this spec pins Node + Edge only. (Even the Edge case is
   grammatically unreachable via parse; the field is added for wire
   symmetry per §2.3.)

---

## 11. Definition of done for the spec cycle

The spec PR is done when:

- This file lands on master under
  `docs/specs/model-change-5xg-required-bare-ref.md`.
- No behavioural code changes (spec-only cycle).
- Review explicitly ACKs the five pinned decisions:
  - **Axis shape**: `bool` (shape (b)), NOT
    `[]ClauseRef` (shape (a)), §2.2 — the sole consumer is the
    demotion rule and it needs only a witness bit.
  - **Variant set**: NodeBinding + EdgeBinding only, §2.5 — the
    edge-side true value is grammatically unreachable but added
    for wire symmetry and forward compatibility.
  - **Constructor strategy**: unexported per-variant mutator called
    from `toBinding`, no new public constructor, §3.2 — post-
    introduction axis; ctor addition would carry an unusable
    parameter.
  - **Bare-predicate site**: `collectPatternElement` computes
    `headBare` from chain length; the head-with-empty-chain rule
    is the whole "bare" derivation, §3.3.
  - **Compose-with-ay9**: 5xg pre-pass is orthogonal to ay9's
    `demotedGroups`; a bare-ref-demoted binding does not
    automatically demote its OPTIONAL group; §8.1 conservative
    boundary; §8.4 fixture 4 pins the residual.

The model-change PR (Cycle 2) then implements §4-§7 verbatim; the
resolver-widening PR (Cycle 3) implements §8; the docs-errata PR
(Cycle 4) lands §8.6's successor prose. Docs-errata DoD fence
(§8.6):

```sh
grep -nE "gqlc-5xg|Class B|same-Part regime \(b\)|Same-Part regime \(b\)" \
  docs/specs/resolver-stage-r[4-7].md \
  | grep -v \
    -e "resolver-stage-r4.md:561:" \
    -e "resolver-stage-r4.md:1370:" \
    -e "resolver-stage-r4.md:1486:" \
    -e "resolver-stage-r5.md:1450:"
# MUST print no matches outside strikethrough-annotated regions after
# the docs-errata PR merges. The four whitelist lines are §8.6's
# definitional-verbatim exceptions (Class B name-and-definition,
# two-classes intro, Axis-1-non-closure statement, hk0/Shape B
# independence claim).
```

With this cycle's four PRs merged, the model-model-additions campaign
(hk0 / fvo / 0ig / ay9 / 5xg) has discharged every planned bead. The
edge-side non-bare missing-witness residual (§9) is the sole
outstanding known gap in the R4 §7.5.3 taxonomy, filed as a
follow-up bead at close-out; the R4-inherited "Class B" rows across
the R4-R7 stage specs are retired.
