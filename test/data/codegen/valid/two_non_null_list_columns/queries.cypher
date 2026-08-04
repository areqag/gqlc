// name: ListyColumns :many
MATCH (l:Listy) RETURN l.tags AS tags, l.ranks AS ranks

// name: ListyRow :one
MATCH (l:Listy) RETURN l.tags AS tags, l.ranks AS ranks

// name: ListyMixed :many
MATCH (l:Listy) RETURN l.tags AS tags, l.spare AS spare, l.ranks AS ranks
