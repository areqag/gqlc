// Enrolled on neo4j-go-v5 only, and NOT on apache-age-pgx-v5, which the
// alternation below puts out of reach: Apache AGE 1.7.0's parser has no '|' in
// a relationship detail, so that backend refuses this text ahead of anything
// the column shape could say (internal/codegen/age,
// rejectRelationshipTypeAlternation) and answers with
// age.ErrRelationshipTypeAlternation rather than the sentinel this manifest
// names. A manifest names one expectedError for every target it enrols, and
// the corpus cannot name a backend sentinel anyway (conformance_test.go,
// sentinelIdent resolves codegen/queryfile/cypher only).
//
// What that enrolment used to witness — that AGE stands aside on a repeated
// label and lets the portable refusal through — is witnessed instead by
// invalid/plural_endpoint_edge_union_shared_label, whose pattern names one
// relationship type and spells no '|', and so reaches the shared refusal on
// AGE too.

// name: GetAction :one
MATCH (x:Person)-[r:LIKES|WROTE]-(y:Post) RETURN r
