package resolver

import (
	"fmt"
	"sort"
	"strings"

	"github.com/areqag/gqlc/internal/graph"
	"github.com/areqag/gqlc/internal/procsig"
	"github.com/areqag/gqlc/internal/query"
	"github.com/areqag/gqlc/internal/schema"
)

// resolve is the R5 kernel: it walks q.Branches left-to-right, resolves each
// branch's Part chain via resolveBranch, certifies branch-0 column compatibility
// against every other branch (§4.3), witnesses parameter Uses against the
// merged binding scope from every Part (§4.2.4), and folds Part.Distinct +
// UnionKind into ValidatedQuery.Distinct (§3.2/§4.7).
func resolve(q query.Query, s schema.Schema, r procsig.Registry) (ValidatedQuery, error) {
	if len(q.Branches) == 0 {
		// Defensive tripwire; the parser's buildBranch guarantees >= 1
		// (Query is a builder-maintained product type). Unreachable via parse.
		return ValidatedQuery{}, fmt.Errorf("%w: empty branches", ErrOutOfR0Scope)
	}

	branchCols := make([][]Column, len(q.Branches))
	var mergedScopes []partScope

	for b, branch := range q.Branches {
		cols, uses, err := resolveBranch(branch, s, r)
		if err != nil {
			return ValidatedQuery{}, err
		}
		branchCols[b] = cols
		mergedScopes = append(mergedScopes, useSitesToScopes(uses)...)
	}

	if err := compareBranchColumns(branchCols); err != nil {
		return ValidatedQuery{}, err
	}

	params, err := unifyParameterUsesAcrossScopes(q.Parameters, mergedScopes, s)
	if err != nil {
		return ValidatedQuery{}, err
	}

	return ValidatedQuery{
		Columns:    branchCols[0],
		Parameters: params,
		Statement:  StatementKind(q.StatementKind),
		Distinct:   computeDistinct(q),
	}, nil
}

// partScope captures the resolver-typed binding tables in effect at one Part —
// enough for the top-level parameter walker to witness every Use against the
// Part whose Ref names an in-scope binding. Threaded out of resolveBranch via
// parameterUseSite (one site per Part; the caller reconstructs scopes at
// walk time).
type partScope struct {
	nodeTypes       map[string]schema.NodeType
	edgeTypes       map[string]schema.EdgeType
	edgeCands       map[string][]schema.EdgeKey
	edgeBindings    map[string]query.EdgeBinding
	nullableBinding map[string]bool
}

// useSitesToScopes is the adapter from resolveBranch's []parameterUseSite (its
// pinned second return) to a []partScope the top-level walker consumes. Every
// parameterUseSite in R5 wraps one Part's scope snapshot.
func useSitesToScopes(sites []parameterUseSite) []partScope {
	out := make([]partScope, 0, len(sites))
	for _, s := range sites {
		out = append(out, s.scope)
	}
	return out
}

// branchState is the resolver-typed carry from Part K to Part K+1 within one
// branch (§4.2.1). All maps nil for Part 0 (empty carry).
type branchState struct {
	exportedNodeTypes       map[string]schema.NodeType
	exportedEdgeTypes       map[string]schema.EdgeType
	exportedEdgeKeys        map[string]schema.EdgeKey
	exportedEdgeCands       map[string][]schema.EdgeKey
	exportedEdgeBindings    map[string]query.EdgeBinding
	exportedNullableBinding map[string]bool
	exportedResolvedTypes   map[string]ResolvedType
	exportedOrder           []string
}

// parameterUseSite is resolveBranch's second-return element (pinned type per
// R5 §2.2). In this implementation each site carries one Part's resolved-scope
// snapshot — enough for the top-level unifier to witness every Use whose Ref
// names a binding in-scope at that Part (§4.2.4). The parser does not attribute
// Uses to Parts at the wire level, so per-Part witnessing runs at the top-level
// resolve() after every branch has resolved its Parts.
type parameterUseSite struct {
	scope partScope
}

// resolveBranch walks a branch's Parts left-to-right, threading a branchState
// carry. Returns the final Part's resolved column list (with grouping-key bits
// filled), the aggregated parameter-Use witnesses collected across every Part,
// and the first failure encountered.
//
// Pinned signature per R5 §2.2 / team-lead brief.
func resolveBranch(branch query.Branch, s schema.Schema, r procsig.Registry) ([]Column, []parameterUseSite, error) {
	_ = r // R5 does not admit CALL; the registry is reserved for R7.
	if len(branch.Parts) == 0 {
		// Defensive tripwire; parser's buildBranch guarantees >= 1.
		return nil, nil, fmt.Errorf("%w: empty parts", ErrOutOfR0Scope)
	}

	var carry branchState
	var allUses []parameterUseSite
	var finalCols []Column
	var finalPart query.Part
	lastIdx := len(branch.Parts) - 1

	for k, part := range branch.Parts {
		cols, exported, uses, err := resolvePart(part, carry, s)
		if err != nil {
			return nil, nil, err
		}
		allUses = append(allUses, uses...)
		carry = exported
		if k == lastIdx {
			finalCols = cols
			finalPart = part
		}
	}

	// Grouping-key discovery runs only for the final Part (§4.5). The
	// per-column bit lives on ValidatedQuery.Columns (§3.2.1); no other
	// consumer reads it. Non-final Parts fold via exportedResolvedTypes.
	fillGroupingKeys(finalCols, finalPart)
	return finalCols, allUses, nil
}

