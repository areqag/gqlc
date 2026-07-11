package queryfile

import "errors"

// Sentinels returned by Parse when the query file violates the annotation
// grammar (§4.1 of docs/specs/codegen-stage-c0.md). Package-level values so
// callers branch with errors.Is; fail-sites wrap them with detail via
// fmt.Errorf("%w: line %d: %s", …) — the schema/gql and resolver
// convention.
var (
	// ErrMissingAnnotation is returned when a query file contains a query
	// body with no preceding annotation, or an annotation line at end-of-
	// file with no body.
	ErrMissingAnnotation = errors.New("missing query annotation")

	// ErrUnknownCardinality is returned when an annotation's cardinality
	// token is not one of :one, :many, :exec. :iter is reserved for
	// post-v1 (ADR 0010 D8, gqlc-1a5) and currently fails here.
	ErrUnknownCardinality = errors.New("unknown cardinality")

	// ErrInvalidQueryName is returned when an annotation's name token is
	// not a valid exported Go identifier (^[A-Z][A-Za-z0-9]*$). The
	// generator maps names to method identifiers verbatim (ADR 0010 D2
	// Q5.1), so validation lands here, not in codegen.
	ErrInvalidQueryName = errors.New("invalid query name")

	// ErrDuplicateQueryName is returned when two annotations in one query
	// file share a name. Cross-file collisions are codegen's job — the
	// batch-level check reuses a codegen-side sentinel, not this one.
	ErrDuplicateQueryName = errors.New("duplicate query name")

	// ErrMalformedAnnotation is returned when a line begins with the
	// annotation prefix ("// name:") but does not conform to the grammar
	// (missing cardinality token, name/token pair fails to lex, etc.).
	ErrMalformedAnnotation = errors.New("malformed query annotation")

	// ErrTextBeforeAnnotation is returned when a query file has query text
	// (non-comment, non-blank) before its first annotation. File-leading
	// comment-and-blank content is permitted.
	ErrTextBeforeAnnotation = errors.New("query text before first annotation")

	// ErrNoQueries is returned when a query file's parse yielded zero
	// AnnotatedQueries. An empty query file is a bug per ADR 0010 D1's
	// reject-don't-guess posture — a legitimately empty file should just
	// not be listed in Input.Queries.
	ErrNoQueries = errors.New("no queries in query file")
)

// allSentinels is the canonical closed set of sentinels the queryfile
// parser may return, kept in one place so TestSentinelReachability can
// sweep it against the invalid-fixture map. A sentinel added here must be
// paired with at least one negative fixture; a retired one must be dropped
// from both.
var allSentinels = []error{
	ErrMissingAnnotation,
	ErrUnknownCardinality,
	ErrInvalidQueryName,
	ErrDuplicateQueryName,
	ErrMalformedAnnotation,
	ErrTextBeforeAnnotation,
	ErrNoQueries,
}

// AllSentinels returns a copy of the queryfile package's user-input-
// reachable sentinels. Exported for cross-package harnesses (e.g.
// codegen's fixture loader) that need to map fully-qualified sentinel
// names back to values. Callers must not rely on ordering — the slice
// is copy-returned so a mutation cannot leak into the canonical set.
func AllSentinels() []error {
	out := make([]error, len(allSentinels))
	copy(out, allSentinels)
	return out
}
