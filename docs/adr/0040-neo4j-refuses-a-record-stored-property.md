# The neo4j backend refuses a record as a stored property

A schema declaring `addr :: RECORD<city STRING NOT NULL, zip INT32>` on a node
type fails generation for the neo4j targets:

    unstorable property width: entity "Person" property "addr" has
    RECORD<city_STRING_NOT_NULL,zip_INT32>, which the neo4j backend cannot
    store as a property

So does `RECORD<ANY>`, `RECORD<>`, and a `LIST` of any of them. The same
declarations generate for Apache AGE.

Written 2026-09-05 by Այգ, executing stage 1 of the design ruled by Արթուր in
`docs/specs/codegen-record-union-carriers.md` (bead `gqlc-x9tg7`, executed on
`gqlc-jffyz`).

This ADR is ADR 0035's sibling with the **same** roles, not reversed: neo4j
refuses the storage, AGE performs it, and the refusal is on the storage axis
while the carrier axis still answers. What differs is the size of the
consequence, which §6 of the spec is about and which the last section here
records.

## The two measurements

Against the pinned images, 2026-09-05, on the live-smoke runner:

- **neo4j refuses the write.**
  `CREATE (:RecordPropertyProbe {addr: {city: 'Yerevan', zip: 1}})` comes back
  with

      Neo.ClientError.Statement.TypeError (Property values can only be of
      primitive types or arrays thereof. Encountered: Map{zip -> Long(1),
      city -> String("Yerevan")}.)

  The rule is the server's, not the driver's, which is why one arm (v5)
  witnesses it rather than one per driver major.

- **AGE performs it.** The same property is created and read back, and both
  fields survive as a nested object rather than as a flattened or stringified
  map. agtype is JSON-shaped.

Both are live rows: `TestNeo4jRefusesAMapValuedStoredProperty` and
`TestAGEStoresARecordProperty` in
`test/data/codegen/live_record_property_test.go`. The first is the premise's
tripwire — if an image bump ever serves the write, it reds and the town
re-opens the question with evidence rather than with this document.

### The neo4j row carries two controls, and they are the point

The outcome this design expected was a REFUSAL, and a refusal is also what a
broken probe produces: a connection failure, a bad credential and a syntax
error all present as "the server said no", and every one would have confirmed
the expected branch for the wrong reason. So two controls ran green in the same
run before the refusal was read as a fact about maps:

- a scalar property on the same label and the same session **was stored**, so
  the write path, the session and the credentials are known good;
- the identical map came back as a **projected column**, so the refusal is
  about STORAGE and not about the server handling a map at all.

That second control is also the live half of the asymmetry §6 turns on.

## The rule is wider than the design drafted it, on measured evidence

§5 named `KindRecord` and `TypeAnyRecord`. A `LIST<RECORD<…>>` is neither, and
would have reached the server through `StorableProperty`'s existing list arm,
which asks only whether the ELEMENT is itself a list. The AGE-only record
fixture already declares one, so this is a width the language reaches today.

It could not be inferred either way. The rule the server states admits *"arrays
thereof"*, and a flat list of scalars **is** stored — that is ADR 0035's own
premise and its control. So the sibling width was asked about separately, in
the same file and the same run, and is refused by the same rule with the same
wording. The arm covers it:

```go
func (typeMap) StorableProperty(pt graph.PropertyType) bool {
	if pt.Kind() == graph.KindRecord {
		return false
	}
	if pt.Kind() != graph.KindList {
		return true
	}
	elem := pt.Elem().Kind()
	return elem != graph.KindList && elem != graph.KindRecord
}
```

`Kind()` tests the `RECORD<` prefix, which `RECORD<ANY>`, `RECORD<>` and every
declared record share, so one arm covers all three spellings. `Elem()` strips
the `NOT NULL` suffix, so `LIST<RECORD<…> NOT NULL>` is caught the same way
ADR 0035's nested list is. A depth-3 list of records is already refused one
level out by the nested-list arm. AGE's `StorableProperty` stays `return true`.

`LIST<ANY VALUE>` remains ADMITTED and can carry a map at runtime, which no
static check can see; that write fails at the server as it does today. ADR 0035
names the same limit.

## Storage, not carrier — and here the split does more work than in 0035

`TypeMap.Property` still answers `map[string]any` for `RECORD<ANY>` and a
struct text for a declared record, and is right to: a record arriving as a
QUERY VALUE decodes on this backend, and the projection control above is the
server half of that. Putting the refusal on the carrier axis would claim a
carrier that exists does not, and would take the query-value decode with it.

Every row of `TestStorablePropertyRefusesARecord` therefore asserts **both**
axes — `StorableProperty == false` beside `Property` returning `ok` — so a
later change that moved the refusal onto the carrier cannot pass there.
`TestRecordPropertyRejectionReachesTheCaller` is the caller-facing half, and
its `NotErrorIs` is load-bearing rather than the `ErrorIs` restated: a record
with an unrepresentable FIELD is refused through `ErrUnrepresentableWidth`,
which is a real refusal of a real record and would be the wrong one here.

## The consequence, which is larger than ADR 0035's

§6 predicted it: with no legal record declaration, there is no property for a
column to project or a parameter to be compared against, so **no valid input
reaches a neo4j record column or parameter** and the four consumption positions
collapse to zero on that backend. A record width has exactly one derivation
root — a property or field type in the schema — and nothing else in the
language mints one. ADR 0035's refusal costs neo4j one declaration, because
`collect()` still derives a nested list from a storable flat one; a record
refusal costs it the whole position set.

Two things follow, and both are done rather than noted.

**The decoder probe loses its record leaf.** `decoderProbeLeaves` is
schema-driven — every entry is declared as a stored property — so a width the
storage axis refuses cannot appear in it at all: generation refuses the schema
and the probe fails to build rather than measuring any decode arm. The
coverage test is paired with that rather than weakened. Its leaf table must now
be **storable as well as carried**, which is the assertion that fires, and its
coverage loop excludes only what `StorableProperty` refuses.

**The record carrier and render half nevertheless stays in the tree**, on the
sentinel-honesty ground §5 ruled: unreachable *from a schema* is not the same
as unreachable, and the code stays unit-witnessed and mutation-screened where
it stands. Its witness is `internal/codegen/neo4j/render_record_test.go`, which
builds its widths with `graph.RecordOf` instead of parsing a schema — the
non-schema path §5 anticipated.

## Attribution: neo4j names itself here, and that is not ADR 0035's answer

ADR 0035 ruled that *a backend names itself exactly when another enrolled
backend answers the same declaration differently*, and under that rule declined
carrier wording for neo4j while keeping the storage suffix. The record widths
are squarely on the owed side: AGE stores them and neo4j does not, so a run
emitting both targets fails on one and only the name says which.

Nothing new is written to hold that. `TestAContingentRefusalNamesItsBackend` in
`internal/cli/backends` already sweeps each scalar as the single field of a
record and as a list of that record, plus the two fieldless records, and
requires a backend refusing what another accepts to name itself. Before this
change those rows divided the roster nowhere and passed vacuously; they now
carry the divergence actively, and the day AGE's answer changes the sweep reds.

## What this does not decide

Unions. §5's ruling extends the storage refusal to *"any union with a
map-family or record member"*, and that is stage 2's to write when the union
work lands. This ADR records the record half only, which is what has been
measured.

Field access *into* a record inside query text (`RETURN p.addr.city`) is out of
scope for both stages; a record value moves whole. That limit is the spec's
(§6) and is repeated here only so a reader of this ADR does not infer it was
settled.
