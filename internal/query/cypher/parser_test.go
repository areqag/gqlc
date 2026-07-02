package cypher_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/areqag/gqlc/internal/graph"
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

var mustParse = map[string]struct {
	src  string
	want query.Query
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
	// flag is true (ADR 0006). The RETURN item traces back to it via Ref{n,""}.
	"optional match simple": {
		src: "OPTIONAL MATCH (n)\nRETURN n",
		want: oneBranch(query.Part{
			Bindings: []query.Binding{
				must(query.NewNullableNodeBinding("n", nil)),
			},
			Returns: []query.ReturnItem{
				{Name: "n", Value: query.NewRefProjection(query.Ref{Variable: "n"}, query.TypeNode{})},
			},
		}),
	},
	// Match7 [2] "OPTIONAL MATCH with previously bound nodes" — verbatim TCK
	// query. Pins the reuse rule (ADR 0006): n is first introduced in the
	// required MATCH (non-nullable); the anonymous :NOT_EXIST edge and x are
	// first introduced in OPTIONAL MATCH (both nullable). The anonymous edge
	// carries the nullable flag uniformly even though no Ref reads it.
	"optional match reuses prior binding": {
		src: "MATCH (n)\nOPTIONAL MATCH (n)-[:NOT_EXIST]->(x)\nRETURN n, x",
		want: oneBranch(query.Part{
			Bindings: []query.Binding{
				must(query.NewNodeBinding("n", nil)),
				must(query.NewNullableEdgeBinding("", graph.LabelSet{"NOT_EXIST"},
					must(query.NewVarEndpoint("n")),
					must(query.NewVarEndpoint("x")),
					true,
				)),
				must(query.NewNullableNodeBinding("x", nil)),
			},
			Returns: []query.ReturnItem{
				{Name: "n", Value: query.NewRefProjection(query.Ref{Variable: "n"}, query.TypeNode{})},
				{Name: "x", Value: query.NewRefProjection(query.Ref{Variable: "x"}, query.TypeNode{})},
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
	// the in-scope bindings post-freeze; the parser records "every in-scope
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
					must(query.NewEdgeHops(intPtr(1), intPtr(3))),
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
	// resolver forms one candidate EdgeKey per type post-freeze. The RETURN
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
					must(query.NewEdgeHops(intPtr(1), intPtr(3))),
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
	// type; the resolver upgrades from the schema post-freeze) with
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
	// count still types as TypeInt unconditionally.
	"count distinct": {
		src: "OPTIONAL MATCH (a)\nRETURN count(DISTINCT a)",
		want: oneBranch(query.Part{
			Bindings: []query.Binding{
				must(query.NewNullableNodeBinding("a", nil)),
			},
			Returns: []query.ReturnItem{
				{Name: "count(DISTINCT a)", Value: query.NewAggregateProjection(query.AggCount, []query.Ref{{Variable: "a"}}, true, query.TypeInt{})},
			},
		}),
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
	// the schema post-freeze).
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
	// the aggregate's touched ref and the promoted result type.
	"count in arithmetic": {
		src: "MATCH (n)\nRETURN count(n) + 1",
		want: oneBranch(query.Part{
			Bindings: []query.Binding{must(query.NewNodeBinding("n", nil))},
			Returns: []query.ReturnItem{
				{Name: "count(n) + 1", Value: query.NewExprProjection([]query.Ref{{Variable: "n"}}, query.TypeInt{})},
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
}

// intPtr is a small helper to take the address of an int literal so
// NewEdgeHops (which takes *int for optional bounds) can be called cleanly
// from the mustParse table. Extracted here so the pin table stays terse.
func intPtr(i int) *int { return &i }

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
	// AUTHORED: a write clause (CREATE) is out of scope throughout (ADR 0004) and
	// the fail-site for ErrUnsupportedClause. Stage 4 supports WITH/UNION, so the
	// prior `with clause` reject (which pinned this sentinel) now parses; this
	// preserves ErrUnsupportedClause reachability via a surviving rejected clause.
	// Replace with a verbatim corpus query if a clean one appears at the pinned tag.
	"write clause": {
		query: "CREATE (n)\nRETURN n",
		want:  cypher.ErrUnsupportedClause,
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

// allSentinels is the canonical list of the five Parse sentinels — the single
// source of truth TestSentinelReachability checks against. A new sentinel must be
// added here (and exercised by a mustReject case); a removed one must be dropped.
// errNotImplemented is deliberately absent: it is the run-A stub, not a contract
// sentinel. Stage 6 retired ErrUnsupportedProjection: the projection classifier
// now accepts every scalar expression at RETURN / WITH position, so the sentinel
// has no fail-site left to guard. Stage 8 retired ErrUnsupportedPattern: the
// three pattern shapes it flagged (named paths, variable-length, multi-type)
// all parse under the widened model, so the sentinel has no fail-site left.
// Stage 11 adds ErrPatternInProjection: a pattern predicate used as a scalar
// RETURN / WITH column is a bucket-1 parse-shape rejection (Pattern1 [22]/[23]).
var allSentinels = []error{
	cypher.ErrUnsupportedClause,
	cypher.ErrUnsupportedParameter,
	cypher.ErrUnboundVariable,
	cypher.ErrVariableKindConflict,
	cypher.ErrPatternInProjection,
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
				// to check. The parameter's own type is inferred post-freeze.
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
