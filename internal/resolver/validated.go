package resolver

import (
	"encoding/json"

	"github.com/areqag/gqlc/internal/graph"
	"github.com/areqag/gqlc/internal/schema"
)

// ValidatedQuery is the resolver's output: resolved result columns, resolved
// parameters, and the statement kind codegen keys its transaction-mode decision
// on. Provisional through R7; a future ADR — the ADR 0008 analogue — freezes
// the shape once the resolver is feature-complete (ADR 0009).
type ValidatedQuery struct {
	Columns    []Column            `json:"columns"`
	Parameters []ResolvedParameter `json:"parameters"`
	Statement  StatementKind       `json:"statement"`
}

// Column is one result column in projection order: its name (an explicit alias
// or the parser's source-text name, verbatim from ReturnItem.Name) and its
// resolved type.
type Column struct {
	Name string       `json:"name"`
	Type ResolvedType `json:"type"`
}

// ResolvedParameter is one query parameter in query-wide first-appearance
// order: its name and its resolved type. R2 unifies the type across the
// parameter's Uses per §4.6/§4.8 of the R2 spec; a conflict short-circuits
// with ErrParameterTypeConflict.
type ResolvedParameter struct {
	Name string       `json:"name"`
	Type ResolvedType `json:"type"`
}

// StatementKind mirrors query.StatementKind but is redeclared locally so the
// codegen-facing wire shape of ValidatedQuery does not force consumers to
// import internal/query. Wire tags match query.StatementKind so an
// errors.Is-style equivalence check works when a consumer already holds both.
type StatementKind int

const (
	// StatementRead is a query composed only of reading clauses. Zero-value
	// default.
	StatementRead StatementKind = iota
	// StatementWrite is a query with at least one write clause at outer
	// scope.
	StatementWrite
)

// String is the wire tag ("read" / "write"). Single source the JSON encoding
// derives from.
func (k StatementKind) String() string {
	if k == StatementWrite {
		return "write"
	}
	return "read"
}

// MarshalJSON renders a StatementKind as its wire string.
func (k StatementKind) MarshalJSON() ([]byte, error) {
	return json.Marshal(k.String())
}

// ResolvedType is the sealed sum of resolved types. Each variant carries a
// String() wire tag and a MarshalJSON that emits a tagged-union object with a
// "kind" discriminator, so the golden encoding is stable and readable. R0
// contributes ResolvedNode and ResolvedProperty; R1 adds ResolvedEdge; R2
// adds ResolvedScalar, ResolvedTemporal, ResolvedList, and ResolvedUnknown
// (§3 of the R2 spec).
type ResolvedType interface {
	String() string
	isResolvedType()
}

// ResolvedNode is a whole-entity projection whose Ref names a node binding,
// keyed by the resolved node type's canonical label set.
type ResolvedNode struct {
	Labels graph.LabelSetKey `json:"labels"`
}

// String is the wire tag "node".
func (ResolvedNode) String() string { return "node" }

// MarshalJSON emits a tagged-union object with a "kind" discriminator.
func (n ResolvedNode) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Kind   string            `json:"kind"`
		Labels graph.LabelSetKey `json:"labels"`
	}{Kind: n.String(), Labels: n.Labels})
}

func (ResolvedNode) isResolvedType() {}

// ResolvedProperty is a property-of-entity projection or an inline-map
// parameter use: the schema's normalised property type plus the nullability
// bit. The R0 resolver produces this variant for both a projected property
// column and an inline-map parameter (§4.3).
type ResolvedProperty struct {
	Type     graph.PropertyType `json:"type"`
	Nullable bool               `json:"nullable"`
}

// String is a diagnostic Stringer, composing the property-type family into
// the tag so a reader distinguishes ResolvedProperty{INT32} from
// ResolvedProperty{INT64} at a glance. It is NOT the wire tag — the JSON
// encoding is emitted by MarshalJSON, whose "kind" discriminator is the bare
// string "property".
func (p ResolvedProperty) String() string { return "property:" + string(p.Type) }

