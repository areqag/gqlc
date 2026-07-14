package cypher_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/areqag/gqlc/internal/graph"
	"github.com/areqag/gqlc/internal/procsig"
	"github.com/areqag/gqlc/internal/query"
	"github.com/areqag/gqlc/internal/query/cypher"
)

// --- Layer 2: targeted sentinel checks (the TCK doesn't encode our taxonomy) ---
//
// Layer-2 rule:
//
// ACCEPT-PATH cases (mustParse) come VERBATIM from the corpus. The hand-built
// query.Query in each entry is the regression layer the golden snapshots —
// which -update silently rebaselines — cannot give us, but the SHAPE we pin
// against must come from a committee-authored input — otherwise we would be
// asserting the shape we want the parser to produce against the input we chose
// to produce it (evidentiary circularity). We add a mustParse case only when
// the corpus supplies one.
//
// REJECT-PATH cases (mustReject) come VERBATIM from the corpus where the
// corpus exercises the fail-site; otherwise they are AUTHORED with an inline
// `// AUTHORED:` marker naming the fail-site by domain. The sentinel taxonomy
// is ours (the TCK doesn't encode it), and the only assertion is ABSENCE of a
// model — no shape to outsource — so the accept-path's circularity concern
// does not apply on this side.
//
// Authored mustReject cases are bounded: at most one per fail-site (the same
// way the corpus provides at most one per scenario), and only when no verbatim
// corpus query exercises that fail-site at the pinned TCK tag.
//
// Both rules carry the revisit-on-TCK-bump obligation: when a bump adds a
// corpus query for an authored case's fail-site, the corpus entry replaces the
// authored one (the corpus is always preferred when available).

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
// oneBranch wraps a single part (and query-wide parameters) into the
// one-branch/one-part Query shape the read-core (WITH-free, UNION-free) queries
// lower to. The Stage-4 nesting is always present even for the flat common case.
func oneBranch(part query.Part, params ...query.Parameter) query.Query {
	q := query.Query{Branches: []query.Branch{{Parts: []query.Part{part}}}}
	if len(params) > 0 {
		q.Parameters = params
	}
	return q
}

// oneWriteBranch is the Stage-12 write-side companion to oneBranch: the same
// one-branch/one-part shape, with StatementKind set to StatementWrite. Every
// Stage-12 mustParse pin exercising a write clause uses this helper so a pin
// asserts on the statement-kind axis end-to-end.
func oneWriteBranch(part query.Part, params ...query.Parameter) query.Query {
	q := oneBranch(part, params...)
	q.StatementKind = query.StatementWrite
	return q
}

