// All three targets, one sentinel. A vector inside a container — here a list
// element — is refused by every enrolled target (ADR 0050): AGE refuses a
// vector anywhere, and neo4j refuses one inside a list, a record or a union on
// both majors, a property array being homogeneous in a scalar storage type and
// no list walk in that backend decoding a vector element. Every target refusing
// under one sentinel means no refusal carries a backend name.

// name: GetDocument :one
MATCH (d:Document) RETURN d
