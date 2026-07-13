package cypher

import (
	"fmt"
	"strconv"

	"github.com/antlr4-go/antlr/v4"

	"github.com/areqag/gqlc/internal/grammar/cypher/gen"
	"github.com/areqag/gqlc/internal/graph"
	"github.com/areqag/gqlc/internal/query"
)

// isDotDotToken reports whether tree is the '..' terminal in an oC_RangeLiteral —
// the grammar token that separates the lower bound from the upper. The
// generated token constant is CypherParserT__11 (=12); routing through the
// ANTLR TerminalNode/Symbol interface keeps this test structural.
func isDotDotToken(tree antlr.Tree) bool {
	tn, ok := tree.(antlr.TerminalNode)
	if !ok {
		return false
	}
	return tn.GetSymbol().GetTokenType() == gen.CypherParserT__11
}

// collectPattern lowers one MATCH clause's comma-separated pattern parts into
// bindings. Each part is a chain of node patterns joined by relationship
// patterns. A named path (p = ...) contributes its member bindings the same
// way plus a PathBinding whose Members list is the shape-faithful, tagged-sum
// sequence of the path's elements in textual order (Stage 8 spec §1.2, §1.5):
// named nodes/edges surface as NamedNodeMember / NamedEdgeMember, and
// anonymous edges and anonymous intermediate nodes surface as
// AnonEdgeMember / AnonNodeMember slots (the anonymous slots carry no name,
// so they never collide with a user variable in the byVar namespace).
// group is the introducing OPTIONAL MATCH clause's query-scoped id, or 0 for
// a required clause (ay9): any binding first introduced here records it and
// is nullable iff group ≥ 1 (ADR 0006).
func (l *listener) collectPattern(p gen.IOC_PatternContext, group int) {
	if p == nil || l.err != nil {
		return
	}
	for _, part := range p.AllOC_PatternPart() {
		l.collectPatternPart(part, group)
		if l.err != nil {
			return
		}
	}
}

// collectPatternPart lowers a single oC_PatternPart. Factored out of
// collectPattern so MERGE (which admits exactly one oC_PatternPart per the
// grammar — Cypher.g4 §oC_Merge) can share the code path without the outer
// comma-list loop.
func (l *listener) collectPatternPart(part gen.IOC_PatternPartContext, group int) {
	if part == nil {
		return
	}
	var pathVar string
	if v := part.OC_Variable(); v != nil {
		pathVar = v.GetText()
	}
	var pathMembers []query.PathMember
	if pathVar != "" {
		// Only accumulate the members list when the pattern part is a
		// named path — collectPatternElement records to this slice
		// alongside the part's raw bindings when non-nil.
		pathMembers = make([]query.PathMember, 0, 8)
		l.curPart.pathMemberSink = &pathMembers
	}
	l.collectPatternElement(part.OC_AnonymousPatternPart().OC_PatternElement(), group)
	l.curPart.pathMemberSink = nil
	if l.err != nil {
		return
	}
	if pathVar != "" {
		// Three-way collision sweep: an existing UnwindBinding with the same
		// name is a kind conflict (path vs unwind), symmetric with the
		// pathBindings-vs-byVar check in buildPart. byVar and byVar-vs-path
		// are handled elsewhere; this catches the path-vs-unwind direction
		// at listener time so the fail-site stays local to the offending
		// clause (spec §4.3 amend).
		for _, ub := range l.curPart.unwindBindings {
			if ub.Variable() == pathVar {
				l.fail(fmt.Errorf("%w: %s", ErrVariableKindConflict, pathVar))
				return
			}
		}
		pb, err := query.NewPathBinding(pathVar, pathMembers)
		if err != nil {
			l.fail(err)
			return
		}
		l.curPart.pathBindings = append(l.curPart.pathBindings, pb)
	}
}

