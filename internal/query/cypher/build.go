package cypher

import (
	"fmt"

	"github.com/antranig-yeretzian/gqlc/internal/graph"
	"github.com/antranig-yeretzian/gqlc/internal/query"
)

// build assembles the collected raw bindings, parameters and return items into a
// query.Query, after a self-consistency validation: every referenced variable
// (return item, parameter use, edge endpoint) must resolve to a collected
// binding (else ErrUnboundVariable), and a variable used as both a node and an
// edge is a kind conflict (else ErrVariableKindConflict). This is a validation,
// not a schema resolution — no schema is consulted. build returns the zero Query
// on any error.
func (l *listener) build() (query.Query, error) {
	if l.err != nil {
		return query.Query{}, l.err
	}

	for _, ref := range l.refs {
		idx, ok := l.byVar[ref.name]
		if !ok {
			return query.Query{}, fmt.Errorf("%w: %s", ErrUnboundVariable, ref.name)
		}
		// An endpoint must reference a node binding; a return/param use accepts
		// either kind. An endpoint resolving to an edge is the same conflict
		// mergeBinding catches for a variable bound twice.
		if ref.endpointRef && l.bindings[idx].kind != graph.Node {
			return query.Query{}, fmt.Errorf("%w: %s", ErrVariableKindConflict, ref.name)
		}
	}

	bindings := make([]query.Binding, 0, len(l.bindings))
	for _, rb := range l.bindings {
		b, err := rb.toBinding()
		if err != nil {
			return query.Query{}, err
		}
		bindings = append(bindings, b)
	}

	params := make([]query.Parameter, 0, len(l.params))
	for _, p := range l.params {
		params = append(params, *p)
	}

	part := query.QueryPart{Returns: l.returns, ReturnsAll: l.returnsAll}
	if len(bindings) > 0 {
		part.Bindings = bindings
	}

	q := query.Query{
		Branches: []query.QueryBranch{{Parts: []query.QueryPart{part}}},
	}
	if len(params) > 0 {
		q.Parameters = params
	}
	return q, nil
}

// toBinding builds the model binding from a raw binding via the branch-1 smart
// constructors, so the model's invariants are enforced at assembly. The
// nullable flag picks the OPTIONAL-introduced variant (ADR 0006).
func (rb *rawBinding) toBinding() (query.Binding, error) {
	if rb.kind == graph.Edge {
		if rb.nullable {
			return query.NewNullableEdgeBinding(rb.variable, rb.labels, rb.source, rb.target)
		}
		return query.NewEdgeBinding(rb.variable, rb.labels, rb.source, rb.target)
	}
	if rb.nullable {
		return query.NewNullableNodeBinding(rb.variable, rb.labels)
	}
	return query.NewNodeBinding(rb.variable, rb.labels)
}
