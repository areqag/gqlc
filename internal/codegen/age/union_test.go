package age_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/codegen/age"
	"github.com/areqag/gqlc/internal/graph"
)

// unionAddr is a declared record used as a union member below, so the rows
// asking about the MAP family ask about a record rather than about
// RECORD<ANY>, which carries map[string]any outright and would make the
// collision trivially a comparison of one text with itself.
var unionAddr = graph.RecordOf([]graph.RecordField{
	{Name: "city", Type: graph.TypeString},
	{Name: "zip", Type: graph.TypeInt32, NotNull: true},
})

// TestTypeMapAdmitsAWireDistinctUnion pins the admitted half of spec §4's
// admission rule on this driver: a closed union whose members arrive as
// pairwise-distinct agtype families carries `any`, and the member list is
// enforced by generated validation rather than by the type.
//
// agtype's vocabulary is six words wide — boolean, integer, float, string,
// list and map — where the neo4j driver hands back a distinct Go type per
// temporal width. So this table is the narrower of the two, and every row
// here is admitted for a reason that survives the narrowing: DATE rides
// this backend's string, so DATE|INT32 is admitted while DATE|STRING (the
// next test) is not.
func TestTypeMapAdmitsAWireDistinctUnion(t *testing.T) {
	admitted := []graph.PropertyType{
		// One member each from a different scalar family.
		graph.UnionOf([]graph.UnionMember{{Type: graph.TypeBool}, {Type: graph.TypeInt64}}),
		graph.UnionOf([]graph.UnionMember{{Type: graph.TypeInt32}, {Type: graph.TypeString}}),
		// A DATE beside an INTEGER: the date rides agtype's string, which
		// is not where an integer width lands, so the pair is distinct
		// here even though DATE|STRING is not.
		graph.UnionOf([]graph.UnionMember{{Type: graph.TypeDate}, {Type: graph.TypeInt32}}),
		// A LOCALTIME is an integer on this wire, so it sits beside a
		// string without colliding — while LOCALTIME|DURATION does.
		graph.UnionOf([]graph.UnionMember{{Type: graph.TypeLocalTime}, {Type: graph.TypeString}}),
		// A list is its own family whatever its elements.
		graph.UnionOf([]graph.UnionMember{{Type: graph.TypeString}, {Type: graph.ListOf(graph.TypeInt32, false)}}),
		// A record is the map family, distinct from every scalar.
		graph.UnionOf([]graph.UnionMember{{Type: graph.TypeInt64}, {Type: unionAddr}}),
		// All six families agtype owns, at once.
		graph.UnionOf([]graph.UnionMember{
			{Type: graph.TypeBool},
			{Type: graph.TypeInt32},
			{Type: graph.TypeFloat64},
			{Type: graph.TypeString},
			{Type: graph.ListOf(graph.TypeString, true)},
			{Type: unionAddr},
		}),
		// A lone member that does not collapse: UnionOf keeps a NOT NULL
		// member as a union, because the union is the only place that
		// qualifier could live. One member is no pair, so it is admitted.
		graph.UnionOf([]graph.UnionMember{{Type: graph.TypeString, NotNull: true}}),
	}
	for _, pt := range admitted {
		t.Run(string(pt), func(t *testing.T) {
			require.Equal(t, graph.KindUnion, pt.Kind(),
				"UnionOf reduced this declaration to something that is not a union, so the row below asks about a "+
					"width other than the one it names")
			got, ok := age.TypeMap{}.Property(pt)
			require.True(t, ok, "%s", pt)
			require.Equal(t, "any", got,
				"Go has no sum type, so every closed union carries `any` and the member set is enforced by the "+
					"emitted validation (spec §4)")
		})
	}
}