// MarshalJSON emits a tagged-union object with a "kind" discriminator plus the
// property type and nullability bit.
func (p ResolvedProperty) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Kind     string             `json:"kind"`
		Type     graph.PropertyType `json:"type"`
		Nullable bool               `json:"nullable"`
	}{Kind: "property", Type: p.Type, Nullable: p.Nullable})
}

func (ResolvedProperty) isResolvedType() {}

// ResolvedEdge is a whole-entity edge projection: the schema's canonical
// (source, label, target) triple. R1 produces this variant for a RefProjection
// whose Ref names an EdgeBinding and whose Property is empty. Multi-hop
// (list<edge>) is R3's business.
type ResolvedEdge struct {
	EdgeKey schema.EdgeKey `json:"edgeKey"`
}

// String is the wire tag "edge".
func (ResolvedEdge) String() string { return "edge" }

// MarshalJSON emits a tagged-union object with a "kind" discriminator.
func (e ResolvedEdge) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Kind    string         `json:"kind"`
		EdgeKey schema.EdgeKey `json:"edgeKey"`
	}{Kind: e.String(), EdgeKey: e.EdgeKey})
}

func (ResolvedEdge) isResolvedType() {}

// ResolvedEdgeUnion is a multi-candidate edge whole-entity projection: the
// closed set of schema EdgeKeys the resolver committed for a multi-type edge
// binding whose labels (or the label × orientation cross-product for an
// undirected multi-type edge) resolve to more than one edge in the schema.
// Produced by R3 for a RefProjection whose Ref names an EdgeBinding with a
// multi-candidate committed set. The single-candidate case stays ResolvedEdge.
// EdgeKeys is deterministic (canonical order per R3 spec §4.4) and non-empty
// with len >= 2; the empty case is ErrUnknownEdge, the single case is
// ResolvedEdge.
type ResolvedEdgeUnion struct {
	EdgeKeys []schema.EdgeKey `json:"edgeKeys"`
}

// String is the wire tag "edgeUnion".
func (ResolvedEdgeUnion) String() string { return "edgeUnion" }

// MarshalJSON emits a tagged-union object with a "kind" discriminator plus
// the ordered candidate slice.
func (u ResolvedEdgeUnion) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Kind     string           `json:"kind"`
		EdgeKeys []schema.EdgeKey `json:"edgeKeys"`
	}{Kind: u.String(), EdgeKeys: u.EdgeKeys})
}

func (ResolvedEdgeUnion) isResolvedType() {}

// ResolvedScalar carries a parser-coarse scalar kind: an openCypher literal
// or an integer clause slot (SKIP / LIMIT). Distinct from ResolvedProperty,
// which carries a bit-width family from the schema (ADR 0002). Introduced at
// R2 (§3.1).
type ResolvedScalar struct {
	Kind Scalar `json:"scalar"`
}

// Scalar is the closed enum of parser-coarse scalar kinds. Mirrors
// query.Type's scalar sub-sum (bool, int, float, string, null, map). The int
// clause-slot witness (ClauseSlotUse -> ScalarInt) is a producer, not a new
// variant.
type Scalar int

const (
	// ScalarBool is the openCypher boolean literal / predicate result.
	ScalarBool Scalar = iota
	// ScalarInt is an integer literal or a SKIP/LIMIT clause slot.
	ScalarInt
	// ScalarFloat is a floating-point literal.
	ScalarFloat
	// ScalarString is a string literal.
	ScalarString
	// ScalarNull is the openCypher NULL literal.
	ScalarNull
	// ScalarMap is a map literal.
	ScalarMap
)

// String is the wire tag ("bool" / "int" / "float" / "string" / "null" /
// "map"). Single source the JSON encoding derives from.
func (s Scalar) String() string {
	switch s {
	case ScalarInt:
		return "int"
	case ScalarFloat:
		return "float"
	case ScalarString:
		return "string"
	case ScalarNull:
		return "null"
	case ScalarMap:
		return "map"
	default:
		return "bool"
	}
}

