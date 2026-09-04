# The type map answers whether a backend carries a kind, one arm per kind

`TypeMap.Temporal` returns `(goType string, ok bool)`. A backend with no
faithful Go carrier for a temporal kind answers `ok=false`, and the shared phase
turns that into `ErrUnrepresentableTemporal` naming the query, the column and the
kind. The answer is given per kind, in its own switch arm, not per backend and
not in the backend's unserved-query gate.

`TypeMap.Scalar` keeps a total signature. `TypeMap.Property` already had the same
`(string, bool)` shape; this aligns `Temporal` with it.

## Context

`Temporal` was documented "total over the closed enum", which was true while
neo4j was the only backend: the driver ships a `dbtype` carrier for every kind.
Apache AGE falsifies it. `agtype` has no temporal value and no cast reaches one
(spike `gqlc-35yu.5`, empirically confirmed against the `pg_cast` table), so
there is no kind AGE can carry natively.

With no way to refuse, AGE's table returned `"any"` for every kind — a column no
emitted decoder can fill, written at exit 0 with no diagnostic. That is the
opposite disposition from the one the `Property` path takes for a width the
backend cannot hold, and the difference came from the interface's shape rather
than from a decision.

### Why the type map and not the backend's gate

AGE already has a per-query gate (`rejectUnservedQueries`) that drops a query
whose columns it has no emission for. Answering temporal there would work, and
was rejected on two counts.

The gate reports **one reason for a whole query**, so it cannot name the kind, and
the message an author needs is the kind — `RETURN d.created` fails differently
from `RETURN duration(...)` once the encodings land one at a time. And the type
map is already the place that answers "does this backend carry X" for widths;
putting temporal somewhere else means two tables that have to agree, with nothing
holding them together.

### Why per kind and not per backend

A single `Temporal(k) bool` on the backend, or one blanket refusal, would be
smaller. It would also be wrong by the time the encodings land: the spike found
faithful encodings for instants, dates, durations and zoned datetimes, and **no
faithful encoding for a calendar duration** (months and years are not a fixed
micro count). So the enum splits, and the split has to be expressible.

One arm per kind buys a second thing. `exhaustive` holds a switch to its enum's
full membership, so a kind the resolver gains fails the build in every backend
table rather than inheriting the answer chosen for the kinds before it. The
previous AGE implementation ignored its argument entirely, which left the linter
nothing to hold — the only type-map entry in the tree with no growth fence.

### Why a new sentinel rather than `ErrUnrepresentableWidth`

A temporal expression carries no property width at all: the resolver keeps
`ResolvedTemporal` apart from `ResolvedProperty`'s `DATE` / `TIMESTAMP` families
(ADR 0002). The two sentinels therefore ask for different edits — a schema change
for a width, a `RETURN` clause change for a kind — and folding them would make the
message name a width that does not exist.

### Why `Scalar` keeps a total signature

`resolver.Scalar`'s membership — bool, int, float, string, null, map — is the
openCypher literal vocabulary. A store a Cypher query runs against accepts a
value of each written into a query, so every backend has something to answer with
and the signature admits no refusal. A kind added to that enum with no such value
behind it would falsify the ground, and would need the channel `Temporal` now
carries.

That is a claim about the **store**, and it is narrower than it looks. It does not
say a backend can decode every kind it can name, and on AGE it does not: see
below.

## Considered options

**Leave `Temporal` total and let AGE return `"any"`.** Rejected: it emits a
column no decoder can fill at exit 0. Generate-time refusal is this codebase's
posture for "this backend cannot represent that", and temporal escaped it by
accident of interface shape.

**Refuse temporal in AGE's unserved-query gate.** Rejected: the gate reports one
reason per query and cannot name the kind, and it duplicates an answer the type
map already owns for widths.

**One blanket refusal per backend rather than one arm per kind.** Rejected: the
encodings land per kind and one of them never will, so the enum splits; and a
switch is what gives `exhaustive` a fence against the resolver growing a kind.

**Reuse `ErrUnrepresentableWidth`.** Rejected: it would name a width a temporal
expression does not have, and point the author at the schema rather than the
query.

**Give `Scalar` the same channel for symmetry.** Rejected: nothing in it is
unrepresentable in the store sense, and a refusal channel no implementation takes
is surface that reads as a capability. What AGE actually lacks for two of its
scalar kinds is a decode helper, which is a different fix — below.

## Consequences

- The shared phases fail on a temporal kind at two sites, `phaseBDerive` for a
  column and `buildListElemPlan` for a list element's leaf, each naming the kind.
  Both are fenced independently.

