package resolver

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/graph"
	"github.com/areqag/gqlc/internal/query/cypher"
	"github.com/areqag/gqlc/internal/schema"
)

// driftSchema declares two relationship types between Person and Post, plus a
// third (REPORTED) that runs the other way. Nothing declares FLAGGED.
//
// The three make the two axes separable. AUTHORED and LIKES are declared AND
// reachable between the ends the fixture queries write; REPORTED is declared
// but not between those ends, so a query naming it loses it to ENDPOINT
// narrowing; FLAGGED is declared nowhere, so a query naming it loses it to
// TYPE narrowing. ADR 0032 warns about the third and is silent about the
// second, and a schema carrying only AUTHORED and FLAGGED could not tell a
// correct implementation from one that warns on every narrowed candidate.
func driftSchema() schema.Schema {
	person := schema.NodeType{
		KeyLabels:      graph.LabelSet{"Person"}.Key(),
		CompleteLabels: graph.LabelSet{"Person"}.Key(),
	}
	post := schema.NodeType{
		KeyLabels:      graph.LabelSet{"Post"}.Key(),
		CompleteLabels: graph.LabelSet{"Post"}.Key(),
	}
	edge := func(src graph.LabelSetKey, label string, tgt graph.LabelSetKey) schema.EdgeKey {
		return schema.EdgeKey{Source: src, KeyLabels: graph.LabelSet{label}.Key(), Target: tgt}
	}
	authored := edge(person.KeyLabels, "AUTHORED", post.KeyLabels)
	likes := edge(person.KeyLabels, "LIKES", post.KeyLabels)
	reported := edge(post.KeyLabels, "REPORTED", person.KeyLabels)
	return schema.Schema{
		Nodes: map[graph.LabelSetKey]schema.NodeType{
			person.KeyLabels: person,
			post.KeyLabels:   post,
		},
		Edges: map[schema.EdgeKey]schema.EdgeType{
			authored: {EdgeKey: authored, CompleteLabels: authored.KeyLabels},
			likes:    {EdgeKey: likes, CompleteLabels: likes.KeyLabels},
			reported: {EdgeKey: reported, CompleteLabels: reported.KeyLabels},
		},
	}
}

func resolveDrift(t *testing.T, text string) ValidatedQuery {
	t.Helper()
	q, err := cypher.New().Parse(bytes.NewReader([]byte(text)))
	require.NoError(t, err)
	vq, err := New(driftSchema()).Resolve(q)
	require.NoError(t, err)
	return vq
}

// The bead's headline shape: FLAGGED is narrowed out of the candidate set and
// the compile succeeds. It must succeed loudly (ADR 0032).
func TestUndeclaredRelationshipTypeIsWarnedAbout(t *testing.T) {
	vq := resolveDrift(t, "MATCH (:Person)-[r:AUTHORED|LIKES|FLAGGED]->(p:Post) RETURN r")

	require.Len(t, vq.Warnings, 1)
	require.Contains(t, vq.Warnings[0], `"FLAGGED"`)
	require.Contains(t, vq.Warnings[0], "not declared")
	require.Contains(t, vq.Warnings[0], `edge "r"`)
	// The declared members of the same alternation are not accused.
	require.NotContains(t, vq.Warnings[0], "AUTHORED")
	require.NotContains(t, vq.Warnings[0], "LIKES")
}

// The negative half. Remove the one undeclared type and the same query shape
// must be silent — otherwise the guard above passes for a resolver that warns
// unconditionally.
func TestFullyDeclaredAlternationIsSilent(t *testing.T) {
	vq := resolveDrift(t, "MATCH (:Person)-[r:AUTHORED|LIKES]->(p:Post) RETURN r")
	require.Empty(t, vq.Warnings)
}

// The second negative, and the one that pins WHICH narrowing is reported.
// REPORTED is a declared relationship type; this pattern's ends drop it from
// the candidate set anyway. That is ADR 0022 endpoint narrowing, deliberately
// out of ADR 0032's scope: a plural endpoint narrowing to a subset is the
// resolver working, and warning there would bury the typo signal in noise.
//
// Broaden the detector from "declared nowhere" to "not in the candidate set"
// and this row goes red.
func TestEndpointNarrowedButDeclaredTypeIsSilent(t *testing.T) {
	vq := resolveDrift(t, "MATCH (:Person)-[r:AUTHORED|REPORTED]->(p:Post) RETURN r")
	require.Empty(t, vq.Warnings)
}

// A single-typed edge whose type is declared nowhere has an empty candidate
// set, so it is refused outright and never reaches the warning. An error and a
// warning about the same edge would be two verdicts on one fact.
func TestWhollyUndeclaredEdgeIsRefusedNotWarned(t *testing.T) {
	q, err := cypher.New().Parse(bytes.NewReader([]byte(
		"MATCH (:Person)-[r:FLAGGED]->(p:Post) RETURN r")))
	require.NoError(t, err)

	vq, err := New(driftSchema()).Resolve(q)
	require.ErrorIs(t, err, ErrUnknownEdge)
	require.Empty(t, vq.Warnings, "the zero ValidatedQuery carries no warnings")
}

// One undeclared type named twice is one warning, not two. Two distinct
// undeclared types are two warnings, in first-appearance order — dropping the
// dedup makes the first assertion red, and dropping the walk's second binding
// makes the second red.
func TestUndeclaredRelationshipTypesAreDedupedAndOrdered(t *testing.T) {
	vq := resolveDrift(t,
		"MATCH (:Person)-[r:AUTHORED|FLAGGED]->(p:Post) "+
			"MATCH (:Person)-[s:LIKES|FLAGGED]->(q:Post) RETURN p")
	require.Len(t, vq.Warnings, 1, "FLAGGED is named twice and warned about once")

	vq = resolveDrift(t,
		"MATCH (:Person)-[r:AUTHORED|FLAGGED]->(p:Post) "+
			"MATCH (:Person)-[s:LIKES|BOOSTED]->(q:Post) RETURN p")
	require.Len(t, vq.Warnings, 2)
	require.Contains(t, vq.Warnings[0], `"FLAGGED"`)
	require.Contains(t, vq.Warnings[1], `"BOOSTED"`)
}

// The message has to be actionable on its own: the offending type, the edge the
// author can find in their query text, and both readings of what happened.
func TestUndeclaredRelationshipTypeMessageNamesBothReadings(t *testing.T) {
	vq := resolveDrift(t, "MATCH (:Person)-[r:AUTHORED|FLAGGED]->(p:Post) RETURN r")
	require.Len(t, vq.Warnings, 1)
	m := vq.Warnings[0]
	require.Contains(t, m, "no decoder", "says what gqlc did")
	require.Contains(t, m, "misspelled", "names the typo reading")
	require.Contains(t, m, "declare it", "names the drift reading")
}

// An anonymous edge has no variable to quote, so the message must place it by
// its pattern instead of quoting the empty string.
func TestUndeclaredRelationshipTypeOnAnonymousEdge(t *testing.T) {
	vq := resolveDrift(t, "MATCH (:Person)-[:AUTHORED|FLAGGED]->(p:Post) RETURN p")
	require.Len(t, vq.Warnings, 1)
	require.NotContains(t, vq.Warnings[0], `edge ""`)
	require.Contains(t, vq.Warnings[0], `"FLAGGED"`)
}
