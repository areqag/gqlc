package query

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/areqag/gqlc/internal/graph"
)

// Query is the model of a single parsed query: its UNION-joined branches and the
// parameters it takes. It is schema-agnostic — it records what the query says,
// not whether any schema supports it; resolving it against a schema.Schema is a
// separate stage (ADR 0003).
//
// The branch/part nesting mirrors the grammar (oC_RegularQuery → oC_SingleQuery
// → oC_SinglePartQuery/oC_MultiPartQuery), so the structure is always present:
// the common case MATCH (n) RETURN n is one branch of one part. The resolver
// never special-cases "flat vs nested" (Stage-4 spec §3).
//
// Query needs no custom MarshalJSON: its members are order-preserving slices of
// products and sum-type values, so its serialisation is deterministic by
// construction (the sum types carry the determinism discipline themselves). The
// lowercase json tags fix the wire key names; UnionKind marshals via its
// stringer.
type Query struct {
	// Branches are the query's UNION-joined result arms, one per oC_SingleQuery,
	// in source order. A query without UNION is one branch; N UNIONs make N+1
	// branches combined left to right. Always at least one branch.
	Branches []Branch `json:"branches"`

	// Combinators records how each branch after the first was joined to its
	// predecessor: the i-th entry is how branch i+1 joins branch i (UNION distinct
	// vs UNION ALL). It has len(Branches)-1 entries — nil (one branch). Always
	// emitted in JSON (null when one branch), matching the always-emit convention.
	Combinators []UnionKind `json:"combinators"`

	// Parameters are the query's inputs, deduplicated by name in first-appearance
	// order. They stay at Query level: a parameter used in any part of any branch
	// is one generated method argument, deduplicated query-wide (Stage-4 spec §2).
	Parameters []Parameter `json:"parameters"`

	// StatementKind is the read/write axis the driver's transaction mode is
	// chosen from (Stage 12): StatementWrite iff the query contains at least one
	// write clause at outer scope of any branch/part (a write suppressed inside
	// an EXISTS { ... } subquery does not flip the axis — the outer query does
	// not modify the graph). Binary, not three-state: a write followed by RETURN
	// is still a write, so the driver picks writeTx unambiguously. Zero value is
	// StatementRead, so a pre-Stage-12 query wire shape is a strict additive
	// extension.
	StatementKind StatementKind `json:"statementKind"`
}

// Branch is one UNION-joined arm of a query — one oC_SingleQuery — an
// ordered chain of one or more Parts. Non-final parts each end in a WITH;
// the final part ends in a RETURN (positional — no per-part terminal flag). It
// is a product type: exported fields, the builder maintains the invariant (at
// least one part), no smart constructor — mirroring Query (Stage-4 spec §3).
type Branch struct {
	// Parts are the branch's WITH-bounded scope segments, in source order. At
	// least one (the final RETURN part).
	Parts []Part `json:"parts"`
}

// Part is one WITH-bounded scope segment of a branch — the Stage-0..3 flat
// scope, now scoped to one part. A non-final part's Returns/ReturnsAll carry its
// WITH projection (a WITH item is a RETURN item — same oC_ProjectionBody, same
// Stage-3 Projection sum); the final part's carry the branch's result columns.
// Stage 12: Effects carries the part's write clauses in walk order; a part with
// non-empty Effects and empty Returns / ReturnsAll=false is a projection-less
// write, a legal complete shape whose result is zero columns.
// It is a product type with exported fields, but Stage 12 introduces the
// NewPart smart constructor to enforce the "at least one of bindings, effects,
// or projection" invariant — an all-empty Part is unrepresentable at
// construction. The listener routes through NewPart (build.go's buildPart) so
// any parse path that would yield an empty Part fails at build time. Direct
// struct-literal construction (mustParse fixtures) bypasses the guard; those
// callers are trusted to supply a well-formed shape, and the sweep-time test
// TestNewPartRejectsEmpty exercises the adversarial construction against the
// constructor.
type Part struct {
	// Bindings are the entities this part's own MATCH or CREATE clauses
	// introduce, a NodeBinding, EdgeBinding, PathBinding, or UnwindBinding each.
	// Among a part's named bindings the variable is unique; Returns and edge
	// endpoints reference them by it (or a name the prior part's WITH carried
	// forward). Only an edge may be anonymous. Stage 12: CREATE-introduced
	// bindings enter this slice via the same collectPattern path MATCH uses; the
	// CreateEffect on Effects records which bindings this specific clause
	// introduced.
	Bindings []Binding `json:"bindings"`

	// Returns are the part's result columns, in source order with duplicates kept:
	// RETURN a, b is a different shape from RETURN b, a. Empty when ReturnsAll is
	// true (WITH * / RETURN * does not mix with explicit items). Stage 12: may
	// also be empty for a projection-less write (a part whose only clauses are
	// writes and that ends the branch — codegen emits a no-result method).
	Returns []ReturnItem `json:"returns"`

	// ReturnsAll is true iff the projection body was the '*' alternative
	// (WITH * / RETURN *). A query-level wildcard over the part's in-scope
	// bindings, not a return item; the resolver owns expansion. When true, Returns
	// is empty. Always emitted in JSON (matching the always-emit convention).
	ReturnsAll bool `json:"returnsAll"`

	// Distinct is true iff the part's projection body carried the DISTINCT
	// keyword (RETURN DISTINCT … or WITH DISTINCT …). Composes freely with
	// ReturnsAll — RETURN DISTINCT * and WITH DISTINCT * are legal openCypher
	// shapes and lower with both flags set. Independent of the two other
	// DISTINCT axes in the model: AggregateProjection.Distinct (an aggregate
	// deduplicates its input rows before aggregation) and UnionKind (UNION
	// DISTINCT vs. UNION ALL deduplicates across two branches' output rows).
	// Each axis is on a distinct model surface and captures a different
	// cardinality-affecting decision. Always emitted in JSON.
	Distinct bool `json:"distinct"`

	// Effects are the write clauses in this part, in walk order — the per-part
	// analogue of Returns for the write side (Stage 12). Each write clause
	// contributes one or more Effects: a CREATE clause contributes one
	// CreateEffect; a DELETE contributes one DeleteEffect; a SET contributes one
	// effect per SetItem; a REMOVE contributes one effect per RemoveItem. A part
	// with no Effects is read-only. Always emitted in JSON.
	Effects []Effect `json:"effects"`
}

// ErrEmptyPart rejects a Part whose bindings, projection, and effects are all
// empty — a shape the grammar rules out (oC_ReadingClause / oC_UpdatingClause /
// oC_ProjectionBody each guarantee at least one non-empty field in the parsed
// Part) but the model constructor still refuses, per Stage 12 §3.2 / §4.6.
// Illegal states unrepresentable at construction: no parse path can reach this
// sentinel, so it is a model-invariant guard, not a user-facing rejection.
// It is deliberately NOT in cypher's sentinel-reachability sweep — the sweep
// checks user-reachable sentinels, and this one is exercised only by the
// adversarial constructor test TestNewPartRejectsEmpty in query_test.go.
var ErrEmptyPart = errors.New("query: part must carry at least one binding, projection, or effect")

// NewPart is the smart constructor for a Part (Stage 12). It enforces the
// "at least one of bindings, projection, or effect" invariant that the flat-
// struct field-level types cannot express alone. Callers pass the fields in
// walk order; NewPart rejects the all-empty shape with ErrEmptyPart, and any
// well-formed part passes through unchanged. The distinct axis (part-distinct-
// axis spec §1) does not satisfy the invariant on its own: DISTINCT is a
// modifier on a projection that must exist, so distinct=true with every
// other axis empty is still ErrEmptyPart.
//
// buildPart in the cypher listener routes through NewPart so a parse path that
// would yield an empty Part (which the grammar rules out anyway) fails at
// build time rather than emitting a wire-shape violation. Direct struct-literal
// construction in tests (mustParse fixtures) bypasses this guard by design —
// those callers are trusted to hand-write a well-formed shape.
func NewPart(bindings []Binding, returns []ReturnItem, returnsAll bool, distinct bool, effects []Effect) (Part, error) {
	if len(bindings) == 0 && len(returns) == 0 && !returnsAll && len(effects) == 0 {
		return Part{}, ErrEmptyPart
	}
	return Part{
		Bindings:   bindings,
		Returns:    returns,
		ReturnsAll: returnsAll,
		Distinct:   distinct,
		Effects:    effects,
	}, nil
}

// UnionKind is which UNION combinator joins two branches: distinct (collapses
// duplicate result rows) or ALL (keeps duplicates). The distinction changes
// result cardinality, which the generated code models — the branch-level
// analogue of the aggregate kind. It is an int-backed enum with a stringer,
// mirroring AggregateFunc / ClauseSlot; the JSON value derives from String, the
// single source, so it cannot drift.
type UnionKind int

const (
	// UnionDistinct is plain UNION: it collapses duplicate result rows.
	UnionDistinct UnionKind = iota
	// UnionAll is UNION ALL: it keeps duplicate result rows.
	UnionAll
)

// String is the canonical wire name of the combinator ("union" / "unionAll").
// It is the single source the JSON value derives from, so the serialised name
// can never drift from the enum. The default arm is UnionDistinct (plain UNION).
func (k UnionKind) String() string {
	switch k {
	case UnionAll:
		return "unionAll"
	default:
		return "union"
	}
}

// MarshalJSON renders a UnionKind as its wire string (derived from String, the
// single source), so the combinator serialises to a stable scalar matching the
// always-emit convention the other enums follow.
func (k UnionKind) MarshalJSON() ([]byte, error) {
	return json.Marshal(k.String())
}

// StatementKind is the query-wide read/write axis (Stage 12): the driver
// chooses its transaction mode from this scalar — a readTx cannot execute a
// CREATE, and a writeTx executes both reads and writes, so the axis is binary.
// Int-backed with a stringer, mirroring UnionKind / AggregateFunc / ClauseSlot;
// the JSON value derives from String, the single source, so it cannot drift.
type StatementKind int

const (
	// StatementRead is a query composed only of reading clauses (MATCH, WITH,
	// UNION, UNWIND, RETURN, …). The zero-value default, so a pre-Stage-12
	// query wire shape is a strict additive extension.
	StatementRead StatementKind = iota
	// StatementWrite is a query that contains at least one write clause
	// (CREATE, DELETE, SET, REMOVE, and (Stage 13) MERGE) at outer scope. A
	// write followed by RETURN is still a write — the driver's tx mode is
	// binary. Writes suppressed inside an EXISTS { ... } subquery do not
	// flip the axis; the outer query does not modify the graph.
	StatementWrite
)

// String is the canonical wire name of the kind ("read" / "write"). It is the
// single source the JSON value derives from, so the serialised name can never
// drift from the enum. The default arm is StatementRead.
func (k StatementKind) String() string {
	if k == StatementWrite {
		return "write"
	}
	return "read"
}

// MarshalJSON renders a StatementKind as its wire string (derived from String,
// the single source), matching the always-emit convention.
func (k StatementKind) MarshalJSON() ([]byte, error) {
	return json.Marshal(k.String())
}

// BindingKind is which kind of query-level binding a value is (Stage 8 spec §3.1):
// a node, an edge, or a path. Two of the three tags overlap with graph.EntityKind's
// stringer output ("node"/"edge"), so the wire-encoded discriminator stays
// stable for NodeBinding / EdgeBinding across the Stage-8 widening; "path" is new
// for PathBinding. Int-backed with a stringer — mirrors AggregateFunc / UnionKind —
// so the JSON discriminator derives from one source and cannot drift.
type BindingKind int

