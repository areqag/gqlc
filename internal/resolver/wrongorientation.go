package resolver

import (
	"fmt"

	"github.com/areqag/gqlc/internal/graph"
	"github.com/areqag/gqlc/internal/query"
	"github.com/areqag/gqlc/internal/schema"
)

// orientationEvidence is what edge closure observed about the relationship
// types a query named: the ones that reached some committed candidate set, and
// the ones dropped in the single shape this detector accuses.
//
// It is collected during closure rather than reconstructed afterwards because
// closure is the only place both halves are in hand at once — the committed
// candidate set, and the endpoint keys still carrying the covers bit that
// clause 3 reads. By the time Resolve consumes one of these it has absorbed
// every Part of every branch, which is what makes the survival rule query-wide.
type orientationEvidence struct {
	// survived is every relationship type name that reached some edge's
	// committed candidate set, anywhere in the query.
	survived map[string]bool
	// drops are the accusations that passed clauses 1-4, in first-appearance
	// order. Clause 5 is applied by wrongOrientationDropWarnings, not here: a
	// type dropped in Part 1 may still survive on an edge in Part 2, and no
	// recorder can know that at the moment it records.
	drops []orientationDrop
}

// orientationDrop is one candidate accusation. reversed holds the declared keys
// that witness the wrong arrow — the message is unactionable without them,
// because the author cannot otherwise tell a mis-drawn arrow from a stale type.
type orientationDrop struct {
	label    string
	edge     query.EdgeBinding
	reversed []schema.EdgeKey
}

// absorb folds another Part's or branch's evidence into this one.
func (ev *orientationEvidence) absorb(o orientationEvidence) {
	for l := range o.survived {
		if ev.survived == nil {
			ev.survived = make(map[string]bool, len(o.survived))
		}
		ev.survived[l] = true
	}
	ev.drops = append(ev.drops, o.drops...)
}

// recordEdge is the closure-time half of the detector, run at both closeEdge
// call sites in CloseEdges once the close has SUCCEEDED. A refused edge records
// nothing: it already has its error, and an edge collecting both a refusal and
// a warning would be two verdicts on one fact.
//
// Two of the six clauses are absent from the code because the clauses that are
// present already imply them:
//
//   - "L is declared somewhere in the schema" is implied by the reversed-key
//     probe below, whose hit IS a declaration of L.
//   - "the edge is directed" is implied by the drop plus the reversed probe: an
//     undirected edge probes both orientations, so a declared reversed key would
//     have been in its own candidate set and L would not have dropped.
func (ev *orientationEvidence) recordEdge(e query.EdgeBinding, src, tgt endpointKeys, s schema.Schema) {
	srcs, tgts := src.declared(), tgt.declared()
	survivors := make(map[graph.LabelSetKey]bool, len(e.Labels()))
	for _, k := range edgeCandidates(e, srcs, tgts, s) {
		survivors[k.KeyLabels] = true
	}

	for _, l := range e.Labels() {
		if survivors[graph.LabelSet{l}.Key()] {
			if ev.survived == nil {
				ev.survived = make(map[string]bool, len(e.Labels()))
			}
			ev.survived[l] = true
			continue
		}
		// Clause 3. An endpoint that reports only the keys it happens to know —
		// a WITH carry, an uncovered Phase-B inference — cannot support an
		// accusation: the drop may be an artifact of what the resolver failed to
		// learn rather than of the arrow the author drew.
		if !src.covers || !tgt.covers {
			continue
		}
		rev := reversedDeclarations(l, srcs, tgts, s)
		if len(rev) == 0 {
			continue
		}
		ev.drops = append(ev.drops, orientationDrop{label: l, edge: e, reversed: rev})
	}
}

// reversedDeclarations is clause 4: the declared keys naming L between this
// edge's committed endpoints with the ends swapped. Order follows the endpoint
// slices, which are themselves ordered, so the message is stable across runs.
func reversedDeclarations(l string, srcs, tgts []graph.LabelSetKey, s schema.Schema) []schema.EdgeKey {
	labelKey := graph.LabelSet{l}.Key()
	var out []schema.EdgeKey
	seen := make(map[schema.EdgeKey]bool)
	for _, src := range srcs {
		for _, tgt := range tgts {
			k := schema.EdgeKey{Source: tgt, KeyLabels: labelKey, Target: src}
			if _, ok := s.Edges[k]; !ok || seen[k] {
				continue
			}
			seen[k] = true
			out = append(out, k)
		}
	}
	return out
}

// wrongOrientationDropWarnings applies clause 5 and renders what survives it.
//
// Clause 5 — the type reached no candidate set anywhere in the query — is what
// keeps the mirrored-alternation idiom silent: an author who writes the same
// AUTHORED|LIKES alternation on both orientations across two MATCHes means the
// drop on each, and accusing them would fire on 9 corpus cells that are all
// that one legitimate shape.
//
// Deduplicated by type name in first-appearance order, on the same reasoning as
// ADR 0032's lane: one mis-drawn arrow repeated across three MATCHes is one
// mistake.
func wrongOrientationDropWarnings(ev orientationEvidence) []Warning {
	var out []Warning
	seen := make(map[string]bool)
	for _, d := range ev.drops {
		if ev.survived[d.label] || seen[d.label] {
			continue
		}
		seen[d.label] = true
		out = append(out, Warning{
			Producer: producerWrongOrientationDrop,
			Text:     wrongOrientationDropMessage(d),
		})
	}
	return out
}

// wrongOrientationDropMessage words one warning. Like ADR 0032's it must be
// actionable read alone on stderr, so it names the type, places the edge in the
// author's own query text, says what gqlc did, shows the reversed declaration
// that witnesses the wrong arrow, and offers both remedies — gqlc cannot tell a
// mis-drawn arrow from a stale type and must not pretend to.
func wrongOrientationDropMessage(d orientationDrop) string {
	return fmt.Sprintf(
		"warning: relationship type %q on %s is declared only in the opposite orientation (%s); the pattern's arrow drops it and no decoder is generated for it, so a row of that type fails at runtime. Flip the arrow if the direction is the mistake, or remove %[1]q from the pattern if the type is stale.",
		d.label, describeEdgeBinding(d.edge), formatEdgeKeys(d.reversed))
}
