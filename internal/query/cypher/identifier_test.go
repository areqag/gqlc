package cypher_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/graph"
	"github.com/areqag/gqlc/internal/procsig"
	"github.com/areqag/gqlc/internal/query"
	"github.com/areqag/gqlc/internal/query/cypher"
)

// --- delimited identifiers in the query front end (bd gqlc-y25yo) ---
//
// A delimited identifier's delimiters are syntax and not content, so the
// delimited and undelimited spellings of one name must reach the model as the
// same bytes. Until gqlc-y25yo the query front end carried the delimiters
// through at every position, so a schema declaring Person and a query naming the
// delimited spelling of Person named two types that never unified.
//
// The schema front end settled this at gqlc-tzu9r; the two fronts have to agree
// or the asymmetry IS the defect, so the rows below are stated against the same
// chokepoint the schema side reaches — graph.LabelSet.Key — rather than against
// the parser's own output alone.

// parseQuery parses src through the public Parse with no procedure registry.
func parseQuery(t *testing.T, src string) (query.Query, error) {
	t.Helper()
	return cypher.New().Parse(strings.NewReader(src))
}

// modelJSON is the parsed model rendered as JSON. The per-site rows assert
// against it rather than against a dozen different model accessors because the
// claim under test is one claim — that no delimiter survives into the model —
// and the JSON is the one surface every position shares. It is also the surface
// the goldens are written on, so a row and a golden cannot disagree about what
// the parser produced.
func modelJSON(t *testing.T, q query.Query) string {
	t.Helper()
	b, err := json.Marshal(q)
	require.NoError(t, err)
	return string(b)
}

// nodeLabelsOfN returns the label set the parser committed to the node binding
// named n, searching every branch and part. Every query that reaches it binds
// its node as n, so the name is fixed rather than a parameter: a parameter only
// ever passed one value reads as a generality the file does not have.
func nodeLabelsOfN(t *testing.T, q query.Query) graph.LabelSet {
	t.Helper()
	for _, br := range q.Branches {
		for _, part := range br.Parts {
			for _, b := range part.Bindings {
				if nb, ok := b.(query.NodeBinding); ok && nb.Variable() == "n" {
					return nb.Labels()
				}
			}
		}
	}
	t.Fatal("no node binding named n in parsed model")
	return nil
}

// TestADelimitedLabelKeysAsTheUndelimitedOne is this bead's falsifier, and what
// it asserts is the KEY SPELLING rather than merely that the parse succeeded.
//
// The distinction is the whole test, and it is the one internal/schema/gql's
// TestAnAmpersandBearingLabelKeysAsOneLabel makes on the other front. A decode
// that reached the model with the key layer still unquoted would ALSO parse
// cleanly, and would spell the delimited one-label set A&B identically to the
// two-label set — the type-identity forgery that made this bead wait on
// gqlc-649co. require.NoError cannot tell those apart; only the spelling can.
//
// graph.LabelSet.Key is the single point every producer of labels flows through,
// schema and query alike, so a query label keying as the schema's key for the
// same name IS the unification this bead is about.
func TestADelimitedLabelKeysAsTheUndelimitedOne(t *testing.T) {
	t.Run("a node label", func(t *testing.T) {
		q, err := parseQuery(t, "MATCH (n:`Person`) RETURN n")
		require.NoError(t, err)
		require.Equal(t, graph.LabelSet{"Person"}.Key(), nodeLabelsOfN(t, q).Key(),
			"the delimited spelling must key as the schema's key for Person, or ErrUnknownLabel is raised for a label that exists")
	})

	t.Run("an ampersand-bearing label stays distinct from the two-label set", func(t *testing.T) {
		one, err := parseQuery(t, "MATCH (n:`A&B`) RETURN n")
		require.NoError(t, err)
		two, err := parseQuery(t, "MATCH (n:A:B) RETURN n")
		require.NoError(t, err)

		oneKey := nodeLabelsOfN(t, one).Key()
		twoKey := nodeLabelsOfN(t, two).Key()

		require.Equal(t, graph.LabelSetKey("`A&B`"), oneKey,
			"the key quotes the label; spelled A&B it would be the two-label set")
		require.Equal(t, graph.LabelSetKey("A&B"), twoKey)
		require.NotEqual(t, oneKey, twoKey,
			"decoding opened the type-identity forgery through the query front end (bd gqlc-649co)")
		require.Equal(t, graph.LabelSet{"A&B"}, oneKey.Split())
	})
}

