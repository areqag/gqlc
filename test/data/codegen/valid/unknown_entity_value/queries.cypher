// Expressions the resolver types as unknown although they hold a whole
// vertex or edge. Each column is `any` on every target, so the dynamic type
// is the driver's: on neo4j a dbtype.Node or dbtype.Relationship, on AGE the
// map[string]any agtypeValue reads from the annotated agtype (bd gqlc-2omj).
// No path is here because none reaches codegen: the resolver refuses a path
// binding at R0, measured 2026-10-04.

// name: FirstPerson :one
MATCH (p:Person) WITH p ORDER BY p.id RETURN head(collect(p)) AS first

// name: FirstKnows :one
MATCH (:Person)-[k:KNOWS]->(:Person) RETURN head(collect(k)) AS first

// name: KnowsStarts :many
MATCH (:Person)-[k:KNOWS]->(:Person) RETURN [startNode(k)] AS origins ORDER BY k.since
