# Temporal columns are carried by gqlc-owned neutral types

The emitted public surface — models, querier signatures, row structs — today
names driver types for four temporal widths on the neo4j targets:
`dbtype.Date`, `dbtype.Time`, `dbtype.LocalTime`, `dbtype.Duration`. The AGE
target refuses those same four widths outright. So a schema using DATE compiles
for one backend, fails generation for the other, and a target swap on the one
that compiled breaks every caller that touched the field (bead `gqlc-49hu`).

This ADR decides three things: the neutral representation, where it lives, and
what the AGE backend does with the four widths it used to refuse.

## The representation: flat component structs, and `time.Time` for TIMESTAMP

gqlc owns five types, emitted verbatim into the generated package:

```go
type Date struct{ Year, Month, Day int }
type LocalTime struct{ Hour, Minute, Second, Nanosecond int }
type Time struct {
        Hour, Minute, Second, Nanosecond int
        OffsetSeconds                    int
}
type LocalDateTime struct {
        Year, Month, Day                 int
        Hour, Minute, Second, Nanosecond int
}
type Duration struct {
        Months, Days, Seconds int64
        Nanos                 int
}
```

TIMESTAMP (zoned datetime) stays `time.Time`: the stdlib type is already
driver-neutral, both backends already use it, and it is the one temporal whose
semantics (an instant) `time.Time` models without residue.

`Duration` deliberately has the same field shape as `dbtype.Duration`
(`Months, Days, Seconds int64; Nanos int`), so the neo4j conversion is a
field-for-field copy with nothing to get wrong.

### Rejected: `time.Time`-backed newtypes (the dbtype design)

The driver's `dbtype.Date` is `type Date time.Time`. Copying that shape was
rejected because it smuggles dimensions the width does not have: a
`dbtype.Date` carries a clock time and a `Location`, both of which are
garbage the type's methods paper over, `==` compares the garbage, and two
equal dates can compare unequal. The driver lives with this for wire-protocol
convenience gqlc does not share — bolt packs a Date as epoch-days regardless.
Component structs make every carried field meaningful, `==` is exactly value
equality, and the zero value is inspectable.

### Rejected: `time.Duration` for DURATION

`time.Duration` is a nanosecond count. A Cypher DURATION carries months, and
months do not have a length in nanoseconds. Any flattening invents one.

## Placement: emitted into each generated package, not a runtime module

The five types are rendered into a `temporal.go` in the generated package,
byte-identical across targets, emitted only when the generated surface
references at least one of them. There is no importable gqlc runtime module.

Rejected: a runtime module. gqlc currently emits everything and depends on
nothing at the use site (ADR 0012's exclusively-owned output directory is the
standing shape; bead `gqlc-0aa` already declined a runtime module once). A
module would be the project's first user-facing dependency and would create a
generator/runtime version-skew axis that today cannot exist. The cost of
emission — the same ~40 declared lines in every generated package — is borne
by generated code nobody hand-maintains, and cross-target agreement on the
bytes is already enforced (see Gates below).

Because the names now live in the user's package, they are reserved: `Date`,
`Time`, `LocalTime`, `LocalDateTime` and `Duration` join the existing
`reservedIdentifiers` set (internal/codegen/prepare.go), whose documented
posture this follows — the set is the union across backends and batches, so
the names are reserved even in a batch that emits no `temporal.go`, because a
name that works in one batch but not another is exactly the renaming scheme
that set already refused. A collision today produces a Go compile error in
generated code; membership turns it into a generate-time
`ErrIdentifierCollision` naming the schema construct.

## The neo4j targets: conversion is backend-private and lossless

`typeMap.Property` switches DATE/TIME/LOCALTIME/DURATION to the neutral names;
decode/encode converts to and from `dbtype.*` inside the generated helpers.
Losslessness is not assumed — it is what the wire format already guarantees,
verified in driver source (v5.28.4, `neo4j/dbtype/temporal.go` and
`bolt/outgoing.go`): bolt packs Date as epoch-days, Time as
(clock-nanos, offset-seconds), LocalTime as clock-nanos, Duration as its four
components. Every one of those is a bijection with the component structs
above. Nothing a neo4j server can send is dropped by the neutral carrier, and
nothing the carrier holds is dropped on the way in.

## The AGE target: the four widths are admitted, on the settled encodings

The encodings are the owner-approved table from `gqlc-35yu.11` and are not
relitigated here; agtype has no temporal values, so each width rides an
ordinary agtype property whose gate is ORDERING, not round-tripping:

- **DATE** — ISO `YYYY-MM-DD`, zero-padded, as an agtype string. Decode
  range-checks against the four-digit calendar, the same bound and the same
  argument as ADR 0031: the property is not private to gqlc's writer. This
  closes the range-check debt filed as `gqlc-2fz1`.
- **LOCALTIME** — microseconds since midnight, int64 in `[0, 86400e6)`.
- **TIME** — the same count, UTC-normalised, plus the existing `<f>Offset`
  offset-seconds sidecar — the mechanism ADR 0031's neighbour already bounds,
  and `rejectOffsetSidecarCollisions` (age/errors.go) already reserves the
  name for. The bead asked whether the sidecar suffices for neutral zoned
  TIME: it does — `Time` is exactly (clock reading, offset), which is exactly
  (count, sidecar).
- **DURATION** — total microseconds, int64. ADR 0002 collapsed the
  `(YEAR TO MONTH)`/`(DAY TO SECOND)` qualifier, so a calendar duration cannot
  be detected at generate time; encode therefore refuses at runtime when
  `Months != 0`, naming the field. Decode returns the count normalised into
  `Seconds` + `Nanos` with `Months = Days = 0`.

Sub-microsecond precision truncates at encode, silently — the policy
`agtypeMicros` already applies to TIMESTAMP, extended, not invented.

Two refusals survive unchanged. Temporal **expression** kinds stay refused on
AGE (`Temporal()` in age/types.go): AGE 1.7.0 has no temporal constructor
functions, so this is a dialect gap, already loud and documented at the
refusal site. Lists of temporals stay refused: the sidecar scheme has no
per-element home for offsets, and admitting the non-zoned ones alone is a
follow-up with its own bead, not a rider here.

## Gates

**Cross-backend agreement already has a gate.** `TestBackendInvariantSurface`
(conformance_test.go) compares declared Go surfaces across every fixture
enrolled in ≥ 2 targets. Falsifier run for this ADR: mutating one
dual-enrolled golden signature (`arg int64` → `arg int32`) turned it red,
naming the fixture and the declaration. The work this design adds is
enrolment — the temporal fixtures gain the AGE target, so divergence in the
temporal surface becomes a failure the existing gate can see.

**Driver-freedom needs a new gate.** Nothing today fails when a driver package
appears in the emitted public surface — the neo4j goldens are the
counterexample, importing dbtype from models.go right now. The new conformance
sweep walks every golden's AST and fails on any exported declaration outside
the connection surface — `db.go` and `graph.go`, the same two files
`connectionSurface` already names, where the driver is the point (`DBTX`
quotes pgx, `SessionInit` takes a `*pgx.Conn`) — that names a driver-package
type. Unexported positions stay free to use the driver: that is where the
conversions live. It is self-witnessing in the branch that
introduces it: red against the goldens before the typeMap switch, green after,
in the same PR.

## Consequence

A schema using any temporal width generates for both backends with an
identical public surface, and swapping targets recompiles callers untouched.
The remaining AGE temporal gap is expressions, not columns, and it is refused
loudly at generate time. Execution is split across three beads (neo4j switch +
gates; AGE non-zoned admission; AGE zoned TIME), each gated on this design.
