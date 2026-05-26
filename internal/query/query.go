package query

import (
	"encoding/json"
	"errors"

	"github.com/antranig-yeretzian/gqlc/internal/graph"
)

// Query is the model of a single parsed query: the entities it binds, the
// parameters it takes, and the values it returns. It is schema-agnostic — it
// records what the query says, not whether any schema supports it; resolving it
// against a schema.Schema is a separate stage (ADR 0003).
//
// Query needs no custom MarshalJSON: its members are order-preserving slices of
// strings and sum-type values, so its serialisation is deterministic by
// construction (the sum types carry the determinism discipline themselves).
type Query struct {
	// Bindings are the entities the query binds, a NodeBinding or an EdgeBinding
	// each. Among named bindings the variable is unique; Returns, Parameters and
	// edge endpoints reference them by it. Only an edge may be anonymous (an empty
	// variable), e.g. the relationship in (a)-->(b).
	Bindings []Binding

	// Parameters are the query's inputs, deduplicated by name in first-appearance
	// order.
	Parameters []Parameter

	// Returns are the query's result columns, in source order with duplicates
	// kept: RETURN a, b is a different shape from RETURN b, a.
	Returns []ReturnItem
}

// Binding is a query variable bound to a graph entity, carrying its labels as
// written. It is a closed sum of NodeBinding and EdgeBinding — no other type can
// implement it — so a binding is exactly one of the two and a node can never
// carry endpoints nor an edge omit them. Both variants hold their data in
// unexported fields, so NewNodeBinding / NewEdgeBinding are the only way to
// construct a non-zero value: the invariants the types alone cannot express
// (a non-empty node variable, both edge endpoints present) hold for every value
// that exists.
type Binding interface {
	// Kind reports whether the binding is a node or an edge.
	Kind() graph.EntityKind
	isBinding()
}

// NodeBinding is a query variable bound to a node, carrying its labels as
// written. Labels may be empty when the variable is unlabelled (the b in
// (a:Person)-[:KNOWS]->(b)); the resolver infers its type from the edges that
// touch it. A node binding is never anonymous — an anonymous node is a pure
// filter, not a binding — so its variable is always non-empty (NewNodeBinding).
type NodeBinding struct {
	variable string         // the name as written: the p in (p:Person)
	labels   graph.LabelSet // labels as written; may be empty
}

// NewNodeBinding builds a NodeBinding, rejecting the empty variable: an anonymous
// node is never a binding (Stage-0 spec, C3). Labels may be empty (C7).
func NewNodeBinding(variable string, labels graph.LabelSet) (NodeBinding, error) {
	if variable == "" {
		return NodeBinding{}, errors.New("query: node binding requires a non-empty variable")
	}
	return NodeBinding{variable: variable, labels: labels}, nil
}

// Variable is the name as written: the p in (p:Person). Always non-empty.
func (b NodeBinding) Variable() string { return b.variable }

// Labels are the labels as written; may be empty (C7).
func (b NodeBinding) Labels() graph.LabelSet { return b.labels }

// Kind reports that a NodeBinding is a node.
func (NodeBinding) Kind() graph.EntityKind { return graph.Node }

func (NodeBinding) isBinding() {}

// EdgeBinding is a query variable bound to an edge, carrying its labels as
// written and both endpoints, in canonical source->target order (a left-pointing
// edge is canonicalised). Labels may be empty for an untyped edge (C7). The
// variable may be empty: unlike a node, an anonymous edge is its own binding (the
// relationship in (a)-->(b)). Source and Target are always present (NewEdgeBinding).
type EdgeBinding struct {
	variable string         // the name as written: the r in [r:KNOWS]; empty if anonymous
	labels   graph.LabelSet // labels as written; may be empty
	source   Endpoint       // the source endpoint; always set
	target   Endpoint       // the target endpoint; always set
}

// NewEdgeBinding builds an EdgeBinding, rejecting a missing endpoint: an edge
// always has both a source and a target. Variable may be empty (an anonymous
// edge) and Labels may be empty (an untyped edge, C7).
func NewEdgeBinding(variable string, labels graph.LabelSet, source, target Endpoint) (EdgeBinding, error) {
	if source == nil || target == nil {
		return EdgeBinding{}, errors.New("query: edge binding requires both a source and a target endpoint")
	}
	return EdgeBinding{variable: variable, labels: labels, source: source, target: target}, nil
}

// Variable is the name as written: the r in [r:KNOWS]; empty for an anonymous edge.
func (b EdgeBinding) Variable() string { return b.variable }

// Labels are the labels as written; may be empty (C7).
func (b EdgeBinding) Labels() graph.LabelSet { return b.labels }

// Source is the source endpoint; always set.
func (b EdgeBinding) Source() Endpoint { return b.source }

// Target is the target endpoint; always set.
func (b EdgeBinding) Target() Endpoint { return b.target }

// Kind reports that an EdgeBinding is an edge.
func (EdgeBinding) Kind() graph.EntityKind { return graph.Edge }

func (EdgeBinding) isBinding() {}