const (
	// BindingNode is a node binding — the query-level projection of graph.Node.
	BindingNode BindingKind = iota
	// BindingEdge is an edge binding — the query-level projection of graph.Edge.
	BindingEdge
	// BindingPath is a named-path binding — a Stage-8 construct with no
	// graph.EntityKind counterpart (a path is not a graph entity, it is a
	// query-level composition of them).
	BindingPath
	// BindingUnwind is an UNWIND-introduced binding — a Stage-9 construct
	// with no graph.EntityKind counterpart (a scalar drawn from a list is
	// not a graph entity). The binding carries the source expression's
	// element type as recorded by the Stage-6 typer.
	BindingUnwind
	// BindingCall is a CALL YIELD binding — a Stage-14 construct: one
	// binding per YIELD result column, each carrying the fully-qualified
	// procedure name, the signature-declared source column, and the
	// bridged Stage-6 result type. No graph.EntityKind counterpart (a
	// procedure result column is a scalar per row, not a graph entity).
	// CALL is a Read clause, so a CallBinding does not flip
	// Query.StatementKind.
	BindingCall
)

// String is the canonical lowercase name of the kind ("node" / "edge" /
// "path" / "unwind" / "call"). It is the single source the JSON
// discriminator derives from; two of the five tags match
// graph.EntityKind.String() by construction, so pre-Stage-8 wire shapes
// for node/edge bindings are preserved verbatim. "path" (Stage 8),
// "unwind" (Stage 9), and "call" (Stage 14) are query-side only.
func (k BindingKind) String() string {
	switch k {
	case BindingEdge:
		return "edge"
	case BindingPath:
		return "path"
	case BindingUnwind:
		return "unwind"
	case BindingCall:
		return "call"
	default:
		return "node"
	}
}

// Binding is a query variable bound to a graph entity, a named path, or an
// UNWIND source's per-row element. Entity bindings carry labels; a path
// binding carries member names; an unwind binding carries the source list's
// element type. It is a closed sum of NodeBinding, EdgeBinding, (Stage 8)
// PathBinding, and (Stage 9) UnwindBinding — no other type can implement
// it — so a binding is exactly one of the four. Every variant holds its
// data in unexported fields, so the smart constructors are the only way
// to construct a non-zero value: the invariants the types alone cannot
// express (a non-empty node variable, both edge endpoints present, a
// non-empty path variable with at least one member, a non-empty UNWIND
// variable) hold for every value that exists.
type Binding interface {
	// Kind reports whether the binding is a node, an edge, a path, or an
	// UNWIND-introduced per-row element.
	Kind() BindingKind
	// Nullable reports whether the binding was first introduced inside an
	// OPTIONAL MATCH clause (ADR 0006). The flag is a static, local fact set
	// by the parser; flow-typing across clauses lives in the resolver.
	Nullable() bool
	isBinding()
}

// NodeBinding is a query variable bound to a node, carrying its labels as
// written. Labels may be empty when the variable is unlabelled (the b in
// (a:Person)-[:KNOWS]->(b)); the resolver infers its type from the edges that
// touch it. A node binding is never anonymous — an anonymous node is a pure
// filter, not a binding — so its variable is always non-empty (NewNodeBinding).
type NodeBinding struct {
	variable string         // the name as written: the p in (p:Person)
	labels   graph.LabelSet // labels as written; may be empty
	nullable bool           // set when first introduced in OPTIONAL MATCH (ADR 0006)
}

// NewNodeBinding builds a NodeBinding, rejecting the empty variable: an anonymous
// node is never a binding (Stage-0 spec, C3). Labels may be empty (C7).
func NewNodeBinding(variable string, labels graph.LabelSet) (NodeBinding, error) {
	if variable == "" {
		return NodeBinding{}, errors.New("query: node binding requires a non-empty variable")
	}
	return NodeBinding{variable: variable, labels: labels}, nil
}

// NewNullableNodeBinding builds the OPTIONAL-introduced variant (ADR 0006): same
// invariants as NewNodeBinding, with the Nullable flag set.
func NewNullableNodeBinding(variable string, labels graph.LabelSet) (NodeBinding, error) {
	b, err := NewNodeBinding(variable, labels)
	if err != nil {
		return NodeBinding{}, err
	}
	b.nullable = true
	return b, nil
}

// Variable is the name as written: the p in (p:Person). Always non-empty.
func (b NodeBinding) Variable() string { return b.variable }

// Labels are the labels as written; may be empty (C7).
func (b NodeBinding) Labels() graph.LabelSet { return b.labels }

// Kind reports that a NodeBinding is a node.
func (NodeBinding) Kind() BindingKind { return BindingNode }

// EntityKind returns the graph-vocabulary kind of the entity this binding
// refers to (graph.Node). Only entity bindings (NodeBinding, EdgeBinding)
// expose EntityKind — a path is not a graph entity, so PathBinding has no
// equivalent method. The resolver reads EntityKind to form the schema key.
func (NodeBinding) EntityKind() graph.EntityKind { return graph.Node }

// Nullable reports whether the binding was first introduced inside an OPTIONAL
// MATCH clause (ADR 0006).
func (b NodeBinding) Nullable() bool { return b.nullable }

func (NodeBinding) isBinding() {}

// EdgeBinding is a query variable bound to an edge, carrying its labels as
// written, both endpoints, a direction marker, and (Stage 8) an optional hop
// range for variable-length relationships. For a directed edge the endpoints
// are in canonical source->target order (a left-pointing edge is
// canonicalised); for an undirected edge (directed=false) the endpoints are in
// textual order, with no authoritative orientation (the resolver tries both).
// Labels may be empty for an untyped edge (C7) or carry more than one entry
// for a multi-type edge ([r:A|B]). The variable may be empty: unlike a node,
// an anonymous edge is its own binding (the relationship in (a)-->(b)).
// Source and Target are always present (NewEdgeBinding). hops is nil for a
// single-hop edge (Stages 0..7) and non-nil for a variable-length edge (Stage 8);
// a var-length edge binding projects as list<edge>, a single-hop as edge.
type EdgeBinding struct {
	variable string         // the name as written: the r in [r:KNOWS]; empty if anonymous
	labels   graph.LabelSet // labels as written; may be empty; may carry multiple types (Stage 8)
	source   Endpoint       // the source endpoint; always set
	target   Endpoint       // the target endpoint; always set
	nullable bool           // set when first introduced in OPTIONAL MATCH (ADR 0006)
	directed bool           // true for a one-arrow edge; false for an undirected edge (Stage 5)
	hops     *EdgeHops      // Stage 8: nil for single-hop; non-nil for variable-length
}

// NewEdgeBinding builds a single-hop EdgeBinding, rejecting a missing endpoint:
// an edge always has both a source and a target. Variable may be empty (an
// anonymous edge) and Labels may be empty (an untyped edge, C7). directed marks
// a one-arrow edge (true) versus an undirected edge (false). Stage 8: this
// constructor produces a single-hop binding (Hops() == nil); use
// NewVarLengthEdgeBinding for the variable-length shape.
func NewEdgeBinding(variable string, labels graph.LabelSet, source, target Endpoint, directed bool) (EdgeBinding, error) {
	if source == nil || target == nil {
		return EdgeBinding{}, errors.New("query: edge binding requires both a source and a target endpoint")
	}
	return EdgeBinding{variable: variable, labels: labels, source: source, target: target, directed: directed}, nil
}

// NewNullableEdgeBinding builds the OPTIONAL-introduced single-hop variant
// (ADR 0006): same invariants as NewEdgeBinding, with the Nullable flag set.
// The flag is applied uniformly to every binding the OPTIONAL clause
// introduces, including the anonymous-edge case where no Ref will ever read it.
func NewNullableEdgeBinding(variable string, labels graph.LabelSet, source, target Endpoint, directed bool) (EdgeBinding, error) {
	b, err := NewEdgeBinding(variable, labels, source, target, directed)
	if err != nil {
		return EdgeBinding{}, err
	}
	b.nullable = true
	return b, nil
}

// NewVarLengthEdgeBinding builds a variable-length EdgeBinding (Stage 8 spec §3.4):
// the same fields as a single-hop edge, plus an EdgeHops range value. hops is
// stored by pointer so a nil Hops() distinguishes the single-hop case (the
// zero-value of *EdgeHops) from a var-length case whose bounds are both
// unbounded (a non-nil pointer to an EdgeHops{nil, nil}).
func NewVarLengthEdgeBinding(variable string, labels graph.LabelSet, source, target Endpoint, directed bool, hops EdgeHops) (EdgeBinding, error) {
	b, err := NewEdgeBinding(variable, labels, source, target, directed)
	if err != nil {
		return EdgeBinding{}, err
	}
	b.hops = &hops
	return b, nil
}

// NewNullableVarLengthEdgeBinding builds the OPTIONAL-introduced variable-length
// variant. The nullable flag applies to the whole binding uniformly per ADR 0006.
func NewNullableVarLengthEdgeBinding(variable string, labels graph.LabelSet, source, target Endpoint, directed bool, hops EdgeHops) (EdgeBinding, error) {
	b, err := NewVarLengthEdgeBinding(variable, labels, source, target, directed, hops)
	if err != nil {
		return EdgeBinding{}, err
	}
	b.nullable = true
	return b, nil
}

// Variable is the name as written: the r in [r:KNOWS]; empty for an anonymous edge.
func (b EdgeBinding) Variable() string { return b.variable }

// Labels are the labels as written; may be empty (C7).
func (b EdgeBinding) Labels() graph.LabelSet { return b.labels }

// Source is the source endpoint; always set.
func (b EdgeBinding) Source() Endpoint { return b.source }

// Target is the target endpoint; always set.
func (b EdgeBinding) Target() Endpoint { return b.target }

// Directed reports whether the edge was written with a single arrow (true) or as
// an undirected pattern (false, the resolver tries both orientations).
func (b EdgeBinding) Directed() bool { return b.directed }

// Hops reports the variable-length hop range, or nil for a single-hop edge
// (Stages 0..7). A non-nil Hops means the binding projects as list<edge>;
// the resolver reads the min/max to form its endpoint-plus-range lookup.
func (b EdgeBinding) Hops() *EdgeHops { return b.hops }

// Kind reports that an EdgeBinding is an edge.
func (EdgeBinding) Kind() BindingKind { return BindingEdge }

// EntityKind returns the graph-vocabulary kind of the entity this binding
// refers to (graph.Edge). The resolver reads EntityKind to form the schema
// EdgeKey (source label, edge label, target label triple).
func (EdgeBinding) EntityKind() graph.EntityKind { return graph.Edge }

// Nullable reports whether the binding was first introduced inside an OPTIONAL
// MATCH clause (ADR 0006).
func (b EdgeBinding) Nullable() bool { return b.nullable }

func (EdgeBinding) isBinding() {}

// EdgeHops is the hop range of a variable-length relationship (Stage 8 spec §3.3):
// [r*], [r*3], [r*1..3], [r*3..], [r*..5]. Both bounds are optional (nil for
// unbounded), and the constructor rejects illegal ranges (negative bounds, an
// upper bound below the lower bound). Its data fields are unexported so
// NewEdgeHops is the only writer, and the invariants — the ones the type alone
// cannot express — hold for every value that exists.
type EdgeHops struct {
	min *int
	max *int
}

// NewEdgeHops builds an EdgeHops from optional min and max bounds. Rejects a
// negative bound (openCypher integer literals are non-negative, so a negative
// value could never come from a well-formed range literal — this is the sole
// invariant the type alone cannot express).
//
// An empty range (max < min, e.g. `[*2..1]`) is accepted: the openCypher TCK
// includes it as a positive scenario returning zero rows, so the runtime rule
// "no valid hop count satisfies the range" sits below the type-interface
// boundary (ADR 0005). The parser records the range as written; the engine
// interprets the empty result. A zero lower bound (`*0..N`) is likewise
// accepted for the same reason.
func NewEdgeHops(minHops, maxHops *int) (EdgeHops, error) {
	if minHops != nil && *minHops < 0 {
		return EdgeHops{}, errors.New("query: edge hop range requires a non-negative lower bound")
	}
	if maxHops != nil && *maxHops < 0 {
		return EdgeHops{}, errors.New("query: edge hop range requires a non-negative upper bound")
	}
	return EdgeHops{min: minHops, max: maxHops}, nil
}

// Min is the lower bound of the hop range; nil for unbounded.
func (h EdgeHops) Min() *int { return h.min }

