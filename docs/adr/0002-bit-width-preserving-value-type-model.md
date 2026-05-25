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
`BIGINT≈Int64`, `UBIGINT≈Uint64`, `REAL≈Float32`, `DOUBLE≈Float64`); length and
precision qualifiers (`VARCHAR(255)`, `DECIMAL(p,s)`) are dropped. Value types
outside the supported families (reference, list, record, path, time-only,
duration) are rejected with `ErrUnsupportedType`.

## Consequences

`Int128/Int256/Uint128/Uint256` and `Decimal` have no native Go type. The parser
records them faithfully; choosing a Go representation (e.g. `math/big`) is left
to the codegen stage. The enum is correspondingly large, and the normalisation
table from grammar spellings to enum constants must be kept in sync with the
grammar.
