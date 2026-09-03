// Package gql implements schema.Parser for GQL graph types: an ANTLR
// listener-driven walk collects raw element types, and a post-walk pass
// resolves edge endpoints (forward references are legal in a graph type body).
package gql

import (
	"github.com/areqag/gqlc/internal/grammar/gql/gen"
	"github.com/areqag/gqlc/internal/graph"
	"github.com/areqag/gqlc/internal/schema"
)

// rawEdge is an edge type collected during the walk, before resolution — one of
// the "raw" intermediate forms (with [rawNode] and [rawEndpoint]) that exist only
// between collection and resolve(). Its endpoints stay unresolved (as rawEndpoint)
// because a GQL graph type is an order-independent set: an endpoint may name a
// node type declared later in the body, so it can't be resolved until every node
// type is known (see resolve.go). source and target are already in canonical
// source->target order — the grammar normalises a left-pointing arc, exposing the
// arrow's tail as the source.
//
// Its two label fields split at `=>` exactly as [rawNode]'s do; see there for why
// the key/implied split is preserved into the raw form instead of being collapsed
// during the walk.
type rawEdge struct {
	hasKeyLabelSet bool
	keyLabels      graph.LabelSet
	impliedLabels  graph.LabelSet
	name           string
	props          map[string]schema.Property
	source         rawEndpoint
	target         rawEndpoint
}

// rawEndpoint is one end of a [rawEdge] before resolution: either an alias bound
// to a node type elsewhere in the body, or an inline node-type filler whose label
// set is its identity (inline properties are ignored — the filler only names which
// declared node type the endpoint is). It is its own type, rather than four loose
// fields on rawEdge, so source and target each read as a single endpoint.
type rawEndpoint struct {
	alias  string
	labels graph.LabelSet
}

// sourceRef and destRef read an edge endpoint in either form the grammar allows:
// a bound alias (preferred when present), or an inline node-type filler whose
// label set names the endpoint. They differ only in the alias accessor, which the
// generated source/destination reference types do not share. The error is
// forwarded from fillerLabels — an inline filler that carries a property-type
// spec has no consumer for those declarations and is rejected as a sentinel
// rather than parsed and silently discarded.
func sourceRef(r gen.ISourceNodeTypeReferenceContext) (rawEndpoint, error) {
	if a := r.SourceNodeTypeAlias(); a != nil {
		return rawEndpoint{alias: a.GetText()}, nil
	}
	labels, err := fillerLabels(r.NodeTypeFiller())
	if err != nil {
		return rawEndpoint{}, err
	}
	return rawEndpoint{labels: labels}, nil
}

func destRef(r gen.IDestinationNodeTypeReferenceContext) (rawEndpoint, error) {
	if a := r.DestinationNodeTypeAlias(); a != nil {
		return rawEndpoint{alias: a.GetText()}, nil
	}
	labels, err := fillerLabels(r.NodeTypeFiller())
	if err != nil {
		return rawEndpoint{}, err
	}
	return rawEndpoint{labels: labels}, nil
}

// fillerLabels reads the label set that names the node type an inline endpoint
// refers to. The endpoint is a reference to a node type declared elsewhere in the
// same graph type (CONTEXT.md: "resolved to the referenced node type's label
// set"), and since gqlc-h9n.8 a node type is named by its KEY label set — so an
// endpoint written with `=>` is identified by the phrase before it, and only a
// `=>`-less one falls back to the implied content under GG22.
//
// Everything else the grammar admits inside a filler is rejected rather than
// dropped. Property types have no consumer here and were silently discarded until
// gqlc-h9n.18; implied labels are the same defect wearing GG21's syntax, because
// `(:Person => :Employee)` as an endpoint asserts the referenced type carries
// Employee and nothing checks that against the actual declaration. Both are
// rejected so the assertion cannot be quietly ignored. Note the implied-label
// rejection needs the key label set to be present: without one the implied
// content IS the endpoint's label set, which is the ordinary `(:Person)` spelling.
//
// f is nil when the endpoint is written as empty parens `()`: the grammar makes
// the filler optional (`LEFT_PAREN nodeTypeFiller? RIGHT_PAREN`), so `()` with no
// alias and no filler is parseable. We don't enforce a non-nil invariant because
// that case is legitimately reachable and already handled: nil labels yield the
// empty label-set key, which matches no declared node type and surfaces as
// ErrUnknownEndpoint during resolution.
func fillerLabels(f gen.INodeTypeFillerContext) (graph.LabelSet, error) {
	if f == nil {
		return nil, nil
	}

	var impliedLabels gen.INodeTypeLabelSetContext
	// The implied content is absent for a key-label-set-only filler such as
	// `(:Person =>)`, which still names a node type — so read the key label set
	// outside this block rather than bailing here.
	if ic := f.NodeTypeImpliedContent(); ic != nil {
		if pts := ic.NodeTypePropertyTypes(); pts != nil {
			if spec := pts.PropertyTypesSpecification(); spec != nil {
				// The nil check carries the whole test. `{ }` yields a nil list
				// rather than an empty one: propertyTypeList is optional inside
				// the braces and requires at least one propertyType when present
				// (GQL.g4:1691-1697), so a non-nil list can never be empty.
				if list := spec.PropertyTypeList(); list != nil {
					return nil, ErrEndpointFillerHasProperties
				}
			}
		}
		impliedLabels = ic.NodeTypeLabelSet()
	}

	if kls := f.NodeTypeKeyLabelSet(); kls != nil {
		if impliedLabels != nil {
			return nil, ErrEndpointFillerImpliesLabels
		}
		return labelSet(kls.LabelSetPhrase())
	}
	if impliedLabels == nil {
		return nil, nil
	}
	return labelSet(impliedLabels.LabelSetPhrase())
}