// decodedSite is one identifier read position, its isolating query, and the
// decoded spelling that query must put into the model.
//
// One row per READ SITE, not one per grammar production: the sites are what a
// regression reverts one of, and the bead this fixes was re-scoped precisely
// because a file-scoped wave desynchronises binding lookup (see
// TestDelimitedVariablesDecodeAtomically). Each query is chosen so that
// reverting its own site — and only its own site — to the undecoded read reds
// this row.
type decodedSite struct {
	site  string // file / function the row pins
	query string
	// want is a JSON fragment of the parsed model that must be present. The
	// absent-delimiter check is global and applies to every row.
	want string
	sigs []procsig.Signature
}

var decodedSites = map[string]decodedSite{
	"node label": {
		site:  "pattern.go nodeLabels",
		query: "MATCH (n:`Person`) RETURN n",
		want:  `"labels":["Person"]`,
	},
	"relationship type": {
		site:  "pattern.go relTypes",
		query: "MATCH ()-[r:`KNOWS`]->() RETURN r",
		want:  `"labels":["KNOWS"]`,
	},
	"node variable": {
		site:  "pattern.go collectNode",
		query: "MATCH (`n`:Person) RETURN 1",
		want:  `"kind":"node","variable":"n"`,
	},
	"edge variable": {
		site:  "pattern.go collectEdge",
		query: "MATCH (a)-[`r`:KNOWS]->(b) RETURN 1",
		want:  `"kind":"edge","variable":"r"`,
	},
	"path variable": {
		site:  "pattern.go collectPatternPart",
		query: "MATCH `p` = (a)-[]->(b) RETURN a",
		want:  `"kind":"path","variable":"p"`,
	},
	"endpoint variable": {
		site:  "pattern.go endpoint",
		query: "MATCH (`a`)-[r]->(b) RETURN r",
		want:  `"source":{"kind":"var","variable":"a"}`,
	},
	"comparison operand variable": {
		site:  "shape.go refFromNonArithmetic (variable)",
		query: "MATCH (`n`) WHERE `n`.age = $p RETURN 1",
		want:  `"kind":"property","variable":"n"`,
	},
	"comparison operand property": {
		site:  "shape.go refFromNonArithmetic (property)",
		query: "MATCH (n) WHERE n.`age` = $p RETURN 1",
		want:  `"property":"age"`,
	},
	"parameter name": {
		site:  "shape.go parameterName",
		query: "MATCH (n) WHERE n.age = $`p` RETURN 1",
		want:  `"parameters":[{"name":"p"`,
	},
	"SET property target": {
		site:  "shape.go propertyExpressionRef",
		query: "MATCH (n) SET n.`age` = 1",
		want:  `"kind":"setProperty","variable":"n","property":"age"`,
	},
	"SET property target variable": {
		site:  "shape.go bareVariableFromAtom",
		query: "MATCH (`n`) SET `n`.age = 1",
		want:  `"kind":"setProperty","variable":"n"`,
	},
	"UNWIND variable": {
		site:  "expr.go collectUnwind",
		query: "UNWIND [1, 2] AS `x` RETURN `x`",
		want:  `"kind":"unwind","variable":"x"`,
	},
	"projection alias": {
		site:  "expr.go collectReturnItem (alias)",
		query: "MATCH (n) RETURN n AS `col`",
		want:  `"name":"col"`,
	},
	"bare projected variable": {
		site:  "expr.go collectReturnItem (bare variable)",
		query: "MATCH (`n`) RETURN `n`",
		want:  `"name":"n"`,
	},
	"inline map key, bare parameter value": {
		site:  "expr.go mineInlineMap (fast path)",
		query: "MATCH (n {`age`: $p}) RETURN 1",
		want:  `"kind":"property","variable":"n","property":"age"`,
	},
	"inline map key, widened value": {
		site:  "expr.go mineInlineMap (widening path)",
		query: "MATCH (n {`age`: [$p]}) RETURN 1",
		want:  `"kind":"property","variable":"n","property":"age"`,
	},
	"SET labels target": {
		site:  "expr.go collectSetItem (labels arm)",
		query: "MATCH (`n`) SET `n`:Person",
		want:  `"kind":"setLabels","variable":"n"`,
	},
	"SET entity target": {
		site:  "expr.go collectSetItem (entity arm)",
		query: "MATCH (`n`) SET `n` = {}",
		want:  `"kind":"setEntity","variable":"n"`,
	},
	"REMOVE labels target": {
		site:  "expr.go collectRemoveItem",
		query: "MATCH (`n`) REMOVE `n`:Person",
		want:  `"kind":"removeLabels","variable":"n"`,
	},
	// Both rich-expression rows alias their projection. Un-aliased they
	// would trip the absent-delimiter sweep on a position that is
	// deliberately verbatim — ReturnItem.Name for a non-bare expression is
	// the source text, delimiters and all, and
	// TestARichProjectionKeepsItsSourceText is where that is asserted
	// rather than dodged. The alias moves the name off the sweep's path
	// without moving the site under test: collectReturnItem mines the
	// expression through typing.go before it reads the alias.
	"rich-expression property lookup": {
		site:  "typing.go typeNonArithmetic",
		query: "MATCH (n) RETURN n.`age` + 1 AS x",
		want:  `"property":"age"`,
	},
	"rich-expression variable atom": {
		site:  "typing.go typeAtom",
		query: "MATCH (`n`) RETURN `n` IS NULL AS x",
		want:  `"refs":[{"variable":"n","property":""}]`,
	},
	"procedure name": {
		site:  "call.go extractProcedureName",
		query: "CALL `test`.`my`.`proc`() YIELD out RETURN out",
		want:  `"kind":"call","variable":"out"`,
		sigs:  []procsig.Signature{procSig()},
	},
	"YIELD alias": {
		site:  "call.go extractYieldItem (variable)",
		query: "CALL test.my.proc() YIELD out AS `o` RETURN `o`",
		want:  `"kind":"call","variable":"o"`,
		sigs:  []procsig.Signature{procSig()},
	},
	"YIELD result field": {
		site:  "call.go extractYieldItem (source field)",
		query: "CALL test.my.proc() YIELD `out` AS o RETURN o",
		want:  `"kind":"call","variable":"o"`,
		sigs:  []procsig.Signature{procSig()},
	},
}

