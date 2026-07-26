package gql

import (
	"github.com/areqag/gqlc/internal/graph"
	"github.com/areqag/gqlc/internal/schema"
)

// rawSchema is the complete unresolved intermediate form produced by the walk:
// the graph type name plus the raw node and edge types (whose endpoints are
// rawEndpoints). It is the boundary between the two stages — the listener fills it
// from the parse tree, then resolve() turns it into the final schema.Schema — so
// resolution stays pure Go, independent of ANTLR and testable on its own.
type rawSchema struct {
	name  string
	nodes []rawNode
	edges []rawEdge
}

// resolve turns the collected rawSchema into the final schema.Schema, in plain Go.
//
// A second pass is unavoidable because a GQL graph type is an order-independent
// set of element types: an edge may reference a node type declared later in the
// body, so endpoints can only be resolved once every node type is known. Hence
// two phases: build the node types and the alias table first, then resolve each
// edge's endpoints against them.
func (r rawSchema) resolve() (schema.Schema, error) {
	s := schema.Schema{
		Name:  r.name,
		Nodes: make(map[graph.LabelSetKey]schema.NodeType),
		Edges: make(map[schema.EdgeKey]schema.EdgeType),
	}

	idx := nodeIndex{
		aliases:  make(map[string]graph.LabelSetKey),
		declared: make(map[string]bool),
		types:    s.Nodes,
	}
	for _, n := range r.nodes {
		if len(n.labels) == 0 {
			return schema.Schema{}, ErrUnnamedNodeType
		}
		key := n.labels.Key()
		if _, dup := s.Nodes[key]; dup {
			return schema.Schema{}, ErrDuplicateNodeType
		}
		s.Nodes[key] = schema.NodeType{
			Labels:     key,
			Name:       n.name,
			Properties: n.props,
		}
		if n.alias != "" {
			idx.aliases[n.alias] = key
		}
		if n.name != "" {
			idx.declared[n.name] = true
		}
		for _, label := range n.labels {
			idx.declared[label] = true
		}
	}

	for _, e := range r.edges {
		if len(e.labels) == 0 {
			return schema.Schema{}, ErrUnnamedEdgeType
		}
		source, err := e.source.resolve(idx)
		if err != nil {
			return schema.Schema{}, err
		}
		target, err := e.target.resolve(idx)
		if err != nil {
			return schema.Schema{}, err
		}

		key := schema.EdgeKey{Source: source, Label: e.labels.Key(), Target: target}
		if _, dup := s.Edges[key]; dup {
			return schema.Schema{}, ErrDuplicateEdgeType
		}
		s.Edges[key] = schema.EdgeType{
			EdgeKey:    key,
			Name:       e.name,
			Properties: e.props,
		}
	}

	return s, nil
}

// nodeIndex is what an edge endpoint resolves against: the alias table, the
// identifiers the declared node types answer to, and the node types themselves.
//
// declared exists to sharpen a diagnostic and for nothing else.
// `CONNECTING (Person TO Person)` puts an identifier where the grammar reads an
// alias, so it misses unless some declaration bound `Person` as one — and
// reporting that as an undeclared node type sends an author who wrote the type's
// own name or label looking for a declaration that is on the screen in front of
// them. Names and labels go in whether or not their type also binds an alias:
// naming the type where an alias belongs is the same mistake either way.
type nodeIndex struct {
	aliases  map[string]graph.LabelSetKey
	declared map[string]bool
	types    map[graph.LabelSetKey]schema.NodeType
}

// resolve maps an edge endpoint to the canonical key of the declared node type it
// names: an alias via the alias table, or an inline filler via its own label set.
// Either way the resolved type must have been declared.
func (ref rawEndpoint) resolve(idx nodeIndex) (graph.LabelSetKey, error) {
	var key graph.LabelSetKey
	if ref.alias != "" {
		// The alias table is consulted first because an alias is usually spelled the
		// same as the label it is bound to (`... AS Person`), and there the alias is
		// what the endpoint means.
		k, ok := idx.aliases[ref.alias]
		if !ok {
			if idx.declared[ref.alias] {
				return "", ErrEndpointNotAlias
			}
			return "", ErrUnknownEndpoint
		}
		key = k
	} else {
		key = ref.labels.Key()
	}

	if _, ok := idx.types[key]; !ok {
		return "", ErrUnknownEndpoint
	}
	return key, nil
}