// Max is the upper bound of the hop range; nil for unbounded.
func (h EdgeHops) Max() *int { return h.max }

// MarshalJSON renders an EdgeHops as an object with always-emitted min/max
// keys, both possibly null. The always-emit convention matches nullable /
// returnsAll / directed on the surrounding EdgeBinding.
func (h EdgeHops) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Min *int `json:"min"`
		Max *int `json:"max"`
	}{Min: h.min, Max: h.max})
}

// PathMember is one element of a named path's members list (Stage 8 spec
// §1.2, §3.2). It is a closed sum of NamedNodeMember, NamedEdgeMember,
// AnonEdgeMember, and AnonNodeMember — no other type can implement it — so
// a member is exactly one of four things: a named node, a named edge, an
// anonymous edge slot, or an anonymous node slot. The named variants
// reference a binding by variable; the anonymous variants carry no name,
// so an anonymous slot in a path never competes with a user-chosen
// variable in the part's byVar namespace (an earlier design used a
// synthetic-name string that collided with legal oC_SymbolicName inputs
// like `__anon_edge_0`; the tagged sum makes that unrepresentable).
type PathMember interface {
	// Kind reports whether the member is a node position or an edge
	// position (BindingNode / BindingEdge). A path member is never a path.
	Kind() BindingKind
	// Variable is the named binding this member references; empty for the
	// two anonymous variants.
	Variable() string
	// Anonymous reports whether this member carries no name (the two
	// AnonXMember variants).
	Anonymous() bool
	isPathMember()
}

// NamedNodeMember is a path member that references a named node binding by
// variable — the a and b in `MATCH p = (a)-[r]->(b)`. The variable is
// always non-empty (NewNamedNodeMember).
type NamedNodeMember struct {
	variable string
}

// NewNamedNodeMember builds a NamedNodeMember, rejecting the empty variable:
// a named member always names a binding, and the anonymous case is
// AnonNodeMember.
func NewNamedNodeMember(variable string) (NamedNodeMember, error) {
	if variable == "" {
		return NamedNodeMember{}, errors.New("query: named-node path member requires a non-empty variable")
	}
	return NamedNodeMember{variable: variable}, nil
}

// Variable is the named binding this member references; always non-empty.
func (m NamedNodeMember) Variable() string { return m.variable }

// Kind reports that a NamedNodeMember occupies a node position.
func (NamedNodeMember) Kind() BindingKind { return BindingNode }

// Anonymous reports false — this member names a binding.
func (NamedNodeMember) Anonymous() bool { return false }

func (NamedNodeMember) isPathMember() {}

// NamedEdgeMember is a path member that references a named edge binding by
// variable — the r in `MATCH p = (a)-[r]->(b)`. The variable is always
// non-empty (NewNamedEdgeMember).
type NamedEdgeMember struct {
	variable string
}

// NewNamedEdgeMember builds a NamedEdgeMember, rejecting the empty variable:
// an anonymous edge inside a named path is AnonEdgeMember, not a
// NamedEdgeMember with an empty variable.
func NewNamedEdgeMember(variable string) (NamedEdgeMember, error) {
	if variable == "" {
		return NamedEdgeMember{}, errors.New("query: named-edge path member requires a non-empty variable")
	}
	return NamedEdgeMember{variable: variable}, nil
}

// Variable is the named binding this member references; always non-empty.
func (m NamedEdgeMember) Variable() string { return m.variable }

// Kind reports that a NamedEdgeMember occupies an edge position.
func (NamedEdgeMember) Kind() BindingKind { return BindingEdge }

// Anonymous reports false — this member names a binding.
func (NamedEdgeMember) Anonymous() bool { return false }

func (NamedEdgeMember) isPathMember() {}

// AnonEdgeMember is a path member for an anonymous edge slot — the
// `-[]-` link inside `p = (a)-[]-(b)`. It carries no name (the anonymous
// edge is still its own binding in the part's Bindings slice, but the path
// member does not name it — a name would risk collision with a user
// variable in the byVar namespace). Empty struct: no state.
type AnonEdgeMember struct{}

// Variable is always empty for an AnonEdgeMember (the anonymous variant
// carries no name).
func (AnonEdgeMember) Variable() string { return "" }

// Kind reports that an AnonEdgeMember occupies an edge position.
func (AnonEdgeMember) Kind() BindingKind { return BindingEdge }

// Anonymous reports true — this member has no name.
func (AnonEdgeMember) Anonymous() bool { return true }

func (AnonEdgeMember) isPathMember() {}

// AnonNodeMember is a path member for an anonymous intermediate node — the
// `()` inside `p = (a)-[]-()-[]-(b)`. An anonymous node is not itself a
// binding (§C3, the node is a pure filter and does not appear in
// Part.Bindings), but the path's shape requires a placeholder at the node
// position so codegen can reconstruct the path shape from Members() alone.
// Empty struct: no state.
type AnonNodeMember struct{}

// Variable is always empty for an AnonNodeMember (the anonymous variant
// carries no name).
func (AnonNodeMember) Variable() string { return "" }

// Kind reports that an AnonNodeMember occupies a node position.
func (AnonNodeMember) Kind() BindingKind { return BindingNode }

// Anonymous reports true — this member has no name.
func (AnonNodeMember) Anonymous() bool { return true }

func (AnonNodeMember) isPathMember() {}

// PathBinding is a query variable bound to a named path (Stage 8 spec §1.2):
// the p in MATCH p = (a)-[r]->(b) RETURN p. It carries the path variable name
// and the shape-faithful ordered list of members the path composes, as a
// tagged sum (PathMember). Named members reference the part's own entity
// bindings by variable (the path binding does not co-own them); anonymous
// members are positional slots that carry no name, so an anonymous slot in
// a path never competes with a user-chosen variable in the byVar namespace.
// PathBinding never has a Nullable flag at Stage 8: the OPTIONAL-introduced
// case flows through the member bindings themselves.
type PathBinding struct {
	variable string       // the path variable name; always non-empty
	members  []PathMember // the members in shape-faithful textual order; always non-empty, no nil entries
}

// NewPathBinding builds a PathBinding. Rejects an empty variable (a path
// with no name is not a binding — the parser emits no PathBinding for an
// unnamed pattern), an empty members slice (a pattern element always has
// at least one node so a path always has at least one member), a nil
// member entry (every member is one of the four tagged-sum variants),
// and a kind-inconsistent repeat of a named member: openCypher lets the
// same variable appear multiple times in a pattern (`(n)-->(k)<--(n)`),
// so repeats of a *same-kind* named member are legal, but two named
// members that share a variable and disagree on Kind() would collide
// with the part's byVar (a kind conflict at the pattern level, which
// mergeBinding also catches). The anonymous variants may repeat freely.
func NewPathBinding(variable string, members []PathMember) (PathBinding, error) {
	if variable == "" {
		return PathBinding{}, errors.New("query: path binding requires a non-empty variable")
	}
	if len(members) == 0 {
		return PathBinding{}, errors.New("query: path binding requires at least one member")
	}
	kindByName := map[string]BindingKind{}
	for i, m := range members {
		if m == nil {
			return PathBinding{}, fmt.Errorf("query: path binding member %d is nil", i)
		}
		if m.Anonymous() {
			continue
		}
		v := m.Variable()
		if prior, ok := kindByName[v]; ok && prior != m.Kind() {
			return PathBinding{}, fmt.Errorf("query: path binding member %q appears with conflicting kinds (%s vs %s)", v, prior.String(), m.Kind().String())
		}
		kindByName[v] = m.Kind()
	}
	// Copy so the caller cannot mutate the binding's members after construction.
	membersCopy := make([]PathMember, len(members))
	copy(membersCopy, members)
	return PathBinding{variable: variable, members: membersCopy}, nil
}

// Variable is the path variable name; always non-empty.
func (b PathBinding) Variable() string { return b.variable }

// Members are the members in shape-faithful textual order; always non-empty,
// no nil entries. Codegen reads Members() to reconstruct the path's shape
// (node, edge, node, edge, …, node) and to identify the named members
// against the part's bindings.
func (b PathBinding) Members() []PathMember { return b.members }

// Kind reports that a PathBinding is a path.
func (PathBinding) Kind() BindingKind { return BindingPath }

// Nullable is always false at Stage 8: the OPTIONAL-introduced case flows
// through the member bindings themselves (Stage 8 spec §1.2).
func (PathBinding) Nullable() bool { return false }

func (PathBinding) isBinding() {}

// The PathMember discriminators name the wire tag for each variant. The
// named variants share their tag with graph.EntityKind ("node"/"edge") for
// wire continuity with NodeBinding / EdgeBinding; the anonymous variants
// use distinct tags so a consumer never confuses an anonymous slot with a
// named member of an empty variable.
const (
	pathMemberKindNamedNode = "node"
	pathMemberKindNamedEdge = "edge"
	pathMemberKindAnonEdge  = "anon-edge"
	pathMemberKindAnonNode  = "anon-node"
)

// MarshalJSON on the named variants emits `{"kind","variable"}`; the
// anonymous variants emit `{"kind"}` alone. Same one-level-deep posture as
// the other tagged unions in the model.
func (m NamedNodeMember) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Kind     string `json:"kind"`
		Variable string `json:"variable"`
	}{Kind: pathMemberKindNamedNode, Variable: m.variable})
}

// MarshalJSON on NamedEdgeMember mirrors NamedNodeMember's shape.
func (m NamedEdgeMember) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Kind     string `json:"kind"`
		Variable string `json:"variable"`
	}{Kind: pathMemberKindNamedEdge, Variable: m.variable})
}

// MarshalJSON on AnonEdgeMember emits only the "kind" discriminator.
func (AnonEdgeMember) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Kind string `json:"kind"`
	}{Kind: pathMemberKindAnonEdge})
}

// MarshalJSON on AnonNodeMember emits only the "kind" discriminator.
func (AnonNodeMember) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Kind string `json:"kind"`
	}{Kind: pathMemberKindAnonNode})
}

// MarshalJSON renders a PathBinding as a tagged union member discriminated by
// "kind" (derived from BindingKind, the single source), carrying its variable
// and members. Members serialise as an array of tagged-sum PathMember values
// (§3.2), one object per member. The always-emit nullable field (false, per
// Stage 8 spec §1.2) matches the entity bindings' shape.
func (b PathBinding) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Kind     string       `json:"kind"`
		Variable string       `json:"variable"`
		Members  []PathMember `json:"members"`
		Nullable bool         `json:"nullable"`
	}{Kind: b.Kind().String(), Variable: b.variable, Members: b.members, Nullable: b.Nullable()})
}

// UnwindBinding is a query variable bound to the current value drawn from an
// UNWIND clause's source list (Stage 9 spec §1.1): the x in
// `UNWIND [1,2,3] AS x`. It carries the AS variable name and the Stage-6
// element type of the source expression (TypeInt for `[1,2,3]`, TypeUnknown
// for `range(1,3)` or `null` or a bare `$param` — the parser records the
// honest "cannot tell" instead of guessing, and the resolver upgrades from
// the schema post-freeze). UNWIND is a reading clause distinct from MATCH,
// so an UnwindBinding is not a graph entity — it has no labels, no
// endpoints, no EntityKind(). Never nullable at Stage 9: an empty or null
// source list yields zero rows at runtime, a row-cardinality fact below
// the type-interface boundary (ADR 0005).
type UnwindBinding struct {
	variable string // the AS name; always non-empty
	elemType Type   // the source list's Stage-6 element type; TypeUnknown when the parser cannot commit
}

// NewUnwindBinding builds an UnwindBinding, rejecting the empty variable
// (an UNWIND without `AS name` is a grammatical error, so the parser
// never emits an anonymous UnwindBinding). A nil elemType is normalised
// to TypeUnknown — the "cannot tell" case is never a nil Type on the
// wire, mirroring NewTypeList's convention.
func NewUnwindBinding(variable string, elemType Type) (UnwindBinding, error) {
	if variable == "" {
		return UnwindBinding{}, errors.New("query: unwind binding requires a non-empty variable")
	}
	if elemType == nil {
		elemType = TypeUnknown{}
	}
	return UnwindBinding{variable: variable, elemType: elemType}, nil
}

