package codegen_test

import (
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/codegen"
	"github.com/areqag/gqlc/internal/graph"
)

// goCarrier is a carrier callback standing in for a backend's Property
// over the few widths these tests declare. It refuses BYTES, which is
// how the AGE refusal reaches recordStructText in production, and it
// panics rather than silently answering for anything else so a test that
// drifts onto an undeclared width fails loudly instead of pinning a
// carrier nobody wrote.
func goCarrier(pt graph.PropertyType) (string, bool) {
	switch pt {
	case graph.TypeString:
		return "string", true
	case graph.TypeInt32:
		return "int32", true
	case graph.TypeBool:
		return "bool", true
	case graph.TypeBytes:
		return "", false
	default:
		panic("goCarrier: no answer declared for " + string(pt))
	}
}

// TestRecordStructTextIsAFunctionOfTheEncoding is spec §8's first
// falsifier, and it is the reason the carrier is an ANONYMOUS struct.
//
// Go gives anonymous struct types structural identity, so two texts that
// are byte-identical are the same type to the compiler with no registry
// and no declaration to share. That property is only worth anything if
// one canonical record always produces one text — and RecordOf sorts
// fields by name precisely so the two author spellings below encode
// identically. If the builder ever iterated something other than the
// canonical order, or interpolated anything site-derived, this fails
// here rather than surfacing as two incompatible Go types at a caller.
func TestRecordStructTextIsAFunctionOfTheEncoding(t *testing.T) {
	declared := graph.RecordOf([]graph.RecordField{
		{Name: "city", Type: graph.TypeString},
		{Name: "zip", Type: graph.TypeInt32},
	})
	reversed := graph.RecordOf([]graph.RecordField{
		{Name: "zip", Type: graph.TypeInt32},
		{Name: "city", Type: graph.TypeString},
	})
	require.Equal(t, declared, reversed,
		"the premise: RecordOf must already have collapsed the two spellings, or this test is about RecordOf and not about the builder")

	first, ok := codegen.RecordStructText(declared.Fields(), goCarrier)
	require.True(t, ok)
	second, ok := codegen.RecordStructText(reversed.Fields(), goCarrier)
	require.True(t, ok)

	require.Equal(t, first, second)
	require.Equal(t, "struct {\n\tCity *string\n\tZip *int32\n}", first,
		"the exact text is pinned because it is a Go TYPE identity, not a rendering preference")
}

// TestRecordStructTextSpellsEachField pins the three per-field decisions
// the spec makes, one row each, so a regression names which one moved.
func TestRecordStructTextSpellsEachField(t *testing.T) {
	tests := []struct {
		name   string
		fields []graph.RecordField
		want   string
	}{{
		// Spec §2: a field without NOT NULL is a pointer, spelled the
		// way a nullable property is spelled.
		name:   "a field without NOT NULL is a pointer",
		fields: []graph.RecordField{{Name: "city", Type: graph.TypeString}},
		want:   "struct {\n\tCity *string\n}",
	}, {
		name:   "a NOT NULL field drops the pointer",
		fields: []graph.RecordField{{Name: "city", Type: graph.TypeString, NotNull: true}},
		want:   "struct {\n\tCity string\n}",
	}, {
		// Spec §2: field names go through the paramFieldName mangle,
		// the same walk entity properties use.
		name:   "the field name goes through the parameter mangle",
		fields: []graph.RecordField{{Name: "zip_code", Type: graph.TypeInt32}},
		want:   "struct {\n\tZipCode *int32\n}",
	}, {
		// Spec §3: a record with no fields is the unit type, and Go
		// spells the unit type struct{}. This is RECORD<>; RECORD<ANY>
		// also has nil Fields and never reaches here, which the type
		// map tests pin separately.
		name:   "no declared fields is the unit type",
		fields: nil,
		want:   "struct{}",
	}, {
		name: "a nested record is spelled inline",
		fields: []graph.RecordField{{Name: "at", Type: graph.RecordOf([]graph.RecordField{
			{Name: "city", Type: graph.TypeString},
		})}},
		// The carrier callback is what recurses, so the nested text is
		// whatever the backend's Property answers; goCarrier has no arm
		// for a record, so this row supplies its own.
		want: "struct {\n\tAt *struct {\n\tCity *string\n}\n}",
	}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			carrier := goCarrier
			if tt.name == "a nested record is spelled inline" {
				carrier = func(pt graph.PropertyType) (string, bool) {
					if pt.Kind() == graph.KindRecord {
						return codegen.RecordStructText(pt.Fields(), goCarrier)
					}
					return goCarrier(pt)
				}
			}
			got, ok := codegen.RecordStructText(tt.fields, carrier)
			require.True(t, ok)
			require.Equal(t, tt.want, got)
		})
	}
}

