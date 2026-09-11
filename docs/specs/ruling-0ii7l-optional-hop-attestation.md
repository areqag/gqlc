# Ruling — the introducing OPTIONAL clause, and the condition that replaces `n.Nullable()`

The design answer for **gqlc-0ii7l** ("can the resolver carry the introducing
OPTIONAL part per edge, and what sound condition replaces `n.Nullable()` for
attestation?"), and the implementation brief for its execution bead
**gqlc-1qijx** ("an OPTIONAL hop could attest for the binding it introduces").

Every file:line below was read at this branch's base, master `6187c2f3`.

**The short answer, and why this document is not an ADR.** The resolver already
carries the introducing OPTIONAL clause, per binding, on both ends — it has
since the ay9 model change shipped `OptionalGroup()`. So gqlc-0ii7l's first
option is the live one: the condition is a query over state the model already
holds, not a change to what the binding record stores, and not a decline. What
the ruling costs is one parameter threaded down one call path. Nothing durable
moves: no model axis, no wire key, no exported signature, no generated-code
contract. An ADR here would document an internal predicate, and every ADR in
`docs/adr` is written after implementation against a corpus measurement this
ruling does not have. §7 says which ADR the execution PR may owe, and on what
evidence.

---

## 1. What the resolver already carries

`NodeBinding.OptionalGroup()` and `EdgeBinding.OptionalGroup()`
(`internal/query/query.go:408`, `:598`) each return the id of the `OPTIONAL
MATCH` clause that **first introduced** that binding. Ids are minted once per
`OPTIONAL MATCH` at `listener.go:441-446` through `mintOptionalGroup`
(`listener.go:358`), and the allocation scope is **per-query, never reset**
(spec `model-change-ay9-optional-group.md` §3.3). So two bindings carrying the
same non-zero id were introduced by the same clause, and two bindings carrying
different non-zero ids were not. That is precisely the "introducing part per
edge" the bead asks whether the resolver can carry.

The bead's sentence "which the resolver does not carry at that point today" is
right about the **point** and wrong about the **resolver**. `candidateTypes`
(`resolve.go:1686`) is handed `written` and `demoted` but reads
`e.OptionalGroup()` already, at `resolve.go:1712`, for the `innerJoined` lane.
Both bindings are in scope at the gate on line 1708. Nothing has to be mined,
stored, or plumbed to ask whether they agree.

## 2. The semantic premise, and where it is already load-bearing

The widening rests on one claim about `OPTIONAL MATCH`:

> Every binding a single `OPTIONAL MATCH` clause introduces is null on exactly
> the same rows. The clause matches its whole pattern or none of it.

This is not a new assumption this ruling introduces. It is the premise
`DemoteNullability`'s group closure already runs on, in the opposite direction:
`demoteGroup` (`scope.go:724`) demotes **every** member of a group the moment
**one** member is proven non-null, and ay9 §2.4 clause (iii) states the rule.
If the premise is false, the shipped demotion is unsound today, independently of
anything here. The two uses are the two directions of one biconditional, and
they stand or fall together — which is the strongest thing that can honestly be
said for a premise neither of them tests directly.

What this ruling does **not** claim: that the premise has been verified against
a server. Neither has ay9's. If someone wants it witnessed, the live-arm corpus
is where that belongs, and it would be one bead covering both consumers.

## 3. The condition

Per pending unlabelled node binding `n` and touching edge `e`:

```
introducedByThisHop(n, e) :=
       n.OptionalGroup() >= 1                       // (a) floor
    && n.OptionalGroup() == e.OptionalGroup()       // (b) same introducing clause
    && singleHopPattern(e)                          // (c) carried over
    && !written[e.Variable()]                       // (d) carried over
    && n.Variable() is not carried into this Part   // (e) not a re-declaration
```

and the gate at `resolve.go:1708` becomes

```go
if otherCovers && (witnessesItsEndpoints(e, written, demoted) || introducedByThisHop(n, e)) {
```

Three shape decisions, each with its reason:

- **A disjunct beside the existing conjunction, not a relaxation of it.** Both
  conjuncts of the current gate are separately pinned — the bead records the
  three measurements from PR #2237's branch, and none of them is repeated here.
  The widening adds a second route to `attested` and removes none.
- **`otherCovers` stays outside the disjunction.** It answers which types the
  far end can enumerate, which is orthogonal to which rows the edge is evidence
  about. A same-group hop whose far end is itself an uncovered Phase-B inference
  still attests nothing.
- **Do not widen `witnessesItsEndpoints` itself.** Its second caller,
  `endpointNarrowing` (`resolve.go:1158`), asks about an edge with **no binding
  in hand** — it is deciding what the edge licenses about both of its own ends,
  not about one nominated binding. Making the function binding-relative would
  change that caller's question silently. `introducedByThisHop` is a sibling
  predicate that shares `singleHopPattern` and the `written` lookup with it.

### 3.1 Why each conjunct, and what would be wrong without it

**(b), and why `n.Nullable()` is not the condition.** This is the bead's own
argument and the ruling agrees with it. `n.Nullable()` says the binding is
nullable, not which clause made it so, so `n.Nullable() && e.Nullable()` credits
an edge from OPTIONAL part B for a binding introduced by OPTIONAL part A — and
on the rows where B missed, `n` is non-null and B's far end enumerates nothing
about it. Clause (b) refuses B and keeps A. Note what that means for the bead's
two-part shape: the widening still **fires** there, through A's edge. It is B's
contribution that is withheld, so the committed set is A's contribution alone
rather than A ∩ B. The observable is the width of the committed set, not the
coverage bit; §5 gives the fixture.

**(a), the floor, and the biconditional that does not hold.** `query.go:404`
documents `Nullable() == true ⇔ OptionalGroup() >= 1`, correctly qualified "for
every parser-produced binding". The six preserved legacy constructors falsify it
for hand-constructed ones: `NewNullableNodeBinding` and `NewNullableEdgeBinding`
return `Nullable() == true` with `OptionalGroup() == 0`, pinned by
`TestOptionalGroupZeroOnLegacyConstructors` (`query_test.go:775`). Without the
floor, clause (b) reads `0 == 0` for two such bindings and credits an edge that
`witnessesItsEndpoints` refuses.

The floor is therefore unreachable from any parser output, and that is the
honest description of it rather than a reason to drop it. Two parser-produced
bindings at group 0 are both **required**, and a required edge already passes
`witnessesItsEndpoints`, so the disjunct never decides anything for them. §6
says what its mutation row has to be, given that no corpus fixture can supply
one.

**(c) and (d), carried over because the disjunct bypasses the function that
holds them.** `singleHopPattern` (`resolve.go:2042`) is inside
`witnessesItsEndpoints` today, so a disjunct that skipped it would let
`OPTIONAL MATCH (p)-[a:R*2]->(n)` attest — and a two-hop closure names the ends
of one of its edges rather than the ends of the pattern, which is the wrong
answer and not a coarser one (`resolve.go:2063-2070`). Being introduced by the
clause does not repair that: `n` is introduced by the hop and the hop still does
not say what `n` is. What a multi-hop's far end licenses is gqlc-3uof's open
question and this ruling does not answer it. `written` is carried over for the
same mechanical reason, though no `OPTIONAL MATCH` can CREATE or MERGE, so the
conjunct is expected to be inert.

**(e), and the shape that makes it necessary.** `mergeBinding` dedups **per
part** — "a name re-MATCHed in a later part is a fresh binding there"
(`pattern.go:404`). So a name carried across `WITH` and re-referenced inside a
**later** part's `OPTIONAL MATCH` becomes a fresh binding carrying that clause's
group id, alongside that clause's edge. Clauses (a)–(d) then all hold, and the
attestation is unsound: the binding is non-null from its Part-K introduction on
rows where the Part-K+1 hop missed.

The shape is in the corpus, not hypothetical. ay9 §3.3 cites
`Match7_91ea67e28e2f` — `OPTIONAL MATCH (a:NotThere) OPTIONAL MATCH (b:NotThere)
WITH a, b OPTIONAL MATCH (b)-[r:NOR_THIS]->(a) RETURN a, b, r` — where Part 1's
`a`, `b` and `r` all carry group 3 and `a` and `b` are carried from groups 1 and
2. That particular query cannot reach the gate, because `a` and `b` are
labelled and Phase B only considers unlabelled bindings; it is cited as the
witness that the re-declaration shape occurs, not as a fixture.

### 3.2 (e) is the one conjunct whose reachability is unsettled

`inferUnlabelled`'s CARRY WINS filter (`resolve.go:1254-1266`) drops from
`pending` every name already in `t.resolved` or `t.cands`, and `newScope`
(`scope.go:135-140`) seeds both lanes from the carry. So a name carried **as a
node** can never reach `candidateTypes` at all, and for those names clause (e)
is a guard nothing can fail.

The residue is a name carried as something that is **not** a node — a
`WITH count(p) AS c` alias lands in `carriedResolvedTypes` only, and a CALL
YIELD scalar lands in `callTypes` — which then appears as an unlabelled node
pattern in a later part's `OPTIONAL MATCH`. Such a query is nonsense and is
expected to be refused, but it is refused *somewhere else*, and this ruling did
not establish where or whether that refusal precedes the gate.

So clause (e) is stated as required and its reachability is left open, on
purpose. ADR 0038's precedent is the one to follow: a branch that cannot be made
to fail is not a guard, and the remedy is to demote it to a derivation with the
implying mechanism pinned by a test — never to leave an unreachable branch
standing unexamined. §6 makes the measurement an execution obligation with a
declared outcome either way.

## 4. What the condition still cannot attest

The bead asks for this list explicitly, and it is the half of the ruling that
keeps the other half honest.

- **A binding that is a real node on every returned row, reached only by outer
  joins.** `MATCH (c) OPTIONAL MATCH (c)-[a:AUTHORED]->(x:Post) RETURN c.name`
  — gqlc-6aed's reproducer, quoted at `resolve.go:1605`. `c` is at group 0,
  clause (b) fails, and it must keep failing. The widening must not move this
  query.
- **The foreign-group edge, in every instance.** Clause (b) is static. It has no
  access to whether part B's clause happens to match on every row of a
  particular database, and it never credits B's edge even when it would have
  been sound for that data. The only route from "B always matches" to
  attestation is `demotedGroups`, which is Phase D's job and is unchanged here.
- **A same-group variable-length hop.** Clause (c), for the reason in §3.1.
- **A same-group hop whose far end does not cover.** `otherCovers` is untouched.
  A far end that is a `WITH` carry, or itself an uncovered Phase-B inference,
  still blocks attestation — and a carried singular node type is deliberately
  left uncovered (`scope.go:128-134`), so this is a real and common block, not
  a corner.
- **Anything about which rows come back.** Attestation licenses a claim of the
  form "on the rows where `n` is non-null, `n`'s type is in this set". It does
  not make `n` non-null. `bindingNullable` (`resolve.go:1538`) is untouched, and
  ADR 0006's conservative nullability posture is unchanged by this ruling — a
  column typed nullable before is typed nullable after.
- **The coverage bit's downstream readers.** This is the limit that matters
  most, and this ruling does **not** clear it. A commitment made covered through
  the widening is covering *conditional on the binding being non-null*. Today's
  covered commitments carry no such condition. Three readers must be checked
  against that difference before the widening ships: `endpointNarrowing`'s two
  `covering()` gates (`resolve.go:1177-1181`), `NarrowPluralEndpoints`, and ADR
  0038's wrong-orientation clause 3, which reads `endpointKeys.covers` on each
  endpoint and treats an uncovered end as a reason to stay silent.

  The argument that it is probably benign, stated so it can be attacked rather
  than trusted: for the widened binding `n` to mislead one of those readers, a
  reader must learn from an edge `f` touching `n` on a row where `n` is null. If
  `f` is required, `DemoteNullability` demotes `n`'s group and
  `witnessesItsEndpoints` already answered true without the widening; if `f` is
  optional and undemoted, `endpointNarrowing` skips it at line 1158. That
  argument covers the narrowing lane. It does **not** cover ADR 0038's detector,
  which is per-query and reads coverage after the close. §6 requires the
  measurement rather than the argument.

## 5. Fixtures the execution owes — one per direction

**Gain.** `MATCH (p:Person) OPTIONAL MATCH (p)-[a:AUTHORED]->(c) RETURN c.title`
— the shape `resolve.go:1534` already records as measured on master (`STRING`
nullable, committed uncovered). `c` is introduced at group 1 by `a`, which is at
group 1, so the substitution fires and the commitment becomes covered. This is
the query the widening exists for; if it does not move, nothing was widened.

**Foreign-group withholding.** The direct extension of the existing
`valid/unlabelled_optional_hop_shared_property.cypher`, with the first hop moved
into an OPTIONAL clause so the binding is OPTIONAL-introduced:

```
MATCH (p:Person)
OPTIONAL MATCH (p)-[q:WORKS_AT]->(c)
OPTIONAL MATCH (c)-[h:HAS_DESK]->(d:Desk)
RETURN c.name
```

`c` is at group 1 with `q`; `h` is at group 2. `inferred` folds both edges,
`attainable` folds `q` alone, and the committed set must be `q`'s contribution —
**not** `q ∩ h`. The observable is the width of the committed set in the golden,
so the fixture is only meaningful if the schema makes those two sets differ; the
implementer must check that against the fixture schema and pick a relationship
pair that does, rather than committing a fixture where both readings agree.

**Must not move.** `MATCH (c) OPTIONAL MATCH (c)-[a:AUTHORED]->(x:Post) RETURN
c.name` keeps refusing, and every fixture in the existing
`invalid/unlabelled_optional_*` family keeps its current sentinel —
`unlabelled_optional_hop_empty_intersection.cypher` in particular, whose
`ErrorIs(ErrUnknownLabel)` the substitution is documented to break
(`resolve.go:1562`) and which the widening must not route around.

The bead asks for "the two-OPTIONAL-part twin that must still commit
uncovered". That phrasing predates this condition and should be read as
superseded: under clause (b) the two-part shape commits **covered**, through
part A's own edge, and what must be withheld is part B's *contribution*. A
fixture that still commits uncovered needs a binding OPTIONAL-introduced with no
same-group touching edge at all — a bare `(n)` in the introducing clause's comma
pattern. Whether the parser accepts that shape was not established here; if it
does, it is the third fixture, and if it does not, the bead should record that
the direction is unreachable rather than quietly drop it.

## 6. Mutation rows the execution owes

Declare the expected victim before each run, per decision 0005. One row per
conjunct, because a five-conjunct predicate shipped on one aggregate kill count
is four untested conjuncts.

| # | mutation | expected victim |
|---|---|---|
| 1 | `\|\|` → `&&` in the widened gate | the gain fixture reverts to uncovered |
| 2 | drop (b), keep the rest | the foreign-group fixture commits `q ∩ h` |
| 3 | drop (c) `singleHopPattern` | needs a same-group `*2` hop fixture; if none exists, the row is vacuous and the conjunct is untested — say so |
| 4 | drop (a) the floor | **no corpus fixture can supply this row.** The row is a unit test constructing `NewNullableNodeBinding` / `NewNullableEdgeBinding` directly and asserting the gate refuses |
| 5 | drop (e) not-carried | governed by §3.2 — if reachable, the row is the carried-redeclaration fixture; if the measurement shows it unreachable, the conjunct is deleted and replaced by a test pinning the CARRY WINS filter at `resolve.go:1254-1266`, which is then the mechanism that implies it |

Row 3 and row 5 are the two that can come back vacuous, and a vacuous row is a
finding to report, not a row to quietly drop. Screen every row with
`go test -c -o /dev/null ./internal/resolver/` before trusting a RED: a mutation
the compiler rejects is not a mutation the tests killed.

## 7. What the execution PR owes beyond the code

- **A corpus delta, in ADR 0038's table form**, before the widening is described
  as precision-only. The claim to measure is how many corpus cells change
  commitment — count separately the cells that go uncovered → covered and the
  cells whose committed SET changes width, because the second class is where a
  singular commitment can become plural and change generated code.
- **The three coverage consumers of §4's last bullet**, checked rather than
  argued.
- **An ADR only if the delta is non-empty.** If corpus cells move, a query the
  toolchain accepted now generates different code, and that is what `docs/adr`
  is for — written then, with the measurement in it, on the next free ordinal
  re-derived at push time. If the delta is empty except for the new fixtures,
  this document plus the bead prose is the whole record and no ADR is owed.

## 8. Rejected alternatives

- **`n.Nullable() && e.Nullable()`.** The bead's own falsifier: the two-part
  shape credits B. Not re-litigated.
- **Store the introducing clause on the binding as new state** (gqlc-0ii7l's
  second option). Unnecessary — `OptionalGroup()` is that state and has been
  since ay9. Adding a second spelling of it would be two mechanisms where the
  model already has one, and would put a second axis on the wire for nothing.
- **Decline gqlc-1qijx** (gqlc-0ii7l's third option). Falsified by §1: the
  condition exists, is expressible over shipped state, and its unsound
  neighbours are separable by conjuncts (a), (b) and (e).
- **Fold the widening into `witnessesItsEndpoints`.** Rejected in §3 —
  `endpointNarrowing` calls it with no binding in hand, and would silently start
  answering a different question.
- **Widen `demotedGroups` instead**, by treating a group as demoted for the
  purpose of its own introduced bindings. Rejected: `demotedGroups` means "this
  clause's rows all survived", which is a claim about the whole result set and
  is read by `DemoteNullability` to flip `Nullable` bits. Reusing it for a
  per-binding, non-null-conditional claim would make ADR 0006's nullability
  answer wrong in order to make Phase B's type answer sharper.
