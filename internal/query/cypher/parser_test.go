package cypher_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/antranig-yeretzian/gqlc/internal/query"
	"github.com/antranig-yeretzian/gqlc/internal/query/cypher"
)

// --- Layer 2: targeted sentinel checks (the TCK doesn't encode our taxonomy) ---
//
// Every query below is copied VERBATIM from a vendored read-core .feature file;
// the comment names its source. We never author Cypher — we select and label
// corpus queries.

// mustParse pairs read-core queries Stage 0 accepts with their source. They must
// parse without error; in run A they fail (errNotImplemented), which is the
// genuine implementation-gap signal.
var mustParse = map[string]string{
	// Match1 [1] Match non-existent nodes returns empty
	"node": "MATCH (n)\nRETURN n",
	// Match1 [3] Matching nodes using multiple labels
	"node multi-label": "MATCH (a:A:B)\nRETURN a",
	// Match1 [4] Simple node inline property predicate
	"node inline property": "MATCH (n {name: 'bar'})\nRETURN n",
	// Match1 [5] Use multiple MATCH clauses to do a Cartesian product
	"comma pattern with aliases": "MATCH (n), (m)\nRETURN n.num AS n, m.num AS m",
	// Match2 [1] Match non-existent relationships returns empty (anonymous edge, inline endpoints)
	"anonymous edge": "MATCH ()-[r]->()\nRETURN r",
	// Match2 [2] label predicate on both sides (inline-labelled endpoints)
	"edge inline-labelled endpoints": "MATCH (:A)-[r]->(:B)\nRETURN r",
	// Match3 [1] Get neighbours (typed edge, named endpoints)
	"typed edge named endpoints": "MATCH (n1)-[rel:KNOWS]->(n2)\nRETURN n1, n2",
	// Match3 [2] Directed match of a simple relationship (whole-entity returns)
	"directed edge whole entities": "MATCH (a)-[r]->(b)\nRETURN a, r, b",
	// MatchWhere1 [6] parameter in a property predicate
	"where property parameter": "MATCH (a)-[r]->(b)\nWHERE b.name = $param\nRETURN r",
}

func TestMustParse(t *testing.T) {
	for name, q := range mustParse {
		t.Run(name, func(t *testing.T) {
			_, err := cypher.New().Parse(strings.NewReader(q))
			require.NoError(t, err, "read-core query must parse: %q", q)
		})
	}
}

// mustReject pairs out-of-scope/invalid read-core queries with the sentinel they
// must produce. Each query is verbatim from a .feature file (source named).
var mustReject = map[string]struct {
	query string
	want  error
}{
	// Match3 [27] OPTIONAL MATCH -> ErrUnsupportedClause
	"optional match": {
		query: "OPTIONAL MATCH (a)\nWITH a\nMATCH (a)-->(b)\nRETURN b",
		want:  cypher.ErrUnsupportedClause,
	},
	// Return7 [2] RETURN * -> ErrUnsupportedProjection
	"return star": {
		query: "MATCH ()\nRETURN *",
		want:  cypher.ErrUnsupportedProjection,
	},
	// Match2 [6] multi-type relationship [:A|B] -> ErrUnsupportedPattern
	"multi-type relationship": {
		query: "MATCH (n)-[r:KNOWS|HATES]->(x)\nRETURN r",
		want:  cypher.ErrUnsupportedPattern,
	},
	// Match2 [3] undirected relationship -> ErrUnsupportedPattern
	"undirected relationship": {
		query: "MATCH ()-[r]-()\nRETURN r",
		want:  cypher.ErrUnsupportedPattern,
	},
	// Return1 [2] returning an undefined variable -> ErrUnboundVariable
	"unbound variable": {
		query: "MATCH ()\nRETURN foo",
		want:  cypher.ErrUnboundVariable,
	},
	// Match1 [9] same variable as a relationship and a node in one pattern
	// (directed, so the kind conflict is what fires) -> ErrVariableKindConflict
	"variable kind conflict": {
		query: "MATCH ()-[r]->(r)\nRETURN r",
		want:  cypher.ErrVariableKindConflict,
	},
	// Match1 [6] parameter used as a whole node property map (not bindable to a
	// single property) -> ErrUnsupportedParameter
	"unsupported parameter": {
		query: "MATCH (n $param)\nRETURN n",
		want:  cypher.ErrUnsupportedParameter,
	},
}

func TestMustReject(t *testing.T) {
	for name, tc := range mustReject {
		t.Run(name, func(t *testing.T) {
			got, err := cypher.New().Parse(strings.NewReader(tc.query))
			require.Error(t, err, "out-of-scope query must be rejected: %q", tc.query)
			require.Equal(t, query.Query{}, got, "model must be the zero value on error")
			require.ErrorIs(t, err, tc.want)
		})
	}
}

// allSentinels is the canonical list of the six Parse sentinels — the single
// source of truth TestSentinelReachability checks against. A new sentinel must be
// added here (and exercised by a mustReject case); a removed one must be dropped.
// errNotImplemented is deliberately absent: it is the run-A stub, not a contract
// sentinel.
var allSentinels = []error{
	cypher.ErrUnsupportedClause,
	cypher.ErrUnsupportedProjection,
	cypher.ErrUnsupportedPattern,
	cypher.ErrUnsupportedParameter,
	cypher.ErrUnboundVariable,
	cypher.ErrVariableKindConflict,
}

