package graph_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/graph"
)

// TestScalarConstantsCarryNoSpace holds the invariant splitNotNull rests on.
//
// splitNotNull decides "does this encoded type carry a trailing NOT NULL" by a
// suffix test, and that is only safe because a nested qualifier is never last
// (every parameterised encoding ends in ">") and no scalar constant contains a
// space of its own. The second half is a property of the constant table, and
// nothing but this test stops it changing: a future constant spelled
// "ZONED TIME" would be split into "ZONED" the moment anything asked whether
// "ZONED TIME NOT NULL" was qualified — no compile error, no parse error, a
// wrong type.
//
// Derived from the source rather than from a list, so a constant added
// tomorrow is checked without anyone remembering to add it here. The
// parameterised constants are exempt by shape, not by name: they are the ones
// whose spelling contains "<", which is precisely what makes them immune.
func TestScalarConstantsCarryNoSpace(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "propertytype.go", nil, 0)
	require.NoError(t, err)

	var checked int
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			id, ok := vs.Type.(*ast.Ident)
			if !ok || id.Name != "PropertyType" {
				continue
			}
			for i, val := range vs.Values {
				lit, ok := val.(*ast.BasicLit)
				require.True(t, ok, "%s is not a string literal", vs.Names[i].Name)
				spelling, err := strconv.Unquote(lit.Value)
				require.NoError(t, err)
				if strings.Contains(spelling, "<") {
					continue
				}
				checked++
				require.NotContains(t, spelling, " ",
					"scalar constant %s is spelled %q; a space in a constant breaks splitNotNull's "+
						"suffix test, which is how every record field and union member finds its "+
						"own NOT NULL qualifier", vs.Names[i].Name, spelling)
			}
		}
	}
	require.NotZero(t, checked, "walked the constant table and found no scalar PropertyType constants, "+
		"so this test asserted nothing — the walk, not the table, is what broke")
}

// TestPropertyTypeKindDiscriminates pins that Kind names each parameterised
// family and that no scalar constant is mistaken for one. The scalar rows are
// not filler: Kind is a prefix test, so a constant beginning with the same
// letters as a family prefix would be misread, and these are the constants that
// exist to be misread.
func TestPropertyTypeKindDiscriminates(t *testing.T) {
	for _, row := range []struct {
		pt   graph.PropertyType
		want graph.PropertyTypeKind
	}{
		{graph.TypeString, graph.KindScalar},
		{graph.TypeInt, graph.KindScalar},
		{graph.TypeAnyPropertyValue, graph.KindScalar},
		{graph.TypeList, graph.KindList},
		{graph.ListOf(graph.TypeString, false), graph.KindList},
		{graph.TypeAnyRecord, graph.KindRecord},
		{"RECORD<>", graph.KindRecord},
		{graph.RecordOf([]graph.RecordField{{Name: "a", Type: graph.TypeInt}}), graph.KindRecord},
		{graph.UnionOf([]graph.UnionMember{{Type: graph.TypeString}, {Type: graph.TypeInt}}), graph.KindUnion},
	} {
		require.Equal(t, row.want, row.pt.Kind(), "Kind of %q", row.pt)
	}
}

// TestRecordOfCanonicalisesFieldOrder is the falsifier for the whole
// canonicalisation clause. resolver/scope.go compares property types with ==
// to unify a query ref across labels, so two spellings of one record type must
// produce one string or the unification silently refuses a valid query. Nothing
// else in the suite catches that: it is not a parse error and not an emission
// error, it is a query that stops working.
func TestRecordOfCanonicalisesFieldOrder(t *testing.T) {
	ab := graph.RecordOf([]graph.RecordField{
		{Name: "a", Type: graph.TypeInt},
		{Name: "b", Type: graph.TypeString},
	})
	ba := graph.RecordOf([]graph.RecordField{
		{Name: "b", Type: graph.TypeString},
		{Name: "a", Type: graph.TypeInt},
	})
	require.Equal(t, ab, ba, "field order must not distinguish two spellings of one record type")
	require.Equal(t, graph.PropertyType("RECORD<a INT,b STRING>"), ab)
}

// TestUnionOfCanonicalisesMemberOrder is the same falsifier for the union
// family: a closed dynamic union is a SET of members, so spelling order is not
// part of the type.
func TestUnionOfCanonicalisesMemberOrder(t *testing.T) {
	si := graph.UnionOf([]graph.UnionMember{{Type: graph.TypeString}, {Type: graph.TypeInt}})
	is := graph.UnionOf([]graph.UnionMember{{Type: graph.TypeInt}, {Type: graph.TypeString}})
	require.Equal(t, si, is, "member order must not distinguish two spellings of one union type")
	require.Equal(t, graph.PropertyType("UNION<INT|STRING>"), si)
}

