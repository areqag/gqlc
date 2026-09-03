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
// It is not itself the query model's JSON discriminator, though this comment
// claimed so until bd gqlc-r79zi. That tag comes from query.BindingKind.String,
// a different sum in a different package, which answers for three further kinds
// this one has no member for — but that sum's "node" and "edge" arms call this
// method rather than spelling the two words a second time (bd gqlc-avtrx). So
// this method reaches the wire by way of that one, and its spellings are not
// free to change: internal/query's suite reds if they do.
//
// Until gqlc-avtrx the two pairs of literals matched "by construction" alone.
// Measured then, on the pair as it stood: rewriting "node" here left the whole
// of internal/query green, with only this package's own test to catch it.
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