// Variable is the AS name; always non-empty.
func (b UnwindBinding) Variable() string { return b.variable }

// ElementType is the source list's Stage-6 element type; a bare-ref
// projection on the binding types as this value.
func (b UnwindBinding) ElementType() Type { return b.elemType }

// Kind reports that an UnwindBinding is an unwind binding.
func (UnwindBinding) Kind() BindingKind { return BindingUnwind }

// Nullable is always false at Stage 9: an empty or null source list is a
// row-cardinality fact below the type-interface boundary (ADR 0005),
// not a per-binding static nullability.
func (UnwindBinding) Nullable() bool { return false }

func (UnwindBinding) isBinding() {}

// MarshalJSON renders an UnwindBinding as a tagged union member
// discriminated by "kind" (derived from BindingKind, the single source),
// carrying its variable and its always-emitted element type. The always-
// emit nullable field (false, per Stage 9 spec §1.4) matches the entity
// and path bindings' shape.
func (b UnwindBinding) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Kind     string `json:"kind"`
		Variable string `json:"variable"`
		ElemType Type   `json:"elemType"`
		Nullable bool   `json:"nullable"`
	}{Kind: b.Kind().String(), Variable: b.variable, ElemType: b.elemType, Nullable: b.Nullable()})
}

// CallBinding is a query variable bound to one YIELD result column of a
// CALL clause (Stage 14 spec §1.2 / §3.2). Each YIELD item mints one
// CallBinding, mirroring UnwindBinding's "one variable per binding"
// shape: the variable is the AS-alias (or the bare source-field name
// when no AS is present); the procedure is the fully-qualified name
// looked up in the procedure registry (procsig.Registry); sourceField
// is the signature-declared result column this binding draws from
// (kept even when it equals the variable, so a YIELD out AS x is
// unambiguous at codegen). The resultType is the bridged Stage-6
// type (TypeInt / TypeFloat / TypeString / TypeUnknown — the last for
// a NUMBER signature token, whose runtime column type post-freeze
// codegen reads from the registry directly). Nullable mirrors the
// signature's trailing `?` verbatim.
//
// CALL is a Read clause: a CallBinding does not flip
// Query.StatementKind. It participates in the Stage-6 refType
// classifier the same way UnwindBinding does — a bare RETURN on a
// call-yielded variable types as ResultType() (see cypher's
// internal refType in typing.go).
type CallBinding struct {
	variable    string // always non-empty
	procedure   string // fully-qualified name; always non-empty
	sourceField string // signature-declared result column; always non-empty
	resultType  Type   // bridged Stage-6 result type; TypeUnknown for NUMBER
	nullable    bool   // signature's trailing '?'
}

// NewCallBinding builds a CallBinding, rejecting the empty variable /
// procedure / sourceField (the CALL grammar rules out each empty case,
// so the parser never reaches these — the constructor is the model-
// invariant guard). A nil resultType is normalised to TypeUnknown, the
// same "cannot tell" fallback NewUnwindBinding and NewRefProjection
// use.
func NewCallBinding(variable, procedure, sourceField string, resultType Type, nullable bool) (CallBinding, error) {
	if variable == "" {
		return CallBinding{}, errors.New("query: call binding requires a non-empty variable")
	}
	if procedure == "" {
		return CallBinding{}, errors.New("query: call binding requires a non-empty procedure name")
	}
	if sourceField == "" {
		return CallBinding{}, errors.New("query: call binding requires a non-empty source field")
	}
	if resultType == nil {
		resultType = TypeUnknown{}
	}
	return CallBinding{
		variable:    variable,
		procedure:   procedure,
		sourceField: sourceField,
		resultType:  resultType,
		nullable:    nullable,
	}, nil
}

// Variable is the YIELD-item variable this column exposes into scope —
// the AS-alias for `YIELD out AS x`, or the bare source-field name for
// `YIELD out`. Always non-empty.
func (b CallBinding) Variable() string { return b.variable }

// Procedure is the fully-qualified procedure name this CALL invoked
// (e.g. "test.my.proc"). Always non-empty.
func (b CallBinding) Procedure() string { return b.procedure }

// SourceField is the signature-declared result column this binding
// draws from. It equals Variable for a bare `YIELD out`; it differs
// for `YIELD out AS x`. Always non-empty. Codegen post-freeze reads
// SourceField to name the driver-visible column and Variable to name
// the caller-visible one.
func (b CallBinding) SourceField() string { return b.sourceField }

// ResultType is the CallBinding's Stage-6 result type: the bridged
// query.Type for the signature's declared token (INTEGER → TypeInt,
// FLOAT → TypeFloat, STRING → TypeString, NUMBER → TypeUnknown — the
// wire type; the registry stays the source of truth for NUMBER's
// assignable-from semantics).
func (b CallBinding) ResultType() Type { return b.resultType }

// Kind reports that a CallBinding is a call binding.
func (CallBinding) Kind() BindingKind { return BindingCall }

// Nullable mirrors the signature's trailing `?` on the source result
// field verbatim — a static parser-time fact set at construction.
func (b CallBinding) Nullable() bool { return b.nullable }

func (CallBinding) isBinding() {}

// MarshalJSON renders a CallBinding as a tagged union member
// discriminated by "kind" (derived from BindingKind, the single
// source), carrying every field always-emitted. sourceField is
// always emitted (even when equal to variable) so a rename
// (`YIELD out AS x`) is unambiguous at the wire.
func (b CallBinding) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Kind        string `json:"kind"`
		Variable    string `json:"variable"`
		Procedure   string `json:"procedure"`
		SourceField string `json:"sourceField"`
		ResultType  Type   `json:"resultType"`
		Nullable    bool   `json:"nullable"`
	}{
		Kind:        b.Kind().String(),
		Variable:    b.variable,
		Procedure:   b.procedure,
		SourceField: b.sourceField,
		ResultType:  b.resultType,
		Nullable:    b.nullable,
	})
}

// Endpoint is one end of an edge. It is a closed sum of VarEndpoint and
// InlineEndpoint — no other type can implement it — so an endpoint either names a
// binding or carries inline labels, never both and never neither. Both variants
// hold their data in unexported fields, so NewVarEndpoint / NewInlineEndpoint are
// the only way to construct a non-zero value.
type Endpoint interface {
	isEndpoint()
}

// VarEndpoint is an endpoint that references a named binding. Its labels live on
// that binding, not here, so they are never duplicated (Stage-0 spec, C4). Its
// variable is always non-empty (NewVarEndpoint); the empty case is InlineEndpoint.
type VarEndpoint struct {
	variable string // the binding referred to
}

// NewVarEndpoint builds a VarEndpoint, rejecting the empty variable: an endpoint
// that names no binding is the inline case, expressed with NewInlineEndpoint.
func NewVarEndpoint(variable string) (VarEndpoint, error) {
	if variable == "" {
		return VarEndpoint{}, errors.New("query: variable endpoint requires a non-empty variable")
	}
	return VarEndpoint{variable: variable}, nil
}

// Variable is the binding referred to. Always non-empty.
func (e VarEndpoint) Variable() string { return e.variable }

func (VarEndpoint) isEndpoint() {}

// InlineEndpoint is an anonymous endpoint node carrying inline labels, which may
// be empty — the fully anonymous () endpoint (Stage-0 spec, C4).
type InlineEndpoint struct {
	labels graph.LabelSet // labels as written; may be empty
}

// NewInlineEndpoint builds an InlineEndpoint. Labels may be empty (the ()
// endpoint), so construction cannot fail.
func NewInlineEndpoint(labels graph.LabelSet) InlineEndpoint {
	return InlineEndpoint{labels: labels}
}

// Labels are the labels as written; may be empty (the () endpoint).
func (e InlineEndpoint) Labels() graph.LabelSet { return e.labels }

func (InlineEndpoint) isEndpoint() {}

// Ref points from a ReturnItem or Parameter into the query's bindings: a whole
// entity (Property empty) or one of its properties. Property is a single name, so
// multi-level access (a.b.c) is unrepresentable. For example, the return items in
// RETURN p, p.name:
//
//	p       →  Ref{Variable: "p"}                   // the whole binding
//	p.name  →  Ref{Variable: "p", Property: "name"} // one of its properties
type Ref struct {
	Variable string // the binding referred to
	Property string // a property of that binding; empty means the whole entity
}

// ReturnItem is one result column: its name (an explicit alias, or derived from
// the source) and the Value describing what it projects — a Projection sum.
type ReturnItem struct {
	Name  string
	Value Projection
}

// MarshalJSON renders a ReturnItem with its Value as a tagged-union member one
// level deep, matching the Binding/Use convention: lowercase "name" and "value"
// keys, the projection carrying its own "kind" discriminator. (The pre-Stage-3
// shape used PascalCase "Name"/"Ref"; the move to a sum makes the value a
// tagged union, so the item joins the sum-marshalling convention.)
func (i ReturnItem) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Name  string     `json:"name"`
		Value Projection `json:"value"`
	}{Name: i.Name, Value: i.Value})
}

// Projection is what one ReturnItem projects. It is a closed sum of
// RefProjection, LiteralProjection, FuncProjection, AggregateProjection, and
// (Stage 6) ExprProjection — no other type can implement it — so a projection
// is exactly one of the five. Each variant holds its data in unexported
// fields, so the smart constructors (NewRefProjection / NewLiteralProjection /
// NewFuncProjection / NewAggregateProjection / NewExprProjection) are the only
// way to construct a non-zero value, mirroring the Use sum
// (internal/query/query.go).
//
// A projection carries exactly what the resolver needs to reach a schema type
// (the referenced bindings as Refs), its Stage-6 result type, and the one
// cardinality-bearing distinction (aggregate vs not); nothing of the
// expression tree (ADR 0003).
//
// Every variant carries Type() Type — the whole point of Stage 6 is that every
// projected column carries a result type. Promoting the accessor onto the
// interface removes the structural-typing shim listeners once needed to read
// it and keeps the sum's exhaustiveness honest.
type Projection interface {
	// Type is the projection's Stage-6 result type; TypeUnknown when the parser
	// cannot commit schema-free (property lookups, function results, aggregate
	// results, NULL propagation).
	Type() Type
	isProjection()
}

// RefProjection wraps a Ref — the Stage-0/1/2 var / var.prop case verbatim, now
// carrying its result type (Stage 6). The listener only builds it after the
// shape gates accept a bare variable or a single-level property lookup; the
// type it passes is TypeNode / TypeEdge for a whole-entity ref and TypeUnknown
// for a property lookup (the schema owns property typing per ADR 0003).
type RefProjection struct {
	ref        Ref  // the binding or binding property this column projects
	resultType Type // the projection's result type; TypeUnknown for a property lookup
}

// NewRefProjection builds a RefProjection carrying its result type. Total: the
// listener supplies a Ref it has already validated against the projection shape
// gates and a Type it computed from the referenced binding's kind (Stage 6 §1);
// no constructor error is possible.
func NewRefProjection(r Ref, t Type) RefProjection {
	return RefProjection{ref: r, resultType: t}
}

// Ref is the binding or binding property this column projects.
func (p RefProjection) Ref() Ref { return p.ref }

// Type is the projection's result type (Stage 6): TypeNode / TypeEdge for a
// whole-entity ref and TypeUnknown for a property lookup — the schema owns
// property typing.
func (p RefProjection) Type() Type { return p.resultType }

func (RefProjection) isProjection() {}

// LiteralProjection carries the scalar literal's kind as its only exported
// datum: a boolean literal is TypeBool, an integer literal is TypeInt, a list
// literal is TypeList (parameterised by its element type), and so on. The
// literal's *value* still lives below the type-interface boundary (ADR 0005,
// B1) — re-executed from the original text, never reconstructed — but the type
// enters the model because it becomes the projected column's typed result
// (Stage 6). It carries no Ref because a literal traces back to no binding.
type LiteralProjection struct {
	resultType Type // the literal's scalar / list / map kind (Stage 6)
}