// TestUnionOfSetSemantics pins the four reductions the design names, each of
// which is a decision rather than an inevitability (the ISO semantics volume is
// unavailable — see the ADR's caveat). The NOT NULL singleton is the row that
// distinguishes a correct implementation from one that collapses eagerly:
// collapsing it would drop the qualifier on the floor.
func TestUnionOfSetSemantics(t *testing.T) {
	t.Run("exact duplicates are dropped", func(t *testing.T) {
		require.Equal(t,
			graph.PropertyType("UNION<INT|STRING>"),
			graph.UnionOf([]graph.UnionMember{
				{Type: graph.TypeString}, {Type: graph.TypeInt}, {Type: graph.TypeString},
			}))
	})

	t.Run("a nullable singleton collapses to its member", func(t *testing.T) {
		require.Equal(t, graph.TypeString,
			graph.UnionOf([]graph.UnionMember{{Type: graph.TypeString}}))
	})

	t.Run("a NOT NULL singleton stays a union", func(t *testing.T) {
		require.Equal(t, graph.PropertyType("UNION<STRING NOT NULL>"),
			graph.UnionOf([]graph.UnionMember{{Type: graph.TypeString, NotNull: true}}))
	})

	// Members sort by their ENCODED spelling, so the unqualified member sorts
	// first: "STRING" is a prefix of "STRING NOT NULL". Pinned in that order
	// because the order is the canonical form, not an incidental one.
	t.Run("duplicates differing only in NOT NULL are both kept", func(t *testing.T) {
		require.Equal(t, graph.PropertyType("UNION<STRING|STRING NOT NULL>"),
			graph.UnionOf([]graph.UnionMember{
				{Type: graph.TypeString}, {Type: graph.TypeString, NotNull: true},
			}))
	})

	t.Run("a bare ANY member absorbs the union", func(t *testing.T) {
		require.Equal(t, graph.TypeAnyPropertyValue,
			graph.UnionOf([]graph.UnionMember{
				{Type: graph.TypeString}, {Type: graph.TypeAnyPropertyValue},
			}))
	})

	t.Run("nested unions are flattened", func(t *testing.T) {
		inner := graph.UnionOf([]graph.UnionMember{{Type: graph.TypeInt}, {Type: graph.TypeBool}})
		require.Equal(t, graph.PropertyType("UNION<BOOL|INT|STRING>"),
			graph.UnionOf([]graph.UnionMember{{Type: inner}, {Type: graph.TypeString}}))
	})
}

// TestRecordFieldNameQuoting is the security-shaped row. fieldName admits a
// DELIMITED_IDENTIFIER, so a grammatical field name may contain the very bytes
// the encoding uses as structure. Unquoted, such a name forges a field
// boundary and two different record types collide on one string — which,
// because == is how types unify, silently equates them.
func TestRecordFieldNameQuoting(t *testing.T) {
	forged := graph.RecordOf([]graph.RecordField{
		{Name: "a INT,b", Type: graph.TypeString},
	})
	honest := graph.RecordOf([]graph.RecordField{
		{Name: "a", Type: graph.TypeInt},
		{Name: "b", Type: graph.TypeString},
	})
	require.NotEqual(t, honest, forged,
		"a field name containing the encoding's separators must not forge a second field")

	require.Equal(t, []graph.RecordField{{Name: "a INT,b", Type: graph.TypeString}}, forged.Fields(),
		"the quoted name must round-trip verbatim, separators and all")

	t.Run("an internal backtick is doubled and round-trips", func(t *testing.T) {
		pt := graph.RecordOf([]graph.RecordField{{Name: "we`ird", Type: graph.TypeInt}})
		require.Equal(t, []graph.RecordField{{Name: "we`ird", Type: graph.TypeInt}}, pt.Fields())
	})

	t.Run("a name carrying the closing angle bracket round-trips", func(t *testing.T) {
		pt := graph.RecordOf([]graph.RecordField{
			{Name: "x>", Type: graph.TypeInt},
			{Name: "y", Type: graph.TypeString},
		})
		require.Equal(t, []graph.RecordField{
			{Name: "x>", Type: graph.TypeInt},
			{Name: "y", Type: graph.TypeString},
		}, pt.Fields())
	})
}

