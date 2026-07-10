// Package cypher implements query.Parser for openCypher: an ANTLR
// listener-driven lowering of query source into the curated query model,
// grown test-first against the openCypher TCK (ADR 0004).
package cypher

import (
	"fmt"

	"github.com/areqag/gqlc/internal/graph"
	"github.com/areqag/gqlc/internal/query"
)

// build assembles the collected branches, parts, parameters and return items into
// a query.Query, after a per-part self-consistency validation (spec §2/§4): every
// referenced variable (return item, edge endpoint) must resolve to a binding in
// its own part OR to a name the previous part's WITH carried into it (else
// ErrUnboundVariable), and an edge endpoint resolving to an edge binding in its
// own part is a kind conflict (else ErrVariableKindConflict). This is a
// validation, not a schema resolution — no schema is consulted. build returns the
// zero Query on any error.
func (l *listener) build() (query.Query, error) {
	if l.err != nil {
		return query.Query{}, l.err
	}

	branches := make([]query.Branch, 0, len(l.branches))
	for _, rb := range l.branches {
		branch, err := l.buildBranch(rb)
		if err != nil {
			return query.Query{}, err
		}
		branches = append(branches, branch)
	}

	params := make([]query.Parameter, 0, len(l.params))
	for _, p := range l.params {
		params = append(params, *p)
	}

	q := query.Query{Branches: branches}
	if len(l.combinators) > 0 {
		q.Combinators = l.combinators
	}
	if len(params) > 0 {
		q.Parameters = params
	}
	if l.writeSeen {
		q.StatementKind = query.StatementWrite
	}
	return q, nil
}

// buildBranch validates and assembles one branch's parts, threading the
// exported-name set left to right: part K resolves its refs against {its own
// bindings} ∪ {names part K−1 exported via WITH}, and computes what it exports
// into part K+1 (spec §4).
func (l *listener) buildBranch(rb *rawBranch) (query.Branch, error) {
	parts := make([]query.Part, 0, len(rb.parts))
	imported := map[string]bool{} // names the previous part's WITH carried in
	for _, rp := range rb.parts {
		part, exported, err := l.buildPart(rp, imported)
		if err != nil {
			return query.Branch{}, err
		}
		parts = append(parts, part)
		imported = exported
	}
	return query.Branch{Parts: parts}, nil
}

