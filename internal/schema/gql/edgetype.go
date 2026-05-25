package gql

import (
	"github.com/antranig-yeretzian/gqlc/internal/grammar/gql/gen"
	"github.com/antranig-yeretzian/gqlc/internal/schema"
)

// rawEdge is an edge type collected during the walk, before resolution — one of
// the "raw" intermediate forms (with [rawNode] and [rawEndpoint]) that exist only
// between collection and resolve(). Its endpoints stay unresolved (as rawEndpoint)
// because a GQL graph type is an order-independent set: an endpoint may name a
// node type declared later in the body, so it can't be resolved until every node
// type is known (see resolve.go). source and target are already in canonical
// source->target order — the grammar normalizes a left-pointing arc, exposing the
// arrow's tail as the source.
type rawEdge struct {
	labels schema.LabelSet
	name   string
	props  map[string]schema.Property
	source rawEndpoint
	target rawEndpoint
}

// rawEndpoint is one end of a [rawEdge] before resolution: either an alias bound
// to a node type elsewhere in the body, or an inline node-type filler whose label
// set is its identity (inline properties are ignored — the filler only names which
// declared node type the endpoint is). It is its own type, rather than four loose
// fields on rawEdge, so source and target each read as a single endpoint.
type rawEndpoint struct {
	alias  string
	labels schema.LabelSet
}

// sourceRef and destRef read an edge endpoint in either form the grammar allows:
// a bound alias (preferred when present), or an inline node-type filler whose
// label set names the endpoint. They differ only in the alias accessor, which the
// generated source/destination reference types do not share.
func sourceRef(r gen.ISourceNodeTypeReferenceContext) rawEndpoint {
	if a := r.SourceNodeTypeAlias(); a != nil {
		return rawEndpoint{alias: a.GetText()}
	}
	return rawEndpoint{labels: fillerLabels(r.NodeTypeFiller())}
}

func destRef(r gen.IDestinationNodeTypeReferenceContext) rawEndpoint {
	if a := r.DestinationNodeTypeAlias(); a != nil {
		return rawEndpoint{alias: a.GetText()}
	}
	return rawEndpoint{labels: fillerLabels(r.NodeTypeFiller())}
}

// fillerLabels reads just the label set from an inline node-type filler used as
// an edge endpoint — the only part of the filler that is the endpoint's identity
// (inline properties on an endpoint are ignored).
//
// f is nil when the endpoint is written as empty parens `()`: the grammar makes
// the filler optional (`LEFT_PAREN nodeTypeFiller? RIGHT_PAREN`), so `()` with no
// alias and no filler is parseable. We don't enforce a non-nil invariant because
// that case is legitimately reachable and already handled: nil labels yield the
// empty label-set key, which matches no declared node type and surfaces as
// ErrUnknownEndpoint during resolution.
func fillerLabels(f gen.INodeTypeFillerContext) schema.LabelSet {
	if f == nil {
		return nil
	}
	ic := f.NodeTypeImpliedContent()
	if ic == nil {
		return nil
	}
	ls := ic.NodeTypeLabelSet()
	if ls == nil {
		return nil
	}
	return labelSet(ls.LabelSetPhrase())
}
