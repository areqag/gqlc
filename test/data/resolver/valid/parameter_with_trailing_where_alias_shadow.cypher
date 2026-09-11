// gqlc-dvd1 / ruling-4w5 §6 row S1 — this is FINDING-9's fixture
// (docs/specs/model-change-fvo-use-part.md §7.6.1).
//
// The admit below is a KNOWN RESIDUAL, NOT the target state. `a` is re-aliased
// to `a.title`, a STRING, so Cypher evaluates `a.id` as a property access on a
// string and Neo4j refuses the query. Master committed $p :: property:INT — the
// type read off the PRE-projection binding, which is confidently wrong. Post-swap
// $p is `unknown`: the false type is removed, the admit is not. It must NOT be
// INT again.
//
// Making this REFUSE needs a seventh partScope lane for the carried non-entity
// type, which contradicts the documented §2.3 invariant #3 in scope.go ("carry-
// only lanes and callTypes are NOT observable through partScope"). Ruling §5.1
// prices it and deliberately leaves it out; it is a separate bead, not a rider.
MATCH (a:Post) WITH a.title AS a WHERE a.id = $p RETURN a
