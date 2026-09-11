package codegen_test

import (
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
	switch pt {
	case graph.TypeInt32:
		return "int32", true
	case graph.TypeInt64:
		return "int64", true
	case graph.TypeString:
		return "string", true
	case graph.TypeDate:
		return "Date", true
	case graph.TypeAnyPropertyValue:
		return "any", true
	}
	return "", false
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