// buildPart validates one part against its scope ({its own named bindings} ∪
// imported) and returns the assembled query.Part plus the set of names it
// exports into the next part. Endpoint refs must resolve to a NODE binding within
// the part's own bindings (an imported name carries no kind to check, and an edge
// endpoint always names a node in the same MATCH).
func (l *listener) buildPart(rp *rawPart, imported map[string]bool) (query.Part, map[string]bool, error) {
	scope := map[string]bool{}
	for k := range imported {
		scope[k] = true
	}
	for _, b := range rp.bindings {
		if b.variable != "" {
			scope[b.variable] = true
		}
	}
	// Stage 8: path variables introduced in this part enter the scope the
	// same way entity variables do — a RETURN p on a named path resolves
	// against the path binding, and its type is TypePath (via refType).
	// A path variable that clashes with an entity binding of the same name
	// (a preceding MATCH bound r as a node, and this MATCH used r = (...))
	// is a kind conflict — three-way (node/edge/path), extending the
	// Stage 0..7 two-way check (§1.6).
	for _, pb := range rp.pathBindings {
		if _, ok := rp.byVar[pb.Variable()]; ok {
			return query.Part{}, nil, fmt.Errorf("%w: %s", ErrVariableKindConflict, pb.Variable())
		}
		scope[pb.Variable()] = true
	}
	// Stage 9: UNWIND-introduced variables enter the scope alongside entity
	// and path variables. A RETURN x on `UNWIND … AS x` resolves against
	// the unwind binding, and its type is the recorded element type
	// (via refType). A same-name entity, path, or earlier unwind binding
	// preceding it in the same part is a kind conflict — collectUnwind
	// catches the byVar and unwind-vs-unwind and path-vs-unwind cases at
	// listener time; the three sweeps here are the belt-and-braces
	// symmetric backstop (spec §4.3 amend).
	pathByVar := make(map[string]bool, len(rp.pathBindings))
	for _, pb := range rp.pathBindings {
		pathByVar[pb.Variable()] = true
	}
	unwindByVar := make(map[string]bool, len(rp.unwindBindings))
	for _, ub := range rp.unwindBindings {
		if _, ok := rp.byVar[ub.Variable()]; ok {
			return query.Part{}, nil, fmt.Errorf("%w: %s", ErrVariableKindConflict, ub.Variable())
		}
		if pathByVar[ub.Variable()] {
			return query.Part{}, nil, fmt.Errorf("%w: %s", ErrVariableKindConflict, ub.Variable())
		}
		if unwindByVar[ub.Variable()] {
			return query.Part{}, nil, fmt.Errorf("%w: %s", ErrVariableKindConflict, ub.Variable())
		}
		unwindByVar[ub.Variable()] = true
		scope[ub.Variable()] = true
	}
	// Stage 14: CALL YIELD bindings enter the scope alongside entity,
	// path, and unwind bindings. Five-way collision sweep (entity /
	// path / unwind / prior call / imported) — imported catches
	// Call1[15]'s `WITH 'Hi' AS label CALL test.labels() YIELD label`
	// pattern, where the CallBinding's variable collides with a name
	// exported by the preceding WITH. The `if callByVar[v]` arm is
	// the sole authority for intra-YIELD name collision — the two
	// `CALL YIELD intra rename collision` reject cases in
	// parser_test.go discriminate it.
	callByVar := make(map[string]bool, len(rp.callBindings))
	for _, cb := range rp.callBindings {
		v := cb.Variable()
		if _, ok := rp.byVar[v]; ok {
			return query.Part{}, nil, fmt.Errorf("%w: %s", ErrVariableKindConflict, v)
		}
		if pathByVar[v] {
			return query.Part{}, nil, fmt.Errorf("%w: %s", ErrVariableKindConflict, v)
		}
		if unwindByVar[v] {
			return query.Part{}, nil, fmt.Errorf("%w: %s", ErrVariableKindConflict, v)
		}
		if callByVar[v] {
			return query.Part{}, nil, fmt.Errorf("%w: %s", ErrVariableKindConflict, v)
		}
		if imported[v] {
			return query.Part{}, nil, fmt.Errorf("%w: %s", ErrVariableKindConflict, v)
		}
		callByVar[v] = true
		scope[v] = true
	}

	for _, ref := range rp.refs {
		if !scope[ref.name] {
			return query.Part{}, nil, fmt.Errorf("%w: %s", ErrUnboundVariable, ref.name)
		}
		// An endpoint must reference a node binding; it always names a node in the
		// same MATCH, so its kind is checked against this part's own bindings. A
		// return-item ref accepts either kind (and may resolve to an imported name).
		if ref.endpointRef {
			idx, ok := rp.byVar[ref.name]
			if ok && rp.bindings[idx].kind != graph.Node {
				return query.Part{}, nil, fmt.Errorf("%w: %s", ErrVariableKindConflict, ref.name)
			}
		}
	}

	bindings := make([]query.Binding, 0, len(rp.bindings)+len(rp.pathBindings)+len(rp.unwindBindings)+len(rp.callBindings))
	for _, rb := range rp.bindings {
		b, err := rb.toBinding()
		if err != nil {
			return query.Part{}, nil, err
		}
		bindings = append(bindings, b)
	}
	// Path bindings appear after every entity binding they capture (Stage 8):
	// build() appends them in the order collectPattern recorded, so the wire
	// shape is deterministic and the path member list references bindings
	// already present earlier in the slice.
	for _, pb := range rp.pathBindings {
		bindings = append(bindings, pb)
	}
	// Unwind bindings appear after path bindings (Stage 9): the ordering is
	// arbitrary but deterministic — collectUnwind records in walk order, and
	// no downstream shape depends on the position within the slice.
	for _, ub := range rp.unwindBindings {
		bindings = append(bindings, ub)
	}
	// Stage 14: CallBindings appear after unwind bindings, in
	// collectCall walk order (which for standalone YIELD * and
	// no-YIELD is signature declaration order). No downstream shape
	// depends on the position within the slice; the discipline is
	// deterministic ordering.
	for _, cb := range rp.callBindings {
		bindings = append(bindings, cb)
	}

	// Stage 14: standalone CALL without a downstream RETURN populates
	// Part.Returns from the CallBindings (spec §4.3). The listener
	// sets callStandalone when the standalone path fires and no
	// explicit RETURN populated rp.returns; this branch mints one
	// RefProjection per CallBinding in walk order (signature-
	// declaration order) and sets returnsAll to mirror the
	// grammar's `YIELD *` / no-YIELD implicit-all posture.
	if rp.callStandalone && len(rp.returns) == 0 && !rp.returnsAll {
		for _, cb := range rp.callBindings {
			rp.returns = append(rp.returns, query.ReturnItem{
				Name: cb.Variable(),
				Value: query.NewRefProjection(
					query.Ref{Variable: cb.Variable()},
					cb.ResultType(),
				),
			})
		}
		rp.returnsAll = true
	}

	var (
		partBindings []query.Binding
		partEffects  []query.Effect
	)
	if len(bindings) > 0 {
		partBindings = bindings
	}
	if len(rp.effects) > 0 {
		partEffects = rp.effects
	}
	// Route through NewPart so the model's "at least one of bindings /
	// projection / effects" invariant is enforced at the type-interface
	// boundary (Stage 12 §3.2 amend). The grammar rules out the all-empty
	// shape, so ErrEmptyPart is unreachable via parse — but the belt-and-
	// braces guard keeps illegal states unrepresentable if a future grammar
	// widening slips.
	part, err := query.NewPart(partBindings, rp.returns, rp.returnsAll, rp.distinct, partEffects)
	if err != nil {
		return query.Part{}, nil, err
	}

	// The names this part exports into the next: under WITH * the whole in-scope
	// set carries forward (transitive — spec §4); otherwise each return item's
	// Name (the AS alias, or the bare variable for WITH a).
	exported := map[string]bool{}
	if rp.returnsAll {
		for k := range scope {
			exported[k] = true
		}
	} else {
		for _, r := range rp.returns {
			exported[r.Name] = true
		}
	}
	return part, exported, nil
}

