package codegen_test

import (
	"go/ast"
	"go/parser"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/codegen"
	"github.com/areqag/gqlc/internal/graph"
)

// stubFamily is a wire-family table with neither backend's shape: INT32
// and INT64 share "i", STRING and DATE are each their own, and ANY is the
// tag that collides with everything. Written rather than borrowed so these
// rows keep asserting the WALK when a backend's own table changes, which
// is the whole reason the walk lives here and the tables do not.
func stubFamily(goType string) string {
	switch goType {
	case "int32", "int64":
		return "i"
	case "string":
		return "s"
	case "Date":
		return "d"
	case "any":
		return codegen.WireFamilyIndistinct
	}
	return "unknown"
}

// stubCarrier is a carrier table narrow enough to have a refusal in it:
// DECIMAL has no row, which is how these rows reach the SKIPPED path
// without needing a backend.
func stubCarrier(pt graph.PropertyType) (string, bool) {
	// Recursive on a nested union for the reason both real backends are:
	// the member arm is the same table, so a NOT NULL nested union — the
	// one shape UnionOf does not flatten — carries `any` and lands in the
	// indistinct family, and the rows below can put that family on the
	// LATE side of a pair, which TypeAnyPropertyValue cannot.
	if pt.Kind() == graph.KindUnion {
		return codegen.UnionCarrier(pt, stubCarrier, stubFamily)
	}
	text, ok := stubCarriers[pt]
	return text, ok
}

// A map rather than a switch on pt, and not only for brevity: the
// exhaustive linter reads a switch over a PropertyType as an obligation
// to name every declared width, which is the opposite of what a stub with
// a deliberate hole in it is for.
var stubCarriers = map[graph.PropertyType]string{
	graph.TypeInt32:            "int32",
	graph.TypeInt64:            "int64",
	graph.TypeString:           "string",
	graph.TypeDate:             "Date",
	graph.TypeAnyPropertyValue: "any",
}

// TestUnionMemberCollisionFindsThePairOneFamilyHolds pins the admission
// rule's positive answer (spec §4) and, as much as the verdict, WHICH pair
// it names. A refusal message quotes the pair, so a walk that found some
// other colliding pair would produce a message pointing the author at the
// wrong member to drop.
func TestUnionMemberCollisionFindsThePairOneFamilyHolds(t *testing.T) {
	t.Run("two members of one family are the pair", func(t *testing.T) {
		pt := graph.UnionOf([]graph.UnionMember{{Type: graph.TypeInt32}, {Type: graph.TypeInt64}})
		a, b, collides := codegen.UnionMemberCollision(pt, stubCarrier, stubFamily)
		require.True(t, collides)
		// Canonical order is the members' own, which UnionOf sorts by
		// encoded spelling: INT32 before INT64.
		require.Equal(t, graph.TypeInt32, a)
		require.Equal(t, graph.TypeInt64, b)
	})

	t.Run("the pair is named in the members' own canonical order", func(t *testing.T) {
		// Declared later-first, so a walk reporting in DECLARATION order
		// would answer the reverse pair. UnionOf sorts, so the answer is a
		// function of the encoding rather than of how the author typed it.
		pt := graph.UnionOf([]graph.UnionMember{{Type: graph.TypeInt64}, {Type: graph.TypeInt32}})
		a, b, collides := codegen.UnionMemberCollision(pt, stubCarrier, stubFamily)
		require.True(t, collides)
		require.Equal(t, graph.TypeInt32, a)
		require.Equal(t, graph.TypeInt64, b)
	})

	t.Run("distinct families collide on no pair", func(t *testing.T) {
		pt := graph.UnionOf([]graph.UnionMember{{Type: graph.TypeString}, {Type: graph.TypeDate}})
		a, b, collides := codegen.UnionMemberCollision(pt, stubCarrier, stubFamily)
		require.False(t, collides)
		require.Empty(t, a)
		require.Empty(t, b)
	})

	t.Run("a lone member is no pair", func(t *testing.T) {
		pt := graph.UnionOf([]graph.UnionMember{{Type: graph.TypeInt32, NotNull: true}})
		require.Equal(t, graph.KindUnion, pt.Kind())
		_, _, collides := codegen.UnionMemberCollision(pt, stubCarrier, stubFamily)
		require.False(t, collides)
	})
}

