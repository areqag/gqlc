# Design — the Go carriers for RECORD and closed dynamic union types

The design ruling for bead `gqlc-x9tg7`, answering what Go type a
declared `RECORD<city STRING, zip INT32>` or `UNION<BOOL | DATE>`
property emits, where that answer lives, which positions it binds, and
what refusal remains for anything left unbuilt. The premise is the
merged PR #2308 (`gqlc-h9n.33`, ADR 0039): the schema front end
resolves both shapes into a parameterised `graph.PropertyType` whose
canonical string encoding makes `==` mean type identity, and
`internal/codegen` refuses both kinds wholesale through
`ErrUnimplementedTypeKind`, asked before any type map sees the value.
This document rules on the emission that retires that refusal.
Execution is staged into two warrior beads — records first
(`gqlc-jffyz`), closed unions second — whose implementation-ready
plans live in the beads' own notes and cite the sections here.

Precedent extended: ADR 0035's two-axis split (a *carrier* question —
can this backend spell a Go type for the value — versus a *storage*
question — will this backend's server keep it as a property), and its
rule that the storage axis binds stored properties alone. Precedent
bent: ADR 0039's deliberate non-descent — the unimplemented-kind walk
does not recurse through `Fields()` or `Members()` because nothing
below an always-refused node is reachable. Stage 1 makes record fields
reachable, so the walk gains the `Fields()` descent ADR 0039 explicitly
deferred; ADR 0039 itself named this the expected reversal.

---

## 1. The shape of the problem

A record is a product: a fixed set of named, typed fields. A closed
union is a sum: a value that is exactly one of an enumerated set of
member types. Go has a natural spelling for the first (a struct) and
no natural spelling for the second (no sum types), which is why the
two look similar in the schema grammar and could not be more different
as emission problems.

Three constraints frame every ruling below:

1. **`TypeMap.Property` is a pure function of the resolved type**
   (`internal/codegen/typemap.go`). The carrier text must be derivable
   from the `PropertyType` alone — no schema context, no site name, no
   registry threaded through. Two spellings of one canonical record
   (`RECORD<zip INT32, city STRING>` and `RECORD<city STRING, zip
   INT32>` encode identically; propertytype.go sorts fields by name)
   reach `Property` as one string and MUST get one Go type.

2. **The declared surface does not vary by backend.** Every enrolled
   backend that admits a width spells the same caller-facing Go type
   for it (`TestBackendInvariantSurface`; the AGE temporal carriers
   exist for exactly this reason, ADR 0033). A record admitted on both
   backends must be the same struct on both.

3. **Refusal stays loud and stays first.** ADR 0039's ordering rule —
   the unimplemented-kind question is asked before the type-map
   question at all four preparation sites (prepare.go:614, :717, :764,
   :1331) — must survive every intermediate state. At no commit may a
   record or union reach a type map that has no arm for it, and at no
   commit may generation exit 0 having emitted a field no decoder can
   fill.

## 2. Ruling: a declared record emits an anonymous canonical struct

`RECORD<city STRING, zip INT32>` on a non-nullable property emits the
carrier text:

```go
struct {
	City *string
	Zip  *int32
}
```

- **Anonymous, not named.** Go's structural identity for anonymous
  struct types delivers constraint 1's "two spellings, one struct"
  for free: identical canonical encodings produce identical field
  lists in identical order, and identical anonymous struct texts are
  the *same type* to the Go compiler. No registry, no name mangle, no
  collision with entity names — `Property` stays pure. A named struct
  per record was rejected on both available naming schemes: a
  content-mangled name (`RecordCityStringZipInt32`) explodes in length
  and reintroduces mangle collisions for zero caller value, and a
  site-derived name (`PlaceAddr`) is not derivable from the type
  alone, so it breaks `Property`'s purity and renames the caller's
  type when a schema edit moves the property — an API break caused by
  a rename, the instability ADR 0033's neutral carriers exist to
  avoid.

- **Field order is the canonical (name-sorted) order** `Fields()`
  returns. This is what makes the struct text a function of the
  encoding.

- **Field names go through the `paramFieldName` mangle**
  (names.go:42), the same walk entity properties use, for the same
  reason: `city` → `City`, `zip_code` → `ZipCode`. Two record fields
  that mangle to one Go name are refused with a new sentinel,
  `ErrRecordFieldCollision`, mirroring `ErrPropertyFieldCollision`
  (prepare.go:610) — same shape, naming the record encoding and both
  source field names. A field whose mangle is empty (a name of
  underscores alone) is refused unconditionally under the same
  sentinel family: unlike the single-parameter form that keeps `$_`
  served (prepare.go:959), a record field is *always* spelled as a
  struct field, so there is no position where the empty mangle is
  harmless.

