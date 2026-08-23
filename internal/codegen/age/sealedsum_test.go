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
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/codegen"
	"github.com/areqag/gqlc/internal/graph"
	"github.com/areqag/gqlc/internal/resolver"
	"github.com/areqag/gqlc/internal/schema"
)

// Each embedder promotes one variant's methods, the unexported marker
// included, and declares none of its own. What the rows below depend on is the
// narrower half of that — no String — and no String is what
// TestUnservedColumnFallThroughIsNotANinthVariant/the_embedders_add_no_String_of_their_own
// holds them to, by reading String declarations off this package's own source.
// A method under any other name passes that row, and no row here reads one.
// This file failing to build is itself the claim being falsified: if
// the marker sealed the interface, the compile-time assignments in the table
// below would not typecheck from here.
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

// The constructions compose, which is the step from "more inhabitants" to
// "the arms enumerate declaring types, not arriving shapes" — what
// unservedColumn's fall-through comment says and this file would otherwise only
// assert. Embedding a POINTER promotes the value methods a second time;
// embedding an EMBEDDER promotes what it was itself given. Neither declares a
// method, and each is a distinct type again, so each is one more shape no arm
// names.
type (
	embedNodePointer  struct{ *resolver.ResolvedNode }
	embedNodeEmbedder struct{ embedNode }
)

// shadowEdgeUnion and shadowList are the construction the embedders above are
// chosen to exclude: each embeds a variant AND declares String at depth 0,
// which shadows the promoted one. Promotion only reaches a method no shallower
// declaration hides, so this is where the fall-through's `"projects " +
// t.String()` stops quoting internal/resolver and starts quoting the caller.
//
// They are also the ALLOW half of the_embedders_add_no_String_of_their_own:
// that row's source walk has to find these two before its silence about the
// embedders means anything.
type shadowEdgeUnion struct{ resolver.ResolvedEdgeUnion }

// shadowList is the same construction over the variant whose arm returns the
// fall-through's own expression. It exists because "the list arm and the
// fall-through cannot be told apart" does not follow from their sharing that
// expression: see the row that reads shadowListText.
type shadowList struct{ resolver.ResolvedList }

// shadowListText is deliberately something the list arm does not return: that
// arm answers `"projects " + ct.String()` for resolver.ResolvedList and
// resolver.ResolvedUnknown, and the row reading this const asserts against
// both of those Stringers rather than against a copy of their text.
const shadowListText = "CALLER-CHOSE-THIS"

func (shadowList) String() string { return shadowListText }

// shadowEdgeUnionText is a caller writing out the candidate list
// edgeUnionReason would have produced for probeEdgeUnion — formatLabelList
// spells two labels exactly this way. The full reasons still differ, so this is
// not a collision; what reusing the labels defeats is the plausible shortcut of
// ranking a reason by looking for candidate names in it, which is the shortcut
// the row that reads this const exists to rule out.
const shadowEdgeUnionText = "AUTHORED and LIKES"

func (shadowEdgeUnion) String() string { return shadowEdgeUnionText }

// deeperNode embeds an embedder of one variant and a second variant directly,
// and declares no String. The one it promotes is resolver.ResolvedNode's,
// because promotion takes the shallowest: ResolvedNode's String is one level
// down, and the resolver.ResolvedEdgeUnion one embedEdgeUnion carries is two.
type deeperNode struct {
	embedEdgeUnion
	resolver.ResolvedNode
}

// probeEdgeUnion is a two-candidate union carrying two distinct labels: the
// shape edgeUnionReason answers rather than stands aside from. The labels are
// written out rather than derived, so that a row looking for them in a reason
// compares against a literal this file fixes rather than against whatever
// edgeUnionReason happens to format.
var probeEdgeUnion = resolver.ResolvedEdgeUnion{EdgeKeys: []schema.EdgeKey{
	{Source: "Person", KeyLabels: "AUTHORED", Target: "Post"},
	{Source: "Person", KeyLabels: "LIKES", Target: "Post"},
}}