// TestWireFamilyIndistinctCollidesWithEverything holds the property that
// forces the walk to be pairwise rather than a map keyed on the family
// tag. "One family" is not an equivalence relation here: the indistinct
// tag collides with every other tag AND with itself, and a map keyed on
// the tag can only express a relation that is transitive.
func TestWireFamilyIndistinctCollidesWithEverything(t *testing.T) {
	for _, other := range []graph.PropertyType{graph.TypeInt32, graph.TypeString, graph.TypeDate} {
		t.Run(string(other), func(t *testing.T) {
			pt := graph.UnionOf([]graph.UnionMember{
				{Type: graph.TypeAnyPropertyValue, NotNull: true}, {Type: other},
			})
			require.Equal(t, graph.KindUnion, pt.Kind())
			_, _, collides := codegen.UnionMemberCollision(pt, stubCarrier, stubFamily)
			require.True(t, collides, "a member that arrives as any shape leaves no shape for a member beside it")
		})
	}

	t.Run("the tag is read on either side of the pair", func(t *testing.T) {
		// The rows above all put the indistinct tag on the EARLY side of
		// the pair, and not by choice: members sort by encoded spelling
		// and "ANY NOT NULL" sorts before every other width gqlc has. So
		// a walk that tested only tags[i] would pass all of them.
		early := graph.UnionOf([]graph.UnionMember{
			{Type: graph.TypeAnyPropertyValue, NotNull: true}, {Type: graph.TypeString},
		})
		require.Equal(t, graph.TypeAnyPropertyValue, early.Members()[0].Type,
			"the rows above put the indistinct tag on the EARLY side, which is what this row exists to complement")

		// A NOT NULL nested union is the shape that reaches the late side:
		// UnionOf does not flatten it, it carries `any`, and "UNION<...>
		// NOT NULL" sorts after DATE.
		nested := graph.UnionOf([]graph.UnionMember{{Type: graph.TypeInt32}, {Type: graph.TypeString}})
		require.Equal(t, graph.KindUnion, nested.Kind())
		late := graph.UnionOf([]graph.UnionMember{
			{Type: nested, NotNull: true}, {Type: graph.TypeDate},
		})
		require.Equal(t, graph.TypeDate, late.Members()[0].Type,
			"this row puts the indistinct tag on the LATE side, so a walk reading only the early tag fails it")
		_, a, collides := codegen.UnionMemberCollision(late, stubCarrier, stubFamily)
		require.True(t, collides)
		require.Equal(t, nested, a, "the pair names the nested union as its later half")
	})
}