// TestFieldsAndMembersSplitAtTopLevelOnly pins the splitter against the shapes
// a naive strings.Split gets wrong. Both nest a separator inside a parameter,
// so a depth-blind split reports more fields or members than exist and every
// one of them is garbage.
func TestFieldsAndMembersSplitAtTopLevelOnly(t *testing.T) {
	t.Run("a record field whose type is a record", func(t *testing.T) {
		inner := graph.RecordOf([]graph.RecordField{
			{Name: "x", Type: graph.TypeInt},
			{Name: "y", Type: graph.TypeInt},
		})
		outer := graph.RecordOf([]graph.RecordField{
			{Name: "a", Type: inner},
			{Name: "b", Type: graph.TypeString},
		})
		require.Equal(t, graph.PropertyType("RECORD<a RECORD<x INT,y INT>,b STRING>"), outer)
		require.Equal(t, []graph.RecordField{
			{Name: "a", Type: inner},
			{Name: "b", Type: graph.TypeString},
		}, outer.Fields())
	})

	t.Run("a union member whose type is a list of unions", func(t *testing.T) {
		inner := graph.UnionOf([]graph.UnionMember{{Type: graph.TypeBool}, {Type: graph.TypeDate}})
		listed := graph.ListOf(inner, false)
		outer := graph.UnionOf([]graph.UnionMember{{Type: listed}, {Type: graph.TypeString}})
		require.Equal(t, graph.PropertyType("UNION<LIST<UNION<BOOL|DATE>>|STRING>"), outer)
		require.Equal(t, []graph.UnionMember{
			{Type: listed}, {Type: graph.TypeString},
		}, outer.Members())
	})

	t.Run("member NOT NULL is carried per member", func(t *testing.T) {
		pt := graph.UnionOf([]graph.UnionMember{
			{Type: graph.TypeInt, NotNull: true},
			{Type: graph.TypeString},
		})
		require.Equal(t, []graph.UnionMember{
			{Type: graph.TypeInt, NotNull: true},
			{Type: graph.TypeString},
		}, pt.Members())
	})

	t.Run("field NOT NULL is carried per field", func(t *testing.T) {
		pt := graph.RecordOf([]graph.RecordField{
			{Name: "a", Type: graph.TypeInt, NotNull: true},
			{Name: "b", Type: graph.TypeString},
		})
		require.Equal(t, graph.PropertyType("RECORD<a INT NOT NULL,b STRING>"), pt)
		require.Equal(t, []graph.RecordField{
			{Name: "a", Type: graph.TypeInt, NotNull: true},
			{Name: "b", Type: graph.TypeString},
		}, pt.Fields())
	})
}

// TestFieldsAndMembersAreNilOffFamily pins that the accessors do not invent
// structure for a type that has none. The two parameterless record spellings
// are the interesting rows: they ARE records, so Kind says KindRecord, and a
// caller that trusted a non-nil Fields() to mean "has fields" would be wrong
// about exactly them.
func TestFieldsAndMembersAreNilOffFamily(t *testing.T) {
	for _, pt := range []graph.PropertyType{
		graph.TypeAnyRecord, "RECORD<>", graph.TypeString, graph.TypeList,
		graph.UnionOf([]graph.UnionMember{{Type: graph.TypeInt}, {Type: graph.TypeString}}),
	} {
		require.Nil(t, pt.Fields(), "Fields of %q", pt)
	}
	for _, pt := range []graph.PropertyType{
		graph.TypeAnyRecord, "RECORD<>", graph.TypeString, graph.TypeList,
		graph.RecordOf([]graph.RecordField{{Name: "a", Type: graph.TypeInt}}),
	} {
		require.Nil(t, pt.Members(), "Members of %q", pt)
	}
}

// TestRecordAnyAndEmptyAreDistinct pins that a braceless RECORD and RECORD { }
// are two types. ISO's semantics volume is unavailable, so this is a recorded
// gqlc decision (see the ADR): one is "some record, fields unknown", the other
// is "a record with no fields", and collapsing them would make the second
// unifiable with anything the first is.
func TestRecordAnyAndEmptyAreDistinct(t *testing.T) {
	require.NotEqual(t, graph.TypeAnyRecord, graph.PropertyType("RECORD<>"))
	require.Equal(t, graph.KindRecord, graph.TypeAnyRecord.Kind())
	require.Equal(t, graph.KindRecord, graph.PropertyType("RECORD<>").Kind())
}

// TestElemTrimChainSurvivesParameterisedElements pins an accident. Elem strips
// a trailing ">" and then a trailing " NOT NULL", which is only safe because
// TrimSuffix(">") removes exactly one byte and a parameterised element still
// ends in ">" afterwards, so the NOT NULL trim cannot fire on the element's own
// interior qualifier. Արփինէ predicted this was broken and a scratch probe
// falsified the prediction; these rows exist so a later refactor to TrimRight —
// which strips every trailing ">" — cannot break it silently.
func TestElemTrimChainSurvivesParameterisedElements(t *testing.T) {
	for _, row := range []struct {
		list        graph.PropertyType
		wantElem    graph.PropertyType
		wantNotNull bool
	}{
		{graph.ListOf(graph.TypeString, false), graph.TypeString, false},
		{graph.ListOf(graph.TypeString, true), graph.TypeString, true},
		{
			graph.ListOf(graph.RecordOf([]graph.RecordField{{Name: "a", Type: graph.TypeInt, NotNull: true}}), false),
			graph.PropertyType("RECORD<a INT NOT NULL>"), false,
		},
		{
			graph.ListOf(graph.RecordOf([]graph.RecordField{{Name: "a", Type: graph.TypeInt}}), true),
			graph.PropertyType("RECORD<a INT>"), true,
		},
		{
			graph.ListOf(graph.UnionOf([]graph.UnionMember{
				{Type: graph.TypeInt, NotNull: true}, {Type: graph.TypeString},
			}), false),
			graph.PropertyType("UNION<INT NOT NULL|STRING>"), false,
		},
		{graph.ListOf(graph.ListOf(graph.TypeString, true), false), graph.ListOf(graph.TypeString, true), false},
	} {
		require.Equal(t, row.wantElem, row.list.Elem(), "Elem of %q", row.list)
		require.Equal(t, row.wantNotNull, row.list.ElemNotNull(), "ElemNotNull of %q", row.list)
	}
}
