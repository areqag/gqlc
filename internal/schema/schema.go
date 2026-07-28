package schema

import (
	"cmp"
	"encoding/json"
	"slices"

	"github.com/areqag/gqlc/internal/graph"
)

// Schema is the parsed representation of a directed, property graph type.
//
// # Key label sets versus complete label sets
//
// ISO GQL distinguishes an element type's *key* label set — the labels that
// identify it — from its *complete* label set, which additionally carries any
// labels the declaration implies. The grammar names the split: in
// `nodeTypeKeyLabelSet : labelSetPhrase? IMPLIES` the phrase before `=>` is the
// key label set and everything after it is implied content. Annex D's optional
// feature GG25, "Relaxed key label set uniqueness for edge types", is what
// establishes the base rule — a feature that relaxes key-label-set uniqueness
// presupposes that uniqueness is keyed on the key label set.
//
// So identity is the key label set: Nodes is keyed by NodeType.KeyLabels and
// EdgeKey.KeyLabels carries the edge's. Matching, by contrast, is defined on the
// complete label set — an element carries its implied labels, so a query label
// expression is satisfied against CompleteLabels.
//
// gqlc does not implement GG21 (explicit key label sets via `=>`), so every type
// this package builds today has an *inferred* key label set (GG22) that happens
// to equal its complete one. Representing that coincidence rather than assuming
// it is the point: the two roles were previously served by one field, and any
// consumer that picked the wrong one could not be caught.
type Schema struct {
	Name  string // graph type name
	Nodes map[graph.LabelSetKey]NodeType
	Edges map[EdgeKey]EdgeType
}

// NodeType is a kind of vertex in the graph type: a key label set (its
// identity), a complete label set (what its elements carry), and properties.
type NodeType struct {
	KeyLabels      graph.LabelSetKey   `json:"key_labels"`      // identity; also the Nodes map key
	CompleteLabels graph.LabelSetKey   `json:"complete_labels"` // key labels plus any implied; what matching tests against
	Name           string              `json:"name"`
	Properties     map[string]Property `json:"properties"`
}

// EdgeType is a kind of directed relationship between two node types: its
// EdgeKey identity plus a complete label set and a set of properties.
type EdgeType struct {
	EdgeKey                            // Source, KeyLabels, Target; also the Edges map key. Promoted inline in JSON.
	CompleteLabels graph.LabelSetKey   `json:"complete_labels"`
	Name           string              `json:"name"`
	Properties     map[string]Property `json:"properties"`
}

// EdgeKey identifies an edge type. The same key labels may connect different
// endpoint pairs, so identity is the triple, not the edge labels alone.
type EdgeKey struct {
	Source    graph.LabelSetKey `json:"source"` // the source node type's key label set
	KeyLabels graph.LabelSetKey `json:"key_labels"`
	Target    graph.LabelSetKey `json:"target"` // the target node type's key label set
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
	slices.SortFunc(nodes, func(a, b NodeType) int { return cmp.Compare(a.KeyLabels, b.KeyLabels) })

	edges := make([]EdgeType, 0, len(s.Edges))
	for _, e := range s.Edges {
		edges = append(edges, e)
	}
	slices.SortFunc(edges, func(a, b EdgeType) int {
		return cmp.Or(
			cmp.Compare(a.Source, b.Source),
			cmp.Compare(a.KeyLabels, b.KeyLabels),
			cmp.Compare(a.Target, b.Target),
		)
	})

	return json.Marshal(struct {
		Name  string     `json:"name"`
		Nodes []NodeType `json:"nodes"`
		Edges []EdgeType `json:"edges"`
	}{Name: s.Name, Nodes: nodes, Edges: edges})
}