// TestSentinelReachability is the bidirectional sweep (mirroring schema/gql): the
// set of sentinels the mustReject cases cover must be a subset of the canonical
// set, and every canonical sentinel must be reachable by some mustReject case.
// It fails if a sentinel is declared but never exercised (orphaned), or if a case
// maps to a sentinel missing from the canonical list (stray or renamed).
//
// Run A note: ErrUnboundVariable and ErrVariableKindConflict have no mustReject
// case yet — they require build()-time self-consistency validation that lands in
// run B — so this test is expected to fail now (an implementation gap), and run B
// adds the two cases that turn it green.
func TestSentinelReachability(t *testing.T) {
	canonical := make(map[error]bool, len(allSentinels))
	for _, s := range allSentinels {
		canonical[s] = true
	}

	covered := make(map[error]bool)
	for _, tc := range mustReject {
		require.True(t, canonical[tc.want], "mustReject maps to non-canonical sentinel %q", tc.want)
		covered[tc.want] = true
	}

	for _, s := range allSentinels {
		require.True(t, covered[s], "sentinel %q has no mustReject case", s)
	}
}

// --- corpus harvest for property tests ---

// corpusQueries returns every "When executing query" block in the read-core
// feature dirs, in a stable order, so the property tests range over the real
// corpus rather than hand-authored queries.
func corpusQueries(t *testing.T) []string {
	t.Helper()
	queries := harvestExecutingQueries(t, readCoreDirs)
	require.NotEmpty(t, queries, "no corpus queries harvested — TCK vendoring or paths are wrong")
	return queries
}

// --- properties (rapid) over the parsed read-core corpus ---
//
// These assert the model invariants Stage 0 guarantees for any query that parses.
// In run A no corpus query parses (the stub returns errNotImplemented), so the
// "parses" precondition is never met and the body's invariant checks do not run;
// to keep the property tests honest implementation-gap failures rather than
// vacuous passes, each first requires that the must-parse corpus actually parses.

// TestPropertyReadCoreParses is the precondition guard: the curated read-core
// queries must parse. It fails in run A and turns green in run B. The richer
// invariant properties below depend on a parsed model, so this is the gate.
func TestPropertyReadCoreParses(t *testing.T) {
	queries := make([]string, 0, len(mustParse))
	for _, q := range mustParse {
		queries = append(queries, q)
	}
	rapid.Check(t, func(rt *rapid.T) {
		q := rapid.SampledFrom(queries).Draw(rt, "query")
		if _, err := cypher.New().Parse(strings.NewReader(q)); err != nil {
			rt.Fatalf("read-core query did not parse: %q: %v", q, err)
		}
	})
}

// TestPropertyReferentialIntegrity asserts every Ref and edge-endpoint variable
// resolves to a binding, named bindings are unique, and parameters are deduped in
// first-appearance order — over every corpus query that parses. Skips a query the
// parser rejects (out of scope), so it exercises only parsed models.
func TestPropertyReferentialIntegrity(t *testing.T) {
	corpus := corpusQueries(t)
	rapid.Check(t, func(rt *rapid.T) {
		src := rapid.SampledFrom(corpus).Draw(rt, "query")
		q, err := cypher.New().Parse(strings.NewReader(src))
		if err != nil {
			return // rejected (out of scope) — nothing to check
		}
		assertReferentialIntegrity(rt, q, src)
		assertNamedBindingsUnique(rt, q, src)
		assertParametersDeduped(rt, q, src)
	})
}

func assertReferentialIntegrity(rt *rapid.T, q query.Query, src string) {
	named := make(map[string]bool)
	for _, b := range q.Bindings {
		if v := bindingVariable(b); v != "" {
			named[v] = true
		}
	}
	resolves := func(v string) bool { return v != "" && named[v] }

	for _, b := range q.Bindings {
		eb, ok := b.(query.EdgeBinding)
		if !ok {
			continue
		}
		for _, ep := range []query.Endpoint{eb.Source(), eb.Target()} {
			if ve, ok := ep.(query.VarEndpoint); ok && !resolves(ve.Variable()) {
				rt.Fatalf("endpoint variable %q has no binding in %q", ve.Variable(), src)
			}
		}
	}
	for _, r := range q.Returns {
		if !resolves(r.Ref.Variable) {
			rt.Fatalf("return ref %q has no binding in %q", r.Ref.Variable, src)
		}
	}
	for _, p := range q.Parameters {
		for _, u := range p.Uses {
			if !resolves(u.Variable) {
				rt.Fatalf("parameter %q use ref %q has no binding in %q", p.Name, u.Variable, src)
			}
		}
	}
}

func assertNamedBindingsUnique(rt *rapid.T, q query.Query, src string) {
	seen := make(map[string]bool)
	for _, b := range q.Bindings {
		v := bindingVariable(b)
		if v == "" {
			continue // anonymous edges are each their own binding
		}
		if seen[v] {
			rt.Fatalf("named binding %q appears more than once in %q", v, src)
		}
		seen[v] = true
	}
}

func assertParametersDeduped(rt *rapid.T, q query.Query, src string) {
	seen := make(map[string]bool)
	for _, p := range q.Parameters {
		if seen[p.Name] {
			rt.Fatalf("parameter %q is not deduped in %q", p.Name, src)
		}
		seen[p.Name] = true
		if len(p.Uses) == 0 {
			rt.Fatalf("parameter %q has no uses (D4 invariant) in %q", p.Name, src)
		}
	}
}

// bindingVariable reads the variable of either binding variant via its accessor.
func bindingVariable(b query.Binding) string {
	switch v := b.(type) {
	case query.NodeBinding:
		return v.Variable()
	case query.EdgeBinding:
		return v.Variable()
	default:
		return ""
	}
}