// recordPathNode appends a node member for the current pattern position onto
// the current named-path member sink (a no-op outside a named path). A named
// node contributes its NamedNodeMember; an anonymous node contributes an
// AnonNodeMember placeholder so the members list is shape-faithful.
func (l *listener) recordPathNode(variable string) {
	if l.curPart.pathMemberSink == nil {
		return
	}
	if variable == "" {
		*l.curPart.pathMemberSink = append(*l.curPart.pathMemberSink, query.AnonNodeMember{})
		return
	}
	m, err := query.NewNamedNodeMember(variable)
	if err != nil {
		l.fail(err)
		return
	}
	*l.curPart.pathMemberSink = append(*l.curPart.pathMemberSink, m)
}

// recordPathEdge appends an edge member for the current pattern position onto
// the current named-path member sink (a no-op outside a named path). A named
// edge contributes its NamedEdgeMember; an anonymous edge contributes an
// AnonEdgeMember slot (no name, so it never competes with a user variable
// in the byVar namespace — the fix for the pre-fix synthetic-name collision).
func (l *listener) recordPathEdge(variable string) {
	if l.curPart.pathMemberSink == nil {
		return
	}
	if variable == "" {
		*l.curPart.pathMemberSink = append(*l.curPart.pathMemberSink, query.AnonEdgeMember{})
		return
	}
	m, err := query.NewNamedEdgeMember(variable)
	if err != nil {
		l.fail(err)
		return
	}
	*l.curPart.pathMemberSink = append(*l.curPart.pathMemberSink, m)
}

// collectPatternElement lowers a single pattern element: a head node followed by
// zero or more (relationship, node) chain links. A parenthesised element
// ('(' patternElement ')') is unwrapped. Each chain link becomes an edge binding
// whose endpoints are the node on either side. group flows through so any
// binding first introduced here records its OPTIONAL group (0 = required).
func (l *listener) collectPatternElement(e gen.IOC_PatternElementContext, group int) {
	for e != nil && e.OC_NodePattern() == nil {
		e = e.OC_PatternElement() // unwrap '(' patternElement ')'
	}
	if e == nil {
		return
	}

	// 5xg: the head node is "bare" iff no chain link follows it. A node
	// with an adjacent edge chain link on either side is never bare (§2.3);
	// non-head nodes in the loop below always have the immediately-preceding
	// chain link on their left, so they pass bare=false unconditionally.
	chain := e.AllOC_PatternElementChain()
	headBare := len(chain) == 0

	prev := e.OC_NodePattern()
	l.collectNode(prev, group, headBare)
	if l.err != nil {
		return
	}

	for _, link := range chain {
		next := link.OC_NodePattern()
		// Record in textual first-appearance order: the relationship variable is
		// written before the node that follows it. collectEdge reads next only to
		// form the target endpoint; it does not need next's binding recorded first.
		l.collectEdge(link.OC_RelationshipPattern(), prev, next, group)
		if l.err != nil {
			return
		}
		l.collectNode(next, group, false)
		if l.err != nil {
			return
		}
		prev = next
	}
}

// collectNode records a node pattern. A named node is a binding (deduped, labels
// merged); an anonymous node is not a binding (C3) — its labels live inline on
// the edge endpoint, and a standalone anonymous node is a pure filter, ignored.
// Inside a named path (pathMemberSink is non-nil), the node also contributes
// a member entry so the path's Members list is shape-faithful. Stage 9: when
// the variable is already bound in the current part as an UNWIND binding, the
// MATCH occurrence is a constraint on that existing name (the UNWIND element
// type may itself be a node — a `list<node>` unwound yields node-typed values,
// so MATCH-reuse is legitimate) — the parser does not emit a fresh NodeBinding;
// the endpoint / path-member is recorded against the existing binding via the
// shared name. A path binding deliberately does NOT trigger the skip: a named
// path reused as a node/edge pattern is a compile-time kind conflict per
// openCypher (a path is never a node/edge), so the existing buildPart
// pathBindings-vs-byVar collision check must fire.
func (l *listener) collectNode(n gen.IOC_NodePatternContext, group int, bare bool) {
	if n == nil {
		return
	}
	variable := ""
	if v := n.OC_Variable(); v != nil {
		variable = v.GetText()
	}
	l.mineInlineMap(variable, n.OC_Properties())
	if variable != "" && !l.nameBoundAsUnwind(variable) {
		l.mergeBinding(variable, graph.Node, nodeLabels(n.OC_NodeLabels()), nil, nil, group, false, nil, bare)
	}
	l.recordPathNode(variable)
}

