package resolver

import "errors"

// Sentinels returned by Resolve when the query is inconsistent with the schema
// or falls outside the R0 capability scope. They are package-level values so
// callers can branch with errors.Is; fail-sites wrap them with detail via
// fmt.Errorf("%w: %s", …) — the schema/gql and cypher-parser convention (ADR
// 0009).
var (
	// ErrUnknownLabel is returned when a node binding's label set does not
	// resolve to a declared node type in the schema.
	ErrUnknownLabel = errors.New("unknown label")

	// ErrUnknownProperty is returned when a property reference — a property
	// projection or an inline-map parameter use — names a property the
	// resolved node type does not declare.
	ErrUnknownProperty = errors.New("unknown property")

	// ErrOutOfR0Scope is returned when the query contains a construct the R0
	// capability scope does not support (edges, multiple bindings, non-Ref
	// projections, WITH, UNION, writes, CALL, RETURN DISTINCT, RETURN *,
	// parameters outside an inline node property map, more than one Use per
	// Parameter). Its fail-sites retire stage by stage as R1..R7 introduce
	// the constructs.
	ErrOutOfR0Scope = errors.New("query construct not supported at resolver stage R0")
)

// allSentinels is the canonical closed set of sentinels the R0 resolver may
// return, kept in one place so TestSentinelReachability can sweep it against
// the invalid-fixture map. A sentinel added here must be paired with at least
// one negative fixture; a retired one must be dropped from both.
var allSentinels = []error{
	ErrUnknownLabel,
	ErrUnknownProperty,
	ErrOutOfR0Scope,
}
