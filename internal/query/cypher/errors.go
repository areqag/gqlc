package cypher

import "errors"

// The five sentinels Parse returns for valid openCypher that affects the type
// interface but the current model cannot faithfully represent (spec §3/B3).
// They are category-grained, not per-construct: when a later stage supports a
// construct we delete one Enter* handler, never rename a sentinel. Each is
// wrapped with text naming the offending construct at the failing site, so
// callers branch with errors.Is while reading a concrete message. A
// sentinel-reachability sweep (parser_test.go) guards the set. Stage 6 retired
// ErrUnsupportedProjection: rich scalar expressions at RETURN / WITH position
// now parse to an ExprProjection, so the sentinel has no fail-site.
var (
	// ErrUnsupportedClause rejects clauses outside the read core: the write
	// clauses (CREATE/MERGE/SET/DELETE/REMOVE), UNWIND, CALL. (WITH and UNION are
	// supported as of Stage 4.)
	ErrUnsupportedClause = errors.New("unsupported clause")

	// ErrUnsupportedPattern rejects pattern shapes the model cannot carry yet:
	// multi-type relationships, variable-length paths, and named paths.
	ErrUnsupportedPattern = errors.New("unsupported pattern")

	// ErrUnsupportedParameter rejects a parameter that cannot be bound to a
	// binding property or a clause slot: SKIP/LIMIT $n non-bare, bare-predicate
	// params that mine can't approve, and params nested in lists or maps that
	// escape the mining sweep. Stage 6 accepts a $p inside a rich projection or
	// WHERE expression as an ExprUse on the parameter, so this sentinel no
	// longer fires on those shapes.
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
