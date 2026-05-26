package graph

// EntityKind distinguishes the two kinds of entity in a property graph: a node
// or an edge.
type EntityKind int

const (
	Node EntityKind = iota
	Edge
)

// String is the lowercase name of the kind ("node" / "edge"). It is the single
// source the query model's JSON discriminator derives from, so the serialised
// tag can never drift from the enum.
func (k EntityKind) String() string {
	switch k {
	case Edge:
		return "edge"
	default:
		return "node"
	}
}
