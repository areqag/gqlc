# Query parser is built test-first; the query model evolves until feature-complete, then locks

The Cypher implementation of `query.Parser` is built test-first against a corpus
of standard openCypher queries (the openCypher TCK), growing support one
capability at a time. We start on the current `query.Query` model — which is
complete for the read core and nothing more — and let the corpus drive what we
add next. The model is **deliberately unlocked** for the whole of this build and
will change shape repeatedly; it is frozen only once the parser is
feature-complete, and **no consumer** (resolver, codegen, generated driver) is
written until after that freeze.

## Context

`query.Query` today (ADR 0003) faithfully represents a single read query —
`MATCH … WHERE … RETURN var`/`var.prop`, with parameters traced to binding
properties — and nothing beyond it. Supporting the full readable openCypher
surface (pagination parameters, `OPTIONAL MATCH`, projections/aggregations,
`WITH`, `UNION`) requires four distinct model capabilities the current shape
cannot express. We weighed designing that full model up front against evolving it
incrementally.

## Considered options

**Design the whole model up front, then build once.** Tempting because the
endpoint is known (the full readable surface), so this is not speculative
generality. Rejected: knowing the *requirement* is not knowing the
*representation*. How `ReturnItem` should hold an aggregate, or how `Query`
should hold UNION branches, is decided by the consumers (resolver, codegen) and
by the real corpus — neither of which exists yet. An up-front lock is a lock on a
guess; when the guess proves wrong it must be reopened *with the resolver already
attached to it*, which is the exact downstream cascade the lock was meant to
prevent. A large wrong model is also harder to change than a small concrete one.

**Evolve the model incrementally, test-first, and lock at feature-complete.**
Chosen. While the parser is under construction the only consumer of `query.Query`
is the parser itself and its golden files (regenerated mechanically), so model
churn is cheap *now* and only becomes expensive once a resolver exists. Locking
before consumers exist — not before the corpus is understood — is the
anti-cascade guarantee, and it lets the corpus dictate the minimal honest shape
of each addition (curation, per ADR 0003).

## Decision

- **Test-first.** A golden-file suite (mirroring `internal/schema/gql`'s
  valid/invalid fixtures + sentinel-reachability sweep) is written with each
  capability, before the capability is implemented. Fixtures are drawn from the
  openCypher TCK (`github.com/opencypher/openCypher`,
  `tck/features/{clauses,expressions,useCases}`), whose `.feature` files carry the
  query, its parameters, and its expected result or error. The corpus need not
  match any schema we ship — parsing is schema-agnostic (ADR 0003).
- **Incremental, corpus-driven.** Support grows one capability at a time; each
  capability is a TDD slice that may change the model. We do not pre-build the
  endpoint.
- **Model unlocked until feature-complete.** The query model is explicitly
  provisional. ADR 0003's "stable contract" applies *from the freeze onward*, not
  during this build.
- **Freeze, then consumers.** When the parser parses the targeted corpus, we
  freeze the model, revise ADR 0003 and CONTEXT.md to the final shape, and only
  then build the resolver, codegen, and generated driver code — each written once
  against a stable model.

## Stages to feature-complete

Each stage is a TDD slice; stages 1–5 evolve the model.

0. **Read core** (current model, unchanged) — one or more `MATCH`, optional
   `WHERE` (property comparisons and string predicates, mined for parameter
   uses), `RETURN` of `var` or `var.prop` with optional `AS`. Includes the
   type-irrelevant cosmetics that need no model change: `ORDER BY`, `DISTINCT`,
   and `SKIP`/`LIMIT` with literal arguments.
1. **Non-property-typed parameters** — a parameter whose type comes from its
   position rather than a binding property (e.g. `SKIP $off`, `LIMIT $lim`).
2. **Nullability** — `OPTIONAL MATCH` makes its bindings and the columns derived
   from them nullable; the model must carry that flag for type-safe codegen.
3. **Projections & aggregations** — result/intermediate values that are
   expressions, aggregates (`count`, `sum`, `collect`, …), literals, function
   calls, and `RETURN *`.
4. **Multi-scope & UNION** — `WITH` chaining and `UNION` branches; `Query` gains
   part/branch structure. Depends on stage 3, since `WITH` projects exactly the
   values stage 3 introduces.
5. **Undirected patterns** — `(a)-[r]-(b)` matches in either direction; the edge
   binding gains a direction-agnostic marker, and resolution tries both
   orientations against the directed schema. Rejected outright in Stage 0 (the
   schema is directed-only). Ordering among stages 1–5 may be resequenced as the
   corpus dictates.

Out of scope throughout: write clauses (`CREATE`/`MERGE`/`SET`/`DELETE`/
`REMOVE`), variable-length paths, and the full predicate tree beyond what
parameter and edge-endpoint extraction need (ADR 0003).

## Consequences

The query model changes shape across several commits; reviewers and any early
readers must treat it as provisional until the freeze ADR lands, and must not
build against it before then. The golden suite absorbs most of the churn via
`-update`. Because the corpus is Gherkin `.feature` files, an extraction step
lifts the Cypher query strings (and their declared parameters and expected
errors) into our fixture format.
