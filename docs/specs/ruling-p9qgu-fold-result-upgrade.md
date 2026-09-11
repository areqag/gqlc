# Ruling — `min`/`max` upgrade over a certified operand; `sum` declined

The design answer for **gqlc-p9qgu** ("sum/min/max upgrade over certified
operands — can `aggregateResultType` cross the parser/resolver boundary?"), and
the implementation brief for its execution bead **gqlc-b8m8f**.

This is the residue `docs/specs/model-change-f45qn-ref-valued-leaves.md` §9
deferred, filed as a non-goal of that ruling. gqlc-p9qgu's own gate — "do not
begin before gqlc-t0bk's implementation has merged" — is satisfied: the
certificate machinery is in the tree (`query.go:1441`, `expr.go:447-451`,
`scope.go:1019-1096`) and this ruling is written against it rather than against
the spec that proposed it.

Every file:line below was read at this branch's base, master `6187c2f3`.

**The short answer.** `aggregateResultType` should not cross the boundary, and
it does not need to. The bead's framing — "a bare unknown is overloaded" — is
true of the **type** in isolation and false of the **projection**, which also
carries `Func()`. The overloading dissolves at the only place the resolver
actually stands. What is then available is smaller than the bead's title: `min`
and `max` upgrade, `sum` is declined, and the two are declined and admitted for
opposite reasons rather than as a matter of degree.

**Why this is not an ADR.** It rules no model change, no new axis, no exported
signature. f45qn is the precedent for the genre — a written ruling in
`docs/specs`, with the ADR notes owed by the *execution* PR (its §8). §6 says
which ADR gqlc-b8m8f owes and why that one is not optional.

---

## 1. The bare unknown is not overloaded where the resolver reads it

`AggregateProjection` carries `Func() AggregateFunc` (`query.go:1447`), and
`AggregateFunc` lives in `internal/query` — the curated shared model both sides
already depend on. `projectionType` (`scope.go:990`) has the whole projection in
hand at line 1001 and discards the function identity before calling
`certifiedProjectionType`.

That is the entire question. "Unknown because the operand was unknown" and
"unknown because the fold is engine-dependent" are separated by **which
aggregate it is**, not by anything about the type:

- `avg`, `stDev`, `stDevP`, `percentileCont`, `percentileDisc` are
  engine-dependent by function identity, unconditionally. `aggregateResultType`
  (`shape.go:346-355`) returns unknown for them regardless of operand — `avg` is
  the bead's own witness and it is a witness about the *function*, not about the
  operand.
- `sum`, `min`, `max` return unknown at `shape.go:334`/`:344` only when the
  operand type missed their domain arm, and a property lookup is typed
  `TypeUnknown` by the parser (ADR 0003), so it always misses.

So the resolver reaching a bare unknown under `AggMin` knows the unknown means
"the operand's type was not committed at parse time". It does not have to
recover that from the type, and it never had to.

## 2. Why `aggregateResultType` must not cross anyway

Even setting §1 aside, exporting it would not work, for a reason stronger than
the coupling the bead names.

The two sides speak different type languages, and the difference is exactly the
one ADR 0002 exists to protect. `aggregateResultType` is
`query.Type -> query.Type`, and `query.TypeInt{}` is **width-free**. The
resolver holds `ResolvedProperty{Type: graph.PropertyType}`
(`validated.go:181-184`), and `graph.PropertyType` carries the width —
`INT8`, `INT16`, `INT32`, `INT64`, `INT128`, `INT256`, and the unsigned family
(`propertytype.go:331-343`).

To call the exported table the resolver would have to demote
`ResolvedProperty{INT32}` to `query.TypeInt{}`, receive `query.TypeInt{}` back,
and then invent a width for the answer. That is discarding the fidelity ADR 0002
commits to and re-guessing it one line later. The coupling objection the bead
raises is real and secondary; this one is disqualifying on its own.

The corollary is that "carry the verdict in the model" — the bead's other option
— buys nothing either. The verdict `aggregateResultType` computes is already in
the model, as `Type()`; what the resolver needs is a **different** table, over
`graph.PropertyType`, which the parser cannot compute because it has no schema.
Neither option in the bead's framing is taken, and the axis ADR 0003 admitted
for the certificate is not joined by a second one.

## 3. The trio splits: `min`/`max` yes, `sum` no

### 3.1 `min` / `max` — admitted, and they need no fold table

`min` and `max` do not fold. They **select**: the result is one of the operand's
own values, unchanged. So the result is exactly as representable as the property
is, and the machinery that already round-trips `RETURN p.age` through a driver's
64-bit integer into a declared `INT32` — ADR 0002's width preservation with
ADR 0037's out-of-width read refusal — covers the aggregate result with nothing
added. The rule is not "look the fold up"; it is "a selection returns its
operand's type", and that rule needs no table at all.

Two things it is **not**:

- **It is not the operand's nullability.** `min` and `max` over zero rows are
  NULL, so the column is nullable even when the property is declared `NOT NULL`
  and the binding is non-nullable. A rule that reused `refProjectionType`'s
  answer verbatim would emit a non-pointer field that receives NULL — the
  ADR 0041 defect, in the other direction. The committed type is the operand's
  `ResolvedProperty` with `Nullable` forced **true**.

  The sharper rule is available and is declined here: a projection list with a
  grouping key has no empty groups, so `min` over one is null only if every
  value in the group is null. The resolver does compute grouping keys
  (`resolve.go:362-404`), so this is reachable. It is declined because it makes
  the column type depend on a second, unrelated property of the projection list,
  and the gain is one pointer on a shape nobody has asked for. If someone wants
  it, it is its own bead with its own fixture.
- **It is not every operand type.** `min`/`max` order integers, floats, strings,
  booleans and the temporal families (`shape.go:337-345`). `BYTES` is not in
  that set and the resolver-side rule must not admit it, so there **is** a small
  resolver-side table — over `graph.PropertyType`, listing which families a
  selection preserves. It is not `aggregateResultType`, it is not derived from
  it, and it must not be described as a copy of it: the two range over different
  domains and answer different questions.

**Mint condition.** `refValuedShape` (`shape.go:83`) returns a depth alongside
`ok`, and a bare `var`/`var.prop` is depth **0** (`shape.go:88-90`). The mint
site at `expr.go:447-450` widens to:

```go
switch {
case fn == query.AggCollect && len(args) == 1:
    _, leavesAreRefs = refValuedShape(args[0])
case (fn == query.AggMin || fn == query.AggMax) && len(args) == 1:
    d, ok := refValuedShape(args[0])
    leavesAreRefs = ok && d == 0
}
```

Depth 0 exactly, and that is deliberate. `min([p.id, p.age])` orders *lists*,
and whether a list ordering is well-defined enough to type its result is a
question this ruling does not open. `collect` keeps its any-depth mint, for the
reason f45qn gives.

### 3.2 `sum` — declined, and not as a matter of degree

`sum` folds, and the fold does not stay inside the operand's declared width. The
sum of a column of `INT32` values need not fit in `INT32`. So:

- **Committing the operand's width is wrong.** ADR 0037 fails a read whose value
  falls outside the declared width, so `sum(p.count32)` over a wide-enough table
  would fail on data the schema permits — which is precisely the fault ADR 0041
  names: "it failed on a value the schema declared legal, and the type it failed
  through was a type the schema had not asked for."
- **Committing a wider width is a claim about the accumulator**, which is the
  driver's, not the schema's. It is also not always available: `INT256` and
  `UINT64` have nothing above them in `propertytype.go`, so a widening table
  runs out of room rather than degrading, and the arms where it runs out are the
  ones where overflow is least hypothetical.

`any` is therefore the correct **permanent** answer for `sum`, on the same
footing f45qn puts `[]any` on for folds — not a gap awaiting a cleverer
inference. The bead's title should be read as superseded on this third of it.

One arm someone may revisit, named so the decline is not overclaimed:
`sum(DURATION)` is the case where the accumulator question has a different
shape, since `aggregateResultType` already commits `TypeDuration` for it
(`shape.go:332-333`) and the temporal carriers are driver-neutral (ADR 0033).
It is not admitted here because nothing measured it and because one arm is not
worth a second code path; it is a bead, not an oversight.

`avg`, `stDev`, `stDevP` and the percentiles are unchanged and stay
`TypeUnknown`. §1 is what keeps them out: they are excluded by function
identity, before any operand is looked at.

## 4. Where the code goes — and the belt that must not be widened

`fillLeaf`'s `underList` parameter (`scope.go:1084-1096`) is the never-fill-a-
bare-unknown belt, and the `min`/`max` upgrade fills a bare unknown. **Do not
reach it by relaxing the belt.** Passing `underList=true` at the top call, or
deleting the condition, would authorise exactly the fill the belt exists to
refuse — `avg`'s — and would do it silently, since `avg` mints no certificate
today only by the mint site's grace.

The shape instead:

1. `projectionType`'s `AggregateProjection` arm (`scope.go:1000`) stops sharing
   a call with the `ExprProjection` arm and dispatches on `pp.Func()`.
2. `collect` keeps `certifiedProjectionType` verbatim. The list-spine path and
   its belt are untouched.
3. `min`/`max` take a new sibling that resolves the single ref through
   `s.refProjectionType` — **reused verbatim**, which is f45qn §5's consistency
   invariant and applies here for the same reason — checks the result is a
   `ResolvedProperty` in the orderable family, and returns it with `Nullable`
   forced true. Anything else degrades to `base`, which is today's `any`.
4. Resolution **errors propagate**, as they do for `collect`: `min(p.nosuch)`
   refuses `ErrUnknownProperty` where it is silently `any` today. This is the
   same named acceptance change f45qn made and for the same reason, and the
   implementer owes the same sweep of existing fixtures for certified-shape
   queries over undeclared properties.

`fillLeaf` is not called on this path and its signature does not change.

## 5. Tests and mutation rows the execution owes

**Fixtures**, at minimum one per ruling clause:

| shape | expectation |
|---|---|
| `RETURN min(p.age)`, `age :: INT32` | `*int32`, not `any` |
| `RETURN max(p.name)`, `name :: STRING NOT NULL` | `*string` — **nullable despite NOT NULL**; this is §3.1's trap and the fixture that catches it |
| `RETURN sum(p.age)` | stays `any` |
| `RETURN avg(p.age)` | stays `any` |
| `RETURN min(p.blob)`, `blob :: BYTES` | stays `any` — the orderable-family table's negative row |
| `RETURN min([p.id, p.age])` | stays `any` — the depth-0 mint condition's negative row |
| `RETURN min(n)` for a node binding `n` | stays `any` — the non-`ResolvedProperty` degrade |
| `RETURN min(p.nosuch)` | refuses `ErrUnknownProperty`, in the invalid corpus |

**Mutation rows**, victim declared before each run, per decision 0005:

| # | mutation | expected victim |
|---|---|---|
| 1 | mint for `AggSum` as well | the `sum(p.age)` fixture's golden moves off `any` |
| 2 | drop the `d == 0` depth check | the `min([p.id, p.age])` fixture |
| 3 | force `Nullable` from the operand instead of true | the `NOT NULL` max fixture |
| 4 | admit every `graph.PropertyType` in the orderable table | the `BYTES` fixture |
| 5 | swallow the `refProjectionType` error instead of propagating | the `min(p.nosuch)` invalid fixture |

Row 4 is the one most likely to come back SURVIVED, because the orderable table
has many arms and one fixture blinds one of them. If it survives, the finding is
that the table is under-tested, not that the mutation was weak — add the row per
family or say which families ship unwitnessed.

Screen every row with `go test -c -o /dev/null ./internal/query/cypher/
./internal/resolver/` before trusting a RED. Regenerate goldens before each row:
a golden that absorbs the mutant goes green, and that SURVIVED is the finding.

## 6. The ADR the execution PR owes

**One is owed, and it is not optional.** `min(p.age)` changes from `any` to
`*int32` in already-generated code, so every caller that used the column as
`any` fails to compile. That is the same class of break as ADR 0041 ("a nullable
list element is emitted as a pointer"), which is the model to follow: state the
break in the first paragraph, give the compiler error text, give the remedy, and
say which backends move together. The next free ordinal is re-derived with
`just adr-next` **at push time**, not when the branch is cut — two branches pick
the same number and never conflict.

Also owed in that ADR, because it is the part a reader will get wrong: `min` and
`max` over a `NOT NULL` property are still nullable, and the reason is the empty
group, not the property.

## 7. What this ruling leaves unsolved

- **`sum` at every width.** Declined permanently as framed (§3.2), with
  `sum(DURATION)` named as the one arm a later bead could reopen on a
  measurement. Nothing here makes `sum` easier to admit later; if it is
  admitted, it will be on an accumulator-width decision this ruling did not
  make.
- **The grouping-key sharpening** of §3.1 — reachable, declined, its own bead if
  anyone wants it.
- **Whether `min`/`max` over lists can be typed.** Not opened. The depth-0 mint
  condition is what keeps it closed, and it is closed by construction rather
  than by argument.
- **The empty-group nullability is asserted from openCypher's stated semantics,
  not witnessed here.** The generated type depends on it, so it belongs in the
  live-arm corpus against both drivers rather than only in a golden. If a driver
  disagrees, `min`'s column type is wrong in the safe direction (a pointer that
  is never nil), which is why this is an obligation and not a blocker.
- **f45qn §9's remaining non-goals** are untouched: parenthesised bare refs, map
  literal values, WHERE-position expressions, UNWIND source lists, and
  `collect`'s null-skipping element-nullability sharpening.

## 8. Rejected alternatives

- **Export `aggregateResultType`.** §2 — the two sides range over different type
  languages, and the resolver would have to destroy ADR 0002's width to make the
  call and guess it back afterwards. The dialect-coupling objection the bead
  raises is true and is the weaker of the two reasons.
- **Carry `aggregateResultType`'s verdict in the model as a new axis.** §2 — the
  verdict is already carried, as `Type()`. What the resolver needs is a table
  the parser cannot compute, because it has no schema.
- **Admit the trio uniformly.** Falsified by §3.2: `min`/`max` select and `sum`
  folds, so the width argument that admits the first two is the same argument
  that refuses the third. A uniform answer would have to be uniformly `any`.
- **Reach the fill by relaxing `fillLeaf`'s belt.** §4 — it would authorise
  `avg`'s fill as a side effect, silently, and the belt is the one artefact in
  the f45qn design specifically built to stop that.
- **Do nothing.** Defensible, and it was the shipped answer until now. It is
  declined because §1 shows the distinguishing information was already in the
  model and the deferral rested on a premise about the bare unknown that the
  projection's own `Func()` falsifies.
