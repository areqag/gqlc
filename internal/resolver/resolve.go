package resolver

import (
	"errors"
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
// against every other branch (§4.3), witnesses each parameter Use against the
// exact lexical Part's scope in its lexical branch (fvo per ADR 0008 amendment
// 2026-07-06; the pre-fvo any-valid-witness rule at §4.2.4 retires), and folds
// Part.Distinct + UnionKind into ValidatedQuery.Distinct (§3.2/§4.7).
func resolve(q query.Query, s schema.Schema, r procsig.Registry) (ValidatedQuery, error) {
	if len(q.Branches) == 0 {
		// Defensive tripwire; the parser's buildBranch guarantees >= 1
		// (Query is a builder-maintained product type). Unreachable via parse.
		return ValidatedQuery{}, fmt.Errorf("%w: empty branches", ErrOutOfR0Scope)
	}

	branchCols := make([][]Column, len(q.Branches))
	branchScopeTables := make([][]partScope, len(q.Branches))

	for b, branch := range q.Branches {
		cols, uses, err := resolveBranch(branch, s, r)
		if err != nil {
			return ValidatedQuery{}, err
		}
		branchCols[b] = cols
		branchScopeTables[b] = useSitesToScopes(uses)
	}

	if err := compareBranchColumns(branchCols); err != nil {
		return ValidatedQuery{}, err
	}

	params, err := unifyParameterUsesAcrossBranches(q.Parameters, branchScopeTables, s)
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
//
// exportedOptionalGroup carries the OPTIONAL-group id of a WITH-carried
// binding across the Part boundary — gqlc-984, closing the residual §2.5 note
// of model-change-ay9-optional-group.md. Group ids are per-query and unique
// across the whole parse (§3.3), so a carried id cannot collide with a local
// id in the downstream Part. Demotion of a member proven in Part K+1 pulls
// the whole carried group via the ay9 fixed-point in demoteNullableInPlace.
type branchState struct {
	exportedNodeTypes       map[string]schema.NodeType
	exportedEdgeTypes       map[string]schema.EdgeType
	exportedEdgeKeys        map[string]schema.EdgeKey
	exportedEdgeCands       map[string][]schema.EdgeKey
	exportedEdgeBindings    map[string]query.EdgeBinding
	exportedNullableBinding map[string]bool
	exportedOptionalGroup   map[string]int
	exportedResolvedTypes   map[string]ResolvedType
	exportedCallTypes       map[string]callBindingSlot
	exportedOrder           []string
}

// callBindingSlot carries the resolver-side view of a CallBinding at a Part's
// Phase A1: bridged Stage-6 type, nullability, and the two identity strings
// codegen may consult on the query.Query side (spec §4.1).
type callBindingSlot struct {
	resultType  query.Type
	nullable    bool
	procedure   string
	sourceField string
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
		cols, exported, uses, err := resolvePart(part, carry, s, r)
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
func resolvePart(part query.Part, carry branchState, s schema.Schema, r procsig.Registry) ([]Column, branchState, []parameterUseSite, error) {
	nodeTypes := make(map[string]schema.NodeType)
	edgeTypes := make(map[string]schema.EdgeType)
	edgeKeys := make(map[string]schema.EdgeKey)
	edgeCands := make(map[string][]schema.EdgeKey)
	edgeBindings := make(map[string]query.EdgeBinding)
	callTypes := make(map[string]callBindingSlot)
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
	for name, slot := range carry.exportedCallTypes {
		callTypes[name] = slot
	}

	// Phase A1: local labelled node bindings (shadows carry) + edge admission
	// screening + CALL binding admission (R7 §4.1). Unlabelled node bindings
	// defer to Phase B (with a matching call-collision check at commit).
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
			// R7 §4.1.2.1: a carried CALL YIELD scalar cannot re-bind as a
			// labelled node — the shape-posture extension of R5's LabelSetKey
			// check. Fires BEFORE the R5 arm so the scalar-vs-entity fault is
			// named correctly, not masked by the node-vs-node message.
			if _, seenCall := callTypes[bb.Variable()]; seenCall {
				return nil, branchState{}, nil, fmt.Errorf("%w: variable %q carried as CALL YIELD scalar, re-bound as %s", ErrPartBindingTypeConflict, bb.Variable(), key)
			}
			// R5 §6.4: a labelled re-binding of a carried name whose schema-
			// typed identity differs from the carry is irreconcilable. Same
			// LabelSetKey = trivial re-binding, admit. Any pre-existing entry
			// here can only originate from the carry seed (§4.2.3): local
			// same-Part siblings with the same variable are merged into one
			// binding at parse time.
			if prev, seen := nodeTypes[bb.Variable()]; seen && prev.Labels != nt.Labels {
				return nil, branchState{}, nil, fmt.Errorf("%w: variable %q carried as %s, re-bound as %s", ErrPartBindingTypeConflict, bb.Variable(), prev.Labels, nt.Labels)
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
				// R7 §4.1.2.2: reciprocal call-vs-edge shape-mismatch guard.
				if _, seenCall := callTypes[v]; seenCall {
					return nil, branchState{}, nil, fmt.Errorf("%w: variable %q carried as CALL YIELD scalar, re-bound as edge with labels %s", ErrPartBindingTypeConflict, v, bb.Labels().Key())
				}
				// R5 §6.4 edge parity: if the carry seed already carried an
				// edge binding for `v`, and the local re-bind's label set
				// differs, that is a Part-cross irreconcilable re-typing.
				// Same label-set key = trivial re-bind, admit (openCypher
				// semantics for the analogous node case). Different key =
				// ErrPartBindingTypeConflict, same sentinel as the node arm.
				if prev, seen := edgeBindings[v]; seen && prev.Labels().Key() != bb.Labels().Key() {
					return nil, branchState{}, nil, fmt.Errorf("%w: variable %q carried as edge with labels %s, re-bound with labels %s", ErrPartBindingTypeConflict, v, prev.Labels().Key(), bb.Labels().Key())
				}
				edgeBindings[v] = bb
				// Edge shadows any carried node state.
				delete(nodeTypes, v)
				// Local edge re-bind resets any carried closed-edge state
				// for `v` — Phase A2/C's closeEdge is authoritative for the
				// new binding's source/target endpoints, which may differ
				// from the carried binding's even under a trivial re-bind.
				delete(edgeTypes, v)
				delete(edgeKeys, v)
				delete(edgeCands, v)
			}
		case query.CallBinding:
			v := bb.Variable()
			// R7 §4.1: local CallBinding shadows any carried entity state
			// at the same name (parser-unreachable belt-and-braces, since
			// build.go:148-150's imported[v] check rejects the collision
			// direction at parse). Same posture as R5's local-shadows-carry
			// rule for node/edge (§4.2.3 R5).
			delete(nodeTypes, v)
			delete(edgeTypes, v)
			delete(edgeKeys, v)
			delete(edgeCands, v)
			delete(edgeBindings, v)
			// Same-Part duplicate CallBinding variable is grammar-impossible
			// (parser Stage 14 §4.7 ErrVariableKindConflict — see
			// internal/query/cypher/build.go:127-153). Defensive tripwire.
			if _, seen := callTypes[v]; seen {
				return nil, branchState{}, nil, fmt.Errorf("%w: variable %q re-CALL-bound in single part", ErrPartBindingTypeConflict, v)
			}
			// 0ig: argument-site assignability. Each CallBinding minted
			// from the same CALL clause carries the SAME args slice by
			// parser construction (§4.3.1), so the check runs at most
			// once per CALL — subsequent bindings from the same clause
			// hit the same slice header and re-verify the same
			// assignments.
			//
			// Sentinel discipline (spec §8.1): ErrCallArgAssignability is
			// reserved for the per-position lattice check — the sole
			// resolver-reachable fail mode this axis introduces. The two
			// drift arms below (registry miss, arity mismatch) are
			// parser-authoritative pre-conditions (spec §4.4 trust
			// posture; ErrUnknownProcedure + ErrProcedureArity fire at
			// parse-time) and unreachable in-corpus; they surface as
			// plain non-sentinel errors so a drift bug is loud but does
			// not pollute the assignability sentinel's fixture semantics.
			// R7 §5.2 retired ErrOutOfR0Scope at the BindingCall fail-site
			// so it is not reused here either.
			if args := bb.Args(); len(args) > 0 {
				sig, ok := r.Lookup(bb.Procedure())
				if !ok {
					return nil, branchState{}, nil, fmt.Errorf("resolver: procedure %q missing from registry (parser drift)", bb.Procedure())
				}
				if len(args) != len(sig.Params) {
					return nil, branchState{}, nil, fmt.Errorf("resolver: procedure %q expects %d arguments, got %d (parser drift)", bb.Procedure(), len(sig.Params), len(args))
				}
				for i, a := range args {
					if !argAssignable(sig.Params[i].Token, a.Type()) {
						return nil, branchState{}, nil, fmt.Errorf("%w: procedure %q argument %d: cannot assign %s to %s", ErrCallArgAssignability, bb.Procedure(), i, a.Type().String(), sig.Params[i].Token)
					}
				}
			}
			callTypes[v] = callBindingSlot{
				resultType:  bb.ResultType(),
				nullable:    bb.Nullable(),
				procedure:   bb.Procedure(),
				sourceField: bb.SourceField(),
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
	// R7 §4.1.2.1 addendum: pass callTypes so an inferred unlabelled node
	// whose Variable collides with a carried CALL YIELD scalar fails at
	// commit with ErrPartBindingTypeConflict, mirroring the labelled arm.
	if err := inferUnlabelled(pendingNodes, supportedEdges, s, nodeTypes, callTypes); err != nil {
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
	demoteNullableInPlace(part.Bindings, nullableBinding, carry.exportedOptionalGroup)

	// Phase E (R6 §4.1): effect validation. Runs after Phase D so effect
	// targets see the same schema-committed binding tables and effective-
	// nullability map that the projection walk sees. First failure short-
	// circuits.
	if err := validateEffects(part.Effects, nodeTypes, edgeTypes, edgeCands, edgeBindings, carry.exportedResolvedTypes, s); err != nil {
		return nil, branchState{}, nil, err
	}

	// Ordered in-scope name list — used by ReturnsAll expansion (§4.4.1).
	// R7 §4.3: buildScopeOrder is widened to include CALL YIELD variables.
	scopeOrder := buildScopeOrder(part.Bindings, carry.exportedOrder, nodeTypes, edgeBindings, callTypes)

	// Materialise the Part's ReturnItems: either the parser's Returns verbatim,
	// or the virtual items §4.4.2 constructs for RETURN * / WITH *. R7's
	// virtualProjection widening synthesises CALL YIELD RefProjections with
	// the CallBinding's ResultType (§4.7).
	items, err := materialiseReturns(part, scopeOrder, carry, nodeTypes, edgeBindings, callTypes)
	if err != nil {
		return nil, branchState{}, nil, err
	}

	// Projection walk — each item to a Column. GroupingKey stays false here;
	// resolveBranch fills it on the final Part only.
	columns := make([]Column, 0, len(items))
	for _, item := range items {
		colType, err := projectionType(item.Value, nodeTypes, edgeTypes, edgeKeys, edgeCands, edgeBindings, nullableBinding, callTypes, carry.exportedResolvedTypes, s)
		if err != nil {
			return nil, branchState{}, nil, err
		}
		columns = append(columns, Column{Name: item.Name, Type: colType})
	}

	// Emit this Part's scope snapshot as one parameterUseSite. The top-level
	// unifier walks every parameter's Uses against every scope; a PropertyUse
	// witnesses at the scope whose tables contain its Ref's binding (§4.2.4).
	site := parameterUseSite{scope: snapshotScope(nodeTypes, edgeTypes, edgeCands, edgeBindings, nullableBinding)}

	// Build the exported branchState for Part K+1. R7 §4.6 adds the
	// exportedCallTypes lane for CALL YIELD carry-forward. gqlc-984 adds
	// exportedOptionalGroup: names surviving WITH keep their Part-K OPTIONAL
	// group id (locally minted, or inherited from carry), so downstream
	// Parts can close cross-Part group demotion via the ay9 fixed point.
	exported := exportScope(part, columns, items, scopeOrder, nodeTypes, edgeTypes, edgeKeys, edgeCands, edgeBindings, nullableBinding, callTypes, carry.exportedOptionalGroup)

	return columns, exported, []parameterUseSite{site}, nil
}

// materialiseReturns handles the RETURN * / WITH * expansion (§4.4). When
// ReturnsAll is false, returns part.Returns unchanged. When true, builds the
// virtual ReturnItem sequence over scopeOrder (§4.4.2) — one item per in-scope
// name in own-Part-first, shadowing-dedup order. R7 threads callTypes so
// CALL YIELD variables synthesise a properly-typed RefProjection (§4.7).
func materialiseReturns(part query.Part, scopeOrder []string, carry branchState, nodeTypes map[string]schema.NodeType, edgeBindings map[string]query.EdgeBinding, callTypes map[string]callBindingSlot) ([]query.ReturnItem, error) {
	if !part.ReturnsAll {
		return part.Returns, nil
	}
	// Empty in-scope set → empty column list (§4.4.3). Legal shape.
	if len(scopeOrder) == 0 {
		return nil, nil
	}
	items := make([]query.ReturnItem, 0, len(scopeOrder))
	for _, v := range scopeOrder {
		val, err := virtualProjection(v, nodeTypes, edgeBindings, carry, callTypes)
		if err != nil {
			return nil, err
		}
		items = append(items, query.ReturnItem{Name: v, Value: val})
	}
	return items, nil
}

// virtualProjection constructs the RefProjection (or carried-alias Value)
// §4.4.2 assigns to a wildcard-expanded name. R7 §4.7: the callTypes lane
// (appended at the tail) synthesises a CALL YIELD variable's RefProjection
// with the CallBinding's bridged ResultType.
func virtualProjection(name string, nodeTypes map[string]schema.NodeType, edgeBindings map[string]query.EdgeBinding, carry branchState, callTypes map[string]callBindingSlot) (query.Projection, error) {
	if _, ok := nodeTypes[name]; ok {
		return query.NewRefProjection(query.Ref{Variable: name}, query.TypeNode{}), nil
	}
	if b, ok := edgeBindings[name]; ok {
		if b.Hops() != nil {
			return query.NewRefProjection(query.Ref{Variable: name}, query.TypeList{}), nil
		}
		return query.NewRefProjection(query.Ref{Variable: name}, query.TypeEdge{}), nil
	}
	if slot, ok := callTypes[name]; ok {
		return query.NewRefProjection(query.Ref{Variable: name}, slot.resultType), nil
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
// as the deterministic export order for a non-ReturnsAll WITH. R7 §4.3
// widens the walk to include CALL YIELD variables so standalone-CALL Parts
// (parser Stage 14 §4.3 ReturnsAll=true) synthesise their column list.
func buildScopeOrder(bindings []query.Binding, carryOrder []string, nodeTypes map[string]schema.NodeType, edgeBindings map[string]query.EdgeBinding, callTypes map[string]callBindingSlot) []string {
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
			continue
		}
		if _, isCall := callTypes[v]; isCall {
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
// Part reads it) but we still build it for symmetry. R7 §4.6 adds the
// exportedCallTypes lane so CALL YIELD scalars survive a bare `WITH v`
// carry (aliased carry also lands in exportedResolvedTypes via the R5 path).
func exportScope(part query.Part, columns []Column, items []query.ReturnItem, scopeOrder []string, nodeTypes map[string]schema.NodeType, edgeTypes map[string]schema.EdgeType, edgeKeys map[string]schema.EdgeKey, edgeCands map[string][]schema.EdgeKey, edgeBindings map[string]query.EdgeBinding, nullableBinding map[string]bool, callTypes map[string]callBindingSlot, carriedGroups map[string]int) branchState {
	out := branchState{
		exportedNodeTypes:       make(map[string]schema.NodeType),
		exportedEdgeTypes:       make(map[string]schema.EdgeType),
		exportedEdgeKeys:        make(map[string]schema.EdgeKey),
		exportedEdgeCands:       make(map[string][]schema.EdgeKey),
		exportedEdgeBindings:    make(map[string]query.EdgeBinding),
		exportedNullableBinding: make(map[string]bool),
		exportedOptionalGroup:   make(map[string]int),
		exportedResolvedTypes:   make(map[string]ResolvedType),
		exportedCallTypes:       make(map[string]callBindingSlot),
	}
	// Build the local group-id lookup up front: a name gets its local
	// binding's OptionalGroup if declared this Part, otherwise its carried
	// group id from the incoming carry. Local shadows carry — a local
	// re-declaration with a distinct (possibly zero) group replaces the
	// carried id (gqlc-984). Only names surviving into exportedNames get
	// promoted to the outgoing carry below.
	localGroup := map[string]int{}
	for _, b := range part.Bindings {
		var v string
		var g int
		switch bb := b.(type) {
		case query.NodeBinding:
			v = bb.Variable()
			g = bb.OptionalGroup()
		case query.EdgeBinding:
			v = bb.Variable()
			g = bb.OptionalGroup()
		default:
			continue
		}
		if v == "" {
			continue
		}
		localGroup[v] = g
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
		if slot, ok := callTypes[v]; ok {
			out.exportedCallTypes[v] = slot
		}
		// Group id: local wins over carry. A local binding with
		// OptionalGroup == 0 (e.g. a re-MATCH of a carried OPTIONAL name
		// in a required MATCH) drops the carried group id — the name is
		// no longer OPTIONAL-scoped in this Part. Only propagate a
		// positive id, so downstream Parts do not have to distinguish
		// "declared, group 0" from "not declared" (§3.3 semantics).
		if g, ok := localGroup[v]; ok {
			if g > 0 {
				out.exportedOptionalGroup[v] = g
			}
		} else if g, ok := carriedGroups[v]; ok && g > 0 {
			out.exportedOptionalGroup[v] = g
		}
	}
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
// §4.5.2. Grouping mode is entered when Returns contains at least one
// aggregate — either as a top-level AggregateProjection OR embedded inside
// an ExprProjection (ContainsAggregate() == true). In grouping mode,
// ExprProjection is a grouping key iff ContainsAggregate() == false
// (ADR 0008 amendment 2026-07-06).
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
		switch v := item.Value.(type) {
		case query.AggregateProjection:
			hasAggregate = true
		case query.ExprProjection:
			if v.ContainsAggregate() {
				hasAggregate = true
			}
		}
		if hasAggregate {
			break
		}
	}
	if !hasAggregate {
		return
	}
	// Grouping applies. Non-aggregate items are keys; ExprProjection is a
	// key iff it does NOT contain a nested aggregate.
	for i, item := range part.Returns {
		switch v := item.Value.(type) {
		case query.RefProjection, query.LiteralProjection, query.FuncProjection:
			cols[i].GroupingKey = true
		case query.ExprProjection:
			if !v.ContainsAggregate() {
				cols[i].GroupingKey = true
			}
		}
		// AggregateProjection stays false (the aggregate itself is not a key).
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

// unifyParameterUsesAcrossBranches walks each parameter's Uses in emission
// order, attributes each Use to its lexical branch via a position-cursor over
// Part-index resets, and witnesses against that branch's Part-indexed scope
// table (fvo per ADR 0008 amendment 2026-07-06). Witnesses are unified via
// R2's lattice; the first conflict fires ErrParameterTypeConflict.
//
// Position-cursor recovery. The parser walks branches in source order and
// appends every emission via addParameterUse. Within one branch, Part indices
// on p.Uses are non-decreasing (WITH-swap only advances). A drop in Part index
// signals a UNION boundary — curBranch increments. Same-Part boundaries
// (branch-0 emits Part 0 and branch-1's first emission is also Part 0) are
// ambiguous by index alone: the cursor defaults to the earlier branch, and
// the cross-branch fallback below covers the case where that guess misses.
//
// Cross-branch fallback for the UNION corpus. If the cursor's target scope
// does not contain the Use's Ref variable, the Use contributes zero witnesses
// (bottom by unifier — matches pre-fvo behaviour). If the target scope
// contains the variable but the property lookup fails with ErrUnknownProperty,
// the resolver tries every OTHER branch's scope-table at the same Use.Part()
// index; if any produces a witness, it wins. Only when every branch's scope
// at Use.Part() fails does ErrUnknownProperty surface. This preserves
// byte-identity for parameter_across_union_same_name.cypher (Class-A per
// spec §7.5) while retiring any-valid-witness within a single branch. The
// residual (same-Part UNION boundary + genuine unknown-property) is recorded
// as a follow-up per §7.7 — closing it needs a Use.Branch axis (§3.5), out
// of this cycle's scope.
func unifyParameterUsesAcrossBranches(params []query.Parameter, tables [][]partScope, s schema.Schema) ([]ResolvedParameter, error) {
	if len(params) == 0 {
		return []ResolvedParameter{}, nil
	}
	if len(tables) == 0 {
		return nil, fmt.Errorf("%w: no branch scope tables", ErrOutOfR0Scope)
	}
	out := make([]ResolvedParameter, 0, len(params))
	for _, p := range params {
		var unified ResolvedType
		seen := false
		curBranch := 0
		prevPart := -1
		for _, u := range p.Uses {
			// Part-index-drop cursor: a Part decrease within one parameter's
			// Uses signals a UNION-boundary transition to the next branch.
			// Guard against overshoot when a query has fewer branches than
			// naive index growth would suggest.
			part := usePart(u)
			if part < prevPart && curBranch+1 < len(tables) {
				curBranch++
			}
			prevPart = part

			ws, err := witnessInBranch(u, tables, curBranch, s)
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
			unified = ResolvedUnknown{}
		}
		out = append(out, ResolvedParameter{Name: p.Name, Type: unified})
	}
	return out, nil
}

// usePart returns the branch-relative Part index for a Use. PropertyUse,
// ExprUse, and ClauseSlotUse each carry the axis (fvo). Unknown variants
// return 0 — the tripwire lives in witnessAcrossScopes.
func usePart(u query.Use) int {
	switch uu := u.(type) {
	case query.PropertyUse:
		return uu.Part()
	case query.ExprUse:
		return uu.Part()
	case query.ClauseSlotUse:
		return uu.Part()
	default:
		return 0
	}
}

// witnessInBranch dispatches a Use to its lexical branch's scope table,
// applying the cross-branch fallback for the UNION-same-Part shape. For a
// PropertyUse, if the primary branch's scope at Use.Part() lacks the Ref
// variable, the caller receives zero witnesses (bottom by unifier). If it
// contains the variable but the property lookup fails, retry against every
// other branch's scope at the same Part index; if all fail, the last
// ErrUnknownProperty surfaces. ClauseSlotUse and ExprUse remain
// Part-agnostic — witnessAcrossScopes handles them directly.
func witnessInBranch(u query.Use, tables [][]partScope, primary int, s schema.Schema) ([]ResolvedType, error) {
	if _, isProp := u.(query.PropertyUse); !isProp {
		// Non-property Uses ignore the branch table beyond bounds — pass any
		// non-empty scopes slice; witnessAcrossScopes never indexes into it
		// for these variants. Use the primary branch's scopes for shape.
		return witnessAcrossScopes(u, tables[primary], s)
	}
	ws, err := witnessAcrossScopes(u, tables[primary], s)
	if err == nil {
		return ws, nil
	}
	if !errors.Is(err, ErrUnknownProperty) {
		return nil, err
	}
	// Cross-branch fallback: try every other branch at the same Part.
	// Preserves byte-identity for parameter_across_union_same_name.cypher.
	lastErr := err
	for b := range tables {
		if b == primary {
			continue
		}
		ws, err = witnessAcrossScopes(u, tables[b], s)
		if err == nil {
			return ws, nil
		}
		if errors.Is(err, ErrUnknownProperty) {
			lastErr = err
			continue
		}
		return nil, err
	}
	return nil, lastErr
}

// witnessAcrossScopes produces exactly one witness for a Use — the lexical
// Part attribution now recorded on the Use record (fvo per ADR 0008 amendment
// 2026-07-06) selects the scope. A PropertyUse witnesses against
// branchScopes[u.Part()] only; if that scope does not contain the Ref's
// variable, the caller receives zero witnesses (bottom by unifier — matches
// the pre-fvo behaviour for an unattributed Ref). If the scope contains the
// variable but the property lookup fails, ErrUnknownProperty surfaces
// immediately — the pre-fvo any-valid-witness swallowing (R5 §4.2.4) is
// retired. Non-property faults (ErrOutOfR0Scope for out-of-scope edge Refs,
// var-length edge property projections) surface immediately.
//
// ClauseSlotUse and ExprUse remain Part-agnostic in their type witness —
// the Part axis on their records is a lexical-attribution property for
// future consumer stages (§7.6), not a witness discriminator today.
func witnessAcrossScopes(u query.Use, branchScopes []partScope, s schema.Schema) ([]ResolvedType, error) {
	switch uu := u.(type) {
	case query.PropertyUse:
		ref := uu.Ref()
		idx := uu.Part()
		if idx < 0 || idx >= len(branchScopes) {
			// Defensive: the parser attributes to a valid branch-relative
			// index by construction. An out-of-range index indicates a
			// decoder or model corruption — surface honestly.
			return nil, fmt.Errorf("%w: PropertyUse Part index %d out of range for branch with %d Parts", ErrOutOfR0Scope, idx, len(branchScopes))
		}
		sc := branchScopes[idx]
		if !scopeContains(sc, ref.Variable) {
			return nil, nil
		}
		w, err := propertyUseWitness(ref, sc.nodeTypes, sc.edgeTypes, sc.edgeCands, sc.edgeBindings, sc.nullableBinding, s)
		if err != nil {
			return nil, err
		}
		return []ResolvedType{w}, nil
	case query.ClauseSlotUse:
		return []ResolvedType{ResolvedScalar{Kind: ScalarInt}}, nil
	case query.ExprUse:
		switch uu.Position() {
		case query.ExprInProjection, query.ExprInPredicate,
			query.ExprInSetValue, query.ExprInDeleteTarget:
			w, err := resolveType(uu.EnclosingType())
			if err != nil {
				return nil, err
			}
			return []ResolvedType{w}, nil
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

func inferUnlabelled(pending []query.NodeBinding, edges []query.EdgeBinding, s schema.Schema, resolved map[string]schema.NodeType, callTypes map[string]callBindingSlot) error {
	if len(pending) == 0 {
		return nil
	}
	// R5 §4.2.3 N1 posture: CARRY WINS. An unlabelled binding whose
	// variable was already typed by the carry seed at Phase A1 is a JOIN
	// on the same node identity (openCypher semantics for `WITH a MATCH
	// (a)-[...]->…`), not a redeclaration; skip Phase B inference for it
	// entirely so the carry-seeded type stays authoritative. Doing this
	// here also erases the order-dependence Linus observed in the raw
	// per-Part inference (before this guard, whether an unlabelled `(a)`
	// after `WITH a` got reinferred depended on whether the enclosing
	// edge's other endpoint had already committed).
	if len(resolved) > 0 {
		filtered := pending[:0]
		for _, n := range pending {
			if _, carried := resolved[n.Variable()]; carried {
				continue
			}
			filtered = append(filtered, n)
		}
		pending = filtered
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
				// R7 §4.1.2.1 addendum: an inferred unlabelled node whose
				// name collides with a carried CALL YIELD scalar fails at
				// commit — the shape-posture check the labelled arm runs
				// at Phase A1 fires here for the unlabelled path.
				if _, seenCall := callTypes[n.Variable()]; seenCall {
					return fmt.Errorf("%w: variable %q carried as CALL YIELD scalar, re-bound as %s", ErrPartBindingTypeConflict, n.Variable(), only)
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
func projectionType(p query.Projection, nodeTypes map[string]schema.NodeType, edgeTypes map[string]schema.EdgeType, edgeKeys map[string]schema.EdgeKey, edgeCands map[string][]schema.EdgeKey, edgeBindings map[string]query.EdgeBinding, nullableBinding map[string]bool, callTypes map[string]callBindingSlot, carriedResolvedTypes map[string]ResolvedType, s schema.Schema) (ResolvedType, error) {
	switch pp := p.(type) {
	case query.RefProjection:
		return refProjectionType(pp.Ref(), nodeTypes, edgeTypes, edgeKeys, edgeCands, edgeBindings, nullableBinding, callTypes, carriedResolvedTypes, s)
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
// downstream), refProjectionType returns the carried type directly. R7 §4.2
// adds the callTypes lane BEFORE the carried-alias bypass: a bare Ref against
// a CALL YIELD variable bridges to ResolvedProperty (or ResolvedUnknown for
// NUMBER); a property lookup on a CALL YIELD variable fires ErrUnknownProperty
// with a widened message set.
func refProjectionType(ref query.Ref, nodeTypes map[string]schema.NodeType, edgeTypes map[string]schema.EdgeType, edgeKeys map[string]schema.EdgeKey, edgeCands map[string][]schema.EdgeKey, edgeBindings map[string]query.EdgeBinding, nullableBinding map[string]bool, callTypes map[string]callBindingSlot, carriedResolvedTypes map[string]ResolvedType, s schema.Schema) (ResolvedType, error) {
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
		// R7 §4.2 — CALL YIELD lane, fired BEFORE the carried-alias bypass.
		if slot, ok := callTypes[ref.Variable]; ok {
			return callProjectionType(slot, ref, nullableBinding)
		}
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

// callProjectionType maps a Ref against a CALL YIELD variable's callBindingSlot
// to its resolved column type (spec §4.2.1). Bare Ref bridges the bridged
// query.Type to ResolvedProperty (INT / FLOAT / STRING) or ResolvedUnknown
// (NUMBER-marker → TypeUnknown wire form). Property lookup on a scalar
// (Ref.Property != "") fires ErrUnknownProperty with a widened message set —
// §4.2.2, one of the R7 §5.3 message-set widenings.
func callProjectionType(slot callBindingSlot, ref query.Ref, nullableBinding map[string]bool) (ResolvedType, error) {
	if ref.Property != "" {
		return nil, fmt.Errorf("%w: %s.%s (CALL YIELD variable %q is a scalar)", ErrUnknownProperty, ref.Variable, ref.Property, ref.Variable)
	}
	nullable := nullableBinding[ref.Variable]
	switch slot.resultType.(type) {
	case query.TypeInt:
		return ResolvedProperty{Type: graph.TypeInt, Nullable: nullable}, nil
	case query.TypeFloat:
		return ResolvedProperty{Type: graph.TypeFloat, Nullable: nullable}, nil
	case query.TypeString:
		return ResolvedProperty{Type: graph.TypeString, Nullable: nullable}, nil
	case query.TypeUnknown:
		return ResolvedUnknown{}, nil
	default:
		return ResolvedUnknown{}, nil
	}
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

// demoteNullableInPlace runs the ay9+5xg-widened regime-(a) demotion
// on part.Bindings against a pre-seeded table: bare-ref demotion
// (5xg — a required bare re-reference is a witness, flipping the
// re-referenced binding's table entry directly), plus per-edge
// endpoint witnessing (R4 §4.4), plus OPTIONAL-group closure (ay9).
// The 5xg pre-pass runs before the group-closure fixed point and
// does not touch demotedGroups; the two demotion channels are
// orthogonal (both write false to the same table, both are monotone,
// composition is order-independent). The subsequent fixed-point loop
// may observe 5xg's flipped entries and demote co-introduced siblings
// via (iv), producing the compose-with-group cascade §8.4 fixture 4
// witnesses.
//
// carriedGroups seeds the group-membership maps from the carry (gqlc-984,
// closing spec §2.5 residual): a WITH-carried binding retains its Part-K
// OPTIONAL-group id, so proving any member in Part K+1 pulls its cross-Part
// siblings via the same fixed point. Group ids are per-query and unique
// across the whole parse (§3.3), so carried and local ids share the same
// numeric space without collision. A local binding at the same name that
// carries a distinct local group id overrides the carried id — local
// shadows carry, matching the seedLocalNullability discipline.
func demoteNullableInPlace(bindings []query.Binding, table map[string]bool, carriedGroups map[string]int) {
	// 5xg pre-pass: bare-ref demotion. A binding whose parser-time
	// flag is true was re-referenced in a required bare pattern; the
	// row-drop witness demotes it. Anonymous bindings (v == "") skip
	// — they carry no table entry.
	for _, b := range bindings {
		switch bb := b.(type) {
		case query.NodeBinding:
			if bb.ReferencedInRequiredBarePattern() && bb.Variable() != "" {
				if _, present := table[bb.Variable()]; present {
					table[bb.Variable()] = false
				}
			}
		case query.EdgeBinding:
			if bb.ReferencedInRequiredBarePattern() && bb.Variable() != "" {
				if _, present := table[bb.Variable()]; present {
					table[bb.Variable()] = false
				}
			}
		}
	}
	// ay9 pre-pass: OPTIONAL-group membership scan. A name may belong to
	// multiple groups simultaneously — a carried group id from Part K, plus a
	// fresh local group id if Part K+1 re-declares the name under a new
	// OPTIONAL MATCH. Any one group being proven demotes the name (and every
	// other member of that group). Seed from carry first, then union in the
	// local declarations.
	members := map[int][]string{}   // group id → named members
	groupsOf := map[string][]int{}  // named member → group ids (may span carry + local)
	addMember := func(v string, g int) {
		if v == "" || g <= 0 {
			return
		}
		for _, existing := range groupsOf[v] {
			if existing == g {
				return
			}
		}
		groupsOf[v] = append(groupsOf[v], g)
		members[g] = append(members[g], v)
	}
	for name, g := range carriedGroups {
		addMember(name, g)
	}
	for _, b := range bindings {
		switch bb := b.(type) {
		case query.NodeBinding:
			addMember(bb.Variable(), bb.OptionalGroup())
		case query.EdgeBinding:
			addMember(bb.Variable(), bb.OptionalGroup())
		}
	}
	demotedGroups := map[int]bool{}
	demoteGroup := func(g int) bool {
		if g == 0 || demotedGroups[g] {
			return false
		}
		demotedGroups[g] = true
		for _, m := range members[g] {
			if _, present := table[m]; present {
				table[m] = false
			}
		}
		return true
	}
	// A carried binding whose local Nullable() entry in the table is
	// already false (either from seedLocalNullability's re-MATCH override
	// or from the 5xg pre-pass) is a proven witness for its carried
	// group. Fire that closure before the edge-driven fixed point so a
	// carried group without a local edge witness still demotes.
	for name, gs := range groupsOf {
		if nb, present := table[name]; present && !nb {
			for _, g := range gs {
				demoteGroup(g)
			}
		}
	}
	demoteGroupsOf := func(v string) bool {
		changed := false
		for _, g := range groupsOf[v] {
			if demoteGroup(g) {
				changed = true
			}
		}
		return changed
	}
	for changed := true; changed; {
		changed = false
		for _, b := range bindings {
			e, ok := b.(query.EdgeBinding)
			if !ok {
				continue
			}
			// ay9: an OPTIONAL edge whose group is proven is an
			// effective witness (its existence on surviving rows is
			// established); the §4.4.3 hop gate applies unchanged.
			if (e.Nullable() && !demotedGroups[e.OptionalGroup()]) || !qualifiedDemoter(e) {
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
				if nb, present := table[v]; present && nb {
					table[v] = false
					changed = true
				}
				if demoteGroupsOf(v) {
					changed = true
				}
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
	case query.CallBinding:
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

// validateEffects is R6 Phase E: walk part.Effects in slice order, dispatch
// each Effect through its per-variant validator, short-circuit on first
// failure. Reads from the schema-committed binding tables and the carried
// resolved types; never mutates them. The dispatch is a type switch on the
// closed Effect sum (query.go:1631-1660); the default arm is a defensive
// tripwire for a future Effect variant landing without an R6 refresh.
func validateEffects(effects []query.Effect, nodeTypes map[string]schema.NodeType, edgeTypes map[string]schema.EdgeType, edgeCands map[string][]schema.EdgeKey, edgeBindings map[string]query.EdgeBinding, carriedResolvedTypes map[string]ResolvedType, s schema.Schema) error {
	for _, e := range effects {
		if err := validateEffect(e, nodeTypes, edgeTypes, edgeCands, edgeBindings, carriedResolvedTypes, s); err != nil {
			return err
		}
	}
	return nil
}

func validateEffect(e query.Effect, nodeTypes map[string]schema.NodeType, edgeTypes map[string]schema.EdgeType, edgeCands map[string][]schema.EdgeKey, edgeBindings map[string]query.EdgeBinding, carriedResolvedTypes map[string]ResolvedType, s schema.Schema) error {
	switch ee := e.(type) {
	case query.CreateEffect:
		return validateCreateEffect(ee, nodeTypes, edgeBindings)
	case query.MergeEffect:
		return validateMergeEffect(ee, nodeTypes, edgeTypes, edgeCands, edgeBindings, carriedResolvedTypes, s)
	case query.SetPropertyEffect:
		return validateSetPropertyEffect(ee, nodeTypes, edgeTypes, edgeCands, edgeBindings, carriedResolvedTypes, s)
	case query.SetEntityEffect:
		return validateSetEntityEffect(ee, nodeTypes, edgeTypes, edgeCands, edgeBindings, carriedResolvedTypes)
	case query.SetLabelsEffect:
		return validateSetLabelsEffect(ee, nodeTypes, edgeBindings, carriedResolvedTypes, s)
	case query.RemovePropertyEffect:
		return validateRemovePropertyEffect(ee, nodeTypes, edgeTypes, edgeCands, edgeBindings, carriedResolvedTypes, s)
	case query.RemoveLabelsEffect:
		return validateRemoveLabelsEffect(ee, nodeTypes, edgeBindings, carriedResolvedTypes, s)
	case query.DeleteEffect:
		return validateDeleteEffect(ee, nodeTypes, edgeTypes, edgeCands, edgeBindings, carriedResolvedTypes, s)
	default:
		return fmt.Errorf("%w: unknown Effect variant (%T)", ErrOutOfR0Scope, e)
	}
}

// validateCreateEffect walks e.Variables() and confirms each non-empty name is
// present in nodeTypes OR edgeBindings. Anonymous edges (v == "") skip per
// listener.go:349-350. Reachability of the tripwire is zero from parser input.
func validateCreateEffect(e query.CreateEffect, nodeTypes map[string]schema.NodeType, edgeBindings map[string]query.EdgeBinding) error {
	for _, v := range e.Variables() {
		if v == "" {
			continue
		}
		if _, ok := nodeTypes[v]; ok {
			continue
		}
		if _, ok := edgeBindings[v]; ok {
			continue
		}
		return fmt.Errorf("%w: CREATE variable %q not bound after phase C", ErrInvalidEffectTarget, v)
	}
	return nil
}

// validateMergeEffect runs the CREATE variable-presence check and routes each
// SetEffect in OnMatch / OnCreate through the SET-family validators. Sub-sum
// type-safety is guaranteed by query.go:1651-1660 (only Set-family effects can
// appear inside).
func validateMergeEffect(e query.MergeEffect, nodeTypes map[string]schema.NodeType, edgeTypes map[string]schema.EdgeType, edgeCands map[string][]schema.EdgeKey, edgeBindings map[string]query.EdgeBinding, carriedResolvedTypes map[string]ResolvedType, s schema.Schema) error {
	for _, v := range e.Variables() {
		if v == "" {
			continue
		}
		if _, ok := nodeTypes[v]; ok {
			continue
		}
		if _, ok := edgeBindings[v]; ok {
			continue
		}
		return fmt.Errorf("%w: MERGE variable %q not bound after phase C", ErrInvalidEffectTarget, v)
	}
	for _, se := range e.OnMatch() {
		if err := validateEffect(se, nodeTypes, edgeTypes, edgeCands, edgeBindings, carriedResolvedTypes, s); err != nil {
			return err
		}
	}
	for _, se := range e.OnCreate() {
		if err := validateEffect(se, nodeTypes, edgeTypes, edgeCands, edgeBindings, carriedResolvedTypes, s); err != nil {
			return err
		}
	}
	return nil
}

// validateSetPropertyEffect resolves the target Ref against the binding tables
// and looks up the property on the resolved schema entity. Rejects var-length
// edge targets (a var-length binding is a list of edges, not one edge).
// Rejects projection-alias targets and out-of-scope names (defensive tripwire)
// with ErrInvalidEffectTarget.
func validateSetPropertyEffect(e query.SetPropertyEffect, nodeTypes map[string]schema.NodeType, edgeTypes map[string]schema.EdgeType, edgeCands map[string][]schema.EdgeKey, edgeBindings map[string]query.EdgeBinding, carriedResolvedTypes map[string]ResolvedType, s schema.Schema) error {
	v := e.Target().Variable
	p := e.Target().Property
	if nt, ok := nodeTypes[v]; ok {
		if _, ok := nt.Properties[p]; !ok {
			return fmt.Errorf("%w: %s.%s", ErrUnknownProperty, v, p)
		}
		return nil
	}
	if et, ok := edgeTypes[v]; ok {
		if edgeBindings[v].Hops() != nil {
			return fmt.Errorf("%w: SET on variable-length edge %q", ErrInvalidEffectTarget, v)
		}
		if _, ok := et.Properties[p]; !ok {
			return fmt.Errorf("%w: %s.%s", ErrUnknownProperty, v, p)
		}
		return nil
	}
	if cands, ok := edgeCands[v]; ok {
		if edgeBindings[v].Hops() != nil {
			return fmt.Errorf("%w: SET on variable-length edge %q", ErrInvalidEffectTarget, v)
		}
		if _, err := unionProperty(cands, s, v, p, false); err != nil {
			return err
		}
		return nil
	}
	if _, ok := carriedResolvedTypes[v]; ok {
		return fmt.Errorf("%w: SET %s.%s: %q resolves to a projection alias, not an entity binding", ErrInvalidEffectTarget, v, p, v)
	}
	return fmt.Errorf("%w: SET %s.%s: %q not in any Part scope", ErrInvalidEffectTarget, v, p, v)
}

// validateSetEntityEffect resolves the target variable against the entity
// binding tables. Rejects var-length edge targets and projection-alias / out-
// of-scope targets with ErrInvalidEffectTarget. No property-existence check —
// the RHS map's keys are runtime (per §4.3.2 in the R6 spec).
func validateSetEntityEffect(e query.SetEntityEffect, nodeTypes map[string]schema.NodeType, edgeTypes map[string]schema.EdgeType, edgeCands map[string][]schema.EdgeKey, edgeBindings map[string]query.EdgeBinding, carriedResolvedTypes map[string]ResolvedType) error {
	v := e.TargetVariable()
	if _, ok := nodeTypes[v]; ok {
		return nil
	}
	if _, ok := edgeTypes[v]; ok {
		if edgeBindings[v].Hops() != nil {
			return fmt.Errorf("%w: SET on variable-length edge %q", ErrInvalidEffectTarget, v)
		}
		return nil
	}
	if _, ok := edgeCands[v]; ok {
		if edgeBindings[v].Hops() != nil {
			return fmt.Errorf("%w: SET on variable-length edge %q", ErrInvalidEffectTarget, v)
		}
		return nil
	}
	if _, ok := carriedResolvedTypes[v]; ok {
		return fmt.Errorf("%w: SET %s = ...: %q resolves to a projection alias, not an entity binding", ErrInvalidEffectTarget, v, v)
	}
	return fmt.Errorf("%w: SET %s = ...: %q not in any Part scope", ErrInvalidEffectTarget, v, v)
}

// validateSetLabelsEffect verifies the target is a node binding (edges reject
// with ErrInvalidEffectTarget since labels are node-only), then confirms each
// label individually appears in at least one declared NodeType's LabelSet.
// Missing labels surface ErrUnknownLabel per §4.3.3.
func validateSetLabelsEffect(e query.SetLabelsEffect, nodeTypes map[string]schema.NodeType, edgeBindings map[string]query.EdgeBinding, carriedResolvedTypes map[string]ResolvedType, s schema.Schema) error {
	v := e.TargetVariable()
	if _, ok := nodeTypes[v]; !ok {
		if _, ok := edgeBindings[v]; ok {
			return fmt.Errorf("%w: SET labels on edge binding %q", ErrInvalidEffectTarget, v)
		}
		if _, ok := carriedResolvedTypes[v]; ok {
			return fmt.Errorf("%w: SET labels on projection alias %q", ErrInvalidEffectTarget, v)
		}
		return fmt.Errorf("%w: SET %s: %q not in any Part scope", ErrInvalidEffectTarget, v, v)
	}
	for _, L := range e.Labels() {
		if !labelDeclared(L, s) {
			return fmt.Errorf("%w: SET %s:%s: label %q not declared on any node type", ErrUnknownLabel, v, L, L)
		}
	}
	return nil
}

// validateRemovePropertyEffect mirrors validateSetPropertyEffect: same target
// resolution, same property-existence check. No value side to check.
func validateRemovePropertyEffect(e query.RemovePropertyEffect, nodeTypes map[string]schema.NodeType, edgeTypes map[string]schema.EdgeType, edgeCands map[string][]schema.EdgeKey, edgeBindings map[string]query.EdgeBinding, carriedResolvedTypes map[string]ResolvedType, s schema.Schema) error {
	v := e.Target().Variable
	p := e.Target().Property
	if nt, ok := nodeTypes[v]; ok {
		if _, ok := nt.Properties[p]; !ok {
			return fmt.Errorf("%w: %s.%s", ErrUnknownProperty, v, p)
		}
		return nil
	}
	if et, ok := edgeTypes[v]; ok {
		if edgeBindings[v].Hops() != nil {
			return fmt.Errorf("%w: REMOVE on variable-length edge %q", ErrInvalidEffectTarget, v)
		}
		if _, ok := et.Properties[p]; !ok {
			return fmt.Errorf("%w: %s.%s", ErrUnknownProperty, v, p)
		}
		return nil
	}
	if cands, ok := edgeCands[v]; ok {
		if edgeBindings[v].Hops() != nil {
			return fmt.Errorf("%w: REMOVE on variable-length edge %q", ErrInvalidEffectTarget, v)
		}
		if _, err := unionProperty(cands, s, v, p, false); err != nil {
			return err
		}
		return nil
	}
	if _, ok := carriedResolvedTypes[v]; ok {
		return fmt.Errorf("%w: REMOVE %s.%s: %q resolves to a projection alias, not an entity binding", ErrInvalidEffectTarget, v, p, v)
	}
	return fmt.Errorf("%w: REMOVE %s.%s: %q not in any Part scope", ErrInvalidEffectTarget, v, p, v)
}

// validateRemoveLabelsEffect is the REMOVE analogue of validateSetLabelsEffect:
// same target discipline, same per-label declaration check.
func validateRemoveLabelsEffect(e query.RemoveLabelsEffect, nodeTypes map[string]schema.NodeType, edgeBindings map[string]query.EdgeBinding, carriedResolvedTypes map[string]ResolvedType, s schema.Schema) error {
	v := e.TargetVariable()
	if _, ok := nodeTypes[v]; !ok {
		if _, ok := edgeBindings[v]; ok {
			return fmt.Errorf("%w: REMOVE labels on edge binding %q", ErrInvalidEffectTarget, v)
		}
		if _, ok := carriedResolvedTypes[v]; ok {
			return fmt.Errorf("%w: REMOVE labels on projection alias %q", ErrInvalidEffectTarget, v)
		}
		return fmt.Errorf("%w: REMOVE %s: %q not in any Part scope", ErrInvalidEffectTarget, v, v)
	}
	for _, L := range e.Labels() {
		if !labelDeclared(L, s) {
			return fmt.Errorf("%w: REMOVE %s:%s: label %q not declared on any node type", ErrUnknownLabel, v, L, L)
		}
	}
	return nil
}

// validateDeleteEffect walks e.Targets() for bare-shape checks (entity DELETE
// or bare-property DELETE) and e.Refs() as a defensive walk (parser
// referential integrity already covers them). See §4.4.
func validateDeleteEffect(e query.DeleteEffect, nodeTypes map[string]schema.NodeType, edgeTypes map[string]schema.EdgeType, edgeCands map[string][]schema.EdgeKey, edgeBindings map[string]query.EdgeBinding, carriedResolvedTypes map[string]ResolvedType, s schema.Schema) error {
	for _, t := range e.Targets() {
		v := t.Variable
		p := t.Property
		if p == "" {
			if _, ok := nodeTypes[v]; ok {
				continue
			}
			if _, ok := edgeTypes[v]; ok {
				continue
			}
			if _, ok := edgeCands[v]; ok {
				continue
			}
			if _, ok := carriedResolvedTypes[v]; ok {
				return fmt.Errorf("%w: DELETE %s: %q resolves to a projection alias, not an entity binding", ErrInvalidEffectTarget, v, v)
			}
			return fmt.Errorf("%w: DELETE %s: %q not in any Part scope", ErrInvalidEffectTarget, v, v)
		}
		if nt, ok := nodeTypes[v]; ok {
			if _, ok := nt.Properties[p]; !ok {
				return fmt.Errorf("%w: %s.%s", ErrUnknownProperty, v, p)
			}
			continue
		}
		if et, ok := edgeTypes[v]; ok {
			if edgeBindings[v].Hops() != nil {
				return fmt.Errorf("%w: DELETE on variable-length edge %q", ErrInvalidEffectTarget, v)
			}
			if _, ok := et.Properties[p]; !ok {
				return fmt.Errorf("%w: %s.%s", ErrUnknownProperty, v, p)
			}
			continue
		}
		if cands, ok := edgeCands[v]; ok {
			if edgeBindings[v].Hops() != nil {
				return fmt.Errorf("%w: DELETE on variable-length edge %q", ErrInvalidEffectTarget, v)
			}
			if _, err := unionProperty(cands, s, v, p, false); err != nil {
				return err
			}
			continue
		}
		if _, ok := carriedResolvedTypes[v]; ok {
			return fmt.Errorf("%w: DELETE %s.%s: %q resolves to a projection alias, not an entity binding", ErrInvalidEffectTarget, v, p, v)
		}
		return fmt.Errorf("%w: DELETE %s.%s: %q not in any Part scope", ErrInvalidEffectTarget, v, p, v)
	}
	// Refs walk: parser's referential-integrity sweep covers these; skip per
	// §4.4 step 2 ("R6 runs no additional check on e.Refs()").
	return nil
}

// labelDeclared reports whether label appears as a component of any declared
// NodeType's LabelSetKey — the R6 policy per §4.3.3 (per-label existence, not
// union-existence). Naive O(|s.Nodes| × avg-arity) iteration; schemas are
// small.
func labelDeclared(label string, s schema.Schema) bool {
	for k := range s.Nodes {
		for _, lbl := range k.Split() {
			if lbl == label {
				return true
			}
		}
	}
	return false
}

// argAssignable reports whether an argument mined to argType at parse can be
// assigned to a signature parameter declared with token. The 0ig-adopted
// assignability lattice (spec §8.2):
//
//   - INTEGER: strict — TypeInt only.
//   - FLOAT:   loose  — TypeFloat OR TypeInt (TCK Call3 [5] admits INTEGER
//     at a FLOAT-typed position; ADR 0007 line 173 does not exclude this).
//   - STRING:  strict — TypeString only.
//   - NUMBER:  loose  — TypeInt OR TypeFloat (ADR 0007 line 172-174:
//     "assignable-from INTEGER-or-FLOAT at the argument site").
//
// TypeUnknown and TypeNull are resolver-side wildcards: TypeUnknown for a
// $param / n.name argument (the parser cannot type-narrow those at CALL-site),
// TypeNull for a bare null literal (shape.go:79 mines NULL to TypeNull, a
// distinct sum member from TypeUnknown per type.go:80). Admitting both
// preserves R7's parser-authoritative posture and validates TCK Call5 [4]
// (CALL test.my.proc(null) against nullable-typed params). A downstream
// $param whose enclosing type disagrees with the sig token is caught by the
// parameter-unification pass in ExprInProjection anyway.
func argAssignable(token procsig.TypeToken, argType query.Type) bool {
	if _, isUnknown := argType.(query.TypeUnknown); isUnknown {
		return true
	}
	if _, isNull := argType.(query.TypeNull); isNull {
		return true
	}
	switch token {
	case procsig.TokenInteger:
		_, ok := argType.(query.TypeInt)
		return ok
	case procsig.TokenFloat:
		_, isFloat := argType.(query.TypeFloat)
		_, isInt := argType.(query.TypeInt) // §8.2.1: TCK Call3 [5] admits INTEGER-at-FLOAT.
		return isFloat || isInt
	case procsig.TokenString:
		_, ok := argType.(query.TypeString)
		return ok
	case procsig.TokenNumber:
		_, isFloat := argType.(query.TypeFloat)
		_, isInt := argType.(query.TypeInt)
		return isFloat || isInt
	default:
		return false
	}
}
