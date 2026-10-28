# VECTOR is a plain Go slice, carried on neo4j-go-v6 alone

A schema declaring `embedding :: VECTOR<FLOAT32>(1024) NOT NULL` now generates
a field of type `[]float32` on `neo4j-go-v6`, and is refused on `neo4j-go-v5`
and on Apache AGE, each under a sentinel that says why. The dimension is parsed
and dropped. At the driver boundary the slice becomes the driver's
`dbtype.Vector[float32]` and back, in an emitted file the public surface does
not name.

Written 2026-10-27 on the owner's ruling, executing bead `gqlc-do1` (GitHub
#299), stage 2 of that bead; stage 1 was UUID (`gqlc-eg4b`, ADR 0047).

## The ruling

The bead was parked on one question: `VECTOR<FLOAT32>(1024)` carries an element
type and a dimension, and the flat `graph.PropertyType` enum held neither. The
owner answered it on 2026-10-27:

> let's build it. what is the (1024) in the vector<float32>(1024) though? we
> can keep it as a go slice though. dont want to play around with static arrays
> in golang here. that would be dumb

and, on the targets that lack the type, that VECTOR is refused on Apache AGE
and on neo4j v5 for now, not stored as a list.

The structure question did not need answering in the form it was parked in.
`graph.PropertyType` had already grown parameterised encodings — `ListOf`,
`RecordOf`, `UnionOf`, ADR 0039 — so a vector is one more of those, carrying its
element, and the dimension is a magnitude qualifier of the kind ADR 0002 already
drops.

## Decision

1. **The spelling is Cypher 25's**, `VECTOR<element>(dimension)`, both parameters
   mandatory, as Neo4j's own property type constraints spell it. ISO/IEC 39075
   has no vector value type — the vendored production list has no `VECTOR` — so
   this is a gqlc extension to `GQL.g4`, the second after UUID, added under
   `predefinedType` as its new last alternative. `VECTOR` becomes a reserved
   word, and the delimited identifier is the escape hatch for a name spelled
   `vector` (`TestPropertyVectorReservedWordEscapeHatch`).

2. **The width is `graph.VectorOf(element)`**, encoded `VECTOR<ELEMENT>`, with a
   new `KindVector`. The element is resolved through the ordinary spelling table
   and must be one of the six coordinate types Cypher 25 defines, which are also
   exactly the six `dbtype.VectorElement` admits: `INT8`, `INT16`, `INT32`,
   `INT64`, `FLOAT32`, `FLOAT64`. `INT` folds to `INT64` and `FLOAT` to
   `FLOAT64`, because Cypher defines those coordinates at those widths; without
   the fold `VECTOR<INT>` and `VECTOR<INT64>` would be two strings that never
   unify, and the first would carry Go's platform-sized `int`, which no driver
   packs as a coordinate. Any other element — `STRING`, `UINT8`, `FLOAT16`, a
   list — is refused at schema admission under the new
   `gql.ErrVectorElementType`, which lists the admitted set. So is a `NOT NULL`
   on the element: a coordinate is never null, and Cypher's spelling does not
   admit the qualifier.

3. **The dimension is dropped**, like a string's length (ADR 0002). It never
   reaches the width, so it cannot reach the Go type, and `VECTOR<FLOAT32>(3)`
   and `VECTOR<FLOAT32>(4096)` are one type. gqlc does not check a vector's
   length against it; the server does, where it enforces dimensions at all.

4. **The Go type is a plain slice**, `[]T` of the element's own carrier —
   `[]float32`, `[]int8` — and never a fixed-size array. A nullable vector is
   `*[]T`, the whole-value star every nullable position takes.

5. **The driver's type stays off the public surface** (ADR 0033). The emitted
   `vector_neo4j.go` holds three unexported generic helpers: `toVector[T]`
   asserts the driver's `dbtype.Vector[T]` and returns its `Elems`, failing the
   read on any other shape — including a LIST another writer stored under the
   same key; `fromVector[T]` wraps a slice as a `dbtype.Vector[T]`; and
   `fromVectorPtr[T]` maps a nil pointer to the Cypher null. Each is emitted
   only when a call site needs it. Generic rather than per element, so the file
   does not grow with the elements a schema declares. They take the value as
   `any` rather than calling `neo4j.GetProperty` or `neo4j.GetRecordValue`,
   because those are constrained to an explicit union of instantiations and
   `dbtype.Vector[T]` over a type parameter is not one of its members.

   A vector is told apart from a list by its WIDTH at every render site, never
   by its Go type: `VECTOR<FLOAT32>` and `LIST<FLOAT32 NOT NULL>` both emit
   `[]float32`. Bound bare, the slice would reach the server as a LIST.

6. **neo4j-go-v6 carries it; neo4j-go-v5 refuses it under
   `neo4j.ErrUnrepresentableOnDriverVersion`.** `dbtype.Vector` exists in
   neo4j-go-driver v6 and has no counterpart in v5.28.4, the two versions
   `test/data/codegen/go.mod` pins. The sentinel is the one PR #2898 introduced
   for UUID and ADR 0047 removed when UUID moved to a string wire both majors
   carry; it returns with the same reasoning. `codegen.ErrUnrepresentableWidth`
   would tell the author no carrier exists in this backend, which is false; the
   repair is a different driver major. The neo4j type table learns which major
   it answers for again (`typeMap.vectors`), zero-valued at the refusing answer.

