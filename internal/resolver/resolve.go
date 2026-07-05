package resolver

import (
	"fmt"
	"sort"

	"github.com/areqag/gqlc/internal/graph"
	"github.com/areqag/gqlc/internal/procsig"
	"github.com/areqag/gqlc/internal/query"
	"github.com/areqag/gqlc/internal/schema"
)

// resolve is the R0..R1 resolution kernel: pure, deterministic, short-circuit.
// It walks a query.Query and produces a ValidatedQuery for the R1 capability
// scope (§7 of the R1 spec): one branch, one part, one or more node and
// directed single-hop single-type edge bindings, only RefProjection items, no
// writes, no CALL, no WITH, no UNION, no RETURN DISTINCT, no RETURN *. R1
// resolves unlabelled node bindings via inference from the edges that touch
// them. Everything else routes to ErrOutOfR0Scope.
func resolve(q query.Query, s schema.Schema, _ procsig.Registry) (ValidatedQuery, error) {
	if len(q.Branches) != 1 || len(q.Combinators) != 0 {
		return ValidatedQuery{}, fmt.Errorf("%w: UNION / multi-branch query", ErrOutOfR0Scope)
	}
	branch := q.Branches[0]
	if len(branch.Parts) != 1 {
		return ValidatedQuery{}, fmt.Errorf("%w: WITH / multi-part query", ErrOutOfR0Scope)
	}
	part := branch.Parts[0]

	if len(part.Effects) != 0 {
		return ValidatedQuery{}, fmt.Errorf("%w: write clause", ErrOutOfR0Scope)
	}
	if part.Distinct {
		return ValidatedQuery{}, fmt.Errorf("%w: RETURN DISTINCT / WITH DISTINCT", ErrOutOfR0Scope)
	}
	if part.ReturnsAll {
		return ValidatedQuery{}, fmt.Errorf("%w: RETURN * / WITH *", ErrOutOfR0Scope)
	}
	if len(part.Bindings) == 0 {
		return ValidatedQuery{}, fmt.Errorf("%w: empty binding set", ErrOutOfR0Scope)
	}

	nodeTypes := make(map[string]schema.NodeType)
	edgeTypes := make(map[string]schema.EdgeType)
	edgeKeys := make(map[string]schema.EdgeKey)

	// Phase A1: labelled node bindings. Also reject unsupported binding
	// kinds; unlabelled node bindings are deferred to Phase B.
	var pendingNodes []query.NodeBinding
	var supportedEdges []query.EdgeBinding
	for _, b := range part.Bindings {
		switch bb := b.(type) {
		case query.NodeBinding:
			if len(bb.Labels()) == 0 {
				pendingNodes = append(pendingNodes, bb)
				continue
			}
			key := bb.Labels().Key()
			nt, ok := s.Nodes[key]
			if !ok {
				return ValidatedQuery{}, fmt.Errorf("%w: %s", ErrUnknownLabel, key)
			}
			nodeTypes[bb.Variable()] = nt
		case query.EdgeBinding:
			if err := r1EdgeAdmissible(bb); err != nil {
				return ValidatedQuery{}, err
			}
			supportedEdges = append(supportedEdges, bb)
		default:
			return ValidatedQuery{}, fmt.Errorf("%w: %s binding", ErrOutOfR0Scope, b.Kind())
		}
	}

	// Phase A2: labelled directed single-hop edges — attempt EdgeKey
	// formation. Edges whose endpoints are not yet fully labelled are
	// deferred to Phase C.
	deferredEdges := make([]query.EdgeBinding, 0, len(supportedEdges))
	for _, e := range supportedEdges {
		src, srcOK := endpointLabels(e.Source(), nodeTypes)
		tgt, tgtOK := endpointLabels(e.Target(), nodeTypes)
		if !srcOK || !tgtOK {
			deferredEdges = append(deferredEdges, e)
			continue
		}
		if err := closeEdge(e, src, tgt, s, edgeTypes, edgeKeys); err != nil {
			return ValidatedQuery{}, err
		}
	}

	// Phase B: unlabelled-node inference to a fixed point.
	if err := inferUnlabelled(pendingNodes, supportedEdges, s, nodeTypes); err != nil {
		return ValidatedQuery{}, err
	}

	// Phase C: close deferred edges against the now-complete node table.
	for _, e := range deferredEdges {
		src, srcOK := endpointLabels(e.Source(), nodeTypes)
		tgt, tgtOK := endpointLabels(e.Target(), nodeTypes)
		if !srcOK {
			return ValidatedQuery{}, fmt.Errorf("%w: cannot infer type of source endpoint of edge %q", ErrUnknownLabel, e.Variable())
		}
		if !tgtOK {
			return ValidatedQuery{}, fmt.Errorf("%w: cannot infer type of target endpoint of edge %q", ErrUnknownLabel, e.Variable())
		}
		if err := closeEdge(e, src, tgt, s, edgeTypes, edgeKeys); err != nil {
			return ValidatedQuery{}, err
		}
	}

	if len(part.Returns) == 0 {
		return ValidatedQuery{}, fmt.Errorf("%w: empty projection", ErrOutOfR0Scope)
	}

	columns := make([]Column, 0, len(part.Returns))
	for _, item := range part.Returns {
		colType, err := projectionType(item.Value, nodeTypes, edgeTypes, edgeKeys)
		if err != nil {
			return ValidatedQuery{}, err
		}
		columns = append(columns, Column{Name: item.Name, Type: colType})
	}

	params := make([]ResolvedParameter, 0, len(q.Parameters))
	for _, p := range q.Parameters {
		var unified ResolvedType
		for i, u := range p.Uses {
			w, err := useWitness(u, nodeTypes, edgeTypes)
			if err != nil {
				return ValidatedQuery{}, err
			}
			if i == 0 {
				unified = w
				continue
			}
			merged, ok := unify(unified, w)
			if !ok {
				return ValidatedQuery{}, fmt.Errorf("%w: parameter %q: %s vs %s", ErrParameterTypeConflict, p.Name, unified.String(), w.String())
			}
			unified = merged
		}
		params = append(params, ResolvedParameter{Name: p.Name, Type: unified})
	}

	return ValidatedQuery{
		Columns:    columns,
		Parameters: params,
		Statement:  StatementKind(q.StatementKind),
	}, nil
}

