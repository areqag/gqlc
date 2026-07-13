package resolver

import (
	"fmt"

	"github.com/areqag/gqlc/internal/procsig"
	"github.com/areqag/gqlc/internal/query"
	"github.com/areqag/gqlc/internal/schema"
)

// scope is the resolver-typed state evolving through one Part's phases
// (spec docs/specs/resolver-branch-scope.md §2.1). Fields are private;
// every mutation runs through a method so the twelve lanes stay
// consistent — a cross-lane shadow on one lane cascades to the mirrors.
//
// Constructor seed order is fixed at newScope: carry-in → live-lane
// tables. The Part shape (bindings, effects, returns, returnsAll) is
// bound once by Ingest and read by every per-phase method off the
// receiver — no phase method takes the Part as a parameter (§2.2 D2
// closure). carriedGroups is on scope as a carry-in lane so
// DemoteNullability is parameter-free (§2.2 D1 closure).
type scope struct {
	// Live tables — written by Phases A/B/C/D via Bind*/CloseEdges/
	// InferUnlabelled/SeedLocalNullability/DemoteNullability.
	nodeTypes       map[string]schema.NodeType
	edgeTypes       map[string]schema.EdgeType
	edgeKeys        map[string]schema.EdgeKey
	edgeCands       map[string][]schema.EdgeKey
	edgeBindings    map[string]query.EdgeBinding
	nullableBinding map[string]bool
	callTypes       map[string]callBindingSlot

	// Ingested Part — set once by Ingest, read by every phase method.
	// Only the fields the phase methods actually consume are captured
	// (part.Distinct is not on scope — computeDistinct walks q.Branches
	// at the top level and no scope method reads it).
	bindings   []query.Binding
	effects    []query.Effect
	returns    []query.ReturnItem
	returnsAll bool

	// Projection-walk outputs — set by ResolveProjections, read by
	// Export (§2.1). Empty until ResolveProjections runs.
	columns    []Column
	items      []query.ReturnItem
	scopeOrder []string

	// Carry-in lanes: seeded from the incoming branchState, read by
	// downstream phases, never written within this Part.
	carriedResolvedTypes map[string]ResolvedType
	carriedOrder         []string
	carriedGroups        map[string]int

	// deferredEdges bridges CloseEdges (Phase A2) → CloseEdgesDeferred
	// (Phase C) across the intervening InferUnlabelled (Phase B) call.
	// Reset to nil at Phase C's end.
	deferredEdges []query.EdgeBinding

	// ingested guards Ingest's single-shot contract (§4.1 test #10).
	ingested bool
}

// newScope seeds a scope from Part K's exported carry — the ten fields
// of branchState. Part 0's carry is the zero-value branchState (nil
// maps everywhere) and the constructor treats it as empty without
// nil-guards at every read; the make calls give every downstream
// phase a non-nil map to write into.
func newScope(carry branchState) *scope {
	s := &scope{
		nodeTypes:            make(map[string]schema.NodeType),
		edgeTypes:            make(map[string]schema.EdgeType),
		edgeKeys:             make(map[string]schema.EdgeKey),
		edgeCands:            make(map[string][]schema.EdgeKey),
		edgeBindings:         make(map[string]query.EdgeBinding),
		nullableBinding:      make(map[string]bool),
		callTypes:            make(map[string]callBindingSlot),
		carriedResolvedTypes: make(map[string]ResolvedType),
		carriedGroups:        make(map[string]int),
	}
	// Carry seed. §4.2.3: carried bindings land in the live lanes so
	// local Phase A1 shadowing (Bind*'s delete cascade) works
	// uniformly on carried and local names. Nullability seeds too:
	// Phase D's local-overrides-carry rule (§4.6) overwrites this.
	for name, nt := range carry.exportedNodeTypes {
		s.nodeTypes[name] = nt
	}
	for name, et := range carry.exportedEdgeTypes {
		s.edgeTypes[name] = et
	}
	for name, k := range carry.exportedEdgeKeys {
		s.edgeKeys[name] = k
	}
	for name, cands := range carry.exportedEdgeCands {
		s.edgeCands[name] = cands
	}
	for name, b := range carry.exportedEdgeBindings {
		s.edgeBindings[name] = b
	}
	for name, nb := range carry.exportedNullableBinding {
		s.nullableBinding[name] = nb
	}
	for name, slot := range carry.exportedCallTypes {
		s.callTypes[name] = slot
	}
	for name, rt := range carry.exportedResolvedTypes {
		s.carriedResolvedTypes[name] = rt
	}
	for name, g := range carry.exportedOptionalGroup {
		s.carriedGroups[name] = g
	}
	s.carriedOrder = carry.exportedOrder
	return s
}

