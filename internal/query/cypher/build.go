// Package cypher implements query.Parser for openCypher: an ANTLR
// listener-driven lowering of query source into the curated query model,
// grown test-first against the openCypher TCK (ADR 0004).
package cypher

import (
	"fmt"

	"github.com/areqag/gqlc/internal/graph"
	"github.com/areqag/gqlc/internal/query"
)

// build assembles the collected branches, parts, parameters and return items into
// a query.Query, after a per-part self-consistency validation (spec §2/§4): every
// referenced variable (return item, edge endpoint) must resolve to a binding in
// its own part OR to a name the previous part's WITH carried into it (else
// ErrUnboundVariable), and an edge endpoint resolving to an edge binding in its
// own part is a kind conflict (else ErrVariableKindConflict). This is a
// validation, not a schema resolution — no schema is consulted. build returns the
// zero Query on any error.
func (l *listener) build() (query.Query, error) {
	if l.err != nil {
		return query.Query{}, l.err
	}

	branches := make([]query.QueryBranch, 0, len(l.branches))
	for _, rb := range l.branches {
		branch, err := l.buildBranch(rb)
		if err != nil {
			return query.Query{}, err
		}
		branches = append(branches, branch)
	}

	params := make([]query.Parameter, 0, len(l.params))
	for _, p := range l.params {
		params = append(params, *p)
	}

	q := query.Query{Branches: branches}
	if len(l.combinators) > 0 {
		q.Combinators = l.combinators
	}
	if len(params) > 0 {
		q.Parameters = params
	}
	return q, nil
}

// buildBranch validates and assembles one branch's parts, threading the
// exported-name set left to right: part K resolves its refs against {its own
// bindings} ∪ {names part K−1 exported via WITH}, and computes what it exports
// into part K+1 (spec §4).
func (l *listener) buildBranch(rb *rawBranch) (query.QueryBranch, error) {
	parts := make([]query.QueryPart, 0, len(rb.parts))
	imported := map[string]bool{} // names the previous part's WITH carried in
	for _, rp := range rb.parts {
		part, exported, err := l.buildPart(rp, imported)
		if err != nil {
			return query.QueryBranch{}, err
		}
		parts = append(parts, part)
		imported = exported
	}
	return query.QueryBranch{Parts: parts}, nil
}

// buildPart validates one part against its scope ({its own named bindings} ∪
// imported) and returns the assembled query.QueryPart plus the set of names it
// exports into the next part. Endpoint refs must resolve to a NODE binding within
// the part's own bindings (an imported name carries no kind to check, and an edge
// endpoint always names a node in the same MATCH).
func (l *listener) buildPart(rp *rawPart, imported map[string]bool) (query.QueryPart, map[string]bool, error) {
	scope := map[string]bool{}
	for k := range imported {
		scope[k] = true
	}
	for _, b := range rp.bindings {
		if b.variable != "" {
			scope[b.variable] = true
		}
	}

	for _, ref := range rp.refs {
		if !scope[ref.name] {
			return query.QueryPart{}, nil, fmt.Errorf("%w: %s", ErrUnboundVariable, ref.name)
		}
		// An endpoint must reference a node binding; it always names a node in the
		// same MATCH, so its kind is checked against this part's own bindings. A
		// return-item ref accepts either kind (and may resolve to an imported name).
		if ref.endpointRef {
			idx, ok := rp.byVar[ref.name]
			if ok && rp.bindings[idx].kind != graph.Node {
				return query.QueryPart{}, nil, fmt.Errorf("%w: %s", ErrVariableKindConflict, ref.name)
			}
		}
	}

	bindings := make([]query.Binding, 0, len(rp.bindings))
	for _, rb := range rp.bindings {
		b, err := rb.toBinding()
		if err != nil {
			return query.QueryPart{}, nil, err
		}
		bindings = append(bindings, b)
	}

	part := query.QueryPart{Returns: rp.returns, ReturnsAll: rp.returnsAll}
	if len(bindings) > 0 {
		part.Bindings = bindings
	}

	// The names this part exports into the next: under WITH * the whole in-scope
	// set carries forward (transitive — spec §4); otherwise each return item's
	// Name (the AS alias, or the bare variable for WITH a).
	exported := map[string]bool{}
	if rp.returnsAll {
		for k := range scope {
			exported[k] = true
		}
	} else {
		for _, r := range rp.returns {
			exported[r.Name] = true
		}
	}
	return part, exported, nil
}

// toBinding builds the model binding from a raw binding via the smart
// constructors, so the model's invariants are enforced at assembly. The
// nullable flag picks the OPTIONAL-introduced variant (ADR 0006).
func (rb *rawBinding) toBinding() (query.Binding, error) {
	if rb.kind == graph.Edge {
		// The single polarity flip from the listener's zero-value-safe inverted
		// rawBinding.undirected to the model's positive directed field lives here
		// (Stage 5 §4): directed = !undirected.
		directed := !rb.undirected
		if rb.nullable {
			return query.NewNullableEdgeBinding(rb.variable, rb.labels, rb.source, rb.target, directed)
		}
		return query.NewEdgeBinding(rb.variable, rb.labels, rb.source, rb.target, directed)
	}
	if rb.nullable {
		return query.NewNullableNodeBinding(rb.variable, rb.labels)
	}
	return query.NewNodeBinding(rb.variable, rb.labels)
}
