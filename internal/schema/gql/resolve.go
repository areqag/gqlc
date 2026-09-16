package gql

import (
	"fmt"
	"slices"

	"github.com/areqag/gqlc/internal/graph"
	"github.com/areqag/gqlc/internal/schema"
)

// rawSchema is the complete unresolved intermediate form produced by the walk:
// the graph type name plus the raw node and edge types (whose endpoints are
// rawEndpoints). It is the boundary between the two stages — the listener fills it
// from the parse tree, then resolve() turns it into the final schema.Schema — so
// resolution stays pure Go, independent of ANTLR and testable on its own.
type rawSchema struct {
	name string
	// copyRef is the lowered COPY OF source, nil when the declaration carried an
	// inline body instead. A file with one declares no element types of its own,
	// so resolve() is never called on it — the Loader follows the reference and
	// resolves the file at the end of the chain (ADR 0034 §3.6).
	copyRef *reference
	nodes   []rawNode
	edges   []rawEdge
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
	if err := r.resolveNodes(s.Nodes, idx); err != nil {
		return schema.Schema{}, err
	}
	if err := r.resolveEdges(s.Edges, idx); err != nil {
		return schema.Schema{}, err
	}
	return s, nil
}

// resolveNodes is the first phase: it fills nodes with every declared node type
// and idx with what the edge phase will resolve endpoints against.
func (r rawSchema) resolveNodes(nodes map[graph.LabelSetKey]schema.NodeType, idx nodeIndex) error {
	nodeKeyLabels := make(map[string]bool)
	nodeImplied := make([]graph.LabelSet, 0, len(r.nodes))
	for _, n := range r.nodes {
		key, complete, ok := labelSets(n.hasKeyLabelSet, n.keyLabels, n.impliedLabels)
		if !ok {
			return ErrUnnamedNodeType
		}
		if _, dup := nodes[key]; dup {
			return ErrDuplicateNodeType
		}
		nodes[key] = schema.NodeType{
			KeyLabels:      key,
			CompleteLabels: complete,
			Name:           n.name,
			Properties:     n.props,
		}
		for _, label := range key.Split() {
			nodeKeyLabels[label] = true
		}
		idx.record(n, key, complete)
		if n.hasKeyLabelSet {
			nodeImplied = append(nodeImplied, n.impliedLabels)
		}
	}
	// Only a `=>` declaration has genuinely implied labels. Without one the
	// implied content IS the key label set under GG22, so feeding it in would
	// make every ordinary declaration collide with itself.
	//
	// Deferred past the loop because a collision is order-independent: a later
	// declaration's key label can collide with an earlier one's implied label.
	return rejectInheritance(nodeImplied, nodeKeyLabels)
}

// resolveEdges is the second phase: every edge's endpoints resolved against idx,
// which the node phase has finished filling.
func (r rawSchema) resolveEdges(edges map[schema.EdgeKey]schema.EdgeType, idx nodeIndex) error {
	edgeKeyLabels := make(map[string]bool)
	edgeImplied := make([]graph.LabelSet, 0, len(r.edges))
	for _, e := range r.edges {
		key, complete, ok := labelSets(e.hasKeyLabelSet, e.keyLabels, e.impliedLabels)
		if !ok {
			return ErrUnnamedEdgeType
		}
		if len(key.Split()) > 1 {
			return ErrMultiLabelEdgeType
		}
		edgeKey, err := e.resolveKey(idx, key)
		if err != nil {
			return err
		}
		if _, dup := edges[edgeKey]; dup {
			return ErrDuplicateEdgeType
		}
		edges[edgeKey] = schema.EdgeType{
			EdgeKey:        edgeKey,
			CompleteLabels: complete,
			Name:           e.name,
			Properties:     e.props,
		}
		for _, label := range key.Split() {
			edgeKeyLabels[label] = true
		}
		if e.hasKeyLabelSet {
			edgeImplied = append(edgeImplied, e.impliedLabels)
		}
	}
	return rejectInheritance(edgeImplied, edgeKeyLabels)
}

