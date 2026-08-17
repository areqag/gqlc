// This file is `package age` rather than `package age_test`, and both halves
// of that choice are load-bearing. The claim under test is about what a
// package OUTSIDE internal/resolver can construct, and internal/codegen/age is
// such a package — isResolvedType is unexported to internal/resolver, so this
// package cannot declare it and a witness declared here measures the marker at
// full strength. Being inside internal/codegen/age is what puts the question
// to unservedColumn and unservedReason directly, which is where the comments
// under test are; asking it through Generate would measure the batch gate's
// wording instead of the two functions'.
//
// The counterpart at internal/resolver/sealedsum_test.go has to be
// `resolver_test` for the reason this file does not: inside internal/resolver
// the marker is writable, so an in-package witness there would measure
// nothing.
package age

import (
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/codegen"
	"github.com/areqag/gqlc/internal/graph"
	"github.com/areqag/gqlc/internal/resolver"
	"github.com/areqag/gqlc/internal/schema"
)

// Each embedder promotes one variant's methods, the unexported marker
// included, and declares no method of its own. This file failing to build is
// itself the claim being falsified: if the marker sealed the interface, the
// compile-time assignments in the table below would not typecheck from here.
type (
	embedNode      struct{ resolver.ResolvedNode }
	embedProperty  struct{ resolver.ResolvedProperty }
	embedEdge      struct{ resolver.ResolvedEdge }
	embedEdgeUnion struct{ resolver.ResolvedEdgeUnion }
	embedScalar    struct{ resolver.ResolvedScalar }
	embedTemporal  struct{ resolver.ResolvedTemporal }
	embedList      struct{ resolver.ResolvedList }
	embedUnknown   struct{ resolver.ResolvedUnknown }
)

// probeEdgeUnion is a two-candidate union carrying two distinct labels: the
// shape edgeUnionReason answers rather than stands aside from. Its labels are
// what the "names no candidate" rows below look for and fail to find in the
// non-value forms' reasons, so they are spelled here once and asserted by
// name.
var probeEdgeUnion = resolver.ResolvedEdgeUnion{EdgeKeys: []schema.EdgeKey{
	{Source: "Person", KeyLabels: "AUTHORED", Target: "Post"},
	{Source: "Person", KeyLabels: "LIKES", Target: "Post"},
}}

// inhabitant holds the three forms of one variant that satisfy
// resolver.ResolvedType. value is the form unservedColumn's arms enumerate;
// pointer and embedded are the two an out-of-package caller reaches without
// declaring the marker, and neither matches a `case resolver.Variant:` arm.
//
// valueReason is what unservedColumn answers for the value form. It is
// written out per row rather than derived, because deriving it from the
// function under test is what the ALLOW half exists to avoid.
type inhabitant struct {
	value       resolver.ResolvedType
	pointer     resolver.ResolvedType
	embedded    resolver.ResolvedType
	valueReason string
}

