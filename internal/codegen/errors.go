package codegen

import (
	"errors"
	"fmt"
	"slices"

	"github.com/areqag/gqlc/internal/graph"
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
	// emitted file's raw contents.
	//
	// It spent most of its life documented as "a template bug —
	// unreachable via any legitimate fixture" and held out of
	// allSentinels on that ground. That claim was falsified three times
	// and is now retired.
	//
	// The first two falsifiers were template bugs a user's schema
	// reached. gqlc-2m2v: a query binding two or more parameters, one of
	// them $_, emitted a nameless Params field and reached gofmt on all
	// three targets. gqlc-9xiz: a property of type LIST<RECORD> or
	// LIST<LIST<RECORD>> made AGE derive its decode-helper name from the
	// carrier map[string]any, which is not a Go identifier, so the
	// brackets survived into the emitted call. Both were repaired at the
	// template, and the exclusion survived both on the argument that a
	// repaired template makes the claim true again.
	//
	// The third did not, and it is why the sentinel is in allSentinels
	// now. NOTHING IS BROKEN IN IT: a NamedQuery whose Name is not a Go
	// identifier is emitted as a method name and refused by go/format,
	// with every template correct. Name's own doc below says "Enforced by
	// the queryfile front end; Generate does not re-validate" — which is
	// the pipeline-vs-contract argument §5.1 rejects (gqlc-h4ug), since
	// Name is an exported field of an exported struct and what the front
	// end enforces does not bound what a caller hands over.
	//
	// So the obligation membership carries is discharged the way §5 step
	// 3 provides for a construct with no on-disk form: by a case in
	// conformance/assembled_input_test.go (format-failure-query-name),
	// not by a negative fixture. The earlier reading — that membership
	// would oblige a well-formed schema that fails, i.e. a live defect
	// frozen into the corpus — rested on missing that clause.
	//
	// Wrapped-and-named still beats a bare error string, and now the
	// wrapping is load-bearing rather than defensive: an author whose
	// query name is rejected gets a sentinel they can match on.
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

	// ErrRecordFieldCollision is returned when a declared record's
	// fields have no legal Go spelling: two field names that mangle to
	// one struct field, or a name of underscores alone that mangles to
	// none at all. The fail-message names the position, the record
	// encoding, and the source field names. It mirrors
	// ErrPropertyFieldCollision one container in — a record is spelled
	// as a struct wherever it appears, so the same mangle that decides
	// an entity's fields decides a record's.
	//
	// It is separate from ErrPropertyFieldCollision because the two name
	// different things to fix: that one names an entity and two of its
	// properties, this one a record TYPE that may be declared far from
	// the property carrying it, and renaming a field of it changes every
	// position that type reaches. Introduced at stage 1 of gqlc-x9tg7.
	ErrRecordFieldCollision = errors.New("record field collision")
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
// ErrFormatFailure was the one name excluded here, as "defensive-only,
// unreachable via any legitimate fixture". It is a member now. What
// carried the exclusion was the belief that only a broken template
// reaches go/format's refusal; what ended it is that a NamedQuery whose
// Name is not a Go identifier reaches it with every template correct,
// and Name is an exported field a caller sets. Its witness is the
// assembled case rather than a fixture, so it is listed in
// conformance's assembledOnlySentinels — see this sentinel's own doc
// comment above for the three falsifications and spec §5 step 3 for why
// an assembled case discharges the obligation a fixture usually does.
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
	ErrRecordFieldCollision,
	ErrUnrepresentableWidth,
	ErrUnstorableProperty,
	ErrUnrepresentableEdgeUnion,
	ErrUnrepresentableTemporal,
	ErrExecOnProjection,
	ErrCardinalityShapeMismatch,
	ErrFormatFailure,
}

// AllSentinels returns a copy of the codegen package's user-input-
// reachable sentinels. Exported for cross-package harnesses (a backend's
// fixture loader) that need to map fully-qualified sentinel names back
// to values. Callers must not rely on ordering — the slice is
// copy-returned so a mutation cannot leak into the canonical set.
func AllSentinels() []error { return slices.Clone(allSentinels) }

// widthRefusal is an ErrUnrepresentableWidth carrying the width it is
// about. The four fail-sites that raise that sentinel already name the
// width in their text; this carries the VALUE alongside it, because the
// one decision downstream of the sentinel — whether the backend that
// refused should put its own name on the message — is a decision about
// which width was refused, and a backend reading its own sentence back
// apart to find out would be parsing a message it has no contract with.
//
// Unexported, with a value receiver, so the only way to make one is the
// constructor below and the only way to read one is RefusedWidth. The
// wrapped error carries the sentinel and the whole of the text, so
// errors.Is and Error() are unaffected by the carriage.
type widthRefusal struct {
	err   error
	width graph.PropertyType
}

func (e widthRefusal) Error() string { return e.err.Error() }
func (e widthRefusal) Unwrap() error { return e.err }

// unrepresentableWidth builds an ErrUnrepresentableWidth fail-message
// that remembers the width it refused. The caller passes the sentinel in
// the format arguments as it always did, so the branch still reads as a
// `%w` wrap of ErrUnrepresentableWidth to a reader and to the sentinel
// fence's AST walk, which keys on the identifier inside the return.
func unrepresentableWidth(width graph.PropertyType, format string, args ...any) error {
	return widthRefusal{err: fmt.Errorf(format, args...), width: width}
}

// RefusedWidth reports the declared width an ErrUnrepresentableWidth
// refusal is about, and whether the error carries one at all.
//
// ok=false is the honest answer for an error this package did not raise
// through the constructor above — including a future fail-site that
// forgets to — so a caller must treat it as "not known" rather than as
// "no width". The one caller today is a backend deciding whether to
// attribute its refusal to itself, and it keeps the attribution on
// ok=false: the conservative direction is the message the backend
// already emitted.
func RefusedWidth(err error) (graph.PropertyType, bool) {
	var refusal widthRefusal
	if !errors.As(err, &refusal) {
		return "", false
	}
	return refusal.width, true
}
