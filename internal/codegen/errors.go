package codegen

import "errors"

// Sentinels returned by Generate. Package-level values so callers branch
// with errors.Is; fail-sites wrap them with detail (fmt.Errorf("%w:
// derived package %q", ErrInvalidPackageName, name)) — the schema/gql
// convention.
var (
	// ErrInvalidPackageName is returned when Schema.Name's lowercase
	// mangle does not produce a valid Go package identifier (empty,
	// non-ASCII, digit-leading, contains punctuation other than
	// underscore).
	ErrInvalidPackageName = errors.New("invalid package name")

	// ErrDuplicateSourceFile is returned when two NamedQuery entries in
	// one Input carry SourceFile values whose basenames collide. C0
	// emits no per-source file, but the check runs uniformly regardless
	// of stage — a fixture that fires this at C0 stays firing it at C5.
	ErrDuplicateSourceFile = errors.New("duplicate query file basename")

	// ErrDuplicateQueryName is returned when two NamedQuery entries in
	// one Input share a Name (a cross-file collision the queryfile
	// front end cannot see because it works one file at a time). Same
	// sentinel value as queryfile.ErrDuplicateQueryName is deliberately
	// NOT reused — errors.Is walks separately per package, and the
	// batch-level check is a codegen-owned concern with its own
	// reachability sweep.
	ErrDuplicateQueryName = errors.New("duplicate query name in batch")

	// ErrInvalidCardinality is returned when a NamedQuery's Cardinality
	// field is the zero value — a caller bug the front end never
	// produces. Present so a hand-constructed NamedQuery slipping past
	// the front end fails at generation, not silently.
	ErrInvalidCardinality = errors.New("invalid cardinality")

	// ErrFormatFailure is returned when go/format.Source rejects an
	// emitted file's raw contents. A template bug — unreachable via any
	// legitimate fixture — but wrapped-and-named beats a bare error
	// string when it does fire. Deliberately excluded from allSentinels
	// because it is a codegen-internal invariant violation, not a
	// user-facing failure mode; the reachability sweep skips it.
	ErrFormatFailure = errors.New("format failure")

	// ErrOutOfC2Scope is returned when a C2-admissible input carries a
	// construct C2 does not project: a column whose resolved type is
	// ResolvedEdgeUnion (C5) or ResolvedScalar / ResolvedTemporal /
	// ResolvedList / ResolvedUnknown (C3), a ResolvedProperty column or
	// parameter with an unrepresentable width or a temporal property type
	// (C3), a non-property parameter (C3), a :exec cardinality (C4), or a
	// query text carrying a raw-string-hostile backtick. Category-grained
	// per C0's precedent; C3/C4/C5 retire the sub-cases as they land.
	// Renamed from ErrOutOfC1Scope at C2 — the entity-column axis retired.
	ErrOutOfC2Scope = errors.New("out of C2 scope")

	// ErrParamNameCollision is returned when two Parameters mangle to
	// the same Params-struct field name (§4.2). The fail-message names
	// both parameter positions. Introduced at C1.
	ErrParamNameCollision = errors.New("parameter name collision")

	// ErrRowFieldCollision is returned when two Columns derive to the
	// same Row-struct field name (§4.3). The fail-message names both
	// column positions and prompts an explicit AS alias. Introduced at
	// C1.
	ErrRowFieldCollision = errors.New("row field name collision")

	// ErrAliasRequired is returned when a Column's Name matches neither
	// the bare-identifier shape nor the property-access shape (§4.3),
	// so the row-field name cannot be derived deterministically. The
	// fail-message names the column and prompts an explicit AS alias.
	// Introduced at C1.
	ErrAliasRequired = errors.New("alias required")

	// ErrIdentifierCollision is returned when two generated top-level
	// identifiers in one package collide (§4.4 / §4.6), or a query's
	// method name matches a reserved identifier the emission owns
	// (§4.1). C2 adds entity struct names to the swept identifier set.
	// The fail-message names both identifier sources. C5 hardens the
	// sweep further as decode-helper names enter the exported surface.
	// Introduced at C1; C2 widens.
	ErrIdentifierCollision = errors.New("identifier collision")

	// ErrInvalidEntityName is returned when an explicit NodeType.Name or
	// EdgeType.Name is set but is not a valid exported Go identifier
	// (spec §4.5 Rule 1), or when a single-label mangle (Rule 2 / Rule
	// 3) produces text that fails the exported-Go-identifier grammar.
	// The fail-message names the schema type (labels for a node,
	// edge-key triple for an edge) and the offending string. Introduced
	// at C2.
	ErrInvalidEntityName = errors.New("invalid entity name")

	// ErrUnnamedMultiLabelType is returned when a multi-label node type,
	// a multi-label edge type, or a single-label edge type whose Label
	// is shared across endpoint pairs, has an empty NodeType.Name /
	// EdgeType.Name — Rule 4 requires an explicit name to avoid
	// guessing. The fail-message names the schema type and the axis that
	// made it ambiguous. Checked eagerly regardless of query projection.
	// Introduced at C2.
	ErrUnnamedMultiLabelType = errors.New("unnamed multi-label type")

	// ErrPropertyFieldCollision is returned when two properties on the
	// same entity mangle to the same struct field name (spec §4.5 Rule
	// 5). The fail-message names both properties and the entity.
	// Introduced at C2.
	ErrPropertyFieldCollision = errors.New("property field collision")
)

// allSentinels is the canonical closed set of user-input-reachable
// sentinels Generate may return, kept in one place so
// TestSentinelReachability can sweep it against the invalid-fixture
// map. A sentinel added here must be paired with at least one negative
// fixture; a retired one must be dropped from both.
//
// ErrFormatFailure is intentionally excluded: it is defensive-only,
// unreachable via any legitimate fixture (well-formed emission cannot
// fail formatting), so a fixture that fires it would require synthetic
// template corruption — a test seam whose value does not pay for its
// cost. See spec §9.2.
var allSentinels = []error{
	ErrInvalidPackageName,
	ErrDuplicateSourceFile,
	ErrDuplicateQueryName,
	ErrInvalidCardinality,
	ErrOutOfC2Scope,
	ErrParamNameCollision,
	ErrRowFieldCollision,
	ErrAliasRequired,
	ErrIdentifierCollision,
	ErrInvalidEntityName,
	ErrUnnamedMultiLabelType,
	ErrPropertyFieldCollision,
}
