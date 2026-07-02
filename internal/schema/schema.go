package schema

import (
	"cmp"
	"encoding/json"
	"slices"

	"github.com/areqag/gqlc/internal/graph"
)

// Schema is the parsed representation of a directed, property graph type.
type Schema struct {
	Name  string // graph type name
	Nodes map[graph.LabelSetKey]NodeType
	Edges map[EdgeKey]EdgeType
}

// NodeType is a kind of vertex in the graph type: a label set (its canonical
// identity) plus a set of properties.
type NodeType struct {
	Labels     graph.LabelSetKey   `json:"labels"` // canonical identity; also the Nodes map key
	Name       string              `json:"name"`
	Properties map[string]Property `json:"properties"`
}

// EdgeType is a kind of directed relationship between two node types: its
// EdgeKey identity (source, label, target) plus a set of properties.
type EdgeType struct {
	EdgeKey                        // Source, Label, Target; also the Edges map key. Promoted inline in JSON.
	Name       string              `json:"name"`
	Properties map[string]Property `json:"properties"`
}

// EdgeKey identifies an edge type. The same labels may connect different
// endpoint pairs, so identity is the triple, not the edge labels alone.
type EdgeKey struct {
	Source graph.LabelSetKey `json:"source"`
	Label  graph.LabelSetKey `json:"label"`
	Target graph.LabelSetKey `json:"target"`
}

// Property is a single typed attribute on an entity in the graph.
type Property struct {
	Name     string             `json:"name"`
	Type     graph.PropertyType `json:"type"`
	Nullable bool               `json:"nullable"`
}

// MarshalJSON renders the schema in a deterministic, stable form so every
// consumer — golden tests today, generated output later — is idempotent
// regardless of Go's randomised map iteration order. Its only job is to turn the
// node and edge maps into slices sorted by identity (node label set; edge
// source/label/target); the elements are the real NodeType/EdgeType, so there's
// no parallel JSON model to keep in sync. A custom marshal is needed because
// encoding/json can't render the Edges map directly — its key is a struct
// (EdgeKey), not a valid JSON object key — and because sorted arrays read better
// than objects keyed by identity. Within each element, Properties stays a map and
// marshals as an object whose keys Go already sorts.
func (s Schema) MarshalJSON() ([]byte, error) {
	nodes := make([]NodeType, 0, len(s.Nodes))
	for _, n := range s.Nodes {
		nodes = append(nodes, n)
	}
	slices.SortFunc(nodes, func(a, b NodeType) int { return cmp.Compare(a.Labels, b.Labels) })

	edges := make([]EdgeType, 0, len(s.Edges))
	for _, e := range s.Edges {
		edges = append(edges, e)
	}
	slices.SortFunc(edges, func(a, b EdgeType) int {
		return cmp.Or(
			cmp.Compare(a.Source, b.Source),
			cmp.Compare(a.Label, b.Label),
			cmp.Compare(a.Target, b.Target),
		)
	})

	return json.Marshal(struct {
		Name  string     `json:"name"`
		Nodes []NodeType `json:"nodes"`
		Edges []EdgeType `json:"edges"`
	}{Name: s.Name, Nodes: nodes, Edges: edges})
}