// Ingest binds the current Part into the scope. Called exactly once
// per Part cycle, immediately after newScope. A second call panics —
// a cheap tripwire keeping the phase orchestration honest (§4.1
// test #10).
func (s *scope) Ingest(part query.Part) {
	if s.ingested {
		panic("resolver bug: scope.Ingest called twice")
	}
	s.ingested = true
	s.bindings = part.Bindings
	s.effects = part.Effects
	s.returns = part.Returns
	s.returnsAll = part.ReturnsAll
}

// BindNode admits a labelled NodeBinding — R5 §4.2.3 arm of Phase A1.
// Cascades shadow / delete on the edge, call, and nullable lanes for
// the same variable. Returns ErrPartBindingTypeConflict if a carried
// entry differs in LabelSetKey. R7 §4.1.2.1 call-vs-node shape check
// fires first.
//
// Does NOT append to s.bindings — Ingest owns the ordered list in
// parser order; Bind*'s per-Phase-A1 admission order is different.
func (s *scope) BindNode(nb query.NodeBinding, nt schema.NodeType) error {
	v := nb.Variable()
	// R7 §4.1.2.1: a carried CALL YIELD scalar cannot re-bind as a
	// labelled node — fires BEFORE the R5 arm so the scalar-vs-entity
	// fault is named correctly, not masked by the node-vs-node
	// message.
	if _, seenCall := s.callTypes[v]; seenCall {
		return fmt.Errorf("%w: variable %q carried as CALL YIELD scalar, re-bound as %s", ErrPartBindingTypeConflict, v, nt.Labels)
	}
	// R5 §6.4: a labelled re-binding of a carried name whose schema-
	// typed identity differs from the carry is irreconcilable.
	if prev, seen := s.nodeTypes[v]; seen && prev.Labels != nt.Labels {
		return fmt.Errorf("%w: variable %q carried as %s, re-bound as %s", ErrPartBindingTypeConflict, v, prev.Labels, nt.Labels)
	}
	s.nodeTypes[v] = nt
	// Local binding shadows any carried edge state at the same name;
	// R5 §4.2.3 shadowing rule.
	delete(s.edgeTypes, v)
	delete(s.edgeKeys, v)
	delete(s.edgeCands, v)
	delete(s.edgeBindings, v)
	return nil
}

// BindEdge admits a labelled EdgeBinding — Phase A1's supportedEdges
// arm. Cascades node / call shadow; registers the binding for
// CloseEdges to consume. Returns ErrPartBindingTypeConflict per R5
// §6.4 edge parity and R7 §4.1.2.2.
func (s *scope) BindEdge(eb query.EdgeBinding) error {
	if err := r3EdgeAdmissible(eb); err != nil {
		return err
	}
	v := eb.Variable()
	if v == "" {
		return nil
	}
	// R7 §4.1.2.2: reciprocal call-vs-edge shape-mismatch guard.
	if _, seenCall := s.callTypes[v]; seenCall {
		return fmt.Errorf("%w: variable %q carried as CALL YIELD scalar, re-bound as edge with labels %s", ErrPartBindingTypeConflict, v, eb.Labels().Key())
	}
	// R5 §6.4 edge parity: differing label-set key vs. carry is a
	// Part-cross irreconcilable re-typing.
	if prev, seen := s.edgeBindings[v]; seen && prev.Labels().Key() != eb.Labels().Key() {
		return fmt.Errorf("%w: variable %q carried as edge with labels %s, re-bound with labels %s", ErrPartBindingTypeConflict, v, prev.Labels().Key(), eb.Labels().Key())
	}
	s.edgeBindings[v] = eb
	// Edge shadows any carried node state.
	delete(s.nodeTypes, v)
	// Local edge re-bind resets any carried closed-edge state for v —
	// Phase A2/C's closeEdge is authoritative for the new binding's
	// endpoints, which may differ from the carry's.
	delete(s.edgeTypes, v)
	delete(s.edgeKeys, v)
	delete(s.edgeCands, v)
	return nil
}

