# Three of the declined value-type families are what the construct is, and two are what gqlc has not built

`PATH`, `RECORD`, `ANY VALUE`, the graph/node/edge/binding-table references and
the immaterial types all report one sentinel, `ErrUnsupportedType`, whose message
is "unsupported property value type". A schema author who hits it cannot tell
whether to change the schema or wait for a release, and the deviation register
carried one justification against six families that do not share one.

They now report five sentinels, each named after the ISO production it declines,
each wrapping `ErrUnsupportedType`. The split that matters is not five ways but
two.

## Context

### The distinction to draw is not the one the bead named

`gqlc-h9n.6` asked for the reason to distinguish "not a property value type" from
"not representable in the targeted store", so that a decision could be revisited
when a different backend is targeted.

The second half of that has no members, and the reason is architectural. ADR 0010
already puts store limits in codegen: `INT128` resolves to `graph.TypeInt128` in
the schema model and fails at *generation* time with "unrepresentable: neo4j
stores int64". The schema parser is store-agnostic on purpose. So a family
declined *here* because neo4j cannot store it would be declined in the wrong
layer — it should parse, land in the model, and fail in codegen like `INT128`
does.

Applying that test, nothing in these six families is a store limitation. What
remains is a distinction that cuts the same six a different way, and it is the
one an author actually needs:

- **What the construct is.** `PATH`, the reference types and the immaterial
  types. No change to `graph.PropertyType`, and no future backend, reaches these.
- **What gqlc has not built.** `RECORD` and the dynamic unions. They are property
  value types; gqlc has not modelled them.