// procSig is the one signature the CALL rows resolve against. Its name is
// undelimited: the point of the procedure-name row is that a delimited CALL and
// an undelimited declaration name ONE procedure.
func procSig() procsig.Signature {
	return procsig.Signature{
		Name:    "test.my.proc",
		Results: []procsig.Result{{Name: "out", Token: procsig.TokenInteger, Nullable: true}},
	}
}

// TestADelimitedIdentifierReachesTheModelDecoded walks every read position a
// delimited identifier can arrive at.
//
// The two assertions do different work and neither substitutes for the other.
// The positive fragment says the decoded name landed at the position the row is
// about; the absent-delimiter check says no OTHER position on the same query
// kept one, which is what turns a table of point assertions into a sweep. A row
// asserting only its fragment would pass while a neighbouring site on the same
// query still leaked.
func TestADelimitedIdentifierReachesTheModelDecoded(t *testing.T) {
	for name, tt := range decodedSites {
		t.Run(name, func(t *testing.T) {
			p := cypher.New()
			if len(tt.sigs) > 0 {
				reg, err := procsig.NewRegistry(tt.sigs)
				require.NoError(t, err)
				p = cypher.New(cypher.WithRegistry(reg))
			}
			q, err := p.Parse(strings.NewReader(tt.query))
			require.NoError(t, err, "site %s", tt.site)

			got := modelJSON(t, q)
			require.Contains(t, got, tt.want, "site %s did not decode", tt.site)
			require.NotContains(t, got, "`",
				"site %s: a delimiter survived into the model", tt.site)
		})
	}
}