// TestRecordStructTextRefusesWholeWhenAFieldIsRefused holds spec §2's
// rule that a field carrier the backend refuses makes the WHOLE record
// unrepresentable there. A struct with a hole in it is not a carrier, so
// there is no partial answer to give.
//
// The control matters more than the refusal: the same record with the
// refused field removed must be admitted, or the row above would also
// pass against a builder that had stopped answering at all.
func TestRecordStructTextRefusesWholeWhenAFieldIsRefused(t *testing.T) {
	refused := []graph.RecordField{
		{Name: "city", Type: graph.TypeString},
		{Name: "img", Type: graph.TypeBytes},
	}
	text, ok := codegen.RecordStructText(refused, goCarrier)
	require.False(t, ok, "BYTES is refused by this carrier, so the record carrying it is unrepresentable")
	require.Empty(t, text, "a refusal must not also return text a caller could use")

	admitted, ok := codegen.RecordStructText(refused[:1], goCarrier)
	require.True(t, ok, "the same record without the refused field must be admitted, or the refusal above is not about the field")
	require.Equal(t, "struct {\n\tCity *string\n}", admitted)
}

// TestRecordStructTextSettlesUnderFinaliseAtEveryPosition is the
// measurement spec §2's text form rests on, and this bead's plan asked
// for it EARLY because its answer decides the form. It decided two
// things, both measured rather than reasoned:
//
// The single-line alternative `struct{ City *string; Zip *int32 }` is
// NOT viable — gofmt rewrites it to the multi-line form. So the
// sanctioned fallback in the plan is not taken and spec §2 needs no
// amendment.
//
// And the multi-line form is NOT byte-stable under gofmt either, which
// is the correction that matters. gofmt pads field names into alignment
// columns (`Zip  *int32`) and re-indents the block to its position's
// depth, so ONE text cannot be simultaneously gofmt-clean as a top-level
// alias right-hand side and as a struct field one level deeper. A first
// probe of this used already-aligned input and reported stability; that
// was a badly chosen input, not a property of the form.
//
// So the builder does not chase gofmt, and deliberately emits no
// alignment padding: Finalise runs format.Source over every emitted file
// and owns the padding and the indentation. What this test holds is the
// claim Finalise actually needs — that at every position the emission
// PARSES, and that one format pass leaves a file gofmt would not touch
// again. A form that converged only after two passes would ship a
// generated file that fails a gofmt check.
func TestRecordStructTextSettlesUnderFinaliseAtEveryPosition(t *testing.T) {
	text, ok := codegen.RecordStructText([]graph.RecordField{
		{Name: "city", Type: graph.TypeString},
		{Name: "zip_code", Type: graph.TypeInt32},
		{Name: "at", Type: graph.RecordOf([]graph.RecordField{{Name: "lat", Type: graph.TypeInt32}})},
	}, func(pt graph.PropertyType) (string, bool) {
		if pt.Kind() == graph.KindRecord {
			return codegen.RecordStructText(pt.Fields(), goCarrier)
		}
		return goCarrier(pt)
	})
	require.True(t, ok)
	// A nested record is in the sample on purpose: nesting is where
	// indentation and gofmt's alignment-run breaking both bite, and a
	// flat record would witness neither.
	require.Contains(t, text, "At *struct {")

	positions := []struct {
		name string
		src  string
	}{
		{"a struct field declaration", "package p\n\ntype Row struct {\n\tAddr %s\n}\n"},
		{"a slice element", "package p\n\ntype Row struct {\n\tAddrs []%s\n}\n"},
		{"an alias right-hand side", "package p\n\ntype PlaceAddr = %s\n"},
		{"a parameter list", "package p\n\nfunc f(v %s) { _ = v }\n"},
	}
	for _, pos := range positions {
		t.Run(pos.name, func(t *testing.T) {
			once, err := format.Source([]byte(fmt.Sprintf(pos.src, text)))
			require.NoError(t, err, "the emitted text must parse in this position")
			twice, err := format.Source(once)
			require.NoError(t, err)
			require.Equal(t, string(once), string(twice),
				"one Finalise pass must leave the file gofmt-clean, or generation ships a file a gofmt check would reject")
			require.Contains(t, string(once), "ZipCode", "the mangled field name must survive formatting")
		})
	}
}

