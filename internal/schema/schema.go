package schema

import (
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
	Labels     LabelSetKey // canonical identity; also the Nodes map key
	Name       string
	Properties map[string]Property
}

type EdgeType struct {
	EdgeKey    // Source, Label, Target; also the Edges map key
	Properties map[string]Property
}

// EdgeKey identifies an edge type. The same labels may connect different
// endpoint pairs, so identity is the triple, not the edge labels alone.
type EdgeKey struct {
	Source LabelSetKey
	Label  LabelSetKey
	Target LabelSetKey
}

// Property is a single typed attribute on an entity in the graph.
type Property struct {
	Name     string
	Type     PropertyType
	Nullable bool
}

type PropertyType string

const (
	TypeString    PropertyType = "STRING"
	TypeInt       PropertyType = "INT"
	TypeFloat     PropertyType = "FLOAT"
	TypeBool      PropertyType = "BOOL"
	TypeDate      PropertyType = "DATE"
	TypeTimestamp PropertyType = "TIMESTAMP"
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