// inhabitants is keyed by the variant name internal/resolver declares. The key
// set is held against unservedColumn's own arms by
// TestUnservedColumnFallThroughIsNotANinthVariant/the_arms_and_this_table_name_the_same_variants,
// so a ninth variant with an arm, or an arm removed, fails there rather than
// silently narrowing every row below.
//
// The value forms are chosen to be SERVED wherever an arm serves anything, so
// that the pointer and embedded rows record a flip from "" to a refusal rather
// than a refusal either way. Six of the eight flip. resolver.ResolvedList and
// resolver.ResolvedUnknown do not, and cannot: their arms return
// `"projects " + ct.String()`, the same text the fall-through returns, so no
// assertion on unservedColumn's result can tell those two arms from the
// fall-through. Their rows carry the compile-time inhabitation and the
// refusal; which code path produced it is what the arms row covers instead.
var inhabitants = map[string]inhabitant{
	"ResolvedNode": {
		value:    resolver.ResolvedNode{},
		pointer:  &resolver.ResolvedNode{},
		embedded: embedNode{},
		// A whole vertex: label and properties together fill the entity
		// struct the schema declares.
		valueReason: "",
	},
	"ResolvedProperty": {
		value:       resolver.ResolvedProperty{Type: graph.TypeString},
		pointer:     &resolver.ResolvedProperty{Type: graph.TypeString},
		embedded:    embedProperty{resolver.ResolvedProperty{Type: graph.TypeString}},
		valueReason: "",
	},
	"ResolvedEdge": {
		value:       resolver.ResolvedEdge{},
		pointer:     &resolver.ResolvedEdge{},
		embedded:    embedEdge{},
		valueReason: "",
	},
	"ResolvedEdgeUnion": {
		value:    probeEdgeUnion,
		pointer:  &probeEdgeUnion,
		embedded: embedEdgeUnion{probeEdgeUnion},
		// The one arm that refuses with something other than the width
		// vocabulary: it names the candidates the schema declares.
		valueReason: edgeUnionReason(probeEdgeUnion),
	},
	"ResolvedScalar": {
		value:       resolver.ResolvedScalar{Kind: resolver.ScalarBool},
		pointer:     &resolver.ResolvedScalar{Kind: resolver.ScalarBool},
		embedded:    embedScalar{resolver.ResolvedScalar{Kind: resolver.ScalarBool}},
		valueReason: "",
	},
	"ResolvedTemporal": {
		value:    resolver.ResolvedTemporal{Kind: resolver.TemporalDate},
		pointer:  &resolver.ResolvedTemporal{Kind: resolver.TemporalDate},
		embedded: embedTemporal{resolver.ResolvedTemporal{Kind: resolver.TemporalDate}},
		// Answered by the type table instead — see the arm.
		valueReason: "",
	},
	"ResolvedList": {
		value:       resolver.ResolvedList{Element: resolver.ResolvedUnknown{}},
		pointer:     &resolver.ResolvedList{Element: resolver.ResolvedUnknown{}},
		embedded:    embedList{resolver.ResolvedList{Element: resolver.ResolvedUnknown{}}},
		valueReason: "projects list",
	},
	"ResolvedUnknown": {
		value:       resolver.ResolvedUnknown{},
		pointer:     &resolver.ResolvedUnknown{},
		embedded:    embedUnknown{},
		valueReason: "projects unknown",
	},
}

func sortedInhabitantNames() []string {
	out := make([]string, 0, len(inhabitants))
	for name := range inhabitants {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// unservedColumnArms returns the sorted variant names unservedColumn's type
// switch spells in its `case` clauses, read off this package's own source.
//
// It walks the AST rather than grepping the source, because a commented-out
// `case resolver.ResolvedNinth:` satisfies a grep — which would let the
// enumeration above drift behind a comment and report agreement with a switch
// that does not have the arm.
func unservedColumnArms(t *testing.T) []string {
	t.Helper()

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "errors.go", nil, parser.ParseComments)
	require.NoError(t, err, "parsing errors.go")

	fn := funcDecl(t, f, "unservedColumn")

	var switches []*ast.TypeSwitchStmt
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if ts, isTypeSwitch := n.(*ast.TypeSwitchStmt); isTypeSwitch {
			switches = append(switches, ts)
		}
		return true
	})
	// Exactly one, because more than one would make "the arms" ambiguous and
	// reading the first would compare this table against half a switch.
	require.Lenf(t, switches, 1,
		"unservedColumn holds %d type switches; the rows below are written against one", len(switches))

	var arms []string
	for _, stmt := range switches[0].Body.List {
		clause, isCase := stmt.(*ast.CaseClause)
		require.Truef(t, isCase, "unexpected %T in unservedColumn's type switch body", stmt)
		// A default clause carries no types and contributes nothing; that is
		// the fall-through this file is about, not an arm.
		for _, expr := range clause.List {
			sel, isSelector := expr.(*ast.SelectorExpr)
			require.Truef(t, isSelector,
				"%s: unservedColumn names %T in a case clause; only qualified resolver.X types are read here",
				fset.Position(expr.Pos()), expr)
			pkg, isIdent := sel.X.(*ast.Ident)
			require.Truef(t, isIdent, "%s: unexpected qualifier %T", fset.Position(expr.Pos()), sel.X)
			require.Equalf(t, "resolver", pkg.Name,
				"%s: unservedColumn names %s.%s; the table below enumerates resolver variants only",
				fset.Position(expr.Pos()), pkg.Name, sel.Sel.Name)
			arms = append(arms, sel.Sel.Name)
		}
	}
	sort.Strings(arms)
	return arms
}

// fallThroughComment returns the comment attached to unservedColumn's final
// statement — the fall-through return — as one string.
func fallThroughComment(t *testing.T) string {
	t.Helper()

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "errors.go", nil, parser.ParseComments)
	require.NoError(t, err, "parsing errors.go")

	fn := funcDecl(t, f, "unservedColumn")
	require.NotEmpty(t, fn.Body.List, "unservedColumn has an empty body")

	last := fn.Body.List[len(fn.Body.List)-1]
	_, isReturn := last.(*ast.ReturnStmt)
	require.Truef(t, isReturn,
		"%s: unservedColumn's last statement is %T, not the fall-through return this row reads the comment off",
		fset.Position(last.Pos()), last)

	groups := ast.NewCommentMap(fset, fn, f.Comments)[last]
	require.NotEmptyf(t, groups,
		"%s: unservedColumn's fall-through return carries no comment", fset.Position(last.Pos()))

	var text string
	for _, g := range groups {
		text += g.Text()
	}
	return text
}

