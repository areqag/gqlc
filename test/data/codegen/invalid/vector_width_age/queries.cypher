// Apache AGE alone. The declaration is the one
// test/data/codegen/valid/vector_property emits on neo4j-go-v6.
//
// AGE stores lists at any depth, so a vector's coordinates WOULD round-trip
// as an agtype list. It is refused anyway, on the owner's ruling (ADR 0051):
// AGE has no vector type and no vector functions — no similarity search, no
// dimension enforcement — so the stored value would be a list that only looks
// like the vector the schema declared, and VECTOR would mean one thing on
// neo4j-go-v6 and another here. The refusal names the Apache AGE backend,
// because neo4j-go-v6 accepts the declaration (ADR 0035's attribution rule),
// and gives that reason.

// name: GetDocument :one
MATCH (d:Document) RETURN d
