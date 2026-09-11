package resolver

import (
	"fmt"

	"github.com/areqag/gqlc/internal/graph"
	"github.com/areqag/gqlc/internal/procsig"
	"github.com/areqag/gqlc/internal/query"
	"github.com/areqag/gqlc/internal/schema"
)

// scope is the resolver-typed state evolving through one Part's phases
// (spec docs/specs/resolver-branch-scope.md §2.1). Fields are private;
// every mutation runs through a method so the thirteen lanes stay
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
	nodeTypes map[string]schema.NodeType
	nodeCands map[string][]schema.NodeType
	// pluralByInference holds the names Phase B's plural commitment put in
	// nodeCands — as against BindNodeCands, which puts a LABELLED binding's
	// plural satisfying set there. That is the axis errors.go selects the
	// whole-entity refusal's sentinel on (ErrAmbiguousLabel for a label set,
	// ErrAmbiguousBinding for an unlabelled binding), and it is not
	// recoverable from nodeCands, whose values are the same []schema.NodeType
	// either way.
	//
	// Two writers: BindNodeCands' Phase B counterpart, and the carry seed.
	// BindNodeCands still does NOT write a false here, and that remains
	// unreachable — a name cannot be in the lane before BindNodeCands runs,
	// since Phase A1 precedes Phase B within a Part, so the ordering that
	// would let an inference entry be inherited by a later labelled re-bind
	// does not occur and the zero value is already the answer.
	//
	// The carry seed is live as of gqlc-etj6. It used to be unreachable for a
	// stated reason that no longer holds: Export records a nodeCands entry
	// only for a bare `WITH v` (alias equal to the variable, no property), and
	// a bare whole-entity projection of a plural binding was refused in that
	// very Part, so the resolve stopped before any downstream Part read it.
	// ResolveProjections now defers that refusal in a non-final Part
	// (carriesPluralBinding), which is precisely what makes a carried plural
	// name possible — and it has to bring this lane with it, because the
	// sentinel is not recoverable from nodeCands.
	//
	// Nothing deletes from the lane. The narrowing's shrink rewrites a name
	// already in it, and shrinking a candidate set does not change how the
	// binding was written; the narrowing's collapse and Bind*'s shadow cascade
	// both take the name out of nodeCands, and refProjectionType asks this
	// lane only from inside the nodeCands arm.
	pluralByInference map[string]bool
	resolvedCovers    map[string]struct{}
	edgeTypes         map[string]schema.EdgeType
	edgeKeys          map[string]schema.EdgeKey
	edgeCands         map[string][]schema.EdgeKey
	edgeBindings      map[string]query.EdgeBinding
	nullableBinding   map[string]bool
	// demotedGroups is DemoteNullability's ay9 group-closure result, kept
	// because NarrowPluralEndpoints asks the same question of the same shape:
	// an OPTIONAL edge whose group is proven IS a witness about its endpoints,
	// and the two guards must not drift (bd gqlc-o8oc). Nil until
	// DemoteNullability has run, and a nil map reads false for every id, which
	// is the pre-refinement answer.
	demotedGroups map[int]bool
	callTypes     map[string]callBindingSlot

	// orientation is CloseEdges' diagnostic by-product: which relationship
	// types the closure kept and which it dropped in the wrong-orientation
	// shape. It is a lane rather than a CloseEdges return value so the phase
	// methods keep their signatures; resolvePart reads it after the close.
	orientation orientationEvidence

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

	// ingested guards Ingest's single-shot contract (§4.1 test #10).
	ingested bool
}

