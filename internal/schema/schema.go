package schema

import (
	"cmp"
	"encoding/json"
	"slices"
	"strings"
)

// Schema is the parsed representation of a directed, property graph type.
type Schema struct {
	Name  string // graph type name
	Nodes map[LabelSetKey]NodeType
	Edges map[EdgeKey]EdgeType
}

type NodeType struct {
	Labels     LabelSetKey         `json:"labels"` // canonical identity; also the Nodes map key
	Name       string              `json:"name"`
	Properties map[string]Property `json:"properties"`
}

type EdgeType struct {
	EdgeKey                        // Source, Label, Target; also the Edges map key. Promoted inline in JSON.
	Name       string              `json:"name"`
	Properties map[string]Property `json:"properties"`
}

// EdgeKey identifies an edge type. The same labels may connect different
// endpoint pairs, so identity is the triple, not the edge labels alone.
type EdgeKey struct {
	Source LabelSetKey `json:"source"`
	Label  LabelSetKey `json:"label"`
	Target LabelSetKey `json:"target"`
}

// Property is a single typed attribute on an entity in the graph.
type Property struct {
	Name     string       `json:"name"`
	Type     PropertyType `json:"type"`
	Nullable bool         `json:"nullable"`
}

// MarshalJSON renders the schema in a deterministic, stable form so every
// consumer — golden tests today, generated output later — is idempotent
// regardless of Go's randomized map iteration order. Its only job is to turn the
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

// PropertyType is the normalized value type of a property. Numeric types keep
// their bit width rather than collapsing to a single Int/Float, so codegen can
// preserve the signedness and width the schema author stated (ADR 0002).
type PropertyType string

const (
	TypeString    PropertyType = "STRING"
	TypeBool      PropertyType = "BOOL"
	TypeDate      PropertyType = "DATE"
	TypeTimestamp PropertyType = "TIMESTAMP"

	TypeInt    PropertyType = "INT"
	TypeInt8   PropertyType = "INT8"
	TypeInt16  PropertyType = "INT16"
	TypeInt32  PropertyType = "INT32"
	TypeInt64  PropertyType = "INT64"
	TypeInt128 PropertyType = "INT128"
	TypeInt256 PropertyType = "INT256"

	TypeUint    PropertyType = "UINT"
	TypeUint8   PropertyType = "UINT8"
	TypeUint16  PropertyType = "UINT16"
	TypeUint32  PropertyType = "UINT32"
	TypeUint64  PropertyType = "UINT64"
	TypeUint128 PropertyType = "UINT128"
	TypeUint256 PropertyType = "UINT256"

	TypeFloat    PropertyType = "FLOAT"
	TypeFloat16  PropertyType = "FLOAT16"
	TypeFloat32  PropertyType = "FLOAT32"
	TypeFloat64  PropertyType = "FLOAT64"
	TypeFloat128 PropertyType = "FLOAT128"
	TypeFloat256 PropertyType = "FLOAT256"

	TypeDecimal PropertyType = "DECIMAL"
)

// LabelSet is a set of labels in source form. It is the input used to build
// a LabelSetKey; the parsed model stores identity as the key, not the slice.
type LabelSet []string

// LabelSetKey is the canonical, comparable form of a LabelSet, usable as a
// map key: labels sorted, deduplicated, and joined with "&".
type LabelSetKey string

// Key canonicalizes the set into its map key. The original slice is left
// unmodified.
func (ls LabelSet) Key() LabelSetKey {
	sorted := slices.Clone(ls)
	slices.Sort(sorted)
	sorted = slices.Compact(sorted)
	return LabelSetKey(strings.Join(sorted, "&"))
}

// Split returns the individual labels encoded in the key. It is the inverse
// of LabelSet.Key. The empty key yields no labels.
func (k LabelSetKey) Split() LabelSet {
	if k == "" {
		return nil
	}
	return strings.Split(string(k), "&")
}