// TestDelimitedVariablesDecodeAtomically is the row that makes this a five-file
// change rather than a pattern.go one.
//
// A variable is bound at one site and referenced at others in four other files.
// Decode the binding side alone and the two spellings stop matching, so a query
// that parses TODAY starts failing as an unbound variable — a REGRESSION, not an
// incomplete fix. The measurement that re-scoped this bead used exactly the
// first query below (bd gqlc-y25yo notes, 2026-09-10).
//
// The WITH row is the one the bead's own site inventory missed. ReturnItem.Name
// is read twice — as the output column name and, through exportedNames, as the
// name a WITH exports into the next part's scope — so leaving it as the verbatim
// source text desynchronises the export from every reference to it.
func TestDelimitedVariablesDecodeAtomically(t *testing.T) {
	for name, src := range map[string]string{
		"bind in a pattern, reference in a projection": "MATCH (`n`:Person) RETURN `n`",
		"bind in a pattern, reference in a SET target": "MATCH (`n`) SET `n`.age = 1",
		"carry across a WITH with no alias":            "MATCH (`n`) WITH `n` RETURN `n`",
		"carry across a WITH with an alias":            "MATCH (`n`) WITH `n` AS `m` RETURN `m`",
		"carry across a WITH star":                     "MATCH (`n`) WITH * RETURN `n`",
		"bind in UNWIND, reference in a projection":    "UNWIND [1, 2] AS `x` RETURN `x`",
		"reference an endpoint bound elsewhere":        "MATCH (`a`), (`a`)-[r]->(b) RETURN r",
	} {
		t.Run(name, func(t *testing.T) {
			q, err := parseQuery(t, src)
			require.NoError(t, err,
				"the binding side and the reference side disagree about the name")
			require.NotContains(t, modelJSON(t, q), "`")
		})
	}
}

// TestADelimitedIdentifierDecodesItsEscape covers the one escape Cypher.g4
// §EscapedSymbolicName admits: a doubled delimiter denoting one.
//
// The rows are stated at the label position because graph.LabelSet.Key is where
// a mis-decode becomes a wrong identity rather than a wrong string. The last two
// are the interesting ones: a name that IS a delimiter, and a name whose first
// byte is one, which needsQuoting quotes at the key even though the name has no
// ampersand.
func TestADelimitedIdentifierDecodesItsEscape(t *testing.T) {
	for name, tt := range map[string]struct {
		query string
		want  graph.LabelSet
	}{
		"no escape":            {"MATCH (n:`Person`) RETURN n", graph.LabelSet{"Person"}},
		"an interior escape":   {"MATCH (n:`A``B`) RETURN n", graph.LabelSet{"A`B"}},
		"two interior escapes": {"MATCH (n:`A``B``C`) RETURN n", graph.LabelSet{"A`B`C"}},
		"the name is one byte of escape": {
			"MATCH (n:````) RETURN n", graph.LabelSet{"`"},
		},
		"a leading escape": {"MATCH (n:```A`) RETURN n", graph.LabelSet{"`A"}},
		"a trailing escape": {
			"MATCH (n:`A```) RETURN n", graph.LabelSet{"A`"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			q, err := parseQuery(t, tt.query)
			require.NoError(t, err)
			got := nodeLabelsOfN(t, q)
			require.Equal(t, tt.want, got)
			require.Equal(t, tt.want, got.Key().Split(),
				"the decoded label must survive its own key round-trip")
		})
	}
}