// resolvePart runs the per-Part kernel: R4's Phase A/B/C for the local
// bindings, R4's Phase D nullability with a carry-seed extension (§4.6),
// carried-scope-seeded binding tables (§4.2.3), projection walk with
// AggregateProjection support (§4.5) and RETURN * / WITH * expansion (§4.4),
// and per-Part parameter-Use witness collection (§4.2.4). Returns the Part's
// column list (unfilled GroupingKey; filled by resolveBranch on the final
// Part), the branchState exported to Part K+1 (§4.2.2), and the parameter-Use
// witnesses collected inside this Part.
func resolvePart(part query.Part, carry branchState, s schema.Schema) ([]Column, branchState, []parameterUseSite, error) {
	if len(part.Effects) != 0 {
		return nil, branchState{}, nil, fmt.Errorf("%w: write clause", ErrOutOfR0Scope)
	}

	nodeTypes := make(map[string]schema.NodeType)
	edgeTypes := make(map[string]schema.EdgeType)
	edgeKeys := make(map[string]schema.EdgeKey)
	edgeCands := make(map[string][]schema.EdgeKey)
	edgeBindings := make(map[string]query.EdgeBinding)
	// Carry seed happens BEFORE local bindings write in — local shadows carry
	// per §4.2.3.
	for name, nt := range carry.exportedNodeTypes {
		nodeTypes[name] = nt
	}
	for name, et := range carry.exportedEdgeTypes {
		edgeTypes[name] = et
	}
	for name, k := range carry.exportedEdgeKeys {
		edgeKeys[name] = k
	}
	for name, cands := range carry.exportedEdgeCands {
		edgeCands[name] = cands
	}
	for name, b := range carry.exportedEdgeBindings {
		edgeBindings[name] = b
	}

	// Phase A1: local labelled node bindings (shadows carry) + edge admission
	// screening. Unlabelled node bindings defer to Phase B.
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
				return nil, branchState{}, nil, fmt.Errorf("%w: %s", ErrUnknownLabel, key)
			}
			nodeTypes[bb.Variable()] = nt
			// Local binding shadows any carried edge state at the same name;
			// R5 §4.2.3 shadowing rule.
			delete(edgeTypes, bb.Variable())
			delete(edgeKeys, bb.Variable())
			delete(edgeCands, bb.Variable())
			delete(edgeBindings, bb.Variable())
		case query.EdgeBinding:
			if err := r3EdgeAdmissible(bb); err != nil {
				return nil, branchState{}, nil, err
			}
			supportedEdges = append(supportedEdges, bb)
			if v := bb.Variable(); v != "" {
				edgeBindings[v] = bb
				// Edge shadows any carried node state.
				delete(nodeTypes, v)
			}
		default:
			return nil, branchState{}, nil, fmt.Errorf("%w: %s binding", ErrOutOfR0Scope, b.Kind())
		}
	}

	// Phase A2: R3-admitted edges — attempt candidate-set formation.
	deferredEdges := make([]query.EdgeBinding, 0, len(supportedEdges))
	for _, e := range supportedEdges {
		src, srcOK := endpointLabels(e.Source(), nodeTypes)
		tgt, tgtOK := endpointLabels(e.Target(), nodeTypes)
		if !srcOK || !tgtOK {
			deferredEdges = append(deferredEdges, e)
			continue
		}
		if err := closeEdge(e, src, tgt, s, edgeTypes, edgeKeys, edgeCands); err != nil {
			return nil, branchState{}, nil, err
		}
	}

	// Phase B: unlabelled-node inference over R3-admitted touching edges.
	if err := inferUnlabelled(pendingNodes, supportedEdges, s, nodeTypes); err != nil {
		return nil, branchState{}, nil, err
	}

	// Phase C: close deferred edges against the now-complete node table.
	for _, e := range deferredEdges {
		src, srcOK := endpointLabels(e.Source(), nodeTypes)
		tgt, tgtOK := endpointLabels(e.Target(), nodeTypes)
		if !srcOK {
			return nil, branchState{}, nil, fmt.Errorf("%w: cannot infer type of source endpoint of edge %q", ErrUnknownLabel, e.Variable())
		}
		if !tgtOK {
			return nil, branchState{}, nil, fmt.Errorf("%w: cannot infer type of target endpoint of edge %q", ErrUnknownLabel, e.Variable())
		}
		if err := closeEdge(e, src, tgt, s, edgeTypes, edgeKeys, edgeCands); err != nil {
			return nil, branchState{}, nil, err
		}
	}

	// Phase D (§4.6): seed with carry, override with local, then demote.
	nullableBinding := make(map[string]bool)
	for name, nb := range carry.exportedNullableBinding {
		nullableBinding[name] = nb
	}
	// Local Bindings override the carry with the local Nullable() bit before
	// demotion runs. This is what makes a Part K+1 that re-MATCHes an
	// OPTIONAL-carried `b` see nullableBinding["b"] = false.
	seedLocalNullability(part.Bindings, nullableBinding)
	demoteNullableInPlace(part.Bindings, nullableBinding)

	// Ordered in-scope name list — used by ReturnsAll expansion (§4.4.1).
	scopeOrder := buildScopeOrder(part.Bindings, carry.exportedOrder, nodeTypes, edgeBindings)

	// Materialise the Part's ReturnItems: either the parser's Returns verbatim,
	// or the virtual items §4.4.2 constructs for RETURN * / WITH *.
	items, err := materialiseReturns(part, scopeOrder, carry, nodeTypes, edgeBindings, edgeTypes, edgeCands)
	if err != nil {
		return nil, branchState{}, nil, err
	}

	// Projection walk — each item to a Column. GroupingKey stays false here;
	// resolveBranch fills it on the final Part only.
	columns := make([]Column, 0, len(items))
	for _, item := range items {
		colType, err := projectionType(item.Value, nodeTypes, edgeTypes, edgeKeys, edgeCands, edgeBindings, nullableBinding, carry.exportedResolvedTypes, s)
		if err != nil {
			return nil, branchState{}, nil, err
		}
		columns = append(columns, Column{Name: item.Name, Type: colType})
	}

	// Emit this Part's scope snapshot as one parameterUseSite. The top-level
	// unifier walks every parameter's Uses against every scope; a PropertyUse
	// witnesses at the scope whose tables contain its Ref's binding (§4.2.4).
	site := parameterUseSite{scope: snapshotScope(nodeTypes, edgeTypes, edgeCands, edgeBindings, nullableBinding)}

	// Build the exported branchState for Part K+1.
	exported := exportScope(part, columns, items, scopeOrder, nodeTypes, edgeTypes, edgeKeys, edgeCands, edgeBindings, nullableBinding, carry)

	return columns, exported, []parameterUseSite{site}, nil
}