// toBinding builds the model binding from a raw binding via the smart
// constructors, so the model's invariants are enforced at assembly. A
// positive optionalGroup picks the OPTIONAL-introduced InGroup variant
// (ADR 0006; ay9 — nullable ⇔ group ≥ 1 is the parser-side invariant).
// Stage 8: hops picks the variable-length variant (list-of-edge
// cardinality) — the four-way choice (OPTIONAL-introduced × var-length)
// routes through the constructors. 5xg (ADR 0008 amendment 2026-07-11):
// when the raw binding recorded a required same-Part bare re-reference,
// the constructed binding is post-mutated through the unexported
// per-variant markReferencedInRequiredBarePattern; the mutation is
// gated on the raw flag so untouched-common-case bindings are
// bit-identical to their pre-5xg values.
func (rb *rawBinding) toBinding() (query.Binding, error) {
	var b query.Binding
	var err error
	switch {
	case rb.kind == graph.Edge:
		// The single polarity flip from the listener's zero-value-safe inverted
		// rawBinding.undirected to the model's positive directed field lives here
		// (Stage 5 §4): directed = !undirected.
		directed := !rb.undirected
		switch {
		case rb.hops != nil && rb.optionalGroup > 0:
			b, err = query.NewNullableVarLengthEdgeBindingInGroup(rb.variable, rb.labels, rb.source, rb.target, directed, *rb.hops, rb.optionalGroup)
		case rb.hops != nil:
			b, err = query.NewVarLengthEdgeBinding(rb.variable, rb.labels, rb.source, rb.target, directed, *rb.hops)
		case rb.optionalGroup > 0:
			b, err = query.NewNullableEdgeBindingInGroup(rb.variable, rb.labels, rb.source, rb.target, directed, rb.optionalGroup)
		default:
			b, err = query.NewEdgeBinding(rb.variable, rb.labels, rb.source, rb.target, directed)
		}
	case rb.optionalGroup > 0:
		b, err = query.NewNullableNodeBindingInGroup(rb.variable, rb.labels, rb.optionalGroup)
	default:
		b, err = query.NewNodeBinding(rb.variable, rb.labels)
	}
	if err != nil {
		return nil, err
	}
	if rb.referencedInRequiredBarePattern {
		// 5xg: pointer receiver on the mutator forces a type-switch so the
		// local variable is addressable; mutation is committed back through
		// the interface via b = bb. The mutator is exported on the model
		// (query.MarkReferencedInRequiredBarePattern*) rather than unexported
		// because build.go lives outside package query — see ADR 0008 5xg
		// amendment; the exported symbol has no legitimate external caller
		// (the flag is only knowable at parser merge time) and the accessor
		// remains the only public read path.
		switch bb := b.(type) {
		case query.NodeBinding:
			query.MarkNodeBindingReferencedInRequiredBarePattern(&bb)
			b = bb
		case query.EdgeBinding:
			query.MarkEdgeBindingReferencedInRequiredBarePattern(&bb)
			b = bb
		}
	}
	return b, nil
}
