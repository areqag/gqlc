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
