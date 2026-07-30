// name: EventsInRange :many
MATCH (e:Event) WHERE e.created >= $from AND e.created <= $to RETURN e.name AS name