- **A field without `NOT NULL` is a pointer.** GQL record fields are
  nullable by default, exactly like properties, and the emission uses
  the same spelling nullable properties use (a leading `*` on the
  carrier). A `NOT NULL` field drops the pointer.

- **Field carriers come from the same backend `Property` recursion
  lists use** (age/types.go:102, neo4j/types.go:31). A record field of
  a width the backend refuses makes the whole record unrepresentable
  there — `RECORD<img BYTES>` is refused on AGE because BYTES is, and
  the refusal routes through the existing `ErrUnrepresentableWidth`
  channel naming the position. A zoned temporal field (TIME,
  TIMESTAMP) is refused *inside a record* on AGE for the same reason
  the list rule refuses it (age/types.go:104): the offset rides a
  sidecar named after the property, and a container gives its
  contents no property names of their own to hang a sidecar on. The
  `carriesZone` predicate is the one answer to which widths those
  are; the rule generalises from "list element" to "any container
  position" and stage 1 rewords its comment accordingly.

### 2.1 The severable ergonomics layer: site-named aliases

An anonymous struct is the correct *type*; it is a poor thing to make
a caller type out. So for each record-typed **entity property**, the
backend's `models.go` additionally emits a site-named type **alias**:

```go
// PlaceAddr is the record type of Place property addr.
type PlaceAddr = struct {
	City *string
	Zip  *int32
}
```

An alias (`=`), never a defined type: aliases preserve type identity,
so the site-named spelling and the anonymous spelling used in Row and
Params structs remain assignable with no conversion, and two
properties declaring the same record share one underlying type as
constraint 1 demands. The alias name derives `<Entity><Field>` and
enrols in `sweepIdentifiers` (prepare.go:1212) as a new source, so a
collision with an entity name, a decode helper, or any other emitted
identifier is refused deterministically at generate time — the
sweep's whole purpose.

This layer is **severable**: the emission is correct and complete
without it, because the alias adds no type the anonymous spelling
does not already denote. The stage-1 plan orders it last and a
Ռազմիկ who finds it fighting the identifier sweep may land the
carrier without it and file the alias as a follow-up bead — the
design is not hostage to its ergonomics.

## 3. Ruling: `RECORD<ANY>` and `RECORD<>` are two types, two carriers

- **`RECORD<ANY>`** (`graph.TypeAnyRecord` — fields *undeclared*)
  emits `map[string]any`. The symmetry is exact and already
  established: `ANY` → `any` (ADR 0020), `LIST<ANY>` → `[]any`, so
  the record whose fields are unconstrained maps to Go's
  unconstrained string-keyed product. `Kind()` classifies
  `RECORD<ANY>` as `KindRecord` (propertytype.go:46), so the new
  `KindRecord` guard intercepts it before the switch and the existing
  `case graph.TypeAnyRecord` arms (age/types.go:156,
  neo4j/types.go:85) stay unreachable — stage 1 rewrites them to
  answer `"map[string]any"` so they keep agreeing with the guard that
  does the work, the exact arrangement `graph.TypeList` already has
  (age/types.go:141). The owner's principle applies: it is valid
  ISO GQL, so gqlc allows it, subject only to each backend's storage
  answer (§5).

- **`RECORD<>`** (fields *declared and empty*) emits `struct{}`. A
  record with no fields is a unit type, and Go spells the unit type
  `struct{}`. Collapsing it onto `map[string]any` would erase a
  distinction the resolver went out of its way to keep (ADR 0039
  records the two encodings as deliberately distinct); emitting the
  unit keeps the carrier honest about what the author declared —
  there is nothing in this value, and the Go type says so.

## 4. Ruling: a closed union emits `any` plus generated member-set
## validation, and is admitted only where its members are wire-distinct

`UNION<BOOL | DATE>` emits carrier text `any` — the same text ADR
0020 chose for the open union, because Go has no sum types and every
alternative manufactures one badly. The wrapper-interface scheme (a
generated `isXMember()` marker interface with one wrapper struct per
member) was rejected for its naming explosion and its ergonomics:
scalars arrive wrapped, so every caller unwraps at every use, and the
wrapper names have all the collision problems of §2's rejected named
structs. The struct-of-pointers scheme (one optional field per
member) was rejected because it spells invalid states — zero or two
members set — as representable values, and because it has the same
member-naming problem with no principled answer for `UNION<INT |
LIST<INT>>`.

What distinguishes the *closed* union from ADR 0020's open one is
that the member list is not decorative. It buys two things:

1. **Bind-time validation.** The emitted encode path type-switches
   the `any` value against the member carriers and refuses, with a
   run-time error naming the declared union and the offered Go type,
   anything outside the set. An open union accepts everything; a
   closed union enforces its declaration at the boundary.

