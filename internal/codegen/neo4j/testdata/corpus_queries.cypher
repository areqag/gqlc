// name: BinColumns :many
MATCH (b:Bin) RETURN b.loose AS loose, b.bag AS bag, b.piles AS piles

// name: ListyColumns :many
MATCH (l:Listy) RETURN l.tags AS tags, l.blobs AS blobs, l.depths AS depths

// name: NestColumns :many
MATCH (n:Nest) RETURN n.cube AS cube, n.lhs AS lhs, n.rhs AS rhs, n.deep AS deep

// name: AnythingColumns :many
MATCH (a:Anything) RETURN a.payload AS payload, a.maybe AS maybe

// name: EntityColumns :many
MATCH (s:Scalar)-[e:Edgy]->(l:Listy) RETURN s AS s, e AS e, l AS l

// name: EdgeUnionColumn :many
MATCH (s:Scalar)-[e:Edgy|Edgier]->(l:Listy) RETURN e AS e

// The six queries above reach the column arms whose carrier is []any,
// dbtype.Node or dbtype.Relationship. The six below reach the rest, and
// each is here for one emitter arm rather than for a schema width:
//
//   ScalarColumns             writeSingleColumnDecodeIndent's fall-through
//                             — neo4j.GetRecordValue[T] over a scalar
//                             carrier, its err gate, and its isNil gate
//                             on both the nullable and non-nullable side.
//   OptionalEntityColumns     writeEntityColumnDecodeIndent's nullable
//                             arm, over both dbtype carriers at once.
//   OptionalEdgeUnionColumn   writeEdgeUnionColumnDecodeIndent's nullable
//                             arm, whose else-branch assigns nil rather
//                             than returning.
//   VarLengthEdgeColumn       walkListElemBody's ColumnEdge case. A
//                             variable-length pattern is what makes the
//                             resolver hand back a list over an edge;
//                             its ColumnNode case has no such reachable
//                             pattern (resolveType refuses list-of-nodes
//                             projection as out of R0 scope, and nodes(p)
//                             needs a path binding the resolver refuses).
//   VarLengthEdgeUnionColumn  walkListElemBody's ColumnEdgeUnion case,
//                             which dispatches on the wire label per
//                             element, default arm included.
//   TwoEdgeUnionColumns       the union arm's len(RowFields) > 1 branch,
//                             which suffixes raw/rel/ok/entity locals.
//                             One union column cannot collide with
//                             itself, so two are needed to observe it.
//
// name: ScalarColumns :many
MATCH (s:Scalar) RETURN s.id AS id, s.rank AS rank, s.active AS active, s.name AS name, s.weight AS weight

// name: OptionalEntityColumns :many
MATCH (s:Scalar) OPTIONAL MATCH (s)-[e:Edgy]->(l:Listy) RETURN s AS s, e AS e, l AS l

// name: OptionalEdgeUnionColumn :many
MATCH (s:Scalar) OPTIONAL MATCH (s)-[e:Edgy|Edgier]->(l:Listy) RETURN s AS s, e AS e

// name: VarLengthEdgeColumn :many
MATCH (s:Scalar)-[e:Edgy*1..3]->(l:Listy) RETURN e AS e

// name: VarLengthEdgeUnionColumn :many
MATCH (s:Scalar)-[e:Edgy|Edgier*1..3]->(l:Listy) RETURN e AS e

// name: TwoEdgeUnionColumns :many
MATCH (s:Scalar)-[e:Edgy|Edgier]->(l:Listy) MATCH (s)-[f:Edgy|Edgier]->(l) RETURN e AS e, f AS f
