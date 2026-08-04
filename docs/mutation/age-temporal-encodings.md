# Mutation record: the Apache AGE temporal encodings

The table below is the evidence behind the claim that the AGE temporal
encoding is pinned by tests rather than merely covered by them. It is
here rather than in a review comment so that a reader can re-derive every
row.

## Protocol

Each row is one compile-valid edit to gqlc's own source, applied alone.
Per mutant:

1. apply, then `go build ./...` — a mutation that does not compile kills
   its mutant for the wrong reason and is not admissible;
2. `go test ./internal/codegen/conformance/ -update -run
   'TestConformanceSuite/TestValid'` — **regenerate the goldens from the
   mutant**;
3. `go test -count=1 ./...`;
4. restore.

Step 2 is the whole of the audit. A conformance golden is a stored copy
of what the generator emits, so any mutation that moves the emitted bytes
reddens `TestValid` — and `-update` blesses it away again. A mutant whose
only killer was that diff is not killed; it is recorded here as
**golden-only**, which counts as a survivor. The `moved` column is how
many golden files step 2 rewrote, and it is what separates a golden-only
kill from a mutant that moved nothing at all.

`TestGoldenBuild` is not golden-only: it compiles the regenerated tree,
so it holds after `-update`. It proves the emitted package compiles and
nothing beyond that, which is why the semantic rows below are carried by
the corpus tests instead — those run the emitted helpers against captured
agtype text, from fixture inputs rather than from goldens.

Five rows — M07, M21, M23, M24, M27 — name `TestGoldenBuild` as their
**sole** killer. Each is a compilation claim and nothing more: the mutant
emits a call to a helper the file does not declare, an import it does not
use, or a value of the wrong type. The pin matches the claim in those
five cases. A row asserting a *behaviour* with only `TestGoldenBuild`
beside it would be a gap, and there are none.

Corpus test names (`TestAgtype*`, `TestDecodeVertex*`,
`TestEmittedMethodBindsAnInstantAsMicroseconds`) are declared in
`internal/codegen/age/testdata/corpus_test.go.txt` and run inside the
throwaway module `TestEmittedHelpersDecodeTheAgtypeCorpus` assembles.

## Table

