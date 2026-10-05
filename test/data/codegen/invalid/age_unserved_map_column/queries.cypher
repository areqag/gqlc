// name: EveryoneAsAMap :many
MATCH (p:Person) RETURN {id: p.id} AS m
