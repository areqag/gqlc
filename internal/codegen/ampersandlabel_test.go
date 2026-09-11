package codegen_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/codegen"
	"github.com/areqag/gqlc/internal/graph"
	"github.com/areqag/gqlc/internal/schema"
)

// TestAnAmpersandBearingLabelReachesTheNamingRules covers the codegen path
// gqlc-649co made reachable. Until that bead the schema front end refused a label
// carrying "&", so no key could hold one and Phase Z never saw this shape; the
// path was fail-closed by unreachability rather than by a rule.
//
// It is now reachable, and the interesting part is WHICH refusal it reaches. Both
// arms of that question are ErrUnnamedMultiLabelType vs ErrInvalidEntityName, and
// they disagree about what the schema said:
//
//   - Before the quoting, the key spelled itself "A&B", so labels.Split() cut it
//     into two and Phase Z reported a MULTI-LABEL node type — a diagnostic about
//     a declaration the author never wrote. That is the type-identity forgery
//     arriving in codegen, wearing an error message.
//   - After it, the key spells itself "`A&B`", Split returns the single label
//     A&B, and the refusal is the honest one: a one-label node type whose label
//     does not mangle to an exported Go identifier.
//
// So the NotErrorIs is the load-bearing half. Asserting only "Phase Z refuses
// this" passes under both spellings and cannot see the difference the bead is
// about.
func TestAnAmpersandBearingLabelReachesTheNamingRules(t *testing.T) {
	key := graph.LabelSet{"A&B"}.Key()
	require.Equal(t, graph.LabelSetKey("`A&B`"), key,
		"the rest of this test reads the refusal that key spelling selects")

	t.Run("without an explicit Name it is refused as one unmanglable label", func(t *testing.T) {
		nt := schema.NodeType{KeyLabels: key, CompleteLabels: key}
		_, _, err := codegen.PhaseZAdmit(schema.Schema{
			Name:  "T",
			Nodes: map[graph.LabelSetKey]schema.NodeType{key: nt},
		}, stubTypeMap{})

		require.ErrorIs(t, err, codegen.ErrInvalidEntityName)
		require.NotErrorIs(t, err, codegen.ErrUnnamedMultiLabelType,
			"one label, not two: reporting a multi-label type here means the key split on an unquoted separator")
		require.ErrorContains(t, err, "A&B", "the diagnostic must name the label the author wrote")
	})

	t.Run("with an explicit Name it is admitted", func(t *testing.T) {
		nt := schema.NodeType{KeyLabels: key, CompleteLabels: key, Name: "PersonEmployee"}
		entities, index, err := codegen.PhaseZAdmit(schema.Schema{
			Name:  "T",
			Nodes: map[graph.LabelSetKey]schema.NodeType{key: nt},
		}, stubTypeMap{})

		require.NoError(t, err)
		require.Len(t, entities, 1)
		require.Equal(t, "PersonEmployee", entities[0].Name)
		require.Equal(t, key, entities[0].Labels,
			"the entity carries the quoted key, so it still indexes the schema it came from")
		require.Contains(t, index, codegen.EntityLookupKey{Kind: codegen.EntityNode, Labels: key})
	})
}
