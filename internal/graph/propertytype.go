package graph

// PropertyType is the normalised value type of a property; numeric types carry
// their bit width (ADR 0002).
type PropertyType string

// The normalised property types: scalars first, then the numeric families,
// each width-preserving variant its own type (ADR 0002).
const (
	TypeString    PropertyType = "STRING"
	TypeBool      PropertyType = "BOOL"
	TypeDate      PropertyType = "DATE"
	TypeTimestamp PropertyType = "TIMESTAMP"

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
)
