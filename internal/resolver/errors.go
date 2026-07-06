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
	// projection, an inline-map parameter use, or a rich-expression /
	// predicate PropertyUse — names a property the resolved node or edge
	// type does not declare. Message-set widened at R2 to include
	// PropertyUse witnesses from ExprInProjection / ExprInPredicate slots.
	ErrUnknownProperty = errors.New("unknown property")

	// ErrOutOfR0Scope is returned when the query contains a construct the
	// current capability scope does not support (multi-part, multi-branch,
	// AggregateProjection, WITH, UNION, writes, CALL, RETURN DISTINCT,
	// RETURN *, undirected / var-length / multi-type / untyped edges, path /
	// unwind / call bindings, write-side ExprUses (SET / DELETE),
	// ExprProjection typed as list-of-entities). Its fail-sites retire
	// stage by stage as R2..R7 introduce the constructs.
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

	// ErrParameterTypeConflict is returned when a parameter's Uses carry
	// witnesses that do not unify: two PropertyUses whose properties differ
	// in type or nullability, a mixed PropertyUse × ClauseSlotUse against a
	// non-integer property, or an ExprUse whose enclosing type disagrees
	// with a co-occurring PropertyUse. Introduced at R2. See §4.8 for the
	// unification lattice.
	ErrParameterTypeConflict = errors.New("parameter type conflict")

	// ErrAmbiguousEdgeOrientation is returned when an undirected single-type
	// single-hop edge binding's two-orientation trial matches two distinct
	// EdgeKeys against the schema — both {A, L, B} and {B, L, A} are declared
	// as distinct edge types with the same label, and the author's undirected
	// pattern (no `|` union opt-in) cannot commit to one without erasing the
	// other. Introduced at R3. See R3 spec §4.6 verdict-C.
	ErrAmbiguousEdgeOrientation = errors.New("ambiguous edge orientation")

	// ErrUnionColumnMismatch is returned when a UNION query has branches whose
	// result columns disagree on count, on names at the same index, on types
	// at the same index, or on the nullability bit of a same-named column.
	// Introduced at R5. See R5 spec §4.3.
	ErrUnionColumnMismatch = errors.New("union column mismatch")

	// ErrPartBindingTypeConflict is returned when a Part K > 0 re-declares a
	// carried variable with a labelled binding whose schema-typed identity
	// disagrees with the carried type. Concretely: at Part K a labelled
	// NodeBinding for name `v` resolves to a schema.NodeType whose
	// LabelSetKey differs from the carry-seed's LabelSetKey for `v`. Same
	// key = trivial re-binding, admitted. Different key = irreconcilable,
	// rejected. Introduced at R5. See R5 spec §6.4.
	ErrPartBindingTypeConflict = errors.New("part binding type conflict")

	// ErrInvalidEffectTarget is returned when a write clause's target
	// variable is bound at the parser scope but resolves to something
	// other than an entity (node or edge) binding — a projection alias
	// exported by a WITH, a literal, or a scalar-typed carried entry.
	// Concretely: SET / REMOVE / DELETE on a variable that lives in
	// carriedResolvedTypes but not in nodeTypes / edgeTypes / edgeCands.
	// Also fires for SET / REMOVE labels on an edge binding (labels are
	// node-only), for SET / REMOVE / DELETE on a var-length edge property
	// (a var-length binding is a list, not a single edge), and for the
	// defensive tripwire where a write's target variable is not in any
	// Part scope (parser scope check should have caught this; the guard
	// keeps the invariant tight). Introduced at R6. See R6 spec §4.3,
	// §4.4 for the fail-sites.
	ErrInvalidEffectTarget = errors.New("invalid effect target")
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
	ErrParameterTypeConflict,
	ErrAmbiguousEdgeOrientation,
	ErrUnionColumnMismatch,
	ErrPartBindingTypeConflict,
	ErrInvalidEffectTarget,
}
