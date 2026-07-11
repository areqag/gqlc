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
}