2. **Decode dispatch.** The emitted decode path switches on the
   *wire shape* of the incoming value and narrows it to the member
   that shape belongs to — an INT32 member comes back `int32`, not
   the driver's widened `int64`.

Point 2 is only possible when the members are distinguishable on the
wire, which yields the admission rule:

> **A closed union is emittable on a backend iff its members map to
> pairwise-distinct wire families there.** Otherwise `Property`
> returns `("", false)` and the refusal routes through
> `ErrUnrepresentableWidth`, naming the union and the colliding pair.

A *wire family* is the equivalence class of declared widths that
arrive as one indistinguishable shape from a given backend's driver:

- **neo4j**: every integer width widens to `int64` and every float
  to `float64` (`driverCarrier`, neo4j/types.go:198-205); all lists
  arrive `[]any`; maps arrive `map[string]any`; the temporal kinds
  each arrive as a distinct `dbtype` type.
- **AGE**: agtype's scalar vocabulary is null / boolean / integer /
  float / string / list / map. DATE encodes as ISO text
  (age/types.go:58-69), so it shares the *string* family with
  STRING; TIMESTAMP, LOCAL TIME and DURATION all ride the integer
  scalar as microsecond counts (age/types.go:36-92), so they share
  the *integer* family with every INT and UINT width and with each
  other.

Consequences, which the stage-2 tests pin per backend:

- `UNION<INT32 | INT64>` is refused on **both** backends (one
  integer family everywhere).
- `UNION<DATE | STRING>` is emittable on neo4j and refused on AGE —
  a contingent refusal, so AGE's refusal text names the backend per
  ADR 0035's attribution rule, and
  `TestAContingentRefusalNamesItsBackend` extends its width
  vocabulary to cover union encodings.
- At most one list-family and one map-family member per union, per
  backend.
- A zoned temporal member is refused inside a union on AGE — the
  union is a container position and §2's generalised `carriesZone`
  rule applies; on neo4j the member is fine (`dbtype.OffsetTime` is
  its own wire family and carries its own zone).
- A member `NOT NULL` has no codegen effect. Nullability of the
  *value* is the property's own nullability, spelled the way `any`
  spells absence (nil); a per-member `NOT NULL` constrains what the
  schema admits, which the resolver owns, not what the carrier
  spells. Recorded here as a gqlc decision so nobody hunts for the
  missing emission.

The `any` carrier means constraint 2 (backend-invariant surface) is
satisfied trivially; the *validation sets* differ per backend only in
which unions exist at all, never in what an admitted union accepts.

## 5. Ruling: placement — carrier per backend, storage per backend,
## helpers per backend, derivation helpers shared

- **Carrier derivation lives in each backend's `Property`** — the
  existing recursion (the list arm is the template) gains a
  `KindRecord` and, at stage 2, a `KindUnion` arm. This placement is
  forced, not chosen: field and member carriers must inherit each
  backend's own refusals (BYTES on AGE, zoned-in-container on AGE),
  and the wire-family relation of §4 is a per-backend fact. What is
  *shared* in `internal/codegen` is the pure text derivation — the
  canonical-order struct-text builder and the record-field mangle
  walk — so the two backends cannot drift on the shape of the struct
  they both emit. One shared function, two callers, mirroring how
  `paramFieldName` already serves both.

- **Storage stays a separate axis** (`StorableProperty`), exactly per
  ADR 0035. AGE's `StorableProperty` remains `return true` — agtype
  is JSON-shaped and nests without limit, and the stage-1 live test
  measures a record property round-trip against the pinned image the
  way `TestAGEStoresANestedListProperty` measures the nested list.
  neo4j's `StorableProperty` returns false for `KindRecord`,
  `TypeAnyRecord`, and any union with a map-family or record member,
  because the server refuses map-valued properties — **a premise this
  repository has never measured**: no map-property live test exists
  under `test/data/codegen` today. Stage 1 therefore *demands the
  measurement* (§7) rather than asserting the folklore, with a named
  fork: if the pinned server in fact stores a map property, the
  refusal is wrong, the `StorableProperty` arm is not written, and
  the divergence ADR mirrors 0035 with the backends' roles reversed
  from expectation.

- **Encode/decode helpers are emitted per backend in `models.go`**,
  one per distinct record or union encoding reachable from the
  schema and queries, collected the way temporal helper uses already
  are. Helper names derive from a deterministic short hash of the
  canonical encoding, with a doc comment spelling the full encoding —
  the content-named `agtypeListOf<Type>` helpers are the precedent,
  and the hash form is what keeps an arbitrary nesting depth from
  producing an unbounded name. Hash-derived names enrol in the
  identifier sweep like every other emitted name.

