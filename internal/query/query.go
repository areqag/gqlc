package query

import "github.com/antranig-yeretzian/gqlc/internal/graph"

// Query is the model of a single parsed query: the entities it binds, the
// parameters it takes, and the values it returns. It is schema-agnostic — it
// records what the query says, not whether any schema supports it; resolving it
// against a schema.Schema is a separate stage (ADR 0003).
type Query struct {
	// Bindings are the entities the query binds. Among named bindings the Variable
	// is unique; Returns, Parameters and edge endpoints reference them by it. An
	// anonymous binding (Variable == "") is always an edge, e.g. the relationship
	// in (a)-->(b).
	Bindings []Binding

	// Parameters are the query's inputs, deduplicated by name in first-appearance
	// order.
	Parameters []Parameter

	// Returns are the query's result columns, in source order with duplicates
	// kept: RETURN a, b is a different shape from RETURN b, a.
	Returns []ReturnItem
}

// Binding is a query variable bound to a graph entity, carrying its labels as
// written. Labels may be empty when the variable is unlabelled (the b in
// (a:Person)-[:KNOWS]->(b)). For an edge, Source and Target are set, in canonical
// source->target order (a left-pointing edge is canonicalised); for a node they
// are nil.
type Binding struct {
	Variable string         // the name as written: the p in (p:Person)
	Labels   graph.LabelSet // labels as written; may be empty
	Source   *Endpoint      // set for an edge, nil for a node
	Target   *Endpoint      // set for an edge, nil for a node
}

// Kind reports whether the binding is a node or an edge.
func (b Binding) Kind() graph.EntityKind {
	if b.Source != nil {
		return graph.Edge
	}
	return graph.Node
}

// Endpoint is one end of an edge: a reference to a named Binding (Variable), or
// an anonymous endpoint node, which leaves Variable empty and carries inline
// Labels (themselves possibly empty, as in `()`).
type Endpoint struct {
	Variable string
	Labels   graph.LabelSet
}

// Ref points from a ReturnItem or Parameter into the query's bindings: a whole
// entity (Property empty) or one of its properties. For example, the return
// items in RETURN p, p.name:
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