The messages carry it. The permanent three say what the construct *is* ("PATH is
a traversal a query produces, not a value an element stores"). The other two end
"which this model does not have yet", and the *yet* is load-bearing.

### 1. `PATH` — permanent

`pathValueType : PATH notNull?` (GQL.g4:1964). A path is a sequence of
alternating nodes and edges produced by a traversal. Storing one on an element
would store a query result, and the result is only meaningful relative to the
graph that produced it.

### 2. Reference value types — permanent

`referenceValueType` (GQL.g4:1900-1904) covers graph, node, edge and binding
table references, open and closed.

A property typed `ANY NODE` is a relationship between two elements. gqlc's schema
model already has exactly one place relationships live — `Schema.Edges`, keyed by
a source/label/target triple — and a node-valued property would be a second one,
invisible to every traversal and to every generated repository method built from
`Schema.Edges`. The construct is not unrepresentable; it is *already*
representable, as the edge it is.

The binding table shares the sentinel because it is ISO's fourth alternative of
the same production and because it is a query result rather than stored data. It
is filed here rather than given its own sentinel for the reason in §"one sentinel
per justification" below.

### 3. Immaterial value types — permanent

`nullType : NULL_KW` and `emptyType : NULL_KW notNull | NOTHING`
(GQL.g4:1907-1919).

`NULL` admits exactly one value. `schema.Property.Nullable` already records that
a property may be null, so the type adds nothing the model does not have — it is
not that gqlc cannot represent `NULL`, it is that `NULL` says only what the
nullability flag says and leaves no type behind.

`NOTHING` and `NULL NOT NULL` admit no value at all. A property of the empty type
could never be written or read, so there is no generated accessor that would be
correct.

Note that `emptyType`'s `notNull` is *mandatory*, unlike everywhere else in the
value-type grammar. `NULL NOT NULL` is not a null type with a qualifier; it is a
distinct production spelled out of two tokens that each mean something else.

### 4. `RECORD` — unimplemented

`recordType` (GQL.g4:1977-1980), with `fieldTypesSpecification` fully describing
the fields. The grammar leaves nothing out; `graph.PropertyType` is
`type PropertyType string` over a flat set of constants, with nowhere to put
them.

That is the same missing thing `LIST` needs, and `gqlc-h9n.5` holds the design
note for it — including why encoding the parameter in the string is not viable
(`goType`'s bare `return "", false` at internal/codegen/types.go:163 already
means *permanently* unrepresentable, so a structured string would report an
unimplemented feature as a permanent boundary). `gqlc-h9n.33` is filed for
`RECORD` and the closed unions together.

### 5. Dynamic union types — unimplemented, and two different blockers

`ANY VALUE` (alt 7), `ANY? PROPERTY VALUE` (alt 8), `ANY VALUE<A | B>` (alt 9)
and the bare `A | B` (alt 10).

The closed unions need the enum to carry members — `gqlc-h9n.33`, with `RECORD`.

The open ones do not. `ANY VALUE` and `ANY PROPERTY VALUE` are atomic, so a
single `graph.TypeAny` would hold them today. What is missing is a decision about
the generated Go, and the ground is already broken: ADR 0010's mapping table
emits `any` for `scalar null` and `map[string]any` for `scalar map`, so an
untyped value in a generated repository is not a new position. `gqlc-h9n.34` owns
it, and the real question there is narrower than it looks — `ANY VALUE` ranges
over paths and references, which §1 and §2 decided are not property values at
all, while `ANY PROPERTY VALUE` is by construction the union of the storable
ones.

**They share one sentinel anyway.** See below.

### The taxonomy is ISO's, and that is checkable

Each sentinel is named after the production it declines: `path value type`,
`reference value type`, `immaterial value type`, `record type`, `dynamic union
type`. All five are in `isobnf.DDLClosure`, the free ISO artefact vendored under
`internal/schema/gql/isobnf`, and `TestValueTypeFamiliesAreIsoProductions` checks
them against it.

That check is worth more than it looks. A claim that a taxonomy is the standard's
is cheap to write and rots silently; a sentinel later renamed after a Go-side
concern would read just as plausibly. The test is what notices.

It matters because these five names are the vocabulary an author gets back when
their schema is rejected. A name they can look up in the standard is a name they
can act on.

### One sentinel per justification, not per construct

`gqlc-h9n.12` split `ErrUnsupportedSource` into `ErrLikeGraphSource` and
`ErrCopyOfSource` because the two rejections had *incompatible* justifications —
one permanent, one merely unimplemented — and a single sentinel could not carry
both. The converse is the rule applied here: constructs that differ but share a
justification keep one sentinel.

So the four reference types are one sentinel, not four: a graph, a node, an edge
and a binding table are all handles rather than values, and four copies of that
sentence is the per-name register `gqlc-h9n.26` rejected.

The dynamic unions are the hard case, because the open and closed halves *do*
have different blockers. They keep one sentinel because the blocker is a fact
about gqlc's enum and the sentinel names an ISO production: splitting `dynamic
union type` in two because gqlc's internals differ would put gqlc's
implementation into the error vocabulary, which is exactly what naming the
sentinels after ISO productions avoids. The difference is recorded where it
belongs — in two beads, and in the declined-carriage register's account.

### What still reports the bare class

`ErrUnsupportedType` remains, and is still produced bare, by `LIST`/`ARRAY` and
by any predefined scalar spelling `typeSpellings` does not carry. `LIST` is
`gqlc-h9n.5`'s to justify and has no recorded reason of its own, so it keeps the
class rather than borrowing a reason from a family it does not belong to. A
`valueType` alternative added to the grammar after this ADR lands in the same
place, for the same reason.

That is why `ErrUnsupportedType` stays in `allSentinels` while
`ErrUnsupportedSource` does not: it is a class *and* a reachable leaf.
`TestListStillReportsTheBareClass` pins it, asserting the absence of all five
family sentinels rather than merely the presence of the class — otherwise a
classifier that grew a `LIST` case would be caught only by corpus entries, which
are data and would be updated to match it.

### The declined-carriage register had to learn about classes

`declinedCarriers` (corpus_resolving_test.go) accounts for every grammar name no
resolving corpus file reaches, filed under the sentinel explaining it, and it
required every carrier of a name to reject with that *exact* sentinel.

Splitting the sentinel breaks that for seven names, and the breakage is
informative rather than incidental: `ANY` spells both `ANY VALUE` and `ANY NODE`;
`fieldName`, `fieldType`, `fieldTypeList` and `fieldTypesSpecification` serve
`RECORD { f :: STRING }` and `BINDING TABLE { id :: STRING }` alike; the angle
brackets are shared by `LIST<T>` and `ANY VALUE<A | B>`. No leaf accounts for all
the carriers of any of them.

The rule is now: a name is filed under the **most specific** sentinel that
accounts for every one of its carriers. Where all carriers share an exact
sentinel, that exact sentinel is required — without which the register could
satisfy itself by parking everything under the class, which is the
undifferentiated state this ADR was filed against. Where they do not, the class
is the honest answer, and it says something true about the grammar that
per-family filing would suppress.

Two register groups were also blaming `gqlc-h9n.6` for undirected edges —
"retire when gqlc-h9n.6 decides whether to model it". That bead decides value
types and never touched undirectedness; ADR 0016 decided it, permanently. Both
now name `gqlc-0ri` and state the permanence, which is what
`18.3-edge-type/pattern_undirected.gql` has said all along.

## Considered options

**Decline `RECORD` because neo4j has no record-valued property.** Rejected: by
ADR 0010's own division that decline belongs in codegen, and the parser would
have to accept it first. The real blocker is the flat enum, which is gqlc's and
not the store's.

**Give the four reference types four sentinels.** Rejected: they share a
justification, and `gqlc-h9n.12` split `ErrUnsupportedSource` precisely because
its two leaves did *not*. Splitting on construct rather than justification
reproduces the per-name register `gqlc-h9n.26` measured and rejected.

**Split the dynamic unions into open and closed sentinels.** Rejected, and it is
the closest call here, because the two halves genuinely have different blockers.
It loses more than it gains: the sentinel names an ISO production, and dividing
one production on the shape of `graph.PropertyType` makes the error vocabulary
track gqlc's internals rather than the standard the author wrote against. The
distinction is recorded in `gqlc-h9n.33` and `gqlc-h9n.34`.

**Add a middle class — `ErrValueTypeNotStorable` / `ErrValueTypeNotModelled` —
so permanence is matchable with `errors.Is` rather than only readable.**
Rejected: no caller branches on permanence today, and an unconsumed layer between
the leaves and the class would be shape without a consumer. If one appears, the
leaves are already grouped the right way and inserting it is mechanical.

**Implement `ANY VALUE` as `graph.TypeAny` here, since nothing blocks it.**
Rejected: `gqlc-h9n.6` is a decision bead, and the open question — whether
`ANY VALUE`, which ranges over the things §1 and §2 just declined, should be
accepted on the same terms as `ANY PROPERTY VALUE` — is a decision of its own.
`gqlc-h9n.34`.

**Leave the single sentinel and record the six reasons in the deviation register
only.** Rejected: that is the state `gqlc-h9n.6` was filed against. The register
is read by gqlc's authors; the error message is read by the schema author, who is
the one who has to decide whether to change the schema or wait.

## Consequences

- Five new sentinels, all wrapping `ErrUnsupportedType`. Every existing
  `errors.Is(err, ErrUnsupportedType)` keeps matching, so the split widened the
  public surface rather than narrowing it.
- `declineValueType` (propertytype.go) decides on the parse tree rather than the
  spelling, because a lookup miss can only report that the lookup missed. It
  reads the outermost context and does not descend: `RECORD { f :: STRING }` and
  `BINDING TABLE { id :: STRING }` both nest a `predefinedType` for the field, so
  a subtree search would answer for the field instead of the property.
- Sixteen corpus entries move from `ErrUnsupportedType` to a family sentinel. The
  four `LIST`/`ARRAY` entries do not. `wantCorpusEntries` is unchanged — no file
  was added, because the corpus already covered every alternative.
- Corpus `bead:` fields follow the split: the permanent three name `gqlc-0ri`,
  the two unimplemented ones name `gqlc-h9n.33` / `gqlc-h9n.34`.
- `gqlc-h9n.33` and `gqlc-h9n.34` are filed. Neither is blocked; `gqlc-h9n.34` in
  particular needs no model change, which is why it is not folded into
  `gqlc-h9n.5`.
- `gqlc-h9n.5` is unaffected and undecided. `LIST` still reports the bare class,
  and `TestListStillReportsTheBareClass` fails if that changes without a decision.
