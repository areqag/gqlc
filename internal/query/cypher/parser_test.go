package cypher_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/antranig-yeretzian/gqlc/internal/graph"
	"github.com/antranig-yeretzian/gqlc/internal/query"
	"github.com/antranig-yeretzian/gqlc/internal/query/cypher"
)

// --- Layer 2: targeted sentinel checks (the TCK doesn't encode our taxonomy) ---
//
// Every query below is copied VERBATIM from a vendored read-core .feature file;
// the comment names its source. We never author Cypher — we select and label
// corpus queries. The expected query.Query in mustParse is hand-built from the
// Stage-0 spec (Clusters C/D/E), not copied from the parser's current output;
// it is the regression layer the golden snapshots — which -update silently
// rebaselines — cannot give us.

// mustParse pairs each read-core query with the exact query.Query Stage 0 must
// produce for it, built via the branch-1 model constructors. The test asserts
// deep equality, so a parser change that shifts the shape must update this
// hand-built expectation deliberately — there is no -update escape hatch.

// Cluster rules NOT exact-shape asserted here, because the openCypher TCK at the
// pinned tag (justfile: tck_tag) contains no verbatim read-core query that
// exercises them — every candidate uses constructs Stage 0 rejects (WITH,
// CREATE/MERGE, variable-length, etc.) — and the no-authoring-Cypher rule means
// we add a case only when the corpus supplies one:
//   - C2: label union across multiple occurrences of the same variable
//   - D2: a parameter token used in two value positions (dedup with multiple Uses)
//   - D1b: an inline property map whose value is a $param ((a {id: $id}))
//
// Revisit on every TCK bump: a new feature file may close one of these gaps.
var mustParse = map[string]struct {
	src  string
	want query.Query
}{
	// Match1 [1] Match non-existent nodes returns empty: one node binding,
	// bare-variable return → Ref{n,""}, column name "n".
	"node": {
		src: "MATCH (n)\nRETURN n",
		want: query.Query{
			Bindings: []query.Binding{
				must(query.NewNodeBinding("n", nil)),
			},
			Returns: []query.ReturnItem{
				{Name: "n", Ref: query.Ref{Variable: "n"}},
			},
		},
	},
	// Match1 [3] Matching nodes using multiple labels: C2 conjunctive labels in
	// source order, A then B.
	"node multi-label": {
		src: "MATCH (a:A:B)\nRETURN a",
		want: query.Query{
			Bindings: []query.Binding{
				must(query.NewNodeBinding("a", graph.LabelSet{"A", "B"})),
			},
			Returns: []query.ReturnItem{
				{Name: "a", Ref: query.Ref{Variable: "a"}},
			},
		},
	},
	// Match1 [4] Simple node inline property predicate: the inline map value is a
	// literal (not a $param), so no parameter use is mined (D1b).
	"node inline property": {
		src: "MATCH (n {name: 'bar'})\nRETURN n",
		want: query.Query{
			Bindings: []query.Binding{
				must(query.NewNodeBinding("n", nil)),
			},
			Returns: []query.ReturnItem{
				{Name: "n", Ref: query.Ref{Variable: "n"}},
			},
		},
	},
	// Match1 [5] Use multiple MATCH clauses to do a Cartesian product: two nodes
	// in textual order [n, m]; explicit AS aliases (E1) become the column names.
	"comma pattern with aliases": {
		src: "MATCH (n), (m)\nRETURN n.num AS n, m.num AS m",
		want: query.Query{
			Bindings: []query.Binding{
				must(query.NewNodeBinding("n", nil)),
				must(query.NewNodeBinding("m", nil)),
			},
			Returns: []query.ReturnItem{
				{Name: "n", Ref: query.Ref{Variable: "n", Property: "num"}},
				{Name: "m", Ref: query.Ref{Variable: "m", Property: "num"}},
			},
		},
	},
	// Match2 [1] Match non-existent relationships returns empty: C1 anonymous edge
	// is its own binding; C4 both endpoints are inline-empty (the () case).
	"anonymous edge": {
		src: "MATCH ()-[r]->()\nRETURN r",
		want: query.Query{
			Bindings: []query.Binding{
				must(query.NewEdgeBinding("r", nil,
					query.NewInlineEndpoint(nil),
					query.NewInlineEndpoint(nil),
				)),
			},
			Returns: []query.ReturnItem{
				{Name: "r", Ref: query.Ref{Variable: "r"}},
			},
		},
	},
	// Match2 [2] label predicate on both sides: C4 anonymous endpoints carry
	// inline labels — [A] on the source, [B] on the target.
	"edge inline-labelled endpoints": {
		src: "MATCH (:A)-[r]->(:B)\nRETURN r",
		want: query.Query{
			Bindings: []query.Binding{
				must(query.NewEdgeBinding("r", nil,
					query.NewInlineEndpoint(graph.LabelSet{"A"}),
					query.NewInlineEndpoint(graph.LabelSet{"B"}),
				)),
			},
			Returns: []query.ReturnItem{
				{Name: "r", Ref: query.Ref{Variable: "r"}},
			},
		},
	},
	// Match3 [1] Get neighbours: textual first-appearance order [n1, rel, n2];
	// var endpoints for named nodes (C4 — labels live on their bindings).
	"typed edge named endpoints": {
		src: "MATCH (n1)-[rel:KNOWS]->(n2)\nRETURN n1, n2",
		want: query.Query{
			Bindings: []query.Binding{
				must(query.NewNodeBinding("n1", nil)),
				must(query.NewEdgeBinding("rel", graph.LabelSet{"KNOWS"},
					must(query.NewVarEndpoint("n1")),
					must(query.NewVarEndpoint("n2")),
				)),
				must(query.NewNodeBinding("n2", nil)),
			},
			Returns: []query.ReturnItem{
				{Name: "n1", Ref: query.Ref{Variable: "n1"}},
				{Name: "n2", Ref: query.Ref{Variable: "n2"}},
			},
		},
	},
	// Match3 [2] Directed match of a simple relationship: E3 whole-entity returns
	// → Ref{var, ""} for each; textual order [a, r, b].
	"directed edge whole entities": {
		src: "MATCH (a)-[r]->(b)\nRETURN a, r, b",
		want: query.Query{
			Bindings: []query.Binding{
				must(query.NewNodeBinding("a", nil)),
				must(query.NewEdgeBinding("r", nil,
					must(query.NewVarEndpoint("a")),
					must(query.NewVarEndpoint("b")),
				)),
				must(query.NewNodeBinding("b", nil)),
			},
			Returns: []query.ReturnItem{
				{Name: "a", Ref: query.Ref{Variable: "a"}},
				{Name: "r", Ref: query.Ref{Variable: "r"}},
				{Name: "b", Ref: query.Ref{Variable: "b"}},
			},
		},
	},
	// MatchWhere1 [6] parameter in a property predicate: D1a pairs $param with
	// b.name → one Parameter with Use PropertyUse{Ref{b, name}}.
	"where property parameter": {
		src: "MATCH (a)-[r]->(b)\nWHERE b.name = $param\nRETURN r",
		want: query.Query{
			Bindings: []query.Binding{
				must(query.NewNodeBinding("a", nil)),
				must(query.NewEdgeBinding("r", nil,
					must(query.NewVarEndpoint("a")),
					must(query.NewVarEndpoint("b")),
				)),
				must(query.NewNodeBinding("b", nil)),
			},
			Parameters: []query.Parameter{
				{Name: "param", Uses: []query.Use{
					query.NewPropertyUse(query.Ref{Variable: "b", Property: "name"}),
				}},
			},
			Returns: []query.ReturnItem{
				{Name: "r", Ref: query.Ref{Variable: "r"}},
			},
		},
	},
	// ReturnSkipLimit1 [2] "Start the result from second row by param" —
	// verbatim TCK query. Stage 1: SKIP $p is a clause-slot-typed parameter
	// use; the parameter carries one Use = ClauseSlotUse{Skip}, not a
	// property Ref. ORDER BY a bare var.prop is accept-and-ignored (E4).
	"skip parameter": {
		src: "MATCH (n)\nRETURN n\nORDER BY n.name ASC\nSKIP $skipAmount",
		want: query.Query{
			Bindings: []query.Binding{
				must(query.NewNodeBinding("n", nil)),
			},
			Parameters: []query.Parameter{
				{Name: "skipAmount", Uses: []query.Use{
					query.NewClauseSlotUse(query.ClauseSlotSkip),
				}},
			},
			Returns: []query.ReturnItem{
				{Name: "n", Ref: query.Ref{Variable: "n"}},
			},
		},
	},
	// Create2 [4] control query: a left-pointing arc. C-Direction: the canonical
	// edge is source=b, target=a (the arrow's tail is the source) — independent of
	// how it was written. The relationship has no variable (anonymous edge, C1).
	// (TCK uses "When executing control query:" here, not "When executing query:",
	// so this scenario is outside our godog suite; the verbatim query is still
	// fair Layer-2 material.)
	"edge left-pointing canonical": {
		src: "MATCH (a:A)<-[:R]-(b:B)\nRETURN a, b",
		want: query.Query{
			Bindings: []query.Binding{
				must(query.NewNodeBinding("a", graph.LabelSet{"A"})),
				must(query.NewEdgeBinding("", graph.LabelSet{"R"},
					must(query.NewVarEndpoint("b")),
					must(query.NewVarEndpoint("a")),
				)),
				must(query.NewNodeBinding("b", graph.LabelSet{"B"})),
			},
			Returns: []query.ReturnItem{
				{Name: "a", Ref: query.Ref{Variable: "a"}},
				{Name: "b", Ref: query.Ref{Variable: "b"}},
			},
		},
	},
	// Temporal4 [1] property return with no alias: E1 derives the column name from
	// the verbatim expression text — "n.created", not "created".
	"property return no alias": {
		src: "MATCH (n)\nRETURN n.created",
		want: query.Query{
			Bindings: []query.Binding{
				must(query.NewNodeBinding("n", nil)),
			},
			Returns: []query.ReturnItem{
				{Name: "n.created", Ref: query.Ref{Variable: "n", Property: "created"}},
			},
		},
	},
}