// TestAnEmptyDelimitedIdentifierIsRefused pins the one refusal this change adds,
// at a position from each of the model's two absence sentinels plus one that has
// neither.
//
// Emptiness is the model's own spelling of ABSENCE — rawBinding.variable == "" is
// an anonymous pattern element and query.Ref.Property == "" is a bare variable
// reference — so decoding an empty name into either reports a DIFFERENT query
// rather than one the model cannot hold. The label row has no such collision and
// is refused anyway, because the schema front end refuses the same shape and a
// name no schema can declare is one no query can name.
//
// Each row asserts the sentinel, not merely an error: an empty variable name
// that reached collection surfaces downstream as ErrUnboundVariable, which is a
// symptom of this and of several unrelated mistakes.
func TestAnEmptyDelimitedIdentifierIsRefused(t *testing.T) {
	for name, src := range map[string]string{
		"a node variable":     "MATCH (``) RETURN 1",
		"a node label":        "MATCH (n:``) RETURN n",
		"a relationship type": "MATCH ()-[r:``]->() RETURN r",
		"a property key":      "MATCH (n) RETURN n.``",
		"a projection alias":  "MATCH (n) RETURN n AS ``",
		"an UNWIND variable":  "UNWIND [1] AS `` RETURN 1",
		"a parameter":         "MATCH (n) WHERE n.age = $`` RETURN n",
		// This row pins the ORDER as well as the refusal, and it is the only
		// row here that does. The sweep runs before the collection walk so
		// that ErrEmptyIdentifier names the cause; moved after the walk, the
		// six rows above still report it, because nothing downstream of them
		// fails first. This shape does fail first — two anonymous endpoints
		// reach query.NewVarEndpoint, whose own non-empty precondition
		// refuses — so a post-walk sweep reports a constructor's internal
		// complaint about a variable the author never wrote, and the sentinel
		// never surfaces. Measured as a surviving mutant before this row
		// existed (bd gqlc-y25yo).
		"a relationship endpoint, before the walk can mistake it": "MATCH (``)-[r]->(``) RETURN r",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := parseQuery(t, src)
			require.ErrorIs(t, err, cypher.ErrEmptyIdentifier)
		})
	}
}

// TestAnUndelimitedIdentifierIsUnchanged is the negative control for every row
// above: the decode must be invisible to a query that delimits nothing.
//
// Without it the sweep is satisfiable by a change that mangles ordinary names
// too — and the rows above, which all read delimited input, would not notice.
func TestAnUndelimitedIdentifierIsUnchanged(t *testing.T) {
	q, err := parseQuery(t, "MATCH (n:Person)-[r:KNOWS]->(m) WHERE n.age = $p RETURN n.age AS col")
	require.NoError(t, err)

	got := modelJSON(t, q)
	for _, want := range []string{
		`"variable":"n"`, `"labels":["Person"]`, `"variable":"r"`, `"labels":["KNOWS"]`,
		`"variable":"m"`, `"name":"p"`, `"property":"age"`, `"name":"col"`,
	} {
		require.Contains(t, got, want)
	}
}

// TestAParenthesisedProjectionKeepsItsSourceText pins the boundary of the
// projection-name exception.
//
// bareProjectedVariable does not unwrap parentheses the way bareVariableFromAtom
// does, so an un-aliased parenthesised projection keeps the verbatim column name
// the E1 rule gives it. Without this row the exception could be widened to the
// recursive unwrap with no test noticing, which would rename a column nothing
// about delimited identifiers asked to rename.
func TestAParenthesisedProjectionKeepsItsSourceText(t *testing.T) {
	q, err := parseQuery(t, "MATCH (n) RETURN (n)")
	require.NoError(t, err)
	require.Contains(t, modelJSON(t, q), `"name":"(n)"`)
}

