// NEAR is decoded nowhere but as the element of a variable-length path's
// relationship list, so that list element is the only position that owes
// decodeNear (bd gqlc-m1dk). Were codegen.DecodedEntities to miss it, the
// method would call a decoder nothing declared and the package would not
// build. Tag is never decoded, and has no decoder. neo4j only: Apache AGE
// refuses a list-of-edge column.

// name: NearChains :many
MATCH (:Tag)-[n:NEAR*1..3]->(:Tag) RETURN n
