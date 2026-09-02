# AGE admits non-zoned temporal list elements at every depth

A property or parameter whose width is a list — at any nesting depth — of
`DATE`, `LOCALTIME`, or `DURATION` generates for the Apache AGE target. A
list of `TIME` or `TIMESTAMP` is refused at every depth with
`ErrUnrepresentableWidth`, exactly as before. This document ratifies the
first half, which drifted onto master without a ruling, and gives the second
half the reason it has so far carried only in a code comment.

Written 2026-09-01 by Արփինէ, ruling bead `gqlc-iahs`. The one code change
this ruling mandates — an encoder defect at depth ≥ 2, below — is executed
under `gqlc-vhvz7` and tracked as bug `gqlc-jc8mc`, not here.

## How the question arrived

ADR 0033, which fixed the five driver-neutral temporal carriers and the AGE
scalar encodings, deferred this family explicitly: "Lists of temporals stay
refused… admitting the non-zoned ones alone is a follow-up with its own
bead, not a rider here." That bead is this one.

By the time it was taken, master had moved. PR #1679 rewrote the AGE type
table's list arm and admitted temporal elements implicitly; PR #1797
narrowed the admission deliberately, refusing any element for which
`carriesZone` holds at any depth; PR #1812 added live equality witnesses for
depth-1 lists of all three non-zoned carriers (`age_test.go`'s
`listCarrierParamQuery`, bead `gqlc-t0dp`). So the open choice the bead
posed — admit, or document a dialect gap and keep the refusal — had already
been half-taken by the tree. This ADR's job is to decide whether that drift
is the right rule, and it is, with one defect to name.

## The rule, and why it is the element-width rule

The admission is a property of the **element**, not of the list shape:

```go
if pt.Kind() == graph.KindList {
    elemTy, ok := t.Property(pt.Elem())
    if !ok || carriesZone(elemTy) {
        return "", false
    }
    return "[]" + elemTy, true
}
```

(`internal/codegen/age/types.go`.) Recursion through `Property` means the
rule composes: `LIST<LIST<DATE>>` is admitted because `LIST<DATE>` is,
because `DATE` is; `LIST<LIST<TIME>>` is refused at any depth because `TIME`
is. `types_test.go` already pins both directions at depth 1 and depth 2.

Per-element encodings are the scalar table of ADR 0033, unchanged:

| element | agtype encoding |
|---|---|
| `DATE` | ISO `YYYY-MM-DD` string |
| `LOCALTIME` | microseconds since midnight, int64 |
| `DURATION` | total microseconds, int64 (encode refuses `Months != 0`) |

No list-level wrapper, no length prefix: a `LIST<DATE>` is an agtype array
of ISO strings, which plain Cypher and every other AGE client reads without
gqlc's help.

**The gate for a list encoding is round-trip and equality** (`WHERE prop =
$param`), witnessed live. It is *not* ordering. ADR 0033's scalar encodings
were chosen so lexical/numeric order matches chronological order; whether
agtype compares arrays element-wise in a way that extends that property is
unmeasured, and this ADR deliberately claims nothing about `ORDER BY` or
range predicates over list-typed properties. A future bead that wants that
claim must bring a measurement.

## Why the zoned family stays refused, at every depth

A scalar `TIME` or `TIMESTAMP` property survives on AGE only because of ADR
0033's sidecar: the instant is stored UTC-normalised and the zone offset
rides in a **flat sibling property**, `<f>Offset`, one per property. A list
element has no sibling. There is nowhere in the encoding for the offset of
any element but — at best — the first, and a zone silently dropped from
elements 2..n is exactly the corruption the sidecar exists to prevent.

So `carriesZone` refuses `time.Time` and the zoned carrier at every depth,
and the refusal is loud (`ErrUnrepresentableWidth`, naming entity, property,
and width) rather than a documented data hazard. This is the **documented
dialect gap** half of the original question: zoned temporal lists work on
neo4j and do not exist on AGE, and a schema that declares one is refused for
the AGE target at generation time.

### Rejected: a parallel offset-list sidecar

`<f>Offsets :: LIST<INT>`, one offset per element, index-aligned. Rejected
because it manufactures a corruption class the flat sidecar cannot have —
a length mismatch between value list and offset list, writable by any
non-gqlc client and detectable only at decode — to serve a demand nobody
has brought. Same verdict for encoding each element as a `{t, o}` map:
that is a gqlc-private storage dialect inside a shared database, the shape
ADR 0035 already rejected for nested lists on neo4j. Both remain reversible
later; the refusal loses no stored data.

### Rejected: refusing depth ≥ 2 instead of fixing the encoder

Nested lists of temporals are constructible **only on AGE** — ADR 0035 has
neo4j refuse every nested-list stored property — so refusing them here
would erase the width from the product entirely, on the one backend whose
store holds it, to avoid a bounded encoder fix. The admission rule above is
honest about composition; the emitter should be too.

## The defect this ruling surfaces: depth ≥ 2 encodes the wrong bytes

Admission and decode compose; encode does not. `listHelperName`
(`render_models.go`) loops `strings.CutPrefix(goType, "[]")` and emits
`agtypeListOfListOfDate` for a depth-2 property. `fallibleParamEncoder`
(`render_queries.go`) calls `CutPrefix` **once**:

```go
elem, list := strings.CutPrefix(f.GoType, "[]")
```

so `[][]civil.Date` leaves `elem = "[]civil.Date"`, matches no leaf arm,
and returns `("", false)` — which the call site reads as "crosses raw".
The parameter goes into `agtypeArgs` (plain `json.Marshal`, and the
carriers define no `MarshalJSON`), crossing as JSON objects while the
decode path expects ISO strings.

Witnessed 2026-09-01 by live generation from a scratch fixture (`dates2 ::
LIST<LIST<DATE NOT NULL> NOT NULL>`, `WHERE g.dates2 = $dates2`): the
generated args map carries the parameter untouched. Equality can never
match; nothing reds; the corpus contains no AGE fixture at this width, which
is why it stayed silent since #1679.

The mandated fix (`gqlc-vhvz7`, bug `gqlc-jc8mc`) is to make the encoder
compose recursively, mirroring the decode side's loop — not to refuse the
width, per the rejection above. The execution also enrols fixtures so the
corpus witnesses what this ADR claims: the existing `temporal_list_param`
fixture (all non-zoned, currently neo4j-only) gains the AGE target, and a
new AGE-only fixture carries a depth-2 non-zoned temporal list. The loud
zoned-list refusal needs no new fixture: `invalid/
unrepresentable_width_list_element_schema` already pins `LIST<TIMESTAMP>`
against `ErrUnrepresentableWidth` on the AGE target, and `types_test.go`
pins the depth-2 refusals at unit level.

## Consequences

- The AGE dialect table gains a row family: lists of non-zoned temporals,
  any depth, per-element scalar encodings, equality-gated. Zoned temporal
  lists are the named, refused gap.
- `LOCALDATETIME` does not appear in this ruling because the width does not
  exist in gqlc's vocabulary (`internal/graph/propertytype.go` has no such
  type); the `LocalDateTime` carrier of ADR 0033 is reachable only from
  widths that do not form lists here. If the width is ever added, its list
  form is non-zoned and falls under the admission rule.
- Until `gqlc-vhvz7` merges, a depth ≥ 2 non-zoned temporal list parameter
  on AGE generates code whose equality predicate cannot match. That window
  is accepted rather than patched with a temporary refusal: the width has
  no known user, the corpus proves nobody generates it today, and a
  stop-gap refusal would ship a second behaviour change only to be reverted
  by the fix.
