package resolver

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/graph"
	"github.com/areqag/gqlc/internal/query/cypher"
	"github.com/areqag/gqlc/internal/schema"
)

// The headline row for this detector lives beside the ADR 0032 rows it shares a
// boundary with: TestWrongOrientationDropIsWarnedAbout, in
// undeclaredreltype_test.go, which also declares driftSchema and resolveDrift.

// Survival suppression, the clause that keeps the mirrored-alternation idiom
// silent. REPORTED drops on r for the same reason it does in the headline row,
// but it survives on s in the same query — so the author has demonstrably not
// lost it, and accusing them would fire on every query that covers both
// directions across two MATCHes.
//
// Delete clause 5 and this row goes red.
func TestWrongOrientationSurvivalElsewhereSuppresses(t *testing.T) {
	vq := resolveDrift(t,
		"MATCH (:Person)-[r:AUTHORED|REPORTED]->(p:Post) "+
			"MATCH (:Post)-[s:REPORTED]->(q:Person) RETURN p")
	require.Empty(t, vq.Warnings, "REPORTED survives on s, so its drop on r is not a mistake")
}

// Suppression is query-wide, not branch-wide: a UNION whose two arms draw the
// same alternation in opposite directions is the same idiom written across a
// combinator, and evidence that stopped at the branch would accuse it.
func TestWrongOrientationSurvivalCrossesUnionBranches(t *testing.T) {
	vq := resolveDrift(t,
		"MATCH (:Person)-[r:AUTHORED|REPORTED]->(p:Post) RETURN p "+
			"UNION MATCH (p:Post)-[s:REPORTED]->(:Person) RETURN p")
	require.Empty(t, vq.Warnings)
}

// Undirected is silent, and silent for a reason the detector never has to
// state: an undirected edge probes both orientations, so a declared reversed
// key lands in its own candidate set and the type does not drop at all. This
// row is what would catch a later change that made the orientation probe
// one-way.
func TestWrongOrientationUndirectedEdgeIsSilent(t *testing.T) {
	vq := resolveDrift(t, "MATCH (:Person)-[r:AUTHORED|REPORTED]-(p:Post) RETURN r")
	require.Empty(t, vq.Warnings)
}

// employsSchema is driftSchema plus EMPLOYS, declared Person->Person and
// nowhere else. A query drawing EMPLOYS from Person to Post drops it — but with
// no reversed declaration, so nothing witnesses a wrong arrow and the drop is
// ordinary ADR 0022 narrowing.
func employsSchema() schema.Schema {
	s := driftSchema()
	person := graph.LabelSet{"Person"}.Key()
	employs := schema.EdgeKey{Source: person, KeyLabels: graph.LabelSet{"EMPLOYS"}.Key(), Target: person}
	s.Edges[employs] = schema.EdgeType{EdgeKey: employs, CompleteLabels: employs.KeyLabels}
	return s
}

// The broad-shape control, and the row that reds if anyone drops the wrong-arrow
// witness and warns on any declared-but-dropped type. The design measured that
// broadening at 27 corpus cells over 3 legitimate queries.
//
// Delete clause 4 and this row goes red.
func TestWrongOrientationBroadDropWithoutReversedWitnessIsSilent(t *testing.T) {
	q, err := cypher.New().Parse(bytes.NewReader([]byte(
		"MATCH (:Person)-[r:AUTHORED|EMPLOYS]->(p:Post) RETURN r")))
	require.NoError(t, err)
	vq, err := New(employsSchema()).Resolve(q)
	require.NoError(t, err)
	require.Empty(t, vq.Warnings,
		"EMPLOYS drops with no reversed declaration, so nothing witnesses a wrong arrow")
}

