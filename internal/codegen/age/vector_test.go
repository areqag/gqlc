package age_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/codegen"
	"github.com/areqag/gqlc/internal/codegen/age"
	"github.com/areqag/gqlc/internal/graph"
	"github.com/areqag/gqlc/internal/schema"
)

func vectorInput(pt graph.PropertyType) codegen.Input {
	labels := graph.LabelSetKey("Doc")
	return codegen.Input{Schema: schema.Schema{
		Name: "Vectors",
		Nodes: map[graph.LabelSetKey]schema.NodeType{labels: {
			KeyLabels: labels, CompleteLabels: labels, Name: "Doc",
			Properties: map[string]schema.Property{"embedding": {Name: "embedding", Type: pt}},
		}},
	}}
}

// TestAVectorIsRefusedWithItsReason pins the owner's ruling (ADR 0050): AGE
// refuses every VECTOR rather than storing it as the agtype list it could
// round-trip as, and the refusal says why, because "AGE stores lists" is the
// objection an author reaches for. It names the backend: neo4j-go-v6 carries
// the declaration, so this refusal is contingent (ADR 0035).
func TestAVectorIsRefusedWithItsReason(t *testing.T) {
	for _, elem := range graph.VectorElements {
		vec, ok := graph.VectorOf(elem)
		require.True(t, ok)

		_, carried := age.TypeMap{}.Property(vec)
		require.False(t, carried, "AGE carried %s", vec)

		_, err := age.New().Generate(vectorInput(vec))
		require.ErrorIs(t, err, codegen.ErrUnrepresentableWidth)
		require.ErrorContains(t, err, "the Apache AGE backend")
		require.ErrorContains(t, err, "no vector type and no vector functions")
	}
}

// TestAVectorInsideAContainerIsAnUnnamedRefusal is the converse: a vector in
// a list is refused by every enrolled target, so AGE's refusal of it owes no
// name and no AGE-specific reason (carriedByNoBackend, nestsAVector).
func TestAVectorInsideAContainerIsAnUnnamedRefusal(t *testing.T) {
	vec, ok := graph.VectorOf(graph.TypeFloat32)
	require.True(t, ok)

	_, err := age.New().Generate(vectorInput(graph.ListOf(vec, true)))
	require.ErrorIs(t, err, codegen.ErrUnrepresentableWidth)
	require.NotContains(t, err.Error(), "Apache AGE")
}