// TestTypeMapRefusesAUnionWhoseMembersShareAWireFamily pins the refusing
// half of the admission rule, one row per family this driver collapses
// widths into.
//
// Two rows are spec §8's falsifier, and they are the point of this file.
// UNION<INT32|INT64> must be refused on BOTH backends, every integer width
// widening to one agtype integer everywhere. UNION<DATE|STRING> must be
// refused HERE and admitted on neo4j: a DATE is zero-padded ISO text in
// agtype and a dbtype.Date on the other wire, so the two backends really
// do disagree about it — which is what makes this refusal contingent and
// so obliges the message to name the backend (ADR 0035). The disagreement
// itself is asserted from the composition root, which can see both tables;
// this package can only hold up its own half.
func TestTypeMapRefusesAUnionWhoseMembersShareAWireFamily(t *testing.T) {
	refused := map[string]graph.PropertyType{
		"integer widths all widen to one agtype integer": graph.UnionOf([]graph.UnionMember{
			{Type: graph.TypeInt32}, {Type: graph.TypeInt64},
		}),
		"a signed and an unsigned width widen to the same integer": graph.UnionOf([]graph.UnionMember{
			{Type: graph.TypeInt16}, {Type: graph.TypeUint8},
		}),
		"float widths all widen to one agtype float": graph.UnionOf([]graph.UnionMember{
			{Type: graph.TypeFloat32}, {Type: graph.TypeFloat64},
		}),
		// The spec §8 falsifier cell: refused HERE, admitted on neo4j.
		"a DATE is ISO text and so shares the string family": graph.UnionOf([]graph.UnionMember{
			{Type: graph.TypeDate}, {Type: graph.TypeString},
		}),
		"a DURATION is an integer count and so collides with one": graph.UnionOf([]graph.UnionMember{
			{Type: graph.TypeDuration}, {Type: graph.TypeInt64},
		}),
		"two temporal widths both encoded as integers collide": graph.UnionOf([]graph.UnionMember{
			{Type: graph.TypeDuration}, {Type: graph.TypeLocalTime},
		}),
		"every list is one agtype list whatever its elements": graph.UnionOf([]graph.UnionMember{
			{Type: graph.ListOf(graph.TypeInt32, false)}, {Type: graph.ListOf(graph.TypeString, false)},
		}),
		"a bare LIST is the same agtype list a declared one is": graph.UnionOf([]graph.UnionMember{
			{Type: graph.TypeList}, {Type: graph.ListOf(graph.TypeString, false)},
		}),
		"two declared records are both an agtype map": graph.UnionOf([]graph.UnionMember{
			{Type: unionAddr},
			{Type: graph.RecordOf([]graph.RecordField{{Name: "lat", Type: graph.TypeFloat64, NotNull: true}})},
		}),
		"a declared record and RECORD<ANY> share the map family": graph.UnionOf([]graph.UnionMember{
			{Type: unionAddr}, {Type: graph.TypeAnyRecord},
		}),
		// ANY owns no shape, so it leaves none for a member beside it.
		// UnionOf absorbs an UNQUALIFIED ANY into the whole union, so the
		// only spelling that reaches this table carries NOT NULL.
		"ANY arrives as whatever was written and so collides with everything": graph.UnionOf([]graph.UnionMember{
			{Type: graph.TypeAnyPropertyValue, NotNull: true}, {Type: graph.TypeInt32},
		}),
		// Three members, two of which collide. The pair is what refuses,
		// not the count, so a union whose other members are fine is still
		// refused.
		"one colliding pair refuses a union whose other members are distinct": graph.UnionOf([]graph.UnionMember{
			{Type: graph.TypeString}, {Type: graph.TypeInt32}, {Type: graph.TypeUint64},
		}),
	}
	for name, pt := range refused {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, graph.KindUnion, pt.Kind(),
				"UnionOf reduced this declaration to something that is not a union, so the row below asks about a "+
					"width other than the one it names")
			got, ok := age.TypeMap{}.Property(pt)
			require.False(t, ok, "%s", pt)
			require.Empty(t, got)
		})
	}
}

// TestTypeMapRefusesAUnionOnTheOrdinaryCarrierGround holds the refusal
// that is NOT the admission rule, so the two are not read as one. A member
// of a width this driver has no carrier for refuses the union for the
// reason a bare property of that width is refused, and would be refused
// whatever the other members were.
//
// BYTES is the row this backend has and neo4j does not: agtype has no
// binary scalar, so BYTES is refused here as a bare property already and
// carries that refusal into every union it appears in. The two refusals
// owe different messages and different repairs — a carrier refusal names a
// width to change, an admission refusal names a PAIR either half of which
// the author may drop.
func TestTypeMapRefusesAUnionOnTheOrdinaryCarrierGround(t *testing.T) {
	for _, pt := range []graph.PropertyType{
		graph.UnionOf([]graph.UnionMember{{Type: graph.TypeBytes}, {Type: graph.TypeString}}),
		graph.UnionOf([]graph.UnionMember{{Type: graph.TypeDecimal}, {Type: graph.TypeString}}),
		graph.UnionOf([]graph.UnionMember{{Type: graph.TypeInt128}, {Type: graph.TypeBool}}),
		graph.UnionOf([]graph.UnionMember{{Type: graph.ListOf(graph.TypeFloat256, false)}, {Type: graph.TypeBool}}),
		graph.UnionOf([]graph.UnionMember{
			{Type: graph.RecordOf([]graph.RecordField{{Name: "f", Type: graph.TypeUint256, NotNull: true}})},
			{Type: graph.TypeBool},
		}),
	} {
		t.Run(string(pt), func(t *testing.T) {
			got, ok := age.TypeMap{}.Property(pt)
			require.False(t, ok, "%s", pt)
			require.Empty(t, got)
		})
	}
}