func funcDecl(t *testing.T, f *ast.File, name string) *ast.FuncDecl {
	t.Helper()

	var found *ast.FuncDecl
	for _, decl := range f.Decls {
		fn, isFunc := decl.(*ast.FuncDecl)
		if !isFunc || fn.Recv != nil || fn.Name.Name != name {
			continue
		}
		require.Nilf(t, found, "errors.go declares %s more than once", name)
		found = fn
	}
	require.NotNilf(t, found, "errors.go declares no %s", name)
	require.NotNilf(t, found.Body, "%s has no body", name)
	return found
}

// TestUnservedColumnFallThroughIsNotANinthVariant measures what
// resolver.ResolvedType's unexported marker does and does not buy at this
// gate, and is what unservedColumn's fall-through comment cites.
//
// What it buys, and what has no row here because the compiler enforces it: no
// type outside internal/resolver can DECLARE isResolvedType, so the eight
// variants are the whole set of declaring types.
//
// What it does not buy: two constructions obtain the marker without declaring
// it — the pointer form of a variant, and a struct embedding one — and neither
// matches an arm. So the arms are not the interface's membership, and reaching
// the fall-through takes no ninth variant: every refusal row below gets there
// with a variant the arms already name.
func TestUnservedColumnFallThroughIsNotANinthVariant(t *testing.T) {
	// The ALLOW half. Without it every refusal row below is satisfied by an
	// unservedColumn that refuses everything handed to it.
	t.Run("value form is answered by its own arm", func(t *testing.T) {
		for _, name := range sortedInhabitantNames() {
			in := inhabitants[name]
			require.Equalf(t, in.valueReason, unservedColumn(in.value),
				"%s in its value form must be answered by the arm naming it", name)
		}
	})

	t.Run("pointer form inhabits the interface and reaches the fall-through", func(t *testing.T) {
		for _, name := range sortedInhabitantNames() {
			in := inhabitants[name]
			require.NotNilf(t, in.pointer, "%s: no pointer form enumerated", name)
			// The fall-through's exact text. Asserting the text rather than
			// only non-emptiness is what separates "reached the fall-through"
			// from "reached some arm that also refuses": for six of the eight
			// the arm answers "" instead.
			require.Equalf(t, "projects "+in.pointer.String(), unservedColumn(in.pointer),
				"*%s must reach unservedColumn's fall-through; every marker and String on the variants takes a value receiver, and a pointer's method set contains its value methods, so the pointer satisfies resolver.ResolvedType while `case resolver.%s:` does not match it",
				name, name)
		}
	})

	t.Run("embedded form inhabits the interface and reaches the fall-through", func(t *testing.T) {
		for _, name := range sortedInhabitantNames() {
			in := inhabitants[name]
			require.NotNilf(t, in.embedded, "%s: no embedding form enumerated", name)
			require.Equalf(t, "projects "+in.embedded.String(), unservedColumn(in.embedded),
				"struct{ resolver.%s } declared in this package must reach unservedColumn's fall-through; Go promotes an embedded type's unexported methods, so it satisfies resolver.ResolvedType without naming the marker, and it is a distinct type so `case resolver.%s:` does not match it",
				name, name)
		}
	})

	// The refusal, which is the half the fall-through's behaviour rests on: an
	// unrecognised shape is dropped with a reason, not served through an arm
	// chosen for some other shape. Stated separately from the text rows above
	// because it is the property the comment argues, and it survives a change
	// to the wording of the reason.
	t.Run("no non-value form is served", func(t *testing.T) {
		for _, name := range sortedInhabitantNames() {
			in := inhabitants[name]
			require.NotEmptyf(t, unservedColumn(in.pointer), "*%s was served", name)
			require.NotEmptyf(t, unservedColumn(in.embedded), "struct{ resolver.%s } was served", name)
		}
	})

	t.Run("the arms and this table name the same variants", func(t *testing.T) {
		require.Equal(t, sortedInhabitantNames(), unservedColumnArms(t),
			"unservedColumn's case arms and the set this file enumerates have diverged. A name in the arms and not in inhabitants means a variant landed: add its three forms and its valueReason, or the rows above measure a switch smaller than the one shipping. A name in inhabitants and not in the arms means an arm left: drop the stale entry")
	})

	// The comment is the artefact this bead fixed, so it is pinned too — a
	// revert to "ResolvedType is a sealed interface, so the switch above is
	// its whole membership" is exactly what every row above falsifies, and
	// nothing else here would notice the prose going back.
	//
	// The row reads the two mechanisms by name, not the sentence around them:
	// a rewrite that keeps naming the pointer form and the embedding still
	// passes, and one that drops either stops being the comment the rows above
	// witness. It does not police any other wording.
	t.Run("the fall-through comment names both constructions", func(t *testing.T) {
		comment := fallThroughComment(t)
		require.Containsf(t, comment, "pointer",
			"unservedColumn's fall-through comment does not name the pointer form, which is one of the two constructions that reaches it: %q", comment)
		require.Containsf(t, comment, "embed",
			"unservedColumn's fall-through comment does not name the embedding, which is the other: %q", comment)
	})
}

