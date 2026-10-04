// name: OneMarker :one
MATCH (m:Marker) RETURN m

// name: OneLink :one
MATCH (:Marker)-[l:LINKS]->(:Marker) RETURN l