// TestAMemberTheCarrierRefusesIsSkipped pins the separation of the two
// refusals. A member of a width the backend has no carrier for refuses the
// union already, on the ordinary carrier ground and through a message that
// names the WIDTH. Reporting it here as well would produce a message
// naming a PAIR for a union that has no colliding pair in it.
//
// The skip is on both sides of the comparison, and the second row is why
// that matters: two refused members must not match each other through the
// empty tag they share.
func TestAMemberTheCarrierRefusesIsSkipped(t *testing.T) {
	t.Run("a refused member does not collide with a carried one", func(t *testing.T) {
		pt := graph.UnionOf([]graph.UnionMember{{Type: graph.TypeDecimal}, {Type: graph.TypeString}})
		_, ok := stubCarrier(graph.TypeDecimal)
		require.False(t, ok, "this row is vacuous unless the stub carrier really refuses DECIMAL")
		_, _, collides := codegen.UnionMemberCollision(pt, stubCarrier, stubFamily)
		require.False(t, collides)
	})

	t.Run("two refused members do not collide with each other", func(t *testing.T) {
		pt := graph.UnionOf([]graph.UnionMember{{Type: graph.TypeDecimal}, {Type: graph.TypeBytes}})
		_, _, collides := codegen.UnionMemberCollision(pt, stubCarrier, stubFamily)
		require.False(t, collides,
			"the empty tag marks a member the walk does not speak for; it is not a family two members can share")
	})

	t.Run("a carried pair still collides beside a refused member", func(t *testing.T) {
		pt := graph.UnionOf([]graph.UnionMember{
			{Type: graph.TypeDecimal}, {Type: graph.TypeInt32}, {Type: graph.TypeInt64},
		})
		a, b, collides := codegen.UnionMemberCollision(pt, stubCarrier, stubFamily)
		require.True(t, collides, "skipping a member must not skip the pair beside it")
		require.Equal(t, graph.TypeInt32, a)
		require.Equal(t, graph.TypeInt64, b)
	})
}

// TestUnionCarrierRefusesOnBothGrounds pins that UnionCarrier answers the
// same text on every admitted union and refuses on EITHER ground, which is
// the whole of its contract: the two refusals reach the caller as one
// ok=false and are told apart by asking UnionMemberCollision afterwards.
func TestUnionCarrierRefusesOnBothGrounds(t *testing.T) {
	t.Run("a wire-distinct union carries any", func(t *testing.T) {
		pt := graph.UnionOf([]graph.UnionMember{{Type: graph.TypeString}, {Type: graph.TypeDate}})
		got, ok := codegen.UnionCarrier(pt, stubCarrier, stubFamily)
		require.True(t, ok)
		require.Equal(t, codegen.UnionCarrierText, got)
		require.Equal(t, "any", got, "Go has no sum type, so the carrier is `any` on every backend")
	})

	t.Run("a member with no carrier refuses the union", func(t *testing.T) {
		pt := graph.UnionOf([]graph.UnionMember{{Type: graph.TypeDecimal}, {Type: graph.TypeString}})
		got, ok := codegen.UnionCarrier(pt, stubCarrier, stubFamily)
		require.False(t, ok)
		require.Empty(t, got)
		// And it is NOT the admission rule that refused it, so a caller
		// building a message finds no pair to name.
		_, _, collides := codegen.UnionMemberCollision(pt, stubCarrier, stubFamily)
		require.False(t, collides)
	})

	t.Run("a colliding pair refuses the union", func(t *testing.T) {
		pt := graph.UnionOf([]graph.UnionMember{{Type: graph.TypeInt32}, {Type: graph.TypeInt64}})
		got, ok := codegen.UnionCarrier(pt, stubCarrier, stubFamily)
		require.False(t, ok)
		require.Empty(t, got)
		_, _, collides := codegen.UnionMemberCollision(pt, stubCarrier, stubFamily)
		require.True(t, collides, "this refusal owes a message naming the pair")
	})

	t.Run("a member NOT NULL does not change the carrier", func(t *testing.T) {
		// Spec §4's recorded decision rather than an omission: the value's
		// nullability is the property's own, and a per-member NOT NULL
		// constrains what the schema admits, which is the resolver's
		// concern and not the carrier's.
		plain := graph.UnionOf([]graph.UnionMember{{Type: graph.TypeString}, {Type: graph.TypeDate}})
		qualified := graph.UnionOf([]graph.UnionMember{
			{Type: graph.TypeString, NotNull: true}, {Type: graph.TypeDate, NotNull: true},
		})
		require.NotEqual(t, plain, qualified, "the two declarations differ, so this row is not comparing one with itself")

		plainText, ok := codegen.UnionCarrier(plain, stubCarrier, stubFamily)
		require.True(t, ok)
		qualifiedText, ok := codegen.UnionCarrier(qualified, stubCarrier, stubFamily)
		require.True(t, ok)
		require.Equal(t, plainText, qualifiedText)
	})
}