// r1EdgeAdmissible screens an EdgeBinding against R1's edge shape predicate:
// directed, single-hop, single-type. Everything else — undirected, var-length,
// multi-type, untyped — routes to ErrOutOfR0Scope with a fail-message
// distinguishing the sub-case.
func r1EdgeAdmissible(e query.EdgeBinding) error {
	if !e.Directed() {
		return fmt.Errorf("%w: undirected edge", ErrOutOfR0Scope)
	}
	if e.Hops() != nil {
		return fmt.Errorf("%w: variable-length edge", ErrOutOfR0Scope)
	}
	switch len(e.Labels()) {
	case 0:
		return fmt.Errorf("%w: untyped edge", ErrOutOfR0Scope)
	case 1:
		return nil
	default:
		return fmt.Errorf("%w: multi-type edge", ErrOutOfR0Scope)
	}
}

// endpointLabels reads the labels an edge endpoint carries at the point
// EdgeKey formation needs them: for a VarEndpoint, the labels of the binding
// it names (already resolved in Phase A or B); for an InlineEndpoint, the
// labels written inline on the pattern. Returns (canonicalKey, ok): ok is
// false when the endpoint is a VarEndpoint whose binding is still pending
// inference or an empty-labels InlineEndpoint.
func endpointLabels(e query.Endpoint, resolved map[string]schema.NodeType) (graph.LabelSetKey, bool) {
	switch ep := e.(type) {
	case query.VarEndpoint:
		nt, ok := resolved[ep.Variable()]
		if !ok {
			return "", false
		}
		return nt.Labels, true
	case query.InlineEndpoint:
		ls := ep.Labels()
		if len(ls) == 0 {
			return "", false
		}
		return ls.Key(), true
	default:
		// Unreachable: Endpoint is a sealed sum of VarEndpoint and
		// InlineEndpoint (internal/query/query.go:939-941).
		return "", false
	}
}

