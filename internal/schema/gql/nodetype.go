package gql

import (
	"github.com/areqag/gqlc/internal/grammar/gql/gen"
	"github.com/areqag/gqlc/internal/graph"
	"github.com/areqag/gqlc/internal/schema"
)

// rawNode is a node type collected during the walk, before resolution — one of
// the "raw" intermediate forms (with [rawEdge] and [rawEndpoint]) that exist only
// between collection and resolve(). Its labels are still raw LabelSets, not yet
// canonical keys, and it keeps the declaration-local alias, which is used only to
// resolve edge endpoints and is never part of the persisted identity.
//
// The two label fields mirror nodeTypeFiller (GQL.g4:1519-1522) rather than the
// final model: keyLabels is the phrase before `=>` and impliedLabels the label
// set after it. Applying GG22 — inferring an absent key label set from the
// implied content — is resolve()'s job, so this form stays a faithful reading of
// the parse tree and the semantic rule stays testable in plain Go.
type rawNode struct {
	// hasKeyLabelSet records whether a nodeTypeKeyLabelSet child was present,
	// which keyLabels alone cannot say: `labelSetPhrase?` is optional, so the
	// explicitly empty `(=> :Thing)` and the `=>`-less `(:Thing)` both leave
	// keyLabels empty and must resolve differently. Only the latter gets GG22's
	// inference; the former declared an empty identity and is rejected.
	hasKeyLabelSet bool
	keyLabels      graph.LabelSet
	impliedLabels  graph.LabelSet
	name           string
	alias          string
	props          map[string]schema.Property
}

// labelSet reads the labels off a label set phrase: either a single LABEL form
// (`:Person`) or an ampersand-joined set (`:Person&Employee`).
func labelSet(p gen.ILabelSetPhraseContext) (graph.LabelSet, error) {
	if p == nil {
		return nil, nil
	}
	if n := p.LabelName(); n != nil {
		label, err := labelName(n)
		if err != nil {
			return nil, err
		}
		return graph.LabelSet{label}, nil
	}
	spec := p.LabelSetSpecification()
	if spec == nil {
		return nil, nil
	}
	names := spec.AllLabelName()
	labels := make(graph.LabelSet, len(names))
	for i, n := range names {
		label, err := labelName(n)
		if err != nil {
			return nil, err
		}
		labels[i] = label
	}
	return labels, nil
}

// labelName reads one label as the name it denotes. Every byte a delimited
// identifier can hold is admitted, including the "&" that graph.LabelSet.Key
// joins on: the key quotes such a label rather than colliding on it, so there is
// nothing left here to refuse (bd gqlc-649co). Refusing it was this function's
// job only while the key was unquoted, and a refusal would have had to be copied
// into every front end that reads a label.
func labelName(n gen.ILabelNameContext) (string, error) {
	return identifierName(n.Identifier())
}
