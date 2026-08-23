# ADR 0022 — Plural label satisfaction: property intersection and whole-entity rejection

**Status:** Accepted
**Date:** 2026-07-28
**Bead:** gqlc-h9n.22

## Context

A query label expression `(:A)` is satisfied by every declared node type whose
complete label set is a superset of `{A}`. After gqlc-h9n.7 the satisfying set
could be plural, and step 1 (gqlc-h9n.7) refused any plural result with
ErrAmbiguousLabel, leaving a provisional tie-break: if the identity of a
declared type equals the queried label set, it resolved outright as singular.

The bead exposing this as provisional was gqlc-h9n.22. Its NOTES enumerate two
answers:

**(A) Keep precedence.** An exact-identity hit resolves singular; plural only
runs on a miss. The existing fixture `label_satisfy_exact_wins.cypher` encoded
this.

**(B) Satisfaction is satisfaction (ISO 39075 §16.8).** Every type whose
complete label set is a superset of the queried set satisfies it, including
types whose identity equals it. The satisfying set is computed without a
fast-path and may include both.

Answer (A) has a named soundness hazard: a `MATCH (p:Person)` that resolves to
the bare `Person` type via exact precedence can produce generated Go code with a
typed field that has nothing behind it at runtime, when the matched node happens
to be labelled `{Person, Employee}` — the Person struct's field has no driver
value to fill it. The bead describes this as "a declared field with nothing
behind it", and it is the stronger argument for (B).

## Decision

Adopt **(B)**. The exact-match fast path is removed from `resolveNodeLabels`.
The satisfying set is always the complete superset closure.

Where that set is plural:

- A **property projection** (`p.name`) resolves to the **intersection**: the
  property must exist with identical type and nullability on every satisfying
  type, else ErrUnknownProperty. This mirrors `unionProperty` for edge unions.
- A **whole-entity reference** (`RETURN p`) is **rejected** with ErrAmbiguousLabel.
  *Amendment 2026-08-23 (bead gqlc-yxqq):* that sentinel is this ADR's case —
  a binding that spelled labels. Phase B's plural commitment (ADR 0022 as
  applied by PR #1032) reaches the same whole-entity refusal from a binding
  that spelled none, and there the sentinel is `ErrAmbiguousBinding`, which
  `internal/resolver/errors.go` already documented as the unlabelled case.
  Both are refusals; only the category-grained name differs.

(B) narrows one accepted query (`label_satisfy_exact_wins`) to a rejected one.
ADR 0006's caution about narrowing applies; it is overridden here because the
accepted query had a soundness defect: it let generated code name a field that
has no runtime backing on a legal runtime node shape. Relaxing later
(allowing intersection structs, codegen union structs, or an explicit
disambiguation syntax) is non-breaking; shipping the unsound code is not.

## Consequences

- The former `valid/label_satisfy_exact_wins.cypher` becomes invalid.
- Property-access queries on a label used by multiple declared types require
  the property to be present on all of them with identical type and nullability.
- Whole-entity references on a plural satisfying set are refused; the schema
  author must use the full conjunctive label set to name the exact type.
- Edge endpoint resolution becomes a cross-product over the satisfying node
  type sets of both endpoints. Schemas are small; naive iteration is deliberate
  (see `labelDeclared`'s comment).

> **Amendment (2026-08-04, gqlc-0tft).** "Plural" is decided after edge
> closure, not at label satisfaction. Closing an edge names a node type on each
> end of the pattern, so a binding at one of those ends is narrowed by the
> closure the same way an unlabelled one is by Phase B — union across the
> candidate set's two readings per touching edge, intersect across touching
> edges (resolver R3 spec §4.6.2). Where that leaves one type the binding is
> singular and this ADR's whole-entity refusal does not fire; where it leaves
> several, or where the intersection is empty, every verdict above stands
> unchanged on the surviving set. The relaxation this ADR names as non-breaking
> is the direction taken here: no query this ADR accepted is refused.