// closeEdge forms the EdgeKey for one already-endpoint-resolved edge, looks
// it up in the schema, and records the type against the edge's variable (if
// named). An anonymous edge closes successfully but is not added to
// edgeTypes/edgeKeys — nothing can project it (§4.4).
func closeEdge(e query.EdgeBinding, src, tgt graph.LabelSetKey, s schema.Schema, edgeTypes map[string]schema.EdgeType, edgeKeys map[string]schema.EdgeKey) error {
	key := schema.EdgeKey{Source: src, Label: e.Labels().Key(), Target: tgt}
	et, ok := s.Edges[key]
	if !ok {
		return fmt.Errorf("%w: %s-[%s]->%s", ErrUnknownEdge, key.Source, key.Label, key.Target)
	}
	if v := e.Variable(); v != "" {
		edgeTypes[v] = et
		edgeKeys[v] = key
	}
	return nil
}

// inferUnlabelled runs Phase B: iterate the pending unlabelled node binding
// set, computing each binding's candidate set from the R1-supported edges
// that touch it, until every binding is committed or an unbreakable pending
// set remains. Returns ErrUnknownLabel for a binding with an empty candidate
// set and ErrAmbiguousBinding for a multi-candidate or cycle case.
func inferUnlabelled(pending []query.NodeBinding, edges []query.EdgeBinding, s schema.Schema, resolved map[string]schema.NodeType) error {
	if len(pending) == 0 {
		return nil
	}
	for len(pending) > 0 {
		var next []query.NodeBinding
		committed := 0
		for _, n := range pending {
			cands := candidateTypes(n, edges, s, resolved)
			switch len(cands) {
			case 0:
				return fmt.Errorf("%w: cannot infer type of unlabelled binding %q — no edge in the pattern reaches a compatible schema node type", ErrUnknownLabel, n.Variable())
			case 1:
				var only graph.LabelSetKey
				for k := range cands {
					only = k
				}
				resolved[n.Variable()] = s.Nodes[only]
				committed++
			default:
				next = append(next, n)
			}
		}
		if committed == 0 {
			// Zero-commit pass with pending remaining: either a genuine
			// multi-candidate or an unbreakable cycle. Fail on the first
			// pending (parser first-appearance order).
			n := next[0]
			cands := candidateTypes(n, edges, s, resolved)
			return fmt.Errorf("%w: cannot uniquely infer type of unlabelled binding %q — candidate types: %s", ErrAmbiguousBinding, n.Variable(), joinCandidates(cands))
		}
		pending = next
	}
	return nil
}

// candidateTypes computes the intersection of node-type candidates for one
// pending unlabelled binding across every R1-supported edge that touches it.
// A touching edge whose other endpoint is still unlabelled contributes
// nothing (it cannot constrain the binding alone).
func candidateTypes(n query.NodeBinding, edges []query.EdgeBinding, s schema.Schema, resolved map[string]schema.NodeType) map[graph.LabelSetKey]struct{} {
	var acc map[graph.LabelSetKey]struct{}
	for _, e := range edges {
		side, touches := touchingSide(e, n.Variable())
		if !touches {
			continue
		}
		other := e.Source()
		if side == "source" {
			other = e.Target()
		}
		otherKey, ok := endpointLabels(other, resolved)
		if !ok {
			continue
		}
		label := e.Labels().Key()
		cand := make(map[graph.LabelSetKey]struct{})
		for k := range s.Edges {
			if k.Label != label {
				continue
			}
			if side == "source" && k.Target == otherKey {
				cand[k.Source] = struct{}{}
			}
			if side == "target" && k.Source == otherKey {
				cand[k.Target] = struct{}{}
			}
		}
		if acc == nil {
			acc = cand
		} else {
			acc = intersect(acc, cand)
		}
	}
	if acc == nil {
		return map[graph.LabelSetKey]struct{}{}
	}
	return acc
}

