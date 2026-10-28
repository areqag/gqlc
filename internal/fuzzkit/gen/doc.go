// Package gen holds the byte-steered generators the structured fuzz targets
// (T7 FuzzResolve, T8/T9 in internal/codegen/conformance, T10
// FuzzPipelineRun) draw their inputs from: Schema, Query and QueryFile. The
// plan that fixes this surface is docs/specs/nightly-fuzzing-plan.md §6.2.
//
// Each generator is a total, deterministic function of its *fuzzkit.Stream:
// any bytes, including none, yield text, and the same bytes yield the same
// text. Every decision past the end of the input is the first choice, and the
// first choice is the valid one, so an exhausted stream writes a schema and a
// query the pipeline admits. The controlled mutations, each documented on its
// generator, sit behind low-rate decisions a run of zero bytes never takes.
//
// The point of generating rather than mutating bytes is that the resolver and
// the backends are reached at all (plan §7). Measured 2026-10-28 at the commit
// that added this package, over 10 000 streams of 64–1024 bytes from
// math/rand/v2's PCG seeded (1, 2), with
//
//	go test ./internal/fuzzkit/gen -run '^TestAdmittedShare$' -v -gen.sample 10000 -gen.codegen
//
// each stage counting only pairs that passed the one before it:
//
//	gen.Schema parses (gql.New().Parse)                    9613  96.1 %
//	gen.Query then parses (cypher.New().Parse)             9613  96.1 %
//	the pair resolves (resolver.New(s).Resolve)            7868  78.7 %
//	neo4j-go-v5 Generate admits it                         5097  51.0 %
//	neo4j-go-v6 Generate admits it                         5324  53.2 %
//	apache-age Generate admits it                          3793  37.9 %
//	at least one backend admits it                         5386  53.9 %
//	gen.QueryFile parses (queryfile.New().Parse), apart    8901  89.0 %
//	a stage panicked                                          0
//
// The plan's floor is 30 % of pairs parsing and resolving (§6.2).
// TestAdmittedShare holds it over a 1 000-stream sample on every `go test`, so
// a generator change that drops under it goes red. The figures move with any
// change to the generators' decision order or to the pipeline's admission, so
// they are a dated measurement, not a contract; the test's log is the current
// reading. The refusals behind the gaps are mostly deliberate: the mutations
// on each generator, schema-wide backend refusals of the constructed types (a RECORD on
// neo4j, BYTES on AGE, a multi-label type on AGE per ADR 0027), and
// non-property parameters (SKIP/LIMIT $n), which codegen refuses as out of C6
// scope. No generated query is refused by the Cypher parser in this sample:
// every query mutation here is a schema-level one the resolver refuses, and
// parser-level damage is T1's to find.
//
// What it does not generate: CALL (no procedure registry is in a Vocab),
// MERGE, REMOVE, list and pattern comprehensions, EXISTS subqueries, path
// variables, and identifiers outside the fixed alphabets in schema.go. A
// defect reachable only through those is not found by a target that draws
// from here; the text-mutating arm T7 keeps (plan §7) is what can reach them.
package gen
