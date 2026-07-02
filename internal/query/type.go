package query

import "encoding/json"

// marshalType renders a Type as its stringer value, quoted as a JSON string.
// Every variant's MarshalJSON routes through here so the wire encoding follows
// String, the single source, without repetition per variant.
func marshalType(t Type) ([]byte, error) { return json.Marshal(t.String()) }

// Type is the result type of a Projection: the freeze-locked type vocabulary
// the resolver reads from a parsed query (Stage 6 spec §3). It is a sealed sum
// via the private isType() marker — no foreign package can add a variant, so
// switching on a Type is exhaustive across the parser+resolver boundary. Each
// variant carries String, the single source the wire tag derives from, so the
// serialised type name cannot drift from the Go type.
//
// The sum is incremental: Stage 6 lands the scalar and collection base; Stage 7
// adds temporal variants; Stage 8 adds TypePath. TypeUnknown is the parser's
// honest posture for types it cannot compute schema-free (property lookups,
// function results, NULL propagation) — the resolver upgrades from the schema.
type Type interface {
	// String is the canonical lowercase wire name of the type ("int", "list<int>",
	// "unknown"). The single source the JSON encoding derives from.
	String() string
	isType()
}

// TypeBool is the boolean scalar: a boolean literal, an IS NULL predicate, a
// comparison, a string-comparison predicate (STARTS WITH, etc.), a boolean
// operator.
type TypeBool struct{}

// String is the wire tag "bool".
func (TypeBool) String() string { return "bool" }

// MarshalJSON renders TypeBool as its wire tag, quoted.
func (t TypeBool) MarshalJSON() ([]byte, error) { return marshalType(t) }

func (TypeBool) isType() {}

// TypeInt is the integer scalar: an integer literal or arithmetic over integer
// operands.
type TypeInt struct{}

// String is the wire tag "int".
func (TypeInt) String() string { return "int" }

// MarshalJSON renders TypeInt as its wire tag, quoted.
func (t TypeInt) MarshalJSON() ([]byte, error) { return marshalType(t) }

func (TypeInt) isType() {}

// TypeFloat is the floating-point scalar: a float literal or arithmetic when at
// least one operand is float.
type TypeFloat struct{}

// String is the wire tag "float".
func (TypeFloat) String() string { return "float" }

// MarshalJSON renders TypeFloat as its wire tag, quoted.
func (t TypeFloat) MarshalJSON() ([]byte, error) { return marshalType(t) }

func (TypeFloat) isType() {}

// TypeString is the string scalar: a string literal or string concatenation
// (via +) between two string operands.
type TypeString struct{}

// String is the wire tag "string".
func (TypeString) String() string { return "string" }

// MarshalJSON renders TypeString as its wire tag, quoted.
func (t TypeString) MarshalJSON() ([]byte, error) { return marshalType(t) }

func (TypeString) isType() {}

// TypeNull is the null literal's type. NULL participating in an operator with
// another operand produces TypeUnknown at the model boundary; propagation is
// runtime semantics (ADR 0005).
type TypeNull struct{}

// String is the wire tag "null".
func (TypeNull) String() string { return "null" }

// MarshalJSON renders TypeNull as its wire tag, quoted.
func (t TypeNull) MarshalJSON() ([]byte, error) { return marshalType(t) }

func (TypeNull) isType() {}

// TypeMap is a map literal's type. openCypher maps are heterogeneous in value
// type, so the parameterisation stops at "this is a map" — per-key typing is a
// resolver concern (spec §3).
type TypeMap struct{}

// String is the wire tag "map".
func (TypeMap) String() string { return "map" }

// MarshalJSON renders TypeMap as its wire tag, quoted.
func (t TypeMap) MarshalJSON() ([]byte, error) { return marshalType(t) }

func (TypeMap) isType() {}

// TypeNode is a whole-entity RefProjection whose Ref names a node binding.
// Reached only via RefProjection.Type(); there is no node-literal syntax.
type TypeNode struct{}

// String is the wire tag "node".
func (TypeNode) String() string { return "node" }

// MarshalJSON renders TypeNode as its wire tag, quoted.
func (t TypeNode) MarshalJSON() ([]byte, error) { return marshalType(t) }

func (TypeNode) isType() {}

// TypeEdge is a whole-entity RefProjection whose Ref names an edge binding.
// Reached only via RefProjection.Type(); there is no edge-literal syntax.
type TypeEdge struct{}

// String is the wire tag "edge".
func (TypeEdge) String() string { return "edge" }

// MarshalJSON renders TypeEdge as its wire tag, quoted.
func (t TypeEdge) MarshalJSON() ([]byte, error) { return marshalType(t) }

func (TypeEdge) isType() {}

// TypeList is a list literal's or list-valued expression's type, parameterised
// by an element Type. A mixed-element or empty list is TypeList(TypeUnknown);
// otherwise the element is the common type of the elements the parser can
// compute. Its data field is unexported, so NewTypeList is the only writer.
type TypeList struct {
	element Type
}

// NewTypeList builds a TypeList with the given element type. Total: TypeList's
// invariant is a non-nil element, and the constructor is the only writer, so an
// element of TypeUnknown covers the "cannot tell" case without ever passing nil.
func NewTypeList(element Type) TypeList {
	if element == nil {
		element = TypeUnknown{}
	}
	return TypeList{element: element}
}

// Element is the list's element type.
func (l TypeList) Element() Type { return l.element }

// String composes the wire tag as "list<" + element.String() + ">", so nested
// lists compose ("list<list<int>>") and an untyped element reads
// "list<unknown>". Falls back to "unknown" element when the zero value slips
// through (defence-in-depth against a caller who bypasses the constructor).
func (l TypeList) String() string {
	elem := "unknown"
	if l.element != nil {
		elem = l.element.String()
	}
	return "list<" + elem + ">"
}

// MarshalJSON renders TypeList as its wire tag, quoted. The element type
// composes through String, so nested lists share one encoding path.
func (l TypeList) MarshalJSON() ([]byte, error) { return marshalType(l) }

func (TypeList) isType() {}

// TypeUnknown is the parser's honest posture for types it cannot compute
// schema-free (spec §3): property lookups, function results, aggregate results,
// arithmetic involving TypeNull or TypeUnknown, and any expression whose
// result type the parser is not willing to commit to. The resolver upgrades
// TypeUnknown from the schema, and the runtime re-executes the original text
// (ADR 0005).
type TypeUnknown struct{}

// String is the wire tag "unknown".
func (TypeUnknown) String() string { return "unknown" }

// MarshalJSON renders TypeUnknown as its wire tag, quoted.
func (t TypeUnknown) MarshalJSON() ([]byte, error) { return marshalType(t) }

func (TypeUnknown) isType() {}
