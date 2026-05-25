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