// TestRecordStructTextIsAGoTypeExpression holds the property the rest of
// the pipeline reads the carrier text with. typeTextNamesCarrier
// (temporal.go:126) parses carrier text as a Go expression to decide
// whether the generated package needs its temporal carriers declared,
// and treats a PARSE FAILURE as "yes" — so a record text that did not
// parse would not fail, it would silently over-emit. This asserts the
// premise that keeps that path honest.
func TestRecordStructTextIsAGoTypeExpression(t *testing.T) {
	text, ok := codegen.RecordStructText([]graph.RecordField{
		{Name: "city", Type: graph.TypeString},
		{Name: "zip", Type: graph.TypeInt32},
	}, goCarrier)
	require.True(t, ok)

	expr, err := parser.ParseExpr(text)
	require.NoError(t, err)
	require.IsType(t, &ast.StructType{}, expr, "the carrier text must be a struct type, not merely something that parses")
}

// TestRecordFieldLegalityAdmitsAndRefusesOnTheMangle is the guard
// RecordStructText's doc comment names as its precondition: the builder
// spells every field through paramFieldName and would otherwise emit a
// struct with two fields of one name, or a field with no name at all.
// Neither is Go, so without this check the refusal arrives from
// go/format as ErrFormatFailure — a sentinel that names a template bug,
// handed to an author whose schema is the thing at fault.
//
// Both halves are the mangle's doing rather than the author's spelling:
// `min_age` and `minAge` are two distinct GQL field names, and a name of
// underscores alone is a legal GQL field name that has no Go spelling.
func TestRecordFieldLegalityAdmitsAndRefusesOnTheMangle(t *testing.T) {
	refused := []struct {
		name   string
		fields []graph.RecordField
		reason string
	}{
		{
			"two spellings of one Go name",
			[]graph.RecordField{
				{Name: "min_age", Type: graph.TypeInt32},
				{Name: "minAge", Type: graph.TypeInt32},
			},
			`fields "minAge" and "min_age" both mangle to "MinAge"`,
		},
		{
			"a name of one underscore",
			[]graph.RecordField{{Name: "_", Type: graph.TypeInt32}},
			`field "_" mangles to no Go field name`,
		},
		{
			"a name of underscores alone",
			[]graph.RecordField{{Name: "___", Type: graph.TypeInt32}},
			`field "___" mangles to no Go field name`,
		},
		// The third illegality, and the one a delimited identifier makes
		// reachable: paramFieldName capitalises and drops underscores and
		// does nothing else, so every character the author wrote survives
		// into the struct field name. A GQL field name is only obliged to
		// be a legal GQL identifier, and `x-y` / `a b` / `pct%s` are all
		// of those when written delimited — the schema parser admits each.
		//
		// A '%' is called out separately because it does not merely fail
		// to compile: the decode helper's failure wording carries the
		// field name, and a name holding a verb the call has no argument
		// for is what `go vet` of the generated package would fail on if
		// the file got that far. It is refused here instead.
		{
			"a name holding a hyphen",
			[]graph.RecordField{{Name: "x-y", Type: graph.TypeInt32}},
			`field "x-y" mangles to "X-y", which is not a Go field name`,
		},
		{
			"a name holding a space",
			[]graph.RecordField{{Name: "a b", Type: graph.TypeInt32}},
			`field "a b" mangles to "A b", which is not a Go field name`,
		},
		{
			"a name holding a format verb",
			[]graph.RecordField{{Name: "pct%s", Type: graph.TypeInt32}},
			`field "pct%s" mangles to "Pct%s", which is not a Go field name`,
		},
		{
			"a name holding a quote",
			[]graph.RecordField{{Name: `we"ird`, Type: graph.TypeInt32}},
			`field "we\"ird" mangles to "We\"ird", which is not a Go field name`,
		},
		{
			"a name beginning with a digit",
			[]graph.RecordField{{Name: "1st", Type: graph.TypeInt32}},
			`field "1st" mangles to "1st", which is not a Go field name`,
		},
	}
	for _, tc := range refused {
		t.Run(tc.name, func(t *testing.T) {
			rec := graph.RecordOf(tc.fields)
			offender, reason, illegal := codegen.RecordFieldLegality(rec)
			require.True(t, illegal)
			require.Equal(t, rec, offender, "the refusal must name the record that carries the offending fields")
			require.Equal(t, tc.reason, reason)
		})
	}

	// The controls. Each is the refused row with the one thing that
	// makes it illegal taken away, so a check that refused every record
	// could not pass both tables.
	admitted := []struct {
		name   string
		fields []graph.RecordField
	}{
		{"two names that mangle apart", []graph.RecordField{
			{Name: "min_age", Type: graph.TypeInt32},
			{Name: "max_age", Type: graph.TypeInt32},
		}},
		{"a leading underscore that still leaves a name", []graph.RecordField{
			{Name: "_a", Type: graph.TypeInt32},
		}},
		{"one field", []graph.RecordField{{Name: "city", Type: graph.TypeString}}},
		// The control for the spelling clause, and the reason it cannot be
		// an ASCII test: a Go field name is any unicode letter followed by
		// letters, digits and underscores, so a schema written in a
		// non-Latin script mangles to a perfectly legal struct field. A
		// check that swept for [A-Za-z] would refuse this one.
		{"a name in a non-Latin script", []graph.RecordField{
			{Name: "քաղաք", Type: graph.TypeString},
		}},
		{"a name holding a digit after the first rune", []graph.RecordField{
			{Name: "line2", Type: graph.TypeString},
		}},
	}
	for _, tc := range admitted {
		t.Run(tc.name, func(t *testing.T) {
			_, _, illegal := codegen.RecordFieldLegality(graph.RecordOf(tc.fields))
			require.False(t, illegal)
		})
	}
}