// Endpoint is one end of an edge. It is a closed sum of VarEndpoint and
// InlineEndpoint — no other type can implement it — so an endpoint either names a
// binding or carries inline labels, never both and never neither. Both variants
// hold their data in unexported fields, so NewVarEndpoint / NewInlineEndpoint are
// the only way to construct a non-zero value.
type Endpoint interface {
	isEndpoint()
}

// VarEndpoint is an endpoint that references a named binding. Its labels live on
// that binding, not here, so they are never duplicated (Stage-0 spec, C4). Its
// variable is always non-empty (NewVarEndpoint); the empty case is InlineEndpoint.
type VarEndpoint struct {
	variable string // the binding referred to
}

// NewVarEndpoint builds a VarEndpoint, rejecting the empty variable: an endpoint
// that names no binding is the inline case, expressed with NewInlineEndpoint.
func NewVarEndpoint(variable string) (VarEndpoint, error) {
	if variable == "" {
		return VarEndpoint{}, errors.New("query: variable endpoint requires a non-empty variable")
	}
	return VarEndpoint{variable: variable}, nil
}

// Variable is the binding referred to. Always non-empty.
func (e VarEndpoint) Variable() string { return e.variable }

func (VarEndpoint) isEndpoint() {}

// InlineEndpoint is an anonymous endpoint node carrying inline labels, which may
// be empty — the fully anonymous () endpoint (Stage-0 spec, C4).
type InlineEndpoint struct {
	labels graph.LabelSet // labels as written; may be empty
}

// NewInlineEndpoint builds an InlineEndpoint. Labels may be empty (the ()
// endpoint), so construction cannot fail.
func NewInlineEndpoint(labels graph.LabelSet) InlineEndpoint {
	return InlineEndpoint{labels: labels}
}

// Labels are the labels as written; may be empty (the () endpoint).
func (e InlineEndpoint) Labels() graph.LabelSet { return e.labels }

func (InlineEndpoint) isEndpoint() {}

// Ref points from a ReturnItem or Parameter into the query's bindings: a whole
// entity (Property empty) or one of its properties. Property is a single name, so
// multi-level access (a.b.c) is unrepresentable. For example, the return items in
// RETURN p, p.name:
//
//	p       →  Ref{Variable: "p"}                   // the whole binding
//	p.name  →  Ref{Variable: "p", Property: "name"} // one of its properties
type Ref struct {
	Variable string // the binding referred to
	Property string // a property of that binding; empty means the whole entity
}

// ReturnItem is one result column: its name (an explicit alias, or derived from
// the source) and the Ref tracing what it projects.
type ReturnItem struct {
	Name string
	Ref  Ref
}

// Parameter is a query input. Uses are the value-positions where the parameter
// appears — each a Ref to the binding property it sits against — so a parameter
// written in N places collapses to one Parameter with N uses. For example, in
//
//	WHERE a.age > $threshold AND b.age > $threshold
//
// $threshold has two uses: {Variable: "a", Property: "age"} and
// {Variable: "b", Property: "age"}.
type Parameter struct {
	Name string
	Uses []Ref
}

// The "var" and "inline" endpoint discriminators have no graph-vocabulary
// counterpart (the distinction is query-side only), so they are named here, the
// one place they are emitted.
const (
	endpointKindVar    = "var"
	endpointKindInline = "inline"
)

// MarshalJSON renders a NodeBinding as a tagged union member discriminated by
// "kind", so the Binding sum marshals to a stable, self-describing shape across
// both variants. The tag derives from graph.EntityKind, so it cannot drift from
// Kind(). Mirrors schema.Schema's determinism discipline: the encoding is fixed
// and independent of any map iteration order.
func (b NodeBinding) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Kind     string         `json:"kind"`
		Variable string         `json:"variable"`
		Labels   graph.LabelSet `json:"labels"`
	}{Kind: b.Kind().String(), Variable: b.variable, Labels: b.labels})
}

// MarshalJSON renders an EdgeBinding as a tagged union member discriminated by
// "kind" (derived from graph.EntityKind). Source and Target are themselves
// tagged-union endpoints.
func (b EdgeBinding) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Kind     string         `json:"kind"`
		Variable string         `json:"variable"`
		Labels   graph.LabelSet `json:"labels"`
		Source   Endpoint       `json:"source"`
		Target   Endpoint       `json:"target"`
	}{Kind: b.Kind().String(), Variable: b.variable, Labels: b.labels, Source: b.source, Target: b.target})
}

// MarshalJSON renders a VarEndpoint as a tagged union member discriminated by
// "kind", matching the Binding sum's convention.
func (e VarEndpoint) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Kind     string `json:"kind"`
		Variable string `json:"variable"`
	}{Kind: endpointKindVar, Variable: e.variable})
}

// MarshalJSON renders an InlineEndpoint as a tagged union member discriminated by
// "kind", matching the Binding sum's convention.
func (e InlineEndpoint) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Kind   string         `json:"kind"`
		Labels graph.LabelSet `json:"labels"`
	}{Kind: endpointKindInline, Labels: e.labels})
}