// newScope seeds a scope from Part K's exported carry — the twelve fields
// of branchState. Part 0's carry is the zero-value branchState (nil
// maps everywhere) and the constructor treats it as empty without
// nil-guards at every read; the make calls give every downstream
// phase a non-nil map to write into.
func newScope(carry branchState) *scope {
	s := &scope{
		nodeTypes:            make(map[string]schema.NodeType),
		nodeCands:            make(map[string][]schema.NodeType),
		pluralByInference:    make(map[string]bool),
		resolvedCovers:       make(map[string]struct{}),
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
	// resolvedCovers is NOT seeded. branchState carries the type, not how Part
	// K arrived at it, so a carried singular node type is a commitment this
	// Part cannot see the provenance of — and Part K's Phase B inference is one
	// of the things it can be. Leaving it uncovered means the narrowing does
	// not learn from a carried singular endpoint, which lands on the
	// pre-narrowing answer. The cost is precision on
	// `MATCH (a:Only) WITH a MATCH (p:Plural)-[r]->(a)`.
	for name, nt := range carry.exportedNodeTypes {
		s.nodeTypes[name] = nt
	}
	for name, nts := range carry.exportedNodeCands {
		s.nodeCands[name] = nts
	}
	for name, byInference := range carry.exportedPluralByInference {
		s.pluralByInference[name] = byInference
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

// ValidateCarriedKinds is the cross-lane kind check Phase A1 owes, and it runs
// before any local admission: a Part may re-declare a carried name as a node or
// edge pattern only if the carry bound that same KIND of entity there
// (bd gqlc-60jb).
//
// The defect it closes is not that either lane was individually wrong. It is
// that carriedResolvedTypes and the binding tables were never asked whether they
// disagree about a name, so `MATCH (p:Person) WITH count(p) AS c` followed by
// `MATCH (c)-[a:AUTHORED]->(y:Post)` was ACCEPTED and typed `c` as a Post node —
// an INTEGER handed to the caller behind a node decoder.
//
// Measured on master, every one of these accepted: an unlabelled `(c)` under a
// required MATCH and under an OPTIONAL MATCH, a labelled `(c:Post)`, an edge
// `-[c:AUTHORED]->`, and the same four with the carry spelled `WITH p.name AS c`
// or `WITH collect(p.name) AS c` rather than as an aggregate. The labelled and
// edge spellings never reach Phase B at all, which is why this is not at the
// inference site: a fix there closes half of it. It is not in
// cypher.mergeBinding either — that dedups WITHIN one Part and cannot see the
// carry, so it is structurally unable to ask the question.
//
// The comparison is KIND AGREEMENT rather than "both sides are entities", and
// the difference is a second shape of the same fault: a carried edge
// re-declared as `(r)` was accepted too, typing the AUTHORED hop it then
// anchored as though `r` were a Post. Widening from the first form to this one
// costs nothing on the corpus — swept over every fixture x schema cell, the two
// forms move exactly the same set, which is one fixture.
//
// A name in callTypes is skipped. The R7 4.1.2.1 / 4.1.2.2 shape checks in
// BindNode / BindNodeCands / BindEdge and the two in commitUnlabelledRound
// already refuse every spelling for those, and they name the fault more
// precisely than this one can ("carried as CALL YIELD scalar"). Running first
// and unconditionally would replace five pinned messages with a vaguer one for
// no soundness gain.
func (s *scope) ValidateCarriedKinds() error {
	for _, b := range s.bindings {
		v, declared, ok := declaredEntityKind(b)
		if !ok || v == "" {
			continue
		}
		if _, seenCall := s.callTypes[v]; seenCall {
			continue
		}
		rt, carried := s.carriedResolvedTypes[v]
		if !carried || s.carryBoundKindAt(v, b.Kind()) {
			continue
		}
		return fmt.Errorf("%w: variable %q carried as %s, re-declared as %s", ErrPartBindingTypeConflict, v, rt, declared)
	}
	return nil
}

// carryBoundKindAt reports whether the carry bound an entity of `kind` at v —
// the second of the two lanes ValidateCarriedKinds compares.
//
// It reads the binding TABLES rather than switching on carriedResolvedTypes'
// variant, and the corpus is why. `valid/edge_rebind_var_length_vs_fixed.cypher`
// carries a `[r:KNOWS*1..3]` and re-declares `r` as a fixed `[r:KNOWS]`: the
// carried COLUMN type is ResolvedList, because a var-length hop projects a list,
// while the carried BINDING is an edge. A variant switch reads that pair as a
// list wearing an edge's clothes and refuses a fixture the resolver has always
// accepted — measured, 18 accept-to-refuse cells. The tables answer the question
// the guard actually asks, and they answer it right for the var-length hop.
//
// Every lane newScope seeds from a binding table is listed, split by the kind it
// stands for. It is a read-only method over lanes no local Bind* has touched
// yet, since ValidateCarriedKinds runs before the admission loop. callTypes is
// not here: its names are skipped by the caller for the reason given there.
func (s *scope) carryBoundKindAt(v string, kind query.BindingKind) bool {
	if kind == query.BindingNode {
		if _, ok := s.nodeTypes[v]; ok {
			return true
		}
		_, ok := s.nodeCands[v]
		return ok
	}
	if _, ok := s.edgeBindings[v]; ok {
		return true
	}
	if _, ok := s.edgeTypes[v]; ok {
		return true
	}
	if _, ok := s.edgeKeys[v]; ok {
		return true
	}
	_, ok := s.edgeCands[v]
	return ok
}

// declaredEntityKind names the entity a binding spells, or reports false for a
// binding that is not an entity pattern at all. Path / unwind / call bindings
// are out: this guard is about a carried value wearing an entity's clothes, and
// only a node or edge pattern puts them on. (ErrOutOfR0Scope in
// admitLocalBindings is what answers for the rest.)
//
// Its two live arms are the only BindingKinds carryBoundKindAt can be handed,
// so that function's `kind == BindingNode` test is total against what reaches
// it rather than a two-way split over a five-member enum.
func declaredEntityKind(b query.Binding) (variable, declared string, ok bool) {
	switch bb := b.(type) {
	case query.NodeBinding:
		return bb.Variable(), "a node pattern", true
	case query.EdgeBinding:
		return bb.Variable(), "an edge pattern", true
	default:
		return "", "", false
	}
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
		return fmt.Errorf("%w: variable %q carried as CALL YIELD scalar, re-bound as %s", ErrPartBindingTypeConflict, v, nt.KeyLabels)
	}
	// R5 §6.4: a labelled re-binding of a carried name whose schema-
	// typed identity differs from the carry is irreconcilable.
	if prev, seen := s.nodeTypes[v]; seen && prev.KeyLabels != nt.KeyLabels {
		return fmt.Errorf("%w: variable %q carried as %s, re-bound as %s", ErrPartBindingTypeConflict, v, prev.KeyLabels, nt.KeyLabels)
	}
	// A binding carried as plural cannot be re-bound as singular.
	if _, wasCands := s.nodeCands[v]; wasCands {
		return fmt.Errorf("%w: variable %q carried as plural node types, re-bound as singular %s", ErrPartBindingTypeConflict, v, nt.KeyLabels)
	}
	s.nodeTypes[v] = nt
	// nt came from resolveNodeLabels: it IS the set of declared types
	// satisfying the labels this binding spells, and that set has one member.
	// So the entry covers every key a matching row can put at this binding —
	// see nodeTable.resolvedCovers.
	s.resolvedCovers[v] = struct{}{}
	// Local binding shadows any carried edge state at the same name;
	// R5 §4.2.3 shadowing rule.
	delete(s.edgeTypes, v)
	delete(s.edgeKeys, v)
	delete(s.edgeCands, v)
	delete(s.edgeBindings, v)
	return nil
}

// BindNodeCands admits a labelled NodeBinding that satisfies multiple declared
// node types (plural satisfaction, ADR 0022). Property projections resolve via
// intersection; whole-entity references are rejected at projection time.
func (s *scope) BindNodeCands(nb query.NodeBinding, nts []schema.NodeType) error {
	v := nb.Variable()
	if _, seenCall := s.callTypes[v]; seenCall {
		return fmt.Errorf("%w: variable %q carried as CALL YIELD scalar, re-bound as plural node", ErrPartBindingTypeConflict, v)
	}
	if _, wasSingular := s.nodeTypes[v]; wasSingular {
		return fmt.Errorf("%w: variable %q carried as singular node type, re-bound as plural", ErrPartBindingTypeConflict, v)
	}
	if prevCands, seen := s.nodeCands[v]; seen {
		// Consistent re-bind: same cardinality and same key labels in the same order.
		if len(nts) != len(prevCands) {
			return fmt.Errorf("%w: variable %q plural re-bind cardinality changed (%d -> %d)", ErrPartBindingTypeConflict, v, len(prevCands), len(nts))
		}
		for i := range nts {
			if nts[i].KeyLabels != prevCands[i].KeyLabels {
				return fmt.Errorf("%w: variable %q carried as %s, re-bound as %s", ErrPartBindingTypeConflict, v, prevCands[i].KeyLabels, nts[i].KeyLabels)
			}
		}
	}
	s.nodeCands[v] = nts
	// A cands entry is a satisfying set by construction, so it needs no
	// resolvedCovers mark; the lane qualifies `resolved` only. The delete keeps
	// the two lanes from ever both claiming v: the singular arm above already
	// refuses a v in s.nodeTypes, so the lane holds no mark for v on entry, and
	// the delete keeps it that way however the scope was seeded.
	// TestScopeBindNodeCandsClearsASeededResolvedCoversMark pins it from a
	// seeded scope.
	delete(s.resolvedCovers, v)
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
	// Edge shadows any carried node state. The resolvedCovers delete keeps the
	// lane from outliving the `resolved` entry it qualifies: the only node
	// state a shadow can find at v is the carry's, newScope does not seed the
	// lane from the carry, and a same-Part BindNode at v never reaches here —
	// the PARSER refuses that query, with `variable bound with conflicting
	// kinds` from cypher.mergeBinding, so no such scope is ever built.
	// (Nothing above this cascade looks at s.nodeTypes: the checks there are
	// the callTypes shape check and the edge-label parity check.)
	// TestScopeBindEdgeClearsASeededResolvedCoversMark pins it from a seeded
	// scope: the delete is what stops a stale mark from making an
	// edge-shadowed name read as a covering endpoint.
	delete(s.nodeTypes, v)
	delete(s.nodeCands, v)
	delete(s.resolvedCovers, v)
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
	// the same name (belt-and-braces: build.go's imported[v] check rejects the
	// collision at parse).
	//
	// The resolvedCovers delete keeps the lane from outliving the `resolved`
	// entry it qualifies, however the scope was seeded.
	// TestScopeBindCallClearsASeededResolvedCoversMark pins it from a seeded
	// scope.
	delete(s.nodeTypes, v)
	delete(s.nodeCands, v)
	delete(s.resolvedCovers, v)
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
		resultType: cb.ResultType(),
	}
	return nil
}

// nodeTable is the three node lanes endpointLabels reads, by reference. Every
// caller goes through this rather than passing s.nodeTypes and s.nodeCands
// positionally, so a lane cannot be left behind at one call site: the third one
// is the provenance bit, and dropping it is what makes a Phase B commitment
// read as a set of satisfying types.
func (s *scope) nodeTable() nodeTable {
	return nodeTable{resolved: s.nodeTypes, cands: s.nodeCands, pluralByInference: s.pluralByInference, resolvedCovers: s.resolvedCovers}
}

// CloseEdges runs Phases A2 + B + C scope-internally: try every edge
// admitted this Part (A2); self-call InferUnlabelled to fill unlabelled
// node types (B); retry the deferred edges against the post-B node
// table (C). Reads s.bindings for the edge list and s.nodeTypes for
// endpoint lookups. Writes edgeTypes / edgeKeys / edgeCands via the
// free-function closeEdge helper. An endpoint still missing after C
// is an ErrUnknownLabel.
func (s *scope) CloseEdges(sch schema.Schema) error {
	var deferred []query.EdgeBinding
	for _, b := range s.bindings {
		eb, ok := b.(query.EdgeBinding)
		if !ok {
			continue
		}
		src, srcOK := endpointLabels(eb.Source(), s.nodeTable(), sch)
		tgt, tgtOK := endpointLabels(eb.Target(), s.nodeTable(), sch)
		if !srcOK || !tgtOK {
			deferred = append(deferred, eb)
			continue
		}
		// declared(), not covering(): the close only needs each probed key to
		// be declared, and refusing an uncovered endpoint here would turn
		// queries master resolves into ErrUnknownEdge.
		if err := closeEdge(eb, src.declared(), tgt.declared(), sch, s.edgeTypes, s.edgeKeys, s.edgeCands); err != nil {
			return err
		}
		s.orientation.recordEdge(eb, src, tgt, sch)
	}
	if err := s.InferUnlabelled(sch); err != nil {
		return err
	}
	for _, eb := range deferred {
		src, srcOK := endpointLabels(eb.Source(), s.nodeTable(), sch)
		tgt, tgtOK := endpointLabels(eb.Target(), s.nodeTable(), sch)
		switch {
		case !srcOK:
			return fmt.Errorf("%w: cannot infer type of source endpoint of %s", ErrUnknownLabel, describeEdgeBinding(eb))
		case !tgtOK:
			return fmt.Errorf("%w: cannot infer type of target endpoint of %s", ErrUnknownLabel, describeEdgeBinding(eb))
		}
		if err := closeEdge(eb, src.declared(), tgt.declared(), sch, s.edgeTypes, s.edgeKeys, s.edgeCands); err != nil {
			return err
		}
		s.orientation.recordEdge(eb, src, tgt, sch)
	}
	s.NarrowPluralEndpoints(sch)
	return nil
}

// NarrowPluralEndpoints is the second half of Phase C. Closing an edge commits
// a candidate set, and every member of that set names a node type on each of
// the pattern's two ends — so the ends are constrained by the closure whether
// or not the binding at one of them was written with a label. R3 §4.5.2 already
// applies that constraint to unlabelled bindings in Phase B; this applies the
// same rule to a labelled binding that plural label satisfaction (ADR 0022)
// left with several candidate types, which Phase B skips precisely because it
// is already bound.
//
// That constraint holds only for an edge EVERY RETURNED ROW HAS. A commitment
// describes the rows that carry the edge; it says nothing about a row that does
// not. So only bindings that pass witnessesItsEndpoints are folded in — see
// there for the three shapes that fail and what each would cost. Skipping an
// edge is always safe: it lands on the pre-narrowing answer, which is what R3
// gave before this pass existed.
//
// The rule itself is endpointNarrowing, which Phase B's candidateTypes reads
// too — see there for why the two share it. This method is its APPLIER: it
// takes the key set the rule leaves each plural binding and rewrites the
// binding's candidate list to match.
//
// An empty intersection leaves the binding alone. It means the touching edges
// pin it to disjoint types, so no node satisfies all of them and the pattern
// matches nothing — a fact about which rows come back, not about which types
// the projection can name. Refusing there would narrow a query the resolver
// accepts today with no soundness case behind it (ADR 0006), so the pre-closure
// satisfying set stands and ADR 0022's own verdicts run on it unchanged.
//
// Contributions are computed from one snapshot of the binding tables and
// applied afterwards, so no binding's narrowing can depend on another's, and
// the map walks below are order-independent. Edges are NOT re-closed against
// the narrowed tables: a narrowed endpoint slice re-classifies the readings of
// candidates that previously read both ways, which can manufacture an
// orientation disagreement where the pre-narrowing close found none — a
// narrowing of accepted queries reached from a widening, which is the one
// direction this change must not move in. That is also why the call sits below
// the deferred-close loop in CloseEdges rather than above it, which
// TestDeferredEdgesCloseBeforeTheNarrowing pins.
func (s *scope) NarrowPluralEndpoints(sch schema.Schema) {
	// A pure optimization: with nodeCands empty endpointNarrowing hands back an
	// empty map — it gives an entry only to a plural binding — so the loop
	// below never runs and the work the body does before reaching it only
	// reads. The return skips work that is not free: writtenBindings walks the
	// effects and every surviving edge re-runs edgeCandidates over the schema,
	// on the common scope where nothing is plural.
	//
	// TestEndpointNarrowingGivesNoEntryWhenNothingIsPlural pins the premise,
	// both ways round: the row where p IS plural is what stops the empty answer
	// being read as an edge that would never have contributed anything.
	if len(s.nodeCands) == 0 {
		return
	}
	var edges []query.EdgeBinding
	for _, b := range s.bindings {
		if e, ok := b.(query.EdgeBinding); ok {
			edges = append(edges, e)
		}
	}
	for v, keep := range endpointNarrowing(edges, s.nodeTable(), sch, s.writtenBindings(), s.demotedGroups) {
		cands := s.nodeCands[v]
		narrowed := make([]schema.NodeType, 0, len(cands))
		for _, nt := range cands {
			if _, ok := keep[nt.KeyLabels]; ok {
				narrowed = append(narrowed, nt)
			}
		}
		// No "all candidates survived" arm: narrowed is built by filtering cands
		// in order, so keeping all of them makes it element-wise equal to
		// cands, and the default arm's write-back is then a no-op. Every v here
		// came from a plural binding, so len(cands) >= 2 and the all-survived
		// case can never collide with the singleton arm either.
		switch {
		case len(narrowed) == 0:
			// The touching edges agree on nothing this binding can be. Under
			// ADR 0006 that is a statement about which rows come back, not
			// about which types the projection may name, so the pre-narrowing
			// set stands and ADR 0022's verdicts run on it unchanged.
		case len(narrowed) == 1:
			// Determined. The binding leaves the plural lane entirely, so
			// whole-entity projection and every singular-type property lookup
			// see the type the closure pinned.
			s.nodeTypes[v] = narrowed[0]
			// v was plural, so cands was a satisfying set, and every edge folded
			// into `keep` passed covering() on both ends — the types dropped are
			// ones no matching row can have. The survivor is still a superset of
			// the attainable types, so the entry stays covered.
			//
			// The write maintains the lane's invariant — membership implies covers,
			// and the survivor qualifies per the paragraph above — for whatever
			// reads the lane after this point. Nothing does within this Part:
			// this pass applies its effects from a snapshot taken before the
			// loop, CloseEdges returns immediately after it, and resolvedCovers
			// is not carried.
			// TestScopeNarrowingToASingletonMarksTheBindingCovering pins it by
			// reading the lane back off a scope.
			s.resolvedCovers[v] = struct{}{}
			delete(s.nodeCands, v)
		default:
			s.nodeCands[v] = narrowed
		}
	}
}

// writtenBindings is the set of variable names this Part's CREATE and MERGE
// clauses introduce, read off s.effects. An anonymous EDGE enters as the empty
// string, exactly as CreateEffect.Variables / MergeEffect.Variables record it,
// so a matched anonymous edge in a Part that writes an anonymous edge tests
// positive whether or not it is the same one. That over-approximates, and it is
// the safe direction: the only consumer, witnessesItsEndpoints, treats
// membership as "do not learn from this edge", which lands on the pre-narrowing
// answer.
//
// An anonymous NODE contributes nothing at all — it is not a binding (C3), so
// it never reaches either Variables list. The empty string therefore has
// exactly one source, and it is an edge; a CREATE carrying no edge cannot
// silence a matched one however it spells its nodes.
//
// Named node bindings are in the set. They cost nothing, but not for the reason
// above: a Part's named bindings have unique variables, so a written node's
// name can never equal a matched edge's name.
func (s *scope) writtenBindings() map[string]struct{} {
	out := make(map[string]struct{})
	for _, eff := range s.effects {
		var vars []string
		switch ee := eff.(type) {
		case query.CreateEffect:
			vars = ee.Variables()
		case query.MergeEffect:
			vars = ee.Variables()
		default:
			continue
		}
		for _, v := range vars {
			out[v] = struct{}{}
		}
	}
	return out
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
	// writtenBindings is computed after the early return, for the same reason
	// NarrowPluralEndpoints computes it after its own: it walks the effects, and
	// a Part with nothing to infer has no use for the answer.
	return inferUnlabelled(pending, edges, sch, s.nodeTable(), s.callTypes, s.writtenBindings(), s.demotedGroups, s.carriedNames())
}

// carriedNames is the set of variables Part K exported into this Part, as a
// membership lookup over the same s.carriedOrder buildScopeOrder walks. Phase
// B's introducedByThisHop reads it for the one conjunct that is about the
// binding's HISTORY rather than about the pattern in hand: a name the previous
// Part already bound is non-null on rows this Part's OPTIONAL clause missed,
// however cleanly it re-declares under that clause and takes its group id.
//
// It is built per InferUnlabelled call rather than kept as a lane because the
// carry is read once here and s.carriedOrder is the authoritative order lane;
// a second copy would be a second thing to keep in step with newScope's seed.
func (s *scope) carriedNames() map[string]struct{} {
	if len(s.carriedOrder) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(s.carriedOrder))
	for _, v := range s.carriedOrder {
		out[v] = struct{}{}
	}
	return out
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
	s.demoteBareReferences()
	groups := s.optionalGroupMembership()
	s.demotedGroups = map[int]bool{}
	s.demoteProvenGroups(groups)
	s.demoteAcrossEdges(groups)
}