// TestTypeMapRefusesAZonedMemberOnTheContainerRule pins the third refusal,
// which exists on this backend alone and is the one a reader is most
// likely to mistake for a family collision.
//
// A zoned temporal rides an offset sidecar named after the PROPERTY, and a
// union member has no name of its own inside one — exactly the argument
// that refuses a zoned element in a list (spec §2), generalised to the
// other container position. So it refuses before the family question is
// asked, and it would refuse whatever the other member's family was: the
// rows below pair a zoned member with a string and with an integer, and
// both refuse.
//
// Neither width is refused as a bare property here — TIMESTAMP carries
// time.Time and TIME carries Time — so a reader who suspects the carrier
// table can check it in one line, and neither collides with the other
// member's family, so a reader who suspects the admission rule can rule it
// out too. The container rule is the only thing left.
func TestTypeMapRefusesAZonedMemberOnTheContainerRule(t *testing.T) {
	for _, zoned := range []graph.PropertyType{graph.TypeTimestamp, graph.TypeTime} {
		t.Run(string(zoned), func(t *testing.T) {
			bare, ok := age.TypeMap{}.Property(zoned)
			require.True(t, ok, "this width is not refused as a bare property, so the refusals below are the "+
				"container rule rather than the carrier table")
			require.NotEmpty(t, bare)

			for _, other := range []graph.PropertyType{graph.TypeString, graph.TypeInt32} {
				pt := graph.UnionOf([]graph.UnionMember{{Type: zoned}, {Type: other}})
				got, ok := age.TypeMap{}.Property(pt)
				require.False(t, ok, "%s", pt)
				require.Empty(t, got)
			}
		})
	}
}

// TestAUnionRidesAContainer holds that the union arm is reached through
// the same two container descents every other width is, so a union nested
// under a list or inside a record field inherits its own admission answer
// rather than being admitted by the container's.
//
// Without it the KindUnion guard would be a top-level-only rule and
// LIST<UNION<INT32|INT64>> would carry []*any — a decoder with nothing to
// dispatch on, which is the exact failure the rule exists to prevent.
func TestAUnionRidesAContainer(t *testing.T) {
	distinct := graph.UnionOf([]graph.UnionMember{{Type: graph.TypeBool}, {Type: graph.TypeString}})
	collides := graph.UnionOf([]graph.UnionMember{{Type: graph.TypeInt32}, {Type: graph.TypeInt64}})

	t.Run("a list of an admitted union carries its element", func(t *testing.T) {
		got, ok := age.TypeMap{}.Property(graph.ListOf(distinct, true))
		require.True(t, ok)
		// No star: `any` already carries null as nil, so the element rule
		// exempts it whatever the declared element nullability.
		require.Equal(t, "[]any", got)
	})

	t.Run("a list of a refused union is refused", func(t *testing.T) {
		_, ok := age.TypeMap{}.Property(graph.ListOf(collides, false))
		require.False(t, ok)
	})

	t.Run("a record field of an admitted union carries it", func(t *testing.T) {
		got, ok := age.TypeMap{}.Property(graph.RecordOf([]graph.RecordField{
			{Name: "v", Type: distinct, NotNull: true},
		}))
		require.True(t, ok)
		require.Equal(t, "struct {\n\tV any\n}", got)
	})

	t.Run("a record field of a refused union refuses the record", func(t *testing.T) {
		_, ok := age.TypeMap{}.Property(graph.RecordOf([]graph.RecordField{
			{Name: "v", Type: collides, NotNull: true},
		}))
		require.False(t, ok)
	})
}
