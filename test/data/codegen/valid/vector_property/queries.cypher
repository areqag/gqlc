// neo4j-go-v6 alone. A VECTOR is a plain Go slice on the emitted surface,
// []float32 for VECTOR<FLOAT32>, and the driver's dbtype.Vector[T] at the
// boundary, reached only through vector_neo4j.go (ADR 0051). neo4j-go-v5 has
// no dbtype.Vector and refuses the width
// (test/data/codegen/invalid/vector_width_driver_version), and Apache AGE has
// no vector type and refuses it too (test/data/codegen/invalid/vector_width_age).
//
// The two vector properties differ in NULLABILITY and in ELEMENT, and both
// are load-bearing. embedding is []float32 and sketch is *[]int8: the
// whole-value star is the emission site's, and a nullable vector reads and
// binds through different arms (a Props presence test on read, fromVectorPtr
// on bind). Two elements show the bridge is generic in the element rather than
// emitted per width — vector_neo4j.go has one toVector, not two.
//
// The dimension is dropped (ADR 0002): nothing below or in the golden spells
// a 4, and a slice of any length binds.
//
// The positions are the three a width can reach. DocumentWhole decodes the
// entity through models.go; DocumentVectors projects each property as a
// COLUMN, which is different code from the property read; and the parameters
// of WriteDocument and DocumentsByEmbedding are the encode direction, in both
// nullabilities, which between them reach every helper vector_neo4j.go can
// emit.
//
// THE LIVE HALF is TestNeo4jStoresAndRoundTripsAVector, and it does not run
// against the pinned image: neo4j:5-community is Neo4j 5.26 on Bolt 5.8, and a
// vector needs Bolt 6.0 (Neo4j 2025.10 or later). Measured on 2026-10-27, the
// pinned server answered a vector parameter with "Unexpected struct tag: 0x56",
// and a 2025.10 community server refused to STORE one (its aligned store
// format holds no vector property). Against a 2025.10 enterprise server, in a
// scratch copy with only the image changed, the test ran 6/6 PASS. Moving the
// pin is an owner call, so in CI the test skips naming why, and this golden is
// compiled and asserted byte-for-byte.

// name: DocumentWhole :one
MATCH (d:Document) WHERE d.id = $id RETURN d

// name: DocumentVectors :many
MATCH (d:Document) RETURN d.id AS id, d.embedding AS embedding, d.sketch AS sketch

// name: DocumentEmbedding :one
MATCH (d:Document) WHERE d.id = $id RETURN d.embedding AS embedding

// name: DocumentsByEmbedding :many
MATCH (d:Document) WHERE d.embedding = $embedding RETURN d.id AS id

// name: DocumentsBySketch :many
MATCH (d:Document) WHERE d.sketch = $sketch RETURN d.id AS id

// name: WriteDocument :exec
CREATE (d:Document {id: $id, embedding: $embedding, sketch: $sketch})
