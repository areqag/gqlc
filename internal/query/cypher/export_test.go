package cypher

// Opens the aggregate mint predicate to the external test package, which
// builds its argument list from a raw parse tree: Parse refuses every
// aggregate call whose argument count is wrong (bd gqlc-jjot2.12), so no query
// delivers the multi-argument call the predicate's arity clause is for.
//
// This file imports nothing, and that is load-bearing: `vuln-root-residual`
// measures a package's blindness from the imports of its in-package test files,
// and the parse tree's types are antlr's (see internal/schema/gql's
// export_test.go for the whole of the reason).
var AggregateLeavesAreRefs = aggregateLeavesAreRefs
