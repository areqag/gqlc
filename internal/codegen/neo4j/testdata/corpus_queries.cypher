// name: BinColumns :many
MATCH (b:Bin) RETURN b.loose AS loose, b.bag AS bag, b.piles AS piles

// name: ListyColumns :many
MATCH (l:Listy) RETURN l.tags AS tags, l.blobs AS blobs, l.depths AS depths

// name: NestColumns :many
MATCH (n:Nest) RETURN n.cube AS cube, n.lhs AS lhs, n.rhs AS rhs

// name: AnythingColumns :many
MATCH (a:Anything) RETURN a.payload AS payload, a.maybe AS maybe