// TestRecordFieldLegalityDescendsThroughContainers pins the reach of the
// check. A record is legal at its own node and illegal three levels down
// just as readily, and the position it is reached through — a list
// element, another record's field, both at once — does not change the
// answer, because every one of them is spelled as a Go struct in the
// end.
func TestRecordFieldLegalityDescendsThroughContainers(t *testing.T) {
	bad := graph.RecordOf([]graph.RecordField{
		{Name: "min_age", Type: graph.TypeInt32},
		{Name: "minAge", Type: graph.TypeInt32},
	})
	good := graph.RecordOf([]graph.RecordField{{Name: "city", Type: graph.TypeString}})

	refused := []struct {
		name string
		ty   graph.PropertyType
	}{
		{"the record itself", bad},
		{"under a list", graph.ListOf(bad, false)},
		{"under two lists", graph.ListOf(graph.ListOf(bad, false), false)},
		{"a field of a record", graph.RecordOf([]graph.RecordField{{Name: "at", Type: bad}})},
		{"a field of a field of a record", graph.RecordOf([]graph.RecordField{
			{Name: "at", Type: graph.RecordOf([]graph.RecordField{{Name: "inner", Type: bad}})},
		})},
		{"a list-valued field of a record", graph.RecordOf([]graph.RecordField{
			{Name: "ats", Type: graph.ListOf(bad, false)},
		})},
	}
	for _, tc := range refused {
		t.Run(tc.name, func(t *testing.T) {
			offender, reason, illegal := codegen.RecordFieldLegality(tc.ty)
			require.True(t, illegal)
			require.Equal(t, bad, offender,
				"the refusal names the record that carries the offending fields, not the type it was reached through")
			require.Contains(t, reason, `both mangle to "MinAge"`)
		})
	}

	admitted := []struct {
		name string
		ty   graph.PropertyType
	}{
		{"a scalar", graph.TypeString},
		{"a list of scalars", graph.ListOf(graph.TypeString, false)},
		{"a record whose fields are undeclared", graph.TypeAnyRecord},
		{"a record with no fields", graph.RecordOf(nil)},
		{"a legal record", good},
		{"a legal record under a list", graph.ListOf(good, false)},
		{"a legal record inside a legal record", graph.RecordOf([]graph.RecordField{{Name: "at", Type: good}})},
	}
	for _, tc := range admitted {
		t.Run(tc.name, func(t *testing.T) {
			_, _, illegal := codegen.RecordFieldLegality(tc.ty)
			require.False(t, illegal)
		})
	}
}

// TestRecordFieldLegalityReportsTheOutermostOffender fixes the report
// order where two levels are both illegal. The author is told about the
// declaration they can see rather than one nested inside it, which is
// why the check finishes an entire level before descending — a single
// loop that checked and descended field by field would answer with
// whichever offender sorted earlier, and the sort is over field names
// that have nothing to do with depth.
func TestRecordFieldLegalityReportsTheOutermostOffender(t *testing.T) {
	inner := graph.RecordOf([]graph.RecordField{
		{Name: "min_age", Type: graph.TypeInt32},
		{Name: "minAge", Type: graph.TypeInt32},
	})
	// "at" sorts before "z_id"/"zId", so a field-by-field walk would
	// descend into the inner offender before ever reaching the outer
	// collision beside it.
	outer := graph.RecordOf([]graph.RecordField{
		{Name: "at", Type: inner},
		{Name: "z_id", Type: graph.TypeInt32},
		{Name: "zId", Type: graph.TypeInt32},
	})

	offender, reason, illegal := codegen.RecordFieldLegality(outer)
	require.True(t, illegal)
	require.Equal(t, outer, offender)
	require.Equal(t, `fields "zId" and "z_id" both mangle to "ZId"`, reason)
}