// probeColumnQuery is a one-column read whose column carries t. Nothing else
// on the query is unserved, so a reason from unservedReason can only be the
// column's.
func probeColumnQuery(t resolver.ResolvedType) codegen.NamedQuery {
	return codegen.NamedQuery{
		Name:        "Actions",
		Cardinality: codegen.CardinalityMany,
		SourceText:  "MATCH (:Person)-[r:AUTHORED|LIKES]->(:Post) RETURN r\n",
		Validated: resolver.ValidatedQuery{
			Columns: []resolver.Column{{Name: "r", Type: t}},
		},
	}
}

// TestEdgeUnionRankingFlagNamesTheValueFormOnly records the ruling on the
// edgeUnion flag unservedReason returns, which rejectUnservedQueries reads to
// decide whether a column's reason outranks the text gate's.
//
// The assertion producing that flag names the value form only, and the ruling
// is that this is correct rather than a gap to close. The flag's contract is
// "this reason came from edgeUnionReason", because that is the reason that
// earns the rank: it names the candidates the schema declares for the pattern,
// which the text gate cannot say. unservedColumn reaches edgeUnionReason from
// `case resolver.ResolvedEdgeUnion:`, which matches the value form and nothing
// else, so the assertion and the arm agree exactly. A pointer or embedded edge
// union reaches the fall-through instead, whose reason is "projects edgeUnion"
// — it names no candidate, so it says strictly LESS than the alternation the
// text gate quotes back, and promoting it over the text would make the author
// worse off in precisely the trade the exception to the yield exists to avoid.
//
// The false rows are therefore load-bearing: widening the assertion to
// recognise the non-value forms reddens them, and is meant to.
func TestEdgeUnionRankingFlagNamesTheValueFormOnly(t *testing.T) {
	// The ALLOW half. Without it a flag hardwired to false passes every row
	// below.
	t.Run("a value edge-union column outranks", func(t *testing.T) {
		reason, edgeUnion := unservedReason(probeColumnQuery(probeEdgeUnion))
		require.True(t, edgeUnion,
			"a value resolver.ResolvedEdgeUnion column is the one axis whose answer outranks the text gate's")
		require.Contains(t, reason, "binds more than one relationship type")
		require.Contains(t, reason, "AUTHORED", "the rank is earned by naming the candidates")
		require.Contains(t, reason, "LIKES")
	})

	nonValue := []struct {
		name string
		form resolver.ResolvedType
	}{
		{"a pointer edge-union column does not outrank", &probeEdgeUnion},
		{"an embedded edge-union column does not outrank", embedEdgeUnion{probeEdgeUnion}},
	}
	for _, tc := range nonValue {
		t.Run(tc.name, func(t *testing.T) {
			reason, edgeUnion := unservedReason(probeColumnQuery(tc.form))
			require.Falsef(t, edgeUnion,
				"%T reaches unservedColumn's fall-through rather than edgeUnionReason, so its reason is not the one that outranks the text",
				tc.form)
			// The ground the ruling rests on, asserted rather than assumed:
			// the reason really does say less. If a future edit routed the
			// non-value forms to edgeUnionReason, these rows would fail here
			// first, and flipping the flag would then be right.
			require.Equal(t, `column "r" projects edgeUnion`, reason)
			require.NotContains(t, reason, "AUTHORED",
				"the fall-through reason names no candidate, which is why it does not outrank the text")
			require.NotContains(t, reason, "LIKES")
			// Still refused, not served — the degradation ruled acceptable is
			// in the ranking only.
			require.NotEmpty(t, reason)
		})
	}
}
