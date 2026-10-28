# A declared binary width rounds up to the smallest Go type that fits

`INT(7)` resolves to `Int8` and `INT(10)` to `Int16`. A parenthesised binary
width that the model has no exact constant for resolves to the narrowest width
constant that holds it. Until now it fell through to the unqualified machine
word, `Int` or `Uint`, which generates as Go `int` or `uint`.

This supersedes [ADR 0017](0017-off-lattice-binary-widths-accepted-and-lost.md),
which decided that such a width is accepted and lost, and which considered and
rejected rounding up.

## The owner ruling

On 2026-10-27 the repository owner ruled, verbatim:

> let's do smallest go type that fits

Recorded on `gqlc-h9n.31` (GH #462).

## Decision

The rule covers the five spellings the grammar gives a parenthesised binary
integer: `INT(p)`, `INTEGER(p)` and `SIGNED INTEGER(p)` on the signed lattice,
`UINT(p)` and `UNSIGNED INTEGER(p)` on the unsigned one (`GQL.g4`
`signedBinaryExactNumericType`, `unsignedBinaryExactNumericType`,
`verboseBinaryExactNumericType`). `p` resolves to the first width constant
whose bit count is at least `p`:

| `p` | signed | unsigned | Go type |
|---|---|---|---|
| 1 – 8 | `Int8` | `Uint8` | `int8` / `uint8` |
| 9 – 16 | `Int16` | `Uint16` | `int16` / `uint16` |
| 17 – 32 | `Int32` | `Uint32` | `int32` / `uint32` |
| 33 – 64 | `Int64` | `Uint64` | `int64` / `uint64` |
| 65 – 128 | `Int128` | `Uint128` | none, refused by both targets |
| 129 – 256 | `Int256` | `Uint256` | none, refused by both targets |
| 0, or above 256 | `Int` | `Uint` | `int` / `uint`, unchanged |

The exact widths keep the fold `gqlc-h9n.16` gave them (`INT(8)` is still
`Int8`). They are the boundary case of the same rule now, not a separate one.

**`p` is read as a count of bits.** That is gqlc's own decision, not a reading of
ISO/IEC 39075. The free BNF gives `precision` as an unsigned integer and does not
say what it counts. Reading it as bits matches the fold `INT(8)` → `Int8` already
shipped, and matches the sibling tokens `INT8 … INT256`, whose names count bits.
Under ADR 0049 the paid text that could say otherwise is not being bought, so
this stands as a gqlc decision, in the way ADR 0049's three do.

`FLOAT(p)` is out of scope. ADR 0049 settled that it is not folded, and this
decision does not touch it.

## Why ADR 0017's objections no longer hold

ADR 0017 rejected rounding up on two grounds.

**"It is unsourced."** That was an objection to reading the standard without a
citation. This decision does not claim to read the standard. It is an owner
ruling about what gqlc generates, recorded under gqlc's own name, which is the
form ADR 0049 established for questions the free artefacts cannot settle.

**"It buys no correctness even if granted."** That still holds for range: a
signed 7-bit integer holds −64 to 63 and `int8` holds −128 to 127, so a value
the author's declaration excludes still fits the carrier, and nothing checks it.
What rounding up does buy is the carrier the owner asked for. `INT(7)` now
generates `int8`, the Go type nearest to what the author wrote, where before it
generated `int`, which is eight times wider.

ADR 0017's remaining argument stands: rejecting `INT(7)` would make gqlc a
dialect. This decision does not reject anything new in the schema parser.

## Edge cases

What the grammar admits as `precision` is `UNSIGNED_DECIMAL_INTEGER`, which is
`DIGIT (UNDERSCORE? DIGIT)*`. So:

- **Zero.** `INT(0)` keeps resolving to `Int`, as it did before. Zero bits is not
  a width, and nothing is rounded up to cover it. It is not rejected either.
  ADR 0017's argument against failing a whole schema over one qualifier applies
  here as well.
- **Above 256.** `INT(257)` and wider keep resolving to `Int` and `UINT(257)` to
  `Uint`, as before, because no constant holds them. This means `INT(200)` is
  refused by both codegen targets (it is `Int256`), while `INT(300)` generates
  `int`. That is inconsistent, and it is kept on purpose: this decision changes
  only the widths it can round up to a constant, and has no opinion on what a
  width beyond the lattice should be. A value that does not fit the parser's
  16-bit read (above 65535) lands in the same place.
- **Leading zeros and digit separators.** `INT(08)` and `INT(1_6)` are read for
  the number they spell, giving `Int8` and `Int16`. Before this decision they
  missed the exact-width rows, which were matched as text, and resolved to `Int`.
  That was a defect of matching on text, and this decision fixes it as a side
  effect.

## What each codegen target does

Measured 2026-10-27 by running `gqlc generate` on a one-property schema, `p ::
<type> NOT NULL`, with one query returning `p`:

| Declared | neo4j-go-v5 | apache-age-pgx-v5 |
|---|---|---|
| `INT(7)` | `int8` | `int8` |
| `INT(10)` | `int16` | `int16` |
| `UINT(33)` | `uint64` | `uint64` |
| `INT(65)` | refused: `unrepresentable property width … has INT128` | refused: query dropped, `projects property:INT128` |
| `UINT(200)` | refused, `UINT256` | refused, `UINT256` |
| `INT(300)`, `INT(0)` | `int` | `int` |

**This is a breaking change for a schema that declares a width from 65 to 256.**
Before, it generated `int` or `uint`, so it silently held less than declared.
Now it resolves to the 128- or 256-bit constant and fails generation, the same
way an explicit `INT128` already fails. The failure is loud and names the width.
To keep generating, declare a width of 64 or less.

## Consequences

- `typeSpellings` loses its 30 parenthesised-width rows (`INT(8)` … `UNSIGNED
  INTEGER(256)`). `normaliseType` rounds every parenthesised binary width up
  from the type its bare head resolves to, so the exact widths and the
  off-lattice ones go through one rule, and a head cannot be left out of it.
  Missing a head is how `SIGNED INTEGER(8)` once resolved to `Int`.
- `INT(10)` and `INT(16)` now resolve to the same constant. The declared 10 is
  still not recorded: the model has no width field, and this decision does not
  add one. What changed is that the loss is now a rounding the owner chose,
  rather than a fall-through to the machine word.
- The two semantic cases ADR 0017 added for `scalar_signed_integer.gql` and
  `scalar_unsigned_integer.gql` are deleted. `TestSemanticCaseCollisions` went
  red on both, as ADR 0017 predicted, and `wantSemanticCases` moves from 17 to
  15. The `scalar_float.gql` case stays, because `FLOAT(p)` is not folded.
- `unionProperty` and `unify` in the resolver are unaffected in kind. No field
  was added, so two properties still unify if and only if their constants match.
  The only difference is which constant `INT(p)` lands on.
- ADR 0017 is marked superseded. Its `FLOAT(p)` section is still in force, as
  restated by ADR 0049. ADR 0002's paragraph on widths it cannot express points
  here.

## What would reopen this

- A consumer that needs the exact declared width, such as a range-checking
  setter for `INT(7)`. That would mean adding a width field, which is the change
  `gqlc-h9n.31` originally proposed and this decision does not make.
- A decision about widths beyond 256, in either direction: refusing them like
  `INT128`, or keeping `int`. Today they keep the behaviour from before this
  decision, which is the inconsistency recorded above.

## Provenance

Owner ruling 2026-10-27, recorded on `gqlc-h9n.31`.
