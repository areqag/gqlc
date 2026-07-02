package cypher

import "errors"

// The five sentinels Parse returns for valid openCypher that affects the type
// interface but the current model cannot faithfully represent (spec §3/B3), or
// for a bucket-1 parse-shape rejection the type-interface boundary owns. They
// are category-grained, not per-construct: when a later stage supports a
// construct we delete one Enter* handler, never rename a sentinel. Each is
// wrapped with text naming the offending construct at the failing site, so
// callers branch with errors.Is while reading a concrete message. A
// sentinel-reachability sweep (parser_test.go) guards the set. Stage 6 retired
// ErrUnsupportedProjection: rich scalar expressions at RETURN / WITH position
// now parse to an ExprProjection, so the sentinel has no fail-site. Stage 8
// retired ErrUnsupportedPattern: the three pattern shapes it flagged (named
// paths, variable-length relationships, multi-type relationships) all parse
// under the widened Stage-8 model. Stage 11 adds ErrPatternInProjection for
// pattern predicates at projection position — a freeze-durable true-rejection.
var (
	// ErrUnsupportedClause rejects clauses outside the read core: the write
	// clauses (CREATE/MERGE/SET/DELETE/REMOVE), UNWIND, CALL. (WITH and UNION are
	// supported as of Stage 4.)
	ErrUnsupportedClause = errors.New("unsupported clause")

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
	// an edge (or as a path). Stage 8 extends the check to the three-way kind
	// space (node/edge/path).
	ErrVariableKindConflict = errors.New("variable used as both node and edge")

	// ErrPatternInProjection rejects a pattern predicate at RETURN or WITH
	// projection position — a bucket-1 parse-shape rule (ADR 0007 §I). Pattern
	// predicates are legal openCypher only inside a boolean position
	// (WHERE / EXISTS / rich-expression predicate); using one as a scalar
	// column (MATCH (n) RETURN (n)-[]->()) is SyntaxError:UnexpectedSyntax per
	// the TCK (Pattern1 [22]/[23]). Stage 11 adds the fail-site; the sentinel
	// is freeze-durable — pattern predicates never become legal projection
	// atoms.
	ErrPatternInProjection = errors.New("pattern predicate in projection position")
)