// materialiseReturns handles the RETURN * / WITH * expansion (§4.4). When
// ReturnsAll is false, returns part.Returns unchanged. When true, builds the
// virtual ReturnItem sequence over scopeOrder (§4.4.2) — one item per in-scope
// name in own-Part-first, shadowing-dedup order.
func materialiseReturns(part query.Part, scopeOrder []string, carry branchState, nodeTypes map[string]schema.NodeType, edgeBindings map[string]query.EdgeBinding, edgeTypes map[string]schema.EdgeType, edgeCands map[string][]schema.EdgeKey) ([]query.ReturnItem, error) {
	if !part.ReturnsAll {
		return part.Returns, nil
	}
	// Empty in-scope set → empty column list (§4.4.3). Legal shape.
	if len(scopeOrder) == 0 {
		return nil, nil
	}
	items := make([]query.ReturnItem, 0, len(scopeOrder))
	for _, v := range scopeOrder {
		val, err := virtualProjection(v, nodeTypes, edgeBindings, edgeTypes, edgeCands, carry)
		if err != nil {
			return nil, err
		}
		items = append(items, query.ReturnItem{Name: v, Value: val})
	}
	return items, nil
}

// virtualProjection constructs the RefProjection (or carried-alias Value)
// §4.4.2 assigns to a wildcard-expanded name.
func virtualProjection(name string, nodeTypes map[string]schema.NodeType, edgeBindings map[string]query.EdgeBinding, edgeTypes map[string]schema.EdgeType, edgeCands map[string][]schema.EdgeKey, carry branchState) (query.Projection, error) {
	if _, ok := nodeTypes[name]; ok {
		return query.NewRefProjection(query.Ref{Variable: name}, query.TypeNode{}), nil
	}
	if b, ok := edgeBindings[name]; ok {
		if b.Hops() != nil {
			return query.NewRefProjection(query.Ref{Variable: name}, query.TypeList{}), nil
		}
		return query.NewRefProjection(query.Ref{Variable: name}, query.TypeEdge{}), nil
	}
	// Not a binding — must be a projection-alias carried through WITH; the
	// §4.5.4 bypass path serves it. Use a placeholder RefProjection whose
	// Value.Type() the walker will consult via the carried-resolved-types map.
	if _, ok := carry.exportedResolvedTypes[name]; ok {
		return query.NewRefProjection(query.Ref{Variable: name}, query.TypeUnknown{}), nil
	}
	// A name in scopeOrder that resolves to nothing is a resolver-side bug —
	// the scope builder must not put such names in the list.
	return nil, fmt.Errorf("%w: wildcard-expanded name %q resolves to no binding or carry", ErrOutOfR0Scope, name)
}