// BindCall admits a CallBinding — R7 §4.1. Cascades node / edge
// shadow; runs the 0ig arg-assignability check against r.
// ErrPartBindingTypeConflict on a same-Part duplicate.
func (s *scope) BindCall(cb query.CallBinding, r procsig.Registry) error {
	v := cb.Variable()
	// R7 §4.1: local CallBinding shadows any carried entity state at
	// the same name (parser-unreachable belt-and-braces since
	// build.go's imported[v] check rejects the collision at parse).
	delete(s.nodeTypes, v)
	delete(s.edgeTypes, v)
	delete(s.edgeKeys, v)
	delete(s.edgeCands, v)
	delete(s.edgeBindings, v)
	// Same-Part duplicate CallBinding variable is grammar-impossible;
	// defensive tripwire.
	if _, seen := s.callTypes[v]; seen {
		return fmt.Errorf("%w: variable %q re-CALL-bound in single part", ErrPartBindingTypeConflict, v)
	}
	// 0ig arg-site assignability. Multiple CallBindings from one CALL
	// share the same args slice header (parser §4.3.1), so re-check is
	// a no-op on the second occurrence.
	//
	// Registry miss + arity mismatch are parser-authoritative
	// pre-conditions (spec §4.4 trust posture); they surface as
	// non-sentinel errors so a drift bug is loud but does not pollute
	// ErrCallArgAssignability's fixture semantics.
	if args := cb.Args(); len(args) > 0 {
		sig, ok := r.Lookup(cb.Procedure())
		if !ok {
			return fmt.Errorf("resolver: procedure %q missing from registry (parser drift)", cb.Procedure())
		}
		if len(args) != len(sig.Params) {
			return fmt.Errorf("resolver: procedure %q expects %d arguments, got %d (parser drift)", cb.Procedure(), len(sig.Params), len(args))
		}
		for i, a := range args {
			if !argAssignable(sig.Params[i].Token, a.Type()) {
				return fmt.Errorf("%w: procedure %q argument %d: cannot assign %s to %s", ErrCallArgAssignability, cb.Procedure(), i, a.Type().String(), sig.Params[i].Token)
			}
		}
	}
	s.callTypes[v] = callBindingSlot{
		resultType:  cb.ResultType(),
		nullable:    cb.Nullable(),
		procedure:   cb.Procedure(),
		sourceField: cb.SourceField(),
	}
	return nil
}

// CloseEdges runs Phases A2 + C: try every edge admitted this Part;
// defer unfulfilled ones to Phase B; retry after InferUnlabelled fills
// the node types. Reads s.bindings for the edge list and s.nodeTypes
// for endpoint lookups. Writes edgeTypes / edgeKeys / edgeCands via
// the free-function closeEdge helper.
//
// A2 and C were two separate loops in resolvePart. Absorbing both
// here removes the pendingNodes / deferredEdges / supportedEdges
// locals from resolvePart entirely.
func (s *scope) CloseEdges(sch schema.Schema) error {
	deferredEdges := make([]query.EdgeBinding, 0, len(s.bindings))
	for _, b := range s.bindings {
		eb, ok := b.(query.EdgeBinding)
		if !ok {
			continue
		}
		src, srcOK := endpointLabels(eb.Source(), s.nodeTypes)
		tgt, tgtOK := endpointLabels(eb.Target(), s.nodeTypes)
		if !srcOK || !tgtOK {
			deferredEdges = append(deferredEdges, eb)
			continue
		}
		if err := closeEdge(eb, src, tgt, sch, s.edgeTypes, s.edgeKeys, s.edgeCands); err != nil {
			return err
		}
	}
	// Phase C is a re-run of A2 against the post-Phase-B node table.
	// resolvePart used to call InferUnlabelled between A2 and C; the
	// caller still does — CloseEdgesDeferred picks up here after
	// InferUnlabelled has committed the unlabelled types.
	s.deferredEdges = deferredEdges
	return nil
}

