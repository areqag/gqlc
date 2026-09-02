# RECORD and closed dynamic unions resolve into a parameterised property type

A schema declaring `addr :: RECORD { city :: STRING, zip :: INT32 }`, or
`v :: ANY VALUE<BOOL | DATE>`, used to fail in the schema front end:

    unsupported property value type: RECORD needs a property type that carries
    its fields, which this model does not have yet

It now parses, resolves, and reaches codegen as `RECORD<city STRING,zip INT32>`
— and codegen refuses it, once, with a sentinel of its own:

    property type kind not implemented yet: entity "Place" property "addr" has
    RECORD<city STRING,zip INT32>

The refusal moved down a layer, and the reason it gives changed with it. Before,
gqlc could not *model* a record; now it models one and has not built its
emission. Those are different repairs for a reader, and the second is the true
one.

Written 2026-09-02 by Ար, executing the design ruled by Արփինէ on bead
`gqlc-be1me` (`gqlc-h9n.33`), with two deviations recorded below.

## The encoding: parameters in the string, not a struct beside it

`graph.PropertyType` is a `string`. It already carried one parameterised family,
`"LIST<ELEM>"`; this adds three spellings beside it:

| spelling | means |
|---|---|
| `RECORD<ANY>` | `RECORD` / `ANY RECORD` — fields undeclared |
| `RECORD<>` | `RECORD { }` — declared to have none |
| `RECORD<city STRING,zip INT32>` | the declared fields, sorted by name |
| `UNION<BOOL\|DATE>` | a closed dynamic union's members, sorted |

An inner `enc NOT NULL` carries that field's or member's own qualifier.
`Kind()`, `Fields()` and `Members()` read the encodings back; `RecordOf` and
`UnionOf` are the only sanctioned way to build one.

### Rejected: a composite type

The obvious shape is `PropertyType` becoming a struct with a kind, an element,
a field slice and a member slice. It was rejected because **comparability is
load-bearing in three places at once**, and a struct holding slices is not
comparable in Go at all:

- the resolver unifies a query reference across labels by comparing property
  types with `==` (`scope.go:1023`, `resolve.go:1807`, `:1830`, `:2037`);
- both backends' type maps are a `switch pt { case graph.TypeString: … }`, and a
  Go switch on a non-comparable value does not compile;
- `PropertyType` marshals as a bare string in the validated-schema JSON
  (`resolver/validated.go:181`), so a composite would change that wire shape.

Replacing `==` with a deep-equality helper would have meant finding every
comparison, and the ones it missed would compile and silently stop unifying.
The string keeps all three properties and confines the new complexity to the
constructors.

That confinement is the whole bet, so it is guarded rather than trusted:
canonicalisation is what makes string equality mean type equality, and a string
assembled by hand skips it. `RecordOf` sorts fields by name, so
`RECORD { a :: INT, b :: STRING }` and its reverse spelling are one type rather
than two that never unify (`TestRecordOfCanonicalisesFieldOrder`, and through
the parser, `TestRecordFieldOrderCanonicalisedThroughParser`).

### The field name is the encoding's own hazard

GQL's `<field name>` admits a delimited identifier (GQL.g4:2891, :2956-2958), so
a legal field name can contain `,`, `|`, `<` and `>` — the encoding's structure.
Left bare, such a name forges a field boundary and two unrelated record types
collide on one string; because `==` is how types unify, that silently equates
them. `quoteFieldName` uses the lexer's own escape — backtick-delimited,
internal backticks doubled — and `splitTopLevel` treats quoted spans as opaque
and splits only at nesting depth zero. Without either, a naive split reports
four fields for `RECORD<a RECORD<x INT,y INT>,b STRING>` and three members for
`UNION<LIST<UNION<BOOL|DATE>>|STRING>`, all garbage
(`TestFieldsAndMembersSplitAtTopLevelOnly`, `TestRecordFieldNameQuoting`).

One more property holds the `NOT NULL` suffix test up: no scalar constant
contains a space, and every parameterised encoding ends in `>`, so a trailing
` NOT NULL` is never one belonging to something nested. That is a property of
the constant table rather than an argument, so
`TestScalarConstantsCarryNoSpace` sweeps the table and holds it.

## The reductions are gqlc's decisions, not readings of ISO

`UnionOf` reduces its members to a canonical set: nested unqualified unions
flatten, a bare `ANY` absorbs the whole union, exact duplicates collapse,
members sort by encoded spelling, and a lone unqualified member is not a union
at all but that member's own type.

