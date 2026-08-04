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
// That un-enrolment is correct only while the text gate runs AHEAD of
// codegen.Prepare, since it is Prepare that gives the portable sentinel this
// manifest names. The ordering is asserted, not assumed: this schema and this
// query are copied verbatim into internal/cli/pipeline,
// TestRunApacheAgeAnswersAnAlternationAheadOfSharedAdmission, which requires
// AGE to answer with age.ErrRelationshipTypeAlternation and neo4j-go-v5 to
// answer with the sentinel above.
//
// What this fixture's AGE enrolment used to witness — that the AGE column gate
// stands aside on a repeated label and lets the portable refusal through — is
// witnessed instead by internal/codegen/age,
// TestRejectsEdgeUnionColumns/"a duplicate label stands the gate aside even
// among distinct ones". That subtest is the one carrying this fixture's own
// candidate shape: three candidates under two labels, one repeating.
// invalid/plural_endpoint_edge_union_shared_label is NOT that witness — its
// union is two candidates under one label, so it enters edgeUnionReason in a
// different state and stays green under a mutation that breaks the stand-aside
// for the mixed case alone.

// name: GetAction :one
MATCH (x:Person)-[r:LIKES|WROTE]-(y:Post) RETURN r
