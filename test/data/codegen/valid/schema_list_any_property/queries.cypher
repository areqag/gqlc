// name: Bins :many
MATCH (b:Bin) RETURN b

// name: BinById :one
MATCH (b:Bin {id: $id}) RETURN b

// name: BinColumns :many
MATCH (b:Bin) RETURN b.bag AS bag, b.loose AS loose, b.piles AS piles
