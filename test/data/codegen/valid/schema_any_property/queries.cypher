// name: EventIds :many
MATCH (e:Event) RETURN e.id AS id

// name: EventColumns :many
MATCH (e:Event) RETURN e.marker AS marker, e.payload AS payload

// name: EventMarker :one
MATCH (e:Event) RETURN e.marker AS marker

// name: EventPayload :one
MATCH (e:Event) RETURN e.payload AS payload

// name: EventPropertyColumns :many
MATCH (e:Event) RETURN e.badge AS badge, e.tag AS tag

// name: EventBadge :one
MATCH (e:Event) RETURN e.badge AS badge

// name: EventTag :one
MATCH (e:Event) RETURN e.tag AS tag
