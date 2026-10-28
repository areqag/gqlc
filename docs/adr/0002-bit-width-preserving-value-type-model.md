# Preserve integer/float bit widths in the property value-type model

The `schema.PropertyType` enum carries the bit width of numeric types
(`Int8…Int256`, `Uint8…Uint256`, `Float16…Float256`, plus machine-word `Int`/
`Uint`/`Float` and `Decimal`), rather than collapsing everything to a single
`Int`/`Float`.

## Considered options

The simplest model maps all integers to one `Int` and all reals to one `Float`.
We rejected that: gqlc generates Go code, and GQL distinguishes `UINT*` from
`INT*` and narrow from wide types. Losing signedness and width would force
codegen to emit `int`/`uint` everywhere and discard information the schema
author explicitly stated.

GQL type spellings are normalised into this enum (e.g. `SMALLINT≈Int16`,
`BIGINT≈Int64`, `UBIGINT≈Uint64`, `REAL≈Float32`, `DOUBLE≈Float64`). A
parenthesised precision folds onto a width constant only where the grammar
makes the parenthesised form a sibling of an explicit width token *and* the
parenthetical admits no scale — that is, `INT(p)` under
`signedBinaryExactNumericType` (`GQL.g4:1801`), `UINT(p)` under
`unsignedBinaryExactNumericType` (`:1814`), and `INTEGER(p)` under
`verboseBinaryExactNumericType` (`:1827`). In those three cases the
parenthetical is a spelling of a type this enum already carries, and the fold
matches the sibling width token (`INT(8)`≈`INT8`).

A declared width this enum has no constant for — `INT(7)`, `INT(10)` — rounds up
to the smallest width constant that holds it: `INT(7)` is `Int8`, `INT(10)` is
`Int16`, reading `p` as a count of bits. That is an owner ruling recorded in
ADR 0050, and a gqlc decision rather than a reading of ISO. It replaces
ADR 0017, under which such a width fell through to the unqualified type and was
lost. The declared `p` itself is still not recorded: `INT(10)` and `INT(16)`
resolve to the same constant.

`FLOAT(p)` is not folded. Its parenthetical is `(LEFT_PAREN precision (COMMA
scale)? RIGHT_PAREN)` at `:1849` — the same scale-bearing shape as
`DECIMAL(p,s)` at `:1832`, not a bit-width spelling — and nothing in the
grammar establishes that its precision counts bits. `FLOAT(16)` therefore
resolves to `Float`, exactly as today. ADR 0049 makes that gqlc's settled
decision rather than one pending an ISO citation: the precision is dropped, and
gqlc does not decide whether it counts bits or decimal digits.

The length/character/decimal-digit parenthetical on other branches
(`VARCHAR(255)`, `STRING(10)`, `DECIMAL(p,s)`, `CHAR(4)`) is a qualifier on the
type rather than a spelling of one and is still dropped. That is settled: the
model does not grow a length field, because the qualifier does not change the
generated Go type (owner ruling 2026-10-27, `gqlc-5md` closed as won't-do; it
reopens only if generated code is asked to validate lengths). Value types
outside the supported families are rejected with `ErrUnsupportedType`; the
exhaustive set of rejected constructs is enumerated by the corpus fixtures
under `internal/schema/gql/corpus_area_d2_test.go`, which is the source of
truth an author should check against before adding a new family here.
Time-only, byte-string and duration types are supported (bead `gqlc-h9n.4`):
see the widening below.

### Widening the qualifier-drop class (bead `gqlc-h9n.4`)

Prior to `gqlc-h9n.4` this ADR's qualifier-drop clause covered only *magnitude*
qualifiers — length, character, decimal-digit — where the parenthetical is a
scalar bound on how much the value carries. `gqlc-h9n.4` extends the class to
one *field-selector* qualifier: `DURATION(YEAR TO MONTH)` and
`DURATION(DAY TO SECOND)` both resolve to a single `TypeDuration`, with the
qualifier discarded.

This is a deliberate widening, not the existing clause read broadly. The
grounds are:

- The neo4j driver represents duration as one `dbtype.Duration` struct
  (`Months`, `Days`, `Seconds`, `Nanos`); the two ISO qualifiers correspond to
  populating disjoint subsets of those fields at write time. There is no
  distinct `YearMonthDuration` type to project onto, and the driver's
  `PropertyValue` constraint carries a single `Duration` slot.
- No emission is observably different between the two spellings — the codegen
  arm returns `dbtype.Duration` for either, and the schema author's chosen
  spelling would be discarded downstream regardless.
- **Fidelity limit:** the schema-side write constraint (which fields of the
  carrier a value may populate) is not preserved. A schema declaring
  `DURATION(YEAR TO MONTH)` and one declaring `DURATION(DAY TO SECOND)` are
  indistinguishable at every stage after this ADR's normalisation, exactly as
  `CHAR(4)` and `CHAR(8)` are today. That is settled: both
  qualifiers generate the same Go type, so the model does not grow a
  duration-qualifier field (owner ruling 2026-10-27, `gqlc-8pe` closed as
  won't-do; it reopens only if generated code is asked to validate which
  duration fields a value populates).

Byte-string types (`BYTES(n)`, `BINARY(n)`, `VARBINARY(n)`) reach a single
`TypeBytes` under the pre-existing magnitude-qualifier arm — no widening
required for them.

**ADR 0002 is amended, not superseded.** Future extensions to the class stay
visible: name the new qualifier kind, state which Go carrier absorbs it, and
state the fidelity limit.

## Consequences

`Int128/Int256/Uint128/Uint256` and `Decimal` have no native Go type. The parser
records them faithfully; choosing a Go representation (e.g. `math/big`) is left
to the codegen stage. The enum is correspondingly large, and the normalisation
table from grammar spellings to enum constants must be kept in sync with the
grammar.

- **Time-only, byte-string and duration are now supported** (bead
  `gqlc-h9n.4`). `TIME WITH TIME ZONE` / `ZONED TIME` resolve to `TypeTime`
  (Go: `dbtype.Time`); `TIME WITHOUT TIME ZONE` / `LOCAL TIME` resolve to
  `TypeLocalTime` (Go: `dbtype.LocalTime`); `BYTES(n)` / `BINARY(n)` /
  `VARBINARY(n)` resolve to `TypeBytes` (Go: `[]byte`); `DURATION(YEAR TO
  MONTH)` and `DURATION(DAY TO SECOND)` both resolve to `TypeDuration` (Go:
  `dbtype.Duration`) with the qualifier collapse noted above. `SIGNED?` /
  `UNSIGNED` prefixes on `verboseBinaryExactNumericType` map to the same
  width as the corresponding bare or `U`-prefixed spelling.
