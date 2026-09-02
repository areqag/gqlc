package codegen

import (
	"errors"
	"slices"
)

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
	//
	// "Unreachable via any legitimate fixture" was false until gqlc-2m2v:
	// a query binding two or more parameters, one of them $_, emitted a
	// nameless Params field and reached gofmt on all three targets. The
	// exclusion is not self-certifying and is no longer taken on trust —
	// TestExcludedBranchesAreUnreached measures this branch against the
	// corpus coverage profile on every run.
	ErrFormatFailure = errors.New("format failure")

	// ErrOutOfC6Scope is returned when a C6-admissible input carries a
	// construct C6 does not project: a non-property parameter (post-v1;
	// whole-node / whole-edge / scalar-literal / list / unknown / bare-
	// temporal-expression parameter is still out of scope), or a query
	// text carrying a raw-string-hostile backtick. Category-grained per
	// C0's precedent. Renamed from ErrOutOfC5Scope at C6; no scope-
	// widening at C6 (polish stage, no new capability admitted).
	ErrOutOfC6Scope = errors.New("out of C6 scope")

	// ErrUnrepresentableWidth is returned when a schema property, a query
	// column, a query parameter, or a list element's leaf has a property
	// width the target's TypeMap reports no faithful Go carrier for.
	// Distinct from ErrOutOfC6Scope: the refused widths follow from what
	// the store can hold and what the driver can carry, so no future
	// stage retires them — a permanent unrepresentability, not a deferred
	// capability. The fail-message names the fail-site (entity +
	// property; query + column; query + parameter) and the offending
	// width. Checked eagerly at Phase Z for schema properties; lazily at
	// Phase A for parameters and columns; lazily during list recursion
	// for list leaves. Introduced at C3.
	ErrUnrepresentableWidth = errors.New("unrepresentable property width")

	// ErrUnstorableProperty is returned when a schema property has a width
	// the target's TypeMap reports a faithful Go carrier for and the
	// target's STORE will not hold. The carrier is what distinguishes it
	// from ErrUnrepresentableWidth: there the answer is that no Go type
	// carries the value, here the Go type exists and is emitted elsewhere
	// on the same backend — a nested list decodes fine as a query value on
	// neo4j (render_queries.go) while the server refuses it as a stored
	// property (ADR 0035). So the two sentinels address different edits:
	// a width refusal is answered by changing the declared width, an
	// unstorable one by moving the value out of the schema and into a
	// query, or by generating against a backend whose store holds it.
	//
	// Asked eagerly at Phase Z for declared entity properties and NOWHERE
	// else, which is the whole of its scope: a query column and a query
	// parameter are read and bound, never stored, so a storage rule has
	// nothing to say about them. The fail-message names the entity, the
	// property, and the width.
	ErrUnstorableProperty = errors.New("unstorable property width")

	// ErrUnimplementedTypeKind is returned when a schema property, a query
	// column, a query parameter, or a list element carries a PropertyType
	// of a KIND gqlc has built no emission for — today KindRecord and
	// KindUnion, which the schema front end resolves (gqlc-h9n.33) and no
	// backend renders.
	//
	// It asks a question the other three refusals below it do not, and
	// asking it first is the whole point. ErrUnrepresentableWidth means
	// the backend has no Go type wide enough for a width gqlc does emit;
	// ErrUnstorableProperty means the carrier exists and the store will
	// not keep it. Both send the reader to a declared width. There is no
	// width to change here, so a dialect table falling off its switch and
	// answering ok=false would name the wrong edit — the answer is that
	// gqlc emits nothing of this shape yet, at any width, on any backend.
	// That is why the refusal is gqlc's own and lives in prepare.go
	// rather than in the two typeMaps, which are untouched by it.
	//
	// The one sentinel here carrying a "yet". The kinds are unbuilt, not
	// refused: an emission retires it, and this is the fence that keeps
	// the interim honest rather than silently generating a Go type named
	// after an encoding. It is asked at every position that asks a
	// TypeMap for a property carrier, and the ask RECURSES through list
	// elements — a shallow check admits LIST<RECORD<…>>, whose record
	// then dies inside the table as a width error, which is exactly the
	// confusion this sentinel exists to prevent. The fail-message names
	// the fail-site, the declared width, and the sub-type with no
	// emission, because under a list those last two differ.
	ErrUnimplementedTypeKind = errors.New("property type kind not implemented yet")

	// ErrUnrepresentableEdgeUnion is returned when a query column, or a
	// list element's leaf, resolves to an edge union two of whose
	// candidates carry the same label. An edge value carries its label
	// and its properties, never its endpoint types, so the label is the
	// whole of what the dispatch has to choose a candidate by, and two
	// candidates sharing one are indistinguishable once the value has
	// arrived. The fail-message names the fail-site, the two candidate
	// entities, and the label they share.
	ErrUnrepresentableEdgeUnion = errors.New("unrepresentable edge union")

	// ErrUnrepresentableTemporal is returned when a query column, or a
	// list element's leaf, is a temporal expression of a kind the
	// target's TypeMap reports no faithful Go carrier for. Distinct from
	// ErrUnrepresentableWidth: a temporal expression carries no property
	// width at all (resolver keeps ResolvedTemporal apart from
	// ResolvedProperty's DATE / TIMESTAMP families, ADR 0002), so the two
	// sentinels address different edits — a schema for that one, a
	// query's RETURN clause for this one. Permanent on the same terms:
	// which kinds refuse follows from what the target can hold, so it is
	// a target's answer and not a stage's, and it is per kind, so a
	// target admitting part of the enum still fails on the rest
	// (ADR 0025). The fail-message names the fail-site (query + column;
	// list element) and the temporal kind.
	ErrUnrepresentableTemporal = errors.New("unrepresentable temporal kind")

	// ErrExecOnProjection is returned when a query annotated :exec has at
	// least one projected column (len(Validated.Columns) > 0). The caller
	// either drops the :exec annotation (annotate :one or :many per the
	// desired arity) or drops the RETURN clause (annotate :exec on the
	// pure write). sqlc silently allows :exec on a SELECT, discarding
	// rows; we refuse (ADR 0010 D1 Resolved: reject-don't-guess). The
	// fail-message names the query, the cardinality (:exec), the projected
	// column count, and the first column's name. Introduced at C4.
	ErrExecOnProjection = errors.New("exec cardinality on projection query")

	// ErrCardinalityShapeMismatch is returned when a query annotated :one
	// or :many has zero projected columns (len(Validated.Columns) == 0).
	// Zero-column reads and zero-column writes both flag: the caller
	// either annotates :exec (if no rows are wanted) or adds a RETURN
	// clause (if rows are wanted). The fail-message names the query, the
	// cardinality (:one or :many), the statement kind (read or write),
	// and the shape ("zero-column read" or "zero-column write"). Distinct
	// from ErrExecOnProjection: the two sentinels address different query
	// edits (annotation vs clause). Introduced at C4.
	ErrCardinalityShapeMismatch = errors.New("cardinality-shape mismatch")

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
// sentinels Generate may return, kept in one place so a backend's
// TestSentinelReachability can sweep it against the invalid-fixture
// map. A sentinel added here must be paired with at least one negative
// fixture; a retired one must be dropped from both.
//
// docs/specs/codegen-sentinel-taxonomy.md indexes this set and the
// constructs that route to each member; TestSentinelTaxonomy holds the
// two against each other, so an edit here needs the matching rows there.
//
// A handful of fail-sites in this package carry a sentinel but no
// schema, query, CLI option or Input an out-of-package caller can
// assemble reaches: each is shadowed by an earlier check applying the
// same predicate to the same value. Each is tagged
// `//gqlc:unreachable <site>` above its return and recorded in that
// document's §3 under the same site name, with the argument it rests
// on. Two arguments are not available there. "The resolver would never
// build this" is about the pipeline: Input, NamedQuery and every
// resolver.Resolved* variant are exported structs with exported fields,
// so a caller assembles one without the resolver's help. "The switch
// names every variant of a sealed interface" is about a seal that is
// not one: resolver.ResolvedType's unexported marker stops another
// package writing an implementation from scratch and stops nothing
// else, because Go promotes an embedded type's unexported methods — so
// `struct{ resolver.ResolvedNode }`, declared anywhere, satisfies the
// interface and matches no `case Variant:` arm. The pointer forms are
// the same hole a size smaller. The set falling through such a default
// is open, so no switch over that interface is total and no count of
// its inhabitants is worth taking. The tag is not a comment the fence
// trusts: TestSentinelTaxonomy runs the corpus under coverage of this
// package and fails if a tagged branch executes, so tagging a branch
// anything reaches turns the suite red rather than silencing it.
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
	ErrOutOfC6Scope,
	ErrParamNameCollision,
	ErrRowFieldCollision,
	ErrAliasRequired,
	ErrIdentifierCollision,
	ErrInvalidEntityName,
	ErrUnnamedMultiLabelType,
	ErrPropertyFieldCollision,
	ErrUnrepresentableWidth,
	ErrUnstorableProperty,
	ErrUnimplementedTypeKind,
	ErrUnrepresentableEdgeUnion,
	ErrUnrepresentableTemporal,
	ErrExecOnProjection,
	ErrCardinalityShapeMismatch,
}

// AllSentinels returns a copy of the codegen package's user-input-
// reachable sentinels. Exported for cross-package harnesses (a backend's
// fixture loader) that need to map fully-qualified sentinel names back
// to values. Callers must not rely on ordering — the slice is
// copy-returned so a mutation cannot leak into the canonical set.
func AllSentinels() []error { return slices.Clone(allSentinels) }