// MarshalJSON renders a Scalar as its wire string.
func (s Scalar) MarshalJSON() ([]byte, error) { return json.Marshal(s.String()) }

// String is the wire tag "scalar".
func (ResolvedScalar) String() string { return "scalar" }

// MarshalJSON emits a tagged-union object with a "kind" discriminator plus the
// scalar kind.
func (r ResolvedScalar) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Kind   string `json:"kind"`
		Scalar Scalar `json:"scalar"`
	}{Kind: r.String(), Scalar: r.Kind})
}

func (ResolvedScalar) isResolvedType() {}

// ResolvedTemporal carries an openCypher temporal kind — the full temporal
// set (date, time, localtime, datetime, localdatetime, duration), distinct
// from ResolvedProperty's DATE / TIMESTAMP bit-width families (ADR 0002).
// R2 draws the storage-vs-expression line (§3.2).
type ResolvedTemporal struct {
	Kind Temporal `json:"temporal"`
}

// Temporal is the closed enum of openCypher temporals.
type Temporal int

const (
	// TemporalDate is the openCypher DATE.
	TemporalDate Temporal = iota
	// TemporalTime is the openCypher TIME (zoned).
	TemporalTime
	// TemporalLocalTime is the openCypher LOCAL TIME.
	TemporalLocalTime
	// TemporalDateTime is the openCypher DATETIME (zoned).
	TemporalDateTime
	// TemporalLocalDateTime is the openCypher LOCAL DATETIME.
	TemporalLocalDateTime
	// TemporalDuration is the openCypher DURATION.
	TemporalDuration
)

// String is the wire tag ("date" / "time" / "localtime" / "datetime" /
// "localdatetime" / "duration"). Single source the JSON encoding derives
// from.
func (t Temporal) String() string {
	switch t {
	case TemporalTime:
		return "time"
	case TemporalLocalTime:
		return "localtime"
	case TemporalDateTime:
		return "datetime"
	case TemporalLocalDateTime:
		return "localdatetime"
	case TemporalDuration:
		return "duration"
	default:
		return "date"
	}
}

// MarshalJSON renders a Temporal as its wire string.
func (t Temporal) MarshalJSON() ([]byte, error) { return json.Marshal(t.String()) }

// String is the wire tag "temporal".
func (ResolvedTemporal) String() string { return "temporal" }

// MarshalJSON emits a tagged-union object with a "kind" discriminator plus the
// temporal kind.
func (r ResolvedTemporal) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Kind     string   `json:"kind"`
		Temporal Temporal `json:"temporal"`
	}{Kind: r.String(), Temporal: r.Kind})
}

func (ResolvedTemporal) isResolvedType() {}

// ResolvedList is a list of elements. Element is the recursive resolved type
// of the list's element position. Introduced at R2 (§3.3); R3 widens the
// element vocabulary when the schema gains list-typed columns.
type ResolvedList struct {
	Element ResolvedType `json:"element"`
}

// String is the wire tag "list".
func (ResolvedList) String() string { return "list" }

// MarshalJSON emits a tagged-union object with a "kind" discriminator plus
// the element type (itself tagged-union).
func (r ResolvedList) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Kind    string       `json:"kind"`
		Element ResolvedType `json:"element"`
	}{Kind: r.String(), Element: r.Element})
}

func (ResolvedList) isResolvedType() {}

// ResolvedUnknown is the resolver's honest posture when the parser was
// already TypeUnknown and no schema witness commits: a rich expression whose
// result type the parser could not compute (property-participating
// arithmetic, NULL propagation), a non-aggregate function call's result
// (function identity below the type-interface boundary, ADR 0005), a list
// literal whose element type the parser did not commit. Introduced at R2
// (§3.4).
type ResolvedUnknown struct{}

// String is the wire tag "unknown".
func (ResolvedUnknown) String() string { return "unknown" }

// MarshalJSON emits a tagged-union object with a "kind" discriminator; no
// extra fields.
func (r ResolvedUnknown) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Kind string `json:"kind"`
	}{Kind: r.String()})
}

func (ResolvedUnknown) isResolvedType() {}
