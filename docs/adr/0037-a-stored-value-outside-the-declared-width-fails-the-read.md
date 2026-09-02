# A stored value outside the declared width fails the read

A schema declaring `rank :: INT32` used to decode a stored `1099511627776`
into a Go `int32` of `0`, and `tally :: UINT64` used to decode a stored
`-1` into `18446744073709551615`. Both now fail the read:

    decode Person.Rank: value 1099511627776 does not fit the declared int32 width
    decode Person.Tally: value -1 does not fit the declared uint64 width

Struct shapes, method signatures and nullability semantics are unchanged.
What changed is that a decode which could only ever have produced a wrong
number now produces an error instead.

Written 2026-09-01 by Ար, executing the design ruled by Արթուր on bead
`gqlc-k2p68`, for `gqlc-awtb`.

## Why it could happen at all

Neither wire carries the width the schema declares. Bolt hands back
`int64` and `float64`; agtype has exactly two numeric scalars and they are
the same two widths. Everything narrower — `INT8` through `INT32`, every
unsigned width, `FLOAT32` — is a promise the *schema* makes that the
*transport* knows nothing about.

The generated decoders used to keep that promise with a plain Go
conversion at the point of assignment. A Go conversion between integer
widths is defined to truncate, and one from `float64` to `float32`
overflows to an infinity. Neither reports anything. So a value that had
been written outside the declared range — by an earlier version of the
schema, by another writer, by a migration, or by gqlc's own write path
(below) — was silently rewritten into a different, plausible number on
the way out.

That is the worst available failure mode. A wrapped integer is
indistinguishable from a real one: `0` is a perfectly ordinary rank.

## The rule

**A stored value the declared width cannot hold fails the decode.**

The check is per width, and it is two clauses for integers:

```go
out := T(v)
if int64(out) != v || (out < T(0)) != (v < 0) {
    // refuse
}
```

Both clauses are load-bearing, and neither subsumes the other:

- The **round-trip** catches every width whose range is a strict subset of
  `int64`'s. This is all of them except one.
- The **sign comparison** exists for `uint64` alone, where the conversion
  is a bijection: `int64(uint64(-1))` is `-1` again, so the round-trip
  sees nothing wrong and only the signs disagree.

A measurement worth writing down, because the design predicted otherwise:
deleting the round-trip clause does **not** kill an `INT32` value of
`-(1<<40)`. That value truncates to exactly `0`, whose sign differs from
the carrier's, so the *sign* clause refuses it. The two clauses partition
the integer failures differently than "round-trip for the signed ones,
sign for `uint64`" suggests. Both backends behave identically here, which
is how we know it is a property of the check and not of one spelling.

## The float line: overflow is refused, rounding is not

`FLOAT32` is approximate by construction. Every `float64` in range rounds
on the way into it, so refusing precision loss would refuse almost every
value the width exists to hold. What is refused is narrower:

```go
out := float32(v)
if math.IsInf(float64(out), 0) && !math.IsInf(v, 0) {
    // refuse
}
```

— an infinity the conversion *invented*. An infinity or a NaN the store
already held passes through unchanged, because the conversion did not lose
it.

The spelling matters, and it is not interchangeable with the obvious
magnitude test. A `float64` strictly greater than `math.MaxFloat32` can
still round *down* to exactly `MaxFloat32`: it lies below the rounding
boundary and is perfectly representable. `v > math.MaxFloat32` refuses
that value; the `IsInf` composite admits it. The corpus pins this on both
backends with `math.Nextafter(float64(math.MaxFloat32), math.Inf(1))`,
asserted in the **pass** direction — the only direction in which the two
spellings differ.

## What a UINT64 property can actually hold

Both wires carry signed 64-bit integers. A `UINT64` property's readable
set is therefore `[0, MaxInt64]`: a value above `MaxInt64` is *unstorable*
rather than unreadable, and a negative carrier is always a violation of
the declared width rather than a large unsigned value that happens to look
negative. That is the whole justification for treating `uint64(-1)` as an
error rather than as `18446744073709551615`.

## What was rejected

**Saturate to the width's bound.** Clamping `1<<40` to `MaxInt32` replaces
one wrong number with a different wrong number and keeps the property that
the caller cannot tell. It is worse than wrapping in one respect: a
saturated value looks deliberate.

**Trust the schema and document the conversion.** This is what the code
did, unintentionally. Writing it down would have made it a decision
without making it safe, and the values that reach a decoder are exactly
the ones no schema controls — written by other clients, by earlier
versions of the same schema, or by migrations.

**An exported `ErrValueOutOfRange` sentinel.** Declined as surface we
cannot yet justify. No caller has been shown to need to distinguish this
failure from the other decode failures, and an exported sentinel is a
compatibility commitment. The error *text* names the value and the
declared width, which is what a human debugging a bad row needs. If a
caller ever needs to branch on it, adding the sentinel later is a
backwards-compatible change; removing it would not be.

## Where the check lives, and why that differs per backend

The two backends put it in different places, because their decode shapes
differ and the design followed the shape rather than imposing symmetry.

**AGE decodes through a function per width.** Every property and column
goes through `agtypeProperty(props, key, decodeFunc(goType))`, so
`decodeFunc` is a single chokepoint. The check folds into two new
helpers — `agtypeIntAs[T]` and `agtypeFloat32` — and the four sites that
used to convert were **deleted**, not amended. A list wrapper that
previously took a closure wrapping its carrier's decoder now names its
element helper directly.

**neo4j receives asserted locals**, so there is no single function to fold
into and the check sits at each of the eight emission sites, calling
`narrowInt[T]` or `narrowFloat32`. Because that is eight places rather
than one, the neo4j package carries a guard —
`TestEmittedDecodersNarrowThroughACheck` — which parses the emitted decode
bodies and fails on a bare numeric narrowing conversion, so a ninth site
added later cannot quietly reintroduce the wrap. AGE has no counterpart
and does not obviously need one; the gap is recorded as `gqlc-rww4b`
rather than assumed harmless.

Temporal carriers are untouched by all of this. A temporal conversion is a
*shape* change (ADR 0033) — a driver value into a neutral carrier — and
carries no range question, so it keeps its `to<X>` helper.

## The write path is not symmetric, and is not fixed here

Reading is now checked. Writing is not: a `uint64` parameter above
`MaxInt64` still wraps when it is bound on neo4j, and the driver-packed
nullable and list arms, along with AGE's JSON encode path, are
**unmeasured** rather than known-good. That asymmetry is deliberate scope,
and it is tracked as `gqlc-tzjqu`.

One consequence is worth stating plainly, because it looks like a
regression and is not: now that reads are checked, a value that gqlc's own
write path wrapped on the way in becomes **visible** at read-back, as an
error. The bad value was always there. What changed is that it is now
reported instead of being quietly wrapped a second time.