// demoteIfPresent writes false into v's nullable-lane entry only when the
// table already holds one; a name with no entry stays absent.
func (s *scope) demoteIfPresent(v string) {
	if _, present := s.nullableBinding[v]; present {
		s.nullableBinding[v] = false
	}
}

// demoteBareReferences is the 5xg pre-pass: bare-ref demotion. A binding
// whose parser-time flag is true was re-referenced in a required bare
// pattern; the row-drop witness demotes it. Anonymous bindings (v == "")
// skip — they carry no table entry.
func (s *scope) demoteBareReferences() {
	for _, b := range s.bindings {
		switch bb := b.(type) {
		case query.NodeBinding:
			if bb.ReferencedInRequiredBarePattern() && bb.Variable() != "" {
				s.demoteIfPresent(bb.Variable())
			}
		case query.EdgeBinding:
			// ReferencedInRequiredBarePattern: unreachable in practice — an edge is
			// never bare (the parser passes bare=false at every collectEdge call site).
			// The arm is kept for symmetry with NodeBinding.
			if bb.ReferencedInRequiredBarePattern() && bb.Variable() != "" {
				s.demoteIfPresent(bb.Variable())
			}
			// 0kq: an OPTIONAL-introduced edge variable that is re-referenced in a
			// required chain witnesses its own non-nullness; demote it here, before
			// the group-closure fixed point runs, so the group closure can also
			// propagate to co-introduced siblings if any.
			if bb.ReferencedInRequiredChain() && bb.Variable() != "" {
				s.demoteIfPresent(bb.Variable())
			}
		}
	}
}