7. **Apache AGE refuses it**, under `codegen.ErrUnrepresentableWidth`, naming
   itself, with the owner's reason in the message. AGE CAN store lists — its
   type table admits `LIST` at any depth, with live tests — so a vector's
   coordinates would round-trip as an agtype list. But AGE has no vector type
   and no vector functions: no similarity search and no dimension enforcement.
   Stored as a list, the value would lose the query semantics `VECTOR`
   declares, and one schema declaration would mean a vector on one backend and
   a plain list on another.

8. **A vector inside a container is refused everywhere.** As a list element, a
   record field or a union member, neo4j refuses it on both majors (its type
   table's `contained`) and AGE refuses it as it refuses every vector, so the
   refusal carries no backend name (ADR 0035's attribution rule; AGE's
   `nestsAVector`). A neo4j property array is homogeneous in a scalar storage
   type and a map is no property at all, and no list walk in that backend
   decodes a vector element; the slice carrier would otherwise ride the walk
   for `LIST<T>` and fail on every vector at run time. The query-value case —
   `collect(d.embedding)` on neo4j-go-v6, where the element is a vector the
   target carries bare — is refused by the shared front end at the list-element
   plan, naming the list width.

9. **The resolver is unchanged.** A vector is not orderable for `min`/`max`: it
   falls to the allow-list's default arm, as every composite width does.

## Measured

Against the image this repository pins (`neo4j:5-community` by digest, Neo4j
5.26.28), a v6.2.0 driver negotiated **Bolt 5.8**, packed a `dbtype.Vector`
without complaint — unlike `dbtype.UUID`, it has no client-side protocol gate —
and the server refused the request:

    Neo.ClientError.Request.Invalid (Illegal value for field "params":
    Unexpected struct tag: 0x56)

A newer community image is not enough. `neo4j:2025.10-community` (2025.10.1)
negotiated **Bolt 6.0** and refused to store the property:

    Neo.ClientError.Data.DataUnsupportedByStoreFormat (storing properties of
    type vector is not supported in aligned store format)

and that edition will not start a database in the `block` format a vector
property needs ("No supported database format 'block'. Available formats are:
'aligned', ...").

Against `neo4j:2025.10-enterprise` under its evaluation licence, in a scratch
copy of the codegen module with only the image constant and the licence
variable changed, `TestNeo4jStoresAndRoundTripsAVector` ran **6 rows, 6 PASS**:
a vector written through the generated `WriteDocument` is stored as a
`Vector[float32]` and a `Vector[int8]`, read through a raw session; a nil
nullable vector binds null; the entity decode, the column decode and the `:one`
column decode read both back; a vector parameter matches the node holding it;
and a LIST stored under the same key fails the read. One mutation, victims
declared first: binding the embedding bare instead of through `fromVector`
failed the storage row and the three read rows that decode it, and left the
null row and the stored-list row green, as predicted.

## Consequences

- **The live arm skips in CI.** `TestNeo4jStoresAndRoundTripsAVector` is in the
  `test-codegen-live-neo4j` run list and skips three ways, each naming its
  reason: on the pinned digest, before booting a container; on a server
  negotiating Bolt below 6; and on a community edition. It runs unedited once
  the pin moves to an image that can store a vector, which is now known to mean
  an enterprise edition of 2025.10 or later — an owner call twice over, the pin
  and a licence, with every live arm in its blast radius. Until then the v6
  golden is compiled and asserted byte-for-byte without a server round trip.

- **`ErrUnrepresentableOnDriverVersion` is a published neo4j sentinel again**,
  witnessed by `test/data/codegen/invalid/vector_width_driver_version`, and the
  registry's two neo4j entries publish it.

- **`VECTOR` is reserved** in schema source. A schema using `vector` as a bare
  property, label or variable name stops parsing.

- **A slice-carried `:one` column zeroes as `nil`.** `goTypeZeroText` answered
  the numeric `0` for any type it did not name, which does not compile against
  a `[]float32` return.

## Alternatives declined

- **A fixed-size Go array**, `[1024]float32`. Declined by the owner. It would
  also have made the dimension part of the type, which every other magnitude
  qualifier is not.

- **Storing VECTOR as a LIST on neo4j-go-v5 and AGE.** Declined by the owner,
  for decision 7's reason. On v5 it would have had a second cost: the property
  would be a list on the server, so a v6 reader of the same graph would refuse
  it.

- **`dbtype.Vector[T]` on the public surface.** Leaves a driver type in every
  struct an author writes against, which ADR 0033 refuses, and ties the field's
  type to one driver major.

- **Carrying the dimension in the width** (`VECTOR<FLOAT32,1024>`). Nothing
  emitted would read it, and it would make two widths of one element unequal,
  so a query unifying them across labels would refuse.
