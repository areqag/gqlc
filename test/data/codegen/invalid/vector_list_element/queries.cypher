// neo4j-go-v6, which carries the bare width, so what is refused is the LIST.
// collect() over a vector property projects a list query value whose element
// is a vector, and no list walk on this backend decodes one: the element's
// []float32 carrier would ride the walk for LIST<FLOAT32>, which asserts each
// element to []any and fails on every vector at run time. The shared front
// end refuses it at the list-element plan instead (ADR 0051), naming the list
// width, LIST<VECTOR<FLOAT32>>, which is the width the type map refuses as a
// declared property too (test/data/codegen/invalid/vector_list_property).

// name: AllEmbeddings :one
MATCH (d:Document) RETURN collect(d.embedding) AS embeddings