// touchingSide reports whether edge e's source or target endpoint is a
// VarEndpoint naming variable v. Returns the side ("source"/"target") and
// whether the edge touches v.
func touchingSide(e query.EdgeBinding, v string) (string, bool) {
	if src, ok := e.Source().(query.VarEndpoint); ok && src.Variable() == v {
		return "source", true
	}
	if tgt, ok := e.Target().(query.VarEndpoint); ok && tgt.Variable() == v {
		return "target", true
	}
	return "", false
}

// intersect returns the set intersection of two label-set-key sets.
func intersect(a, b map[graph.LabelSetKey]struct{}) map[graph.LabelSetKey]struct{} {
	out := make(map[graph.LabelSetKey]struct{})
	for k := range a {
		if _, ok := b[k]; ok {
			out[k] = struct{}{}
		}
	}
	return out
}

// joinCandidates renders a candidate set as a deterministic
// ascending-sorted comma-separated list for a fail-message.
func joinCandidates(c map[graph.LabelSetKey]struct{}) string {
	keys := make([]string, 0, len(c))
	for k := range c {
		keys = append(keys, string(k))
	}
	sort.Strings(keys)
	out := ""
	for i, k := range keys {
		if i > 0 {
			out += ", "
		}
		out += k
	}
	return out
}

// projectionType dispatches a Projection to its handler and returns the
// column's resolved type. R2 admits RefProjection, LiteralProjection,
// FuncProjection, and ExprProjection; AggregateProjection routes to
// ErrOutOfR0Scope (R5 owns grouping). §4.5.
func projectionType(p query.Projection, nodeTypes map[string]schema.NodeType, edgeTypes map[string]schema.EdgeType, edgeKeys map[string]schema.EdgeKey) (ResolvedType, error) {
	switch pp := p.(type) {
	case query.RefProjection:
		return refProjectionType(pp.Ref(), nodeTypes, edgeTypes, edgeKeys)
	case query.LiteralProjection:
		return resolveType(pp.Type())
	case query.FuncProjection:
		return resolveType(pp.Type())
	case query.ExprProjection:
		return resolveType(pp.Type())
	case query.AggregateProjection:
		return nil, fmt.Errorf("%w: aggregate projection (R5 owns grouping)", ErrOutOfR0Scope)
	default:
		return nil, fmt.Errorf("%w: unknown projection variant (%T)", ErrOutOfR0Scope, p)
	}
}

// refProjectionType dispatches a RefProjection's Ref against the resolved
// node and edge binding tables. Whole-entity (Property == "") emits
// ResolvedNode or ResolvedEdge; property lookup emits ResolvedProperty via
// the schema witness. A ref naming no known binding at R2 is architecturally
// possible only for a variable pointing at an as-yet-unsupported binding
// kind (path, unwind, call) — those are rejected in Phase A with
// ErrOutOfR0Scope.
func refProjectionType(ref query.Ref, nodeTypes map[string]schema.NodeType, edgeTypes map[string]schema.EdgeType, edgeKeys map[string]schema.EdgeKey) (ResolvedType, error) {
	if nt, ok := nodeTypes[ref.Variable]; ok {
		if ref.Property == "" {
			return ResolvedNode{Labels: nt.Labels}, nil
		}
		prop, ok := nt.Properties[ref.Property]
		if !ok {
			return nil, fmt.Errorf("%w: %s.%s", ErrUnknownProperty, ref.Variable, ref.Property)
		}
		return ResolvedProperty{Type: prop.Type, Nullable: prop.Nullable}, nil
	}
	if et, ok := edgeTypes[ref.Variable]; ok {
		if ref.Property == "" {
			return ResolvedEdge{EdgeKey: edgeKeys[ref.Variable]}, nil
		}
		prop, ok := et.Properties[ref.Property]
		if !ok {
			return nil, fmt.Errorf("%w: %s.%s", ErrUnknownProperty, ref.Variable, ref.Property)
		}
		return ResolvedProperty{Type: prop.Type, Nullable: prop.Nullable}, nil
	}
	return nil, fmt.Errorf("%w: %s", ErrOutOfR0Scope, ref.Variable)
}

