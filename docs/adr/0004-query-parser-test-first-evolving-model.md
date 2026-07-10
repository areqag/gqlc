# Query parser is built test-first; the query model evolves with the openCypher corpus

The Cypher implementation of `query.Parser` is built test-first against a corpus
of standard openCypher queries (the openCypher TCK), growing support one
capability at a time. We start on the current `query.Query` model — which is
complete for the read core and nothing more — and let the corpus drive what we
add next. The model changes shape as capabilities are added; **consumers**
(resolver, codegen, generated driver) are built once the parser expresses the
queries they target, so that model churn stays cheap while there is no
downstream code yet.

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
by the real corpus — neither of which exists yet. Committing to a shape up
front commits to a guess; revising when the guess proves wrong is expensive
*with the resolver already attached to it*, which is the downstream cascade
we want to defer. A large wrong model is also harder to change than a small
concrete one.

**Evolve the model incrementally, test-first.** Chosen. While the parser is
under construction the only consumer of `query.Query` is the parser itself
and its golden files (regenerated mechanically), so model churn is cheap
*now* and only becomes expensive once a resolver exists. Deferring consumer
construction until the corpus is understood is the anti-cascade guarantee,
and it lets the corpus dictate the minimal honest shape of each addition
(curation, per ADR 0003).

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
- **Model provisional through the build.** The query model changes shape as
  capabilities are added; ADR 0003's "stable contract" framing describes the
  parser-to-resolver interface once the parser expresses the queries the
  resolver targets, not the middle of this build.
- **Consumers come when the parser is ready.** When the parser parses the
  targeted corpus, we build the resolver, then codegen, then generated driver
  code. Consumers are updated when the model changes — that's normal
  coupling between coupled internal packages, and the churn is bounded by
  keeping the parser the only consumer for as long as possible.

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

> _Note (ADR 0007): the feature-complete target expands from the read core
> (Stages 0–5, now complete) to the full openCypher surface. Nine further
> stages 6–14 extend the model — expressions and result types, temporals, the
> full pattern surface, remaining read clauses, aggregations, quantifier and
> existential predicates, writes, `MERGE`, and `CALL` with a procedure
> registry. The "out of scope throughout" paragraph above no longer applies;
> every construct it names is now in scope. Build discipline (test-first,
> one capability per slice, model provisional through the build) is
> unchanged._

## Consequences

The query model changes shape across several commits; reviewers and any early
readers must treat it as provisional through the parser build, and consumers
open only when the parser expresses the queries they target. The golden
suite absorbs most of the churn via
`-update`. Because the corpus is Gherkin `.feature` files, an extraction step
lifts the Cypher query strings (and their declared parameters and expected
errors) into our fixture format.
