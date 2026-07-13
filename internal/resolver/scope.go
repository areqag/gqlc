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
