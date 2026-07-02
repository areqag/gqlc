# Nullability is parsed as a static introduction fact; flow-typing belongs to the resolver

The Cypher query parser marks a `Binding` as `Nullable` iff its variable is
first introduced inside an `OPTIONAL MATCH` clause. The flag is a **static,
local fact** about where the binding was introduced; the parser never demotes
it based on downstream clauses, even when downstream clauses prove the binding
must exist (a required `MATCH` referencing it, a required edge whose endpoints
include it, etc.). Refining nullability across clause structure —
**flow-typing** — is reserved for the **resolver** (ADR 0003): the post-freeze
stage that takes `(query.Query, schema.Schema)` and produces a validated query.

## Context

`OPTIONAL MATCH` makes its bindings nullable in the result. But openCypher
semantics frequently make those bindings effectively non-null downstream — a
required `MATCH (b)-[:R]->(c)` filters out rows where `b` is NULL, so by the
time `b` reaches `RETURN`, it cannot be NULL. The question is whether the
parser should compute that, or just record the introduction site and let a
later stage refine.

## Considered options

**Flow-type in the parser.** The parser walks clauses in order and demotes a
binding's `Nullable` flag when a later required clause proves the binding must
exist. Generated API matches the semantics exactly. Rejected: the analysis is
dataflow over clause structure, which is exactly the kind of predicate /
clause structure ADR 0003 curates *out* of the model. Doing it in the parser
either drags that structure into the model now (contradicting ADR 0003) or
hides it inside the parser as throwaway intermediate state that the model
cannot represent (a worse outcome — the answer becomes unreviewable post-hoc).
It also bundles two distinct capabilities into Stage 2, breaking ADR 0004's
one-capability-per-slice rule.

**Conservative in the parser, flow-typing in the resolver.** Chosen. The
parser records the introduction fact via `Nullable() bool` on the binding —
the minimum information needed to preserve the OPTIONAL/required distinction
across the parser-to-resolver boundary. The resolver, which already exists in
ADR 0003 as the schema-aware analysis stage and in CONTEXT.md as the producer
of the **validated query**, is the natural home for any subsequent
refinement. Schema- and structure-aware reasoning is its purpose.

## Consequences

- **Codegen is conservative until flow-typing lands.** A binding that is
  visibly non-null at `RETURN` is still emitted as nullable in generated code.
  This is correct but pessimistic. Relaxing the type later (nullable →
  non-null) is a non-breaking refinement; the reverse would not be — so the
  conservative default is the safe starting position.
- **Two flow-typing regimes, with different model dependencies.** Some cases
  are flow-typable with the Stage 2 model alone — a non-nullable edge implies
  its endpoints exist, so the resolver can chain "non-nullable edge →
  non-nullable endpoints" without any new model structure. Other cases (a
  bare `MATCH (b)` reusing an OPTIONAL-introduced binding) require per-clause
  structure to record the second reference; that structure is what Stage 4
  (`WITH` / `UNION`) introduces.
- **No commitment to when flow-typing ships.** This ADR commits only to the
  parser side and to the resolver as the right home. Whether or when the
  resolver actually implements flow-typing — and which subset of cases it
  covers — is left to the resolver's own work. The Stage 2 model commits
  only to *not destroying the information the resolver would need*.
- **Resolver API shape is deferred.** The resolver does not exist as code
  yet (ADR 0004's freeze gate). Its API will be specified in the freeze ADR
  that revises ADR 0003 at parser feature-complete. ADR 0006 only names the
  resolver as the home for flow-typing; it does not pin the resolver's
  package path, constructor signature, or output type.
