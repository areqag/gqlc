package cypher_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/graph"
	"github.com/areqag/gqlc/internal/query"
	"github.com/areqag/gqlc/internal/query/cypher"
)

// --- re-binding a pattern variable within one part (gqlc-rrtl) ---
//
// Two axes meet on a relationship pattern and pull in opposite directions.
// Within one occurrence, `[r:A|B]` is a DISJUNCTION: the edge matches if its
// single type is A or B, so the occurrence contributes a candidate set. Across
// occurrences of the same variable in one part, the constraints are a
// CONJUNCTION: `r` denotes one relationship, and that relationship must satisfy
// every occurrence. openCypher gives a relationship exactly one type, so the
// conjunction of two occurrences is the intersection of their candidate sets,
// and an empty intersection means no relationship can ever match.
//
// Nodes are the other way round: a node carries any number of labels
// simultaneously, so `(n:Person)` and `(n:Employee)` conjoin to "carries both"
// — the union — and the two kinds must not share a merge rule.

// namedEdgeLabels returns the label set the parser committed to the named edge
// binding in the query's first part.
func namedEdgeLabels(t *testing.T, q query.Query, variable string) graph.LabelSet {
	t.Helper()
	for _, br := range q.Branches {
		for _, part := range br.Parts {
			for _, b := range part.Bindings {
				if eb, ok := b.(query.EdgeBinding); ok && eb.Variable() == variable {
					return eb.Labels()
				}
			}
		}
	}
	t.Fatalf("no edge binding named %q in parsed model", variable)
	return nil
}

// namedNodeLabels is the node twin of namedEdgeLabels.
func namedNodeLabels(t *testing.T, q query.Query, variable string) graph.LabelSet {
	t.Helper()
	for _, br := range q.Branches {
		for _, part := range br.Parts {
			for _, b := range part.Bindings {
				if nb, ok := b.(query.NodeBinding); ok && nb.Variable() == variable {
					return nb.Labels()
				}
			}
		}
	}
	t.Fatalf("no node binding named %q in parsed model", variable)
	return nil
}

// TestRelationshipRebindIntersectsTypes pins the satisfiable half of the
// conjunction: the surviving candidate set is the intersection of the
// occurrences that name types, in first-occurrence order, and an occurrence
// that names no type is the identity element rather than an annihilator.
func TestRelationshipRebindIntersectsTypes(t *testing.T) {
	cases := map[string]struct {
		query string
		want  graph.LabelSet
	}{
		// The disjunctive construct, untouched: one occurrence, two
		// alternatives, both candidates survive.
		"single occurrence alternation widens": {
			query: "MATCH (:Person)-[r:AUTHORED|LIKES]->(:Post) RETURN r",
			want:  graph.LabelSet{"AUTHORED", "LIKES"},
		},
		// Two alternations overlapping in one type: the conjunction narrows to
		// the overlap. A union would answer AUTHORED, LIKES, WROTE — a wider
		// set than either occurrence permits.
		"two alternations narrow to the overlap": {
			query: "MATCH (:Person)-[r:AUTHORED|LIKES]->(:Post), (:Person)-[r:LIKES|WROTE]->(:Post) RETURN r",
			want:  graph.LabelSet{"LIKES"},
		},
		// Same two candidates, opposite spelling order. The surviving set is
		// ordered by first appearance, not by the narrowing occurrence, so the
		// edge-union candidate order downstream code emits stays a function of
		// where each type is first written (C2).
		"survivors keep first-appearance order": {
			query: "MATCH (:Person)-[r:AUTHORED|LIKES]->(:Post), (:Person)-[r:LIKES|AUTHORED]->(:Post) RETURN r",
			want:  graph.LabelSet{"AUTHORED", "LIKES"},
		},
		"same type twice is idempotent": {
			query: "MATCH (a:Person)-[r:AUTHORED]->(b:Post), (a)-[r:AUTHORED]->(b) RETURN r",
			want:  graph.LabelSet{"AUTHORED"},
		},
		// An untyped occurrence constrains nothing; intersecting against it
		// must not empty the set. Both orders, because the identity element
		// has to work on either side of the operator.
		"untyped second occurrence constrains nothing": {
			query: "MATCH (a:Person)-[r:AUTHORED]->(b:Post), (a)-[r]->(b) RETURN r",
			want:  graph.LabelSet{"AUTHORED"},
		},
		"untyped first occurrence constrains nothing": {
			query: "MATCH (a:Person)-[r]->(b:Post), (a)-[r:AUTHORED]->(b) RETURN r",
			want:  graph.LabelSet{"AUTHORED"},
		},
		"untyped throughout stays unconstrained": {
			query: "MATCH (a:Person)-[r]->(b:Post), (a)-[r]->(b) RETURN r",
			want:  nil,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := cypher.New().Parse(strings.NewReader(tc.query))
			require.NoError(t, err)
			require.Equal(t, tc.want, namedEdgeLabels(t, got, "r"))
		})
	}
}