// must lifts a fallible model constructor into an expression usable in a struct
// literal: it panics if err is non-nil. The mustParse inputs are hard-coded valid
// values, so any error here is a programmer error and panic is the honest signal.
func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

func TestMustParse(t *testing.T) {
	for name, c := range mustParse {
		t.Run(name, func(t *testing.T) {
			got, err := cypher.New().Parse(strings.NewReader(c.src))
			require.NoError(t, err, "read-core query must parse: %q", c.src)
			require.Equal(t, c.want, got)
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

// TestPropertyReadCoreParses is the precondition guard for the richer invariant
// properties below: every curated read-core query must parse. If it ever fails,
// the property tests below would pass vacuously, so this is the gate.
func TestPropertyReadCoreParses(t *testing.T) {
	queries := make([]string, 0, len(mustParse))
	for _, c := range mustParse {
		queries = append(queries, c.src)
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
			switch use := u.(type) {
			case query.PropertyUse:
				if !resolves(use.Ref().Variable) {
					rt.Fatalf("parameter %q use ref %q has no binding in %q", p.Name, use.Ref().Variable, src)
				}
			case query.ClauseSlotUse:
				// A clause-slot use has no Variable — referential check is N/A.
			default:
				rt.Fatalf("parameter %q has unknown Use variant %T in %q", p.Name, u, src)
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
