// name: Bins :many
MATCH (b:Bin) RETURN b

// name: BinById :one
MATCH (b:Bin {id: $id}) RETURN b