// nameBoundAsUnwind reports whether a variable is already bound in the
// current part as an UNWIND binding whose element type could plausibly
// stand in for the pattern position (node or edge). The three-way gate
// — TypeNode, TypeEdge, TypeUnknown — is the correctness fix (Stage 9
// fix round, B2): a scalar-elemType UNWIND is not a legitimate
// pattern-position source, so the skip must not fire and the reuse must
// fall through to mergeBinding → byVar collision → ErrVariableKindConflict.
// Without the gate, a MATCH after `UNWIND [1,2] AS x` silently discarded
// the node/edge binding (label constraints included), and the resolver
// saw an unrelated a and b as if the edge did not exist.
//
// TypeNode / TypeEdge / TypeUnknown are the safe passes:
//   - TypeNode / TypeEdge: the concrete list-of-entity case
//     (`WITH collect(n) AS ns UNWIND ns AS m MATCH (m)`);
//   - TypeUnknown: the honest posture the Stage-6 typer records when
//     the source expression's element type cannot be pinned (aggregate
//     identity below the boundary, ADR 0005), and the resolver upgrades
//     from the schema.
//
// Any other concrete elemType (int, string, bool, list<…>, temporal, …)
// is definitely not a node or an edge, and the parser rejects at
// compile time — the byVar collision is the fail-site.
//
// Path bindings deliberately do NOT trigger this skip: a named-path
// variable reused as a node/edge pattern is a **compile-time** kind
// conflict per openCypher (a path is never a node/edge), so the
// existing buildPart pathBindings-vs-byVar collision check must fire.
func (l *listener) nameBoundAsUnwind(variable string) bool {
	for _, ub := range l.curPart.unwindBindings {
		if ub.Variable() != variable {
			continue
		}
		switch ub.ElementType().(type) {
		case query.TypeNode, query.TypeEdge, query.TypeUnknown:
			return true
		}
		return false
	}
	return false
}