// resolveType maps a parser Type into its resolver ResolvedType per the R0
// §4 mapping table (R2 revision — §4.7). Deterministic and pure. Returns
// ErrOutOfR0Scope for TypeList{TypeNode|TypeEdge} — a list literal of bare
// entity variables that would forfeit the schema witness by collapsing to
// ResolvedNode{} / ResolvedEdge{}; deferred to R5. Panics on bare TypeNode /
// TypeEdge / TypePath — those are unreachable at R2 (RefProjection bypasses
// resolveType; path bindings are gated out).
func resolveType(t query.Type) (ResolvedType, error) {
	switch tt := t.(type) {
	case query.TypeBool:
		return ResolvedScalar{Kind: ScalarBool}, nil
	case query.TypeInt:
		return ResolvedScalar{Kind: ScalarInt}, nil
	case query.TypeFloat:
		return ResolvedScalar{Kind: ScalarFloat}, nil
	case query.TypeString:
		return ResolvedScalar{Kind: ScalarString}, nil
	case query.TypeNull:
		return ResolvedScalar{Kind: ScalarNull}, nil
	case query.TypeMap:
		return ResolvedScalar{Kind: ScalarMap}, nil
	case query.TypeDate:
		return ResolvedTemporal{Kind: TemporalDate}, nil
	case query.TypeTime:
		return ResolvedTemporal{Kind: TemporalTime}, nil
	case query.TypeLocalTime:
		return ResolvedTemporal{Kind: TemporalLocalTime}, nil
	case query.TypeDateTime:
		return ResolvedTemporal{Kind: TemporalDateTime}, nil
	case query.TypeLocalDateTime:
		return ResolvedTemporal{Kind: TemporalLocalDateTime}, nil
	case query.TypeDuration:
		return ResolvedTemporal{Kind: TemporalDuration}, nil
	case query.TypeList:
		switch tt.Element().(type) {
		case query.TypeNode:
			return nil, fmt.Errorf("%w: list-of-nodes projection", ErrOutOfR0Scope)
		case query.TypeEdge:
			return nil, fmt.Errorf("%w: list-of-edges projection", ErrOutOfR0Scope)
		}
		el, err := resolveType(tt.Element())
		if err != nil {
			return nil, err
		}
		return ResolvedList{Element: el}, nil
	case query.TypeUnknown:
		return ResolvedUnknown{}, nil
	case query.TypeNode:
		panic("resolver bug: resolveType reached bare TypeNode (RefProjection bypasses this mapper)")
	case query.TypeEdge:
		panic("resolver bug: resolveType reached bare TypeEdge (RefProjection bypasses this mapper)")
	case query.TypePath:
		panic("resolver bug: resolveType reached TypePath (R2 does not admit path bindings)")
	default:
		panic(fmt.Sprintf("resolver bug: resolveType reached unhandled query.Type %T", t))
	}
}

