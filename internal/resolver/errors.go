package resolver

import "errors"

// Sentinels returned by Resolve when the query is inconsistent with the schema
// or falls outside the R0..R1 capability scope. They are package-level values
// so callers can branch with errors.Is; fail-sites wrap them with detail via
// fmt.Errorf("%w: %s", …) — the schema/gql and cypher-parser convention (ADR
// 0009).
var (
	// ErrUnknownLabel is returned when a node binding's label set does not
	// resolve to a declared node type in the schema, or (at R1) an unlabelled
	// node binding's candidate set from touching edges is empty, or an
	// anonymous inline endpoint carries no labels the resolver can commit.
	ErrUnknownLabel = errors.New("unknown label")

	// ErrUnknownProperty is returned when a property reference — a property
	// projection or an inline-map parameter use — names a property the
	// resolved node or edge type does not declare.
	ErrUnknownProperty = errors.New("unknown property")

	// ErrOutOfR0Scope is returned when the query contains a construct the
	// current capability scope does not support (multi-part, multi-branch,
	// non-Ref projections, WITH, UNION, writes, CALL, RETURN DISTINCT,
	// RETURN *, undirected / var-length / multi-type / untyped edges, path /
	// unwind / call bindings, parameters with more than one Use, non-Property
	// uses). Its fail-sites retire stage by stage as R2..R7 introduce the
	// constructs.
	ErrOutOfR0Scope = errors.New("query construct not supported at resolver stage R0")

	// ErrUnknownEdge is returned when a directed single-hop edge binding's
	// endpoints and label form an EdgeKey the schema does not declare — i.e.,
	// the schema has no edge of that label with that (source, target) pair.
	// Introduced at R1.
	ErrUnknownEdge = errors.New("unknown edge")

	// ErrAmbiguousBinding is returned when an unlabelled node binding cannot
	// be uniquely typed from the edges that touch it: either its candidate
	// set (intersected across touching edges) has more than one node type,
	// or the pattern's unlabelled bindings form a cycle no single edge can
	// break. Introduced at R1.
	ErrAmbiguousBinding = errors.New("ambiguous binding")
)

// allSentinels is the canonical closed set of sentinels the resolver may
// return, kept in one place so TestSentinelReachability can sweep it against
// the invalid-fixture map. A sentinel added here must be paired with at least
// one negative fixture; a retired one must be dropped from both.
var allSentinels = []error{
	ErrUnknownLabel,
	ErrUnknownProperty,
	ErrOutOfR0Scope,
	ErrUnknownEdge,
	ErrAmbiguousBinding,
}