| # | Arm | Mutation | Verdict | moved | Killed by |
|---|-----|----------|---------|-------|-----------|
| M01 | TIMESTAMP's Go carrier is `time.Time` | `Property` returns `"int64"` | killed | 10 | `TestBackendInvariantSurface` (all four temporal fixtures), `TestTypeMapProperty`, corpus |
| M02 | a list of instants is refused | `Property` drops the `elemTy == goInstant` disjunct | killed | 0 | `TestTypeMapProperty/a_list_of_an_uncarried_element_width_is_rejected`, `TestConformanceSuite/TestInvalid` |
| M03 | a temporal *expression* column is refused | `Temporal` admits `TemporalDateTime` | killed | 0 | `TestTypeMapTemporal/datetime`, `TestTemporalProjectionIsRefusedNamingTheKind/datetime` |
| M04 | the sidecar read is marked from the instant and nothing else | `h.zone` marked off `"string"` | killed | 29 | `TestZoneIsMarkedOnlyBesideTheInstant`, `TestEmittedHelpersAreClosedOverWhatTheyCall`, `TestGoldenBuild` |
| M05 | an entity carrying an instant emits `agtypeZone` | `h.zone` never marked | killed | 4 | `TestZoneIsMarkedOnlyBesideTheInstant`, `TestGoldenBuild`, corpus |
| M06 | the instant helper drags in the integer helper it reads through | `need` drops `h.integer` from the instant arm | killed | 0 | `TestEmittedHelpersAreClosedOverWhatTheyCall` |
| M07 | a bound instant marks the encoder of its own nullability | `forParams` arms swapped | killed | 1 | `TestGoldenBuild` |
| M08 | a decoded instant puts the `time` import in models.go | `temporal()` drops `h.instant` | killed | 2 | `TestZoneIsMarkedOnlyBesideTheInstant`, `TestGoldenBuild` |
| M09 | `agtypeInstant` reads MICROseconds | `time.UnixMilli` | killed | 4 | `TestAgtypeInstantCountsMicrosecondsFromTheEpoch` |
| M10 | `agtypeInstant` normalises to UTC | `.UTC()` dropped | killed | 4 | `TestAgtypeInstantCountsMicrosecondsFromTheEpoch`, `TestDecodeVertexReadsTheOffsetSidecarBesideItsInstant` |
| M11 | the sidecar is offset-SECONDS | `int(offset)*60` | killed | 4 | `TestAgtypeZoneReadsAnOffsetInSeconds` |
| M12 | an absent sidecar leaves the instant alone | returns `time.Time{}` | killed | 4 | `TestAgtypeZoneReadsAnOffsetInSeconds`, `TestDecodeVertexReadsTheOffsetSidecarBesideItsInstant` |
| M13 | `agtypeMicros` writes MICROseconds | `at.UnixMilli()` | killed | 1 | `TestEmittedMethodBindsAnInstantAsMicroseconds`, `TestAgtypeMicrosEncodesTheInstantAndNotTheWallClock` |
| M14 | `agtypeNullableMicros` writes MICROseconds | `at.UnixMilli()` | killed | 2 | `TestAgtypeNullableMicrosCarriesAbsence` |
| M15 | **control** — semantically identical | `agtypeMicros` body becomes `at.UTC().UnixMicro()` | **golden-only** | 1 | nothing |
| M16 | **control** — the same count, inlined at the call site | `encodeParam` emits `<access>.UTC().UnixMicro()` | **golden-only** | 1 | nothing |
| M17 | the sidecar is named `<property>Offset` | `<property>Zone` | killed | 4 | `TestDecodeVertexReadsTheOffsetSidecarBesideItsInstant`, `TestRejectsAnAuthorOwnedOffsetSidecar` |
| M18 | a projected instant decodes through `agtypeInstant` | `decodeFunc` returns `agtypeInt64` | killed | 6 | `TestGoldenBuild`, corpus |
| M19 | a bound instant is encoded, not sent as a formatted string | `encodeParam` returns the bare access | killed | 2 | `TestEmittedMethodBindsAnInstantAsMicroseconds` |
| M20 | the parameter encoder matches the parameter's nullability | `encodeParam` arms swapped | killed | 2 | `TestGoldenBuild`, corpus |
| M21 | an instant's zero value is a composite literal | `zeroLiteral` returns `"0"` | killed | 2 | `TestGoldenBuild` |
| M22 | a BOUND instant puts the `time` import in its `.cypher.go` | `namesInstant` drops the `ParamFields` disjunct | killed | 2 | `TestGoldenBuild`, corpus |
| M23 | a PROJECTED instant puts the `time` import in its `.cypher.go` | `namesInstant` drops the `RowFields` disjunct | killed | 2 | `TestGoldenBuild` |
| M24 | the interface file imports `time` when a signature spells the instant | `renderQuerier`'s arm negated | killed | 48 | `TestGoldenBuild` |
| M25 | an entity decoder reads the sidecar beside its instant | zoning call deleted from `writeEntityFieldDecode` | killed | 4 | `TestDecodeVertexReadsTheOffsetSidecarBesideItsInstant` |
| M26 | the sidecar is read from its own key, not the instant's | `writeInstantZoning` emits `f.PropName` | killed | 2 | `TestDecodeVertexReadsTheOffsetSidecarBesideItsInstant` |
| M27 | a `:one` over a stored TIMESTAMP returns the struct zero | neo4j `zeroValueText` drops `"time.Time"` | killed | 4 | `TestGoldenBuild` |
| M28 | **control** — the successor sentinel | `TemporalCount = int(TemporalDuration) + 1` | **survived (inert)** | 0 | nothing |
| M29 | the offset-sidecar collision gate runs at all | call deleted from `generate` | killed | 0 | `TestRejectsAnAuthorOwnedOffsetSidecar`, `TestConformanceSuite/TestInvalid` |
| M30 | the collision gate scans every field | `e.Fields[:min(1, len(e.Fields))]` | killed | 0 | `TestRejectsAnAuthorOwnedOffsetSidecar` |
| M31 | the collision gate scans edges as well as nodes | `if e.Kind != codegen.EntityNode { continue }` | killed | 0 | `TestRejectsAnAuthorOwnedOffsetSidecar` |

28 killed, 2 golden-only, 1 inert survivor.

## What the three non-kills mean

**M15 and M16** are the audit's own controls, built to be semantically
identical to what they replace: `UnixMicro` is defined off the absolute
instant, so `.UTC()` in front of it cannot change the count. Both move
the emitted bytes and nothing else, and both are green the moment the
goldens are regenerated. They are in the table to show that the column
distinguishes them — an encoding table whose kills were all of this shape
would be pinned by nothing.

**M28** restores the spelling `TemporalCount` replaced, and is inert
because the two expressions agree at the current membership of
`resolver.Temporal`. That is the point: the defect the sentinel exists to
catch is only observable under a change to the enum, so its evidence is
the append experiment recorded on the commit that introduced it —
appending a member reddens both type-table sweeps with `should have 7
item(s), but has 6`, where `int(TemporalDuration)+1` left them green. A
mutation of the expression alone cannot show it, and this row says so
rather than counting itself as coverage.