// NewLiteralProjection builds a LiteralProjection carrying its scalar-literal
// kind. Total: the listener computes the type at classification time from the
// grammar node; the constructor is the sole writer.
func NewLiteralProjection(t Type) LiteralProjection {
	return LiteralProjection{resultType: t}
}

// Type is the literal's result type (Stage 6).
func (p LiteralProjection) Type() Type { return p.resultType }

func (LiteralProjection) isProjection() {}

// FuncProjection is a non-aggregate function call. It carries the function's
// referenced bindings as []Ref (the var/var.prop arguments the resolver must
// trace) and its Stage-6 result type; nothing about the function itself — not
// its name, arity, or signature. The function's identity is a resolver/engine
// concern below the type-interface boundary (ADR 0005), so the parser records
// TypeUnknown for every function call. The model carries "this column depends
// on these bindings" so referential integrity holds, plus the honest "cannot
// tell" for its result type.
type FuncProjection struct {
	refs       []Ref // the var/var.prop arguments the function touches
	resultType Type  // Stage 6: TypeUnknown — function identity is below the boundary
}

// NewFuncProjection builds a FuncProjection over the bindings the call
// references and the result type the listener computes (TypeUnknown today).
// Total: the listener supplies Refs it has already mined and a Type value.
func NewFuncProjection(refs []Ref, t Type) FuncProjection {
	return FuncProjection{refs: refs, resultType: t}
}

// Refs are the var/var.prop arguments the function touches.
func (p FuncProjection) Refs() []Ref { return p.refs }

// Type is the function's result type (Stage 6): TypeUnknown, because a
// non-aggregate function's identity lives below the type-interface boundary.
func (p FuncProjection) Type() Type { return p.resultType }

func (FuncProjection) isProjection() {}

// AggregateProjection is an aggregate function call. It carries an AggregateFunc
// (the cardinality-bearing distinction §4: an aggregate collapses rows, a fact
// the generated code models differently), the referenced bindings as []Ref, a
// Stage-10 DISTINCT axis (single-bit annotation — `count(DISTINCT x)` and
// `count(x)` are observably-different queries, so the model preserves the axis),
// and its Stage-10 result type (per-aggregate table against the operand's
// Stage-6 type, spec §1.2). count(*) is the degenerate case — AggCount with an
// empty []Ref and a TypeInt result.
type AggregateProjection struct {
	fn         AggregateFunc // which aggregate this is (the cardinality signal)
	refs       []Ref         // the var/var.prop arguments the aggregate touches
	distinct   bool          // Stage 10: DISTINCT dedup axis; changes result semantics
	resultType Type          // Stage 10: per-aggregate result type; TypeUnknown when the parser cannot commit
}

// NewAggregateProjection builds an AggregateProjection. Total: the listener
// supplies an AggregateFunc from the closed enum, Refs it has already mined,
// the DISTINCT flag (read from the OC_FunctionInvocation grammar node), and
// the Stage-10 result type it computed via aggregateResultType.
func NewAggregateProjection(fn AggregateFunc, refs []Ref, distinct bool, t Type) AggregateProjection {
	return AggregateProjection{fn: fn, refs: refs, distinct: distinct, resultType: t}
}

// Func is which aggregate this is — the cardinality-bearing distinction (§4).
func (p AggregateProjection) Func() AggregateFunc { return p.fn }

// Refs are the var/var.prop arguments the aggregate touches.
func (p AggregateProjection) Refs() []Ref { return p.refs }

// Distinct reports whether the aggregate was written with a DISTINCT
// deduplication prefix (Stage 10). `count(DISTINCT x)` returns true;
// `count(x)`, `count(*)`, and every aggregate without the keyword return
// false. The axis changes result semantics, so the model carries it.
func (p AggregateProjection) Distinct() bool { return p.distinct }

// Type is the aggregate's result type (Stage 10, spec §1.2): TypeInt for
// count; list<T> for collect; sum/min/max commit to a concrete type when the
// operand's type commits, else TypeUnknown; avg / stDev / percentile* stay
// TypeUnknown (engine-dependent). A wrong concrete type would be strictly
// worse than an honest TypeUnknown the resolver can upgrade from the schema.
func (p AggregateProjection) Type() Type { return p.resultType }

func (AggregateProjection) isProjection() {}

// ExprProjection is a rich scalar expression at a RETURN or WITH position
// (Stage 6): arithmetic, string/list/null predicates, list or map literals,
// list indexing/slicing/concatenation, CASE, chained comparisons, and
// parenthesised composites. It carries only the result type the parser
// computed and the []Ref every binding the sub-expression touched — no
// expression tree, per ADR 0003 (the tree is re-executed from the original
// text, ADR 0005). A rich expression whose type the parser cannot compute
// (property-participating arithmetic, NULL propagation, unknown function
// return types) types as TypeUnknown; the resolver upgrades from the schema.
type ExprProjection struct {
	refs              []Ref // the var/var.prop bindings the expression touches
	resultType        Type  // the parser-computed result type; TypeUnknown when it cannot commit
	containsAggregate bool  // true iff the expression subtree contains at least one aggregate call (Shape B, ADR 0008 amendment 2026-07-06)
}

// NewExprProjection builds an ExprProjection carrying its result type and
// touched refs. Total: the listener supplies Refs it has already mined from
// the sub-expression and a Type value. Forwards containsAggregate=false to
// NewExprProjectionWithAggregate — the zero-value-safe shorthand for callers
// that predate the ADR 0008 2026-07-06 amendment.
func NewExprProjection(refs []Ref, t Type) ExprProjection {
	return NewExprProjectionWithAggregate(refs, t, false)
}

// NewExprProjectionWithAggregate builds an ExprProjection carrying its
// result type, touched refs, and the ContainsAggregate bit — true iff the
// expression subtree contains at least one aggregate function call (Shape B
// per ADR 0008 amendment 2026-07-06). Callers that do not need the bit use
// NewExprProjection, which forwards containsAggregate=false.
func NewExprProjectionWithAggregate(refs []Ref, t Type, containsAggregate bool) ExprProjection {
	return ExprProjection{refs: refs, resultType: t, containsAggregate: containsAggregate}
}

// Refs are the var/var.prop bindings the expression touches, so the
// referential-integrity sweep covers every ref inside a rich projection.
func (p ExprProjection) Refs() []Ref { return p.refs }

// Type is the projection's Stage-6 result type — the whole point of the
// variant. TypeUnknown when the parser cannot commit (property-participating
// arithmetic, NULL propagation, unknown function return types).
func (p ExprProjection) Type() Type { return p.resultType }

// ContainsAggregate reports whether the expression subtree contains at least
// one aggregate function call. Populated parser-side during
// classifyRichExpression's walk (Shape B, ADR 0008 amendment 2026-07-06).
// The resolver's grouping-key discriminator reads this bit
// (docs/specs/resolver-stage-r5.md §4.5.3 close-out).
func (p ExprProjection) ContainsAggregate() bool { return p.containsAggregate }

func (ExprProjection) isProjection() {}

// MarshalJSON renders an ExprProjection as a tagged union member discriminated
// by "kind", carrying its refs, always-emitted result type, and the
// ContainsAggregate axis (omit-when-false — the campaign convention recorded
// in the ADR 0008 2026-07-06 amendment note, so pre-freeze goldens whose
// subtree carries no aggregate stay byte-identical).
func (p ExprProjection) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Kind              string    `json:"kind"`
		Refs              []flatRef `json:"refs"`
		Type              Type      `json:"type"`
		ContainsAggregate bool      `json:"containsAggregate,omitempty"`
	}{
		Kind:              projectionKindExpr,
		Refs:              flattenRefs(p.refs),
		Type:              projectionType(p.resultType),
		ContainsAggregate: p.containsAggregate,
	})
}

// AggregateFunc identifies one of the openCypher aggregating functions. The set
// is closed and known (§4), so it is an int-backed enum with a stringer —
// mirroring graph.EntityKind / ClauseSlot — and the JSON "func" tag derives from
// the one source (String), so it cannot drift.
type AggregateFunc int

const (
	// AggCount is the count(...) aggregate (count(*) is its degenerate case).
	AggCount AggregateFunc = iota
	// AggSum is the sum(...) aggregate.
	AggSum
	// AggCollect is the collect(...) aggregate.
	AggCollect
	// AggMin is the min(...) aggregate.
	AggMin
	// AggMax is the max(...) aggregate.
	AggMax
	// AggAvg is the avg(...) aggregate.
	AggAvg
	// AggStdev is the stDev/stDevP aggregate.
	AggStdev
	// AggPercentile is the percentileCont/percentileDisc aggregate.
	AggPercentile
)

// String is the canonical lowercase name of the aggregate. It is the single
// source the JSON "func" discriminator derives from, so the serialised name can
// never drift from the enum. The default arm is AggCount, the degenerate
// count(*) case.
func (f AggregateFunc) String() string {
	switch f {
	case AggSum:
		return "sum"
	case AggCollect:
		return "collect"
	case AggMin:
		return "min"
	case AggMax:
		return "max"
	case AggAvg:
		return "avg"
	case AggStdev:
		return "stdev"
	case AggPercentile:
		return "percentile"
	default:
		return "count"
	}
}

// Parameter is a query input. Uses are the value-positions where the parameter
// appears — each a Use describing the slot it sits in — so a parameter written
// in N places collapses to one Parameter with N uses. A Use is exactly one of
// PropertyUse (the parameter is bound to a binding property, e.g. the $threshold
// in WHERE a.age > $threshold) or ClauseSlotUse (the parameter occupies a
// SKIP/LIMIT clause slot whose type comes from the slot, not from a binding).
// For example, in
//
//	WHERE a.age > $threshold AND b.age > $threshold
//
// $threshold has two PropertyUses: Ref{Variable: "a", Property: "age"} and
// Ref{Variable: "b", Property: "age"}. In SKIP $page, $page has one
// ClauseSlotUse{ClauseSlotSkip}. The resolver judges type unification across
// uses post-freeze (the parser stays schema-agnostic per ADR 0003); mixed-kind
// uses on one Parameter are not a parser-level conflict.
type Parameter struct {
	Name string `json:"name"`
	Uses []Use  `json:"uses"`
}

// Use is one position where a parameter appears. It is a closed sum of
// PropertyUse, ClauseSlotUse, and (Stage 6) ExprUse — no other type can
// implement it — so a use is exactly one of the three: bound to a binding
// property, sat in a clause slot, or embedded inside a rich scalar expression
// whose result type is what the model records. Every variant holds its data
// in unexported fields, so NewPropertyUse / NewClauseSlotUse / NewExprUse are
// the only ways to construct a non-zero value: the invariants the types alone
// cannot express hold for every value that exists.
type Use interface {
	isUse()
}

// PropertyUse is a parameter use bound to a binding property: the $threshold in
// WHERE a.age > $threshold sits against Ref{Variable: "a", Property: "age"}.
// The Ref is always a single-level property reference (parser invariant D1);
// multi-level access (a.b.c) is unrepresentable, because Ref itself only carries
// one Property name.
type PropertyUse struct {
	ref  Ref // the binding property the parameter sits against
	part int // the branch-relative index of the enclosing Part (fvo, ADR 0008 amendment 2026-07-06)
}

// NewPropertyUse builds a PropertyUse at Part 0. Total: a parameter use carries
// a Ref the listener has already validated (parameter mining only fires after
// the expression shape gates accept a bound variable + property), so no
// constructor error is possible at the call site. Mirrors NewInlineEndpoint's
// total posture. Delegates to NewPropertyUseAt with part=0 so single-Part
// callers stay verbatim.
func NewPropertyUse(r Ref) PropertyUse {
	return NewPropertyUseAt(r, 0)
}