**None of these is read out of the standard.** ISO/IEC 39075's semantics volume
is not among the freely published artefacts — the BNF and Annex D are free, the
semantics cost CHF 227, and `gqlc-lir` is the bead that declined the purchase
and remains the arbiter. So these are recorded here as gqlc's answers, to be
re-opened against the prose if it is ever bought, rather than presented as
conformance.

Three of them have an edge the reduction had to be careful about:

- **A lone `NOT NULL` member does not collapse.** `UNION<STRING NOT NULL>`
  stays a union, because the union is the only place that qualifier could live
  and collapsing would drop it silently.
- **Only an *unqualified* nested union flattens.** A `NOT NULL` one has nowhere
  to put its qualifier once its members are spliced in. The parser cannot
  produce that shape — neither closed-union alternative admits an outer
  `NOT NULL` (GQL.g4:1731-1732) — so the arm exists for direct callers.
- **`RECORD<>` and `RECORD<ANY>` are distinct types.** One is a record with no
  fields; the other a record whose fields are undeclared. Folding them would
  make `RECORD { }` mean `ANY RECORD`, which admits everything
  (`TestRecordAnyAndEmptyAreDistinct`).

`TestUnionOfSetSemantics` and `TestUnionOfCanonicalisesMemberOrder` hold the
set; `TestElemTrimChainSurvivesParameterisedElements` holds that `Elem()`'s
suffix-trimming chain still reads a parameterised element correctly.

### A trailing NOT NULL binds to the last member, not to the property

`p :: BOOL | DATE NOT NULL` reads, under GQL.g4, as a union of `BOOL` and
`DATE NOT NULL` — a nullable property whose second member is not null. The
charitable reading (the author meant the property) is available and is not
taken: neither closed-union alternative admits a `NOT NULL` of its own, so the
grammar has already decided where the token attaches, and inventing an
attachment the grammar does not have would make gqlc's answer unpredictable from
the source. `TestClosedUnionTrailingNotNullBindsToTheRightMember` pins it, and
the corpus file says so where an author would meet it.

## A repeated field name is rejected, on ADR 0030's grounds

`RECORD { a :: INT, a :: STRING }` now has an answer, and the answer is a
refusal: `ErrDuplicateFieldName`. This is ADR 0030's rule read onto fields, and
it is provisional on exactly the same footing — `<field type list>` states no
uniqueness constraint either, so the free BNF admits it and the reading lives in
Syntax Rules prose gqlc has not bought.

It is a sentinel of its own rather than a reuse of `ErrDuplicatePropertyName`
because the two are not interchangeable to a reader: a field name repeats inside
one *property's value type*, and reporting a repeated PROPERTY name would send
that reader to the property list, where the names are distinct.

This case **could not have been written before**. A record was declined at its
outermost context and its fields were never read, so a duplicate among them was
unreachable; the encoding that made fields resolve is what made the duplicate a
question with an answer. It is the corpus's one new file
(`constructed_record_field_name_repeated.gql`, 136 → 137 entries).

## The emission question, asked before the carrier question

This is the half the design named and the half most likely to be got wrong
later, so it is stated as an ordering rule.

`prepare.go` asks a dialect's type map `Property(pt)` for a Go carrier. A table
that falls off its switch answers `ok=false`, which becomes
`ErrUnrepresentableWidth` — *"there is no faithful Go type for this declared
width"*. For `RECORD<a INT32>` that sentence names the wrong edit. There is no
width to change; there is nothing at any width, on any backend.

So a third question is asked **first**:

```go
if kind, unbuilt := unimplementedTypeKind(p.Type); unbuilt {
    return nil, fmt.Errorf("%w: entity %q property %q has %s", ErrUnimplementedTypeKind, ...)
}
ty, ok := tm.Property(p.Type)
```

`ErrUnimplementedTypeKind` ("property type kind not implemented yet") is asked
at four sites — the entity property sweep, the query-column sweep, the
query-parameter sweep, and the list-element plan — and it is the only sentinel
in the set that is **not a target's answer**. `ErrUnrepresentableWidth` and
`ErrUnstorableProperty` are refusals one backend makes and another may not,
which is why ADR 0035's rule has them naming themselves. This one holds on every
target at once, which is why it lives in `prepare.go`, why both type maps are
untouched by it, why its message names no backend, and why the invalid fixture
`unimplemented_kind_record_property` enrols **all three** targets rather than
one. It is also the only sentinel in the taxonomy carrying a *"yet"*: the three
`schema/gql` families that used to carry one no longer do, because a family
declined for want of a build belongs at the layer that has not built it.