// optionalGroups is the ay9 pre-pass's membership index, held in both
// directions so a proof about one name reaches every co-introduced sibling.
type optionalGroups struct {
	members  map[int][]string // group id → named members
	groupsOf map[string][]int // named member → group ids (may span carry + local)
}

// add records v as a member of group g, ignoring anonymous names and the
// non-group id 0, and keeping groupsOf[v] duplicate-free.
func (g optionalGroups) add(v string, id int) {
	if v == "" || id <= 0 {
		return
	}
	for _, existing := range g.groupsOf[v] {
		if existing == id {
			return
		}
	}
	g.groupsOf[v] = append(g.groupsOf[v], id)
	g.members[id] = append(g.members[id], v)
}

// optionalGroupMembership is the ay9 pre-pass: OPTIONAL-group membership
// scan. A name may belong to multiple groups simultaneously — a carried
// group id from Part K, plus a fresh local group id if Part K+1 re-declares
// the name under a new OPTIONAL MATCH. Any one group being proven demotes
// the name (and every other member of that group). Seed from carry first,
// then union in the local declarations.
func (s *scope) optionalGroupMembership() optionalGroups {
	groups := optionalGroups{
		members:  map[int][]string{},
		groupsOf: map[string][]int{},
	}
	for name, g := range s.carriedGroups {
		groups.add(name, g)
	}
	for _, b := range s.bindings {
		switch bb := b.(type) {
		case query.NodeBinding:
			groups.add(bb.Variable(), bb.OptionalGroup())
		case query.EdgeBinding:
			groups.add(bb.Variable(), bb.OptionalGroup())
		}
	}
	return groups
}

// demoteGroup demotes every member of group g, reporting whether this call
// was the one that proved it. Requires s.demotedGroups to be non-nil.
func (s *scope) demoteGroup(groups optionalGroups, g int) bool {
	if g == 0 || s.demotedGroups[g] {
		return false
	}
	s.demotedGroups[g] = true
	for _, m := range groups.members[g] {
		s.demoteIfPresent(m)
	}
	return true
}

// demoteGroupsOf demotes every group v belongs to, reporting whether any of
// them was newly proven.
func (s *scope) demoteGroupsOf(groups optionalGroups, v string) bool {
	changed := false
	for _, g := range groups.groupsOf[v] {
		if s.demoteGroup(groups, g) {
			changed = true
		}
	}
	return changed
}