// CloseEdgesDeferred runs Phase C: retries the edges CloseEdges
// (Phase A2) deferred, now that InferUnlabelled has populated the
// node types they were waiting on. An endpoint still missing here is
// an ErrUnknownLabel.
func (s *scope) CloseEdgesDeferred(sch schema.Schema) error {
	for _, eb := range s.deferredEdges {
		src, srcOK := endpointLabels(eb.Source(), s.nodeTypes)
		tgt, tgtOK := endpointLabels(eb.Target(), s.nodeTypes)
		if !srcOK {
			return fmt.Errorf("%w: cannot infer type of source endpoint of edge %q", ErrUnknownLabel, eb.Variable())
		}
		if !tgtOK {
			return fmt.Errorf("%w: cannot infer type of target endpoint of edge %q", ErrUnknownLabel, eb.Variable())
		}
		if err := closeEdge(eb, src, tgt, sch, s.edgeTypes, s.edgeKeys, s.edgeCands); err != nil {
			return err
		}
	}
	s.deferredEdges = nil
	return nil
}

// InferUnlabelled runs Phase B against s.bindings' pending unlabelled
// nodes (bindings with no labels). Writes inferred entries directly
// into nodeTypes via the free-function inferUnlabelled helper; R7
// §4.1.2.1 call-collision guard preserved.
func (s *scope) InferUnlabelled(sch schema.Schema) error {
	var pending []query.NodeBinding
	var edges []query.EdgeBinding
	for _, b := range s.bindings {
		switch bb := b.(type) {
		case query.NodeBinding:
			if len(bb.Labels()) == 0 {
				pending = append(pending, bb)
			}
		case query.EdgeBinding:
			// r3EdgeAdmissible check already ran at BindEdge; any
			// edge reaching InferUnlabelled has passed it.
			if r3EdgeAdmissible(bb) == nil {
				edges = append(edges, bb)
			}
		}
	}
	if len(pending) == 0 {
		return nil
	}
	return inferUnlabelled(pending, edges, sch, s.nodeTypes, s.callTypes)
}

// HasNode / HasEdge / HasCall are read-only presence predicates for
// callers that need to know whether a name is currently in a live
// lane without reaching for the map itself (§2.4). Used by tests and
// (once step 6 lands) by the parameter walker's Contains path.

func (s *scope) HasNode(v string) bool {
	_, ok := s.nodeTypes[v]
	return ok
}

func (s *scope) HasEdge(v string) bool {
	if _, ok := s.edgeTypes[v]; ok {
		return true
	}
	_, ok := s.edgeCands[v]
	return ok
}

func (s *scope) HasCall(v string) bool {
	_, ok := s.callTypes[v]
	return ok
}

// SeedLocalNullability writes each binding's own Nullable() bit into the
// nullable lane, overwriting any carry entry per §4.6 ("local overrides
// carry"). Reads s.bindings; writes s.nullableBinding. Anonymous
// bindings (v == "") skip. Runs before DemoteNullability so the fixed
// point sees the local-authoritative baseline.
func (s *scope) SeedLocalNullability() {
	for _, b := range s.bindings {
		v, ok := bindingVariable(b)
		if !ok || v == "" {
			continue
		}
		s.nullableBinding[v] = b.Nullable()
	}
}