// buildScopeOrder computes the deterministic order for RETURN * / WITH *
// expansion (§4.4.1): local Part.Bindings in first-appearance order (named
// only), then carried names not covered by local, in carry-order. Also serves
// as the deterministic export order for a non-ReturnsAll WITH.
func buildScopeOrder(bindings []query.Binding, carryOrder []string, nodeTypes map[string]schema.NodeType, edgeBindings map[string]query.EdgeBinding) []string {
	seen := make(map[string]bool)
	out := make([]string, 0, len(bindings)+len(carryOrder))
	for _, b := range bindings {
		v, ok := bindingVariable(b)
		if !ok || v == "" || seen[v] {
			continue
		}
		// Only include names that actually resolved (Phase A/B/C committed).
		// Unresolved names are impossible at this point — Phase C either
		// resolved or short-circuited — but the guard keeps the invariant
		// tight.
		if _, isNode := nodeTypes[v]; isNode {
			seen[v] = true
			out = append(out, v)
			continue
		}
		if _, isEdge := edgeBindings[v]; isEdge {
			seen[v] = true
			out = append(out, v)
		}
	}
	for _, v := range carryOrder {
		if seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

// exportScope builds the branchState Part K passes to Part K+1 (§4.2.2). For an
// explicit WITH (ReturnsAll == false), the exported set is exactly the Part's
// Returns items keyed by Name. For WITH * (ReturnsAll == true), the exported
// set is the full in-scope binding set at the moment WITH ran, in scopeOrder.
// For a final Part (RETURN), the returned branchState is irrelevant (no next
// Part reads it) but we still build it for symmetry.
func exportScope(part query.Part, columns []Column, items []query.ReturnItem, scopeOrder []string, nodeTypes map[string]schema.NodeType, edgeTypes map[string]schema.EdgeType, edgeKeys map[string]schema.EdgeKey, edgeCands map[string][]schema.EdgeKey, edgeBindings map[string]query.EdgeBinding, nullableBinding map[string]bool, carry branchState) branchState {
	out := branchState{
		exportedNodeTypes:       make(map[string]schema.NodeType),
		exportedEdgeTypes:       make(map[string]schema.EdgeType),
		exportedEdgeKeys:        make(map[string]schema.EdgeKey),
		exportedEdgeCands:       make(map[string][]schema.EdgeKey),
		exportedEdgeBindings:    make(map[string]query.EdgeBinding),
		exportedNullableBinding: make(map[string]bool),
		exportedResolvedTypes:   make(map[string]ResolvedType),
	}

	// Names that leave via WITH — for WITH * that's every scopeOrder name;
	// for an explicit WITH item that's item.Name (which for a bare `WITH v`
	// equals v, and for `WITH e.p AS x` equals `x`, not `v`).
	var exportedNames []string
	if part.ReturnsAll {
		exportedNames = scopeOrder
		for i, item := range items {
			// items[i].Name == scopeOrder[i] for the wildcard-expanded case.
			// carried-type entries pass through unchanged; binding-derived
			// entries populate the binding maps below.
			out.exportedResolvedTypes[item.Name] = columns[i].Type
		}
	} else {
		exportedNames = make([]string, 0, len(part.Returns))
		for i, item := range part.Returns {
			exportedNames = append(exportedNames, item.Name)
			out.exportedResolvedTypes[item.Name] = columns[i].Type
		}
	}
	out.exportedOrder = exportedNames

	// Populate the binding maps for exports whose Name corresponds to an
	// in-scope binding-name (bare RefProjection{Ref{v, ""}}). An aliased
	// export like `WITH e.p AS x` puts `x` only in exportedResolvedTypes, not
	// in any binding map — downstream refs to `x` bypass via §4.5.4.
	for _, item := range choose(part.Returns, items, part.ReturnsAll) {
		alias := item.Name
		rp, ok := item.Value.(query.RefProjection)
		if !ok {
			continue
		}
		ref := rp.Ref()
		// Only export a binding entry when the alias matches the bare
		// binding-name reference (Ref{Variable: v, Property: ""} named by
		// its own variable). Anything else — property projection, renamed
		// alias — lives only in exportedResolvedTypes.
		if ref.Property != "" || alias != ref.Variable {
			continue
		}
		v := ref.Variable
		if nt, ok := nodeTypes[v]; ok {
			out.exportedNodeTypes[v] = nt
		}
		if et, ok := edgeTypes[v]; ok {
			out.exportedEdgeTypes[v] = et
			if k, ok := edgeKeys[v]; ok {
				out.exportedEdgeKeys[v] = k
			}
		}
		if cands, ok := edgeCands[v]; ok {
			out.exportedEdgeCands[v] = cands
		}
		if b, ok := edgeBindings[v]; ok {
			out.exportedEdgeBindings[v] = b
		}
		if nb, ok := nullableBinding[v]; ok {
			out.exportedNullableBinding[v] = nb
		}
	}
	_ = carry
	return out
}

// choose returns items when returnsAll is true and returns otherwise; used to
// give exportScope one unified iteration for both wildcard and explicit WITH.
func choose(returns []query.ReturnItem, items []query.ReturnItem, returnsAll bool) []query.ReturnItem {
	if returnsAll {
		return items
	}
	return returns
}

// fillGroupingKeys populates Column.GroupingKey for branch 0's final Part per
// §4.5.2. hasAggregate gate: at least one AggregateProjection in Returns.
// Uniform-exclude posture: ExprProjection is NEVER a grouping key.
func fillGroupingKeys(cols []Column, part query.Part) {
	// A ReturnsAll-expanded Part's Returns is empty (parser guarantees
	// mutual exclusion); expanded items are RefProjection over bindings,
	// which are grouping-key candidates. Since AggregateProjection cannot
	// appear inside a bare-name RefProjection, a ReturnsAll Part cannot fire
	// the hasAggregate gate — nothing to do.
	if part.ReturnsAll {
		return
	}
	hasAggregate := false
	for _, item := range part.Returns {
		if _, ok := item.Value.(query.AggregateProjection); ok {
			hasAggregate = true
			break
		}
	}
	if !hasAggregate {
		return
	}
	// Grouping applies. Non-aggregate, non-ExprProjection items are keys.
	for i, item := range part.Returns {
		switch item.Value.(type) {
		case query.RefProjection, query.LiteralProjection, query.FuncProjection:
			cols[i].GroupingKey = true
		}
		// AggregateProjection and ExprProjection remain false (§4.5.2
		// uniform-exclude).
	}
}

// compareBranchColumns runs the R5 UNION column compatibility check (§4.3).
// Every branch's column list must equal branch 0's index-wise on count, name,
// and type (strict Go-value equality; no lattice widening across branches).
func compareBranchColumns(branchCols [][]Column) error {
	if len(branchCols) < 2 {
		return nil
	}
	base := branchCols[0]
	for b := 1; b < len(branchCols); b++ {
		other := branchCols[b]
		if len(other) != len(base) {
			return fmt.Errorf("%w: branch %d has %d columns; branch 0 has %d", ErrUnionColumnMismatch, b, len(other), len(base))
		}
		for i := range base {
			if other[i].Name != base[i].Name {
				return fmt.Errorf("%w: branch %d column %d named %q; branch 0 column %d named %q", ErrUnionColumnMismatch, b, i, other[i].Name, i, base[i].Name)
			}
			if !resolvedTypeEqual(other[i].Type, base[i].Type) {
				return fmt.Errorf("%w: branch %d column %q has type %s; branch 0 has type %s", ErrUnionColumnMismatch, b, other[i].Name, other[i].Type.String(), base[i].Type.String())
			}
		}
	}
	return nil
}

// resolvedTypeEqual is Go-value equality for ResolvedType. Rendering to their
// stable MarshalJSON output would work too, but a variant-by-variant check is
// direct and avoids the allocation.
func resolvedTypeEqual(a, b ResolvedType) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	switch aa := a.(type) {
	case ResolvedNode:
		bb, ok := b.(ResolvedNode)
		return ok && aa == bb
	case ResolvedProperty:
		bb, ok := b.(ResolvedProperty)
		return ok && aa == bb
	case ResolvedEdge:
		bb, ok := b.(ResolvedEdge)
		return ok && aa == bb
	case ResolvedEdgeUnion:
		bb, ok := b.(ResolvedEdgeUnion)
		if !ok || aa.Nullable != bb.Nullable || len(aa.EdgeKeys) != len(bb.EdgeKeys) {
			return false
		}
		for i := range aa.EdgeKeys {
			if aa.EdgeKeys[i] != bb.EdgeKeys[i] {
				return false
			}
		}
		return true
	case ResolvedScalar:
		bb, ok := b.(ResolvedScalar)
		return ok && aa == bb
	case ResolvedTemporal:
		bb, ok := b.(ResolvedTemporal)
		return ok && aa == bb
	case ResolvedList:
		bb, ok := b.(ResolvedList)
		return ok && resolvedTypeEqual(aa.Element, bb.Element)
	case ResolvedUnknown:
		_, ok := b.(ResolvedUnknown)
		return ok
	default:
		return false
	}
}

// computeDistinct folds Part.Distinct across every branch × Part with the
// UnionKind ∈ Combinators fold (§3.2 / §4.7).
func computeDistinct(q query.Query) bool {
	for _, branch := range q.Branches {
		for _, part := range branch.Parts {
			if part.Distinct {
				return true
			}
		}
	}
	for _, c := range q.Combinators {
		if c == query.UnionDistinct {
			return true
		}
	}
	return false
}

// unifyParameterUsesAcrossScopes walks each parameter's Uses against every
// Part scope. A PropertyUse witnesses at the Part whose scope contains the
// Ref's binding — under §4.2.4 that's the Part where the parser attributed
// the Use, though the wire doesn't carry Part attribution. A ClauseSlotUse
// and ExprUse are Part-agnostic and witness once. Witnesses are unified via
// R2's lattice; the first conflict fires ErrParameterTypeConflict.
func unifyParameterUsesAcrossScopes(params []query.Parameter, scopes []partScope, s schema.Schema) ([]ResolvedParameter, error) {
	if len(params) == 0 {
		return []ResolvedParameter{}, nil
	}
	out := make([]ResolvedParameter, 0, len(params))
	for _, p := range params {
		var unified ResolvedType
		seen := false
		for _, u := range p.Uses {
			ws, err := witnessAcrossScopes(u, scopes, s)
			if err != nil {
				return nil, err
			}
			for _, w := range ws {
				if !seen {
					unified = w
					seen = true
					continue
				}
				merged, ok := unify(unified, w)
				if !ok {
					return nil, fmt.Errorf("%w: parameter %q: %s vs %s", ErrParameterTypeConflict, p.Name, unified.String(), w.String())
				}
				unified = merged
			}
		}
		if !seen {
			// No Part attributed the Use — falls to ResolvedUnknown; consumers
			// that hit an out-of-scope Use would have short-circuited by now
			// via ErrOutOfR0Scope in the projection walk, so this arm is
			// the honest bottom.
			unified = ResolvedUnknown{}
		}
		out = append(out, ResolvedParameter{Name: p.Name, Type: unified})
	}
	return out, nil
}

// witnessAcrossScopes produces one witness per Part whose scope contains the
// Use's Ref (for a PropertyUse), or exactly one witness for a Part-agnostic
// Use (ClauseSlot / ExprUse). An unattributed PropertyUse (no scope contains
// its Ref) returns zero witnesses — the unifier treats this as ResolvedUnknown.
func witnessAcrossScopes(u query.Use, scopes []partScope, s schema.Schema) ([]ResolvedType, error) {
	switch uu := u.(type) {
	case query.PropertyUse:
		ref := uu.Ref()
		out := make([]ResolvedType, 0, 1)
		for _, sc := range scopes {
			if !scopeContains(sc, ref.Variable) {
				continue
			}
			w, err := propertyUseWitness(ref, sc.nodeTypes, sc.edgeTypes, sc.edgeCands, sc.edgeBindings, sc.nullableBinding, s)
			if err != nil {
				return nil, err
			}
			out = append(out, w)
		}
		return out, nil
	case query.ClauseSlotUse:
		return []ResolvedType{ResolvedScalar{Kind: ScalarInt}}, nil
	case query.ExprUse:
		switch uu.Position() {
		case query.ExprInProjection, query.ExprInPredicate:
			w, err := resolveType(uu.EnclosingType())
			if err != nil {
				return nil, err
			}
			return []ResolvedType{w}, nil
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

func scopeContains(sc partScope, v string) bool {
	if _, ok := sc.nodeTypes[v]; ok {
		return true
	}
	if _, ok := sc.edgeTypes[v]; ok {
		return true
	}
	if _, ok := sc.edgeCands[v]; ok {
		return true
	}
	return false
}

// snapshotScope captures the tables in effect at one Part for the top-level
// parameter walker. Called at the end of resolvePart against the local (post-
// carry-seed, post-shadow, post-demote) tables so the snapshot represents the
// exact tables the parser attributed Uses against.
func snapshotScope(nodeTypes map[string]schema.NodeType, edgeTypes map[string]schema.EdgeType, edgeCands map[string][]schema.EdgeKey, edgeBindings map[string]query.EdgeBinding, nullableBinding map[string]bool) partScope {
	sc := partScope{
		nodeTypes:       make(map[string]schema.NodeType, len(nodeTypes)),
		edgeTypes:       make(map[string]schema.EdgeType, len(edgeTypes)),
		edgeCands:       make(map[string][]schema.EdgeKey, len(edgeCands)),
		edgeBindings:    make(map[string]query.EdgeBinding, len(edgeBindings)),
		nullableBinding: make(map[string]bool, len(nullableBinding)),
	}
	for k, v := range nodeTypes {
		sc.nodeTypes[k] = v
	}
	for k, v := range edgeTypes {
		sc.edgeTypes[k] = v
	}
	for k, v := range edgeCands {
		sc.edgeCands[k] = v
	}
	for k, v := range edgeBindings {
		sc.edgeBindings[k] = v
	}
	for k, v := range nullableBinding {
		sc.nullableBinding[k] = v
	}
	return sc
}

// r3EdgeAdmissible screens an EdgeBinding against R3's edge shape predicate:
// labelled (at least one type). Every R3 shape — directed or undirected,
// single-hop or var-length, single-type or multi-type — is admitted; untyped
// edges route to ErrOutOfR0Scope (R-later takes them up).
func r3EdgeAdmissible(e query.EdgeBinding) error {
	if len(e.Labels()) == 0 {
		return fmt.Errorf("%w: untyped edge", ErrOutOfR0Scope)
	}
	return nil
}

// edgeCandidates enumerates the closed candidate set for one edge binding
// whose endpoint keys are already committed.
func edgeCandidates(e query.EdgeBinding, src, tgt graph.LabelSetKey, s schema.Schema) []schema.EdgeKey {
	out := make([]schema.EdgeKey, 0, len(e.Labels()))
	for _, L := range e.Labels() {
		labelKey := graph.LabelSet{L}.Key()
		orientations := [][2]graph.LabelSetKey{{src, tgt}}
		if !e.Directed() {
			orientations = append(orientations, [2]graph.LabelSetKey{tgt, src})
		}
		for _, o := range orientations {
			k := schema.EdgeKey{Source: o[0], Label: labelKey, Target: o[1]}
			if _, ok := s.Edges[k]; ok {
				out = append(out, k)
			}
		}
	}
	return out
}

func formatEdgeKey(k schema.EdgeKey) string {
	return fmt.Sprintf("%s-[%s]->%s", k.Source, k.Label, k.Target)
}

func formatEdgeKeys(keys []schema.EdgeKey) string {
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = formatEdgeKey(k)
	}
	return strings.Join(parts, ", ")
}

// endpointLabels reads the labels an edge endpoint carries at the point
// EdgeKey formation needs them.
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
		return "", false
	}
}

// closeEdge applies edge-candidate closure to one already-endpoint-resolved
// edge and records the resolved shape.
func closeEdge(e query.EdgeBinding, src, tgt graph.LabelSetKey, s schema.Schema, edgeTypes map[string]schema.EdgeType, edgeKeys map[string]schema.EdgeKey, edgeCands map[string][]schema.EdgeKey) error {
	cands := edgeCandidates(e, src, tgt, s)
	v := e.Variable()

	switch len(cands) {
	case 0:
		return fmt.Errorf("%w: %s", ErrUnknownEdge, describeTriedEdges(e, src, tgt))
	case 1:
		key := cands[0]
		et := s.Edges[key]
		if v != "" {
			edgeTypes[v] = et
			edgeKeys[v] = key
		}
		return nil
	default:
		if !e.Directed() && len(e.Labels()) == 1 {
			return fmt.Errorf("%w: edge %q matches both %s", ErrAmbiguousEdgeOrientation, v, formatEdgeKeys(cands))
		}
		if v != "" {
			edgeCands[v] = cands
		}
		return nil
	}
}

func describeTriedEdges(e query.EdgeBinding, src, tgt graph.LabelSetKey) string {
	parts := make([]string, 0, len(e.Labels())*2)
	for _, L := range e.Labels() {
		labelKey := graph.LabelSet{L}.Key()
		parts = append(parts, formatEdgeKey(schema.EdgeKey{Source: src, Label: labelKey, Target: tgt}))
		if !e.Directed() {
			parts = append(parts, formatEdgeKey(schema.EdgeKey{Source: tgt, Label: labelKey, Target: src}))
		}
	}
	return strings.Join(parts, ", ")
}

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
			n := next[0]
			cands := candidateTypes(n, edges, s, resolved)
			return fmt.Errorf("%w: cannot uniquely infer type of unlabelled binding %q — candidate types: %s", ErrAmbiguousBinding, n.Variable(), joinCandidates(cands))
		}
		pending = next
	}
	return nil
}

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
		cand := make(map[graph.LabelSetKey]struct{})
		orientations := []bool{true}
		if !e.Directed() {
			orientations = []bool{true, false}
		}
		for _, L := range e.Labels() {
			labelKey := graph.LabelSet{L}.Key()
			for _, forward := range orientations {
				for k := range s.Edges {
					if k.Label != labelKey {
						continue
					}
					nAtSource := (side == "source") == forward
					if nAtSource && k.Target == otherKey {
						cand[k.Source] = struct{}{}
					}
					if !nAtSource && k.Source == otherKey {
						cand[k.Target] = struct{}{}
					}
				}
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

func touchingSide(e query.EdgeBinding, v string) (string, bool) {
	if src, ok := e.Source().(query.VarEndpoint); ok && src.Variable() == v {
		return "source", true
	}
	if tgt, ok := e.Target().(query.VarEndpoint); ok && tgt.Variable() == v {
		return "target", true
	}
	return "", false
}

func intersect(a, b map[graph.LabelSetKey]struct{}) map[graph.LabelSetKey]struct{} {
	out := make(map[graph.LabelSetKey]struct{})
	for k := range a {
		if _, ok := b[k]; ok {
			out[k] = struct{}{}
		}
	}
	return out
}

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
// column's resolved type. R5 admits AggregateProjection (§4.5) and threads a
// carriedResolvedTypes map so the §4.5.4 RefProjection bypass path can serve
// carried-alias refs.
func projectionType(p query.Projection, nodeTypes map[string]schema.NodeType, edgeTypes map[string]schema.EdgeType, edgeKeys map[string]schema.EdgeKey, edgeCands map[string][]schema.EdgeKey, edgeBindings map[string]query.EdgeBinding, nullableBinding map[string]bool, carriedResolvedTypes map[string]ResolvedType, s schema.Schema) (ResolvedType, error) {
	switch pp := p.(type) {
	case query.RefProjection:
		return refProjectionType(pp.Ref(), nodeTypes, edgeTypes, edgeKeys, edgeCands, edgeBindings, nullableBinding, carriedResolvedTypes, s)
	case query.LiteralProjection:
		return resolveType(pp.Type())
	case query.FuncProjection:
		return resolveType(pp.Type())
	case query.ExprProjection:
		return resolveType(pp.Type())
	case query.AggregateProjection:
		return resolveType(pp.Type())
	default:
		return nil, fmt.Errorf("%w: unknown projection variant (%T)", ErrOutOfR0Scope, p)
	}
}

// refProjectionType dispatches a RefProjection's Ref against the resolved
// node and edge binding tables. §4.5.4 adds the carried-alias bypass — when a
// name lives ONLY in carriedResolvedTypes (e.g. `WITH count(n) AS c` seen
// downstream), refProjectionType returns the carried type directly.
func refProjectionType(ref query.Ref, nodeTypes map[string]schema.NodeType, edgeTypes map[string]schema.EdgeType, edgeKeys map[string]schema.EdgeKey, edgeCands map[string][]schema.EdgeKey, edgeBindings map[string]query.EdgeBinding, nullableBinding map[string]bool, carriedResolvedTypes map[string]ResolvedType, s schema.Schema) (ResolvedType, error) {
	if nt, ok := nodeTypes[ref.Variable]; ok {
		if ref.Property == "" {
			return ResolvedNode{Labels: nt.Labels, Nullable: nullableBinding[ref.Variable]}, nil
		}
		prop, ok := nt.Properties[ref.Property]
		if !ok {
			return nil, fmt.Errorf("%w: %s.%s", ErrUnknownProperty, ref.Variable, ref.Property)
		}
		return ResolvedProperty{Type: prop.Type, Nullable: prop.Nullable || nullableBinding[ref.Variable]}, nil
	}
	_, singleCand := edgeTypes[ref.Variable]
	cands, multiCand := edgeCands[ref.Variable]
	if !singleCand && !multiCand {
		// §4.5.4 — carried-alias bypass. A RefProjection whose Variable lives
		// only in carriedResolvedTypes yields the carried type verbatim
		// (property lookups on a carried alias are unreachable — parser scope
		// check rejects Ref{"c", "p"} unless c is a binding-name in scope).
		if rt, ok := carriedResolvedTypes[ref.Variable]; ok && ref.Property == "" {
			return rt, nil
		}
		return nil, fmt.Errorf("%w: %s", ErrOutOfR0Scope, ref.Variable)
	}

	binding := edgeBindings[ref.Variable]
	varLength := binding.Hops() != nil
	edgeNullable := nullableBinding[ref.Variable]

	if ref.Property == "" {
		var element ResolvedType
		if singleCand {
			element = ResolvedEdge{EdgeKey: edgeKeys[ref.Variable], Nullable: edgeNullable}
		} else {
			element = ResolvedEdgeUnion{EdgeKeys: cands, Nullable: edgeNullable}
		}
		if varLength {
			return ResolvedList{Element: element}, nil
		}
		return element, nil
	}

	if varLength {
		return nil, fmt.Errorf("%w: property projection on variable-length edge binding: reach list elements via list-element access (UNWIND in R5 or later)", ErrOutOfR0Scope)
	}
	if singleCand {
		et := edgeTypes[ref.Variable]
		prop, ok := et.Properties[ref.Property]
		if !ok {
			return nil, fmt.Errorf("%w: %s.%s", ErrUnknownProperty, ref.Variable, ref.Property)
		}
		return ResolvedProperty{Type: prop.Type, Nullable: prop.Nullable || edgeNullable}, nil
	}
	return unionProperty(cands, s, ref.Variable, ref.Property, edgeNullable)
}

func unionProperty(cands []schema.EdgeKey, s schema.Schema, refVar, refProp string, bindingNullable bool) (ResolvedType, error) {
	var first ResolvedProperty
	for i, k := range cands {
		et := s.Edges[k]
		prop, ok := et.Properties[refProp]
		if !ok {
			return nil, fmt.Errorf("%w: property %s.%s missing on union member %s", ErrUnknownProperty, refVar, refProp, formatEdgeKey(k))
		}
		hit := ResolvedProperty{Type: prop.Type, Nullable: prop.Nullable}
		if i == 0 {
			first = hit
			continue
		}
		if hit.Type != first.Type || hit.Nullable != first.Nullable {
			return nil, fmt.Errorf("%w: property %s.%s type differs across union members: %s vs %s", ErrUnknownProperty, refVar, refProp, first.String(), hit.String())
		}
	}
	first.Nullable = first.Nullable || bindingNullable
	return first, nil
}

// resolveType maps a parser Type into its ResolvedType. R5 is unchanged from
// R4 in mechanic — the AggregateProjection.Type() dispatch (per §4.5.1) rides
// this table for its result-type emission.
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
		panic("resolver bug: resolveType reached TypePath (R5 does not admit path bindings)")
	default:
		panic(fmt.Sprintf("resolver bug: resolveType reached unhandled query.Type %T", t))
	}
}