// TestARichProjectionKeepsItsSourceText is the other half of that boundary, and
// the one position in the model where a delimiter is expected to survive.
//
// An un-aliased projection that is not a bare variable takes its column name
// from the verbatim source text, so a delimiter inside the expression reaches
// ReturnItem.Name. That is not a decode miss: the name is a rendering of what
// the author wrote, not an identifier the model resolves anything against — the
// refs beside it carry the decoded names, and this row asserts both at once.
// The decodedSites sweep aliases its rich-expression rows so their names leave
// this position; without this row that sidestep would look like coverage.
func TestARichProjectionKeepsItsSourceText(t *testing.T) {
	q, err := parseQuery(t, "MATCH (n) RETURN n.`age` + 1")
	require.NoError(t, err)

	got := modelJSON(t, q)
	require.Contains(t, got, "\"name\":\"n.`age` + 1\"",
		"the column name is the author's text")
	require.Contains(t, got, `"variable":"n","property":"age"`,
		"the ref beside it is decoded")
}

// --- delimited function names (bd gqlc-e3k0) ---
//
// gqlc-y25yo decoded 24 identifier read sites and deliberately left two:
// shape.go's functionName and fullFunctionName. Both match against a catalogue
// of BUILT-IN openCypher names rather than against anything the schema declares,
// so the defect that bead fixed — two spellings of one name failing to unify
// across the schema/query boundary — cannot arise here. The open question was
// narrower and was a semantics question: does the delimited spelling of a
// built-in DENOTE that built-in?
//
// openCypher's answer is yes, and it is stated rather than inferred. The
// Cypher 9 reference (Procedures) carries a worked example headed "Call a
// procedure using a quoted namespace and name", whose prose reads "This calls a
// procedure db.labels" over the query CALL `db`.`labels` — neither segment is a
// reserved word, so the escaping there is gratuitous and the example exists only
// to say that the gratuitously-escaped spelling resolves to the built-in. Its
// reserved-word appendix says the same thing from the other side: it names
// Variables, FUNCTION NAMES and Parameters as the three contexts a reserved word
// may not be used in, and that "by escaping any of the reserved words
// (encapsulating in backticks `), they would be valid as identifiers in the
// above contexts" — so a function name is an identifier position and the
// backticks are the identifier's spelling, not a different name.
//
// Two servers were asked the same question directly and agree (both against the
// image digests this repository already pins for its live batteries, 2026-09-11):
//
//   - neo4j 5.26.28 answers `toString`(1) with "1", `count`(n) with the
//     aggregate, and `nosuchfunc`(1) with Unknown function 'nosuchfunc' —
//     quoting the name back DECODED, which is the server saying the delimiters
//     were gone before it consulted its catalogue.
//   - apache/age 1.7.0 answers `date`() with `function date does not exist`,
//     byte-identical to what it answers date() — same decode, different server,
//     different catalogue.
//
// The limits, because the evidence does not reach as far as the claim: the
// spec's worked example is for a PROCEDURE, and openCypher 9 contains no worked
// example of a delimited FUNCTION name. The transfer rests on the two names
// being one production — Cypher.g4 §oC_FunctionName and §oC_ProcedureName are
// both `oC_Namespace oC_SymbolicName` — plus the appendix sentence above, plus
// the two server measurements. The openCypher TCK settles nothing either way:
// across its whole corpus in this tree exactly ONE delimited identifier appears
// inside a query block (Call1.feature, `RETURN n.name AS ` + "`name`" + `), and it is
// an alias. The falsifier for the whole section is a server that resolves the
// delimited spelling to a DIFFERENT function than the undelimited one; neither
// server tested does, and no third was tried.
//
// This is also the reading the tree already committed to for the sibling
// production: call.go's procedureNameOf has decoded each segment of a procedure
// name since gqlc-y25yo, which is exactly the spec example's case. Leaving the
// function readers undecoded left the two halves of one grammar rule disagreeing.

