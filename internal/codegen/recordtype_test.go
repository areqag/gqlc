package codegen_test

import (
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
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
	}
	panic("goCarrier: no answer declared for " + string(pt))
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