func propertyUseWitness(ref query.Ref, nodeTypes map[string]schema.NodeType, edgeTypes map[string]schema.EdgeType, edgeCands map[string][]schema.EdgeKey, edgeBindings map[string]query.EdgeBinding, nullableBinding map[string]bool, s schema.Schema) (ResolvedType, error) {
	if nt, ok := nodeTypes[ref.Variable]; ok {
		prop, ok := nt.Properties[ref.Property]
		if !ok {
			return nil, fmt.Errorf("%w: %s.%s", ErrUnknownProperty, ref.Variable, ref.Property)
		}
		return ResolvedProperty{Type: prop.Type, Nullable: prop.Nullable || nullableBinding[ref.Variable]}, nil
	}
	_, singleCand := edgeTypes[ref.Variable]
	cands, multiCand := edgeCands[ref.Variable]
	if !singleCand && !multiCand {
		return nil, fmt.Errorf("%w: %s", ErrOutOfR0Scope, ref.Variable)
	}
	if binding := edgeBindings[ref.Variable]; binding.Hops() != nil {
		return nil, fmt.Errorf("%w: property projection on variable-length edge binding: reach list elements via list-element access (UNWIND in R5 or later)", ErrOutOfR0Scope)
	}
	edgeNullable := nullableBinding[ref.Variable]
	if singleCand {
		et := edgeTypes[ref.Variable]
		prop, ok := et.Properties[ref.Property]
		if !ok {
			return nil, fmt.Errorf("%w: %s.%s", ErrUnknownProperty, ref.Variable, ref.Property)
		}
		return ResolvedProperty{Type: prop.Type, Nullable: prop.Nullable || edgeNullable}, nil
	}
	return unionProperty(cands, s, ref.Variable, ref.Property, edgeNullable)
}

