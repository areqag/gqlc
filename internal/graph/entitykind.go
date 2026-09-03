package graph

// EntityKind distinguishes the two kinds of entity in a property graph: a node
// or an edge.
type EntityKind int

// The two entity kinds; the query model's binding sum mirrors this split.
const (
	Node EntityKind = iota
	Edge
)

// String is the lowercase name of the kind ("node" / "edge").
//
// It is not what the query model's JSON discriminator derives from, though this
// comment claimed so until bd gqlc-r79zi. That tag comes from
// query.BindingKind.String, a different sum in a different package, which
// answers for three further kinds this one has no member for. The two share the
// spellings "node" and "edge" by construction and nothing enforces the match.
// Measured 2026-09-03: deleting this method leaves `go build ./...` clean, so it
// has no production caller at all.
func (k EntityKind) String() string {
	switch k {
	case Node:
		return "node"
	case Edge:
		return "edge"
	}
	// Below the switch rather than in a `default`, so `exhaustive` still checks
	// this stringer for a missing arm. Every declared member has an arm above,
	// so this answers only for a value outside the enum — which is what the
	// deleted `default` answered for it too (bd gqlc-r79zi, the precedent set by
	// gqlc-5225b).
	return "node"
}
