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
//
// Methods (Contains, PropertyUseWitness, WitnessUse) live in scope.go
// alongside partScope's producer (scope.Snapshot).
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
// binding across the Part boundary. Group ids are per-query and unique across
// the whole parse (§3.3), so a carried id cannot collide with a local id in
// the downstream Part. Demotion of a member proven in Part K+1 pulls the
// whole carried group via the ay9 fixed-point in demoteNullableInPlace.
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
// snapshot — enough for the top-level unifier to witness every Use against
// the scope its emission-time (branch, part) attribution selects (§4.2.4).
// Witnessing runs at the top-level resolve() after every branch has resolved
// its Parts, because a Use may live in any branch.
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
	sc := newScope(carry)
	sc.Ingest(part)

	// Phase A1: local labelled-node / edge / call admission. The
	// unlabelled-node arm defers to Phase B (InferUnlabelled reads
	// s.bindings and picks them up). Bind* runs the cross-lane shadow
	// cascade and the R5/R7 conflict checks.
	for _, b := range sc.bindings {
		switch bb := b.(type) {
		case query.NodeBinding:
			if len(bb.Labels()) == 0 {
				continue
			}
			key := bb.Labels().Key()
			nt, ok := s.Nodes[key]
			if !ok {
				return nil, branchState{}, nil, fmt.Errorf("%w: %s", ErrUnknownLabel, key)
			}
			if err := sc.BindNode(bb, nt); err != nil {
				return nil, branchState{}, nil, err
			}
		case query.EdgeBinding:
			if err := sc.BindEdge(bb); err != nil {
				return nil, branchState{}, nil, err
			}
		case query.CallBinding:
			if err := sc.BindCall(bb, r); err != nil {
				return nil, branchState{}, nil, err
			}
		default:
			return nil, branchState{}, nil, fmt.Errorf("%w: %s binding", ErrOutOfR0Scope, b.Kind())
		}
	}

	// Phase A2 (defer unfulfilled endpoint edges) + Phase B (infer
	// unlabelled nodes) + Phase C (retry deferred edges), all
	// scope-internal per spec §2.2.
	if err := sc.CloseEdges(s); err != nil {
		return nil, branchState{}, nil, err
	}

	// Phase D (§4.6): seed with carry, override with local, then demote.
	// Nullability seed already ran at newScope (the carry's nullable
	// bindings landed in sc.nullableBinding). Local Bindings override
	// the carry before demotion runs so Part K+1 that re-MATCHes an
	// OPTIONAL-carried `b` sees sc.nullableBinding["b"] = false.
	sc.SeedLocalNullability()
	sc.DemoteNullability()

	// Phase E (R6 §4.1): effect validation. Runs after Phase D so effect
	// targets see the same schema-committed binding tables and
	// effective-nullability map that the projection walk sees. First
	// failure short-circuits.
	if err := sc.ValidateEffects(s); err != nil {
		return nil, branchState{}, nil, err
	}

	// Projection walk (§4.4): scopeOrder + materialiseReturns +
	// per-item projectionType, all on scope. Populates sc.scopeOrder,
	// sc.items, sc.columns for Export.
	if err := sc.ResolveProjections(s); err != nil {
		return nil, branchState{}, nil, err
	}

	// Emit this Part's scope snapshot as one parameterUseSite. The
	// top-level unifier walks every parameter's Uses against every
	// scope; a PropertyUse witnesses at the scope whose tables contain
	// its Ref's binding (§4.2.4).
	site := parameterUseSite{scope: sc.Snapshot()}

	// Build the exported branchState for Part K+1. R7 §4.6 adds the
	// exportedCallTypes lane for CALL YIELD carry-forward. Names
	// surviving WITH keep their Part-K OPTIONAL group id via
	// exportedOptionalGroup, so downstream Parts can close cross-Part
	// group demotion via the ay9 fixed point.
	return sc.columns, sc.Export(), []parameterUseSite{site}, nil
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