### The walk recurses through list elements

`unimplementedTypeKind` is recursive on `Elem()`. A shallow check would admit
`LIST<RECORD<a INT32>>`, whose record then dies inside the type table as a width
error — the exact confusion this sentinel exists to prevent, one level down.

**Deviation 1 (recorded in the code):** the design named `Fields()` and
`Members()` as further descent points. The walk does not use them. A record is
refused at its own node, so nothing below one is reachable, and the arms would
be mechanism no test could witness. When emission arrives and a record stops
being refused at its node, that descent becomes reachable and is the emission
bead's to add.

### How the ordering is measured rather than asserted

Each refused row runs against **two** type maps, and the pair is the point:

- a table with **no case for the width** would refuse it anyway, so this row
  measures ORDER — move the walk below `tm.Property` and every one of these
  reds;
- a table **admitting every width** would refuse nothing, so this row measures
  INDEPENDENCE — the refusal must not borrow a table's.

`TestUnimplementedKindRefusedBeforeTheCarrierQuestion` runs 4 positions × 2
tables × 7 refused widths, and then 4 positions × 3 admitted widths as an
over-refusal fence. **Those fence rows were green before the change** — which is
what makes them a fence rather than a co-signer.

The whole guard was mutation-screened in two passes, because KILLED
self-certifies and SURVIVED does not: 12 mutants, each compiled with
`go test -c` first to exclude a compiler-killed fake RED, each verdict scoped
with an anchored `-run` so it names the guard claimed. 12/12 KILLED, zero
compile-failed. The two rows that matter most — deleting the recursion (the
design's central falsifier) and moving the walk below `tm.Property` — are killed
by this test alone.

## The corpus moved, and the count was measured rather than derived

`wantCorpusResolving` 78 → 86: the six record spellings and the two closed
dynamic unions in §18.9. Derived from the retired sentinels' entries, the answer
would have been **89** — three files spelled `RECORD` only incidentally, to pin
that a collector forwards a decline from a nested position. Those were swapped
to `PATH` and stay unsupported, and the pin they carry is now stronger, since
`RECORD { p :: PATH }` reports `ErrPathValueType` from the field rather than
being declined at the record.

**Deviation 2:** the design's last step said to retire four `isoGaps` rows in
`corpus_iso_test.go` and lower `isoGapRatchet` from 14. That gate was already
green and stayed green — those entries record absence from **GQL.g4 as a named
rule**, not absence of implementation, and GQL.g4 spells these as labelled
alternatives of `valueType` either way. Deleting them would have reddened
`TestISOProductionInventory`. The four `why` fields were rewritten to say
"implemented:" instead, the rows and the ratchet stand, and the conflation the
step tripped over — a list whose name says "gap" holding mostly implemented
constructs — is filed as `gqlc-ke7ox` rather than ridden here.

## Consequences

- `ErrRecordValueType` and `ErrDynamicUnionType` are deleted.
  `internal/schema/gql`'s `ErrUnsupportedType` family is now three leaves, all
  of the permanent kind, and the absence of a "yet" among them is the claim.
- `declineValueType` narrows to `PathValueTypeLabelContext` and
  `PredefinedTypeLabelContext`. It still reads only the outermost context;
  descent happens in `resolveValueType`, and only through a construct that
  resolves.
- Both type maps gain an explicit `case graph.TypeAnyRecord` returning
  `("", false)`. The arm is unreachable — `prepare.go` refuses the kind first —
  and exists because `TypeAnyRecord` is a *constant*, so the `exhaustive` linter
  demands it, exactly as `graph.TypeList`'s equally unreachable arm is demanded.
  Its answer is fail-closed rather than a width claim; that is what the sweep
  rows pin. neo4j's `TestTypeMapProperty` walks the arms and named the omission
  itself. AGE's does not walk them, so its row was added by hand — filed as
  `gqlc-ozdkx`.
- The four sentinel-taxonomy rows and the `sentinelIdent` arm are registered, so
  the bidirectional registry sweep is satisfied by enrolment rather than by
  exemption.
- Emission is **not** in this change and is not this bead's. It is filed as a
  design-plus-stub pair under epic `h9n`, because the shape of a Go carrier for
  a record — a generated struct, a `map[string]any`, or per-backend — is a
  design question, and answering it inside an execution bead would be the
  design gate skipped.