// NewPropertyUseAt builds a PropertyUse carrying its Ref and the branch-relative
// index of the enclosing Part (Part 0 for a single-Part branch, or the position
// under EnterOC_With's Part swap; fvo per ADR 0008 amendment 2026-07-06).
func NewPropertyUseAt(r Ref, part int) PropertyUse {
	return PropertyUse{ref: r, part: part}
}

// Ref is the binding property the parameter sits against.
func (u PropertyUse) Ref() Ref { return u.ref }

// Part reports the branch-relative index of the Part the parameter Use
// lexically occurs in (fvo per ADR 0008 amendment 2026-07-06). Populated
// parser-side by addParameterUse's currentPartIndex call. The resolver's
// witnessAcrossScopes reads this to select the exact scope to witness
// against (docs/specs/resolver-stage-r5.md §4.2.4 close-out).
func (u PropertyUse) Part() int { return u.part }

func (PropertyUse) isUse() {}

// ClauseSlot identifies a clause whose value slot can hold a parameter:
// currently SKIP or LIMIT. Int-backed with a stringer — mirrors
// graph.EntityKind's discipline — so the JSON discriminator derives from one
// source and cannot drift.
type ClauseSlot int

const (
	// ClauseSlotSkip is the SKIP clause's integer slot.
	ClauseSlotSkip ClauseSlot = iota
	// ClauseSlotLimit is the LIMIT clause's integer slot.
	ClauseSlotLimit
)

// String is the lowercase name of the slot ("skip" / "limit"). It is the
// single source the JSON discriminator's "slot" field derives from.
func (s ClauseSlot) String() string {
	switch s {
	case ClauseSlotLimit:
		return "limit"
	default:
		return "skip"
	}
}

// ClauseName is the uppercase clause name for use in an error message
// ("SKIP" / "LIMIT"). Derived from String so the two names share one source.
func (s ClauseSlot) ClauseName() string {
	switch s {
	case ClauseSlotLimit:
		return "LIMIT"
	default:
		return "SKIP"
	}
}

// ExprPosition names where a rich-expression parameter use appears (Stage 6,
// widened in Stage 12): inside a projection column, a predicate, a SET value,
// or a DELETE target. It is int-backed with a stringer — mirrors
// AggregateFunc / ClauseSlot — so the JSON discriminator derives from one
// source and cannot drift.
type ExprPosition int

const (
	// ExprInProjection is a rich-expression parameter use at a RETURN or WITH
	// projection column.
	ExprInProjection ExprPosition = iota
	// ExprInPredicate is a rich-expression parameter use inside a WHERE
	// predicate or a comparable predicate position.
	ExprInPredicate
	// ExprInSetValue is a rich-expression parameter use inside a SET value
	// expression — the RHS of SET n.prop = <expr> or SET n = <expr> /
	// SET n += <expr>. Distinct from Projection (Stage 12): a SET value is a
	// producer of a value written to the graph, semantically opposite to a
	// RETURN column's consumer role. The write-side distinction lets the
	// resolver key on the target property's type for a schema-cross-check
	// that a projection use would not carry.
	ExprInSetValue
	// ExprInDeleteTarget is a rich-expression parameter use inside a DELETE
	// target expression — anything that isn't a bare var or var.prop
	// (DELETE friends[$idx], DELETE nodes(p)[0]). Distinct from Projection
	// (Stage 12): a DELETE target is a resolver-side lookup whose runtime
	// entity kind determines whether the delete is legal (only node/edge
	// values can be deleted). The position keeps the axis honest across the
	// write set.
	ExprInDeleteTarget
)

// String is the lowercase wire tag ("projection" / "predicate" / "setValue" /
// "deleteTarget"). The single source the JSON discriminator's "position" field
// derives from.
func (p ExprPosition) String() string {
	switch p {
	case ExprInPredicate:
		return "predicate"
	case ExprInSetValue:
		return "setValue"
	case ExprInDeleteTarget:
		return "deleteTarget"
	default:
		return "projection"
	}
}

// ExprUse is a parameter use that appears inside a rich scalar expression
// (Stage 6). Its own type is not directly bindable to a single property or a
// clause slot — the expression's result type is what the model carries, and
// the resolver unifies the parameter's type from the enclosing expression
// post-freeze. The variant carries the enclosing expression's Stage-6 result
// type and a position discriminator (a projection column vs a predicate) so
// the resolver can distinguish uses that participate in aggregation grouping
// from uses that participate in filtering.
type ExprUse struct {
	enclosingType Type         // the result type of the enclosing rich expression
	position      ExprPosition // where the enclosing expression sits (projection / predicate)
	part          int          // the branch-relative index of the enclosing Part (fvo)
}

// NewExprUse builds an ExprUse at Part 0 carrying the enclosing rich
// expression's result type and position. Delegates to NewExprUseAt with
// part=0 so single-Part callers stay verbatim.
func NewExprUse(enclosing Type, position ExprPosition) ExprUse {
	return NewExprUseAt(enclosing, position, 0)
}

// NewExprUseAt builds an ExprUse carrying the enclosing rich expression's
// result type, position discriminator, and the branch-relative index of the
// enclosing Part (fvo per ADR 0008 amendment 2026-07-06).
func NewExprUseAt(enclosing Type, position ExprPosition, part int) ExprUse {
	return ExprUse{enclosingType: enclosing, position: position, part: part}
}

// EnclosingType is the result type of the enclosing rich expression at the
// parameter's position. The resolver reads it to infer the parameter's type.
func (u ExprUse) EnclosingType() Type { return u.enclosingType }

// Position is where the enclosing expression sits (projection column / predicate).
func (u ExprUse) Position() ExprPosition { return u.position }

// Part reports the branch-relative index of the Part the parameter Use
// lexically occurs in (fvo per ADR 0008 amendment 2026-07-06).
func (u ExprUse) Part() int { return u.part }

func (ExprUse) isUse() {}

// MarshalJSON renders an ExprUse as a tagged union member discriminated by
// "kind", carrying the enclosing type, position, and (fvo per ADR 0008
// amendment 2026-07-06) the branch-relative Part index. "part" is
// omit-when-zero-value per the post-freeze wire convention.
func (u ExprUse) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Kind          string `json:"kind"`
		EnclosingType Type   `json:"enclosingType"`
		Position      string `json:"position"`
		Part          int    `json:"part,omitempty"`
	}{
		Kind:          useKindExpr,
		EnclosingType: projectionType(u.enclosingType),
		Position:      u.position.String(),
		Part:          u.part,
	})
}

// ClauseSlotUse is a parameter use that occupies a SKIP/LIMIT clause slot. The
// parameter's type comes from the slot (an integer) rather than from a binding
// property, so this variant carries no Ref.
type ClauseSlotUse struct {
	slot ClauseSlot
	part int // the branch-relative index of the enclosing Part (fvo)
}

// NewClauseSlotUse builds a ClauseSlotUse at Part 0. Delegates to
// NewClauseSlotUseAt with part=0 so single-Part callers stay verbatim.
func NewClauseSlotUse(s ClauseSlot) ClauseSlotUse {
	return NewClauseSlotUseAt(s, 0)
}

// NewClauseSlotUseAt builds a ClauseSlotUse carrying the clause slot and the
// branch-relative index of the enclosing Part (fvo per ADR 0008 amendment
// 2026-07-06).
func NewClauseSlotUseAt(s ClauseSlot, part int) ClauseSlotUse {
	return ClauseSlotUse{slot: s, part: part}
}

// Slot is the clause whose slot the parameter occupies.
func (u ClauseSlotUse) Slot() ClauseSlot { return u.slot }

// Part reports the branch-relative index of the Part the parameter Use
// lexically occurs in (fvo per ADR 0008 amendment 2026-07-06).
func (u ClauseSlotUse) Part() int { return u.part }

func (ClauseSlotUse) isUse() {}

// The Use discriminators have no graph-vocabulary counterpart (the distinction
// is query-side only), so they are named here, the one place they are emitted.
const (
	useKindProperty   = "property"
	useKindClauseSlot = "clause-slot"
	useKindExpr       = "expr"
)

// The "var" and "inline" endpoint discriminators have no graph-vocabulary
// counterpart (the distinction is query-side only), so they are named here, the
// one place they are emitted.
const (
	endpointKindVar    = "var"
	endpointKindInline = "inline"
)

// The Projection discriminators have no graph-vocabulary counterpart (the
// distinction is query-side only), so they are named here, the one place they
// are emitted. They sit next to the other kind constants per the Stage-3 spec §5.
const (
	projectionKindRef       = "ref"
	projectionKindLiteral   = "literal"
	projectionKindFunc      = "func"
	projectionKindAggregate = "aggregate"
	projectionKindExpr      = "expr"
)

// MarshalJSON renders a NodeBinding as a tagged union member discriminated by
// "kind", so the Binding sum marshals to a stable, self-describing shape across
// both variants. The tag derives from graph.EntityKind, so it cannot drift from
// Kind(). Mirrors schema.Schema's determinism discipline: the encoding is fixed
// and independent of any map iteration order.
func (b NodeBinding) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Kind     string         `json:"kind"`
		Variable string         `json:"variable"`
		Labels   graph.LabelSet `json:"labels"`
		Nullable bool           `json:"nullable"`
	}{Kind: b.Kind().String(), Variable: b.variable, Labels: b.labels, Nullable: b.nullable})
}

// MarshalJSON renders an EdgeBinding as a tagged union member discriminated by
// "kind" (derived from BindingKind). Source and Target are themselves
// tagged-union endpoints. Stage 8: hops is always emitted — null for a
// single-hop edge (Stages 0..7), a {"min", "max"} object for a variable-length
// edge — matching the always-emit convention nullable / directed / returnsAll follow.
func (b EdgeBinding) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Kind     string         `json:"kind"`
		Variable string         `json:"variable"`
		Labels   graph.LabelSet `json:"labels"`
		Source   Endpoint       `json:"source"`
		Target   Endpoint       `json:"target"`
		Nullable bool           `json:"nullable"`
		Directed bool           `json:"directed"`
		Hops     *EdgeHops      `json:"hops"`
	}{Kind: b.Kind().String(), Variable: b.variable, Labels: b.labels, Source: b.source, Target: b.target, Nullable: b.nullable, Directed: b.directed, Hops: b.hops})
}

// MarshalJSON renders a VarEndpoint as a tagged union member discriminated by
// "kind", matching the Binding sum's convention.
func (e VarEndpoint) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Kind     string `json:"kind"`
		Variable string `json:"variable"`
	}{Kind: endpointKindVar, Variable: e.variable})
}

// MarshalJSON renders an InlineEndpoint as a tagged union member discriminated by
// "kind", matching the Binding sum's convention.
func (e InlineEndpoint) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Kind   string         `json:"kind"`
		Labels graph.LabelSet `json:"labels"`
	}{Kind: endpointKindInline, Labels: e.labels})
}

// MarshalJSON renders a PropertyUse as a tagged union member discriminated by
// "kind", flattening its Ref into sibling "variable" and "property" fields so
// the use's shape stays one level deep — same posture as the Binding sum.
// "part" is omit-when-zero-value per the post-freeze wire convention (fvo per
// ADR 0008 amendment 2026-07-06).
func (u PropertyUse) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Kind     string `json:"kind"`
		Variable string `json:"variable"`
		Property string `json:"property"`
		Part     int    `json:"part,omitempty"`
	}{
		Kind:     useKindProperty,
		Variable: u.ref.Variable,
		Property: u.ref.Property,
		Part:     u.part,
	})
}

// MarshalJSON renders a ClauseSlotUse as a tagged union member discriminated by
// "kind". The "slot" tag derives from ClauseSlot.String, so the serialised slot
// can never drift from Slot(). "part" is omit-when-zero-value per the
// post-freeze wire convention (fvo per ADR 0008 amendment 2026-07-06).
func (u ClauseSlotUse) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Kind string `json:"kind"`
		Slot string `json:"slot"`
		Part int    `json:"part,omitempty"`
	}{
		Kind: useKindClauseSlot,
		Slot: u.slot.String(),
		Part: u.part,
	})
}