// demoteProvenGroups fires the carried-group closure. A carried binding
// whose local Nullable() entry in the table is already false (either from
// SeedLocalNullability's re-MATCH override or from the 5xg pre-pass) is a
// proven witness for its carried group. Fire that closure before the
// edge-driven fixed point so a carried group without a local edge witness
// still demotes. Map iteration order is unobservable here: demoteGroup
// writes false idempotently to each member's table entry, so any visit
// order converges to the same fixed point.
func (s *scope) demoteProvenGroups(groups optionalGroups) {
	for name, gs := range groups.groupsOf {
		if nb, present := s.nullableBinding[name]; present && !nb {
			for _, g := range gs {
				s.demoteGroup(groups, g)
			}
		}
	}
}

// demoteAcrossEdges runs the edge-driven fixed point (§4.4) to convergence.
func (s *scope) demoteAcrossEdges(groups optionalGroups) {
	for changed := true; changed; {
		changed = false
		for _, b := range s.bindings {
			e, ok := b.(query.EdgeBinding)
			if !ok {
				continue
			}
			// ay9's exemption — an OPTIONAL edge whose group is proven
			// is an effective witness, its existence on surviving rows
			// established — lives in presentOnEveryRow, which
			// witnessesItsEndpoints calls too. Calling it rather than
			// re-spelling it is what stops the two drifting. The §4.4.3
			// hop gate beside it differs between the two callers and
			// stays here.
			if !presentOnEveryRow(e, s.demotedGroups) || !qualifiedDemoter(e) {
				continue
			}
			if s.demoteEndpoints(groups, e) {
				changed = true
			}
		}
	}
}

// demoteEndpoints demotes both named endpoints of a qualified demoter edge
// and their groups, reporting whether anything moved.
func (s *scope) demoteEndpoints(groups optionalGroups, e query.EdgeBinding) bool {
	changed := false
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
		if s.demoteGroupsOf(groups, v) {
			changed = true
		}
	}
	return changed
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
func (s *scope) ResolveProjections(sch schema.Schema, final bool) error {
	s.scopeOrder = s.buildScopeOrder()

	items, err := s.materialiseReturns()
	if err != nil {
		return err
	}
	s.items = items

	s.columns = make([]Column, 0, len(items))
	for _, item := range items {
		if !final && s.carriesPluralBinding(item) {
			s.columns = append(s.columns, Column{Name: item.Name, Type: ResolvedUnknown{}})
			continue
		}
		colType, err := s.projectionType(item.Value, sch)
		if err != nil {
			return err
		}
		s.columns = append(s.columns, Column{Name: item.Name, Type: colType})
	}
	return nil
}

// carriesPluralBinding reports whether item is the one projection shape a
// non-final Part may leave untyped: a bare `WITH v` (or its WITH * equivalent)
// naming a node binding that several declared types satisfy.
//
// Such a Part's columns are discarded by resolveBranch, so this projection is a
// carry and not a column anyone reads a type off. Refusing it here is premature
// — the downstream Part may narrow the candidate set by edge closure, and if it
// does not, the whole-entity refusal fires there instead, on the same candidate
// set and through the same sentinel. The deferral is why exportedNodeCands is
// reachable at all.
//
// The predicate deliberately restates Export's own condition rather than
// asking "is this a whole-entity projection". Export records a nodeCands entry
// only when the alias equals the variable and no property is named, so those
// are exactly the projections whose candidate set survives the boundary. For
// `WITH p AS q` the set is dropped and only exportedResolvedTypes carries `q`
// — deferring there would send a name downstream with nothing left to refuse
// it against, so an aliased plural projection is still refused here.
//
// The column type is ResolvedUnknown because at this Part the type genuinely is
// not yet determined. It reaches no caller: resolveBranch keeps only the final
// Part's columns, and while Export does copy it into exportedResolvedTypes, the
// same name also lands in exportedNodeCands, which every downstream reader
// consults first.
func (s *scope) carriesPluralBinding(item query.ReturnItem) bool {
	rp, ok := item.Value.(query.RefProjection)
	if !ok {
		return false
	}
	ref := rp.Ref()
	if ref.Property != "" || item.Name != ref.Variable {
		return false
	}
	_, plural := s.nodeCands[ref.Variable]
	return plural
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
		if _, isCand := s.nodeCands[v]; isCand {
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
		return query.NewRefProjection(query.Ref{Variable: name}, query.TypeUnknown{}), nil
	}
	if _, ok := s.nodeCands[name]; ok {
		return query.NewRefProjection(query.Ref{Variable: name}, query.TypeUnknown{}), nil
	}
	if _, ok := s.edgeBindings[name]; ok {
		return query.NewRefProjection(query.Ref{Variable: name}, query.TypeUnknown{}), nil
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
		return s.certifiedProjectionType(pp.Type(), pp.Refs(), pp.LeavesAreRefs(), sch)
	case query.AggregateProjection:
		// The bare unknown is overloaded as a TYPE and not as a PROJECTION:
		// Func() is in the shared model and separates "the operand's type was
		// not committed at parse time" from "the fold is engine-dependent"
		// without the resolver ever consulting the parser's fold table (spec
		// ruling-p9qgu §1). min/max SELECT and so take the selection sibling;
		// collect folds into a list spine and keeps certifiedProjectionType
		// verbatim; everything else keeps today's answer.
		if fn := pp.Func(); fn == query.AggMin || fn == query.AggMax {
			return s.selectionProjectionType(pp.Type(), pp.Refs(), pp.LeavesAreRefs(), sch)
		}
		return s.certifiedProjectionType(pp.Type(), pp.Refs(), pp.LeavesAreRefs(), sch)
	default:
		return nil, fmt.Errorf("%w: unknown projection variant (%T)", ErrOutOfR0Scope, p)
	}
}

// certifiedProjectionType resolves a rich projection's column type, filling
// the committed type's unknown leaf from the schema when the projection
// carries the ref-valued-leaf certificate (query.ExprProjection.LeavesAreRefs,
// spec docs/specs/model-change-f45qn-ref-valued-leaves.md §5). Without the
// certificate this is resolveType and nothing else — the behaviour every rich
// projection had before the axis existed.
//
// The consistency invariant the design hangs on: a certified projection's leaf
// type is byte-identical to what the same ref projected BARE would resolve to,
// because the refs go through refProjectionType rather than a second reading of
// the schema. Same nullability OR, same plural-candidate intersection, same
// CALL-YIELD and carried-alias lanes, same refusals.
func (s *scope) certifiedProjectionType(t query.Type, refs []query.Ref, certified bool, sch schema.Schema) (ResolvedType, error) {
	base, err := resolveType(t)
	if err != nil || !certified {
		return base, err
	}
	leaf, unified, err := s.unifiedRefPropertyType(refs, sch)
	if err != nil {
		return nil, err
	}
	if !unified {
		return base, nil
	}
	return fillLeaf(base, leaf, false), nil
}