// collectEdge records a relationship between prev and next as an edge binding.
// Multi-type relationships collect every type in textual first-appearance
// order onto the binding's LabelSet (Stage 8); variable-length relationships
// carry a non-nil hops range (Stage 8). A directed left-arc is canonicalised
// to source->target, while an undirected edge keeps textual order with the
// undirected flag set (Stage 5). Each endpoint is formed from its node (a
// VarEndpoint for a named node, an InlineEndpoint otherwise). group marks any
// edge binding (named or anonymous) introduced here with its OPTIONAL clause's
// id (0 = required clause; nullable ⇔ group ≥ 1).
func (l *listener) collectEdge(r gen.IOC_RelationshipPatternContext, prev, next gen.IOC_NodePatternContext, group int) {
	left := r.OC_LeftArrowHead() != nil
	right := r.OC_RightArrowHead() != nil
	// One arrow (left != right) is directed; both heads (<-[]->) or neither (-[]-)
	// is undirected — openCypher treats both spellings the same (Stage 5).
	directed := left != right

	// Canonicalise only a directed left-pointing arc to next->prev (mirrors the
	// schema side). An undirected edge keeps textual order prev->next — note <-->
	// has left==true, so the directed guard is required to avoid flipping it.
	srcNode, tgtNode := prev, next
	if directed && left {
		srcNode, tgtNode = next, prev
	}
	source := l.endpoint(srcNode)
	target := l.endpoint(tgtNode)

	var variable string
	var labels graph.LabelSet
	var hops *query.EdgeHops
	if d := r.OC_RelationshipDetail(); d != nil {
		if v := d.OC_Variable(); v != nil {
			variable = v.GetText()
		}
		l.mineInlineMap(variable, d.OC_Properties())
		if l.err != nil {
			return
		}
		labels = relTypes(d.OC_RelationshipTypes())
		if rl := d.OC_RangeLiteral(); rl != nil {
			h, err := edgeHopsFromRangeLiteral(rl)
			if err != nil {
				l.fail(err)
				return
			}
			hops = &h
		}
	}

	l.recordEndpointRefs(source, target)
	if variable == "" {
		// An anonymous edge is its own binding (C1): append the raw binding and let
		// build() construct it once, exactly as the named path does — no early
		// construct just to read back the (unchanged) labels. Anonymous edges
		// introduced inside OPTIONAL MATCH carry the group id (and thus the
		// nullable flag) uniformly (ADR 0006; ay9) even though no Ref will ever
		// observe it.
		rb := &rawBinding{variable: "", kind: graph.Edge, source: source, target: target, optionalGroup: group, undirected: !directed, hops: hops}
		rb.mergeLabels(labels)
		l.curPart.bindings = append(l.curPart.bindings, rb)
		l.recordPathEdge("")
		return
	}
	if !l.nameBoundAsUnwind(variable) {
		// 5xg: an edge is grammatically never bare — it always sits inside
		// -[...]- between two node positions — so the parameter is a
		// compile-time constant false at this site.
		l.mergeBinding(variable, graph.Edge, labels, source, target, group, !directed, hops, false)
	}
	l.recordPathEdge(variable)
}

// edgeHopsFromRangeLiteral reads a variable-length relationship's hop range
// from the grammar's oC_RangeLiteral rule (Stage 8 spec §3.3). The rule shape
// is `'*' SP? (IntegerLiteral SP?)? ('..' SP? (IntegerLiteral SP?)?)?`, so the
// integer literals appear zero, one, or two times, and the '..' terminal (T__11)
// discriminates the fixed-count case (one integer, no '..') from the
// lower-bound-only case (one integer, '..' present but no upper). Walk the
// direct children and pair each integer with its position relative to the '..'.
func edgeHopsFromRangeLiteral(rl gen.IOC_RangeLiteralContext) (query.EdgeHops, error) {
	var minPtr, maxPtr *int
	dotsSeen := false
	for i := 0; i < rl.GetChildCount(); i++ {
		child := rl.GetChild(i)
		if intLit, ok := child.(gen.IOC_IntegerLiteralContext); ok {
			n, err := strconv.Atoi(intLit.GetText())
			if err != nil {
				return query.EdgeHops{}, fmt.Errorf("query: invalid integer in hop range: %w", err)
			}
			if !dotsSeen {
				v := n
				minPtr = &v
				continue
			}
			v := n
			maxPtr = &v
			continue
		}
		// The '..' terminal is CypherParserT__11; every other terminal is SP or '*'.
		if isDotDotToken(child) {
			dotsSeen = true
		}
	}
	// Fixed-count case: `*3` (one integer, no '..') → min = max.
	if !dotsSeen && minPtr != nil {
		v := *minPtr
		maxPtr = &v
	}
	return query.NewEdgeHops(minPtr, maxPtr)
}

// endpoint forms an edge endpoint from a node pattern: a VarEndpoint for a named
// node (its labels live on that node's binding, C4), an InlineEndpoint carrying
// the node's inline labels otherwise (empty labels for the () case).
func (l *listener) endpoint(n gen.IOC_NodePatternContext) query.Endpoint {
	if v := n.OC_Variable(); v != nil {
		e, err := query.NewVarEndpoint(v.GetText())
		if err != nil {
			l.fail(err)
			return nil
		}
		return e
	}
	return query.NewInlineEndpoint(nodeLabels(n.OC_NodeLabels()))
}