// useWitness computes the ResolvedType witness for one parameter Use.
// §4.6. Dispatches on the sealed Use sum. Write-side ExprUses
// (ExprInSetValue / ExprInDeleteTarget) route to ErrOutOfR0Scope.
func useWitness(u query.Use, nodeTypes map[string]schema.NodeType, edgeTypes map[string]schema.EdgeType) (ResolvedType, error) {
	switch uu := u.(type) {
	case query.PropertyUse:
		return propertyUseWitness(uu.Ref(), nodeTypes, edgeTypes)
	case query.ClauseSlotUse:
		return ResolvedScalar{Kind: ScalarInt}, nil
	case query.ExprUse:
		switch uu.Position() {
		case query.ExprInProjection, query.ExprInPredicate:
			return resolveType(uu.EnclosingType())
		case query.ExprInSetValue:
			return nil, fmt.Errorf("%w: parameter used in SET value", ErrOutOfR0Scope)
		case query.ExprInDeleteTarget:
			return nil, fmt.Errorf("%w: parameter used in DELETE target", ErrOutOfR0Scope)
		default:
			return nil, fmt.Errorf("%w: unknown ExprUse position", ErrOutOfR0Scope)
		}
	default:
		return nil, fmt.Errorf("%w: unknown Use variant (%T)", ErrOutOfR0Scope, u)
	}
}

// propertyUseWitness looks up the schema property named by a PropertyUse's
// Ref. Miss -> ErrUnknownProperty. §4.6.
func propertyUseWitness(ref query.Ref, nodeTypes map[string]schema.NodeType, edgeTypes map[string]schema.EdgeType) (ResolvedType, error) {
	if nt, ok := nodeTypes[ref.Variable]; ok {
		prop, ok := nt.Properties[ref.Property]
		if !ok {
			return nil, fmt.Errorf("%w: %s.%s", ErrUnknownProperty, ref.Variable, ref.Property)
		}
		return ResolvedProperty{Type: prop.Type, Nullable: prop.Nullable}, nil
	}
	if et, ok := edgeTypes[ref.Variable]; ok {
		prop, ok := et.Properties[ref.Property]
		if !ok {
			return nil, fmt.Errorf("%w: %s.%s", ErrUnknownProperty, ref.Variable, ref.Property)
		}
		return ResolvedProperty{Type: prop.Type, Nullable: prop.Nullable}, nil
	}
	return nil, fmt.Errorf("%w: %s", ErrOutOfR0Scope, ref.Variable)
}

// unify agrees two ResolvedTypes iff they are structurally equal or one side
// is ResolvedUnknown (the resolver's honest bottom — any concrete witness
// dominates it). Returns the agreed type and true on success, (nil, false)
// on conflict. §4.8.
func unify(a, b ResolvedType) (ResolvedType, bool) {
	if _, ok := a.(ResolvedUnknown); ok {
		return b, true
	}
	if _, ok := b.(ResolvedUnknown); ok {
		return a, true
	}
	switch aa := a.(type) {
	case ResolvedProperty:
		bb, ok := b.(ResolvedProperty)
		if !ok || bb.Type != aa.Type || bb.Nullable != aa.Nullable {
			return nil, false
		}
		return aa, true
	case ResolvedScalar:
		bb, ok := b.(ResolvedScalar)
		if !ok || bb.Kind != aa.Kind {
			return nil, false
		}
		return aa, true
	case ResolvedTemporal:
		bb, ok := b.(ResolvedTemporal)
		if !ok || bb.Kind != aa.Kind {
			return nil, false
		}
		return aa, true
	case ResolvedList:
		bb, ok := b.(ResolvedList)
		if !ok {
			return nil, false
		}
		el, ok := unify(aa.Element, bb.Element)
		if !ok {
			return nil, false
		}
		return ResolvedList{Element: el}, true
	case ResolvedNode:
		bb, ok := b.(ResolvedNode)
		if !ok || bb.Labels != aa.Labels {
			return nil, false
		}
		return aa, true
	case ResolvedEdge:
		bb, ok := b.(ResolvedEdge)
		if !ok || bb.EdgeKey != aa.EdgeKey {
			return nil, false
		}
		return aa, true
	default:
		return nil, false
	}
}