## 6. Ruling: which positions — carrier binds all four, storage binds
## stored properties alone

The carrier question binds every position a `PropertyType` reaches:
stored property, query column, query parameter, list element. The
storage question binds stored properties only — prepareEntityFields
asks it (prepare.go:620) and the column and parameter sweeps do not.
This asymmetry is ADR 0035's, inherited deliberately: on neo4j a
record *property* is refused (the store will not keep it) while a
record *column* and a record *parameter* work — the server happily
projects and binds map values it will not store, which is the same
sentence ADR 0035 wrote about nested lists, now with a second kind
making it true.

The prepared surface changes once, in stage 1: `EntityField`,
`Param`, `Row`, and `ListElem` each gain a `Width
graph.PropertyType` field carrying the resolved type through to
render, so the render layer dispatches on `Width.Kind()` instead of
growing a parallel enum. `ColumnKind` keeps `ColumnProperty`; no new
kind is minted, because a record-valued column is still a property
value — what changed is its width, which the new field carries.

**Named limit:** resolver support for field access *into* a record
inside query text (`RETURN p.addr.city`) is out of scope for both
stages. A record value moves whole. The resolver rules on that
access separately if a bead ever asks; nothing in this design blocks
it, and nothing in it is built here.

## 7. Ruling: staging, and the refusal that remains

**Stage 1 — records** (`gqlc-jffyz`, retitled to records only):
`KindRecord` + `TypeAnyRecord` + `RECORD<>` carriers on both
backends, all four positions, aliases layer, helpers, and the walk
change: `unimplementedTypeKind` (prepare.go:559) descends through
`Fields()`, so `RECORD<u UNION<…>>` is still refused — at the union
node, with the union named — while plain records pass. The
`KindUnion` arm of the walk survives stage 1 untouched; ADR 0039's
ordering rule holds at every commit.

Live measurements stage 1 demands, ADR-0035-style (a live test per
claim, premise tripwires where the claim is about the server):

1. neo4j refuses a map-valued stored property (the tripwire under
   the `StorableProperty` refusal; fork condition in §5).
2. AGE stores and round-trips a record property, nested record
   included.
3. neo4j round-trips a record **column** and a record **parameter**
   (the positions storage does not bind).

**Stage 2 — closed unions** (new warrior bead, blocked by
`gqlc-jffyz`): the `KindUnion` carrier per §4, the wire-family
admission rule and its per-backend refusal tests, bind-time
validation and decode dispatch, `LIST<UNION<…>>` storage on neo4j
measured against the server's heterogeneous-array behaviour before
any `StorableProperty` arm is written, and — the point of the stage —
**`ErrUnimplementedTypeKind` is deleted entirely**: the sentinel, the
walk, its registry row, and its taxonomy entry. After stage 2 the
codegen taxonomy contains no "yet": every refusal that remains is
permanent (`ErrUnrepresentableWidth`, `ErrUnstorableProperty`) or
authorial (collisions), which is the terminal state ADR 0039
promised.

Between the stages, `UNION` declarations keep failing loudly through
the walk — no intermediate commit ever emits a compiling package
that lies about a union.

## 8. Falsifiers

Checks that FAIL if this design is wrong, named per the design gate:

- **Structural identity:** a test declaring the same record under two
  field spellings asserts the two prepared `GoType` texts are
  byte-identical — if the canonical order or the shared text builder
  is broken, this fails, not a human reader.
- **Ordering rule:** ADR 0039's existing per-site refusal tests keep
  running at every stage-1 commit; a record reaching a type map
  before the walk would flip a pinned error identity.
- **Wire families:** the stage-2 refusal tests assert
  `UNION<INT32|INT64>` refused on both backends and
  `UNION<DATE|STRING>` refused on AGE *with the backend named* while
  admitted on neo4j — a wrong wire-family table flips one of the
  three cells.
- **Storage premise:** the neo4j map-property tripwire is a live test
  that FAILS if the server accepts the write — the refusal is
  contingent on a measured behaviour, not on this document.
- **Round-trip scope matches prose:** the stage-1 live tests cover
  exactly the positions §6 claims work (columns and parameters on
  neo4j, properties on AGE) — a passing suite that skipped one of
  those would be a scope mismatch a reader of §6 can catch by
  grepping the test names against the table.

## 9. Vocabulary

*Wire family* enters the domain language (CONTEXT.md, same PR as this
spec): the equivalence class of declared widths a backend's driver
delivers as one indistinguishable wire shape. It is the load-bearing
term of §4's admission rule and the first word of the union refusal
messages, so it must mean one thing.
