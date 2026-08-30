// name: ReadingColumns :many
MATCH (r:Reading) RETURN r.tags AS tags, r.ranks AS ranks, r.flags AS flags, r.marks AS marks

// name: ReadingWhole :one
MATCH (r:Reading) RETURN r

// name: DropTagged :exec
MATCH (r:Reading {tags: $tags}) DELETE r
