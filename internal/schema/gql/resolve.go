package gql

import (
	"github.com/antranig-yeretzian/gqlc/internal/graph"
	"github.com/antranig-yeretzian/gqlc/internal/schema"
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

	aliases := make(map[string]graph.LabelSetKey)
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
			aliases[n.alias] = key
		}
	}

	for _, e := range r.edges {
		if len(e.labels) == 0 {
			return schema.Schema{}, ErrUnnamedEdgeType
		}
		source, err := e.source.resolve(aliases, s.Nodes)
		if err != nil {
			return schema.Schema{}, err
		}
		target, err := e.target.resolve(aliases, s.Nodes)
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

// resolve maps an edge endpoint to the canonical key of the declared node type it
// names: an alias via the alias table, or an inline filler via its own label set.
// Either way the resolved type must have been declared.
func (ref rawEndpoint) resolve(aliases map[string]graph.LabelSetKey, nodes map[graph.LabelSetKey]schema.NodeType) (graph.LabelSetKey, error) {
	var key graph.LabelSetKey
	if ref.alias != "" {
		k, ok := aliases[ref.alias]
		if !ok {
			return "", ErrUnknownEndpoint
		}
		key = k
	} else {
		key = ref.labels.Key()
	}

	if _, ok := nodes[key]; !ok {
		return "", ErrUnknownEndpoint
	}
	return key, nil
}
