// name: EveryoneScored :many
MATCH (p:Person) RETURN score(p.id) AS score