// DemoteNullability runs the ay9+5xg-widened regime-(a) demotion in
// place: 5xg pre-pass (a required bare re-reference witnesses the
// re-referenced binding, flipping its table entry), plus ay9 pre-pass
// (OPTIONAL-group membership scan seeded from s.carriedGroups and
// unioned with local declarations, so any one group being proven
// demotes every member), plus the edge-driven fixed point (§4.4). The
// 5xg pre-pass runs before the group-closure fixed point and does not
// touch demotedGroups; the two demotion channels are orthogonal (both
// write false to the same table, both are monotone, composition is
// order-independent). The subsequent fixed-point loop may observe
// 5xg's flipped entries and demote co-introduced siblings via (iv),
// producing the compose-with-group cascade §8.4 fixture 4 witnesses.
//
// s.carriedGroups seeds the group-membership maps from the carry: a
// WITH-carried binding retains its Part-K OPTIONAL-group id, so
// proving any member in Part K+1 pulls its cross-Part siblings via
// the same fixed point. Group ids are per-query and unique across the
// whole parse (§3.3), so carried and local ids share the same numeric
// space without collision. A local binding at the same name that
// carries a distinct local group id overrides the carried id — local
// shadows carry, matching the SeedLocalNullability discipline.
//
// Reads s.bindings and s.carriedGroups; writes s.nullableBinding.
// Parameter-free per §2.2 D1.
func (s *scope) DemoteNullability() {
	// 5xg pre-pass: bare-ref demotion. A binding whose parser-time
	// flag is true was re-referenced in a required bare pattern; the
	// row-drop witness demotes it. Anonymous bindings (v == "") skip
	// — they carry no table entry.
	for _, b := range s.bindings {
		switch bb := b.(type) {
		case query.NodeBinding:
			if bb.ReferencedInRequiredBarePattern() && bb.Variable() != "" {
				if _, present := s.nullableBinding[bb.Variable()]; present {
					s.nullableBinding[bb.Variable()] = false
				}
			}
		case query.EdgeBinding:
			if bb.ReferencedInRequiredBarePattern() && bb.Variable() != "" {
				if _, present := s.nullableBinding[bb.Variable()]; present {
					s.nullableBinding[bb.Variable()] = false
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
	members := map[int][]string{}  // group id → named members
	groupsOf := map[string][]int{} // named member → group ids (may span carry + local)
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
	for name, g := range s.carriedGroups {
		addMember(name, g)
	}
	for _, b := range s.bindings {
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
			if _, present := s.nullableBinding[m]; present {
				s.nullableBinding[m] = false
			}
		}
		return true
	}
	// A carried binding whose local Nullable() entry in the table is
	// already false (either from SeedLocalNullability's re-MATCH
	// override or from the 5xg pre-pass) is a proven witness for its
	// carried group. Fire that closure before the edge-driven fixed
	// point so a carried group without a local edge witness still
	// demotes. Map iteration order is unobservable here: demoteGroup
	// writes false idempotently to each member's table entry, so any
	// visit order converges to the same fixed point.
	for name, gs := range groupsOf {
		if nb, present := s.nullableBinding[name]; present && !nb {
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
		for _, b := range s.bindings {
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
				if nb, present := s.nullableBinding[v]; present && nb {
					s.nullableBinding[v] = false
					changed = true
				}
				if demoteGroupsOf(v) {
					changed = true
				}
			}
		}
	}
}

// ResolveProjections runs the full §4.4 projection walk end-to-end:
// builds s.scopeOrder (§4.4.1), materialises s.items (RETURN * / WITH
// * expansion at §4.4.2, or verbatim s.returns), types each item via
// projectionType / refProjectionType into s.columns. GroupingKey stays
// false — fillGroupingKeys is called by resolveBranch on the final
// Part only.
//
// After a successful call, s.scopeOrder / s.items / s.columns are the
// authoritative outputs Export reads (§2.1). Reads s.bindings /
// s.carriedOrder / s.nodeTypes / s.edgeBindings / s.callTypes /
// s.returns / s.returnsAll / s.carriedResolvedTypes; writes the three
// projection-output fields. First error short-circuits.
func (s *scope) ResolveProjections(sch schema.Schema) error {
	s.scopeOrder = s.buildScopeOrder()

	items, err := s.materialiseReturns()
	if err != nil {
		return err
	}
	s.items = items

	s.columns = make([]Column, 0, len(items))
	for _, item := range items {
		colType, err := s.projectionType(item.Value, sch)
		if err != nil {
			return err
		}
		s.columns = append(s.columns, Column{Name: item.Name, Type: colType})
	}
	return nil
}

// buildScopeOrder computes the deterministic order for RETURN * / WITH *
// expansion (§4.4.1): local s.bindings in first-appearance order (named
// only), then s.carriedOrder names not covered by local, in carry order.
// Also serves as the deterministic export order for a non-ReturnsAll
// WITH. R7 §4.3 widens the walk to include CALL YIELD variables so
// standalone-CALL Parts (parser Stage 14 §4.3 ReturnsAll=true)
// synthesise their column list.
func (s *scope) buildScopeOrder() []string {
	seen := make(map[string]bool)
	out := make([]string, 0, len(s.bindings)+len(s.carriedOrder))
	for _, b := range s.bindings {
		v, ok := bindingVariable(b)
		if !ok || v == "" || seen[v] {
			continue
		}
		// Only include names that actually resolved (Phase A/B/C committed).
		// Unresolved names are impossible at this point — Phase C either
		// resolved or short-circuited — but the guard keeps the invariant
		// tight.
		if _, isNode := s.nodeTypes[v]; isNode {
			seen[v] = true
			out = append(out, v)
			continue
		}
		if _, isEdge := s.edgeBindings[v]; isEdge {
			seen[v] = true
			out = append(out, v)
			continue
		}
		if _, isCall := s.callTypes[v]; isCall {
			seen[v] = true
			out = append(out, v)
		}
	}
	for _, v := range s.carriedOrder {
		if seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

// materialiseReturns handles the RETURN * / WITH * expansion (§4.4).
// When s.returnsAll is false, returns s.returns unchanged. When true,
// builds the virtual ReturnItem sequence over s.scopeOrder (§4.4.2)
// — one item per in-scope name in own-Part-first, shadowing-dedup
// order. R7 threads s.callTypes so CALL YIELD variables synthesise a
// properly-typed RefProjection (§4.7).
func (s *scope) materialiseReturns() ([]query.ReturnItem, error) {
	if !s.returnsAll {
		return s.returns, nil
	}
	// Empty in-scope set → empty column list (§4.4.3). Legal shape.
	if len(s.scopeOrder) == 0 {
		return nil, nil
	}
	items := make([]query.ReturnItem, 0, len(s.scopeOrder))
	for _, v := range s.scopeOrder {
		val, err := s.virtualProjection(v)
		if err != nil {
			return nil, err
		}
		items = append(items, query.ReturnItem{Name: v, Value: val})
	}
	return items, nil
}

// virtualProjection constructs the RefProjection (or carried-alias
// Value) §4.4.2 assigns to a wildcard-expanded name. R7 §4.7: the
// callTypes lane (appended at the tail) synthesises a CALL YIELD
// variable's RefProjection with the CallBinding's bridged ResultType.
func (s *scope) virtualProjection(name string) (query.Projection, error) {
	if _, ok := s.nodeTypes[name]; ok {
		return query.NewRefProjection(query.Ref{Variable: name}, query.TypeNode{}), nil
	}
	if b, ok := s.edgeBindings[name]; ok {
		if b.Hops() != nil {
			return query.NewRefProjection(query.Ref{Variable: name}, query.TypeList{}), nil
		}
		return query.NewRefProjection(query.Ref{Variable: name}, query.TypeEdge{}), nil
	}
	if slot, ok := s.callTypes[name]; ok {
		return query.NewRefProjection(query.Ref{Variable: name}, slot.resultType), nil
	}
	// Not a binding — must be a projection-alias carried through WITH; the
	// §4.5.4 bypass path serves it. Use a placeholder RefProjection whose
	// Value.Type() the walker will consult via the carried-resolved-types map.
	if _, ok := s.carriedResolvedTypes[name]; ok {
		return query.NewRefProjection(query.Ref{Variable: name}, query.TypeUnknown{}), nil
	}
	// A name in scopeOrder that resolves to nothing is a resolver-side bug —
	// the scope builder must not put such names in the list.
	return nil, fmt.Errorf("%w: wildcard-expanded name %q resolves to no binding or carry", ErrOutOfR0Scope, name)
}

// projectionType dispatches a Projection to its handler and returns
// the column's resolved type. R5 admits AggregateProjection (§4.5);
// carried-alias RefProjections (§4.5.4) route through
// refProjectionType against s.carriedResolvedTypes.
func (s *scope) projectionType(p query.Projection, sch schema.Schema) (ResolvedType, error) {
	switch pp := p.(type) {
	case query.RefProjection:
		return s.refProjectionType(pp.Ref(), sch)
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

// refProjectionType dispatches a RefProjection's Ref against the
// resolved node and edge binding tables. §4.5.4 adds the carried-alias
// bypass — when a name lives ONLY in s.carriedResolvedTypes (e.g.
// `WITH count(n) AS c` seen downstream), refProjectionType returns
// the carried type directly. R7 §4.2 adds the callTypes lane BEFORE
// the carried-alias bypass: a bare Ref against a CALL YIELD variable
// bridges to ResolvedProperty (or ResolvedUnknown for NUMBER); a
// property lookup on a CALL YIELD variable fires ErrUnknownProperty
// with a widened message set.
func (s *scope) refProjectionType(ref query.Ref, sch schema.Schema) (ResolvedType, error) {
	if nt, ok := s.nodeTypes[ref.Variable]; ok {
		if ref.Property == "" {
			return ResolvedNode{Labels: nt.Labels, Nullable: s.nullableBinding[ref.Variable]}, nil
		}
		prop, ok := nt.Properties[ref.Property]
		if !ok {
			return nil, fmt.Errorf("%w: %s.%s", ErrUnknownProperty, ref.Variable, ref.Property)
		}
		return ResolvedProperty{Type: prop.Type, Nullable: prop.Nullable || s.nullableBinding[ref.Variable]}, nil
	}
	_, singleCand := s.edgeTypes[ref.Variable]
	cands, multiCand := s.edgeCands[ref.Variable]
	if !singleCand && !multiCand {
		// R7 §4.2 — CALL YIELD lane, fired BEFORE the carried-alias bypass.
		if slot, ok := s.callTypes[ref.Variable]; ok {
			return callProjectionType(slot, ref, s.nullableBinding)
		}
		// §4.5.4 — carried-alias bypass. A RefProjection whose Variable lives
		// only in carriedResolvedTypes yields the carried type verbatim
		// (property lookups on a carried alias are unreachable — parser scope
		// check rejects Ref{"c", "p"} unless c is a binding-name in scope).
		if rt, ok := s.carriedResolvedTypes[ref.Variable]; ok && ref.Property == "" {
			return rt, nil
		}
		return nil, fmt.Errorf("%w: %s", ErrOutOfR0Scope, ref.Variable)
	}

	binding := s.edgeBindings[ref.Variable]
	varLength := binding.Hops() != nil
	edgeNullable := s.nullableBinding[ref.Variable]

	if ref.Property == "" {
		var element ResolvedType
		if singleCand {
			element = ResolvedEdge{EdgeKey: s.edgeKeys[ref.Variable], Nullable: edgeNullable}
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
		et := s.edgeTypes[ref.Variable]
		prop, ok := et.Properties[ref.Property]
		if !ok {
			return nil, fmt.Errorf("%w: %s.%s", ErrUnknownProperty, ref.Variable, ref.Property)
		}
		return ResolvedProperty{Type: prop.Type, Nullable: prop.Nullable || edgeNullable}, nil
	}
	return unionProperty(cands, sch, ref.Variable, ref.Property, edgeNullable)
}

// ValidateEffects is R6 Phase E: walk s.effects in slice order,
// dispatch each Effect through its per-variant validator, short-
// circuit on first failure. Reads from the schema-committed binding
// tables and the carried resolved types on the receiver; never mutates
// them. Single public entry; the seven per-variant validators are
// unexported package-level helpers taking *scope (§2.2 §5 step 3).
func (s *scope) ValidateEffects(sch schema.Schema) error {
	for _, e := range s.effects {
		if err := validateEffect(s, e, sch); err != nil {
			return err
		}
	}
	return nil
}

// Snapshot returns the parameter-witness partScope this Part
// contributes: a deep copy of the five witness lanes (nodeTypes,
// edgeTypes, edgeCands, edgeBindings, nullableBinding). Called once
// per Part after Phase D. §2.3 invariant #3 pins the narrowing —
// carry-only lanes and callTypes are NOT observable through
// partScope.
func (s *scope) Snapshot() partScope {
	sc := partScope{
		nodeTypes:       make(map[string]schema.NodeType, len(s.nodeTypes)),
		edgeTypes:       make(map[string]schema.EdgeType, len(s.edgeTypes)),
		edgeCands:       make(map[string][]schema.EdgeKey, len(s.edgeCands)),
		edgeBindings:    make(map[string]query.EdgeBinding, len(s.edgeBindings)),
		nullableBinding: make(map[string]bool, len(s.nullableBinding)),
	}
	for k, v := range s.nodeTypes {
		sc.nodeTypes[k] = v
	}
	for k, v := range s.edgeTypes {
		sc.edgeTypes[k] = v
	}
	for k, v := range s.edgeCands {
		sc.edgeCands[k] = v
	}
	for k, v := range s.edgeBindings {
		sc.edgeBindings[k] = v
	}
	for k, v := range s.nullableBinding {
		sc.nullableBinding[k] = v
	}
	return sc
}
