package gql

import (
	"github.com/antranig-yeretzian/gqlc/internal/grammar/gql/gen"
	"github.com/antranig-yeretzian/gqlc/internal/schema"
)

// rawNode is a node type collected during the walk, before resolution — one of
// the "raw" intermediate forms (with [rawEdge] and [rawEndpoint]) that exist only
// between collection and resolve(). Its labels are still a raw LabelSet, not yet a
// canonical key, and it keeps the declaration-local alias, which is used only to
// resolve edge endpoints and is never part of the persisted identity.
type rawNode struct {
	labels schema.LabelSet
	name   string
	alias  string
	props  map[string]schema.Property
}

// labelSet reads the labels off a label set phrase: either a single LABEL form
// (`:Person`) or an ampersand-joined set (`:Person&Employee`).
func labelSet(p gen.ILabelSetPhraseContext) schema.LabelSet {
	if p == nil {
		return nil
	}
	if name := p.LabelName(); name != nil {
		return schema.LabelSet{name.GetText()}
	}
	spec := p.LabelSetSpecification()
	if spec == nil {
		return nil
	}
	names := spec.AllLabelName()
	labels := make(schema.LabelSet, len(names))
	for i, n := range names {
		labels[i] = n.GetText()
	}
	return labels
}