// TestTheFamilyIsAskedOfTheCarrierText holds the argument shape the walk
// is built on, which a refactor would otherwise be free to change
// silently. The family fold is asked of the carrier TEXT and never of the
// declared width, so it stays a fold over the answer the backend's decode
// is actually built on rather than a second table beside Property with its
// own chance to disagree with it.
func TestTheFamilyIsAskedOfTheCarrierText(t *testing.T) {
	var asked []string
	spy := func(goType string) string {
		asked = append(asked, goType)
		return stubFamily(goType)
	}
	pt := graph.UnionOf([]graph.UnionMember{{Type: graph.TypeString}, {Type: graph.TypeDate}})
	_, ok := codegen.UnionCarrier(pt, stubCarrier, spy)
	require.True(t, ok)
	require.NotEmpty(t, asked, "the fold was never called, so the rows above pin nothing about what it is asked")
	require.Equal(t, []string{"Date", "string"}, asked,
		"every question is a CARRIER text; a declared width reaching here would spell DATE or STRING")
}

// TestUnionMembersRendersThePlanInCanonicalOrder binds the emission plan to
// the order the encode type-switch, the decode dispatch and the refusal
// message all read. They are three renderings of one member list, so a plan
// that reordered would put a schema's members in one order in the switch and
// another in the message that explains it.
//
// The width is carried BESIDE the carrier text, and the last assertion is
// why: a member that is a declared record names its helper from the width,
// and the anonymous struct text does not run backwards into a PropertyType.
func TestUnionMembersRendersThePlanInCanonicalOrder(t *testing.T) {
	// Declared late-first, so a plan built in DECLARATION order answers the
	// reverse and this row is not satisfied by accident.
	pt := graph.UnionOf([]graph.UnionMember{{Type: graph.TypeString}, {Type: graph.TypeDate}})
	plan, ok := codegen.UnionMembers(pt, stubCarrier)
	require.True(t, ok)
	require.Equal(t, []codegen.UnionMemberPlan{
		{GoType: "Date", Width: graph.TypeDate},
		{GoType: "string", Width: graph.TypeString},
	}, plan, "DATE sorts before STRING in the canonical encoding, whatever order the author typed")

	members := pt.Members()
	require.Len(t, plan, len(members), "the plan must speak for every member or the dispatch is missing an arm")
	for i, m := range members {
		require.Equal(t, m.Type, plan[i].Width, "entry %d must carry the member's own declared width", i)
	}
}

// TestUnionMembersRefusesWholeWhenAMemberIsRefused holds the property that
// keeps a dispatch total. One member this backend has no carrier for refuses
// the WHOLE union with no partial plan, because a dispatch missing an arm is
// not a carrier: it would accept a value at bind and have nothing to narrow
// it back to at decode.
//
// The nil assertion is the load-bearing half. A walk that returned the
// members it did manage, alongside ok=false, would hand a caller that
// ignored the flag a plan short one arm — and that emits a package which
// compiles.
func TestUnionMembersRefusesWholeWhenAMemberIsRefused(t *testing.T) {
	_, ok := stubCarrier(graph.TypeDecimal)
	require.False(t, ok, "this test is vacuous unless the stub carrier really refuses DECIMAL")

	// DECIMAL sorts after both carried members, so the refusal has to be
	// reached AFTER two entries were already appended: a walk that returned
	// what it had would answer a two-entry plan here.
	pt := graph.UnionOf([]graph.UnionMember{
		{Type: graph.TypeString}, {Type: graph.TypeDate}, {Type: graph.TypeDecimal},
	})
	plan, ok := codegen.UnionMembers(pt, stubCarrier)
	require.False(t, ok)
	require.Nil(t, plan, "a partial plan emits a dispatch missing an arm, and that compiles")
}

