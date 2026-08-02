package age

import (
	"github.com/areqag/gqlc/internal/graph"
	"github.com/areqag/gqlc/internal/resolver"
)

// typeMap is the backend's Go-type table (spec §5.1) the shared phases
// read. Stateless: every entry is a pure function of the resolved type.
type typeMap struct{}

// Property maps a resolved property type to its native Go emission (spec
// §5.1). Returns (typeText, ok): ok=false routes the caller to
// ErrUnrepresentableWidth naming the width. Callers append a leading '*'
// for nullable columns and parameters at emission time.
//
// agtype's scalar vocabulary is null, boolean, integer (int64), float
// (float64), string, list and map. Narrower integer and float widths ride
// the wider carrier and narrow through a Go conversion, the same
// arrangement every backend uses. BYTES and the five temporal widths have
// no agtype scalar at all: their encoding is a storage decision the
// temporal arm owns, so admitting them here would emit a column no
// decoder can fill. The eight oversized numeric widths have no faithful
// carrier anywhere and are permanently out (§9).
//
// LIST and ANY are refused on the same grounds. This backend's decode
// vocabulary is one helper per agtype scalar, so a property of either
// width would reach a struct field with no helper to fill it.
func (typeMap) Property(pt graph.PropertyType) (string, bool) {
	if pt.Kind() == graph.KindList {
		return "", false
	}
	switch pt {
	case graph.TypeString:
		return "string", true
	case graph.TypeBool:
		return "bool", true
	case graph.TypeInt:
		return "int", true
	case graph.TypeInt8:
		return "int8", true
	case graph.TypeInt16:
		return "int16", true
	case graph.TypeInt32:
		return "int32", true
	case graph.TypeInt64:
		return "int64", true
	case graph.TypeUint:
		return "uint", true
	case graph.TypeUint8:
		return "uint8", true
	case graph.TypeUint16:
		return "uint16", true
	case graph.TypeUint32:
		return "uint32", true
	case graph.TypeUint64:
		return "uint64", true
	case graph.TypeFloat, graph.TypeFloat64:
		return "float64", true
	case graph.TypeFloat32:
		return "float32", true
	case graph.TypeAnyPropertyValue,
		graph.TypeList,
		graph.TypeBytes,
		graph.TypeDate, graph.TypeTime, graph.TypeLocalTime,
		graph.TypeTimestamp, graph.TypeDuration,
		graph.TypeInt128, graph.TypeInt256,
		graph.TypeUint128, graph.TypeUint256,
		graph.TypeFloat16, graph.TypeFloat128, graph.TypeFloat256,
		graph.TypeDecimal:
		return "", false
	}
	// PropertyType is an open string type, so a width internal/graph gains
	// without a row above arrives here rather than failing to compile.
	// Rejecting it routes the caller to ErrUnrepresentableWidth naming the
	// width: generation fails loudly instead of emitting a field no
	// decoder can fill.
	return "", false
}

// Temporal maps a resolved temporal-expression kind to the Go type text
// this backend emits. Returns (typeText, ok): ok=false routes the caller
// to ErrUnrepresentableTemporal naming the kind.
//
// agtype has no temporal value in its vocabulary and no cast reaching
// one, so every arm refuses. Encoding them is a storage decision (bd
// gqlc-35yu.11); its spike found a faithful encoding for some kinds and
// none for a calendar duration, so arms are admitted one at a time
// (ADR 0025).
func (typeMap) Temporal(k resolver.Temporal) (string, bool) {
	switch k {
	case resolver.TemporalDate:
		return "", false
	case resolver.TemporalTime:
		return "", false
	case resolver.TemporalLocalTime:
		return "", false
	case resolver.TemporalDateTime:
		return "", false
	case resolver.TemporalLocalDateTime:
		return "", false
	case resolver.TemporalDuration:
		return "", false
	}
	// Only a value converted in from outside resolver.Temporal's
	// vocabulary reaches here. Refusing stays right if that vocabulary
	// grows in a build this file was not recompiled against.
	return "", false
}

// Scalar maps a resolved scalar-expression kind to the Go type text this
// backend emits (spec §5.1 column-shape table). Null → any: the
// openCypher null literal is a legal-but-pointless projection.
//
// The bool / int / float / string arms are the Go shape of an agtype
// scalar a decode helper reads. The null and map arms are not: decodeFunc
// has no agtypeNull or agtypeMap arm, so unservedColumn refuses a column
// of either kind and no emitted code reaches them. They name what a
// helper would fill, not a carrier that exists (ADR 0025).
//
// Sharp edge for the day agtypeMap lands: agtype arrives as JSON, so an
// integer inside a map decodes to float64 where the neo4j driver yields
// int64. Same declared type on both backends, different runtime type,
// and TestBackendInvariantSurface compares declarations.
func (typeMap) Scalar(k resolver.Scalar) string {
	switch k {
	case resolver.ScalarBool:
		return "bool"
	case resolver.ScalarInt:
		return "int64"
	case resolver.ScalarFloat:
		return "float64"
	case resolver.ScalarString:
		return "string"
	case resolver.ScalarNull:
		return "any"
	case resolver.ScalarMap:
		return "map[string]any"
	}
	// Only a value converted in from outside resolver.Scalar's vocabulary
	// reaches here; projecting it undecoded beats guessing a Go type for
	// a kind the resolver never named.
	return "any"
}
