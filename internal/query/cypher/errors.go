package cypher

import "errors"

// The six sentinels Parse returns for valid openCypher that affects the type
// interface but Stage 0 cannot faithfully represent (spec §3/B3). They are
// category-grained, not per-construct: when a later stage supports a construct we
// delete one Enter* handler, never rename a sentinel. Each is wrapped with text
// naming the offending construct at the failing site, so callers branch with
// errors.Is while reading a concrete message. A sentinel-reachability sweep
// (parser_test.go) guards the set.
var (
	// ErrUnsupportedClause rejects clauses outside the read core: WITH, UNION,
	// the write clauses (CREATE/MERGE/SET/DELETE/REMOVE), UNWIND, CALL.
	ErrUnsupportedClause = errors.New("unsupported clause")

	// ErrUnsupportedProjection rejects a RETURN item that is not a bare variable or
	// a single-level property lookup: RETURN *, aggregations, function calls,
	// arithmetic, literals, CASE, comprehensions.
	ErrUnsupportedProjection = errors.New("unsupported projection")

	// ErrUnsupportedPattern rejects pattern shapes the model cannot carry yet:
	// multi-type relationships, variable-length paths, named paths, and undirected
	// relationships.
	ErrUnsupportedPattern = errors.New("unsupported pattern")

	// ErrUnsupportedParameter rejects a parameter that cannot be bound to a binding
	// property: SKIP/LIMIT $n, params in arithmetic, bare-predicate params,
	// param-vs-param/literal, IN $p, and params nested in lists or maps.
	ErrUnsupportedParameter = errors.New("unsupported parameter")

	// ErrUnboundVariable rejects a variable that reaches the model — a return item,
	// a parameter use, or an edge endpoint — without a binding in any MATCH. It
	// does not cover a variable that appears only inside an ignored WHERE predicate
	// (e.g. b in WHERE a.x = b.y with no parameter): predicate structure is not
	// modelled (B1), so such a variable is never tracked.
	ErrUnboundVariable = errors.New("unbound variable")

	// ErrVariableKindConflict rejects a variable bound once as a node and once as
	// an edge.
	ErrVariableKindConflict = errors.New("variable used as both node and edge")
)