// seedLocalNullability writes bindings' own Nullable() bit into the table,
// overwriting any carry entry (§4.6 "local overrides carry"). Anonymous
// bindings (v == "") skip.
func seedLocalNullability(bindings []query.Binding, table map[string]bool) {
	for _, b := range bindings {
		v, ok := bindingVariable(b)
		if !ok || v == "" {
			continue
		}
		table[v] = b.Nullable()
	}
}

// demoteNullableInPlace runs R4's regime-(a) demotion on part.Bindings against
// a pre-seeded table. Same semantics as R4's demoteNullable, but the table is
// supplied by the caller so §4.6's carry-seed → local-override → demote order
// is applied to the same map.
func demoteNullableInPlace(bindings []query.Binding, table map[string]bool) {
	for _, b := range bindings {
		e, ok := b.(query.EdgeBinding)
		if !ok {
			continue
		}
		if e.Nullable() || !qualifiedDemoter(e) {
			continue
		}
		for _, side := range [2]query.Endpoint{e.Source(), e.Target()} {
			ve, ok := side.(query.VarEndpoint)
			if !ok {
				continue
			}
			v := ve.Variable()
			if v == "" {
				continue
			}
			if _, present := table[v]; present {
				table[v] = false
			}
		}
	}
}

func bindingVariable(b query.Binding) (string, bool) {
	switch bb := b.(type) {
	case query.NodeBinding:
		return bb.Variable(), true
	case query.EdgeBinding:
		return bb.Variable(), true
	default:
		return "", false
	}
}

func qualifiedDemoter(e query.EdgeBinding) bool {
	h := e.Hops()
	if h == nil {
		return true
	}
	lower := h.Min()
	if lower == nil {
		return true
	}
	return *lower >= 1
}

// unify agrees two ResolvedTypes iff they are structurally equal or one side
// is ResolvedUnknown. Returns the agreed type and true on success, (nil, false)
// on conflict.
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
