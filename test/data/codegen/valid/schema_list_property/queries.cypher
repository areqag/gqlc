// name: GetEventTags :one
MATCH (e:Event) RETURN e.tags AS tags

// name: AllEvents :many
MATCH (e:Event) RETURN e
