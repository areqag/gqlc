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
	require.Equal(t, producerUndeclaredRelationshipType, vq.Warnings[0].Producer)
	require.Contains(t, vq.Warnings[0].Text, `"FLAGGED"`)
	require.Contains(t, vq.Warnings[0].Text, "not declared")
	require.Contains(t, vq.Warnings[0].Text, `edge "r"`)
	// The declared members of the same alternation are not accused.
	require.NotContains(t, vq.Warnings[0].Text, "AUTHORED")
	require.NotContains(t, vq.Warnings[0].Text, "LIKES")
}

// The negative half. Remove the one undeclared type and the same query shape
// must be silent — otherwise the guard above passes for a resolver that warns
// unconditionally.
func TestFullyDeclaredAlternationIsSilent(t *testing.T) {
	vq := resolveDrift(t, "MATCH (:Person)-[r:AUTHORED|LIKES]->(p:Post) RETURN r")
	require.Empty(t, vq.Warnings)
}

// The wrong-orientation drop, which ADR 0032 deliberately left silent and the
// wrong-orientation-drop detector now warns about in one narrow shape. REPORTED
// is declared, but only as (Post)-[:REPORTED]->(Person); this pattern draws the
// arrow the other way, so endpoint narrowing (ADR 0022) drops it, no decoder is
// generated for it, and the server still sees the verbatim type name at
// runtime.
//
// This row replaces TestEndpointNarrowedButDeclaredTypeIsSilent, which asserted
// the opposite boundary. The replacement is deliberate rather than a deletion:
// the boundary moved, and a row has to state where it moved TO.
//
// AUTHORED shares the alternation and survives, so it must not be accused —
// that is what separates this from a detector that fires on any narrowing.
func TestWrongOrientationDropIsWarnedAbout(t *testing.T) {
	vq := resolveDrift(t, "MATCH (:Person)-[r:AUTHORED|REPORTED]->(p:Post) RETURN r")

	require.Len(t, vq.Warnings, 1)
	require.Equal(t, producerWrongOrientationDrop, vq.Warnings[0].Producer)
	require.Contains(t, vq.Warnings[0].Text, `"REPORTED"`)
	require.Contains(t, vq.Warnings[0].Text, `edge "r"`)
	// The reversed declared key is the witness that makes this actionable:
	// without it the author cannot tell a wrong arrow from a stale type.
	require.Contains(t, vq.Warnings[0].Text, "Post-[REPORTED]->Person")
	// Both remedies, because gqlc cannot choose between them.
	require.Contains(t, vq.Warnings[0].Text, "Flip the arrow")
	require.Contains(t, vq.Warnings[0].Text, "remove")
	require.NotContains(t, vq.Warnings[0].Text, "AUTHORED")
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
	require.Contains(t, vq.Warnings[0].Text, `"FLAGGED"`)
	require.Contains(t, vq.Warnings[1].Text, `"BOOSTED"`)
}

// The message has to be actionable on its own: the offending type, the edge the
// author can find in their query text, and both readings of what happened.
func TestUndeclaredRelationshipTypeMessageNamesBothReadings(t *testing.T) {
	vq := resolveDrift(t, "MATCH (:Person)-[r:AUTHORED|FLAGGED]->(p:Post) RETURN r")
	require.Len(t, vq.Warnings, 1)
	m := vq.Warnings[0].Text
	require.Contains(t, m, "no decoder", "says what gqlc did")
	require.Contains(t, m, "misspelled", "names the typo reading")
	require.Contains(t, m, "declare it", "names the drift reading")
}

// An anonymous edge has no variable to quote, so the message must place it by
// its pattern instead of quoting the empty string.
func TestUndeclaredRelationshipTypeOnAnonymousEdge(t *testing.T) {
	vq := resolveDrift(t, "MATCH (:Person)-[:AUTHORED|FLAGGED]->(p:Post) RETURN p")
	require.Len(t, vq.Warnings, 1)
	require.NotContains(t, vq.Warnings[0].Text, `edge ""`)
	require.Contains(t, vq.Warnings[0].Text, `"FLAGGED"`)
}