// TestUnionMembersDoesNotReAskTheAdmissionRule pins a deliberate absence.
// UnionCarrier answers the admission rule at the preparation sites, before
// any emission walk runs, so a union that collides never reaches an encoding
// set — and asking again here would be a second copy of the rule with its
// own chance to disagree with the one the refusal messages are worded from.
//
// Asserted as the plan a colliding union still renders, because that is the
// only observable difference between "does not ask" and "asks and happens to
// agree".
func TestUnionMembersDoesNotReAskTheAdmissionRule(t *testing.T) {
	pt := graph.UnionOf([]graph.UnionMember{{Type: graph.TypeInt32}, {Type: graph.TypeInt64}})
	_, _, collides := codegen.UnionMemberCollision(pt, stubCarrier, stubFamily)
	require.True(t, collides, "this row is vacuous unless the union really is one the admission rule refuses")
	_, ok := codegen.UnionCarrier(pt, stubCarrier, stubFamily)
	require.False(t, ok, "and unless UnionCarrier is the thing that refuses it")

	plan, ok := codegen.UnionMembers(pt, stubCarrier)
	require.True(t, ok, "UnionMembers asks the carrier and nothing else; the admission rule is answered upstream")
	require.Len(t, plan, 2)
}

// TestUnionHelperSuffixIsAFunctionOfTheEncoding is the union half of the
// claim TestRecordHelperSuffixIsAFunctionOfTheEncoding makes: one canonical
// union names one helper pair, whatever order the author typed its members
// in, and two different unions name two.
//
// The last row is the namespace claim, and it is the one that is not
// implied by the others. A record and a union are different widths, so they
// never collide in graph.PropertyType — but their DIGESTS are over
// different strings, and without the differing "Union"/"Record" fragment a
// shared prefix would let one emission declare two helpers of one name by
// accident of the hash.
func TestUnionHelperSuffixIsAFunctionOfTheEncoding(t *testing.T) {
	byOrder := graph.UnionOf([]graph.UnionMember{{Type: graph.TypeString}, {Type: graph.TypeDate}})
	reversed := graph.UnionOf([]graph.UnionMember{{Type: graph.TypeDate}, {Type: graph.TypeString}})
	require.Equal(t, byOrder, reversed, "UnionOf did not canonicalise the two spellings, so the row below tests nothing about the suffix")
	require.Equal(t, codegen.UnionHelperSuffix(byOrder), codegen.UnionHelperSuffix(reversed))

	distinct := map[string]graph.PropertyType{}
	for _, pt := range []graph.PropertyType{
		byOrder,
		graph.UnionOf([]graph.UnionMember{{Type: graph.TypeInt32}, {Type: graph.TypeDate}}),
		graph.UnionOf([]graph.UnionMember{{Type: graph.TypeString, NotNull: true}, {Type: graph.TypeDate}}),
		graph.UnionOf([]graph.UnionMember{{Type: graph.TypeString}, {Type: graph.TypeDate}, {Type: graph.TypeInt32}}),
	} {
		suffix := codegen.UnionHelperSuffix(pt)
		prior, clash := distinct[suffix]
		require.False(t, clash, "%s and %s both name helper %q", prior, pt, suffix)
		distinct[suffix] = pt
	}
	require.Len(t, distinct, 4, "four distinct encodings produced %d names", len(distinct))

	record := graph.RecordOf([]graph.RecordField{{Name: "zip", Type: graph.TypeInt32}})
	require.NotEqual(t, codegen.UnionHelperSuffix(byOrder), codegen.RecordHelperSuffix(record))
	require.True(t, strings.HasPrefix(codegen.UnionHelperSuffix(byOrder), "Union"),
		"the fragment that keeps the two namespaces apart is the literal prefix, not the digest under it")
	require.True(t, strings.HasPrefix(codegen.RecordHelperSuffix(record), "Record"))
}

