// name: BinColumns :many
MATCH (b:Bin) RETURN b.loose AS loose, b.bag AS bag

// name: ListyColumns :many
MATCH (l:Listy) RETURN l.tags AS tags, l.blobs AS blobs, l.depths AS depths

// NestColumns is spelled over LITERALS, where every other read here
// projects a declared property, and the difference is ADR 0035's. The
// columns are nested lists; the neo4j server serves those as query
// values and refuses to STORE them, so there is no longer a nested-list
// property in the schema to project one from — and the decode under test
// belongs to the query path anyway (render_queries.go, binding
// inner<n>/innerAcc<n>) rather than to the entity path.
//
// The literals fix the column TYPES and nothing else: the driver values
// each decode runs against are supplied by the test, so the empty and
// multi-element lists the assertions need are not spelled here.
//
// TWO THINGS THIS NO LONGER COVERS, named rather than left as silence:
// the columns are non-nullable, because a literal is, so the nullable
// nested-column arm that `deep` reached is unwitnessed here; and nothing
// in this file now decodes a nested list off dbtype.Node at all, that
// being the arm ADR 0035 makes unreachable and the same change deletes.
//
// name: NestColumns :many
RETURN [[[1]]] AS cube, [[1]] AS lhs, [["a"]] AS rhs, [[["x"]]] AS deep

// name: AnythingColumns :many
MATCH (a:Anything) RETURN a.payload AS payload, a.maybe AS maybe

// name: AnyPropColumns :many
MATCH (a:AnyProp) RETURN a.badge AS badge, a.tag AS tag

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

// The only parameterised query here, and the only one whose subject is
// what the emission SENDS rather than what it reads back. Every query
// above is parameterless, so before this one nothing the corpus ran
// reached paramBindExpr or the from<X>List helpers at all — they were
// held by byte goldens, which pin text and not behaviour, and by the
// compiler, which accepts both of the mistakes that matter here: binding
// a temporal list bare compiles and then raises UnsupportedTypeError on
// the wire, and dropping from<X>ListPtr's nil guard compiles and then
// panics on the null the schema declared (bd gqlc-rw0m).
//
// $windows was here, and its removal costs the ORDERING witness as well
// as the depth-2 bind: the emitter keeps one list depth per carrier and
// must keep the DEEPEST, so a depth-2 parameter declared before a depth-1
// one over the same carrier caught a last-one-wins emitter. Every bind
// left is depth 1, so nothing here now distinguishes keeping the deepest
// from keeping the last. ADR 0035 leaves no neo4j property of a nested
// width to compare a parameter against, so this is not replaceable on
// this backend.
//
// name: SlotsMatching :many
MATCH (s:Slot)
WHERE s.tags = $tags AND s.days = $days AND s.spans = $spans
RETURN s.id AS id
