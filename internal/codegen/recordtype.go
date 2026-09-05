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
// Three illegalities, all the mangle's doing rather than the author's
// spelling (spec §2). Two fields whose paramFieldName mangles collide
// would emit a struct declaring one Go name twice; a name of underscores
// alone mangles to the empty string and would emit a field with no name;
// and a name holding a character paramFieldName does not drop — a
// hyphen, a space, a '%', a leading digit — mangles to a spelling that
// is not a Go identifier. None is Go, so without this the refusal
// arrives from go/format as ErrFormatFailure, naming a template bug for
// a schema's fault. The single-parameter form's standing exemption for
// $_ (prepare.go's Params derivation) does not extend here: a record
// field is ALWAYS spelled as a struct field, so there is no position
// where the empty mangle is harmless.
//
// The third is reachable because a GQL field name is only obliged to be
// a legal GQL identifier, and a DELIMITED one holds anything: the parser
// accepts an accent-quoted field name spelled pct%s. It is checked
// against the unicode
// Go identifier grammar rather than an ASCII one, because paramFieldName
// deliberately keeps the author's script — a schema written in Armenian
// mangles to a legal struct field and is admitted (see goFieldName).
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
			if !goFieldName(mangled) {
				return pt, "field " + strconv.Quote(f.Name) + " mangles to " + strconv.Quote(mangled) + ", which is not a Go field name", true
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

// RecordFieldPlan is one field of a declared record as an emitter needs
// it: the key the wire map carries the value at, the Go struct field
// name the mangle produced, the backend's carrier text for the declared
// width, and the nullability that decides both the leading '*' and
// whether a missing key is an error.
//
// Width is kept beside GoType for the reason the prepared surface keeps
// it beside every other carrier (spec §6): a nested record's helper is
// named from its canonical encoding, and the struct text does not run
// backwards into a PropertyType.
type RecordFieldPlan struct {
	Key      string
	Field    string
	GoType   string
	Nullable bool
	Width    graph.PropertyType
}

// RecordFields renders the per-field emission plan for one declared
// record — RecordStructText's own walk, exposed, so a backend's
// encode/decode helper bodies read the SAME field names in the SAME
// order with the SAME nullability the struct text declared. Two walks
// would be two chances to disagree about which Go field a declared name
// mangles to, and the disagreement emits a helper assigning a field the
// struct does not have: a package that does not compile, with no line in
// the schema to point at. It is shared between the backends for the
// reason RecordStructText is (spec §5, "the record-field mangle walk").
//
// carrier is the backend's own Property, threaded in rather than
// imported, so a field inherits that backend's refusals. One refusal
// refuses the whole record and is reported as ok=false with no partial
// plan, because a struct with a hole in it is not a carrier.
//
// It assumes its fields are already LEGAL — distinct non-empty mangles —
// exactly as RecordStructText does, and for the same reason:
// recordFieldLegality answers that at each preparation site before any
// type map is asked.
func RecordFields(fields []graph.RecordField, carrier func(graph.PropertyType) (string, bool)) ([]RecordFieldPlan, bool) {
	out := make([]RecordFieldPlan, 0, len(fields))
	for _, f := range fields {
		fieldTy, ok := carrier(f.Type)
		if !ok {
			return nil, false
		}
		out = append(out, RecordFieldPlan{
			Key:   f.Name,
			Field: paramFieldName(f.Name),
			// GQL record fields are nullable by default, so NotNull is
			// the exception and the plan states the common case.
			GoType:   fieldTy,
			Nullable: !f.NotNull,
			Width:    f.Type,
		})
	}
	return out, true
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
	plan, ok := RecordFields(fields, carrier)
	if !ok {
		return "", false
	}
	var b strings.Builder
	b.WriteString("struct {\n")
	for _, f := range plan {
		b.WriteString("\t")
		b.WriteString(f.Field)
		b.WriteString(" ")
		if f.Nullable {
			// A nullable carrier is spelled with a leading * here for
			// the same reason a nullable property is.
			b.WriteString("*")
		}
		b.WriteString(f.GoType)
		b.WriteString("\n")
	}
	b.WriteString("}")
	return b.String(), true
}

// IsRecordStruct reports whether a Go type text is the anonymous struct
// a DECLARED record carries as. It is the shape question — "does this
// value arrive as a keyed collection that has to be checked into a
// struct" — and it is asked of the text because that is the currency
// every backend's render layer trades in.
//
// What it deliberately does NOT answer is WHICH record, because the text
// cannot say: the mapping does not run backwards, and there is no
// PropertyType to recover from `struct {\n\tCity *string\n}` and
// therefore no helper name. That is why the sites that need a NAME take
// the width the prepared surface carries rather than sniffing it out of
// here.
//
// RECORD<ANY> is NOT a record struct by this test, and that is the whole
// point of the distinction. It carries as map[string]any, which both
// backends already read as a value of no declared shape, so it needs no
// check and no helper of its own.
//
// A prefix test rather than a parse: RecordStructText is the only
// producer of a `struct`-prefixed type text in this pipeline, and
// recordtype_test.go pins the two forms it emits (`struct{}` for a
// record with no fields, `struct {\n...` otherwise). Every other emitted
// Go type is an identifier, a slice of one, or a qualified name.
//
// Shared rather than per-backend because it is the inverse of
// RecordStructText and belongs beside its producer. Two copies of it
// would be two chances to disagree about whether RECORD<ANY> is a
// record, and a backend that answered yes would name a helper no
// emission declares.
func IsRecordStruct(goType string) bool {
	return strings.HasPrefix(goType, "struct")
}

// IsDeclaredRecord reports whether a Go type text and the width beside it
// are a record an emission declares a helper pair for — that is, a record
// with DECLARED fields, which RecordEncodings collects and RECORD<ANY> is
// deliberately absent from.
//
// Both halves are asked, and neither alone is the test. The kind admits
// RECORD<ANY>, whose carrier is map[string]any and which needs no helper;
// the text is a struct for every declared record but says nothing about
// which, and a bare-struct test would also admit a carrier some future
// arm spells as a struct for an unrelated reason. Together they are the
// exact set RecordEncodings emits for, which is what makes a name derived
// at a call site resolve to a declaration.
func IsDeclaredRecord(goType string, width graph.PropertyType) bool {
	return width.Kind() == graph.KindRecord && IsRecordStruct(goType)
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

// RecordAliasName is the unexported type alias one declared record's
// carrier is spelled as inside the emitted package.
//
// An ALIAS, never a definition. The struct text is what every model
// field, row field and parameter of that record is declared with, and a
// defined type would be a DIFFERENT Go type from all of them — the
// helpers would not accept the values the rest of the package holds. The
// alias exists only so the helper signatures and the `var out` line do
// not carry a fifth and sixth copy of a multi-line struct text.
//
// Derived from RecordHelperSuffix rather than hashing again, so the
// carrier and its helpers cannot come from different digests: the suffix
// is "Record"+hex, and lowering its first byte is what makes the name
// unexported.
//
// Shared for the reason the suffix is, plus one the suffix does not have:
// sweepIdentifiers enrols these names, and a sweep that derived the alias
// by its own copy of the lowering rule could pass over a name the
// backends actually emit.
func RecordAliasName(pt graph.PropertyType) string {
	suffix := RecordHelperSuffix(pt)
	return strings.ToLower(suffix[:1]) + suffix[1:]
}

// RecordHelperNames is every package-level identifier the record
// emission owns for one encoding: the carrier alias and the five
// conversion helpers.
//
// All five helpers, not the subset a given batch reaches. Which
// directions are emitted is a per-backend reading of the same batch, and
// the sweep runs before any backend has made it — so a sweep over the
// reached subset would admit a name today and refuse it tomorrow when a
// query added elsewhere reached one more direction. The names are
// generator-owned and pairwise distinct by construction; reserving all
// of them costs nothing an author can observe except the refusal, which
// is the point.
func RecordHelperNames(pt graph.PropertyType) []string {
	suffix := RecordHelperSuffix(pt)
	return []string{
		RecordAliasName(pt),
		"encode" + suffix,
		"encode" + suffix + "Ptr",
		"encode" + suffix + "List",
		"encode" + suffix + "ListPtr",
		"decode" + suffix,
	}
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

// RecordSiteAlias is one site-named record alias: the exported name, the
// entity property it is named from, and the encoding it stands for.
type RecordSiteAlias struct {
	Name     string
	Entity   string
	Property string
	Width    graph.PropertyType
}

// RecordSiteAliases is the site-named alias each record-typed entity
// property gets, in entity order and then field order (spec §2.1).
//
// The ergonomics layer, and the only emitted name derived from two
// pieces of author text at once. An anonymous struct is the correct type
// — it is what keeps two declarations of one record assignable — but it
// is a poor thing to make a caller type out, so the property's own site
// supplies a name for it.
//
// ENTITY properties only. A record reached at a query column or a
// parameter has no pair of author names to derive from: the column is
// positional and the parameter's name is the query's, so a site name
// there would either collide across queries or restate the digest alias
// under a longer spelling.
//
// The population is exactly the properties whose width already has a
// digest carrier alias — RecordEncodings' membership rule, applied at
// the top level and not transitively. That is one rule and not two: a
// site alias names a carrier, so it exists where the carrier does.
// graph.TypeAnyRecord is therefore absent here as it is there, and
// RECORD<> is present, because struct{} is still a type a caller writes.
//
// Nested records get no site name even though they have a carrier,
// because the site that would name one is a FIELD of a record, not a
// property of an entity, and the entity's own name would have to be
// joined through a path to reach it.
func RecordSiteAliases(entities []Entity) []RecordSiteAlias {
	var out []RecordSiteAlias
	for _, e := range entities {
		for _, f := range e.Fields {
			if f.Width.Kind() != graph.KindRecord || f.Width == graph.TypeAnyRecord {
				continue
			}
			out = append(out, RecordSiteAlias{
				Name:     e.Name + f.Field,
				Entity:   e.Name,
				Property: f.PropName,
				Width:    f.Width,
			})
		}
	}
	return out
}