// TestUnionHelperSuffixIsASpellableIdentifierFragment holds what the
// emitted call sites need of the name: "encode"+suffix and "decode"+suffix
// have to be Go identifiers, because the emitted package declares and calls
// them as functions. The live hazard is a derivation that stopped hashing
// and interpolated the encoding — UNION<DATE|STRING> is full of characters
// Go's grammar does not admit mid-identifier.
//
// Parsed rather than pattern-matched, because a regexp over the name is a
// second opinion about Go's identifier grammar and this test would then pin
// the opinion rather than the grammar.
func TestUnionHelperSuffixIsASpellableIdentifierFragment(t *testing.T) {
	for _, pt := range []graph.PropertyType{
		graph.UnionOf([]graph.UnionMember{{Type: graph.TypeString}, {Type: graph.TypeDate}}),
		graph.UnionOf([]graph.UnionMember{
			{Type: graph.RecordOf([]graph.RecordField{{Name: "zip", Type: graph.TypeInt32}})},
			{Type: graph.TypeString},
		}),
	} {
		for _, verb := range []string{"encode", "decode"} {
			name := verb + codegen.UnionHelperSuffix(pt)
			expr, err := parser.ParseExpr(name)
			require.NoError(t, err, "%s names helper %q, which is not a Go expression", pt, name)
			ident, ok := expr.(*ast.Ident)
			require.True(t, ok, "%s names helper %q, which parses as %T rather than a bare identifier", pt, name, expr)
			require.Equal(t, name, ident.Name)
		}
	}
}

// TestUnionHelperNamesAreTheNamesEmitted binds what the identifier sweep
// reserves to what a backend writes, for the reason
// TestRecordHelperNamesAreTheNamesEmitted binds the record five: the sweep
// is the only reader, and it runs before any file is rendered, so nothing
// else would notice if the two drifted.
//
// The ListPtr wrapper is one of the five and not an oversight in the
// other direction: a nullable LIST<UNION<…>> parameter binds nil as the
// Cypher null its declaration asked for, which an `encode…List` taking a
// value slice has no nil to distinguish. The record group carries the same
// wrapper for the same reason.
//
// The absence of a carrier alias is asserted rather than left implicit. A
// union carries as `any`, a predeclared name every emission already spells,
// so an alias would be a second spelling of a type the backends must not
// drift on — and a sweep reserving one would refuse a schema for an
// identifier nothing emits.
func TestUnionHelperNamesAreTheNamesEmitted(t *testing.T) {
	pt := graph.UnionOf([]graph.UnionMember{{Type: graph.TypeString}, {Type: graph.TypeDate}})
	suffix := codegen.UnionHelperSuffix(pt)

	names := codegen.UnionHelperNames(pt)
	require.ElementsMatch(t, []string{
		"encode" + suffix,
		"encode" + suffix + "Ptr",
		"encode" + suffix + "List",
		"encode" + suffix + "ListPtr",
		"decode" + suffix,
	}, names)

	seen := make(map[string]bool, len(names))
	for _, n := range names {
		require.False(t, seen[n], "%q is reserved twice, so the group occupies fewer identifiers than it declares", n)
		seen[n] = true
	}
	for _, n := range names {
		require.NotEqual(t, codegen.UnionCarrierText, n,
			"a union needs no carrier alias; `any` is predeclared and an alias would be a second spelling of it")
	}
}