// selectionProjectionType resolves min/max's column type (spec
// ruling-p9qgu-fold-result-upgrade.md §3.1, §4). min and max do not FOLD, they
// SELECT: the result is one of the operand's own values, unchanged, so it is
// exactly as representable as the property is and the machinery that already
// round-trips `RETURN p.age` through a driver's 64-bit integer into a declared
// INT32 — ADR 0002's width preservation with ADR 0037's out-of-width read
// refusal — covers the aggregate result with nothing added. There is no fold
// table here and there must not be one.
//
// It is a SIBLING of certifiedProjectionType rather than a flag on it, and
// fillLeaf is deliberately not on this path. fillLeaf's underList parameter is
// the never-fill-a-bare-unknown belt, and min/max fill a bare unknown; reaching
// it by passing underList=true or deleting the condition would authorise avg's
// fill as a side effect, silently, which is the one thing that belt exists to
// stop (ruling §4).
//
// Three ways this is NOT the operand's type verbatim:
//
//   - Nullable is forced TRUE. min/max over an EMPTY GROUP are NULL, so the
//     column is nullable even where the property is declared NOT NULL and the
//     binding is non-nullable. Reusing refProjectionType's nullability verbatim
//     would emit a non-pointer field that receives NULL — the ADR 0041 defect
//     in the other direction.
//   - Only an ORDERABLE family fills. BYTES is not orderable and must not be
//     admitted; see orderableSelection.
//   - Only a ResolvedProperty fills. min(n) over a node binding degrades to
//     base, which is today's any.
//
// The single ref is resolved through s.refProjectionType REUSED VERBATIM, which
// is f45qn §5's consistency invariant: a certified projection's leaf type is
// byte-identical to what the same ref projected BARE would resolve to, because
// it goes through the one reader rather than a second reading of the schema.
// The named acceptance change rides on that reuse — min(p.nosuch) now refuses
// ErrUnknownProperty where it was silently any, exactly as collect already does.
//
// len(refs) != 1 degrades rather than refusing, and rides the same guard as the
// uncertified case because it is the same answer for the same reason: there is
// nothing to upgrade base FROM. The depth-0 mint condition makes a certified
// min/max operand a single bare var/var.prop, so it is a belt against a future
// mint site rather than a reachable arm today. It shares that guard's return
// spelling deliberately — TestNilColumnTypeIsNotConstructible requires every
// arm returning a value it did not build to be distinguishable BY ITS RETURN
// TEXT, so two arms with one recorded argument must be one arm.
func (s *scope) selectionProjectionType(t query.Type, refs []query.Ref, certified bool, sch schema.Schema) (ResolvedType, error) {
	base, err := resolveType(t)
	if err != nil || !certified || len(refs) != 1 {
		return base, err
	}
	rt, err := s.refProjectionType(refs[0], sch)
	if err != nil {
		return nil, err
	}
	prop, ok := rt.(ResolvedProperty)
	if !ok || !orderableSelection(prop.Type) {
		return base, nil
	}
	prop.Nullable = true
	return prop, nil
}

// orderableSelection reports whether a min/max over a property of this declared
// type yields a value of that same type — i.e. whether the family carries a
// total order the selection preserves (spec ruling-p9qgu §3.1).
//
// It is NOT aggregateResultType and is NOT derived from it, and it must not be
// described as a copy of it. That table is query.Type -> query.Type over the
// parser's WIDTH-FREE types; this one is a predicate over graph.PropertyType,
// which carries the width ADR 0002 exists to preserve. Calling the parser's
// table from here would mean demoting ResolvedProperty{INT32} to
// query.TypeInt{} and then inventing a width for the answer, which is
// discarding the fidelity ADR 0002 commits to and re-guessing it one line later
// (ruling §2). The two range over different domains and answer different
// questions.
//
// It is an ALLOW-list, so anything unenumerated degrades to any — the safe
// direction, since a wrong concrete type is strictly worse than an honest
// unknown. What that excludes, and why:
//
//   - BYTES. The ruling's named negative: not ordered by the aggregate.
//   - DECIMAL. Not among the families the ruling enumerates. Excluding it
//     degrades to any, which costs a column its type and cannot be wrong;
//     admitting it would be a decision no one has made.
//   - ANY, LIST<ANY>, RECORD<ANY> and every composite spelling (LIST<INT32>,
//     RECORD<...>). Ordering these is engine-dependent at best, and "can
//     min/max over a list be typed" is explicitly not opened by the ruling.
//
// PropertyType is a string type with an open composite grammar rather than a
// closed enum, so this cannot be written as an exhaustive switch over declared
// constants; TestOrderableSelectionNamesEveryPropertyType holds the verdict for
// every constant internal/graph declares, which is where the arms this corpus
// does not reach are witnessed.
func orderableSelection(t graph.PropertyType) bool {
	switch t {
	case graph.TypeString, graph.TypeBool,
		graph.TypeDate, graph.TypeTime, graph.TypeLocalTime,
		graph.TypeTimestamp, graph.TypeDuration,
		graph.TypeInt, graph.TypeInt8, graph.TypeInt16, graph.TypeInt32,
		graph.TypeInt64, graph.TypeInt128, graph.TypeInt256,
		graph.TypeUint, graph.TypeUint8, graph.TypeUint16, graph.TypeUint32,
		graph.TypeUint64, graph.TypeUint128, graph.TypeUint256,
		graph.TypeFloat, graph.TypeFloat16, graph.TypeFloat32,
		graph.TypeFloat64, graph.TypeFloat128, graph.TypeFloat256:
		return true
	default:
		// The allow-list's point, and also what keeps `exhaustive` off this
		// switch: it runs default-signifies-exhaustive, so a default arm is
		// the declaration that the unenumerated constants are handled
		// deliberately. That linter could not hold the full set anyway — the
		// composite spellings are not constants — which is why the verdict
		// table lives in a test instead.
		return false
	}
}

// unifiedRefPropertyType resolves every ref of a certified projection and
// unifies them into the one property type they all mean. unified is false when
// they mean more than one thing — [p.id, p.name] is int64 and string, so the
// certificate is spent and the honest list<any> stands; that is a degrade, not
// an error. A resolution failure IS an error and propagates: `[p.nosuch]`
// refuses exactly as bare `p.nosuch` already does.
//
// Only ResolvedProperty fills. It is the sole variant that both arrives at a
// committed TypeUnknown leaf (the parser types every property lookup unknown,
// ADR 0003) and carries a schema-declared type to put there. Restricting the
// fill also keeps a refusal intact: a node or edge leaf reaching a list
// position is what resolveType refuses as a list-of-entities projection, and
// filling one in would route around that refusal rather than satisfy it.
//
// Widths never widen, per ADR 0002: ResolvedProperty{INT32} and
// ResolvedProperty{INT64} do not unify. Nullability is the one field that
// composes, and it ORs — a list holding one nullable element is nullable at
// the element position.
func (s *scope) unifiedRefPropertyType(refs []query.Ref, sch schema.Schema) (ResolvedProperty, bool, error) {
	var unified ResolvedProperty
	for i, ref := range refs {
		rt, err := s.refProjectionType(ref, sch)
		if err != nil {
			return ResolvedProperty{}, false, err
		}
		prop, ok := rt.(ResolvedProperty)
		if !ok {
			return ResolvedProperty{}, false, nil
		}
		if i == 0 {
			unified = prop
			continue
		}
		if prop.Type != unified.Type {
			return ResolvedProperty{}, false, nil
		}
		unified.Nullable = unified.Nullable || prop.Nullable
	}
	return unified, len(refs) > 0, nil
}