// inhabitant holds the three forms of one variant that satisfy
// resolver.ResolvedType. value is the form unservedColumn's arms enumerate;
// pointer and embedded are forms an out-of-package caller reaches without
// declaring the marker, and neither matches a `case resolver.Variant:` arm.
//
// valueReason is what unservedColumn answers for the value form. Every row but
// resolver.ResolvedEdgeUnion's writes it out; that one is derived, by calling
// edgeUnionReason — deliberately, and it is the weaker of the two: a change to
// that function's wording moves both sides of the ALLOW assertion together, so
// the row pins the ROUTING (that a value edge union reaches that arm rather
// than the fall-through or a served "") and says nothing about the text.
// Routing is what the ALLOW half is for here; the exact text is pinned by
// wantEdgeUnionReason in age_test.go, which writes it out and which this file
// cannot call, that file being `package age_test` while this one has to be
// `package age` for the reason at the top.
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
// than a refusal either way. That splits the eight three ways, not two.
//
// Five flip: resolver.ResolvedNode, resolver.ResolvedProperty,
// resolver.ResolvedEdge, resolver.ResolvedScalar and resolver.ResolvedTemporal
// are served in their value form, so their rows read "" against a refusal.
//
// Two refuse either way, with a DIFFERENT refusal: resolver.ResolvedEdgeUnion
// and resolver.ResolvedList. For the edge union's value — probeEdgeUnion, two
// candidates under distinct labels — its arm names the candidates the schema
// declares, and for the two forms this row carries the fall-through's
// `"projects " + t.String()` does not, so its rows still tell the arm from the
// fall-through. The arm does not refuse every edge union: fewer than two
// candidates, or a repeated label, returns "" and stands aside for shared
// admission. That difference is the ground
// TestEdgeUnionRankingFlagNamesTheValueFormOnly rests its ruling on. The list
// arm is the same shape for the same reason: since bd gqlc-p6cb it recurses
// into the element and names the one it has no carrier for, so its value row
// reads "projects a list of node" where the fall-through would read "projects
// list".
//
// One refuses either way, and for the value THIS table hands it the refusal is
// character-identical: resolver.ResolvedUnknown. Its arm returns `"projects " +
// ct.String()`, the expression the fall-through also returns, and the Stringer
// it reaches returns a string literal (internal/resolver/validated.go). So on
// that row the text does not say which path produced it. That is a fact about
// that form and not about the arm:
// a_shadowing_list_embedder_is_outside_the_list_arm's_range hands a variant to
// the fall-through under a caller's own String and asserts a text no arm can
// return. Its row here carries the compile-time inhabitation and the refusal;
// which code path produced it is what the arms row covers instead.
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
		// A list of whole vertices. The arm no longer refuses by column
		// kind — it recurses into the ELEMENT (unservedListElement, bd
		// gqlc-p6cb) — so a list of a served element is now served, and
		// the value this row hands over has to be one the arm still
		// refuses or the ALLOW row below asserts nothing about it. The
		// element it refuses for is named in the reason, which is what
		// moves this row out of the pair whose refusal is
		// character-identical to the fall-through's.
		value:       resolver.ResolvedList{Element: resolver.ResolvedNode{}},
		pointer:     &resolver.ResolvedList{Element: resolver.ResolvedNode{}},
		embedded:    embedList{resolver.ResolvedList{Element: resolver.ResolvedNode{}}},
		valueReason: "projects a list of node",
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