// The covers gate. `a` reaches the second Part through a WITH carry, whose
// provenance that Part cannot see, so its key set is not known to cover every
// key a matching row can put there. The drop may then be an artifact of what
// the resolver failed to learn rather than of the arrow the author drew, and an
// accusation would be unfounded.
//
// Delete the covers gate and this row goes red.
func TestWrongOrientationCarriedEndpointIsSilent(t *testing.T) {
	vq := resolveDrift(t,
		"MATCH (a:Person) WITH a MATCH (a)-[r:AUTHORED|REPORTED]->(p:Post) RETURN p")
	require.Empty(t, vq.Warnings, "a carried endpoint is not known to cover, so it cannot accuse")
}

// The deferred close path. `x` is unlabelled, so its edge misses Phase A2
// entirely and is closed in Phase C against the post-inference node table. The
// detector has to record at that call site too — recording only in the A2 loop
// leaves this shape silent, which is a detector that works on some queries.
//
// Skip the deferred call site's recordEdge and this row goes red.
func TestWrongOrientationFiresOnDeferredClosePath(t *testing.T) {
	vq := resolveDrift(t, "MATCH (x)-[r:AUTHORED|REPORTED]->(p:Post) RETURN p")

	require.Len(t, vq.Warnings, 1)
	require.Equal(t, producerWrongOrientationDrop, vq.Warnings[0].Producer)
	require.Contains(t, vq.Warnings[0].Text, `"REPORTED"`)
}

// An anonymous edge has no variable to quote, so it is placed by its pattern.
// The quoted empty string would read as a defect in gqlc rather than as a
// description of the author's query.
func TestWrongOrientationOnAnonymousEdge(t *testing.T) {
	vq := resolveDrift(t, "MATCH (:Person)-[:AUTHORED|REPORTED]->(p:Post) RETURN p")

	require.Len(t, vq.Warnings, 1)
	require.NotContains(t, vq.Warnings[0].Text, `edge ""`)
	require.Contains(t, vq.Warnings[0].Text, `"REPORTED"`)
}

// Both lanes at once, which is the row that pins the split. FLAGGED is declared
// nowhere and belongs to ADR 0032; REPORTED is declared backwards and belongs
// here. Each type is accused exactly once, by the right detector, and 0032
// leads — generate.go prints the warning block ahead of the diagnostics on an
// argument about the 0032 lane specifically.
func TestWrongOrientationAndUndeclaredLanesBothFireInOrder(t *testing.T) {
	vq := resolveDrift(t, "MATCH (:Person)-[r:AUTHORED|FLAGGED|REPORTED]->(p:Post) RETURN r")

	require.Len(t, vq.Warnings, 2)
	require.Equal(t, producerUndeclaredRelationshipType, vq.Warnings[0].Producer)
	require.Contains(t, vq.Warnings[0].Text, `"FLAGGED"`)
	require.Equal(t, producerWrongOrientationDrop, vq.Warnings[1].Producer)
	require.Contains(t, vq.Warnings[1].Text, `"REPORTED"`)

	// Neither type collects the other lane's accusation. Asserted per warning
	// rather than over the concatenation: a total that mentions each name once
	// is also what two warnings about one type would produce.
	require.NotContains(t, vq.Warnings[0].Text, `"REPORTED"`)
	require.NotContains(t, vq.Warnings[1].Text, `"FLAGGED"`)
}

// One mis-drawn arrow repeated across three MATCHes is one mistake. Dropping
// the dedup makes this red at 3.
func TestWrongOrientationDropIsDedupedQueryWide(t *testing.T) {
	vq := resolveDrift(t,
		"MATCH (:Person)-[r:AUTHORED|REPORTED]->(p:Post) "+
			"MATCH (:Person)-[u:LIKES|REPORTED]->(v:Post) "+
			"MATCH (:Person)-[w:AUTHORED|REPORTED]->(z:Post) RETURN p")

	require.Len(t, vq.Warnings, 1)
	require.Contains(t, vq.Warnings[0].Text, `"REPORTED"`)
	require.NotContains(t, vq.Warnings[0].Text, "AUTHORED")
}
