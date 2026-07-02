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
// way plus a PathBinding capturing the path variable and every member's name
// in textual order (Stage 8 spec §1.5). optional flags an OPTIONAL MATCH
// clause: any binding first introduced here is marked nullable (ADR 0006).
func (l *listener) collectPattern(p gen.IOC_PatternContext, optional bool) {
	if p == nil || l.err != nil {
		return
	}
	for _, part := range p.AllOC_PatternPart() {
		var pathVar string
		if v := part.OC_Variable(); v != nil {
			pathVar = v.GetText()
		}
		before := len(l.curPart.bindings)
		l.collectPatternElement(part.OC_AnonymousPatternPart().OC_PatternElement(), optional)
		if l.err != nil {
			return
		}
		if pathVar != "" {
			members := l.pathMemberNames(l.curPart.bindings[before:])
			pb, err := query.NewPathBinding(pathVar, members)
			if err != nil {
				l.fail(err)
				return
			}
			l.curPart.pathBindings = append(l.curPart.pathBindings, pb)
		}
	}
}

// pathMemberNames reads the ordered member names of a named path from the
// raw bindings the pattern element just added. Named members contribute their
// variable directly; an anonymous edge inside a named path is given a
// synthetic name (Stage 8 spec §1.2, §8) so every member on the path binding
// carries a name — the synthetic name is scoped per part (l.curPart.anonEdges
// counts anonymous edges seen inside named paths in the current part).
func (l *listener) pathMemberNames(added []*rawBinding) []string {
	out := make([]string, 0, len(added))
	for _, rb := range added {
		if rb.variable != "" {
			out = append(out, rb.variable)
			continue
		}
		// Anonymous edge inside a named path: mint a synthetic name and
		// record it back on the raw binding so build() keeps the (anonymous)
		// wire shape (variable stays "") but the path member list carries a
		// stable identifier the resolver can trace.
		name := "__anon_edge_" + strconv.Itoa(l.curPart.anonEdges)
		l.curPart.anonEdges++
		rb.pathMemberName = name
		out = append(out, name)
	}
	return out
}

// collectPatternElement lowers a single pattern element: a head node followed by
// zero or more (relationship, node) chain links. A parenthesised element
// ('(' patternElement ')') is unwrapped. Each chain link becomes an edge binding
// whose endpoints are the node on either side. optional flows through so any
// binding first introduced here is marked nullable.
func (l *listener) collectPatternElement(e gen.IOC_PatternElementContext, optional bool) {
	for e != nil && e.OC_NodePattern() == nil {
		e = e.OC_PatternElement() // unwrap '(' patternElement ')'
	}
	if e == nil {
		return
	}

	prev := e.OC_NodePattern()
	l.collectNode(prev, optional)
	if l.err != nil {
		return
	}

	for _, link := range e.AllOC_PatternElementChain() {
		next := link.OC_NodePattern()
		// Record in textual first-appearance order: the relationship variable is
		// written before the node that follows it. collectEdge reads next only to
		// form the target endpoint; it does not need next's binding recorded first.
		l.collectEdge(link.OC_RelationshipPattern(), prev, next, optional)
		if l.err != nil {
			return
		}
		l.collectNode(next, optional)
		if l.err != nil {
			return
		}
		prev = next
	}
}

// collectNode records a node pattern. A named node is a binding (deduped, labels
// merged); an anonymous node is not a binding (C3) — its labels live inline on
// the edge endpoint, and a standalone anonymous node is a pure filter, ignored.
func (l *listener) collectNode(n gen.IOC_NodePatternContext, optional bool) {
	if n == nil {
		return
	}
	variable := ""
	if v := n.OC_Variable(); v != nil {
		variable = v.GetText()
	}
	l.mineInlineMap(variable, n.OC_Properties())
	if variable != "" {
		l.mergeBinding(variable, graph.Node, nodeLabels(n.OC_NodeLabels()), nil, nil, optional, false, nil)
	}
}

// collectEdge records a relationship between prev and next as an edge binding.
// Multi-type relationships collect every type in textual first-appearance
// order onto the binding's LabelSet (Stage 8); variable-length relationships
// carry a non-nil hops range (Stage 8). A directed left-arc is canonicalised
// to source->target, while an undirected edge keeps textual order with the
// undirected flag set (Stage 5). Each endpoint is formed from its node (a
// VarEndpoint for a named node, an InlineEndpoint otherwise). optional marks any
// edge binding (named or anonymous) introduced here as nullable.
func (l *listener) collectEdge(r gen.IOC_RelationshipPatternContext, prev, next gen.IOC_NodePatternContext, optional bool) {
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
		// introduced inside OPTIONAL MATCH carry the nullable flag uniformly
		// (ADR 0006) even though no Ref will ever observe it.
		rb := &rawBinding{variable: "", kind: graph.Edge, source: source, target: target, nullable: optional, undirected: !directed, hops: hops}
		rb.mergeLabels(labels)
		l.curPart.bindings = append(l.curPart.bindings, rb)
		return
	}
	l.mergeBinding(variable, graph.Edge, labels, source, target, optional, !directed, hops)
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
// only. optional is honoured only on first introduction (ADR 0006): a binding's
// nullability is a static fact about its *introducing* clause; a later
// non-OPTIONAL occurrence neither sets nor clears the flag — that demotion is the
// resolver's job (gqlc-lqm). Stage 8: hops carries the var-length hop range
// (nil for single-hop); it is honoured only on first introduction, matching
// the nullable/directed discipline.
func (l *listener) mergeBinding(variable string, kind graph.EntityKind, labels graph.LabelSet, source, target query.Endpoint, optional, undirected bool, hops *query.EdgeHops) {
	part := l.curPart
	idx, ok := part.byVar[variable]
	if !ok {
		rb := &rawBinding{variable: variable, kind: kind, seen: map[string]bool{}, source: source, target: target, nullable: optional, undirected: undirected, hops: hops}
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