// TestIsDeclaredUnionNeedsBothHalves screens the predicate every union call
// site asks before naming a helper, and it is two claims because either
// half alone over-answers.
//
// The TEXT cannot answer: `any` is also the carrier of ANY VALUE and of ANY
// PROPERTY VALUE, and neither has a member set to validate against — an
// emission reading the text alone would name a helper for every ANY column
// in the batch, and the emitted package would not compile.
//
// The WIDTH cannot answer either: a union this backend refused has no
// carrier text at all, and asking the kind alone would name a helper for a
// declaration the preparation rejected.
func TestIsDeclaredUnionNeedsBothHalves(t *testing.T) {
	declared := graph.UnionOf([]graph.UnionMember{{Type: graph.TypeString}, {Type: graph.TypeDate}})
	carrier, ok := codegen.UnionCarrier(declared, stubCarrier, stubFamily)
	require.True(t, ok)
	require.True(t, codegen.IsDeclaredUnion(carrier, declared),
		"the pair an admitted union presents must be the pair a helper is named for")

	anyText, ok := stubCarrier(graph.TypeAnyPropertyValue)
	require.True(t, ok)
	require.Equal(t, codegen.UnionCarrierText, anyText,
		"this row is vacuous unless %s really shares the union's carrier text", graph.TypeAnyPropertyValue)
	require.False(t, codegen.IsDeclaredUnion(anyText, graph.TypeAnyPropertyValue),
		"%s carries as %q and has no member set to validate against, so the text alone cannot name a helper",
		graph.TypeAnyPropertyValue, codegen.UnionCarrierText)

	refused := graph.UnionOf([]graph.UnionMember{{Type: graph.TypeInt32}, {Type: graph.TypeInt64}})
	text, ok := codegen.UnionCarrier(refused, stubCarrier, stubFamily)
	require.False(t, ok, "this row is vacuous unless the admission rule really refuses %s", refused)
	require.False(t, codegen.IsDeclaredUnion(text, refused),
		"a refused union has no carrier text, so no call site may name a helper for it")

	require.False(t, codegen.IsDeclaredUnion("map[string]any", declared),
		"a width alone does not name a helper; the carrier text is the other half")
}

// TestUnionEncodingsIsTransitiveThroughEveryHidingPosition is the union
// half of the claim the emitted package's compilability rests on. A union
// nested inside another shape still owes its own helper pair, and one that
// named a helper nothing declared would fail at `go build` of the EMITTED
// package with no line in the author's schema to point at.
//
// A union directly inside a union cannot arise — graph.UnionOf flattens an
// unqualified nested union into its parent — so the union-member route is
// exercised through a record field under a member, which is the shape that
// CAN arise and the one a walk closed over only two positions would miss.
func TestUnionEncodingsIsTransitiveThroughEveryHidingPosition(t *testing.T) {
	leaf := func(name string) graph.PropertyType {
		return graph.UnionOf([]graph.UnionMember{
			{Type: graph.RecordOf([]graph.RecordField{{Name: name, Type: graph.TypeString}})},
			{Type: graph.TypeInt32},
		})
	}
	entityLeaf, paramLeaf, colLeaf, listLeaf, memberLeaf := leaf("e"), leaf("p"), leaf("c"), leaf("l"), leaf("m")

	underField := func(hidden graph.PropertyType) graph.PropertyType {
		return graph.RecordOf([]graph.RecordField{{Name: "inner", Type: hidden}})
	}

	// Each position hides its leaf union one level down by a different
	// route, so a walk that closed over record fields but not list
	// elements (or the reverse, or not over union members) leaves a
	// named gap rather than a uniform miss.
	entityUnion := graph.UnionOf([]graph.UnionMember{{Type: underField(entityLeaf)}, {Type: graph.TypeString}})
	paramUnion := graph.UnionOf([]graph.UnionMember{{Type: graph.ListOf(underField(paramLeaf), false)}, {Type: graph.TypeString}})
	colUnion := graph.UnionOf([]graph.UnionMember{{Type: underField(colLeaf)}, {Type: graph.TypeDate}})
	listUnion := graph.UnionOf([]graph.UnionMember{{Type: underField(listLeaf)}, {Type: graph.TypeInt64}})
	nestedUnion := graph.UnionOf([]graph.UnionMember{{Type: underField(memberLeaf)}, {Type: graph.TypeBool}})

	entities := []codegen.Entity{{
		Name: "Blob",
		Fields: []codegen.EntityField{
			{PropName: "u", Field: "U", GoType: "any", Width: entityUnion},
			{PropName: "plain", Field: "Plain", GoType: "*string", Width: graph.TypeString},
		},
	}}
	prepared := []codegen.Query{{
		MethodName: "Q",
		ParamFields: []codegen.Param{
			{RawName: "u", Field: "U", GoType: "any", Width: paramUnion},
			{RawName: "plain", Field: "Plain", GoType: "*string", Width: graph.TypeString},
		},
		RowFields: []codegen.Row{
			{ColumnName: "u", Field: "U", Kind: codegen.ColumnProperty, Width: colUnion},
			{ColumnName: "node", Field: "Node", Kind: codegen.ColumnNode, GoType: "Blob"},
			{
				ColumnName: "us",
				Field:      "Us",
				Kind:       codegen.ColumnList,
				Width:      graph.ListOf(graph.ListOf(listUnion, false), false),
				ListElem: &codegen.ListElem{
					Kind:  codegen.ColumnList,
					Width: graph.ListOf(listUnion, false),
					Nested: &codegen.ListElem{
						Kind:  codegen.ColumnProperty,
						Width: nestedUnion,
					},
				},
			},
		},
	}}

	got := codegen.UnionEncodings(entities, prepared)

	want := []graph.PropertyType{
		entityUnion, entityLeaf,
		paramUnion, paramLeaf,
		colUnion, colLeaf,
		listUnion, listLeaf,
		nestedUnion, memberLeaf,
	}
	for _, pt := range want {
		require.Contains(t, got, pt, "%s is reachable from the batch and names a helper, but the walk did not report it", pt)
	}
	require.Len(t, got, len(want),
		"the walk reported %d encodings for a batch declaring %d; the extras are %v", len(got), len(want), got)
	require.True(t, slices.IsSorted(got),
		"the helper block is emitted in this order and must not move between runs; got %v", got)
}