// unifyParameterUsesAcrossBranches witnesses each parameter Use against its
// emission-attributed branch's Part-indexed scope table — Use.Branch selects
// the branch (gqlc-qcc per ADR 0008 amendment 2026-07-12), Use.Part the Part
// within it (fvo per ADR 0008 amendment 2026-07-06). Witnesses are unified
// via R2's lattice; the first conflict fires ErrParameterTypeConflict.
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
		for _, u := range p.Uses {
			b := u.Branch()
			if b < 0 || b >= len(tables) {
				// Defensive: the parser attributes to a valid query-level
				// branch index by construction — same posture as the Part
				// guard in witnessAcrossScopes.
				return nil, fmt.Errorf("%w: Use Branch index %d out of range for query with %d branches", ErrOutOfR0Scope, b, len(tables))
			}
			sc, err := selectPartScope(u, tables[b])
			if err != nil {
				return nil, err
			}
			ws, err := sc.WitnessUse(u, s)
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

// selectPartScope resolves which partScope the unifier should dispatch to
// for a given Use. PropertyUse needs the Part-attributed scope so its Ref
// hits the correct binding tables; the range check that used to live inside
// witnessAcrossScopes moves here (parser attribution is authoritative — an
// out-of-range index is decoder / model corruption). ClauseSlotUse and
// ExprUse are Part-agnostic in their type witness (the Part axis is a
// lexical-attribution property for future consumer stages, not a witness
// discriminator today), so any scope value works — a zero partScope keeps
// the WitnessUse receiver argument-typed without touching branchScopes.
func selectPartScope(u query.Use, branchScopes []partScope) (partScope, error) {
	pu, ok := u.(query.PropertyUse)
	if !ok {
		return partScope{}, nil
	}
	idx := pu.Part()
	if idx < 0 || idx >= len(branchScopes) {
		return partScope{}, fmt.Errorf("%w: PropertyUse Part index %d out of range for branch with %d Parts", ErrOutOfR0Scope, idx, len(branchScopes))
	}
	return branchScopes[idx], nil
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

// validateEffect dispatches one Effect through its per-variant validator against
// the scope's committed binding tables. The dispatch is a type switch on the
// closed Effect sum (query.go:1631-1660); the default arm is a defensive
// tripwire for a future Effect variant landing without an R6 refresh.
func validateEffect(sc *scope, e query.Effect, s schema.Schema) error {
	switch ee := e.(type) {
	case query.CreateEffect:
		return validateCreateEffect(sc, ee)
	case query.MergeEffect:
		return validateMergeEffect(sc, ee, s)
	case query.SetPropertyEffect:
		return validateSetPropertyEffect(sc, ee, s)
	case query.SetEntityEffect:
		return validateSetEntityEffect(sc, ee)
	case query.SetLabelsEffect:
		return validateSetLabelsEffect(sc, ee, s)
	case query.RemovePropertyEffect:
		return validateRemovePropertyEffect(sc, ee, s)
	case query.RemoveLabelsEffect:
		return validateRemoveLabelsEffect(sc, ee, s)
	case query.DeleteEffect:
		return validateDeleteEffect(sc, ee, s)
	default:
		return fmt.Errorf("%w: unknown Effect variant (%T)", ErrOutOfR0Scope, e)
	}
}

// validateCreateEffect walks e.Variables() and confirms each non-empty name is
// present in sc.nodeTypes OR sc.edgeBindings. Anonymous edges (v == "") skip
// per listener.go:349-350. Reachability of the tripwire is zero from parser
// input.
func validateCreateEffect(sc *scope, e query.CreateEffect) error {
	for _, v := range e.Variables() {
		if v == "" {
			continue
		}
		if _, ok := sc.nodeTypes[v]; ok {
			continue
		}
		if _, ok := sc.edgeBindings[v]; ok {
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
func validateMergeEffect(sc *scope, e query.MergeEffect, s schema.Schema) error {
	for _, v := range e.Variables() {
		if v == "" {
			continue
		}
		if _, ok := sc.nodeTypes[v]; ok {
			continue
		}
		if _, ok := sc.edgeBindings[v]; ok {
			continue
		}
		return fmt.Errorf("%w: MERGE variable %q not bound after phase C", ErrInvalidEffectTarget, v)
	}
	for _, se := range e.OnMatch() {
		if err := validateEffect(sc, se, s); err != nil {
			return err
		}
	}
	for _, se := range e.OnCreate() {
		if err := validateEffect(sc, se, s); err != nil {
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
func validateSetPropertyEffect(sc *scope, e query.SetPropertyEffect, s schema.Schema) error {
	v := e.Target().Variable
	p := e.Target().Property
	if nt, ok := sc.nodeTypes[v]; ok {
		if _, ok := nt.Properties[p]; !ok {
			return fmt.Errorf("%w: %s.%s", ErrUnknownProperty, v, p)
		}
		return nil
	}
	if et, ok := sc.edgeTypes[v]; ok {
		if sc.edgeBindings[v].Hops() != nil {
			return fmt.Errorf("%w: SET on variable-length edge %q", ErrInvalidEffectTarget, v)
		}
		if _, ok := et.Properties[p]; !ok {
			return fmt.Errorf("%w: %s.%s", ErrUnknownProperty, v, p)
		}
		return nil
	}
	if cands, ok := sc.edgeCands[v]; ok {
		if sc.edgeBindings[v].Hops() != nil {
			return fmt.Errorf("%w: SET on variable-length edge %q", ErrInvalidEffectTarget, v)
		}
		if _, err := unionProperty(cands, s, v, p, false); err != nil {
			return err
		}
		return nil
	}
	if _, ok := sc.carriedResolvedTypes[v]; ok {
		return fmt.Errorf("%w: SET %s.%s: %q resolves to a projection alias, not an entity binding", ErrInvalidEffectTarget, v, p, v)
	}
	return fmt.Errorf("%w: SET %s.%s: %q not in any Part scope", ErrInvalidEffectTarget, v, p, v)
}

// validateSetEntityEffect resolves the target variable against the entity
// binding tables. Rejects var-length edge targets and projection-alias / out-
// of-scope targets with ErrInvalidEffectTarget. No property-existence check —
// the RHS map's keys are runtime (per §4.3.2 in the R6 spec).
func validateSetEntityEffect(sc *scope, e query.SetEntityEffect) error {
	v := e.TargetVariable()
	if _, ok := sc.nodeTypes[v]; ok {
		return nil
	}
	if _, ok := sc.edgeTypes[v]; ok {
		if sc.edgeBindings[v].Hops() != nil {
			return fmt.Errorf("%w: SET on variable-length edge %q", ErrInvalidEffectTarget, v)
		}
		return nil
	}
	if _, ok := sc.edgeCands[v]; ok {
		if sc.edgeBindings[v].Hops() != nil {
			return fmt.Errorf("%w: SET on variable-length edge %q", ErrInvalidEffectTarget, v)
		}
		return nil
	}
	if _, ok := sc.carriedResolvedTypes[v]; ok {
		return fmt.Errorf("%w: SET %s = ...: %q resolves to a projection alias, not an entity binding", ErrInvalidEffectTarget, v, v)
	}
	return fmt.Errorf("%w: SET %s = ...: %q not in any Part scope", ErrInvalidEffectTarget, v, v)
}

// validateSetLabelsEffect verifies the target is a node binding (edges reject
// with ErrInvalidEffectTarget since labels are node-only), then confirms each
// label individually appears in at least one declared NodeType's LabelSet.
// Missing labels surface ErrUnknownLabel per §4.3.3.
func validateSetLabelsEffect(sc *scope, e query.SetLabelsEffect, s schema.Schema) error {
	v := e.TargetVariable()
	if _, ok := sc.nodeTypes[v]; !ok {
		if _, ok := sc.edgeBindings[v]; ok {
			return fmt.Errorf("%w: SET labels on edge binding %q", ErrInvalidEffectTarget, v)
		}
		if _, ok := sc.carriedResolvedTypes[v]; ok {
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
func validateRemovePropertyEffect(sc *scope, e query.RemovePropertyEffect, s schema.Schema) error {
	v := e.Target().Variable
	p := e.Target().Property
	if nt, ok := sc.nodeTypes[v]; ok {
		if _, ok := nt.Properties[p]; !ok {
			return fmt.Errorf("%w: %s.%s", ErrUnknownProperty, v, p)
		}
		return nil
	}
	if et, ok := sc.edgeTypes[v]; ok {
		if sc.edgeBindings[v].Hops() != nil {
			return fmt.Errorf("%w: REMOVE on variable-length edge %q", ErrInvalidEffectTarget, v)
		}
		if _, ok := et.Properties[p]; !ok {
			return fmt.Errorf("%w: %s.%s", ErrUnknownProperty, v, p)
		}
		return nil
	}
	if cands, ok := sc.edgeCands[v]; ok {
		if sc.edgeBindings[v].Hops() != nil {
			return fmt.Errorf("%w: REMOVE on variable-length edge %q", ErrInvalidEffectTarget, v)
		}
		if _, err := unionProperty(cands, s, v, p, false); err != nil {
			return err
		}
		return nil
	}
	if _, ok := sc.carriedResolvedTypes[v]; ok {
		return fmt.Errorf("%w: REMOVE %s.%s: %q resolves to a projection alias, not an entity binding", ErrInvalidEffectTarget, v, p, v)
	}
	return fmt.Errorf("%w: REMOVE %s.%s: %q not in any Part scope", ErrInvalidEffectTarget, v, p, v)
}

// validateRemoveLabelsEffect is the REMOVE analogue of validateSetLabelsEffect:
// same target discipline, same per-label declaration check.
func validateRemoveLabelsEffect(sc *scope, e query.RemoveLabelsEffect, s schema.Schema) error {
	v := e.TargetVariable()
	if _, ok := sc.nodeTypes[v]; !ok {
		if _, ok := sc.edgeBindings[v]; ok {
			return fmt.Errorf("%w: REMOVE labels on edge binding %q", ErrInvalidEffectTarget, v)
		}
		if _, ok := sc.carriedResolvedTypes[v]; ok {
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
func validateDeleteEffect(sc *scope, e query.DeleteEffect, s schema.Schema) error {
	for _, t := range e.Targets() {
		v := t.Variable
		p := t.Property
		if p == "" {
			if _, ok := sc.nodeTypes[v]; ok {
				continue
			}
			if _, ok := sc.edgeTypes[v]; ok {
				continue
			}
			if _, ok := sc.edgeCands[v]; ok {
				continue
			}
			if _, ok := sc.carriedResolvedTypes[v]; ok {
				return fmt.Errorf("%w: DELETE %s: %q resolves to a projection alias, not an entity binding", ErrInvalidEffectTarget, v, v)
			}
			return fmt.Errorf("%w: DELETE %s: %q not in any Part scope", ErrInvalidEffectTarget, v, v)
		}
		if nt, ok := sc.nodeTypes[v]; ok {
			if _, ok := nt.Properties[p]; !ok {
				return fmt.Errorf("%w: %s.%s", ErrUnknownProperty, v, p)
			}
			continue
		}
		if et, ok := sc.edgeTypes[v]; ok {
			if sc.edgeBindings[v].Hops() != nil {
				return fmt.Errorf("%w: DELETE on variable-length edge %q", ErrInvalidEffectTarget, v)
			}
			if _, ok := et.Properties[p]; !ok {
				return fmt.Errorf("%w: %s.%s", ErrUnknownProperty, v, p)
			}
			continue
		}
		if cands, ok := sc.edgeCands[v]; ok {
			if sc.edgeBindings[v].Hops() != nil {
				return fmt.Errorf("%w: DELETE on variable-length edge %q", ErrInvalidEffectTarget, v)
			}
			if _, err := unionProperty(cands, s, v, p, false); err != nil {
				return err
			}
			continue
		}
		if _, ok := sc.carriedResolvedTypes[v]; ok {
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