// flatRef is the one-level-deep shape a Ref takes inside a projection's "refs"
// array: sibling lowercase "variable"/"property" fields, matching the
// PropertyUse convention (Ref has no json tags of its own, so flattening here
// keeps the wire shape lowercase and stable).
type flatRef struct {
	Variable string `json:"variable"`
	Property string `json:"property"`
}

// flattenRefs maps Refs onto their wire shape, preserving order. A nil input
// marshals as a JSON null, matching the always-emit posture of the other sums.
func flattenRefs(refs []Ref) []flatRef {
	if refs == nil {
		return nil
	}
	out := make([]flatRef, len(refs))
	for i, r := range refs {
		out[i] = flatRef(r)
	}
	return out
}

// projectionType returns the projection's result type or, when it is nil (the
// zero-value case), TypeUnknown — so every projection marshals a concrete type
// tag even when constructed via a struct literal that bypassed the smart
// constructor. The always-emit convention matches nullable / returnsAll.
func projectionType(t Type) Type {
	if t == nil {
		return TypeUnknown{}
	}
	return t
}

// MarshalJSON renders a RefProjection as a tagged union member discriminated by
// "kind", flattening its Ref into sibling "variable"/"property" fields so the
// projection stays one level deep — same posture as PropertyUse. Stage 6: the
// result type is always emitted.
func (p RefProjection) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Kind     string `json:"kind"`
		Variable string `json:"variable"`
		Property string `json:"property"`
		Type     Type   `json:"type"`
	}{Kind: projectionKindRef, Variable: p.ref.Variable, Property: p.ref.Property, Type: projectionType(p.resultType)})
}

// MarshalJSON renders a LiteralProjection as a tagged union member discriminated
// by "kind". Stage 6: the literal's scalar-kind is emitted as "type".
func (p LiteralProjection) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Kind string `json:"kind"`
		Type Type   `json:"type"`
	}{Kind: projectionKindLiteral, Type: projectionType(p.resultType)})
}

// MarshalJSON renders a FuncProjection as a tagged union member discriminated by
// "kind", carrying its referenced bindings as "refs" and nothing of the function
// itself (§2). Stage 6: the result type is emitted as "type".
func (p FuncProjection) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Kind string    `json:"kind"`
		Refs []flatRef `json:"refs"`
		Type Type      `json:"type"`
	}{Kind: projectionKindFunc, Refs: flattenRefs(p.refs), Type: projectionType(p.resultType)})
}

// MarshalJSON renders an AggregateProjection as a tagged union member
// discriminated by "kind", emitting the aggregate kind as "func" (derived from
// AggregateFunc.String, the single source), its referenced bindings as "refs",
// the Stage-10 DISTINCT axis as "distinct" (always emitted, matching the
// always-emit convention nullable / directed / hops / returnsAll follow),
// and its Stage-10 result type as "type".
func (p AggregateProjection) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Kind     string    `json:"kind"`
		Func     string    `json:"func"`
		Refs     []flatRef `json:"refs"`
		Distinct bool      `json:"distinct"`
		Type     Type      `json:"type"`
	}{Kind: projectionKindAggregate, Func: p.fn.String(), Refs: flattenRefs(p.refs), Distinct: p.distinct, Type: projectionType(p.resultType)})
}

// Effect is one write operation the query performs at a specific query part
// (Stage 12): the per-part analogue of a return item for the write side. It
// is a closed sum of CreateEffect, DeleteEffect, SetPropertyEffect,
// SetEntityEffect, SetLabelsEffect, RemovePropertyEffect, and
// RemoveLabelsEffect — no other type can implement it — so an effect is
// exactly one of the seven. Each variant holds its data in unexported fields;
// smart constructors are the only way to build a non-zero value, so the
// invariants the types alone cannot express (non-empty target variables for
// the SET / REMOVE variants that carry one) hold for every value that exists.
//
// The Effect sum does not carry the value expression's tree (ADR 0003); the
// SET / DELETE value's structure lives below the type-interface boundary
// (ADR 0005), while its result type and the bindings it touches enter the
// model. Executing the query re-executes the original text with parameters
// bound, so the driver never needs the tree back — only the type interface
// codegen emits.
type Effect interface {
	isEffect()
}

// SetEffect is the sealed sub-sum of Effect implemented by exactly the three
// SET-family effect variants (SetPropertyEffect, SetEntityEffect,
// SetLabelsEffect). MergeEffect.OnMatch and MergeEffect.OnCreate carry
// []SetEffect (not []Effect), so the type system rejects a CreateEffect /
// DeleteEffect / MergeEffect / RemovePropertyEffect / RemoveLabelsEffect
// inside an ON action slot — matching the grammar's oC_MergeAction rule,
// which admits only oC_Set (Cypher.g4 §oC_MergeAction).
type SetEffect interface {
	Effect
	isSetEffect()
}

// CreateEffect records one CREATE clause: the ordered list of binding
// variables the clause introduced (named or anonymous — an empty string
// records an anonymous position, matching the raw binding slice's
// discipline). The bindings themselves live in the enclosing Part's
// Bindings slice (Stage 12: CREATE reuses collectPattern, so a
// CREATE-introduced binding enters the part's Bindings the same way a
// MATCH-introduced one does). This variant is a marker: the CreateEffect
// carries no value expression and no property Refs, only the variable-name
// index that tells a caller "these bindings came from this clause."
type CreateEffect struct {
	variables []string
}

// NewCreateEffect builds a CreateEffect. Total: variables is what the
// collectPattern walk observed (possibly empty for a CREATE that produced
// no bindings the caller cares to record); the smart constructor exists for
// the always-copy-on-construct discipline the other sums follow.
func NewCreateEffect(variables []string) CreateEffect {
	if len(variables) == 0 {
		return CreateEffect{}
	}
	cp := make([]string, len(variables))
	copy(cp, variables)
	return CreateEffect{variables: cp}
}

// Variables are the binding variable names the CREATE clause introduced, in
// walk order. May carry empty strings for anonymous positions (an anonymous
// edge is its own binding, C1). May be empty when the CREATE composed no
// bindings a caller cares to record.
func (e CreateEffect) Variables() []string { return e.variables }

func (CreateEffect) isEffect() {}

// MarshalJSON renders a CreateEffect as a tagged union member discriminated
// by "kind", carrying its variables list.
func (e CreateEffect) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Kind      string   `json:"kind"`
		Variables []string `json:"variables"`
	}{Kind: effectKindCreate, Variables: e.variables})
}

// DeleteEffect records one DELETE or DETACH DELETE clause: the Refs the
// clause names as deletion targets (bare var / var.prop shapes each), the
// Refs a rich-expression target touched (for a target like
// friends[$friendIndex] whose structure is below the type-interface
// boundary), and a Detach flag distinguishing DETACH DELETE (delete the
// entity plus every edge that touches it) from DELETE (require the entity's
// edges gone first). The Targets / Refs split preserves the type-interface
// information codegen needs — the bare shapes enter Targets so the
// resolver can trace each to a schema entity kind, and the rich shapes
// enter Refs so referential integrity still holds.
type DeleteEffect struct {
	targets []Ref
	refs    []Ref
	detach  bool
}

// NewDeleteEffect builds a DeleteEffect. Total: targets and refs are the
// walk's observations (both may be nil); detach comes from the DETACH token
// on the clause.
func NewDeleteEffect(targets, refs []Ref, detach bool) DeleteEffect {
	var t, r []Ref
	if len(targets) > 0 {
		t = make([]Ref, len(targets))
		copy(t, targets)
	}
	if len(refs) > 0 {
		r = make([]Ref, len(refs))
		copy(r, refs)
	}
	return DeleteEffect{targets: t, refs: r, detach: detach}
}

// Targets are the bare var / var.prop shapes the DELETE names as targets,
// in walk order.
func (e DeleteEffect) Targets() []Ref { return e.targets }

// Refs are the var / var.prop atoms a rich-expression DELETE target
// touched (nil for pure bare-target DELETEs). Referential-integrity
// coverage: every Ref here must resolve, exactly as a projection's Refs
// must.
func (e DeleteEffect) Refs() []Ref { return e.refs }

// Detach reports whether the clause was DETACH DELETE (true) or plain
// DELETE (false). The engine's semantic distinction (cascade edges vs
// require them absent) is a runtime rule; the axis is preserved on the
// model for codegen.
func (e DeleteEffect) Detach() bool { return e.detach }

func (DeleteEffect) isEffect() {}

// MarshalJSON renders a DeleteEffect as a tagged union member discriminated
// by "kind", carrying its targets, refs, and always-emitted detach flag.
func (e DeleteEffect) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Kind    string    `json:"kind"`
		Targets []flatRef `json:"targets"`
		Refs    []flatRef `json:"refs"`
		Detach  bool      `json:"detach"`
	}{Kind: effectKindDelete, Targets: flattenRefs(e.targets), Refs: flattenRefs(e.refs), Detach: e.detach})
}

// SetOp is which SET-item alternative one SetEntityEffect represents (Stage
// 12): SetOpReplace for `SET var = expression` (whole-entity replace),
// SetOpMerge for `SET var += expression` (map-merge onto the existing
// entity). The distinction changes result semantics — replace clears
// properties not in the RHS map; merge keeps them — so the model preserves
// the axis. Int-backed with a stringer, mirroring UnionKind / AggregateFunc.
type SetOp int

const (
	// SetOpReplace is `SET var = expression`: replace the entity's
	// properties with the RHS map.
	SetOpReplace SetOp = iota
	// SetOpMerge is `SET var += expression`: merge the RHS map onto the
	// entity, keeping properties not in the RHS.
	SetOpMerge
)

// String is the canonical wire name of the op ("replace" / "merge").
func (o SetOp) String() string {
	if o == SetOpMerge {
		return "merge"
	}
	return "replace"
}

// MarshalJSON renders a SetOp as its wire string.
func (o SetOp) MarshalJSON() ([]byte, error) {
	return json.Marshal(o.String())
}

// SetPropertyEffect records one SET item of the shape `n.prop = value` — a
// single property assignment. It carries the property target (a Ref{Variable,
// Property} pair, single-level: the model does not carry multi-level lookups
// like n.a.b.c at Stage 12; a multi-level LHS rejects at parse with
// ErrNestedPropertyTarget, a bucket-1 sentinel — see the Stage-12 spec §1.5
// and §8 "Nested SET/REMOVE LHS is a hard reject"). The value expression is
// typed via the Stage-6 rich typer, and its result type enters the effect
// (the typed-write contract that lets the resolver infer parameter types).
// Refs are the var / var.prop atoms the value expression touched, so
// referential integrity covers them.
type SetPropertyEffect struct {
	target    Ref
	valueType Type
	refs      []Ref
}

// NewSetPropertyEffect builds a SetPropertyEffect. Rejects an empty target
// variable (a SET without a variable is a parser bug).
func NewSetPropertyEffect(target Ref, valueType Type, refs []Ref) (SetPropertyEffect, error) {
	if target.Variable == "" {
		return SetPropertyEffect{}, errors.New("query: SetPropertyEffect requires a non-empty target variable")
	}
	if valueType == nil {
		valueType = TypeUnknown{}
	}
	var cp []Ref
	if len(refs) > 0 {
		cp = make([]Ref, len(refs))
		copy(cp, refs)
	}
	return SetPropertyEffect{target: target, valueType: valueType, refs: cp}, nil
}

// Target is the property being assigned: Ref{Variable, Property}, single-
// level.
func (e SetPropertyEffect) Target() Ref { return e.target }

// ValueType is the Stage-6 result type of the value expression on the RHS.
// TypeUnknown when the parser cannot commit.
func (e SetPropertyEffect) ValueType() Type { return e.valueType }

// Refs are the var / var.prop atoms the value expression touched.
func (e SetPropertyEffect) Refs() []Ref { return e.refs }

func (SetPropertyEffect) isEffect()    {}
func (SetPropertyEffect) isSetEffect() {}