// TestUnionEncodingsReportsEachEncodingOnce holds the property that makes
// the result a declaration list rather than a visit log: one encoding
// reached from four positions is one helper pair, and a second entry would
// declare it twice in the emitted package.
//
// The empty and scalars-only controls are what make that worth anything —
// a walk that reported nothing at all passes every dedup assertion ever
// written.
func TestUnionEncodingsReportsEachEncodingOnce(t *testing.T) {
	shared := graph.UnionOf([]graph.UnionMember{{Type: graph.TypeString}, {Type: graph.TypeDate}})

	require.Empty(t, codegen.UnionEncodings(nil, nil), "a batch declaring nothing reached a union encoding")
	require.Empty(t, codegen.UnionEncodings(
		[]codegen.Entity{{Name: "Blob", Fields: []codegen.EntityField{
			{PropName: "s", Width: graph.TypeString},
			{PropName: "any", Width: graph.TypeAnyPropertyValue},
			{PropName: "rec", Width: graph.RecordOf([]graph.RecordField{{Name: "zip", Type: graph.TypeInt32}})},
		}}},
		nil,
	), "a batch of scalars and records reached a union encoding; ANY PROPERTY VALUE is a scalar and names no helper")

	got := codegen.UnionEncodings(
		[]codegen.Entity{{Name: "Blob", Fields: []codegen.EntityField{
			{PropName: "a", Width: shared},
			{PropName: "b", Width: graph.ListOf(shared, false)},
		}}},
		[]codegen.Query{{
			MethodName:  "Q",
			ParamFields: []codegen.Param{{RawName: "c", Width: shared}},
			RowFields:   []codegen.Row{{ColumnName: "d", Kind: codegen.ColumnProperty, Width: shared}},
		}},
	)
	require.Equal(t, []graph.PropertyType{shared}, got,
		"one encoding reached from four positions must be one helper pair")
}