// TestRecordHelperSuffixIsAFunctionOfTheEncoding is the naming half of
// the claim TestRecordStructTextIsAFunctionOfTheEncoding holds for the
// carrier text, and the two are one claim at the two ends of an
// emission: the struct text is what a record field IS, the suffix is
// what its codec is CALLED. A derivation that agreed on one and not the
// other would emit a helper whose name did not match the type it
// decodes into, in a package where the type has no declaration to
// anchor it.
//
// It is derived from the canonical encoding alone, so the two author
// spellings below — which RecordOf sorts into one PropertyType — must
// name one helper. That is what lets the two backends share the name
// without sharing any code, and what stops one schema declaring two
// helpers for one struct type.
//
// The second half is the control, and it is what makes the first half
// worth anything: records differing only in a field NAME, only in a
// field TYPE, or only in NULLABILITY must get distinct suffixes. A
// derivation that ignored its argument entirely passes the first half
// alone.
func TestRecordHelperSuffixIsAFunctionOfTheEncoding(t *testing.T) {
	byName := graph.RecordOf([]graph.RecordField{
		{Name: "city", Type: graph.TypeString},
		{Name: "zip", Type: graph.TypeInt32},
	})
	reversed := graph.RecordOf([]graph.RecordField{
		{Name: "zip", Type: graph.TypeInt32},
		{Name: "city", Type: graph.TypeString},
	})
	require.Equal(t, byName, reversed, "RecordOf did not canonicalise the two spellings, so the row below tests nothing about the suffix")
	require.Equal(t, codegen.RecordHelperSuffix(byName), codegen.RecordHelperSuffix(reversed),
		"one canonical record %s spelled two ways names two helpers, so a backend would declare one struct type's codec twice", byName)

	distinct := map[string]graph.PropertyType{}
	for _, pt := range []graph.PropertyType{
		byName,
		graph.RecordOf([]graph.RecordField{{Name: "town", Type: graph.TypeString}, {Name: "zip", Type: graph.TypeInt32}}),
		graph.RecordOf([]graph.RecordField{{Name: "city", Type: graph.TypeString}, {Name: "zip", Type: graph.TypeInt64}}),
		graph.RecordOf([]graph.RecordField{{Name: "city", Type: graph.TypeString, NotNull: true}, {Name: "zip", Type: graph.TypeInt32}}),
		graph.RecordOf(nil),
		graph.TypeAnyRecord,
	} {
		suffix := codegen.RecordHelperSuffix(pt)
		prior, clash := distinct[suffix]
		require.False(t, clash, "%s and %s both name helper %q", prior, pt, suffix)
		distinct[suffix] = pt
	}
	require.Len(t, distinct, 6, "six distinct encodings produced %d names", len(distinct))
}

// TestRecordHelperSuffixIsASpellableIdentifierFragment holds what the
// emitted call sites need of the name and nothing more: "encode"+suffix
// and "decode"+suffix have to be Go identifiers, because the emitted
// package declares and calls them as functions.
//
// The live hazard is a suffix carrying a character Go's grammar does
// not admit mid-identifier, which is exactly what a derivation that
// stopped hashing and interpolated the encoding itself would produce —
// RECORD<city STRING> is full of them. It is NOT the fragment's leading
// character: the verb prefix means the concatenation never starts with
// it, so a suffix beginning with a digit is a perfectly good name and
// this test says nothing about it. Measured, not reasoned — a mutant
// prefixing the fragment with "1" SURVIVED this test, and the sentence
// it falsified used to be here.
//
// Parsed rather than pattern-matched, because a regexp over the name is
// a second opinion about Go's identifier grammar and this test would
// then be pinning the opinion rather than the grammar.
func TestRecordHelperSuffixIsASpellableIdentifierFragment(t *testing.T) {
	for _, pt := range []graph.PropertyType{
		graph.RecordOf(nil),
		graph.RecordOf([]graph.RecordField{{Name: "city", Type: graph.TypeString}}),
		graph.TypeAnyRecord,
	} {
		for _, verb := range []string{"encode", "decode"} {
			name := verb + codegen.RecordHelperSuffix(pt)
			expr, err := parser.ParseExpr(name)
			require.NoError(t, err, "%s names helper %q, which is not a Go expression", pt, name)
			ident, ok := expr.(*ast.Ident)
			require.True(t, ok, "%s names helper %q, which parses as %T rather than a bare identifier", pt, name, expr)
			require.Equal(t, name, ident.Name)
		}
	}
}