// MarshalJSON renders a SetPropertyEffect as a tagged union member
// discriminated by "kind", flattening the target Ref into sibling
// "variable"/"property" fields (matching the PropertyUse posture) and
// always-emitting the value type.
func (e SetPropertyEffect) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Kind     string    `json:"kind"`
		Variable string    `json:"variable"`
		Property string    `json:"property"`
		Type     Type      `json:"type"`
		Refs     []flatRef `json:"refs"`
	}{Kind: effectKindSetProperty, Variable: e.target.Variable, Property: e.target.Property, Type: projectionType(e.valueType), Refs: flattenRefs(e.refs)})
}

// SetEntityEffect records one SET item of the shape `var = value` or
// `var += value` — a whole-entity replace / map-merge. The op axis
// distinguishes the two alternatives (SetOpReplace / SetOpMerge). The
// value's Stage-6 type and its touched refs enter the effect for the same
// reasons as SetPropertyEffect.
type SetEntityEffect struct {
	targetVar string
	op        SetOp
	valueType Type
	refs      []Ref
}

// NewSetEntityEffect builds a SetEntityEffect. Rejects an empty target
// variable.
func NewSetEntityEffect(targetVar string, op SetOp, valueType Type, refs []Ref) (SetEntityEffect, error) {
	if targetVar == "" {
		return SetEntityEffect{}, errors.New("query: SetEntityEffect requires a non-empty target variable")
	}
	if valueType == nil {
		valueType = TypeUnknown{}
	}
	var cp []Ref
	if len(refs) > 0 {
		cp = make([]Ref, len(refs))
		copy(cp, refs)
	}
	return SetEntityEffect{targetVar: targetVar, op: op, valueType: valueType, refs: cp}, nil
}

// TargetVariable is the variable being assigned to.
func (e SetEntityEffect) TargetVariable() string { return e.targetVar }

// Op is the assignment alternative (replace / merge).
func (e SetEntityEffect) Op() SetOp { return e.op }

// ValueType is the Stage-6 result type of the value expression.
func (e SetEntityEffect) ValueType() Type { return e.valueType }

// Refs are the var / var.prop atoms the value expression touched.
func (e SetEntityEffect) Refs() []Ref { return e.refs }

func (SetEntityEffect) isEffect()    {}
func (SetEntityEffect) isSetEffect() {}

// MarshalJSON renders a SetEntityEffect as a tagged union member
// discriminated by "kind", always-emitting the op axis.
func (e SetEntityEffect) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Kind     string    `json:"kind"`
		Variable string    `json:"variable"`
		Op       string    `json:"op"`
		Type     Type      `json:"type"`
		Refs     []flatRef `json:"refs"`
	}{Kind: effectKindSetEntity, Variable: e.targetVar, Op: e.op.String(), Type: projectionType(e.valueType), Refs: flattenRefs(e.refs)})
}

// SetLabelsEffect records one SET item of the shape `var :Labels` — add a
// label set to an existing entity. Carries the target variable name and the
// labels as written.
type SetLabelsEffect struct {
	targetVar string
	labels    graph.LabelSet
}

// NewSetLabelsEffect builds a SetLabelsEffect. Rejects an empty target
// variable or an empty label set (a `SET var:` with no label is a
// grammatical error).
func NewSetLabelsEffect(targetVar string, labels graph.LabelSet) (SetLabelsEffect, error) {
	if targetVar == "" {
		return SetLabelsEffect{}, errors.New("query: SetLabelsEffect requires a non-empty target variable")
	}
	if len(labels) == 0 {
		return SetLabelsEffect{}, errors.New("query: SetLabelsEffect requires at least one label")
	}
	return SetLabelsEffect{targetVar: targetVar, labels: labels}, nil
}

// TargetVariable is the variable whose labels are being augmented.
func (e SetLabelsEffect) TargetVariable() string { return e.targetVar }

// Labels are the labels being added.
func (e SetLabelsEffect) Labels() graph.LabelSet { return e.labels }

func (SetLabelsEffect) isEffect()    {}
func (SetLabelsEffect) isSetEffect() {}

// MarshalJSON renders a SetLabelsEffect as a tagged union member
// discriminated by "kind".
func (e SetLabelsEffect) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Kind     string         `json:"kind"`
		Variable string         `json:"variable"`
		Labels   graph.LabelSet `json:"labels"`
	}{Kind: effectKindSetLabels, Variable: e.targetVar, Labels: e.labels})
}

// RemovePropertyEffect records one REMOVE item of shape `var.prop` — remove
// one property from an existing entity. Same single-level Ref discipline as
// SetPropertyEffect.
type RemovePropertyEffect struct {
	target Ref
}

// NewRemovePropertyEffect builds a RemovePropertyEffect. Rejects an empty
// target variable or property.
func NewRemovePropertyEffect(target Ref) (RemovePropertyEffect, error) {
	if target.Variable == "" {
		return RemovePropertyEffect{}, errors.New("query: RemovePropertyEffect requires a non-empty target variable")
	}
	if target.Property == "" {
		return RemovePropertyEffect{}, errors.New("query: RemovePropertyEffect requires a non-empty target property")
	}
	return RemovePropertyEffect{target: target}, nil
}

// Target is the property being removed.
func (e RemovePropertyEffect) Target() Ref { return e.target }

func (RemovePropertyEffect) isEffect() {}

// MarshalJSON renders a RemovePropertyEffect as a tagged union member
// discriminated by "kind", flattening the target Ref into sibling fields.
func (e RemovePropertyEffect) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Kind     string `json:"kind"`
		Variable string `json:"variable"`
		Property string `json:"property"`
	}{Kind: effectKindRemoveProperty, Variable: e.target.Variable, Property: e.target.Property})
}

// RemoveLabelsEffect records one REMOVE item of shape `var :Labels`.
type RemoveLabelsEffect struct {
	targetVar string
	labels    graph.LabelSet
}

// NewRemoveLabelsEffect builds a RemoveLabelsEffect. Rejects an empty
// target variable or label set.
func NewRemoveLabelsEffect(targetVar string, labels graph.LabelSet) (RemoveLabelsEffect, error) {
	if targetVar == "" {
		return RemoveLabelsEffect{}, errors.New("query: RemoveLabelsEffect requires a non-empty target variable")
	}
	if len(labels) == 0 {
		return RemoveLabelsEffect{}, errors.New("query: RemoveLabelsEffect requires at least one label")
	}
	return RemoveLabelsEffect{targetVar: targetVar, labels: labels}, nil
}

// TargetVariable is the variable whose labels are being removed.
func (e RemoveLabelsEffect) TargetVariable() string { return e.targetVar }

// Labels are the labels being removed.
func (e RemoveLabelsEffect) Labels() graph.LabelSet { return e.labels }

func (RemoveLabelsEffect) isEffect() {}

// MarshalJSON renders a RemoveLabelsEffect as a tagged union member
// discriminated by "kind".
func (e RemoveLabelsEffect) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Kind     string         `json:"kind"`
		Variable string         `json:"variable"`
		Labels   graph.LabelSet `json:"labels"`
	}{Kind: effectKindRemoveLabels, Variable: e.targetVar, Labels: e.labels})
}

// MergeEffect records one MERGE clause: the ordered list of binding variables
// the clause introduced (identical semantics to CreateEffect.Variables — the
// delta over the enclosing Part's Bindings after collectPattern runs) plus
// the two optional ON action branches. OnMatch holds the SetEffects the
// ON MATCH SET action produced (empty if no ON MATCH clause); OnCreate holds
// the SetEffects the ON CREATE SET action produced (empty if no ON CREATE
// clause). Each ON slot is []SetEffect (the sealed sub-sum of Effect) so
// only Set-family effects can appear inside — matching the grammar's
// oC_MergeAction rule (Cypher.g4 §oC_MergeAction admits only oC_Set).
//
// A MERGE clause is semantically a match-or-create alternation: the engine
// attempts match first, and creates only on miss. Representing this as
// CreateEffect would erase the "match this if you can" half — a wrong
// concrete representation. The dedicated variant preserves the axis a caller
// walking Part.Effects needs to distinguish "definitely creates" from
// "creates iff no match" (spec §1.1).
//
// No aggregate Refs on MergeEffect: nested SetEffect.Refs cover the ON
// branches' value expressions, and the enclosing Part.refs (walked by
// buildPart's referential-integrity sweep) covers pattern refs and the
// ON-branch refs the SetEffect handlers already push into curPart.refs.
// An aggregate would duplicate and can drift (spec §3.1 / Q4).
type MergeEffect struct {
	variables []string
	onMatch   []SetEffect
	onCreate  []SetEffect
}

// NewMergeEffect builds a MergeEffect. Total: variables is what the
// collectPattern walk observed (possibly empty for a MERGE whose pattern
// composed no new bindings); onMatch and onCreate are the ordered SetEffects
// each ON action produced (both may be nil for a MERGE with no ON branch).
// Each entry in onMatch / onCreate must be non-nil — a nil interface value
// would surface as {"kind": ""} on the wire, so the constructor rejects it
// with a domain error. Same discipline the other Effect constructors follow.
func NewMergeEffect(variables []string, onMatch, onCreate []SetEffect) (MergeEffect, error) {
	var vs []string
	if len(variables) > 0 {
		vs = make([]string, len(variables))
		copy(vs, variables)
	}
	var om, oc []SetEffect
	if len(onMatch) > 0 {
		om = make([]SetEffect, len(onMatch))
		for i, e := range onMatch {
			if e == nil {
				return MergeEffect{}, errors.New("query: MergeEffect OnMatch contains a nil SetEffect")
			}
			om[i] = e
		}
	}
	if len(onCreate) > 0 {
		oc = make([]SetEffect, len(onCreate))
		for i, e := range onCreate {
			if e == nil {
				return MergeEffect{}, errors.New("query: MergeEffect OnCreate contains a nil SetEffect")
			}
			oc[i] = e
		}
	}
	return MergeEffect{variables: vs, onMatch: om, onCreate: oc}, nil
}

// Variables are the binding variable names the MERGE clause introduced, in
// walk order. Same semantics as CreateEffect.Variables: may carry empty
// strings for anonymous positions, may be empty when the MERGE composed no
// bindings a caller cares to record.
func (e MergeEffect) Variables() []string { return e.variables }

// OnMatch are the SetEffects the ON MATCH SET action produced, in walk
// order. Nil if the MERGE has no ON MATCH clause.
func (e MergeEffect) OnMatch() []SetEffect { return e.onMatch }

// OnCreate are the SetEffects the ON CREATE SET action produced, in walk
// order. Nil if the MERGE has no ON CREATE clause.
func (e MergeEffect) OnCreate() []SetEffect { return e.onCreate }

func (MergeEffect) isEffect() {}

// MarshalJSON renders a MergeEffect as a tagged union member discriminated
// by "kind", carrying its variables list and the two ON branches. Each
// element of onMatch / onCreate marshals through its concrete SetEffect
// variant's MarshalJSON, preserving the "kind" discriminator inside.
// Empty slices marshal as null — matching CreateEffect.Variables /
// DeleteEffect.Targets / DeleteEffect.Refs (spec §1.3 / A2 fold-in).
func (e MergeEffect) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Kind      string      `json:"kind"`
		Variables []string    `json:"variables"`
		OnMatch   []SetEffect `json:"onMatch"`
		OnCreate  []SetEffect `json:"onCreate"`
	}{Kind: effectKindMerge, Variables: e.variables, OnMatch: e.onMatch, OnCreate: e.onCreate})
}

// The Effect discriminators have no graph-vocabulary counterpart (the
// distinction is query-side only), so they are named here, the one place
// they are emitted. They sit next to the other kind constants per the
// Stage-3 spec §5 convention.
const (
	effectKindCreate         = "create"
	effectKindDelete         = "delete"
	effectKindSetProperty    = "setProperty"
	effectKindSetEntity      = "setEntity"
	effectKindSetLabels      = "setLabels"
	effectKindRemoveProperty = "removeProperty"
	effectKindRemoveLabels   = "removeLabels"
	effectKindMerge          = "merge"
)
