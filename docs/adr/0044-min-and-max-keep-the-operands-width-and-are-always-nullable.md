# min and max keep the operand's width, and are always nullable

A query returning `min(p.rank)` or `max(p.name)` over a declared property now
generates the property's own Go type behind a pointer where it generated `any`.
With `rank :: INT32 NOT NULL`, `MinRank` moves from `(any, error)` to
`(*int32, error)`; with `name :: STRING NOT NULL`, `MaxName` moves to
`(*string, error)`.

**This is a breaking change to already-generated code**, and the break is the
reason this document exists rather than only a commit message. Regenerating
against an unchanged schema changes the signature of an existing query method,
and every caller that reads the result as an interface fails to compile:

    invalid operation: v (variable of type *int32) is not an interface

The remedy is to drop the assertion and nil-check the pointer instead — the
value is already the width the schema declares, so `if v != nil { use(*v) }`
replaces `if n, ok := v.(int64); ok`. Note the second half of that swap: the old
assertion had to name the DRIVER's carrier (`int64`, whatever the schema said),
and the new pointee names the SCHEMA's (`int32`). A caller that kept asserting
`int64` was reading a width the declaration never promised.

All three targets move together — `neo4j-go-v5`, `neo4j-go-v6` and
`apache-age-pgx-v5` are byte-identical on both signatures, so there is no target
to switch to in order to keep the old text. The goldens are under
`test/data/codegen/valid/certified_selection/`.

On AGE the change is not only a stronger type. That backend serves scalar and
entity columns only, so a query whose column stays `any` is DROPPED from the
generated package rather than emitted weakly: `min(p.rank)` had no method there
at all before this, and `certified_selection`'s AGE golden exists because the
selection now commits a scalar.

Written 2026-09-10, executing spec `ruling-p9qgu-fold-result-upgrade.md` on bead
`gqlc-b8m8f`.

## The pointer is the match's emptiness, not the property's

`rank` is declared `INT32 NOT NULL` and `min(p.rank)` is still `*int32`. That is
not an oversight and it is not the ADR 0041 rule leaking into a new position:

- **`NOT NULL` is a claim about a PROPERTY of a node that exists.** Every
  `:Person` the store holds has a rank.
- **`min` answers about a GROUP, and a group can be empty.** An aggregation with
  no grouping key over zero matched rows still answers one row, and that row's
  value is null. No schema constrains how many nodes a query matches, so no
  schema can take that null away.

So the nullability is not derived from the operand's at all: it is forced true.
Reusing the ref's own nullability verbatim would emit a bare `int32` field into
which the driver hands a NULL, which is the ADR 0041 defect pointed the other
way.

That emptiness is the one premise here that is a fact about the SERVERS rather
than about gqlc, so it is witnessed on both rather than quoted from openCypher:
`test/data/codegen/live_empty_group_aggregate_test.go` sends the same six
statements to neo4j 5 and Apache AGE and asserts each answers exactly one row
holding null, with the non-empty match beside it answering the extremes so a
server that answered null to everything cannot pass. Exactly one row matters as
much as the null does: zero rows would be reported as `ErrNoRows` by a generated
one-row method and the column's type would never be reached.

## Why the width survives at all

`min` and `max` do not FOLD, they SELECT. The result is one of the operand's own
values, returned unchanged, so it is exactly as representable as the property is
and the machinery that already round-trips a bare `RETURN p.rank` covers it with
nothing added — [ADR 0002](0002-bit-width-preserving-value-type-model.md)'s width
preservation, with [ADR 0037](0037-a-stored-value-outside-the-declared-width-fails-the-read.md)'s
refusal of a stored value outside the declared width.

`sum` is the contrast, and it is the reason this is a rule about two functions
and not about aggregates. `sum` commits the result of a fold: the leaf holds a
value no row ever held, so the operand's width is not the answer's. Committing
`INT32` there would fail an ADR 0037 read on data the schema permits — two
in-range `rank`s can sum out of range — and committing a wider width would be a
claim about the driver's accumulator that the schema cannot make. **`sum` stays
`any`, permanently rather than pending**, and so do `avg` and every other fold.
`collect` is unaffected: it already filled its list spine's leaf and continues
to.

## What fills, and what stays `any`

The upgrade applies only where all four hold. Anything else degrades to today's
`any`, which costs a column its type and cannot be wrong:

| shape | resolves to | why | witnessed by |
|---|---|---|---|
| `min(p.rank)`, `rank :: INT32 NOT NULL` | `*int32` | the rule | `certified_selection`, `certified_min_width_preserved` |
| `max(p.name)`, `name :: STRING NOT NULL` | `*string` | the rule, and the NOT NULL trap above | `certified_selection`, `certified_max_not_null_nullable` |
| `min(p.meta)`, `meta :: ANY VALUE` | `any` | not an orderable family | `certified_min_unorderable_stays_any` |
| `min([p.rank])` | `any` | the operand is not depth 0 | `certified_min_single_ref_list_stays_any` |
| `min([p.id, p.age])` | `any` | and its refs do not unify to one | `certified_min_list_stays_any` |
| `min(p)` | `any` | a node binding is not a property | `certified_min_node_stays_any` |
| `sum(p.rank)`, `avg(p.rank)` | `any` | a fold, not a selection | `certified_sum_stays_any`, `certified_avg_stays_any` |

The two list rows are not one row written twice. `min([p.id, p.age])` never
reaches the depth condition — the resolver declines it earlier, because its refs
do not name one property — so it stays `any` even with the depth condition
deleted, and only `min([p.rank])` isolates the condition that is actually
carrying the rule. That was found by mutating the condition and watching the
first fixture stay green.

The `any` rows are witnessed in the resolver corpus rather than as goldens,
because on AGE an `any` column takes the whole fixture off the target (above) and
the degrade is a resolver decision that needs no emission to record.

The orderable families are the scalars carrying a total order the selection
stays inside: `STRING`, `BOOL`, the five temporal carriers, and every signed,
unsigned and floating width. It is an **allow-list**, so a family nobody has
ruled on degrades rather than guessing. Two exclusions are deliberate and named
rather than accidental:

- **`BYTES`** is the ruling's own named refusal: it is not ordered by the
  aggregate.
- **`DECIMAL`** carries an order but is not among the families the ruling
  enumerates. Refusing it is a non-decision that costs a column its type;
  admitting it would be a decision nobody has taken.

`ANY VALUE`, `LIST` and `RECORD` stay out for a third reason: whether a list
ordering is well-defined enough to type its result is a question the ruling
explicitly does not open, and the depth-0 condition keeps it closed.

`internal/graph.PropertyType` is a string type with an open composite grammar
rather than a closed enum, so the allow-list cannot be an exhaustive switch a
linter holds. `TestOrderableSelectionNamesEveryPropertyType` holds a verdict for
every constant `propertytype.go` declares, reading the constant set out of that
file's source, and fails on a declared type the table does not name — so a new
width cannot be refused by default with nobody having said so.

## One acceptance change, in the refusing direction

`min(p.nosuch)` now refuses with `ErrUnknownProperty` where it previously
resolved silently to `any`. That follows from resolving the operand through the
same reader a bare `RETURN p.nosuch` goes through, which is also what keeps the
filled type byte-identical to the bare projection's; `collect` already behaved
this way. A query that named a property no schema declares was not generating
anything useful before.