// methodReceivers maps each type name that declares a method called `method`
// to the file declaring it, over every .go file in this package's directory.
//
// It reads the directory rather than this file alone because a String declared
// on one of these helpers shadows the promoted one from wherever in package age
// it is written, and the rows that read a promoted String would then be reading
// that method's return instead, with nothing else turning red.
func methodReceivers(t *testing.T, method string) map[string]string {
	t.Helper()

	entries, err := os.ReadDir(".")
	require.NoError(t, err, "reading this package's directory")

	declaredIn := make(map[string]string)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, entry.Name(), nil, 0)
		require.NoErrorf(t, err, "parsing %s", entry.Name())

		for _, decl := range f.Decls {
			fn, isFunc := decl.(*ast.FuncDecl)
			if !isFunc || fn.Recv == nil || len(fn.Recv.List) != 1 || fn.Name.Name != method {
				continue
			}
			recv := fn.Recv.List[0].Type
			if star, isPointer := recv.(*ast.StarExpr); isPointer {
				recv = star.X
			}
			if ident, isIdent := recv.(*ast.Ident); isIdent {
				declaredIn[ident.Name] = entry.Name()
			}
		}
	}
	return declaredIn
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
// type outside internal/resolver can DECLARE isResolvedType, so every declarer
// lives in that package. How many live there is a different question, and not
// one the compiler answers: a ninth variant declared alongside the others
// leaves every row below green, measured. The declared set is held by
// internal/resolver's own counterpart, TestResolvedTypeSumIsNotClosed's
// "declared variants".
//
// What it does not buy: the marker is promoted as well as declared. A pointer
// to a variant carries it, and so does a struct embedding one; neither matches
// an arm. So the arms are not the interface's membership, and reaching the
// fall-through takes no ninth variant: every refusal row below gets there with
// a variant the arms already name.
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
			// The fall-through's exact text. Asserting the text rather
			// than only non-emptiness separates "reached the
			// fall-through" from "reached an arm that also refuses" on
			// one of the three rows where that distinction arises, not
			// on all eight. Three have an arm that refuses the value
			// this table hands it — the 2 and the 1 of the partition
			// above: resolver.ResolvedEdgeUnion, resolver.ResolvedList
			// and resolver.ResolvedUnknown. The edge-union arm answers
			// with the candidate list and the list arm with the element
			// it has no carrier for, neither of which `"projects " +
			// String()` carries, so on those two rows the text does
			// separate the two paths. The unknown arm builds its text
			// from the expression this row asserts, so on that one it
			// does not. The other five have an arm that serves, which
			// non-emptiness alone would have separated.
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

	// The refusal on its own: an unrecognised shape is dropped with a reason
	// rather than served. Strictly weaker than the two rows above — every
	// `"projects " + String()` they pin is non-empty, so this row cannot RED
	// while they are green. It is here for what it still says once the
	// fall-through's wording changes, which is when those two stop saying
	// anything until they are rewritten: the shape is refused, whatever the
	// reason reads. It does not witness WHICH path refused — a non-empty reason
	// is equally consistent with an arm.
	t.Run("no non-value form is served", func(t *testing.T) {
		for _, name := range sortedInhabitantNames() {
			in := inhabitants[name]
			require.NotEmptyf(t, unservedColumn(in.pointer), "*%s was served", name)
			require.NotEmptyf(t, unservedColumn(in.embedded), "struct{ resolver.%s } was served", name)
		}
	})

	// What refusing at the fall-through buys, held to the half that is a fact
	// about THIS tree. The fall-through's comment says the refusal is the
	// diagnostic and not the prevention of a wrong emission, and its evidence
	// is a counterfactual — that line returning "" instead — which no row in
	// this package can run. What a row CAN run is the reason the
	// counterfactual holds: codegen.Prepare refuses these same shapes on its
	// own, so serving them here would move which gate answers and not whether
	// one does. Every non-value form goes to Prepare directly as a column
	// type and comes back ErrOutOfC6Scope, spelling the arriving type's own
	// String — the same expression the fall-through reaches, from a switch
	// that is not this one. Prepare is entered the way generate.go:46 enters
	// it, so this is the gate behind the fall-through and not a third one.
	//
	// phaseAAdmit is what answers: Prepare returns on Phase A before reaching
	// phaseBDerive, whose own default is labelled for a Phase A miss. The
	// message this row pins is phaseAAdmit's, which is what makes that
	// ordering observable rather than asserted.
	t.Run("codegen.Prepare refuses the same shapes on its own", func(t *testing.T) {
		for _, name := range sortedInhabitantNames() {
			in := inhabitants[name]
			for _, form := range []struct {
				spelling string
				typ      resolver.ResolvedType
			}{
				{"*resolver." + name, in.pointer},
				{"struct{ resolver." + name + " }", in.embedded},
			} {
				_, err := codegen.Prepare(codegen.Input{
					Schema: schemaWithPayload(graph.TypeString),
					Queries: []codegen.NamedQuery{{
						Name:        "Q",
						Cardinality: codegen.CardinalityMany,
						SourceFile:  "q.cypher",
						SourceText:  "MATCH (b:Blob) RETURN b AS c\n",
						Validated: resolver.ValidatedQuery{
							Columns: []resolver.Column{{Name: "c", Type: form.typ}},
						},
					}},
				}, typeMap{}, "age")
				require.ErrorIsf(t, err, codegen.ErrOutOfC6Scope,
					"%s must be refused by codegen.Prepare's own column switch, which names the same variants unservedColumn's arms do, so a shape matching no arm here matches none there",
					form.spelling)
				require.EqualErrorf(t, err,
					`out of C6 scope: query "Q" column 0 "c" resolved as `+form.typ.String(),
					"%s: Prepare's refusal must spell the arriving type's own String and name no backend",
					form.spelling)
			}
		}
	})

	// The nesting the comment rests "no bound" on. Two levels is not a proof
	// that the set is infinite; what it shows is that the constructions
	// compose, so no enumeration of forms closes the way an enumeration of
	// declaring types does.
	t.Run("the constructions nest", func(t *testing.T) {
		nested := map[string]resolver.ResolvedType{
			"struct{ *resolver.ResolvedNode }": embedNodePointer{&resolver.ResolvedNode{}},
			"struct{ embedNode }":              embedNodeEmbedder{},
		}
		for _, spelling := range []string{"struct{ *resolver.ResolvedNode }", "struct{ embedNode }"} {
			require.Equalf(t, "projects node", unservedColumn(nested[spelling]),
				"%s satisfies resolver.ResolvedType and must reach unservedColumn's fall-through", spelling)
		}
	})

	// The other shape of the fall-through's text. The forms above declare no
	// String — held to that by the_embedders_add_no_String_of_their_own below
	// — so the variant's own diagnostic Stringer stays shallowest and answers.
	// A shadowing String reaches the same fall-through and puts the caller's
	// text there instead.
	t.Run("a shadowing embedder chooses the text after projects", func(t *testing.T) {
		shadow := shadowEdgeUnion{probeEdgeUnion}
		require.Equal(t, "projects "+shadowEdgeUnionText, unservedColumn(shadow),
			"struct{ resolver.ResolvedEdgeUnion } declaring its own String reaches the fall-through, which returns that String")
		require.NotEqual(t, "projects "+probeEdgeUnion.String(), unservedColumn(shadow),
			"the shadowing String must differ from the promoted one, or this row witnesses nothing")
	})

	// The ResolvedUnknown row above cannot say which path answered it, because
	// its arm returns the expression the fall-through returns. That is a fact
	// about that row's form, and this row is why it does not generalise into
	// "the arm and the fall-through are indistinguishable": what a variant's
	// own arm can return is "projects " and then that variant's Stringer. The
	// assertions below are written against the two Stringers a list-shaped
	// embedder could reach, so they say "outside what those arms return"
	// whatever internal/resolver makes them say, and they hold.
	t.Run("a shadowing list embedder is outside the list arm's range", func(t *testing.T) {
		require.Equal(t, "projects "+shadowListText, unservedColumn(shadowList{}),
			"struct{ resolver.ResolvedList } declaring its own String reaches the fall-through, which returns that String")
		require.NotEqual(t, "projects "+resolver.ResolvedList{}.String(), unservedColumn(shadowList{}),
			"the fall-through returns this for a bare list, so a row equal to it would witness nothing")
		require.NotEqual(t, "projects "+resolver.ResolvedUnknown{}.String(), unservedColumn(shadowList{}),
			"the unknown arm returns this")
	})

	// The key set is held against the arms below, and that says nothing about
	// what a row CARRIES. With every key left alone, all eight rows could hold
	// one variant's three forms and every row above would stay green while
	// measuring that variant eight times. This is what makes "each of the
	// eight variants" a property of the table and not only of its keys.
	t.Run("each row carries the variant its key names", func(t *testing.T) {
		for _, name := range sortedInhabitantNames() {
			in := inhabitants[name]
			require.Equalf(t, name, reflect.TypeOf(in.value).Name(),
				"%s: the value form is a %T", name, in.value)
			require.Equalf(t, name, reflect.TypeOf(in.pointer).Elem().Name(),
				"%s: the pointer form points at a %T", name, in.pointer)
			embedded := reflect.TypeOf(in.embedded)
			require.Equalf(t, 1, embedded.NumField(),
				"%s: the embedded form %T has %d fields; these rows read the promoted variant off a single embedded one",
				name, in.embedded, embedded.NumField())
			require.Equalf(t, name, embedded.Field(0).Type.Name(),
				"%s: the embedded form embeds a %s", name, embedded.Field(0).Type)
		}
	})

	// The pointer and embedded rows assert `"projects " + form.String()`,
	// whose two sides move together when the embedder declares its own String:
	// making embedScalar, embedEdge and embedList each declare one left the
	// whole package green, 0 failing subtests, before this row existed. So
	// the top of this file saying the embedders declare no String was prose
	// nothing held. This row holds it off the source, where a declaration's
	// absence shows even when its text would not — a shadow returning the
	// promoted text changes no behaviour to observe.
	//
	// the_constructions_nest asserts a literal instead and does redden for the
	// form it carries, which is why that row is not enough on its own.
	t.Run("the embedders add no String of their own", func(t *testing.T) {
		declaredIn := methodReceivers(t, "String")

		// The ALLOW half, and the proof the walk is live: it must find the two
		// types this file declares a String on. Without it an empty map — a
		// walk that read nothing, or read the wrong directory — satisfies
		// every assertion below.
		require.Containsf(t, declaredIn, "shadowEdgeUnion",
			"the walk found no String on shadowEdgeUnion, which this file declares one on, so its silence about the embedders witnesses nothing; it read %v", declaredIn)
		require.Containsf(t, declaredIn, "shadowList",
			"the walk found no String on shadowList, which this file declares one on; it read %v", declaredIn)

		// Derived from the table rather than written out again, so a variant
		// added to inhabitants is covered without editing a second list.
		forms := []string{"embedNodePointer", "embedNodeEmbedder", "deeperNode"}
		for _, name := range sortedInhabitantNames() {
			forms = append(forms, reflect.TypeOf(inhabitants[name].embedded).Name())
		}
		for _, form := range forms {
			require.NotContainsf(t, declaredIn, form,
				"%s declares its own String (in %s), which shadows the promoted one; every row reading %s's text is then asserting that method's return rather than the embedded variant's, and would stay green while measuring nothing",
				form, declaredIn[form], form)
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
	// The row reads two mechanisms by name, not the sentence around them: a
	// rewrite that keeps naming the pointer form and the embedding still
	// passes, and one that drops either stops being the comment the rows above
	// witness. It does not police any other wording, and it is not a claim
	// that these two are all there is — embedding resolver.ResolvedType itself
	// reaches the same line and embeds no variant. These two are what the rows
	// above measure, so these two are what this row requires the comment to
	// keep.
	t.Run("the fall-through comment names the pointer form and the embedding", func(t *testing.T) {
		comment := fallThroughComment(t)
		require.Containsf(t, comment, "pointer",
			"unservedColumn's fall-through comment does not name the pointer form, which the pointer rows above measure: %q", comment)
		require.Containsf(t, comment, "embed",
			"unservedColumn's fall-through comment does not name the embedding, which the embedded rows above measure: %q", comment)
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
// else, so the assertion recognises exactly the arm that calls edgeUnionReason.
// A pointer or embedded edge union reaches the fall-through instead. The two
// such forms below declare no String, so the promoted one answers and their
// reason is "projects edgeUnion" — it names no candidate, so it says LESS than
// the alternation the text gate quotes back, and promoting it over the text
// would make the author worse off in precisely the trade the exception to the
// yield exists to avoid.
//
// The last row is the shape that stops this being an argument about wording. An
// embedder may declare its own String, shadowing the promoted one, and its
// reason then carries whatever that method returns — the candidate names
// included, with edgeUnionReason never called. A flag that read the rank off the
// reason's text would hand it to that string. Reading the column's type is what
// confines the rank to answers edgeUnionReason actually gave, and that row holds
// the difference.
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
				"this form declares no String, so its fall-through reason is the promoted tag and names no candidate — which is why yielding to the text costs nothing here")
			require.NotContains(t, reason, "LIKES")
			// Still refused, not served — the degradation ruled acceptable is
			// in the ranking only.
			require.NotEmpty(t, reason)
		})
	}

	// The bound on the rows above is the two forms that slice carries: a
	// pointer to probeEdgeUnion, and a struct embedding it and declaring no
	// String. Here the reason names both candidates without edgeUnionReason
	// having run, so the reason's wording is not evidence of which path
	// produced it. The flag reads the type and is unmoved; a flag matching text
	// would have been moved by a caller's method.
	t.Run("a shadowing embedder does not outrank, whatever its reason reads", func(t *testing.T) {
		reason, edgeUnion := unservedReason(probeColumnQuery(shadowEdgeUnion{probeEdgeUnion}))
		require.False(t, edgeUnion,
			"the flag is a type assertion on the value form, so an embedder does not set it however its String reads")
		require.Equal(t, `column "r" projects `+shadowEdgeUnionText, reason)
		require.Contains(t, reason, "AUTHORED",
			"this row is only a witness if the fall-through reason does name a candidate")
		require.NotEmpty(t, reason)
	})

	// The other half of that bound, and the reason it is stated as two forms
	// rather than as "forms that add no String": adding none does not by itself
	// produce `projects edgeUnion`. deeperNode embeds an edge union and adds no
	// String, and the reason still names no candidate — for a different cause,
	// a shallower Stringer rather than a shadowing one. The ruling is unchanged
	// either way, which is the point: it rests on the flag reading the type.
	t.Run("adding no String is not what those two rows turn on", func(t *testing.T) {
		reason, edgeUnion := unservedReason(probeColumnQuery(deeperNode{}))
		require.False(t, edgeUnion,
			"deeperNode is not resolver.ResolvedEdgeUnion, so the type assertion does not fire")
		require.Equal(t, `column "r" projects node`, reason,
			"deeperNode declares no String and embeds an edge union, and still does not answer `projects edgeUnion`: promotion took resolver.ResolvedNode's String, which is a level shallower than the one embedEdgeUnion carries")
		require.NotContains(t, reason, "AUTHORED")
	})
}