// fillLeaf replaces t's unknown leaf with leaf wherever it sits STRICTLY under
// a list, leaving every other position alone.
//
// underList is the belt, and it is a real guard rather than a formality even
// though every certified projection today commits a list spine. A future mint
// site over an engine-dependent result (avg) or a fold would certify a BARE
// unknown, where the unknown means "the server decides" — something no schema
// lookup is entitled to overwrite. Such a projection must fail toward any,
// never toward a confidently wrong concrete, and this is where that happens.
func fillLeaf(t, leaf ResolvedType, underList bool) ResolvedType {
	switch tt := t.(type) {
	case ResolvedList:
		return ResolvedList{Element: fillLeaf(tt.Element, leaf, true), Nullable: tt.Nullable}
	case ResolvedUnknown:
		if underList {
			return leaf
		}
		return t
	default:
		return t
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
			return ResolvedNode{Labels: nt.KeyLabels, Nullable: s.nullableBinding[ref.Variable]}, nil
		}
		prop, ok := nt.Properties[ref.Property]
		if !ok {
			return nil, fmt.Errorf("%w: %s.%s", ErrUnknownProperty, ref.Variable, ref.Property)
		}
		return ResolvedProperty{Type: prop.Type, Nullable: prop.Nullable || s.nullableBinding[ref.Variable]}, nil
	}
	if nts, ok := s.nodeCands[ref.Variable]; ok {
		if ref.Property == "" {
			// Which sentinel is the binding's provenance, not this arm's:
			// errors.go documents ErrAmbiguousLabel as a LABEL SET whose
			// proper-superset satisfying set has more than one element, and
			// ErrAmbiguousBinding as an unlabelled binding whose candidate set
			// has more than one node type. An unlabelled binding reaching here
			// has no label set to be ambiguous about; it got here because
			// commit() bound the widened plural set instead of deferring, and
			// the same query without the widening refuses with
			// ErrAmbiguousBinding at Phase B's terminal deferral. The message
			// text is one string for both because the fault is one fault —
			// only its category-grained name differs (r1 §5, sentinels are
			// category-grained).
			sentinel := ErrAmbiguousLabel
			if s.pluralByInference[ref.Variable] {
				sentinel = ErrAmbiguousBinding
			}
			return nil, fmt.Errorf("%w: %s is satisfied by more than one declared node type: %s", sentinel, ref.Variable, formatNodeTypeKeys(nts))
		}
		return unionNodeProperty(nts, ref.Variable, ref.Property, s.nullableBinding[ref.Variable], clauseRead)
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
		// A var-length binding is a LIST, and binding optionality is the
		// list's own rather than its elements'. An unmatched OPTIONAL pattern
		// binds the whole list to null; the elements of a path that did match
		// are edges and are never individually null.
		elemNullable := edgeNullable
		if varLength {
			elemNullable = false
		}
		var element ResolvedType
		if singleCand {
			element = ResolvedEdge{EdgeKey: s.edgeKeys[ref.Variable], Nullable: elemNullable}
		} else {
			element = ResolvedEdgeUnion{EdgeKeys: cands, Nullable: elemNullable}
		}
		if varLength {
			return ResolvedList{Element: element, Nullable: edgeNullable}, nil
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
	return unionProperty(cands, sch, ref.Variable, ref.Property, edgeNullable, clauseRead)
}

// Export builds the branchState Part K passes to Part K+1 (§4.2.2).
// For an explicit WITH (s.returnsAll == false), the exported set is
// exactly s.returns keyed by Name. For WITH * (s.returnsAll == true),
// the exported set is the full in-scope binding set at the moment
// WITH ran, in s.scopeOrder. For a final Part (RETURN), the returned
// branchState is irrelevant (no next Part reads it) but we still
// build it for symmetry. R7 §4.6 adds the exportedCallTypes lane so
// CALL YIELD scalars survive a bare `WITH v` carry (aliased carry
// also lands in exportedResolvedTypes via the R5 path).
//
// Parameter-free per §2.2 — reads s.returns / s.returnsAll / s.items
// / s.columns / s.scopeOrder / s.bindings / the seven live binding
// lanes / s.carriedGroups off the receiver.
func (s *scope) Export() branchState {
	out := branchState{
		exportedNodeTypes:         make(map[string]schema.NodeType),
		exportedNodeCands:         make(map[string][]schema.NodeType),
		exportedPluralByInference: make(map[string]bool),
		exportedEdgeTypes:         make(map[string]schema.EdgeType),
		exportedEdgeKeys:          make(map[string]schema.EdgeKey),
		exportedEdgeCands:         make(map[string][]schema.EdgeKey),
		exportedEdgeBindings:      make(map[string]query.EdgeBinding),
		exportedNullableBinding:   make(map[string]bool),
		exportedOptionalGroup:     make(map[string]int),
		exportedResolvedTypes:     make(map[string]ResolvedType),
		exportedCallTypes:         make(map[string]callBindingSlot),
	}
	localGroup := s.localGroupIDs()

	// Names that leave via WITH — for WITH * that's every scopeOrder name;
	// for an explicit WITH item that's item.Name (which for a bare `WITH v`
	// equals v, and for `WITH e.p AS x` equals `x`, not `v`).
	var exportedNames []string
	if s.returnsAll {
		exportedNames = s.scopeOrder
		for i, item := range s.items {
			// s.items[i].Name == s.scopeOrder[i] for the wildcard-expanded
			// case. carried-type entries pass through unchanged; binding-
			// derived entries populate the binding maps below.
			out.exportedResolvedTypes[item.Name] = s.columns[i].Type
		}
	} else {
		exportedNames = make([]string, 0, len(s.returns))
		for i, item := range s.returns {
			exportedNames = append(exportedNames, item.Name)
			out.exportedResolvedTypes[item.Name] = s.columns[i].Type
		}
	}
	out.exportedOrder = exportedNames

	iter := s.returns
	if s.returnsAll {
		iter = s.items
	}
	for _, item := range iter {
		s.exportBindingLanes(&out, item, localGroup)
	}
	return out
}

// localGroupIDs is the local group-id lookup Export builds up front: a name
// gets its local binding's OptionalGroup if declared this Part, otherwise its
// carried group id from the incoming carry. Local shadows carry — a local
// re-declaration with a distinct (possibly zero) group replaces the carried
// id. Only names surviving into exportedNames get promoted to the outgoing
// carry.
func (s *scope) localGroupIDs() map[string]int {
	localGroup := map[string]int{}
	for _, b := range s.bindings {
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
	return localGroup
}

// exportBindingLanes populates the binding maps for an export whose Name
// corresponds to an in-scope binding-name (bare RefProjection{Ref{v, ""}}).
// An aliased export like `WITH e.p AS x` puts `x` only in
// exportedResolvedTypes, not in any binding map — downstream refs to `x`
// bypass via §4.5.4.
func (s *scope) exportBindingLanes(out *branchState, item query.ReturnItem, localGroup map[string]int) {
	alias := item.Name
	rp, ok := item.Value.(query.RefProjection)
	if !ok {
		return
	}
	ref := rp.Ref()
	// Only export a binding entry when the alias matches the bare
	// binding-name reference (Ref{Variable: v, Property: ""} named by
	// its own variable). Anything else — property projection, renamed
	// alias — lives only in exportedResolvedTypes.
	if ref.Property != "" || alias != ref.Variable {
		return
	}
	v := ref.Variable
	if nt, ok := s.nodeTypes[v]; ok {
		out.exportedNodeTypes[v] = nt
	}
	if cands, ok := s.nodeCands[v]; ok {
		out.exportedNodeCands[v] = cands
		// Only alongside a nodeCands entry: the lane is meaningless for a
		// name with no candidate set, and seeding it for one would put a
		// true into a downstream scope that nothing ever clears.
		if s.pluralByInference[v] {
			out.exportedPluralByInference[v] = true
		}
	}
	if et, ok := s.edgeTypes[v]; ok {
		out.exportedEdgeTypes[v] = et
		if k, ok := s.edgeKeys[v]; ok {
			out.exportedEdgeKeys[v] = k
		}
	}
	if cands, ok := s.edgeCands[v]; ok {
		out.exportedEdgeCands[v] = cands
	}
	if b, ok := s.edgeBindings[v]; ok {
		out.exportedEdgeBindings[v] = b
	}
	if nb, ok := s.nullableBinding[v]; ok {
		out.exportedNullableBinding[v] = nb
	}
	if slot, ok := s.callTypes[v]; ok {
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
	} else if g, ok := s.carriedGroups[v]; ok && g > 0 {
		out.exportedOptionalGroup[v] = g
	}
}

// ValidateEffects is R6 Phase E: walk s.effects in slice order,
// dispatch each Effect through its per-variant validator, short-
// circuit on first failure. Reads from the schema-committed binding
// tables and the carried resolved types on the receiver; never mutates
// them. Single public entry; the seven per-variant validators are
// unexported package-level helpers taking *scope (§2.2 §5 step 3).
func (s *scope) ValidateEffects(sch schema.Schema) error {
	for _, e := range s.effects {
		if err := validateEffect(s, e, sch, clauseSet); err != nil {
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
		nodeCands:       make(map[string][]schema.NodeType, len(s.nodeCands)),
		edgeTypes:       make(map[string]schema.EdgeType, len(s.edgeTypes)),
		edgeCands:       make(map[string][]schema.EdgeKey, len(s.edgeCands)),
		edgeBindings:    make(map[string]query.EdgeBinding, len(s.edgeBindings)),
		nullableBinding: make(map[string]bool, len(s.nullableBinding)),
	}
	for k, v := range s.nodeTypes {
		sc.nodeTypes[k] = v
	}
	for k, v := range s.nodeCands {
		sc.nodeCands[k] = v
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

// Contains reports whether variable v names an in-scope entity binding
// (node, single-cand edge, or edge-union). Used by the parameter-Use
// walker to decide whether a Ref hits this scope's tables. callTypes
// and carry-only lanes are NOT observable through partScope per §2.3
// invariant #3 — a CALL YIELD variable does not surface here.
func (sc partScope) Contains(v string) bool {
	if _, ok := sc.nodeTypes[v]; ok {
		return true
	}
	if _, ok := sc.nodeCands[v]; ok {
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

// PropertyUseWitness resolves a Ref against this scope's binding
// tables and returns the property's ResolvedType. Assumes Contains
// has already gated the call — an out-of-scope Ref here surfaces
// ErrOutOfR0Scope. A var-length edge property projection is rejected
// with the R5-canonical message. The single-cand / union-cand branch
// mirrors refProjectionType (§4.5.4-adjacent, but for parameter
// witnessing rather than projection).
func (sc partScope) PropertyUseWitness(ref query.Ref, s schema.Schema) (ResolvedType, error) {
	if nt, ok := sc.nodeTypes[ref.Variable]; ok {
		prop, ok := nt.Properties[ref.Property]
		if !ok {
			return nil, fmt.Errorf("%w: %s.%s", ErrUnknownProperty, ref.Variable, ref.Property)
		}
		return ResolvedProperty{Type: prop.Type, Nullable: prop.Nullable || sc.nullableBinding[ref.Variable]}, nil
	}
	if nts, ok := sc.nodeCands[ref.Variable]; ok {
		return unionNodeProperty(nts, ref.Variable, ref.Property, sc.nullableBinding[ref.Variable], clauseRead)
	}
	_, singleCand := sc.edgeTypes[ref.Variable]
	cands, multiCand := sc.edgeCands[ref.Variable]
	if !singleCand && !multiCand {
		// unreachable: WitnessUse gates on Contains() before calling here;
		// Contains() requires nodeTypes, nodeCands, edgeTypes, or edgeCands.
		// Having passed the nodeTypes and nodeCands arms above, the variable
		// must be in edgeTypes or edgeCands — so this arm can only be reached
		// if that gate is relaxed.
		return nil, fmt.Errorf("%w: %s", ErrOutOfR0Scope, ref.Variable)
	}
	if binding := sc.edgeBindings[ref.Variable]; binding.Hops() != nil {
		return nil, fmt.Errorf("%w: property projection on variable-length edge binding: reach list elements via list-element access (UNWIND in R5 or later)", ErrOutOfR0Scope)
	}
	edgeNullable := sc.nullableBinding[ref.Variable]
	if singleCand {
		et := sc.edgeTypes[ref.Variable]
		prop, ok := et.Properties[ref.Property]
		if !ok {
			return nil, fmt.Errorf("%w: %s.%s", ErrUnknownProperty, ref.Variable, ref.Property)
		}
		return ResolvedProperty{Type: prop.Type, Nullable: prop.Nullable || edgeNullable}, nil
	}
	return unionProperty(cands, s, ref.Variable, ref.Property, edgeNullable, clauseRead)
}

// WitnessUse produces exactly one witness (or zero) for a Use. The
// lexical Part attribution recorded on the Use record (fvo per ADR
// 0008 amendment 2026-07-06) selects which scope this method runs
// on — the unifier's selectPartScope helper handles that dispatch;
// range-checking a PropertyUse's Part index lives at the caller
// (§6.1). A PropertyUse against a scope that doesn't contain its Ref
// yields zero witnesses (unifier bottom — matches pre-fvo behaviour).
// If the scope contains the variable but property lookup fails,
// ErrUnknownProperty surfaces immediately (the pre-fvo any-valid-
// witness swallowing is retired). ClauseSlotUse and ExprUse are
// Part-agnostic — their witness ignores the receiver's tables
// entirely (a zero partScope is a legal receiver from the caller).
func (sc partScope) WitnessUse(u query.Use, s schema.Schema) ([]ResolvedType, error) {
	switch uu := u.(type) {
	case query.PropertyUse:
		ref := uu.Ref()
		if !sc.Contains(ref.Variable) {
			return nil, nil
		}
		w, err := sc.PropertyUseWitness(ref, s)
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
		}
		// Below the inner switch rather than in a `default`, so `exhaustive`
		// still checks it for a missing ExprPosition arm. Every declared
		// member is named above, so this answers only for a position outside
		// the enum — which is what the deleted `default` answered for it too
		// (bd gqlc-qr09l). The OUTER default stays: query.Use is an open
		// interface, not a closed sum.
		return nil, fmt.Errorf("%w: unknown ExprUse position", ErrOutOfR0Scope)
	default:
		return nil, fmt.Errorf("%w: unknown Use variant (%T)", ErrOutOfR0Scope, u)
	}
}
