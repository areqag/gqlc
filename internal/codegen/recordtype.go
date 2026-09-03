package codegen

import (
	"strings"

	"github.com/areqag/gqlc/internal/graph"
)

// recordStructText renders the Go carrier text for a declared record's
// fields: an anonymous struct, one field per declared field, in the
// canonical order Fields returns (spec §2, the ruling for gqlc-x9tg7 in
// docs/specs/codegen-record-union-carriers.md).
//
// Anonymous rather than named is the whole point. Go gives anonymous
// struct types structural identity, so two spellings of one canonical
// record — RECORD<zip INT32, city STRING> and RECORD<city STRING, zip
// INT32> encode identically — produce the same text and therefore the
// SAME Go type, with no registry and no name mangle to collide. That is
// what lets TypeMap.Property stay a pure function of the resolved type.
//
// carrier is the backend's own Property, threaded in rather than
// imported, because a field carrier must inherit that backend's
// refusals: RECORD<img BYTES> is unrepresentable on AGE for the same
// reason a bare BYTES property is. One refusal anywhere refuses the
// whole record, because a struct with a hole in it is not a carrier.
//
// The multi-line form is measured, not assumed: emitted inside a struct
// field declaration, a slice element, an alias right-hand side and a
// parameter list, gofmt leaves it byte-identical, while the single-line
// `struct{ City *string; Zip *int32 }` is rewritten to this form. Field
// alignment is deliberately not padded here — Finalise runs every file
// through format.Source, which owns the padding.
//
// It assumes its fields are already LEGAL: distinct non-empty mangles.
// recordFieldLegality answers that at each of the four preparation
// sites, before any type map is asked, so that Property keeps its pure
// (string, bool) signature and has no error channel to need.
func recordStructText(fields []graph.RecordField, carrier func(graph.PropertyType) (string, bool)) (string, bool) {
	if len(fields) == 0 {
		// A record with no declared fields is the unit type, and Go
		// spells the unit type struct{}. Reached for RECORD<> only:
		// RECORD<ANY> also has nil Fields but is intercepted by the
		// TypeAnyRecord arm before this is called, because undeclared
		// fields are a different claim from no fields.
		return "struct{}", true
	}
	var b strings.Builder
	b.WriteString("struct {\n")
	for _, f := range fields {
		fieldTy, ok := carrier(f.Type)
		if !ok {
			return "", false
		}
		b.WriteString("\t")
		b.WriteString(paramFieldName(f.Name))
		b.WriteString(" ")
		if !f.NotNull {
			// GQL record fields are nullable by default, and a
			// nullable carrier is spelled with a leading * here for
			// the same reason a nullable property is.
			b.WriteString("*")
		}
		b.WriteString(fieldTy)
		b.WriteString("\n")
	}
	b.WriteString("}")
	return b.String(), true
}