// TestRecordEncodingsIsTransitiveThroughEveryHidingPosition is the
// claim the emitted package's compilability rests on: a decode helper
// calls its record fields' helpers rather than inlining them, so an
// encoding the walk misses is a call to a function nothing declared.
// That failure lands at go build of the GENERATED package, with no line
// in the author's schema to point at, which is why it is asserted here
// rather than left to a golden.
//
// One batch carries a record at every position the prepared surface has
// — entity field, parameter, scalar column, list column and the
// ListElem chain under it — and each of those records hides a further
// one under a list element or a record field. The nesting is the point:
// a walk that visited only the top level would find all five and still
// miss all five of their children, so the count below distinguishes the
// two.
func TestRecordEncodingsIsTransitiveThroughEveryHidingPosition(t *testing.T) {
	leaf := func(name string) graph.PropertyType {
		return graph.RecordOf([]graph.RecordField{{Name: name, Type: graph.TypeString}})
	}
	// Each outer record hides its leaf one level down, by a different
	// route per position, so a walk that closed over record fields but
	// not list elements (or the reverse) leaves a named gap.
	underField := func(name string, hidden graph.PropertyType) graph.PropertyType {
		return graph.RecordOf([]graph.RecordField{{Name: name, Type: hidden}})
	}
	underList := func(name string, hidden graph.PropertyType) graph.PropertyType {
		return graph.RecordOf([]graph.RecordField{{Name: name, Type: graph.ListOf(hidden, false)}})
	}

	entityLeaf, paramLeaf, colLeaf, listLeaf, nestedLeaf :=
		leaf("e"), leaf("p"), leaf("c"), leaf("l"), leaf("n")
	entityRec := underField("inner", entityLeaf)
	paramRec := underList("inner", paramLeaf)
	colRec := underField("inner", colLeaf)
	listRec := underList("inner", listLeaf)
	nestedRec := underField("inner", nestedLeaf)

	entities := []codegen.Entity{{
		Name: "Blob",
		Fields: []codegen.EntityField{
			{PropName: "rec", Field: "Rec", GoType: "struct{}", Width: entityRec},
			{PropName: "plain", Field: "Plain", GoType: "*string", Width: graph.TypeString},
		},
	}}
	prepared := []codegen.Query{{
		MethodName: "Q",
		ParamFields: []codegen.Param{
			{RawName: "rec", Field: "Rec", GoType: "struct{}", Width: paramRec},
			{RawName: "plain", Field: "Plain", GoType: "*string", Width: graph.TypeString},
		},
		RowFields: []codegen.Row{
			{ColumnName: "rec", Field: "Rec", Kind: codegen.ColumnProperty, Width: colRec},
			{ColumnName: "node", Field: "Node", Kind: codegen.ColumnNode, GoType: "Blob"},
			{
				ColumnName: "recs",
				Field:      "Recs",
				Kind:       codegen.ColumnList,
				Width:      graph.ListOf(graph.ListOf(listRec, false), false),
				ListElem: &codegen.ListElem{
					Kind:  codegen.ColumnList,
					Width: graph.ListOf(listRec, false),
					Nested: &codegen.ListElem{
						Kind:  codegen.ColumnProperty,
						Width: nestedRec,
					},
				},
			},
		},
	}}

	got := codegen.RecordEncodings(entities, prepared)

	want := []graph.PropertyType{
		entityRec, entityLeaf,
		paramRec, paramLeaf,
		colRec, colLeaf,
		listRec, listLeaf,
		nestedRec, nestedLeaf,
	}
	for _, pt := range want {
		require.Contains(t, got, pt, "%s is reachable from the batch and names a helper, but the walk did not report it", pt)
	}
	require.Len(t, got, len(want),
		"the walk reported %d encodings for a batch declaring %d; the extras are %v", len(got), len(want), got)
}

