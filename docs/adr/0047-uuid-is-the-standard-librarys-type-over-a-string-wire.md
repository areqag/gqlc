# UUID is the standard library's `uuid.UUID`, carried as a string

A schema declaring `ref :: UUID NOT NULL` now generates a field of the Go
standard library's `uuid.UUID`, on **both** neo4j driver majors, and the value
is stored as its RFC 9562 text — a STRING to the driver and to the server. No
driver type is involved in either direction.

Apache AGE adopted the same carrier on 2026-09-19 (bead `gqlc-ytf9`); see
"Apache AGE" below. With it the width is carried by all three enrolled targets.

**This is a breaking change to already-generated code** for anyone who
generated against `neo4j-go-v6` between PR #2898 and this one. The emitted
`UUID` was a gqlc-owned `type UUID [16]byte`; it is now
`type UUID = uuid.UUID`. The two share an underlying type, so a caller's
`UUID(v)` conversion still compiles, but the value on the wire moved from a
Bolt 6.1 UUID structure to a string, and a graph written under the old
emission is not readable under the new one. Nothing could have been written
under the old one against the image this repository pins, which is the defect
the next section describes.

Written 2026-09-19 on the owner's direction, executing bead `gqlc-ybk2`
(GitHub #2900).

## What was wrong

PR #2898 (stage 2 of `gqlc-eg4b`) carried the width on the driver's own
`dbtype.UUID`, behind a neutral `[16]byte` carrier, and asserted the EMISSION
only. `gqlc-ybk2` was filed the same day to measure the two claims that left
open — that a UUID round-trips, and that one can be stored in a property slot
at all — and was blocked within three days, on a measurement rather than on a
guess:

    neo4j-go-driver/v6@v6.2.0/neo4j/internal/bolt/hydrator.go:531
      "received UUID packstream type 0xE0 on Bolt protocol prior to 6.1"
    neo4j-go-driver/v6@v6.2.0/neo4j/internal/bolt/outgoing.go:622
      Reason: "requires at least Bolt 6.1"

`dbtype.UUID` is a Bolt 6.1 value, the refusal is in the CLIENT and fires
before a byte reaches the wire, and the pinned image is a `neo4j:5` line that
does not offer Bolt 6.x. So the width as carried could not be written or read
against the server every other live arm runs on, and no arm could be added
that measured the server rather than the driver's gate.

That tied three things together that a schema author should not have to
hold at once: a property width, a driver major (`neo4j-go-v5` had no carrier
and refused the width under its own sentinel,
`neo4j.ErrUnrepresentableOnDriverVersion`), and a server protocol line.

## Decision

1. **The Go type is the standard library's.** `uuid.go` declares
   `type UUID = uuid.UUID`. The `uuid` package ships in Go 1.27, which is why
   `gqlc-9thx` moved this repository's `go` directive first. A caller hands
   `uuid.NewV7()` to a generated method with no conversion, and gets `String`,
   `Compare`, `MarshalText` and `Parse` from the library rather than from a
   second implementation gqlc would emit into every package.

2. **An alias, spelled unqualified everywhere else.** Every other emitted file
   writes `UUID`, not `uuid.UUID`, so the `"uuid"` import lives in `uuid.go`
   and the conversions file alone and none of the per-file import walks has a
   package to account for. The alias costs nothing at a call site: it IS
   the library type, not a type convertible to it.

3. **The wire form is the RFC 9562 text**, lowercase and hyphenated — what
   `uuid.UUID.String` returns. This is the form Cypher's own `randomUUID()`
   produces, it is a string property like any other to a constraint or an
   index, and both driver majors pack it unaided. It is not a byte array:
   every other writer to the same graph would have to know that convention,
   and nothing Cypher produces follows it.

4. **Every decode is checked.** A property slot holds whatever string a writer
   put there, so the emitted `toUUID` parses and can fail, and each decode
   site emits the error arm the checked numeric narrowings emit (ADR 0037). A
   stored string that is not a UUID fails the read, naming the value.

5. **Both neo4j majors carry the width**, identically. With no driver type
   involved there is nothing for them to disagree about, so the per-major
   type-table field, `neo4j.ErrUnrepresentableOnDriverVersion`, the refusal
   that raised it, the `neo4j.Sentinels` map that published it and the
   `uuid_width_driver_version` fixture that witnessed it are all removed
   rather than left reachable by nothing.

6. **Version 7 is a recommendation, not a constraint.** The width does not
   record a version and the carrier accepts any. What version 7 buys is
   measured below: its text form sorts in creation order, so the stored
   property does too.

### Why a string carrier is not the widening spec §5.1 refuses

`age/types.go` declines a string carrier for UUID on the ground that it
"would round-trip the SPELLING and drop the declared type, which is the
silent widening §5.1 exists to refuse". That objection is to an UNCHECKED
string: a `string` field over a `UUID` declaration, where any text reads
back. Decision 4 is the answer to it. The declared type is on the Go surface
as `uuid.UUID`, not `string`, and a value that is not one does not arrive.

When this was first written Apache AGE still refused the width on exactly
that ground, and whether it should follow was left to bead `gqlc-ytf9`. The
owner ruled that it should; the next section is that change.

## Apache AGE

AGE carries the width the way it already carries `DATE`: in the agtype string
scalar, read back through a decoder that parses. `propertyCarriers` answers
`"UUID"`, `wireFamily` files it under `string`, and the emitted `agtypeUUID`
reads the text through `agtypeString` and then `uuid.Parse`, failing the read
on text that is not a UUID. `uuid.go` is the same file the neo4j targets emit.

**There is no encoder.** This backend binds parameters through
`encoding/json`, and `uuid.UUID` implements `encoding.TextMarshaler`, so it
marshals as the lowercase hyphenated text `agtypeUUID` reads. Probed before
relying on it, and then measured live: bare, behind a pointer, as a list
element, as a nil pointer (`null`), as a nil list element (`null`), and as the
dynamic value inside the `any` a union carries. The cost of that economy is
that the write form rests on the `uuid` package's marshalling rather than on
text gqlc emits; the live storage row is what would notice it change.

`TestAGEStoresAndRoundTripsAUUID` runs the neo4j half's rows against the
pinned AGE image — 8 rows, 8 PASS on 2026-09-19 — with two differences that
are the store's. A **nil list element is stored and read back**, which neo4j's
property arrays cannot hold, so `agtypeNullableElem`'s nil arm is reached by a
value that came from the server. And a bound list **holding a null still
matches** the stored list under `=`; that was written into the test as the
opposite belief first and corrected by a probe, so it is recorded here as
measured rather than assumed.

Three mutations in a scratch copy, victims declared first: `agtypeUUID`
swallowing its parse error failed the not-a-UUID row; a nullable list element
decoded as the zero UUID instead of nil failed the read-back row; `OpenAccount`
binding `ref` upper-cased failed the storage row and — not predicted — the
parameter row, since the mutant stores upper case while the parameter stays
lower. The unmutated control ran clean.

What differs from neo4j for an author:

- `UNION<UUID|STRING>` is refused here too, and so is **`UNION<UUID|DATE>`**,
  which neo4j admits: on AGE a DATE is ISO text, so both members are the
  string family.
- Which sentinel refuses a colliding union on AGE depends on the POSITION the
  query reaches it through. A whole-entity read is refused in the entity
  sweep under `ErrUnrepresentableWidth`, as on neo4j, and
  `invalid/uuid_union_string_collision` reads the entity so that one manifest
  holds all three targets. A column projection of the same property is
  answered first by AGE's own check that it can serve each column, as
  "unsupported query"; the fixture projected the column at first and its AGE
  arm went red on exactly that.
- `invalid/uuid_width_unrepresentable`, the fixture that held AGE's refusal
  end to end, is removed with the refusal.

## Measured

`TestNeo4jStoresAndRoundTripsAUUID` runs against the pinned image, on both
driver majors off one container, with version 7 values throughout. 16 rows,
16 PASS on 2026-09-19. The storage observations are made through a raw driver
session rather than through generated code, so an emission cannot be wrong in
agreement with itself:

- a UUID written through the generated `OpenAccount` is **stored**, in a
  property slot, as the canonical lowercase text — bare, nullable, in a list,
  and as a union member;
- it **reads back equal** through the entity decode, the column decode and the
  `:one` column decode;
- a nil `*UUID` binds the Cypher null, not the text of the zero UUID;
- a UUID parameter matches the node holding it and no other, through each of
  the four bind helpers;
- a stored `'not-a-uuid'` fails the read, and the error says
  `is not a UUID`;
- five version 7 UUIDs, inserted out of order under ids that agree with
  neither the insertion nor the mint order, come back in **mint order** under
  `ORDER BY a.ref`.

Three mutations of the emitted helpers were run in a scratch copy, each
against the v6 golden alone, to confirm the rows can fail. `fromUUID`
upper-casing its result failed the storage row and the uppercase row.
`fromUUIDPtr` binding the zero UUID for nil failed the null row and the INT64
union row, which also binds a nil `prior`. `toUUID` swallowing its parse error
failed the not-a-UUID row. The v5 arm stayed green under all three and the
unmutated control ran clean. One prediction was wrong and is recorded as such:
the upper-casing mutant was expected to fail the parameter-match row and did
not, because it upper-cases the write and the parameter alike.

## Consequences

- **`UNION<UUID|STRING>` is refused on neo4j.** A closed union's members must
  be pairwise distinct on the wire (spec §4) and a UUID now arrives as a
  string. Under `dbtype.UUID` the declaration generated. The refusal is
  witnessed by `test/data/codegen/invalid/uuid_union_string_collision`;
  `UNION<UUID|INT64>` still generates and is what
  `test/data/codegen/valid/uuid_property` declares.

- **Matching is exact and the read is lenient**, which is a limit and is
  measured as one. `uuid.Parse` accepts upper case, braces, the URN form and
  the undashed form, so a UUID another writer stored in one of those READS
  BACK; the server compares strings, so a parameter — always rendered
  canonical — does not MATCH it. Writers outside gqlc must store the canonical
  lowercase form to be findable.

- **Generated code that declares a UUID property requires Go 1.27** from its
  consumer. Code that declares none is unaffected: `uuid.go` is emitted only
  when the surface names the carrier.

- **`UUID` is a reserved identifier declared by every target** now, where it
  was declared by the neo4j targets alone while AGE refused the width
  (`docs/specs/codegen-sentinel-taxonomy.md` §6).

- **The driver's `dbtype.UUID` is never asserted by generated code.** It stays
  in `driverScalarCarriers` as an unwitnessed row, so a driver type returning
  to the emission is a red test rather than a quiet one.

- **The resolver still rules UUID not orderable** for `min`/`max`, on a ground
  this change does not touch. The ordering measurement above is evidence for
  that ruling, not the ruling: bead `gqlc-fdfk`.

- **No server upgrade is needed**, which is what unblocked `gqlc-ybk2`. Moving
  the pinned image to a Bolt 6.1 line remains an owner call with every live
  arm in its blast radius, and nothing about UUID now waits on it.

## Alternatives declined

- **Keep `dbtype.UUID` and wait for a Bolt 6.1 image.** Leaves the width
  unwritable against the server this repository tests on, v6-only, and makes a
  property width a function of a server protocol line.

- **`github.com/google/uuid`.** Same `[16]byte` shape and a `NewV7` of its
  own, and it works on Go 1.26. Declined because the standard library now has
  the type: a third-party module in every generated package's dependency graph
  is a cost the alias does not pay.

- **Emit `uuid.UUID` qualified at every site instead of an alias.** Reads the
  same to a caller and threads a third import flag through every per-file
  import walk, which today return two. The alias gets the identical type for one import in
  one file.

- **Keep the gqlc-owned `[16]byte` and change only the wire form.** Would have
  needed an emitted `String` and an emitted parser — a second RFC 9562 §4
  implementation in every generated package, which PR #2898's own carrier
  comment declined for the same reason.