- AGE refuses every temporal kind today, so no fixture may enrol an
  `apache-age-pgx-v5` target on a query projecting a temporal column until
  `gqlc-35yu.11` commits the encodings. It admits kinds one arm at a time as that
  bead lands; the calendar duration arm is expected to stay refused permanently.

- **The AGE `Scalar` table is answered in two places, and the gate is what makes
  the table's answer inert.** The table names `any` for `ScalarNull` and
  `map[string]any` for `ScalarMap`; `unservedColumn` returns a reason for a
  column of either kind, `unservedReason` carries it, and
  `rejectUnservedQueries` reads it ahead of `Prepare` — so the query is refused
  before any `Row` is derived, and no emitted code reaches those two table arms.
  One case is answered elsewhere: where the query text also spells a dialect
  gap, `rejectUnservedQueries` stands aside and `rejectDialectGaps` answers
  instead. Both gates run ahead of `Prepare`, so the column reaches no emission
  on either path.

  That refusal is load-bearing, but not for the reason first recorded here. It
  is not propping up a missing helper. `decodeFunc` answers both texts — `"any"`
  with `agtypeValue`, `"map[string]any"` with `agtypeMap` — and both helpers are
  emitted, gated together on `helpers.value`. Six goldens carry them, and they
  arrive by two routes rather than one. Four declare an ANY width in the SCHEMA,
  as a property or inside a list of one: `schema_any_property`,
  `schema_any_property_alone`, `schema_list_any_property` and
  `nested_list_property`. Two declare no ANY width at all —
  `certified_list_element`, whose widest declared property is `INT64`, and
  `list_unknown`, which declares one `INT64` property and nothing else — and get
  the carrier from a list expression whose element type the resolver does not
  fix: `RETURN [foo(p.id)]`, an unknown function's result, and `RETURN [p.id +
  p.age]`, a fold over two declared `INT64` properties that mints no element
  certificate. Neither route is a scalar column, which is the thing this bullet
  is about. In each of the six, `agtypeMap` is named three times: its own
  declaration, that declaration's doc comment, and `agtypeValue`'s call on the
  `'{'` arm, which is the only CALL outside the helper.
  `agtypeMap` returns `map[string]any`, the same text the
  table names for `ScalarMap`, and `agtypeValue` reads an agtype inline `null`
  as Go `nil` rather than refusing it.

  So the two arms are not placeholders for helpers that do not exist. They are
  table entries the gate keeps out of emission, which is what `types.go`'s
  `Scalar` doc says at the site. Lifting the gate arm is a question about
  whether the value `agtypeMap` returns is the one a map column should carry,
  not about writing a helper first.

  Nor is either `decodeFunc` arm optional.
  `TestDecodeFuncHasAnArmForEveryCarrierTheTypeTableProduces` reads the Go type
  texts `typeMap` returns out of the package's own AST and requires an arm for
  each; `TestDecodeFuncNamesTheHelperForEveryServedCarrier` names the helper per
  text. Deleting either arm reddens both — but the closure pin's count is not
  one subtest per mutation, and the two pins do not redden in the same run.
  Measured at `bc9d7acc`: deleting the `map[string]any` arm reddens one closure
  subtest, `Scalar/map[string]any`; deleting the `any` arm reddens three,
  `Property/any`, `Property/[]any` and `Scalar/any`, because the table names
  that text on both halves and once more as a list element.

  Both counts are readable only under a `-run` that selects the closure pin
  alone. `decodeFunc` answers an unknown carrier by panicking, and a panic in a
  subtest takes the whole test binary rather than that subtest, so in any run
  that reaches an earlier caller the closure pin never executes. Under `go test
  ./internal/codegen/age/` the `map[string]any` mutation dies in
  `TestDecodeFuncNamesTheHelperForEveryServedCarrier`, which precedes it in this
  file, and the `any` mutation dies earlier still, in
  `TestNarrowingWidthsAgreesWithTheTypeTable`. A reader reproducing "reddens
  both" the obvious way sees neither count.

  So the `map[string]any` arm is held up by that pin and not by this bullet:
  the one place the table produces that text is `Scalar`'s `ScalarMap` arm,
  which the gate above refuses.

- The declared Go surface is backend-invariant and the runtime types behind it
  need not be. This ADR recorded one instance — a `map[string]any` column
  decoding an integer member as `float64` on AGE against `int64` on neo4j — and
  it is not one. The neo4j half of it holds; the AGE half does not.
  `agtypeMap` does not unmarshal the object as JSON: `agtypeObject` splits it by
  scanning bytes, and each member is read by `agtypeValue`, which tries the
  integer parse before the float one. Run against the `schema_any_property`
  golden, `agtypeMap` over `{"n": 3, "f": 1.5, "s": "x", "b": true, "z": null}`
  yields `int64` for `n`, `float64` for `f`, `string` for `s`, `bool` for `b` and
  `nil` for `z`. Both neo4j drivers answer that object with those same types:
  `amap` reads each member with `value`, which answers a packed integer with
  `Int() int64`, a float with `Float() float64`, a string with `String() string`,
  `PackedNil` with a nil `any`, and `PackedTrue`/`PackedFalse` with `bool` —
  identically on `neo4j-go-driver` v5.28.4 and v6.2.0, the two versions
  `test/data/codegen/go.mod` pins. A list member arrives as `[]any` and a
  nested-map member as `map[string]any` on all three, with the integer and the
  float inside them landing as the rows above say. So no runtime-type
  divergence between the backends survives for the member kinds in those two
  objects: integer, float, string, boolean, null, list and nested map.

  How that was measured, and what it does not witness. Each driver's own
  hydrator was executed over a `RECORD` packed with that driver's own
  `packstream.Packer`, which is how its `hydrator_test.go` builds a case —
  "same was as server would" — and `neo4j.Record` is an alias for the
  `db.Record` that hydrator fills, so the members measured are the ones a
  caller reads off a record. That is the driver's mapping from packed marker to
  Go type, not a live session: it does not witness which marker a server picks
  for a given Cypher value. A live run witnesses that, and now does: the
  fixture whose golden returns a `map[string]any` is `scalar_map`
  (`RETURN {a: 1} AS m`, emitted for `neo4j-go-v5` and `neo4j-go-v6`), and
  `mapColumnScenarios` drives it on both neo4j arms of the live battery,
  requiring the emitted read to answer with a one-member map whose member `a`
  is `int64(1)` (bd `gqlc-y6mo`). One member of one kind on one server, which is
  narrower than the table below in every direction — it says which marker THIS
  server picks for a literal integer inside a map, and says nothing about any
  other kind a member can hold, nor about Aura, a cluster, or another 5.x. Those
  other kinds are not unaddressed, they are addressed by a different instrument:
  the bullet below answers them structurally, from the one call site both
  `record` and `amap` fill through, which is an argument about the driver rather
  than a witness of a server. What this scenario closes is the gap that the
  marker→Go-type mapping alone left open: until it existed, no scenario in the
  live battery drove `scalar_map` at all, so a green `live-smoke` witnessed
  nothing about a map member.

  Only the AGE half of this is gated. `TestAgtypeValue`, in the corpus the
  emitted helpers are run against, reads
  `{"a": 1, "b": [true, null, "z"], "c": {"d": 1.5}}` back through the helpers
  `Generate` produced and requires `int64` for the integer member, a `[]any`
  holding `true`, `nil` and `"z"` for the list member, and a `map[string]any`
  holding `1.5` for the nested one. Emitting the float parse ahead of the
  integer one reddens it, as does coercing a decoded member to `float64`. The
  neo4j half is a driver's behaviour rather than this repository's, and nothing
  here gates it.

  One bound this does not cross. An integer member outside `int64` range
  decodes as `float64` on AGE, where `agtypeInt64` fails and `agtypeValue` falls
  through to the float parse; the driver reads every packed integer marker
  through `Int() int64`, so that is a value the neo4j path does not carry rather
  than one the two decode differently. AGE spells such a value with an
  annotation, and both integer and float helpers strip it by hand
  (`strings.TrimSuffix(…, "::numeric")`), so the width alone decides which one
  answers: `3::numeric` reaches the caller as `int64` and
  `123456789012345678901234567890::numeric` as `float64(1.2345678901234568e+29)`.
  `TestAgtypeValue` pins the annotated rows at both widths that fit and the
  unannotated overflow; the annotated overflow is measured here and not gated,
  since it is those two behaviours composed rather than a third.

  What never depended on the answer is that `TestBackendInvariantSurface`
  compares declarations — it nils each method body and passes over the
  receiver-less decode helpers, which is where this behaviour lives — so it
  would not see a divergence of this shape either way. `scalar_map` is enrolled
  in `neo4j-go-v5` and `neo4j-go-v6`, so it clears that test's two-target floor
  and is compared — but those two targets are one emitter under two version
  options, which is the reason the test's own header gives for the edge-union
  fixtures holding nothing there, and it applies here unchanged.

- A member holding a temporal, a spatial value, a byte array, or a v6 vector or
  UUID carries the same Go type as the identical value at the top level, on both
  driver majors — and on neo4j that is structural rather than sampled, so it
  holds for kinds nobody packed. `record` fills `rec.Values[i] = h.value()` and
  `amap` fills `m[key] = h.value()`: the same method, entered with the same
  `unp.Curr`, in v5.28.4 and v6.2.0 alike. There is no per-kind bound to look
  for because the one call site covers every kind `value` can return. That is
  the answer to the question `gqlc-c4mb` raised — the rows below check the
  reading, they do not enumerate the set.

  Each row packed ONE `RECORD` carrying the value twice, bare as field 0 and as
  the single member of a map as field 1, so a difference could not come from
  hydrator state drifting between two runs, and hydrated it with the driver's
  own `hydrator` (`boltMajor: 5`, `useUtc: true`; v6 additionally
  `supportsUuid: true`). Both majors: `'D'` → `dbtype.Date`, `'t'` →
  `dbtype.LocalTime`, `'T'` → `dbtype.Time`, `'d'` → `dbtype.LocalDateTime`,
  `'I'` and `'i'` → `time.Time`, `'E'` → `dbtype.Duration`, `'X'` →
  `dbtype.Point2D`, `'Y'` → `dbtype.Point3D`, a packed byte array → `[]uint8`.
  v6 only: `'V'` → `dbtype.Vector[float64]`, and the `0xE0` UUID →
  `dbtype.UUID` — that last one is not a struct tag, so it reaches its answer
  through a different branch of `value` than every other row here. The two
  controls answered: an integer bare against a float nested reported the pair
  unequal, and the same value stored under a key the reader does not ask for
  reported the member absent.

  `useUtc: true` means `'F'` and `'f'`, the pre-UTC datetime spellings, were not
  packed — `value` refuses them outright in that mode. The structural argument
  above covers them; a row does not.

  The two datetime tags are worth naming on their own, twice over. A zoned
  datetime member is the one kind here whose carrier is not a driver alias — it
  arrives as bare `time.Time`, so it is indistinguishable by type from any other
  `time.Time` a member might hold. And `'i'` is the one kind whose carrier is
  not decided by the wire bytes alone: it carries a zone NAME, and
  `utcDateTimeNamedZone` answers a name `time.LoadLocation` cannot resolve with
  `*dbtype.InvalidValue` instead. Both outcomes were measured, and the second
  one by accident: `Asia/Yerevan` gives `time.Time` on both majors, and the
  probe read `*dbtype.InvalidValue` on both majors while it had the zone
  misspelt as `Europe/Yerevan`. So a caller type-switching on `time.Time` has a
  row it can miss for a reason no other kind here has.

  Whether a correctly spelt name can also miss — on a host whose zone database
  does not carry it — is the reading of `time.LoadLocation` and is NOT measured
  here. `ZONEINFO` pointed at an empty directory does not falsify it: the
  resolution fell through to the system database and answered `time.Time`
  anyway, so this host cannot produce the absence.

  AGE has no answer to compare for any of those kinds, and the reason is
  structural on that side too: `agtypeValue`'s switch can return `string`,
  `[]any`, `map[string]any`, a nil `any`, `bool`, `int64` and `float64`, and
  nothing else. Run against the shipped `schema_any_property` helper it
  reproduces the seven rows above, answers `"\\x0102"` and `"2026-09-01"` with
  `string` because they are strings, and refuses `zzz`. So each of these kinds
  is a value the AGE path does not carry, the same shape as the out-of-range
  integer, rather than one the two backends decode differently.

  What that does not witness, beyond the limits the previous bullet already
  states. It does not witness that a server places any of these kinds inside a
  map — only what the decoder does when one arrives. And on the AGE side the
  claim that no temporal can arrive at all rests on the `ag_catalog` sweep
  recorded at `age.typeMap.Temporal`, which is provenance from spike
  `gqlc-35yu.5` against AGE 1.7.0 and was not re-run here.

- Only the AGE generator wraps `ErrUnrepresentableTemporal` (and
  `ErrUnrepresentableWidth`) with its own name. Neither neo4j backend does, so a
  multi-target run that failed on a neo4j table would not say which target
  failed. Inert today — neo4j refuses no temporal kind and no width the shared
  phases reach — and pre-existing for the width sentinel. The sentinel's doc
  states only what holds.

- `gqlc-yr3n` records the separate gap that AGE's served `Scalar` arms have no
  corpus reach: they are checked by a hand-written mirror of the table rather
  than by a golden diff.
