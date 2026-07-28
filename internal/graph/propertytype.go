package graph

import "strings"

// PropertyType is the normalised value type of a property; numeric types carry
// their bit width (ADR 0002). List types are encoded as "LIST<ELEM>" where
// ELEM is the element's PropertyType string; the element is "ELEM NOT NULL"
// when the element type carries a NOT NULL qualifier. Methods Kind, Elem, and
// ElemNotNull provide structured access to these encodings without parsing.
type PropertyType string

// PropertyTypeKind distinguishes scalar property types from parameterised list
// types. The zero value is KindScalar so uninitialized values name a scalar.
type PropertyTypeKind int

// KindScalar is the zero value; KindList means the PropertyType encodes a list.
const (
	KindScalar PropertyTypeKind = iota
	KindList
)

// Kind returns KindList when pt is a list type (encoded as "LIST<...>"),
// KindScalar otherwise. It is the discriminator that callers must read before
// switching on the scalar constant table.
func (pt PropertyType) Kind() PropertyTypeKind {
	if strings.HasPrefix(string(pt), "LIST<") {
		return KindList
	}
	return KindScalar
}

// Elem returns the element type of a list PropertyType. The result is
// undefined (returns "") when Kind() != KindList.
func (pt PropertyType) Elem() PropertyType {
	s := strings.TrimPrefix(string(pt), "LIST<")
	s = strings.TrimSuffix(s, ">")
	s = strings.TrimSuffix(s, " NOT NULL")
	return PropertyType(s)
}

// ElemNotNull reports whether the element type of a list PropertyType carries
// a NOT NULL qualifier. Returns false when Kind() != KindList.
func (pt PropertyType) ElemNotNull() bool {
	s := strings.TrimPrefix(string(pt), "LIST<")
	return strings.HasSuffix(s, " NOT NULL>")
}

// ListOf constructs a list PropertyType whose element type is elem and whose
// element carries a NOT NULL qualifier when elemNotNull is true. Use TypeList
// for a bare LIST / ARRAY with no declared element type.
func ListOf(elem PropertyType, elemNotNull bool) PropertyType {
	if elemNotNull {
		return PropertyType("LIST<" + string(elem) + " NOT NULL>")
	}
	return PropertyType("LIST<" + string(elem) + ">")
}

// The normalised property types: scalars first, then the numeric families,
// each width-preserving variant its own type (ADR 0002).
const (
	TypeString    PropertyType = "STRING"
	TypeBytes     PropertyType = "BYTES"
	TypeBool      PropertyType = "BOOL"
	TypeDate      PropertyType = "DATE"
	TypeTime      PropertyType = "TIME"
	TypeLocalTime PropertyType = "LOCALTIME"
	TypeTimestamp PropertyType = "TIMESTAMP"
	TypeDuration  PropertyType = "DURATION"

	TypeInt    PropertyType = "INT"
	TypeInt8   PropertyType = "INT8"
	TypeInt16  PropertyType = "INT16"
	TypeInt32  PropertyType = "INT32"
	TypeInt64  PropertyType = "INT64"
	TypeInt128 PropertyType = "INT128"
	TypeInt256 PropertyType = "INT256"

	TypeUint    PropertyType = "UINT"
	TypeUint8   PropertyType = "UINT8"
	TypeUint16  PropertyType = "UINT16"
	TypeUint32  PropertyType = "UINT32"
	TypeUint64  PropertyType = "UINT64"
	TypeUint128 PropertyType = "UINT128"
	TypeUint256 PropertyType = "UINT256"

	TypeFloat    PropertyType = "FLOAT"
	TypeFloat16  PropertyType = "FLOAT16"
	TypeFloat32  PropertyType = "FLOAT32"
	TypeFloat64  PropertyType = "FLOAT64"
	TypeFloat128 PropertyType = "FLOAT128"
	TypeFloat256 PropertyType = "FLOAT256"

	TypeDecimal PropertyType = "DECIMAL"

	// TypeAnyPropertyValue is the open dynamic union of storable property value
	// types — ISO GQL's ANY VALUE and ANY? PROPERTY VALUE. Both spellings are the
	// same type: a property whose value the author did not or could not constrain
	// further. Emitted as Go's any. (ADR 0020)
	TypeAnyPropertyValue PropertyType = "ANY"

	// TypeList is the bare list type — ISO GQL's LIST and ARRAY with no declared
	// element type. Equivalent to LIST<ANY VALUE>; emitted as Go's []any.
	TypeList PropertyType = "LIST<ANY>"
)