// resolveKey resolves the edge's two endpoints, source first, and pairs them
// with its key label set.
func (e rawEdge) resolveKey(idx nodeIndex, key graph.LabelSetKey) (schema.EdgeKey, error) {
	source, err := e.source.resolve(idx)
	if err != nil {
		return schema.EdgeKey{}, err
	}
	target, err := e.target.resolve(idx)
	if err != nil {
		return schema.EdgeKey{}, err
	}
	return schema.EdgeKey{Source: source, KeyLabels: key, Target: target}, nil
}

// labelSets turns a declaration's two raw label sets into the pair the model
// stores: the key label set that identifies the type, and the complete label set
// its elements carry. ok is false when the key label set comes out empty, which
// leaves the type with no identity — the caller supplies the node or edge
// sentinel for that.
//
// hasKey distinguishes the two ways a key label set can be empty, and they do not
// resolve alike. Without a `=>` there is no declared key label set at all, so one
// is inferred from the whole phrase (optional feature GG22) and coincides with the
// complete set. With a `=>` the author declared the key label set explicitly
// (GG21) and an empty one is an empty identity, so `(=> :Thing)` is rejected
// rather than quietly re-inferred from the content it implies — that would
// contradict what was written.
func labelSets(hasKey bool, key, implied graph.LabelSet) (keyKey, complete graph.LabelSetKey, ok bool) {
	if !hasKey {
		inferred := implied.Key()
		return inferred, inferred, inferred != ""
	}
	keyKey = key.Key()
	if keyKey == "" {
		return "", "", false
	}
	return keyKey, append(slices.Clone(key), implied...).Key(), true
}

// rejectInheritance refuses any declaration that implies a label some declaration
// also holds as a key label. It is the one part of GG21 gqlc declines, and the
// reason is that the two implementations of the syntax disagree about what it
// means: given `(:Person {name STRING})` beside `(:Engineer => :Person)`,
// Microsoft Fabric inherits Person's properties onto Engineer while Neo4j
// forbids the schema outright, a label there being identifying or implied but
// never both. Whether ISO/IEC 39075:2024 mandates the inheritance is unresolved —
// the normative prose is in the paid PDF, which gqlc-lir declined to buy — so
// accepting the schema means picking a vendor's answer and calling it the
// standard. Rejecting is the same move ErrEdgeKindArcMismatch makes for the same
// reason, under the no-dialect principle. See ADR 0015.
//
// The check is per-label rather than per-label-set: `(:A&B)` holds both A and B
// as key labels, so implying either collides. Node and edge labels are checked
// against their own kind only — they are separate namespaces, keying separate
// maps, and an edge type labelled KNOWS says nothing about a node type's Person.
//
// Everything else GG21 admits is implemented, because the implied content is
// declared inline (nodeTypeImpliedContent, GQL.g4:1526-1530) and needs no
// cross-declaration reading to interpret. Where no key label is implied, the two
// vendors agree, and that is exactly the subset this leaves standing.
func rejectInheritance(implied []graph.LabelSet, keyLabels map[string]bool) error {
	for _, set := range implied {
		for _, label := range set {
			if keyLabels[label] {
				return fmt.Errorf("%w: %s", ErrImpliedLabelIsKeyLabel, label)
			}
		}
	}
	return nil
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

// record enters one declared node type: its alias, if it binds one, and every
// identifier it answers to.
func (idx nodeIndex) record(n rawNode, key, complete graph.LabelSetKey) {
	if n.alias != "" {
		idx.aliases[n.alias] = key
	}
	if n.name != "" {
		idx.declared[n.name] = true
	}
	// Every label the type answers to, implied ones included: idx.declared
	// exists to tell "you named a type where an alias belongs" apart from
	// "no such type", and an author who writes an implied label has made
	// that same mistake.
	for _, label := range complete.Split() {
		idx.declared[label] = true
	}
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
