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
  emitted, gated together on `helpers.value`. Three goldens carry them:
  `schema_any_property`, `schema_any_property_alone` and
  `schema_list_any_property`, whose schemas declare an ANY-width *property* or a
  list of one — not a scalar column. In each of the three, the only mention of
  `agtypeMap` outside its own declaration is `agtypeValue`'s call on the `'{'`
  arm. `agtypeMap` returns `map[string]any`, the same text the
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
  text. Deleting either arm reddens both, at the subtest for the text that lost
  it. So the `map[string]any` arm is held up by that pin and not by this bullet:
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
  nested-map member as `map[string]any` on all three, whose own members follow
  the rows above. So no runtime-type divergence between the backends survives
  for the member kinds in those two objects: integer, float, string, boolean,
  null, list and nested map.

  How that was measured, and what it does not witness. Each driver's own
  hydrator was executed over a `RECORD` packed with that driver's own
  `packstream.Packer`, which is how its `hydrator_test.go` builds a case —
  "same was as server would" — and `neo4j.Record` is an alias for the
  `db.Record` that hydrator fills, so the members measured are the ones a
  caller reads off a record. That is the driver's mapping from packed marker to
  Go type, not a live session: it does not witness which marker a server picks
  for a given Cypher value. Nor was a live run an alternative. The fixture whose
  golden returns a `map[string]any` is `scalar_map` (`RETURN {a: 1} AS m`,
  emitted for `neo4j-go-v5`), and no scenario in the live battery drives it, so
  a green `live-smoke` witnesses nothing about a map member either.

  Two bounds this does not cross. An integer member outside `int64` range
  decodes as `float64` on AGE, where `agtypeInt64` fails and `agtypeValue` falls
  through to the float parse; the driver reads every packed integer marker
  through `Int() int64`, so that is a value the neo4j path does not carry rather
  than one the two decode differently. And a member holding a temporal, a point,
  a byte array, or a v6 vector or UUID was not measured.

  What never depended on the answer is that `TestBackendInvariantSurface`
  compares declarations — it nils each method body and passes over the
  receiver-less decode helpers, which is where this behaviour lives — so it
  would not see a divergence of this shape either way. It also skips a fixture
  enrolled in fewer than two targets, and `scalar_map` is enrolled in one.

- Only the AGE generator wraps `ErrUnrepresentableTemporal` (and
  `ErrUnrepresentableWidth`) with its own name. Neither neo4j backend does, so a
  multi-target run that failed on a neo4j table would not say which target
  failed. Inert today — neo4j refuses no temporal kind and no width the shared
  phases reach — and pre-existing for the width sentinel. The sentinel's doc
  states only what holds.

- `gqlc-yr3n` records the separate gap that AGE's served `Scalar` arms have no
  corpus reach: they are checked by a hand-written mirror of the table rather
  than by a golden diff.
