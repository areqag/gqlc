package cypher

import (
	"fmt"

	"github.com/antranig-yeretzian/gqlc/internal/grammar/cypher/gen"
	"github.com/antranig-yeretzian/gqlc/internal/graph"
	"github.com/antranig-yeretzian/gqlc/internal/query"
)

// collectPattern lowers one MATCH clause's comma-separated pattern parts into
// bindings. Each part is a chain of node patterns joined by relationship
// patterns; a named path (p = ...) is rejected (ErrUnsupportedPattern).
func (l *listener) collectPattern(p gen.IOC_PatternContext) {
	if p == nil || l.err != nil {
		return
	}
	for _, part := range p.AllOC_PatternPart() {
		if part.OC_Variable() != nil {
			l.fail(fmt.Errorf("%w: named path", ErrUnsupportedPattern))
			return
		}
		l.collectPatternElement(part.OC_AnonymousPatternPart().OC_PatternElement())
		if l.err != nil {
			return
		}
	}
}

// collectPatternElement lowers a single pattern element: a head node followed by
// zero or more (relationship, node) chain links. A parenthesised element
// ('(' patternElement ')') is unwrapped. Each chain link becomes an edge binding
// whose endpoints are the node on either side.
func (l *listener) collectPatternElement(e gen.IOC_PatternElementContext) {
	for e != nil && e.OC_NodePattern() == nil {
		e = e.OC_PatternElement() // unwrap '(' patternElement ')'
	}
	if e == nil {
		return
	}

	prev := e.OC_NodePattern()
	l.collectNode(prev)
	if l.err != nil {
		return
	}

	for _, link := range e.AllOC_PatternElementChain() {
		next := link.OC_NodePattern()
		// Record in textual first-appearance order: the relationship variable is
		// written before the node that follows it. collectEdge reads next only to
		// form the target endpoint; it does not need next's binding recorded first.
		l.collectEdge(link.OC_RelationshipPattern(), prev, next)
		if l.err != nil {
			return
		}
		l.collectNode(next)
		if l.err != nil {
			return
		}
		prev = next
	}
}

// collectNode records a node pattern. A named node is a binding (deduped, labels
// merged); an anonymous node is not a binding (C3) — its labels live inline on
// the edge endpoint, and a standalone anonymous node is a pure filter, ignored.
func (l *listener) collectNode(n gen.IOC_NodePatternContext) {
	if n == nil {
		return
	}
	variable := ""
	if v := n.OC_Variable(); v != nil {
		variable = v.GetText()
	}
	l.mineInlineMap(variable, n.OC_Properties())
	if variable != "" {
		l.mergeBinding(variable, graph.Node, nodeLabels(n.OC_NodeLabels()), nil, nil)
	}
}

// collectEdge records a relationship between prev and next as an edge binding.
// It rejects undirected and multi-type relationships, canonicalises a
// left-pointing arc to source->target, and forms each endpoint from its node
// (a VarEndpoint for a named node, an InlineEndpoint otherwise).
func (l *listener) collectEdge(r gen.IOC_RelationshipPatternContext, prev, next gen.IOC_NodePatternContext) {
	left := r.OC_LeftArrowHead() != nil
	right := r.OC_RightArrowHead() != nil
	if left == right {
		// Both heads (<-[]->) or neither (-[]-) is undirected; the schema is
		// directed-only (C5).
		l.fail(fmt.Errorf("%w: undirected relationship", ErrUnsupportedPattern))
		return
	}

	// Canonicalise: a right-pointing arc keeps prev->next; a left-pointing arc is
	// the edge next->prev (mirrors the schema side).
	srcNode, tgtNode := prev, next
	if left {
		srcNode, tgtNode = next, prev
	}
	source := l.endpoint(srcNode)
	target := l.endpoint(tgtNode)

	var variable string
	var labels graph.LabelSet
	if d := r.OC_RelationshipDetail(); d != nil {
		if v := d.OC_Variable(); v != nil {
			variable = v.GetText()
		}
		l.mineInlineMap(variable, d.OC_Properties())
		if l.err != nil {
			return
		}
		var ok bool
		labels, ok = relTypes(d.OC_RelationshipTypes())
		if !ok {
			l.fail(fmt.Errorf("%w: multi-type relationship", ErrUnsupportedPattern))
			return
		}
	}

	l.recordEndpointRefs(source, target)
	if variable == "" {
		// An anonymous edge is its own binding (C1): append the raw binding and let
		// build() construct it once, exactly as the named path does — no early
		// construct just to read back the (unchanged) labels.
		rb := &rawBinding{variable: "", kind: graph.Edge, source: source, target: target}
		rb.mergeLabels(labels)
		l.bindings = append(l.bindings, rb)
		return
	}
	l.mergeBinding(variable, graph.Edge, labels, source, target)
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
// resolve to a node binding.
func (l *listener) recordEndpointRefs(eps ...query.Endpoint) {
	for _, ep := range eps {
		if ve, ok := ep.(query.VarEndpoint); ok {
			l.refs = append(l.refs, varRef{name: ve.Variable(), endpointRef: true})
		}
	}
}

// mergeBinding records a binding for variable, deduping named bindings by
// variable in first-appearance order and unioning their labels (ordered, first
// appearance, C2). A variable seen as both a node and an edge is a kind conflict
// (recorded for build()). For an edge's first occurrence the endpoints are set;
// later occurrences merge labels only.
func (l *listener) mergeBinding(variable string, kind graph.EntityKind, labels graph.LabelSet, source, target query.Endpoint) {
	idx, ok := l.byVar[variable]
	if !ok {
		rb := &rawBinding{variable: variable, kind: kind, seen: map[string]bool{}, source: source, target: target}
		rb.mergeLabels(labels)
		l.byVar[variable] = len(l.bindings)
		l.bindings = append(l.bindings, rb)
		return
	}
	rb := l.bindings[idx]
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

// relTypes reads a relationship's types. A single type yields its label; an
// untyped relationship yields no labels; more than one type is unsupported (the
// model carries a single label set) and yields ok=false.
func relTypes(rt gen.IOC_RelationshipTypesContext) (graph.LabelSet, bool) {
	if rt == nil {
		return nil, true
	}
	names := rt.AllOC_RelTypeName()
	if len(names) > 1 {
		return nil, false
	}
	if len(names) == 0 {
		return nil, true
	}
	return graph.LabelSet{names[0].GetText()}, true
}