// recordEndpointRefs notes each named endpoint as a reference build() must
// resolve to a node binding, scoped to the current part.
func (l *listener) recordEndpointRefs(eps ...query.Endpoint) {
	for _, ep := range eps {
		if ve, ok := ep.(query.VarEndpoint); ok {
			l.curPart.refs = append(l.curPart.refs, varRef{name: ve.Variable(), endpointRef: true})
		}
	}
}

// mergeBinding records a binding for variable in the current part, deduping the
// part's named bindings by variable in first-appearance order and unioning their
// labels (ordered, first appearance, C2). Dedup is per-part: a name re-MATCHed in
// a later part is a fresh binding there (spec §3). A variable seen as both a node
// and an edge within a part is a kind conflict (recorded for build()). For an
// edge's first occurrence the endpoints are set; later occurrences merge labels
// only. group is honoured only on first introduction (ADR 0006; ay9): a
// binding's nullability and OPTIONAL-group membership are static facts about
// its *introducing* clause; a later non-OPTIONAL occurrence neither sets nor
// clears them — that demotion is the resolver's job (gqlc-lqm). Stage 8: hops
// carries the var-length hop range (nil for single-hop); it is honoured only
// on first introduction, matching the group/directed discipline.
func (l *listener) mergeBinding(variable string, kind graph.EntityKind, labels graph.LabelSet, source, target query.Endpoint, group int, undirected bool, hops *query.EdgeHops, bare bool) {
	if l.suppressed() {
		return
	}
	part := l.curPart
	idx, ok := part.byVar[variable]
	if !ok {
		rb := &rawBinding{variable: variable, kind: kind, seen: map[string]bool{}, source: source, target: target, optionalGroup: group, undirected: undirected, hops: hops}
		rb.mergeLabels(labels)
		part.byVar[variable] = len(part.bindings)
		part.bindings = append(part.bindings, rb)
		return
	}
	rb := part.bindings[idx]
	if rb.kind != kind {
		l.fail(fmt.Errorf("%w: %q", ErrVariableKindConflict, variable))
		return
	}
	rb.mergeLabels(labels)
	if group == 0 && bare {
		// 5xg: the current occurrence is a required (non-OPTIONAL) bare
		// pattern re-reference of a binding that was previously introduced.
		// The flag is monotone (once set, stays true), so repeated bare
		// re-references are idempotent. The first-introduction arm above
		// never sets it — by definition, no re-reference has occurred yet.
		rb.referencedInRequiredBarePattern = true
	}
}

// mergeLabels adds labels not already present, preserving first-appearance order
// for deterministic golden output (C2).
func (rb *rawBinding) mergeLabels(labels graph.LabelSet) {
	if rb.seen == nil {
		rb.seen = map[string]bool{}
	}
	for _, label := range labels {
		if rb.seen[label] {
			continue
		}
		rb.seen[label] = true
		rb.labels = append(rb.labels, label)
	}
}

// nodeLabels reads a node's conjunctive labels in source order.
func nodeLabels(ls gen.IOC_NodeLabelsContext) graph.LabelSet {
	if ls == nil {
		return nil
	}
	var out graph.LabelSet
	for _, nl := range ls.AllOC_NodeLabel() {
		out = append(out, nl.OC_LabelName().GetText())
	}
	return out
}

// relTypes reads a relationship's types (Stage 8): every named type joins the
// LabelSet in textual first-appearance order. An untyped relationship yields
// no labels; a single-type edge yields one label; a multi-type edge
// (`[r:A|B|C]`) yields the ordered set of every type it mentions.
func relTypes(rt gen.IOC_RelationshipTypesContext) graph.LabelSet {
	if rt == nil {
		return nil
	}
	names := rt.AllOC_RelTypeName()
	if len(names) == 0 {
		return nil
	}
	out := make(graph.LabelSet, 0, len(names))
	for _, n := range names {
		out = append(out, n.GetText())
	}
	return out
}
