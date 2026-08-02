// The resolver newly admits this. `(:Person)` is satisfied by two declared
// node types (ADR 0022), so the endpoint cross-product closes an undirected
// single-type binding to two FOUNDED keys. Both run person-side to Company —
// no candidate disagrees about which of the pattern's endpoints the edge runs
// from — so R3 §4.6 case C does not fire and case D types them as a union.
//
// Every union that arm can produce carries ONE label on all its members: the
// call site guards on `len(e.Labels()) == 1`. So every one of them is
// unrepresentable here — an edge value carries its label and its properties,
// never its endpoint types, which leaves the emitted dispatch nothing to
// branch on.
//
// The refusal is staged on purpose: the resolver's answer is honest about the
// schema, and codegen is the stage that knows what a Go edge value can carry.
// This fixture records that the newly-admitted class terminates one stage
// later rather than reaching a backend, and it is red from both ends — if the
// resolver goes back to refusing the class, the load fails on the wrong
// sentinel; if codegen's same-label guard goes, generation succeeds.

// name: GetFounded :one
MATCH (p:Person)-[r:FOUNDED]-(c:Company) RETURN r