// TestADelimitedBuiltinNameDenotesTheBuiltin is this bead's falsifier.
//
// Each row asserts the CLASSIFICATION the delimited spelling earns, not merely
// that it parsed: before this bead every one of them parsed, and every one
// lowered to the undifferentiated FuncProjection/TypeUnknown fall-through, which
// is a clean parse of the wrong query. Asserting the model JSON is what
// separates "the name reached the catalogue" from "the name reached the parser".
func TestADelimitedBuiltinNameDenotesTheBuiltin(t *testing.T) {
	for name, tt := range map[string]struct {
		src  string
		want string
	}{
		// functionName → aggregateFunc. The aggregate kind is the whole
		// answer: a FuncProjection here would drop the grouping semantics
		// the resolver builds its GROUP BY from.
		"an aggregate at RETURN position": {
			"MATCH (n) RETURN `count`(n) AS c",
			`"kind":"aggregate"`,
		},
		// The case-folding and the decode compose, in that order: the row
		// would pass on a decode that forgot to lowercase only if the
		// catalogue held the author's case, which it does not.
		"an aggregate spelled in mixed case": {
			"MATCH (n) RETURN `cOuNt`(n) AS c",
			`"kind":"aggregate"`,
		},
		// fullFunctionName → temporalConstructorType, bare arm.
		"a bare temporal constructor": {
			"MATCH (n) RETURN `date`() AS d",
			`"type":"date"`,
		},
		// fullFunctionName → temporalConstructorType, NAMESPACED arm. This
		// row is the one that reaches the namespace loop rather than the
		// trailing symbolic name, so a decode applied only to the bare name
		// leaves it failing.
		"a namespaced temporal constructor": {
			"MATCH (n) RETURN `duration`.`between`(n.a, n.b) AS d",
			`"type":"duration"`,
		},
		// Half-delimited, because an author who backticks one segment has
		// no reason to backtick the other and the two segments are decoded
		// by different lines of fullFunctionName.
		"a namespaced constructor with only its namespace delimited": {
			"MATCH (n) RETURN `duration`.between(n.a, n.b) AS d",
			`"type":"duration"`,
		},
		"a namespaced constructor with only its bare name delimited": {
			"MATCH (n) RETURN duration.`between`(n.a, n.b) AS d",
			`"type":"duration"`,
		},
		// functionName → builtinScalarFuncType, the third catalogue these
		// two readers feed. Its arg-shape guard is satisfied by the node
		// binding, so a miss here is the NAME missing and nothing else.
		"a builtin scalar function": {
			"MATCH (n) RETURN `elementId`(n) AS e",
			`"type":"string"`,
		},
		// typing.go's rich-expression walk, which reads both readers through
		// its own call sites. Without this row the RETURN-position rows
		// above could be satisfied by a change to classifyFunction alone,
		// leaving the two positions disagreeing about one call — the drift
		// those call sites' own comments say is impossible.
		"an aggregate inside a rich expression": {
			"MATCH (n) RETURN `count`(n) + 1 AS c",
			`"containsAggregate":true`,
		},
		// The temporal half of that same walk. Parenthesised rather than
		// bare so the call is typed by typeExpression's atom arm instead of
		// by classifyFunction, which the bare rows above already cover: an
		// ExprProjection carrying "date" is typing.go's answer, and a
		// FuncProjection carrying it would be expr.go's.
		"a temporal constructor inside a rich expression": {
			"MATCH (n) RETURN (`date`()) AS d",
			`"kind":"expr","refs":null,"type":"date"`,
		},
	} {
		t.Run(name, func(t *testing.T) {
			q, err := parseQuery(t, tt.src)
			require.NoError(t, err)
			require.Contains(t, modelJSON(t, q), tt.want)
		})
	}
}

