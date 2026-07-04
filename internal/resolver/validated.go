package resolver

import (
	"encoding/json"

	"github.com/areqag/gqlc/internal/graph"
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
// order: its name and its resolved type. R0's capability scope guarantees
// exactly one Use per parameter (a PropertyUse on a node inline map), so no
// unification across uses runs at R0 (R2's business).
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
// "kind" discriminator, so the golden encoding is stable and readable. The R0
// resolver produces only ResolvedNode (whole-entity projections) and
// ResolvedProperty (property projections and inline-map parameter uses);
// later stages add variants (ResolvedEdge at R1, ResolvedList at R2/R3, etc.).
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
