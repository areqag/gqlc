package graph

// EntityKind distinguishes the two kinds of entity in a property graph: a node
// or an edge.
type EntityKind int

const (
	Node EntityKind = iota
	Edge
)