// TestADelimitedNameThatIsNotTheBuiltinIsNotTheBuiltin is the negative control,
// and it is what makes the rows above about DECODING rather than about stripping.
//
// A decode resolves a doubled delimiter to one, so the delimited spelling whose
// text is co-backtick-backtick-unt denotes co`unt, and no catalogue holds that.
//
// The second call is not decoration. Without it this row is satisfied by a
// change that makes EVERY delimited name miss the catalogue — which is precisely
// the behaviour before this bead — so it would read as a guard while guarding
// nothing. The pair is the claim: one delimited name that lands in the
// catalogue (asserted by the test above) and one that does not.
func TestADelimitedNameThatIsNotTheBuiltinIsNotTheBuiltin(t *testing.T) {
	q, err := parseQuery(t, "MATCH (n) RETURN `co``unt`(n) AS c")
	require.NoError(t, err)
	require.NotContains(t, modelJSON(t, q), `"kind":"aggregate"`,
		"a name whose decode is not in the catalogue must not match it")

	// The positive control: a delimited name that DOES decode into the
	// catalogue, so the row above cannot pass by every delimited name missing.
	q2, err := parseQuery(t, "MATCH (n) RETURN `count`(n) AS c")
	require.NoError(t, err)
	require.Contains(t, modelJSON(t, q2), `"kind":"aggregate"`,
		"positive control: a delimited name that decodes into the catalogue matches")
}

// TestADelimitedCountIsNotTheStarAggregate pins the ONE place openCypher says
// the delimited spelling does not reach the built-in, and says why that is not
// an exception to the section above.
//
// count(*) is not a function invocation at all. Cypher.g4 §oC_Atom carries it as
// its own alternative, `( COUNT SP? '(' SP? '*' SP? ')' )`, binding the COUNT
// KEYWORD token — and a delimited spelling lexes as EscapedSymbolicName, which
// is a different token that alternative cannot match. So the query below is not
// a count that fails to be recognised; it is an oC_FunctionInvocation whose
// argument list holds '*', and '*' is not an oC_Expression. It does not parse.
//
// That is exactly what the reserved-word appendix predicts — escaping a reserved
// word makes it an identifier, and COUNT's star form needs the keyword — and it
// is what neo4j 5.26.28 answers, measured 2026-09-11: `count`(*) is refused with
// "Invalid input '(': expected an expression", while count(*) beside it returns
// a row.
//
// The row asserts a REFUSAL rather than a classification, because a decode
// written one production too wide — at the token level rather than at
// oC_SymbolicName — would make this parse and silently invent a count(*) the
// grammar has no spelling for.
func TestADelimitedCountIsNotTheStarAggregate(t *testing.T) {
	_, err := parseQuery(t, "MATCH (n) RETURN `count`(*) AS c")
	require.ErrorContains(t, err, "syntax error",
		"`count`(*) is a syntax error, as it is on the server — not a call that "+
			"reaches the catalogue and misses")

	// The positive control beside it, so the row cannot be passing because
	// the harness refuses everything.
	q, err := parseQuery(t, "MATCH (n) RETURN count(*) AS c")
	require.NoError(t, err)
	require.Contains(t, modelJSON(t, q), `"kind":"aggregate"`)
}

// TestTheFunctionScansStillQuoteTheAuthorsText is the guard the bead asked for
// by name, and it is stated here — beside the change — rather than left to the
// consumer's own package.
//
// internal/codegen/age/dialect.go builds its four dialect gaps on
// UnqualifiedFunctionCalls and QualifiedFunctionCalls, and what it does with
// Call.Text is quote it back at the author, so that field must stay the bytes
// that are in the file. Those two scans are an INDEPENDENT reading — functions.go
// carries its own listeners and reaches oC_SymbolicName directly, never through
// shape.go — so decoding the shape.go readers cannot reach them. This row is what
// turns that from a claim about the call graph into a fact about the output, and
// it is what would fail if a later change "tidied" the two readings into one.
func TestTheFunctionScansStillQuoteTheAuthorsText(t *testing.T) {
	calls := cypher.UnqualifiedFunctionCalls("RETURN `date`(), `co``unt`(n)")
	require.Equal(t, []string{"`date`", "`co``unt`"}, callTexts(calls),
		"the fence quotes the author back; a decoded name here would print bytes nobody typed")

	qcalls := cypher.QualifiedFunctionCalls("RETURN `duration`.`between`(a, b)")
	require.Len(t, qcalls, 1)
	require.Equal(t, "`duration`.`between`", qcalls[0].Text)
}