// TestRecordEncodingsReportsEachEncodingOnce holds the property that
// makes the result a declaration list rather than a visit log: one
// encoding reached from three positions is one helper pair, and a
// second entry would declare it twice in the emitted package.
//
// The control is the empty batch. Without it a walk that reported the
// empty set unconditionally would pass every dedup assertion ever
// written.
func TestRecordEncodingsReportsEachEncodingOnce(t *testing.T) {
	shared := graph.RecordOf([]graph.RecordField{{Name: "city", Type: graph.TypeString}})

	require.Empty(t, codegen.RecordEncodings(nil, nil),
		"a batch declaring nothing reached a record encoding")
	require.Empty(t, codegen.RecordEncodings(
		[]codegen.Entity{{Name: "Blob", Fields: []codegen.EntityField{{PropName: "s", Width: graph.TypeString}}}},
		[]codegen.Query{{MethodName: "Q", RowFields: []codegen.Row{{ColumnName: "s", Kind: codegen.ColumnProperty, Width: graph.ListOf(graph.TypeString, false)}}}},
	), "a batch of scalars and lists of scalars reached a record encoding")

	got := codegen.RecordEncodings(
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

// TestRecordEncodingsOmitsTheUndeclaredRecord holds the one exclusion
// the walk makes, and holds it against its own neighbour so the row
// cannot be read as "records are skipped".
//
// ANY_RECORD declares no fields, so there is no struct to build and no
// field-wise decode to write: both backends carry it as map[string]any,
// which is the driver's own shape. RECORD<> also declares no fields and
// IS present, because struct{} is a Go type a driver map still has to
// be checked into. The two are one keystroke apart in a schema and they
// get opposite answers, so the pair is asserted together.
//
// The nested row is the one that matters: ANY_RECORD as a FIELD of a
// declared record must not be reported either, or the walk would name a
// helper for it while the field itself carries a map.
func TestRecordEncodingsOmitsTheUndeclaredRecord(t *testing.T) {
	empty := graph.RecordOf(nil)
	holder := graph.RecordOf([]graph.RecordField{{Name: "loose", Type: graph.TypeAnyRecord}})

	got := codegen.RecordEncodings([]codegen.Entity{{Name: "Blob", Fields: []codegen.EntityField{
		{PropName: "any", Width: graph.TypeAnyRecord},
		{PropName: "anylist", Width: graph.ListOf(graph.TypeAnyRecord, false)},
		{PropName: "empty", Width: empty},
		{PropName: "holder", Width: holder},
	}}}, nil)

	require.NotContains(t, got, graph.TypeAnyRecord,
		"%s declares no fields and carries as a driver map, so a helper pair for it would decode a struct that does not exist", graph.TypeAnyRecord)
	require.Contains(t, got, empty, "%s is a declared record with zero fields, and struct{} is a Go type a map has to be checked into", empty)
	require.Contains(t, got, holder, "%s is a declared record and names a helper whatever its fields carry as", holder)
	require.Len(t, got, 2, "the walk reported %v; only the two declared records name helpers", got)
}

// TestRecordEncodingsIsOrderedByEncoding holds what keeps a generated
// file byte-stable across runs. The walk accumulates into a map, and Go
// randomises map iteration, so without the sort the helper block would
// reorder on every generation and every golden in the corpus would be
// noise that no schema change explains.
//
// Asserted as the sorted order rather than as "equal to a previous
// call", because repeating a call is a weak screen: two runs of a
// randomised iteration agree by chance often enough at these lengths
// that the row would pass a good fraction of the time against an
// unsorted walk.
func TestRecordEncodingsIsOrderedByEncoding(t *testing.T) {
	var fields []codegen.EntityField
	for _, name := range []string{"m", "d", "z", "a", "q", "f", "k", "w"} {
		fields = append(fields, codegen.EntityField{
			PropName: name,
			Width:    graph.RecordOf([]graph.RecordField{{Name: name, Type: graph.TypeString}}),
		})
	}
	got := codegen.RecordEncodings([]codegen.Entity{{Name: "Blob", Fields: fields}}, nil)

	require.Len(t, got, len(fields), "the eight declared encodings are distinct; the walk reported %d", len(got))
	require.True(t, slices.IsSorted(got), "the helper block is emitted in this order and must not move between runs; got %v", got)
}

// TestRecordFieldsAgreesWithTheStructTextItRenders is the claim that
// makes the walk worth sharing at all.
//
// A backend's encode/decode helper bodies assign and read the Go fields
// of the very struct RecordStructText declared. If the plan named a
// field the struct does not have, spelled a pointer where the struct
// spelled a value, or ordered its entries differently, the emitted
// package would not compile — and the failure would arrive from `go
// build` of generated code, with no line in the schema to point at. So
// the plan is checked against the text rather than against a second
// hand-written expectation: a hand-written one could drift with it.
//
// The record deliberately mixes both nullabilities, a mangled name and
// an already-Go name, so a plan that got any single decision wrong is
// visible as a disagreement rather than absorbed by uniformity.
func TestRecordFieldsAgreesWithTheStructTextItRenders(t *testing.T) {
	fields := []graph.RecordField{
		{Name: "city", Type: graph.TypeString},
		{Name: "ok", Type: graph.TypeBool, NotNull: true},
		{Name: "zip_code", Type: graph.TypeInt32},
	}
	declared := graph.RecordOf(fields)

	plan, ok := codegen.RecordFields(declared.Fields(), goCarrier)
	require.True(t, ok)
	text, ok := codegen.RecordStructText(declared.Fields(), goCarrier)
	require.True(t, ok)

	var rebuilt strings.Builder
	rebuilt.WriteString("struct {\n")
	for _, f := range plan {
		rebuilt.WriteString("\t" + f.Field + " ")
		if f.Nullable {
			rebuilt.WriteString("*")
		}
		rebuilt.WriteString(f.GoType + "\n")
	}
	rebuilt.WriteString("}")
	require.Equal(t, text, rebuilt.String(),
		"the plan must spell the same fields, in the same order, with the same nullability as the struct the helpers assign into")
}

// TestRecordFieldsCarriesTheWireKeyUnmangled separates the two names a
// record field has, because the emitted helper needs BOTH and they are
// not the same string.
//
// Key is what the driver's map is subscripted at — the name the author
// declared, which the server stores verbatim. Field is the Go struct
// field, which the mangle produced. A helper that read the map at the
// mangled name would compile cleanly and find nothing at run time, which
// is the failure this separation exists to make impossible: `m["ZipCode"]`
// against a map holding `zip_code`.
func TestRecordFieldsCarriesTheWireKeyUnmangled(t *testing.T) {
	declared := graph.RecordOf([]graph.RecordField{
		{Name: "zip_code", Type: graph.TypeInt32},
	})
	plan, ok := codegen.RecordFields(declared.Fields(), goCarrier)
	require.True(t, ok)
	require.Len(t, plan, 1)
	require.Equal(t, "zip_code", plan[0].Key, "the wire key is the declared name; the server never saw the mangle")
	require.Equal(t, "ZipCode", plan[0].Field)
	require.NotEqual(t, plan[0].Key, plan[0].Field,
		"the premise: a field whose two names coincide could not falsify a helper that used the wrong one")
}

// TestRecordFieldsCarriesTheDeclaredWidth pins the field the plan holds
// purely for the nested case. A record field that is itself a record is
// emitted as a call to that record's own helper, whose name is hashed
// from the canonical encoding — and the carrier text, an anonymous
// struct, does not run backwards into a PropertyType. Without Width the
// nested helper could not be named at all.
func TestRecordFieldsCarriesTheDeclaredWidth(t *testing.T) {
	inner := graph.RecordOf([]graph.RecordField{{Name: "city", Type: graph.TypeString}})
	declared := graph.RecordOf([]graph.RecordField{
		{Name: "at", Type: inner},
		{Name: "n", Type: graph.TypeInt32},
	})
	carrier := func(pt graph.PropertyType) (string, bool) {
		if pt.Kind() == graph.KindRecord {
			return codegen.RecordStructText(pt.Fields(), goCarrier)
		}
		return goCarrier(pt)
	}
	plan, ok := codegen.RecordFields(declared.Fields(), carrier)
	require.True(t, ok)
	require.Len(t, plan, 2)

	byField := map[string]codegen.RecordFieldPlan{}
	for _, f := range plan {
		byField[f.Field] = f
	}
	require.Equal(t, inner, byField["At"].Width,
		"the nested helper is named from this, so it must be the encoding and not a re-derivation from the text")
	require.Equal(t, graph.TypeInt32, byField["N"].Width)
}

// TestRecordFieldsRefusesWholeWhenAFieldIsRefused mirrors the rule
// RecordStructText already holds, at the plan the helper bodies are
// built from: one refused field refuses the whole record, and there is
// no partial plan to hand back. A partial one would be worse than the
// refusal — the emitter would write a helper for a struct missing the
// very field the backend could not carry.
//
// The control is the same record with the refused field removed, so this
// cannot pass against a walk that had stopped answering at all.
func TestRecordFieldsRefusesWholeWhenAFieldIsRefused(t *testing.T) {
	refused := []graph.RecordField{
		{Name: "city", Type: graph.TypeString},
		{Name: "img", Type: graph.TypeBytes},
	}
	plan, ok := codegen.RecordFields(refused, goCarrier)
	require.False(t, ok, "BYTES is refused by this carrier, so the record has no representation")
	require.Nil(t, plan, "a partial plan would emit a helper for fields the backend cannot carry")

	admitted, ok := codegen.RecordFields(refused[:1], goCarrier)
	require.True(t, ok, "the control: the same record without the refused field is representable")
	require.Len(t, admitted, 1)
}
