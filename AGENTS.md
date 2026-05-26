# AGENTS.md

Conventions for working in this repo. gqlc is an sqlc analogue for graph query
languages: it parses GQL/Cypher schemas and queries and generates type-safe Go.

See [CONTEXT.md](./CONTEXT.md) for the domain glossary and `docs/adr/` for
recorded decisions.

## Comments

Before writing a comment, ask: **is this comment useful?** A comment earns its
place by carrying context that is hard to recover from the code itself — a
non-obvious *why*, an invariant, a reference to the spec or grammar, a warning
about a sharp edge. A comment that restates what a name or the code already says
is noise; delete it.

- Explain *why*, not *what*. The code already says what.
- No comments that paraphrase the function name or the next line.
- No narration of changes, TODO dumps, or LLM scaffolding left in place.
- No abstraction leaks. Describe what a thing *is* and its contract — never
  justify it by contrasting rejected designs ("deliberately not an AST",
  "rather than collapsing to X") or by appealing to other layers ("so codegen
  can…", "which is why the resolver…"). That rationale lives in an ADR; a comment
  may *cite* one ("(ADR 0003)") but must not restate it. The code should read the
  same whether or not the reader knows which alternatives were rejected.
- Prefer a clearer name over a comment that explains a bad one.

## Tests

- Use **testify** (`suite.Suite` for setup/grouping, `require` for fail-fast
  assertions). Not bare `testing.T`.
- **Table-driven.** Each case is a named row; the loop is the test body.
- High signal, no fluff: no asserting the obvious, no dead helpers, no cases
  that duplicate each other's coverage. A reader should see what each case
  proves at a glance.
- **Golden files** for parsed results: a `.golden.json` beside each `.gql`
  fixture, regenerated with `go test -update`. The serialisation is the domain
  model's own deterministic `schema.Schema.MarshalJSON` (maps sorted into stable
  slices), so one canonical form serves golden tests today and generated output
  later — calls to the tool stay idempotent.
- Schema fixtures live in `test/data/schema/gql/`. Prefer real GQL schemas
  validated against the grammar; supplement with hand-authored files. Negative
  fixtures pair a `.gql` with the sentinel error it must produce.

## Conventions

- **Parse-tree walking:** use the ANTLR **listener** (`ParseTreeWalker` +
  `BaseGQLListener`), overriding only the `Enter*` rules you need. Do not
  hand-roll tree traversal or use the visitor (see ADR 0001).
- **Errors:** unsupported/invalid constructs are package-level **sentinels**
  matched with `errors.Is`. Syntax errors come from a custom
  `antlr.ErrorListener`, not `panic`/`recover`. Parsing fails fast on the first
  error.
- **Generated code:** never edit `internal/grammar/**/gen`. Regenerate it with
  `just build-grammar` after changing a `.g4` grammar.
- **Make illegal states unrepresentable.** Encode invariants in types so
  invalid combinations cannot be constructed at all; prefer a shape the
  compiler enforces over a runtime check or a comment warning the next reader.
  Go techniques:
  - **Closed sum types** — an interface with an unexported marker method
    makes the sum exhaustive: only types in the same package can implement it,
    so callers cannot smuggle in a third variant and the compiler sees every
    case.
  - **Unexported fields + a `New*` constructor** — when an invariant cannot
    live in the types alone (a string that must be non-empty, fields that
    must both be set), hide the fields so the constructor is the only way to
    obtain a non-zero value, and validate the remaining invariants once
    there.
  - **A type per variant, not flags on one struct** — split shapes that
    share fields rather than carrying optional fields and a runtime
    discriminator the compiler cannot see.
  - **A shape that excludes the bad case** — pick the representation that
    makes the invalid value unwriteable (a scalar instead of a slice when
    only one is meaningful; canonicalising on construction so a
    non-canonical form cannot exist; enums over free strings).
