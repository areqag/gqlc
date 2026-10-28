package neo4j

import (
	"strings"

	"github.com/areqag/gqlc/internal/codegen"
	"github.com/areqag/gqlc/internal/graph"
)

// vectorUse records which of vector_neo4j.go's three helpers the emission
// calls. Each is gated on its own bit, because an unexported helper nothing
// calls is what `just check-goldens-unused` reds a golden for.
type vectorUse struct {
	decode    bool // toVector: an entity property or a row column read
	encode    bool // fromVector: a non-nullable parameter bound
	encodePtr bool // fromVectorPtr: a nullable parameter bound
}

func (u vectorUse) used() bool { return u.decode || u.encode || u.encodePtr }

// vectorUses walks the positions a VECTOR width can reach on this backend:
// decoded entity properties, row columns and parameters. Nothing nested is
// walked, because the type map refuses a vector inside a list, a record or
// a union, and the front end refuses one as a list element of a query value
// (ADR 0050) — so a vector is only ever a position's whole width.
func vectorUses(prepared codegen.Prepared) vectorUse {
	var u vectorUse
	for _, e := range codegen.DecodedEntities(prepared) {
		for _, f := range e.Fields {
			u.decode = u.decode || isVector(f.Width)
		}
	}
	for _, q := range prepared.Queries {
		u.markQuery(q)
	}
	return u
}

// markQuery marks one query's columns as decodes and its parameters as
// encodes, split by nullability.
func (u *vectorUse) markQuery(q codegen.Query) {
	for _, f := range q.RowFields {
		u.decode = u.decode || isVector(f.Width)
	}
	for _, f := range q.ParamFields {
		if !isVector(f.Width) {
			continue
		}
		if f.Nullable {
			u.encodePtr = true
			continue
		}
		u.encode = true
	}
}

// isVector reports whether a position's declared width is a vector. Asked of
// the WIDTH, never of the Go type: VECTOR<FLOAT32> and LIST<FLOAT32 NOT NULL>
// both emit []float32, and only the width says which wire shape arrives.
func isVector(width graph.PropertyType) bool {
	return width.Kind() == graph.KindVector
}

// vectorElem is the Go element type of a vector position's carrier: the
// text after "[]" in the slice the type map answered.
func vectorElem(goType string) string {
	return strings.TrimPrefix(goType, "[]")
}

// renderVectorConversions emits vector_neo4j.go: the unexported bridge
// between the plain slice a VECTOR property is on the emitted surface and
// the driver's dbtype.Vector[T], which wraps that same slice in its Elems
// field (ADR 0050). Generic in the element, so one body serves all six
// elements dbtype.VectorElement admits and the file does not grow per
// element the schema declares.
//
// Emitted on neo4j-go-v6 alone, because only that major's type table
// carries the width; v5 refuses it before emission, and its dbtype has no
// Vector to name.
//
// toVector takes the value as `any` and asserts, rather than reaching for
// neo4j.GetProperty or neo4j.GetRecordValue: both are constrained to an
// explicit union of instantiations, and dbtype.Vector[T] over a type
// parameter T is not one of its members, so a generic helper cannot call
// them. The assertion is what they do inside.
func renderVectorConversions(pkg string, use vectorUse, target driverTarget) []byte {
	var b strings.Builder
	b.WriteString(codegen.Header())
	b.WriteString("package ")
	b.WriteString(pkg)
	b.WriteString("\n\nimport (\n")
	if use.decode {
		b.WriteString("\t\"fmt\"\n\n")
	}
	b.WriteString("\t\"" + target.dbtypeImport + "\"\n)\n")
	if use.decode {
		b.WriteString(`
// toVector reads a value the driver hydrated from a Bolt VECTOR as the
// slice it is declared as. A value of any other shape — a LIST the same
// property was written as by another writer, or a vector of a different
// element — fails the read rather than arriving converted.
func toVector[T dbtype.VectorElement](v any) ([]T, error) {
	vec, ok := v.(dbtype.Vector[T])
	if !ok {
		return nil, fmt.Errorf("expected %T, got %T", vec, v)
	}
	return vec.Elems, nil
}
`)
	}
	if use.encode || use.encodePtr {
		b.WriteString(`
// fromVector binds a slice as a Bolt VECTOR. Bound bare, the driver would
// pack the same slice as a LIST, and the server would store a list
// rather than a vector.
func fromVector[T dbtype.VectorElement](v []T) dbtype.Vector[T] {
	return dbtype.Vector[T]{Elems: v}
}
`)
	}
	if use.encodePtr {
		b.WriteString(`
// fromVectorPtr binds a nullable vector parameter: a nil pointer is the
// Cypher null the schema's nullability declared, not an empty vector.
func fromVectorPtr[T dbtype.VectorElement](v *[]T) any {
	if v == nil {
		return nil
	}
	return fromVector(*v)
}
`)
	}
	return []byte(b.String())
}