var mustParse = map[string]struct {
	src  string
	want query.Query
	// sigs is Stage 14: per-pin procedure signatures the parser needs to
	// resolve CALL clauses in `src`. Empty (nil) for every pre-Stage-14
	// pin — the parser is constructed with no options, matching the
	// pre-Stage-14 behaviour for queries without CALL.
	sigs []procsig.Signature
}{
	// Match1 [1] Match non-existent nodes returns empty: one node binding,
	// bare-variable return → Ref{n,""}, column name "n".
	"node": {
		src: "MATCH (n)\nRETURN n",
		want: oneBranch(query.Part{
			Bindings: []query.Binding{
				must(query.NewNodeBinding("n", nil)),
			},
			Returns: []query.ReturnItem{
				{Name: "n", Value: query.NewRefProjection(query.Ref{Variable: "n"}, query.TypeNode{})},
			},
		}),
	},
	// Match1 [3] Matching nodes using multiple labels: C2 conjunctive labels in
	// source order, A then B.
	"node multi-label": {
		src: "MATCH (a:A:B)\nRETURN a",
		want: oneBranch(query.Part{
			Bindings: []query.Binding{
				must(query.NewNodeBinding("a", graph.LabelSet{"A", "B"})),
			},
			Returns: []query.ReturnItem{
				{Name: "a", Value: query.NewRefProjection(query.Ref{Variable: "a"}, query.TypeNode{})},
			},
		}),
	},
	// Match1 [4] Simple node inline property predicate: the inline map value is a
	// literal (not a $param), so no parameter use is mined (D1b).
	"node inline property": {
		src: "MATCH (n {name: 'bar'})\nRETURN n",
		want: oneBranch(query.Part{
			Bindings: []query.Binding{
				must(query.NewNodeBinding("n", nil)),
			},
			Returns: []query.ReturnItem{
				{Name: "n", Value: query.NewRefProjection(query.Ref{Variable: "n"}, query.TypeNode{})},
			},
		}),
	},
	// Match1 [5] Use multiple MATCH clauses to do a Cartesian product: two nodes
	// in textual order [n, m]; explicit AS aliases (E1) become the column names.
	"comma pattern with aliases": {
		src: "MATCH (n), (m)\nRETURN n.num AS n, m.num AS m",
		want: oneBranch(query.Part{
			Bindings: []query.Binding{
				must(query.NewNodeBinding("n", nil)),
				must(query.NewNodeBinding("m", nil)),
			},
			Returns: []query.ReturnItem{
				{Name: "n", Value: query.NewRefProjection(query.Ref{Variable: "n", Property: "num"}, query.TypeUnknown{})},
				{Name: "m", Value: query.NewRefProjection(query.Ref{Variable: "m", Property: "num"}, query.TypeUnknown{})},
			},
		}),
	},
	// Match2 [1] Match non-existent relationships returns empty: C1 anonymous edge
	// is its own binding; C4 both endpoints are inline-empty (the () case).
	"anonymous edge": {
		src: "MATCH ()-[r]->()\nRETURN r",
		want: oneBranch(query.Part{
			Bindings: []query.Binding{
				must(query.NewEdgeBinding("r", nil,
					query.NewInlineEndpoint(nil),
					query.NewInlineEndpoint(nil),
					true,
				)),
			},
			Returns: []query.ReturnItem{
				{Name: "r", Value: query.NewRefProjection(query.Ref{Variable: "r"}, query.TypeEdge{})},
			},
		}),
	},
	// Match2 [2] label predicate on both sides: C4 anonymous endpoints carry
	// inline labels — [A] on the source, [B] on the target.
	"edge inline-labelled endpoints": {
		src: "MATCH (:A)-[r]->(:B)\nRETURN r",
		want: oneBranch(query.Part{
			Bindings: []query.Binding{
				must(query.NewEdgeBinding("r", nil,
					query.NewInlineEndpoint(graph.LabelSet{"A"}),
					query.NewInlineEndpoint(graph.LabelSet{"B"}),
					true,
				)),
			},
			Returns: []query.ReturnItem{
				{Name: "r", Value: query.NewRefProjection(query.Ref{Variable: "r"}, query.TypeEdge{})},
			},
		}),
	},
	// Match3 [1] Get neighbours: textual first-appearance order [n1, rel, n2];
	// var endpoints for named nodes (C4 — labels live on their bindings).
	"typed edge named endpoints": {
		src: "MATCH (n1)-[rel:KNOWS]->(n2)\nRETURN n1, n2",
		want: oneBranch(query.Part{
			Bindings: []query.Binding{
				must(query.NewNodeBinding("n1", nil)),
				must(query.NewEdgeBinding("rel", graph.LabelSet{"KNOWS"},
					must(query.NewVarEndpoint("n1")),
					must(query.NewVarEndpoint("n2")),
					true,
				)),
				must(query.NewNodeBinding("n2", nil)),
			},
			Returns: []query.ReturnItem{
				{Name: "n1", Value: query.NewRefProjection(query.Ref{Variable: "n1"}, query.TypeNode{})},
				{Name: "n2", Value: query.NewRefProjection(query.Ref{Variable: "n2"}, query.TypeNode{})},
			},
		}),
	},
	// Match3 [2] Directed match of a simple relationship: E3 whole-entity returns
	// → Ref{var, ""} for each; textual order [a, r, b].
	"directed edge whole entities": {
		src: "MATCH (a)-[r]->(b)\nRETURN a, r, b",
		want: oneBranch(query.Part{
			Bindings: []query.Binding{
				must(query.NewNodeBinding("a", nil)),
				must(query.NewEdgeBinding("r", nil,
					must(query.NewVarEndpoint("a")),
					must(query.NewVarEndpoint("b")),
					true,
				)),
				must(query.NewNodeBinding("b", nil)),
			},
			Returns: []query.ReturnItem{
				{Name: "a", Value: query.NewRefProjection(query.Ref{Variable: "a"}, query.TypeNode{})},
				{Name: "r", Value: query.NewRefProjection(query.Ref{Variable: "r"}, query.TypeEdge{})},
				{Name: "b", Value: query.NewRefProjection(query.Ref{Variable: "b"}, query.TypeNode{})},
			},
		}),
	},
	// Match3 [3] "Undirected match on simple relationship graph" — verbatim corpus
	// query (RETURN a, r, b), the undirected twin of "directed edge whole entities"
	// (Match3 [2]). Stage 5: directed=false (the trailing false), endpoints in
	// textual order [a, r, b] with no canonical flip (the resolver tries both).
	"undirected edge whole entities": {
		src: "MATCH (a)-[r]-(b)\nRETURN a, r, b",
		want: oneBranch(query.Part{
			Bindings: []query.Binding{
				must(query.NewNodeBinding("a", nil)),
				must(query.NewEdgeBinding("r", nil,
					must(query.NewVarEndpoint("a")),
					must(query.NewVarEndpoint("b")),
					false,
				)),
				must(query.NewNodeBinding("b", nil)),
			},
			Returns: []query.ReturnItem{
				{Name: "a", Value: query.NewRefProjection(query.Ref{Variable: "a"}, query.TypeNode{})},
				{Name: "r", Value: query.NewRefProjection(query.Ref{Variable: "r"}, query.TypeEdge{})},
				{Name: "b", Value: query.NewRefProjection(query.Ref{Variable: "b"}, query.TypeNode{})},
			},
		}),
	},
	// MatchWhere1 [6] parameter in a property predicate: D1a pairs $param with
	// b.name → one Parameter with Use PropertyUse{Ref{b, name}}.
	"where property parameter": {
		src: "MATCH (a)-[r]->(b)\nWHERE b.name = $param\nRETURN r",
		want: oneBranch(query.Part{
			Bindings: []query.Binding{
				must(query.NewNodeBinding("a", nil)),
				must(query.NewEdgeBinding("r", nil,
					must(query.NewVarEndpoint("a")),
					must(query.NewVarEndpoint("b")),
					true,
				)),
				must(query.NewNodeBinding("b", nil)),
			},
			Returns: []query.ReturnItem{
				{Name: "r", Value: query.NewRefProjection(query.Ref{Variable: "r"}, query.TypeEdge{})},
			},
		}, query.Parameter{Name: "param", Uses: []query.Use{
			query.NewPropertyUse(query.Ref{Variable: "b", Property: "name"}),
		}}),
	},
	// ReturnSkipLimit1 [2] "Start the result from second row by param" —
	// verbatim TCK query. Stage 1: SKIP $p is a clause-slot-typed parameter
	// use; the parameter carries one Use = ClauseSlotUse{Skip}, not a
	// property Ref. ORDER BY a bare var.prop is accept-and-ignored (E4).
	"skip parameter": {
		src: "MATCH (n)\nRETURN n\nORDER BY n.name ASC\nSKIP $skipAmount",
		want: oneBranch(query.Part{
			Bindings: []query.Binding{
				must(query.NewNodeBinding("n", nil)),
			},
			Returns: []query.ReturnItem{
				{Name: "n", Value: query.NewRefProjection(query.Ref{Variable: "n"}, query.TypeNode{})},
			},
		}, query.Parameter{Name: "skipAmount", Uses: []query.Use{
			query.NewClauseSlotUse(query.ClauseSlotSkip),
		}}),
	},
	// ReturnSkipLimit2 [10] "Negative parameter for LIMIT should fail" —
	// verbatim TCK query. The TCK asserts a runtime NegativeIntegerArgument
	// (parameter _limit = -1), which is out of scope for a parser; the query
	// parses fine and that's what we pin: LIMIT $p is a clause-slot-typed
	// parameter use carrying one Use = ClauseSlotUse{Limit}.
	"limit parameter": {
		src: "MATCH (p:Person)\nRETURN p.name AS name\nLIMIT $_limit",
		want: oneBranch(query.Part{
			Bindings: []query.Binding{
				must(query.NewNodeBinding("p", graph.LabelSet{"Person"})),
			},
			Returns: []query.ReturnItem{
				{Name: "name", Value: query.NewRefProjection(query.Ref{Variable: "p", Property: "name"}, query.TypeUnknown{})},
			},
		}, query.Parameter{Name: "_limit", Uses: []query.Use{
			query.NewClauseSlotUse(query.ClauseSlotLimit),
		}}),
	},
	// Create2 [4] control query: a left-pointing arc. C-Direction: the canonical
	// edge is source=b, target=a (the arrow's tail is the source) — independent of
	// how it was written. The relationship has no variable (anonymous edge, C1).
	// (TCK uses "When executing control query:" here, not "When executing query:",
	// so this scenario is outside our godog suite; the verbatim query is still
	// fair Layer-2 material.)
	"edge left-pointing canonical": {
		src: "MATCH (a:A)<-[:R]-(b:B)\nRETURN a, b",
		want: oneBranch(query.Part{
			Bindings: []query.Binding{
				must(query.NewNodeBinding("a", graph.LabelSet{"A"})),
				must(query.NewEdgeBinding("", graph.LabelSet{"R"},
					must(query.NewVarEndpoint("b")),
					must(query.NewVarEndpoint("a")),
					true,
				)),
				must(query.NewNodeBinding("b", graph.LabelSet{"B"})),
			},
			Returns: []query.ReturnItem{
				{Name: "a", Value: query.NewRefProjection(query.Ref{Variable: "a"}, query.TypeNode{})},
				{Name: "b", Value: query.NewRefProjection(query.Ref{Variable: "b"}, query.TypeNode{})},
			},
		}),
	},
	// Match7 [1] "Simple OPTIONAL MATCH on empty graph" — verbatim TCK query.
	// The single node binding is introduced in OPTIONAL MATCH, so its nullable
	// flag is true (ADR 0006) and it carries the clause's group id 1 (ay9).
	// The RETURN item traces back to it via Ref{n,""}.
	"optional match simple": {
		src: "OPTIONAL MATCH (n)\nRETURN n",
		want: oneBranch(query.Part{
			Bindings: []query.Binding{
				must(query.NewNullableNodeBindingInGroup("n", nil, 1)),
			},
			Returns: []query.ReturnItem{
				{Name: "n", Value: query.NewRefProjection(query.Ref{Variable: "n"}, query.TypeNode{})},
			},
		}),
	},
	// Match7 [2] "OPTIONAL MATCH with previously bound nodes" — verbatim TCK
	// query. Pins the reuse rule (ADR 0006): n is first introduced in the
	// required MATCH (non-nullable, group 0); the anonymous :NOT_EXIST edge
	// and x are first introduced in OPTIONAL MATCH (both nullable, sharing
	// the clause's group id 1 — ay9). The anonymous edge carries the group
	// uniformly even though no Ref reads it.
	"optional match reuses prior binding": {
		src: "MATCH (n)\nOPTIONAL MATCH (n)-[:NOT_EXIST]->(x)\nRETURN n, x",
		want: oneBranch(query.Part{
			Bindings: []query.Binding{
				must(query.NewNodeBinding("n", nil)),
				must(query.NewNullableEdgeBindingInGroup("", graph.LabelSet{"NOT_EXIST"},
					must(query.NewVarEndpoint("n")),
					must(query.NewVarEndpoint("x")),
					true,
					1,
				)),
				must(query.NewNullableNodeBindingInGroup("x", nil, 1)),
			},
			Returns: []query.ReturnItem{
				{Name: "n", Value: query.NewRefProjection(query.Ref{Variable: "n"}, query.TypeNode{})},
				{Name: "x", Value: query.NewRefProjection(query.Ref{Variable: "x"}, query.TypeNode{})},
			},
		}),
	},
	// ay9 spec §2.2 — two OPTIONAL MATCH clauses mint distinct group ids: a
	// takes the first clause's group 1, b the second clause's group 2. A group
	// per clause (not per query-wide OPTIONAL region) is what lets the
	// resolver's group-closure demotion discriminate a proven clause from an
	// unproven one.
	"two optional matches mint distinct groups": {
		src: "OPTIONAL MATCH (a)\nOPTIONAL MATCH (b)\nRETURN a, b",
		want: oneBranch(query.Part{
			Bindings: []query.Binding{
				must(query.NewNullableNodeBindingInGroup("a", nil, 1)),
				must(query.NewNullableNodeBindingInGroup("b", nil, 2)),
			},
			Returns: []query.ReturnItem{
				{Name: "a", Value: query.NewRefProjection(query.Ref{Variable: "a"}, query.TypeNode{})},
				{Name: "b", Value: query.NewRefProjection(query.Ref{Variable: "b"}, query.TypeNode{})},
			},
		}),
	},
	// ay9 spec §3.3 — group ids are query-scoped, not per-Part: the counter
	// does not reset at the WITH boundary, so Part 1's OPTIONAL clause mints
	// group 2, not a colliding "group 1 of Part 1". Query-scoped ids keep a
	// future cross-Part carry well-defined without re-minting.
	"optional group ids span parts without reset": {
		src: "OPTIONAL MATCH (a)\nWITH a\nOPTIONAL MATCH (b)\nRETURN a, b",
		want: query.Query{
			Branches: []query.Branch{{Parts: []query.Part{
				{
					Bindings: []query.Binding{
						must(query.NewNullableNodeBindingInGroup("a", nil, 1)),
					},
					Returns: []query.ReturnItem{
						{Name: "a", Value: query.NewRefProjection(query.Ref{Variable: "a"}, query.TypeNode{})},
					},
				},
				{
					Bindings: []query.Binding{
						must(query.NewNullableNodeBindingInGroup("b", nil, 2)),
					},
					Returns: []query.ReturnItem{
						{Name: "a", Value: query.NewRefProjection(query.Ref{Variable: "a"}, query.TypeNode{})},
						{Name: "b", Value: query.NewRefProjection(query.Ref{Variable: "b"}, query.TypeNode{})},
					},
				},
			}}},
		},
	},
	// 5xg spec §5.1.1 — a required (non-OPTIONAL) bare re-reference of an
	// OPTIONAL-introduced node sets the referencedInRequiredBarePattern flag
	// on the existing binding at mergeBinding's merge arm. `a` and the
	// anonymous edge stay flag-false: only `b` is bare-re-referenced (the
	// second MATCH's element has no adjacent chain).
	"required bare re-reference sets flag on optional node": {
		src: "OPTIONAL MATCH (a)-[:R]->(b)\nMATCH (b)\nRETURN b",
		want: oneBranch(query.Part{
			Bindings: []query.Binding{
				must(query.NewNullableNodeBindingInGroup("a", nil, 1)),
				must(query.NewNullableEdgeBindingInGroup("", graph.LabelSet{"R"},
					must(query.NewVarEndpoint("a")),
					must(query.NewVarEndpoint("b")),
					true,
					1,
				)),
				markBareRefNode(must(query.NewNullableNodeBindingInGroup("b", nil, 1))),
			},
			Returns: []query.ReturnItem{
				{Name: "b", Value: query.NewRefProjection(query.Ref{Variable: "b"}, query.TypeNode{})},
			},
		}),
	},
	// 5xg spec §5.1.2 — kill-probe: a required chain re-reference (b at the
	// head of `-[:S]->(c)`) has an adjacent edge on its right, so `bare` is
	// false at the merge site and the flag is NOT set. The existing R4
	// endpoint-witnessing already demotes `b` via the required :S edge; the
	// 5xg axis stays out of the way. This pin fails loudly if the parser
	// misfires the flag on non-bare chain re-references.
	"required chain re-reference does not set bare-ref flag": {
		src: "OPTIONAL MATCH (a)-[:R]->(b)\nMATCH (b)-[:S]->(c)\nRETURN b, c",
		want: oneBranch(query.Part{
			Bindings: []query.Binding{
				must(query.NewNullableNodeBindingInGroup("a", nil, 1)),
				must(query.NewNullableEdgeBindingInGroup("", graph.LabelSet{"R"},
					must(query.NewVarEndpoint("a")),
					must(query.NewVarEndpoint("b")),
					true,
					1,
				)),
				// b's flag is NOT set — the second occurrence is chain-headed,
				// not bare (a `-[:S]->` follows it in the same pattern element).
				must(query.NewNullableNodeBindingInGroup("b", nil, 1)),
				must(query.NewEdgeBinding("", graph.LabelSet{"S"},
					must(query.NewVarEndpoint("b")),
					must(query.NewVarEndpoint("c")),
					true,
				)),
				must(query.NewNodeBinding("c", nil)),
			},
			Returns: []query.ReturnItem{
				{Name: "b", Value: query.NewRefProjection(query.Ref{Variable: "b"}, query.TypeNode{})},
				{Name: "c", Value: query.NewRefProjection(query.Ref{Variable: "c"}, query.TypeNode{})},
			},
		}),
	},
	// Temporal4 [1] property return with no alias: E1 derives the column name from
	// the verbatim expression text — "n.created", not "created".
	"property return no alias": {
		src: "MATCH (n)\nRETURN n.created",
		want: oneBranch(query.Part{
			Bindings: []query.Binding{
				must(query.NewNodeBinding("n", nil)),
			},
			Returns: []query.ReturnItem{
				{Name: "n.created", Value: query.NewRefProjection(query.Ref{Variable: "n", Property: "created"}, query.TypeUnknown{})},
			},
		}),
	},
	// Stage 3 — canonical aggregate. count(*) is the degenerate aggregate: the
	// count-star atom, AggCount with no referenced bindings (it counts rows, not a
	// binding). Column name is the verbatim text "count(*)". The aggregate kind is
	// carried because it changes result cardinality (spec §4); the function's
	// identity below the boundary is not. Stage 10 upgrades the result type from
	// TypeUnknown to TypeInt — count returns an integer by openCypher spec.
	"count star aggregate": {
		src: "MATCH (n)\nRETURN count(*)",
		want: oneBranch(query.Part{
			Bindings: []query.Binding{
				must(query.NewNodeBinding("n", nil)),
			},
			Returns: []query.ReturnItem{
				{Name: "count(*)", Value: query.NewAggregateProjection(query.AggCount, nil, false, query.TypeInt{})},
			},
		}),
	},
	// Stage 3 — RETURN *. The query-level wildcard over in-scope bindings: the
	// honest schema-agnostic representation is ReturnsAll, with Returns empty (the
	// two are mutually exclusive at Stage 3, spec §3). The resolver expands * to
	// the in-scope bindings; the parser records "every in-scope
	// binding" without guessing the column list.
	"return all": {
		src: "MATCH (n)\nRETURN *",
		want: oneBranch(query.Part{
			Bindings: []query.Binding{
				must(query.NewNodeBinding("n", nil)),
			},
			ReturnsAll: true,
		}),
	},
	// Stage 4 — canonical WITH chain carrying a whole entity. Two parts in one
	// branch: part 1 binds a and its WITH projects the bare variable a (a
	// whole-entity RefProjection, empty Property — the resolver chases it back to
	// the binding); part 2 has no bindings of its own and RETURNs a, which
	// resolves against the name part 1 exported. The bare-variable WITH item's
	// name is the variable itself ("a").
	"with chain whole entity": {
		src: "MATCH (a)\nWITH a\nRETURN a",
		want: query.Query{
			Branches: []query.Branch{{Parts: []query.Part{
				{
					Bindings: []query.Binding{must(query.NewNodeBinding("a", nil))},
					Returns: []query.ReturnItem{
						{Name: "a", Value: query.NewRefProjection(query.Ref{Variable: "a"}, query.TypeNode{})},
					},
				},
				{
					Returns: []query.ReturnItem{
						{Name: "a", Value: query.NewRefProjection(query.Ref{Variable: "a"}, query.TypeNode{})},
					},
				},
			}}},
		},
	},
	// Stage 4 — canonical value-projecting WITH chain. WITH a.name AS n exports
	// the scalar name n (a RefProjection with a non-empty Property); part 2 RETURNs
	// n, resolving against the exported alias. The item's name is the AS alias.
	"with chain value projection": {
		src: "MATCH (a)\nWITH a.name AS n\nRETURN n",
		want: query.Query{
			Branches: []query.Branch{{Parts: []query.Part{
				{
					Bindings: []query.Binding{must(query.NewNodeBinding("a", nil))},
					Returns: []query.ReturnItem{
						{Name: "n", Value: query.NewRefProjection(query.Ref{Variable: "a", Property: "name"}, query.TypeUnknown{})},
					},
				},
				{
					Returns: []query.ReturnItem{
						// n is imported from part 1's WITH a.name AS n — a scalar
						// property lookup, so its Stage-6 result type is TypeUnknown
						// (the schema owns property typing, ADR 0003). The listener
						// looks up the imported name's type from part 1's exported
						// projection map.
						{Name: "n", Value: query.NewRefProjection(query.Ref{Variable: "n"}, query.TypeUnknown{})},
					},
				},
			}}},
		},
	},
	// With7 [2] "Multiple WITHs using a predicate and aggregation" — verbatim
	// TCK query (the "foaf shape"). The regression this locks is at expr.go's
	// mineWhere: the WHERE that follows a WITH-with-aggregation-alias references
	// a name (foaf) bound by the WITH projection itself, not by any earlier
	// pattern binding. The rich-typer sweep inside mineWhere would otherwise
	// append that alias's refs to curPart.refs and break the referential-
	// integrity sweep at build; a snapshot/restore of l.curPart.refs around
	// typeExpressionMining (expr.go:450–452) discards them. The pin catches a
	// refactor that drops the snapshot restore — Part 1's Returns must be
	// exactly [otherPerson] (no leaked foaf/count(*) refs), and Parameters must
	// stay nil (the literal-vs-alias predicate mines nothing).
	"with-aggregate-where scope snapshot (foaf)": {
		src: "MATCH (david {name: 'David'})--(otherPerson)-->()\nWITH otherPerson, count(*) AS foaf\nWHERE foaf > 1\nWITH otherPerson\nWHERE otherPerson.name <> 'NotOther'\nRETURN count(*)",
		want: query.Query{
			Branches: []query.Branch{{Parts: []query.Part{
				{
					Bindings: []query.Binding{
						must(query.NewNodeBinding("david", nil)),
						must(query.NewEdgeBinding("", nil,
							must(query.NewVarEndpoint("david")),
							must(query.NewVarEndpoint("otherPerson")),
							false,
						)),
						must(query.NewNodeBinding("otherPerson", nil)),
						must(query.NewEdgeBinding("", nil,
							must(query.NewVarEndpoint("otherPerson")),
							query.NewInlineEndpoint(nil),
							true,
						)),
					},
					Returns: []query.ReturnItem{
						{Name: "otherPerson", Value: query.NewRefProjection(query.Ref{Variable: "otherPerson"}, query.TypeNode{})},
						{Name: "foaf", Value: query.NewAggregateProjection(query.AggCount, nil, false, query.TypeInt{})},
					},
				},
				{
					Returns: []query.ReturnItem{
						{Name: "otherPerson", Value: query.NewRefProjection(query.Ref{Variable: "otherPerson"}, query.TypeNode{})},
					},
				},
				{
					Returns: []query.ReturnItem{
						{Name: "count(*)", Value: query.NewAggregateProjection(query.AggCount, nil, false, query.TypeInt{})},
					},
				},
			}}},
		},
	},
	// Stage 4 — canonical two-branch UNION (distinct). Each branch is one part
	// with its own binding; Combinators has one entry, UnionDistinct. The branches
	// are recorded verbatim in source order — the parser does not pre-pick branch 0
	// as the result-naming branch (that is the resolver's rule, spec §3).
	"union two branches": {
		src: "MATCH (a)\nRETURN a\nUNION\nMATCH (b)\nRETURN b",
		want: query.Query{
			Branches: []query.Branch{
				{Parts: []query.Part{{
					Bindings: []query.Binding{must(query.NewNodeBinding("a", nil))},
					Returns: []query.ReturnItem{
						{Name: "a", Value: query.NewRefProjection(query.Ref{Variable: "a"}, query.TypeNode{})},
					},
				}}},
				{Parts: []query.Part{{
					Bindings: []query.Binding{must(query.NewNodeBinding("b", nil))},
					Returns: []query.ReturnItem{
						{Name: "b", Value: query.NewRefProjection(query.Ref{Variable: "b"}, query.TypeNode{})},
					},
				}}},
			},
			Combinators: []query.UnionKind{query.UnionDistinct},
		},
	},
	// gqlc-qcc — UNION branch attribution on Uses (ADR 0008 amendment
	// 2026-07-12). One shared parameter with a Use in each branch: the first
	// mines at (part 0, branch 0); the second, behind branch 1's WITH, at
	// (part 1, branch 1). Pins addParameterUse's currentBranchIndex stamping —
	// the resolver routes each Use to its branch's scope table by this
	// coordinate, so a regression here silently mis-witnesses UNION parameters.
	"union param branch attribution": {
		src: "MATCH (a) WHERE a.title = $p\nRETURN a\nUNION\nMATCH (b)\nWITH b\nMATCH (c) WHERE c.name = $p\nRETURN c",
		want: query.Query{
			Branches: []query.Branch{
				{Parts: []query.Part{{
					Bindings: []query.Binding{must(query.NewNodeBinding("a", nil))},
					Returns: []query.ReturnItem{
						{Name: "a", Value: query.NewRefProjection(query.Ref{Variable: "a"}, query.TypeNode{})},
					},
				}}},
				{Parts: []query.Part{
					{
						Bindings: []query.Binding{must(query.NewNodeBinding("b", nil))},
						Returns: []query.ReturnItem{
							{Name: "b", Value: query.NewRefProjection(query.Ref{Variable: "b"}, query.TypeNode{})},
						},
					},
					{
						Bindings: []query.Binding{must(query.NewNodeBinding("c", nil))},
						Returns: []query.ReturnItem{
							{Name: "c", Value: query.NewRefProjection(query.Ref{Variable: "c"}, query.TypeNode{})},
						},
					},
				}},
			},
			Combinators: []query.UnionKind{query.UnionDistinct},
			Parameters: []query.Parameter{{Name: "p", Uses: []query.Use{
				query.NewPropertyUseAt(query.Ref{Variable: "a", Property: "title"}, 0, 0),
				query.NewPropertyUseAt(query.Ref{Variable: "c", Property: "name"}, 1, 1),
			}}},
		},
	},
	// Stage 6 — canonical arithmetic-in-RETURN. "Arithmetic precedence test"
	// (Mathematical8 [1] verbatim). All operands are integer literals, so the
	// parser types the result as TypeInt. The projection is an ExprProjection
	// (no bindings touched, so refs are nil). The item's name is the verbatim
	// expression text.
	"arithmetic in return": {
		src: "RETURN 12 / 4 * 3 - 2 * 4",
		want: query.Query{Branches: []query.Branch{{Parts: []query.Part{{
			Returns: []query.ReturnItem{
				{Name: "12 / 4 * 3 - 2 * 4", Value: query.NewExprProjection(nil, query.TypeInt{})},
			},
		}}}}},
	},
	// Stage 6 — canonical IS NULL predicate. "Property null check on non-null
	// node" (Null1 [1] verbatim). n.missing IS NULL is a predicate: result type
	// TypeBool; the sub-expression touches n via the property lookup, so the
	// ExprProjection carries one Ref. The literal-null lookup and property lookup
	// on the same expression yields a single Ref to n.
	"is null in return": {
		src: "MATCH (n)\nRETURN n.missing IS NULL",
		want: oneBranch(query.Part{
			Bindings: []query.Binding{must(query.NewNodeBinding("n", nil))},
			Returns: []query.ReturnItem{
				{Name: "n.missing IS NULL", Value: query.NewExprProjection(
					[]query.Ref{{Variable: "n", Property: "missing"}},
					query.TypeBool{},
				)},
			},
		}),
	},
	// Stage 6 — arithmetic over a projection: RETURN n.num + 1. Reclassified
	// from a mustReject (former ErrUnsupportedProjection fail-site pre-Stage-6).
	// The addition of an unknown-typed property lookup and an int literal
	// collapses to TypeUnknown under promoteArith (unknown propagates), and
	// the ref to n.num is mined for referential integrity.
	"arithmetic over projection": {
		src: "MATCH (n)\nRETURN n.num + 1",
		want: oneBranch(query.Part{
			Bindings: []query.Binding{must(query.NewNodeBinding("n", nil))},
			Returns: []query.ReturnItem{
				{Name: "n.num + 1", Value: query.NewExprProjection(
					[]query.Ref{{Variable: "n", Property: "num"}},
					query.TypeUnknown{},
				)},
			},
		}),
	},
	// Stage 6 — unary-signed projection: RETURN -n.num. Reclassified from a
	// mustReject. A leading sign does not change the operand's arithmetic type;
	// but n.num types as TypeUnknown (property lookup), so the result is
	// TypeUnknown. The ref carries the property.
	"unary-signed projection": {
		src: "MATCH (n)\nRETURN -n.num",
		want: oneBranch(query.Part{
			Bindings: []query.Binding{must(query.NewNodeBinding("n", nil))},
			Returns: []query.ReturnItem{
				{Name: "-n.num", Value: query.NewExprProjection(
					[]query.Ref{{Variable: "n", Property: "num"}},
					query.TypeUnknown{},
				)},
			},
		}),
	},
	// Stage 6 — canonical list literal. A list of integer literals types as
	// TypeList(TypeInt); no bindings are touched.
	"list literal in return": {
		src: "RETURN [1, 2, 3]",
		want: query.Query{Branches: []query.Branch{{Parts: []query.Part{{
			Returns: []query.ReturnItem{
				{Name: "[1, 2, 3]", Value: query.NewExprProjection(nil, query.NewTypeList(query.TypeInt{}))},
			},
		}}}}},
	},
	// Stage 6 — parameter inside a rich projection: RETURN $x. Authored to
	// pin the spec §4 "no parameter is silently dropped" rule at projection
	// position. A bare-$p RETURN is a rich shape (the projection classifier
	// falls through to the rich typer), so $x is recorded as an ExprUse on
	// the parameter — enclosingType is TypeUnknown (parameter's own type is
	// below the boundary at parse time) and position is projection.
	"return bare param": {
		src: "RETURN $x",
		want: query.Query{
			Branches: []query.Branch{{Parts: []query.Part{{
				Returns: []query.ReturnItem{
					{Name: "$x", Value: query.NewExprProjection(nil, query.TypeUnknown{})},
				},
			}}}},
			Parameters: []query.Parameter{
				{Name: "x", Uses: []query.Use{
					query.NewExprUse(query.TypeUnknown{}, query.ExprInProjection),
				}},
			},
		},
	},
	// Stage 6 — parameter inside a rich arithmetic projection: RETURN a.n + $delta.
	// mineComparisons doesn't fire (no comparison; RETURN never runs
	// mineWhere), so the pair miner does not touch $delta. The rich typer
	// records $delta as an ExprUse{enclosingType=TypeUnknown (a.n propagates
	// unknown), ExprInProjection}. The ref a.n is mined for referential
	// integrity.
	"return rich param": {
		src: "MATCH (a)\nRETURN a.n + $delta",
		want: query.Query{
			Branches: []query.Branch{{Parts: []query.Part{{
				Bindings: []query.Binding{must(query.NewNodeBinding("a", nil))},
				Returns: []query.ReturnItem{
					{Name: "a.n + $delta", Value: query.NewExprProjection(
						[]query.Ref{{Variable: "a", Property: "n"}},
						query.TypeUnknown{},
					)},
				},
			}}}},
			Parameters: []query.Parameter{
				{Name: "delta", Uses: []query.Use{
					query.NewExprUse(query.TypeUnknown{}, query.ExprInProjection),
				}},
			},
		},
	},
	// Stage 6 — parameter inside a rich WHERE predicate: a.n + $x > 5. The
	// pair miner catches neither `a.n + $x` (arithmetic, not bare) nor `5`
	// (literal) as a var.prop-vs-$p pair, so $x falls through to the rich
	// typer and records an ExprUse{enclosingType=TypeBool (the > predicate),
	// ExprInPredicate}. The reviewer flagged this as a should-fix: spec §4
	// commits to WHERE-side ExprUse.
	"where rich param": {
		src: "MATCH (a)\nWHERE a.n + $x > 5\nRETURN a",
		want: query.Query{
			Branches: []query.Branch{{Parts: []query.Part{{
				Bindings: []query.Binding{must(query.NewNodeBinding("a", nil))},
				Returns: []query.ReturnItem{
					{Name: "a", Value: query.NewRefProjection(query.Ref{Variable: "a"}, query.TypeNode{})},
				},
			}}}},
			Parameters: []query.Parameter{
				{Name: "x", Uses: []query.Use{
					query.NewExprUse(query.TypeBool{}, query.ExprInPredicate),
				}},
			},
		},
	},
	// Stage 6 — parameter directly inside a list literal in RETURN:
	// RETURN [1, $x, 3]. The rich typer walks each element expression via
	// listLiteralType, so $x is visited under typeAtom and mined into
	// exprParams. Element types diverge (int, unknown from param, int) →
	// list<unknown>. The parameter records an ExprUse against that list
	// type at ExprInProjection.
	"return list literal with param": {
		src: "RETURN [1, $x, 3]",
		want: query.Query{
			Branches: []query.Branch{{Parts: []query.Part{{
				Returns: []query.ReturnItem{
					{Name: "[1, $x, 3]", Value: query.NewExprProjection(nil,
						query.NewTypeList(query.TypeUnknown{}))},
				},
			}}}},
			Parameters: []query.Parameter{
				{Name: "x", Uses: []query.Use{
					query.NewExprUse(query.NewTypeList(query.TypeUnknown{}), query.ExprInProjection),
				}},
			},
		},
	},
	// Stage 6 — CASE WHEN … THEN … ELSE … END. Pins that WHEN predicates
	// contribute refs only, never arm-type: the boolean WHEN is walked but
	// its type is not unified with the value-producing arms. THEN 'a' and
	// ELSE 'b' both type as TypeString, so the CASE types as TypeString
	// (not TypeUnknown, which is what happens if a boolean WHEN sneaks
	// into arm-type unification).
	"case when-then-else types by arms only": {
		src: "RETURN CASE WHEN true THEN 'a' ELSE 'b' END",
		want: query.Query{Branches: []query.Branch{{Parts: []query.Part{{
			Returns: []query.ReturnItem{
				{Name: "CASE WHEN true THEN 'a' ELSE 'b' END", Value: query.NewExprProjection(nil, query.TypeString{})},
			},
		}}}}},
	},
	// Stage 4 — UNION ALL variant. Same two-branch shape; the combinator is
	// UnionAll (the ALL token is present), the cardinality-preserving join.
	"union all two branches": {
		src: "MATCH (a)\nRETURN a\nUNION ALL\nMATCH (b)\nRETURN b",
		want: query.Query{
			Branches: []query.Branch{
				{Parts: []query.Part{{
					Bindings: []query.Binding{must(query.NewNodeBinding("a", nil))},
					Returns: []query.ReturnItem{
						{Name: "a", Value: query.NewRefProjection(query.Ref{Variable: "a"}, query.TypeNode{})},
					},
				}}},
				{Parts: []query.Part{{
					Bindings: []query.Binding{must(query.NewNodeBinding("b", nil))},
					Returns: []query.ReturnItem{
						{Name: "b", Value: query.NewRefProjection(query.Ref{Variable: "b"}, query.TypeNode{})},
					},
				}}},
			},
			Combinators: []query.UnionKind{query.UnionAll},
		},
	},
	// Stage 7 — bare temporal constructor at RETURN. date('2024-01-01') is a
	// bare-atom function invocation, so the projection is a FuncProjection
	// (no ExprProjection promotion); Stage 7's constructor lookup widens the
	// FuncProjection's Type() to TypeDate (Stage 7 spec §4).
	"return date constructor": {
		src: "RETURN date('2024-01-01') AS d",
		want: query.Query{Branches: []query.Branch{{Parts: []query.Part{{
			Returns: []query.ReturnItem{
				{Name: "d", Value: query.NewFuncProjection(nil, query.TypeDate{})},
			},
		}}}}},
	},
	// Stage 7 — bare duration constructor. Same shape as the date pin above;
	// the duration name maps to TypeDuration.
	"return duration constructor": {
		src: "RETURN duration('P1D') AS d",
		want: query.Query{Branches: []query.Branch{{Parts: []query.Part{{
			Returns: []query.ReturnItem{
				{Name: "d", Value: query.NewFuncProjection(nil, query.TypeDuration{})},
			},
		}}}}},
	},
	// Stage 7 — temporal arithmetic: date + duration → date. Not a bare
	// atom (arithmetic), so falls through to the rich classifier's
	// ExprProjection. The rich typer types the LHS via the constructor
	// lookup (TypeDate), the RHS likewise (TypeDuration), and promoteAddSub
	// yields TypeDate. Both function invocations mine no refs (literal args
	// only).
	"return date plus duration": {
		src: "RETURN date() + duration('P1D') AS d",
		want: query.Query{Branches: []query.Branch{{Parts: []query.Part{{
			Returns: []query.ReturnItem{
				{Name: "d", Value: query.NewExprProjection(nil, query.TypeDate{})},
			},
		}}}}},
	},
	// Stage 7 — duration times a scalar: duration * int → duration. Rich
	// classifier route; promoteMulDiv yields TypeDuration.
	"return duration times scalar": {
		src: "RETURN duration('P1D') * 3 AS d",
		want: query.Query{Branches: []query.Branch{{Parts: []query.Part{{
			Returns: []query.ReturnItem{
				{Name: "d", Value: query.NewExprProjection(nil, query.TypeDuration{})},
			},
		}}}}},
	},
	// Stage 7 — property lookup on a temporally-typed atom. The
	// typeNonArithmetic property-lookup arm collapses to TypeUnknown
	// (accessor result type is schema-owned; Stage 7 spec §3), matching
	// the Stage-6 posture for property lookups. Not a bare-atom shape
	// (a property lookup on a function call), so the projection is an
	// ExprProjection. No bindings, no refs.
	"return temporal accessor": {
		src: "RETURN date().year AS y",
		want: query.Query{Branches: []query.Branch{{Parts: []query.Part{{
			Returns: []query.ReturnItem{
				{Name: "y", Value: query.NewExprProjection(nil, query.TypeUnknown{})},
			},
		}}}}},
	},
	// Stage 7 — namespaced constructor: duration.between(t1, t2) yields
	// TypeDuration. Bare-atom shape (a single function invocation atom),
	// so it is a FuncProjection carrying the temporal type. Every argument
	// is a bare literal, so no refs are mined.
	"return duration between": {
		src: "RETURN duration.between('P1D', 'P2D') AS d",
		want: query.Query{Branches: []query.Branch{{Parts: []query.Part{{
			Returns: []query.ReturnItem{
				{Name: "d", Value: query.NewFuncProjection(nil, query.TypeDuration{})},
			},
		}}}}},
	},
	// gqlc-v5t — elementId(node) is a builtin scalar function whose
	// grammar-level result type is string. The bare-atom projection is a
	// FuncProjection whose refs mine the node binding and whose Type()
	// widens from TypeUnknown to TypeString. Enables ADR 0010 D3's
	// identity pattern (RETURN p, elementId(p) AS id) to render as a
	// typed Row field in generated code.
	"return elementId on node": {
		src: "MATCH (p)\nRETURN elementId(p) AS id",
		want: oneBranch(query.Part{
			Bindings: []query.Binding{
				must(query.NewNodeBinding("p", nil)),
			},
			Returns: []query.ReturnItem{
				{Name: "id", Value: query.NewFuncProjection([]query.Ref{{Variable: "p"}}, query.TypeString{})},
			},
		}),
	},
	// gqlc-v5t — elementId(edge) is the twin of the node case: same
	// builtin, same string result. Pins that the arg-kind widening does
	// not accidentally require node-ness.
	"return elementId on edge": {
		src: "MATCH ()-[r]->()\nRETURN elementId(r) AS id",
		want: oneBranch(query.Part{
			Bindings: []query.Binding{
				must(query.NewEdgeBinding("r", nil,
					query.NewInlineEndpoint(nil),
					query.NewInlineEndpoint(nil),
					true,
				)),
			},
			Returns: []query.ReturnItem{
				{Name: "id", Value: query.NewFuncProjection([]query.Ref{{Variable: "r"}}, query.TypeString{})},
			},
		}),
	},
	// gqlc-v5t — id(node) is the second table entry, same shape as
	// elementId but returning TypeInt (the deprecated-but-still-valid
	// integer identity). Two-entry coverage proves the mechanism is
	// generic, not glued to a single builtin.
	"return id on node": {
		src: "MATCH (p)\nRETURN id(p) AS pid",
		want: oneBranch(query.Part{
			Bindings: []query.Binding{
				must(query.NewNodeBinding("p", nil)),
			},
			Returns: []query.ReturnItem{
				{Name: "pid", Value: query.NewFuncProjection([]query.Ref{{Variable: "p"}}, query.TypeInt{})},
			},
		}),
	},
	// gqlc-v5t — elementId on a property lookup falls through to the
	// honest TypeUnknown. The property-lookup atom refType returns
	// TypeUnknown at the parser boundary (schema-owned), so the builtin
	// table's arg-shape check does not match — the projection carries
	// TypeUnknown, matching the pre-widening posture for every non-
	// entity argument.
	"return elementId on property is unknown": {
		src: "MATCH (p)\nRETURN elementId(p.id) AS x",
		want: oneBranch(query.Part{
			Bindings: []query.Binding{
				must(query.NewNodeBinding("p", nil)),
			},
			Returns: []query.ReturnItem{
				{Name: "x", Value: query.NewFuncProjection([]query.Ref{{Variable: "p", Property: "id"}}, query.TypeUnknown{})},
			},
		}),
	},
	// gqlc-v5t — elementId used inside a rich expression exercises the
	// typing.typeAtom builtin-widening arm (the counterpart to
	// classifyFunction's arm). string + string promotes to string via
	// promoteBase; the outer projection is an ExprProjection whose
	// result type is TypeString and whose refs mine the node binding
	// once. Pins that the two call sites cannot disagree on the same
	// builtin.
	"return elementId concatenated with literal": {
		src: "MATCH (p)\nRETURN elementId(p) + '-suffix' AS x",
		want: oneBranch(query.Part{
			Bindings: []query.Binding{
				must(query.NewNodeBinding("p", nil)),
			},
			Returns: []query.ReturnItem{
				{Name: "x", Value: query.NewExprProjection([]query.Ref{{Variable: "p"}}, query.TypeString{})},
			},
		}),
	},
	// gqlc-v5t — a namespaced call whose bare tail is `elementid`
	// (`foo.elementid(p)`) must NOT match the builtin table; the
	// namespaced-name gate on `functionName` is what makes shadowing
	// impossible. Pin so a future functionName refactor cannot silently
	// unshadow the table.
	"return namespaced elementId does not match builtin": {
		src: "MATCH (p)\nRETURN foo.elementid(p) AS x",
		want: oneBranch(query.Part{
			Bindings: []query.Binding{
				must(query.NewNodeBinding("p", nil)),
			},
			Returns: []query.ReturnItem{
				{Name: "x", Value: query.NewFuncProjection([]query.Ref{{Variable: "p"}}, query.TypeUnknown{})},
			},
		}),
	},
	// Stage 7 — temporal-point minus duration → temporal-point. Spec §1's
	// subtraction rule is one-way (temporal - duration is legal; the
	// commutation is not). Rich classifier route, ExprProjection.
	"return date minus duration": {
		src: "RETURN date() - duration('P1D') AS d",
		want: query.Query{Branches: []query.Branch{{Parts: []query.Part{{
			Returns: []query.ReturnItem{
				{Name: "d", Value: query.NewExprProjection(nil, query.TypeDate{})},
			},
		}}}}},
	},
	// Stage 7 — duration minus duration → duration. Spec §1 rule table
	// commits the same-kind subtraction as duration-producing.
	"return duration minus duration": {
		src: "RETURN duration('P1D') - duration('PT1H') AS d",
		want: query.Query{Branches: []query.Branch{{Parts: []query.Part{{
			Returns: []query.ReturnItem{
				{Name: "d", Value: query.NewExprProjection(nil, query.TypeDuration{})},
			},
		}}}}},
	},
	// Stage 7 — duration divided by a scalar → duration. Spec §1 commits
	// division only with duration on the left; the reverse is TypeUnknown
	// (see the reject pin below).
	"return duration divided by scalar": {
		src: "RETURN duration('P1D') / 3 AS d",
		want: query.Query{Branches: []query.Branch{{Parts: []query.Part{{
			Returns: []query.ReturnItem{
				{Name: "d", Value: query.NewExprProjection(nil, query.TypeDuration{})},
			},
		}}}}},
	},
	// Stage 7 — duration - <temporal-point> is out of scope of the spec's
	// rule table (subtraction is one-way; there is no "duration - date"
	// legal in openCypher, and inventing a concrete type here would be
	// strictly worse than the honest TypeUnknown a schema-driven resolver
	// can upgrade). Rich classifier route, TypeUnknown result.
	"return duration minus date is unknown": {
		src: "RETURN duration('P1D') - date() AS d",
		want: query.Query{Branches: []query.Branch{{Parts: []query.Part{{
			Returns: []query.ReturnItem{
				{Name: "d", Value: query.NewExprProjection(nil, query.TypeUnknown{})},
			},
		}}}}},
	},
	// Stage 7 — scalar divided by a duration is out of scope of the spec's
	// rule table (division is one-way; number / duration has no committed
	// result type and is left honestly TypeUnknown).
	"return scalar divided by duration is unknown": {
		src: "RETURN 3 / duration('P1D') AS d",
		want: query.Query{Branches: []query.Branch{{Parts: []query.Part{{
			Returns: []query.ReturnItem{
				{Name: "d", Value: query.NewExprProjection(nil, query.TypeUnknown{})},
			},
		}}}}},
	},
	// Stage 8 — named path projected. `MATCH p = (a)-[r]->(b) RETURN p`: the
	// pattern element is collected as three bindings (a, r, b) in textual
	// order; the named path adds a PathBinding whose members list the three
	// variable names in order. The RETURN item's type is TypePath — the
	// resolver reads the path variable's members via the binding.
	"named path projected": {
		src: "MATCH p = (a)-[r]->(b)\nRETURN p",
		want: oneBranch(query.Part{
			Bindings: []query.Binding{
				must(query.NewNodeBinding("a", nil)),
				must(query.NewEdgeBinding("r", nil,
					must(query.NewVarEndpoint("a")),
					must(query.NewVarEndpoint("b")),
					true,
				)),
				must(query.NewNodeBinding("b", nil)),
				must(query.NewPathBinding("p", []query.PathMember{
					must(query.NewNamedNodeMember("a")),
					must(query.NewNamedEdgeMember("r")),
					must(query.NewNamedNodeMember("b")),
				})),
			},
			Returns: []query.ReturnItem{
				{Name: "p", Value: query.NewRefProjection(query.Ref{Variable: "p"}, query.TypePath{})},
			},
		}),
	},
	// Stage 8 — named path over an anonymous edge. `MATCH p = (a)-[]-(b)`:
	// the anonymous edge is a binding of its own (C1) but has no user-given
	// name, so it surfaces on the PathBinding as an AnonEdgeMember — no
	// name, no byVar collision (§1.2).
	"named path over anonymous edge": {
		src: "MATCH p = (a)-[]-(b)\nRETURN p",
		want: oneBranch(query.Part{
			Bindings: []query.Binding{
				must(query.NewNodeBinding("a", nil)),
				must(query.NewEdgeBinding("", nil,
					must(query.NewVarEndpoint("a")),
					must(query.NewVarEndpoint("b")),
					false,
				)),
				must(query.NewNodeBinding("b", nil)),
				must(query.NewPathBinding("p", []query.PathMember{
					must(query.NewNamedNodeMember("a")),
					query.AnonEdgeMember{},
					must(query.NewNamedNodeMember("b")),
				})),
			},
			Returns: []query.ReturnItem{
				{Name: "p", Value: query.NewRefProjection(query.Ref{Variable: "p"}, query.TypePath{})},
			},
		}),
	},
	// Stage 8 (fix round) — the B1 collision case: a user pattern binds a
	// node named literally `__anon_edge_0` (a legal oC_SymbolicName) and the
	// path also contains anonymous edges. Under the pre-fix string-only
	// members, the user's node name and the synthetic edge name both
	// occupied the byVar namespace on the members list; the tagged sum
	// makes the collision unrepresentable — the user's node is a
	// NamedNodeMember(__anon_edge_0), the anonymous edges are
	// AnonEdgeMember{} slots, and byVar lookup on either is unambiguous.
	"named path with user variable named __anon_edge_0": {
		src: "MATCH p = (__anon_edge_0)-[]-(b)-[]-(c)\nRETURN p",
		want: oneBranch(query.Part{
			Bindings: []query.Binding{
				must(query.NewNodeBinding("__anon_edge_0", nil)),
				must(query.NewEdgeBinding("", nil,
					must(query.NewVarEndpoint("__anon_edge_0")),
					must(query.NewVarEndpoint("b")),
					false,
				)),
				must(query.NewNodeBinding("b", nil)),
				must(query.NewEdgeBinding("", nil,
					must(query.NewVarEndpoint("b")),
					must(query.NewVarEndpoint("c")),
					false,
				)),
				must(query.NewNodeBinding("c", nil)),
				must(query.NewPathBinding("p", []query.PathMember{
					must(query.NewNamedNodeMember("__anon_edge_0")),
					query.AnonEdgeMember{},
					must(query.NewNamedNodeMember("b")),
					query.AnonEdgeMember{},
					must(query.NewNamedNodeMember("c")),
				})),
			},
			Returns: []query.ReturnItem{
				{Name: "p", Value: query.NewRefProjection(query.Ref{Variable: "p"}, query.TypePath{})},
			},
		}),
	},
	// Stage 8 (fix round) — the SF1 shape-faithful case: a chain with an
	// anonymous intermediate node `-()- ` inside a named path records an
	// AnonNodeMember{} at its position, so the members slice has five
	// entries for a 5-element chain instead of silently dropping the
	// middle node to 4. Every binding the pattern emits is still there
	// (a, edge, edge, b), but the path shape is now reconstructable from
	// the members list alone (§1.2).
	"named path with anonymous intermediate node": {
		src: "MATCH p = (a)-[]-()-[]-(b)\nRETURN p",
		want: oneBranch(query.Part{
			Bindings: []query.Binding{
				must(query.NewNodeBinding("a", nil)),
				must(query.NewEdgeBinding("", nil,
					must(query.NewVarEndpoint("a")),
					query.NewInlineEndpoint(nil),
					false,
				)),
				must(query.NewEdgeBinding("", nil,
					query.NewInlineEndpoint(nil),
					must(query.NewVarEndpoint("b")),
					false,
				)),
				must(query.NewNodeBinding("b", nil)),
				must(query.NewPathBinding("p", []query.PathMember{
					must(query.NewNamedNodeMember("a")),
					query.AnonEdgeMember{},
					query.AnonNodeMember{},
					query.AnonEdgeMember{},
					must(query.NewNamedNodeMember("b")),
				})),
			},
			Returns: []query.ReturnItem{
				{Name: "p", Value: query.NewRefProjection(query.Ref{Variable: "p"}, query.TypePath{})},
			},
		}),
	},
	// Stage 8 — variable-length edge, bare `[*]`. The edge binding carries a
	// non-nil Hops with both bounds nil; the RETURN item's type is
	// list<edge>, computed by refType when the ref names a var-length edge.
	"var-length edge unbounded": {
		src: "MATCH (a)-[r*]->(b)\nRETURN r",
		want: oneBranch(query.Part{
			Bindings: []query.Binding{
				must(query.NewNodeBinding("a", nil)),
				must(query.NewVarLengthEdgeBinding("r", nil,
					must(query.NewVarEndpoint("a")),
					must(query.NewVarEndpoint("b")),
					true,
					must(query.NewEdgeHops(nil, nil)),
				)),
				must(query.NewNodeBinding("b", nil)),
			},
			Returns: []query.ReturnItem{
				{Name: "r", Value: query.NewRefProjection(query.Ref{Variable: "r"}, query.NewTypeList(query.TypeEdge{}))},
			},
		}),
	},
	// Stage 8 — variable-length edge, bounded `[*1..3]`.
	"var-length edge bounded": {
		src: "MATCH (a)-[r*1..3]->(b)\nRETURN r",
		want: oneBranch(query.Part{
			Bindings: []query.Binding{
				must(query.NewNodeBinding("a", nil)),
				must(query.NewVarLengthEdgeBinding("r", nil,
					must(query.NewVarEndpoint("a")),
					must(query.NewVarEndpoint("b")),
					true,
					must(query.NewEdgeHops(new(1), new(3))),
				)),
				must(query.NewNodeBinding("b", nil)),
			},
			Returns: []query.ReturnItem{
				{Name: "r", Value: query.NewRefProjection(query.Ref{Variable: "r"}, query.NewTypeList(query.TypeEdge{}))},
			},
		}),
	},
	// Stage 8 — variable-length undirected edge `-[*]-`. Pins the
	// composition of the Stage-5 direction axis and the Stage-8 hop axis:
	// both are recorded independently on the binding.
	"var-length edge undirected": {
		src: "MATCH (a)-[r*]-(b)\nRETURN r",
		want: oneBranch(query.Part{
			Bindings: []query.Binding{
				must(query.NewNodeBinding("a", nil)),
				must(query.NewVarLengthEdgeBinding("r", nil,
					must(query.NewVarEndpoint("a")),
					must(query.NewVarEndpoint("b")),
					false,
					must(query.NewEdgeHops(nil, nil)),
				)),
				must(query.NewNodeBinding("b", nil)),
			},
			Returns: []query.ReturnItem{
				{Name: "r", Value: query.NewRefProjection(query.Ref{Variable: "r"}, query.NewTypeList(query.TypeEdge{}))},
			},
		}),
	},
	// Stage 8 — multi-type relationship `[r:A|B]`. The two types are recorded
	// on the binding's LabelSet in textual first-appearance order; the
	// resolver forms one candidate EdgeKey per type. The RETURN
	// item still types as TypeEdge (a multi-type edge is still a single-hop
	// edge from the type-interface's perspective; the label-set carries the
	// widened admissible type set).
	"multi-type edge": {
		src: "MATCH (a)-[r:KNOWS|LOVES]->(b)\nRETURN r",
		want: oneBranch(query.Part{
			Bindings: []query.Binding{
				must(query.NewNodeBinding("a", nil)),
				must(query.NewEdgeBinding("r", graph.LabelSet{"KNOWS", "LOVES"},
					must(query.NewVarEndpoint("a")),
					must(query.NewVarEndpoint("b")),
					true,
				)),
				must(query.NewNodeBinding("b", nil)),
			},
			Returns: []query.ReturnItem{
				{Name: "r", Value: query.NewRefProjection(query.Ref{Variable: "r"}, query.TypeEdge{})},
			},
		}),
	},
	// Stage 8 — named path over a var-length anonymous edge. Combines every
	// axis: the path binds three members (a, anonymous edge, b), the middle
	// member is a var-length anonymous edge with bounded hops, and the
	// RETURN item's type is TypePath.
	"named path over var-length anonymous edge": {
		src: "MATCH p = (a)-[*1..3]->(b)\nRETURN p",
		want: oneBranch(query.Part{
			Bindings: []query.Binding{
				must(query.NewNodeBinding("a", nil)),
				must(query.NewVarLengthEdgeBinding("", nil,
					must(query.NewVarEndpoint("a")),
					must(query.NewVarEndpoint("b")),
					true,
					must(query.NewEdgeHops(new(1), new(3))),
				)),
				must(query.NewNodeBinding("b", nil)),
				must(query.NewPathBinding("p", []query.PathMember{
					must(query.NewNamedNodeMember("a")),
					query.AnonEdgeMember{},
					must(query.NewNamedNodeMember("b")),
				})),
			},
			Returns: []query.ReturnItem{
				{Name: "p", Value: query.NewRefProjection(query.Ref{Variable: "p"}, query.TypePath{})},
			},
		}),
	},
	// Stage 9 — canonical UNWIND of a scalar list. Unwind1 [1] verbatim.
	// The UnwindBinding carries element type TypeInt (the source list is
	// list<int>); the RETURN item's type is TypeInt (refType reads the
	// UnwindBinding's ElementType).
	"unwind scalar list": {
		src: "UNWIND [1, 2, 3] AS x\nRETURN x",
		want: oneBranch(query.Part{
			Bindings: []query.Binding{
				must(query.NewUnwindBinding("x", query.TypeInt{})),
			},
			Returns: []query.ReturnItem{
				{Name: "x", Value: query.NewRefProjection(query.Ref{Variable: "x"}, query.TypeInt{})},
			},
		}),
	},
	// Stage 9 — UNWIND of a range() function. Unwind1 [2] verbatim.
	// range() is a bare function call: FuncProjection with TypeUnknown
	// return type at Stage 6 (function identity below the boundary,
	// ADR 0005). The source expression types as TypeUnknown, so the
	// element type collapses to TypeUnknown — the honest posture the
	// resolver upgrades from the schema.
	"unwind range function": {
		src: "UNWIND range(1, 3) AS x\nRETURN x",
		want: oneBranch(query.Part{
			Bindings: []query.Binding{
				must(query.NewUnwindBinding("x", query.TypeUnknown{})),
			},
			Returns: []query.ReturnItem{
				{Name: "x", Value: query.NewRefProjection(query.Ref{Variable: "x"}, query.TypeUnknown{})},
			},
		}),
	},
	// Stage 9 — UNWIND of an empty list. Unwind1 [8] verbatim. Empty
	// list literal types as list<unknown> at Stage 6 (mixed / empty
	// element unification), so the element type is TypeUnknown. Runtime
	// yields zero rows — a cardinality fact below the boundary.
	"unwind empty list": {
		src: "UNWIND [] AS empty\nRETURN empty",
		want: oneBranch(query.Part{
			Bindings: []query.Binding{
				must(query.NewUnwindBinding("empty", query.TypeUnknown{})),
			},
			Returns: []query.ReturnItem{
				{Name: "empty", Value: query.NewRefProjection(query.Ref{Variable: "empty"}, query.TypeUnknown{})},
			},
		}),
	},
	// Stage 9 — UNWIND of the null literal. Unwind1 [9] verbatim. null is
	// not a list; the element type collapses to TypeUnknown (wrong
	// concrete type would be strictly worse). Runtime yields zero rows.
	"unwind null": {
		src: "UNWIND null AS nil\nRETURN nil",
		want: oneBranch(query.Part{
			Bindings: []query.Binding{
				must(query.NewUnwindBinding("nil", query.TypeUnknown{})),
			},
			Returns: []query.ReturnItem{
				{Name: "nil", Value: query.NewRefProjection(query.Ref{Variable: "nil"}, query.TypeUnknown{})},
			},
		}),
	},
	// Stage 9 — UNWIND of a list of lists (double unwind, Unwind1 [7]'s
	// first UNWIND). WITH exports `lol` as list<list<int>>, so the
	// UnwindBinding for `x` records element type list<int>. Nested
	// element types compose through TypeList.
	"unwind list of lists": {
		src: "WITH [[1, 2, 3], [4, 5, 6]] AS lol\nUNWIND lol AS x\nRETURN x",
		want: query.Query{
			Branches: []query.Branch{{Parts: []query.Part{
				{
					Returns: []query.ReturnItem{
						{Name: "lol", Value: query.NewExprProjection(nil, query.NewTypeList(query.NewTypeList(query.TypeInt{})))},
					},
				},
				{
					Bindings: []query.Binding{
						must(query.NewUnwindBinding("x", query.NewTypeList(query.TypeInt{}))),
					},
					Returns: []query.ReturnItem{
						{Name: "x", Value: query.NewRefProjection(query.Ref{Variable: "x"}, query.NewTypeList(query.TypeInt{}))},
					},
				},
			}}},
		},
	},
	// Stage 9 — RETURN ... ORDER BY $p. Retires the last
	// rejectClauseParameter fail-site (an ORDER BY parameter was
	// rejected under Stages 0–8). Under Stage 9 the parameter is
	// recorded as an ExprUse against a TypeUnknown enclosing type
	// (a sort-key contributor's role does not commit to a computed
	// type; the resolver upgrades from the schema) with
	// ExprInProjection position (ORDER BY sits over a projection
	// column). The RETURN item itself is a bare RefProjection.
	"return order by param": {
		src: "MATCH (n)\nRETURN n\nORDER BY $p",
		want: query.Query{
			Branches: []query.Branch{{Parts: []query.Part{{
				Bindings: []query.Binding{must(query.NewNodeBinding("n", nil))},
				Returns: []query.ReturnItem{
					{Name: "n", Value: query.NewRefProjection(query.Ref{Variable: "n"}, query.TypeNode{})},
				},
			}}}},
			Parameters: []query.Parameter{
				{Name: "p", Uses: []query.Use{
					query.NewExprUse(query.TypeUnknown{}, query.ExprInProjection),
				}},
			},
		},
	},
	// Stage 9 — MATCH-after-UNWIND with a list-of-nodes source: the
	// legitimate reuse path (a WITH collect(n) AS ns yields a
	// list<node>; the subsequent MATCH (m) is a constraint on the
	// already-bound m, not a fresh binding). nameBoundAsUnwind must fire
	// only when the UNWIND element type is node / edge / unknown — a
	// scalar elemType falls through to a byVar collision →
	// ErrVariableKindConflict (see the six mustReject entries below).
	// Stage 10 upgrades the aggregate result: collect(TypeNode) →
	// list<node>, so the UnwindBinding here records elemType TypeNode
	// and the downstream RefProjection on m types as TypeNode — a
	// strict typing improvement end-to-end.
	"unwind of list-of-nodes reused as node match": {
		src: "MATCH (n)\nWITH collect(n) AS ns\nUNWIND ns AS m\nMATCH (m)\nRETURN m",
		want: query.Query{
			Branches: []query.Branch{{Parts: []query.Part{
				{
					Bindings: []query.Binding{must(query.NewNodeBinding("n", nil))},
					Returns: []query.ReturnItem{
						{Name: "ns", Value: query.NewAggregateProjection(query.AggCollect, []query.Ref{{Variable: "n"}}, false, query.NewTypeList(query.TypeNode{}))},
					},
				},
				{
					Bindings: []query.Binding{
						must(query.NewUnwindBinding("m", query.TypeNode{})),
					},
					Returns: []query.ReturnItem{
						{Name: "m", Value: query.NewRefProjection(query.Ref{Variable: "m"}, query.TypeNode{})},
					},
				},
			}}},
		},
	},
	// Stage 10 — DISTINCT enters the model as a scalar axis. count(DISTINCT a)
	// deduplicates its input before counting, so the model preserves the axis;
	// count still types as TypeInt unconditionally. The aggregate-level DISTINCT
	// does NOT flip the enclosing part's Distinct axis (part-distinct-axis
	// spec §1.5): the two grammar sites (oC_FunctionInvocation DISTINCT vs.
	// oC_ProjectionBody DISTINCT) are read independently, so Part.Distinct
	// stays false here (zero-valued, elided).
	"count distinct": {
		src: "OPTIONAL MATCH (a)\nRETURN count(DISTINCT a)",
		want: oneBranch(query.Part{
			Bindings: []query.Binding{
				must(query.NewNullableNodeBindingInGroup("a", nil, 1)),
			},
			Returns: []query.ReturnItem{
				{Name: "count(DISTINCT a)", Value: query.NewAggregateProjection(query.AggCount, []query.Ref{{Variable: "a"}}, true, query.TypeInt{})},
			},
		}),
	},
	// part-distinct-axis spec §1.9 #4 — RETURN DISTINCT n lifts the projection-
	// body DISTINCT keyword onto Part.Distinct. The projection itself is an
	// ordinary whole-entity RefProjection: DISTINCT deduplicates the row set
	// AFTER projection, so the individual return item is unchanged.
	"return distinct": {
		src: "MATCH (n)\nRETURN DISTINCT n",
		want: oneBranch(query.Part{
			Bindings: []query.Binding{must(query.NewNodeBinding("n", nil))},
			Returns: []query.ReturnItem{
				{Name: "n", Value: query.NewRefProjection(query.Ref{Variable: "n"}, query.TypeNode{})},
			},
			Distinct: true,
		}),
	},
	// part-distinct-axis spec §1.9 #5 — WITH DISTINCT flags the intermediate
	// part it terminates, not the following part. Two parts in one branch:
	// part 1 with Distinct=true and one RefProjection over `a`; part 2 with
	// Distinct=false (zero-valued), an edge binding, and a RefProjection
	// over `b`. Independence of siblings' Distinct axes is the point
	// (spec §1.3).
	"with distinct": {
		src: "MATCH (a)\nWITH DISTINCT a\nMATCH (a)-->(b)\nRETURN b",
		want: query.Query{
			Branches: []query.Branch{{Parts: []query.Part{
				{
					Bindings: []query.Binding{must(query.NewNodeBinding("a", nil))},
					Returns: []query.ReturnItem{
						{Name: "a", Value: query.NewRefProjection(query.Ref{Variable: "a"}, query.TypeNode{})},
					},
					Distinct: true,
				},
				{
					Bindings: []query.Binding{
						must(query.NewNodeBinding("a", nil)),
						must(query.NewEdgeBinding("", nil,
							must(query.NewVarEndpoint("a")),
							must(query.NewVarEndpoint("b")),
							true,
						)),
						must(query.NewNodeBinding("b", nil)),
					},
					Returns: []query.ReturnItem{
						{Name: "b", Value: query.NewRefProjection(query.Ref{Variable: "b"}, query.TypeNode{})},
					},
				},
			}}},
		},
	},
	// Stage 10 — collect(TypeNode) → list<node>. The aggregate always yields a
	// list; the element type composes with Stage-6 typing (a bare node ref
	// types as TypeNode).
	"collect node": {
		src: "MATCH (n)\nRETURN collect(n)",
		want: oneBranch(query.Part{
			Bindings: []query.Binding{must(query.NewNodeBinding("n", nil))},
			Returns: []query.ReturnItem{
				{Name: "collect(n)", Value: query.NewAggregateProjection(query.AggCollect, []query.Ref{{Variable: "n"}}, false, query.NewTypeList(query.TypeNode{}))},
			},
		}),
	},
	// Stage 10 — collect over a property lookup: the element is TypeUnknown
	// (property typing is a schema concern per ADR 0003), so the aggregate
	// types as list<unknown> — never bare TypeUnknown at the outer level,
	// per the "collect always yields a list" invariant (spec §1.2).
	"collect property": {
		src: "MATCH (n)\nRETURN collect(n.name)",
		want: oneBranch(query.Part{
			Bindings: []query.Binding{must(query.NewNodeBinding("n", nil))},
			Returns: []query.ReturnItem{
				{Name: "collect(n.name)", Value: query.NewAggregateProjection(query.AggCollect, []query.Ref{{Variable: "n", Property: "name"}}, false, query.NewTypeList(query.TypeUnknown{}))},
			},
		}),
	},
	// Stage 10 — sum over a Stage-6 int-typed operand commits: sum(TypeInt)
	// → TypeInt. UNWIND [1,2,3] AS x binds x with elemType TypeInt (Stage 6
	// list typing + Stage 9 UNWIND element extraction).
	"sum over unwind int": {
		src: "UNWIND [1, 2, 3] AS x\nRETURN sum(x)",
		want: oneBranch(query.Part{
			Bindings: []query.Binding{must(query.NewUnwindBinding("x", query.TypeInt{}))},
			Returns: []query.ReturnItem{
				{Name: "sum(x)", Value: query.NewAggregateProjection(query.AggSum, []query.Ref{{Variable: "x"}}, false, query.TypeInt{})},
			},
		}),
	},
	// Stage 10 — avg stays honest-Unknown for numeric operands (engine-
	// dependent whether it returns int or float; the resolver upgrades from
	// the schema).
	"avg over unwind int": {
		src: "UNWIND [1, 2, 3] AS x\nRETURN avg(x)",
		want: oneBranch(query.Part{
			Bindings: []query.Binding{must(query.NewUnwindBinding("x", query.TypeInt{}))},
			Returns: []query.ReturnItem{
				{Name: "avg(x)", Value: query.NewAggregateProjection(query.AggAvg, []query.Ref{{Variable: "x"}}, false, query.TypeUnknown{})},
			},
		}),
	},
	// Stage 10 — min over a Stage-6 string-typed operand commits to the
	// operand type (min/max are order-preserving; if the operand is a
	// scalar comparable, the aggregate's result IS the operand type).
	"min over unwind string": {
		src: "UNWIND ['a', 'b'] AS x\nRETURN min(x)",
		want: oneBranch(query.Part{
			Bindings: []query.Binding{must(query.NewUnwindBinding("x", query.TypeString{}))},
			Returns: []query.ReturnItem{
				{Name: "min(x)", Value: query.NewAggregateProjection(query.AggMin, []query.Ref{{Variable: "x"}}, false, query.TypeString{})},
			},
		}),
	},
	// Stage 10 — an aggregate inside a rich expression types via the
	// same table: count(n) types as TypeInt, so count(n) + 1 types as
	// TypeInt via promoteAdd(TypeInt, TypeInt). The ExprProjection carries
	// the aggregate's touched ref, the promoted result type, and the
	// ContainsAggregate=true bit (Shape B per ADR 0008 amendment
	// 2026-07-06).
	//
	// aggregate-kind-rich-exprs spec §4.5 pin #7 — closed. The outer
	// expression is not an aggregate call, so per §1.3 the model still
	// does NOT lift the inner count(n) kind as an AggregateProjection.
	// Instead the parser sets ContainsAggregate=true so the resolver's
	// grouping-key discriminator (fillGroupingKeys, R5 §4.5.3) can
	// exclude the residual honestly.
	"count in arithmetic": {
		src: "MATCH (n)\nRETURN count(n) + 1",
		want: oneBranch(query.Part{
			Bindings: []query.Binding{must(query.NewNodeBinding("n", nil))},
			Returns: []query.ReturnItem{
				{Name: "count(n) + 1", Value: query.NewExprProjectionWithAggregate([]query.Ref{{Variable: "n"}}, query.TypeInt{}, true)},
			},
		}),
	},
	// aggregate-kind-rich-exprs spec §4.5 pin #1 — count over an
	// arithmetic property arg. Today's parser falls through to
	// classifyRichExpression (functionArgRefs rejects arithmetic),
	// yielding ExprProjection{[Ref{n,age}], TypeInt}. Under the
	// widening the aggregate arm of classifyFunction fires first,
	// routes through classifyAggregateCall, and lowers as
	// AggregateProjection{AggCount, [Ref{n,age}], false, TypeInt} —
	// the outer aggregate's kind enters the model. TypeInt because
	// aggregateResultType(AggCount, _) = TypeInt unconditionally.
	// Also asserts the Part.Distinct=false zero value alongside
	// AggregateProjection.Distinct=false — structural coverage of
	// the [[cypher-query-parser-part-distinct-axis]] §1.5
	// independence between the two DISTINCT sites.
	"aggregate count on arithmetic property arg": {
		src: "MATCH (n)\nRETURN count(n.age + 1)",
		want: oneBranch(query.Part{
			Bindings: []query.Binding{must(query.NewNodeBinding("n", nil))},
			Returns: []query.ReturnItem{
				{Name: "count(n.age + 1)", Value: query.NewAggregateProjection(
					query.AggCount,
					[]query.Ref{{Variable: "n", Property: "age"}},
					false,
					query.TypeInt{},
				)},
			},
		}),
	},
	// aggregate-kind-rich-exprs spec §4.5 pin #2 — sum over an
	// arithmetic property arg. Today: ExprProjection{[Ref{n,age}],
	// TypeUnknown} (n.age types unknown, promoteAdd(unknown, int) =
	// unknown, aggregateResultType(AggSum, unknown) = unknown).
	// Target: AggregateProjection{AggSum, [Ref{n,age}], false,
	// TypeUnknown} — refs and type preserved bit-for-bit; only the
	// projection kind and the AggregateFunc / Distinct fields change.
	"aggregate sum on arithmetic property arg": {
		src: "MATCH (n)\nRETURN sum(n.age + 1)",
		want: oneBranch(query.Part{
			Bindings: []query.Binding{must(query.NewNodeBinding("n", nil))},
			Returns: []query.ReturnItem{
				{Name: "sum(n.age + 1)", Value: query.NewAggregateProjection(
					query.AggSum,
					[]query.Ref{{Variable: "n", Property: "age"}},
					false,
					query.TypeUnknown{},
				)},
			},
		}),
	},
	// aggregate-kind-rich-exprs spec §4.5 pin #3 — collect over a
	// boolean composite. Two node bindings feed a comparison whose
	// result types TypeBool; aggregateResultType(AggCollect, TypeBool)
	// = list<bool>. The refs list preserves depth-first, left-to-right
	// traversal order: a.name appears before b.name because the left
	// operand of the comparison is walked first (spec §1.4 mining rule,
	// terminology unified across §1.4 and §7).
	"aggregate collect on boolean composite": {
		src: "MATCH (a), (b)\nRETURN collect(a.name = b.name)",
		want: oneBranch(query.Part{
			Bindings: []query.Binding{
				must(query.NewNodeBinding("a", nil)),
				must(query.NewNodeBinding("b", nil)),
			},
			Returns: []query.ReturnItem{
				{Name: "collect(a.name = b.name)", Value: query.NewAggregateProjection(
					query.AggCollect,
					[]query.Ref{
						{Variable: "a", Property: "name"},
						{Variable: "b", Property: "name"},
					},
					false,
					query.NewTypeList(query.TypeBool{}),
				)},
			},
		}),
	},
	// aggregate-kind-rich-exprs spec §4.5 pin #4 — min over a list
	// literal. The list-literal argument types TypeList(TypeInt) via
	// listLiteralType; aggregateResultType(AggMin, TypeList{...}) =
	// TypeUnknown (min over list is engine-inconsistent, Stage 10 §8).
	// No refs mined — the list is scalar literals only. Target:
	// AggregateProjection{AggMin, nil, false, TypeUnknown}.
	"aggregate min on list literal arg": {
		src: "RETURN min([1, 2, 3])",
		want: oneBranch(query.Part{
			Returns: []query.ReturnItem{
				{Name: "min([1, 2, 3])", Value: query.NewAggregateProjection(
					query.AggMin,
					nil,
					false,
					query.TypeUnknown{},
				)},
			},
		}),
	},
	// aggregate-kind-rich-exprs spec §4.5 pin #5 — sum over a nested
	// non-aggregate function call. range(1, 3) types as TypeUnknown
	// (function identity is below the type-interface boundary, ADR
	// 0005 §5); aggregateResultType(AggSum, TypeUnknown) = TypeUnknown.
	// No refs. Target: AggregateProjection{AggSum, nil, false,
	// TypeUnknown}. This pin exercises the "aggregate-with-nested-
	// non-aggregate-call" branch (spec §1.1: every other function-
	// name path — namespaced, non-aggregate bare, or aggregate-with-
	// nested-aggregate — keeps its current behaviour bit-for-bit).
	"aggregate sum on nested function call": {
		src: "RETURN sum(range(1, 3))",
		want: oneBranch(query.Part{
			Returns: []query.ReturnItem{
				{Name: "sum(range(1, 3))", Value: query.NewAggregateProjection(
					query.AggSum,
					nil,
					false,
					query.TypeUnknown{},
				)},
			},
		}),
	},
	// aggregate-kind-rich-exprs spec §4.5 pin #6 — DISTINCT
	// interaction with a rich argument. sum(DISTINCT n.age + 1)
	// reads DISTINCT via fi.DISTINCT() on oC_FunctionInvocation
	// (§1.5) and sets AggregateProjection.Distinct=true; the
	// [[cypher-query-parser-part-distinct-axis]] §1.5 independence
	// invariant holds — Part.Distinct stays zero-valued (no
	// projection-body DISTINCT).
	"aggregate sum distinct on arithmetic arg": {
		src: "MATCH (n)\nRETURN sum(DISTINCT n.age + 1)",
		want: oneBranch(query.Part{
			Bindings: []query.Binding{must(query.NewNodeBinding("n", nil))},
			Returns: []query.ReturnItem{
				{Name: "sum(DISTINCT n.age + 1)", Value: query.NewAggregateProjection(
					query.AggSum,
					[]query.Ref{{Variable: "n", Property: "age"}},
					true,
					query.TypeUnknown{},
				)},
			},
		}),
	},
	// aggregate-kind-rich-exprs spec §4.5 pin #8 — parameter under a
	// rich aggregate argument (Blocker 1). Today: functionArgRefs
	// rejects arithmetic → fall through → classifyRichExpression
	// registers ExprUse{TypeUnknown, ExprInProjection} for $p (the
	// enclosing rich expression sum($p+1) types unknown). Under the
	// widening, classifyAggregateCall routes through
	// typeExpressionMining and registers the same
	// ExprUse{aggregateResultType(AggSum, TypeUnknown)=TypeUnknown,
	// ExprInProjection} — Stage 6 §4 "no parameter is silently
	// dropped" preserved verbatim. The parameter-Uses assertion is
	// stable across the widening; the RED failure is entirely on the
	// projection-kind axis.
	"aggregate sum on arithmetic parameter arg": {
		src: "MATCH (n)\nRETURN sum($p + 1)",
		want: query.Query{
			Branches: []query.Branch{{Parts: []query.Part{{
				Bindings: []query.Binding{must(query.NewNodeBinding("n", nil))},
				Returns: []query.ReturnItem{
					{Name: "sum($p + 1)", Value: query.NewAggregateProjection(
						query.AggSum,
						nil,
						false,
						query.TypeUnknown{},
					)},
				},
			}}}},
			Parameters: []query.Parameter{
				{Name: "p", Uses: []query.Use{
					query.NewExprUse(query.TypeUnknown{}, query.ExprInProjection),
				}},
			},
		},
	},
	// aggregate-kind-rich-exprs spec §4.5 pin #9 — bare parameter as
	// aggregate argument (Blocker 1). Today: functionArgRefs rejects
	// parameter → fall through → classifyRichExpression registers
	// ExprUse{TypeInt, ExprInProjection} for $p (count($p) types int,
	// so the enclosing rich expression is TypeInt). Under the
	// widening, classifyAggregateCall computes
	// resultType=aggregateResultType(AggCount, TypeUnknown)=TypeInt
	// as the enclosingType — same TypeInt. The enclosingType is the
	// aggregate call's result type, NOT the operand's type — critical
	// for count($p) where operand=TypeUnknown but the aggregate result
	// is TypeInt unconditionally (spec §4.1 code shape, Blocker-1
	// posture). Parameter Uses stable across widening; RED failure is
	// on projection-kind only.
	"aggregate count on bare parameter arg": {
		src: "RETURN count($p)",
		want: query.Query{
			Branches: []query.Branch{{Parts: []query.Part{{
				Returns: []query.ReturnItem{
					{Name: "count($p)", Value: query.NewAggregateProjection(
						query.AggCount,
						nil,
						false,
						query.TypeInt{},
					)},
				},
			}}}},
			Parameters: []query.Parameter{
				{Name: "p", Uses: []query.Use{
					query.NewExprUse(query.TypeInt{}, query.ExprInProjection),
				}},
			},
		},
	},
	// aggregate-kind-rich-exprs spec §4.5 pin #10 — bare-arg
	// regression / bit-identity guard (Blocker 3(b)). Today's bare
	// path via functionArgRefs → refFromNonArithmetic (shape.go:29-48)
	// yields Ref{n, age}; post-widening the rich path via
	// typeExpressionMining → typeAtom (typing.go:322-326) +
	// typeNonArithmetic's single-lookup property upgrade
	// (typing.go:292-300) yields the same Ref{n, age}. This pin is
	// GREEN both pre- and post-widening: any drift in refs mining
	// between the two paths surfaces as a structural break here.
	// The two agreeing sites are named above; keep them synchronised.
	"aggregate sum on bare property arg (regression)": {
		src: "MATCH (n)\nRETURN sum(n.age)",
		want: oneBranch(query.Part{
			Bindings: []query.Binding{must(query.NewNodeBinding("n", nil))},
			Returns: []query.ReturnItem{
				{Name: "sum(n.age)", Value: query.NewAggregateProjection(
					query.AggSum,
					[]query.Ref{{Variable: "n", Property: "age"}},
					false,
					query.TypeUnknown{},
				)},
			},
		}),
	},
	// aggregate-kind-rich-exprs spec §4.5 pin #11 — count(count(*))
	// at unit level (Blocker 4). Today: functionArgRefs rejects the
	// count(*) argument (star atom is not var/var.prop and not a
	// scalar literal) → fall through → classifyRichExpression yields
	// ExprProjection{nil, TypeInt} (inner count(*) types TypeInt via
	// typeAtom.COUNT arm, outer aggregateResultType(AggCount, TypeInt)
	// = TypeInt). Target: AggregateProjection{AggCount, nil, false,
	// TypeInt} — the outer count's kind enters the model; the inner
	// count(*) stays a rich sub-expression the model does NOT expose
	// (spec §1.3 asymmetry: outer aggregate visible, inner aggregate
	// invisible). Parser disposition preserved bit-for-bit: the engine
	// still raises NestedAggregation at compile time (bucket 3 per
	// ADR 0007). Godog-corpus scenario [14] Aggregates in aggregates
	// remains skiplist-pending under catGroupingKeySemantic (§4.7).
	"aggregate count of count star": {
		src: "RETURN count(count(*))",
		want: oneBranch(query.Part{
			Returns: []query.ReturnItem{
				{Name: "count(count(*))", Value: query.NewAggregateProjection(
					query.AggCount,
					nil,
					false,
					query.TypeInt{},
				)},
			},
		}),
	},
	// Stage 11 — ALL quantifier at projection position. Quantifier4 [1]:
	// the source list is an empty literal, the filter body's iteration
	// variable never leaks. Every arm types as TypeBool via typeAtom's
	// Stage-11 quantifier arm. The three columns are three separate
	// ExprProjection items (source-list refs are nil, filter refs are
	// discarded per spec §1.1).
	"all quantifier empty list": {
		src: "RETURN all(x IN [] WHERE true) AS a, all(x IN [] WHERE false) AS b, all(x IN [] WHERE x) AS c",
		want: oneBranch(query.Part{
			Returns: []query.ReturnItem{
				{Name: "a", Value: query.NewExprProjection(nil, query.TypeBool{})},
				{Name: "b", Value: query.NewExprProjection(nil, query.TypeBool{})},
				{Name: "c", Value: query.NewExprProjection(nil, query.TypeBool{})},
			},
		}),
	},
	// Stage 11 — NONE quantifier at projection position. Quantifier1 [1]:
	// same shape as ALL — three empty-list quantifiers over a truthy /
	// falsy / iteration-var predicate, each TypeBool.
	"none quantifier empty list": {
		src: "RETURN none(x IN [] WHERE true) AS a, none(x IN [] WHERE false) AS b, none(x IN [] WHERE x) AS c",
		want: oneBranch(query.Part{
			Returns: []query.ReturnItem{
				{Name: "a", Value: query.NewExprProjection(nil, query.TypeBool{})},
				{Name: "b", Value: query.NewExprProjection(nil, query.TypeBool{})},
				{Name: "c", Value: query.NewExprProjection(nil, query.TypeBool{})},
			},
		}),
	},
	// Stage 11 — EXISTS inline-pattern form inside WHERE (ExistentialSubquery1
	// [1]). The subquery's inner NodeBinding (the anonymous target of `(n)-->()`)
	// must NOT leak into the outer part's Bindings: after Stage 11 the outer
	// part has only NodeBinding{n}. The EXISTS itself types as TypeBool inside
	// the WHERE predicate; predicate structure stays below the boundary
	// (ADR 0003), so the RETURN item is a bare RefProjection on n.
	"exists inline pattern in where": {
		src: "MATCH (n) WHERE exists { (n)-->() }\nRETURN n",
		want: oneBranch(query.Part{
			Bindings: []query.Binding{must(query.NewNodeBinding("n", nil))},
			Returns: []query.ReturnItem{
				{Name: "n", Value: query.NewRefProjection(query.Ref{Variable: "n"}, query.TypeNode{})},
			},
		}),
	},
	// Stage 11 — EXISTS subquery form (ExistentialSubquery2 [1]).
	// The inner MATCH inside EXISTS { ... } uses the outer name n
	// (correlated) plus introduces an anonymous target; every inner
	// binding is suppressed by the subqueryDepth counter, so the outer
	// part still binds only n.
	"exists subquery form in where": {
		src: "MATCH (n) WHERE exists { MATCH (n)-->() RETURN true }\nRETURN n",
		want: oneBranch(query.Part{
			Bindings: []query.Binding{must(query.NewNodeBinding("n", nil))},
			Returns: []query.ReturnItem{
				{Name: "n", Value: query.NewRefProjection(query.Ref{Variable: "n"}, query.TypeNode{})},
			},
		}),
	},
	// Stage 11 — nested EXISTS (ExistentialSubquery3 [1] shape). Two
	// levels of subqueryDepth push/pop; every inner binding (m, l, the
	// anonymous target of the inner pattern) is suppressed at every
	// depth. The outer part still binds only n.
	"exists nested": {
		src: "MATCH (n) WHERE exists { MATCH (m) WHERE exists { (n)-[]->(m) WHERE n.prop = m.prop } RETURN true }\nRETURN n",
		want: oneBranch(query.Part{
			Bindings: []query.Binding{must(query.NewNodeBinding("n", nil))},
			Returns: []query.ReturnItem{
				{Name: "n", Value: query.NewRefProjection(query.Ref{Variable: "n"}, query.TypeNode{})},
			},
		}),
	},
	// Stage 11 §1.5 — a $param inside an EXISTS subquery body, paired with an
	// OUTER-scope var.prop (n.age > $threshold where n is bound outside). The
	// outer WHERE is exists {...} — its expression tree contains the inner
	// comparison. mineWhere's mineComparisons walk must NOT descend into the
	// EXISTS subtree (§1.2 boundary) and record a PropertyUse{Ref{n, age}} on
	// $threshold, because the enclosing predicate for the parameter is the
	// EXISTS body's WHERE (boolean), not the outer WHERE. The parameter's Use
	// is the honest ExprUse{TypeBool, ExprInPredicate} minted by
	// EnterOC_ExistentialSubquery's parameter sweep.
	"exists body param paired with outer prop": {
		src: "MATCH (n) WHERE exists { MATCH (m)-->() WHERE n.age > $threshold RETURN true }\nRETURN n",
		want: oneBranch(query.Part{
			Bindings: []query.Binding{must(query.NewNodeBinding("n", nil))},
			Returns: []query.ReturnItem{
				{Name: "n", Value: query.NewRefProjection(query.Ref{Variable: "n"}, query.TypeNode{})},
			},
		}, query.Parameter{Name: "threshold", Uses: []query.Use{
			query.NewExprUse(query.TypeBool{}, query.ExprInPredicate),
		}}),
	},
	// Stage 11 §1.5 — a $param inside an EXISTS body paired with an INNER-scope
	// var.prop (m.age > $threshold where m is bound only inside the EXISTS).
	// Without a boundary guard, mineComparisons would descend and append the
	// INNER varRef `m` onto the outer part's refs list — build()'s referential-
	// integrity sweep would then reject the legal query with ErrUnboundVariable.
	// The correct behaviour: parse OK, outer bindings hold only n, $threshold
	// records the honest ExprUse{TypeBool, ExprInPredicate}.
	"exists body param paired with inner prop": {
		src: "MATCH (n) WHERE exists { (n)-->(m) WHERE m.age > $threshold }\nRETURN n",
		want: oneBranch(query.Part{
			Bindings: []query.Binding{must(query.NewNodeBinding("n", nil))},
			Returns: []query.ReturnItem{
				{Name: "n", Value: query.NewRefProjection(query.Ref{Variable: "n"}, query.TypeNode{})},
			},
		}, query.Parameter{Name: "threshold", Uses: []query.Use{
			query.NewExprUse(query.TypeBool{}, query.ExprInPredicate),
		}}),
	},
	// Stage 11 §1.5 — the WITH-WHERE variant of the same shape: listener.go's
	// EnterOC_With calls mineWhere too, so the boundary guard must cover both
	// entry points. Structurally the same as the MATCH-WHERE case above; the
	// pin catches a regression that fixes one call site but not the other.
	"exists body param via with-where": {
		src: "MATCH (n)\nWITH n WHERE exists { (n)-->(m) WHERE m.age > $threshold }\nRETURN n",
		want: query.Query{Branches: []query.Branch{{Parts: []query.Part{
			{
				Bindings: []query.Binding{must(query.NewNodeBinding("n", nil))},
				Returns: []query.ReturnItem{
					{Name: "n", Value: query.NewRefProjection(query.Ref{Variable: "n"}, query.TypeNode{})},
				},
			},
			{
				Returns: []query.ReturnItem{
					{Name: "n", Value: query.NewRefProjection(query.Ref{Variable: "n"}, query.TypeNode{})},
				},
			},
		}}}, Parameters: []query.Parameter{{Name: "threshold", Uses: []query.Use{
			// EnterOC_ExistentialSubquery fires after EnterOC_With's Part swap
			// (listener.go:293), so emission-time curPart is Part 1 (fvo, ADR
			// 0008 amendment 2026-07-06). Part 1's scope carries n via
			// exportedTypes — resolver-adequate for the EXISTS body's $threshold.
			query.NewExprUseAt(query.TypeBool{}, query.ExprInPredicate, 1, 0),
		}}}},
	},
	// Stage 11 §1.1 — a $param inside a quantifier's filter WHERE body, paired
	// with the ITERATION variable x. typeQuantifier's savedOuter restore rolls
	// back curPart.refs after the rich typer walks the filter body — but a
	// pairAddSub in mineComparisons (called from mineWhere BEFORE typeQuantifier
	// runs) has already leaked `x` onto curPart.refs. The boundary guard on
	// mineComparisons is what stops that leak: parse OK, outer refs contain n
	// only (x never appears), and $needle records ExprUse{TypeBool,
	// ExprInPredicate} once via typeQuantifier's own parameter mining.
	"quantifier body param paired with iteration var": {
		src: "MATCH (n) WHERE any(x IN n.tags WHERE x = $needle)\nRETURN n",
		want: oneBranch(query.Part{
			Bindings: []query.Binding{must(query.NewNodeBinding("n", nil))},
			Returns: []query.ReturnItem{
				{Name: "n", Value: query.NewRefProjection(query.Ref{Variable: "n"}, query.TypeNode{})},
			},
		}, query.Parameter{Name: "needle", Uses: []query.Use{
			query.NewExprUse(query.TypeBool{}, query.ExprInPredicate),
		}}),
	},
	// A $param occupying LIMIT's clause-slot inside an EXISTS { RegularQuery }
	// body. Under the Stage 11 §1.2 subqueryDepth suppression,
	// collectProjection (the only outer path that reaches
	// mineClauseSlotParameter) does not run, so
	// EnterOC_ExistentialSubquery's own clause-slot pass has to mint the
	// precise ClauseSlotUse{ClauseSlotLimit} — the clause slot is a syntactic
	// property of the enclosing OC_Skip/OC_Limit node, so a $lim in a nested
	// LIMIT gets the same Use variant as an outer LIMIT.
	"exists body limit clause-slot param": {
		src: "MATCH (n) WHERE exists { MATCH (m) RETURN m LIMIT $lim }\nRETURN n",
		want: oneBranch(query.Part{
			Bindings: []query.Binding{must(query.NewNodeBinding("n", nil))},
			Returns: []query.ReturnItem{
				{Name: "n", Value: query.NewRefProjection(query.Ref{Variable: "n"}, query.TypeNode{})},
			},
		}, query.Parameter{Name: "lim", Uses: []query.Use{
			query.NewClauseSlotUse(query.ClauseSlotLimit),
		}}),
	},
	// SKIP twin of the LIMIT case above. The two slots share one code path
	// in mineClauseSlotParameter, so the pair asserts the axis is honoured,
	// not the token.
	"exists body skip clause-slot param": {
		src: "MATCH (n) WHERE exists { MATCH (m) RETURN m SKIP $off }\nRETURN n",
		want: oneBranch(query.Part{
			Bindings: []query.Binding{must(query.NewNodeBinding("n", nil))},
			Returns: []query.ReturnItem{
				{Name: "n", Value: query.NewRefProjection(query.Ref{Variable: "n"}, query.TypeNode{})},
			},
		}, query.Parameter{Name: "off", Uses: []query.Use{
			query.NewClauseSlotUse(query.ClauseSlotSkip),
		}}),
	},
	// Mixed — both LIMIT and SKIP slots in one EXISTS body, plus a residual
	// WHERE parameter that still lands on the blanket sweep as
	// ExprUse{TypeBool, ExprInPredicate}. Pins that slot-classification and
	// blanket-fallback coexist on the same subquery walk — one $param per
	// slot, one per residual, no double-recording. Emission order: the
	// slot-classification passes run before the blanket findParameters sweep,
	// so $off + $lim precede $threshold in the query-wide Parameters slice.
	"exists body mixed limit skip and where params": {
		src: "MATCH (n) WHERE exists { MATCH (m) WHERE m.age > $threshold RETURN m SKIP $off LIMIT $lim }\nRETURN n",
		want: oneBranch(query.Part{
			Bindings: []query.Binding{must(query.NewNodeBinding("n", nil))},
			Returns: []query.ReturnItem{
				{Name: "n", Value: query.NewRefProjection(query.Ref{Variable: "n"}, query.TypeNode{})},
			},
		},
			query.Parameter{Name: "off", Uses: []query.Use{
				query.NewClauseSlotUse(query.ClauseSlotSkip),
			}},
			query.Parameter{Name: "lim", Uses: []query.Use{
				query.NewClauseSlotUse(query.ClauseSlotLimit),
			}},
			query.Parameter{Name: "threshold", Uses: []query.Use{
				query.NewExprUse(query.TypeBool{}, query.ExprInPredicate),
			}},
		),
	},
	// collection-sink Phase C pin — an inner WITH inside an EXISTS body
	// projects a bare variable m (bound only in the inner MATCH). Without the
	// classifyProjection→appendRef migration, the inner WITH's suppressed
	// EnterOC_With would still reach classifyProjection's bare-var branch and
	// leak varRef{m} onto the OUTER part's refs slice, which build()'s
	// referential-integrity sweep would then reject with ErrUnboundVariable
	// (m is bound inside the subquery, not on the outer part). The outer part
	// binds only n. Corpus was blind to this class before the sink migration
	// because every prior WITH-in-EXISTS shape carried n (outer scope) rather
	// than an inner-only binding — build() didn't reject, but the outer refs
	// slice silently grew. This pin turns that latent leak golden-visible.
	"exists body with-item bare inner var no outer refs leak": {
		src: "MATCH (n) WHERE exists { MATCH (m) WITH m RETURN m }\nRETURN n",
		want: oneBranch(query.Part{
			Bindings: []query.Binding{must(query.NewNodeBinding("n", nil))},
			Returns: []query.ReturnItem{
				{Name: "n", Value: query.NewRefProjection(query.Ref{Variable: "n"}, query.TypeNode{})},
			},
		}),
	},
	// collection-sink Phase C pin — twin of the bare-var case, exercising the
	// classifyFunction arg-refs loop instead: id(m) inside a WITH body. Without
	// the classifyFunction→appendRef migration, the suppressed EnterOC_With
	// would reach classifyFunction's functionArgRefs loop and leak varRef{m}
	// onto the outer part's refs slice via the append site — same
	// ErrUnboundVariable rejection as the bare-var case. Two pins (one per
	// migrated append site) so a future partial regression can't hide behind
	// the other's coverage.
	"exists body with-item function arg no outer refs leak": {
		src: "MATCH (n) WHERE exists { MATCH (m) WITH id(m) AS mid RETURN mid }\nRETURN n",
		want: oneBranch(query.Part{
			Bindings: []query.Binding{must(query.NewNodeBinding("n", nil))},
			Returns: []query.ReturnItem{
				{Name: "n", Value: query.NewRefProjection(query.Ref{Variable: "n"}, query.TypeNode{})},
			},
		}),
	},
	// collection-sink Phase C pin — entry-point twin of the two WITH pins
	// above, exercising Return's own reach chain. RETURN m inside an EXISTS
	// body binds inner m via classifyProjection's bare-var arm — the exact
	// same code path as the WITH bare-var pin, but reached through
	// EnterOC_Return post-guard-drop instead of EnterOC_With. Because the
	// 240 and 356 refs sites are already sink-routed under 6f2b2c1, this
	// guard-drop is safe by pre-clearance; without those sinks (or if this
	// commit were re-ordered before them), varRef{m} would leak onto the
	// outer part's refs slice and build()'s referential-integrity sweep
	// would reject with ErrUnboundVariable (outer part binds only n). One
	// pin at the entry point is sufficient coverage for both the bare-var
	// and function-arg site classes — those are proven independently by the
	// two WITH pins above; this pin proves the Return entry is safe.
	"exists body return-item bare inner var no outer refs leak": {
		src: "MATCH (n) WHERE exists { MATCH (m) RETURN m }\nRETURN n",
		want: oneBranch(query.Part{
			Bindings: []query.Binding{must(query.NewNodeBinding("n", nil))},
			Returns: []query.ReturnItem{
				{Name: "n", Value: query.NewRefProjection(query.Ref{Variable: "n"}, query.TypeNode{})},
			},
		}),
	},
	// collection-sink Phase C pin — Unwind entry-point twin. UNWIND [m] AS k
	// inside an EXISTS body reaches TWO Category A/B writes in collectUnwind
	// via post-guard-drop EnterOC_Unwind: (1) the source-expression refs sweep
	// mines varRef{m} from the [m] list literal (Category A, expr.go:124), and
	// (2) the UnwindBinding{k} construction appends to the part's unwind
	// bindings (Category B, expr.go:139). Both writes now route through the
	// appendRef / appendUnwindBinding sinks; without either routing, this pin
	// would fail — an un-sunk refs write leaks varRef{m} onto the outer part
	// (which only binds n), and build()'s referential-integrity sweep would
	// reject with ErrUnboundVariable: m; an un-sunk unwindBindings write
	// leaks UnwindBinding{k} onto the outer part's Bindings, and require.Equal
	// would reject the Bindings-slice shape. Single pin, both leak classes.
	// Return here binds only k inside the subquery — the k pin does not exit
	// EXISTS (commit-5 already proved the Return sink). Outer part binds only
	// n; a successful parse with Bindings=[n] proves suppression by
	// construction across both sink routings.
	"exists body unwind refs and binding no outer leak": {
		src: "MATCH (n) WHERE exists { MATCH (m) UNWIND [m] AS k RETURN k }\nRETURN n",
		want: oneBranch(query.Part{
			Bindings: []query.Binding{must(query.NewNodeBinding("n", nil))},
			Returns: []query.ReturnItem{
				{Name: "n", Value: query.NewRefProjection(query.Ref{Variable: "n"}, query.TypeNode{})},
			},
		}),
	},
	// collection-sink Phase C pin — Set entry-point twin. A single SET clause
	// with three items (m.p = 1, m:L, m += {x: 2}) inside an EXISTS body
	// exercises ALL THREE arms of collectSetItem in one construction:
	// propertyExpression (:750 refs + :764 effects), variable+labels (:768
	// refs + :775 effects), and variable+expression (:779 refs + :794
	// effects). Post-guard-drop, each arm's refs write is routed through
	// appendRef and each arm's effects write through appendEffect; the
	// terminating writeSeen flag flip in EnterOC_Set is routed through
	// markWriteSeen. Under EXISTS suppression, all seven routings no-op,
	// so no SetEffect leaks into outer Effects, no varRef{m} leaks onto
	// outer refs, and StatementKind stays StatementRead. Without ANY of the
	// three sink routings, this pin fails: an un-sunk effects write leaks
	// three SetEffects into outer Effects (require.Equal on Effects shape);
	// an un-sunk refs write leaks varRef{m} onto outer refs (build's
	// referential-integrity sweep rejects with ErrUnboundVariable: m —
	// outer part binds only n); an un-sunk markWriteSeen flips outer
	// StatementKind from StatementRead to StatementWrite (require.Equal on
	// query-level StatementKind). One pin, seven leak-lines, three sinks.
	// The three arms are structurally identical (all follow refs-then-eff-
	// then-append pattern), so one pin covering all three via multi-item
	// SET is sufficient coverage — the per-sink temp-revert probe proves
	// each is independently load-bearing.
	"exists body set three-arm no outer leak": {
		src: "MATCH (n) WHERE exists { MATCH (m) SET m.p = 1, m:L, m += {x: 2} RETURN m }\nRETURN n",
		want: oneBranch(query.Part{
			Bindings: []query.Binding{must(query.NewNodeBinding("n", nil))},
			Returns: []query.ReturnItem{
				{Name: "n", Value: query.NewRefProjection(query.Ref{Variable: "n"}, query.TypeNode{})},
			},
		}),
	},
	// collection-sink Phase C pin — Delete entry-point twin. A single DELETE
	// clause with two bare-variable targets (m, o) inside an EXISTS body
	// exercises all three sink classes reachable from EnterOC_Delete in one
	// construction: the per-target refs write at listener.go:655 fires twice
	// (once per bare-variable target that hits the nonArithmeticAtom +
	// refFromNonArithmetic path), the terminating effects write at :669
	// fires once (one DeleteEffect{targets=[m,o], detach=true}), and the
	// terminating writeSeen flip at :670 fires once. DETACH is included to
	// exercise the detach flag naturally alongside — it toggles a bool on
	// the effect payload, not a sink surface, but its presence keeps the
	// pin realistic (DELETE without DETACH on referenced nodes is an
	// arity/edge error at runtime). Post-guard-drop, refs writes route
	// through appendRef, effects through appendEffect, writeSeen through
	// markWriteSeen. Under EXISTS suppression, all four routings no-op:
	// no DeleteEffect leaks into outer Effects, no varRef{m}/varRef{o}
	// leaks onto outer refs, and StatementKind stays StatementRead.
	// Without ANY of the three sink routings, this pin fails: an un-sunk
	// effects write leaks one DeleteEffect into outer Effects (require.Equal
	// on Effects shape); an un-sunk refs write leaks varRef{m} and varRef{o}
	// onto outer refs (build's referential-integrity sweep rejects with
	// ErrUnboundVariable — outer part binds only n); an un-sunk
	// markWriteSeen flips outer StatementKind from StatementRead to
	// StatementWrite (require.Equal on query-level StatementKind). One
	// pin, four leak-lines, three sinks. Multi-target DELETE covers the
	// loop's inner refs sink twice, so a single pin is sufficient — the
	// per-sink temp-revert probe proves each is independently load-bearing.
	"exists body delete detach two-target no outer leak": {
		src: "MATCH (n) WHERE exists { MATCH (m)-[]->(o) DETACH DELETE m, o RETURN true }\nRETURN n",
		want: oneBranch(query.Part{
			Bindings: []query.Binding{must(query.NewNodeBinding("n", nil))},
			Returns: []query.ReturnItem{
				{Name: "n", Value: query.NewRefProjection(query.Ref{Variable: "n"}, query.TypeNode{})},
			},
		}),
	},
	// Nested — SKIP $off in the OUTER RegularQuery-form EXISTS, LIMIT $lim in
	// an inner EXISTS one level deeper. EnterOC_ExistentialSubquery fires on
	// the outer subquery first (parent EnterRule precedes child EnterRule);
	// its clause-slot classification pass walks the whole subtree, finding
	// both the outer OC_Skip and the inner OC_Limit, classifying each against
	// its own precise slot. The inner subquery's own
	// EnterOC_ExistentialSubquery still fires when the walker descends, but
	// the approved-tree guard in addParameterUse (and in
	// mineClauseSlotParameter) makes both sweeps idempotent — neither
	// parameter double-records. Emission order: the SKIP pass runs before the
	// LIMIT pass in the outer sweep, so $off precedes $lim in the query-wide
	// Parameters slice.
	"exists nested with limit skip clause-slot params": {
		src: "MATCH (n) WHERE exists { MATCH (m) WHERE exists { MATCH (l) RETURN l LIMIT $lim } RETURN m SKIP $off }\nRETURN n",
		want: oneBranch(query.Part{
			Bindings: []query.Binding{must(query.NewNodeBinding("n", nil))},
			Returns: []query.ReturnItem{
				{Name: "n", Value: query.NewRefProjection(query.Ref{Variable: "n"}, query.TypeNode{})},
			},
		},
			query.Parameter{Name: "off", Uses: []query.Use{
				query.NewClauseSlotUse(query.ClauseSlotSkip),
			}},
			query.Parameter{Name: "lim", Uses: []query.Use{
				query.NewClauseSlotUse(query.ClauseSlotLimit),
			}},
		),
	},
	// Stage 12 — CREATE, DELETE, SET, REMOVE (write clauses).

	// Create1 [1]: CREATE () — the minimum CREATE. An anonymous node is not a
	// binding (C3), so bindings is empty; the CreateEffect records the empty
	// variables slice (no named creation to track). StatementKind flips to
	// StatementWrite. Projection-less shape: no Returns, no ReturnsAll.
	"create anonymous node": {
		src: "CREATE ()",
		want: oneWriteBranch(query.Part{
			Effects: []query.Effect{query.NewCreateEffect(nil)},
		}),
	},

	// Create2 [?] equivalent: CREATE (n:Label). The CreateEffect records the
	// named binding, and the NodeBinding enters Part.Bindings verbatim (as if
	// MATCH had bound it). Projection-less.
	"create named labelled node": {
		src: "CREATE (n:Label)",
		want: oneWriteBranch(query.Part{
			Bindings: []query.Binding{
				must(query.NewNodeBinding("n", graph.LabelSet{"Label"})),
			},
			Effects: []query.Effect{query.NewCreateEffect([]string{"n"})},
		}),
	},

	// Create + RETURN: CREATE (n) RETURN n. Still StatementWrite (a write
	// followed by a read-back is a write for tx-mode purposes). The
	// CreateEffect and the RefProjection coexist in the same part.
	"create then return": {
		src: "CREATE (n)\nRETURN n",
		want: oneWriteBranch(query.Part{
			Bindings: []query.Binding{must(query.NewNodeBinding("n", nil))},
			Returns: []query.ReturnItem{
				{Name: "n", Value: query.NewRefProjection(query.Ref{Variable: "n"}, query.TypeNode{})},
			},
			Effects: []query.Effect{query.NewCreateEffect([]string{"n"})},
		}),
	},

	// Delete1 [1]: MATCH (n) DELETE n — the DeleteEffect targets the MATCH-
	// bound n; Detach is false. Projection-less write.
	"delete node": {
		src: "MATCH (n)\nDELETE n",
		want: oneWriteBranch(query.Part{
			Bindings: []query.Binding{must(query.NewNodeBinding("n", nil))},
			Effects: []query.Effect{
				query.NewDeleteEffect([]query.Ref{{Variable: "n"}}, nil, false),
			},
		}),
	},

	// Delete1 [2]: MATCH (n) DETACH DELETE n — same shape with Detach=true.
	"detach delete node": {
		src: "MATCH (n)\nDETACH DELETE n",
		want: oneWriteBranch(query.Part{
			Bindings: []query.Binding{must(query.NewNodeBinding("n", nil))},
			Effects: []query.Effect{
				query.NewDeleteEffect([]query.Ref{{Variable: "n"}}, nil, true),
			},
		}),
	},

	// Set1 [1] shape: MATCH (n:A) SET n.name = 'Michael' RETURN n. The
	// SetPropertyEffect records target Ref{n, name} and the value's Stage-6
	// type (TypeString from a scalar literal).
	"set property to literal": {
		src: "MATCH (n:A)\nSET n.name = 'Michael'\nRETURN n",
		want: oneWriteBranch(query.Part{
			Bindings: []query.Binding{
				must(query.NewNodeBinding("n", graph.LabelSet{"A"})),
			},
			Returns: []query.ReturnItem{
				{Name: "n", Value: query.NewRefProjection(query.Ref{Variable: "n"}, query.TypeNode{})},
			},
			Effects: []query.Effect{
				must(query.NewSetPropertyEffect(query.Ref{Variable: "n", Property: "name"}, query.TypeString{}, nil)),
			},
		}),
	},

	// Set4 shape: MATCH (n) SET n = {name: 'Andres'} RETURN n. The
	// SetEntityEffect uses SetOpReplace; the RHS types as TypeMap.
	"set entity replace with map literal": {
		src: "MATCH (n)\nSET n = {name: 'Andres'}\nRETURN n",
		want: oneWriteBranch(query.Part{
			Bindings: []query.Binding{must(query.NewNodeBinding("n", nil))},
			Returns: []query.ReturnItem{
				{Name: "n", Value: query.NewRefProjection(query.Ref{Variable: "n"}, query.TypeNode{})},
			},
			Effects: []query.Effect{
				must(query.NewSetEntityEffect("n", query.SetOpReplace, query.TypeMap{}, nil)),
			},
		}),
	},

	// Set5 shape: MATCH (n) SET n:Foo RETURN n. SetLabelsEffect carries the
	// variable and labels; no value expression.
	"set labels": {
		src: "MATCH (n)\nSET n:Foo\nRETURN n",
		want: oneWriteBranch(query.Part{
			Bindings: []query.Binding{must(query.NewNodeBinding("n", nil))},
			Returns: []query.ReturnItem{
				{Name: "n", Value: query.NewRefProjection(query.Ref{Variable: "n"}, query.TypeNode{})},
			},
			Effects: []query.Effect{
				must(query.NewSetLabelsEffect("n", graph.LabelSet{"Foo"})),
			},
		}),
	},

	// Remove1 [1] shape: MATCH (n) REMOVE n.num. Projection-less write; the
	// RemovePropertyEffect carries Ref{n, num}.
	"remove property": {
		src: "MATCH (n)\nREMOVE n.num",
		want: oneWriteBranch(query.Part{
			Bindings: []query.Binding{must(query.NewNodeBinding("n", nil))},
			Effects: []query.Effect{
				must(query.NewRemovePropertyEffect(query.Ref{Variable: "n", Property: "num"})),
			},
		}),
	},

	// Remove2 [?] shape: MATCH (n) REMOVE n:L. RemoveLabelsEffect analogous
	// to SetLabelsEffect.
	"remove labels": {
		src: "MATCH (n)\nREMOVE n:L",
		want: oneWriteBranch(query.Part{
			Bindings: []query.Binding{must(query.NewNodeBinding("n", nil))},
			Effects: []query.Effect{
				must(query.NewRemoveLabelsEffect("n", graph.LabelSet{"L"})),
			},
		}),
	},

	// AUTHORED (Stage 12 §1.10): CREATE with an inline-map $param. The
	// pinned-tag TCK does not exercise a $param inside a CREATE inline map
	// (grep confirmed zero); this shape pins the typed-Create story — the
	// inline-map miner records PropertyUse{Ref{p, name}} against $name, so
	// the resolver upgrades $name's type from Person.name via the schema.
	// Replaces with a verbatim corpus entry if a future TCK
	// bump supplies one.
	"create with inline-map param": {
		src: "CREATE (p:Person {name: $name})",
		want: oneWriteBranch(query.Part{
			Bindings: []query.Binding{
				must(query.NewNodeBinding("p", graph.LabelSet{"Person"})),
			},
			Effects: []query.Effect{query.NewCreateEffect([]string{"p"})},
		}, query.Parameter{Name: "name", Uses: []query.Use{
			query.NewPropertyUse(query.Ref{Variable: "p", Property: "name"}),
		}}),
	},

	// AUTHORED (Stage 12 §1.10): SET property with a bare $param on the RHS.
	// The pinned-tag TCK does not exercise a $param inside a SET value
	// expression (grep confirmed zero). The bare $param has no enclosing
	// arithmetic that would pin a concrete type, so the value expression
	// types as TypeUnknown at the parser boundary — an honest posture the
	// resolver upgrades from n.age via the schema. The Use
	// records ExprUse{TypeUnknown, ExprInSetValue} — the SET value is a
	// producer position (value written to the graph), semantically opposite
	// to a projection column's consumer role, so the position discriminator
	// stays honest across the write set. Replaces with a verbatim corpus
	// entry if a future TCK bump supplies one.
	"set property with bare param": {
		src: "MATCH (n)\nSET n.age = $newAge\nRETURN n",
		want: oneWriteBranch(query.Part{
			Bindings: []query.Binding{must(query.NewNodeBinding("n", nil))},
			Returns: []query.ReturnItem{
				{Name: "n", Value: query.NewRefProjection(query.Ref{Variable: "n"}, query.TypeNode{})},
			},
			Effects: []query.Effect{
				must(query.NewSetPropertyEffect(query.Ref{Variable: "n", Property: "age"}, query.TypeUnknown{}, nil)),
			},
		}, query.Parameter{Name: "newAge", Uses: []query.Use{
			query.NewExprUse(query.TypeUnknown{}, query.ExprInSetValue),
		}}),
	},

	// AUTHORED (Stage 12 amend §4.2): DELETE with a rich-shape target
	// (a function invocation, DELETE nodes($p)). The pinned-tag TCK does
	// not exercise a $param inside a DELETE rich expression (grep
	// confirmed zero). The DeleteEffect carries no resolved Target (the
	// rich shape has no honest single-Ref view; the value's entity kind
	// is a resolver-time lookup below the parser boundary per ADR 0005).
	// The $p records ExprUse{TypeUnknown, ExprInDeleteTarget} — the
	// DELETE target is a consumer position whose runtime entity kind
	// determines whether the delete is legal, semantically distinct from
	// a SET value's producer role and from a projection column's return-
	// side role. Function-invocation shape chosen over list-index
	// (n.friends[$idx]) because the current typer mines params inside
	// function args but not inside list-operator suffixes; the shape
	// robustly exercises the ExprInDeleteTarget wiring at the pinned tag.
	"delete rich expression with param": {
		src: "MATCH (n)\nDELETE nodes($p)",
		want: oneWriteBranch(query.Part{
			Bindings: []query.Binding{must(query.NewNodeBinding("n", nil))},
			Effects: []query.Effect{
				query.NewDeleteEffect(nil, nil, false),
			},
		}, query.Parameter{Name: "p", Uses: []query.Use{
			query.NewExprUse(query.TypeUnknown{}, query.ExprInDeleteTarget),
		}}),
	},

	// Stage 13 — MERGE. Seven verbatim corpus shapes plus three authored
	// pins (StatementKind non-flip inside EXISTS, branch-leak kill-probe,
	// CREATE-side var-PROP inline-map widening).

	// Merge1 [1] Merge node when no nodes exist: MERGE (a) — the minimum
	// MERGE. One NodeBinding for the named pattern element, one MergeEffect
	// with Variables ["a"] and empty ON branches, projection-less write.
	"merge bare named node": {
		src: "MERGE (a)",
		want: oneWriteBranch(query.Part{
			Bindings: []query.Binding{must(query.NewNodeBinding("a", nil))},
			Effects: []query.Effect{
				must(query.NewMergeEffect([]string{"a"}, nil, nil)),
			},
		}),
	},

	// Merge1 [3] Merge node with label when it exists: MERGE (a:TheLabel)
	// RETURN a.id — a labelled MERGE feeding a property projection. Still
	// StatementWrite (a write followed by a read-back is a write for
	// tx-mode purposes).
	"merge labelled node returning property": {
		src: "MERGE (a:TheLabel)\nRETURN a.id",
		want: oneWriteBranch(query.Part{
			Bindings: []query.Binding{
				must(query.NewNodeBinding("a", graph.LabelSet{"TheLabel"})),
			},
			Returns: []query.ReturnItem{
				{Name: "a.id", Value: query.NewRefProjection(query.Ref{Variable: "a", Property: "id"}, query.TypeUnknown{})},
			},
			Effects: []query.Effect{
				must(query.NewMergeEffect([]string{"a"}, nil, nil)),
			},
		}),
	},

	// Merge1 [11] Merge should be able to merge using property of bound node:
	// MATCH (person:Person) MERGE (city:City {name: person.bornIn}). Pins
	// the mineInlineMap value-side widening — the MERGE part's refs list
	// carries `person` (from person.bornIn), and MergeEffect.Variables is
	// ["city"] (only the newly-introduced binding). Projection-less write.
	"merge with inline map referencing bound var": {
		src: "MATCH (person:Person)\nMERGE (city:City {name: person.bornIn})",
		want: oneWriteBranch(query.Part{
			Bindings: []query.Binding{
				must(query.NewNodeBinding("person", graph.LabelSet{"Person"})),
				must(query.NewNodeBinding("city", graph.LabelSet{"City"})),
			},
			Effects: []query.Effect{
				must(query.NewMergeEffect([]string{"city"}, nil, nil)),
			},
		}),
	},

	// Merge3 [1] Merge should be able to set labels on match: MERGE (a) ON
	// MATCH SET a:L. One MergeEffect with an OnMatch slice of one
	// SetLabelsEffect{"a", ["L"]} and empty OnCreate.
	"merge with on match set labels": {
		src: "MERGE (a)\n  ON MATCH SET a:L",
		want: oneWriteBranch(query.Part{
			Bindings: []query.Binding{must(query.NewNodeBinding("a", nil))},
			Effects: []query.Effect{
				must(query.NewMergeEffect(
					[]string{"a"},
					[]query.SetEffect{must(query.NewSetLabelsEffect("a", graph.LabelSet{"L"}))},
					nil,
				)),
			},
		}),
	},

	// Merge2 [2] ON CREATE on created nodes: MERGE (b) ON CREATE SET
	// b.created = 1. Pins the ON CREATE path with a SetPropertyEffect
	// payload — one MergeEffect with Variables ["b"], empty OnMatch, and
	// OnCreate carrying a SetPropertyEffect{Ref{b, created}, TypeInt}.
	// Projection-less write.
	"merge with on create set property": {
		src: "MERGE (b)\n  ON CREATE SET b.created = 1",
		want: oneWriteBranch(query.Part{
			Bindings: []query.Binding{must(query.NewNodeBinding("b", nil))},
			Effects: []query.Effect{
				must(query.NewMergeEffect(
					[]string{"b"},
					nil,
					[]query.SetEffect{must(query.NewSetPropertyEffect(query.Ref{Variable: "b", Property: "created"}, query.TypeInt{}, nil))},
				)),
			},
		}),
	},

	// Merge4 [1] Merge should be able to set labels on match and on create:
	// MATCH () MERGE (a:L) ON MATCH SET a:M1 ON CREATE SET a:M2. Both ON
	// branches populated with distinct SetEffect payloads. MATCH () introduces
	// no binding (anonymous nodes are pure filters on the read side, C3); the
	// MERGE introduces `a` as a NodeBinding, and the MergeEffect carries
	// Variables ["a"] with OnMatch and OnCreate each carrying one
	// SetLabelsEffect.
	"merge with both on branches": {
		src: "MATCH ()\nMERGE (a:L)\n  ON MATCH SET a:M1\n  ON CREATE SET a:M2",
		want: oneWriteBranch(query.Part{
			Bindings: []query.Binding{
				must(query.NewNodeBinding("a", graph.LabelSet{"L"})),
			},
			Effects: []query.Effect{
				must(query.NewMergeEffect(
					[]string{"a"},
					[]query.SetEffect{must(query.NewSetLabelsEffect("a", graph.LabelSet{"M1"}))},
					[]query.SetEffect{must(query.NewSetLabelsEffect("a", graph.LabelSet{"M2"}))},
				)),
			},
		}),
	},

	// Merge1 [8] Merge should handle argument properly: WITH 42 AS var
	// MERGE (c:N {var: var}). Two-part query: the WITH exports `var`, the
	// second part MERGES on a pattern using `var` as an inline-map value.
	// Pins the inline-map value-side widening on the MERGE side for a
	// bare-variable value — the MERGE part's refs list carries `var`.
	"merge argument handling across with": {
		src: "WITH 42 AS var\nMERGE (c:N {var: var})",
		want: func() query.Query {
			q := query.Query{
				Branches: []query.Branch{{
					Parts: []query.Part{
						{
							Returns: []query.ReturnItem{
								{Name: "var", Value: query.NewLiteralProjection(query.TypeInt{})},
							},
						},
						{
							Bindings: []query.Binding{
								must(query.NewNodeBinding("c", graph.LabelSet{"N"})),
							},
							Effects: []query.Effect{
								must(query.NewMergeEffect([]string{"c"}, nil, nil)),
							},
						},
					},
				}},
			}
			q.StatementKind = query.StatementWrite
			return q
		}(),
	},

	// AUTHORED (Stage 13 §1.8): MERGE inside EXISTS does not flip
	// StatementKind. The outer EnterOC_Merge early-returns under
	// subqueryDepth > 0 before markWrite/writeSeen fires, so a query with a
	// MERGE only inside an EXISTS { ... } subquery stays StatementRead.
	// No MergeEffect appears anywhere in the model (Stage 11 §1.6: the
	// inner subquery body is not walked for collection). Pins the
	// walk-time semantic explicitly for the newest write shape.
	"authored merge in exists does not flip statement kind": {
		src: "MATCH (n)\nWHERE exists { MERGE (m)\nRETURN true }\nRETURN n",
		want: oneBranch(query.Part{
			Bindings: []query.Binding{must(query.NewNodeBinding("n", nil))},
			Returns: []query.ReturnItem{
				{Name: "n", Value: query.NewRefProjection(query.Ref{Variable: "n"}, query.TypeNode{})},
			},
		}),
	},

	// AUTHORED (Stage 13 §1.8): branch-leak kill-probe for the two-level
	// effects slot. MERGE (n) ON CREATE SET n.a = 1 SET n.b = 2 — the first
	// SET is inside ON CREATE's inner scope, the second SET is a top-level
	// SET clause on the outer part. Pins the save/restore around
	// curPart.effects in collectMergeAction (§4.2): the ON CREATE
	// SetPropertyEffect{Ref{n, a}, TypeInt} lives inside
	// MergeEffect.OnCreate; the outer SetPropertyEffect{Ref{n, b}, TypeInt}
	// is a peer of the MergeEffect on Part.Effects. A missing save/restore
	// would either leak the ON CREATE effect to the outer part (double-
	// recording n.a) or capture the outer SET into OnCreate (silently
	// dropping the top-level SET into the branch). Merge4[1] pins the
	// branch-populated shape but not this adjacency; corpus has no such
	// shape at the pinned tag.
	"authored merge branch-leak kill probe": {
		src: "MERGE (n)\n  ON CREATE SET n.a = 1\nSET n.b = 2",
		want: oneWriteBranch(query.Part{
			Bindings: []query.Binding{must(query.NewNodeBinding("n", nil))},
			Effects: []query.Effect{
				must(query.NewMergeEffect(
					[]string{"n"},
					nil,
					[]query.SetEffect{must(query.NewSetPropertyEffect(query.Ref{Variable: "n", Property: "a"}, query.TypeInt{}, nil))},
				)),
				must(query.NewSetPropertyEffect(query.Ref{Variable: "n", Property: "b"}, query.TypeInt{}, nil)),
			},
		}),
	},

	// AUTHORED (Stage 13 §1.8, ruling Q3(b) bound-guard): CREATE-side
	// var-PROP inline-map bound-ref guard. MATCH (b) CREATE (a {name: b.c})
	// RETURN a — `b` is bound from the preceding MATCH, so the widened refs
	// list on the CREATE part carries `b` (from the value expression b.c via
	// typeExpressionMining on the inline-map value); CreateEffect Variables
	// ["a"], one RefProjection for a. This pin PASSES in RED and must keep
	// passing after GREEN — it is a regression lock against over-rejection
	// by the widening (a future stage that splits mineInlineMap into
	// per-clause helpers and accidentally rejects bound refs would fail
	// here). The unbound-ref kill-probe on the other side of the pair lives
	// in mustReject as `authored create unbound inline map ref kill probe`.
	// Create6 exercises the shared fix at Layer 1 via bare-var {num: x};
	// this pin locks the var-PROP shape Create6 does not exercise.
	"authored create inline map var prop bound guard": {
		src: "MATCH (b)\nCREATE (a {name: b.c})\nRETURN a",
		want: oneWriteBranch(query.Part{
			Bindings: []query.Binding{
				must(query.NewNodeBinding("b", nil)),
				must(query.NewNodeBinding("a", nil)),
			},
			Returns: []query.ReturnItem{
				{Name: "a", Value: query.NewRefProjection(query.Ref{Variable: "a"}, query.TypeNode{})},
			},
			Effects: []query.Effect{query.NewCreateEffect([]string{"a"})},
		}),
	},
	// --- Stage 14 (CALL + procsig registry) mustParse pins ---
	// Verbatim + AUTHORED per spec §1.8. Every CALL-carrying pin ships
	// its per-scenario procedure signature slice; the parser is
	// constructed with cypher.WithRegistry(NewRegistry(sigs)) per pin.

	// Call1 [1] Standalone CALL, no args, no yields, no results in
	// signature. StatementRead (CALL is Read), zero CallBindings,
	// projection-less part — the Stage-14 shape most similar to a
	// projection-less write.
	"CALL standalone no-args no-yields empty-results (Call1[1])": {
		src: "CALL test.doNothing()",
		want: oneBranch(query.Part{
			ReturnsAll: true,
		}),
		sigs: []procsig.Signature{{Name: "test.doNothing"}},
	},
	// Call1 [2] Standalone CALL implicit invocation (no parens).
	// Same shape as [1]; implicit-vs-explicit is a runtime
	// distinction (implicit args come from parameters at runtime),
	// invisible at the type-interface boundary.
	"CALL standalone implicit no-args empty-results (Call1[2])": {
		src: "CALL test.doNothing",
		want: oneBranch(query.Part{
			ReturnsAll: true,
		}),
		sigs: []procsig.Signature{{Name: "test.doNothing"}},
	},
	// Call1 [3] In-query CALL with no YIELD followed by RETURN n.
	// MATCH binds n; CALL introduces no CallBinding (in-query no-YIELD
	// posture, spec §4.2 step 9); RETURN n resolves via the MATCH
	// binding.
	"CALL in-query no-YIELD RETURN prior-match (Call1[3])": {
		src: "MATCH (n)\nCALL test.doNothing()\nRETURN n",
		want: oneBranch(query.Part{
			Bindings: []query.Binding{
				must(query.NewNodeBinding("n", nil)),
			},
			Returns: []query.ReturnItem{
				{Name: "n", Value: query.NewRefProjection(query.Ref{Variable: "n"}, query.TypeNode{})},
			},
		}),
		sigs: []procsig.Signature{{Name: "test.doNothing"}},
	},
	// Call1 [5] Standalone CALL with no args + implicit YIELD * from
	// the one-result signature. One CallBinding `label` (STRING?);
	// Part.Returns is one RefProjection on `label` at TypeString;
	// ReturnsAll true.
	"CALL standalone no-args implicit-YIELD (Call1[5])": {
		src: "CALL test.labels()",
		want: oneBranch(query.Part{
			Bindings: []query.Binding{
				must(query.NewCallBinding("label", "test.labels", "label", query.TypeString{}, true)),
			},
			Returns: []query.ReturnItem{
				{Name: "label", Value: query.NewRefProjection(query.Ref{Variable: "label"}, query.TypeString{})},
			},
			ReturnsAll: true,
		}),
		sigs: []procsig.Signature{{
			Name: "test.labels",
			Results: []procsig.Result{
				{Name: "label", Token: procsig.TokenString, Nullable: true},
			},
		}},
	},
	// Call1 [6] In-query CALL YIELD followed by RETURN. One
	// CallBinding `label`; Returns is the RETURN clause's
	// RefProjection on `label`, ReturnsAll false (no `*`).
	"CALL in-query YIELD RETURN (Call1[6])": {
		src: "CALL test.labels() YIELD label\nRETURN label",
		want: oneBranch(query.Part{
			Bindings: []query.Binding{
				must(query.NewCallBinding("label", "test.labels", "label", query.TypeString{}, true)),
			},
			Returns: []query.ReturnItem{
				{Name: "label", Value: query.NewRefProjection(query.Ref{Variable: "label"}, query.TypeString{})},
			},
		}),
		sigs: []procsig.Signature{{
			Name: "test.labels",
			Results: []procsig.Result{
				{Name: "label", Token: procsig.TokenString, Nullable: true},
			},
		}},
	},
	// Call2 [1] In-query CALL with explicit args + YIELD + RETURN.
	// Two CallBindings (city, country_code); two args are literals
	// (no refs), so no ExprUses. RETURN drives the returns slice.
	"CALL in-query explicit args YIELD RETURN (Call2[1])": {
		src: "CALL test.my.proc('Stefan', 1) YIELD city, country_code\nRETURN city, country_code",
		want: oneBranch(query.Part{
			Bindings: []query.Binding{
				must(query.NewCallBindingWithArgs("city", "test.my.proc", "city", query.TypeString{}, true, []query.CallArg{query.NewCallArg(query.TypeString{}), query.NewCallArg(query.TypeInt{})})),
				must(query.NewCallBindingWithArgs("country_code", "test.my.proc", "country_code", query.TypeInt{}, true, []query.CallArg{query.NewCallArg(query.TypeString{}), query.NewCallArg(query.TypeInt{})})),
			},
			Returns: []query.ReturnItem{
				{Name: "city", Value: query.NewRefProjection(query.Ref{Variable: "city"}, query.TypeString{})},
				{Name: "country_code", Value: query.NewRefProjection(query.Ref{Variable: "country_code"}, query.TypeInt{})},
			},
		}),
		sigs: []procsig.Signature{{
			Name: "test.my.proc",
			Params: []procsig.Param{
				{Name: "name", Token: procsig.TokenString, Nullable: true},
				{Name: "id", Token: procsig.TokenInteger, Nullable: true},
			},
			Results: []procsig.Result{
				{Name: "city", Token: procsig.TokenString, Nullable: true},
				{Name: "country_code", Token: procsig.TokenInteger, Nullable: true},
			},
		}},
	},
	// Call2 [2] Standalone CALL with explicit args (implicit YIELD).
	// Two CallBindings (signature order), Part.Returns synthesised
	// from CallBindings, ReturnsAll true.
	"CALL standalone explicit args implicit-YIELD (Call2[2])": {
		src: "CALL test.my.proc('Stefan', 1)",
		want: oneBranch(query.Part{
			Bindings: []query.Binding{
				must(query.NewCallBindingWithArgs("city", "test.my.proc", "city", query.TypeString{}, true, []query.CallArg{query.NewCallArg(query.TypeString{}), query.NewCallArg(query.TypeInt{})})),
				must(query.NewCallBindingWithArgs("country_code", "test.my.proc", "country_code", query.TypeInt{}, true, []query.CallArg{query.NewCallArg(query.TypeString{}), query.NewCallArg(query.TypeInt{})})),
			},
			Returns: []query.ReturnItem{
				{Name: "city", Value: query.NewRefProjection(query.Ref{Variable: "city"}, query.TypeString{})},
				{Name: "country_code", Value: query.NewRefProjection(query.Ref{Variable: "country_code"}, query.TypeInt{})},
			},
			ReturnsAll: true,
		}),
		sigs: []procsig.Signature{{
			Name: "test.my.proc",
			Params: []procsig.Param{
				{Name: "name", Token: procsig.TokenString, Nullable: true},
				{Name: "id", Token: procsig.TokenInteger, Nullable: true},
			},
			Results: []procsig.Result{
				{Name: "city", Token: procsig.TokenString, Nullable: true},
				{Name: "country_code", Token: procsig.TokenInteger, Nullable: true},
			},
		}},
	},
	// Call5 [8] Standalone CALL with args + YIELD *. Same expansion
	// as Call2 [2] (implicit YIELD == YIELD * for the standalone
	// path).
	"CALL standalone explicit args YIELD * (Call5[8])": {
		src: "CALL test.my.proc('Stefan', 1) YIELD *",
		want: oneBranch(query.Part{
			Bindings: []query.Binding{
				must(query.NewCallBindingWithArgs("city", "test.my.proc", "city", query.TypeString{}, true, []query.CallArg{query.NewCallArg(query.TypeString{}), query.NewCallArg(query.TypeInt{})})),
				must(query.NewCallBindingWithArgs("country_code", "test.my.proc", "country_code", query.TypeInt{}, true, []query.CallArg{query.NewCallArg(query.TypeString{}), query.NewCallArg(query.TypeInt{})})),
			},
			Returns: []query.ReturnItem{
				{Name: "city", Value: query.NewRefProjection(query.Ref{Variable: "city"}, query.TypeString{})},
				{Name: "country_code", Value: query.NewRefProjection(query.Ref{Variable: "country_code"}, query.TypeInt{})},
			},
			ReturnsAll: true,
		}),
		sigs: []procsig.Signature{{
			Name: "test.my.proc",
			Params: []procsig.Param{
				{Name: "name", Token: procsig.TokenString, Nullable: true},
				{Name: "id", Token: procsig.TokenInteger, Nullable: true},
			},
			Results: []procsig.Result{
				{Name: "city", Token: procsig.TokenString, Nullable: true},
				{Name: "country_code", Token: procsig.TokenInteger, Nullable: true},
			},
		}},
	},
	// Call3 [1] NUMBER accepts INTEGER — the arg-type check is
	// bucket-3, so the pin exercises the accept-path structurally
	// (one CallBinding `out` at TypeString, nullable true). The
	// argument 42 is a literal, so no refs.
	"CALL NUMBER accepts INTEGER standalone (Call3[1])": {
		src: "CALL test.my.proc(42)",
		want: oneBranch(query.Part{
			Bindings: []query.Binding{
				must(query.NewCallBindingWithArgs("out", "test.my.proc", "out", query.TypeString{}, true, []query.CallArg{query.NewCallArg(query.TypeInt{})})),
			},
			Returns: []query.ReturnItem{
				{Name: "out", Value: query.NewRefProjection(query.Ref{Variable: "out"}, query.TypeString{})},
			},
			ReturnsAll: true,
		}),
		sigs: []procsig.Signature{{
			Name: "test.my.proc",
			Params: []procsig.Param{
				{Name: "in", Token: procsig.TokenNumber, Nullable: true},
			},
			Results: []procsig.Result{
				{Name: "out", Token: procsig.TokenString, Nullable: true},
			},
		}},
	},
	// AUTHORED (Stage 14 §1.8 pin a): CALL inside EXISTS does not
	// populate an outer CallBinding. The subqueryDepth counter
	// suppresses the inner CALL; the outer query stays a plain
	// MATCH ... RETURN with no CallBinding leaks.
	"authored CALL inside EXISTS suppression": {
		src: "MATCH (n)\nWHERE exists { CALL test.labels() YIELD label RETURN label }\nRETURN n",
		want: oneBranch(query.Part{
			Bindings: []query.Binding{
				must(query.NewNodeBinding("n", nil)),
			},
			Returns: []query.ReturnItem{
				{Name: "n", Value: query.NewRefProjection(query.Ref{Variable: "n"}, query.TypeNode{})},
			},
		}),
		sigs: []procsig.Signature{{
			Name: "test.labels",
			Results: []procsig.Result{
				{Name: "label", Token: procsig.TokenString, Nullable: true},
			},
		}},
	},
	// AUTHORED (Stage 14 §1.8 pin b): bound-var CALL argument
	// regression lock. MATCH binds n; CALL uses n.name — the
	// property expression records `n` on curPart.refs via arg-side
	// typeExpressionMining. Matched-pair counterpart of the mustReject
	// pin `authored CALL unbound var argument kill probe`. Passes as
	// an accept-path regression lock.
	"authored CALL bound-var argument regression lock": {
		src: "MATCH (n)\nCALL test.labels(n.name) YIELD label\nRETURN label",
		want: oneBranch(query.Part{
			Bindings: []query.Binding{
				must(query.NewNodeBinding("n", nil)),
				must(query.NewCallBindingWithArgs("label", "test.labels", "label", query.TypeString{}, true, []query.CallArg{query.NewCallArg(query.TypeUnknown{})})),
			},
			Returns: []query.ReturnItem{
				{Name: "label", Value: query.NewRefProjection(query.Ref{Variable: "label"}, query.TypeString{})},
			},
		}),
		sigs: []procsig.Signature{{
			Name: "test.labels",
			Params: []procsig.Param{
				{Name: "in", Token: procsig.TokenString, Nullable: true},
			},
			Results: []procsig.Result{
				{Name: "label", Token: procsig.TokenString, Nullable: true},
			},
		}},
	},
	// AUTHORED (Stage 14 §1.8 pin c): standalone-CALL Returns
	// expansion is deterministic signature-declaration order. Two
	// results (a: INTEGER, b: STRING); the CALL has no YIELD, so the
	// expansion follows sig.Results order.
	"authored CALL standalone Returns signature-declaration-order": {
		src: "CALL test.my.proc(42)",
		want: oneBranch(query.Part{
			Bindings: []query.Binding{
				must(query.NewCallBindingWithArgs("a", "test.my.proc", "a", query.TypeInt{}, true, []query.CallArg{query.NewCallArg(query.TypeInt{})})),
				must(query.NewCallBindingWithArgs("b", "test.my.proc", "b", query.TypeString{}, true, []query.CallArg{query.NewCallArg(query.TypeInt{})})),
			},
			Returns: []query.ReturnItem{
				{Name: "a", Value: query.NewRefProjection(query.Ref{Variable: "a"}, query.TypeInt{})},
				{Name: "b", Value: query.NewRefProjection(query.Ref{Variable: "b"}, query.TypeString{})},
			},
			ReturnsAll: true,
		}),
		sigs: []procsig.Signature{{
			Name: "test.my.proc",
			Params: []procsig.Param{
				{Name: "in", Token: procsig.TokenNumber, Nullable: true},
			},
			Results: []procsig.Result{
				{Name: "a", Token: procsig.TokenInteger, Nullable: true},
				{Name: "b", Token: procsig.TokenString, Nullable: true},
			},
		}},
	},
	// Call6 [1] Two CALLs across WITH. Two parts:
	//  - Part 1: one CallBinding `label`; Returns is `count(*) AS c`
	//    (aggregate on the empty-args count).
	//  - Part 2: fresh CallBinding `label` (this part's own CALL);
	//    Returns is RETURN * over the in-scope set {c, label}.
	// Pins the "CallBinding does NOT leak past a WITH's explicit
	// export" rule: part 2 introduces its own `label` binding, and
	// buildPart's imported map (from part 1's WITH) carries `c`
	// only.
	"CALL then WITH then CALL (Call6[1])": {
		src: "CALL test.labels() YIELD label\nWITH count(*) AS c\nCALL test.labels() YIELD label\nRETURN *",
		want: query.Query{
			Branches: []query.Branch{{Parts: []query.Part{
				{
					Bindings: []query.Binding{
						must(query.NewCallBinding("label", "test.labels", "label", query.TypeString{}, true)),
					},
					Returns: []query.ReturnItem{
						{Name: "c", Value: query.NewAggregateProjection(query.AggCount, nil, false, query.TypeInt{})},
					},
				},
				{
					Bindings: []query.Binding{
						must(query.NewCallBinding("label", "test.labels", "label", query.TypeString{}, true)),
					},
					ReturnsAll: true,
				},
			}}},
		},
		sigs: []procsig.Signature{{
			Name: "test.labels",
			Results: []procsig.Result{
				{Name: "label", Token: procsig.TokenString, Nullable: true},
			},
		}},
	},
	// AUTHORED (Stage 14 §1.8 pin d): YIELD…WHERE arg-mining probe.
	// The grammar admits `oC_YieldItems → … ( SP? oC_Where )?`;
	// the corpus is silent. Pin verifies the WHERE is walked for
	// parameter uses ($needle enters query.Parameters) without
	// disrupting the CallBinding shape.
	"authored CALL YIELD trailing WHERE parameter-mining probe": {
		src: "CALL test.labels() YIELD label WHERE label = $needle\nRETURN label",
		want: func() query.Query {
			q := oneBranch(query.Part{
				Bindings: []query.Binding{
					must(query.NewCallBinding("label", "test.labels", "label", query.TypeString{}, true)),
				},
				Returns: []query.ReturnItem{
					{Name: "label", Value: query.NewRefProjection(query.Ref{Variable: "label"}, query.TypeString{})},
				},
			})
			q.Parameters = []query.Parameter{{
				Name: "needle",
				Uses: []query.Use{query.NewExprUse(query.TypeBool{}, query.ExprInPredicate)},
			}}
			return q
		}(),
		sigs: []procsig.Signature{{
			Name: "test.labels",
			Results: []procsig.Result{
				{Name: "label", Token: procsig.TokenString, Nullable: true},
			},
		}},
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

// markBareRefNode is the 5xg parser-test bridge to the unexported
// markReferencedInRequiredBarePattern mutator. It captures the value in an
// addressable local, flips the flag through the exported parser wrapper
// (query.MarkNodeBindingReferencedInRequiredBarePattern — the same symbol
// build.go uses), and returns the mutated value so it can sit inline in a
// struct-literal Bindings slice. Only used from mustParse pins that exercise
// the axis's true side (§5.1.1); every non-5xg pin stays byte-identical.
func markBareRefNode(b query.NodeBinding) query.NodeBinding {
	query.MarkNodeBindingReferencedInRequiredBarePattern(&b)
	return b
}

func TestMustParse(t *testing.T) {
	for name, c := range mustParse {
		t.Run(name, func(t *testing.T) {
			p := newParserFor(t, c.sigs)
			got, err := p.Parse(strings.NewReader(c.src))
			require.NoError(t, err, "read-core query must parse: %q", c.src)
			require.Equal(t, c.want, got)
		})
	}
}

// newParserFor builds a cypher parser wired with a procedure signature
// registry populated from the given slice (or the empty registry if the
// slice is empty). Stage 14 pins targeting CALL supply signatures; pins
// that do not touch CALL leave sigs nil. Every registry construction is
// asserted valid at the test boundary — a malformed pin sig is a
// programmer error, not a runtime concern.
func newParserFor(t *testing.T, sigs []procsig.Signature) query.Parser {
	t.Helper()
	if len(sigs) == 0 {
		return cypher.New()
	}
	reg, err := procsig.NewRegistry(sigs)
	require.NoError(t, err, "test-side registry must be valid")
	return cypher.New(cypher.WithRegistry(reg))
}

// mustReject pairs out-of-scope/invalid read-core queries with the sentinel they
// must produce. Each query is verbatim from a .feature file (source named).
var mustReject = map[string]struct {
	query string
	want  error
	// sigs is Stage 14: per-pin procedure signatures the parser needs to
	// resolve CALL clauses in `query`. Empty for every pre-Stage-14 pin.
	sigs []procsig.Signature
}{
	// Stage 14 (Call1 [13]): standalone CALL against an unknown procedure.
	// The parser holds an empty (or non-covering) procedure registry, so
	// the collectCall lookup misses and raises ErrUnknownProcedure.
	// Verbatim from clauses/call/Call1.feature [13]; TCK error class
	// ProcedureError:ProcedureNotFound.
	"CALL unknown procedure standalone": {
		query: "CALL test.my.proc",
		want:  cypher.ErrUnknownProcedure,
	},
	// Stage 14 (Call1 [14]): in-query CALL against an unknown procedure.
	// Same fail-site as the standalone twin above — the two share
	// collectCall's registry-lookup step (spec §4.2 step 2). Verbatim
	// from clauses/call/Call1.feature [14].
	"CALL unknown procedure in-query": {
		query: "CALL test.my.proc() YIELD out\nRETURN out",
		want:  cypher.ErrUnknownProcedure,
	},
	// Stage 14 (Call1 [7]): standalone CALL with too few explicit
	// arguments. Signature declares two params; call passes one.
	// Statically provable from the registry, so bucket-1 reject via
	// ErrProcedureArity (spec §4.2 step 4). Verbatim from
	// clauses/call/Call1.feature [7]; TCK error class
	// SyntaxError:InvalidNumberOfArguments.
	"CALL too few explicit args (standalone)": {
		query: "CALL test.my.proc('Dobby')",
		want:  cypher.ErrProcedureArity,
		sigs: []procsig.Signature{{
			Name: "test.my.proc",
			Params: []procsig.Param{
				{Name: "name", Token: procsig.TokenString, Nullable: true},
				{Name: "in", Token: procsig.TokenInteger, Nullable: true},
			},
			Results: []procsig.Result{
				{Name: "out", Token: procsig.TokenInteger, Nullable: true},
			},
		}},
	},
	// Stage 14 (Call1 [9]): standalone CALL with too many explicit
	// arguments. Signature declares one param; call passes four. Same
	// fail-site as the too-few twin above. Verbatim from
	// clauses/call/Call1.feature [9].
	"CALL too many explicit args (standalone)": {
		query: "CALL test.my.proc(1, 2, 3, 4)",
		want:  cypher.ErrProcedureArity,
		sigs: []procsig.Signature{{
			Name: "test.my.proc",
			Params: []procsig.Param{
				{Name: "in", Token: procsig.TokenInteger, Nullable: true},
			},
			Results: []procsig.Result{
				{Name: "out", Token: procsig.TokenInteger, Nullable: true},
			},
		}},
	},
	// Stage 14 (Call1 [15]): YIELD binds a name already in imported
	// scope from a preceding WITH. Fail-site: buildPart's Stage-14
	// four-way sweep (spec §4.7) — the CallBinding `label` collides
	// with the imported name `label` from the WITH's export. Verbatim
	// from clauses/call/Call1.feature [15]; TCK error class
	// SyntaxError:VariableAlreadyBound. Reuses ErrVariableKindConflict
	// per the Stage-9 unwind-vs-unwind precedent (Q4 ruling).
	"CALL YIELD shadows imported name": {
		query: "WITH 'Hi' AS label\nCALL test.labels() YIELD label\nRETURN *",
		want:  cypher.ErrVariableKindConflict,
		sigs: []procsig.Signature{{
			Name: "test.labels",
			Results: []procsig.Result{
				{Name: "label", Token: procsig.TokenString, Nullable: true},
			},
		}},
	},
	// Stage 14 (Call5 [5]): YIELD intra-list rename collision — `YIELD
	// a, b AS a` — the second item's AS-alias equals the first item's
	// bare name. Fail-site: collectCall's intra-YIELD collision check
	// (spec §4.2 step 6). Verbatim from clauses/call/Call5.feature [5];
	// TCK error class SyntaxError:VariableAlreadyBound.
	"CALL YIELD intra rename collision (b AS a)": {
		query: "CALL test.my.proc(null) YIELD a, b AS a\nRETURN a",
		want:  cypher.ErrVariableKindConflict,
		sigs: []procsig.Signature{{
			Name: "test.my.proc",
			Params: []procsig.Param{
				{Name: "in", Token: procsig.TokenInteger, Nullable: true},
			},
			Results: []procsig.Result{
				{Name: "a", Token: procsig.TokenInteger, Nullable: true},
				{Name: "b", Token: procsig.TokenInteger, Nullable: true},
			},
		}},
	},
	// Stage 14 (Call5 [6]): YIELD intra-list rename collision — `YIELD
	// a AS c, b AS c` — two items rename to the same target. Same
	// fail-site as the Call5 [5] twin. Verbatim from
	// clauses/call/Call5.feature [6].
	"CALL YIELD intra rename collision (both to c)": {
		query: "CALL test.my.proc(null) YIELD a AS c, b AS c\nRETURN c",
		want:  cypher.ErrVariableKindConflict,
		sigs: []procsig.Signature{{
			Name: "test.my.proc",
			Params: []procsig.Param{
				{Name: "in", Token: procsig.TokenInteger, Nullable: true},
			},
			Results: []procsig.Result{
				{Name: "a", Token: procsig.TokenInteger, Nullable: true},
				{Name: "b", Token: procsig.TokenInteger, Nullable: true},
			},
		}},
	},
	// Stage 14 (Call1 [12]): in-query CALL with no YIELD followed by
	// RETURN referencing a would-be result column. In-query CALL
	// introduces NO CallBinding without a YIELD (spec §4.2 step 9), so
	// `RETURN out` names nothing in scope — falls out of the existing
	// buildPart refs-vs-scope sweep as ErrUnboundVariable. Verbatim
	// from clauses/call/Call1.feature [12]; TCK error class
	// SyntaxError:UndefinedVariable.
	"in-query CALL no YIELD RETURN references dropped result": {
		query: "CALL test.my.proc(1)\nRETURN out",
		want:  cypher.ErrUnboundVariable,
		sigs: []procsig.Signature{{
			Name: "test.my.proc",
			Params: []procsig.Param{
				{Name: "in", Token: procsig.TokenInteger, Nullable: true},
			},
			Results: []procsig.Result{
				{Name: "out", Token: procsig.TokenInteger, Nullable: true},
			},
		}},
	},
	// AUTHORED (Stage 14 §1.8 matched pair): a CALL argument references
	// an unbound variable. No preceding MATCH binds `m`, so the arg-
	// mining path records `m` on curPart.refs (spec §4.2 step 3), and
	// buildPart's referential-integrity sweep raises ErrUnboundVariable.
	// Matched-pair counterpart of the mustParse pin "authored CALL
	// bound-var argument regression lock" — that pin exercises the
	// accept-path with a bound `n`; this one exercises the reject-path
	// with an unbound `m`. Corpus is silent on this shape at the
	// clauses/call pinned tag.
	"authored CALL unbound var argument kill probe": {
		query: "CALL test.labels(m.name) YIELD label\nRETURN label",
		want:  cypher.ErrUnboundVariable,
		sigs: []procsig.Signature{{
			Name: "test.labels",
			Params: []procsig.Param{
				{Name: "in", Token: procsig.TokenString, Nullable: true},
			},
			Results: []procsig.Result{
				{Name: "label", Token: procsig.TokenString, Nullable: true},
			},
		}},
	},
	// AUTHORED (Stage 14 §1.8 Q1 fold-in): YIELD references a result
	// field the signature does not declare. Fail-site: collectCall's
	// YIELD-item enumeration (spec §4.2 step 6, first sub-item). Pins
	// the one-sentinel-covers-both ruling (Q1): registry-miss and
	// unknown-YIELD-field-miss share ErrUnknownProcedure, with the
	// wrapped message doing the sub-category disambiguation ("unknown
	// procedure result field: nofield on test.labels"). Corpus is
	// silent on this shape.
	"authored CALL YIELD unknown result field kill probe": {
		query: "CALL test.labels() YIELD nofield\nRETURN nofield",
		want:  cypher.ErrUnknownProcedure,
		sigs: []procsig.Signature{{
			Name: "test.labels",
			Results: []procsig.Result{
				{Name: "label", Token: procsig.TokenString, Nullable: true},
			},
		}},
	},
	// AUTHORED (Stage 12 amend §1.5): SET with a nested propertyExpression
	// target (n.a.b). The model's Ref carries a single Property, so a nested
	// LHS has no honest single-Ref shape — accept-and-truncate would claim
	// SET target n.a when the query says n.a.b, a wrong concrete claim about
	// the very field repository codegen consumes. The pinned-tag TCK
	// exercises zero such shapes (grep confirmed), so parse-reject is a
	// bucket-1 posture with zero corpus fallout. Real engines reject nested
	// SET anyway ("only directly attached properties can be set"), so the
	// fail-site aligns parser semantics with runtime semantics.
	"nested SET target": {
		query: "MATCH (n)\nSET n.a.b = 1\nRETURN n",
		want:  cypher.ErrNestedPropertyTarget,
	},
	// AUTHORED (Stage 12 amend §1.6): REMOVE with a nested propertyExpression
	// target (n.a.b). Same shape rule as nested SET — the Ref cannot
	// represent a multi-lookup target, and the pinned-tag TCK exercises zero
	// such shapes. Bucket-1 reject.
	"nested REMOVE target": {
		query: "MATCH (n)\nREMOVE n.a.b\nRETURN n",
		want:  cypher.ErrNestedPropertyTarget,
	},
	// (Stage 6: the two ErrUnsupportedProjection pins from Stages 3-5 —
	// "arithmetic over projection" and "unary-signed projection" — are RETIRED.
	// Their queries now parse as ExprProjection, and the sentinel is deleted;
	// see the mustParse cases "arithmetic over projection" and "unary-signed
	// projection" for the accept-path.)
	// (Stage 8: the "multi-type relationship" pin from Stages 0-7 — the
	// former ErrUnsupportedPattern fail-site — is RETIRED. The three
	// pattern shapes (named paths, variable-length, multi-type) now parse
	// under Stage 8's widened model; see the "multi-type edge" and its
	// siblings in mustParse for the accept-path. ErrUnsupportedPattern is
	// deleted from the sentinel set entirely — its last remaining fail-site
	// is retired.)
	// Return1 [2] returning an undefined variable -> ErrUnboundVariable
	"unbound variable": {
		query: "MATCH ()\nRETURN foo",
		want:  cypher.ErrUnboundVariable,
	},
	// AUTHORED: per-part scope (spec §4). WITH a.x AS n exports only the scalar
	// name "n" into the next part, so the final RETURN a references a name dropped
	// by the WITH — out of scope downstream -> ErrUnboundVariable. The fail-site is
	// build()'s per-part scope check (Stage 4). No verbatim corpus query exercises
	// the dropped-binding case cleanly at the pinned tag.
	"out of scope after with": {
		query: "MATCH (a)\nWITH a.x AS n\nRETURN a",
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
	// AUTHORED: non-bare $p in SKIP/LIMIT — fail-site is
	// mineClauseSlotParameter's findParameters>0 branch (the Stage 1
	// fail-site cycles 1/2 introduced for the bare-vs-non-bare accept
	// rule). The cycle-3 audit of return-skip-limit/ verified every $p
	// in that dir is a bare atom, so no verbatim TCK query exercises
	// this shape at the pinned tag. Replace with the corpus entry when
	// a TCK bump adds one.
	"skip non-bare param": {
		query: "MATCH (n)\nRETURN n\nSKIP $p + 1",
		want:  cypher.ErrUnsupportedParameter,
	},
	// AUTHORED (Stage 9 fix round B1a): a named path (p = (a)-->(b))
	// followed by UNWIND [...] AS p in the same part is a three-way
	// kind clash — path vs unwind — the same class as the Stage-8
	// path-vs-entity check. Fail-site: collectUnwind scans
	// pathBindings for the same name and raises
	// ErrVariableKindConflict at listener time.
	"unwind name clashes with prior named path": {
		query: "MATCH p=(a)-->(b)\nUNWIND [1] AS p\nRETURN p",
		want:  cypher.ErrVariableKindConflict,
	},
	// AUTHORED (Stage 9 fix round B1b): the reversed order — UNWIND
	// binds p first, then MATCH p = (...) tries to introduce the same
	// name as a named-path binding. Fail-site: collectPattern scans
	// unwindBindings before appending the pathBinding.
	"named path clashes with prior unwind": {
		query: "UNWIND [1] AS p\nMATCH p=(a)-->(b)\nRETURN p",
		want:  cypher.ErrVariableKindConflict,
	},
	// AUTHORED (Stage 9 fix round B1c): two UNWINDs in the same part
	// binding the same variable — a self-collision the byVar check
	// alone missed (an UnwindBinding does not enter byVar). Fail-site:
	// collectUnwind also scans the existing unwindBindings.
	"unwind name reused within a part": {
		query: "UNWIND [1] AS x\nUNWIND [2] AS x\nRETURN x",
		want:  cypher.ErrVariableKindConflict,
	},
	// AUTHORED (Stage 9 fix round B2a): UNWIND [1,2] AS x binds x to a
	// scalar (int); the following MATCH (x) reuses the name in a
	// node-pattern position. Under Stage 9's initial (over-eager)
	// nameBoundAsUnwind skip, the node binding was silently discarded.
	// The rule: MATCH-reuse is legitimate only when the UNWIND element
	// type is node / edge / unknown; scalar elemType falls through to
	// mergeBinding → byVar collision → ErrVariableKindConflict.
	"unwind scalar reused as node match": {
		query: "UNWIND [1,2] AS x\nMATCH (x)\nRETURN x",
		want:  cypher.ErrVariableKindConflict,
	},
	// AUTHORED (Stage 9 fix round B2b): UNWIND [1,2] AS r binds r to a
	// scalar (int); the following MATCH (a)-[r]->(b) reuses r in an
	// edge-pattern position. Same rule as B2a: scalar elemType blocks
	// the skip and yields a byVar collision at MATCH time. Without the
	// gate, the edge binding was silently erased (a would be unrelated
	// to b to any downstream consumer).
	"unwind scalar reused as edge match": {
		query: "UNWIND [1,2] AS r\nMATCH (a)-[r]->(b)\nRETURN a,b,r",
		want:  cypher.ErrVariableKindConflict,
	},
	// AUTHORED (Stage 9 fix round B2c): UNWIND [1,2] AS b2 binds b2 to
	// a scalar (int); the following MATCH (b2:Label) reuses it in a
	// node-pattern position, this time with a label constraint. Same
	// rule as B2a: without the gate, the label constraint was silently
	// dropped alongside the node binding.
	"unwind scalar reused as labelled node match": {
		query: "UNWIND [1,2] AS b2\nMATCH (b2:Label)\nRETURN b2",
		want:  cypher.ErrVariableKindConflict,
	},
	// Stage 11 (gqlc-3r0 fold) — Pattern1 [22]: a pattern predicate used
	// as a scalar RETURN column. Verbatim corpus query — the TCK cites
	// SyntaxError:UnexpectedSyntax, a bucket-1 parse-shape rejection
	// (ADR 0007 §I). The fail-site is collectReturnItem's Stage-11
	// isPatternPredicateAtom check; retiring the two Pattern1 [22]/[23]
	// skiplist entries pushes both scenarios onto this fail-site.
	"pattern predicate in return projection": {
		query: "MATCH (n) RETURN (n)-[]->()",
		want:  cypher.ErrPatternInProjection,
	},
	// AUTHORED (Stage 13 §1.8, ruling Q3(b) kill-probe): unbound inline-
	// map ref via var-PROP. CREATE (a {name: b.c}) — `b` is not bound by
	// any preceding clause. Today's mineInlineMap walks OC_MapLiteral
	// values only for PARAMETER uses, never records `b`, so buildPart's
	// referential-integrity sweep sees no unbound ref and the query
	// parses silently — that is exactly the "silent info drop where
	// parser could reject" blocker the widening fixes. After GREEN,
	// mineInlineMap records `b` on curPart.refs and the sweep raises
	// ErrUnboundVariable. Textbook RED→GREEN pin, no wire delta.
	// Layer-1 counterpart: Create1[20] + Create2[24] come off the
	// skiplist (spec §5). Archetype: Create1[20]'s bare-var shape
	// (CREATE (b {name: missing})) — this pin uses var-PROP (b.c) to
	// lock the shape Create1[20] does not exercise.
	"authored create unbound inline map ref kill probe": {
		query: "CREATE (a {name: b.c})",
		want:  cypher.ErrUnboundVariable,
	},
}

func TestMustReject(t *testing.T) {
	for name, tc := range mustReject {
		t.Run(name, func(t *testing.T) {
			p := newParserFor(t, tc.sigs)
			got, err := p.Parse(strings.NewReader(tc.query))
			require.Error(t, err, "out-of-scope query must be rejected: %q", tc.query)
			require.Equal(t, query.Query{}, got, "model must be the zero value on error")
			require.ErrorIs(t, err, tc.want)
		})
	}
}

// mustRejectGrammar pairs each verbatim/authored query with the grammar-level
// parse failure it must produce — i.e. an ANTLR-level syntax error, not one
// of the six domain sentinels. The pin still asserts (a) non-nil error and
// (b) zero-value Query, so a future grammar widening that silently accepts
// the shape fails loudly. TestSentinelReachability is NOT run against this
// map because there is no domain sentinel to reach (Stage 13 §4.4, A1).
var mustRejectGrammar = map[string]struct {
	query string
	// sigs is Stage 14: some grammar-reject pins carry corpus-verbatim
	// signatures alongside their queries. The ANTLR grammar rejects
	// before the listener consults the registry, so signatures are
	// documentation-only here — they mirror the TCK background step
	// intent verbatim.
	sigs []procsig.Signature
}{
	// Stage 13 (A1 fold-in): MERGE grammar admits exactly ONE oC_PatternPart
	// (Cypher.g4 §oC_Merge : MERGE SP? oC_PatternPart ( SP oC_MergeAction )*).
	// A comma-separated second pattern part fails at the ANTLR-generated
	// parser before the listener runs — no domain sentinel raised.
	"merge multiple pattern parts": {
		query: "MERGE (a), (b)",
	},
	// Stage 14: in-query CALL disallows implicit invocation. The grammar
	// (Cypher.g4 §oC_InQueryCall) requires oC_ExplicitProcedureInvocation
	// (parens), so `CALL test.my.proc YIELD out` fails at ANTLR before
	// the listener runs. Verbatim from clauses/call/Call2.feature [4],
	// which carries the @skipGrammarCheck tag in the TCK for exactly
	// this posture.
	"in-query CALL with implicit invocation": {
		query: "CALL test.my.proc YIELD out\nRETURN out",
		sigs: []procsig.Signature{{
			Name: "test.my.proc",
			Params: []procsig.Param{
				{Name: "in", Token: procsig.TokenInteger, Nullable: true},
			},
			Results: []procsig.Result{
				{Name: "out", Token: procsig.TokenInteger, Nullable: true},
			},
		}},
	},
	// Stage 14: in-query CALL disallows YIELD *. The grammar
	// (Cypher.g4 §oC_InQueryCall) permits YIELD only over an
	// oC_YieldItems list — the '*' alternative is on
	// oC_StandaloneCall exclusively. Verbatim from clauses/call/
	// Call5.feature [7], which carries the @skipGrammarCheck tag in
	// the TCK for exactly this posture.
	"in-query CALL with YIELD *": {
		query: "CALL test.my.proc('Stefan', 1) YIELD *\nRETURN city, country_code",
		sigs: []procsig.Signature{{
			Name: "test.my.proc",
			Params: []procsig.Param{
				{Name: "name", Token: procsig.TokenString, Nullable: true},
				{Name: "id", Token: procsig.TokenInteger, Nullable: true},
			},
			Results: []procsig.Result{
				{Name: "city", Token: procsig.TokenString, Nullable: true},
				{Name: "country_code", Token: procsig.TokenInteger, Nullable: true},
			},
		}},
	},
}

func TestMustRejectGrammar(t *testing.T) {
	for name, tc := range mustRejectGrammar {
		t.Run(name, func(t *testing.T) {
			p := newParserFor(t, tc.sigs)
			got, err := p.Parse(strings.NewReader(tc.query))
			require.Error(t, err, "grammar-invalid query must be rejected: %q", tc.query)
			require.Equal(t, query.Query{}, got, "model must be the zero value on error")
		})
	}
}

// allSentinels is the canonical list of the seven Parse sentinels — the
// single source of truth TestSentinelReachability checks against. A new
// sentinel must be added here (and exercised by a mustReject case); a
// removed one must be dropped. errNotImplemented is deliberately absent:
// it is the run-A stub, not a contract sentinel. Stage 6 retired
// ErrUnsupportedProjection: the projection classifier now accepts every
// scalar expression at RETURN / WITH position. Stage 8 retired
// ErrUnsupportedPattern. Stage 11 added ErrPatternInProjection. Stage 12
// added ErrNestedPropertyTarget. Stage 14 retires ErrUnsupportedClause
// entirely (its last fail-site was CALL, which is supported after
// Stage 14) and adds ErrUnknownProcedure and ErrProcedureArity.
// The internal model-invariant sentinel ErrEmptyPart lives on the query
// package (not cypher) and is NOT included here — it is unreachable via
// parse, so a reachability sweep would fail.
var allSentinels = []error{
	cypher.ErrUnsupportedParameter,
	cypher.ErrUnboundVariable,
	cypher.ErrVariableKindConflict,
	cypher.ErrPatternInProjection,
	cypher.ErrNestedPropertyTarget,
	cypher.ErrUnknownProcedure,
	cypher.ErrProcedureArity,
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
// the property tests below would pass vacuously, so this is the gate. Stage 14:
// each pin's per-scenario signature slice is carried through
// newParserFor so CALL pins parse with the same registry TestMustParse
// constructs.
func TestPropertyReadCoreParses(t *testing.T) {
	type pin struct {
		src  string
		sigs []procsig.Signature
	}
	pins := make([]pin, 0, len(mustParse))
	for _, c := range mustParse {
		pins = append(pins, pin{src: c.src, sigs: c.sigs})
	}
	rapid.Check(t, func(rt *rapid.T) {
		p := rapid.SampledFrom(pins).Draw(rt, "pin")
		parser := newParserFor(t, p.sigs)
		if _, err := parser.Parse(strings.NewReader(p.src)); err != nil {
			rt.Fatalf("read-core query did not parse: %q: %v", p.src, err)
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

// assertReferentialIntegrity checks each part's refs (return-item refs and edge
// endpoints) resolve against that part's bindings OR a name the prior part's WITH
// exported into it (the per-part scope §4). It threads the exported-name set left
// to right across parts within each branch; parameters are query-wide and must
// resolve against the union of every part's bindings.
func assertReferentialIntegrity(rt *rapid.T, q query.Query, src string) {
	allNamed := make(map[string]bool)
	for _, br := range q.Branches {
		for _, part := range br.Parts {
			for _, b := range part.Bindings {
				if v := bindingVariable(b); v != "" {
					allNamed[v] = true
				}
			}
		}
	}

	for _, br := range q.Branches {
		imported := make(map[string]bool) // names the prior part exported into this one
		for _, part := range br.Parts {
			named := make(map[string]bool)
			for k := range imported {
				named[k] = true
			}
			for _, b := range part.Bindings {
				if v := bindingVariable(b); v != "" {
					named[v] = true
				}
			}
			resolves := func(v string) bool { return v != "" && named[v] }

			for _, b := range part.Bindings {
				switch bb := b.(type) {
				case query.EdgeBinding:
					for _, ep := range []query.Endpoint{bb.Source(), bb.Target()} {
						if ve, ok := ep.(query.VarEndpoint); ok && !resolves(ve.Variable()) {
							rt.Fatalf("endpoint variable %q has no binding in %q", ve.Variable(), src)
						}
					}
				case query.PathBinding:
					// Stage 8 (fix round, B1): every named member must resolve
					// to a binding of the matching kind in the same part.
					// Anonymous members carry no name, so no lookup is done.
					for i, m := range bb.Members() {
						if m.Anonymous() {
							continue
						}
						v := m.Variable()
						if !resolves(v) {
							rt.Fatalf("path %q member %d %q has no binding in %q", bb.Variable(), i, v, src)
						}
						assertPathMemberKindAgrees(rt, part, bb, i, m, src)
					}
				}
			}
			for _, r := range part.Returns {
				switch v := r.Value.(type) {
				case query.RefProjection:
					if !resolves(v.Ref().Variable) {
						rt.Fatalf("return ref %q has no binding in %q", v.Ref().Variable, src)
					}
				case query.LiteralProjection:
					// A literal traces back to no binding — referential check is N/A.
				case query.FuncProjection:
					for _, ref := range v.Refs() {
						if !resolves(ref.Variable) {
							rt.Fatalf("func projection ref %q has no binding in %q", ref.Variable, src)
						}
					}
				case query.AggregateProjection:
					for _, ref := range v.Refs() {
						if !resolves(ref.Variable) {
							rt.Fatalf("aggregate projection ref %q has no binding in %q", ref.Variable, src)
						}
					}
				case query.ExprProjection:
					// Stage 6: a rich scalar expression's refs are the union of
					// every var/var.prop atom the typer walked into. Every one
					// must resolve — the referential-integrity invariant is
					// unchanged; the variant is new.
					for _, ref := range v.Refs() {
						if !resolves(ref.Variable) {
							rt.Fatalf("expr projection ref %q has no binding in %q", ref.Variable, src)
						}
					}
				default:
					rt.Fatalf("return item has unknown Projection variant %T in %q", r.Value, src)
				}
			}

			// Compute the set this part exports into the next: under WITH *, the
			// whole in-scope set carries forward; otherwise each return item's Name.
			next := make(map[string]bool)
			if part.ReturnsAll {
				for k := range named {
					next[k] = true
				}
			} else {
				for _, r := range part.Returns {
					next[r.Name] = true
				}
			}
			imported = next
		}
	}

	for _, p := range q.Parameters {
		for _, u := range p.Uses {
			switch use := u.(type) {
			case query.PropertyUse:
				if !allNamed[use.Ref().Variable] {
					rt.Fatalf("parameter %q use ref %q has no binding in %q", p.Name, use.Ref().Variable, src)
				}
			case query.ClauseSlotUse:
				// A clause-slot use has no Variable — referential check is N/A.
			case query.ExprUse:
				// Stage 6 §4: an ExprUse carries the enclosing expression's
				// result type and a projection/predicate discriminator; no Ref
				// to check. The parameter's own type is inferred later.
			default:
				rt.Fatalf("parameter %q has unknown Use variant %T in %q", p.Name, u, src)
			}
		}
	}
}

// assertNamedBindingsUnique checks named bindings are unique within each part
// (uniqueness is per-part scope, not query-wide — a name re-MATCHed in a later
// part is a fresh binding there).
func assertNamedBindingsUnique(rt *rapid.T, q query.Query, src string) {
	for _, br := range q.Branches {
		for _, part := range br.Parts {
			seen := make(map[string]bool)
			for _, b := range part.Bindings {
				v := bindingVariable(b)
				if v == "" {
					continue // anonymous edges are each their own binding
				}
				if seen[v] {
					rt.Fatalf("named binding %q appears more than once in a part in %q", v, src)
				}
				seen[v] = true
			}
		}
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

// bindingVariable reads the variable of any binding variant via its accessor.
// Stage 8: PathBinding joins the sum with an always-non-empty variable.
// Stage 9: UnwindBinding joins the sum with an always-non-empty variable.
func bindingVariable(b query.Binding) string {
	switch v := b.(type) {
	case query.NodeBinding:
		return v.Variable()
	case query.EdgeBinding:
		return v.Variable()
	case query.PathBinding:
		return v.Variable()
	case query.UnwindBinding:
		return v.Variable()
	default:
		return ""
	}
}

// assertPathMemberKindAgrees checks a named path member's kind matches the
// resolved binding's kind in the same part: a NamedNodeMember must resolve
// to a NodeBinding, a NamedEdgeMember to an EdgeBinding. This is the
// referential-integrity guard the string-only members representation
// could not offer — under the tagged sum, a mismatch is a parser bug.
func assertPathMemberKindAgrees(rt *rapid.T, part query.Part, pb query.PathBinding, i int, m query.PathMember, src string) {
	name := m.Variable()
	for _, b := range part.Bindings {
		if bindingVariable(b) != name {
			continue
		}
		switch b.(type) {
		case query.NodeBinding:
			if m.Kind() != query.BindingNode {
				rt.Fatalf("path %q member %d (%q) is %s in the path but the part binds it as node in %q",
					pb.Variable(), i, name, m.Kind().String(), src)
			}
		case query.EdgeBinding:
			if m.Kind() != query.BindingEdge {
				rt.Fatalf("path %q member %d (%q) is %s in the path but the part binds it as edge in %q",
					pb.Variable(), i, name, m.Kind().String(), src)
			}
		}
		return
	}
}
