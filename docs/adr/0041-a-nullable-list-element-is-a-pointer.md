# A nullable list element is emitted as a pointer

A schema declaring `scores :: LIST<INT64>` now generates `[]*int64` where it
generated `[]int64`. `LIST<INT64 NOT NULL>` still generates `[]int64`.

**This is a breaking change to already-generated code**, and the break is the
reason this document exists rather than only a commit message. Regenerating
against an unchanged schema can change the signature of an existing query
method, and every caller that reads an element fails to compile:

    cannot use v (variable of type *int64) as int64 value in ...

The remedy is a dereference at the call site, or `LIST<INT64 NOT NULL>` in the
schema if the elements were never meant to be null. Both backends move
together — `neo4j-go-v5`, `neo4j-go-v6` and `apache-age-pgx-v5` emitted the
identical wrong signature before and emit the identical corrected one now, so
there is no target to switch to in order to keep the old text.

Written 2026-09-08, executing the design ruled on bead `gqlc-sokgc` and
implemented on `gqlc-dxhwp`.

## What was wrong

The resolver commits to element-position nullability and codegen threw it
away. `buildListElemPlan` read the element's width and never its `Nullable`,
and both backends' list arms composed `"[]" + elem` without consulting
`ElemNotNull`. So `LIST<INT64>` — a declaration that says an element may be
NULL — produced `[]int64`, a Go type in which no element can be absent.

The emitted decode then asserted each driver element straight to the bare
carrier. On neo4j a NULL element arrives as a nil `any`, so `elem.(int64)`
reports `ok=false` and the whole row fails, naming the column and the element
index; on AGE the element decoder meets the literal `null` and refuses it.

That is **fail-closed rather than corrupting** — nothing was silently
mistyped, which is why this was not treated as a P1. But it failed on a value
the schema declared legal, and the type it failed through was a type the
schema had not asked for. A `[]int64` over a `LIST<INT64>` is not a
conservative approximation of the schema; it is a claim the schema
contradicts.

## The rule

An element the schema permits to be NULL takes a leading `*` on its emitted
Go type. One carve-out, and one qualifier that opts out:

- **`any` stays bare.** An element whose mapped type is `any` — `ANY VALUE`,
  a bare `LIST`, a resolved null scalar — already carries absence as nil, so
  a star would be a second spelling of the same thing. `LIST<ANY VALUE>` is
  `[]any`, not `[]*any`. On neo4j this is load-bearing rather than tidy: the
  emission deliberately does not walk a `[]any` at all, because walking one
  would fail the whole decode on exactly the null element that width exists
  to carry.
- **`NOT NULL` opts out.** `LIST<T NOT NULL>` keeps `[]T`, and the emitted
  decode still refuses a null element naming its index. A schema that has
  always been honest about its elements sees no signature change at all.

Everything else takes the star uniformly: the scalar widths, `STRING`,
`BYTES`, the temporal carriers, records, and nested lists.

### Each level of nesting decides for itself

The qualifier is read at the level it is written, so the two stars a nested
list can take are independent:

| declared | emitted |
|---|---|
| `LIST<LIST<STRING>>` | `[]*[]*string` |
| `LIST<LIST<STRING> NOT NULL>` | `[]*[]string` |
| `LIST<LIST<STRING>> NOT NULL` at the element position | `[][]*string` |

There is no rule that a starred outer element implies a starred inner one, or
the reverse. Depth 3 composes the same way.

Worth knowing where those shapes come from on neo4j, because
[ADR 0035](0035-neo4j-refuses-a-nested-list-stored-property.md) refuses a
nested list as a **stored property** on that backend and it would be easy to
read the table above as unreachable there. It is not: that refusal is about
storage and says nothing about values, and the nesting can be built by the
query rather than declared. `RETURN [g.tags, g.tags]` over a flat
`tags :: LIST<STRING>` projects `[][]*[]*string` on every target
(`test/data/codegen/valid/nested_list_element_projection`).

## Why a pointer, and not the two alternatives

**Refuse a nullable element at codegen with a named sentinel.** Fail-closed at
generate time instead of run time, which is the direction gqlc usually
prefers. Rejected because it regresses every stored-list schema that generates
and works today whenever no element happens to be NULL, and because the
element position would then be refused in one context and admitted in another
— which `docs/specs/model-change-f45qn-ref-valued-leaves.md` §6 had already
ruled against, requiring stored lists and projection lists to share one
answer.

**Degrade a nullable element to `any`.** Keeps the current runtime behaviour
and every existing signature. Rejected because it throws away the typing the
element position had just won, and hides absence from the type rather than
expressing it — the same fidelity commitment
([ADR 0002](0002-bit-width-preserving-value-type-model.md)) that rules out
refusing it.

What decided it is that the element position was the **one** nullable position
exempted from a rule the emission already applies everywhere else. Entity
fields, row fields and parameters are all wrapped when nullable, including a
shapeless `*any`. CONTEXT.md's "Nullable" entry says a nullable result is
emitted as a pointer or option type and does not carve out list elements. The
exemption was the bug, and the three candidates were not equally available:
two of them require the emission to keep discarding a field the resolver
computes.

## Consequences

- Any schema declaring a list property or projecting a list of a nullable
  property changes signature on regeneration. The break is compile-loud at
  every caller, never a silent behaviour change, and it does not reach a
  schema whose list elements are all `NOT NULL`.
- The corpus moved about sixty goldens. One fixture name now reads oddly and
  is correct: `two_non_null_list_columns` moves `Ranks []int32` to `[]*int32`,
  because the name refers to the two COLUMNS being `NOT NULL` and not to their
  elements.
- `TypeMap.Property` owns the element star; the whole-value star is still the
  caller's. A backend adding a list arm has to apply it, and
  `TestListColumnTextAgreesWithItsElementPlan` in each backend package is what
  fails when only one of the two routes to the text does.
