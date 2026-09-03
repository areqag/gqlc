package codegen

import (
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strconv"
	"strings"

	"github.com/areqag/gqlc/internal/graph"
)

// recordFieldLegality reports the outermost record under pt whose
// declared fields have no legal Go spelling, with the reason. It is
// asked at every position that asks a TypeMap for a property carrier,
// AFTER unimplementedTypeKind and BEFORE the table, because it is the
// precondition RecordStructText assumes and the table has no channel to
// report it through — Property answers (string, bool), and "this record
// cannot be spelled" is not the same claim as "this width has no
// carrier".
//
// Two illegalities, both the mangle's doing rather than the author's
// spelling (spec §2). Two fields whose paramFieldName mangles collide
// would emit a struct declaring one Go name twice; a name of underscores
// alone mangles to the empty string and would emit a field with no name.
// Neither is Go, so without this the refusal arrives from go/format as
// ErrFormatFailure, naming a template bug for a schema's fault. The
// single-parameter form's standing exemption for $_ (prepare.go's Params
// derivation) does not extend here: a record field is ALWAYS spelled as
// a struct field, so there is no position where the empty mangle is
// harmless.
//
// The recursion is through list elements and record fields, matching
// unimplementedTypeKind, because those are the positions a record can be
// reached through. A union is refused at its own node by the walk that
// runs first, so nothing under one is reachable here.
func recordFieldLegality(pt graph.PropertyType) (graph.PropertyType, string, bool) {
	switch pt.Kind() {
	case graph.KindRecord:
		fields := pt.Fields()
		// The whole level is checked before any descent, so that where
		// two levels are both illegal the author is told about the
		// declaration they can see. Checking and descending in one loop
		// would answer with whichever offender sorted earlier, over
		// field names that have nothing to do with depth.
		seen := make(map[string]string, len(fields))
		for _, f := range fields {
			mangled := paramFieldName(f.Name)
			if mangled == "" {
				return pt, "field " + strconv.Quote(f.Name) + " mangles to no Go field name", true
			}
			if first, dup := seen[mangled]; dup {
				return pt, "fields " + strconv.Quote(first) + " and " + strconv.Quote(f.Name) + " both mangle to " + strconv.Quote(mangled), true
			}
			seen[mangled] = f.Name
		}
		for _, f := range fields {
			if offender, reason, illegal := recordFieldLegality(f.Type); illegal {
				return offender, reason, true
			}
		}
		return "", "", false
	case graph.KindList:
		return recordFieldLegality(pt.Elem())
	case graph.KindScalar, graph.KindUnion:
		// Neither declares fields: a scalar has none, and a union is
		// refused before this is asked. Named rather than left to the
		// default so a fourth kind cannot be added silently.
	}
	return "", "", false
}

// recordFieldDetail renders the tail every ErrRecordFieldCollision
// message shares, so the four fail-sites differ only in how they name
// themselves — the arrangement unimplementedKindDetail already has. When
// the declared type IS the offending record the two arguments are the
// same string and naming it twice would say nothing.
func recordFieldDetail(declared, record graph.PropertyType, reason string) string {
	if declared == record {
		return string(declared) + ", whose " + reason
	}
	return string(declared) + ", whose " + string(record) + " has " + reason
}

// RecordStructText renders the Go carrier text for a declared record's
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
func RecordStructText(fields []graph.RecordField, carrier func(graph.PropertyType) (string, bool)) (string, bool) {
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

// RecordHelperSuffix is the identifier fragment naming one record
// encoding, so a backend spells its encode/decode pair
// "encode"+suffix / "decode"+suffix.
//
// A short hash of the canonical encoding rather than the encoding
// itself, because the encoding is unbounded: RECORD<a RECORD<b
// RECORD<...>>> nests as deep as an author writes, and a name derived
// from the text would grow with it. The content-named
// agtypeListOf<Type> helpers are the precedent for deriving a helper
// name from what it decodes; the hash is what keeps that bounded (spec
// §5).
//
// It is SHARED rather than per-backend for the same reason
// RecordStructText is: the two backends must not drift on which
// encodings they consider one. graph.RecordOf sorts fields, so two
// spellings of one record are one string here and therefore one suffix
// — the same structural identity the anonymous struct gives the Go
// type.
//
// Eight hex digits is 32 bits, and NOTHING here checks for a collision
// between two distinct encodings in one emission — which would declare
// one helper twice and fail at go build of the emitted package. The
// birthday bound over n distinct encodings is about n²/2³³: 3e-7 at
// n=50, 1e-4 at n=1000. A guard would need a sentinel, a
// //gqlc:unreachable tag and two taxonomy rows for a branch no input
// can reach, so the bound is stated here instead of asserted anywhere.
func RecordHelperSuffix(pt graph.PropertyType) string {
	sum := sha256.Sum256([]byte(pt))
	return "Record" + hex.EncodeToString(sum[:4])
}

// RecordEncodings is every distinct declared-record encoding one batch
// reaches, in canonical-encoding order, so a backend emits one helper
// pair per entry and a caller can look one up by width.
//
// TRANSITIVE, because a record's decode helper calls its record fields'
// helpers rather than inlining them: the set is closed under list
// elements and record fields, which are the two positions a record can
// hide under. Without the closure a nested record would name a helper
// nothing declared, which fails at go build of the EMITTED package —
// a failure with no line in the schema to point at.
//
// graph.TypeAnyRecord is deliberately absent. It declares no fields, so
// there is no struct to build and no field-wise decode to write: both
// backends carry it as map[string]any, which is the driver's own shape
// and needs no helper. RECORD<> is present, because struct{} is a Go
// type a map still has to be checked into.
//
// The order is the encoding's own, so the emitted file is byte-stable
// across runs — a map iteration here would reorder the helper block on
// every generation and every golden would be noise.
func RecordEncodings(entities []Entity, prepared []Query) []graph.PropertyType {
	seen := make(map[graph.PropertyType]bool)
	var walk func(graph.PropertyType)
	walk = func(pt graph.PropertyType) {
		switch pt.Kind() {
		case graph.KindRecord:
			if pt == graph.TypeAnyRecord || seen[pt] {
				return
			}
			seen[pt] = true
			for _, f := range pt.Fields() {
				walk(f.Type)
			}
		case graph.KindList:
			walk(pt.Elem())
		case graph.KindScalar, graph.KindUnion:
			// Neither can hide a record: a scalar has no contents, and a
			// union is refused by the kind walk before any of this is
			// reached. Named rather than defaulted so a fourth kind
			// cannot be added silently.
		}
	}
	for _, e := range entities {
		for _, f := range e.Fields {
			walk(f.Width)
		}
	}
	for _, p := range prepared {
		for _, f := range p.ParamFields {
			walk(f.Width)
		}
		for _, f := range p.RowFields {
			walk(f.Width)
			for elem := f.ListElem; elem != nil; elem = elem.Nested {
				walk(elem.Width)
			}
		}
	}
	out := make([]graph.PropertyType, 0, len(seen))
	for pt := range seen {
		out = append(out, pt)
	}
	slices.Sort(out)
	return out
}