// TestRelationshipRebindUnsatisfiableIsRefused covers the four clause shapes
// that re-bind one relationship variable to disjoint type sets inside a single
// part. Each is a conjunction with an empty intersection, so no relationship
// can satisfy it and the query returns no rows on any backend; emitting a
// decoder for it would be emitting a decoder for a candidate set the query
// excludes. The message must name both conflicting types — the reader's only
// clue is which two occurrences disagree.
func TestRelationshipRebindUnsatisfiableIsRefused(t *testing.T) {
	cases := map[string]struct {
		query string
		types [2]string
	}{
		"comma-joined pattern parts": {
			query: "MATCH (:Person)-[r:AUTHORED]->(:Post), (:Person)-[r:LIKES]->(:Post) RETURN r",
			types: [2]string{"AUTHORED", "LIKES"},
		},
		"OPTIONAL MATCH extends a bound relationship": {
			query: "MATCH (p:Person)-[r:AUTHORED]->(:Post)\nOPTIONAL MATCH (p)-[r:LIKES]->(:Post)\nRETURN r",
			types: [2]string{"AUTHORED", "LIKES"},
		},
		"sibling MATCH clauses": {
			query: "MATCH (:Person)-[r:AUTHORED]->(:Post)\nMATCH (:Person)-[r:LIKES]->(:Post)\nRETURN r",
			types: [2]string{"AUTHORED", "LIKES"},
		},
		"MERGE re-binds a matched relationship": {
			query: "MATCH (p:Person)-[r:AUTHORED]->(q:Post)\nMERGE (p)-[r:LIKES]->(q)\nRETURN r",
			types: [2]string{"AUTHORED", "LIKES"},
		},
		// Disjoint alternations: neither occurrence is a single type, so the
		// message has to name the sets, not just two bare labels.
		"disjoint alternations": {
			query: "MATCH (:Person)-[r:AUTHORED|WROTE]->(:Post), (:Person)-[r:LIKES|SHARED]->(:Post) RETURN r",
			types: [2]string{"AUTHORED|WROTE", "LIKES|SHARED"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := cypher.New().Parse(strings.NewReader(tc.query))
			require.ErrorIs(t, err, cypher.ErrUnsatisfiableRelationshipType)
			require.Equal(t, query.Query{}, got, "model must be the zero value on error")
			for _, want := range tc.types {
				require.ErrorContains(t, err, want, "message must name both conflicting relationship types")
			}
			require.ErrorContains(t, err, "exactly one type",
				"message must say why the conjunction is unsatisfiable")
		})
	}
}

// TestNodeRebindUnionsLabels is the control for the rule above: a node can
// carry every label it is asked for at once, so re-binding a node variable
// conjoins to the union and no label set is unsatisfiable at parse time. A fix
// that intersected both kinds would narrow this to the empty set and then have
// to refuse a perfectly satisfiable query.
func TestNodeRebindUnionsLabels(t *testing.T) {
	got, err := cypher.New().Parse(strings.NewReader("MATCH (n:Person), (n:Employee) RETURN n"))
	require.NoError(t, err)
	require.Equal(t, graph.LabelSet{"Person", "Employee"}, namedNodeLabels(t, got, "n"))
}
